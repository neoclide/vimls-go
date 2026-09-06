package analysis

import (
	"reflect"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// optionDiagnostics returns E113/E518 diagnostics for one source in the given
// role, ordered by span like the normal pipeline.
func optionDiagnostics(source string, configFile bool) []syntax.Diagnostic {
	file := syntax.Parse(source)
	if configFile {
		return filterOptionDiagnostics(AnalyzeConfigFile(file).Diagnostics)
	}
	return filterOptionDiagnostics(Analyze(file).Diagnostics)
}

func filterOptionDiagnostics(diagnostics []syntax.Diagnostic) []syntax.Diagnostic {
	var got []syntax.Diagnostic
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "vim/E113" || diagnostic.Code == "vim/E518" {
			got = append(got, diagnostic)
		}
	}
	return got
}

// TestConfigOptionNameDiagnosticsPreservePinnedBehavior verifies §6.1/§9 P0-5:
// E113 (expressions) and E518 (:set family) behave identically in config mode
// for short/long names, no/inv prefixes, +=/-=/^=/?/& operators, :setlocal and
// :setglobal, and &g:/&l: scopes; unknown t_ terminal options stay unknown.
func TestConfigOptionNameDiagnosticsPreservePinnedBehavior(t *testing.T) {
	source := "set ts=4\nset tabstop=4\nset notabstop\nset invtabstop\nset ts+=4\nset ts-=2\nset ts^=8\nset ts?\nset ts&\nsetlocal ts=4\nsetglobal ts=4\nset t_ZZ=abc\nlet a = &ts\nlet b = &g:ts\nlet c = &l:ts\nset missingopt\nsetlocal missinglocal\nsetglobal missingglobal\nlet d = &missingexpr\nlet e = &g:missingg\nlet f = &l:missingl\nlet terminal = &t_runtime\n"
	want := []syntax.Diagnostic{
		{Code: "vim/E518", Message: "Unknown option: missingopt"},
		{Code: "vim/E518", Message: "Unknown option: missinglocal"},
		{Code: "vim/E518", Message: "Unknown option: missingglobal"},
		{Code: "vim/E113", Message: "Unknown option: missingexpr"},
		{Code: "vim/E113", Message: "Unknown option: missingg"},
		{Code: "vim/E113", Message: "Unknown option: missingl"},
	}
	for _, configFile := range []bool{false, true} {
		got := optionDiagnostics(source, configFile)
		if len(got) != len(want) {
			t.Fatalf("configFile=%v option diagnostics = %#v, want %#v", configFile, got, want)
		}
		for index, diagnostic := range got {
			if diagnostic.Code != want[index].Code || diagnostic.Message != want[index].Message {
				t.Fatalf("configFile=%v diagnostic[%d] = %#v, want %#v", configFile, index, diagnostic, want[index])
			}
		}
	}
}

// TestConfigOptionMetadataMatchesBothRoles checks the t_ termcode exclusion and
// option metadata lookups do not differ between roles, keeping the pinned
// v9.2.1015 option table authoritative in configuration files.
func TestConfigOptionMetadataMatchesBothRoles(t *testing.T) {
	sources := []string{
		"set t_AB=terminal\nlet x = &t_CD\n",
		"set columns=100\nlet &numberwidth = 4\n",
		"set fillchars=vert:\\|,eob:~\n",
	}
	for _, source := range sources {
		plugin := optionDiagnostics(source, false)
		config := optionDiagnostics(source, true)
		if !reflect.DeepEqual(plugin, config) {
			t.Fatalf("source %q: plugin diagnostics %#v != config diagnostics %#v", source, plugin, config)
		}
	}
}

func TestUnknownOptionInGuiOrNvimGuard(t *testing.T) {
	cases := []struct {
		name        string
		source      string
		wantWarning bool
	}{
		{
			name: "if has gui_running",
			source: `if has('gui_running')
  set missingopt
  let &missingexpr = 1
endif`,
			wantWarning: true,
		},
		{
			name: "if has nvim double quotes",
			source: `if has("nvim")
  setlocal missingopt
  let &g:missingexpr = 1
endif`,
			wantWarning: false,
		},
		{
			name: "if has gui_running or nvim",
			source: `if has('gui_running') || has('nvim')
  set missingopt
endif`,
			wantWarning: false,
		},
		{
			name: "if has gui_running and other condition",
			source: `if has('gui_running') && has('mac')
  set missingopt
endif`,
			wantWarning: true,
		},
		{
			name: "elseif has gui_running",
			source: `if 0
  echo 1
elseif has('gui_running')
  set missingopt
endif`,
			wantWarning: true,
		},
		{
			name: "comparison to 1",
			source: `if has('gui_running') == 1
  set missingopt
endif`,
			wantWarning: true,
		},
		{
			name: "else branch of has gui_running",
			source: `if has('gui_running')
  echo 'gui'
else
  set missingopt
endif`,
			wantWarning: false,
		},
		{
			name: "negated has gui_running",
			source: `if !has('gui_running')
  set missingopt
endif`,
			wantWarning: false,
		},
		{
			name: "regular if condition",
			source: `if some_var
  set missingopt
endif`,
			wantWarning: false,
		},
		{
			name:        "outside if",
			source:      `set missingopt`,
			wantWarning: false,
		},
		{
			name:        "vim9script if has nvim",
			source:      "vim9script\nif has('nvim')\n  &missingexpr = 1\nendif\n",
			wantWarning: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := syntax.Parse(tc.source)
			analysis := Analyze(file)
			diags := filterOptionDiagnostics(analysis.Diagnostics)
			if len(diags) == 0 {
				t.Fatalf("expected unknown option diagnostics, got none")
			}
			for _, diag := range diags {
				if tc.wantWarning {
					if diag.Severity == nil || *diag.Severity != syntax.DiagnosticWarning {
						t.Errorf("diagnostic %v: severity = %v, want DiagnosticWarning", diag.Code, diag.Severity)
					}
				} else {
					if diag.Severity != nil {
						t.Errorf("diagnostic %v: severity = %v, want nil (default error)", diag.Code, *diag.Severity)
					}
				}
			}
		})
	}
}
