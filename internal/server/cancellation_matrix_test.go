package server

import (
	"context"
	"errors"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// Requests may be cancelled between dispatch and any parsing/index work.  Run
// every request family against an open document with a cancelled context so
// that this contract remains consistent as handlers are added.
func TestCancelledRequestMatrix(t *testing.T) {
	s := New(nil, nil, nil)
	u := uri.File("/tmp/cancelled.vim")
	s.documents.Open(u.String(), 1, "vim9script\nvar item = 1\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pos := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}}
	mustCancelled := func(name string, err error) {
		if !errors.Is(err, protocol.ErrRequestCancelled) {
			t.Errorf("%s error = %v", name, err)
		}
	}
	_, err := s.Completion(ctx, &protocol.CompletionParams{TextDocumentPositionParams: pos})
	mustCancelled("completion", err)
	_, err = s.SignatureHelp(ctx, &protocol.SignatureHelpParams{TextDocumentPositionParams: pos})
	mustCancelled("signature", err)
	_, err = s.Definition(ctx, &protocol.DefinitionParams{TextDocumentPositionParams: pos})
	mustCancelled("definition", err)
	_, err = s.Declaration(ctx, &protocol.DeclarationParams{TextDocumentPositionParams: pos})
	mustCancelled("declaration", err)
	_, err = s.References(ctx, &protocol.ReferenceParams{TextDocumentPositionParams: pos})
	mustCancelled("references", err)
	_, err = s.DocumentHighlight(ctx, &protocol.DocumentHighlightParams{TextDocumentPositionParams: pos})
	mustCancelled("highlight", err)
	_, err = s.Hover(ctx, &protocol.HoverParams{TextDocumentPositionParams: pos})
	mustCancelled("hover", err)
	_, err = s.PrepareRename(ctx, &protocol.PrepareRenameParams{TextDocumentPositionParams: pos})
	mustCancelled("prepare rename", err)
	_, err = s.Implementation(ctx, &protocol.ImplementationParams{TextDocumentPositionParams: pos})
	mustCancelled("implementation", err)
	_, err = s.PrepareTypeHierarchy(ctx, &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: pos})
	mustCancelled("type hierarchy", err)
	_, err = s.PrepareCallHierarchy(ctx, &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: pos})
	mustCancelled("call hierarchy", err)
	_, err = s.DocumentSymbol(ctx, &protocol.DocumentSymbolParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}})
	mustCancelled("symbols", err)
	_, err = s.FoldingRanges(ctx, &protocol.FoldingRangeParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}})
	mustCancelled("folding", err)
	_, err = s.SelectionRange(ctx, &protocol.SelectionRangeParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}})
	mustCancelled("selection", err)
	_, err = s.DocumentLink(ctx, &protocol.DocumentLinkParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}})
	mustCancelled("links", err)
	_, err = s.Formatting(ctx, &protocol.DocumentFormattingParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}, Options: protocol.FormattingOptions{TabSize: 2}})
	mustCancelled("format", err)
	_, err = s.RangeFormatting(ctx, &protocol.DocumentRangeFormattingParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}, Options: protocol.FormattingOptions{TabSize: 2}})
	mustCancelled("range format", err)
	_, err = s.SemanticTokensFull(ctx, &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}})
	mustCancelled("tokens", err)
	_, err = s.CodeAction(ctx, &protocol.CodeActionParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}})
	mustCancelled("actions", err)
	_, err = s.InlayHint(ctx, &protocol.InlayHintParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}})
	mustCancelled("hints", err)
	_, err = s.Diagnostic(ctx, &protocol.DocumentDiagnosticParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}})
	mustCancelled("diagnostics", err)
	_, err = s.DiagnosticWorkspace(ctx, &protocol.WorkspaceDiagnosticParams{})
	mustCancelled("workspace diagnostics", err)
}
