package syntax

import (
	"strings"
	"testing"
)

func TestLoadKeymapConsumesPayloadToEOF(t *testing.T) {
	source := "echo before\nloadkeymap\n" +
		"\" comment with | and Ex-looking text\n" +
		"\n" +
		"  a\tA\tASCII A\n" +
		"b <char-0x62> trailing comment\n" +
		"if this is keymap payload\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	command := &file.Commands[1]
	if command.Keymap == nil || len(command.Keymap.Entries) != 3 {
		t.Fatalf("loadkeymap = %#v", command)
	}
	bodyStart := len("echo before\nloadkeymap\n")
	if command.Keymap.Body != (Span{Start: bodyStart, End: len(source)}) || file.Text(command.Keymap.Body) != source[bodyStart:] {
		t.Fatalf("body = %#v, text = %q", command.Keymap.Body, file.Text(command.Keymap.Body))
	}
	entries := command.Keymap.Entries
	if file.Text(entries[0].From) != "a" || file.Text(entries[0].To) != "A" || file.Text(entries[1].From) != "b" || file.Text(entries[1].To) != "<char-0x62>" {
		t.Fatalf("entries = %#v", entries)
	}
	for _, entry := range entries {
		if entry.From.Start < command.Keymap.Body.Start || entry.To.End > command.Keymap.Body.End || file.Text(entry.From) == "" || file.Text(entry.To) == "" {
			t.Fatalf("entry span = %#v", entry)
		}
	}
	for _, command := range file.Commands {
		if file.Text(command.Span) == "if this is keymap payload" {
			t.Fatalf("keymap payload was parsed as Ex command: %#v", file.Commands)
		}
	}
	if countTokens(file, TokenOpaque) != 4 {
		t.Fatalf("keymap payload tokens = %#v", file.Tokens)
	}
	assertFileSpans(t, file)
}

func TestLoadKeymapCommentsBlankLinesAndMissingColumnRecovery(t *testing.T) {
	source := "loadkeymap\r\n" +
		"\" comment\r\n" +
		"\r\n" +
		"a\r\n" +
		"b B\r\n" +
		"c\t\r\n" +
		"d D\r\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Commands) != 1 || file.Commands[0].Keymap == nil || len(file.Commands[0].Keymap.Entries) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if len(file.Diagnostics) != 2 || file.Diagnostics[0].Code != "vim/E791" || file.Diagnostics[1].Code != "vim/E791" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	entries := file.Commands[0].Keymap.Entries
	if file.Text(entries[0].From) != "b" || file.Text(entries[0].To) != "B" || file.Text(entries[1].From) != "d" || file.Text(entries[1].To) != "D" {
		t.Fatalf("entries = %#v", entries)
	}
	for _, diagnostic := range file.Diagnostics {
		if file.Text(diagnostic.Span) != "a" && file.Text(diagnostic.Span) != "c" {
			t.Fatalf("diagnostic span = %#v, text = %q", diagnostic, file.Text(diagnostic.Span))
		}
	}
}

func TestLoadKeymapWithoutTrailingNewline(t *testing.T) {
	source := "loadkeymap\na A"
	file := (LegacyParser{}).Parse(source)
	if len(file.Commands) != 1 || file.Commands[0].Keymap == nil || len(file.Commands[0].Keymap.Entries) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if got := file.Text(file.Commands[0].Keymap.Body); got != "a A" || file.Commands[0].Keymap.Body.Start != len("loadkeymap\n") {
		t.Fatalf("body = %#v, text = %q", file.Commands[0].Keymap.Body, got)
	}
}

func TestInvalidLoadKeymapHeaderRecoversOnNextLine(t *testing.T) {
	file := (LegacyParser{}).Parse("loadkeymap extra\nlet g:after = 1\n")
	if len(file.Commands) != 2 || !hasDiagnostic(file, "vim/E488") {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[0].Keymap != nil || file.Commands[1].Canonical != "let" || file.Commands[1].Declaration == nil {
		t.Fatalf("commands = %#v", file.Commands)
	}
	assertFileSpans(t, file)
}

func TestLoadKeymapLengthUsesFieldBytesOnly(t *testing.T) {
	acceptedFrom := "a"
	acceptedTo := strings.Repeat("x", 198)
	droppedFrom := "b"
	droppedTo := strings.Repeat("y", 199)
	source := "loadkeymap\n  " + acceptedFrom + "          " + acceptedTo + "\n" + droppedFrom + " " + droppedTo + "\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Keymap == nil || len(file.Commands[0].Keymap.Entries) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	entry := file.Commands[0].Keymap.Entries[0]
	if file.Text(entry.From) != acceptedFrom || file.Text(entry.To) != acceptedTo {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestLoadKeymapHashLineIsDataInVim9(t *testing.T) {
	file := Parse("vim9script\nloadkeymap\n# comment\na A\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Keymap == nil || len(file.Commands[1].Keymap.Entries) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	entry := file.Commands[1].Keymap.Entries[0]
	if file.Text(entry.From) != "#" || file.Text(entry.To) != "comment" {
		t.Fatalf("hash entry = %#v", entry)
	}
}

func TestCommandBlockClosesDeferredLoadKeymapBody(t *testing.T) {
	file := Parse("vim9script\ncommand LoadKeys {\n  loadkeymap\na A\n}\nvar after = 1\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 || len(file.Blocks) != 1 || file.Blocks[0].End != 3 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	keymap := file.Commands[2].Keymap
	if keymap == nil || len(keymap.Entries) != 1 || file.Text(keymap.Body) != "a A\n}" || file.Text(keymap.Entries[0].From) != "a" || file.Text(keymap.Entries[0].To) != "A" {
		t.Fatalf("keymap = %#v, body = %q", keymap, file.Text(keymap.Body))
	}
	if file.Commands[3].Kind != CommandBlockEnd || file.Commands[4].Declaration == nil {
		t.Fatalf("following commands = %#v", file.Commands[3:])
	}
	assertFileSpans(t, file)
}

func TestLoadKeymapInsideFunctionDoesNotConsumeDefinitionLines(t *testing.T) {
	file := (LegacyParser{}).Parse("function! F()\n  loadkeymap\n  let value = 1\nendfunction\nlet g:after = 1\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 || len(file.Blocks) != 1 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	if file.Commands[1].Keymap != nil || file.Commands[2].Canonical != "let" || file.Commands[2].Declaration == nil || file.Commands[3].Canonical != "endfunction" || file.Commands[4].Declaration == nil {
		t.Fatalf("commands = %#v", file.Commands)
	}
	assertFileSpans(t, file)
}
