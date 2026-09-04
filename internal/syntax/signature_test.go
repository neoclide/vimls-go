package syntax

import "testing"

func TestFunctionMissingOpeningParenthesisDiagnostic(t *testing.T) {
	for _, test := range []struct {
		name, source, message, span string
	}{
		{
			name:    "legacy function",
			source:  "function Xfunc abc ()\nendfunction\nlet after = 1\n",
			message: "Missing '(': Xfunc abc ()",
			span:    "abc ()",
		},
		{
			name:    "Vim9-root function",
			source:  "vim9script\nfunction Foo abc ()\nendfunction\nvar after = 1\n",
			message: "Missing '(': Foo abc ()",
			span:    "abc ()",
		},
		{
			name:    "legacy function generic-like tail",
			source:  "function Foo<T>()\nendfunction\nlet after = 1\n",
			message: "Missing '(': Foo<T>()",
			span:    "<T>()",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E124" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E488" {
					t.Fatalf("E124 source retained trailing-character fallback: %#v", file.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E124 diagnostics = %#v", file.Diagnostics)
			}
			foundFunction := false
			foundDeclaration := false
			for index := range file.Commands {
				foundFunction = foundFunction || file.Commands[index].Function != nil
				foundDeclaration = foundDeclaration || file.Commands[index].Declaration != nil
			}
			if !foundFunction || !foundDeclaration {
				t.Fatalf("function or next declaration was not retained: %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"function Foo()\nendfunction\n",
		"vim9script\ndef Foo()\nenddef\n",
		"function Foo\n",
		"function Foo \" comment\n",
		"vim9script\ndef Foo\n",
		"vim9script\ndef Foo ()\nenddef\n",
		"function Foo(\n",
	} {
		file := Parse(source)
		if hasDiagnostic(file, "vim/E124") {
			t.Fatalf("guard source unexpectedly received E124: %#v\n%s", file.Diagnostics, source)
		}
	}
}

func TestFunctionIllegalArgumentDiagnostic(t *testing.T) {
	for _, test := range []struct {
		name, source, message, span string
	}{
		{
			name:    "comment starts Vim9 arguments",
			source:  "vim9script\ndef Func(# comment\n  arg: string)\nenddef\nvar after = 1\n",
			message: "Illegal argument: # comment",
			span:    "# comment",
		},
		{
			name:    "multiline missing default",
			source:  "vim9script\ndef Func(f=\n)\nenddef\nvar after = 1\n",
			message: "Illegal argument: )",
			span:    ")",
		},
		{
			name:    "legacy unclosed empty arguments",
			source:  "function Xfunc(\n",
			message: "Illegal argument: ",
			span:    "",
		},
		{
			name:    "digit starts argument",
			source:  "function Foo(1arg)\nendfunction\nlet after = 1\n",
			message: "Illegal argument: 1arg)",
			span:    "1arg",
		},
		{
			name:    "invalid argument character",
			source:  "vim9script\ndef Func(foo-bar: number)\nenddef\nvar after = 1\n",
			message: "Illegal argument: foo-bar: number)",
			span:    "foo-bar",
		},
		{
			name:    "legacy reserved argument",
			source:  "function Foo(firstline)\nendfunction\nlet after = 1\n",
			message: "Illegal argument: firstline)",
			span:    "firstline",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E125" {
					got = append(got, diagnostic)
				}
				if test.name == "multiline missing default" && diagnostic.Code == "vim/E15" {
					t.Fatalf("multiline missing default retained E15: %#v", file.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E125 diagnostics = %#v", file.Diagnostics)
			}
			if test.name != "legacy unclosed empty arguments" && file.Commands[len(file.Commands)-1].Declaration == nil {
				t.Fatalf("following declaration was not retained: %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef Func(arg: number = )\nenddef\n",
		"vim9script\ndef Func(arg: number)\nenddef\n",
		"vim9script\ndef Func(_)\nenddef\n",
		"function Foo(...)\nendfunction\n",
		"function Foo(arg\n",
		"vim9script\ndef Func(\n",
	} {
		file := Parse(source)
		if hasDiagnostic(file, "vim/E125") {
			t.Fatalf("guard source unexpectedly received E125: %#v\n%s", file.Diagnostics, source)
		}
	}
}

func TestFunctionArgumentOrderDiagnostic(t *testing.T) {
	tests := []struct {
		name, source, span string
		parameterCount     int
	}{
		{
			name:           "legacy non-default after default",
			source:         "function Bad(a, b = 1, c)\nendfunction\n",
			span:           "c",
			parameterCount: 3,
		},
		{
			name:           "vim9 def non-default after default",
			source:         "vim9script\ndef Bad(a: number = 1, b: number)\nenddef\n",
			span:           "b",
			parameterCount: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			var function *Function
			foundEnd := false
			for _, command := range file.Commands {
				if command.Function != nil {
					function = command.Function
				}
				if command.Canonical == "endfunction" || command.Canonical == "enddef" {
					foundEnd = true
				}
			}
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E989" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Non-default argument follows default argument" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E989 diagnostics = %#v", got)
			}
			if function == nil || len(function.Parameters) != test.parameterCount {
				t.Fatalf("function parameters = %#v, want %d", file.Commands, test.parameterCount)
			}
			if !foundEnd {
				t.Fatalf("end command not recovered: %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"function Good(a, b = 1)\nendfunction\n",
		"function Good(a = 1, b = 2)\nendfunction\n",
		"function Good(a = 1, ...)\nendfunction\n",
		"function Bad(a =, b)\nendfunction\n",
	} {
		file := Parse(source)
		if hasDiagnostic(file, "vim/E989") {
			t.Fatalf("guard unexpectedly received E989: %#v", file.Diagnostics)
		}
	}
}

func TestDuplicateFunctionArgumentNameDiagnostic(t *testing.T) {
	for _, test := range []struct {
		name, source, message, span string
		argCount                    int
	}{
		{
			name:     "legacy duplicate argument",
			source:   "function Bad(a, a)\nendfunction\n",
			message:  "Duplicate argument name: a",
			span:     "a",
			argCount: 2,
		},
		{
			name:     "vim9 duplicate argument",
			source:   "vim9script\ndef Bad(a: number, a: number)\nenddef\n",
			message:  "Duplicate argument name: a",
			span:     "a",
			argCount: 2,
		},
		{
			name:     "legacy duplicate underscore arguments",
			source:   "function Bad(_, _)\nendfunction\n",
			message:  "Duplicate argument name: _",
			span:     "_",
			argCount: 2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			var function *Function
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E853" {
					got = append(got, diagnostic)
				}
			}
			for _, command := range file.Commands {
				if command.Function != nil {
					function = command.Function
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E853 diagnostics = %#v", got)
			}
			if function == nil || len(function.Parameters) != test.argCount || file.Text(function.Parameters[1].Name) != test.span {
				t.Fatalf("function AST = %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"function Bad(a, a, a)\nendfunction\n",
		"vim9script\ndef Good(_, _)\nenddef\n",
	} {
		file := Parse(source)
		if source == "function Bad(a, a, a)\nendfunction\n" {
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E853" || file.Text(file.Diagnostics[0].Span) != "a" {
				t.Fatalf("legacy triple duplicate diagnostics = %#v", file.Diagnostics)
			}
			continue
		}
		if hasDiagnostic(file, "vim/E853") {
			t.Fatalf("guard unexpectedly received E853: %#v", file.Diagnostics)
		}
	}

	file := Parse("function Outer(a)\n  function Inner(a, a)\n  endfunction\nendfunction\n")
	var got []Diagnostic
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code == "vim/E853" {
			got = append(got, diagnostic)
		}
	}
	if len(got) != 1 {
		t.Fatalf("nested function diagnostics = %#v", file.Diagnostics)
	}
	var innerFunction *Function
	for _, command := range file.Commands {
		if command.Function != nil && file.Text(command.Function.Name) == "Inner" {
			innerFunction = command.Function
		}
	}
	if innerFunction == nil || len(innerFunction.Parameters) != 2 || file.Text(got[0].Span) != file.Text(innerFunction.Parameters[1].Name) || got[0].Span.Start != innerFunction.Parameters[1].Name.Start {
		t.Fatalf("inner duplicate diagnostic/span = %#v, function = %#v", got, innerFunction)
	}
}

func TestFunctionClosureDisallowedAtTopLevelDiagnostic(t *testing.T) {
	for _, test := range []struct {
		name, source, message, span string
	}{
		{
			name:    "legacy top-level closure",
			source:  "function F1() closure\nendfunction\n",
			message: "Closure function should not be at top level: F1",
			span:    "F1",
		},
		{
			name:    "vim9-root function closure",
			source:  "vim9script\nfunction F2() closure\nendfunction\n",
			message: "Closure function should not be at top level: F2",
			span:    "F2",
		},
		{
			name:    "abort closure order",
			source:  "function F3() abort closure\nendfunction\n",
			message: "Closure function should not be at top level: F3",
			span:    "F3",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E932" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E932 diagnostics = %#v", got)
			}
		})
	}

	for _, source := range []string{
		"function Outer()\n  function Inner() closure\n  endfunction\nendfunction\n",
		"vim9script\ndef Outer()\n  function Inner() closure\n  endfunction\nenddef\n",
		"vim9script\nfunction Normal()\nendfunction\n",
	} {
		file := Parse(source)
		if hasDiagnostic(file, "vim/E932") {
			t.Fatalf("guard unexpectedly received E932: %#v", file.Diagnostics)
		}
	}
}

func TestLegacyFunctionNameCapitalDiagnostic(t *testing.T) {
	for _, test := range []struct {
		name, source, function string
	}{
		{
			name:     "plain lowercase",
			source:   "function xfunc()\nendfunction\nlet after = 1\n",
			function: "xfunc",
		},
		{
			name:     "explicit global",
			source:   "function! g:test()\nendfunction\nlet after = 1\n",
			function: "g:test",
		},
		{
			name:     "lowercase before comment",
			source:   "function! test2() \"#\nendfunction\nlet after = 1\n",
			function: "test2",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E128" {
					got = append(got, diagnostic)
				}
			}
			message := `Function name must start with a capital or "s:": ` + test.function
			if len(got) != 1 || got[0].Message != message || file.Text(got[0].Span) != test.function {
				t.Fatalf("E128 diagnostics = %#v", file.Diagnostics)
			}
			if file.Commands[len(file.Commands)-1].Declaration == nil {
				t.Fatalf("following declaration was not retained: %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"function Capital()\nendfunction\n",
		"function s:local()\nendfunction\n",
		"function lower#autoload()\nendfunction\n",
		"function object.method()\nendfunction\n",
		"function b:local()\nendfunction\n",
		"vim9script\nfunction lower()\nendfunction\n",
		"def lower()\nenddef\n",
	} {
		file := Parse(source)
		if hasDiagnostic(file, "vim/E128") {
			t.Fatalf("guard source unexpectedly received E128: %#v\n%s", file.Diagnostics, source)
		}
	}
}

func TestFunctionNameColonDiagnostic(t *testing.T) {
	for _, test := range []struct{ source, name string }{
		{"function b:test()\nendfunction\n", "b:test"},
		{"function s:Good:bad()\nendfunction\n", "s:Good:bad"},
		{"vim9script\ndef <SID>: list<string>\n", "<SID>:"},
		{"vim9script\ndef <SID>:Name()\nenddef\n", "<SID>:Name"},
		{"vim9script\ndef b:Name()\nenddef\n", "b:Name"},
	} {
		file := Parse(test.source)
		var got []Diagnostic
		for _, diagnostic := range file.Diagnostics {
			if diagnostic.Code == "vim/E884" {
				got = append(got, diagnostic)
			}
		}
		if len(got) != 1 || got[0].Message != "Function name cannot contain a colon: "+test.name || file.Text(got[0].Span) != test.name {
			t.Fatalf("E884 diagnostics = %#v\n%s", file.Diagnostics, test.source)
		}
	}

	for _, source := range []string{
		"function s:local()\nendfunction\n",
		"function! g:Global()\nendfunction\n",
		"vim9script\ndef s:Name()\nenddef\n",
		"vim9script\ndef Outer()\n  def b:Nested()\n  enddef\nenddef\n",
	} {
		if file := Parse(source); hasDiagnostic(file, "vim/E884") {
			t.Fatalf("guard unexpectedly received E884: %#v\n%s", file.Diagnostics, source)
		}
	}
}

func TestVim9VariadicDefaultDiagnostic(t *testing.T) {
	tests := []struct {
		name, source, span, defaultText string
	}{
		{"typed", "vim9script\ndef Func(...items: list<any> = [1])\nenddef\nvar after = 1\n", "= [1]", "[1]"},
		{"untyped", "vim9script\ndef Func(...items = 'value')\nenddef\nvar after = 1\n", "= 'value'", "'value'"},
		{"official multiline", "vim9script\ndef Func(  # some comment\n          ...items = []\n          )\n  echo items\nenddef\nvar after = 1\n", "= []", "[]"},
		{"Legacy-root def", "def Func(...items: list<any> = [1])\nenddef\nlet after = 1\n", "= [1]", "[1]"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1160" {
					got = append(got, diagnostic)
				}
			}
			if len(file.Diagnostics) != 1 || len(got) != 1 {
				t.Fatalf("diagnostics = %#v, want one E1160", file.Diagnostics)
			}
			if got[0].Message != "Cannot use a default for variable arguments" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1160 diagnostic = %#v on %q", got[0], file.Text(got[0].Span))
			}
			var function *Function
			foundEnd := false
			for index := range file.Commands {
				if file.Commands[index].Function != nil {
					function = file.Commands[index].Function
				}
				foundEnd = foundEnd || file.Commands[index].Canonical == "enddef"
			}
			if function == nil || len(function.Parameters) != 1 || !function.Parameters[0].Variadic ||
				file.Text(function.Parameters[0].Name) != "items" || function.Parameters[0].Default == nil ||
				file.Text(function.Parameters[0].DefaultSpan) != test.defaultText || file.Text(function.Parameters[0].Default.Span) != test.defaultText ||
				!foundEnd || file.Commands[len(file.Commands)-1].Declaration == nil {
				t.Fatalf("parameter/recovery = %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef Func(...items: list<any>)\nenddef\n",
		"vim9script\ndef Func(item: number = 1)\nenddef\n",
		"function Func(...)\nendfunction\n",
		"vim9script\nvar Callback = (item: number = 1) => item\n",
	} {
		file := Parse(source)
		if hasDiagnostic(file, "vim/E1160") {
			t.Fatalf("source unexpectedly received E1160: %#v", file.Diagnostics)
		}
	}
}

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
	if hasDiagnostic(file, "vim/E1268") {
		t.Fatalf("nested namespace precedence = %#v", file.Diagnostics)
	}
	if file.Commands[len(file.Commands)-1].Declaration == nil {
		t.Fatalf("next-line recovery = %#v", file.Commands)
	}
}

func TestVim9ScriptNamespaceFunctionDiagnostic(t *testing.T) {
	message := func(name string) string { return "Cannot use s: in Vim9 script: " + name }
	for _, test := range []struct {
		name, source, function string
	}{
		{name: "def", source: "vim9script\ndef s:Name()\nenddef\nvar after = 1\n", function: "s:Name"},
		{name: "function", source: "vim9script\nfunction s:Name()\nendfunction\nvar after = 1\n", function: "s:Name"},
		{name: "dictionary-shaped name", source: "vim9script\ndef s:Object.Method()\nenddef\nvar after = 1\n", function: "s:Object.Method"},
		{name: "autoload-shaped name", source: "vim9script\ndef s:name#Func()\nenddef\nvar after = 1\n", function: "s:name#Func"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1268" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1267" || diagnostic.Code == "vim/E1263" || diagnostic.Code == "vim/E1182" {
					t.Fatalf("unexpected competing name diagnostic: %#v", file.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != message(test.function) || file.Text(got[0].Span) != test.function {
				t.Fatalf("E1268 diagnostics = %#v", file.Diagnostics)
			}
			foundAfter := false
			for _, command := range file.Commands {
				foundAfter = foundAfter || command.Declaration != nil && file.Text(command.Declaration.Name) == "after"
			}
			if !foundAfter {
				t.Fatalf("following declaration was not retained: %#v", file.Commands)
			}
		})
	}
}

func TestVim9FunctionNameRequiredDiagnostic(t *testing.T) {
	for _, test := range []struct {
		name, source, span string
	}{
		{
			name:   "empty global name",
			source: "vim9script\ndef g: list<string>\nvar after = 1\n",
			span:   "g:",
		},
		{
			name:   "empty autoload part",
			source: "vim9script\ndef loadme#()\nenddef\nvar after = 1\n",
			span:   "loadme#",
		},
		{
			name:   "legacy comment in Vim9 query",
			source: "vim9script\nfunction \" comment\nvar after = 1\n",
			span:   "\" comment",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E129" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1263" || diagnostic.Code == "vim/E1267" {
					t.Fatalf("unexpected competing name diagnostic: %#v", file.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "Function name required" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E129 diagnostics = %#v", file.Diagnostics)
			}
			if file.Commands[len(file.Commands)-1].Declaration == nil {
				t.Fatalf("following declaration was not retained: %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef g:Global()\nenddef\n",
		"vim9script\ndef loadme#Func()\nenddef\n",
		"vim9script\nfunction # comment\n",
		"function loadme#()\nendfunction\n",
	} {
		file := Parse(source)
		if hasDiagnostic(file, "vim/E129") {
			t.Fatalf("unexpected E129: %#v", file.Diagnostics)
		}
	}
}

func TestVim9FunctionNameCapitalDiagnostic(t *testing.T) {
	for _, name := range []string{"_Foo", "lower", "g:globalFunc"} {
		t.Run(name, func(t *testing.T) {
			file := Parse("vim9script\ndef " + name + "()\nenddef\nvar after = 1\n")
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1267" || file.Diagnostics[0].Message != "Function name must start with a capital: "+name || file.Text(file.Diagnostics[0].Span) != name {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			if file.Commands[len(file.Commands)-1].Declaration == nil {
				t.Fatalf("next-line recovery = %#v", file.Commands)
			}
		})
	}
	for _, source := range []string{
		"vim9script\ndef Func()\nenddef\n",
		"vim9script\ndef g:GlobalFunc()\nenddef\n",
		"vim9script\nclass Thing\n  def _Foo()\n  enddef\nendclass\n",
		"def _Foo()\nenddef\nlet after = 1\n",
		"vim9script\nlegacy def _Foo()\nenddef\nvar after = 1\n",
	} {
		file := Parse(source)
		if hasDiagnostic(file, "vim/E1267") {
			t.Fatalf("unexpected E1267: %#v", file.Diagnostics)
		}
	}
}

func TestVim9AutoloadFunctionNameDiagnostic(t *testing.T) {
	message := "Cannot use name with # in Vim9 script, use export instead"
	for _, test := range []struct {
		name, source, function string
	}{
		{
			name:     "def",
			source:   "vim9script\ndef somescript#Func()\nenddef\nvar after = 1\n",
			function: "somescript#Func",
		},
		{
			name:     "function bang",
			source:   "vim9script\nfunction! Script#Func()\nendfunction\nvar after = 1\n",
			function: "Script#Func",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1263" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1267" {
					t.Fatalf("unexpected E1267: %#v", file.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != message || file.Text(got[0].Span) != test.function {
				t.Fatalf("E1263 diagnostics = %#v", file.Diagnostics)
			}
			foundAfter := false
			for _, command := range file.Commands {
				foundAfter = foundAfter || command.Declaration != nil && file.Text(command.Declaration.Name) == "after"
			}
			if !foundAfter {
				t.Fatalf("following declaration was not retained: %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"def g:some#func()\nenddef\nlet after = 1\n",
		"vim9script\nlegacy def Some#Func()\nenddef\nvar after = 1\n",
		"vim9script\nexport def Some#Func()\nenddef\n",
		"vim9script\ndef loadme#()\nenddef\n",
		"vim9script\ndef Func(value: string = '#')\n  var text = 'a#b'\nenddef\n# comment #\n",
	} {
		file := Parse(source)
		if hasDiagnostic(file, "vim/E1263") {
			t.Fatalf("unexpected E1263: %#v", file.Diagnostics)
		}
	}
}

func TestVim9DictionaryFunctionDiagnostic(t *testing.T) {
	tests := []struct {
		name, source string
		wantNames    []string
	}{
		{
			name:      "top-level function",
			source:    "vim9script\nfunction Object.Method()\nendfunction\nvar after = 1\n",
			wantNames: []string{"Object.Method"},
		},
		{
			name:      "top-level def",
			source:    "vim9script\ndef Object.Method()\nenddef\n",
			wantNames: []string{"Object.Method"},
		},
		{
			name:      "legacy-root def",
			source:    "def Object.Method()\nenddef\n",
			wantNames: []string{"Object.Method"},
		},
		{
			name:      "global receiver",
			source:    "vim9script\nfunction g:Object.Method()\nendfunction\n",
			wantNames: []string{"g:Object.Method"},
		},
		{
			name:      "vim9cmd function",
			source:    "vim9cmd function Object.Method()\nendfunction\n",
			wantNames: []string{"Object.Method"},
		},
		{
			name:      "legacy-root def body",
			source:    "def Define()\n  function s:Object.Method()\n  endfunction\n  def Object.Method()\n  enddef\nenddef\n",
			wantNames: []string{"s:Object.Method", "Object.Method"},
		},
		{
			name:      "lowercase receiver takes precedence",
			source:    "vim9script\nfunction object.Method()\nendfunction\n",
			wantNames: []string{"object.Method"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1182" {
					got = append(got, diagnostic)
				}
				if test.name == "lowercase receiver takes precedence" && diagnostic.Code == "vim/E1267" {
					t.Fatalf("unexpected E1267: %#v", file.Diagnostics)
				}
			}
			if len(got) != len(test.wantNames) {
				t.Fatalf("E1182 diagnostics = %#v", file.Diagnostics)
			}
			if len(file.Diagnostics) != len(got) {
				t.Fatalf("E1182 retained competing diagnostics: %#v", file.Diagnostics)
			}
			for index, name := range test.wantNames {
				if got[index].Message != "Cannot define a dict function in Vim9 script: "+name || file.Text(got[index].Span) != name {
					t.Fatalf("E1182 diagnostic = %#v on %q", got[index], file.Text(got[index].Span))
				}
				foundFunction := false
				for _, command := range file.Commands {
					foundFunction = foundFunction || command.Function != nil && file.Text(command.Function.Name) == name
				}
				if !foundFunction {
					t.Fatalf("function AST for %q was not retained: %#v", name, file.Commands)
				}
			}
			if test.name == "top-level function" && (file.Commands[len(file.Commands)-1].Declaration == nil || file.Text(file.Commands[len(file.Commands)-1].Declaration.Name) != "after") {
				t.Fatalf("following declaration was not retained: %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"function Object.Method()\nendfunction\n",
		"vim9script\nlegacy function Object.Method()\nendfunction\n",
		"vim9script\ndef Func()\nenddef\n",
		"vim9script\nclass Thing\n  def Method()\n  enddef\nendclass\n",
	} {
		file := Parse(source)
		if hasDiagnostic(file, "vim/E1182") {
			t.Fatalf("unexpected E1182: %#v", file.Diagnostics)
		}
	}
}

func TestVim9MissingArgumentTypeDiagnostic(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_func.vim:2484.
	file := (Vim9Parser{}).Parse("def Func5(items)\n  echo items\nenddef\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1077" || file.Diagnostics[0].Message != "Missing argument type for items" || file.Text(file.Diagnostics[0].Span) != "items" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 4 || file.Commands[0].Function == nil || len(file.Commands[0].Function.Parameters) != 1 || file.Commands[3].Declaration == nil {
		t.Fatalf("commands = %#v", file.Commands)
	}
	assertFileSpans(t, file)

	for _, source := range []string{
		"def Func(items: list<any>)\nenddef\n",
		"def Func(items = [])\nenddef\n",
		"def Func(_)\nenddef\n",
		"def Func(...items: list<any>)\nenddef\n",
		"class Thing\n  var value: number\n  def new(this.value)\n  enddef\nendclass\n",
	} {
		valid := (Vim9Parser{}).Parse(source)
		if hasDiagnostic(valid, "vim/E1077") {
			t.Fatalf("valid parameter reported E1077: %#v\n%s", valid.Diagnostics, source)
		}
	}
	legacy := (LegacyParser{}).Parse("function Func(items)\nendfunction\n")
	if hasDiagnostic(legacy, "vim/E1077") {
		t.Fatalf("legacy function diagnostics = %#v", legacy.Diagnostics)
	}
}

func TestVim9MissingReturnTypeDiagnostic(t *testing.T) {
	source := "def Func():\n  return 1\nenddef\nvar after = 1\n"
	file := Parse(source)
	if !hasDiagnostic(file, "vim/E1056") {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	var missing Diagnostic
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code == "vim/E1056" {
			missing = diagnostic
		}
	}
	if missing.Message != "Expected a type: " || missing.Span != (Span{Start: len("def Func():"), End: len("def Func():")}) || file.Text(missing.Span) != "" {
		t.Fatalf("diagnostic = %#v (%q)", missing, file.Text(missing.Span))
	}
	if len(file.Commands) != 4 || file.Commands[1].Canonical != "return" || file.Commands[3].Declaration == nil {
		t.Fatalf("recovery commands = %#v", file.Commands)
	}
	if function := file.Commands[0].Function; function == nil || function.ReturnType == nil || function.ReturnType.Kind != TypeMissing {
		t.Fatalf("return type = %#v", file.Commands[0].Function)
	}

	for _, source := range []string{
		"def Func()\nenddef\n",
		"def Func(): void\nenddef\n",
		"function Func():\nendfunction\n",
	} {
		valid := Parse(source)
		if hasDiagnostic(valid, "vim/E1056") {
			t.Fatalf("valid source reported E1056: %#v\n%s", valid.Diagnostics, source)
		}
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
		message    string
		span       string
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
		{signature: "Fn<t>()", diagnostic: "vim/E1552", message: "Type variable name must start with an uppercase letter: t>()", span: "t"},
		{signature: "Fn<My-type>()", diagnostic: "vim/E1553", message: "Missing comma after type in generic function: My-type>()", span: "-"},
		{signature: "Fn<T()", diagnostic: "vim/E1553", message: "Missing comma after type in generic function: T()", span: "T"},
		{signature: "Fn<>()", diagnostic: "vim/E1555", message: "Empty type list specified for generic function 'Fn'", span: "<>"},
		{signature: "Fn<T, A, T>()", diagnostic: "vim/E1561", message: "Duplicate type variable name: T", span: "T"},
	}
	for _, test := range invalid {
		t.Run("invalid "+test.signature, func(t *testing.T) {
			source := "vim9script\ndef " + test.signature + "\nenddef\nvar after = 1\n"
			file := Parse(source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.diagnostic {
				t.Fatalf("signature=%q diagnostics=%#v", test.signature, file.Diagnostics)
			}
			if test.message != "" && (file.Diagnostics[0].Message != test.message || file.Text(file.Diagnostics[0].Span) != test.span) {
				t.Fatalf("signature=%q diagnostic=%#v span=%q", test.signature, file.Diagnostics[0], file.Text(file.Diagnostics[0].Span))
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

func TestVim9NestedGenericTypeVariableDuplicate(t *testing.T) {
	file := Parse("vim9script\ndef Outer<T>()\n  def Inner<T>()\n  enddef\nenddef\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1561" || file.Diagnostics[0].Message != "Duplicate type variable name: T" || file.Text(file.Diagnostics[0].Span) != "T" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if file.Commands[len(file.Commands)-1].Declaration == nil {
		t.Fatalf("next-line recovery = %#v", file.Commands)
	}
	for _, source := range []string{
		"vim9script\ndef First<T>()\nenddef\ndef Second<T>()\nenddef\n",
		"vim9script\ndef Outer<T>()\n  def Inner<U>()\n  enddef\nenddef\n",
		"function Legacy<T>()\nendfunction\n",
	} {
		for _, diagnostic := range Parse(source).Diagnostics {
			if diagnostic.Code == "vim/E1561" {
				t.Fatalf("independent generic scopes reported E1561: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestParsesLegacyFunctionSignatureAndAttributes(t *testing.T) {
	file := (LegacyParser{}).Parse("function! s:Collect(items, ...) range abort dict\nendfunction\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	function := file.Commands[0].Function
	if function == nil || file.Text(function.Name) != "s:Collect" || len(function.Parameters) != 2 || !function.Parameters[1].Variadic || file.Text(function.AttributeTail) != " range abort dict" || len(function.Attributes) != 3 {
		t.Fatalf("function = %#v", function)
	}
	for index, want := range []string{"range", "abort", "dict"} {
		if got := file.Text(function.Attributes[index]); got != want {
			t.Fatalf("attribute %d = %q, want %q", index, got, want)
		}
	}

	withoutSpace := (LegacyParser{}).Parse("function! s:InstallOptions(...)abort\nendfunction\n")
	function = withoutSpace.Commands[0].Function
	if function == nil || len(function.Attributes) != 1 || withoutSpace.Text(function.Attributes[0]) != "abort" || len(withoutSpace.Diagnostics) != 0 {
		t.Fatalf("function attribute without space = %#v, diagnostics = %#v", function, withoutSpace.Diagnostics)
	}

	incomplete := (LegacyParser{}).Parse("function! Foo() cl\nendfunction\n")
	function = incomplete.Commands[0].Function
	if function == nil || incomplete.Text(function.AttributeTail) != " cl" || len(function.Attributes) != 0 || len(incomplete.Diagnostics) != 1 || incomplete.Diagnostics[0].Code != "vim/E488" {
		t.Fatalf("incomplete function = %#v, diagnostics = %#v", function, incomplete.Diagnostics)
	}

	continued := (LegacyParser{}).Parse("function! Continued()\n  \\ range abort\nendfunction\n")
	function = continued.Commands[0].Function
	if function == nil || len(function.Attributes) != 2 || continued.Text(function.Attributes[0]) != "range" || continued.Text(function.Attributes[1]) != "abort" || len(continued.Diagnostics) != 0 {
		t.Fatalf("continued function = %#v, diagnostics = %#v", function, continued.Diagnostics)
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
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1055" || file.Diagnostics[0].Message != "Missing name after ..." || file.Text(file.Diagnostics[0].Span) != "..." {
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
	for _, source := range []string{
		"def Valid(...items: list<any>)\nenddef\n",
		"function Legacy(...)\nendfunction\n",
	} {
		valid := Parse(source)
		for _, diagnostic := range valid.Diagnostics {
			if diagnostic.Code == "vim/E1055" {
				t.Fatalf("valid source reported E1055: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestIncompleteFunctionSignatureRecovers(t *testing.T) {
	file := (Vim9Parser{}).Parse("def Broken<T(value: list<number>\nvar after = 1\n")
	found := false
	for _, diagnostic := range file.Diagnostics {
		found = found || diagnostic.Code == "vim/E1553"
	}
	if len(file.Commands) < 1 || file.Commands[0].Function == nil || !found {
		t.Fatalf("file = %#v", file)
	}
}

func TestVim9ConstructorParameterTarget(t *testing.T) {
	source := "vim9script\nclass Person\n  def new(\n    this.name,\n    this.age: number = v:none,\n    this.\n  )\n  enddef\nendclass\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) < 3 || file.Commands[2].Function == nil {
		t.Fatalf("file = %#v", file)
	}
	parameters := file.Commands[2].Function.Parameters
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
		"vim9script\nclass Person\n  def new(thisArg, this.a.b)\n  enddef\nendclass\n",
		"vim9script\nclass Person\n  def new(this.1, this.a-b)\n  enddef\nendclass\n",
	} {
		file := Parse(source)
		if len(file.Commands) < 3 || file.Commands[2].Function == nil {
			t.Fatalf("source=%q file=%#v", source, file)
		}
		for _, parameter := range file.Commands[2].Function.Parameters {
			if parameter.Target != nil {
				t.Fatalf("source=%q unexpectedly recognized target=%#v", source, parameter.Target)
			}
		}
	}

	legacy := (LegacyParser{}).Parse("function s:new(this.name)\nendfunction\n")
	if len(legacy.Diagnostics) != 1 || legacy.Diagnostics[0].Code != "vim/E475" || legacy.Diagnostics[0].Message != "Invalid argument: this.name)" || legacy.Text(legacy.Diagnostics[0].Span) != "this.name" || len(legacy.Commands) == 0 || legacy.Commands[0].Function == nil || legacy.Commands[0].Function.Parameters[0].Target != nil {
		t.Fatalf("legacy constructor parameter = %#v", legacy)
	}
}

func TestVim9ObjectParameterRequiresNewMethod(t *testing.T) {
	for _, modifier := range []string{"", "static "} {
		source := "vim9script\nclass A\n  var val = 10\n  " + modifier + "def Foo(this.val: number)\n  enddef\nendclass\n"
		file := Parse(source)
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1390" || file.Diagnostics[0].Message != `Cannot use an object variable "this.val" except with the "new" method` || file.Text(file.Diagnostics[0].Span) != "this.val" {
			t.Fatalf("modifier=%q diagnostics=%#v", modifier, file.Diagnostics)
		}
		function := file.Commands[3].Function
		if function == nil || len(function.Parameters) != 1 || function.Parameters[0].Target == nil {
			t.Fatalf("modifier=%q function=%#v", modifier, function)
		}
		assertFileSpans(t, file)
	}
	for _, source := range []string{
		"vim9script\nclass A\n  var val = 10\n  def newVals(this.val)\n  enddef\nendclass\n",
		"vim9script\ndef Foo(this.val: number)\nenddef\n",
	} {
		file := Parse(source)
		for _, diagnostic := range file.Diagnostics {
			if diagnostic.Code == "vim/E1390" {
				t.Fatalf("source=%q unexpected E1390: %#v", source, diagnostic)
			}
		}
	}
}
