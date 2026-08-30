package vimdata

import (
	"reflect"
	"testing"
)

func TestLookupVariableMetadata(t *testing.T) {
	if VariableVimTag != "v9.2.1015" || VariableVimCommit != "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae" {
		t.Fatalf("variable provenance = %s/%s", VariableVimTag, VariableVimCommit)
	}
	tests := []struct {
		name, typ, source string
		flags             VariableFlags
	}{
		{"v:count", "number", "eval.txt", VariableCompatible | VariableReadOnly},
		{"v:errmsg", "string", "eval.txt", VariableCompatible},
		{"v:oldfiles", "list<string>", "eval.txt", 0},
		{"v:event", "dict<any>", "eval.txt", VariableReadOnly},
		{"v:stacktrace", "list<dict<any>>", "eval.txt", VariableReadOnly},
	}
	for _, test := range tests {
		variable, ok := LookupVariable(test.name)
		if !ok || variable.Name != test.name || variable.Type != test.typ || variable.Flags != test.flags || variable.Documentation == "" || variable.DocumentationSource != test.source {
			t.Errorf("LookupVariable(%q) = %#v, %v", test.name, variable, ok)
		}
	}
	for _, name := range []string{"", "errmsg", "g:errmsg", "v:nosuch"} {
		if variable, ok := LookupVariable(name); ok {
			t.Errorf("LookupVariable(%q) = %#v, true", name, variable)
		}
	}
	if BuiltinVariableCount() != 118 {
		t.Fatalf("BuiltinVariableCount() = %d, want 118", BuiltinVariableCount())
	}
	seen := make(map[string]Variable, len(builtinVariables))
	for _, variable := range builtinVariables {
		if variable.Name == "" {
			t.Fatalf("variable has empty name: %#v", variable)
		}
		if variable.Documentation == "" || variable.DocumentationSource == "" {
			t.Fatalf("%s documentation/source = %q/%q", variable.Name, variable.Documentation, variable.DocumentationSource)
		}
		if variable.DocumentationSource != "eval.txt" {
			t.Fatalf("%s documentation source = %q", variable.Name, variable.DocumentationSource)
		}
		if _, exists := seen[variable.Name]; exists {
			t.Fatalf("duplicate builtin variable %q", variable.Name)
		}
		seen[variable.Name] = variable
		if got, ok := LookupVariable(variable.Name); !ok || !reflect.DeepEqual(got, variable) {
			t.Fatalf("LookupVariable(%q) = %#v, %v", variable.Name, got, ok)
		}
	}
}
