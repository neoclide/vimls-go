package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const defaultRuntimepathDebounce = 100 * time.Millisecond

const runtimepathScanWorkers = 4

type DidChangeRuntimepathParams struct {
	Runtimepath []string `json:"runtimepath"`
}

func (s *Server) discoverRuntimePathFilesContext(ctx context.Context, root string, limit int) (workspace.RuntimePathFiles, bool, error) {
	if hook := s.testHooks.discoverWorkspaceFiles; hook != nil {
		files, truncated, err := hook(ctx, root, limit)
		var result workspace.RuntimePathFiles
		for _, path := range files {
			if workspace.IsRuntimePathColorPath(root, path) {
				result.Colors = append(result.Colors, path)
			} else {
				// Test hooks provide an already-selected deterministic file set.
				result.Sources = append(result.Sources, path)
			}
		}
		return result, truncated, err
	}
	return workspace.DiscoverRuntimePathFilesContext(ctx, root, limit)
}

type runtimeRootDiscovery struct {
	files     workspace.RuntimePathFiles
	truncated bool
	err       error
}

// Keep filesystem concurrency bounded and preserve runtimepath order when
// consuming results, so scheduling cannot change precedence or capacity limits.
func (s *Server) discoverRuntimeRoots(ctx context.Context, roots []string, limit int) []runtimeRootDiscovery {
	results := make([]runtimeRootDiscovery, len(roots))
	jobs := make(chan int, len(roots))
	for index := range roots {
		jobs <- index
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(runtimepathScanWorkers, len(roots)) {
		workers.Go(func() {
			for index := range jobs {
				if ctx.Err() != nil {
					return
				}
				result := &results[index]
				result.files, result.truncated, result.err = s.discoverRuntimePathFilesContext(ctx, roots[index], limit)
				message := fmt.Sprintf("scanned runtimepath %s: %d Vim files, %d colors", roots[index], len(result.files.Sources), len(result.files.Colors))
				if result.err != nil {
					message += ": " + result.err.Error()
				}
				s.logScanMessage(ctx, message)
			}
		})
	}
	workers.Wait()

	return results
}

// usableRuntimePaths keeps only roots that can be read. Runtimepath is an
// optional client input, so unusable entries deliberately have no warning.
func usableRuntimePaths(paths []string) []string {
	paths = normalizeRuntimePaths(paths)
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		directory, err := openNonBlockingFile(path)
		if err != nil {
			continue
		}
		info, err := directory.Stat()
		if err != nil || !info.IsDir() {
			_ = directory.Close()
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

func (s *Server) setRuntimePaths(paths []string) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	s.workspaceMu.Lock()
	s.runtimePaths = append([]string(nil), paths...)
	s.updateRuntimeHelpLocked()
	s.workspaceResolver = nil
	s.resetWorkspaceGraphLocked()
	s.workspaceRevision++
	s.workspaceMu.Unlock()
}

func runtimePathSourceInRoots(path string, roots []string) bool {
	for _, root := range roots {
		if workspace.IsRuntimePathSourcePath(root, path) {
			return true
		}
	}
	return false
}

func (s *Server) runtimepathHandler(next jsonrpc2.Handler) jsonrpc2.Handler {
	return func(ctx context.Context, request *jsonrpc2.Request) (any, error) {
		if request.Method() != MethodDidChangeRuntimepath {
			return next(ctx, request)
		}
		var params *DidChangeRuntimepathParams
		if err := protocol.Unmarshal(request.Params(), &params); err != nil {
			return nil, jsonrpc2.ErrInvalidParams
		}
		err := s.DidChangeRuntimepath(ctx, params)
		if ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		return nil, err
	}
}

func (s *Server) DidChangeRuntimepath(ctx context.Context, params *DidChangeRuntimepathParams) error {
	return s.changeRuntimepath(ctx, params, false)
}

func (s *Server) changeRuntimepath(ctx context.Context, params *DidChangeRuntimepathParams, reconcile bool) error {
	if params == nil {
		return nil
	}
	s.workspaceMu.Lock()
	if reconcile && len(s.runtimePaths) == 0 && len(s.runtimepathIndexedPaths) == 0 {
		s.workspaceMu.Unlock()
		return nil
	}
	if s.analysisStopped || s.analysisContext.Err() != nil || ctx.Err() != nil {
		s.workspaceMu.Unlock()
		return nil
	}
	// Reserve input order before releasing the JSON-RPC read loop. Pending
	// updates coalesce; an active batch is never canceled by a newer update.
	if !reconcile {
		s.runtimepathGeneration++
	}
	generation := s.runtimepathGeneration
	s.runtimepathWG.Add(1)
	s.workspaceMu.Unlock()
	defer s.runtimepathWG.Done()
	jsonrpc2.Async(ctx)
	ctx, cancel := s.workspaceOperationContext(ctx)
	defer cancel()
	if !reconcile {
		timer := time.NewTimer(defaultRuntimepathDebounce)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
	}
	s.runtimepathRunMu.Lock()
	defer s.runtimepathRunMu.Unlock()
	s.workspaceMu.Lock()
	current := reconcile || generation == s.runtimepathGeneration
	paths := append([]string(nil), s.runtimePaths...)
	s.workspaceMu.Unlock()
	if !current || ctx.Err() != nil {
		return nil
	}
	if !reconcile {
		paths = usableRuntimePaths(params.Runtimepath)
	} else {
		paths = usableRuntimePaths(paths)
	}
	// Measure the entire update, across all directory batches and analysis,
	// excluding debounce/queue time. Runtime help has its own asynchronous timer.
	started := time.Now()
	for ctx.Err() == nil {
		s.publishMu.Lock()
		s.workspaceMu.Lock()
		if s.analysisContext.Err() != nil || ctx.Err() != nil {
			s.workspaceMu.Unlock()
			s.publishMu.Unlock()
			return nil
		}
		oldPaths := append([]string(nil), s.runtimepathIndexedPaths...)
		if slices.Equal(oldPaths, paths) && slices.Equal(s.runtimePaths, paths) && (!reconcile || len(paths) == 0) {
			s.workspaceMu.Unlock()
			s.publishMu.Unlock()
			return nil
		}
		openSnapshots := s.documents.Snapshots()
		applied := s.applyRuntimepathDeltaLocked(ctx, oldPaths, paths, openSnapshots)
		indexComplete := s.workspaceIndex != nil && s.workspaceIndex.Complete()
		s.workspaceMu.Unlock()
		s.publishMu.Unlock()
		if applied {
			s.logScanDuration(ctx, "scanned runtimepath", started)
			s.scheduleWorkspaceRefresh(indexComplete)
			for _, snapshot := range openSnapshots {
				s.startAnalysis(snapshot.URI())
			}
			return nil
		}
		// Retry only this delta after overlay churn; never rescan the workspace.
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Millisecond):
		}
	}
	return nil
}

// workspaceOperationContext is canceled when either the caller abandons its
// request or the server lifecycle stops.
func (s *Server) workspaceOperationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	combined, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.analysisContext, cancel)
	if s.analysisContext.Err() != nil {
		cancel()
	}
	return combined, func() {
		stop()
		cancel()
	}
}

func remainingWorkspaceIndexCapacity(index *workspace.Index, workspaceFiles map[string]struct{}, openByPath map[string]*text.Snapshot, retain func(string) bool, maxFiles, maxBytes int) (files, bytes int) {
	removed := make(map[string]struct{}, len(workspaceFiles)+len(openByPath))
	for path := range workspaceFiles {
		if !retain(path) {
			removed[path] = struct{}{}
		}
	}
	for path := range openByPath {
		if !retain(path) {
			removed[path] = struct{}{}
		}
	}
	removedFiles := 0
	removedBytes := 0
	for path := range removed {
		if source, ok := index.Source(path); ok {
			removedFiles++
			removedBytes += len(source)
		}
	}
	return max(0, maxFiles-index.FileCount()+removedFiles), max(0, maxBytes-index.IndexedBytes()+removedBytes)
}

// applyRuntimepathDeltaLocked updates the existing index rather than starting
// another complete workspace scan. The caller holds publishMu and workspaceMu.
func (s *Server) applyRuntimepathDeltaLocked(ctx context.Context, oldPaths, newPaths []string, openSnapshots []*text.Snapshot) bool {
	ctx, cancel := s.workspaceOperationContext(ctx)
	defer cancel()
	if ctx.Err() != nil || s.analysisStopped || s.analysisContext.Err() != nil {
		return false
	}
	oldGraph := s.workspaceGraphView
	index := s.workspaceIndex
	identity := s.workspaceIdentityLocked()
	workspaceRoots := normalizeWorkspaceRoots(s.workspaceRoots)
	retain := func(path string) bool {
		return workspacePathInRoots(path, workspaceRoots) || runtimePathSourceInRoots(path, newPaths)
	}
	allOpenByPath := make(map[string]*text.Snapshot, len(openSnapshots))
	openByPath := make(map[string]*text.Snapshot, len(openSnapshots))
	for _, snapshot := range openSnapshots {
		path, ok := workspaceURIPath(uri.URI(snapshot.URI()))
		if !ok || snapshot.ByteLen() > maxFileBytes {
			continue
		}
		allOpenByPath[path] = snapshot
		if retain(path) {
			openByPath[path] = snapshot
		}
	}

	// Calculate how many bytes and files will be freed by removing files
	// belonging to deleted runtime paths. This ensures the capacity budget
	// accurately credits freed space when swapping runtime paths.
	remainingFiles, remainingBytes := remainingWorkspaceIndexCapacity(index, s.workspaceFiles, allOpenByPath, retain, maxWorkspaceFiles, maxIndexBytes)

	complete := index.Complete()
	indexedSources := make(map[string]string, len(s.workspaceFiles)+len(allOpenByPath))
	for path := range s.workspaceFiles {
		if source, ok := index.Source(path); ok {
			indexedSources[path] = source
		}
	}
	for path := range allOpenByPath {
		if source, ok := index.Source(path); ok {
			indexedSources[path] = source
		}
	}
	// Discovery, source reads, parsing and import resolution use captured inputs.
	// Revalidate both workspace identity and open overlays before installation.
	s.workspaceMu.Unlock()
	s.publishMu.Unlock()
	locked := false
	defer func() {
		if !locked {
			s.publishMu.Lock()
			s.workspaceMu.Lock()
		}
	}()

	resolver := workspacePathResolver(workspaceRoots, newPaths)
	oldSet := make(map[string]struct{}, len(oldPaths))
	for _, path := range oldPaths {
		oldSet[path] = struct{}{}
	}
	discovered := make(map[string]struct{})
	var runtimeColors []string
	newPathsToIndex := make([]string, 0)
	newSources := make([]string, 0)
	newDiskFiles := make([]bool, 0)
	var addedRoots []string
	for _, root := range newPaths {
		if _, retained := oldSet[root]; !retained && !workspacePathInRoots(root, workspaceRoots) {
			addedRoots = append(addedRoots, root)
		}
	}
	for rootStart := 0; rootStart < len(addedRoots); rootStart += runtimepathScanWorkers {
		if remainingBytes <= 0 || remainingFiles <= len(newPathsToIndex) {
			complete = false
			break
		}
		rootEnd := min(rootStart+runtimepathScanWorkers, len(addedRoots))
		rootResults := s.discoverRuntimeRoots(ctx, addedRoots[rootStart:rootEnd], remainingFiles-len(newPathsToIndex))
		for _, result := range rootResults {
			if remainingBytes <= 0 {
				complete = false
				break
			}
			remaining := remainingFiles - len(newPathsToIndex)
			if remaining <= 0 {
				complete = false
				break
			}
			runtimeFiles, truncated, err := result.files, result.truncated, result.err
			files := runtimeFiles.Sources
			runtimeColors = append(runtimeColors, runtimeFiles.Colors...)
			if ctx.Err() != nil {
				return false
			}
			if err != nil {
				// Runtimepath roots are client-owned optional inputs. Treat a root
				// that disappears or becomes unreadable as absent without a warning.
				continue
			}
			if truncated {
				complete = false
			}
			for _, path := range files {
				if workspacePathInRoots(path, workspaceRoots) {
					continue
				}
				if len(newPathsToIndex) >= remainingFiles {
					complete = false
					break
				}
				if _, seen := discovered[path]; seen {
					continue
				}
				discovered[path] = struct{}{}
				if _, indexed := indexedSources[path]; indexed {
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
					content, ok := readRegularWorkspaceFile(path, maxFileBytes)
					if !ok {
						complete = false
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
	}
	if ctx.Err() != nil {
		return false
	}
	var parsed []workspace.AnalyzedSource
	if len(newSources) > 0 {
		parsed = s.parseAndAnalyzeSources(ctx, newSources)
		if ctx.Err() != nil {
			return false
		}
	}
	resolvedFacts := make(map[string][]workspace.ImportFact, len(indexedSources)+len(parsed))
	for path := range indexedSources {
		if retain(path) {
			resolvedFacts[path] = oldGraph.Imports(path)
		}
	}
	for position, item := range parsed {
		if item.File != nil {
			path := newPathsToIndex[position]
			resolvedFacts[path] = collectWorkspaceImportFacts(path, item.File, resolver, openByPath)
		}
	}
	for path, facts := range resolvedFacts {
		if ctx.Err() != nil {
			return false
		}
		resolvedFacts[path] = resolveRuntimepathImportFacts(path, facts, resolver, openByPath, resolvedFacts)
	}
	// Discovery and analysis must both complete before any workspace state is
	// changed, so a canceled runtimepath request leaves the current index live.
	s.publishMu.Lock()
	s.workspaceMu.Lock()
	locked = true
	if ctx.Err() != nil || s.analysisStopped || s.analysisContext.Err() != nil || !s.workspaceIdentityCurrentLocked(identity) || !workspaceSnapshotsCurrent(s.documents.Snapshots(), openSnapshots) {
		return false
	}
	s.runtimePaths = append([]string(nil), newPaths...)
	s.runtimepathIndexedPaths = append([]string(nil), newPaths...)
	s.runtimepathWorkspaceRoots = append([]string(nil), workspaceRoots...)
	s.updateRuntimeHelpLocked()
	s.workspaceResolver = resolver
	searchPaths := newPaths
	if len(searchPaths) == 0 {
		searchPaths = workspaceRoots
	}
	s.workspaceIndex.SetRuntimePaths(searchPaths)
	for _, path := range runtimeColors {
		if workspacePathInRoots(path, workspaceRoots) {
			continue
		}
		if s.workspaceIndex.AddRuntimePathFile(path) != nil {
			complete = false
		}
	}
	for path := range s.workspaceFiles {
		if retain(path) {
			continue
		}
		s.workspaceIndex.Remove(path)
		delete(s.workspaceFiles, path)
	}
	// Open snapshots with no disk backing are not in workspaceFiles. They must
	// disappear with a removed root just like ordinary indexed files.
	for path := range allOpenByPath {
		if retain(path) {
			continue
		}
		s.workspaceIndex.Remove(path)
	}
	for position, item := range parsed {
		if item.File == nil || s.workspaceIndex.ReplaceWithAnalysis(newPathsToIndex[position], item.File, item.Analysis) != nil {
			complete = false
			continue
		}
		if newDiskFiles[position] {
			s.workspaceFiles[newPathsToIndex[position]] = struct{}{}
		}
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
		facts := retainWorkspaceImportTargets(resolvedFacts[path], func(target string) bool {
			if openByPath[target] != nil {
				return true
			}
			_, ok := s.workspaceIndex.Source(target)
			return ok
		})
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
	return true
}

func resolveRuntimepathImportFacts(importer string, facts []workspace.ImportFact, resolver *workspace.PathResolver, openByPath map[string]*text.Snapshot, indexed map[string][]workspace.ImportFact) []workspace.ImportFact {
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
		_, ok := indexed[target]
		return ok
	})
}
