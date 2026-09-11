package vimdata

import (
	"strings"
	"testing"
)

func TestAutocmdDocumentationInventory(t *testing.T) {
	entries := AutocmdEventDocumentations()
	if len(entries) != 156 {
		t.Fatalf("got %d events", len(entries))
	}
	seen := map[string]bool{}
	vimCount, nvimCount := 0, 0
	for _, e := range entries {
		name := strings.ToLower(e.Name)
		if seen[name] || e.Documentation == "" || e.Tag == "" || e.Line < 1 || e.Source == "" || len(e.Revision) != 40 {
			t.Fatalf("invalid metadata: %+v", e)
		}
		seen[name] = true
		if e.Editor == "Vim" {
			vimCount++
		} else if e.Editor == "Neovim" {
			nvimCount++
		} else {
			t.Fatal(e.Editor)
		}
		if found, ok := LookupAutocmdEventDocumentation(name); !ok || found != e {
			t.Fatalf("case-insensitive lookup failed for %s", e.Name)
		}
	}
	if vimCount != 127 || nvimCount != 29 {
		t.Fatalf("source counts: %d Vim, %d Neovim", vimCount, nvimCount)
	}
	for _, event := range AutocmdEvents() {
		doc, ok := LookupAutocmdEventDocumentation(event.Name)
		if !ok || doc.Editor != "Vim" {
			t.Fatalf("Vim precedence missing for %s", event.Name)
		}
	}
	entries[0].Name = "changed"
	if AutocmdEventDocumentations()[0].Name == "changed" {
		t.Fatal("caller mutated shared inventory")
	}
	if _, ok := LookupAutocmdEventDocumentation("NotAnEvent"); ok {
		t.Fatal("unknown event found")
	}
}

func TestAutocmdDocumentationSourcesAndContent(t *testing.T) {
	for _, tc := range []struct {
		name, editor, path, contains string
		line                         int
	}{
		{"FileType", "Vim", "runtime/doc/autocmd.txt", "pattern is matched against the filetype", 914},
		{"LspAttach", "Neovim", "runtime/doc/lsp.txt", "client/registerCapability", 735},
		{"DiagnosticChanged", "Neovim", "runtime/doc/diagnostic.txt", "vim.api.nvim_create_autocmd", 423},
		{"PackChanged", "Neovim", "runtime/doc/pack.txt", "PackChanged", 384},
		{"PackChangedPre", "Neovim", "runtime/doc/pack.txt", "These events can be used to execute plugin hooks", 383},
	} {
		doc, ok := LookupAutocmdEventDocumentation(tc.name)
		if !ok || doc.Editor != tc.editor || doc.Source != tc.path || doc.Line != tc.line || !strings.Contains(doc.Documentation, tc.contains) {
			t.Fatalf("unexpected %s: %+v", tc.name, doc)
		}
	}
	filetype, _ := LookupAutocmdEventDocumentation("FileType")
	if strings.Contains(filetype.Documentation, "FileWriteCmd") {
		t.Fatal("next event leaked into FileType")
	}
	last, _ := LookupAutocmdEventDocumentation("WinScrolled")
	if strings.Contains(last.Documentation, "Defining autocommands") {
		t.Fatal("following section leaked into last event")
	}
	alias, _ := LookupAutocmdEventDocumentation("BufRead")
	if alias.AliasOf != "BufReadPost" {
		t.Fatalf("wrong canonical event: %+v", alias)
	}
}
