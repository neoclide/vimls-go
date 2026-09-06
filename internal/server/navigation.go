package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/vimdata"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type navigationDocument struct {
	server            *Server
	snapshot          *text.Snapshot
	encoding          text.Encoding
	analysis          *analysis.FileAnalysis
	declaration       *analysis.Declaration
	definition        *analysis.Declaration
	occurrence        syntax.Span
	external          *workspace.ExternalReferenceFact
	externalMember    string
	externalClass     bool
	optionName        string
	augroupName       string
	memberSnapshots   map[uri.URI]*text.Snapshot
	memberTarget      syntax.Span
	memberDefinition  syntax.Span
	memberConstructor bool
}

func (s *Server) navigationAt(ctx context.Context, documentURI string, position protocol.Position) (*navigationDocument, error) {
	s.publishMu.Lock()
	snapshot, ok := s.documents.Snapshot(documentURI)
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
	file, result := s.analyzeSnapshotContext(ctx, snapshot)
	if file == nil || result == nil {
		if err := ctx.Err(); err != nil {
			return nil, protocol.ErrRequestCancelled
		}
		return nil, nil
	}

	document := &navigationDocument{server: s, snapshot: snapshot, encoding: encoding, analysis: result}
	for _, declaration := range result.Declarations {
		if spanContains(declaration.Span, offset) {
			document.declaration = declaration
			document.occurrence = declaration.Span
			if aggregateMemberDeclaration(declaration) {
				document.memberTarget = declaration.Span
				document.memberConstructor = declaration.Kind == analysis.SymbolKindConstructor
			}
			return document, document.checkCurrent(ctx)
		}
	}
	for _, reference := range result.References {
		if spanContains(reference.Span, offset) {
			document.declaration = reference.Declaration
			document.occurrence = reference.Span
			if reference.Declaration != nil {
				return document, document.checkCurrent(ctx)
			}
			break
		}
	}
	path, workspacePath := workspaceURIPath(uri.URI(documentURI))
	externalFacts := []workspace.ExternalReferenceFact(nil)
	if workspacePath {
		externalFacts = workspace.CollectExternalReferencesFromAnalysis(path, file, result)
		for _, reference := range externalFacts {
			if spanContains(reference.Span, offset) {
				reference := reference
				document.external = &reference
				document.occurrence = reference.Span
				break
			}
		}
	}
	if document.external == nil && document.occurrence.Start >= document.occurrence.End {
		member := memberExpressionAtOffset(file.Commands, offset)
		if member != nil {
			if workspacePath && len(member.Children) == 1 {
				if reference := importedAggregateReferenceForReceiver(path, file, result, member.Children[0], externalFacts); reference != nil {
					document.external = reference
					document.externalMember = member.Value
					document.externalClass = importedMemberClassReceiver(file, member.Children[0])
					document.memberConstructor = member.Value == "new"
					document.occurrence = syntax.Span{Start: member.Operator.End, End: member.Span.End}
				}
			}
			if document.external != nil {
				return document, document.checkCurrent(ctx)
			}
			if file.Text(member.Operator) == "->" {
				if span, ok := expressionMemberSpan(member); ok {
					document.occurrence = span
				}
			}
			declarationSymbol, definitionSymbol, ok := memberNavigationSymbols(file, result, member)
			if ok {
				document.declaration = declarationForSymbol(result, declarationSymbol)
				document.definition = declarationForSymbol(result, definitionSymbol)
				if document.declaration != nil && document.definition != nil {
					document.occurrence = syntax.Span{Start: member.Operator.End, End: member.Span.End}
					document.memberTarget = declarationSymbol.SelectionRange
					document.memberDefinition = definitionSymbol.SelectionRange
					document.memberConstructor = member.Value == "new" && (definitionSymbol.Kind == analysis.SymbolKindClass || definitionSymbol.Kind == analysis.SymbolKindConstructor)
				}
			}
		}
	}
	if document.occurrence.Start >= document.occurrence.End {
		if name, span, ok := autocmdAugroupReferenceAt(file, offset); ok {
			document.augroupName = name
			document.occurrence = span
		}
	}
	if document.occurrence.Start >= document.occurrence.End {
		walkCommands(file.Commands, func(command *syntax.Command) {
			if document.occurrence.Start < document.occurrence.End || !spanContains(command.Name, offset) {
				return
			}
			if _, ok := vimdata.Lookup(":" + file.Text(command.Name)); ok || command.Kind == syntax.CommandUser {
				document.occurrence = command.Name
			}
		})
	}
	if document.occurrence.Start >= document.occurrence.End {
		walkCommands(file.Commands, func(command *syntax.Command) {
			if document.occurrence.Start < document.occurrence.End || command.Set == nil {
				return
			}
			for _, option := range command.Set.Options {
				matchSpan := option.Name
				if option.Prefix.Start < option.Prefix.End {
					matchSpan = syntax.Span{Start: option.Prefix.Start, End: option.Name.End}
				}
				if spanContains(matchSpan, offset) {
					if _, ok := vimdata.LookupOptionMetadata(file.Text(option.Name)); ok {
						document.occurrence = matchSpan
						document.optionName = file.Text(option.Name)
						return
					}
				}
			}
		})
	}
	if document.occurrence.Start >= document.occurrence.End {
		if contextKind, argument := completionBuiltinStringAt(file, offset); contextKind == completionContextHasFeature || contextKind == completionContextExpandSpecial {
			if span, ok := completionBuiltinStringValueSpan(file, argument, contextKind); ok && spanContains(span, offset) {
				document.occurrence = span
			}
		}
	}
	if document.occurrence.Start >= document.occurrence.End {
		if span, _, ok := userCommandAttributeHoverAt(file, offset); ok {
			document.occurrence = span
		}
	}
	if document.occurrence.Start >= document.occurrence.End {
		if span, _, ok := mappingHoverAt(file, offset); ok {
			document.occurrence = span
		}
	}
	return document, document.checkCurrent(ctx)
}

func importedMemberClassReceiver(file *syntax.File, receiver *syntax.Expression) bool {
	return file != nil && receiver != nil && receiver.Kind == syntax.ExpressionMember && file.Text(receiver.Operator) == "."
}

func declarationForSymbol(result *analysis.FileAnalysis, symbol *analysis.Symbol) *analysis.Declaration {
	if result == nil || symbol == nil {
		return nil
	}
	for _, declaration := range result.Declarations {
		if declaration.Span == symbol.SelectionRange {
			return declaration
		}
	}
	return nil
}

func memberNavigationSymbols(file *syntax.File, result *analysis.FileAnalysis, member *syntax.Expression) (*analysis.Symbol, *analysis.Symbol, bool) {
	symbols, container, classReceiver, ok := memberContainerForStaticReceiver(file, result, member)
	if !ok {
		return nil, nil, false
	}
	resolved, _, ok := memberSymbolInContainer(file, symbols, container, member.Value, classReceiver)
	if !ok {
		return nil, nil, false
	}
	declaration, definition := resolved, resolved
	if !classReceiver && !hasInterfaceImplementationDiagnostic(result) {
		switch container.Kind {
		case analysis.SymbolKindInterface:
			if concrete := concreteMemberContainer(file, result, symbols, member.Children[0]); concrete != nil {
				if candidate, _, found := memberSymbolInContainer(file, symbols, concrete, member.Value, false); found && sameMemberCategory(resolved.Kind, candidate.Kind) {
					definition = candidate
				}
			}
		case analysis.SymbolKindClass:
			interfaceMember := implementedInterfaceMember(file, symbols, container, member.Value, resolved.Kind)
			abstractMember := inheritedAbstractMember(file, symbols, container, member.Value, resolved.Kind)
			if interfaceMember != nil && abstractMember == nil {
				declaration = interfaceMember
			} else if abstractMember != nil && interfaceMember == nil {
				declaration = abstractMember
			}
		}
	}
	return declaration, definition, true
}

func inheritedAbstractMember(file *syntax.File, symbols []*analysis.Symbol, class *analysis.Symbol, name string, kind analysis.SymbolKind) *analysis.Symbol {
	seen := make(map[string]bool)
	for class != nil && !seen[class.Name] {
		seen[class.Name] = true
		command := commandForAggregateSpan(file.Commands, class.SelectionRange)
		if command == nil || command.Aggregate == nil || len(command.Aggregate.Extends) == 0 {
			return nil
		}
		class = completionContainer(symbols, file.Text(command.Aggregate.Extends[0]))
		if class == nil {
			return nil
		}
		for _, child := range class.Children {
			if child.Name != name || !sameMemberCategory(kind, child.Kind) {
				continue
			}
			member := commandForSymbolSpan(file.Commands, child.SelectionRange)
			if hasCommandModifier(member, "abstract") {
				return child
			}
			return nil
		}
	}
	return nil
}

func concreteMemberContainer(file *syntax.File, result *analysis.FileAnalysis, symbols []*analysis.Symbol, receiver *syntax.Expression) *analysis.Symbol {
	declaration := declarationForExpression(result, receiver)
	if declaration == nil {
		return nil
	}
	_, initializer := declarationSyntax(file.Commands, declaration.Span)
	if initializer == nil {
		return nil
	}
	return completionContainer(symbols, result.TypeOf(initializer).Name)
}

func implementedInterfaceMember(file *syntax.File, symbols []*analysis.Symbol, class *analysis.Symbol, name string, kind analysis.SymbolKind) *analysis.Symbol {
	var found *analysis.Symbol
	seen := make(map[string]bool)
	for class != nil && !seen[class.Name] {
		seen[class.Name] = true
		command := commandForAggregateSpan(file.Commands, class.SelectionRange)
		if command == nil || command.Aggregate == nil {
			return nil
		}
		for _, implemented := range command.Aggregate.Implements {
			iface := completionContainer(symbols, file.Text(implemented))
			candidate, _, ok := memberSymbolInContainer(file, symbols, iface, name, false)
			if !ok || !sameMemberCategory(kind, candidate.Kind) {
				continue
			}
			if found != nil && found.SelectionRange != candidate.SelectionRange {
				return nil
			}
			found = candidate
		}
		if len(command.Aggregate.Extends) == 0 {
			break
		}
		class = completionContainer(symbols, file.Text(command.Aggregate.Extends[0]))
	}
	return found
}

func sameMemberCategory(left, right analysis.SymbolKind) bool {
	if left == analysis.SymbolKindMethod || left == analysis.SymbolKindConstructor {
		return right == analysis.SymbolKindMethod || right == analysis.SymbolKindConstructor
	}
	if left == analysis.SymbolKindVariable || left == analysis.SymbolKindConstant {
		return right == analysis.SymbolKindVariable || right == analysis.SymbolKindConstant
	}
	return left == right
}

func hasInterfaceImplementationDiagnostic(result *analysis.FileAnalysis) bool {
	if result == nil {
		return true
	}
	for _, diagnostic := range result.Diagnostics {
		switch diagnostic.Code {
		case "vim/E1348", "vim/E1349", "vim/E1367", "vim/E1382", "vim/E1383":
			return true
		}
	}
	return false
}

func aggregateMemberDeclaration(declaration *analysis.Declaration) bool {
	if declaration == nil || declaration.Scope == nil {
		return false
	}
	switch declaration.Scope.Kind {
	case syntax.BlockClass, syntax.BlockInterface, syntax.BlockEnum:
		return true
	default:
		return false
	}
}

func memberExpressionAtOffset(commands []syntax.Command, offset int) *syntax.Expression {
	var result *syntax.Expression
	walkCommands(commands, func(command *syntax.Command) {
		walkCommandExpressions(command, func(expression *syntax.Expression) {
			if expression.Kind != syntax.ExpressionMember {
				return
			}
			span := syntax.Span{Start: expression.Operator.End, End: expression.Span.End}
			if spanContains(span, offset) && (result == nil || expression.Span.End-expression.Span.Start < result.Span.End-result.Span.Start) {
				result = expression
			}
		})
	})
	return result
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
	return s.definitionLocations(ctx, params.TextDocumentPositionParams, false)
}

func (s *Server) definitionLocations(ctx context.Context, params protocol.TextDocumentPositionParams, declaration bool) (protocol.LocationSlice, error) {
	for attempt := range 2 {
		document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
		if err != nil || document == nil {
			return protocol.LocationSlice{}, err
		}
		if document.augroupName != "" {
			state := s.captureWorkspaceNavigationState()
			locations, snapshots := document.augroupDefinitionLocations(state)
			current, err := document.workspaceNavigationCurrent(ctx, state, workspaceNavigationTarget{}, snapshots...)
			if err != nil {
				return nil, err
			}
			if current {
				return locations, nil
			}
			if attempt == 1 {
				return nil, protocol.ErrContentModified
			}
			continue
		}
		if document.external == nil {
			if document.declaration == nil {
				return protocol.LocationSlice{}, nil
			}
			target := document.declaration
			if !declaration && document.definition != nil {
				target = document.definition
			}
			location, ok := document.location(target.Span)
			if !ok {
				return protocol.LocationSlice{}, document.checkCurrent(ctx)
			}
			if err := document.checkCurrent(ctx); err != nil {
				return nil, err
			}
			return protocol.LocationSlice{location}, nil
		}
		state := s.captureWorkspaceNavigationState()
		target, ok := document.workspaceTargetInState(state)
		if ok {
			location, valid := document.server.workspaceTargetLocation(target, document.encoding)
			if !valid {
				ok = false
			} else if current, err := document.workspaceNavigationCurrent(ctx, state, target); err != nil {
				return nil, err
			} else if current {
				return protocol.LocationSlice{location}, nil
			} else if attempt == 1 {
				return nil, protocol.ErrContentModified
			} else {
				continue
			}
		}
		if current, err := document.workspaceNavigationCurrent(ctx, state, workspaceNavigationTarget{}); err != nil {
			return nil, err
		} else if current {
			return protocol.LocationSlice{}, nil
		} else if attempt == 1 {
			return nil, protocol.ErrContentModified
		}
	}
	return nil, protocol.ErrContentModified
}

func autocmdAugroupReferenceAt(file *syntax.File, offset int) (string, syntax.Span, bool) {
	if file == nil {
		return "", syntax.Span{}, false
	}
	var name string
	var span syntax.Span
	walkCommands(file.Commands, func(command *syntax.Command) {
		if name != "" {
			return
		}
		candidateName, candidateSpan, ok := analysis.AutocmdAugroupReference(file, command)
		if ok && spanContains(candidateSpan, offset) {
			name = candidateName
			span = candidateSpan
		}
	})
	return name, span, name != ""
}

func (document *navigationDocument) augroupDefinitionLocations(state workspaceNavigationSnapshot) (protocol.LocationSlice, []*text.Snapshot) {
	openByPath := make(map[string]*text.Snapshot)
	document.server.publishMu.Lock()
	for _, snapshot := range document.server.documents.Snapshots() {
		path, ok := workspaceURIPath(uri.URI(snapshot.URI()))
		if ok && snapshot.ByteLen() <= maxFileBytes {
			openByPath[path] = snapshot
		}
	}
	document.server.publishMu.Unlock()

	locations := make(protocol.LocationSlice, 0)
	if state.index != nil {
		for _, match := range state.index.AugroupDefinitions(document.augroupName) {
			if openByPath[match.Fact.Path] != nil {
				continue
			}
			snapshot := text.NewSnapshot(uri.File(match.Fact.Path).String(), 0, nil, match.Source)
			if rangeValue, ok := protocolRange(snapshot, document.encoding, match.Fact.Span); ok {
				locations = append(locations, protocol.Location{URI: uri.File(match.Fact.Path), Range: rangeValue})
			}
		}
	}

	checked := make([]*text.Snapshot, 0, len(openByPath))
	currentPath, currentPathOK := workspaceURIPath(uri.URI(document.snapshot.URI()))
	roots := workspaceIndexRoots(state.workspaceRoots, state.runtimePaths)
	for path, snapshot := range openByPath {
		if path != currentPath && !workspacePathInRoots(path, roots) {
			continue
		}
		checked = append(checked, snapshot)
		file := document.server.parseSnapshot(snapshot)
		for _, definition := range analysis.CollectAugroupDefinitions(file) {
			if definition.Name != document.augroupName {
				continue
			}
			if rangeValue, ok := protocolRange(snapshot, document.encoding, definition.Span); ok {
				locations = append(locations, protocol.Location{URI: uri.File(path), Range: rangeValue})
			}
		}
	}
	if !currentPathOK {
		for _, definition := range analysis.CollectAugroupDefinitions(document.analysis.File) {
			if definition.Name == document.augroupName {
				if location, ok := document.location(definition.Span); ok {
					locations = append(locations, location)
				}
			}
		}
	}
	sort.SliceStable(locations, func(left, right int) bool {
		if locations[left].URI != locations[right].URI {
			return locations[left].URI < locations[right].URI
		}
		if locations[left].Range.Start.Line != locations[right].Range.Start.Line {
			return locations[left].Range.Start.Line < locations[right].Range.Start.Line
		}
		return locations[left].Range.Start.Character < locations[right].Range.Start.Character
	})
	return locations, checked
}

func (s *Server) Declaration(ctx context.Context, params *protocol.DeclarationParams) (protocol.DeclarationResult, error) {
	return s.definitionLocations(ctx, params.TextDocumentPositionParams, true)
}

func (s *Server) References(ctx context.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) {
	for attempt := range 2 {
		document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
		if err != nil || document == nil {
			return []protocol.Location{}, err
		}
		workspaceAttempt := document.external != nil
		if !workspaceAttempt {
			_, _, workspaceAttempt = document.workspaceLocalTarget()
		}
		if workspaceAttempt {
			state := s.captureWorkspaceNavigationState()
			target, ok := document.workspaceTargetInState(state)
			if ok {
				if document.externalMember != "" {
					locations, err := document.workspaceMemberReferencesInState(ctx, state, target, params.Context.IncludeDeclaration)
					if errors.Is(err, protocol.ErrContentModified) && attempt == 0 {
						continue
					}
					return locations, err
				}
				locations, err := document.workspaceReferencesInState(ctx, state, target, params.Context.IncludeDeclaration)
				if err != nil {
					return nil, err
				}
				if current, err := document.workspaceNavigationCurrent(ctx, state, target); err != nil {
					return nil, err
				} else if current {
					return locations, nil
				} else if attempt == 1 {
					return nil, protocol.ErrContentModified
				} else {
					continue
				}
			}
			if current, err := document.workspaceNavigationCurrent(ctx, state, workspaceNavigationTarget{}); err != nil {
				return nil, err
			} else if !current {
				if attempt == 1 {
					return nil, protocol.ErrContentModified
				}
				continue
			}
		}
		if document.declaration == nil {
			return []protocol.Location{}, nil
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
	return nil, protocol.ErrContentModified
}

func (s *Server) DocumentHighlight(ctx context.Context, params *protocol.DocumentHighlightParams) ([]protocol.DocumentHighlight, error) {
	for attempt := range 2 {
		document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
		if err != nil || document == nil {
			return []protocol.DocumentHighlight{}, err
		}
		if document.external == nil {
			if document.declaration == nil {
				return []protocol.DocumentHighlight{}, nil
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
		state := s.captureWorkspaceNavigationState()
		target, ok := document.workspaceTargetInState(state)
		if ok && state.resolver != nil {
			if document.externalMember != "" {
				locations, err := document.workspaceMemberReferencesInState(ctx, state, target, false)
				if err != nil {
					if errors.Is(err, protocol.ErrContentModified) && attempt == 0 {
						continue
					}
					return nil, err
				}
				highlights := make([]protocol.DocumentHighlight, 0)
				for _, location := range locations {
					if sameNavigationURI(location.URI, uri.URI(document.snapshot.URI())) {
						highlights = append(highlights, protocol.DocumentHighlight{Range: location.Range, Kind: protocol.DocumentHighlightKindText})
					}
				}
				return highlights, nil
			}
			path, valid := workspaceURIPath(uri.URI(document.snapshot.URI()))
			if valid {
				highlights := make([]protocol.DocumentHighlight, 0)
				for _, reference := range workspace.CollectExternalReferencesFromAnalysis(path, document.analysis.File, document.analysis) {
					if workspaceReferenceMatchesTarget(state, reference, target) {
						if rangeValue, ok := protocolRange(document.snapshot, document.encoding, reference.Span); ok {
							highlights = append(highlights, protocol.DocumentHighlight{Range: rangeValue, Kind: protocol.DocumentHighlightKindText})
						}
					}
				}
				if current, err := document.workspaceNavigationCurrent(ctx, state, target); err != nil {
					return nil, err
				} else if current {
					return highlights, nil
				} else if attempt == 1 {
					return nil, protocol.ErrContentModified
				} else {
					continue
				}
			}
		}
		if current, err := document.workspaceNavigationCurrent(ctx, state, workspaceNavigationTarget{}); err != nil {
			return nil, err
		} else if current {
			return []protocol.DocumentHighlight{}, nil
		} else if attempt == 1 {
			return nil, protocol.ErrContentModified
		}
	}
	return nil, protocol.ErrContentModified
}

func sameNavigationURI(left, right uri.URI) bool {
	leftPath, leftOK := workspaceURIPath(left)
	rightPath, rightOK := workspaceURIPath(right)
	if leftOK && rightOK {
		return sameWorkspacePath(leftPath, rightPath)
	}
	return left == right
}

func (document *navigationDocument) occurrences(includeDeclaration bool) []syntax.Span {
	if document.memberTarget.Start < document.memberTarget.End {
		return document.memberOccurrences(includeDeclaration)
	}
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

func (document *navigationDocument) memberOccurrences(includeDeclaration bool) []syntax.Span {
	spans := make([]syntax.Span, 0)
	if includeDeclaration {
		spans = append(spans, document.memberTarget)
		if document.memberDefinition.Start < document.memberDefinition.End && document.memberDefinition != document.memberTarget {
			spans = append(spans, document.memberDefinition)
		}
	}
	walkCommands(document.analysis.File.Commands, func(command *syntax.Command) {
		walkCommandExpressions(command, func(expression *syntax.Expression) {
			if expression.Kind != syntax.ExpressionMember {
				return
			}
			declaration, definition, resolved := memberNavigationSymbols(document.analysis.File, document.analysis, expression)
			if !resolved || declaration.SelectionRange != document.memberTarget && definition.SelectionRange != document.memberDefinition {
				return
			}
			spans = append(spans, syntax.Span{Start: expression.Operator.End, End: expression.Span.End})
		})
	})
	sort.SliceStable(spans, func(left, right int) bool { return spans[left].Start < spans[right].Start })
	if len(spans) < 2 {
		return spans
	}
	unique := spans[:1]
	for _, span := range spans[1:] {
		if span != unique[len(unique)-1] {
			unique = append(unique, span)
		}
	}
	return unique
}

func (s *Server) Hover(ctx context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	for attempt := range 2 {
		document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
		if err != nil || document == nil {
			return nil, err
		}
		if document.external != nil {
			state := s.captureWorkspaceNavigationState()
			target, ok := document.workspaceTargetInState(state)
			if ok {
				displayName := document.external.Name
				if displayName == "" {
					displayName = target.match.Fact.Name
				}
				kind := analysis.SymbolKind(target.match.Fact.Kind)
				signature := target.match.Fact.Signature
				var contents protocol.HoverContents
				if signature != "" && isFunctionSymbolKind(kind) {
					if displayName != target.match.Fact.Name && strings.HasPrefix(signature, target.match.Fact.Name+"(") {
						signature = displayName + signature[len(target.match.Fact.Name):]
					}
					contents = s.signatureHover(signature, target.match.Fact.Documentation)
				} else {
					_, declaration := s.analyzeWorkspaceTarget(target)
					typeName, _ := hoverDeclarationType(declaration)
					header := hoverDeclarationDescription(displayName, kind, typeName)
					lines := []string{header}
					if typeName != "" && typeName != "unknown" && kind != analysis.SymbolKindVariable && kind != analysis.SymbolKindConstant {
						lines = append(lines, "type: "+typeName)
					}
					if target.match.Fact.Documentation != "" {
						lines = append(lines, "", target.match.Fact.Documentation)
					}
					contents = s.hoverContent(strings.Join(lines, "\n"))
				}
				rangeValue, valid := protocolRange(document.snapshot, document.encoding, document.occurrence)
				if valid {
					if current, err := document.workspaceNavigationCurrent(ctx, state, target); err != nil {
						return nil, err
					} else if current {
						return &protocol.Hover{
							Contents: s.appendRuntimeHelp(document, contents, kind),
							Range:    &rangeValue,
						}, nil
					} else if attempt == 1 {
						return nil, protocol.ErrContentModified
					} else {
						continue
					}
				}
			}
			if current, err := document.workspaceNavigationCurrent(ctx, state, workspaceNavigationTarget{}); err != nil {
				return nil, err
			} else if current {
				if document.external.Kind == workspace.ExternalReferenceGlobalFunction && startsWithUppercaseASCII(document.external.Name) && state.index != nil && !state.index.HasGlobalFunction(document.external.Name) {
					return s.localHoverResult(ctx, document, []string{
						fmt.Sprintf("**%s** A function.", document.external.Name),
						"",
						"function not found in workspace index",
					})
				}
				return s.localHoverContents(ctx, document, nil)
			} else if attempt == 1 {
				return nil, protocol.ErrContentModified
			}
			continue
		}
		hover, err := s.localHover(ctx, document)
		if hover != nil || err != nil {
			return hover, err
		}
		return s.localHoverContents(ctx, document, nil)
	}
	return nil, protocol.ErrContentModified
}

func startsWithUppercaseASCII(name string) bool {
	name = strings.TrimPrefix(name, "g:")
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

func (s *Server) localHover(ctx context.Context, document *navigationDocument) (*protocol.Hover, error) {
	if document.declaration == nil {
		name := document.analysis.File.Text(document.occurrence)
		if document.optionName != "" {
			name = document.optionName
		}
		if contextKind, _ := completionBuiltinStringAt(document.analysis.File, document.occurrence.Start); contextKind == completionContextHasFeature || contextKind == completionContextExpandSpecial {
			var values []vimdata.CompletionValue
			var header string
			if contextKind == completionContextHasFeature {
				values = vimdata.HasFeatures()
				header = fmt.Sprintf("**%s** A has() feature.", name)
			} else {
				values = vimdata.ExpandSpecials()
				header = fmt.Sprintf("**%s** An expand() special.", name)
			}
			for _, value := range values {
				if value.Name == name {
					lines := []string{header}
					if value.Documentation != "" {
						lines = append(lines, "", value.Documentation)
					}
					return s.localHoverResult(ctx, document, lines)
				}
			}
			return nil, nil
		}
		if _, lines, ok := userCommandAttributeHoverAt(document.analysis.File, document.occurrence.Start); ok {
			return s.localHoverResult(ctx, document, lines)
		}
		if span, lines, ok := mappingHoverAt(document.analysis.File, document.occurrence.Start); ok && span == document.occurrence {
			return s.localHoverResult(ctx, document, lines)
		}
		if option, ok := vimdata.LookupOptionMetadata(name); ok {
			documentation := optionDocumentation(option)
			header, body, split := strings.Cut(documentation, "\n\n")
			if split && s.languageFeatures.hoverMarkup == protocol.MarkupKindMarkdown {
				return s.localHoverContents(ctx, document, protocol.MarkedStringSlice{
					protocol.String(boundedDocumentationText(header)),
					protocol.String(boundedDocumentationText(body)),
				})
			}
			return s.localHoverContents(ctx, document, s.hoverContent(documentation))
		}
		if variable, ok := vimdata.LookupVariable(name); ok {
			if variable.Documentation != "" {
				return s.localHoverResult(ctx, document, []string{variable.Documentation})
			}
			var header string
			if variable.Type != "" {
				header = fmt.Sprintf("**%s** A predefined %s variable.", variable.Name, variable.Type)
			} else {
				header = fmt.Sprintf("**%s** A predefined variable.", variable.Name)
			}
			return s.localHoverResult(ctx, document, []string{header})
		}
		function, method, ok := builtinFunctionAt(document.analysis.File, document.occurrence)
		if ok {
			return s.localHoverContents(ctx, document, s.builtinFunctionHover(function))
		}
		if method {
			return s.localHoverResult(ctx, document, []string{fmt.Sprintf("**%s** A function.", name), "", "function not found"})
		}
		if command, ok := exCommandAt(document.analysis.File, document.occurrence); ok && !vimdata.IsNeovimCompatCommand(command.Name) {
			return s.localHoverResult(ctx, document, []string{fmt.Sprintf("**%s** An Ex command.", command.Name)})
		}
		return nil, nil
	}
	declaration := document.declaration
	if isFunctionSymbolKind(declaration.Kind) {
		if functionCommand := functionCommandForDeclaration(document.analysis.File.Commands, declaration); functionCommand != nil {
			signature, _ := workspace.FormatFunctionSignature(document.analysis.File, declaration.Name, functionCommand.Function)
			documentation := workspace.LeadingFunctionDocumentation(document.analysis.File, functionCommand)
			return s.localHoverContents(ctx, document, s.signatureHover(signature, documentation))
		}
	}
	typeName, _ := hoverDeclarationType(declaration)
	line := hoverDeclarationDescription(declaration.Name, declaration.Kind, typeName)
	return s.localHoverResult(ctx, document, []string{line})
}

func exCommandAt(file *syntax.File, span syntax.Span) (vimdata.Command, bool) {
	var result vimdata.Command
	found := false
	walkCommands(file.Commands, func(command *syntax.Command) {
		if found || command.Name != span {
			return
		}
		result, found = vimdata.Lookup(":" + file.Text(command.Name))
	})
	return result, found
}

func builtinFunctionAt(file *syntax.File, span syntax.Span) (vimdata.BuiltinFunction, bool, bool) {
	var result vimdata.BuiltinFunction
	method, found := false, false
	walkCommands(file.Commands, func(command *syntax.Command) {
		if found {
			return
		}
		walkCommandExpressions(command, func(expression *syntax.Expression) {
			if found || expression == nil {
				return
			}
			name := ""
			switch expression.Kind {
			case syntax.ExpressionIdentifier:
				if expression.Span == span {
					name = expression.Value
				}
			case syntax.ExpressionMember:
				if file.Text(expression.Operator) == "->" {
					if memberSpan, ok := expressionMemberSpan(expression); ok && memberSpan == span {
						name, method = expression.Value, true
					}
				}
			}
			if name != "" {
				result, found = vimdata.LookupFunction(name)
			}
		})
	})
	return result, method, found
}

func isFunctionSymbolKind(kind analysis.SymbolKind) bool {
	return kind == analysis.SymbolKindFunction || kind == analysis.SymbolKindMethod || kind == analysis.SymbolKindConstructor
}

// Keep option metadata together before the help prose. Preserve defaults and
// platform-specific notes from the pinned help instead of reconstructing them.
func optionDocumentation(option vimdata.Option) string {
	requirement := optionBuildRequirement(option.AvailableWhen, option.RequiredFeatures)
	lines := strings.Split(option.Documentation, "\n")
	for index, line := range lines {
		if line != "global" && !strings.HasPrefix(line, "global or local to ") && !strings.HasPrefix(line, "local to ") {
			continue
		}
		header := strings.Join(lines[:index], " ")
		prefix, defaults, hasDefaults := strings.Cut(header, "(")
		header = strings.Join(strings.Fields(prefix), " ")
		if hasDefaults {
			header += " (" + defaults
		}
		scope, reference, hasReference := strings.Cut(line, " `")
		metadata := "Scope: **" + scope + "**"
		if hasReference {
			metadata += " `" + reference
		}
		if requirement != "" {
			metadata += " build requirement: " + requirement
		}
		body := strings.TrimSpace(strings.Join(lines[index+1:], "\n"))
		if strings.HasPrefix(body, "{") && requirement != "" {
			if note, rest, found := strings.Cut(body, "}"); found {
				normalized := strings.Join(strings.Fields(strings.ReplaceAll(note+"}", "`", "")), " ")
				if normalized == "{not available when compiled without the "+requirement+" feature}" ||
					normalized == "{only available when compiled with the "+requirement+" feature}" {
					body = strings.TrimSpace(rest)
				}
			}
		}
		return strings.TrimSpace(header + "\n" + metadata + "\n\n" + body)
	}
	documentation := option.Documentation
	if documentation == "" {
		documentation = fmt.Sprintf("**%s** An option.", option.Name)
		if optType := optionTypeName(option); optType != "" {
			documentation = fmt.Sprintf("**%s** %s %s option.", option.Name, titleArticle(optType), optType)
		}
	}
	if requirement != "" {
		documentation += "\n\nbuild requirement: " + requirement
	}
	return documentation
}

func optionBuildRequirement(condition string, features []string) string {
	if condition == "0" {
		return "unavailable in Vim " + vimdata.OptionVimTag
	}
	if len(features) > 0 {
		formatted := make([]string, len(features))
		for i, feature := range features {
			formatted[i] = "+" + feature
		}
		return strings.Join(formatted, ", ")
	}
	return ""
}

func signatureHoverContents(signature string, documentation string, markdown bool) protocol.HoverContents {
	if !markdown {
		value := signature
		if documentation != "" {
			value += "\n\n" + markdownToPlainText(documentation)
		}
		return boundedMarkupContent(protocol.MarkupKindPlainText, value)
	}
	fence := "```"
	for strings.Contains(signature, fence) {
		fence += "`"
	}
	value := fence + "vim\n" + signature + "\n" + fence
	if documentation != "" {
		value += "\n\n" + documentation
	}
	return boundedMarkupContent(protocol.MarkupKindMarkdown, value)
}

func (s *Server) localHoverContents(ctx context.Context, document *navigationDocument, contents protocol.HoverContents) (*protocol.Hover, error) {
	rangeValue, ok := protocolRange(document.snapshot, document.encoding, document.occurrence)
	if !ok {
		return nil, document.checkCurrent(ctx)
	}
	if err := document.checkCurrent(ctx); err != nil {
		return nil, err
	}
	contents = s.appendRuntimeHelp(document, contents, "")
	if contents == nil {
		return nil, nil
	}
	return &protocol.Hover{Contents: contents, Range: &rangeValue}, nil
}

func (s *Server) localHoverResult(ctx context.Context, document *navigationDocument, lines []string) (*protocol.Hover, error) {
	return s.localHoverContents(ctx, document, s.hoverContent(strings.Join(lines, "\n")))
}

func (s *Server) signatureHover(signature string, documentation string) protocol.HoverContents {
	return signatureHoverContents(signature, documentation, s.languageFeatures.hoverMarkup == protocol.MarkupKindMarkdown)
}

func (s *Server) builtinFunctionHover(function vimdata.BuiltinFunction) protocol.HoverContents {
	signature := function.Signature
	if signature == "" {
		signature, _ = formatBuiltinFunctionSignature(function)
	}
	return s.signatureHover(signature, "")
}

func (s *Server) hoverContent(value string) *protocol.MarkupContent {
	if s.languageFeatures.hoverMarkup != protocol.MarkupKindMarkdown {
		value = markdownToPlainText(value)
	}
	return boundedMarkupContent(s.languageFeatures.hoverMarkup, value)
}

func titleArticle(word string) string {
	if word == "" {
		return "A"
	}
	switch unicode.ToLower(rune(word[0])) {
	case 'a', 'e', 'i', 'o', 'u':
		return "An"
	default:
		return "A"
	}
}

func hoverDeclarationDescription(name string, kind analysis.SymbolKind, typeName string) string {
	switch kind {
	case analysis.SymbolKindVariable:
		if typeName != "" {
			return fmt.Sprintf("**%s** %s %s variable.", name, titleArticle(typeName), typeName)
		}
		return fmt.Sprintf("**%s** A variable.", name)
	case analysis.SymbolKindConstant:
		if typeName != "" {
			return fmt.Sprintf("**%s** %s %s constant.", name, titleArticle(typeName), typeName)
		}
		return fmt.Sprintf("**%s** A constant.", name)
	case analysis.SymbolKindFunction:
		return fmt.Sprintf("**%s** A function.", name)
	case analysis.SymbolKindMethod:
		return fmt.Sprintf("**%s** A method.", name)
	case analysis.SymbolKindConstructor:
		return fmt.Sprintf("**%s** A constructor.", name)
	case analysis.SymbolKindClass:
		return fmt.Sprintf("**%s** A class.", name)
	case analysis.SymbolKindInterface:
		return fmt.Sprintf("**%s** An interface.", name)
	case analysis.SymbolKindEnum:
		return fmt.Sprintf("**%s** An enum.", name)
	case analysis.SymbolKindEnumMember:
		return fmt.Sprintf("**%s** An enum member.", name)
	case analysis.SymbolKindTypeAlias:
		if typeName != "" && typeName != "unknown" {
			return fmt.Sprintf("**%s** A type alias for %s.", name, typeName)
		}
		return fmt.Sprintf("**%s** A type alias.", name)
	case analysis.SymbolKindImport:
		return fmt.Sprintf("**%s** An import.", name)
	default:
		kindStr := string(kind)
		if typeName != "" && typeName != "unknown" {
			return fmt.Sprintf("**%s** %s %s %s.", name, titleArticle(typeName), typeName, kindStr)
		}
		return fmt.Sprintf("**%s** %s %s.", name, titleArticle(kindStr), kindStr)
	}
}

func hoverDeclarationType(declaration *analysis.Declaration) (string, bool) {
	if declaration == nil {
		return "", false
	}
	if declaration.Type.Name == "" {
		if declaration.Kind == analysis.SymbolKindVariable || declaration.Kind == analysis.SymbolKindConstant {
			return "unknown", true
		}
		return "", false
	}
	return formatValueType(declaration.Type), true
}

func formatValueType(value analysis.ValueType) string {
	if value.Name == "" {
		return "?"
	}
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

func userCommandAttributeHoverAt(file *syntax.File, offset int) (syntax.Span, []string, bool) {
	if file == nil {
		return syntax.Span{}, nil, false
	}
	_, attribute := completionUserCommandAttributeAt(file, offset)
	if attribute == nil || !spanContains(attribute.Span, offset) {
		return syntax.Span{}, nil, false
	}
	attrName := completionUserCommandAttributeName(file.Text(attribute.Name))
	if attribute.Value.Start < attribute.Value.End && spanContains(attribute.Value, offset) {
		valText := file.Text(attribute.Value)
		valSpan := attribute.Value
		if attrName == "complete" {
			if comma := strings.IndexByte(valText, ','); comma >= 0 {
				methodSpan := syntax.Span{Start: attribute.Value.Start, End: attribute.Value.Start + comma}
				if spanContains(methodSpan, offset) {
					valText = valText[:comma]
					valSpan = methodSpan
				}
			}
		}
		if val, detail, ok := vimdata.LookupUserCommandAttributeValue(attrName, valText); ok {
			lines := []string{fmt.Sprintf("**%s** %s %s value.", val.Name, titleArticle(detail), detail)}
			if val.Documentation != "" {
				lines = append(lines, "", val.Documentation)
			}
			return valSpan, lines, true
		}
	}
	if attr, ok := vimdata.LookupUserCommandAttribute(attrName); ok {
		attrSpan := attribute.Span
		if attribute.Value.Start < attribute.Value.End && !spanContains(attribute.Value, offset) {
			nameEnd := attribute.Name.End
			if attribute.Equal.Start < attribute.Equal.End {
				nameEnd = attribute.Equal.End
			}
			attrSpan = syntax.Span{Start: attribute.Span.Start, End: nameEnd}
		}
		lines := []string{fmt.Sprintf("**-%s** A user command attribute (%s).", attr.Name, attr.Detail)}
		if attr.Documentation != "" {
			lines = append(lines, "", attr.Documentation)
		}
		return attrSpan, lines, true
	}
	return syntax.Span{}, nil, false
}

func mappingHoverAt(file *syntax.File, offset int) (syntax.Span, []string, bool) {
	if file == nil {
		return syntax.Span{}, nil, false
	}
	var (
		foundSpan  syntax.Span
		foundLines []string
		found      bool
	)
	walkCommands(file.Commands, func(command *syntax.Command) {
		if found || command.Mapping == nil {
			return
		}
		mapping := command.Mapping
		for _, modSpan := range mapping.Modifiers {
			if spanContains(modSpan, offset) {
				if item, ok := vimdata.LookupMappingItem(file.Text(modSpan)); ok {
					foundSpan = modSpan
					foundLines = formatMappingItemHover(item)
					found = true
					return
				}
			}
		}
		for _, span := range []syntax.Span{mapping.LHS, mapping.RHS} {
			if spanContains(span, offset) {
				if plugSpan, ok := mappingPlugAt(file.Source, span, offset); ok {
					foundSpan = plugSpan
					// The prefix describes key notation; the parenthesized name
					// identifies a separate runtime help tag.
					if offset < plugSpan.Start+len("<Plug>") {
						item, _ := vimdata.LookupMappingItem("<Plug>")
						foundSpan.End = plugSpan.Start + len("<Plug>")
						foundLines = formatMappingItemHover(item)
					}
					found = true
					return
				}
				if keySpan, keyText, ok := mappingKeyAt(file.Source, span, offset); ok {
					if item, ok := vimdata.LookupMappingItem(keyText); ok {
						foundSpan = keySpan
						foundLines = formatMappingItemHover(item)
						found = true
						return
					}
				}
			}
		}
	})
	return foundSpan, foundLines, found
}

func formatMappingItemHover(item vimdata.MappingItem) []string {
	lines := []string{fmt.Sprintf("**%s** %s", item.Name, item.Detail)}
	if item.Documentation != "" {
		lines = append(lines, "", item.Documentation)
	}
	return lines
}

func mappingPlugAt(source string, container syntax.Span, offset int) (syntax.Span, bool) {
	if container.Start < 0 || container.End > len(source) || offset < container.Start || offset >= container.End {
		return syntax.Span{}, false
	}
	for start := container.Start; start < container.End; start++ {
		if start+len("<Plug>(") > container.End || !strings.EqualFold(source[start:start+len("<Plug>(")], "<Plug>(") {
			continue
		}
		end := start + len("<Plug>(")
		for end < container.End && source[end] != ')' && !strings.ContainsRune(" \t\r\n(", rune(source[end])) {
			end++
		}
		if end == start+len("<Plug>(") || end >= container.End || source[end] != ')' {
			continue
		}
		span := syntax.Span{Start: start, End: end + 1}
		if spanContains(span, offset) {
			return span, true
		}
		start = end
	}
	return syntax.Span{}, false
}

func mappingKeyAt(source string, container syntax.Span, offset int) (syntax.Span, string, bool) {
	if offset < container.Start || offset >= container.End {
		return syntax.Span{}, "", false
	}
	start := -1
	for i := offset; i >= container.Start; i-- {
		if source[i] == '<' {
			start = i
			break
		}
		if source[i] == '>' && i < offset {
			return syntax.Span{}, "", false
		}
	}
	if start < 0 {
		return syntax.Span{}, "", false
	}
	end := -1
	for i := offset; i < container.End; i++ {
		if source[i] == '>' {
			end = i + 1
			break
		}
		if source[i] == '<' && i > start {
			return syntax.Span{}, "", false
		}
	}
	if end < 0 || strings.ContainsAny(source[start:end], " \t\r\n") {
		return syntax.Span{}, "", false
	}
	keySpan := syntax.Span{Start: start, End: end}
	return keySpan, source[start:end], true
}
