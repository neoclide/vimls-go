package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestWorkspaceFoldersOverrideRootURIAndBuildSymbolIndex(t *testing.T) {
	root := t.TempDir()
	folder := t.TempDir()
	writeWorkspaceFile(t, root, "root.vim", "vim9script\nvar rootOnly = 1\n")
	writeWorkspaceFile(t, folder, "folder.vim", "vim9script\nvar prefix = '𐐀' | var folderOnly = 1\n")
	rootURI := uri.File(root)
	folderReal, _ := filepath.EvalSymlinks(folder)
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	result, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		WorkspaceFoldersInitializeParams: protocol.WorkspaceFoldersInitializeParams{WorkspaceFolders: protocol.NewNullable([]protocol.WorkspaceFolder{{URI: uri.File(folder), Name: "folder"}})},
		RootURI:                          &rootURI,
		InitializationOptions:            protocol.LSPAny([]byte(`{"runtimepath":[]}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	instance.workspaceDelay = 0
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
	if !ok || location.URI != uri.File(filepath.Join(folderReal, "folder.vim")) || location.Range.Start != (protocol.Position{Line: 1, Character: 24}) || location.Range.End != (protocol.Position{Line: 1, Character: 34}) {
		t.Fatalf("folder location = %#v", symbols[0].Location)
	}
	if symbols := workspaceSymbols(t, instance, "rootOnly"); len(symbols) != 0 {
		t.Fatalf("rootUri leaked through workspaceFolders precedence: %#v", symbols)
	}
}

func TestRuntimepathInitializationAndNotificationReplaceIndex(t *testing.T) {
	workspaceRoot := t.TempDir()
	firstRuntime := t.TempDir()
	secondRuntime := t.TempDir()
	writeWorkspaceFile(t, firstRuntime, filepath.Join("plugin", "first.vim"), "vim9script\nvar firstRuntimeName = 1\n")
	writeWorkspaceFile(t, secondRuntime, filepath.Join("autoload", "second.vim"), "vim9script\nexport var secondRuntimeName = 1\n")
	rootURI := uri.File(workspaceRoot)
	options, err := json.Marshal(map[string]any{"runtimepath": []string{firstRuntime}})
	if err != nil {
		t.Fatal(err)
	}
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{RootURI: &rootURI, InitializationOptions: protocol.LSPAny(options)}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceDelay = 0
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	if symbols := workspaceSymbols(t, instance, "firstRuntimeName"); len(symbols) != 0 {
		t.Fatalf("runtimepath-only symbols leaked = %#v", symbols)
	}
	if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{secondRuntime}}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	if symbols := workspaceSymbols(t, instance, "firstRuntimeName"); len(symbols) != 0 {
		t.Fatalf("old runtimepath symbols = %#v", symbols)
	}
	if symbols := workspaceSymbols(t, instance, "secondRuntimeName"); len(symbols) != 0 {
		t.Fatalf("updated runtimepath-only symbols leaked = %#v", symbols)
	}
}

func TestWorkspaceSymbolsExcludeRuntimepathOnlyFiles(t *testing.T) {
	runtimeRoot := t.TempDir()
	workspaceRoot := filepath.Join(runtimeRoot, "project")
	runtimePath := writeWorkspaceFile(t, runtimeRoot, "plugin/runtime.vim", "vim9script\nvar RuntimeOnly = 1\n")
	workspacePath := writeWorkspaceFile(t, workspaceRoot, "plugin/workspace.vim", "vim9script\nvar WorkspaceOnly = 1\n")
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	rootURI := uri.File(workspaceRoot)
	runtimeOptions, err := json.Marshal(map[string]any{"runtimepath": []string{runtimeRoot}})
	if err != nil {
		t.Fatal(err)
	}
	options := protocol.LSPAny(runtimeOptions)
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{RootURI: &rootURI, InitializationOptions: options}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceDelay = 0
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	if symbols := workspaceSymbols(t, instance, "RuntimeOnly"); len(symbols) != 0 {
		t.Fatalf("runtimepath-only workspace symbols = %#v", symbols)
	}
	symbols := workspaceSymbols(t, instance, "WorkspaceOnly")
	if len(symbols) != 1 {
		t.Fatalf("workspace symbols = %#v", symbols)
	}
	location, ok := symbols[0].Location.(*protocol.Location)
	if !ok || location.URI != uri.File(mustWorkspaceCanonicalPath(t, workspacePath)) {
		t.Fatalf("workspace symbol location = %#v", symbols[0].Location)
	}
	instance.workspaceMu.Lock()
	_, runtimeIndexed := instance.workspaceIndex.Source(runtimePath)
	instance.workspaceMu.Unlock()
	if !runtimeIndexed {
		t.Fatal("runtimepath file was removed instead of being query-filtered")
	}
}

func TestInitializedRegistersOnlyWorkspaceWatchers(t *testing.T) {
	workspaceRoot := t.TempDir()
	firstRuntime := t.TempDir()
	secondRuntime := t.TempDir()
	rootURI := uri.File(workspaceRoot)
	options, err := json.Marshal(map[string]any{"runtimepath": []string{firstRuntime}})
	if err != nil {
		t.Fatal(err)
	}
	dynamic := true
	relative := true
	client := &watchRegistrationClient{}
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.client = client
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI:               &rootURI,
		InitializationOptions: protocol.LSPAny(options),
		Capabilities: protocol.ClientCapabilities{Workspace: &protocol.WorkspaceClientCapabilities{
			DidChangeWatchedFiles: &protocol.DidChangeWatchedFilesClientCapabilities{DynamicRegistration: &dynamic, RelativePatternSupport: &relative},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.watchWG.Wait()
	instance.workspaceWG.Wait()
	if len(client.registrations) != 1 || len(client.registrations[0].Registrations) != 1 {
		t.Fatalf("registrations = %#v", client.registrations)
	}
	assertWatchRegistration(t, client.registrations[0], []string{workspaceRoot})
	unexpectedBuild := make(chan struct{}, 1)
	instance.testHooks.beforeWorkspaceBuild = func([]*text.Snapshot) {
		select {
		case unexpectedBuild <- struct{}{}:
		default:
		}
	}
	if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{secondRuntime}}); err != nil {
		t.Fatal(err)
	}
	instance.watchWG.Wait()
	instance.workspaceWG.Wait()
	if len(client.unregistrations) != 0 || len(client.registrations) != 1 {
		t.Fatalf("registrations = %d, unregistrations = %d", len(client.registrations), len(client.unregistrations))
	}
	select {
	case <-unexpectedBuild:
		t.Fatal("watch refresh started a complete workspace build")
	default:
	}
}

func TestVimWatcherRegistrationHonorsClientCapabilities(t *testing.T) {
	root := t.TempDir()
	client := &watchRegistrationClient{}
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.client = client
	rootURI := uri.File(root)
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI:               &rootURI,
		InitializationOptions: protocol.LSPAny([]byte(`{"runtimepath":[]}`)),
	}); err != nil {
		t.Fatal(err)
	}
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.watchWG.Wait()
	if len(client.registrations) != 0 {
		t.Fatalf("registration without dynamic capability = %#v", client.registrations)
	}

	watchers := vimFileWatchers([]string{root}, false)
	wantPattern := protocol.Pattern(filepath.ToSlash(filepath.Join(root, "**", "*.vim")))
	if len(watchers) != 1 || watchers[0].GlobPattern != wantPattern {
		t.Fatalf("absolute watcher = %#v, want %q", watchers, wantPattern)
	}
	if watchers := vimFileWatchers(nil, false); len(watchers) != 0 {
		t.Fatalf("watchers without workspace roots = %#v", watchers)
	}
}

func TestRuntimepathCustomNotificationDispatch(t *testing.T) {
	runtimeRoot := t.TempDir()
	input := encodeFrames(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`,
		`{"jsonrpc":"2.0","method":"initialized","params":{}}`,
		fmt.Sprintf(`{"jsonrpc":"2.0","method":%q,"params":{"runtimepath":[%q]}}`, MethodDidChangeRuntimepath, runtimeRoot),
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var output bytes.Buffer
	instance := New(&input, &output, io.Discard)
	if code := instance.Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	instance.workspaceMu.Lock()
	paths := append([]string(nil), instance.runtimePaths...)
	instance.workspaceMu.Unlock()
	runtimeRootReal, _ := filepath.EvalSymlinks(runtimeRoot)
	if len(paths) != 1 || paths[0] != runtimeRootReal {
		t.Fatalf("runtimepath = %#v", paths)
	}
}

func TestRuntimepathCustomRequestDispatchesNullResult(t *testing.T) {
	runtimeRoot := t.TempDir()
	input := encodeFrames(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{},"initializationOptions":{"runtimepath":[]}}}`,
		`{"jsonrpc":"2.0","method":"initialized","params":{}}`,
		fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":%q,"params":{"runtimepath":[%q]}}`, MethodDidChangeRuntimepath, runtimeRoot),
		`{"jsonrpc":"2.0","id":3,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var output bytes.Buffer
	if code := New(&input, &output, io.Discard).Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	messages := decodeFrames(t, &output)
	if len(messages) != 3 || idNumber(t, messages[1]) != 2 || string(messages[1]["result"]) != "null" {
		t.Fatalf("request response = %#v", messages)
	}
}

func TestRuntimepathCustomRequestRejectsInvalidParams(t *testing.T) {
	input := encodeFrames(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{},"initializationOptions":{"runtimepath":[]}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"vimls/didChangeRuntimepath","params":"not-an-object"}`,
		`{"jsonrpc":"2.0","id":3,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var output bytes.Buffer
	if code := New(&input, &output, io.Discard).Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	messages := decodeFrames(t, &output)
	if len(messages) != 3 || errorCode(t, messages[1]) != int(jsonrpc2.InvalidParams) {
		t.Fatalf("invalid params response = %#v", messages)
	}
}

func TestRuntimepathDeltaNoopAndReorder(t *testing.T) {
	root := t.TempDir()
	first := t.TempDir()
	second := t.TempDir()
	writeWorkspaceFile(t, first, "autoload/choice.vim", "vim9script\nexport def First()\nenddef\n")
	writeWorkspaceFile(t, second, "autoload/choice.vim", "vim9script\nexport def Second()\nenddef\n")
	instance := initializeWorkspaceServer(t, root)
	if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{first, second}}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceMu.Lock()
	revision := instance.workspaceRevision
	index := instance.workspaceIndex
	instance.workspaceMu.Unlock()
	firstFile := mustWorkspaceCanonicalPath(t, filepath.Join(first, "autoload", "choice.vim"))
	if got, ok := index.RuntimeFile("autoload/choice.vim"); !ok || got != firstFile {
		t.Fatalf("first runtime file = %q, %t", got, ok)
	}
	unexpectedBuild := make(chan struct{}, 1)
	instance.testHooks.beforeWorkspaceBuild = func([]*text.Snapshot) {
		select {
		case unexpectedBuild <- struct{}{}:
		default:
		}
	}
	if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{first, second}}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceMu.Lock()
	if instance.workspaceRevision != revision {
		t.Fatalf("no-op revision = %d, want %d", instance.workspaceRevision, revision)
	}
	instance.workspaceMu.Unlock()
	if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{second, first}}); err != nil {
		t.Fatal(err)
	}
	secondFile := mustWorkspaceCanonicalPath(t, filepath.Join(second, "autoload", "choice.vim"))
	if got, ok := index.RuntimeFile("autoload/choice.vim"); !ok || got != secondFile {
		t.Fatalf("reordered runtime file = %q, %t", got, ok)
	}
	instance.workspaceWG.Wait()
	select {
	case <-unexpectedBuild:
		t.Fatal("runtimepath delta started a complete workspace build")
	default:
	}
}

func TestRuntimepathDeltaDoesNotDiscardConcurrentWorkspaceRebuild(t *testing.T) {
	workspaceRoot := t.TempDir()
	runtimeRoot := t.TempDir()
	writeWorkspaceFile(t, workspaceRoot, "workspace.vim", "vim9script\nvar Workspace = 1\n")
	writeWorkspaceFile(t, runtimeRoot, "plugin/runtime.vim", "vim9script\nvar Runtime = 1\n")
	instance := initializeWorkspaceServer(t, workspaceRoot)
	started := make(chan struct{})
	release := make(chan struct{})
	builds := 0
	instance.testHooks.beforeWorkspaceBuild = func([]*text.Snapshot) {
		builds++
		if builds == 1 {
			close(started)
			<-release
		}
	}
	instance.scheduleWorkspaceRebuild()
	<-started
	if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{runtimeRoot}}); err != nil {
		t.Fatal(err)
	}
	close(release)
	instance.workspaceWG.Wait()
	if builds < 2 {
		t.Fatalf("workspace rebuilds = %d, want retry after concurrent runtimepath delta", builds)
	}
	if symbols := workspaceSymbols(t, instance, "Workspace"); len(symbols) != 1 {
		t.Fatalf("workspace symbols after retry = %#v", symbols)
	}
	if symbols := workspaceSymbols(t, instance, "Runtime"); len(symbols) != 0 {
		t.Fatalf("runtime symbols after retry leaked = %#v", symbols)
	}
}

func TestRuntimepathDeltaAddRemoveAndNestedRoots(t *testing.T) {
	workspaceRoot := t.TempDir()
	runtimeRoot := t.TempDir()
	nested := filepath.Join(runtimeRoot, "pack", "bundle")
	writeWorkspaceFile(t, runtimeRoot, "plugin/outer.vim", "vim9script\nvar OuterRuntime = 1\n")
	writeWorkspaceFile(t, nested, "plugin/nested.vim", "vim9script\nvar NestedRuntime = 1\n")
	instance := initializeWorkspaceServer(t, workspaceRoot)
	if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{runtimeRoot, nested}}); err != nil {
		t.Fatal(err)
	}
	if symbols := workspaceSymbols(t, instance, "OuterRuntime"); len(symbols) != 0 {
		t.Fatalf("added outer runtime symbols leaked = %#v", symbols)
	}
	if symbols := workspaceSymbols(t, instance, "NestedRuntime"); len(symbols) != 0 {
		t.Fatalf("added nested runtime symbols leaked = %#v", symbols)
	}
	if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{nested}}); err != nil {
		t.Fatal(err)
	}
	if symbols := workspaceSymbols(t, instance, "OuterRuntime"); len(symbols) != 0 {
		t.Fatalf("removed outer runtime symbols = %#v", symbols)
	}
	if symbols := workspaceSymbols(t, instance, "NestedRuntime"); len(symbols) != 0 {
		t.Fatalf("retained nested runtime symbols leaked = %#v", symbols)
	}
}

func TestRuntimepathDeltaSilentlyDropsInvalidRoots(t *testing.T) {
	root := t.TempDir()
	notDirectory := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(notDirectory, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	instance := initializeWorkspaceServer(t, root)
	if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{filepath.Join(root, "missing"), notDirectory}}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceMu.Lock()
	paths := append([]string(nil), instance.runtimePaths...)
	complete := instance.workspaceIndex.Complete()
	instance.workspaceMu.Unlock()
	if len(paths) != 0 || !complete {
		t.Fatalf("runtimepath=%#v complete=%t, want empty complete index", paths, complete)
	}
}

func TestRuntimepathDeltaCancellationLeavesExistingIndex(t *testing.T) {
	workspaceRoot := t.TempDir()
	oldRuntime := t.TempDir()
	newRuntime := t.TempDir()
	oldFile := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, oldRuntime, "plugin/old.vim", "vim9script\nvar OldRuntime = 1\n"))
	newFile := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, newRuntime, "plugin/new.vim", "vim9script\nvar NewRuntime = 1\n"))
	instance := initializeWorkspaceServer(t, workspaceRoot)
	if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{oldRuntime}}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceMu.Lock()
	revision := instance.workspaceRevision
	instance.workspaceMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := instance.DidChangeRuntimepath(ctx, &DidChangeRuntimepathParams{Runtimepath: []string{newRuntime}}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceMu.Lock()
	paths := append([]string(nil), instance.runtimePaths...)
	gotRevision := instance.workspaceRevision
	instance.workspaceMu.Unlock()
	if len(paths) != 1 || paths[0] != mustWorkspaceCanonicalPath(t, oldRuntime) || gotRevision != revision {
		t.Fatalf("cancelled runtimepath delta paths=%#v revision=%d, want old path and revision %d", paths, gotRevision, revision)
	}
	instance.workspaceMu.Lock()
	_, oldIndexed := instance.workspaceIndex.Source(oldFile)
	_, newIndexed := instance.workspaceIndex.Source(newFile)
	instance.workspaceMu.Unlock()
	if !oldIndexed || newIndexed {
		t.Fatalf("cancelled runtimepath delta indexed old=%t new=%t, want true, false", oldIndexed, newIndexed)
	}
}

func TestRuntimepathDeltaAfterLifecycleCancellationLeavesExistingIndex(t *testing.T) {
	workspaceRoot := t.TempDir()
	oldRuntime := t.TempDir()
	newRuntime := t.TempDir()
	oldFile := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, oldRuntime, "plugin/old.vim", "vim9script\nvar OldRuntime = 1\n"))
	newFile := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, newRuntime, "plugin/new.vim", "vim9script\nvar NewRuntime = 1\n"))
	instance := initializeWorkspaceServer(t, workspaceRoot)
	if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{oldRuntime}}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceMu.Lock()
	revision := instance.workspaceRevision
	instance.workspaceMu.Unlock()
	instance.cancelAnalysis()
	if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{newRuntime}}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceMu.Lock()
	paths := append([]string(nil), instance.runtimePaths...)
	gotRevision := instance.workspaceRevision
	_, oldIndexed := instance.workspaceIndex.Source(oldFile)
	_, newIndexed := instance.workspaceIndex.Source(newFile)
	instance.workspaceMu.Unlock()
	if len(paths) != 1 || paths[0] != mustWorkspaceCanonicalPath(t, oldRuntime) || gotRevision != revision || !oldIndexed || newIndexed {
		t.Fatalf("stopped runtimepath delta paths=%#v revision=%d indexed old=%t new=%t, want old path, revision %d, true, false", paths, gotRevision, oldIndexed, newIndexed, revision)
	}
}

func TestRuntimepathDeltaCancelsInFlightDiscoveryWithoutMutation(t *testing.T) {
	for _, test := range []struct {
		name   string
		cancel func(*Server, context.CancelFunc)
	}{
		{
			name: "request",
			cancel: func(_ *Server, cancel context.CancelFunc) {
				cancel()
			},
		},
		{
			name: "lifecycle",
			cancel: func(instance *Server, _ context.CancelFunc) {
				instance.cancelAnalysis()
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspaceRoot := t.TempDir()
			oldRuntime := t.TempDir()
			newRuntime := t.TempDir()
			writeWorkspaceFile(t, oldRuntime, "plugin/old.vim", "vim9script\nvar OldRuntime = 1\n")
			instance := initializeWorkspaceServer(t, workspaceRoot)
			if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{oldRuntime}}); err != nil {
				t.Fatal(err)
			}
			instance.workspaceMu.Lock()
			revision := instance.workspaceRevision
			instance.workspaceMu.Unlock()

			newRuntime = mustWorkspaceCanonicalPath(t, newRuntime)
			started := make(chan struct{})
			requestValue := make(chan any, 1)
			instance.testHooks.discoverWorkspaceFiles = func(ctx context.Context, root string, limit int) ([]string, bool, error) {
				if root != newRuntime {
					return nil, false, fmt.Errorf("discovery root = %q, want %q", root, newRuntime)
				}
				requestValue <- ctx.Value("request-context")
				close(started)
				<-ctx.Done()
				return nil, false, ctx.Err()
			}
			requestCtx, requestCancel := context.WithCancel(context.WithValue(context.Background(), "request-context", test.name))
			defer requestCancel()
			result := make(chan error, 1)
			go func() {
				result <- instance.DidChangeRuntimepath(requestCtx, &DidChangeRuntimepathParams{Runtimepath: []string{newRuntime}})
			}()
			waitForServerRace(t, started, "runtimepath delta discovery")
			if got := <-requestValue; got != test.name {
				t.Fatalf("discovery request context value = %v, want %q", got, test.name)
			}
			test.cancel(instance, requestCancel)
			select {
			case err := <-result:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("cancelled runtimepath delta did not return")
			}
			instance.workspaceMu.Lock()
			paths := append([]string(nil), instance.runtimePaths...)
			gotRevision := instance.workspaceRevision
			instance.workspaceMu.Unlock()
			if len(paths) != 1 || paths[0] != mustWorkspaceCanonicalPath(t, oldRuntime) || gotRevision != revision {
				t.Fatalf("cancelled runtimepath delta paths=%#v revision=%d, want old path and revision %d", paths, gotRevision, revision)
			}
		})
	}
}

func TestRuntimepathDeltaSilentlyDropsUnreadableRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix directory permissions are not portable to Windows")
	}
	root := t.TempDir()
	unreadable := t.TempDir()
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o700) })
	if _, err := os.ReadDir(unreadable); err == nil {
		t.Skip("current user can still read chmod 000 directory")
	}
	var logs bytes.Buffer
	instance := New(nil, nil, &logs)
	t.Cleanup(instance.stopAnalysis)
	rootURI := uri.File(root)
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{RootURI: &rootURI, InitializationOptions: protocol.LSPAny([]byte(`{"runtimepath":[]}`))}); err != nil {
		t.Fatal(err)
	}
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{unreadable}}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceMu.Lock()
	paths := append([]string(nil), instance.runtimePaths...)
	complete := instance.workspaceIndex.Complete()
	instance.workspaceMu.Unlock()
	if len(paths) != 0 || !complete || logs.Len() != 0 {
		t.Fatalf("runtimepath=%#v complete=%t logs=%q", paths, complete, logs.String())
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

func TestOversizedWorkspaceDocumentIsNotIndexedAndEvictsPriorAST(t *testing.T) {
	root := t.TempDir()
	path := writeWorkspaceFile(t, root, "current.vim", "vim9script\nimport './target.vim' as Target\nvar diskValue = Target.Value\n")
	writeWorkspaceFile(t, root, "target.vim", "vim9script\nexport var Value = 1\n")
	path = mustWorkspaceCanonicalPath(t, path)
	instance := initializeWorkspaceServer(t, root)
	documentURI := uri.File(path)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: "vim9script\nimport './target.vim' as Target\nvar openValue = Target.Value\n",
	}}); err != nil {
		t.Fatal(err)
	}
	if symbols := waitForWorkspaceSymbols(t, instance, "openValue", 1); len(symbols) != 1 {
		t.Fatalf("open overlay symbols = %#v", symbols)
	}
	snapshot, ok := instance.documents.Snapshot(documentURI.String())
	if !ok || instance.parseSnapshot(snapshot) == nil {
		t.Fatal("small overlay was not parsed")
	}
	if graph := waitForImportGraph(t, instance, func(graph workspace.ImportGraphSnapshot) bool { return graph.Ready() && len(graph.Imports(path)) == 1 }); len(graph.Imports(path)) != 1 {
		t.Fatalf("small overlay imports = %#v", graph.Imports(path))
	}
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
	instance.mu.Lock()
	instance.client = client
	instance.mu.Unlock()
	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{Text: strings.Repeat("x", maxFileBytes+1)}},
	}); err != nil {
		t.Fatal(err)
	}
	var diagnostics *protocol.PublishDiagnosticsParams
	for diagnostics == nil {
		candidate := waitForDiagnostics(t, client.published)
		if version, ok := candidate.Version.Get(); ok && version == 2 {
			diagnostics = candidate
		}
	}
	if len(diagnostics.Diagnostics) != 1 || diagnostics.Diagnostics[0].Code != protocol.String("vimls/file-too-large") {
		t.Fatalf("oversized diagnostics = %#v", diagnostics)
	}
	instance.publishMu.Lock()
	_, cached := instance.parsed[documentURI.String()]
	instance.publishMu.Unlock()
	instance.workspaceMu.Lock()
	_, indexed := instance.workspaceIndex.Source(path)
	graph := instance.workspaceGraphView
	pending := len(instance.workspacePending)
	instance.workspaceMu.Unlock()
	if cached || indexed || len(graph.Imports(path)) != 0 || !graph.Ready() || pending != 0 {
		t.Fatalf("oversized workspace cache=%t indexed=%t imports=%#v ready=%t pending=%d", cached, indexed, graph.Imports(path), graph.Ready(), pending)
	}
	if symbols := workspaceSymbols(t, instance, "openValue"); len(symbols) != 0 {
		t.Fatalf("oversized overlay remained indexed: %#v", symbols)
	}
	graphRevision := graph.Revision()
	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 3},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{Text: strings.Repeat("x", maxFileBytes+1)}},
	}); err != nil {
		t.Fatal(err)
	}
	for {
		candidate := waitForDiagnostics(t, client.published)
		if version, ok := candidate.Version.Get(); ok && version == 3 {
			break
		}
	}
	instance.workspaceMu.Lock()
	graph = instance.workspaceGraphView
	instance.workspaceMu.Unlock()
	if graph.Revision() != graphRevision {
		t.Fatalf("same oversized change advanced graph: got %d, want %d", graph.Revision(), graphRevision)
	}
	instance.scheduleWorkspaceRebuild()
	instance.workspaceWG.Wait()
	instance.workspaceMu.Lock()
	_, indexed = instance.workspaceIndex.Source(path)
	graph = instance.workspaceGraphView
	_, knownDiskFile := instance.workspaceFiles[path]
	pending = len(instance.workspacePending)
	instance.workspaceMu.Unlock()
	if indexed || len(graph.Imports(path)) != 0 || !graph.Ready() || pending != 0 || !knownDiskFile {
		t.Fatalf("rebuild oversized workspace indexed=%t imports=%#v ready=%t pending=%d disk=%t", indexed, graph.Imports(path), graph.Ready(), pending, knownDiskFile)
	}
	if err := instance.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceMu.Lock()
	source, indexed := instance.workspaceIndex.Source(path)
	graph = instance.workspaceGraphView
	instance.workspaceMu.Unlock()
	if !indexed || source != "vim9script\nimport './target.vim' as Target\nvar diskValue = Target.Value\n" || len(graph.Imports(path)) != 1 {
		t.Fatalf("close restore source=%q indexed=%t imports=%#v", source, indexed, graph.Imports(path))
	}
	if symbols := workspaceSymbols(t, instance, "diskValue"); len(symbols) != 1 {
		t.Fatalf("close restore symbols = %#v", symbols)
	}
}

func TestOversizedWorkspaceSaveEvictsPriorAST(t *testing.T) {
	root := t.TempDir()
	path := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "save.vim", "let g:diskValue = 1\n"))
	instance := initializeWorkspaceServer(t, root)
	instance.analysisMu.Lock()
	instance.analysisWorkers = 1
	instance.analysisMu.Unlock()
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 1)}
	instance.mu.Lock()
	instance.client = client
	instance.mu.Unlock()
	documentURI := uri.File(path)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: "let g:openValue = 1\n",
	}}); err != nil {
		t.Fatal(err)
	}
	instance.analyzeDocument(documentURI.String())
	snapshot, ok := instance.documents.Snapshot(documentURI.String())
	if !ok || instance.parseSnapshot(snapshot) == nil {
		t.Fatal("small save overlay was not parsed")
	}
	oversized := strings.Repeat("x", maxFileBytes+1)
	if err := instance.DidSave(context.Background(), &protocol.DidSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Text: &oversized,
	}); err != nil {
		t.Fatal(err)
	}
	instance.analyzeDocument(documentURI.String())
	diagnostics := waitForDiagnostics(t, client.published)
	if len(diagnostics.Diagnostics) != 1 || diagnostics.Diagnostics[0].Code != protocol.String("vimls/file-too-large") {
		t.Fatalf("oversized save diagnostics = %#v", diagnostics)
	}
	instance.publishMu.Lock()
	_, cached := instance.parsed[documentURI.String()]
	instance.publishMu.Unlock()
	instance.workspaceMu.Lock()
	_, indexed := instance.workspaceIndex.Source(path)
	graph := instance.workspaceGraphView
	pending := len(instance.workspacePending)
	instance.workspaceMu.Unlock()
	if cached || indexed || len(graph.Imports(path)) != 0 || !graph.Ready() || pending != 0 {
		t.Fatalf("oversized save cache=%t indexed=%t imports=%#v ready=%t pending=%d", cached, indexed, graph.Imports(path), graph.Ready(), pending)
	}
}

func TestWorkspaceRestoreInstallsCapturedDiskSourceAndFacts(t *testing.T) {
	root := t.TempDir()
	path := writeWorkspaceFile(t, root, "main.vim", "vim9script\nimport './target.vim' as Target\n")
	target := writeWorkspaceFile(t, root, "target.vim", "vim9script\nexport var Value = 1\n")
	path, target = mustWorkspaceCanonicalPath(t, path), mustWorkspaceCanonicalPath(t, target)
	instance := initializeWorkspaceServer(t, root)
	documentURI := uri.File(path).String()
	restore, ok := instance.captureWorkspaceRestore(documentURI)
	if !ok {
		t.Fatal("restore was not captured")
	}
	instance.workspaceMu.Lock()
	if err := instance.workspaceIndex.Replace(path, syntax.Parse("vim9script\nvar stale = 1\n")); err != nil {
		instance.workspaceMu.Unlock()
		t.Fatal(err)
	}
	if err := instance.workspaceGraph.Replace(path, nil); err != nil {
		instance.workspaceMu.Unlock()
		t.Fatal(err)
	}
	instance.workspaceGraphView = instance.workspaceGraph.Snapshot()
	instance.workspaceMu.Unlock()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if dependents := instance.installWorkspaceRestore(restore, syntax.Parse(string(content))); len(dependents) != 0 {
		t.Fatalf("restore dependents=%#v", dependents)
	}
	instance.workspaceMu.Lock()
	source, indexed := instance.workspaceIndex.Source(path)
	graph := instance.workspaceGraphView
	instance.workspaceMu.Unlock()
	if !indexed || source != string(content) {
		t.Fatalf("restored source=%q indexed=%t", source, indexed)
	}
	if outgoing := graph.Outgoing(path); len(outgoing) != 1 || !sameWorkspacePath(outgoing[0].Target, target) {
		t.Fatalf("restored facts=%#v", outgoing)
	}
}

func TestWorkspaceRestoreIndexFailureRemovesIndexAndGraphFacts(t *testing.T) {
	root := t.TempDir()
	path := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "main.vim", "vim9script\nimport './target.vim' as Target\n"))
	writeWorkspaceFile(t, root, "target.vim", "vim9script\nexport var Value = 1\n")
	instance := initializeWorkspaceServer(t, root)
	oldSource := "vim9script\nimport './target.vim' as Target\n"
	instance.workspaceMu.Lock()
	limited := workspace.NewIndex(0, len(oldSource))
	if err := limited.Replace(path, syntax.Parse(oldSource)); err != nil {
		instance.workspaceMu.Unlock()
		t.Fatal(err)
	}
	limited.SetComplete(true)
	instance.workspaceIndex = limited
	instance.workspaceMu.Unlock()
	restore, ok := instance.captureWorkspaceRestore(uri.File(path).String())
	if !ok {
		t.Fatal("restore was not captured")
	}
	if dependents := instance.installWorkspaceRestore(restore, syntax.Parse(oldSource+"var this_source_exceeds_the_index_limit = 1\n")); len(dependents) != 0 {
		t.Fatalf("failed restore dependents = %#v", dependents)
	}
	instance.workspaceMu.Lock()
	_, indexed := instance.workspaceIndex.Source(path)
	graph := instance.workspaceGraphView
	complete := instance.workspaceIndex.Complete()
	pending := len(instance.workspacePending)
	instance.workspaceMu.Unlock()
	if indexed || graph.Has(path) || complete || pending != 0 || !graph.Ready() {
		t.Fatalf("failed restore indexed=%t graph=%t complete=%t pending=%d ready=%t", indexed, graph.Has(path), complete, pending, graph.Ready())
	}
}

func TestWorkspaceRestoreRejectsReopenedURIAndAliasOverlay(t *testing.T) {
	root := t.TempDir()
	path := writeWorkspaceFile(t, root, "restore.vim", "vim9script\nimport './disk.vim' as Disk\n")
	diskTarget := writeWorkspaceFile(t, root, "disk.vim", "vim9script\nexport var Disk = 1\n")
	sentinelTarget := writeWorkspaceFile(t, root, "sentinel.vim", "vim9script\nexport var Sentinel = 1\n")
	path, diskTarget, sentinelTarget = mustWorkspaceCanonicalPath(t, path), mustWorkspaceCanonicalPath(t, diskTarget), mustWorkspaceCanonicalPath(t, sentinelTarget)
	instance := initializeWorkspaceServer(t, root)
	documentURI := uri.File(path).String()
	diskSource := "vim9script\nimport './disk.vim' as Disk\n"
	sentinelSource := "vim9script\nimport './sentinel.vim' as Sentinel\n"
	installSentinel := func() uint64 {
		t.Helper()
		instance.workspaceMu.Lock()
		defer instance.workspaceMu.Unlock()
		if err := instance.workspaceIndex.Replace(path, syntax.Parse(sentinelSource)); err != nil {
			t.Fatal(err)
		}
		facts := collectWorkspaceImportFacts(path, syntax.Parse(sentinelSource), instance.workspaceResolver, nil)
		if err := instance.workspaceGraph.Replace(path, facts); err != nil {
			t.Fatal(err)
		}
		instance.workspaceGraphView = instance.workspaceGraph.Snapshot()
		return instance.workspaceRevision
	}
	assertSentinel := func(label string, revision uint64) {
		t.Helper()
		instance.workspaceMu.Lock()
		source, indexed := instance.workspaceIndex.Source(path)
		graph := instance.workspaceGraphView
		gotRevision := instance.workspaceRevision
		instance.workspaceMu.Unlock()
		if !indexed || source != sentinelSource || gotRevision != revision {
			t.Fatalf("%s source=%q indexed=%t revision=%d want %d", label, source, indexed, gotRevision, revision)
		}
		if outgoing := graph.Outgoing(path); len(outgoing) != 1 || !sameWorkspacePath(outgoing[0].Target, sentinelTarget) || sameWorkspacePath(outgoing[0].Target, diskTarget) {
			t.Fatalf("%s facts=%#v", label, outgoing)
		}
	}
	restore, ok := instance.captureWorkspaceRestore(documentURI)
	if !ok {
		t.Fatal("restore was not captured")
	}
	instance.documents.Open(documentURI, 1, "vim9script\nvar reopened = 1\n")
	revision := installSentinel()
	if dependents := instance.installWorkspaceRestore(restore, syntax.Parse(diskSource)); len(dependents) != 0 {
		t.Fatalf("reopened restore dependents=%#v", dependents)
	}
	assertSentinel("reopened", revision)
	instance.documents.Close(documentURI)
	restore, ok = instance.captureWorkspaceRestore(documentURI)
	if !ok {
		t.Fatal("alias restore was not captured")
	}
	alias := filepath.Join(root, "alias.vim")
	if err := os.Symlink(path, alias); err != nil {
		t.Skipf("cannot create symlink overlay: %v", err)
	}
	instance.documents.Open(uri.File(alias).String(), 1, "vim9script\nvar alias = 1\n")
	revision = installSentinel()
	if dependents := instance.installWorkspaceRestore(restore, syntax.Parse(diskSource)); len(dependents) != 0 {
		t.Fatalf("alias restore dependents=%#v", dependents)
	}
	assertSentinel("alias", revision)
}

func TestWorkspaceRestoreGenerationStaleSchedulesMergedRebuild(t *testing.T) {
	root := t.TempDir()
	path := writeWorkspaceFile(t, root, "restore.vim", "vim9script\nimport './target.vim' as Target\n")
	target := writeWorkspaceFile(t, root, "target.vim", "vim9script\nexport var Value = 1\n")
	path, target = mustWorkspaceCanonicalPath(t, path), mustWorkspaceCanonicalPath(t, target)
	instance := initializeWorkspaceServer(t, root)
	restore, ok := instance.captureWorkspaceRestore(uri.File(path).String())
	if !ok {
		t.Fatal("restore was not captured")
	}
	instance.workspaceMu.Lock()
	instance.workspaceIndex.Remove(path)
	instance.workspaceGraph.Remove(path)
	instance.workspaceGraphView = instance.workspaceGraph.Snapshot()
	instance.workspaceRevision++
	instance.workspaceMu.Unlock()
	if dependents := instance.installWorkspaceRestore(restore, syntax.Parse("vim9script\nimport './target.vim' as Target\n")); len(dependents) != 0 {
		t.Fatalf("stale restore dependents=%#v", dependents)
	}
	instance.workspaceWG.Wait()
	instance.workspaceMu.Lock()
	source, indexed := instance.workspaceIndex.Source(path)
	graph := instance.workspaceGraphView
	instance.workspaceMu.Unlock()
	if !indexed || source != "vim9script\nimport './target.vim' as Target\n" {
		t.Fatalf("rebuilt source=%q indexed=%t", source, indexed)
	}
	if outgoing := graph.Outgoing(path); len(outgoing) != 1 || !sameWorkspacePath(outgoing[0].Target, target) {
		t.Fatalf("rebuilt facts=%#v", outgoing)
	}
}

func TestWorkspaceRestoreCancelledDoesNotInstallOrRebuild(t *testing.T) {
	root := t.TempDir()
	path := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "restore.vim", "vim9script\nvar disk = 1\n"))
	instance := initializeWorkspaceServer(t, root)
	restore, ok := instance.captureWorkspaceRestore(uri.File(path).String())
	if !ok {
		t.Fatal("restore was not captured")
	}
	instance.workspaceMu.Lock()
	source, indexed := instance.workspaceIndex.Source(path)
	revision := instance.workspaceRevision
	instance.workspaceMu.Unlock()
	instance.stopAnalysis()
	if dependents := instance.installWorkspaceRestore(restore, syntax.Parse("vim9script\nvar changed = 1\n")); len(dependents) != 0 {
		t.Fatalf("cancelled restore dependents=%#v", dependents)
	}
	instance.workspaceMu.Lock()
	gotSource, gotIndexed := instance.workspaceIndex.Source(path)
	gotRevision := instance.workspaceRevision
	instance.workspaceMu.Unlock()
	if gotSource != source || gotIndexed != indexed || gotRevision != revision {
		t.Fatalf("cancelled restore source=%q indexed=%t revision=%d", gotSource, gotIndexed, gotRevision)
	}
}

func TestWorkspaceSymbolsKeepDeprecatedTag(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "deprecated.vim", "vim9script\n# @deprecated\nexport def OldFunc()\nenddef\n")
	instance := initializeWorkspaceServer(t, root)
	symbols := workspaceSymbols(t, instance, "OldFunc")
	if len(symbols) != 1 || len(symbols[0].Tags) != 1 || symbols[0].Tags[0] != protocol.SymbolTagDeprecated {
		t.Fatalf("workspace symbols = %#v", symbols)
	}
}

func TestWorkspaceImportGraphBuildsDirectedReadySnapshot(t *testing.T) {
	root := t.TempDir()
	a := writeWorkspaceFile(t, root, "a.vim", "vim9script\nimport './b.vim' as B\nimport './c.vim' as C\n")
	b := writeWorkspaceFile(t, root, "b.vim", "vim9script\nimport './d.vim' as D\n")
	c := writeWorkspaceFile(t, root, "c.vim", "vim9script\nimport autoload './d.vim' as D\n")
	d := writeWorkspaceFile(t, root, "d.vim", "vim9script\nimport './a.vim' as A\n")
	a, b, c, d = mustWorkspaceCanonicalPath(t, a), mustWorkspaceCanonicalPath(t, b), mustWorkspaceCanonicalPath(t, c), mustWorkspaceCanonicalPath(t, d)
	instance := initializeWorkspaceServer(t, root)

	instance.workspaceMu.Lock()
	graph := instance.workspaceGraphView
	instance.workspaceMu.Unlock()
	if !graph.Ready() || graph.Revision() == 0 {
		t.Fatalf("graph state: ready=%t revision=%d", graph.Ready(), graph.Revision())
	}
	if outgoing := graph.Outgoing(a); len(outgoing) != 2 || outgoing[0].Alias != "B" || outgoing[1].Alias != "C" {
		t.Fatalf("A outgoing = %#v", outgoing)
	}
	if incoming := graph.Incoming(d); len(incoming) != 2 || !sameWorkspacePath(incoming[0].Importer, b) || !sameWorkspacePath(incoming[1].Importer, c) || !incoming[1].Autoload {
		t.Fatalf("D incoming = %#v", incoming)
	}
	if incoming := graph.Incoming(a); len(incoming) != 1 || !sameWorkspacePath(incoming[0].Importer, d) {
		t.Fatalf("cycle incoming(A) = %#v", incoming)
	}
}

func TestWorkspaceImportGraphOmitsUnreadableTarget(t *testing.T) {
	root := t.TempDir()
	mainPath := writeWorkspaceFile(t, root, "main.vim", "vim9script\nimport './private.vim' as Private\n")
	targetPath := writeWorkspaceFile(t, root, "private.vim", "vim9script\nexport var Value = 1\n")
	if err := os.Chmod(targetPath, 0); err != nil {
		t.Skipf("cannot make target unreadable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(targetPath, 0o600) })
	if _, err := os.ReadFile(targetPath); err == nil {
		t.Skip("file permissions do not make the target unreadable")
	}
	resolver := workspacePathResolver([]string{root}, nil)
	instance := New(nil, nil, io.Discard)
	_, graph, _, _ := instance.buildWorkspaceIndex(context.Background(), []string{root}, nil, resolver, nil)
	mainPath = mustWorkspaceCanonicalPath(t, mainPath)
	imports := graph.Snapshot().Imports(mainPath)
	if len(imports) != 1 || imports[0].Target != "" || imports[0].Missing || imports[0].Dynamic || len(graph.Snapshot().Outgoing(mainPath)) != 0 {
		t.Fatalf("unreadable target imports = %#v", imports)
	}
}

func TestWorkspaceImportGraphIgnoresFunctionLocalImport(t *testing.T) {
	root := t.TempDir()
	mainPath := writeWorkspaceFile(t, root, "main.vim", "vim9script\ndef Deferred()\n  import './lib.vim' as Lib\n  echo Lib.Value\nenddef\n")
	targetPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport var Value = 1\n")
	instance := initializeWorkspaceServer(t, root)
	graph := currentImportGraph(instance)
	mainPath = mustWorkspaceCanonicalPath(t, mainPath)
	targetPath = mustWorkspaceCanonicalPath(t, targetPath)
	if imports := graph.Imports(mainPath); len(imports) != 0 || len(graph.Outgoing(mainPath)) != 0 || len(graph.Incoming(targetPath)) != 0 {
		t.Fatalf("function-local import entered graph: imports=%#v outgoing=%#v incoming=%#v", imports, graph.Outgoing(mainPath), graph.Incoming(targetPath))
	}
}

func TestWorkspaceImportGraphTracksOpenDocumentChanges(t *testing.T) {
	root := t.TempDir()
	mainPath := writeWorkspaceFile(t, root, "main.vim", "vim9script\nimport './one.vim' as Lib\n")
	onePath := writeWorkspaceFile(t, root, "one.vim", "vim9script\nexport var One = 1\n")
	twoPath := writeWorkspaceFile(t, root, "two.vim", "vim9script\nexport var Two = 2\n")
	mainPath, onePath, twoPath = mustWorkspaceCanonicalPath(t, mainPath), mustWorkspaceCanonicalPath(t, onePath), mustWorkspaceCanonicalPath(t, twoPath)
	instance := initializeWorkspaceServer(t, root)
	initial := currentImportGraph(instance)
	if outgoing := initial.Outgoing(mainPath); len(outgoing) != 1 || !sameWorkspacePath(outgoing[0].Target, onePath) {
		t.Fatalf("initial imports = %#v", outgoing)
	}
	documentURI := uri.File(mainPath)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: "vim9script\nimport './two.vim' as Lib\n",
	}}); err != nil {
		t.Fatal(err)
	}
	opened := waitForImportGraph(t, instance, func(graph workspace.ImportGraphSnapshot) bool {
		outgoing := graph.Outgoing(mainPath)
		return graph.Ready() && len(outgoing) == 1 && sameWorkspacePath(outgoing[0].Target, twoPath)
	})
	if opened.Revision() <= initial.Revision() {
		t.Fatalf("open revision = %d, initial = %d", opened.Revision(), initial.Revision())
	}
	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: "vim9script\nvar local = 1\n"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	changed := waitForImportGraph(t, instance, func(graph workspace.ImportGraphSnapshot) bool {
		return graph.Ready() && len(graph.Outgoing(mainPath)) == 0 && graph.Revision() > opened.Revision()
	})
	if len(changed.Incoming(twoPath)) != 0 {
		t.Fatalf("changed graph retained reverse edge = %#v", changed.Incoming(twoPath))
	}
	if err := instance.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}}); err != nil {
		t.Fatal(err)
	}
	restored := currentImportGraph(instance)
	if outgoing := restored.Outgoing(mainPath); !restored.Ready() || restored.Revision() <= changed.Revision() || len(outgoing) != 1 || !sameWorkspacePath(outgoing[0].Target, onePath) {
		t.Fatalf("restored graph: ready=%t revision=%d outgoing=%#v", restored.Ready(), restored.Revision(), outgoing)
	}
}

func TestWorkspaceIdentitySameContentSkipsInstalledFacts(t *testing.T) {
	root := t.TempDir()
	path := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "main.vim", "vim9script\nvar Value = 1\n"))
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	instance.setWorkspaceRoots([]string{root})
	instance.workspaceMu.Lock()
	instance.workspaceBuilt = true
	instance.workspaceGraph.SetReady(true)
	instance.workspaceGraphView = instance.workspaceGraph.Snapshot()
	instance.workspaceMu.Unlock()
	file := syntax.Parse("vim9script\nvar Value = 1\n")
	instance.replaceWorkspaceFile(uri.File(path).String(), file)

	state := func() (uint64, uint64, int, int) {
		instance.workspaceMu.Lock()
		defer instance.workspaceMu.Unlock()
		return instance.workspaceIndex.Revision(), instance.workspaceGraphView.Revision(), len(instance.workspacePending), len(instance.workspaceDependents)
	}
	indexRevision, graphRevision, pending, dependents := state()
	instance.replaceWorkspaceFile(uri.File(path).String(), syntax.Parse(file.Source))
	if gotIndex, gotGraph, gotPending, gotDependents := state(); gotIndex != indexRevision || gotGraph != graphRevision || gotPending != pending || gotDependents != dependents {
		t.Fatalf("same content changed index=%d/%d graph=%d/%d pending=%d/%d dependents=%d/%d", gotIndex, indexRevision, gotGraph, graphRevision, gotPending, pending, gotDependents, dependents)
	}
}

func TestWorkspaceIdentitySameSourceInstallsMissingFacts(t *testing.T) {
	root := t.TempDir()
	path := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "main.vim", "vim9script\nvar Value = 1\n"))
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	instance.setWorkspaceRoots([]string{root})
	instance.workspaceMu.Lock()
	instance.workspaceBuilt = true
	instance.workspaceGraph.SetReady(true)
	instance.workspaceGraphView = instance.workspaceGraph.Snapshot()
	instance.workspaceMu.Unlock()
	file := syntax.Parse("vim9script\nvar Value = 1\n")
	instance.replaceWorkspaceFile(uri.File(path).String(), file)

	assertInstalled := func(label string, beforeIndex, beforeGraph uint64) {
		t.Helper()
		instance.workspaceMu.Lock()
		defer instance.workspaceMu.Unlock()
		_, indexed := instance.workspaceIndex.Source(path)
		if !indexed || !instance.workspaceGraphView.Has(path) || instance.workspaceIndex.Revision() <= beforeIndex || instance.workspaceGraphView.Revision() <= beforeGraph || len(instance.workspacePending) != 0 {
			t.Fatalf("%s indexed=%t graph=%t index=%d/%d graph=%d/%d pending=%d", label, indexed, instance.workspaceGraphView.Has(path), instance.workspaceIndex.Revision(), beforeIndex, instance.workspaceGraphView.Revision(), beforeGraph, len(instance.workspacePending))
		}
	}
	instance.workspaceMu.Lock()
	beforeIndex, beforeGraph := instance.workspaceIndex.Revision(), instance.workspaceGraphView.Revision()
	instance.workspacePending[path] = struct{}{}
	instance.workspaceMu.Unlock()
	instance.replaceWorkspaceFile(uri.File(path).String(), syntax.Parse(file.Source))
	assertInstalled("pending", beforeIndex, beforeGraph)

	instance.workspaceMu.Lock()
	beforeIndex, beforeGraph = instance.workspaceIndex.Revision(), instance.workspaceGraphView.Revision()
	instance.workspaceGraph.Remove(path)
	instance.workspaceGraphView = instance.workspaceGraph.Snapshot()
	instance.workspaceMu.Unlock()
	instance.replaceWorkspaceFile(uri.File(path).String(), syntax.Parse(file.Source))
	assertInstalled("missing graph facts", beforeIndex, beforeGraph)

	instance.workspaceMu.Lock()
	beforeIndex, beforeGraph = instance.workspaceIndex.Revision(), instance.workspaceGraphView.Revision()
	instance.workspaceIndex.Remove(path)
	instance.workspaceMu.Unlock()
	instance.replaceWorkspaceFile(uri.File(path).String(), syntax.Parse(file.Source))
	assertInstalled("missing index facts", beforeIndex, beforeGraph)
}

func TestWorkspaceIdentityChangedImportReplacesEdgesAndRequeuesDependents(t *testing.T) {
	root := t.TempDir()
	first := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "first.vim", "vim9script\nexport var First = 1\n"))
	second := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "second.vim", "vim9script\nexport var Second = 1\n"))
	main := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "main.vim", "vim9script\nimport './first.vim' as Lib\n"))
	dependent := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "dependent.vim", "vim9script\nimport './main.vim' as Main\n"))
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	instance.setWorkspaceRoots([]string{root})
	instance.workspaceMu.Lock()
	instance.workspaceBuilt = true
	instance.workspaceResolver = workspacePathResolver([]string{root}, nil)
	instance.workspaceGraph.SetReady(true)
	instance.workspaceGraphView = instance.workspaceGraph.Snapshot()
	instance.workspaceMu.Unlock()
	for _, entry := range []struct{ path, source string }{
		{first, "vim9script\nexport var First = 1\n"}, {second, "vim9script\nexport var Second = 1\n"},
		{main, "vim9script\nimport './first.vim' as Lib\n"}, {dependent, "vim9script\nimport './main.vim' as Main\n"},
	} {
		instance.replaceWorkspaceFile(uri.File(entry.path).String(), syntax.Parse(entry.source))
	}
	dependents := instance.replaceWorkspaceFile(uri.File(main).String(), syntax.Parse("vim9script\nimport './second.vim' as Lib\n"))
	if len(dependents) != 1 || dependents[0] != dependent {
		t.Fatalf("changed import dependents = %#v, want %q", dependents, dependent)
	}
	graph := currentImportGraph(instance)
	if outgoing := graph.Outgoing(main); len(outgoing) != 1 || !sameWorkspacePath(outgoing[0].Target, second) || sameWorkspacePath(outgoing[0].Target, first) {
		t.Fatalf("changed import edges = %#v", outgoing)
	}
}

func TestWorkspaceIdentityIndexFailureRemovesOldFactsAndRequeuesDependents(t *testing.T) {
	root := t.TempDir()
	main := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "main.vim", "vim9script\nimport './first.vim' as First\n"))
	first := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "first.vim", "vim9script\nexport var First = 1\n"))
	dependent := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "dependent.vim", "vim9script\nimport './main.vim' as Main\n"))
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	instance.setWorkspaceRoots([]string{root})
	instance.workspaceMu.Lock()
	instance.workspaceBuilt = true
	instance.workspaceResolver = workspacePathResolver([]string{root}, nil)
	instance.workspaceGraph.SetReady(true)
	instance.workspaceGraphView = instance.workspaceGraph.Snapshot()
	instance.workspaceMu.Unlock()
	for _, entry := range []struct{ path, source string }{
		{first, "vim9script\nexport var First = 1\n"}, {main, "vim9script\nimport './first.vim' as First\n"}, {dependent, "vim9script\nimport './main.vim' as Main\n"},
	} {
		instance.replaceWorkspaceFile(uri.File(entry.path).String(), syntax.Parse(entry.source))
	}
	oldSource := "vim9script\nimport './first.vim' as First\n"
	instance.workspaceMu.Lock()
	limited := workspace.NewIndex(0, len(oldSource))
	if err := limited.Replace(main, syntax.Parse(oldSource)); err != nil {
		instance.workspaceMu.Unlock()
		t.Fatal(err)
	}
	limited.SetComplete(true)
	instance.workspaceIndex = limited
	before := instance.workspaceIdentityLocked()
	instance.workspaceMu.Unlock()
	snapshot, dependents := instance.replaceWorkspaceFileWithSnapshot(uri.File(main).String(), syntax.Parse(oldSource+"var this_source_exceeds_the_index_limit = 1\n"))
	if len(dependents) != 1 || dependents[0] != dependent {
		t.Fatalf("index failure dependents = %#v, want %q", dependents, dependent)
	}
	instance.workspaceMu.Lock()
	_, indexed := instance.workspaceIndex.Source(main)
	graph := instance.workspaceGraphView
	stillCurrent := instance.workspaceIdentityCurrentLocked(before)
	instance.workspaceMu.Unlock()
	if indexed || graph.Has(main) || stillCurrent || snapshot.identity == before {
		t.Fatalf("index failure indexed=%t graph=%t oldCurrent=%t identity=%#v old=%#v", indexed, graph.Has(main), stillCurrent, snapshot.identity, before)
	}
}

func TestWorkspaceIdentityGraphFailureRemovesOldFactsAndRequeuesDependents(t *testing.T) {
	root := t.TempDir()
	main := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "main.vim", "vim9script\nimport './first.vim' as First\n"))
	first := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "first.vim", "vim9script\nexport var First = 1\n"))
	dependent := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "dependent.vim", "vim9script\nimport './main.vim' as Main\n"))
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	instance.setWorkspaceRoots([]string{root})
	instance.workspaceMu.Lock()
	instance.workspaceBuilt = true
	instance.workspaceResolver = workspacePathResolver([]string{root}, nil)
	instance.workspaceGraph.SetReady(true)
	instance.workspaceGraphView = instance.workspaceGraph.Snapshot()
	instance.workspaceMu.Unlock()
	for _, entry := range []struct{ path, source string }{
		{first, "vim9script\nexport var First = 1\n"}, {main, "vim9script\nimport './first.vim' as First\n"}, {dependent, "vim9script\nimport './main.vim' as Main\n"},
	} {
		instance.replaceWorkspaceFile(uri.File(entry.path).String(), syntax.Parse(entry.source))
	}
	instance.workspaceMu.Lock()
	instance.workspacePending[main] = struct{}{}
	instance.workspaceGraph.SetReady(false)
	instance.workspaceGraphView = instance.workspaceGraph.Snapshot()
	instance.workspaceMu.Unlock()
	instance.testHooks.replaceWorkspaceGraph = func(*workspace.ImportGraph, string, []workspace.ImportFact) error {
		return errors.New("graph replace failed")
	}
	dependents := instance.replaceWorkspaceFile(uri.File(main).String(), syntax.Parse("vim9script\nimport './first.vim' as First\nvar Changed = 1\n"))
	if len(dependents) != 1 || dependents[0] != dependent {
		t.Fatalf("graph failure dependents = %#v, want %q", dependents, dependent)
	}
	instance.workspaceMu.Lock()
	_, indexed := instance.workspaceIndex.Source(main)
	graph := instance.workspaceGraphView
	complete := instance.workspaceIndex.Complete()
	pending := len(instance.workspacePending)
	instance.workspaceMu.Unlock()
	if indexed || graph.Has(main) || complete || pending != 0 || !graph.Ready() {
		t.Fatalf("graph failure indexed=%t graph=%t complete=%t pending=%d ready=%t", indexed, graph.Has(main), complete, pending, graph.Ready())
	}
}

func TestWorkspaceImportGraphResolvesOpenOnlyTarget(t *testing.T) {
	root := t.TempDir()
	instance := initializeWorkspaceServer(t, root)
	targetPath := mustWorkspaceCanonicalPath(t, filepath.Join(root, "openlib.vim"))
	targetURI := uri.File(targetPath)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: targetURI, Version: 1, Text: "vim9script\nexport var Value = 1\n",
	}}); err != nil {
		t.Fatal(err)
	}
	waitForImportGraph(t, instance, func(graph workspace.ImportGraphSnapshot) bool {
		return graph.Ready() && graph.Has(targetPath)
	})
	importerPath := mustWorkspaceCanonicalPath(t, filepath.Join(root, "main.vim"))
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: uri.File(importerPath), Version: 1, Text: "vim9script\nimport './openlib.vim' as Lib\necho Lib.Value\n",
	}}); err != nil {
		t.Fatal(err)
	}
	graph := waitForImportGraph(t, instance, func(graph workspace.ImportGraphSnapshot) bool {
		outgoing := graph.Outgoing(importerPath)
		return graph.Ready() && len(outgoing) == 1 && sameWorkspacePath(outgoing[0].Target, targetPath)
	})
	if incoming := graph.Incoming(targetPath); len(incoming) != 1 || !sameWorkspacePath(incoming[0].Importer, importerPath) {
		t.Fatalf("open-only target incoming = %#v", incoming)
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

func TestWorkspaceSymbolIdentityRetry(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "symbols.vim", "vim9script\nexport def CurrentSymbol()\nenddef\n")
	instance := initializeWorkspaceServer(t, root)

	t.Run("retries current result", func(t *testing.T) {
		checks := 0
		instance.testHooks.beforeWorkspaceIdentityCheck = func() {
			checks++
			if checks == 1 {
				instance.workspaceMu.Lock()
				instance.workspaceRevision++
				instance.workspaceMu.Unlock()
			}
		}
		result, err := instance.Symbols(context.Background(), &protocol.WorkspaceSymbolParams{Query: "CurrentSymbol"})
		symbols, ok := result.(protocol.WorkspaceSymbolSlice)
		if err != nil || !ok || len(symbols) != 1 || checks != 2 {
			t.Fatalf("symbols=%#v checks=%d error=%v", result, checks, err)
		}
	})

	t.Run("drops second stale empty result", func(t *testing.T) {
		checks := 0
		instance.testHooks.beforeWorkspaceIdentityCheck = func() {
			checks++
			instance.workspaceMu.Lock()
			instance.workspaceRevision++
			instance.workspaceMu.Unlock()
		}
		result, err := instance.Symbols(context.Background(), &protocol.WorkspaceSymbolParams{Query: "missing"})
		if !errors.Is(err, protocol.ErrContentModified) || result != nil || checks != 2 {
			t.Fatalf("symbols=%#v checks=%d error=%v", result, checks, err)
		}
	})
}

func TestWorkspaceResolverIsReusedUntilRootsChange(t *testing.T) {
	root := t.TempDir()
	instance := initializeWorkspaceServer(t, root)
	first := instance.captureWorkspaceNavigationState().resolver
	second := instance.captureWorkspaceNavigationState().resolver
	if first == nil || second != first {
		t.Fatalf("resolver was rebuilt without a workspace change: %p, %p", first, second)
	}
	instance.setRuntimePaths([]string{root})
	invalidated := instance.captureWorkspaceNavigationState().resolver
	if invalidated != nil {
		t.Fatalf("resolver was retained after runtimepath change: %p", invalidated)
	}
	instance.refreshWorkspaceResolver()
	refreshed := instance.captureWorkspaceNavigationState().resolver
	if refreshed == nil || refreshed == first {
		t.Fatalf("resolver was not refreshed: before=%p after=%p", first, refreshed)
	}
}

func TestServerCloseReopenRejectsPausedRestore(t *testing.T) {
	root := t.TempDir()
	path := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "restore.vim", "vim9script\nvar Disk = 1\n"))
	instance := initializeWorkspaceServer(t, root)
	documentURI := uri.File(path)
	instance.publishMu.Lock()
	instance.documents.Open(documentURI.String(), 1, "vim9script\nvar Overlay = 1\n")
	instance.removeWorkspaceURI(documentURI.String())
	delete(instance.parsed, documentURI.String())
	instance.publishMu.Unlock()

	paused := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	var capturedRevision uint64
	instance.testHooks.beforeWorkspaceRestoreRead = func(restore workspaceRestore) {
		if restore.documentURI == documentURI.String() {
			capturedRevision = restore.revision
			close(paused)
			<-release
		}
	}
	closeDone := make(chan struct{})
	var closeErr error
	go func() {
		closeErr = instance.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
		close(closeDone)
	}()
	releaseThenJoin := func() {
		releaseOnce.Do(func() { close(release) })
		waitForServerRace(t, closeDone, "workspace restore completion")
	}
	t.Cleanup(releaseThenJoin)
	waitForServerRace(t, paused, "workspace restore capture")

	currentSource := "vim9script\nvar Reopened = 2\n"
	instance.publishMu.Lock()
	currentSnapshot := instance.documents.Open(documentURI.String(), 2, currentSource)
	delete(instance.parsed, documentURI.String())
	instance.publishMu.Unlock()
	currentFile := instance.parseSnapshot(currentSnapshot)
	if currentFile == nil || currentFile.Source != currentSource {
		t.Fatalf("reopened parse = %#v", currentFile)
	}
	instance.publishMu.Lock()
	instance.replaceWorkspaceFile(documentURI.String(), currentFile)
	instance.publishMu.Unlock()
	instance.workspaceMu.Lock()
	currentRevision := instance.workspaceRevision
	instance.workspaceMu.Unlock()
	if currentRevision != capturedRevision {
		t.Fatalf("reopen changed workspace revision: got %d, want %d", currentRevision, capturedRevision)
	}

	releaseThenJoin()
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if snapshot, ok := instance.documents.Snapshot(documentURI.String()); !ok || snapshot != currentSnapshot {
		t.Fatalf("stale restore replaced reopened snapshot: %#v", snapshot)
	}
	instance.publishMu.Lock()
	cached := instance.parsed[documentURI.String()]
	instance.publishMu.Unlock()
	if cached.file == nil {
		t.Fatal("stale restore cleared reopened cache")
	}
	if cached.file != currentFile || cached.contentID != currentSnapshot.ContentID() {
		t.Fatalf("stale restore replaced reopened cache: file=%p want=%p contentID=%x want=%x", cached.file, currentFile, cached.contentID, currentSnapshot.ContentID())
	}
	instance.workspaceMu.Lock()
	source, indexed := instance.workspaceIndex.Source(path)
	graph := instance.workspaceGraphView
	instance.workspaceMu.Unlock()
	if !indexed || source != currentSource || !graph.Has(path) {
		t.Fatalf("stale restore replaced reopened workspace: indexed=%t source=%q graphHas=%t", indexed, source, graph.Has(path))
	}
}

func TestServerRebuildRejectsCapturedSnapshotAfterOpenEdit(t *testing.T) {
	root := t.TempDir()
	path := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "rebuild.vim", "vim9script\nvar Disk = 1\n"))
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.workspaceDelay = 0
	instance.setWorkspaceRoots([]string{root})
	documentURI := uri.File(path)
	oldSource := "vim9script\nvar Old = 1\n"
	instance.publishMu.Lock()
	oldSnapshot := instance.documents.Open(documentURI.String(), 1, oldSource)
	instance.publishMu.Unlock()

	paused := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	hookCalls := 0
	instance.testHooks.beforeWorkspaceBuild = func(snapshots []*text.Snapshot) {
		hookCalls++
		if len(snapshots) == 1 && snapshots[0] == oldSnapshot {
			close(paused)
			<-release
		}
	}
	instance.scheduleWorkspaceRebuild()
	workspaceDone := make(chan struct{})
	go func() {
		instance.workspaceWG.Wait()
		close(workspaceDone)
	}()
	releaseThenJoin := func() {
		releaseOnce.Do(func() { close(release) })
		waitForServerRace(t, workspaceDone, "workspace rebuild completion")
	}
	t.Cleanup(releaseThenJoin)
	waitForServerRace(t, paused, "workspace rebuild snapshot capture")

	currentSource := "vim9script\nvar Current = 2\n"
	instance.publishMu.Lock()
	currentSnapshot, changed, err := instance.documents.Change(documentURI.String(), 2, text.UTF16, []text.Change{{Text: currentSource}})
	instance.publishMu.Unlock()
	if err != nil || !changed {
		t.Fatalf("direct document change: changed=%t err=%v", changed, err)
	}
	releaseThenJoin()

	if snapshot, ok := instance.documents.Snapshot(documentURI.String()); !ok || snapshot != currentSnapshot || snapshot.Text() != currentSource {
		t.Fatalf("final snapshot = %#v", snapshot)
	}
	instance.workspaceMu.Lock()
	source, indexed := instance.workspaceIndex.Source(path)
	graph := instance.workspaceGraphView
	built := instance.workspaceBuilt
	instance.workspaceMu.Unlock()
	if hookCalls != 2 || !built || !indexed || source != currentSource || !graph.Has(path) {
		t.Fatalf("stale rebuild published: hooks=%d built=%t indexed=%t source=%q graphHas=%t", hookCalls, built, indexed, source, graph.Has(path))
	}
}

func TestWorkspaceRebuildDebouncesBurst(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	delayStarted := make(chan struct{})
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorker := func() {
		releaseOnce.Do(func() { close(release) })
	}
	t.Cleanup(func() {
		releaseWorker()
		instance.workspaceWG.Wait()
	})
	instance.testHooks.beforeWorkspaceRebuildDelay = func() { close(delayStarted) }
	instance.testHooks.beforeWorkspaceBuild = func([]*text.Snapshot) {
		close(started)
		<-release
	}
	instance.scheduleWorkspaceRebuild()
	waitForServerRace(t, delayStarted, "workspace rebuild debounce timer")
	time.Sleep(80 * time.Millisecond)
	instance.scheduleWorkspaceRebuild()
	select {
	case <-started:
		t.Fatal("workspace rebuild ignored the debounce window")
	case <-time.After(40 * time.Millisecond):
	}
	waitForServerRace(t, started, "debounced workspace rebuild")
	releaseWorker()
}

func TestWorkspaceIndexReportsWorkDoneProgress(t *testing.T) {
	root := t.TempDir()
	runtimeRoot := mustWorkspaceCanonicalPath(t, root)
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	endedAfterPublish := false
	progress := &workspaceProgressClient{creates: make(chan protocol.ProgressToken, 1), updates: make(chan *protocol.ProgressParams, 3)}
	progress.onProgress = func(params *protocol.ProgressParams) {
		var end protocol.WorkDoneProgressEnd
		if protocol.Unmarshal(params.Value, &end) != nil || end.Kind != "end" {
			return
		}
		instance.workspaceMu.Lock()
		endedAfterPublish = instance.workspaceBuilt && !instance.workspaceRunning
		instance.workspaceMu.Unlock()
	}
	instance.client = progress
	supported := true
	rootURI := uri.File(root)
	options, err := json.Marshal(map[string]any{"runtimepath": []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI:               &rootURI,
		InitializationOptions: protocol.LSPAny(options),
		Capabilities:          protocol.ClientCapabilities{Window: &protocol.WindowClientCapabilities{WorkDoneProgress: &supported}},
	}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceDelay = 0
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	var token protocol.ProgressToken
	select {
	case token = <-progress.creates:
	case <-time.After(10 * time.Second):
		t.Fatal("workspace index did not create a progress token")
	}
	begin := waitForWorkspaceProgress(t, progress.updates)
	var beginValue protocol.WorkDoneProgressBegin
	if err := protocol.Unmarshal(begin.Value, &beginValue); err != nil {
		t.Fatal(err)
	}
	if begin.Token != token || beginValue.Kind != "begin" || beginValue.Title != "Indexing workspace" {
		t.Fatalf("progress begin = %#v, value = %#v", begin, beginValue)
	}
	report := waitForWorkspaceProgress(t, progress.updates)
	var reportValue protocol.WorkDoneProgressReport
	if err := protocol.Unmarshal(report.Value, &reportValue); err != nil {
		t.Fatal(err)
	}
	if report.Token != token || reportValue.Kind != "report" || reportValue.Message == nil || !strings.Contains(*reportValue.Message, runtimeRoot) || reportValue.Percentage != nil {
		t.Fatalf("progress report = %#v, value = %#v", report, reportValue)
	}
	instance.workspaceWG.Wait()
	end := waitForWorkspaceProgress(t, progress.updates)
	var endValue protocol.WorkDoneProgressEnd
	if err := protocol.Unmarshal(end.Value, &endValue); err != nil {
		t.Fatal(err)
	}
	if end.Token != token || endValue.Kind != "end" {
		t.Fatalf("progress end = %#v, value = %#v", end, endValue)
	}
	if !endedAfterPublish {
		t.Fatal("workspace progress ended before the index was published")
	}
}

func TestWorkspaceIndexProgressIsCapabilityGated(t *testing.T) {
	root := t.TempDir()
	progress := &workspaceProgressClient{creates: make(chan protocol.ProgressToken, 1), updates: make(chan *protocol.ProgressParams, 3)}
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.client = progress
	rootURI := uri.File(root)
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI:               &rootURI,
		InitializationOptions: protocol.LSPAny([]byte(`{"runtimepath":["` + root + `"]}`)),
	}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceDelay = 0
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	select {
	case created := <-progress.creates:
		t.Fatalf("progress token created without capability: %v", created)
	default:
	}
	select {
	case update := <-progress.updates:
		t.Fatalf("progress notification sent without capability: %#v", update)
	default:
	}
}

func TestWorkspaceIndexProgressReportFailureStillEnds(t *testing.T) {
	root := t.TempDir()
	progress := &workspaceProgressClient{creates: make(chan protocol.ProgressToken, 1), updates: make(chan *protocol.ProgressParams, 3)}
	progress.progress = func(_ context.Context, params *protocol.ProgressParams) error {
		var report protocol.WorkDoneProgressReport
		if protocol.Unmarshal(params.Value, &report) == nil && report.Kind == "report" {
			return errors.New("report unavailable")
		}
		return nil
	}
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.client = progress
	supported := true
	rootURI := uri.File(root)
	options, err := json.Marshal(map[string]any{"runtimepath": []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI:               &rootURI,
		InitializationOptions: protocol.LSPAny(options),
		Capabilities:          protocol.ClientCapabilities{Window: &protocol.WindowClientCapabilities{WorkDoneProgress: &supported}},
	}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceDelay = 0
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	begin := waitForWorkspaceProgress(t, progress.updates)
	report := waitForWorkspaceProgress(t, progress.updates)
	end := waitForWorkspaceProgress(t, progress.updates)
	var beginValue protocol.WorkDoneProgressBegin
	var reportValue protocol.WorkDoneProgressReport
	var endValue protocol.WorkDoneProgressEnd
	if protocol.Unmarshal(begin.Value, &beginValue) != nil || beginValue.Kind != "begin" || protocol.Unmarshal(report.Value, &reportValue) != nil || reportValue.Kind != "report" || protocol.Unmarshal(end.Value, &endValue) != nil || endValue.Kind != "end" || begin.Token != report.Token || report.Token != end.Token {
		t.Fatalf("progress after report failure = %#v, %#v, %#v", begin, report, end)
	}
}

func TestWorkspaceIndexProgressReportsBoundedRuntimeRoots(t *testing.T) {
	roots := make([]string, workspaceProgressReportLimit+1)
	for index := range roots {
		roots[index] = t.TempDir()
	}
	reported := make([]string, 0, workspaceProgressReportLimit)
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.buildWorkspaceIndex(context.Background(), roots, roots, nil, nil, func(root string) {
		reported = append(reported, root)
	})
	if len(reported) != workspaceProgressReportLimit {
		t.Fatalf("runtime root reports = %d, want %d", len(reported), workspaceProgressReportLimit)
	}
	for index, root := range reported {
		if root != roots[index] {
			t.Fatalf("runtime root report %d = %q, want %q", index, root, roots[index])
		}
	}
}

func TestWorkspaceIndexProgressCreateTimesOut(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.client = &workspaceProgressClient{create: func(ctx context.Context, _ *protocol.WorkDoneProgressCreateParams) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	instance.workspaceProgress = true
	instance.workspaceDelay = 0
	instance.testHooks.workspaceProgressTimeout = time.Millisecond
	instance.scheduleWorkspaceRebuild()
	done := make(chan struct{})
	go func() {
		instance.workspaceWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("workspace rebuild waited indefinitely for progress creation")
	}
}

func TestWorkspaceIndexProgressDoesNotBlockOnUncooperativeClient(t *testing.T) {
	root := t.TempDir()
	progress := &blockingWorkspaceProgressClient{
		beginStarted: make(chan struct{}),
		endStarted:   make(chan struct{}),
		releaseBegin: make(chan struct{}),
		events:       make(chan string, 2),
	}
	instance := New(nil, nil, io.Discard)
	instance.setWorkspaceRoots([]string{root})
	instance.setRuntimePaths([]string{root})
	instance.client = progress
	instance.workspaceProgress = true
	instance.workspaceDelay = 0
	instance.testHooks.workspaceProgressTimeout = time.Millisecond
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(progress.releaseBegin) }) }
	t.Cleanup(func() {
		release()
		instance.stopAnalysis()
	})

	instance.scheduleWorkspaceRebuild()
	waitForServerRace(t, progress.beginStarted, "blocking workspace progress begin")
	done := make(chan struct{})
	go func() {
		instance.workspaceWG.Wait()
		close(done)
	}()
	waitForServerRace(t, done, "workspace rebuild after blocking progress")
	select {
	case event := <-progress.events:
		t.Fatalf("timed-out notification escaped before release: %q", event)
	default:
	}
	// The queued end cannot overtake the blocked begin. Releasing the begin
	// must deliver the token's notifications in protocol order.
	release()
	if event := <-progress.events; event != "begin" {
		t.Fatalf("first progress event = %q, want begin", event)
	}
	waitForServerRace(t, progress.endStarted, "best-effort workspace progress end")
	if event := <-progress.events; event != "end" {
		t.Fatalf("second progress event = %q, want end", event)
	}
	if !progress.endHasDeadline {
		t.Fatal("best-effort progress end had no deadline")
	}
	if progress.reports != 0 {
		t.Fatalf("reports sent after begin timeout: %d", progress.reports)
	}
	instance.workspaceMu.Lock()
	built, running := instance.workspaceBuilt, instance.workspaceRunning
	instance.workspaceMu.Unlock()
	if !built || running {
		t.Fatalf("blocking progress stalled workspace: built=%t running=%t", built, running)
	}
	if instance.workspaceProgressEnabled() {
		t.Fatal("progress remained enabled after a timed-out client call")
	}
}

func TestWorkspaceIndexProgressReportDoesNotArriveAfterEnd(t *testing.T) {
	root := mustWorkspaceCanonicalPath(t, t.TempDir())
	reportStarted := make(chan struct{})
	releaseReport := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseReport) }) }
	progress := &workspaceProgressClient{creates: make(chan protocol.ProgressToken, 1), updates: make(chan *protocol.ProgressParams, 3)}
	progress.onProgress = func(params *protocol.ProgressParams) {
		var report protocol.WorkDoneProgressReport
		if protocol.Unmarshal(params.Value, &report) == nil && report.Kind == "report" {
			close(reportStarted)
			<-releaseReport
		}
	}
	instance := New(nil, nil, io.Discard)
	instance.setWorkspaceRoots([]string{root})
	instance.setRuntimePaths([]string{root})
	instance.client = progress
	instance.workspaceProgress = true
	instance.workspaceDelay = 0
	instance.testHooks.workspaceProgressTimeout = time.Millisecond
	t.Cleanup(func() {
		release()
		instance.stopAnalysis()
	})

	instance.scheduleWorkspaceRebuild()
	begin := waitForWorkspaceProgress(t, progress.updates)
	var beginValue protocol.WorkDoneProgressBegin
	if protocol.Unmarshal(begin.Value, &beginValue) != nil || beginValue.Kind != "begin" {
		t.Fatalf("progress begin = %#v", begin)
	}
	waitForServerRace(t, reportStarted, "blocking workspace progress report")
	instance.workspaceWG.Wait()
	select {
	case update := <-progress.updates:
		t.Fatalf("terminal progress overtook blocked report: %#v", update)
	default:
	}

	release()
	report := waitForWorkspaceProgress(t, progress.updates)
	end := waitForWorkspaceProgress(t, progress.updates)
	var reportValue protocol.WorkDoneProgressReport
	var endValue protocol.WorkDoneProgressEnd
	if protocol.Unmarshal(report.Value, &reportValue) != nil || reportValue.Kind != "report" || protocol.Unmarshal(end.Value, &endValue) != nil || endValue.Kind != "end" || begin.Token != report.Token || report.Token != end.Token {
		t.Fatalf("late progress ordering = %#v, %#v, %#v", begin, report, end)
	}
}

func TestWorkspaceIndexDiscoveryHonorsCancellationBeforeRuntimeRoot(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "plugin/cancel.vim", "vim9script\nvar Cancelled = 1\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	instance := New(nil, nil, io.Discard)
	index, graph, diskFiles, warnings := instance.buildWorkspaceIndex(ctx, []string{root}, []string{root}, nil, nil, func(string) {
		cancel()
	})
	if index.FileCount() != 0 || graph.Snapshot().Ready() || len(diskFiles) != 0 || len(warnings) != 0 {
		t.Fatalf("cancelled discovery indexed files=%d graph-ready=%t disk=%#v warnings=%#v", index.FileCount(), graph.Snapshot().Ready(), diskFiles, warnings)
	}
}

func TestWorkspaceIndexProgressTokensAreUnique(t *testing.T) {
	progress := &workspaceProgressClient{creates: make(chan protocol.ProgressToken, 2), updates: make(chan *protocol.ProgressParams, 4)}
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.client = progress
	instance.workspaceProgress = true
	first := instance.startWorkspaceIndexProgress()
	second := instance.startWorkspaceIndexProgress()
	instance.finishWorkspaceIndexProgress(first)
	instance.finishWorkspaceIndexProgress(second)
	if first == nil || second == nil || first.token == second.token {
		t.Fatalf("workspace progress tokens reused: %v, %v", first, second)
	}
}

func TestWaitForWorkspaceIndex(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.workspaceDelay = 0
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorker := func() {
		releaseOnce.Do(func() { close(release) })
	}
	t.Cleanup(func() {
		releaseWorker()
		instance.workspaceWG.Wait()
	})
	instance.testHooks.beforeWorkspaceBuild = func([]*text.Snapshot) {
		close(started)
		<-release
	}
	instance.scheduleWorkspaceRebuild()
	result := make(chan error, 1)
	go func() { result <- instance.waitForWorkspaceIndex(context.Background()) }()
	waitForServerRace(t, started, "workspace rebuild start")
	select {
	case err := <-result:
		t.Fatalf("wait returned before rebuild completed: %v", err)
	default:
	}
	releaseWorker()
	if err := <-result; err != nil {
		t.Fatalf("wait result: %v", err)
	}
}

func TestWaitForWorkspaceIndexPending(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	waiting := make(chan struct{})
	instance.testHooks.beforeWorkspaceIndexWait = func() { close(waiting) }
	instance.workspaceMu.Lock()
	instance.workspacePending["file.vim"] = struct{}{}
	instance.notifyWorkspaceIndexChangedLocked()
	instance.workspaceMu.Unlock()
	result := make(chan error, 1)
	go func() { result <- instance.waitForWorkspaceIndex(context.Background()) }()
	waitForServerRace(t, waiting, "workspace pending wait")
	select {
	case err := <-result:
		t.Fatalf("wait returned while index update was pending: %v", err)
	default:
	}
	instance.workspaceMu.Lock()
	delete(instance.workspacePending, "file.vim")
	instance.notifyWorkspaceIndexChangedLocked()
	instance.workspaceMu.Unlock()
	if err := <-result; err != nil {
		t.Fatalf("wait result: %v", err)
	}
}

func TestWaitForWorkspaceIndexTimesOut(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.testHooks.workspaceIndexWaitTimeout = time.Millisecond
	instance.workspaceMu.Lock()
	instance.workspaceRunning = true
	instance.workspaceMu.Unlock()
	err := instance.waitForWorkspaceIndex(context.Background())
	var rpcError *jsonrpc2.Error
	if !errors.As(err, &rpcError) || rpcError.Code != jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed) || rpcError.Message != "workspace index did not become ready within 5s" {
		t.Fatalf("timeout error = %#v", err)
	}
}

func TestWaitForWorkspaceIndexCancellableRequestHasNoTimeout(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.testHooks.workspaceIndexWaitTimeout = time.Millisecond
	waiting := make(chan struct{})
	var waitingOnce sync.Once
	instance.testHooks.beforeWorkspaceIndexWait = func() {
		waitingOnce.Do(func() { close(waiting) })
	}
	instance.workspaceMu.Lock()
	instance.workspaceRunning = true
	instance.workspaceMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	result := make(chan error, 1)
	go func() { result <- instance.waitForWorkspaceIndex(ctx) }()
	waitForServerRace(t, waiting, "workspace index wait")
	time.Sleep(10 * time.Millisecond)
	select {
	case err := <-result:
		t.Fatalf("cancellable wait returned at fallback timeout: %v", err)
	default:
	}
	cancel()
	if err := <-result; !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Fatalf("cancel error = %v", err)
	}
}

func TestDocumentRequestsDoNotWaitForWorkspaceIndex(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nclass Item\nendclass\ndef Local(value: number): number\n  return value\nenddef\nvar result = Local(1)\n")
	t.Cleanup(instance.stopAnalysis)
	instance.testHooks.workspaceIndexWaitTimeout = time.Millisecond
	instance.workspaceMu.Lock()
	instance.workspaceRunning = true
	instance.workspaceMu.Unlock()

	textDocument := protocol.TextDocumentIdentifier{URI: documentURI}
	position := protocol.Position{Line: 6, Character: 15}
	positionParams := protocol.TextDocumentPositionParams{TextDocument: textDocument, Position: position}
	requests := []struct {
		name string
		run  func() error
	}{
		{name: "document symbols", run: func() error {
			_, err := instance.DocumentSymbol(context.Background(), &protocol.DocumentSymbolParams{TextDocument: textDocument})
			return err
		}},
		{name: "document links", run: func() error {
			_, err := instance.DocumentLink(context.Background(), &protocol.DocumentLinkParams{TextDocument: textDocument})
			return err
		}},
		{name: "signature help", run: func() error {
			_, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: positionParams})
			return err
		}},
		{name: "definition", run: func() error {
			_, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: positionParams})
			return err
		}},
		{name: "references", run: func() error {
			_, err := instance.References(context.Background(), &protocol.ReferenceParams{TextDocumentPositionParams: positionParams})
			return err
		}},
		{name: "document highlights", run: func() error {
			_, err := instance.DocumentHighlight(context.Background(), &protocol.DocumentHighlightParams{TextDocumentPositionParams: positionParams})
			return err
		}},
		{name: "hover", run: func() error {
			_, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: positionParams})
			return err
		}},
		{name: "type definition", run: func() error {
			_, err := instance.TypeDefinition(context.Background(), &protocol.TypeDefinitionParams{TextDocumentPositionParams: positionParams})
			return err
		}},
		{name: "implementation", run: func() error {
			_, err := instance.Implementation(context.Background(), &protocol.ImplementationParams{TextDocumentPositionParams: positionParams})
			return err
		}},
		{name: "prepare call hierarchy", run: func() error {
			_, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: positionParams})
			return err
		}},
		{name: "prepare type hierarchy", run: func() error {
			_, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: textDocument, Position: protocol.Position{Line: 1, Character: 7},
			}})
			return err
		}},
	}
	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			if err := request.run(); err != nil {
				t.Fatalf("request was blocked by workspace index work: %v", err)
			}
		})
	}
}

func TestServerRebuildCancellationDoesNotPublishIndex(t *testing.T) {
	root := t.TempDir()
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.setWorkspaceRoots([]string{root})
	beginSent := make(chan struct{})
	releaseBegin := make(chan struct{})
	endDeadline := make(chan bool, 1)
	progress := &workspaceProgressClient{creates: make(chan protocol.ProgressToken, 1), updates: make(chan *protocol.ProgressParams, 2)}
	progress.progress = func(ctx context.Context, params *protocol.ProgressParams) error {
		var end protocol.WorkDoneProgressEnd
		if protocol.Unmarshal(params.Value, &end) == nil && end.Kind == "end" {
			_, hasDeadline := ctx.Deadline()
			endDeadline <- hasDeadline
		}
		return nil
	}
	progress.onProgress = func(params *protocol.ProgressParams) {
		var begin protocol.WorkDoneProgressBegin
		if protocol.Unmarshal(params.Value, &begin) == nil && begin.Kind == "begin" {
			close(beginSent)
			<-releaseBegin
		}
	}
	instance.client = progress
	instance.workspaceProgress = true
	instance.workspaceDelay = 0
	instance.scheduleWorkspaceRebuild()
	waitForServerRace(t, beginSent, "workspace progress begin")
	instance.cancelAnalysis()
	close(releaseBegin)
	instance.workspaceWG.Wait()
	begin := waitForWorkspaceProgress(t, progress.updates)
	end := waitForWorkspaceProgress(t, progress.updates)
	var beginValue protocol.WorkDoneProgressBegin
	var endValue protocol.WorkDoneProgressEnd
	if protocol.Unmarshal(begin.Value, &beginValue) != nil || beginValue.Kind != "begin" || protocol.Unmarshal(end.Value, &endValue) != nil || endValue.Kind != "end" || begin.Token != end.Token {
		t.Fatalf("cancelled progress updates = %#v, %#v", begin, end)
	}
	if !<-endDeadline {
		t.Fatal("cancelled progress end had no deadline")
	}
	instance.workspaceMu.Lock()
	running, built, index := instance.workspaceRunning, instance.workspaceBuilt, instance.workspaceIndex
	instance.workspaceMu.Unlock()
	if running || built || index == nil || index.Complete() {
		t.Fatalf("cancelled rebuild published: running=%t built=%t index=%#v", running, built, index)
	}
}

func BenchmarkWorkspaceRebuild(b *testing.B) {
	root := b.TempDir()
	paths := make([]string, 0, 32)
	totalBytes := 0
	for fileNumber := range 32 {
		var source strings.Builder
		source.WriteString("vim9script\n")
		for functionNumber := range 64 {
			fmt.Fprintf(&source, "def Benchmark_%02d_%02d(): number\n  return %d\nenddef\n", fileNumber, functionNumber, functionNumber)
		}
		content := source.String()
		path := filepath.Join(root, fmt.Sprintf("benchmark-%02d.vim", fileNumber))
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			b.Fatal(err)
		}
		canonical, err := workspace.CanonicalPath(path)
		if err != nil {
			b.Fatal(err)
		}
		paths = append(paths, canonical)
		totalBytes += len(content)
	}
	canonicalRoot, err := workspace.CanonicalPath(root)
	if err != nil {
		b.Fatal(err)
	}
	roots := []string{canonicalRoot}
	resolver := workspacePathResolver(roots, nil)
	if resolver == nil {
		b.Fatal("workspace resolver is nil")
	}
	instance := New(nil, nil, io.Discard)
	index, graph, diskFiles, warnings := instance.buildWorkspaceIndex(context.Background(), roots, nil, resolver, nil)
	if len(diskFiles) != len(paths) || !index.Complete() || !graph.Snapshot().Ready() || len(warnings) != 0 {
		b.Fatalf("workspace preflight: diskFiles=%d complete=%t ready=%t warnings=%#v", len(diskFiles), index.Complete(), graph.Snapshot().Ready(), warnings)
	}
	for _, path := range paths {
		if source, ok := index.Source(path); !ok || source == "" || !graph.Snapshot().Has(path) {
			b.Fatalf("workspace preflight missing %q: indexed=%t source=%q graph=%t", path, ok, source, graph.Snapshot().Has(path))
		}
	}
	b.ReportAllocs()
	b.SetBytes(int64(totalBytes))
	for b.Loop() {
		instance.buildWorkspaceIndex(context.Background(), roots, nil, resolver, nil)
	}
}

func BenchmarkRuntimepathIndexing(b *testing.B) {
	previousProcs := runtime.GOMAXPROCS(1)
	b.Cleanup(func() { runtime.GOMAXPROCS(previousProcs) })
	var runtimePaths []string
	totalBytes := 0
	for rootNumber := range 2 {
		root := filepath.Join(b.TempDir(), fmt.Sprintf("runtime-%d", rootNumber))
		for fileNumber := range 128 {
			directory := "plugin"
			filename := fmt.Sprintf("file-%03d.vim", fileNumber)
			content := fmt.Sprintf("function! BenchmarkRoot%dFunc%d(value)\n  return a:value\nendfunction\n", rootNumber, fileNumber)
			if fileNumber%2 != 0 {
				directory = "autoload/benchmark"
				filename = fmt.Sprintf("file_%03d.vim", fileNumber)
				content = fmt.Sprintf("function! benchmark#file_%03d#Func(value)\n  return a:value\nendfunction\n", fileNumber)
			}
			path := filepath.Join(root, directory, filename)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				b.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				b.Fatal(err)
			}
			totalBytes += len(content)
		}
		canonical, err := workspace.CanonicalPath(root)
		if err != nil {
			b.Fatal(err)
		}
		runtimePaths = append(runtimePaths, canonical)
	}
	roots := workspaceIndexRoots(nil, runtimePaths)
	resolver := workspacePathResolver(nil, runtimePaths)
	instance := New(nil, nil, io.Discard)
	index, graph, diskFiles, warnings := instance.buildWorkspaceIndex(context.Background(), roots, runtimePaths, resolver, nil)
	if len(diskFiles) != 256 || index.FileCount() != 256 || !index.Complete() || !graph.Snapshot().Ready() || len(warnings) != 0 {
		b.Fatalf("runtimepath preflight: disk=%d index=%d complete=%t ready=%t warnings=%#v", len(diskFiles), index.FileCount(), index.Complete(), graph.Snapshot().Ready(), warnings)
	}
	b.ReportAllocs()
	b.SetBytes(int64(totalBytes))
	b.ResetTimer()
	for b.Loop() {
		instance.buildWorkspaceIndex(context.Background(), roots, runtimePaths, resolver, nil)
	}
}

func BenchmarkReverseDependentReanalysis(b *testing.B) {
	previousProcs := runtime.GOMAXPROCS(1)
	b.Cleanup(func() { runtime.GOMAXPROCS(previousProcs) })
	root := b.TempDir()
	paths := make([]string, 32)
	for index := len(paths) - 1; index >= 0; index-- {
		source := "vim9script\nexport var Value = 1\n"
		if index+1 < len(paths) {
			source = fmt.Sprintf("vim9script\nimport './file-%02d.vim' as Next\nexport var Value = Next.Value\n", index+1)
		}
		path := filepath.Join(root, fmt.Sprintf("file-%02d.vim", index))
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			b.Fatal(err)
		}
		canonical, err := workspace.CanonicalPath(path)
		if err != nil {
			b.Fatal(err)
		}
		paths[index] = canonical
	}
	canonicalRoot, err := workspace.CanonicalPath(root)
	if err != nil {
		b.Fatal(err)
	}
	roots := []string{canonicalRoot}
	resolver := workspacePathResolver(roots, nil)
	instance := New(nil, nil, io.Discard)
	b.Cleanup(instance.stopAnalysis)
	for index, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		instance.documents.Open(uri.File(path).String(), int32(index+1), string(source))
	}
	index, graph, diskFiles, warnings := instance.buildWorkspaceIndex(context.Background(), roots, nil, resolver, instance.documents.Snapshots())
	view := graph.Snapshot()
	dependents := view.ReverseDependents(paths[len(paths)-1])
	if len(diskFiles) != len(paths) || len(dependents) != len(paths)-1 || !index.Complete() || !view.Ready() || len(warnings) != 0 {
		b.Fatalf("dependent preflight: disk=%d dependents=%d complete=%t ready=%t warnings=%#v", len(diskFiles), len(dependents), index.Complete(), view.Ready(), warnings)
	}
	instance.workspaceMu.Lock()
	instance.workspaceRoots = roots
	instance.workspaceResolver = resolver
	instance.workspaceIndex = index
	instance.workspaceGraph = graph
	instance.workspaceGraphView = view
	instance.workspaceFiles = diskFiles
	instance.workspaceBuilt = true
	instance.workspaceMu.Unlock()
	instance.analysisMu.Lock()
	instance.analysisWorkers = 1
	instance.analysisMu.Unlock()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		instance.workspaceMu.Lock()
		dependents = instance.workspaceGraphView.ReverseDependents(paths[len(paths)-1])
		instance.workspaceMu.Unlock()
		for _, path := range dependents {
			instance.analyzeDocument(uri.File(path).String())
		}
	}
}

func initializeWorkspaceServer(t *testing.T, root string) *Server {
	t.Helper()
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	rootURI := uri.File(root)
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI:               &rootURI,
		InitializationOptions: protocol.LSPAny([]byte(`{"runtimepath":[]}`)),
	}); err != nil {
		t.Fatal(err)
	}
	// Tests using this helper wait for index completion explicitly. Keeping the
	// production debounce only adds wall-clock delay; debounce behavior has its
	// own focused test below.
	instance.workspaceDelay = 0
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
	deadline := time.Now().Add(30 * time.Second)
	for {
		matches := workspaceSymbols(t, instance, query)
		symbols := make(protocol.WorkspaceSymbolSlice, 0, len(matches))
		for _, symbol := range matches {
			if symbol.Name == query {
				symbols = append(symbols, symbol)
			}
		}
		if len(symbols) == count {
			return symbols
		}
		if time.Now().After(deadline) {
			t.Fatalf("workspace symbols named %q: got %d, want %d (all fuzzy matches: %#v)", query, len(symbols), count, matches)
		}
		time.Sleep(time.Millisecond)
	}
}

func currentImportGraph(instance *Server) workspace.ImportGraphSnapshot {
	instance.workspaceMu.Lock()
	defer instance.workspaceMu.Unlock()
	return instance.workspaceGraphView
}

func waitForImportGraph(t *testing.T, instance *Server, ready func(workspace.ImportGraphSnapshot) bool) workspace.ImportGraphSnapshot {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		graph := currentImportGraph(instance)
		if ready(graph) {
			return graph
		}
		if time.Now().After(deadline) {
			t.Fatalf("import graph did not reach expected state: ready=%t revision=%d", graph.Ready(), graph.Revision())
		}
		time.Sleep(time.Millisecond)
	}
}

func writeWorkspaceFile(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustWorkspaceCanonicalPath(t *testing.T, path string) string {
	t.Helper()
	canonical, err := workspace.CanonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

type watchRegistrationClient struct {
	protocol.UnimplementedClient
	registrations   []*protocol.RegistrationParams
	unregistrations []*protocol.UnregistrationParams
}

type blockingWorkspaceProgressClient struct {
	protocol.UnimplementedClient
	beginStarted   chan struct{}
	endStarted     chan struct{}
	releaseBegin   chan struct{}
	endHasDeadline bool
	reports        int
	events         chan string
}

func (c *blockingWorkspaceProgressClient) WorkDoneProgressCreate(_ context.Context, _ *protocol.WorkDoneProgressCreateParams) error {
	return nil
}

func (c *blockingWorkspaceProgressClient) Progress(ctx context.Context, params *protocol.ProgressParams) error {
	var begin protocol.WorkDoneProgressBegin
	if protocol.Unmarshal(params.Value, &begin) == nil && begin.Kind == "begin" {
		close(c.beginStarted)
		<-c.releaseBegin
		c.events <- "begin"
		return nil
	}
	var end protocol.WorkDoneProgressEnd
	if protocol.Unmarshal(params.Value, &end) == nil && end.Kind == "end" {
		_, c.endHasDeadline = ctx.Deadline()
		c.events <- "end"
		close(c.endStarted)
		return nil
	}
	var report protocol.WorkDoneProgressReport
	if protocol.Unmarshal(params.Value, &report) == nil && report.Kind == "report" {
		c.reports++
	}
	return nil
}

type workspaceProgressClient struct {
	protocol.UnimplementedClient
	creates    chan protocol.ProgressToken
	updates    chan *protocol.ProgressParams
	create     func(context.Context, *protocol.WorkDoneProgressCreateParams) error
	progress   func(context.Context, *protocol.ProgressParams) error
	onProgress func(*protocol.ProgressParams)
}

func (c *workspaceProgressClient) WorkDoneProgressCreate(ctx context.Context, params *protocol.WorkDoneProgressCreateParams) error {
	if c.create != nil {
		return c.create(ctx, params)
	}
	c.creates <- params.Token
	return nil
}

func (c *workspaceProgressClient) Progress(ctx context.Context, params *protocol.ProgressParams) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.onProgress != nil {
		c.onProgress(params)
	}
	c.updates <- params
	if c.progress != nil {
		return c.progress(ctx, params)
	}
	return nil
}

func waitForWorkspaceProgress(t *testing.T, updates <-chan *protocol.ProgressParams) *protocol.ProgressParams {
	t.Helper()
	select {
	case params := <-updates:
		return params
	case <-time.After(10 * time.Second):
		t.Fatal("workspace progress notification not received")
		return nil
	}
}

func (c *watchRegistrationClient) RegisterCapability(_ context.Context, params *protocol.RegistrationParams) error {
	c.registrations = append(c.registrations, params)
	return nil
}

func (c *watchRegistrationClient) UnregisterCapability(_ context.Context, params *protocol.UnregistrationParams) error {
	c.unregistrations = append(c.unregistrations, params)
	return nil
}

func assertWatchRegistration(t *testing.T, params *protocol.RegistrationParams, roots []string) {
	t.Helper()
	registration := params.Registrations[0]
	if registration.ID != fileWatchRegistrationID || registration.Method != protocol.MethodWorkspaceDidChangeWatchedFiles {
		t.Fatalf("registration = %#v", registration)
	}
	var options protocol.DidChangeWatchedFilesRegistrationOptions
	if err := protocol.Unmarshal(registration.RegisterOptions, &options); err != nil {
		t.Fatal(err)
	}
	if len(options.Watchers) != len(roots) {
		t.Fatalf("watchers = %#v", options.Watchers)
	}
	kind := protocol.WatchKindCreate | protocol.WatchKindChange | protocol.WatchKindDelete
	wantRoots := normalizeWorkspaceRoots(roots)
	for index, root := range wantRoots {
		pattern, ok := options.Watchers[index].GlobPattern.(*protocol.RelativePattern)
		if !ok || pattern.BaseURI != protocol.URI(uri.File(root)) || pattern.Pattern != protocol.Pattern("**/*.vim") || options.Watchers[index].Kind != kind {
			t.Fatalf("watcher %d = %#v", index, options.Watchers[index])
		}
	}
}

func TestRemainingWorkspaceIndexCapacityCreditsRemovedOpenFileAtLimit(t *testing.T) {
	activeRoot := mustWorkspaceCanonicalPath(t, t.TempDir())
	removedRoot := mustWorkspaceCanonicalPath(t, t.TempDir())
	diskSource := "vim9script\nvar Disk = 1\n"
	openSource := "vim9script\nvar Open = 1\n"
	diskPath := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, activeRoot, "plugin/disk.vim", diskSource))
	openPath := mustWorkspaceCanonicalPath(t, filepath.Join(removedRoot, "plugin", "open.vim"))
	index := workspace.NewIndex(2, len(diskSource)+len(openSource))
	if err := index.Replace(diskPath, syntax.Parse(diskSource)); err != nil {
		t.Fatal(err)
	}
	if err := index.Replace(openPath, syntax.Parse(openSource)); err != nil {
		t.Fatal(err)
	}
	workspaceFiles := map[string]struct{}{diskPath: {}}
	openByPath := map[string]*text.Snapshot{diskPath: nil, openPath: nil}
	files, bytes := remainingWorkspaceIndexCapacity(index, workspaceFiles, openByPath, []string{activeRoot}, 2, len(diskSource)+len(openSource))
	if files != 1 || bytes != len(openSource) {
		t.Fatalf("remaining capacity = %d files, %d bytes, want 1 file, %d bytes", files, bytes, len(openSource))
	}
}
