package server

import (
	"errors"
	"path/filepath"
	"testing"
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
	paths, warning := runtimepathFromOptions([]byte(`{"runtimepath":["` + first + `","` + second + `","` + first + `"]}`))
	if warning != "" || len(paths) != 2 || paths[0] != filepath.Clean(first) || paths[1] != filepath.Clean(second) {
		t.Fatalf("runtimepath = %#v, warning = %q", paths, warning)
	}
	for _, raw := range []any{
		[]byte(`{"runtimepath":"` + first + `"}`),
		[]byte(`{"runtimepath":["` + first + `",1]}`),
		[]byte(`[]`),
	} {
		if paths, warning := runtimepathFromOptions(raw); len(paths) != 0 || warning == "" {
			t.Fatalf("invalid runtimepath %s = %#v, warning = %q", raw, paths, warning)
		}
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
