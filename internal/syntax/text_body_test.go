package syntax

import "testing"

func TestLegacyTextCommandsConsumeOpaquePhysicalBody(t *testing.T) {
	for _, name := range []string{"append", "change", "insert"} {
		t.Run(name, func(t *testing.T) {
			source := "function! Update()\n  " + name + "!\nnot a command | still text\n\" still literal text\n\n.\n  let g:after = 1\nendfunction\n"
			file := (LegacyParser{}).Parse(source)
			if len(file.Diagnostics) != 0 || len(file.Commands) != 4 || len(file.Blocks) != 1 {
				t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
			}
			command := &file.Commands[1]
			if command.Canonical != name || file.Text(command.Bang) != "!" || command.TextBody == nil {
				t.Fatalf("text command = %#v", command)
			}
			body := command.TextBody
			if len(body.Lines) != 3 || file.Text(body.Lines[0]) != "not a command | still text" || file.Text(body.Lines[1]) != "\" still literal text" || file.Text(body.Lines[2]) != "" {
				t.Fatalf("body lines = %#v", body.Lines)
			}
			if got := file.Text(body.Body); got != "not a command | still text\n\" still literal text\n" {
				t.Fatalf("body = %q, span = %#v", got, body.Body)
			}
			if file.Text(body.EndMarker) != "." || command.Span.End != body.EndMarker.End {
				t.Fatalf("end marker = %#v, command span = %#v", body.EndMarker, command.Span)
			}
			if file.Commands[2].Canonical != "let" || file.Commands[2].Declaration == nil || file.Commands[3].Canonical != "endfunction" {
				t.Fatalf("following commands = %#v", file.Commands[2:])
			}
			assertFileSpans(t, file)
		})
	}
}

func TestLegacyTextCommandInlineFirstLineOwnsBarsQuotesAndTrailingSpace(t *testing.T) {
	source := "append |one \" literal | text  \ntwo\n.\nlet g:after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || countTokens(file, TokenSeparator) != 0 {
		t.Fatalf("commands = %#v, tokens = %#v, diagnostics = %#v", file.Commands, file.Tokens, file.Diagnostics)
	}
	command := &file.Commands[0]
	if command.TextBody == nil || file.Text(command.Argument) != "|one \" literal | text  " || file.Text(command.TextBody.Separator) != "|" {
		t.Fatalf("append = %#v, argument = %q", command, file.Text(command.Argument))
	}
	if len(command.TextBody.Lines) != 2 || file.Text(command.TextBody.Lines[0]) != "one \" literal | text  " || file.Text(command.TextBody.Lines[1]) != "two" {
		t.Fatalf("lines = %#v", command.TextBody.Lines)
	}
	if got := file.Text(command.TextBody.Body); got != "one \" literal | text  \ntwo" {
		t.Fatalf("body = %q", got)
	}
	if file.Commands[1].Canonical != "let" || file.Commands[1].Declaration == nil {
		t.Fatalf("following command = %#v", file.Commands[1])
	}
	assertFileSpans(t, file)
}

func TestLegacyTextCommandRequiresExactPhysicalDotAndHandlesCRLF(t *testing.T) {
	source := "a\r\n .\r\n. \r\n.\r\nlet g:after = 1\r\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[0].Canonical != "append" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	body := file.Commands[0].TextBody
	if body == nil || len(body.Lines) != 2 || file.Text(body.Lines[0]) != " ." || file.Text(body.Lines[1]) != ". " || file.Text(body.EndMarker) != "." {
		t.Fatalf("text body = %#v", body)
	}
	if file.Commands[1].Canonical != "let" || file.Commands[1].Declaration == nil {
		t.Fatalf("following command = %#v", file.Commands[1])
	}
	assertFileSpans(t, file)
}

func TestIncompleteLegacyTextBodyRemainsOpaqueThroughEOF(t *testing.T) {
	file := (LegacyParser{}).Parse("append\npayload | not a command\nlet this is still text\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	body := file.Commands[0].TextBody
	if body == nil || !body.Incomplete || len(body.Lines) != 2 || body.EndMarker != (Span{}) || file.Text(body.Body) != "payload | not a command\nlet this is still text" {
		t.Fatalf("text body = %#v, body text = %q", body, file.Text(body.Body))
	}
	assertFileSpans(t, file)
}

func TestInvalidLegacyTextCommandHeaderRecoversOnNextLine(t *testing.T) {
	for _, source := range []string{
		"append trailing\nlet g:after = 1\n",
		"insert 2\nlet g:after = 1\n",
		"change trailing\nlet g:after = 1\n",
	} {
		file := (LegacyParser{}).Parse(source)
		if len(file.Commands) != 2 || !hasDiagnostic(file, "vim/E488") {
			t.Fatalf("source = %q, commands = %#v, diagnostics = %#v", source, file.Commands, file.Diagnostics)
		}
		if file.Commands[0].TextBody != nil || file.Commands[1].Canonical != "let" || file.Commands[1].Declaration == nil {
			t.Fatalf("source = %q, commands = %#v", source, file.Commands)
		}
		assertFileSpans(t, file)
	}
}

func TestLegacyChangeCountAndInlineFirstLine(t *testing.T) {
	file := (LegacyParser{}).Parse("change2|first\nsecond\n.\nlet g:after = 1\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	command := &file.Commands[0]
	if file.Text(command.Count) != "2" || command.TextBody == nil || file.Text(command.TextBody.Separator) != "|" || file.Text(command.TextBody.Body) != "first\nsecond" {
		t.Fatalf("change = %#v, body = %#v", command, command.TextBody)
	}
	if file.Commands[1].Canonical != "let" || file.Commands[1].Declaration == nil {
		t.Fatalf("following command = %#v", file.Commands[1])
	}
	assertFileSpans(t, file)
}

func TestIncompleteLegacyFunctionTextBodyRecoversAtEndFunction(t *testing.T) {
	source := "function! Update()\n  append\npayload | still text\nendfunction\nlet g:after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Commands) != 4 || len(file.Blocks) != 1 || file.Blocks[0].End != 2 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	body := file.Commands[1].TextBody
	if body == nil || !body.Incomplete || len(body.Lines) != 1 || file.Text(body.Body) != "payload | still text" || body.EndMarker != (Span{}) {
		t.Fatalf("text body = %#v, body = %q", body, file.Text(body.Body))
	}
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1145" || file.Commands[2].Canonical != "endfunction" || file.Commands[3].Canonical != "let" || file.Commands[3].Declaration == nil {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	assertFileSpans(t, file)
}

func TestCompleteLegacyFunctionTextBodyCanContainEndFunctionLine(t *testing.T) {
	source := "function! Update()\n  append\nendfunction\nstill text\n.\nendfunction\nlet g:after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 || len(file.Blocks) != 1 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	body := file.Commands[1].TextBody
	if body == nil || body.Incomplete || len(body.Lines) != 2 || file.Text(body.Lines[0]) != "endfunction" || file.Text(body.Body) != "endfunction\nstill text" || file.Text(body.EndMarker) != "." {
		t.Fatalf("text body = %#v, body = %q", body, file.Text(body.Body))
	}
	if file.Commands[2].Canonical != "endfunction" || file.Commands[3].Canonical != "let" {
		t.Fatalf("following commands = %#v", file.Commands[2:])
	}
	assertFileSpans(t, file)
}

func TestTextCommandsAreRejectedInVim9WithoutLegacyModifier(t *testing.T) {
	for _, name := range []string{"append", "change", "insert", "open", "t", "xit"} {
		t.Run(name, func(t *testing.T) {
			file := Parse("vim9script\n" + name + "\nvar after = 1\n")
			if len(file.Commands) != 3 || !hasDiagnostic(file, "vim/E1100") {
				t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
			}
			if file.Commands[1].TextBody != nil || file.Commands[2].Canonical != "var" || file.Commands[2].Declaration == nil {
				t.Fatalf("commands = %#v", file.Commands)
			}
			assertFileSpans(t, file)
		})
	}

	expression := Parse("vim9script\nappend(0, 'text')\n")
	if len(expression.Diagnostics) != 0 || len(expression.Commands) != 2 || expression.Commands[1].Kind != CommandExpression {
		t.Fatalf("expression commands = %#v, diagnostics = %#v", expression.Commands, expression.Diagnostics)
	}

	prefixed := (LegacyParser{}).Parse("vim9cmd append\nlet g:after = 1\n")
	if len(prefixed.Commands) != 2 || !hasDiagnostic(prefixed, "vim/E1100") || prefixed.Commands[1].Canonical != "let" {
		t.Fatalf("prefixed commands = %#v, diagnostics = %#v", prefixed.Commands, prefixed.Diagnostics)
	}

}

func TestModifiedTextCommandInsideFunctionUsesInlineBodyOnly(t *testing.T) {
	file := Parse("vim9script\ndef F()\n  legacy append | one\n  var after = 1\nenddef\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 || len(file.Blocks) != 1 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	command := &file.Commands[2]
	if command.TextBody == nil || len(command.TextBody.Lines) != 1 || file.Text(command.TextBody.Body) != " one" || command.TextBody.EndMarker != (Span{}) || command.TextBody.Incomplete {
		t.Fatalf("inline text body = %#v, text = %q", command.TextBody, file.Text(command.TextBody.Body))
	}
	if file.Commands[3].Canonical != "var" || file.Commands[3].Declaration == nil || file.Commands[4].Canonical != "enddef" {
		t.Fatalf("following commands = %#v", file.Commands[3:])
	}
	assertFileSpans(t, file)

	physical := Parse("vim9script\ndef F()\n  legacy append\n  one\nenddef\n")
	if len(physical.Commands) != 5 || physical.Commands[2].TextBody != nil || physical.Commands[3].TypedName != "one" || physical.Commands[4].Canonical != "enddef" {
		t.Fatalf("physical commands = %#v, diagnostics = %#v", physical.Commands, physical.Diagnostics)
	}
}

func TestCommandBlockClosesIncompleteDeferredTextBody(t *testing.T) {
	file := Parse("vim9script\ncommand InsertText {\n  legacy append\npayload | still text\n}\nvar after = 1\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 || len(file.Blocks) != 1 || file.Blocks[0].End != 3 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	body := file.Commands[2].TextBody
	if body == nil || !body.Incomplete || len(body.Lines) != 2 || file.Text(body.Body) != "payload | still text\n}" || file.Text(body.Lines[1]) != "}" || body.EndMarker != (Span{}) {
		t.Fatalf("text body = %#v, text = %q", body, file.Text(body.Body))
	}
	if file.Commands[3].Kind != CommandBlockEnd || file.Commands[4].Declaration == nil {
		t.Fatalf("following commands = %#v", file.Commands[3:])
	}
	assertFileSpans(t, file)
}

func TestLegacyTextBodyInsideVim9FileRestoresVim9Dialect(t *testing.T) {
	file := Parse("vim9script\nlegacy append\ntext\n.\nvar after = 1\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[1].Canonical != "append" || file.Commands[1].Dialect != Legacy || file.Commands[1].TextBody == nil || file.Text(file.Commands[1].TextBody.Body) != "text" {
		t.Fatalf("legacy append = %#v", file.Commands[1])
	}
	if file.Commands[2].Dialect != Vim9 || file.Commands[2].Declaration == nil {
		t.Fatalf("following Vim9 command = %#v", file.Commands[2])
	}
}
