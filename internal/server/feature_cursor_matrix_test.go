package server

import (
	"context"
	"testing"

	"go.lsp.dev/protocol"
)

// Language requests are made while a cursor moves through incomplete as well
// as complete declarations.  Exercise the shared navigation path at each such
// boundary: no request is allowed to depend on a token being complete.
func TestLanguageRequestsAtEditingBoundaries(t *testing.T) {
	root := t.TempDir()
	source := "vim9script\n" +
		"interface Worker\n  def Run(value: number): string\nendinterface\n" +
		"class Base\n  var value: number = 1\n  def Run(value: number): string\n    return string(value)\n  enddef\nendclass\n" +
		"class Child extends Base implements Worker\n  def Run(value: number): string\n    return string(value)\n  enddef\nendclass\n" +
		"def Use(worker: Worker): string\n  var child = Child.new()\n  return child.Run(1)\nenddef\n" +
		"echo Use(Child.new())\n"
	path := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)
	lines := splitCompletionLines(source)
	ctx := context.Background()
	for line, content := range lines {
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
			if _, err := instance.PrepareTypeHierarchy(ctx, &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("type hierarchy %d:%d: %v", line, character, err)
			}
			if _, err := instance.PrepareCallHierarchy(ctx, &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("call hierarchy %d:%d: %v", line, character, err)
			}
		}
	}
}
