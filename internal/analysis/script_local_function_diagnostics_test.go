package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// Vim v9.2.1015 runtime/doc/map.txt, script-local: <SID> in a mapping
// identifies the same script-local function as s: in its declaration.
func TestScriptLocalFunctionCallDiagnostics(t *testing.T) {
	for _, call := range []string{
		"nmap <leader>sr :call <SID>SessionReload()<CR>\n",
		"nmap <leader>sr :call <sid>SessionReload()<CR>\n",
		"nnoremap <expr> <F1> <SID>SessionReload()\n",
		"call <SID>SessionReload()\n",
		"call s:SessionReload()\n",
	} {
		for _, definition := range []string{"", "function! s:SessionReload()\nendfunction\n", "function! SessionReload()\nendfunction\n"} {
			for _, forward := range []bool{false, true} {
				source := definition + call
				if forward {
					source = call + definition
				}
				file := syntax.Parse(source)
				var diagnostics []syntax.Diagnostic
				for _, diagnostic := range Analyze(file).Diagnostics {
					if diagnostic.Code == "vim/E117" {
						diagnostics = append(diagnostics, diagnostic)
					}
				}
				want := 1
				if strings.Contains(definition, "s:SessionReload") {
					want = 0
				}
				if len(diagnostics) != want {
					t.Fatalf("%q: diagnostics = %#v, want %d", source, diagnostics, want)
				}
				if want == 1 && (file.Text(diagnostics[0].Span) != "SessionReload" || diagnostics[0].Message != "Unknown function: s:SessionReload") {
					t.Fatalf("%q: diagnostic = %#v", source, diagnostics[0])
				}
			}
		}
	}
}
