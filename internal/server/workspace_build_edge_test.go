package server

import (
	"context"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
)

func TestBuildWorkspaceIndexCancellationAndOpenSnapshotBoundaries(t *testing.T) {
	s := New(nil, nil, nil)
	t.Cleanup(s.stopAnalysis)
	// Empty roots and a cancelled build are both completed without attempting
	// filesystem discovery; callers receive a usable, explicitly incomplete
	// index in the latter case.
	index, graph, files, warnings := s.buildWorkspaceIndex(context.Background(), nil, nil, nil, nil)
	if !index.Complete() || !graph.Ready() || len(files) != 0 || len(warnings) != 0 {
		t.Fatalf("empty build = %#v, %#v, %#v, %#v", index, graph, files, warnings)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	index, graph, files, warnings = s.buildWorkspaceIndex(ctx, []string{t.TempDir()}, nil, nil, nil)
	if index.Complete() || graph.Ready() || len(files) != 0 || len(warnings) != 0 {
		t.Fatalf("cancelled build = %#v, %#v, %#v, %#v", index, graph, files, warnings)
	}
	// An invalid root is recoverable and gives the user a warning instead of
	// preventing indexing of other roots.
	index, graph, _, warnings = s.buildWorkspaceIndex(context.Background(), []string{"\x00invalid"}, nil, nil, nil)
	if index.Complete() || !graph.Ready() || len(warnings) == 0 {
		t.Fatalf("invalid root = %#v, %#v, %#v", index, graph, warnings)
	}

	root := t.TempDir()
	path := writeWorkspaceFile(t, root, "open.vim", "vim9script\nvar disk = 1\n")
	snapshot := text.NewSnapshot(canonicalTestURI(t, path).String(), 1, nil, "vim9script\nvar open = 2\n")
	index, graph, files, warnings = s.buildWorkspaceIndex(context.Background(), []string{root}, nil, nil, []*text.Snapshot{snapshot})
	if !index.Complete() || !graph.Ready() || len(files) != 1 || len(warnings) != 0 {
		t.Fatalf("open build state = complete:%v ready:%v files:%#v warnings:%#v", index.Complete(), graph.Ready(), files, warnings)
	}
	if len(index.FileSymbols(path)) == 0 {
		t.Fatalf("open snapshot was not indexed: %#v", index.FileSymbols(path))
	}
}
