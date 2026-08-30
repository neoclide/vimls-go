package server

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/chemzqm/vimls-go/internal/analysis"
	"github.com/chemzqm/vimls-go/internal/syntax"
	"github.com/chemzqm/vimls-go/internal/text"
	"github.com/chemzqm/vimls-go/internal/workspace"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

var renameIdentifier = regexp.MustCompile(`^(?:[sglabwtv]:)?[A-Za-z_][A-Za-z0-9_]*$`)

func (s *Server) PrepareRename(ctx context.Context, params *protocol.PrepareRenameParams) (protocol.PrepareRenameResult, error) {
	document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
	if err != nil || document == nil {
		return nil, err
	}
	if target, ok := document.workspaceTarget(); ok {
		if !renameIdentifier.MatchString(target.match.Fact.Name) {
			return nil, document.checkCurrent(ctx)
		}
		locations, err := document.workspaceReferences(ctx, target, true)
		if err != nil {
			return nil, err
		}
		openLocations, err := s.openWorkspaceReferenceLocations(ctx, target, document.encoding)
		if err != nil {
			return nil, err
		}
		locations = normalizeRenameLocations(append(locations, openLocations...))
		_, snapshots, index, revision, err := s.renameEdits(ctx, document.encoding, target.match.Fact.Name, "", locations)
		if err != nil {
			if errors.Is(err, protocol.ErrRequestCancelled) || errors.Is(err, protocol.ErrContentModified) {
				return nil, err
			}
			return nil, document.checkCurrent(ctx)
		}
		if err := s.checkRenameSnapshots(ctx, snapshots); err != nil {
			return nil, err
		}
		if err := s.checkRenameIndex(index, revision); err != nil {
			return nil, err
		}
	} else if document.declaration == nil || document.external != nil {
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

func (s *Server) Rename(ctx context.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	if !renameIdentifier.MatchString(params.NewName) {
		return nil, jsonrpc2.NewError(jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed), "new name is not a statically valid Vim identifier")
	}
	document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
	if err != nil || document == nil {
		return nil, err
	}
	if target, ok := document.workspaceTarget(); ok {
		if !validRenameReplacement(target.match.Fact.Name, params.NewName) {
			return nil, unsafeRenameError()
		}
		locations, err := document.workspaceReferences(ctx, target, true)
		if err != nil {
			return nil, err
		}
		openLocations, err := s.openWorkspaceReferenceLocations(ctx, target, document.encoding)
		if err != nil {
			return nil, err
		}
		locations = normalizeRenameLocations(append(locations, openLocations...))
		documentChanges, snapshots, index, revision, err := s.renameEdits(ctx, document.encoding, target.match.Fact.Name, params.NewName, locations)
		if err != nil {
			return nil, err
		}
		if err := s.checkRenameSnapshots(ctx, snapshots); err != nil {
			return nil, err
		}
		if err := s.checkRenameIndex(index, revision); err != nil {
			return nil, err
		}
		return &protocol.WorkspaceEdit{DocumentChanges: documentChanges}, nil
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

func (s *Server) renameEdits(ctx context.Context, encoding text.Encoding, oldName, newName string, locations []protocol.Location) ([]protocol.DocumentChange, []*text.Snapshot, *workspace.Index, uint64, error) {
	type documentState struct {
		uri      uri.URI
		snapshot *text.Snapshot
		edits    []protocol.TextDocumentEditElement
	}
	states := make(map[uri.URI]*documentState)
	openSnapshots := make([]*text.Snapshot, 0)
	var closedIndex *workspace.Index
	var closedRevision uint64
	for index, location := range locations {
		if index%32 == 0 && ctx.Err() != nil {
			return nil, nil, nil, 0, protocol.ErrRequestCancelled
		}
		state := states[location.URI]
		if state == nil {
			path, pathOK := workspaceURIPath(location.URI)
			s.publishMu.Lock()
			var openSnapshot *text.Snapshot
			var open bool
			if pathOK {
				openSnapshot, _, open = s.openWorkspaceSnapshotLocked(path)
			}
			s.publishMu.Unlock()
			if open {
				state = &documentState{uri: uri.URI(openSnapshot.URI()), snapshot: openSnapshot}
				openSnapshots = append(openSnapshots, openSnapshot)
			} else {
				if !pathOK {
					return nil, nil, nil, 0, unsafeRenameError()
				}
				s.workspaceMu.Lock()
				currentIndex := s.workspaceIndex
				s.workspaceMu.Unlock()
				if closedIndex == nil {
					closedIndex = currentIndex
					closedRevision = currentIndex.Revision()
				} else if closedIndex != currentIndex {
					return nil, nil, nil, 0, protocol.ErrContentModified
				}
				source, ok := currentIndex.Source(path)
				if !ok {
					return nil, nil, nil, 0, unsafeRenameError()
				}
				state = &documentState{uri: location.URI, snapshot: text.NewSnapshot(location.URI.String(), 0, nil, source)}
			}
			states[location.URI] = state
		}
		start, startErr := state.snapshot.Offset(fromProtocolPosition(location.Range.Start), encoding)
		end, endErr := state.snapshot.Offset(fromProtocolPosition(location.Range.End), encoding)
		if startErr != nil || endErr != nil || start < 0 || end > len(state.snapshot.Text()) || state.snapshot.Text()[start:end] != oldName {
			return nil, nil, nil, 0, unsafeRenameError()
		}
		if newName != "" {
			state.edits = append(state.edits, &protocol.TextEdit{Range: location.Range, NewText: newName})
		}
	}
	if newName == "" {
		return nil, openSnapshots, closedIndex, closedRevision, nil
	}
	uris := make([]uri.URI, 0, len(states))
	for documentURI := range states {
		uris = append(uris, documentURI)
	}
	sort.Slice(uris, func(i, j int) bool { return uris[i] < uris[j] })
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
	return changes, openSnapshots, closedIndex, closedRevision, nil
}

func (s *Server) checkRenameSnapshots(ctx context.Context, snapshots []*text.Snapshot) error {
	if ctx.Err() != nil {
		return protocol.ErrRequestCancelled
	}
	for _, snapshot := range snapshots {
		current, ok := s.documents.Snapshot(snapshot.URI())
		if !ok || current != snapshot {
			return protocol.ErrContentModified
		}
	}
	return nil
}

func (s *Server) checkRenameIndex(index *workspace.Index, revision uint64) error {
	if index == nil {
		return nil
	}
	s.workspaceMu.Lock()
	current := s.workspaceIndex
	s.workspaceMu.Unlock()
	if current != index || index.Revision() != revision {
		return protocol.ErrContentModified
	}
	return nil
}

func (s *Server) openWorkspaceReferenceLocations(ctx context.Context, target workspaceNavigationTarget, encoding text.Encoding) ([]protocol.Location, error) {
	resolver, _, _ := s.workspaceNavigationState()
	if resolver == nil {
		return nil, nil
	}
	locations := make([]protocol.Location, 0)
	for _, snapshot := range s.documents.Snapshots() {
		if ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		path, ok := workspaceURIPath(uri.URI(snapshot.URI()))
		if !ok || filepath.Clean(path) == filepath.Clean(target.match.Fact.Path) {
			continue
		}
		s.publishMu.Lock()
		parsed := s.parsed[snapshot.URI()]
		s.publishMu.Unlock()
		file := parsed.file
		if file == nil || parsed.revision != snapshot.Revision() {
			file = syntax.Parse(snapshot.Text())
		}
		fileAnalysis := analysis.Analyze(file)
		for _, reference := range workspace.CollectExternalReferencesFromAnalysis(path, file, fileAnalysis) {
			if !workspaceReferenceMatchesTarget(resolver, reference, target) {
				continue
			}
			if rangeValue, ok := protocolRange(snapshot, encoding, reference.Span); ok {
				locations = append(locations, protocol.Location{URI: uri.URI(snapshot.URI()), Range: rangeValue})
			}
		}
	}
	return locations, nil
}

func normalizeRenameLocations(locations []protocol.Location) []protocol.Location {
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
