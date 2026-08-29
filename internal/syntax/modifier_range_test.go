package syntax

import "testing"

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
