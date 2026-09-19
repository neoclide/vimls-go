package analysis

import (
	"reflect"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestStaticTypeIsolationAndUnknownIdentities(t *testing.T) {
	value := ValueType{Name: "func", Arguments: []ValueType{{Name: "list", Arguments: []ValueType{{Name: "string"}}}}, Return: &ValueType{Name: "number"}, ArgumentCountKnown: true, RequiredArguments: 1, Variadic: true}
	frozen := FreezeType(value)
	want := frozen.ValueType()
	value.Arguments[0].Arguments[0].Name = "float"
	value.Return.Name = "string"
	copy := frozen.ValueType()
	copy.Arguments[0].Arguments[0].Name = "bool"
	copy.Return.Name = "float"
	if got := frozen.ValueType(); !reflect.DeepEqual(got, want) {
		t.Fatalf("frozen type mutated: %#v", got)
	}
	for _, typ := range []ValueType{{Name: "Widget"}, {Name: "number", imported: true}, {}} {
		if got := FreezeType(typ).ValueType(); got.Name != "" {
			t.Fatalf("unsafe type survived: %#v", got)
		}
	}
	cycle := &ValueType{Name: "func"}
	cycle.Return = cycle
	_ = FreezeType(*cycle) // bounded even for adversarial caller input
	branch := ValueType{Name: "func", Arguments: make([]ValueType, 2)}
	branch.Arguments[0], branch.Arguments[1] = branch, branch
	_ = FreezeType(branch).ValueType() // shared cycles must not expand exponentially
}

func TestImportedValueInferenceAndLocalExportBoundary(t *testing.T) {
	for _, alias := range []string{"as lib", ""} {
		source := "vim9script\nimport './lib.vim' " + alias + "\n" + `
var numberValue = lib.Count
var element = lib.Items[0]
var called = lib.Callback('x')
var dynamicValue = lib.Dynamic
var missingValue = lib.Private
export var Forwarded = lib.Count
export var Derived = lib.Count + 1
export var [Destructured] = lib.Items
export var Annotated: number = lib.Count
def Check()
  var wrong: string = lib.Count
enddef
def Shadow(lib: dict<string>)
  var shadowed = lib.Count
enddef
`
		file := syntax.Parse(source)
		span := ImportDeclarationSpan(file, file.Commands[1].Import)
		members := map[string]StaticType{
			"Count":    FreezeType(ValueType{Name: "number"}),
			"Items":    FreezeType(ValueType{Name: "list", Arguments: []ValueType{{Name: "number"}}}),
			"Callback": FreezeType(ValueType{Name: "func", Arguments: []ValueType{{Name: "string"}}, Return: &ValueType{Name: "bool"}, ArgumentCountKnown: true, RequiredArguments: 1}),
			"Dynamic":  FreezeType(ValueType{Name: "any"}),
		}
		exports := NewExportTypes(members)
		members["Count"] = FreezeType(ValueType{Name: "string"})
		bindings := []ImportTypeBinding{{Declaration: span, Exports: exports}}
		imports := NewImportTypes(bindings)
		bindings[0].Exports = nil
		result, err := AnalyzeWithOptions(file, Options{Imports: imports})
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"numberValue": "number", "element": "number", "called": "bool", "dynamicValue": "any", "missingValue": "", "shadowed": "string", "Annotated": "number"}
		for _, declaration := range result.Declarations {
			if expected, ok := want[declaration.Name]; ok {
				if declaration.Type.Name != expected {
					t.Errorf("%s = %#v, want %s", declaration.Name, declaration.Type, expected)
				}
				delete(want, declaration.Name)
			}
			switch declaration.Name {
			case "Forwarded", "Derived", "Destructured":
				if got := FreezeType(declaration.Type).ValueType(); got.Name != "" {
					t.Errorf("external fact leaked via %s: %#v", declaration.Name, got)
				}
			case "Annotated":
				if got := FreezeType(declaration.Type).ValueType(); got.Name != "number" {
					t.Errorf("explicit annotation lost: %#v", got)
				}
			}
		}
		if len(want) != 0 {
			t.Fatal(want)
		}
		mismatch := false
		for _, diagnostic := range result.Diagnostics {
			mismatch = mismatch || diagnostic.Code == "vim/E1012"
		}
		if !mismatch {
			t.Fatalf("missing E1012: %#v", result.Diagnostics)
		}
		facts := CollectCompletionFactsWithImports(file, imports)
		query := NewCompletionTypes(facts)
		for _, declaration := range facts.Declarations {
			if declaration.Name == "element" && query.DeclarationType(declaration).Name != "number" {
				t.Fatal("completion did not consume imports")
			}
		}
	}
}

func TestImportedTypeProvenanceIsIndependentOfIndexWarmth(t *testing.T) {
	file := syntax.Parse(`vim9script
import './lib.vim'
export var Literal = 1
export var Compare = lib.Count == 1
export var Length = len(lib.Items)
export var [Item] = lib.Items
export var ItemCompare = Item == 1
export var Annotated: number = lib.Count
def Choose()
  if g:flag
    return lib.Count
  endif
  return 1
enddef
export var FromFunction = Choose()
export var Closure = () => {
  if g:flag
    return lib.Count
  endif
  return 1
}
`)
	imports := NewImportTypes([]ImportTypeBinding{{Declaration: ImportDeclarationSpan(file, file.Commands[1].Import), Exports: NewExportTypes(map[string]StaticType{
		"Count": FreezeType(ValueType{Name: "number"}),
		"Items": FreezeType(ValueType{Name: "list", Arguments: []ValueType{{Name: "number"}}}),
	})}})
	cold := Analyze(file)
	warm, err := AnalyzeWithOptions(file, Options{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	for i, decl := range cold.Declarations {
		if decl.Scope != cold.Root || decl.Kind != SymbolKindVariable {
			continue
		}
		a, b := FreezeType(decl.Type).ValueType(), FreezeType(warm.Declarations[i].Type).ValueType()
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s cold=%#v warm=%#v", decl.Name, a, b)
		}
	}
}
