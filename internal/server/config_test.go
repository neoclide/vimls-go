package server

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.lsp.dev/protocol"
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
	paths, configured, warning := runtimepathFromOptions([]byte(`{"runtimepath":["` + second + `","` + first + `","` + second + `"]}`))
	secondReal, _ := filepath.EvalSymlinks(second)
	firstReal, _ := filepath.EvalSymlinks(first)
	if warning != "" || !configured || len(paths) != 2 || paths[0] != secondReal || paths[1] != firstReal {
		t.Fatalf("runtimepath = %#v, configured = %v, warning = %q", paths, configured, warning)
	}
	for _, raw := range []any{
		[]byte(`{"runtimepath":"` + first + `"}`),
		[]byte(`{"runtimepath":["` + first + `",1]}`),
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
		{name: "direct", settings: `{"workspaceRebuildDebounce":50}`, want: 50 * time.Millisecond},
		{name: "nested", settings: `{"vim":{"workspaceRebuildDebounce":0}}`, want: 0},
		{name: "invalid", settings: `{"workspaceRebuildDebounce":-1}`, want: previous, wantWarning: true},
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
		{name: "missing suggest", settings: `{"workspaceRebuildDebounce":100}`},
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
		`{"disabledDiagnostics":["vim/E117","vimls/deprecated","future/code"],"overrideDiagnostics":{"vim/E121":"warning","vimls/deprecated":"information"}}`,
		`{"vim":{"disabledDiagnostics":["vim/E117","vimls/deprecated","future/code"],"overrideDiagnostics":{"vim/E121":"warning","vimls/deprecated":"information"}}}`,
	} {
		disabled, overrides, warning := diagnosticSettingsFromSettings([]byte(raw), previousDisabled, previousOverrides)
		if warning != "" || len(disabled) != 3 || len(overrides) != 2 || overrides["vim/E121"] != protocol.DiagnosticSeverityWarning || overrides["vimls/deprecated"] != protocol.DiagnosticSeverityInformation {
			t.Fatalf("settings %s = disabled=%#v overrides=%#v warning=%q", raw, disabled, overrides, warning)
		}
	}
	disabled, overrides, warning := diagnosticSettingsFromSettings([]byte(`{}`), previousDisabled, previousOverrides)
	if warning != "" || len(disabled) != 0 || len(overrides) != 0 {
		t.Fatalf("missing fields did not reset: disabled=%#v overrides=%#v warning=%q", disabled, overrides, warning)
	}
	disabled, overrides, warning = diagnosticSettingsFromSettings([]byte(`{"disabledDiagnostics":["vim/E117",1],"overrideDiagnostics":{"vim/E121":"off"}}`), previousDisabled, previousOverrides)
	if warning == "" || len(disabled) != 1 || len(overrides) != 1 || overrides["vim/E121"] != protocol.DiagnosticSeverityHint {
		t.Fatalf("invalid fields did not retain independently: disabled=%#v overrides=%#v warning=%q", disabled, overrides, warning)
	}
	disabled, overrides, warning = diagnosticSettingsFromSettings([]byte(`{"disabledDiagnostics":[]}`), previousDisabled, previousOverrides)
	if warning != "" || len(disabled) != 0 || len(overrides) != 0 {
		t.Fatalf("empty settings = disabled=%#v overrides=%#v warning=%q", disabled, overrides, warning)
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
	if err := instance.applyWorkspaceConfiguration(context.Background(), []byte(`{"vim":{"disabledDiagnostics":["vim/E117"],"overrideDiagnostics":{"vim/E121":"hint"}}}`)); err != nil {
		t.Fatal(err)
	}
	if err := instance.applyWorkspaceConfiguration(context.Background(), []byte(`{"vim":{"disabledDiagnostics":[1],"overrideDiagnostics":{"vim/E121":"information"}}}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case warning := <-client.warnings:
		if !strings.Contains(warning, "disabledDiagnostics") {
			t.Fatalf("warning = %q", warning)
		}
	case <-time.After(time.Second):
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
	if err := instance.applyWorkspaceConfiguration(context.Background(), []byte(`{"vim":{"disabledDiagnostics":["vim/E117"],"overrideDiagnostics":{"vim/E121":"information"}}}`)); err != nil {
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
		InitializationOptions: protocol.LSPAny([]byte(`{"disabledDiagnostics":["vim/E117"],"overrideDiagnostics":{"vim/E121":"hint"}}`)),
	}); err != nil {
		t.Fatal(err)
	}
	instance.mu.Lock()
	defer instance.mu.Unlock()
	if len(instance.disabledDiagnostics) != 0 || len(instance.overrideDiagnostics) != 0 {
		t.Fatalf("initializationOptions changed diagnostic settings: disabled=%#v overrides=%#v", instance.disabledDiagnostics, instance.overrideDiagnostics)
	}
}
