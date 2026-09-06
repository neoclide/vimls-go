package vimdata

import "testing"

func TestMacVimCompatOptions(t *testing.T) {
	for _, option := range macvimCompatOptions {
		names := []string{option.Name, "&g:" + option.Name}
		if option.ShortName != "" {
			names = append(names, "&l:"+option.ShortName)
		}
		for _, name := range names {
			compat, ok := LookupOptionCompatibility(name)
			if !ok || compat.Feature != "gui_macvim" || compat.Variant.Name != option.Name || compat.Vim.Name != "" {
				t.Fatalf("%s: %#v", name, compat)
			}
		}
	}
	if IsMacVimCompatOption("toolbariconsize") || IsMacVimCompatOption("unknownoption") || IsMacVimCompatOption("") {
		t.Fatal("unexpected MacVim option")
	}
	if _, ok := LookupOption("macmeta"); ok {
		t.Fatal("overlay leaked into public metadata")
	}
}
