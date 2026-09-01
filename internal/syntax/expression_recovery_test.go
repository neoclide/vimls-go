package syntax

import "testing"

// These are intentionally incomplete expressions.  Editors request syntax
// information while text is being typed, so both dialect parsers must retain
// bounded spans and diagnostics instead of panicking or reading past input.
func TestExpressionRecoveryBoundaries(t *testing.T) {
	sources := []string{
		"(", "[", "[1,", "{", "{key:", "#{key:", "{->", "{ value ->",
		"name<", "name<number,", "name<number> (", "value->", "value -> method(",
		"value[", "value[:", "value[1 :", "value.", "value?.", "value..",
		"$'${", "$\"{", "'unterminated", "\"unterminated", "0zA", "0zABC",
		"@", "@=", "&", "g:", "s:", "{name", "{name}", "{-> 1}",
		"(a, b) =>", "(a: number, ...) =>", "a ? b :", "a ??", "a &&",
		"a\n  +", "a\n  ->", "a isnot#", "a =~# /", "a =~ /[/",
	}
	for _, dialect := range []Dialect{Legacy, Vim9} {
		parser := LegacyExpressionParser{}
		if dialect == Vim9 {
			for _, source := range sources {
				expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
				assertExpressionRecoveryBounds(t, dialect, source, expression, diagnostics)
			}
			continue
		}
		for _, source := range sources {
			expression, diagnostics := parser.Parse(source)
			assertExpressionRecoveryBounds(t, dialect, source, expression, diagnostics)
		}
	}
}

func assertExpressionRecoveryBounds(t *testing.T, dialect Dialect, source string, expression *Expression, diagnostics []Diagnostic) {
	t.Helper()
	if expression != nil && (expression.Span.Start < 0 || expression.Span.End < expression.Span.Start || expression.Span.End > len(source)) {
		t.Fatalf("%s expression %q has invalid span %#v", dialect, source, expression.Span)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Span.Start < 0 || diagnostic.Span.End < diagnostic.Span.Start || diagnostic.Span.End > len(source) {
			t.Fatalf("%s expression %q has invalid diagnostic %#v", dialect, source, diagnostic)
		}
	}
}
