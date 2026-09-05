package server

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestPrepareRenameAndRenameBoundSymbol(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar value = 1\necho value\nvalue += 1\n")
	checks := 0
	instance.testHooks.beforeWorkspaceIdentityCheck = func() { checks++ }
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
	if checks != 0 {
		t.Fatalf("pure-local rename checked workspace identity %d times", checks)
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
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: mainURI, Version: 3, Text: mainSource}}); err != nil {
		t.Fatal(err)
	}
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: otherURI, Version: 7, Text: otherSource}}); err != nil {
		t.Fatal(err)
	}
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
	wantVersions := map[uri.URI]*int32{canonicalTestURI(t, libPath): nil, mainURI: pointerInt32(3), otherURI: pointerInt32(7)}
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

func pointerInt32(value int32) *int32 { return new(value) }

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

func TestWorkspaceRenameRejectsIncompleteIndex(t *testing.T) {
	instance, _, position := openWorkspaceNavigationRetryDocument(t, "vim9script\nimport './lib.vim' as lib\necho lib.Run()\n")
	instance.workspaceIndex.SetComplete(false)

	prepared, err := instance.PrepareRename(context.Background(), &protocol.PrepareRenameParams{TextDocumentPositionParams: position})
	if err != nil || prepared != nil {
		t.Fatalf("prepare with incomplete index = %#v, %v", prepared, err)
	}
	edit, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: position, NewName: "Execute"})
	if err == nil || edit != nil {
		t.Fatalf("rename with incomplete index = %#v, %v", edit, err)
	}
}

func TestRenameRejectsNamespaceChanges(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "let s:value = 1\necho s:value\n")
	params := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 8}}
	if _, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: params, NewName: "g:value"}); err == nil {
		t.Fatal("namespace-changing rename succeeded")
	}
}

func TestRenameRejectsBindingConflicts(t *testing.T) {
	for _, test := range []struct {
		name, source, replacement string
		position                  protocol.Position
	}{
		{"same scope", "vim9script\nvar first = 1\nvar other = 2\necho first + other\n", "other", protocol.Position{Line: 1, Character: 5}},
		{"legacy overwrite without references", "function! s:First()\nendfunction\nfunction! s:Other()\nendfunction\n", "s:Other", protocol.Position{Line: 0, Character: 13}},
		{"nested capture", "vim9script\nvar first = 1\ndef Run()\n  var other = 2\n  echo first + other\nenddef\n", "other", protocol.Position{Line: 1, Character: 5}},
		{"parameter conflict", "vim9script\ndef Run(other: number)\n  var first = 1\n  echo first + other\nenddef\n", "other", protocol.Position{Line: 2, Character: 7}},
		{"reserved name", "vim9script\nvar first = 1\necho first\n", "true", protocol.Position{Line: 1, Character: 5}},
		{"function capitalization", "vim9script\ndef Run()\nenddef\nRun()\n", "lowercase", protocol.Position{Line: 1, Character: 5}},
		{"capture existing unresolved use", "function! Run()\n  let first = 1\n  echo other\n  echo first\nendfunction\n", "other", protocol.Position{Line: 1, Character: 7}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			t.Cleanup(s.stopAnalysis)
			edit, err := s.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: test.position}, NewName: test.replacement})
			if err == nil || edit != nil {
				t.Fatalf("unsafe rename accepted: edit=%#v err=%v", edit, err)
			}
		})
	}
}

func TestRenameAllowsSameNameInSeparateFunctionScopes(t *testing.T) {
	source := "function! First()\n  let first = 1\n  echo first\nendfunction\nfunction! Second()\n  let other = 2\n  echo other\nendfunction\n"
	s, documentURI := openNavigationDocument(t, text.UTF16, source)
	t.Cleanup(s.stopAnalysis)
	edit, err := s.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 7}}, NewName: "other"})
	if err != nil || edit == nil {
		t.Fatalf("independent scope rejected: %#v %v", edit, err)
	}
}

func TestRenameRejectsGlobalCollisionInUneditedFile(t *testing.T) {
	root := t.TempDir()
	source := "function! First()\nendfunction\ncall First()\n"
	path := writeWorkspaceFile(t, root, "first.vim", source)
	writeWorkspaceFile(t, root, "other.vim", "function! Other()\nendfunction\n")
	s := initializeWorkspaceServer(t, root)
	documentURI := uri.File(path)
	s.documents.Open(documentURI.String(), 1, source)
	edit, err := s.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 0, Character: 12}}, NewName: "Other"})
	if err == nil || edit != nil {
		t.Fatalf("global collision accepted: %#v %v", edit, err)
	}
}

func TestRenameLegacyScriptLocalPrefixesPreservesSpelling(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "function! s:Run()\nendfunction\ncall s:Run()\ncall <SID>Run()\n")
	params := protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: 3, Character: 9},
	}
	edit, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: params, NewName: "s:Execute"})
	if err != nil || edit == nil || len(edit.DocumentChanges) != 1 {
		t.Fatalf("script-local rename = %#v, %v", edit, err)
	}
	documentEdit := edit.DocumentChanges[0].(*protocol.TextDocumentEdit)
	want := []struct {
		rangeValue protocol.Range
		newText    string
	}{
		{navigationRange(0, 10, 15), "s:Execute"},
		{navigationRange(2, 5, 10), "s:Execute"},
		{navigationRange(3, 5, 13), "<SID>Execute"},
	}
	if len(documentEdit.Edits) != len(want) {
		t.Fatalf("script-local edits = %#v", documentEdit.Edits)
	}
	for index, expected := range want {
		textEdit := documentEdit.Edits[index].(*protocol.TextEdit)
		if textEdit.Range != expected.rangeValue || textEdit.NewText != expected.newText {
			t.Errorf("edit %d = %#v, want %#v", index, textEdit, expected)
		}
	}
}

func TestRenameProvenVim9MemberAndRejectConstructor(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nclass Base\n  def Resize(width: number)\n  enddef\nendclass\nclass Child extends Base\nendclass\nvar child = Child.new()\necho child.Resize(1)\n")
	textDocument := protocol.TextDocumentIdentifier{URI: documentURI}
	method := protocol.TextDocumentPositionParams{TextDocument: textDocument, Position: protocol.Position{Line: 8, Character: 13}}
	edit, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: method, NewName: "Scale"})
	if err != nil || edit == nil || len(edit.DocumentChanges) != 1 {
		t.Fatalf("member rename = %#v, %v", edit, err)
	}
	edits := edit.DocumentChanges[0].(*protocol.TextDocumentEdit).Edits
	if len(edits) != 2 || edits[0].(*protocol.TextEdit).Range != navigationRange(2, 6, 12) || edits[1].(*protocol.TextEdit).Range != navigationRange(8, 11, 17) {
		t.Fatalf("member edits = %#v", edits)
	}
	constructor := protocol.TextDocumentPositionParams{TextDocument: textDocument, Position: protocol.Position{Line: 7, Character: 20}}
	if prepared, err := instance.PrepareRename(context.Background(), &protocol.PrepareRenameParams{TextDocumentPositionParams: constructor}); err != nil || prepared != nil {
		t.Fatalf("constructor prepare rename = %#v, %v", prepared, err)
	}
	if _, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: constructor, NewName: "Create"}); err == nil {
		t.Fatal("constructor rename succeeded")
	}
}

func TestRenameProvenInterfaceImplementationMember(t *testing.T) {
	source := "vim9script\ninterface Face\n  def Run(value: number): number\nendinterface\nclass Impl implements Face\n  def Run(value: number): number\n    return value\n  enddef\nendclass\nvar face: Face = Impl.new()\necho face.Run(1)\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	params := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 10, Character: 11}}
	edit, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: params, NewName: "Execute"})
	if err != nil || edit == nil || len(edit.DocumentChanges) != 1 {
		t.Fatalf("interface member rename = %#v, %v", edit, err)
	}
	edits := edit.DocumentChanges[0].(*protocol.TextDocumentEdit).Edits
	want := []protocol.Range{navigationRange(2, 6, 9), navigationRange(5, 6, 9), navigationRange(10, 10, 13)}
	if len(edits) != len(want) {
		t.Fatalf("interface member edits = %#v", edits)
	}
	for index := range want {
		if edits[index].(*protocol.TextEdit).Range != want[index] {
			t.Errorf("edit %d = %#v, want %#v", index, edits[index], want[index])
		}
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

func TestWorkspaceIdentityRenameRetriesAndRejectsStaleResults(t *testing.T) {
	t.Run("prepare retry", func(t *testing.T) {
		instance, _, position := openWorkspaceNavigationRetryDocument(t, "vim9script\nimport './lib.vim' as lib\necho lib.Run()\n")
		checks := 0
		instance.testHooks.beforeWorkspaceIdentityCheck = func() {
			checks++
			if checks == 1 {
				instance.workspaceMu.Lock()
				instance.workspaceRevision++
				instance.workspaceMu.Unlock()
			}
		}
		prepared, err := instance.PrepareRename(context.Background(), &protocol.PrepareRenameParams{TextDocumentPositionParams: position})
		if err != nil || prepared == nil || checks != 2 {
			t.Fatalf("prepare=%#v checks=%d error=%v", prepared, checks, err)
		}
	})

	t.Run("rename retry returns full edit", func(t *testing.T) {
		instance, documentURI, position := openWorkspaceNavigationRetryDocument(t, "vim9script\nimport './lib.vim' as lib\necho lib.Run()\n")
		checks := 0
		instance.testHooks.beforeWorkspaceIdentityCheck = func() {
			checks++
			if checks == 1 {
				instance.workspaceMu.Lock()
				instance.workspaceRevision++
				instance.workspaceMu.Unlock()
			}
		}
		edit, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: position, NewName: "Execute"})
		if err != nil || edit == nil || checks != 2 || len(edit.DocumentChanges) != 2 {
			t.Fatalf("edit=%#v checks=%d error=%v", edit, checks, err)
		}
		var openEdits, closedEdits int
		for _, change := range edit.DocumentChanges {
			documentEdit := change.(*protocol.TextDocumentEdit)
			if len(documentEdit.Edits) != 1 || documentEdit.Edits[0].(*protocol.TextEdit).NewText != "Execute" {
				t.Fatalf("document edit=%#v", documentEdit)
			}
			if documentEdit.TextDocument.URI == documentURI {
				if documentEdit.TextDocument.Version == nil || *documentEdit.TextDocument.Version != 1 {
					t.Fatalf("open document version=%#v", documentEdit.TextDocument.Version)
				}
				openEdits++
			} else if documentEdit.TextDocument.Version == nil {
				closedEdits++
			}
		}
		if openEdits != 1 || closedEdits != 1 {
			t.Fatalf("open=%d closed=%d edits=%#v", openEdits, closedEdits, edit.DocumentChanges)
		}
	})

	t.Run("second stale returns no edit", func(t *testing.T) {
		instance, _, position := openWorkspaceNavigationRetryDocument(t, "vim9script\nimport './lib.vim' as lib\necho lib.Run()\n")
		checks := 0
		instance.testHooks.beforeWorkspaceIdentityCheck = func() {
			checks++
			instance.workspaceMu.Lock()
			instance.workspaceRevision++
			instance.workspaceMu.Unlock()
		}
		edit, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: position, NewName: "Execute"})
		if !errors.Is(err, protocol.ErrContentModified) || edit != nil || checks != 2 {
			t.Fatalf("edit=%#v checks=%d error=%v", edit, checks, err)
		}
	})

	t.Run("workspace miss validates identity", func(t *testing.T) {
		instance, _, position := openWorkspaceNavigationRetryDocument(t, "vim9script\nvar path = './lib.vim'\nimport path as lib\necho lib.Run()\n")
		checks := 0
		instance.testHooks.beforeWorkspaceIdentityCheck = func() { checks++ }
		prepared, err := instance.PrepareRename(context.Background(), &protocol.PrepareRenameParams{TextDocumentPositionParams: position})
		if err != nil || prepared != nil || checks != 1 {
			t.Fatalf("prepare=%#v checks=%d error=%v", prepared, checks, err)
		}
	})

	t.Run("rename workspace miss is rejected after identity check", func(t *testing.T) {
		instance, _, position := openWorkspaceNavigationRetryDocument(t, "vim9script\nvar path = './lib.vim'\nimport path as lib\necho lib.Run()\n")
		checks := 0
		instance.testHooks.beforeWorkspaceIdentityCheck = func() { checks++ }
		edit, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: position, NewName: "Execute"})
		if edit != nil || err == nil || checks != 1 {
			t.Fatalf("edit=%#v checks=%d error=%v", edit, checks, err)
		}
	})

	t.Run("rename workspace miss retries after stale identity", func(t *testing.T) {
		instance, _, position := openWorkspaceNavigationRetryDocument(t, "vim9script\nvar path = './lib.vim'\nimport path as lib\necho lib.Run()\n")
		checks := 0
		instance.testHooks.beforeWorkspaceIdentityCheck = func() {
			checks++
			if checks == 1 {
				instance.workspaceMu.Lock()
				instance.workspaceRevision++
				instance.workspaceMu.Unlock()
			}
		}
		edit, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: position, NewName: "Execute"})
		if edit != nil || err == nil || checks != 2 {
			t.Fatalf("edit=%#v checks=%d error=%v", edit, checks, err)
		}
	})
}

func TestWorkspaceIdentityImportedMemberRename(t *testing.T) {
	source := "vim9script\nimport './lib.vim' as lib\nvar box: lib.Box\necho box.Resize(1)\n"

	t.Run("retries", func(t *testing.T) {
		instance, documentURI, _ := openWorkspaceFeatureRetryDocument(t, source)
		position := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 3, Character: 12}}
		checks := 0
		instance.testHooks.beforeWorkspaceIdentityCheck = func() {
			checks++
			if checks == 1 {
				instance.workspaceMu.Lock()
				instance.workspaceRevision++
				instance.workspaceMu.Unlock()
			}
		}
		edit, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: position, NewName: "Scale"})
		if err != nil || edit == nil || checks != 3 || len(edit.DocumentChanges) != 2 {
			t.Fatalf("edit=%#v checks=%d error=%v", edit, checks, err)
		}
	})

	t.Run("rejects second stale result", func(t *testing.T) {
		instance, documentURI, _ := openWorkspaceFeatureRetryDocument(t, source)
		position := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 3, Character: 12}}
		checks := 0
		instance.testHooks.beforeWorkspaceIdentityCheck = func() {
			checks++
			instance.workspaceMu.Lock()
			instance.workspaceRevision++
			instance.workspaceMu.Unlock()
		}
		edit, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: position, NewName: "Scale"})
		if !errors.Is(err, protocol.ErrContentModified) || edit != nil || checks != 2 {
			t.Fatalf("edit=%#v checks=%d error=%v", edit, checks, err)
		}
	})
}

func TestRenameOverlayScanKeepsCapturedSnapshot(t *testing.T) {
	source := "vim9script\nimport './lib.vim' as lib\necho lib.Run()\n"
	instance, documentURI, position := openWorkspaceNavigationRetryDocument(t, source)
	otherSource := "vim9script\nimport './lib.vim' as lib\necho lib.Run()\n"
	otherPath := writeWorkspaceFile(t, filepath.Dir(documentURI.FsPath()), "other.vim", otherSource)
	otherURI := uri.File(otherPath)
	instance.documents.Open(otherURI.String(), 1, otherSource)
	document, err := instance.navigationAt(context.Background(), documentURI.String(), position.Position)
	if err != nil || document == nil {
		t.Fatalf("navigation document=%#v error=%v", document, err)
	}
	state := instance.captureWorkspaceNavigationState()
	target, ok := document.workspaceTargetInState(state)
	if !ok {
		t.Fatal("workspace target was not resolved")
	}
	locations, err := document.workspaceReferencesInState(context.Background(), state, target, true)
	if err != nil {
		t.Fatal(err)
	}
	openLocations, snapshots, err := instance.openWorkspaceReferenceLocationsInState(context.Background(), state, target, text.UTF16)
	if err != nil {
		t.Fatal(err)
	}
	path, ok := workspaceURIPath(otherURI)
	if !ok {
		t.Fatalf("workspace path for %s", otherURI)
	}
	captured := snapshots[uri.File(path)]
	if captured == nil || captured.Text() != otherSource {
		t.Fatalf("scanned snapshot=%#v", captured)
	}
	locations = normalizeRenameLocations(append(locations, openLocations...))
	changed := "vim9script\nimport './lib.vim' as lib\necho lib.Other()\n"
	if _, _, err := instance.documents.Change(otherURI.String(), 2, text.UTF16, []text.Change{{Text: changed}}); err != nil {
		t.Fatal(err)
	}
	edits, used, err := instance.renameEdits(context.Background(), state, snapshots, text.UTF16, "Run", "Execute", locations)
	if err != nil || len(edits) != 3 {
		t.Fatalf("edits=%#v used=%#v error=%v", edits, used, err)
	}
	for _, change := range edits {
		documentEdit := change.(*protocol.TextDocumentEdit)
		if documentEdit.TextDocument.URI == otherURI && (documentEdit.TextDocument.Version == nil || *documentEdit.TextDocument.Version != 1) {
			t.Fatalf("rename switched to current snapshot version=%#v", documentEdit.TextDocument.Version)
		}
	}
	current, err := document.workspaceNavigationCurrent(context.Background(), state, target, used...)
	if err == nil || current || !errors.Is(err, protocol.ErrContentModified) {
		t.Fatalf("current=%t error=%v", current, err)
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
