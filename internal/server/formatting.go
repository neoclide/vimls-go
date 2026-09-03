package server

import (
	"context"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

func (s *Server) Formatting(ctx context.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	if params == nil || !validFormattingOptions(params.Options) {
		return nil, jsonrpc2.ErrInvalidParams
	}
	return s.formattingEdits(ctx, params.TextDocument.URI.String(), params.Options, nil)
}

func (s *Server) RangeFormatting(ctx context.Context, params *protocol.DocumentRangeFormattingParams) ([]protocol.TextEdit, error) {
	if params == nil || !validFormattingOptions(params.Options) {
		return nil, jsonrpc2.ErrInvalidParams
	}
	return s.formattingEdits(ctx, params.TextDocument.URI.String(), params.Options, &params.Range)
}

// RangesFormatting formats several selections of one document in a single
// request (LSP 3.18 textDocument/rangesFormatting). The requested ranges form
// a selection union: every candidate IndentEdit is returned at most once, in
// syntax.IndentEdits document order, when it lies fully inside at least one
// requested range. The document snapshot, syntax file, encoding and IndentEdits
// are obtained exactly once so the result is never spliced across versions.
func (s *Server) RangesFormatting(ctx context.Context, params *protocol.DocumentRangesFormattingParams) ([]protocol.TextEdit, error) {
	if params == nil || !validFormattingOptions(params.Options) {
		return nil, jsonrpc2.ErrInvalidParams
	}
	snapshot, file, encoding, err := s.structureDocument(ctx, params.TextDocument.URI.String())
	if err != nil {
		return nil, err
	}
	if snapshot == nil || file == nil {
		return []protocol.TextEdit{}, nil
	}
	// Convert every requested range to a byte span first. Any invalid range
	// fails the whole request before any edit is produced.
	spans := make([]syntax.Span, 0, len(params.Ranges))
	for _, requested := range params.Ranges {
		start, startErr := snapshot.Offset(fromProtocolPosition(requested.Start), encoding)
		end, endErr := snapshot.Offset(fromProtocolPosition(requested.End), encoding)
		if startErr != nil || endErr != nil || end < start {
			return nil, jsonrpc2.ErrInvalidParams
		}
		spans = append(spans, syntax.Span{Start: start, End: end})
	}
	candidates := syntax.IndentEdits(file, syntax.IndentOptions{TabSize: int(params.Options.TabSize), InsertSpaces: params.Options.InsertSpaces})
	result := make([]protocol.TextEdit, 0, len(candidates))
	for _, candidate := range candidates {
		if !indentEditCovered(candidate.Span, spans) {
			continue
		}
		rangeValue, ok := protocolRange(snapshot, encoding, candidate.Span)
		if !ok {
			continue
		}
		result = append(result, protocol.TextEdit{Range: rangeValue, NewText: candidate.NewText})
	}
	if err := s.structureCurrent(ctx, snapshot); err != nil {
		return nil, err
	}
	return result, nil
}

// indentEditInside reports whether candidate [start,end) is fully inside the
// byte span [spanStart,spanEnd), applying the exact containment rule of the
// existing single-range formatting filter: a candidate is kept when
// candidate.Start >= spanStart && candidate.End <= spanEnd &&
// candidate.Start < spanEnd.
func indentEditInside(candidate syntax.Span, spanStart, spanEnd int) bool {
	return candidate.Start >= spanStart && candidate.End <= spanEnd && candidate.Start < spanEnd
}

// indentEditCovered reports whether candidate lies fully inside at least one
// requested byte span. Duplicates, out-of-order, adjacent and overlapping
// requested ranges are all accepted because each candidate is decided once
// against every span.
func indentEditCovered(candidate syntax.Span, spans []syntax.Span) bool {
	for _, span := range spans {
		if indentEditInside(candidate, span.Start, span.End) {
			return true
		}
	}
	return false
}

// OnTypeFormatting re-indents the line the cursor rests on. Typing "\"
// re-aligns a continuation line (both legacy scripts and multi-line
// expressions in Vim9 script where a leading backslash is used), and
// typing a newline indents the fresh blank line from the same syntax-backed
// plan as Formatting: the block level inside if/def/... and the bracket
// level inside list, dict, and call continuations.
func (s *Server) OnTypeFormatting(ctx context.Context, params *protocol.DocumentOnTypeFormattingParams) ([]protocol.TextEdit, error) {
	if params == nil || !validFormattingOptions(params.Options) {
		return nil, jsonrpc2.ErrInvalidParams
	}
	ch := params.Ch
	if ch == "\r\n" {
		ch = "\n"
	}
	if ch != "\\" && ch != "\n" {
		return []protocol.TextEdit{}, nil
	}
	snapshot, file, encoding, err := s.structureDocument(ctx, params.TextDocument.URI.String())
	if err != nil || snapshot == nil || file == nil {
		return []protocol.TextEdit{}, err
	}
	options := syntax.IndentOptions{TabSize: int(params.Options.TabSize), InsertSpaces: params.Options.InsertSpaces}
	source := snapshot.Text()
	line := int(params.Position.Line)
	lineStart, err := snapshot.Offset(fromProtocolPosition(protocol.Position{Line: params.Position.Line, Character: 0}), encoding)
	if err != nil {
		return []protocol.TextEdit{}, s.structureCurrent(ctx, snapshot)
	}
	lineEnd := lineEndOffset(source, lineStart)
	indentEnd := lineStart
	for indentEnd < lineEnd && (source[indentEnd] == ' ' || source[indentEnd] == '\t') {
		indentEnd++
	}
	var hasEdit bool
	var editSpan syntax.Span
	var newText string
	switch ch {
	case "\\":
		if !isContinuationLine(source, lineStart) {
			return []protocol.TextEdit{}, s.structureCurrent(ctx, snapshot)
		}
		for _, candidate := range syntax.IndentEdits(file, options) {
			if candidate.Span.Start < lineStart || candidate.Span.Start > lineEnd {
				continue
			}
			editSpan, newText = candidate.Span, candidate.NewText
			hasEdit = true
			break
		}
	case "\n":
		wanted, ok := syntax.IndentForLine(file, options, line)
		if !ok || source[lineStart:indentEnd] == wanted {
			return []protocol.TextEdit{}, s.structureCurrent(ctx, snapshot)
		}
		editSpan, newText = syntax.Span{Start: lineStart, End: indentEnd}, wanted
		hasEdit = true
	}
	if !hasEdit || editSpan.Start > editSpan.End {
		return []protocol.TextEdit{}, s.structureCurrent(ctx, snapshot)
	}
	rangeValue, ok := protocolRange(snapshot, encoding, editSpan)
	if !ok {
		return []protocol.TextEdit{}, s.structureCurrent(ctx, snapshot)
	}
	edit := protocol.TextEdit{Range: rangeValue, NewText: newText}
	return []protocol.TextEdit{edit}, s.structureCurrent(ctx, snapshot)
}

// isContinuationLine reports whether the physical line starting at offset has
// a backslash as its first non-blank character, the leading form Vim's
// line-continuation requires in legacy script.
func isContinuationLine(source string, offset int) bool {
	if offset < 0 || offset >= len(source) {
		return false
	}
	for index := offset; index < len(source); index++ {
		switch source[index] {
		case ' ', '\t':
			continue
		case '\\':
			return true
		default:
			return false
		}
	}
	return false
}

// lineEndOffset returns the offset of the line terminator at or after offset,
// excluding a carriage return, or the end of the source for the final line.
func lineEndOffset(source string, offset int) int {
	end := len(source)
	if index := strings.IndexByte(source[offset:], '\n'); index >= 0 {
		end = offset + index
	}
	if end > offset && source[end-1] == '\r' {
		end--
	}
	return end
}

func validFormattingOptions(options protocol.FormattingOptions) bool {
	return options.TabSize > 0 && options.TabSize <= uint32(maxFileBytes)
}

func (s *Server) formattingEdits(ctx context.Context, documentURI string, options protocol.FormattingOptions, requested *protocol.Range) ([]protocol.TextEdit, error) {
	snapshot, file, encoding, err := s.structureDocument(ctx, documentURI)
	if err != nil {
		return nil, err
	}
	if snapshot == nil || file == nil {
		return []protocol.TextEdit{}, nil
	}
	start, end := 0, snapshot.ByteLen()
	if requested != nil {
		var startErr, endErr error
		start, startErr = snapshot.Offset(fromProtocolPosition(requested.Start), encoding)
		end, endErr = snapshot.Offset(fromProtocolPosition(requested.End), encoding)
		if startErr != nil || endErr != nil || end < start {
			return nil, jsonrpc2.ErrInvalidParams
		}
	}
	candidates := syntax.IndentEdits(file, syntax.IndentOptions{TabSize: int(options.TabSize), InsertSpaces: options.InsertSpaces})
	result := make([]protocol.TextEdit, 0, len(candidates))
	for _, candidate := range candidates {
		if !indentEditInside(candidate.Span, start, end) {
			continue
		}
		rangeValue, ok := protocolRange(snapshot, encoding, candidate.Span)
		if !ok {
			continue
		}
		result = append(result, protocol.TextEdit{Range: rangeValue, NewText: candidate.NewText})
	}
	if err := s.structureCurrent(ctx, snapshot); err != nil {
		return nil, err
	}
	return result, nil
}
