package server

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestSemanticTokenLifecycleNotificationsDoNotInstallResults(t *testing.T) {
	instance := New(nil, nil, nil)
	t.Cleanup(instance.stopAnalysis)
	documentURI := uri.MustParse("file:///semantic-lifecycle.vim")
	assertNoResult := func() {
		t.Helper()
		instance.publishMu.Lock()
		_, installed := instance.semanticTokenResults[documentURI.String()]
		instance.publishMu.Unlock()
		if installed {
			t.Fatal("lifecycle notification installed semantic token result")
		}
	}

	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: "echo opened\n",
	}}); err != nil {
		t.Fatal(err)
	}
	assertNoResult()
	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: "echo changed\n"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	assertNoResult()
	saved := "echo saved\n"
	if err := instance.DidSave(context.Background(), &protocol.DidSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Text: &saved,
	}); err != nil {
		t.Fatal(err)
	}
	assertNoResult()
}

func TestSemanticTokensFullDeltaCachesLatestResultAndAppliesEdits(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar value = 1\necho value\n")
	first, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil || first.ResultID == nil || *first.ResultID == "" {
		t.Fatalf("first full result = %#v, error = %v", first, err)
	}
	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: "vim9script\nvar changed = 1\necho changed\n"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	instance.publishMu.Lock()
	cached, ok := instance.semanticTokenResults[documentURI.String()]
	instance.publishMu.Unlock()
	if !ok || cached.resultID != *first.ResultID || !reflect.DeepEqual(cached.data, first.Data) {
		t.Fatalf("change installed semantic token result = %#v, want result ID %q and initial data %#v", cached, *first.ResultID, first.Data)
	}
	deltaResult, err := instance.SemanticTokensFullDelta(context.Background(), &protocol.SemanticTokensDeltaParams{
		TextDocument:     protocol.TextDocumentIdentifier{URI: documentURI},
		PreviousResultID: *first.ResultID,
	})
	if err != nil {
		t.Fatal(err)
	}
	delta, ok := deltaResult.(*protocol.SemanticTokensDelta)
	if !ok || delta.ResultID == nil || *delta.ResultID == *first.ResultID || len(delta.Edits) != 1 {
		t.Fatalf("delta result = %#v", deltaResult)
	}
	current, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	if got := applySemanticTokenEdits(t, first.Data, delta.Edits); !reflect.DeepEqual(got, current.Data) {
		t.Fatalf("applied delta = %#v, current = %#v", got, current.Data)
	}

	equalResult, err := instance.SemanticTokensFullDelta(context.Background(), &protocol.SemanticTokensDeltaParams{
		TextDocument:     protocol.TextDocumentIdentifier{URI: documentURI},
		PreviousResultID: *current.ResultID,
	})
	if err != nil {
		t.Fatal(err)
	}
	equal, ok := equalResult.(*protocol.SemanticTokensDelta)
	if !ok || equal.ResultID == nil || len(equal.Edits) != 0 || equal.Edits == nil {
		t.Fatalf("equal delta result = %#v", equalResult)
	}
}

func TestSemanticTokensFullDeltaFallsBackForNonLatestAndLifecycleResults(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "echo 1\n")
	params := &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}}
	first, err := instance.SemanticTokensFull(context.Background(), params)
	if err != nil || first.ResultID == nil {
		t.Fatalf("first result = %#v, error = %v", first, err)
	}
	second, err := instance.SemanticTokensFull(context.Background(), params)
	if err != nil || second.ResultID == nil {
		t.Fatalf("second result = %#v, error = %v", second, err)
	}
	fallback := func(document uri.URI, previous string) *protocol.SemanticTokens {
		t.Helper()
		result, err := instance.SemanticTokensFullDelta(context.Background(), &protocol.SemanticTokensDeltaParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: document}, PreviousResultID: previous,
		})
		if err != nil {
			t.Fatal(err)
		}
		full, ok := result.(*protocol.SemanticTokens)
		if !ok || full.ResultID == nil {
			t.Fatalf("fallback result = %#v", result)
		}
		return full
	}
	fallback(documentURI, *first.ResultID) // Replaced by a concurrent full result.
	fallback(documentURI, "unknown")

	otherURI := uri.MustParse("file:///other.vim")
	instance.documents.Open(otherURI.String(), 1, "echo 2\n")
	fallback(otherURI, *second.ResultID) // A valid ID for another URI.
	if err := instance.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}}); err != nil {
		t.Fatal(err)
	}
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 3, Text: "echo 3\n"}}); err != nil {
		t.Fatal(err)
	}
	fallback(documentURI, *second.ResultID) // Close/reopen evicted the old lifecycle result.
}

func applySemanticTokenEdits(t *testing.T, data []uint32, edits []protocol.SemanticTokensEdit) []uint32 {
	t.Helper()
	result := append([]uint32(nil), data...)
	for _, edit := range edits {
		start := int(edit.Start)
		end := start + int(edit.DeleteCount)
		if start < 0 || end < start || end > len(result) {
			t.Fatalf("invalid edit %#v for %#v", edit, result)
		}
		next := make([]uint32, 0, len(result)-int(edit.DeleteCount)+len(edit.Data))
		next = append(next, result[:start]...)
		next = append(next, edit.Data...)
		next = append(next, result[end:]...)
		result = next
	}
	return result
}

func TestSemanticTokensFullReusesParserCacheForSameSnapshot(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar value = 1\necho value\n")
	cacheMisses := 0
	instance.testHooks.beforeParseSnapshotCacheMiss = func(*text.Snapshot) {
		cacheMisses++
	}
	t.Cleanup(func() {
		instance.testHooks.beforeParseSnapshotCacheMiss = nil
	})
	params := &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}}
	for range 2 {
		if _, err := instance.SemanticTokensFull(context.Background(), params); err != nil {
			t.Fatal(err)
		}
	}
	if cacheMisses != 1 {
		t.Fatalf("parser cache misses = %d, want 1", cacheMisses)
	}
}

func TestSemanticTokensFullDoesNotInstallStaleSnapshot(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "echo before\n")
	started := make(chan struct{})
	continueParse := make(chan struct{})
	instance.testHooks.beforeParseSnapshotCacheMiss = func(*text.Snapshot) {
		close(started)
		<-continueParse
	}
	result := make(chan error, 1)
	go func() {
		_, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
		result <- err
	}()
	<-started
	instance.publishMu.Lock()
	_, _, err := instance.documents.Change(documentURI.String(), 2, text.UTF16, []text.Change{{Text: "echo after\n"}})
	instance.publishMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	close(continueParse)
	if err := <-result; !errors.Is(err, protocol.ErrContentModified) {
		t.Fatalf("stale full error = %v", err)
	}
	instance.publishMu.Lock()
	_, cached := instance.semanticTokenResults[documentURI.String()]
	instance.publishMu.Unlock()
	if cached {
		t.Fatal("stale semantic token result was cached")
	}
}

func TestSemanticTokensFullClassifiesSyntaxAndBoundSymbols(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nconst value = 1\n# comment\necho value\n")
	tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint32{
		0, 0, 10, 1, 16,
		1, 0, 5, 1, 16,
		0, 6, 5, 3, 3,
		1, 0, 9, 0, 0,
		1, 0, 4, 1, 16,
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

func TestSemanticTokensAndInlayHintReturnDocumentResultsDuringWorkspaceIndexWork(t *testing.T) {
	for _, name := range []string{"rebuild", "pending"} {
		t.Run(name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar value = 1\n")
			t.Cleanup(instance.stopAnalysis)
			instance.workspaceMu.Lock()
			if name == "rebuild" {
				instance.workspaceRunning = true
			} else {
				instance.workspacePending["file.vim"] = struct{}{}
			}
			instance.workspaceMu.Unlock()
			textDocument := protocol.TextDocumentIdentifier{URI: documentURI}
			tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: textDocument})
			if err != nil || tokens == nil || len(tokens.Data) == 0 {
				t.Fatalf("semantic tokens = %#v, error = %v", tokens, err)
			}
			rangeTokens, err := instance.SemanticTokensRange(context.Background(), &protocol.SemanticTokensRangeParams{
				TextDocument: textDocument,
				Range:        protocol.Range{Start: protocol.Position{}, End: protocol.Position{Line: 1, Character: 13}},
			})
			if err != nil || rangeTokens == nil || len(rangeTokens.Data) == 0 {
				t.Fatalf("range semantic tokens = %#v, error = %v", rangeTokens, err)
			}
			delta, err := instance.SemanticTokensFullDelta(context.Background(), &protocol.SemanticTokensDeltaParams{
				TextDocument:     textDocument,
				PreviousResultID: *tokens.ResultID,
			})
			if err != nil || delta == nil {
				t.Fatalf("delta semantic tokens = %#v, error = %v", delta, err)
			}
			hints, err := instance.InlayHint(context.Background(), &protocol.InlayHintParams{
				TextDocument: textDocument,
				Range:        protocol.Range{Start: protocol.Position{}, End: protocol.Position{Line: 1, Character: 13}},
			})
			if err != nil || len(hints) != 1 {
				t.Fatalf("inlay hints = %#v, error = %v", hints, err)
			}
		})
	}
}

func TestSemanticTokensClassifyLegacyNamesAndPinnedBuiltins(t *testing.T) {
	source := "let g:value = &ignorecase\necho g:value @a $HOME v:version len([])\ncommand! Build echo 1\nBuild\nfunction! s:Run(arg)\n  echo a:arg\n  call <SID>Run(1)\nendfunction\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	assertSemanticToken(t, tokens.Data, 0, 4, semanticNamespace, 0)
	assertSemanticToken(t, tokens.Data, 0, 6, semanticVariable, semanticDeclaration)
	assertSemanticToken(t, tokens.Data, 0, 14, semanticVariable, semanticDefaultLibrary)
	assertSemanticToken(t, tokens.Data, 1, 13, semanticVariable, 0)
	assertSemanticToken(t, tokens.Data, 1, 16, semanticVariable, 0)
	assertSemanticToken(t, tokens.Data, 1, 22, semanticNamespace, 0)
	assertSemanticTokenHasModifiers(t, tokens.Data, 1, 24, semanticVariable, semanticDefaultLibrary)
	assertSemanticToken(t, tokens.Data, 1, 32, semanticFunction, semanticDefaultLibrary)
	assertSemanticToken(t, tokens.Data, 2, 9, semanticKeyword, semanticDeclaration)
	assertSemanticToken(t, tokens.Data, 3, 0, semanticKeyword, 0)
	assertSemanticToken(t, tokens.Data, 4, 10, semanticNamespace, 0)
	assertSemanticToken(t, tokens.Data, 4, 12, semanticFunction, semanticDeclaration|semanticReadonly)
	assertSemanticToken(t, tokens.Data, 4, 16, semanticParameter, semanticDeclaration)
	assertSemanticToken(t, tokens.Data, 5, 7, semanticNamespace, 0)
	assertSemanticToken(t, tokens.Data, 5, 9, semanticParameter, 0)
	assertSemanticToken(t, tokens.Data, 6, 7, semanticNamespace, 0)
	assertSemanticToken(t, tokens.Data, 6, 12, semanticFunction, semanticReadonly)
}

func TestSemanticTokensDistinguishLegacyUserCommandFromVim9FunctionCall(t *testing.T) {
	for _, test := range []struct {
		name      string
		source    string
		line      uint32
		tokenType uint32
	}{
		{name: "legacy user command", source: "function! s:AddStrict() abort\n  CocRestart\nendfunction\n", line: 1, tokenType: semanticKeyword},
		{name: "Vim9 function call", source: "vim9script\ndef AddStrict(): void\n  CocRestart()\nenddef\n", line: 2, tokenType: semanticFunction},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
			if err != nil {
				t.Fatal(err)
			}
			assertSemanticToken(t, tokens.Data, test.line, 2, test.tokenType, 0)
		})
	}
}

func TestSemanticTokensClassifyMappingCommandBody(t *testing.T) {
	prefix := "vnoremap <silent> <Plug>(coc-range-select) :<C-u>"
	source := prefix + "call CocActionAsync('rangeSelect', visualmode(), v:true)<CR>\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	commandStart := uint32(len(prefix))
	functionStart := commandStart + uint32(len("call "))
	visualmodeStart := functionStart + uint32(len("CocActionAsync('rangeSelect', "))
	vimVariableStart := visualmodeStart + uint32(len("visualmode(), "))
	assertSemanticToken(t, tokens.Data, 0, commandStart, semanticKeyword, semanticDefaultLibrary)
	assertSemanticToken(t, tokens.Data, 0, functionStart, semanticFunction, 0)
	assertSemanticToken(t, tokens.Data, 0, visualmodeStart, semanticFunction, semanticDefaultLibrary)
	assertSemanticToken(t, tokens.Data, 0, vimVariableStart, semanticNamespace, 0)
	assertSemanticTokenHasModifiers(t, tokens.Data, 0, vimVariableStart+2, semanticVariable, semanticDefaultLibrary)
}

func TestSemanticTokensClassifyBuiltinMethodCallAsFunction(t *testing.T) {
	source := "vim9script\necho values({})->flattennew(1)\n"
	file := syntax.Parse(source)
	span := syntax.Span{Start: len("vim9script\necho values({})->"), End: len("vim9script\necho values({})->flattennew")}
	count := 0
	for _, fact := range collectSemanticFacts(file, nil) {
		if fact.span == span {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("flattennew semantic fact count = %d, want 1", count)
	}
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	assertSemanticToken(t, tokens.Data, 1, uint32(len("echo values({})->")), semanticFunction, semanticDefaultLibrary)
}

func TestSemanticTokensClassifyForInAsKeyword(t *testing.T) {
	source := "vim9script\nexport def Tabpage_is_valid(tid: number): bool\n  for nr in range(1, tabpagenr('$'))\n    if gettabvar(nr, '__coc_tid', -1) == tid\n      return true\n    endif\n  endfor\n  return false\nenddef\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	assertSemanticToken(t, tokens.Data, 2, uint32(len("  for nr ")), semanticKeyword, semanticDefaultLibrary)
}

func TestSemanticTokensClassifyFunctionAttributesAsModifiers(t *testing.T) {
	source := "function! Outer()\n  function! Inner() range abort dict closure\n  endfunction\nendfunction\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	character := uint32(len("  function! Inner() "))
	for _, attribute := range []string{"range", "abort", "dict", "closure"} {
		assertSemanticToken(t, tokens.Data, 1, character, semanticModifier, 0)
		character += uint32(len(attribute) + 1)
	}
}

func TestSemanticTokensClassifyVim9DeclarationsAndProvenModifiers(t *testing.T) {
	source := "vim9script\nimport './mod.vim' as mod\nclass Thing\n  static final Value = 1\n  static def Build(arg: number)\n    echo arg\n  enddef\nendclass\n# @deprecated\nconst old = 1\necho old\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	assertSemanticToken(t, tokens.Data, 1, 22, semanticNamespace, semanticDeclaration|semanticReadonly)
	assertSemanticToken(t, tokens.Data, 2, 6, semanticClass, semanticDeclaration|semanticReadonly)
	assertSemanticToken(t, tokens.Data, 3, 15, semanticProperty, semanticDeclaration|semanticReadonly|semanticStatic)
	assertSemanticToken(t, tokens.Data, 4, 13, semanticMethod, semanticDeclaration|semanticReadonly|semanticStatic)
	assertSemanticToken(t, tokens.Data, 4, 19, semanticParameter, semanticDeclaration)
	assertSemanticToken(t, tokens.Data, 5, 9, semanticParameter, 0)
	assertSemanticToken(t, tokens.Data, 9, 6, semanticVariable, semanticDeclaration|semanticReadonly|semanticDeprecated)
	assertSemanticToken(t, tokens.Data, 10, 5, semanticVariable, semanticReadonly|semanticDeprecated)
}

func TestSemanticTokensClassifyVim9TypesEnumsAndMembers(t *testing.T) {
	source := "vim9script\ninterface Shape\n  def Draw()\nendinterface\nenum Color\n  Red\nendenum\ntype Alias = number\nvar shape: Shape\nshape.Draw()\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	assertSemanticToken(t, tokens.Data, 1, 10, semanticInterface, semanticDeclaration|semanticReadonly)
	assertSemanticToken(t, tokens.Data, 2, 6, semanticMethod, semanticDeclaration|semanticReadonly)
	assertSemanticToken(t, tokens.Data, 4, 5, semanticEnum, semanticDeclaration|semanticReadonly)
	assertSemanticToken(t, tokens.Data, 5, 2, semanticEnumMember, semanticDeclaration|semanticReadonly)
	assertSemanticToken(t, tokens.Data, 7, 5, semanticTypeName, semanticDeclaration|semanticReadonly)
	assertSemanticToken(t, tokens.Data, 8, 11, semanticTypeName, 0)
	assertSemanticToken(t, tokens.Data, 9, 6, semanticMethod, 0)
}

func TestSemanticTokensClassifyImportedTypesAndMembers(t *testing.T) {
	source := "vim9script\nimport './m.vim' as mod\nvar item: mod.Item\nmod.Build()\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	assertSemanticToken(t, tokens.Data, 1, 20, semanticNamespace, semanticDeclaration|semanticReadonly)
	assertSemanticToken(t, tokens.Data, 2, 10, semanticNamespace, 0)
	assertSemanticToken(t, tokens.Data, 2, 14, semanticTypeName, 0)
	assertSemanticToken(t, tokens.Data, 3, 0, semanticNamespace, semanticReadonly)
	assertSemanticToken(t, tokens.Data, 3, 4, semanticMethod, 0)
}

func assertSemanticToken(t *testing.T, data []uint32, wantLine, wantCharacter, wantType, wantModifiers uint32) {
	t.Helper()
	tokenType, modifiers, ok := semanticTokenAt(data, wantLine, wantCharacter)
	if !ok || tokenType != wantType || modifiers != wantModifiers {
		t.Fatalf("token at %d:%d = type %d modifiers %d found %t; data = %#v", wantLine, wantCharacter, tokenType, modifiers, ok, data)
	}
}

func assertSemanticTokenHasModifiers(t *testing.T, data []uint32, wantLine, wantCharacter, wantType, wantModifiers uint32) {
	t.Helper()
	tokenType, modifiers, ok := semanticTokenAt(data, wantLine, wantCharacter)
	if !ok || tokenType != wantType || modifiers&wantModifiers != wantModifiers {
		t.Fatalf("token at %d:%d = type %d modifiers %d found %t; data = %#v", wantLine, wantCharacter, tokenType, modifiers, ok, data)
	}
}

func semanticTokenAt(data []uint32, wantLine, wantCharacter uint32) (uint32, uint32, bool) {
	line, character := uint32(0), uint32(0)
	for index := 0; index+4 < len(data); index += 5 {
		line += data[index]
		if data[index] == 0 {
			character += data[index+1]
		} else {
			character = data[index+1]
		}
		if line == wantLine && character == wantCharacter {
			return data[index+3], data[index+4], true
		}
	}
	return 0, 0, false
}

func TestCodeActionInsertsKnownMissingEnd(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nif true\n  echo 'x'\n")
	diagnostic := protocol.Diagnostic{Range: navigationRange(1, 0, 2), Code: protocol.String("vim/E171")}
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

func TestCodeActionRepairsOnlyUniqueSyntaxBackedEdit(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		code       string
		diagnostic protocol.Range
		title      string
		edit       protocol.Range
		newText    string
		fixed      string
	}{
		{
			name:       "function parameter terminator",
			source:     "vim9script\ndef F(arg: number\nenddef\n",
			code:       "vim/E475",
			diagnostic: navigationRange(1, 5, 17),
			title:      "Insert missing )",
			edit:       navigationRange(1, 17, 17),
			newText:    ")",
			fixed:      "vim9script\ndef F(arg: number)\nenddef\n",
		},
		{
			name:       "method call parentheses",
			source:     "vim9script\nvar x = 123->(Func)\n",
			code:       "vimls/missing-method-call",
			diagnostic: navigationRange(1, 19, 19),
			title:      "Insert missing ()",
			edit:       navigationRange(1, 19, 19),
			newText:    "()",
			fixed:      "vim9script\nvar x = 123->(Func)()\n",
		},
		{
			name:       "compiled call comma",
			source:     "vim9script\ndef F()\n  echo len(1 2)\nenddef\n",
			code:       "vim/E1123",
			diagnostic: navigationRange(2, 13, 14),
			title:      "Insert missing comma",
			edit:       navigationRange(2, 12, 13),
			newText:    ", ",
			fixed:      "vim9script\ndef F()\n  echo len(1, 2)\nenddef\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			diagnostic := protocol.Diagnostic{Range: test.diagnostic, Code: protocol.String(test.code)}
			actions, err := instance.CodeAction(context.Background(), &protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
				Range:        test.diagnostic,
				Context:      protocol.CodeActionContext{Diagnostics: []protocol.Diagnostic{diagnostic}},
			})
			if err != nil || len(actions) != 1 {
				t.Fatalf("actions = %#v, error = %v", actions, err)
			}
			action := actions[0].(*protocol.CodeAction)
			documentEdit := action.Edit.DocumentChanges[0].(*protocol.TextDocumentEdit)
			textEdit := documentEdit.Edits[0].(*protocol.TextEdit)
			if action.Title != test.title || textEdit.Range != test.edit || textEdit.NewText != test.newText {
				t.Fatalf("action = %#v, edit = %#v", action, textEdit)
			}
			if parsed := syntax.Parse(test.fixed); len(parsed.Diagnostics) != 0 {
				t.Fatalf("fixed source diagnostics = %#v", parsed.Diagnostics)
			}
		})
	}
}

func TestCodeActionRepairsStyleDiagnostics(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		code        string
		diagnostic  protocol.Range
		wantTitles  []string
		wantNewText []string
	}{
		{
			name:        "normal bang",
			source:      "normal gg\n",
			code:        "vimls/normal-without-bang",
			diagnostic:  navigationRange(0, 0, 6),
			wantTitles:  []string{"Use :normal!"},
			wantNewText: []string{"!"},
		},
		{
			name:        "command bang",
			source:      "command MyCmd echo 1\n",
			code:        "vim/E174",
			diagnostic:  navigationRange(0, 8, 13),
			wantTitles:  []string{"Use :command!"},
			wantNewText: []string{"!"},
		},
		{
			name:        "embedded command bang",
			source:      "command! Outer command Inner echo 1\n",
			code:        "vim/E174",
			diagnostic:  navigationRange(0, 23, 28),
			wantTitles:  []string{"Use :command!"},
			wantNewText: []string{"!"},
		},
		{
			name:        "function bang",
			source:      "function MyFn()\nendfunction\n",
			code:        "vim/E122",
			diagnostic:  navigationRange(0, 9, 13),
			wantTitles:  []string{"Use :function!"},
			wantNewText: []string{"!"},
		},
		{
			name:        "embedded function bang",
			source:      "autocmd VimEnter * {\n  legacy function s:Run()\n  legacy endfunction\n}\n",
			code:        "vim/E122",
			diagnostic:  navigationRange(1, 18, 23),
			wantTitles:  []string{"Use :function!"},
			wantNewText: []string{"!"},
		},
		{
			name:        "function abort",
			source:      "function! s:Run()\nendfunction\n",
			code:        "vimls/function-without-abort",
			diagnostic:  navigationRange(0, 0, 8),
			wantTitles:  []string{"Add abort"},
			wantNewText: []string{" abort"},
		},
		{
			name:        "embedded normal bang",
			source:      "command! Outer normal gg\n",
			code:        "vimls/normal-without-bang",
			diagnostic:  navigationRange(0, 15, 21),
			wantTitles:  []string{"Use :normal!"},
			wantNewText: []string{"!"},
		},
		{
			name:        "embedded function abort",
			source:      "autocmd VimEnter * {\n  legacy function! s:Run()\n  legacy endfunction\n}\n",
			code:        "vimls/function-without-abort",
			diagnostic:  navigationRange(1, 9, 17),
			wantTitles:  []string{"Add abort"},
			wantNewText: []string{" abort"},
		},
		{
			name:        "explicit string case",
			source:      "let name = 'Foo'\nif name == 'foo'\nendif\n",
			code:        "vimls/implicit-string-case",
			diagnostic:  navigationRange(1, 8, 10),
			wantTitles:  []string{"Use case-sensitive comparison", "Use case-insensitive comparison"},
			wantNewText: []string{"==#", "==?"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			diagnostic := protocol.Diagnostic{Range: test.diagnostic, Code: protocol.String(test.code)}
			actions, err := instance.CodeAction(context.Background(), &protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
				Range:        test.diagnostic,
				Context:      protocol.CodeActionContext{Diagnostics: []protocol.Diagnostic{diagnostic}, Only: []protocol.CodeActionKind{protocol.CodeActionKindQuickFix}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(actions) != len(test.wantTitles) {
				t.Fatalf("actions = %#v", actions)
			}
			for index, item := range actions {
				action, ok := item.(*protocol.CodeAction)
				if !ok || action.Edit == nil || action.Title != test.wantTitles[index] || len(action.Edit.DocumentChanges) != 1 {
					t.Fatalf("action[%d] = %#v", index, item)
				}
				documentEdit := action.Edit.DocumentChanges[0].(*protocol.TextDocumentEdit)
				textEdit := documentEdit.Edits[0].(*protocol.TextEdit)
				if textEdit.NewText != test.wantNewText[index] {
					t.Fatalf("action[%d] text edit = %#v", index, textEdit)
				}
			}
		})
	}
}

// TestConfigOnlyDiagnosticsDoNotOfferQuickFixes proves the §8 safety boundary
// using the real config-file role and diagnostics published by the server.
// These messages intentionally stay explanatory: their apparent repairs would
// change reload or mapping semantics.
func TestConfigOnlyDiagnosticsDoNotOfferQuickFixes(t *testing.T) {
	root := mustWorkspaceCanonicalPath(t, t.TempDir())
	path := filepath.Join(root, ".vimrc")
	source := "augroup config_test\n  autocmd BufRead * echo 'once'\naugroup END\nmap <Leader>a :echo 1<CR>\nlet g:mapleader = ','\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	instance := newRootedServer(t, root)
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
	instance.client = client
	documentURI := uri.File(path)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source}}); err != nil {
		t.Fatal(err)
	}
	params := waitForDiagnostics(t, client.published)
	for _, code := range []protocol.String{
		"vimls/autocmd-group-not-cleared",
		"vimls/config-mapleader-order",
	} {
		var diagnostic *protocol.Diagnostic
		for index := range params.Diagnostics {
			if params.Diagnostics[index].Code == code {
				diagnostic = &params.Diagnostics[index]
				break
			}
		}
		if diagnostic == nil {
			t.Fatalf("missing %s in %#v", code, params.Diagnostics)
		}
		actions, err := instance.CodeAction(context.Background(), &protocol.CodeActionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Range:        diagnostic.Range,
			Context: protocol.CodeActionContext{
				Diagnostics: []protocol.Diagnostic{*diagnostic},
				Only:        []protocol.CodeActionKind{protocol.CodeActionKindQuickFix},
			},
		})
		if err != nil || len(actions) != 0 {
			t.Fatalf("%s actions = %#v, error = %v", code, actions, err)
		}
	}
}

// TestConfigFileQuickFixRejectsAStalePublishedDiagnostic proves that the
// existing stale-range protocol guard also applies when the document has the
// config role.  Config-only diagnostics above are deliberately not eligible
// for a fix, so their stale state cannot be used to manufacture an action.
func TestConfigFileQuickFixRejectsAStalePublishedDiagnostic(t *testing.T) {
	root := mustWorkspaceCanonicalPath(t, t.TempDir())
	path := filepath.Join(root, ".vimrc")
	source := "normal gg\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	instance := newRootedServer(t, root)
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
	instance.client = client
	documentURI := uri.File(path)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source}}); err != nil {
		t.Fatal(err)
	}
	params := waitForDiagnostics(t, client.published)
	var stale *protocol.Diagnostic
	for index := range params.Diagnostics {
		if params.Diagnostics[index].Code == protocol.String("vimls/normal-without-bang") {
			stale = &params.Diagnostics[index]
			break
		}
	}
	if stale == nil {
		t.Fatalf("missing normal-without-bang in %#v", params.Diagnostics)
	}
	if _, _, err := instance.documents.Change(documentURI.String(), 2, text.UTF16, []text.Change{{Text: "normal! gg\n"}}); err != nil {
		t.Fatal(err)
	}
	actions, err := instance.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Range:        stale.Range,
		Context: protocol.CodeActionContext{
			Diagnostics: []protocol.Diagnostic{*stale},
			Only:        []protocol.CodeActionKind{protocol.CodeActionKindQuickFix},
		},
	})
	if !errors.Is(err, protocol.ErrContentModified) || len(actions) != 0 {
		t.Fatalf("stale config quick fix = %#v, error = %v", actions, err)
	}
}

func TestCodeActionRejectsStaleDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, code string
		diagnostic         protocol.Range
	}{
		{
			name:       "syntax diagnostic",
			source:     "echo 1\n",
			code:       "vim/E171",
			diagnostic: navigationRange(0, 0, 4),
		},
		{
			name:       "style diagnostic",
			source:     "if 1 == 2\nendif\n",
			code:       "vimls/implicit-string-case",
			diagnostic: navigationRange(0, 5, 7),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			_, err := instance.CodeAction(context.Background(), &protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
				Range:        test.diagnostic,
				Context: protocol.CodeActionContext{Diagnostics: []protocol.Diagnostic{{
					Range: test.diagnostic,
					Code:  protocol.String(test.code),
				}}},
			})
			if !errors.Is(err, protocol.ErrContentModified) {
				t.Fatalf("error = %v, want %v", err, protocol.ErrContentModified)
			}
		})
	}
}

func TestCodeActionRejectsMultipleValidEdits(t *testing.T) {
	source := "vim9script\nvar x = 123->(One)\nvar y = 456->(Two)\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	diagnostics := []protocol.Diagnostic{
		{Range: navigationRange(1, 18, 18), Code: protocol.String("vimls/missing-method-call")},
		{Range: navigationRange(2, 18, 18), Code: protocol.String("vimls/missing-method-call")},
	}
	actions, err := instance.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Range:        protocol.Range{Start: protocol.Position{Line: 1}, End: protocol.Position{Line: 3}},
		Context:      protocol.CodeActionContext{Diagnostics: diagnostics},
	})
	if err != nil || len(actions) != 0 {
		t.Fatalf("actions = %#v, error = %v", actions, err)
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
			diagnostic := protocol.Diagnostic{Range: navigationRange(1, 0, 2), Code: protocol.String("vim/E171")}
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

func decodeSemanticTokens(data []uint32) [][5]uint32 {
	tokens := make([][5]uint32, 0, len(data)/5)
	line, character := uint32(0), uint32(0)
	for index := 0; index+4 < len(data); index += 5 {
		if data[index] != 0 {
			line += data[index]
			character = data[index+1]
		} else {
			character += data[index+1]
		}
		tokens = append(tokens, [5]uint32{line, character, data[index+2], data[index+3], data[index+4]})
	}
	return tokens
}

func TestSemanticTokensRangeCapabilityAndMethod(t *testing.T) {
	instance := New(nil, nil, nil)
	t.Cleanup(instance.stopAnalysis)
	result, err := instance.Initialize(context.Background(), &protocol.InitializeParams{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Capabilities.SemanticTokensProvider == nil {
		t.Fatal("semantic tokens provider missing")
	}
	options, ok := result.Capabilities.SemanticTokensProvider.(*protocol.SemanticTokensOptions)
	if !ok {
		t.Fatalf("semantic tokens provider = %#v", result.Capabilities.SemanticTokensProvider)
	}
	if options.Range == nil {
		t.Fatalf("semantic tokens range = %#v", options.Range)
	}
	encoded, err := protocol.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"semanticTokensProvider":{"legend":`)) || !bytes.Contains(encoded, []byte(`"range":true`)) {
		t.Fatalf("initialize result omitted semantic tokens range: %s", encoded)
	}
	if !implementedMethod(protocol.MethodTextDocumentSemanticTokensRange) {
		t.Fatalf("method %q is not implemented", protocol.MethodTextDocumentSemanticTokensRange)
	}
}

func TestSemanticTokensRangeFiltersByRange(t *testing.T) {
	source := "vim9script\nconst value = 1\n# comment\necho value\necho value\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)

	full, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	all := decodeSemanticTokens(full.Data)
	if len(all) == 0 {
		t.Fatal("full tokens are empty")
	}

	rangeResult, err := instance.SemanticTokensRange(context.Background(), &protocol.SemanticTokensRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Range:        protocol.Range{Start: protocol.Position{Line: 3, Character: 0}, End: protocol.Position{Line: 4, Character: 0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	limited := decodeSemanticTokens(rangeResult.Data)
	if len(limited) == 0 {
		t.Fatalf("range tokens are empty; full = %#v", all)
	}
	for _, token := range limited {
		if token[0] != 3 {
			t.Fatalf("range token on line %d, want line 3 only: %#v", token[0], limited)
		}
	}

	// A range covering the whole document returns the same tokens.
	whole, err := instance.SemanticTokensRange(context.Background(), &protocol.SemanticTokensRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Range:        protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 4, Character: 10}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decodeSemanticTokens(whole.Data), all) {
		t.Fatalf("whole-document range = %#v, want %#v", whole.Data, full.Data)
	}
}

func TestSemanticTokensRangeIncludesOverlappingTokens(t *testing.T) {
	source := "vim9script\nconst value = 1\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	// The range starts inside `const` and ends inside `value`.
	// LSP 3.18 requires returning tokens that overlap the range boundary,
	// so both `const` and `value` must be returned, while `1` is excluded.
	result, err := instance.SemanticTokensRange(context.Background(), &protocol.SemanticTokensRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Range:        protocol.Range{Start: protocol.Position{Line: 1, Character: 3}, End: protocol.Position{Line: 1, Character: 8}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tokens := decodeSemanticTokens(result.Data)
	if len(tokens) != 2 ||
		tokens[0][0] != 1 || tokens[0][1] != 0 || tokens[0][2] != 5 ||
		tokens[1][0] != 1 || tokens[1][1] != 6 || tokens[1][2] != 5 {
		t.Fatalf("range tokens = %#v", tokens)
	}
}

func TestSemanticTokensRangeInvalidRangesReturnEmpty(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nconst value = 1\n")
	for name, valueRange := range map[string]protocol.Range{
		"empty-origin": {Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 0}},
		"inverted":     {Start: protocol.Position{Line: 1, Character: 5}, End: protocol.Position{Line: 1, Character: 2}},
		"empty":        {Start: protocol.Position{Line: 1, Character: 3}, End: protocol.Position{Line: 1, Character: 3}},
		"beyond":       {Start: protocol.Position{Line: 99, Character: 0}, End: protocol.Position{Line: 99, Character: 9}},
	} {
		result, err := instance.SemanticTokensRange(context.Background(), &protocol.SemanticTokensRangeParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Range:        valueRange,
		})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(result.Data) != 0 {
			t.Fatalf("%s: data = %#v, want empty", name, result.Data)
		}
	}
}

func TestSemanticTokensRangeDoesNotTouchDeltaRegistry(t *testing.T) {
	documentURI := uri.MustParse("file:///semantic-range-registry.vim")
	instance := New(nil, nil, nil)
	t.Cleanup(instance.stopAnalysis)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: "vim9script\nconst value = 1\necho value\n",
	}}); err != nil {
		t.Fatal(err)
	}
	result, err := instance.SemanticTokensRange(context.Background(), &protocol.SemanticTokensRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Range:        protocol.Range{Start: protocol.Position{Line: 1, Character: 0}, End: protocol.Position{Line: 2, Character: 0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ResultID != nil {
		t.Fatalf("range result ID = %q, want none", *result.ResultID)
	}
	instance.publishMu.Lock()
	_, installed := instance.semanticTokenResults[documentURI.String()]
	instance.publishMu.Unlock()
	if installed {
		t.Fatal("range request installed a delta registry entry")
	}
}
