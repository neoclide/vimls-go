package vimdata

import (
	"strings"
	"testing"
)

func TestCompletionMetadataTables(t *testing.T) {
	if ModifierVimTag != "v9.2.1015" || ModifierVimCommit != "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae" ||
		AutocmdEventVimTag != "v9.2.1015" || AutocmdEventVimCommit != "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae" {
		t.Fatal("completion metadata provenance changed")
	}
	options := Options()
	variables := Variables()
	for _, table := range [][]string{{options[0].Name}, {variables[0].Name}} {
		if table[0][0] == 0 {
			t.Fatal("empty completion metadata name")
		}
	}
	if len(options) != BuiltinOptionCount() || len(variables) != BuiltinVariableCount() {
		t.Fatal("completion metadata lost entries")
	}
	for i := 1; i < len(options); i++ {
		if options[i-1].Name >= options[i].Name {
			t.Fatal("options are not sorted")
		}
	}
	for i := 1; i < len(variables); i++ {
		if variables[i-1].Name >= variables[i].Name {
			t.Fatal("variables are not sorted")
		}
	}
	options[0].Name = "changed"
	if Options()[0].Name == "changed" {
		t.Fatal("Options exposed its table")
	}
	variables[0].Name = "changed"
	if Variables()[0].Name == "changed" {
		t.Fatal("Variables exposed its table")
	}

	modifiers := Modifiers()
	if len(modifiers) != 30 {
		t.Fatalf("modifiers = %d, want 30", len(modifiers))
	}
	wantMembers := map[string]int{"abstract": 3, "export": 6, "public": 3, "static": 4}
	for _, modifier := range modifiers {
		if want, ok := wantMembers[modifier.Name]; ok && (!modifier.Vim9Member || modifier.MinLen != want) {
			t.Fatalf("modifier %#v", modifier)
		}
	}

	events := AutocmdEvents()
	if len(events) != 127 {
		t.Fatalf("events = %d, want 127", len(events))
	}
	wantAliases := map[string]string{"BufCreate": "BufAdd", "BufRead": "BufReadPost", "BufWrite": "BufWritePost", "FileEncoding": "EncodingChanged"}
	for i, event := range events {
		if i > 0 && strings.ToLower(events[i-1].Name) >= strings.ToLower(event.Name) {
			t.Fatal("events are not event_tab sorted")
		}
		if want, ok := wantAliases[event.Name]; ok && event.AliasOf != want {
			t.Fatalf("%s alias = %q", event.Name, event.AliasOf)
		}
		if event.AliasOf != "" {
			if _, ok := wantAliases[event.Name]; !ok {
				t.Fatalf("unexpected alias %#v", event)
			}
		}
	}
}
