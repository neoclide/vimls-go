package server

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/vimdata"
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
		if tokens.Data[index+4]&semanticDeprecated != 0 {
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
	if got, want := capabilities.CompletionProvider.TriggerCharacters, []string{".", ":", "&", "#", "<", "\"", "'"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("completion trigger characters = %#v, want %#v", got, want)
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

func TestAdvertisedCompletionTriggersReturnContextualItems(t *testing.T) {
	tests := []struct {
		name, source, trigger, label string
		line, character              uint32
		kind                         protocol.CompletionItemKind
	}{
		{name: "member", source: "vim9script\nclass Box\n  var value: number\nendclass\nvar box = Box.new()\necho box.\n", trigger: ".", label: "value", line: 5, character: 9, kind: protocol.CompletionItemKindField},
		{name: "command", source: ":\n", trigger: ":", label: "echo", line: 0, character: 1, kind: protocol.CompletionItemKindKeyword},
		{name: "scope", source: "let g:scoped = 1\necho g:\n", trigger: ":", label: "g:scoped", line: 1, character: 7, kind: protocol.CompletionItemKindVariable},
		{name: "option", source: "echo &\n", trigger: "&", label: "&ignorecase", line: 0, character: 6, kind: protocol.CompletionItemKindProperty},
		{name: "autoload", source: "function foo#Run()\nendfunction\necho foo#\n", trigger: "#", label: "foo#Run", line: 2, character: 9, kind: protocol.CompletionItemKindFunction},
		{name: "expand angle", source: "echo expand('<\n", trigger: "<", label: "<cfile>", line: 0, character: 13, kind: protocol.CompletionItemKindEnumMember},
		{name: "has single quote", source: "echo has('\n", trigger: "'", label: "vim9script", line: 0, character: 10, kind: protocol.CompletionItemKindEnumMember},
		{name: "expand double quote", source: "echo expand(\"\n", trigger: "\"", label: "%", line: 0, character: 13, kind: protocol.CompletionItemKindEnumMember},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			trigger := test.trigger
			result, err := instance.Completion(context.Background(), &protocol.CompletionParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: test.line, Character: test.character}},
				Context:                    protocol.CompletionContext{TriggerKind: protocol.CompletionTriggerKindTriggerCharacter, TriggerCharacter: &trigger},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !hasCompletion(completionItems(t, result), test.label, test.kind) {
				t.Fatalf("trigger %q missing %q in %#v", test.trigger, test.label, completionItems(t, result))
			}
		})
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

func TestCompletionReturnsPinnedHasFeaturesAndExpandSpecials(t *testing.T) {
	tests := []struct {
		name, source, label, detail, documentation string
		position                                   protocol.Position
		edit                                       protocol.Range
	}{
		{
			name:          "has feature in incomplete single quoted string",
			source:        "\" 𐐀\r\necho has('gui_\r\n",
			label:         "gui_running",
			detail:        "has() feature",
			documentation: "Whether the Vim GUI is running or starting.",
			position:      protocol.Position{Line: 1, Character: 14},
			edit:          navigationRange(1, 10, 14),
		},
		{
			name:          "expand special preserves modifier",
			source:        "echo expand(\"<cf:p\")\n",
			label:         "<cfile>",
			detail:        "expand() special token",
			documentation: "File name under the cursor.",
			position:      protocol.Position{Line: 0, Character: 16},
			edit:          navigationRange(0, 13, 16),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: test.position,
			}})
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range completionItems(t, result) {
				if item.Label != test.label {
					continue
				}
				detail, ok := item.Detail.Get()
				documentation, documentationOK := item.Documentation.(protocol.String)
				edit, editOK := item.TextEdit.(*protocol.TextEdit)
				if !ok || detail != test.detail || !documentationOK || string(documentation) != test.documentation || !editOK || edit.Range != test.edit || edit.NewText != test.label {
					t.Fatalf("completion item = %#v", item)
				}
				return
			}
			t.Fatalf("completion %q missing from %#v", test.label, completionItems(t, result))
		})
	}
}

func TestPinnedBuiltinStringCompletionIsCompleteAndDeterministic(t *testing.T) {
	tests := []struct {
		source    string
		position  protocol.Position
		itemCount int
	}{
		{source: "echo has('')\n", position: protocol.Position{Line: 0, Character: 10}, itemCount: 222},
		{source: "echo expand('')\n", position: protocol.Position{Line: 0, Character: 13}, itemCount: 16},
	}
	for _, test := range tests {
		instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
		complete := func() *protocol.CompletionList {
			result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: test.position,
			}})
			if err != nil {
				t.Fatal(err)
			}
			list, ok := result.(*protocol.CompletionList)
			if !ok {
				t.Fatalf("completion result = %T", result)
			}
			return list
		}
		first, second := complete(), complete()
		if first.IsIncomplete || len(first.Items) != test.itemCount || !reflect.DeepEqual(first.Items, second.Items) {
			t.Fatalf("completion for %q: first=%d incomplete=%t deterministic=%t", test.source, len(first.Items), first.IsIncomplete, reflect.DeepEqual(first.Items, second.Items))
		}
	}
}

func TestLegacyWorkspaceGlobalVariableCompletionRespectsCallableScope(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "globals.vim", "let g:GlobalValue = 1\nlet BareValue = 2\nfunction GlobalFn()\nendfunction\n")
	source := "echo GlobalV\nfunction Local()\n  echo GlobalV\n  echo g:GlobalV\n  echo g:GlobalF\nendfunction\necho FutureG\nlet g:FutureGlobal = 1\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := uri.File(mainPath)
	instance.documents.Open(documentURI.String(), 1, source)
	complete := func(line, character uint32) protocol.CompletionItemSlice {
		result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: line, Character: character},
		}})
		if err != nil {
			t.Fatal(err)
		}
		return completionItems(t, result)
	}
	if items := complete(0, 12); !hasCompletion(items, "GlobalValue", protocol.CompletionItemKindVariable) {
		t.Fatalf("top-level global variable completion = %#v", items)
	}
	if items := complete(2, 14); hasCompletionLabel(items, "GlobalValue") {
		t.Fatalf("bare global variable leaked into function = %#v", items)
	}
	items := complete(3, 16)
	found := false
	for _, item := range items {
		if item.Label != "g:GlobalValue" {
			continue
		}
		detail, ok := item.Detail.Get()
		edit, editOK := item.TextEdit.(*protocol.TextEdit)
		if !ok || detail != "workspace global variable" || !editOK || edit.Range != navigationRange(3, 7, 16) || edit.NewText != "g:GlobalValue" {
			t.Fatalf("scoped global variable completion = %#v", item)
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("scoped global variable completion missing: %#v", items)
	}
	if items := complete(4, 16); !hasCompletion(items, "g:GlobalFn", protocol.CompletionItemKindFunction) {
		t.Fatalf("scoped global function completion = %#v", items)
	}
	if items := complete(6, 12); hasCompletionLabel(items, "FutureGlobal") {
		t.Fatalf("forward current-file global variable leaked from workspace index = %#v", items)
	}
}

func TestCompletionReturnsRuntimeColorschemeWithPrefixEdit(t *testing.T) {
	root, runtimePath := t.TempDir(), t.TempDir()
	writeWorkspaceFile(t, runtimePath, filepath.Join("colors", "default.vim"), "")
	writeWorkspaceFile(t, runtimePath, filepath.Join("colors", "desert.vim"), "")
	writeWorkspaceFile(t, runtimePath, filepath.Join("colors", "my-dark.vim"), "")
	writeWorkspaceFile(t, runtimePath, filepath.Join("colors", "lists", "default.vim"), "")
	source := "\" 𐐀\r\ncolorscheme my-da\r\n"
	main := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	instance.setRuntimePaths([]string{runtimePath})
	instance.refreshWorkspaceResolver()
	instance.scheduleWorkspaceRebuild()
	instance.workspaceWG.Wait()
	if err := os.Remove(filepath.Join(runtimePath, "colors", "my-dark.vim")); err != nil {
		t.Fatal(err)
	}
	documentURI := uri.File(main)
	instance.documents.Open(documentURI.String(), 1, source)
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 17},
	}})
	if err != nil {
		t.Fatal(err)
	}
	items := completionItems(t, result)
	if len(items) != 1 || items[0].Label != "my-dark" || items[0].Kind != protocol.CompletionItemKindValue {
		t.Fatalf("colorscheme completion = %#v", items)
	}
	edit, ok := items[0].TextEdit.(*protocol.TextEdit)
	if !ok || edit.NewText != "my-dark" || edit.Range != navigationRange(1, 12, 17) {
		t.Fatalf("colorscheme edit = %#v", items[0].TextEdit)
	}
}

func TestRuntimeImportRequestsUseIndexedSourceTable(t *testing.T) {
	root, runtimePath := t.TempDir(), t.TempDir()
	target := writeWorkspaceFile(t, runtimePath, filepath.Join("import", "pkg", "api.vim"), "vim9script\nexport def Run(): void\nenddef\n")
	autoloadTarget := writeWorkspaceFile(t, runtimePath, filepath.Join("autoload", "cached.vim"), "function cached#Run()\nendfunction\n")
	source := "vim9script\nimport 'pkg/' as pending\nimport 'pkg/api.vim' as api\necho api.\n"
	main := writeWorkspaceFile(t, root, "main.vim", source)
	legacySource := "call cached#Run()\n"
	legacy := writeWorkspaceFile(t, root, "legacy.vim", legacySource)
	targetURI := canonicalTestURI(t, target)
	autoloadTargetURI := canonicalTestURI(t, autoloadTarget)
	instance := initializeWorkspaceServer(t, root)
	instance.setRuntimePaths([]string{runtimePath})
	instance.refreshWorkspaceResolver()
	instance.scheduleWorkspaceRebuild()
	instance.workspaceWG.Wait()
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(autoloadTarget); err != nil {
		t.Fatal(err)
	}
	documentURI := uri.File(main)
	instance.documents.Open(documentURI.String(), 1, source)
	pathResult, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 12},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !hasCompletionLabel(completionItems(t, pathResult), "pkg/api.vim") {
		t.Fatalf("indexed import path completion = %#v", completionItems(t, pathResult))
	}
	memberResult, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 3, Character: 9},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !hasCompletionLabel(completionItems(t, memberResult), "Run") {
		t.Fatalf("indexed import member completion = %#v", completionItems(t, memberResult))
	}
	links, err := instance.DocumentLink(context.Background(), &protocol.DocumentLinkParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil || len(links) != 1 || links[0].Target == nil || *links[0].Target != targetURI {
		t.Fatalf("indexed import document links = %#v, %v", links, err)
	}
	legacyURI := uri.File(legacy)
	instance.documents.Open(legacyURI.String(), 1, legacySource)
	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: legacyURI}, Position: protocol.Position{Line: 0, Character: 8},
	}})
	locations, ok := definition.(protocol.LocationSlice)
	if err != nil || !ok || len(locations) != 1 || locations[0].URI != autoloadTargetURI {
		t.Fatalf("indexed autoload definition = %#v, %v", definition, err)
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

func TestSignatureHelpForBuiltinFunction(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		line   uint32
	}{
		{name: "legacy", source: "echo get([], 'x', )\n"},
		{name: "vim9", source: "vim9script\necho get([], 'x', )\n", line: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			line := strings.Split(test.source, "\n")[test.line]
			help, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
				Position:     protocol.Position{Line: test.line, Character: uint32(strings.IndexByte(line, ')'))},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if help == nil || len(help.Signatures) != 1 || help.Signatures[0].Label != "get({list}, {idx} [, {default}]): any" || len(help.Signatures[0].Parameters) != 3 {
				t.Fatalf("signature help = %#v", help)
			}
			if active, ok := help.Signatures[0].ActiveParameter.Get(); !ok || active != 2 {
				t.Fatalf("active parameter = %#v", help.Signatures[0].ActiveParameter)
			}
		})
	}
	printf, ok := vimdata.LookupFunction("printf")
	if !ok {
		t.Fatal("missing printf metadata")
	}
	label, parameters := formatBuiltinFunctionSignature(printf)
	if label != "printf({fmt}, {expr1} ...): string" || len(parameters) != 3 || parameters[2].Label != protocol.String("...") {
		t.Fatalf("variadic signature = %q, %#v", label, parameters)
	}
	source := "vim9script\necho printf('%s%s', 'a', 'b')\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	help, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: uint32(strings.IndexByte(strings.Split(source, "\n")[1], ')'))},
	}})
	if err != nil || help == nil {
		t.Fatalf("variadic signature help = %#v, %v", help, err)
	}
	if active, ok := help.Signatures[0].ActiveParameter.Get(); !ok || active != 2 {
		t.Fatalf("variadic active parameter = %#v", help.Signatures[0].ActiveParameter)
	}
}

func TestHoverAndSignatureHelpRespectDocumentationFormats(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\necho get([], 0)\n")
	capabilities := protocol.ClientCapabilities{TextDocument: &protocol.TextDocumentClientCapabilities{
		Hover: &protocol.HoverClientCapabilities{ContentFormat: []protocol.MarkupKind{protocol.MarkupKindMarkdown}},
		SignatureHelp: &protocol.SignatureHelpClientCapabilities{SignatureInformation: &protocol.ClientSignatureInformationOptions{
			DocumentationFormat: []protocol.MarkupKind{protocol.MarkupKindPlainText},
		}},
	}}
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: capabilities}); err != nil {
		t.Fatal(err)
	}
	hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 7},
	}})
	if err != nil || hover == nil {
		t.Fatalf("hover = %#v, %v", hover, err)
	}
	hoverContent, ok := hover.Contents.(*protocol.MarkupContent)
	if !ok || hoverContent.Kind != protocol.MarkupKindMarkdown || !strings.Contains(hoverContent.Value, "get(") || len(hoverContent.Value) > maxLanguageFeatureDocumentationBytes {
		t.Fatalf("hover content = %#v", hover.Contents)
	}
	help, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 14},
	}})
	if err != nil || help == nil || len(help.Signatures) != 1 {
		t.Fatalf("signature help = %#v, %v", help, err)
	}
	documentation, ok := help.Signatures[0].Documentation.(*protocol.MarkupContent)
	if !ok || documentation.Kind != protocol.MarkupKindPlainText || !strings.Contains(documentation.Value, "get(") || len(documentation.Value) > maxLanguageFeatureDocumentationBytes {
		t.Fatalf("signature documentation = %#v", help.Signatures[0].Documentation)
	}
}

func TestLanguageFeatureDocumentationIsBoundedUTF8(t *testing.T) {
	content := boundedMarkupContent(protocol.MarkupKindMarkdown, strings.Repeat("界", maxLanguageFeatureDocumentationBytes))
	if content.Kind != protocol.MarkupKindMarkdown || len(content.Value) > maxLanguageFeatureDocumentationBytes || !utf8.ValidString(content.Value) || !strings.HasSuffix(content.Value, "…") {
		t.Fatalf("bounded content kind=%q bytes=%d valid=%t suffix=%q", content.Kind, len(content.Value), utf8.ValidString(content.Value), content.Value[len(content.Value)-3:])
	}
}

func TestSignatureHelpUsesShadowingFunctionValue(t *testing.T) {
	source := "vim9script\nvar get = (value) => value\necho get(1)\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	help, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 10},
	}})
	if err != nil || help == nil || help.Signatures[0].Label != "get(?): ?" {
		t.Fatalf("shadowed builtin signature = %#v, %v", help, err)
	}
}

func TestSignatureHelpForFunctionTypedValues(t *testing.T) {
	for _, test := range []struct {
		name, source, wantLabel string
		line, character         uint32
		wantActive              uint32
	}{
		{name: "lambda", source: "vim9script\nvar Callback = (first: number, second: string): number => first\necho Callback(1, 'x')\n", wantLabel: "Callback(number, string): number", line: 2, character: 20, wantActive: 1},
		{name: "explicit any", source: "vim9script\nvar Callback: func(any): any\necho Callback(1)\n", wantLabel: "Callback(any): any", line: 2, character: 15},
		{name: "optional", source: "vim9script\nvar Callback: func(number, ?string): bool\necho Callback(1, 'x')\n", wantLabel: "Callback(number, ?string): bool", line: 2, character: 20, wantActive: 1},
		{name: "variadic", source: "vim9script\nvar Callback: func(number, ...list<any>): bool\necho Callback(1, 2, 3)\n", wantLabel: "Callback(number, ...list<any>): bool", line: 2, character: 21, wantActive: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			help, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: test.line, Character: test.character},
			}})
			if err != nil || help == nil || len(help.Signatures) != 1 || help.Signatures[0].Label != test.wantLabel {
				t.Fatalf("signature help = %#v, %v", help, err)
			}
			if active, ok := help.Signatures[0].ActiveParameter.Get(); !ok || active != test.wantActive {
				t.Fatalf("active parameter = %#v", help.Signatures[0].ActiveParameter)
			}
		})
	}
}

func TestSignatureHelpForLocalClassMethodsAndConstructors(t *testing.T) {
	source := `vim9script
class Base
  def Resize(width: number, height: number = 1): number
    return width * height
  enddef
  static def Build(name: string): Base
    return Base.new()
  enddef
endclass
class Box extends Base
  def new(value: number)
  enddef
  def Check()
    echo this.Resize(2, 3)
    echo super.Resize(2, 3)
  enddef
endclass
class Empty
endclass
class Protected
  def _new(value: number)
  enddef
  def _Resize(value: number)
  enddef
endclass
var box = Box.new(1)
var protected = Protected.new()
echo box.Resize(2, 3)
echo Base.Build('x')
echo Empty.new()
echo box.Build('x')
echo Base.Resize(2, 3)
echo protected._Resize(1)
`
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	for _, test := range []struct {
		name, call, wantLabel string
		wantActive            uint32
	}{
		{name: "constructor", call: "Box.new(1)", wantLabel: "new(value: number)"},
		{name: "inherited object method", call: "box.Resize(2, 3)", wantLabel: "Resize(width: number, height: number = 1): number", wantActive: 1},
		{name: "static method", call: "Base.Build('x')", wantLabel: "Build(name: string): Base"},
		{name: "default constructor", call: "Empty.new()", wantLabel: "new()"},
		{name: "this method", call: "this.Resize(2, 3)", wantLabel: "Resize(width: number, height: number = 1): number", wantActive: 1},
		{name: "super method", call: "super.Resize(2, 3)", wantLabel: "Resize(width: number, height: number = 1): number", wantActive: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			callOffset := strings.Index(source, test.call)
			closing := callOffset + strings.LastIndex(test.call, ")")
			prefix := source[:closing]
			position := protocol.Position{Line: uint32(strings.Count(prefix, "\n")), Character: uint32(len(prefix) - strings.LastIndex(prefix, "\n") - 1)}
			help, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: position,
			}})
			if err != nil || help == nil || len(help.Signatures) != 1 || help.Signatures[0].Label != test.wantLabel {
				t.Fatalf("signature help = %#v, %v", help, err)
			}
			if active, ok := help.Signatures[0].ActiveParameter.Get(); !ok || active != test.wantActive {
				t.Fatalf("active parameter = %#v", help.Signatures[0].ActiveParameter)
			}
		})
	}
	for _, call := range []string{"box.Build('x')", "Base.Resize(2, 3)", "Protected.new()", "protected._Resize(1)"} {
		callOffset := strings.Index(source, call)
		closing := callOffset + strings.LastIndex(call, ")")
		prefix := source[:closing]
		help, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Position:     protocol.Position{Line: uint32(strings.Count(prefix, "\n")), Character: uint32(len(prefix) - strings.LastIndex(prefix, "\n") - 1)},
		}})
		if err != nil || help != nil {
			t.Fatalf("invalid receiver call %q signature help = %#v, %v", call, help, err)
		}
	}
}

func TestSignatureHelpForBuiltinMethodCall(t *testing.T) {
	for _, test := range []struct {
		name       string
		source     string
		line       uint32
		wantLabel  string
		wantParams int
		wantActive uint32
	}{
		{name: "legacy receiver first", source: "echo [1]->add(2)\n", wantLabel: "add({expr})", wantParams: 1},
		{name: "vim9 receiver second", source: "vim9script\necho ['x']->append(1)\n", line: 1, wantLabel: "append({lnum}): number|bool", wantParams: 1},
		{name: "nested optional", source: "vim9script\necho len([]->get(0, 1))\n", line: 1, wantLabel: "get({idx}, [{default}]): any", wantParams: 2, wantActive: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			line := strings.Split(test.source, "\n")[test.line]
			closing := strings.IndexByte(line, ')')
			help, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
				Position:     protocol.Position{Line: test.line, Character: uint32(closing)},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if help == nil || len(help.Signatures) != 1 || help.Signatures[0].Label != test.wantLabel || len(help.Signatures[0].Parameters) != test.wantParams {
				t.Fatalf("signature help = %#v", help)
			}
			if active, ok := help.Signatures[0].ActiveParameter.Get(); !ok || active != test.wantActive {
				t.Fatalf("active parameter = %#v", help.Signatures[0].ActiveParameter)
			}
		})
	}
}

func TestBuiltinMethodSignatureMetadataIncludesReceiver(t *testing.T) {
	for _, function := range vimdata.BuiltinFunctions() {
		if function.MethodArgument == 0 {
			continue
		}
		label, _ := formatBuiltinMethodSignature(function)
		if label == "" {
			t.Fatalf("%s documentation does not identify method receiver %d", function.Name, function.MethodArgument)
		}
	}
	printf, _ := vimdata.LookupFunction("printf")
	label, _ := formatBuiltinMethodSignature(printf)
	if label != "printf({fmt}, ...): string" {
		t.Fatalf("printf method signature = %q", label)
	}
}

func TestSignatureHelpForStaticImportedFunction(t *testing.T) {
	source := "vim9script\nimport './lib.vim' as lib\necho lib.Run()\n"
	instance, documentURI, targetURI := openWorkspaceFeatureRetryDocument(t, source)
	params := &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 13},
	}}
	help, err := instance.SignatureHelp(context.Background(), params)
	if err != nil || help == nil || len(help.Signatures) != 1 || help.Signatures[0].Label != "Run(): number" {
		t.Fatalf("imported signature = %#v, %v", help, err)
	}

	overlay := "vim9script\nexport def Run(value: string, fallback: bool = false): string\n  return value\nenddef\n"
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: targetURI, Version: 2, Text: overlay}}); err != nil {
		t.Fatal(err)
	}
	help, err = instance.SignatureHelp(context.Background(), params)
	if err != nil || help == nil || help.Signatures[0].Label != "Run(value: string, fallback: bool = false): string" {
		t.Fatalf("open imported signature = %#v, %v", help, err)
	}
}

func TestSignatureHelpForStaticImportedAggregateMembers(t *testing.T) {
	source := "vim9script\nimport './lib.vim' as lib\nvar box = lib.Box.new(1)\necho box.Resize(2, 3)\nvar typed: lib.Box\necho typed.Resize(4, 5)\necho lib.Box.new(1).Resize(6, 7)\ntype BoxAlias = lib.Box\nvar aliased: BoxAlias\necho aliased.Resize(8, 9)\ndef Make(): lib.Box\n  return lib.Box.new(1)\nenddef\nvar returned = Make()\necho returned.Resize(10, 11)\nvar copied = typed\necho copied.Resize(12, 13)\nvar boxes: list<lib.Box> = []\necho boxes[0].Resize(14, 15)\necho lib.Make().Resize(16, 17)\nvar assigned: any\nassigned = lib.Box.new(1)\necho assigned.Resize(18, 19)\nvar invalidated: any\ninvalidated = lib.Box.new(1)\ninvalidated = Unknown()\necho invalidated.Resize(20, 21)\nvar conditional: any\nif true\n  conditional = lib.Box.new(1)\nendif\necho conditional.Resize(22, 23)\necho lib.Box.Build('x')\necho lib.Box._Hidden()\n"
	instance, documentURI, targetURI := openWorkspaceFeatureRetryDocument(t, source)
	for _, test := range []struct {
		call, wantLabel string
	}{
		{call: "lib.Box.new(1)", wantLabel: "new(value: number)"},
		{call: "box.Resize(2, 3)", wantLabel: "Resize(width: number, height: number = 1): number"},
		{call: "typed.Resize(4, 5)", wantLabel: "Resize(width: number, height: number = 1): number"},
		{call: "lib.Box.new(1).Resize(6, 7)", wantLabel: "Resize(width: number, height: number = 1): number"},
		{call: "aliased.Resize(8, 9)", wantLabel: "Resize(width: number, height: number = 1): number"},
		{call: "returned.Resize(10, 11)", wantLabel: "Resize(width: number, height: number = 1): number"},
		{call: "copied.Resize(12, 13)", wantLabel: "Resize(width: number, height: number = 1): number"},
		{call: "boxes[0].Resize(14, 15)", wantLabel: "Resize(width: number, height: number = 1): number"},
		{call: "lib.Make().Resize(16, 17)", wantLabel: "Resize(width: number, height: number = 1): number"},
		{call: "assigned.Resize(18, 19)", wantLabel: "Resize(width: number, height: number = 1): number"},
		{call: "lib.Box.Build('x')", wantLabel: "Build(name: string): Box"},
	} {
		callOffset := strings.Index(source, test.call)
		closing := callOffset + strings.LastIndex(test.call, ")")
		prefix := source[:closing]
		help, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Position:     protocol.Position{Line: uint32(strings.Count(prefix, "\n")), Character: uint32(len(prefix) - strings.LastIndex(prefix, "\n") - 1)},
		}})
		if err != nil || help == nil || len(help.Signatures) != 1 || help.Signatures[0].Label != test.wantLabel {
			t.Fatalf("%s signature help = %#v, %v", test.call, help, err)
		}
	}
	hidden := strings.Index(source, "lib.Box._Hidden()") + len("lib.Box._Hidden(")
	hiddenPrefix := source[:hidden]
	help, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: uint32(strings.Count(hiddenPrefix, "\n")), Character: uint32(len(hiddenPrefix) - strings.LastIndex(hiddenPrefix, "\n") - 1)},
	}})
	if err != nil || help != nil {
		t.Fatalf("protected imported signature help = %#v, %v", help, err)
	}
	for _, call := range []string{"invalidated.Resize(20, 21)", "conditional.Resize(22, 23)"} {
		closing := strings.Index(source, call) + strings.LastIndex(call, ")")
		prefix := source[:closing]
		help, err = instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Position:     protocol.Position{Line: uint32(strings.Count(prefix, "\n")), Character: uint32(len(prefix) - strings.LastIndex(prefix, "\n") - 1)},
		}})
		if err != nil || help != nil {
			t.Fatalf("unsafe %s signature help = %#v, %v", call, help, err)
		}
	}

	overlay := "vim9script\nexport class Box\n  def new(name: string)\n  enddef\n  def Resize(label: string): string\n    return label\n  enddef\n  static def Build(value: number): Box\n    return Box.new('x')\n  enddef\nendclass\n"
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: targetURI, Version: 2, Text: overlay}}); err != nil {
		t.Fatal(err)
	}
	callOffset := strings.Index(source, "lib.Box.new(1)")
	closing := callOffset + strings.LastIndex("lib.Box.new(1)", ")")
	prefix := source[:closing]
	help, err = instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: uint32(strings.Count(prefix, "\n")), Character: uint32(len(prefix) - strings.LastIndex(prefix, "\n") - 1)},
	}})
	if err != nil || help == nil || help.Signatures[0].Label != "new(name: string)" {
		t.Fatalf("open imported aggregate signature = %#v, %v", help, err)
	}
	callOffset = strings.Index(source, "typed.Resize(4, 5)")
	closing = callOffset + strings.LastIndex("typed.Resize(4, 5)", ")")
	prefix = source[:closing]
	help, err = instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: uint32(strings.Count(prefix, "\n")), Character: uint32(len(prefix) - strings.LastIndex(prefix, "\n") - 1)},
	}})
	if err != nil || help == nil || help.Signatures[0].Label != "Resize(label: string): string" {
		t.Fatalf("open imported object signature = %#v, %v", help, err)
	}
}

func TestSignatureHelpForStaticImportRetriesWorkspaceIdentity(t *testing.T) {
	source := "vim9script\nimport './lib.vim' as lib\necho lib.Run()\n"
	instance, documentURI, _ := openWorkspaceFeatureRetryDocument(t, source)
	params := &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 13},
	}}
	checks := 0
	instance.beforeWorkspaceIdentityCheck = func() {
		checks++
		if checks == 1 {
			instance.workspaceMu.Lock()
			instance.workspaceRevision++
			instance.workspaceMu.Unlock()
		}
	}
	help, err := instance.SignatureHelp(context.Background(), params)
	if err != nil || help == nil || help.Signatures[0].Label != "Run(): number" || checks != 2 {
		t.Fatalf("imported signature = %#v, checks=%d, error=%v", help, checks, err)
	}

	instance.beforeWorkspaceIdentityCheck = func() {
		checks++
		instance.workspaceMu.Lock()
		instance.workspaceRevision++
		instance.workspaceMu.Unlock()
	}
	checks = 0
	help, err = instance.SignatureHelp(context.Background(), params)
	if !errors.Is(err, protocol.ErrContentModified) || help != nil || checks != 2 {
		t.Fatalf("stale imported signature = %#v, checks=%d, error=%v", help, checks, err)
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
