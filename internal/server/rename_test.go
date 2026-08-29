package server

import (
	"context"
	"errors"
	"testing"

	"github.com/chemzqm/vimls-go/internal/syntax"
	"github.com/chemzqm/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestPrepareRenameAndRenameBoundSymbol(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar value = 1\necho value\nvalue += 1\n")
	params := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 7}}
	prepared, err := instance.PrepareRename(context.Background(), &protocol.PrepareRenameParams{TextDocumentPositionParams: params})
	if err != nil {
		t.Fatal(err)
	}
	preparedRange, ok := prepared.(*protocol.Range)
	if !ok || *preparedRange != navigationRange(2, 5, 10) {
		t.Fatalf("prepare rename = %#v", prepared)
	}
	edit, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: params, NewName: "renamed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(edit.DocumentChanges) != 1 {
		t.Fatalf("workspace edit = %#v", edit)
	}
	documentEdit, ok := edit.DocumentChanges[0].(*protocol.TextDocumentEdit)
	if !ok || documentEdit.TextDocument.Version == nil || *documentEdit.TextDocument.Version != 1 || len(documentEdit.Edits) != 3 {
		t.Fatalf("document edit = %#v", edit.DocumentChanges[0])
	}
	want := []protocol.Range{navigationRange(1, 4, 9), navigationRange(2, 5, 10), navigationRange(3, 0, 5)}
	for index, element := range documentEdit.Edits {
		textEdit, ok := element.(*protocol.TextEdit)
		if !ok || textEdit.Range != want[index] || textEdit.NewText != "renamed" {
			t.Fatalf("edit %d = %#v", index, element)
		}
	}
}

func TestRenameStaticImportAcrossClosedAndOpenDocuments(t *testing.T) {
	root := t.TempDir()
	libPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Run()\n  return Run()\nenddef\n")
	mainSource := "vim9script\nimport './lib.vim' as lib\necho lib.Run()\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", mainSource)
	otherSource := "vim9script\nimport './lib.vim' as lib\necho lib.Run()\n"
	otherPath := writeWorkspaceFile(t, root, "other.vim", otherSource)
	instance := initializeWorkspaceServer(t, root)
	mainURI := uri.File(mainPath)
	otherURI := uri.File(otherPath)
	instance.documents.Open(mainURI.String(), 3, mainSource)
	instance.removeWorkspaceURI(mainURI.String())
	instance.documents.Open(otherURI.String(), 7, otherSource)
	instance.removeWorkspaceURI(otherURI.String())
	params := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: mainURI}, Position: protocol.Position{Line: 2, Character: 10}}
	prepared, err := instance.PrepareRename(context.Background(), &protocol.PrepareRenameParams{TextDocumentPositionParams: params})
	if err != nil || prepared == nil {
		t.Fatalf("prepare import rename = %#v, %v", prepared, err)
	}
	edit, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: params, NewName: "Execute"})
	if err != nil {
		t.Fatal(err)
	}
	if len(edit.DocumentChanges) != 3 {
		t.Fatalf("document changes = %#v", edit.DocumentChanges)
	}
	wantVersions := map[uri.URI]*int32{uri.File(libPath): nil, mainURI: pointerInt32(3), otherURI: pointerInt32(7)}
	for _, change := range edit.DocumentChanges {
		documentEdit := change.(*protocol.TextDocumentEdit)
		wantVersion, ok := wantVersions[documentEdit.TextDocument.URI]
		if !ok {
			t.Fatalf("unexpected document edit URI %s", documentEdit.TextDocument.URI)
		}
		if wantVersion == nil && documentEdit.TextDocument.Version != nil || wantVersion != nil && (documentEdit.TextDocument.Version == nil || *documentEdit.TextDocument.Version != *wantVersion) {
			t.Fatalf("version for %s = %#v, want %#v", documentEdit.TextDocument.URI, documentEdit.TextDocument.Version, wantVersion)
		}
		for _, element := range documentEdit.Edits {
			if textEdit := element.(*protocol.TextEdit); textEdit.NewText != "Execute" {
				t.Fatalf("edit = %#v", textEdit)
			}
		}
	}
}

func pointerInt32(value int32) *int32 { return &value }

func TestRenameRejectsUnknownDynamicAndInvalidNames(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\necho unknown\n")
	params := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 7}}
	if prepared, err := instance.PrepareRename(context.Background(), &protocol.PrepareRenameParams{TextDocumentPositionParams: params}); err != nil || prepared != nil {
		t.Fatalf("unknown prepare rename = %#v, %v", prepared, err)
	}
	if _, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: params, NewName: "valid"}); err == nil {
		t.Fatal("unknown rename succeeded")
	}
	if _, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: params, NewName: "bad-name"}); err == nil {
		t.Fatal("invalid rename succeeded")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := instance.PrepareRename(canceled, &protocol.PrepareRenameParams{TextDocumentPositionParams: params}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Fatalf("prepare cancellation = %v", err)
	}
}

func TestRenameRejectsNamespaceChanges(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "let s:value = 1\necho s:value\n")
	params := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 8}}
	if _, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: params, NewName: "g:value"}); err == nil {
		t.Fatal("namespace-changing rename succeeded")
	}
}

func TestRenameRejectsAutoloadReferenceWithDifferentSpelling(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "autoload/api.vim", "function api#Run()\nendfunction\n")
	mainSource := "call api#Run()\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", mainSource)
	instance := initializeWorkspaceServer(t, root)
	documentURI := uri.File(mainPath)
	instance.documents.Open(documentURI.String(), 1, mainSource)
	params := protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: 0, Character: 9},
	}
	if prepared, err := instance.PrepareRename(context.Background(), &protocol.PrepareRenameParams{TextDocumentPositionParams: params}); err != nil || prepared != nil {
		t.Fatalf("autoload prepare rename = %#v, %v", prepared, err)
	}
	if _, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: params, NewName: "Execute"}); err == nil {
		t.Fatal("autoload rename succeeded despite differing external spelling")
	}
}

func TestRenameIndexGenerationCheck(t *testing.T) {
	instance, _ := openNavigationDocument(t, text.UTF16, "")
	index := instance.workspaceIndex
	revision := index.Revision()
	path := writeWorkspaceFile(t, t.TempDir(), "entry.vim", "var value = 1\n")
	if err := index.Replace(path, syntax.Parse("var value = 1\n")); err != nil {
		t.Fatal(err)
	}
	if err := instance.checkRenameIndex(index, revision); !errors.Is(err, protocol.ErrContentModified) {
		t.Fatalf("generation check = %v", err)
	}
}

func TestRenameUsesNegotiatedEncoding(t *testing.T) {
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
			edit, err := instance.Rename(context.Background(), &protocol.RenameParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
					Position:     protocol.Position{Line: 2, Character: test.character},
				},
				NewName: "renamed",
			})
			if err != nil {
				t.Fatal(err)
			}
			documentEdit := edit.DocumentChanges[0].(*protocol.TextDocumentEdit)
			reference := documentEdit.Edits[1].(*protocol.TextEdit)
			if reference.Range.Start.Character != test.character || reference.Range.End.Character != test.character+5 {
				t.Fatalf("reference range = %#v", reference.Range)
			}
		})
	}
}
