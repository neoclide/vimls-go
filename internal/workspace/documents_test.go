package workspace

import (
	"context"
	"errors"
	"testing"

	"github.com/chemzqm/vimls-go/internal/text"
)

func TestDocumentsOpenChangeSaveCloseAndReopen(t *testing.T) {
	documents := NewDocuments()
	opened := documents.Open("file:///a.vim", 1, "let x = '𐐀'\n")
	if opened.Revision() != 1 || documents.Len() != 1 {
		t.Fatalf("open revision = %d, len = %d", opened.Revision(), documents.Len())
	}
	changed, err := documents.Change("file:///a.vim", 2, text.UTF16, []text.Change{{
		Range: &text.Range{Start: text.Position{Character: 9}, End: text.Position{Character: 11}},
		Text:  "value",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Text() != "let x = 'value'\n" || changed.Revision() != 2 {
		t.Fatalf("changed = %q revision %d", changed.Text(), changed.Revision())
	}
	if opened.Text() != "let x = '𐐀'\n" {
		t.Fatal("open snapshot changed")
	}

	if saved, err := documents.Save("file:///a.vim", nil); err != nil || saved != changed {
		t.Fatalf("save without text = %#v, %v", saved, err)
	}
	same := changed.Text()
	if saved, err := documents.Save("file:///a.vim", &same); err != nil || saved != changed {
		t.Fatalf("save same text = %#v, %v", saved, err)
	}
	replacement := "vim9script\n"
	saved, err := documents.Save("file:///a.vim", &replacement)
	if err != nil || saved.Text() != replacement || saved.Revision() != 3 {
		t.Fatalf("save replacement = %#v, %v", saved, err)
	}
	if !documents.Close("file:///a.vim") || documents.Close("file:///a.vim") || documents.Len() != 0 {
		t.Fatal("close behavior is incorrect")
	}
	reopened := documents.Open("file:///a.vim", 1, "reopened")
	if reopened.Revision() != 4 || reopened.Text() != "reopened" {
		t.Fatalf("reopened = %#v", reopened)
	}
}

func TestDocumentsRejectStaleInvalidAndMissingChanges(t *testing.T) {
	documents := NewDocuments()
	documents.Open("u", 4, "abc")
	for _, version := range []int32{3, 4} {
		if _, err := documents.Change("u", version, text.UTF16, []text.Change{{Text: "stale"}}); !errors.Is(err, ErrStaleVersion) {
			t.Fatalf("version %d error = %v", version, err)
		}
	}
	if _, err := documents.Change("missing", 1, text.UTF16, nil); !errors.Is(err, ErrDocumentNotOpen) {
		t.Fatalf("missing change error = %v", err)
	}
	if _, err := documents.Save("missing", nil); !errors.Is(err, ErrDocumentNotOpen) {
		t.Fatalf("missing save error = %v", err)
	}
	if _, err := documents.Change("u", 5, text.UTF16, []text.Change{{Range: &text.Range{Start: text.Position{Character: 9}}}}); !errors.Is(err, text.ErrInvalidPosition) {
		t.Fatalf("invalid change error = %v", err)
	}
	snapshot, _ := documents.Snapshot("u")
	if snapshot.Text() != "abc" || snapshot.Revision() != 1 {
		t.Fatalf("invalid change mutated snapshot: %#v", snapshot)
	}
}

func TestDocumentsCancelAndRejectStaleAnalysis(t *testing.T) {
	documents := NewDocuments()
	documents.Open("file:///b.vim", 1, "b")
	documents.Open("file:///a.vim", 1, "a")
	first, ok := documents.BeginAnalysis(context.Background(), "file:///a.vim")
	if !ok || !documents.IsCurrent(first) {
		t.Fatal("initial analysis is not current")
	}
	second, ok := documents.BeginAnalysis(context.Background(), "file:///a.vim")
	if !ok || first.Context.Err() == nil || !documents.IsCurrent(second) {
		t.Fatal("replacement analysis did not cancel the first")
	}
	if _, err := documents.Change("file:///a.vim", 2, text.UTF16, []text.Change{{Text: "new"}}); err != nil {
		t.Fatal(err)
	}
	if second.Context.Err() == nil || documents.IsCurrent(second) {
		t.Fatal("changed analysis remained current")
	}
	third, _ := documents.BeginAnalysis(context.Background(), "file:///a.vim")
	snapshots := documents.ConfigurationChanged()
	if third.Context.Err() == nil || documents.IsCurrent(third) {
		t.Fatal("configuration change did not invalidate analysis")
	}
	if documents.ConfigRevision() != 2 || len(snapshots) != 2 || snapshots[0].URI() != "file:///a.vim" || snapshots[1].URI() != "file:///b.vim" {
		t.Fatalf("configuration result = %d, %#v", documents.ConfigRevision(), snapshots)
	}
	fourth, _ := documents.BeginAnalysis(context.Background(), "file:///a.vim")
	documents.Close("file:///a.vim")
	if fourth.Context.Err() == nil || documents.IsCurrent(fourth) {
		t.Fatal("closed analysis remained current")
	}
	if _, ok := documents.BeginAnalysis(context.Background(), "missing"); ok {
		t.Fatal("analysis started for missing document")
	}
	if _, ok := documents.Snapshot("missing"); ok {
		t.Fatal("missing snapshot exists")
	}
}

func TestDocumentsSnapshotsAreSortedAndIndependent(t *testing.T) {
	documents := NewDocuments()
	documents.Open("file:///z.vim", 1, "z")
	documents.Open("file:///a.vim", 1, "a")
	snapshots := documents.Snapshots()
	if len(snapshots) != 2 || snapshots[0].URI() != "file:///a.vim" || snapshots[1].URI() != "file:///z.vim" {
		t.Fatalf("snapshot order = %#v", snapshots)
	}
	if _, err := documents.Change("file:///z.vim", 2, text.UTF8, []text.Change{{Text: "updated"}}); err != nil {
		t.Fatal(err)
	}
	snapshots[0] = nil
	current, ok := documents.Snapshot("file:///a.vim")
	if !ok || current == nil || current.Text() != "a" || current.Revision() != 2 {
		t.Fatalf("snapshot slice changed document state: %#v", current)
	}
	current, ok = documents.Snapshot("file:///z.vim")
	if !ok || current.Text() != "updated" || current.Revision() != 3 {
		t.Fatalf("changed snapshot = %#v", current)
	}
	snapshots = documents.Snapshots()
	if snapshots[1].URI() != "file:///z.vim" || snapshots[1].Text() != "updated" || snapshots[1].Revision() != 3 {
		t.Fatalf("current snapshots = %#v", snapshots)
	}
}
