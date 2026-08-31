package server

import (
	"context"
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
	server      *Server
	snapshot    *text.Snapshot
	encoding    text.Encoding
	analysis    *analysis.FileAnalysis
	declaration *analysis.Declaration
	occurrence  syntax.Span
	external    *workspace.ExternalReferenceFact
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
	if path, ok := workspaceURIPath(uri.URI(documentURI)); ok {
		for _, reference := range workspace.CollectExternalReferencesFromAnalysis(path, file, result) {
			if spanContains(reference.Span, offset) {
				reference := reference
				document.external = &reference
				document.occurrence = reference.Span
				break
			}
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
	return s.definitionLocations(ctx, params.TextDocumentPositionParams)
}

func (s *Server) definitionLocations(ctx context.Context, params protocol.TextDocumentPositionParams) (protocol.LocationSlice, error) {
	for attempt := range 2 {
		document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
		if err != nil || document == nil {
			return protocol.LocationSlice{}, err
		}
		if document.external == nil {
			if document.declaration == nil {
				return protocol.LocationSlice{}, nil
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
	return s.definitionLocations(ctx, params.TextDocumentPositionParams)
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
			path, valid := workspaceURIPath(uri.URI(document.snapshot.URI()))
			if valid {
				highlights := make([]protocol.DocumentHighlight, 0)
				for _, reference := range workspace.CollectExternalReferencesFromAnalysis(path, document.analysis.File, document.analysis) {
					if workspaceReferenceMatchesTarget(state.resolver, reference, target) {
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
	for attempt := range 2 {
		document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
		if err != nil || document == nil {
			return nil, err
		}
		if document.external != nil {
			state := s.captureWorkspaceNavigationState()
			target, ok := document.workspaceTargetInState(state)
			if ok {
				lines := []string{"name: " + target.match.Fact.Name, "kind: " + string(target.match.Fact.Kind)}
				_, declaration := s.analyzeWorkspaceTarget(target)
				if declaration != nil && declaration.Type.Name != "" && declaration.Type.Name != analysis.ValueTypeAny {
					lines = append(lines, "type: "+formatValueType(declaration.Type))
				}
				rangeValue, valid := protocolRange(document.snapshot, document.encoding, document.occurrence)
				if valid {
					if current, err := document.workspaceNavigationCurrent(ctx, state, target); err != nil {
						return nil, err
					} else if current {
						return &protocol.Hover{
							Contents: &protocol.MarkupContent{Kind: protocol.MarkupKindPlainText, Value: strings.Join(lines, "\n")},
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
		rangeValue, ok := protocolRange(document.snapshot, document.encoding, document.occurrence)
		if !ok {
			return nil, document.checkCurrent(ctx)
		}
		if err := document.checkCurrent(ctx); err != nil {
			return nil, err
		}
		return &protocol.Hover{Contents: &protocol.MarkupContent{Kind: protocol.MarkupKindPlainText, Value: strings.Join(lines, "\n")}, Range: &rangeValue}, nil
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
