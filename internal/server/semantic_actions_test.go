package server

import (
	"context"
	"errors"
	"testing"

	"github.com/chemzqm/vimls-go/internal/text"
	"go.lsp.dev/protocol"
)

func TestSemanticTokensFullClassifiesSyntaxAndBoundSymbols(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nconst value = 1\n# comment\necho value\n")
	tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint32{
		0, 0, 10, 1, 0,
		1, 0, 5, 1, 0,
		0, 6, 5, 3, 3,
		1, 0, 9, 0, 0,
		1, 0, 4, 1, 0,
		0, 5, 5, 3, 2,
	}
	if len(tokens.Data) != len(want) {
		t.Fatalf("semantic data = %#v", tokens.Data)
	}
	for index := range want {
		if tokens.Data[index] != want[index] {
			t.Fatalf("semantic data[%d] = %d, want %d; all = %#v", index, tokens.Data[index], want[index], tokens.Data)
		}
	}
}

func TestCodeActionInsertsKnownMissingEnd(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nif true\n  echo 'x'\n")
	diagnostic := protocol.Diagnostic{Range: navigationRange(1, 0, 2), Code: protocol.String("vimls/missing-end")}
	actions, err := instance.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Range:        navigationRange(1, 0, 2),
		Context:      protocol.CodeActionContext{Diagnostics: []protocol.Diagnostic{diagnostic}, Only: []protocol.CodeActionKind{protocol.CodeActionKindQuickFix}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions = %#v", actions)
	}
	action, ok := actions[0].(*protocol.CodeAction)
	if !ok || action.Edit == nil || action.Title != "Insert :endif" || len(action.Edit.DocumentChanges) != 1 {
		t.Fatalf("action = %#v", actions[0])
	}
	documentEdit := action.Edit.DocumentChanges[0].(*protocol.TextDocumentEdit)
	textEdit := documentEdit.Edits[0].(*protocol.TextEdit)
	if textEdit.Range != navigationRange(3, 0, 0) || textEdit.NewText != "endif\n" {
		t.Fatalf("text edit = %#v", textEdit)
	}
}

func TestSemanticAndCodeActionBoundaries(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\necho unknown\n")
	actions, err := instance.CodeAction(context.Background(), &protocol.CodeActionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil || len(actions) != 0 {
		t.Fatalf("empty actions = %#v, %v", actions, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := instance.SemanticTokensFull(canceled, &protocol.SemanticTokensParams{}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Fatalf("semantic cancellation = %v", err)
	}
}

func TestSemanticTokensUseNegotiatedEncoding(t *testing.T) {
	tests := []struct {
		name      string
		encoding  text.Encoding
		character uint32
	}{
		{name: "UTF-8", encoding: text.UTF8, character: 19},
		{name: "UTF-16", encoding: text.UTF16, character: 17},
		{name: "UTF-32", encoding: text.UTF32, character: 16},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, test.encoding, "vim9script\nvar value = 1\necho '𐐀' | echo value\n")
			tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
			if err != nil {
				t.Fatal(err)
			}
			line, character := uint32(0), uint32(0)
			found := false
			for index := 0; index+4 < len(tokens.Data); index += 5 {
				line += tokens.Data[index]
				if tokens.Data[index] == 0 {
					character += tokens.Data[index+1]
				} else {
					character = tokens.Data[index+1]
				}
				if line == 2 && character == test.character && tokens.Data[index+3] == 3 {
					found = true
				}
			}
			if !found {
				t.Fatalf("reference token not found in %#v", tokens.Data)
			}
		})
	}
}

func TestInlayHintShowsOnlySafelyInferredTypes(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar inferred = 1\nvar explicit: number = 2\nvar name = 'x'\n")
	hints, err := instance.InlayHint(context.Background(), &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Range:        protocol.Range{Start: protocol.Position{}, End: protocol.Position{Line: 4}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hints) != 2 || hints[0].Position != (protocol.Position{Line: 1, Character: 12}) || hints[0].Label != protocol.String(": number") || hints[1].Label != protocol.String(": string") {
		t.Fatalf("inlay hints = %#v", hints)
	}
}

func TestCodeActionUsesNegotiatedEncodingAtEndOfFile(t *testing.T) {
	tests := []struct {
		name      string
		encoding  text.Encoding
		character uint32
	}{
		{name: "UTF-8", encoding: text.UTF8, character: 13},
		{name: "UTF-16", encoding: text.UTF16, character: 11},
		{name: "UTF-32", encoding: text.UTF32, character: 10},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, test.encoding, "vim9script\nif true\n  echo '𐐀'")
			diagnostic := protocol.Diagnostic{Range: navigationRange(1, 0, 2), Code: protocol.String("vimls/missing-end")}
			actions, err := instance.CodeAction(context.Background(), &protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
				Range:        diagnostic.Range,
				Context:      protocol.CodeActionContext{Diagnostics: []protocol.Diagnostic{diagnostic}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(actions) != 1 {
				t.Fatalf("actions = %#v", actions)
			}
			action := actions[0].(*protocol.CodeAction)
			documentEdit := action.Edit.DocumentChanges[0].(*protocol.TextDocumentEdit)
			textEdit := documentEdit.Edits[0].(*protocol.TextEdit)
			want := protocol.Position{Line: 2, Character: test.character}
			if textEdit.Range.Start != want || textEdit.Range.End != want || textEdit.NewText != "\nendif\n" {
				t.Fatalf("text edit = %#v", textEdit)
			}
		})
	}
}
