package analysis

import (
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestMappingExpressionPromptDiagnostics(t *testing.T) {
	for _, test := range []struct{ source, code, target string }{
		{"nnoremap <F5> :call writefile([], <C-R>=getcwd()<CR>)<CR>\n", "", ""},
		{"nnoremap <leader>e :e <C-R>=substitute(expand('%:p:h').'/', getcwd().'/', '', '')<CR>\n", "", ""},
		{"inoremap <F5> <C-R><C-O>=doesnotexist()<CR>\n", "vim/E117", "doesnotexist"},
		{"nnoremap <F5> \"=len()<CR>p\n", "vim/E119", "len"},
		{"function! s:Path() abort\nreturn ''\nendfunction\ninoremap <F5> <C-R>=<SID>Path()<CR>\n", "", ""},
		{"inoremap <F5> <C-O>=doesnotexist()<CR>\n", "", ""},
	} {
		file := syntax.Parse(test.source)
		var found []syntax.Diagnostic
		for _, diagnostic := range CombinedDiagnostics(file, Analyze(file)) {
			if diagnostic.Code == "vim/E117" || diagnostic.Code == "vim/E119" {
				found = append(found, diagnostic)
			}
		}
		if test.code == "" {
			if len(found) != 0 {
				t.Fatalf("%q: unexpected diagnostics %#v", test.source, found)
			}
		} else if len(found) != 1 || found[0].Code != test.code || file.Text(found[0].Span) != test.target {
			t.Fatalf("%q: diagnostics %#v, want %s on %q", test.source, found, test.code, test.target)
		}
	}
}
