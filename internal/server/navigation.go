package server

import (
	"context"
	"errors"
	"sort"
	"strings"

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
	memberSnapshots   map[uri.URI]*text.Snapshot
	memberTarget      syntax.Span
	memberDefinition  syntax.Span
	memberConstructor bool
}

func (s *Server) navigationAt(ctx context.Context, documentURI string, position protocol.Position) (*navigationDocument, error) {
	if err := s.waitForWorkspaceIndex(ctx); err != nil {
		return nil, err
	}
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
	file := s.parseSnapshot(snapshot)
	if file == nil {
		return nil, nil
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
		walkCommands(file.Commands, func(command *syntax.Command) {
			if document.occurrence.Start < document.occurrence.End || !spanContains(command.Name, offset) {
				return
			}
			if _, ok := vimdata.Lookup(file.Text(command.Name)); ok {
				document.occurrence = command.Name
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
				lines := []string{"name: " + displayName, "kind: " + string(target.match.Fact.Kind)}
				if signature := target.match.Fact.Signature; signature != "" {
					if displayName != target.match.Fact.Name && strings.HasPrefix(signature, target.match.Fact.Name+"(") {
						signature = displayName + signature[len(target.match.Fact.Name):]
					}
					lines = append(lines, "signature: "+signature)
				}
				_, declaration := s.analyzeWorkspaceTarget(target)
				if typeName, ok := hoverDeclarationType(declaration); ok {
					lines = append(lines, "type: "+typeName)
				}
				if target.match.Fact.Documentation != "" {
					lines = append(lines, "", target.match.Fact.Documentation)
				}
				rangeValue, valid := protocolRange(document.snapshot, document.encoding, document.occurrence)
				if valid {
					if current, err := document.workspaceNavigationCurrent(ctx, state, target); err != nil {
						return nil, err
					} else if current {
						return &protocol.Hover{
							Contents: s.hoverContent(strings.Join(lines, "\n")),
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
				return nil, nil
			} else if attempt == 1 {
				return nil, protocol.ErrContentModified
			}
			continue
		}
		return s.localHover(ctx, document)
	}
	return nil, protocol.ErrContentModified
}

func (s *Server) localHover(ctx context.Context, document *navigationDocument) (*protocol.Hover, error) {
	if document.declaration == nil {
		name := document.analysis.File.Text(document.occurrence)
		if contextKind, _ := completionBuiltinStringAt(document.analysis.File, document.occurrence.Start); contextKind == completionContextHasFeature || contextKind == completionContextExpandSpecial {
			var values []vimdata.CompletionValue
			kind := "has() feature"
			if contextKind == completionContextHasFeature {
				values = vimdata.HasFeatures()
			} else {
				values = vimdata.ExpandSpecials()
				kind = "expand() special"
			}
			for _, value := range values {
				if value.Name == name {
					lines := []string{"name: " + value.Name, "kind: " + kind}
					if value.Documentation != "" {
						lines = append(lines, "", value.Documentation)
					}
					return s.localHoverResult(ctx, document, lines)
				}
			}
			return nil, nil
		}
		if option, ok := vimdata.LookupOption(name); ok {
			lines := []string{"name: " + option.Name, "kind: option", "type: " + optionTypeName(option)}
			if option.Documentation != "" {
				lines = append(lines, "", option.Documentation)
			}
			return s.localHoverResult(ctx, document, lines)
		}
		if variable, ok := vimdata.LookupVariable(name); ok {
			lines := []string{"name: " + variable.Name, "kind: predefined variable", "type: " + variable.Type}
			if variable.Documentation != "" {
				lines = append(lines, "", variable.Documentation)
			}
			return s.localHoverResult(ctx, document, lines)
		}
		if command, ok := vimdata.Lookup(name); ok && !vimdata.IsNeovimCompatCommand(command.Name) {
			lines := []string{"name: " + command.Name, "kind: Ex command"}
			if command.Documentation != "" {
				lines = append(lines, "", command.Documentation)
			}
			return s.localHoverResult(ctx, document, lines)
		}
		call := callAt(document.analysis.File, document.occurrence.Start)
		if call == nil || len(call.Children) == 0 || call.Children[0].Span != document.occurrence {
			return nil, nil
		}
		function, ok := vimdata.LookupFunction(document.analysis.File.Text(document.occurrence))
		if !ok {
			return nil, nil
		}
		lines := []string{"name: " + function.Name, "kind: builtin function"}
		if returnType := function.ReturnType.DisplayName(); returnType != "" {
			lines = append(lines, "type: "+returnType)
		}
		if function.Documentation != "" {
			lines = append(lines, "", function.Documentation)
		}
		return s.localHoverResult(ctx, document, lines)
	}
	declaration := document.declaration
	lines := []string{"name: " + declaration.Name, "kind: " + string(declaration.Kind)}
	if typeName, ok := hoverDeclarationType(declaration); ok {
		lines = append(lines, "type: "+typeName)
	}
	return s.localHoverResult(ctx, document, lines)
}

func (s *Server) localHoverResult(ctx context.Context, document *navigationDocument, lines []string) (*protocol.Hover, error) {
	rangeValue, ok := protocolRange(document.snapshot, document.encoding, document.occurrence)
	if !ok {
		return nil, document.checkCurrent(ctx)
	}
	if err := document.checkCurrent(ctx); err != nil {
		return nil, err
	}
	return &protocol.Hover{Contents: s.hoverContent(strings.Join(lines, "\n")), Range: &rangeValue}, nil
}

func (s *Server) hoverContent(value string) *protocol.MarkupContent {
	return boundedMarkupContent(s.languageFeatures.hoverMarkup, value)
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
