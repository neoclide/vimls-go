package analysis

import (
	"reflect"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestAutoloadExportedDefDiagnostics(t *testing.T) {
	conflict := "vim9script\n\nvar Clash = 'value'\n\nexport def Clash()\nenddef\n"
	for _, test := range []struct {
		name     string
		source   string
		autoload bool
		wantCode string
	}{
		{
			name:     "rewrites exported def conflict in autoload script",
			source:   conflict,
			autoload: true,
			wantCode: "vim/E707",
		},
		{
			name:     "rewrites exported def conflict with constant in autoload script",
			source:   "vim9script\n\nconst Clash = 'value'\n\nexport def Clash()\nenddef\n",
			autoload: true,
			wantCode: "vim/E707",
		},
		{
			name:     "does not rewrite outside autoload script",
			source:   conflict,
			autoload: false,
			wantCode: "vim/E1041",
		},
		{
			name:     "does not rewrite non-exported def",
			source:   "vim9script\n\nvar Clash = 'value'\n\ndef Clash()\nenddef\n",
			autoload: true,
			wantCode: "vim/E1041",
		},
		{
			name:     "does not rewrite when variable follows exported def",
			source:   "vim9script\n\nexport def Clash()\nenddef\n\nvar Clash = 'value'\n",
			autoload: true,
			wantCode: "vim/E1041",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			input := append([]syntax.Diagnostic(nil), result.Diagnostics...)

			got := AutoloadExportedDefDiagnostics(file, result, test.autoload, result.Diagnostics)
			if len(got) != 1 || got[0].Code != test.wantCode || file.Text(got[0].Span) != "Clash" {
				t.Fatalf("diagnostics = %#v, want one %s for Clash", got, test.wantCode)
			}
			if !reflect.DeepEqual(result.Diagnostics, input) {
				t.Fatalf("input diagnostics mutated: got %#v, want %#v", result.Diagnostics, input)
			}
		})
	}
}
