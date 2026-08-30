package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/chemzqm/vimls-go/internal/analysis"
	"github.com/chemzqm/vimls-go/internal/syntax"
)

func TestIndexLookupIncludesNestedSymbols(t *testing.T) {
	index := NewIndex(10, 10000)
	path := filepath.Join(t.TempDir(), "nested.vim")
	file := syntax.Parse("vim9script\nclass Widget\n  def run()\n    var inside = 1\n  enddef\nendclass\n")
	if err := index.Replace(path, file); err != nil {
		t.Fatal(err)
	}
	class := index.Lookup("Widget")
	if len(class) != 1 || class[0].Path != mustResolverCanonical(t, path) || class[0].Kind != analysis.SymbolKindClass {
		t.Fatalf("class lookup = %#v", class)
	}
	method := index.Lookup("run")
	if len(method) != 1 || method[0].Kind != analysis.SymbolKindMethod {
		t.Fatalf("method lookup = %#v", method)
	}
	variable := index.Lookup("inside")
	if len(variable) != 1 || variable[0].Kind != analysis.SymbolKindVariable {
		t.Fatalf("nested lookup = %#v", variable)
	}
}

func TestIndexLookupOrdersSameNamesAndReplaceRemovesOldSymbols(t *testing.T) {
	index := NewIndex(10, 10000)
	root := t.TempDir()
	first := filepath.Join(root, "z.vim")
	second := filepath.Join(root, "a.vim")
	if err := index.Replace(first, syntax.Parse("var same = 1\n")); err != nil {
		t.Fatal(err)
	}
	if err := index.Replace(second, syntax.Parse("var same = 2\nvar same = 3\n")); err != nil {
		t.Fatal(err)
	}
	got := index.Lookup("same")
	if len(got) != 3 || got[0].Path != mustResolverCanonical(t, second) || got[1].Path != mustResolverCanonical(t, second) || got[2].Path != mustResolverCanonical(t, first) {
		t.Fatalf("same-name order = %#v", got)
	}
	if got[0].SelectionRange.Start >= got[1].SelectionRange.Start {
		t.Fatalf("same-file spans are not ordered: %#v", got)
	}
	if err := index.Replace(first, syntax.Parse("var replacement = 1\n")); err != nil {
		t.Fatal(err)
	}
	if len(index.Lookup("same")) != 2 || len(index.Lookup("replacement")) != 1 {
		t.Fatalf("replace did not remove old facts: same=%#v replacement=%#v", index.Lookup("same"), index.Lookup("replacement"))
	}
}

func TestIndexSearchRanksSubsequencesAndLimitsResults(t *testing.T) {
	index := NewIndex(10, 10000)
	root := t.TempDir()
	path := filepath.Join(root, "symbols.vim")
	source := "var Exact = 1\nvar exactly = 2\nvar e_x_a_c_t = 3\nvar unrelated = 4\n"
	if err := index.Replace(path, syntax.Parse(source)); err != nil {
		t.Fatal(err)
	}
	results := index.Search("EXACT", 0)
	if len(results) != 3 {
		t.Fatalf("search result count = %d, want 3: %#v", len(results), results)
	}
	if got := []string{results[0].Fact.Name, results[1].Fact.Name, results[2].Fact.Name}; got[0] != "Exact" || got[1] != "exactly" || got[2] != "e_x_a_c_t" {
		t.Fatalf("search ranking = %#v", got)
	}
	if results[0].Source != source || results[1].Source != source || results[2].Source != source {
		t.Fatalf("search source = %#v", results)
	}
	if got := index.Search("x_a_c", 1); len(got) != 1 || got[0].Fact.Name != "e_x_a_c_t" {
		t.Fatalf("subsequence or limit result = %#v", got)
	}
	if got := index.Search("", -1); len(got) != 4 {
		t.Fatalf("empty query result count = %d", len(got))
	}
}

func TestIndexSearchReturnsIndependentFactsAndRetainsCurrentSource(t *testing.T) {
	index := NewIndex(10, 10000)
	root := t.TempDir()
	path := filepath.Join(root, "source.vim")
	oldSource := "var old = 1\n"
	oldFile := syntax.Parse(oldSource)
	if err := index.Replace(path, oldFile); err != nil {
		t.Fatal(err)
	}
	if retained, ok := index.Source(path); !ok || retained != oldSource {
		t.Fatalf("Source() = %q, %v", retained, ok)
	}
	oldFile.Source = "var mutated = 2\n"
	results := index.Search("old", 0)
	if len(results) != 1 || results[0].Source != oldSource {
		t.Fatalf("retained source = %#v", results)
	}
	results[0].Fact.Name = "changed"
	results[0].Fact.Range.Start = 999
	results[0].Source = "changed"
	results = index.Search("old", 0)
	if len(results) != 1 || results[0].Fact.Name != "old" || results[0].Fact.Range.Start == 999 || results[0].Source != oldSource {
		t.Fatalf("search leaked mutable result: %#v", results)
	}

	newSource := "var replacement = 123\n"
	if err := index.Replace(path, syntax.Parse(newSource)); err != nil {
		t.Fatal(err)
	}
	if index.IndexedBytes() != len(newSource) || len(index.Search("old", 0)) != 0 {
		t.Fatalf("replacement state: bytes=%d old=%#v", index.IndexedBytes(), index.Search("old", 0))
	}
	if retained, ok := index.Source(path); !ok || retained != newSource {
		t.Fatalf("replacement Source() = %q, %v", retained, ok)
	}
	results = index.Search("replacement", 0)
	if len(results) != 1 || results[0].Source != newSource {
		t.Fatalf("replacement source = %#v", results)
	}
	index.Remove(path)
	if retained, ok := index.Source(path); ok || retained != "" {
		t.Fatalf("removed Source() = %q, %v", retained, ok)
	}
	if index.IndexedBytes() != 0 || len(index.Search("replacement", 0)) != 0 {
		t.Fatalf("removal state: bytes=%d results=%#v", index.IndexedBytes(), index.Search("replacement", 0))
	}
}

func TestIndexCollectsExportedSymbolsAndStaticExternalReferences(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.vim")
	source := `vim9script
import './lib.vim' as lib
var first = lib.Run()
echo lib.Value
var typed: lib.Public<number>
echo unknown.Run()
export def Public()
enddef
def Private()
enddef
`
	file := syntax.Parse(source)
	facts := CollectSymbolFacts(path, file)
	exported := make(map[string]bool)
	for _, fact := range facts {
		exported[fact.Name] = fact.Exported
	}
	if !exported["Public"] || exported["Private"] || exported["first"] {
		t.Fatalf("exported facts = %#v", facts)
	}
	references := CollectExternalReferences(path, file)
	if len(references) != 3 || references[0].Name != "Run" || references[1].Name != "Value" || references[2].Name != "Public" {
		t.Fatalf("import references = %#v", references)
	}
	for _, reference := range references {
		if reference.Kind != ExternalReferenceImportMember || reference.ImportPath != "'./lib.vim'" || file.Text(reference.Span) != reference.Name {
			t.Fatalf("import reference = %#v", reference)
		}
	}
	defaultImport := syntax.Parse("vim9script\nimport autoload 'for/search.vim'\necho search.Run()\n")
	defaultReferences := CollectExternalReferences(filepath.Join(root, "default.vim"), defaultImport)
	if len(defaultReferences) != 1 || defaultReferences[0].Name != "Run" || defaultReferences[0].ImportPath != "'for/search.vim'" || !defaultReferences[0].ImportAutoload {
		t.Fatalf("default import reference = %#v", defaultReferences)
	}
	ambiguousImport := syntax.Parse("vim9script\nimport './one.vim' as duplicate\nimport './two.vim' as duplicate\necho duplicate.Run()\n")
	if ambiguousReferences := CollectExternalReferences(filepath.Join(root, "ambiguous.vim"), ambiguousImport); len(ambiguousReferences) != 0 {
		t.Fatalf("ambiguous import references = %#v", ambiguousReferences)
	}

	legacyPath := filepath.Join(root, "legacy.vim")
	legacySource := "call foo#bar#Run()\nlet value = g:foo#bar#Value\n"
	legacy := syntax.Parse(legacySource)
	legacyReferences := CollectExternalReferences(legacyPath, legacy)
	if len(legacyReferences) != 2 || legacyReferences[0].Name != "foo#bar#Run" || legacyReferences[1].Name != "foo#bar#Value" {
		t.Fatalf("autoload references = %#v", legacyReferences)
	}
	for _, reference := range legacyReferences {
		if reference.Kind != ExternalReferenceAutoload {
			t.Fatalf("autoload reference = %#v", reference)
		}
	}

	index := NewIndex(10, 10000)
	if err := index.Replace(path, file); err != nil {
		t.Fatal(err)
	}
	if err := index.Replace(legacyPath, legacy); err != nil {
		t.Fatal(err)
	}
	if got := index.ExternalReferences("Run"); len(got) != 1 || got[0].Fact.Path != mustResolverCanonical(t, path) || got[0].Source != source {
		t.Fatalf("indexed import references = %#v", got)
	}
	if got := index.ExternalReferences("foo#bar#Run"); len(got) != 1 || got[0].Fact.Path != mustResolverCanonical(t, legacyPath) || got[0].Source != legacySource {
		t.Fatalf("indexed autoload references = %#v", got)
	}
	if got := index.LookupFile(path, "Public"); len(got) != 1 || !got[0].Fact.Exported || got[0].Source != source {
		t.Fatalf("file lookup = %#v", got)
	}
	if err := index.Replace(path, syntax.Parse("vim9script\n")); err != nil {
		t.Fatal(err)
	}
	if got := index.ExternalReferences("Run"); len(got) != 0 {
		t.Fatalf("replaced references = %#v", got)
	}
}

func TestIndexUsesCanonicalPathIdentity(t *testing.T) {
	realRoot := t.TempDir()
	aliasRoot := filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	realPath := filepath.Join(realRoot, "main.vim")
	aliasPath := filepath.Join(aliasRoot, "main.vim")
	index := NewIndex(1, 1000)
	if err := index.Replace(aliasPath, syntax.Parse("var old = 1\n")); err != nil {
		t.Fatal(err)
	}
	if _, ok := index.Source(realPath); !ok {
		t.Fatal("canonical path did not find symlink-indexed source")
	}
	if err := index.Replace(realPath, syntax.Parse("var current = 1\n")); err != nil {
		t.Fatal(err)
	}
	if index.FileCount() != 1 || len(index.Lookup("old")) != 0 || len(index.Lookup("current")) != 1 {
		t.Fatalf("canonical replacement state: files=%d old=%#v current=%#v", index.FileCount(), index.Lookup("old"), index.Lookup("current"))
	}
	index.Remove(aliasPath)
	if index.FileCount() != 0 {
		t.Fatalf("canonical removal left %d files", index.FileCount())
	}
}

func TestIndexRemoveFreesCapacity(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.vim")
	second := filepath.Join(root, "second.vim")
	file := syntax.Parse("var value = 1\n")
	index := NewIndex(1, len(file.Source))
	if err := index.Replace(first, file); err != nil {
		t.Fatal(err)
	}
	if index.FileCount() != 1 || index.IndexedBytes() != len(file.Source) {
		t.Fatalf("initial stats = %d files, %d bytes", index.FileCount(), index.IndexedBytes())
	}
	if err := index.Replace(second, file); !errors.Is(err, ErrIndexLimit) {
		t.Fatalf("second replace error = %v, want ErrIndexLimit", err)
	}
	index.Remove(first)
	if index.FileCount() != 0 || index.IndexedBytes() != 0 || len(index.Lookup("value")) != 0 {
		t.Fatalf("remove stats = %d files, %d bytes, lookup=%#v", index.FileCount(), index.IndexedBytes(), index.Lookup("value"))
	}
	if err := index.Replace(second, file); err != nil {
		t.Fatalf("replace after remove: %v", err)
	}
}

func TestIndexRevisionTracksSuccessfulChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "entry.vim")
	file := syntax.Parse("var value = 1\n")
	index := NewIndex(1, len(file.Source))
	if got := index.Revision(); got != 0 {
		t.Fatalf("initial revision = %d", got)
	}
	index.Remove(path)
	if got := index.Revision(); got != 0 {
		t.Fatalf("no-op remove revision = %d", got)
	}
	if err := index.Replace(path, file); err != nil {
		t.Fatal(err)
	}
	if got := index.Revision(); got != 1 {
		t.Fatalf("replace revision = %d", got)
	}
	if err := index.Replace(filepath.Join(root, "other.vim"), file); !errors.Is(err, ErrIndexLimit) {
		t.Fatalf("rejected replace error = %v", err)
	}
	if got := index.Revision(); got != 1 {
		t.Fatalf("rejected replace revision = %d", got)
	}
	index.Remove(path)
	if got := index.Revision(); got != 2 {
		t.Fatalf("remove revision = %d", got)
	}
}

func TestIndexLimitsRejectReplaceAndKeepOldEntry(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "entry.vim")
	old := syntax.Parse("var old = 1\n")
	newFile := syntax.Parse("var replacement = 123456\n")
	index := NewIndex(1, len(old.Source))
	if err := index.Replace(path, old); err != nil {
		t.Fatal(err)
	}
	if err := index.Replace(path, newFile); !errors.Is(err, ErrIndexLimit) {
		t.Fatalf("oversized replacement error = %v, want ErrIndexLimit", err)
	}
	if len(index.Lookup("old")) != 1 || len(index.Lookup("replacement")) != 0 || index.IndexedBytes() != len(old.Source) {
		t.Fatalf("rejected replacement changed state: old=%#v replacement=%#v bytes=%d", index.Lookup("old"), index.Lookup("replacement"), index.IndexedBytes())
	}
	other := filepath.Join(root, "other.vim")
	if err := index.Replace(other, old); !errors.Is(err, ErrIndexLimit) {
		t.Fatalf("file-limit error = %v, want ErrIndexLimit", err)
	}
	if index.FileCount() != 1 || len(index.Lookup("old")) != 1 {
		t.Fatalf("rejected file changed state: files=%d lookup=%#v", index.FileCount(), index.Lookup("old"))
	}

	exact := NewIndex(2, len(old.Source))
	if err := exact.Replace(path, old); err != nil {
		t.Fatalf("exact byte limit rejected: %v", err)
	}
	if exact.IndexedBytes() != len(old.Source) {
		t.Fatalf("exact byte stats = %d", exact.IndexedBytes())
	}
}

func TestIndexRejectsInvalidInputAndIsolatedResults(t *testing.T) {
	index := NewIndex(10, 1000)
	file := syntax.Parse("var value = 1\n")
	if err := index.Replace("", file); !errors.Is(err, ErrIndexInvalidPath) {
		t.Fatalf("empty path error = %v", err)
	}
	if err := index.Replace("  \t", file); !errors.Is(err, ErrIndexInvalidPath) {
		t.Fatalf("blank path error = %v", err)
	}
	if err := index.Replace("file.vim", nil); !errors.Is(err, ErrIndexNilFile) {
		t.Fatalf("nil file error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "isolated.vim")
	if err := index.Replace(path, file); err != nil {
		t.Fatal(err)
	}
	result := index.Lookup("value")
	if len(result) != 1 {
		t.Fatalf("lookup = %#v", result)
	}
	result[0].Path = "mutated"
	result[0].Range.Start = 999
	result = append(result, SymbolFact{Name: "caller-only"})
	file.Source = "var changed = 2\n"
	file.Commands = nil
	again := index.Lookup("value")
	if len(again) != 1 || again[0].Path == "mutated" || again[0].Range.Start == 999 || len(index.Lookup("caller-only")) != 0 {
		t.Fatalf("index leaked mutable state: %#v", again)
	}
	index.Remove("")
}

func TestIndexConcurrentOperations(t *testing.T) {
	index := NewIndex(20, 100000)
	root := t.TempDir()
	var group sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		worker := worker
		group.Add(1)
		go func() {
			defer group.Done()
			for iteration := 0; iteration < 100; iteration++ {
				path := filepath.Join(root, "file", string(rune('a'+worker)), "doc.vim")
				file := syntax.Parse("var shared = 1\n")
				_ = index.Replace(path, file)
				_ = index.Lookup("shared")
				_ = index.Search("sha", 0)
				if iteration%3 == 0 {
					index.Remove(path)
				}
			}
		}()
	}
	group.Wait()
	if index.FileCount() < 0 || index.IndexedBytes() < 0 {
		t.Fatalf("invalid final stats: files=%d bytes=%d", index.FileCount(), index.IndexedBytes())
	}
}
