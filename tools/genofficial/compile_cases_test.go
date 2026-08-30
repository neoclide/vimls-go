package main

import (
	"reflect"
	"testing"
)

func TestCompileExpectedCodes(t *testing.T) {
	tests := []struct {
		name   string
		source string
		helper string
		cases  int
		want   []string
	}{
		{name: "direct single quote", source: "'E1012: Type mismatch'", helper: "CheckDefFailure", cases: 1, want: []string{"vim/E1012"}},
		{name: "direct double quote", source: `"E1001: Variable not found"`, helper: "CheckSourceDefFailure", cases: 1, want: []string{"vim/E1001"}},
		{name: "separate lane codes", source: "['E1013: def', 'E121: script']", helper: "CheckDefAndScriptFailure", cases: 2, want: []string{"vim/E1013", "vim/E121"}},
		{name: "shared lane code", source: "'E1097:'", helper: "CheckSourceDefAndScriptFailure", cases: 2, want: []string{"vim/E1097", "vim/E1097"}},
		{name: "direct helper rejects list", source: "['E1012:', 'E121:']", helper: "CheckDefFailure", cases: 1},
		{name: "dynamic identifier", source: "msg", helper: "CheckDefAndScriptFailure", cases: 2},
		{name: "message without code", source: "'expected number but got string'", helper: "CheckDefFailure", cases: 1},
		{name: "ambiguous pattern", source: "'E1012:.*E1191:'", helper: "CheckDefFailure", cases: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := []byte(test.source)
			got := compileExpectedCodes(source, helperArgument{Start: 0, End: len(source)}, test.helper, test.cases)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("compileExpectedCodes(%q) = %q, want %q", test.source, got, test.want)
			}
		})
	}
}
