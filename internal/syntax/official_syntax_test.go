package syntax

import "testing"

// These are small, executable extracts from Vim's own v9.2.1015 tests.  Keep
// the source reference with every case so parser coverage can be audited when
// Vim changes its grammar.
func TestOfficialVimSyntaxMatrix(t *testing.T) {
	tests := []struct {
		name      string
		reference string
		source    string
		dialect   Dialect
		commands  []string
		blocks    []BlockKind
		check     func(*testing.T, *File)
	}{
		{
			name:      "legacy control flow and function",
			reference: "v9.2.1015 src/testdir/test_vimscript.vim Test_interrupt_func_try",
			source:    "function! s:Run(items, ...) abort\n  try\n    for item in a:items\n      if item > 0\n        echo item\n      endif\n    endfor\n  catch /^Vim:/\n  finally\n    let g:done = 1\n  endtry\nendfunction\n",
			dialect:   Legacy,
			commands:  []string{"function", "try", "for", "if", "echo", "endif", "endfor", "catch", "finally", "let", "endtry", "endfunction"},
			blocks:    []BlockKind{BlockFunction, BlockTry, BlockFor, BlockIf},
			check: func(t *testing.T, file *File) {
				if function := file.Commands[0].Function; function == nil || len(function.Parameters) != 2 || !function.Parameters[1].Variadic {
					t.Fatalf("legacy function = %#v", function)
				}
			},
		},
		{
			name:      "generic function declaration and call",
			reference: "v9.2.1015 src/testdir/test_vim9_generics.vim Test_generic_func_definition",
			source:    "vim9script\ndef Fn<A, B>(x: A, y: B): A\n  return x\nenddef\nvar result = Fn<number, string>(1, 'x')\n",
			dialect:   Vim9,
			commands:  []string{"vim9script", "def", "return", "enddef", "var"},
			blocks:    []BlockKind{BlockDef},
			check: func(t *testing.T, file *File) {
				function := file.Commands[1].Function
				call := file.Commands[4].Declaration.Initializer
				if function == nil || len(function.TypeParameters) != 2 || call.Kind != ExpressionCall || len(call.TypeArguments) != 2 {
					t.Fatalf("function = %#v, call = %#v", function, call)
				}
			},
		},
		{
			name:      "typed lambda command block",
			reference: "v9.2.1015 src/testdir/test_vim9_expr.vim Test_expr9_lambda_block",
			source:    "vim9script\nvar Func = (s: string): string => {\n  if s == ''\n    return 'empty'\n  endif\n  return 'hello ' .. s\n}\n",
			dialect:   Vim9,
			commands:  []string{"vim9script", "var"},
			check: func(t *testing.T, file *File) {
				lambda := file.Commands[1].Declaration.Initializer
				if lambda.Kind != ExpressionLambda || lambda.LambdaBody == nil || len(lambda.LambdaBody.Commands) != 4 || lambda.ReturnType.Name != "string" {
					t.Fatalf("lambda = %#v", lambda)
				}
			},
		},
		{
			name:      "interface and implementing class",
			reference: "v9.2.1015 src/testdir/test_vim9_interface.vim Test_interface_method",
			source:    "vim9script\ninterface One\n  def IsEven(nr: number): bool\nendinterface\nclass Two implements One\n  def IsEven(nr: number): bool\n    return nr % 2 == 0\n  enddef\nendclass\n",
			dialect:   Vim9,
			commands:  []string{"vim9script", "interface", "def", "endinterface", "class", "def", "return", "enddef", "endclass"},
			blocks:    []BlockKind{BlockInterface, BlockClass, BlockDef},
			check: func(t *testing.T, file *File) {
				if node := file.Commands[4].Aggregate; node == nil || len(node.Implements) != 1 || file.Text(node.Implements[0]) != "One" {
					t.Fatalf("class = %#v", node)
				}
			},
		},
		{
			name:      "enum comments and constructors",
			reference: "v9.2.1015 src/testdir/test_vim9_enum.vim Test_enum_comments",
			source:    "vim9script\nenum Car  # cars\n  # before enum\n  Honda(), # honda\n  # before enum\n  Ford()   # ford\nendenum\n",
			dialect:   Vim9,
			commands:  []string{"vim9script", "enum", "Honda", "endenum"},
			blocks:    []BlockKind{BlockEnum},
			check: func(t *testing.T, file *File) {
				values := file.Commands[2].EnumValues
				if len(values) != 2 || file.Text(values[0].Name) != "Honda" || file.Text(values[1].Name) != "Ford" || values[0].Initializer == nil {
					t.Fatalf("enum values = %#v", values)
				}
			},
		},
		{
			name:      "type alias to class and tuple",
			reference: "v9.2.1015 src/testdir/test_vim9_typealias.vim Test_typealias",
			source:    "vim9script\nclass C\nendclass\ntype Alias = C\ntype Pair = tuple<number, string>\n",
			dialect:   Vim9,
			commands:  []string{"vim9script", "class", "endclass", "type", "type"},
			blocks:    []BlockKind{BlockClass},
			check: func(t *testing.T, file *File) {
				if file.Commands[3].TypeAlias.Type.Name != "C" || len(file.Commands[4].TypeAlias.Type.Arguments) != 2 {
					t.Fatalf("aliases = %#v, %#v", file.Commands[3].TypeAlias, file.Commands[4].TypeAlias)
				}
			},
		},
		{
			name:      "autoload import and export",
			reference: "v9.2.1015 src/testdir/test_vim9_import.vim Test_vim9script_autoload",
			source:    "vim9script\nimport autoload './XrelautoloadExport.vim' as some\nexport const VALUE: number = 1\nexport def Get(): number\n  return VALUE\nenddef\n",
			dialect:   Vim9,
			commands:  []string{"vim9script", "import", "const", "def", "return", "enddef"},
			blocks:    []BlockKind{BlockDef},
			check: func(t *testing.T, file *File) {
				if node := file.Commands[1].Import; node == nil || !node.Autoload || file.Text(node.Alias) != "some" || len(file.Commands[2].Modifiers) != 1 || len(file.Commands[3].Modifiers) != 1 {
					t.Fatalf("import/export = %#v, %#v, %#v", node, file.Commands[2].Modifiers, file.Commands[3].Modifiers)
				}
			},
		},
		{
			name:      "typed destructuring assignment",
			reference: "v9.2.1015 src/testdir/test_vim9_assign.vim Test_assignment_var_list",
			source:    "vim9script\nvar [X: number, Y: list<tuple<number, string>>; Rest] = values\n",
			dialect:   Vim9,
			commands:  []string{"vim9script", "var"},
			check: func(t *testing.T, file *File) {
				bindings := file.Commands[1].Declaration.Bindings
				if len(bindings) != 3 || bindings[1].ParsedType.Name != "list" || !bindings[2].Rest {
					t.Fatalf("bindings = %#v", bindings)
				}
			},
		},
		{
			name:      "tuple value and type",
			reference: "v9.2.1015 src/testdir/test_tuple.vim Test_tuple_declaration",
			source:    "vim9script\nvar pair: tuple<number, string> = (30, 'vim')\nvar single: tuple<number> = (20,)\n",
			dialect:   Vim9,
			commands:  []string{"vim9script", "var", "var"},
			check: func(t *testing.T, file *File) {
				if file.Commands[1].Declaration.Initializer.Kind != ExpressionTuple || file.Commands[2].Declaration.Initializer.Kind != ExpressionTuple {
					t.Fatalf("tuple declarations = %#v, %#v", file.Commands[1].Declaration, file.Commands[2].Declaration)
				}
			},
		},
		{
			name:      "mixed dialect function bodies",
			reference: "v9.2.1015 src/testdir/test_vim9_script.vim Test_vim9cmd",
			source:    "vim9script\nfunction Legacy()\n  let value = 1 . 2\nendfunction\ndef Modern()\n  var value = 1 .. 2\nenddef\n",
			dialect:   Vim9,
			commands:  []string{"vim9script", "function", "let", "endfunction", "def", "var", "enddef"},
			blocks:    []BlockKind{BlockFunction, BlockDef},
			check: func(t *testing.T, file *File) {
				if file.Commands[2].Dialect != Legacy || file.Commands[5].Dialect != Vim9 {
					t.Fatalf("body dialects = %s, %s", file.Commands[2].Dialect, file.Commands[5].Dialect)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 0 {
				t.Fatalf("%s: diagnostics = %#v", test.reference, file.Diagnostics)
			}
			if file.Dialect != test.dialect {
				t.Fatalf("%s: dialect = %s, want %s", test.reference, file.Dialect, test.dialect)
			}
			if len(file.Commands) != len(test.commands) {
				t.Fatalf("%s: commands = %#v, want %#v", test.reference, commandNames(file), test.commands)
			}
			for index, name := range test.commands {
				if got := commandDisplayName(file.Commands[index]); got != name {
					t.Fatalf("%s: command %d = %q, want %q", test.reference, index, got, name)
				}
			}
			if len(file.Blocks) != len(test.blocks) {
				t.Fatalf("%s: blocks = %#v, want %#v", test.reference, file.Blocks, test.blocks)
			}
			for index, kind := range test.blocks {
				if file.Blocks[index].Kind != kind {
					t.Fatalf("%s: block %d = %s, want %s", test.reference, index, file.Blocks[index].Kind, kind)
				}
			}
			if test.check != nil {
				test.check(t, file)
			}
		})
	}
}

func commandNames(file *File) []string {
	names := make([]string, len(file.Commands))
	for index, command := range file.Commands {
		names[index] = commandDisplayName(command)
	}
	return names
}

func commandDisplayName(command Command) string {
	if command.Canonical != "" {
		return command.Canonical
	}
	return command.TypedName
}

func TestOfficialVimRecoveryMatrix(t *testing.T) {
	tests := []struct {
		name      string
		reference string
		source    string
		code      string
	}{
		{"shortened enum", "v9.2.1015 src/testdir/test_vim9_script.vim Test_command_is_not_shortened", "vim9script\nenu E\nendenum\nvar after = 1\n", "vim/E1065"},
		{"misplaced vim9script", "v9.2.1015 src/testdir/test_vim9_script.vim Test_vim9script_not_first", "let before = 1\nvim9script\nlet after = 2\n", "vim/E1039"},
		{"incomplete generic declaration", "v9.2.1015 src/testdir/test_vim9_generics.vim Test_generic_func_definition", "vim9script\ndef Fn<T(value: T)\nenddef\nvar after = 1\n", "vimls/missing-generic-end"},
		{"incomplete tuple type", "v9.2.1015 src/testdir/test_tuple.vim Test_tuple_type", "vim9script\nvar value: tuple<number, string = (1, 'x')\nvar after = 1\n", "vimls/missing-type-delimiter"},
		{"unmatched block", "v9.2.1015 src/testdir/test_vim9_script.vim Test_missing_endif", "vim9script\nif true\n  echo 'x'\nvar after = 1\n", "vimls/missing-end"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			found := false
			for _, diagnostic := range file.Diagnostics {
				found = found || diagnostic.Code == test.code
			}
			if !found {
				t.Fatalf("%s: diagnostics = %#v, want %s", test.reference, file.Diagnostics, test.code)
			}
			if len(file.Commands) == 0 {
				t.Fatalf("%s: parser did not recover any commands", test.reference)
			}
			last := file.Commands[len(file.Commands)-1]
			if last.Declaration == nil || file.Text(last.Declaration.Name) != "after" {
				t.Fatalf("%s: did not recover to after declaration: %#v", test.reference, last)
			}
		})
	}
}

func TestVim9IncompleteExpressionRecoversAtFollowingStatements(t *testing.T) {
	source := "vim9script\ndef Broken()\n  var value = (\n  echo 'still parsed'\nenddef\nvar after = 1\n"
	file := Parse(source)
	if !hasDiagnostic(file, "vimls/missing-delimiter") || len(file.Commands) != 6 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[3].Canonical != "echo" || file.Commands[4].Canonical != "enddef" || file.Commands[5].Declaration == nil || file.Text(file.Commands[5].Declaration.Name) != "after" {
		t.Fatalf("parser did not recover: %#v", file.Commands)
	}
}
