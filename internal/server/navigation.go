package server

import (
	"context"
	"sort"
	"strings"

	"github.com/chemzqm/vimls-go/internal/analysis"
	"github.com/chemzqm/vimls-go/internal/syntax"
	"github.com/chemzqm/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type navigationDocument struct {
	server      *Server
	snapshot    *text.Snapshot
	encoding    text.Encoding
	analysis    *analysis.FileAnalysis
	declaration *analysis.Declaration
	occurrence  syntax.Span
}

func (s *Server) navigationAt(ctx context.Context, documentURI string, position protocol.Position) (*navigationDocument, error) {
	s.publishMu.Lock()
	snapshot, ok := s.documents.Snapshot(documentURI)
	parsed := s.parsed[documentURI]
	s.publishMu.Unlock()
	if !ok || snapshot.ByteLen() > maxFileBytes {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, protocol.ErrRequestCancelled
	}

	s.mu.Lock()
	encoding := s.encoding
	s.mu.Unlock()
	offset, err := snapshot.Offset(fromProtocolPosition(position), encoding)
	if err != nil {
		return nil, nil
	}
	file := parsed.file
	if file == nil || parsed.revision != snapshot.Revision() {
		file = syntax.Parse(snapshot.Text())
	}
	result := analysis.Analyze(file)
	if err := ctx.Err(); err != nil {
		return nil, protocol.ErrRequestCancelled
	}

	document := &navigationDocument{server: s, snapshot: snapshot, encoding: encoding, analysis: result}
	for _, declaration := range result.Declarations {
		if spanContains(declaration.Span, offset) {
			document.declaration = declaration
			document.occurrence = declaration.Span
			return document, document.checkCurrent(ctx)
		}
	}
	for _, reference := range result.References {
		if spanContains(reference.Span, offset) {
			document.declaration = reference.Declaration
			document.occurrence = reference.Span
			return document, document.checkCurrent(ctx)
		}
	}
	return document, document.checkCurrent(ctx)
}

func spanContains(span syntax.Span, offset int) bool {
	return span.Start <= offset && offset < span.End
}

func (document *navigationDocument) checkCurrent(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return protocol.ErrRequestCancelled
	}
	current, ok := document.server.documents.Snapshot(document.snapshot.URI())
	if !ok || current != document.snapshot {
		return protocol.ErrContentModified
	}
	return nil
}

func (document *navigationDocument) location(span syntax.Span) (protocol.Location, bool) {
	rangeValue, ok := protocolRange(document.snapshot, document.encoding, span)
	if !ok {
		return protocol.Location{}, false
	}
	return protocol.Location{URI: uri.URI(document.snapshot.URI()), Range: rangeValue}, true
}

func (s *Server) Definition(ctx context.Context, params *protocol.DefinitionParams) (protocol.DefinitionResult, error) {
	document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
	if err != nil || document == nil || document.declaration == nil {
		return protocol.LocationSlice{}, err
	}
	location, ok := document.location(document.declaration.Span)
	if !ok {
		return protocol.LocationSlice{}, document.checkCurrent(ctx)
	}
	if err := document.checkCurrent(ctx); err != nil {
		return nil, err
	}
	return protocol.LocationSlice{location}, nil
}

func (s *Server) Declaration(ctx context.Context, params *protocol.DeclarationParams) (protocol.DeclarationResult, error) {
	document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
	if err != nil || document == nil || document.declaration == nil {
		return protocol.LocationSlice{}, err
	}
	location, ok := document.location(document.declaration.Span)
	if !ok {
		return protocol.LocationSlice{}, document.checkCurrent(ctx)
	}
	if err := document.checkCurrent(ctx); err != nil {
		return nil, err
	}
	return protocol.LocationSlice{location}, nil
}

func (s *Server) References(ctx context.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) {
	document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
	if err != nil || document == nil || document.declaration == nil {
		return []protocol.Location{}, err
	}
	spans := document.occurrences(params.Context.IncludeDeclaration)
	locations := make([]protocol.Location, 0, len(spans))
	for _, span := range spans {
		if location, ok := document.location(span); ok {
			locations = append(locations, location)
		}
	}
	if err := document.checkCurrent(ctx); err != nil {
		return nil, err
	}
	return locations, nil
}

func (s *Server) DocumentHighlight(ctx context.Context, params *protocol.DocumentHighlightParams) ([]protocol.DocumentHighlight, error) {
	document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
	if err != nil || document == nil || document.declaration == nil {
		return []protocol.DocumentHighlight{}, err
	}
	spans := document.occurrences(true)
	highlights := make([]protocol.DocumentHighlight, 0, len(spans))
	for _, span := range spans {
		if rangeValue, ok := protocolRange(document.snapshot, document.encoding, span); ok {
			highlights = append(highlights, protocol.DocumentHighlight{Range: rangeValue, Kind: protocol.DocumentHighlightKindText})
		}
	}
	if err := document.checkCurrent(ctx); err != nil {
		return nil, err
	}
	return highlights, nil
}

func (document *navigationDocument) occurrences(includeDeclaration bool) []syntax.Span {
	spans := make([]syntax.Span, 0, len(document.analysis.References)+1)
	if includeDeclaration {
		spans = append(spans, document.declaration.Span)
	}
	for _, reference := range document.analysis.References {
		if reference.Declaration == document.declaration {
			spans = append(spans, reference.Span)
		}
	}
	sort.SliceStable(spans, func(left, right int) bool { return spans[left].Start < spans[right].Start })
	return spans
}

func (s *Server) Hover(ctx context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
	if err != nil || document == nil || document.declaration == nil {
		return nil, err
	}
	declaration := document.declaration
	lines := []string{"name: " + declaration.Name, "kind: " + string(declaration.Kind)}
	if declaration.Type.Name != "" && declaration.Type.Name != analysis.ValueTypeAny {
		lines = append(lines, "type: "+formatValueType(declaration.Type))
	}
	rangeValue, ok := protocolRange(document.snapshot, document.encoding, document.occurrence)
	if !ok {
		return nil, document.checkCurrent(ctx)
	}
	if err := document.checkCurrent(ctx); err != nil {
		return nil, err
	}
	return &protocol.Hover{
		Contents: &protocol.MarkupContent{Kind: protocol.MarkupKindPlainText, Value: strings.Join(lines, "\n")},
		Range:    &rangeValue,
	}, nil
}

func formatValueType(value analysis.ValueType) string {
	arguments := make([]string, 0, len(value.Arguments))
	for _, argument := range value.Arguments {
		arguments = append(arguments, formatValueType(argument))
	}
	if value.Name == "func" {
		result := "func(" + strings.Join(arguments, ", ") + ")"
		if value.Return != nil {
			result += ": " + formatValueType(*value.Return)
		}
		return result
	}
	if len(arguments) > 0 {
		return value.Name + "<" + strings.Join(arguments, ", ") + ">"
	}
	return value.Name
}
