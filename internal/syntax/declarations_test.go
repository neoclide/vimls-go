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

func TestOfficialVimParserDuplicateImplements(t *testing.T) {
	file := Parse("vim9script\ninterface I\nendinterface\nclass C implements I implements I\nendclass\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1350" || file.Diagnostics[0].Message != `Duplicate "implements"` || file.Text(file.Diagnostics[0].Span) != "implements" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	var class *Command
	for index := range file.Commands {
		if file.Commands[index].Aggregate != nil && file.Text(file.Commands[index].Aggregate.Name) == "C" {
			class = &file.Commands[index]
			break
		}
	}
	if class == nil || class.Aggregate == nil || file.Text(class.Aggregate.Name) != "C" || len(class.Aggregate.Implements) != 1 || file.Text(class.Aggregate.Implements[0]) != "I" {
		t.Fatalf("class = %#v", class)
	}
	if class.Block < 0 || file.Blocks[class.Block].Kind != BlockClass || file.Blocks[class.Block].End < 0 {
		t.Fatalf("commands/blocks = %#v", file.Commands)
	}
	following := -1
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Declaration != nil && file.Text(command.Declaration.Name) == "after" {
			following = index
			break
		}
	}
	if following < 0 || file.Commands[following].Canonical != "var" || file.Text(file.Commands[following].Declaration.Name) != "after" {
		t.Fatalf("following declaration = %#v", file.Commands)
	}
	assertFileSpans(t, file)

	missing := Parse("vim9script\nclass C implements I implements\nendclass\n")
	if len(missing.Diagnostics) != 1 || missing.Diagnostics[0].Code != "vim/E1350" || missing.Text(missing.Diagnostics[0].Span) != "implements" || hasDiagnostic(missing, "vim/E1389") {
		t.Fatalf("missing-name diagnostics = %#v", missing.Diagnostics)
	}

	for _, test := range []struct {
		name, source, owner string
	}{
		{"single clause", "vim9script\nclass C implements I\nendclass\n", ""},
		{"duplicate list entry", "vim9script\nclass C implements I, I\nendclass\n", ""},
		{"interface owns implements", "vim9script\ninterface C implements I\nendinterface\n", "vim/E1381"},
		{"Legacy root", "class C implements I implements I\nendclass\n", "vim/E1316"},
		{"one-shot Legacy", "vim9script\nlegacy class C implements I implements I\nendclass\n", "vim/E1316"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed := Parse(test.source)
			if hasDiagnostic(parsed, "vim/E1350") {
				t.Fatalf("guard unexpectedly received E1350: %#v", parsed.Diagnostics)
			}
			if test.owner != "" && !hasDiagnostic(parsed, test.owner) {
				t.Fatalf("guard diagnostics = %#v, want %s", parsed.Diagnostics, test.owner)
			}
		})
	}
}

func TestOfficialVimParserDuplicateExtends(t *testing.T) {
	file := Parse("vim9script\nclass C extends Base extends Base\nendclass\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1352" || file.Diagnostics[0].Message != `Duplicate "extends"` || file.Text(file.Diagnostics[0].Span) != "extends" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	var class *Aggregate
	for index := range file.Commands {
		if file.Commands[index].Aggregate != nil && file.Text(file.Commands[index].Aggregate.Name) == "C" {
			class = file.Commands[index].Aggregate
			break
		}
	}
	if class == nil || class.Kind != BlockClass || len(class.Extends) != 1 || file.Text(class.Extends[0]) != "Base" {
		t.Fatalf("class = %#v", file.Commands)
	}
	var following *Command
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Declaration != nil && file.Text(command.Declaration.Name) == "after" {
			following = command
			break
		}
	}
	if following == nil || following.Canonical != "var" || file.Text(following.Declaration.Name) != "after" {
		t.Fatalf("following declaration = %#v", file.Commands)
	}
	assertFileSpans(t, file)

	file = Parse("vim9script\nclass C extends Base extends\nendclass\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1352" || file.Text(file.Diagnostics[0].Span) != "extends" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}

	file = Parse("vim9script\nclass C extends Base\"\nendclass\n")
	if !hasDiagnostic(file, "vim/E1315") || hasDiagnostic(file, "vim/E1352") {
		t.Fatalf("malformed extends diagnostics = %#v", file.Diagnostics)
	}

	for _, test := range []struct {
		name, source, owner, message, span string
	}{
		{"single extends", "vim9script\nclass C extends Base\nendclass\n", "", "", ""},
		{"second implements", "vim9script\nclass C extends Base implements I implements J\nendclass\n", "vim/E1350", "", ""},
		{"legacy root", "class C extends Base extends Base\nendclass\n", "vim/E1316", "", ""},
		{"one-shot legacy", "vim9script\nlegacy class C extends Base extends Base\nendclass\n", "vim/E1316", "", ""},
		{"interface duplicate extends", "vim9script\ninterface I extends A extends A\nendinterface\n", "vim/E1352", `Duplicate "extends"`, "extends"},
		{"enum first extends", "vim9script\nenum C extends A\n  Value\nendenum\n", "vim/E1416", `Enum cannot extend a class or enum`, "extends"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed := Parse(test.source)
			if test.owner == "" {
				if hasDiagnostic(parsed, "vim/E1352") {
					t.Fatalf("guard unexpectedly received E1352: %#v", parsed.Diagnostics)
				}
				return
			}
			if !hasDiagnostic(parsed, test.owner) {
				t.Fatalf("guard diagnostics = %#v, want %s", parsed.Diagnostics, test.owner)
			}
			if test.message != "" {
				diagnostic := parsed.Diagnostics[0]
				if diagnostic.Code != test.owner || diagnostic.Message != test.message || parsed.Text(diagnostic.Span) != test.span {
					t.Fatalf("guard diagnostics = %#v", parsed.Diagnostics)
				}
			}
			if test.name == "interface duplicate extends" {
				var interfaceAggregate *Aggregate
				for index := range parsed.Commands {
					if parsed.Commands[index].Aggregate != nil && parsed.Text(parsed.Commands[index].Aggregate.Name) == "I" {
						interfaceAggregate = parsed.Commands[index].Aggregate
						break
					}
				}
				if interfaceAggregate == nil || interfaceAggregate.Kind != BlockInterface || len(interfaceAggregate.Extends) != 1 || parsed.Text(interfaceAggregate.Extends[0]) != "A" {
					t.Fatalf("interface aggregate = %#v", parsed.Commands)
				}
			}
		})
	}
}

func TestOfficialVimParserDuplicateImplementsWithEnum(t *testing.T) {
	file := Parse("vim9script\ninterface I\nendinterface\nenum C implements I, I\n  Apple\nendenum\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1351" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if file.Diagnostics[0].Message != `Duplicate interface after "implements": I` || file.Text(file.Diagnostics[0].Span) != "I" {
		t.Fatalf("diagnostic = %#v", file.Diagnostics[0])
	}
	var aggregate *Aggregate
	for index := range file.Commands {
		if file.Commands[index].Aggregate != nil && file.Text(file.Commands[index].Aggregate.Name) == "C" {
			aggregate = file.Commands[index].Aggregate
			break
		}
	}
	if aggregate == nil || aggregate.Kind != BlockEnum || len(aggregate.Implements) != 1 || file.Text(aggregate.Implements[0]) != "I" {
		t.Fatalf("aggregate = %#v", aggregate)
	}
	var enumBlock *Block
	for index := range file.Blocks {
		if file.Blocks[index].Kind == BlockEnum && file.Blocks[index].End >= 0 {
			enumBlock = &file.Blocks[index]
			break
		}
	}
	if enumBlock == nil || enumBlock.Kind != BlockEnum || enumBlock.End < 0 {
		t.Fatalf("enum block = %#v", file.Blocks)
	}
	var following *Command
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Declaration != nil && file.Text(command.Declaration.Name) == "after" {
			following = command
			break
		}
	}
	if following == nil || following.Canonical != "var" || file.Text(following.Declaration.Name) != "after" {
		t.Fatalf("following declaration = %#v", file.Commands)
	}
	assertFileSpans(t, file)
}

func TestOfficialVimParserDuplicateImplementsName(t *testing.T) {
	source := "vim9script\ninterface I\nendinterface\ninterface J\nendinterface\nclass C implements I, J, I\nendclass\nvar after = 1\n"
	file := Parse(source)
	if len(file.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	diagnostic := file.Diagnostics[0]
	if diagnostic.Code != "vim/E1351" || diagnostic.Message != `Duplicate interface after "implements": I` || file.Text(diagnostic.Span) != "I" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	var class *Aggregate
	for index := range file.Commands {
		if file.Commands[index].Aggregate != nil && file.Text(file.Commands[index].Aggregate.Name) == "C" {
			class = file.Commands[index].Aggregate
			break
		}
	}
	if class == nil || class.Kind != BlockClass || len(class.Implements) != 2 || file.Text(class.Implements[0]) != "I" || file.Text(class.Implements[1]) != "J" {
		t.Fatalf("class = %#v", file.Commands)
	}
	var following *Command
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Declaration != nil && file.Text(command.Declaration.Name) == "after" {
			following = command
			break
		}
	}
	if following == nil || following.Canonical != "var" || file.Text(following.Declaration.Name) != "after" {
		t.Fatalf("following declaration = %#v", file.Commands)
	}
	assertFileSpans(t, file)

	for _, test := range []struct {
		name, source, owner string
	}{
		{"second clause", "vim9script\nclass C implements I implements I\nendclass\n", "vim/E1350"},
		{"missing comma whitespace", "vim9script\nclass C implements I,I\nendclass\n", "vim/E1315"},
		{"interface owns implements", "vim9script\ninterface C implements I\nendinterface\n", "vim/E1381"},
		{"Legacy root", "class C implements I, I\nendclass\n", "vim/E1316"},
		{"one-shot Legacy", "vim9script\nlegacy class C implements I, I\nendclass\n", "vim/E1316"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed := Parse(test.source)
			if hasDiagnostic(parsed, "vim/E1351") || !hasDiagnostic(parsed, test.owner) {
				t.Fatalf("guard diagnostics = %#v, want %s without E1351", parsed.Diagnostics, test.owner)
			}
		})
	}
}

func TestOfficialVimParserDuplicateImplementsQualifiedName(t *testing.T) {
	file := Parse("vim9script\nclass C implements M.Face, M.Face\nendclass\n")
	if len(file.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	diagnostic := file.Diagnostics[0]
	if diagnostic.Code != "vim/E1351" || diagnostic.Message != `Duplicate interface after "implements": M.Face` || file.Text(diagnostic.Span) != "M.Face" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	var class *Aggregate
	for index := range file.Commands {
		if file.Commands[index].Aggregate != nil && file.Text(file.Commands[index].Aggregate.Name) == "C" {
			class = file.Commands[index].Aggregate
			break
		}
	}
	if class == nil || class.Kind != BlockClass || len(class.Implements) != 1 || file.Text(class.Implements[0]) != "M.Face" {
		t.Fatalf("class = %#v", file.Commands)
	}
}

func TestOfficialVimParserDuplicateImplementsDistinctNamesAreAllowed(t *testing.T) {
	file := Parse("vim9script\nclass C implements I, i\nendclass\n")
	if hasDiagnostic(file, "vim/E1351") {
		t.Fatalf("unexpected duplicate diagnostic: %#v", file.Diagnostics)
	}
	var class *Aggregate
	for index := range file.Commands {
		if file.Commands[index].Aggregate != nil && file.Text(file.Commands[index].Aggregate.Name) == "C" {
			class = file.Commands[index].Aggregate
			break
		}
	}
	if class == nil || len(class.Implements) != 2 || file.Text(class.Implements[0]) != "I" || file.Text(class.Implements[1]) != "i" {
		t.Fatalf("class implements = %#v", class)
	}
}

func TestOfficialVimParserDuplicateImplementsWithEnumClause(t *testing.T) {
	file := Parse("vim9script\ninterface I\nendinterface\nenum C implements I implements\n  Apple\nendenum\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1350" || file.Diagnostics[0].Message != `Duplicate "implements"` || file.Text(file.Diagnostics[0].Span) != "implements" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	var enum *Aggregate
	for index := range file.Commands {
		if file.Commands[index].Aggregate != nil && file.Commands[index].Aggregate.Kind == BlockEnum && file.Text(file.Commands[index].Aggregate.Name) == "C" {
			enum = file.Commands[index].Aggregate
			break
		}
	}
	if enum == nil || enum.Kind != BlockEnum || file.Text(enum.Name) != "C" || len(enum.Implements) != 1 || file.Text(enum.Implements[0]) != "I" {
		t.Fatalf("enum = %#v", enum)
	}
	var enumBlock *Block
	for index := range file.Blocks {
		if file.Blocks[index].Kind == BlockEnum && file.Blocks[index].End >= 0 {
			enumBlock = &file.Blocks[index]
			break
		}
	}
	if enumBlock == nil || enumBlock.Kind != BlockEnum || enumBlock.End < 0 {
		t.Fatalf("commands/blocks = %#v", file.Commands)
	}
	following := -1
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Declaration != nil && file.Text(command.Declaration.Name) == "after" {
			following = index
			break
		}
	}
	if following < 0 || file.Commands[following].Canonical != "var" || file.Text(file.Commands[following].Declaration.Name) != "after" {
		t.Fatalf("following declaration = %#v", file.Commands)
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

func TestVim9LegacyTypeAliasDiagnostic(t *testing.T) {
	file := Parse("vim9script\nlegacy type Index = number\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1393" || file.Diagnostics[0].Message != "Type can only be defined in Vim9 script" || file.Text(file.Diagnostics[0].Span) != "type" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	alias := file.Commands[1].TypeAlias
	if alias == nil || file.Text(alias.Name) != "Index" || alias.Type == nil || alias.Type.Name != "number" || len(file.Commands) != 3 || file.Commands[2].Declaration == nil {
		t.Fatalf("commands = %#v", file.Commands)
	}
	assertFileSpans(t, file)
}

func TestVim9TypeAliasLowercaseNameDiagnostic(t *testing.T) {
	file := Parse("vim9script\ntype index = number\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1394" || file.Diagnostics[0].Message != "Type name must start with an uppercase letter: index = number" || file.Text(file.Diagnostics[0].Span) != "index" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	alias := file.Commands[1].TypeAlias
	if alias == nil || file.Text(alias.Name) != "index" || alias.Type == nil || alias.Type.Name != "number" || len(file.Commands) != 3 || file.Commands[2].Declaration == nil {
		t.Fatalf("commands = %#v", file.Commands)
	}
	assertFileSpans(t, file)
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

func TestNestedImportDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		wantE1094    bool
		malformed    bool
	}{
		{
			name: "official def import before assignment",
			source: "vim9script\ndef Func()\n  import './module.vim' as module\n" +
				"  module = 1\nenddef\n",
			wantE1094: true,
		},
		{
			name: "official def import before call",
			source: "vim9script\ndef Func()\n  import './module.vim' as module\n" +
				"  module.Func()\nenddef\n",
			wantE1094: true,
		},
		{
			name: "legacy function body",
			source: "function Legacy()\n  import './module.vim' as module\n" +
				"endfunction\n",
			wantE1094: true,
		},
		{
			name: "malformed nested import retains AST only",
			source: "vim9script\ndef Func()\n  import [] as 9module\n" +
				"enddef\n",
			wantE1094: true,
			malformed: true,
		},
		{
			name:   "top level import remains valid",
			source: "vim9script\nimport './module.vim' as module\n",
		},
		{
			name:   "legacy root import remains valid",
			source: "import './module.vim' as module\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var imports []*Import
			var e1094 []Diagnostic
			for index := range file.Commands {
				command := &file.Commands[index]
				if command.Import != nil {
					imports = append(imports, command.Import)
				}
			}
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1094" {
					e1094 = append(e1094, diagnostic)
				}
			}
			if len(imports) != 1 || imports[0].Path == nil || imports[0].PathSpan.Start >= imports[0].PathSpan.End {
				t.Fatalf("import recovery = %#v", imports)
			}
			if test.wantE1094 {
				if len(file.Diagnostics) != 1 || len(e1094) != 1 || e1094[0].Message != "Import can only be used in a script" || file.Text(e1094[0].Span) != "import" {
					t.Fatalf("diagnostics = %#v", file.Diagnostics)
				}
			} else if len(e1094) != 0 || len(file.Diagnostics) != 0 {
				t.Fatalf("top-level import diagnostics = %#v", file.Diagnostics)
			}
			if test.malformed && (imports[0].Path.Kind != ExpressionList || file.Text(imports[0].PathSpan) != "[]" || file.Text(imports[0].Alias) != "") {
				t.Fatalf("malformed import AST = %#v", imports[0])
			}
		})
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
		for _, member := range []string{"var count = 7", "static var count = 7"} {
			file := Parse("interface Legacy\n  " + member + "\nendinterface\n")
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1342" || file.Text(file.Diagnostics[0].Span) != "interface" {
				t.Fatalf("member=%q diagnostics=%#v", member, file.Diagnostics)
			}
			assertFileSpans(t, file)
		}
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

func TestVim9EnumExtendsReportsE1416(t *testing.T) {
	for name, declaration := range map[string]string{
		"class": "class Base\nendclass\n",
		"enum":  "enum Base\n  First\nendenum\n",
	} {
		t.Run(name, func(t *testing.T) {
			file := Parse("vim9script\n" + declaration + "enum Child extends Base\n  Value\nendenum\n")
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1416" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Enum cannot extend a class or enum" || file.Text(got[0].Span) != "extends" {
				t.Fatalf("E1416 diagnostics = %#v; all diagnostics = %#v", got, file.Diagnostics)
			}
			var child *Command
			for index := range file.Commands {
				if file.Commands[index].Aggregate != nil && file.Text(file.Commands[index].Aggregate.Name) == "Child" {
					child = &file.Commands[index]
					break
				}
			}
			if child == nil || len(child.Aggregate.Extends) != 0 {
				t.Fatalf("commands = %#v", file.Commands)
			}
			assertFileSpans(t, file)
		})
	}

	allowed := Parse("vim9script\ninterface Face\nendinterface\nenum Child implements Face\n  Value\nendenum\n")
	if hasDiagnostic(allowed, "vim/E1416") {
		t.Fatalf("implements diagnostics = %#v", allowed.Diagnostics)
	}
}

func TestVim9ClassNameReportsE1314(t *testing.T) {
	for _, test := range []struct {
		name, source, span, message string
	}{
		{"ordinary lowercase recovery", "vim9script\nclass notWorking\nendclass\nvar after = 1\n", "notWorking", "Class name must start with an uppercase letter: notWorking"},
		{"abstract lowercase", "vim9script\nabstract class lower\nendclass\n", "lower", "Class name must start with an uppercase letter: lower"},
		{"digit", "vim9script\nclass 1Thing\nendclass\n", "1Thing", "Class name must start with an uppercase letter: 1Thing"},
		{"underscore", "vim9script\nclass _Thing\nendclass\n", "_Thing", "Class name must start with an uppercase letter: _Thing"},
		{"non-ASCII", "vim9script\nclass ÄThing\nendclass\n", "ÄThing", "Class name must start with an uppercase letter: ÄThing"},
		{"before whitespace diagnostic", "vim9script\nclass lower!\nendclass\n", "lower", "Class name must start with an uppercase letter: lower!"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1314" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1315" {
					t.Fatalf("E1314 source retained E1315: %#v", file.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1314 diagnostics = %#v", file.Diagnostics)
			}
			if file.Commands[1].Aggregate == nil || file.Text(file.Commands[1].Aggregate.Name) != test.span || len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockClass || file.Blocks[0].End < 0 {
				t.Fatalf("class recovery = %#v, blocks = %#v", file.Commands, file.Blocks)
			}
			if test.name == "ordinary lowercase recovery" && (len(file.Commands) != 4 || file.Commands[3].Declaration == nil) {
				t.Fatalf("following declaration recovery = %#v", file.Commands)
			}
			assertFileSpans(t, file)
		})
	}

	valid := Parse("vim9script\nclass Working\nendclass\n")
	if hasDiagnostic(valid, "vim/E1314") {
		t.Fatalf("valid class diagnostics = %#v", valid.Diagnostics)
	}
	legacy := Parse("class lower\nendclass\n")
	if !hasDiagnostic(legacy, "vim/E1316") || hasDiagnostic(legacy, "vim/E1314") {
		t.Fatalf("Legacy class diagnostics = %#v", legacy.Diagnostics)
	}
}

func TestVim9InterfaceNameReportsE1343(t *testing.T) {
	for _, test := range []struct {
		name, source, span, message string
	}{
		{"lowercase recovery", "vim9script\ninterface notWorking\nendinterface\nvar after = 1\n", "notWorking", "Interface name must start with an uppercase letter: notWorking"},
		{"digit", "vim9script\ninterface 1Thing\nendinterface\n", "1Thing", "Interface name must start with an uppercase letter: 1Thing"},
		{"underscore", "vim9script\ninterface _Thing\nendinterface\n", "_Thing", "Interface name must start with an uppercase letter: _Thing"},
		{"non-ASCII", "vim9script\ninterface ÄThing\nendinterface\n", "ÄThing", "Interface name must start with an uppercase letter: ÄThing"},
		{"before whitespace diagnostic", "vim9script\ninterface lower@bad\nendinterface\n", "lower", "Interface name must start with an uppercase letter: lower@bad"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1343" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1315" {
					t.Fatalf("E1343 source retained E1315: %#v", file.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1343 diagnostics = %#v", file.Diagnostics)
			}
			if file.Commands[1].Aggregate == nil || file.Text(file.Commands[1].Aggregate.Name) != test.span || len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockInterface || file.Blocks[0].End < 0 {
				t.Fatalf("interface recovery = %#v, blocks = %#v", file.Commands, file.Blocks)
			}
			if test.name == "lowercase recovery" && (len(file.Commands) != 4 || file.Commands[3].Declaration == nil) {
				t.Fatalf("following declaration recovery = %#v", file.Commands)
			}
			assertFileSpans(t, file)
		})
	}

	valid := Parse("vim9script\ninterface Working\nendinterface\n")
	if hasDiagnostic(valid, "vim/E1343") {
		t.Fatalf("valid interface diagnostics = %#v", valid.Diagnostics)
	}
	for _, source := range []string{
		"interface lower\nendinterface\n",
		"vim9script\nlegacy interface lower\nendinterface\n",
	} {
		file := Parse(source)
		if !hasDiagnostic(file, "vim/E1342") || hasDiagnostic(file, "vim/E1343") {
			t.Fatalf("Legacy interface diagnostics = %#v", file.Diagnostics)
		}
	}
}

func TestVim9DeclarationWhitespaceReportsE1315(t *testing.T) {
	for _, test := range []struct {
		name, source, span, remainder string
		aggregate                     bool
	}{
		{"class name", "vim9script\nclass Not@working\nendclass\nvar after = 1\n", "Not@working", "Not@working", true},
		{"enum name", "vim9script\nenum Foo@bar\nendenum\nvar after = 1\n", "Foo@bar", "Foo@bar", true},
		{"class extends", "vim9script\nclass B extends A\"\nendclass\nvar after = 1\n", "A\"", "A\"", true},
		{"class implements", "vim9script\nclass B implements A;\nendclass\nvar after = 1\n", "A;", "A;", true},
		{"class implements comma", "vim9script\nclass C implements A,B\nendclass\nvar after = 1\n", "A,B", "A,B", true},
		{"interface extends comma", "vim9script\ninterface C extends A, B\nendinterface\nvar after = 1\n", "A, B", "A, B", true},
		{"type assignment", "vim9script\ntype MyType=number\nvar after = 1\n", "MyType=number", "MyType=number", false},
		{"type colon", "vim9script\ntype Index:number\nvar after = 1\n", "Index:number", "Index:number", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1315" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E488" || diagnostic.Code == "vim/E398" || diagnostic.Code == "vim/E1069" {
					t.Fatalf("E1315 source retained secondary diagnostic: %#v", file.Diagnostics)
				}
			}
			message := "White space required after name: " + test.remainder
			if len(got) != 1 || got[0].Message != message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1315 diagnostics = %#v", file.Diagnostics)
			}
			found, after := false, false
			for _, command := range file.Commands {
				if test.aggregate && command.Aggregate != nil {
					found = true
				}
				if !test.aggregate && command.TypeAlias != nil {
					found = true
				}
				if command.Declaration != nil {
					after = true
				}
			}
			if !found || !after {
				t.Fatalf("declaration recovery = %#v", file.Commands)
			}
			if test.aggregate && len(file.Blocks) != 1 || test.aggregate && file.Blocks[0].End < 0 {
				t.Fatalf("aggregate blocks = %#v", file.Blocks)
			}
			assertFileSpans(t, file)
		})
	}

	for _, test := range []struct{ source, code string }{
		{"vim9script\nclass lower@bad\nendclass\n", "vim/E1314"},
		{"vim9script\ninterface lower@bad\nendinterface\n", "vim/E1343"},
		{"vim9script\nenum lower@bad\nendenum\n", "vim/E1415"},
		{"vim9script\ntype lower=number\n", "vim/E1394"},
	} {
		file := Parse(test.source)
		if !hasDiagnostic(file, test.code) || hasDiagnostic(file, "vim/E1315") {
			t.Fatalf("name-priority diagnostics = %#v", file.Diagnostics)
		}
	}
	for _, source := range []string{
		"vim9script\nclass B extends A\nendclass\n",
		"vim9script\nenum Foo\nendenum\n",
		"vim9script\ntype MyType = number\n",
	} {
		if file := Parse(source); hasDiagnostic(file, "vim/E1315") {
			t.Fatalf("valid whitespace diagnostics = %#v", file.Diagnostics)
		}
	}
	legacy := Parse("class B@bad\nendclass\n")
	if !hasDiagnostic(legacy, "vim/E1316") || hasDiagnostic(legacy, "vim/E1315") {
		t.Fatalf("Legacy aggregate diagnostics = %#v", legacy.Diagnostics)
	}
}

func TestVim9LowercaseEnumNameReportsE1415(t *testing.T) {
	file := Parse("vim9script\nenum foo\nendenum\n")
	var got []Diagnostic
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code == "vim/E1415" {
			got = append(got, diagnostic)
		}
	}
	if len(got) != 1 || got[0].Message != "Enum name must start with an uppercase letter: foo" || file.Text(got[0].Span) != "foo" {
		t.Fatalf("E1415 diagnostics = %#v; all diagnostics = %#v", got, file.Diagnostics)
	}
	if len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockEnum || file.Blocks[0].End < 0 || file.Commands[1].Aggregate == nil || file.Text(file.Commands[1].Aggregate.Name) != "foo" {
		t.Fatalf("commands = %#v, blocks = %#v", file.Commands, file.Blocks)
	}
	assertFileSpans(t, file)

	valid := Parse("vim9script\nenum Foo\n  lowercase\nendenum\n")
	if hasDiagnostic(valid, "vim/E1415") {
		t.Fatalf("valid enum diagnostics = %#v", valid.Diagnostics)
	}
}

func TestVim9InterfacePublicMemberReportsE1387(t *testing.T) {
	for _, member := range []string{
		"public static var num: number",
		"public final num: number = 1",
		"public def Foo()\n  enddef",
	} {
		source := "vim9script\ninterface A\n  " + member + "\nendinterface\nvar after = 1\n"
		file := Parse(source)
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1387" || file.Diagnostics[0].Message != "public variable not supported in an interface" || file.Text(file.Diagnostics[0].Span) != "public" {
			t.Fatalf("member=%q diagnostics=%#v", member, file.Diagnostics)
		}
		last := file.Commands[len(file.Commands)-1]
		if last.Declaration == nil || file.Text(last.Declaration.Name) != "after" {
			t.Fatalf("member=%q commands=%#v", member, file.Commands)
		}
		assertFileSpans(t, file)
	}
}

func TestVim9InterfaceImplementsReportsE1381(t *testing.T) {
	file := Parse("vim9script\ninterface A\nendinterface\ninterface B implements A\nendinterface\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1381" || file.Diagnostics[0].Message != `Interface cannot use "implements"` || file.Text(file.Diagnostics[0].Span) != "implements" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 6 || file.Commands[3].Aggregate == nil || file.Text(file.Commands[3].Aggregate.Name) != "B" || len(file.Commands[3].Aggregate.Implements) != 0 || file.Commands[5].Declaration == nil {
		t.Fatalf("commands = %#v", file.Commands)
	}
	assertFileSpans(t, file)
}

func TestVim9InterfaceProtectedMethodReportsE1380(t *testing.T) {
	file := Parse("vim9script\ninterface A\n  def _Foo(d: dict<any>): list<string>\nendinterface\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1380" || file.Diagnostics[0].Message != "Protected method not supported in an interface" || file.Text(file.Diagnostics[0].Span) != "_Foo" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 5 || file.Commands[2].Function == nil || file.Text(file.Commands[2].Function.Name) != "_Foo" || file.Commands[4].Declaration == nil {
		t.Fatalf("commands = %#v", file.Commands)
	}
	assertFileSpans(t, file)

	for _, source := range []string{
		"vim9script\ninterface A\n  def Foo()\nendinterface\n",
		"vim9script\ninterface A\n  static def _Foo()\nendinterface\n",
		"vim9script\ninterface A\n  public def _Foo()\nendinterface\n",
		"vim9script\nclass A\n  def _Foo()\n  enddef\nendclass\n",
	} {
		if hasDiagnostic(Parse(source), "vim/E1380") {
			t.Fatalf("guard source reported E1380: %s", source)
		}
	}
}

func TestVim9InterfaceProtectedVariableReportsE1379(t *testing.T) {
	file := Parse("vim9script\ninterface A\n  var _Foo: list<string> = []\nendinterface\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1379" || file.Diagnostics[0].Message != "Protected variable not supported in an interface" || file.Text(file.Diagnostics[0].Span) != "_Foo" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 5 || file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "_Foo" || file.Commands[4].Declaration == nil {
		t.Fatalf("commands = %#v", file.Commands)
	}
	assertFileSpans(t, file)

	for _, source := range []string{
		"vim9script\ninterface A\n  var Foo: list<string>\nendinterface\n",
		"vim9script\ninterface A\n  static var _Foo: list<string>\nendinterface\n",
		"vim9script\ninterface A\n  public var _Foo: list<string>\nendinterface\n",
		"vim9script\nclass A\n  var _Foo: list<string>\nendclass\n",
	} {
		if hasDiagnostic(Parse(source), "vim/E1379") {
			t.Fatalf("guard source reported E1379: %s", source)
		}
	}
}

func TestVim9InterfaceStaticMemberReportsE1378(t *testing.T) {
	for _, member := range []string{
		"static var num: number",
		"static var _num: number",
		"static def Foo(d: dict<any>): list<string>",
		"static def _Foo()",
		"static final value: number = 1",
		"static const value: number = 1",
	} {
		source := "vim9script\ninterface A\n  " + member + "\nendinterface\nvar after = 1\n"
		file := Parse(source)
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1378" || file.Diagnostics[0].Message != "Static member not supported in an interface" || file.Text(file.Diagnostics[0].Span) != "static" {
			t.Fatalf("member=%q diagnostics=%#v", member, file.Diagnostics)
		}
		last := file.Commands[len(file.Commands)-1]
		if last.Declaration == nil || file.Text(last.Declaration.Name) != "after" {
			t.Fatalf("member=%q commands=%#v", member, file.Commands)
		}
		assertFileSpans(t, file)
	}

	for _, source := range []string{
		"vim9script\ninterface A\n  var num: number\n  def Foo()\nendinterface\n",
		"vim9script\nclass A\n  static var num: number\n  static def Foo()\n  enddef\nendclass\n",
		"vim9script\ninterface A\n  public static var num: number\nendinterface\n",
		"vim9script\ninterface A\n  abstract static def Foo()\n  enddef\nendinterface\n",
	} {
		if hasDiagnostic(Parse(source), "vim/E1378") {
			t.Fatalf("guard source reported E1378: %s", source)
		}
	}
}

func TestVim9InterfaceConstReportsE1410(t *testing.T) {
	file := Parse("vim9script\ninterface A\n  const foo: number = 10\nendinterface\n")
	var got []Diagnostic
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code == "vim/E1410" {
			got = append(got, diagnostic)
		}
	}
	if len(got) != 1 || got[0].Message != "Const variable not supported in an interface" || file.Text(got[0].Span) != "const" {
		t.Fatalf("E1410 diagnostics = %#v; all diagnostics = %#v", got, file.Diagnostics)
	}
	if len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockInterface || file.Blocks[0].End < 0 || file.Commands[2].Block != 0 || file.Commands[2].Declaration != nil {
		t.Fatalf("commands = %#v, blocks = %#v", file.Commands, file.Blocks)
	}
	assertFileSpans(t, file)

	for name, source := range map[string]string{
		"interface var": "vim9script\ninterface A\n  var foo: number\nendinterface\n",
		"class const":   "vim9script\nclass A\n  const foo: number = 10\nendclass\n",
	} {
		t.Run(name, func(t *testing.T) {
			valid := Parse(source)
			if hasDiagnostic(valid, "vim/E1410") {
				t.Fatalf("diagnostics = %#v", valid.Diagnostics)
			}
		})
	}
}

func TestVim9InterfaceFinalReportsE1408(t *testing.T) {
	file := Parse("vim9script\ninterface A\n  final foo: number = 10\nendinterface\n")
	var got []Diagnostic
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code == "vim/E1408" {
			got = append(got, diagnostic)
		}
	}
	if len(got) != 1 || got[0].Message != "Final variable not supported in an interface" || file.Text(got[0].Span) != "final" {
		t.Fatalf("E1408 diagnostics = %#v; all diagnostics = %#v", got, file.Diagnostics)
	}
	if len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockInterface || file.Blocks[0].End < 0 || file.Commands[2].Block != 0 || file.Commands[2].Declaration != nil {
		t.Fatalf("commands = %#v, blocks = %#v", file.Commands, file.Blocks)
	}
	assertFileSpans(t, file)

	for name, source := range map[string]string{
		"interface var":          "vim9script\ninterface A\n  var foo: number\nendinterface\n",
		"class final":            "vim9script\nclass A\n  final foo: number = 10\nendclass\n",
		"interface static final": "vim9script\ninterface A\n  static final foo: number = 10\nendinterface\n",
	} {
		t.Run(name, func(t *testing.T) {
			valid := Parse(source)
			if hasDiagnostic(valid, "vim/E1408") {
				t.Fatalf("diagnostics = %#v", valid.Diagnostics)
			}
		})
	}
}

func TestVim9InterfaceAbstractReportsE1404(t *testing.T) {
	for name, member := range map[string]string{
		"method":          "abstract def Foo()\n  enddef",
		"static method":   "abstract static def Foo()\n  enddef",
		"static variable": "abstract static foo: number = 10",
	} {
		t.Run(name, func(t *testing.T) {
			file := Parse("vim9script\ninterface A\n  " + member + "\nendinterface\n")
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1404" || file.Diagnostics[0].Message != "Abstract cannot be used in an interface" || file.Text(file.Diagnostics[0].Span) != "abstract" {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			if len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockInterface || file.Blocks[0].End < 0 {
				t.Fatalf("blocks = %#v, commands = %#v", file.Blocks, file.Commands)
			}
			assertFileSpans(t, file)
		})
	}

	for name, source := range map[string]string{
		"interface method": "vim9script\ninterface A\n  def Foo()\n  enddef\nendinterface\n",
		"abstract class":   "vim9script\nabstract class A\n  abstract def Foo()\n  enddef\nendclass\n",
	} {
		t.Run(name, func(t *testing.T) {
			file := Parse(source)
			if hasDiagnostic(file, "vim/E1404") {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
		})
	}
}

func TestVim9ConcreteClassAbstractMemberReportsE1372(t *testing.T) {
	for _, member := range []string{
		"abstract def Foo()",
		"abstract static def Foo(): number",
		"abstract this.val = 10",
	} {
		source := "vim9script\nclass A\n  " + member + "\nendclass\nvar after = 1\n"
		file := Parse(source)
		var got []Diagnostic
		for _, diagnostic := range file.Diagnostics {
			if diagnostic.Code == "vim/E1372" {
				got = append(got, diagnostic)
			}
			if diagnostic.Code == "vim/E1371" {
				t.Fatalf("member=%q also reported E1371: %#v", member, diagnostic)
			}
		}
		if len(got) != 1 || got[0].Message != `Abstract method "`+member+`" cannot be defined in a concrete class` || file.Text(got[0].Span) != member {
			t.Fatalf("member=%q E1372 diagnostics=%#v; all diagnostics=%#v", member, got, file.Diagnostics)
		}
		last := file.Commands[len(file.Commands)-1]
		if last.Declaration == nil || file.Text(last.Declaration.Name) != "after" {
			t.Fatalf("member=%q commands=%#v", member, file.Commands)
		}
		assertFileSpans(t, file)
	}

	for _, source := range []string{
		"vim9script\nabstract class A\n  abstract def Foo()\nendclass\nclass B extends A\n  def Foo()\n  enddef\nendclass\n",
		"vim9script\nabstract class A\n  abstract static def Foo()\nendclass\n",
		"vim9script\ninterface A\n  abstract def Foo()\nendinterface\n",
		"vim9script\nenum A\n  abstract def Foo()\nendenum\n",
	} {
		if hasDiagnostic(Parse(source), "vim/E1372") {
			t.Fatalf("guard source reported E1372: %s", source)
		}
	}
}

func TestVim9AbstractClassInvalidAbstractMemberReportsE1371(t *testing.T) {
	for _, member := range []string{
		"abstract this.val = 10",
		"abstract static def Foo(): number",
	} {
		source := "vim9script\nabstract class A\n  " + member + "\nendclass\nvar after = 1\n"
		file := Parse(source)
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1371" || file.Diagnostics[0].Message != `Abstract must be followed by "def"` || file.Text(file.Diagnostics[0].Span) != "abstract" {
			t.Fatalf("member=%q diagnostics=%#v", member, file.Diagnostics)
		}
		last := file.Commands[len(file.Commands)-1]
		if last.Declaration == nil || file.Text(last.Declaration.Name) != "after" {
			t.Fatalf("member=%q commands=%#v", member, file.Commands)
		}
		assertFileSpans(t, file)
	}

	for _, source := range []string{
		"vim9script\nabstract class A\n  abstract def Foo()\nendclass\n",
		"vim9script\nclass A\n  abstract static def Foo()\nendclass\n",
		"vim9script\ninterface A\n  abstract static def Foo()\nendinterface\n",
		"vim9script\nenum A\n  abstract def Foo()\nendenum\n",
	} {
		if hasDiagnostic(Parse(source), "vim/E1371") {
			t.Fatalf("guard source reported E1371: %s", source)
		}
	}
}

func TestVim9StaticConstructorReportsE1370(t *testing.T) {
	for _, method := range []string{"new", "newOther", "_new", "_newPrivate"} {
		source := "vim9script\nclass A\n  static def " + method + "()\n  enddef\nendclass\nvar after = 1\n"
		file := Parse(source)
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1370" || file.Diagnostics[0].Message != `Cannot define a "new" method as static` || file.Text(file.Diagnostics[0].Span) != "static" {
			t.Fatalf("method=%q diagnostics=%#v", method, file.Diagnostics)
		}
		last := file.Commands[len(file.Commands)-1]
		if last.Declaration == nil || file.Text(last.Declaration.Name) != "after" {
			t.Fatalf("method=%q commands=%#v", method, file.Commands)
		}
		assertFileSpans(t, file)
	}

	withReturn := Parse("vim9script\nclass A\n  static def new(): number\n  enddef\nendclass\n")
	if len(withReturn.Diagnostics) != 1 || withReturn.Diagnostics[0].Code != "vim/E1370" {
		t.Fatalf("return diagnostics=%#v", withReturn.Diagnostics)
	}

	for _, source := range []string{
		"vim9script\nclass A\n  def new()\n  enddef\nendclass\n",
		"vim9script\nclass A\n  static def New()\n  enddef\nendclass\n",
		"vim9script\nclass A\n  static def Build()\n  enddef\nendclass\n",
		"vim9script\nabstract class A\n  static def new()\n  enddef\nendclass\n",
		"vim9script\nclass A\n  abstract static def new()\nendclass\n",
		"vim9script\ninterface A\n  static def new()\nendinterface\n",
	} {
		if hasDiagnostic(Parse(source), "vim/E1370") {
			t.Fatalf("guard source reported E1370: %s", source)
		}
	}
}

func TestVim9ConstructorReturnTypeReportsE1365(t *testing.T) {
	for _, test := range []struct {
		name       string
		method     string
		returnType string
	}{
		{name: "official any", method: "new", returnType: "any"},
		{name: "official dictionary", method: "new", returnType: "dict<any>"},
		{name: "named constructor", method: "newValues", returnType: "number"},
		{name: "protected constructor", method: "_newPrivate", returnType: "string"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "vim9script\nclass A\n  def " + test.method + "(): " + test.returnType + "\n  enddef\nendclass\nvar after = 1\n"
			file := Parse(source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1365" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != `Cannot use a return type with the "new" method` || file.Text(got[0].Span) != test.returnType {
				t.Fatalf("diagnostics=%#v", file.Diagnostics)
			}
			constructor := &file.Commands[file.Commands[1].Aggregate.Members[0]]
			if constructor.Function == nil || constructor.Function.ReturnType == nil {
				t.Fatalf("constructor signature not retained: %#v", constructor)
			}
			last := file.Commands[len(file.Commands)-1]
			if last.Declaration == nil || file.Text(last.Declaration.Name) != "after" {
				t.Fatalf("following declaration not retained: %#v", last)
			}
			assertFileSpans(t, file)
		})
	}

	for _, source := range []string{
		"vim9script\nclass A\n  def new()\n  enddef\nendclass\n",
		"vim9script\nclass A\n  def new(): void\n  enddef\n  def newValues(): void\n  enddef\n  def _newPrivate(): void\n  enddef\nendclass\n",
		"vim9script\nclass A\n  def Build(): number\n    return 1\n  enddef\nendclass\n",
		"vim9script\nclass A\n  static def new(): number\n  enddef\nendclass\n",
		"vim9script\nclass A\n  abstract def new(): number\nendclass\n",
		"vim9script\nabstract class A\n  def new(): number\n  enddef\nendclass\n",
	} {
		file := Parse(source)
		for _, diagnostic := range file.Diagnostics {
			if diagnostic.Code == "vim/E1365" {
				t.Fatalf("guard source reported E1365: %#v\n%s", diagnostic, source)
			}
		}
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

func TestLegacyRootDefScriptVariableDeclarationDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source, span string
		recovery           bool
	}{
		{
			name: "var declaration",
			source: "def Func()\n  var s:name = 1\n" +
				"  var after = 2\nenddef\n",
			span: "s:name",
		},
		{
			name: "typed var declaration without initializer",
			source: "def Func()\n  var s:typed: number\n" +
				"  var after = 2\nenddef\n",
			span: "s:typed",
		},
		{
			name: "final declaration",
			source: "def Func()\n  final s:name = 1\n" +
				"  var after = 2\nenddef\n",
			span: "s:name",
		},
		{
			name: "const declaration",
			source: "def Func()\n  const s:name = 1\n" +
				"  var after = 2\nenddef\n",
			span: "s:name",
		},
		{
			name: "typed-tail recovery",
			source: "def Func()\n  s:notexist:repl\n" +
				"  var after = 2\nenddef\n",
			span:     "s:notexist",
			recovery: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1101" || file.Diagnostics[0].Message != "Cannot declare a script variable in a function: "+test.span || file.Text(file.Diagnostics[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			if len(file.Commands) != 4 || file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "after" {
				t.Fatalf("following-command recovery = %#v", file.Commands)
			}
			if test.recovery {
				if len(file.Commands[1].Expressions) != 1 || file.Commands[1].Expressions[0].Kind != ExpressionIdentifier || file.Commands[1].Expressions[0].Value != test.span {
					t.Fatalf("typed-tail recovery = %#v", file.Commands[1])
				}
			} else if file.Commands[1].Declaration == nil || file.Text(file.Commands[1].Declaration.Name) != test.span {
				t.Fatalf("declaration recovery = %#v", file.Commands[1])
			}
			assertFileSpans(t, file)
		})
	}

	for _, source := range []string{
		"vim9script\ndef Func()\n  var s:name = 1\nenddef\n",
		"function Legacy()\n  var s:name = 1\nendfunction\n",
		"vim9cmd var s:name = 1\n",
		"def Func()\n  s:name = 1\nenddef\n",
		"def Func()\n  var g:name = 1\n  const g:constant = 1\n  final w:window = 1\nenddef\n",
	} {
		file := Parse(source)
		for _, diagnostic := range file.Diagnostics {
			if diagnostic.Code == "vim/E1101" {
				t.Fatalf("unexpected E1101 = %#v\n%s", file.Diagnostics, source)
			}
		}
	}
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

func TestVim9ScopedVariableTypeDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source, span string
	}{
		{"window", "vim9script\nw:foo: number = 1\n", "w:foo:"},
		{"tab", "vim9script\nt:foo: bool = true\n", "t:foo:"},
		{"buffer", "vim9script\nb:foo: string = 'x'\n", "b:foo:"},
		{"global", "vim9script\ng:foo: number = 1\n", "g:foo:"},
		{"scoped const", "vim9script\nconst w:FOO: number = 1\n", "w:FOO:"},
		{"scoped final", "vim9script\nfinal g:FOO: number = 1\n", "g:FOO:"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1304" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Cannot use type with this variable: "+test.span || file.Text(got[0].Span) != test.span {
				t.Fatalf("source = %q, diagnostics = %#v", test.source, file.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef F()\n  w:foo: number = 1\nenddef\n",
		"vim9script\nvar g:foo: number = 1\n",
		"vim9script\ng:foo = 1\n",
		"vim9script\nvar foo: number = 1\n",
		"let w:foo: number = 1\n",
		"vim9script\nw:\n",
	} {
		for _, diagnostic := range Parse(source).Diagnostics {
			if diagnostic.Code == "vim/E1304" {
				t.Fatalf("guard unexpectedly received E1304: %#v in source %q", diagnostic, source)
			}
		}
	}
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
