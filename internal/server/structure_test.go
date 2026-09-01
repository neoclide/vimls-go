package server

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestStructureCapabilitiesAndMethods(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	result, err := instance.Initialize(context.Background(), &protocol.InitializeParams{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Capabilities.FoldingRangeProvider == nil || result.Capabilities.SelectionRangeProvider == nil {
		t.Fatalf("structure capabilities = %#v", result.Capabilities)
	}
	for _, method := range []string{protocol.MethodTextDocumentFoldingRange, protocol.MethodTextDocumentSelectionRange} {
		if !implementedMethod(method) {
			t.Errorf("method %q is not implemented", method)
		}
	}
}

func TestFoldingRangesUseBlocksAndBodies(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\ndef Demo()\n  if true\n    var value =<< trim END\n      body\n    END\n  endif\nenddef\n")
	result, err := instance.FoldingRanges(context.Background(), &protocol.FoldingRangeParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("folding ranges = %#v", result)
	}
	for index := 1; index < len(result); index++ {
		if result[index-1].StartLine > result[index].StartLine || (result[index-1].StartLine == result[index].StartLine && result[index-1].EndLine > result[index].EndLine) {
			t.Fatalf("folding ranges are not source ordered = %#v", result)
		}
	}
	wanted := map[[2]uint32]bool{{1, 7}: true, {2, 6}: true, {3, 5}: true}
	for _, fold := range result {
		delete(wanted, [2]uint32{fold.StartLine, fold.EndLine})
		if fold.StartLine == fold.EndLine || fold.StartCharacter != nil || fold.EndCharacter != nil {
			t.Fatalf("invalid line-only fold = %#v", fold)
		}
	}
	if len(wanted) != 0 {
		t.Fatalf("missing folds %v in %#v", wanted, result)
	}
}

func TestIncompleteDeclarationStructuresRetainHierarchyAndRanges(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		root       string
		children   []string
		position   protocol.Position
		foldStart  uint32
		foldEnd    uint32
		grandchild string
	}{
		{name: "legacy function", source: "function! Legacy()\n  let value = 1\n", root: "Legacy", children: []string{"value"}, position: protocol.Position{Line: 1, Character: 6}, foldEnd: 1},
		{name: "class", source: "vim9script\nclass Box\n  def Run()\n    var value = 1\n", root: "Box", children: []string{"Run"}, grandchild: "value", position: protocol.Position{Line: 3, Character: 8}, foldStart: 1, foldEnd: 3},
		{name: "interface", source: "vim9script\ninterface Face\n  def Run()\n", root: "Face", children: []string{"Run"}, position: protocol.Position{Line: 2, Character: 7}, foldStart: 1, foldEnd: 2},
		{name: "enum", source: "vim9script\nenum Color\n  Red,\n  Green\n", root: "Color", children: []string{"Red", "Green"}, position: protocol.Position{Line: 3, Character: 3}, foldStart: 1, foldEnd: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			result, err := instance.DocumentSymbol(context.Background(), &protocol.DocumentSymbolParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
			if err != nil {
				t.Fatal(err)
			}
			symbols := result.(protocol.DocumentSymbolSlice)
			if len(symbols) != 1 || symbols[0].Name != test.root || symbols[0].Range.End != (protocol.Position{Line: uint32(strings.Count(test.source, "\n"))}) {
				t.Fatalf("incomplete symbols = %#v", symbols)
			}
			if len(symbols[0].Children) != len(test.children) {
				t.Fatalf("incomplete children = %#v", symbols[0].Children)
			}
			for index, name := range test.children {
				if symbols[0].Children[index].Name != name {
					t.Errorf("child %d = %#v, want %q", index, symbols[0].Children[index], name)
				}
			}
			if test.grandchild != "" && (len(symbols[0].Children[0].Children) != 1 || symbols[0].Children[0].Children[0].Name != test.grandchild) {
				t.Fatalf("incomplete grandchildren = %#v", symbols[0].Children[0].Children)
			}
			folds, err := instance.FoldingRanges(context.Background(), &protocol.FoldingRangeParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
			if err != nil {
				t.Fatal(err)
			}
			foundFold := false
			for _, fold := range folds {
				foundFold = foundFold || fold.StartLine == test.foldStart && fold.EndLine == test.foldEnd
			}
			if !foundFold {
				t.Fatalf("incomplete folds = %#v", folds)
			}
			selections, err := instance.SelectionRange(context.Background(), &protocol.SelectionRangeParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Positions: []protocol.Position{test.position},
			})
			if err != nil || len(selections) != 1 {
				t.Fatalf("incomplete selections = %#v, %v", selections, err)
			}
			containsRoot := false
			for current := &selections[0]; current != nil; current = current.Parent {
				containsRoot = containsRoot || current.Range == symbols[0].Range
			}
			if !containsRoot {
				t.Fatalf("selection chain does not contain root %#v: %#v", symbols[0].Range, selections[0])
			}
		})
	}
}

func TestSelectionRangesReturnChainsInRequestOrder(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar value = 1\necho value\n")
	result, err := instance.SelectionRange(context.Background(), &protocol.SelectionRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Positions:    []protocol.Position{{Line: 2, Character: 6}, {Line: 99, Character: 0}, {Line: 1, Character: 4}, {Line: 1, Character: 12}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 4 || result[1] != (protocol.SelectionRange{}) {
		t.Fatalf("selection ranges = %#v", result)
	}
	if result[0].Range != navigationRange(2, 5, 10) || result[0].Parent == nil {
		t.Fatalf("reference selection = %#v", result[0])
	}
	if result[0].Parent.Range.Start.Line > result[0].Range.Start.Line || result[0].Parent.Range.End.Line < result[0].Range.End.Line {
		t.Fatalf("selection parent does not contain child = %#v", result[0])
	}
	if result[2].Range != navigationRange(1, 4, 9) || result[2].Parent == nil {
		t.Fatalf("declaration selection = %#v", result[2])
	}
	if result[3].Range != navigationRange(1, 12, 13) || result[3].Parent == nil {
		t.Fatalf("initializer selection = %#v", result[3])
	}
}

func TestFoldingRangesTraverseEmbeddedCommandsAndTextBodies(t *testing.T) {
	t.Run("embedded commands", func(t *testing.T) {
		instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\ncommand Foo {\n  if true\n    echo 1\n  endif\n}\n")
		result, err := instance.FoldingRanges(context.Background(), &protocol.FoldingRangeParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result) != 2 || result[0].StartLine != 1 || result[0].EndLine != 5 || result[1].StartLine != 2 || result[1].EndLine != 4 {
			t.Fatalf("embedded folding ranges = %#v", result)
		}
	})

	t.Run("text body", func(t *testing.T) {
		instance, documentURI := openNavigationDocument(t, text.UTF16, "append\none\ntwo\n.\n")
		result, err := instance.FoldingRanges(context.Background(), &protocol.FoldingRangeParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result) != 1 || result[0].StartLine != 0 || result[0].EndLine != 3 {
			t.Fatalf("text body folding ranges = %#v", result)
		}
	})
}

func TestSelectionRangesUseNegotiatedPositionEncoding(t *testing.T) {
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
			instance, documentURI := openNavigationDocument(t, test.encoding, "vim9script\necho '𐐀' | echo value\n")
			result, err := instance.SelectionRange(context.Background(), &protocol.SelectionRangeParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
				Positions:    []protocol.Position{{Line: 1, Character: test.character}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result) != 1 || result[0].Range.Start != (protocol.Position{Line: 1, Character: test.character}) || result[0].Range.End.Character != test.character+5 {
				t.Fatalf("selection ranges = %#v", result)
			}
		})
	}
}

func TestStructureCancellationAndUnavailableDocument(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	if result, err := instance.FoldingRanges(context.Background(), &protocol.FoldingRangeParams{TextDocument: protocol.TextDocumentIdentifier{URI: uri.MustParse("file:///missing.vim")}}); err != nil || len(result) != 0 {
		t.Fatalf("missing folding = %#v, error = %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := instance.SelectionRange(ctx, &protocol.SelectionRangeParams{}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Fatalf("canceled selection error = %v", err)
	}

	current, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar value = 1\n")
	snapshot, _, _, err := current.structureDocument(context.Background(), documentURI.String())
	if err != nil || snapshot == nil {
		t.Fatalf("structure document snapshot = %#v, error = %v", snapshot, err)
	}
	version := int32(2)
	if _, _, err := current.documents.Change(documentURI.String(), version, text.UTF16, []text.Change{{Text: "vim9script\nvar changed = 1\n"}}); err != nil {
		t.Fatal(err)
	}
	if err := current.structureCurrent(context.Background(), snapshot); !errors.Is(err, protocol.ErrContentModified) {
		t.Fatalf("modified structure error = %v", err)
	}
}

func TestStructureCollectorRetainsEveryTypedChild(t *testing.T) {
	span := func(start, end int) syntax.Span { return syntax.Span{Start: start, End: end} }
	source := strings.Repeat("x", 200)
	typeNode := &syntax.Type{Span: span(30, 38), Arguments: []*syntax.Type{{Span: span(31, 34)}}, ReturnType: &syntax.Type{Span: span(35, 37)}}
	expression := &syntax.Expression{
		Span:       span(40, 70),
		Children:   []*syntax.Expression{{Span: span(41, 45)}},
		Parameters: []syntax.Parameter{{Name: span(46, 48), Target: &syntax.Expression{Span: span(49, 51)}, Type: typeNode, Default: &syntax.Expression{Span: span(52, 54)}}},
		ReturnType: typeNode,
		LambdaBody: &syntax.File{Source: source, Commands: []syntax.Command{{Span: span(55, 60), Name: span(55, 57)}}},
	}
	command := syntax.Command{
		Span: span(0, 180), Name: span(0, 3),
		Heredoc:     &syntax.Heredoc{Body: span(4, 8), EndMarker: span(8, 10)},
		TextBody:    &syntax.TextBody{Body: span(11, 15), EndMarker: span(15, 17)},
		Embedded:    &syntax.CommandList{Span: span(18, 29), Blocks: []syntax.Block{{Span: span(19, 28)}}, Commands: []syntax.Command{{Span: span(20, 27), Name: span(20, 22)}}},
		Declaration: &syntax.Declaration{Bindings: []syntax.Binding{{Name: span(71, 73), ParsedType: typeNode}}, Initializer: expression},
		For:         &syntax.ForLoop{Bindings: []syntax.Binding{{Name: span(74, 76), ParsedType: typeNode}}, Iterable: expression},
		Function:    &syntax.Function{Name: span(77, 80), TypeParameters: []syntax.TypeParameter{{Span: span(81, 82)}}, Parameters: expression.Parameters, ReturnType: typeNode},
		Aggregate:   &syntax.Aggregate{Name: span(83, 86), Extends: []syntax.Span{span(87, 90)}, Implements: []syntax.Span{span(91, 94)}},
		TypeAlias:   &syntax.TypeAlias{Name: span(95, 98), Type: typeNode},
		EnumValues:  []syntax.EnumValue{{Name: span(99, 102), Initializer: expression, Arguments: []*syntax.Expression{expression}}},
		Import:      &syntax.Import{Alias: span(103, 106), Path: expression},
		Mapping:     &syntax.Mapping{LHS: span(107, 110), RHS: span(111, 114), RHSExpression: expression},
		Expressions: []*syntax.Expression{expression}, Targets: []*syntax.Expression{expression},
	}
	file := &syntax.File{Source: source, Blocks: []syntax.Block{{Span: span(0, 180)}}, Commands: []syntax.Command{command}}
	collector := collectSpans(file)
	if len(collector.spans) < 30 || len(collector.folds) != 4 {
		t.Fatalf("spans=%d folds=%#v", len(collector.spans), collector.folds)
	}
	if chain := structureSelectionChain(collector.spans, 42, len(source)); len(chain) < 2 || chain[0] != span(41, 45) || chain[len(chain)-1] != span(0, len(source)) {
		t.Fatalf("selection chain = %#v", chain)
	}
}
