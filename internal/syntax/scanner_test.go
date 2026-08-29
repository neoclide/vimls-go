package syntax

import "testing"

func TestParseRequiresFirstEffectiveVim9ScriptCommand(t *testing.T) {
	source := "\ufeff\n  \" legacy comment before Vim knows the dialect\nvim9s\nvar name = 'value' # Vim9 comment\n"
	file := Parse(source)
	if file.Dialect != Vim9 {
		t.Fatalf("dialect = %s", file.Dialect)
	}
	if len(file.Commands) != 2 {
		t.Fatalf("commands = %#v", file.Commands)
	}
	if file.Commands[0].Canonical != "vim9script" || file.Commands[0].Dialect != Legacy {
		t.Fatalf("trigger = %#v", file.Commands[0])
	}
	if file.Commands[1].Canonical != "var" || file.Commands[1].Dialect != Vim9 {
		t.Fatalf("Vim9 declaration = %#v", file.Commands[1])
	}
	if countTokens(file, TokenComment) != 2 {
		t.Fatalf("comment tokens = %#v", file.Tokens)
	}

	legacy := Parse("var name = 'value' # not a legacy comment\n")
	if legacy.Dialect != Legacy || len(legacy.Commands) != 1 || legacy.Commands[0].Dialect != Legacy || countTokens(legacy, TokenComment) != 0 {
		t.Fatalf("legacy dispatch = %#v", legacy)
	}

	misplaced := Parse("let g:x = 1\nvim9script\nvar x = 2\n")
	if misplaced.Dialect != Legacy || len(misplaced.Diagnostics) != 1 || misplaced.Diagnostics[0].Code != "vim/E1039" {
		t.Fatalf("misplaced vim9script = %#v", misplaced)
	}
}

func TestVim9ScriptArgumentsAndRecovery(t *testing.T) {
	for _, source := range []string{
		"vim9script\nvar value = 1\n",
		"vim9script noclear\nvar value = 1\n",
	} {
		file := Parse(source)
		if file.Dialect != Vim9 || len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
			t.Fatalf("valid vim9script source = %q, file = %#v", source, file)
		}
		if file.Commands[0].Canonical != "vim9script" || file.Commands[0].Dialect != Legacy || file.Commands[1].Dialect != Vim9 {
			t.Fatalf("valid vim9script commands = %#v", file.Commands)
		}
	}

	tests := []struct {
		argument string
		code     string
		span     string
		start    int
	}{
		{argument: "autoload", code: "vim/E475", span: "autoload", start: 11},
		{argument: "noclears", code: "vim/E475", span: "noclears", start: 11},
		{argument: "noclear noclear", code: "vim/E983", span: "noclear", start: 19},
	}
	for _, test := range tests {
		source := "vim9script " + test.argument + "\nvar after = 1\n"
		file := Parse(source)
		if file.Dialect != Vim9 || len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != test.code {
			t.Fatalf("invalid vim9script %q, file = %#v", test.argument, file)
		}
		diagnostic := file.Diagnostics[0]
		if diagnostic.Span != (Span{Start: test.start, End: test.start + len(test.span)}) || file.Text(diagnostic.Span) != test.span {
			t.Fatalf("invalid vim9script %q diagnostic span = %#v (%q), want %q", test.argument, diagnostic.Span, file.Text(diagnostic.Span), test.span)
		}
		if len(file.Commands) != 2 || file.Commands[0].Dialect != Legacy || file.Commands[1].Canonical != "var" || file.Commands[1].Dialect != Vim9 {
			t.Fatalf("invalid vim9script %q did not recover in Vim9 mode: %#v", test.argument, file.Commands)
		}
	}

	duplicate := Parse("vim9script noclear noclear\nvar after = 1\n")
	if duplicate.Text(duplicate.Commands[0].Argument) != "noclear noclear" {
		t.Fatalf("vim9script argument = %q", duplicate.Text(duplicate.Commands[0].Argument))
	}

	guarded := Parse("if !has('vim9script')\n  finish\nendif\nvim9script noclears\nvar after = 1\n")
	if guarded.Dialect != Vim9 || len(guarded.Diagnostics) != 1 || guarded.Diagnostics[0].Code != "vim/E475" || len(guarded.Commands) != 5 || guarded.Commands[4].Dialect != Vim9 {
		t.Fatalf("invalid guarded vim9script recovery = %#v", guarded)
	}
}

func TestIndependentLegacyAndVim9Parsers(t *testing.T) {
	source := "echo 'value' # dialect comment\n"
	legacy := (LegacyParser{}).Parse(source)
	vim9 := (Vim9Parser{}).Parse(source)
	if legacy.Dialect != Legacy || legacy.Commands[0].Dialect != Legacy || countTokens(legacy, TokenComment) != 0 {
		t.Fatalf("legacy parse = %#v", legacy)
	}
	if vim9.Dialect != Vim9 || vim9.Commands[0].Dialect != Vim9 || countTokens(vim9, TokenComment) != 1 {
		t.Fatalf("Vim9 parse = %#v", vim9)
	}
}

func TestMixedDialectBodiesAndCommandModifiers(t *testing.T) {
	legacy := Parse("def Build()\nvar x = 1 # Vim9\nenddef\nvim9cmd var y = 2 # Vim9\nlet g:z = 3 \" legacy\n")
	wantLegacy := []Dialect{Legacy, Vim9, Vim9, Vim9, Legacy}
	if len(legacy.Commands) != len(wantLegacy) {
		t.Fatalf("legacy commands = %#v", legacy.Commands)
	}
	for index, want := range wantLegacy {
		if got := legacy.Commands[index].Dialect; got != want {
			t.Fatalf("legacy command %d dialect = %s, want %s", index, got, want)
		}
	}

	vim9 := Parse("vim9script\nfunction Old()\nlet x = 1 \" legacy\nendfunction\nlegacy let g:y = 2 \" legacy\nvar z = 3 # Vim9\n")
	wantVim9 := []Dialect{Legacy, Vim9, Legacy, Legacy, Legacy, Vim9}
	if len(vim9.Commands) != len(wantVim9) {
		t.Fatalf("Vim9 commands = %#v", vim9.Commands)
	}
	for index, want := range wantVim9 {
		if got := vim9.Commands[index].Dialect; got != want {
			t.Fatalf("Vim9 command %d dialect = %s, want %s", index, got, want)
		}
	}
}

func TestExRangesModifiersBarsStringsAndComments(t *testing.T) {
	file := (LegacyParser{}).Parse("%silent! echo \"x\" | echomsg 'y' | \" comment\n")
	if len(file.Commands) != 2 {
		t.Fatalf("commands = %#v", file.Commands)
	}
	first := file.Commands[0]
	if file.Text(first.Range) != "%" || len(first.Modifiers) != 1 || first.Modifiers[0].Name != "silent" || first.Canonical != "echo" || file.Text(first.Argument) != "\"x\"" {
		t.Fatalf("first command = %#v, range %q, argument %q", first, file.Text(first.Range), file.Text(first.Argument))
	}
	second := file.Commands[1]
	if second.Canonical != "echomsg" || file.Text(second.Argument) != "'y'" || countTokens(file, TokenSeparator) != 2 || countTokens(file, TokenComment) != 1 {
		t.Fatalf("second command = %#v, tokens = %#v", second, file.Tokens)
	}

	vim9 := (Vim9Parser{}).Parse(":1,3 delete | echo \"a|b\" # comment\n")
	if len(vim9.Commands) != 2 || fileText(vim9, vim9.Commands[0].Range) != "1,3" || vim9.Commands[1].Canonical != "echo" || fileText(vim9, vim9.Commands[1].Argument) != "\"a|b\"" {
		t.Fatalf("Vim9 commands = %#v", vim9.Commands)
	}
}

func TestCommentAfterColon(t *testing.T) {
	legacy := (LegacyParser{}).Parse(":\" legacy comment\n")
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 1 || legacy.Commands[0].Kind != CommandEmpty || countTokens(legacy, TokenComment) != 1 {
		t.Fatalf("legacy = %#v", legacy)
	}
	vim9 := (Vim9Parser{}).Parse(": # Vim9 comment\n")
	if len(vim9.Diagnostics) != 0 || len(vim9.Commands) != 1 || vim9.Commands[0].Kind != CommandEmpty || countTokens(vim9, TokenComment) != 1 {
		t.Fatalf("Vim9 = %#v", vim9)
	}
}

func TestUnknownUserCommandsAndLegacyContinuationRecover(t *testing.T) {
	file := (LegacyParser{}).Parse("echo 'known' | MyCommand opaque\nfuturecommand data\nlet g:value = one\n  \\ .. two\n")
	if len(file.Commands) != 4 || file.Commands[1].Kind != CommandUser || file.Commands[2].Kind != CommandUnknown || file.Commands[3].Canonical != "let" {
		t.Fatalf("commands = %#v", file.Commands)
	}
	if countTokens(file, TokenContinuation) != 1 || file.Text(file.Commands[3].Argument) != "g:value = one\n  \\ .. two" {
		t.Fatalf("continuation = %#v, command = %#v, argument = %q", file.Tokens, file.Commands[3], file.Text(file.Commands[3].Argument))
	}
	for _, token := range file.Tokens {
		if token.Span.Start < 0 || token.Span.End < token.Span.Start || token.Span.End > len(file.Source) {
			t.Fatalf("invalid token span %#v", token)
		}
	}
}

func TestOfficialVim9CompatibilityGuardTriggersVim9(t *testing.T) {
	// v9.2.1015 runtime/doc/vim9.txt *vim9-mix* and
	// src/testdir/test_vim9_script.vim Test_vim9script_feature.
	source := "\" legacy comment\nif !has('vim9script')\n  finish\nendif\nvim9script\nvar value = 1\n"
	file := Parse(source)
	if file.Dialect != Vim9 || len(file.Diagnostics) != 0 || len(file.Commands) != 5 || file.Commands[4].Dialect != Vim9 {
		t.Fatalf("file = %#v", file)
	}

	patch := Parse("if !has(\"patch-9.1.1232\")\n  echoerr 'upgrade Vim'\n  finish\nendif\nvim9script\nvar pair = (1, 2)\n")
	if patch.Dialect != Vim9 || len(patch.Diagnostics) != 0 || patch.Commands[len(patch.Commands)-1].Declaration == nil {
		t.Fatalf("patch guard = %#v", patch)
	}
}

func TestOfficialVim9RangeOnlyCommand(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_script.vim Test_range_only.
	file := Parse("vim9script\n:1|\n:|\n:2\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 {
		t.Fatalf("file = %#v", file)
	}
	for index := 1; index < len(file.Commands); index++ {
		if file.Commands[index].Kind != CommandEmpty {
			t.Fatalf("command %d = %#v", index, file.Commands[index])
		}
	}
	if file.Text(file.Commands[1].Range) != "1" || file.Text(file.Commands[3].Range) != "2" || countTokens(file, TokenSeparator) != 2 {
		t.Fatalf("commands = %#v, tokens = %#v", file.Commands, file.Tokens)
	}
}

func TestLeadingBarCreatesAnEmptyCommandBeforeFollowingCommand(t *testing.T) {
	file := (LegacyParser{}).Parse("| let g:value = 1\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[0].Kind != CommandEmpty || file.Commands[1].Declaration == nil || file.Text(file.Commands[1].Declaration.Name) != "g:value" || countTokens(file, TokenSeparator) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	assertFileSpans(t, file)
}

func TestOfficialVim9BuiltinCommandArgumentIsNotAssignment(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_script.vim Test_skipped_redir.
	file := Parse("vim9script\ndef Tredir()\n  redir => l[0]\n  redir END\nenddef\n")
	if len(file.Diagnostics) != 0 || file.Commands[2].Canonical != "redir" || file.Commands[2].Kind != CommandBuiltin {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
}

func TestOfficialUnnamedRegisterAssignmentCommand(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_assign.vim Test_assignment_vim9script.
	file := Parse("vim9script\n@\" = 'bar'\n['foo', @\"]->setline(\"]=<<\"->count('='))\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || file.Commands[1].Kind != CommandExpression || file.Commands[1].Expressions[0].Kind != ExpressionAssignment {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
}

func TestOfficialModifierNamedVariableAssignment(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_assign.vim
	// Test_assign_command_modifier.
	file := Parse("vim9script\nvar verbose = 0\nsilent verbose = 2\nsilent verbose += 2\nredir => output\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	for _, index := range []int{2, 3} {
		if file.Commands[index].Kind != CommandExpression || len(file.Commands[index].Modifiers) != 1 || file.Commands[index].Modifiers[0].Name != "silent" {
			t.Fatalf("assignment %d = %#v", index, file.Commands[index])
		}
	}
	if file.Commands[4].Canonical != "redir" {
		t.Fatalf("redir = %#v", file.Commands[4])
	}
}

func TestOfficialVim9WhitespaceBeforeCallParen(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_expr.vim:4487 and
	// src/testdir/test_vim9_func.vim:781.  Vim reports E492 for the
	// top-level unknown command and E476 while compiling a def; the parser
	// keeps the command opaque in both contexts.
	bare := Parse("vim9script\nCallMe ('yes')\n")
	if len(bare.Commands) != 2 || bare.Commands[1].Kind != CommandUser || bare.Commands[1].Canonical != "CallMe" {
		t.Fatalf("bare call command = %#v", bare.Commands)
	}
	if !hasDiagnostic(bare, "vim/E492") {
		t.Fatalf("bare call diagnostics = %#v", bare.Diagnostics)
	}
	compiled := Parse("def Func()\n  CallMe ('yes')\nenddef\ndefcompile\n")
	if len(compiled.Diagnostics) != 1 || compiled.Diagnostics[0].Code != "vim/E476" || compiled.Text(compiled.Diagnostics[0].Span) != "CallMe" {
		t.Fatalf("compiled call diagnostics = %#v", compiled.Diagnostics)
	}

	call := Parse("vim9script\ncall Test ('text')\n")
	if len(call.Commands) != 2 || call.Commands[1].Kind != CommandBuiltin || call.Commands[1].Canonical != "call" {
		t.Fatalf("call command = %#v", call.Commands)
	}
	if !hasDiagnostic(call, "vim/E1068") {
		t.Fatalf("call diagnostics = %#v", call.Diagnostics)
	}

	valid := Parse("vim9script\ncall Test('text')\n")
	if len(valid.Diagnostics) != 0 || len(valid.Commands) != 2 || valid.Commands[1].Canonical != "call" {
		t.Fatalf("valid call = %#v", valid)
	}

	legacy := (LegacyParser{}).Parse("CallMe ('yes')\n")
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 1 || legacy.Commands[0].Kind != CommandUser || legacy.Commands[0].Canonical != "CallMe" {
		t.Fatalf("legacy user command = %#v", legacy)
	}
}

func TestOfficialModifierBeforeColonAndSearchRange(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_func.vim Test_silent_range_command.
	file := Parse("vim9script\ndef Crash()\n  sil! :/not found/d _\n  sil! :/not found/put _\nenddef\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	for _, index := range []int{2, 3} {
		if file.Commands[index].Range.Start == file.Commands[index].Range.End || len(file.Commands[index].Modifiers) != 1 {
			t.Fatalf("range command %d = %#v", index, file.Commands[index])
		}
	}
	if file.Commands[2].Canonical != "delete" || file.Commands[3].Canonical != "put" {
		t.Fatalf("commands = %#v", file.Commands)
	}
}

func TestOfficialImportedAliasCollidingWithCommandAbbreviation(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_func.vim
	// Test_call_funcref_when_func_is_imported_and_modified.
	file := Parse("vim9script\nimport './Xcall.vim' as imp\nimp.Run()\nimp.Setup = () => 1\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	for _, index := range []int{2, 3} {
		if file.Commands[index].Kind != CommandExpression {
			t.Fatalf("command %d = %#v", index, file.Commands[index])
		}
	}
}

func TestLegacyExpressionCommandsAllowTrailingDoubleQuoteComments(t *testing.T) {
	file := (LegacyParser{}).Parse("if !s:f() \" comment\nendif\nwhile 1 \" comment\nendwhile\nreturn \"value\"\necho \"x\"\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 6 || countTokens(file, TokenComment) != 2 {
		t.Fatalf("commands = %#v, tokens = %#v", file.Commands, file.Tokens)
	}
	wantArguments := []string{"!s:f()", "", "1", "", "\"value\"", "\"x\""}
	for index, want := range wantArguments {
		if got := file.Text(file.Commands[index].Argument); got != want {
			t.Fatalf("command %d argument = %q, want %q", index, got, want)
		}
	}

	quoted := (LegacyParser{}).Parse("if value == \"quoted\" \" comment\nendif\n")
	if len(quoted.Diagnostics) != 0 || len(quoted.Commands) != 2 || quoted.Text(quoted.Commands[0].Argument) != "value == \"quoted\"" {
		t.Fatalf("quoted expression = %#v, diagnostics = %#v", quoted.Commands, quoted.Diagnostics)
	}
}

func TestOfficialIPutEqualsArgumentIsNotAssignment(t *testing.T) {
	file := Parse("vim9script\niput =1\nput =2\n")
	if len(file.Commands) != 3 || file.Commands[1].Canonical != "iput" || file.Commands[2].Canonical != "put" {
		t.Fatalf("commands = %#v", file.Commands)
	}
}

func TestVim9CompatibilityGuardRejectsActiveAlternative(t *testing.T) {
	for _, source := range []string{
		"if exists('*has')\n  finish\nendif\nvim9script\n",
		"if !has('vim9script')\n  finish\nelse\n  echo 'active'\nendif\nvim9script\n",
	} {
		file := Parse(source)
		if file.Dialect != Legacy || !hasDiagnostic(file, "vim/E1039") {
			t.Fatalf("source = %q, file = %#v", source, file)
		}
	}
}

func TestVim9DynamicGuardWithDirectFinish(t *testing.T) {
	file := Parse("if exists('g:skip_vim9')\n  silent finish\nendif\nvim9script\nvar value = 1\n")
	if len(file.Diagnostics) != 0 || file.Dialect != Vim9 || len(file.Commands) != 5 {
		t.Fatalf("file = %#v", file)
	}
	for index, want := range []Dialect{Legacy, Legacy, Legacy, Legacy, Vim9} {
		if file.Commands[index].Dialect != want {
			t.Fatalf("command %d dialect = %v, want %v", index, file.Commands[index].Dialect, want)
		}
	}
}

func TestVim9DynamicGuardRequiresDirectUnconditionalFinishPath(t *testing.T) {
	tests := []string{
		"if g:skip_vim9\n  echo 'active'\nendif\nvim9script\n",
		"if g:skip_vim9\n  if g:nested\n    finish\n  endif\nendif\nvim9script\n",
		"if g:skip_vim9\n  finish\nelse\n  echo 'active'\nendif\nvim9script\n",
		"if g:skip_vim9\n  finish later\nendif\nvim9script\n",
		"if g:skip_vim9\n  1finish\nendif\nvim9script\n",
		"if g:skip_vim9\n  finish!\nendif\nvim9script\n",
		"if g:skip_vim9\n  for item in []\n    finish\n  endfor\nendif\nvim9script\n",
		"if g:skip_vim9 | for item in [] | finish | endfor | endif\nvim9script\n",
	}
	for _, source := range tests {
		file := Parse(source)
		if file.Dialect != Legacy || !hasDiagnostic(file, "vim/E1039") {
			t.Fatalf("source = %q, file = %#v", source, file)
		}
	}
}

func TestDeclarationsAndVim9ExpressionCommands(t *testing.T) {
	file := Parse("vim9script\nvar total: number = 1 + 2 * 3\ntotal += 4\nResult(total)\necho total 'done'\n")
	if len(file.Commands) != 5 {
		t.Fatalf("commands = %#v", file.Commands)
	}
	declaration := file.Commands[1].Declaration
	if declaration == nil || file.Text(declaration.Name) != "total" || file.Text(declaration.Type) != "number" || declaration.Initializer == nil || declaration.Initializer.Value != "+" {
		t.Fatalf("declaration = %#v", declaration)
	}
	assignment := file.Commands[2]
	if assignment.Kind != CommandExpression || len(assignment.Expressions) != 1 || assignment.Expressions[0].Value != "+=" {
		t.Fatalf("assignment = %#v", assignment)
	}
	call := file.Commands[3]
	if call.Kind != CommandExpression || len(call.Expressions) != 1 || call.Expressions[0].Kind != ExpressionCall {
		t.Fatalf("call = %#v", call)
	}
	echo := file.Commands[4]
	if len(echo.Expressions) != 2 || echo.Expressions[0].Value != "total" || echo.Expressions[1].Kind != ExpressionString {
		t.Fatalf("echo = %#v", echo)
	}
}

func TestVim9CommandsMarkedWholeCannotBeShortened(t *testing.T) {
	file := (Vim9Parser{}).Parse("if true\n  th 'failure'\nendi\n")
	if len(file.Diagnostics) != 2 || file.Diagnostics[0].Code != "vim/E1065" || file.Diagnostics[1].Code != "vim/E1065" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if file.Commands[1].Canonical != "throw" || file.Commands[2].Canonical != "endif" {
		t.Fatalf("commands = %#v", file.Commands)
	}
}

func TestOfficialRangesExplicitContinuationAndExpressionQuotes(t *testing.T) {
	legacy := (LegacyParser{}).Parse("'a;/needle/+2silent echo 'ok'\nreturn \"value\"\nthrow \"failure\"\n")
	if len(legacy.Commands) != 3 || legacy.Text(legacy.Commands[0].Range) != "'a;/needle/+2" || legacy.Commands[1].Expressions[0].Kind != ExpressionString || legacy.Commands[2].Expressions[0].Kind != ExpressionString {
		t.Fatalf("legacy commands = %#v, diagnostics = %#v", legacy.Commands, legacy.Diagnostics)
	}

	vim9 := Parse("vim9script\nclass Child\n  \\ extends pkg.Base\n  #\\ continuation comment\n  \\ implements One, pkg.Two\nendclass\n")
	if len(vim9.Diagnostics) != 0 || countTokens(vim9, TokenContinuation) != 2 {
		t.Fatalf("Vim9 continuation = %#v, diagnostics = %#v", vim9.Tokens, vim9.Diagnostics)
	}
	aggregate := vim9.Commands[1].Aggregate
	if aggregate == nil || len(aggregate.Extends) != 1 || vim9.Text(aggregate.Extends[0]) != "pkg.Base" || len(aggregate.Implements) != 2 || vim9.Text(aggregate.Implements[1]) != "pkg.Two" {
		t.Fatalf("aggregate = %#v", aggregate)
	}
}

func TestSourceUsesExFilenameGrammar(t *testing.T) {
	legacy := Parse("source foo\"comment\nsource 'part|echo done\nsource trailing\\ \n")
	if len(legacy.Commands) != 4 {
		t.Fatalf("legacy source commands = %#v", legacy.Commands)
	}
	if got := legacy.Text(legacy.Commands[0].Argument); got != "foo" {
		t.Fatalf("source before comment = %q", got)
	}
	if got := legacy.Text(legacy.Commands[1].Argument); got != "'part" {
		t.Fatalf("single quote filename segment = %q", got)
	}
	if legacy.Commands[2].Canonical != "echo" || legacy.Text(legacy.Commands[2].Argument) != "done" {
		t.Fatalf("command after filename bar = %#v", legacy.Commands[2])
	}
	if got := legacy.Text(legacy.Commands[3].Argument); got != "trailing\\ " {
		t.Fatalf("escaped trailing filename whitespace = %q", got)
	}

	vim9 := Parse("vim9script\nsource \"quoted.vim\" # comment\n")
	if len(vim9.Commands) != 2 || vim9.Text(vim9.Commands[1].Argument) != "\"quoted.vim\"" {
		t.Fatalf("Vim9 source argument = %#v", vim9.Commands)
	}
}

func TestOfficialVim9XFileBacktickExpressionsAndRecovery(t *testing.T) {
	// Vim v9.2.1015 src/testdir/test_vim9_cmd.vim Test_edit_wildcards.
	valid := Parse("vim9script\ndef Func()\n" +
		"var filename = 'Xtest'\nvar filenr = 77\n" +
		"edit `=filename`\nedit Xtest`=filenr`\n" +
		"edit `=filename``=filenr`\nedit X`=filename`xx`=filenr`yy | echo done\nenddef\n")
	if len(valid.Diagnostics) != 0 {
		t.Fatalf("valid xfile diagnostics = %#v", valid.Diagnostics)
	}
	wantExpressions := [][]string{{"filename"}, {"filenr"}, {"filename", "filenr"}, {"filename", "filenr"}}
	editIndex := 0
	for index := range valid.Commands {
		command := &valid.Commands[index]
		if command.Canonical != "edit" {
			continue
		}
		if editIndex >= len(wantExpressions) || len(command.Expressions) != len(wantExpressions[editIndex]) {
			t.Fatalf("edit %d expressions = %#v", editIndex, command.Expressions)
		}
		for expressionIndex, want := range wantExpressions[editIndex] {
			if got := valid.Text(command.Expressions[expressionIndex].Span); got != want {
				t.Fatalf("edit %d expression %d = %q, want %q", editIndex, expressionIndex, got, want)
			}
		}
		editIndex++
	}
	if editIndex != len(wantExpressions) {
		t.Fatalf("edit count = %d, want %d", editIndex, len(wantExpressions))
	}

	malformed := Parse("vim9script\ndef Func()\nedit `=\"foo\" | echo hidden\nvar after = 1\nenddef\n")
	if len(malformed.Diagnostics) != 1 || malformed.Diagnostics[0].Code != "vim/E1083" || malformed.Diagnostics[0].Span.Start != malformed.Diagnostics[0].Span.End {
		t.Fatalf("malformed xfile diagnostics = %#v", malformed.Diagnostics)
	}
	var edit *Command
	for index := range malformed.Commands {
		command := &malformed.Commands[index]
		if command.Canonical == "echo" {
			t.Fatalf("malformed xfile exposed same-line command: %#v", malformed.Commands)
		}
		if command.Canonical == "edit" {
			edit = command
		}
	}
	if edit == nil || malformed.Text(edit.Argument) != "`=\"foo\" | echo hidden" || len(edit.Expressions) != 1 || malformed.Text(edit.Expressions[0].Span) != "\"foo\"" {
		t.Fatalf("malformed edit = %#v", edit)
	}
	if len(malformed.Commands) < 2 || malformed.Commands[len(malformed.Commands)-2].Canonical != "var" || malformed.Commands[len(malformed.Commands)-1].Canonical != "enddef" {
		t.Fatalf("malformed xfile recovery commands = %#v", malformed.Commands)
	}
	assertFileSpans(t, valid)
	assertFileSpans(t, malformed)
}

func TestVariableCommandsSplitAtTopLevelBar(t *testing.T) {
	file := Parse("if exists('x') | unlet x | endif\n" +
		"unlet g:items[fn('a|b')] | lockvar 2 g:items[0] | unlockvar g:items[1]\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
	}
	want := []string{"if", "unlet", "endif", "unlet", "lockvar", "unlockvar"}
	if len(file.Commands) != len(want) {
		t.Fatalf("commands = %#v", file.Commands)
	}
	for index, name := range want {
		if file.Commands[index].Canonical != name {
			t.Fatalf("command %d = %#v, want %q", index, file.Commands[index], name)
		}
	}
	if got := file.Text(file.Commands[3].Argument); got != "g:items[fn('a|b')]" {
		t.Fatalf("nested bar split argument = %q", got)
	}
	if got := file.Text(file.Commands[4].Count); got != "2" {
		t.Fatalf("lockvar count = %q", got)
	}
}

func TestExpressionCommandsSplitAtTopLevelBar(t *testing.T) {
	// This is the compact form used by Vim runtime scripts: :return owns an
	// expression, but its command-table entry handles the following bar itself
	// instead of carrying EX_EXPR_ARG.
	file := (LegacyParser{}).Parse("if ret | return [ret, res] | endif\n" +
		"if ret | throw ret | endif\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
	}
	want := []string{"if", "return", "endif", "if", "throw", "endif"}
	if len(file.Commands) != len(want) {
		t.Fatalf("commands = %#v", file.Commands)
	}
	for index, name := range want {
		if file.Commands[index].Canonical != name {
			t.Fatalf("command %d = %#v, want %q", index, file.Commands[index], name)
		}
	}
	if len(file.Commands[1].Expressions) != 1 || file.Commands[1].Expressions[0].Kind != ExpressionList {
		t.Fatalf("return expression = %#v", file.Commands[1].Expressions)
	}
	if len(file.Commands[4].Expressions) != 1 || file.Text(file.Commands[4].Expressions[0].Span) != "ret" {
		t.Fatalf("throw expression = %#v", file.Commands[4].Expressions)
	}

	vim9 := Parse("vim9script\nif ret | final item = 1 | endif\n")
	if len(vim9.Diagnostics) != 0 || len(vim9.Commands) != 4 {
		t.Fatalf("Vim9 commands = %#v, diagnostics = %#v", vim9.Commands, vim9.Diagnostics)
	}
	for index, name := range []string{"vim9script", "if", "final", "endif"} {
		if vim9.Commands[index].Canonical != name {
			t.Fatalf("Vim9 command %d = %#v, want %q", index, vim9.Commands[index], name)
		}
	}
}

func TestSyntaxSmallHelpers(t *testing.T) {
	if Legacy.String() != "legacy" || Vim9.String() != "vim9" {
		t.Fatalf("dialects = %q, %q", Legacy.String(), Vim9.String())
	}
	file := Parse("echo 1\n")
	if file.Text(Span{Start: -1, End: 1}) != "" || file.Text(Span{Start: 2, End: 1}) != "" || file.Text(Span{Start: 0, End: len(file.Source) + 1}) != "" {
		t.Fatal("invalid spans must return empty text")
	}
}

func TestOfficialScriptVersionState(t *testing.T) {
	// v9.2.1015 src/testdir/test_vimscript.vim Test_scriptversion.
	file := (LegacyParser{}).Parse("let before = 1\nscriptversion 4\nlet after = 48'879\ndef Modern()\n  var value = .123\nenddef\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	want := []uint8{1, 1, 4, 4, 4, 4}
	if len(file.Commands) != len(want) {
		t.Fatalf("commands = %#v", file.Commands)
	}
	for index, version := range want {
		if file.Commands[index].ScriptVersion != version {
			t.Fatalf("command %d script version = %d, want %d", index, file.Commands[index].ScriptVersion, version)
		}
	}
}

func TestScriptVersionDialectRules(t *testing.T) {
	legacyBeforeVim9 := Parse("scriptversion 2\nvim9script\n")
	if len(legacyBeforeVim9.Diagnostics) != 1 || legacyBeforeVim9.Diagnostics[0].Code != "vim/E1039" {
		t.Fatalf("scriptversion before vim9script diagnostics = %#v", legacyBeforeVim9.Diagnostics)
	}
	if len(legacyBeforeVim9.Commands) != 2 || legacyBeforeVim9.Commands[0].Dialect != Legacy || legacyBeforeVim9.Commands[1].Dialect != Legacy {
		t.Fatalf("scriptversion before vim9script commands = %#v", legacyBeforeVim9.Commands)
	}

	vim9ScriptVersion := Parse("vim9script\nscriptversion 2\nvar after = 1\n")
	if len(vim9ScriptVersion.Diagnostics) != 1 || vim9ScriptVersion.Diagnostics[0].Code != "vim/E1040" {
		t.Fatalf("scriptversion in Vim9 diagnostics = %#v", vim9ScriptVersion.Diagnostics)
	}
	if len(vim9ScriptVersion.Commands) != 3 || vim9ScriptVersion.Commands[1].Dialect != Vim9 || vim9ScriptVersion.Commands[2].ScriptVersion != 1 {
		t.Fatalf("scriptversion in Vim9 commands = %#v", vim9ScriptVersion.Commands)
	}

	legacyModifier := Parse("vim9script\nlegacy scriptversion 2\nvar after = 1\n")
	if len(legacyModifier.Diagnostics) != 0 || len(legacyModifier.Commands) != 3 || legacyModifier.Commands[1].Dialect != Legacy || legacyModifier.Commands[2].ScriptVersion != 2 {
		t.Fatalf("legacy scriptversion modifier = %#v", legacyModifier)
	}

	vim9Modifier := Parse("vim9cmd scriptversion 2\nvar after = 1\n")
	if len(vim9Modifier.Diagnostics) != 2 || vim9Modifier.Diagnostics[0].Code != "vim/E1040" || vim9Modifier.Diagnostics[1].Code != "vim/E1124" {
		t.Fatalf("vim9cmd scriptversion diagnostics = %#v", vim9Modifier.Diagnostics)
	}
	if len(vim9Modifier.Commands) != 2 || vim9Modifier.Commands[0].Dialect != Vim9 || vim9Modifier.Commands[1].Dialect != Legacy || vim9Modifier.Commands[1].ScriptVersion != 1 {
		t.Fatalf("vim9cmd scriptversion commands = %#v", vim9Modifier.Commands)
	}
}

func countTokens(file *File, kind TokenKind) int {
	count := 0
	for _, token := range file.Tokens {
		if token.Kind == kind {
			count++
		}
	}
	return count
}

func hasDiagnostic(file *File, code string) bool {
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func fileText(file *File, span Span) string {
	return file.Text(span)
}
