package vimdata

import (
	"testing"
)

func TestLookupMappingItem(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"<silent>", "<silent>"},
		{"silent", "<silent>"},
		{"<SILENT>", "<silent>"},
		{"<buffer>", "<buffer>"},
		{"buffer", "<buffer>"},
		{"<nowait>", "<nowait>"},
		{"<special>", "<special>"},
		{"<script>", "<script>"},
		{"<unique>", "<unique>"},
		{"<expr>", "<expr>"},
		{"<Plug>", "<Plug>"},
		{"<plug>", "<Plug>"},
		{"Plug", "<Plug>"},
		{"<SID>", "<SID>"},
		{"<sid>", "<SID>"},
		{"<ScriptCmd>", "<ScriptCmd>"},
		{"<Cmd>", "<Cmd>"},
		{"<Leader>", "<Leader>"},
		{"<LocalLeader>", "<LocalLeader>"},
	} {
		item, ok := LookupMappingItem(tc.input)
		if !ok || item.Name != tc.want {
			t.Fatalf("LookupMappingItem(%q) = %#v, %v, want %q", tc.input, item, ok, tc.want)
		}
		if item.Documentation == "" {
			t.Fatalf("LookupMappingItem(%q) missing documentation: %q", tc.input, item.Documentation)
		}
	}
	if _, ok := LookupMappingItem("unknown"); ok {
		t.Fatal("LookupMappingItem(unknown) should fail")
	}
}
