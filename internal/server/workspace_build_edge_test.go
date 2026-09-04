package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/jsonrpc"
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
	for _, number := range []int{0, 42, 100} {
		writeWorkspaceFile(t, root, fmt.Sprintf("file_%03d.vim", number), fmt.Sprintf("vim9script\nexport var val_%03d = %d\n", number, number))
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
	instance.testHooks.discoverWorkspaceFiles = func(ctx context.Context, r string, limit int) ([]string, bool, error) {
		discovered = true
		return nil, false, errors.New("discovery forbidden during incremental change")
	}

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
	instance.testHooks.discoverWorkspaceFiles = func(ctx context.Context, r string, limit int) ([]string, bool, error) {
		discovered = true
		return nil, false, errors.New("discovery forbidden")
	}

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
	instance.testHooks.discoverWorkspaceFiles = func(ctx context.Context, r string, limit int) ([]string, bool, error) {
		discovered = true
		return nil, false, errors.New("discovery forbidden")
	}

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
	instance.testHooks.discoverWorkspaceFiles = func(ctx context.Context, r string, limit int) ([]string, bool, error) {
		discovered = true
		select {
		case discoveryRan <- struct{}{}:
		default:
		}
		return workspace.DiscoverFilesContext(ctx, r, limit)
	}

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

func TestWatchedFilesCreatedResolvesMissingImportEquivalence(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()

	setup := func(root string) (string, string) {
		mainFile := writeWorkspaceFile(t, root, "main.vim", "vim9script\nimport 'later.vim' as Later\necho Later.exportedVal\n")
		laterDir := filepath.Join(root, "import")
		_ = os.MkdirAll(laterDir, 0755)
		laterFile := filepath.Join(laterDir, "later.vim")
		return mainFile, laterFile
	}
	main1, later1 := setup(root1)
	main2, later2 := setup(root2)

	// Server 1: initialized before later.vim exists
	s1 := initializeWorkspaceServer(t, root1)
	s1.workspaceWG.Wait()

	// Initial state in s1: main.vim has Missing: true, Target: ""
	s1.workspaceMu.Lock()
	facts1 := s1.workspaceGraphView.Imports(main1)
	s1.workspaceMu.Unlock()
	if len(facts1) != 1 || !facts1[0].Missing || facts1[0].Target != "" {
		t.Fatalf("expected initial missing import fact, got %#v", facts1)
	}

	// Create later.vim on disk in both roots
	writeWorkspaceFile(t, filepath.Dir(later1), "later.vim", "vim9script\nexport var exportedVal = 42\n")
	writeWorkspaceFile(t, filepath.Dir(later2), "later.vim", "vim9script\nexport var exportedVal = 42\n")

	// Server 1 receives Created watched event
	err := s1.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{
			{URI: uri.File(later1), Type: protocol.FileChangeTypeCreated},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s1.workspaceWG.Wait()

	// Server 2: initialized fresh (full rebuild) from root2
	s2 := initializeWorkspaceServer(t, root2)
	s2.workspaceWG.Wait()

	// 1. Compare ImportGraph facts between s1 and s2
	s1.workspaceMu.Lock()
	after1 := s1.workspaceGraphView.Imports(main1)
	s1.workspaceMu.Unlock()

	s2.workspaceMu.Lock()
	after2 := s2.workspaceGraphView.Imports(main2)
	s2.workspaceMu.Unlock()

	if len(after1) != 1 || after1[0].Missing || after1[0].Target == "" {
		t.Fatalf("s1 after create: expected resolved import, got %#v", after1)
	}
	if len(after2) != 1 || after2[0].Missing || after2[0].Target == "" {
		t.Fatalf("s2 after create: expected resolved import, got %#v", after2)
	}
	if filepath.Base(after1[0].Target) != filepath.Base(after2[0].Target) {
		t.Fatalf("target mismatch: %s vs %s", after1[0].Target, after2[0].Target)
	}

	// 2. Compare diagnostics between s1 and s2 for main.vim
	rep1, err := s1.Diagnostic(context.Background(), &protocol.DocumentDiagnosticParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri.File(main1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	rep2, err := s2.Diagnostic(context.Background(), &protocol.DocumentDiagnosticParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri.File(main2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	full1, ok1 := rep1.(*protocol.RelatedFullDocumentDiagnosticReport)
	full2, ok2 := rep2.(*protocol.RelatedFullDocumentDiagnosticReport)
	if !ok1 || !ok2 {
		t.Fatalf("expected full reports, got %T, %T", rep1, rep2)
	}
	if len(full1.Items) != len(full2.Items) {
		t.Fatalf("diagnostics count mismatch: %d vs %d", len(full1.Items), len(full2.Items))
	}
}

func TestWatchedFilesReadLoopNotBlocked(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { _ = serverConn.Close() })
	t.Cleanup(func() { _ = clientConn.Close() })

	instance := New(serverConn, serverConn, io.Discard)
	root := t.TempDir()
	filePath := filepath.Join(root, "missing.vim")

	started := make(chan struct{})
	release := make(chan struct{})
	indexed := make(chan struct{})

	instance.testHooks.beforeWatchedFileProcess = func(p string) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
	}
	instance.testHooks.afterWorkspaceIndexWorker = func() { close(indexed) }

	done := make(chan int, 1)
	go func() { done <- instance.Run(context.Background()) }()

	writer := jsonrpc.NewWriter(clientConn)
	reader := jsonrpc.NewReader(clientConn)

	writeFrame(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"workspaceFolders":[{"uri":%q,"name":"root"}],"initializationOptions":{"runtimepath":[]}}}`, uri.File(root)))
	if msg := readFrame(t, reader); idNumber(t, msg) != 1 {
		t.Fatalf("initialize response = %#v", msg)
	}
	instance.workspaceMu.Lock()
	instance.workspaceDelay = 0
	instance.workspaceMu.Unlock()
	writeFrame(t, writer, `{"jsonrpc":"2.0","method":"initialized","params":{}}`)
	select {
	case <-indexed:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for initial empty workspace index")
	}

	// Send didChangeWatchedFiles notification
	writeFrame(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","method":"workspace/didChangeWatchedFiles","params":{"changes":[{"uri":%q,"type":3}]}}`, uri.File(filePath)))

	// Wait until watched-file processing has blocked at the hook
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for watched file processing to start")
	}

	// While watched-file processing is still paused at the hook, send a shutdown request.
	// Because workspace/didChangeWatchedFiles is async, the read loop must process shutdown!
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- writer.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`))
	}()
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("write shutdown failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out writing shutdown request (read loop blocked)")
	}

	// Unblock watched-file processing so shutdown can complete waiting for background work
	close(release)

	msg := readFrame(t, reader)
	if idNumber(t, msg) != 2 {
		t.Fatalf("expected shutdown response id=2, got %#v", msg)
	}

	writeFrame(t, writer, `{"jsonrpc":"2.0","method":"exit"}`)
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not exit")
	}
}

func TestDidChangeWatchedFilesContextCancellation(t *testing.T) {
	root := t.TempDir()
	p1 := writeWorkspaceFile(t, root, "f1.vim", "vim9script\nvar x = 1\n")
	p2 := writeWorkspaceFile(t, root, "f2.vim", "vim9script\nvar y = 2\n")

	instance := initializeWorkspaceServer(t, root)
	instance.workspaceWG.Wait()

	started := make(chan struct{})
	release := make(chan struct{})
	instance.testHooks.beforeWatchedFileProcess = func(p string) {
		if filepath.Base(p) == "f1.vim" {
			select {
			case <-started:
			default:
				close(started)
			}
			<-release
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- instance.DidChangeWatchedFiles(ctx, &protocol.DidChangeWatchedFilesParams{
			Changes: []protocol.FileEvent{
				{URI: uri.File(p1), Type: protocol.FileChangeTypeChanged},
				{URI: uri.File(p2), Type: protocol.FileChangeTypeChanged},
			},
		})
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for f1.vim")
	}

	// Cancel context while blocked
	cancel()
	close(release)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DidChangeWatchedFiles did not return after cancellation")
	}
}

func TestWatchedFilesOverlayCheckPreservedAfterDidOpen(t *testing.T) {
	root := t.TempDir()
	path := writeWorkspaceFile(t, root, "overlay_race.vim", "vim9script\nvar diskVal = 1\n")
	instance := initializeWorkspaceServer(t, root)
	instance.workspaceWG.Wait()

	started := make(chan struct{})
	release := make(chan struct{})
	instance.testHooks.beforeWatchedFileInstall = func(p string) {
		if filepath.Base(p) == "overlay_race.vim" {
			select {
			case <-started:
			default:
				close(started)
			}
			<-release
		}
	}

	// Update disk file
	writeWorkspaceFile(t, root, "overlay_race.vim", "vim9script\nvar diskVal = 2\n")

	// Trigger watched files notification (runs asynchronously)
	go func() {
		_ = instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
			Changes: []protocol.FileEvent{
				{URI: uri.File(path), Type: protocol.FileChangeTypeChanged},
			},
		})
	}()

	// Wait until watched-file processing has passed initial checks and blocked at install hook
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for watched file processing to reach install hook")
	}

	// In the meantime, user opens an overlay for the file
	docURI := uri.File(path)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: docURI, Version: 1, Text: "vim9script\nvar overlayVal = 42\n"},
	}); err != nil {
		t.Fatal(err)
	}

	if len(waitForWorkspaceSymbols(t, instance, "overlayVal", 1)) != 1 {
		t.Fatal("overlayVal not indexed")
	}

	// Release watched handler to proceed with its disk install check
	close(release)
	instance.watchWG.Wait()

	// Assert overlay was NOT overwritten by disk result
	if len(workspaceSymbols(t, instance, "overlayVal")) != 1 {
		t.Fatal("overlayVal was overwritten by disk result")
	}
	if len(workspaceSymbols(t, instance, "diskVal")) != 0 {
		t.Fatal("diskVal should not be indexed while overlay is open")
	}
}

func TestWatchedFilesOverlayCheckPreservedOnDeleteEvent(t *testing.T) {
	root := t.TempDir()
	path := writeWorkspaceFile(t, root, "overlay_del.vim", "vim9script\nvar initVal = 1\n")
	instance := initializeWorkspaceServer(t, root)
	instance.workspaceWG.Wait()

	started := make(chan struct{})
	release := make(chan struct{})
	instance.testHooks.beforeWatchedFileInstall = func(p string) {
		if filepath.Base(p) == "overlay_del.vim" {
			select {
			case <-started:
			default:
				close(started)
			}
			<-release
		}
	}

	// Remove file on disk
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	// Trigger delete event
	go func() {
		_ = instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
			Changes: []protocol.FileEvent{
				{URI: uri.File(path), Type: protocol.FileChangeTypeDeleted},
			},
		})
	}()

	// Wait until delete handler has blocked at the install hook (os.Stat confirmed nonexistence)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for delete handler to reach install hook")
	}

	// User opens an overlay for the deleted file while delete handler is blocked
	docURI := uri.File(path)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: docURI, Version: 1, Text: "vim9script\nvar overlayDelVal = 99\n"},
	}); err != nil {
		t.Fatal(err)
	}

	if len(waitForWorkspaceSymbols(t, instance, "overlayDelVal", 1)) != 1 {
		t.Fatal("overlayDelVal not indexed")
	}

	// Unblock delete handler to attempt removing facts
	close(release)
	instance.watchWG.Wait()

	// Overlay must NOT be deleted by the stale delete event
	if len(workspaceSymbols(t, instance, "overlayDelVal")) != 1 {
		t.Fatal("overlay facts were cleared by stale delete event")
	}
}

func TestDidChangeWatchedFilesBurstMergesToSingleRebuild(t *testing.T) {
	root := t.TempDir()
	p := writeWorkspaceFile(t, root, "burst.vim", "vim9script\nvar burstVal = 1\n")
	instance := initializeWorkspaceServer(t, root)
	instance.workspaceWG.Wait()

	var rebuildCount atomic.Int32
	instance.testHooks.beforeWorkspaceBuild = func([]*text.Snapshot) {
		rebuildCount.Add(1)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	instance.testHooks.beforeWatchedFileProcess = func(p string) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
	}

	// 1. First event becomes active and blocks
	go func() {
		_ = instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
			Changes: []protocol.FileEvent{
				{URI: uri.File(p), Type: protocol.FileChangeTypeChanged},
			},
		})
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for active handler")
	}

	// 2. Send 20 concurrent burst notifications; all must return immediately without blocking!
	burstDone := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		for range 20 {
			wg.Go(func() {
				_ = instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
					Changes: []protocol.FileEvent{
						{URI: uri.File(p), Type: protocol.FileChangeTypeChanged},
					},
				})
			})
		}
		wg.Wait()
		close(burstDone)
	}()

	select {
	case <-burstDone:
		// Succeeded: all 20 returned immediately without blocking on the active handler
	case <-time.After(2 * time.Second):
		t.Fatal("burst notifications blocked on active handler")
	}

	// 3. Release active handler
	close(release)
	instance.watchWG.Wait()
	instance.workspaceWG.Wait()

	if count := rebuildCount.Load(); count != 1 {
		t.Fatalf("expected exactly 1 workspace rebuild for burst notifications, got %d", count)
	}
}

func TestDidChangeWatchedFilesTOCTOUFileGrowth(t *testing.T) {
	root := t.TempDir()
	filePath := writeWorkspaceFile(t, root, "grow.vim", "vim9script\nvar initial = 1\n")
	instance := initializeWorkspaceServer(t, root)
	instance.workspaceWG.Wait()

	// In the hook before processing, write oversized content
	instance.testHooks.beforeWatchedFileProcess = func(p string) {
		if filepath.Base(p) == "grow.vim" {
			_ = os.WriteFile(p, []byte(strings.Repeat("x", maxFileBytes+1)), 0644)
		}
	}

	err := instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{
			{URI: uri.File(filePath), Type: protocol.FileChangeTypeChanged},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The file should NOT be indexed with symbols because it exceeded maxFileBytes
	syms := workspaceSymbols(t, instance, "initial")
	if len(syms) != 0 {
		t.Fatalf("oversized file symbols should be cleared, got %#v", syms)
	}
}
