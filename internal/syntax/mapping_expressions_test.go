package syntax

import (
	"reflect"
	"testing"
)

func TestMappingExpressionPrompts(t *testing.T) {
	for _, test := range []struct {
		name, source string
		calls        []string
	}{
		{"file path", "nnoremap <leader>e :e <C-R>=substitute(expand('%:p:h').'/', getcwd().'/', '', '')<CR>\n", []string{"substitute", "expand", "getcwd"}},
		{"continuation", "nnoremap <F5> :e <C-R>=substitute(\n\\ expand('%:p:h').'/', getcwd().'/', '', '')<CR>\n", []string{"substitute", "expand", "getcwd"}},
		{"unicode CRLF", "nnoremap 😀 :e <C-R>=getcwd()<CR>\r\n", []string{"getcwd"}},
		{"insert register", "inoremap <F5> <C-R>=string(1)<CR>\n", []string{"string"}},
		{"literal register", "cnoremap <F5> <C-R><C-R>=string(1)<CR>\n", []string{"string"}},
		{"insert literal no indent", "inoremap <F5> <C-R><C-O>=string(1)<CR>\n", []string{"string"}},
		{"command line literal", "cnoremap <F5> <C-R><C-O>=string(1)<CR>\n", []string{"string"}},
		{"insert fixed indent", "inoremap <F5> <C-R><C-P>=string(1)<CR>\n", []string{"string"}},
		{"command line replacement", "cnoremap <F5> <C-\\>estring(1)<CR>\n", []string{"string"}},
		{"normal register", "nnoremap <F5> \"=string(1)<CR>p\n", []string{"string"}},
		{"normal execute register", "nnoremap <F5> @=string(1)<CR>\n", []string{"string"}},
		{"enter insert", "nnoremap <F5> i<C-R>=string(1)<CR><Esc>\n", []string{"string"}},
		{"insert one command", "inoremap <F5> <C-O>:echo <C-R>=string(1)<CR><CR>\n", []string{"string"}},
		{"insert one register command", "inoremap <F5> <C-\\><C-O>\"=string(1)<CR>p\n", []string{"string"}},
		{"multiple prompts", "nnoremap <F5> :e <C-R>=expand('%')<CR>/<C-R><C-O>=getcwd()<CR><CR>\n", []string{"expand", "getcwd"}},
		{"abbreviation", "iabbrev x <C-R>=string(1)<CR>\n", []string{"string"}},
		{"lowercase and Enter alias", "inoremap <F5> <c-r><c-o>=string(1)<Enter>\n", []string{"string"}},
		{"control M alias", "inoremap <F5> <C-R>=string(1)<C-M>\n", []string{"string"}},
		{"literal control keys", "inoremap <F5> \x12\x0f=string(1)<CR>\n", []string{"string"}},
		{"comparison", "inoremap <F5> <C-R>=len([]) < 1<CR>\n", []string{"len"}},
		{"script prefix", "inoremap <F5> <C-R>=<SID>Value()<CR>\n", []string{"<SID>Value"}},
		{"incomplete prompt", "inoremap <F5> <C-R>=getcwd(\n", []string{"getcwd"}},
		{"normal redo is not register", "nnoremap <F5> <C-R>=string(1)<CR>\n", nil},
		{"single control O is not register", "inoremap <F5> <C-O>=string(1)<CR>\n", nil},
		{"command line control P is path", "cnoremap <F5> <C-R><C-P>=string(1)<CR>\n", nil},
		{"quoted register key", "inoremap <F5> <C-V><C-R>=string(1)<CR>\n", nil},
		{"cancelled prompt", "inoremap <F5> <C-R>=string(1)<Esc>\n", nil},
		{"edited prompt stays opaque", "inoremap <F5> <C-R>=str<BS>ing(1)<CR>\n", nil},
		{"after command line terminator", "cnoremap <F5> <CR><C-R>=string(1)<CR>\n", nil},
		{"after execute register", "nnoremap <F5> @=string(1)<CR><C-R>=getcwd()<CR>\n", []string{"string"}},
		{"expr mapping string", "inoremap <expr> <F5> '<C-R>=string(1)<CR>'\n", nil},
		{"direct command payload", "inoremap <F5> <Cmd>echo '<C-R>=string(1)<CR>'<CR>\n", nil},
		{"empty prompt", "inoremap <F5> <C-R>=<CR>\n", nil},
		{"terminal mapping", "tnoremap <F5> <C-R>=string(1)<CR>\n", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Commands) != 1 || len(file.Diagnostics) != 0 {
				t.Fatalf("commands = %d; diagnostics = %#v", len(file.Commands), file.Diagnostics)
			}
			var calls []string
			var walk func(*Expression)
			walk = func(expression *Expression) {
				if expression == nil {
					return
				}
				if expression.Span.Start < 0 || expression.Span.End > len(file.Source) {
					t.Fatalf("invalid span: %#v", expression.Span)
				}
				if expression.Kind == ExpressionCall && len(expression.Children) > 0 {
					callee := expression.Children[0]
					calls = append(calls, callee.Value)
					if file.Text(callee.Span) != callee.Value {
						t.Fatalf("callee span %q != %q", file.Text(callee.Span), callee.Value)
					}
				}
				for _, child := range expression.Children {
					walk(child)
				}
			}
			for _, expression := range file.Commands[0].Expressions {
				walk(expression)
			}
			if !reflect.DeepEqual(calls, test.calls) {
				t.Fatalf("calls = %#v, want %#v", calls, test.calls)
			}
			if embedded := file.Commands[0].Embedded; embedded != nil && len(file.Commands[0].Expressions) > 0 {
				for _, command := range embedded.Commands {
					if command.Span.End > file.Commands[0].Expressions[0].Span.Start {
						t.Fatal("embedded command overlaps the register expression")
					}
				}
			}
		})
	}
}

func TestMappingExpressionStaticCommandPrefix(t *testing.T) {
	for _, test := range []struct{ rhs, name string }{
		{":edit <C-R>=getcwd()<CR>", "edit"},
		{":<C-U>silent! vs <C-R>=getcwd()<CR>", "vs"},
		{":tabe! <C-R>=getcwd()<CR>", "tabe"},
		{":call Foo(<C-R>=string(1)<CR>)<CR>", "call"},
		{":edit<C-R>=getcwd()<CR>", ""},
		{":<C-R>=getcwd()<CR>", ""},
		{":edit <C-\\>egetcwd()<CR>", ""},
		{":edit <BS><C-R>=getcwd()<CR>", ""},
	} {
		t.Run(test.rhs, func(t *testing.T) {
			file := Parse("nnoremap <F5> " + test.rhs + "\n")
			embedded := file.Commands[0].Embedded
			if test.name == "" {
				if embedded != nil {
					t.Fatalf("unexpected command head: %#v", embedded)
				}
				return
			}
			if embedded == nil || len(embedded.Commands) != 1 {
				t.Fatalf("command head = %#v", embedded)
			}
			head := embedded.Commands[0]
			if file.Text(head.Name) != test.name {
				t.Fatalf("command head = %#v", head)
			}
			if len(file.Diagnostics) != 0 {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
		})
	}
}
