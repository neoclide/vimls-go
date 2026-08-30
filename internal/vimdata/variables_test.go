package vimdata

import "testing"

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
}
