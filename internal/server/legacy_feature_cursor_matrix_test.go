package server

import (
	"context"
	"testing"

	"go.lsp.dev/protocol"
)

// Legacy-root files take distinct syntax and conservative semantic paths from
// Vim9 files. Exercise each advertised document feature at command and
// expression boundaries in a representative legacy script.
func TestLegacyLanguageRequestsAtEditingBoundaries(t *testing.T) {
	root := t.TempDir()
	source := `let g:Value = 1
function! s:Compute(value, ...) abort
  let l:local = a:value + g:Value
  if l:local > 0
    echo printf('%d', l:local)
  endif
  return l:local
endfunction
command! -nargs=* Demo call s:Compute(<args>)
augroup LegacyCoverage
  autocmd! BufEnter *.vim call s:Compute(1)
augroup END
let s:result = s:Compute(g:Value)
echo s:result
`
	path := writeWorkspaceFile(t, root, "legacy.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)
	ctx := context.Background()
	for line, content := range splitCompletionLines(source) {
		for _, character := range []uint32{0, uint32(len(content) / 2), uint32(len(content))} {
			position := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: uint32(line), Character: character}}
			if _, err := instance.Completion(ctx, &protocol.CompletionParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("completion %d:%d: %v", line, character, err)
			}
			if _, err := instance.SignatureHelp(ctx, &protocol.SignatureHelpParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("signature %d:%d: %v", line, character, err)
			}
			if _, err := instance.Definition(ctx, &protocol.DefinitionParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("definition %d:%d: %v", line, character, err)
			}
			if _, err := instance.Declaration(ctx, &protocol.DeclarationParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("declaration %d:%d: %v", line, character, err)
			}
			if _, err := instance.References(ctx, &protocol.ReferenceParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("references %d:%d: %v", line, character, err)
			}
			if _, err := instance.DocumentHighlight(ctx, &protocol.DocumentHighlightParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("highlight %d:%d: %v", line, character, err)
			}
			if _, err := instance.Hover(ctx, &protocol.HoverParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("hover %d:%d: %v", line, character, err)
			}
			if _, err := instance.PrepareRename(ctx, &protocol.PrepareRenameParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("rename %d:%d: %v", line, character, err)
			}
			if _, err := instance.Implementation(ctx, &protocol.ImplementationParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("implementation %d:%d: %v", line, character, err)
			}
		}
	}
}
