package syntax

import "testing"

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

func TestVim9UserCommandBlockDoesNotDuplicateDiagnostics(t *testing.T) {
	file := Parse("vim9script\ncommand Foo {\n  if true\n    echo 'missing end'\n}\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vimls/missing-end" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if file.Commands[1].Embedded != nil || len(file.Blocks) != 2 || file.Blocks[0].Kind != BlockCommand || file.Blocks[1].Kind != BlockIf || file.Blocks[1].End != -1 {
		t.Fatalf("blocks = %#v, command = %#v", file.Blocks, file.Commands[1])
	}
}
