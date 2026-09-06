package server

import (
	"context"
	"testing"
	"unicode/utf8"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestNeovimOptionHintProtocolRangeAndSettings(t *testing.T) {
	const prefix = "echo '😀é' | set scl="
	source := prefix + "auto:2\r\n"
	file := syntax.Parse(source)
	file.Diagnostics = analysis.CombinedDiagnostics(file, analysis.Analyze(file))
	snapshot := text.NewSnapshot("file:///compat.vim", 1, nil, source)
	for _, test := range []struct {
		encoding text.Encoding
		start    int
	}{
		{text.UTF8, len(prefix)}, {text.UTF16, utf8.RuneCountInString(prefix) + 1}, {text.UTF32, utf8.RuneCountInString(prefix)},
	} {
		diagnostics := protocolDiagnostics(snapshot, file, test.encoding, false, nil, 100)
		if len(diagnostics) != 1 {
			t.Fatalf("diagnostics %#v", diagnostics)
		}
		diagnostic := diagnostics[0]
		want := protocol.Range{Start: protocol.Position{Character: uint32(test.start)}, End: protocol.Position{Character: uint32(test.start + 6)}}
		if diagnostic.Code != protocol.String("vimls/neovim-only-option") || diagnostic.Severity != protocol.DiagnosticSeverityHint || diagnostic.Range != want {
			t.Fatalf("%s diagnostic %#v want range %#v", test.encoding, diagnostic, want)
		}
	}
	overrides := map[string]protocol.DiagnosticSeverity{"vimls/neovim-only-option": protocol.DiagnosticSeverityWarning}
	if diagnostics := protocolDiagnostics(snapshot, file, text.UTF16, false, overrides, 100); len(diagnostics) != 1 || diagnostics[0].Severity != protocol.DiagnosticSeverityWarning {
		t.Fatalf("override %#v", diagnostics)
	}
	if diagnostics := filterDisabledDiagnostics(file.Diagnostics, map[string]struct{}{"vimls/neovim-only-option": {}}); len(diagnostics) != 0 {
		t.Fatalf("disabled %#v", diagnostics)
	}
}

func TestNeovimOptionGuardEditRepublishesDiagnostics(t *testing.T) {
	instance, client := openDiagnosticsServer(t)
	documentURI := uri.MustParse("file:///neovim-option.vim")
	for index, source := range []string{
		"set scl=auto:2\n",
		"if has('nvim')\nset scl=auto:2\nendif\n",
		"if !has('nvim')\nset scl=auto:2\nendif\n",
		"if has('nvim')\nset scl=auto:10\nendif\n",
	} {
		version := int32(index + 1)
		var err error
		if index == 0 {
			err = instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: documentURI, LanguageID: "vim", Version: version, Text: source}})
		} else {
			err = instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
				TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: version},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{Text: source}},
			})
		}
		if err != nil {
			t.Fatal(err)
		}
		published := waitForDiagnostics(t, client.published)
		if got, ok := published.Version.Get(); !ok || got != version {
			t.Fatalf("version %#v want %d", published.Version, version)
		}
		if index == 1 {
			if len(published.Diagnostics) != 0 {
				t.Fatalf("guarded diagnostics %#v", published.Diagnostics)
			}
			continue
		}
		code, severity := "vimls/neovim-only-option", protocol.DiagnosticSeverityHint
		if index == 3 {
			code, severity = "vim/E474", protocol.DiagnosticSeverityError
		}
		if len(published.Diagnostics) != 1 || published.Diagnostics[0].Code != protocol.String(code) || published.Diagnostics[0].Severity != severity {
			t.Fatalf("version %d diagnostics %#v", version, published.Diagnostics)
		}
	}
}

func TestMacVimOptionHintProtocol(t *testing.T) {
	source := "set macmeta\n"
	file := syntax.Parse(source)
	file.Diagnostics = analysis.CombinedDiagnostics(file, analysis.Analyze(file))
	snapshot := text.NewSnapshot("file:///macvim.vim", 1, nil, source)
	diagnostics := protocolDiagnostics(snapshot, file, text.UTF16, false, nil, 100)
	want := protocol.Range{Start: protocol.Position{Character: 4}, End: protocol.Position{Character: 11}}
	if len(diagnostics) != 1 || diagnostics[0].Code != protocol.String("vimls/macvim-only-option") || diagnostics[0].Severity != protocol.DiagnosticSeverityHint || diagnostics[0].Range != want {
		t.Fatalf("diagnostics %#v", diagnostics)
	}
	if got := filterDisabledDiagnostics(file.Diagnostics, map[string]struct{}{"vimls/macvim-only-option": {}}); len(got) != 0 {
		t.Fatal(got)
	}
}
