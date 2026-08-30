package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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
	".exrc":   {},
	".gvimrc": {},
	".vimrc":  {},
	"_exrc":   {},
	"_gvimrc": {},
	"_vimrc":  {},
	"exrc":    {},
	"gvimrc":  {},
	"vimrc":   {},
}

// DiscoverFiles returns Vim script files below root in deterministic lexical
// order. A non-positive limit means no limit. A positive limit returns at
// most limit files and sets truncated when another matching file exists.
//
// Only regular files are returned. Symlinked directories are never traversed;
// a symlink to a regular file is returned only when its resolved target stays
// below root. Walk errors return no files.
func DiscoverFiles(root string, limit int) (files []string, truncated bool, err error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, false, fmt.Errorf("resolve workspace root %q: %w", root, err)
	}
	absoluteRoot = filepath.Clean(absoluteRoot)
	rootInfo, err := os.Lstat(absoluteRoot)
	if err != nil {
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
		return nil, false, fmt.Errorf("resolve workspace root %q: %w", absoluteRoot, err)
	}
	resolvedRoot, err = filepath.Abs(resolvedRoot)
	if err != nil {
		return nil, false, fmt.Errorf("resolve workspace root %q: %w", absoluteRoot, err)
	}
	resolvedRoot = filepath.Clean(resolvedRoot)

	walkErr := filepath.WalkDir(absoluteRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
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
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
		} else {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
		}
		if !isVimFile(relative, name, underRuntimeDirectory) {
			return nil
		}
		files = append(files, filepath.Clean(path))
		if limit > 0 && len(files) > limit {
			return errDiscoveryLimit
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errDiscoveryLimit) {
		return nil, false, fmt.Errorf("walk workspace root %q: %w", absoluteRoot, walkErr)
	}
	sort.Strings(files)
	if limit > 0 && len(files) > limit {
		return files[:limit], true, nil
	}
	return files, false, nil
}

func hasVimRuntimeDirectory(relative string) bool {
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if _, ok := vimRuntimeDirectories[part]; ok {
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
		if _, ok := vimConfigNames[name]; ok {
			return true
		}
	}
	return strings.HasSuffix(name, ".vim")
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative != ".." && relative != "." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
