package server

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/synctest"
	"time"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type refreshClient struct {
	protocol.UnimplementedClient
	diagnosticCalls       chan struct{}
	diagnosticRelease     chan struct{}
	semanticTokensCalls   chan struct{}
	semanticTokensRelease chan struct{}
	inlayHintCalls        chan struct{}
	inlayHintRelease      chan struct{}
	codeLensCalls         chan struct{}
	codeLensRelease       chan struct{}
	diagnosticErr         error
}

func newRefreshClient() *refreshClient {
	return &refreshClient{
		diagnosticCalls:       make(chan struct{}, 2),
		diagnosticRelease:     make(chan struct{}, 2),
		semanticTokensCalls:   make(chan struct{}, 2),
		semanticTokensRelease: make(chan struct{}, 2),
		inlayHintCalls:        make(chan struct{}, 2),
		inlayHintRelease:      make(chan struct{}, 2),
		codeLensCalls:         make(chan struct{}, 2),
		codeLensRelease:       make(chan struct{}, 2),
	}
}

func (c *refreshClient) DiagnosticRefresh(ctx context.Context) error {
	select {
	case c.diagnosticCalls <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-c.diagnosticRelease:
		return c.diagnosticErr
	case <-ctx.Done():
		return ctx.Err()
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

func (c *refreshClient) CodeLensRefresh(ctx context.Context) error {
	select {
	case c.codeLensCalls <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-c.codeLensRelease:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func initializeRefreshServer(t *testing.T, instance *Server, semanticTokensSupport, inlayHintSupport, codeLensSupport *bool) {
	t.Helper()
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: protocol.ClientCapabilities{
		Workspace: &protocol.WorkspaceClientCapabilities{
			SemanticTokens: &protocol.SemanticTokensWorkspaceClientCapabilities{RefreshSupport: semanticTokensSupport},
			InlayHint:      &protocol.InlayHintWorkspaceClientCapabilities{RefreshSupport: inlayHintSupport},
			CodeLens:       &protocol.CodeLensWorkspaceClientCapabilities{RefreshSupport: codeLensSupport},
		},
	}}); err != nil {
		t.Fatal(err)
	}
}

func waitForRefresh(t *testing.T, calls <-chan struct{}) {
	t.Helper()
	select {
	case <-calls:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for refresh")
	}
}

func assertNoRefresh(t *testing.T, calls <-chan struct{}) {
	t.Helper()
	select {
	case <-calls:
		t.Fatal("unexpected refresh")
	default:
	}
}

func waitForRefreshIdle(t *testing.T, instance *Server) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		instance.mu.Lock()
		running := false
		for _, refresh := range instance.refreshes {
			running = running || refresh.running
		}
		instance.mu.Unlock()
		if !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("refresh workers did not become idle")
		}
		runtime.Gosched()
	}
}

func TestRefreshCapabilityGuard(t *testing.T) {
	falseValue := false
	trueValue := true
	for _, test := range []struct {
		name                                                 string
		semanticTokensSupport, inlaySupport, codeLensSupport *bool
		wantSemanticTokens, wantInlayHints, wantCodeLenses   bool
	}{
		{name: "nil"},
		{name: "false", semanticTokensSupport: &falseValue, inlaySupport: &falseValue},
		{name: "semantic tokens only", semanticTokensSupport: &trueValue, inlaySupport: &falseValue, codeLensSupport: &falseValue, wantSemanticTokens: true},
		{name: "inlay hints only", semanticTokensSupport: &falseValue, inlaySupport: &trueValue, codeLensSupport: &falseValue, wantInlayHints: true},
		{name: "code lens only", semanticTokensSupport: &falseValue, inlaySupport: &falseValue, codeLensSupport: &trueValue, wantCodeLenses: true},
		{name: "true", semanticTokensSupport: &trueValue, inlaySupport: &trueValue, codeLensSupport: &trueValue, wantSemanticTokens: true, wantInlayHints: true, wantCodeLenses: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance := New(nil, nil, io.Discard)
			t.Cleanup(instance.stopAnalysis)
			client := newRefreshClient()
			instance.client = client
			initializeRefreshServer(t, instance, test.semanticTokensSupport, test.inlaySupport, test.codeLensSupport)

			instance.mu.Lock()
			semanticTokensSupported := instance.refreshes[refreshSemanticTokens].supported
			inlayHintsSupported := instance.refreshes[refreshInlayHint].supported
			codeLensesSupported := instance.refreshes[refreshCodeLens].supported
			instance.mu.Unlock()
			if semanticTokensSupported != test.wantSemanticTokens || inlayHintsSupported != test.wantInlayHints || codeLensesSupported != test.wantCodeLenses {
				t.Fatalf("refresh support = semantic tokens %t, inlay hints %t, code lenses %t; want semantic tokens %t, inlay hints %t, code lenses %t", semanticTokensSupported, inlayHintsSupported, codeLensesSupported, test.wantSemanticTokens, test.wantInlayHints, test.wantCodeLenses)
			}

			instance.scheduleRefresh(refreshSemanticTokens)
			instance.scheduleRefresh(refreshInlayHint)
			instance.scheduleRefresh(refreshCodeLens)
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
			if test.wantCodeLenses {
				waitForRefresh(t, client.codeLensCalls)
				client.codeLensRelease <- struct{}{}
			} else {
				assertNoRefresh(t, client.codeLensCalls)
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
	initializeRefreshServer(t, instance, &value, &value, &value)
	instance.mu.Lock()
	instance.pullDiagnostics = true
	instance.refreshes[refreshDiagnostic].supported = true
	instance.mu.Unlock()

	instance.scheduleRefresh(refreshDiagnostic)
	instance.scheduleRefresh(refreshSemanticTokens)
	instance.scheduleRefresh(refreshInlayHint)
	instance.scheduleRefresh(refreshCodeLens)
	waitForRefresh(t, client.diagnosticCalls)
	waitForRefresh(t, client.semanticTokensCalls)
	waitForRefresh(t, client.inlayHintCalls)
	waitForRefresh(t, client.codeLensCalls)
	for range 3 {
		instance.scheduleRefresh(refreshDiagnostic)
		instance.scheduleRefresh(refreshSemanticTokens)
		instance.scheduleRefresh(refreshInlayHint)
		instance.scheduleRefresh(refreshCodeLens)
	}
	client.diagnosticRelease <- struct{}{}
	client.semanticTokensRelease <- struct{}{}
	client.inlayHintRelease <- struct{}{}
	client.codeLensRelease <- struct{}{}
	waitForRefresh(t, client.diagnosticCalls)
	waitForRefresh(t, client.semanticTokensCalls)
	waitForRefresh(t, client.inlayHintCalls)
	waitForRefresh(t, client.codeLensCalls)
	client.diagnosticRelease <- struct{}{}
	client.semanticTokensRelease <- struct{}{}
	client.inlayHintRelease <- struct{}{}
	client.codeLensRelease <- struct{}{}
	waitForRefreshIdle(t, instance)
	assertNoRefresh(t, client.diagnosticCalls)
	assertNoRefresh(t, client.semanticTokensCalls)
	assertNoRefresh(t, client.inlayHintCalls)
	assertNoRefresh(t, client.codeLensCalls)
}

func TestRefreshDoesNotSendAfterShutdown(t *testing.T) {
	value := true
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := newRefreshClient()
	instance.client = client
	initializeRefreshServer(t, instance, &value, &value, &value)
	instance.mu.Lock()
	instance.pullDiagnostics = true
	instance.refreshes[refreshDiagnostic].supported = true
	instance.mu.Unlock()
	if err := instance.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	instance.scheduleRefresh(refreshDiagnostic)
	instance.scheduleRefresh(refreshSemanticTokens)
	instance.scheduleRefresh(refreshInlayHint)
	instance.scheduleRefresh(refreshCodeLens)
	assertNoRefresh(t, client.diagnosticCalls)
	assertNoRefresh(t, client.semanticTokensCalls)
	assertNoRefresh(t, client.inlayHintCalls)
	assertNoRefresh(t, client.codeLensCalls)
}

func TestDiagnosticRefreshCapabilityGuard(t *testing.T) {
	falseValue := false
	trueValue := true
	for _, test := range []struct {
		name                  string
		support               *bool
		pull, supported, want bool
	}{
		{name: "nil support", pull: true},
		{name: "false support", support: &falseValue, pull: true},
		{name: "pull disabled", support: &trueValue, supported: true},
		{name: "supported pull diagnostics", support: &trueValue, pull: true, supported: true, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				instance := New(nil, nil, io.Discard)
				defer instance.stopAnalysis()
				client := newRefreshClient()
				instance.client = client
				capabilities := protocol.ClientCapabilities{Workspace: &protocol.WorkspaceClientCapabilities{
					Diagnostics: &protocol.DiagnosticWorkspaceClientCapabilities{RefreshSupport: test.support},
				}}
				if test.pull {
					capabilities.TextDocument = &protocol.TextDocumentClientCapabilities{Diagnostic: &protocol.DiagnosticClientCapabilities{}}
				}
				if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: capabilities}); err != nil {
					t.Fatal(err)
				}
				instance.mu.Lock()
				supported := instance.refreshes[refreshDiagnostic].supported
				pullDiagnostics := instance.pullDiagnostics
				instance.mu.Unlock()
				if supported != test.supported || pullDiagnostics != test.pull {
					t.Fatalf("diagnostic capability = support %t, pull %t", supported, pullDiagnostics)
				}

				instance.scheduleRefresh(refreshDiagnostic)
				synctest.Wait()
				if !test.want {
					assertNoRefresh(t, client.diagnosticCalls)
					return
				}
				waitForRefresh(t, client.diagnosticCalls)
				client.diagnosticRelease <- struct{}{}
				synctest.Wait()
			})
		})
	}
}

func TestRefreshKindsRunIndependentlyAndRequireExplicitRescheduleAfterError(t *testing.T) {
	value := true
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := newRefreshClient()
	client.diagnosticErr = errors.New("refresh failed")
	instance.client = client
	initializeRefreshServer(t, instance, &value, &value, &value)
	instance.mu.Lock()
	instance.pullDiagnostics = true
	instance.refreshes[refreshDiagnostic].supported = true
	instance.mu.Unlock()

	instance.scheduleRefresh(refreshSemanticTokens)
	waitForRefresh(t, client.semanticTokensCalls)
	instance.scheduleRefresh(refreshDiagnostic)
	waitForRefresh(t, client.diagnosticCalls)
	client.semanticTokensRelease <- struct{}{}
	client.diagnosticRelease <- struct{}{}
	waitForRefreshIdle(t, instance)
	instance.scheduleRefresh(refreshDiagnostic)
	waitForRefresh(t, client.diagnosticCalls)
	client.diagnosticErr = nil
	client.diagnosticRelease <- struct{}{}
}

func TestDidOpenSchedulesInitialRefreshAfterParsing(t *testing.T) {
	value := true
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := newRefreshClient()
	instance.client = client
	initializeRefreshServer(t, instance, &value, &value, &value)
	documentURI := uri.File(filepath.Join(t.TempDir(), "main.vim"))
	parsing := make(chan struct{})
	continueParsing := make(chan struct{})
	instance.testHooks.beforeParseSnapshotCacheMiss = func(*text.Snapshot) {
		close(parsing)
		<-continueParsing
	}

	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: "vim9script\nvar Value = 1\n",
	}}); err != nil {
		t.Fatal(err)
	}
	<-parsing
	assertNoRefresh(t, client.semanticTokensCalls)
	assertNoRefresh(t, client.inlayHintCalls)
	assertNoRefresh(t, client.codeLensCalls)
	close(continueParsing)
	waitForRefresh(t, client.semanticTokensCalls)
	waitForRefresh(t, client.inlayHintCalls)
	waitForRefresh(t, client.codeLensCalls)
	instance.publishMu.Lock()
	parsed := instance.parsed[documentURI.String()]
	instance.publishMu.Unlock()
	if parsed.file == nil {
		t.Fatal("refresh sent before AST was installed")
	}
	client.semanticTokensRelease <- struct{}{}
	client.inlayHintRelease <- struct{}{}
	client.codeLensRelease <- struct{}{}
}

func TestSingleDocumentAnalysisDoesNotScheduleRefresh(t *testing.T) {
	value := true
	root := mustWorkspaceCanonicalPath(t, t.TempDir())
	path := mustWorkspaceCanonicalPath(t, filepath.Join(root, "main.vim"))
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := newRefreshClient()
	instance.client = client
	initializeRefreshServer(t, instance, &value, &value, &value)
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
	assertNoRefresh(t, client.codeLensCalls)
}

func TestDependentAnalysisDoesNotScheduleRefresh(t *testing.T) {
	value := true
	root := mustWorkspaceCanonicalPath(t, t.TempDir())
	path := mustWorkspaceCanonicalPath(t, filepath.Join(root, "dependent.vim"))
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := newRefreshClient()
	instance.client = client
	initializeRefreshServer(t, instance, &value, &value, &value)
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
	assertNoRefresh(t, client.codeLensCalls)
}

func TestWorkspaceDeltaSchedulesRefresh(t *testing.T) {
	for _, kind := range []string{"watched", "runtimepath"} {
		for _, supported := range []bool{false, true} {
			t.Run(kind+"/"+map[bool]string{false: "unsupported", true: "supported"}[supported], func(t *testing.T) {
				root := t.TempDir()
				path := writeWorkspaceFile(t, root, "lib.vim", "function Initial()\nendfunction\n")
				instance := initializeWorkspaceServer(t, root)
				client := newRefreshClient()
				instance.mu.Lock()
				instance.client = client
				instance.pullDiagnostics = supported
				for kind := refreshDiagnostic; kind < refreshKindCount; kind++ {
					instance.refreshes[kind].supported = supported
				}
				instance.mu.Unlock()
				if err := os.WriteFile(path, []byte("function Updated()\nendfunction\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				runtimeRoot := t.TempDir()
				writeWorkspaceFile(t, runtimeRoot, "plugin/extra.vim", "function Extra()\nendfunction\n")
				apply := func() {
					t.Helper()
					if kind == "watched" {
						if !instance.applyWatchedFileChanges(context.Background(), []protocol.FileEvent{{URI: uri.File(path), Type: protocol.FileChangeTypeChanged}}) {
							t.Fatal("unexpected rebuild fallback")
						}
					} else if err := instance.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{runtimeRoot}}); err != nil {
						t.Fatal(err)
					}
				}
				apply()
				apply() // Repeated identical events must not schedule another refresh.
				instance.mu.Lock()
				generations := make([]uint64, refreshKindCount)
				for kind := refreshDiagnostic; kind < refreshKindCount; kind++ {
					generations[kind] = instance.refreshes[kind].generation
				}
				instance.mu.Unlock()
				var want uint64
				if supported {
					want = 1
				}
				for _, got := range generations {
					if got != want {
						t.Fatalf("refresh generations = %v, want all %d", generations, want)
					}
				}
				if supported {
					for _, calls := range []chan struct{}{client.diagnosticCalls, client.semanticTokensCalls, client.inlayHintCalls, client.codeLensCalls} {
						waitForRefresh(t, calls)
					}
				}
			})
		}
	}
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
			Diagnostics:    &protocol.DiagnosticWorkspaceClientCapabilities{RefreshSupport: &value},
			SemanticTokens: &protocol.SemanticTokensWorkspaceClientCapabilities{RefreshSupport: &value},
			InlayHint:      &protocol.InlayHintWorkspaceClientCapabilities{RefreshSupport: &value},
			CodeLens:       &protocol.CodeLensWorkspaceClientCapabilities{RefreshSupport: &value},
		}, TextDocument: &protocol.TextDocumentClientCapabilities{Diagnostic: &protocol.DiagnosticClientCapabilities{}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	waitForRefresh(t, client.diagnosticCalls)
	waitForRefresh(t, client.semanticTokensCalls)
	waitForRefresh(t, client.inlayHintCalls)
	waitForRefresh(t, client.codeLensCalls)
	client.diagnosticRelease <- struct{}{}
	client.semanticTokensRelease <- struct{}{}
	client.inlayHintRelease <- struct{}{}
	client.codeLensRelease <- struct{}{}
}
