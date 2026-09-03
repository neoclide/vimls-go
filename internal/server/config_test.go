package server

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestCompletionCapabilitiesFromClient(t *testing.T) {
	enabled := true
	capabilities := completionCapabilitiesFromClient(&protocol.TextDocumentClientCapabilities{Completion: &protocol.CompletionClientCapabilities{CompletionItem: &protocol.ClientCompletionItemOptions{
		SnippetSupport:       &enabled,
		InsertReplaceSupport: &enabled,
		PreselectSupport:     &enabled,
		TagSupport:           protocol.CompletionItemTagOptions{ValueSet: []protocol.CompletionItemTag{protocol.CompletionItemTagDeprecated}},
		DocumentationFormat:  []protocol.MarkupKind{protocol.MarkupKindPlainText, protocol.MarkupKindMarkdown},
	}}})
	if !capabilities.snippet || !capabilities.insertReplace || !capabilities.preselect || !capabilities.tags || !capabilities.docsMarkdown {
		t.Fatalf("completion capabilities = %#v", capabilities)
	}
	if capabilities := completionCapabilitiesFromClient(nil); capabilities != (completionCapabilities{}) {
		t.Fatalf("absent completion capabilities = %#v", capabilities)
	}
}

func TestLanguageFeatureCapabilitiesRespectClientOrder(t *testing.T) {
	enabled := true
	capabilities := languageFeatureCapabilitiesFromClient(&protocol.TextDocumentClientCapabilities{
		Hover: &protocol.HoverClientCapabilities{ContentFormat: []protocol.MarkupKind{protocol.MarkupKindPlainText, protocol.MarkupKindMarkdown}},
		SignatureHelp: &protocol.SignatureHelpClientCapabilities{SignatureInformation: &protocol.ClientSignatureInformationOptions{
			DocumentationFormat: []protocol.MarkupKind{protocol.MarkupKindMarkdown, protocol.MarkupKindPlainText},
		}},
		PublishDiagnostics: &protocol.PublishDiagnosticsClientCapabilities{DiagnosticsCapabilities: protocol.DiagnosticsCapabilities{RelatedInformation: &enabled}},
	})
	if capabilities.hoverMarkup != protocol.MarkupKindPlainText || capabilities.signatureMarkup != protocol.MarkupKindMarkdown || !capabilities.diagnosticRelatedInformation {
		t.Fatalf("language feature capabilities = %#v", capabilities)
	}
	if capabilities := languageFeatureCapabilitiesFromClient(nil); capabilities.hoverMarkup != protocol.MarkupKindPlainText || capabilities.signatureMarkup != protocol.MarkupKindPlainText || capabilities.diagnosticRelatedInformation {
		t.Fatalf("absent language feature capabilities = %#v", capabilities)
	}
}

func TestPullDiagnosticRelatedInformationDoesNotInheritPushCapability(t *testing.T) {
	enabled := true
	capabilities := languageFeatureCapabilitiesFromClient(&protocol.TextDocumentClientCapabilities{
		PublishDiagnostics: &protocol.PublishDiagnosticsClientCapabilities{DiagnosticsCapabilities: protocol.DiagnosticsCapabilities{RelatedInformation: &enabled}},
	})
	capabilities = languageFeatureCapabilitiesFromDiagnostic(&protocol.DiagnosticClientCapabilities{}, capabilities)
	if capabilities.diagnosticRelatedInformation {
		t.Fatal("pull diagnostics inherited push-only related information support")
	}
	capabilities = languageFeatureCapabilitiesFromDiagnostic(&protocol.DiagnosticClientCapabilities{DiagnosticsCapabilities: protocol.DiagnosticsCapabilities{RelatedInformation: &enabled}}, capabilities)
	if !capabilities.diagnosticRelatedInformation {
		t.Fatal("pull diagnostics ignored related information support")
	}
}

func TestRuntimepathFromOptions(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	options, err := json.Marshal(map[string]any{"runtimepath": []string{second, first, second}})
	if err != nil {
		t.Fatal(err)
	}
	paths, configured, warning := runtimepathFromOptions(options)
	secondReal, _ := filepath.EvalSymlinks(second)
	firstReal, _ := filepath.EvalSymlinks(first)
	if warning != "" || !configured || len(paths) != 2 || paths[0] != secondReal || paths[1] != firstReal {
		t.Fatalf("runtimepath = %#v, configured = %v, warning = %q", paths, configured, warning)
	}
	invalidString, err := json.Marshal(map[string]any{"runtimepath": first})
	if err != nil {
		t.Fatal(err)
	}
	invalidElement, err := json.Marshal(map[string]any{"runtimepath": []any{first, 1}})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []any{
		invalidString,
		invalidElement,
		[]byte(`[]`),
	} {
		if paths, _, warning := runtimepathFromOptions(raw); len(paths) != 0 || warning == "" {
			t.Fatalf("invalid runtimepath %s = %#v, warning = %q", raw, paths, warning)
		}
	}
	if paths, configured, warning := runtimepathFromOptions([]byte(`{"runtimepath":[]}`)); len(paths) != 0 || !configured || warning != "" {
		t.Fatalf("empty configured runtimepath = %#v, %v, %q", paths, configured, warning)
	}
	if paths, configured, warning := runtimepathFromOptions([]byte(`{}`)); len(paths) != 0 || configured || warning != "" {
		t.Fatalf("absent runtimepath = %#v, %v, %q", paths, configured, warning)
	}
}

func TestDefaultRuntimePathsUseOneInstallationAndItsNewestVersion(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	for _, path := range []string{
		filepath.Join(first, "vimfiles", "after"),
		filepath.Join(first, "vim91", "doc"), filepath.Join(first, "vim91", "syntax"),
		filepath.Join(first, "vim92", "doc"), filepath.Join(first, "vim92", "syntax"),
		filepath.Join(second, "vim99", "doc"), filepath.Join(second, "vim99", "syntax"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	firstReal, _ := filepath.EvalSymlinks(first)
	want := []string{
		filepath.Join(firstReal, "vimfiles"),
		filepath.Join(firstReal, "vim92"),
		filepath.Join(firstReal, "vimfiles", "after"),
	}
	if got := firstInstalledVimRuntimePaths([]string{filepath.Join(first, "missing"), first, second}); !reflect.DeepEqual(got, want) {
		t.Fatalf("default runtimepath = %#v, want %#v", got, want)
	}
	direct := filepath.Join(t.TempDir(), "runtime")
	for _, name := range []string{"doc", "syntax"} {
		if err := os.MkdirAll(filepath.Join(direct, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	directReal, _ := filepath.EvalSymlinks(direct)
	if got := firstInstalledVimRuntimePaths([]string{direct}); !reflect.DeepEqual(got, []string{directReal}) {
		t.Fatalf("direct runtimepath = %#v", got)
	}
}

func TestVimInstallCandidatesCoverPlatformConventions(t *testing.T) {
	t.Setenv("ProgramFiles", `C:\\Program Files`)
	t.Setenv("ProgramFiles(x86)", `C:\\Program Files (x86)`)
	t.Setenv("SystemDrive", "D:")
	windows := vimInstallCandidates("windows")
	if len(windows) != 3 || !strings.Contains(windows[0], "Program Files") || !strings.Contains(windows[2], "Vim") {
		t.Fatalf("windows candidates = %#v", windows)
	}
	if got := vimInstallCandidates("darwin"); len(got) != 4 {
		t.Fatalf("darwin candidates = %#v", got)
	}
	if got := vimInstallCandidates("linux"); len(got) != 2 {
		t.Fatalf("unix candidates = %#v", got)
	}
}

func TestDefaultRuntimePathsSkipUnreadableCandidate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix directory permissions are not portable to Windows")
	}
	unreadable := t.TempDir()
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o700) })

	installed := t.TempDir()
	for _, name := range []string{"doc", "syntax"} {
		if err := os.MkdirAll(filepath.Join(installed, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	installedReal, _ := filepath.EvalSymlinks(installed)
	if got := firstInstalledVimRuntimePaths([]string{unreadable, installed}); !reflect.DeepEqual(got, []string{installedReal}) {
		t.Fatalf("default runtimepath = %#v, want only %#v", got, installed)
	}
}

func TestWorkspaceRebuildDebounceFromSettings(t *testing.T) {
	previous := 250 * time.Millisecond
	tests := []struct {
		name        string
		settings    string
		want        time.Duration
		wantWarning bool
	}{
		{name: "empty", settings: `{}`, want: previous},
		{name: "direct", settings: `{"workspace":{"rebuildDebounce":50}}`, want: 50 * time.Millisecond},
		{name: "nested", settings: `{"vim":{"workspace":{"rebuildDebounce":0}}}`, want: 0},
		{name: "invalid", settings: `{"workspace":{"rebuildDebounce":-1}}`, want: previous, wantWarning: true},
		{name: "missing section", settings: `{"diagnostic":{"disabled":[]}}`, want: previous},
		{name: "null section", settings: `{"workspace":null}`, want: previous},
		{name: "missing value", settings: `{"workspace":{}}`, want: previous},
		{name: "invalid section", settings: `{"workspace":[]}`, want: previous, wantWarning: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			delay, warning := workspaceRebuildDebounceFromSettings([]byte(test.settings), previous)
			if delay != test.want || (warning != "") != test.wantWarning {
				t.Fatalf("delay=%s warning=%q", delay, warning)
			}
		})
	}
}

func TestExcludeRuntimePathFromSettings(t *testing.T) {
	previous := true
	tests := []struct {
		name, settings string
		want           bool
		warning        bool
	}{
		{name: "direct", settings: `{"suggest":{"excludeRuntimePath":true}}`, want: true},
		{name: "nested", settings: `{"vim":{"suggest":{"excludeRuntimePath":true}}}`, want: true},
		{name: "false", settings: `{"suggest":{"excludeRuntimePath":false}}`},
		{name: "empty", settings: `{}`},
		{name: "null", settings: `null`},
		{name: "missing suggest", settings: `{"workspace":{"rebuildDebounce":100}}`},
		{name: "null suggest", settings: `{"suggest":null}`},
		{name: "missing value", settings: `{"suggest":{}}`},
		{name: "invalid value", settings: `{"suggest":{"excludeRuntimePath":"yes"}}`, want: previous, warning: true},
		{name: "invalid suggest", settings: `{"suggest":[]}`, want: previous, warning: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, warning := excludeRuntimePathFromSettings([]byte(test.settings), previous)
			if got != test.want || (warning != "") != test.warning {
				t.Fatalf("setting = %t, warning = %q", got, warning)
			}
		})
	}
}

func TestDiagnosticSettingsFromSettings(t *testing.T) {
	previousDisabled := map[string]struct{}{"vim/E117": {}}
	previousOverrides := map[string]protocol.DiagnosticSeverity{"vim/E121": protocol.DiagnosticSeverityHint}
	for _, raw := range []string{
		`{"diagnostic":{"disabled":["vim/E117","vimls/deprecated","future/code"],"override":{"vim/E121":"warning","vimls/deprecated":"information"}}}`,
		`{"vim":{"diagnostic":{"disabled":["vim/E117","vimls/deprecated","future/code"],"override":{"vim/E121":"warning","vimls/deprecated":"information"}}}}`,
	} {
		disabled, overrides, warning := diagnosticSettingsFromSettings([]byte(raw), previousDisabled, previousOverrides)
		if warning != "" || len(disabled) != 3 || len(overrides) != 2 || overrides["vim/E121"] != protocol.DiagnosticSeverityWarning || overrides["vimls/deprecated"] != protocol.DiagnosticSeverityInformation {
			t.Fatalf("settings %s = disabled=%#v overrides=%#v warning=%q", raw, disabled, overrides, warning)
		}
	}
	for _, raw := range []string{
		`{}`,
		`{"workspace":{"rebuildDebounce":100}}`,
		`{"diagnostic":null}`,
		`{"vim":null}`,
	} {
		disabled, overrides, warning := diagnosticSettingsFromSettings([]byte(raw), previousDisabled, previousOverrides)
		if warning != "" || len(disabled) != 0 || len(overrides) != 0 {
			t.Fatalf("missing diagnostic settings %s did not reset silently: disabled=%#v overrides=%#v warning=%q", raw, disabled, overrides, warning)
		}
	}
	disabled, overrides, warning := diagnosticSettingsFromSettings([]byte(`{"diagnostic":{"disabled":["vim/E117",1],"override":{"vim/E121":"off"}}}`), previousDisabled, previousOverrides)
	if warning == "" || len(disabled) != 1 || len(overrides) != 1 || overrides["vim/E121"] != protocol.DiagnosticSeverityHint {
		t.Fatalf("invalid fields did not retain independently: disabled=%#v overrides=%#v warning=%q", disabled, overrides, warning)
	}
	disabled, overrides, warning = diagnosticSettingsFromSettings([]byte(`{"diagnostic":{"disabled":[]}}`), previousDisabled, previousOverrides)
	if warning != "" || len(disabled) != 0 || len(overrides) != 0 {
		t.Fatalf("empty settings = disabled=%#v overrides=%#v warning=%q", disabled, overrides, warning)
	}
	disabled, overrides, warning = diagnosticSettingsFromSettings([]byte(`{"diagnostic":{"disabled":null,"override":null}}`), previousDisabled, previousOverrides)
	if warning != "" || len(disabled) != 0 || len(overrides) != 0 {
		t.Fatalf("null diagnostic fields = disabled=%#v overrides=%#v warning=%q", disabled, overrides, warning)
	}
	disabled, overrides, warning = diagnosticSettingsFromSettings([]byte(`{"diagnostic":[]}`), previousDisabled, previousOverrides)
	if warning == "" || len(disabled) != 1 || len(overrides) != 1 || disabled["vim/E117"] != struct{}{} || overrides["vim/E121"] != protocol.DiagnosticSeverityHint {
		t.Fatalf("invalid diagnostic section did not retain: disabled=%#v overrides=%#v warning=%q", disabled, overrides, warning)
	}
}

type diagnosticConfigWarningClient struct {
	protocol.UnimplementedClient
	warnings chan string
}

func (c *diagnosticConfigWarningClient) LogMessage(_ context.Context, params *protocol.LogMessageParams) error {
	c.warnings <- params.Message
	return nil
}

func TestApplyDiagnosticSettingsRetainsMalformedFieldAndUpdatesOtherField(t *testing.T) {
	client := &diagnosticConfigWarningClient{warnings: make(chan string, 1)}
	instance := New(nil, nil, nil)
	t.Cleanup(instance.stopAnalysis)
	instance.client = client
	if err := instance.applyWorkspaceConfiguration(context.Background(), []byte(`{"vim":{"diagnostic":{"disabled":["vim/E117"],"override":{"vim/E121":"hint"}}}}`)); err != nil {
		t.Fatal(err)
	}
	if err := instance.applyWorkspaceConfiguration(context.Background(), []byte(`{"vim":{"diagnostic":{"disabled":[1],"override":{"vim/E121":"information"}}}}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case warning := <-client.warnings:
		if !strings.Contains(warning, "diagnostic.disabled") {
			t.Fatalf("warning = %q", warning)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for configuration warning")
	}
	instance.mu.Lock()
	disabled := maps.Clone(instance.disabledDiagnostics)
	overrides := maps.Clone(instance.overrideDiagnostics)
	instance.mu.Unlock()
	if len(disabled) != 1 || len(overrides) != 1 || overrides["vim/E121"] != protocol.DiagnosticSeverityInformation {
		t.Fatalf("configuration state = disabled=%#v overrides=%#v", disabled, overrides)
	}
	revision := instance.documents.ConfigRevision()
	if err := instance.applyWorkspaceConfiguration(context.Background(), []byte(`{"vim":{"diagnostic":{"disabled":["vim/E117"],"override":{"vim/E121":"information"}}}}`)); err != nil {
		t.Fatal(err)
	}
	if got := instance.documents.ConfigRevision(); got != revision {
		t.Fatalf("unchanged diagnostics changed config revision from %d to %d", revision, got)
	}
}

func TestInitializeDoesNotReadDiagnosticInitializationOptions(t *testing.T) {
	instance := New(nil, nil, nil)
	t.Cleanup(instance.stopAnalysis)
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		InitializationOptions: protocol.LSPAny([]byte(`{"diagnostic":{"disabled":["vim/E117"],"override":{"vim/E121":"hint"}}}`)),
	}); err != nil {
		t.Fatal(err)
	}
	instance.mu.Lock()
	defer instance.mu.Unlock()
	if len(instance.disabledDiagnostics) != 0 || len(instance.overrideDiagnostics) != 0 {
		t.Fatalf("initializationOptions changed diagnostic settings: disabled=%#v overrides=%#v", instance.disabledDiagnostics, instance.overrideDiagnostics)
	}
}

func TestConfigFilesFromOptions(t *testing.T) {
	files, configured, warning := configFilesFromOptions([]byte(`{"configFiles":["rc/*.vim","~/.vimrc","  "]}`))
	if !configured || warning != "" || !reflect.DeepEqual(files, []string{"rc/*.vim", "~/.vimrc"}) {
		t.Fatalf("configFilesFromOptions valid = %#v configured=%v warning=%q", files, configured, warning)
	}
	files, configured, warning = configFilesFromOptions([]byte(`{"configFiles":123}`))
	if !configured || !strings.Contains(warning, "array of strings") || len(files) != 0 {
		t.Fatalf("configFilesFromOptions invalid type = %#v configured=%v warning=%q", files, configured, warning)
	}
	files, configured, warning = configFilesFromOptions(nil)
	if configured || warning != "" || len(files) != 0 {
		t.Fatalf("configFilesFromOptions nil = %#v configured=%v warning=%q", files, configured, warning)
	}
}

func TestServerIsConfigFile(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rootURI := uri.File(root)
	instance := New(nil, nil, nil)
	t.Cleanup(instance.stopAnalysis)
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI: &rootURI,
	}); err != nil {
		t.Fatal(err)
	}

	vimrcPath := filepath.Join(root, ".vimrc")
	pluginPath := filepath.Join(root, "plugin", "foo.vim")
	rcPath := filepath.Join(root, "rc", "bar.vim")

	if !instance.IsConfigFile(vimrcPath) {
		t.Fatalf("expected %s to be config file", vimrcPath)
	}
	if instance.IsConfigFile(pluginPath) {
		t.Fatalf("expected %s NOT to be config file by default", pluginPath)
	}
	if !instance.IsConfigFile(rcPath) {
		t.Fatalf("expected %s to be config file", rcPath)
	}

	// Instance with relative configFiles pattern - must be ignored
	relativeInstance := New(nil, nil, nil)
	t.Cleanup(relativeInstance.stopAnalysis)
	if _, err := relativeInstance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI:               &rootURI,
		InitializationOptions: protocol.LSPAny([]byte(`{"configFiles":["plugin/*.vim"]}`)),
	}); err != nil {
		t.Fatal(err)
	}
	if relativeInstance.IsConfigFile(pluginPath) {
		t.Fatalf("expected relative configFiles pattern to be ignored for %s", pluginPath)
	}

	// Instance with absolute configFiles configured in InitializationOptions
	configuredInstance := New(nil, nil, nil)
	t.Cleanup(configuredInstance.stopAnalysis)
	patternJSON, err := json.Marshal(map[string]any{
		"configFiles": []string{filepath.Join(root, "plugin", "*.vim")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configuredInstance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI:               &rootURI,
		InitializationOptions: protocol.LSPAny(patternJSON),
	}); err != nil {
		t.Fatal(err)
	}
	if !configuredInstance.IsConfigFile(pluginPath) {
		t.Fatalf("expected %s to be config file after initializationOptions", pluginPath)
	}

	// Instance with runtimePaths: runtime files default to non-config, ancestor path segments not misjudged
	runtimeDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rtpOptions, err := json.Marshal(map[string]any{
		"runtimepath": []string{runtimeDir},
	})
	if err != nil {
		t.Fatal(err)
	}
	rtpInstance := New(nil, nil, nil)
	t.Cleanup(rtpInstance.stopAnalysis)
	if _, err := rtpInstance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI:               &rootURI,
		InitializationOptions: protocol.LSPAny(rtpOptions),
	}); err != nil {
		t.Fatal(err)
	}
	defaultsPath := filepath.Join(runtimeDir, "defaults.vim")
	if rtpInstance.IsConfigFile(defaultsPath) {
		t.Fatalf("expected runtimepath file %s NOT to be config file", defaultsPath)
	}
	ancestorTmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ancestorPluginDir := filepath.Join(ancestorTmp, "plugin")
	ancestorFilePath := filepath.Join(ancestorPluginDir, "my_custom.vim")
	if !rtpInstance.IsConfigFile(ancestorFilePath) {
		t.Fatalf("expected file with ancestor named plugin outside roots %s to be config file", ancestorFilePath)
	}
}

func TestServerIsConfigFileSymlink(t *testing.T) {
	tempDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dotfiles := filepath.Join(tempDir, "dotfiles", "nvim")
	if err := os.MkdirAll(filepath.Join(dotfiles, "plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	realFile := filepath.Join(dotfiles, "plugin", "settings.vim")
	if err := os.WriteFile(realFile, []byte("echo 1"), 0o644); err != nil {
		t.Fatal(err)
	}

	configDir := filepath.Join(tempDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkNvim := filepath.Join(configDir, "nvim")
	if err := os.Symlink(dotfiles, symlinkNvim); err != nil {
		t.Skip("symlinks not supported")
	}
	symlinkFile := filepath.Join(symlinkNvim, "plugin", "settings.vim")

	server := New(nil, nil, nil)
	t.Cleanup(server.stopAnalysis)
	patternJSON, err := json.Marshal(map[string]any{
		"configFiles": []string{filepath.Join(symlinkNvim, "**", "*.vim")},
	})
	if err != nil {
		t.Fatal(err)
	}
	rootURI := uri.File(tempDir)
	if _, err := server.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI:               &rootURI,
		InitializationOptions: protocol.LSPAny(patternJSON),
	}); err != nil {
		t.Fatal(err)
	}

	if !server.IsConfigFile(symlinkFile) {
		t.Fatalf("expected symlinked file %s to match configFiles pattern", symlinkFile)
	}
}
