package syntax

import (
	"strings"
	"testing"
)

func TestCollectedAutocmdBlockBarSeparatorDiagnostic(t *testing.T) {
	file := (LegacyParser{}).Parse("autocmd BufEnter * {\n  throw 'bad' | echo 'after'\n}\nlet after = 1\n")
	var got []Diagnostic
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code == "vim/E1231" {
			got = append(got, diagnostic)
		}
	}
	if len(got) != 1 || file.Text(got[0].Span) != "|" || got[0].Message != "Cannot use a bar to separate commands here: | echo 'after'" {
		t.Fatalf("E1231 diagnostics = %#v", got)
	}
	lineEnd, _ := physicalLineEnd(file.Source, got[0].Span.Start)
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code != "vim/E1231" && diagnostic.Span.Start >= got[0].Span.End && diagnostic.Span.Start < lineEnd {
			t.Fatalf("post-bar diagnostic = %#v", diagnostic)
		}
	}
	if len(file.Commands) == 0 || file.Commands[0].Embedded == nil {
		t.Fatalf("autocmd block body was not retained: %#v", file.Commands)
	}
	for _, command := range file.Commands[0].Embedded.Commands {
		if command.Canonical == "echo" {
			t.Fatalf("post-bar echo was retained: %#v", file.Commands[0].Embedded.Commands)
		}
	}
	if len(file.Commands) < 2 || file.Commands[len(file.Commands)-1].Declaration == nil || file.Text(file.Commands[len(file.Commands)-1].Declaration.Name) != "after" {
		t.Fatalf("following declaration was not retained: %#v", file.Commands)
	}
}

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
	} {
		file := (LegacyParser{}).Parse(source)
		if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Embedded != nil {
			t.Fatalf("source = %q, command = %#v, diagnostics = %#v", source, file.Commands, file.Diagnostics)
		}
	}
	file := (LegacyParser{}).Parse("autocmd BufEnter * | echo outside\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Embedded == nil || len(file.Commands[0].Embedded.Commands) != 1 {
		t.Fatalf("bar body was not parsed: commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[0].Embedded.Commands[0].Canonical != "echo" || file.Text(file.Commands[0].Embedded.Commands[0].Argument) != "outside" {
		t.Fatalf("bar body = %#v", file.Commands[0].Embedded.Commands)
	}
}

func TestAutocmdHeaderSpansAndOperations(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		operation AutocmdOperation
		group     string
		events    []string
		pattern   string
		modifiers []AutocmdModifierKind
	}{
		{name: "query", source: "autocmd BufRead *.vim\n", operation: AutocmdQuery, events: []string{"BufRead"}, pattern: "*.vim"},
		{name: "clear", source: "autocmd! BufRead *.vim\n", operation: AutocmdClear, events: []string{"BufRead"}, pattern: "*.vim"},
		{name: "define", source: "autocmd BufRead,BufWrite {a,b} echo x\n", operation: AutocmdDefine, events: []string{"BufRead", "BufWrite"}, pattern: "{a,b}"},
		{name: "replace", source: "autocmd! BufRead *.vim echo x\n", operation: AutocmdReplace, events: []string{"BufRead"}, pattern: "*.vim"},
		{name: "group-head", source: "autocmd mygroup BufRead *.vim ++once ++nested echo x\n", operation: AutocmdDefine, group: "mygroup", events: []string{"BufRead"}, pattern: "*.vim", modifiers: []AutocmdModifierKind{AutocmdOnce, AutocmdNested}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := (LegacyParser{}).Parse(test.source)
			if len(file.Diagnostics) != 0 || len(file.Commands) != 1 {
				t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
			}
			command := &file.Commands[0]
			if command.Autocmd == nil || command.Autocmd.Operation != test.operation {
				t.Fatalf("autocmd = %#v, operation = %v", command.Autocmd, command.Autocmd.Operation)
			}
			if file.Text(command.Autocmd.Group) != test.group || file.Text(command.Autocmd.Pattern) != test.pattern {
				t.Fatalf("group = %q, pattern = %q", file.Text(command.Autocmd.Group), file.Text(command.Autocmd.Pattern))
			}
			if len(command.Autocmd.Events) != len(test.events) {
				t.Fatalf("events = %#v", command.Autocmd.Events)
			}
			for index, expected := range test.events {
				if file.Text(command.Autocmd.Events[index]) != expected {
					t.Fatalf("event %d = %q", index, file.Text(command.Autocmd.Events[index]))
				}
			}
			if len(command.Autocmd.Modifiers) != len(test.modifiers) {
				t.Fatalf("modifiers = %#v", command.Autocmd.Modifiers)
			}
			for index, expected := range test.modifiers {
				if command.Autocmd.Modifiers[index].Kind != expected {
					t.Fatalf("modifier %d = %#v", index, command.Autocmd.Modifiers[index])
				}
			}
			if command.Autocmd.Head.Start < command.Argument.Start || command.Autocmd.Head.End > command.Argument.End {
				t.Fatalf("head span = %#v, argument = %#v", command.Autocmd.Head, command.Argument)
			}
		})
	}
}

func TestAutocmdUnknownHeadRemainsAmbiguous(t *testing.T) {
	file := (LegacyParser{}).Parse("autocmd future_group FutureEvent *.vim echo x\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	autocmd := file.Commands[0].Autocmd
	if autocmd == nil || autocmd.Group != (Span{}) || file.Text(autocmd.Head) != "future_group" || autocmd.Pattern != (Span{}) || file.Commands[0].Embedded != nil {
		t.Fatalf("ambiguous head = %#v", autocmd)
	}
}

func TestAutocmdBarOwnership(t *testing.T) {
	withBody := (LegacyParser{}).Parse("autocmd BufRead * | echo outside\n")
	if len(withBody.Diagnostics) != 0 || withBody.Commands[0].Embedded == nil || len(withBody.Commands[0].Embedded.Commands) != 1 {
		t.Fatalf("body = %#v, diagnostics = %#v", withBody.Commands[0].Embedded, withBody.Diagnostics)
	}
	withoutSeparator := (LegacyParser{}).Parse("autocmd BufRead *|echo\n")
	if len(withoutSeparator.Diagnostics) != 0 || withoutSeparator.Commands[0].Embedded != nil || withoutSeparator.Text(withoutSeparator.Commands[0].Autocmd.Pattern) != "*|echo" {
		t.Fatalf("pattern = %#v, embedded = %#v, diagnostics = %#v", withoutSeparator.Commands[0].Autocmd, withoutSeparator.Commands[0].Embedded, withoutSeparator.Diagnostics)
	}
	missingPattern := (LegacyParser{}).Parse("autocmd! BufRead | next\n")
	if len(missingPattern.Diagnostics) != 0 || len(missingPattern.Commands) != 2 || missingPattern.Commands[0].Autocmd == nil || missingPattern.Commands[0].Autocmd.Operation != AutocmdClear || missingPattern.Commands[1].Canonical != "next" {
		t.Fatalf("missing-pattern bar = %#v, diagnostics = %#v", missingPattern.Commands, missingPattern.Diagnostics)
	}
	trailingBar := (LegacyParser{}).Parse("autocmd BufRead * |\n")
	if len(trailingBar.Diagnostics) != 0 || trailingBar.Commands[0].Autocmd == nil || trailingBar.Commands[0].Autocmd.Operation != AutocmdDefine || trailingBar.Commands[0].Embedded == nil {
		t.Fatalf("trailing body bar = %#v, diagnostics = %#v", trailingBar.Commands[0], trailingBar.Diagnostics)
	}
	empty := (LegacyParser{}).Parse("autocmd! | echo next\nlet after = 1\n")
	if len(empty.Diagnostics) != 0 || len(empty.Commands) != 3 || empty.Commands[0].Autocmd == nil || empty.Commands[0].Autocmd.Operation != AutocmdClear || empty.Commands[1].Canonical != "echo" || empty.Commands[2].Canonical != "let" {
		t.Fatalf("empty-argument bar = %#v, diagnostics = %#v", empty.Commands, empty.Diagnostics)
	}
}

func TestAutocmdBlocksUseVim9BodyAndRecover(t *testing.T) {
	source := "autocmd BufEnter * {\n  var value = 1\n}\nlet after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	outer := &file.Commands[0]
	if outer.Autocmd == nil || outer.Embedded == nil || len(outer.Embedded.Commands) != 1 || outer.Embedded.Commands[0].Dialect != Vim9 {
		t.Fatalf("outer = %#v, embedded = %#v", outer, outer.Embedded)
	}
	if file.Text(outer.Embedded.Commands[0].Declaration.Name) != "value" || file.Commands[1].Canonical != "let" {
		t.Fatalf("embedded = %#v, following = %#v", outer.Embedded.Commands, file.Commands[1])
	}

	incomplete := (LegacyParser{}).Parse("autocmd BufEnter * {\n  echo value\nlet after = 1\n")
	if len(incomplete.Commands) < 2 || incomplete.Commands[len(incomplete.Commands)-1].Canonical != "let" || incomplete.Commands[0].Autocmd == nil || incomplete.Commands[0].Autocmd.Operation != AutocmdDefine {
		t.Fatalf("incomplete block swallowed following command: %#v", incomplete.Commands)
	}
}

func TestAutocmdNestedBlockKeepsEnclosingDialect(t *testing.T) {
	legacy := (LegacyParser{}).Parse("autocmd BufEnter * autocmd BufLeave * {\n  var nested = 1\n}\nlet after = 1\n")
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 2 || legacy.Commands[0].Embedded == nil || len(legacy.Commands[0].Embedded.Commands) != 1 {
		t.Fatalf("legacy nested block = %#v, diagnostics = %#v", legacy.Commands, legacy.Diagnostics)
	}
	nested := &legacy.Commands[0].Embedded.Commands[0]
	if nested.Dialect != Legacy || nested.Autocmd == nil || nested.Embedded == nil || len(nested.Embedded.Commands) != 1 || nested.Embedded.Commands[0].Dialect != Vim9 || nested.Embedded.Commands[0].Declaration == nil || legacy.Commands[1].Canonical != "let" {
		t.Fatalf("legacy nested owner = %#v, following = %#v", nested, legacy.Commands[1])
	}

	vim9 := (Vim9Parser{}).Parse("vim9script\nautocmd BufEnter * autocmd BufLeave * {\n  var nested = 1\n}\nvar after = 1\n")
	if len(vim9.Diagnostics) != 0 || len(vim9.Commands) != 3 || vim9.Commands[1].Embedded == nil {
		t.Fatalf("vim9 nested block = %#v, diagnostics = %#v", vim9.Commands, vim9.Diagnostics)
	}
	if vim9.Commands[1].Dialect != Vim9 || len(vim9.Commands[1].Embedded.Commands) != 1 || vim9.Commands[1].Embedded.Commands[0].Dialect != Vim9 || vim9.Commands[1].Embedded.Commands[0].Embedded == nil || vim9.Commands[1].Embedded.Commands[0].Embedded.Commands[0].Dialect != Vim9 || vim9.Commands[2].Canonical != "var" {
		t.Fatalf("vim9 nested owner = %#v, following = %#v", vim9.Commands[1].Embedded, vim9.Commands[2])
	}
}

func TestAutocmdNestedBlockHeaderChain(t *testing.T) {
	source := "autocmd BufEnter * autocmd BufLeave * autocmd BufNew * {\n  var nested = 1\n}\nlet after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	outer := &file.Commands[0]
	if outer.Embedded == nil || len(outer.Embedded.Commands) != 1 {
		t.Fatalf("outer embedded = %#v", outer.Embedded)
	}
	middle := &outer.Embedded.Commands[0]
	if middle.Dialect != Legacy || middle.Embedded == nil || len(middle.Embedded.Commands) != 1 {
		t.Fatalf("middle = %#v", middle)
	}
	owner := &middle.Embedded.Commands[0]
	if owner.Dialect != Legacy || owner.Embedded == nil || len(owner.Embedded.Commands) != 1 || owner.Embedded.Commands[0].Dialect != Vim9 || owner.Embedded.Commands[0].Declaration == nil {
		t.Fatalf("owner = %#v", owner)
	}
}

func TestAutocmdNestedCommandBlockHeaderChain(t *testing.T) {
	source := "autocmd BufEnter * command Nested autocmd BufLeave * {\n  var nested = 1\n}\nlet after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	outer := &file.Commands[0]
	if outer.Embedded == nil || len(outer.Embedded.Commands) != 1 {
		t.Fatalf("outer embedded = %#v", outer.Embedded)
	}
	definition := &outer.Embedded.Commands[0]
	if definition.Canonical != "command" || definition.Dialect != Legacy || definition.Embedded == nil || len(definition.Embedded.Commands) != 1 {
		t.Fatalf("definition = %#v", definition)
	}
	owner := &definition.Embedded.Commands[0]
	if owner.Canonical != "autocmd" || owner.Dialect != Legacy || owner.Embedded == nil || len(owner.Embedded.Commands) != 1 || owner.Embedded.Commands[0].Dialect != Vim9 || owner.Embedded.Commands[0].Declaration == nil {
		t.Fatalf("owner = %#v", owner)
	}
}

func TestAutocmdNestedUserCommandOwnsBlock(t *testing.T) {
	source := "autocmd BufEnter * command Nested {\n  var nested = 1\n}\nlet after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	outer := &file.Commands[0]
	if outer.Embedded == nil || len(outer.Embedded.Commands) != 1 {
		t.Fatalf("outer embedded = %#v", outer.Embedded)
	}
	owner := &outer.Embedded.Commands[0]
	if owner.Canonical != "command" || owner.Dialect != Legacy || owner.Embedded == nil || len(owner.Embedded.Commands) != 1 || owner.Embedded.Commands[0].Dialect != Vim9 || owner.Embedded.Commands[0].Declaration == nil {
		t.Fatalf("owner = %#v", owner)
	}
}

func TestVim9AutocmdBlockAutomaticContinuation(t *testing.T) {
	source := "vim9script\nautocmd BufEnter * {\n  var values = [\n    1,\n    2,\n  ]\n}\nvar after = 1\n"
	file := (Vim9Parser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || file.Commands[1].Embedded == nil || len(file.Commands[1].Embedded.Commands) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[1].Embedded.Commands[0].Declaration == nil || file.Text(file.Commands[1].Embedded.Commands[0].Declaration.Name) != "values" || file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "after" {
		t.Fatalf("embedded = %#v, following = %#v", file.Commands[1].Embedded.Commands, file.Commands[2])
	}
}

func TestAutocmdBlockOpeningComments(t *testing.T) {
	tests := []struct {
		name   string
		parse  func(string) *File
		source string
		index  int
	}{
		{name: "legacy-comment", parse: (LegacyParser{}).Parse, source: "autocmd BufEnter * { \" opening comment\n  var value = 1\n}\nlet after = 1\n"},
		{name: "vim9-comment", parse: (Vim9Parser{}).Parse, source: "vim9script\nautocmd BufEnter * { # opening comment\n  var value = 1\n}\nvar after = 1\n", index: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			if len(file.Diagnostics) != 0 || len(file.Commands) != test.index+2 {
				t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
			}
			outer := &file.Commands[test.index]
			if outer.Embedded == nil || len(outer.Embedded.Commands) != 1 || outer.Embedded.Commands[0].Dialect != Vim9 || outer.Embedded.Commands[0].Declaration == nil || file.Text(outer.Embedded.Commands[0].Declaration.Name) != "value" {
				t.Fatalf("outer = %#v, embedded = %#v", outer, outer.Embedded)
			}
		})
	}
}

func TestVim9AutocmdRejectsLegacyNestedModifier(t *testing.T) {
	file := (Vim9Parser{}).Parse("vim9script\nautocmd BufRead * nested echo value\n")
	if !hasDiagnostic(file, "vim/E1078") || file.Commands[1].Autocmd == nil || file.Commands[1].Embedded == nil {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
}

func TestAutocmdModifierBoundariesAndDuplicates(t *testing.T) {
	trailing := (LegacyParser{}).Parse("autocmd BufRead * ++once\n")
	if len(trailing.Diagnostics) != 0 || trailing.Commands[0].Autocmd == nil || len(trailing.Commands[0].Autocmd.Modifiers) != 0 || trailing.Commands[0].Embedded == nil {
		t.Fatalf("trailing modifier = %#v, diagnostics = %#v", trailing.Commands[0], trailing.Diagnostics)
	}
	duplicate := (LegacyParser{}).Parse("autocmd BufRead * ++once ++once ++nested ++nested echo value\n")
	if !hasDiagnostic(duplicate, "vim/E983") || len(duplicate.Diagnostics) != 1 || duplicate.Commands[0].Autocmd == nil || len(duplicate.Commands[0].Autocmd.Modifiers) != 4 {
		t.Fatalf("duplicate modifiers = %#v, diagnostics = %#v", duplicate.Commands[0].Autocmd, duplicate.Diagnostics)
	}
	if duplicate.Diagnostics[0].Message != "Duplicate argument: ++once" || duplicate.Text(duplicate.Diagnostics[0].Span) != "++once" {
		t.Fatalf("duplicate modifier diagnostic = %#v", duplicate.Diagnostics[0])
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
	if outer.Autocmd == nil || file.Text(outer.Autocmd.Events[0]) != "BufNewFile" || file.Text(outer.Autocmd.Pattern) != "*.match" {
		t.Fatalf("autocmd header = %#v", outer.Autocmd)
	}
	for _, span := range append([]Span{outer.Autocmd.Head, outer.Autocmd.Pattern}, outer.Autocmd.Events...) {
		if span.Start < outer.Argument.Start || span.End > outer.Argument.End || file.Text(span) == "" {
			t.Fatalf("header span = %#v, argument = %#v", span, outer.Argument)
		}
	}
}

func TestAutocmdEmbeddedBlockRecoveryAndAbsoluteSpans(t *testing.T) {
	source := "autocmd BufEnter * if condition | echo value\nedit next.txt\n"
	file := (LegacyParser{}).Parse(source)
	if !hasDiagnostic(file, "vim/E171") || len(file.Commands) != 2 || file.Commands[1].Canonical != "edit" {
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
