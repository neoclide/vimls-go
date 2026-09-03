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
