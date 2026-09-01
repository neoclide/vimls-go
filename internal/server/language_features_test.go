package server

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
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
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI:               &rootURI,
		InitializationOptions: protocol.LSPAny([]byte(`{"runtimepath":[]}`)),
	}); err != nil {
		t.Fatal(err)
	}
	documentURI := uri.File(mainPath)
	instance.documents.Open(documentURI.String(), 1, source)
	links, err := instance.DocumentLink(context.Background(), &protocol.DocumentLinkParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	canonicalURI := func(path string) uri.URI {
		realpath, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		return uri.File(realpath)
	}
	if len(links) != 2 || links[0].Target == nil || *links[0].Target != canonicalURI(modulePath) || links[1].Target == nil || *links[1].Target != canonicalURI(sourcedPath) {
		t.Fatalf("links = %#v", links)
	}
	if links[0].Range != navigationRange(1, 7, 21) || links[1].Range != navigationRange(2, 7, 18) {
		t.Fatalf("link ranges = %#v", links)
	}
}

func TestDeprecatedVim9DeclarationsReachLanguageFeatures(t *testing.T) {
	source := "vim9script\n# deprecated use NewValue\nvar OldValue = 1\n# @DEPRECATED use NewFunc\ndef OldFunc()\nenddef\necho OldValue\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	capabilities := protocol.ClientCapabilities{TextDocument: &protocol.TextDocumentClientCapabilities{
		Completion: &protocol.CompletionClientCapabilities{CompletionItem: &protocol.ClientCompletionItemOptions{
			TagSupport: protocol.CompletionItemTagOptions{ValueSet: []protocol.CompletionItemTag{protocol.CompletionItemTagDeprecated}},
		}},
	}}
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: capabilities}); err != nil {
		t.Fatal(err)
	}

	result, err := instance.DocumentSymbol(context.Background(), &protocol.DocumentSymbolParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	symbols := result.(protocol.DocumentSymbolSlice)
	if len(symbols) != 2 || len(symbols[0].Tags) != 1 || symbols[0].Tags[0] != protocol.SymbolTagDeprecated || len(symbols[1].Tags) != 1 || symbols[1].Tags[0] != protocol.SymbolTagDeprecated {
		t.Fatalf("document symbols = %#v", symbols)
	}

	completion, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 6, Character: 5},
	}})
	if err != nil {
		t.Fatal(err)
	}
	deprecated := map[string]bool{}
	for _, item := range completionItems(t, completion) {
		if item.Label == "OldValue" || item.Label == "OldFunc" {
			deprecated[item.Label] = len(item.Tags) == 1 && item.Tags[0] == protocol.CompletionItemTagDeprecated
		}
	}
	if !deprecated["OldValue"] || !deprecated["OldFunc"] {
		t.Fatalf("deprecated completions = %#v", deprecated)
	}

	tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	deprecatedTokens := 0
	for index := 0; index+4 < len(tokens.Data); index += 5 {
		if tokens.Data[index+4]&4 != 0 {
			deprecatedTokens++
		}
	}
	if deprecatedTokens != 3 {
		t.Fatalf("deprecated semantic tokens = %d, data = %#v", deprecatedTokens, tokens.Data)
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
	commands := completionItems(t, commandResult)
	if !hasCompletion(commands, "echo", protocol.CompletionItemKindKeyword) || hasCompletionLabel(commands, "abs") {
		t.Fatalf("command completion missing context filtering")
	}
	expressionResult, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 5},
	}})
	if err != nil {
		t.Fatal(err)
	}
	expressions := completionItems(t, expressionResult)
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
	items := completionItems(t, result)
	if !hasCompletionLabel(items, "Run") || !hasCompletionLabel(items, "Value") || hasCompletionLabel(items, "Private") {
		t.Fatalf("import member completion = %#v", items)
	}
}

func TestCompletionImportMemberPrefixAndEdit(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Run()\nenddef\nexport def Rest()\nenddef\n")
	source := "vim9script\nimport './lib.vim' as alias\necho alias.Ru\n"
	main := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := uri.File(main)
	instance.documents.Open(documentURI.String(), 1, source)
	for _, character := range []uint32{11, 13} {
		result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: character}}})
		if err != nil {
			t.Fatal(err)
		}
		items := completionItems(t, result)
		if !hasCompletionLabel(items, "Run") || character == 13 && hasCompletionLabel(items, "Rest") {
			t.Fatalf("prefix %d: %#v", character, items)
		}
		for _, item := range items {
			if item.Label == "Run" {
				edit, ok := item.TextEdit.(*protocol.TextEdit)
				if !ok || edit.Range != navigationRange(2, 11, 13) {
					t.Fatalf("source %q edit %#v", source, item.TextEdit)
				}
				break
			}
		}
	}
}

func TestCompletionStaticMembersAndUnknown(t *testing.T) {
	source := "vim9script\nclass Box\n  var value: number\n  def Run()\n  enddef\nendclass\nvar obj: Box = Box.new()\necho obj.\necho Box.\necho unknown.\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	for _, test := range []struct {
		line uint32
		want string
	}{{7, "value"}, {8, "Run"}, {9, ""}} {
		result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: test.line, Character: 9}}})
		if err != nil {
			t.Fatal(err)
		}
		items := completionItems(t, result)
		if test.want == "" && len(items) != 0 {
			t.Fatalf("unknown: %#v", items)
		}
		if test.want != "" && !hasCompletionLabel(items, test.want) {
			t.Fatalf("%s: %#v", test.want, items)
		}
	}
}

func TestCompletionStaticMemberDeepIsBounded(t *testing.T) {
	source := "vim9script\nclass Inner\n  var first: number\n  var second: number\n  var third: number\n  var fourth: number\nendclass\nclass Outer\n  var child: Inner\nendclass\nvar obj: Outer = Outer.new()\necho obj.\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 11, Character: 9}}})
	if err != nil {
		t.Fatal(err)
	}
	list := result.(*protocol.CompletionList)
	var deep []string
	for _, item := range list.Items {
		if strings.Contains(item.Label, ".") {
			deep = append(deep, item.Label)
		}
	}
	if len(deep) != 3 || !list.IsIncomplete {
		t.Fatalf("source %q deep=%#v list=%#v", source, deep, list)
	}
}

func TestCompletionRelativeImportPathEditAndKinds(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "lib.vim", "vim9script\n")
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := "vim9script\nimport './' as local\n"
	main := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := uri.File(main)
	instance.documents.Open(documentURI.String(), 1, source)
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 10}}})
	if err != nil {
		t.Fatal(err)
	}
	items := completionItems(t, result)
	want := map[string]protocol.CompletionItemKind{"./lib.vim": protocol.CompletionItemKindFile, "./nested/": protocol.CompletionItemKindFolder}
	for _, item := range items {
		kind, ok := want[item.Label]
		if !ok {
			continue
		}
		edit, editOK := item.TextEdit.(*protocol.TextEdit)
		if item.Kind != kind || !editOK || edit.Range != navigationRange(1, 8, 10) {
			t.Fatalf("source %q item %#v", source, item)
		}
		delete(want, item.Label)
	}
	if len(want) != 0 {
		t.Fatalf("source %q missing %#v in %#v", source, want, items)
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

func completionItems(t *testing.T, result protocol.CompletionResult) protocol.CompletionItemSlice {
	t.Helper()
	list, ok := result.(*protocol.CompletionList)
	if !ok {
		t.Fatalf("completion result = %T, want *protocol.CompletionList", result)
	}
	return protocol.CompletionItemSlice(list.Items)
}

func TestCompletionSelectionAndProtocolItems(t *testing.T) {
	selectionCases := []struct {
		source     string
		cursor     int
		start, end int
		prefix     string
	}{
		{source: "echo absTail", cursor: 8, start: 5, end: 12, prefix: "abs"},
		{source: "echo \U0001f4a9nameTail", cursor: len("echo \U0001f4a9name"), start: len("echo \U0001f4a9"), end: len("echo \U0001f4a9nameTail"), prefix: "name"},
	}
	for _, test := range selectionCases {
		selection := completionSelectionAt(test.source, test.cursor)
		if selection.start != test.start || selection.end != test.end || selection.prefix != test.prefix {
			t.Fatalf("selection(%q, %d) = %#v", test.source, test.cursor, selection)
		}
	}

	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\necho absc\n")
	enabled := true
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: protocol.ClientCapabilities{TextDocument: &protocol.TextDocumentClientCapabilities{Completion: &protocol.CompletionClientCapabilities{CompletionItem: &protocol.ClientCompletionItemOptions{InsertReplaceSupport: &enabled, PreselectSupport: &enabled}}}}}); err != nil {
		t.Fatal(err)
	}
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 8}}})
	if err != nil {
		t.Fatal(err)
	}
	items := completionItems(t, result)
	if len(items) == 0 {
		t.Fatalf("completion items = %#v", items)
	}
	preselected, preselectOK := items[0].Preselect.Get()
	_, sortOK := items[0].SortText.Get()
	_, filterOK := items[0].FilterText.Get()
	if !preselectOK || !preselected || !sortOK || !filterOK {
		t.Fatalf("completion items = %#v", items)
	}
	for _, item := range items {
		if item.Label != "abs" {
			continue
		}
		edit, ok := item.TextEdit.(*protocol.InsertReplaceEdit)
		if !ok || edit.Insert != navigationRange(1, 5, 8) || edit.Replace != navigationRange(1, 5, 9) {
			t.Fatalf("abs text edit = %#v", item.TextEdit)
		}
		return
	}
	t.Fatalf("abs completion missing: %#v", items)
}

func hasCompletionLabel(items protocol.CompletionItemSlice, label string) bool {
	for _, item := range items {
		if item.Label == label {
			return true
		}
	}
	return false
}
