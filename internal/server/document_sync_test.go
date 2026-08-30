package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

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
	// Repeating the same client version is stale and must not replace the snapshot.
	_ = instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI},
			Version:                2,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: "stale"},
		},
	})
	snapshot, ok := instance.documents.Snapshot(documentURI.String())
	if !ok || snapshot.Text() != "new" {
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
	parsed := instance.parsed[documentURI.String()].file
	instance.publishMu.Unlock()
	if parsed == nil || len(parsed.Commands) != 0 || len(parsed.Source) != maxFileBytes+1 {
		t.Fatalf("parsed = %#v", parsed)
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
