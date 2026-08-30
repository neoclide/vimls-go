package server

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/chemzqm/vimls-go/internal/syntax"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

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

func TestWorkspaceConfigurationRequestOnInitializedAndNullChange(t *testing.T) {
	client := &configurationClient{settings: protocol.LSPAny([]byte(`{"targetVersion":"9.2.4"}`))}
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.client = client
	supported := true
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		InitializationOptions: protocol.LSPAny([]byte(`{"runtimepath":[]}`)),
		Capabilities:          protocol.ClientCapabilities{Workspace: &protocol.WorkspaceClientCapabilities{Configuration: &supported}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	if client.calls != 1 || instance.TargetVersion().String() != "9.2.0004" {
		t.Fatalf("initialized configuration calls=%d target=%s", client.calls, instance.TargetVersion().String())
	}
	client.settings = protocol.LSPAny([]byte(`{"targetVersion":"latest"}`))
	if err := instance.DidChangeConfiguration(context.Background(), &protocol.DidChangeConfigurationParams{Settings: protocol.LSPAny([]byte("null"))}); err != nil {
		t.Fatal(err)
	}
	if client.calls != 2 || instance.TargetVersion().String() != "latest" {
		t.Fatalf("changed configuration calls=%d target=%s", client.calls, instance.TargetVersion().String())
	}
}
