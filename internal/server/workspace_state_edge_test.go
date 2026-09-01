package server

import (
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestWorkspaceIncrementalStateBoundaries(t *testing.T) {
	root := t.TempDir()
	path := writeWorkspaceFile(t, root, "state.vim", "vim9script\nexport var value = 1\n")
	s := New(nil, nil, nil)
	t.Cleanup(s.stopAnalysis)
	s.setWorkspaceRoots([]string{root})
	file := syntax.Parse("vim9script\nexport var value = 1\n")
	// First installation creates facts; a matching replacement is a no-op and
	// a nil replacement removes the retained workspace file.
	first, dependents := s.replaceWorkspaceFileWithSnapshot(canonicalTestURI(t, path).String(), file)
	if first.path == "" || len(dependents) != 0 {
		t.Fatalf("first snapshot = %#v, dependents=%#v", first, dependents)
	}
	second, dependents := s.replaceWorkspaceFileWithSnapshot(canonicalTestURI(t, path).String(), file)
	if second.path != first.path || len(dependents) != 0 {
		t.Fatalf("same replacement = %#v, %#v", second, dependents)
	}
	removed, dependents := s.replaceWorkspaceFileWithSnapshot(canonicalTestURI(t, path).String(), nil)
	if removed.path == "" || len(dependents) != 0 {
		t.Fatalf("removed snapshot = %#v, %#v", removed, dependents)
	}
	// Paths outside configured roots and malformed URIs never enter the index.
	if got := s.replaceWorkspaceFile("untitled:buffer", file); got != nil {
		t.Fatalf("non-file replacement = %#v", got)
	}
	s.removeWorkspaceURI("untitled:buffer")
	if restore, ok := s.captureWorkspaceRestore("untitled:buffer"); ok || restore.path != "" {
		t.Fatalf("invalid restore = %#v, %v", restore, ok)
	}
}
