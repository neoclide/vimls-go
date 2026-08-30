package vimdata

import "testing"

func TestLookupFunctionMetadata(t *testing.T) {
	if BuiltinVimTag != "v9.2.1015" || BuiltinVimCommit != "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae" {
		t.Fatalf("builtin provenance = %s/%s", BuiltinVimTag, BuiltinVimCommit)
	}
	tests := []struct {
		name                  string
		min, max, checkCount  int
		returnType            FunctionReturnType
		firstCheck, lastCheck string
	}{
		{"abs", 1, 1, 1, ReturnAny, "arg_float_or_nr", "arg_float_or_nr"},
		{"argc", 0, 1, 1, ReturnNumber, "arg_number", "arg_number"},
		{"printf", 1, 19, 19, ReturnString, "arg_string_or_nr", "arg_any"},
		{"range", 1, 3, 3, ReturnList, "arg_number", "arg_number"},
		{"ch_open", 1, 2, 2, ReturnChannel, "arg_string", "arg_dict_any"},
		{"map", 2, 2, 2, ReturnUnknown, "arg_list_or_dict_or_blob_or_string_mod", "arg_map_func"},
		{"instanceof", 2, -1, 2, ReturnBool, "arg_object", "varargs_class"},
		{"xor", 2, 2, 2, ReturnNumber, "arg_number", "arg_number"},
	}
	if function, ok := LookupFunction("append"); !ok || function.MethodArgument != 2 {
		t.Fatalf("append method metadata = %#v, %v", function, ok)
	}
	if function, ok := LookupFunction("argc"); !ok || function.MethodArgument != 0 {
		t.Fatalf("argc method metadata = %#v, %v", function, ok)
	}
	if function, ok := LookupFunction("copy"); !ok || function.ReturnHelper != "ret_copy" {
		t.Fatalf("copy return metadata = %#v, %v", function, ok)
	}
	for _, test := range tests {
		function, ok := LookupFunction(test.name)
		if !ok || function.Name != test.name || function.MinArgs != test.min || function.MaxArgs != test.max || function.ReturnType != test.returnType || len(function.ArgumentChecks) != test.checkCount || function.ArgumentChecks[0] != test.firstCheck || function.ArgumentChecks[len(function.ArgumentChecks)-1] != test.lastCheck || function.Documentation == "" || function.DocumentationSource == "" {
			t.Fatalf("LookupFunction(%q) = %#v, %v", test.name, function, ok)
		}
	}
	if _, ok := LookupFunction(""); ok {
		t.Fatal("empty function resolved")
	}
	if _, ok := LookupFunction("ABS"); ok {
		t.Fatal("case-insensitive function resolved")
	}
	if BuiltinFunctionCount() != 591 {
		t.Fatalf("BuiltinFunctionCount() = %d, want 591", BuiltinFunctionCount())
	}
}
