package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/vimdata"
)

func TestSourceValues(t *testing.T) {
	got, err := sourceValues()
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string]string, len(got))
	for _, line := range got {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" || value == "" || strings.ContainsAny(line, " \t\r\n") {
			t.Fatalf("unsafe environment line %q", line)
		}
		if _, duplicate := values[key]; duplicate {
			t.Fatalf("duplicate environment key %q", key)
		}
		values[key] = value
	}
	if values["VIMLS_VIM_TAG"] != vimdata.VimSourceTag || values["VIMLS_VIM_COMMIT"] != vimdata.VimSourceCommit || values["VIMLS_NEOVIM_COMMIT"] != vimdata.NeovimSourceCommit {
		t.Fatalf("source provenance = %#v", values)
	}
	version, ok := vimdata.ParseVimVersion(values["VIMLS_VIM_VERSION"])
	if !ok {
		t.Fatalf("invalid Vim version %q", values["VIMLS_VIM_VERSION"])
	}
	fromTag, _ := vimdata.ParseVimVersion(vimdata.VimSourceTag)
	next, ok := vimdata.ParseVimVersion(values["VIMLS_VIM_NEXT_PATCH"])
	if !ok || version != fromTag || next.Major != version.Major || next.Minor != version.Minor || next.Patch != version.Patch+1 {
		t.Fatalf("versions = current:%#v next:%#v source:%#v", version, next, fromTag)
	}
	if values["VIMLS_VIM_VVERSION"] != strconv.Itoa(version.Major*100+version.Minor) || len(values) != 6 {
		t.Fatalf("source values = %#v", values)
	}
}
