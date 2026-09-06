package vimdata

import "testing"

func TestOptionCompatibilityValues(t *testing.T) {
	for _, test := range []struct {
		name, value string
		vim, nvim   bool
	}{
		{"scl", "auto:1-9", false, true}, {"scl", "auto:1-1", false, false},
		{"scl", "auto:9", false, true}, {"scl", "yes:10", false, false},
		{"fdc", "auto:9", false, true}, {"fdc", "9", true, true}, {"fdc", "12", true, false},
		{"cot", "menu,preselect", false, true}, {"cot", "popuphidden", true, false},
		{"cot", "popuphidden,preselect", false, false},
		{"fcs", "horiz:─", false, true}, {"fcs", "horiz:xx", false, false},
		{"cpo", "_", false, true}, {"cpo", "g", true, false}, {"cpo", "g_", false, false},
		{"jop", "view,clean", false, true},
		{"pb", "single,margin", true, false}, {"pb", "25", false, true},
		{"winborder", "+,-,+,|,+,-,+,|", false, true}, {"winborder", "round", false, false},
		{"mousescroll", "ver:0,hor:6", false, true}, {"mousescroll", "ver:2,ver:3", false, false},
	} {
		compat, ok := LookupOptionCompatibility(test.name)
		if !ok {
			t.Fatalf("missing %s", test.name)
		}
		validate := func(option Option) bool {
			if option.Name == "" {
				return false
			}
			// The shared value validator assumes its caller has decoded numeric
			// syntax. Exact validation suffices for the non-numeric side here.
			if option.Type == OptionNumber {
				for _, c := range test.value {
					if c < '0' || c > '9' {
						return false
					}
				}
			}
			_, invalid := ValidateOptionValue(option.Validation, test.value)
			return !invalid
		}
		if gotVim, gotNvim := validate(compat.Vim), validate(compat.Variant); gotVim != test.vim || gotNvim != test.nvim {
			t.Errorf("%s=%s got Vim=%v Neovim=%v, want %v %v", test.name, test.value, gotVim, gotNvim, test.vim, test.nvim)
		}
	}
}

func TestOptionCompatibilityDoesNotMutatePinnedMetadata(t *testing.T) {
	compat, ok := LookupOptionCompatibility("&l:scl")
	if !ok || compat.Vim.Name != "signcolumn" || compat.Variant.Name != "signcolumn" {
		t.Fatalf("lookup %#v", compat)
	}
	compat.Variant.Validation.Values[0] = "changed"
	next, _ := LookupOptionCompatibility("scl")
	if next.Variant.Validation.Values[0] == "changed" {
		t.Fatal("compatibility lookup shares mutable metadata")
	}
	vim, _ := LookupOption("foldcolumn")
	if vim.Type != OptionNumber || vim.Validation.Kind != ValidationNone {
		t.Fatal("pinned Vim metadata changed")
	}
	if _, ok := LookupOptionCompatibility("unknownoption"); ok {
		t.Fatal("unknown option accepted")
	}
	alias, ok := LookupOptionCompatibility("&g:pb")
	if !ok || alias.Vim.Name != "pumborder" || alias.Variant.Name != "pumblend" {
		t.Fatalf("alias collision %#v", alias)
	}
}
