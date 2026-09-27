package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestVim921132SkippedCommandBlocks(t *testing.T) {
	// Vim v9.2.1132 src/testdir/test_usercommands.vim
	// Test_command_block_skipped(), commit f3dc0fee778439ac8ff8680b42552f7f13d47396.
	source := `vim9script
def Something(s: string)
  echo s
enddef
if 0
  command! -bar -nargs=? Xfoo {
    Something(<q-args>)
  }
endif
if 0
  autocmd BufRead *.xyz {
    Something(<q-args>)
  }
endif
while 0
  command! Xbar {
    Something('x')
  }
endwhile
g:skipped = 'all three'
`
	file := syntax.Parse(source)
	if diagnostics := CombinedDiagnostics(file, Analyze(file)); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if got := file.Text(file.Commands[len(file.Commands)-1].Argument); got != "g:skipped = 'all three'" {
		t.Fatalf("following command = %q", got)
	}
}

func TestSkippedCommandBlockDiagnosticBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, before, after string
		skipped             bool
	}{
		{"if false", "if false\n", "endif\n", true},
		{"while zero", "while 0\n", "endwhile\n", true},
		{"nested", "if 0\nif g:condition\n", "endif\nendif\n", true},
		{"elseif false", "if g:condition\nelseif 0\n", "endif\n", true},
		{"else after true", "if 1\nelse\n", "endif\n", true},
		{"active", "if 1\n", "endif\n", false},
		{"else after false", "if 0\nelse\n", "endif\n", false},
		{"unknown", "if g:condition\n", "endif\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse("vim9script\n" + test.before + "autocmd BufRead *.xyz {\n  echo <q-args>\n}\n" + test.after)
			diagnostics := CombinedDiagnostics(file, Analyze(file))
			found := false
			for _, diagnostic := range diagnostics {
				found = found || diagnostic.Code == "vim/E1010"
			}
			if found == test.skipped {
				t.Fatalf("skipped = %t, diagnostics = %#v", test.skipped, diagnostics)
			}
		})
	}
	// An unexpanded command argument cannot hide earlier literal mismatches or
	// following script errors. The stored block still has to end with a brace.
	for _, test := range []struct{ source, code string }{
		{"command! -nargs=* Demo {\n  var n: number = 'bad'\n  echo <q-args>\n}\n", "vim/E1012"},
		{"if 0\ncommand! Demo {\n  echo <q-args>\n", "vim/E1026"},
		{"if 0\nautocmd BufRead *.xyz {\n  echo <q-args>\n}\nendif\nvar n: number = 'bad'\n", "vim/E1012"},
	} {
		file := syntax.Parse("vim9script\n" + test.source)
		diagnostics := CombinedDiagnostics(file, Analyze(file))
		found := false
		for _, diagnostic := range diagnostics {
			found = found || diagnostic.Code == test.code
		}
		if !found {
			t.Errorf("%q missing %s: %#v", test.source, test.code, diagnostics)
		}
	}
	for _, header := range []string{"", "vim9script\n"} {
		file := syntax.Parse(header + "command! -nargs=* Demo {\n  echo <q-args>\n}\n")
		if diagnostics := CombinedDiagnostics(file, Analyze(file)); len(diagnostics) != 0 {
			t.Errorf("command template diagnostics = %#v", diagnostics)
		}
	}
}

func TestVim921132SkippedAggregateBodies(t *testing.T) {
	// Vim v9.2.1132 src/testdir/test_vim9_class.vim Test_class_body_skipped().
	source := `vim9script
if 0
  interface Bar
    def M(): number
  endinterface
endif
if 0
  class Foo
    var x: number = 1
  endclass
endif
while 0
  enum Baz
    One,
    Two
  endenum
endwhile
g:skipped = 'all three'
`
	file := syntax.Parse(source)
	if diagnostics := CombinedDiagnostics(file, Analyze(file)); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if !strings.HasPrefix(file.Text(file.Commands[len(file.Commands)-1].Argument), "g:skipped =") {
		t.Fatal("lost the command after the skipped blocks")
	}
}
