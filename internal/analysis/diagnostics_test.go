package analysis

import (
	"reflect"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestAnalyzeBuiltinArgumentTypeDiagnostics(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   int
	}{
		{"number", "vim9script\ndef F()\n  abs('x')\nenddef\n", 1},
		{"string", "vim9script\ndef F()\n  split(1)\nenddef\n", 1},
		{"buffer union mismatch", "vim9script\ndef F()\n  bufname({})\nenddef\n", 1},
		{"buffer union matches", "vim9script\ndef F()\n  bufname(1)\n  bufname('x')\nenddef\n", 0},
		{"len union", "vim9script\ndef F()\n  len(1)\n  len({})\n  len(1.0)\nenddef\n", 1},
		{"method argument", "vim9script\ndef F()\n  ['x']->map(3)\nenddef\n", 1},
		{"null argument", "vim9script\ndef F()\n  assert_match('a', 'b', null)\nenddef\n", 1},
		{"builtin function result", "vim9script\ndef F()\n  foldclosed(function('min'))\nenddef\n", 1},
		{"builtin channel result", "vim9script\ndef F()\n  map(test_null_channel(), '1')\nenddef\n", 1},
		{"builtin void result", "vim9script\ndef F()\n  test_feedinput(test_void())\nenddef\n", 1},
		{"filter callback parameter", "vim9script\ndef F()\n  var values = [1, 2]\n  filter(values, (i: string, v: number) => true)\nenddef\n", 1},
		{"map callback return", "vim9script\ndef F()\n  var values: list<number> = [1, 2]\n  map(values, (_, v) => [])\nenddef\n", 1},
		{"sort callback return", "vim9script\ndef F()\n  sort([1, 2], (a: number, b: number) => true)\nenddef\n", 1},
		{"callback void return", "vim9script\ndef F()\n  def TestIdx(k: number, v: dict<any>)\n  enddef\n  indexof([{color: 'red'}], TestIdx)\nenddef\n", 1},
		{"unknown argument", "vim9script\ndef F(value: any)\n  abs(value)\nenddef\n", 0},
		{"incomplete call", "vim9script\ndef F()\n  len(\nenddef\n", 0},
		{"legacy", "echo abs('x')\n", 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			var got int
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1013" {
					got++
					if diagnostic.Span.Start >= diagnostic.Span.End || diagnostic.Message == "" {
						t.Fatalf("invalid E1013 = %#v", diagnostic)
					}
				}
			}
			if got != test.want {
				t.Fatalf("E1013 diagnostics = %d, want %d; all diagnostics = %#v", got, test.want, result.Diagnostics)
			}
		})
	}
}

func TestAnalyzeE1031VoidValueDiagnostics(t *testing.T) {
	tests := []struct {
		name   string
		source string
		text   string
	}{
		{
			name:   "inferred initializer",
			source: "vim9script\ndef F()\n  var name = feedkeys(\"0\")\nenddef\n",
			text:   "feedkeys(\"0\")",
		},
		{
			name:   "destructuring assignment",
			source: "vim9script\ndef F()\n  var v1: number\n  var v2: number\n  [v1, v2] = popup_clear()\nenddef\n",
			text:   "popup_clear()",
		},
		{
			name:   "indexof callback",
			source: "vim9script\ndef TestIdx(k: number, v: dict<any>)\nenddef\nindexof([{color: \"red\"}], TestIdx)\n",
			text:   "TestIdx",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1031" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Cannot use void value" || file.Text(got[0].Span) != test.text {
				t.Fatalf("E1031 diagnostics = %#v, want one on %q; all diagnostics = %#v", got, test.text, result.Diagnostics)
			}
			if test.name == "indexof callback" {
				for _, diagnostic := range result.Diagnostics {
					if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.text {
						t.Fatalf("indexof void callback retained E1013: %#v", result.Diagnostics)
					}
				}
			}
		})
	}

	t.Run("guards", func(t *testing.T) {
		source := "vim9script\ndef F()\n  feedkeys(\"0\")\n  var n: number = feedkeys(\"0\")\nenddef\n"
		file := syntax.Parse(source)
		result := Analyze(file)
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1031" {
				t.Fatalf("effect-only or typed void use reported E1031: %#v", result.Diagnostics)
			}
		}
		var typed []syntax.Diagnostic
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1012" {
				typed = append(typed, diagnostic)
			}
		}
		if len(typed) != 1 || file.Text(typed[0].Span) != "feedkeys(\"0\")" {
			t.Fatalf("typed void initializer E1012 = %#v, want one on call; all diagnostics = %#v", typed, result.Diagnostics)
		}
	})
}

func TestAnalyzeArithmeticDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, code, message, span string
	}{
		{
			name: "remainder", source: "vim9script\ndef F()\n  var x = '1' % '2'\nenddef\n",
			code: "vim/E1035", message: "% requires number arguments", span: "%",
		},
		{
			name: "multiplication", source: "vim9script\ndef F()\n  var x = [1] * [2]\nenddef\n",
			code: "vim/E1036", message: "* requires number or float arguments", span: "*",
		},
		{
			name: "subtraction", source: "vim9script\ndef F()\n  echo {} - 22\nenddef\n",
			code: "vim/E1036", message: "- requires number or float arguments", span: "-",
		},
		{
			name: "addition", source: "vim9script\ndef F()\n  var x = 0z01 + 2\nenddef\n",
			code: "vim/E1051", message: "Wrong argument type for +", span: "+",
		},
		{
			name: "compound addition", source: "vim9script\ndef F()\n  v:errmsg += 'more'\nenddef\n",
			code: "vim/E1051", message: "Wrong argument type for +", span: "+=",
		},
		{
			name: "script lambda", source: "vim9script\necho filter([1, 2, 3], (_, v: string) => v + 1)\n",
			code: "vim/E1051", message: "Wrong argument type for +", span: "+",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == test.code {
					got = append(got, diagnostic)
				}
				if test.name == "script lambda" && diagnostic.Code == "vim/E1013" {
					t.Fatalf("invalid lambda retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one %s %q on %q; all diagnostics = %#v", got, test.code, test.message, test.span, result.Diagnostics)
			}
		})
	}
}

func TestAnalyzeArithmeticDiagnosticsStayConservative(t *testing.T) {
	source := "vim9script\nvar scriptOnly = '1' % '2'\ndef F(value: any)\n  var remainder = 5 % 2\n  var sum = 1.0 + 2\n  var unknown = value + 1\n  var lists = [1] + [2]\n  var tuples = (1, 'one') + (2, 'two')\n  var blobs = 0z01 + 0z02\nenddef\nlegacy let legacyValue = '1' % '2'\n"
	for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
		if diagnostic.Code == "vim/E1035" || diagnostic.Code == "vim/E1036" || diagnostic.Code == "vim/E1051" {
			t.Fatalf("valid, unknown, script, or legacy expression diagnostic = %#v", diagnostic)
		}
	}
}

func TestAnalyzeSpecialAsNumberDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, wantCode, wantMessage string
		wantSpans                           []string
	}{
		{
			name: "vim9 script", source: "vim9script\nvar noneValue = 1 + v:none\nvar nullValue = v:null + 1\n",
			wantCode: "vim/E611", wantMessage: "Using a Special as a Number", wantSpans: []string{"v:none", "v:null"},
		},
		{
			name: "legacy script", source: "let g:value = v:none + 1\n",
			wantCode: "vim/E611", wantMessage: "Using a Special as a Number", wantSpans: []string{"v:none"},
		},
		{
			name: "compiled def", source: "vim9script\ndef F()\n  var value = 1 + v:none\nenddef\n",
			wantCode: "vim/E1051", wantMessage: "Wrong argument type for +", wantSpans: []string{"+"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var spans []string
			for _, diagnostic := range result.Diagnostics {
				if test.name == "compiled def" && diagnostic.Code == "vim/E611" {
					t.Fatalf("compiled def retained E611: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.wantCode {
					if diagnostic.Message != test.wantMessage {
						t.Fatalf("diagnostic = %#v", diagnostic)
					}
					spans = append(spans, file.Text(diagnostic.Span))
				}
			}
			if !reflect.DeepEqual(spans, test.wantSpans) {
				t.Fatalf("spans = %q, want %q; diagnostics = %#v", spans, test.wantSpans, result.Diagnostics)
			}
		})
	}
}

func TestAnalyzeFuncrefAsNumberDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, span string
	}{
		{
			name: "vim9 script index",
			source: "vim9script\n" +
				"var value = [4, 6][() => 1]\n",
			span: "() => 1",
		},
		{
			name: "vim9 script arithmetic",
			source: "vim9script\n" +
				"var F = () => 1\n" +
				"var value = F + 1\n",
			span: "F",
		},
		{
			name: "legacy arithmetic",
			source: "let F = function('len')\n" +
				"let value = F + 1\n",
			span: "F",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E703" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Using a Funcref as a Number" || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one E703 on %q; all diagnostics = %#v", got, test.span, result.Diagnostics)
			}
		})
	}

	file := syntax.Parse("vim9script\ndef F()\n  var value = [4, 6][() => 1]\nenddef\n")
	result := Analyze(file)
	var mismatch []syntax.Diagnostic
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E703" {
			t.Fatalf("compiled def retained E703: %#v", result.Diagnostics)
		}
		if diagnostic.Code == "vim/E1012" {
			mismatch = append(mismatch, diagnostic)
		}
	}
	if len(mismatch) != 1 || file.Text(mismatch[0].Span) != "() => 1" {
		t.Fatalf("compiled def diagnostics = %#v, want one E1012 on lambda", result.Diagnostics)
	}
}

func TestAnalyzeDictionaryAsNumberDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, span string
	}{
		{
			name:   "vim9 script left operand",
			source: "vim9script\nvar value = {one: 1} * {two: 2}\n",
			span:   "{one: 1}",
		},
		{
			name:   "vim9 script right operand",
			source: "vim9script\nvar value = 22 % {two: 2}\n",
			span:   "{two: 2}",
		},
		{
			name:   "legacy operand",
			source: "let value = {} - 22\n",
			span:   "{}",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E728" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Using a Dictionary as a Number" || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one E728 on %q; all diagnostics = %#v", got, test.span, result.Diagnostics)
			}
		})
	}

	file := syntax.Parse("vim9script\ndef F()\n  var value = {} - 22\nenddef\n")
	result := Analyze(file)
	var mismatch []syntax.Diagnostic
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E728" {
			t.Fatalf("compiled def retained E728: %#v", result.Diagnostics)
		}
		if diagnostic.Code == "vim/E1036" {
			mismatch = append(mismatch, diagnostic)
		}
	}
	if len(mismatch) != 1 || file.Text(mismatch[0].Span) != "-" {
		t.Fatalf("compiled def diagnostics = %#v, want one E1036 on operator", result.Diagnostics)
	}
}

func TestAnalyzeFuncrefAsStringDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, span string
	}{
		{
			name:   "vim9 script Funcref",
			source: "vim9script\nvar value = 'a' .. function('len')\n",
			span:   "function('len')",
		},
		{
			name:   "vim9 script Partial",
			source: "vim9script\nvar value = 'a' .. function('len', ['a'])\n",
			span:   "function('len', ['a'])",
		},
		{
			name:   "legacy Funcref",
			source: "let value = 'a' . function('len')\n",
			span:   "function('len')",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E729" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Using a Funcref as a String" || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one E729 on %q; all diagnostics = %#v", got, test.span, result.Diagnostics)
			}
		})
	}

	file := syntax.Parse("vim9script\ndef F()\n  var value = 'a' .. function('len')\nenddef\n")
	result := Analyze(file)
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E729" {
			t.Fatalf("compiled def retained E729: %#v", result.Diagnostics)
		}
	}
}

func TestAnalyzeListAsStringDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, span string
	}{
		{
			name:   "vim9 script concatenation",
			source: "vim9script\nvar value = 'a' .. [1]\n",
			span:   "[1]",
		},
		{
			name:   "legacy concatenation",
			source: "let value = 'a' . [1]\n",
			span:   "[1]",
		},
		{
			name:   "builtin string or function argument",
			source: "vim9script\nsearch('a', '', 9, 0, [0])\n",
			span:   "[0]",
		},
		{
			name:   "computed Dictionary key",
			source: "vim9script\nvar value = {[[1, 2]]: 0}\n",
			span:   "[1, 2]",
		},
		{
			name:   "typed computed Dictionary key",
			source: "vim9script\nvar key: list<number> = [1]\nvar value = {[key]: 0}\n",
			span:   "key",
		},
		{
			name:   "string option assignment",
			source: "vim9script\n&grepprg = [343]\n",
			span:   "[343]",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E730" {
					got = append(got, diagnostic)
				}
				if (diagnostic.Code == "vim/E1012" || diagnostic.Code == "vim/E1013") && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("generic type mismatch retained for %q: %#v", test.span, result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "Using a List as a String" || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one E730 on %q; all diagnostics = %#v", got, test.span, result.Diagnostics)
			}
		})
	}

	file := syntax.Parse("vim9script\ndef F()\n  var value = 'a' .. [1]\nenddef\n")
	result := Analyze(file)
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E730" {
			t.Fatalf("compiled def retained E730: %#v", result.Diagnostics)
		}
	}

	file = syntax.Parse("vim9script\nvar key: list<number> = [1]\nvar value = {key: 0}\n")
	result = Analyze(file)
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E730" {
			t.Fatalf("Vim9 literal key used a same-named variable type: %#v", result.Diagnostics)
		}
	}
}

func TestAnalyzeDictionaryAsStringDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, span string
	}{
		{
			name:   "vim9 script right operand",
			source: "vim9script\nvar value = 'a' .. {a: 1}\n",
			span:   "{a: 1}",
		},
		{
			name:   "vim9 script left operand",
			source: "vim9script\nvar value = {a: 1} .. 'a'\n",
			span:   "{a: 1}",
		},
		{
			name:   "legacy operand",
			source: "let value = 'a' . {'a': 1}\n",
			span:   "{'a': 1}",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E731" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Using a Dictionary as a String" || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one E731 on %q; all diagnostics = %#v", got, test.span, result.Diagnostics)
			}
		})
	}

	file := syntax.Parse("vim9script\ndef F()\n  var value = 'a' .. {a: 1}\nenddef\n")
	result := Analyze(file)
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E731" {
			t.Fatalf("compiled def retained E731: %#v", result.Diagnostics)
		}
	}
}

func TestAnalyzeWrongVariableTypeDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, message, span string
	}{
		{
			name:    "vim9 script concatenates List",
			source:  "vim9script\nvar value = '-'\nvalue ..= [1, 2]\n",
			message: "Wrong variable type for .=",
			span:    "..=",
		},
		{
			name:    "vim9 script concatenates Dictionary",
			source:  "vim9script\nvar value = '-'\nvalue ..= {a: 2}\n",
			message: "Wrong variable type for .=",
			span:    "..=",
		},
		{
			name:    "legacy concatenates List",
			source:  "let value = '-'\nlet value .= [1, 2]\n",
			message: "Wrong variable type for .=",
			span:    ".=",
		},
		{
			name:    "vim9 script Dictionary target",
			source:  "vim9script\nvar value: dict<number> = {a: 1}\nvalue += 1\n",
			message: "Wrong variable type for +=",
			span:    "+=",
		},
		{
			name:    "compiled Dictionary target",
			source:  "vim9script\ndef F()\n  var value: dict<number> = {a: 1}\n  value *= 1\nenddef\n",
			message: "Wrong variable type for *=",
			span:    "*=",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E734" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one E734 %q on %q; all diagnostics = %#v", got, test.message, test.span, result.Diagnostics)
			}
		})
	}

	file := syntax.Parse("vim9script\ndef F()\n  var value = '-'\n  value ..= [1, 2]\nenddef\n")
	result := Analyze(file)
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E734" {
			t.Fatalf("compiled string concatenation retained E734: %#v", result.Diagnostics)
		}
	}
}

func TestAnalyzeListAsNumberDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, span string
	}{
		{
			name:   "vim9 script left arithmetic operand",
			source: "vim9script\nvar value = [] - 33\n",
			span:   "[]",
		},
		{
			name:   "vim9 script right arithmetic operand",
			source: "vim9script\nvar value = 33 * []\n",
			span:   "[]",
		},
		{
			name:   "legacy arithmetic operand",
			source: "let value = 33 % []\n",
			span:   "[]",
		},
		{
			name:   "logical left operand",
			source: "vim9script\nvar value = [] || false\n",
			span:   "[]",
		},
		{
			name:   "evaluated logical right operand",
			source: "vim9script\nvar value = false || []\n",
			span:   "[]",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E745" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Using a List as a Number" || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one E745 on %q; all diagnostics = %#v", got, test.span, result.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nvar value = [1] + [2]\n",
		"vim9script\nvar value = true || []\n",
		"vim9script\nvar value = false && []\n",
		"vim9script\nvar value = 0z01 + []\n",
		"vim9script\ndef F()\n  var value = [] - 33\nenddef\n",
	} {
		file := syntax.Parse(source)
		result := Analyze(file)
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E745" {
				t.Fatalf("source %q unexpectedly received E745: %#v", source, result.Diagnostics)
			}
		}
	}
}

func TestAnalyzeListLogicalDiagnosticsUseContextualVimCodes(t *testing.T) {
	for _, expression := range []string{
		"[] || false",
		"$'{false || []}'",
		"$'{true && []}'",
	} {
		for _, test := range []struct {
			name, source, want string
		}{
			{
				name:   "vim9 script",
				source: "vim9script\nvar value = " + expression + "\n",
				want:   "vim/E745",
			},
			{
				name:   "compiled def",
				source: "vim9script\ndef F()\n  var value = " + expression + "\nenddef\n",
				want:   "vim/E1012",
			},
		} {
			t.Run(test.name+" "+expression, func(t *testing.T) {
				result := Analyze(syntax.Parse(test.source))
				found := false
				wrongContext := "vim/E745"
				if test.want == "vim/E745" {
					wrongContext = "vim/E1012"
				}
				for _, diagnostic := range result.Diagnostics {
					if diagnostic.Code == test.want {
						found = true
					}
					if diagnostic.Code == "vim/E1013" || diagnostic.Code == wrongContext {
						t.Fatalf("source %q received wrong contextual diagnostic: %#v", test.source, result.Diagnostics)
					}
				}
				if !found {
					t.Fatalf("source %q diagnostics = %#v, want %s", test.source, result.Diagnostics, test.want)
				}
			})
		}
	}
}

func TestAnalyzeFloatModuloDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
	}{
		{
			name:   "vim9 script left Float",
			source: "vim9script\nvar value = 1.0 % 2\n",
		},
		{
			name:   "vim9 script right Float",
			source: "vim9script\nvar value = 2 % 1.0\n",
		},
		{
			name:   "legacy script",
			source: "let value = 1.0 % 2\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E804" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Cannot use '%' with Float" || file.Text(got[0].Span) != "%" {
				t.Fatalf("diagnostics = %#v, want one E804 on %% operator; all diagnostics = %#v", got, result.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nvar value = 1.0 * 2\n",
		"vim9script\nvar value = 1.0 / 2\n",
	} {
		result := Analyze(syntax.Parse(source))
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E804" {
				t.Fatalf("source %q unexpectedly received E804: %#v", source, result.Diagnostics)
			}
		}
	}

	compiled := Analyze(syntax.Parse("vim9script\ndef F()\n  var value = 1.0 % 2\nenddef\n"))
	foundE1035 := false
	for _, diagnostic := range compiled.Diagnostics {
		if diagnostic.Code == "vim/E1035" {
			foundE1035 = true
		}
		if diagnostic.Code == "vim/E804" {
			t.Fatalf("compiled def retained E804: %#v", compiled.Diagnostics)
		}
	}
	if !foundE1035 {
		t.Fatalf("compiled def diagnostics = %#v, want E1035", compiled.Diagnostics)
	}

	for _, source := range []string{
		"vim9script\nvar value = [] % 1.0\n",
		"vim9script\nvar value = 1.0 % []\n",
	} {
		result := Analyze(syntax.Parse(source))
		foundE745 := false
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E745" {
				foundE745 = true
			}
			if diagnostic.Code == "vim/E804" {
				t.Fatalf("source %q received E804 before E745: %#v", source, result.Diagnostics)
			}
		}
		if !foundE745 {
			t.Fatalf("source %q diagnostics = %#v, want E745 precedence", source, result.Diagnostics)
		}
	}
}

func TestAnalyzeFloatAsNumberDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, span string
	}{
		{
			name:   "vim9 script ternary condition",
			source: "vim9script\nvar value = 0.1 ? 'one' : 'two'\n",
			span:   "0.1",
		},
		{
			name:   "compiled def ternary condition",
			source: "vim9script\ndef F()\n  var value = 0.1 ? 'one' : 'two'\nenddef\n",
			span:   "0.1",
		},
		{
			name:   "legacy ternary condition",
			source: "let value = 0.1 ? 'one' : 'two'\n",
			span:   "0.1",
		},
		{
			name:   "vim9 script builtin Number argument",
			source: "vim9script\nextendnew(0z0102, 0z03, 1.1)\n",
			span:   "1.1",
		},
		{
			name:   "vim9 script List index",
			source: "vim9script\nvar values = [1, 2, 3]\nvalues[1.1] = 4\n",
			span:   "1.1",
		},
		{
			name:   "legacy List index",
			source: "let values = [1, 2, 3]\nlet item = values[1.1]\n",
			span:   "1.1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E805" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("script-level Float retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "Using a Float as a Number" || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one E805 on %q; all diagnostics = %#v", got, test.span, result.Diagnostics)
			}
		})
	}

	for _, test := range []struct {
		source, want string
	}{
		{
			source: "vim9script\ndef F()\n  extendnew(0z0102, 0z03, 1.1)\nenddef\n",
			want:   "vim/E1013",
		},
		{
			source: "vim9script\ndef F()\n  var values = [1, 2, 3]\n  values[1.1] = 4\nenddef\n",
			want:   "vim/E1012",
		},
	} {
		file := syntax.Parse(test.source)
		result := Analyze(file)
		found := false
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == test.want {
				found = true
			}
			if diagnostic.Code == "vim/E805" {
				t.Fatalf("compiled def retained E805: %#v", result.Diagnostics)
			}
		}
		if !found {
			t.Fatalf("compiled def diagnostics = %#v, want %s", result.Diagnostics, test.want)
		}
	}

	for _, source := range []string{
		"vim9script\nvar value = 1.0 * 2\n",
		"vim9script\nvar value = !!0.1 ? 'one' : 'two'\n",
		"vim9script\nvar value = 1.0 % 2\n",
	} {
		result := Analyze(syntax.Parse(source))
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E805" {
				t.Fatalf("source %q unexpectedly received E805: %#v", source, result.Diagnostics)
			}
		}
	}
}

func TestAnalyzeFuncrefVariableNameDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, wantName string
	}{
		{
			name:     "vim9 script inferred type",
			source:   "vim9script\nvar lambda = () => 1\n",
			wantName: "lambda",
		},
		{
			name:     "compiled def inferred type",
			source:   "vim9script\ndef F()\n  var lambda = () => 1\nenddef\n",
			wantName: "lambda",
		},
		{
			name:     "compiled def explicit type",
			source:   "vim9script\ndef F()\n  var ref1: func()\nenddef\n",
			wantName: "ref1",
		},
		{
			name:     "compiled def parameter",
			source:   "vim9script\ndef F(_Fn: func)\nenddef\n",
			wantName: "_Fn",
		},
		{
			name:     "lambda parameter",
			source:   "vim9script\nvar Fn = (_Farg: func) => 1\n",
			wantName: "_Farg",
		},
		{
			name:     "legacy inferred type",
			source:   "let lower = function('len')\n",
			wantName: "lower",
		},
		{
			name:     "legacy global",
			source:   "let g:lower = function('len')\n",
			wantName: "g:lower",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E704" {
					got = append(got, diagnostic)
				}
			}
			message := "Funcref variable name must start with a capital: " + test.wantName
			if len(got) != 1 || got[0].Message != message || file.Text(got[0].Span) != test.wantName {
				t.Fatalf("diagnostics = %#v, want one E704 %q on %q; all diagnostics = %#v", got, message, test.wantName, result.Diagnostics)
			}
		})
	}

	valid := []struct {
		name, source string
	}{
		{name: "capital", source: "vim9script\nvar Fn = () => 1\n"},
		{name: "global capital", source: "let g:Fn = function('len')\n"},
		{name: "legacy script local", source: "let s:lower = function('len')\n"},
		{name: "window local", source: "let w:lower = function('len')\n"},
		{name: "autoload", source: "let package#lower = function('len')\n"},
		{name: "class member", source: "vim9script\nclass C\n  static var handler: func\nendclass\n"},
	}
	for _, test := range valid {
		t.Run("valid "+test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E704" {
					t.Fatalf("valid name diagnostic = %#v; all diagnostics = %#v", diagnostic, result.Diagnostics)
				}
			}
		})
	}
}

func TestAnalyzeMissingDictionaryKeyDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, key, span string
	}{
		{
			name:   "vim9 invalid hash member tail",
			source: "vim9script\nvar x = { \"a#b\": 1 }\nx.a#b\n",
			key:    "a",
			span:   "x.a#b",
		},
		{
			name:   "vim9 invalid colon member tail",
			source: "vim9script\nvar x = { \"a:b\": 1 }\nx.a:b\n",
			key:    "a",
			span:   "x.a:b",
		},
		{
			name:   "vim9 missing member",
			source: "vim9script\nvar x = {present: 1}\necho x.missing\n",
			key:    "missing",
			span:   "x.missing",
		},
		{
			name:   "vim9 missing index",
			source: "vim9script\nvar x = {present: 1}\necho x['missing']\n",
			key:    "missing",
			span:   "x['missing']",
		},
		{
			name:   "vim9 default empty dictionary",
			source: "vim9script\nvar x: dict<number>\necho x.missing\n",
			key:    "missing",
			span:   "x.missing",
		},
		{
			name:   "legacy missing member",
			source: "let x = {'present': 1}\necho x.missing\n",
			key:    "missing",
			span:   "x.missing",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E716" {
					got = append(got, diagnostic)
				}
			}
			message := "Key not present in Dictionary: \"" + test.key + "\""
			if len(got) != 1 || got[0].Message != message || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one E716 %q on %q; all diagnostics = %#v; commands = %#v", got, message, test.span, result.Diagnostics, file.Commands)
			}
		})
	}
}

func TestAnalyzeMissingDictionaryKeyStaysConservative(t *testing.T) {
	tests := []struct {
		name, source string
	}{
		{
			name:   "present key",
			source: "vim9script\nvar x = {present: 1}\necho x.present\n",
		},
		{
			name:   "present index",
			source: "vim9script\nvar x = {present: 1}\necho x['present']\n",
		},
		{
			name:   "dynamic index",
			source: "vim9script\nvar key = 'missing'\nvar x = {present: 1}\necho x[key]\n",
		},
		{
			name:   "dynamic dictionary parameter",
			source: "vim9script\ndef F(x: dict<number>)\n  echo x.missing\nenddef\n",
		},
		{
			name:   "dictionary passed before access",
			source: "vim9script\nvar x = {present: 1}\nMutate(x)\necho x.missing\n",
		},
		{
			name:   "compiled invalid member tail",
			source: "vim9script\ndef F()\n  var x = { \"a#b\": 1 }\n  x.a#b\nenddef\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E716" {
					t.Fatalf("conservative case diagnostic = %#v; all diagnostics = %#v", diagnostic, result.Diagnostics)
				}
			}
		})
	}
}

func TestAnalyzeBuiltinNativeArgumentDiagnostics(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		code     string
		message  string
		argument string
	}{
		{
			name:     "string argument",
			source:   "vim9script\nsubstitute('Hallo', 'a', 'e', true)\n",
			code:     "vim/E1174",
			message:  "String required for argument 4",
			argument: "true",
		},
		{
			name:     "dictionary method receiver",
			source:   "vim9script\n[8, 9]->keys()\n",
			code:     "vim/E1206",
			message:  "Dictionary required for argument 1",
			argument: "[8, 9]",
		},
		{
			name:     "script len argument",
			source:   "vim9script\nlen(true)\n",
			code:     "vim/E701",
			message:  "Invalid type for len()",
			argument: "true",
		},
		{
			name:     "legacy len argument",
			source:   "echo len(v:none)\n",
			code:     "vim/E701",
			message:  "Invalid type for len()",
			argument: "v:none",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var native []syntax.Diagnostic
			var e1013 int
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == test.code {
					native = append(native, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.argument {
					e1013++
				}
			}
			if len(native) != 1 {
				t.Fatalf("native diagnostics = %#v, want one %s; all diagnostics = %#v", native, test.code, result.Diagnostics)
			}
			diagnostic := native[0]
			if diagnostic.Message != test.message || file.Text(diagnostic.Span) != test.argument {
				t.Fatalf("diagnostic = %#v (span text %q), want %s %q on %q", diagnostic, file.Text(diagnostic.Span), test.code, test.message, test.argument)
			}
			if e1013 != 0 {
				t.Fatalf("E1013 diagnostics for remapped argument = %d, want none; all diagnostics = %#v", e1013, result.Diagnostics)
			}
		})
	}

	positive := syntax.Parse("vim9script\nsubstitute('Hallo', 'a', 'e', '')\n{'a': 1}->keys()\nlen(123)\n")
	for _, diagnostic := range Analyze(positive).Diagnostics {
		if diagnostic.Code == "vim/E701" || diagnostic.Code == "vim/E1174" || diagnostic.Code == "vim/E1206" {
			t.Fatalf("valid builtin argument diagnostic = %#v", diagnostic)
		}
	}
}

func TestAnalyzeUnknownSignDependsOnRuntimeRegistry(t *testing.T) {
	script := syntax.Parse("vim9script\nsign_undefine([1])\n")
	for _, diagnostic := range Analyze(script).Diagnostics {
		if diagnostic.Code == "vim/E155" || diagnostic.Code == "vim/E1013" {
			t.Fatalf("runtime-dependent script sign diagnostic = %#v", diagnostic)
		}
	}

	def := syntax.Parse("vim9script\ndef Check()\n  sign_undefine([1])\nenddef\n")
	var got []syntax.Diagnostic
	for _, diagnostic := range Analyze(def).Diagnostics {
		if diagnostic.Code == "vim/E1013" {
			got = append(got, diagnostic)
		}
		if diagnostic.Code == "vim/E155" {
			t.Fatalf("def type mismatch reported runtime E155: %#v", diagnostic)
		}
	}
	if len(got) != 1 || got[0].Message != "Argument 1: type mismatch, expected string or list<string> but got list<number>" || def.Text(got[0].Span) != "[1]" {
		t.Fatalf("def sign_undefine diagnostics = %#v", got)
	}
}

func TestAnalyzeFunctionArgumentTypeDiagnostics(t *testing.T) {
	source := "vim9script\ndef Filter(x: string, Cond: func(string): bool): bool\n  return Cond(x)\nenddef\ndef Defaults(one: string, two = 'foo', ...rest: list<string>)\nenddef\ndef Varargs(...args: list<string>)\nenddef\ndef F()\n  var Ref = (x: number, y: number) => x + y\n  Ref(1, 'lambda')\n  Ref(1, 2)\n  Filter('foo', (v) => v .. '^b')\n  Defaults('one', 22)\n  Defaults('one', 'two', 123)\n  Varargs(1)\n  Varargs('one', 2)\nenddef\n"
	var diagnostics []syntax.Diagnostic
	for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
		if diagnostic.Code == "vim/E1013" {
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	want := []string{"'lambda'", "(v) => v .. '^b'", "22", "123", "1", "2"}
	if len(diagnostics) != len(want) {
		t.Fatalf("E1013 diagnostics = %#v", diagnostics)
	}
	for index, diagnostic := range diagnostics {
		if got := source[diagnostic.Span.Start:diagnostic.Span.End]; got != want[index] {
			t.Fatalf("E1013[%d] span = %q, want %q; diagnostics = %#v", index, got, want[index], diagnostics)
		}
	}
}

func TestAnalyzeTypeMismatchDiagnostics(t *testing.T) {
	source := "vim9script\ndef F()\n  var numberValue: number = 'bad'\n  var numbers: list<number> = [1, 'bad']\n  var dictionary: dict<number> = {ok: 1, bad: 'bad'}\n  var tupleValue = ('x', 'y')\n  tupleValue = (1, 2)\n  numbers[0] = 'bad'\n  numbers[:] = ['bad']\n  var badIndex = numbers['bad']\n  var badCast = <number>string(1)\n  var BadLambda = (value: number): string => 99\n  var VoidCallback: func(number)\n  VoidCallback = (value) => !value\n  if ['condition']\n  endif\n  var badLogical = true && 'operand'\n  &t_TI = 123\n  for item: number in ['bad']\n  endfor\nenddef\n"
	var got []string
	for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
		if diagnostic.Code == "vim/E1012" {
			got = append(got, source[diagnostic.Span.Start:diagnostic.Span.End])
		}
	}
	want := []string{"'bad'", "'bad'", "'bad'", "1", "'bad'", "'bad'", "'bad'", "string(1)", "99", "(value) => !value", "['condition']", "'operand'", "123", "['bad']"}
	if len(got) != len(want) {
		t.Fatalf("E1012 spans = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("E1012 span[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestAnalyzeConcatenatingAssignmentRequiresStringTarget(t *testing.T) {
	source := `vim9script
def F()
  var anr = 4
  anr ..= "text"
  &ts ..= "xxx"
  var text = "ok"
  text ..= "text"
  var values = [1]
  values[0] ..= "text"
  var dictionary = {key: 1}
  dictionary.key ..= "text"
  s:dynamic ..= "text"
enddef
`
	file := syntax.Parse(source)
	var got []syntax.Diagnostic
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vim/E1019" {
			got = append(got, diagnostic)
		}
	}
	want := []string{"anr", "&ts"}
	if len(got) != len(want) {
		t.Fatalf("E1019 diagnostics = %#v, want targets %v", got, want)
	}
	for index, target := range want {
		if got[index].Message != "Can only concatenate to string" || file.Text(got[index].Span) != target {
			t.Fatalf("E1019 diagnostic[%d] = %#v (%q), want target %q", index, got[index], file.Text(got[index].Span), target)
		}
	}
}

func TestAnalyzeOptionTypesAndUnknownWarnings(t *testing.T) {
	source := "vim9script\n" +
		"set ts=4 tabstop=4 tabs=4 nofutureoption t_ZZ=terminal\n" +
		"var shortName = &ts\n" +
		"var longName = &l:tabstop\n" +
		"var boolName = &nu\n" +
		"var stringName = &encoding\n" +
		"var future = &futureoption\n" +
		"var terminal = &t_ZZ\n" +
		"&ts = [7]\n" +
		"&futureoption = [7]\n" +
		"&t_TI = 123\n"
	file := syntax.Parse(source)
	result := Analyze(file)

	declarations := make(map[string]*Declaration)
	for _, declaration := range result.Root.Declarations {
		declarations[declaration.Name] = declaration
	}
	for name, want := range map[string]string{
		"shortName": "number", "longName": "number", "boolName": "bool", "stringName": "string", "future": "any", "terminal": "string",
	} {
		if declaration := declarations[name]; declaration == nil || declaration.Type.Name != want {
			t.Errorf("%s type = %#v, want %s", name, declaration, want)
		}
	}

	var unknownSpans []string
	var unknownCodes []string
	var unknownMessages []string
	var mismatchSpans []string
	for _, diagnostic := range result.Diagnostics {
		switch diagnostic.Code {
		case "vim/E113", "vim/E518":
			unknownSpans = append(unknownSpans, file.Text(diagnostic.Span))
			unknownCodes = append(unknownCodes, diagnostic.Code)
			unknownMessages = append(unknownMessages, diagnostic.Message)
		case "vim/E1012":
			mismatchSpans = append(mismatchSpans, file.Text(diagnostic.Span))
		}
	}
	wantUnknown := []string{"tabs", "futureoption", "&futureoption", "&futureoption"}
	if !reflect.DeepEqual(unknownSpans, wantUnknown) {
		t.Fatalf("unknown option spans = %#v, want %#v; diagnostics = %#v", unknownSpans, wantUnknown, result.Diagnostics)
	}
	wantCodes := []string{"vim/E518", "vim/E518", "vim/E113", "vim/E113"}
	if !reflect.DeepEqual(unknownCodes, wantCodes) {
		t.Fatalf("unknown option codes = %#v, want %#v", unknownCodes, wantCodes)
	}
	wantMessages := []string{"Unknown option: tabs", "Unknown option: futureoption", "Unknown option: futureoption", "Unknown option: futureoption"}
	if !reflect.DeepEqual(unknownMessages, wantMessages) {
		t.Fatalf("unknown option messages = %#v, want %#v", unknownMessages, wantMessages)
	}
	wantMismatch := []string{"[7]", "123"}
	if !reflect.DeepEqual(mismatchSpans, wantMismatch) {
		t.Fatalf("E1012 spans = %#v, want %#v; diagnostics = %#v", mismatchSpans, wantMismatch, result.Diagnostics)
	}
}

func TestAnalyzeLegacyUnknownOptionDiagnostics(t *testing.T) {
	source := "let value = &missingopt\nset missingopt\nsetlocal anothermissing\nlet &missingopt = 1\nlet terminal = &t_runtime\n"
	file := syntax.Parse(source)
	result := Analyze(file)
	want := []syntax.Diagnostic{
		{Code: "vim/E113", Message: "Unknown option: missingopt"},
		{Code: "vim/E518", Message: "Unknown option: missingopt"},
		{Code: "vim/E518", Message: "Unknown option: anothermissing"},
		{Code: "vim/E113", Message: "Unknown option: missingopt"},
	}
	var got []syntax.Diagnostic
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E113" || diagnostic.Code == "vim/E518" {
			got = append(got, diagnostic)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("unknown option diagnostics = %#v, want %#v; all diagnostics = %#v", got, want, result.Diagnostics)
	}
	for index, diagnostic := range got {
		if diagnostic.Code != want[index].Code || diagnostic.Message != want[index].Message {
			t.Fatalf("diagnostic[%d] = %#v, want %#v", index, diagnostic, want[index])
		}
	}
}

func TestAnalyzeRejectsRedeclaringDefArgument(t *testing.T) {
	source := "vim9script\ndef F(value: number)\n  var value = 1\nenddef\n"
	file := syntax.Parse(source)
	result := Analyze(file)
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "vim/E1006" || result.Diagnostics[0].Message != "value is used as an argument" || file.Text(result.Diagnostics[0].Span) != "value" {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
}

func TestAnalyzeMapCallbackReturnTypeDiagnostics(t *testing.T) {
	source := `vim9script
def StringMap(i: number, value: number): string
  return 'bad'
enddef
var numbers: list<number> = [1, 2]
map(numbers, StringMap)
var dictionary: dict<number> = {key: 1}
map(dictionary, (_, value) => [])
`
	file := syntax.Parse(source)
	var got []string
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vim/E1013" {
			t.Fatalf("map callback return was reported as E1013: %#v", diagnostic)
		}
		if diagnostic.Code == "vim/E1012" {
			got = append(got, file.Text(diagnostic.Span))
		}
	}
	want := []string{"StringMap", "(_, value) => []"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("E1012 spans = %#v, want %#v", got, want)
	}
}

func TestAnalyzeMapCallbackReturnTypeInDefIsArgumentMismatch(t *testing.T) {
	source := `vim9script
def StringMap(i: number, value: number): string
  return 'bad'
enddef
def Check()
  var numbers: list<number> = [1, 2]
  map(numbers, StringMap)
enddef
`
	file := syntax.Parse(source)
	for _, diagnostic := range Analyze(file).Diagnostics {
		if file.Text(diagnostic.Span) == "StringMap" {
			if diagnostic.Code != "vim/E1013" {
				t.Fatalf("map callback diagnostic = %#v, want E1013", diagnostic)
			}
			return
		}
	}
	t.Fatal("missing map callback diagnostic")
}

func TestAnalyzeMalformedVariadicTupleDoesNotPanic(t *testing.T) {
	source := "vim9script\nvar t: tuple<number, ...number> = (1, 2, 3)\n"
	file := syntax.Parse(source)
	result := Analyze(file)
	for _, diagnostic := range append(file.Diagnostics, result.Diagnostics...) {
		if diagnostic.Code == "vim/E1539" {
			return
		}
	}
	t.Fatalf("diagnostics = %#v, want E1539", append(file.Diagnostics, result.Diagnostics...))
}

func TestAnalyzeBuiltinArityDiagnostics(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []syntax.Diagnostic
	}{
		{
			name:   "legacy direct builtins",
			source: "echo len()\necho abs(1, 2)\necho range(1, 2, 3, 4)\n",
			want: []syntax.Diagnostic{
				{Code: "vim/E119", Message: "Not enough arguments for function: len"},
				{Code: "vim/E118", Message: "Too many arguments for function: abs"},
				{Code: "vim/E118", Message: "Too many arguments for function: range"},
			},
		},
		{
			name:   "vim9 direct builtins",
			source: "vim9script\nvar first = len()\nvar second = abs(1, 2)\n",
			want: []syntax.Diagnostic{
				{Code: "vim/E119", Message: "Not enough arguments for function: len"},
				{Code: "vim/E118", Message: "Too many arguments for function: abs"},
			},
		},
		{
			name:   "legacy builtin methods",
			source: "echo 'x'->len(2)\necho ''->printf()\necho 10->setwinvar()\n",
			want: []syntax.Diagnostic{
				{Code: "vim/E118", Message: "Too many arguments for function: len"},
				{Code: "vim/E119", Message: "Not enough arguments for function: printf"},
				{Code: "vim/E119", Message: "Not enough arguments for function: setwinvar"},
			},
		},
		{
			name:   "optional variadic and conservative targets",
			source: "vim9script\nvar optional = range(1)\nvar variadic = instanceof(null_object, 2, 3, 4)\nvar dynamic = call('len', [])\ng:MyFunction()\ns:len()\ng:items->len()\n",
		},
		{
			name:   "incomplete calls do not cascade",
			source: "vim9script\nvar missing = len(\nvar missingComma = len(1 2)\nvar missingArgument = abs(1,)\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			got := Analyze(file).Diagnostics
			if len(got) != len(test.want) {
				t.Fatalf("diagnostics = %#v, want %#v (syntax diagnostics = %#v)", got, test.want, file.Diagnostics)
			}
			last := -1
			for index, diagnostic := range got {
				want := test.want[index]
				if diagnostic.Code != want.Code || diagnostic.Message != want.Message {
					t.Fatalf("diagnostic[%d] = %#v, want %#v", index, diagnostic, want)
				}
				name := diagnostic.Message[strings.LastIndex(diagnostic.Message, ": ")+2:]
				start := strings.Index(test.source[last+1:], name) + last + 1
				wantSpan := syntax.Span{Start: start, End: start + len(name)}
				if start <= last || diagnostic.Span != wantSpan || file.Text(diagnostic.Span) != name {
					t.Fatalf("diagnostic[%d] span = %#v (%q), want %#v (%q)", index, diagnostic.Span, file.Text(diagnostic.Span), wantSpan, name)
				}
				last = start
			}
		})
	}
}

func TestAnalyzeUnknownFunctionDiagnostics(t *testing.T) {
	longName := "Func" + strings.Repeat("x", 196)
	tests := []struct {
		name, source, code, message, text string
	}{
		{
			name:   "Vim9 script",
			source: "vim9script\ndoesnotexist()\n",
			code:   "vim/E117", message: "Unknown function: doesnotexist", text: "doesnotexist",
		},
		{
			name:   "Vim9 def",
			source: "vim9script\ndef Test()\n  Missing()\nenddef\n",
			code:   "vim/E117", message: "Unknown function: Missing", text: "Missing",
		},
		{
			name:   "unscoped global function",
			source: "vim9script\ndef g:ExistingGlobal()\nenddef\nExistingGlobal()\n",
			code:   "vim/E117", message: "Unknown function: ExistingGlobal", text: "ExistingGlobal",
		},
		{
			name:   "long Vim9 script call",
			source: "vim9script\necho " + longName + "()\n",
			code:   "vim/E117", message: "Unknown function: " + longName, text: longName,
		},
		{
			name:   "long Vim9 def call",
			source: "vim9script\ndef Test()\n  echo " + longName + "()\nenddef\n",
			code:   "vim/E1011", message: "Name too long: " + longName, text: longName,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E117" || diagnostic.Code == "vim/E1011" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Code != test.code || got[0].Message != test.message || file.Text(got[0].Span) != test.text {
				t.Fatalf("diagnostics = %#v, want %s %q on %q; syntax diagnostics = %#v; all diagnostics = %#v", got, test.code, test.message, test.text, file.Diagnostics, result.Diagnostics)
			}
		})
	}

	conservative := "vim9script\n" +
		"len([])\n" +
		"Known()\n" +
		"def Known()\nenddef\n" +
		"var Dynamic: func\nDynamic()\n" +
		"g:Dynamic()\n" +
		"plugin#Dynamic()\n" +
		"var object: any\nobject.Dynamic()\n" +
		"[]->Dynamic()\n" +
		"legacy call Missing()\n"
	file := syntax.Parse(conservative)
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vim/E117" {
			t.Fatalf("dynamic, scoped, known, member, or legacy call reported E117: %#v; syntax diagnostics = %#v", diagnostic, file.Diagnostics)
		}
	}
}

func TestAnalyzeUserFunctionArityDiagnostics(t *testing.T) {
	type wantDiagnostic struct {
		code, message, text string
	}
	tests := []struct {
		name   string
		source string
		want   []wantDiagnostic
	}{
		{
			name: "legacy fixed default and variadic",
			source: "function! Fixed(value)\nendfunction\n" +
				"call Fixed()\ncall Fixed(1, 2)\n" +
				"function! Flexible(required, optional = 1, ...)\nendfunction\n" +
				"call Flexible()\ncall Flexible(1)\ncall Flexible(1, v:null, 3, 4)\n",
			want: []wantDiagnostic{
				{code: "vim/E119", message: "Not enough arguments for function: Fixed", text: "Fixed"},
				{code: "vim/E118", message: "Too many arguments for function: Fixed", text: "Fixed"},
				{code: "vim/E119", message: "Not enough arguments for function: Flexible", text: "Flexible"},
			},
		},
		{
			name: "legacy method receiver counts as an argument",
			source: "function! Pair(first, second)\nendfunction\n" +
				"echo 'one'->Pair()\necho 'one'->Pair('two', 'three')\n",
			want: []wantDiagnostic{
				{code: "vim/E119", message: "Not enough arguments for function: Pair", text: "Pair"},
				{code: "vim/E118", message: "Too many arguments for function: Pair", text: "Pair"},
			},
		},
		{
			name:   "legacy forward function stays conservative",
			source: "call Later()\nfunction! Later(value)\nendfunction\n",
		},
		{
			name: "Vim9 nested def",
			source: "vim9script\ndef Outer()\n" +
				"  def Empty()\n  enddef\n  Empty(1)\n" +
				"  def One(value: string)\n  enddef\n  One()\nenddef\n",
			want: []wantDiagnostic{
				{code: "vim/E118", message: "Too many arguments for function: Empty", text: "Empty"},
				{code: "vim/E119", message: "Not enough arguments for function: One", text: "One"},
			},
		},
		{
			name: "Vim9 default and variadic",
			source: "vim9script\n" +
				"def Defaulted(required: number, optional: number = 1)\nenddef\n" +
				"Defaulted()\nDefaulted(1)\nDefaulted(1, v:none)\nDefaulted(1, 2, 3)\n" +
				"def Flexible(required: number, ...rest: list<any>)\nenddef\n" +
				"Flexible()\nFlexible(1)\nFlexible(1, 2, 3)\n",
			want: []wantDiagnostic{
				{code: "vim/E119", message: "Not enough arguments for function: Defaulted", text: "Defaulted"},
				{code: "vim/E118", message: "Too many arguments for function: Defaulted", text: "Defaulted"},
				{code: "vim/E119", message: "Not enough arguments for function: Flexible", text: "Flexible"},
			},
		},
		{
			name: "Vim9 lambda and method receiver",
			source: "vim9script\n" +
				"var Ref = (value) => value\nRef()\nRef(1, 2)\n" +
				"echo ((value) => value)()\n" +
				"echo ((value) => value)(1, 2)\n" +
				"echo 'one'->((value) => value)('two')\n",
			want: []wantDiagnostic{
				{code: "vim/E119", message: "Not enough arguments for function: Ref", text: "Ref"},
				{code: "vim/E118", message: "Too many arguments for function: Ref", text: "Ref"},
				{code: "vim/E119", message: "Not enough arguments for function: <lambda>", text: "((value) => value)"},
				{code: "vim/E118", message: "Too many arguments for function: <lambda>", text: "((value) => value)"},
				{code: "vim/E118", message: "Too many arguments for function: <lambda>", text: "((value) => value)"},
			},
		},
		{
			name:   "unknown and incomplete calls stay conservative",
			source: "vim9script\nvar Unknown: func\nUnknown(1, 2)\ng:Dynamic()\nvar value: any\nvalue()\nvar missing = ((x) => x)(\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E118" || diagnostic.Code == "vim/E119" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != len(test.want) {
				t.Fatalf("arity diagnostics = %#v, want %#v; syntax diagnostics = %#v; all diagnostics = %#v", got, test.want, file.Diagnostics, result.Diagnostics)
			}
			for index, diagnostic := range got {
				want := test.want[index]
				if diagnostic.Code != want.code || diagnostic.Message != want.message || file.Text(diagnostic.Span) != want.text {
					t.Fatalf("arity diagnostic[%d] = %#v on %q, want %s %q on %q", index, diagnostic, file.Text(diagnostic.Span), want.code, want.message, want.text)
				}
			}
		})
	}
}

func TestAnalyzeIndexofCallbackArityDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		wantE118     bool
	}{
		{
			name: "Vim9 script uses E118",
			source: "vim9script\ndef TestIdx(value: dict<any>): bool\n  return true\nenddef\n" +
				"indexof([{color: 'red'}], TestIdx)\n",
			wantE118: true,
		},
		{
			name: "def keeps its distinct callback diagnostic",
			source: "vim9script\ndef Outer()\n  def TestIdx(value: dict<any>): bool\n    return true\n  enddef\n" +
				"  indexof([{color: 'red'}], TestIdx)\nenddef\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E118" {
					got = append(got, diagnostic)
				}
			}
			if test.wantE118 {
				if len(got) != 1 || got[0].Message != "Too many arguments for function: TestIdx" || file.Text(got[0].Span) != "TestIdx" {
					t.Fatalf("E118 diagnostics = %#v, want one on TestIdx; all diagnostics = %#v", got, result.Diagnostics)
				}
			} else if len(got) != 0 {
				t.Fatalf("def callback was incorrectly mapped to E118: %#v", result.Diagnostics)
			}
		})
	}
}

func TestAnalyzeBuiltinCallbackArityDiagnostics(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name: "def rejects incompatible callback counts",
			source: "vim9script\ndef Test()\n" +
				"  echo [0, 1, 2]->map(() => 123)\n" +
				"  echo [0, 1, 2]->map((_) => 123)\n" +
				"  echo [0, 1, 2]->map((a, b, c) => a + b + c)\n" +
				"  echo [0, 1, 2]->map((a, b, c, d) => a + b + c + d)\n" +
				"  echo [0, 1, 2]->map((index, value, ...rest) => value)\n" +
				"  def One(value: number): bool\n    return true\n  enddef\n" +
				"  indexof([1, 2], One)\nenddef\n",
			want: []string{
				"() => 123",
				"(_) => 123",
				"(a, b, c) => a + b + c",
				"(a, b, c, d) => a + b + c + d",
				"(index, value, ...rest) => value",
				"One",
			},
		},
		{
			name: "accepted callback shapes",
			source: "vim9script\ndef Test()\n" +
				"  echo [0, 1, 2]->map((index, value) => value)\n" +
				"  echo [0, 1, 2]->map((...args) => args[1])\n" +
				"  echo [0, 1, 2]->map((index, ...rest) => rest[0])\nenddef\n",
		},
		{
			name:   "script uses its context-specific codes",
			source: "vim9script\necho [0, 1, 2]->map((a, b, c) => a + b + c)\n",
		},
		{
			name:   "sort callback is not E176",
			source: "vim9script\ndef Test()\n  sort([1, 2], (a, b, c) => 0)\nenddef\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E176" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != len(test.want) {
				t.Fatalf("E176 diagnostics = %#v, want spans %#v; syntax diagnostics = %#v; all diagnostics = %#v", got, test.want, file.Diagnostics, result.Diagnostics)
			}
			for index, diagnostic := range got {
				if diagnostic.Message != "Invalid number of arguments" || file.Text(diagnostic.Span) != test.want[index] {
					t.Fatalf("E176 diagnostic[%d] = %#v on %q, want %q", index, diagnostic, file.Text(diagnostic.Span), test.want[index])
				}
			}
		})
	}
}

func TestAnalyzeImmutableAssignmentDiagnostics(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []syntax.Diagnostic
	}{
		{
			name:   "script const and final",
			source: "vim9script\nconst first = 1\nfinal second = 2\nfirst = 3\nsecond = 4\n",
			want: []syntax.Diagnostic{
				{Code: "vim/E46", Message: `Cannot change read-only variable "first"`},
				{Code: "vim/E46", Message: `Cannot change read-only variable "second"`},
			},
		},
		{
			name:   "def const and final",
			source: "vim9script\ndef Test()\n  const first = 1\n  final second = 2\n  first = 3\n  second = 4\nenddef\n",
			want: []syntax.Diagnostic{
				{Code: "vim/E1018", Message: "Cannot assign to a constant: first"},
				{Code: "vim/E1018", Message: "Cannot assign to a constant: second"},
			},
		},
		{
			name:   "script read-only Vim variables",
			source: "vim9script\nv:true = false\nv:false = true\nv:null = 11\nv:none = 22\n",
			want: []syntax.Diagnostic{
				{Code: "vim/E46", Message: `Cannot change read-only variable "v:true"`},
				{Code: "vim/E46", Message: `Cannot change read-only variable "v:false"`},
				{Code: "vim/E46", Message: `Cannot change read-only variable "v:null"`},
				{Code: "vim/E46", Message: `Cannot change read-only variable "v:none"`},
			},
		},
		{
			name:   "def read-only Vim variables",
			source: "vim9script\ndef Test()\n  v:true = false\n  v:false = true\n  v:null = 11\n  v:none = 22\nenddef\n",
			want: []syntax.Diagnostic{
				{Code: "vim/E46", Message: `Cannot change read-only variable "v:true"`},
				{Code: "vim/E46", Message: `Cannot change read-only variable "v:false"`},
				{Code: "vim/E46", Message: `Cannot change read-only variable "v:null"`},
				{Code: "vim/E46", Message: `Cannot change read-only variable "v:none"`},
			},
		},
		{
			name:   "legacy read-only Vim variable",
			source: "let v:true = 0\n",
			want:   []syntax.Diagnostic{{Code: "vim/E46", Message: `Cannot change read-only variable "v:true"`}},
		},
		{
			name:   "legacy function arguments",
			source: "function Test(value, ...)\n  let a:value = 1\n  let a:000 += [1]\n  let a:0 = 1\n  let a:firstline = 2\n  let a:lastline = 3\nendfunction\n",
			want: []syntax.Diagnostic{
				{Code: "vim/E46", Message: `Cannot change read-only variable "a:value"`},
				{Code: "vim/E46", Message: `Cannot change read-only variable "a:000"`},
				{Code: "vim/E46", Message: `Cannot change read-only variable "a:0"`},
				{Code: "vim/E46", Message: `Cannot change read-only variable "a:firstline"`},
				{Code: "vim/E46", Message: `Cannot change read-only variable "a:lastline"`},
			},
		},
		{
			name:   "conservative exclusions",
			source: "vim9script\nconst fixed = [1]\nvar mutable = 1\nif true\n  var fixed = 2\n  fixed = 3\nendif\nmutable = 2\nv:errmsg = 'ok'\nlegacy fixed = 4\ns:fixed = 5\nfixed.member = 6\nfixed[0] = 6\n[fixed] = [7]\nfixed += 8\nfixed++\nfixed--\nmissing = 9\nfixed =\ndef Modern(value)\n  a:value = 1\nenddef\nfunction Legacy(value)\n  let a:missing = 1\n  let a:value[0] = 1\nendfunction\nlegacy let a:value = 1\n",
		},
		{
			name:   "embedded command",
			source: "vim9script\nconst fixed = 1\nglobal /x/ fixed = 2\n",
			want:   []syntax.Diagnostic{{Code: "vim/E46", Message: `Cannot change read-only variable "fixed"`}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			got := Analyze(file).Diagnostics
			if len(got) != len(test.want) {
				t.Fatalf("diagnostics = %#v, want %#v", got, test.want)
			}
			last := -1
			for index := range got {
				if got[index].Code != test.want[index].Code || got[index].Message != test.want[index].Message {
					t.Fatalf("diagnostic[%d] = %#v, want %#v", index, got[index], test.want[index])
				}
				name := immutableDiagnosticName(got[index].Message)
				start := strings.LastIndex(test.source, name)
				wantSpan := syntax.Span{Start: start, End: start + len(name)}
				if got[index].Span.Start <= last || got[index].Span != wantSpan || file.Text(got[index].Span) != name {
					t.Fatalf("diagnostic[%d] span = %#v (%q), want %#v (%q)", index, got[index].Span, file.Text(got[index].Span), wantSpan, name)
				}
				last = got[index].Span.Start
			}
		})
	}
}

func TestAnalyzeStringIndexAssignmentDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		want         []string
	}{
		{
			name:   "vim9 script",
			source: "vim9script\nvar s = 'abc'\ns[1] += 'x'\ns[2] ..= 'y'\n",
			want:   []string{"s[1]", "s[2]"},
		},
		{
			name:   "legacy script",
			source: "let s = 'abc'\nlet s[1] = 5\n",
			want:   []string{"s[1]"},
		},
		{
			name:   "compiled def",
			source: "vim9script\ndef F()\n  var s = 'abc'\n  s[1] += 'x'\nenddef\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []string
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code != "vim/E689" {
					continue
				}
				// The message retains the complete assignment, while the span
				// selects only the invalid indexed target.
				if !strings.HasPrefix(diagnostic.Message, "Index not allowed after a string: "+file.Text(diagnostic.Span)) {
					t.Fatalf("diagnostic = %#v", diagnostic)
				}
				got = append(got, file.Text(diagnostic.Span))
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("E689 spans = %q, want %q; diagnostics = %#v", got, test.want, result.Diagnostics)
			}
		})
	}
}

func immutableDiagnosticName(message string) string {
	if index := strings.LastIndex(message, ": "); index >= 0 {
		return message[index+2:]
	}
	return strings.Trim(message[len(`Cannot change read-only variable `):], `"`)
}

func TestAnalyzeUndefinedVim9DefIdentifiers(t *testing.T) {
	source := `vim9script
var scriptValue = 1
def Check(value: number)
  echo missing
  echo missing + scriptValue
  if value > 0
    echo blockMissing
  endif
  echo MissingCall()
  echo MissingGeneric<number>(1)
  echo s:scoped
  echo this
  echo super
  echo v:nosuch
  var validVimVariable = v:version
  var lambda = (item: number) => item + lambdaMissing
  legacy echo legacyMissing
enddef
var outsideLambda = (item: number) => item + outsideLambdaMissing
echo &t_TI
echo $VIMLS_MISSING
echo @z
echo outsideMissing
`
	file := syntax.Parse(source)
	result := Analyze(file)
	want := []string{"missing", "missing", "blockMissing", "v:nosuch", "lambdaMissing", "outsideLambdaMissing"}
	var got []syntax.Diagnostic
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1001" {
			got = append(got, diagnostic)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("E1001 diagnostics = %#v, want %v (all diagnostics = %#v, syntax diagnostics = %#v)", got, want, result.Diagnostics, file.Diagnostics)
	}
	for index, name := range want {
		diagnostic := got[index]
		if diagnostic.Message != "Variable not found: "+name || file.Text(diagnostic.Span) != name {
			t.Fatalf("diagnostic[%d] = %#v (%q), want %q", index, diagnostic, file.Text(diagnostic.Span), name)
		}
	}
}

func TestAnalyzeUndefinedVim9ScriptIdentifiers(t *testing.T) {
	source := `vim9script
var known = 1
var ordinary = missing
var member = root.value
var interpolation = $"foo{inside}"
var indexed = g:items[indexMissing]
echo a:somevar
echo l:somevar
echo x:somevar
var vimVariable = v:nosuch
v:another += 1
echo g:dynamic
echo s:maybe
def Check()
  echo defMissing
  echo a:argument
enddef
var Lambda = () => lambdaMissing
legacy echo legacyMissing
echo known
`
	file := syntax.Parse(source)
	result := Analyze(file)
	wantE121 := []string{"missing", "root", "inside", "indexMissing", "a:somevar", "l:somevar", "x:somevar", "v:nosuch", "v:another"}
	var gotE121 []syntax.Diagnostic
	var gotCompiled []syntax.Diagnostic
	for _, diagnostic := range result.Diagnostics {
		switch diagnostic.Code {
		case "vim/E121":
			gotE121 = append(gotE121, diagnostic)
		case "vim/E1001", "vim/E1075":
			gotCompiled = append(gotCompiled, diagnostic)
		}
	}
	if len(gotE121) != len(wantE121) {
		t.Fatalf("E121 diagnostics = %#v, want %v; syntax diagnostics = %#v; all diagnostics = %#v", gotE121, wantE121, file.Diagnostics, result.Diagnostics)
	}
	for index, name := range wantE121 {
		if gotE121[index].Message != "Undefined variable: "+name || file.Text(gotE121[index].Span) != name {
			t.Fatalf("E121[%d] = %#v on %q, want %q", index, gotE121[index], file.Text(gotE121[index].Span), name)
		}
	}
	wantCompiled := []struct{ code, message, text string }{
		{code: "vim/E1001", message: "Variable not found: defMissing", text: "defMissing"},
		{code: "vim/E1075", message: "Namespace not supported: a:argument", text: "a:argument"},
		{code: "vim/E1001", message: "Variable not found: lambdaMissing", text: "lambdaMissing"},
	}
	if len(gotCompiled) != len(wantCompiled) {
		t.Fatalf("compiled diagnostics = %#v, want %#v; all diagnostics = %#v", gotCompiled, wantCompiled, result.Diagnostics)
	}
	for index, want := range wantCompiled {
		if gotCompiled[index].Code != want.code || gotCompiled[index].Message != want.message || file.Text(gotCompiled[index].Span) != want.text {
			t.Fatalf("compiled diagnostic[%d] = %#v on %q, want %#v", index, gotCompiled[index], file.Text(gotCompiled[index].Span), want)
		}
	}
}

func TestAnalyzeE1089UnknownAssignmentTargets(t *testing.T) {
	source := `vim9script
var scriptValue = {}
def Check(parameter: number)
  var output = ''
  missing = missingValue
  [firstMissing, secondMissing] = [1, 2]
  unknownMember.key = 1
  unknownIndex['key'] = 1
  scriptValue.key = 1
  output[0] = 'x'
  parameter = 2
  redir => output
  redir END
  redir => redirMissing
  redir END
enddef
function Legacy()
  let legacyMissing = 1
endfunction
`
	file := syntax.Parse(source)
	result := Analyze(file)
	var got []syntax.Diagnostic
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1089" {
			got = append(got, diagnostic)
		}
	}
	want := []string{"missing", "firstMissing", "secondMissing", "unknownMember", "unknownIndex", "redirMissing"}
	if len(got) != len(want) {
		t.Fatalf("E1089 diagnostics = %#v, want names %v; all diagnostics = %#v", got, want, result.Diagnostics)
	}
	for index, name := range want {
		if got[index].Message != "Unknown variable: "+name || file.Text(got[index].Span) != name {
			t.Fatalf("E1089[%d] = %#v (%q), want %q", index, got[index], file.Text(got[index].Span), name)
		}
	}
	var rhs bool
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1001" && diagnostic.Message == "Variable not found: missingValue" && file.Text(diagnostic.Span) == "missingValue" {
			rhs = true
		}
		if diagnostic.Code == "vim/E1089" {
			switch file.Text(diagnostic.Span) {
			case "scriptValue", "output", "parameter":
				t.Fatalf("declared assignment target reported E1089: %#v", diagnostic)
			}
		}
	}
	if !rhs {
		t.Fatalf("RHS E1001 was suppressed: %#v", result.Diagnostics)
	}
}

func TestAnalyzeSearchpairCompiledExpressionIdentifiers(t *testing.T) {
	source := `vim9script
def Check()
  var flag = true
  searchpair("a", "b", "c", "d", "missing", 33)
  searchpair('a', 'b', 'c', 'd', 'flag && v:true', 33)
enddef
`
	file := syntax.Parse(source)
	var diagnostics []syntax.Diagnostic
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vim/E1001" {
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	if len(diagnostics) != 1 || diagnostics[0].Message != "Variable not found: missing" || file.Text(diagnostics[0].Span) != "missing" {
		t.Fatalf("E1001 diagnostics = %#v", diagnostics)
	}
}

func TestAnalyzeSearchpairInvalidLiteralFlags(t *testing.T) {
	source := `vim9script
echo searchpair("a", "b", "c", "d", "f", 33)
echo searchpairpos('a', 'b', 'c', 'ns')
echo searchpair('a', 'b', 'c', 'bW')
var flags = 'd'
echo searchpair('a', 'b', 'c', flags)
def Deferred()
  echo searchpair("a", "b", "c", "d", "missing", 33)
enddef
`
	file := syntax.Parse(source)
	result := Analyze(file)
	var invalid []syntax.Diagnostic
	var compiledMissing bool
	for _, diagnostic := range result.Diagnostics {
		switch diagnostic.Code {
		case "vim/E475":
			invalid = append(invalid, diagnostic)
		case "vim/E1001":
			if file.Text(diagnostic.Span) == "missing" {
				compiledMissing = true
			}
		case "vim/E121":
			t.Fatalf("script skip expression was compiled: %#v", diagnostic)
		}
	}
	want := []struct {
		message string
		text    string
	}{
		{message: "Invalid argument: d", text: `"d"`},
		{message: "Invalid argument: ns", text: "'ns'"},
	}
	if len(invalid) != len(want) {
		t.Fatalf("E475 diagnostics = %#v, want %v; all diagnostics = %#v", invalid, want, result.Diagnostics)
	}
	for index, diagnostic := range invalid {
		if diagnostic.Message != want[index].message || file.Text(diagnostic.Span) != want[index].text {
			t.Fatalf("E475[%d] = %#v on %q, want %v", index, diagnostic, file.Text(diagnostic.Span), want[index])
		}
	}
	if !compiledMissing {
		t.Fatalf("def skip expression did not retain E1001: %#v", result.Diagnostics)
	}
}

func TestAnalyzeE488TrailingCharacters(t *testing.T) {
	source := `vim9script
assert_equal("4", substitute("3", '\d', '\="text" x', 'g'))
assert_equal("4", substitute("3", '\d', '\=str2nr("3") + 1', 'g'))
var text = ''
var scriptMember = text.memb
def Check()
  assert_equal("4", substitute("3", '\d', '\="text" x', 'g'))
  var localText = ''
  var defMember = localText.memb
enddef
`
	file := syntax.Parse(source)
	var got []syntax.Diagnostic
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vim/E488" {
			got = append(got, diagnostic)
		}
	}
	want := []string{"x", ".memb", "x"}
	if len(got) != len(want) {
		t.Fatalf("E488 diagnostics = %#v, want %q", got, want)
	}
	for index, diagnostic := range got {
		if diagnostic.Message != "Trailing characters: "+want[index] || file.Text(diagnostic.Span) != want[index] {
			t.Fatalf("E488[%d] = %#v on %q, want %q", index, diagnostic, file.Text(diagnostic.Span), want[index])
		}
	}
}

func TestAnalyzeE1017VariableRedeclaration(t *testing.T) {
	tests := []struct {
		name   string
		source string
		text   string
	}{
		{
			name:   "local variable",
			source: "vim9script\ndef F()\n  final one = 234\n  var one = 99\n  legacy echo one\nenddef\nvar after = 1\n",
			text:   "one",
		},
		{
			name:   "for binding",
			source: "vim9script\ndef F()\n  var x = 5\n  for x in range(5)\n  endfor\nenddef\nvar after = 1\n",
			text:   "x",
		},
		{
			name:   "script compound target",
			source: "vim9script\nvar dd = {one: 1}\nvar dd.one = 2\nvar after = 1\n",
			text:   "dd",
		},
		{
			name:   "block lambda local",
			source: "vim9script\nvar Callback = () => {\n  var inner = 1\n  var inner = 2\n}\nvar after = 1\n",
			text:   "inner",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1017" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Variable already declared: "+test.text || file.Text(got[0].Span) != test.text {
				t.Fatalf("E1017 diagnostics = %#v; syntax diagnostics = %#v", got, file.Diagnostics)
			}
			if file.Commands[len(file.Commands)-1].Declaration == nil {
				t.Fatalf("next-line recovery = %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nvar value = 1\nvar value = 2\n",
		"vim9script\ndef F()\n  var value = 1\n  if true\n    var value = 2\n  endif\nenddef\n",
		"function F()\n  let value = 1\n  let value = 2\nendfunction\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1017" {
				t.Fatalf("guard source reported E1017: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1041ScriptItemRedefinition(t *testing.T) {
	tests := []struct {
		name, source, text, forbidden string
		want                          int
	}{
		{"official for binding", "vim9script\nvar x = 5\nfor x in range(5)\nendfor\n", "x", "vim/E1017", 1},
		{"script variables", "vim9script\nvar x = 1\nvar x = 2\n", "x", "vim/E1017", 1},
		{"legacy guard", "var x = 1\nvar x = 2\n", "x", "", 0},
		{"def local uses E1017", "vim9script\ndef F()\n  var x = 1\n  var x = 2\nenddef\n", "x", "", 0},
		{"def then variable", "vim9script\ndef Func()\nenddef\nvar Func = 1\n", "Func", "vim/E1073", 1},
		{"variable then def", "vim9script\nvar Func = 1\ndef Func()\nenddef\n", "Func", "vim/E1073", 1},
		{"variable then class", "vim9script\nvar Thing = 1\nclass Thing\nendclass\n", "Thing", "", 1},
		{"enum then variable", "vim9script\nenum Thing\n  One\nendenum\nvar Thing = 1\n", "Thing", "", 1},
		{"duplicate class", "vim9script\nclass Thing\nendclass\nclass Thing\nendclass\n", "Thing", "", 1},
		{"variable then type alias", "vim9script\nvar Thing = 1\ntype Thing = string\n", "Thing", "", 1},
		{"type alias then variable", "vim9script\ntype Thing = number\nvar Thing = 1\n", "Thing", "", 1},
		{"type aliases use E1396", "vim9script\ntype Thing = number\ntype Thing = string\n", "Thing", "", 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			diagnostics := Analyze(file).Diagnostics
			var got []syntax.Diagnostic
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == "vim/E1041" {
					got = append(got, diagnostic)
				}
				if test.forbidden != "" && diagnostic.Code == test.forbidden {
					t.Fatalf("%s conflict used %s: %#v", test.name, test.forbidden, diagnostic)
				}
			}
			if len(got) != test.want {
				t.Fatalf("E1041 diagnostics = %#v", got)
			}
			if test.want == 1 {
				if got[0].Message != `Redefining script item: "`+test.text+`"` || file.Text(got[0].Span) != test.text {
					t.Fatalf("E1041 diagnostic = %#v, text=%q", got[0], file.Text(got[0].Span))
				}
			}
		})
	}
}

func TestAnalyzeE1041GenericTypeParameterConflicts(t *testing.T) {
	tests := []struct {
		name, source, text string
	}{
		{"type alias", "vim9script\ntype A = number\ndef Fn<A>()\nenddef\n", "A"},
		{"script function", "vim9script\ndef MyFunc()\nenddef\ndef Fn<MyFunc>()\nenddef\n", "MyFunc"},
		{"class", "vim9script\nclass A\nendclass\ndef Fn<A>()\nenddef\n", "A"},
		{"script variable", "vim9script\nvar B = 'abc'\ndef Fn<A, B>()\nenddef\n", "B"},
		{"generic function name", "vim9script\ndef I<A>(x: A): A\n  return x\nenddef\ndef Id<I>(x: I): I\n  return x\nenddef\n", "I"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1041" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != `Redefining script item: "`+test.text+`"` || file.Text(got[0].Span) != test.text {
				t.Fatalf("E1041 diagnostics = %#v, want one on %q", got, test.text)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef One<A>()\nenddef\ndef Two<A>()\nenddef\n",
		"vim9script\ndef Outer<T>()\n  def Inner<T>()\n  enddef\nenddef\n",
		"vim9script\ndef Duplicate<A, A>()\nenddef\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1041" {
				t.Fatalf("reusable or duplicate local generic parameter reported E1041: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1073NameAlreadyDefined(t *testing.T) {
	tests := []struct {
		name, source, text string
		want               int
	}{
		{"nested defs", "vim9script\ndef Outer()\n  def Inner()\n  enddef\n  def Inner()\n  enddef\nenddef\n", "Inner", 1},
		{"parameter and nested def", "vim9script\ndef Outer(Ref: number)\n  def Ref()\n  enddef\nenddef\n", "Ref", 1},
		{"local and nested def", "vim9script\ndef Outer()\n  var Inner = 1\n  def Inner()\n  enddef\nenddef\n", "Inner", 1},
		{"script defs", "vim9script\ndef Func()\nenddef\ndef Func()\nenddef\n", "Func", 1},
		{"script and nested def", "vim9script\ndef Func()\nenddef\ndef Outer()\n  def Func()\n  enddef\nenddef\n", "Func", 1},
		{"import alias", "vim9script\nimport autoload 'one.vim' as one\nimport autoload 'two.vim' as one\n", "one", 1},
		{"function and import alias", "vim9script\ndef one()\nenddef\nimport autoload 'one.vim' as one\n", "one", 1},
		{"variable and import alias", "vim9script\nvar one = 1\nimport autoload 'one.vim' as one\n", "one", 0},
		{"different functions", "vim9script\ndef A()\n  def Inner()\n  enddef\nenddef\ndef B()\n  def Inner()\n  enddef\nenddef\n", "Inner", 0},
		{"legacy root def", "def Outer()\n  def Inner()\n  enddef\n  def Inner()\n  enddef\nenddef\n", "Inner", 1},
		{"legacy function", "function Outer()\n  function Inner()\n  endfunction\n  function Inner()\n  endfunction\nendfunction\n", "Inner", 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1073" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != test.want || test.want == 1 && (got[0].Message != "Name already defined: "+test.text || file.Text(got[0].Span) != test.text) {
				t.Fatalf("E1073 diagnostics = %#v", got)
			}
		})
	}
}

func TestAnalyzeE1054ImportAliasConflictsWithScriptItem(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		item   string
	}{
		{name: "variable", prefix: "var exported = 'something'", item: "exported"},
		{name: "constant", prefix: "const exported = 'something'", item: "exported"},
		{name: "final", prefix: "final exported = 'something'", item: "exported"},
		{name: "class", prefix: "class Exported\nendclass", item: "Exported"},
		{name: "type alias", prefix: "type Exported = number", item: "Exported"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse("vim9script\n" + test.prefix + "\nimport './Xexport.vim' as " + test.item + "\n")
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1054" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Variable already declared in the script: "+test.item || file.Text(got[0].Span) != test.item {
				t.Fatalf("E1054 diagnostics = %#v", got)
			}
		})
	}
}

func TestAnalyzeE1093DestructuringCardinality(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		message   string
		rightHand string
	}{
		{
			name:      "declaration too few",
			source:    "vim9script\ndef F()\n  var [v1, v2] = [1]\nenddef\nvar after = 1\n",
			message:   "Expected 2 items but got 1",
			rightHand: "[1]",
		},
		{
			name:      "single declaration too many",
			source:    "vim9script\ndef F()\n  var [v1] = [1, 2]\nenddef\nvar after = 1\n",
			message:   "Expected 1 items but got 2",
			rightHand: "[1, 2]",
		},
		{
			name:      "declaration rest too few",
			source:    "vim9script\ndef F()\n  var [v1, v2; rest] = [1]\nenddef\nvar after = 1\n",
			message:   "Expected 2 items but got 1",
			rightHand: "[1]",
		},
		{
			name:      "assignment too many",
			source:    "vim9script\ndef F()\n  var v1: number\n  var v2: number\n  [v1, v2] = [1, 2, 3]\nenddef\nvar after = 1\n",
			message:   "Expected 2 items but got 3",
			rightHand: "[1, 2, 3]",
		},
		{
			name:      "assignment rest too few",
			source:    "vim9script\ndef F()\n  var v1: number\n  var v2: number\n  [v1, v2; _] = [1]\nenddef\nvar after = 1\n",
			message:   "Expected 2 items but got 1",
			rightHand: "[1]",
		},
		{
			name:      "block lambda declaration",
			source:    "vim9script\nvar Callback = () => {\n  var [v1, v2] = [1]\n}\nvar after = 1\n",
			message:   "Expected 2 items but got 1",
			rightHand: "[1]",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1093" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.rightHand {
				t.Fatalf("E1093 diagnostics = %#v; syntax diagnostics = %#v", got, file.Diagnostics)
			}
			if file.Commands[len(file.Commands)-1].Declaration == nil {
				t.Fatalf("next-line recovery = %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef F(values: list<any>)\n  var [v1, v2] = values\nenddef\n",
		"vim9script\ndef F()\n  var [v1, v2] = [1, 2]\nenddef\n",
		"function F()\n  let [v1, v2] = [1]\nendfunction\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1093" {
				t.Fatalf("guard source reported E1093: %#v\n%s", diagnostic, source)
			}
		}
	}
}
