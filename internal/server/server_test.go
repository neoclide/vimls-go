package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/chemzqm/vimls-go/internal/jsonrpc"
	"github.com/chemzqm/vimls-go/internal/syntax"
	"github.com/chemzqm/vimls-go/internal/text"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestNegotiatePositionEncoding(t *testing.T) {
	tests := []struct {
		name    string
		general *protocol.GeneralClientCapabilities
		want    text.Encoding
		wire    protocol.PositionEncodingKind
	}{
		{name: "omitted defaults to UTF-16", want: text.UTF16, wire: protocol.PositionEncodingKindUTF16},
		{name: "empty defaults to UTF-16", general: &protocol.GeneralClientCapabilities{}, want: text.UTF16, wire: protocol.PositionEncodingKindUTF16},
		{name: "client preference", general: &protocol.GeneralClientCapabilities{PositionEncodings: []protocol.PositionEncodingKind{protocol.PositionEncodingKindUTF8, protocol.PositionEncodingKindUTF16}}, want: text.UTF8, wire: protocol.PositionEncodingKindUTF8},
		{name: "UTF-32", general: &protocol.GeneralClientCapabilities{PositionEncodings: []protocol.PositionEncodingKind{protocol.PositionEncodingKindUTF32}}, want: text.UTF32, wire: protocol.PositionEncodingKindUTF32},
		{name: "unknown falls back to UTF-16", general: &protocol.GeneralClientCapabilities{PositionEncodings: []protocol.PositionEncodingKind{"custom"}}, want: text.UTF16, wire: protocol.PositionEncodingKindUTF16},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, wire := negotiatePositionEncoding(test.general)
			if got != test.want || wire != test.wire {
				t.Fatalf("encoding = %v/%q, want %v/%q", got, wire, test.want, test.wire)
			}
		})
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
	if len(messages) != 2 {
		t.Fatalf("responses = %d, want 2", len(messages))
	}
	if result := messages[0]["result"]; !bytes.Contains(result, []byte(`"positionEncoding":"utf-8"`)) || !bytes.Contains(result, []byte(`"change":2`)) {
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

func TestProtocolDiagnosticSeverity(t *testing.T) {
	if got := protocolDiagnosticSeverity(syntax.DiagnosticUnknownOption); got != protocol.DiagnosticSeverityWarning {
		t.Fatalf("unknown-option severity = %v, want warning", got)
	}
	for _, code := range []string{"vimls/missing-end", "vim/E1012", "future/source"} {
		if got := protocolDiagnosticSeverity(code); got != protocol.DiagnosticSeverityError {
			t.Errorf("%s severity = %v, want error", code, got)
		}
	}
}

func TestServerTruncatesDiagnosticsDeterministically(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 1)}
	instance.mu.Lock()
	instance.client = client
	instance.mu.Unlock()
	_, _ = instance.Initialize(context.Background(), &protocol.InitializeParams{})
	documentURI := uri.MustParse("file:///many-diagnostics.vim")
	_ = instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: strings.Repeat("if true\n", maxDiagnosticsPerDocument+25)},
	})
	result := waitForDiagnostics(t, client.published)
	if len(result.Diagnostics) != maxDiagnosticsPerDocument || result.Diagnostics[len(result.Diagnostics)-1].Code != protocol.String("vimls/diagnostics-truncated") {
		t.Fatalf("diagnostic count = %d, last = %#v", len(result.Diagnostics), result.Diagnostics[len(result.Diagnostics)-1])
	}
}

func TestInitializationTargetOverrideHasPrecedence(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		InitializationOptions: []byte(`{"targetVersion":"9.1.1232"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := instance.DidChangeConfiguration(context.Background(), &protocol.DidChangeConfigurationParams{
		Settings: []byte(`{"vimls":{"targetVersion":"9.2.4"}}`),
	}); err != nil {
		t.Fatal(err)
	}
	if got := instance.TargetVersion().String(); got != "9.1.1232" {
		t.Fatalf("target version = %q", got)
	}
	if revision := instance.documents.ConfigRevision(); revision != 2 {
		t.Fatalf("config revision = %d, want 2", revision)
	}
}

func TestServerPublishesVersionedSyntaxDiagnosticsAndClearsThem(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
	instance.mu.Lock()
	instance.client = client
	instance.mu.Unlock()
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{}); err != nil {
		t.Fatal(err)
	}
	documentURI := uri.MustParse("file:///diagnostics.vim")
	_ = instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: "if true\n"},
	})
	first := waitForDiagnostics(t, client.published)
	if version, ok := first.Version.Get(); !ok || version != 1 || len(first.Diagnostics) != 1 || first.Diagnostics[0].Code != protocol.String("vimls/missing-end") {
		t.Fatalf("first diagnostics = %#v", first)
	}
	_ = instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{Text: "let g:value = 1\n"}},
	})
	cleared := waitForDiagnostics(t, client.published)
	if version, ok := cleared.Version.Get(); !ok || version != 2 || len(cleared.Diagnostics) != 0 {
		t.Fatalf("cleared diagnostics = %#v", cleared)
	}
	instance.publishMu.Lock()
	parsed := instance.parsed[documentURI.String()].file
	instance.publishMu.Unlock()
	if parsed == nil || parsed.Dialect != syntax.Legacy || len(parsed.Commands) != 1 {
		t.Fatalf("parsed file = %#v", parsed)
	}
}

func TestServerPublishesVersionedSemanticDiagnosticsAndClearsThem(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
	instance.mu.Lock()
	instance.client = client
	instance.mu.Unlock()
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{}); err != nil {
		t.Fatal(err)
	}
	documentURI := uri.MustParse("file:///semantic-diagnostics.vim")
	_ = instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI: documentURI, Version: 1,
			Text: "vim9script\nconst value = 1\nvalue = 2\n",
		},
	})
	first := waitForDiagnostics(t, client.published)
	if version, ok := first.Version.Get(); !ok || version != 1 || len(first.Diagnostics) != 1 {
		t.Fatalf("first diagnostics = %#v", first)
	}
	diagnostic := first.Diagnostics[0]
	if diagnostic.Code != protocol.String("vim/E46") || diagnostic.Message != protocol.String(`Cannot change read-only variable "value"`) ||
		diagnostic.Range != (protocol.Range{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 2, Character: 5}}) {
		t.Fatalf("semantic diagnostic = %#v", diagnostic)
	}

	_ = instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{
			Text: "vim9script\nvar value = 1\nvalue = 2\n",
		}},
	})
	cleared := waitForDiagnostics(t, client.published)
	if version, ok := cleared.Version.Get(); !ok || version != 2 || len(cleared.Diagnostics) != 0 {
		t.Fatalf("cleared diagnostics = %#v", cleared)
	}
}

func TestTargetVersionCompatibilityDiagnosticsReanalyze(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
	instance.mu.Lock()
	instance.client = client
	instance.mu.Unlock()
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{}); err != nil {
		t.Fatal(err)
	}
	documentURI := uri.MustParse("file:///versioned-syntax.vim")
	_ = instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: "vim9script\nenum Color\n  Red\nendenum\n"},
	})
	first := waitForDiagnostics(t, client.published)
	if len(first.Diagnostics) != 1 || first.Diagnostics[0].Code != protocol.String("vimls/target-version") {
		t.Fatalf("default-target diagnostics = %#v", first)
	}
	_ = instance.DidChangeConfiguration(context.Background(), &protocol.DidChangeConfigurationParams{Settings: []byte(`{"targetVersion":"9.2.1015"}`)})
	cleared := waitForDiagnostics(t, client.published)
	if len(cleared.Diagnostics) != 0 {
		t.Fatalf("updated-target diagnostics = %#v", cleared)
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

func TestServerLifecycle(t *testing.T) {
	input := encodeFrames(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{},"initializationOptions":{"targetVersion":"9.1.1232"}}}`,
		`{"jsonrpc":"2.0","method":"initialized","params":{}}`,
		`{"jsonrpc":"2.0","method":"custom/notification"}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var output, logs bytes.Buffer
	instance := New(&input, &output, &logs)
	if code := instance.Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d, output = %q, logs = %s", code, output.String(), logs.String())
	}
	if got := instance.TargetVersion().String(); got != "9.1.1232" {
		t.Fatalf("target version = %s", got)
	}
	messages := decodeFrames(t, &output)
	if len(messages) != 2 {
		t.Fatalf("responses = %d, want 2", len(messages))
	}
	if idNumber(t, messages[0]) != 1 || string(messages[0]["result"]) == "null" {
		t.Fatalf("initialize response = %s", messages[0])
	}
	if idNumber(t, messages[1]) != 2 || string(messages[1]["result"]) != "null" {
		t.Fatalf("shutdown response = %s", messages[1])
	}
	if logs.Len() != 0 {
		t.Fatalf("logs = %q", logs.String())
	}
}

func TestServerLifecycleErrors(t *testing.T) {
	input := encodeFrames(t,
		`{"jsonrpc":"2.0","id":0,"method":"textDocument/hover"}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"unknown"}`,
		`{"jsonrpc":"2.0","id":3,"method":"exit"}`,
		`{"jsonrpc":"2.0","id":4,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","id":5,"method":"textDocument/hover"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var output bytes.Buffer
	var logs bytes.Buffer
	if code := New(&input, &output, &logs).Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d, output = %q, logs = %q", code, output.String(), logs.String())
	}
	messages := decodeFrames(t, &output)
	wantCodes := []int{int(jsonrpc2.ServerNotInitialized), 0, int(jsonrpc2.InvalidRequest), int(jsonrpc2.MethodNotFound), int(jsonrpc2.InvalidRequest), 0, int(jsonrpc2.InvalidRequest)}
	if len(messages) != len(wantCodes) {
		t.Fatalf("responses = %d, want %d", len(messages), len(wantCodes))
	}
	for i, want := range wantCodes {
		if got := errorCode(t, messages[i]); got != want {
			t.Fatalf("response %d code = %d, want %d: %s", i, got, want, messages[i])
		}
	}
}

func TestServerRecoversAfterMalformedJSONBody(t *testing.T) {
	input := encodeFrames(t,
		`{`,
		`{"jsonrpc":"2.0","id":true,"method":"bad-id"}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var output, logs bytes.Buffer
	if code := New(&input, &output, &logs).Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d, output = %q, logs = %q", code, output.String(), logs.String())
	}
	messages := decodeFrames(t, &output)
	if len(messages) != 4 || errorCode(t, messages[0]) != int(jsonrpc2.ParseError) || string(messages[1]["id"]) != "null" {
		t.Fatalf("responses = %#v", messages)
	}
}

func TestServerDoesNotRespondToInvalidNotificationParams(t *testing.T) {
	input := encodeFrames(t,
		`{"jsonrpc":"2.0","method":"ignored","params":"invalid"}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var output bytes.Buffer
	if code := New(&input, &output, io.Discard).Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if messages := decodeFrames(t, &output); len(messages) != 2 {
		t.Fatalf("responses = %d, want 2", len(messages))
	}
}

func TestServerRejectsInvalidLifecycleShapes(t *testing.T) {
	input := encodeFrames(t,
		`{"jsonrpc":"2.0","method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":[]}`,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"initialized"}`,
		`{"jsonrpc":"2.0","id":4,"method":"$/cancelRequest"}`,
		`{"jsonrpc":"2.0","method":"shutdown"}`,
		`{"jsonrpc":"2.0","id":5,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var output bytes.Buffer
	if code := New(&input, &output, io.Discard).Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	messages := decodeFrames(t, &output)
	wantCodes := []int{int(jsonrpc2.ParseError), 0, int(jsonrpc2.InvalidRequest), int(jsonrpc2.InvalidRequest), 0}
	if len(messages) != len(wantCodes) {
		t.Fatalf("responses = %d, want %d: %#v", len(messages), len(wantCodes), messages)
	}
	for i, want := range wantCodes {
		if got := errorCode(t, messages[i]); got != want {
			t.Fatalf("response %d code = %d, want %d", i, got, want)
		}
	}
}

func TestServerWarnsForInvalidTargetAfterInitialized(t *testing.T) {
	input := encodeFrames(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"initializationOptions":{"targetVersion":"9.0"}}}`,
		`{"jsonrpc":"2.0","method":"initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var output, logs bytes.Buffer
	instance := New(&input, &output, &logs)
	if code := instance.Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d, output = %q, logs = %q", code, output.String(), logs.String())
	}
	if got := instance.TargetVersion().String(); got != DefaultTargetVersion {
		t.Fatalf("target = %s", got)
	}
	messages := decodeFrames(t, &output)
	if len(messages) != 3 || string(messages[1]["method"]) != `"window/logMessage"` {
		t.Fatalf("messages = %#v", messages)
	}
	if !strings.Contains(string(messages[1]["params"]), "versions before 9.1") {
		t.Fatalf("warning = %s", messages[1]["params"])
	}
}

func TestServerExitBeforeShutdownFails(t *testing.T) {
	input := encodeFrames(t, `{"jsonrpc":"2.0","method":"exit"}`)
	if code := New(&input, io.Discard, io.Discard).Run(context.Background()); code != 1 {
		t.Fatalf("exit code = %d", code)
	}
}

func TestServerFramingAndOutputFailuresAreControlled(t *testing.T) {
	var logs bytes.Buffer
	code := New(strings.NewReader("invalid"), io.Discard, &logs).Run(context.Background())
	if code != 1 || !strings.Contains(logs.String(), "connection error") {
		t.Fatalf("code = %d, logs = %q", code, logs.String())
	}

	input := encodeFrames(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	logs.Reset()
	code = New(&input, errorWriter{}, &logs).Run(context.Background())
	if code != 1 || !strings.Contains(logs.String(), "connection error") {
		t.Fatalf("code = %d, logs = %q", code, logs.String())
	}
}

func TestServerCancellationAndEOFExitCleanly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := New(strings.NewReader("blocked input is not read"), io.Discard, io.Discard).Run(ctx); code != 0 {
		t.Fatalf("canceled exit code = %d", code)
	}
	if code := New(strings.NewReader(""), io.Discard, io.Discard).Run(context.Background()); code != 0 {
		t.Fatalf("EOF exit code = %d", code)
	}

	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel = context.WithCancel(context.Background())
	result := make(chan int, 1)
	go func() {
		result <- New(reader, io.Discard, io.Discard).Run(ctx)
	}()
	cancel()
	select {
	case code := <-result:
		if code != 0 {
			t.Fatalf("blocked cancellation exit code = %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked read did not stop after cancellation")
	}
}

func TestServerSoakNotifications(t *testing.T) {
	const notifications = 10_000
	var input bytes.Buffer
	writer := jsonrpc.NewWriter(&input)
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	for range notifications {
		writeFrame(t, writer, `{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":999}}`)
	}
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":2,"method":"shutdown"}`)
	writeFrame(t, writer, `{"jsonrpc":"2.0","method":"exit"}`)

	var output bytes.Buffer
	if code := New(&input, &output, io.Discard).Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if messages := decodeFrames(t, &output); len(messages) != 2 {
		t.Fatalf("responses = %d", len(messages))
	}
}

func encodeFrames(t *testing.T, bodies ...string) bytes.Buffer {
	t.Helper()
	var input bytes.Buffer
	writer := jsonrpc.NewWriter(&input)
	for _, body := range bodies {
		writeFrame(t, writer, body)
	}
	return input
}

func writeFrame(t *testing.T, writer *jsonrpc.Writer, body string) {
	t.Helper()
	if err := writer.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
}

func decodeFrames(t *testing.T, input io.Reader) []map[string]json.RawMessage {
	t.Helper()
	reader := jsonrpc.NewReader(input)
	var messages []map[string]json.RawMessage
	for {
		body, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return messages
		}
		if err != nil {
			t.Fatal(err)
		}
		var message map[string]json.RawMessage
		if err := json.Unmarshal(body, &message); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
	}
}

func idNumber(t *testing.T, message map[string]json.RawMessage) int {
	t.Helper()
	var id int
	if err := json.Unmarshal(message["id"], &id); err != nil {
		t.Fatal(err)
	}
	return id
}

func errorCode(t *testing.T, message map[string]json.RawMessage) int {
	t.Helper()
	if len(message["error"]) == 0 {
		return 0
	}
	var responseError struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(message["error"], &responseError); err != nil {
		t.Fatal(err)
	}
	return responseError.Code
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

type diagnosticClient struct {
	protocol.UnimplementedClient
	published chan *protocol.PublishDiagnosticsParams
}

func (c *diagnosticClient) PublishDiagnostics(_ context.Context, params *protocol.PublishDiagnosticsParams) error {
	c.published <- params
	return nil
}

func waitForDiagnostics(t *testing.T, published <-chan *protocol.PublishDiagnosticsParams) *protocol.PublishDiagnosticsParams {
	t.Helper()
	select {
	case params := <-published:
		return params
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for diagnostics")
		return nil
	}
}
