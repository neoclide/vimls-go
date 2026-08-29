package syntax

import "testing"

func TestSetCommandQueriesAndScopes(t *testing.T) {
	for _, test := range []struct {
		name      string
		source    string
		canonical string
		bang      bool
	}{
		{"set-query", "set\n", "set", false},
		{"set-bang", "set!\n", "set", true},
		{"setlocal", "setlocal\n", "setlocal", false},
		{"setglobal", "setglobal\n", "setglobal", false},
		{"setlocal-abbreviation", "setl ts=8\n", "setlocal", false},
		{"setglobal-abbreviation", "setg ts=8\n", "setglobal", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := (LegacyParser{}).Parse(test.source)
			wantOptions := 0
			if test.source == "setl ts=8\n" || test.source == "setg ts=8\n" {
				wantOptions = 1
			}
			if len(file.Commands) != 1 || file.Commands[0].Canonical != test.canonical || file.Commands[0].Set == nil || len(file.Commands[0].Set.Options) != wantOptions {
				t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
			}
			if (file.Commands[0].Bang.Start < file.Commands[0].Bang.End) != test.bang {
				t.Fatalf("bang=%#v command=%#v", file.Commands[0].Bang, file.Commands[0])
			}
		})
	}
}

func TestSetCommandOptionsPreserveSpans(t *testing.T) {
	source := "set nofoo invbar novice all all& termcap ts=8 sw+=2 hls! t_k1=foo <t_k2>=bar | echo done\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	set := file.Commands[0].Set
	if set == nil || len(set.Options) != 11 {
		t.Fatalf("set=%#v", set)
	}
	checks := []struct {
		name, prefix, operator, value string
	}{
		{"foo", "no", "", ""},
		{"bar", "inv", "", ""},
		{"novice", "", "", ""},
		{"all", "", "", ""},
		{"all", "", "&", ""},
		{"termcap", "", "", ""},
		{"ts", "", "=", "8"},
		{"sw", "", "+=", "2"},
		{"hls", "", "!", ""},
	}
	for index, want := range checks {
		option := set.Options[index]
		if file.Text(option.Name) != want.name || file.Text(option.Prefix) != want.prefix || file.Text(option.Operator) != want.operator || file.Text(option.Value) != want.value {
			t.Fatalf("option %d=%#v text=(%q,%q,%q,%q), want=(%q,%q,%q,%q)", index, option, file.Text(option.Name), file.Text(option.Prefix), file.Text(option.Operator), file.Text(option.Value), want.name, want.prefix, want.operator, want.value)
		}
	}
	angle := set.Options[10]
	if file.Text(angle.Name) != "<t_k2>" || file.Text(angle.Operator) != "=" || file.Text(angle.Value) != "bar" {
		t.Fatalf("angle option=%#v", angle)
	}
	if file.Text(set.Options[5].Span) != "termcap" || file.Text(set.Options[6].Span) != "ts=8" || file.Commands[1].Canonical != "echo" {
		t.Fatalf("spans=%#v commands=%#v", set.Options, file.Commands)
	}
	assertFileSpans(t, file)
}

func TestSetCommandAllOperatorsAndText(t *testing.T) {
	source := "set foo? bar! baz& qux&vi quux&vim opt< eq=1 colon:2 add+=3 prepend^=4 remove-=5 utf=值\r\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Set == nil {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	set := file.Commands[0].Set
	wantOperators := []string{"?", "!", "&", "&vi", "&vim", "<", "=", ":", "+=", "^=", "-=", "="}
	if len(set.Options) != len(wantOperators) {
		t.Fatalf("options=%#v", set.Options)
	}
	for index, want := range wantOperators {
		if got := file.Text(set.Options[index].Operator); got != want {
			t.Fatalf("operator %d=%q want %q option=%#v", index, got, want, set.Options[index])
		}
	}
	if file.Text(set.Options[len(set.Options)-1].Value) != "值" || file.Text(file.Commands[0].Argument) != "foo? bar! baz& qux&vi quux&vim opt< eq=1 colon:2 add+=3 prepend^=4 remove-=5 utf=值" {
		t.Fatalf("utf8 argument=%q option=%#v", file.Text(file.Commands[0].Argument), set.Options[len(set.Options)-1])
	}
	assertFileSpans(t, file)
}

func TestSetCommandSpecialPrefixesAndEscapes(t *testing.T) {
	source := "set no<t_k1> novicefoo termcapfoo all? all_ foo=one\\ two\\|three\x16|four \"comment | echo hidden\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Set == nil {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	set := file.Commands[0].Set
	if len(set.Options) != 9 {
		t.Fatalf("options=%#v", set.Options)
	}
	if file.Text(set.Options[0].Prefix) != "no" || file.Text(set.Options[0].Name) != "<t_k1>" {
		t.Fatalf("angle prefix=%#v", set.Options[0])
	}
	if file.Text(set.Options[1].Name) != "novicefoo" || file.Text(set.Options[1].Prefix) != "" {
		t.Fatalf("novice prefix=%#v", set.Options[1])
	}
	if file.Text(set.Options[2].Span) != "termcap" || file.Text(set.Options[3].Span) != "foo" {
		t.Fatalf("termcap split=%q,%q", file.Text(set.Options[2].Span), file.Text(set.Options[3].Span))
	}
	if file.Text(set.Options[4].Span) != "all" || file.Text(set.Options[5].Span) != "?" || file.Text(set.Options[6].Span) != "all" || file.Text(set.Options[7].Span) != "_" {
		t.Fatalf("all split=%q,%q,%q,%q", file.Text(set.Options[4].Span), file.Text(set.Options[5].Span), file.Text(set.Options[6].Span), file.Text(set.Options[7].Span))
	}
	if file.Text(set.Options[8].Value) != "one\\ two\\|three\x16|four" || countTokens(file, TokenComment) != 1 {
		t.Fatalf("escaped value=%q tokens=%#v", file.Text(set.Options[8].Value), file.Tokens)
	}
}

func TestSetCommandBoundaryEscapesAndOpaqueProgress(t *testing.T) {
	legacy := (LegacyParser{}).Parse("set title=one\\\"two opaque=\\|bar =foo\\|tail | echo done\n")
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 2 || legacy.Commands[0].Set == nil || legacy.Commands[1].Canonical != "echo" {
		t.Fatalf("commands=%#v diagnostics=%#v", legacy.Commands, legacy.Diagnostics)
	}
	options := legacy.Commands[0].Set.Options
	if len(options) != 3 || legacy.Text(options[0].Value) != `one\"two` || legacy.Text(options[1].Span) != `opaque=\|bar` || legacy.Text(options[2].Span) != `=foo\|tail` {
		t.Fatalf("options=%#v", options)
	}

	termcode := (LegacyParser{}).Parse("set t_| echo done\n")
	if len(termcode.Diagnostics) != 0 || len(termcode.Commands) != 2 || termcode.Commands[0].Set == nil || termcode.Text(termcode.Commands[0].Set.Options[0].Name) != "t_" {
		t.Fatalf("termcode commands=%#v diagnostics=%#v", termcode.Commands, termcode.Diagnostics)
	}
}

func TestSetCommandVim9CommentsAndSeparatedItems(t *testing.T) {
	source := "vim9script\nset title=foo#bar escaped=foo\\#bar ts ? colon :1 # comment\n"
	file := (Vim9Parser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Set == nil || countTokens(file, TokenComment) != 1 {
		t.Fatalf("commands=%#v diagnostics=%#v tokens=%#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	options := file.Commands[1].Set.Options
	want := []string{"title=foo#bar", `escaped=foo\#bar`, "ts", "?", "colon", ":1"}
	if len(options) != len(want) {
		t.Fatalf("options=%#v", options)
	}
	for index := range want {
		if got := file.Text(options[index].Span); got != want[index] {
			t.Fatalf("option %d=%q want %q", index, got, want[index])
		}
	}
	if file.Text(options[2].Operator) != "" || file.Text(options[3].Operator) != "" || file.Text(options[5].Operator) != "" {
		t.Fatalf("separated operators=%#v", options)
	}
}

func TestSetCommandCtrlVDoesNotProtectValueWhitespace(t *testing.T) {
	file := (LegacyParser{}).Parse("set title=foo\x16 bar\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Set == nil || len(file.Commands[0].Set.Options) != 2 {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	options := file.Commands[0].Set.Options
	if file.Text(options[0].Value) != "foo\x16" || file.Text(options[1].Name) != "bar" {
		t.Fatalf("options=%#v", options)
	}
}

func TestSetCommandSpecialSuffixes(t *testing.T) {
	legacy := (LegacyParser{}).Parse("set all&vim no inv\n")
	if len(legacy.Diagnostics) != 0 || legacy.Commands[0].Set == nil || len(legacy.Commands[0].Set.Options) != 4 {
		t.Fatalf("commands=%#v diagnostics=%#v", legacy.Commands, legacy.Diagnostics)
	}
	options := legacy.Commands[0].Set.Options
	if legacy.Text(options[0].Name) != "all" || legacy.Text(options[0].Operator) != "&" || legacy.Text(options[1].Name) != "vim" || legacy.Text(options[2].Prefix) != "no" || options[2].Name.Start != options[2].Name.End || legacy.Text(options[3].Prefix) != "inv" || options[3].Name.Start != options[3].Name.End {
		t.Fatalf("options=%#v", options)
	}

	vim9 := (Vim9Parser{}).Parse("vim9script\nset termcap& | echo hidden\nset sw=2\n")
	if !hasDiagnostic(vim9, "vim/E1205") || countTokens(vim9, TokenSeparator) != 0 || len(vim9.Commands) != 3 || vim9.Commands[2].Set == nil {
		t.Fatalf("commands=%#v diagnostics=%#v tokens=%#v", vim9.Commands, vim9.Diagnostics, vim9.Tokens)
	}
}

func TestSetCommandVim9WhitespaceRecovery(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		code   string
	}{
		{"equals", "vim9script\nset ts =1 | echo hidden\nset sw=2\n", "vim/E1205"},
		{"add", "vim9script\nset ts +=1 | echo hidden\nset sw=2\n", "vim/E1205"},
		{"prepend", "vim9script\nset ts ^=2 | echo hidden\nset sw=2\n", "vim/E1205"},
		{"remove", "vim9script\nset ts -=1 | echo hidden\nset sw=2\n", "vim/E1205"},
		{"bang", "vim9script\nset hls ! | echo hidden\nset sw=2\n", "vim/E1205"},
		{"ampersand", "vim9script\nset hls &vim | echo hidden\nset sw=2\n", "vim/E1205"},
		{"angle", "vim9script\nset ts < | echo hidden\nset sw=2\n", "vim/E474"},
		{"angle-termcode", "vim9script\nset <t_xxx> | echo hidden\nset sw=2\n", "vim/E474"},
		{"angle-termcode-short", "vim9script\nset <t_>; | echo hidden\nset sw=2\n", "vim/E474"},
		{"angle-bar", "vim9script\nset <bad | echo '>'\nset sw=2\n", "vim/E474"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := (Vim9Parser{}).Parse(test.source)
			if !hasDiagnostic(file, test.code) || countTokens(file, TokenSeparator) != 0 || len(file.Commands) != 3 || file.Commands[2].Canonical != "set" {
				t.Fatalf("commands=%#v diagnostics=%#v tokens=%#v", file.Commands, file.Diagnostics, file.Tokens)
			}
		})
	}
}

func TestSetCommandLegacyWhitespaceAndContinuation(t *testing.T) {
	legacy := (LegacyParser{}).Parse("set ts =8 hls ! sw &\n")
	if len(legacy.Diagnostics) != 0 || legacy.Commands[0].Set == nil || len(legacy.Commands[0].Set.Options) != 3 {
		t.Fatalf("legacy=%#v diagnostics=%#v", legacy.Commands, legacy.Diagnostics)
	}
	legacyContinuation := (LegacyParser{}).Parse("set ts=8\n  \\ sw=2 | setlocal hls?\n")
	if len(legacyContinuation.Diagnostics) != 0 || len(legacyContinuation.Commands) != 2 || legacyContinuation.Commands[0].Set == nil || len(legacyContinuation.Commands[0].Set.Options) != 2 || legacyContinuation.Commands[1].Set == nil {
		t.Fatalf("continuation=%#v diagnostics=%#v", legacyContinuation.Commands, legacyContinuation.Diagnostics)
	}
	if legacyContinuation.Text(legacyContinuation.Commands[0].Set.Options[1].Value) != "2" {
		t.Fatalf("continuation option=%#v", legacyContinuation.Commands[0].Set.Options[1])
	}
	assertFileSpans(t, legacyContinuation)
}

func TestSetCommandModifierDialect(t *testing.T) {
	legacy := (LegacyParser{}).Parse("set ts =8\nvim9cmd set sw=2\n")
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 2 || legacy.Commands[0].Dialect != Legacy || legacy.Commands[1].Dialect != Vim9 || legacy.Commands[0].Set == nil || legacy.Commands[1].Set == nil {
		t.Fatalf("legacy commands=%#v diagnostics=%#v", legacy.Commands, legacy.Diagnostics)
	}

	vim9 := (Vim9Parser{}).Parse("vim9script\nlegacy set ts =8\nset sw=2\n")
	if len(vim9.Diagnostics) != 0 || len(vim9.Commands) != 3 || vim9.Commands[1].Dialect != Legacy || vim9.Commands[2].Dialect != Vim9 || vim9.Commands[1].Set == nil || vim9.Commands[2].Set == nil {
		t.Fatalf("vim9 commands=%#v diagnostics=%#v", vim9.Commands, vim9.Diagnostics)
	}
}

func TestSetCommandVim9LambdaSpans(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_func.vim Test_lambda_with_following_cmd.
	source := "vim9script\nvar Lambda = () => {\n  set ts=4\n} | set ts=3\n"
	file := (Vim9Parser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || file.Commands[1].Declaration == nil || file.Commands[2].Set == nil {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	lambda := file.Commands[1].Declaration.Initializer
	if lambda == nil || lambda.LambdaBody == nil || len(lambda.LambdaBody.Commands) != 1 {
		t.Fatalf("lambda=%#v", lambda)
	}
	command := lambda.LambdaBody.Commands[0]
	if command.Set == nil || len(command.Set.Options) != 1 {
		t.Fatalf("lambda command=%#v", command)
	}
	option := command.Set.Options[0]
	if file.Text(option.Span) != "ts=4" || file.Text(option.Name) != "ts" || file.Text(option.Operator) != "=" || file.Text(option.Value) != "4" {
		t.Fatalf("lambda option=%#v", option)
	}
	assertFileSpans(t, file)
}
