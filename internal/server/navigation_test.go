package server

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
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

func TestLegacyScriptLocalPrefixNavigation(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "function! s:Run()\nendfunction\ncall s:Run()\ncall <SID>Run()\n")
	position := protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: 3, Character: 9},
	}
	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: position})
	if err != nil {
		t.Fatal(err)
	}
	locations := definition.(protocol.LocationSlice)
	if len(locations) != 1 || locations[0].Range != navigationRange(0, 10, 15) {
		t.Fatalf("script-local definition = %#v", definition)
	}
	declaration, err := instance.Declaration(context.Background(), &protocol.DeclarationParams{TextDocumentPositionParams: position})
	if err != nil || len(declaration.(protocol.LocationSlice)) != 1 || declaration.(protocol.LocationSlice)[0] != locations[0] {
		t.Fatalf("script-local declaration = %#v, %v", declaration, err)
	}
	references, err := instance.References(context.Background(), &protocol.ReferenceParams{
		TextDocumentPositionParams: position, Context: protocol.ReferenceContext{IncludeDeclaration: true},
	})
	want := []protocol.Range{navigationRange(0, 10, 15), navigationRange(2, 5, 10), navigationRange(3, 5, 13)}
	if err != nil || len(references) != len(want) {
		t.Fatalf("script-local references = %#v, %v", references, err)
	}
	for index := range want {
		if references[index].Range != want[index] {
			t.Errorf("reference %d = %#v, want %#v", index, references[index].Range, want[index])
		}
	}
	highlights, err := instance.DocumentHighlight(context.Background(), &protocol.DocumentHighlightParams{TextDocumentPositionParams: position})
	if err != nil || len(highlights) != len(want) {
		t.Fatalf("script-local highlights = %#v, %v", highlights, err)
	}
	for index := range want {
		if highlights[index].Range != want[index] {
			t.Errorf("highlight %d = %#v, want %#v", index, highlights[index].Range, want[index])
		}
	}
}

func TestLegacyArgumentAndLocalPrefixNavigation(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "function! Run(arg)\n  let local = a:arg\n  echo l:local a:arg\nendfunction\n")
	tests := []struct {
		name     string
		position protocol.Position
		want     protocol.Range
	}{
		{name: "argument", position: protocol.Position{Line: 2, Character: 17}, want: navigationRange(0, 14, 17)},
		{name: "local", position: protocol.Position{Line: 2, Character: 10}, want: navigationRange(1, 6, 11)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: test.position,
			}})
			if err != nil {
				t.Fatal(err)
			}
			locations := definition.(protocol.LocationSlice)
			if len(locations) != 1 || locations[0].Range != test.want {
				t.Fatalf("definition = %#v, want %#v", definition, test.want)
			}
		})
	}
}

func TestLegacyExplicitScopeNavigation(t *testing.T) {
	source := "let g:item = 1\nlet b:item = 2\nlet w:item = 3\nlet t:item = 4\nlet s:item = 5\necho g:item b:item w:item t:item s:item\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	for index, character := range []uint32{7, 14, 21, 28, 35} {
		definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 5, Character: character},
		}})
		if err != nil {
			t.Fatal(err)
		}
		locations := definition.(protocol.LocationSlice)
		if len(locations) != 1 || locations[0].Range != navigationRange(uint32(index), 4, 10) {
			t.Errorf("scope %d definition = %#v", index, definition)
		}
	}
}

func TestLocalVim9MemberNavigation(t *testing.T) {
	source := "vim9script\nclass Base\n  var value: number\n  def Resize(width: number)\n  enddef\nendclass\nclass Child extends Base\nendclass\nvar child = Child.new()\necho child.Resize(1)\necho child.value\necho Child.new()\nenum Color\n  Red,\n  Green\nendenum\necho Color.Red\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	textDocument := protocol.TextDocumentIdentifier{URI: documentURI}
	tests := []struct {
		name       string
		position   protocol.Position
		definition protocol.Range
		references []protocol.Range
	}{
		{name: "inherited method", position: protocol.Position{Line: 9, Character: 13}, definition: navigationRange(3, 6, 12), references: []protocol.Range{navigationRange(3, 6, 12), navigationRange(9, 11, 17)}},
		{name: "inherited variable", position: protocol.Position{Line: 10, Character: 13}, definition: navigationRange(2, 6, 11), references: []protocol.Range{navigationRange(2, 6, 11), navigationRange(10, 11, 16)}},
		{name: "default constructor", position: protocol.Position{Line: 11, Character: 12}, definition: navigationRange(6, 6, 11), references: []protocol.Range{navigationRange(6, 6, 11), navigationRange(8, 18, 21), navigationRange(11, 11, 14)}},
		{name: "enum value", position: protocol.Position{Line: 16, Character: 13}, definition: navigationRange(13, 2, 5), references: []protocol.Range{navigationRange(13, 2, 5), navigationRange(16, 11, 14)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := protocol.TextDocumentPositionParams{TextDocument: textDocument, Position: test.position}
			definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: params})
			if err != nil {
				t.Fatal(err)
			}
			locations := definition.(protocol.LocationSlice)
			if len(locations) != 1 || locations[0].Range != test.definition {
				t.Fatalf("definition = %#v", definition)
			}
			declaration, err := instance.Declaration(context.Background(), &protocol.DeclarationParams{TextDocumentPositionParams: params})
			if err != nil || len(declaration.(protocol.LocationSlice)) != 1 || declaration.(protocol.LocationSlice)[0].Range != test.definition {
				t.Fatalf("declaration = %#v, %v", declaration, err)
			}
			references, err := instance.References(context.Background(), &protocol.ReferenceParams{TextDocumentPositionParams: params, Context: protocol.ReferenceContext{IncludeDeclaration: true}})
			if err != nil || len(references) != len(test.references) {
				t.Fatalf("references = %#v, %v", references, err)
			}
			for index, expected := range test.references {
				if references[index].Range != expected {
					t.Errorf("reference %d = %#v, want %#v", index, references[index].Range, expected)
				}
			}
			highlights, err := instance.DocumentHighlight(context.Background(), &protocol.DocumentHighlightParams{TextDocumentPositionParams: params})
			if err != nil || len(highlights) != len(test.references) {
				t.Fatalf("highlights = %#v, %v", highlights, err)
			}
		})
	}
}

func TestVim9InterfaceMemberDeclarationAndDefinition(t *testing.T) {
	source := "vim9script\ninterface Face\n  def Run(value: number): number\nendinterface\nclass Impl implements Face\n  def new()\n  enddef\n  def Run(value: number): number\n    return value\n  enddef\nendclass\nvar face: Face = Impl.new()\necho face.Run(1)\nvar impl = Impl.new()\necho impl.Run(2)\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	textDocument := protocol.TextDocumentIdentifier{URI: documentURI}
	for _, position := range []protocol.Position{{Line: 12, Character: 11}, {Line: 14, Character: 11}} {
		params := protocol.TextDocumentPositionParams{TextDocument: textDocument, Position: position}
		definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: params})
		if err != nil || len(definition.(protocol.LocationSlice)) != 1 || definition.(protocol.LocationSlice)[0].Range != navigationRange(7, 6, 9) {
			t.Fatalf("interface definition = %#v, %v", definition, err)
		}
		declaration, err := instance.Declaration(context.Background(), &protocol.DeclarationParams{TextDocumentPositionParams: params})
		if err != nil || len(declaration.(protocol.LocationSlice)) != 1 || declaration.(protocol.LocationSlice)[0].Range != navigationRange(2, 6, 9) {
			t.Fatalf("interface declaration = %#v, %v", declaration, err)
		}
		references, err := instance.References(context.Background(), &protocol.ReferenceParams{TextDocumentPositionParams: params, Context: protocol.ReferenceContext{IncludeDeclaration: true}})
		want := []protocol.Range{navigationRange(2, 6, 9), navigationRange(7, 6, 9), navigationRange(12, 10, 13), navigationRange(14, 10, 13)}
		if err != nil || len(references) != len(want) {
			t.Fatalf("interface references = %#v, %v", references, err)
		}
		for index := range want {
			if references[index].Range != want[index] {
				t.Errorf("reference %d = %#v, want %#v", index, references[index].Range, want[index])
			}
		}
	}
}

func TestVim9AbstractMemberDeclarationAndDefinition(t *testing.T) {
	source := "vim9script\nabstract class Base\n  abstract def Draw(): string\nendclass\nclass Shape extends Base\n  def Draw(): string\n    return 'shape'\n  enddef\nendclass\nvar shape = Shape.new()\necho shape.Draw()\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	params := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 10, Character: 13}}
	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: params})
	if err != nil || len(definition.(protocol.LocationSlice)) != 1 || definition.(protocol.LocationSlice)[0].Range != navigationRange(5, 6, 10) {
		t.Fatalf("abstract definition = %#v, %v", definition, err)
	}
	declaration, err := instance.Declaration(context.Background(), &protocol.DeclarationParams{TextDocumentPositionParams: params})
	if err != nil || len(declaration.(protocol.LocationSlice)) != 1 || declaration.(protocol.LocationSlice)[0].Range != navigationRange(2, 15, 19) {
		t.Fatalf("abstract declaration = %#v, %v", declaration, err)
	}
	references, err := instance.References(context.Background(), &protocol.ReferenceParams{TextDocumentPositionParams: params, Context: protocol.ReferenceContext{IncludeDeclaration: true}})
	want := []protocol.Range{navigationRange(2, 15, 19), navigationRange(5, 6, 10), navigationRange(10, 11, 15)}
	if err != nil || len(references) != len(want) {
		t.Fatalf("abstract references = %#v, %v", references, err)
	}
	for index := range want {
		if references[index].Range != want[index] {
			t.Errorf("reference %d = %#v, want %#v", index, references[index].Range, want[index])
		}
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
	instance.testHooks.beforeWorkspaceIdentityCheck = func() { checks++ }
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
	configFile := instance.configFileRoleForURI(documentURI.String())
	instance.parsed[documentURI.String()] = parsedDocument{contentID: snapshot.ContentID(), configFile: configFile, file: cached}

	document, err := instance.navigationAt(context.Background(), documentURI.String(), protocol.Position{Line: 2, Character: 6})
	if err != nil || document == nil || document.analysis.File != cached {
		t.Fatalf("navigation analysis = %#v, error = %v", document, err)
	}

	stale := syntax.Parse("vim9script\necho missing\n")
	instance.parsed[documentURI.String()] = parsedDocument{contentID: snapshot.ContentID(), configFile: configFile, file: stale}
	document, err = instance.navigationAt(context.Background(), documentURI.String(), protocol.Position{Line: 2, Character: 6})
	if err != nil || document == nil || document.analysis.File == stale || document.declaration == nil {
		t.Fatalf("stale navigation analysis = %#v, error = %v", document, err)
	}
}

func TestHoverShowsVariableTypes(t *testing.T) {
	for _, test := range []struct {
		name, source, want string
	}{
		{name: "unknown", source: "vim9script\nvar value = UnknownCall()\necho value\n", want: "unknown"},
		{name: "explicit any", source: "vim9script\nvar value: any\necho value\n", want: "any"},
		{name: "null literal", source: "vim9script\nvar value = null\necho value\n", want: "null"},
		{name: "v:null", source: "vim9script\nvar value = v:null\necho value\n", want: "null"},
		{name: "v:none", source: "vim9script\nvar value = v:none\necho value\n", want: "none"},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
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
			if !ok || content.Value != "name: value\nkind: variable\ntype: "+test.want {
				t.Fatalf("hover = %#v", hover)
			}
		})
	}
}

func TestHoverShowsVim9HeredocListStringType(t *testing.T) {
	source := "vim9script\nconst call_function =<< trim CALL_FUNCTION_END\n  function! coc#api#call(method, args) abort\n  endfunction\nCALL_FUNCTION_END\necho call_function\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	hover, err := instance.Hover(context.Background(), &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Position:     protocol.Position{Line: 5, Character: 7},
		},
	})
	if err != nil || hover == nil {
		t.Fatalf("hover = %#v, error = %v", hover, err)
	}
	content, ok := hover.Contents.(*protocol.MarkupContent)
	if !ok || content.Value != "name: call_function\nkind: constant\ntype: list<string>" {
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
	if !ok || content.Kind != protocol.MarkupKindPlainText || !strings.HasPrefix(content.Value, "name: argc\nkind: builtin function\ntype: number\n\nargc([{winid}])") || len(content.Value) > maxLanguageFeatureDocumentationBytes {
		t.Fatalf("builtin hover = %#v", hover)
	}
}

func TestHoverShowsPinnedOptionAndPredefinedVariableHelp(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\necho &number v:version\n")
	for _, test := range []struct {
		name      string
		character uint32
		prefix    string
		fragment  string
	}{
		{name: "option", character: 7, prefix: "name: number\nkind: option\ntype: bool", fragment: "Print the line number"},
		{name: "predefined variable", character: 15, prefix: "name: v:version\nkind: predefined variable\ntype: number", fragment: "Version number of Vim"},
	} {
		t.Run(test.name, func(t *testing.T) {
			hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: test.character},
			}})
			if err != nil || hover == nil {
				t.Fatalf("hover = %#v, %v", hover, err)
			}
			content, ok := hover.Contents.(*protocol.MarkupContent)
			if !ok || content.Kind != protocol.MarkupKindPlainText || !strings.HasPrefix(content.Value, test.prefix) || !strings.Contains(content.Value, test.fragment) || len(content.Value) > maxLanguageFeatureDocumentationBytes {
				t.Fatalf("hover content = %#v", hover.Contents)
			}
		})
	}
}

func TestHoverShowsOptionBuildRequirement(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\necho &autochdir &ballooneval\n")
	for _, test := range []struct {
		character uint32
		want      string
	}{
		{character: 8, want: "build requirement: +autochdir (defined(FEAT_AUTOCHDIR))"},
		{character: 20, want: "build requirement: +balloon_eval (defined(FEAT_BEVAL_GUI))"},
	} {
		hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: test.character},
		}})
		if err != nil || hover == nil {
			t.Fatalf("hover = %#v, %v", hover, err)
		}
		content, ok := hover.Contents.(*protocol.MarkupContent)
		if !ok || !strings.Contains(content.Value, test.want) {
			t.Fatalf("hover content = %#v, want %q", hover.Contents, test.want)
		}
	}
}

func TestOptionBuildRequirementFormatting(t *testing.T) {
	tests := map[string]string{
		"1":                                  "",
		"0":                                  "unavailable in Vim v9.2.1015",
		"defined(FEAT_AUTOCHDIR)":            "+autochdir (defined(FEAT_AUTOCHDIR))",
		"defined(FEAT_X) && defined(FEAT_Y)": "defined(FEAT_X) && defined(FEAT_Y)",
		"defined(MSWIN) || defined(FEAT_WAYLAND)": "defined(MSWIN) || defined(FEAT_WAYLAND)",
	}
	for condition, want := range tests {
		var features []string
		if condition == "defined(FEAT_AUTOCHDIR)" {
			features = []string{"autochdir"}
		}
		if got := optionBuildRequirement(condition, features); got != want {
			t.Errorf("optionBuildRequirement(%q) = %q, want %q", condition, got, want)
		}
	}
}

func TestHoverShowsPinnedExCommandHelpForAbbreviation(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "ec 'value'\n")
	hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Character: 1},
	}})
	if err != nil || hover == nil {
		t.Fatalf("hover = %#v, %v", hover, err)
	}
	content, ok := hover.Contents.(*protocol.MarkupContent)
	if !ok || content.Kind != protocol.MarkupKindPlainText || !strings.HasPrefix(content.Value, "name: echo\nkind: Ex command") || !strings.Contains(content.Value, "Echoes each {expr1}") || len(content.Value) > maxLanguageFeatureDocumentationBytes {
		t.Fatalf("hover content = %#v", hover.Contents)
	}
}

func TestHoverShowsHasFeatureAndExpandSpecialDocs(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\necho has('gui_running')\necho expand('<cfile>')\n")
	for _, test := range []struct {
		name      string
		line      uint32
		character uint32
		prefix    string
		fragment  string
	}{
		{name: "has feature", line: 1, character: 15, prefix: "name: gui_running\nkind: has() feature", fragment: "Whether the Vim GUI is running"},
		{name: "expand special", line: 2, character: 16, prefix: "name: <cfile>\nkind: expand() special", fragment: "File name under the cursor"},
	} {
		t.Run(test.name, func(t *testing.T) {
			hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: test.line, Character: test.character},
			}})
			if err != nil || hover == nil {
				t.Fatalf("hover = %#v, %v", hover, err)
			}
			content, ok := hover.Contents.(*protocol.MarkupContent)
			if !ok || content.Kind != protocol.MarkupKindPlainText || !strings.HasPrefix(content.Value, test.prefix) || !strings.Contains(content.Value, test.fragment) || len(content.Value) > maxLanguageFeatureDocumentationBytes {
				t.Fatalf("hover content = %#v", hover.Contents)
			}
		})
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
	if !ok || content.Value != "name: Run\nkind: function\nsignature: Run(): number\ntype: func(): number" || hover.Range == nil || *hover.Range != navigationRange(2, 17, 20) {
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

func TestImportedVim9AggregateMemberDefinition(t *testing.T) {
	source := "vim9script\nimport './lib.vim' as lib\nvar box: lib.Box\necho box.Resize(1, 2)\necho lib.Box.new(1)\necho lib.Box.Build('x')\necho lib.Make().Resize(3, 4)\necho lib.Color.Red\n"
	instance, documentURI, targetURI := openWorkspaceFeatureRetryDocument(t, source)
	targetPath, ok := workspaceURIPath(targetURI)
	if !ok {
		t.Fatalf("workspace path for %s", targetURI)
	}
	targetURI = uri.File(targetPath)
	tests := []struct {
		name     string
		position protocol.Position
		want     protocol.Range
	}{
		{name: "typed inherited object method", position: protocol.Position{Line: 3, Character: 12}, want: navigationRange(7, 6, 12)},
		{name: "constructor", position: protocol.Position{Line: 4, Character: 15}, want: navigationRange(5, 6, 9)},
		{name: "static method", position: protocol.Position{Line: 5, Character: 15}, want: navigationRange(10, 13, 18)},
		{name: "factory result method", position: protocol.Position{Line: 6, Character: 20}, want: navigationRange(7, 6, 12)},
		{name: "enum value", position: protocol.Position{Line: 7, Character: 16}, want: navigationRange(20, 2, 5)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: test.position}
			definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: params})
			if err != nil {
				t.Fatal(err)
			}
			locations := definition.(protocol.LocationSlice)
			if len(locations) != 1 || locations[0].URI != targetURI || locations[0].Range != test.want {
				t.Fatalf("imported member definition = %#v", definition)
			}
			declaration, err := instance.Declaration(context.Background(), &protocol.DeclarationParams{TextDocumentPositionParams: params})
			if err != nil || len(declaration.(protocol.LocationSlice)) != 1 || declaration.(protocol.LocationSlice)[0] != locations[0] {
				t.Fatalf("imported member declaration = %#v, %v", declaration, err)
			}
		})
	}
	mainPath, ok := workspaceURIPath(documentURI)
	if !ok {
		t.Fatalf("workspace path for %s", documentURI)
	}
	otherSource := "vim9script\nimport './lib.vim' as lib\nvar other: lib.Box\necho other.Resize(5, 6)\n"
	otherPath := writeWorkspaceFile(t, filepath.Dir(mainPath), "other.vim", otherSource)
	if err := instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{Changes: []protocol.FileEvent{{URI: uri.File(otherPath), Type: protocol.FileChangeTypeCreated}}}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	params := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 3, Character: 12}}
	references, err := instance.References(context.Background(), &protocol.ReferenceParams{TextDocumentPositionParams: params, Context: protocol.ReferenceContext{IncludeDeclaration: true}})
	wantReferences := []protocol.Location{
		{URI: targetURI, Range: navigationRange(7, 6, 12)},
		{URI: uri.File(mainPath), Range: navigationRange(3, 9, 15)},
		{URI: uri.File(mainPath), Range: navigationRange(6, 16, 22)},
		{URI: canonicalTestURI(t, otherPath), Range: navigationRange(3, 11, 17)},
	}
	if err != nil || len(references) != len(wantReferences) {
		t.Fatalf("imported member references = %#v, %v", references, err)
	}
	for index := range wantReferences {
		if references[index] != wantReferences[index] {
			t.Errorf("reference %d = %#v, want %#v", index, references[index], wantReferences[index])
		}
	}
	highlights, err := instance.DocumentHighlight(context.Background(), &protocol.DocumentHighlightParams{TextDocumentPositionParams: params})
	if err != nil || len(highlights) != 2 || highlights[0].Range != navigationRange(3, 9, 15) || highlights[1].Range != navigationRange(6, 16, 22) {
		t.Fatalf("imported member highlights = %#v, %v", highlights, err)
	}
	edit, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: params, NewName: "Scale"})
	if err != nil || edit == nil || len(edit.DocumentChanges) != 3 {
		t.Fatalf("imported member rename = %#v, %v", edit, err)
	}
	version := int32(1)
	wantEdits := map[uri.URI]struct {
		version *int32
		ranges  []protocol.Range
	}{
		targetURI:                      {ranges: []protocol.Range{navigationRange(7, 6, 12)}},
		documentURI:                    {version: &version, ranges: []protocol.Range{navigationRange(3, 9, 15), navigationRange(6, 16, 22)}},
		canonicalTestURI(t, otherPath): {ranges: []protocol.Range{navigationRange(3, 11, 17)}},
	}
	for _, change := range edit.DocumentChanges {
		documentEdit := change.(*protocol.TextDocumentEdit)
		expected, ok := wantEdits[documentEdit.TextDocument.URI]
		if !ok || len(documentEdit.Edits) != len(expected.ranges) || (documentEdit.TextDocument.Version == nil) != (expected.version == nil) {
			t.Fatalf("unexpected member document edit = %#v", documentEdit)
		}
		if expected.version != nil && *documentEdit.TextDocument.Version != *expected.version {
			t.Errorf("edit version = %#v, want %#v", documentEdit.TextDocument.Version, expected.version)
		}
		for index, rangeValue := range expected.ranges {
			textEdit := documentEdit.Edits[index].(*protocol.TextEdit)
			if textEdit.Range != rangeValue || textEdit.NewText != "Scale" {
				t.Errorf("member edit %d = %#v", index, textEdit)
			}
		}
		delete(wantEdits, documentEdit.TextDocument.URI)
	}
	if len(wantEdits) != 0 {
		t.Fatalf("missing member edits = %#v", wantEdits)
	}
	constructor := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 4, Character: 15}}
	if prepared, err := instance.PrepareRename(context.Background(), &protocol.PrepareRenameParams{TextDocumentPositionParams: constructor}); err != nil || prepared != nil {
		t.Fatalf("imported constructor prepare rename = %#v, %v", prepared, err)
	}
	if _, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: constructor, NewName: "Create"}); err == nil {
		t.Fatal("imported constructor rename succeeded")
	}
}

func TestImportedVim9AggregateMemberUsesOpenOverlay(t *testing.T) {
	source := "vim9script\nimport './lib.vim' as lib\nvar box: lib.Box\necho box.Resize(1)\n"
	instance, documentURI, targetURI := openWorkspaceFeatureRetryDocument(t, source)
	overlay := "vim9script\nexport class Box\n\n  def Resize(width: number): number\n    return width\n  enddef\nendclass\n"
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: targetURI, Version: 2, Text: overlay}}); err != nil {
		t.Fatal(err)
	}
	params := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 3, Character: 12}}
	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: params})
	if err != nil {
		t.Fatal(err)
	}
	locations := definition.(protocol.LocationSlice)
	if len(locations) != 1 || !sameNavigationURI(locations[0].URI, targetURI) || locations[0].Range != navigationRange(3, 6, 12) {
		t.Fatalf("overlay member definition = %#v", definition)
	}
}

func TestWorkspaceIdentityImportedMemberNavigation(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(*Server, protocol.TextDocumentPositionParams) (int, error)
	}{
		{name: "references", run: func(instance *Server, position protocol.TextDocumentPositionParams) (int, error) {
			result, err := instance.References(context.Background(), &protocol.ReferenceParams{TextDocumentPositionParams: position, Context: protocol.ReferenceContext{IncludeDeclaration: true}})
			return len(result), err
		}},
		{name: "document highlight", run: func(instance *Server, position protocol.TextDocumentPositionParams) (int, error) {
			result, err := instance.DocumentHighlight(context.Background(), &protocol.DocumentHighlightParams{TextDocumentPositionParams: position})
			return len(result), err
		}},
	} {
		t.Run(test.name+" retries", func(t *testing.T) {
			instance, documentURI, _ := openWorkspaceFeatureRetryDocument(t, "vim9script\nimport './lib.vim' as lib\nvar box: lib.Box\necho box.Resize(1)\n")
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
			count, err := test.run(instance, position)
			if err != nil || checks != 2 || count == 0 {
				t.Fatalf("result count=%d checks=%d error=%v", count, checks, err)
			}
		})
		t.Run(test.name+" rejects second stale result", func(t *testing.T) {
			instance, documentURI, _ := openWorkspaceFeatureRetryDocument(t, "vim9script\nimport './lib.vim' as lib\nvar box: lib.Box\necho box.Resize(1)\n")
			position := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 3, Character: 12}}
			checks := 0
			instance.testHooks.beforeWorkspaceIdentityCheck = func() {
				checks++
				instance.workspaceMu.Lock()
				instance.workspaceRevision++
				instance.workspaceMu.Unlock()
			}
			count, err := test.run(instance, position)
			if !errors.Is(err, protocol.ErrContentModified) || checks != 2 || count != 0 {
				t.Fatalf("result count=%d checks=%d error=%v", count, checks, err)
			}
		})
	}
}

func TestCrossFileLegacyAutoloadDefinitionAndReferences(t *testing.T) {
	root := t.TempDir()
	targetPath := writeWorkspaceFile(t, root, filepath.Join("autoload", "foo", "bar.vim"), "function g:foo#bar#Run()\nendfunction\n")
	mainPath := writeWorkspaceFile(t, root, "plugin.vim", "call foo#bar#Run()\n")
	otherPath := writeWorkspaceFile(t, root, "other.vim", "let value = g:foo#bar#Run()\n")
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
	if len(locations) != 1 || locations[0].URI != targetURI || locations[0].Range != navigationRange(0, 9, 22) {
		t.Fatalf("autoload definition = %#v", definition)
	}
	declaration, err := instance.Declaration(context.Background(), &protocol.DeclarationParams{TextDocumentPositionParams: position})
	if err != nil || len(declaration.(protocol.LocationSlice)) != 1 || declaration.(protocol.LocationSlice)[0] != locations[0] {
		t.Fatalf("autoload declaration = %#v, %v", declaration, err)
	}
	references, err := instance.References(context.Background(), &protocol.ReferenceParams{
		TextDocumentPositionParams: position,
		Context:                    protocol.ReferenceContext{IncludeDeclaration: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []protocol.Location{
		{URI: targetURI, Range: navigationRange(0, 9, 22)},
		{URI: otherURI, Range: navigationRange(0, 12, 25)},
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
	highlights, err := instance.DocumentHighlight(context.Background(), &protocol.DocumentHighlightParams{TextDocumentPositionParams: position})
	if err != nil || len(highlights) != 1 || highlights[0].Range != navigationRange(0, 5, 16) {
		t.Fatalf("autoload highlights = %#v, %v", highlights, err)
	}
}

func TestCrossFileLegacyGlobalFunctionCompletionDefinitionAndHover(t *testing.T) {
	root := t.TempDir()
	targetSource := "\" Run the indexed task.\nfunction GlobalRun(arg, ...)\nendfunction\n"
	targetPath := writeWorkspaceFile(t, root, "functions.vim", targetSource)
	source := "call GlobalR\ncall GlobalRun('x')\n"
	mainPath := writeWorkspaceFile(t, root, "plugin.vim", source)
	writeWorkspaceFile(t, root, "other.vim", "call GlobalRun('y')\n")
	instance := initializeWorkspaceServer(t, root)
	mainURI := uri.File(mainPath)
	instance.documents.Open(mainURI.String(), 1, source)

	completion, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: mainURI}, Position: protocol.Position{Line: 0, Character: 12},
	}})
	if err != nil {
		t.Fatal(err)
	}
	items := completionItems(t, completion)
	if len(items) != 1 || items[0].Label != "GlobalRun" {
		t.Fatalf("legacy global function completion = %#v", items)
	}
	if detail, ok := items[0].Detail.Get(); !ok || detail != "GlobalRun(arg, ...)" {
		t.Fatalf("legacy global function detail = %#v", items[0])
	}

	position := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: mainURI}, Position: protocol.Position{Line: 1, Character: 9}}
	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: position})
	locations, ok := definition.(protocol.LocationSlice)
	if err != nil || !ok || len(locations) != 1 || locations[0].URI != canonicalTestURI(t, targetPath) || locations[0].Range != navigationRange(1, 9, 18) {
		t.Fatalf("legacy global function definition = %#v, %v", definition, err)
	}
	hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: position})
	if err != nil || hover == nil {
		t.Fatalf("legacy global function hover = %#v, %v", hover, err)
	}
	content, ok := hover.Contents.(*protocol.MarkupContent)
	if !ok || !strings.Contains(content.Value, "signature: GlobalRun(arg, ...)") || !strings.Contains(content.Value, "Run the indexed task.") {
		t.Fatalf("legacy global function hover content = %#v", hover.Contents)
	}
	references, err := instance.References(context.Background(), &protocol.ReferenceParams{TextDocumentPositionParams: position, Context: protocol.ReferenceContext{IncludeDeclaration: true}})
	if err != nil || len(references) != 3 {
		t.Fatalf("legacy global function references = %#v, %v", references, err)
	}
	targetURI := uri.File(targetPath)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: targetURI, Version: 1, Text: targetSource}}); err != nil {
		t.Fatal(err)
	}
	instance.replaceWorkspaceFile(targetURI.String(), syntax.Parse(targetSource))
	fromDeclaration, err := instance.References(context.Background(), &protocol.ReferenceParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: targetURI}, Position: protocol.Position{Line: 1, Character: 12},
	}, Context: protocol.ReferenceContext{IncludeDeclaration: true}})
	if err != nil || len(fromDeclaration) != 3 {
		t.Fatalf("references from open global function declaration = %#v, %v", fromDeclaration, err)
	}
	duplicatePath := writeWorkspaceFile(t, root, "duplicate.vim", "function GlobalRun()\nendfunction\n")
	if err := instance.workspaceIndex.Replace(duplicatePath, syntax.Parse("function GlobalRun()\nendfunction\n")); err != nil {
		t.Fatal(err)
	}
	definition, err = instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: position})
	if err != nil || len(definition.(protocol.LocationSlice)) != 0 {
		t.Fatalf("ambiguous global function definition = %#v, %v", definition, err)
	}
}

func TestCrossFileLegacyGlobalVariableDefinitionReferencesAndAmbiguity(t *testing.T) {
	root := t.TempDir()
	targetPath := writeWorkspaceFile(t, root, "globals.vim", "let g:WorkspaceValue = 1\n")
	mainPath := writeWorkspaceFile(t, root, "plugin.vim", "echo g:WorkspaceValue\n")
	otherPath := writeWorkspaceFile(t, root, "other.vim", "let copy = g:WorkspaceValue\n")
	instance := initializeWorkspaceServer(t, root)
	mainURI := uri.File(mainPath)
	instance.documents.Open(mainURI.String(), 1, "echo g:WorkspaceValue\n")
	position := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: mainURI}, Position: protocol.Position{Character: 10}}
	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: position})
	locations := definition.(protocol.LocationSlice)
	if err != nil || len(locations) != 1 || locations[0].URI != canonicalTestURI(t, targetPath) || locations[0].Range != navigationRange(0, 4, 20) {
		t.Fatalf("global variable definition = %#v, %v", definition, err)
	}
	references, err := instance.References(context.Background(), &protocol.ReferenceParams{TextDocumentPositionParams: position, Context: protocol.ReferenceContext{IncludeDeclaration: true}})
	if err != nil || len(references) != 3 {
		t.Fatalf("global variable references = %#v, %v", references, err)
	}
	wantURIs := []uri.URI{canonicalTestURI(t, targetPath), canonicalTestURI(t, mainPath), canonicalTestURI(t, otherPath)}
	slices.Sort(wantURIs)
	for index := range wantURIs {
		if references[index].URI != wantURIs[index] {
			t.Errorf("reference %d = %#v, want URI %s", index, references[index], wantURIs[index])
		}
	}
	targetURI := uri.File(targetPath)
	targetSource := "let g:WorkspaceValue = 1\n"
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: targetURI, Version: 1, Text: targetSource}}); err != nil {
		t.Fatal(err)
	}
	instance.replaceWorkspaceFile(targetURI.String(), syntax.Parse(targetSource))
	fromDeclaration, err := instance.References(context.Background(), &protocol.ReferenceParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: targetURI}, Position: protocol.Position{Character: 10},
	}, Context: protocol.ReferenceContext{IncludeDeclaration: true}})
	if err != nil || len(fromDeclaration) != 3 {
		t.Fatalf("references from open global declaration = %#v, %v", fromDeclaration, err)
	}
	duplicatePath := writeWorkspaceFile(t, root, "duplicate.vim", "let g:WorkspaceValue = 2\n")
	duplicate := syntax.Parse("let g:WorkspaceValue = 2\n")
	if err := instance.workspaceIndex.Replace(duplicatePath, duplicate); err != nil {
		t.Fatal(err)
	}
	definition, err = instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: position})
	if err != nil || len(definition.(protocol.LocationSlice)) != 0 {
		t.Fatalf("ambiguous global variable definition = %#v, %v", definition, err)
	}
}

func TestCrossFileVim9AutoloadExportUsesImportAndLegacyNames(t *testing.T) {
	root := t.TempDir()
	targetPath := writeWorkspaceFile(t, root, filepath.Join("autoload", "api.vim"), "vim9script\n# Return the cached result.\nexport def Run(arg: string = 'ok'): string\n  return arg\nenddef\n")
	legacyPath := writeWorkspaceFile(t, root, "legacy.vim", "call api#Run()\n")
	importPath := writeWorkspaceFile(t, root, "plugin.vim", "vim9script\nimport autoload 'api.vim'\necho api.Run()\n")
	vim9Path := writeWorkspaceFile(t, root, "direct.vim", "vim9script\necho api#Run()\n")
	targetURI := canonicalTestURI(t, targetPath)
	instance := initializeWorkspaceServer(t, root)
	if err := os.Remove(targetPath); err != nil {
		t.Fatal(err)
	}
	legacyURI := uri.File(legacyPath)
	indexedLegacyURI := canonicalTestURI(t, legacyPath)
	importURI := canonicalTestURI(t, importPath)
	vim9URI := canonicalTestURI(t, vim9Path)
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
	if len(locations) != 1 || locations[0].URI != targetURI || locations[0].Range != navigationRange(2, 11, 14) {
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
		{URI: targetURI, Range: navigationRange(2, 11, 14)},
		{URI: indexedLegacyURI, Range: navigationRange(0, 5, 12)},
		{URI: importURI, Range: navigationRange(2, 9, 12)},
		{URI: vim9URI, Range: navigationRange(1, 5, 12)},
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
	hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: position})
	if err != nil || hover == nil {
		t.Fatalf("Vim9 autoload hover = %#v, %v", hover, err)
	}
	content, ok := hover.Contents.(*protocol.MarkupContent)
	if !ok || !strings.Contains(content.Value, "signature: api#Run(arg: string = 'ok'): string") || !strings.Contains(content.Value, "Return the cached result.") {
		t.Fatalf("Vim9 autoload hover content = %#v", hover.Contents)
	}
	completionSource := "vim9script\necho api#R\n"
	completionPath := writeWorkspaceFile(t, root, "completion.vim", completionSource)
	completionURI := uri.File(completionPath)
	instance.documents.Open(completionURI.String(), 1, completionSource)
	completion, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: completionURI}, Position: protocol.Position{Line: 1, Character: 10},
	}})
	if err != nil {
		t.Fatal(err)
	}
	items := completionItems(t, completion)
	if len(items) != 1 || items[0].Label != "api#Run" {
		t.Fatalf("Vim9 autoload completion = %#v", items)
	}
	if detail, ok := items[0].Detail.Get(); !ok || detail != "api#Run(arg: string = 'ok'): string" {
		t.Fatalf("Vim9 autoload completion detail = %#v", items[0])
	}
	if documentation, ok := items[0].Documentation.(protocol.String); !ok || string(documentation) != "Return the cached result." {
		t.Fatalf("Vim9 autoload completion documentation = %#v", items[0].Documentation)
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
	target, ok := document.workspaceTargetInState(instance.captureWorkspaceNavigationState())
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
			instance.testHooks.beforeWorkspaceIdentityCheck = func() {
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
			instance.testHooks.beforeWorkspaceIdentityCheck = func() {
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
	instance.testHooks.beforeWorkspaceIdentityCheck = func() {
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

func TestWorkspaceIdentityDocumentLinkRetry(t *testing.T) {
	instance, documentURI, _ := openWorkspaceFeatureRetryDocument(t, "vim9script\nimport './lib.vim' as lib\n")
	checks := 0
	instance.testHooks.beforeWorkspaceIdentityCheck = func() {
		checks++
		if checks == 1 {
			instance.workspaceMu.Lock()
			instance.workspaceRevision++
			instance.workspaceMu.Unlock()
		}
	}
	links, err := instance.DocumentLink(context.Background(), &protocol.DocumentLinkParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil || len(links) != 1 || checks != 2 {
		t.Fatalf("links=%#v checks=%d error=%v", links, checks, err)
	}
}

func TestWorkspaceIdentityDocumentLinkDropsSecondStaleMiss(t *testing.T) {
	instance, documentURI, _ := openWorkspaceFeatureRetryDocument(t, "vim9script\nvar path = './lib.vim'\nimport path as lib\n")
	checks := 0
	instance.testHooks.beforeWorkspaceIdentityCheck = func() {
		checks++
		instance.workspaceMu.Lock()
		instance.workspaceRevision++
		instance.workspaceMu.Unlock()
	}
	links, err := instance.DocumentLink(context.Background(), &protocol.DocumentLinkParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if !errors.Is(err, protocol.ErrContentModified) || links != nil || checks != 2 {
		t.Fatalf("links=%#v checks=%d error=%v", links, checks, err)
	}
}

func TestWorkspaceIdentityCompletionRetry(t *testing.T) {
	instance, documentURI, _ := openWorkspaceFeatureRetryDocument(t, "vim9script\nimport './lib.vim' as lib\necho lib.\n")
	params := &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 9},
	}}
	checks := 0
	instance.testHooks.beforeWorkspaceIdentityCheck = func() {
		checks++
		if checks == 1 {
			instance.workspaceMu.Lock()
			instance.workspaceRevision++
			instance.workspaceMu.Unlock()
		}
	}
	result, err := instance.Completion(context.Background(), params)
	items := completionItems(t, result)
	if err != nil || !hasCompletionLabel(items, "Run") || checks != 2 {
		t.Fatalf("completion=%#v checks=%d error=%v", result, checks, err)
	}
}

func TestWorkspaceIdentityCompletionDropsSecondStaleMiss(t *testing.T) {
	instance, documentURI, _ := openWorkspaceFeatureRetryDocument(t, "vim9script\nvar path = './lib.vim'\nimport path as lib\necho lib.\n")
	checks := 0
	instance.testHooks.beforeWorkspaceIdentityCheck = func() {
		checks++
		instance.workspaceMu.Lock()
		instance.workspaceRevision++
		instance.workspaceMu.Unlock()
	}
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 3, Character: 9},
	}})
	if !errors.Is(err, protocol.ErrContentModified) || result != nil || checks != 2 {
		t.Fatalf("completion=%#v checks=%d error=%v", result, checks, err)
	}
}

func TestImportCompletionOpenTargetStaleIsImmediate(t *testing.T) {
	instance, documentURI, targetURI := openWorkspaceFeatureRetryDocument(t, "vim9script\nimport './lib.vim' as lib\necho lib.\n")
	instance.documents.Open(targetURI.String(), 1, "vim9script\nexport def Run()\nenddef\n")
	checks := 0
	instance.testHooks.beforeWorkspaceIdentityCheck = func() {
		checks++
		instance.documents.Open(targetURI.String(), 2, "vim9script\nexport def Changed()\nenddef\n")
	}
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 9},
	}})
	if !errors.Is(err, protocol.ErrContentModified) || result != nil || checks != 1 {
		t.Fatalf("completion=%#v checks=%d error=%v", result, checks, err)
	}
}

func TestExpressionCompletionChecksWorkspaceFunctionIndexIdentity(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar value = 1\necho value\n")
	checks := 0
	instance.testHooks.beforeWorkspaceIdentityCheck = func() { checks++ }
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 10},
	}})
	if err != nil || result == nil || checks != 1 {
		t.Fatalf("completion=%#v checks=%d error=%v", result, checks, err)
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
	canonicalMain, ok := workspaceURIPath(documentURI)
	if !ok {
		t.Fatalf("workspace path for %s", documentURI)
	}
	waitForImportGraph(t, instance, func(graph workspace.ImportGraphSnapshot) bool {
		return graph.Ready() && graph.Has(canonicalMain)
	})
	return instance, documentURI, protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: uint32(strings.Count(source[:strings.Index(source, "Run")], "\n")), Character: 9},
	}
}

func openWorkspaceFeatureRetryDocument(t *testing.T, source string) (*Server, uri.URI, uri.URI) {
	t.Helper()
	root := t.TempDir()
	libPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Run(): number\n  return 1\nenddef\nexport class Box\n  def new(value: number)\n  enddef\n  def Resize(width: number, height: number = 1): number\n    return width * height\n  enddef\n  static def Build(name: string): Box\n    return Box.new(1)\n  enddef\n  static def _Hidden()\n  enddef\nendclass\nexport def Make(): Box\n  return Box.new(1)\nenddef\nexport enum Color\n  Red,\n  Green\nendenum\n")
	mainPath := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := uri.File(mainPath)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source}}); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceSymbols(t, instance, "Run", 1)
	mainPath, ok := workspaceURIPath(documentURI)
	if !ok {
		t.Fatalf("workspace path for %s", documentURI)
	}
	waitForImportGraph(t, instance, func(graph workspace.ImportGraphSnapshot) bool {
		return graph.Ready() && graph.Has(mainPath)
	})
	return instance, documentURI, uri.File(libPath)
}

func openNavigationDocument(t *testing.T, encoding text.Encoding, source string) (*Server, uri.URI) {
	t.Helper()
	instance := New(nil, nil, io.Discard)
	instance.encoding = encoding
	documentURI := uri.File(mustWorkspaceCanonicalPath(t, filepath.Join(t.TempDir(), "navigation.vim")))
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
