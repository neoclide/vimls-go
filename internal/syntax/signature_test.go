package syntax

import "testing"

func TestVim9NestedUnsupportedFunctionNamespaces(t *testing.T) {
	file := Parse("vim9script\ndef Outer()\n  def s:Nested()\n  enddef\n  def b:Nested()\n  enddef\nenddef\nvar after = 1\n")
	if len(file.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	for i, name := range []string{"s:Nested", "b:Nested"} {
		if file.Diagnostics[i].Code != "vim/E1075" || file.Diagnostics[i].Message != "Namespace not supported: "+name || file.Text(file.Diagnostics[i].Span) != name {
			t.Fatalf("diagnostic = %#v", file.Diagnostics[i])
		}
	}
	if file.Commands[len(file.Commands)-1].Declaration == nil {
		t.Fatalf("next-line recovery = %#v", file.Commands)
	}
}

func TestParsesVim9GenericFunctionSignature(t *testing.T) {
	file := (Vim9Parser{}).Parse("def Zip<T, U>(first: list<T>, second: list<U> = []): list<tuple<T, U>>\nenddef\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("file = %#v", file)
	}
	function := file.Commands[0].Function
	if function == nil || file.Text(function.Name) != "Zip" || len(function.TypeParameters) != 2 || function.TypeParameters[0].Name != "T" || len(function.Parameters) != 2 {
		t.Fatalf("function = %#v", function)
	}
	if function.Parameters[0].Type == nil || function.Parameters[0].Type.Name != "list" || function.Parameters[1].Default == nil || function.Parameters[1].Default.Kind != ExpressionList {
		t.Fatalf("parameters = %#v", function.Parameters)
	}
	if function.ReturnType == nil || function.ReturnType.Name != "list" || len(function.ReturnType.Arguments) != 1 || function.ReturnType.Arguments[0].Name != "tuple" {
		t.Fatalf("return type = %#v", function.ReturnType)
	}
}

func TestVim9GenericFunctionTypeParameterGrammar(t *testing.T) {
	for _, signature := range []string{
		"Fn<A, B>()",
		"Fn<MyType1, My_Type2>()",
	} {
		t.Run("valid "+signature, func(t *testing.T) {
			file := Parse("vim9script\ndef " + signature + "\nenddef\n")
			if len(file.Diagnostics) != 0 || len(file.Commands) < 2 || file.Commands[1].Function == nil || len(file.Commands[1].Function.TypeParameters) != 2 {
				t.Fatalf("signature=%q file=%#v", signature, file)
			}
		})
	}

	invalid := []struct {
		signature  string
		diagnostic string
	}{
		{signature: "Fn <A>()", diagnostic: "vim/E1068"},
		{signature: "Fn <A> ()", diagnostic: "vim/E1068"},
		{signature: "Fn<A> ()", diagnostic: "vim/E1068"},
		{signature: "Fn< A>()", diagnostic: "vim/E1202"},
		{signature: "Fn<A >()", diagnostic: "vim/E1202"},
		{signature: "Fn<A,>()", diagnostic: "vim/E1069"},
		{signature: "Fn<A, >()", diagnostic: "vim/E1008"},
		{signature: "Fn<, A>()", diagnostic: "vim/E1008"},
		{signature: "Fn<,A>()", diagnostic: "vim/E1008"},
		{signature: "Fn< , A>()", diagnostic: "vim/E1202"},
		{signature: "Fn<A,B>()", diagnostic: "vim/E1069"},
		{signature: "Fn<A , B>()", diagnostic: "vim/E1202"},
		{signature: "Fn<t>()", diagnostic: "vim/E1552"},
		{signature: "Fn<My-type>()", diagnostic: "vim/E1553"},
		{signature: "Fn<>()", diagnostic: "vim/E1555"},
		{signature: "Fn<T, A, T>()", diagnostic: "vim/E1561"},
	}
	for _, test := range invalid {
		t.Run("invalid "+test.signature, func(t *testing.T) {
			source := "vim9script\ndef " + test.signature + "\nenddef\nvar after = 1\n"
			file := Parse(source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.diagnostic {
				t.Fatalf("signature=%q diagnostics=%#v", test.signature, file.Diagnostics)
			}
			if len(file.Commands) == 0 || file.Commands[1].Function == nil {
				t.Fatalf("signature=%q function recovery=%#v", test.signature, file.Commands)
			}
			last := file.Commands[len(file.Commands)-1]
			if last.Canonical != "var" || last.Declaration == nil || file.Text(last.Declaration.Name) != "after" {
				t.Fatalf("signature=%q next command=%#v", test.signature, last)
			}
		})
	}
}

func TestParsesLegacyFunctionSignatureAndAttributes(t *testing.T) {
	file := (LegacyParser{}).Parse("function! s:Collect(items, ...) abort dict\nendfunction\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	function := file.Commands[0].Function
	if function == nil || file.Text(function.Name) != "s:Collect" || len(function.Parameters) != 2 || !function.Parameters[1].Variadic || file.Text(function.Attributes) != "abort dict" {
		t.Fatalf("function = %#v", function)
	}
}

func TestLegacyFunctionSignatureGrammarInVim9Script(t *testing.T) {
	// v9.2.1015 runtime/doc/vim9.txt says :function keeps legacy syntax in a
	// Vim9 script.  The official suite covers comma spacing and anonymous ...;
	// src/userfunc.c keeps types and generic parameters exclusive to :def.
	valid := Parse("vim9script\nfunc Legacy(first,second = {x -> x},...) \" comment\nendfunc\n")
	if len(valid.Diagnostics) != 0 || len(valid.Commands) != 3 || valid.Commands[1].Function == nil {
		t.Fatalf("valid legacy signature = %#v", valid)
	}
	parameters := valid.Commands[1].Function.Parameters
	if len(parameters) != 3 || parameters[1].Default == nil || parameters[1].Default.Kind != ExpressionLambda || !parameters[2].Variadic || parameters[2].Name.Start != parameters[2].Name.End {
		t.Fatalf("parameters = %#v", parameters)
	}

	for _, test := range []struct {
		name   string
		header string
		code   string
	}{
		{name: "parameter type", header: "Typed(value: number)", code: "vim/E475"},
		{name: "return type", header: "Returns(): number", code: "vim/E488"},
		{name: "generic parameters", header: "Generic<T>()", code: "vim/E124"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse("vim9script\nfunc " + test.header + "\nendfunc\nvar after = 1\n")
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || len(file.Commands) != 4 || file.Commands[1].Function == nil {
				t.Fatalf("header=%q file=%#v", test.header, file)
			}
			function := file.Commands[1].Function
			if len(function.TypeParameters) != 0 || function.ReturnType != nil {
				t.Fatalf("header=%q function=%#v", test.header, function)
			}
			last := file.Commands[len(file.Commands)-1]
			if last.Canonical != "var" || last.Declaration == nil || file.Text(last.Declaration.Name) != "after" {
				t.Fatalf("header=%q recovery=%#v", test.header, file.Commands)
			}
		})
	}
}

func TestOfficialMultilineVim9FunctionHeader(t *testing.T) {
	// v9.2.1015 runtime/doc/vim9.txt *vim9-line-continuation* and
	// src/testdir/test_vim9_func.vim.
	file := Parse("vim9script\ndef Join(\n  text: string,\n  separator: string = '-'\n  ): string\n  return text .. separator\nenddef\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	function := file.Commands[1].Function
	if function == nil || len(function.Parameters) != 2 || file.Text(function.Parameters[1].Name) != "separator" || function.Parameters[1].Default == nil || function.ReturnType == nil || function.ReturnType.Name != "string" {
		t.Fatalf("function = %#v, argument = %q", function, file.Text(file.Commands[1].Argument))
	}
}

func TestOfficialMultilineLegacyFunctionHeaderInVim9Script(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_cmd.vim Test_method_call_linebreak.
	file := Parse("vim9script\nvar res = []\nfunc RetArg(\n  arg\n  )\n  let s:res = a:arg\nendfunc\n[1,\n  2,\n  3]->RetArg()\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 6 {
		t.Fatalf("commands = %#v", file.Commands)
	}
	function := file.Commands[2].Function
	if function == nil || len(function.Parameters) != 1 || file.Text(function.Parameters[0].Name) != "arg" {
		t.Fatalf("function = %#v, argument = %q", function, file.Text(file.Commands[2].Argument))
	}
	if file.Commands[3].Dialect != Legacy || file.Commands[3].Canonical != "let" || file.Commands[3].Declaration == nil {
		t.Fatalf("legacy function body = %#v", file.Commands[3])
	}
	if countTokens(file, TokenContinuation) != 4 {
		t.Fatalf("tokens = %#v", file.Tokens)
	}
}

func TestOfficialGenericReturnTypeEndsFunctionHeader(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_script.vim
	// Test_block_local_vars_with_func.
	file := Parse("vim9script\ndef Values(): list<string>\n  return []\nenddef\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 || file.Commands[1].Function.ReturnType.Name != "list" || countTokens(file, TokenContinuation) != 0 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
}

func TestOfficialComparisonInDefaultDoesNotHideFollowingParameter(t *testing.T) {
	// Accepted by Vim v9.2.1015.  A comparison operator is not a generic
	// delimiter when splitting the function parameter list.
	file := Parse("vim9script\ndef F(a = 1 < 2 ? 1 : 2, b = 0): void\nenddef\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || file.Commands[1].Function == nil || len(file.Commands[1].Function.Parameters) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	parameters := file.Commands[1].Function.Parameters
	if file.Text(parameters[0].Name) != "a" || parameters[0].Default == nil || file.Text(parameters[1].Name) != "b" || parameters[1].Default == nil {
		t.Fatalf("parameters = %#v", parameters)
	}
}

func TestOfficialVim9FunctionSignatureRejectsSpaceBeforeParen(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_func.vim Test_white_space_before_paren.
	for _, source := range []string{
		"vim9script\ndef Test ()\nenddef\n",
		"vim9script\nfunc Test ()\nendfunc\n",
		"def Test ()\nenddef\n",
		"vim9script\ndef Generic<T> ()\nenddef\n",
	} {
		file := Parse(source)
		if !hasDiagnostic(file, "vim/E1068") {
			t.Fatalf("source = %q, diagnostics = %#v", source, file.Diagnostics)
		}
	}
	legacy := (LegacyParser{}).Parse("func Test ()\nendfunc\n")
	if len(legacy.Diagnostics) != 0 {
		t.Fatalf("legacy diagnostics = %#v", legacy.Diagnostics)
	}
}

func TestVim9FunctionSignatureTypeAndCommaSpacing(t *testing.T) {
	valid := Parse("vim9script\ndef Func(a: number, b: string): bool\n  return true\nenddef\n")
	if len(valid.Diagnostics) != 0 || len(valid.Commands) < 2 || valid.Commands[1].Function == nil {
		t.Fatalf("valid signature = %#v", valid)
	}

	invalid := []struct {
		header     string
		diagnostic string
	}{
		{header: "Func():number", diagnostic: "vim/E1069"},
		{header: "Func(items:string)", diagnostic: "vim/E1069"},
		{header: "Func(...items:list<number>)", diagnostic: "vim/E1069"},
		{header: "Func(a: number , b: number)", diagnostic: "vim/E1068"},
		{header: "Func(a: number,b: number)", diagnostic: "vim/E1069"},
	}
	for _, test := range invalid {
		t.Run(test.header, func(t *testing.T) {
			file := Parse("vim9script\ndef " + test.header + "\nenddef\nvar after = 1\n")
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.diagnostic {
				t.Fatalf("header=%q diagnostics=%#v", test.header, file.Diagnostics)
			}
			last := file.Commands[len(file.Commands)-1]
			if last.Canonical != "var" || last.Declaration == nil || file.Text(last.Declaration.Name) != "after" {
				t.Fatalf("header=%q recovery=%#v", test.header, file.Commands)
			}
		})
	}
}

func TestVim9FunctionSignatureMissingVariadicName(t *testing.T) {
	file := Parse("def Func4(...)\necho \"a\"\nenddef\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1055" || file.Diagnostics[0].Message != "missing name after ..." {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 3 || file.Commands[0].Function == nil || len(file.Commands[0].Function.Parameters) != 1 {
		t.Fatalf("commands = %#v", file.Commands)
	}
	parameter := file.Commands[0].Function.Parameters[0]
	if !parameter.Variadic || parameter.Name.Start != parameter.Name.End {
		t.Fatalf("parameter = %#v", parameter)
	}
	if file.Commands[1].Canonical != "echo" || file.Commands[2].Canonical != "enddef" || len(file.Blocks) != 1 {
		t.Fatalf("recovery commands = %#v, blocks = %#v", file.Commands, file.Blocks)
	}
}

func TestIncompleteFunctionSignatureRecovers(t *testing.T) {
	file := (Vim9Parser{}).Parse("def Broken<T(value: list<number>\nvar after = 1\n")
	found := false
	for _, diagnostic := range file.Diagnostics {
		found = found || diagnostic.Code == "vimls/missing-generic-end"
	}
	if len(file.Commands) < 1 || file.Commands[0].Function == nil || !found {
		t.Fatalf("file = %#v", file)
	}
}

func TestVim9ConstructorParameterTarget(t *testing.T) {
	source := "vim9script\ndef new(\n  this.name,\n  this.age: number = v:none,\n  this.\n)\nenddef\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) < 2 || file.Commands[1].Function == nil {
		t.Fatalf("file = %#v", file)
	}
	parameters := file.Commands[1].Function.Parameters
	if len(parameters) != 3 {
		t.Fatalf("parameters = %#v", parameters)
	}
	want := []struct {
		name     string
		member   string
		operator string
	}{
		{name: "this.name", member: "name", operator: "."},
		{name: "this.age", member: "age", operator: "."},
		{name: "this.", member: "", operator: "."},
	}
	for index, expected := range want {
		parameter := parameters[index]
		if file.Text(parameter.Name) != expected.name || parameter.Target == nil || parameter.Target.Kind != ExpressionMember || file.Text(parameter.Target.Span) != expected.name || parameter.Target.Value != expected.member || file.Text(parameter.Target.Operator) != expected.operator {
			t.Fatalf("parameter %d = %#v, text=%q", index, parameter, file.Text(parameter.Name))
		}
		if len(parameter.Target.Children) != 1 || parameter.Target.Children[0].Kind != ExpressionIdentifier || file.Text(parameter.Target.Children[0].Span) != "this" || parameter.Target.Children[0].Value != "this" {
			t.Fatalf("parameter %d target children = %#v", index, parameter.Target.Children)
		}
	}
	if parameters[1].Type == nil || file.Text(parameters[1].TypeSpan) != "number" || parameters[1].Default == nil || file.Text(parameters[1].DefaultSpan) != "v:none" {
		t.Fatalf("typed/default constructor parameter = %#v", parameters[1])
	}

	for _, source := range []string{
		"vim9script\ndef new(thisArg, this.a.b)\nenddef\n",
		"vim9script\ndef new(this.1, this.a-b)\nenddef\n",
	} {
		file := Parse(source)
		if len(file.Commands) < 2 || file.Commands[1].Function == nil {
			t.Fatalf("source=%q file=%#v", source, file)
		}
		for _, parameter := range file.Commands[1].Function.Parameters {
			if parameter.Target != nil {
				t.Fatalf("source=%q unexpectedly recognized target=%#v", source, parameter.Target)
			}
		}
	}

	legacy := (LegacyParser{}).Parse("function s:new(this.name)\nendfunction\n")
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) == 0 || legacy.Commands[0].Function == nil || legacy.Commands[0].Function.Parameters[0].Target != nil {
		t.Fatalf("legacy constructor parameter = %#v", legacy)
	}
}
