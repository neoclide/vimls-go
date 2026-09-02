package server

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type refreshClient struct {
	protocol.UnimplementedClient
	semanticTokensCalls   chan struct{}
	semanticTokensRelease chan struct{}
	inlayHintCalls        chan struct{}
	inlayHintRelease      chan struct{}
}

func newRefreshClient() *refreshClient {
	return &refreshClient{
		semanticTokensCalls:   make(chan struct{}, 2),
		semanticTokensRelease: make(chan struct{}, 2),
		inlayHintCalls:        make(chan struct{}, 2),
		inlayHintRelease:      make(chan struct{}, 2),
	}
}

func (c *refreshClient) SemanticTokensRefresh(ctx context.Context) error {
	select {
	case c.semanticTokensCalls <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-c.semanticTokensRelease:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *refreshClient) InlayHintRefresh(ctx context.Context) error {
	select {
	case c.inlayHintCalls <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-c.inlayHintRelease:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func initializeRefreshServer(t *testing.T, instance *Server, semanticTokensSupport, inlayHintSupport *bool) {
	t.Helper()
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: protocol.ClientCapabilities{
		Workspace: &protocol.WorkspaceClientCapabilities{
			SemanticTokens: &protocol.SemanticTokensWorkspaceClientCapabilities{RefreshSupport: semanticTokensSupport},
			InlayHint:      &protocol.InlayHintWorkspaceClientCapabilities{RefreshSupport: inlayHintSupport},
		},
	}}); err != nil {
		t.Fatal(err)
	}
}

func waitForRefresh(t *testing.T, calls <-chan struct{}) {
	t.Helper()
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for refresh")
	}
}

func assertNoRefresh(t *testing.T, calls <-chan struct{}) {
	t.Helper()
	select {
	case <-calls:
		t.Fatal("unexpected refresh")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRefreshCapabilityGuard(t *testing.T) {
	falseValue := false
	trueValue := true
	for _, test := range []struct {
		name                                string
		semanticTokensSupport, inlaySupport *bool
		wantSemanticTokens, wantInlayHints  bool
	}{
		{name: "nil"},
		{name: "false", semanticTokensSupport: &falseValue, inlaySupport: &falseValue},
		{name: "semantic tokens only", semanticTokensSupport: &trueValue, inlaySupport: &falseValue, wantSemanticTokens: true},
		{name: "inlay hints only", semanticTokensSupport: &falseValue, inlaySupport: &trueValue, wantInlayHints: true},
		{name: "true", semanticTokensSupport: &trueValue, inlaySupport: &trueValue, wantSemanticTokens: true, wantInlayHints: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance := New(nil, nil, io.Discard)
			t.Cleanup(instance.stopAnalysis)
			client := newRefreshClient()
			instance.client = client
			initializeRefreshServer(t, instance, test.semanticTokensSupport, test.inlaySupport)

			instance.mu.Lock()
			semanticTokensSupported := instance.semanticTokensRefreshSupport
			inlayHintsSupported := instance.inlayHintRefreshSupport
			instance.mu.Unlock()
			if semanticTokensSupported != test.wantSemanticTokens || inlayHintsSupported != test.wantInlayHints {
				t.Fatalf("refresh support = semantic tokens %t, inlay hints %t; want semantic tokens %t, inlay hints %t", semanticTokensSupported, inlayHintsSupported, test.wantSemanticTokens, test.wantInlayHints)
			}

			instance.scheduleSemanticTokensRefresh()
			instance.scheduleInlayHintRefresh()
			if test.wantSemanticTokens {
				waitForRefresh(t, client.semanticTokensCalls)
				client.semanticTokensRelease <- struct{}{}
			} else {
				assertNoRefresh(t, client.semanticTokensCalls)
			}
			if test.wantInlayHints {
				waitForRefresh(t, client.inlayHintCalls)
				client.inlayHintRelease <- struct{}{}
			} else {
				assertNoRefresh(t, client.inlayHintCalls)
			}
		})
	}
}

func TestRefreshCoalescesRequests(t *testing.T) {
	value := true
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := newRefreshClient()
	instance.client = client
	initializeRefreshServer(t, instance, &value, &value)

	instance.scheduleSemanticTokensRefresh()
	instance.scheduleInlayHintRefresh()
	waitForRefresh(t, client.semanticTokensCalls)
	waitForRefresh(t, client.inlayHintCalls)
	for range 3 {
		instance.scheduleSemanticTokensRefresh()
		instance.scheduleInlayHintRefresh()
	}
	client.semanticTokensRelease <- struct{}{}
	client.inlayHintRelease <- struct{}{}
	waitForRefresh(t, client.semanticTokensCalls)
	waitForRefresh(t, client.inlayHintCalls)
	client.semanticTokensRelease <- struct{}{}
	client.inlayHintRelease <- struct{}{}
	assertNoRefresh(t, client.semanticTokensCalls)
	assertNoRefresh(t, client.inlayHintCalls)
}

func TestRefreshDoesNotSendAfterShutdown(t *testing.T) {
	value := true
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := newRefreshClient()
	instance.client = client
	initializeRefreshServer(t, instance, &value, &value)
	if err := instance.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	instance.scheduleSemanticTokensRefresh()
	instance.scheduleInlayHintRefresh()
	assertNoRefresh(t, client.semanticTokensCalls)
	assertNoRefresh(t, client.inlayHintCalls)
}

func TestDidOpenSchedulesInitialRefresh(t *testing.T) {
	value := true
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := newRefreshClient()
	instance.client = client
	initializeRefreshServer(t, instance, &value, &value)
	documentURI := uri.File(filepath.Join(t.TempDir(), "main.vim"))

	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: "vim9script\nvar Value = 1\n",
	}}); err != nil {
		t.Fatal(err)
	}
	waitForRefresh(t, client.semanticTokensCalls)
	waitForRefresh(t, client.inlayHintCalls)
	client.semanticTokensRelease <- struct{}{}
	client.inlayHintRelease <- struct{}{}
}

func TestSingleDocumentAnalysisDoesNotScheduleRefresh(t *testing.T) {
	value := true
	root := mustWorkspaceCanonicalPath(t, t.TempDir())
	path := mustWorkspaceCanonicalPath(t, filepath.Join(root, "main.vim"))
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := newRefreshClient()
	instance.client = client
	initializeRefreshServer(t, instance, &value, &value)
	instance.setWorkspaceRoots([]string{root})
	instance.workspaceMu.Lock()
	instance.workspaceBuilt = true
	instance.workspaceResolver = workspacePathResolver([]string{root}, nil)
	instance.workspaceGraph.SetReady(true)
	instance.workspaceGraphView = instance.workspaceGraph.Snapshot()
	instance.workspaceMu.Unlock()

	source := "vim9script\nvar Value = 1\n"
	documentURI := uri.File(path)
	instance.documents.Open(documentURI.String(), 1, source)
	work, ok := instance.documents.BeginAnalysis(context.Background(), documentURI.String())
	if !ok {
		t.Fatal("analysis did not start")
	}
	file := syntax.Parse(source)
	if _, ok := instance.prepareSyntax(work, file, analysis.Analyze(file)); !ok {
		t.Fatal("analysis preparation failed")
	}
	assertNoRefresh(t, client.semanticTokensCalls)
	assertNoRefresh(t, client.inlayHintCalls)
}

func TestDependentAnalysisDoesNotScheduleRefresh(t *testing.T) {
	value := true
	root := mustWorkspaceCanonicalPath(t, t.TempDir())
	path := mustWorkspaceCanonicalPath(t, filepath.Join(root, "dependent.vim"))
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := newRefreshClient()
	instance.client = client
	initializeRefreshServer(t, instance, &value, &value)
	documentURI := uri.File(path)
	instance.documents.Open(documentURI.String(), 1, "vim9script\nvar Value = 1\n")
	instance.analysisMu.Lock()
	instance.analysisWorkers = 1
	instance.analysisMu.Unlock()

	instance.startWorkspaceDependents([]string{path})
	instance.analysisMu.Lock()
	_, queued := instance.analysisPending[documentURI.String()]
	instance.analysisMu.Unlock()
	if !queued {
		t.Fatal("dependent analysis was not queued")
	}
	assertNoRefresh(t, client.semanticTokensCalls)
	assertNoRefresh(t, client.inlayHintCalls)
}

func TestWorkspaceIndexSchedulesRefresh(t *testing.T) {
	value := true
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plugin.vim"), []byte("vim9script\nvar indexed = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootURI := uri.File(root)
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := newRefreshClient()
	instance.client = client
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI:               &rootURI,
		InitializationOptions: protocol.LSPAny([]byte(`{"runtimepath":[]}`)),
		Capabilities: protocol.ClientCapabilities{Workspace: &protocol.WorkspaceClientCapabilities{
			SemanticTokens: &protocol.SemanticTokensWorkspaceClientCapabilities{RefreshSupport: &value},
			InlayHint:      &protocol.InlayHintWorkspaceClientCapabilities{RefreshSupport: &value},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	waitForRefresh(t, client.semanticTokensCalls)
	waitForRefresh(t, client.inlayHintCalls)
	client.semanticTokensRelease <- struct{}{}
	client.inlayHintRelease <- struct{}{}
}
