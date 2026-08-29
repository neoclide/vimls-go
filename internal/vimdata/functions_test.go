package vimdata

import "testing"

func TestLookupFunctionMetadata(t *testing.T) {
	if BuiltinVimTag != "v9.2.1015" || BuiltinVimCommit != "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae" {
		t.Fatalf("builtin provenance = %s/%s", BuiltinVimTag, BuiltinVimCommit)
	}
	tests := []struct {
		name       string
		min, max   int
		returnType FunctionReturnType
	}{
		{"abs", 1, 1, ReturnAny},
		{"argc", 0, 1, ReturnNumber},
		{"printf", 1, 19, ReturnString},
		{"range", 1, 3, ReturnList},
		{"ch_open", 1, 2, ReturnChannel},
		{"map", 2, 2, ReturnUnknown},
		{"xor", 2, 2, ReturnNumber},
	}
	for _, test := range tests {
		function, ok := LookupFunction(test.name)
		if !ok || function.Name != test.name || function.MinArgs != test.min || function.MaxArgs != test.max || function.ReturnType != test.returnType {
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
