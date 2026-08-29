package syntax

import "testing"

func TestDirectFinishMakesRemainingSourceOpaque(t *testing.T) {
	source := "let before = 1\nfinish | echo 'dead'\nHELP TEXT *tag*\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[1].Canonical != "finish" || file.Text(file.OpaqueTail) != "| echo 'dead'\nHELP TEXT *tag*\n" {
		t.Fatalf("finish = %#v, opaque tail = %q", file.Commands[1], file.Text(file.OpaqueTail))
	}
	if countTokens(file, TokenOpaque) != 1 {
		t.Fatalf("tokens = %#v", file.Tokens)
	}
}

func TestConditionalFinishDoesNotHideFollowingCommands(t *testing.T) {
	source := "if !has('vim9script')\n  finish\nendif\nvim9script\nvar value = 1\n"
	file := Parse(source)
	if file.Dialect != Vim9 || len(file.Diagnostics) != 0 || len(file.Commands) != 5 {
		t.Fatalf("file = %#v", file)
	}
	if file.OpaqueTail.Start != 0 || file.OpaqueTail.End != 0 || countTokens(file, TokenOpaque) != 0 {
		t.Fatalf("opaque tail = %#v, tokens = %#v", file.OpaqueTail, file.Tokens)
	}
}

func TestNeovimFinishGuardTriggersVim9(t *testing.T) {
	source := "if has('nvim')\n  finish\nendif\nvim9script\nvar value = 1\n"
	file := Parse(source)
	if file.Dialect != Vim9 || len(file.Diagnostics) != 0 || len(file.Commands) != 5 || file.Commands[4].Dialect != Vim9 {
		t.Fatalf("file = %#v", file)
	}
}

func TestInvalidFinishDoesNotHideFollowingCommands(t *testing.T) {
	for _, source := range []string{
		"finish!\necho 'still parsed'\n",
		"finish extra\necho 'still parsed'\n",
	} {
		file := (LegacyParser{}).Parse(source)
		if len(file.Commands) != 2 || file.Commands[1].Canonical != "echo" || file.OpaqueTail.Start != 0 || file.OpaqueTail.End != 0 {
			t.Fatalf("source = %q, file = %#v", source, file)
		}
	}
}
