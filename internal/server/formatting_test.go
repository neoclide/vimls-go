package server

import (
	"context"
	"errors"
	"io"
	"reflect"
	"sort"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestFormattingUsesNegotiatedEncodingAndPreservesOtherBytes(t *testing.T) {
	source := "\" 𐐀e\u0301\nvim9script\nif true\n echo 'x'  \nendif"
	want := "\" 𐐀e\u0301\nvim9script\nif true\n    echo 'x'  \nendif"
	for _, encoding := range []text.Encoding{text.UTF8, text.UTF16, text.UTF32} {
		t.Run(string(encoding), func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, encoding, source)
			keep := true
			edits, err := instance.Formatting(context.Background(), &protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
				Options: protocol.FormattingOptions{
					TabSize: 4, InsertSpaces: true,
					TrimTrailingWhitespace: &keep, InsertFinalNewline: &keep, TrimFinalNewlines: &keep,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(edits) != 1 || edits[0].Range.Start != (protocol.Position{Line: 3}) || edits[0].Range.End != (protocol.Position{Line: 3, Character: 1}) || edits[0].NewText != "    " {
				t.Fatalf("formatting edits = %#v", edits)
			}
			if got := applyProtocolEdits(t, source, encoding, edits); got != want {
				t.Fatalf("formatted source = %q, want %q", got, want)
			}
		})
	}
}

func TestFormattingRangeStartsAfterBOM(t *testing.T) {
	source := "\ufeff if true\n echo 1\nendif\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	edits, err := instance.Formatting(context.Background(), &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Options:      protocol.FormattingOptions{TabSize: 2, InsertSpaces: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 2 || edits[0].Range != (protocol.Range{Start: protocol.Position{Character: 1}, End: protocol.Position{Character: 2}}) || edits[0].NewText != "" {
		t.Fatalf("BOM formatting edits = %#v", edits)
	}
	if got := applyProtocolEdits(t, source, text.UTF16, edits); got != "\ufeffif true\n  echo 1\nendif\n" {
		t.Fatalf("BOM formatting result = %q", got)
	}
}

func TestRangeFormattingFiltersCompletePrefixes(t *testing.T) {
	source := "vim9script\nif true\n echo 1\nelse\necho 2\nendif"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	options := protocol.FormattingOptions{TabSize: 2, InsertSpaces: true}
	document, err := instance.Formatting(context.Background(), &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Options: options,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok := instance.documents.Snapshot(documentURI.String())
	if !ok {
		t.Fatal("missing snapshot")
	}
	end, err := snapshot.Position(snapshot.ByteLen(), text.UTF16)
	if err != nil {
		t.Fatal(err)
	}
	full, err := instance.RangeFormatting(context.Background(), &protocol.DocumentRangeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Range:        protocol.Range{End: protocol.Position{Line: uint32(end.Line), Character: uint32(end.Character)}},
		Options:      options,
	})
	if err != nil || !reflect.DeepEqual(full, document) {
		t.Fatalf("full range = %#v, %v; document = %#v", full, err, document)
	}

	tests := []struct {
		name       string
		rangeValue protocol.Range
		want       int
	}{
		{name: "complete existing prefix", rangeValue: protocol.Range{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 2, Character: 1}}, want: 1},
		{name: "start after prefix", rangeValue: protocol.Range{Start: protocol.Position{Line: 2, Character: 1}, End: protocol.Position{Line: 3}}, want: 0},
		{name: "exclusive line end", rangeValue: protocol.Range{Start: protocol.Position{Line: 1}, End: protocol.Position{Line: 2}}, want: 0},
		{name: "zero width", rangeValue: protocol.Range{Start: protocol.Position{Line: 4}, End: protocol.Position{Line: 4}}, want: 0},
		{name: "zero-width insertion before end", rangeValue: protocol.Range{Start: protocol.Position{Line: 4}, End: protocol.Position{Line: 5}}, want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := instance.RangeFormatting(context.Background(), &protocol.DocumentRangeFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Range: test.rangeValue, Options: options,
			})
			if err != nil || len(got) != test.want {
				t.Fatalf("range edits = %#v, %v, want %d", got, err, test.want)
			}
			for _, edit := range got {
				if protocolPositionLess(edit.Range.Start, test.rangeValue.Start) || protocolPositionLess(test.rangeValue.End, edit.Range.End) || edit.Range.Start == test.rangeValue.End {
					t.Fatalf("edit %#v escaped %#v", edit, test.rangeValue)
				}
			}
		})
	}
}

func TestFormattingRejectsInvalidParamsAndStaleSnapshot(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nif true\necho 1\nendif\n")
	if edits, err := instance.Formatting(context.Background(), &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Options: protocol.FormattingOptions{},
	}); !errors.Is(err, jsonrpc2.ErrInvalidParams) || edits != nil {
		t.Fatalf("zero tab size = %#v, %v", edits, err)
	}
	if edits, err := instance.RangeFormatting(context.Background(), &protocol.DocumentRangeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Range:        protocol.Range{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 1}},
		Options:      protocol.FormattingOptions{TabSize: 2},
	}); !errors.Is(err, jsonrpc2.ErrInvalidParams) || edits != nil {
		t.Fatalf("reversed range = %#v, %v", edits, err)
	}

	stale, staleURI := openNavigationDocument(t, text.UTF16, "vim9script\nif true\necho 1\nendif\n")
	stale.beforeParseSnapshotCacheMissForTest = func(*text.Snapshot) {
		stale.publishMu.Lock()
		stale.documents.Open(staleURI.String(), 2, "vim9script\n")
		stale.publishMu.Unlock()
	}
	edits, err := stale.Formatting(context.Background(), &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: staleURI}, Options: protocol.FormattingOptions{TabSize: 2, InsertSpaces: true},
	})
	if !errors.Is(err, protocol.ErrContentModified) || edits != nil {
		t.Fatalf("stale formatting = %#v, %v", edits, err)
	}
}

func TestFormattingUnknownDocumentReturnsEmptyEdits(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	edits, err := instance.Formatting(context.Background(), &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri.MustParse("file:///missing.vim")},
		Options:      protocol.FormattingOptions{TabSize: 4, InsertSpaces: true},
	})
	if err != nil || edits == nil || len(edits) != 0 {
		t.Fatalf("missing document formatting = %#v, %v", edits, err)
	}
}

func applyProtocolEdits(t *testing.T, source string, encoding text.Encoding, edits []protocol.TextEdit) string {
	t.Helper()
	snapshot := text.NewSnapshot("file:///format.vim", 1, nil, source)
	type replacement struct {
		start int
		end   int
		text  string
	}
	replacements := make([]replacement, 0, len(edits))
	for _, edit := range edits {
		start, startErr := snapshot.Offset(fromProtocolPosition(edit.Range.Start), encoding)
		end, endErr := snapshot.Offset(fromProtocolPosition(edit.Range.End), encoding)
		if startErr != nil || endErr != nil || end < start {
			t.Fatalf("invalid protocol edit %#v: %v, %v", edit, startErr, endErr)
		}
		replacements = append(replacements, replacement{start: start, end: end, text: edit.NewText})
	}
	sort.Slice(replacements, func(left, right int) bool { return replacements[left].start > replacements[right].start })
	for _, replacement := range replacements {
		source = source[:replacement.start] + replacement.text + source[replacement.end:]
	}
	return source
}

func protocolPositionLess(left, right protocol.Position) bool {
	return left.Line < right.Line || left.Line == right.Line && left.Character < right.Character
}
