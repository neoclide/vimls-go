package analysis

import (
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// Provenance: Vim v9.2.1015 src/testdir/test_tuple.vim, especially
// Test_tuple_indexing, Test_tuple_slice, Test_multi_assign_from_tuple,
// Test_tuple_for and Test_tuple_modify_mutable_item. Runtime values are also
// checked by test/oracle/testdata/legacy_tuple.vim.
func TestLegacyTupleTypesAndReferences(t *testing.T) {
	file := syntax.Parse(`let pair = (1, 'two')
let alias = pair
let first = alias[0]
let last = pair[-1]
let sliced = pair[0 : 0]
let [count, label] = pair
let [head; rest] = pair
for item in (1, 2)
  echo item
endfor
for [key, value] in ((1, 'one'), (2, 'two'))
  echo key value
endfor
function! Dynamic(values) abort
  let l:unknown = a:values[0]
endfunction
`)
	result := Analyze(file)
	if diagnostics := CombinedDiagnostics(file, result); len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
	declarations := make(map[string]*Declaration)
	for _, declaration := range result.Declarations {
		declarations[declaration.Name] = declaration
	}
	for name, want := range map[string]string{
		"pair": "tuple", "alias": "tuple", "first": "number", "last": "string",
		"sliced": "tuple", "count": "number", "label": "string", "head": "number",
		"rest": "tuple", "item": "number", "key": "number", "value": "string", "l:unknown": "",
	} {
		if declaration := declarations[name]; declaration == nil || declaration.Type.Name != want {
			t.Errorf("%s = %#v, want type %q", name, declaration, want)
		}
	}
	for name, want := range map[string][]string{"pair": {"number", "string"}, "alias": {"number", "string"}, "rest": {"string"}} {
		declaration := declarations[name]
		if declaration == nil || len(declaration.Type.Arguments) != len(want) {
			t.Fatalf("%s = %#v, want tuple arguments %v", name, declaration, want)
		}
		for i, argument := range declaration.Type.Arguments {
			if argument.Name != want[i] {
				t.Errorf("%s argument %d = %q, want %q", name, i, argument.Name, want[i])
			}
		}
	}
	// Slices deliberately retain only their container kind, not unsound arity.
	if len(declarations["sliced"].Type.Arguments) != 0 {
		t.Fatal("slice retained source tuple arity")
	}
	resolved := make(map[string]int)
	for _, reference := range result.References {
		if reference.Name == "pair" || reference.Name == "alias" || reference.Name == "item" || reference.Name == "key" || reference.Name == "value" {
			if reference.Declaration != declarations[reference.Name] {
				t.Errorf("reference %q does not resolve to its lexical declaration", reference.Name)
			}
			resolved[reference.Name]++
		}
	}
	for _, name := range []string{"pair", "alias", "item", "key", "value"} {
		if resolved[name] == 0 {
			t.Errorf("no reference for %s", name)
		}
	}
}

func TestLegacyTupleMutationBoundary(t *testing.T) {
	for _, test := range []struct{ source, code string }{
		{"let pair = (1, 2)\nlet pair[0] = 3\n", "vim/E1532"},
		{"let pair = (1, (2, 3))\nlet pair[1][0] = 3\n", "vim/E1532"},
		{"let [only] = (1, 2)\n", "vim/E1537"},
		{"let [one, two] = (1,)\n", "vim/E1538"},
		{"let [one; rest] = (1, 2)\n", ""},
		{"let pair = (1, 2)\nlet pair[-1] = 3\n", "vim/E1532"},
		{"let pair = (1, 2)\nlet pair[9] = 3\n", ""},
		{"let pair = (1, 2)\nlet pair[0] =\n", "vimls/missing-expression"},
		{"let pair = (1, 2)\ncall ReplacePair()\nlet pair[0] = 3\n", ""},
		{"let pair = (1, 2)\nlet pair = [1, 2]\nlet pair[0] = 3\n", ""},
		{"let pair = (1, 2)\nlet pair[DynamicIndex()] = 3\n", ""},
		{"let pair = ([1], {'key': 2})\nlet pair[0][0] = 3\nlet pair[1].key = 4\n", ""},
		{"function! Dynamic(value) abort\nlet a:value[0] = 3\nendfunction\n", ""},
	} {
		t.Run(test.source, func(t *testing.T) {
			file := syntax.Parse(test.source)
			diagnostics := CombinedDiagnostics(file, Analyze(file))
			if test.code == "" && len(diagnostics) == 0 {
				return
			}
			if len(diagnostics) == 1 && diagnostics[0].Code == test.code {
				return
			}
			t.Fatalf("diagnostics = %#v, want %q", diagnostics, test.code)
		})
	}
}
