package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSourceAndTerms(t *testing.T) {
	source := []byte(`#define PV_BETA OPT_BUF(BV_BETA)
#define PV_GAMMA OPT_BOTH(OPT_WIN(WV_GAMMA))
static struct vimoption options[] = {
 {"alpha", "al", P_BOOL|P_VI_DEF, (char_u *)&p_alpha, PV_NONE, NULL},
 {"beta", NULL, P_NUM|P_VI_DEF, (char_u *)&p_beta, PV_BETA, NULL},
#ifdef FEATURE
 {"gamma", "gm", P_STRING|P_VI_DEF, (char_u *)&p_gamma, PV_GAMMA, NULL},
#else
 (char_u *)NULL, PV_NONE, NULL,
#endif
};
#define p_term(sss, vvv) {sss, NULL, P_STRING, (char_u *)&vvv, PV_NONE}
p_term("t_ZZ", T_ZZ)
`)
	options, err := parseSource(source)
	if err != nil || len(options) != 3 {
		t.Fatalf("options = %#v, err = %v", options, err)
	}
	if options[0].Scope != "OptionGlobal" || options[1].Name != "beta" || options[1].Scope != "OptionBuffer" || options[2].Type != "OptionString" || options[2].Scope != "OptionGlobalLocal" {
		t.Fatalf("options = %#v", options)
	}
	terms, err := parseTerms(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(terms) != 1 || terms[0].Name != "t_ZZ" || terms[0].ShortName != "" || terms[0].Type != "OptionString" {
		t.Fatalf("terms = %#v", terms)
	}
	if err := validateOptions(append(options, terms...)); err != nil {
		t.Fatal(err)
	}
}

func TestParseSourceRejectsMalformedInput(t *testing.T) {
	if _, err := parseSource([]byte("static struct vimoption options[] = {};")); err == nil {
		t.Fatal("parseSource accepted an empty options table")
	}
}

func TestPinnedRevisionAndGeneratedTable(t *testing.T) {
	root := "/Users/chemzqm/lib/vim"
	resolved, err := exec.Command("git", "-C", root, "rev-list", "-n", "1", vimTag).Output()
	if err != nil || strings.TrimSpace(string(resolved)) != vimCommit {
		t.Skip("pinned Vim checkout is not available")
	}
	source, err := readRevisionFile(root, "src/optiondefs.h")
	if err != nil {
		t.Fatal(err)
	}
	options, err := parseSource(source)
	if err != nil {
		t.Fatal(err)
	}
	terms, err := parseTerms(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 469 || len(terms) != 93 {
		t.Fatalf("ordinary/terminal options = %d/%d, want 469/93", len(options), len(terms))
	}
	options = append(options, terms...)
	if err := validateOptions(options); err != nil {
		t.Fatal(err)
	}
	typeCounts := map[string]int{}
	byName := make(map[string]option, len(options))
	for _, option := range options {
		typeCounts[option.Type]++
		byName[option.Name] = option
	}
	if typeCounts["OptionBool"] != 157 || typeCounts["OptionNumber"] != 89 || typeCounts["OptionString"] != 316 {
		t.Fatalf("option type counts = %#v", typeCounts)
	}
	for name, want := range map[string]string{
		"backup": "OptionGlobal", "tabstop": "OptionBuffer", "wrap": "OptionWindow", "scrolloff": "OptionGlobalLocal",
	} {
		if got := byName[name].Scope; got != want {
			t.Errorf("%s scope = %q, want %q", name, got, want)
		}
	}
	if err := addDocumentation(root, options); err != nil {
		t.Fatal(err)
	}
	for _, option := range options {
		if option.Documentation == "" || option.DocumentationSource == "" {
			t.Fatalf("%s documentation = %q from %q", option.Name, option.Documentation, option.DocumentationSource)
		}
	}
	generated := filepath.Join(t.TempDir(), "options_generated.go")
	if err := writeOutput(generated, options); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(generated)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "internal", "vimdata", "options_generated.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("options_generated.go is stale; run tools/genoptions")
	}
}
