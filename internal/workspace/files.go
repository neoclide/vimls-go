package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

var caseInsensitiveFS = runtime.GOOS == "windows"

var errDiscoveryLimit = errors.New("workspace file discovery limit reached")

var vimRuntimeDirectories = map[string]struct{}{
	"after":    {},
	"autoload": {},
	"colors":   {},
	"compiler": {},
	"ftplugin": {},
	"indent":   {},
	"import":   {},
	"keymap":   {},
	"pack":     {},
	"plugin":   {},
	"syntax":   {},
}

var vcsMetadataNames = map[string]struct{}{
	".bzr":           {},
	".git":           {},
	".gitattributes": {},
	".gitignore":     {},
	".gitmodules":    {},
	".hg":            {},
	".svn":           {},
	"CVS":            {},
}

var vimConfigNames = map[string]struct{}{
	".exrc":     {},
	".gvimrc":   {},
	".vimrc":    {},
	"_exrc":     {},
	"_gvimrc":   {},
	"_vimrc":    {},
	"exrc":      {},
	"ginit.vim": {},
	"gvimrc":    {},
	"init.vim":  {},
	"vimrc":     {},
}

// DiscoverFiles returns Vim script files below root in deterministic lexical
// order. A non-positive limit means no limit. A positive limit returns at
// most limit files and sets truncated when another matching file exists.
//
// Only regular files are returned. Symlinked directories are never traversed;
// a symlink to a regular file is returned only when its resolved target stays
// below root. Permission-denied entries are skipped; other walk errors return
// no files.
func DiscoverFiles(root string, limit int) (files []string, truncated bool, err error) {
	return DiscoverFilesContext(context.Background(), root, limit)
}

// DiscoverFilesContext is DiscoverFiles with cancellation checks before and
// during filesystem traversal. It cannot interrupt a filesystem syscall that
// is already blocked, but stops ordinary WalkDir traversal promptly.
func DiscoverFilesContext(ctx context.Context, root string, limit int) (files []string, truncated bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, false, fmt.Errorf("resolve workspace root %q: %w", root, err)
	}
	absoluteRoot = filepath.Clean(absoluteRoot)
	ignoredRoots := ignoredDiscoveryRoots()
	if canonicalRoot, canonicalErr := canonicalPath(absoluteRoot); canonicalErr == nil {
		if _, ignored := ignoredRoots[canonicalRoot]; ignored {
			return []string{}, false, nil
		}
	}
	rootInfo, err := os.Lstat(absoluteRoot)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return []string{}, false, nil
		}
		return nil, false, fmt.Errorf("stat workspace root %q: %w", absoluteRoot, err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, false, fmt.Errorf("workspace root %q must not be a symlink", absoluteRoot)
	}
	if !rootInfo.IsDir() {
		return nil, false, fmt.Errorf("workspace root %q is not a directory", absoluteRoot)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return []string{}, false, nil
		}
		return nil, false, fmt.Errorf("resolve workspace root %q: %w", absoluteRoot, err)
	}
	resolvedRoot, err = filepath.Abs(resolvedRoot)
	if err != nil {
		return nil, false, fmt.Errorf("resolve workspace root %q: %w", absoluteRoot, err)
	}
	resolvedRoot = filepath.Clean(resolvedRoot)

	seenFiles := make(map[string]struct{})
	walkErr := filepath.WalkDir(absoluteRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrPermission) {
				if entry != nil && entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			return walkErr
		}
		name := entry.Name()
		if _, skip := vcsMetadataNames[name]; skip {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if name == "node_modules" {
				return fs.SkipDir
			}
			relative, relativeErr := filepath.Rel(absoluteRoot, path)
			if relativeErr != nil {
				return relativeErr
			}
			canonicalDirectory := filepath.Clean(filepath.Join(resolvedRoot, relative))
			if _, ignored := ignoredRoots[canonicalDirectory]; ignored {
				return fs.SkipDir
			}
			return nil
		}

		relative, err := filepath.Rel(absoluteRoot, path)
		if err != nil {
			return err
		}
		underRuntimeDirectory := hasVimRuntimeDirectory(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				if errors.Is(err, os.ErrPermission) {
					return nil
				}
				return err
			}
			resolved, err = filepath.Abs(resolved)
			if err != nil {
				return err
			}
			resolved = filepath.Clean(resolved)
			if !pathWithin(resolvedRoot, resolved) {
				return nil
			}
			info, err := os.Stat(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				if errors.Is(err, os.ErrPermission) {
					return nil
				}
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
		} else {
			info, err := entry.Info()
			if err != nil {
				if errors.Is(err, os.ErrPermission) {
					return nil
				}
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
		}
		if !isVimFile(relative, name, underRuntimeDirectory) {
			return nil
		}
		canonical, canonicalErr := CanonicalPath(path)
		if canonicalErr != nil {
			if errors.Is(canonicalErr, os.ErrPermission) {
				return nil
			}
			return canonicalErr
		}
		if _, seen := seenFiles[canonical]; seen {
			return nil
		}
		seenFiles[canonical] = struct{}{}
		files = append(files, canonical)
		if limit > 0 && len(files) > limit {
			return errDiscoveryLimit
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errDiscoveryLimit) {
		if errors.Is(walkErr, context.Canceled) || errors.Is(walkErr, context.DeadlineExceeded) {
			return nil, false, walkErr
		}
		return nil, false, fmt.Errorf("walk workspace root %q: %w", absoluteRoot, walkErr)
	}
	files = uniqueSorted(files)
	if limit > 0 && len(files) > limit {
		return files[:limit], true, nil
	}
	return files, false, nil
}

// RuntimePathFiles separates scripts that should be parsed from colorscheme
// files that only need to be retained for completion.
type RuntimePathFiles struct {
	Sources []string
	Colors  []string
}

// DiscoverRuntimePathFiles returns the statically useful part of an external
// runtimepath root. plugin, autoload, and import are recursive. Colors contain
// only direct *.vim children and are catalogued separately so they need not be
// parsed merely to provide :colorscheme completion.
func DiscoverRuntimePathFiles(root string, limit int) (RuntimePathFiles, bool, error) {
	return DiscoverRuntimePathFilesContext(context.Background(), root, limit)
}

// DiscoverRuntimePathFilesContext is DiscoverRuntimePathFiles with
// cancellation checks.
func DiscoverRuntimePathFilesContext(ctx context.Context, root string, limit int) (RuntimePathFiles, bool, error) {
	if err := ctx.Err(); err != nil {
		return RuntimePathFiles{}, false, err
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return RuntimePathFiles{}, false, fmt.Errorf("resolve runtimepath root %q: %w", root, err)
	}
	absoluteRoot = filepath.Clean(absoluteRoot)
	if canonicalRoot, canonicalErr := canonicalPath(absoluteRoot); canonicalErr == nil {
		if _, ignored := ignoredDiscoveryRoots()[canonicalRoot]; ignored {
			return RuntimePathFiles{}, false, nil
		}
	}
	rootInfo, err := os.Lstat(absoluteRoot)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return RuntimePathFiles{}, false, nil
		}
		return RuntimePathFiles{}, false, fmt.Errorf("stat runtimepath root %q: %w", absoluteRoot, err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return RuntimePathFiles{}, false, fmt.Errorf("runtimepath root %q must not be a symlink", absoluteRoot)
	}
	if !rootInfo.IsDir() {
		return RuntimePathFiles{}, false, fmt.Errorf("runtimepath root %q is not a directory", absoluteRoot)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return RuntimePathFiles{}, false, fmt.Errorf("resolve runtimepath root %q: %w", absoluteRoot, err)
	}
	resolvedRoot = filepath.Clean(resolvedRoot)

	var result RuntimePathFiles
	for _, directory := range []string{"plugin", "autoload", "import"} {
		if err := ctx.Err(); err != nil {
			return RuntimePathFiles{}, false, err
		}
		directoryPath := filepath.Join(absoluteRoot, directory)
		info, statErr := os.Lstat(directoryPath)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) || errors.Is(statErr, os.ErrPermission) {
				continue
			}
			return RuntimePathFiles{}, false, fmt.Errorf("stat runtimepath directory %q: %w", directoryPath, statErr)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		files, _, discoverErr := DiscoverFilesContext(ctx, directoryPath, 0)
		if discoverErr != nil {
			return RuntimePathFiles{}, false, discoverErr
		}
		for _, path := range files {
			if strings.EqualFold(filepath.Ext(path), ".vim") {
				result.Sources = append(result.Sources, path)
			}
		}
	}

	colorsPath := filepath.Join(absoluteRoot, "colors")
	entries, readErr := os.ReadDir(colorsPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) && !errors.Is(readErr, os.ErrPermission) {
		return RuntimePathFiles{}, false, fmt.Errorf("read runtimepath directory %q: %w", colorsPath, readErr)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return RuntimePathFiles{}, false, err
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".vim") {
			continue
		}
		path := filepath.Join(colorsPath, entry.Name())
		info, infoErr := os.Stat(path)
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		canonical, canonicalErr := CanonicalPath(path)
		if canonicalErr == nil && pathWithin(resolvedRoot, canonical) {
			result.Colors = append(result.Colors, canonical)
		}
	}
	result.Sources = uniqueSorted(result.Sources)
	result.Colors = uniqueSorted(result.Colors)
	if limit > 0 && len(result.Sources) > limit {
		result.Sources = result.Sources[:limit]
		return result, true, nil
	}
	return result, false, nil
}

// IsRuntimePathSourcePath reports whether path is one of the source locations
// selected by DiscoverRuntimePathFiles.
func IsRuntimePathSourcePath(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) < 2 || !strings.EqualFold(filepath.Ext(parts[len(parts)-1]), ".vim") {
		return false
	}
	switch strings.ToLower(parts[0]) {
	case "plugin":
		return true
	case "autoload", "import":
		return true
	default:
		return false
	}
}

// IsRuntimePathColorPath reports whether path is a direct colors/*.vim child.
func IsRuntimePathColorPath(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	return len(parts) == 2 && strings.EqualFold(parts[0], "colors") && strings.EqualFold(filepath.Ext(parts[1]), ".vim")
}

func ignoredDiscoveryRoots() map[string]struct{} {
	roots := make(map[string]struct{}, 2)
	candidates := []string{os.TempDir()}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, home)
	}
	for _, candidate := range candidates {
		canonical, err := canonicalPath(candidate)
		if err == nil {
			roots[canonical] = struct{}{}
		}
	}
	return roots
}

func uniqueSorted(files []string) []string {
	sort.Strings(files)
	if len(files) < 2 {
		return files
	}
	out := files[:1]
	for _, file := range files[1:] {
		if file != out[len(out)-1] {
			out = append(out, file)
		}
	}
	return out
}

func isVimConfigName(name string) bool {
	lookup := name
	if caseInsensitiveFS {
		lookup = strings.ToLower(name)
	}
	_, ok := vimConfigNames[lookup]
	return ok
}

func hasVimRuntimeDirectory(relative string) bool {
	for part := range strings.SplitSeq(relative, string(filepath.Separator)) {
		lookup := part
		if caseInsensitiveFS {
			lookup = strings.ToLower(part)
		}
		if _, ok := vimRuntimeDirectories[lookup]; ok {
			return true
		}
	}
	return false
}

func isVimFile(relative, name string, underRuntimeDirectory bool) bool {
	if underRuntimeDirectory {
		extension := strings.ToLower(filepath.Ext(name))
		return extension == "" || extension == ".vim"
	}
	if filepath.Dir(relative) == "." {
		if isVimConfigName(name) {
			return true
		}
	}
	if caseInsensitiveFS {
		return strings.EqualFold(filepath.Ext(name), ".vim")
	}
	return strings.HasSuffix(name, ".vim")
}

// IsVimSourcePath applies discovery's lexical source selection to a file event.
// File type and symlink safety are checked separately by the caller.
func IsVimSourcePath(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	parts := strings.Split(relative, string(filepath.Separator))
	for index, part := range parts {
		if _, skip := vcsMetadataNames[part]; skip {
			return false
		}
		if part == "node_modules" && index < len(parts)-1 {
			return false
		}
	}
	return isVimFile(relative, filepath.Base(path), hasVimRuntimeDirectory(relative))
}

// VimFileWatchPattern covers discovery's source names. Runtime subtrees are
// watched broadly enough for extensionless files; event selection filters them.
func VimFileWatchPattern() string {
	literal := func(name string) string {
		if !caseInsensitiveFS {
			return name
		}
		var result strings.Builder
		for _, c := range name {
			if c >= 'a' && c <= 'z' {
				result.WriteByte('[')
				result.WriteRune(c)
				result.WriteRune(c - 'a' + 'A')
				result.WriteByte(']')
			} else {
				result.WriteRune(c)
			}
		}
		return result.String()
	}
	patterns := []string{"**/*." + literal("vim")}
	for name := range vimConfigNames {
		patterns = append(patterns, literal(name))
	}
	for name := range vimRuntimeDirectories {
		patterns = append(patterns, "**/"+literal(name)+"/**")
	}
	sort.Strings(patterns)
	return "{" + strings.Join(patterns, ",") + "}"
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative != ".." && relative != "." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
