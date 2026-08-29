package server

import (
	"context"
	"io"
	"testing"

	"github.com/chemzqm/vimls-go/internal/text"
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

func openNavigationDocument(t *testing.T, encoding text.Encoding, source string) (*Server, uri.URI) {
	t.Helper()
	instance := New(nil, nil, io.Discard)
	instance.encoding = encoding
	documentURI := uri.MustParse("file:///navigation.vim")
	version := int32(1)
	instance.documents.Open(documentURI.String(), version, source)
	return instance, documentURI
}

func navigationRange(line, start, end uint32) protocol.Range {
	return protocol.Range{
		Start: protocol.Position{Line: line, Character: start},
		End:   protocol.Position{Line: line, Character: end},
	}
}
