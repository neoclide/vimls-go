package syntax

import (
	"reflect"
	"strings"
	"testing"
)

func TestEditorContextBranches(t *testing.T) {
	for _, test := range []struct {
		condition string
		yes, no   EditorContext
	}{
		{"has('nvim')", EditorNeovim, EditorVim},
		{"has('gui_macvim')", EditorMacVim, 4},
		{"!has('nvim') && has('gui_macvim')", EditorMacVim, 4},
		{"has('nvim') && has('gui_macvim')", EditorUnreachable, EditorUnknown},
		{"has('nvim') || has('gui_macvim')", 1, 2 | 4},
		{"has('gui_macvim') == 1", EditorMacVim, 4},
		{"!has(\"nvim\")", EditorVim, EditorNeovim},
		{"!!(has('nvim'))", EditorNeovim, EditorVim},
		{"has('nvim') && other", EditorNeovim, EditorUnknown},
		{"has('nvim') || other", EditorUnknown, EditorVim},
		{"!(has('nvim') || other)", EditorVim, EditorUnknown},
		{"has('nvim') == 1", EditorNeovim, EditorVim},
		{"0 != has('nvim')", EditorNeovim, EditorVim},
		{"has('nvim') == 0", EditorVim, EditorNeovim},
		{"has('nvim') == 2", EditorUnknown, EditorUnknown},
		{"has('gui_running')", EditorUnknown, EditorUnknown},
		{"has(feature)", EditorUnknown, EditorUnknown},
		{"has('nvim', extra)", EditorUnknown, EditorUnknown},
		{"CustomHas('nvim')", EditorUnknown, EditorUnknown},
		{"has('nvim') || false", EditorUnknown, EditorVim},
	} {
		for _, prefix := range []string{"", "vim9script\n"} {
			t.Run(prefix+test.condition, func(t *testing.T) {
				file := Parse(prefix + "if " + test.condition + "\nset signcolumn=auto:2\nelse\nset signcolumn=yes:2\nendif\nset signcolumn=yes\n")
				var got []EditorContext
				for _, command := range file.Commands {
					if command.Set != nil {
						got = append(got, command.EditorContext)
					}
				}
				want := []EditorContext{test.yes, test.no, EditorUnknown}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("contexts %v want %v; diagnostics %#v", got, want, file.Diagnostics)
				}
			})
		}
	}
}

func TestEditorContextNestedAndElseif(t *testing.T) {
	file := Parse(`if !has('nvim')
  echo 1
elseif other
  set signcolumn=auto:2
else
  if !has('nvim')
    set signcolumn=auto:3
  else
    set signcolumn=auto:4
  endif
  set signcolumn=auto:5
endif
set signcolumn=auto:6
`)
	var got []EditorContext
	for _, command := range file.Commands {
		if command.Set != nil {
			got = append(got, command.EditorContext)
		}
	}
	want := []EditorContext{EditorNeovim, EditorUnreachable, EditorNeovim, EditorNeovim, EditorUnknown}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("contexts %v want %v", got, want)
	}
}

func TestEditorContextExpressionsAndNestedBodies(t *testing.T) {
	file := Parse(`if has('nvim')
  function! SetColumn()
    set signcolumn=auto:2
  endfunction
  autocmd BufEnter * set signcolumn=auto:3
  command! SetColumn set signcolumn=auto:4
endif
echo has('nvim') && NvimAnd()
echo has('nvim') || VimOr()
echo !has('nvim') ? VimThen() : NvimElse()
`)
	got := make(map[string]EditorContext)
	var visit func(*Expression)
	visit = func(expression *Expression) {
		if expression == nil {
			return
		}
		if expression.Kind == ExpressionIdentifier {
			got[expression.Value] = expression.EditorContext
		}
		for _, child := range expression.Children {
			visit(child)
		}
	}
	for _, command := range file.Commands {
		for _, expression := range command.Expressions {
			visit(expression)
		}
		if command.Set != nil && command.EditorContext != EditorNeovim {
			t.Fatal("function lost defining context")
		}
		if command.Embedded != nil {
			for _, child := range command.Embedded.Commands {
				if child.Set != nil && child.EditorContext != EditorNeovim {
					t.Fatal("embedded command lost defining context")
				}
			}
		}
	}
	for name, want := range map[string]EditorContext{"NvimAnd": EditorNeovim, "VimOr": EditorVim, "VimThen": EditorVim, "NvimElse": EditorNeovim} {
		if value, ok := got[name]; !ok || value != want {
			t.Errorf("%s context %v want %v (found %v)", name, value, want, ok)
		}
	}
}

func TestEditorContextLambdaAndFreshParse(t *testing.T) {
	source := "vim9script\nif has('nvim')\nvar F = () => {\nset signcolumn=auto:2\n}\nendif\n"
	for _, test := range []struct {
		source string
		want   EditorContext
	}{
		{source, EditorNeovim},
		{strings.ReplaceAll(source, "has('nvim')", "other"), EditorUnknown},
		{strings.ReplaceAll(source, "has('nvim')", "!has('nvim')"), EditorVim},
	} {
		file := Parse(test.source)
		found := false
		var visit func(*Expression)
		visit = func(expression *Expression) {
			if expression == nil {
				return
			}
			if expression.LambdaBody != nil {
				for _, command := range expression.LambdaBody.Commands {
					if command.Set != nil {
						found = true
						if command.EditorContext != test.want {
							t.Errorf("lambda context %v want %v", command.EditorContext, test.want)
						}
					}
				}
			}
			for _, child := range expression.Children {
				visit(child)
			}
		}
		for _, command := range file.Commands {
			for _, expression := range command.Expressions {
				visit(expression)
			}
		}
		if !found {
			t.Fatal("missing lambda body")
		}
	}
}

func TestEditorContextRecoveryAndContinuation(t *testing.T) {
	for _, source := range []string{
		"function! F()\nif has('nvim')\nset signcolumn=auto:2\nendfunction\nset signcolumn=auto:3\n",
		"if !has('nvim')\nelse\nset signcolumn=auto:2\nendif\nendif\nset signcolumn=auto:3\n",
		"\ufeffif has(\r\n  \\ 'nvim')\r\nset signcolumn=auto:2\r\nendif\r\nset signcolumn=auto:3\r\n",
	} {
		file := Parse(source)
		var got []EditorContext
		for _, command := range file.Commands {
			if command.Set != nil {
				got = append(got, command.EditorContext)
			}
		}
		if !reflect.DeepEqual(got, []EditorContext{EditorNeovim, EditorUnknown}) {
			t.Fatalf("contexts %v in %q", got, source)
		}
	}
}

func TestEditorContextStandaloneExpressions(t *testing.T) {
	for _, parse := range []func(string) (*Expression, []Diagnostic){(LegacyExpressionParser{}).Parse, (Vim9ExpressionParser{}).Parse} {
		expression, diagnostics := parse("has('nvim') && NvimCall()")
		if len(diagnostics) != 0 || len(expression.Children) != 2 || expression.Children[1].EditorContext != EditorNeovim {
			t.Fatalf("expression %#v diagnostics %#v", expression, diagnostics)
		}
	}
}

func TestEditorContextIncompleteCondition(t *testing.T) {
	// Vim9's automatic continuation would consume the next physical line;
	// use the Legacy command boundary to retain a separate body command.
	file := Parse("if has('nvim') &&\nset scl=auto:2\nendif\n")
	if len(file.Commands) != 3 || file.Commands[1].EditorContext != EditorUnknown {
		t.Fatalf("incomplete condition leaked context: %#v", file.Commands)
	}
}

// Provenance: Vim v9.2.1015 runtime/doc/repeat.txt, :finish.
func TestEditorContextFinishFallthrough(t *testing.T) {
	for _, test := range []struct {
		guard string
		want  EditorContext
	}{
		{"if !has('nvim') | finish | endif", EditorNeovim},
		{"if has('nvim') | finish | endif", EditorVim},
		{"if !has('gui_macvim') | finish | endif", EditorMacVim},
		{"if has('gui_macvim') | finish | endif", 4},
		{"if has('gui_macvim') | finish | endif\nif !has('nvim') | finish | endif", EditorNeovim},
		{"if has('nvim')\necho 1\nelse\nfinish\nendif", EditorNeovim},
		{"if has('nvim')\necho 1\nelseif has('gui_macvim')\nfinish\nelse\nfinish\nendif", EditorNeovim},
		{"if !has('nvim')\nif other\nfinish\nelse\nfinish\nendif\nendif", EditorNeovim},
		{"if !has('nvim')\nif other\nfinish\nendif\nendif", EditorUnknown},
		{"if !has('nvim') && other | finish | endif", EditorUnknown},
		{"if !has('nvim')\nfinish!\nendif", EditorUnknown},
		{"if !has('nvim')\nfinish extra\nendif", EditorUnknown},
		{"if !has('nvim')\n1finish\nendif", EditorUnknown},
		{"while other\nif !has('nvim') | finish | endif\nendwhile", EditorUnknown},
		{"try\nif !has('nvim') | finish | endif\nfinally\necho 1\nendtry", EditorUnknown},
		{"function! Guard()\nif !has('nvim') | finish | endif\nendfunction", EditorUnknown},
		{"autocmd BufEnter * if !has('nvim') | finish | endif", EditorUnknown},
	} {
		for _, prefix := range []string{"", "vim9script\n"} {
			source := prefix + test.guard + "\nset inccommand=nosplit\necho has('nvim') && &inccommand\n"
			file := Parse(source)
			var found bool
			for _, command := range file.Commands {
				if command.Set != nil {
					found = true
					if command.EditorContext != test.want {
						t.Fatalf("%q context=%v want=%v", source, command.EditorContext, test.want)
					}
				}
			}
			if !found {
				t.Fatalf("missing set in %q", source)
			}
		}
	}
}

func TestEditorContextFinishUpdatesRemainingBranchBody(t *testing.T) {
	file := Parse("if other\nif !has('gui_macvim') | finish | endif\nset macmeta\nelse\nset macmeta\nendif\nset macmeta\n")
	var got []EditorContext
	for _, command := range file.Commands {
		if command.Set != nil {
			got = append(got, command.EditorContext)
		}
	}
	if want := []EditorContext{EditorMacVim, EditorUnknown, EditorUnknown}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
