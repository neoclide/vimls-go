package server

import (
	"context"
	"errors"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
)

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

func TestSemanticTokensAndInlayHintRejectDuringWorkspaceIndexWork(t *testing.T) {
	for _, name := range []string{"rebuild", "pending"} {
		t.Run(name, func(t *testing.T) {
			instance := New(nil, nil, nil)
			t.Cleanup(instance.stopAnalysis)
			instance.workspaceMu.Lock()
			if name == "rebuild" {
				instance.workspaceRunning = true
			} else {
				instance.workspacePending["file.vim"] = struct{}{}
			}
			instance.workspaceMu.Unlock()
			if _, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{}); !errors.Is(err, protocol.ErrContentModified) {
				t.Fatalf("semantic tokens error = %v", err)
			}
			if _, err := instance.InlayHint(context.Background(), &protocol.InlayHintParams{}); !errors.Is(err, protocol.ErrContentModified) {
				t.Fatalf("inlay hints error = %v", err)
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
	assertSemanticToken(t, tokens.Data, 2, 9, semanticFunction, semanticDeclaration)
	assertSemanticToken(t, tokens.Data, 3, 0, semanticFunction, 0)
	assertSemanticToken(t, tokens.Data, 4, 10, semanticNamespace, 0)
	assertSemanticToken(t, tokens.Data, 4, 12, semanticFunction, semanticDeclaration|semanticReadonly)
	assertSemanticToken(t, tokens.Data, 4, 16, semanticParameter, semanticDeclaration)
	assertSemanticToken(t, tokens.Data, 5, 7, semanticNamespace, 0)
	assertSemanticToken(t, tokens.Data, 5, 9, semanticParameter, 0)
	assertSemanticToken(t, tokens.Data, 6, 7, semanticNamespace, 0)
	assertSemanticToken(t, tokens.Data, 6, 12, semanticFunction, semanticReadonly)
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
			name:        "function abort",
			source:      "function! s:Run()\nendfunction\n",
			code:        "vimls/function-without-abort",
			diagnostic:  navigationRange(0, 0, 8),
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
