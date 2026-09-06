package vimdata

import (
	"strings"
	"testing"
)

func TestCompletionMetadataTables(t *testing.T) {
	if ModifierVimTag != "v9.2.1015" || ModifierVimCommit != "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae" ||
		AutocmdEventVimTag != "v9.2.1015" || AutocmdEventVimCommit != "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae" ||
		CompletionValueVimTag != "v9.2.1015" || CompletionValueVimCommit != "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae" {
		t.Fatal("completion metadata provenance changed")
	}

	features := HasFeatures()
	if len(features) != 224 {
		t.Fatalf("has() features = %d, want 224", len(features))
	}
	for index, feature := range features {
		if feature.Name == "" || feature.Documentation == "" {
			t.Fatalf("empty has() feature metadata at %d: %#v", index, feature)
		}
		if index > 0 && strings.ToLower(features[index-1].Name) >= strings.ToLower(feature.Name) {
			t.Fatalf("has() features are not uniquely sorted at %q", feature.Name)
		}
	}
	wantFeatures := map[string]bool{"all_builtin_terms": false, "clipboard_working": false, "gui_macvim": false, "nvim": false, "patch-9.2.1015": false, "vim9script": false, "X11": false, ":tearoff": false}
	for _, feature := range features {
		if _, ok := wantFeatures[feature.Name]; ok {
			wantFeatures[feature.Name] = true
		}
	}
	for name, found := range wantFeatures {
		if !found {
			t.Fatalf("missing has() feature %q", name)
		}
	}
	features[0].Name = "changed"
	if HasFeatures()[0].Name == "changed" {
		t.Fatal("HasFeatures exposed its table")
	}

	specials := ExpandSpecials()
	if len(specials) != 16 || specials[0].Name != "%" || specials[1].Name != "#" || specials[len(specials)-1].Name != "<client>" {
		t.Fatalf("expand() special metadata = %#v", specials)
	}
	for _, special := range specials {
		if special.Documentation == "" {
			t.Fatalf("missing expand() documentation for %q", special.Name)
		}
	}
	specials[0].Name = "changed"
	if ExpandSpecials()[0].Name == "changed" {
		t.Fatal("ExpandSpecials exposed its table")
	}
	options := Options()
	variables := Variables()
	for _, table := range [][]string{{options[0].Name}, {variables[0].Name}} {
		if table[0][0] == 0 {
			t.Fatal("empty completion metadata name")
		}
	}
	if len(options) != len(builtinOptions) || len(variables) != len(builtinVariables) {
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

	attributes := UserCommandAttributes()
	if len(attributes) != 11 {
		t.Fatalf("user command attributes count = %d, want 11", len(attributes))
	}
	for _, attr := range attributes {
		if attr.Name == "" || attr.Detail == "" || attr.Documentation == "" {
			t.Fatalf("incomplete attribute metadata: %#v", attr)
		}
		lookup, ok := LookupUserCommandAttribute(attr.Name)
		if !ok || lookup.Name != attr.Name {
			t.Fatalf("lookup failed for attribute %q: %#v, %t", attr.Name, lookup, ok)
		}
		lookupMinus, ok := LookupUserCommandAttribute("-" + attr.Name)
		if !ok || lookupMinus.Name != attr.Name {
			t.Fatalf("lookup with prefix failed for attribute %q: %#v, %t", attr.Name, lookupMinus, ok)
		}
	}

	for _, valueList := range [][]CompletionValue{
		UserCommandNargsValues(),
		UserCommandAddrValues(),
		UserCommandCompleteoptValues(),
		UserCommandCompleteValues(),
	} {
		if len(valueList) == 0 {
			t.Fatal("empty user command attribute value list")
		}
		for _, value := range valueList {
			if value.Name == "" || value.Documentation == "" {
				t.Fatalf("incomplete attribute value metadata: %#v", value)
			}
		}
	}

	val, detail, ok := LookupUserCommandAttributeValue("nargs", "+")
	if !ok || val.Name != "+" || detail != "command argument count" || val.Documentation == "" {
		t.Fatalf("LookupUserCommandAttributeValue nargs + = %#v, %q, %t", val, detail, ok)
	}
	val, detail, ok = LookupUserCommandAttributeValue("-complete", "custom")
	if !ok || val.Name != "custom" || detail != "command completion type" || val.Documentation == "" {
		t.Fatalf("LookupUserCommandAttributeValue complete custom = %#v, %q, %t", val, detail, ok)
	}
	if _, _, ok := LookupUserCommandAttributeValue("nargs", "invalid"); ok {
		t.Fatal("LookupUserCommandAttributeValue succeeded for invalid value")
	}
}
