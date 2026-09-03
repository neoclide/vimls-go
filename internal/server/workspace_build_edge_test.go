package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
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

func TestWatchedFilesIncrementalSingleFileChangeNoDiscovery(t *testing.T) {
	root := t.TempDir()
	const numFiles = 256
	for i := 0; i < numFiles; i++ {
		writeWorkspaceFile(t, root, fmt.Sprintf("file_%03d.vim", i), fmt.Sprintf("vim9script\nexport var val_%03d = %d\n", i, i))
	}
	instance := initializeWorkspaceServer(t, root)
	instance.workspaceWG.Wait()

	// Initial check: val_042 exists
	symbols := workspaceSymbols(t, instance, "val_042")
	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol for val_042, got %d", len(symbols))
	}

	// Forbid discovery during incremental update
	discovered := false
	prevHook := workspaceDiscoverFilesContextForTest
	workspaceDiscoverFilesContextForTest = func(ctx context.Context, r string, limit int) ([]string, bool, error) {
		discovered = true
		return nil, false, errors.New("discovery forbidden during incremental change")
	}
	t.Cleanup(func() {
		workspaceDiscoverFilesContextForTest = prevHook
	})

	// Change file_042 on disk
	filePath := filepath.Join(root, "file_042.vim")
	writeWorkspaceFile(t, root, "file_042.vim", "vim9script\nexport var different_item_042 = 9999\n")

	err := instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{
			{URI: uri.File(filePath), Type: protocol.FileChangeTypeChanged},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if discovered {
		t.Fatal("full directory discovery was triggered for single watched file change")
	}

	// Verify updated symbol is present
	newSymbols := workspaceSymbols(t, instance, "different_item_042")
	if len(newSymbols) != 1 {
		t.Fatalf("expected different_item_042 to be indexed, got %d", len(newSymbols))
	}
	// Verify old symbol is gone
	oldSymbols := workspaceSymbols(t, instance, "val_042")
	if len(oldSymbols) != 0 {
		t.Fatalf("expected old val_042 to be removed, got %d: %#v", len(oldSymbols), oldSymbols)
	}
	// Verify other files remain indexed
	otherSymbols := workspaceSymbols(t, instance, "val_100")
	if len(otherSymbols) != 1 {
		t.Fatalf("expected val_100 to remain indexed, got %d", len(otherSymbols))
	}
}

func TestWatchedFilesIncrementalCreateAndDelete(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "base.vim", "vim9script\nexport var baseVal = 1\n")
	instance := initializeWorkspaceServer(t, root)
	instance.workspaceWG.Wait()

	discovered := false
	prevHook := workspaceDiscoverFilesContextForTest
	workspaceDiscoverFilesContextForTest = func(ctx context.Context, r string, limit int) ([]string, bool, error) {
		discovered = true
		return nil, false, errors.New("discovery forbidden")
	}
	t.Cleanup(func() {
		workspaceDiscoverFilesContextForTest = prevHook
	})

	// 1. Create new file
	newPath := filepath.Join(root, "created.vim")
	writeWorkspaceFile(t, root, "created.vim", "vim9script\nexport var createdVal = 2\n")

	err := instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{
			{URI: uri.File(newPath), Type: protocol.FileChangeTypeCreated},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if discovered {
		t.Fatal("discovery triggered on create")
	}
	if len(workspaceSymbols(t, instance, "createdVal")) != 1 {
		t.Fatal("createdVal not indexed")
	}

	// 2. Delete file
	if err := os.Remove(newPath); err != nil {
		t.Fatal(err)
	}
	err = instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{
			{URI: uri.File(newPath), Type: protocol.FileChangeTypeDeleted},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if discovered {
		t.Fatal("discovery triggered on delete")
	}
	if len(workspaceSymbols(t, instance, "createdVal")) != 0 {
		t.Fatal("createdVal still indexed after delete")
	}
	if len(workspaceSymbols(t, instance, "baseVal")) != 1 {
		t.Fatal("baseVal lost after delete")
	}
}

func TestWatchedFilesIncrementalAtomicSaveAndBurst(t *testing.T) {
	root := t.TempDir()
	path := writeWorkspaceFile(t, root, "atomic.vim", "vim9script\nvar initial = 1\n")
	instance := initializeWorkspaceServer(t, root)
	instance.workspaceWG.Wait()

	discovered := false
	prevHook := workspaceDiscoverFilesContextForTest
	workspaceDiscoverFilesContextForTest = func(ctx context.Context, r string, limit int) ([]string, bool, error) {
		discovered = true
		return nil, false, errors.New("discovery forbidden")
	}
	t.Cleanup(func() {
		workspaceDiscoverFilesContextForTest = prevHook
	})

	// Write new content to atomic.vim
	writeWorkspaceFile(t, root, "atomic.vim", "vim9script\nvar atomicReplaced = 2\n")

	// Client sends burst: Delete followed by Create
	err := instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{
			{URI: uri.File(path), Type: protocol.FileChangeTypeDeleted},
			{URI: uri.File(path), Type: protocol.FileChangeTypeCreated},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if discovered {
		t.Fatal("discovery triggered during atomic save burst")
	}
	if len(workspaceSymbols(t, instance, "atomicReplaced")) != 1 {
		t.Fatal("atomicReplaced not indexed")
	}
	if len(workspaceSymbols(t, instance, "initial")) != 0 {
		t.Fatal("initial still indexed")
	}
}

func TestWatchedFilesDirectoryAndInvalidFallback(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "init.vim", "vim9script\nvar initVal = 1\n")
	instance := initializeWorkspaceServer(t, root)
	instance.workspaceWG.Wait()

	// Subdirectory created
	subDir := filepath.Join(root, "subdir")
	_ = os.Mkdir(subDir, 0755)

	discovered := false
	discoveryRan := make(chan struct{}, 1)
	prevHook := workspaceDiscoverFilesContextForTest
	workspaceDiscoverFilesContextForTest = func(ctx context.Context, r string, limit int) ([]string, bool, error) {
		discovered = true
		select {
		case discoveryRan <- struct{}{}:
		default:
		}
		return workspace.DiscoverFilesContext(ctx, r, limit)
	}
	t.Cleanup(func() {
		workspaceDiscoverFilesContextForTest = prevHook
	})

	// Sending watched event for directory must fall back to full rebuild
	err := instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{
			{URI: uri.File(subDir), Type: protocol.FileChangeTypeCreated},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-discoveryRan:
	case <-time.After(10 * time.Second):
		t.Fatal("expected full rebuild discovery to run on directory event")
	}
	if !discovered {
		t.Fatal("expected full rebuild on directory event")
	}
}

func TestWatchedFilesOpenOverlayPreserved(t *testing.T) {
	root := t.TempDir()
	path := writeWorkspaceFile(t, root, "mod.vim", "vim9script\nvar diskVal = 1\n")
	instance := initializeWorkspaceServer(t, root)
	instance.workspaceWG.Wait()

	// Open overlay
	docURI := uri.File(path)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: docURI, Version: 1, Text: "vim9script\nvar overlayVal = 2\n"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(waitForWorkspaceSymbols(t, instance, "overlayVal", 1)) != 1 {
		t.Fatal("overlayVal not indexed")
	}

	// Change file on disk to thirdVal
	writeWorkspaceFile(t, root, "mod.vim", "vim9script\nvar thirdDiskVal = 3\n")

	// Send watched event from client
	err := instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{
			{URI: docURI, Type: protocol.FileChangeTypeChanged},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Overlay must remain active and not overwritten by disk
	if len(workspaceSymbols(t, instance, "overlayVal")) != 1 {
		t.Fatal("overlayVal lost after disk watched event")
	}
	if len(workspaceSymbols(t, instance, "thirdDiskVal")) != 0 {
		t.Fatal("thirdDiskVal leaked into overlay")
	}
}

func TestWatchedFilesIncrementalEquivalenceWithFullRebuild(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()

	setup := func(root string) (string, string) {
		p1 := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport var exportedVal = 100\n")
		p2 := writeWorkspaceFile(t, root, "main.vim", "vim9script\nimport './lib.vim' as Lib\necho Lib.exportedVal\n")
		return p1, p2
	}
	lib1, _ := setup(root1)
	setup(root2)

	// Server 1: will receive incremental update
	s1 := initializeWorkspaceServer(t, root1)
	s1.workspaceWG.Wait()

	// Update lib.vim in both roots
	writeWorkspaceFile(t, root1, "lib.vim", "vim9script\nexport var updatedVal = 200\n")
	writeWorkspaceFile(t, root2, "lib.vim", "vim9script\nexport var updatedVal = 200\n")

	// Server 1 gets incremental watched event
	err := s1.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{
			{URI: uri.File(lib1), Type: protocol.FileChangeTypeChanged},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Server 2: initialized fresh (full rebuild) from root2
	s2 := initializeWorkspaceServer(t, root2)
	s2.workspaceWG.Wait()

	// Compare symbols
	syms1 := workspaceSymbols(t, s1, "")
	syms2 := workspaceSymbols(t, s2, "")
	if len(syms1) != len(syms2) {
		t.Fatalf("symbol count mismatch: incremental=%d full=%d", len(syms1), len(syms2))
	}
	names1 := make([]string, len(syms1))
	names2 := make([]string, len(syms2))
	for i := range syms1 {
		names1[i] = syms1[i].Name
		names2[i] = syms2[i].Name
	}
	slices.Sort(names1)
	slices.Sort(names2)
	if !slices.Equal(names1, names2) {
		t.Fatalf("symbols mismatch: %v vs %v", names1, names2)
	}
}
