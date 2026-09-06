package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
)

func TestLanguageFeaturesPreservePhysicalNewlines(t *testing.T) {
	for _, endings := range [][]string{{"\n"}, {"\r\n"}, {"\r"}, {"\n", "\r", "\r\n"}} {
		for _, encoding := range []text.Encoding{text.UTF8, text.UTF16, text.UTF32} {
			t.Run(fmt.Sprintf("%q/%s", endings, encoding), func(t *testing.T) {
				lines := []string{"\ufeffvim9script", "# 😀é", "# deprecated use NewValue", "var value = 1", "if true", " echo value", "endif", "echo missing", ""}
				var input strings.Builder
				for i, line := range lines {
					if i > 0 {
						input.WriteString(endings[(i-1)%len(endings)])
					}
					input.WriteString(line)
				}
				source := input.String()
				instance, documentURI := openNavigationDocument(t, encoding, source)
				t.Cleanup(instance.stopAnalysis)
				client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
				instance.client = client
				if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source}}); err != nil {
					t.Fatal(err)
				}
				found := false
				for _, diagnostic := range waitForDiagnostics(t, client.published).Diagnostics {
					if diagnostic.Code == protocol.String("vim/E121") {
						found = true
						if diagnostic.Range != navigationRange(7, 5, 12) {
							t.Fatalf("diagnostic range=%#v", diagnostic.Range)
						}
					}
				}
				if !found {
					t.Fatal("missing E121 diagnostic")
				}
				result, err := instance.DocumentSymbol(context.Background(), &protocol.DocumentSymbolParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
				if err != nil {
					t.Fatal(err)
				}
				symbols, ok := result.(protocol.DocumentSymbolSlice)
				if !ok || len(symbols) != 1 || symbols[0].SelectionRange != navigationRange(3, 4, 9) || len(symbols[0].Tags) != 1 || symbols[0].Tags[0] != protocol.SymbolTagDeprecated {
					t.Fatalf("symbols=%#v", result)
				}
				edit, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 5, Character: 8}}, NewName: "renamed"})
				if err != nil || edit == nil || len(edit.DocumentChanges) != 1 {
					t.Fatalf("rename=%#v, %v", edit, err)
				}
				documentEdit, ok := edit.DocumentChanges[0].(*protocol.TextDocumentEdit)
				if !ok || len(documentEdit.Edits) != 2 {
					t.Fatalf("document edit=%#v", edit.DocumentChanges[0])
				}
				var edits []protocol.TextEdit
				for _, element := range documentEdit.Edits {
					textEdit, ok := element.(*protocol.TextEdit)
					if !ok {
						t.Fatalf("edit=%#v", element)
					}
					edits = append(edits, *textEdit)
				}
				if got := applyProtocolEdits(t, source, encoding, edits); got != strings.ReplaceAll(source, "value", "renamed") {
					t.Fatalf("renamed bytes=%q", got)
				}
				edits, err = instance.Formatting(context.Background(), &protocol.DocumentFormattingParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Options: protocol.FormattingOptions{TabSize: 4, InsertSpaces: true}})
				if err != nil {
					t.Fatal(err)
				}
				if got := applyProtocolEdits(t, source, encoding, edits); got != strings.Replace(source, " echo value", "    echo value", 1) {
					t.Fatalf("formatted bytes=%q", got)
				}
				snapshot, _ := instance.documents.Snapshot(documentURI.String())
				if snapshot.Text() != source || snapshot.ContentID() != text.ContentIDOf(source) {
					t.Fatal("feature request mutated document")
				}
			})
		}
	}
}
