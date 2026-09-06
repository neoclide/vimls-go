package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/neoclide/vimls-go/internal/syntax"
)

var ErrResolverRoot = errors.New("workspace path resolver root is invalid")

// PathResolution describes a local path lookup. Path is empty when no safe,
// readable regular file was found. Candidates are the safe paths considered,
// in Vim's search order; they are useful to callers for explaining a missing
// file without exposing a path that escaped the configured roots. Dynamic is
// true when the expression cannot be evaluated without running Vimscript.
type PathResolution struct {
	Path       string
	Candidates []string
	Dynamic    bool
}

type PathCompletion struct {
	Display string
	Path    string
	IsDir   bool
}

// ImportPathCompletions returns direct children for a literal import path.
// It deliberately does not recurse: completion must not turn an edit into a
// workspace walk. The bool reports that a caller-visible limit was reached.
func (r *PathResolver) ImportPathCompletions(from, prefix string, autoload bool, limit int, acceptPath ...func(string) bool) ([]PathCompletion, bool) {
	if r == nil || limit <= 0 || !safeImportCompletionPrefix(prefix) {
		return nil, false
	}
	absolute := isAbsolutePath(prefix)
	var roots []string
	if absolute {
		roots = []string{filepath.VolumeName(prefix) + string(filepath.Separator)}
	} else if strings.HasPrefix(prefix, ".") {
		base := r.root
		if from != "" {
			if absolute, err := filepath.Abs(from); err == nil {
				base = filepath.Dir(absolute)
			}
		}
		roots = []string{base}
	} else {
		directory := "import"
		if autoload {
			directory = "autoload"
		}
		for _, runtimePath := range r.runtimePaths {
			roots = append(roots, filepath.Join(runtimePath, directory))
		}
	}
	result := make([]PathCompletion, 0)
	seen := make(map[string]struct{})
	fromCanonical, _ := r.Canonical(from)
	truncated := false
	for _, root := range roots {
		dirPart, name := filepath.Split(filepath.FromSlash(prefix))
		directory := dirPart
		if !absolute {
			directory = filepath.Join(root, dirPart)
		}
		canonical, ok := r.Canonical(directory)
		if !ok {
			continue
		}
		entries, err := os.ReadDir(canonical)
		if err != nil {
			continue
		}
		checked := 0
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), name) {
				continue
			}
			if !entry.IsDir() && !strings.HasSuffix(entry.Name(), ".vim") {
				continue
			}
			// Bound filesystem validation after cheap name filtering, so an
			// unrelated alphabetic prefix cannot hide an exact match.
			if checked == 200 {
				truncated = true
				break
			}
			checked++
			path := filepath.Join(canonical, entry.Name())
			info, err := os.Stat(path)
			if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
				continue
			}
			pathCanonical, ok := r.Canonical(path)
			if !ok {
				continue
			}
			if fromCanonical != "" && pathCanonical == fromCanonical {
				continue
			}
			if len(acceptPath) > 0 && acceptPath[0] != nil && !acceptPath[0](pathCanonical) {
				continue
			}
			value := filepath.ToSlash(filepath.Join(dirPart, entry.Name()))
			if strings.HasPrefix(prefix, "./") && !strings.HasPrefix(value, "./") {
				value = "./" + value
			}
			if info.IsDir() {
				value += "/"
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			if len(result) == limit || len(result) == 2000 {
				truncated = true
				continue
			}
			result = append(result, PathCompletion{Display: value, Path: path, IsDir: info.IsDir()})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Display < result[j].Display })
	return result, truncated
}

func safeImportCompletionPrefix(prefix string) bool {
	if strings.ContainsAny(prefix, "\x00\r\n") || (strings.Contains(prefix, "\\") && (runtime.GOOS != "windows" || !isAbsolutePath(prefix))) {
		return false
	}
	return true
}

// PathResolver resolves Vim9 imports and source paths using only local files.
// Root represents Vim's current working directory/workspace boundary. Runtime
// paths are searched in order, like 'runtimepath'. The resolver never follows
// a candidate outside Root or a configured runtime path, including through a
// symlink.
type PathResolver struct {
	root         string
	runtimePaths []string
	boundaries   []string
}

// NewPathResolver creates a resolver rooted at root. Runtime paths may be
// absent or not yet created, which is useful while a workspace is loading.
// Existing runtime paths are canonicalized so symlink checks are applied to
// their actual location.
func NewPathResolver(root string, runtimePaths []string) (*PathResolver, error) {
	return NewPathResolverForRoots([]string{root}, runtimePaths)
}

// NewPathResolverForRoots creates a resolver for one or more workspace roots.
// The first valid root supplies Vim's cwd-equivalent root for :source; all
// valid roots remain security boundaries for paths originating in any open
// workspace. Runtime paths retain their caller-provided precedence.
func NewPathResolverForRoots(roots []string, runtimePaths []string) (*PathResolver, error) {
	resolver := &PathResolver{}
	seenBoundaries := make(map[string]struct{}, len(roots)+len(runtimePaths))
	seenRuntimePaths := make(map[string]struct{}, len(runtimePaths))
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		absRoot = filepath.Clean(absRoot)
		info, err := os.Stat(absRoot)
		if err != nil || !info.IsDir() {
			continue
		}
		boundary, err := canonicalPath(absRoot)
		if err != nil {
			continue
		}
		if resolver.root == "" {
			// Keep the lexical root for returned paths and candidate construction;
			// boundaries use canonical paths for mount and symlink safety.
			resolver.root = absRoot
		}
		if _, exists := seenBoundaries[boundary]; exists {
			continue
		}
		seenBoundaries[boundary] = struct{}{}
		resolver.boundaries = append(resolver.boundaries, boundary)
	}
	if resolver.root == "" {
		return nil, ErrResolverRoot
	}
	for _, runtimePath := range runtimePaths {
		if strings.TrimSpace(runtimePath) == "" {
			continue
		}
		path, err := filepath.Abs(runtimePath)
		if err != nil {
			continue
		}
		path = filepath.Clean(path)
		runtimePath := path
		if info, statErr := os.Stat(runtimePath); statErr == nil && !info.IsDir() {
			continue
		}
		boundary, canonicalErr := canonicalPathAllowMissing(runtimePath)
		if canonicalErr != nil {
			continue
		}
		if _, exists := seenRuntimePaths[boundary]; exists {
			continue
		}
		seenRuntimePaths[boundary] = struct{}{}
		// Vim's runtime lookup uses DIP_NOAFTER for imports and autoloads.
		// An explicit after entry is still trusted as a path boundary, but is
		// not itself a search root.
		if filepath.Base(runtimePath) != "after" {
			resolver.runtimePaths = append(resolver.runtimePaths, runtimePath)
		}
		if _, exists := seenBoundaries[boundary]; !exists {
			seenBoundaries[boundary] = struct{}{}
			resolver.boundaries = append(resolver.boundaries, boundary)
		}
	}
	return resolver, nil
}

// Canonical returns path with existing symlinks and existing parents resolved
// when the resulting path remains within a configured boundary. Missing paths
// retain their missing suffix after their nearest existing parent is resolved.
func (r *PathResolver) Canonical(path string) (string, bool) {
	if r == nil || strings.TrimSpace(path) == "" {
		return "", false
	}
	canonical, err := canonicalPathAllowMissing(path)
	if err != nil || !r.withinBoundary(canonical) {
		return "", false
	}
	return canonical, true
}

// Allows reports whether path has a canonical spelling within a configured
// workspace or runtime boundary.
func (r *PathResolver) Allows(path string) bool {
	_, ok := r.Canonical(path)
	return ok
}

// ResolveImport resolves a parsed :import command. Literal string imports
// are resolved exactly as Vim does: relative names use the importing script's
// directory, absolute names are direct, and other names are searched under
// import/ or autoload/ in each runtime path. Non-literal expressions are
// returned as Dynamic and are deliberately not guessed.
func (r *PathResolver) ResolveImport(from string, file *syntax.File, importNode *syntax.Import) PathResolution {
	if r == nil || file == nil || importNode == nil {
		return PathResolution{}
	}
	return r.ResolveImportPath(from, file.Text(importNode.PathSpan), importNode.Autoload)
}

// ResolveImportPath resolves the retained path spelling of a parsed import.
// It is used by workspace index facts after the syntax tree itself has been
// discarded.
func (r *PathResolver) ResolveImportPath(from, raw string, autoload bool) PathResolution {
	if r == nil {
		return PathResolution{}
	}
	spec, ok := decodeStaticPath(raw)
	if !ok {
		return PathResolution{Dynamic: true}
	}
	candidates := r.importCandidates(from, spec, autoload)
	return r.choose(candidates)
}

// StaticImportPath decodes a literal import expression without executing
// Vimscript. Dynamic expressions return ok=false.
func StaticImportPath(raw string) (path string, ok bool) {
	return decodeStaticPath(raw)
}

// RuntimeImport reports whether a literal import uses import/ or autoload/
// lookup below 'runtimepath', rather than a relative or absolute path.
func RuntimeImport(raw string) bool {
	path, ok := decodeStaticPath(raw)
	return ok && path != "" && RuntimeImportCompletionPrefix(path)
}

// RuntimeImportCompletionPrefix reports whether a path prefix completes below
// runtimepath rather than beside the importing script or from an absolute root.
func RuntimeImportCompletionPrefix(prefix string) bool {
	return !strings.HasPrefix(prefix, ".") && !isAbsolutePath(prefix)
}

// ResolveAutoload maps a legacy name such as foo#bar#Func to
// autoload/foo/bar.vim in configured runtime-path order. This follows Vim's
// autoload_name() rule and never guesses a name without a complete prefix.
func (r *PathResolver) ResolveAutoload(name string) PathResolution {
	if r == nil {
		return PathResolution{}
	}
	relative, ok := AutoloadPath(name)
	if !ok {
		return PathResolution{}
	}
	candidates := make([]string, 0, len(r.runtimePaths))
	for _, runtimePath := range r.runtimePaths {
		candidates = append(candidates, filepath.Join(runtimePath, filepath.FromSlash(relative)))
	}
	return r.choose(candidates)
}

// AutoloadPath maps a complete legacy autoload name to its runtime-relative
// source path without accessing the filesystem.
func AutoloadPath(name string) (string, bool) {
	name = strings.TrimPrefix(name, "g:")
	separator := strings.LastIndexByte(name, '#')
	if separator <= 0 || separator == len(name)-1 {
		return "", false
	}
	prefix := strings.ReplaceAll(name[:separator], "#", "/") + ".vim"
	return "autoload/" + prefix, true
}

// ResolveSource resolves the direct filename accepted by :source. Vim does
// not search 'runtimepath' for :source; a relative name is relative to the
// current working directory, represented here by Root. The from argument is
// accepted for symmetry with ResolveImport and for future source-buffer
// callers, but does not alter Vim's cwd-relative behavior.
func (r *PathResolver) ResolveSource(from, spec string) PathResolution {
	if r == nil {
		return PathResolution{}
	}
	value, ok := decodeStaticSourcePath(spec)
	if !ok {
		return PathResolution{Dynamic: strings.TrimSpace(spec) != ""}
	}
	if value == "" {
		return PathResolution{}
	}
	if isAbsolutePath(value) {
		return r.choose([]string{value})
	}
	return r.choose([]string{filepath.Join(r.root, filepath.FromSlash(value))})
}

// decodeStaticSourcePath decodes the already-scanned argument of :source.
// Unlike :import, :source does not accept a Vim string expression: quote
// bytes are part of a filename (or, in legacy script, were removed earlier
// as a comment). Vim expands unescaped special filename forms before opening
// the file, so those are dynamic for static analysis. On Unix Vim then halves
// backslashes in an EX_NOSPC filename argument.
func decodeStaticSourcePath(raw string) (string, bool) {
	// Command.Argument has already had unprotected trailing whitespace
	// removed by syntax. Keep protected trailing whitespace intact here.
	raw = strings.TrimLeft(raw, " \t")
	if raw == "" {
		return "", true
	}
	var builder strings.Builder
	builder.Grow(len(raw))
	for index := 0; index < len(raw); index++ {
		character := raw[index]
		if character == '\\' {
			if index+1 >= len(raw) {
				builder.WriteByte(character)
				continue
			}
			index++
			builder.WriteByte(raw[index])
			continue
		}
		if character == 0x16 {
			// EX_XFILE preserves a literal CTRL-V in the argument. Its
			// terminal-input meaning cannot be reproduced as a path byte.
			return "", false
		}
		switch character {
		case '%', '#', '<', '$', '~', '*', '?', '[', '`':
			return "", false
		case 0:
			return "", false
		default:
			builder.WriteByte(character)
		}
	}
	return builder.String(), true
}

func (r *PathResolver) importCandidates(from, spec string, autoload bool) []string {
	if spec == "" {
		return nil
	}
	if strings.HasPrefix(spec, ".") {
		base := r.root
		if from != "" {
			if absolute, err := filepath.Abs(from); err == nil {
				base = filepath.Dir(filepath.Clean(absolute))
			}
		}
		return []string{filepath.Join(base, filepath.FromSlash(spec))}
	}
	if isAbsolutePath(spec) {
		return []string{spec}
	}
	directory := "import"
	if autoload {
		directory = "autoload"
	}
	candidates := make([]string, 0, len(r.runtimePaths))
	for _, runtimePath := range r.runtimePaths {
		candidates = append(candidates, filepath.Join(runtimePath, directory, filepath.FromSlash(spec)))
	}
	return candidates
}

func (r *PathResolver) choose(candidates []string) PathResolution {
	result := PathResolution{Candidates: make([]string, 0, len(candidates))}
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		absolute = filepath.Clean(absolute)
		resolved, safe, regular := r.safeRegularFile(absolute)
		if !safe {
			continue
		}
		identity := resolved
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		result.Candidates = append(result.Candidates, resolved)
		if result.Path == "" && regular {
			result.Path = resolved
		}
	}
	return result
}

func (r *PathResolver) safeRegularFile(path string) (resolved string, safe, regular bool) {
	info, err := os.Stat(path)
	if err != nil {
		resolved, resolveErr := canonicalPathAllowMissing(path)
		return resolved, resolveErr == nil && r.withinBoundary(resolved), false
	}
	if !info.Mode().IsRegular() {
		return path, false, false
	}
	resolved, err = canonicalPath(path)
	if err != nil {
		return path, false, false
	}
	for _, boundary := range r.boundaries {
		if pathWithinOrEqual(boundary, resolved) {
			return resolved, true, true
		}
	}
	return resolved, false, true
}

func (r *PathResolver) withinBoundary(path string) bool {
	for _, boundary := range r.boundaries {
		if pathWithinOrEqual(boundary, path) {
			return true
		}
	}
	return false
}

func canonicalPath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Clean(resolved))
}

// CanonicalPath returns the filesystem identity of path. Existing paths have
// all symlinks resolved; a missing suffix is preserved after its nearest
// existing parent is resolved.
func CanonicalPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("empty path")
	}
	return canonicalPathAllowMissing(path)
}

// canonicalPathAllowMissing resolves every existing path component and then
// appends the still-missing suffix. A dangling symlink is rejected instead of
// being treated as an ordinary missing filename.
func canonicalPathAllowMissing(path string) (string, error) {
	path, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	current := path
	for {
		_, statErr := os.Lstat(current)
		if statErr == nil {
			resolved, resolveErr := canonicalPath(current)
			if resolveErr != nil {
				return "", resolveErr
			}
			suffix, relativeErr := filepath.Rel(current, path)
			if relativeErr != nil {
				return "", relativeErr
			}
			if suffix == "." {
				return resolved, nil
			}
			return filepath.Join(resolved, suffix), nil
		}
		if !os.IsNotExist(statErr) {
			return "", statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", statErr
		}
		current = parent
	}
}

func pathWithinOrEqual(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func isAbsolutePath(path string) bool {
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\") {
		return true
	}
	// filepath.VolumeName is platform-specific. Recognize a Windows drive
	// path while parsing on Unix too, so an input cannot be mistaken for a
	// runtime-relative filename merely because the server runs elsewhere.
	return len(path) >= 3 && ((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z')) && path[1] == ':' && (path[2] == '/' || path[2] == '\\')
}

func decodeStaticPath(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 {
		return "", false
	}
	if raw[0] == '$' {
		// Interpolated strings can depend on variables or expressions. Vim
		// permits them in :import, but static analysis must not evaluate them.
		return "", false
	}
	quote := raw[0]
	if (quote != '\'' && quote != '"') || raw[len(raw)-1] != quote {
		return "", false
	}
	value := raw[1 : len(raw)-1]
	if quote == '\'' {
		var builder strings.Builder
		for index := 0; index < len(value); index++ {
			if value[index] != '\'' {
				builder.WriteByte(value[index])
				continue
			}
			if index+1 >= len(value) || value[index+1] != '\'' {
				return "", false
			}
			builder.WriteByte('\'')
			index++
		}
		return builder.String(), true
	}
	var builder strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] != '\\' {
			builder.WriteByte(value[index])
			continue
		}
		if index+1 >= len(value) {
			return "", false
		}
		index++
		switch value[index] {
		case 'b':
			builder.WriteByte('\b')
		case 'e':
			builder.WriteByte(0x1b)
		case 'f':
			builder.WriteByte('\f')
		case 'n':
			builder.WriteByte('\n')
		case 'r':
			builder.WriteByte('\r')
		case 't':
			builder.WriteByte('\t')
		case '0', '1', '2', '3', '4', '5', '6', '7':
			valueNumber := int(value[index] - '0')
			for count := 1; count < 3 && index+1 < len(value) && value[index+1] >= '0' && value[index+1] <= '7'; count++ {
				index++
				valueNumber = valueNumber*8 + int(value[index]-'0')
			}
			if valueNumber == 0 {
				return "", false
			}
			builder.WriteByte(byte(valueNumber))
		case 'x', 'X':
			valueNumber, consumed := decodeHexEscape(value, index+1, 2)
			if consumed == 0 {
				builder.WriteByte(value[index])
				continue
			}
			index += consumed
			if valueNumber == 0 {
				return "", false
			}
			builder.WriteByte(byte(valueNumber))
		case 'u', 'U':
			limit := 4
			if value[index] == 'U' {
				limit = 8
			}
			valueNumber, consumed := decodeHexEscape(value, index+1, limit)
			if consumed == 0 {
				builder.WriteByte(value[index])
				continue
			}
			index += consumed
			character := rune(valueNumber)
			if character == 0 || !utf8.ValidRune(character) {
				return "", false
			}
			builder.WriteRune(character)
		case '<':
			// Key-code expansion depends on Vim's terminal tables and is not a
			// filesystem spelling that static analysis can reproduce safely.
			return "", false
		default:
			// Vim drops the backslash for an otherwise unknown escape.
			builder.WriteByte(value[index])
		}
	}
	return builder.String(), true
}

func decodeHexEscape(source string, start, limit int) (int, int) {
	value := 0
	consumed := 0
	for start+consumed < len(source) && consumed < limit {
		digit := hexDigit(source[start+consumed])
		if digit < 0 {
			break
		}
		value = value*16 + digit
		consumed++
	}
	return value, consumed
}

func hexDigit(character byte) int {
	switch {
	case character >= '0' && character <= '9':
		return int(character - '0')
	case character >= 'a' && character <= 'f':
		return int(character-'a') + 10
	case character >= 'A' && character <= 'F':
		return int(character-'A') + 10
	default:
		return -1
	}
}
