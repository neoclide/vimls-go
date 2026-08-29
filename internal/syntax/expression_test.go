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
	for _, source := range []string{"@/", "@0", "@9", "@-", "@*", "@+", "@\""} {
		expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
		if len(diagnostics) != 0 || expression.Kind != ExpressionIdentifier || expression.Value != source {
			t.Fatalf("%q = %#v, diagnostics = %#v", source, expression, diagnostics)
		}
	}
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
		{"#{key: 1, other: 2}", ExpressionDictionary},
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
