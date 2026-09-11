package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// Oracle: Vim v9.2.1015, evalvars.c:ex_let_option, option.c and
// testdir/test_let.vim / test_vim9_assign.vim. Script and def errors differ.
func TestOptionAssignmentErrors(t *testing.T) {
	for _, tc := range []struct {
		body  string
		codes [3]string // Legacy, Vim9 script, Vim9 def
	}{
		{"setglobal buflisted=1", [3]string{"E474", "E474", "E474"}},
		{"setglobal diff=1", [3]string{"E474", "E474", "E474"}},
		{"set diff=1", [3]string{"E474", "E474", "E474"}},
		{"set foldclose=bogus", [3]string{"E474", "E474", "E474"}},
		{"&foldclose = 'bogus'", [3]string{"E474", "E474", "E474"}},
		{"&diff = 'abc'", [3]string{"E521", "E521", "E1012"}},
		{"&foldenable = 'abc'", [3]string{"E521", "E521", "E1012"}},
		{"&syntax = v:true", [3]string{"E928", "E928", "E1012"}},
		{"&hidden = '  1'", [3]string{"E521", "E521", "E1012"}},
		{"&hidden = '+1'", [3]string{"E521", "E521", "E1012"}},
		{"&hidden = '0x0'", [3]string{"E521", "E521", "E1012"}},
		{"&hidden = '0b0'", [3]string{"E521", "E521", "E1012"}},
		{"&hidden = '0o0'", [3]string{"E521", "E521", "E1012"}},
		{"&hidden = '-0'", [3]string{"E521", "E521", "E1012"}},
		{"&hidden = '0x1'", [3]string{"", "E521", "E1012"}},
		{"&hidden = '-1'", [3]string{"", "E521", "E1012"}},
		{"set hidden=abc", [3]string{"E474", "E474", "E474"}},
		{"set nohidden=1", [3]string{"E474", "E474", "E474"}},
		{"set hidden:abc", [3]string{"E474", "E474", "E474"}},
		{"setlocal hid+=1", [3]string{"E474", "E474", "E474"}},
		{"setglobal history+=abc", [3]string{"E521", "E521", "E521"}},
		{"&hidden = 'abc'", [3]string{"E521", "E521", "E1012"}},
		{"&g:hi = 'abc'", [3]string{"E521", "E521", "E1012"}},
		{"&titlestring = v:true", [3]string{"E928", "E928", "E1012"}},
		{"&l:titlestring = v:null", [3]string{"E928", "E928", "E1012"}},
		{"&titlestring += 1", [3]string{"E734", "E734", "E1012"}},
		{"&hidden ..= 'x'", [3]string{"E734", "E734", "E521"}},
		{"&history = function('tr')", [3]string{"E703", "E703", "E1012"}},
		{"&titlestring = function('tr')", [3]string{"E729", "E729", "E1012"}},
		{"&hidden = []", [3]string{"E745", "E745", "E1012"}},
		{"&titlestring = []", [3]string{"E730", "E730", "E1012"}},
		{"&history = {}", [3]string{"E728", "E728", "E1012"}},
		{"&titlestring = {}", [3]string{"E731", "E731", "E1012"}},
		{"&hidden = 0z01", [3]string{"E974", "E974", "E1012"}},
		{"&titlestring = 0z01", [3]string{"E976", "E976", "E1012"}},
		{"&history = 1.5", [3]string{"E805", "E805", "E1012"}},
		{"&history = v:none", [3]string{"", "E611", "E1012"}},
		{"&history = v:true", [3]string{"", "E1138", "E1012"}},
		{"&hidden = 0", [3]string{"", "", ""}},
		{"&hidden = 1", [3]string{"", "", ""}},
		{"&titlestring += 'x'", [3]string{"E734", "E734", "E1051"}},
		{"&hidden += v:true", [3]string{"", "", "E521"}},
		{"&hidden = 2", [3]string{"", "E1023", "E1012"}},
		{"&titlestring ..= v:true", [3]string{"E928", "E928", "E1012"}},
		{"&titlestring ..= 123", [3]string{"", "E928", "E1012"}},
		{"&hidden += 'abc'", [3]string{"", "E521", "E521"}},
		{"&history += 'abc'", [3]string{"", "", "E1012"}},
		{"&titlestring = (v:true)", [3]string{"E928", "E928", "E1012"}},
		{"&hidden = ('abc')", [3]string{"E521", "E521", "E1012"}},
		{"&titlestring = 1.5", [3]string{"", "", "E1012"}},
		{"&hidden = '0'", [3]string{"", "", "E1012"}},
		{"&hidden = '1abc'", [3]string{"", "E521", "E1012"}},
		{"&hidden = v:null", [3]string{"", "", "E1012"}},
		{"&opfunc = function('tr')", [3]string{"", "", ""}},
	} {
		for mode, code := range tc.codes {
			source := tc.body + "\n"
			if mode == 0 && !strings.HasPrefix(source, "set") {
				source = "let " + source
			} else if mode == 1 {
				source = "vim9script\n" + source
			} else if mode == 2 {
				source = "vim9script\ndef Assign()\n" + source + "enddef\n"
			}
			t.Run(source, func(t *testing.T) { assertOptionAssignmentError(t, source, code) })
		}
	}
}

func assertOptionAssignmentError(t *testing.T, source, code string) {
	t.Helper()
	file := syntax.Parse(source)
	var errors []syntax.Diagnostic
	for _, diagnostic := range CombinedDiagnostics(file, Analyze(file)) {
		if strings.HasPrefix(diagnostic.Code, "vim/E") {
			errors = append(errors, diagnostic)
		}
	}
	if code == "" {
		if len(errors) != 0 {
			t.Fatalf("unexpected errors: %#v", errors)
		}
		return
	}
	if len(errors) != 1 || errors[0].Code != "vim/"+code {
		t.Fatalf("errors = %#v, want exactly %s", errors, code)
	}
	span := errors[0].Span
	if span.Start < 0 || span.End <= span.Start || span.End > len(source) {
		t.Fatalf("invalid diagnostic span: %#v", span)
	}
}

func TestOptionAssignmentKnownAndUnknownRHS(t *testing.T) {
	for _, tc := range []struct{ source, code string }{
		{"let value = []\nlet value = g:dynamic\nlet &hidden = value\n", ""},
		{"let Bad = [1]\nlet &hidden = Bad\n", "E745"},
		{"let Bad = {}\nlet &titlestring = Bad\n", "E731"},
		{"vim9script\ndef Assign(value: list<number>)\n&hidden = value\nenddef\n", "E1012"},
		{"vim9script\nvar value: bool = true\n&titlestring = value\n", "E928"},
		{"vim9cmd &titlestring = v:true\n", "E928"},
		{"vim9script\nlegacy let &titlestring = 123\n", ""},
		{"let &hidden = g:dynamic\nlet &titlestring ..= g:dynamic\n", ""},
		{"let value = 'abc'\nlet &hidden = value\n", ""}, // string type alone cannot prove a zero conversion
		{"let &titlestring =\n", ""},
	} {
		t.Run(tc.source, func(t *testing.T) { assertOptionAssignmentError(t, tc.source, tc.code) })
	}
	for _, target := range []string{"hidden", "history", "titlestring", "diff", "foldenable", "syntax"} {
		for _, op := range []string{"=", "+=", "-=", "*=", "/=", "%=", "..="} {
			for _, prefix := range []string{"vim9script\nvar value: any = g:dynamic\n", "vim9script\ndef Assign(value: any)\n"} {
				source := prefix + "&" + target + " " + op + " value\n"
				if strings.Contains(prefix, "def Assign") {
					source += "enddef\n"
				}
				t.Run(source, func(t *testing.T) { assertOptionAssignmentError(t, source, "") })
			}
			assertOptionAssignmentError(t, "let &"+target+" "+op+" g:dynamic\n", "")
		}
	}
}

// Deliberate project policy: compiled boolean compounds use E521 regardless
// of the known RHS type. Ordinary assignments and interpreted code keep their
// existing diagnostics (covered above).
func TestCompiledBooleanOptionCompoundAssignmentPolicy(t *testing.T) {
	for _, tc := range []struct{ target, op, rhs string }{
		{"hidden", "+=", "1"},
		{"hidden", "-=", "1"},
		{"hidden", "%=", "1"},
		{"hidden", "*=", "v:true"},
		{"hidden", "/=", "2"},
		{"hidden", "..=", "'text'"},
		{"g:diff", "+=", "[]"},
	} {
		source := "vim9script\ndef Assign()\n&" + tc.target + " " + tc.op + " " + tc.rhs + "\nenddef\n"
		t.Run(source, func(t *testing.T) {
			assertOptionAssignmentError(t, source, "E521")
			file := syntax.Parse(source)
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E521" && (file.Text(diagnostic.Span) != tc.op || diagnostic.Message != "Compound assignment is not supported for a boolean option") {
					t.Fatalf("unexpected compound assignment diagnostic: %#v", diagnostic)
				}
			}
		})
	}
}
