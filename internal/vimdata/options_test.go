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
}
