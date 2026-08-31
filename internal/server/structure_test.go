package server

import (
	"context"
	"errors"
	"io"
	"testing"

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
