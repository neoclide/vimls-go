package server

import (
	"context"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
)

func TestMappingEscapesNavigationAndRename(t *testing.T) {
	source := "\ufefffunction! s:Before() abort\r\nendfunction\r\n" +
		"function! s:Target(value) abort\r\n  return a:value\r\nendfunction\r\n" +
		"nnoremap 😀中e\u0301 :<C-U>call s:Before() \\| call s:Target('中😀e\u0301\\|x<Bar>y')\r\n" +
		"      \\ <bAr>call s:Target(42)<CR>\r\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	snapshot := text.NewSnapshot(documentURI.String(), 1, nil, source)
	positionAt := func(offset int) protocol.Position {
		t.Helper()
		position, err := snapshot.Position(offset, text.UTF16)
		if err != nil {
			t.Fatal(err)
		}
		return protocol.Position{Line: uint32(position.Line), Character: uint32(position.Character)}
	}
	var ranges []protocol.Range
	for _, offset := range []int{strings.Index(source, "s:Target"), strings.Index(source, "s:Target('"), strings.LastIndex(source, "s:Target")} {
		ranges = append(ranges, protocol.Range{Start: positionAt(offset), End: positionAt(offset + len("s:Target"))})
	}
	params := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}}
	for _, expected := range ranges[1:] {
		params.Position = expected.Start
		params.Position.Character += 3
		definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: params})
		locations, ok := definition.(protocol.LocationSlice)
		if err != nil || !ok || len(locations) != 1 || locations[0].URI != documentURI || locations[0].Range != ranges[0] {
			t.Fatalf("definition at %v = %#v, error = %v", params.Position, definition, err)
		}
		hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: params})
		if err != nil || hover == nil || hover.Range == nil || *hover.Range != expected {
			t.Fatalf("hover at %v = %#v, error = %v", params.Position, hover, err)
		}
		assertFunctionHoverContents(t, hover, "s:Target(value)", "")
		prepared, err := instance.PrepareRename(context.Background(), &protocol.PrepareRenameParams{TextDocumentPositionParams: params})
		preparedRange, ok := prepared.(*protocol.Range)
		if err != nil || !ok || *preparedRange != expected {
			t.Fatalf("prepare rename = %#v, error = %v", prepared, err)
		}
	}
	references, err := instance.References(context.Background(), &protocol.ReferenceParams{
		TextDocumentPositionParams: params, Context: protocol.ReferenceContext{IncludeDeclaration: true},
	})
	if err != nil || len(references) != len(ranges) {
		t.Fatalf("references = %#v, error = %v", references, err)
	}
	for index, expected := range ranges {
		if references[index].URI != documentURI || references[index].Range != expected {
			t.Fatalf("reference %d = %#v, want %v", index, references[index], expected)
		}
	}
	edit, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: params, NewName: "s:Renamed"})
	if err != nil || edit == nil || len(edit.DocumentChanges) != 1 {
		t.Fatalf("rename = %#v, error = %v", edit, err)
	}
	documentEdit := edit.DocumentChanges[0].(*protocol.TextDocumentEdit)
	if len(documentEdit.Edits) != len(ranges) {
		t.Fatalf("rename edits = %#v", documentEdit.Edits)
	}
	if got, want := applyTextEdits(t, source, documentEdit.Edits), strings.ReplaceAll(source, "s:Target", "s:Renamed"); got != want {
		t.Fatalf("renamed source = %q, want %q", got, want)
	}
}

func TestMappingEscapesCompletionAndSignature(t *testing.T) {
	for _, separator := range []string{`\|`, "<Bar>"} {
		t.Run(separator, func(t *testing.T) {
			source := "nnoremap <F5> :echo 'x' " + separator + " call getcw<CR>\n"
			instance, documentURI := openNavigationDocument(t, text.UTF16, source)
			start := uint32(strings.Index(source, "getcw"))
			params := protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Character: start + 5},
			}
			result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: params})
			list, ok := result.(*protocol.CompletionList)
			if err != nil || !ok || !hasCompletion(list.Items, "getcwd", protocol.CompletionItemKindFunction) {
				t.Fatalf("completion = %#v, error = %v", result, err)
			}
			for _, item := range list.Items {
				if item.Label == "getcwd" {
					edit, ok := item.TextEdit.(*protocol.TextEdit)
					if !ok || edit.Range != navigationRange(0, start, start+5) {
						t.Fatalf("completion edits escape spelling: %#v", item.TextEdit)
					}
				}
			}

			source = "nnoremap <F5> :echo 'x' " + separator + " call getcwd(0)<CR>\n"
			instance.documents.Open(documentURI.String(), 2, source)
			params.Position.Character = uint32(strings.Index(source, "(0)") + 1)
			help, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: params})
			if err != nil || help == nil || len(help.Signatures) != 1 || !strings.HasPrefix(help.Signatures[0].Label, "getcwd(") {
				t.Fatalf("signature help = %#v, error = %v", help, err)
			}
			tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: params.TextDocument})
			if err != nil {
				t.Fatal(err)
			}
			assertSemanticToken(t, tokens.Data, 0, start, semanticFunction, semanticDefaultLibrary)
		})
	}
}
