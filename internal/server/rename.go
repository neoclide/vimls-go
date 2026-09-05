package server

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

var renameIdentifier = regexp.MustCompile(`^(?:[sglabwtv]:)?[A-Za-z_][A-Za-z0-9_]*$`)

func (s *Server) PrepareRename(ctx context.Context, params *protocol.PrepareRenameParams) (protocol.PrepareRenameResult, error) {
	if err := s.waitForWorkspaceIndex(ctx); err != nil {
		return nil, err
	}
	for attempt := range 2 {
		document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
		if err != nil || document == nil {
			return nil, err
		}
		if document.memberConstructor {
			return nil, document.checkCurrent(ctx)
		}
		_, _, workspaceAttempt := document.workspaceLocalTarget()
		workspaceAttempt = workspaceAttempt || document.external != nil
		workspaceTargetResolved := false
		if workspaceAttempt {
			state := s.captureWorkspaceNavigationState()
			if state.index == nil || !state.index.Complete() {
				if current, currentErr := document.workspaceNavigationCurrent(ctx, state, workspaceNavigationTarget{}); currentErr != nil {
					return nil, currentErr
				} else if !current {
					if attempt == 0 {
						continue
					}
					return nil, protocol.ErrContentModified
				}
				return nil, nil
			}
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
					if errors.Is(err, protocol.ErrContentModified) && attempt == 0 {
						continue
					}
					return nil, err
				}
				openLocations, scannedSnapshots, err := s.openWorkspaceReferenceLocationsInState(ctx, state, target, document.encoding)
				if err != nil {
					return nil, err
				}
				maps.Copy(scannedSnapshots, document.memberSnapshots)
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
	if !validRenameIdentifier(params.NewName) {
		return nil, jsonrpc2.NewError(jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed), "new name is not a statically valid Vim identifier")
	}
	if err := s.waitForWorkspaceIndex(ctx); err != nil {
		return nil, err
	}
	for attempt := range 2 {
		document, err := s.navigationAt(ctx, params.TextDocument.URI.String(), params.Position)
		if err != nil || document == nil {
			return nil, err
		}
		if document.memberConstructor {
			return nil, unsafeRenameError()
		}
		_, _, workspaceAttempt := document.workspaceLocalTarget()
		workspaceAttempt = workspaceAttempt || document.external != nil
		if workspaceAttempt {
			state := s.captureWorkspaceNavigationState()
			if state.index == nil || !state.index.Complete() {
				if current, currentErr := document.workspaceNavigationCurrent(ctx, state, workspaceNavigationTarget{}); currentErr != nil {
					return nil, currentErr
				} else if !current {
					if attempt == 0 {
						continue
					}
					return nil, protocol.ErrContentModified
				}
				return nil, unsafeRenameError()
			}
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
				if !validRenameReplacement(target.match.Fact.Name, params.NewName) || renameGlobalConflict(state, target, params.NewName) {
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
					if errors.Is(err, protocol.ErrContentModified) && attempt == 0 {
						continue
					}
					return nil, err
				}
				openLocations, scannedSnapshots, err := s.openWorkspaceReferenceLocationsInState(ctx, state, target, document.encoding)
				if err != nil {
					return nil, err
				}
				maps.Copy(scannedSnapshots, document.memberSnapshots)
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
			edit := &protocol.TextEdit{Range: rangeValue, NewText: localRenameText(document.analysis.File.Text(span), params.NewName)}
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
		if err := validateRenameBindings(ctx, document.snapshot, document.encoding, edits); err != nil {
			return nil, err
		}
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

func renameGlobalConflict(state workspaceNavigationSnapshot, target workspaceNavigationTarget, newName string) bool {
	fact := target.match.Fact
	_, scriptLocal := serverScriptLocalName(fact.Name)
	global := strings.HasPrefix(fact.Name, "g:") || fact.Dialect == syntax.Legacy && fact.TopLevel && !scriptLocal
	if !global || fact.Name == newName || state.index == nil {
		return false
	}
	return len(state.index.GlobalNameFacts(strings.TrimPrefix(newName, "g:"))) != 0
}

// Validate the proposed text without mutating document/index state. Checking all
// references also detects capture of occurrences that are not being renamed.
func validateRenameBindings(ctx context.Context, snapshot *text.Snapshot, encoding text.Encoding, edits []protocol.TextDocumentEditElement) error {
	if ctx.Err() != nil {
		return protocol.ErrRequestCancelled
	}
	type replacement struct {
		span syntax.Span
		text string
	}
	changes := make([]replacement, 0, len(edits))
	newSize := snapshot.ByteLen()
	for _, element := range edits {
		edit, ok := element.(*protocol.TextEdit)
		if !ok {
			return unsafeRenameError()
		}
		start, startErr := snapshot.Offset(fromProtocolPosition(edit.Range.Start), encoding)
		end, endErr := snapshot.Offset(fromProtocolPosition(edit.Range.End), encoding)
		if startErr != nil || endErr != nil || end < start {
			return unsafeRenameError()
		}
		newSize += len(edit.NewText) - (end - start)
		if newSize > maxFileBytes {
			return unsafeRenameError()
		}
		changes = append(changes, replacement{syntax.Span{Start: start, End: end}, edit.NewText})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].span.Start < changes[j].span.Start })
	var source strings.Builder
	previous := 0
	shifts := make([]int, len(changes)+1)
	for index, change := range changes {
		if change.span.Start < previous {
			return unsafeRenameError()
		}
		source.WriteString(snapshot.Text()[previous:change.span.Start])
		source.WriteString(change.text)
		previous = change.span.End
		shifts[index+1] = shifts[index] + len(change.text) - (change.span.End - change.span.Start)
	}
	source.WriteString(snapshot.Text()[previous:])
	if source.Len() > maxFileBytes {
		return unsafeRenameError()
	}
	mapOffset := func(offset int) int {
		index := sort.Search(len(changes), func(i int) bool { return changes[i].span.End > offset })
		return offset + shifts[index]
	}
	mapSpan := func(span syntax.Span) syntax.Span {
		return syntax.Span{Start: mapOffset(span.Start), End: mapOffset(span.End)}
	}
	beforeFile, afterFile := syntax.Parse(snapshot.Text()), syntax.Parse(source.String())
	before, after := analysis.Analyze(beforeFile), analysis.Analyze(afterFile)
	if ctx.Err() != nil {
		return protocol.ErrRequestCancelled
	}
	if len(before.Declarations) != len(after.Declarations) || len(before.References) != len(after.References) {
		return unsafeRenameError()
	}
	declarations := make(map[syntax.Span]*analysis.Declaration, len(after.Declarations))
	for _, declaration := range after.Declarations {
		declarations[declaration.Span] = declaration
	}
	type declarationKey struct {
		scope *analysis.Scope
		name  string
	}
	seen := make(map[declarationKey]*analysis.Declaration)
	for _, declaration := range before.Declarations {
		renamed := declarations[mapSpan(declaration.Span)]
		if renamed == nil || renamed.Kind != declaration.Kind {
			return unsafeRenameError()
		}
		name := renamed.Name
		if suffix, ok := serverScriptLocalName(name); ok {
			name = "s:" + suffix
		}
		key := declarationKey{renamed.Scope, name}
		if other := seen[key]; other != nil && other.Name != declaration.Name {
			return unsafeRenameError()
		}
		seen[key] = declaration
	}
	references := make(map[syntax.Span]*analysis.Reference, len(after.References))
	for _, reference := range after.References {
		references[reference.Span] = reference
	}
	for _, reference := range before.References {
		renamed := references[mapSpan(reference.Span)]
		if renamed == nil || (reference.Declaration == nil) != (renamed.Declaration == nil) {
			return unsafeRenameError()
		}
		if reference.Declaration != nil && mapSpan(reference.Declaration.Span) != renamed.Declaration.Span {
			return unsafeRenameError()
		}
	}
	type diagnosticKey struct {
		code string
		span syntax.Span
	}
	existing := make(map[diagnosticKey]bool)
	for _, diagnostic := range analysis.CombinedDiagnostics(beforeFile, before) {
		existing[diagnosticKey{diagnostic.Code, mapSpan(diagnostic.Span)}] = true
	}
	for _, diagnostic := range analysis.CombinedDiagnostics(afterFile, after) {
		if strings.HasPrefix(diagnostic.Code, "vim/") && !existing[diagnosticKey{diagnostic.Code, diagnostic.Span}] {
			return unsafeRenameError()
		}
	}
	return nil
}

func validRenameReplacement(oldName, newName string) bool {
	if !validRenameIdentifier(oldName) || !validRenameIdentifier(newName) {
		return false
	}
	return renameNamespace(oldName) == renameNamespace(newName)
}

func validRenameIdentifier(name string) bool {
	if renameIdentifier.MatchString(name) {
		return true
	}
	suffix, ok := serverScriptLocalName(name)
	return ok && renameIdentifier.MatchString(suffix)
}

func renameNamespace(name string) string {
	if _, ok := serverScriptLocalName(name); ok {
		return "s:"
	}
	if len(name) >= 2 && name[1] == ':' {
		return name[:2]
	}
	return ""
}

func localRenameText(oldName, newName string) string {
	newSuffix, newScript := serverScriptLocalName(newName)
	_, oldScript := serverScriptLocalName(oldName)
	if !newScript || !oldScript {
		return newName
	}
	if strings.HasPrefix(oldName, "s:") {
		return "s:" + newSuffix
	}
	return oldName[:len("<SID>")] + newSuffix
}

func serverScriptLocalName(name string) (string, bool) {
	if strings.HasPrefix(name, "s:") && len(name) > 2 {
		return name[2:], true
	}
	if len(name) > len("<SID>") && strings.EqualFold(name[:len("<SID>")], "<SID>") {
		return name[len("<SID>"):], true
	}
	return "", false
}

func (s *Server) renameEdits(ctx context.Context, workspaceState workspaceNavigationSnapshot, capturedSnapshots map[uri.URI]*text.Snapshot, encoding text.Encoding, oldName, newName string, locations []protocol.Location) ([]protocol.DocumentChange, []*text.Snapshot, error) {
	type documentState struct {
		uri        uri.URI
		snapshot   *text.Snapshot
		edits      []protocol.TextDocumentEditElement
		closedPath string
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
				content, readable := readRegularWorkspaceFile(path, maxFileBytes)
				if !readable || string(content) != source {
					return nil, openSnapshots, protocol.ErrContentModified
				}
				state = &documentState{uri: location.URI, snapshot: text.NewSnapshot(location.URI.String(), 0, nil, source), closedPath: path}
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
		if err := validateRenameBindings(ctx, state.snapshot, encoding, state.edits); err != nil {
			return nil, openSnapshots, err
		}
		// Recheck after collecting references and validating the proposed text.
		// Closed files have no client version to reject an already-stale range.
		if state.closedPath != "" {
			content, readable := readRegularWorkspaceFile(state.closedPath, maxFileBytes)
			if !readable || string(content) != state.snapshot.Text() {
				return nil, openSnapshots, protocol.ErrContentModified
			}
		}
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
		file, fileAnalysis := s.analyzeSnapshotContext(ctx, snapshot)
		if file == nil || fileAnalysis == nil {
			continue
		}
		for _, reference := range workspace.CollectExternalReferencesFromAnalysis(path, file, fileAnalysis) {
			if !workspaceReferenceMatchesTarget(workspaceState, reference, target) {
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
