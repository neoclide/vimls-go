package server

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
)

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

func TestRuntimepathFromOptions(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	paths, configured, warning := runtimepathFromOptions([]byte(`{"runtimepath":["` + second + `","` + first + `","` + second + `"]}`))
	if warning != "" || !configured || len(paths) != 2 || paths[0] != filepath.Clean(second) || paths[1] != filepath.Clean(first) {
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

func TestRuntimeDirectoriesFromVimOutputAndEnvironment(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing")
	output := []byte(`["` + first + `","` + missing + `","` + second + `","` + first + `"]`)
	paths, err := runtimeDirectoriesFromVimOutput(output)
	if err != nil || !reflect.DeepEqual(paths, []string{filepath.Clean(first), filepath.Clean(second)}) {
		t.Fatalf("runtime directories = %#v, %v", paths, err)
	}
	for _, output := range [][]byte{nil, []byte(`null`), []byte(`{"runtimepath":[]}`), []byte("[]\nnoise")} {
		paths, err := runtimeDirectoriesFromVimOutput(output)
		if err == nil {
			t.Fatalf("invalid Vim output %q accepted as %#v", output, paths)
		}
	}

	t.Setenv("VIMRUNTIME", first)
	paths, cause := runtimepathEnvironmentFallback(errors.New("query failed"))
	if !reflect.DeepEqual(paths, []string{filepath.Clean(first)}) || cause == nil {
		t.Fatalf("environment fallback = %#v, %v", paths, cause)
	}
}

func TestCleanVimEnvironment(t *testing.T) {
	environment := cleanVimEnvironment([]string{
		"PATH=/bin", "VIMINIT=source bad.vim", "EXINIT=bad", "GVIMINIT=bad",
		"VIM=/tmp/vim", "VIMRUNTIME=/tmp/runtime", "XDG_CONFIG_HOME=/tmp/config",
		"LANG=zh_CN.UTF-8", "LC_ALL=zh_CN.UTF-8", "KEEP=value",
	})
	joined := strings.Join(environment, "\n")
	for _, forbidden := range []string{"VIMINIT=", "EXINIT=", "GVIMINIT=", "VIM=", "VIMRUNTIME=", "XDG_CONFIG_HOME="} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("environment retained %q: %q", forbidden, joined)
		}
	}
	for _, required := range []string{"PATH=/bin", "KEEP=value", "LC_ALL=C", "LANG=C"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("environment omitted %q: %q", required, joined)
		}
	}
	if count := strings.Count(joined, "LANG="); count != 1 {
		t.Fatalf("LANG count = %d in %q", count, joined)
	}
}

func TestInitializeDiscoversRuntimepathOnlyWhenAbsent(t *testing.T) {
	discovered := t.TempDir()
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	calls := 0
	instance.runtimepathFinder = func(context.Context) ([]string, error) {
		calls++
		return []string{discovered}, nil
	}
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceMu.Lock()
	paths := append([]string(nil), instance.runtimePaths...)
	instance.workspaceMu.Unlock()
	if calls != 1 || !reflect.DeepEqual(paths, []string{filepath.Clean(discovered)}) {
		t.Fatalf("discovered runtimepath = %#v, calls = %d", paths, calls)
	}

	explicit := New(nil, nil, io.Discard)
	t.Cleanup(explicit.stopAnalysis)
	explicit.runtimepathFinder = func(context.Context) ([]string, error) {
		t.Fatal("runtimepath finder called for an explicitly empty runtimepath")
		return nil, nil
	}
	if _, err := explicit.Initialize(context.Background(), &protocol.InitializeParams{InitializationOptions: protocol.LSPAny([]byte(`{"runtimepath":[]}`))}); err != nil {
		t.Fatal(err)
	}
	explicit.workspaceMu.Lock()
	paths = append(paths[:0], explicit.runtimePaths...)
	explicit.workspaceMu.Unlock()
	if len(paths) != 0 {
		t.Fatalf("explicit empty runtimepath = %#v", paths)
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
