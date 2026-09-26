package workspace

import (
	"path/filepath"
	"testing"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestExportTypesReuseAnalysisAndCopyImmutableFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lib.vim")
	path = mustResolverCanonical(t, path)
	file := syntax.Parse("vim9script\nexport var Count = 1\nexport const Names = ['a']\nexport final Fixed = true\nvar Private = 2\n")
	result := analysis.Analyze(file)
	index := NewIndex(10, 10000)
	if err := index.ReplaceWithAnalysis(path, file, result); err != nil {
		t.Fatal(err)
	}
	if index.ExportTypes(path) == nil {
		t.Fatalf("missing exports: dialect=%v diagnostics=%#v facts=%#v", file.Dialect, file.Diagnostics, index.FileSymbols(path))
	}
	for _, d := range result.Declarations {
		d.Type.Name = "float"
		if len(d.Type.Arguments) > 0 {
			d.Type.Arguments[0].Name = "number"
		}
	}
	want := map[string]string{"Count": "number", "Names": "list", "Fixed": "bool", "Private": ""}
	for _, match := range index.FileSymbols(path) {
		if got := match.Fact.StaticType.ValueType(); got.Name != want[match.Fact.Name] {
			t.Errorf("%s = %#v", match.Fact.Name, got)
		}
		if match.Fact.Name == "Names" && match.Fact.StaticType.ValueType().Arguments[0].Name != "string" {
			t.Fatal("nested type mutated")
		}
	}
	copy := NewIndex(10, 10000)
	if err := copy.CopyFileFrom(index, path); err != nil {
		t.Fatal(err)
	}
	if copy.ExportTypes(path) != index.ExportTypes(path) {
		t.Fatal("copy rebuilt immutable exports")
	}
	for _, fact := range CollectSymbolFacts(path, file) {
		if fact.StaticType.ValueType().Name != "" {
			t.Fatal("syntax-only helper started inference")
		}
	}
}

func TestExportTypesKeepColdNominalValuesAndDerivedAliases(t *testing.T) {
	path := mustResolverCanonical(t, filepath.Join(t.TempDir(), "lib.vim"))
	source := `vim9script
export interface Named
endinterface
export class Base implements Named
endclass
export class Child extends Base
  def new()
  enddef
endclass
export type Alias = Child
export var Value = Child.new()
export var Values = [Value]
export def Make(): Child
  return Child.new()
enddef
`
	file := syntax.Parse(source)
	index := NewIndex(10, 10000)
	if err := index.Replace(path, file); err != nil {
		t.Fatal(err)
	}
	exports := index.ExportTypes(path)
	if exports == nil {
		t.Fatalf("missing cold export facts: %#v", file.Diagnostics)
	}
	input := index.ExportTypeInput(path)
	derived := DeriveExportTypes(input, analysis.ImportTypes{})
	if derived == nil {
		t.Fatal("missing derived export facts")
	}
	for _, summary := range []*analysis.ExportTypes{exports, derived} {
		consumer := syntax.Parse("vim9script\nimport './lib.vim' as lib\nvar value: lib.Alias = lib.Value\nvar item: lib.Named = lib.Values[0]\nvar made: lib.Base = lib.Make()\n")
		imports := analysis.NewImportTypes([]analysis.ImportTypeBinding{{Declaration: analysis.ImportDeclarationSpan(consumer, consumer.Commands[1].Import), Exports: summary}})
		result, err := analysis.AnalyzeWithOptions(consumer, analysis.Options{Imports: imports})
		if err != nil {
			t.Fatal(err)
		}
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1012" {
				t.Fatalf("unexpected nominal mismatch with %#v", diagnostic)
			}
		}
		var value, item, made analysis.ValueType
		for _, declaration := range result.Declarations {
			switch declaration.Name {
			case "value":
				value = declaration.Type
			case "item":
				item = declaration.Type
			case "made":
				made = declaration.Type
			}
		}
		if value.Nominal.Path != path || item.Nominal.Path != path || made.Nominal.Path != path || !value.HasNominal(item.Nominal) || !value.HasNominal(made.Nominal) {
			t.Fatalf("lost nominal ancestry: value=%#v item=%#v made=%#v", value, item, made)
		}
	}
}
