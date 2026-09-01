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
		target, ok := document.server.resolveWorkspaceReference(state, *document.external)
		if !ok || document.externalMember == "" {
			return target, ok
		}
		return document.server.resolveWorkspaceMemberTarget(target, document.externalMember, document.externalClass)
	}
	target, needsResolver, ok := document.workspaceLocalTarget()
	if !ok || !needsResolver {
		return target, ok
	}
	if state.resolver == nil {
		return workspaceNavigationTarget{}, false
	}
	path, _ := workspaceURIPath(uri.URI(document.snapshot.URI()))
	resolution := resolveAutoloadInState(state, target.match.Fact.Name)
	return target, resolution.Path != "" && sameWorkspacePath(resolution.Path, path)
}

func (s *Server) resolveWorkspaceMemberTarget(target workspaceNavigationTarget, name string, classReceiver bool) (workspaceNavigationTarget, bool) {
	if name == "" || strings.HasPrefix(name, "_") {
		return workspaceNavigationTarget{}, false
	}
	file := s.fileForWorkspaceTarget(target)
	if file == nil {
		return workspaceNavigationTarget{}, false
	}
	symbols := analysis.CollectSymbols(file)
	container := symbolForWorkspaceTarget(symbols, target.match.Fact)
	if container != nil && container.Kind == analysis.SymbolKindFunction {
		function := functionForWorkspaceTargetFile(file, target)
		if function == nil || function.ReturnType == nil || strings.Contains(function.ReturnType.Name, ".") {
			return workspaceNavigationTarget{}, false
		}
		container = completionContainer(symbols, function.ReturnType.Name)
	}
	if container == nil {
		return workspaceNavigationTarget{}, false
	}
	symbol, _, ok := memberSymbolInContainer(file, symbols, container, name, classReceiver)
	if !ok {
		return workspaceNavigationTarget{}, false
	}
	for _, fact := range workspace.CollectSymbolFacts(target.match.Fact.Path, file) {
		if fact.SelectionRange == symbol.SelectionRange && fact.Name == symbol.Name && fact.Kind == symbol.Kind {
			return workspaceNavigationTarget{match: workspace.SymbolMatch{Fact: fact, Source: target.match.Source}, openSnapshot: target.openSnapshot}, true
		}
	}
	return workspaceNavigationTarget{}, false
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

func (s *Server) resolveWorkspaceReference(state workspaceNavigationSnapshot, reference workspace.ExternalReferenceFact) (workspaceNavigationTarget, bool) {
	if state.index == nil {
		return workspaceNavigationTarget{}, false
	}
	switch reference.Kind {
	case workspace.ExternalReferenceImportMember:
		resolution := resolveImportPathInState(state, reference.Path, reference.ImportPath, reference.ImportAutoload)
		if resolution.Dynamic || resolution.Path == "" {
			return workspaceNavigationTarget{}, false
		}
		return s.lookupWorkspaceTarget(state.index, resolution.Path, func(fact workspace.SymbolFact) bool {
			return fact.Exported && fact.Name == reference.Name
		})
	case workspace.ExternalReferenceAutoload:
		resolution := resolveAutoloadInState(state, reference.Name)
		if resolution.Path == "" {
			return workspaceNavigationTarget{}, false
		}
		baseName := reference.Name[strings.LastIndexByte(reference.Name, '#')+1:]
		return s.lookupWorkspaceTarget(state.index, resolution.Path, func(fact workspace.SymbolFact) bool {
			name := strings.TrimPrefix(fact.Name, "g:")
			return name == reference.Name || fact.Exported && name == baseName
		})
	case workspace.ExternalReferenceGlobalFunction:
		match, ok := state.index.GlobalFunction(reference.Name)
		if !ok {
			return workspaceNavigationTarget{}, false
		}
		return s.lookupWorkspaceTarget(state.index, match.Fact.Path, func(fact workspace.SymbolFact) bool {
			return fact.SelectionRange == match.Fact.SelectionRange && fact.Kind == analysis.SymbolKindFunction
		})
	default:
		return workspaceNavigationTarget{}, false
	}
}

func resolveImportInState(state workspaceNavigationSnapshot, from string, file *syntax.File, importNode *syntax.Import) workspace.PathResolution {
	if file == nil || importNode == nil {
		return workspace.PathResolution{}
	}
	return resolveImportPathInState(state, from, file.Text(importNode.PathSpan), importNode.Autoload)
}

func resolveImportPathInState(state workspaceNavigationSnapshot, from, raw string, autoload bool) workspace.PathResolution {
	if workspace.RuntimeImport(raw) {
		path, ok := workspace.StaticImportPath(raw)
		if !ok {
			return workspace.PathResolution{Dynamic: true}
		}
		directory := "import"
		if autoload {
			directory = "autoload"
		}
		if state.index != nil {
			if target, found := state.index.RuntimeFile(filepath.ToSlash(filepath.Join(directory, filepath.FromSlash(path)))); found {
				return workspace.PathResolution{Path: target}
			}
		}
		return workspace.PathResolution{}
	}
	if state.resolver == nil {
		return workspace.PathResolution{}
	}
	return state.resolver.ResolveImportPath(from, raw, autoload)
}

func resolveAutoloadInState(state workspaceNavigationSnapshot, name string) workspace.PathResolution {
	relative, ok := workspace.AutoloadPath(name)
	if !ok || state.index == nil {
		return workspace.PathResolution{}
	}
	path, ok := state.index.RuntimeFile(relative)
	if !ok {
		return workspace.PathResolution{}
	}
	return workspace.PathResolution{Path: path}
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
	if document.externalMember != "" {
		return document.workspaceMemberReferencesInState(ctx, state, target, includeDeclaration)
	}
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
			if !workspaceReferenceMatchesTarget(state, candidate.Fact, target) {
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
		if workspaceReferenceMatchesTarget(state, *document.external, target) {
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

func (document *navigationDocument) workspaceMemberReferencesInState(ctx context.Context, state workspaceNavigationSnapshot, target workspaceNavigationTarget, includeDeclaration bool) ([]protocol.Location, error) {
	if state.resolver == nil || state.index == nil || document.external == nil || document.externalMember == "" {
		return []protocol.Location{}, document.checkCurrent(ctx)
	}
	sources := map[string]string{target.match.Fact.Path: target.match.Source}
	if path, ok := workspaceURIPath(uri.URI(document.snapshot.URI())); ok {
		sources[path] = document.snapshot.Text()
	}
	names := []string{document.external.Name}
	targetFile := document.server.fileForWorkspaceTarget(target)
	if targetFile != nil {
		symbols := analysis.CollectSymbols(targetFile)
		if symbol := symbolForWorkspaceTarget(symbols, target.match.Fact); symbol != nil {
			if owner := enclosingAggregateContainer(symbols, symbol.SelectionRange); owner != nil {
				names = append(names, owner.Name)
			}
		}
	}
	for _, name := range names {
		for _, candidate := range state.index.ExternalReferences(name) {
			sources[candidate.Fact.Path] = candidate.Source
		}
	}
	open := make(map[string]*text.Snapshot)
	for _, snapshot := range document.server.documents.Snapshots() {
		if path, ok := workspaceURIPath(uri.URI(snapshot.URI())); ok {
			open[path] = snapshot
			sources[path] = snapshot.Text()
		}
	}
	locations := make([]protocol.Location, 0)
	if includeDeclaration {
		if location, ok := document.server.workspaceTargetLocation(target, document.encoding); ok {
			locations = append(locations, location)
		}
	}
	paths := make([]string, 0, len(sources))
	for path := range sources {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	scanned := make([]*text.Snapshot, 0, len(open))
	for _, path := range paths {
		if ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		snapshot := open[path]
		var file *syntax.File
		if snapshot != nil {
			file = document.server.parseSnapshot(snapshot)
			scanned = append(scanned, snapshot)
		} else {
			snapshot = text.NewSnapshot(uri.File(path).String(), 0, nil, sources[path])
			file = syntax.Parse(sources[path])
		}
		if file == nil {
			continue
		}
		result := analysis.Analyze(file)
		facts := workspace.CollectExternalReferencesFromAnalysis(path, file, result)
		walkCommands(file.Commands, func(command *syntax.Command) {
			walkCommandExpressions(command, func(expression *syntax.Expression) {
				if expression.Kind != syntax.ExpressionMember || expression.Value != document.externalMember || len(expression.Children) != 1 {
					return
				}
				matched := false
				if declaration, definition, ok := memberNavigationSymbols(file, result, expression); ok {
					matched = sameWorkspaceMemberSymbol(path, declaration, target) || sameWorkspaceMemberSymbol(path, definition, target)
				}
				if !matched {
					reference := importedAggregateReferenceForReceiver(path, file, result, expression.Children[0], facts)
					if reference != nil {
						base, ok := document.server.resolveWorkspaceReference(state, *reference)
						if ok {
							member, ok := document.server.resolveWorkspaceMemberTarget(base, expression.Value, importedMemberClassReceiver(file, expression.Children[0]))
							matched = ok && sameWorkspaceMemberTarget(member, target)
						}
					}
				}
				if matched {
					span := syntax.Span{Start: expression.Operator.End, End: expression.Span.End}
					if rangeValue, ok := protocolRange(snapshot, document.encoding, span); ok {
						locations = append(locations, protocol.Location{URI: uri.File(path), Range: rangeValue})
					}
				}
			})
		})
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
		if locations[left].Range.Start.Line != locations[right].Range.Start.Line {
			return locations[left].Range.Start.Line < locations[right].Range.Start.Line
		}
		return locations[left].Range.Start.Character < locations[right].Range.Start.Character
	})
	locations = deduplicateLocations(locations)
	current, err := document.workspaceNavigationCurrent(ctx, state, target, scanned...)
	if err != nil {
		return nil, err
	}
	if !current {
		return nil, protocol.ErrContentModified
	}
	document.memberSnapshots = make(map[uri.URI]*text.Snapshot, len(open))
	for path, snapshot := range open {
		document.memberSnapshots[uri.File(path)] = snapshot
	}
	return locations, nil
}

func sameWorkspaceMemberSymbol(path string, symbol *analysis.Symbol, target workspaceNavigationTarget) bool {
	return symbol != nil && sameWorkspacePath(path, target.match.Fact.Path) && symbol.SelectionRange == target.match.Fact.SelectionRange && symbol.Name == target.match.Fact.Name
}

func sameWorkspaceMemberTarget(left, right workspaceNavigationTarget) bool {
	return sameWorkspacePath(left.match.Fact.Path, right.match.Fact.Path) && left.match.Fact.SelectionRange == right.match.Fact.SelectionRange && left.match.Fact.Name == right.match.Fact.Name
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

func workspaceReferenceMatchesTarget(state workspaceNavigationSnapshot, reference workspace.ExternalReferenceFact, target workspaceNavigationTarget) bool {
	switch reference.Kind {
	case workspace.ExternalReferenceImportMember:
		resolution := resolveImportPathInState(state, reference.Path, reference.ImportPath, reference.ImportAutoload)
		return !resolution.Dynamic && resolution.Path != "" && sameWorkspacePath(resolution.Path, target.match.Fact.Path) && target.match.Fact.Exported && reference.Name == target.match.Fact.Name
	case workspace.ExternalReferenceAutoload:
		resolution := resolveAutoloadInState(state, reference.Name)
		if resolution.Path == "" || !sameWorkspacePath(resolution.Path, target.match.Fact.Path) {
			return false
		}
		name := strings.TrimPrefix(target.match.Fact.Name, "g:")
		if name == reference.Name {
			return true
		}
		return target.match.Fact.Exported && name == reference.Name[strings.LastIndexByte(reference.Name, '#')+1:]
	case workspace.ExternalReferenceGlobalFunction:
		match, ok := state.index.GlobalFunction(reference.Name)
		return ok && sameWorkspacePath(match.Fact.Path, target.match.Fact.Path) && match.Fact.SelectionRange == target.match.Fact.SelectionRange
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
