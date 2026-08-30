package server

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chemzqm/vimls-go/internal/analysis"
	"github.com/chemzqm/vimls-go/internal/syntax"
	"github.com/chemzqm/vimls-go/internal/text"
	"github.com/chemzqm/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type workspaceNavigationTarget struct {
	match        workspace.SymbolMatch
	openSnapshot *text.Snapshot
}

func (document *navigationDocument) workspaceTarget() (workspaceNavigationTarget, bool) {
	resolver, index, _ := document.server.workspaceNavigationState()
	if resolver == nil || index == nil {
		return workspaceNavigationTarget{}, false
	}
	if document.external != nil {
		return document.server.resolveWorkspaceReference(resolver, index, *document.external)
	}
	if document.declaration == nil {
		return workspaceNavigationTarget{}, false
	}
	path, ok := workspaceURIPath(uri.URI(document.snapshot.URI()))
	if !ok {
		return workspaceNavigationTarget{}, false
	}
	for _, fact := range workspace.CollectSymbolFacts(path, document.analysis.File) {
		if fact.SelectionRange != document.declaration.Span || fact.Name != document.declaration.Name {
			continue
		}
		if fact.Exported {
			return workspaceNavigationTarget{match: workspace.SymbolMatch{Fact: fact, Source: document.snapshot.Text()}, openSnapshot: document.snapshot}, true
		}
		if strings.Contains(strings.TrimPrefix(fact.Name, "g:"), "#") {
			resolution := resolver.ResolveAutoload(fact.Name)
			if resolution.Path != "" && sameWorkspacePath(resolution.Path, path) {
				return workspaceNavigationTarget{match: workspace.SymbolMatch{Fact: fact, Source: document.snapshot.Text()}, openSnapshot: document.snapshot}, true
			}
		}
	}
	return workspaceNavigationTarget{}, false
}

func (s *Server) workspaceNavigationState() (*workspace.PathResolver, *workspace.Index, []string) {
	s.workspaceMu.Lock()
	workspaceRoots := append([]string(nil), s.workspaceRoots...)
	runtimePaths := append([]string(nil), s.runtimePaths...)
	index := s.workspaceIndex
	resolver := s.workspaceResolver
	s.workspaceMu.Unlock()
	searchPaths := runtimePaths
	if len(searchPaths) == 0 {
		searchPaths = workspaceRoots
	}
	return resolver, index, searchPaths
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
	documentURI := uri.File(path).String()
	s.publishMu.Lock()
	snapshot, open := s.documents.Snapshot(documentURI)
	parsed := s.parsed[documentURI]
	s.publishMu.Unlock()
	var candidates []workspace.SymbolMatch
	if open {
		if snapshot.ByteLen() > maxFileBytes {
			return workspaceNavigationTarget{}, false
		}
		file := parsed.file
		if file == nil || parsed.revision != snapshot.Revision() {
			file = syntax.Parse(snapshot.Text())
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

func (document *navigationDocument) workspaceReferences(ctx context.Context, target workspaceNavigationTarget, includeDeclaration bool) ([]protocol.Location, error) {
	resolver, index, roots := document.server.workspaceNavigationState()
	if resolver == nil || index == nil {
		return []protocol.Location{}, document.checkCurrent(ctx)
	}
	locations := make([]protocol.Location, 0)
	targetAnalysis, targetDeclaration := analyzeWorkspaceTarget(target)
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
		if autoloadName, ok := workspaceAutoloadName(target.match.Fact.Path, target.match.Fact.Name, roots); ok {
			names = append(names, autoloadName)
		}
	}
	seenCandidates := make(map[workspace.ExternalReferenceFact]bool)
	candidateSnapshots := make(map[string]*text.Snapshot)
	for _, name := range names {
		for _, candidate := range index.ExternalReferences(name) {
			if seenCandidates[candidate.Fact] {
				continue
			}
			seenCandidates[candidate.Fact] = true
			if !workspaceReferenceMatchesTarget(resolver, candidate.Fact, target) {
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
		if workspaceReferenceMatchesTarget(resolver, *document.external, target) {
			if location, ok := document.location(document.external.Span); ok {
				locations = append(locations, location)
			}
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

func analyzeWorkspaceTarget(target workspaceNavigationTarget) (*analysis.FileAnalysis, *analysis.Declaration) {
	result := analysis.Analyze(syntax.Parse(target.match.Source))
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
		autoloadRoot := filepath.Join(root, "autoload")
		relative, err := filepath.Rel(autoloadRoot, path)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.Ext(relative) != ".vim" {
			continue
		}
		prefix := strings.TrimSuffix(relative, ".vim")
		prefix = strings.ReplaceAll(prefix, string(filepath.Separator), "#")
		return prefix + "#" + strings.TrimPrefix(name, "g:"), true
	}
	return "", false
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
