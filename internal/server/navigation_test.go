package server

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestNavigationCapabilitiesAndMethods(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	result, err := instance.Initialize(context.Background(), &protocol.InitializeParams{})
	if err != nil {
		t.Fatal(err)
	}
	capabilities := result.Capabilities
	if capabilities.DeclarationProvider == nil || capabilities.DefinitionProvider == nil || capabilities.ReferencesProvider == nil || capabilities.DocumentHighlightProvider == nil || capabilities.HoverProvider == nil {
		t.Fatalf("navigation capabilities = %#v", capabilities)
	}
	for _, method := range []string{
		protocol.MethodTextDocumentDeclaration,
		protocol.MethodTextDocumentDefinition,
		protocol.MethodTextDocumentReferences,
		protocol.MethodTextDocumentDocumentHighlight,
		protocol.MethodTextDocumentHover,
	} {
		if !implementedMethod(method) {
			t.Errorf("method %q is not implemented", method)
		}
	}
}

func TestDocumentNavigation(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar value: number = 1\nvar copy = value\necho value\n")
	position := protocol.Position{Line: 2, Character: 12}
	textDocument := protocol.TextDocumentIdentifier{URI: documentURI}

	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: textDocument, Position: position},
	})
	if err != nil {
		t.Fatal(err)
	}
	definitionLocations, ok := definition.(protocol.LocationSlice)
	if !ok || len(definitionLocations) != 1 || definitionLocations[0].Range != navigationRange(1, 4, 9) {
		t.Fatalf("definition = %#v", definition)
	}

	declaration, err := instance.Declaration(context.Background(), &protocol.DeclarationParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: textDocument, Position: position},
	})
	if err != nil {
		t.Fatal(err)
	}
	declarationLocations, ok := declaration.(protocol.LocationSlice)
	if !ok || len(declarationLocations) != 1 || declarationLocations[0].Range != navigationRange(1, 4, 9) {
		t.Fatalf("declaration = %#v", declaration)
	}

	references, err := instance.References(context.Background(), &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: textDocument, Position: position},
		Context:                    protocol.ReferenceContext{IncludeDeclaration: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantRanges := []protocol.Range{navigationRange(1, 4, 9), navigationRange(2, 11, 16), navigationRange(3, 5, 10)}
	if len(references) != len(wantRanges) {
		t.Fatalf("references = %#v", references)
	}
	for index, want := range wantRanges {
		if references[index].Range != want {
			t.Errorf("reference %d = %#v, want %#v", index, references[index].Range, want)
		}
	}

	highlights, err := instance.DocumentHighlight(context.Background(), &protocol.DocumentHighlightParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: textDocument, Position: position},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(highlights) != len(wantRanges) {
		t.Fatalf("highlights = %#v", highlights)
	}
	for index, want := range wantRanges {
		if highlights[index].Range != want || highlights[index].Kind != protocol.DocumentHighlightKindText {
			t.Errorf("highlight %d = %#v, want range %#v", index, highlights[index], want)
		}
	}

	hover, err := instance.Hover(context.Background(), &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: textDocument, Position: position},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hover == nil {
		t.Fatal("hover is nil")
	}
	content, ok := hover.Contents.(*protocol.MarkupContent)
	if !ok || content.Kind != protocol.MarkupKindPlainText || content.Value != "name: value\nkind: variable\ntype: number" || hover.Range == nil || *hover.Range != navigationRange(2, 11, 16) {
		t.Fatalf("hover = %#v", hover)
	}
}

func TestNavigationUsesNegotiatedPositionEncoding(t *testing.T) {
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
			locations, err := instance.References(context.Background(), &protocol.ReferenceParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
					Position:     protocol.Position{Line: 2, Character: test.character},
				},
				Context: protocol.ReferenceContext{IncludeDeclaration: false},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(locations) != 1 || locations[0].Range.Start != (protocol.Position{Line: 2, Character: test.character}) {
				t.Fatalf("locations = %#v", locations)
			}
		})
	}
}

func TestReferencesStayPureLocalWithoutWorkspaceIdentityCheck(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar value = 1\necho value\n")
	checks := 0
	instance.beforeWorkspaceIdentityCheck = func() { checks++ }
	references, err := instance.References(context.Background(), &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Position:     protocol.Position{Line: 2, Character: 6},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	})
	if err != nil || checks != 0 || len(references) != 2 {
		t.Fatalf("references=%#v checks=%d error=%v", references, checks, err)
	}
}

func TestNavigationReturnsEmptyForUnavailableTargets(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\necho dynamic\n")
	unknown := protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: 1, Character: 6},
	}

	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: unknown})
	definitionLocations, ok := definition.(protocol.LocationSlice)
	if err != nil || !ok || len(definitionLocations) != 0 {
		t.Fatalf("unknown definition = %#v, error = %v", definition, err)
	}
	declaration, err := instance.Declaration(context.Background(), &protocol.DeclarationParams{TextDocumentPositionParams: unknown})
	declarationLocations, ok := declaration.(protocol.LocationSlice)
	if err != nil || !ok || len(declarationLocations) != 0 {
		t.Fatalf("unknown declaration = %#v, error = %v", declaration, err)
	}
	references, err := instance.References(context.Background(), &protocol.ReferenceParams{TextDocumentPositionParams: unknown})
	if err != nil || len(references) != 0 {
		t.Fatalf("unknown references = %#v, error = %v", references, err)
	}
	highlights, err := instance.DocumentHighlight(context.Background(), &protocol.DocumentHighlightParams{TextDocumentPositionParams: unknown})
	if err != nil || len(highlights) != 0 {
		t.Fatalf("unknown highlights = %#v, error = %v", highlights, err)
	}
	hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: unknown})
	if err != nil || hover != nil {
		t.Fatalf("unknown hover = %#v, error = %v", hover, err)
	}

	for name, params := range map[string]protocol.TextDocumentPositionParams{
		"missing document": {
			TextDocument: protocol.TextDocumentIdentifier{URI: uri.MustParse("file:///missing.vim")},
		},
		"line past end": {
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Position:     protocol.Position{Line: 20},
		},
		"character past end": {
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Position:     protocol.Position{Line: 1, Character: 200},
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: params})
			locations, ok := result.(protocol.LocationSlice)
			if err != nil || !ok || len(locations) != 0 {
				t.Fatalf("definition = %#v, error = %v", result, err)
			}
		})
	}
	t.Run("middle of UTF-16 surrogate pair", func(t *testing.T) {
		unicode, unicodeURI := openNavigationDocument(t, text.UTF16, "vim9script\necho '𐐀' | echo value\n")
		result, err := unicode.Definition(context.Background(), &protocol.DefinitionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: unicodeURI},
				Position:     protocol.Position{Line: 1, Character: 7},
			},
		})
		locations, ok := result.(protocol.LocationSlice)
		if err != nil || !ok || len(locations) != 0 {
			t.Fatalf("definition = %#v, error = %v", result, err)
		}
	})

	large, largeURI := openNavigationDocument(t, text.UTF16, strings.Repeat("x", maxFileBytes+1))
	result, err := large.Definition(context.Background(), &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: largeURI}},
	})
	locations, ok := result.(protocol.LocationSlice)
	if err != nil || !ok || len(locations) != 0 {
		t.Fatalf("large definition = %#v, error = %v", result, err)
	}
}

func TestNavigationCancellationAndSnapshotInvalidation(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar value = 1\necho value\n")
	params := protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: 2, Character: 6},
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := instance.Hover(canceled, &protocol.HoverParams{TextDocumentPositionParams: params}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Fatalf("canceled hover error = %v", err)
	}

	document, err := instance.navigationAt(context.Background(), documentURI.String(), params.Position)
	if err != nil || document == nil {
		t.Fatalf("navigation document = %#v, error = %v", document, err)
	}
	version := int32(2)
	if _, _, err := instance.documents.Change(documentURI.String(), version, text.UTF16, []text.Change{{Text: "vim9script\nvar changed = 1\n"}}); err != nil {
		t.Fatal(err)
	}
	if err := document.checkCurrent(context.Background()); !errors.Is(err, protocol.ErrContentModified) {
		t.Fatalf("modified snapshot error = %v", err)
	}

	document, err = instance.navigationAt(context.Background(), documentURI.String(), protocol.Position{Line: 1, Character: 5})
	if err != nil || document == nil {
		t.Fatalf("changed navigation document = %#v, error = %v", document, err)
	}
	instance.documents.Close(documentURI.String())
	if err := document.checkCurrent(context.Background()); !errors.Is(err, protocol.ErrContentModified) {
		t.Fatalf("closed snapshot error = %v", err)
	}
}

func TestNavigationReusesCurrentParsedDocument(t *testing.T) {
	source := "vim9script\nvar value = 1\necho value\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	snapshot, ok := instance.documents.Snapshot(documentURI.String())
	if !ok {
		t.Fatal("document snapshot is missing")
	}
	cached := syntax.Parse(source)
	instance.parsed[documentURI.String()] = parsedDocument{contentID: snapshot.ContentID(), file: cached}

	document, err := instance.navigationAt(context.Background(), documentURI.String(), protocol.Position{Line: 2, Character: 6})
	if err != nil || document == nil || document.analysis.File != cached {
		t.Fatalf("navigation analysis = %#v, error = %v", document, err)
	}

	stale := syntax.Parse("vim9script\necho missing\n")
	instance.parsed[documentURI.String()] = parsedDocument{contentID: snapshot.ContentID(), file: stale}
	document, err = instance.navigationAt(context.Background(), documentURI.String(), protocol.Position{Line: 2, Character: 6})
	if err != nil || document == nil || document.analysis.File == stale || document.declaration == nil {
		t.Fatalf("stale navigation analysis = %#v, error = %v", document, err)
	}
}

func TestHoverOmitsUnknownType(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar value = UnknownCall()\necho value\n")
	hover, err := instance.Hover(context.Background(), &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Position:     protocol.Position{Line: 2, Character: 6},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hover == nil {
		t.Fatal("hover is nil")
	}
	content, ok := hover.Contents.(*protocol.MarkupContent)
	if !ok || content.Value != "name: value\nkind: variable" {
		t.Fatalf("hover = %#v", hover)
	}
}

func TestHoverShowsPinnedBuiltinReturnType(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\necho argc()\n")
	hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: 1, Character: 7},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if hover == nil {
		t.Fatal("builtin hover is nil")
	}
	content, ok := hover.Contents.(*protocol.MarkupContent)
	if !ok || content.Value != "name: argc\nkind: builtin function\ntype: number" {
		t.Fatalf("builtin hover = %#v", hover)
	}
}

func TestCrossFileVim9ImportDefinitionDeclarationAndReferences(t *testing.T) {
	root := t.TempDir()
	libPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Run(): number\n  return 1\nenddef\ndef Private()\nenddef\nexport class Box\nendclass\n")
	libURI := canonicalTestURI(t, libPath)
	mainSource := "vim9script\nimport './lib.vim' as lib\nvar result = lib.Run()\nvar hidden = lib.Private()\nvar path = './lib.vim'\nimport path as dynamic\necho dynamic.Run()\nvar item: lib.Box\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", mainSource)
	otherPath := writeWorkspaceFile(t, root, "other.vim", "vim9script\nimport './lib.vim' as module\necho module.Run()\n")
	instance := initializeWorkspaceServer(t, root)
	mainURI := uri.File(mainPath)
	indexedMainURI := canonicalTestURI(t, mainPath)
	otherURI := canonicalTestURI(t, otherPath)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: mainURI, Version: 1, Text: mainSource,
	}}); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceSymbols(t, instance, "lib", 1)
	position := protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: mainURI},
		Position:     protocol.Position{Line: 2, Character: 18},
	}
	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: position})
	if err != nil {
		t.Fatal(err)
	}
	definitionLocations := definition.(protocol.LocationSlice)
	if len(definitionLocations) != 1 || definitionLocations[0].URI != libURI || definitionLocations[0].Range != navigationRange(1, 11, 14) {
		t.Fatalf("cross-file definition = %#v", definition)
	}
	declaration, err := instance.Declaration(context.Background(), &protocol.DeclarationParams{TextDocumentPositionParams: position})
	if err != nil {
		t.Fatal(err)
	}
	declarationLocations := declaration.(protocol.LocationSlice)
	if len(declarationLocations) != 1 || declarationLocations[0] != definitionLocations[0] {
		t.Fatalf("cross-file declaration = %#v", declaration)
	}
	references, err := instance.References(context.Background(), &protocol.ReferenceParams{
		TextDocumentPositionParams: position,
		Context:                    protocol.ReferenceContext{IncludeDeclaration: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []protocol.Location{
		{URI: libURI, Range: navigationRange(1, 11, 14)},
		{URI: indexedMainURI, Range: navigationRange(2, 17, 20)},
		{URI: otherURI, Range: navigationRange(2, 12, 15)},
	}
	if len(references) != len(want) {
		t.Fatalf("cross-file references = %#v", references)
	}
	for index := range want {
		if references[index] != want[index] {
			t.Errorf("reference %d = %#v, want %#v", index, references[index], want[index])
		}
	}
	withoutDeclaration, err := instance.References(context.Background(), &protocol.ReferenceParams{
		TextDocumentPositionParams: position,
		Context:                    protocol.ReferenceContext{IncludeDeclaration: false},
	})
	if err != nil || len(withoutDeclaration) != 2 {
		t.Fatalf("cross-file references without declaration = %#v, error = %v", withoutDeclaration, err)
	}
	for _, location := range withoutDeclaration {
		if location.URI == libURI && location.Range == navigationRange(1, 11, 14) {
			t.Fatalf("declaration included when disabled: %#v", withoutDeclaration)
		}
	}
	highlights, err := instance.DocumentHighlight(context.Background(), &protocol.DocumentHighlightParams{TextDocumentPositionParams: position})
	if err != nil || len(highlights) != 1 || highlights[0].Range != navigationRange(2, 17, 20) {
		t.Fatalf("cross-file highlights = %#v, error = %v", highlights, err)
	}
	hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: position})
	if err != nil {
		t.Fatal(err)
	}
	content, ok := hover.Contents.(*protocol.MarkupContent)
	if !ok || content.Value != "name: Run\nkind: function\ntype: func(): number" || hover.Range == nil || *hover.Range != navigationRange(2, 17, 20) {
		t.Fatalf("cross-file hover = %#v", hover)
	}

	unknown, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: mainURI}, Position: protocol.Position{Line: 3, Character: 19},
	}})
	if err != nil || len(unknown.(protocol.LocationSlice)) != 0 {
		t.Fatalf("private import definition = %#v, error = %v", unknown, err)
	}
	dynamic, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: mainURI}, Position: protocol.Position{Line: 6, Character: 14},
	}})
	if err != nil || len(dynamic.(protocol.LocationSlice)) != 0 {
		t.Fatalf("dynamic import definition = %#v, error = %v", dynamic, err)
	}
	typeDefinition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: mainURI}, Position: protocol.Position{Line: 7, Character: 15},
	}})
	if err != nil {
		t.Fatal(err)
	}
	typeLocations := typeDefinition.(protocol.LocationSlice)
	if len(typeLocations) != 1 || typeLocations[0].URI != libURI || typeLocations[0].Range != navigationRange(6, 13, 16) {
		t.Fatalf("imported type definition = %#v", typeDefinition)
	}
	typeReferences, err := instance.References(context.Background(), &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: mainURI}, Position: protocol.Position{Line: 7, Character: 15},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	})
	if err != nil || len(typeReferences) != 2 || typeReferences[0].URI != libURI || typeReferences[1].URI != indexedMainURI {
		t.Fatalf("imported type references = %#v, error = %v", typeReferences, err)
	}
	writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Changed()\nenddef\n")
	if err := instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{Changes: []protocol.FileEvent{{URI: uri.File(libPath), Type: protocol.FileChangeTypeChanged}}}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	stale, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: position})
	if err != nil || len(stale.(protocol.LocationSlice)) != 0 {
		t.Fatalf("definition after client file event = %#v, error = %v", stale, err)
	}
}

func TestCrossFileLegacyAutoloadDefinitionAndReferences(t *testing.T) {
	root := t.TempDir()
	targetPath := writeWorkspaceFile(t, root, filepath.Join("autoload", "foo", "bar.vim"), "function foo#bar#Run()\nendfunction\n")
	mainPath := writeWorkspaceFile(t, root, "plugin.vim", "call foo#bar#Run()\n")
	otherPath := writeWorkspaceFile(t, root, "other.vim", "let value = foo#bar#Run()\n")
	instance := initializeWorkspaceServer(t, root)
	mainURI := uri.File(mainPath)
	targetURI := canonicalTestURI(t, targetPath)
	indexedMainURI := canonicalTestURI(t, mainPath)
	otherURI := canonicalTestURI(t, otherPath)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: mainURI, Version: 1, Text: "call foo#bar#Run()\n",
	}}); err != nil {
		t.Fatal(err)
	}
	position := protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: mainURI},
		Position:     protocol.Position{Line: 0, Character: 10},
	}
	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: position})
	if err != nil {
		t.Fatal(err)
	}
	locations := definition.(protocol.LocationSlice)
	if len(locations) != 1 || locations[0].URI != targetURI || locations[0].Range != navigationRange(0, 9, 20) {
		t.Fatalf("autoload definition = %#v", definition)
	}
	references, err := instance.References(context.Background(), &protocol.ReferenceParams{
		TextDocumentPositionParams: position,
		Context:                    protocol.ReferenceContext{IncludeDeclaration: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []protocol.Location{
		{URI: targetURI, Range: navigationRange(0, 9, 20)},
		{URI: otherURI, Range: navigationRange(0, 12, 23)},
		{URI: indexedMainURI, Range: navigationRange(0, 5, 16)},
	}
	sort.SliceStable(want, func(left, right int) bool { return want[left].URI < want[right].URI })
	if len(references) != len(want) {
		t.Fatalf("autoload references = %#v", references)
	}
	for index := range want {
		if references[index] != want[index] {
			t.Errorf("reference %d = %#v, want %#v", index, references[index], want[index])
		}
	}
}

func TestCrossFileVim9AutoloadExportUsesImportAndLegacyNames(t *testing.T) {
	root := t.TempDir()
	targetPath := writeWorkspaceFile(t, root, filepath.Join("autoload", "api.vim"), "vim9script\nexport def Run(): string\n  return 'ok'\nenddef\n")
	legacyPath := writeWorkspaceFile(t, root, "legacy.vim", "call api#Run()\n")
	importPath := writeWorkspaceFile(t, root, "plugin.vim", "vim9script\nimport autoload 'api.vim'\necho api.Run()\n")
	instance := initializeWorkspaceServer(t, root)
	legacyURI := uri.File(legacyPath)
	targetURI := canonicalTestURI(t, targetPath)
	indexedLegacyURI := canonicalTestURI(t, legacyPath)
	importURI := canonicalTestURI(t, importPath)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: legacyURI, Version: 1, Text: "call api#Run()\n",
	}}); err != nil {
		t.Fatal(err)
	}
	position := protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: legacyURI},
		Position:     protocol.Position{Line: 0, Character: 8},
	}
	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: position})
	if err != nil {
		t.Fatal(err)
	}
	locations := definition.(protocol.LocationSlice)
	if len(locations) != 1 || locations[0].URI != targetURI || locations[0].Range != navigationRange(1, 11, 14) {
		t.Fatalf("Vim9 autoload definition = %#v", definition)
	}
	references, err := instance.References(context.Background(), &protocol.ReferenceParams{
		TextDocumentPositionParams: position,
		Context:                    protocol.ReferenceContext{IncludeDeclaration: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []protocol.Location{
		{URI: targetURI, Range: navigationRange(1, 11, 14)},
		{URI: indexedLegacyURI, Range: navigationRange(0, 5, 12)},
		{URI: importURI, Range: navigationRange(2, 9, 12)},
	}
	sort.SliceStable(want, func(left, right int) bool { return want[left].URI < want[right].URI })
	if len(references) != len(want) {
		t.Fatalf("Vim9 autoload references = %#v", references)
	}
	for index := range want {
		if references[index] != want[index] {
			t.Errorf("reference %d = %#v, want %#v", index, references[index], want[index])
		}
	}
}

func TestCrossFileNavigationUsesNegotiatedEncodingAndInvalidatesOpenTarget(t *testing.T) {
	root := t.TempDir()
	targetSource := "vim9script\nexport def 𐐀Run()\nenddef\n"
	targetPath := writeWorkspaceFile(t, root, "lib.vim", targetSource)
	mainSource := "vim9script\nimport './lib.vim' as lib\necho lib.𐐀Run()\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", mainSource)
	rootURI := uri.File(root)
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI:               &rootURI,
		InitializationOptions: protocol.LSPAny([]byte(`{"runtimepath":[]}`)),
		Capabilities: protocol.ClientCapabilities{General: &protocol.GeneralClientCapabilities{
			PositionEncodings: []protocol.PositionEncodingKind{protocol.PositionEncodingKindUTF8},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	mainURI := uri.File(mainPath)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: mainURI, Version: 1, Text: mainSource}}); err != nil {
		t.Fatal(err)
	}
	position := protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: mainURI},
		Position:     protocol.Position{Line: 2, Character: 9},
	}
	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: position})
	if err != nil {
		t.Fatal(err)
	}
	locations := definition.(protocol.LocationSlice)
	if len(locations) != 1 || locations[0].URI != canonicalTestURI(t, targetPath) || locations[0].Range != navigationRange(1, 11, 18) {
		t.Fatalf("UTF-8 cross-file definition = %#v", definition)
	}

	targetURI := uri.File(targetPath)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: targetURI, Version: 1, Text: targetSource}}); err != nil {
		t.Fatal(err)
	}
	document, err := instance.navigationAt(context.Background(), mainURI.String(), position.Position)
	if err != nil || document == nil {
		t.Fatalf("navigation document = %#v, error = %v", document, err)
	}
	target, ok := document.workspaceTarget()
	if !ok || target.openSnapshot == nil {
		t.Fatalf("open workspace target = %#v", target)
	}
	if _, _, err := instance.documents.Change(targetURI.String(), 2, text.UTF8, []text.Change{{Text: "vim9script\nexport def Changed()\nenddef\n"}}); err != nil {
		t.Fatal(err)
	}
	if err := document.checkWorkspaceTarget(context.Background(), target); !errors.Is(err, protocol.ErrContentModified) {
		t.Fatalf("modified target error = %v", err)
	}
}

func TestCrossFileNavigationHandlesCyclicAndAmbiguousImports(t *testing.T) {
	root := t.TempDir()
	aSource := "vim9script\nimport './b.vim' as b\nexport def A(): number\n  return b.B()\nenddef\n"
	aPath := writeWorkspaceFile(t, root, "a.vim", aSource)
	bPath := writeWorkspaceFile(t, root, "b.vim", "vim9script\nimport './a.vim' as a\nexport def B(): number\n  return a.A()\nenddef\n")
	writeWorkspaceFile(t, root, "duplicate.vim", "vim9script\nexport def Same()\nenddef\nexport def Same()\nenddef\n")
	duplicateMain := writeWorkspaceFile(t, root, "duplicate-main.vim", "vim9script\nimport './duplicate.vim' as duplicate\necho duplicate.Same()\n")
	instance := initializeWorkspaceServer(t, root)
	aURI := uri.File(aPath)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: aURI, Version: 1, Text: aSource}}); err != nil {
		t.Fatal(err)
	}
	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: aURI}, Position: protocol.Position{Line: 3, Character: 11},
	}})
	if err != nil {
		t.Fatal(err)
	}
	locations := definition.(protocol.LocationSlice)
	if len(locations) != 1 || locations[0].URI != canonicalTestURI(t, bPath) || locations[0].Range != navigationRange(2, 11, 12) {
		t.Fatalf("cyclic import definition = %#v", definition)
	}

	duplicateURI := uri.File(duplicateMain)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: duplicateURI, Version: 1, Text: "vim9script\nimport './duplicate.vim' as duplicate\necho duplicate.Same()\n",
	}}); err != nil {
		t.Fatal(err)
	}
	ambiguous, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: duplicateURI}, Position: protocol.Position{Line: 2, Character: 17},
	}})
	if err != nil || len(ambiguous.(protocol.LocationSlice)) != 0 {
		t.Fatalf("ambiguous import definition = %#v, error = %v", ambiguous, err)
	}
}

func TestCrossFileLegacyImportUsesDefaultScriptLocalName(t *testing.T) {
	root := t.TempDir()
	targetPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport function Run()\nendfunction\n")
	mainSource := "import './lib.vim'\ncall s:lib.Run()\n"
	mainPath := writeWorkspaceFile(t, root, "legacy-import.vim", mainSource)
	instance := initializeWorkspaceServer(t, root)
	mainURI := uri.File(mainPath)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: mainURI, Version: 1, Text: mainSource}}); err != nil {
		t.Fatal(err)
	}
	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: mainURI}, Position: protocol.Position{Line: 1, Character: 12},
	}})
	if err != nil {
		t.Fatal(err)
	}
	locations := definition.(protocol.LocationSlice)
	if len(locations) != 1 || locations[0].URI != canonicalTestURI(t, targetPath) || locations[0].Range != navigationRange(1, 16, 19) {
		t.Fatalf("legacy default import definition = %#v", definition)
	}
}

func TestCrossFileIndexFollowsBoundedStaticImports(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "nested", "vim9script\nexport const Value = 1\n")
	targetPath := writeWorkspaceFile(t, root, "module", "vim9script\nimport './nested' as nested\nexport def Run(): number\n  return nested.Value\nenddef\n")
	mainSource := "vim9script\nimport './module' as module\necho module.Run()\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", mainSource)
	instance := initializeWorkspaceServer(t, root)
	if symbols := workspaceSymbols(t, instance, "Run"); len(symbols) != 1 || symbols[0].Name != "Run" {
		t.Fatalf("transitively indexed symbols = %#v", symbols)
	}
	mainURI := uri.File(mainPath)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: mainURI, Version: 1, Text: mainSource}}); err != nil {
		t.Fatal(err)
	}
	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: mainURI}, Position: protocol.Position{Line: 2, Character: 13},
	}})
	if err != nil {
		t.Fatal(err)
	}
	locations := definition.(protocol.LocationSlice)
	if len(locations) != 1 || locations[0].URI != canonicalTestURI(t, targetPath) || locations[0].Range != navigationRange(2, 11, 14) {
		t.Fatalf("transitive import definition = %#v", definition)
	}
}

func TestWorkspaceIdentityNavigationRetriesCurrentResult(t *testing.T) {
	tests := []struct {
		name string
		want int
		run  func(*Server, protocol.TextDocumentPositionParams) (int, error)
	}{
		{name: "definition", want: 1, run: func(instance *Server, position protocol.TextDocumentPositionParams) (int, error) {
			result, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: position})
			return len(result.(protocol.LocationSlice)), err
		}},
		{name: "declaration", want: 1, run: func(instance *Server, position protocol.TextDocumentPositionParams) (int, error) {
			result, err := instance.Declaration(context.Background(), &protocol.DeclarationParams{TextDocumentPositionParams: position})
			return len(result.(protocol.LocationSlice)), err
		}},
		{name: "references", want: 2, run: func(instance *Server, position protocol.TextDocumentPositionParams) (int, error) {
			result, err := instance.References(context.Background(), &protocol.ReferenceParams{TextDocumentPositionParams: position, Context: protocol.ReferenceContext{IncludeDeclaration: true}})
			return len(result), err
		}},
		{name: "document highlight", want: 1, run: func(instance *Server, position protocol.TextDocumentPositionParams) (int, error) {
			result, err := instance.DocumentHighlight(context.Background(), &protocol.DocumentHighlightParams{TextDocumentPositionParams: position})
			return len(result), err
		}},
		{name: "hover", want: 1, run: func(instance *Server, position protocol.TextDocumentPositionParams) (int, error) {
			result, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: position})
			if result == nil {
				return 0, err
			}
			return 1, err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, _, position := openWorkspaceNavigationRetryDocument(t, "vim9script\nimport './lib.vim' as lib\necho lib.Run()\n")
			checks := 0
			instance.beforeWorkspaceIdentityCheck = func() {
				checks++
				if checks == 1 {
					instance.workspaceMu.Lock()
					instance.workspaceRevision++
					instance.workspaceMu.Unlock()
				}
			}
			count, err := test.run(instance, position)
			if err != nil || checks != 2 || count != test.want {
				t.Fatalf("result count=%d checks=%d error=%v", count, checks, err)
			}
		})
	}
}

func TestWorkspaceIdentityNavigationDropsSecondStaleResult(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(*Server, protocol.TextDocumentPositionParams) (int, error)
	}{
		{name: "definition", run: func(instance *Server, position protocol.TextDocumentPositionParams) (int, error) {
			result, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: position})
			if result == nil {
				return 0, err
			}
			return len(result.(protocol.LocationSlice)), err
		}},
		{name: "references", run: func(instance *Server, position protocol.TextDocumentPositionParams) (int, error) {
			result, err := instance.References(context.Background(), &protocol.ReferenceParams{TextDocumentPositionParams: position, Context: protocol.ReferenceContext{IncludeDeclaration: true}})
			return len(result), err
		}},
		{name: "document highlight", run: func(instance *Server, position protocol.TextDocumentPositionParams) (int, error) {
			result, err := instance.DocumentHighlight(context.Background(), &protocol.DocumentHighlightParams{TextDocumentPositionParams: position})
			return len(result), err
		}},
		{name: "hover", run: func(instance *Server, position protocol.TextDocumentPositionParams) (int, error) {
			result, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: position})
			if result == nil {
				return 0, err
			}
			return 1, err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance, _, position := openWorkspaceNavigationRetryDocument(t, "vim9script\nimport './lib.vim' as lib\necho lib.Run()\n")
			checks := 0
			instance.beforeWorkspaceIdentityCheck = func() {
				checks++
				instance.workspaceMu.Lock()
				instance.workspaceRevision++
				instance.workspaceMu.Unlock()
			}
			count, err := test.run(instance, position)
			if !errors.Is(err, protocol.ErrContentModified) || count != 0 || checks != 2 {
				t.Fatalf("result count=%d checks=%d error=%v", count, checks, err)
			}
		})
	}
}

func TestWorkspaceIdentityNavigationValidatesMiss(t *testing.T) {
	instance, _, position := openWorkspaceNavigationRetryDocument(t, "vim9script\nvar path = './lib.vim'\nimport path as lib\necho lib.Run()\n")
	checks := 0
	instance.beforeWorkspaceIdentityCheck = func() {
		checks++
		instance.workspaceMu.Lock()
		instance.workspaceRevision++
		instance.workspaceMu.Unlock()
	}
	result, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: position})
	locations, ok := result.(protocol.LocationSlice)
	if !errors.Is(err, protocol.ErrContentModified) || !ok || len(locations) != 0 || checks != 2 {
		t.Fatalf("result=%#v checks=%d error=%v", result, checks, err)
	}
}

func openWorkspaceNavigationRetryDocument(t *testing.T, source string) (*Server, uri.URI, protocol.TextDocumentPositionParams) {
	t.Helper()
	root := t.TempDir()
	writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Run(): number\n  return 1\nenddef\n")
	mainPath := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := uri.File(mainPath)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source}}); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceSymbols(t, instance, "Run", 1)
	return instance, documentURI, protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: uint32(strings.Count(source[:strings.Index(source, "Run")], "\n")), Character: 9},
	}
}

func openNavigationDocument(t *testing.T, encoding text.Encoding, source string) (*Server, uri.URI) {
	t.Helper()
	instance := New(nil, nil, io.Discard)
	instance.encoding = encoding
	documentURI := uri.MustParse("file:///navigation.vim")
	version := int32(1)
	instance.documents.Open(documentURI.String(), version, source)
	return instance, documentURI
}

func canonicalTestURI(t *testing.T, path string) uri.URI {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return uri.File(canonical)
}

func navigationRange(line, start, end uint32) protocol.Range {
	return protocol.Range{
		Start: protocol.Position{Line: line, Character: start},
		End:   protocol.Position{Line: line, Character: end},
	}
}
