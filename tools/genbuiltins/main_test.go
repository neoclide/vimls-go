package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestParseSourceFixture(t *testing.T) {
	source := []byte(`static argcheck_T arg2_string[] = {arg_string, arg_string};
static const funcentry_T global_functions[] =
{
    {"zeta", 1, VARGS, 0, NULL,
            ret_any, f_zeta},
    {"alpha", 0, 2, 0, arg2_string,
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
	if functions[2].MaxArgs != -1 || functions[0].ReturnType != "ReturnList" || functions[1].ReturnType != "ReturnNumberOrBool" {
		t.Fatalf("metadata = %#v", functions)
	}
	if got := strings.Join(functions[0].ArgumentChecks, ","); got != "arg_string,arg_string" {
		t.Fatalf("alpha argument checks = %q", got)
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
}
