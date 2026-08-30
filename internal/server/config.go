package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/workspace"
)

const DefaultTargetVersion = "9.1.0000"
const MaximumTargetVersion = "9.2.1015"

var ErrInvalidTargetVersion = errors.New("invalid Vim target version")

const defaultUnresolvedSeverity = syntax.DiagnosticWarning

type TargetVersion struct {
	Major  int
	Minor  int
	Patch  int
	Latest bool
}

func (v TargetVersion) String() string {
	if v.Latest {
		return "latest"
	}
	return fmt.Sprintf("%d.%d.%04d", v.Major, v.Minor, v.Patch)
}

func ParseTargetVersion(value string) (TargetVersion, error) {
	if value == "latest" {
		return TargetVersion{Latest: true}, nil
	}
	parts := strings.Split(value, ".")
	if len(parts) != 2 && len(parts) != 3 {
		return TargetVersion{}, fmt.Errorf("%w: expected major.minor[.patch] or latest", ErrInvalidTargetVersion)
	}
	numbers := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			return TargetVersion{}, fmt.Errorf("%w: empty version component", ErrInvalidTargetVersion)
		}
		for _, b := range []byte(part) {
			if b < '0' || b > '9' {
				return TargetVersion{}, fmt.Errorf("%w: non-decimal version component", ErrInvalidTargetVersion)
			}
		}
		number, err := strconv.Atoi(part)
		if err != nil {
			return TargetVersion{}, fmt.Errorf("%w: %v", ErrInvalidTargetVersion, err)
		}
		numbers[i] = number
	}
	version := TargetVersion{Major: numbers[0], Minor: numbers[1]}
	if len(numbers) == 3 {
		version.Patch = numbers[2]
	}
	if version.Patch > 9999 {
		return TargetVersion{}, fmt.Errorf("%w: patch must be at most 9999", ErrInvalidTargetVersion)
	}
	if version.Major < 9 || version.Major == 9 && version.Minor < 1 {
		return TargetVersion{}, fmt.Errorf("%w: versions before 9.1 are unsupported", ErrInvalidTargetVersion)
	}
	if version.Major > 9 || version.Minor > 2 || version.Minor == 2 && version.Patch > 1015 {
		return TargetVersion{}, fmt.Errorf("%w: versions after %s are not described by this build", ErrInvalidTargetVersion, MaximumTargetVersion)
	}
	return version, nil
}

func targetVersionFromOptions(raw any) (TargetVersion, bool, string) {
	fallback, _ := ParseTargetVersion(DefaultTargetVersion)
	if raw == nil {
		return fallback, false, ""
	}
	var options map[string]any
	switch value := raw.(type) {
	case map[string]any:
		options = value
	case []byte:
		if len(value) == 0 || string(value) == "null" {
			return fallback, false, ""
		}
		if err := json.Unmarshal(value, &options); err != nil {
			return fallback, false, "vimls: initializationOptions must be an object; using target 9.1.0000"
		}
	default:
		return fallback, false, "vimls: initializationOptions must be an object; using target 9.1.0000"
	}
	target, exists := options["targetVersion"]
	if !exists || target == nil {
		return fallback, false, ""
	}
	value, ok := target.(string)
	if !ok {
		return fallback, false, "vimls: targetVersion must be a string; using target 9.1.0000"
	}
	version, err := ParseTargetVersion(value)
	if err != nil {
		return fallback, false, fmt.Sprintf("vimls: %v; using target 9.1.0000", err)
	}
	return version, true, ""
}

func unresolvedSeverityFromOptions(raw any) (syntax.DiagnosticSeverity, string) {
	if raw == nil {
		return defaultUnresolvedSeverity, ""
	}
	var options map[string]any
	switch value := raw.(type) {
	case map[string]any:
		options = value
	case []byte:
		if len(value) == 0 || string(value) == "null" {
			return defaultUnresolvedSeverity, ""
		}
		if err := json.Unmarshal(value, &options); err != nil {
			return defaultUnresolvedSeverity, "vimls: initializationOptions must be an object; using unresolvedSeverity warning"
		}
	default:
		return defaultUnresolvedSeverity, "vimls: initializationOptions must be an object; using unresolvedSeverity warning"
	}
	value, exists := options["unresolvedSeverity"]
	if !exists || value == nil {
		return defaultUnresolvedSeverity, ""
	}
	text, ok := value.(string)
	if !ok {
		return defaultUnresolvedSeverity, "vimls: unresolvedSeverity must be a string; using warning"
	}
	severity, ok := parseDiagnosticSeverity(text)
	if !ok {
		return defaultUnresolvedSeverity, "vimls: unresolvedSeverity must be error, warning, information, or hint; using warning"
	}
	return severity, ""
}

func runtimepathFromOptions(raw any) ([]string, bool, string) {
	if raw == nil {
		return nil, false, ""
	}
	var options map[string]any
	switch value := raw.(type) {
	case map[string]any:
		options = value
	case []byte:
		if len(value) == 0 || string(value) == "null" {
			return nil, false, ""
		}
		if err := json.Unmarshal(value, &options); err != nil {
			return nil, false, "vimls: initializationOptions must be an object; ignoring runtimepath"
		}
	default:
		return nil, false, "vimls: initializationOptions must be an object; ignoring runtimepath"
	}
	rawPaths, exists := options["runtimepath"]
	if !exists || rawPaths == nil {
		return nil, false, ""
	}
	var paths []string
	switch values := rawPaths.(type) {
	case []string:
		paths = append(paths, values...)
	case []any:
		paths = make([]string, 0, len(values))
		for _, rawPath := range values {
			path, ok := rawPath.(string)
			if !ok {
				return nil, true, "vimls: runtimepath must be an array of strings; ignoring runtimepath"
			}
			paths = append(paths, path)
		}
	default:
		return nil, true, "vimls: runtimepath must be an array of strings; ignoring runtimepath"
	}
	return normalizeRuntimePaths(paths), true, ""
}

// normalizeRuntimePaths keeps the caller's order while dropping paths that
// cannot be used.  EvalSymlinks supplies the realpath identity used for
// de-duplication, which is important on macOS where several spellings can
// refer to the same runtime directory.
func normalizeRuntimePaths(paths []string) []string {
	result := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		path, err := workspace.CanonicalPath(raw)
		if err != nil {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	return result
}

func defaultRuntimePaths() []string {
	return firstInstalledVimRuntimePaths(vimInstallCandidates(runtime.GOOS))
}

// vimInstallCandidates returns conventional installation locations in
// precedence order. Discovery uses only the first candidate containing a Vim
// runtime; paths from different installations are never combined.
func vimInstallCandidates(goos string) []string {
	switch goos {
	case "windows":
		roots := make([]string, 0, 3)
		for _, variable := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
			if value := strings.TrimSpace(os.Getenv(variable)); value != "" {
				roots = append(roots, filepath.Join(value, "Vim"))
			}
		}
		if drive := strings.TrimSpace(os.Getenv("SystemDrive")); drive != "" {
			roots = append(roots, filepath.Join(drive+string(filepath.Separator), "Vim"))
		}
		return roots
	case "darwin":
		return []string{
			"/usr/local/share/vim",
			"/opt/homebrew/share/vim",
			"/usr/share/vim",
			"/Applications/MacVim.app/Contents/Resources/vim/runtime",
		}
	default:
		return []string{"/usr/local/share/vim", "/usr/share/vim"}
	}
}

func firstInstalledVimRuntimePaths(candidates []string) []string {
	for _, installRoot := range candidates {
		canonicalRoot, err := workspace.CanonicalPath(installRoot)
		if err != nil {
			continue
		}
		installRoot = canonicalRoot
		if isInstalledVimRuntime(installRoot) {
			return []string{installRoot}
		}
		entries, err := os.ReadDir(installRoot)
		if err != nil {
			// Default discovery is best-effort. Missing and unreadable install
			// locations must not make language-server initialization fail.
			continue
		}
		type versionDirectory struct {
			version int
			path    string
		}
		versions := make([]versionDirectory, 0)
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "vim") {
				continue
			}
			version, err := strconv.Atoi(strings.TrimPrefix(entry.Name(), "vim"))
			if err != nil || version < 1 {
				continue
			}
			path := filepath.Join(installRoot, entry.Name())
			if isInstalledVimRuntime(path) {
				versions = append(versions, versionDirectory{version: version, path: path})
			}
		}
		if len(versions) == 0 {
			continue
		}
		sort.Slice(versions, func(left, right int) bool { return versions[left].version > versions[right].version })
		paths := make([]string, 0, 3)
		vimfiles := filepath.Join(installRoot, "vimfiles")
		if isDirectory(vimfiles) {
			paths = append(paths, vimfiles)
		}
		paths = append(paths, versions[0].path)
		if after := filepath.Join(vimfiles, "after"); isDirectory(after) {
			paths = append(paths, after)
		}
		return normalizeRuntimePaths(paths)
	}
	return nil
}

func isInstalledVimRuntime(path string) bool {
	return isDirectory(path) && isDirectory(filepath.Join(path, "doc")) && isDirectory(filepath.Join(path, "syntax"))
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func targetVersionFromSettings(raw []byte, previous TargetVersion) (TargetVersion, string) {
	if len(raw) == 0 || string(raw) == "null" {
		return previous, ""
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return previous, "vimls: workspace settings must be an object; retaining target " + previous.String()
	}
	value, exists := settings["targetVersion"]
	if nested, ok := settings["vimls"].(map[string]any); ok {
		if nestedValue, nestedExists := nested["targetVersion"]; nestedExists {
			value, exists = nestedValue, true
		}
	}
	if !exists {
		return previous, ""
	}
	text, ok := value.(string)
	if !ok {
		return previous, "vimls: targetVersion must be a string; retaining target " + previous.String()
	}
	version, err := ParseTargetVersion(text)
	if err != nil {
		return previous, fmt.Sprintf("vimls: %v; retaining target %s", err, previous.String())
	}
	return version, ""
}

func unresolvedSeverityFromSettings(raw []byte, previous syntax.DiagnosticSeverity) (syntax.DiagnosticSeverity, string) {
	if len(raw) == 0 || string(raw) == "null" {
		return previous, ""
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return previous, "vimls: workspace settings must be an object; retaining unresolvedSeverity"
	}
	value, exists := settings["unresolvedSeverity"]
	if nested, ok := settings["vimls"].(map[string]any); ok {
		if nestedValue, nestedExists := nested["unresolvedSeverity"]; nestedExists {
			value, exists = nestedValue, true
		}
	}
	if !exists {
		return previous, ""
	}
	text, ok := value.(string)
	if !ok {
		return previous, "vimls: unresolvedSeverity must be a string; retaining previous value"
	}
	severity, ok := parseDiagnosticSeverity(text)
	if !ok {
		return previous, "vimls: unresolvedSeverity must be error, warning, information, or hint; retaining previous value"
	}
	return severity, ""
}

func parseDiagnosticSeverity(value string) (syntax.DiagnosticSeverity, bool) {
	switch value {
	case "error":
		return syntax.DiagnosticError, true
	case "warning":
		return syntax.DiagnosticWarning, true
	case "information":
		return syntax.DiagnosticInformation, true
	case "hint":
		return syntax.DiagnosticHint, true
	default:
		return 0, false
	}
}
