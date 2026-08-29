package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chemzqm/vimls-go/internal/syntax"
	"github.com/chemzqm/vimls-go/internal/text"
	"github.com/chemzqm/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

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
	path, err := filepath.Abs(documentURI.FsPath())
	if err != nil || strings.TrimSpace(path) == "" {
		return "", false
	}
	return filepath.Clean(path), true
}

func normalizeWorkspaceRoots(roots []string) []string {
	result := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		absolute, err := filepath.Abs(root)
		if err != nil || strings.TrimSpace(root) == "" {
			continue
		}
		absolute = filepath.Clean(absolute)
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
	s.workspaceRevision++
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
		roots := append([]string(nil), s.workspaceRoots...)
		s.workspaceMu.Unlock()

		index, diskFiles, warnings := s.buildWorkspaceIndex(s.analysisContext, roots)
		if s.analysisContext.Err() != nil {
			s.workspaceMu.Lock()
			s.workspaceRunning = false
			s.workspaceMu.Unlock()
			return
		}

		s.workspaceMu.Lock()
		if revision != s.workspaceRevision {
			s.workspaceMu.Unlock()
			continue
		}
		for _, snapshot := range s.documents.Snapshots() {
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
		s.workspaceIndex = index
		s.workspaceFiles = diskFiles
		s.workspaceRunning = false
		s.workspaceMu.Unlock()
		for _, warning := range warnings {
			_ = s.sendWarning(s.analysisContext, warning)
		}
		return
	}
}

func (s *Server) buildWorkspaceIndex(ctx context.Context, roots []string) (*workspace.Index, map[string]struct{}, []string) {
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
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			paths = append(paths, path)
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
	if !workspacePathInRoots(path, s.workspaceRoots) {
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

func (s *Server) DidChangeWorkspaceFolders(_ context.Context, params *protocol.DidChangeWorkspaceFoldersParams) error {
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
	for position, match := range matches {
		if position%32 == 0 && ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		documentURI := uri.File(match.Fact.Path)
		snapshot := text.NewSnapshot(documentURI.String(), 0, nil, match.Source)
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
