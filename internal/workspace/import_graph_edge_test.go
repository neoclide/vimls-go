package workspace

import (
	"math"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestImportGraphNilAndMutationBoundaries(t *testing.T) {
	var nilGraph *ImportGraph
	if nilGraph.Revision() != 0 || nilGraph.Ready() || nilGraph.Snapshot().Ready() {
		t.Fatal("nil graph did not retain its empty contract")
	}
	if err := nilGraph.Replace("a", nil); err != ErrImportGraphPath {
		t.Fatalf("nil replace error = %v", err)
	}
	nilGraph.Remove("a")
	nilGraph.SetReady(true)
	nilGraph.AdvanceRevision(2)

	graph := NewImportGraph()
	if err := graph.Replace(" ", nil); err != ErrImportGraphPath {
		t.Fatalf("empty importer error = %v", err)
	}
	if err := graph.Replace("/a.vim", []ImportFact{
		{Target: "/z.vim", ImportPath: "z", Alias: "z", PathSpan: span(3, 4)},
		{Target: "/b.vim", ImportPath: "b", Alias: "b", PathSpan: span(1, 2)},
		{Dynamic: true, Target: "/ignored.vim", ImportPath: "dynamic", Alias: "d", PathSpan: span(2, 3)},
	}); err != nil {
		t.Fatal(err)
	}
	graph.SetReady(true)
	graph.SetReady(true)
	before := graph.Revision()
	graph.AdvanceRevision(before - 1)
	if graph.Revision() != before {
		t.Fatal("revision regressed")
	}
	graph.AdvanceRevision(math.MaxUint64)
	graph.AdvanceRevision(math.MaxUint64)
	if graph.Revision() != math.MaxUint64 {
		t.Fatal("maximum revision not retained")
	}

	snapshot := graph.Snapshot()
	if !snapshot.Has("/a.vim") || !snapshot.Has("/b.vim") || snapshot.Has(" ") {
		t.Fatal("unexpected membership")
	}
	if got := snapshot.Imports(" "); got != nil {
		t.Fatalf("invalid imports = %#v", got)
	}
	imports := snapshot.Imports("/a.vim")
	if len(imports) != 3 || imports[0].ImportPath != "b" {
		t.Fatalf("imports not sorted: %#v", imports)
	}
	imports[0].Alias = "changed"
	if snapshot.Imports("/a.vim")[0].Alias == "changed" {
		t.Fatal("imports were not cloned")
	}
	if got := snapshot.Outgoing("/a.vim"); len(got) != 2 {
		t.Fatalf("outgoing = %#v", got)
	}
	if got := snapshot.Incoming("/b.vim"); len(got) != 1 {
		t.Fatalf("incoming = %#v", got)
	}
	if got := snapshot.ReverseDependents("/b.vim"); len(got) != 1 || got[0] != "/a.vim" {
		t.Fatalf("dependents = %#v", got)
	}
	if got := snapshot.ReverseDependents(" "); got != nil {
		t.Fatalf("invalid dependents = %#v", got)
	}

	graph.Remove("/b.vim")
	after := graph.Snapshot().Imports("/a.vim")
	if len(after) != 3 || !after[0].Missing || after[0].Target != "" {
		t.Fatalf("removed target was not marked missing: %#v", after)
	}
	graph.Remove("/does-not-exist")
}

func span(start, end int) (result syntax.Span) { return syntax.Span{Start: start, End: end} }
