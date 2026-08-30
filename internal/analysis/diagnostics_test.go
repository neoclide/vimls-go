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

	positive := syntax.Parse("vim9script\nsubstitute('Hallo', 'a', 'e', '')\n{'a': 1}->keys()\n")
	for _, diagnostic := range Analyze(positive).Diagnostics {
		if diagnostic.Code == "vim/E1174" || diagnostic.Code == "vim/E1206" {
			t.Fatalf("valid builtin argument diagnostic = %#v", diagnostic)
		}
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
	var unknownMessages []string
	var mismatchSpans []string
	for _, diagnostic := range result.Diagnostics {
		switch diagnostic.Code {
		case syntax.DiagnosticUnknownOption:
			unknownSpans = append(unknownSpans, file.Text(diagnostic.Span))
			unknownMessages = append(unknownMessages, diagnostic.Message)
		case "vim/E1012":
			mismatchSpans = append(mismatchSpans, file.Text(diagnostic.Span))
		}
	}
	wantUnknown := []string{"tabs", "futureoption", "&futureoption", "&futureoption"}
	if !reflect.DeepEqual(unknownSpans, wantUnknown) {
		t.Fatalf("unknown option spans = %#v, want %#v; diagnostics = %#v", unknownSpans, wantUnknown, result.Diagnostics)
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
			name:   "optional variadic and conservative targets",
			source: "vim9script\nvar optional = range(1)\nvar variadic = instanceof(null_object, 2, 3, 4)\nvar dynamic = call('len', [])\nMyFunction()\ns:len()\nitems->len()\n",
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
			name:   "conservative exclusions",
			source: "vim9script\nconst fixed = [1]\nvar mutable = 1\nif true\n  var fixed = 2\n  fixed = 3\nendif\nmutable = 2\nlegacy fixed = 4\ns:fixed = 5\nfixed.member = 6\nfixed[0] = 6\n[fixed] = [7]\nfixed += 8\nfixed++\nfixed--\nmissing = 9\nfixed =\n",
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
				start := strings.LastIndex(test.source, name+" =")
				wantSpan := syntax.Span{Start: start, End: start + len(name)}
				if got[index].Span.Start <= last || got[index].Span != wantSpan || file.Text(got[index].Span) != name {
					t.Fatalf("diagnostic[%d] span = %#v (%q), want %#v (%q)", index, got[index].Span, file.Text(got[index].Span), wantSpan, name)
				}
				last = got[index].Span.Start
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
		{"variable and import alias", "vim9script\nvar one = 1\nimport autoload 'one.vim' as one\n", "one", 1},
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
