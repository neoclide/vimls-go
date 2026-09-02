package server

import (
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
