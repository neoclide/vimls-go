package vimdata

import "testing"

func TestNeovimCompatLookupsDoNotExpandPublicLists(t *testing.T) {
	if !IsNeovimCompatFunction("nvim_buf_get_lines") {
		t.Fatal("Neovim compatibility function was not recognized")
	}
	if _, ok := LookupFunction("nvim_buf_get_lines"); ok {
		t.Fatal("Neovim compatibility function leaked into normal lookup")
	}
	for _, function := range BuiltinFunctions() {
		if function.Name == "nvim_buf_get_lines" {
			t.Fatalf("Neovim compatibility function leaked into BuiltinFunctions: %#v", function)
		}
	}
	if _, ok := LookupFunction("doesnotexist_nvim_any"); ok {
		t.Fatal("LookupFunction accepted an unknown name")
	}

	if !IsNeovimCompatOption("shada") || !IsNeovimCompatOption("&g:shada") {
		t.Fatal("Neovim compatibility option was not recognized")
	}
	if _, ok := LookupOption("shada"); ok {
		t.Fatal("Neovim compatibility option leaked into normal lookup")
	}
	for _, option := range Options() {
		if option.Name == "shada" {
			t.Fatalf("Neovim compatibility option leaked into Options: %#v", option)
		}
	}
	if _, ok := LookupOption("doesnotexist_neovim_option"); ok {
		t.Fatal("LookupOption accepted an unknown option")
	}

	if !IsNeovimCompatVariable("v:lua") {
		t.Fatal("Neovim compatibility variable was not recognized")
	}
	if _, ok := LookupVariable("v:lua"); ok {
		t.Fatal("Neovim compatibility variable leaked into normal lookup")
	}
	for _, variable := range Variables() {
		if variable.Name == "v:lua" {
			t.Fatalf("Neovim compatibility variable leaked into Variables: %#v", variable)
		}
	}
	if _, ok := LookupVariable("v:doesnotexist_neovim"); ok {
		t.Fatal("LookupVariable accepted an unknown variable")
	}

	command, ok := Lookup("rshada")
	if !ok {
		t.Fatal("Lookup did not recognize Neovim compatibility command")
	}
	if command.Flags&AllowBang == 0 || command.Flags&AllowBar == 0 || command.Flags&FileArgument == 0 || command.Flags&NeedArgument != 0 {
		t.Fatalf("rshada command flags = %b", command.Flags)
	}
	if command, ok := Lookup("wsh"); !ok || command.Name != "wshada" {
		t.Fatalf("wshada abbreviation = %#v, %t", command, ok)
	}
	for _, command := range Commands() {
		if command.Name == "rshada" {
			t.Fatalf("Neovim compatibility command leaked into Commands: %#v", command)
		}
	}
	if _, ok := Lookup("doesnotexist_neovim_cmd"); ok {
		t.Fatal("Lookup accepted an unknown command")
	}
}
