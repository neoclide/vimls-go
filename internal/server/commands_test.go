package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type renameCommandClient struct {
	protocol.UnimplementedClient
	requests []*protocol.ApplyWorkspaceEditParams
	apply    func(context.Context) (*protocol.ApplyWorkspaceEditResult, error)
}

func (c *renameCommandClient) ApplyEdit(ctx context.Context, params *protocol.ApplyWorkspaceEditParams) (*protocol.ApplyWorkspaceEditResult, error) {
	c.requests = append(c.requests, params)
	if c.apply != nil {
		return c.apply(ctx)
	}
	return &protocol.ApplyWorkspaceEditResult{Applied: true}, nil
}

func initializeRenameCommandServer(t *testing.T, root string, runtimePaths ...string) (*Server, *renameCommandClient) {
	t.Helper()
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := &renameCommandClient{}
	instance.client = client
	rootURI := uri.File(root)
	supported := true
	if len(runtimePaths) == 0 {
		runtimePaths = []string{root}
	}
	options, err := json.Marshal(map[string]any{"runtimepath": runtimePaths})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI: &rootURI,
		Capabilities: protocol.ClientCapabilities{Workspace: &protocol.WorkspaceClientCapabilities{
			ApplyEdit: &supported, WorkspaceEdit: &protocol.WorkspaceEditClientCapabilities{DocumentChanges: &supported},
		}},
		InitializationOptions: protocol.LSPAny(options),
	}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceDelay = 0
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	instance.runtimepathWG.Wait()
	return instance, client
}

func renameCommandParams(t *testing.T, paths ...string) *protocol.ExecuteCommandParams {
	t.Helper()
	argument, err := protocol.Marshal(renameFileParams(paths...))
	if err != nil {
		t.Fatal(err)
	}
	return &protocol.ExecuteCommandParams{Command: CommandUpdateImportsOnRename, Arguments: []protocol.LSPAny{argument}}
}

func TestRenameCommandCapability(t *testing.T) {
	for _, supported := range []bool{false, true} {
		instance := New(nil, nil, io.Discard)
		t.Cleanup(instance.stopAnalysis)
		result, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
			Capabilities:          protocol.ClientCapabilities{Workspace: &protocol.WorkspaceClientCapabilities{ApplyEdit: &supported}},
			InitializationOptions: protocol.LSPAny(`{"runtimepath":[]}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		provider := result.Capabilities.ExecuteCommandProvider
		if !supported && len(provider.Commands) != 0 || supported && !slices.Equal(provider.Commands, []string{CommandUpdateImportsOnRename}) {
			t.Fatalf("applyEdit=%t: provider = %#v", supported, provider)
		}
	}
}

func TestRenameCommandAfterFilesystemRename(t *testing.T) {
	for _, indexState := range []string{"before watcher", "after watcher", "fresh index"} {
		t.Run(indexState, func(t *testing.T) {
			root := t.TempDir()
			old := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport var Value = 1\n")
			newPath := filepath.Join(root, "util.vim")
			source := "vim9script\nimport './lib.vim'\necho lib.Value\n"
			main := writeWorkspaceFile(t, root, "main.vim", source)
			var instance *Server
			var client *renameCommandClient
			if indexState != "fresh index" {
				instance, client = initializeRenameCommandServer(t, root)
			}
			if err := os.Rename(old, newPath); err != nil {
				t.Fatal(err)
			}
			if indexState == "fresh index" {
				instance, client = initializeRenameCommandServer(t, root)
			} else if indexState == "after watcher" {
				if err := instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{Changes: []protocol.FileEvent{
					{URI: uri.File(old), Type: protocol.FileChangeTypeDeleted},
					{URI: uri.File(newPath), Type: protocol.FileChangeTypeCreated},
				}}); err != nil {
					t.Fatal(err)
				}
				instance.workspaceWG.Wait()
			}
			result, err := instance.ExecuteCommand(context.Background(), renameCommandParams(t, old, newPath))
			if err != nil || string(result) != `{"applied":true}` || len(client.requests) != 1 {
				t.Fatalf("result=%s error=%v requests=%d", result, err, len(client.requests))
			}
			updated := applyRenameEdits(t, &client.requests[0].Edit, map[string]string{main: source})
			if updated[main] != "vim9script\nimport './util.vim'\necho util.Value\n" {
				t.Fatalf("updated = %#v", updated)
			}
		})
	}
}

func TestRenameCommandEditsMovedImporterAtNewURI(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		for _, open := range []bool{false, true} {
			root := t.TempDir()
			oldLib := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport var Value = 1\n")
			newLib := filepath.Join(root, "util.vim")
			source := "vim9script\r\nimport './lib.vim' as lib\r\necho lib.Value\r\n"
			oldMain := writeWorkspaceFile(t, root, "main.vim", source)
			newMain := filepath.Join(root, "sub", "main.vim")
			instance, client := initializeRenameCommandServer(t, root)
			if err := os.Mkdir(filepath.Dir(newMain), 0o700); err != nil {
				t.Fatal(err)
			}
			for _, pair := range [][2]string{{oldLib, newLib}, {oldMain, newMain}} {
				if err := os.Rename(pair[0], pair[1]); err != nil {
					t.Fatal(err)
				}
			}
			if refresh {
				instance.scheduleWorkspaceRebuild()
				instance.workspaceWG.Wait()
			}
			if open {
				source = "# 😀é unsaved\r\n" + source
				if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: uri.File(newMain), Version: 7, Text: source}}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := instance.ExecuteCommand(context.Background(), renameCommandParams(t, oldLib, newLib, oldMain, newMain)); err != nil {
				t.Fatal(err)
			}
			if len(client.requests) != 1 {
				t.Fatalf("refresh=%t open=%t: requests=%d", refresh, open, len(client.requests))
			}
			edit := &client.requests[0].Edit
			updated := applyRenameEdits(t, edit, map[string]string{newMain: source})
			want := "vim9script\r\nimport '../util.vim' as lib\r\necho lib.Value\r\n"
			if open {
				want = "# 😀é unsaved\r\n" + want
				version := edit.DocumentChanges[0].(*protocol.TextDocumentEdit).TextDocument.Version
				if version == nil || *version != 7 {
					t.Fatalf("version = %v", version)
				}
			}
			if updated[newMain] != want {
				t.Fatalf("refresh=%t open=%t: updated = %#v", refresh, open, updated)
			}
		}
	}
}

func TestRenameCommandPreservesRuntimeLookupOrder(t *testing.T) {
	for _, shadow := range []bool{false, true} {
		root := t.TempDir()
		first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
		// The second lib becomes visible after the first is renamed. Imports
		// still referred to the first file before the operation.
		old := writeWorkspaceFile(t, root, "first/autoload/lib.vim", "vim9script\nexport var Value = 1\n")
		writeWorkspaceFile(t, root, "second/autoload/lib.vim", "vim9script\nexport var Value = 2\n")
		if shadow {
			writeWorkspaceFile(t, root, "shadow/autoload/util.vim", "vim9script\nexport var Value = 3\n")
		}
		source := "vim9script\nimport autoload 'lib.vim' as lib\necho lib.Value\n"
		main := writeWorkspaceFile(t, root, "main.vim", source)
		newPath := filepath.Join(first, "autoload", "util.vim")
		if err := os.Rename(old, newPath); err != nil {
			t.Fatal(err)
		}
		instance, client := initializeRenameCommandServer(t, root, filepath.Join(root, "shadow"), first, second)
		if _, err := instance.ExecuteCommand(context.Background(), renameCommandParams(t, old, newPath)); err != nil {
			t.Fatal(err)
		}
		if shadow {
			if len(client.requests) != 0 {
				t.Fatal("rewrote a shadowed runtime import")
			}
		} else {
			if len(client.requests) != 1 {
				t.Fatalf("requests = %d", len(client.requests))
			}
			updated := applyRenameEdits(t, &client.requests[0].Edit, map[string]string{main: source})
			if updated[main] != "vim9script\nimport autoload 'util.vim' as lib\necho lib.Value\n" {
				t.Fatalf("updated = %#v", updated)
			}
		}
	}
}

func TestRenameCommandSkipsUnverifiableEdits(t *testing.T) {
	for _, scenario := range []string{"not moved", "stale content", "incomplete index", "unbound namespace", "unrelated"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			old := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport var Value = 1\n")
			newPath := filepath.Join(root, "util.vim")
			source := "vim9script\nimport './lib.vim'\necho lib.Value\n"
			if scenario == "unbound namespace" {
				source += "# lib comment\n"
			} else if scenario == "unrelated" {
				source = "vim9script\n"
			}
			main := writeWorkspaceFile(t, root, "main.vim", source)
			instance, client := initializeRenameCommandServer(t, root)
			if scenario != "not moved" {
				if err := os.Rename(old, newPath); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "stale content" {
				if err := os.WriteFile(main, []byte(source+"echo 2\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "incomplete index" {
				instance.workspaceIndex.SetComplete(false)
			}
			result, err := instance.ExecuteCommand(context.Background(), renameCommandParams(t, old, newPath))
			if err != nil || result != nil || len(client.requests) != 0 {
				t.Fatalf("result=%s error=%v requests=%d", result, err, len(client.requests))
			}
		})
	}
}

func TestRenameCommandApplyResultAndCancellation(t *testing.T) {
	root := t.TempDir()
	old := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport var Value = 1\n")
	newPath := filepath.Join(root, "util.vim")
	main := writeWorkspaceFile(t, root, "main.vim", "vim9script\nimport './lib.vim' as lib\n")
	instance, client := initializeRenameCommandServer(t, root)
	instance.mu.Lock()
	instance.documentChangesSupport = false
	instance.mu.Unlock()
	if err := os.Rename(old, newPath); err != nil {
		t.Fatal(err)
	}
	params := renameCommandParams(t, old, newPath)
	reason := "user declined"
	client.apply = func(context.Context) (*protocol.ApplyWorkspaceEditResult, error) {
		return &protocol.ApplyWorkspaceEditResult{FailureReason: &reason}, nil
	}
	result, err := instance.ExecuteCommand(context.Background(), params)
	if err != nil || string(result) != `{"applied":false,"failureReason":"user declined"}` {
		t.Fatalf("result=%s error=%v", result, err)
	}
	edit := client.requests[0].Edit
	if len(edit.DocumentChanges) != 0 || len(edit.Changes[uri.File(mustWorkspaceCanonicalPath(t, main))]) != 1 {
		t.Fatalf("fallback edit = %#v", edit)
	}
	failure := errors.New("apply failed")
	client.apply = func(context.Context) (*protocol.ApplyWorkspaceEditResult, error) { return nil, failure }
	if _, err := instance.ExecuteCommand(context.Background(), params); !errors.Is(err, failure) {
		t.Fatalf("apply failure = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	client.apply = func(ctx context.Context) (*protocol.ApplyWorkspaceEditResult, error) {
		cancel()
		return nil, ctx.Err()
	}
	if _, err := instance.ExecuteCommand(ctx, params); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Fatalf("apply cancellation = %v", err)
	}
	count := len(client.requests)
	if _, err := instance.ExecuteCommand(ctx, params); !errors.Is(err, protocol.ErrRequestCancelled) || len(client.requests) != count {
		t.Fatalf("cancelled request: error=%v calls=%d", err, len(client.requests))
	}
}

func TestRenameCommandRejectsInvalidRequests(t *testing.T) {
	instance, client := initializeRenameCommandServer(t, t.TempDir())
	for _, params := range []*protocol.ExecuteCommandParams{
		{Command: "unknown"},
		{Command: CommandUpdateImportsOnRename},
		{Command: CommandUpdateImportsOnRename, Arguments: []protocol.LSPAny{protocol.LSPAny(`null`)}},
		{Command: CommandUpdateImportsOnRename, Arguments: []protocol.LSPAny{protocol.LSPAny(`{"files":[]}`)}},
		{Command: CommandUpdateImportsOnRename, Arguments: []protocol.LSPAny{protocol.LSPAny(`{"files":[{"oldUri":"untitled:old","newUri":"file:///new.vim"}]}`)}},
	} {
		_, err := instance.ExecuteCommand(context.Background(), params)
		var rpcError *jsonrpc2.Error
		if !errors.As(err, &rpcError) || rpcError.Code != jsonrpc2.InvalidParams {
			t.Fatalf("params=%#v error=%v", params, err)
		}
	}
	instance.mu.Lock()
	instance.applyEditSupport = false
	instance.mu.Unlock()
	_, err := instance.ExecuteCommand(context.Background(), renameCommandParams(t, "/old.vim", "/new.vim"))
	var rpcError *jsonrpc2.Error
	if !errors.As(err, &rpcError) || rpcError.Code != jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed) || len(client.requests) != 0 {
		t.Fatalf("unsupported applyEdit: error=%v requests=%d", err, len(client.requests))
	}
}
