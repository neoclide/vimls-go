package syntax

import "testing"

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
