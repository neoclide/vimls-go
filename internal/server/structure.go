package server

import (
	"context"
	"sort"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
)

// structureDocument returns one immutable parsed view of an open document.
// The analysis worker normally populates parsed, but requests also parse a
// current snapshot when the worker has not caught up yet.
func (s *Server) structureDocument(ctx context.Context, documentURI string) (*text.Snapshot, *syntax.File, text.Encoding, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, text.UTF16, protocol.ErrRequestCancelled
	}
	s.publishMu.Lock()
	snapshot, ok := s.documents.Snapshot(documentURI)
	parsed := s.parsed[documentURI]
	s.publishMu.Unlock()
	s.mu.Lock()
	encoding := s.encoding
	s.mu.Unlock()
	if !ok || snapshot.ByteLen() > maxFileBytes {
		return nil, nil, encoding, nil
	}
	file := parsed.file
	if file == nil || parsed.revision != snapshot.Revision() {
		file = syntax.Parse(snapshot.Text())
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, encoding, protocol.ErrRequestCancelled
	}
	return snapshot, file, encoding, nil
}

func (s *Server) structureCurrent(ctx context.Context, snapshot *text.Snapshot) error {
	if err := ctx.Err(); err != nil {
		return protocol.ErrRequestCancelled
	}
	current, ok := s.documents.Snapshot(snapshot.URI())
	if !ok || current != snapshot {
		return protocol.ErrContentModified
	}
	return nil
}

// FoldingRanges returns line-only folds for known syntax regions. Spans
// are collected from nested command lists as well as the top-level file.
func (s *Server) FoldingRanges(ctx context.Context, params *protocol.FoldingRangeParams) ([]protocol.FoldingRange, error) {
	snapshot, file, encoding, err := s.structureDocument(ctx, params.TextDocument.URI.String())
	if err != nil {
		return nil, err
	}
	if snapshot == nil || file == nil {
		return []protocol.FoldingRange{}, nil
	}
	spans := collectFoldingSpans(file)
	result := make([]protocol.FoldingRange, 0, len(spans))
	seen := make(map[[2]uint32]struct{})
	for index, span := range spans {
		if index%32 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, protocol.ErrRequestCancelled
			}
		}
		if !validStructureSpan(span, len(snapshot.Text())) {
			continue
		}
		start, startErr := snapshot.Position(span.Start, encoding)
		end, endErr := snapshot.Position(span.End, encoding)
		if startErr != nil || endErr != nil {
			continue
		}
		endLine := end.Line
		if end.Character == 0 {
			endLine--
		}
		if start.Line >= endLine {
			continue
		}
		key := [2]uint32{uint32(start.Line), uint32(endLine)}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, protocol.FoldingRange{StartLine: uint32(start.Line), EndLine: uint32(endLine)})
	}
	if err := s.structureCurrent(ctx, snapshot); err != nil {
		return nil, err
	}
	return result, nil
}

// SelectionRange returns one hierarchy per requested position. Invalid
// positions intentionally produce the protocol's zero value so one malformed
// position does not discard otherwise valid results.
func (s *Server) SelectionRange(ctx context.Context, params *protocol.SelectionRangeParams) ([]protocol.SelectionRange, error) {
	snapshot, file, encoding, err := s.structureDocument(ctx, params.TextDocument.URI.String())
	if err != nil {
		return nil, err
	}
	if snapshot == nil || file == nil {
		return []protocol.SelectionRange{}, nil
	}
	spans := collectStructureSpans(file)
	result := make([]protocol.SelectionRange, 0, len(params.Positions))
	for index, position := range params.Positions {
		if index%16 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, protocol.ErrRequestCancelled
			}
		}
		offset, offsetErr := snapshot.Offset(fromProtocolPosition(position), encoding)
		if offsetErr != nil {
			result = append(result, protocol.SelectionRange{})
			continue
		}
		chain := structureSelectionChain(spans, offset, len(snapshot.Text()))
		if len(chain) == 0 {
			result = append(result, protocol.SelectionRange{})
			continue
		}
		result = append(result, selectionRangeChain(snapshot, encoding, chain))
	}
	if err := s.structureCurrent(ctx, snapshot); err != nil {
		return nil, err
	}
	return result, nil
}

func selectionRangeChain(snapshot *text.Snapshot, encoding text.Encoding, spans []syntax.Span) protocol.SelectionRange {
	var parent *protocol.SelectionRange
	for index := len(spans) - 1; index >= 0; index-- {
		rangeValue, ok := protocolRange(snapshot, encoding, spans[index])
		if !ok {
			continue
		}
		current := &protocol.SelectionRange{Range: rangeValue, Parent: parent}
		parent = current
	}
	if parent == nil {
		return protocol.SelectionRange{}
	}
	return *parent
}

func structureSelectionChain(spans []syntax.Span, offset, sourceLength int) []syntax.Span {
	candidates := make([]syntax.Span, 0, len(spans))
	for _, span := range spans {
		if !validStructureSpan(span, sourceLength) || span.Start > offset || offset >= span.End {
			continue
		}
		candidates = append(candidates, span)
	}
	sort.Slice(candidates, func(left, right int) bool {
		leftLength := candidates[left].End - candidates[left].Start
		rightLength := candidates[right].End - candidates[right].Start
		if leftLength != rightLength {
			return leftLength < rightLength
		}
		if candidates[left].Start != candidates[right].Start {
			return candidates[left].Start < candidates[right].Start
		}
		return candidates[left].End < candidates[right].End
	})
	chain := make([]syntax.Span, 0, len(candidates))
	for _, candidate := range candidates {
		if len(chain) == 0 {
			chain = append(chain, candidate)
			continue
		}
		last := chain[len(chain)-1]
		if candidate.Start <= last.Start && candidate.End >= last.End && (candidate.Start < last.Start || candidate.End > last.End) {
			chain = append(chain, candidate)
		}
	}
	return chain
}

func validStructureSpan(span syntax.Span, sourceLength int) bool {
	return span.Start >= 0 && span.End > span.Start && span.End <= sourceLength
}

func collectStructureSpans(file *syntax.File) []syntax.Span {
	collector := collectSpans(file)
	sort.SliceStable(collector.spans, func(left, right int) bool {
		if collector.spans[left].Start != collector.spans[right].Start {
			return collector.spans[left].Start < collector.spans[right].Start
		}
		return collector.spans[left].End < collector.spans[right].End
	})
	return collector.spans
}

func collectFoldingSpans(file *syntax.File) []syntax.Span {
	collector := collectSpans(file)
	sort.SliceStable(collector.folds, func(left, right int) bool {
		if collector.folds[left].Start != collector.folds[right].Start {
			return collector.folds[left].Start < collector.folds[right].Start
		}
		return collector.folds[left].End < collector.folds[right].End
	})
	return collector.folds
}

func collectSpans(file *syntax.File) structureSpanCollector {
	collector := structureSpanCollector{seenFiles: make(map[*syntax.File]bool), seenLists: make(map[*syntax.CommandList]bool)}
	collector.file(file)
	return collector
}

type structureSpanCollector struct {
	spans     []syntax.Span
	folds     []syntax.Span
	seenFiles map[*syntax.File]bool
	seenLists map[*syntax.CommandList]bool
}

func (collector *structureSpanCollector) add(span syntax.Span) {
	collector.spans = append(collector.spans, span)
}

func (collector *structureSpanCollector) file(file *syntax.File) {
	if file == nil || collector.seenFiles[file] {
		return
	}
	collector.seenFiles[file] = true
	collector.add(syntax.Span{Start: 0, End: len(file.Source)})
	for _, block := range file.Blocks {
		collector.add(block.Span)
		collector.folds = append(collector.folds, block.Span)
	}
	for index := range file.Commands {
		collector.command(&file.Commands[index])
	}
}

func (collector *structureSpanCollector) list(list *syntax.CommandList) {
	if list == nil || collector.seenLists[list] {
		return
	}
	collector.seenLists[list] = true
	collector.add(list.Span)
	for _, block := range list.Blocks {
		collector.add(block.Span)
		collector.folds = append(collector.folds, block.Span)
	}
	for index := range list.Commands {
		collector.command(&list.Commands[index])
	}
}

func (collector *structureSpanCollector) command(command *syntax.Command) {
	if command == nil {
		return
	}
	collector.add(command.Span)
	collector.add(command.Name)
	if command.Heredoc != nil {
		collector.add(command.Heredoc.Body)
		collector.add(command.Heredoc.EndMarker)
		collector.folds = append(collector.folds, command.Span)
	}
	if command.TextBody != nil {
		collector.add(command.TextBody.Body)
		collector.add(command.TextBody.EndMarker)
		collector.folds = append(collector.folds, command.Span)
	}
	collector.list(command.Embedded)
	if command.Declaration != nil {
		for _, binding := range command.Declaration.Bindings {
			collector.add(binding.Name)
			collector.typeNode(binding.ParsedType)
		}
		collector.expression(command.Declaration.Initializer)
	}
	if command.For != nil {
		for _, binding := range command.For.Bindings {
			collector.add(binding.Name)
			collector.typeNode(binding.ParsedType)
		}
		collector.expression(command.For.Iterable)
	}
	if command.Function != nil {
		collector.add(command.Function.Name)
		for _, typeParameter := range command.Function.TypeParameters {
			collector.add(typeParameter.Span)
		}
		for _, parameter := range command.Function.Parameters {
			collector.add(parameter.Name)
			collector.expression(parameter.Target)
			collector.typeNode(parameter.Type)
			collector.expression(parameter.Default)
		}
		collector.typeNode(command.Function.ReturnType)
	}
	if command.Aggregate != nil {
		collector.add(command.Aggregate.Name)
		for _, span := range command.Aggregate.Extends {
			collector.add(span)
		}
		for _, span := range command.Aggregate.Implements {
			collector.add(span)
		}
	}
	if command.TypeAlias != nil {
		collector.add(command.TypeAlias.Name)
		collector.typeNode(command.TypeAlias.Type)
	}
	for _, value := range command.EnumValues {
		collector.add(value.Name)
		collector.expression(value.Initializer)
		for _, argument := range value.Arguments {
			collector.expression(argument)
		}
	}
	if command.Import != nil {
		collector.add(command.Import.Alias)
		collector.expression(command.Import.Path)
	}
	if command.Mapping != nil {
		collector.add(command.Mapping.LHS)
		collector.add(command.Mapping.RHS)
		collector.expression(command.Mapping.RHSExpression)
	}
	for _, expression := range command.Expressions {
		collector.expression(expression)
	}
	for _, expression := range command.Targets {
		collector.expression(expression)
	}
}

func (collector *structureSpanCollector) expression(expression *syntax.Expression) {
	if expression == nil {
		return
	}
	collector.add(expression.Span)
	for _, child := range expression.Children {
		collector.expression(child)
	}
	if expression.LambdaBody != nil {
		collector.file(expression.LambdaBody)
	}
	for _, parameter := range expression.Parameters {
		collector.add(parameter.Name)
		collector.expression(parameter.Target)
		collector.typeNode(parameter.Type)
		collector.expression(parameter.Default)
	}
	collector.typeNode(expression.ReturnType)
}

func (collector *structureSpanCollector) typeNode(typeNode *syntax.Type) {
	if typeNode == nil {
		return
	}
	collector.add(typeNode.Span)
	for _, argument := range typeNode.Arguments {
		collector.typeNode(argument)
	}
	collector.typeNode(typeNode.ReturnType)
}
