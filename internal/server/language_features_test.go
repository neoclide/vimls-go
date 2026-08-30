package server

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/chemzqm/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestDocumentLinkReturnsOnlyStaticResolvedFiles(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.vim")
	modulePath := filepath.Join(root, "module.vim")
	sourcedPath := filepath.Join(root, "sourced.vim")
	for _, path := range []string{modulePath, sourcedPath} {
		if err := os.WriteFile(path, []byte("vim9script\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	source := "vim9script\nimport './module.vim' as module\nsource sourced.vim\nimport dynamic as nope\n"
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	rootURI := uri.File(root)
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{RootURI: &rootURI}); err != nil {
		t.Fatal(err)
	}
	documentURI := uri.File(mainPath)
	instance.documents.Open(documentURI.String(), 1, source)
	links, err := instance.DocumentLink(context.Background(), &protocol.DocumentLinkParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 || links[0].Target == nil || *links[0].Target != uri.File(modulePath) || links[1].Target == nil || *links[1].Target != uri.File(sourcedPath) {
		t.Fatalf("links = %#v", links)
	}
	if links[0].Range != navigationRange(1, 7, 21) || links[1].Range != navigationRange(2, 7, 18) {
		t.Fatalf("link ranges = %#v", links)
	}
}

func TestLanguageFeatureCapabilitiesAndMethods(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	prepare := true
	result, err := instance.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: protocol.ClientCapabilities{TextDocument: &protocol.TextDocumentClientCapabilities{
		Rename: &protocol.RenameClientCapabilities{PrepareSupport: &prepare},
		CodeAction: &protocol.CodeActionClientCapabilities{CodeActionLiteralSupport: protocol.ClientCodeActionLiteralOptions{
			CodeActionKind: protocol.ClientCodeActionKindOptions{ValueSet: []protocol.CodeActionKind{protocol.CodeActionKindQuickFix}},
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	capabilities := result.Capabilities
	if capabilities.DocumentLinkProvider == nil || capabilities.CompletionProvider == nil || capabilities.SignatureHelpProvider == nil || capabilities.RenameProvider == nil || capabilities.SemanticTokensProvider == nil || capabilities.CodeActionProvider == nil || capabilities.InlayHintProvider == nil {
		t.Fatalf("language capabilities = %#v", capabilities)
	}
	for _, method := range []string{
		protocol.MethodTextDocumentDocumentLink,
		protocol.MethodTextDocumentCompletion,
		protocol.MethodCompletionItemResolve,
		protocol.MethodTextDocumentSignatureHelp,
		protocol.MethodTextDocumentPrepareRename,
		protocol.MethodTextDocumentRename,
		protocol.MethodTextDocumentSemanticTokensFull,
		protocol.MethodTextDocumentCodeAction,
		protocol.MethodTextDocumentInlayHint,
	} {
		if !implementedMethod(method) {
			t.Errorf("method %q is not implemented", method)
		}
	}
}

func TestCompletionUsesCommandAndExpressionContexts(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar value: number = 1\necho value\n\n")
	commandResult, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 3},
	}})
	if err != nil {
		t.Fatal(err)
	}
	commands := commandResult.(protocol.CompletionItemSlice)
	if !hasCompletion(commands, "echo", protocol.CompletionItemKindKeyword) || hasCompletionLabel(commands, "abs") {
		t.Fatalf("command completion missing context filtering")
	}
	expressionResult, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 10},
	}})
	if err != nil {
		t.Fatal(err)
	}
	expressions := expressionResult.(protocol.CompletionItemSlice)
	if !hasCompletion(expressions, "value", protocol.CompletionItemKindVariable) || !hasCompletion(expressions, "abs", protocol.CompletionItemKindFunction) || hasCompletionLabel(expressions, "echo") {
		t.Fatalf("expression completion missing context filtering")
	}
	resolved, err := instance.CompletionResolve(context.Background(), &protocol.CompletionItem{Label: "abs", Kind: protocol.CompletionItemKindFunction})
	if err != nil {
		t.Fatal(err)
	}
	if detail, ok := resolved.Detail.Get(); !ok || detail == "" {
		t.Fatalf("resolved builtin = %#v", resolved)
	}
	argc, err := instance.CompletionResolve(context.Background(), &protocol.CompletionItem{Label: "argc", Kind: protocol.CompletionItemKindFunction})
	if err != nil {
		t.Fatal(err)
	}
	if detail, ok := argc.Detail.Get(); !ok || detail != "builtin function (0..1 arguments): number" {
		t.Fatalf("resolved argc = %#v", argc)
	}
}

func TestCompletionReturnsStaticImportMembers(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Run()\nenddef\nexport const Value = 1\ndef Private()\nenddef\n")
	source := "vim9script\nimport './lib.vim' as lib\necho lib.\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := uri.File(mainPath)
	instance.documents.Open(documentURI.String(), 1, source)
	instance.removeWorkspaceURI(documentURI.String())
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 9},
	}})
	if err != nil {
		t.Fatal(err)
	}
	items := result.(protocol.CompletionItemSlice)
	if !hasCompletionLabel(items, "Run") || !hasCompletionLabel(items, "Value") || hasCompletionLabel(items, "Private") {
		t.Fatalf("import member completion = %#v", items)
	}
}

func TestSignatureHelpForResolvedUserFunction(t *testing.T) {
	source := "vim9script\ndef Add(left: number, right: number = 1): number\n  return left + right\nenddef\necho Add(1, )\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	help, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 4, Character: 12},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if help == nil || len(help.Signatures) != 1 || help.Signatures[0].Label != "Add(left: number, right: number = 1): number" || len(help.Signatures[0].Parameters) != 2 {
		t.Fatalf("signature help = %#v", help)
	}
	if active, ok := help.Signatures[0].ActiveParameter.Get(); !ok || active != 1 {
		t.Fatalf("active parameter = %#v", help.Signatures[0].ActiveParameter)
	}
}

func TestLanguageFeaturesCancellationAndUnknownResults(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\necho unknown(1)\n")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := instance.Completion(canceled, &protocol.CompletionParams{}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Fatalf("completion cancellation = %v", err)
	}
	help, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 13},
	}})
	if err != nil || help != nil {
		t.Fatalf("unknown signature = %#v, %v", help, err)
	}
}

func hasCompletion(items protocol.CompletionItemSlice, label string, kind protocol.CompletionItemKind) bool {
	for _, item := range items {
		if item.Label == label && item.Kind == kind {
			return true
		}
	}
	return false
}

func hasCompletionLabel(items protocol.CompletionItemSlice, label string) bool {
	for _, item := range items {
		if item.Label == label {
			return true
		}
	}
	return false
}
