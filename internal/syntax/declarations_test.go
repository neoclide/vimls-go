package syntax

import "testing"

func TestLegacyAggregateDialectGate(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		code       string
		message    string
		kind       BlockKind
		header     string
		aggregate  string
		terminator string
	}{
		{
			name: "class", source: "class LegacyClass\nendclass\nlet after = 1\n",
			code: "vim/E1316", message: "Class can only be defined in Vim9 script",
			kind: BlockClass, header: "class", aggregate: "LegacyClass", terminator: "endclass",
		},
		{
			name: "interface", source: "interface LegacyInterface\nendinterface\nlet after = 1\n",
			code: "vim/E1342", message: "Interface can only be defined in Vim9 script",
			kind: BlockInterface, header: "interface", aggregate: "LegacyInterface", terminator: "endinterface",
		},
		{
			name: "enum", source: "enum LegacyEnum\nendenum\nlet after = 1\n",
			code: "vim/E1414", message: "Enum can only be defined in Vim9 script",
			kind: BlockEnum, header: "enum", aggregate: "LegacyEnum", terminator: "endenum",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || file.Diagnostics[0].Message != test.message {
				t.Fatalf("diagnostics = %#v, want %s", file.Diagnostics, test.code)
			}
			if file.Text(file.Diagnostics[0].Span) != test.header {
				t.Fatalf("diagnostic span = %q, want %q", file.Text(file.Diagnostics[0].Span), test.header)
			}
			if len(file.Commands) != 3 || file.Commands[0].Canonical != test.header || file.Commands[1].Canonical != test.terminator || file.Commands[2].Canonical != "let" {
				t.Fatalf("commands = %#v", file.Commands)
			}
			aggregate := file.Commands[0].Aggregate
			if aggregate == nil || aggregate.Kind != test.kind || file.Text(aggregate.Name) != test.aggregate {
				t.Fatalf("aggregate = %#v", aggregate)
			}
			if len(file.Blocks) != 1 || file.Blocks[0].Kind != test.kind || file.Blocks[0].Header != 0 || file.Blocks[0].End != 1 {
				t.Fatalf("blocks = %#v", file.Blocks)
			}
			if file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "after" {
				t.Fatalf("following command = %#v", file.Commands[2])
			}
			assertFileSpans(t, file)
		})
	}
}

func TestAggregateDialectGateIsContextual(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "vim9 script",
			source: "vim9script\nclass VimClass\nendclass\nvar after = 1\n",
		},
		{
			name:   "def body",
			source: "def Build()\n  class Nested\n  endclass\nenddef\nvar after = 1\n",
		},
		{
			name:   "vim9cmd one shot",
			source: "vim9cmd class OneShot\nendclass\nlet after = 1\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1316" || diagnostic.Code == "vim/E1342" || diagnostic.Code == "vim/E1414" {
					t.Fatalf("unexpected aggregate dialect diagnostic = %#v", diagnostic)
				}
			}
			found := false
			for _, command := range file.Commands {
				if command.Canonical != "class" {
					continue
				}
				found = true
				if command.Dialect != Vim9 || command.Aggregate == nil || file.Text(command.Aggregate.Name) == "" {
					t.Fatalf("class command = %#v", command)
				}
			}
			if !found {
				t.Fatalf("class command not found: %#v", file.Commands)
			}
		})
	}
}

func TestVim9AggregateDeclarations(t *testing.T) {
	source := "vim9script\nclass Child extends Base implements One, Two\nendclass\ninterface Item extends Parent\nendinterface\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
	}
	class := file.Commands[1].Aggregate
	if class == nil || class.Kind != BlockClass || file.Text(class.Name) != "Child" || len(class.Extends) != 1 || file.Text(class.Extends[0]) != "Base" || len(class.Implements) != 2 || file.Text(class.Implements[0]) != "One" || file.Text(class.Implements[1]) != "Two" {
		t.Fatalf("unexpected class declaration: %#v", class)
	}
	interfaceNode := file.Commands[3].Aggregate
	if interfaceNode == nil || interfaceNode.Kind != BlockInterface || file.Text(interfaceNode.Name) != "Item" || len(interfaceNode.Extends) != 1 || file.Text(interfaceNode.Extends[0]) != "Parent" {
		t.Fatalf("unexpected interface declaration: %#v", interfaceNode)
	}
}

func TestVim9AggregateMembers(t *testing.T) {
	source := "vim9script\ninterface One\n  def Read(): string\nendinterface\nclass Two implements One\n  var value: number\n  def Read(): string\n    var local = ''\n    return local\n  enddef\nendclass\nenum Color\n  Red,\n  Green\n  def Name(): string\n    return 'color'\n  enddef\nendenum\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	tests := []struct {
		command int
		members []int
	}{
		{command: 1, members: []int{2}},
		{command: 4, members: []int{5, 6}},
		{command: 11, members: []int{12, 13}},
	}
	for _, test := range tests {
		aggregate := file.Commands[test.command].Aggregate
		if aggregate == nil || len(aggregate.Members) != len(test.members) {
			t.Fatalf("command %d aggregate = %#v", test.command, aggregate)
		}
		for index, member := range test.members {
			if aggregate.Members[index] != member {
				t.Fatalf("command %d members = %#v, want %#v", test.command, aggregate.Members, test.members)
			}
		}
	}
	if file.Commands[7].Declaration == nil || file.Commands[7].Block < 0 || file.Blocks[file.Commands[7].Block].Kind != BlockDef {
		t.Fatalf("local declaration = %#v", file.Commands[7])
	}
	incomplete := Parse("vim9script\nclass Open\n  var value = 1\n")
	if aggregate := incomplete.Commands[1].Aggregate; aggregate == nil || len(aggregate.Members) != 1 || aggregate.Members[0] != 2 {
		t.Fatalf("incomplete aggregate = %#v", aggregate)
	}
}

func TestOfficialVimParserMissingImplementsName(t *testing.T) {
	file := Parse("vim9script\nclass A implements\nendclass\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1389" || file.Diagnostics[0].Message != "missing name after implements" || file.Text(file.Diagnostics[0].Span) != "implements" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 4 || len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockClass || file.Blocks[0].End != 2 {
		t.Fatalf("commands = %#v, blocks = %#v", file.Commands, file.Blocks)
	}
	class := file.Commands[1].Aggregate
	if class == nil || file.Text(class.Name) != "A" || len(class.Implements) != 0 {
		t.Fatalf("class = %#v", class)
	}
	if file.Commands[3].Canonical != "var" || file.Commands[3].Declaration == nil || file.Text(file.Commands[3].Declaration.Name) != "after" {
		t.Fatalf("following command = %#v", file.Commands[3])
	}
	assertFileSpans(t, file)
}

func TestVim9EnumValues(t *testing.T) {
	source := "vim9script\nenum Color\n  Red,\n  RGB(1, 2 + 3, 'blue')\nendenum\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
	}
	enumNode := file.Commands[1].Aggregate
	if enumNode == nil || enumNode.Kind != BlockEnum || file.Text(enumNode.Name) != "Color" {
		t.Fatalf("unexpected enum declaration: %#v", enumNode)
	}
	values := file.Commands[2].EnumValues
	if len(values) != 2 || file.Text(values[0].Name) != "Red" || len(values[0].Arguments) != 0 || file.Text(values[1].Name) != "RGB" || len(values[1].Arguments) != 3 || values[1].Arguments[1].Kind != ExpressionBinary {
		value := values
		t.Fatalf("unexpected enum values: %#v", value)
	}
}

func TestVim9EnumValuesCanShareAndContinueLines(t *testing.T) {
	source := "vim9script\nenum Color\n  White,\n  Red, Green,\n  RGB(\n    1, # red\n    2,\n    3\n  )\n  var channel: number\nendenum\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
	}
	values := file.Commands[2].EnumValues
	if len(values) != 4 {
		t.Fatalf("enum values = %#v; commands = %#v", values, file.Commands)
	}
	want := []string{"White", "Red", "Green", "RGB"}
	for index, value := range values {
		if got := file.Text(value.Name); got != want[index] {
			t.Fatalf("value %d = %q, want %q", index, got, want[index])
		}
	}
	if len(values[3].Arguments) != 3 || file.Commands[3].Declaration == nil {
		t.Fatalf("RGB = %#v, member = %#v", values[3], file.Commands[3])
	}
}

func TestVim9EnumValuesRejectUnexpectedPayload(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "colon",
			source: "vim9script\nenum Foo\n  first,\n  second : 20\nendenum\n",
			want:   []string{"first", "second"},
		},
		{
			name:   "number",
			source: "vim9script\nenum Foo\n  first,\n  second = 2\nendenum\n",
			want:   []string{"first", "second"},
		},
		{
			name:   "string",
			source: "vim9script\nenum Foo\n  first,\n  second = 'second'\nendenum\ndefcompile\n",
			want:   []string{"first", "second"},
		},
		{
			name:   "list",
			source: "vim9script\nenum Foo\n  first,\n  second = []\nendenum\n",
			want:   []string{"first", "second"},
		},
		{
			name:   "empty-colon",
			source: "vim9script\nenum Foo\n\n  # first\n  first:\n  second\nendenum\n",
			want:   []string{"first"},
		},
		{
			name:   "double-equals",
			source: "vim9script\nenum Foo\n  first == 1\nendenum\ndefcompile\n",
			want:   []string{"first"},
		},
		{
			name:   "missing-comma",
			source: "vim9script\nenum Planet\n  mercury venus earth\nendenum\ndefcompile\n",
			want:   []string{"mercury"},
		},
		{
			name:   "object-variable",
			source: "vim9script\nenum Foo\n  final n: number = 10\nendenum\n",
			want:   []string{"final"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1123" {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			var values []string
			for _, command := range file.Commands {
				for _, value := range command.EnumValues {
					values = append(values, file.Text(value.Name))
				}
			}
			if len(values) != len(test.want) {
				t.Fatalf("enum values = %#v, want %#v; commands = %#v", values, test.want, file.Commands)
			}
			for index, want := range test.want {
				if values[index] != want {
					t.Fatalf("enum value %d = %q, want %q", index, values[index], want)
				}
			}
			if file.Commands[len(file.Commands)-1].Canonical != "endenum" && file.Commands[len(file.Commands)-2].Canonical != "endenum" {
				t.Fatalf("endenum was not recovered: commands = %#v", file.Commands)
			}
		})
	}
}

func TestVim9EnumValuesRejectSpaceBeforeConstructor(t *testing.T) {
	for _, constructor := range []string{"Orange (20)", "Orange (20"} {
		t.Run(constructor, func(t *testing.T) {
			source := "vim9script\nenum Fruit\n  Apple(10),\n  " + constructor + "\n\n  def new(t: number)\n  enddef\nendenum\ndefcompile\n"
			file := Parse(source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1068" {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			if len(file.Commands) < 2 || len(file.Commands[2].EnumValues) != 2 || file.Text(file.Commands[2].EnumValues[1].Name) != "Orange" {
				t.Fatalf("commands = %#v", file.Commands)
			}
			if file.Commands[len(file.Commands)-2].Canonical != "endenum" {
				t.Fatalf("endenum was not recovered: commands = %#v", file.Commands)
			}
		})
	}
}

func TestOfficialVim9EnumAllowsTrailingComma(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_class.vim
	// Test_list_type_inference_with_enum_and_object.
	file := Parse("vim9script\nenum Color\n  Red,\n  Green,\nendenum\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 || file.Commands[3].Canonical != "endenum" || len(file.Commands[2].EnumValues) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
}

func TestVim9EnumValueCommaSpacing(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		code      string
		spanText  string
		expectErr bool
	}{
		{
			name:      "space before comma",
			source:    "vim9script\nenum Planet\n  mercury ,\n  venus\nendenum\n",
			code:      "vim/E1068",
			spanText:  " ,",
			expectErr: true,
		},
		{
			name:      "missing space after comma",
			source:    "vim9script\nenum Planet\n  mercury,venus\nendenum\n",
			code:      "vim/E1069",
			spanText:  ",",
			expectErr: true,
		},
		{
			name:   "trailing comma",
			source: "vim9script\nenum Planet\n  mercury,\nendenum\n",
		},
		{
			name:   "comment after comma",
			source: "vim9script\nenum Planet\n  mercury, # comment\n  venus\nendenum\n",
		},
		{
			name:   "constructor comma",
			source: "vim9script\nenum Planet\n  mercury(1, 2), venus\nendenum\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if test.expectErr {
				if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || file.Text(file.Diagnostics[0].Span) != test.spanText {
					t.Fatalf("diagnostics = %#v, want %s over %q", file.Diagnostics, test.code, test.spanText)
				}
				return
			}
			if len(file.Diagnostics) != 0 {
				t.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
			}
		})
	}
}

func TestVim9TypeAlias(t *testing.T) {
	file := Parse("vim9script\ntype Pair = tuple<number, string>\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
	}
	alias := file.Commands[1].TypeAlias
	if alias == nil || file.Text(alias.Name) != "Pair" || file.Text(alias.Assignment) != "=" || alias.Type == nil || alias.Type.Kind != TypeGeneric || alias.Type.Name != "tuple" || len(alias.Type.Arguments) != 2 {
		t.Fatalf("unexpected type alias: %#v", alias)
	}
}

func TestVim9TypeAliasMissingPartsRecoverNextLine(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		code      string
		wantAlias bool
		wantName  string
		wantType  TypeKind
	}{
		{name: "name", source: "vim9script\ntype\nvar after = 1\n", code: "vim/E1397"},
		{name: "type", source: "vim9script\ntype MyType =\nvar after = 1\n", code: "vim/E1398", wantAlias: true, wantName: "MyType", wantType: TypeMissing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			if len(file.Commands) != 3 || file.Commands[2].Canonical != "var" || file.Commands[2].Declaration == nil {
				t.Fatalf("commands = %#v", file.Commands)
			}
			alias := file.Commands[1].TypeAlias
			if test.wantAlias && (alias == nil || file.Text(alias.Name) != test.wantName || alias.Type == nil || alias.Type.Kind != test.wantType || file.Text(alias.Assignment) != "=") {
				t.Fatalf("type alias = %#v", alias)
			}
			if !test.wantAlias && alias != nil {
				t.Fatalf("unexpected type alias = %#v", alias)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestImportForms(t *testing.T) {
	file := Parse("vim9script\nimport 'dir/as-name.vim' as module\nimport autoload $'autoload/{name}.vim' as lazy\nexport def Public()\nenddef\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
	}
	direct := file.Commands[1].Import
	if direct == nil || direct.Autoload || file.Text(direct.PathSpan) != "'dir/as-name.vim'" || file.Text(direct.Alias) != "module" || direct.Path.Kind != ExpressionString {
		t.Fatalf("direct import = %#v", direct)
	}
	lazy := file.Commands[2].Import
	if lazy == nil || !lazy.Autoload || file.Text(lazy.PathSpan) != "$'autoload/{name}.vim'" || file.Text(lazy.Alias) != "lazy" {
		t.Fatalf("autoload import = %#v", lazy)
	}
	if len(file.Commands[3].Modifiers) != 1 || file.Commands[3].Modifiers[0].Name != "export" || file.Commands[3].Function == nil {
		t.Fatalf("exported def = %#v", file.Commands[3])
	}
}

func TestDestructuringDeclarationsAndForLoop(t *testing.T) {
	file := Parse("vim9script\nvar [first: number, second; rest] = values\nfor [key, value] in items\n  echo key value\nendfor\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
	}
	declaration := file.Commands[1].Declaration
	if declaration == nil || len(declaration.Bindings) != 3 || file.Text(declaration.Bindings[0].Name) != "first" || declaration.Bindings[0].ParsedType == nil || !declaration.Bindings[2].Rest {
		t.Fatalf("destructuring declaration = %#v", declaration)
	}
	loop := file.Commands[2].For
	if loop == nil || len(loop.Bindings) != 2 || file.Text(loop.Bindings[1].Name) != "value" || loop.Iterable == nil || loop.Iterable.Value != "items" {
		t.Fatalf("for loop = %#v", loop)
	}
}

func TestOfficialLegacyLetAssignmentTargets(t *testing.T) {
	// v9.2.1015 src/testdir/test_assert.vim and test_changedtick.vim.
	file := (LegacyParser{}).Parse("let s:values[0].name = 'first'\nlet b:[\"changedtick\"] += 1\nlet &l:tabstop = 4\nlet @a = 'text'\nlet $VIMLS_TEST = 'env'\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	want := []ExpressionKind{ExpressionMember, ExpressionIndex, ExpressionIdentifier, ExpressionIdentifier, ExpressionIdentifier}
	for index, kind := range want {
		declaration := file.Commands[index].Declaration
		if declaration == nil || declaration.Target == nil || declaration.Target.Kind != kind || len(file.Commands[index].Expressions) != 1 || file.Commands[index].Expressions[0].Kind != ExpressionAssignment {
			t.Fatalf("assignment %d = %#v", index, file.Commands[index])
		}
	}
}

func TestOfficialVariableMutationCommands(t *testing.T) {
	// v9.2.1015 src/testdir/test_unlet.vim, test_changedtick.vim and
	// test_vim9_assign.vim.
	file := (Vim9Parser{}).Parse("unlet! g:first g:items[0]\nlockvar 2 g:first g:items\nunlockvar g:first\n++counter\n--counter\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, commands = %#v", file.Diagnostics, file.Commands)
	}
	if len(file.Commands[0].Targets) != 2 || file.Commands[0].Targets[1].Kind != ExpressionIndex || file.Text(file.Commands[1].Count) != "2" || len(file.Commands[1].Targets) != 2 || len(file.Commands[2].Targets) != 1 {
		t.Fatalf("variable commands = %#v", file.Commands[:3])
	}
	for _, command := range file.Commands[3:] {
		if len(command.Targets) != 1 || len(command.Expressions) != 1 || command.Expressions[0].Kind != ExpressionUnary {
			t.Fatalf("increment command = %#v", command)
		}
	}
}

func TestOfficialGenericTypedDeclarationEndsAtNewline(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_script.vim
	// Test_break_in_try_in_for.
	file := Parse("vim9script\ndef Values(): list<string>\n  var values: list<string>\n  for value in ['x']\n    values += [value]\n  endfor\n  return values\nenddef\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 8 || file.Commands[2].Declaration.ParsedType.Name != "list" || file.Commands[3].Canonical != "for" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
}

func TestOfficialVim9FoldCommentAfterDeclaration(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_expr.vim Test_expr9_dict.
	file := Parse("vim9script\nvar first: number #{{ fold\nvar second = 9 #{{ fold\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || countTokens(file, TokenComment) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
}

func TestOfficialSingleLetterTypedClassMember(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_class.vim Test_final_object_variable.
	file := Parse("vim9script\nclass A\n  public final l: list<number>\n  def new()\n    this.l = []\n  enddef\nendclass\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 7 || file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "l" || file.Commands[3].Canonical != "def" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[2].Declaration.ParsedType == nil || file.Commands[2].Declaration.ParsedType.Name != "list" {
		t.Fatalf("declaration = %#v", file.Commands[2].Declaration)
	}
}

func TestVim9ClassMemberMissingName(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		code      string
		message   string
		span      string
		wantType  bool
		wantValue bool
	}{
		{name: "typed", source: "vim9script\nclass Something\n  var: number\nendclass\n", code: "vim/E1317", message: "invalid object variable declaration", span: "var: number", wantType: true},
		{name: "typed assignment", source: "vim9script\nclass Something\n  var: number = 42\nendclass\n", code: "vim/E1317", message: "invalid object variable declaration", span: "var: number = 42", wantType: true, wantValue: true},
		{name: "assignment", source: "vim9script\nclass Something\n  var = 42\nendclass\n", code: "vim/E1317", message: "invalid object variable declaration", span: "var = 42", wantValue: true},
		{name: "static typed", source: "vim9script\n\nclass Something\n  static var: number\nendclass\n", code: "vim/E1329", message: "invalid class variable declaration", span: "static var: number", wantType: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source + "var after = 1\n")
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || file.Diagnostics[0].Message != test.message || file.Text(file.Diagnostics[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, commands = %#v", file.Diagnostics, file.Commands)
			}
			if len(file.Commands) != 5 || file.Commands[4].Canonical != "var" || file.Commands[4].Declaration == nil {
				t.Fatalf("commands = %#v", file.Commands)
			}
			declarationCommand := file.Commands[2]
			if declarationCommand.Declaration == nil || declarationCommand.Declaration.Name.Start != declarationCommand.Declaration.Name.End || declarationCommand.Block < 0 || file.Blocks[declarationCommand.Block].Kind != BlockClass {
				t.Fatalf("declaration = %#v, blocks = %#v", declarationCommand, file.Blocks)
			}
			if test.wantType && (declarationCommand.Declaration.ParsedType == nil || file.Text(declarationCommand.Declaration.Type) != "number") {
				t.Fatalf("type = %#v", declarationCommand.Declaration)
			}
			if test.wantValue && (declarationCommand.Declaration.Initializer == nil || declarationCommand.Declaration.Target == nil || declarationCommand.Declaration.Initializer.Value != "42") {
				t.Fatalf("initializer/target = %#v", declarationCommand.Declaration)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestVim9ClassMemberModifierOrder(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		code        string
		message     string
		span        string
		wantName    string
		wantValue   string
		declaration bool
	}{
		{
			name:      "public expression",
			source:    "vim9script\nclass Something\n  public val = 1\n  public var good = 2\nendclass\nvar after = 3\n",
			code:      "vim/E1331",
			message:   `public must be followed by "var" or "static" or "final" or "const"`,
			span:      "public",
			wantName:  "val",
			wantValue: "1",
		},
		{
			name:        "static public",
			source:      "vim9script\nclass Something\n  static public var val = 1\n  public var good = 2\nendclass\nvar after = 3\n",
			code:        "vim/E1368",
			message:     `Static must be followed by "var" or "def" or "final" or "const"`,
			span:        "static",
			wantName:    "val",
			wantValue:   "1",
			declaration: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || file.Diagnostics[0].Message != test.message || file.Text(file.Diagnostics[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, commands = %#v", file.Diagnostics, file.Commands)
			}
			if len(file.Commands) < 4 {
				t.Fatalf("commands = %#v", file.Commands)
			}
			command := file.Commands[2]
			if test.declaration {
				if command.Declaration == nil || file.Text(command.Declaration.Name) != test.wantName || command.Declaration.Initializer == nil || command.Declaration.Initializer.Value != test.wantValue {
					t.Fatalf("declaration = %#v, commands = %#v", command.Declaration, file.Commands)
				}
			} else if len(command.Expressions) != 1 || command.Expressions[0].Kind != ExpressionAssignment || command.Expressions[0].Children[0].Value != test.wantName || command.Expressions[0].Children[1].Value != test.wantValue {
				t.Fatalf("expression = %#v, commands = %#v", command.Expressions, file.Commands)
			}
			last := file.Commands[len(file.Commands)-1]
			if last.Canonical != "var" || last.Declaration == nil {
				t.Fatalf("recovery command = %#v", last)
			}
			assertFileSpans(t, file)
		})
	}
}
