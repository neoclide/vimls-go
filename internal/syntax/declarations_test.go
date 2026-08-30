package syntax

import "testing"

func TestVim9ReservedDeclarationNames(t *testing.T) {
	file := Parse("vim9script\nvar true = 1\nvar false = 2\nvar null = 3\nvar this = 4\nvar super = 5\nvar after = 6\n")
	if len(file.Diagnostics) != 5 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	for index, name := range []string{"true", "false", "null", "this", "super"} {
		diagnostic := file.Diagnostics[index]
		if diagnostic.Code != "vim/E1034" || diagnostic.Message != "Cannot use reserved name "+name || file.Text(diagnostic.Span) != name {
			t.Fatalf("diagnostic %d = %#v, span=%q", index, diagnostic, file.Text(diagnostic.Span))
		}
	}
	if file.Commands[len(file.Commands)-1].Declaration == nil || file.Text(file.Commands[len(file.Commands)-1].Declaration.Name) != "after" {
		t.Fatalf("following declaration = %#v", file.Commands[len(file.Commands)-1])
	}
}

func TestVim9OptionDeclaration(t *testing.T) {
	file := Parse("vim9script\nvar &tabstop = 4\nvar after = 6\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1052" || file.Diagnostics[0].Message != "Cannot declare an option: &tabstop" || file.Text(file.Diagnostics[0].Span) != "&tabstop" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if file.Commands[1].Declaration == nil || file.Text(file.Commands[1].Declaration.Name) != "&tabstop" || file.Commands[1].Declaration.Initializer == nil || file.Commands[2].Declaration == nil {
		t.Fatalf("declarations = %#v", file.Commands)
	}
}

func TestCannotLockOptionDeclaration(t *testing.T) {
	for _, test := range []struct {
		name, source, span string
	}{
		{
			name:   "vim9 final without assignment",
			source: "vim9script\ndef F()\n  final &option\nenddef\n",
			span:   "&option",
		},
		{
			name:   "vim9 const assignment",
			source: "vim9script\nconst &filetype = 'vim'\n",
			span:   "&filetype",
		},
		{
			name:   "legacy const assignment",
			source: "const &filetype = 'vim'\n",
			span:   "&filetype",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E996" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1125" {
					t.Fatalf("option lock retained E1125: %#v", file.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "Cannot lock an option" || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one E996 on %q; all diagnostics = %#v", got, test.span, file.Diagnostics)
			}
		})
	}

	file := Parse("vim9script\ndef F()\n  final name\nenddef\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1125" {
		t.Fatalf("ordinary final diagnostics = %#v", file.Diagnostics)
	}

	file = Parse("final &filetype = 'vim'\n")
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code == "vim/E996" {
			t.Fatalf("Legacy cross-dialect final diagnostics = %#v", file.Diagnostics)
		}
	}
}

func TestVim9DeclarationDiagnosticGuards(t *testing.T) {
	file := Parse("vim9script\nvar truthful = 1\nconst nullValue = 2\n&tabstop = 4\nfinal &option\nlegacy let true = 1\n")
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code == "vim/E1034" || diagnostic.Code == "vim/E1052" {
			t.Fatalf("ordinary identifier, option assignment, or legacy declaration diagnostic = %#v", diagnostic)
		}
	}

	scoped := Parse("vim9script\nvar &g:tabstop = 4\n")
	if len(scoped.Diagnostics) != 1 || scoped.Diagnostics[0].Code != "vim/E1052" || scoped.Diagnostics[0].Message != "Cannot declare an option: &g:tabstop" || scoped.Text(scoped.Diagnostics[0].Span) != "&g:tabstop" {
		t.Fatalf("scoped option diagnostics = %#v", scoped.Diagnostics)
	}
}

func TestVim9RegisterDeclarationPreservesTargetAndInitializer(t *testing.T) {
	for _, test := range []struct {
		name, source, code string
	}{
		{"def", "def F()\n  var @. = 5\n  var after = 6\nenddef\n", "vim/E354"},
		{"script", "vim9script\nvar @% = 5\nvar after = 6\n", "vim/E1066"},
		{"script nested", "vim9script\nif true\n  var @. = 5\nendif\n", "vim/E1066"},
		{"def writable", "def F()\n  var @a = 5\nenddef\n", "vim/E1066"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			var declarationCommand *Command
			for index := range file.Commands {
				if file.Commands[index].Canonical == "var" {
					declarationCommand = &file.Commands[index]
					break
				}
			}
			if declarationCommand == nil || declarationCommand.Declaration == nil || declarationCommand.Declaration.Target == nil || declarationCommand.Declaration.Initializer == nil {
				t.Fatalf("declaration = %#v", declarationCommand)
			}
			if file.Text(declarationCommand.Declaration.Target.Span) != file.Text(declarationCommand.Declaration.Name) || file.Text(declarationCommand.Declaration.Target.Span)[:1] != "@" {
				t.Fatalf("target/name = %q/%q", file.Text(declarationCommand.Declaration.Target.Span), file.Text(declarationCommand.Declaration.Name))
			}
			if declarationCommand.Declaration.Initializer.Kind != ExpressionNumber || declarationCommand.Declaration.Initializer.Value != "5" {
				t.Fatalf("initializer = %#v", declarationCommand.Declaration.Initializer)
			}
			if file.Text(file.Diagnostics[0].Span) != file.Text(declarationCommand.Declaration.Name)[1:] {
				t.Fatalf("diagnostic span = %q", file.Text(file.Diagnostics[0].Span))
			}
		})
	}
}

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

func TestVim9InterfaceInsideDefReportsE1436(t *testing.T) {
	source := "vim9script\ndef Fn()\n  var x = 1\n  interface Foo\n  endinterface\nenddef\nvar after = 1\n"
	file := Parse(source)
	var got []Diagnostic
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code == "vim/E1436" {
			got = append(got, diagnostic)
		}
	}
	if len(got) != 1 || got[0].Message != "Interface can only be used in a script" || file.Text(got[0].Span) != "interface" {
		t.Fatalf("E1436 diagnostics = %#v; all diagnostics = %#v", got, file.Diagnostics)
	}
	if len(file.Blocks) != 2 || file.Blocks[0].Kind != BlockDef || file.Blocks[1].Kind != BlockInterface || file.Blocks[1].Parent != 0 {
		t.Fatalf("blocks = %#v", file.Blocks)
	}
	if file.Commands[len(file.Commands)-1].Declaration == nil || file.Text(file.Commands[len(file.Commands)-1].Declaration.Name) != "after" {
		t.Fatalf("following declaration = %#v", file.Commands[len(file.Commands)-1])
	}
	assertFileSpans(t, file)

	topLevel := Parse("vim9script\ninterface Foo\nendinterface\n")
	for _, diagnostic := range topLevel.Diagnostics {
		if diagnostic.Code == "vim/E1436" {
			t.Fatalf("top-level interface reported E1436: %#v", diagnostic)
		}
	}
}

func TestVim9EnumInsideDefReportsE1435(t *testing.T) {
	source := "vim9script\ndef Fn()\n  var x = 1\n  enum Foo\n    Red\n  endenum\nenddef\nvar after = 1\n"
	file := Parse(source)
	var got []Diagnostic
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code == "vim/E1435" {
			got = append(got, diagnostic)
		}
	}
	if len(got) != 1 || got[0].Message != "Enum can only be used in a script" || file.Text(got[0].Span) != "enum" {
		t.Fatalf("E1435 diagnostics = %#v; all diagnostics = %#v", got, file.Diagnostics)
	}
	if len(file.Blocks) != 2 || file.Blocks[0].Kind != BlockDef || file.Blocks[1].Kind != BlockEnum || file.Blocks[1].Parent != 0 {
		t.Fatalf("blocks = %#v", file.Blocks)
	}
	if file.Commands[len(file.Commands)-1].Declaration == nil || file.Text(file.Commands[len(file.Commands)-1].Declaration.Name) != "after" {
		t.Fatalf("following declaration = %#v", file.Commands[len(file.Commands)-1])
	}
	assertFileSpans(t, file)

	topLevel := Parse("vim9script\nenum Foo\n  Red\nendenum\n")
	for _, diagnostic := range topLevel.Diagnostics {
		if diagnostic.Code == "vim/E1435" {
			t.Fatalf("top-level enum reported E1435: %#v", diagnostic)
		}
	}
}

func TestVim9ClassInsideDefReportsE1429(t *testing.T) {
	source := "vim9script\ndef Fn()\n  class Foo\n  endclass\nenddef\nvar after = 1\n"
	file := Parse(source)
	var got []Diagnostic
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code == "vim/E1429" {
			got = append(got, diagnostic)
		}
	}
	if len(got) != 1 || got[0].Message != "Class can only be used in a script" || file.Text(got[0].Span) != "class" {
		t.Fatalf("E1429 diagnostics = %#v; all diagnostics = %#v", got, file.Diagnostics)
	}
	if len(file.Blocks) != 2 || file.Blocks[0].Kind != BlockDef || file.Blocks[1].Kind != BlockClass || file.Blocks[1].Parent != 0 {
		t.Fatalf("blocks = %#v", file.Blocks)
	}
	if file.Commands[len(file.Commands)-1].Declaration == nil || file.Text(file.Commands[len(file.Commands)-1].Declaration.Name) != "after" {
		t.Fatalf("following declaration = %#v", file.Commands[len(file.Commands)-1])
	}
	assertFileSpans(t, file)

	topLevel := Parse("vim9script\nclass Foo\nendclass\n")
	for _, diagnostic := range topLevel.Diagnostics {
		if diagnostic.Code == "vim/E1429" {
			t.Fatalf("top-level class reported E1429: %#v", diagnostic)
		}
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
		message   string
		wantAlias bool
		wantName  string
		wantType  TypeKind
	}{
		{name: "name", source: "vim9script\ntype\nvar after = 1\n", code: "vim/E1397", message: "Missing type alias name"},
		{name: "type", source: "vim9script\ntype MyType =\nvar after = 1\n", code: "vim/E1398", message: "Missing type alias type", wantAlias: true, wantName: "MyType", wantType: TypeMissing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || file.Diagnostics[0].Message != test.message {
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
			if test.code == "vim/E1397" && file.Text(file.Diagnostics[0].Span) != "type" {
				t.Fatalf("name diagnostic span = %#v", file.Diagnostics[0])
			}
			if test.code == "vim/E1398" && (alias == nil || file.Diagnostics[0].Span.Start != file.Diagnostics[0].Span.End || file.Diagnostics[0].Span.Start != alias.Assignment.End) {
				t.Fatalf("type diagnostic span = %#v, alias = %#v", file.Diagnostics[0], alias)
			}
			assertFileSpans(t, file)
		})
	}
	for _, source := range []string{"type\nlet after = 1\n", "type MyType = number\nlet after = 1\n"} {
		for _, diagnostic := range Parse(source).Diagnostics {
			if diagnostic.Code == "vim/E1397" || diagnostic.Code == "vim/E1398" {
				t.Fatalf("legacy source reported Vim9 type-alias diagnostic: %#v\n%s", diagnostic, source)
			}
		}
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

func TestVim9ImportInvalidAliasDiagnostics(t *testing.T) {
	for _, alias := range []string{"9foo", "the#foo", "g:foo"} {
		t.Run(alias, func(t *testing.T) {
			file := Parse("vim9script\nimport './module.vim' as " + alias + "\nvar after = 1\n")
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1047" || file.Diagnostics[0].Message != "Syntax error in import: "+alias || file.Text(file.Diagnostics[0].Span) != alias {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			if len(file.Commands) != 3 || file.Commands[1].Import == nil || file.Commands[2].Declaration == nil {
				t.Fatalf("commands = %#v", file.Commands)
			}
			assertFileSpans(t, file)
		})
	}

	valid := Parse("vim9script\nimport './module.vim' as module_9\nvar after = 1\n")
	if len(valid.Diagnostics) != 0 {
		t.Fatalf("valid import diagnostics = %#v", valid.Diagnostics)
	}
	legacy := (LegacyParser{}).Parse("import './module.vim' as 9foo\nlet g:after = 1\n")
	if hasDiagnostic(legacy, "vim/E1047") {
		t.Fatalf("legacy import diagnostics = %#v", legacy.Diagnostics)
	}
}

func TestVim9ImportPathDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, code, message, path string
	}{
		{"empty import string", "vim9script\nimport \"\" as abc\nvar after = 1\n", "vim/E1071", `Invalid string for :import: "" as abc`, `""`},
		{"non string import value", "vim9script\nimport [] as abc\nvar after = 1\n", "vim/E1071", `Invalid string for :import: [] as abc`, `[]`},
		{"null import string", "vim9script\nimport test_null_string() as abc\nvar after = 1\n", "vim/E1071", `Invalid string for :import: test_null_string() as abc`, `test_null_string()`},
		{"vim script requires alias", "vim9script\nimport './Ximport/.vim'\nvar after = 1\n", "vim/E1261", `Cannot import .vim without using "as"`, "'./Ximport/.vim'"},
		{"non vim script requires alias or suffix", "vim9script\nimport './module'\nvar after = 1\n", "vim/E1257", `Imported script must use "as" or end in .vim: module`, "'./module'"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || file.Diagnostics[0].Message != test.message || file.Text(file.Diagnostics[0].Span) != test.path {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			if len(file.Commands) != 3 || file.Commands[1].Import == nil || file.Commands[2].Declaration == nil {
				t.Fatalf("recovery = %#v", file.Commands)
			}
			assertFileSpans(t, file)
		})
	}
	for _, source := range []string{
		"import './module.vim'\nlet after = 1\n",
		"import [] as abc\nlet after = 1\n",
		"vim9script\nlegacy import './module.vim'\nvar after = 1\n",
		"vim9script\nimport './module.vim' as module\nvar after = 1\n",
		"vim9script\nimport dynamic_path as module\nvar after = 1\n",
	} {
		file := Parse(source)
		if hasDiagnostic(file, "vim/E1071") || hasDiagnostic(file, "vim/E1257") || hasDiagnostic(file, "vim/E1261") {
			t.Fatalf("unexpected import diagnostic = %#v", file.Diagnostics)
		}
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

func TestVim9InterfaceMemberInitializer(t *testing.T) {
	t.Run("initializer", func(t *testing.T) {
		file := Parse("vim9script\ninterface SomethingWrong\n  var value: string\n  var count = 7\n  def GetCount(): number\nendinterface\nvar after = 1\n")
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1344" || file.Diagnostics[0].Message != "Cannot initialize a variable in an interface" || file.Text(file.Diagnostics[0].Span) != "var count = 7" {
			t.Fatalf("diagnostics = %#v", file.Diagnostics)
		}
		if len(file.Commands) != 7 || len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockInterface || file.Blocks[0].Header != 1 || file.Blocks[0].End != 5 {
			t.Fatalf("commands = %#v, blocks = %#v", file.Commands, file.Blocks)
		}
		declaration := file.Commands[3].Declaration
		if declaration == nil || file.Text(declaration.Name) != "count" || file.Text(declaration.Assignment) != "=" || declaration.Target == nil || file.Text(declaration.Target.Span) != "count" || declaration.Initializer == nil || declaration.Initializer.Value != "7" || file.Commands[3].Block != 0 {
			t.Fatalf("declaration = %#v, command = %#v", declaration, file.Commands[3])
		}
		if file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "value" || file.Commands[4].Canonical != "def" || file.Commands[4].Function == nil || file.Commands[4].Block != 0 || file.Commands[5].Canonical != "endinterface" || file.Commands[5].Block != 0 {
			t.Fatalf("interface members = %#v", file.Commands)
		}
		if file.Commands[6].Canonical != "var" || file.Commands[6].Declaration == nil || file.Text(file.Commands[6].Declaration.Name) != "after" || file.Commands[6].Block != -1 {
			t.Fatalf("recovery command = %#v", file.Commands[6])
		}
		assertFileSpans(t, file)
	})

	t.Run("incomplete initializer", func(t *testing.T) {
		file := Parse("vim9script\ninterface Broken\n  var count =\nendinterface\nvar after = 1\n")
		if !hasDiagnostic(file, "vim/E1344") || len(file.Commands) != 5 || file.Commands[2].Declaration == nil || file.Commands[2].Declaration.Assignment.Start >= file.Commands[2].Declaration.Assignment.End || file.Commands[2].Declaration.Initializer == nil || file.Commands[4].Declaration == nil {
			t.Fatalf("diagnostics = %#v, commands = %#v", file.Diagnostics, file.Commands)
		}
		assertFileSpans(t, file)
	})

	t.Run("valid interface and class", func(t *testing.T) {
		file := Parse("vim9script\ninterface Good\n  var value: string\nendinterface\nclass GoodClass\n  var count = 7\nendclass\n")
		if len(file.Diagnostics) != 0 || len(file.Commands) != 7 {
			t.Fatalf("diagnostics = %#v, commands = %#v", file.Diagnostics, file.Commands)
		}
		assertFileSpans(t, file)
	})

	t.Run("legacy interface", func(t *testing.T) {
		file := Parse("interface Legacy\n  var count = 7\nendinterface\n")
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1342" || file.Text(file.Diagnostics[0].Span) != "interface" {
			t.Fatalf("diagnostics = %#v", file.Diagnostics)
		}
		assertFileSpans(t, file)
	})
}

func TestVim9InterfaceMethodBodyRecovery(t *testing.T) {
	file := Parse("vim9script\ninterface SomethingWrong\n  def GetCount(): number\n    return 5\n  enddef\nendinterface\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1345" || file.Diagnostics[0].Message != "Not a valid command in an interface: return 5" || file.Text(file.Diagnostics[0].Span) != "return 5" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 7 || len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockInterface || file.Blocks[0].End != 5 || file.Commands[2].Function == nil || file.Commands[3].Canonical != "return" || file.Commands[4].Canonical != "enddef" || file.Commands[5].Canonical != "endinterface" || file.Commands[6].Declaration == nil || file.Text(file.Commands[6].Declaration.Name) != "after" {
		t.Fatalf("commands = %#v blocks = %#v", file.Commands, file.Blocks)
	}
	assertFileSpans(t, file)

	valid := Parse("vim9script\ninterface Good\n  def First(): number\n  var value: string\n  def Second(): number\nendinterface\nvar after = 1\n")
	if len(valid.Diagnostics) != 0 || len(valid.Blocks) != 1 || len(valid.Commands[1].Aggregate.Members) != 3 || valid.Commands[6].Declaration == nil {
		t.Fatalf("valid interface = %#v, diagnostics = %#v", valid.Commands, valid.Diagnostics)
	}
	assertFileSpans(t, valid)
}

func TestVim9EnumEndTrailingCharacters(t *testing.T) {
	t.Run("trailing payload", func(t *testing.T) {
		file := Parse("vim9script\nenum Something\nendenum school's out\nvar after = 1\n")
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E488" || file.Diagnostics[0].Message != "trailing characters" || file.Text(file.Diagnostics[0].Span) != "school's out" {
			t.Fatalf("diagnostics = %#v", file.Diagnostics)
		}
		if len(file.Commands) != 4 || len(file.Blocks) != 1 || file.Commands[1].Aggregate == nil || file.Text(file.Commands[1].Aggregate.Name) != "Something" || file.Blocks[0].Kind != BlockEnum || file.Blocks[0].Header != 1 || file.Blocks[0].End != 2 || file.Commands[2].Canonical != "endenum" || file.Commands[2].Block != 0 {
			t.Fatalf("commands = %#v, blocks = %#v", file.Commands, file.Blocks)
		}
		if file.Commands[3].Declaration == nil || file.Text(file.Commands[3].Declaration.Name) != "after" {
			t.Fatalf("recovery command = %#v", file.Commands[3])
		}
		assertFileSpans(t, file)
	})

	t.Run("guards", func(t *testing.T) {
		valid := Parse("vim9script\nenum Good\nendenum\n")
		if len(valid.Diagnostics) != 0 {
			t.Fatalf("valid endenum diagnostics = %#v", valid.Diagnostics)
		}
		legacy := Parse("endenum school's out\n")
		stray := Parse("vim9script\nendenum extra\n")
		if len(legacy.Diagnostics) != 1 || hasDiagnostic(legacy, "vim/E488") || len(stray.Diagnostics) != 1 || hasDiagnostic(stray, "vim/E488") {
			t.Fatalf("legacy diagnostics = %#v, Vim9 stray diagnostics = %#v", legacy.Diagnostics, stray.Diagnostics)
		}
		assertFileSpans(t, valid)
		assertFileSpans(t, legacy)
		assertFileSpans(t, stray)
	})
}

func TestVim9MissingEndenumReportsE1420(t *testing.T) {
	for name, source := range map[string]string{
		"extra suffix": "vim9script\nenum Something\nendenums\n",
		"wrong case":   "vim9script\nenum Something\nEndenum\n",
	} {
		t.Run(name, func(t *testing.T) {
			file := Parse(source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1420" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Missing :endenum" || file.Text(got[0].Span) != "enum" {
				t.Fatalf("E1420 diagnostics = %#v; all diagnostics = %#v", got, file.Diagnostics)
			}
			if len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockEnum || file.Blocks[0].End != -1 || file.Commands[1].Aggregate == nil || file.Text(file.Commands[1].Aggregate.Name) != "Something" {
				t.Fatalf("commands = %#v, blocks = %#v", file.Commands, file.Blocks)
			}
			assertFileSpans(t, file)
		})
	}

	valid := Parse("vim9script\nenum Something\nendenum\n")
	if hasDiagnostic(valid, "vim/E1420") {
		t.Fatalf("valid enum diagnostics = %#v", valid.Diagnostics)
	}
}

func TestVim9InvalidEnumBodyCommandReportsE1419(t *testing.T) {
	for name, source := range map[string]string{
		"missing comma": "vim9script\nenum Planet\n  mercury\n  venus\nendenum\n",
		"leading comma": "vim9script\nenum Planet\n  mercury\n  ,venus\nendenum\n",
		"after comment": "vim9script\nenum Planet\n  mercury\n\n  # Venus\n  venus\nendenum\n",
	} {
		t.Run(name, func(t *testing.T) {
			file := Parse(source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1419" {
					got = append(got, diagnostic)
				}
			}
			want := "venus"
			if name == "leading comma" {
				want = ",venus"
			}
			if len(got) != 1 || got[0].Message != "Not a valid command in an Enum: "+want || file.Text(got[0].Span) != want {
				t.Fatalf("E1419 diagnostics = %#v; all diagnostics = %#v", got, file.Diagnostics)
			}
			if len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockEnum || file.Blocks[0].End < 0 || file.Commands[file.Blocks[0].End].Canonical != "endenum" {
				t.Fatalf("commands = %#v, blocks = %#v", file.Commands, file.Blocks)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestVim9InvalidEnumValueReportsE1418(t *testing.T) {
	for name, source := range map[string]string{
		"invalid token": "vim9script\nenum Fruit\n  Apple,\n  $%@\nendenum\n",
		"bar command":   "vim9script\nenum Fruit\n  One, | var y = 10\nendenum\n",
	} {
		t.Run(name, func(t *testing.T) {
			file := Parse(source)
			want := "$%@"
			valueName := "Apple"
			if name == "bar command" {
				want = "| var y = 10"
				valueName = "One"
			}
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1418" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Invalid enum value declaration: "+want || file.Text(got[0].Span) != want {
				t.Fatalf("E1418 diagnostics = %#v; all diagnostics = %#v", got, file.Diagnostics)
			}
			if len(file.Commands) < 3 || len(file.Commands[2].EnumValues) != 1 || file.Text(file.Commands[2].EnumValues[0].Name) != valueName {
				t.Fatalf("commands = %#v", file.Commands)
			}
			if len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockEnum || file.Blocks[0].End < 0 || file.Commands[file.Blocks[0].End].Canonical != "endenum" {
				t.Fatalf("commands = %#v, blocks = %#v", file.Commands, file.Blocks)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestVim9AbstractEnumMethodReportsE1417(t *testing.T) {
	file := Parse("vim9script\nenum Foo\n  Apple\n  abstract def Bar()\nendenum\n")
	var got []Diagnostic
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code == "vim/E1417" {
			got = append(got, diagnostic)
		}
	}
	if len(got) != 1 || got[0].Message != "Abstract cannot be used in an Enum" || file.Text(got[0].Span) != "abstract" {
		t.Fatalf("E1417 diagnostics = %#v; all diagnostics = %#v", got, file.Diagnostics)
	}
	if len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockEnum || file.Blocks[0].End < 0 || file.Commands[file.Blocks[0].End].Canonical != "endenum" {
		t.Fatalf("commands = %#v, blocks = %#v", file.Commands, file.Blocks)
	}
	assertFileSpans(t, file)

	for name, source := range map[string]string{
		"value named abstract": "vim9script\nenum Foo\n  abstract\nendenum\n",
		"concrete method":      "vim9script\nenum Foo\n  Apple\n  def Bar()\n  enddef\nendenum\n",
	} {
		t.Run(name, func(t *testing.T) {
			valid := Parse(source)
			if hasDiagnostic(valid, "vim/E1417") {
				t.Fatalf("diagnostics = %#v", valid.Diagnostics)
			}
		})
	}
}

func TestVim9E1016ExplicitScopedDeclarations(t *testing.T) {
	source := "vim9script\n" +
		"var $ENV = 1\n" +
		"var g:global = 2\n" +
		"var w:window = 3\n" +
		"var b:buffer = 4\n" +
		"var t:tab = 5\n" +
		"var v:version = 6\n" +
		"var after = 7\n"
	file := Parse(source)
	if len(file.Diagnostics) != 6 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	want := []struct {
		name, message string
	}{
		{"$ENV", "Cannot declare an environment variable: $ENV"},
		{"g:global", "Cannot declare a global variable: g:global"},
		{"w:window", "Cannot declare a window variable: w:window"},
		{"b:buffer", "Cannot declare a buffer variable: b:buffer"},
		{"t:tab", "Cannot declare a tab variable: t:tab"},
		{"v:version", "Cannot declare a v: variable: v:version"},
	}
	for index, test := range want {
		diagnostic := file.Diagnostics[index]
		if diagnostic.Code != "vim/E1016" || diagnostic.Message != test.message || file.Text(diagnostic.Span) != test.name {
			t.Fatalf("diagnostic %d = %#v, want E1016 %q on %q", index, diagnostic, test.message, test.name)
		}
		command := file.Commands[index+1]
		if command.Declaration == nil || file.Text(command.Declaration.Name) != test.name || command.Declaration.Target == nil || command.Declaration.Initializer == nil {
			t.Fatalf("declaration %d = %#v", index, command.Declaration)
		}
	}
	if file.Commands[7].Canonical != "var" || file.Commands[7].Declaration == nil || file.Text(file.Commands[7].Declaration.Name) != "after" {
		t.Fatalf("following command = %#v", file.Commands[7])
	}
	assertFileSpans(t, file)
}

func TestVim9E1016TypedScopedRecovery(t *testing.T) {
	source := "def Build()\n" +
		"  w:width: number = 10\n" +
		"  t:tab: bool = true\n" +
		"  b:name: string = 'x'\n" +
		"  g:count: number = 1\n" +
		"  var after = 2\n" +
		"enddef\n"
	file := Parse(source)
	if len(file.Diagnostics) != 4 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	for index, test := range []struct {
		name, message string
	}{
		{"w:width", "Cannot declare a window variable: w:width"},
		{"t:tab", "Cannot declare a tab variable: t:tab"},
		{"b:name", "Cannot declare a buffer variable: b:name"},
		{"g:count", "Cannot declare a global variable: g:count"},
	} {
		diagnostic := file.Diagnostics[index]
		if diagnostic.Code != "vim/E1016" || diagnostic.Message != test.message || file.Text(diagnostic.Span) != test.name {
			t.Fatalf("diagnostic %d = %#v, want E1016 %q on %q", index, diagnostic, test.message, test.name)
		}
		command := file.Commands[index+1]
		if command.Declaration == nil || file.Text(command.Declaration.Name) != test.name || command.Declaration.ParsedType == nil || command.Declaration.Target == nil || command.Declaration.Initializer == nil {
			t.Fatalf("typed declaration %d = %#v", index, command.Declaration)
		}
	}
	if file.Commands[5].Canonical != "var" || file.Commands[5].Declaration == nil || file.Text(file.Commands[5].Declaration.Name) != "after" {
		t.Fatalf("following command = %#v", file.Commands[5])
	}
	assertFileSpans(t, file)
}

func TestVim9E1016ScopedExpressionRecovery(t *testing.T) {
	source := "def Build()\n" +
		"  g:notexist:cmd\n" +
		"  var after = 2\n" +
		"enddef\n"
	file := Parse(source)
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1016" || file.Diagnostics[0].Message != "Cannot declare a global variable: g:notexist" || file.Text(file.Diagnostics[0].Span) != "g:notexist" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 4 || len(file.Commands[1].Expressions) != 1 || file.Commands[1].Expressions[0].Kind != ExpressionIdentifier || file.Commands[1].Expressions[0].Value != "g:notexist" {
		t.Fatalf("expression recovery = %#v", file.Commands)
	}
	if file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "after" {
		t.Fatalf("following command = %#v", file.Commands[2])
	}
	assertFileSpans(t, file)
}

func TestVim9E1016ScopeGuards(t *testing.T) {
	valid := Parse("vim9script\ng:foo = 1\nvar after = 2\n")
	if len(valid.Diagnostics) != 0 || len(valid.Commands) != 3 || len(valid.Commands[2].Expressions) != 1 {
		t.Fatalf("valid scoped assignment = %#v", valid)
	}
	scriptTyped := Parse("vim9script\nw:foo: number = 1\nvar after = 2\n")
	if hasDiagnostic(scriptTyped, "vim/E1016") || len(scriptTyped.Commands) != 3 || scriptTyped.Commands[1].Declaration == nil || scriptTyped.Commands[2].Declaration == nil {
		t.Fatalf("script typed scope = %#v", scriptTyped)
	}
	legacy := (LegacyParser{}).Parse("var g:foo = 1\nvar after = 2\n")
	if hasDiagnostic(legacy, "vim/E1016") || len(legacy.Commands) != 2 || legacy.Commands[1].Declaration == nil {
		t.Fatalf("legacy scoped declaration = %#v", legacy)
	}
	rootEnvironment := Parse("vim9script\nvar $ENV: number\nvar after = 2\n")
	if hasDiagnostic(rootEnvironment, "vim/E1016") || !hasDiagnostic(rootEnvironment, "vim/E475") || rootEnvironment.Commands[1].Declaration == nil || rootEnvironment.Commands[2].Declaration == nil {
		t.Fatalf("root environment declaration = %#v", rootEnvironment)
	}
	constants := Parse("vim9script\nconst g:GLOBAL = 1\nfinal w:WINDOW = 2\n")
	if hasDiagnostic(constants, "vim/E1016") {
		t.Fatalf("scoped constants = %#v", constants.Diagnostics)
	}
	assertFileSpans(t, valid)
	assertFileSpans(t, scriptTyped)
	assertFileSpans(t, legacy)
	assertFileSpans(t, rootEnvironment)
	assertFileSpans(t, constants)
}

func TestVim9E1020CompoundDeclarationAssignment(t *testing.T) {
	tests := []struct {
		name, source string
	}{
		{
			name:   "def",
			source: "def Build()\n  var xnr += 4\n  var after = 2\nenddef\n",
		},
		{
			name:   "script",
			source: "vim9script\nvar xnr += 4\nvar after = 2\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1020" || file.Diagnostics[0].Message != "Cannot use an operator on a new variable: xnr" || file.Text(file.Diagnostics[0].Span) != "xnr" {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			var declarationCommand *Command
			for index := range file.Commands {
				if file.Commands[index].Canonical == "var" {
					declarationCommand = &file.Commands[index]
					break
				}
			}
			if declarationCommand == nil || declarationCommand.Declaration == nil || declarationCommand.Declaration.Target == nil || declarationCommand.Declaration.Initializer == nil {
				t.Fatalf("declaration = %#v", declarationCommand)
			}
			declaration := declarationCommand.Declaration
			if file.Text(declaration.Assignment) != "+=" || file.Text(declaration.Target.Span) != "xnr" || declaration.Initializer.Value != "4" {
				t.Fatalf("declaration = %#v", declaration)
			}
			recovered := false
			for _, command := range file.Commands {
				if command.Canonical == "var" && command.Declaration != nil && file.Text(command.Declaration.Name) == "after" {
					recovered = true
					break
				}
			}
			if !recovered {
				t.Fatalf("recovery commands = %#v", file.Commands)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestCompoundAssignmentDoesNotReportE1020ForExistingOrLegacyVariables(t *testing.T) {
	vim9 := Parse("vim9script\nvar x = 1\nx += 4\nvar after = 2\n")
	if hasDiagnostic(vim9, "vim/E1020") || len(vim9.Diagnostics) != 0 || len(vim9.Commands) != 4 {
		t.Fatalf("existing-variable assignment = %#v", vim9)
	}
	legacy := (LegacyParser{}).Parse("let x = 1\nlet x += 4\nlet after = 2\n")
	if hasDiagnostic(legacy, "vim/E1020") || len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 3 {
		t.Fatalf("legacy assignment = %#v", legacy)
	}
	assertFileSpans(t, vim9)
	assertFileSpans(t, legacy)
}
