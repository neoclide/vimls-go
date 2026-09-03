package server

import (
	"bytes"
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
	stale.testHooks.beforeParseSnapshotCacheMiss = func(*text.Snapshot) {
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

func TestRangesFormattingCapabilityGating(t *testing.T) {
	rangesSupportFalse := false
	rangesSupportTrue := true
	tests := []struct {
		name       string
		capability *protocol.DocumentRangeFormattingClientCapabilities
		wantWire   string
	}{
		{name: "no client range formatting capability", capability: nil, wantWire: `"documentRangeFormattingProvider":true`},
		{name: "client rangesSupport false", capability: &protocol.DocumentRangeFormattingClientCapabilities{RangesSupport: &rangesSupportFalse}, wantWire: `"documentRangeFormattingProvider":true`},
		{name: "client rangesSupport true", capability: &protocol.DocumentRangeFormattingClientCapabilities{RangesSupport: &rangesSupportTrue}, wantWire: `"documentRangeFormattingProvider":{"rangesSupport":true}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance := New(nil, nil, io.Discard)
			params := &protocol.InitializeParams{}
			if test.capability != nil {
				params.Capabilities.TextDocument = &protocol.TextDocumentClientCapabilities{RangeFormatting: test.capability}
			}
			result, err := instance.Initialize(context.Background(), params)
			if err != nil {
				t.Fatal(err)
			}
			provider := result.Capabilities.DocumentRangeFormattingProvider
			options, optionsOK := provider.(*protocol.DocumentRangeFormattingOptions)
			if test.capability != nil && test.capability.RangesSupport != nil && *test.capability.RangesSupport {
				if !optionsOK || options.RangesSupport == nil || !*options.RangesSupport {
					t.Fatalf("provider = %#v, want options with rangesSupport true", provider)
				}
			} else if _, ok := provider.(protocol.Boolean); !ok {
				t.Fatalf("provider = %#v, want plain true", provider)
			}
			encoded, err := protocol.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(encoded, []byte(test.wantWire)) {
				t.Fatalf("initialize result omitted %s: %s", test.wantWire, encoded)
			}
			if !implementedMethod(protocol.MethodTextDocumentRangesFormatting) {
				t.Fatalf("method %q is not implemented", protocol.MethodTextDocumentRangesFormatting)
			}
		})
	}
}

// multiBlockFormattingSource has two separate mis-indented regions: the body of
// the first if is one space too shallow and the body of the second has no
// indentation at all, so whole-document formatting edits both lines.
const multiBlockFormattingSource = "vim9script\nif true\n echo 1\nendif\nif false\necho 2\nendif\n"

// multiBlockFormattingEdits are the two candidate IndentEdits for
// multiBlockFormattingSource with two-space indentation, in document order.
var multiBlockFormattingEdits = []protocol.TextEdit{
	{Range: protocol.Range{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 2, Character: 1}}, NewText: "  "},
	{Range: protocol.Range{Start: protocol.Position{Line: 5}, End: protocol.Position{Line: 5}}, NewText: "  "},
}

func TestRangesFormattingSelectionUnion(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, multiBlockFormattingSource)
	options := protocol.FormattingOptions{TabSize: 2, InsertSpaces: true}
	textDocument := protocol.TextDocumentIdentifier{URI: documentURI}
	line2 := protocol.Range{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 3}}
	line5 := protocol.Range{Start: protocol.Position{Line: 5}, End: protocol.Position{Line: 6}}
	fullEdits, err := instance.Formatting(context.Background(), &protocol.DocumentFormattingParams{
		TextDocument: textDocument, Options: options,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fullEdits, multiBlockFormattingEdits) {
		t.Fatalf("whole-document edits = %#v, want %#v", fullEdits, multiBlockFormattingEdits)
	}
	tests := []struct {
		name   string
		ranges []protocol.Range
	}{
		{name: "two disjoint ranges", ranges: []protocol.Range{line2, line5}},
		{name: "out of order ranges", ranges: []protocol.Range{line5, line2}},
		{name: "duplicate ranges", ranges: []protocol.Range{line2, line2, line5}},
		{name: "overlapping ranges", ranges: []protocol.Range{
			{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 6}},
			{Start: protocol.Position{Line: 3}, End: protocol.Position{Line: 6}},
		}},
		{name: "adjacent ranges", ranges: []protocol.Range{
			line2,
			{Start: protocol.Position{Line: 3}, End: protocol.Position{Line: 4}},
			{Start: protocol.Position{Line: 4}, End: protocol.Position{Line: 5}},
			line5,
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := instance.RangesFormatting(context.Background(), &protocol.DocumentRangesFormattingParams{
				TextDocument: textDocument, Ranges: test.ranges, Options: options,
			})
			if err != nil || !reflect.DeepEqual(got, multiBlockFormattingEdits) {
				t.Fatalf("ranges edits = %#v, %v, want %#v", got, err, multiBlockFormattingEdits)
			}
		})
	}

	// The selection formed by the two mis-indented lines formats to the same
	// text as whole-document formatting of the same snapshot.
	rangesResult, err := instance.RangesFormatting(context.Background(), &protocol.DocumentRangesFormattingParams{
		TextDocument: textDocument, Ranges: []protocol.Range{line2, line5}, Options: options,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := applyProtocolEdits(t, multiBlockFormattingSource, text.UTF16, rangesResult), applyProtocolEdits(t, multiBlockFormattingSource, text.UTF16, fullEdits); got != want {
		t.Fatalf("ranges formatting text = %q, whole-document formatting text = %q", got, want)
	}
}

func TestRangesFormattingSameLineRanges(t *testing.T) {
	// One candidate IndentEdit replaces the three-space indent on line 2.
	source := "vim9script\nif true\n   echo 1\nendif\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	options := protocol.FormattingOptions{TabSize: 2, InsertSpaces: true}
	textDocument := protocol.TextDocumentIdentifier{URI: documentURI}
	candidate := protocol.TextEdit{Range: protocol.Range{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 2, Character: 3}}, NewText: "  "}

	// Two requested ranges on the same line that both fully contain the
	// candidate span still return it exactly once.
	got, err := instance.RangesFormatting(context.Background(), &protocol.DocumentRangesFormattingParams{
		TextDocument: textDocument,
		Ranges: []protocol.Range{
			{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 2, Character: 4}},
			{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 2, Character: 5}},
		},
		Options: options,
	})
	if err != nil || !reflect.DeepEqual(got, []protocol.TextEdit{candidate}) {
		t.Fatalf("same-line covering ranges = %#v, %v, want %#v", got, err, []protocol.TextEdit{candidate})
	}

	// Two same-line ranges that only jointly span the indent are not a union:
	// a candidate must lie fully inside one requested range, exactly like the
	// single-range filter which drops a range starting after the indent.
	partial := []protocol.Range{
		{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 2, Character: 2}},
		{Start: protocol.Position{Line: 2, Character: 2}, End: protocol.Position{Line: 2, Character: 6}},
	}
	got, err = instance.RangesFormatting(context.Background(), &protocol.DocumentRangesFormattingParams{
		TextDocument: textDocument, Ranges: partial, Options: options,
	})
	if err != nil || len(got) != 0 {
		t.Fatalf("partial same-line ranges = %#v, %v, want no edits", got, err)
	}
	single, err := instance.RangeFormatting(context.Background(), &protocol.DocumentRangeFormattingParams{
		TextDocument: textDocument, Range: partial[0], Options: options,
	})
	if err != nil || len(single) != 0 {
		t.Fatalf("single partial range = %#v, %v, want no edits", single, err)
	}
}

func TestRangesFormattingEmptyInvalidAndUnknown(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nif true\necho 1\nendif\n")
	textDocument := protocol.TextDocumentIdentifier{URI: documentURI}
	options := protocol.FormattingOptions{TabSize: 2, InsertSpaces: true}
	validRange := protocol.Range{Start: protocol.Position{Line: 1}, End: protocol.Position{Line: 3}}

	if edits, err := instance.RangesFormatting(context.Background(), nil); !errors.Is(err, jsonrpc2.ErrInvalidParams) || edits != nil {
		t.Fatalf("nil params = %#v, %v", edits, err)
	}
	if edits, err := instance.RangesFormatting(context.Background(), &protocol.DocumentRangesFormattingParams{
		TextDocument: textDocument, Ranges: []protocol.Range{validRange}, Options: protocol.FormattingOptions{},
	}); !errors.Is(err, jsonrpc2.ErrInvalidParams) || edits != nil {
		t.Fatalf("invalid options = %#v, %v", edits, err)
	}

	// One invalid range fails the whole request even when other ranges are
	// valid; no partial results are returned.
	reversed := protocol.Range{Start: protocol.Position{Line: 3}, End: protocol.Position{Line: 1}}
	if edits, err := instance.RangesFormatting(context.Background(), &protocol.DocumentRangesFormattingParams{
		TextDocument: textDocument, Ranges: []protocol.Range{validRange, reversed}, Options: options,
	}); !errors.Is(err, jsonrpc2.ErrInvalidParams) || edits != nil {
		t.Fatalf("reversed range among valid = %#v, %v", edits, err)
	}
	outOfDocument := protocol.Range{Start: protocol.Position{Line: 99}, End: protocol.Position{Line: 100}}
	if edits, err := instance.RangesFormatting(context.Background(), &protocol.DocumentRangesFormattingParams{
		TextDocument: textDocument, Ranges: []protocol.Range{validRange, outOfDocument}, Options: options,
	}); !errors.Is(err, jsonrpc2.ErrInvalidParams) || edits != nil {
		t.Fatalf("out-of-document range among valid = %#v, %v", edits, err)
	}

	// An empty selection formats nothing but is not an error.
	empty, err := instance.RangesFormatting(context.Background(), &protocol.DocumentRangesFormattingParams{
		TextDocument: textDocument, Ranges: []protocol.Range{}, Options: options,
	})
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty ranges = %#v, %v", empty, err)
	}

	// An unknown document mirrors whole-document formatting: empty edits, nil error.
	missing := New(nil, nil, io.Discard)
	unknown, err := missing.RangesFormatting(context.Background(), &protocol.DocumentRangesFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri.MustParse("file:///missing-ranges.vim")},
		Ranges:       []protocol.Range{validRange},
		Options:      options,
	})
	if err != nil || unknown == nil || len(unknown) != 0 {
		t.Fatalf("unknown document ranges = %#v, %v", unknown, err)
	}
}

func TestRangesFormattingRejectsStaleSnapshot(t *testing.T) {
	// beforeParseSnapshotCacheMiss replaces the open document while the
	// request parses, so the final freshness check must fail even when the
	// requested ranges (or none at all) would otherwise format successfully.
	tests := []struct {
		name   string
		ranges []protocol.Range
	}{
		{name: "with requested ranges", ranges: []protocol.Range{
			{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 3}},
			{Start: protocol.Position{Line: 5}, End: protocol.Position{Line: 6}},
		}},
		{name: "empty ranges", ranges: []protocol.Range{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stale, staleURI := openNavigationDocument(t, text.UTF16, multiBlockFormattingSource)
			stale.testHooks.beforeParseSnapshotCacheMiss = func(*text.Snapshot) {
				stale.publishMu.Lock()
				stale.documents.Open(staleURI.String(), 2, "vim9script\n")
				stale.publishMu.Unlock()
			}
			edits, err := stale.RangesFormatting(context.Background(), &protocol.DocumentRangesFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: staleURI},
				Ranges:       test.ranges,
				Options:      protocol.FormattingOptions{TabSize: 2, InsertSpaces: true},
			})
			if !errors.Is(err, protocol.ErrContentModified) || edits != nil {
				t.Fatalf("stale ranges formatting = %#v, %v", edits, err)
			}
		})
	}
}

func TestOnTypeFormattingCapabilityAndMethod(t *testing.T) {
	instance := New(nil, nil, nil)
	t.Cleanup(instance.stopAnalysis)
	result, err := instance.Initialize(context.Background(), &protocol.InitializeParams{})
	if err != nil {
		t.Fatal(err)
	}
	options := result.Capabilities.DocumentOnTypeFormattingProvider
	if options.FirstTriggerCharacter != "\n" || len(options.MoreTriggerCharacter) != 1 || options.MoreTriggerCharacter[0] != "\\" {
		t.Fatalf("on-type formatting provider = %#v", options)
	}
	if !implementedMethod(protocol.MethodTextDocumentOnTypeFormatting) {
		t.Fatalf("method %q is not implemented", protocol.MethodTextDocumentOnTypeFormatting)
	}
}

func TestOnTypeFormattingMatchesFormattingPlanForContinuation(t *testing.T) {
	for _, source := range []string{
		"vim9script\nvar list = [\n  \\'one',\n  \\'two',\n  \\]\n",
		"let list = [\n  \\ 'one',\n  \\ 'two',\n  \\ ]\n",
	} {
		instance, documentURI := openNavigationDocument(t, text.UTF16, source)
		edits, err := instance.Formatting(context.Background(), &protocol.DocumentFormattingParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Options:      protocol.FormattingOptions{TabSize: 2, InsertSpaces: true},
		})
		if err != nil || len(edits) == 0 {
			t.Fatalf("formatting edits = %#v, %v for %q", edits, err, source)
		}
		for _, edit := range edits {
			onType, err := instance.OnTypeFormatting(context.Background(), &protocol.DocumentOnTypeFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
				Position:     edit.Range.Start,
				Ch:           "\\",
				Options:      protocol.FormattingOptions{TabSize: 2, InsertSpaces: true},
			})
			if err != nil {
				t.Fatalf("on-type error for %q: %v", source, err)
			}
			if len(onType) != 1 || onType[0] != edit {
				t.Fatalf("on-type = %#v, want formatting edit %#v for %q", onType, edit, source)
			}
		}
	}
}

func TestOnTypeFormattingIgnoresOtherCharactersAndNonContinuations(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar text = 'a\\\\b'\nvar items = [1,\n  2]\n")
	options := protocol.FormattingOptions{TabSize: 2, InsertSpaces: true}
	textDocument := protocol.TextDocumentIdentifier{URI: documentURI}

	// A trigger character other than backslash does nothing, even on a
	// continuation line.
	onType, err := instance.OnTypeFormatting(context.Background(), &protocol.DocumentOnTypeFormattingParams{
		TextDocument: textDocument, Position: protocol.Position{Line: 3, Character: 4}, Ch: "o", Options: options,
	})
	if err != nil || len(onType) != 0 {
		t.Fatalf("non-trigger = %#v, %v", onType, err)
	}

	// Backslash inside a regular line is not a continuation head.
	onType, err = instance.OnTypeFormatting(context.Background(), &protocol.DocumentOnTypeFormattingParams{
		TextDocument: textDocument, Position: protocol.Position{Line: 1, Character: 14}, Ch: "\\", Options: options,
	})
	if err != nil || len(onType) != 0 {
		t.Fatalf("string backslash = %#v, %v", onType, err)
	}
}

func TestOnTypeFormattingAlignedContinuationNeedsNoEdit(t *testing.T) {
	misaligned := "let list = [\n\\ 'one',\n\\ 'two',\n\\ ]\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, misaligned)
	options := protocol.FormattingOptions{TabSize: 2, InsertSpaces: true}
	edits, err := instance.Formatting(context.Background(), &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Options:      options,
	})
	if err != nil || len(edits) == 0 {
		t.Fatalf("formatting edits = %#v, %v", edits, err)
	}
	aligned := applyProtocolEdits(t, misaligned, text.UTF16, edits)

	alignedInstance, alignedURI := openNavigationDocument(t, text.UTF16, aligned)
	for line := uint32(1); line < 4; line++ {
		onType, err := alignedInstance.OnTypeFormatting(context.Background(), &protocol.DocumentOnTypeFormattingParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: alignedURI},
			Position:     protocol.Position{Line: line, Character: 4},
			Ch:           "\\",
			Options:      options,
		})
		if err != nil || len(onType) != 0 {
			t.Fatalf("aligned continuation line %d = %#v, %v (source %q)", line, onType, err, aligned)
		}
	}
}

func TestOnTypeFormattingSkipsProtectedPayloadLines(t *testing.T) {
	source := "vim9script\nvar lines =<< trim END\n  \\not a continuation\nEND\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	onType, err := instance.OnTypeFormatting(context.Background(), &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: 2, Character: 4},
		Ch:           "\\",
		Options:      protocol.FormattingOptions{TabSize: 2, InsertSpaces: true},
	})
	if err != nil || len(onType) != 0 {
		t.Fatalf("heredoc line = %#v, %v", onType, err)
	}
}

func TestOnTypeFormattingInvalidParamsAndMissingDocument(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\n")
	if _, err := instance.OnTypeFormatting(context.Background(), &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: 1, Character: 0},
		Ch:           "\\",
		Options:      protocol.FormattingOptions{TabSize: 0},
	}); !errors.Is(err, jsonrpc2.ErrInvalidParams) {
		t.Fatalf("invalid options error = %v", err)
	}
	missing := uri.MustParse("file:///missing-on-type.vim")
	onType, err := instance.OnTypeFormatting(context.Background(), &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: missing},
		Position:     protocol.Position{Line: 0, Character: 0},
		Ch:           "\\",
		Options:      protocol.FormattingOptions{TabSize: 2, InsertSpaces: true},
	})
	if err != nil || len(onType) != 0 {
		t.Fatalf("missing document = %#v, %v", onType, err)
	}
	if _, err := instance.OnTypeFormatting(context.Background(), &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: 99, Character: 0},
		Ch:           "\\",
		Options:      protocol.FormattingOptions{TabSize: 2, InsertSpaces: true},
	}); err != nil {
		t.Fatalf("out-of-range line error = %v", err)
	}
}

func TestOnTypeFormattingNewlineLegacyAndVim9(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		line     uint32
		tabSize  uint32
		spaces   bool
		wantEdit protocol.TextEdit
		noEdit   bool
	}{
		{
			name:    "Legacy closed function newline",
			source:  "function! Foo()\n\nendfunction\n",
			line:    1,
			tabSize: 4,
			spaces:  true,
			wantEdit: protocol.TextEdit{
				Range:   protocol.Range{Start: protocol.Position{Line: 1, Character: 0}, End: protocol.Position{Line: 1, Character: 0}},
				NewText: "    ",
			},
		},
		{
			name:    "Legacy unclosed function newline",
			source:  "function! Foo()\n\n",
			line:    1,
			tabSize: 4,
			spaces:  true,
			wantEdit: protocol.TextEdit{
				Range:   protocol.Range{Start: protocol.Position{Line: 1, Character: 0}, End: protocol.Position{Line: 1, Character: 0}},
				NewText: "    ",
			},
		},
		{
			name:    "Legacy nested if newline",
			source:  "function! Foo()\n    if 1\n\n    endif\nendfunction\n",
			line:    2,
			tabSize: 4,
			spaces:  true,
			wantEdit: protocol.TextEdit{
				Range:   protocol.Range{Start: protocol.Position{Line: 2, Character: 0}, End: protocol.Position{Line: 2, Character: 0}},
				NewText: "        ",
			},
		},
		{
			name:    "Vim9 closed def newline",
			source:  "vim9script\ndef Foo()\n\nenddef\n",
			line:    2,
			tabSize: 4,
			spaces:  true,
			wantEdit: protocol.TextEdit{
				Range:   protocol.Range{Start: protocol.Position{Line: 2, Character: 0}, End: protocol.Position{Line: 2, Character: 0}},
				NewText: "    ",
			},
		},
		{
			name:    "Vim9 unclosed def newline",
			source:  "vim9script\ndef Foo()\n\n",
			line:    2,
			tabSize: 4,
			spaces:  true,
			wantEdit: protocol.TextEdit{
				Range:   protocol.Range{Start: protocol.Position{Line: 2, Character: 0}, End: protocol.Position{Line: 2, Character: 0}},
				NewText: "    ",
			},
		},
		{
			name:    "Vim9 closed list bracket newline",
			source:  "vim9script\nvar list = [\n\n]\n",
			line:    2,
			tabSize: 4,
			spaces:  true,
			wantEdit: protocol.TextEdit{
				Range:   protocol.Range{Start: protocol.Position{Line: 2, Character: 0}, End: protocol.Position{Line: 2, Character: 0}},
				NewText: "    ",
			},
		},
		{
			name:    "Vim9 unclosed list bracket newline",
			source:  "vim9script\nvar list = [\n\n",
			line:    2,
			tabSize: 4,
			spaces:  true,
			wantEdit: protocol.TextEdit{
				Range:   protocol.Range{Start: protocol.Position{Line: 2, Character: 0}, End: protocol.Position{Line: 2, Character: 0}},
				NewText: "    ",
			},
		},
		{
			name:    "Vim9 re-indent existing spaces on newline",
			source:  "vim9script\ndef Foo()\n  \nenddef\n",
			line:    2,
			tabSize: 4,
			spaces:  true,
			wantEdit: protocol.TextEdit{
				Range:   protocol.Range{Start: protocol.Position{Line: 2, Character: 0}, End: protocol.Position{Line: 2, Character: 2}},
				NewText: "    ",
			},
		},
		{
			name:    "Vim9 strip spaces on top level blank line",
			source:  "vim9script\nvar x = 1\n  \nvar y = 2\n",
			line:    2,
			tabSize: 4,
			spaces:  true,
			wantEdit: protocol.TextEdit{
				Range:   protocol.Range{Start: protocol.Position{Line: 2, Character: 0}, End: protocol.Position{Line: 2, Character: 2}},
				NewText: "",
			},
		},
		{
			name:    "Vim9 tab indentation",
			source:  "vim9script\ndef Foo()\n\nenddef\n",
			line:    2,
			tabSize: 4,
			spaces:  false,
			wantEdit: protocol.TextEdit{
				Range:   protocol.Range{Start: protocol.Position{Line: 2, Character: 0}, End: protocol.Position{Line: 2, Character: 0}},
				NewText: "\t",
			},
		},
		{
			name:    "Line already properly indented needs no edit",
			source:  "vim9script\ndef Foo()\n    \nenddef\n",
			line:    2,
			tabSize: 4,
			spaces:  true,
			noEdit:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			instance, docURI := openNavigationDocument(t, text.UTF16, tc.source)
			edits, err := instance.OnTypeFormatting(context.Background(), &protocol.DocumentOnTypeFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
				Position:     protocol.Position{Line: tc.line, Character: 0},
				Ch:           "\n",
				Options:      protocol.FormattingOptions{TabSize: tc.tabSize, InsertSpaces: tc.spaces},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.noEdit {
				if len(edits) != 0 {
					t.Fatalf("expected no edits, got: %#v", edits)
				}
				return
			}
			if len(edits) != 1 {
				t.Fatalf("expected 1 edit, got %d: %#v", len(edits), edits)
			}
			if edits[0] != tc.wantEdit {
				t.Fatalf("got edit %#v, want %#v", edits[0], tc.wantEdit)
			}
		})
	}
}
