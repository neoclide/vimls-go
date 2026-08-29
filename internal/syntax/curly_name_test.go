package syntax

import "testing"

func TestLegacyCurlyNamePrefixesAndScopedArguments(t *testing.T) {
	expression, diagnostics := (LegacyExpressionParser{}).Parse(`{prefix}_{part}_name`)
	if len(diagnostics) != 0 || expression.Kind != ExpressionCurlyName || expression.Span.Start != 0 || expression.Span.End != len(`{prefix}_{part}_name`) {
		t.Fatalf("leading and repeated curly names = %#v, diagnostics = %#v", expression, diagnostics)
	}
	file := (LegacyParser{}).Parse("let {a:vt}netrw_optionsave = 1\nunlet {a:vt}netrw_optionsave\ncall netrw#NetRead(3, a:{i})\nlet w:{k} = winvars[k]\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("curly command diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 4 || file.Commands[0].Declaration == nil || file.Commands[1].Canonical != "unlet" || file.Commands[2].Canonical != "call" || file.Commands[3].Declaration == nil {
		t.Fatalf("curly commands = %#v", file.Commands)
	}
	if file.Commands[2].Expressions[0].Kind != ExpressionCall {
		t.Fatalf("curly call expression = %#v", file.Commands[2].Expressions)
	}
	_, vim9Diagnostics := (Vim9ExpressionParser{}).Parse(`{prefix}name`)
	if len(vim9Diagnostics) == 0 {
		t.Fatal("Vim9 curly-braces name must be rejected")
	}
}

func TestLegacyComputedScopeCurlyName(t *testing.T) {
	source := `{scope[i]}:{name}`
	expression, diagnostics := (LegacyExpressionParser{}).Parse(source)
	if len(diagnostics) != 0 || expression.Kind != ExpressionCurlyName || expression.Span != (Span{Start: 0, End: len(source)}) {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
	file := (LegacyParser{}).Parse("return {scope[i]}:{name}\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || len(file.Commands[0].Expressions) != 1 || file.Commands[0].Expressions[0].Span != (Span{Start: 7, End: 24}) {
		t.Fatalf("file = %#v", file)
	}
}

func TestLegacyConcatenationWithScopedCurlyName(t *testing.T) {
	expression, diagnostics := (LegacyExpressionParser{}).Parse(`"prefix".a:{i}."suffix"`)
	if len(diagnostics) != 0 || expression == nil || expression.Kind != ExpressionBinary {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
}

func TestLegacyScopePrefixMember(t *testing.T) {
	expression, diagnostics := (LegacyExpressionParser{}).Parse(`l:.url`)
	if len(diagnostics) != 0 || expression.Kind != ExpressionMember || expression.Value != "url" || expression.Children[0].Value != "l:" {
		t.Fatalf("expression = %#v, diagnostics = %#v", expression, diagnostics)
	}
	file := (LegacyParser{}).Parse("let l:.url = call(Handler, [copy(opts)])\n")
	if len(file.Diagnostics) != 0 || file.Commands[0].Declaration == nil || file.Commands[0].Declaration.Target.Kind != ExpressionMember {
		t.Fatalf("file = %#v", file)
	}
}
