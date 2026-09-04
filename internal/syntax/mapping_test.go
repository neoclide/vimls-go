package syntax

import "testing"

// Mapping semantics follow Vim v9.2.1015 runtime/doc/map.txt and the command
// handling in src/map.c. The parser keeps the raw RHS span, exposing an
// expression for <expr> mappings and an embedded command list only for RHS
// forms that directly execute Ex commands.

func TestMappingExprAST(t *testing.T) {
	tests := []struct {
		name             string
		source           string
		index            int
		allowDiagnostics bool
		check            func(*testing.T, *File, *Mapping)
	}{
		{
			name:   "combined modifiers and trailing rhs",
			source: "map <buffer><nowait> foo bar  \n",
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if mapping.Kind != MappingDefine || mapping.Mode != MappingModeNormalVisualSelectOperator || !mapping.Buffer || !mapping.Nowait || mapping.Query {
					t.Fatalf("mapping = %#v", mapping)
				}
				if got := file.Text(mapping.LHS); got != "foo" {
					t.Fatalf("lhs = %q", got)
				}
				if got := file.Text(mapping.RHS); got != "bar  " {
					t.Fatalf("rhs = %q", got)
				}
			},
		},
		{
			name:   "all mapping flags",
			source: "vnoremap <script> <buffer> <expr> <silent> <special> <unique> bar isbar\n",
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if mapping.Kind != MappingNoremap || mapping.Mode != MappingModeVisual|MappingModeSelect || !mapping.Script || !mapping.Buffer || !mapping.Expr || !mapping.Silent || !mapping.Special || !mapping.Unique {
					t.Fatalf("mapping = %#v", mapping)
				}
				if file.Text(mapping.LHS) != "bar" || file.Text(mapping.RHS) != "isbar" {
					t.Fatalf("spans = lhs %q rhs %q", file.Text(mapping.LHS), file.Text(mapping.RHS))
				}
			},
		},
		{
			name:   "legacy expr call and binary",
			source: "nmap <expr> lhs Fn('x') . suffix\n",
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if !mapping.Expr || mapping.RHSExpression == nil || mapping.RHSExpression.Kind != ExpressionBinary {
					t.Fatalf("legacy expression = %#v", mapping.RHSExpression)
				}
				if got := file.Text(mapping.RHS); got != "Fn('x') . suffix" {
					t.Fatalf("rhs = %q", got)
				}
				if mapping.RHSExpression.Span != (Span{Start: mapping.RHS.Start, End: mapping.RHS.End}) {
					t.Fatalf("rhs expression span = %#v, rhs = %#v", mapping.RHSExpression.Span, mapping.RHS)
				}
				if mapping.RHSExpression.Children[0].Kind != ExpressionCall || file.Text(mapping.RHSExpression.Children[0].Span) != "Fn('x')" {
					t.Fatalf("call = %#v", mapping.RHSExpression.Children[0])
				}
			},
		},
		{
			name:   "vim9 expr concat",
			source: "vim9cmd nmap <expr> lhs left .. right\n",
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if file.Commands[0].Dialect != Vim9 || mapping.RHSExpression == nil || mapping.RHSExpression.Kind != ExpressionBinary || mapping.RHSExpression.Value != ".." {
					t.Fatalf("vim9 expression = %#v", mapping.RHSExpression)
				}
				if mapping.RHSExpression.Operator != (Span{Start: mapping.RHS.Start + len("left "), End: mapping.RHS.Start + len("left ..")}) {
					t.Fatalf("operator span = %#v, rhs = %#v", mapping.RHSExpression.Operator, mapping.RHS)
				}
				if file.Text(mapping.RHSExpression.Span) != "left .. right" {
					t.Fatalf("expression span text = %q", file.Text(mapping.RHSExpression.Span))
				}
			},
		},
		{
			name:   "legacy modifier selects legacy expression",
			source: "vim9script\nlegacy nmap <expr> lhs Fn ('x') . suffix\n",
			index:  1,
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if file.Commands[1].Dialect != Legacy || mapping.RHSExpression == nil || mapping.RHSExpression.Kind != ExpressionBinary {
					t.Fatalf("legacy modifier command=%#v expression=%#v", file.Commands[1], mapping.RHSExpression)
				}
				if mapping.RHSExpression.Children[0].Kind != ExpressionCall {
					t.Fatalf("legacy whitespace call = %#v", mapping.RHSExpression.Children[0])
				}
			},
		},
		{
			name:   "vim9 file maps logical expression spans",
			source: "vim9script\nnmap <expr> lhs left .. right\n",
			index:  1,
			check: func(t *testing.T, file *File, mapping *Mapping) {
				expression := mapping.RHSExpression
				if file.Commands[1].Dialect != Vim9 || expression == nil || expression.Kind != ExpressionBinary || expression.Value != ".." {
					t.Fatalf("vim9 mapping command=%#v expression=%#v", file.Commands[1], expression)
				}
				if expression.Span != mapping.RHS || file.Text(expression.Span) != "left .. right" || file.Text(expression.Operator) != ".." {
					t.Fatalf("expression span=%#v operator=%#v rhs=%#v", expression.Span, expression.Operator, mapping.RHS)
				}
				if len(expression.Children) != 2 || file.Text(expression.Children[0].Span) != "left" || file.Text(expression.Children[1].Span) != "right" {
					t.Fatalf("expression children = %#v", expression.Children)
				}
			},
		},
		{
			name:   "ordinary mapping keeps opaque rhs",
			source: "nmap lhs left .. right\n",
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if mapping.Expr || mapping.RHSExpression != nil || file.Text(mapping.RHS) != "left .. right" {
					t.Fatalf("ordinary mapping = %#v rhs=%q", mapping, file.Text(mapping.RHS))
				}
			},
		},
		{
			name:   "empty expr rhs remains query without expression",
			source: "nmap <expr> lhs\n",
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if !mapping.Expr || !mapping.Query || mapping.RHS != (Span{}) || mapping.RHSExpression != nil {
					t.Fatalf("empty expr mapping = %#v", mapping)
				}
			},
		},
		{
			name:             "incomplete expr recovers next line",
			source:           "nmap <expr> lhs Fn(\necho next\n",
			allowDiagnostics: true,
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if mapping.RHSExpression == nil || len(file.Diagnostics) == 0 {
					t.Fatalf("incomplete expression=%#v diagnostics=%#v", mapping.RHSExpression, file.Diagnostics)
				}
				if mapping.RHSExpression.Kind != ExpressionCall || file.Text(mapping.RHSExpression.Span) != "Fn(" {
					t.Fatalf("partial expression = %#v text=%q", mapping.RHSExpression, file.Text(mapping.RHSExpression.Span))
				}
				if len(file.Commands) != 2 || file.Commands[1].Canonical != "echo" || file.Text(file.Commands[1].Argument) != "next" {
					t.Fatalf("recovery commands = %#v", file.Commands)
				}
			},
		},
		{
			name:   "vim9 script command rhs",
			source: "vim9script\nnoremap <buffer> <F5> <ScriptCmd>MyFunc('a') \\| MyFunc('b')<CR>\n",
			index:  1,
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if mapping.Kind != MappingNoremap || mapping.Mode != MappingModeNormalVisualSelectOperator || !mapping.Buffer {
					t.Fatalf("mapping = %#v commands = %#v", mapping, file.Commands)
				}
				if file.Text(mapping.RHS) != "<ScriptCmd>MyFunc('a') \\| MyFunc('b')<CR>" {
					t.Fatalf("rhs = %q", file.Text(mapping.RHS))
				}
				if len(file.Commands) != 2 || file.Commands[1].Canonical != "noremap" {
					t.Fatalf("commands = %#v", file.Commands)
				}
			},
		},
		{
			name:   "unmap keeps trailing lhs whitespace",
			source: "map @@ foo\nunmap @@ | echo done\n",
			index:  1,
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if mapping.Kind != MappingUnmap || mapping.Mode != MappingModeNormalVisualSelectOperator || file.Text(mapping.LHS) != "@@ " || mapping.RHS != (Span{}) {
					t.Fatalf("mapping = %#v lhs=%q", mapping, file.Text(mapping.LHS))
				}
				if len(file.Commands) != 3 || file.Commands[2].Canonical != "echo" {
					t.Fatalf("commands = %#v", file.Commands)
				}
			},
		},
		{
			name:   "queries",
			source: "map\nnmap foo\n",
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if !mapping.Query || mapping.LHS != (Span{}) || mapping.RHS != (Span{}) {
					t.Fatalf("all query = %#v", mapping)
				}
				if len(file.Commands) != 2 || file.Commands[1].Mapping == nil || !file.Commands[1].Mapping.Query || file.Text(file.Commands[1].Mapping.LHS) != "foo" {
					t.Fatalf("prefix query = %#v", file.Commands)
				}
			},
		},
		{
			name:   "clear modes",
			source: "mapclear! <buffer>\nabclear <buffer>\n",
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if mapping.Kind != MappingClear || mapping.Mode != MappingModeInsertCommandLine || !mapping.Bang || !mapping.Buffer || !mapping.Clear {
					t.Fatalf("mapclear = %#v", mapping)
				}
				abbr := file.Commands[1].Mapping
				if abbr == nil || abbr.Kind != MappingClear || !abbr.Abbreviation || abbr.Mode != MappingModeInsertCommandLine || !abbr.Buffer {
					t.Fatalf("abclear = %#v", abbr)
				}
			},
		},
		{
			name:   "abbreviation",
			source: "iabbrev <buffer><silent> foo four old otters\nnoreabbrev <expr> bar Bar()\n",
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if mapping.Mode != MappingModeInsert || !mapping.Abbreviation || !mapping.Buffer || !mapping.Silent || file.Text(mapping.RHS) != "four old otters" {
					t.Fatalf("iabbrev = %#v rhs=%q commands=%#v", mapping, file.Text(mapping.RHS), file.Commands)
				}
				abbrev := file.Commands[1].Mapping
				if abbrev == nil || abbrev.Kind != MappingNoremap || abbrev.Mode != MappingModeInsertCommandLine || !abbrev.Abbreviation || !abbrev.Expr {
					t.Fatalf("noreabbrev = %#v", abbrev)
				}
			},
		},
		{
			name:   "key notation is ordinary lhs text",
			source: "nmap <C-V> w <Nop>\n",
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if file.Text(mapping.LHS) != "<C-V>" || file.Text(mapping.RHS) != "w <Nop>" {
					t.Fatalf("spans = lhs %q rhs %q", file.Text(mapping.LHS), file.Text(mapping.RHS))
				}
			},
		},
		{
			name:   "literal ctrl-v protects lhs whitespace",
			source: "nmap foo\x16 bar rhs\n",
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if file.Text(mapping.LHS) != "foo\x16 bar" || file.Text(mapping.RHS) != "rhs" {
					t.Fatalf("spans = lhs %q rhs %q", file.Text(mapping.LHS), file.Text(mapping.RHS))
				}
			},
		},
		{
			name:   "backslash follows default cpoptions",
			source: "nmap foo\\ bar rhs\n",
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if file.Text(mapping.LHS) != "foo\\" || file.Text(mapping.RHS) != "bar rhs" {
					t.Fatalf("spans = lhs %q rhs %q", file.Text(mapping.LHS), file.Text(mapping.RHS))
				}
			},
		},
		{
			name:   "quotes do not protect mapping separator",
			source: `nmap lhs "quoted | echo done` + "\n",
			check: func(t *testing.T, file *File, mapping *Mapping) {
				if file.Text(mapping.RHS) != `"quoted ` {
					t.Fatalf("rhs = %q", file.Text(mapping.RHS))
				}
				if len(file.Commands) != 2 || file.Commands[1].Canonical != "echo" {
					t.Fatalf("commands = %#v", file.Commands)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if !test.allowDiagnostics && len(file.Diagnostics) != 0 {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			if len(file.Commands) <= test.index || file.Commands[test.index].Mapping == nil {
				t.Fatalf("commands = %#v", file.Commands)
			}
			test.check(t, file, file.Commands[test.index].Mapping)
		})
	}
}

func TestMappingCommandRHSAST(t *testing.T) {
	source := "vnoremap <silent> <Plug>(coc-range-select) :<C-u>call CocActionAsync('rangeSelect', visualmode(), v:true)<CR>\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	command := &file.Commands[0]
	if command.Mapping == nil || command.Embedded == nil || file.Text(command.Embedded.Span) != "call CocActionAsync('rangeSelect', visualmode(), v:true)" {
		t.Fatalf("mapping = %#v, embedded = %#v", command.Mapping, command.Embedded)
	}
	if len(command.Embedded.Commands) != 1 || command.Embedded.Commands[0].Canonical != "call" {
		t.Fatalf("embedded commands = %#v", command.Embedded.Commands)
	}
	if got := file.Text(command.Embedded.Commands[0].Argument); got != "CocActionAsync('rangeSelect', visualmode(), v:true)" {
		t.Fatalf("embedded call argument = %q", got)
	}
}
