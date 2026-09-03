package workspace

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// IsConfigFile reports whether filePath represents a user Vim configuration file.
// Evaluation order:
//  1. Explicit configFiles patterns take precedence.
//  2. Known config filenames (e.g. .vimrc, init.vim) are always config files.
//  3. Files under workspace roots are config files unless located in a standard Vim runtime directory
//     (plugin, autoload, etc.) relative to the workspace root.
//  4. Other files under runtime roots (e.g. $VIMRUNTIME/defaults.vim) default to non-config files.
//  5. Standalone files outside all roots default to config files without scanning absolute ancestor paths.
func IsConfigFile(filePath string, configPatterns []string, workspaceRoots []string, runtimeRoots []string) bool {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return false
	}
	filePath = filepath.Clean(filePath)

	canonical := filePath
	if c, err := CanonicalPath(filePath); err == nil {
		canonical = c
	}

	// 1. Explicit configFiles patterns take precedence (matching raw filePath or canonical path).
	for _, pattern := range configPatterns {
		if MatchConfigFilePattern(pattern, filePath) || (canonical != filePath && MatchConfigFilePattern(pattern, canonical)) {
			return true
		}
	}

	// 2. Known config filenames (e.g. .vimrc, vimrc, init.vim).
	base := filepath.Base(filePath)
	if isVimConfigName(base) || (canonical != filePath && isVimConfigName(filepath.Base(canonical))) {
		return true
	}

	// 3. Files under workspace roots: check runtime directory relative to the workspace root using canonical and raw paths.
	for _, wsRoot := range workspaceRoots {
		if wsRoot == "" {
			continue
		}
		target := canonical
		if !pathWithin(wsRoot, target) && pathWithin(wsRoot, filePath) {
			target = filePath
		}
		if target == wsRoot || pathWithin(wsRoot, target) {
			if rel, err := filepath.Rel(wsRoot, target); err == nil {
				if hasVimRuntimeDirectory(filepath.Dir(rel)) {
					return false
				}
			}
			return true
		}
	}

	// 4. Other files located under a runtime root default to non-config files.
	for _, rtp := range runtimeRoots {
		if rtp != "" && (canonical == rtp || pathWithin(rtp, canonical) || filePath == rtp || pathWithin(rtp, filePath)) {
			return false
		}
	}

	// 5. Standalone files outside all roots default to config files without scanning ancestor path segments.
	return true
}

// MatchConfigFilePattern reports whether an absolute pattern matches filePath,
// taking into account "~/" user home directory expansion and "*" / "**" glob wildcards.
// Patterns that are not absolute paths (or ~ expanded) are ignored and return false.
func MatchConfigFilePattern(pattern, filePath string, _ ...string) bool {
	pattern = strings.TrimSpace(pattern)
	filePath = strings.TrimSpace(filePath)
	if pattern == "" || filePath == "" {
		return false
	}
	pattern = expandHome(pattern)
	if !isAbsolutePath(pattern) {
		return false
	}
	filePath = filepath.Clean(filePath)

	normPattern := filepath.ToSlash(pattern)
	normPath := filepath.ToSlash(filePath)

	return matchGlobPath(normPattern, normPath)
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func matchGlobPath(pattern, path string) bool {
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(path)

	var sb strings.Builder
	if caseInsensitiveFS {
		sb.WriteString("(?i)")
	}
	sb.WriteString("^")
	i := 0
	for i < len(pattern) {
		switch {
		case strings.HasPrefix(pattern[i:], "/**/"):
			sb.WriteString("/(?:.*/)?")
			i += 4
		case strings.HasPrefix(pattern[i:], "/**"):
			sb.WriteString("/.*")
			i += 3
		case strings.HasPrefix(pattern[i:], "**/"):
			sb.WriteString("(?:.*/)?")
			i += 3
		case strings.HasPrefix(pattern[i:], "**"):
			sb.WriteString(".*")
			i += 2
		case pattern[i] == '*':
			sb.WriteString("[^/]*")
			i++
		case pattern[i] == '?':
			sb.WriteString("[^/]")
			i++
		case pattern[i] == '.':
			sb.WriteString(`\.`)
			i++
		case strings.ContainsRune(`+()^$|{}[]\`, rune(pattern[i])):
			sb.WriteString(`\`)
			sb.WriteByte(pattern[i])
			i++
		default:
			sb.WriteByte(pattern[i])
			i++
		}
	}
	sb.WriteString("$")
	re, err := regexp.Compile(sb.String())
	if err != nil {
		return false
	}
	return re.MatchString(path)
}
