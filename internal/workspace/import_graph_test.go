package workspace

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestImportGraphChainDiamondAndCycle(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.vim")
	b := filepath.Join(root, "b.vim")
	c := filepath.Join(root, "c.vim")
	d := filepath.Join(root, "d.vim")
	graph := NewImportGraph()
	replaceGraphImports(t, graph, a,
		ImportFact{Target: b, ImportPath: "'./b.vim'", PathSpan: syntax.Span{Start: 7, End: 16}, Alias: "B", AliasSpan: syntax.Span{Start: 20, End: 21}},
		ImportFact{Target: c, ImportPath: "'./c.vim'", PathSpan: syntax.Span{Start: 30, End: 39}, Alias: "C"},
	)
	replaceGraphImports(t, graph, b, ImportFact{Target: d, ImportPath: "'./d.vim'", Alias: "D"})
	replaceGraphImports(t, graph, c, ImportFact{Target: d, ImportPath: "'./d.vim'", Alias: "D", Autoload: true})
	replaceGraphImports(t, graph, d, ImportFact{Target: a, ImportPath: "'./a.vim'", Alias: "A"})
	graph.SetReady(true)
	snapshot := graph.Snapshot()

	if !snapshot.Ready() || len(snapshot.Outgoing(a)) != 2 || len(snapshot.Incoming(a)) != 1 {
		t.Fatalf("chain/cycle state: ready=%t outgoing(A)=%#v incoming(A)=%#v", snapshot.Ready(), snapshot.Outgoing(a), snapshot.Incoming(a))
	}
	incomingD := snapshot.Incoming(d)
	if len(incomingD) != 2 || filepath.Base(incomingD[0].Importer) != "b.vim" || filepath.Base(incomingD[1].Importer) != "c.vim" {
		t.Fatalf("diamond reverse edges = %#v", incomingD)
	}
	first := snapshot.Outgoing(a)[0]
	if first.ImportPath != "'./b.vim'" || first.PathSpan != (syntax.Span{Start: 7, End: 16}) || first.Alias != "B" || first.AliasSpan != (syntax.Span{Start: 20, End: 21}) || first.Autoload {
		t.Fatalf("retained import metadata = %#v", first)
	}
}

func TestImportGraphReplaceUpdatesReverseEdgesAndKeepsOldSnapshot(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.vim")
	b := filepath.Join(root, "b.vim")
	c := filepath.Join(root, "c.vim")
	d := filepath.Join(root, "d.vim")
	graph := NewImportGraph()
	replaceGraphImports(t, graph, a, ImportFact{Target: b}, ImportFact{Target: c})
	old := graph.Snapshot()
	replaceGraphImports(t, graph, a, ImportFact{Target: d})
	current := graph.Snapshot()

	if len(old.Outgoing(a)) != 2 || len(old.Incoming(b)) != 1 || len(old.Incoming(c)) != 1 || len(old.Incoming(d)) != 0 {
		t.Fatalf("old snapshot changed: outgoing=%#v B=%#v C=%#v D=%#v", old.Outgoing(a), old.Incoming(b), old.Incoming(c), old.Incoming(d))
	}
	if got := current.Outgoing(a); len(got) != 1 || sameGraphPath(got[0].Target, d) == false {
		t.Fatalf("replacement outgoing = %#v", got)
	}
	if len(current.Incoming(b)) != 0 || len(current.Incoming(c)) != 0 || len(current.Incoming(d)) != 1 {
		t.Fatalf("replacement reverse edges: B=%#v C=%#v D=%#v", current.Incoming(b), current.Incoming(c), current.Incoming(d))
	}
}

func TestImportGraphRemoveClearsIncidentEdges(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.vim")
	b := filepath.Join(root, "b.vim")
	c := filepath.Join(root, "c.vim")
	graph := NewImportGraph()
	replaceGraphImports(t, graph, a, ImportFact{Target: b, ImportPath: "'./b.vim'"})
	replaceGraphImports(t, graph, b, ImportFact{Target: c, ImportPath: "'./c.vim'"})
	replaceGraphImports(t, graph, c)
	graph.Remove(b)
	snapshot := graph.Snapshot()

	if snapshot.Has(b) || len(snapshot.Outgoing(b)) != 0 || len(snapshot.Incoming(b)) != 0 || len(snapshot.Incoming(c)) != 0 {
		t.Fatalf("removed node retained edges: has=%t out=%#v in=%#v C=%#v", snapshot.Has(b), snapshot.Outgoing(b), snapshot.Incoming(b), snapshot.Incoming(c))
	}
	imports := snapshot.Imports(a)
	if len(imports) != 1 || imports[0].Target != "" || imports[0].Dynamic || imports[0].ImportPath != "'./b.vim'" {
		t.Fatalf("incoming import was not retained as unresolved: %#v", imports)
	}
}

func TestImportGraphRetainsDynamicAndMissingImportsWithoutEdges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.vim")
	graph := NewImportGraph()
	replaceGraphImports(t, graph, path,
		ImportFact{Target: filepath.Join(root, "ignored.vim"), ImportPath: "name", Dynamic: true},
		ImportFact{ImportPath: "'./missing.vim'", PathSpan: syntax.Span{Start: 10, End: 25}, Missing: true},
	)
	graph.SetReady(true)
	snapshot := graph.Snapshot()
	imports := snapshot.Imports(path)
	if !snapshot.Ready() || len(imports) != 2 || !imports[0].Dynamic || imports[0].Missing || imports[1].Dynamic || !imports[1].Missing || imports[0].Target != "" || imports[1].Target != "" || len(snapshot.Outgoing(path)) != 0 {
		t.Fatalf("unknown imports: ready=%t imports=%#v outgoing=%#v", snapshot.Ready(), imports, snapshot.Outgoing(path))
	}
}

func TestImportGraphUsesCanonicalRealpathIdentity(t *testing.T) {
	realRoot := t.TempDir()
	aliasRoot := filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	realImporter := filepath.Join(realRoot, "main.vim")
	realTarget := filepath.Join(realRoot, "lib.vim")
	for _, path := range []string{realImporter, realTarget} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	aliasImporter := filepath.Join(aliasRoot, "main.vim")
	aliasTarget := filepath.Join(aliasRoot, "lib.vim")
	graph := NewImportGraph()
	replaceGraphImports(t, graph, aliasImporter, ImportFact{Target: aliasTarget})
	snapshot := graph.Snapshot()
	if !snapshot.Has(realImporter) || !snapshot.Has(realTarget) || len(snapshot.Outgoing(realImporter)) != 1 || len(snapshot.Incoming(realTarget)) != 1 {
		t.Fatalf("canonical graph state: importer=%t target=%t outgoing=%#v incoming=%#v", snapshot.Has(realImporter), snapshot.Has(realTarget), snapshot.Outgoing(realImporter), snapshot.Incoming(realTarget))
	}
	replaceGraphImports(t, graph, realImporter)
	if got := graph.Snapshot(); len(got.Outgoing(aliasImporter)) != 0 || len(got.Incoming(aliasTarget)) != 0 {
		t.Fatalf("realpath replacement left alias edges: outgoing=%#v incoming=%#v", got.Outgoing(aliasImporter), got.Incoming(aliasTarget))
	}
}

func TestImportGraphRevisionReadyAndSnapshotIsolation(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.vim")
	b := filepath.Join(root, "b.vim")
	graph := NewImportGraph()
	if graph.Revision() != 0 || graph.Ready() {
		t.Fatalf("initial state: revision=%d ready=%t", graph.Revision(), graph.Ready())
	}
	replaceGraphImports(t, graph, a, ImportFact{Target: b, Alias: "B"})
	first := graph.Snapshot()
	if first.Revision() != 1 || first.Ready() {
		t.Fatalf("first snapshot: revision=%d ready=%t", first.Revision(), first.Ready())
	}
	replaceGraphImports(t, graph, a, ImportFact{Target: b, Alias: "B"})
	graph.SetReady(true)
	if graph.Revision() != 3 || !graph.Ready() {
		t.Fatalf("updated graph: revision=%d ready=%t", graph.Revision(), graph.Ready())
	}
	graph.SetReady(true)
	graph.Remove(filepath.Join(root, "absent.vim"))
	if graph.Revision() != 3 {
		t.Fatalf("no-op changes advanced revision to %d", graph.Revision())
	}
	graph.AdvanceRevision(9)
	if graph.Revision() != 10 {
		t.Fatalf("advanced revision = %d", graph.Revision())
	}

	outgoing := first.Outgoing(a)
	outgoing[0].Alias = "mutated"
	if got := first.Outgoing(a); len(got) != 1 || got[0].Alias != "B" || first.Revision() != 1 || first.Ready() {
		t.Fatalf("snapshot leaked mutation or later state: %#v revision=%d ready=%t", got, first.Revision(), first.Ready())
	}
	if current := graph.Snapshot(); current.Revision() != 10 || !current.Ready() || current.Outgoing(a)[0].Alias != "B" {
		t.Fatalf("current snapshot = %#v", current)
	}
}

func TestImportGraphRevisionDoesNotWrap(t *testing.T) {
	graph := NewImportGraph()
	graph.revision = math.MaxUint64 - 1
	replaceGraphImports(t, graph, filepath.Join(t.TempDir(), "main.vim"))
	if graph.Revision() != math.MaxUint64 {
		t.Fatalf("revision = %d, want max uint64", graph.Revision())
	}
	graph.SetReady(true)
	graph.AdvanceRevision(math.MaxUint64)
	if graph.Revision() != math.MaxUint64 {
		t.Fatalf("revision wrapped to %d", graph.Revision())
	}
}

func replaceGraphImports(t *testing.T, graph *ImportGraph, importer string, imports ...ImportFact) {
	t.Helper()
	if err := graph.Replace(importer, imports); err != nil {
		t.Fatal(err)
	}
}

func sameGraphPath(left, right string) bool {
	leftCanonical, leftErr := CanonicalPath(left)
	rightCanonical, rightErr := CanonicalPath(right)
	return leftErr == nil && rightErr == nil && leftCanonical == rightCanonical
}
