package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const defaultWorkspaceRebuildDebounce = 100 * time.Millisecond
const workspaceIndexWaitTimeout = time.Second
const workspaceProgressCreateTimeout = 100 * time.Millisecond

type DidChangeRuntimepathParams struct {
	Runtimepath []string `json:"runtimepath"`
}

var workspaceGraphReplaceForTest func(*workspace.ImportGraph, string, []workspace.ImportFact) error

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
	legacy := legacyInitializeFields(params)
	if legacy.RootURI != nil {
		if path, ok := workspaceURIPath(*legacy.RootURI); ok {
			return []string{path}
		}
		return nil
	}
	if legacy.RootPath != nil {
		return normalizeWorkspaceRoots([]string{*legacy.RootPath})
	}
	return nil
}

type legacyInitializeRootFields struct {
	RootPath *string  `json:"rootPath"`
	RootURI  *uri.URI `json:"rootUri"`
}

// legacyInitializeFields reads deprecated initialization fields from the wire
// representation for clients that have not adopted workspaceFolders.
func legacyInitializeFields(params *protocol.InitializeParams) legacyInitializeRootFields {
	encoded, err := protocol.Marshal(params)
	if err != nil {
		return legacyInitializeRootFields{}
	}
	var roots legacyInitializeRootFields
	if err := json.Unmarshal(encoded, &roots); err != nil {
		return legacyInitializeRootFields{}
	}
	return roots
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

// usableRuntimePaths keeps only roots that can be read. Runtimepath is an
// optional client input, so unusable entries deliberately have no warning.
func usableRuntimePaths(paths []string) []string {
	paths = normalizeRuntimePaths(paths)
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		directory, err := os.Open(path)
		if err != nil {
			continue
		}
		_, readErr := directory.ReadDir(1)
		closeErr := directory.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) || closeErr != nil {
			continue
		}
		result = append(result, path)
	}
	return result
}

func (s *Server) setWorkspaceRoots(roots []string) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	s.workspaceMu.Lock()
	s.workspaceRoots = append([]string(nil), roots...)
	s.workspaceResolver = nil
	s.resetWorkspaceGraphLocked()
	s.workspaceRevision++
	s.workspaceMu.Unlock()
}

func (s *Server) setRuntimePaths(paths []string) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	s.workspaceMu.Lock()
	s.runtimePaths = append([]string(nil), paths...)
	s.workspaceResolver = nil
	s.resetWorkspaceGraphLocked()
	s.workspaceRevision++
	s.workspaceMu.Unlock()
}

func (s *Server) resetWorkspaceGraphLocked() {
	graph := workspace.NewImportGraph()
	graph.AdvanceRevision(s.workspaceGraphView.Revision())
	s.workspaceGraph = graph
	s.workspaceGraphView = graph.Snapshot()
	s.workspacePending = make(map[string]struct{})
	s.workspaceDependents = make(map[string]struct{})
	s.workspaceBuilt = false
	s.notifyWorkspaceIndexChangedLocked()
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
	if s.analysisStopped || s.analysisContext.Err() != nil {
		s.workspaceMu.Unlock()
		return
	}
	if s.workspaceRunning {
		select {
		case s.workspaceWake <- struct{}{}:
		default:
		}
		s.workspaceMu.Unlock()
		return
	}
	s.workspaceRunning = true
	s.notifyWorkspaceIndexChangedLocked()
	s.workspaceWG.Add(1)
	s.workspaceMu.Unlock()
	go s.workspaceIndexWorker()
}

func (s *Server) workspaceIndexWorker() {
	defer s.workspaceWG.Done()
	timer := time.NewTimer(s.workspaceRebuildDelay())
	defer timer.Stop()
	if s.beforeWorkspaceRebuildDelayForTest != nil {
		s.beforeWorkspaceRebuildDelayForTest()
	}
	for {
		select {
		case <-s.analysisContext.Done():
			s.workspaceMu.Lock()
			s.finishWorkspaceRebuildLocked()
			s.workspaceMu.Unlock()
			return
		case <-s.workspaceWake:
			s.resetWorkspaceRebuildTimer(timer)
			continue
		case <-timer.C:
		}
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
		openSnapshots := s.documents.Snapshots()
		if s.beforeWorkspaceBuildForTest != nil {
			s.beforeWorkspaceBuildForTest(openSnapshots)
		}

		progressClient, progressToken, progressStarted := s.startWorkspaceIndexProgress()
		index, graph, diskFiles, warnings := s.buildWorkspaceIndex(s.analysisContext, roots, runtimePaths, resolver, openSnapshots)
		s.workspaceMu.Lock()
		if s.analysisStopped || s.analysisContext.Err() != nil {
			s.finishWorkspaceRebuildLocked()
			s.workspaceMu.Unlock()
			s.finishWorkspaceIndexProgress(progressClient, progressToken, progressStarted)
			return
		}
		s.workspaceMu.Unlock()

		s.publishMu.Lock()
		if !workspaceSnapshotsCurrent(s.documents.Snapshots(), openSnapshots) {
			s.publishMu.Unlock()
			s.finishWorkspaceIndexProgress(progressClient, progressToken, progressStarted)
			s.resetWorkspaceRebuildTimer(timer)
			continue
		}
		s.workspaceMu.Lock()
		if s.analysisStopped || s.analysisContext.Err() != nil {
			s.finishWorkspaceRebuildLocked()
			s.workspaceMu.Unlock()
			s.publishMu.Unlock()
			s.finishWorkspaceIndexProgress(progressClient, progressToken, progressStarted)
			return
		}
		if revision != s.workspaceRevision {
			s.workspaceMu.Unlock()
			s.publishMu.Unlock()
			s.finishWorkspaceIndexProgress(progressClient, progressToken, progressStarted)
			s.resetWorkspaceRebuildTimer(timer)
			continue
		}
		graph.AdvanceRevision(s.workspaceGraphView.Revision())
		s.workspaceIndex = index
		s.workspaceGraph = graph
		s.workspaceGraphView = graph.Snapshot()
		s.workspaceResolver = resolver
		s.workspaceFiles = diskFiles
		s.workspacePending = make(map[string]struct{})
		s.workspaceDependents = make(map[string]struct{})
		s.workspaceBuilt = true
		indexComplete := index.Complete()
		s.finishWorkspaceRebuildLocked()
		s.workspaceMu.Unlock()
		s.publishMu.Unlock()
		s.scheduleDiagnosticRefresh()
		s.scheduleSemanticTokensRefresh()
		s.scheduleInlayHintRefresh()
		if indexComplete {
			s.scheduleCodeLensRefresh()
		}
		s.finishWorkspaceIndexProgress(progressClient, progressToken, progressStarted)
		for _, warning := range warnings {
			_ = s.sendWarning(s.analysisContext, warning)
		}
		for _, snapshot := range openSnapshots {
			s.startAnalysis(snapshot.URI())
		}
		return
	}
}

// startWorkspaceIndexProgress creates a token before a workspace scan. The
// bounded request avoids holding rebuild completion on an unresponsive client.
func (s *Server) startWorkspaceIndexProgress() (protocol.Client, protocol.ProgressToken, bool) {
	s.mu.Lock()
	client := s.client
	supported := s.workspaceProgress
	s.workspaceProgressID++
	identifier := s.workspaceProgressID
	s.mu.Unlock()
	if client == nil || !supported {
		return nil, nil, false
	}
	token := protocol.String(fmt.Sprintf("vimls-workspace-index-%d", identifier))
	ctx, cancel := context.WithTimeout(s.analysisContext, workspaceProgressCreateTimeout)
	defer cancel()
	if err := client.WorkDoneProgressCreate(ctx, &protocol.WorkDoneProgressCreateParams{Token: token}); err != nil {
		if s.analysisContext.Err() == nil && ctx.Err() == nil {
			s.logf("vimls: create workspace index progress: %v", err)
		}
		return nil, nil, false
	}
	value, err := protocol.Marshal(&protocol.WorkDoneProgressBegin{Kind: "begin", Title: "Indexing workspace"})
	if err != nil {
		s.logf("vimls: encode workspace index progress: %v", err)
		return nil, nil, false
	}
	if err := client.Progress(s.analysisContext, &protocol.ProgressParams{Token: token, Value: protocol.LSPAny(value)}); err != nil {
		if s.analysisContext.Err() == nil {
			s.logf("vimls: send workspace index progress: %v", err)
		}
		return nil, nil, false
	}
	return client, token, true
}

func (s *Server) finishWorkspaceIndexProgress(client protocol.Client, token protocol.ProgressToken, started bool) {
	if !started {
		return
	}
	value, err := protocol.Marshal(&protocol.WorkDoneProgressEnd{Kind: "end"})
	if err == nil {
		_ = client.Progress(s.analysisContext, &protocol.ProgressParams{Token: token, Value: protocol.LSPAny(value)})
	}
}

func (s *Server) finishWorkspaceRebuildLocked() {
	if !s.workspaceRunning {
		return
	}
	s.workspaceRunning = false
	s.notifyWorkspaceIndexChangedLocked()
}

func (s *Server) workspaceIndexBusyLocked() bool {
	return s.workspaceRunning || len(s.workspacePending) > 0
}

func (s *Server) notifyWorkspaceIndexChangedLocked() {
	close(s.workspaceChanged)
	s.workspaceChanged = make(chan struct{})
}

// waitForWorkspaceIndex blocks while workspace index work is active.
func (s *Server) waitForWorkspaceIndex(ctx context.Context) error {
	timer := time.NewTimer(workspaceIndexWaitTimeout)
	defer timer.Stop()
	for {
		s.workspaceMu.Lock()
		busy := s.workspaceIndexBusyLocked()
		changed := s.workspaceChanged
		s.workspaceMu.Unlock()
		if !busy {
			return nil
		}
		if s.beforeWorkspaceIndexWaitForTest != nil {
			s.beforeWorkspaceIndexWaitForTest()
		}
		select {
		case <-ctx.Done():
			return protocol.ErrRequestCancelled
		case <-s.analysisContext.Done():
			return protocol.ErrRequestCancelled
		case <-timer.C:
			return jsonrpc2.NewError(jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed), "workspace index did not become ready within 1s")
		case <-changed:
		}
	}
}

func (s *Server) workspaceIndexRebuilding() bool {
	s.workspaceMu.Lock()
	defer s.workspaceMu.Unlock()
	return s.workspaceIndexBusyLocked()
}

func (s *Server) workspaceRebuildDelay() time.Duration {
	s.workspaceMu.Lock()
	defer s.workspaceMu.Unlock()
	return s.workspaceDelay
}

func (s *Server) resetWorkspaceRebuildTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(s.workspaceRebuildDelay())
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

func (s *Server) buildWorkspaceIndex(ctx context.Context, roots, runtimePaths []string, resolver *workspace.PathResolver, openSnapshots []*text.Snapshot) (*workspace.Index, *workspace.ImportGraph, map[string]struct{}, []string) {
	index := newWorkspaceIndex()
	searchPaths := runtimePaths
	if len(searchPaths) == 0 {
		searchPaths = roots
	}
	index.SetRuntimePaths(searchPaths)
	graph := workspace.NewImportGraph()
	diskFiles := make(map[string]struct{})
	if len(roots) == 0 || ctx.Err() != nil {
		index.SetComplete(ctx.Err() == nil)
		graph.SetReady(ctx.Err() == nil)
		return index, graph, diskFiles, nil
	}
	complete := true

	paths := make([]string, 0)
	seen := make(map[string]struct{})
	openByPath := make(map[string]*text.Snapshot, len(openSnapshots))
	oversizedOpen := make(map[string]struct{})
	discoveredRecoverable := make(map[string]struct{})
	var warnings []string
	for _, root := range roots {
		runtimeRoot := slices.Contains(runtimePaths, root)
		remaining := maxWorkspaceFiles - len(paths)
		if remaining <= 0 {
			complete = false
			warnings = appendWarning(warnings, "vimls: workspace file limit reached; additional files were omitted")
			break
		}
		files, truncated, err := workspace.DiscoverFiles(root, remaining)
		if err != nil {
			if !runtimeRoot {
				complete = false
				warnings = appendWarning(warnings, fmt.Sprintf("vimls: workspace discovery failed for %s: %v", root, err))
			}
			continue
		}
		for _, path := range files {
			canonical, err := workspace.CanonicalPath(path)
			if err != nil {
				if !runtimeRoot {
					complete = false
				}
				continue
			}
			if _, ok := seen[canonical]; ok {
				continue
			}
			seen[canonical] = struct{}{}
			paths = append(paths, canonical)
			if info, err := os.Stat(canonical); err == nil && info.Mode().IsRegular() && info.Size() <= maxFileBytes {
				discoveredRecoverable[canonical] = struct{}{}
			}
		}
		if truncated {
			complete = false
			warnings = appendWarning(warnings, "vimls: workspace file limit reached; additional files were omitted")
			break
		}
	}
	for _, snapshot := range openSnapshots {
		path, ok := workspaceURIPath(uri.URI(snapshot.URI()))
		if !ok || !workspacePathInRoots(path, roots) {
			continue
		}
		if snapshot.ByteLen() > maxFileBytes {
			oversizedOpen[path] = struct{}{}
			continue
		}
		openByPath[path] = snapshot
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	if len(oversizedOpen) > 0 {
		filtered := paths[:0]
		for _, path := range paths {
			if _, oversized := oversizedOpen[path]; !oversized {
				filtered = append(filtered, path)
			} else if _, recoverable := discoveredRecoverable[path]; recoverable {
				diskFiles[path] = struct{}{}
			}
		}
		paths = filtered
	}
	sort.Strings(paths)

	indexedPaths := make([]string, 0, len(paths))
	sources := make([]string, 0, len(paths))
	indexedDiskFiles := make([]bool, 0, len(paths))
	indexedBytes := 0
	readSource := func(path string) (string, bool, bool) {
		info, statErr := os.Stat(path)
		diskFile := statErr == nil && info.Mode().IsRegular()
		if snapshot := openByPath[path]; snapshot != nil {
			return snapshot.Text(), diskFile, true
		}
		if !diskFile || info.Size() > maxFileBytes {
			return "", false, false
		}
		content, err := os.ReadFile(path)
		if err != nil {
			if !workspacePathInRoots(path, runtimePaths) {
				s.logf("vimls: read workspace file %s: %v", path, err)
			}
			return "", false, false
		}
		return string(content), true, true
	}
	for _, path := range paths {
		if ctx.Err() != nil {
			return newWorkspaceIndex(), workspace.NewImportGraph(), map[string]struct{}{}, nil
		}
		source, diskFile, ok := readSource(path)
		if !ok {
			if !workspacePathInRoots(path, runtimePaths) {
				complete = false
			}
			continue
		}
		if len(source) > maxIndexBytes-indexedBytes {
			complete = false
			warnings = appendWarning(warnings, "vimls: workspace index byte limit reached; additional symbols were omitted")
			break
		}
		indexedBytes += len(source)
		indexedPaths = append(indexedPaths, path)
		sources = append(sources, source)
		indexedDiskFiles = append(indexedDiskFiles, diskFile)
	}
	files := workspace.ParseAndAnalyzeSources(ctx, sources, 0)
	if ctx.Err() != nil {
		return newWorkspaceIndex(), workspace.NewImportGraph(), map[string]struct{}{}, nil
	}
	for {
		if ctx.Err() != nil {
			return newWorkspaceIndex(), workspace.NewImportGraph(), map[string]struct{}{}, nil
		}
		candidates := make([]string, 0)
		for position, item := range files {
			if position%32 == 0 && ctx.Err() != nil {
				return newWorkspaceIndex(), workspace.NewImportGraph(), map[string]struct{}{}, nil
			}
			if item.File == nil {
				continue
			}
			for _, fact := range collectWorkspaceImportFacts(indexedPaths[position], item.File, resolver, openByPath) {
				if fact.Target == "" {
					continue
				}
				if _, ok := seen[fact.Target]; ok {
					continue
				}
				seen[fact.Target] = struct{}{}
				candidates = append(candidates, fact.Target)
			}
		}
		if len(candidates) == 0 {
			break
		}
		sort.Strings(candidates)
		newPaths := make([]string, 0, len(candidates))
		newSources := make([]string, 0, len(candidates))
		newDiskFiles := make([]bool, 0, len(candidates))
		for _, path := range candidates {
			if ctx.Err() != nil {
				return newWorkspaceIndex(), workspace.NewImportGraph(), map[string]struct{}{}, nil
			}
			if len(indexedPaths)+len(newPaths) >= maxWorkspaceFiles {
				complete = false
				warnings = appendWarning(warnings, "vimls: workspace file limit reached; additional files were omitted")
				break
			}
			source, diskFile, ok := readSource(path)
			if !ok {
				if !workspacePathInRoots(path, runtimePaths) {
					complete = false
				}
				continue
			}
			if len(source) > maxIndexBytes-indexedBytes {
				complete = false
				warnings = appendWarning(warnings, "vimls: workspace index byte limit reached; additional symbols were omitted")
				break
			}
			indexedBytes += len(source)
			newPaths = append(newPaths, path)
			newSources = append(newSources, source)
			newDiskFiles = append(newDiskFiles, diskFile)
		}
		if len(newPaths) == 0 {
			break
		}
		parsed := workspace.ParseAndAnalyzeSources(ctx, newSources, 0)
		if ctx.Err() != nil {
			return newWorkspaceIndex(), workspace.NewImportGraph(), map[string]struct{}{}, nil
		}
		indexedPaths = append(indexedPaths, newPaths...)
		indexedDiskFiles = append(indexedDiskFiles, newDiskFiles...)
		files = append(files, parsed...)
	}
	indexed := make(map[string]struct{}, len(files))
	for position, item := range files {
		if item.File == nil {
			continue
		}
		path := indexedPaths[position]
		if err := index.ReplaceWithAnalysis(path, item.File, item.Analysis); err != nil {
			complete = false
			warnings = appendWarning(warnings, "vimls: workspace index byte limit reached; additional symbols were omitted")
			break
		}
		indexed[path] = struct{}{}
		if indexedDiskFiles[position] {
			diskFiles[path] = struct{}{}
		}
	}
	for position, item := range files {
		if item.File == nil {
			continue
		}
		path := indexedPaths[position]
		if _, ok := indexed[path]; !ok {
			continue
		}
		facts := retainWorkspaceImportTargets(collectWorkspaceImportFacts(path, item.File, resolver, openByPath), func(target string) bool {
			_, ok := indexed[target]
			return ok
		})
		if err := graph.Replace(path, facts); err != nil {
			complete = false
			index.Remove(path)
			delete(indexed, path)
			delete(diskFiles, path)
		}
	}
	index.SetComplete(complete)
	graph.SetReady(true)
	return index, graph, diskFiles, warnings
}

func collectWorkspaceImportFacts(importer string, file *syntax.File, resolver *workspace.PathResolver, openByPath map[string]*text.Snapshot) []workspace.ImportFact {
	if file == nil {
		return nil
	}
	facts := make([]workspace.ImportFact, 0)
	var collect func([]syntax.Command, []syntax.Block, bool)
	collect = func(commands []syntax.Command, blocks []syntax.Block, deferred bool) {
		for index := range commands {
			command := &commands[index]
			insideFunction := deferred || syntax.CommandInsideFunction(command, blocks)
			if command.Import != nil && !insideFunction {
				importNode := command.Import
				resolution := workspace.PathResolution{Dynamic: true}
				if resolver != nil {
					resolution = resolver.ResolveImport(importer, file, importNode)
				}
				target := resolution.Path
				if target == "" && !resolution.Dynamic {
					for _, candidate := range resolution.Candidates {
						if openByPath[candidate] != nil {
							target = candidate
							break
						}
					}
				}
				facts = append(facts, workspace.ImportFact{
					Target: target, ImportPath: strings.Clone(file.Text(importNode.PathSpan)), PathSpan: importNode.PathSpan,
					Alias: strings.Clone(workspace.ImportAlias(file, importNode)), AliasSpan: importNode.Alias,
					Autoload: importNode.Autoload, Dynamic: resolution.Dynamic,
					Missing: !resolution.Dynamic && target == "" && len(resolution.Candidates) > 0,
				})
			}
			if command.Embedded != nil {
				collect(command.Embedded.Commands, command.Embedded.Blocks, insideFunction)
			}
		}
	}
	collect(file.Commands, file.Blocks, false)
	return facts
}

func retainWorkspaceImportTargets(facts []workspace.ImportFact, known func(string) bool) []workspace.ImportFact {
	for index := range facts {
		if facts[index].Target == "" || known(facts[index].Target) {
			continue
		}
		facts[index].Target = ""
		facts[index].Missing = false
	}
	return facts
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
	if slices.Contains(warnings, warning) {
		return warnings
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

func (s *Server) replaceWorkspaceFile(documentURI string, file *syntax.File) []string {
	_, dependents := s.replaceWorkspaceFileWithSnapshot(documentURI, file)
	return dependents
}

// replaceWorkspaceFileWithSnapshot installs one open-document state and copies
// every cross-file input needed by its analysis under publishMu -> workspaceMu.
// The caller holds publishMu when the document-current check matters.
func (s *Server) replaceWorkspaceFileWithSnapshot(documentURI string, file *syntax.File) (workspaceAnalysisSnapshot, []string) {
	return s.replaceWorkspaceFileWithAnalysisSnapshot(documentURI, file, nil)
}

func (s *Server) replaceWorkspaceFileWithAnalysisSnapshot(documentURI string, file *syntax.File, result *analysis.FileAnalysis) (workspaceAnalysisSnapshot, []string) {
	path, ok := workspaceURIPath(uri.URI(documentURI))
	openByPath := make(map[string]*text.Snapshot)
	for _, snapshot := range s.documents.Snapshots() {
		if openPath, valid := workspaceURIPath(uri.URI(snapshot.URI())); valid && snapshot.ByteLen() <= maxFileBytes {
			openByPath[openPath] = snapshot
		}
	}
	s.workspaceMu.Lock()
	if !ok || !workspacePathInRoots(path, workspaceIndexRoots(s.workspaceRoots, s.runtimePaths)) {
		snapshot := s.workspaceAnalysisSnapshotLocked("", nil)
		s.workspaceMu.Unlock()
		return snapshot, nil
	}
	if file == nil {
		_, indexed := s.workspaceIndex.Source(path)
		_, pending := s.workspacePending[path]
		if indexed || pending || s.workspaceGraphView.Has(path) {
			s.queueWorkspaceDependentsLocked(path)
			s.workspaceIndex.Remove(path)
			s.workspaceGraph.Remove(path)
			delete(s.workspacePending, path)
		}
		if s.workspaceBuilt && len(s.workspacePending) == 0 {
			s.workspaceGraph.SetReady(true)
		}
		s.workspaceGraphView = s.workspaceGraph.Snapshot()
		snapshot := s.workspaceAnalysisSnapshotLocked(path, nil)
		dependents := s.readyWorkspaceDependentsLocked()
		s.notifyWorkspaceIndexChangedLocked()
		s.workspaceMu.Unlock()
		return snapshot, dependents
	}
	sameSource, indexed := s.workspaceIndex.Source(path)
	_, pending := s.workspacePending[path]
	if !pending && indexed && sameSource == file.Source && s.workspaceGraphView.Has(path) {
		snapshot := s.workspaceAnalysisSnapshotLocked(path, file)
		dependents := s.readyWorkspaceDependentsLocked()
		s.workspaceMu.Unlock()
		return snapshot, dependents
	}
	s.queueWorkspaceDependentsLocked(path)
	if err := s.workspaceIndex.ReplaceWithAnalysis(path, file, result); err != nil {
		s.workspaceIndex.SetComplete(false)
		s.workspaceIndex.Remove(path)
		s.workspaceGraph.Remove(path)
		delete(s.workspacePending, path)
		s.logf("vimls: workspace index limit reached for %s: %v", path, err)
	} else {
		facts := retainWorkspaceImportTargets(collectWorkspaceImportFacts(path, file, s.workspaceResolver, openByPath), func(target string) bool {
			if openByPath[target] != nil {
				return true
			}
			_, ok := s.workspaceIndex.Source(target)
			return ok
		})
		var graphErr error
		if workspaceGraphReplaceForTest != nil {
			graphErr = workspaceGraphReplaceForTest(s.workspaceGraph, path, facts)
		} else {
			graphErr = s.workspaceGraph.Replace(path, facts)
		}
		if graphErr != nil {
			s.logf("vimls: update import graph for %s: %v", path, graphErr)
			s.workspaceIndex.Remove(path)
			s.workspaceIndex.SetComplete(false)
			s.workspaceGraph.Remove(path)
			delete(s.workspacePending, path)
		} else {
			delete(s.workspacePending, path)
		}
	}
	if s.workspaceBuilt && len(s.workspacePending) == 0 {
		s.workspaceGraph.SetReady(true)
	}
	s.workspaceGraphView = s.workspaceGraph.Snapshot()
	snapshot := s.workspaceAnalysisSnapshotLocked(path, file)
	dependents := s.readyWorkspaceDependentsLocked()
	s.notifyWorkspaceIndexChangedLocked()
	s.workspaceMu.Unlock()
	return snapshot, dependents
}

func (s *Server) workspaceIdentityLocked() workspaceIdentity {
	identity := workspaceIdentity{generation: s.workspaceRevision, index: s.workspaceIndex, graphRevision: s.workspaceGraphView.Revision()}
	if identity.index != nil {
		identity.indexRevision = identity.index.Revision()
	}
	return identity
}

func (s *Server) workspaceIdentityCurrentLocked(identity workspaceIdentity) bool {
	return identity == s.workspaceIdentityLocked()
}

// workspaceIndexReadyLocked reports whether workspaceIndex can be queried.
// It may still be incomplete, so callers that require a complete index must
// additionally check workspaceIndex.Complete().
func (s *Server) workspaceIndexReadyLocked() bool {
	return s.workspaceBuilt && len(s.workspacePending) == 0 && s.workspaceIndex != nil
}

func (s *Server) workspaceAnalysisSnapshotLocked(path string, file *syntax.File) workspaceAnalysisSnapshot {
	snapshot := workspaceAnalysisSnapshot{
		identity: s.workspaceIdentityLocked(), path: path, graph: s.workspaceGraphView,
		roots: workspaceIndexRoots(s.workspaceRoots, s.runtimePaths), ready: true,
	}
	if path == "" || !workspacePathInRoots(path, snapshot.roots) {
		return snapshot
	}
	if !snapshot.graph.Ready() {
		s.workspaceDependents[path] = struct{}{}
		snapshot.ready = false
		return snapshot
	}
	if s.workspaceIndexReadyLocked() {
		snapshot.globalDiagnostics = s.workspaceIndex.GlobalNameConflictDiagnostics(path, file)
		if s.workspaceIndex.Complete() {
			snapshot.indexComplete = true
			snapshot.userCommandNames = s.workspaceIndex.UserCommandNames()
		}
	}
	imports := snapshot.graph.Imports(path)
	snapshot.targets = make(map[string]importTargetSnapshot)
	for _, importFact := range imports {
		if importFact.Target == "" {
			continue
		}
		if _, exists := snapshot.targets[importFact.Target]; exists {
			continue
		}
		source, sourceOK := s.workspaceIndex.Source(importFact.Target)
		if !sourceOK {
			snapshot.targets[importFact.Target] = importTargetSnapshot{}
			continue
		}
		matches := s.workspaceIndex.FileSymbols(importFact.Target)
		symbols := make([]workspace.SymbolFact, 0, len(matches))
		for _, match := range matches {
			symbols = append(symbols, match.Fact)
		}
		snapshot.targets[importFact.Target] = importTargetSnapshot{source: source, symbols: symbols}
	}
	return snapshot
}

func (s *Server) removeWorkspaceURI(documentURI string) {
	path, ok := workspaceURIPath(uri.URI(documentURI))
	if !ok {
		return
	}
	s.workspaceMu.Lock()
	if !workspacePathInRoots(path, workspaceIndexRoots(s.workspaceRoots, s.runtimePaths)) {
		s.workspaceMu.Unlock()
		return
	}
	s.workspaceIndex.Remove(path)
	s.queueWorkspaceDependentsLocked(path)
	if err := s.workspaceGraph.Replace(path, nil); err == nil {
		s.workspaceGraph.SetReady(false)
		s.workspaceGraphView = s.workspaceGraph.Snapshot()
		s.workspacePending[path] = struct{}{}
		s.workspaceRevision++
		s.notifyWorkspaceIndexChangedLocked()
	}
	s.workspaceMu.Unlock()
}

type workspaceRestore struct {
	documentURI   string
	path          string
	revision      uint64
	knownDiskFile bool
}

// captureWorkspaceRestore records the state that a disk restore must still
// match. The caller performs filesystem I/O only after this method returns.
func (s *Server) captureWorkspaceRestore(documentURI string) (workspaceRestore, bool) {
	path, ok := workspaceURIPath(uri.URI(documentURI))
	if !ok {
		return workspaceRestore{}, false
	}
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if _, open := s.documents.Snapshot(documentURI); open {
		return workspaceRestore{}, false
	}
	s.workspaceMu.Lock()
	defer s.workspaceMu.Unlock()
	if s.analysisStopped || s.analysisContext.Err() != nil || !workspacePathInRoots(path, workspaceIndexRoots(s.workspaceRoots, s.runtimePaths)) {
		return workspaceRestore{}, false
	}
	_, knownDiskFile := s.workspaceFiles[path]
	return workspaceRestore{documentURI: documentURI, path: path, revision: s.workspaceRevision, knownDiskFile: knownDiskFile}, true
}

func (s *Server) restoreWorkspaceDocument(documentURI string) {
	restore, ok := s.captureWorkspaceRestore(documentURI)
	if !ok {
		return
	}
	if s.beforeWorkspaceRestoreReadForTest != nil {
		s.beforeWorkspaceRestoreReadForTest(restore)
	}
	var file *syntax.File
	if !restore.knownDiskFile {
		file = nil
	} else if content, err := os.ReadFile(restore.path); err == nil && len(content) <= maxFileBytes {
		file = syntax.Parse(string(content))
	}
	dependents := s.installWorkspaceRestore(restore, file)
	s.startWorkspaceDependents(dependents)
}

// installWorkspaceRestore conditionally applies a disk result captured by
// captureWorkspaceRestore. It intentionally performs no filesystem I/O or
// parsing while holding server locks.
func (s *Server) installWorkspaceRestore(restore workspaceRestore, file *syntax.File) []string {
	s.publishMu.Lock()
	openSnapshots := s.documents.Snapshots()
	originalOpen := false
	overlayOpen := false
	openByPath := make(map[string]*text.Snapshot)
	for _, snapshot := range openSnapshots {
		if snapshot.URI() == restore.documentURI {
			originalOpen = true
		}
		if openPath, valid := workspaceURIPath(uri.URI(snapshot.URI())); valid {
			if sameWorkspacePath(openPath, restore.path) {
				overlayOpen = true
			}
			if snapshot.ByteLen() <= maxFileBytes {
				openByPath[openPath] = snapshot
			}
		}
	}
	s.workspaceMu.Lock()
	if s.analysisStopped || s.analysisContext.Err() != nil || originalOpen || overlayOpen {
		s.workspaceMu.Unlock()
		s.publishMu.Unlock()
		return nil
	}
	if restore.revision != s.workspaceRevision {
		s.workspaceMu.Unlock()
		s.publishMu.Unlock()
		s.scheduleWorkspaceRebuild()
		return nil
	}
	if !workspacePathInRoots(restore.path, workspaceIndexRoots(s.workspaceRoots, s.runtimePaths)) {
		s.workspaceMu.Unlock()
		s.publishMu.Unlock()
		return nil
	}
	s.queueWorkspaceDependentsLocked(restore.path)
	if file == nil {
		s.workspaceIndex.Remove(restore.path)
		s.workspaceGraph.Remove(restore.path)
	} else {
		if err := s.workspaceIndex.Replace(restore.path, file); err != nil {
			s.workspaceIndex.SetComplete(false)
			s.workspaceIndex.Remove(restore.path)
			s.workspaceGraph.Remove(restore.path)
		} else {
			facts := retainWorkspaceImportTargets(collectWorkspaceImportFacts(restore.path, file, s.workspaceResolver, openByPath), func(target string) bool {
				if openByPath[target] != nil {
					return true
				}
				_, ok := s.workspaceIndex.Source(target)
				return ok
			})
			if err := s.workspaceGraph.Replace(restore.path, facts); err != nil {
				s.logf("vimls: restore import graph for %s: %v", restore.path, err)
				s.workspaceIndex.Remove(restore.path)
				s.workspaceIndex.SetComplete(false)
				s.workspaceGraph.Remove(restore.path)
			}
		}
	}
	delete(s.workspacePending, restore.path)
	if s.workspaceBuilt && len(s.workspacePending) == 0 {
		s.workspaceGraph.SetReady(true)
	}
	s.workspaceGraphView = s.workspaceGraph.Snapshot()
	s.workspaceRevision++
	dependents := s.readyWorkspaceDependentsLocked()
	s.notifyWorkspaceIndexChangedLocked()
	s.workspaceMu.Unlock()
	s.publishMu.Unlock()
	return dependents
}

func (s *Server) queueWorkspaceDependentsLocked(path string) {
	for _, dependent := range s.workspaceGraphView.ReverseDependents(path) {
		s.workspaceDependents[dependent] = struct{}{}
	}
}

func (s *Server) readyWorkspaceDependentsLocked() []string {
	if !s.workspaceGraphView.Ready() || len(s.workspaceDependents) == 0 {
		return nil
	}
	paths := make([]string, 0, len(s.workspaceDependents))
	for path := range s.workspaceDependents {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	s.workspaceDependents = make(map[string]struct{})
	return paths
}

func (s *Server) startWorkspaceDependents(paths []string) {
	if len(paths) == 0 {
		return
	}
	wanted := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		wanted[path] = struct{}{}
	}
	for _, snapshot := range s.documents.Snapshots() {
		path, ok := workspaceURIPath(uri.URI(snapshot.URI()))
		if !ok {
			continue
		}
		if _, ok := wanted[path]; ok {
			s.startAnalysis(snapshot.URI())
		}
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
	paths := usableRuntimePaths(params.Runtimepath)
	s.publishMu.Lock()
	s.workspaceMu.Lock()
	oldPaths := append([]string(nil), s.runtimePaths...)
	if slices.Equal(oldPaths, paths) {
		s.workspaceMu.Unlock()
		s.publishMu.Unlock()
		return nil
	}
	workspaceRoots := append([]string(nil), s.workspaceRoots...)
	resolver := workspacePathResolver(workspaceRoots, paths)
	openSnapshots := s.documents.Snapshots()
	s.applyRuntimepathDeltaLocked(ctx, oldPaths, paths, resolver, openSnapshots)
	s.workspaceMu.Unlock()
	s.publishMu.Unlock()
	for _, snapshot := range openSnapshots {
		s.startAnalysis(snapshot.URI())
	}
	return nil
}

// applyRuntimepathDeltaLocked updates the existing index rather than starting
// another complete workspace scan. The caller holds publishMu and workspaceMu.
func (s *Server) applyRuntimepathDeltaLocked(ctx context.Context, oldPaths, newPaths []string, resolver *workspace.PathResolver, openSnapshots []*text.Snapshot) {
	oldGraph := s.workspaceGraphView
	activeRoots := workspaceIndexRoots(s.workspaceRoots, newPaths)
	allOpenByPath := make(map[string]*text.Snapshot, len(openSnapshots))
	openByPath := make(map[string]*text.Snapshot, len(openSnapshots))
	for _, snapshot := range openSnapshots {
		path, ok := workspaceURIPath(uri.URI(snapshot.URI()))
		if !ok || snapshot.ByteLen() > maxFileBytes {
			continue
		}
		allOpenByPath[path] = snapshot
		if workspacePathInRoots(path, activeRoots) {
			openByPath[path] = snapshot
		}
	}

	s.runtimePaths = append([]string(nil), newPaths...)
	s.workspaceResolver = resolver
	s.workspaceIndex.SetRuntimePaths(newPaths)
	complete := s.workspaceIndex.Complete()
	for path := range s.workspaceFiles {
		if workspacePathInRoots(path, activeRoots) {
			continue
		}
		s.workspaceIndex.Remove(path)
		delete(s.workspaceFiles, path)
	}
	// Open snapshots with no disk backing are not in workspaceFiles. They must
	// disappear with a removed root just like ordinary indexed files.
	for path := range allOpenByPath {
		if workspacePathInRoots(path, activeRoots) {
			continue
		}
		s.workspaceIndex.Remove(path)
	}

	oldSet := make(map[string]struct{}, len(oldPaths))
	for _, path := range oldPaths {
		oldSet[path] = struct{}{}
	}
	discovered := make(map[string]struct{})
	indexedAdded := make(map[string]struct{})
	newPathsToIndex := make([]string, 0)
	newSources := make([]string, 0)
	newDiskFiles := make([]bool, 0)
	remainingBytes := maxIndexBytes - s.workspaceIndex.IndexedBytes()
	for _, root := range newPaths {
		if _, retained := oldSet[root]; retained {
			continue
		}
		if remainingBytes <= 0 {
			complete = false
			break
		}
		remaining := maxWorkspaceFiles - s.workspaceIndex.FileCount() - len(newPathsToIndex)
		if remaining <= 0 {
			complete = false
			break
		}
		files, truncated, err := workspace.DiscoverFiles(root, remaining)
		if err != nil {
			// Runtimepath roots are client-owned optional inputs. Treat a root
			// that disappears or becomes unreadable as absent without a warning.
			continue
		}
		if truncated {
			complete = false
		}
		for _, path := range files {
			if _, seen := discovered[path]; seen {
				continue
			}
			discovered[path] = struct{}{}
			if _, indexed := s.workspaceIndex.Source(path); indexed {
				continue
			}
			var source string
			diskFile := false
			if snapshot := openByPath[path]; snapshot != nil {
				source = snapshot.Text()
				if info, statErr := os.Stat(path); statErr == nil && info.Mode().IsRegular() {
					diskFile = true
				}
			} else {
				content, readErr := os.ReadFile(path)
				if readErr != nil {
					continue
				}
				source = string(content)
				diskFile = true
			}
			if len(source) > maxFileBytes {
				complete = false
				continue
			}
			if len(source) > remainingBytes {
				complete = false
				remainingBytes = 0
				break
			}
			remainingBytes -= len(source)
			newPathsToIndex = append(newPathsToIndex, path)
			newSources = append(newSources, source)
			newDiskFiles = append(newDiskFiles, diskFile)
		}
	}
	if ctx.Err() == nil && len(newSources) > 0 {
		parsed := workspace.ParseAndAnalyzeSources(ctx, newSources, 0)
		for position, item := range parsed {
			if item.File == nil || s.workspaceIndex.ReplaceWithAnalysis(newPathsToIndex[position], item.File, item.Analysis) != nil {
				complete = false
				continue
			}
			indexedAdded[newPathsToIndex[position]] = struct{}{}
			if newDiskFiles[position] {
				s.workspaceFiles[newPathsToIndex[position]] = struct{}{}
			}
		}
	} else if ctx.Err() != nil {
		complete = false
	}

	retained := make(map[string]struct{}, len(s.workspaceFiles)+len(openByPath))
	for path := range s.workspaceFiles {
		if _, ok := s.workspaceIndex.Source(path); ok {
			retained[path] = struct{}{}
		}
	}
	for path := range openByPath {
		if _, ok := s.workspaceIndex.Source(path); ok {
			retained[path] = struct{}{}
		}
	}
	graph := workspace.NewImportGraph()
	pathsToGraph := make([]string, 0, len(retained))
	for path := range retained {
		pathsToGraph = append(pathsToGraph, path)
	}
	sort.Strings(pathsToGraph)
	for _, path := range pathsToGraph {
		facts := oldGraph.Imports(path)
		if _, newlyIndexed := indexedAdded[path]; newlyIndexed {
			if source, ok := s.workspaceIndex.Source(path); ok {
				facts = collectWorkspaceImportFacts(path, syntax.Parse(source), resolver, openByPath)
			}
		}
		facts = resolveRuntimepathImportFacts(path, facts, resolver, openByPath, s.workspaceIndex)
		if err := graph.Replace(path, facts); err != nil {
			complete = false
			s.workspaceIndex.Remove(path)
			delete(s.workspaceFiles, path)
		}
	}
	graph.SetReady(true)
	graph.AdvanceRevision(oldGraph.Revision())
	s.workspaceIndex.SetComplete(complete)
	s.workspaceGraph = graph
	s.workspaceGraphView = graph.Snapshot()
	s.workspacePending = make(map[string]struct{})
	s.workspaceDependents = make(map[string]struct{})
	s.workspaceBuilt = true
	s.workspaceRevision++
	s.notifyWorkspaceIndexChangedLocked()
}

func resolveRuntimepathImportFacts(importer string, facts []workspace.ImportFact, resolver *workspace.PathResolver, openByPath map[string]*text.Snapshot, index *workspace.Index) []workspace.ImportFact {
	for position := range facts {
		fact := &facts[position]
		resolution := workspace.PathResolution{Dynamic: true}
		if resolver != nil {
			resolution = resolver.ResolveImportPath(importer, fact.ImportPath, fact.Autoload)
		}
		fact.Dynamic = resolution.Dynamic
		fact.Target = resolution.Path
		fact.Missing = !resolution.Dynamic && fact.Target == "" && len(resolution.Candidates) > 0
		if fact.Target == "" && !resolution.Dynamic {
			for _, candidate := range resolution.Candidates {
				if openByPath[candidate] != nil {
					fact.Target = candidate
					fact.Missing = false
					break
				}
			}
		}
	}
	return retainWorkspaceImportTargets(facts, func(target string) bool {
		if openByPath[target] != nil {
			return true
		}
		_, ok := index.Source(target)
		return ok
	})
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
	roots := append([]string(nil), s.workspaceRoots...)
	s.workspaceMu.Unlock()
	watchers := vimFileWatchers(roots, relative)
	if len(watchers) == 0 {
		return nil
	}
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
		return nil
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
	s.mu.Lock()
	encoding := s.encoding
	s.mu.Unlock()
	for attempt := range 2 {
		state := s.captureWorkspaceNavigationState()
		var matches []workspace.SymbolMatch
		if state.index != nil {
			matches = state.index.Search(params.Query, maxWorkspaceSymbols)
		}
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
			information := protocol.BaseSymbolInformation{Name: match.Fact.Name, Kind: protocolSymbolKind(match.Fact.Kind)}
			if match.Fact.Deprecated {
				information.Tags = []protocol.SymbolTag{protocol.SymbolTagDeprecated}
			}
			result = append(result, protocol.WorkspaceSymbol{
				BaseSymbolInformation: information,
				Location:              &protocol.Location{URI: documentURI, Range: rangeValue},
			})
		}
		if err := ctx.Err(); err != nil {
			return nil, protocol.ErrRequestCancelled
		}
		if hook := s.beforeWorkspaceIdentityCheck; hook != nil {
			hook()
		}
		s.workspaceMu.Lock()
		current := s.workspaceIdentityCurrentLocked(state.identity)
		s.workspaceMu.Unlock()
		if current {
			return result, nil
		}
		if attempt == 1 {
			return nil, protocol.ErrContentModified
		}
	}
	return nil, protocol.ErrContentModified
}
