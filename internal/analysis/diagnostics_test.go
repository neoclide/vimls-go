package analysis

import (
	"reflect"
	"strconv"
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
		{"index container", "vim9script\ndef F()\n  index('x', 'x')\nenddef\n", 1},
		{"join container", "vim9script\ndef F()\n  join('x')\nenddef\n", 1},
		{"max container", "vim9script\ndef F()\n  max(5)\nenddef\n", 1},
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

func TestAnalyzeE1428DuplicateEnumValue(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "across lines",
			source: "vim9script\nenum A\n  Foo,\n  Bar,\n  Foo\nendenum\n",
			want:   "Foo",
		},
		{
			name:   "same logical line stops after first duplicate",
			source: "vim9script\nenum A\n  Foo, Bar, Foo,\n  Bar\nendenum\n",
			want:   "Foo",
		},
		{
			name:   "case sensitive and scoped per enum",
			source: "vim9script\nenum A\n  Foo, foo\nendenum\nenum B\n  Foo\nendenum\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1428" {
					got = append(got, diagnostic)
				}
			}
			if test.want == "" {
				if len(got) != 0 {
					t.Fatalf("E1428 diagnostics = %#v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Message != "Duplicate enum value: "+test.want || file.Text(got[0].Span) != test.want || got[0].Span.Start != strings.LastIndex(test.source, test.want) {
				t.Fatalf("E1428 diagnostics = %#v; all diagnostics = %#v", got, result.Diagnostics)
			}
		})
	}
}

func TestAnalyzeE1427EnumNameCannotBeModified(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   int
	}{
		{
			name:   "constructor",
			source: "vim9script\nenum Planet\n  Mercury\n  def new()\n    this.name = 'foo'\n  enddef\nendenum\n",
			want:   1,
		},
		{
			name:   "ordinary enum method compound assignment",
			source: "vim9script\nenum Planet\n  Mercury\n  def Rename()\n    this.name ..= '!'\n  enddef\nendenum\n",
			want:   1,
		},
		{
			name:   "read and ordinary member assignment",
			source: "vim9script\nenum Planet\n  Mercury\n  var label: string\n  def Rename()\n    echo this.name\n    this.label = 'foo'\n  enddef\nendenum\n",
		},
		{
			name:   "outside enum",
			source: "vim9script\nenum Planet\n  Mercury\nendenum\ndef Rename()\n  Planet.Mercury.name = 'foo'\nenddef\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1427" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != test.want {
				t.Fatalf("E1427 diagnostics = %#v; all diagnostics = %#v", got, result.Diagnostics)
			}
			if test.want == 1 && (got[0].Message != "Enum \"Planet\" name cannot be modified" || file.Text(got[0].Span) != "this.name") {
				t.Fatalf("E1427 diagnostic = %#v", got[0])
			}
		})
	}
}

func TestAnalyzeE1426EnumOrdinalCannotBeModified(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   int
	}{
		{
			name:   "constructor",
			source: "vim9script\nenum Planet\n  Mercury\n  def new()\n    this.ordinal = 20\n  enddef\nendenum\n",
			want:   1,
		},
		{
			name:   "ordinary enum method compound assignment",
			source: "vim9script\nenum Planet\n  Mercury\n  def Advance()\n    this.ordinal += 1\n  enddef\nendenum\n",
			want:   1,
		},
		{
			name:   "read and ordinary member assignment",
			source: "vim9script\nenum Planet\n  Mercury\n  var count: number\n  def Advance()\n    echo this.ordinal\n    this.count = 1\n  enddef\nendenum\n",
		},
		{
			name:   "class member",
			source: "vim9script\nclass Counter\n  var ordinal: number\n  def Advance()\n    this.ordinal = 1\n  enddef\nendclass\n",
		},
		{
			name:   "outside enum",
			source: "vim9script\nenum Planet\n  Mercury\nendenum\ndef Advance()\n  Planet.Mercury.ordinal = 1\nenddef\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1426" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != test.want {
				t.Fatalf("E1426 diagnostics = %#v; all diagnostics = %#v", got, result.Diagnostics)
			}
			if test.want == 1 && (got[0].Message != "Enum \"Planet\" ordinal value cannot be modified" || file.Text(got[0].Span) != "this.ordinal") {
				t.Fatalf("E1426 diagnostic = %#v", got[0])
			}
		})
	}
}

func TestAnalyzeE1423EnumValueCannotBeModified(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   string
		inDef  bool
	}{
		{name: "enum value", target: "Foo.Apple", want: "Foo.Apple"},
		{name: "enum value in def", target: "Foo.Apple", want: "Foo.Apple", inDef: true},
		{name: "builtin name", target: "Foo.Apple.name", want: "Foo.name", inDef: true},
		{name: "builtin ordinal", target: "Foo.Apple.ordinal", want: "Foo.ordinal", inDef: true},
		{name: "instance member", target: "Foo.Apple.count", want: "Foo.count", inDef: true},
		{name: "top level object member uses runtime diagnostic", target: "Foo.Apple.name"},
		{name: "unknown enum value", target: "Foo.Pear", inDef: true},
		{name: "unknown member", target: "Foo.Apple.missing", inDef: true},
		{name: "ordinary receiver", target: "Other.Apple", inDef: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assignment := "  " + test.target + " = 20\n"
			if test.inDef {
				assignment = "def Fn()\n  " + test.target + " = 20\nenddef\n"
			}
			source := "vim9script\nenum Foo\n  Apple\n  var count: number\nendenum\n" + assignment
			file := syntax.Parse(source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1423" {
					got = append(got, diagnostic)
				}
			}
			if test.want == "" {
				if len(got) != 0 {
					t.Fatalf("E1423 diagnostics = %#v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Message != "Enum value \""+test.want+"\" cannot be modified" || file.Text(got[0].Span) != test.target {
				t.Fatalf("E1423 diagnostics = %#v; all diagnostics = %#v", got, result.Diagnostics)
			}
		})
	}

	for name, source := range map[string]string{
		"shadowed enum name": "vim9script\nenum Foo\n  Apple\nendenum\ndef Fn(Foo: any)\n  Foo.Apple = 20\nenddef\n",
		"inside owning enum": "vim9script\nenum Foo\n  Apple\n  def Rename()\n    Foo.Apple.name = 'x'\n  enddef\nendenum\n",
	} {
		t.Run(name, func(t *testing.T) {
			result := Analyze(syntax.Parse(source))
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1423" {
					t.Fatalf("unexpected E1423 = %#v", diagnostic)
				}
			}
		})
	}
}

func TestAnalyzeE1422EnumValueNotFound(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{
			name:   "missing value",
			source: "vim9script\nenum Foo\n  apple, orange\nendenum\nvar value: Foo = Foo.pear\n",
			want:   true,
		},
		{
			name:   "missing assignment value in def",
			source: "vim9script\nenum Foo\n  apple\nendenum\ndef Fn()\n  Foo.pear = Foo.apple\nenddef\n",
			want:   true,
		},
		{
			name:   "known selectors",
			source: "vim9script\nenum Foo\n  apple\n  static var farm: string\n  static def Work()\n  enddef\nendenum\necho Foo.apple\necho Foo.values\necho Foo.farm\necho Foo.Work\n",
		},
		{
			name:   "object member",
			source: "vim9script\nenum Foo\n  apple\nendenum\necho Foo.apple.missing\n",
		},
		{
			name:   "shadowed enum",
			source: "vim9script\nenum Foo\n  apple\nendenum\ndef Fn(Foo: any)\n  echo Foo.pear\nenddef\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1422" {
					got = append(got, diagnostic)
				}
			}
			wantCount := 0
			if test.want {
				wantCount = 1
			}
			if len(got) != wantCount {
				t.Fatalf("E1422 diagnostics = %#v; all diagnostics = %#v", got, result.Diagnostics)
			}
			if test.want && (got[0].Message != "Enum value \"pear\" not found in enum \"Foo\"" || file.Text(got[0].Span) != "pear") {
				t.Fatalf("E1422 diagnostic = %#v", got[0])
			}
		})
	}
}

func TestAnalyzeE1421EnumCannotBeUsedAsValue(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "initializer", body: "var value = Fruit", want: "Fruit"},
		{name: "binary operand", body: "var value = 1 + Fruit", want: "Fruit"},
		{name: "concatenation operand", body: "var value = 'x' .. Fruit", want: "Fruit"},
		{name: "script assignment target", body: "Fruit = 10", want: "Fruit"},
		{name: "assignment rhs wins", body: "Fruit = Color", want: "Color"},
		{name: "enum value", body: "var value = Fruit.Apple"},
		{name: "enum values", body: "var values = Fruit.values"},
		{name: "type builtin", body: "echo type(Fruit)\necho typename(Fruit)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := "vim9script\nenum Fruit\n  Apple\nendenum\nenum Color\n  Red\nendenum\n" + test.body + "\n"
			file := syntax.Parse(source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1421" {
					got = append(got, diagnostic)
				}
			}
			if test.want == "" {
				if len(got) != 0 {
					t.Fatalf("E1421 diagnostics = %#v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Message != "Enum \""+test.want+"\" cannot be used as a value" || file.Text(got[0].Span) != test.want {
				t.Fatalf("E1421 diagnostics = %#v; all diagnostics = %#v", got, result.Diagnostics)
			}
		})
	}

	shadowed := Analyze(syntax.Parse("vim9script\nenum Fruit\n  Apple\nendenum\ndef Fn(Fruit: any)\n  echo Fruit\nenddef\n"))
	for _, diagnostic := range shadowed.Diagnostics {
		if diagnostic.Code == "vim/E1421" {
			t.Fatalf("shadowed enum reported E1421: %#v", diagnostic)
		}
	}
}

func TestAnalyzeE1411MissingDotAfterObject(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "inferred object", body: "var o = A.new()\ndef F()\n  o += 4\nenddef", want: true},
		{name: "typed object", body: "var o: A = A.new()\ndef F()\n  o ..= 'x'\nenddef", want: true},
		{name: "simple assignment", body: "var o = A.new()\ndef F()\n  o = A.new()\nenddef"},
		{name: "object member", body: "var o = A.new()\ndef F()\n  o.value += 1\nenddef"},
		{name: "unknown any", body: "var o: any = A.new()\ndef F()\n  o += 1\nenddef"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse("vim9script\nclass A\n  var value: number\nendclass\n" + test.body + "\n")
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1411" {
					got = append(got, diagnostic)
				}
			}
			wantCount := 0
			if test.want {
				wantCount = 1
			}
			if len(got) != wantCount {
				t.Fatalf("E1411 diagnostics = %#v; all diagnostics = %#v", got, result.Diagnostics)
			}
			if test.want && (got[0].Message != "Missing dot after object \"o\"" || file.Text(got[0].Span) != "o") {
				t.Fatalf("E1411 diagnostic = %#v", got[0])
			}
			if test.want {
				for _, diagnostic := range result.Diagnostics {
					if diagnostic.Code == "vim/E1051" || diagnostic.Code == "vim/E1019" {
						t.Fatalf("cascading diagnostic = %#v", diagnostic)
					}
				}
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

func TestAnalyzeE1186VoidEchoExpressionDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source, span string
		recovery           bool
	}{
		{"official top-level echo recovery", "vim9script\ndef NoReturn()\nenddef\necho NoReturn()\nvar after = 1\n", "NoReturn()", true},
		{"compiled def echo", "vim9script\ndef NoReturn()\nenddef\ndef Func()\n  echo NoReturn()\nenddef\n", "NoReturn()", false},
		{"echon", "vim9script\ndef NoReturn()\nenddef\nechon NoReturn()\n", "NoReturn()", false},
		{"later expression item", "vim9script\ndef NoReturn()\nenddef\necho 1 NoReturn()\n", "NoReturn()", false},
		{"Legacy-root def echo", "def NoReturn()\nenddef\necho NoReturn()\n", "NoReturn()", false},
		{"typed lambda", "vim9script\ndef NoReturn()\nenddef\nvar Callback: func = () => {\n  echo NoReturn()\n}\n", "NoReturn()", false},
		{"assigned lambda", "vim9script\ndef NoReturn()\nenddef\nvar Callback: func\nCallback = () => {\n  echo NoReturn()\n}\n", "NoReturn()", false},
		{"explicit legacy echo", "vim9script\ndef NoReturn()\nenddef\nlegacy echo NoReturn()\n", "NoReturn()", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1186" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1031" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1186 source retained E1031: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "Expression does not result in a value: "+test.span || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1186 diagnostics = %#v", got)
			}
			if test.recovery && (file.Commands[len(file.Commands)-1].Declaration == nil || file.Text(file.Commands[len(file.Commands)-1].Declaration.Name) != "after") {
				t.Fatalf("following declaration was not retained: %#v", file.Commands)
			}
		})
	}

	for _, command := range []string{"echomsg", "echoerr", "echoconsole", "echowindow", "execute"} {
		t.Run("compiled "+command, func(t *testing.T) {
			file := syntax.Parse("vim9script\ndef NoReturn()\nenddef\ndef Func()\n  " + command + " NoReturn()\nenddef\n")
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1186" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Expression does not result in a value: NoReturn()" || file.Text(got[0].Span) != "NoReturn()" {
				t.Fatalf("E1186 diagnostics = %#v", got)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef NoReturn()\nenddef\nNoReturn()\n",
		"vim9script\ndef NoReturn()\nenddef\nechomsg NoReturn()\nechoerr NoReturn()\nechoconsole NoReturn()\nechowindow NoReturn()\nexecute NoReturn()\n",
		"vim9script\ndef NoReturn()\nenddef\ndef Func()\n  legacy echomsg NoReturn()\nenddef\n",
		"vim9script\necho len([])\n",
		"vim9script\ndef NoReturn()\nenddef\necho NoReturn(\n",
		"vim9script\ndef NoReturn()\nenddef\ndef Func()\n  var value = NoReturn()\nenddef\n",
	} {
		file := syntax.Parse(source)
		result := Analyze(file)
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1186" {
				t.Fatalf("guard unexpectedly received E1186: %#v", result.Diagnostics)
			}
		}
	}
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

func TestAnalyzeE1030UsingStringAsNumber(t *testing.T) {
	for _, test := range []struct {
		name, source, message, span string
	}{
		{
			name: "script addition checks left first", source: "vim9script\nvar x = 'asdf' + 0z1122\n",
			message: `Using a String as a Number: "asdf"`, span: "'asdf'",
		},
		{
			name: "script multiplication", source: "vim9script\nvar x = '1' * '2'\n",
			message: `Using a String as a Number: "1"`, span: "'1'",
		},
		{
			name: "compiled unary minus", source: "vim9script\ndef F()\n  var x = -'xx'\nenddef\n",
			message: `Using a String as a Number: "xx"`, span: "'xx'",
		},
		{
			name: "script unary plus", source: "vim9script\nvar x = +'xx'\n",
			message: `Using a String as a Number: "xx"`, span: "'xx'",
		},
		{
			name: "script index", source: "vim9script\nvar x = 'asdf'['1']\n",
			message: `Using a String as a Number: "1"`, span: "'1'",
		},
		{
			name: "script slice checks first invalid bound", source: "vim9script\nvar x = 'asdf'['1' : '2']\n",
			message: `Using a String as a Number: "1"`, span: "'1'",
		},
		{
			name:    "known string without static value",
			source:  "vim9script\nvar index: string = input('index: ')\nvar x = 'asdf'[index]\n",
			message: "Using a String as a Number", span: "index",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1030" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1012" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1030 expression retained E1012: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1030 diagnostics = %#v, want %q on %q; all diagnostics = %#v", got, test.message, test.span, result.Diagnostics)
			}
		})
	}

	for _, test := range []struct {
		name, source, code string
	}{
		{name: "compiled addition", source: "vim9script\ndef F()\n  var x = 'asdf' + 0z1122\nenddef\n", code: "vim/E1051"},
		{name: "compiled multiplication", source: "vim9script\ndef F()\n  var x = '1' * '2'\nenddef\n", code: "vim/E1036"},
		{name: "compiled remainder", source: "vim9script\ndef F()\n  var x = '1' % '2'\nenddef\n", code: "vim/E1035"},
		{name: "compiled index", source: "vim9script\ndef F()\n  var x = 'asdf'['1']\nenddef\n", code: "vim/E1012"},
		{name: "Legacy coercion", source: "let g:x = -'xx'\nlet g:y = '1' * '2'\nlet g:z = 'asdf'['1']\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			foundContextCode := test.code == ""
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1030" {
					t.Fatalf("source unexpectedly received E1030: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.code {
					foundContextCode = true
				}
			}
			if !foundContextCode {
				t.Fatalf("diagnostics = %#v, want %s", result.Diagnostics, test.code)
			}
		})
	}
}

func TestAnalyzeE1037InvalidIdentityComparison(t *testing.T) {
	for _, test := range []struct {
		name, source, message, span string
	}{
		{
			name: "bool is", source: "vim9script\nvar x = true is false\n",
			message: `Cannot use "is" with bool`, span: "is",
		},
		{
			name: "special isnot", source: "vim9script\nvar x = v:none isnot v:null\n",
			message: `Cannot use "isnot" with special`, span: "isnot",
		},
		{
			name: "compiled number", source: "vim9script\ndef F()\n  var x = 123 is 123\nenddef\n",
			message: `Cannot use "is" with number`, span: "is",
		},
		{
			name: "float isnot", source: "vim9script\nvar x = 1.3 isnot 1.3\n",
			message: `Cannot use "isnot" with float`, span: "isnot",
		},
		{
			name: "vim9cmd", source: "vim9cmd echo 1 is 1\n",
			message: `Cannot use "is" with number`, span: "is",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1037" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1037 diagnostics = %#v, want %q on %q; all diagnostics = %#v", got, test.message, test.span, result.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"let g:x = 1 is 1\n",
		"vim9script\nvar x = 'a' is 'a'\n",
		"vim9script\nvar x = [1] is [1]\n",
		"vim9script\nvar x = 1 is 1.0\n",
		"vim9script\nvar x = v:none is 8\n",
		"vim9script\nvar x = 1 is# 1\n",
	} {
		result := Analyze(syntax.Parse(source))
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1037" {
				t.Fatalf("source %q unexpectedly received E1037: %#v", source, result.Diagnostics)
			}
		}
	}
}

func TestAnalyzeE1072InvalidComparison(t *testing.T) {
	for _, test := range []struct {
		name, expression, message string
	}{
		{name: "list and special", expression: "[] == v:none", message: "Cannot compare list with special"},
		{name: "number and special", expression: "123 == v:none", message: "Cannot compare number with special"},
		{name: "blob and special", expression: "0z00 == v:none", message: "Cannot compare blob with special"},
		{name: "special and bool", expression: "v:none == true", message: "Cannot compare special with bool"},
		{name: "ordered bools", expression: "false >= true", message: "Cannot compare bool with bool"},
		{name: "string and number", expression: "'' == 0", message: "Cannot compare string with number"},
	} {
		for _, context := range []string{"script", "def", "vim9cmd"} {
			t.Run(test.name+"/"+context, func(t *testing.T) {
				source := "vim9script\necho " + test.expression + "\n"
				if context == "def" {
					source = "vim9script\ndef F()\n  echo " + test.expression + "\nenddef\n"
				} else if context == "vim9cmd" {
					source = "vim9cmd echo " + test.expression + "\n"
				}
				file := syntax.Parse(source)
				result := Analyze(file)
				var got []syntax.Diagnostic
				for _, diagnostic := range result.Diagnostics {
					if diagnostic.Code == "vim/E1072" {
						got = append(got, diagnostic)
					}
				}
				if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != strings.Fields(test.expression)[1] {
					t.Fatalf("E1072 diagnostics = %#v; all diagnostics = %#v", got, result.Diagnostics)
				}
			})
		}
	}

	for _, source := range []string{
		"echo [] == v:none\n",
		"vim9script\necho 1 == 1.0\n",
		"vim9script\necho '' == v:none\n",
		"vim9script\necho true == false\n",
		"vim9script\necho [] == []\n",
		"vim9script\necho v:null == true\n",
		"vim9script\necho (v:none) == ''\n",
	} {
		result := Analyze(syntax.Parse(source))
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1072" {
				t.Fatalf("source %q unexpectedly received E1072: %#v", source, result.Diagnostics)
			}
		}
	}
}

func TestAnalyzeE1060ImportAliasRequiresDot(t *testing.T) {
	for _, test := range []struct {
		name, source, message, span string
	}{
		{
			name:    "script bare alias",
			source:  "vim9script\nimport './Xfoo.vim' as foo\nvar that = foo\n",
			message: "Expected dot after name: foo", span: "foo",
		},
		{
			name: "compiled binary expression",
			source: "vim9script\nimport './Xfoo.vim' as Export\ndef Func()\n  var dummy = 1\n" +
				"  var imported = Export + dummy\nenddef\ndefcompile\n",
			message: "Expected dot after name: Export + dummy", span: "Export",
		},
		{
			name:    "following bare token",
			source:  "vim9script\nimport './Xfoo.vim' as Export\ng:imported = Export exported\n",
			message: "Expected dot after name: Export exported", span: "Export",
		},
		{
			name:    "compound right hand side",
			source:  "vim9script\nimport './Xfoo.vim' as foo\nvar that: any\nthat += foo\n",
			message: "Expected dot after name: foo", span: "foo",
		},
		{
			name:    "compound target",
			source:  "vim9script\nimport './Xfoo.vim' as foo\nfoo += 9\n",
			message: "Expected dot after name: foo", span: "foo",
		},
		{
			name:    "line break before dot",
			source:  "vim9script\nimport './Xfoo.vim' as expo\ng:value = expo\n  .exported\n",
			message: "Expected dot after name: expo", span: "expo",
		},
		{
			name:    "space before dot",
			source:  "vim9script\nimport './Xfoo.vim' as foo\nvar that = foo .exported\n",
			message: "Expected dot after name: foo .exported", span: "foo",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1060" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1060 diagnostics = %#v, want %q on %q; all diagnostics = %#v", got, test.message, test.span, result.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nimport './Xfoo.vim' as foo\nvar that = foo.exported\n",
		"vim9script\nimport './Xfoo.vim' as foo\nvar that = foo. exported\n",
		"vim9script\nimport './Xfoo.vim' as foo\nfoo = 'value'\n",
		"vim9script\nvar foo = 1\nvar that = foo + 1\n",
		"import './Xfoo.vim' as foo\nlet g:that = foo\n",
	} {
		result := Analyze(syntax.Parse(source))
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1060" {
				t.Fatalf("source %q unexpectedly received E1060: %#v", source, result.Diagnostics)
			}
		}
	}
}

func TestAnalyzeE1236ImportNamespaceDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, span string }{
		{"official assignment", "vim9script\nimport './Xfoo.vim' as foo\nfoo = 'bar'\n", "foo"},
		{"official call", "vim9script\nimport './Xfoo.vim' as That\nThat()\n", "That"},
		{"call inside def", "vim9script\nimport './Xfoo.vim' as That\ndef Func()\n  That()\nenddef\n", "That"},
		{"nested def collision", "vim9script\nimport './Xfoo.vim' as That\ndef Outer()\n  def That()\n  enddef\nenddef\n", "That"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1236" {
					got = append(got, diagnostic)
				}
				if (diagnostic.Code == "vim/E1060" || diagnostic.Code == "vim/E1213") && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1236 source retained %s: %#v", diagnostic.Code, result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "Cannot use "+test.span+" itself, it is imported" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1236 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct{ name, source, want string }{
		{"member read", "vim9script\nimport './Xfoo.vim' as Item\nvar value = Item.Member\n", ""},
		{"member call", "vim9script\nimport './Xfoo.vim' as Item\nItem.Member()\n", ""},
		{"member assignment", "vim9script\nimport './Xfoo.vim' as Item\nItem.Member = 1\n", ""},
		{"bare read keeps E1060", "vim9script\nimport './Xfoo.vim' as Item\nvar value = Item\n", "vim/E1060"},
		{"compound assignment keeps E1060", "vim9script\nimport './Xfoo.vim' as Item\nItem += 1\n", "vim/E1060"},
		{"top-level declaration keeps E1213", "vim9script\nimport './Xfoo.vim' as Item\ndef Item()\nenddef\n", "vim/E1213"},
		{"unrelated local", "vim9script\nimport './Xfoo.vim' as Item\nvar Other = 1\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1236" {
					t.Fatalf("guard unexpectedly received E1236: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1258CompiledImportNamespaceAssignmentDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source, tail string
		following          bool
	}{
		{"official def", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  expo = 9\nenddef\n", "expo = 9", false},
		{"compound def", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  expo += 9\nenddef\n", "expo += 9", false},
		{"block lambda", "vim9script\nimport './Xfoo.vim' as expo\nvar Callback = () => {\n  expo ..= 'x'\n}\n", "expo ..= 'x'", false},
		{"incomplete rhs recovery", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  expo =\n  var after = 1\nenddef\n", "expo =", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1258" {
					got = append(got, diagnostic)
				}
				if (diagnostic.Code == "vim/E1236" || diagnostic.Code == "vim/E1060") && file.Text(diagnostic.Span) == "expo" {
					t.Fatalf("E1258 source retained %s: %#v", diagnostic.Code, result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "No '.' after imported name: "+test.tail || file.Text(got[0].Span) != "expo" {
				t.Fatalf("E1258 diagnostics = %#v", got)
			}
			if test.following {
				found := false
				for _, declaration := range result.Declarations {
					found = found || declaration.Name == "after"
				}
				if !found {
					t.Fatalf("following declaration was not retained: %#v", result.Declarations)
				}
			}
		})
	}

	for _, test := range []struct{ name, source, want string }{
		{"top-level direct retains E1236", "vim9script\nimport './Xfoo.vim' as expo\nexpo = 9\n", "vim/E1236"},
		{"top-level compound retains E1060", "vim9script\nimport './Xfoo.vim' as expo\nexpo += 9\n", "vim/E1060"},
		{"compiled bare read retains E1060", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  var value = expo\nenddef\n", "vim/E1060"},
		{"compiled call retains E1236", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  expo()\nenddef\n", "vim/E1236"},
		{"member assignment", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  expo.Member = 9\nenddef\n", ""},
		{"spaced member assignment", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  expo . Member = 9\nenddef\n", ""},
		{"Legacy", "import './Xfoo.vim' as expo\nlet expo = 9\n", ""},
		{"legacy command in def", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  legacy let expo = 9\nenddef\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1258" {
					t.Fatalf("guard unexpectedly received E1258: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1259CompiledImportNamespaceMemberDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source, tail string
		following          bool
	}{
		{"official def", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  expo.99 = 9\nenddef\n", "expo.99 = 9", false},
		{"compound def", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  expo.99 += 9\nenddef\n", "expo.99 += 9", false},
		{"block lambda", "vim9script\nimport './Xfoo.vim' as expo\nvar Callback = () => {\n  expo.99 = 9\n}\n", "expo.99 = 9", false},
		{"incomplete rhs recovery", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  expo.99 =\n  var after = 1\nenddef\n", "expo.99 =", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1259" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Missing name after imported name: "+test.tail || file.Text(got[0].Span) != "expo" {
				t.Fatalf("E1259 diagnostics = %#v", got)
			}
			for _, diagnostic := range CombinedDiagnostics(file, result) {
				if diagnostic.Code == "vimls/missing-member" {
					t.Fatalf("E1259 source retained provisional missing-member: %#v", diagnostic)
				}
			}
			if test.following {
				found := false
				for _, declaration := range result.Declarations {
					found = found || declaration.Name == "after"
				}
				if !found {
					t.Fatalf("following declaration was not retained: %#v", result.Declarations)
				}
			}
		})
	}

	for _, test := range []struct{ name, source string }{
		{"valid member", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  expo.Member = 9\nenddef\n"},
		{"missing dot owns E1258", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  expo = 9\nenddef\n"},
		{"numeric member read", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  var value = expo.99\nenddef\n"},
		{"numeric member comparison", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  var value = expo.99 == 9\nenddef\n"},
		{"numeric member call", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  expo.99()\nenddef\n"},
		{"top-level assignment", "vim9script\nimport './Xfoo.vim' as expo\nexpo.99 = 9\n"},
		{"spaced member keeps E1074", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  expo. Member = 9\nenddef\n"},
		{"Legacy", "import './Xfoo.vim' as expo\nlet expo.99 = 9\n"},
		{"legacy command in def", "vim9script\nimport './Xfoo.vim' as expo\ndef Func()\n  legacy let expo.99 = 9\nenddef\n"},
		{"non-import receiver", "vim9script\ndef Func()\n  var expo = {}\n  expo.99 = 9\nenddef\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, diagnostic := range Analyze(syntax.Parse(test.source)).Diagnostics {
				if diagnostic.Code == "vim/E1259" {
					t.Fatalf("guard unexpectedly received E1259: %#v", diagnostic)
				}
			}
		})
	}
}

func TestAnalyzeE1074ImportWhitespaceAfterDot(t *testing.T) {
	for _, test := range []struct {
		name, source string
	}{
		{
			name:   "script",
			source: "vim9script\nimport './Xfoo.vim' as Export\ng:value = Export. exported\n",
		},
		{
			name:   "line break",
			source: "vim9script\nimport './Xfoo.vim' as expo\ng:value = expo.\n  exported\n",
		},
		{
			name: "compiled def",
			source: "vim9script\nimport './Xfoo.vim' as Export\ndef Func()\n" +
				"  var imported = Export . exported\nenddef\ndefcompile\n",
		},
		{
			name:   "vim9cmd",
			source: "vim9cmd import './Xfoo.vim' as Export\nvim9cmd echo Export. exported\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			diagnostics := CombinedDiagnostics(file, Analyze(file))
			var got []syntax.Diagnostic
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == "vim/E1074" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1202" || diagnostic.Code == "vimls/missing-member" {
					t.Fatalf("provisional member diagnostic was not suppressed: %#v", diagnostics)
				}
			}
			if len(diagnostics) != 1 || len(got) != 1 || got[0].Message != "No white space allowed after dot" || strings.Trim(file.Text(got[0].Span), " \t\r\n") != "" {
				t.Fatalf("E1074 diagnostics = %#v; all diagnostics = %#v", got, diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nimport './Xfoo.vim' as Export\ng:value = Export . exported\n",
		"vim9script\nimport './Xfoo.vim' as Export\ng:value = Export.member\n",
		"vim9script\nimport './Xfoo.vim' as Export\ndef Func()\n  var value = Export .member\nenddef\n",
		"vim9script\nimport './Xfoo.vim' as Export\ndef Func()\n  var value = Export.\nenddef\n",
		"vim9script\nvar value = {key: 1}\necho value. key\n",
		"import './Xfoo.vim' as Export\necho Export. exported\n",
	} {
		file := syntax.Parse(source)
		for _, diagnostic := range CombinedDiagnostics(file, Analyze(file)) {
			if diagnostic.Code == "vim/E1074" {
				t.Fatalf("source %q unexpectedly received E1074", source)
			}
		}
	}

	file := syntax.Parse("vim9script\nimport './Xfoo.vim' as Export\ng:value = Export . exported\n")
	diagnostics := CombinedDiagnostics(file, Analyze(file))
	if len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1060" {
		t.Fatalf("script space-before-dot diagnostics = %#v, want only E1060", diagnostics)
	}
}

func TestAnalyzeE1062CannotIndexNumber(t *testing.T) {
	for _, test := range []struct {
		name, source, span string
	}{
		{name: "decimal literal", source: "vim9script\nvar x = 1234[3]\n", span: "1234"},
		{name: "hex literal", source: "vim9script\nvar x = 0xff[1]\n", span: "0xff"},
		{name: "declared number", source: "vim9script\nvar number: number = 1234\nvar x = number[3]\n", span: "number"},
		{name: "slice", source: "vim9script\nvar x = 1234[1 : 2]\n", span: "1234"},
		{name: "vim9cmd", source: "vim9cmd echo 1234[3]\n", span: "1234"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1062" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Cannot index a Number" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1062 diagnostics = %#v, want one on %q; all diagnostics = %#v", got, test.span, result.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"let g:x = 1234[3]\n",
		"vim9script\ndef F()\n  var x = 1234[3]\nenddef\n",
		"vim9script\nvar x = 0.7[1]\n",
		"vim9script\nvar x = '1234'[3]\n",
		"vim9script\nvar value: any\nvar x = value[3]\n",
	} {
		result := Analyze(syntax.Parse(source))
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1062" {
				t.Fatalf("source %q unexpectedly received E1062: %#v", source, result.Diagnostics)
			}
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

func TestAnalyzeE1023NumberAsBoolDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source, span, message string
	}{
		{
			name: "script logical left", source: "vim9script\nvar value = 3 || false\n",
			span: "3", message: "Using a Number as a Bool: 3",
		},
		{
			name: "script logical right", source: "vim9script\nvar value = false || 3\n",
			span: "3", message: "Using a Number as a Bool: 3",
		},
		{
			name: "script numeric false evaluates right", source: "vim9script\nvar value = 0 || 3\n",
			span: "3", message: "Using a Number as a Bool: 3",
		},
		{
			name: "script numeric true evaluates right", source: "vim9script\nvar value = 1 && 3\n",
			span: "3", message: "Using a Number as a Bool: 3",
		},
		{
			name: "script condition", source: "vim9script\nif 3\nendif\n",
			span: "3", message: "Using a Number as a Bool: 3",
		},
		{
			name: "compiled condition", source: "vim9script\ndef F()\n  if 3\n  endif\nenddef\n",
			span: "3", message: "Using a Number as a Bool: 3",
		},
		{
			name: "script logical continuation", source: "vim9script\nif true\n    && 3\nendif\n",
			span: "3", message: "Using a Number as a Bool: 3",
		},
		{
			name: "hex condition", source: "vim9script\nif 0x02\nendif\n",
			span: "0x02", message: "Using a Number as a Bool: 2",
		},
		{
			name: "negative condition", source: "vim9script\nif -2\nendif\n",
			span: "-2", message: "Using a Number as a Bool: -2",
		},
		{
			name: "ternary condition", source: "vim9script\nvar value = 3 ? 'yes' : 'no'\n",
			span: "3", message: "Using a Number as a Bool: 3",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1023" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1023 diagnostics = %#v, want one on %q; syntax diagnostics = %#v", got, test.span, file.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"if 3\nendif\n",
		"vim9script\nif 0\nendif\n",
		"vim9script\nif 1\nendif\n",
		"vim9script\nvar value = true || 3\n",
		"vim9script\nvar value = false && 3\n",
		"vim9script\nvar value = 1 || 3\n",
		"vim9script\nvar value = 0 && 3\n",
		"vim9script\nvar condition = 3\nif condition\nendif\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1023" {
				t.Fatalf("source %q unexpectedly received E1023: %#v", source, diagnostic)
			}
		}
	}

	compiled := Analyze(syntax.Parse("vim9script\ndef F()\n  var value = 3 || false\nenddef\n"))
	foundE1012 := false
	for _, diagnostic := range compiled.Diagnostics {
		if diagnostic.Code == "vim/E1012" {
			foundE1012 = true
		}
		if diagnostic.Code == "vim/E1023" {
			t.Fatalf("compiled logical expression retained E1023: %#v", compiled.Diagnostics)
		}
	}
	if !foundE1012 {
		t.Fatalf("compiled logical diagnostics = %#v, want E1012", compiled.Diagnostics)
	}
}

func TestAnalyzeStringAsBoolDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		want         []struct{ message, text string }
	}{
		{
			name:   "Vim9 script ternary and conditions",
			source: "vim9script\nvar ternary = ('ternary') ? 1 : 0\nif 'if'\nelseif ('elseif')\nendif\nwhile 'while'\n  break\nendwhile\n",
			want: []struct{ message, text string }{
				{`Using a String as a Bool: "ternary"`, "('ternary')"},
				{`Using a String as a Bool: "if"`, "'if'"},
				{`Using a String as a Bool: "elseif"`, "('elseif')"},
				{`Using a String as a Bool: "while"`, "'while'"},
			},
		},
		{
			name:   "compiled def literals except while",
			source: "vim9script\ndef Func()\n  var ternary = ('ternary') ? 1 : 0\n  if 'if'\n  elseif ('elseif')\n  endif\n  while 'while'\n    break\n  endwhile\nenddef\n",
			want: []struct{ message, text string }{
				{`Using a String as a Bool: "ternary"`, "('ternary')"},
				{`Using a String as a Bool: "if"`, "'if'"},
				{`Using a String as a Bool: "elseif"`, "('elseif')"},
			},
		},
		{
			name:   "Legacy-root def and Vim9 lambda literals",
			source: "def Func()\n  if 'legacy def'\n  elseif ('legacy elseif')\n  endif\nenddef\nvim9cmd var Callback = () => {\n  if 'lambda'\n  endif\n}\n",
			want: []struct{ message, text string }{
				{`Using a String as a Bool: "legacy def"`, "'legacy def'"},
				{`Using a String as a Bool: "legacy elseif"`, "('legacy elseif')"},
				{`Using a String as a Bool: "lambda"`, "'lambda'"},
			},
		},
		{
			name:   "Vim9 script logical evaluated operands",
			source: "vim9script\nvar typed: string = 'typed'\nvar left = 'left' && false\nvar right = true && typed\n",
			want: []struct{ message, text string }{
				{`Using a String as a Bool: "left"`, "'left'"},
				{"Using a String as a Bool", "typed"},
			},
		},
		{
			name:   "filter and indexof string callback returns",
			source: "vim9script\ndef Predicate(index: number, value: number): string\n  return 'yes'\nenddef\nfilter([1], Predicate)\nindexof([1], Predicate)\n",
			want: []struct{ message, text string }{
				{"Using a String as a Bool", "Predicate"},
				{"Using a String as a Bool", "Predicate"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1135" {
					got = append(got, diagnostic)
				}
				if (test.name == "filter and indexof string callback returns") && diagnostic.Code == "vim/E1013" {
					t.Fatalf("callback E1135 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != len(test.want) {
				t.Fatalf("E1135 diagnostics = %#v, want %#v; all diagnostics = %#v", got, test.want, result.Diagnostics)
			}
			for index, diagnostic := range got {
				if diagnostic.Message != test.want[index].message || file.Text(diagnostic.Span) != test.want[index].text {
					t.Fatalf("E1135[%d] = %#v on %q, want %q on %q", index, diagnostic, file.Text(diagnostic.Span), test.want[index].message, test.want[index].text)
				}
			}
		})
	}

	for _, test := range []struct {
		name, source string
		wantCode     string
	}{
		{"compiled nonliteral if retains E1012", "vim9script\ndef Func()\n  var text: string = 'value'\n  if text\n  endif\nenddef\n", "vim/E1012"},
		{"compiled nonliteral ternary is not remapped", "vim9script\ndef Func()\n  var text: string = 'value'\n  var ternary = text ? 1 : 0\nenddef\n", ""},
		{"compiled nonliteral logical retains E1012", "vim9script\ndef Func()\n  var text: string = 'value'\n  var value = text && true\nenddef\n", "vim/E1012"},
		{"compiled while literal retains E1012", "vim9script\ndef Func()\n  while 'text'\n    break\n  endwhile\nenddef\n", "vim/E1012"},
		{"short-circuited right strings", "vim9script\nvar one = false && 'no'\nvar two = true || 'no'\n", ""},
		{"invalid left keeps precedence", "vim9script\nvar value = 3 && 'right'\n", "vim/E1023"},
		{"Legacy script and function", "if 'legacy'\nendif\nfunction Legacy()\n  if 'function'\n  endif\nendfunction\n", ""},
		{"permissive not incomplete bool numbers any and unknown", "vim9script\nvar anything: any\nvar value = !'text'\nvar boolValue = true ? 1 : 0\nvar zero = 0 ? 1 : 0\nvar one = 1 ? 1 : 0\nvar anyValue = anything ? 1 : 0\nvar unknownValue = Unknown ? 1 : 0\nvar missing = 'text' ?\n", ""},
		{"compiled filter string callback remains E1013", "vim9script\ndef Predicate(index: number, value: number): string\n  return 'yes'\nenddef\ndef Func()\n  filter([1], Predicate)\nenddef\n", "vim/E1013"},
		{"map and foreach string callbacks are not E1135", "vim9script\ndef Predicate(index: number, value: number): string\n  return 'yes'\nenddef\nmap([1], Predicate)\nforeach([1], Predicate)\n", ""},
		{"indexof wrong callback count keeps E118", "vim9script\ndef Predicate(value: number): string\n  return 'yes'\nenddef\nindexof([1], Predicate)\n", "vim/E118"},
		{"filter wrong callback types keeps E1013", "vim9script\ndef Predicate(index: string, value: number): string\n  return 'yes'\nenddef\nfilter([1], Predicate)\n", "vim/E1013"},
		{"map callback count keeps E1106", "vim9script\nmap([1], () => 'yes')\n", "vim/E1106"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			foundCode := test.wantCode == ""
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1135" {
					t.Fatalf("source unexpectedly received E1135: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.wantCode {
					foundCode = true
				}
			}
			if !foundCode {
				t.Fatalf("source diagnostics = %#v, want %s", result.Diagnostics, test.wantCode)
			}
		})
	}
}

func TestAnalyzeBoolAsNumberDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		want         []string
	}{
		{
			name: "Vim9 script bool spellings operands and parentheses",
			source: "vim9script\nvar one = 1 + v:true\nvar two = 1 + v:false\nvar three = 1 + true\n" +
				"var four = 1 + false\nvar left = (true) + 1\nvar value: bool = true\nvar identifier = 1 + value\n",
			want: []string{"v:true", "v:false", "true", "false", "(true)", "value"},
		},
		{
			name:   "Vim9 script compound assignment",
			source: "vim9script\nvar value = 1\nvalue += true\nvar flag: bool = true\nflag += 1\n",
			want:   []string{"true", "flag"},
		},
		{
			name:   "Vim9 script sort direct and named callbacks",
			source: "vim9script\ndef Compare(first: number, second: number): bool\n  return true\nenddef\nsort([2, 1], (first, second) => true)\nsort([2, 1], Compare)\n",
			want:   []string{"(first, second) => true", "Compare"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1138" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && test.name == "Vim9 script sort direct and named callbacks" {
					t.Fatalf("script sort E1138 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != len(test.want) {
				t.Fatalf("E1138 diagnostics = %#v, want spans %#v; all diagnostics = %#v", got, test.want, result.Diagnostics)
			}
			for index, diagnostic := range got {
				if diagnostic.Message != "Using a Bool as a Number" || file.Text(diagnostic.Span) != test.want[index] {
					t.Fatalf("E1138[%d] = %#v on %q, want Bool-as-Number on %q", index, diagnostic, file.Text(diagnostic.Span), test.want[index])
				}
			}
		})
	}

	for _, test := range []struct {
		name, source string
		wantCode     string
	}{
		{"compiled def arithmetic retains E1051", "vim9script\ndef Func()\n  var value = true + 1\nenddef\n", "vim/E1051"},
		{"compiled lambda arithmetic retains E1051", "vim9script\nvar Callback = () => true + 1\n", "vim/E1051"},
		{"compiled sort bool callback retains E1013", "vim9script\ndef Func()\n  sort([2, 1], (first, second) => true)\nenddef\n", "vim/E1013"},
		{"lambda sort bool callback retains E1013", "vim9script\nvar Callback = () => sort([2, 1], (first, second) => true)\n", "vim/E1013"},
		{"Legacy arithmetic", "let value = v:true + 1\n", ""},
		{"wrong sort signatures retain E1013", "vim9script\nsort([2, 1], (value) => true)\nsort([2, 1], (first: string, second: number) => true)\n", "vim/E1013"},
		{"number sort callback is accepted", "vim9script\nsort([2, 1], (first, second) => first - second)\n", ""},
		{"map filter and indexof Bool callbacks", "vim9script\nmap([1], (index, value) => true)\nfilter([1], (index, value) => true)\nindexof([1], (index, value) => true)\n", ""},
		{"Bool conditions logical assignment concatenation any and incomplete", "vim9script\nvar value: bool = true\nif value\nendif\nvar logical = value && false\nvar ternary = value ? 1 : 0\nvar copy: bool = value\nvar text = value .. ''\nvar anything: any\nvar unknown = anything + 1\nvar unknownRight = anything + true\nvar tupleRight = (1, 2) + true\nvar missing = 1 +\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			foundCode := test.wantCode == ""
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1138" {
					t.Fatalf("source unexpectedly received E1138: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.wantCode {
					foundCode = true
				}
			}
			if !foundCode {
				t.Fatalf("source diagnostics = %#v, want %s", result.Diagnostics, test.wantCode)
			}
		})
	}
}

func TestAnalyzeBitshiftOperandDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         []string
	}{
		{"Legacy left operand", "let value = 'text' << 1\n", []string{"<<"}},
		{"Legacy right operand", "let value = 1 >> []\n", []string{">>"}},
		{"Vim9 script left and right", "vim9script\nvar left = 'text' << 1\nvar right = 1 >> []\n", []string{"<<", ">>"}},
		{"left operand wins", "vim9script\nvar value = 'text' << []\n", []string{"<<"}},
		{"compiled direct constants", "vim9script\ndef Func()\n  var left = ('text') << 1\n  var right = 1 >> (0.5)\nenddef\n", []string{"<<", ">>"}},
		{"compiled Blob and literal identifiers", "vim9script\ndef Func()\n  var blob = (0z12) << 1\n  var bool = true >> 1\n  var special = v:none << 1\n  var nullValue = null >> 1\n  var nullList = null_list << 1\nenddef\n", []string{"<<", ">>", "<<", ">>", "<<"}},
		{"block lambda constant", "vim9script\nvar Callback = () => ('text') << 1\n", []string{"<<"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1282" {
					got = append(got, diagnostic)
				}
				if (test.name == "compiled direct constants" || test.name == "compiled Blob and literal identifiers") && diagnostic.Code == "vim/E1012" {
					t.Fatalf("compiled constant retained E1012: %#v", result.Diagnostics)
				}
			}
			if len(got) != len(test.want) {
				t.Fatalf("E1282 diagnostics = %#v, want %#v", result.Diagnostics, test.want)
			}
			for index, diagnostic := range got {
				if diagnostic.Message != "Bitshift operands must be numbers" || file.Text(diagnostic.Span) != test.want[index] {
					t.Fatalf("E1282 diagnostic = %#v on %q", diagnostic, file.Text(diagnostic.Span))
				}
			}
		})
	}

	for _, test := range []struct {
		name, source, span, message string
	}{
		{"compiled string variable", "vim9script\ndef Func()\n  var text: string = 'text'\n  var value = text << 1\nenddef\n", "text", "Type mismatch; expected number but got string"},
		{"compiled not-list", "vim9script\ndef Func()\n  var value = ![] >> 1\nenddef\n", "![]", "Type mismatch; expected number but got bool"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1282" {
					t.Fatalf("compiled variable unexpectedly received E1282: %#v", result.Diagnostics)
				}
				if diagnostic.Code == "vim/E1012" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1012 diagnostics = %#v", result.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nvar anyValue: any\nvar value = anyValue << 1\nvar unknown = Unknown >> 1\n",
		"vim9script\nvar value = 1 << 2\nvar other = 8 >> 1\n",
		"vim9script\ndef Func()\n  var value = 1 << 2\nenddef\n",
	} {
		file := syntax.Parse(source)
		result := Analyze(file)
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1282" {
				t.Fatalf("source unexpectedly received E1282: %#v", result.Diagnostics)
			}
		}
	}
}

func TestAnalyzeNegativeBitshiftAmountDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source, span string
	}{
		{"Legacy literal", "let value = 2 << -1\n", "-1"},
		{"Vim9 script literal", "vim9script\nvar value = 2 >> (-1)\n", "(-1)"},
		{"compiled def literal", "vim9script\ndef Func()\n  var value = 2 << -1\nenddef\n", "-1"},
		{"compiled block lambda literal", "vim9script\nvar Callback = () => 2 >> -1\n", "-1"},
		{"Legacy initializer", "let a = 2\nlet b = -1\nlet value = a << b\n", "b"},
		{"Vim9 script initializer", "vim9script\nvar a = 2\nvar b = -1\nvar value = a << b\n", "b"},
		{"compiled def initializer", "vim9script\ndef Func()\n  var a = 2\n  var b = -1\n  var value = a << b\nenddef\n", "b"},
		{"parenthesized initializer", "vim9script\nvar a = 2\nvar b = -1\nvar value = a << (b)\n", "(b)"},
		{"shadowed initializer", "vim9script\ndef Func()\n  var amount = 1\n  if true\n    var amount = -1\n    var value = 2 << amount\n  endif\nenddef\n", "amount"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1283" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Bitshift amount must be a positive number" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1283 diagnostics = %#v", result.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nvar value = 2 << 1\n",
		"vim9script\ndef Func()\n  var amount = -1\n  amount = 1\n  var value = 2 << amount\nenddef\n",
		"vim9script\ndef Func()\n  var amount = -1\n  if true\n    var value = 2 << amount\n  endif\nenddef\n",
		"vim9script\nvar amount: any\nvar value = 2 << amount\n",
		"vim9script\ndef Func()\n  var value = 'text' << -1\nenddef\n",
		"vim9script\nvar value = 2 << -1 << -2\n",
	} {
		file := syntax.Parse(source)
		result := Analyze(file)
		var got []syntax.Diagnostic
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1283" {
				got = append(got, diagnostic)
			}
		}
		if strings.Contains(source, "2 << -1 << -2") {
			if len(got) != 1 || file.Text(got[0].Span) != "-1" {
				t.Fatalf("left-associative chain diagnostics = %#v", result.Diagnostics)
			}
			continue
		}
		if len(got) != 0 {
			t.Fatalf("source unexpectedly received E1283: %#v", result.Diagnostics)
		}
	}
}

func TestAnalyzeIndexableAssignmentDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		want         []string
	}{
		{
			name: "compiled direct compound and slice assignments",
			source: "vim9script\ndef Func()\n  var lines: string = 'text'\n  lines[9] = 'asdf'\n  var n: number = 1\n  n.key = 0\n" +
				"  var s = 'text'\n  s[1] += 'x'\n  s[2] ..= 'x'\n  lines[1 : 2] = 'x'\n  var after = 1\nenddef\n",
			want: []string{"lines", "n", "s", "s", "lines"},
		},
		{
			name:   "compiled redir and append redir targets",
			source: "vim9script\ndef Func()\n  var ls = 'text'\n  redir => ls[1]\n  redir END\n  redir =>> ls[2]\n  redir END\n  var after = 1\nenddef\n",
			want:   []string{"ls", "ls"},
		},
		{
			name:   "Legacy-root def and Vim9 lambda",
			source: "def LegacyDef()\n  var value: number = 1\n  value[0] = 1\nenddef\nvim9cmd var Callback = () => {\n  var text = 'x'\n  text[0] = 'y'\n}\n",
			want:   []string{"value", "text"},
		},
		{
			name:   "nested target reports first known invalid receiver once",
			source: "vim9script\ndef Func()\n  var value: number = 1\n  value.member[0] = 1\nenddef\n",
			want:   []string{"value"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1141" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != len(test.want) {
				t.Fatalf("E1141 diagnostics = %#v, want spans %#v; syntax diagnostics = %#v; all diagnostics = %#v", got, test.want, file.Diagnostics, result.Diagnostics)
			}
			for index, diagnostic := range got {
				if diagnostic.Message != "Indexable type required" || file.Text(diagnostic.Span) != test.want[index] {
					t.Fatalf("E1141[%d] = %#v on %q, want receiver %q", index, diagnostic, file.Text(diagnostic.Span), test.want[index])
				}
			}
		})
	}

	for _, test := range []struct {
		name, source string
		wantCode     string
	}{
		{"Vim9 script assignment guards", "vim9script\nvar text = 'x'\ntext[0] = 'y'\nvar n = 1\nn.key = 1\n", ""},
		{"Legacy assignment guard", "let text = 'x'\nlet text[0] = 'y'\n", ""},
		{"valid list dict blob object and enum receivers", "vim9script\nclass Item\n  var value = 0\nendclass\nenum Choice\n  First\nendenum\ndef Func()\n  var list = [1]\n  list[0] = 2\n  var dict = {key: 1}\n  dict.key = 2\n  var blob = 0z12\n  blob[0] = 3\n  var object = Item.new()\n  object.value = 1\n  var choice: Choice = Choice.First\n  choice.value = 1\nenddef\n", ""},
		{"tuple keeps existing immutability", "vim9script\ndef Func()\n  var tuple = (1, 2)\n  tuple[0] = 3\n  tuple[0 : 1] = 3\nenddef\n", "vim/E1532"},
		{"Class and Typealias declarations are not receivers", "vim9script\nclass Item\nendclass\ntype Number = number\ndef Func()\n  Item.value = 1\n  Number.value = 1\nenddef\n", ""},
		{"unknown receiver stays conservative", "vim9script\ndef Func()\n  unknown[0] = 1\nenddef\n", ""},
		{"read does not diagnose", "vim9script\ndef Func()\n  var text = 'x'\n  var character = text[0]\nenddef\n", ""},
		{"incomplete target does not cascade", "vim9script\ndef Func()\n  var text = 'x'\n  text[\nenddef\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			foundCode := test.wantCode == ""
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1141" {
					t.Fatalf("source unexpectedly received E1141: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.wantCode {
					foundCode = true
				}
			}
			if !foundCode {
				t.Fatalf("source diagnostics = %#v, want %s", result.Diagnostics, test.wantCode)
			}
		})
	}
}

func TestAnalyzeObjectComparisonDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		want         []string
	}{
		{
			name: "Vim9 script all object operations",
			source: "vim9script\nclass Item\nendclass\nvar left = Item.new()\nvar right = Item.new()\n" +
				"var one = left > right\nvar two = left >= right\nvar three = left < right\nvar four = left <= right\nvar five = left =~ right\nvar six = left !~ right\n",
			want: []string{">", ">=", "<", "<=", "=~", "!~"},
		},
		{
			name: "compiled def all object operations",
			source: "vim9script\nclass Item\nendclass\ndef Func()\n  var left = Item.new()\n  var right = Item.new()\n" +
				"  var one = left > right\n  var two = left >= right\n  var three = left < right\n  var four = left <= right\n  var five = left =~ right\n  var six = left !~ right\nenddef\n",
			want: []string{">", ">=", "<", "<=", "=~", "!~"},
		},
		{
			name:   "Legacy-root def",
			source: "class LegacyItem\nendclass\ndef LegacyDef()\n  var left = LegacyItem.new()\n  var right = LegacyItem.new()\n  var value = left > right\nenddef\n",
			want:   []string{">"},
		},
		{
			name:   "Vim9 lambda",
			source: "vim9script\nclass LambdaItem\nendclass\nvar Callback = () => {\n  var left = LambdaItem.new()\n  var right = LambdaItem.new()\n  var value = left =~ right\n}\n",
			want:   []string{"=~"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1153" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != len(test.want) {
				t.Fatalf("E1153 diagnostics = %#v, want operators %#v; all diagnostics = %#v", got, test.want, result.Diagnostics)
			}
			for index, diagnostic := range got {
				if diagnostic.Message != "Invalid operation for object" || file.Text(diagnostic.Span) != test.want[index] {
					t.Fatalf("E1153[%d] = %#v on %q, want operator %q", index, diagnostic, file.Text(diagnostic.Span), test.want[index])
				}
			}
		})
	}

	for _, test := range []struct{ name, source string }{
		{"valid object comparisons", "vim9script\nclass Item\nendclass\ndef Func()\n  var left = Item.new()\n  var right = Item.new()\n  var one = left == right\n  var two = left != right\n  var three = left is right\n  var four = left isnot right\nenddef\n"},
		{"mixed object and number", "vim9script\nclass Item\nendclass\ndef Func()\n  var object = Item.new()\n  var value = object > 1\nenddef\n"},
		{"ordered Bool keeps E1072", "vim9script\nvar value = true > false\n"},
		{"direct class enum and typealias declarations", "vim9script\nclass Item\nendclass\nenum Choice\n  First\nendenum\ntype Number = number\nvar one = Item > Item\nvar two = Choice > Choice\nvar three = Number > Number\n"},
		{"unknown and incomplete", "vim9script\nvar one = Unknown > Other\nvar two = 1 >\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, diagnostic := range Analyze(syntax.Parse(test.source)).Diagnostics {
				if diagnostic.Code == "vim/E1153" {
					t.Fatalf("source unexpectedly received E1153: %#v", diagnostic)
				}
			}
		})
	}
}

func TestAnalyzeFlattenVim9Diagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		want         []string
	}{
		{
			name:   "Vim9 script direct method and arity precedence",
			source: "vim9script\nflatten([1])\n[[1]]->flatten()\nflatten()\nflatten(1, 2, 3)\n",
			want:   []string{"flatten", "flatten", "flatten", "flatten"},
		},
		{
			name:   "compiled def lambda vim9cmd and Legacy-root def",
			source: "vim9script\ndef Func()\n  flatten([1])\nenddef\nvar Callback = () => flatten([1])\nvim9cmd flatten([1])\n",
			want:   []string{"flatten", "flatten", "flatten"},
		},
		{
			name:   "Legacy-root def",
			source: "def Func()\n  flatten([1])\nenddef\n",
			want:   []string{"flatten"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1158" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E118" || diagnostic.Code == "vim/E119" || diagnostic.Code == "vim/E1013" {
					if test.name == "Vim9 script direct method and arity precedence" {
						t.Fatalf("E1158 source retained %s: %#v", diagnostic.Code, result.Diagnostics)
					}
				}
			}
			if len(got) != len(test.want) {
				t.Fatalf("E1158 diagnostics = %#v, want spans %#v; all diagnostics = %#v", got, test.want, result.Diagnostics)
			}
			for index, diagnostic := range got {
				if diagnostic.Message != "Cannot use flatten() in Vim9 script, use flattennew()" || file.Text(diagnostic.Span) != test.want[index] {
					t.Fatalf("E1158[%d] = %#v on %q", index, diagnostic, file.Text(diagnostic.Span))
				}
			}
		})
	}

	for _, source := range []string{
		"flatten([1])\nfunction Legacy()\n  flatten([1])\nendfunction\n",
		"vim9script\nlegacy call flatten([1])\nflattennew([1])\ng:flatten([1])\ns:flatten([1])\nobj.flatten([1])\ncall('flatten', [1])\n",
		"vim9script\nflatten(\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1158" {
				t.Fatalf("source %q unexpectedly received E1158: %#v", source, diagnostic)
			}
		}
	}
}

func TestAnalyzeDestructuringElementTypeDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, span, message string
	}{
		{"Vim9 script official assignment", "vim9script\nvar x: number\nvar y: number\nvar z: string\n[x, y, z] = [1, 2, 3]\n", "3", "Variable 3: type mismatch, expected string but got number"},
		{"compiled def official assignment", "vim9script\ndef Func()\n  var x: number\n  var y: number\n  var z: string\n  [x, y, z] = [1, 2, 3]\nenddef\n", "3", "Variable 3: type mismatch, expected string but got number"},
		{"typed declaration heterogeneous literal", "vim9script\nvar [x: number, y: string] = [1, 2]\n", "2", "Variable 2: type mismatch, expected string but got number"},
		{"first mismatch and underscore", "vim9script\nvar x: string\nvar y: string\n[x, _, y] = [1, 2, 3]\n", "1", "Variable 1: type mismatch, expected string but got number"},
		{"known list member", "vim9script\nvar values: list<number> = [1]\nvar target: string\n[target] = values\n", "values", "Variable 1: type mismatch, expected string but got number"},
		{"known tuple member", "vim9script\nvar values: tuple<number, number> = (1, 2)\nvar first: number\nvar second: string\n[first, second] = values\n", "values", "Variable 2: type mismatch, expected string but got number"},
		{"Legacy-root def", "def Func()\n  var value: string\n  [value] = [1]\nenddef\n", "1", "Variable 1: type mismatch, expected string but got number"},
		{"Vim9 block lambda", "vim9script\nvar Callback = () => {\n  var value: string\n  [value] = [1]\n}\n", "1", "Variable 1: type mismatch, expected string but got number"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1163" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1012" {
					t.Fatalf("E1163 source retained E1012: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1163 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"compatible and number to float", "vim9script\nvar value: float\n[value] = [1]\n", ""},
		{"any unknown incomplete and rest", "vim9script\nvar value: number\nvar rest: string\nvar anything: any\n[value] = anything\n[value; rest] = [1, 2]\n[value] = [\n", ""},
		{"Legacy script and function", "let value = ''\nlet [value] = [1]\nfunction Legacy()\n  let [value] = [1]\nendfunction\n", ""},
		{"typed literal cardinality", "vim9script\nvar [value: string, other: string] = [1]\n", "vim/E1093"},
		{"noncontainer", "vim9script\nvar value: string\n[value] = 1\n", "vim/E1535"},
		{"void", "vim9script\ndef Void()\nenddef\nvar value: string\n[value] = Void()\n", "vim/E1031"},
		{"known Tuple cardinality", "vim9script\nvar values: tuple<number> = (1,)\nvar first: string\nvar second: string\n[first, second] = values\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			found := test.want == ""
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1163" || diagnostic.Code == "vim/E1012" {
					t.Fatalf("source unexpectedly received %s: %#v", diagnostic.Code, result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					found = true
				}
			}
			if !found {
				t.Fatalf("diagnostics = %#v, want %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1165SliceAssignmentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, span, message string }{
		{"official dict def recovery", "vim9script\ndef Func()\n  var d = {x: 1}\n  d[1 : 2] = {y: 2}\n  var after = 1\nenddef\n", "d[1 : 2]", "Cannot use a range with an assignment: d[1 : 2] = {y: 2}"},
		{"number def", "vim9script\ndef Func()\n  var n = 1\n  n[1 : 2] = 3\nenddef\n", "n[1 : 2]", "Cannot use a range with an assignment: n[1 : 2] = 3"},
		{"Legacy-root def", "def Func()\n  var d = {}\n  d[1 : 2] = {}\nenddef\n", "d[1 : 2]", "Cannot use a range with an assignment: d[1 : 2] = {}"},
		{"lambda", "vim9script\nvar Callback = () => {\n  var d = {}\n  d[1 : 2] = {}\n}\n", "d[1 : 2]", "Cannot use a range with an assignment: d[1 : 2] = {}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1165" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1141" || diagnostic.Code == "vim/E1012" {
					t.Fatalf("E1165 source retained %s: %#v", diagnostic.Code, result.Diagnostics)
				}
			}
			if len(got) != 1 || file.Text(got[0].Span) != test.span || got[0].Message != test.message {
				t.Fatalf("E1165 diagnostics = %#v", got)
			}
			if test.name == "official dict def recovery" && file.Commands[len(file.Commands)-2].Declaration == nil {
				t.Fatalf("following declaration not retained: %#v", file.Commands)
			}
		})
	}
	for _, source := range []string{
		"vim9script\nvar d = {}\nd[1 : 2] = {}\n", "let d = {}\nlet d[1 : 2] = {}\n",
		"vim9script\ndef F()\n  var l = [1]\n  l[0 : 1] = [2]\n  var b = 0z12\n  b[0 : 1] = 0z34\n  var a: any\n  a[0 : 1] = 1\nenddef\n",
		"vim9script\ndef F()\n  var n = 1\n  n[0] = 1\n  unknown[0 : 1] = 1\n  n[\nenddef\n",
	} {
		result := Analyze(syntax.Parse(source))
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1165" {
				t.Fatalf("guard unexpectedly received E1165: %#v", result.Diagnostics)
			}
		}
	}
	for _, test := range []struct {
		name, source, want string
	}{
		{"tuple keeps E1533", "vim9script\ndef F()\n  var t = (1, 2)\n  t[0 : 1] = 1\nenddef\n", "vim/E1533"},
		{"compound slice keeps E1141", "vim9script\ndef F()\n  var n = 1\n  n[0 : 1] += 1\nenddef\n", "vim/E1141"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1165" {
					t.Fatalf("source retained E1165: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
					if test.want == "vim/E1533" && diagnostic.Message != "Cannot slice a tuple" {
						t.Fatalf("tuple diagnostic = %#v", diagnostic)
					}
				}
			}
			if count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1183CompoundSliceAssignmentDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source, span, message string
		recovery                    bool
	}{
		{"official blob def recovery", "vim9script\ndef Func()\n  var b = 0z1234\n  b[1 : 1] ..= 0z55\n  var after = 1\nenddef\n", "b[1 : 1]", "Cannot use a range with an assignment operator: b[1 : 1] ..= 0z55", true},
		{"list plus", "vim9script\ndef Func()\n  var values = [1, 2]\n  values[0 : 1] += 1\nenddef\n", "values[0 : 1]", "Cannot use a range with an assignment operator: values[0 : 1] += 1", false},
		{"dict plus", "vim9script\ndef Func()\n  var values = {a: 1}\n  values['a' : 'a'] += 1\nenddef\n", "values['a' : 'a']", "Cannot use a range with an assignment operator: values['a' : 'a'] += 1", false},
		{"string concat", "vim9script\ndef Func()\n  var text = 'ab'\n  text[0 : 1] ..= 'x'\nenddef\n", "text[0 : 1]", "Cannot use a range with an assignment operator: text[0 : 1] ..= 'x'", false},
		{"tuple plus", "vim9script\ndef Func()\n  var values = (1, 2)\n  values[0 : 1] += 1\nenddef\n", "values[0 : 1]", "Cannot use a range with an assignment operator: values[0 : 1] += 1", false},
		{"any plus", "vim9script\ndef Func()\n  var values: any\n  values[0 : 1] += 1\nenddef\n", "values[0 : 1]", "Cannot use a range with an assignment operator: values[0 : 1] += 1", false},
		{"unknown plus", "vim9script\ndef Func()\n  unknown[0 : 1] += 1\nenddef\n", "unknown[0 : 1]", "Cannot use a range with an assignment operator: unknown[0 : 1] += 1", false},
		{"Legacy-root def", "def Func()\n  var b = 0z12\n  b[0 : 1] ..= 0z34\nenddef\n", "b[0 : 1]", "Cannot use a range with an assignment operator: b[0 : 1] ..= 0z34", false},
		{"lambda", "vim9script\nvar Callback = () => {\n  var values = [1]\n  values[0 : 1] += 1\n}\n", "values[0 : 1]", "Cannot use a range with an assignment operator: values[0 : 1] += 1", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1183" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1141" || diagnostic.Code == "vim/E1165" || diagnostic.Code == "vim/E1019" || diagnostic.Code == "vim/E1012" {
					t.Fatalf("E1183 source retained %s: %#v", diagnostic.Code, result.Diagnostics)
				}
			}
			if len(got) != 1 || file.Text(got[0].Span) != test.span || got[0].Message != test.message {
				t.Fatalf("E1183 diagnostics = %#v", got)
			}
			if test.recovery && (file.Commands[len(file.Commands)-2].Declaration == nil || file.Text(file.Commands[len(file.Commands)-2].Declaration.Name) != "after") {
				t.Fatalf("following declaration was not retained: %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef Func()\n  var values = [1]\n  values[0 : 1] = [2]\n  values[0] += 1\nenddef\n",
		"vim9script\nvar values = [1]\nvalues[0 : 1] += 1\n",
		"let values = [1]\nlet values[0 : 1] += 1\n",
		"vim9script\ndef Func()\n  var values = [1]\n  legacy let values[0 : 1] += 1\nenddef\n",
		"vim9script\ndef Func()\n  var values = [1]\n  values[0 :\nenddef\n",
	} {
		result := Analyze(syntax.Parse(source))
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1183" {
				t.Fatalf("guard unexpectedly received E1183: %#v", result.Diagnostics)
			}
		}
	}
}

func TestAnalyzeE1166DictionaryRangeUnletDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source, span string
		recovery           bool
	}{
		{"official def recovery", "vim9script\ndef Func()\n  var dd = {a: 1}\n  unlet dd['a' : 'a']\n  var after = 1\nenddef\n", "dd['a' : 'a']", true},
		{"typed dict", "vim9script\ndef Func()\n  var dd: dict<number> = {}\n  unlet dd['a' : 'a']\nenddef\n", "dd['a' : 'a']", false},
		{"Legacy-root def", "def Func()\n  var dd = {}\n  unlet dd['a' : 'a']\nenddef\n", "dd['a' : 'a']", false},
		{"lambda", "vim9script\nvar Callback = () => {\n  var dd = {}\n  unlet dd['a' : 'a']\n}\n", "dd['a' : 'a']", false},
		{"bang after valid target", "vim9script\ndef Func()\n  var values = [1]\n  var dd = {}\n  unlet! values[0] dd['a' : 'a']\nenddef\n", "dd['a' : 'a']", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			all := CombinedDiagnostics(file, result)
			for _, diagnostic := range all {
				if diagnostic.Code == "vim/E1166" {
					got = append(got, diagnostic)
				}
			}
			if len(all) != 1 || len(got) != 1 || got[0].Message != "Cannot use a range with a dictionary" || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one E1166 on %q", all, test.span)
			}
			if test.recovery {
				found := false
				for index := range file.Commands {
					declaration := file.Commands[index].Declaration
					if declaration != nil && file.Text(declaration.Name) == "after" {
						found = true
					}
				}
				if !found {
					t.Fatalf("following declaration not retained: %#v", file.Commands)
				}
			}
		})
	}

	for _, test := range []struct{ name, source, want string }{
		{"ordinary Vim9 script", "vim9script\nvar d = {}\nunlet d['a' : 'a']\n", ""},
		{"Legacy script and function", "let d = {}\nunlet d['a' : 'a']\nfunction F()\n  unlet d['a' : 'a']\nendfunction\n", ""},
		{"legacy modifier", "vim9script\ndef F()\n  var d = {}\n  legacy unlet d['a' : 'a']\nenddef\n", ""},
		{"List range", "vim9script\ndef F()\n  var values = [1]\n  unlet values[0 : 1]\nenddef\n", ""},
		{"Blob range", "vim9script\ndef F()\n  var value = 0z12\n  unlet value[0 : 1]\nenddef\n", ""},
		{"any receiver", "vim9script\ndef F()\n  var value: any\n  unlet value[0 : 1]\nenddef\n", ""},
		{"unknown receiver", "vim9script\ndef F()\n  unlet unknown[0 : 1]\nenddef\n", "vim/E1001"},
		{"dynamic receiver", "vim9script\ndef F()\n  unlet ({})['a' : 'a']\nenddef\n", ""},
		{"Tuple receiver", "vim9script\ndef F()\n  var value = (1, 2)\n  unlet value[0 : 1]\nenddef\n", ""},
		{"number receiver", "vim9script\ndef F()\n  var value = 1\n  unlet value[0 : 1]\nenddef\n", ""},
		{"Dictionary index and member", "vim9script\ndef F()\n  var value = {}\n  unlet value['a'] value.a\nenddef\n", ""},
		{"incomplete range", "vim9script\ndef F()\n  var value = {}\n  unlet value['a' :\nenddef\n", ""},
		{"slice spacing precedence", "vim9script\ndef F()\n  var value = {}\n  unlet value['a':'a']\nenddef\n", "vim/E1004"},
		{"unresolved bound precedence", "vim9script\ndef F()\n  var value = {}\n  unlet value[missing : 'a']\nenddef\n", "vim/E1001"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			all := CombinedDiagnostics(file, Analyze(file))
			found := test.want == ""
			for _, diagnostic := range all {
				if diagnostic.Code == "vim/E1166" {
					t.Fatalf("unexpected E1166: %#v", all)
				}
				if diagnostic.Code == test.want {
					found = true
				}
			}
			if !found {
				t.Fatalf("diagnostics = %#v, want %s", all, test.want)
			}
		})
	}
}

func TestAnalyzeE1260ImportedMemberUnletDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source, span string
		recovery           bool
	}{
		{"top-level Vim9", "vim9script\nimport './Xfoo.vim' as Alias\nunlet Alias.Member\n", "Member", false},
		{"compiled def", "vim9script\nimport './Xfoo.vim' as Alias\ndef Func()\n  unlet! Alias.Member\nenddef\n", "Member", false},
		{"block lambda", "vim9script\nimport './Xfoo.vim' as Alias\nvar Callback = () => {\n  unlet Alias.Member\n}\n", "Member", false},
		{"Legacy function", "vim9script\nimport './Xfoo.vim' as Alias\nfunction Legacy()\n  unlet Alias.Member\nendfunction\n", "Member", false},
		{"multiple target recovery", "vim9script\nimport './Xfoo.vim' as Alias\nvar value = {}\nunlet value.key Alias.Member\nvar after = 1\n", "Member", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1260" {
					got = append(got, diagnostic)
				}
				if (diagnostic.Code == "vim/E1081" || diagnostic.Code == "vim/E1060") && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1260 source retained %s: %#v", diagnostic.Code, result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "Cannot unlet an imported item: Member" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1260 diagnostics = %#v", got)
			}
			if test.recovery {
				found := false
				for _, declaration := range result.Declarations {
					found = found || declaration.Name == "after"
				}
				if !found {
					t.Fatalf("following declaration was not retained: %#v", result.Declarations)
				}
			}
		})
	}

	for _, test := range []struct{ name, source string }{
		{"nested member", "vim9script\nimport './Xfoo.vim' as Alias\nunlet Alias.Member.key\n"},
		{"nested index", "vim9script\nimport './Xfoo.vim' as Alias\nunlet Alias.Member[0]\n"},
		{"nested slice", "vim9script\nimport './Xfoo.vim' as Alias\nunlet Alias.Member[0 : 1]\n"},
		{"bare alias", "vim9script\nimport './Xfoo.vim' as Alias\nunlet Alias\n"},
		{"non-import", "vim9script\nvar Alias = {}\nunlet Alias.Member\n"},
		{"lockvar", "vim9script\nimport './Xfoo.vim' as Alias\nlockvar Alias.Member\n"},
		{"numeric member", "vim9script\nimport './Xfoo.vim' as Alias\nunlet Alias.99\n"},
		{"spacing", "vim9script\nimport './Xfoo.vim' as Alias\nunlet Alias. Member\n"},
		{"Legacy root", "import './Xfoo.vim' as Alias\nunlet Alias.Member\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, diagnostic := range Analyze(syntax.Parse(test.source)).Diagnostics {
				if diagnostic.Code == "vim/E1260" {
					t.Fatalf("guard unexpectedly received E1260: %#v", diagnostic)
				}
			}
		})
	}
}

func TestAnalyzeE1167ArgumentShadowDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source, span string
		recovery           bool
	}{
		{"lambda shadows def local", "vim9script\ndef F()\n  var value = 1\n  var Callback = (value) => value\n  var after = 1\nenddef\n", "value", true},
		{"nested def shadows local", "vim9script\ndef F()\n  var value = 1\n  def Inner(value: number)\n  enddef\nenddef\n", "value", false},
		{"nested def shadows argument", "vim9script\ndef F(value: number)\n  def Inner(value: number)\n  enddef\nenddef\n", "value", false},
		{"Legacy-root def lambda", "def F()\n  var value = 1\n  var Callback = (value) => value\nenddef\n", "value", false},
		{"lambda shadows def argument", "vim9script\ndef F(value: number)\n  var Callback = (value) => value\nenddef\n", "value", false},
		{"nested lambda shadows lambda argument", "vim9script\ndef F()\n  var Callback = (value) => (value) => value\nenddef\n", "value", false},
		{"lambda shadows enclosing block local", "vim9script\ndef F()\n  if true\n    var value = 1\n    var Callback = (value) => value\n  endif\nenddef\n", "value", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			count := 0
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1167" {
					count++
					if diagnostic.Message != "Argument name shadows existing variable: value" || file.Text(diagnostic.Span) != test.span {
						t.Fatalf("diagnostic = %#v", diagnostic)
					}
				}
			}
			if count != 1 {
				t.Fatalf("want one E1167, got %d", count)
			}
			if test.recovery {
				found := false
				for index := range file.Commands {
					declaration := file.Commands[index].Declaration
					if declaration != nil && file.Text(declaration.Name) == "after" {
						found = true
					}
				}
				if !found {
					t.Fatalf("following declaration not retained: %#v", file.Commands)
				}
			}
		})
	}

	for _, test := range []struct{ name, source string }{
		{"script variable reserved for E1168", "vim9script\nvar value = 1\nvar Callback = (value) => value\n"},
		{"def argument conflicts with script variable", "vim9script\nvar value = 1\ndef F(value: number)\nenddef\n"},
		{"underscore parameter", "vim9script\ndef F(_: any)\n  var Callback = (_) => 0\nenddef\n"},
		{"outer local declared later", "vim9script\ndef F()\n  var Callback = (value) => value\n  var value = 1\nenddef\n"},
		{"sibling block local", "vim9script\ndef F()\n  if true\n    var value = 1\n  endif\n  if true\n    var Callback = (value) => value\n  endif\nenddef\n"},
		{"Legacy lambda and function", "let value = 1\nlet Callback = {value -> value}\nfunction Legacy(value)\nendfunction\n"},
		{"same-scope duplicate arguments", "vim9script\ndef F(value: number, value: number)\nenddef\n"},
		{"class member reserved for E1340", "vim9script\nclass Item\n  var value: number\n  def Method(value: number)\n  enddef\nendclass\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			for _, diagnostic := range CombinedDiagnostics(file, Analyze(file)) {
				if diagnostic.Code == "vim/E1167" {
					t.Fatalf("unexpected E1167: %#v", diagnostic)
				}
			}
		})
	}
}

func TestAnalyzeE1168ScriptArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source, span, message string
		recovery                    bool
	}{
		{"root def recovery", "vim9script\nvar name = 1\ndef Func(name: string)\nenddef\nvar after = 1\n", "name", "Argument already declared in the script: name: string)", true},
		{"same script block", "vim9script\nif true\n  var name = 'piet'\n  def Func(name: string)\n  enddef\nendif\n", "name", "Argument already declared in the script: name: string)", false},
		{"second block own variable", "vim9script\nif true\n  var name = 'piet'\nendif\nif true\n  var name = 'peter'\n  def Func(name: string)\n  enddef\nendif\n", "name", "Argument already declared in the script: name: string)", false},
		{"later root def", "vim9script\ndef Func(name: string)\nenddef\nvar name = 1\n", "name", "Argument already declared in the script: name: string)", false},
		{"script lambda", "vim9script\nvar name = 1\nvar Callback = (name) => name\n", "name", "Argument already declared in the script: name)", false},
		{"type alias", "vim9script\ntype A = number\ndef Foo(A: number)\nenddef\n", "A", "Argument already declared in the script: A: number)", false},
		{"lambda in def", "vim9script\nvar name = 1\ndef Func()\n  var Callback = (name) => name\nenddef\n", "name", "Argument already declared in the script: name)", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1167" {
					t.Fatalf("E1168 cascaded E1167: %#v", result.Diagnostics)
				}
				if diagnostic.Code == "vim/E1168" {
					count++
					if diagnostic.Message != test.message || file.Text(diagnostic.Span) != test.span {
						t.Fatalf("diagnostic = %#v", diagnostic)
					}
				}
			}
			if count != 1 {
				t.Fatalf("want one E1168, got %d", count)
			}
			if test.recovery {
				found := false
				for index := range file.Commands {
					declaration := file.Commands[index].Declaration
					if declaration != nil && file.Text(declaration.Name) == "after" {
						found = true
					}
				}
				if !found {
					t.Fatalf("following declaration not retained: %#v", file.Commands)
				}
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"variable in sibling block", "vim9script\nif true\n  var name = 1\nendif\nif true\n  def Func(name: number)\n  enddef\nendif\n", ""},
		{"later variable after script lambda", "vim9script\nvar Callback = (name) => name\nvar name = 1\n", ""},
		{"Legacy root", "let name = 1\nlet Callback = {name -> name}\nfunction Legacy(name)\nendfunction\n", ""},
		{"scoped global variable", "vim9script\ng:name = 1\ndef Func(name: number)\nenddef\n", ""},
		{"underscore", "vim9script\ndef Func(_: any)\nenddef\n", ""},
		{"class member reserved for E1340", "vim9script\nclass Item\n  var name: number\n  def Method(name: number)\n  enddef\nendclass\n", ""},
		{"outer local remains E1167", "vim9script\ndef Func()\n  var name = 1\n  var Callback = (name) => name\nenddef\n", "vim/E1167"},
		{"import and function names", "vim9script\nimport './Xmodule.vim' as module\ndef Helper()\nenddef\ndef Func(module: any, Helper: any)\nenddef\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			all := CombinedDiagnostics(file, Analyze(file))
			found := test.want == ""
			for _, diagnostic := range all {
				if diagnostic.Code == "vim/E1168" {
					t.Fatalf("unexpected E1168: %#v", all)
				}
				if diagnostic.Code == test.want {
					found = true
				}
			}
			if !found {
				t.Fatalf("diagnostics = %#v, want %s", all, test.want)
			}
		})
	}
}

func TestAnalyzeE1177UnsupportedForIterableDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, message, span string }{
		{"script dict", "vim9script\nfor i in {a: 1}\nendfor\n", "For loop on dict not supported", "{a: 1}"},
		{"def number", "vim9script\ndef F()\n  for i in 1\n  endfor\nenddef\n", "For loop on number not supported", "1"},
		{"lambda bool", "vim9script\nvar F = () => {\n  for i in true\n  endfor\n}\n", "For loop on bool not supported", "true"},
		{"Legacy-root def", "def F()\n  for i in 1\n  endfor\nenddef\n", "For loop on number not supported", "1"},
		{"class object", "vim9script\nclass Item\nendclass\ndef F()\n  var item = Item.new()\n  for value in item\n  endfor\nenddef\n", "For loop on object not supported", "item"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1177" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1012" {
					t.Fatalf("retained E1012: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1177 = %#v", got)
			}
		})
	}
	for _, source := range []string{
		"vim9script\nfor i in [1]\nendfor\nfor i in (1, 2)\nendfor\nfor i in 'x'\nendfor\nfor i in 0z12\nendfor\n",
		"vim9script\nfor i in Unknown\nendfor\nvar value: any\nfor i in value\nendfor\nlegacy for i in 1\nendfor\n",
		"for i in 1\nendfor\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1177" {
				t.Fatalf("unexpected E1177: %#v", diagnostic)
			}
		}
	}
	file := syntax.Parse("vim9script\ndef F()\n  for item: number in ['bad']\n  endfor\nenddef\n")
	result := Analyze(file)
	e1012 := 0
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1177" {
			t.Fatalf("valid iterable received E1177: %#v", result.Diagnostics)
		}
		if diagnostic.Code == "vim/E1012" {
			e1012++
			if file.Text(diagnostic.Span) != "['bad']" {
				t.Fatalf("E1012 = %#v", diagnostic)
			}
		}
	}
	if e1012 != 1 {
		t.Fatalf("diagnostics = %#v, want one E1012", result.Diagnostics)
	}
	for _, test := range []struct {
		name, source, code, message string
	}{
		{"class value", "vim9script\nclass Item\nendclass\nfor value in Item\nendfor\n", "vim/E1405", `Class "Item" cannot be used as a value`},
		{"script type alias", "vim9script\ntype T = dict<number>\nfor value in T\nendfor\n", "vim/E1403", `Type alias "T" cannot be used as a value`},
		{"compiled type alias", "vim9script\ntype T = dict<number>\ndef F()\n  for value in T\n  endfor\nenddef\n", "vim/E1407", "Cannot use a Typealias as a variable or value"},
	} {
		t.Run(test.name, func(t *testing.T) {
			owned := 0
			for _, diagnostic := range Analyze(syntax.Parse(test.source)).Diagnostics {
				if diagnostic.Code == "vim/E1177" {
					t.Fatalf("owned iterable received E1177: %#v", diagnostic)
				}
				if diagnostic.Code == test.code && diagnostic.Message == test.message {
					owned++
				}
			}
			if owned != 1 {
				t.Fatalf("owner count = %d, want 1", owned)
			}
		})
	}
	malformed := syntax.Parse("vim9script\ndef F()\n  for item in [1 : ]\n  endfor\nenddef\n")
	if len(malformed.Diagnostics) == 0 {
		t.Fatal("malformed iterable did not retain a syntax diagnostic")
	}
	malformedResult := Analyze(malformed)
	for _, diagnostic := range malformedResult.Diagnostics {
		if diagnostic.Code == "vim/E1177" {
			t.Fatalf("malformed iterable received E1177: %#v", malformedResult.Diagnostics)
		}
	}
}

func TestAnalyzeE1254ScriptVariableForBindingDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, span string }{
		{"official def", "vim9script\ndef Func()\n  for s:item in [1]\n  endfor\nenddef\n", "s:item"},
		{"block lambda", "vim9script\nvar Callback = () => {\n  for s:item in [1]\n  endfor\n}\n", "s:item"},
		{"destructuring", "vim9script\ndef Func()\n  for [item, s:scriptItem] in [[1, 2]]\n  endfor\nenddef\n", "s:scriptItem"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1254" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Cannot use script variable in for loop" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1254 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct{ name, source string }{
		{"top-level Vim9", "vim9script\nfor s:item in [1]\nendfor\n"},
		{"Legacy", "for s:item in [1]\nendfor\n"},
		{"legacy command in def", "vim9script\ndef Func()\n  legacy for s:item in [1]\n  endfor\nenddef\n"},
		{"ordinary and global", "vim9script\ndef Func()\n  for item in [1]\n  endfor\n  for g:item in [1]\n  endfor\nenddef\n"},
		{"underscore", "vim9script\ndef Func()\n  for _ in [1]\n  endfor\nenddef\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, diagnostic := range Analyze(syntax.Parse(test.source)).Diagnostics {
				if diagnostic.Code == "vim/E1254" {
					t.Fatalf("guard unexpectedly received E1254: %#v", diagnostic)
				}
			}
		})
	}

	file := syntax.Parse("vim9script\ndef Func()\n  for s:item in 1\n  endfor\nenddef\n")
	result := Analyze(file)
	var e1254, e1177 int
	for _, diagnostic := range result.Diagnostics {
		switch diagnostic.Code {
		case "vim/E1254":
			e1254++
			if file.Text(diagnostic.Span) != "s:item" {
				t.Fatalf("E1254 = %#v", diagnostic)
			}
		case "vim/E1177":
			e1177++
		}
	}
	if e1254 != 1 || e1177 != 1 {
		t.Fatalf("diagnostics = %#v, want E1254 and E1177", result.Diagnostics)
	}
}

func TestAnalyzeE1178LocalLockDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source, span string
		following          bool
	}{
		{"lock local", "vim9script\ndef F()\n  var values = [1]\n  lockvar values\n  var after = 1\nenddef\n", "values", true},
		{"unlock local", "vim9script\ndef F()\n  var values = [1]\n  unlockvar values\nenddef\n", "values", false},
		{"nested final", "vim9script\ndef F()\n  if true\n    final value = [1]\n    lockvar value\n  endif\nenddef\n", "value", false},
		{"for binding", "vim9script\ndef F()\n  for value in [1]\n    lockvar value\n  endfor\nenddef\n", "value", false},
		{"captured local", "vim9script\ndef F()\n  var value = [1]\n  var Lock = () => {\n    unlockvar value\n  }\nenddef\n", "value", false},
		{"Legacy-root def count", "def F()\n  var values = [1]\n  lockvar 2 values\nenddef\n", "values", false},
		{"bang first target", "vim9script\ndef F()\n  var first = [1]\n  var second = [2]\n  lockvar! first second\nenddef\n", "first", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1178" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Cannot lock or unlock a local variable" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1178 = %#v", got)
			}
			if test.following {
				found := false
				for _, declaration := range result.Declarations {
					if declaration.Name == "after" {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("following declaration was not retained: %#v", result.Declarations)
				}
			}
		})
	}
	for _, source := range []string{
		"vim9script\ndef F(value: list<number>)\n  lockvar value\n  var Lock = (argument: list<number>) => {\n    unlockvar argument\n  }\nenddef\n",
		"vim9script\nclass C\n  def Lock()\n    lockvar this\n  enddef\nendclass\n",
		"vim9script\ndef F()\n  var values = [{key: 1}]\n  lockvar values[0]\n  lockvar values[:]\n  lockvar values[0].key\nenddef\n",
		"vim9script\nvar script = [1]\ndef F()\n  lockvar script\nenddef\n",
		"vim9script\nclass C\n  var value = [1]\n  def Lock()\n    lockvar value\n  enddef\nendclass\n",
		"vim9script\ndef F()\n  lockvar g:value\n  lockvar b:value\n  lockvar $VIM\n  lockvar &tabstop\n  lockvar @a\n  lockvar unknown\nenddef\n",
		"vim9script\nvar value = [1]\nlockvar value\n",
		"let value = [1]\nfunction F()\n  let local = [1]\n  lockvar local\nendfunction\n",
		"vim9script\ndef F()\n  var value = [1]\n  legacy lockvar value\nenddef\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1178" {
				t.Fatalf("unexpected E1178: %#v", diagnostic)
			}
		}
	}
	malformed := syntax.Parse("vim9script\ndef F()\n  var value = [1]\n  lockvar value[)\nenddef\n")
	if len(malformed.Diagnostics) == 0 {
		t.Fatal("malformed lock target did not retain a syntax diagnostic")
	}
	for _, diagnostic := range Analyze(malformed).Diagnostics {
		if diagnostic.Code == "vim/E1178" {
			t.Fatalf("malformed target received E1178: %#v", diagnostic)
		}
	}
}

func TestAnalyzeE1181IgnoredUnderscoreDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source string
		following    bool
	}{
		{"script declaration", "vim9script\nvar _ = 1\nvar after = 2\n", true},
		{"def declaration", "vim9script\ndef F()\n  var _ = 1\nenddef\n", false},
		{"declaration owns rhs", "vim9script\nvar _ = _\n", false},
		{"script read", "vim9script\nvar value = _\n", false},
		{"def read", "vim9script\ndef F()\n  var value = _\nenddef\n", false},
		{"assignment target", "vim9script\ndef F(_)\n  _ = 1\nenddef\n", false},
		{"final declaration", "vim9script\nfinal _ = 1\n", false},
		{"call", "vim9script\n_(1)\n", false},
		{"operator", "vim9script\nvar value = _ + 1\n", false},
		{"member", "vim9script\nvar value = _.member\n", false},
		{"index", "vim9script\nvar value = _[0]\n", false},
		{"Legacy-root def", "def F()\n  var value = _\nenddef\n", false},
		{"vim9cmd", "vim9cmd var value = _\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1181" {
					got = append(got, diagnostic)
				}
				if (diagnostic.Code == "vim/E1001" || diagnostic.Code == "vim/E121" || diagnostic.Code == "vim/E1089" || diagnostic.Code == "vim/E1090" || diagnostic.Code == "vim/E117") && file.Text(diagnostic.Span) == "_" {
					t.Fatalf("underscore retained cascade: %#v", diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Cannot use an underscore here" || file.Text(got[0].Span) != "_" {
				t.Fatalf("E1181 = %#v; all diagnostics = %#v", got, result.Diagnostics)
			}
			if test.following {
				found := false
				for _, declaration := range result.Declarations {
					if declaration.Name == "after" {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("following declaration was not retained: %#v", result.Declarations)
				}
			}
		})
	}
	for _, source := range []string{
		"vim9script\ndef F(_, _)\nenddef\nvar Callback = (_, _) => 1\n",
		"vim9script\nfor _ in [1, 2]\nendfor\n",
		"vim9script\nvar [_, value] = [1, 2]\n[_, value] = [3, 4]\n",
		"vim9script\nvar dictionary = {_: 1}\nvar _value = dictionary._\nvar global = g:_\n",
		"let _ = 1\necho _\n",
		"vim9script\nlegacy echo _\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1181" {
				t.Fatalf("valid ignored underscore received E1181: %#v", diagnostic)
			}
		}
	}
}

func TestAnalyzeE1024NumberAsStringDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source string
	}{
		{name: "filter", source: "vim9script\nfilter([1, 2], 4)\n"},
		{name: "map", source: "vim9script\nmap([1, 2], 4)\n"},
		{name: "method", source: "vim9script\n[1, 2]->filter(4)\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1024" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Using a Number as a String" || file.Text(got[0].Span) != "4" {
				t.Fatalf("E1024 diagnostics = %#v; all diagnostics = %#v", got, result.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"call filter([1, 2], 4)\n",
		"vim9script\ndef F()\n  filter([1, 2], 4)\nenddef\n",
		"vim9script\nvar Callback = () => filter([1, 2], 4)\n",
		"vim9script\nfilter(1.1, 4)\n",
		"vim9script\nindexof({}, 4)\n",
		"vim9script\nfilter([1, 2], '4')\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1024" {
				t.Fatalf("source %q unexpectedly received E1024: %#v", source, diagnostic)
			}
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

func TestAnalyzeFloatAsStringDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, span string
	}{
		{
			name:   "vim9 script literal receiver",
			source: "vim9script\nvar value = 0.7[1]\n",
			span:   "0.7",
		},
		{
			name:   "vim9 script typed receiver",
			source: "vim9script\nvar receiver = 0.7\nvar value = receiver[1]\n",
			span:   "receiver",
		},
		{
			name:   "legacy receiver",
			source: "echo 1.1[0]\n",
			span:   "1.1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E806" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Using a Float as a String" || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one E806 on %q; all diagnostics = %#v", got, test.span, result.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef F()\n  var value = 0.7[1]\nenddef\n",
		"vim9script\nvar value = 'x' .. 0.7\n",
		"let value = 'x' . 0.7\n",
		"vim9script\nvar values = [1]\nvar value = values[0.7]\n",
	} {
		result := Analyze(syntax.Parse(source))
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E806" {
				t.Fatalf("source %q unexpectedly received E806: %#v", source, result.Diagnostics)
			}
		}
	}
}

func TestAnalyzeExtendArgumentDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, message, span string
	}{
		{
			name:    "vim9 script invalid first argument",
			source:  "vim9script\nextend('a', 1)\n",
			message: "Argument of extend() must be a List, Dictionary or Blob",
			span:    "'a'",
		},
		{
			name:    "vim9 script invalid second argument",
			source:  "vim9script\nextend([1, 2], 3)\n",
			message: "Argument of extend() must be a List, Dictionary or Blob",
			span:    "3",
		},
		{
			name:    "vim9 script mismatched extendnew containers",
			source:  "vim9script\nextendnew({a: 1}, [42])\n",
			message: "Argument of extendnew() must be a List, Dictionary or Blob",
			span:    "[42]",
		},
		{
			name:    "legacy invalid second argument",
			source:  "call extend(0z01, [2])\n",
			message: "Argument of extend() must be a List, Dictionary or Blob",
			span:    "[2]",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E896" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" {
					t.Fatalf("runtime E896 case retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one E896 %q on %q; all diagnostics = %#v", got, test.message, test.span, result.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef F()\n  extend('a', 1)\nenddef\n",
		"vim9script\ndef F()\n  extendnew({a: 1}, [42])\nenddef\n",
	} {
		result := Analyze(syntax.Parse(source))
		foundE1013 := false
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1013" {
				foundE1013 = true
			}
			if diagnostic.Code == "vim/E896" {
				t.Fatalf("compiled def retained E896: %#v", result.Diagnostics)
			}
		}
		if !foundE1013 {
			t.Fatalf("compiled def diagnostics = %#v, want E1013", result.Diagnostics)
		}
	}

	for _, source := range []string{
		"vim9script\nextend([1], ['x'])\n",
		"vim9script\nextend([1], [2])\n",
		"vim9script\nextend({a: 1}, {b: 2})\n",
		"vim9script\nextend(0z01, 0z02)\n",
	} {
		result := Analyze(syntax.Parse(source))
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E896" {
				t.Fatalf("source %q unexpectedly received E896: %#v", source, result.Diagnostics)
			}
		}
	}
}

func TestAnalyzeInvalidStringValueDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, message, span string
	}{
		{
			name:    "void right operand",
			source:  "vim9script\nvar value = 'a' .. test_void()\n",
			message: "Using an invalid value as a String: void",
			span:    "test_void()",
		},
		{
			name:    "job right operand",
			source:  "vim9script\nvar value = 'a' .. test_null_job()\n",
			message: "Using an invalid value as a String: job",
			span:    "test_null_job()",
		},
		{
			name:    "channel right operand",
			source:  "vim9script\nvar value = 'a' .. test_null_channel()\n",
			message: "Using an invalid value as a String: channel",
			span:    "test_null_channel()",
		},
		{
			name:    "job left operand",
			source:  "vim9script\nvar value = test_null_job() .. 'a'\n",
			message: "Using an invalid value as a String: job",
			span:    "test_null_job()",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E908" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one E908 %q on %q; all diagnostics = %#v", got, test.message, test.span, result.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef F()\n  var value = 'a' .. test_null_job()\nenddef\n",
		"let value = 'a' . test_null_job()\n",
		"vim9script\nvar value = test_void() .. 'a'\n",
		"vim9script\nvar value = 0z01 .. test_null_job()\n",
	} {
		result := Analyze(syntax.Parse(source))
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E908" {
				t.Fatalf("source %q unexpectedly received E908: %#v", source, result.Diagnostics)
			}
		}
	}
}

func TestAnalyzeBlobAsNumberDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, span string
	}{
		{
			name:   "vim9 script left binary operand",
			source: "vim9script\nvar value = 0z1234 - 44\n",
			span:   "0z1234",
		},
		{
			name:   "vim9 script right binary operand",
			source: "vim9script\nvar value = 33 + 0z1122\n",
			span:   "0z1122",
		},
		{
			name:   "legacy binary operand",
			source: "let value = 0z12 * 3\n",
			span:   "0z12",
		},
		{
			name:   "vim9 script ternary condition",
			source: "vim9script\nvar value = 0z12 ? 'one' : 'two'\n",
			span:   "0z12",
		},
		{
			name:   "compiled def ternary condition",
			source: "vim9script\ndef F()\n  var value = 0z12 ? 'one' : 'two'\nenddef\n",
			span:   "0z12",
		},
		{
			name:   "compiled def unary operand",
			source: "vim9script\ndef F()\n  var value = -0z12\nenddef\n",
			span:   "0z12",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E974" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Using a Blob as a Number" || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one E974 on %q; all diagnostics = %#v", got, test.span, result.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nvar value = 0z01 + 0z02\n",
		"vim9script\nvar value = [3] + 0z1122\n",
		"vim9script\nvar value = 'text' + 0z1122\n",
		"vim9script\ndef F()\n  var value = 0z12 - 3\nenddef\n",
		"vim9script\nvar value = 'text' .. 0z12\n",
	} {
		result := Analyze(syntax.Parse(source))
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E974" {
				t.Fatalf("source %q unexpectedly received E974: %#v", source, result.Diagnostics)
			}
		}
	}
}

func TestAnalyzeBlobAsStringDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, span string
	}{
		{
			name:   "vim9 script right operand",
			source: "vim9script\nvar value = 'a' .. 0z32\n",
			span:   "0z32",
		},
		{
			name:   "vim9 script left operand",
			source: "vim9script\nvar value = 0z32 .. 'a'\n",
			span:   "0z32",
		},
		{
			name:   "legacy operand",
			source: "let value = 'a' . 0z32\n",
			span:   "0z32",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E976" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Using a Blob as a String" || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one E976 on %q; all diagnostics = %#v", got, test.span, result.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef F()\n  var value = 'a' .. 0z32\nenddef\n",
		"vim9script\nvar value = 0z01 + 0z02\n",
		"vim9script\nvar value = 0z01 - 2\n",
	} {
		result := Analyze(syntax.Parse(source))
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E976" {
				t.Fatalf("source %q unexpectedly received E976: %#v", source, result.Diagnostics)
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
		{
			name:     "index container",
			source:   "vim9script\nindex('a', 'a')\n",
			code:     "vim/E1528",
			message:  "List or Tuple or Blob required for argument 1",
			argument: "'a'",
		},
		{
			name:     "indexof method receiver",
			source:   "vim9script\n{}->indexof((_, _) => true)\n",
			code:     "vim/E1528",
			message:  "List or Tuple or Blob required for argument 1",
			argument: "{}",
		},
		{
			name:     "join container",
			source:   "vim9script\njoin('abc')\n",
			code:     "vim/E1529",
			message:  "List or Tuple required for argument 1",
			argument: "'abc'",
		},
		{
			name:     "join method receiver",
			source:   "vim9script\n{}->join()\n",
			code:     "vim/E1529",
			message:  "List or Tuple required for argument 1",
			argument: "{}",
		},
		{
			name:     "max container",
			source:   "vim9script\nmax(5)\n",
			code:     "vim/E1530",
			message:  "List or Tuple or Dictionary required for argument 1",
			argument: "5",
		},
		{
			name:     "min method receiver",
			source:   "vim9script\n5->min()\n",
			code:     "vim/E1530",
			message:  "List or Tuple or Dictionary required for argument 1",
			argument: "5",
		},
		{
			name:     "get container",
			source:   "vim9script\nget('a', 1)\n",
			code:     "vim/E1531",
			message:  "Argument of get() must be a List, Tuple, Dictionary or Blob",
			argument: "'a'",
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

	positive := syntax.Parse("vim9script\nsubstitute('Hallo', 'a', 'e', '')\n{'a': 1}->keys()\nlen(123)\nindex([1], 1)\nindex((1, 2), 1)\nindex(0z12, 1)\njoin([1])\n(1, 2)->join()\nmax([1])\n(1, 2)->min()\nmax({a: 1})\nvar dynamic: any\nindex(dynamic, 1)\njoin(dynamic)\nmax(dynamic)\nget([1], 0)\n(1, 2)->get(0)\nget(0z12, 0)\nget({a: 1}, 'a')\nfunction('max')->get('name')\n")
	for _, diagnostic := range Analyze(positive).Diagnostics {
		if diagnostic.Code == "vim/E701" || diagnostic.Code == "vim/E1174" || diagnostic.Code == "vim/E1206" || diagnostic.Code == "vim/E1528" || diagnostic.Code == "vim/E1529" || diagnostic.Code == "vim/E1530" || diagnostic.Code == "vim/E1531" {
			t.Fatalf("valid builtin argument diagnostic = %#v", diagnostic)
		}
	}

	def := syntax.Parse("vim9script\ndef F()\n  get('a', 1)\nenddef\n")
	var e1013 int
	for _, diagnostic := range Analyze(def).Diagnostics {
		if diagnostic.Code == "vim/E1531" {
			t.Fatalf("def mismatch reported script-level E1531: %#v", diagnostic)
		}
		if diagnostic.Code == "vim/E1013" {
			e1013++
		}
	}
	if e1013 != 1 {
		t.Fatalf("def E1013 diagnostics = %d, want one", e1013)
	}

	for _, diagnostic := range Analyze(syntax.Parse("echo get('a', 1)\n")).Diagnostics {
		if diagnostic.Code == "vim/E1531" {
			t.Fatalf("legacy call reported static E1531: %#v", diagnostic)
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

func TestAnalyzeNonCallableFunctionDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, text string
		wantE1085          bool
	}{
		{
			name: "official number variable in def",
			source: "vim9script\ndef Test()\n" +
				"  var Ref: number\n  Ref()\nenddef\n",
			text:      "Ref",
			wantE1085: true,
		},
		{
			name: "official string variable in def",
			source: "vim9script\ndef Test()\n" +
				"  var Ref: string\n  var res = Ref()\nenddef\n",
			text:      "Ref",
			wantE1085: true,
		},
		{
			name:   "unknown and any remain conservative",
			source: "vim9script\ndef Test()\n  var Unknown: func\n  Unknown()\n  var Value: any\n  Value()\nenddef\n",
		},
		{
			name: "function value continues to arity diagnostics",
			source: "vim9script\ndef Test()\n  def Ref(value: number)\n  enddef\n" +
				"  Ref()\nenddef\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var e1085 []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1085" {
					e1085 = append(e1085, diagnostic)
				}
			}
			if test.wantE1085 {
				if len(e1085) != 1 || e1085[0].Message != "Not a callable type: "+test.text || file.Text(e1085[0].Span) != test.text {
					t.Fatalf("E1085 diagnostics = %#v, want one on %q; syntax diagnostics = %#v", e1085, test.text, file.Diagnostics)
				}
				return
			}
			if len(e1085) != 0 {
				t.Fatalf("E1085 diagnostics = %#v, want none; syntax diagnostics = %#v", e1085, file.Diagnostics)
			}
		})
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

func TestAnalyzeVim9ScriptMapCallbackDiagnostics(t *testing.T) {
	tests := []struct {
		name, source       string
		want               []struct{ message, text string }
		wantE176, wantE118 int
	}{
		{
			name: "official method callbacks",
			source: "vim9script\n[0, 1, 2]->map(() => 123)\n" +
				"[0, 1, 2]->map((_) => 123)\n",
			want: []struct{ message, text string }{{"2 arguments too many", "() => 123"}, {"One argument too many", "(_) => 123"}},
		},
		{
			name: "direct filter and foreach callbacks",
			source: "vim9script\nfilter([1], () => true)\n" +
				"foreach([1], (_) => 0)\n",
			want: []struct{ message, text string }{{"2 arguments too many", "() => true"}, {"One argument too many", "(_) => 0"}},
		},
		{
			name: "compiled def keeps E176",
			source: "vim9script\ndef Compiled()\n  map([1], () => 0)\n" +
				"  map([1], (_) => 0)\nenddef\n",
			wantE176: 2,
		},
		{
			name:     "indexof retains E118",
			source:   "vim9script\nindexof([1], () => true)\n",
			wantE118: 1,
		},
		{
			name:   "Legacy-root lambda is excluded",
			source: "call map([1], { -> 0 })\n",
		},
		{
			name: "accepted and too-many Vim9 lambda shapes are excluded",
			source: "vim9script\nmap([1], (index, value) => value)\n" +
				"map([1], (index, ...rest) => rest[0])\nmap([1], (a, b, c) => a)\n",
		},
		{
			name: "stored and any callbacks are excluded",
			source: "vim9script\nvar Callback = () => 0\nmap([1], Callback)\n" +
				"var Dynamic: any\nmap([1], Dynamic)\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			e176, e118 := 0, 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1106" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E176" {
					e176++
				}
				if diagnostic.Code == "vim/E118" {
					e118++
				}
				if diagnostic.Code == "vim/E1013" && len(test.want) > 0 {
					t.Fatalf("E1106 source retained E1013: %#v", diagnostic)
				}
			}
			if len(got) != len(test.want) {
				t.Fatalf("E1106 diagnostics = %#v, want %#v; all diagnostics = %#v", got, test.want, result.Diagnostics)
			}
			if e176 != test.wantE176 || e118 != test.wantE118 {
				t.Fatalf("guard diagnostics E176=%d E118=%d, want E176=%d E118=%d; all diagnostics = %#v", e176, e118, test.wantE176, test.wantE118, result.Diagnostics)
			}
			for index, diagnostic := range got {
				want := test.want[index]
				if diagnostic.Message != want.message || file.Text(diagnostic.Span) != want.text {
					t.Fatalf("E1106[%d] = %#v on %q, want %q on %q", index, diagnostic, file.Text(diagnostic.Span), want.message, want.text)
				}
			}
		})
	}
}

func TestAnalyzeE1190Vim9ScriptCallbackDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source, message, span string
	}{
		{"official method one too few", "vim9script\n[0, 1, 2]->map((a, b, c) => a)\n", "One argument too few", "(a, b, c) => a"},
		{"official method plural", "vim9script\n[0, 1, 2]->map((a, b, c, d) => a)\n", "2 arguments too few", "(a, b, c, d) => a"},
		{"direct filter", "vim9script\nfilter([1], (a, b, c) => true)\n", "One argument too few", "(a, b, c) => true"},
		{"direct foreach", "vim9script\nforeach([1], (a, b, c, d) => 0)\n", "2 arguments too few", "(a, b, c, d) => 0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1190" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" {
					t.Fatalf("E1190 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1190 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct {
		name, source, wantCode string
		wantCount              int
	}{
		{"zero and one slots retain E1106", "vim9script\nmap([1], () => 0)\nmap([1], (value) => value)\n", "vim/E1106", 2},
		{"two slots and variadic rest", "vim9script\nmap([1], (index, value) => value)\nmap([1], (index, value, ...rest) => value)\n", "", 0},
		{"compiled def retains E176", "vim9script\ndef Func()\n  map([1], (a, b, c) => a)\nenddef\n", "vim/E176", 1},
		{"compiled block lambda", "vim9script\nvar Func = () => {\n  map([1], (a, b, c) => a)\n}\n", "", 0},
		{"Legacy-root expression", "let Callback = {a, b, c -> a}\ncall map([1], Callback)\n", "", 0},
		{"stored and dynamic callback", "vim9script\nvar Callback = (a, b, c) => a\nmap([1], Callback)\nvar Dynamic: any\nmap([1], Dynamic)\n", "", 0},
		{"dynamic container", "vim9script\nvar Container: any\nmap(Container, (a, b, c) => a)\n", "", 0},
		{"ordinary call keeps E119", "vim9script\ndef Func(a: number, b: number, c: number)\nenddef\nFunc(1)\n", "vim/E119", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1190" {
					t.Fatalf("guard unexpectedly received E1190: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.wantCode {
					count++
				}
			}
			if count != test.wantCount {
				t.Fatalf("%s count = %d, want %d; diagnostics = %#v", test.wantCode, count, test.wantCount, result.Diagnostics)
			}
		})
	}
}

func TestAnalyzeE1207NameOnlyExpressionDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, span string }{
		{"register recovery", "vim9script\n@a\n# comment\nvar after = 1\n", "@a"},
		{"search register", "vim9script\n@/\n", "@/"},
		{"options", "vim9script\n&opfunc\n&l:showbreak\n&g:showbreak\n", "&opfunc"},
		{"environment", "vim9script\n$SomeEnv\n", "$SomeEnv"},
		{"eval strings", "vim9script\neval 'text'\neval \"text\"\n", "'text'"},
		{"literal", "vim9script\ntrue\n", "true"},
		{"predefined variable", "vim9script\nv:version\n", "v:version"},
		{"resolved variable", "vim9script\nvar value = 1\nvalue\n", "value"},
		{"shadowed command", "vim9script\nvar undo = 1\nundo\n", "undo"},
		{"compiled def", "vim9script\ndef Func()\n  @a\nenddef\n", "@a"},
		{"Legacy-root def", "def Func()\n  @a\nenddef\n", "@a"},
		{"vim9cmd", "vim9cmd @a\n", "@a"},
		{"modifier", "vim9script\nsilent @a\n", "@a"},
		{"lambda", "vim9script\nvar Callback = () => {\n  @a\n}\n", "@a"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1207" {
					got = append(got, diagnostic)
				}
			}
			if test.name == "options" {
				if len(got) != 3 {
					t.Fatalf("E1207 diagnostics = %#v", got)
				}
				return
			}
			if test.name == "eval strings" {
				if len(got) != 2 || file.Text(got[0].Span) != "'text'" || file.Text(got[1].Span) != "\"text\"" {
					t.Fatalf("E1207 diagnostics = %#v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Message != "Expression without an effect: "+test.span || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1207 diagnostics = %#v", got)
			}
			if test.name == "register recovery" && (file.Commands[len(file.Commands)-1].Declaration == nil || file.Text(file.Commands[len(file.Commands)-1].Declaration.Name) != "after") {
				t.Fatalf("following declaration was not retained: %#v", file.Commands)
			}
		})
	}
	for _, source := range []string{
		"vim9script\nlen([])\nvalue = 1\nvalue.method()\nvalue[0]\n(value)\n1\n[]\n{}\n", "vim9script\nUnknown\n&unknown_option\nundo\n@\n@<\n$\n@a tail\n",
		"vim9script\nlegacy eval 'text'\n", "let value = 1\neval 'text'\n", "vim9script\nvalue\nvar value = 1\n",
		"vim9script\ndef Func()\nenddef\nFunc\n",
	} {
		result := Analyze(syntax.Parse(source))
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1207" {
				t.Fatalf("guard unexpectedly received E1207: %#v", result.Diagnostics)
			}
		}
	}
}

func TestAnalyzeE1210BuiltinNumberArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, message, span string }{
		{"arg_number first", "vim9script\nand([], 1)\n", "Number required for argument 1", "[]"},
		{"arg_number second", "vim9script\nand(1, [])\n", "Number required for argument 2", "[]"},
		{"method argument index", "vim9script\n1->and('x')\n", "Number required for argument 2", "'x'"},
		{"blob add item", "vim9script\nadd(0z12, 'x')\n", "Number required for argument 2", "'x'"},
		{"list remove index", "vim9script\nremove([1], 'x')\n", "Number required for argument 2", "'x'"},
		{"blob remove index", "vim9script\nremove(0z12, 'x')\n", "Number required for argument 2", "'x'"},
		{"vim9cmd", "vim9cmd and([], 1)\n", "Number required for argument 1", "[]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1210" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1210 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1210 diagnostics = %#v", got)
			}
		})
	}
	for _, test := range []struct{ name, source, want string }{
		{"compiled def", "vim9script\ndef Func()\n  and([], 1)\nenddef\n", "vim/E1013"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  and([], 1)\n}\n", "vim/E1013"},
		{"float keeps E805", "vim9script\nand(1.0, 1)\n", "vim/E805"},
		{"unknown", "vim9script\nand(Unknown, 1)\n", ""},
		{"Legacy", "let value = and([], 1)\n", ""},
		{"list item mismatch", "vim9script\nadd([1], 'x')\n", ""},
		{"extend third", "vim9script\nextend([1], [2], 'x')\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1210" {
					t.Fatalf("guard unexpectedly received E1210: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1211BuiltinListArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, message, span string }{
		{"list any non-first", "vim9script\ncomplete(1, 'x')\n", "List required for argument 2", "'x'"},
		{"list number", "vim9script\nsetpos('.', 'x')\n", "List required for argument 2", "'x'"},
		{"list string", "vim9script\ncomplete_info('x')\n", "List required for argument 1", "'x'"},
		{"slice", "vim9script\nslice({}, 1)\n", "List required for argument 1", "{}"},
		{"method index", "vim9script\n1->complete('x')\n", "List required for argument 2", "'x'"},
		{"vim9cmd", "vim9cmd complete(1, 'x')\n", "List required for argument 2", "'x'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1211" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1211 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1211 diagnostics = %#v", got)
			}
		})
	}
	for _, test := range []struct{ name, source, want string }{
		{"compiled def", "vim9script\ndef Func()\n  complete(1, 'x')\nenddef\n", "vim/E1013"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  complete(1, 'x')\n}\n", "vim/E1013"},
		{"Legacy", "let value = complete(1, 'x')\n", ""},
		{"unknown", "vim9script\ncomplete(1, Unknown)\n", ""},
		{"list item mismatch", "vim9script\ncomplete_info([1])\n", "vim/E1013"},
		{"arg reverse", "vim9script\nreverse({})\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1211" {
					t.Fatalf("guard unexpectedly received E1211: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1212BuiltinBoolArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, message, span string }{
		{"first argument", "vim9script\ndigraph_getlist(2)\n", "Bool required for argument 1", "2"},
		{"later argument", "vim9script\ndeepcopy('x', 2)\n", "Bool required for argument 2", "2"},
		{"string", "vim9script\ndeepcopy('x', 'no')\n", "Bool required for argument 2", "'no'"},
		{"container", "vim9script\ndeepcopy('x', [])\n", "Bool required for argument 2", "[]"},
		{"method argument", "vim9script\n'x'->deepcopy(2)\n", "Bool required for argument 2", "2"},
		{"vim9cmd", "vim9cmd deepcopy('x', 2)\n", "Bool required for argument 2", "2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1212" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1212 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1212 diagnostics = %#v", got)
			}
		})
	}
	for _, test := range []struct{ name, source, want, forbid string }{
		{"compiled def", "vim9script\ndef Func()\n  deepcopy('x', 2)\nenddef\n", "vim/E1013", ""},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  deepcopy('x', 2)\n}\n", "vim/E1013", ""},
		{"literal zero one", "vim9script\ndeepcopy('x', 0)\ndeepcopy('x', (+1))\n", "", "vim/E1013"},
		{"script variable number conservative", "vim9script\nvar value = 2\ndeepcopy('x', value)\n", "", "vim/E1013"},
		{"def variable number", "vim9script\ndef Func()\n  var value = 2\n  deepcopy('x', value)\nenddef\n", "vim/E1013", ""},
		{"Legacy", "let value = deepcopy('x', 2)\n", "", ""},
		{"unknown", "vim9script\ndeepcopy('x', Unknown)\n", "", ""},
		{"bool-or-number", "vim9script\ngetchar('x')\n", "", ""},
		{"bool-or-dict", "vim9script\nlistener_add('Callback', 1, [])\n", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1212" {
					t.Fatalf("guard unexpectedly received E1212: %#v", result.Diagnostics)
				}
				if test.forbid != "" && diagnostic.Code == test.forbid {
					t.Fatalf("guard unexpectedly received %s: %#v", test.forbid, result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1235BuiltinBoolOrNumberArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, span string }{
		{"getchar string", "vim9script\ngetchar('1')\n", "'1'"},
		{"getcharstr string", "vim9script\ngetcharstr('1')\n", "'1'"},
		{"container", "vim9script\ngetchar([])\n", "[]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1235" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1235 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "Bool or Number required for argument 1" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1235 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct{ name, source, want string }{
		{"compiled def", "vim9script\ndef Func()\n  getchar('1')\nenddef\n", "vim/E1013"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  getcharstr('1')\n}\n", "vim/E1013"},
		{"bool", "vim9script\ngetchar(true)\n", ""},
		{"number", "vim9script\ngetchar(1)\n", ""},
		{"unknown", "vim9script\ngetchar(Unknown)\n", ""},
		{"Legacy", "let value = getchar('1')\n", ""},
		{"number value retains E1023", "vim9script\ngetchar(2)\n", "vim/E1023"},
		{"second options argument retains E1206", "vim9script\ngetchar(0, [])\n", "vim/E1206"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1235" {
					t.Fatalf("guard unexpectedly received E1235: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1238BuiltinBlobArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, span string }{
		{"official number", "vim9script\nblob2list(10)\n", "10"},
		{"official string", "vim9script\nblob2str('ab')\n", "'ab'"},
		{"method receiver", "vim9script\n10->blob2list()\n", "10"},
		{"base64 encode", "vim9script\nbase64_encode([])\n", "[]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1238" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1238 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "Blob required for argument 1" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1238 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct{ name, source, want string }{
		{"compiled def", "vim9script\ndef Func()\n  blob2list(10)\nenddef\n", "vim/E1013"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  blob2str('ab')\n}\n", "vim/E1013"},
		{"blob", "vim9script\nblob2list(0z12)\n", ""},
		{"unknown", "vim9script\nblob2list(Unknown)\n", ""},
		{"Legacy", "let value = blob2list(10)\n", ""},
		{"second options argument retains E1206", "vim9script\nblob2str(0z12, [])\n", "vim/E1206"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1238" {
					t.Fatalf("guard unexpectedly received E1238: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1251BuiltinContainerArgumentDiagnostics(t *testing.T) {
	const message = "List, Tuple, Dictionary, Blob or String required for argument 1"
	for _, test := range []struct{ name, source, span string }{
		{"filter script", "vim9script\nfilter(1, (_, _) => true)\n", "1"},
		{"foreach script job", "vim9script\nforeach(null_job, (_, _) => true)\n", "null_job"},
		{"foreach def job", "vim9script\ndef Func()\n  foreach(null_job, (_, _) => true)\nenddef\n", "null_job"},
		{"items method def", "vim9script\ndef Func()\n  123->items()\nenddef\n", "123"},
		{"map channel", "vim9script\nmap(null_channel, (_, _) => 1)\n", "null_channel"},
		{"mapnew job", "vim9script\nmapnew(null_job, (_, _) => 1)\n", "null_job"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1251" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1251 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1251 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct{ name, source, want string }{
		{"compiled filter", "vim9script\ndef Func()\n  filter(1, (_, _) => true)\nenddef\n", "vim/E1013"},
		{"compiled map", "vim9script\ndef Func()\n  map(1, (_, _) => 1)\nenddef\n", "vim/E1013"},
		{"compiled mapnew", "vim9script\ndef Func()\n  mapnew(1, (_, _) => 1)\nenddef\n", "vim/E1013"},
		{"valid containers", "vim9script\nfilter([], (_, _) => true)\nforeach({}, (_, _) => true)\nitems(0z12)\n", ""},
		{"unknown", "vim9script\nfilter(Unknown, (_, _) => true)\n", ""},
		{"Legacy", "let value = filter(1, 'Callback')\n", ""},
		{"tuple filter", "vim9script\nfilter((1, 2), (_, _) => true)\n", ""},
		{"tuple items", "vim9script\ndef Func()\n  (1, 2)->items()\nenddef\n", ""},
		{"callback ownership", "vim9script\nfilter([], () => true)\n", ""},
		{"arity ownership", "vim9script\nfilter([])\n", "vim/E119"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1251" {
					t.Fatalf("guard unexpectedly received E1251: %#v", result.Diagnostics)
				}
				if test.name == "tuple filter" && diagnostic.Code == "vim/E1013" {
					t.Fatalf("top-level tuple incorrectly received E1013: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1253ReduceReverseContainerDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, span string }{
		{"official reduce", "vim9script\nreduce({a: 10}, '1')\n", "{a: 10}"},
		{"official reverse", "vim9script\nreverse(10)\n", "10"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1253" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1253 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "String, List, Tuple or Blob required for argument 1" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1253 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct{ name, source, want string }{
		{"compiled def", "vim9script\ndef Func()\n  reverse(10)\nenddef\n", "vim/E1013"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  reduce({a: 10}, '1')\n}\n", "vim/E1013"},
		{"valid containers", "vim9script\nreverse('text')\nreverse([1])\nreverse((1, 2))\nreverse(0z12)\n", ""},
		{"unknown", "vim9script\nreverse(Unknown)\n", ""},
		{"reduce callback ownership", "vim9script\nreduce([], 'not callback')\n", ""},
		{"arity ownership", "vim9script\nreverse()\n", "vim/E119"},
		{"Legacy", "let value = reverse(10)\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1253" {
					t.Fatalf("guard unexpectedly received E1253: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1301RepeatArgumentDiagnostics(t *testing.T) {
	const message = "String, Number, List, Tuple or Blob required for argument 1"
	for _, test := range []struct{ name, source, span string }{
		{"official float", "vim9script\nrepeat(1.1, 2)\n", "1.1"},
		{"official dictionary", "vim9script\nrepeat({a: 10}, 2)\n", "{a: 10}"},
		{"Bool method receiver", "vim9script\ntrue->repeat(2)\n", "true"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1301" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1301 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1301 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct{ name, source, want string }{
		{"compiled def", "vim9script\ndef Func()\n  repeat(1.1, 2)\nenddef\n", "vim/E1013"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  repeat({a: 10}, 2)\n}\n", "vim/E1013"},
		{"valid values", "vim9script\nrepeat('x', 2)\nrepeat(1, 2)\nrepeat([1], 2)\nrepeat((1, 2), 2)\nrepeat(0z12, 2)\n", ""},
		{"unknown", "vim9script\nrepeat(Unknown, 2)\n", ""},
		{"Legacy", "let value = repeat(1.1, 2)\n", ""},
		{"arity ownership", "vim9script\nrepeat()\n", "vim/E119"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1301" {
					t.Fatalf("guard unexpectedly received E1301: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1306LoopNestingDiagnostics(t *testing.T) {
	nestedLoops := func(kinds []string, prefix, suffix string) string {
		var source strings.Builder
		source.WriteString(prefix)
		for index, kind := range kinds {
			if kind == "for" {
				source.WriteString(strings.Repeat("  ", index))
				source.WriteString("for item")
				source.WriteString(strconv.Itoa(index))
				source.WriteString(" in [1]\n")
			} else {
				source.WriteString(strings.Repeat("  ", index))
				source.WriteString("while true\n")
			}
		}
		source.WriteString(strings.Repeat("  ", len(kinds)))
		source.WriteString("var value = 1\n")
		for index := len(kinds) - 1; index >= 0; index-- {
			if kinds[index] == "for" {
				source.WriteString(strings.Repeat("  ", index))
				source.WriteString("endfor\n")
			} else {
				source.WriteString(strings.Repeat("  ", index))
				source.WriteString("endwhile\n")
			}
		}
		source.WriteString(suffix)
		return source.String()
	}
	elevenFor := make([]string, 11)
	for index := range elevenFor {
		elevenFor[index] = "for"
	}
	mixed := append([]string(nil), elevenFor[:10]...)
	mixed = append(mixed, "while")
	lambdaEleven := nestedLoops(elevenFor, "vim9script\nvar Callback = () => {\n", "}\n")
	for _, test := range []struct {
		name, source, span string
		want               int
	}{
		{"official Legacy-root def mixed loops", nestedLoops(mixed, "def Func()\n", "enddef\n"), "while", 1},
		{"eleven for loops", nestedLoops(elevenFor, "vim9script\ndef Func()\n", "enddef\n"), "for", 1},
		{"eleventh while", nestedLoops(mixed, "vim9script\ndef Func()\n", "enddef\n"), "while", 1},
		{"block lambda eleven loops", lambdaEleven, "for", 1},
		{"ten loops", nestedLoops(elevenFor[:10], "vim9script\ndef Func()\n", "enddef\n"), "", 0},
		{"Vim9 script", nestedLoops(elevenFor, "vim9script\n", ""), "", 0},
		{"Legacy function", nestedLoops(elevenFor, "function Func()\n", "endfunction\n"), "", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1306" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != test.want {
				t.Fatalf("E1306 diagnostics = %#v", got)
			}
			for _, diagnostic := range got {
				if diagnostic.Message != "Loop nesting too deep" || file.Text(diagnostic.Span) != test.span {
					t.Fatalf("E1306 diagnostic = %#v", diagnostic)
				}
			}
		})
	}

	lambdaSource := nestedLoops(elevenFor[:10], "vim9script\ndef Func()\n", "enddef\n")
	lambdaSource = strings.Replace(lambdaSource, "                    var value = 1\n", "                    var Callback = () => {\n                      for inner in [1]\n                      endfor\n                    }\n", 1)
	lambdaResult := Analyze(syntax.Parse(lambdaSource))
	for _, diagnostic := range lambdaResult.Diagnostics {
		if diagnostic.Code == "vim/E1306" {
			t.Fatalf("lambda loop boundary diagnostics = %#v", lambdaResult.Diagnostics)
		}
	}

	twoDefs := nestedLoops(elevenFor, "vim9script\ndef First()\n", "enddef\ndef Second()\n") + nestedLoops(elevenFor, "", "enddef\n")
	file := syntax.Parse(twoDefs)
	result := Analyze(file)
	count := 0
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1306" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("independent def diagnostics = %#v", result.Diagnostics)
	}
}

func TestAnalyzeE1307ConstBuiltinMutationDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source, span, typeName string
	}{
		{"add list", "vim9script\ndef Func()\n  const value = [1]\n  add(value, 2)\nenddef\n", "value", "list<number>"},
		{"extend list", "vim9script\ndef Func()\n  const value = [1]\n  extend(value, [2])\nenddef\n", "value", "list<number>"},
		{"filter list", "vim9script\ndef Func()\n  const value = [1]\n  filter(value, (_, _) => true)\nenddef\n", "value", "list<number>"},
		{"map list", "vim9script\ndef Func()\n  const value = [1]\n  map(value, (_, item) => item)\nenddef\n", "value", "list<number>"},
		{"remove dictionary", "vim9script\ndef Func()\n  const value = {a: 1}\n  remove(value, 'a')\nenddef\n", "value", "dict<number>"},
		{"reverse blob", "vim9script\ndef Func()\n  const value = 0z12\n  reverse(value)\nenddef\n", "value", "blob"},
		{"sort list", "vim9script\ndef Func()\n  const value = [1]\n  sort(value)\nenddef\n", "value", "list<number>"},
		{"uniq list", "vim9script\ndef Func()\n  const value = [1]\n  uniq(value)\nenddef\n", "value", "list<number>"},
		{"filter string", "vim9script\ndef Func()\n  const value = 'text'\n  filter(value, (_, _) => true)\nenddef\n", "value", "string"},
		{"reverse tuple", "vim9script\ndef Func()\n  const value = (1, 2)\n  reverse(value)\nenddef\n", "value", "tuple<number, number>"},
		{"method parenthesized", "vim9script\ndef Func()\n  const value = [1]\n  (value)->add(2)\nenddef\n", "(value)", "list<number>"},
		{"nested block", "vim9script\ndef Func()\n  const value = [1]\n  if true\n    add(value, 2)\n  endif\nenddef\n", "value", "list<number>"},
		{"block lambda", "vim9script\nvar Callback = () => {\n  const value = [1]\n  add(value, 2)\n}\n", "value", "list<number>"},
		{"Legacy-root def", "def Func()\n  const value = [1]\n  add(value, 2)\nenddef\n", "value", "list<number>"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1307" {
					got = append(got, diagnostic)
				}
			}
			message := "Argument 1: Trying to modify a const " + test.typeName
			if len(got) != 1 || got[0].Message != message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1307 diagnostics = %#v", result.Diagnostics)
			}
		})
	}

	file := syntax.Parse("vim9script\ndef Func()\n  const value = [1]\n  add(value, 'bad')\nenddef\n")
	result := Analyze(file)
	count := 0
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1307" {
			count++
		}
		if diagnostic.Code == "vim/E1013" {
			t.Fatalf("E1307-owned call retained E1013: %#v", result.Diagnostics)
		}
	}
	if count != 1 {
		t.Fatalf("E1307-owned call diagnostics = %#v", result.Diagnostics)
	}

	file = syntax.Parse("vim9script\nvar Callback = () => {\n  const value = [1]\n  map(value, (_, _) => 'bad')\n}\n")
	result = Analyze(file)
	count = 0
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1307" {
			count++
		}
		if diagnostic.Code == "vim/E1012" || diagnostic.Code == "vim/E1013" {
			t.Fatalf("E1307-owned lambda call retained type mismatch: %#v", result.Diagnostics)
		}
	}
	if count != 1 {
		t.Fatalf("E1307-owned lambda diagnostics = %#v", result.Diagnostics)
	}

	for _, test := range []struct{ name, source, want string }{
		{"final", "vim9script\ndef Func()\n  final value = [1]\n  add(value, 2)\nenddef\n", ""},
		{"for binding", "vim9script\ndef Func()\n  for value in [[1]]\n    add(value, 2)\n  endfor\nenddef\n", ""},
		{"extendnew", "vim9script\ndef Func()\n  const value = [1]\n  extendnew(value, [2])\nenddef\n", ""},
		{"insert", "vim9script\ndef Func()\n  const value = [1]\n  insert(value, 2)\nenddef\n", ""},
		{"top-level Vim9", "vim9script\nconst value = [1]\nadd(value, 2)\n", ""},
		{"Legacy function", "function Func()\n  const value = [1]\n  add(value, 2)\nendfunction\n", ""},
		{"literal", "vim9script\ndef Func()\n  add([1], 2)\nenddef\n", ""},
		{"unknown", "vim9script\ndef Func(value: any)\n  add(value, 2)\nenddef\n", ""},
		{"first argument type mismatch", "vim9script\ndef Func()\n  const value = 1\n  add(value, 2)\nenddef\n", "vim/E1013"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1307" {
					t.Fatalf("guard unexpectedly received E1307: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1325MissingAggregateMethodDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, method, class string }{
		{"class receiver", "vim9script\nclass A\nendclass\nA.Missing()\n", "Missing", "A"},
		{"abstract constructor", "vim9script\nabstract class A\nendclass\nA.new()\n", "new", "A"},
		{"protected constructor hides default", "vim9script\nclass A\n  def _new()\n  enddef\nendclass\nA.new()\n", "new", "A"},
		{"enum default constructor", "vim9script\nenum Fruit\n  Apple\nendenum\nFruit.new()\n", "new", "Fruit"},
		{"enum constructor", "vim9script\nenum Fruit\n  Apple\nendenum\nFruit.newFruit()\n", "newFruit", "Fruit"},
		{"enum missing regular method", "vim9script\nenum Fruit\n  Apple\nendenum\nFruit.Missing()\n", "Missing", "Fruit"},
		{"object receiver", "vim9script\nclass A\nendclass\nvar a = A.new()\na.Missing()\n", "Missing", "A"},
		{"typed parameter", "vim9script\nclass A\nendclass\ndef Func(a: A)\n  a.Missing()\nenddef\n", "Missing", "A"},
		{"generic object call", "vim9script\nclass A\nendclass\ndef Func(a: A)\n  a.Bar<number, string>()\nenddef\n", "Bar", "A"},
		{"this object method", "vim9script\nclass A\n  def Check()\n    this.Missing()\n  enddef\nendclass\n", "Missing", "A"},
		{"super ignores static parent method", "vim9script\nclass A\n  static def ParentOnly()\n  enddef\nendclass\nclass B extends A\n  def Check()\n    super.ParentOnly()\n  enddef\nendclass\n", "ParentOnly", "B"},
		{"class methods do not inherit", "vim9script\nclass A\n  static def ParentOnly()\n  enddef\nendclass\nclass B extends A\nendclass\nB.ParentOnly()\n", "ParentOnly", "B"},
		{"protected static does not inherit through class", "vim9script\nclass A\n  static def _Foo()\n  enddef\nendclass\nclass C extends A\nendclass\nC._Foo()\n", "_Foo", "C"},
		{"protected static does not inherit through object", "vim9script\nclass A\n  static def _Foo()\n  enddef\nendclass\nclass C extends A\nendclass\nvar c = C.new()\nc._Foo()\n", "_Foo", "C"},
		{"class alias", "vim9script\nclass A\nendclass\ntype Alias = A\nAlias.Missing()\n", "Missing", "A"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1325" {
					got = append(got, diagnostic)
				}
				if strings.Contains(test.name, "protected static") && diagnostic.Code == "vim/E1366" {
					t.Fatalf("non-inherited protected class method reported E1366: %#v", result.Diagnostics)
				}
			}
			message := `Method "` + test.method + `" not found in class "` + test.class + `"`
			if len(got) != 1 || got[0].Message != message || file.Text(got[0].Span) != test.method {
				t.Fatalf("E1325 diagnostics = %#v", result.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nclass A\nendclass\nA.new()\n",
		"vim9script\nclass A\n  def new()\n  enddef\nendclass\nA.new()\n",
		"vim9script\nclass A\n  static var Fn: func\nendclass\nA.Fn()\n",
		"vim9script\nclass A\n  var Fn: func\nendclass\nclass B extends A\nendclass\nvar b = B.new()\nb.Fn()\n",
		"vim9script\nenum Fruit\n  Apple\n  static def Values()\n  enddef\nendenum\nFruit.Values()\n",
		"vim9script\nclass A\n  static def Foo()\n  enddef\nendclass\nA.Foo()\n",
		"vim9script\nclass A\n  def Foo()\n  enddef\nendclass\nclass B extends A\nendclass\nvar b = B.new()\nb.Foo()\n",
		"vim9script\nclass A\n  def Foo()\n  enddef\nendclass\nA.Foo()\n",
		"vim9script\nclass A\n  static def Foo()\n  enddef\nendclass\nvar a = A.new()\na.Foo()\n",
		"vim9script\nclass A\n  static def _Foo()\n  enddef\nendclass\nA._Foo()\n",
		"vim9script\nvar value: any\nvalue.Missing()\n",
		"function Legacy()\n  var a = 1\n  a.Missing()\nendfunction\n",
		"vim9script\nclass A\nendclass\nvar a = A.new()\nvar value = a.Missing\n",
		"vim9script\nclass A\nendclass\nvar a = A.new()\na.Missing(\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1325" {
				t.Fatalf("guard unexpectedly received E1325: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1326MissingObjectVariableDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, member, class string }{
		{"constructor this", "vim9script\nclass A\n  def new()\n    var value = this.state\n  enddef\nendclass\n", "state", "A"},
		{"script read", "vim9script\nclass A\nendclass\nvar a = A.new()\nvar value = a.bar\n", "bar", "A"},
		{"assignment", "vim9script\nclass A\nendclass\nvar a = A.new()\na.four = 4\n", "four", "A"},
		{"typed parameter", "vim9script\nclass A\nendclass\ndef Func(a: A)\n  a.missing = 1\nenddef\n", "missing", "A"},
		{"inherited object", "vim9script\nclass A\nendclass\nclass B extends A\nendclass\nvar b = B.new()\nvar value = b.missing\n", "missing", "B"},
		{"inherited static variable", "vim9script\nclass A\n  static var token: number\nendclass\nclass B extends A\nendclass\nvar b = B.new()\nvar value = b.token\n", "token", "B"},
		{"inherited static method reference", "vim9script\nclass A\n  static def Build()\n  enddef\nendclass\nclass B extends A\nendclass\nvar b = B.new()\nvar Callback = b.Build\n", "Build", "B"},
		{"super skips parent static", "vim9script\nclass A\n  static var foo: number\nendclass\nclass B extends A\n  def Check()\n    var value = super.foo\n  enddef\nendclass\n", "foo", "B"},
		{"nested typed member", "vim9script\nclass Inner\nendclass\nclass Outer\n  var inner: Inner\nendclass\nvar outer = Outer.new()\nvar value = outer.inner.missing\n", "missing", "Inner"},
		{"enum object", "vim9script\nenum Fruit\n  Apple\nendenum\nvar fruit: Fruit = Fruit.Apple\nvar value = fruit.missing\n", "missing", "Fruit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1326" {
					got = append(got, diagnostic)
				}
			}
			message := `Variable "` + test.member + `" not found in object "` + test.class + `"`
			if len(got) != 1 || got[0].Message != message || file.Text(got[0].Span) != test.member {
				t.Fatalf("E1326 diagnostics = %#v", result.Diagnostics)
			}
		})
	}

	for _, test := range []struct{ name, source string }{
		{"direct and inherited object variables", "vim9script\nclass A\n  var one: number\nendclass\nclass B extends A\nendclass\nvar b = B.new()\nvar value = b.one\n"},
		{"super object variable", "vim9script\nclass A\n  var one: number\nendclass\nclass B extends A\n  def Check()\n    var value = super.one\n  enddef\nendclass\n"},
		{"direct static members", "vim9script\nclass A\n  static var count: number\n  static def Build()\n  enddef\nendclass\nvar a = A.new()\nvar count = a.count\nvar Callback = a.Build\n"},
		{"protected and readonly object variables", "vim9script\nclass A\n  var _hidden: number\n  final fixed: number\nendclass\nvar a = A.new()\nvar hidden = a._hidden\na.fixed = 1\n"},
		{"enum object fields", "vim9script\nenum Fruit\n  Apple\n  var color: string\nendenum\nvar fruit = Fruit.Apple\nvar value = fruit.name .. fruit.ordinal .. fruit.color\n"},
		{"object method reference", "vim9script\nclass A\n  def Check()\n  enddef\nendclass\nvar a = A.new()\nvar Callback = a.Check\n"},
		{"method calls own E1325", "vim9script\nclass A\nendclass\nvar a = A.new()\na.Missing()\n"},
		{"class receiver", "vim9script\nclass A\nendclass\nvar value = A.Missing\n"},
		{"unknown and Legacy", "vim9script\nvar value: any\nvar result = value.missing\nlegacy var legacy = value.missing\n"},
		{"incomplete", "vim9script\nclass A\nendclass\nvar a = A.new()\nvar value = a.\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, diagnostic := range Analyze(syntax.Parse(test.source)).Diagnostics {
				if diagnostic.Code == "vim/E1326" {
					t.Fatalf("guard unexpectedly received E1326: %#v\n%s", diagnostic, test.source)
				}
			}
		})
	}
}

func TestAnalyzeE1328ConstructorDefaultValueDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, tail string }{
		{"class string default", "vim9script\nclass A\n  def new(this.val = 'a')\n  enddef\nendclass\n", " = 'a'"},
		{"no space", "vim9script\nclass A\n  def new(this.val='a')\n  enddef\nendclass\n", "='a'"},
		{"new prefix", "vim9script\nclass A\n  def newNamed(this.value = 1)\n  enddef\nendclass\n", " = 1"},
		{"enum constructor", "vim9script\nenum Result\n  Ok\n  def newValue(this.value = 1)\n  enddef\nendenum\n", " = 1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1328" {
					got = append(got, diagnostic)
				}
			}
			message := "Constructor default value must be v:none: " + test.tail
			if len(got) != 1 || got[0].Message != message || file.Text(got[0].Span) != test.tail {
				t.Fatalf("E1328 diagnostics = %#v", result.Diagnostics)
			}
		})
	}

	for _, test := range []struct{ name, source string }{
		{"top-level def", "vim9script\ndef new(this.val = 'a')\nenddef\n"},
		{"non-constructor", "vim9script\nclass A\n  def Value(this.val = 'a')\n  enddef\nendclass\n"},
		{"underscore constructor", "vim9script\nclass A\n  def _new(this.val = 'a')\n  enddef\nendclass\n"},
		{"ordinary parameter", "vim9script\nclass A\n  def new(value: string = 'a')\n  enddef\nendclass\n"},
		{"typed target", "vim9script\nclass A\n  def new(this.val: string = 'a')\n  enddef\nendclass\n"},
		{"v none", "vim9script\nclass A\n  def new(this.val = v:none)\n  enddef\nendclass\n"},
		{"v none prefix", "vim9script\nclass A\n  def new(this.val = v:none + 1)\n  enddef\nendclass\n"},
		{"no default", "vim9script\nclass A\n  def new(this.val)\n  enddef\nendclass\n"},
		{"incomplete default", "vim9script\nclass A\n  def new(this.val = )\n  enddef\nendclass\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, diagnostic := range Analyze(syntax.Parse(test.source)).Diagnostics {
				if diagnostic.Code == "vim/E1328" {
					t.Fatalf("guard unexpectedly received E1328: %#v\n%s", diagnostic, test.source)
				}
			}
		})
	}
}

func TestAnalyzeE1330InvalidVoidValueTypeDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, span string }{
		{"script variable", "vim9script\nvar value: void\n", "void"},
		{"def local", "vim9script\ndef Func()\n  var value: void\nenddef\n", "void"},
		{"function argument", "vim9script\ndef Func(value: void)\nenddef\n", "void"},
		{"legacy root def argument", "def Func(value: void)\nenddef\n", "void"},
		{"initializer", "vim9script\nvar value: void = 1\n", "void"},
		{"list", "vim9script\nvar value: list<void>\n", "void"},
		{"tuple", "vim9script\nvar value: tuple<void, number>\n", "void"},
		{"dict", "vim9script\nvar value: dict<void>\n", "void"},
		{"lambda argument", "vim9script\nvar Callback = (value: void) => value\n", "void"},
		{"class member", "vim9script\nclass A\n  var value: void\nendclass\n", "void"},
		{"for binding", "vim9script\nfor value: void in []\nendfor\n", "void"},
		{"generic call", "vim9script\nFn<void>()\n", "void"},
		{"function argument type", "vim9script\nvar callback: func(void): void\n", "void"},
		{"type alias", "vim9script\ntype MyType = void\nvar value: MyType\n", "MyType"},
		{"type alias member", "vim9script\ntype MyType = list<void>\n", "void"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1330" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Invalid type used in variable declaration: void" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1330 diagnostics = %#v", result.Diagnostics)
			}
		})
	}

	for _, test := range []struct{ name, source string }{
		{"def return void", "vim9script\ndef Func(): void\nenddef\n"},
		{"lambda return void", "vim9script\nvar Callback = (): void => {}\n"},
		{"function type return void", "vim9script\nvar callback: func(): void\n"},
		{"unused alias", "vim9script\ntype MyType = void\n"},
		{"alias function return", "vim9script\ntype MyType = void\ndef Func(): MyType\nenddef\n"},
		{"valid type", "vim9script\nvar value: list<number>\n"},
		{"Legacy", "let value: void = 1\n"},
		{"legacy function", "vim9script\nfunction Foo(value: void)\nendfunction\n"},
		{"missing type", "vim9script\nvar value: \n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, diagnostic := range Analyze(syntax.Parse(test.source)).Diagnostics {
				if diagnostic.Code == "vim/E1330" {
					t.Fatalf("guard unexpectedly received E1330: %#v\n%s", diagnostic, test.source)
				}
			}
		})
	}
}

func TestAnalyzeE1256BuiltinCallbackArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, span string }{
		{"sort zero script", "vim9script\nsort(['a', 'b'], 0)\n", "0"},
		{"sort one script", "vim9script\nsort(['a', 'b'], 1)\n", "1"},
		{"sort zero def", "vim9script\ndef Func()\n  sort(['a', 'b'], 0)\nenddef\n", "0"},
		{"sort one def", "vim9script\ndef Func()\n  sort(['a', 'b'], 1)\nenddef\n", "1"},
		{"filter number def", "vim9script\ndef Func()\n  filter([1], 1)\nenddef\n", "1"},
		{"map number def", "vim9script\ndef Func()\n  map([1], 1)\nenddef\n", "1"},
		{"block lambda", "vim9script\nvar Callback = () => {\n  map([1], 1)\n}\n", "1"},
		{"foreach compiled", "vim9script\ndef Func()\n  foreach([1], 1)\nenddef\n", "1"},
		{"uniq script", "vim9script\nuniq([1], 1)\n", "1"},
		{"indexof script", "vim9script\nindexof([1], 1)\n", "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1256" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1256 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "String or function required for argument 2" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1256 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct{ name, source, want string }{
		{"top-level filter keeps E1024", "vim9script\nfilter([1], 1)\n", "vim/E1024"},
		{"top-level map keeps E1024", "vim9script\nmap([1], 1)\n", "vim/E1024"},
		{"valid string function partial unknown", "vim9script\nsort([], '')\nsort([], (left, right) => 0)\nsort([], function('len', ['x']))\nsort([], Unknown)\n", ""},
		{"function signature keeps E176", "vim9script\ndef Func()\n  filter([1], () => true)\nenddef\n", "vim/E176"},
		{"arity ownership", "vim9script\nindexof([1])\n", "vim/E119"},
		{"Legacy", "let value = sort([1], 1)\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1256" {
					t.Fatalf("guard unexpectedly received E1256: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1216DigraphSetlistDiagnostics(t *testing.T) {
	const message = "digraph_setlist() argument must be a list of lists with two items"
	for _, test := range []struct{ name, source, span string }{
		{"script outer non-list", "vim9script\ndigraph_setlist('bad')\n", "'bad'"},
		{"script short inner list", "vim9script\ndigraph_setlist([['aa']])\n", "[['aa']]"},
		{"def non-list inner", "vim9script\ndef Func()\n  digraph_setlist([1])\nenddef\n", "[1]"},
		{"def null list inner", "vim9script\ndef Func()\n  digraph_setlist([null_list])\nenddef\n", "[null_list]"},
		{"parenthesized outer", "vim9script\ndigraph_setlist((['aa']))\n", "(['aa'])"},
		{"method receiver", "vim9script\n['aa']->digraph_setlist()\n", "['aa']"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1216" {
					got = append(got, diagnostic)
				}
				if (diagnostic.Code == "vim/E1211" || diagnostic.Code == "vim/E1013") && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1216 source retained %s: %#v", diagnostic.Code, result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1216 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"compiled outer non-list", "vim9script\ndef Func()\n  digraph_setlist('bad')\nenddef\n", "vim/E1013"},
		{"valid nested literal", "vim9script\ndigraph_setlist([['aa', 'き'], ['bb', 'く']])\n", ""},
		{"empty outer list", "vim9script\ndigraph_setlist([])\n", ""},
		{"null outer list", "vim9script\ndigraph_setlist(null_list)\n", ""},
		{"dynamic list", "vim9script\nvar values: list<any> = []\ndigraph_setlist(values)\n", ""},
		{"dynamic inner list", "vim9script\nvar pair: list<string> = []\ndigraph_setlist([pair])\n", ""},
		{"unrelated list string checker", "vim9script\ncomplete_info([1])\n", "vim/E1013"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1216" {
					t.Fatalf("guard unexpectedly received E1216: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1217ChannelOrJobBuiltinArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, message, span string }{
		{"first argument", "vim9script\nch_close(1)\n", "Channel or Job required for argument 1", "1"},
		{"later argument", "vim9script\nch_log('msg', 1)\n", "Channel or Job required for argument 2", "1"},
		{"method argument", "vim9script\n1->ch_close()\n", "Channel or Job required for argument 1", "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1217" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1217 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1217 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"compiled def", "vim9script\ndef Func()\n  ch_close(1)\nenddef\n", "vim/E1013"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  ch_close(1)\n}\n", "vim/E1013"},
		{"null channel", "vim9script\nch_close(null_channel)\n", ""},
		{"typed job", "vim9script\nvar handle: job\nch_close(handle)\n", ""},
		{"unknown", "vim9script\nch_close(Unknown)\n", ""},
		{"Legacy", "let value = ch_close(1)\n", ""},
		{"arg job", "vim9script\njob_status(1)\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1217" {
					t.Fatalf("guard unexpectedly received E1217: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1218JobBuiltinArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, message, span string }{
		{"first argument", "vim9script\njob_status('bad')\n", "Job required for argument 1", "'bad'"},
		{"method receiver", "vim9script\n1->job_status()\n", "Job required for argument 1", "1"},
		{"channel is incompatible", "vim9script\njob_status(null_channel)\n", "Job required for argument 1", "null_channel"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1218" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1218 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1218 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"compiled def", "vim9script\ndef Func()\n  job_status('bad')\nenddef\n", "vim/E1013"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  job_status('bad')\n}\n", "vim/E1013"},
		{"null job", "vim9script\njob_status(null_job)\n", ""},
		{"typed job", "vim9script\nvar handle: job\njob_status(handle)\n", ""},
		{"unknown", "vim9script\njob_status(Unknown)\n", ""},
		{"Legacy", "let value = job_status('bad')\n", ""},
		{"channel or job", "vim9script\nch_close(1)\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1218" {
					t.Fatalf("guard unexpectedly received E1218: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1219FloatOrNumberBuiltinArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, message, span string }{
		{"first argument", "vim9script\nabs('bad')\n", "Float or Number required for argument 1", "'bad'"},
		{"later argument", "vim9script\natan2(1.0, 'bad')\n", "Float or Number required for argument 2", "'bad'"},
		{"method receiver", "vim9script\n'bad'->abs()\n", "Float or Number required for argument 1", "'bad'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1219" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1219 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1219 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"compiled def", "vim9script\ndef Func()\n  abs('bad')\nenddef\n", "vim/E1013"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  abs('bad')\n}\n", "vim/E1013"},
		{"float", "vim9script\nabs(1.0)\n", ""},
		{"number", "vim9script\nabs(1)\n", ""},
		{"unknown", "vim9script\nabs(Unknown)\n", ""},
		{"Legacy", "let value = abs('bad')\n", ""},
		{"arg number", "vim9script\nand([], 1)\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1219" {
					t.Fatalf("guard unexpectedly received E1219: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1220StringOrNumberBuiltinArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, message, span string }{
		{"string or number", "vim9script\nassert_fails([])\n", "String or Number required for argument 1", "[]"},
		{"buffer", "vim9script\nappendbufline([], 0, 'x')\n", "String or Number required for argument 1", "[]"},
		{"line number", "vim9script\nappend([], 'x')\n", "String or Number required for argument 1", "[]"},
		{"later dictionary remove", "vim9script\nremove({key: 1}, [])\n", "String or Number required for argument 2", "[]"},
		{"method argument", "vim9script\n['x']->append([])\n", "String or Number required for argument 1", "[]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1220" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1220 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1220 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"compiled def", "vim9script\ndef Func()\n  assert_fails([])\nenddef\n", "vim/E1013"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  assert_fails([])\n}\n", "vim/E1013"},
		{"string", "vim9script\nappend('$', 'x')\n", ""},
		{"number", "vim9script\nappend(0, 'x')\n", ""},
		{"unknown", "vim9script\nappend(Unknown, 'x')\n", ""},
		{"Legacy", "let value = append([], 'x')\n", ""},
		{"list remove keeps E1210", "vim9script\nremove([1], 'x')\n", "vim/E1210"},
		{"buffer or dict union", "vim9script\ngetbufinfo(true)\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1220" {
					t.Fatalf("guard unexpectedly received E1220: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1221StringOrBlobBuiltinArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, message, span string }{
		{"first argument", "vim9script\nsha256(100)\n", "String or Blob required for argument 1", "100"},
		{"later argument", "vim9script\nch_evalraw(null_channel, 1)\n", "String or Blob required for argument 2", "1"},
		{"method receiver", "vim9script\n1->sha256()\n", "String or Blob required for argument 1", "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1221" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1221 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1221 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"compiled def", "vim9script\ndef Func()\n  sha256(100)\nenddef\n", "vim/E1013"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  sha256(100)\n}\n", "vim/E1013"},
		{"string", "vim9script\nsha256('text')\n", ""},
		{"blob", "vim9script\nsha256(0z12)\n", ""},
		{"unknown", "vim9script\nsha256(Unknown)\n", ""},
		{"Legacy", "let value = sha256(100)\n", ""},
		{"string or number", "vim9script\nassert_fails([])\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1221" {
					t.Fatalf("guard unexpectedly received E1221: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1222StringOrListBuiltinArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, message, span string }{
		{"string or list", "vim9script\nmatch(1, 'x')\n", "String or List required for argument 1", "1"},
		{"list string", "vim9script\nexecute(1)\n", "String or List required for argument 1", "1"},
		{"later argument", "vim9script\nassert_fails('x', true)\n", "String or List required for argument 2", "true"},
		{"method receiver", "vim9script\n1->match('x')\n", "String or List required for argument 1", "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1222" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1222 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1222 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"compiled def", "vim9script\ndef Func()\n  match(1, 'x')\nenddef\n", "vim/E1013"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  match(1, 'x')\n}\n", "vim/E1013"},
		{"string", "vim9script\nmatch('text', 'x')\n", ""},
		{"list", "vim9script\nmatch(['text'], 'x')\n", ""},
		{"unknown", "vim9script\nmatch(Unknown, 'x')\n", ""},
		{"Legacy", "let value = match(1, 'x')\n", ""},
		{"list element mismatch", "vim9script\nexecute([1])\n", "vim/E1013"},
		{"cursor checker", "vim9script\ncursor(0z10)\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1222" {
					t.Fatalf("guard unexpectedly received E1222: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1223StringOrDictionaryBuiltinArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, message, span string }{
		{"dictionary or string", "vim9script\ncomplete_add([])\n", "String or Dictionary required for argument 1", "[]"},
		{"string or dictionary", "vim9script\nmapset([], true, {})\n", "String or Dictionary required for argument 1", "[]"},
		{"method receiver", "vim9script\n[]->complete_add()\n", "String or Dictionary required for argument 1", "[]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1223" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1223 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1223 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"compiled def", "vim9script\ndef Func()\n  complete_add([])\nenddef\n", "vim/E1013"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  complete_add([])\n}\n", "vim/E1013"},
		{"string", "vim9script\ncomplete_add('item')\n", ""},
		{"dictionary", "vim9script\ncomplete_add({word: 'item'})\n", ""},
		{"unknown", "vim9script\ncomplete_add(Unknown)\n", ""},
		{"Legacy", "let value = complete_add([])\n", ""},
		{"buffer or dictionary union", "vim9script\ngetbufinfo(true)\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1223" {
					t.Fatalf("guard unexpectedly received E1223: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1224StringNumberOrListBuiltinArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, message, span string }{
		{"cursor", "vim9script\ncursor({})\n", "String, Number or List required for argument 1", "{}"},
		{"later argument", "vim9script\nsystem('echo', {})\n", "String, Number or List required for argument 2", "{}"},
		{"setbufline", "vim9script\nsetbufline(1, 1, {})\n", "String, Number or List required for argument 3", "{}"},
		{"method receiver", "vim9script\n{}->setbufline(1, 1)\n", "String, Number or List required for argument 3", "{}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1224" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1224 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1224 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"compiled def", "vim9script\ndef Func()\n  cursor({})\nenddef\n", "vim/E1013"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  cursor({})\n}\n", "vim/E1013"},
		{"string", "vim9script\nsystem('echo', 'input')\n", ""},
		{"number", "vim9script\nsystem('echo', 1)\n", ""},
		{"list", "vim9script\nsystem('echo', ['input'])\n", ""},
		{"unknown", "vim9script\nsystem('echo', Unknown)\n", ""},
		{"Legacy", "let value = cursor({})\n", ""},
		{"buffer or dictionary union", "vim9script\ngetbufinfo(true)\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1224" {
					t.Fatalf("guard unexpectedly received E1224: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1225StringListTupleOrDictionaryBuiltinArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, span string }{
		{"ordinary", "vim9script\ncount(1, 1)\n", "1"},
		{"method receiver", "vim9script\n1->count(1)\n", "1"},
		{"compiled def", "vim9script\ndef Func()\n  count(1, 1)\nenddef\n", "1"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  count(1, 1)\n}\n", "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1225" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1225 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "String, List, Tuple or Dictionary required for argument 1" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1225 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"string", "vim9script\ncount('text', 't')\n", ""},
		{"list", "vim9script\ncount([1], 1)\n", ""},
		{"tuple", "vim9script\ncount((1, 2), 1)\n", ""},
		{"dictionary", "vim9script\ncount({key: 1}, 1)\n", ""},
		{"unknown", "vim9script\ncount(Unknown, 1)\n", ""},
		{"Legacy", "let value = count(1, 1)\n", ""},
		{"list tuple dictionary blob string", "vim9script\nitems(1)\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1225" {
					t.Fatalf("guard unexpectedly received E1225: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1226ListOrBlobBuiltinArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, span string }{
		{"modifiable checker", "vim9script\nadd({}, 1)\n", "{}"},
		{"non-modifiable checker", "vim9script\ninsert({}, 1)\n", "{}"},
		{"method receiver", "vim9script\n{}->add(1)\n", "{}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1226" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1226 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "List or Blob required for argument 1" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1226 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"compiled def", "vim9script\ndef Func()\n  add({}, 1)\nenddef\n", "vim/E1013"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  add({}, 1)\n}\n", "vim/E1013"},
		{"list", "vim9script\nadd([], 1)\n", ""},
		{"blob", "vim9script\nadd(0z12, 1)\n", ""},
		{"unknown", "vim9script\nadd(Unknown, 1)\n", ""},
		{"Legacy", "let value = add({}, 1)\n", ""},
		{"list dictionary blob union", "vim9script\nextend(1, [])\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1226" {
					t.Fatalf("guard unexpectedly received E1226: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1228ListDictionaryOrBlobBuiltinArgumentDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, span string }{
		{"extend", "vim9script\nextend('bad', [])\n", "'bad'"},
		{"remove", "vim9script\nremove('bad', 1)\n", "'bad'"},
		{"method receiver", "vim9script\n'bad'->extend([])\n", "'bad'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1228" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1228 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "List, Dictionary or Blob required for argument 1" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1228 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"compiled def", "vim9script\ndef Func()\n  extend('bad', [])\nenddef\n", "vim/E1013"},
		{"compiled lambda", "vim9script\nvar Callback = () => {\n  extend('bad', [])\n}\n", "vim/E1013"},
		{"list", "vim9script\nextend([], [])\n", ""},
		{"dictionary", "vim9script\nextend({}, {})\n", ""},
		{"blob", "vim9script\nextend(0z12, 0z34)\n", ""},
		{"unknown", "vim9script\nextend(Unknown, [])\n", ""},
		{"Legacy", "let value = extend('bad', [])\n", ""},
		{"string union", "vim9script\nfilter('text', (_, _) => true)\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1228" {
					t.Fatalf("guard unexpectedly received E1228: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1229CompiledMemberAccessDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, message, span string }{
		{"official string def", "vim9script\ndef Func()\n  var text = ''\n  var value = text.memb\nenddef\n", `Expected dictionary for using key "memb", but got string`, "text.memb"},
		{"nested parenthesized list", "vim9script\ndef Func()\n  var value = ([1]).first.second\nenddef\n", `Expected dictionary for using key "first", but got list<number>`, "([1]).first"},
		{"block lambda", "vim9script\nvar Callback = () => {\n  var value = ''.memb\n}\n", `Expected dictionary for using key "memb", but got string`, "''.memb"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1229" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1229 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"top-level keeps E488", "vim9script\nvar text = ''\nvar value = text.memb\n", "vim/E488"},
		{"dictionary", "vim9script\ndef Func()\n  var values = {}\n  var value = values.key\nenddef\n", ""},
		{"any", "vim9script\ndef Func()\n  var value: any = 1\n  var item = value.key\nenddef\n", ""},
		{"unknown", "vim9script\ndef Func()\n  var item = Unknown.key\nenddef\n", ""},
		{"class object", "vim9script\nclass A\n  var key: number\nendclass\ndef Func()\n  var object = A.new()\n  var value = object.key\nenddef\n", ""},
		{"enum selector", "vim9script\nenum E\n  Value\nendenum\ndef Func()\n  var value = E.Value\nenddef\n", ""},
		{"arrow method", "vim9script\ndef Func()\n  var value = 'text'->len()\nenddef\n", ""},
		{"Legacy function", "function Func()\n  let value = 'text'.memb\nendfunction\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1229" {
					t.Fatalf("guard unexpectedly received E1229: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}

	file := syntax.Parse("vim9script\ndef Func()\n  var text = ''\n  var value = text.\nenddef\n")
	if len(file.Diagnostics) == 0 {
		t.Fatal("incomplete member source lacks parser diagnostics")
	}
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vim/E1229" {
			t.Fatalf("incomplete member unexpectedly received E1229: %#v", diagnostic)
		}
	}
}

func TestAnalyzeE1232ExistsCompiledLiteralDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, span string }{
		{"official number", "vim9script\ndef Func()\n  var value = exists_compiled(10)\nenddef\n", "10"},
		{"official identifier", "vim9script\ndef Func()\n  var value = exists_compiled(v:progname)\nenddef\n", "v:progname"},
		{"string expression", "vim9script\ndef Func()\n  var value = exists_compiled('a' .. 'b')\nenddef\n", "'a' .. 'b'"},
		{"parenthesized literal", "vim9script\ndef Func()\n  var value = exists_compiled(('feature'))\nenddef\n", "('feature')"},
		{"missing argument", "vim9script\ndef Func()\n  var value = exists_compiled()\nenddef\n", "exists_compiled"},
		{"extra argument", "vim9script\ndef Func()\n  var value = exists_compiled('feature', 'extra')\nenddef\n", "'extra'"},
		{"method form", "vim9script\ndef Func()\n  var value = 'feature'->exists_compiled()\nenddef\n", "exists_compiled"},
		{"block lambda", "vim9script\nvar Callback = () => {\n  var value = exists_compiled(10)\n}\n", "10"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1232" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1232 source retained E1013: %#v", result.Diagnostics)
				}
				if diagnostic.Code == "vim/E118" || diagnostic.Code == "vim/E119" {
					t.Fatalf("E1232 source retained an arity diagnostic: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "Argument of exists_compiled() must be a literal string" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1232 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []string{
		"vim9script\ndef Func()\n  var single = exists_compiled('feature')\n  var double = exists_compiled(\"feature\")\nenddef\n",
		"vim9script\nvar value = exists_compiled(10)\n",
		"vim9script\ndef Func()\n  legacy echo exists_compiled(10)\nenddef\n",
	} {
		result := Analyze(syntax.Parse(test))
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1232" {
				t.Fatalf("guard unexpectedly received E1232: %#v", result.Diagnostics)
			}
		}
	}
}

func TestAnalyzeE1233ExistsCompiledRuntimeDiagnostics(t *testing.T) {
	const message = "exists_compiled() can only be used in a :def function"
	for _, test := range []struct {
		name   string
		source string
	}{
		{"official number", "vim9script\nvar value = exists_compiled(10)\n"},
		{"official identifier", "vim9script\nvar value = exists_compiled(v:progname)\n"},
		{"top-level literal", "vim9script\nvar value = exists_compiled('feature')\n"},
		{"Legacy script", "let value = exists_compiled('feature')\n"},
		{"legacy command in def", "vim9script\ndef Func()\n  legacy echo exists_compiled('feature')\nenddef\n"},
		{"method receiver", "vim9script\nvar value = 'feature'->exists_compiled()\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1233" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1013" {
					t.Fatalf("E1233 source retained E1013: %#v", result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != message || file.Text(got[0].Span) != "exists_compiled" {
				t.Fatalf("E1233 diagnostics = %#v", got)
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"compiled literal", "vim9script\ndef Func()\n  var value = exists_compiled('feature')\nenddef\n", ""},
		{"compiled invalid", "vim9script\ndef Func()\n  var value = exists_compiled(10)\nenddef\n", "vim/E1232"},
		{"block lambda invalid", "vim9script\nvar Callback = () => {\n  var value = exists_compiled(10)\n}\n", "vim/E1232"},
		{"scoped call", "vim9script\nvar value = g:exists_compiled(10)\n", ""},
		{"dynamic call", "vim9script\nvar Callback = (value) => value\nvar value = Callback(10)\n", ""},
		{"missing runtime argument", "vim9script\nvar value = exists_compiled()\n", "vim/E119"},
		{"extra runtime argument", "vim9script\nvar value = exists_compiled('one', 'two')\n", "vim/E118"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1233" {
					t.Fatalf("guard unexpectedly received E1233: %#v", result.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", result.Diagnostics, test.want)
			}
		})
	}
}

func TestAnalyzeE1213ImportedItemRedefinitionDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, span string }{
		{"var", "vim9script\nimport './item.vim' as Item\nvar Item = 1\n", "Item"},
		{"root def", "vim9script\nimport './item.vim' as Item\ndef Item()\nenddef\n", "Item"},
		{"const destructuring and control", "vim9script\nimport './item.vim' as Item\nconst Item = 1\nvar [Item, other] = [1, 2]\nif true\n  var Item = 3\nendif\n", "Item"},
		{"cross dialect function", "vim9script\nimport './item.vim' as Item\nlegacy function Item()\nendfunction\n", "Item"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1213" {
					got = append(got, diagnostic)
				}
				if (diagnostic.Code == "vim/E1041" || diagnostic.Code == "vim/E1017" || diagnostic.Code == "vim/E1073") && file.Text(diagnostic.Span) == test.span {
					t.Fatalf("E1213 source retained %s: %#v", diagnostic.Code, result.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != `Redefining imported item "`+test.span+`"` || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1213 diagnostics = %#v", got)
			}
		})
	}
	for _, source := range []string{
		"vim9script\nvar Item = 1\nimport './item.vim' as Item\nvar Item = 2\n",
		"import './item.vim' as Item\nlet Item = 1\n",
		"vim9script\nimport './item.vim' as Item\ndef Outer()\n  var Item = 1\nenddef\nvar Callback = () => {\n  var Item = 1\n}\n",
		"vim9script\nimport './item.vim' as Item\nItem = 1\necho Item\nclass Item\nendclass\ntype Item = number\nvar Other = 1\n",
		"vim9script\ndef Func()\n  import './item.vim' as Item\n  var Item = 1\nenddef\n",
	} {
		result := Analyze(syntax.Parse(source))
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1213" {
				t.Fatalf("guard unexpectedly received E1213: %#v", result.Diagnostics)
			}
		}
	}
}

func TestAnalyzeCompiledIndexReceiverDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		want         []string
	}{
		{
			name: "Vim9 def number float and slice",
			source: "vim9script\ndef Func()\n  var hex = 0xff[1]\n  var float = 0.7[1]\n" +
				"  var number = 1234[3]\n  var grouped = (1234)[3]\n  var slice = 1.5[1 : 2]\nenddef\n",
			want: []string{"0xff", "0.7", "1234", "(1234)", "1.5"},
		},
		{
			name:   "Legacy-root def",
			source: "def Func()\n  var value = 1234[3]\nenddef\n",
			want:   []string{"1234"},
		},
		{
			name:   "Vim9 lambda",
			source: "vim9script\nvar Callback = () => 1234[3]\n",
			want:   []string{"1234"},
		},
		{
			name:   "valid unknown and incomplete receivers",
			source: "vim9script\ndef Func()\n  var value: any\n  var a = value[1]\n  var b = 'x'[0]\n  var c = [1][0]\n  var d = {}['x']\n  var tuple = (1, 2)[0]\n  var blob = 0z12[0]\n  var e = 1[\nenddef\n",
		},
		{
			name: "typealias and other invalid categories stay on their own paths",
			source: "vim9script\ntype NumberAlias = number\ntype FloatAlias = float\ndef Func()\n" +
				"  var direct = NumberAlias[0]\n  var grouped = (FloatAlias)[0]\n  var funcref = function('len')[0]\n" +
				"  var partial = null_partial[0]\n  var boolean = true[0]\n  var special = v:none[0]\nenddef\n",
		},
		{
			name:   "top-level Vim9 keeps E1062 and E806",
			source: "vim9script\nvar number = 1234[3]\nvar float = 0.7[1]\n",
		},
		{
			name:   "Legacy-root script and function are excluded",
			source: "let script = 1234[3]\nfunction Legacy()\n  let local = 0.7[1]\nendfunction\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			e1062, e806 := 0, 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1107" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1062" {
					e1062++
				}
				if diagnostic.Code == "vim/E806" {
					e806++
				}
				if diagnostic.Code == "vim/E1062" || diagnostic.Code == "vim/E806" {
					if len(test.want) > 0 {
						t.Fatalf("compiled source retained %s: %#v", diagnostic.Code, diagnostic)
					}
				}
			}
			if len(got) != len(test.want) {
				t.Fatalf("E1107 diagnostics = %#v, want %q", got, test.want)
			}
			if test.name == "top-level Vim9 keeps E1062 and E806" && (e1062 != 1 || e806 != 1) {
				t.Fatalf("top-level diagnostics E1062=%d E806=%d; all diagnostics = %#v", e1062, e806, result.Diagnostics)
			}
			for index, diagnostic := range got {
				if diagnostic.Message != "String, List, Dict or Blob required" || file.Text(diagnostic.Span) != test.want[index] {
					t.Fatalf("E1107[%d] = %#v on %q, want %q", index, diagnostic, file.Text(diagnostic.Span), test.want[index])
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

func TestAnalyzeUnreachableCodeDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		want         []struct{ text, message string }
	}{
		{
			name: "official all-return if branches",
			source: "vim9script\ndef Func(): number\n  if true\n    return 1\n" +
				"  else\n    return 2\n  endif\n  return 3\nenddef\n",
			want: []struct{ text, message string }{{"return", "Unreachable code after :return"}},
		},
		{
			name: "official legacy-root def all-return if branches",
			source: "def Func(): number\n  if true\n    return 1\n" +
				"  else\n    return 2\n  endif\n  return 3\nenddef\n",
			want: []struct{ text, message string }{{"return", "Unreachable code after :return"}},
		},
		{
			name: "official throw before catch command",
			source: "vim9script\ndef Func()\n  try\n    throw 'failed'\n" +
				"    echo 'unreachable'\n  catch\n    echo 'caught'\n  endtry\nenddef\n",
			want: []struct{ text, message string }{{"echo", "Unreachable code after :throw"}},
		},
		{
			name: "direct def and top-level returns",
			source: "vim9script\ndef Direct()\n  return\n  echo 'def'\nenddef\n" +
				"return\necho 'top'\n",
			want: []struct{ text, message string }{{"echo", "Unreachable code after :return"}, {"echo", "Unreachable code after :return"}},
		},
		{
			name: "all-return try catch followed by code",
			source: "vim9script\ndef Func()\n  try\n    return\n  catch\n" +
				"    return\n  endtry\n  echo 'after'\nenddef\n",
			want: []struct{ text, message string }{{"echo", "Unreachable code after :return"}},
		},
		{
			name: "Vim9 lambda command block",
			source: "vim9script\nvar Callback = () => {\n  return\n" +
				"  echo 'after'\n}\n",
			want: []struct{ text, message string }{{"echo", "Unreachable code after :return"}},
		},
		{
			name: "one-sided if loops legacy and malformed stay conservative",
			source: "vim9script\ndef Conditional()\n  if true\n    return\n  endif\n  echo 'after if'\n" +
				"enddef\ndef Loop()\n  while true\n    return\n  endwhile\n  echo 'after loop'\nenddef\n" +
				"function Legacy()\n  return\n  echo 'legacy'\nendfunction\ndef Broken()\n  if true\n    return\n  echo 'editing'\n",
		},
		{
			name: "separate defs are independent",
			source: "vim9script\ndef First()\n  return\n  echo 'first'\nenddef\n" +
				"def Second()\n  throw 'failed'\n  echo 'second'\nenddef\n",
			want: []struct{ text, message string }{{"echo", "Unreachable code after :return"}, {"echo", "Unreachable code after :throw"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1095" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != len(test.want) {
				t.Fatalf("E1095 diagnostics = %#v, want %q; all diagnostics = %#v; syntax diagnostics = %#v", got, test.want, Analyze(file).Diagnostics, file.Diagnostics)
			}
			for index, diagnostic := range got {
				want := test.want[index]
				if file.Text(diagnostic.Span) != want.text || diagnostic.Message != want.message {
					t.Fatalf("E1095 diagnostic[%d] = %#v on %q, want %q on %q", index, diagnostic, file.Text(diagnostic.Span), want.message, want.text)
				}
			}
		})
	}
}

func TestAnalyzeStrictStringConversionDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		want         []struct{ message, text string }
	}{
		{
			name: "compiled concatenation and compound assignment",
			source: "vim9script\ndef Func()\n  var text = ''\n  var values = [1]\n" +
				"  var value = values .. {}\n  text ..= values\nenddef\n",
			want: []struct{ message, text string }{{"Cannot convert list to string", "values"}, {"Cannot convert list to string", "values"}},
		},
		{
			name: "computed dictionary key and interpolated values",
			source: "vim9script\ndef Func()\n  var dict = {[[1, 2]]: 0}\n" +
				"  var blob = 0z12\n  var text = $'{blob}'\nenddef\n",
			want: []struct{ message, text string }{{"Cannot convert list to string", "[1, 2]"}, {"Cannot convert blob to string", "blob"}},
		},
		{
			name: "typealias interpolation has E1105 precedence",
			source: "vim9script\ntype MyType = number\ndef Func()\n" +
				"  var text = $\"-{MyType}-\"\nenddef\n",
			want: []struct{ message, text string }{{"Cannot convert typealias to string", "MyType"}},
		},
		{
			name: "compiled known non-string values and left precedence",
			source: "vim9script\ndef Void()\nenddef\ndef Func()\n" +
				"  var tuple = (1, 2) .. 'x'\n  var dictionary = {} .. 'x'\n  var void = Void() .. 'x'\n  var blob = 0z12 .. 'x'\n" +
				"  var funcref = function('len') .. 'x'\n  var partial = function('len', ['a']) .. 'x'\n  var null = null_partial .. 'x'\n" +
				"  var job = null_job .. 'x'\n  var channel = null_channel .. 'x'\n  var first = [1] .. {}\nenddef\n",
			want: []struct{ message, text string }{
				{"Cannot convert tuple to string", "(1, 2)"}, {"Cannot convert dict to string", "{}"}, {"Cannot convert void to string", "Void()"},
				{"Cannot convert blob to string", "0z12"}, {"Cannot convert func to string", "function('len')"}, {"Cannot convert partial to string", "function('len', ['a'])"},
				{"Cannot convert partial to string", "null_partial"}, {"Cannot convert job to string", "null_job"}, {"Cannot convert channel to string", "null_channel"}, {"Cannot convert list to string", "[1]"},
			},
		},
		{
			name: "computed key lambda and plain key guard",
			source: "vim9script\ndef Func()\n  var values = [1]\n  var plain = {values: 1}\n" +
				"  var computed = {[[1]]: 1}\n  var Callback = () => {\n    return [1] .. 'x'\n  }\nenddef\n",
			want: []struct{ message, text string }{{"Cannot convert list to string", "[1]"}, {"Cannot convert list to string", "[1]"}},
		},
		{
			name: "class and object values",
			source: "vim9script\nclass A\nendclass\ndef Func()\n  var object = A.new()\n" +
				"  var objectText = object .. ''\n  var classText = A .. ''\nenddef\n",
			want: []struct{ message, text string }{{"Cannot convert object to string", "object"}, {"Cannot convert class to string", "A"}},
		},
		{
			name:   "legacy-root def",
			source: "def Func()\n  var values = [1]\n  var text = values .. 'x'\nenddef\n",
			want:   []struct{ message, text string }{{"Cannot convert list to string", "values"}},
		},
		{
			name:   "Vim9 script top level retains old path",
			source: "vim9script\nvar values = [1]\nvar top = values .. 'x'\n",
		},
		{
			name:   "Legacy-root function retains old path",
			source: "function Legacy()\n  let old = [1] . 'x'\nendfunction\n",
		},
		{
			name: "allowed scalar and interpolation container conversions",
			source: "vim9script\ndef Func()\n  var scalar = 'x' .. v:none .. true .. 1 .. 1.5\n" +
				"  var interpolated = $\"{[1]}-{(1, 2)}-{{key: 1}}\"\nenddef\n",
		},
		{
			name:   "unknown any scalar and interpolated containers stay valid",
			source: "vim9script\ndef Func()\n  var value: any\n  var text = value .. 1\n  var more = $'{[1]}-{ {key: 1} }'\nenddef\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1105" {
					got = append(got, diagnostic)
				}
			}
			if test.name == "typealias interpolation has E1105 precedence" {
				for _, diagnostic := range result.Diagnostics {
					if diagnostic.Code == "vim/E1407" || diagnostic.Code == "vim/E1403" {
						t.Fatalf("typealias interpolation retained %s: %#v", diagnostic.Code, diagnostic)
					}
				}
			}
			if test.name != "Vim9 script top level retains old path" && test.name != "Legacy-root function retains old path" {
				for _, diagnostic := range result.Diagnostics {
					switch diagnostic.Code {
					case "vim/E729", "vim/E730", "vim/E731", "vim/E734", "vim/E908", "vim/E976":
						t.Fatalf("compiled source retained %s: %#v", diagnostic.Code, diagnostic)
					}
				}
			}
			if len(got) != len(test.want) {
				t.Fatalf("E1105 diagnostics = %#v, want %#v", got, test.want)
			}
			for index, diagnostic := range got {
				if diagnostic.Message != test.want[index].message || file.Text(diagnostic.Span) != test.want[index].text {
					t.Fatalf("E1105[%d] = %#v on %q, want %q on %q", index, diagnostic, file.Text(diagnostic.Span), test.want[index].message, test.want[index].text)
				}
			}
		})
	}
}

func TestAnalyzeReturningValueWithoutReturnTypeDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		want         int
	}{
		{
			name:   "omitted return type",
			source: "vim9script\ndef Func()\n  return 1\nenddef\n",
			want:   1,
		},
		{
			name:   "explicit void return type",
			source: "vim9script\ndef Func(): void\n  return 1\nenddef\n",
			want:   1,
		},
		{
			name:   "legacy-root def",
			source: "def Func()\n  return 1\nenddef\n",
			want:   1,
		},
		{
			name: "nested defs remain independent",
			source: "vim9script\ndef Outer()\n  def Inner()\n    return 1\n" +
				"  enddef\n  return 2\nenddef\n",
			want: 2,
		},
		{
			name:   "top-level def named new is not a constructor",
			source: "vim9script\ndef new()\n  return 1\nenddef\n",
			want:   1,
		},
		{
			name:   "top-level def named newName is not a constructor",
			source: "vim9script\ndef newName()\n  return 1\nenddef\n",
			want:   1,
		},
		{
			name: "non-void missing bare legacy lambda and constructor returns are exempt",
			source: "vim9script\ndef Typed(): number\n  return 1\nenddef\n" +
				"def Missing():\n  return 1\nenddef\ndef Bare(): void\n  return\nenddef\n" +
				"var Callback = () => {\n  return 1\n}\nclass Item\n  def new()\n    return 1\n  enddef\n  def newName()\n    return 1\n  enddef\n  def _new()\n    return 1\n  enddef\nendclass\n" +
				"function Legacy()\n  return 1\nendfunction\nreturn 1\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1096" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != test.want {
				t.Fatalf("E1096 diagnostics = %#v, want %d; all diagnostics = %#v", got, test.want, Analyze(file).Diagnostics)
			}
			for _, diagnostic := range got {
				if diagnostic.Message != "Returning a value in a function without a return type" || file.Text(diagnostic.Span) != "return" {
					t.Fatalf("E1096 diagnostic = %#v on %q", diagnostic, file.Text(diagnostic.Span))
				}
			}
		})
	}
}

func TestAnalyzeVim9ParameterAssignmentDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		want         []string
	}{
		{
			name: "official direct and compound def assignments",
			source: "vim9script\ndef Func(arg: number)\n" +
				"  arg = 3\n  arg += 1\nenddef\n",
			want: []string{"arg", "arg"},
		},
		{
			name:   "lambda parameter assignment",
			source: "vim9script\nvar Callback = (arg: number) => {\n  arg = 3\n}\n",
			want:   []string{"arg"},
		},
		{
			name: "container mutation and legacy arguments",
			source: "vim9script\ndef Modern(listArg: list<number>, dictArg: dict<number>)\n" +
				"  listArg[0] = 3\n  dictArg.key = 3\nenddef\n" +
				"function Legacy(value)\n  let a:value[0] = 1\nendfunction\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1090" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != len(test.want) {
				t.Fatalf("E1090 diagnostics = %#v, want %q; syntax diagnostics = %#v", got, test.want, file.Diagnostics)
			}
			for index, diagnostic := range got {
				if diagnostic.Message != "Cannot assign to argument "+test.want[index] || file.Text(diagnostic.Span) != test.want[index] {
					t.Fatalf("E1090 diagnostic[%d] = %#v on %q, want argument %q", index, diagnostic, file.Text(diagnostic.Span), test.want[index])
				}
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

func TestAnalyzeReadOnlyClassMemberDiagnostics(t *testing.T) {
	source := `vim9script
class A
  final foo: string = 'a'
  public final values: list<number> = [1]
  public static final label: string = 'a'
  const token: string
  var mutable: string
  def new()
    this.foo = 'initialized'
    this.token = 'initialized'
  enddef
  def Change()
    this.foo = 'b'
    this.values[0] = 2
    this.mutable = 'b'
  enddef
  static def ChangeLabel()
    label = 'b'
  enddef
endclass
var a = A.new()
a.values = [2]
A.label = 'b'
`
	file := syntax.Parse(source)
	result := Analyze(file)
	var got []syntax.Diagnostic
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1409" {
			got = append(got, diagnostic)
		}
	}
	want := []struct {
		message string
		span    string
	}{
		{`Cannot change read-only variable "foo" in class "A"`, "foo"},
		{`Cannot change read-only variable "label" in class "A"`, "label"},
		{`Cannot change read-only variable "values" in class "A"`, "values"},
		{`Cannot change read-only variable "label" in class "A"`, "label"},
	}
	if len(got) != len(want) {
		t.Fatalf("E1409 diagnostics = %#v, want %#v; syntax diagnostics = %#v; all diagnostics = %#v", got, want, file.Diagnostics, result.Diagnostics)
	}
	for index, diagnostic := range got {
		if diagnostic.Message != want[index].message || file.Text(diagnostic.Span) != want[index].span {
			t.Fatalf("E1409 diagnostic[%d] = %#v on %q, want %#v", index, diagnostic, file.Text(diagnostic.Span), want[index])
		}
	}
}

func TestAnalyzeTypeAliasAsValueDiagnostic(t *testing.T) {
	source := `vim9script
type A = number
def F(): any
  var first = A
  var second = A + 1
  echo len(A)
  return A
enddef
`
	file := syntax.Parse(source)
	result := Analyze(file)
	var got []syntax.Diagnostic
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1407" {
			got = append(got, diagnostic)
		}
	}
	if len(got) != 4 {
		t.Fatalf("E1407 diagnostics = %#v; syntax diagnostics = %#v; all diagnostics = %#v", got, file.Diagnostics, result.Diagnostics)
	}
	for index, diagnostic := range got {
		if diagnostic.Message != "Cannot use a Typealias as a variable or value" || file.Text(diagnostic.Span) != "A" {
			t.Fatalf("E1407 diagnostic[%d] = %#v on %q", index, diagnostic, file.Text(diagnostic.Span))
		}
	}

	valid := Analyze(syntax.Parse(`vim9script
class C
endclass
type A = number
type ClassAlias = C
def F(value: A): A
  var copy: A = value
  echo type(A)
  echo typename(A)
  echo string(A)
  var object = C.new()
  echo instanceof(object, ClassAlias)
  var aliasObject = ClassAlias.new()
  return copy
enddef
`))
	for _, diagnostic := range valid.Diagnostics {
		if diagnostic.Code == "vim/E1407" {
			t.Fatalf("type position reported E1407: %#v", valid.Diagnostics)
		}
	}
}

func TestAnalyzeE1406PublicProtectedMemberNames(t *testing.T) {
	source := `vim9script
class A
  var inherited = 1
endclass
class B extends A
endclass
class C extends B
  var _inherited = 2
  var value = 1
  var _value = 2
  static var label = 'a'
  static var _label = 'b'
  var prefix = 1
  var prefixLong = 2
endclass
`
	file := syntax.Parse(source)
	result := Analyze(file)
	var got []syntax.Diagnostic
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1406" {
			got = append(got, diagnostic)
		}
	}
	want := []struct {
		message string
		span    string
	}{
		{"Public and protected member have the same name: inherited and _inherited", "_inherited"},
		{"Public and protected member have the same name: value and _value", "_value"},
		{"Public and protected member have the same name: label and _label", "_label"},
	}
	if len(got) != len(want) {
		t.Fatalf("E1406 diagnostics = %#v, want %#v; syntax diagnostics = %#v; all diagnostics = %#v", got, want, file.Diagnostics, result.Diagnostics)
	}
	for index, diagnostic := range got {
		if diagnostic.Message != want[index].message || file.Text(diagnostic.Span) != want[index].span {
			t.Fatalf("E1406 diagnostic[%d] = %#v on %q, want %#v", index, diagnostic, file.Text(diagnostic.Span), want[index])
		}
	}
}

func TestAnalyzeE1332PublicUnderscoreVariableDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, source, command string }{
		{"class", "vim9script\nclass A\n  public var _val = 10\nendclass\n", "public var _val = 10"},
		{"static", "vim9script\nclass A\n  public static var _val = 10\nendclass\n", "public static var _val = 10"},
		{"final", "vim9script\nclass A\n  public final _val = 10\nendclass\n", "public final _val = 10"},
		{"single underscore", "vim9script\nclass A\n  public var _\nendclass\n", "public var _"},
		{"interface", "vim9script\ninterface A\n  public var _val: number\nendinterface\n", "public var _val: number"},
		{"enum", "vim9script\nenum A\n  public var _val: number\nendenum\n", "public var _val: number"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1332" {
					got = append(got, diagnostic)
				}
			}
			message := "public variable name cannot start with underscore: " + test.command
			if len(got) != 1 || got[0].Message != message || file.Text(got[0].Span) != test.command {
				t.Fatalf("E1332 diagnostics = %#v", result.Diagnostics)
			}
		})
	}

	for _, test := range []struct{ name, source string }{
		{"implicit protected", "vim9script\nclass A\n  var _val = 10\nendclass\n"},
		{"public regular", "vim9script\nclass A\n  public var value = 10\nendclass\n"},
		{"public method", "vim9script\nclass A\n  public def _Value()\n  enddef\nendclass\n"},
		{"top-level", "vim9script\npublic var _val = 10\n"},
		{"Legacy", "class A\n  public var _val = 10\nendclass\n"},
		{"incomplete", "vim9script\nclass A\n  public var\nendclass\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, diagnostic := range Analyze(syntax.Parse(test.source)).Diagnostics {
				if diagnostic.Code == "vim/E1332" {
					t.Fatalf("guard unexpectedly received E1332: %#v\n%s", diagnostic, test.source)
				}
			}
		})
	}

	file := syntax.Parse("vim9script\nclass A\n  var value = 1\n  public var _value = 2\nendclass\n")
	result := Analyze(file)
	var e1332, e1406 int
	for _, diagnostic := range result.Diagnostics {
		switch diagnostic.Code {
		case "vim/E1332":
			e1332++
		case "vim/E1406":
			e1406++
		}
	}
	if e1332 != 1 || e1406 != 0 {
		t.Fatalf("E1332 precedence diagnostics = %#v", result.Diagnostics)
	}
}

func TestAnalyzeE1369DuplicateClassVariables(t *testing.T) {
	for _, test := range []struct {
		name    string
		source  string
		message string
		span    string
	}{
		{
			name:    "object variable",
			source:  "vim9script\nclass C\n  var val = 10\n  var val = 20\nendclass\n",
			message: "Duplicate variable: val", span: "val",
		},
		{
			name:    "protected variable",
			source:  "vim9script\nclass C\n  var _val = 10\n  var _val = 20\nendclass\n",
			message: "Duplicate variable: _val", span: "_val",
		},
		{
			name:    "static and object variable",
			source:  "vim9script\nclass C\n  static var val = 10\n  var val = 20\nendclass\n",
			message: "Duplicate variable: val", span: "val",
		},
		{
			name:    "interface variable",
			source:  "vim9script\ninterface I\n  var val: number\n  var val: number\nendinterface\n",
			message: "Duplicate variable: val", span: "val",
		},
		{
			name:    "enum name variable",
			source:  "vim9script\nenum Planet\n  Mercury\n  var name: string\nendenum\n",
			message: "Duplicate variable: name", span: "name",
		},
		{
			name:    "enum ordinal variable",
			source:  "vim9script\nenum Planet\n  Mercury\n  var ordinal: number\nendenum\n",
			message: "Duplicate variable: ordinal", span: "ordinal",
		},
		{
			name:    "inherited object variable",
			source:  "vim9script\nclass A\n  var val = 10\nendclass\nclass B extends A\nendclass\nclass C extends B\n  var val = 20\nendclass\n",
			message: "Duplicate variable: val", span: "endclass",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1369" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1369 diagnostics=%#v; syntax diagnostics=%#v", got, file.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nclass C\n  var val = 10\n  var _val = 20\nendclass\n",
		"vim9script\nclass A\n  static var val = 10\nendclass\nclass B extends A\n  static var val = 20\nendclass\n",
		"vim9script\nclass C\n  var svar2 = 10\n  var svar = 20\n  def new()\n  enddef\nendclass\n",
		"vim9script\nclass A extends B\n  var val = 10\nendclass\nclass B extends A\nendclass\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1369" {
				t.Fatalf("guard source reported E1369: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1367InterfaceVariableAccessMismatch(t *testing.T) {
	for _, member := range []string{
		"public var val = 10",
		"public final val = 10",
		"public const val = 10",
	} {
		source := "vim9script\ninterface A\n  var val: number\nendinterface\nclass B implements A\n  " + member + "\nendclass\n"
		file := syntax.Parse(source)
		var got []syntax.Diagnostic
		for _, diagnostic := range Analyze(file).Diagnostics {
			if diagnostic.Code == "vim/E1367" {
				got = append(got, diagnostic)
			}
			if diagnostic.Code == "vim/E1382" {
				t.Fatalf("member=%q also reported E1382: %#v", member, diagnostic)
			}
		}
		if len(got) != 1 || got[0].Message != `Access level of variable "val" of interface "A" is different` || file.Text(got[0].Span) != "endclass" {
			t.Fatalf("member=%q E1367 diagnostics=%#v; syntax diagnostics=%#v", member, got, file.Diagnostics)
		}
	}

	for _, test := range []struct {
		name    string
		source  string
		message string
	}{
		{
			name: "inherited class variable",
			source: "vim9script\ninterface A\n  var val: number\nendinterface\nclass Parent\n  public var val = 10\nendclass\n" +
				"class B extends Parent implements A\nendclass\n",
			message: `Access level of variable "val" of interface "A" is different`,
		},
		{
			name: "inherited interface requirement",
			source: "vim9script\ninterface A\n  var val: number\nendinterface\ninterface I extends A\nendinterface\n" +
				"class B implements I\n  public var val = 10\nendclass\n",
			message: `Access level of variable "val" of interface "I" is different`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1367" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != "endclass" {
				t.Fatalf("E1367 diagnostics=%#v; syntax diagnostics=%#v", got, file.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ninterface A\n  var val: number\nendinterface\nclass B implements A\n  var val = 10\nendclass\n",
		"vim9script\ninterface A\n  var val: number\nendinterface\nclass B implements A\n  final val = 10\nendclass\n",
		"vim9script\ninterface A\n  var val: number\nendinterface\nclass B implements A\n  static var val = 10\nendclass\n",
		"vim9script\ninterface A\n  var val: number\nendinterface\nclass B implements A\nendclass\n",
		"vim9script\ninterface A\n  var val: number\nendinterface\nclass B implements A\n  var val: string = 'x'\nendclass\n",
		"vim9script\ninterface A\n  var val: number\nendinterface\nclass Parent\n  var val: number = 1\nendclass\nclass B extends Parent implements A\n  public var val: number = 1\nendclass\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1367" {
				t.Fatalf("guard source reported E1367: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1377MethodAccessLevelMismatch(t *testing.T) {
	for _, test := range []struct {
		name       string
		parentName string
		childName  string
	}{
		{name: "public to protected", parentName: "Foo", childName: "_Foo"},
		{name: "protected to public", parentName: "_Foo", childName: "Foo"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "vim9script\nclass A\n  def " + test.parentName + "()\n  enddef\nendclass\nclass B extends A\nendclass\nclass C extends B\n  def " + test.childName + "()\n  enddef\nendclass\nvar after = 1\n"
			file := syntax.Parse(source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1377" {
					got = append(got, diagnostic)
				}
			}
			wantMessage := `Access level of method "` + test.childName + `" is different in class "A"`
			if len(got) != 1 || got[0].Message != wantMessage || file.Text(got[0].Span) != "endclass" {
				t.Fatalf("E1377 diagnostics = %#v; syntax diagnostics = %#v", got, file.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nclass A\n  def Foo()\n  enddef\nendclass\nclass B extends A\n  def Foo()\n  enddef\nendclass\n",
		"vim9script\nclass A\n  def _Foo()\n  enddef\nendclass\nclass B extends A\n  def _Foo()\n  enddef\nendclass\n",
		"vim9script\nclass A\n  static def Foo()\n  enddef\nendclass\nclass B extends A\n  static def _Foo()\n  enddef\nendclass\n",
		"vim9script\nclass A\n  def Foo()\n  enddef\nendclass\nclass B extends A\n  public def _Foo()\n  enddef\nendclass\n",
		"vim9script\nclass A extends B\n  def Foo()\n  enddef\nendclass\nclass B extends A\n  def Foo()\n  enddef\nendclass\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1377" {
				t.Fatalf("guard source reported E1377: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeClassAsValueDiagnostic(t *testing.T) {
	source := `vim9script
class C
endclass
type Alias = C
var scriptValue = [C]
def F(): any
  var direct = C
  var alias = Alias
  echo len(C)
  return C
enddef
`
	file := syntax.Parse(source)
	result := Analyze(file)
	var got []syntax.Diagnostic
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1405" {
			got = append(got, diagnostic)
		}
	}
	wantSpans := []string{"C", "C", "Alias", "C", "C"}
	if len(got) != len(wantSpans) {
		t.Fatalf("E1405 diagnostics = %#v, want spans %#v; syntax diagnostics = %#v; all diagnostics = %#v", got, wantSpans, file.Diagnostics, result.Diagnostics)
	}
	for index, diagnostic := range got {
		if diagnostic.Message != `Class "C" cannot be used as a value` || file.Text(diagnostic.Span) != wantSpans[index] {
			t.Fatalf("E1405 diagnostic[%d] = %#v on %q", index, diagnostic, file.Text(diagnostic.Span))
		}
	}

	valid := Analyze(syntax.Parse(`vim9script
class C
  static var label = 'ok'
endclass
type Alias = C
var object: C = C.new()
var aliasObject: Alias = Alias.new()
echo C.label
echo type(C)
echo typename(C)
echo string(C)
echo instanceof(object, C)
`))
	for _, diagnostic := range valid.Diagnostics {
		if diagnostic.Code == "vim/E1405" {
			t.Fatalf("class context reported E1405: %#v", valid.Diagnostics)
		}
	}
}

func TestAnalyzeTypeAliasScriptValueDiagnostic(t *testing.T) {
	source := `vim9script
type T = number
var direct = T
var nested = [T]
T = 1
T += 2
`
	file := syntax.Parse(source)
	result := Analyze(file)
	var got []syntax.Diagnostic
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1403" {
			got = append(got, diagnostic)
		}
	}
	if len(got) != 4 {
		t.Fatalf("E1403 diagnostics = %#v; syntax diagnostics = %#v; all diagnostics = %#v", got, file.Diagnostics, result.Diagnostics)
	}
	for index, diagnostic := range got {
		if diagnostic.Message != `Type alias "T" cannot be used as a value` || file.Text(diagnostic.Span) != "T" {
			t.Fatalf("E1403 diagnostic[%d] = %#v on %q", index, diagnostic, file.Text(diagnostic.Span))
		}
	}

	compiled := Analyze(syntax.Parse("vim9script\ntype T = number\ndef F()\n  T = 1\nenddef\n"))
	foundE46 := false
	for _, diagnostic := range compiled.Diagnostics {
		if diagnostic.Code == "vim/E1403" {
			t.Fatalf("compiled assignment reported E1403: %#v", compiled.Diagnostics)
		}
		if diagnostic.Code == "vim/E46" && diagnostic.Message == `Cannot change read-only variable "T"` {
			foundE46 = true
		}
	}
	if !foundE46 {
		t.Fatalf("compiled assignment diagnostics = %#v, want E46", compiled.Diagnostics)
	}
}

func TestAnalyzeTypeAliasInsideDefDiagnostic(t *testing.T) {
	source := `vim9script
def F()
  if true
    type Nested = list<string>
  endif
enddef
`
	file := syntax.Parse(source)
	result := Analyze(file)
	var got []syntax.Diagnostic
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1399" {
			got = append(got, diagnostic)
		}
	}
	if len(got) != 1 || got[0].Message != "Type can only be used in a script" || file.Text(got[0].Span) != "def F()" {
		t.Fatalf("E1399 diagnostics = %#v; syntax diagnostics = %#v; all diagnostics = %#v", got, file.Diagnostics, result.Diagnostics)
	}
	for _, declaration := range result.Declarations {
		if declaration.Name == "Nested" {
			t.Fatalf("invalid local type alias was declared: %#v", declaration)
		}
	}

	valid := Analyze(syntax.Parse("vim9script\nif true\n  type ScriptType = list<string>\nendif\n"))
	for _, diagnostic := range valid.Diagnostics {
		if diagnostic.Code == "vim/E1399" {
			t.Fatalf("script type alias reported E1399: %#v", valid.Diagnostics)
		}
	}
}

func TestAnalyzeDuplicateTypeAliasDiagnostic(t *testing.T) {
	source := `vim9script
type Direct = list<number>
type Direct = list<string>
if true
  type Nested = number
endif
type Nested = string
type First = number
type Second = First
`
	file := syntax.Parse(source)
	result := Analyze(file)
	var got []syntax.Diagnostic
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1396" {
			got = append(got, diagnostic)
		}
	}
	want := []struct {
		message string
		span    string
	}{
		{`Type alias "Direct" already exists`, "Direct"},
		{`Type alias "Nested" already exists`, "Nested"},
	}
	if len(got) != len(want) {
		t.Fatalf("E1396 diagnostics = %#v, want %#v; syntax diagnostics = %#v; all diagnostics = %#v", got, want, file.Diagnostics, result.Diagnostics)
	}
	for index, diagnostic := range got {
		if diagnostic.Message != want[index].message || file.Text(diagnostic.Span) != want[index].span {
			t.Fatalf("E1396 diagnostic[%d] = %#v on %q, want %#v", index, diagnostic, file.Text(diagnostic.Span), want[index])
		}
	}

	classAlias := Analyze(syntax.Parse("vim9script\nclass C\nendclass\ntype Alias = C\ntype Alias = C\n"))
	foundE1041 := false
	for _, diagnostic := range classAlias.Diagnostics {
		if diagnostic.Code == "vim/E1396" {
			t.Fatalf("class alias duplicate reported E1396: %#v", classAlias.Diagnostics)
		}
		if diagnostic.Code == "vim/E1041" && diagnostic.Message == `Redefining script item: "Alias"` {
			foundE1041 = true
		}
	}
	if !foundE1041 {
		t.Fatalf("class alias diagnostics = %#v, want E1041", classAlias.Diagnostics)
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

func TestAnalyzeE1560RejectsTypeArgumentsForNonGenericFunction(t *testing.T) {
	tests := []struct {
		name   string
		source string
		span   string
	}{
		{
			name: "direct call",
			source: "vim9script\ndef Fn(x: number)\nenddef\n" +
				"def Use()\n  Fn<number>(10)\nenddef\n",
			span: "Fn",
		},
		{
			name: "function reference",
			source: "vim9script\ndef Fn()\nenddef\n" +
				"var Fx = function(Fn<number>)\n",
			span: "Fn",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			all := Analyze(file).Diagnostics
			var got []syntax.Diagnostic
			for _, diagnostic := range all {
				if diagnostic.Code == "vim/E1560" {
					got = append(got, diagnostic)
				}
			}
			if len(all) != 1 || len(got) != 1 || got[0].Message != "Not a generic function: Fn" || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v; syntax diagnostics = %#v", all, file.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef Fn<T>(x: T)\nenddef\nFn<number>(1)\n",
		"vim9script\nUnknown<number>(1)\n",
		"vim9script\nvar Fn = (x) => x\nFn<number>(1)\n",
		"vim9script\ndef Fn()\nenddef\nFn<>()\n",
		"vim9script\nvar object: any\nobject.Fn<number>(1)\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1560" {
				t.Fatalf("guard source reported E1560: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1559RequiresGenericTypeArguments(t *testing.T) {
	tests := []struct {
		name string
		use  string
		span string
	}{
		{name: "direct call", use: "Fn()", span: "Fn"},
		{name: "function reference", use: "var Fx = function(Fn)", span: "Fn"},
		{name: "quoted function reference", use: "var Fx = function('Fn')", span: "'Fn'"},
		{name: "call identifier", use: "call(Fn, [])", span: "Fn"},
		{name: "call quoted", use: "call('Fn', [])", span: "'Fn'"},
		{name: "value reference", use: "var Fx = Fn", span: "Fn"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := "vim9script\ndef Fn<T>()\nenddef\n" + test.use + "\n"
			file := syntax.Parse(source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1559" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Type arguments missing for generic function 'Fn'" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1559 diagnostics = %#v; syntax diagnostics = %#v", got, file.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef Fn<T>()\nenddef\nFn<number>()\n",
		"vim9script\ndef Fn()\nenddef\nFn()\n",
		"vim9script\nUnknown()\n",
		"vim9script\ndef Fn<T>()\nenddef\nfunction(dynamicName)\n",
		"vim9script\ndef Fn<T>()\nenddef\nfunction('F\\n')\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1559" {
				t.Fatalf("guard source reported E1559: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1557RequiresEveryGenericTypeArgument(t *testing.T) {
	tests := []struct {
		name string
		use  string
	}{
		{name: "direct call", use: "Fn<number>()"},
		{name: "function reference", use: "var Fx = function(Fn<number>)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := "vim9script\ndef Fn<A, B>()\nenddef\n" + test.use + "\n"
			file := syntax.Parse(source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1557" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Not enough types specified for generic function 'Fn'" || file.Text(got[0].Span) != "Fn" {
				t.Fatalf("E1557 diagnostics = %#v; syntax diagnostics = %#v", got, file.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef Fn<A, B>()\nenddef\nFn<number, string>()\n",
		"vim9script\ndef Fn<A>()\nenddef\nFn<number, string>()\n",
		"vim9script\ndef Fn<A>()\nenddef\nFn()\n",
		"vim9script\nUnknown<number>()\n",
		"vim9script\ndef Fn()\nenddef\nFn<number>()\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1557" {
				t.Fatalf("guard source reported E1557: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1556RejectsExtraGenericTypeArguments(t *testing.T) {
	tests := []struct {
		name string
		use  string
	}{
		{name: "direct call", use: "Fn<number, string>()"},
		{name: "function reference", use: "var Fx = function(Fn<number, string>)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := "vim9script\ndef Fn<T>()\nenddef\n" + test.use + "\n"
			file := syntax.Parse(source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1556" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Too many types specified for generic function 'Fn'" || file.Text(got[0].Span) != "Fn" {
				t.Fatalf("E1556 diagnostics = %#v; syntax diagnostics = %#v", got, file.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef Fn<A, B>()\nenddef\nFn<number, string>()\n",
		"vim9script\ndef Fn<A, B>()\nenddef\nFn<number>()\n",
		"vim9script\ndef Fn<A>()\nenddef\nFn()\n",
		"vim9script\nUnknown<number, string>()\n",
		"vim9script\ndef Fn()\nenddef\nFn<number>()\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1556" {
				t.Fatalf("guard source reported E1556: %#v\n%s", diagnostic, source)
			}
		}
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

func TestAnalyzeE1003MissingReturnValue(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		want   int
	}{
		{
			name:   "Vim9 root",
			source: "vim9script\ndef Func(): number\n  return\nenddef\n",
			want:   1,
		},
		{
			name:   "Legacy root def",
			source: "def Func(): number\n  return\nenddef\n",
			want:   1,
		},
		{
			name: "nested def uses innermost return type",
			source: "vim9script\ndef Outer(): number\n" +
				"  def Inner(): void\n" +
				"    return\n" +
				"  enddef\n" +
				"  return\n" +
				"enddef\n",
			want: 1,
		},
		{
			name:   "void return",
			source: "vim9script\ndef Func(): void\n  return\nenddef\n",
		},
		{
			name:   "omitted return type",
			source: "vim9script\ndef Func()\n  return\nenddef\n",
		},
		{
			name:   "malformed return type",
			source: "vim9script\ndef Func():\n  return\nenddef\n",
		},
		{
			name:   "any return type still requires a value",
			source: "vim9script\ndef Func(): any\n  return\nenddef\n",
			want:   1,
		},
		{
			name:   "value supplied",
			source: "vim9script\ndef Func(): number\n  return 1\nenddef\n",
		},
		{
			name: "nested Legacy function is independent",
			source: "vim9script\ndef Outer(): number\n" +
				"  function Inner()\n" +
				"    return\n" +
				"  endfunction\n" +
				"  return 1\n" +
				"enddef\n",
		},
		{
			name:   "missing return statement is E1027 territory",
			source: "vim9script\ndef Func(): number\n  echo 'missing'\nenddef\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1003" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != test.want {
				t.Fatalf("E1003 diagnostics = %#v, want %d", got, test.want)
			}
			if test.want == 1 && (got[0].Message != "Missing return value" || file.Text(got[0].Span) != "return") {
				t.Fatalf("E1003 diagnostic = %#v", got[0])
			}
		})
	}
}

func TestAnalyzeE1027MissingReturnStatement(t *testing.T) {
	for _, test := range []struct {
		name, source, span string
	}{
		{
			name:   "empty def",
			source: "vim9script\ndef Missing(): number\nenddef\n",
			span:   "enddef",
		},
		{
			name: "official first branch falls through",
			source: "vim9script\ndef Missing(): number\n" +
				"  if g:cond\n    echo 'no return'\n  else\n    return 0\n  endif\nenddef\n",
			span: "enddef",
		},
		{
			name: "official second branch falls through",
			source: "vim9script\ndef Missing(): number\n" +
				"  if g:cond\n    return 1\n  else\n    echo 'no return'\n  endif\nenddef\n",
			span: "enddef",
		},
		{
			name:   "loop return is not guaranteed",
			source: "vim9script\ndef Missing(): number\n  while g:cond\n    return 1\n  endwhile\nenddef\n",
			span:   "enddef",
		},
		{
			name: "pattern catch is not exhaustive",
			source: "vim9script\ndef Missing(): string\n  try\n    return 'ok'\n  catch /x/\n" +
				"    return 'caught'\n  endtry\nenddef\n",
			span: "enddef",
		},
		{
			name: "catch throw is cleared by endtry",
			source: "vim9script\ndef Missing(): string\n  try\n    return 'ok'\n  catch\n" +
				"    throw 'failed'\n  endtry\nenddef\n",
			span: "enddef",
		},
		{
			name: "try throw is cleared by endtry",
			source: "vim9script\ndef Missing(): string\n  try\n    throw 'failed'\n  catch\n" +
				"    return 'caught'\n  endtry\nenddef\n",
			span: "enddef",
		},
		{
			name: "fallthrough finally clears earlier returns",
			source: "vim9script\ndef Missing(): string\n  try\n    return 'ok'\n  catch\n" +
				"    return 'caught'\n  finally\n    echo 'done'\n  endtry\nenddef\n",
			span: "enddef",
		},
		{
			name: "finally throw is cleared by endtry",
			source: "vim9script\ndef Missing(): string\n  try\n    echo 'work'\n  finally\n" +
				"    throw 'failed'\n  endtry\nenddef\n",
			span: "enddef",
		},
		{
			name:   "Legacy root def still compiles",
			source: "def Missing(): number\n  echo 'no return'\nenddef\n",
			span:   "enddef",
		},
		{
			name:   "typed block lambda",
			source: "vim9script\ndef Outer()\n  defer (): number => {\n  }()\nenddef\n",
			span:   "}",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1027" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Missing return statement" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1027 diagnostics = %#v, want one on %q; all diagnostics = %#v", got, test.span, result.Diagnostics)
			}
		})
	}

	for _, test := range []struct {
		name, source string
	}{
		{name: "direct return", source: "vim9script\ndef Complete(): number\n  return 1\nenddef\n"},
		{name: "direct throw", source: "vim9script\ndef Complete(): number\n  throw 'failed'\nenddef\n"},
		{
			name: "all conditional branches terminate",
			source: "vim9script\ndef Complete(): number\n" +
				"  if g:first\n    return 1\n  elseif g:second\n    throw 'failed'\n  else\n    return 3\n  endif\nenddef\n",
		},
		{
			name: "catch all returns",
			source: "vim9script\ndef Complete(): string\n  try\n    return 'ok'\n  catch\n" +
				"    return 'caught'\n  endtry\nenddef\n",
		},
		{
			name: "finally returns",
			source: "vim9script\ndef Complete(): string\n  try\n    echo 'work'\n  finally\n" +
				"    return 'done'\n  endtry\nenddef\n",
		},
		{name: "void", source: "vim9script\ndef Complete(): void\n  echo 'done'\nenddef\n"},
		{name: "inferred void", source: "vim9script\ndef Complete()\n  echo 'done'\nenddef\n"},
		{name: "bare return has E1003 priority", source: "vim9script\ndef Complete(): number\n  return\nenddef\n"},
		{name: "loop followed by return", source: "vim9script\ndef Complete(): number\n  while g:cond\n    return 1\n  endwhile\n  return 2\nenddef\n"},
		{name: "inline lambda returns expression", source: "vim9script\nvar Complete = (): number => 1\n"},
		{name: "incomplete def", source: "vim9script\ndef Incomplete(): number\n  echo 'editing'\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1027" {
					t.Fatalf("source unexpectedly received E1027: %#v", result.Diagnostics)
				}
			}
		})
	}
}

func TestAnalyzeE1538MoreTargetsThanTupleItems(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "declaration",
			source: "vim9script\nvar [v1, v2, v3] = ('a', 'b')\nvar after = 1\n",
		},
		{
			name:   "assignment",
			source: "vim9script\nvar v1: string\nvar v2: string\nvar v3: string\n[v1, v2, v3] = ('a', 'b')\nvar after = 1\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1538" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "More targets than Tuple items" || file.Text(got[0].Span) != "('a', 'b')" {
				t.Fatalf("E1538 diagnostics = %#v; syntax diagnostics = %#v", got, file.Diagnostics)
			}
			if file.Commands[len(file.Commands)-1].Declaration == nil {
				t.Fatalf("next-line recovery = %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef F()\n  var [v1, v2, v3] = ('a', 'b')\nenddef\n",
		"vim9script\nvar [v1, v2, v3] = ['a', 'b']\n",
		"vim9script\nvar values: tuple<string, string>\nvar [v1, v2, v3] = values\n",
		"function F()\n  let [v1, v2, v3] = ('a', 'b')\nendfunction\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1538" {
				t.Fatalf("guard source reported E1538: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1537LessTargetsThanTupleItems(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "declaration",
			source: "vim9script\nvar [v1, v2] = ('a', 'b', 'c')\nvar after = 1\n",
		},
		{
			name:   "assignment",
			source: "vim9script\nvar v1: string\nvar v2: string\n[v1, v2] = ('a', 'b', 'c')\nvar after = 1\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1537" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Less targets than Tuple items" || file.Text(got[0].Span) != "('a', 'b', 'c')" {
				t.Fatalf("E1537 diagnostics = %#v; syntax diagnostics = %#v", got, file.Diagnostics)
			}
			if file.Commands[len(file.Commands)-1].Declaration == nil {
				t.Fatalf("next-line recovery = %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef F()\n  var [v1, v2] = ('a', 'b', 'c')\nenddef\n",
		"vim9script\nvar [v1, v2] = ['a', 'b', 'c']\n",
		"vim9script\nvar values: tuple<string, string, string>\nvar [v1, v2] = values\n",
		"vim9script\nvar [v1; rest] = ('a', 'b', 'c')\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1537" {
				t.Fatalf("guard source reported E1537: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1536TupleRequired(t *testing.T) {
	tests := []struct {
		name      string
		rightHand string
	}{
		{name: "null tuple literal", rightHand: "null_tuple"},
		{name: "testing builtin", rightHand: "test_null_tuple()"},
		{name: "parenthesized literal", rightHand: "(null_tuple)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := "vim9script\nvar [v1; rest] = " + test.rightHand + "\nvar after = 1\n"
			file := syntax.Parse(source)
			all := Analyze(file).Diagnostics
			var got []syntax.Diagnostic
			for _, diagnostic := range all {
				if diagnostic.Code == "vim/E1536" {
					got = append(got, diagnostic)
				}
			}
			if len(all) != 1 || len(got) != 1 || got[0].Message != "Tuple required" || file.Text(got[0].Span) != test.rightHand {
				t.Fatalf("diagnostics = %#v; syntax diagnostics = %#v", all, file.Diagnostics)
			}
			if file.Commands[len(file.Commands)-1].Declaration == nil {
				t.Fatalf("next-line recovery = %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ndef F()\n  var [v1; rest] = null_tuple\nenddef\n",
		"vim9script\nvar values: tuple<any> = ()\nvar [v1; rest] = values\n",
		"vim9script\nvar [v1; rest] = ()\n",
		"vim9script\nvar [v1; rest] = Dynamic()\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1536" {
				t.Fatalf("guard source reported E1536: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1535DestructuringRequiresListOrTuple(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		rightHand string
	}{
		{name: "script string", prefix: "vim9script\n", rightHand: "''"},
		{name: "script dictionary", prefix: "vim9script\n", rightHand: "{}"},
		{name: "def string", prefix: "vim9script\ndef F()\n", rightHand: "''"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			suffix := "var after = 1\n"
			indent := ""
			if strings.Contains(test.prefix, "def F") {
				indent = "  "
				suffix = "enddef\nvar after = 1\n"
			}
			source := test.prefix + indent + "var v1: any\n" + indent + "var v2: any\n" + indent + "[v1, v2] = " + test.rightHand + "\n" + suffix
			file := syntax.Parse(source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1535" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "List or Tuple required" || file.Text(got[0].Span) != test.rightHand {
				t.Fatalf("E1535 diagnostics = %#v; syntax diagnostics = %#v", got, file.Diagnostics)
			}
			if file.Commands[len(file.Commands)-1].Declaration == nil {
				t.Fatalf("next-line recovery = %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nvar [v1, v2] = [1, 2]\n",
		"vim9script\nvar [v1, v2] = (1, 2)\n",
		"vim9script\ndef F(values: any)\n  var [v1, v2] = values\nenddef\n",
		"vim9script\nvar [v1, v2] = Dynamic()\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1535" {
				t.Fatalf("guard source reported E1535: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1533CannotSliceTuple(t *testing.T) {
	for _, slice := range []string{"t[1 : 2]", "t[ : 2]", "t[ : ]"} {
		t.Run(slice, func(t *testing.T) {
			source := "vim9script\nvar t: tuple<...list<string>> = ('a', 'b', 'c', 'd')\n" + slice + " = ('x', 'y')\nvar after = 1\n"
			file := syntax.Parse(source)
			all := Analyze(file).Diagnostics
			if len(all) != 1 || all[0].Code != "vim/E1533" || all[0].Message != "Cannot slice a tuple" || file.Text(all[0].Span) != slice {
				t.Fatalf("diagnostics = %#v; syntax diagnostics = %#v", all, file.Diagnostics)
			}
			if file.Commands[len(file.Commands)-1].Declaration == nil {
				t.Fatalf("next-line recovery = %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nvar values = [1, 2, 3]\nvalues[1 : 2] = [4]\n",
		"vim9script\nvar values: any\nvalues[1 : 2] = [4]\n",
		"vim9script\nvar values = (1, 2, 3)\necho values[1 : 2]\n",
		"vim9script\nvar values = (1, 2, 3)\nvalues[0] = 4\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1533" {
				t.Fatalf("guard source reported E1533: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1532CannotModifyTuple(t *testing.T) {
	tests := []struct {
		name   string
		source string
		span   string
	}{
		{
			name:   "direct tuple",
			source: "vim9script\nvar t = (1, 2)\nt[0] = 3\nvar after = 1\n",
			span:   "t[0]",
		},
		{
			name:   "tuple in typed list",
			source: "vim9script\nvar values: list<tuple<string, string>> = [('a', 'b')]\nvalues[0][1] = 'x'\nvar after = 1\n",
			span:   "values[0][1]",
		},
		{
			name:   "nested tuple",
			source: "vim9script\nvar values = ('a', ('b', 'c'))\nvalues[1][0] = 'x'\nvar after = 1\n",
			span:   "values[1][0]",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1532" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Cannot modify a tuple" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1532 diagnostics = %#v; syntax diagnostics = %#v", got, file.Diagnostics)
			}
			if file.Commands[len(file.Commands)-1].Declaration == nil {
				t.Fatalf("next-line recovery = %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nvar values = [1, 2]\nvalues[0] = 3\n",
		"vim9script\nvar values: any\nvalues[0] = 3\n",
		"vim9script\nvar values = ([1, 2], [3])\nvalues[-2][-2] = 5\n",
		"vim9script\nvar values = (1, 2)\nvalues[ : ] = (3, 4)\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1532" {
				t.Fatalf("guard source reported E1532: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1434GenericMethodTypeParameterCount(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "child has more type parameters",
			source: "vim9script\nclass A\n  def Fn<T>(t: T): T\n    return t\n  enddef\nendclass\nclass B extends A\n  def Fn<X, Y>(t: X): X\n    return t\n  enddef\nendclass\n",
		},
		{
			name:   "child has fewer type parameters",
			source: "vim9script\nclass A\n  def Fn<X, Y>(t: X): X\n    return t\n  enddef\nendclass\nclass B extends A\n  def Fn<T>(t: T): T\n    return t\n  enddef\nendclass\n",
		},
		{
			name:   "abstract parent",
			source: "vim9script\nabstract class A\n  abstract def Fn<T>(t: T): T\nendclass\nclass B extends A\n  def Fn<X, Y>(t: X): X\n    return t\n  enddef\nendclass\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1434" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != `Mismatched number of type variables for generic method  "Fn" in class "A"` || file.Text(got[0].Span) != "endclass" {
				t.Fatalf("E1434 diagnostics = %#v; syntax diagnostics = %#v", got, file.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nclass A\n  def Fn<T>(t: T): T\n    return t\n  enddef\nendclass\nclass B extends A\n  def Fn<X>(t: X): X\n    return t\n  enddef\nendclass\n",
		"vim9script\nclass B extends Missing\n  def Fn<X, Y>(t: X): X\n    return t\n  enddef\nendclass\n",
		"vim9script\nclass A\n  def Fn(t: number): number\n    return t\n  enddef\nendclass\nclass B extends A\n  def Fn<X>(t: X): X\n    return t\n  enddef\nendclass\n",
		"vim9script\nclass A\n  static def Fn<T>(t: T): T\n    return t\n  enddef\nendclass\nclass B extends A\n  static def Fn<X, Y>(t: X): X\n    return t\n  enddef\nendclass\n",
		"vim9script\nclass A extends B\n  def Fn<T>(t: T): T\n    return t\n  enddef\nendclass\nclass B extends A\nendclass\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1434" {
				t.Fatalf("guard source reported E1434: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1433GenericMethodOverridesConcreteMethod(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "concrete parent",
			source: "vim9script\nclass A\n  def Fn(t: number): number\n    return t\n  enddef\nendclass\nclass B extends A\n  def Fn<T>(t: T): T\n    return t\n  enddef\nendclass\n",
		},
		{
			name:   "abstract concrete parent method",
			source: "vim9script\nabstract class A\n  abstract def Fn(t: number): number\nendclass\nclass B extends A\n  def Fn<T>(t: T): T\n    return t\n  enddef\nendclass\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1433" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1434" {
					t.Fatalf("E1433 source also reported E1434: %#v", diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != `Overriding concrete method "Fn" in class "A" with a generic method` || file.Text(got[0].Span) != "endclass" {
				t.Fatalf("E1433 diagnostics = %#v; syntax diagnostics = %#v", got, file.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nclass A\n  def Fn<T>(t: T): T\n    return t\n  enddef\nendclass\nclass B extends A\n  def Fn<X>(t: X): X\n    return t\n  enddef\nendclass\n",
		"vim9script\nclass A\n  def Fn<T>(t: T): T\n    return t\n  enddef\nendclass\nclass B extends A\n  def Fn(t: number): number\n    return t\n  enddef\nendclass\n",
		"vim9script\nclass B extends Missing\n  def Fn<T>(t: T): T\n    return t\n  enddef\nendclass\n",
		"vim9script\nclass A\n  static def Fn(t: number): number\n    return t\n  enddef\nendclass\nclass B extends A\n  static def Fn<T>(t: T): T\n    return t\n  enddef\nendclass\n",
		"vim9script\nclass A\n  def _Fn(t: number): number\n    return t\n  enddef\nendclass\nclass B extends A\n  def Fn<T>(t: T): T\n    return t\n  enddef\nendclass\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1433" {
				t.Fatalf("guard source reported E1433: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1432ConcreteMethodOverridesGenericMethod(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "generic parent",
			source: "vim9script\nclass A\n  def Fn<T>(t: T): T\n    return t\n  enddef\nendclass\nclass B extends A\n  def Fn(t: number): number\n    return t\n  enddef\nendclass\n",
		},
		{
			name:   "abstract generic parent method",
			source: "vim9script\nabstract class A\n  abstract def Fn<T>(t: T): T\nendclass\nclass B extends A\n  def Fn(t: number): number\n    return t\n  enddef\nendclass\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1432" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1433" || diagnostic.Code == "vim/E1434" {
					t.Fatalf("E1432 source also reported another generic override error: %#v", diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != `Overriding generic method "Fn" in class "A" with a concrete method` || file.Text(got[0].Span) != "endclass" {
				t.Fatalf("E1432 diagnostics = %#v; syntax diagnostics = %#v", got, file.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nclass A\n  def Fn(t: number): number\n    return t\n  enddef\nendclass\nclass B extends A\n  def Fn(t: number): number\n    return t\n  enddef\nendclass\n",
		"vim9script\nclass A\n  def Fn<T>(t: T): T\n    return t\n  enddef\nendclass\nclass B extends A\n  def Fn<X>(t: X): X\n    return t\n  enddef\nendclass\n",
		"vim9script\nclass B extends Missing\n  def Fn(t: number): number\n    return t\n  enddef\nendclass\n",
		"vim9script\nclass A\n  static def Fn<T>(t: T): T\n    return t\n  enddef\nendclass\nclass B extends A\n  static def Fn(t: number): number\n    return t\n  enddef\nendclass\n",
		"vim9script\nclass A\n  def _Fn<T>(t: T): T\n    return t\n  enddef\nendclass\nclass B extends A\n  def Fn(t: number): number\n    return t\n  enddef\nendclass\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1432" {
				t.Fatalf("guard source reported E1432: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1382VariableTypeMismatch(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		message string
		span    string
	}{
		{
			name:    "class initializer",
			source:  "vim9script\nclass S\n  var l: list<string> = [1, 2, 3]\nendclass\n",
			message: `Variable "l": type mismatch, expected list<string> but got list<number>`, span: "[1, 2, 3]",
		},
		{
			name:    "extended interface",
			source:  "vim9script\ninterface A\n  var val: number\nendinterface\ninterface B extends A\n  var val: string\nendinterface\n",
			message: `Variable "val": type mismatch, expected number but got string`, span: "endinterface",
		},
		{
			name:    "implemented interface parent member",
			source:  "vim9script\ninterface I\n  var value: list<dict<number>>\nendinterface\nclass A\n  var value: list<dict<string>>\nendclass\nclass B extends A implements I\nendclass\n",
			message: `Variable "value": type mismatch, expected list<dict<number>> but got list<dict<string>>`, span: "endclass",
		},
		{
			name:    "implemented interface inferred member",
			source:  "vim9script\ninterface I\n  var value: list<dict<string>>\nendinterface\nclass C implements I\n  var value = {a: 1, b: 2}\nendclass\n",
			message: `Variable "value": type mismatch, expected list<dict<string>> but got dict<number>`, span: "endclass",
		},
		{
			name:    "function variable return",
			source:  "vim9script\ninterface I\n  var Callback: func(number): bool\nendinterface\nclass C implements I\n  var Callback: func(number): string\nendclass\n",
			message: `Variable "Callback": type mismatch, expected func(number): bool but got func(number): string`, span: "endclass",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			analysis := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range analysis.Diagnostics {
				if diagnostic.Code == "vim/E1382" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1382 diagnostics=%#v; syntax diagnostics=%#v", got, file.Diagnostics)
			}
			for _, diagnostic := range analysis.Diagnostics {
				if diagnostic.Code == "vim/E1012" {
					t.Fatalf("unexpected E1012 alongside E1382: %#v", diagnostic)
				}
			}
		})
	}

	for _, source := range []string{
		"vim9script\ninterface I\n  var value: list<dict<number>>\nendinterface\nclass C implements I\n  var value = [{a: 1}]\nendclass\n",
		"vim9script\nclass S\n  var l: list<string> = ['ok']\nendclass\n",
		"vim9script\nclass S\n  var value: float = 1\nendclass\n",
		"vim9script\nclass Base\nendclass\nclass Child extends Base\nendclass\ninterface I\n  var value: Base\nendinterface\nclass C implements I\n  var value: Child\nendclass\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1382" {
				t.Fatalf("guard source reported E1382: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1383MethodTypeMismatch(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		message string
	}{
		{
			name:    "interface return",
			source:  "vim9script\ninterface One\n  def IsEven(nr: number): bool\nendinterface\nclass Two implements One\n  def IsEven(nr: number): string\n  enddef\nendclass\n",
			message: `Method "IsEven": type mismatch, expected func(number): bool but got func(number): string`,
		},
		{
			name:    "interface argument",
			source:  "vim9script\ninterface One\n  def IsEven(nr: number): bool\nendinterface\nclass Two implements One\n  def IsEven(nr: bool): bool\n  enddef\nendclass\n",
			message: `Method "IsEven": type mismatch, expected func(number): bool but got func(bool): bool`,
		},
		{
			name:    "interface variadic",
			source:  "vim9script\ninterface One\n  def IsEven(nr: number): bool\nendinterface\nclass Two implements One\n  def IsEven(nr: number, ...extra: list<number>): bool\n  enddef\nendclass\n",
			message: `Method "IsEven": type mismatch, expected func(number): bool but got func(number, ...list<number>): bool`,
		},
		{
			name:    "inherited interface",
			source:  "vim9script\ninterface One\n  def IsEven(nr: number): bool\nendinterface\ninterface Child extends One\nendinterface\nclass Two implements Child\n  def IsEven(nr: bool): bool\n  enddef\nendclass\n",
			message: `Method "IsEven": type mismatch, expected func(number): bool but got func(bool): bool`,
		},
		{
			name:    "parent override",
			source:  "vim9script\nabstract class A\n  abstract def Foo(a: string, b: number): list<number>\nendclass\nclass B extends A\n  def Foo(a: number, b: string): list<string>\n    return []\n  enddef\nendclass\n",
			message: `Method "Foo": type mismatch, expected func(string, number): list<number> but got func(number, string): list<string>`,
		},
		{
			name:    "nested function return",
			source:  "vim9script\ninterface I\n  def Apply(Fn: func(number): bool)\nendinterface\nclass C implements I\n  def Apply(Fn: func(number): string)\n  enddef\nendclass\n",
			message: `Method "Apply": type mismatch, expected func(func(number): bool) but got func(func(number): string)`,
		},
		{
			name:    "object argument",
			source:  "vim9script\nclass B\nendclass\nabstract class A\n  abstract def Doit(value: B): B\nendclass\nclass C extends A\n  def Doit(value: C): B\n    return B.new()\n  enddef\nendclass\n",
			message: `Method "Doit": type mismatch, expected func(object<B>): object<B> but got func(object<C>): object<B>`,
		},
		{
			name:    "builtin empty protocol",
			source:  "vim9script\nclass A\n  def empty()\n  enddef\nendclass\n",
			message: `Method "empty": type mismatch, expected func(): bool but got func()`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1383" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != "endclass" {
				t.Fatalf("E1383 diagnostics=%#v; syntax diagnostics=%#v", got, file.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\ninterface I\n  def F(n: number): bool\nendinterface\nclass C implements I\n  def F(n: number): bool\n    return true\n  enddef\nendclass\n",
		"vim9script\nclass A\n  def empty(): bool\n    return true\n  enddef\n  def len(): number\n    return 1\n  enddef\n  def string(): string\n    return ''\n  enddef\nendclass\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1383" {
				t.Fatalf("guard source reported E1383: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1384InheritedClassMethodBareCall(t *testing.T) {
	source := "vim9script\nclass A\n  static def Foo()\n  enddef\nendclass\nclass B extends A\n  def Bar()\n    Foo()\n  enddef\nendclass\n"
	file := syntax.Parse(source)
	var got []syntax.Diagnostic
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vim/E1384" {
			got = append(got, diagnostic)
		}
	}
	if len(got) != 1 || got[0].Message != `Class method "Foo" accessible only inside class "A"` || file.Text(got[0].Span) != "Foo" {
		t.Fatalf("E1384 diagnostics=%#v; syntax diagnostics=%#v", got, file.Diagnostics)
	}

	for _, source := range []string{
		"vim9script\nclass A\n  static def Foo()\n  enddef\n  def Bar()\n    Foo()\n  enddef\nendclass\n",
		"vim9script\nclass A\n  static def Foo()\n  enddef\nendclass\nclass B extends A\n  static def Foo()\n  enddef\n  def Bar()\n    Foo()\n  enddef\nendclass\n",
		"vim9script\nclass A\n  static def Foo()\n  enddef\nendclass\nclass B extends A\n  def Bar()\n    A.Foo()\n  enddef\nendclass\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1384" {
				t.Fatalf("guard source reported E1384: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1366ProtectedMethodAccess(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "object call",
			source: "vim9script\nclass A\n  def _Foo()\n  enddef\nendclass\nvar a = A.new()\na._Foo()\n",
		},
		{
			name:   "object method reference",
			source: "vim9script\nclass A\n  def _Foo()\n  enddef\nendclass\nvar a = A.new()\nvar Fn = a._Foo\n",
		},
		{
			name:   "class alias",
			source: "vim9script\nclass A\n  static def _Foo()\n  enddef\nendclass\ntype Alias = A\nAlias._Foo()\n",
		},
		{
			name:   "object method through class",
			source: "vim9script\nclass A\n  def _Foo()\n  enddef\nendclass\nA._Foo()\n",
		},
		{
			name:   "class method through object",
			source: "vim9script\nclass A\n  static def _Foo()\n  enddef\nendclass\nvar a = A.new()\na._Foo()\n",
		},
		{
			name:   "protected static from child",
			source: "vim9script\nclass A\n  static def _Foo()\n  enddef\nendclass\nclass B extends A\n  static def Test()\n    A._Foo()\n  enddef\nendclass\n",
		},
		{
			name:   "protected object from unrelated class",
			source: "vim9script\nclass A\n  def _Foo()\n  enddef\nendclass\nclass B\n  def Test(a: A)\n    a._Foo()\n  enddef\nendclass\n",
		},
		{
			name:   "object method through class inside owner",
			source: "vim9script\nclass A\n  def _Foo()\n  enddef\n  static def Test()\n    A._Foo()\n  enddef\nendclass\n",
		},
		{
			name:   "class method through object inside owner",
			source: "vim9script\nclass A\n  static def _Foo()\n  enddef\n  def Test()\n    this._Foo()\n  enddef\nendclass\n",
		},
		{
			name:   "base class through child object",
			source: "vim9script\nclass A\n  def _Foo()\n  enddef\n  def Test(c: C)\n    c._Foo()\n  enddef\nendclass\nclass C extends A\nendclass\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1366" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E1385" || diagnostic.Code == "vim/E1386" {
					t.Fatalf("protected access reported lower-priority diagnostic: %#v", diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Cannot access protected method: _Foo" || file.Text(got[0].Span) != "_Foo" {
				t.Fatalf("E1366 diagnostics=%#v; syntax diagnostics=%#v", got, file.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nclass A\n  def _Foo()\n  enddef\n  def Test()\n    this._Foo()\n  enddef\nendclass\n",
		"vim9script\nclass A\n  def _Foo()\n  enddef\nendclass\nclass B extends A\n  def Test(a: A)\n    a._Foo()\n  enddef\nendclass\n",
		"vim9script\nclass A\n  static def _Foo()\n  enddef\n  def Test()\n    A._Foo()\n  enddef\nendclass\n",
		"vim9script\nclass A\n  static def _Foo()\n  enddef\nendclass\nclass B extends A\nendclass\nB._Foo()\n",
		"vim9script\nvar value: any\nvalue._Foo()\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1366" {
				t.Fatalf("guard source reported E1366: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1385ClassMethodThroughObject(t *testing.T) {
	for _, use := range []string{
		"var a = A.new()\na.Foo()",
		"def Test()\n  var a = A.new()\n  a.Foo\nenddef",
	} {
		source := "vim9script\nclass A\n  static def Foo()\n  enddef\nendclass\n" + use + "\n"
		file := syntax.Parse(source)
		var got []syntax.Diagnostic
		for _, diagnostic := range Analyze(file).Diagnostics {
			if diagnostic.Code == "vim/E1385" {
				got = append(got, diagnostic)
			}
		}
		if len(got) != 1 || got[0].Message != `Class method "Foo" accessible only using class "A"` || file.Text(got[0].Span) != "Foo" {
			t.Fatalf("use=%q E1385 diagnostics=%#v; syntax diagnostics=%#v", use, got, file.Diagnostics)
		}
	}

	for _, source := range []string{
		"vim9script\nclass A\n  static def Foo()\n  enddef\nendclass\nA.Foo()\n",
		"vim9script\nclass A\n  def Foo()\n  enddef\nendclass\nvar a = A.new()\na.Foo()\n",
		"vim9script\nclass A\n  static def _Foo()\n  enddef\nendclass\nvar a = A.new()\na._Foo()\n",
		"vim9script\nclass A\n  static def Foo()\n  enddef\nendclass\nclass B extends A\nendclass\nvar b = B.new()\nb.Foo()\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1385" {
				t.Fatalf("guard source reported E1385: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1386ObjectMethodThroughClass(t *testing.T) {
	for _, use := range []string{"A.Foo()", "def Test()\n  A.Foo\nenddef"} {
		source := "vim9script\nclass A\n  def Foo()\n  enddef\nendclass\n" + use + "\n"
		file := syntax.Parse(source)
		var got []syntax.Diagnostic
		for _, diagnostic := range Analyze(file).Diagnostics {
			if diagnostic.Code == "vim/E1386" {
				got = append(got, diagnostic)
			}
		}
		if len(got) != 1 || got[0].Message != `Object method "Foo" accessible only using class "A" object` || file.Text(got[0].Span) != "Foo" {
			t.Fatalf("use=%q E1386 diagnostics=%#v; syntax diagnostics=%#v", use, got, file.Diagnostics)
		}
	}

	for _, source := range []string{
		"vim9script\nclass A\n  static def Foo()\n  enddef\nendclass\nA.Foo()\n",
		"vim9script\nclass A\n  def Foo()\n  enddef\nendclass\nvar a = A.new()\na.Foo()\n",
		"vim9script\nclass A\n  def _Foo()\n  enddef\nendclass\nA._Foo()\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1386" {
				t.Fatalf("guard source reported E1386: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1376ObjectVariableThroughClass(t *testing.T) {
	for _, use := range []string{
		"var value = A.val",
		"A.val = 2",
		"def Test()\n  var value = A.val\nenddef",
		"def Test()\n  A.val = 2\nenddef",
		"type Alias = A\nvar value = Alias.val",
	} {
		source := "vim9script\nclass A\n  public var val: number = 1\nendclass\n" + use + "\n"
		file := syntax.Parse(source)
		var got []syntax.Diagnostic
		for _, diagnostic := range Analyze(file).Diagnostics {
			if diagnostic.Code == "vim/E1376" {
				got = append(got, diagnostic)
			}
		}
		if len(got) != 1 || got[0].Message != `Object variable "val" accessible only using class "A" object` || file.Text(got[0].Span) != "val" {
			t.Fatalf("use=%q E1376 diagnostics=%#v; syntax diagnostics=%#v", use, got, file.Diagnostics)
		}
	}

	inherited := syntax.Parse("vim9script\nclass A\n  var val: number = 1\nendclass\nclass B extends A\nendclass\nvar value = B.val\n")
	var inheritedDiagnostics []syntax.Diagnostic
	for _, diagnostic := range Analyze(inherited).Diagnostics {
		if diagnostic.Code == "vim/E1376" {
			inheritedDiagnostics = append(inheritedDiagnostics, diagnostic)
		}
	}
	if len(inheritedDiagnostics) != 1 || inheritedDiagnostics[0].Message != `Object variable "val" accessible only using class "B" object` {
		t.Fatalf("inherited E1376 diagnostics=%#v", inheritedDiagnostics)
	}

	for _, source := range []string{
		"vim9script\nclass A\n  static var val: number = 1\nendclass\nvar value = A.val\n",
		"vim9script\nclass A\n  public var val: number = 1\nendclass\nvar a = A.new()\nvar value = a.val\n",
		"vim9script\nclass A\nendclass\nvar value = A.missing\n",
		"vim9script\nclass A\n  def Foo()\n  enddef\nendclass\nA.Foo()\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1376" {
				t.Fatalf("guard source reported E1376: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1375ClassVariableThroughObject(t *testing.T) {
	for _, use := range []string{
		"var value = a.count",
		"var value = A.new().count",
		"a.count = 2",
		"def Test(a: A)\n  var value = a.count\nenddef",
		"def Test(a: A)\n  a.count = 2\nenddef",
		"var value = a._count",
	} {
		source := "vim9script\nclass A\n  static var count: number = 1\n  static var _count: number = 1\nendclass\nvar a = A.new()\n" + use + "\n"
		file := syntax.Parse(source)
		memberName := "count"
		if strings.Contains(use, "_count") {
			memberName = "_count"
		}
		var got []syntax.Diagnostic
		for _, diagnostic := range Analyze(file).Diagnostics {
			if diagnostic.Code == "vim/E1375" {
				got = append(got, diagnostic)
			}
		}
		wantMessage := `Class variable "` + memberName + `" accessible only using class "A"`
		if len(got) != 1 || got[0].Message != wantMessage || file.Text(got[0].Span) != memberName {
			t.Fatalf("use=%q E1375 diagnostics=%#v; syntax diagnostics=%#v", use, got, file.Diagnostics)
		}
	}

	for _, source := range []string{
		"vim9script\nclass A\n  static var count: number = 1\nendclass\nvar value = A.count\n",
		"vim9script\nclass A\n  static var count: number = 1\nendclass\ntype Alias = A\nvar value = Alias.count\n",
		"vim9script\nclass A\n  var count: number = 1\nendclass\nvar a = A.new()\nvar value = a.count\n",
		"vim9script\nclass A\n  static def Count()\n  enddef\nendclass\nvar a = A.new()\na.Count()\n",
		"vim9script\nclass A\n  static var count: number = 1\nendclass\nclass B extends A\nendclass\nvar b = B.new()\nvar value = b.count\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1375" {
				t.Fatalf("guard source reported E1375: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1374InheritedClassVariableBareAccess(t *testing.T) {
	for _, method := range []string{
		"static def Test()\n    var value = count\n  enddef",
		"static def Test()\n    count = 2\n  enddef",
		"def Test()\n    var value = count\n  enddef",
		"def Test()\n    count = 2\n  enddef",
	} {
		source := "vim9script\nclass A\n  static var count: number = 1\nendclass\nclass B extends A\n  " + method + "\nendclass\n"
		file := syntax.Parse(source)
		var got []syntax.Diagnostic
		for _, diagnostic := range Analyze(file).Diagnostics {
			if diagnostic.Code == "vim/E1374" {
				got = append(got, diagnostic)
			}
			if diagnostic.Code == "vim/E1001" || diagnostic.Code == "vim/E1089" {
				t.Fatalf("method=%q also reported generic unresolved diagnostic: %#v", method, diagnostic)
			}
		}
		if len(got) != 1 || got[0].Message != `Class variable "count" accessible only inside class "A"` || file.Text(got[0].Span) != "count" {
			t.Fatalf("method=%q E1374 diagnostics=%#v; syntax diagnostics=%#v", method, got, file.Diagnostics)
		}
	}

	protected := syntax.Parse("vim9script\nclass A\n  static var _count: number = 1\nendclass\nclass B extends A\n  static def Test()\n    var value = _count\n  enddef\nendclass\n")
	var protectedDiagnostics []syntax.Diagnostic
	for _, diagnostic := range Analyze(protected).Diagnostics {
		if diagnostic.Code == "vim/E1374" {
			protectedDiagnostics = append(protectedDiagnostics, diagnostic)
		}
	}
	if len(protectedDiagnostics) != 1 || protectedDiagnostics[0].Message != `Class variable "_count" accessible only inside class "A"` || protected.Text(protectedDiagnostics[0].Span) != "_count" {
		t.Fatalf("protected E1374 diagnostics=%#v", protectedDiagnostics)
	}

	for _, source := range []string{
		"vim9script\nclass A\n  static var count: number = 1\n  static def Test()\n    var value = count\n  enddef\nendclass\n",
		"vim9script\nclass A\n  public static var count: number = 1\nendclass\nclass B extends A\n  static def Test()\n    var value = A.count\n  enddef\nendclass\n",
		"vim9script\nclass A\n  static var count: number = 1\nendclass\nclass B extends A\n  static var count: number = 2\n  static def Test()\n    var value = count\n  enddef\nendclass\n",
		"vim9script\nclass A\n  static var count: number = 1\nendclass\nclass B extends A\n  def Test(count: number)\n    var value = count\n  enddef\nendclass\n",
		"vim9script\nclass A\n  var count: number = 1\nendclass\nclass B extends A\n  def Test()\n    var value = count\n  enddef\nendclass\n",
		"vim9script\nclass A extends B\nendclass\nclass B extends A\n  static def Test()\n    var value = count\n  enddef\nendclass\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1374" {
				t.Fatalf("guard source reported E1374: %#v\n%s", diagnostic, source)
			}
		}
	}
}

func TestAnalyzeE1373UnimplementedAbstractMethod(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
	}{
		{
			name:   "direct parent",
			source: "vim9script\nabstract class A\n  abstract def Foo()\nendclass\nclass B extends A\nendclass\n",
		},
		{
			name:   "transitive parent",
			source: "vim9script\nabstract class A\n  abstract def Foo()\nendclass\nabstract class B extends A\n  abstract def Bar()\nendclass\nclass C extends B\n  def Bar()\n  enddef\nendclass\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1373" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != `Abstract method "Foo" is not implemented` || file.Text(got[0].Span) != "endclass" {
				t.Fatalf("E1373 diagnostics=%#v; syntax diagnostics=%#v", got, file.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nabstract class A\n  abstract def Foo()\nendclass\nclass B extends A\n  def Foo()\n  enddef\nendclass\n",
		"vim9script\nabstract class A\n  abstract def Foo()\nendclass\nabstract class B extends A\nendclass\n",
		"vim9script\nabstract class A\n  abstract def Foo()\nendclass\nabstract class B extends A\n  def Foo()\n  enddef\nendclass\nclass C extends B\nendclass\n",
		"vim9script\nabstract class A\n  abstract def Foo(value: number)\nendclass\nclass B extends A\n  def Foo(value: string)\n  enddef\nendclass\n",
		"vim9script\nabstract class A\n  abstract def Foo()\nendclass\nclass B extends A\n  def _Foo()\n  enddef\nendclass\n",
		"vim9script\nabstract class A\n  abstract def Foo()\n  abstract def Bar()\nendclass\nclass B extends A\n  abstract def Foo()\nendclass\n",
		"vim9script\nabstract class A\n  abstract def Foo()\nendclass\nclass B extends A\n  public def Foo()\n  enddef\nendclass\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1373" {
				t.Fatalf("guard source reported E1373: %#v\n%s", diagnostic, source)
			}
		}
	}
	Analyze(syntax.Parse("vim9script\nclass A extends B\nendclass\nabstract class B extends A\n  abstract def Foo()\nendclass\n"))
}

func TestAnalyzeE1431AbstractSuperMethodCall(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		message string
		span    string
	}{
		{
			name:    "generic call",
			source:  "vim9script\nabstract class A\n  abstract def F1()\nendclass\nclass B extends A\n  def F1()\n  enddef\n  def Foo()\n    super.F1<number, string>()\n  enddef\nendclass\n",
			message: `Abstract method "F1" in class "A" cannot be accessed directly`, span: "F1",
		},
		{
			name:    "direct abstract parent",
			source:  "vim9script\nabstract class B\n  abstract def ToString(): string\nendclass\nclass C extends B\n  def ToString(): string\n    return super.ToString()\n  enddef\nendclass\n",
			message: `Abstract method "ToString" in class "B" cannot be accessed directly`, span: "ToString",
		},
		{
			name:    "nearest abstract override",
			source:  "vim9script\nclass A\n  def ToString(): string\n    return 'A'\n  enddef\nendclass\nabstract class B extends A\n  abstract def ToString(): string\nendclass\nclass C extends B\n  def ToString(): string\n    return super.ToString()\n  enddef\nendclass\n",
			message: `Abstract method "ToString" in class "B" cannot be accessed directly`, span: "ToString",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			var got []syntax.Diagnostic
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1431" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("E1431 diagnostics = %#v; syntax diagnostics = %#v", got, file.Diagnostics)
			}
		})
	}

	for _, source := range []string{
		"vim9script\nabstract class A\n  abstract def F1()\nendclass\nclass B extends A\n  def F1()\n  enddef\n  def Foo()\n    this.F1()\n  enddef\nendclass\n",
		"vim9script\nabstract class A\n  abstract def F1()\nendclass\nclass B extends A\n  def F1()\n  enddef\nendclass\nclass C extends B\n  def F1()\n    super.F1()\n  enddef\nendclass\n",
		"vim9script\nclass B extends Missing\n  def F1()\n    super.F1()\n  enddef\nendclass\n",
		"vim9script\nabstract class A\n  abstract def F1()\nendclass\ndef Outside()\n  super.F1()\nenddef\n",
		"vim9script\nabstract class A\n  abstract def F1()\nendclass\nclass B extends A\n  def F1()\n  enddef\n  def Foo()\n    super.()\n  enddef\nendclass\n",
	} {
		for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
			if diagnostic.Code == "vim/E1431" {
				t.Fatalf("guard source reported E1431: %#v\n%s", diagnostic, source)
			}
		}
	}
}
