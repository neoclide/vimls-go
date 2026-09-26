package analysis

import (
	"reflect"
	"strings"
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
		exports := NewExportTypes(members, nil)
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
		facts := CollectCompletionFactsWithOptions(file, Options{Imports: imports})
		query := NewCompletionTypes(facts)
		for _, declaration := range facts.Declarations {
			if declaration.Name == "element" && query.DeclarationType(declaration).Name != "number" {
				t.Fatal("completion did not consume imports")
			}
		}
	}
}

func TestImportedNamedAnnotationUsesCanonicalIdentity(t *testing.T) {
	file := syntax.Parse("vim9script\nimport './lib.vim' as lib\nvar value: lib.Box\n")
	identity := NominalType{Path: "/workspace/lib.vim", Span: syntax.Span{Start: 6, End: 9}, Kind: SymbolKindClass}
	exports := NewExportTypes(nil, map[string]NamedType{"Box": {Type: FreezeDerivedType(ValueType{Name: "Box", Nominal: identity}), Exported: true}})
	imports := NewImportTypes([]ImportTypeBinding{{Declaration: ImportDeclarationSpan(file, file.Commands[1].Import), Exports: exports}})
	result, err := AnalyzeWithOptions(file, Options{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range result.Declarations {
		if declaration.Name == "value" && declaration.Type.Nominal != identity {
			t.Fatalf("value type = %#v", declaration.Type)
		}
	}
}

func TestImportedNominalValuesKeepOriginsAncestorsAndEnums(t *testing.T) {
	file := syntax.Parse(`vim9script
import './one.vim' as one
import './two.vim' as two
var base: one.Base = one.Value
var named: one.Named = one.Values[0]
var alias: one.Alias = one.Factory()
var wrong: one.Child = two.Value
var wrongEnum: one.State = 1
var current = one.Current
def Make(): one.Child
  return one.Child.new()
enddef
var returned = Make()
`)
	identity := func(path, name string, start int, kind SymbolKind) NominalType {
		return NominalType{Path: path, Span: syntax.Span{Start: start, End: start + len(name)}, Kind: kind}
	}
	named := identity("/workspace/one.vim", "Named", 1, SymbolKindInterface)
	base := identity("/workspace/one.vim", "Base", 10, SymbolKindClass)
	child := identity("/workspace/one.vim", "Child", 20, SymbolKindClass)
	state := identity("/workspace/one.vim", "State", 30, SymbolKindEnum)
	oneType := ValueType{Name: "Child", Nominal: child, NominalParents: []NominalType{base, named}}
	one := NewExportTypes(map[string]StaticType{
		"Value":   FreezeDerivedType(oneType),
		"Values":  FreezeDerivedType(ValueType{Name: "list", Arguments: []ValueType{oneType}}),
		"Factory": FreezeDerivedType(ValueType{Name: "func", Return: &oneType}),
		"Current": FreezeDerivedType(ValueType{Name: "State", Nominal: state}),
	}, map[string]NamedType{
		"Named": {Type: FreezeDerivedType(ValueType{Name: "Named", Nominal: named}), Exported: true},
		"Base":  {Type: FreezeDerivedType(ValueType{Name: "Base", Nominal: base, NominalParents: []NominalType{named}}), Exported: true},
		"Child": {Type: FreezeDerivedType(oneType), Exported: true},
		"Alias": {Type: FreezeDerivedType(oneType), Exported: true},
		"State": {Type: FreezeDerivedType(ValueType{Name: "State", Nominal: state}), Exported: true},
	})
	twoChild := ValueType{Name: "Child", Nominal: identity("/workspace/two.vim", "Child", 20, SymbolKindClass)}
	two := NewExportTypes(map[string]StaticType{"Value": FreezeDerivedType(twoChild)}, nil)
	imports := NewImportTypes([]ImportTypeBinding{
		{Declaration: ImportDeclarationSpan(file, file.Commands[1].Import), Exports: one},
		{Declaration: ImportDeclarationSpan(file, file.Commands[2].Import), Exports: two},
	})
	result, err := AnalyzeWithOptions(file, Options{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	var mismatches int
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1012" {
			mismatches++
		}
	}
	if mismatches != 2 {
		t.Fatalf("E1012 count = %d, diagnostics=%#v", mismatches, result.Diagnostics)
	}
	for _, declaration := range result.Declarations {
		switch declaration.Name {
		case "current":
			if declaration.Type.Nominal != state {
				t.Fatalf("enum instance type = %#v", declaration.Type)
			}
		case "returned":
			if declaration.Type.Nominal != child {
				t.Fatalf("returned type = %#v", declaration.Type)
			}
		}
	}
}

func TestImportedNamedTypesAreDetachedPerAnalysis(t *testing.T) {
	file := syntax.Parse("vim9script\nimport './lib.vim' as lib\nvar names: lib.Names\nvar children: list<lib.Child>\n")
	child := NominalType{Path: "/workspace/lib.vim", Span: syntax.Span{Start: 20, End: 25}, Kind: SymbolKindClass}
	exports := NewExportTypes(nil, map[string]NamedType{
		"Names": {Type: FreezeDerivedType(ValueType{Name: "list", Arguments: []ValueType{{Name: "string"}}}), Exported: true},
		"Child": {Type: FreezeDerivedType(ValueType{Name: "Child", Nominal: child}), Exported: true},
	})
	imports := NewImportTypes([]ImportTypeBinding{{Declaration: ImportDeclarationSpan(file, file.Commands[1].Import), Exports: exports}})
	first, err := AnalyzeWithOptions(file, Options{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range first.Declarations {
		if declaration.Name == "names" {
			declaration.Type.Arguments[0].Name = "number"
		}
	}
	second, err := AnalyzeWithOptions(file, Options{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range second.Declarations {
		switch declaration.Name {
		case "names":
			if declaration.Type.Arguments[0].Name != "string" {
				t.Fatalf("mutated imported alias: %#v", declaration.Type)
			}
		case "children":
			if len(declaration.Type.Arguments) != 1 || declaration.Type.Arguments[0].Nominal != child {
				t.Fatalf("nested imported type = %#v", declaration.Type)
			}
		}
	}
}

func TestImportedNominalDiagnosticsKeepUnresolvedAncestorsConservative(t *testing.T) {
	file := syntax.Parse(`vim9script
import './base.vim' as base
import './missing.vim' as missing
class Child extends missing.Parent
endclass
class Grandchild extends Child
endclass
var value: base.Base = Grandchild.new()
var wrong: string = Grandchild.new()
`)
	base := ValueType{Name: "Base", Nominal: NominalType{Path: "/workspace/base.vim", Span: syntax.Span{Start: 24, End: 28}, Kind: SymbolKindClass}, TypeValue: true}
	exports := NewExportTypes(nil, map[string]NamedType{"Base": {Type: FreezeDerivedType(base), Exported: true}})
	imports := NewImportTypes([]ImportTypeBinding{{Declaration: ImportDeclarationSpan(file, file.Commands[1].Import), Exports: exports}})
	result, err := AnalyzeWithOptions(file, Options{Imports: imports, SourceIdentity: "/workspace/main.vim"})
	if err != nil {
		t.Fatal(err)
	}
	var mismatches []syntax.Diagnostic
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1012" {
			mismatches = append(mismatches, diagnostic)
		}
	}
	if len(mismatches) != 1 || mismatches[0].Span.Start < strings.Index(file.Source, "var wrong") {
		t.Fatalf("nominal diagnostics = %#v", mismatches)
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
	}, nil)}})
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
