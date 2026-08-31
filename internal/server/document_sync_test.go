package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func waitForServerRace(t *testing.T, event <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func TestServerDocumentSynchronization(t *testing.T) {
	input := encodeFrames(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{"general":{"positionEncodings":["utf-8","utf-16"]}}}}`,
		`{"jsonrpc":"2.0","method":"initialized","params":{}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///sync.vim","languageId":"vim","version":1,"text":"a𐐀b\n"}}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":"file:///sync.vim","version":2},"contentChanges":[{"range":{"start":{"line":0,"character":1},"end":{"line":0,"character":5}},"text":"X"}]}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didSave","params":{"textDocument":{"uri":"file:///sync.vim"},"text":"saved\n"}}`,
		`{"jsonrpc":"2.0","method":"workspace/didChangeConfiguration","params":{"settings":{"vimls":{"targetVersion":"9.2.4"}}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var output, logs bytes.Buffer
	instance := New(&input, &output, &logs)
	if code := instance.Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d, output = %q, logs = %q", code, output.String(), logs.String())
	}

	snapshot, ok := instance.documents.Snapshot("file:///sync.vim")
	if !ok {
		t.Fatal("document was not retained")
	}
	if got := snapshot.Text(); got != "saved\n" {
		t.Fatalf("text = %q", got)
	}
	if version, ok := snapshot.Version(); !ok || version != 2 {
		t.Fatalf("version = %d, present = %v", version, ok)
	}
	if revision := instance.documents.ConfigRevision(); revision != 2 {
		t.Fatalf("config revision = %d, want 2", revision)
	}
	if got := instance.TargetVersion().String(); got != "9.2.0004" {
		t.Fatalf("target version = %q", got)
	}
	messages := decodeFrames(t, &output)
	responses := make([]map[string]json.RawMessage, 0, 2)
	for _, message := range messages {
		if _, ok := message["id"]; ok {
			responses = append(responses, message)
		}
	}
	if len(responses) != 2 {
		t.Fatalf("responses = %d, want 2; messages = %#v", len(responses), messages)
	}
	if result := responses[0]["result"]; !bytes.Contains(result, []byte(`"positionEncoding":"utf-8"`)) || !bytes.Contains(result, []byte(`"change":2`)) {
		t.Fatalf("initialize result = %s", result)
	}
	if logs.Len() != 0 {
		t.Fatalf("logs = %q", logs.String())
	}
}

func TestServerDocumentHandlersCancelStaleAnalysis(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{}); err != nil {
		t.Fatal(err)
	}
	documentURI := uri.MustParse("file:///direct.vim")
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: "old"},
	}); err != nil {
		t.Fatal(err)
	}
	analysis, ok := instance.documents.BeginAnalysis(context.Background(), documentURI.String())
	if !ok {
		t.Fatal("analysis did not start")
	}
	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI},
			Version:                2,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: "new"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if analysis.Context.Err() == nil || instance.documents.IsCurrent(analysis) {
		t.Fatal("document change did not invalidate active analysis")
	}
	sameChange, ok := instance.documents.BeginAnalysis(context.Background(), documentURI.String())
	if !ok {
		t.Fatal("analysis did not start after change")
	}
	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI},
			Version:                3,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: "new"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if sameChange.Context.Err() == nil || instance.documents.IsCurrent(sameChange) {
		t.Fatal("same-content change did not invalidate active analysis")
	}
	unchangedSave, ok := instance.documents.BeginAnalysis(context.Background(), documentURI.String())
	if !ok {
		t.Fatal("analysis did not start after same-content change")
	}
	if err := instance.DidSave(context.Background(), &protocol.DidSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
	}); err != nil {
		t.Fatal(err)
	}
	if unchangedSave.Context.Err() != nil || !instance.documents.IsCurrent(unchangedSave) {
		t.Fatal("save without text invalidated active analysis")
	}
	sameText := "new"
	if err := instance.DidSave(context.Background(), &protocol.DidSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Text:         &sameText,
	}); err != nil {
		t.Fatal(err)
	}
	if unchangedSave.Context.Err() != nil || !instance.documents.IsCurrent(unchangedSave) {
		t.Fatal("same-text save invalidated active analysis")
	}
	savedText := "saved"
	if err := instance.DidSave(context.Background(), &protocol.DidSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Text:         &savedText,
	}); err != nil {
		t.Fatal(err)
	}
	if unchangedSave.Context.Err() == nil || instance.documents.IsCurrent(unchangedSave) {
		t.Fatal("changed save did not invalidate active analysis")
	}
	// Repeating the same client version is stale and must not replace the snapshot.
	_ = instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI},
			Version:                3,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: "stale"},
		},
	})
	snapshot, ok := instance.documents.Snapshot(documentURI.String())
	if !ok || snapshot.Text() != "saved" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if err := instance.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
	}); err != nil {
		t.Fatal(err)
	}
	if instance.documents.Len() != 0 {
		t.Fatal("document was not closed")
	}
}

func TestServerDidCloseClearsCacheAndRestoresOnlyOpenDocument(t *testing.T) {
	root := t.TempDir()
	path := writeWorkspaceFile(t, root, "close.vim", "vim9script\nvar diskValue = 1\n")
	instance := initializeWorkspaceServer(t, root)
	documentURI := uri.File(path)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: "vim9script\nvar overlayValue = 1\n",
	}}); err != nil {
		t.Fatal(err)
	}
	snapshot, ok := instance.documents.Snapshot(documentURI.String())
	if !ok || instance.parseSnapshot(snapshot) == nil {
		t.Fatal("open document was not cached")
	}
	if err := instance.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}}); err != nil {
		t.Fatal(err)
	}
	instance.publishMu.Lock()
	_, cached := instance.parsed[documentURI.String()]
	instance.publishMu.Unlock()
	if cached || instance.documents.Len() != 0 {
		t.Fatalf("close cache=%t documents=%d", cached, instance.documents.Len())
	}
	instance.workspaceMu.Lock()
	source, indexed := instance.workspaceIndex.Source(path)
	instance.workspaceMu.Unlock()
	if !indexed || source != "vim9script\nvar diskValue = 1\n" {
		t.Fatalf("restored source = %q, indexed = %t", source, indexed)
	}

	instance.workspaceMu.Lock()
	if err := instance.workspaceIndex.Replace(path, syntax.Parse("vim9script\nvar untouched = 1\n")); err != nil {
		instance.workspaceMu.Unlock()
		t.Fatal(err)
	}
	revision := instance.workspaceRevision
	instance.workspaceMu.Unlock()
	if err := instance.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceMu.Lock()
	source, indexed = instance.workspaceIndex.Source(path)
	gotRevision := instance.workspaceRevision
	instance.workspaceMu.Unlock()
	if !indexed || source != "vim9script\nvar untouched = 1\n" || gotRevision != revision {
		t.Fatalf("unopened close source=%q indexed=%t revision=%d want %d", source, indexed, gotRevision, revision)
	}
}

func TestServerDocumentParserCache(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	documentURI := uri.MustParse("file:///parser-cache.vim")
	firstSnapshot := instance.documents.Open(documentURI.String(), 1, "vim9script\nvar value = 1\n")
	first := instance.parseSnapshot(firstSnapshot)
	if first == nil {
		t.Fatal("first parse is nil")
	}

	secondSnapshot, changed, err := instance.documents.Change(documentURI.String(), 2, text.UTF16, []text.Change{{Text: firstSnapshot.Text()}})
	if err != nil || changed {
		t.Fatalf("same-source change = %#v, %v", changed, err)
	}
	if got := instance.parseSnapshot(secondSnapshot); got != first {
		t.Fatalf("same-source parse = %p, want cached %p", got, first)
	}

	thirdSnapshot, changed, err := instance.documents.Change(documentURI.String(), 3, text.UTF16, []text.Change{{Text: "vim9script\nvar changed = 1\n"}})
	if err != nil || !changed {
		t.Fatalf("changed-source change = %#v, %v", changed, err)
	}
	if got := instance.parseSnapshot(thirdSnapshot); got == first || got == nil || got.Source != thirdSnapshot.Text() {
		t.Fatalf("changed-source parse = %#v, first = %#v", got, first)
	}
}

func TestSameContentDidChangeRepublishesNewVersionWithCachedAST(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
	instance.client = client
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{}); err != nil {
		t.Fatal(err)
	}
	documentURI := uri.MustParse("file:///same-content.vim")
	source := "if true\n"
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: source,
	}}); err != nil {
		t.Fatal(err)
	}
	first := waitForDiagnostics(t, client.published)
	firstSnapshot, ok := instance.documents.Snapshot(documentURI.String())
	if !ok {
		t.Fatal("first snapshot is missing")
	}
	firstFile := instance.parseSnapshot(firstSnapshot)
	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{Text: source}},
	}); err != nil {
		t.Fatal(err)
	}
	second := waitForDiagnostics(t, client.published)
	secondSnapshot, ok := instance.documents.Snapshot(documentURI.String())
	if !ok || secondSnapshot == firstSnapshot || instance.parseSnapshot(secondSnapshot) != firstFile {
		t.Fatalf("same-content snapshot/cache = %p/%p, want new snapshot and cached %p", secondSnapshot, instance.parseSnapshot(secondSnapshot), firstFile)
	}
	for label, result := range map[string]*protocol.PublishDiagnosticsParams{"first": first, "second": second} {
		got, ok := result.Version.Get()
		if !ok || got != map[string]int32{"first": 1, "second": 2}[label] || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != protocol.String("vimls/missing-end") {
			t.Fatalf("%s diagnostics = %#v", label, result)
		}
	}
}

func TestShutdownCancelsAnalysisBeforeExit(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 1)}
	instance.client = client
	root := t.TempDir()
	instance.setWorkspaceRoots([]string{root})
	documentURI := uri.File(filepath.Join(root, "shutdown.vim"))
	snapshot := instance.documents.Open(documentURI.String(), 1, "if true\n")
	active, ok := instance.documents.BeginAnalysis(instance.analysisContext, documentURI.String())
	if !ok {
		t.Fatal("analysis did not start")
	}
	instance.analysisMu.Lock()
	instance.analysisPending[documentURI.String()] = struct{}{}
	instance.analysisMu.Unlock()
	instance.publishMu.Lock()
	shutdown := make(chan error, 1)
	go func() { shutdown <- instance.Shutdown(context.Background()) }()
	<-active.Context.Done()
	instance.analysisMu.Lock()
	stopped, pending := instance.analysisStopped, len(instance.analysisPending)
	instance.analysisMu.Unlock()
	if !stopped || pending != 0 {
		instance.publishMu.Unlock()
		t.Fatalf("shutdown analysis queue: stopped=%t pending=%d", stopped, pending)
	}
	select {
	case err := <-shutdown:
		instance.publishMu.Unlock()
		t.Fatalf("shutdown bypassed publish barrier: %v", err)
	default:
	}
	instance.publishMu.Unlock()
	if err := <-shutdown; err != nil {
		t.Fatal(err)
	}
	instance.clearDiagnostics(documentURI.String())
	select {
	case published := <-client.published:
		t.Fatalf("shutdown allowed late cleared diagnostics: %#v", published)
	default:
	}
	if parsed := instance.parseSnapshot(snapshot); parsed == nil {
		t.Fatal("shutdown snapshot did not parse for stale-result check")
	}
	instance.publishMu.Lock()
	_, cached := instance.parsed[documentURI.String()]
	instance.publishMu.Unlock()
	if cached {
		t.Fatal("shutdown allowed stale parse to install parser cache")
	}
	instance.startAnalysis(documentURI.String())
	instance.analysisMu.Lock()
	stopped, workers, pending, running := instance.analysisStopped, instance.analysisWorkers, len(instance.analysisPending), len(instance.analysisRunning)
	instance.analysisMu.Unlock()
	if !stopped || workers != 0 || pending != 0 || running != 0 {
		t.Fatalf("shutdown analysis queue: stopped=%t workers=%d pending=%d running=%d", stopped, workers, pending, running)
	}
	instance.analyzeDocument(documentURI.String())
	instance.workspaceMu.Lock()
	_, indexed := instance.workspaceIndex.Source(filepath.Join(root, "shutdown.vim"))
	instance.workspaceMu.Unlock()
	if indexed {
		t.Fatal("shutdown allowed stale analysis to install workspace facts")
	}
	select {
	case published := <-client.published:
		t.Fatalf("shutdown allowed stale diagnostics: %#v", published)
	default:
	}
}

func TestServerAnalysisDoesNotMutateParserCache(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	documentURI := uri.MustParse("file:///pure-parser-cache.vim")
	snapshot := instance.documents.Open(documentURI.String(), 1, "vim9script\nenum Color\n  Red\nendenum\n")
	raw := instance.parseSnapshot(snapshot)
	if raw == nil || len(raw.Diagnostics) != 0 {
		t.Fatalf("raw parser diagnostics = %#v", raw)
	}

	instance.analyzeDocument(documentURI.String())
	instance.publishMu.Lock()
	cached := instance.parsed[documentURI.String()].file
	instance.publishMu.Unlock()
	if cached != raw || len(cached.Diagnostics) != 0 {
		t.Fatalf("parser cache after analysis = %#v", cached)
	}
}

func TestServerRepeatedDocumentOpenClearsParserCache(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	// Prevent handlers from starting a worker while keeping the context live.
	instance.analysisMu.Lock()
	instance.analysisWorkers = 1
	instance.analysisMu.Unlock()
	documentURI := uri.MustParse("file:///reopen-parser-cache.vim")
	params := &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: "vim9script\nvar value = 1\n",
	}}
	if err := instance.DidOpen(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	firstSnapshot, ok := instance.documents.Snapshot(documentURI.String())
	if !ok {
		t.Fatal("first snapshot is missing")
	}
	first := instance.parseSnapshot(firstSnapshot)
	if first == nil {
		t.Fatal("first parse is nil")
	}
	instance.publishMu.Lock()
	firstCached := instance.parsed[documentURI.String()]
	instance.publishMu.Unlock()
	if firstCached.file != first || firstCached.contentID != firstSnapshot.ContentID() {
		t.Fatalf("first parser cache: file=%p want=%p contentID=%x want=%x", firstCached.file, first, firstCached.contentID, firstSnapshot.ContentID())
	}

	params.TextDocument.Version = 2
	if err := instance.DidOpen(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	instance.publishMu.Lock()
	_, cached := instance.parsed[documentURI.String()]
	instance.publishMu.Unlock()
	if cached {
		t.Fatal("repeated didOpen retained parser cache")
	}
	secondSnapshot, ok := instance.documents.Snapshot(documentURI.String())
	if !ok || secondSnapshot == firstSnapshot {
		t.Fatalf("second snapshot = %#v", secondSnapshot)
	}
	second := instance.parseSnapshot(secondSnapshot)
	if second == nil {
		t.Fatal("second parse is nil")
	}
	if second.Source != params.TextDocument.Text || second == first {
		t.Fatalf("second parse: file=%p source=%q wantSource=%q first=%p", second, second.Source, params.TextDocument.Text, first)
	}
}

func TestServerOldSnapshotParserCacheCannotRestoreCurrentLifetime(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	// Prevent handlers from starting a worker while keeping the context live.
	instance.analysisMu.Lock()
	instance.analysisWorkers = 1
	instance.analysisMu.Unlock()
	documentURI := uri.MustParse("file:///old-snapshot-parser-cache.vim")
	oldSource := "vim9script\nvar old = 1\n"
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: oldSource,
	}}); err != nil {
		t.Fatal(err)
	}
	oldSnapshot, ok := instance.documents.Snapshot(documentURI.String())
	if !ok {
		t.Fatal("old snapshot is missing")
	}
	oldFile := instance.parseSnapshot(oldSnapshot)
	if oldFile == nil || oldFile.Source != oldSource {
		t.Fatalf("old parse = %#v", oldFile)
	}
	instance.publishMu.Lock()
	oldCached := instance.parsed[documentURI.String()]
	instance.publishMu.Unlock()
	if oldCached.file != oldFile || oldCached.contentID != oldSnapshot.ContentID() {
		t.Fatalf("old parser cache: file=%p want=%p contentID=%x want=%x", oldCached.file, oldFile, oldCached.contentID, oldSnapshot.ContentID())
	}

	currentSource := "vim9script\nvar current = 1\n"
	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: currentSource},
		},
	}); err != nil {
		t.Fatal(err)
	}
	currentSnapshot, ok := instance.documents.Snapshot(documentURI.String())
	if !ok {
		t.Fatal("current snapshot is missing")
	}
	currentFile := instance.parseSnapshot(currentSnapshot)
	if currentFile == nil || currentFile.Source != currentSource {
		t.Fatalf("current parse = %#v", currentFile)
	}
	if stale := instance.parseSnapshot(oldSnapshot); stale == nil || stale.Source != oldSource {
		t.Fatalf("old snapshot parse after change = %#v", stale)
	}
	instance.publishMu.Lock()
	cachedAfterChange := instance.parsed[documentURI.String()].file
	instance.publishMu.Unlock()
	if cachedAfterChange != currentFile {
		t.Fatalf("old snapshot replaced current cache: got %p, want %p", cachedAfterChange, currentFile)
	}

	if err := instance.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}}); err != nil {
		t.Fatal(err)
	}
	if stale := instance.parseSnapshot(oldSnapshot); stale == nil || stale.Source != oldSource {
		t.Fatalf("old snapshot parse after close = %#v", stale)
	}
	instance.publishMu.Lock()
	_, cachedAfterClose := instance.parsed[documentURI.String()]
	instance.publishMu.Unlock()
	if cachedAfterClose {
		t.Fatal("closed document retained or restored parser cache")
	}

	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 3, Text: oldSource,
	}}); err != nil {
		t.Fatal(err)
	}
	reopened, ok := instance.documents.Snapshot(documentURI.String())
	if !ok || reopened == oldSnapshot {
		t.Fatalf("reopened snapshot = %#v", reopened)
	}
	reopenedFile := instance.parseSnapshot(reopened)
	if reopenedFile == nil || reopenedFile.Source != oldSource {
		t.Fatalf("reopened parse = %#v", reopenedFile)
	}
	if reopenedFile == oldFile {
		t.Fatal("reopened lifetime reused old parser tree")
	}
	if stale := instance.parseSnapshot(oldSnapshot); stale == nil || stale.Source != oldSource {
		t.Fatalf("old snapshot parse after reopen = %#v", stale)
	}
	instance.publishMu.Lock()
	cachedAfterReopen := instance.parsed[documentURI.String()].file
	instance.publishMu.Unlock()
	if cachedAfterReopen != reopenedFile {
		t.Fatalf("old snapshot replaced reopened cache: got %p, want %p", cachedAfterReopen, reopenedFile)
	}
}

func TestServerChangeRejectsPausedParseCacheMiss(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	documentURI := uri.MustParse("file:///paused-parse.vim")
	oldSource := "vim9script\nvar Old = 1\n"
	oldSnapshot := instance.documents.Open(documentURI.String(), 1, oldSource)
	paused := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	instance.beforeParseSnapshotCacheMissForTest = func(snapshot *text.Snapshot) {
		if snapshot == oldSnapshot {
			close(paused)
			<-release
		}
	}

	oldDone := make(chan struct{})
	var oldFile *syntax.File
	go func() {
		oldFile = instance.parseSnapshot(oldSnapshot)
		close(oldDone)
	}()
	releaseThenJoin := func() {
		releaseOnce.Do(func() { close(release) })
		waitForServerRace(t, oldDone, "old parse completion")
	}
	t.Cleanup(releaseThenJoin)
	waitForServerRace(t, paused, "old parse cache miss")

	currentSource := "vim9script\nvar Current = 2\n"
	instance.publishMu.Lock()
	currentSnapshot, changed, err := instance.documents.Change(documentURI.String(), 2, text.UTF16, []text.Change{{Text: currentSource}})
	instance.publishMu.Unlock()
	if err != nil || !changed {
		t.Fatalf("direct document change: changed=%t err=%v", changed, err)
	}
	currentFile := instance.parseSnapshot(currentSnapshot)
	if currentFile == nil || currentFile.Source != currentSource {
		t.Fatalf("current parse = %#v", currentFile)
	}

	releaseThenJoin()
	if oldFile == nil || oldFile.Source != oldSource {
		t.Fatalf("old parse = %#v", oldFile)
	}
	instance.publishMu.Lock()
	cached := instance.parsed[documentURI.String()]
	instance.publishMu.Unlock()
	if cached.file == nil {
		t.Fatal("stale parse cleared current cache")
	}
	if cached.file != currentFile || cached.contentID != currentSnapshot.ContentID() || cached.file.Source != currentSource {
		t.Fatalf("stale parse replaced cache: file=%p want=%p contentID=%x want=%x source=%q", cached.file, currentFile, cached.contentID, currentSnapshot.ContentID(), cached.file.Source)
	}
}

func BenchmarkParseCacheHit(b *testing.B) {
	instance := New(nil, nil, io.Discard)
	b.Cleanup(instance.stopAnalysis)
	line := "var cache_value = 111111111111111111111111111111111111111111111111\n"
	source := "vim9script\n" + strings.Repeat(line, (64*1024-len("vim9script\n"))/len(line))
	snapshot := instance.documents.Open("file:///parse-cache-hit.vim", 1, source)
	first := instance.parseSnapshot(snapshot)
	second := instance.parseSnapshot(snapshot)
	if first == nil || second != first {
		b.Fatalf("cache preflight: first=%p second=%p", first, second)
	}
	b.ReportAllocs()
	b.SetBytes(int64(snapshot.ByteLen()))
	for b.Loop() {
		instance.parseSnapshot(snapshot)
	}
}

func BenchmarkParseCacheChangedFile(b *testing.B) {
	instance := New(nil, nil, io.Discard)
	b.Cleanup(instance.stopAnalysis)
	line := "var cache_value = 111111111111111111111111111111111111111111111111\n"
	baseSource := "vim9script\n" + strings.Repeat(line, (64*1024-len("vim9script\n"))/len(line))
	baseSnapshot := instance.documents.Open("file:///parse-cache-changed.vim", 1, baseSource)
	baseFile := instance.parseSnapshot(baseSnapshot)
	changedSource := strings.Replace(baseSource, "111111", "222222", 1)
	version := int32(2)
	changedSnapshot := text.NewSnapshot(baseSnapshot.URI(), 2, &version, changedSource)
	if baseFile == nil || baseSnapshot.ContentID() == changedSnapshot.ContentID() || baseSource == changedSource {
		b.Fatalf("changed parse preflight: base=%p sameID=%t sameSource=%t", baseFile, baseSnapshot.ContentID() == changedSnapshot.ContentID(), baseSource == changedSource)
	}
	if changedFile := instance.parseSnapshot(changedSnapshot); changedFile == nil || changedFile.Source != changedSource {
		b.Fatalf("changed parse preflight = %#v", changedFile)
	}
	instance.publishMu.Lock()
	cached := instance.parsed[baseSnapshot.URI()]
	instance.publishMu.Unlock()
	if cached.file != baseFile || cached.contentID != baseSnapshot.ContentID() {
		b.Fatalf("changed preflight replaced base cache: file=%p want=%p contentID=%x want=%x", cached.file, baseFile, cached.contentID, baseSnapshot.ContentID())
	}
	b.ReportAllocs()
	b.SetBytes(int64(changedSnapshot.ByteLen()))
	for b.Loop() {
		instance.parseSnapshot(changedSnapshot)
	}
	instance.publishMu.Lock()
	cached = instance.parsed[baseSnapshot.URI()]
	instance.publishMu.Unlock()
	if cached.file != baseFile || cached.contentID != baseSnapshot.ContentID() {
		b.Fatalf("changed parse replaced base cache: file=%p want=%p contentID=%x want=%x", cached.file, baseFile, cached.contentID, baseSnapshot.ContentID())
	}
}

func TestServerParseImportTargetRequiresExactOpenSource(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	documentURI := uri.File(filepath.Join(t.TempDir(), "import-target.vim"))
	path, ok := workspaceURIPath(documentURI)
	if !ok {
		t.Fatal("import target path is invalid")
	}
	capturedSource := "vim9script\nexport var Value = 1\n"
	snapshot := instance.documents.Open(documentURI.String(), 1, capturedSource)
	cached := instance.parseSnapshot(snapshot)
	if got := instance.parseImportTarget(path, capturedSource); got != cached {
		t.Fatalf("exact open import parse = %p, want cached %p", got, cached)
	}

	overlaySource := "vim9script\nexport var Changed = 1\n"
	overlay, changed, err := instance.documents.Change(documentURI.String(), 2, text.UTF16, []text.Change{{Text: overlaySource}})
	if err != nil || !changed {
		t.Fatalf("overlay change = %#v, %v", changed, err)
	}
	overlayFile := instance.parseSnapshot(overlay)
	if got := instance.parseImportTarget(path, capturedSource); got == nil || got.Source != capturedSource || got == overlayFile {
		t.Fatalf("stale captured import parse = %#v, overlay = %#v", got, overlayFile)
	}
	instance.publishMu.Lock()
	current := instance.parsed[documentURI.String()].file
	instance.publishMu.Unlock()
	if current != overlayFile {
		t.Fatalf("stale captured import parse replaced overlay cache: got %p, want %p", current, overlayFile)
	}
}

func TestServerUnchangedSavePreservesAnalysisAndWorkspace(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	root := t.TempDir()
	path := filepath.Join(root, "save.vim")
	documentURI := uri.File(path)
	source := "let g:save_cache_sentinel = 1\n"
	instance.setWorkspaceRoots([]string{root})
	opened := instance.documents.Open(documentURI.String(), 1, source)
	instance.replaceWorkspaceFile(documentURI.String(), syntax.Parse(source))

	instance.workspaceMu.Lock()
	indexedSource, indexed := instance.workspaceIndex.Source(path)
	workspaceRevision := instance.workspaceRevision
	graphRevision := instance.workspaceGraphView.Revision()
	workspacePending := len(instance.workspacePending)
	instance.workspaceMu.Unlock()
	if !indexed || indexedSource != source {
		t.Fatalf("workspace source = %q, indexed = %v", indexedSource, indexed)
	}

	// A non-zero worker count prevents startAnalysis from starting a worker, so
	// an accidental call leaves a deterministic pending entry for this test.
	instance.analysisMu.Lock()
	instance.analysisWorkers = 1
	instance.analysisMu.Unlock()
	assertUnchanged := func(save string) {
		t.Helper()
		instance.analysisMu.Lock()
		pending := len(instance.analysisPending)
		running := len(instance.analysisRunning)
		instance.analysisMu.Unlock()
		if pending != 0 || running != 0 {
			t.Fatalf("%s save analysis: pending = %d, running = %d", save, pending, running)
		}
		instance.workspaceMu.Lock()
		gotSource, ok := instance.workspaceIndex.Source(path)
		gotWorkspaceRevision := instance.workspaceRevision
		gotGraphRevision := instance.workspaceGraphView.Revision()
		gotWorkspacePending := len(instance.workspacePending)
		instance.workspaceMu.Unlock()
		if !ok || gotSource != source || gotWorkspaceRevision != workspaceRevision || gotGraphRevision != graphRevision || gotWorkspacePending != workspacePending {
			t.Fatalf("%s save workspace: source = %q, indexed = %v, revision = %d, graph = %d, pending = %d", save, gotSource, ok, gotWorkspaceRevision, gotGraphRevision, gotWorkspacePending)
		}
		snapshot, ok := instance.documents.Snapshot(documentURI.String())
		if !ok || snapshot != opened {
			t.Fatalf("%s save snapshot = %#v", save, snapshot)
		}
	}

	if err := instance.DidSave(context.Background(), &protocol.DidSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
	}); err != nil {
		t.Fatal(err)
	}
	assertUnchanged("without text")
	if err := instance.DidSave(context.Background(), &protocol.DidSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Text:         &source,
	}); err != nil {
		t.Fatal(err)
	}
	assertUnchanged("with matching text")
}

func TestAnalysisQueueCoalescesRapidDocumentChanges(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 1)}
	instance.mu.Lock()
	instance.client = client
	instance.mu.Unlock()
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{}); err != nil {
		t.Fatal(err)
	}
	documentURI := uri.MustParse("file:///rapid.vim")
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: "let g:value = 1\n"},
	}); err != nil {
		t.Fatal(err)
	}
	for version := int32(2); version <= 1001; version++ {
		content := "let g:value = 1\n"
		if version == 1001 {
			content = "if true\n"
		}
		if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
			TextDocument: protocol.VersionedTextDocumentIdentifier{
				TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: version,
			},
			ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{Text: content}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	result := waitForDiagnostics(t, client.published)
	if version, ok := result.Version.Get(); !ok || version != 1001 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != protocol.String("vimls/missing-end") {
		t.Fatalf("diagnostics = %#v", result)
	}
	instance.analysisMu.Lock()
	workers := instance.analysisWorkers
	pending := len(instance.analysisPending)
	running := len(instance.analysisRunning)
	instance.analysisMu.Unlock()
	if workers != analysisParallelism() || workers < 1 || workers > maxParallelAnalysis || pending > 1 || running > 1 {
		t.Fatalf("workers = %d, pending = %d, running = %d", workers, pending, running)
	}
}

func TestServerSkipsAnalysisForOversizedDocument(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 1)}
	instance.mu.Lock()
	instance.client = client
	instance.mu.Unlock()
	_, _ = instance.Initialize(context.Background(), &protocol.InitializeParams{})
	documentURI := uri.MustParse("file:///large.vim")
	_ = instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: strings.Repeat("x", maxFileBytes+1)},
	})
	result := waitForDiagnostics(t, client.published)
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != protocol.String("vimls/file-too-large") || result.Diagnostics[0].Range.Start != (protocol.Position{}) {
		t.Fatalf("diagnostics = %#v", result)
	}
	instance.publishMu.Lock()
	_, cached := instance.parsed[documentURI.String()]
	instance.publishMu.Unlock()
	if cached {
		t.Fatal("oversized document entered parser cache")
	}
}

func TestDocumentSymbolsUseCurrentSnapshotAndUTF16Ranges(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	initialize, err := instance.Initialize(context.Background(), &protocol.InitializeParams{})
	if err != nil || initialize.Capabilities.DocumentSymbolProvider == nil {
		t.Fatalf("initialize = %#v, error = %v", initialize, err)
	}
	documentURI := uri.MustParse("file:///symbols.vim")
	source := "vim9script\nvar 𐐀name = 1\nclass Widget\n  final ID: number = 1\n  def new()\n    if true\n      var local = 1\n    endif\n  enddef\nendclass\n"
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := instance.DocumentSymbol(context.Background(), &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
	})
	if err != nil {
		t.Fatal(err)
	}
	symbols, ok := result.(protocol.DocumentSymbolSlice)
	if !ok || len(symbols) != 2 {
		t.Fatalf("symbols = %#v", result)
	}
	if symbols[0].Name != "𐐀name" || symbols[0].Kind != protocol.SymbolKindVariable || symbols[0].SelectionRange.Start != (protocol.Position{Line: 1, Character: 4}) || symbols[0].SelectionRange.End != (protocol.Position{Line: 1, Character: 10}) {
		t.Fatalf("unicode variable = %#v", symbols[0])
	}
	class := symbols[1]
	if class.Name != "Widget" || class.Kind != protocol.SymbolKindClass || len(class.Children) != 2 || class.Children[0].Kind != protocol.SymbolKindConstant || class.Children[1].Kind != protocol.SymbolKindConstructor {
		t.Fatalf("class = %#v", class)
	}
	if len(class.Children[1].Children) != 1 || class.Children[1].Children[0].Name != "local" {
		t.Fatalf("constructor = %#v", class.Children[1])
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := instance.DocumentSymbol(canceled, &protocol.DocumentSymbolParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Fatalf("canceled error = %v", err)
	}
}
