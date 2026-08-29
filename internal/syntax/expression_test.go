package syntax

import (
	"strings"
	"testing"
)

func TestExpressionLexerTokenGolden(t *testing.T) {
	tests := []struct {
		name    string
		dialect Dialect
		source  string
		want    []struct {
			kind  expressionTokenKind
			text  string
			start int
			end   int
		}
	}{
		{
			name:    "legacy comments strings numbers and operators",
			dialect: Legacy,
			source:  "prefix\n  \"ignored | #{}\n'a|#{}' 0xFF 1'000 0zAABB isnot# >=#",
			want: []struct {
				kind  expressionTokenKind
				text  string
				start int
				end   int
			}{
				{expressionIdentifier, "prefix", 0, 6},
				{expressionString, "'a|#{}'", 24, 31},
				{expressionNumber, "0xFF", 32, 36},
				{expressionNumber, "1'000", 37, 42},
				{expressionBlob, "0zAABB", 43, 49},
				{expressionOperator, "isnot#", 50, 56},
				{expressionOperator, ">=#", 57, 60},
			},
		},
		{
			name:    "vim9 comments dictionary string and operators",
			dialect: Vim9,
			source:  "\"text|/#{}\"\n#{key: 1}\n#{{ignored | {}}\nfoo && bar ?? baz -> qux",
			want: []struct {
				kind  expressionTokenKind
				text  string
				start int
				end   int
			}{
				{expressionString, "\"text|/#{}\"", 0, 11},
				{expressionPunctuation, "#{", 12, 14},
				{expressionIdentifier, "key", 14, 17},
				{expressionOperator, ":", 17, 18},
				{expressionNumber, "1", 19, 20},
				{expressionPunctuation, "}", 20, 21},
				{expressionIdentifier, "foo", 39, 42},
				{expressionOperator, "&&", 43, 45},
				{expressionIdentifier, "bar", 46, 49},
				{expressionOperator, "??", 50, 52},
				{expressionIdentifier, "baz", 53, 56},
				{expressionOperator, "->", 57, 59},
				{expressionIdentifier, "qux", 60, 63},
			},
		},
	}
	for _, test := range tests {
		for _, base := range []int{0, 100} {
			label := "base=0"
			if base != 0 {
				label = "base=100"
			}
			t.Run(test.name+"/"+label, func(t *testing.T) {
				tokens := lexExpression(test.source, base, test.dialect)
				if len(tokens) != len(test.want)+1 {
					t.Fatalf("tokens = %#v, want %d tokens", tokens, len(test.want)+1)
				}
				for index, want := range test.want {
					token := tokens[index]
					if token.kind != want.kind || token.text != want.text {
						t.Fatalf("token %d = %#v, want kind=%d text=%q", index, token, want.kind, want.text)
					}
					wantSpan := Span{Start: base + want.start, End: base + want.end}
					if token.span != wantSpan {
						t.Fatalf("token %d span = %#v, want %#v", index, token.span, wantSpan)
					}
				}
				eof := tokens[len(tokens)-1]
				wantEOF := Span{Start: base + len(test.source), End: base + len(test.source)}
				if eof.kind != expressionEOF || eof.text != "" || eof.span != wantEOF {
					t.Fatalf("EOF = %#v, want %#v", eof, expressionToken{kind: expressionEOF, span: wantEOF})
				}
			})
		}
	}
}

func TestLongestOperatorSet(t *testing.T) {
	operators := []string{
		"isnot#", "isnot?", "isnot", ">=#", ">=?", "<=#", "<=?", "==#", "==?", "!=#", "!=?", "=~#", "=~?", "!~#", "!~?",
		"->", "=>", "..", "&&", "||", "??", ">#", ">?", "<#", "<?", "<=", ">=", "==", "!=", "=~", "!~", "+=", "-=", "*=", "/=", "%=", "<<", ">>", "**",
		"is#", "is?", "is", "+", "-", "*", "/", "%", ".", "!", "<", ">", "?", ":", "=",
	}
	for _, operator := range operators {
		if got := longestOperator(operator + "tail"); got != operator {
			t.Errorf("longestOperator(%q) = %q, want %q", operator+"tail", got, operator)
		}
	}
	for _, source := range []string{"", "@register", "中文"} {
		if got := longestOperator(source); got != "" {
			t.Errorf("longestOperator(%q) = %q, want empty", source, got)
		}
	}
}

func TestExpressionParserCursorFixtures(t *testing.T) {
	tests := []struct {
		name       string
		dialect    Dialect
		source     string
		base       int
		kind       ExpressionKind
		value      string
		wantDiag   string
		wantDiags  int
		checkExtra func(*testing.T, *Expression)
	}{
		{
			name:    "legacy curly name call",
			dialect: Legacy,
			source:  `vimtex#compiler#{l:method}#init(l:options)`,
			base:    100,
			kind:    ExpressionCall,
			checkExtra: func(t *testing.T, expression *Expression) {
				if expression.Span != (Span{Start: 100, End: 100 + len(`vimtex#compiler#{l:method}#init(l:options)`)}) || expression.Children[0].Kind != ExpressionCurlyName {
					t.Fatalf("curly call = %#v", expression)
				}
			},
		},
		{
			name:    "vim9 generic call",
			dialect: Vim9,
			source:  `factory.Get<list<number>>(items)`,
			kind:    ExpressionCall,
			checkExtra: func(t *testing.T, expression *Expression) {
				if len(expression.TypeArguments) != 1 || expression.TypeArguments[0].Name != "list" {
					t.Fatalf("generic call = %#v", expression)
				}
			},
		},
		{
			name:    "vim9 comparison",
			dialect: Vim9,
			source:  `left < number > (right)`,
			kind:    ExpressionBinary,
			value:   ">",
			checkExtra: func(t *testing.T, expression *Expression) {
				if len(expression.Children) != 2 || expression.Children[0].Value != "<" {
					t.Fatalf("comparison = %#v", expression)
				}
			},
		},
		{
			name:    "interpolated nested braces",
			dialect: Vim9,
			source:  `$'legacy set{local ? 'l' : ''} {name}'`,
			kind:    ExpressionInterpolatedString,
			checkExtra: func(t *testing.T, expression *Expression) {
				if len(expression.Children) != 2 || expression.Children[0].Kind != ExpressionTernary || expression.Children[1].Kind != ExpressionIdentifier {
					t.Fatalf("interpolated = %#v", expression)
				}
			},
		},
		{
			name:      "malformed EOF",
			dialect:   Vim9,
			source:    `Func([1, 2`,
			kind:      ExpressionCall,
			wantDiag:  "vimls/missing-delimiter",
			wantDiags: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expression, diagnostics, consumed := parseExpressionPrefix(test.source, test.base, test.dialect)
			if expression == nil || expression.Kind != test.kind || test.value != "" && expression.Value != test.value {
				t.Fatalf("expression = %#v, want kind=%d value=%q", expression, test.kind, test.value)
			}
			if consumed != len(test.source) {
				t.Fatalf("consumed = %d, want %d", consumed, len(test.source))
			}
			if test.wantDiag == "" {
				if len(diagnostics) != 0 {
					t.Fatalf("diagnostics = %#v", diagnostics)
				}
			} else {
				wantDiags := test.wantDiags
				if wantDiags == 0 {
					wantDiags = 1
				}
				if len(diagnostics) != wantDiags {
					t.Fatalf("diagnostics = %#v, want %d %q diagnostics", diagnostics, wantDiags, test.wantDiag)
				}
				for _, diagnostic := range diagnostics {
					if diagnostic.Code != test.wantDiag {
						t.Fatalf("diagnostics = %#v, want only %q", diagnostics, test.wantDiag)
					}
				}
			}
			if test.checkExtra != nil {
				test.checkExtra(t, expression)
			}
		})
	}
}

func TestVim9UnterminatedStrings(t *testing.T) {
	for _, test := range []struct {
		expression string
		code       string
		kind       ExpressionKind
	}{
		{expression: `"abc`, code: "vim/E114", kind: ExpressionString},
		{expression: `'abc`, code: "vim/E115", kind: ExpressionString},
		{expression: `$"foo`, code: "vim/E114", kind: ExpressionInterpolatedString},
		{expression: `$'foo`, code: "vim/E115", kind: ExpressionInterpolatedString},
	} {
		for _, source := range []string{
			"def F()\nvar value = " + test.expression + "\nvar after = 1\nenddef\n",
			"vim9script\nvar value = " + test.expression + "\nvar after = 1\n",
		} {
			file := Parse(source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || file.Text(file.Diagnostics[0].Span) != test.expression {
				t.Fatalf("expression %q diagnostics = %#v", test.expression, file.Diagnostics)
			}
			if len(file.Commands) < 3 || file.Commands[1].Declaration == nil {
				t.Fatalf("expression %q commands = %#v", test.expression, file.Commands)
			}
			initializer := file.Commands[1].Declaration.Initializer
			if initializer == nil || initializer.Kind != test.kind || file.Text(initializer.Span) != test.expression {
				t.Fatalf("expression %q initializer = %#v", test.expression, initializer)
			}
			if file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "after" {
				t.Fatalf("expression %q swallowed recovery command: %#v", test.expression, file.Commands)
			}
			assertFileSpans(t, file)
		}
	}
	for _, expression := range []string{`"abc"`, `'abc'`, `$"foo"`, `$'foo'`} {
		if _, diagnostics := (Vim9ExpressionParser{}).Parse(expression); len(diagnostics) != 0 {
			t.Fatalf("valid Vim9 expression %q diagnostics = %#v", expression, diagnostics)
		}
	}
	for _, expression := range []string{`"abc"`, `'abc'`} {
		if _, diagnostics := (LegacyExpressionParser{}).Parse(expression); len(diagnostics) != 0 {
			t.Fatalf("valid legacy expression %q diagnostics = %#v", expression, diagnostics)
		}
	}
}

func TestExpressionContinuationAfterCommentHasAbsoluteSpan(t *testing.T) {
	source := "value\n  # full line comment\n  .name"
	expression, diagnostics, consumed := parseExpressionPrefix(source, 100, Vim9)
	if len(diagnostics) != 0 || consumed != len(source) || expression.Kind != ExpressionMember || expression.Value != "name" {
		t.Fatalf("expression = %#v, diagnostics = %#v, consumed = %d", expression, diagnostics, consumed)
	}
	if expression.Span != (Span{Start: 100, End: 100 + len(source)}) || expression.Children[0].Span != (Span{Start: 100, End: 105}) {
		t.Fatalf("absolute continuation spans = %#v", expression)
	}
}

func TestLegacyExpressionParserMatchesVimPrecedence(t *testing.T) {
	expression, diagnostics := (LegacyExpressionParser{}).Parse(`condition ? "top" : 1 << 2 + 3 * 4`)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if expression.Kind != ExpressionTernary || len(expression.Children) != 3 {
		t.Fatalf("ternary = %#v", expression)
	}
	shift := expression.Children[2]
	if shift.Kind != ExpressionBinary || shift.Value != "<<" || shift.Children[1].Value != "+" || shift.Children[1].Children[1].Value != "*" {
		t.Fatalf("precedence tree = %#v", shift)
	}

	concatenation, diagnostics := (LegacyExpressionParser{}).Parse(`"a" . "b" .. 3`)
	if len(diagnostics) != 0 || concatenation.Kind != ExpressionBinary || concatenation.Value != ".." || concatenation.Children[0].Value != "." {
		t.Fatalf("concatenation = %#v, diagnostics = %#v", concatenation, diagnostics)
	}
}

func TestVim9TernarySyntaxDiagnostics(t *testing.T) {
	expression, diagnostics := (Vim9ExpressionParser{}).Parse("1 ? 'one'")
	if len(diagnostics) != 1 || diagnostics[0].Code != "vim/E109" || diagnostics[0].Message != "Missing ':' after '?'" || diagnostics[0].Span != (Span{Start: 9, End: 9}) {
		t.Fatalf("missing colon diagnostics = %#v", diagnostics)
	}
	if expression.Kind != ExpressionTernary || len(expression.Children) != 2 || expression.Children[0].Value != "1" || expression.Children[1].Kind != ExpressionString {
		t.Fatalf("incomplete ternary = %#v", expression)
	}

	file := Parse("vim9script\nvar x = 1 ? 'one'\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E109" || file.Diagnostics[0].Span.Start != file.Diagnostics[0].Span.End || file.Text(file.Diagnostics[0].Span) != "" || len(file.Commands) != 3 || file.Commands[1].Declaration == nil || file.Commands[1].Declaration.Initializer == nil || file.Commands[1].Declaration.Initializer.Kind != ExpressionTernary || len(file.Commands[1].Declaration.Initializer.Children) != 2 || file.Commands[2].Declaration == nil {
		t.Fatalf("command recovery = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	assertFileSpans(t, file)

	for _, test := range []struct {
		source string
		span   string
	}{
		{source: "1? 'one' : 'two'", span: "?"},
		{source: "1 ?'one' : 'two'", span: "?"},
		{source: "1?'one' : 'two'", span: "?"},
		{source: "1 ? 'one': 'two'", span: ":"},
		{source: "1 ? 'one' :'two'", span: ":"},
		{source: "1 ? 'one':'two'", span: ":"},
	} {
		expression, diagnostics := (Vim9ExpressionParser{}).Parse(test.source)
		if len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1004" || test.source[diagnostics[0].Span.Start:diagnostics[0].Span.End] != test.span || expression.Kind != ExpressionTernary || len(expression.Children) != 3 || expression.Children[0].Value != "1" || expression.Children[1].Kind != ExpressionString || expression.Children[2].Kind != ExpressionString {
			t.Fatalf("%q = %#v, diagnostics = %#v", test.source, expression, diagnostics)
		}
	}

	legacy, diagnostics := (LegacyExpressionParser{}).Parse("1 ? 'one'")
	if len(diagnostics) != 1 || diagnostics[0].Code != "vimls/missing-ternary-colon" || legacy.Kind != ExpressionTernary || len(legacy.Children) != 2 {
		t.Fatalf("legacy missing colon = %#v, diagnostics = %#v", legacy, diagnostics)
	}
}

func TestSeparateLegacyAndVim9LambdaSyntax(t *testing.T) {
	legacy, diagnostics := (LegacyExpressionParser{}).Parse(`{key, value -> key .. value}`)
	if len(diagnostics) != 0 || legacy.Kind != ExpressionLambda || legacy.Value != "" || len(legacy.Parameters) != 2 || len(legacy.Children) != 3 {
		t.Fatalf("legacy lambda = %#v, diagnostics = %#v", legacy, diagnostics)
	}
	vim9, diagnostics := (Vim9ExpressionParser{}).Parse(`(left, right) => left .. right`)
	if len(diagnostics) != 0 || vim9.Kind != ExpressionLambda || len(vim9.Children) != 3 || vim9.Children[2].Value != ".." {
		t.Fatalf("Vim9 lambda = %#v, diagnostics = %#v", vim9, diagnostics)
	}

	_, diagnostics = (Vim9ExpressionParser{}).Parse(`"a" . "b"`)
	if len(diagnostics) == 0 || diagnostics[0].Code != "vimls/trailing-expression" {
		t.Fatalf("Vim9 legacy concatenation diagnostics = %#v", diagnostics)
	}
}

func TestVim9CallArgumentCommaWhitespace(t *testing.T) {
	for _, test := range []struct {
		name   string
		call   string
		code   string
		legacy string
	}{
		{name: "space before comma", call: "match(['foo'] , 'foo')", code: "vim/E1068", legacy: "match(['foo'] , 'foo')"},
		{name: "missing space after comma", call: "CallMe2('yes','no')", code: "vim/E1069", legacy: "CallMe2('yes','no')"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, source := range []string{
				"def F()\necho " + test.call + "\nvar after = 1\nenddef\n",
				"vim9script\necho " + test.call + "\nvar after = 1\n",
			} {
				file := Parse(source)
				if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || file.Text(file.Diagnostics[0].Span) != "," {
					t.Fatalf("diagnostics = %#v", file.Diagnostics)
				}
				if len(file.Commands) < 3 || len(file.Commands[1].Expressions) != 1 {
					t.Fatalf("commands = %#v", file.Commands)
				}
				call := file.Commands[1].Expressions[0]
				if call.Kind != ExpressionCall || len(call.Children) != 3 || call.Children[0].Kind != ExpressionIdentifier || call.Children[1].Kind == ExpressionMissing || call.Children[2].Kind == ExpressionMissing || test.code == "vim/E1068" && call.Children[1].Kind != ExpressionList {
					t.Fatalf("call = %#v", call)
				}
				if file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "after" {
					t.Fatalf("following declaration was swallowed: %#v", file.Commands)
				}
				assertFileSpans(t, file)
			}
			legacy, diagnostics := (LegacyExpressionParser{}).Parse(test.legacy)
			if len(diagnostics) != 0 || legacy.Kind != ExpressionCall || len(legacy.Children) != 3 {
				t.Fatalf("legacy = %#v, diagnostics = %#v", legacy, diagnostics)
			}
		})
	}
}

func TestVim9ListCommaWhitespace(t *testing.T) {
	for _, test := range []struct {
		name string
		expr string
		code string
	}{
		{name: "missing space after comma", expr: "[1,2,3]", code: "vim/E1069"},
		{name: "space before comma", expr: "[1 ,2, 3]", code: "vim/E1068"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, source := range []string{
				"def F()\nvar value = " + test.expr + "\nvar after = 1\nenddef\n",
				"vim9script\nvar value = " + test.expr + "\nvar after = 1\n",
			} {
				file := Parse(source)
				if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || file.Text(file.Diagnostics[0].Span) != "," {
					t.Fatalf("diagnostics = %#v", file.Diagnostics)
				}
				if len(file.Commands) < 3 || file.Commands[1].Declaration == nil || file.Commands[1].Declaration.Initializer == nil || file.Commands[1].Declaration.Initializer.Kind != ExpressionList || len(file.Commands[1].Declaration.Initializer.Children) != 3 {
					t.Fatalf("commands = %#v", file.Commands)
				}
				if file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "after" {
					t.Fatalf("following declaration was swallowed: %#v", file.Commands)
				}
				assertFileSpans(t, file)
			}
			_, diagnostics := (LegacyExpressionParser{}).Parse(test.expr)
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == "vim/E1068" || diagnostic.Code == "vim/E1069" {
					t.Fatalf("legacy diagnostics = %#v", diagnostics)
				}
			}
		})
	}
}

func TestVim9LambdaParameterCommaWhitespace(t *testing.T) {
	for _, source := range []string{
		"def F()\nvar value = filter([1], (k,v) => 1)\nvar after = 1\nenddef\n",
		"vim9script\nvar value = filter([1], (k,v) => 1)\nvar after = 1\n",
	} {
		file := Parse(source)
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1069" || file.Text(file.Diagnostics[0].Span) != "," {
			t.Fatalf("diagnostics = %#v", file.Diagnostics)
		}
		if len(file.Commands) < 3 || file.Commands[1].Declaration == nil {
			t.Fatalf("commands = %#v", file.Commands)
		}
		call := file.Commands[1].Declaration.Initializer
		if call == nil || call.Kind != ExpressionCall || len(call.Children) != 3 || call.Children[1].Kind != ExpressionList {
			t.Fatalf("call = %#v", call)
		}
		lambda := call.Children[2]
		if lambda.Kind != ExpressionLambda || len(lambda.Parameters) != 2 || len(lambda.Children) != 3 || lambda.Operator.Start == lambda.Operator.End || lambda.Children[2].Kind != ExpressionNumber {
			t.Fatalf("lambda = %#v", lambda)
		}
		if file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "after" {
			t.Fatalf("following declaration was swallowed: %#v", file.Commands)
		}
		assertFileSpans(t, file)
	}
	_, diagnostics := (LegacyExpressionParser{}).Parse("{k,v -> 1}")
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "vim/E1068" || diagnostic.Code == "vim/E1069" {
			t.Fatalf("legacy diagnostics = %#v", diagnostics)
		}
	}
}

func TestVim9LambdaArrowWhitespace(t *testing.T) {
	for _, expression := range []string{"(a)=>a + 1", "(a)=> a + 1", "(a) =>a + 1"} {
		for _, source := range []string{
			"def F()\nvar value = map([1], " + expression + ")\nvar after = 1\nenddef\n",
			"vim9script\nvar value = map([1], " + expression + ")\nvar after = 1\n",
		} {
			file := Parse(source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1004" || file.Text(file.Diagnostics[0].Span) != "=>" {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			if len(file.Commands) < 3 || file.Commands[1].Declaration == nil {
				t.Fatalf("commands = %#v", file.Commands)
			}
			call := file.Commands[1].Declaration.Initializer
			if call == nil || call.Kind != ExpressionCall || len(call.Children) != 3 || call.Children[1].Kind != ExpressionList {
				t.Fatalf("call = %#v", call)
			}
			lambda := call.Children[2]
			if lambda.Kind != ExpressionLambda || len(lambda.Parameters) != 1 || len(lambda.Children) != 2 || lambda.Operator.Start == lambda.Operator.End || lambda.Children[1].Kind != ExpressionBinary {
				t.Fatalf("lambda = %#v", lambda)
			}
			if file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "after" {
				t.Fatalf("following declaration was swallowed: %#v", file.Commands)
			}
			assertFileSpans(t, file)
		}
	}
	for _, source := range []string{"(a) => a + 1", "(a) => {\n  return a\n}"} {
		if _, diagnostics := (Vim9ExpressionParser{}).Parse(source); len(diagnostics) != 0 {
			t.Fatalf("valid lambda %q diagnostics = %#v", source, diagnostics)
		}
	}
	_, diagnostics := (Vim9ExpressionParser{}).Parse("(a) =>")
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "vim/E1004" {
			t.Fatalf("incomplete arrow diagnostics = %#v", diagnostics)
		}
	}
	_, diagnostics = (LegacyExpressionParser{}).Parse("{a -> a + 1}")
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "vim/E1004" {
			t.Fatalf("legacy diagnostics = %#v", diagnostics)
		}
	}
}

func TestVim9DictionaryDelimiterWhitespace(t *testing.T) {
	for _, test := range []struct {
		name     string
		expr     string
		code     string
		children int
	}{
		{name: "space before colon", expr: "{a : 8}", code: "vim/E1068", children: 2},
		{name: "missing space after colon", expr: "{a:8}", code: "vim/E1069", children: 2},
		{name: "space before comma", expr: "{a: 8 , b: 9}", code: "vim/E1068", children: 4},
		{name: "missing space after comma", expr: "{a: 1,b: 2}", code: "vim/E1069", children: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, source := range []string{
				"def F()\nvar value = " + test.expr + "\nvar after = 1\nenddef\n",
				"vim9script\nvar value = " + test.expr + "\nvar after = 1\n",
			} {
				file := Parse(source)
				if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || file.Text(file.Diagnostics[0].Span) != ":" && file.Text(file.Diagnostics[0].Span) != "," {
					t.Fatalf("diagnostics = %#v", file.Diagnostics)
				}
				if len(file.Commands) < 3 || file.Commands[1].Declaration == nil || file.Commands[1].Declaration.Initializer == nil || file.Commands[1].Declaration.Initializer.Kind != ExpressionDictionary || len(file.Commands[1].Declaration.Initializer.Children) != test.children {
					t.Fatalf("commands = %#v", file.Commands)
				}
				if file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "after" {
					t.Fatalf("following declaration was swallowed: %#v", file.Commands)
				}
				assertFileSpans(t, file)
			}
			_, diagnostics := (LegacyExpressionParser{}).Parse(test.expr)
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == "vim/E1068" || diagnostic.Code == "vim/E1069" {
					t.Fatalf("legacy diagnostics = %#v", diagnostics)
				}
			}
		})
	}
}

func TestOfficialLegacyTupleExpression(t *testing.T) {
	// v9.2.1015 src/testdir/test_tuple.vim Test_try_finally_with_tuple_return.
	for _, source := range []string{"()", "(1, 2)"} {
		expression, diagnostics := (LegacyExpressionParser{}).Parse(source)
		if len(diagnostics) != 0 || expression.Kind != ExpressionTuple {
			t.Fatalf("%q = %#v, diagnostics = %#v", source, expression, diagnostics)
		}
	}
}

func TestVim9TupleValue(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
		items  int
	}{
		{name: "missing space after comma", source: "('a','b')", code: "vim/E1069", items: 2},
		{name: "missing space after later comma", source: "('a', 'b','c')", code: "vim/E1069", items: 3},
		{name: "space before comma", source: "('a', 'b' , 'c')", code: "vim/E1068", items: 3},
		{name: "empty item", source: "('a', , 'b',)", code: "vim/E1068", items: 2},
		{name: "empty trailing item", source: "('a', 'b', ,)", code: "vim/E1068", items: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expression, diagnostics := (Vim9ExpressionParser{}).Parse(test.source)
			if expression.Kind != ExpressionTuple || len(expression.Children) != test.items || len(diagnostics) != 1 || diagnostics[0].Code != test.code {
				t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
			}
		})
	}
}

func TestTupleValue(t *testing.T) {
	for _, dialect := range []Dialect{Legacy, Vim9} {
		t.Run(dialect.String(), func(t *testing.T) {
			missingComma, diagnostics := parseExpression("('a', 'b' 'c')", 0, dialect)
			if missingComma.Kind != ExpressionTuple || len(missingComma.Children) != 2 || len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1527" {
				t.Fatalf("missing comma = %#v, diagnostics = %#v", missingComma, diagnostics)
			}

			unclosed, diagnostics := parseExpression("('a', 'b',", 0, dialect)
			if unclosed.Kind != ExpressionTuple || len(unclosed.Children) != 2 || len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1526" {
				t.Fatalf("unclosed tuple = %#v, diagnostics = %#v", unclosed, diagnostics)
			}
			if unclosed.Span.End != len("('a', 'b'") {
				t.Fatalf("unclosed tuple span = %#v", unclosed.Span)
			}
		})
	}
}

func TestOfficialLegacyLambdaExpressionInsideVim9(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_func.vim Test_abort_even_with_silent.
	expression, diagnostics := (Vim9ExpressionParser{}).Parse("{-> ''}() .. {}['X']")
	if len(diagnostics) != 0 || expression.Kind != ExpressionBinary || expression.Children[0].Kind != ExpressionCall || expression.Children[0].Children[0].Kind != ExpressionLambda {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
}

func TestOfficialDictionaryContainingBlockLambda(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_func.vim Test_nested_lambda_with_linebreak.
	source := `range(10)
  ->mapnew((_, _) => ({
    key: range(10)->mapnew((_, _) => {
      return ' '
    }),
  }))`
	expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
	if len(diagnostics) != 0 || expression.Kind != ExpressionCall {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
}

func TestVim9TypedLambda(t *testing.T) {
	expression, diagnostics := (Vim9ExpressionParser{}).Parse(`(name: string, age: number): string => name .. age`)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if expression.Kind != ExpressionLambda || len(expression.Parameters) != 2 || expression.Parameters[0].Type == nil || expression.Parameters[0].Type.Name != "string" || expression.ReturnType == nil || expression.ReturnType.Name != "string" || expression.Children[len(expression.Children)-1].Value != ".." {
		t.Fatalf("typed lambda = %#v", expression)
	}

	variadic, diagnostics := (Vim9ExpressionParser{}).Parse(`(...values: list<number>): number => values[0]`)
	if len(diagnostics) != 0 || len(variadic.Parameters) != 1 || !variadic.Parameters[0].Variadic || variadic.Parameters[0].Type.Name != "list" {
		t.Fatalf("variadic lambda = %#v, diagnostics = %#v", variadic, diagnostics)
	}
}

func TestVim9LambdaMissingReturnType(t *testing.T) {
	source := "(): => 123"
	expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
	if expression == nil || expression.Kind != ExpressionLambda || len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1157" || diagnostics[0].Message != "missing return type" {
		t.Fatalf("lambda = %#v, diagnostics = %#v", expression, diagnostics)
	}
	if expression.Operator != (Span{Start: 4, End: 6}) || expression.ReturnType == nil || expression.ReturnType.Kind != TypeMissing || expression.ReturnTypeSpan != (Span{Start: 4, End: 4}) {
		t.Fatalf("lambda type and operator = %#v", expression)
	}
	if len(expression.Children) != 1 || expression.Children[0].Kind != ExpressionNumber || expression.Children[0].Value != "123" || expression.Children[0].Span != (Span{Start: 7, End: 10}) {
		t.Fatalf("lambda body = %#v", expression.Children)
	}

	valid, diagnostics := (Vim9ExpressionParser{}).Parse("(): number => 123")
	if valid.ReturnType == nil || valid.ReturnType.Name != "number" || len(diagnostics) != 0 {
		t.Fatalf("valid typed lambda = %#v, diagnostics = %#v", valid, diagnostics)
	}
	withoutType, diagnostics := (Vim9ExpressionParser{}).Parse("() => 123")
	if withoutType.ReturnType != nil || len(diagnostics) != 0 {
		t.Fatalf("lambda without return type = %#v, diagnostics = %#v", withoutType, diagnostics)
	}
}

func TestVim9LambdaCommandBlock(t *testing.T) {
	expression, diagnostics := (Vim9ExpressionParser{}).Parse("(value: number): number => {\n  if value > 0\n    return value\n  endif\n  return 0\n}")
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if expression.Kind != ExpressionLambda || expression.LambdaBody == nil || len(expression.LambdaBody.Commands) != 4 || len(expression.LambdaBody.Blocks) != 1 || expression.LambdaBody.Blocks[0].Kind != BlockIf || expression.Children[len(expression.Children)-1].Kind != ExpressionLambdaBlock {
		t.Fatalf("block lambda = %#v", expression)
	}
}

func TestVim9LambdaCommandBlockKeepsDeclarations(t *testing.T) {
	file := Parse("vim9script\nvar Func = (): number => {\n  var first = 1\n  var second = 2\n  return first + second\n}\nvar after = Func()\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	lambda := file.Commands[1].Declaration.Initializer
	if lambda == nil || lambda.LambdaBody == nil || len(lambda.LambdaBody.Commands) != 3 {
		t.Fatalf("lambda = %#v", lambda)
	}
	first := lambda.LambdaBody.Commands[0].Declaration
	second := lambda.LambdaBody.Commands[1].Declaration
	if first == nil || second == nil || lambda.LambdaBody.Text(first.Name) != "first" || lambda.LambdaBody.Text(second.Name) != "second" {
		t.Fatalf("lambda body commands = %#v", lambda.LambdaBody.Commands)
	}
	if after := file.Commands[2].Declaration; after == nil || file.Text(after.Name) != "after" {
		t.Fatalf("following declaration = %#v", file.Commands[2])
	}
	assertFileSpans(t, file)
}

func TestVim9LambdaCommandBlockKeepsNestedBlocks(t *testing.T) {
	file := Parse("vim9script\nvar Outer = () => {\n  var First = () => {\n    return 1\n  }\n  var Second = () => {\n    return 2\n  }\n  return First() + Second()\n}\nvar after = Outer()\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	outer := file.Commands[1].Declaration.Initializer
	if outer == nil || outer.LambdaBody == nil || len(outer.LambdaBody.Commands) != 3 {
		t.Fatalf("outer lambda = %#v", outer)
	}
	for index := 0; index < 2; index++ {
		declaration := outer.LambdaBody.Commands[index].Declaration
		if declaration == nil || declaration.Initializer == nil || declaration.Initializer.LambdaBody == nil {
			t.Fatalf("nested lambda %d = %#v", index, outer.LambdaBody.Commands[index])
		}
	}
	assertFileSpans(t, file)
}

func TestOfficialVim9InlineFunctionSameLinePayload(t *testing.T) {
	for _, source := range []string{
		"def Func()\nmap([1, 2], (k, v) => { redrawt })\nenddef\ndefcompile\nvar after = 1\n",
		"vim9script\nmap([1, 2], (k, v) => { redrawt })\nvar after = 1\n",
	} {
		file := Parse(source)
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E488" || file.Diagnostics[0].Message != "trailing characters" || !strings.HasPrefix(file.Text(file.Diagnostics[0].Span), "redrawt") {
			t.Fatalf("diagnostics = %#v", file.Diagnostics)
		}
		var call *Expression
		for _, command := range file.Commands {
			if len(command.Expressions) == 1 && command.Expressions[0].Kind == ExpressionCall {
				call = command.Expressions[0]
				break
			}
		}
		if call == nil || len(call.Children) != 3 || call.Children[1].Kind != ExpressionList {
			t.Fatalf("call = %#v", call)
		}
		lambda := call.Children[2]
		if lambda.Kind != ExpressionLambda || len(lambda.Parameters) != 2 || lambda.Parameters[0].Name.Start == lambda.Parameters[0].Name.End || lambda.Operator.Start == lambda.Operator.End || lambda.LambdaBody == nil || len(lambda.LambdaBody.Commands) != 0 || len(lambda.Children) == 0 || lambda.Children[len(lambda.Children)-1].Kind != ExpressionLambdaBlock {
			t.Fatalf("lambda = %#v", lambda)
		}
		foundAfter := false
		for _, command := range file.Commands {
			if command.Declaration != nil && file.Text(command.Declaration.Name) == "after" {
				foundAfter = true
			}
		}
		if !foundAfter {
			t.Fatalf("following command was swallowed: %#v", file.Commands)
		}
		assertFileSpans(t, file)
	}
}

func TestOfficialVim9InlineFunctionMissingBrace(t *testing.T) {
	for _, source := range []string{
		"def Func()\nvar Func = (nr: number): int => {\n  return nr\nenddef\ndefcompile\nvar after = 1\n",
		"vim9script\nvar Func = (nr: number): int => {\n  return nr\nvar after = 1\n",
	} {
		file := Parse(source)
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1171" || file.Diagnostics[0].Message != "Missing } after inline function" {
			t.Fatalf("diagnostics = %#v", file.Diagnostics)
		}
		if len(file.Commands) < 2 || file.Commands[1].Declaration == nil {
			t.Fatalf("commands = %#v", file.Commands)
		}
		lambda := file.Commands[1].Declaration.Initializer
		if lambda == nil || lambda.Kind != ExpressionLambda || len(lambda.Parameters) != 1 || file.Text(lambda.Parameters[0].Name) != "nr" || lambda.Parameters[0].Type == nil || lambda.Parameters[0].Type.Name != "number" || lambda.ReturnType == nil || lambda.ReturnType.Name != "int" || lambda.Operator.Start == lambda.Operator.End || lambda.LambdaBody == nil || len(lambda.LambdaBody.Commands) != 1 || lambda.LambdaBody.Commands[0].Canonical != "return" || len(lambda.Children) == 0 || lambda.Children[len(lambda.Children)-1].Kind != ExpressionLambdaBlock {
			t.Fatalf("recovered lambda = %#v", lambda)
		}
		foundAfter := false
		foundEnddef := false
		foundDefcompile := false
		for _, command := range file.Commands {
			foundEnddef = foundEnddef || command.Canonical == "enddef"
			foundDefcompile = foundDefcompile || command.Canonical == "defcompile"
			if command.Declaration != nil && command.Declaration.Name.End > command.Declaration.Name.Start && file.Text(command.Declaration.Name) == "after" {
				foundAfter = true
			}
		}
		if !foundAfter || strings.HasPrefix(source, "def ") && (!foundEnddef || !foundDefcompile) {
			t.Fatalf("subsequent command was swallowed: %#v", file.Commands)
		}
		assertFileSpans(t, file)
	}
}

func TestOfficialVim9InlineFunctionMissingHeredocEnd(t *testing.T) {
	for _, source := range []string{
		"def Func()\nvar Func = (nr: number): int => {\n    var ll =<< ENDIT\n       nothing\nenddef\ndefcompile\nvar after = 1\n",
		"vim9script\nvar Func = (nr: number): int => {\n    var ll =<< ENDIT\n       nothing\nvar after = 1\n",
	} {
		file := Parse(source)
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1145" || file.Diagnostics[0].Message != "missing heredoc end marker: ENDIT" || file.Text(file.Diagnostics[0].Span) != "Func" {
			t.Fatalf("diagnostics = %#v", file.Diagnostics)
		}
		if len(file.Commands) < 2 || file.Commands[1].Declaration == nil {
			t.Fatalf("commands = %#v", file.Commands)
		}
		lambda := file.Commands[1].Declaration.Initializer
		if lambda == nil || lambda.Kind != ExpressionLambda || lambda.LambdaBody == nil || len(lambda.LambdaBody.Diagnostics) != 0 {
			t.Fatalf("lambda = %#v", lambda)
		}
		var heredoc *Heredoc
		var inner *Command
		for index := range lambda.LambdaBody.Commands {
			command := &lambda.LambdaBody.Commands[index]
			if command.Heredoc != nil {
				heredoc = command.Heredoc
				inner = command
				break
			}
		}
		if inner == nil || inner.Canonical != "var" || !strings.HasPrefix(lambda.LambdaBody.Text(inner.Argument), "ll =<< ENDIT") || heredoc == nil || heredoc.Marker != "ENDIT" || !heredoc.Incomplete || heredoc.EndMarker != (Span{}) || lambda.LambdaBody.Text(heredoc.Body) != "nothing" || !strings.Contains(lambda.LambdaBody.Source, "       nothing") {
			t.Fatalf("heredoc = %#v, body = %q", heredoc, lambda.LambdaBody.Text(heredoc.Body))
		}
		foundAfter, foundEnddef, foundDefcompile := false, false, false
		for _, command := range file.Commands {
			foundEnddef = foundEnddef || command.Canonical == "enddef"
			foundDefcompile = foundDefcompile || command.Canonical == "defcompile"
			foundAfter = foundAfter || command.Declaration != nil && file.Text(command.Declaration.Name) == "after"
		}
		if !foundAfter || strings.HasPrefix(source, "def ") && (!foundEnddef || !foundDefcompile) {
			t.Fatalf("subsequent command was swallowed: %#v", file.Commands)
		}
		assertFileSpans(t, file)
	}
}

func TestVim9InlineFunctionCompleteHeredoc(t *testing.T) {
	file := Parse("vim9script\nvar Func = (nr: number): int => {\n    var ll =<< ENDIT\n       nothing\n    ENDIT\n}\nvar after = 1\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) < 2 || file.Commands[1].Declaration == nil {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	lambda := file.Commands[1].Declaration.Initializer
	if lambda == nil || lambda.LambdaBody == nil || len(lambda.LambdaBody.Commands) != 1 {
		t.Fatalf("lambda = %#v", lambda)
	}
	var heredoc *Heredoc
	var inner *Command
	for index := range lambda.LambdaBody.Commands {
		command := &lambda.LambdaBody.Commands[index]
		if command.Heredoc != nil {
			heredoc = command.Heredoc
			inner = command
			break
		}
	}
	if inner == nil || inner.Canonical != "var" || !strings.HasPrefix(lambda.LambdaBody.Text(inner.Argument), "ll =<< ENDIT") || heredoc == nil || heredoc.Incomplete || lambda.LambdaBody.Text(heredoc.Body) != "nothing" || lambda.LambdaBody.Text(heredoc.EndMarker) != "ENDIT" || !strings.Contains(lambda.LambdaBody.Source, "       nothing") {
		t.Fatalf("heredoc = %#v, body = %q, end = %q", heredoc, lambda.LambdaBody.Text(heredoc.Body), lambda.LambdaBody.Text(heredoc.EndMarker))
	}
	if file.Commands[len(file.Commands)-1].Declaration == nil || file.Text(file.Commands[len(file.Commands)-1].Declaration.Name) != "after" {
		t.Fatalf("following command was swallowed: %#v", file.Commands)
	}
	assertFileSpans(t, file)
}

func TestVim9LambdaBlockRebasesCompleteSourceAndNestedSpans(t *testing.T) {
	// Keep a non-ASCII comment before the lambda so byte offsets differ from
	// rune offsets.  The nested lambda and embedded command list exercise every
	// parser result that used to remain relative to a block substring.
	source := "# 前缀 😀\n(value: list<number>): list<number> => {\n  var inner = (item: number): number => {\n    echo item\n  }\n  windo echo value\n  return inner(value[0])\n}"
	expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
	if len(diagnostics) != 0 || expression.Kind != ExpressionLambda || expression.LambdaBody == nil {
		t.Fatalf("lambda = %#v, diagnostics = %#v", expression, diagnostics)
	}
	body := expression.LambdaBody
	if body.Source != source {
		t.Fatalf("lambda body source was not retained: got %q", body.Source)
	}
	assertFileSpansAt(t, body, "lambda body")
	if len(body.Commands) != 3 || body.Commands[0].Declaration == nil {
		t.Fatalf("lambda body commands = %#v", body.Commands)
	}
	if got := body.Text(body.Commands[0].Span); len(got) < len("var inner = ") || got[:len("var inner = ")] != "var inner = " {
		t.Fatalf("first nested command text = %q", got)
	}
	inner := body.Commands[0].Declaration.Initializer
	if inner == nil || inner.LambdaBody == nil || inner.LambdaBody.Source != source {
		t.Fatalf("inner lambda = %#v", inner)
	}
	if got := body.Text(inner.LambdaBody.Commands[0].Argument); got != "item" {
		t.Fatalf("inner lambda argument = %q", got)
	}
	if embedded := body.Commands[1].Embedded; embedded == nil || len(embedded.Commands) != 1 || body.Text(embedded.Commands[0].Argument) != "value" {
		t.Fatalf("embedded command = %#v", embedded)
	}
}

func TestVim9LambdaRejectsLineBreakBeforeArrow(t *testing.T) {
	for _, source := range []string{
		"(first,\n second) => first",
		"(first)\n => first",
		"(first): number\n => first",
	} {
		expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
		if expression.Kind != ExpressionLambda || len(diagnostics) != 1 || diagnostics[0].Code != "vim/E488" {
			t.Fatalf("%q = %#v, diagnostics = %#v", source, expression, diagnostics)
		}
	}
}

func TestVim9LambdaAllowsLineBreakAfterArrow(t *testing.T) {
	expression, diagnostics := (Vim9ExpressionParser{}).Parse("(first) =>\n first + 1")
	if expression.Kind != ExpressionLambda || len(diagnostics) != 0 {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
}

func TestVim9LambdaAllowsBackslashContinuationBeforeArrow(t *testing.T) {
	source := "(first,\n \\ second)\n \\ => first"
	expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
	if expression.Kind != ExpressionLambda || len(expression.Parameters) != 2 || expression.Parameters[1].Name.Start == expression.Parameters[1].Name.End || len(diagnostics) != 0 {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
}

func TestExpressionCollectionsCallsMethodsAndComparison(t *testing.T) {
	expression, diagnostics := (Vim9ExpressionParser{}).Parse(`items[1 : 3]->map((_, value) => value.name).result isnot null`)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if expression.Kind != ExpressionBinary || expression.Value != "isnot" || expression.Children[0].Kind != ExpressionMember {
		t.Fatalf("expression = %#v", expression)
	}

	dictionary, diagnostics := (LegacyExpressionParser{}).Parse(`{'key': [1, 2], other: {'nested': 3}}`)
	if len(diagnostics) != 0 || dictionary.Kind != ExpressionDictionary || len(dictionary.Children) != 4 {
		t.Fatalf("dictionary = %#v, diagnostics = %#v", dictionary, diagnostics)
	}
}

func TestOfficialMissingDictionaryValueContextAndRecovery(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_expr.vim Test_expr7_dict and
	// runtime/doc/eval.txt: a Dictionary item always requires a value.
	for _, test := range []struct {
		name   string
		source string
		code   string
		index  int
		count  int
	}{
		{
			name:   "script",
			source: "vim9script\nvar d = {'a':\nvar after = 1\n",
			code:   "vim/E15",
			index:  1,
			count:  3,
		},
		{
			name:   "def",
			source: "def Func()\n  var d = {'a':\n  var after = 1\nenddef\n",
			code:   "vim/E723",
			index:  1,
			count:  4,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || len(file.Commands) != test.count {
				t.Fatalf("file = %#v", file)
			}
			declaration := file.Commands[test.index].Declaration
			if declaration == nil || declaration.Initializer == nil || declaration.Initializer.Kind != ExpressionDictionary {
				t.Fatalf("dictionary declaration = %#v", file.Commands[test.index])
			}
			dictionary := declaration.Initializer
			if len(dictionary.Children) != 2 || dictionary.Children[1].Kind != ExpressionMissing || dictionary.Children[1].Span.Start != dictionary.Children[1].Span.End || dictionary.Span.End != dictionary.Children[1].Span.End {
				t.Fatalf("dictionary = %#v", dictionary)
			}
			after := file.Commands[test.index+1].Declaration
			if after == nil || file.Text(after.Name) != "after" || file.Text(after.Initializer.Span) != "1" {
				t.Fatalf("following declaration = %#v", file.Commands[test.index+1])
			}
			assertFileSpans(t, file)
		})
	}
}

func TestOfficialVim9DictionaryKeyMissingBracket(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_expr.vim Test_expr7_dict.
	source := "vim9script\nvar d = {['a']: 234, ['b': 'x'}\nvar after = 1\n"
	file := Parse(source)
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1139" || file.Diagnostics[0].Message != "Missing matching bracket after dict key" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 3 {
		t.Fatalf("commands = %#v", file.Commands)
	}
	declaration := file.Commands[1].Declaration
	if declaration == nil || declaration.Initializer == nil || declaration.Initializer.Kind != ExpressionDictionary {
		t.Fatalf("dictionary declaration = %#v", file.Commands[1])
	}
	dictionary := declaration.Initializer
	if len(dictionary.Children) != 4 {
		t.Fatalf("dictionary = %#v", dictionary)
	}
	if file.Text(dictionary.Children[0].Span) != "['a']" || file.Text(dictionary.Children[1].Span) != "234" || file.Text(dictionary.Children[2].Span) != "['b'" || file.Text(dictionary.Children[3].Span) != "'x'" {
		t.Fatalf("dictionary children = %#v", dictionary.Children)
	}
	if dictionary.Children[0].Kind != ExpressionList || len(dictionary.Children[0].Children) != 1 || dictionary.Children[2].Kind != ExpressionList || len(dictionary.Children[2].Children) != 1 {
		t.Fatalf("computed keys = %#v", dictionary.Children)
	}
	after := file.Commands[2].Declaration
	if after == nil || file.Text(after.Name) != "after" || file.Text(after.Initializer.Span) != "1" {
		t.Fatalf("following declaration = %#v", file.Commands[2])
	}
	assertFileSpans(t, file)
}

func TestOfficialVim9DictionaryExpressionKeys(t *testing.T) {
	expression, diagnostics := (Vim9ExpressionParser{}).Parse("{['a']: 234, [name]: 'x'}")
	if len(diagnostics) != 0 || expression.Kind != ExpressionDictionary || len(expression.Children) != 4 {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
	if expression.Children[0].Kind != ExpressionList || expression.Children[2].Kind != ExpressionList {
		t.Fatalf("computed keys = %#v", expression.Children)
	}

	list, diagnostics := (Vim9ExpressionParser{}).Parse("['a': 'x']")
	if list.Kind != ExpressionList || len(diagnostics) != 1 || diagnostics[0].Code != "vim/E696" {
		t.Fatalf("list = %#v, diagnostics = %#v", list, diagnostics)
	}
}

func TestOfficialVim9SliceColonSpacing(t *testing.T) {
	// v9.2.1015 runtime/doc/vim9.txt and
	// src/testdir/test_vim9_assign.vim Test_unlet_list_slice.
	for _, source := range []string{"values[0 : 1]", "values[:]", "values[0 :]", "values[: 1]"} {
		expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
		if len(diagnostics) != 0 || expression.Kind != ExpressionSlice {
			t.Fatalf("%q = %#v, diagnostics = %#v", source, expression, diagnostics)
		}
	}
	for _, source := range []string{"values[0:1]", "values[0 :1]", "values[0: 1]", "values[:1]", "values[0:]"} {
		_, diagnostics := (Vim9ExpressionParser{}).Parse(source)
		if len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1004" {
			t.Fatalf("%q diagnostics = %#v", source, diagnostics)
		}
	}
	legacy, diagnostics := (LegacyExpressionParser{}).Parse("values[0:1]")
	if len(diagnostics) != 0 || legacy.Kind != ExpressionSlice {
		t.Fatalf("legacy = %#v, diagnostics = %#v", legacy, diagnostics)
	}
}

func TestOfficialMultilineNumericDictionaryMember(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_expr.vim Test_expr_member.
	for _, source := range []string{"value\n  .one", "value\n  .1", "value\n  ._"} {
		expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
		if len(diagnostics) != 0 || expression.Kind != ExpressionMember {
			t.Fatalf("%q = %#v, diagnostics = %#v", source, expression, diagnostics)
		}
	}
}

func TestVim9ExpressionSourceRules(t *testing.T) {
	cast, diagnostics := (Vim9ExpressionParser{}).Parse(`<number>value + 1`)
	if len(diagnostics) != 0 || cast.Kind != ExpressionBinary || cast.Children[0].Kind != ExpressionCast || cast.Children[0].Value != "number" {
		t.Fatalf("cast = %#v, diagnostics = %#v", cast, diagnostics)
	}
	nestedCast, diagnostics := (Vim9ExpressionParser{}).Parse(`<tuple<number, ...list<string>>>value`)
	if len(diagnostics) != 0 || nestedCast.Kind != ExpressionCast || nestedCast.CastType == nil || nestedCast.CastType.Name != "tuple" || len(nestedCast.CastType.Arguments) != 2 || nestedCast.CastType.Arguments[1].Kind != TypeVariadic {
		t.Fatalf("nested cast = %#v, diagnostics = %#v", nestedCast, diagnostics)
	}
	_, diagnostics = (Vim9ExpressionParser{}).Parse(`left+right`)
	if len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1004" {
		t.Fatalf("spacing diagnostics = %#v", diagnostics)
	}
	_, diagnostics = (Vim9ExpressionParser{}).Parse(`left isnot# right`)
	if len(diagnostics) != 1 || diagnostics[0].Code != "vim/E15" {
		t.Fatalf("isnot# diagnostics = %#v", diagnostics)
	}
}

func TestVim9IncompleteTypeCast(t *testing.T) {
	expression, diagnostics := (Vim9ExpressionParser{}).Parse(`<number 123`)
	if len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1104" || diagnostics[0].Span != (Span{Start: 7, End: 7}) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if expression.Kind != ExpressionCast || expression.CastType == nil || expression.CastType.Kind != TypeNamed || expression.CastType.Name != "number" || expression.Operator != (Span{Start: 0, End: 7}) || len(expression.Children) != 0 {
		t.Fatalf("expression = %#v", expression)
	}

	file := Parse("vim9script\nvar x = <number 123\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1104" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 3 || file.Commands[1].Declaration == nil || file.Commands[1].Declaration.Initializer == nil || file.Commands[1].Declaration.Initializer.Kind != ExpressionCast || file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "after" {
		t.Fatalf("commands = %#v", file.Commands)
	}
	assertFileSpans(t, file)

	_, diagnostics = (Vim9ExpressionParser{}).Parse(`<number >123`)
	if len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1068" {
		t.Fatalf("space before > diagnostics = %#v", diagnostics)
	}
	legacy, diagnostics := (LegacyExpressionParser{}).Parse(`<number 123`)
	if legacy.Kind == ExpressionCast || len(diagnostics) == 0 {
		t.Fatalf("legacy = %#v, diagnostics = %#v", legacy, diagnostics)
	}
}

func TestOfficialIncompleteParenthesizedExpression(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{
		{
			name:   "def",
			source: "def Func()\nvar x = (12\nvar after = 1\nenddef\n",
			code:   "vim/E1097",
		},
		{
			name:   "vim9script",
			source: "vim9script\nvar x = (12\nvar after = 1\n",
			code:   "vim/E110",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || file.Diagnostics[0].Span.Start != file.Diagnostics[0].Span.End {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			var declaration *Declaration
			for index := range file.Commands {
				if candidate := file.Commands[index].Declaration; candidate != nil && file.Text(candidate.Name) == "x" {
					declaration = candidate
					break
				}
			}
			if declaration == nil || declaration.Initializer == nil || declaration.Initializer.Kind != ExpressionParenthesized || len(declaration.Initializer.Children) != 1 || declaration.Initializer.Children[0].Kind != ExpressionNumber || declaration.Initializer.Children[0].Value != "12" {
				t.Fatalf("declaration = %#v, commands = %#v", declaration, file.Commands)
			}
			if file.Text(declaration.Initializer.Span) != "(12" {
				t.Fatalf("initializer span = %q (%#v)", file.Text(declaration.Initializer.Span), declaration.Initializer)
			}
			var after *Declaration
			for index := range file.Commands {
				if candidate := file.Commands[index].Declaration; candidate != nil && file.Text(candidate.Name) == "after" {
					after = candidate
					break
				}
			}
			if after == nil || after.Initializer == nil || after.Initializer.Kind != ExpressionNumber || after.Initializer.Value != "1" {
				t.Fatalf("after declaration = %#v, commands = %#v", after, file.Commands)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestVim9MissingOperandRecovery(t *testing.T) {
	tests := []struct {
		name       string
		statement  string
		kind       ExpressionKind
		childCount int
	}{
		{name: "ternary", statement: "var value = false ? ", kind: ExpressionTernary, childCount: 2},
		{name: "index", statement: "var value = g:list_mixed[", kind: ExpressionIndex, childCount: 2},
		{name: "slice", statement: "var value = 'asdf'[1 :", kind: ExpressionSlice, childCount: 3},
		{name: "parenthesized", statement: "echo (", kind: ExpressionParenthesized, childCount: 1},
	}
	for _, test := range tests {
		for _, context := range []struct {
			name   string
			source string
			code   string
		}{
			{name: "def", source: "vim9script\ndef Broken()\n  " + test.statement + "\n  var after = 1\nenddef\n", code: "vim/E1097"},
			{name: "script", source: "vim9script\n" + test.statement + "\nvar after = 1\n", code: "vim/E15"},
		} {
			t.Run(test.name+"/"+context.name, func(t *testing.T) {
				file := Parse(context.source)
				if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != context.code {
					t.Fatalf("diagnostics = %#v, want one %s", file.Diagnostics, context.code)
				}
				var expression *Expression
				foundAfter := false
				for index := range file.Commands {
					command := &file.Commands[index]
					if command.Declaration != nil {
						name := file.Text(command.Declaration.Name)
						if name == "value" {
							expression = command.Declaration.Initializer
						}
						foundAfter = foundAfter || name == "after"
					}
					if command.Canonical == "echo" && len(command.Expressions) == 1 {
						expression = command.Expressions[0]
					}
				}
				if expression == nil || expression.Kind != test.kind || len(expression.Children) != test.childCount || expression.Children[len(expression.Children)-1].Kind != ExpressionMissing {
					t.Fatalf("expression = %#v", expression)
				}
				if !foundAfter {
					t.Fatalf("parser did not recover to after declaration: %#v", file.Commands)
				}
				assertFileSpans(t, file)
			})
		}
	}
}

func TestOfficialListDictRecovery(t *testing.T) {
	memberTests := []struct {
		source string
		code   string
	}{
		{source: "func Func()\nlet d = {\"k\": 10}\necho d.\nendfunc\ncall Func()\n", code: "vim/E15"},
		{source: "def Func()\n# comment\nvar d = {\"k\": 10}\necho d.\n#comment\nenddef\n", code: "vim/E1127"},
		{source: "vim9script\nvar d = {\"k\": 10}\necho d.\n", code: "vim/E15"},
	}
	for _, test := range memberTests {
		file := Parse(test.source)
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || file.Diagnostics[0].Span.Start != file.Diagnostics[0].Span.End {
			t.Fatalf("%s diagnostics = %#v", test.code, file.Diagnostics)
		}
		var member *Expression
		for index := range file.Commands {
			if file.Commands[index].Canonical == "echo" && len(file.Commands[index].Expressions) == 1 {
				member = file.Commands[index].Expressions[0]
			}
		}
		if member == nil || member.Kind != ExpressionMember || file.Text(member.Operator) != "." || len(member.Children) != 2 || member.Children[1].Kind != ExpressionMissing {
			t.Fatalf("%s member = %#v, commands = %#v", test.code, member, file.Commands)
		}
		assertFileSpans(t, file)
	}

	sliceTests := []struct {
		source string
		code   string
	}{
		{source: "func Func()\nlet v = range(5)[2 : 3\nendfunc\ncall Func()\n", code: "vim/E111"},
		{source: "def Func()\n# comment\nvar v = range(5)[2 : 3\n#comment\nenddef\n", code: "vim/E1097"},
		{source: "vim9script\nvar v = range(5)[2 : 3\n", code: "vim/E111"},
	}
	for _, test := range sliceTests {
		file := Parse(test.source)
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || file.Diagnostics[0].Span.Start != file.Diagnostics[0].Span.End {
			t.Fatalf("%s diagnostics = %#v", test.code, file.Diagnostics)
		}
		var slice *Expression
		for index := range file.Commands {
			if declaration := file.Commands[index].Declaration; declaration != nil && declaration.Initializer != nil {
				slice = declaration.Initializer
			}
		}
		if slice == nil || slice.Kind != ExpressionSlice || len(slice.Children) != 3 || slice.Children[1].Value != "2" || slice.Children[2].Value != "3" {
			t.Fatalf("%s slice = %#v, commands = %#v", test.code, slice, file.Commands)
		}
		assertFileSpans(t, file)
	}
}

func TestOfficialVim9MemberDotRecovery(t *testing.T) {
	// Vim v9.2.1015 src/testdir/test_vim9_expr.vim:2617 and 4475.
	list := Parse("vim9script\ndef Main()\n  var values = ['x'.\nenddef\nvar after = 1\n")
	if len(list.Diagnostics) != 1 || list.Diagnostics[0].Code != "vim/E1127" {
		t.Fatalf("list diagnostics = %#v", list.Diagnostics)
	}
	values := list.Commands[2].Declaration
	if values == nil || values.Initializer == nil || values.Initializer.Kind != ExpressionList || len(values.Initializer.Children) != 1 {
		t.Fatalf("values = %#v", values)
	}
	member := values.Initializer.Children[0]
	if member.Kind != ExpressionMember || len(member.Children) != 2 || member.Children[1].Kind != ExpressionMissing || list.Text(member.Operator) != "." {
		t.Fatalf("incomplete member = %#v", member)
	}
	if after := list.Commands[4].Declaration; after == nil || list.Text(after.Name) != "after" || list.Text(after.Initializer.Span) != "1" {
		t.Fatalf("following declaration = %#v", list.Commands[4])
	}
	assertFileSpans(t, list)

	tests := []struct {
		name   string
		source string
		code   string
	}{
		{
			name:   "def",
			source: "vim9script\ndef Main()\n  assert_equal(33, d.\n        one)\nenddef\nvar after = 1\n",
			code:   "vim/E1127",
		},
		{
			name:   "vim9-script",
			source: "vim9script\nassert_equal(33, d.\n      one)\nvar after = 1\n",
			code:   "vim/E116",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			var call *Expression
			var after *Declaration
			for index := range file.Commands {
				command := &file.Commands[index]
				if len(command.Expressions) == 1 && command.Expressions[0].Kind == ExpressionCall {
					call = command.Expressions[0]
				}
				if command.Declaration != nil && file.Text(command.Declaration.Name) == "after" {
					after = command.Declaration
				}
			}
			if call == nil || len(call.Children) != 3 || call.Children[2].Kind != ExpressionMember || call.Children[2].Value != "one" || !strings.Contains(file.Text(call.Children[2].Span), "d.\n") {
				t.Fatalf("call = %#v", call)
			}
			if after == nil || file.Text(after.Initializer.Span) != "1" {
				t.Fatalf("following declaration = %#v", after)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestOfficialVim9SuperMissingMember(t *testing.T) {
	file := Parse("vim9script\nclass A\nendclass\nclass B extends A\n  def Fn()\n    var x = super.()\n  enddef\nendclass\ndefcompile\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1127" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	var member *Expression
	for _, command := range file.Commands {
		if command.Declaration != nil && command.Declaration.Initializer != nil {
			member = command.Declaration.Initializer
		}
	}
	if member == nil || member.Kind != ExpressionMember || file.Text(member.Operator) != "." || len(member.Children) != 2 || member.Children[1].Kind != ExpressionMissing || file.Text(member.Span) != "super.()" {
		t.Fatalf("member = %#v", member)
	}
	if len(file.Commands) < 7 || file.Commands[len(file.Commands)-1].Canonical != "defcompile" {
		t.Fatalf("terminators not retained: %#v", file.Commands)
	}
	assertFileSpans(t, file)
}

func TestVim9LogicalAndOptionExpressions(t *testing.T) {
	logical, diagnostics := (Vim9ExpressionParser{}).Parse("left == 1 && right == 2")
	if len(diagnostics) != 0 || logical.Kind != ExpressionBinary || logical.Value != "&&" {
		t.Fatalf("logical = %#v, diagnostics = %#v", logical, diagnostics)
	}
	option, diagnostics := (Vim9ExpressionParser{}).Parse("&ignorecase && &g:magic")
	if len(diagnostics) != 0 || option.Kind != ExpressionBinary || option.Children[0].Value != "&ignorecase" || option.Children[1].Value != "&g:magic" {
		t.Fatalf("option = %#v, diagnostics = %#v", option, diagnostics)
	}
}

func TestOfficialRegisterExpressions(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_assign.vim
	// Test_assignment_vim9script.
	for _, source := range []string{"@/", "@0", "@9", "@-", "@_", "@#", "@.", "@%", "@:", "@=", "@*", "@+", "@\""} {
		expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
		if len(diagnostics) != 0 || expression.Kind != ExpressionIdentifier || expression.Value != source {
			t.Fatalf("%q = %#v, diagnostics = %#v", source, expression, diagnostics)
		}
	}
}

func TestVim9InvalidRegisterAtoms(t *testing.T) {
	tests := []struct {
		source string
		code   string
		span   string
	}{
		{source: "@", code: "vim/E1002", span: "@"},
		{source: "@<", code: "vim/E354", span: "<"},
	}
	for _, test := range tests {
		expression, diagnostics := (Vim9ExpressionParser{}).Parse(test.source)
		if len(diagnostics) != 1 || diagnostics[0].Code != test.code || test.source[diagnostics[0].Span.Start:diagnostics[0].Span.End] != test.span {
			t.Fatalf("%q expression = %#v, diagnostics = %#v", test.source, expression, diagnostics)
		}
		if expression.Kind != ExpressionIdentifier || expression.Value != test.source || expression.Span != (Span{Start: 0, End: len(test.source)}) {
			t.Fatalf("%q expression = %#v", test.source, expression)
		}
	}

	file := Parse("vim9script\ndef Test()\n  var broken = @<\n  var after = 1\nenddef\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E354" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 5 || file.Commands[2].Declaration == nil || file.Commands[2].Declaration.Initializer == nil || file.Commands[2].Declaration.Initializer.Value != "@<" || file.Commands[3].Declaration == nil || file.Text(file.Commands[3].Declaration.Name) != "after" {
		t.Fatalf("commands = %#v", file.Commands)
	}
	for _, source := range []string{"@", "@<"} {
		expression, diagnostics := (LegacyExpressionParser{}).Parse(source)
		if len(diagnostics) != 0 || expression.Kind != ExpressionIdentifier || expression.Value != source {
			t.Fatalf("legacy %q expression = %#v, diagnostics = %#v", source, expression, diagnostics)
		}
	}
	assertFileSpans(t, file)
}

func TestOfficialUnnamedRegisterBesideDoubleQuotedString(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_assign.vim Test_assignment_vim9script.
	source := `['foo', @"]->setline("]=<<"->count('='))`
	expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
	if len(diagnostics) != 0 || expression.Kind != ExpressionCall {
		t.Fatalf("tokens = %#v, expression = %#v, diagnostics = %#v", lexExpression(source, 0, Vim9), expression, diagnostics)
	}
	expression, diagnostics = parseExpression(source, 100, Vim9)
	if len(diagnostics) != 0 || expression.Kind != ExpressionCall {
		t.Fatalf("based tokens = %#v, expression = %#v, diagnostics = %#v", lexExpression(source, 100, Vim9), expression, diagnostics)
	}
}

func TestOfficialArrowCallableLambda(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_expr.vim
	// Test_expr9_lambda_vim9script.
	source := "10->((a) =>\n  a\n  + 2\n)()"
	expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
	if len(diagnostics) != 0 || expression.Kind != ExpressionCall || expression.Value != "->" || len(expression.Children) != 2 || expression.Children[0].Kind != ExpressionParenthesized || expression.Children[1].Value != "10" {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
}

func TestVim9LambdaTailDiagnostics(t *testing.T) {
	t.Run("trailing parenthesis", func(t *testing.T) {
		source := "() => 123)"
		expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
		if len(diagnostics) != 1 || diagnostics[0].Code != "vim/E488" || diagnostics[0].Message != "trailing characters" || source[diagnostics[0].Span.Start:diagnostics[0].Span.End] != ")" {
			t.Fatalf("diagnostics = %#v", diagnostics)
		}
		if expression.Kind != ExpressionLambda || len(expression.Children) != 1 || expression.Children[0].Value != "123" {
			t.Fatalf("lambda = %#v", expression)
		}

		file := Parse("vim9script\nvar X = () => 123)\nvar after = 1\n")
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E488" || file.Text(file.Diagnostics[0].Span) != ")" || len(file.Commands) != 3 || file.Commands[1].Declaration == nil || file.Commands[1].Declaration.Initializer == nil || file.Commands[1].Declaration.Initializer.Kind != ExpressionLambda || file.Commands[2].Declaration == nil {
			t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
		}
		assertFileSpans(t, file)
	})

	t.Run("missing lambda call", func(t *testing.T) {
		source := "123->((x) => x + 5)"
		expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
		if len(diagnostics) != 1 || diagnostics[0].Code != "vim/E107" || diagnostics[0].Message != "Missing parentheses: lambda" || diagnostics[0].Span != (Span{Start: len(source), End: len(source)}) {
			t.Fatalf("diagnostics = %#v", diagnostics)
		}
		if expression.Kind != ExpressionCall || expression.Value != "->" || len(expression.Children) != 2 || expression.Children[0].Kind != ExpressionParenthesized || len(expression.Children[0].Children) != 1 || expression.Children[0].Children[0].Kind != ExpressionLambda || expression.Children[1].Value != "123" {
			t.Fatalf("arrow lambda = %#v", expression)
		}

		file := Parse("vim9script\nvar x = 123->((x) => x + 5)\nvar after = 1\n")
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E107" || file.Diagnostics[0].Span.Start != file.Diagnostics[0].Span.End || len(file.Commands) != 3 || file.Commands[1].Declaration == nil || file.Commands[1].Declaration.Initializer == nil || file.Commands[1].Declaration.Initializer.Kind != ExpressionCall || file.Commands[2].Declaration == nil {
			t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
		}
		assertFileSpans(t, file)
	})

	valid, diagnostics := (Vim9ExpressionParser{}).Parse("123->((x) => x + 5)()")
	if len(diagnostics) != 0 || valid.Kind != ExpressionCall {
		t.Fatalf("valid arrow lambda = %#v, diagnostics = %#v", valid, diagnostics)
	}
	named, diagnostics := (Vim9ExpressionParser{}).Parse("123->(Func)")
	if len(diagnostics) != 1 || diagnostics[0].Code != "vimls/missing-method-call" || named.Kind != ExpressionCall {
		t.Fatalf("named callable = %#v, diagnostics = %#v", named, diagnostics)
	}
	other, diagnostics := (Vim9ExpressionParser{}).Parse("'1'is2")
	if len(diagnostics) != 1 || diagnostics[0].Code != "vimls/trailing-expression" || other.Kind != ExpressionString {
		t.Fatalf("other trailing expression = %#v, diagnostics = %#v", other, diagnostics)
	}
}

func TestVim9MethodCallableMissingParentheses(t *testing.T) {
	for _, source := range []string{
		"def F()\nvar x = 'yes'->g:Echo\nvar after = 1\nenddef\n",
		"vim9script\nvar x = 'yes'->g:Echo\nvar after = 1\n",
		"def F()\nvar l = [2]\nl->((ll) => add(ll, 8))\nvar after = 1\nenddef\n",
		"vim9script\nvar l = [2]\nl->((ll) => add(ll, 8))\nvar after = 1\n",
	} {
		file := Parse(source)
		message := "Missing parentheses: lambda"
		if strings.Contains(source, "g:Echo") {
			message = "Missing parentheses: g:Echo"
		}
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E107" || file.Diagnostics[0].Message != message || file.Diagnostics[0].Span.Start != file.Diagnostics[0].Span.End {
			t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
		}
		var method *Expression
		for _, command := range file.Commands {
			for _, expression := range command.Expressions {
				if expression.Kind == ExpressionCall && expression.Value == "->" {
					method = expression
				}
			}
			if command.Declaration != nil && command.Declaration.Initializer != nil && command.Declaration.Initializer.Kind == ExpressionCall && command.Declaration.Initializer.Value == "->" {
				method = command.Declaration.Initializer
			}
		}
		if method == nil || len(method.Children) != 2 || method.Children[0] == nil || method.Children[1] == nil {
			t.Fatalf("method = %#v", method)
		}
		if strings.Contains(source, "((ll)") && (method.Children[0].Kind != ExpressionParenthesized || len(method.Children[0].Children) != 1 || method.Children[0].Children[0].Kind != ExpressionLambda) {
			t.Fatalf("lambda callable was not retained: %#v", method)
		}
		if strings.Contains(source, "g:Echo") && (method.Children[0].Kind != ExpressionIdentifier || method.Children[0].Value != "g:Echo") {
			t.Fatalf("named callable was not retained: %#v", method)
		}
		foundAfter := false
		for _, command := range file.Commands {
			foundAfter = foundAfter || command.Declaration != nil && file.Text(command.Declaration.Name) == "after"
		}
		if !foundAfter {
			t.Fatalf("following declaration was swallowed: %#v", file.Commands)
		}
		assertFileSpans(t, file)
	}
	for _, source := range []string{"'yes'->g:Echo()", "l->((ll) => add(ll, 8))()"} {
		if _, diagnostics := (Vim9ExpressionParser{}).Parse(source); len(diagnostics) != 0 {
			t.Fatalf("valid method %q diagnostics = %#v", source, diagnostics)
		}
	}
	if _, diagnostics := (LegacyExpressionParser{}).Parse("'yes'->g:Echo"); len(diagnostics) != 0 {
		t.Fatalf("legacy diagnostics = %#v", diagnostics)
	}
}

func TestVim9InvalidAtomRecovery(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{
		{name: "def member", source: "def F()\nvar x = g:dict_one.#$!\nvar after = 1\nenddef\n", code: "vim/E1002"},
		{name: "script member", source: "vim9script\nvar x = g:dict_one.#$!\nvar after = 1\n", code: "vim/E15"},
		{name: "def dollars", source: "def F()\nvar x = $$$\nvar after = 1\nenddef\n", code: "vim/E1002"},
		{name: "script dollars", source: "vim9script\nvar x = $$$\nvar after = 1\n", code: "vim/E15"},
		{name: "def dollar command", source: "def F()\n$\nvar after = 1\nenddef\n", code: "vim/E1002"},
		{name: "script dollar command", source: "vim9script\n$\nvar after = 1\n", code: "vim/E15"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code {
				t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
			}
			_, diagnosticLineEnd := physicalLineEnd(file.Source, file.Diagnostics[0].Span.Start)
			if diagnosticLineEnd > strings.Index(test.source, "var after") {
				t.Fatalf("diagnostic crossed recovery line: %#v", file.Diagnostics[0])
			}
			var invalid, after *Command
			for index := range file.Commands {
				command := &file.Commands[index]
				if command.Declaration != nil && file.Text(command.Declaration.Name) == "after" {
					after = command
				} else if command.Declaration != nil && file.Text(command.Declaration.Name) == "x" || command.Kind == CommandExpression && file.Text(command.Argument) == "$" {
					invalid = command
				}
			}
			if invalid == nil || after == nil || after.Declaration.Initializer == nil || file.Text(after.Declaration.Initializer.Span) != "1" {
				t.Fatalf("recovery commands = %#v", file.Commands)
			}
			if invalid.Declaration != nil && invalid.Declaration.Initializer == nil || invalid.Kind == CommandExpression && len(invalid.Expressions) != 1 {
				t.Fatalf("invalid AST was discarded: %#v", invalid)
			}
			expression := invalid.Expressions[0]
			if invalid.Declaration != nil {
				expression = invalid.Declaration.Initializer
			}
			if strings.Contains(test.name, "member") {
				if expression.Kind != ExpressionMember || len(expression.Children) != 2 || expression.Children[0].Value != "g:dict_one" || expression.Children[1].Kind != ExpressionMissing || file.Text(expression.Span) != "g:dict_one.#" {
					t.Fatalf("member AST = %#v", expression)
				}
			} else if expression.Kind != ExpressionIdentifier || strings.Trim(file.Text(expression.Span), "$") != "" {
				t.Fatalf("dollar AST = %#v", expression)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestVim9PostfixDelimiterRecovery(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
		kind   ExpressionKind
	}{
		{name: "wrong parenthesis closer", source: "def F()\necho (123]\necho after\nenddef\n", code: "vim/E110", kind: ExpressionParenthesized},
		{name: "slice before command", source: "def F()\nvar d = 'asdf'[1 : 2\necho d\nenddef\n", code: "vim/E111", kind: ExpressionSlice},
		{name: "call missing closer", source: "def F()\necho len('asdf'\necho after\nenddef\n", code: "vim/E110", kind: ExpressionCall},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code {
				t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
			}
			var malformed *Expression
			if test.kind == ExpressionSlice {
				malformed = file.Commands[1].Declaration.Initializer
			} else {
				malformed = file.Commands[1].Expressions[0]
			}
			if malformed == nil || malformed.Kind != test.kind {
				t.Fatalf("malformed expression = %#v, commands = %#v", malformed, file.Commands)
			}
			if len(file.Commands) < 4 || file.Commands[2].Canonical != "echo" {
				t.Fatalf("following command was swallowed: %#v", file.Commands)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestVim9MalformedMethodTailRecovery(t *testing.T) {
	file := Parse("def F()\nconst SetList = [function('len')]\necho 'xx'->SetList[0]x()\necho after\nenddef\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E15" || len(file.Commands[2].Expressions) != 1 || file.Commands[3].Canonical != "echo" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	method := file.Commands[2].Expressions[0]
	if method.Kind != ExpressionCall || method.Value != "->" || len(method.Children) != 2 || method.Children[0].Kind != ExpressionIndex || method.Children[1].Kind != ExpressionMissing || file.Text(method.Children[1].Span) != "x()" {
		t.Fatalf("method = %#v", method)
	}
	callable := method.Children[0].Children[0]
	if callable.Kind != ExpressionMember || callable.Value != "SetList" || len(callable.Children) != 1 || callable.Children[0].Kind != ExpressionString {
		t.Fatalf("callable = %#v, tail = %#v", callable, method.Children[1])
	}
	for _, source := range []string{"value->name()", "value->Xsquare.Square()", "value->(expr)()", "value->((x) => x)()", "value->SetList[0]()", "dict.func(expr)[idx]['func'](expr)->len()"} {
		if _, diagnostics := (Vim9ExpressionParser{}).Parse(source); len(diagnostics) != 0 {
			t.Fatalf("%q diagnostics = %#v", source, diagnostics)
		}
	}
	indexed, diagnostics := (Vim9ExpressionParser{}).Parse("value->SetList[0]()")
	if len(diagnostics) != 0 || indexed.Kind != ExpressionCall || len(indexed.Children) != 1 || indexed.Children[0].Kind != ExpressionIndex || indexed.Children[0].Children[0].Kind != ExpressionMember || indexed.Children[0].Children[0].Value != "SetList" || indexed.Children[0].Children[0].Children[0].Value != "value" {
		t.Fatalf("indexed callable = %#v, diagnostics = %#v", indexed, diagnostics)
	}
	qualified, diagnostics := (Vim9ExpressionParser{}).Parse("value->Xsquare.Square()")
	if len(diagnostics) != 0 || qualified.Kind != ExpressionCall || len(qualified.Children) != 1 || qualified.Children[0].Kind != ExpressionMember || qualified.Children[0].Value != "Square" || qualified.Children[0].Children[0].Kind != ExpressionMember || qualified.Children[0].Children[0].Value != "Xsquare" || qualified.Children[0].Children[0].Children[0].Value != "value" {
		t.Fatalf("qualified callable = %#v, diagnostics = %#v", qualified, diagnostics)
	}
	if _, diagnostics := (LegacyExpressionParser{}).Parse("'xx'->SetList[0]x()"); len(diagnostics) != 1 || diagnostics[0].Code != "vimls/trailing-expression" {
		t.Fatalf("legacy diagnostics = %#v", diagnostics)
	}
	assertFileSpans(t, file)
}

func TestOfficialVim9CallableParenthesisSpacing(t *testing.T) {
	// Vim v9.2.1015 src/testdir/test_vim9_expr.vim:4480.
	source := "vim9script\nvar l = [2]\nl->((ll) => add(ll, 8)) ()\nvar after = 1\necho after\n"
	file := Parse(source)
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E274" || file.Diagnostics[0].Message != "No white space allowed before parenthesis" || file.Text(file.Diagnostics[0].Span) != " " {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if len(file.Commands) != 5 {
		t.Fatalf("commands = %#v", file.Commands)
	}
	call := file.Commands[2].Expressions[0]
	if call.Kind != ExpressionCall || call.Value != "->" || file.Text(call.Span) != "l->((ll) => add(ll, 8))" || len(call.Children) != 2 || call.Children[0].Kind != ExpressionParenthesized || len(call.Children[0].Children) != 1 || call.Children[0].Children[0].Kind != ExpressionLambda {
		t.Fatalf("call = %#v", call)
	}
	if file.Text(file.Commands[2].Argument) != "l->((ll) => add(ll, 8)) ()" || file.Commands[3].Declaration == nil || file.Commands[4].Canonical != "echo" {
		t.Fatalf("commands = %#v", file.Commands)
	}
	assertFileSpans(t, file)

	for _, expressionSource := range []string{"callable()", "[1, 2]", "l -> ((ll) => add(ll, 8))()"} {
		_, diagnostics := (Vim9ExpressionParser{}).Parse(expressionSource)
		if len(diagnostics) != 0 {
			t.Fatalf("%q diagnostics = %#v", expressionSource, diagnostics)
		}
	}
}

func TestOfficialVim9CallMissingComma(t *testing.T) {
	// Vim v9.2.1015 src/testdir/test_vim9_expr.vim:3866.
	tests := []struct {
		name   string
		source string
		code   string
	}{
		{
			name:   "def",
			source: "def Func()\n  var Ref = function('len' [1, 2])\n  var after = 1\nenddef\n",
			code:   "vim/E1123",
		},
		{
			name:   "vim9-script",
			source: "vim9script\nvar Ref = function('len' [1, 2])\nvar after = 1\n",
			code:   "vim/E116",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || file.Text(file.Diagnostics[0].Span) != "[" {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			var ref, after *Declaration
			for index := range file.Commands {
				declaration := file.Commands[index].Declaration
				if declaration == nil {
					continue
				}
				switch file.Text(declaration.Name) {
				case "Ref":
					ref = declaration
				case "after":
					after = declaration
				}
			}
			if ref == nil || ref.Initializer == nil || ref.Initializer.Kind != ExpressionCall || len(ref.Initializer.Children) != 3 {
				t.Fatalf("Ref declaration = %#v", ref)
			}
			if file.Text(ref.Initializer.Span) != "function('len' [1, 2])" || file.Text(ref.Initializer.Children[1].Span) != "'len'" || file.Text(ref.Initializer.Children[2].Span) != "[1, 2]" {
				t.Fatalf("call = %#v", ref.Initializer)
			}
			if after == nil || file.Text(after.Initializer.Span) != "1" {
				t.Fatalf("following declaration = %#v", after)
			}
			assertFileSpans(t, file)
		})
	}

	valid := Parse("vim9script\nvar Ref = function('len', [1, 2])\n")
	if len(valid.Diagnostics) != 0 || valid.Commands[1].Declaration == nil || len(valid.Commands[1].Declaration.Initializer.Children) != 3 {
		t.Fatalf("valid call = %#v, diagnostics = %#v", valid.Commands, valid.Diagnostics)
	}
}

func TestOfficialMultilineMethodOperatorSpacing(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_expr.vim Test_expr9_method_call.
	for _, source := range []string{
		"[3, 1, 2] -> sort()",
		"[3, 1, 2]\n  -> sort()",
		"[1, 2]\n  -> ((x) => x)()",
		"[3, 4]\n  -> ((x) => x)()",
	} {
		expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
		if len(diagnostics) != 0 || expression.Kind != ExpressionCall {
			t.Fatalf("%q = %#v, diagnostics = %#v", source, expression, diagnostics)
		}
	}
}

func TestOfficialTrailingMethodArrowStopsAtPhysicalLine(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{
		{
			name:   "def",
			source: "def Func()\n# comment\n'yes'->\nEcho()\n#comment\nenddef\ndefcompile\n",
			code:   "vim/E488",
		},
		{
			name:   "vim9-script",
			source: "vim9script\n'yes'->\nEcho()\n",
			code:   "vim/E260",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code || file.Diagnostics[0].Span != (Span{Start: strings.Index(test.source, "->"), End: strings.Index(test.source, "->") + 2}) {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			if len(file.Commands) < 3 {
				t.Fatalf("commands = %#v", file.Commands)
			}
			arrow := file.Commands[1]
			if arrow.Kind != CommandExpression || len(arrow.Expressions) != 1 {
				t.Fatalf("arrow command = %#v", arrow)
			}
			expression := arrow.Expressions[0]
			if expression.Kind != ExpressionCall || expression.Value != "->" || expression.Operator != file.Diagnostics[0].Span || len(expression.Children) != 2 {
				t.Fatalf("arrow expression = %#v", expression)
			}
			missing := expression.Children[0]
			if missing.Kind != ExpressionMissing || missing.Span != (Span{Start: expression.Operator.End, End: expression.Operator.End}) || expression.Children[1].Kind != ExpressionString || file.Text(expression.Children[1].Span) != "'yes'" {
				t.Fatalf("incomplete arrow children = %#v", expression.Children)
			}
			next := file.Commands[2]
			if next.Kind != CommandExpression || len(next.Expressions) != 1 || next.Expressions[0].Kind != ExpressionCall || file.Text(next.Argument) != "Echo()" {
				t.Fatalf("recovered next command = %#v", next)
			}
			if test.name == "def" && (len(file.Commands) != 5 || file.Commands[3].Canonical != "enddef" || file.Commands[4].Canonical != "defcompile") {
				t.Fatalf("def recovery commands = %#v", file.Commands)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestOfficialQualifiedImportedMethodCall(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_expr.vim
	// Test_expr9_method_call_import.
	expression, diagnostics := (Vim9ExpressionParser{}).Parse("range(5)\n  ->Xsquare.Square()\n  ->map((_, i) => i * 10)")
	if len(diagnostics) != 0 || expression.Kind != ExpressionCall || expression.Children[0].Kind != ExpressionMember || expression.Children[0].Value != "map" {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
}

func TestOfficialMethodContinuationAcrossComments(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_expr.vim
	// Test_expr9_subscript_linebreak.
	for _, source := range []string{
		"range # trailing\n  ->mapnew('string(v:key)')",
		"range\n  # full line\n  ->mapnew('string(v:key)')",
	} {
		expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
		if len(diagnostics) != 0 || expression.Kind != ExpressionCall {
			t.Fatalf("%q = %#v, diagnostics = %#v", source, expression, diagnostics)
		}
	}
}

func TestOfficialExpressionLiteralForms(t *testing.T) {
	// v9.2.1015 runtime/doc/eval.txt and src/testdir/test_expr.vim.
	tests := []struct {
		source string
		kind   ExpressionKind
	}{
		{"0zFF00ED015DAF", ExpressionBlob},
		{"0z001122.33445566.778899.aabbcc.dd", ExpressionBlob},
		{"48'879", ExpressionNumber},
		{"0xBE'EF", ExpressionNumber},
		{".123", ExpressionNumber},
		{"1.0E-6", ExpressionNumber},
		{"{key: 1, other: 2}", ExpressionDictionary},
	}
	for _, test := range tests {
		expression, diagnostics := (Vim9ExpressionParser{}).Parse(test.source)
		if len(diagnostics) != 0 || expression.Kind != test.kind {
			t.Fatalf("%q = %#v, diagnostics = %#v, want kind %d", test.source, expression, diagnostics, test.kind)
		}
	}

	interpolated, diagnostics := (Vim9ExpressionParser{}).Parse(`$"sum: {left + right}, item: {items[0]}"`)
	if len(diagnostics) != 0 || interpolated.Kind != ExpressionInterpolatedString || len(interpolated.Children) != 2 || interpolated.Children[0].Kind != ExpressionBinary || interpolated.Children[1].Kind != ExpressionIndex {
		t.Fatalf("interpolated = %#v, diagnostics = %#v", interpolated, diagnostics)
	}
	unfinished, _ := (Vim9ExpressionParser{}).Parse("\"unfinished\\")
	if unfinished.Kind != ExpressionString {
		t.Fatalf("unfinished string = %#v", unfinished)
	}
}

func TestVim9InterpolatedLiteralStringNestedSingleQuotes(t *testing.T) {
	source := `$'legacy set{local ? 'l' : ''} {name}'`
	expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
	if len(diagnostics) != 0 || expression.Kind != ExpressionInterpolatedString || expression.Span != (Span{Start: 0, End: len(source)}) {
		t.Fatalf("interpolated literal = %#v, diagnostics = %#v", expression, diagnostics)
	}
	if len(expression.Children) != 2 || expression.Children[0].Kind != ExpressionTernary || expression.Children[1].Kind != ExpressionIdentifier {
		t.Fatalf("interpolated children = %#v", expression.Children)
	}
	firstOpen := strings.Index(source, "{local")
	firstClose := strings.Index(source[firstOpen:], "}") + firstOpen
	if expression.Children[0].Span != (Span{Start: firstOpen + 1, End: firstClose}) || expression.Children[1].Span != (Span{Start: strings.Index(source, "{name}") + 1, End: strings.Index(source, "{name}") + len("{name}") - 1}) {
		t.Fatalf("interpolation spans = %#v", expression.Children)
	}
	ternary := expression.Children[0]
	if len(ternary.Children) != 3 || ternary.Children[1].Kind != ExpressionString || ternary.Children[2].Kind != ExpressionString {
		t.Fatalf("nested single-quoted strings = %#v", ternary)
	}
	if ternary.Children[1].Span != (Span{Start: strings.Index(source, "'l'"), End: strings.Index(source, "'l'") + 3}) || ternary.Children[2].Span != (Span{Start: strings.Index(source, "''"), End: strings.Index(source, "''") + 2}) {
		t.Fatalf("nested string spans = %#v", ternary.Children)
	}

	source = `$'pytest {escape(get(result, '--tb=short'), ' \|"')}'`
	expression, diagnostics = (Vim9ExpressionParser{}).Parse(source)
	if len(diagnostics) != 0 || expression.Kind != ExpressionInterpolatedString || expression.Span != (Span{Start: 0, End: len(source)}) || len(expression.Children) != 1 {
		t.Fatalf("nested call interpolation = %#v, diagnostics = %#v", expression, diagnostics)
	}
	child := expression.Children[0]
	if child.Kind != ExpressionCall || len(child.Children) != 3 {
		t.Fatalf("escape call = %#v", child)
	}
	getCall := child.Children[1]
	if getCall.Kind != ExpressionCall || len(getCall.Children) != 3 || getCall.Children[2].Kind != ExpressionString || getCall.Children[2].Value != `'--tb=short'` {
		t.Fatalf("nested get call = %#v", getCall)
	}
	open := strings.Index(source, "{escape")
	close := strings.LastIndex(source, "}")
	if child.Span != (Span{Start: open + 1, End: close}) || child.Children[2].Span.Start != strings.Index(source, "' \\|\"'") {
		t.Fatalf("nested call spans = %#v", child)
	}
}

func TestOfficialVim9ListComments(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_script.vim Test_list_vimscript.
	expression, diagnostics := (Vim9ExpressionParser{}).Parse("[\n  'one',\n  # full line\n  'two', # trailing\n\n  'three',\n]")
	if len(diagnostics) != 0 || expression.Kind != ExpressionList || len(expression.Children) != 3 {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
}

func TestOfficialWhitespaceSeparatesEchoListExpressions(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_expr.vim Test_expr9_list.
	file := Parse("vim9script\necho [1,\n  2] [3,\n  4]\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || len(file.Commands[1].Expressions) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	for _, expression := range file.Commands[1].Expressions {
		if expression.Kind != ExpressionList || len(expression.Children) != 2 {
			t.Fatalf("expression = %#v", expression)
		}
	}
}

func TestOfficialVim9DictionaryScopeLikeKey(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_script.vim Test_list_vimscript.
	expression, diagnostics := (Vim9ExpressionParser{}).Parse("[{\n  a: 0}]->string()")
	if len(diagnostics) != 0 || expression.Kind != ExpressionCall || expression.Children[0].Kind != ExpressionMember {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
}

func TestOfficialVim9DictionaryDashedKey(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_expr.vim Test_expr9_dict.
	expression, diagnostics := (Vim9ExpressionParser{}).Parse("{xx-x: 8, 8: 9}")
	if len(diagnostics) != 0 || expression.Kind != ExpressionDictionary || len(expression.Children) != 4 || expression.Children[0].Value != "xx-x" {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
}

func TestOfficialNamespaceDictionaryExpression(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_script.vim Test_try_var_decl.
	expression, diagnostics := (Vim9ExpressionParser{}).Parse("get(s:, 'value', -1)")
	if len(diagnostics) != 0 || expression.Kind != ExpressionCall || expression.Children[1].Value != "s:" {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
}

func TestOfficialLegacyCurlyName(t *testing.T) {
	// v9.2.1015 runtime/doc/eval.txt *curly-braces-names* and
	// src/testdir/test_eval_stuff.vim.
	expression, diagnostics := (LegacyExpressionParser{}).Parse(`func{part}name(arg)`)
	if len(diagnostics) != 0 || expression.Kind != ExpressionCall || expression.Children[0].Kind != ExpressionCurlyName || len(expression.Children[0].Children) != 3 {
		t.Fatalf("curly call = %#v, diagnostics = %#v", expression, diagnostics)
	}
}

func TestOfficialLegacyCurlyNameCallWithHashSuffix(t *testing.T) {
	source := `vimtex#compiler#{l:method}#init(l:options)`
	expression, diagnostics := (LegacyExpressionParser{}).Parse(source)
	if len(diagnostics) != 0 || expression.Kind != ExpressionCall || expression.Span != (Span{Start: 0, End: len(source)}) {
		t.Fatalf("curly call = %#v, diagnostics = %#v", expression, diagnostics)
	}
	callee := expression.Children[0]
	nameEnd := len(`vimtex#compiler#{l:method}#init`)
	if callee.Kind != ExpressionCurlyName || callee.Span != (Span{Start: 0, End: nameEnd}) || len(callee.Children) != 3 {
		t.Fatalf("curly callee = %#v", callee)
	}
	if callee.Children[0].Span != (Span{Start: 0, End: len(`vimtex#compiler#`)}) || callee.Children[1].Span != (Span{Start: len(`vimtex#compiler#{`), End: len(`vimtex#compiler#{l:method`)}) || callee.Children[2].Span != (Span{Start: len(`vimtex#compiler#{l:method}`), End: nameEnd}) {
		t.Fatalf("curly children = %#v", callee.Children)
	}
	if len(expression.Children) != 2 || expression.Children[1].Span != (Span{Start: nameEnd + 1, End: len(source) - 1}) {
		t.Fatalf("call children = %#v", expression.Children)
	}

	vim9, diagnostics := (Vim9ExpressionParser{}).Parse(source)
	if vim9.Kind == ExpressionCall || len(diagnostics) == 0 {
		t.Fatalf("Vim9 curly call unexpectedly accepted: expression = %#v, diagnostics = %#v", vim9, diagnostics)
	}
	spaced, diagnostics := (LegacyExpressionParser{}).Parse(`vimtex#compiler#{l:method}#init (l:options)`)
	if spaced.Kind != ExpressionCall || len(diagnostics) != 0 {
		t.Fatalf("spaced curly call = %#v, diagnostics = %#v", spaced, diagnostics)
	}
}

func TestLegacyNamedMemberCallAllowsWhitespace(t *testing.T) {
	expression, diagnostics := (LegacyExpressionParser{}).Parse(`self.Set_Project_File (argv(0))`)
	if len(diagnostics) != 0 || expression.Kind != ExpressionCall || expression.Children[0].Kind != ExpressionMember {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
}

func TestVim9GenericFunctionCalls(t *testing.T) {
	expression, diagnostics := (Vim9ExpressionParser{}).Parse(`factory.Get<list<number>, func(string): bool>(items, predicate)`)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if expression.Kind != ExpressionCall || len(expression.TypeArguments) != 2 || expression.TypeArguments[0].Name != "list" || expression.TypeArguments[1].Kind != TypeFunction || len(expression.Children) != 3 {
		t.Fatalf("generic call = %#v", expression)
	}

	comparison, diagnostics := (Vim9ExpressionParser{}).Parse(`left < number > (right)`)
	if len(diagnostics) != 0 || comparison.Kind != ExpressionBinary || comparison.Value != ">" || comparison.Children[0].Value != "<" {
		t.Fatalf("comparison = %#v, diagnostics = %#v", comparison, diagnostics)
	}
}

func TestOfficialVim9GenericFunctionReference(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_generics.vim
	// Test_get_generic_funcref_using_function.
	expression, diagnostics := (Vim9ExpressionParser{}).Parse(`function(Fn<list<number>>)`)
	if len(diagnostics) != 0 || expression.Kind != ExpressionCall || len(expression.Children) != 2 {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
	reference := expression.Children[1]
	if reference.Kind != ExpressionGenericReference || len(reference.TypeArguments) != 1 || reference.TypeArguments[0].Name != "list" {
		t.Fatalf("reference = %#v", reference)
	}
}

func TestIncompleteExpressionRecovers(t *testing.T) {
	expression, diagnostics := (Vim9ExpressionParser{}).Parse(`Func([1, 2`)
	if expression == nil || len(diagnostics) == 0 || diagnostics[0].Code != "vimls/missing-delimiter" {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
}

func TestLegacyScriptLocalFunctionNames(t *testing.T) {
	for _, test := range []struct {
		source string
		callee string
	}{
		{"<SID>Func()", "<SID>Func"},
		{"<sid>hi(1)", "<sid>hi"},
		{"<SNR>123_Func()", "<SNR>123_Func"},
		{"<snr>9_foo#bar()", "<snr>9_foo#bar"},
	} {
		expression, diagnostics := (LegacyExpressionParser{}).Parse(test.source)
		if len(diagnostics) != 0 || expression.Kind != ExpressionCall || len(expression.Children) == 0 {
			t.Fatalf("%q = %#v, diagnostics = %#v", test.source, expression, diagnostics)
		}
		callee := expression.Children[0]
		if callee.Kind != ExpressionIdentifier || callee.Value != test.callee || callee.Span != (Span{Start: 0, End: len(test.callee)}) {
			t.Fatalf("%q callee = %#v", test.source, callee)
		}
		if expression.Span != (Span{Start: 0, End: len(test.source)}) {
			t.Fatalf("%q call span = %#v", test.source, expression.Span)
		}
	}
	qualified, diagnostics := (LegacyExpressionParser{}).Parse("<SiD>name.Func()")
	if len(diagnostics) != 0 || qualified.Kind != ExpressionCall || qualified.Children[0].Kind != ExpressionMember || qualified.Children[0].Children[0].Value != "<SiD>name" {
		t.Fatalf("qualified script-local name = %#v, diagnostics = %#v", qualified, diagnostics)
	}

	for _, source := range []string{"<SID>()", "<SNR>()"} {
		_, diagnostics := (LegacyExpressionParser{}).Parse(source)
		if len(diagnostics) == 0 {
			t.Fatalf("malformed script-local name %q was accepted", source)
		}
	}
}

func TestLegacyFunctionCallAllowsWhitespace(t *testing.T) {
	for _, source := range []string{`exists ("x")`, `HtmlAttribCallback (a:x)`} {
		expression, diagnostics := (LegacyExpressionParser{}).Parse(source)
		if len(diagnostics) != 0 || expression.Kind != ExpressionCall || expression.Children[0].Kind != ExpressionIdentifier {
			t.Fatalf("%q = %#v, diagnostics = %#v", source, expression, diagnostics)
		}
	}
	_, diagnostics := (Vim9ExpressionParser{}).Parse(`exists ("x")`)
	if len(diagnostics) != 1 || diagnostics[0].Code != "vimls/trailing-expression" {
		t.Fatalf("Vim9 spaced call diagnostics = %#v", diagnostics)
	}
}

func TestVim9DynamicGlobalScopeIndex(t *testing.T) {
	expression, diagnostics := (Vim9ExpressionParser{}).Parse(`g:[name]`)
	if len(diagnostics) != 0 || expression.Kind != ExpressionIndex || len(expression.Children) != 2 || expression.Children[0].Value != "g:" {
		t.Fatalf("dynamic global index = %#v, diagnostics = %#v", expression, diagnostics)
	}
	if expression.Span != (Span{Start: 0, End: len(`g:[name]`)}) || expression.Children[0].Span != (Span{Start: 0, End: 2}) {
		t.Fatalf("dynamic global spans = %#v", expression)
	}

	file := Parse("vim9script\nvar name = 'foo'\ng:[name] = 'value'\nunlet g:[name]\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 {
		t.Fatalf("dynamic global commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if len(file.Commands[2].Expressions) != 1 || file.Commands[2].Expressions[0].Kind != ExpressionAssignment || file.Commands[2].Expressions[0].Children[0].Kind != ExpressionIndex {
		t.Fatalf("dynamic global assignment = %#v", file.Commands[2])
	}
	if len(file.Commands[3].Targets) != 1 || file.Commands[3].Targets[0].Kind != ExpressionIndex {
		t.Fatalf("dynamic global unlet = %#v", file.Commands[3])
	}
}

func FuzzLegacyExpressionNeverPanics(f *testing.F) {
	f.Add(`{'key': [1, 2]}->get('key')`)
	f.Add("((((((((")
	f.Fuzz(func(t *testing.T, source string) {
		expression, _ := (LegacyExpressionParser{}).Parse(source)
		if expression == nil {
			t.Fatal("nil expression")
		}
	})
}

func FuzzVim9ExpressionNeverPanics(f *testing.F) {
	f.Add(`(x) => x ?? 'default'`)
	f.Add("[[[[[[[[")
	f.Fuzz(func(t *testing.T, source string) {
		expression, _ := (Vim9ExpressionParser{}).Parse(source)
		if expression == nil {
			t.Fatal("nil expression")
		}
	})
}

func TestOfficialAdjacentIsOperatorTokenBoundary(t *testing.T) {
	for _, suffix := range []string{"is2", "isnot2"} {
		for _, source := range []string{
			"def Func()\nvar x = '1'" + suffix + "\nvar after = 1\nenddef\n",
			"vim9script\nvar x = '1'" + suffix + "\nvar after = 1\n",
		} {
			file := Parse(source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E488" {
				t.Fatalf("%q diagnostics = %#v", source, file.Diagnostics)
			}
			if file.Text(file.Diagnostics[0].Span) != "i" || len(file.Commands) < 3 || file.Commands[1].Declaration == nil || file.Commands[1].Declaration.Initializer == nil || file.Commands[1].Declaration.Initializer.Kind != ExpressionString || file.Text(file.Commands[1].Declaration.Initializer.Span) != "'1'" {
				t.Fatalf("%q recovery = %#v", source, file.Commands)
			}
			if file.Commands[2].Declaration == nil {
				t.Fatalf("%q did not recover next declaration", source)
			}
			assertFileSpans(t, file)
		}
	}
}

func TestOfficialVim9InvalidDotKeyBoundary(t *testing.T) {
	for _, suffix := range []string{"#b", ":b"} {
		for _, source := range []string{
			"def Func()\nvar x = { 'a" + suffix + "': 1 }\nx.a" + suffix + "\nenddef\ndefcompile\nlet after = 1\n",
			"vim9script\nvar x = { 'a" + suffix + "': 1 }\nx.a" + suffix + "\nvar after = 1\n",
		} {
			file := Parse(source)
			inDef := strings.HasPrefix(source, "def ")
			if inDef && (len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E488" || file.Text(file.Diagnostics[0].Span) != suffix[:1]) {
				t.Fatalf("%q diagnostics = %#v", source, file.Diagnostics)
			}
			if !inDef && len(file.Diagnostics) != 0 {
				t.Fatalf("%q diagnostics = %#v", source, file.Diagnostics)
			}
			member := file.Commands[2].Expressions[0]
			if member == nil || member.Kind != ExpressionMember || file.Text(member.Operator) != "." || !strings.HasPrefix(member.Value, "a") {
				t.Fatalf("%q member = %#v", source, member)
			}
			foundAfter := false
			for _, command := range file.Commands {
				if command.Declaration != nil && file.Text(command.Declaration.Name) == "after" {
					foundAfter = true
				}
			}
			if !foundAfter {
				t.Fatalf("%q did not recover next command: %#v", source, file.Commands)
			}
			assertFileSpans(t, file)
		}
	}
}
