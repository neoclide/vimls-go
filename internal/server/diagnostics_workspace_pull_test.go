package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type workspaceDiagnosticProgressClient struct {
	protocol.UnimplementedClient
	updates []*protocol.ProgressParams
	err     error
}

func (c *workspaceDiagnosticProgressClient) Progress(_ context.Context, params *protocol.ProgressParams) error {
	c.updates = append(c.updates, params)
	return c.err
}

func TestDiagnosticWorkspacePullReportsOpenAndClosedFiles(t *testing.T) {
	root := t.TempDir()
	closedPath := writeWorkspaceFile(t, root, "closed.vim", "vim9script\necho missingClosed\n")
	openPath := writeWorkspaceFile(t, root, "open.vim", "vim9script\necho diskValue\n")
	instance := initializeWorkspaceServer(t, root)
	external := writeWorkspaceFile(t, t.TempDir(), "runtime.vim", "vim9script\necho runtimeValue\n")
	instance.workspaceMu.Lock()
	instance.workspaceFiles[mustWorkspaceCanonicalPath(t, external)] = struct{}{}
	instance.workspaceMu.Unlock()
	openURI := uri.File(openPath)
	instance.documents.Open(openURI.String(), 7, "vim9script\necho openValue\n")

	report, err := instance.DiagnosticWorkspace(context.Background(), &protocol.WorkspaceDiagnosticParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Items) != 2 {
		t.Fatalf("items = %#v", report.Items)
	}
	closedURI := uri.File(mustWorkspaceCanonicalPath(t, closedPath))
	first, ok := workspaceDiagnosticReportForURI(t, report.Items, closedURI).(*protocol.WorkspaceFullDocumentDiagnosticReport)
	if !ok || first.URI != closedURI || first.Version != nil {
		t.Fatalf("closed report = %#v", first)
	}
	second, ok := workspaceDiagnosticReportForURI(t, report.Items, openURI).(*protocol.WorkspaceFullDocumentDiagnosticReport)
	if !ok || second.URI != openURI || second.Version == nil || *second.Version != 7 {
		t.Fatalf("open report = %#v", second)
	}
	if len(first.Items) == 0 || len(second.Items) == 0 || first.Items[0].Message == second.Items[0].Message {
		t.Fatalf("closed=%#v open=%#v", first.Items, second.Items)
	}

	previous := []protocol.PreviousResultId{{URI: first.URI, Value: *first.ResultID}, {URI: second.URI, Value: *second.ResultID}}
	report, err = instance.DiagnosticWorkspace(context.Background(), &protocol.WorkspaceDiagnosticParams{PreviousResultIds: previous})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range report.Items {
		if _, ok := item.(*protocol.WorkspaceUnchangedDocumentDiagnosticReport); !ok {
			t.Fatalf("previous report = %#v", report.Items)
		}
	}

	document, err := instance.Diagnostic(context.Background(), &protocol.DocumentDiagnosticParams{TextDocument: protocol.TextDocumentIdentifier{URI: openURI}})
	if err != nil {
		t.Fatal(err)
	}
	full, ok := document.(*protocol.RelatedFullDocumentDiagnosticReport)
	if !ok || full.ResultID == nil {
		t.Fatalf("document report = %#v", document)
	}
	report, err = instance.DiagnosticWorkspace(context.Background(), &protocol.WorkspaceDiagnosticParams{PreviousResultIds: []protocol.PreviousResultId{{URI: openURI, Value: *full.ResultID}}})
	if err != nil {
		t.Fatal(err)
	}
	item := workspaceDiagnosticReportForURI(t, report.Items, openURI)
	if unchanged, ok := item.(*protocol.WorkspaceUnchangedDocumentDiagnosticReport); !ok || unchanged.ResultID != *full.ResultID {
		t.Fatalf("cross-pull cache = %#v", item)
	}
}

func workspaceDiagnosticReportForURI(t *testing.T, items []protocol.WorkspaceDocumentDiagnosticReport, want uri.URI) protocol.WorkspaceDocumentDiagnosticReport {
	t.Helper()
	for _, item := range items {
		switch report := item.(type) {
		case *protocol.WorkspaceFullDocumentDiagnosticReport:
			if report.URI == want {
				return report
			}
		case *protocol.WorkspaceUnchangedDocumentDiagnosticReport:
			if report.URI == want {
				return report
			}
		}
	}
	t.Fatalf("workspace diagnostic report for %s not found: %#v", want, items)
	return nil
}

func TestDiagnosticWorkspacePullRejectsNilParams(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	if _, err := instance.DiagnosticWorkspace(context.Background(), nil); !errors.Is(err, jsonrpc2.ErrInvalidParams) {
		t.Fatalf("nil params error = %v", err)
	}
}

func TestDiagnosticWorkspacePullInvalidatesAndRejectsIncompleteIndex(t *testing.T) {
	root := t.TempDir()
	path := writeWorkspaceFile(t, root, "main.vim", "vim9script\necho first\n")
	instance := initializeWorkspaceServer(t, root)
	pull := func() *protocol.WorkspaceFullDocumentDiagnosticReport {
		t.Helper()
		report, err := instance.DiagnosticWorkspace(context.Background(), &protocol.WorkspaceDiagnosticParams{})
		if err != nil || len(report.Items) != 1 {
			t.Fatalf("report=%#v err=%v", report, err)
		}
		full, ok := report.Items[0].(*protocol.WorkspaceFullDocumentDiagnosticReport)
		if !ok || full.ResultID == nil {
			t.Fatalf("item=%#v", report.Items[0])
		}
		return full
	}
	first := pull()
	writeWorkspaceFile(t, root, "main.vim", "vim9script\necho second\n")
	second := pull()
	if *second.ResultID == *first.ResultID || second.URI != uri.File(mustWorkspaceCanonicalPath(t, path)) {
		t.Fatalf("source change first=%#v second=%#v", first, second)
	}
	instance.documents.ConfigurationChanged()
	third := pull()
	if *third.ResultID == *second.ResultID {
		t.Fatalf("config change reused %q", *third.ResultID)
	}
	instance.workspaceMu.Lock()
	instance.workspaceRevision++
	instance.workspaceMu.Unlock()
	fourth := pull()
	if *fourth.ResultID == *third.ResultID {
		t.Fatalf("workspace change reused %q", *fourth.ResultID)
	}
	instance.workspaceMu.Lock()
	instance.workspaceIndex.SetComplete(false)
	instance.workspaceMu.Unlock()
	if _, err := instance.DiagnosticWorkspace(context.Background(), &protocol.WorkspaceDiagnosticParams{}); err == nil || err.Error() != workspaceDiagnosticIncompleteMessage {
		t.Fatalf("incomplete error = %v", err)
	}
}

func TestDiagnosticWorkspacePullPrunesOnlyNoLongerEnumeratedClosedCache(t *testing.T) {
	root := t.TempDir()
	path := writeWorkspaceFile(t, root, "main.vim", "vim9script\necho missing\n")
	instance := initializeWorkspaceServer(t, root)
	if _, err := instance.DiagnosticWorkspace(context.Background(), &protocol.WorkspaceDiagnosticParams{}); err != nil {
		t.Fatal(err)
	}
	closedURI := uri.File(mustWorkspaceCanonicalPath(t, path)).String()
	externalURI := uri.File(filepath.Join(t.TempDir(), "outside.vim"))
	instance.documents.Open(externalURI.String(), 1, "vim9script\necho missing\n")
	if _, err := instance.Diagnostic(context.Background(), &protocol.DocumentDiagnosticParams{TextDocument: protocol.TextDocumentIdentifier{URI: externalURI}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	report, err := instance.DiagnosticWorkspace(context.Background(), &protocol.WorkspaceDiagnosticParams{})
	if err != nil || len(report.Items) != 0 {
		t.Fatalf("report=%#v err=%v", report, err)
	}
	instance.publishMu.Lock()
	_, closedCached := instance.pullDiagnosticResults[closedURI]
	_, externalCached := instance.pullDiagnosticResults[externalURI.String()]
	instance.publishMu.Unlock()
	if closedCached || !externalCached {
		t.Fatalf("closed=%t external=%t", closedCached, externalCached)
	}
}

func TestDiagnosticWorkspacePullCancellationTimeoutRetryAndPartialResults(t *testing.T) {
	t.Run("cancellation and timeout", func(t *testing.T) {
		instance := New(nil, nil, io.Discard)
		t.Cleanup(instance.stopAnalysis)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := instance.DiagnosticWorkspace(ctx, &protocol.WorkspaceDiagnosticParams{}); !errors.Is(err, protocol.ErrRequestCancelled) {
			t.Fatalf("cancel error = %v", err)
		}
		instance.workspaceMu.Lock()
		instance.workspaceRunning = true
		instance.workspaceMu.Unlock()
		if _, err := instance.DiagnosticWorkspace(context.Background(), &protocol.WorkspaceDiagnosticParams{}); err == nil || err.Error() != "workspace index did not become ready within 1s" {
			t.Fatalf("timeout error = %v", err)
		}
	})

	t.Run("one retry then content modified", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspaceFile(t, root, "main.vim", "vim9script\necho missing\n")
		instance := initializeWorkspaceServer(t, root)
		checks := 0
		instance.testHooks.beforeWorkspaceIdentityCheck = func() {
			checks++
			if checks == 1 {
				instance.workspaceMu.Lock()
				instance.workspaceRevision++
				instance.workspaceMu.Unlock()
			}
		}
		if _, err := instance.DiagnosticWorkspace(context.Background(), &protocol.WorkspaceDiagnosticParams{}); err != nil || checks != 2 {
			t.Fatalf("retry checks=%d err=%v", checks, err)
		}
		checks = 0
		instance.testHooks.beforeWorkspaceIdentityCheck = func() {
			checks++
			instance.workspaceMu.Lock()
			instance.workspaceRevision++
			instance.workspaceMu.Unlock()
		}
		if _, err := instance.DiagnosticWorkspace(context.Background(), &protocol.WorkspaceDiagnosticParams{}); !errors.Is(err, protocol.ErrContentModified) || checks != 2 {
			t.Fatalf("stale checks=%d err=%v", checks, err)
		}
	})

	t.Run("partial result batches and errors", func(t *testing.T) {
		root := t.TempDir()
		for i := range workspaceDiagnosticPartialBatchSize + 1 {
			writeWorkspaceFile(t, root, fmt.Sprintf("%03d.vim", i), "vim9script\necho missing\n")
		}
		instance := initializeWorkspaceServer(t, root)
		client := &workspaceDiagnosticProgressClient{}
		instance.mu.Lock()
		instance.client = client
		instance.mu.Unlock()
		token := protocol.String("workspace-diagnostics")
		report, err := instance.DiagnosticWorkspace(context.Background(), &protocol.WorkspaceDiagnosticParams{PartialResultParams: protocol.PartialResultParams{PartialResultToken: token}})
		if err != nil || len(report.Items) != 0 || len(client.updates) != 2 {
			t.Fatalf("report=%#v updates=%d err=%v", report, len(client.updates), err)
		}
		for index, update := range client.updates {
			var partial protocol.WorkspaceDiagnosticReportPartialResult
			if update.Token != token || protocol.Unmarshal(update.Value, &partial) != nil || len(partial.Items) == 0 {
				t.Fatalf("partial update %d = %#v", index, update)
			}
		}
		client.err = errors.New("progress failed")
		if _, err := instance.DiagnosticWorkspace(context.Background(), &protocol.WorkspaceDiagnosticParams{PartialResultParams: protocol.PartialResultParams{PartialResultToken: token}}); err == nil || err.Error() != "progress failed" {
			t.Fatalf("progress error = %v", err)
		}
	})
}

func TestDiagnosticWorkspacePullWaitsForInstalledIndex(t *testing.T) {
	root := t.TempDir()
	mainPath := writeWorkspaceFile(t, root, filepath.Join("nested", "main.vim"), "BuildP\n")
	instance := initializeWorkspaceServer(t, root)
	instance.workspaceMu.Lock()
	instance.workspaceIndex.SetComplete(false)
	instance.workspaceRunning = true
	instance.workspaceMu.Unlock()
	started := make(chan struct{})
	instance.testHooks.beforeWorkspaceIndexWait = func() {
		select {
		case <-started:
		default:
			close(started)
		}
	}
	type response struct {
		report *protocol.WorkspaceDiagnosticReport
		err    error
	}
	done := make(chan response, 1)
	go func() {
		report, err := instance.DiagnosticWorkspace(context.Background(), &protocol.WorkspaceDiagnosticParams{})
		done <- response{report: report, err: err}
	}()
	<-started
	select {
	case result := <-done:
		t.Fatalf("request returned before index installation: %#v", result)
	default:
	}
	writeWorkspaceFile(t, root, "commands.vim", "command! BuildProject echo 'ok'\n")
	canonicalRoot := mustWorkspaceCanonicalPath(t, root)
	index, graph, files, warnings := instance.buildWorkspaceIndex(context.Background(), []string{canonicalRoot}, nil, workspacePathResolver([]string{canonicalRoot}, nil), nil)
	if len(warnings) != 0 || !index.Complete() {
		t.Fatalf("new index warnings=%#v complete=%t", warnings, index.Complete())
	}
	instance.workspaceMu.Lock()
	instance.workspaceIndex = index
	instance.workspaceGraph = graph
	instance.workspaceGraphView = graph.Snapshot()
	instance.workspaceFiles = files
	instance.workspaceRevision++
	instance.workspaceRunning = false
	instance.notifyWorkspaceIndexChangedLocked()
	instance.workspaceMu.Unlock()
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	for _, item := range result.report.Items {
		full, ok := item.(*protocol.WorkspaceFullDocumentDiagnosticReport)
		if !ok || full.URI != uri.File(mustWorkspaceCanonicalPath(t, mainPath)) {
			continue
		}
		for _, diagnostic := range full.Items {
			if diagnostic.Code == protocol.String("vim/E464") {
				return
			}
		}
	}
	t.Fatalf("new-index command diagnostic missing from %#v", result.report.Items)
}
