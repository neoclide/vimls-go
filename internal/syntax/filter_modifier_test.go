package syntax

import (
	"strings"
	"testing"
)

func TestFilterModifierFormsAndSpans(t *testing.T) {
	tests := []struct {
		name, source, pattern, delimiter, flags, command string
		dialect                                          Dialect
		bang                                             bool
	}{
		{name: "legacy abbreviation", dialect: Legacy, source: "filt foo echo value\n", pattern: "foo", command: "echo"},
		{name: "legacy bang", dialect: Legacy, source: "filter! foo echo value\n", pattern: "foo", bang: true, command: "echo"},
		{name: "legacy separated bang", dialect: Legacy, source: "filter ! /foo/ echo value\n", pattern: "foo", delimiter: "/", bang: true, command: "echo"},
		{name: "vim9 separated bang", dialect: Vim9, source: "filter ! /foo/ echo value\n", pattern: "foo", delimiter: "/", bang: true, command: "echo"},
		{name: "slash regexp", dialect: Legacy, source: "filter /foo/ echo value\n", pattern: "foo", delimiter: "/", command: "echo"},
		{name: "hash regexp in vim9", dialect: Vim9, source: "filter #foo# echo value\n", pattern: "foo", delimiter: "#", command: "echo"},
		{name: "quote regexp in vim9", dialect: Vim9, source: "filter \" foo \" echo value\n", pattern: " foo ", delimiter: "\"", command: "echo"},
		{name: "regexp flags", dialect: Legacy, source: "filter /foo/gjf echo value\n", pattern: "foo", delimiter: "/", flags: "gjf", command: "echo"},
		{name: "escaped delimiter", dialect: Legacy, source: "filter /foo" + "\\" + "/bar/ echo value\n", pattern: "foo" + "\\" + "/bar", delimiter: "/", command: "echo"},
		{name: "collection delimiter", dialect: Legacy, source: "filter /[a/b]/ echo value\n", pattern: "[a/b]", delimiter: "/", command: "echo"},
		{name: "nested windo", dialect: Legacy, source: "filter /foo/ windo echo value\n", pattern: "foo", delimiter: "/", command: "windo"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var file *File
			if test.dialect == Vim9 {
				file = (Vim9Parser{}).Parse(test.source)
			} else {
				file = (LegacyParser{}).Parse(test.source)
			}
			if len(file.Diagnostics) != 0 || len(file.Commands) != 1 {
				t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
			}
			command := &file.Commands[0]
			if command.Canonical != test.command || len(command.Modifiers) != 1 {
				t.Fatalf("command = %#v", command)
			}
			modifier := &command.Modifiers[0]
			if modifier.Name != "filter" || modifier.Filter == nil || file.Text(modifier.Filter.Pattern) != test.pattern || (modifier.Bang.Start < modifier.Bang.End) != test.bang {
				t.Fatalf("modifier = %#v", modifier)
			}
			if file.Text(modifier.Filter.Delimiter) != test.delimiter || file.Text(modifier.Filter.Flags) != test.flags {
				t.Fatalf("delimiter/flags = %q/%q, modifier = %#v", file.Text(modifier.Filter.Delimiter), file.Text(modifier.Filter.Flags), modifier)
			}
			wantModifierEnd := len(strings.Fields(test.source)[0])
			if modifier.Span.Start != 0 || modifier.Span.End != wantModifierEnd {
				t.Fatalf("modifier span = %#v", modifier.Span)
			}
			if test.bang && file.Text(modifier.Bang) != "!" {
				t.Fatalf("bang span = %#v", modifier.Bang)
			}
			if test.bang && modifier.Bang != (Span{Start: strings.Index(test.source, "!"), End: strings.Index(test.source, "!") + 1}) {
				t.Fatalf("exact bang span = %#v", modifier.Bang)
			}
			if command.Span.Start != 0 || test.command == "echo" && file.Text(command.Argument) != "value" {
				t.Fatalf("command span/argument = %#v/%q", command.Span, file.Text(command.Argument))
			}
			if test.command == "windo" && (command.Embedded == nil || len(command.Embedded.Commands) != 1 || command.Embedded.Commands[0].Canonical != "echo") {
				t.Fatalf("nested command = %#v", command.Embedded)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestFilterModifierVim9CommandAndFunctionBoundaries(t *testing.T) {
	for _, source := range []string{
		"vim9script\nfilter(arg)\nvar after = 1\n",
		"vim9script\nfilter!(/arg/)\nvar after = 1\n",
		"vim9script\nfilter/foo/ echo value\nvar after = 1\n",
		"vim9script\nfilter # pat\nvar after = 1\n",
	} {
		file := (Vim9Parser{}).Parse(source)
		for _, command := range file.Commands {
			if len(command.Modifiers) != 0 {
				t.Fatalf("source %q: unexpected modifiers = %#v", source, command.Modifiers)
			}
		}
		if len(file.Commands) < 2 || file.Commands[len(file.Commands)-1].Canonical != "var" {
			t.Fatalf("source %q: commands = %#v", source, file.Commands)
		}
	}
}

func TestFilterModifierOuterBarAndVim9Recovery(t *testing.T) {
	file := (LegacyParser{}).Parse("filter /foo/ echo one | echo two\n")
	if len(file.Commands) != 2 || file.Commands[0].Canonical != "echo" || file.Commands[1].Canonical != "echo" || len(file.Commands[0].Modifiers) != 1 {
		t.Fatalf("commands = %#v", file.Commands)
	}
	if file.Text(file.Commands[0].Argument) != "one" || file.Text(file.Commands[1].Argument) != "two" {
		t.Fatalf("arguments = %q/%q", file.Text(file.Commands[0].Argument), file.Text(file.Commands[1].Argument))
	}
	if countTokens(file, TokenSeparator) != 1 {
		t.Fatalf("tokens = %#v", file.Tokens)
	}

	vim9 := (Vim9Parser{}).Parse("vim9script\nfilter /unfinished | echo same\nvar after = 1\n")
	if len(vim9.Commands) != 3 || vim9.Commands[1].Canonical != "" || vim9.Commands[2].Canonical != "var" {
		t.Fatalf("Vim9 commands = %#v", vim9.Commands)
	}
	modifier := vim9.Commands[1].Modifiers[0]
	if modifier.Filter == nil || fileText(vim9, modifier.Filter.Delimiter) != "/" || fileText(vim9, modifier.Filter.Pattern) != "unfinished | echo same" || countTokens(vim9, TokenOpaque) == 0 {
		t.Fatalf("Vim9 incomplete modifier = %#v, tokens = %#v", modifier, vim9.Tokens)
	}
	assertFileSpans(t, vim9)
}

func TestFilterModifierRecoveryAndNextLine(t *testing.T) {
	for _, source := range []string{
		"filter /unfinished | echo same-line\necho next\n",
		"filter /foo/\necho next\n",
		"filter\necho next\n",
	} {
		file := (LegacyParser{}).Parse(source)
		if len(file.Commands) != 2 || file.Commands[0].Canonical != "" || file.Commands[1].Canonical != "echo" {
			t.Fatalf("source %q: commands = %#v", source, file.Commands)
		}
		if len(file.Commands[0].Modifiers) != 1 || file.Commands[0].Modifiers[0].Name != "filter" {
			t.Fatalf("source %q: first command = %#v", source, file.Commands[0])
		}
		if strings.Contains(source, "unfinished") {
			modifier := file.Commands[0].Modifiers[0]
			if modifier.Filter == nil || file.Text(modifier.Filter.Delimiter) != "/" || file.Text(modifier.Filter.Pattern) != "unfinished | echo same-line" {
				t.Fatalf("source %q: incomplete modifier = %#v", source, modifier)
			}
			if countTokens(file, TokenOpaque) == 0 {
				t.Fatalf("source %q: missing opaque recovery token", source)
			}
		}
		if file.Commands[1].Span.Start != strings.LastIndex(source, "echo next") {
			t.Fatalf("source %q: next command span = %#v", source, file.Commands[1].Span)
		}
		assertFileSpans(t, file)
	}
}

func TestFilterModifierLogicalContinuationSpans(t *testing.T) {
	source := "filter /foo/\n" + "\\ echo value\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || len(file.Commands[0].Modifiers) != 1 || file.Commands[0].Canonical != "echo" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	modifier := file.Commands[0].Modifiers[0]
	if modifier.Filter == nil || file.Text(modifier.Filter.Pattern) != "foo" || file.Text(modifier.Filter.Delimiter) != "/" || modifier.Span != (Span{Start: 0, End: 6}) {
		t.Fatalf("modifier = %#v, text = %q", modifier, file.Text(modifier.Span))
	}
	if file.Commands[0].Span.Start != 0 || file.Text(file.Commands[0].Argument) != "value" {
		t.Fatalf("command = %#v, argument = %q", file.Commands[0], file.Text(file.Commands[0].Argument))
	}
	assertFileSpans(t, file)
}
