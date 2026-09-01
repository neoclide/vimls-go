package server

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
	"go.lsp.dev/protocol"
)

func TestCompletionCapabilitiesFromClient(t *testing.T) {
	enabled := true
	capabilities := completionCapabilitiesFromClient(&protocol.TextDocumentClientCapabilities{Completion: &protocol.CompletionClientCapabilities{CompletionItem: &protocol.ClientCompletionItemOptions{
		SnippetSupport:       &enabled,
		InsertReplaceSupport: &enabled,
		PreselectSupport:     &enabled,
		DeprecatedSupport:    &enabled,
		TagSupport:           protocol.CompletionItemTagOptions{ValueSet: []protocol.CompletionItemTag{protocol.CompletionItemTagDeprecated}},
		DocumentationFormat:  []protocol.MarkupKind{protocol.MarkupKindPlainText, protocol.MarkupKindMarkdown},
	}}})
	if !capabilities.snippet || !capabilities.insertReplace || !capabilities.preselect || !capabilities.deprecated || !capabilities.tags || !capabilities.docsMarkdown {
		t.Fatalf("completion capabilities = %#v", capabilities)
	}
	if capabilities := completionCapabilitiesFromClient(nil); capabilities != (completionCapabilities{}) {
		t.Fatalf("absent completion capabilities = %#v", capabilities)
	}
}

func TestParseTargetVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "9.1", want: "9.1.0000"},
		{input: "9.1.219", want: "9.1.0219"},
		{input: "9.2.1015", want: "9.2.1015"},
		{input: "latest", want: "latest"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			version, err := ParseTargetVersion(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got := version.String(); got != test.want {
				t.Fatalf("String() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestParseTargetVersionRejectsInvalidValues(t *testing.T) {
	for _, input := range []string{"", "9", "9.0", "8.2.9999", "9.1.", "9.x", "9.1.10000", "9.2.1016", "10.0", "v9.1", "LATEST"} {
		t.Run(input, func(t *testing.T) {
			_, err := ParseTargetVersion(input)
			if !errors.Is(err, ErrInvalidTargetVersion) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestTargetVersionFromOptions(t *testing.T) {
	tests := []struct {
		name        string
		raw         any
		want        string
		wantWarning bool
	}{
		{name: "absent", want: DefaultTargetVersion},
		{name: "null", raw: nil, want: DefaultTargetVersion},
		{name: "empty encoded options", raw: []byte(nil), want: DefaultTargetVersion},
		{name: "encoded null", raw: []byte("null"), want: DefaultTargetVersion},
		{name: "empty object", raw: map[string]any{}, want: DefaultTargetVersion},
		{name: "configured", raw: map[string]any{"targetVersion": "9.2.4"}, want: "9.2.0004"},
		{name: "latest", raw: map[string]any{"targetVersion": "latest"}, want: "latest"},
		{name: "invalid JSON shape", raw: []any{}, want: DefaultTargetVersion, wantWarning: true},
		{name: "invalid type", raw: map[string]any{"targetVersion": 9.1}, want: DefaultTargetVersion, wantWarning: true},
		{name: "invalid version", raw: map[string]any{"targetVersion": "9.0"}, want: DefaultTargetVersion, wantWarning: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			version, explicit, warning := targetVersionFromOptions(test.raw)
			if got := version.String(); got != test.want {
				t.Fatalf("version = %q, want %q", got, test.want)
			}
			if explicit != (test.name == "configured" || test.name == "latest") {
				t.Fatalf("explicit = %v", explicit)
			}
			if (warning != "") != test.wantWarning {
				t.Fatalf("warning = %q", warning)
			}
		})
	}
}

func TestUnresolvedSeverityFromOptions(t *testing.T) {
	tests := []struct {
		name        string
		raw         any
		want        syntax.DiagnosticSeverity
		wantWarning bool
	}{
		{name: "absent", want: syntax.DiagnosticWarning},
		{name: "empty object", raw: map[string]any{}, want: syntax.DiagnosticWarning},
		{name: "error", raw: map[string]any{"unresolvedSeverity": "error"}, want: syntax.DiagnosticError},
		{name: "warning", raw: map[string]any{"unresolvedSeverity": "warning"}, want: syntax.DiagnosticWarning},
		{name: "information", raw: map[string]any{"unresolvedSeverity": "information"}, want: syntax.DiagnosticInformation},
		{name: "hint", raw: []byte(`{"unresolvedSeverity":"hint"}`), want: syntax.DiagnosticHint},
		{name: "invalid shape", raw: []any{}, want: syntax.DiagnosticWarning, wantWarning: true},
		{name: "invalid type", raw: map[string]any{"unresolvedSeverity": 2}, want: syntax.DiagnosticWarning, wantWarning: true},
		{name: "invalid value", raw: map[string]any{"unresolvedSeverity": "off"}, want: syntax.DiagnosticWarning, wantWarning: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			severity, warning := unresolvedSeverityFromOptions(test.raw)
			if severity != test.want || (warning != "") != test.wantWarning {
				t.Fatalf("severity = %v, warning = %q, want %v, warning=%t", severity, warning, test.want, test.wantWarning)
			}
		})
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

func TestTargetVersionFromSettings(t *testing.T) {
	previous, err := ParseTargetVersion("9.1.1232")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		settings    string
		want        string
		wantWarning bool
	}{
		{name: "empty object", settings: `{}`, want: "9.1.1232"},
		{name: "direct", settings: `{"targetVersion":"9.2.4"}`, want: "9.2.0004"},
		{name: "nested", settings: `{"vimls":{"targetVersion":"latest"}}`, want: "latest"},
		{name: "invalid shape", settings: `[]`, want: "9.1.1232", wantWarning: true},
		{name: "invalid type", settings: `{"targetVersion":9.2}`, want: "9.1.1232", wantWarning: true},
		{name: "unsupported", settings: `{"targetVersion":"9.0"}`, want: "9.1.1232", wantWarning: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			version, warning := targetVersionFromSettings([]byte(test.settings), previous)
			if got := version.String(); got != test.want {
				t.Fatalf("version = %q, want %q", got, test.want)
			}
			if (warning != "") != test.wantWarning {
				t.Fatalf("warning = %q", warning)
			}
		})
	}
}

func TestUnresolvedSeverityFromSettings(t *testing.T) {
	previous := syntax.DiagnosticInformation
	tests := []struct {
		name        string
		settings    string
		want        syntax.DiagnosticSeverity
		wantWarning bool
	}{
		{name: "empty object", settings: `{}`, want: previous},
		{name: "direct", settings: `{"unresolvedSeverity":"error"}`, want: syntax.DiagnosticError},
		{name: "nested", settings: `{"vimls":{"unresolvedSeverity":"hint"}}`, want: syntax.DiagnosticHint},
		{name: "invalid shape", settings: `[]`, want: previous, wantWarning: true},
		{name: "invalid type", settings: `{"unresolvedSeverity":2}`, want: previous, wantWarning: true},
		{name: "invalid value", settings: `{"unresolvedSeverity":"off"}`, want: previous, wantWarning: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			severity, warning := unresolvedSeverityFromSettings([]byte(test.settings), previous)
			if severity != test.want || (warning != "") != test.wantWarning {
				t.Fatalf("severity = %v, warning = %q, want %v, warning=%t", severity, warning, test.want, test.wantWarning)
			}
		})
	}
}
