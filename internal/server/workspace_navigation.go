package server

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type workspaceNavigationTarget struct {
	match        workspace.SymbolMatch
	openSnapshot *text.Snapshot
}

type workspaceNavigationSnapshot struct {
	identity workspaceIdentity
	resolver *workspace.PathResolver
	index    *workspace.Index
	roots    []string
}

func (document *navigationDocument) workspaceTarget() (workspaceNavigationTarget, bool) {
	return document.workspaceTargetInState(document.server.captureWorkspaceNavigationState())
}

func (document *navigationDocument) workspaceTargetInState(state workspaceNavigationSnapshot) (workspaceNavigationTarget, bool) {
	if state.resolver == nil || state.index == nil {
		return workspaceNavigationTarget{}, false
	}
	if document.external != nil {
		return document.server.resolveWorkspaceReference(state.resolver, state.index, *document.external)
	}
	target, needsResolver, ok := document.workspaceLocalTarget()
	if !ok || !needsResolver {
		return target, ok
	}
	if state.resolver == nil {
		return workspaceNavigationTarget{}, false
	}
	path, _ := workspaceURIPath(uri.URI(document.snapshot.URI()))
	resolution := state.resolver.ResolveAutoload(target.match.Fact.Name)
	return target, resolution.Path != "" && sameWorkspacePath(resolution.Path, path)
}

func (document *navigationDocument) workspaceLocalTarget() (workspaceNavigationTarget, bool, bool) {
	if document.declaration == nil {
		return workspaceNavigationTarget{}, false, false
	}
	path, ok := workspaceURIPath(uri.URI(document.snapshot.URI()))
	if !ok {
		return workspaceNavigationTarget{}, false, false
	}
	for _, fact := range workspace.CollectSymbolFacts(path, document.analysis.File) {
		if fact.SelectionRange != document.declaration.Span || fact.Name != document.declaration.Name {
			continue
		}
		if fact.Exported {
			return workspaceNavigationTarget{match: workspace.SymbolMatch{Fact: fact, Source: document.snapshot.Text()}, openSnapshot: document.snapshot}, false, true
		}
		if strings.Contains(strings.TrimPrefix(fact.Name, "g:"), "#") {
			return workspaceNavigationTarget{match: workspace.SymbolMatch{Fact: fact, Source: document.snapshot.Text()}, openSnapshot: document.snapshot}, true, true
		}
	}
	return workspaceNavigationTarget{}, false, false
}

func (s *Server) captureWorkspaceNavigationState() workspaceNavigationSnapshot {
	s.workspaceMu.Lock()
	roots := append([]string(nil), s.runtimePaths...)
	if len(roots) == 0 {
		roots = append([]string(nil), s.workspaceRoots...)
	}
	state := workspaceNavigationSnapshot{
		identity: s.workspaceIdentityLocked(),
		resolver: s.workspaceResolver,
		index:    s.workspaceIndex,
		roots:    roots,
	}
	s.workspaceMu.Unlock()
	return state
}

func (s *Server) workspaceNavigationState() (*workspace.PathResolver, *workspace.Index, []string) {
	state := s.captureWorkspaceNavigationState()
	return state.resolver, state.index, state.roots
}

func (s *Server) resolveWorkspaceReference(resolver *workspace.PathResolver, index *workspace.Index, reference workspace.ExternalReferenceFact) (workspaceNavigationTarget, bool) {
	switch reference.Kind {
	case workspace.ExternalReferenceImportMember:
		resolution := resolver.ResolveImportPath(reference.Path, reference.ImportPath, reference.ImportAutoload)
		if resolution.Dynamic || resolution.Path == "" {
			return workspaceNavigationTarget{}, false
		}
		return s.lookupWorkspaceTarget(index, resolution.Path, func(fact workspace.SymbolFact) bool {
			return fact.Exported && fact.Name == reference.Name
		})
	case workspace.ExternalReferenceAutoload:
		resolution := resolver.ResolveAutoload(reference.Name)
		if resolution.Path == "" {
			return workspaceNavigationTarget{}, false
		}
		baseName := reference.Name[strings.LastIndexByte(reference.Name, '#')+1:]
		return s.lookupWorkspaceTarget(index, resolution.Path, func(fact workspace.SymbolFact) bool {
			name := strings.TrimPrefix(fact.Name, "g:")
			return name == reference.Name || fact.Exported && name == baseName
		})
	default:
		return workspaceNavigationTarget{}, false
	}
}

func (s *Server) lookupWorkspaceTarget(index *workspace.Index, path string, accept func(workspace.SymbolFact) bool) (workspaceNavigationTarget, bool) {
	s.publishMu.Lock()
	snapshot, _, open := s.openWorkspaceSnapshotLocked(path)
	s.publishMu.Unlock()
	var candidates []workspace.SymbolMatch
	if open {
		if snapshot.ByteLen() > maxFileBytes {
			return workspaceNavigationTarget{}, false
		}
		file := s.parseSnapshot(snapshot)
		if file == nil {
			return workspaceNavigationTarget{}, false
		}
		for _, fact := range workspace.CollectSymbolFacts(path, file) {
			if accept(fact) {
				candidates = append(candidates, workspace.SymbolMatch{Fact: fact, Source: snapshot.Text()})
			}
		}
	} else {
		for _, match := range index.FileSymbols(path) {
			if sameWorkspacePath(match.Fact.Path, path) && accept(match.Fact) {
				candidates = append(candidates, match)
			}
		}
	}
	if len(candidates) != 1 {
		return workspaceNavigationTarget{}, false
	}
	return workspaceNavigationTarget{match: candidates[0], openSnapshot: snapshot}, true
}

func (s *Server) workspaceTargetLocation(target workspaceNavigationTarget, encoding text.Encoding) (protocol.Location, bool) {
	documentURI := uri.File(target.match.Fact.Path)
	snapshot := text.NewSnapshot(documentURI.String(), 0, nil, target.match.Source)
	rangeValue, ok := protocolRange(snapshot, encoding, target.match.Fact.SelectionRange)
	if !ok {
		return protocol.Location{}, false
	}
	return protocol.Location{URI: documentURI, Range: rangeValue}, true
}

func (document *navigationDocument) checkWorkspaceTarget(ctx context.Context, target workspaceNavigationTarget) error {
	if err := document.checkCurrent(ctx); err != nil {
		return err
	}
	if target.openSnapshot == nil {
		return nil
	}
	current, ok := document.server.documents.Snapshot(target.openSnapshot.URI())
	if !ok || current != target.openSnapshot {
		return protocol.ErrContentModified
	}
	return nil
}

func (document *navigationDocument) workspaceNavigationCurrent(ctx context.Context, state workspaceNavigationSnapshot, target workspaceNavigationTarget, snapshots ...*text.Snapshot) (bool, error) {
	if hook := document.server.beforeWorkspaceIdentityCheck; hook != nil {
		hook()
	}
	document.server.publishMu.Lock()
	defer document.server.publishMu.Unlock()
	if err := document.checkWorkspaceTarget(ctx, target); err != nil {
		return false, err
	}
	for _, snapshot := range snapshots {
		if snapshot == nil || snapshot == target.openSnapshot || snapshot == document.snapshot {
			continue
		}
		current, ok := document.server.documents.Snapshot(snapshot.URI())
		if !ok || current != snapshot {
			return false, protocol.ErrContentModified
		}
	}
	document.server.workspaceMu.Lock()
	current := document.server.workspaceIdentityCurrentLocked(state.identity)
	document.server.workspaceMu.Unlock()
	return current, nil
}

func (document *navigationDocument) workspaceReferencesInState(ctx context.Context, state workspaceNavigationSnapshot, target workspaceNavigationTarget, includeDeclaration bool) ([]protocol.Location, error) {
	if state.resolver == nil || state.index == nil {
		return []protocol.Location{}, document.checkCurrent(ctx)
	}
	locations := make([]protocol.Location, 0)
	targetAnalysis, targetDeclaration := document.server.analyzeWorkspaceTarget(target)
	if includeDeclaration {
		if location, ok := document.server.workspaceTargetLocation(target, document.encoding); ok {
			locations = append(locations, location)
		}
	}
	if targetDeclaration != nil {
		targetURI := uri.File(target.match.Fact.Path)
		targetSnapshot := text.NewSnapshot(targetURI.String(), 0, nil, target.match.Source)
		for _, reference := range targetAnalysis.References {
			if reference.Declaration != targetDeclaration {
				continue
			}
			if rangeValue, ok := protocolRange(targetSnapshot, document.encoding, reference.Span); ok {
				locations = append(locations, protocol.Location{URI: targetURI, Range: rangeValue})
			}
		}
	}

	names := []string{target.match.Fact.Name}
	trimmedName := strings.TrimPrefix(target.match.Fact.Name, "g:")
	if strings.Contains(trimmedName, "#") {
		names = append(names, trimmedName)
	}
	if target.match.Fact.Exported {
		if autoloadName, ok := workspaceAutoloadName(target.match.Fact.Path, target.match.Fact.Name, state.roots); ok {
			names = append(names, autoloadName)
		}
	}
	seenCandidates := make(map[workspace.ExternalReferenceFact]bool)
	candidateSnapshots := make(map[string]*text.Snapshot)
	for _, name := range names {
		for _, candidate := range state.index.ExternalReferences(name) {
			if seenCandidates[candidate.Fact] {
				continue
			}
			seenCandidates[candidate.Fact] = true
			if !workspaceReferenceMatchesTarget(state.resolver, candidate.Fact, target) {
				continue
			}
			candidateURI := uri.File(candidate.Fact.Path)
			candidateSnapshot := candidateSnapshots[candidate.Fact.Path]
			if candidateSnapshot == nil || candidateSnapshot.Text() != candidate.Source {
				candidateSnapshot = text.NewSnapshot(candidateURI.String(), 0, nil, candidate.Source)
				candidateSnapshots[candidate.Fact.Path] = candidateSnapshot
			}
			if rangeValue, ok := protocolRange(candidateSnapshot, document.encoding, candidate.Fact.Span); ok {
				locations = append(locations, protocol.Location{URI: candidateURI, Range: rangeValue})
			}
			if len(locations)%32 == 0 && ctx.Err() != nil {
				return nil, protocol.ErrRequestCancelled
			}
		}
	}
	if document.external != nil && !seenCandidates[*document.external] {
		if workspaceReferenceMatchesTarget(state.resolver, *document.external, target) {
			if location, ok := document.location(document.external.Span); ok {
				locations = append(locations, location)
			}
		}
	}
	for index := range locations {
		if path, ok := workspaceURIPath(locations[index].URI); ok {
			locations[index].URI = uri.File(path)
		}
	}
	sort.SliceStable(locations, func(left, right int) bool {
		if locations[left].URI != locations[right].URI {
			return locations[left].URI < locations[right].URI
		}
		if locations[left].Range.Start != locations[right].Range.Start {
			if locations[left].Range.Start.Line != locations[right].Range.Start.Line {
				return locations[left].Range.Start.Line < locations[right].Range.Start.Line
			}
			return locations[left].Range.Start.Character < locations[right].Range.Start.Character
		}
		return locations[left].Range.End.Character < locations[right].Range.End.Character
	})
	locations = deduplicateLocations(locations)
	if err := document.checkWorkspaceTarget(ctx, target); err != nil {
		return nil, err
	}
	return locations, nil
}

func (s *Server) analyzeWorkspaceTarget(target workspaceNavigationTarget) (*analysis.FileAnalysis, *analysis.Declaration) {
	var file *syntax.File
	if target.openSnapshot != nil && target.openSnapshot.Text() == target.match.Source {
		file = s.parseSnapshot(target.openSnapshot)
	}
	if file == nil {
		file = syntax.Parse(target.match.Source)
	}
	result := analysis.Analyze(file)
	for _, declaration := range result.Declarations {
		if declaration.Span == target.match.Fact.SelectionRange && declaration.Name == target.match.Fact.Name {
			return result, declaration
		}
	}
	return result, nil
}

func workspaceReferenceMatchesTarget(resolver *workspace.PathResolver, reference workspace.ExternalReferenceFact, target workspaceNavigationTarget) bool {
	if resolver == nil {
		return false
	}
	switch reference.Kind {
	case workspace.ExternalReferenceImportMember:
		resolution := resolver.ResolveImportPath(reference.Path, reference.ImportPath, reference.ImportAutoload)
		return !resolution.Dynamic && resolution.Path != "" && sameWorkspacePath(resolution.Path, target.match.Fact.Path) && target.match.Fact.Exported && reference.Name == target.match.Fact.Name
	case workspace.ExternalReferenceAutoload:
		resolution := resolver.ResolveAutoload(reference.Name)
		if resolution.Path == "" || !sameWorkspacePath(resolution.Path, target.match.Fact.Path) {
			return false
		}
		name := strings.TrimPrefix(target.match.Fact.Name, "g:")
		if name == reference.Name {
			return true
		}
		return target.match.Fact.Exported && name == reference.Name[strings.LastIndexByte(reference.Name, '#')+1:]
	default:
		return false
	}
}

func workspaceAutoloadName(path, name string, roots []string) (string, bool) {
	for _, root := range roots {
		relative, ok := workspaceAutoloadPath(path, root)
		if !ok {
			continue
		}
		prefix := strings.TrimSuffix(relative, ".vim")
		prefix = strings.ReplaceAll(prefix, string(filepath.Separator), "#")
		return prefix + "#" + strings.TrimPrefix(name, "g:"), true
	}
	return "", false
}

func workspaceAutoloadPath(path, root string) (string, bool) {
	relative, err := filepath.Rel(filepath.Join(root, "autoload"), path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.Ext(relative) != ".vim" {
		return "", false
	}
	return relative, true
}

func sameWorkspacePath(left, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}

func deduplicateLocations(locations []protocol.Location) []protocol.Location {
	if len(locations) < 2 {
		return locations
	}
	result := locations[:1]
	for _, location := range locations[1:] {
		previous := result[len(result)-1]
		if previous.URI != location.URI || previous.Range != location.Range {
			result = append(result, location)
		}
	}
	return result
}
