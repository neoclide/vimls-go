package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type DidChangeRuntimepathParams struct {
	Runtimepath []string `json:"runtimepath"`
}

func workspaceRootsFromInitialize(params *protocol.InitializeParams) []string {
	if params == nil {
		return nil
	}
	if folders, ok := params.WorkspaceFolders.Get(); ok {
		roots := make([]string, 0, len(folders))
		for _, folder := range folders {
			if path, ok := workspaceURIPath(folder.URI); ok {
				roots = append(roots, path)
			}
		}
		return normalizeWorkspaceRoots(roots)
	}
	if params.RootURI != nil {
		if path, ok := workspaceURIPath(*params.RootURI); ok {
			return []string{path}
		}
		return nil
	}
	if rootPath, ok := params.RootPath.Get(); ok {
		return normalizeWorkspaceRoots([]string{rootPath})
	}
	return nil
}

func workspaceURIPath(documentURI uri.URI) (string, bool) {
	if documentURI.IsZero() || !documentURI.IsFile() {
		return "", false
	}
	if strings.TrimSpace(documentURI.FsPath()) == "" {
		return "", false
	}
	path, err := workspace.CanonicalPath(documentURI.FsPath())
	if err != nil {
		return "", false
	}
	return path, true
}

// openWorkspaceSnapshotLocked finds an open document by canonical filesystem
// identity. The caller must hold publishMu so the snapshot and parsed result
// are observed together.
func (s *Server) openWorkspaceSnapshotLocked(path string) (*text.Snapshot, parsedDocument, bool) {
	var snapshot *text.Snapshot
	var parsed parsedDocument
	for _, candidate := range s.documents.Snapshots() {
		candidatePath, ok := workspaceURIPath(uri.URI(candidate.URI()))
		if !ok || !sameWorkspacePath(candidatePath, path) {
			continue
		}
		snapshot = candidate
		parsed = s.parsed[candidate.URI()]
	}
	return snapshot, parsed, snapshot != nil
}

func normalizeWorkspaceRoots(roots []string) []string {
	result := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		absolute, err := workspace.CanonicalPath(root)
		if err != nil {
			continue
		}
		if _, ok := seen[absolute]; ok {
			continue
		}
		seen[absolute] = struct{}{}
		result = append(result, absolute)
	}
	sort.Strings(result)
	return result
}

func (s *Server) setWorkspaceRoots(roots []string) {
	s.workspaceMu.Lock()
	s.workspaceRoots = append([]string(nil), roots...)
	s.workspaceResolver = nil
	s.workspaceRevision++
	s.workspaceMu.Unlock()
}

func (s *Server) setRuntimePaths(paths []string) {
	s.workspaceMu.Lock()
	s.runtimePaths = append([]string(nil), paths...)
	s.workspaceResolver = nil
	s.workspaceRevision++
	s.workspaceMu.Unlock()
}

func workspaceIndexRoots(workspaceRoots, runtimePaths []string) []string {
	return normalizeWorkspaceRoots(append(append([]string(nil), workspaceRoots...), runtimePaths...))
}

func (s *Server) refreshWorkspaceResolver() {
	s.workspaceMu.Lock()
	revision := s.workspaceRevision
	workspaceRoots := append([]string(nil), s.workspaceRoots...)
	runtimePaths := append([]string(nil), s.runtimePaths...)
	s.workspaceMu.Unlock()
	resolver := workspacePathResolver(workspaceRoots, runtimePaths)
	s.workspaceMu.Lock()
	if revision == s.workspaceRevision {
		s.workspaceResolver = resolver
	}
	s.workspaceMu.Unlock()
}

func (s *Server) scheduleWorkspaceRebuild() {
	s.workspaceMu.Lock()
	s.workspaceRevision++
	if s.workspaceRunning || s.analysisContext.Err() != nil {
		s.workspaceMu.Unlock()
		return
	}
	s.workspaceRunning = true
	s.workspaceWG.Add(1)
	s.workspaceMu.Unlock()
	go s.workspaceIndexWorker()
}

func (s *Server) workspaceIndexWorker() {
	defer s.workspaceWG.Done()
	for {
		s.workspaceMu.Lock()
		revision := s.workspaceRevision
		workspaceRoots := append([]string(nil), s.workspaceRoots...)
		runtimePaths := append([]string(nil), s.runtimePaths...)
		resolver := s.workspaceResolver
		s.workspaceMu.Unlock()
		roots := workspaceIndexRoots(workspaceRoots, runtimePaths)
		if resolver == nil {
			resolver = workspacePathResolver(workspaceRoots, runtimePaths)
		}

		index, diskFiles, warnings := s.buildWorkspaceIndex(s.analysisContext, roots, resolver)
		if s.analysisContext.Err() != nil {
			s.workspaceMu.Lock()
			s.workspaceRunning = false
			s.workspaceMu.Unlock()
			return
		}

		openSnapshots := s.documents.Snapshots()
		for _, snapshot := range openSnapshots {
			path, ok := workspaceURIPath(uri.URI(snapshot.URI()))
			if !ok || !workspacePathInRoots(path, roots) {
				continue
			}
			if snapshot.ByteLen() > maxFileBytes {
				index.Remove(path)
				continue
			}
			if err := index.Replace(path, syntax.Parse(snapshot.Text())); err != nil {
				index.Remove(path)
				warnings = appendWarning(warnings, "vimls: workspace index byte limit reached; additional symbols were omitted")
			}
		}

		s.publishMu.Lock()
		if !workspaceSnapshotsCurrent(s.documents.Snapshots(), openSnapshots) {
			s.publishMu.Unlock()
			continue
		}
		s.workspaceMu.Lock()
		if revision != s.workspaceRevision {
			s.workspaceMu.Unlock()
			s.publishMu.Unlock()
			continue
		}
		s.workspaceIndex = index
		s.workspaceResolver = resolver
		s.workspaceFiles = diskFiles
		s.workspaceRunning = false
		s.workspaceMu.Unlock()
		s.publishMu.Unlock()
		for _, warning := range warnings {
			_ = s.sendWarning(s.analysisContext, warning)
		}
		return
	}
}

func workspaceSnapshotsCurrent(current, indexed []*text.Snapshot) bool {
	if len(current) != len(indexed) {
		return false
	}
	for position := range current {
		if current[position] != indexed[position] {
			return false
		}
	}
	return true
}

func (s *Server) buildWorkspaceIndex(ctx context.Context, roots []string, resolver *workspace.PathResolver) (*workspace.Index, map[string]struct{}, []string) {
	index := workspace.NewIndex(maxWorkspaceFiles, maxIndexBytes)
	diskFiles := make(map[string]struct{})
	if len(roots) == 0 || ctx.Err() != nil {
		return index, diskFiles, nil
	}

	paths := make([]string, 0)
	seen := make(map[string]struct{})
	var warnings []string
	for _, root := range roots {
		remaining := maxWorkspaceFiles - len(paths)
		if remaining <= 0 {
			warnings = appendWarning(warnings, "vimls: workspace file limit reached; additional files were omitted")
			break
		}
		files, truncated, err := workspace.DiscoverFiles(root, remaining)
		if err != nil {
			warnings = appendWarning(warnings, fmt.Sprintf("vimls: workspace discovery failed for %s: %v", root, err))
			continue
		}
		for _, path := range files {
			canonical, err := workspace.CanonicalPath(path)
			if err != nil {
				continue
			}
			if _, ok := seen[canonical]; ok {
				continue
			}
			seen[canonical] = struct{}{}
			paths = append(paths, canonical)
		}
		if truncated {
			warnings = appendWarning(warnings, "vimls: workspace file limit reached; additional files were omitted")
			break
		}
	}
	sort.Strings(paths)

	indexedPaths := make([]string, 0, len(paths))
	sources := make([]string, 0, len(paths))
	indexedBytes := 0
	for _, path := range paths {
		if ctx.Err() != nil {
			return workspace.NewIndex(maxWorkspaceFiles, maxIndexBytes), map[string]struct{}{}, nil
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if info.Size() > maxFileBytes {
			continue
		}
		if info.Size() > int64(maxIndexBytes-indexedBytes) {
			warnings = appendWarning(warnings, "vimls: workspace index byte limit reached; additional symbols were omitted")
			break
		}
		content, err := os.ReadFile(path)
		if err != nil {
			s.logf("vimls: read workspace file %s: %v", path, err)
			continue
		}
		if len(content) > maxIndexBytes-indexedBytes {
			warnings = appendWarning(warnings, "vimls: workspace index byte limit reached; additional symbols were omitted")
			break
		}
		indexedBytes += len(content)
		indexedPaths = append(indexedPaths, path)
		sources = append(sources, string(content))
	}
	files := workspace.ParseSources(ctx, sources, 0)
	if ctx.Err() != nil {
		return workspace.NewIndex(maxWorkspaceFiles, maxIndexBytes), map[string]struct{}{}, nil
	}
	for resolver != nil {
		if ctx.Err() != nil {
			return workspace.NewIndex(maxWorkspaceFiles, maxIndexBytes), map[string]struct{}{}, nil
		}
		candidates := make([]string, 0)
		for position, file := range files {
			if position%32 == 0 && ctx.Err() != nil {
				return workspace.NewIndex(maxWorkspaceFiles, maxIndexBytes), map[string]struct{}{}, nil
			}
			if file == nil {
				continue
			}
			for commandIndex := range file.Commands {
				importNode := file.Commands[commandIndex].Import
				if importNode == nil {
					continue
				}
				resolution := resolver.ResolveImport(indexedPaths[position], file, importNode)
				if resolution.Path == "" {
					continue
				}
				path, err := workspace.CanonicalPath(resolution.Path)
				if err != nil {
					continue
				}
				if _, ok := seen[path]; ok {
					continue
				}
				seen[path] = struct{}{}
				candidates = append(candidates, path)
			}
		}
		if len(candidates) == 0 {
			break
		}
		sort.Strings(candidates)
		newPaths := make([]string, 0, len(candidates))
		newSources := make([]string, 0, len(candidates))
		for _, path := range candidates {
			if ctx.Err() != nil {
				return workspace.NewIndex(maxWorkspaceFiles, maxIndexBytes), map[string]struct{}{}, nil
			}
			if len(indexedPaths)+len(newPaths) >= maxWorkspaceFiles {
				warnings = appendWarning(warnings, "vimls: workspace file limit reached; additional files were omitted")
				break
			}
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() || info.Size() > maxFileBytes {
				continue
			}
			if info.Size() > int64(maxIndexBytes-indexedBytes) {
				warnings = appendWarning(warnings, "vimls: workspace index byte limit reached; additional symbols were omitted")
				break
			}
			content, err := os.ReadFile(path)
			if err != nil {
				s.logf("vimls: read imported workspace file %s: %v", path, err)
				continue
			}
			if len(content) > maxIndexBytes-indexedBytes {
				warnings = appendWarning(warnings, "vimls: workspace index byte limit reached; additional symbols were omitted")
				break
			}
			indexedBytes += len(content)
			newPaths = append(newPaths, path)
			newSources = append(newSources, string(content))
		}
		if len(newPaths) == 0 {
			break
		}
		parsed := workspace.ParseSources(ctx, newSources, 0)
		if ctx.Err() != nil {
			return workspace.NewIndex(maxWorkspaceFiles, maxIndexBytes), map[string]struct{}{}, nil
		}
		indexedPaths = append(indexedPaths, newPaths...)
		files = append(files, parsed...)
	}
	for position, file := range files {
		if file == nil {
			continue
		}
		path := indexedPaths[position]
		if err := index.Replace(path, file); err != nil {
			warnings = appendWarning(warnings, "vimls: workspace index byte limit reached; additional symbols were omitted")
			break
		}
		diskFiles[path] = struct{}{}
	}
	return index, diskFiles, warnings
}

func workspacePathResolver(workspaceRoots, runtimePaths []string) *workspace.PathResolver {
	searchPaths := runtimePaths
	if len(searchPaths) == 0 {
		searchPaths = workspaceRoots
	}
	for _, root := range append(append([]string(nil), workspaceRoots...), searchPaths...) {
		resolver, err := workspace.NewPathResolver(root, searchPaths)
		if err == nil {
			return resolver
		}
	}
	return nil
}

func appendWarning(warnings []string, warning string) []string {
	for _, existing := range warnings {
		if existing == warning {
			return warnings
		}
	}
	return append(warnings, warning)
}

func workspacePathInRoots(path string, roots []string) bool {
	for _, root := range roots {
		relative, err := filepath.Rel(root, path)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (s *Server) replaceWorkspaceFile(documentURI string, file *syntax.File) {
	path, ok := workspaceURIPath(uri.URI(documentURI))
	if !ok || file == nil {
		return
	}
	s.workspaceMu.Lock()
	defer s.workspaceMu.Unlock()
	if !workspacePathInRoots(path, workspaceIndexRoots(s.workspaceRoots, s.runtimePaths)) {
		return
	}
	if err := s.workspaceIndex.Replace(path, file); err != nil {
		s.workspaceIndex.Remove(path)
		s.logf("vimls: workspace index limit reached for %s: %v", path, err)
	}
}

func (s *Server) removeWorkspaceURI(documentURI string) {
	path, ok := workspaceURIPath(uri.URI(documentURI))
	if !ok {
		return
	}
	s.workspaceMu.Lock()
	s.workspaceIndex.Remove(path)
	s.workspaceMu.Unlock()
}

func (s *Server) restoreWorkspaceDocument(documentURI string) {
	path, ok := workspaceURIPath(uri.URI(documentURI))
	if !ok {
		return
	}
	s.workspaceMu.Lock()
	_, knownDiskFile := s.workspaceFiles[path]
	index := s.workspaceIndex
	s.workspaceMu.Unlock()
	if !knownDiskFile {
		index.Remove(path)
		return
	}
	content, err := os.ReadFile(path)
	if err != nil || len(content) > maxFileBytes {
		index.Remove(path)
		return
	}
	if err := index.Replace(path, syntax.Parse(string(content))); err != nil {
		index.Remove(path)
	}
}

func (s *Server) DidChangeWorkspaceFolders(ctx context.Context, params *protocol.DidChangeWorkspaceFoldersParams) error {
	s.workspaceMu.Lock()
	roots := append([]string(nil), s.workspaceRoots...)
	s.workspaceMu.Unlock()
	removed := make(map[string]struct{}, len(params.Event.Removed))
	for _, folder := range params.Event.Removed {
		if path, ok := workspaceURIPath(folder.URI); ok {
			removed[path] = struct{}{}
		}
	}
	next := make([]string, 0, len(roots)+len(params.Event.Added))
	for _, root := range roots {
		if _, ok := removed[root]; !ok {
			next = append(next, root)
		}
	}
	for _, folder := range params.Event.Added {
		if path, ok := workspaceURIPath(folder.URI); ok {
			next = append(next, path)
		}
	}
	s.setWorkspaceRoots(normalizeWorkspaceRoots(next))
	s.refreshWorkspaceResolver()
	s.scheduleFileWatchRegistration()
	s.scheduleWorkspaceRebuild()
	return nil
}

func (s *Server) DidChangeRuntimepath(ctx context.Context, params *DidChangeRuntimepathParams) error {
	if params == nil {
		return nil
	}
	s.setRuntimePaths(normalizeWorkspaceRoots(params.Runtimepath))
	s.refreshWorkspaceResolver()
	s.scheduleFileWatchRegistration()
	s.scheduleWorkspaceRebuild()
	return nil
}

// DidChangeWatchedFiles consumes file events produced by the language client.
// The server deliberately does not create filesystem watchers or poll roots.
func (s *Server) DidChangeWatchedFiles(_ context.Context, params *protocol.DidChangeWatchedFilesParams) error {
	if len(params.Changes) > 0 {
		s.scheduleWorkspaceRebuild()
	}
	return nil
}

func (s *Server) scheduleFileWatchRegistration() {
	s.watchMu.Lock()
	if s.analysisContext.Err() != nil {
		s.watchMu.Unlock()
		return
	}
	s.watchWG.Add(1)
	s.watchMu.Unlock()
	go func() {
		defer s.watchWG.Done()
		s.mu.Lock()
		registrationEnabled := s.client != nil && s.watchDynamicRegistration && s.initialized
		s.mu.Unlock()
		if err := s.refreshFileWatchRegistration(s.analysisContext); err != nil && s.analysisContext.Err() == nil {
			s.logf("vimls: refresh Vim file watchers: %v", err)
		}
		// Scan once more after the registration request completes so a file
		// change during the unregister/register window cannot leave the index
		// permanently stale even if the client could not report that event.
		if registrationEnabled && s.analysisContext.Err() == nil {
			s.scheduleWorkspaceRebuild()
		}
	}()
}

func (s *Server) refreshFileWatchRegistration(ctx context.Context) error {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()

	s.mu.Lock()
	client := s.client
	dynamic := s.watchDynamicRegistration
	relative := s.watchRelativePatterns
	initialized := s.initialized
	s.mu.Unlock()
	if client == nil || !dynamic || !initialized {
		return nil
	}
	if s.watchRegistered {
		if err := client.UnregisterCapability(ctx, &protocol.UnregistrationParams{Unregisterations: []protocol.Unregistration{{
			ID: fileWatchRegistrationID, Method: protocol.MethodWorkspaceDidChangeWatchedFiles,
		}}}); err != nil {
			return err
		}
		s.watchRegistered = false
	}
	s.workspaceMu.Lock()
	roots := workspaceIndexRoots(s.workspaceRoots, s.runtimePaths)
	s.workspaceMu.Unlock()
	watchers := vimFileWatchers(roots, relative)
	options, err := protocol.Marshal(protocol.DidChangeWatchedFilesRegistrationOptions{Watchers: watchers})
	if err != nil {
		return err
	}
	err = client.RegisterCapability(ctx, &protocol.RegistrationParams{Registrations: []protocol.Registration{{
		ID: fileWatchRegistrationID, Method: protocol.MethodWorkspaceDidChangeWatchedFiles,
		RegisterOptions: protocol.LSPAny(options),
	}}})
	if err == nil {
		s.watchRegistered = true
	}
	return err
}

func vimFileWatchers(roots []string, relative bool) []protocol.FileSystemWatcher {
	kind := protocol.WatchKindCreate | protocol.WatchKindChange | protocol.WatchKindDelete
	if len(roots) == 0 {
		return []protocol.FileSystemWatcher{{GlobPattern: protocol.Pattern("**/*.vim"), Kind: kind}}
	}
	watchers := make([]protocol.FileSystemWatcher, 0, len(roots))
	for _, root := range roots {
		var pattern protocol.GlobPattern = protocol.Pattern(filepath.ToSlash(filepath.Join(root, "**", "*.vim")))
		if relative {
			pattern = &protocol.RelativePattern{BaseURI: protocol.URI(uri.File(root)), Pattern: protocol.Pattern("**/*.vim")}
		}
		watchers = append(watchers, protocol.FileSystemWatcher{GlobPattern: pattern, Kind: kind})
	}
	return watchers
}

func (s *Server) Symbols(ctx context.Context, params *protocol.WorkspaceSymbolParams) (protocol.WorkspaceSymbolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, protocol.ErrRequestCancelled
	}
	s.workspaceMu.Lock()
	index := s.workspaceIndex
	s.workspaceMu.Unlock()
	s.mu.Lock()
	encoding := s.encoding
	s.mu.Unlock()
	matches := index.Search(params.Query, maxWorkspaceSymbols)
	result := make(protocol.WorkspaceSymbolSlice, 0, len(matches))
	snapshots := make(map[string]*text.Snapshot)
	for position, match := range matches {
		if position%32 == 0 && ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		documentURI := uri.File(match.Fact.Path)
		snapshot := snapshots[match.Fact.Path]
		if snapshot == nil || snapshot.Text() != match.Source {
			snapshot = text.NewSnapshot(documentURI.String(), 0, nil, match.Source)
			snapshots[match.Fact.Path] = snapshot
		}
		rangeValue, ok := protocolRange(snapshot, encoding, match.Fact.SelectionRange)
		if !ok {
			continue
		}
		result = append(result, protocol.WorkspaceSymbol{
			BaseSymbolInformation: protocol.BaseSymbolInformation{Name: match.Fact.Name, Kind: protocolSymbolKind(match.Fact.Kind)},
			Location:              &protocol.Location{URI: documentURI, Range: rangeValue},
		})
	}
	if err := ctx.Err(); err != nil {
		return nil, protocol.ErrRequestCancelled
	}
	return result, nil
}
