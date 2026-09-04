package server

import (
	"context"
	"strings"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// TypeDefinition jumps from a statically typed declaration, reference, or
// type annotation to the declaration of that type.  Dynamic values, builtin
// container types, and ambiguous names return an empty result; no position is
// ever invented for a value whose type the analysis cannot prove.
func (s *Server) TypeDefinition(ctx context.Context, params *protocol.TypeDefinitionParams) (protocol.DefinitionResult, error) {
	for attempt := range 2 {
		snapshot, file, fileAnalysis, encoding, err := s.structureDocumentWithAnalysis(ctx, params.TextDocument.URI.String())
		if err != nil || snapshot == nil || file == nil {
			return protocol.LocationSlice{}, err
		}
		offset, err := snapshot.Offset(fromProtocolPosition(params.Position), encoding)
		if err != nil {
			return protocol.LocationSlice{}, s.structureCurrent(ctx, snapshot)
		}
		path, ok := workspaceURIPath(params.TextDocument.URI)
		if !ok {
			return protocol.LocationSlice{}, s.structureCurrent(ctx, snapshot)
		}
		state := s.captureWorkspaceNavigationState()
		source := snapshot.Text()
		var symbol hierarchySymbol
		resolved := false
		if nameSpan, ok := typeAnnotationSpanAt(file, offset); ok {
			symbol, resolved = s.resolveTypeName(state, path, source, file, nameSpan)
		}
		if !resolved {
			if fileAnalysis == nil {
				fileAnalysis = analysis.Analyze(file)
			}
			valueType, anchor, ok := typedValueAt(fileAnalysis, offset)
			if ok {
				symbol, resolved = s.resolveUserTypeName(state, path, source, file, valueType.Name, anchor)
			}
		}
		if resolved {
			location, valid := typeDefinitionLocation(symbol, encoding)
			if !valid {
				return protocol.LocationSlice{}, s.structureCurrent(ctx, snapshot)
			}
			current, err := s.hierarchyCurrent(ctx, state, snapshot, symbol.snapshot)
			if err != nil {
				return nil, err
			}
			if current {
				return protocol.LocationSlice{location}, nil
			}
		} else {
			current, err := s.hierarchyCurrent(ctx, state, snapshot)
			if err != nil {
				return nil, err
			}
			if current {
				return protocol.LocationSlice{}, nil
			}
		}
		if attempt == 1 {
			return nil, protocol.ErrContentModified
		}
	}
	return nil, protocol.ErrContentModified
}

// typeAnnotationSpanAt returns the named-type span of the type annotation
// containing offset.  Covers variable and loop bindings, def parameters and
// return types, type aliases, and extends/implements clauses.  Generic and
// function types resolve to the argument or return type the cursor rests on;
// builtin container names themselves resolve to nothing.
func typeAnnotationSpanAt(file *syntax.File, offset int) (syntax.Span, bool) {
	var found syntax.Span
	record := func(node *syntax.Type) {
		if found.Start < found.End {
			return
		}
		if span, ok := namedTypeSpanAt(node, offset); ok {
			found = span
		}
	}
	recordSpan := func(span syntax.Span) {
		if found.Start < found.End {
			return
		}
		if spanContains(span, offset) {
			found = span
		}
	}
	walkCommands(file.Commands, func(command *syntax.Command) {
		if command.Declaration != nil {
			record(command.Declaration.ParsedType)
			for _, binding := range command.Declaration.Bindings {
				record(binding.ParsedType)
			}
		}
		if command.For != nil {
			for _, binding := range command.For.Bindings {
				record(binding.ParsedType)
			}
		}
		if command.Function != nil {
			for _, parameter := range command.Function.Parameters {
				record(parameter.Type)
			}
			record(command.Function.ReturnType)
		}
		if command.TypeAlias != nil {
			record(command.TypeAlias.Type)
		}
		if command.Aggregate != nil {
			for _, extends := range command.Aggregate.Extends {
				recordSpan(extends)
			}
			for _, implements := range command.Aggregate.Implements {
				recordSpan(implements)
			}
		}
	})
	return found, found.Start < found.End
}

// namedTypeSpanAt walks a parsed type tree and returns the span of the
// TypeNamed node containing offset.  Cursors resting on the generic or
// function wrapper itself (list, dict, func, ...) return false.
func namedTypeSpanAt(node *syntax.Type, offset int) (syntax.Span, bool) {
	if node == nil || !spanContains(node.Span, offset) {
		return syntax.Span{}, false
	}
	switch node.Kind {
	case syntax.TypeNamed:
		return node.Span, true
	case syntax.TypeGeneric, syntax.TypeFunction, syntax.TypeOptional, syntax.TypeVariadic:
		for _, argument := range node.Arguments {
			if span, ok := namedTypeSpanAt(argument, offset); ok {
				return span, true
			}
		}
		if span, ok := namedTypeSpanAt(node.ReturnType, offset); ok {
			return span, true
		}
	}
	return syntax.Span{}, false
}

// typedValueAt returns the analysis value type of the declaration or resolved
// reference containing offset, together with the anchor span of the occurrence.
func typedValueAt(result *analysis.FileAnalysis, offset int) (analysis.ValueType, syntax.Span, bool) {
	if result == nil {
		return analysis.ValueType{}, syntax.Span{}, false
	}
	for _, declaration := range result.Declarations {
		if declaration != nil && spanContains(declaration.Span, offset) {
			return declaration.Type, declaration.Span, true
		}
	}
	for _, reference := range result.References {
		if reference == nil || reference.Declaration == nil || !spanContains(reference.Span, offset) {
			continue
		}
		return reference.Declaration.Type, reference.Span, true
	}
	return analysis.ValueType{}, syntax.Span{}, false
}

// resolveUserTypeName resolves a statically known value type name to its
// declaring symbol.  Dotted names resolve through the file's static imports;
// plain names must match exactly one aggregate or alias fact in the same
// file.  Ambiguous, forward-imported, or unknown names return false.
func (s *Server) resolveUserTypeName(state workspaceNavigationSnapshot, path, source string, file *syntax.File, name string, anchor syntax.Span) (hierarchySymbol, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return hierarchySymbol{}, false
	}
	if dot := strings.IndexByte(name, '.'); dot > 0 && dot < len(name)-1 {
		alias, member := name[:dot], name[strings.LastIndexByte(name, '.')+1:]
		var found *syntax.Import
		walkCommands(file.Commands, func(command *syntax.Command) {
			if command.Import == nil || command.Import.PathSpan.End > anchor.Start || workspace.ImportAlias(file, command.Import) != alias {
				return
			}
			if found == nil {
				found = command.Import
			} else {
				found = &syntax.Import{}
			}
		})
		if found == nil || found.PathSpan.Start >= found.PathSpan.End {
			return hierarchySymbol{}, false
		}
		reference := workspace.ExternalReferenceFact{
			Path: path, Name: member, Kind: workspace.ExternalReferenceImportMember,
			ImportPath: file.Text(found.PathSpan), ImportAutoload: found.Autoload,
		}
		if target, ok := s.resolveWorkspaceReference(state, reference); ok {
			if !typeHierarchyKind(target.match.Fact.Kind) {
				return hierarchySymbol{}, false
			}
			return hierarchySymbolFromNavigationTarget(target), true
		}
		return hierarchySymbol{}, false
	}
	var match *workspace.SymbolFact
	for _, fact := range workspace.CollectSymbolFacts(path, file) {
		if fact.Name != name || !typeHierarchyKind(fact.Kind) {
			continue
		}
		if match != nil && match.Key() != fact.Key() {
			return hierarchySymbol{}, false
		}
		copy := fact
		match = &copy
	}
	if match == nil {
		return hierarchySymbol{}, false
	}
	return hierarchySymbol{fact: *match, source: source}, true
}

// typeDefinitionLocation converts a resolved type symbol into a protocol
// location for its selection range.
func typeDefinitionLocation(symbol hierarchySymbol, encoding text.Encoding) (protocol.Location, bool) {
	snapshot := symbol.snapshot
	if snapshot == nil {
		if symbol.source == "" {
			return protocol.Location{}, false
		}
		snapshot = text.NewSnapshot(uri.File(symbol.fact.Path).String(), 0, nil, symbol.source)
	}
	selection, ok := protocolRange(snapshot, encoding, symbol.fact.SelectionRange)
	if !ok {
		return protocol.Location{}, false
	}
	return protocol.Location{URI: uri.File(symbol.fact.Path), Range: selection}, true
}
