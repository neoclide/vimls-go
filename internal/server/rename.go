package server

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"sort"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

var renameIdentifier = regexp.MustCompile(`^(?:[sglabwtv]:)?[A-Za-z_][A-Za-z0-9_]*$`)

func (s *Server) PrepareRename(ctx context.Context, params *protocol.PrepareRenameParams) (protocol.PrepareRenameResult, error) {
	for attempt := range 2 {
		document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
		if err != nil || document == nil {
			return nil, err
		}
		_, _, workspaceAttempt := document.workspaceLocalTarget()
		workspaceAttempt = workspaceAttempt || document.external != nil
		workspaceTargetResolved := false
		if workspaceAttempt {
			state := s.captureWorkspaceNavigationState()
			target, ok := document.workspaceTargetInState(state)
			if !ok {
				current, err := document.workspaceNavigationCurrent(ctx, state, workspaceNavigationTarget{})
				if err != nil {
					return nil, err
				}
				if !current {
					if attempt == 1 {
						return nil, protocol.ErrContentModified
					}
					continue
				}
				if document.declaration == nil || document.external != nil {
					return nil, nil
				}
			} else {
				if !renameIdentifier.MatchString(target.match.Fact.Name) {
					current, err := document.workspaceNavigationCurrent(ctx, state, target)
					if err != nil {
						return nil, err
					}
					if !current {
						if attempt == 1 {
							return nil, protocol.ErrContentModified
						}
						continue
					}
					return nil, nil
				}
				locations, err := document.workspaceReferencesInState(ctx, state, target, true)
				if err != nil {
					return nil, err
				}
				openLocations, scannedSnapshots, err := s.openWorkspaceReferenceLocationsInState(ctx, state, target, document.encoding)
				if err != nil {
					return nil, err
				}
				locations = normalizeRenameLocations(append(locations, openLocations...))
				_, usedSnapshots, err := s.renameEdits(ctx, state, scannedSnapshots, document.encoding, target.match.Fact.Name, "", locations)
				if err != nil {
					if errors.Is(err, protocol.ErrRequestCancelled) || errors.Is(err, protocol.ErrContentModified) {
						return nil, err
					}
					current, currentErr := document.workspaceNavigationCurrent(ctx, state, target, usedSnapshots...)
					if currentErr != nil {
						return nil, currentErr
					}
					if !current {
						if attempt == 1 {
							return nil, protocol.ErrContentModified
						}
						continue
					}
					return nil, document.checkCurrent(ctx)
				}
				current, err := document.workspaceNavigationCurrent(ctx, state, target, usedSnapshots...)
				if err != nil {
					return nil, err
				}
				if !current {
					if attempt == 1 {
						return nil, protocol.ErrContentModified
					}
					continue
				}
				workspaceTargetResolved = true
			}
		}
		if !workspaceTargetResolved && (document.declaration == nil || document.external != nil) {
			return nil, document.checkCurrent(ctx)
		}
		rangeValue, ok := protocolRange(document.snapshot, document.encoding, document.occurrence)
		if !ok {
			return nil, document.checkCurrent(ctx)
		}
		if err := document.checkCurrent(ctx); err != nil {
			return nil, err
		}
		return &rangeValue, nil
	}
	return nil, protocol.ErrContentModified
}

func (s *Server) Rename(ctx context.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	if !renameIdentifier.MatchString(params.NewName) {
		return nil, jsonrpc2.NewError(jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed), "new name is not a statically valid Vim identifier")
	}
	for attempt := range 2 {
		document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
		if err != nil || document == nil {
			return nil, err
		}
		_, _, workspaceAttempt := document.workspaceLocalTarget()
		workspaceAttempt = workspaceAttempt || document.external != nil
		if workspaceAttempt {
			state := s.captureWorkspaceNavigationState()
			target, ok := document.workspaceTargetInState(state)
			if !ok {
				current, err := document.workspaceNavigationCurrent(ctx, state, workspaceNavigationTarget{})
				if err != nil {
					return nil, err
				}
				if !current {
					if attempt == 1 {
						return nil, protocol.ErrContentModified
					}
					continue
				}
				if document.declaration == nil || document.external != nil {
					return nil, unsafeRenameError()
				}
			} else {
				if !validRenameReplacement(target.match.Fact.Name, params.NewName) {
					current, err := document.workspaceNavigationCurrent(ctx, state, target)
					if err != nil {
						return nil, err
					}
					if !current {
						if attempt == 1 {
							return nil, protocol.ErrContentModified
						}
						continue
					}
					return nil, unsafeRenameError()
				}
				locations, err := document.workspaceReferencesInState(ctx, state, target, true)
				if err != nil {
					return nil, err
				}
				openLocations, scannedSnapshots, err := s.openWorkspaceReferenceLocationsInState(ctx, state, target, document.encoding)
				if err != nil {
					return nil, err
				}
				locations = normalizeRenameLocations(append(locations, openLocations...))
				documentChanges, usedSnapshots, err := s.renameEdits(ctx, state, scannedSnapshots, document.encoding, target.match.Fact.Name, params.NewName, locations)
				if err != nil {
					if errors.Is(err, protocol.ErrRequestCancelled) || errors.Is(err, protocol.ErrContentModified) {
						return nil, err
					}
					current, currentErr := document.workspaceNavigationCurrent(ctx, state, target, usedSnapshots...)
					if currentErr != nil {
						return nil, currentErr
					}
					if !current {
						if attempt == 1 {
							return nil, protocol.ErrContentModified
						}
						continue
					}
					return nil, err
				}
				current, err := document.workspaceNavigationCurrent(ctx, state, target, usedSnapshots...)
				if err != nil {
					return nil, err
				}
				if !current {
					if attempt == 1 {
						return nil, protocol.ErrContentModified
					}
					continue
				}
				return &protocol.WorkspaceEdit{DocumentChanges: documentChanges}, nil
			}
		}
		if document.declaration == nil || document.external != nil {
			return nil, unsafeRenameError()
		}
		if !validRenameReplacement(document.declaration.Name, params.NewName) {
			return nil, unsafeRenameError()
		}
		spans := document.occurrences(true)
		edits := make([]protocol.TextDocumentEditElement, 0, len(spans))
		for _, span := range spans {
			rangeValue, ok := protocolRange(document.snapshot, document.encoding, span)
			if !ok {
				continue
			}
			edit := &protocol.TextEdit{Range: rangeValue, NewText: params.NewName}
			edits = append(edits, edit)
		}
		if len(edits) == 0 {
			return nil, jsonrpc2.NewError(jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed), "symbol cannot be renamed safely")
		}
		sort.Slice(edits, func(i, j int) bool {
			left := edits[i].(*protocol.TextEdit).Range
			right := edits[j].(*protocol.TextEdit).Range
			if left.Start.Line != right.Start.Line {
				return left.Start.Line < right.Start.Line
			}
			return left.Start.Character < right.Start.Character
		})
		if err := document.checkCurrent(ctx); err != nil {
			return nil, err
		}
		version, versioned := document.snapshot.Version()
		var versionPointer *int32
		if versioned {
			versionPointer = &version
		}
		return &protocol.WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{&protocol.TextDocumentEdit{
			TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
				TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: params.TextDocument.URI},
				Version:                versionPointer,
			},
			Edits: edits,
		}}}, nil
	}
	return nil, protocol.ErrContentModified
}

func unsafeRenameError() error {
	return jsonrpc2.NewError(jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed), "symbol cannot be renamed safely")
}

func validRenameReplacement(oldName, newName string) bool {
	if !renameIdentifier.MatchString(oldName) || !renameIdentifier.MatchString(newName) {
		return false
	}
	return renameNamespace(oldName) == renameNamespace(newName)
}

func renameNamespace(name string) string {
	if len(name) >= 2 && name[1] == ':' {
		return name[:2]
	}
	return ""
}

func (s *Server) renameEdits(ctx context.Context, workspaceState workspaceNavigationSnapshot, capturedSnapshots map[uri.URI]*text.Snapshot, encoding text.Encoding, oldName, newName string, locations []protocol.Location) ([]protocol.DocumentChange, []*text.Snapshot, error) {
	type documentState struct {
		uri      uri.URI
		snapshot *text.Snapshot
		edits    []protocol.TextDocumentEditElement
	}
	states := make(map[uri.URI]*documentState)
	openSnapshots := make([]*text.Snapshot, 0)
	for index, location := range locations {
		if index%32 == 0 && ctx.Err() != nil {
			return nil, openSnapshots, protocol.ErrRequestCancelled
		}
		state := states[location.URI]
		if state == nil {
			path, pathOK := workspaceURIPath(location.URI)
			if openSnapshot := capturedSnapshots[location.URI]; openSnapshot != nil {
				state = &documentState{uri: uri.URI(openSnapshot.URI()), snapshot: openSnapshot}
				openSnapshots = append(openSnapshots, openSnapshot)
			} else {
				if !pathOK {
					return nil, openSnapshots, unsafeRenameError()
				}
				source, ok := workspaceState.index.Source(path)
				if !ok {
					return nil, openSnapshots, unsafeRenameError()
				}
				state = &documentState{uri: location.URI, snapshot: text.NewSnapshot(location.URI.String(), 0, nil, source)}
			}
			states[location.URI] = state
		}
		start, startErr := state.snapshot.Offset(fromProtocolPosition(location.Range.Start), encoding)
		end, endErr := state.snapshot.Offset(fromProtocolPosition(location.Range.End), encoding)
		if startErr != nil || endErr != nil || start < 0 || end > len(state.snapshot.Text()) || state.snapshot.Text()[start:end] != oldName {
			return nil, openSnapshots, unsafeRenameError()
		}
		if newName != "" {
			state.edits = append(state.edits, &protocol.TextEdit{Range: location.Range, NewText: newName})
		}
	}
	if newName == "" {
		return nil, openSnapshots, nil
	}
	uris := make([]uri.URI, 0, len(states))
	for documentURI := range states {
		uris = append(uris, documentURI)
	}
	slices.Sort(uris)
	changes := make([]protocol.DocumentChange, 0, len(uris))
	for _, documentURI := range uris {
		state := states[documentURI]
		sort.Slice(state.edits, func(i, j int) bool {
			left := state.edits[i].(*protocol.TextEdit).Range.Start
			right := state.edits[j].(*protocol.TextEdit).Range.Start
			if left.Line != right.Line {
				return left.Line < right.Line
			}
			return left.Character < right.Character
		})
		version, versioned := state.snapshot.Version()
		var versionPointer *int32
		if versioned {
			versionPointer = &version
		}
		changes = append(changes, &protocol.TextDocumentEdit{
			TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: state.uri}, Version: versionPointer},
			Edits:        state.edits,
		})
	}
	return changes, openSnapshots, nil
}

func (s *Server) openWorkspaceReferenceLocations(ctx context.Context, target workspaceNavigationTarget, encoding text.Encoding) ([]protocol.Location, error) {
	locations, _, err := s.openWorkspaceReferenceLocationsInState(ctx, s.captureWorkspaceNavigationState(), target, encoding)
	return locations, err
}

func (s *Server) openWorkspaceReferenceLocationsInState(ctx context.Context, workspaceState workspaceNavigationSnapshot, target workspaceNavigationTarget, encoding text.Encoding) ([]protocol.Location, map[uri.URI]*text.Snapshot, error) {
	if workspaceState.resolver == nil {
		return nil, nil, nil
	}
	locations := make([]protocol.Location, 0)
	snapshots := make(map[uri.URI]*text.Snapshot)
	if target.openSnapshot != nil {
		snapshots[uri.File(target.match.Fact.Path)] = target.openSnapshot
	}
	for _, snapshot := range s.documents.Snapshots() {
		if ctx.Err() != nil {
			return nil, nil, protocol.ErrRequestCancelled
		}
		path, ok := workspaceURIPath(uri.URI(snapshot.URI()))
		if !ok || filepath.Clean(path) == filepath.Clean(target.match.Fact.Path) {
			continue
		}
		file := s.parseSnapshot(snapshot)
		if file == nil {
			continue
		}
		fileAnalysis := analysis.Analyze(file)
		for _, reference := range workspace.CollectExternalReferencesFromAnalysis(path, file, fileAnalysis) {
			if !workspaceReferenceMatchesTarget(workspaceState.resolver, reference, target) {
				continue
			}
			if rangeValue, ok := protocolRange(snapshot, encoding, reference.Span); ok {
				documentURI := uri.File(path)
				locations = append(locations, protocol.Location{URI: documentURI, Range: rangeValue})
				snapshots[documentURI] = snapshot
			}
		}
	}
	return locations, snapshots, nil
}

func normalizeRenameLocations(locations []protocol.Location) []protocol.Location {
	for index := range locations {
		if path, ok := workspaceURIPath(locations[index].URI); ok {
			locations[index].URI = uri.File(path)
		}
	}
	sort.Slice(locations, func(i, j int) bool {
		if locations[i].URI != locations[j].URI {
			return locations[i].URI < locations[j].URI
		}
		if locations[i].Range.Start.Line != locations[j].Range.Start.Line {
			return locations[i].Range.Start.Line < locations[j].Range.Start.Line
		}
		return locations[i].Range.Start.Character < locations[j].Range.Start.Character
	})
	return deduplicateLocations(locations)
}
