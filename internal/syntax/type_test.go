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
	if typeNode == nil || len(diagnostics) != 1 || diagnostics[0].Code != "vimls/missing-type-delimiter" {
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

func TestVim9TypeParserBareFuncRemainsValid(t *testing.T) {
	typeNode, diagnostics := (Vim9TypeParser{}).Parse("func")
	if typeNode == nil || typeNode.Kind != TypeFunction || len(diagnostics) != 0 {
		t.Fatalf("type = %#v, diagnostics = %#v", typeNode, diagnostics)
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
