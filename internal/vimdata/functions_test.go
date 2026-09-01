package vimdata

import (
	"reflect"
	"testing"
)

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
	if len(builtinFunctions) != 591 {
		t.Fatalf("len(builtinFunctions) = %d, want 591", len(builtinFunctions))
	}
	seen := make(map[string]BuiltinFunction, len(builtinFunctions))
	for index, function := range builtinFunctions {
		if function.Name == "" {
			t.Fatalf("builtinFunctions[%d] has empty name", index)
		}
		if function.Documentation == "" || function.DocumentationSource == "" {
			t.Fatalf("%s documentation/source = %q/%q", function.Name, function.Documentation, function.DocumentationSource)
		}
		if index > 0 && function.Name <= builtinFunctions[index-1].Name {
			t.Fatalf("builtinFunctions unsorted at %d: %q, %q", index-1, builtinFunctions[index-1].Name, function.Name)
		}
		if _, exists := seen[function.Name]; exists {
			t.Fatalf("duplicate builtin function %q", function.Name)
		}
		seen[function.Name] = function
		if lookup, ok := LookupFunction(function.Name); !ok || !reflect.DeepEqual(lookup, function) {
			t.Fatalf("LookupFunction(%q) = %#v, %v", function.Name, lookup, ok)
		}
	}
}

func TestBuiltinFunctionsReturnsOrderedCopy(t *testing.T) {
	got := BuiltinFunctions()
	if len(got) != len(builtinFunctions) {
		t.Fatalf("BuiltinFunctions() length = %d, want %d", len(got), len(builtinFunctions))
	}
	for index, function := range builtinFunctions {
		if !reflect.DeepEqual(got[index], function) {
			t.Fatalf("BuiltinFunctions()[%d] = %#v, want %#v", index, got[index], function)
		}
	}

	got[0] = BuiltinFunction{Name: "changed"}
	if next := BuiltinFunctions(); !reflect.DeepEqual(next[0], builtinFunctions[0]) {
		t.Fatalf("BuiltinFunctions() exposed package table: first function = %#v, want %#v", next[0], builtinFunctions[0])
	}
	if function, ok := LookupFunction(builtinFunctions[0].Name); !ok || !reflect.DeepEqual(function, builtinFunctions[0]) {
		t.Fatalf("LookupFunction(%q) = %#v, %v after modifying enumeration", builtinFunctions[0].Name, function, ok)
	}
}

func TestFunctionReturnTypeDisplayName(t *testing.T) {
	if got := ReturnNumber.DisplayName(); got != "number" {
		t.Fatalf("number return type = %q", got)
	}
	if got := ReturnList.DisplayName(); got != "list<any>" {
		t.Fatalf("list return type = %q", got)
	}
	if got := ReturnUnknown.DisplayName(); got != "" {
		t.Fatalf("unknown return type = %q", got)
	}
}
