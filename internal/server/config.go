package server

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
)

const completionBudget = 100 * time.Millisecond

type completionCapabilities struct {
	snippet       bool
	insertReplace bool
	preselect     bool
	tags          bool
	docsMarkdown  bool
}

type languageFeatureCapabilities struct {
	hoverMarkup                  protocol.MarkupKind
	signatureMarkup              protocol.MarkupKind
	diagnosticRelatedInformation bool
}

func languageFeatureCapabilitiesFromClient(textDocument *protocol.TextDocumentClientCapabilities) languageFeatureCapabilities {
	result := languageFeatureCapabilities{hoverMarkup: protocol.MarkupKindPlainText, signatureMarkup: protocol.MarkupKindPlainText}
	if textDocument == nil {
		return result
	}
	if textDocument.Hover != nil {
		result.hoverMarkup = preferredMarkupKind(textDocument.Hover.ContentFormat)
	}
	if textDocument.SignatureHelp != nil && textDocument.SignatureHelp.SignatureInformation != nil {
		result.signatureMarkup = preferredMarkupKind(textDocument.SignatureHelp.SignatureInformation.DocumentationFormat)
	}
	if textDocument.PublishDiagnostics != nil && textDocument.PublishDiagnostics.RelatedInformation != nil {
		result.diagnosticRelatedInformation = *textDocument.PublishDiagnostics.RelatedInformation
	}
	return result
}

func languageFeatureCapabilitiesFromDiagnostic(diagnostic *protocol.DiagnosticClientCapabilities, result languageFeatureCapabilities) languageFeatureCapabilities {
	result.diagnosticRelatedInformation = diagnostic != nil && diagnostic.RelatedInformation != nil && *diagnostic.RelatedInformation
	return result
}

func preferredMarkupKind(formats []protocol.MarkupKind) protocol.MarkupKind {
	for _, format := range formats {
		if format == protocol.MarkupKindMarkdown || format == protocol.MarkupKindPlainText {
			return format
		}
	}
	return protocol.MarkupKindPlainText
}

func completionCapabilitiesFromClient(textDocument *protocol.TextDocumentClientCapabilities) completionCapabilities {
	if textDocument == nil || textDocument.Completion == nil || textDocument.Completion.CompletionItem == nil {
		return completionCapabilities{}
	}
	item := textDocument.Completion.CompletionItem
	return completionCapabilities{
		snippet:       item.SnippetSupport != nil && *item.SnippetSupport,
		insertReplace: item.InsertReplaceSupport != nil && *item.InsertReplaceSupport,
		preselect:     item.PreselectSupport != nil && *item.PreselectSupport,
		tags:          len(item.TagSupport.ValueSet) > 0,
		docsMarkdown:  slices.Contains(item.DocumentationFormat, protocol.MarkupKindMarkdown),
	}
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

// workspaceSettingsObject decodes a workspace-settings payload. Clients either
// push the bare vimls settings object through workspace/didChangeConfiguration
// or wrap them in a "vim" section inside their full configuration. When the
// "vim" section is present it is the effective vimls settings object; a null
// or missing "vim" section keeps the top level. Empty, null, and missing
// payloads decode to an empty object without a warning, so an unset setting is
// never an error.
func workspaceSettingsObject(raw []byte, retain string) (map[string]json.RawMessage, string) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return map[string]json.RawMessage{}, ""
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(raw, &settings); err != nil || settings == nil {
		return nil, "vimls: workspace settings must be an object; retaining " + retain
	}
	if rawVim, exists := settings["vim"]; exists {
		if string(rawVim) == "null" {
			return settings, ""
		}
		var vimSettings map[string]json.RawMessage
		if err := json.Unmarshal(rawVim, &vimSettings); err != nil || vimSettings == nil {
			return nil, "vimls: vim workspace settings must be an object; retaining " + retain
		}
		return vimSettings, ""
	}
	return settings, ""
}

func workspaceRebuildDebounceFromSettings(raw []byte, previous time.Duration) (time.Duration, string) {
	settings, warning := workspaceSettingsObject(raw, "workspace.rebuildDebounce")
	if warning != "" {
		return previous, warning
	}
	rawWorkspace, exists := settings["workspace"]
	if !exists || string(rawWorkspace) == "null" {
		return previous, ""
	}
	var workspaceSettings map[string]json.RawMessage
	if err := json.Unmarshal(rawWorkspace, &workspaceSettings); err != nil || workspaceSettings == nil {
		return previous, "vimls: workspace settings must be an object; retaining workspace.rebuildDebounce"
	}
	value, exists := workspaceSettings["rebuildDebounce"]
	if !exists || string(value) == "null" {
		return previous, ""
	}
	delay, ok := workspaceRebuildDebounce(value)
	if !ok {
		return previous, "vimls: workspace.rebuildDebounce must be a non-negative integer in milliseconds; retaining previous value"
	}
	return delay, ""
}

func workspaceRebuildDebounce(value json.RawMessage) (time.Duration, bool) {
	var milliseconds float64
	if err := json.Unmarshal(value, &milliseconds); err != nil {
		return 0, false
	}
	if milliseconds < 0 || milliseconds > float64((1<<63-1)/int64(time.Millisecond)) || milliseconds != float64(int64(milliseconds)) {
		return 0, false
	}
	return time.Duration(milliseconds) * time.Millisecond, true
}

func diagnosticSettingsFromSettings(raw []byte, previousDisabled map[string]struct{}, previousOverrides map[string]protocol.DiagnosticSeverity) (map[string]struct{}, map[string]protocol.DiagnosticSeverity, string) {
	settings, warning := workspaceSettingsObject(raw, "previous diagnostic settings")
	if warning != "" {
		return previousDisabled, previousOverrides, warning
	}
	disabled := make(map[string]struct{})
	overrides := make(map[string]protocol.DiagnosticSeverity)
	rawDiagnostic, exists := settings["diagnostic"]
	if !exists || string(rawDiagnostic) == "null" {
		return disabled, overrides, ""
	}
	var diagnosticSettings map[string]json.RawMessage
	if err := json.Unmarshal(rawDiagnostic, &diagnosticSettings); err != nil || diagnosticSettings == nil {
		return previousDisabled, previousOverrides, "vimls: diagnostic workspace settings must be an object; retaining previous diagnostic settings"
	}
	warnings := make([]string, 0, 2)
	if value, exists := diagnosticSettings["disabled"]; exists && string(value) != "null" {
		var values []json.RawMessage
		if err := json.Unmarshal(value, &values); err != nil || values == nil {
			warnings = append(warnings, "vimls: diagnostic.disabled must be an array of non-empty strings; retaining previous value")
			disabled = maps.Clone(previousDisabled)
		} else {
			valid := true
			for _, item := range values {
				var code string
				if json.Unmarshal(item, &code) != nil || code == "" {
					valid = false
					break
				}
				disabled[code] = struct{}{}
			}
			if !valid {
				warnings = append(warnings, "vimls: diagnostic.disabled must be an array of non-empty strings; retaining previous value")
				disabled = maps.Clone(previousDisabled)
			}
		}
	}
	if value, exists := diagnosticSettings["override"]; exists && string(value) != "null" {
		var values map[string]json.RawMessage
		if err := json.Unmarshal(value, &values); err != nil || values == nil {
			warnings = append(warnings, "vimls: diagnostic.override must be an object of diagnostic codes to severity strings; retaining previous value")
			overrides = maps.Clone(previousOverrides)
		} else {
			valid := true
			for code, item := range values {
				if code == "" {
					valid = false
					break
				}
				var severity string
				if json.Unmarshal(item, &severity) != nil {
					valid = false
					break
				}
				switch severity {
				case "error":
					overrides[code] = protocol.DiagnosticSeverityError
				case "warning":
					overrides[code] = protocol.DiagnosticSeverityWarning
				case "information":
					overrides[code] = protocol.DiagnosticSeverityInformation
				case "hint":
					overrides[code] = protocol.DiagnosticSeverityHint
				default:
					valid = false
				}
				if !valid {
					break
				}
			}
			if !valid {
				warnings = append(warnings, "vimls: diagnostic.override severity must be error, warning, information, or hint; retaining previous value")
				overrides = maps.Clone(previousOverrides)
			}
		}
	}
	return disabled, overrides, strings.Join(warnings, "; ")
}

func excludeRuntimePathFromSettings(raw []byte, previous bool) (bool, string) {
	settings, warning := workspaceSettingsObject(raw, "previous excludeRuntimePath")
	if warning != "" {
		return previous, warning
	}
	rawSuggest, exists := settings["suggest"]
	if !exists || string(rawSuggest) == "null" {
		return false, ""
	}
	var suggestSettings map[string]json.RawMessage
	if err := json.Unmarshal(rawSuggest, &suggestSettings); err != nil || suggestSettings == nil {
		return previous, "vimls: suggest workspace settings must be an object; retaining previous excludeRuntimePath"
	}
	value, exists := suggestSettings["excludeRuntimePath"]
	if !exists || string(value) == "null" {
		return false, ""
	}
	var exclude bool
	if err := json.Unmarshal(value, &exclude); err != nil {
		return previous, "vimls: suggest.excludeRuntimePath must be a boolean; retaining previous value"
	}
	return exclude, ""
}

func configFilesFromOptions(raw any) ([]string, bool, string) {
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
			return nil, false, "vimls: initializationOptions must be an object; ignoring configFiles"
		}
	default:
		return nil, false, "vimls: initializationOptions must be an object; ignoring configFiles"
	}
	rawFiles, exists := options["configFiles"]
	if !exists || rawFiles == nil {
		return nil, false, ""
	}
	var patterns []string
	switch values := rawFiles.(type) {
	case []string:
		patterns = append(patterns, values...)
	case []any:
		patterns = make([]string, 0, len(values))
		for _, rawPattern := range values {
			pattern, ok := rawPattern.(string)
			if !ok {
				return nil, true, "vimls: configFiles must be an array of strings; ignoring configFiles"
			}
			patterns = append(patterns, pattern)
		}
	default:
		return nil, true, "vimls: configFiles must be an array of strings; ignoring configFiles"
	}
	return cleanConfigFilePatterns(patterns), true, ""
}

func cleanConfigFilePatterns(patterns []string) []string {
	result := make([]string, 0, len(patterns))
	for _, p := range patterns {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
