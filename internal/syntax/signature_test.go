package syntax

import "testing"

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
