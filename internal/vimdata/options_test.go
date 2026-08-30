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
		if !ok || option.Name != test.canonical || option.ShortName != test.short || option.Type != test.typ || option.Scope != test.scope || option.Documentation == "" || option.DocumentationSource != test.source {
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
	if BuiltinOptionCount() != 562 {
		t.Fatalf("BuiltinOptionCount() = %d, want 562", BuiltinOptionCount())
	}
	seenCanonical := make(map[string]Option, len(builtinOptions))
	seenShort := make(map[string]Option, len(builtinOptions))
	typeCounts := map[OptionType]int{
		OptionBool:   0,
		OptionNumber: 0,
		OptionString: 0,
	}
	for _, option := range builtinOptions {
		if option.Name == "" {
			t.Fatalf("option has empty canonical name: %#v", option)
		}
		if option.Documentation == "" || option.DocumentationSource == "" {
			t.Fatalf("%s documentation/source = %q/%q", option.Name, option.Documentation, option.DocumentationSource)
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
}
