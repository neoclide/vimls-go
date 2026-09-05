package server

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestDeletingScriptLocalMappingFunctionPublishesDiagnostic(t *testing.T) {
	root := t.TempDir()
	instance, published := initializeWorkspaceDiagnosticServer(t, root)
	documentURI := uri.File(filepath.Join(root, "session.vim"))
	mapping := "nmap <leader>sr :call <SID>SessionReload()<CR>\n"
	source := "function! s:SessionReload() abort\nendfunction\n" + mapping
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source}}); err != nil {
		t.Fatal(err)
	}
	before := waitForDiagnosticsForURI(t, published, documentURI)
	for _, diagnostic := range before.Diagnostics {
		if diagnostic.Code == protocol.String("vim/E117") {
			t.Fatalf("defined function diagnostics = %#v", before.Diagnostics)
		}
	}
	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{Text: mapping}},
	}); err != nil {
		t.Fatal(err)
	}
	after := waitForDiagnosticsForURI(t, published, documentURI)
	var missing []protocol.Diagnostic
	for _, diagnostic := range after.Diagnostics {
		if diagnostic.Code == protocol.String("vim/E117") {
			missing = append(missing, diagnostic)
		}
	}
	start := uint32(strings.Index(mapping, "SessionReload"))
	if len(missing) != 1 || missing[0].Code != protocol.String("vim/E117") || missing[0].Message != protocol.String("Unknown function: s:SessionReload") || missing[0].Range != navigationRange(0, start, start+uint32(len("SessionReload"))) || missing[0].Severity != protocol.DiagnosticSeverityWarning {
		t.Fatalf("deleted function diagnostics = %#v", after.Diagnostics)
	}
}
