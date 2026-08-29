package syntax

import "testing"

func TestLegacyBackslashContinuationFeedsLegacyExpressionParser(t *testing.T) {
	file := (LegacyParser{}).Parse("let g:value = 1\n  \\ + 2\nlet g:after = 3\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("file = %#v", file)
	}
	declaration := file.Commands[0].Declaration
	if declaration == nil || declaration.Initializer.Kind != ExpressionBinary || declaration.Initializer.Value != "+" {
		t.Fatalf("declaration = %#v, argument = %q", declaration, file.Text(file.Commands[0].Argument))
	}
	if countTokens(file, TokenContinuation) != 1 {
		t.Fatalf("tokens = %#v", file.Tokens)
	}
}

func TestLegacyBackslashContinuationSplitsExBars(t *testing.T) {
	// runtime/syntax/2html.vim uses this compact loop form.
	source := "while expr\n" +
		"  \\ && expr |\n" +
		"  \\ let result = expr |\n" +
		"  \\ endwhile\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	want := []string{"while", "let", "endwhile"}
	if len(file.Commands) != len(want) {
		t.Fatalf("commands = %#v", file.Commands)
	}
	for index, name := range want {
		if file.Commands[index].Canonical != name {
			t.Fatalf("command %d = %#v, want %q", index, file.Commands[index], name)
		}
		if file.Commands[index].Span.Start < 0 || file.Commands[index].Span.End > len(source) || file.Text(file.Commands[index].Span) == "" {
			t.Fatalf("command %d span = %#v", index, file.Commands[index].Span)
		}
	}
	if countTokens(file, TokenContinuation) != 3 {
		t.Fatalf("continuation tokens = %#v", file.Tokens)
	}
	if len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockWhile || file.Blocks[0].End != 2 {
		t.Fatalf("blocks = %#v", file.Blocks)
	}
}

func TestLegacyBackslashContinuationLeadingBarStartsCommand(t *testing.T) {
	file := (LegacyParser{}).Parse("echo before\n  \\| echo after\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[0].Canonical != "echo" || file.Commands[1].Canonical != "echo" || file.Text(file.Commands[1].Argument) != "after" {
		t.Fatalf("commands = %#v", file.Commands)
	}
}

func TestLegacyContinuationKeepsDictionaryCommentsOutOfExpression(t *testing.T) {
	source := "let g:settings = extend(get(g:, 'settings', {}), #{\n" +
		"  \\ enabled: v:true,\n" +
		"  \"\\ comment containing { and )\n" +
		"  \\ timeout: 150,\n" +
		"  \\ })\n" +
		"let after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[0].Canonical != "let" || file.Commands[0].Declaration == nil || file.Commands[1].Canonical != "let" {
		t.Fatalf("commands = %#v", file.Commands)
	}
}

func TestLegacyContinuationKeepsMethodArrowAttached(t *testing.T) {
	source := "if synstack(plnum, pline_len)\n" +
		"  \\ ->indexof({_, id -> synIDattr(id, 'name') =~ '\\%(Comment\\|Todo\\)$'}) >= 0\n" +
		"  let min = 1\n" +
		"endif\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[0].Canonical != "if" || file.Commands[1].Canonical != "let" || file.Commands[2].Canonical != "endif" || countTokens(file, TokenContinuation) != 1 {
		t.Fatalf("commands = %#v, tokens = %#v", file.Commands, file.Tokens)
	}
}

func TestLegacyStandaloneScopeDictionaryContinuesWithMethod(t *testing.T) {
	source := "let found = b:\n" +
		"  \\ ->get('values', [])\n" +
		"  \\ ->len()\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	declaration := file.Commands[0].Declaration
	if declaration == nil || declaration.Initializer == nil || declaration.Initializer.Kind != ExpressionCall {
		t.Fatalf("declaration = %#v", declaration)
	}
}

func TestVim9AutomaticContinuation(t *testing.T) {
	source := "vim9script\nvar values = [\n  1,\n  2,\n]\nvar text = 'one'\n  .. 'two'\nvar after = 3\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 {
		t.Fatalf("diagnostics = %#v, commands = %#v", file.Diagnostics, file.Commands)
	}
	values := file.Commands[1].Declaration
	if values == nil || values.Initializer.Kind != ExpressionList || len(values.Initializer.Children) != 2 {
		t.Fatalf("values = %#v, argument = %q", values, file.Text(file.Commands[1].Argument))
	}
	text := file.Commands[2].Declaration
	if text == nil || text.Initializer.Kind != ExpressionBinary || text.Initializer.Value != ".." {
		t.Fatalf("text = %#v, argument = %q", text, file.Text(file.Commands[2].Argument))
	}
	if countTokens(file, TokenContinuation) != 4 {
		t.Fatalf("continuations = %#v", file.Tokens)
	}
}

func TestOfficialVim9AutocmdCommandListContinuation(t *testing.T) {
	// v9.2.1015 runtime/doc/vim9.txt *vim9-line-continuation*.
	file := Parse("vim9script\nautocmd BufNewFile *.match if condition\n  | echo 'match'\n  | endif\nvar after = 1\n")
	if len(file.Commands) != 3 || file.Commands[1].Canonical != "autocmd" || file.Commands[2].Declaration == nil || countTokens(file, TokenContinuation) != 2 {
		t.Fatalf("commands = %#v, tokens = %#v", file.Commands, file.Tokens)
	}
	if got := file.Text(file.Commands[1].Argument); got != "BufNewFile *.match if condition\n  | echo 'match'\n  | endif" {
		t.Fatalf("autocmd payload = %q", got)
	}
}

func TestOfficialVim9AutocmdTrailingCurlyIsOpaque(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_script.vim
	// Test_autocmd_trailing_curly_no_block_in_def.
	file := Parse("vim9script\ndef Setup()\n  au CursorHold * normal! {\n  g:afterCurly = 'reached'\nenddef\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 || file.Commands[3].Kind != CommandExpression {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if got := file.Text(file.Commands[2].Argument); got != "CursorHold * normal! {" {
		t.Fatalf("autocmd argument = %q", got)
	}
}

func TestOfficialVim9ForHeaderContinuation(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_script.vim Test_for_loop.
	file := Parse("vim9script\nfor one in\n  [1]\nendfor\nfor two\n  in [2]\nendfor\nfor three\n  in\n  [3]\nendfor\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 7 || countTokens(file, TokenContinuation) != 4 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	for _, index := range []int{1, 3, 5} {
		loop := file.Commands[index].For
		if loop == nil || loop.Iterable == nil || loop.Iterable.Kind != ExpressionList {
			t.Fatalf("for command %d = %#v", index, file.Commands[index])
		}
	}
}

func TestOfficialVim9ComparisonContinuation(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_expr.vim Test_expr4_match and
	// Test_expr4_nomatch.
	file := Parse("vim9script\nvar one = ''\n  =~ 'x'\nvar two = 'x' =~\n  '[x]'\nvar three = ''\n  !~ 'x'\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 || countTokens(file, TokenContinuation) != 3 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	for index := 1; index < 4; index++ {
		if file.Commands[index].Declaration.Initializer.Kind != ExpressionBinary {
			t.Fatalf("declaration %d = %#v", index, file.Commands[index].Declaration)
		}
	}
}

func TestOfficialVim9CaseSensitiveComparisonDoesNotContinue(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_script.vim Test_const_type.
	file := Parse("vim9script\ndef Compare(): number\n  return lhs.name <# rhs.name ? -1 : 1\nenddef\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 || countTokens(file, TokenContinuation) != 0 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
}

func TestOfficialVim9DigitSeparatorDoesNotOpenString(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_func.vim Test_lambda_line_nr.
	file := Parse("vim9script\nvar id = timer_start(1'000, (_) => 0)\nvar out = id\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || countTokens(file, TokenContinuation) != 0 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
}

func TestOfficialVim9IsOperatorContinuationUsesWordBoundary(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_class.vim Test_class_object_member_access.
	file := Parse("vim9script\ndef Identity(): any\n  return this\nenddef\nvar same = left is\n  right\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 || file.Commands[2].Canonical != "return" || file.Commands[3].Canonical != "enddef" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if countTokens(file, TokenContinuation) != 1 || file.Commands[4].Declaration.Initializer.Kind != ExpressionBinary || file.Commands[4].Declaration.Initializer.Value != "is" {
		t.Fatalf("commands = %#v, tokens = %#v", file.Commands, file.Tokens)
	}
}

func TestOfficialGenericReferenceDoesNotLookLikeShiftContinuation(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_generics.vim
	// Test_generic_funcref_use_from_def_method.
	file := Parse("vim9script\ndef Foo<T>(value: T): T\n  return value\nenddef\nvar Fn = Foo<list<string>>\nvar x = Fn([])\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 6 || file.Commands[4].Declaration == nil || file.Commands[5].Declaration == nil || countTokens(file, TokenContinuation) != 0 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
}

func TestOfficialIdentifierMethodChainWithWhitespaceContinues(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_cmd.vim Test_method_call_linebreak.
	file := (Vim9Parser{}).Parse("def Foo(): string\n  return 'text'\nenddef\ndef Bar(F: func): string\n  return F()\nenddef\ndef Test()\n  Foo  ->Bar()\n       ->setline(1)\nenddef\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 9 || file.Commands[7].Kind != CommandExpression || len(file.Commands[7].Expressions) != 1 {
		t.Fatalf("commands = %#v", file.Commands)
	}
	expression := file.Commands[7].Expressions[0]
	if expression.Kind != ExpressionCall || len(expression.Children) == 0 || expression.Children[0].Kind != ExpressionMember || expression.Children[0].Value != "setline" {
		t.Fatalf("expression = %#v", expression)
	}
	if countTokens(file, TokenContinuation) != 1 {
		t.Fatalf("tokens = %#v", file.Tokens)
	}
}

func TestOfficialPutExpressionMethodContinuation(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_cmd.vim Test_put_with_linebreak.
	file := Parse("vim9script\npu =split('abc', '\\zs')\n        ->join()\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	command := file.Commands[1]
	if command.Canonical != "put" || len(command.Expressions) != 1 || command.Expressions[0].Kind != ExpressionCall {
		t.Fatalf("put command = %#v", command)
	}
	if file.Text(command.Argument) != "=split('abc', '\\zs')\n        ->join()" || countTokens(file, TokenContinuation) != 1 {
		t.Fatalf("argument = %q, tokens = %#v", file.Text(command.Argument), file.Tokens)
	}
}

func TestVim9LeadingTernaryContinuation(t *testing.T) {
	file := (Vim9Parser{}).Parse("def Pick(ok: bool): string\n  return ok\n    ? 'yes'\n    : 'no'\nenddef\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	expression := file.Commands[1].Expressions[0]
	if expression.Kind != ExpressionTernary || file.Text(file.Commands[1].Argument) != "ok\n    ? 'yes'\n    : 'no'" {
		t.Fatalf("return = %#v, argument = %q", expression, file.Text(file.Commands[1].Argument))
	}
}

func TestVim9BuiltinNameBecomesContinuedExpression(t *testing.T) {
	file := (Vim9Parser{}).Parse("def Filter(tags: list<any>)\n  tags\n    # method comment\n    ->filter((_, value) => !empty(value))\nenddef\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	command := file.Commands[1]
	if command.Kind != CommandExpression || command.Canonical != "" || len(command.Expressions) != 1 || command.Expressions[0].Kind != ExpressionCall {
		t.Fatalf("continued expression = %#v", command)
	}
}

func TestVim9ConcatenationAssignmentContinuesWithMethod(t *testing.T) {
	file := (Vim9Parser{}).Parse("def Add(info: string, dict: dict<any>)\n  info ..= dict['cmd']\n    ->matchstr('.*')\nenddef\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	command := file.Commands[1]
	if command.Kind != CommandExpression || len(command.Expressions) != 1 || command.Expressions[0].Kind != ExpressionAssignment {
		t.Fatalf("assignment = %#v", command)
	}
	right := command.Expressions[0].Children[1]
	if right.Kind != ExpressionCall || file.Text(command.Argument) != "info ..= dict['cmd']\n    ->matchstr('.*')" {
		t.Fatalf("right = %#v, argument = %q", right, file.Text(command.Argument))
	}
}

func TestVim9OptionAssignmentContinuesWithConcatenation(t *testing.T) {
	file := (Vim9Parser{}).Parse("&l:define = 'one'\n  .. 'two'\n  .. 'three'\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	command := file.Commands[0]
	if command.Kind != CommandExpression || len(command.Expressions) != 1 || command.Expressions[0].Kind != ExpressionAssignment {
		t.Fatalf("assignment = %#v", command)
	}
	if got := file.Text(command.Argument); got != "&l:define = 'one'\n  .. 'two'\n  .. 'three'" {
		t.Fatalf("argument = %q", got)
	}
}

func TestVim9ScopeDictionaryContinuesWithMethod(t *testing.T) {
	file := (Vim9Parser{}).Parse("var value: string = g:\n  ->get('settings', {})\n  ->get('value', '')\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	declaration := file.Commands[0].Declaration
	if declaration == nil || declaration.Initializer == nil || declaration.Initializer.Kind != ExpressionCall {
		t.Fatalf("declaration = %#v", declaration)
	}
}

func TestVim9RegisterDoesNotContinueAsOperator(t *testing.T) {
	file := (Vim9Parser{}).Parse("def Read()\n  if true\n    var value = @/\n  endif\nenddef\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 || file.Commands[3].Canonical != "endif" || file.Commands[4].Canonical != "enddef" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
}

func TestVim9ColonCommandDoesNotContinueOpaqueCommand(t *testing.T) {
	file := (Vim9Parser{}).Parse("argglobal\n:%argdelete\n:$argadd file.txt\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	for index, want := range []string{"argglobal", "argdelete", "argadd"} {
		if file.Commands[index].Canonical != want {
			t.Fatalf("command %d = %#v", index, file.Commands[index])
		}
	}
}

func TestVim9NullCoalescingDoesNotOpenTernary(t *testing.T) {
	file := (Vim9Parser{}).Parse("def Width(value: number)\n  var width = value ?? 1\n  if width < 30\n    width = 30\n  endif\nenddef\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 6 || file.Commands[2].Canonical != "if" || file.Commands[4].Canonical != "endif" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
}

func TestVim9ContinuationTailKeepsWordBoundary(t *testing.T) {
	tests := []struct {
		source string
		want   bool
	}{
		{source: "value isnot#", want: true},
		{source: "value isnot", want: true},
		{source: "value is", want: true},
		{source: "valueisnot#", want: false},
		{source: "analysis", want: false},
		{source: "value +", want: true},
		{source: "value", want: false},
	}
	for _, test := range tests {
		state := scanVim9Continuation(test.source, vim9ContinuationScan{})
		if got := state.needsContinuation(); got != test.want {
			t.Errorf("%q needs continuation = %t, want %t", test.source, got, test.want)
		}
	}

	state := scanVim9Continuation("value ", vim9ContinuationScan{})
	state = scanVim9Continuation("isnot#", state)
	if !state.needsContinuation() {
		t.Fatal("operator split across physical scans lost its word boundary")
	}
}
