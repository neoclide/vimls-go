package syntax

import (
	"strings"
	"testing"
)

func TestUserCommandLogicalSpans(t *testing.T) {
	for _, prefix := range []string{"", "vim9script\n"} {
		for _, separator := range []string{" ", "\n  \\ "} {
			source := prefix + "command -nargs=1 -buffer" + separator + "Demo echo <args>\n"
			file := Parse(source)
			if len(file.Diagnostics) != 0 {
				t.Fatalf("%q: %v", source, file.Diagnostics)
			}
			command := &file.Commands[len(file.Commands)-1]
			definition := command.UserCommand
			if definition == nil {
				t.Fatalf("%q: missing definition", source)
			}
			check := func(span Span, value string) {
				t.Helper()
				start := strings.Index(source, value)
				if span != (Span{Start: start, End: start + len(value)}) {
					t.Errorf("%q: %q span=%v", source, value, span)
				}
			}
			check(definition.Name, "Demo")
			check(definition.Body, "echo <args>")
			if len(definition.Attributes) != 2 {
				t.Fatalf("attributes=%v", definition.Attributes)
			}
			attr := definition.Attributes[0]
			check(attr.Span, "-nargs=1")
			check(attr.Name, "nargs")
			check(attr.Equal, "=")
			check(attr.Value, "1")
			check(definition.Attributes[1].Span, "-buffer")
			name, span, buffer, ok := DefinedUserCommand(file, command)
			if !ok || name != "Demo" || span != definition.Name || !buffer {
				t.Fatalf("definition lookup: %q %v %v %v", name, span, buffer, ok)
			}
		}
	}
}

func TestIncompleteVim9UserCommandLogicalSpans(t *testing.T) {
	source := "vim9script\ncommand -complete="
	file := Parse(source)
	command := file.Commands[1]
	if command.UserCommand == nil || len(command.UserCommand.Attributes) != 1 {
		t.Fatalf("definition=%#v", command.UserCommand)
	}
	attribute := command.UserCommand.Attributes[0]
	if attribute.Value != (Span{Start: len(source), End: len(source)}) {
		t.Fatalf("value=%v", attribute.Value)
	}
}
