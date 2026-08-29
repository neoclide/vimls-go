package workspace

import (
	"errors"
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
	if len(class) != 1 || class[0].Path != filepath.Clean(path) || class[0].Kind != analysis.SymbolKindClass {
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
	if len(got) != 3 || got[0].Path != filepath.Clean(second) || got[1].Path != filepath.Clean(second) || got[2].Path != filepath.Clean(first) {
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
