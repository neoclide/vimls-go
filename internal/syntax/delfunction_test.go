package syntax

import "testing"

func TestDelfunctionTargets(t *testing.T) {
	tests := []struct {
		name   string
		source string
		parse  func(string) *File
		index  int
		kind   ExpressionKind
		text   string
	}{
		{name: "legacy function", source: "delfunction Foo\n", parse: func(source string) *File { return (LegacyParser{}).Parse(source) }, index: 0, kind: ExpressionIdentifier, text: "Foo"},
		{name: "legacy script local", source: "delfunction s:Foo\n", parse: func(source string) *File { return (LegacyParser{}).Parse(source) }, index: 0, kind: ExpressionIdentifier, text: "s:Foo"},
		{name: "legacy dictionary member", source: "delfunction d.key\n", parse: func(source string) *File { return (LegacyParser{}).Parse(source) }, index: 0, kind: ExpressionMember, text: "d.key"},
		{name: "legacy dictionary index", source: "delfunction d['key']\n", parse: func(source string) *File { return (LegacyParser{}).Parse(source) }, index: 0, kind: ExpressionIndex, text: "d['key']"},
		{name: "vim9 global with comment", source: "vim9script\ndelfunction g:LegacyFunc # comment\n", parse: func(source string) *File { return Parse(source) }, index: 1, kind: ExpressionIdentifier, text: "g:LegacyFunc"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			if len(file.Diagnostics) != 0 {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			if test.index >= len(file.Commands) {
				t.Fatalf("commands = %#v", file.Commands)
			}
			command := file.Commands[test.index]
			if command.Canonical != "delfunction" || len(command.Targets) != 1 {
				t.Fatalf("command = %#v", command)
			}
			target := command.Targets[0]
			if target == nil || target.Kind != test.kind || file.Text(target.Span) != test.text {
				t.Fatalf("target = %#v, text = %q", target, file.Text(target.Span))
			}
			if test.name == "vim9 global with comment" && countTokens(file, TokenComment) != 1 {
				t.Fatalf("tokens = %#v", file.Tokens)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestDelfunctionBangAndEmptyArgumentRecovery(t *testing.T) {
	file := (LegacyParser{}).Parse("delfunction! Foo\ndelfunction\nlet g:after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vimls/missing-argument" || len(file.Commands) != 3 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[0].Bang != (Span{Start: 11, End: 12}) || len(file.Commands[0].Targets) != 1 {
		t.Fatalf("bang command = %#v", file.Commands[0])
	}
	if len(file.Commands[1].Targets) != 0 || file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "g:after" {
		t.Fatalf("empty argument recovery = %#v", file.Commands)
	}
	assertFileSpans(t, file)
}

func TestDelfunctionTrailingInputRecoversNextLine(t *testing.T) {
	file := (LegacyParser{}).Parse("delfunction d.key extra\nlet g:after = 1\n")
	if len(file.Commands) != 2 || file.Commands[1].Declaration == nil || file.Text(file.Commands[1].Declaration.Name) != "g:after" {
		t.Fatalf("commands = %#v", file.Commands)
	}
	if len(file.Commands[0].Targets) != 1 || file.Text(file.Commands[0].Targets[0].Span) != "d.key" {
		t.Fatalf("target = %#v", file.Commands[0].Targets)
	}
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E488" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	assertFileSpans(t, file)
}

func TestDelfunctionVim9AdjacentCommentMatchesOfficialE488(t *testing.T) {
	source := "vim9script\nfunc g:DeleteMeB()\nendfunc\ndelfunction g:DeleteMeB# comment\n"
	file := Parse(source)
	command := file.Commands[len(file.Commands)-1]
	if command.Canonical != "delfunction" || len(command.Targets) != 1 {
		t.Fatalf("command = %#v", command)
	}
	// Vim's function-name scanner accepts '#' as an autoload name byte, so
	// the following space-delimited "comment" is the E488 tail.
	if text := file.Text(command.Targets[0].Span); text != "g:DeleteMeB#" {
		t.Fatalf("target text = %q, target = %#v", text, command.Targets[0])
	}
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E488" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	assertFileSpans(t, file)
}

func TestDelfunctionIncompleteTargetPreservesPartialAST(t *testing.T) {
	file := (LegacyParser{}).Parse("delfunction d[\nlet g:after = 1\n")
	if len(file.Commands) != 2 || file.Commands[1].Declaration == nil || file.Text(file.Commands[1].Declaration.Name) != "g:after" {
		t.Fatalf("commands = %#v", file.Commands)
	}
	if len(file.Commands[0].Targets) != 1 || file.Commands[0].Targets[0] == nil || file.Commands[0].Targets[0].Kind != ExpressionIndex {
		t.Fatalf("target = %#v", file.Commands[0].Targets)
	}
	if len(file.Diagnostics) != 2 || file.Diagnostics[0].Code != "vimls/missing-expression" || file.Diagnostics[1].Code != "vimls/missing-delimiter" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	assertFileSpans(t, file)
}
