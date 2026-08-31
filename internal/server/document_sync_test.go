package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

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
	instance.stopAnalysis()
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
	if got := instance.parseSnapshot(secondSnapshot); got == first {
		t.Fatal("repeated didOpen reused prior lifetime parser tree")
	}
}

func TestServerOldSnapshotParserCacheCannotRestoreCurrentLifetime(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	instance.stopAnalysis()
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
