package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/chemzqm/vimls-go/internal/syntax"
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
	if strings.TrimSpace(root) == "" {
		return nil, ErrResolverRoot
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve resolver root %q: %w", root, err)
	}
	absRoot = filepath.Clean(absRoot)
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("stat resolver root %q: %w", absRoot, err)
	}
	if !info.IsDir() {
		return nil, ErrResolverRoot
	}
	rootBoundary, err := canonicalPath(absRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve resolver root %q: %w", absRoot, err)
	}
	// Keep the lexical root for returned paths and candidate construction;
	// boundaries use the canonical root so /tmp-style mount aliases do not
	// make an in-root file appear to escape.
	resolver := &PathResolver{root: absRoot}
	resolver.boundaries = append(resolver.boundaries, rootBoundary)
	seen := make(map[string]struct{}, len(runtimePaths))
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
		if _, exists := seen[boundary]; exists {
			continue
		}
		seen[boundary] = struct{}{}
		resolver.runtimePaths = append(resolver.runtimePaths, runtimePath)
		resolver.boundaries = append(resolver.boundaries, boundary)
	}
	return resolver, nil
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
	raw := file.Text(importNode.PathSpan)
	spec, ok := decodeStaticPath(raw)
	if !ok {
		return PathResolution{Dynamic: true}
	}
	candidates := r.importCandidates(from, spec, importNode.Autoload)
	return r.choose(candidates)
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
		if _, exists := seen[absolute]; exists {
			continue
		}
		seen[absolute] = struct{}{}
		_, safe, regular := r.safeRegularFile(absolute)
		if !safe {
			continue
		}
		result.Candidates = append(result.Candidates, absolute)
		if result.Path == "" && regular {
			// Keep the spelling used by the caller. A safe symlink is still a
			// valid Vim path, and preserving it makes source locations stable.
			result.Path = absolute
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
	if filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
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
