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
