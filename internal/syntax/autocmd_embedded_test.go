package syntax

import (
	"strings"
	"testing"
)

func TestAutocmdEmbeddedBody(t *testing.T) {
	for _, source := range []string{
		"autocmd BufEnter * echo value\n",
		"autocmd mygroup BufEnter * echo value\n",
		"autocmd BufEnter * ++once ++nested echo value\n",
		"autocmd BufEnter foo\\ bar echo value\n",
		"autocmd BufEnter foo\\,bar echo value\n",
		"autocmd BufEnter <buffer> echo value\n",
	} {
		file := (LegacyParser{}).Parse(source)
		if len(file.Diagnostics) != 0 || len(file.Commands) != 1 {
			t.Fatalf("source = %q, commands = %#v, diagnostics = %#v", source, file.Commands, file.Diagnostics)
		}
		outer := &file.Commands[0]
		if outer.Embedded == nil || len(outer.Embedded.Commands) != 1 || outer.Embedded.Commands[0].Canonical != "echo" {
			t.Fatalf("source = %q, embedded = %#v", source, outer.Embedded)
		}
		if file.Text(outer.Embedded.Commands[0].Argument) != "value" {
			t.Fatalf("source = %q, body argument = %q", source, file.Text(outer.Embedded.Commands[0].Argument))
		}
	}
}

func TestAutocmdNoBodyFormsRemainOpaque(t *testing.T) {
	for _, source := range []string{
		"autocmd\n",
		"autocmd BufEnter\n",
		"autocmd BufEnter *\n",
		"autocmd! BufEnter *\n",
		"autocmd BufEnter * | echo outside\n",
	} {
		file := (LegacyParser{}).Parse(source)
		if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Embedded != nil {
			t.Fatalf("source = %q, command = %#v, diagnostics = %#v", source, file.Commands, file.Diagnostics)
		}
	}
}

func TestVim9AutocmdCommandListContinuation(t *testing.T) {
	source := "vim9script\nautocmd BufNewFile *.match if condition\n  | echo 'match'\n  | endif\nvar after = 1\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || file.Commands[1].Embedded == nil {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	outer := &file.Commands[1]
	if len(outer.Embedded.Commands) != 3 || len(outer.Embedded.Blocks) != 1 || outer.Embedded.Blocks[0].Kind != BlockIf {
		t.Fatalf("embedded = %#v", outer.Embedded)
	}
	if file.Text(outer.Embedded.Commands[1].Argument) != "'match'" || file.Commands[2].Declaration == nil {
		t.Fatalf("commands = %#v, arg = %q, declaration = %#v", file.Commands, file.Text(outer.Embedded.Commands[1].Argument), file.Commands[2].Declaration)
	}
	if file.Text(outer.Argument) != "BufNewFile *.match if condition\n  | echo 'match'\n  | endif" {
		t.Fatalf("autocmd payload = %q", file.Text(outer.Argument))
	}
}

func TestAutocmdEmbeddedBlockRecoveryAndAbsoluteSpans(t *testing.T) {
	source := "autocmd BufEnter * if condition | echo value\nedit next.txt\n"
	file := (LegacyParser{}).Parse(source)
	if !hasDiagnostic(file, "vimls/missing-end") || len(file.Commands) != 2 || file.Commands[1].Canonical != "edit" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	outer := &file.Commands[0]
	if outer.Embedded == nil || len(outer.Embedded.Commands) != 2 || len(outer.Embedded.Blocks) != 1 {
		t.Fatalf("embedded = %#v", outer.Embedded)
	}
	for _, command := range outer.Embedded.Commands {
		if command.Span.Start < outer.Argument.Start || command.Span.End > outer.Argument.End || file.Text(command.Span) == "" {
			t.Fatalf("command span = %#v", command)
		}
	}
}

func TestLegacyAutocmdContinuationRebasesFromBodySlice(t *testing.T) {
	prefix := strings.Repeat("let g:prefix = 1\n", 64)
	source := prefix + "autocmd BufEnter *\n  \\ if exists('x') |\n  \\ echo x |\n  \\ endif\nlet after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 66 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	outer := &file.Commands[len(file.Commands)-2]
	if outer.Canonical != "autocmd" || outer.Embedded == nil || len(outer.Embedded.Commands) != 3 {
		t.Fatalf("autocmd = %#v", outer)
	}
	for _, command := range outer.Embedded.Commands {
		if command.Span.Start < outer.Argument.Start || command.Span.End > outer.Argument.End || file.Text(command.Span) == "" {
			t.Fatalf("embedded command span = %#v, argument = %#v", command, outer.Argument)
		}
	}
	if file.Text(outer.Embedded.Commands[1].Argument) != "x" || file.Text(file.Commands[len(file.Commands)-1].Declaration.Name) != "after" {
		t.Fatalf("embedded = %#v, following = %#v", outer.Embedded.Commands, file.Commands[len(file.Commands)-1])
	}
}

func TestLegacyAutocmdBackslashContinuation(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "body continuation",
			source: "autocmd BufEnter *\n  \\ echo one |\n  \\ echo two\n",
		},
		{
			name:   "separator continuation",
			source: "autocmd BufEnter * echo one\n  \\| echo two\n",
		},
		{
			name:   "leading separator continuation",
			source: "autocmd BufEnter *\n  \\| echo one\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := (LegacyParser{}).Parse(test.source)
			if len(file.Diagnostics) != 0 || len(file.Commands) != 1 {
				t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
			}
			outer := &file.Commands[0]
			expectedCommands := 2
			if test.name == "leading separator continuation" {
				expectedCommands = 1
			}
			if outer.Embedded == nil || len(outer.Embedded.Commands) != expectedCommands {
				t.Fatalf("embedded = %#v", outer.Embedded)
			}
			expectedArguments := []string{"one", "two"}
			if expectedCommands == 1 {
				expectedArguments = expectedArguments[:1]
			}
			for index, expected := range expectedArguments {
				command := &outer.Embedded.Commands[index]
				if command.Canonical != "echo" || file.Text(command.Argument) != expected {
					t.Fatalf("command %d = %#v, argument = %q", index, command, file.Text(command.Argument))
				}
				if command.Span.Start < outer.Argument.Start || command.Span.End > outer.Argument.End || file.Text(command.Span) == "" {
					t.Fatalf("command %d has invalid absolute span: %#v", index, command)
				}
			}
		})
	}
}

func TestLegacyAutocmdBackslashContinuationBuildsNestedBlock(t *testing.T) {
	source := "autocmd BufEnter *\n  \\ if exists('x') |\n  \\ echo x |\n  \\ endif\nlet after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	outer := &file.Commands[0]
	if outer.Embedded == nil || len(outer.Embedded.Commands) != 3 || len(outer.Embedded.Blocks) != 1 || outer.Embedded.Blocks[0].Kind != BlockIf {
		t.Fatalf("embedded = %#v", outer.Embedded)
	}
	block := outer.Embedded.Blocks[0]
	if block.Span.Start < outer.Argument.Start || block.Span.End > outer.Argument.End {
		t.Fatalf("block has invalid absolute span: %#v", block)
	}
	if file.Commands[1].Canonical != "let" || file.Commands[1].Declaration == nil {
		t.Fatalf("commands = %#v", file.Commands)
	}
}
