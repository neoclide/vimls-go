package main

import (
	"testing"
)

func TestParseOptionSourceAndTerms(t *testing.T) {
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
	options, err := parseOptionSource(source)
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
	if _, err := parseOptionSource([]byte("static struct vimoption options[] = {};")); err == nil {
		t.Fatal("parseOptionSource accepted an empty options table")
	}
}
