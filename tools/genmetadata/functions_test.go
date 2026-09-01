package main

import (
	"strings"
	"testing"
)

func TestParseFunctionSourceFixture(t *testing.T) {
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
	functions, err := parseFunctionSource(source)
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
