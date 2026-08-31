package syntax

import "testing"

func TestCollectedCommandBlockBarSeparatorDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source, tail string
	}{
		{
			name:   "user command unlet",
			source: "vim9script\ncommand Foo {\n  unlet value | echo 'after'\n}\nvar after = 1\n",
			tail:   "| echo 'after'",
		},
		{
			name:   "user command final",
			source: "vim9script\ncommand Foo {\n  final value = 1 | echo 'after'\n}\nvar after = 1\n",
			tail:   "| echo 'after'",
		},
		{
			name:   "user command substitute",
			source: "vim9script\ncommand Foo {\n  substitute /a/b/ | echo 'after'\n}\nvar after = 1\n",
			tail:   "| echo 'after'",
		},
		{
			name:   "user command wincmd",
			source: "vim9script\ncommand Foo {\n  wincmd w | echo 'after'\n}\nvar after = 1\n",
			tail:   "| echo 'after'",
		},
		{
			name:   "user command legacy unlet",
			source: "vim9script\ncommand Foo {\n  legacy unlet value | echo 'after'\n}\nvar after = 1\n",
			tail:   "| echo 'after'",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1231" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || file.Text(got[0].Span) != "|" || got[0].Message != "Cannot use a bar to separate commands here: "+test.tail {
				t.Fatalf("E1231 diagnostics = %#v", got)
			}
			lineEnd, _ := physicalLineEnd(file.Source, got[0].Span.Start)
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code != "vim/E1231" && diagnostic.Span.Start >= got[0].Span.End && diagnostic.Span.Start < lineEnd {
					t.Fatalf("post-bar diagnostic = %#v", diagnostic)
				}
			}
			if len(file.Commands) == 0 || file.Commands[len(file.Commands)-1].Declaration == nil || file.Text(file.Commands[len(file.Commands)-1].Declaration.Name) != "after" {
				t.Fatalf("following declaration was not retained: %#v", file.Commands)
			}
			for _, command := range file.Commands {
				if command.Canonical == "echo" {
					t.Fatalf("post-bar echo was retained: %#v", file.Commands)
				}
			}
		})
	}

	for _, test := range []struct {
		name, source string
	}{
		{"echo remains valid", "vim9script\ncommand Foo {\n  echo 'hello' | echo 'there'\n}\n"},
		{"wincmd operand bar", "vim9script\ncommand Foo {\n  wincmd |\n}\n"},
		{"wincmd g operand bar", "vim9script\ncommand Foo {\n  wincmd g|\n}\n"},
		{"outside block remains split", "vim9script\nwincmd w | echo 'after'\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1231" {
					t.Fatalf("unexpected E1231: %#v", file.Diagnostics)
				}
			}
			if test.name == "outside block remains split" && len(file.Commands) != 3 {
				t.Fatalf("outside consumer did not split: %#v", file.Commands)
			}
			if test.name == "wincmd operand bar" || test.name == "wincmd g operand bar" {
				count := 0
				for _, command := range file.Commands {
					if command.Canonical == "wincmd" {
						count++
					}
				}
				if count != 1 {
					t.Fatalf("wincmd operand was split: %#v", file.Commands)
				}
			}
		})
	}
}

func TestUserCommandReplacementBody(t *testing.T) {
	source := "command! -nargs=* -complete=command Foo echo one | echo two\n" +
		"command Bar map x foo\\|bar\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	foo := &file.Commands[0]
	if foo.Bang.Start == foo.Bang.End || foo.Embedded == nil || len(foo.Embedded.Commands) != 2 {
		t.Fatalf("Foo = %#v", foo)
	}
	if file.Text(foo.Embedded.Span) != "echo one | echo two" || file.Text(foo.Embedded.Commands[0].Argument) != "one" || file.Text(foo.Embedded.Commands[1].Argument) != "two" {
		t.Fatalf("Foo body = %#v", foo.Embedded)
	}
	bar := &file.Commands[1]
	if bar.Embedded == nil || len(bar.Embedded.Commands) != 1 || file.Text(bar.Embedded.Commands[0].Argument) != "x foo\\|bar" {
		t.Fatalf("Bar body = %#v", bar.Embedded)
	}
}

func TestUserCommandListingAndQueryHaveNoBody(t *testing.T) {
	for _, source := range []string{
		"command\n",
		"command Foo\n",
		"command -nargs=* Foo\n",
	} {
		file := (LegacyParser{}).Parse(source)
		if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Embedded != nil {
			t.Fatalf("source = %q, command = %#v, diagnostics = %#v", source, file.Commands, file.Diagnostics)
		}
	}
}

func TestUserCommandLowercaseNameDiagnostic(t *testing.T) {
	for _, test := range []struct {
		name, source, span string
		wantFollowing      bool
	}{
		{name: "legacy definition", source: "command! docmd :\nlet after = 1\n", span: "docmd", wantFollowing: true},
		{name: "Vim9 definition", source: "vim9script\ncommand docmd echo 'value'\nvar after = 1\n", span: "docmd", wantFollowing: true},
		{name: "valid completion attribute", source: "command! -complete=custom,CustomComplete docmd :\nlet after = 1\n", span: "docmd", wantFollowing: true},
		{name: "inside function", source: "function Check()\n  command! apple echo 'value'\nendfunction\n", span: "apple"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E183" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "User defined commands must start with an uppercase letter" || file.Text(got[0].Span) != test.span {
				t.Fatalf("E183 diagnostics = %#v", file.Diagnostics)
			}
			if test.wantFollowing && file.Commands[len(file.Commands)-1].Declaration == nil {
				t.Fatalf("following declaration was not retained: %#v", file.Commands)
			}
		})
	}

	for _, source := range []string{
		"command docmd\n",
		"command! DoCmd :\n",
		"command! _ :\n",
		"command! doc_md :\n",
		"command! -xxx docmd :\n",
	} {
		file := Parse(source)
		if hasDiagnostic(file, "vim/E183") {
			t.Fatalf("guard unexpectedly received E183: %#v\n%s", file.Diagnostics, source)
		}
	}
}

func TestUserCommandReplacementRecognizesVimBangVariants(t *testing.T) {
	for _, replacement := range []string{"<q-bang>", "<Q-BANG>", "<f-bang>"} {
		source := "command! -bang Clean call s:clean(" + replacement + " == \"!\")\n"
		file := (LegacyParser{}).Parse(source)
		if len(file.Diagnostics) != 0 || len(file.Commands) != 1 {
			t.Fatalf("replacement %q: commands = %#v, diagnostics = %#v", replacement, file.Commands, file.Diagnostics)
		}
		body := file.Commands[0].Embedded
		if body == nil || len(body.Commands) != 1 || file.Text(body.Span) != "call s:clean("+replacement+" == \"!\")" {
			t.Fatalf("replacement %q: body = %#v", replacement, body)
		}
		if body.Commands[0].Expressions == nil || body.Commands[0].Expressions[0].Kind != ExpressionCall {
			t.Fatalf("replacement %q: embedded command = %#v", replacement, body.Commands[0])
		}
	}
}

func TestVim9UserCommandCompletionRequiresArguments(t *testing.T) {
	tests := []struct {
		source string
		span   string
	}{
		{source: "com! -complete=file DoCmd :", span: "-complete=file"},
		{source: "com! -nargs=0 -complete=file DoCmd :", span: "-complete=file"},
	}
	for _, test := range tests {
		file := Parse("vim9script\n" + test.source + "\nvar after = 1\n")
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1208" || file.Diagnostics[0].Message != "-complete used without allowing arguments" || file.Text(file.Diagnostics[0].Span) != test.span {
			t.Fatalf("%q diagnostics = %#v", test.source, file.Diagnostics)
		}
		if len(file.Commands) != 3 || file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "after" {
			t.Fatalf("%q commands = %#v", test.source, file.Commands)
		}
	}
	for _, source := range []string{
		"vim9script\ncom! -nargs=1 -complete=file DoCmd :\n",
		"vim9script\ncom! -complete=file -nargs=_ DoCmd :\n",
		"com! -complete=file DoCmd :\n",
	} {
		file := Parse(source)
		if len(file.Diagnostics) != 0 {
			t.Fatalf("%q diagnostics = %#v", source, file.Diagnostics)
		}
	}
}

func TestUserCommandInvalidArgumentCount(t *testing.T) {
	tests := []struct {
		name, source, span string
	}{
		{
			name:   "legacy invalid value",
			source: "command! -nargs=x DoCmd :\nlet after = 1\n",
			span:   "-nargs=x",
		},
		{
			name:   "Vim9 invalid value",
			source: "vim9script\ncommand! -nargs=12 DoCmd :\nvar after = 1\n",
			span:   "-nargs=12",
		},
		{
			name:   "empty value",
			source: "command! -nargs= DoCmd :\necho after\n",
			span:   "-nargs=",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E176" || file.Diagnostics[0].Message != "Invalid number of arguments" || file.Text(file.Diagnostics[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want E176 on %q", file.Diagnostics, test.span)
			}
			if len(file.Commands) < 2 || file.Commands[len(file.Commands)-1].Span.Start <= file.Diagnostics[0].Span.End {
				t.Fatalf("parser did not recover after invalid -nargs: %#v", file.Commands)
			}
		})
	}

	for _, dialect := range []string{"", "vim9script\n"} {
		for _, value := range []string{"0", "1", "*", "?", "+", "_"} {
			file := Parse(dialect + "command! -nargs=" + value + " DoCmd :\n")
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E176" {
					t.Fatalf("valid -nargs=%s produced E176: %#v", value, file.Diagnostics)
				}
			}
		}
	}
}

func TestUserCommandCompleteoptEscapeRejectsWholeArgumentMode(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "legacy official order",
			source: "com! -nargs=_ -complete=customlist,EscOne -completeopt=escape DoCmd :\nlet after = 1\n",
		},
		{
			name:   "legacy reverse order",
			source: "com! -completeopt=escape -nargs=_ DoCmd :\nlet after = 1\n",
		},
		{
			name:   "Vim9",
			source: "vim9script\ncom! -completeopt=escape -nargs=_ DoCmd :\nvar after = 1\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1579" || file.Diagnostics[0].Message != "-completeopt=escape cannot be used with -nargs=_" || file.Text(file.Diagnostics[0].Span) != "-completeopt=escape" {
				t.Fatalf("diagnostics = %#v, want E1579 on -completeopt=escape", file.Diagnostics)
			}
			if len(file.Commands) < 2 || file.Commands[len(file.Commands)-1].Span.Start <= file.Diagnostics[0].Span.End {
				t.Fatalf("parser did not recover after E1579: %#v", file.Commands)
			}
		})
	}

	for _, nargs := range []string{"0", "1", "*", "?", "+"} {
		file := Parse("command! -nargs=" + nargs + " -completeopt=escape DoCmd :\n")
		for _, diagnostic := range file.Diagnostics {
			if diagnostic.Code == "vim/E1579" {
				t.Fatalf("valid -nargs=%s produced E1579: %#v", nargs, file.Diagnostics)
			}
		}
	}
}

func TestVim9UserCommandBlockBody(t *testing.T) {
	source := "vim9script\ncommand Foo {\n  var value = 1\n  if value == 1\n    echo 'ok'\n  endif\n}\necho done\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 8 || len(file.Blocks) != 2 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	command := &file.Commands[1]
	if command.Embedded != nil || command.Block < 0 || file.Blocks[command.Block].Kind != BlockCommand {
		t.Fatalf("command block = %#v, blocks = %#v", command, file.Blocks)
	}
	block := file.Blocks[command.Block]
	if block.Header != 1 || block.End != 6 || block.Span.Start != command.Span.Start || block.Span.End != file.Commands[6].Span.End {
		t.Fatalf("command block = %#v", block)
	}
	if file.Text(file.Commands[2].Argument) != "value = 1" || file.Text(file.Commands[4].Argument) != "'ok'" || file.Commands[3].Block == command.Block {
		t.Fatalf("body commands = %#v", file.Commands[2:6])
	}
}

func TestLegacyUserCommandBlockBody(t *testing.T) {
	source := "command Foo {\n  var value = 1\n}\nlet after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	owner := &file.Commands[0]
	if owner.Dialect != Legacy || owner.Embedded == nil || len(owner.Embedded.Commands) != 1 || owner.Embedded.Commands[0].Dialect != Vim9 || owner.Embedded.Commands[0].Declaration == nil || file.Text(owner.Embedded.Commands[0].Declaration.Name) != "value" {
		t.Fatalf("owner = %#v", owner)
	}
}

func TestNestedUserCommandBlockBody(t *testing.T) {
	source := "command DefineIt command DoNested {\n  var value = 1\n}\nlet after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	outer := &file.Commands[0]
	if outer.Embedded == nil || len(outer.Embedded.Commands) != 1 {
		t.Fatalf("outer = %#v", outer)
	}
	owner := &outer.Embedded.Commands[0]
	if owner.Canonical != "command" || owner.Dialect != Legacy || owner.Embedded == nil || len(owner.Embedded.Commands) != 1 || owner.Embedded.Commands[0].Dialect != Vim9 || owner.Embedded.Commands[0].Declaration == nil {
		t.Fatalf("owner = %#v", owner)
	}
}

func TestVim9UserCommandBlockDoesNotDuplicateDiagnostics(t *testing.T) {
	file := Parse("vim9script\ncommand Foo {\n  if true\n    echo 'missing end'\n}\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vimls/missing-end" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if file.Commands[1].Embedded != nil || len(file.Blocks) != 2 || file.Blocks[0].Kind != BlockCommand || file.Blocks[1].Kind != BlockIf || file.Blocks[1].End != -1 {
		t.Fatalf("blocks = %#v, command = %#v", file.Blocks, file.Commands[1])
	}
}

func TestUserCommandMissingBlockEnd(t *testing.T) {
	source := "command DoesNotEnd {\n   echo 'hello'\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1026" || file.Text(file.Diagnostics[0].Span) != "{" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 1 || file.Commands[0].Span.End != len(source) || file.Commands[0].Embedded == nil || len(file.Commands[0].Embedded.Commands) != 1 {
		t.Fatalf("commands = %#v", file.Commands)
	}
	body := file.Commands[0].Embedded.Commands[0]
	if body.Dialect != Vim9 || body.Canonical != "echo" || file.Text(body.Argument) != "'hello'" {
		t.Fatalf("body = %#v", body)
	}
	assertFileSpans(t, file)
}

func TestUserCommandStrayBlockEnd(t *testing.T) {
	source := "command BadCommand {\n" +
		"   echo  {\n" +
		"   'key': 'value',\n" +
		"    }\n" +
		"    }\n" +
		"BadCommand\n"
	file := Parse(source)
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1128" || file.Diagnostics[0].Message != "} without {" || file.Text(file.Diagnostics[0].Span) != "}" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 3 || file.Text(file.Commands[0].Argument) != "BadCommand {\n   echo  {\n   'key': 'value',\n    }" || file.Commands[1].Canonical != "}" || file.Commands[2].Canonical != "BadCommand" {
		t.Fatalf("commands = %#v", file.Commands)
	}
	if file.Commands[1].Span != file.Diagnostics[0].Span || file.Commands[1].Span.End-file.Commands[1].Span.Start != 1 {
		t.Fatalf("stray close = %#v, diagnostic = %#v", file.Commands[1], file.Diagnostics[0])
	}
	legacy := (LegacyParser{}).Parse("}\nlet after = 1\n")
	for _, diagnostic := range legacy.Diagnostics {
		if diagnostic.Code == "vim/E1128" {
			t.Fatalf("legacy stray close diagnostics = %#v", legacy.Diagnostics)
		}
	}
	assertFileSpans(t, file)
}
