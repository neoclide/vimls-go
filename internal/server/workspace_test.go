package server

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestWorkspaceFoldersOverrideRootURIAndBuildSymbolIndex(t *testing.T) {
	root := t.TempDir()
	folder := t.TempDir()
	writeWorkspaceFile(t, root, "root.vim", "vim9script\nvar rootOnly = 1\n")
	writeWorkspaceFile(t, folder, "folder.vim", "vim9script\nvar prefix = '𐐀' | var folderOnly = 1\n")
	rootURI := uri.File(root)
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	result, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		WorkspaceFoldersInitializeParams: protocol.WorkspaceFoldersInitializeParams{WorkspaceFolders: protocol.NewNullable([]protocol.WorkspaceFolder{{URI: uri.File(folder), Name: "folder"}})},
		RootURI:                          &rootURI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Capabilities.WorkspaceSymbolProvider == nil || result.Capabilities.Workspace == nil || result.Capabilities.Workspace.WorkspaceFolders == nil {
		t.Fatalf("workspace capabilities = %#v", result.Capabilities)
	}
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()

	symbols := workspaceSymbols(t, instance, "fdo")
	if len(symbols) != 1 || symbols[0].Name != "folderOnly" {
		t.Fatalf("folder symbols = %#v", symbols)
	}
	location, ok := symbols[0].Location.(*protocol.Location)
	if !ok || location.URI != uri.File(filepath.Join(folder, "folder.vim")) || location.Range.Start != (protocol.Position{Line: 1, Character: 24}) || location.Range.End != (protocol.Position{Line: 1, Character: 34}) {
		t.Fatalf("folder location = %#v", symbols[0].Location)
	}
	if symbols := workspaceSymbols(t, instance, "rootOnly"); len(symbols) != 0 {
		t.Fatalf("rootUri leaked through workspaceFolders precedence: %#v", symbols)
	}
}

func TestOpenDocumentsOverrideDiskAndClientFileEventsRebuild(t *testing.T) {
	root := t.TempDir()
	path := writeWorkspaceFile(t, root, "overlay.vim", "vim9script\nvar diskName = 1\n")
	instance := initializeWorkspaceServer(t, root)
	documentURI := uri.File(path)

	if symbols := workspaceSymbols(t, instance, "diskName"); len(symbols) != 1 {
		t.Fatalf("initial disk symbols = %#v", symbols)
	}
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: "vim9script\nvar openName = 1\n",
	}}); err != nil {
		t.Fatal(err)
	}
	if symbols := waitForWorkspaceSymbols(t, instance, "openName", 1); len(symbols) != 1 {
		t.Fatalf("open symbols = %#v", symbols)
	}
	if symbols := workspaceSymbols(t, instance, "diskName"); len(symbols) != 0 {
		t.Fatalf("disk symbols remained under open overlay: %#v", symbols)
	}
	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: "vim9script\nvar changedName = 1\n"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if symbols := waitForWorkspaceSymbols(t, instance, "changedName", 1); len(symbols) != 1 {
		t.Fatalf("changed symbols = %#v", symbols)
	}
	if err := instance.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}}); err != nil {
		t.Fatal(err)
	}
	if symbols := workspaceSymbols(t, instance, "diskName"); len(symbols) != 1 {
		t.Fatalf("restored disk symbols = %#v", symbols)
	}

	writeWorkspaceFile(t, root, "overlay.vim", "vim9script\nvar watchedName = 1\n")
	if err := instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{Changes: []protocol.FileEvent{{URI: documentURI, Type: protocol.FileChangeTypeChanged}}}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	if symbols := workspaceSymbols(t, instance, "watchedName"); len(symbols) != 1 {
		t.Fatalf("watched symbols = %#v", symbols)
	}
	if symbols := workspaceSymbols(t, instance, "diskName"); len(symbols) != 0 {
		t.Fatalf("stale disk symbols after client event = %#v", symbols)
	}
}

func TestWorkspaceFolderChangesReplaceIndexAtomically(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeWorkspaceFile(t, first, "first.vim", "var firstName = 1\n")
	writeWorkspaceFile(t, second, "second.vim", "var secondName = 1\n")
	instance := initializeWorkspaceServer(t, first)
	if err := instance.DidChangeWorkspaceFolders(context.Background(), &protocol.DidChangeWorkspaceFoldersParams{Event: protocol.WorkspaceFoldersChangeEvent{
		Removed: []protocol.WorkspaceFolder{{URI: uri.File(first), Name: "first"}},
		Added:   []protocol.WorkspaceFolder{{URI: uri.File(second), Name: "second"}},
	}}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	if symbols := workspaceSymbols(t, instance, "firstName"); len(symbols) != 0 {
		t.Fatalf("removed folder symbols = %#v", symbols)
	}
	if symbols := workspaceSymbols(t, instance, "secondName"); len(symbols) != 1 {
		t.Fatalf("added folder symbols = %#v", symbols)
	}
}

func TestWorkspaceSymbolsHonorCancellation(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := instance.Symbols(ctx, &protocol.WorkspaceSymbolParams{}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Fatalf("workspace symbol error = %v, want request cancelled", err)
	}
}

func initializeWorkspaceServer(t *testing.T, root string) *Server {
	t.Helper()
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	rootURI := uri.File(root)
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{RootURI: &rootURI}); err != nil {
		t.Fatal(err)
	}
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	return instance
}

func workspaceSymbols(t *testing.T, instance *Server, query string) protocol.WorkspaceSymbolSlice {
	t.Helper()
	result, err := instance.Symbols(context.Background(), &protocol.WorkspaceSymbolParams{Query: query})
	if err != nil {
		t.Fatal(err)
	}
	symbols, ok := result.(protocol.WorkspaceSymbolSlice)
	if !ok {
		t.Fatalf("workspace symbol result = %#v", result)
	}
	return symbols
}

func waitForWorkspaceSymbols(t *testing.T, instance *Server, query string, count int) protocol.WorkspaceSymbolSlice {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		symbols := workspaceSymbols(t, instance, query)
		if len(symbols) == count {
			return symbols
		}
		if time.Now().After(deadline) {
			t.Fatalf("workspace symbols for %q: got %d, want %d: %#v", query, len(symbols), count, symbols)
		}
		time.Sleep(time.Millisecond)
	}
}

func writeWorkspaceFile(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
