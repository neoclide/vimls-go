package server

import (
	"context"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// An editor can race a close notification with any request. Every document
// feature must therefore return its documented empty result for a missing URI,
// without trying to parse or index stale text.
func TestMissingDocumentRequestMatrix(t *testing.T) {
	s := New(nil, nil, nil)
	ctx := context.Background()
	u := uri.File("/tmp/missing.vim")
	pos := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}}
	if got, err := s.Completion(ctx, &protocol.CompletionParams{TextDocumentPositionParams: pos}); err != nil || len(got.(*protocol.CompletionList).Items) != 0 {
		t.Errorf("completion = %#v, %v", got, err)
	}
	if got, err := s.SignatureHelp(ctx, &protocol.SignatureHelpParams{TextDocumentPositionParams: pos}); err != nil || got != nil {
		t.Errorf("signature = %#v, %v", got, err)
	}
	if got, err := s.Definition(ctx, &protocol.DefinitionParams{TextDocumentPositionParams: pos}); err != nil || len(got.(protocol.LocationSlice)) != 0 {
		t.Errorf("definition = %#v, %v", got, err)
	}
	if got, err := s.Declaration(ctx, &protocol.DeclarationParams{TextDocumentPositionParams: pos}); err != nil || len(got.(protocol.LocationSlice)) != 0 {
		t.Errorf("declaration = %#v, %v", got, err)
	}
	if got, err := s.References(ctx, &protocol.ReferenceParams{TextDocumentPositionParams: pos}); err != nil || len(got) != 0 {
		t.Errorf("references = %#v, %v", got, err)
	}
	if got, err := s.DocumentHighlight(ctx, &protocol.DocumentHighlightParams{TextDocumentPositionParams: pos}); err != nil || len(got) != 0 {
		t.Errorf("highlight = %#v, %v", got, err)
	}
	if got, err := s.Hover(ctx, &protocol.HoverParams{TextDocumentPositionParams: pos}); err != nil || got != nil {
		t.Errorf("hover = %#v, %v", got, err)
	}
	if got, err := s.PrepareRename(ctx, &protocol.PrepareRenameParams{TextDocumentPositionParams: pos}); err != nil || got != nil {
		t.Errorf("rename = %#v, %v", got, err)
	}
	if got, err := s.Implementation(ctx, &protocol.ImplementationParams{TextDocumentPositionParams: pos}); err != nil || len(got.(protocol.LocationSlice)) != 0 {
		t.Errorf("implementation = %#v, %v", got, err)
	}
	if got, err := s.PrepareTypeHierarchy(ctx, &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: pos}); err != nil || len(got) != 0 {
		t.Errorf("type hierarchy = %#v, %v", got, err)
	}
	if got, err := s.PrepareCallHierarchy(ctx, &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: pos}); err != nil || len(got) != 0 {
		t.Errorf("call hierarchy = %#v, %v", got, err)
	}
	if got, err := s.DocumentSymbol(ctx, &protocol.DocumentSymbolParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}}); err != nil || len(got.(protocol.DocumentSymbolSlice)) != 0 {
		t.Errorf("symbols = %#v, %v", got, err)
	}
	if got, err := s.FoldingRanges(ctx, &protocol.FoldingRangeParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}}); err != nil || len(got) != 0 {
		t.Errorf("folding = %#v, %v", got, err)
	}
	if got, err := s.SelectionRange(ctx, &protocol.SelectionRangeParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}}); err != nil || len(got) != 0 {
		t.Errorf("selection = %#v, %v", got, err)
	}
	if got, err := s.DocumentLink(ctx, &protocol.DocumentLinkParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}}); err != nil || len(got) != 0 {
		t.Errorf("links = %#v, %v", got, err)
	}
	options := protocol.FormattingOptions{TabSize: 2}
	if got, err := s.Formatting(ctx, &protocol.DocumentFormattingParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}, Options: options}); err != nil || len(got) != 0 {
		t.Errorf("format = %#v, %v", got, err)
	}
	if got, err := s.RangeFormatting(ctx, &protocol.DocumentRangeFormattingParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}, Options: options}); err != nil || len(got) != 0 {
		t.Errorf("range format = %#v, %v", got, err)
	}
	if got, err := s.SemanticTokensFull(ctx, &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}}); err != nil || got == nil || got.ResultID != nil || len(got.Data) != 0 {
		t.Errorf("tokens = %#v, %v", got, err)
	}
	if got, err := s.SemanticTokensFullDelta(ctx, &protocol.SemanticTokensDeltaParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}, PreviousResultID: "unknown"}); err != nil {
		t.Errorf("token delta error = %v", err)
	} else if full, ok := got.(*protocol.SemanticTokens); !ok || full.ResultID != nil || len(full.Data) != 0 {
		t.Errorf("token delta = %#v", got)
	}
	if got, err := s.CodeAction(ctx, &protocol.CodeActionParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}}); err != nil || len(got) != 0 {
		t.Errorf("actions = %#v, %v", got, err)
	}
	if got, err := s.InlayHint(ctx, &protocol.InlayHintParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}}); err != nil || len(got) != 0 {
		t.Errorf("hints = %#v, %v", got, err)
	}
}
