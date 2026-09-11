package server

import (
	"context"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"testing"
)

func TestVim9UserCommandAttributeValueCompletion(t *testing.T) {
	s, uri := openNavigationDocument(t, text.UTF16, "vim9script\ncommand -nargs=1 Demo echo <args>\n")
	t.Cleanup(s.stopAnalysis)
	result, err := s.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri}, Position: protocol.Position{Line: 1, Character: 15},
	}})
	if err != nil {
		t.Fatal(err)
	}
	items := completionItems(t, result)
	if !hasCompletionLabel(items, "1") || !hasCompletionLabel(items, "*") || hasCompletionLabel(items, "abs") {
		t.Fatalf("wrong attribute completions: %#v", items)
	}
}
