package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestServerVimVersionDiagnosticsFromInitializationOptions(t *testing.T) {
	instance, client := openDiagnosticsServer(t)

	// Target version 9.0.0500:
	// - defer (9.0.0370) is allowed
	// - smoothscroll (9.0.0640) is unknown -> E518
	// - WinResized (9.0.0917) is unknown -> E216
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		InitializationOptions: protocol.LSPAny([]byte(`{"vimVersion":"9.0.0500"}`)),
	}); err != nil {
		t.Fatal(err)
	}

	documentURI := uri.MustParse("file:///version_test.vim")
	source := "defer Close()\nset smoothscroll\nautocmd WinResized * echo 1\n"
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        documentURI,
			LanguageID: "vim",
			Version:    1,
			Text:       source,
		},
	}); err != nil {
		t.Fatal(err)
	}

	params := waitForDiagnosticsForURI(t, client.published, documentURI)
	if params == nil {
		t.Fatal("expected published diagnostics")
	}

	var codes []string
	for _, d := range params.Diagnostics {
		codes = append(codes, fmt.Sprint(d.Code))
	}

	hasE518 := false
	hasE216 := false
	hasE492 := false
	for _, c := range codes {
		if c == "vim/E518" {
			hasE518 = true
		}
		if c == "vim/E216" {
			hasE216 = true
		}
		if c == "vim/E492" {
			hasE492 = true
		}
	}

	if !hasE518 {
		t.Errorf("expected vim/E518 for smoothscroll, got: %v", codes)
	}
	if !hasE216 {
		t.Errorf("expected vim/E216 for WinResized, got: %v", codes)
	}
	if hasE492 {
		t.Errorf("unexpected vim/E492 for defer in 9.0.0500, got: %v", codes)
	}
}

func TestServerVimVersionDiagnosticsDynamicConfiguration(t *testing.T) {
	instance, client := openDiagnosticsServer(t)

	// Initially target 9.0.0500
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		InitializationOptions: protocol.LSPAny([]byte(`{"vimVersion":"9.0.0500"}`)),
	}); err != nil {
		t.Fatal(err)
	}

	documentURI := uri.MustParse("file:///version_dynamic_test.vim")
	source := "set smoothscroll\n"
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        documentURI,
			LanguageID: "vim",
			Version:    1,
			Text:       source,
		},
	}); err != nil {
		t.Fatal(err)
	}

	first := waitForDiagnosticsForURI(t, client.published, documentURI)
	foundE518 := false
	for _, d := range first.Diagnostics {
		if fmt.Sprint(d.Code) == "vim/E518" {
			foundE518 = true
			break
		}
	}
	if !foundE518 {
		t.Fatalf("expected vim/E518 diagnostic for smoothscroll, got: %#v", first.Diagnostics)
	}

	// Upgrade target version to 9.1.0000 via didChangeConfiguration
	if err := instance.applyWorkspaceConfiguration(context.Background(), []byte(`{"vim":{"version":"9.1.0000"}}`)); err != nil {
		t.Fatal(err)
	}

	updated := waitForDiagnosticsForURI(t, client.published, documentURI)
	for _, d := range updated.Diagnostics {
		if fmt.Sprint(d.Code) == "vim/E518" && strings.Contains(fmt.Sprint(d.Message), "smoothscroll") {
			t.Fatalf("unexpected E518 after upgrading target version: %#v", d)
		}
	}
}
