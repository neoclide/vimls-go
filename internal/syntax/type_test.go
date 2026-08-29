package syntax

import "testing"

func TestVim9TypeParserCoversContainersTuplesAndFunctions(t *testing.T) {
	tests := []struct {
		source string
		kind   TypeKind
		name   string
		args   int
	}{
		{source: "number", kind: TypeNamed, name: "number"},
		{source: "list<dict<string>>", kind: TypeGeneric, name: "list", args: 1},
		{source: "tuple<number, string, ...list<bool>>", kind: TypeGeneric, name: "tuple", args: 3},
		{source: "func(number, list<string>): tuple<number, string>", kind: TypeFunction, name: "func", args: 2},
		{source: "T", kind: TypeNamed, name: "T"},
	}
	for _, test := range tests {
		t.Run(test.source, func(t *testing.T) {
			typeNode, diagnostics := (Vim9TypeParser{}).Parse(test.source)
			if len(diagnostics) != 0 || typeNode.Kind != test.kind || typeNode.Name != test.name || len(typeNode.Arguments) != test.args {
				t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
			}
			if test.kind == TypeFunction && (typeNode.ReturnType == nil || typeNode.ReturnType.Name != "tuple") {
				t.Fatalf("function return = %#v", typeNode.ReturnType)
			}
		})
	}
}

func TestVim9TypeParserRecoversFromIncompleteTypes(t *testing.T) {
	typeNode, diagnostics := (Vim9TypeParser{}).Parse("list<dict<number>")
	if typeNode == nil || len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1009" {
		t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
	}
}

func TestVim9TypeParserRequiresContainerArguments(t *testing.T) {
	// Vim v9.2.1015 src/vim9type.c parse_type_member(),
	// parse_type_tuple(), and parse_type_object().
	for _, name := range []string{"list", "dict", "tuple", "object"} {
		t.Run(name+" missing argument", func(t *testing.T) {
			typeNode, diagnostics := (Vim9TypeParser{}).Parse(name)
			if typeNode == nil || typeNode.Name != name || len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1008" {
				t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
			}
		})
		t.Run(name+" whitespace before opener", func(t *testing.T) {
			typeNode, diagnostics := (Vim9TypeParser{}).Parse(name + " <number>")
			if typeNode == nil || typeNode.Name != name || len(typeNode.Arguments) != 1 || typeNode.Arguments[0].Name != "number" || len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1068" {
				t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
			}
		})
	}

	// Unlike tuple types, list, dict, and object allow whitespace immediately
	// inside their angle brackets.
	for _, name := range []string{"list", "dict", "object"} {
		t.Run(name+" allows inner whitespace", func(t *testing.T) {
			typeNode, diagnostics := (Vim9TypeParser{}).Parse(name + "< number >")
			if typeNode == nil || typeNode.Name != name || len(typeNode.Arguments) != 1 || typeNode.Arguments[0].Name != "number" || len(diagnostics) != 0 {
				t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
			}
		})
	}

	for _, name := range []string{"list", "dict", "tuple", "object"} {
		t.Run(name+" empty arguments", func(t *testing.T) {
			typeNode, diagnostics := (Vim9TypeParser{}).Parse(name + "<>")
			if typeNode == nil || typeNode.Name != name || len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1008" {
				t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
			}
		})
	}
}

func TestVim9TypeParserSingleMemberContainers(t *testing.T) {
	for _, name := range []string{"list", "dict", "object"} {
		t.Run(name+" missing closer", func(t *testing.T) {
			typeNode, diagnostics := (Vim9TypeParser{}).Parse(name + "<number")
			if typeNode == nil || len(typeNode.Arguments) != 1 || len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1009" {
				t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
			}
		})
		t.Run(name+" rejects a second member", func(t *testing.T) {
			typeNode, diagnostics := (Vim9TypeParser{}).Parse(name + "<number, string>")
			if typeNode == nil || len(typeNode.Arguments) != 2 || len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1009" {
				t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
			}
		})
	}
}

func TestVim9TypeParserBareFuncRemainsValid(t *testing.T) {
	typeNode, diagnostics := (Vim9TypeParser{}).Parse("func")
	if typeNode == nil || typeNode.Kind != TypeFunction || len(diagnostics) != 0 {
		t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
	}
}

func TestVim9TypeParserFunctionTypeSpacing(t *testing.T) {
	// Vim v9.2.1015 src/vim9type.c parse_type_func().
	valid := []struct {
		source     string
		arguments  int
		returnType string
	}{
		{source: "func"},
		{source: "func()"},
		{source: "func(): void", returnType: "void"},
		{source: "func: number", returnType: "number"},
		{source: "func(number, string): bool", arguments: 2, returnType: "bool"},
	}
	for _, test := range valid {
		t.Run("valid "+test.source, func(t *testing.T) {
			typeNode, diagnostics := (Vim9TypeParser{}).Parse(test.source)
			if typeNode == nil || typeNode.Kind != TypeFunction || len(typeNode.Arguments) != test.arguments || len(diagnostics) != 0 {
				t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
			}
			if test.returnType == "" && typeNode.ReturnType != nil || test.returnType != "" && (typeNode.ReturnType == nil || typeNode.ReturnType.Name != test.returnType) {
				t.Fatalf("return type = %#v", typeNode.ReturnType)
			}
		})
	}

	invalid := []struct {
		source     string
		diagnostic string
	}{
		{source: "func ()", diagnostic: "vim/E488"},
		{source: "func : void", diagnostic: "vim/E488"},
		{source: "func() : void", diagnostic: "vim/E488"},
		{source: "func( number)", diagnostic: "vim/E1010"},
		{source: "func(number )", diagnostic: "vim/E488"},
		{source: "func(number,string)", diagnostic: "vim/E1069"},
		{source: "func(number , string)", diagnostic: "vim/E1068"},
		{source: "func(number):void", diagnostic: "vim/E1069"},
	}
	for _, test := range invalid {
		t.Run("invalid "+test.source, func(t *testing.T) {
			typeNode, diagnostics := (Vim9TypeParser{}).Parse(test.source)
			if typeNode == nil || typeNode.Kind != TypeFunction || len(diagnostics) != 1 || diagnostics[0].Code != test.diagnostic {
				t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
			}
		})
	}
}

func TestVim9TypeParserFunctionOptionalAndVariadicArguments(t *testing.T) {
	valid := []string{
		"func(?number, ?string): string",
		"func(number, ...list<string>): number",
		"func(?number, ...list<string>): number",
	}
	for _, source := range valid {
		t.Run("valid "+source, func(t *testing.T) {
			typeNode, diagnostics := (Vim9TypeParser{}).Parse(source)
			if typeNode == nil || typeNode.Kind != TypeFunction || len(typeNode.Arguments) != 2 || len(diagnostics) != 0 {
				t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
			}
		})
	}

	invalid := []struct {
		source     string
		diagnostic string
		arguments  int
	}{
		{source: "func(?number, string)", diagnostic: "vim/E1007", arguments: 2},
		{source: "func(...any)", diagnostic: "vim/E1180", arguments: 1},
		{source: "func(...bool)", diagnostic: "vim/E1180", arguments: 1},
		{source: "func(...list<number>, string)", diagnostic: "vim/E110", arguments: 2},
		{source: "func(...list<number>, ?string)", diagnostic: "vim/E110", arguments: 2},
	}
	for _, test := range invalid {
		t.Run("invalid "+test.source, func(t *testing.T) {
			typeNode, diagnostics := (Vim9TypeParser{}).Parse(test.source)
			if typeNode == nil || typeNode.Kind != TypeFunction || len(typeNode.Arguments) != test.arguments || len(diagnostics) != 1 || diagnostics[0].Code != test.diagnostic {
				t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
			}
		})
	}
}

func TestVim9TypeParserTupleWhitespaceAndSeparators(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		diagnostic string
		arguments  int
	}{
		{name: "space after opener", source: "tuple< number>", diagnostic: "vim/E1010", arguments: 1},
		{name: "space before closer", source: "tuple<number >", diagnostic: "vim/E488", arguments: 1},
		{name: "space before comma", source: "tuple<number , string>", diagnostic: "vim/E1068", arguments: 2},
		{name: "missing space after comma", source: "tuple<number,string>", diagnostic: "vim/E1069", arguments: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			typeNode, diagnostics := (Vim9TypeParser{}).Parse(test.source)
			if typeNode == nil || typeNode.Name != "tuple" || len(typeNode.Arguments) != test.arguments || len(diagnostics) != 1 || diagnostics[0].Code != test.diagnostic {
				t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
			}
		})
	}
}

func TestVim9TypeParserTupleVariadicMember(t *testing.T) {
	for _, source := range []string{
		"tuple<...list<number>>",
		"tuple<number, ...list<string>>",
	} {
		t.Run("valid "+source, func(t *testing.T) {
			typeNode, diagnostics := (Vim9TypeParser{}).Parse(source)
			if typeNode == nil || typeNode.Name != "tuple" || len(diagnostics) != 0 {
				t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
			}
		})
	}

	invalid := []struct {
		source     string
		diagnostic string
		arguments  int
	}{
		{source: "tuple<number, ...>", diagnostic: "vim/E1010", arguments: 2},
		{source: "tuple<number, ...number>", diagnostic: "vim/E1539", arguments: 2},
		{source: "tuple<number, ...list<string>, bool>", diagnostic: "vim/E1008", arguments: 3},
	}
	for _, test := range invalid {
		t.Run("invalid "+test.source, func(t *testing.T) {
			typeNode, diagnostics := (Vim9TypeParser{}).Parse(test.source)
			if typeNode == nil || len(typeNode.Arguments) != test.arguments || len(diagnostics) != 1 || diagnostics[0].Code != test.diagnostic {
				t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
			}
		})
	}
}

func TestVim9TypeParserContainerErrorRecoversNextCommand(t *testing.T) {
	// Vim v9.2.1015 src/testdir/test_vim9_func.vim checks the same bare list
	// return type in a :def defined from a legacy script.
	file := Parse("def Func(): list\n  return []\nenddef\nlet after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1008" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	function := file.Commands[0].Function
	if function == nil || function.ReturnType == nil || function.ReturnType.Name != "list" {
		t.Fatalf("function = %#v", function)
	}
	last := file.Commands[len(file.Commands)-1]
	if last.Canonical != "let" || last.Declaration == nil || file.Text(last.Declaration.Name) != "after" {
		t.Fatalf("last command = %#v", last)
	}
}

func TestOfficialQualifiedOptionalAndVariadicFunctionTypes(t *testing.T) {
	// v9.2.1015 runtime/doc/vim9.txt *vim9-types* and
	// src/testdir/test_vim9_func.vim.
	typeNode, diagnostics := (Vim9TypeParser{}).Parse("func(pkg.Item, ?string, ...list<number>): module.Result")
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if typeNode.Kind != TypeFunction || len(typeNode.Arguments) != 3 || typeNode.Arguments[0].Name != "pkg.Item" || typeNode.Arguments[1].Kind != TypeOptional || typeNode.Arguments[2].Kind != TypeVariadic || typeNode.ReturnType == nil || typeNode.ReturnType.Name != "module.Result" {
		t.Fatalf("function type = %#v", typeNode)
	}
}

func TestDeclarationOwnsParsedVim9Type(t *testing.T) {
	file := (Vim9Parser{}).Parse("var values: tuple<number, ...list<string>> = (1, 'a')\n")
	declaration := file.Commands[0].Declaration
	if len(file.Diagnostics) != 0 || declaration == nil || declaration.ParsedType == nil || declaration.ParsedType.Name != "tuple" || len(declaration.ParsedType.Arguments) != 2 {
		t.Fatalf("declaration = %#v, diagnostics = %#v", declaration, file.Diagnostics)
	}
}

func FuzzVim9TypeNeverPanics(f *testing.F) {
	f.Add("func(tuple<number, string>, ...list<any>): dict<bool>")
	f.Add("list<list<list<")
	f.Fuzz(func(t *testing.T, source string) {
		typeNode, _ := (Vim9TypeParser{}).Parse(source)
		if typeNode == nil {
			t.Fatal("nil type")
		}
	})
}
