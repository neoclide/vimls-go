package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBodyFingerprintRejectsAnyReviewedBodyChange(t *testing.T) {
	bodies := map[string]string{
		"did_set_chars_option":  "return set_chars_option();",
		"set_chars_option":      "return field_value_err();",
		"get_encoded_char_adv":  "return hexhex2nr();",
		"did_set_statuslineopt": "return statuslineopt_changed();",
		"statuslineopt_changed": "return FAIL;",
		"did_set_winhighlight":  "return update_winhighlight();",
		"update_winhighlight":   "return parse_winhighlight();",
		"parse_winhighlight":    "return e_invalid_argument;",
	}
	for name, body := range bodies {
		fingerprint := bodyFingerprint(body)
		if !bodyMatchesFingerprint(body, fingerprint) {
			t.Fatalf("%s did not match its fingerprint", name)
		}
		if bodyMatchesFingerprint(body+" ", fingerprint) {
			t.Fatalf("%s accepted a changed body", name)
		}
	}
}

func TestValidateOptionsRequiresValuesForValueBasedRules(t *testing.T) {
	base := func(validation optionValidation) option {
		validation.Callback = "did_set_test"
		validation.Sources = []callbackSource{{Source: "src/optionstr.c", Line: 1}}
		return option{
			Name:             "test",
			Type:             "OptionString",
			Flags:            []string{"P_STRING"},
			Variants:         []optionVariant{{Variable: "(char_u *)&p_test", Indirect: "PV_NONE", DidSetCallback: "did_set_test", ExpandCallback: "NULL", ViDefault: "(char_u *)\"\"", VimDefault: "(char_u *)0L"}},
			Validation:       validation,
			AvailableWhen:    "1",
			DefinitionSource: "src/optiondefs.h",
			DefinitionLine:   1,
		}
	}
	for _, kind := range []string{"ValidationExact", "ValidationCommaList", "ValidationFlagList", "ValidationListChars", "ValidationFillChars", "ValidationStatuslineOpt"} {
		t.Run(kind, func(t *testing.T) {
			if err := validateOptions([]option{base(optionValidation{Kind: kind, ErrorCode: "E474"})}); err == nil {
				t.Fatalf("%s accepted empty Values", kind)
			}
		})
	}
	if err := validateOptions([]option{base(optionValidation{Kind: "ValidationWinHighlight", ErrorCode: "E474"})}); err != nil {
		t.Fatalf("ValidationWinHighlight = %v", err)
	}
	if err := validateOptions([]option{base(optionValidation{Kind: "ValidationUnknown", ErrorCode: "E474", Values: []string{"value"}})}); err == nil {
		t.Fatal("unknown validation kind was accepted")
	}
}

func TestParseOptionSourceAndTerms(t *testing.T) {
	source := []byte(`#define PV_BETA OPT_BUF(BV_BETA)
#define PV_GAMMA OPT_BOTH(OPT_WIN(WV_GAMMA))
static struct vimoption options[] = {
 {"alpha", "al", P_BOOL|P_VI_DEF, (char_u *)&p_alpha, PV_NONE, NULL, NULL,
  {(char_u *)FALSE, (char_u *)0L} SCTX_INIT},
 {"beta", NULL, P_NUM|P_VI_DEF, (char_u *)&p_beta, PV_BETA, NULL, NULL,
  {(char_u *)0L, (char_u *)0L} SCTX_INIT},
 {"gamma", "gm", P_STRING|P_VI_DEF,
#ifdef FEATURE
 (char_u *)&p_gamma, PV_GAMMA, did_set_gamma, expand_set_gamma,
#else
 (char_u *)NULL, PV_NONE, NULL, NULL,
#endif
  {(char_u *)"x", (char_u *)0L} SCTX_INIT},
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
	if len(options[2].Variants) != 2 || options[2].AvailableWhen != "defined(FEATURE)" || options[2].Variants[0].Condition != "defined(FEATURE)" || options[2].Variants[0].DidSetCallback != "did_set_gamma" || options[2].Variants[1].Condition != "!(defined(FEATURE))" || !isNullExpression(options[2].Variants[1].Variable) {
		t.Fatalf("gamma variants = %#v", options[2].Variants)
	}
	terms, err := parseTerms(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(terms) != 1 || terms[0].Name != "t_ZZ" || terms[0].ShortName != "" || terms[0].Type != "OptionString" || terms[0].Variants[0].Variable != "(char_u *)&T_ZZ" {
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
	if _, err := expandConditionalText("{\n#if FEATURE\nvalue\n}"); err == nil {
		t.Fatal("expandConditionalText accepted an unterminated conditional")
	}
}

func TestParseOptionAvailabilitySimplifiesPartitionedBranches(t *testing.T) {
	source := []byte(`static struct vimoption options[] = {
 {"delta", "dl", P_STRING|P_VI_DEF,
#ifdef FEATURE_DELTA
  (char_u *)&p_delta, PV_NONE, NULL, NULL,
  {
# if defined(MSWIN)
  (char_u *)"win",
# else
  (char_u *)"other",
# endif
  (char_u *)0L}
#else
  (char_u *)NULL, PV_NONE, NULL, NULL,
  {(char_u *)NULL, (char_u *)0L}
#endif
  SCTX_INIT},
};
`)
	options, err := parseOptionSource(source)
	if err != nil || len(options) != 1 {
		t.Fatalf("options = %#v, err = %v", options, err)
	}
	if options[0].AvailableWhen != "defined(FEATURE_DELTA)" {
		t.Fatalf("AvailableWhen = %q, want %q", options[0].AvailableWhen, "defined(FEATURE_DELTA)")
	}
}

func TestSimplifyConditionsRequiresComplementaryBranches(t *testing.T) {
	conditions := []string{
		"(defined(FEATURE)) && (defined(PLATFORM_ONE))",
		"(defined(FEATURE)) && (defined(PLATFORM_TWO))",
	}
	got := simplifyConditions(conditions)
	if got == "defined(FEATURE)" || !strings.Contains(got, "PLATFORM_ONE") || !strings.Contains(got, "PLATFORM_TWO") {
		t.Fatalf("simplifyConditions(%#v) = %q", conditions, got)
	}
}

func TestExpandConditionalTextPreservesElifAndElseConditions(t *testing.T) {
	forms, err := expandConditionalText("before\n#if FIRST && \\\n    CONTINUED\none\n#elif SECOND\ntwo\n#else\nthree\n#endif\nafter\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []conditionalText{
		{Condition: "FIRST && CONTINUED", Text: "before\n\none\nafter\n\n"},
		{Condition: "(!(FIRST && CONTINUED)) && (SECOND)", Text: "before\ntwo\nafter\n\n"},
		{Condition: "!((FIRST && CONTINUED) || (SECOND))", Text: "before\nthree\nafter\n\n"},
	}
	if len(forms) != len(want) {
		t.Fatalf("forms = %#v", forms)
	}
	for i := range want {
		if forms[i] != want[i] {
			t.Errorf("forms[%d] = %#v, want %#v", i, forms[i], want[i])
		}
	}
}

func TestWriteOptionSetOracleUsesMigratedTypesAndFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "options.vim")
	options := []option{
		{Name: "number", Type: "OptionNumber", Flags: []string{"P_NUM"}, AvailableWhen: "1", DefinitionSource: "src/optiondefs.h", DefinitionLine: 10},
		{Name: "string", Type: "OptionString", Flags: []string{"P_STRING"}, CompletionValues: []string{"one", "two"}, AvailableWhen: "defined(FEATURE)", RequiredFeatures: []string{"feature"}, DefinitionSource: "src/optiondefs.h", DefinitionLine: 20},
		{Name: "nodefault", Type: "OptionString", Flags: []string{"P_STRING", "P_NODEFAULT"}, AvailableWhen: "0", DefinitionSource: "src/optiondefs.h", DefinitionLine: 30},
	}
	if err := writeOptionSetOracle(path, options); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, expected := range []string{
		"'set number&'",
		"'defined(FEATURE)'",
		"['feature']",
		`escape(&string, " \t\\|\"")`,
		"'silent set nodefault?'",
		"s:CheckMigratedCompletion('string', ['one', 'two'], 'src/optiondefs.h:20')",
		"'src/optiondefs.h:30'",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("generated oracle does not contain %q:\n%s", expected, text)
		}
	}
}

func TestParseVimFeatureNames(t *testing.T) {
	features := parseVimFeatures([]byte(`#ifdef FEAT_BEVAL_GUI
    "+balloon_eval",
#else
    "-balloon_eval",
#endif
#if defined(FEAT_AUTOCHDIR)
    "+autochdir",
#else
    "-autochdir",
#endif
#ifdef FEAT_EVAL
    "+eval",
#endif
#ifdef FEAT_EVAL
    "+packages",
#endif
`))
	if features["FEAT_BEVAL_GUI"] != "balloon_eval" || features["FEAT_AUTOCHDIR"] != "autochdir" || features["FEAT_EVAL"] != "" {
		t.Fatalf("features = %#v", features)
	}
}

func TestOptionRequiredFeaturesRequiresCompletePositiveMapping(t *testing.T) {
	features := map[string]string{"FEAT_ONE": "one", "FEAT_TWO": "two"}
	if got := optionRequiredFeatures("defined(FEAT_ONE) && defined(FEAT_TWO)", features); !slices.Equal(got, []string{"one", "two"}) {
		t.Fatalf("complete feature mapping = %#v", got)
	}
	for _, condition := range []string{
		"defined(FEAT_ONE) && defined(MSWIN)",
		"defined(FEAT_ONE) && !defined(FEAT_TWO)",
		"defined(FEAT_ONE) || defined(FEAT_TWO)",
	} {
		if got := optionRequiredFeatures(condition, features); got != nil {
			t.Errorf("optionRequiredFeatures(%q) = %#v, want nil", condition, got)
		}
	}
}

func TestParseFixedOptionCompletionValues(t *testing.T) {
	macros, err := parseCStringMacros([]byte("#define FILE_UNIX \"unix\"\n#define FLAGS \"aB\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	source := []byte(`static char *(formats[]) = {
  FILE_UNIX,
#ifdef FEATURE_DOS
  "dos",
#endif
  NULL};
int
expand_set_format(optexpand_T *args, int *numMatches, char_u ***matches)
{
  return expand_set_opt_string(args, formats, ARRAY_LENGTH(formats) - 1,
      numMatches, matches);
}
int
expand_set_flags(optexpand_T *args, int *numMatches, char_u ***matches)
{
  return expand_set_opt_listflag(args, (char_u *)FLAGS, numMatches, matches);
}
`)
	arrays, err := parseCStringArrays(source, macros)
	if err != nil {
		t.Fatal(err)
	}
	values, err := parseExpandCallbackValues(source, "expand_set_format", arrays, macros)
	if err != nil || !slices.Equal(values, []string{"unix", "dos"}) {
		t.Fatalf("format values = %#v, err = %v", values, err)
	}
	values, err = parseExpandCallbackValues(source, "expand_set_flags", arrays, macros)
	if err != nil || !slices.Equal(values, []string{"a", "B"}) {
		t.Fatalf("flag values = %#v, err = %v", values, err)
	}
}

func TestParseOptionValidationBodies(t *testing.T) {
	arrays := map[string][]string{"values": {"one", "two"}}
	macros := map[string]string{"FLAGS": "aB"}
	tests := []struct {
		name string
		body string
		kind string
		code string
	}{
		{name: "exact guard", body: "\n    if (check_opt_strings(value, values, FALSE) != OK)\n        return e_invalid_argument;\n    side_effect();\n    return NULL;\n", kind: "ValidationExact", code: "E474"},
		{name: "comma list", body: "\n    prepare();\n    return did_set_opt_strings(value, values, TRUE);\n", kind: "ValidationCommaList", code: "E474"},
		{name: "flag list", body: "\n    return did_set_option_listflag(value, (char_u *)FLAGS, errbuf, len);\n", kind: "ValidationFlagList", code: "E539"},
		{name: "runtime alternative", body: "\n    if (check_opt_strings(value, values, FALSE) != OK && !mch_isdir(value))\n        return e_invalid_argument;\n    return NULL;\n", kind: "ValidationNone"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validation, err := parseOptionValidationBody(test.body, arrays, macros, nil)
			if err != nil {
				t.Fatal(err)
			}
			if validation.Kind != test.kind || validation.ErrorCode != test.code {
				t.Fatalf("validation = %#v", validation)
			}
			if test.kind != "ValidationNone" && !slices.Equal(validation.Values, map[string][]string{
				"ValidationExact":     {"one", "two"},
				"ValidationCommaList": {"one", "two"},
				"ValidationFlagList":  {"a", "B"},
			}[test.kind]) {
				t.Fatalf("validation values = %#v", validation.Values)
			}
		})
	}
}

func TestOptionValidationRequiresSameRuleAcrossCallbackDefinitions(t *testing.T) {
	definitions := []callbackDefinition{
		{Source: "src/one.c", Line: 10, Body: "return did_set_opt_strings(value, values, FALSE);"},
		{Source: "src/two.c", Line: 20, Body: "return NULL;"},
	}
	validation, err := parseOptionValidation("did_set_example", definitions, map[string][]string{"values": {"one"}}, nil, nil, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if validation.Kind != "ValidationNone" || len(validation.Sources) != 2 {
		t.Fatalf("validation = %#v", validation)
	}
}

func TestOptionValidationRejectsMismatchVariableOrBool(t *testing.T) {
	arrays := map[string][]string{"values": {"single", "double"}}
	definitions := []callbackDefinition{
		{Source: "src/optionstr.c", Line: 10, Body: "if (check_opt_strings(p_ambw, values, FALSE) != OK) return e_invalid_argument;\nreturn NULL;\n"},
	}
	// OptionBool must never receive string validation
	validation, err := parseOptionValidation("did_set_ambiwidth", definitions, arrays, nil, []string{"p_emoji"}, nil, nil, "OptionBool")
	if err != nil || validation.Kind != "ValidationNone" {
		t.Fatalf("bool option received validation: %#v, err = %v", validation, err)
	}
	// OptionString with mismatched variable must not receive another variable's validation
	validation, err = parseOptionValidation("did_set_ambiwidth", definitions, arrays, nil, []string{"p_other"}, nil, nil, "OptionString")
	if err != nil || validation.Kind != "ValidationNone" {
		t.Fatalf("mismatched variable received validation: %#v, err = %v", validation, err)
	}
	// OptionString with matching variable must receive validation
	validation, err = parseOptionValidation("did_set_ambiwidth", definitions, arrays, nil, []string{"p_ambw"}, nil, nil, "OptionString")
	if err != nil || validation.Kind != "ValidationExact" || !slices.Equal(validation.Values, []string{"single", "double"}) {
		t.Fatalf("matching variable did not receive validation: %#v, err = %v", validation, err)
	}
}

func TestParseNumberValidationBody(t *testing.T) {
	body := `
#define MAX_VALUE 99
if (p_value <= 0)
    errmsg = e_argument_must_be_positive;
else if (p_value > MAX_VALUE)
    errmsg = e_invalid_argument;
if (other < 0)
    errmsg = e_invalid_argument;
`
	validation, err := parseNumberValidationBody(body, []string{"p_value"}, map[string]string{
		"e_argument_must_be_positive": "E487",
		"e_invalid_argument":          "E474",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if validation.Kind != "ValidationNumberRange" || !validation.HasMin || validation.Min != 1 || validation.MinErrorCode != "E487" || !validation.HasMax || validation.Max != 99 || validation.MaxErrorCode != "E474" {
		t.Fatalf("validation = %#v", validation)
	}
}
