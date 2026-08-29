package syntax

import "testing"

func TestMappingTypedAST(t *testing.T) {
	tests := []struct {
		name   string
		source string
		index  int
		check  func(*testing.T, *File, *Mapping)
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
			if len(file.Diagnostics) != 0 {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			if len(file.Commands) <= test.index || file.Commands[test.index].Mapping == nil {
				t.Fatalf("commands = %#v", file.Commands)
			}
			test.check(t, file, file.Commands[test.index].Mapping)
		})
	}
}
