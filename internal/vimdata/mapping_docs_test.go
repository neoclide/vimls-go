package vimdata

import (
	"strings"
	"testing"
)

func TestMappingCommandDocumentation(t *testing.T) {
	for _, tc := range []struct {
		name, usage, mode string
	}{
		{"map", ":map {lhs} {rhs}", "Normal, Visual, Select and Operator-pending"},
		{"nmap", ":nmap {lhs} {rhs}", "Normal"},
		{"vmap", ":vmap {lhs} {rhs}", "Visual and Select"},
		{"xmap", ":xmap {lhs} {rhs}", "Visual"},
		{"smap", ":smap {lhs} {rhs}", "Select"},
		{"omap", ":omap {lhs} {rhs}", "Operator-pending"},
		{"imap", ":imap {lhs} {rhs}", "Insert"},
		{"lmap", ":lmap {lhs} {rhs}", "Insert, Command-line and Lang-Arg"},
		{"cmap", ":cmap {lhs} {rhs}", "Command-line"},
		{"tmap", ":tmap {lhs} {rhs}", "Terminal-Job"},
	} {
		doc, ok := LookupMappingCommandDocumentation(tc.name, false)
		if !ok || !strings.Contains(doc, tc.usage) || !strings.Contains(doc, tc.mode) {
			t.Fatalf("%s: ok=%v doc=%q", tc.name, ok, doc)
		}
		if !strings.Contains(doc, "nested and recursive") || !strings.Contains(doc, "Trailing spaces") {
			t.Fatalf("%s missing shared prose: %q", tc.name, doc)
		}
		if !strings.Contains(doc, "With no arguments") || !strings.Contains(doc, "only `{lhs}`") {
			t.Fatalf("%s missing listing forms: %q", tc.name, doc)
		}
	}
	doc, ok := LookupMappingCommandDocumentation("map", true)
	if !ok || !strings.Contains(doc, ":map! {lhs} {rhs}") || !strings.Contains(doc, "Insert and Command-line") {
		t.Fatalf("map!: ok=%v doc=%q", ok, doc)
	}
	for _, name := range []string{"nmap", "vmap", "xmap", "smap", "omap", "imap", "lmap", "cmap", "tmap"} {
		if _, ok := LookupMappingCommandDocumentation(name, true); ok {
			t.Fatalf("%s! unexpectedly documented", name)
		}
	}
	if _, ok := LookupMappingCommandDocumentation("unknown", false); ok {
		t.Fatal("unknown command unexpectedly documented")
	}
	for _, name := range []string{"MAP"} {
		if _, ok := LookupMappingCommandDocumentation(name, false); ok {
			t.Fatalf("%s unexpectedly documented", name)
		}
	}
}

func TestMappingCommandDocumentationInventory(t *testing.T) {
	for _, name := range []string{"noremap", "nnoremap", "vnoremap", "xnoremap", "snoremap", "onoremap", "inoremap", "lnoremap", "cnoremap", "tnoremap", "unmap", "nunmap", "vunmap", "xunmap", "sunmap", "ounmap", "iunmap", "lunmap", "cunmap", "tunmap", "mapclear", "nmapclear", "vmapclear", "xmapclear", "smapclear", "omapclear", "imapclear", "lmapclear", "cmapclear", "tmapclear", "abbreviate", "iabbrev", "cabbrev", "noreabbrev", "inoreabbrev", "cnoreabbrev", "unabbreviate", "iunabbrev", "cunabbrev", "abclear", "iabclear", "cabclear", "command", "delcommand", "comclear"} {
		if doc, ok := LookupMappingCommandDocumentation(name, false); !ok || doc == "" {
			t.Fatalf("missing %s", name)
		}
	}
	for _, name := range []string{"noremap", "unmap", "mapclear"} {
		if doc, ok := LookupMappingCommandDocumentation(name, true); !ok || doc == "" {
			t.Fatalf("missing %s!", name)
		}
	}
}

func TestMappingCommandDocumentationDetails(t *testing.T) {
	if len(mappingCommandDocumentation) != 60 {
		t.Fatalf("inventory = %d, want 55 commands and 5 bang forms", len(mappingCommandDocumentation))
	}
	for _, tc := range []struct {
		name      string
		bang      bool
		fragments []string
	}{
		{"nunmap", false, []string{"{lhs}", "**Normal**", "`{rhs}`", "Trailing spaces", "<buffer>"}},
		{"vnoremap", false, []string{"Visual and Select", "Disallow mapping", "i_CTRL-]", "c_CTRL-]", "<Plug>", "With no arguments"}},
		{"noremap", true, []string{":noremap!", "**Insert and Command-line**", "Disallow mapping"}},
		{"mapclear", true, []string{"**ALL**", "Insert and Command-line", "<buffer>", "Mac and DOS"}},
		{"iabbrev", false, []string{"**Insert**", "replace it", "contain spaces", "With only `{lhs}`", "<expr>"}},
		{"cnoreabbrev", false, []string{"**Command-line**", "Do not remap", "With no arguments", "<buffer>"}},
		{"unabbreviate", false, []string{"If none is found", "`{rhs}`", "CTRL-V", "<buffer>"}},
		{"command", true, []string{":command!", "unless `!`", "even without `!`", "-nargs", "-buffer"}},
		{"delcommand", false, []string{":delcommand -buffer", "not allowed while", "current buffer"}},
	} {
		doc, ok := LookupMappingCommandDocumentation(tc.name, tc.bang)
		if !ok {
			t.Fatalf("missing %s bang=%v", tc.name, tc.bang)
		}
		for _, fragment := range tc.fragments {
			if !strings.Contains(doc, fragment) {
				t.Errorf("%s bang=%v missing %q", tc.name, tc.bang, fragment)
			}
		}
	}
	for _, name := range []string{"map", "noremap", "unmap", "mapclear"} {
		doc, _ := LookupMappingCommandDocumentation(name, true)
		if strings.Contains(doc, "Visual") || strings.Contains(doc, "Operator-pending") {
			t.Errorf("%s! has non-bang modes: %s", name, doc)
		}
	}
}
