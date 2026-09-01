package server

import (
	"context"

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
		if candidate.Span.Start < start || candidate.Span.End > end || candidate.Span.Start >= end {
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
