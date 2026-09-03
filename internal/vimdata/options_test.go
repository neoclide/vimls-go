package vimdata

import "testing"

func TestLookupOptionMetadata(t *testing.T) {
	if OptionVimTag != "v9.2.1015" || OptionVimCommit != "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae" {
		t.Fatalf("option provenance = %s/%s", OptionVimTag, OptionVimCommit)
	}
	tests := []struct {
		name, canonical, short, source string
		typ                            OptionType
		scope                          OptionScope
	}{
		{name: "aleph", canonical: "aleph", short: "al", source: "options.txt", typ: OptionNumber, scope: OptionGlobal},
		{name: "ambw", canonical: "ambiwidth", short: "ambw", source: "options.txt", typ: OptionString, scope: OptionGlobal},
		{name: "&ts", canonical: "tabstop", short: "ts", source: "options.txt", typ: OptionNumber, scope: OptionBuffer},
		{name: "&l:wrap", canonical: "wrap", source: "options.txt", typ: OptionBool, scope: OptionWindow},
		{name: "&t_TI", canonical: "t_TI", source: "term.txt", typ: OptionString, scope: OptionGlobal},
	}
	for _, test := range tests {
		option, ok := LookupOption(test.name)
		if !ok || option.Name != test.canonical || option.ShortName != test.short || option.Type != test.typ || option.Scope != test.scope || len(option.Flags) == 0 || len(option.Variants) == 0 || option.DefinitionSource != "src/optiondefs.h" || option.DefinitionLine <= 0 || option.Documentation == "" || option.DocumentationSource != test.source {
			t.Errorf("LookupOption(%q) = %#v, %v", test.name, option, ok)
		}
	}
	for _, name := range []string{"", "&", "&g:", "nosuchoption"} {
		if option, ok := LookupOption(name); ok {
			t.Errorf("LookupOption(%q) = %#v, true", name, option)
		}
	}
	for _, name := range []string{"ts", "&g:ts", "nu"} {
		if _, ok := LookupOption(name); !ok {
			t.Errorf("LookupOption(%q) did not accept Vim's documented abbreviation", name)
		}
	}
	for _, name := range []string{"tabs", "Tabstop", "NU"} {
		if option, ok := LookupOption(name); ok {
			t.Errorf("LookupOption(%q) = %#v, true; arbitrary prefixes and case folding are not valid", name, option)
		}
	}
	for _, name := range []string{"t_TI", "&t_future", "&g:t_XX", "<t_k1>"} {
		if !IsTerminalOptionName(name) {
			t.Errorf("IsTerminalOptionName(%q) = false", name)
		}
	}
	if IsTerminalOptionName("term") {
		t.Fatal("IsTerminalOptionName(\"term\") = true")
	}
	if len(builtinOptions) != 562 {
		t.Fatalf("len(builtinOptions) = %d, want 562", len(builtinOptions))
	}
	seenCanonical := make(map[string]Option, len(builtinOptions))
	seenShort := make(map[string]Option, len(builtinOptions))
	typeCounts := map[OptionType]int{
		OptionBool:   0,
		OptionNumber: 0,
		OptionString: 0,
	}
	completionCount := 0
	callbackCount := 0
	validationCount := 0
	for _, option := range builtinOptions {
		if option.Name == "" {
			t.Fatalf("option has empty canonical name: %#v", option)
		}
		if option.Documentation == "" || option.DocumentationSource == "" {
			t.Fatalf("%s documentation/source = %q/%q", option.Name, option.Documentation, option.DocumentationSource)
		}
		if len(option.Flags) == 0 || len(option.Variants) == 0 || option.AvailableWhen == "" || option.DefinitionSource != "src/optiondefs.h" || option.DefinitionLine <= 0 {
			t.Fatalf("%s has incomplete migrated definition: %#v", option.Name, option)
		}
		for _, variant := range option.Variants {
			if variant.Variable == "" || variant.Indirect == "" || variant.DidSetCallback == "" || variant.ExpandCallback == "" || variant.ViDefault == "" || variant.VimDefault == "" {
				t.Fatalf("%s has incomplete variant: %#v", option.Name, variant)
			}
		}
		if _, exists := seenCanonical[option.Name]; exists {
			t.Fatalf("duplicate option name %q", option.Name)
		}
		seenCanonical[option.Name] = option
		if _, exists := seenShort[option.ShortName]; exists {
			t.Fatalf("duplicate option short name %q", option.ShortName)
		}
		gotCanonical, ok := LookupOption(option.Name)
		if !ok || gotCanonical.Name != option.Name || gotCanonical.ShortName != option.ShortName {
			t.Fatalf("LookupOption(%q) = %#v, %v", option.Name, gotCanonical, ok)
		}
		typeCounts[option.Type]++
		if len(option.CompletionValues) > 0 {
			completionCount++
		}
		if option.Validation.Callback != "" {
			callbackCount++
			if len(option.Validation.Sources) == 0 {
				t.Fatalf("%s callback has no source provenance: %#v", option.Name, option.Validation)
			}
			for _, source := range option.Validation.Sources {
				if source.Source == "" || source.Line <= 0 {
					t.Fatalf("%s callback has invalid source provenance: %#v", option.Name, option.Validation)
				}
			}
		}
		if option.Validation.Kind != ValidationNone {
			validationCount++
			if option.Validation.Kind == ValidationNumberRange {
				if !option.Validation.HasMin && !option.Validation.HasMax {
					t.Fatalf("%s has empty number validation: %#v", option.Name, option.Validation)
				}
			} else {
				switch option.Validation.Kind {
				case ValidationExact, ValidationCommaList, ValidationFlagList, ValidationListChars, ValidationFillChars, ValidationStatuslineOpt:
					if len(option.Validation.Values) == 0 || option.Validation.ErrorCode == "" {
						t.Fatalf("%s has incomplete validation: %#v", option.Name, option.Validation)
					}
				case ValidationWinHighlight:
					if option.Validation.ErrorCode == "" {
						t.Fatalf("%s has incomplete validation: %#v", option.Name, option.Validation)
					}
				default:
					t.Fatalf("%s has unknown validation kind: %#v", option.Name, option.Validation)
				}
			}
		}
		if option.ShortName == "" {
			continue
		}
		seenShort[option.ShortName] = option
		gotShort, ok := LookupOption(option.ShortName)
		if !ok || gotShort.Name != option.Name || gotShort.ShortName != option.ShortName {
			t.Fatalf("LookupOption(%q) = %#v, %v", option.ShortName, gotShort, ok)
		}
	}
	if typeCounts[OptionBool] != 157 || typeCounts[OptionNumber] != 89 || typeCounts[OptionString] != 316 {
		t.Fatalf("option type counts = %#v, want bool=157 number=89 string=316", typeCounts)
	}
	if completionCount != 66 {
		t.Fatalf("options with fixed completion values = %d, want 66", completionCount)
	}
	if callbackCount != 334 || validationCount != 48 {
		t.Fatalf("callback/validation counts = %d/%d, want 334/48", callbackCount, validationCount)
	}
}

func TestOptionMetadataIsDeepCopied(t *testing.T) {
	option, ok := LookupOption("aleph")
	if !ok || len(option.Flags) == 0 || len(option.Variants) < 2 {
		t.Fatalf("LookupOption(aleph) = %#v, %v", option, ok)
	}
	option.Flags[0] = "changed"
	option.Variants[0].Variable = "changed"
	again, _ := LookupOption("aleph")
	if again.Flags[0] == "changed" || again.Variants[0].Variable == "changed" {
		t.Fatalf("LookupOption returned shared metadata: %#v", again)
	}
	completionOption, _ := LookupOption("ambiwidth")
	completionOption.CompletionValues[0] = "changed"
	completionOption, _ = LookupOption("ambiwidth")
	if completionOption.CompletionValues[0] == "changed" {
		t.Fatalf("LookupOption returned shared completion values: %#v", completionOption)
	}
	featureOption, _ := LookupOption("ballooneval")
	featureOption.RequiredFeatures[0] = "changed"
	featureOption.Validation.Sources[0].Source = "changed"
	featureOption, _ = LookupOption("ballooneval")
	if featureOption.RequiredFeatures[0] == "changed" || featureOption.Validation.Sources[0].Source == "changed" {
		t.Fatalf("LookupOption returned shared required features: %#v", featureOption)
	}
	options := Options()
	options[0].Flags[0] = "changed"
	options[0].Variants[0].Variable = "changed"
	again, _ = LookupOption(options[0].Name)
	if again.Flags[0] == "changed" || again.Variants[0].Variable == "changed" {
		t.Fatalf("Options returned shared metadata: %#v", again)
	}
}

func TestOptionValuesArePinnedAndCopied(t *testing.T) {
	values := OptionValues("ff")
	if len(values) != 3 || values[0] != "unix" || values[2] != "mac" {
		t.Fatalf("OptionValues(ff) = %#v", values)
	}
	values[0] = "changed"
	if got := OptionValues("fileformat")[0]; got != "unix" {
		t.Fatalf("OptionValues returned shared storage: %q", got)
	}
	for _, name := range []string{"ignorecase", "encoding", "nosuchoption"} {
		if values := OptionValues(name); len(values) != 0 {
			t.Errorf("OptionValues(%q) = %#v", name, values)
		}
	}
	if values := OptionValues("cpoptions"); len(values) < 50 || values[0] != "a" || values[len(values)-1] != "~" {
		t.Fatalf("OptionValues(cpoptions) = %#v", values)
	}
}

func TestValidateOptionValue(t *testing.T) {
	tests := []struct {
		option string
		value  string
		code   string
		span   string
	}{
		{option: "bufhidden", value: "hide"},
		{option: "bufhidden", value: "", code: ""},
		{option: "bufhidden", value: "bogus", code: "E474", span: "bogus"},
		{option: "belloff", value: "all,error"},
		{option: "belloff", value: "all,bogus", code: "E474", span: "all,bogus"},
		{option: "cpoptions", value: "aA"},
		{option: "cpoptions", value: "a@", code: "E539", span: "@"},
		{option: "cpoptions", value: "a★", code: "E539", span: "★"},
		{option: "maxsearchcount", value: "1"},
		{option: "maxsearchcount", value: "9999"},
		{option: "maxsearchcount", value: "0", code: "E487", span: "0"},
		{option: "maxsearchcount", value: "10000", code: "E474", span: "10000"},
		{option: "browsedir", value: "not-a-static-enum"},
		{option: "emoji", value: "single"},
		{option: "listchars", value: "tab:>-,leadtab:.-,eol:$"},
		{option: "listchars", value: "bogus:$", code: "E474", span: "bogus:$"},
		{option: "listchars", value: "eol:$$", code: "E1511", span: "eol:$$"},
		{option: "listchars", value: "leadtab:.-", code: "E1572", span: "leadtab:.-"},
		{option: "listchars", value: "eol:\\x24"},
		{option: "listchars", value: "eol:\\U00000024"},
		{option: "fillchars", value: "stl: ,vert:|"},
		{option: "fillchars", value: "stl:xx", code: "E1511", span: "stl:xx"},
		{option: "fillchars", value: "stl:\\u002d"},
		{option: "statuslineopt", value: "fixedheight,maxheight:2"},
		{option: "statuslineopt", value: "maxheight:0", code: "E474", span: "maxheight:0"},
		{option: "winhighlight", value: "Normal:Comment,LineNr:Identifier"},
		{option: "winhighlight", value: "Normal", code: "E474", span: "Normal"},
	}
	for _, test := range tests {
		option, ok := LookupOption(test.option)
		if !ok {
			t.Fatalf("LookupOption(%q) failed", test.option)
		}
		failure, invalid := ValidateOptionValue(option.Validation, test.value)
		if invalid != (test.code != "") || failure.Code != test.code {
			t.Errorf("%s=%q: failure=%#v invalid=%v, want code %q", test.option, test.value, failure, invalid, test.code)
			continue
		}
		if invalid && test.value[failure.Start:failure.End] != test.span {
			t.Errorf("%s=%q: failure span %d:%d = %q, want %q", test.option, test.value, failure.Start, failure.End, test.value[failure.Start:failure.End], test.span)
		}
	}
}
