package vimdata

import "testing"

var (
	benchmarkLookupCommand Command
	benchmarkLookupOK      bool
)

func TestLookupUsesPinnedVimCommandOrder(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "r", want: "read"},
		{input: "fu", want: "function"},
		{input: "vim9s", want: "vim9script"},
		{input: "def", want: "def"},
		{input: "!", want: "!"},
		{input: "uniq", want: "uniq"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			command, ok := Lookup(test.input)
			if !ok || command.Name != test.want {
				t.Fatalf("Lookup(%q) = %#v, %v", test.input, command, ok)
			}
		})
	}
	if _, ok := Lookup(""); ok {
		t.Fatal("empty command resolved")
	}
	if _, ok := Lookup("not-a-command"); ok {
		t.Fatal("unknown command resolved")
	}
}

func TestCommandsReturnsOrderedCopy(t *testing.T) {
	got := Commands()
	if len(got) != len(commands) {
		t.Fatalf("Commands() length = %d, want %d", len(got), len(commands))
	}
	for index, command := range commands {
		if got[index] != command {
			t.Fatalf("Commands()[%d] = %#v, want %#v", index, got[index], command)
		}
	}

	got[0] = Command{Name: "changed"}
	if next := Commands(); next[0] != commands[0] {
		t.Fatalf("Commands() exposed package table: first command = %#v, want %#v", next[0], commands[0])
	}
	if command, ok := Lookup(commands[0].Name); !ok || command != commands[0] {
		t.Fatalf("Lookup(%q) = %#v, %v after modifying enumeration", commands[0].Name, command, ok)
	}
}

func TestLookupMatchesFullOrderedTableForEveryPrefix(t *testing.T) {
	for _, command := range commands {
		for length := 1; length <= len(command.Name); length++ {
			assertLookupMatchesLinear(t, command.Name[:length])
		}
		assertLookupMatchesLinear(t, command.Name+"~")
	}
	for first := 0; first < 256; first++ {
		assertLookupMatchesLinear(t, string([]byte{byte(first), '~'}))
	}
}

func assertLookupMatchesLinear(t *testing.T, input string) {
	t.Helper()
	want, wantOK := linearLookup(input)
	got, gotOK := Lookup(input)
	if gotOK != wantOK || got != want {
		t.Fatalf("Lookup(%q) = %#v, %t; want %#v, %t", input, got, gotOK, want, wantOK)
	}
}

func linearLookup(name string) (Command, bool) {
	for _, command := range commands {
		if len(command.Name) >= len(name) && command.Name[:len(name)] == name {
			return command, true
		}
	}
	return Command{}, false
}

func BenchmarkLookupCommands(b *testing.B) {
	inputs := []string{
		"append", "au", "call", "def", "echo", "function", "global", "let",
		"setlocal", "s", "syntax", "vim9cmd", "windo", "Next", "++", "FutureCommand",
	}
	b.ReportAllocs()
	b.ResetTimer()
	var command Command
	var ok bool
	for range b.N {
		for _, input := range inputs {
			command, ok = Lookup(input)
		}
	}
	benchmarkLookupCommand = command
	benchmarkLookupOK = ok
}
