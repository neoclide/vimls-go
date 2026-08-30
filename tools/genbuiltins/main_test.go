package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSourceFixture(t *testing.T) {
	source := []byte(`/* Lists of functions that check the argument types of a builtin function. */
static argcheck_T arg2_string[] = {arg_string, arg_string};
static argcheck_T arg2_instanceof[] = {
    arg_object, varargs_class, NULL
};
static garray_T *current_type_gap = NULL;
static const funcentry_T global_functions[] =
{
	{"zeta", 1, VARGS, FEARG_1|FE_X, arg2_instanceof,
            ret_any, f_zeta},
    {"alpha", 0, 2, FEARG_2, arg2_string,
            ret_list_string, f_alpha},
    {"guarded", 1, 1, 0, NULL,
            ret_number_bool,
#ifdef FEATURE
            f_guarded
#else
            NULL
#endif
            },
};`)
	functions, err := parseSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(functions) != 3 || functions[0].Name != "alpha" || functions[1].Name != "guarded" || functions[2].Name != "zeta" {
		t.Fatalf("functions = %#v", functions)
	}
	if functions[2].MaxArgs != -1 || functions[2].MethodArgument != 1 || functions[0].MethodArgument != 2 || functions[1].MethodArgument != 0 || functions[0].ReturnType != "ReturnList" || functions[0].ReturnHelper != "ret_list_string" || functions[1].ReturnType != "ReturnNumberOrBool" {
		t.Fatalf("metadata = %#v", functions)
	}
	if got := strings.Join(functions[0].ArgumentChecks, ","); got != "arg_string,arg_string" {
		t.Fatalf("alpha argument checks = %q", got)
	}
	if got := strings.Join(functions[2].ArgumentChecks, ","); got != "arg_object,varargs_class" {
		t.Fatalf("zeta argument checks = %q", got)
	}
}

func TestPinnedRevisionAndGeneratedTable(t *testing.T) {
	root := "/Users/chemzqm/lib/vim"
	resolved, err := exec.Command("git", "-C", root, "rev-list", "-n", "1", vimTag).Output()
	if err != nil || strings.TrimSpace(string(resolved)) != vimCommit {
		t.Skip("pinned Vim checkout is not available")
	}
	if err := verifyRevision(root); err != nil {
		t.Fatal(err)
	}
	source, err := exec.Command("git", "-C", root, "show", vimTag+":src/evalfunc.c").Output()
	if err != nil {
		t.Fatal(err)
	}
	functions, err := parseSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(functions) != 591 {
		t.Fatalf("got %d functions, want 591", len(functions))
	}
	checks, err := parseArgumentChecks(source)
	if err != nil || len(checks) != 148 {
		t.Fatalf("argument check tables = %d, %v", len(checks), err)
	}
	for i := 1; i < len(functions); i++ {
		if functions[i-1].Name >= functions[i].Name {
			t.Fatalf("functions are not strictly sorted at %d: %q, %q", i, functions[i-1].Name, functions[i].Name)
		}
	}
	for _, name := range []string{"abs", "has", "map", "printf", "sort", "typename", "win_getid", "xor"} {
		found := false
		for _, function := range functions {
			if function.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing builtin %q", name)
		}
	}
	if got := strings.Join(functions[0].ArgumentChecks, ","); functions[0].Name != "abs" || got != "arg_float_or_nr" {
		t.Fatalf("abs metadata = %#v", functions[0])
	}
	if err := addDocumentation(root, functions); err != nil {
		t.Fatal(err)
	}
	wantSources := map[string]string{"abs": "builtin.txt", "ch_open": "channel.txt", "popup_create": "popup.txt", "term_start": "terminal.txt"}
	for _, function := range functions {
		if want := wantSources[function.Name]; want != "" && (function.DocumentationSource != want || function.Documentation == "") {
			t.Errorf("%s documentation = %q from %q", function.Name, function.Documentation, function.DocumentationSource)
		}
	}
	generated := filepath.Join(t.TempDir(), "functions_generated.go")
	if err := writeOutput(generated, functions); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(generated)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "internal", "vimdata", "functions_generated.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("functions_generated.go is stale; run tools/genbuiltins")
	}
}
