package syntax

import "testing"

func TestVim9cmdMissingCommandDiagnostic(t *testing.T) {
	tests := []struct {
		name, source, span, following string
		comment, separator            bool
	}{
		{"Legacy bare", "vim9cmd\nlet after = 1\n", "vim9cmd", "let", false, false},
		{"abbreviation before bar", "vim9c | echo 1\n", "vim9c", "echo", false, true},
		{"Vim9 comment", "vim9script\nvim9cmd # comment\nvar after = 1\n", "vim9cmd", "var", true, false},
		{"Legacy comment", "vim9cmd \" comment\nlet after = 1\n", "vim9cmd", "let", true, false},
		{"preceding modifier", "vim9script\nsilent vim9cm\nvar after = 1\n", "vim9cm", "var", false, false},
		{"Legacy-root def", "def Func()\n  vim9cmd\n  var after = 1\nenddef\n", "vim9cmd", "var", false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			foundEmpty := false
			foundFollowing := false
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1164" {
					got = append(got, diagnostic)
				}
			}
			for _, command := range file.Commands {
				foundEmpty = foundEmpty || command.Kind == CommandEmpty && len(command.Modifiers) > 0 && command.Modifiers[len(command.Modifiers)-1].Name == "vim9cmd"
				foundFollowing = foundFollowing || command.Canonical == test.following
			}
			if len(file.Diagnostics) != 1 || len(got) != 1 || got[0].Message != "vim9cmd must be followed by a command" || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want E1164 on %q", file.Diagnostics, test.span)
			}
			if !foundEmpty || !foundFollowing || (countTokens(file, TokenComment) > 0) != test.comment || (countTokens(file, TokenSeparator) > 0) != test.separator {
				t.Fatalf("recovery commands = %#v, tokens = %#v", file.Commands, file.Tokens)
			}
		})
	}

	for _, source := range []string{
		"vim9cmd echo 1\n",
		"vim9script\nvim9cmd echo 1\n",
		"vim9cmd leftabove\nlet after = 1\n",
		"vim9cmd legacy echo 'ok'\n",
		"vim9cmd # Legacy text\n",
		"vim9script\nvim9cmd \"Vim9 string\"\n",
		"vim9script\nleftabove\n",
		"vim9script\nlegacy\n",
	} {
		file := Parse(source)
		for _, diagnostic := range file.Diagnostics {
			if diagnostic.Code == "vim/E1164" {
				t.Fatalf("source %q unexpectedly received E1164: %#v", source, file.Diagnostics)
			}
		}
	}
}

func TestLegacyMissingCommandDiagnostic(t *testing.T) {
	tests := []struct {
		name, source, span, following string
		comment, separator            bool
	}{
		{"Legacy bare", "legacy\nlet after = 1\n", "legacy", "let", false, false},
		{"abbreviation before bar", "leg | echo 1\nlet after = 1\n", "leg", "echo", false, true},
		{"Vim9 comment", "vim9script\nlegacy # comment\nvar after = 1\n", "legacy", "var", true, false},
		{"Legacy comment", "legacy \" comment\nlet after = 1\n", "legacy", "let", true, false},
		{"preceding modifier", "vim9script\nsilent legacy\nvar after = 1\n", "legacy", "var", false, false},
		{"vim9cmd legacy", "vim9cmd legacy\nlet after = 1\n", "legacy", "let", false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			foundEmpty := false
			foundFollowing := false
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1234" {
					got = append(got, diagnostic)
				}
			}
			for _, command := range file.Commands {
				foundEmpty = foundEmpty || command.Kind == CommandEmpty && len(command.Modifiers) > 0 && command.Modifiers[len(command.Modifiers)-1].Name == "legacy"
				foundFollowing = foundFollowing || command.Canonical == test.following
			}
			if len(file.Diagnostics) != 1 || len(got) != 1 || got[0].Message != "legacy must be followed by a command" || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want E1234 on %q", file.Diagnostics, test.span)
			}
			if !foundEmpty || !foundFollowing || (countTokens(file, TokenComment) > 0) != test.comment || (countTokens(file, TokenSeparator) > 0) != test.separator {
				t.Fatalf("recovery commands = %#v, tokens = %#v", file.Commands, file.Tokens)
			}
		})
	}

	for _, test := range []struct {
		name, source, want string
	}{
		{"following command", "legacy echo 1\n", ""},
		{"range followed by command", "legacy 3delete\n", ""},
		{"Vim9 double quote is not a comment", "vim9script\nlegacy \" text\n", ""},
		{"Legacy hash is not a comment", "legacy # text\n", ""},
		{"legacy then vim9cmd", "legacy vim9cmd\n", "vim/E1164"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			count := 0
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1234" {
					t.Fatalf("guard unexpectedly received E1234: %#v", file.Diagnostics)
				}
				if diagnostic.Code == test.want {
					count++
				}
			}
			if test.want != "" && count != 1 {
				t.Fatalf("diagnostics = %#v, want one %s", file.Diagnostics, test.want)
			}
		})
	}
}

func TestVim9CommandModifierRequiresCommand(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		parse      func(string) *File
		diagnostic bool
	}{
		{
			name:       "def physical line end",
			source:     "def Func()\nleftabove\nvar after = 1\nenddef\n",
			parse:      func(source string) *File { return Parse(source) },
			diagnostic: true,
		},
		{
			name:       "vim9 comment",
			source:     "vim9script\nleftabove # comment\nvar after = 1\n",
			parse:      func(source string) *File { return Parse(source) },
			diagnostic: true,
		},
		{
			name:       "legacy modifier in vim9",
			source:     "vim9script\ndef Func()\nlegacy leftabove\nvar after = 1\nenddef\n",
			parse:      func(source string) *File { return Parse(source) },
			diagnostic: true,
		},
		{
			name:   "legacy vim9cmd modifier",
			source: "vim9cmd leftabove\nlet after = 1\n",
			parse:  func(source string) *File { return (LegacyParser{}).Parse(source) },
		},
		{
			name:   "legacy modifier",
			source: "leftabove\nlet after = 1\n",
			parse:  func(source string) *File { return (LegacyParser{}).Parse(source) },
		},
		{
			name:   "vim9 command after bar",
			source: "vim9script\nleftabove | echo 1\nvar after = 1\n",
			parse:  func(source string) *File { return Parse(source) },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			if test.diagnostic {
				if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1082" {
					t.Fatalf("diagnostics = %#v, commands = %#v", file.Diagnostics, file.Commands)
				}
				modifierCommand := -1
				for index := range file.Commands {
					if len(file.Commands[index].Modifiers) > 0 {
						modifierCommand = index
						break
					}
				}
				if modifierCommand < 0 {
					t.Fatalf("commands = %#v", file.Commands)
				}
				modifier := file.Commands[modifierCommand].Modifiers[len(file.Commands[modifierCommand].Modifiers)-1]
				if file.Diagnostics[0].Span != modifier.Span || file.Text(file.Diagnostics[0].Span) != "leftabove" {
					t.Fatalf("diagnostic span = %#v, modifier = %#v", file.Diagnostics[0].Span, modifier)
				}
				if modifierCommand+1 >= len(file.Commands) {
					t.Fatalf("recovery commands = %#v", file.Commands)
				}
				next := file.Commands[modifierCommand+1].Canonical
				if next != "var" && next != "enddef" {
					t.Fatalf("recovery command = %#v", file.Commands[modifierCommand+1])
				}
			} else if len(file.Diagnostics) != 0 {
				t.Fatalf("diagnostics = %#v, commands = %#v", file.Diagnostics, file.Commands)
			}
		})
	}
}

func TestCommandModifierMayPrecedeRange(t *testing.T) {
	file := (LegacyParser{}).Parse("silent! %foldclose!\nsilent! :/needle/delete\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[0].Canonical != "foldclose" || file.Text(file.Commands[0].Range) != "%" || file.Commands[0].Bang.Start == file.Commands[0].Bang.End {
		t.Fatalf("foldclose = %#v", file.Commands[0])
	}
	if file.Commands[1].Canonical != "delete" || file.Text(file.Commands[1].Range) != "/needle/" {
		t.Fatalf("delete = %#v", file.Commands[1])
	}
	for index := range file.Commands {
		if len(file.Commands[index].Modifiers) != 1 || file.Commands[index].Modifiers[0].Name != "silent" {
			t.Fatalf("command %d modifiers = %#v", index, file.Commands[index].Modifiers)
		}
	}
}
