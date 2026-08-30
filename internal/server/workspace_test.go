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
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/workspace"
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
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	if symbols := workspaceSymbols(t, instance, "firstRuntimeName"); len(symbols) != 1 {
		t.Fatalf("initialized runtimepath symbols = %#v", symbols)
	}
	if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{secondRuntime}}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	if symbols := workspaceSymbols(t, instance, "firstRuntimeName"); len(symbols) != 0 {
		t.Fatalf("old runtimepath symbols = %#v", symbols)
	}
	if symbols := workspaceSymbols(t, instance, "secondRuntimeName"); len(symbols) != 1 {
		t.Fatalf("updated runtimepath symbols = %#v", symbols)
	}
}

func TestInitializedRegistersVimWatchersAndRuntimepathRefreshesRegistration(t *testing.T) {
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
	if len(client.registrations) != 1 || len(client.registrations[0].Registrations) != 1 {
		t.Fatalf("registrations = %#v", client.registrations)
	}
	assertWatchRegistration(t, client.registrations[0], []string{workspaceRoot, firstRuntime})
	if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{secondRuntime}}); err != nil {
		t.Fatal(err)
	}
	instance.watchWG.Wait()
	if len(client.unregistrations) != 1 || len(client.registrations) != 2 {
		t.Fatalf("registrations = %d, unregistrations = %d", len(client.registrations), len(client.unregistrations))
	}
	unregistration := client.unregistrations[0].Unregisterations
	if len(unregistration) != 1 || unregistration[0].ID != fileWatchRegistrationID || unregistration[0].Method != protocol.MethodWorkspaceDidChangeWatchedFiles {
		t.Fatalf("unregistration = %#v", unregistration)
	}
	assertWatchRegistration(t, client.registrations[1], []string{workspaceRoot, secondRuntime})
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
	_, graph, _, _ := instance.buildWorkspaceIndex(context.Background(), []string{root}, resolver, nil)
	mainPath = mustWorkspaceCanonicalPath(t, mainPath)
	imports := graph.Snapshot().Imports(mainPath)
	if len(imports) != 1 || imports[0].Target != "" || imports[0].Missing || imports[0].Dynamic || len(graph.Snapshot().Outgoing(mainPath)) != 0 {
		t.Fatalf("unreadable target imports = %#v", imports)
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

func TestWorkspaceResolverIsReusedUntilRootsChange(t *testing.T) {
	root := t.TempDir()
	instance := initializeWorkspaceServer(t, root)
	first, _, _ := instance.workspaceNavigationState()
	second, _, _ := instance.workspaceNavigationState()
	if first == nil || second != first {
		t.Fatalf("resolver was rebuilt without a workspace change: %p, %p", first, second)
	}
	instance.setRuntimePaths([]string{root})
	invalidated, _, _ := instance.workspaceNavigationState()
	if invalidated != nil {
		t.Fatalf("resolver was retained after runtimepath change: %p", invalidated)
	}
	instance.refreshWorkspaceResolver()
	refreshed, _, _ := instance.workspaceNavigationState()
	if refreshed == nil || refreshed == first {
		t.Fatalf("resolver was not refreshed: before=%p after=%p", first, refreshed)
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

func currentImportGraph(instance *Server) workspace.ImportGraphSnapshot {
	instance.workspaceMu.Lock()
	defer instance.workspaceMu.Unlock()
	return instance.workspaceGraphView
}

func waitForImportGraph(t *testing.T, instance *Server, ready func(workspace.ImportGraphSnapshot) bool) workspace.ImportGraphSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
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
