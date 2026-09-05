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
const workspaceIndexWaitTimeout = 5 * time.Second
const workspaceProgressCreateTimeout = 100 * time.Millisecond
const workspaceProgressNotificationTimeout = 100 * time.Millisecond
const workspaceProgressEndTimeout = workspaceProgressNotificationTimeout
const workspaceProgressReportLimit = 64

type DidChangeRuntimepathParams struct {
	Runtimepath []string `json:"runtimepath"`
}

func (s *Server) discoverWorkspaceFilesContext(ctx context.Context, root string, limit int) ([]string, bool, error) {
	if hook := s.testHooks.discoverWorkspaceFiles; hook != nil {
		return hook(ctx, root, limit)
	}
	return workspace.DiscoverFilesContext(ctx, root, limit)
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
	defer func() {
		if hook := s.testHooks.afterWorkspaceIndexWorker; hook != nil {
			hook()
		}
		s.workspaceWG.Done()
	}()
	timer := time.NewTimer(s.workspaceRebuildDelay())
	defer timer.Stop()
	if hook := s.testHooks.beforeWorkspaceRebuildDelay; hook != nil {
		hook()
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
		if hook := s.testHooks.beforeWorkspaceBuild; hook != nil {
			hook(openSnapshots)
		}

		progress := s.startWorkspaceIndexProgress()
		progressReporting := progress != nil
		index, graph, diskFiles, warnings := s.buildWorkspaceIndex(s.analysisContext, roots, runtimePaths, resolver, openSnapshots, func(root string) {
			progressReporting = s.reportWorkspaceIndexProgress(progress, progressReporting, root)
		})
		s.workspaceMu.Lock()
		if s.analysisStopped || s.analysisContext.Err() != nil {
			s.finishWorkspaceRebuildLocked()
			s.workspaceMu.Unlock()
			s.finishWorkspaceIndexProgress(progress)
			return
		}
		s.workspaceMu.Unlock()

		s.publishMu.Lock()
		if !workspaceSnapshotsCurrent(s.documents.Snapshots(), openSnapshots) {
			s.publishMu.Unlock()
			s.finishWorkspaceIndexProgress(progress)
			s.resetWorkspaceRebuildTimer(timer)
			continue
		}
		s.workspaceMu.Lock()
		if s.analysisStopped || s.analysisContext.Err() != nil {
			s.finishWorkspaceRebuildLocked()
			s.workspaceMu.Unlock()
			s.publishMu.Unlock()
			s.finishWorkspaceIndexProgress(progress)
			return
		}
		if revision != s.workspaceRevision {
			s.workspaceMu.Unlock()
			s.publishMu.Unlock()
			s.finishWorkspaceIndexProgress(progress)
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
		s.finishWorkspaceIndexProgress(progress)
		for _, warning := range warnings {
			_ = s.sendWarning(s.analysisContext, warning)
		}
		for _, snapshot := range openSnapshots {
			s.startAnalysis(snapshot.URI())
		}
		return
	}
}

type workspaceProgressSession struct {
	client protocol.Client
	token  protocol.ProgressToken
	queue  chan workspaceProgressNotification

	// created is owned by the serial worker. A delayed create can therefore
	// only be followed by begin and end in that worker's order.
	created bool
}

type workspaceProgressNotification struct {
	run      func() error
	done     chan error
	terminal bool
}

// startWorkspaceIndexProgress creates a token before a workspace scan. A
// session owns one serial notification worker, so a blocked notification can
// never be overtaken by a terminal end for the same token.
func (s *Server) startWorkspaceIndexProgress() *workspaceProgressSession {
	s.mu.Lock()
	client := s.client
	supported := s.workspaceProgress
	s.workspaceProgressID++
	identifier := s.workspaceProgressID
	s.mu.Unlock()
	if client == nil || !supported {
		return nil
	}
	token := protocol.String(fmt.Sprintf("vimls-workspace-index-%d", identifier))
	session := &workspaceProgressSession{
		client: client,
		token:  token,
		queue:  make(chan workspaceProgressNotification, workspaceProgressReportLimit+1),
	}
	go session.run()
	createTimeout := s.workspaceProgressCallTimeout(workspaceProgressCreateTimeout)
	ctx, cancel := context.WithTimeout(s.analysisContext, createTimeout)
	defer cancel()
	if err := s.sendWorkspaceProgressCall(session, ctx, createTimeout, false, func(ctx context.Context) error {
		err := client.WorkDoneProgressCreate(ctx, &protocol.WorkDoneProgressCreateParams{Token: token})
		session.created = err == nil
		return err
	}); err != nil {
		if ctx.Err() == nil {
			s.logf("vimls: create workspace index progress: %v", err)
			close(session.queue)
			return nil
		}
		s.disableWorkspaceProgress()
	}
	value, err := protocol.Marshal(&protocol.WorkDoneProgressBegin{Kind: "begin", Title: "Indexing workspace"})
	if err != nil {
		s.logf("vimls: encode workspace index progress: %v", err)
		close(session.queue)
		return nil
	}
	if err := s.sendWorkspaceProgress(session, s.analysisContext, s.workspaceProgressCallTimeout(workspaceProgressNotificationTimeout), &protocol.ProgressParams{Token: token, Value: protocol.LSPAny(value)}, false); err != nil {
		s.disableWorkspaceProgress()
		if s.analysisContext.Err() == nil {
			s.logf("vimls: send workspace index progress: %v", err)
		}
	}
	// A create that succeeded may have reached the client even if begin blocks.
	// Keep the session so finishWorkspaceIndexProgress can queue its end behind
	// that begin.
	return session
}

func (session *workspaceProgressSession) run() {
	for notification := range session.queue {
		err := notification.run()
		notification.done <- err
		if notification.terminal {
			return
		}
	}
}

// sendWorkspaceProgress bounds waiting for a progress notification without
// abandoning it: timed-out notifications remain queued in token order, ahead
// of the terminal end. Each actual client call gets its deadline when the
// serial worker reaches it.
func (s *Server) sendWorkspaceProgress(session *workspaceProgressSession, parent context.Context, timeout time.Duration, params *protocol.ProgressParams, terminal bool) error {
	return s.sendWorkspaceProgressCall(session, parent, timeout, terminal, func(ctx context.Context) error {
		if !session.created {
			return nil
		}
		return session.client.Progress(ctx, params)
	})
}

func (s *Server) sendWorkspaceProgressCall(session *workspaceProgressSession, parent context.Context, timeout time.Duration, terminal bool, call func(context.Context) error) error {
	if session == nil {
		return nil
	}
	wait, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	notification := workspaceProgressNotification{
		done:     make(chan error, 1),
		terminal: terminal,
		run: func() error {
			callCtx, callCancel := context.WithTimeout(parent, timeout)
			defer callCancel()
			return call(callCtx)
		},
	}
	select {
	case session.queue <- notification:
	case <-wait.Done():
		return wait.Err()
	}
	select {
	case err := <-notification.done:
		return err
	case <-wait.Done():
		return wait.Err()
	}
}

func (s *Server) disableWorkspaceProgress() {
	s.mu.Lock()
	s.workspaceProgress = false
	s.mu.Unlock()
}

func (s *Server) workspaceProgressEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workspaceProgress
}

func (s *Server) workspaceProgressCallTimeout(fallback time.Duration) time.Duration {
	if s.testHooks.workspaceProgressTimeout > 0 {
		return s.testHooks.workspaceProgressTimeout
	}
	return fallback
}

// reportWorkspaceIndexProgress identifies a runtime root at the discovery
// boundary. It is called outside workspace mutexes and does not affect index
// completion when a client cannot receive a notification.
func (s *Server) reportWorkspaceIndexProgress(session *workspaceProgressSession, started bool, root string) bool {
	if !started || !s.workspaceProgressEnabled() {
		return false
	}
	message := fmt.Sprintf("Discovering runtime path %s", root)
	value, err := protocol.Marshal(&protocol.WorkDoneProgressReport{Kind: "report", Message: &message})
	if err != nil {
		s.logf("vimls: encode workspace index progress report: %v", err)
		return false
	}
	if err := s.sendWorkspaceProgress(session, s.analysisContext, s.workspaceProgressCallTimeout(workspaceProgressNotificationTimeout), &protocol.ProgressParams{Token: session.token, Value: protocol.LSPAny(value)}, false); err != nil {
		s.disableWorkspaceProgress()
		if s.analysisContext.Err() == nil {
			s.logf("vimls: send workspace index progress report: %v", err)
		}
		return false
	}
	return true
}

func (s *Server) finishWorkspaceIndexProgress(session *workspaceProgressSession) {
	if session == nil {
		return
	}
	value, err := protocol.Marshal(&protocol.WorkDoneProgressEnd{Kind: "end"})
	if err != nil {
		return
	}
	_ = s.sendWorkspaceProgress(session, context.Background(), s.workspaceProgressCallTimeout(workspaceProgressEndTimeout), &protocol.ProgressParams{Token: session.token, Value: protocol.LSPAny(value)}, true)
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
	// LSP has no capability flag for request cancellation. Requests handled by
	// cancellationHandler have a cancellable context, so keep them pending until
	// the index is ready or the client sends $/cancelRequest. Retain a bounded
	// fallback for direct callers that pass a non-cancellable context.
	if ctx.Done() != nil {
		return s.waitForWorkspaceIndexUntil(ctx, nil)
	}
	timeout := workspaceIndexWaitTimeout
	if s.testHooks.workspaceIndexWaitTimeout > 0 {
		timeout = s.testHooks.workspaceIndexWaitTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	return s.waitForWorkspaceIndexUntil(ctx, timer.C)
}

func (s *Server) waitForWorkspaceIndexUntil(ctx context.Context, timeout <-chan time.Time) error {
	for {
		s.workspaceMu.Lock()
		busy := s.workspaceIndexBusyLocked()
		changed := s.workspaceChanged
		s.workspaceMu.Unlock()
		if !busy {
			return nil
		}
		if hook := s.testHooks.beforeWorkspaceIndexWait; hook != nil {
			hook()
		}
		select {
		case <-ctx.Done():
			return protocol.ErrRequestCancelled
		case <-s.analysisContext.Done():
			return protocol.ErrRequestCancelled
		case <-timeout:
			return jsonrpc2.NewError(jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed), "workspace index did not become ready within 5s")
		case <-changed:
		}
	}
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

// buildWorkspaceIndex reports up to workspaceProgressReportLimit configured
// runtime roots immediately before discovering their files. Parsing remains
// batched after discovery, so reports identify discovery roots rather than
// individual files.
func (s *Server) buildWorkspaceIndex(ctx context.Context, roots, runtimePaths []string, resolver *workspace.PathResolver, openSnapshots []*text.Snapshot, progress ...func(string)) (*workspace.Index, *workspace.ImportGraph, map[string]struct{}, []string) {
	var reportRuntimeRoot func(string)
	if len(progress) > 0 {
		reportRuntimeRoot = progress[0]
	}
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
	reportedRuntimeRoots := 0
	for _, root := range roots {
		if ctx.Err() != nil {
			return newWorkspaceIndex(), workspace.NewImportGraph(), map[string]struct{}{}, nil
		}
		runtimeRoot := slices.Contains(runtimePaths, root)
		remaining := maxWorkspaceFiles - len(paths)
		if remaining <= 0 {
			complete = false
			warnings = appendWarning(warnings, "vimls: workspace file limit reached; additional files were omitted")
			break
		}
		if runtimeRoot && reportRuntimeRoot != nil && reportedRuntimeRoots < workspaceProgressReportLimit {
			reportRuntimeRoot(root)
			reportedRuntimeRoots++
		}
		files, truncated, err := s.discoverWorkspaceFilesContext(ctx, root, remaining)
		if ctx.Err() != nil {
			return newWorkspaceIndex(), workspace.NewImportGraph(), map[string]struct{}{}, nil
		}
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
		content, ok := readRegularWorkspaceFile(path, maxFileBytes)
		if !ok {
			if !workspacePathInRoots(path, runtimePaths) {
				s.logf("vimls: read workspace file %s: failed or non-regular", path)
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
		snapshot := s.workspaceAnalysisSnapshotLocked("", nil, nil)
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
		snapshot := s.workspaceAnalysisSnapshotLocked(path, nil, nil)
		dependents := s.readyWorkspaceDependentsLocked()
		s.notifyWorkspaceIndexChangedLocked()
		s.workspaceMu.Unlock()
		return snapshot, dependents
	}
	sameSource, indexed := s.workspaceIndex.Source(path)
	_, pending := s.workspacePending[path]
	if !pending && indexed && sameSource == file.Source && s.workspaceGraphView.Has(path) {
		snapshot := s.workspaceAnalysisSnapshotLocked(path, file, result)
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
		s.queueWorkspaceDependentsLocked(path)
		facts := retainWorkspaceImportTargets(collectWorkspaceImportFacts(path, file, s.workspaceResolver, openByPath), func(target string) bool {
			if openByPath[target] != nil {
				return true
			}
			_, ok := s.workspaceIndex.Source(target)
			return ok
		})
		var graphErr error
		if hook := s.testHooks.replaceWorkspaceGraph; hook != nil {
			graphErr = hook(s.workspaceGraph, path, facts)
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
	snapshot := s.workspaceAnalysisSnapshotLocked(path, file, result)
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

func (s *Server) workspaceAnalysisSnapshotLocked(path string, file *syntax.File, result *analysis.FileAnalysis) workspaceAnalysisSnapshot {
	snapshot := workspaceAnalysisSnapshot{
		identity: s.workspaceIdentityLocked(), path: path, graph: s.workspaceGraphView,
		roots: workspaceIndexRoots(s.workspaceRoots, s.runtimePaths), ready: true,
	}
	if path == "" || !workspacePathInRoots(path, snapshot.roots) {
		return snapshot
	}
	if s.workspaceRunning {
		snapshot.ready = false
		return snapshot
	}
	if !snapshot.graph.Ready() {
		s.workspaceDependents[path] = struct{}{}
		snapshot.ready = false
		return snapshot
	}
	if s.workspaceIndexReadyLocked() {
		snapshot.globalDiagnostics = s.workspaceIndex.GlobalNameConflictDiagnostics(path, file)
		snapshot.augroupNames = s.workspaceIndex.AugroupNames()
		var references []workspace.ExternalReferenceFact
		if file != nil {
			if result == nil || result.File != file {
				result = analysis.Analyze(file)
			}
			references = workspace.CollectExternalReferencesFromAnalysis(path, file, result)
			for _, reference := range references {
				if reference.Kind == workspace.ExternalReferenceGlobalFunction && reference.DirectCall && startsWithUppercaseASCII(reference.Name) && !s.workspaceIndex.HasGlobalFunction(reference.Name) {
					if snapshot.missingGlobalFunctions == nil {
						snapshot.missingGlobalFunctions = make(map[string]bool)
					}
					snapshot.missingGlobalFunctions[reference.Name] = true
				}
			}
		}
		if s.workspaceIndex.Complete() {
			snapshot.indexComplete = true
			snapshot.userCommandNames = s.workspaceIndex.UserCommandNames()
			for _, reference := range references {
				if reference.Kind == workspace.ExternalReferenceAutoload && reference.DirectCall && !s.workspaceIndex.HasAutoloadFunction(reference.Name) {
					if snapshot.missingAutoloadFunctions == nil {
						snapshot.missingAutoloadFunctions = make(map[string]bool)
					}
					snapshot.missingAutoloadFunctions[reference.Name] = true
				}
			}
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
	s.queueWorkspaceDependentsLocked(path)
	s.workspaceIndex.Remove(path)
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
	if hook := s.testHooks.beforeWorkspaceRestoreRead; hook != nil {
		hook(restore)
	}
	var file *syntax.File
	if !restore.knownDiskFile {
		file = nil
	} else if content, ok := readRegularWorkspaceFile(restore.path, maxFileBytes); ok {
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
	for _, dependent := range s.workspaceIndex.AutoloadDependents(path) {
		s.workspaceDependents[dependent] = struct{}{}
	}
	for _, dependent := range s.workspaceIndex.GlobalFunctionDependents(path) {
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
	if params == nil {
		return nil
	}
	s.workspaceMu.Lock()
	if s.analysisStopped || s.analysisContext.Err() != nil || ctx.Err() != nil {
		s.workspaceMu.Unlock()
		return nil
	}
	// Reserve input order before releasing the connection read loop. Even a
	// no-op newer update supersedes an older, still-running discovery.
	if s.runtimepathCancel != nil {
		s.runtimepathCancel()
	}
	ctx, cancel := context.WithCancel(ctx)
	s.runtimepathCancel = cancel
	s.runtimepathGeneration++
	generation := s.runtimepathGeneration
	s.runtimepathWG.Add(1)
	s.workspaceMu.Unlock()
	defer s.runtimepathWG.Done()
	defer cancel()
	jsonrpc2.Async(ctx)
	paths := usableRuntimePaths(params.Runtimepath)
	for range 2 {
		s.publishMu.Lock()
		s.workspaceMu.Lock()
		if generation != s.runtimepathGeneration || s.analysisContext.Err() != nil || ctx.Err() != nil {
			s.workspaceMu.Unlock()
			s.publishMu.Unlock()
			return nil
		}
		oldPaths := append([]string(nil), s.runtimePaths...)
		if slices.Equal(oldPaths, paths) {
			s.workspaceMu.Unlock()
			s.publishMu.Unlock()
			return nil
		}
		openSnapshots := s.documents.Snapshots()
		applied := s.applyRuntimepathDeltaLocked(ctx, oldPaths, paths, openSnapshots)
		s.workspaceMu.Unlock()
		s.publishMu.Unlock()
		if applied {
			for _, snapshot := range openSnapshots {
				s.startAnalysis(snapshot.URI())
			}
			return nil
		}
	}
	// Repeated document/index churn must not lose a notification's intent.
	// Fall back to the existing rebuild worker, which validates fresh overlays.
	s.publishMu.Lock()
	s.workspaceMu.Lock()
	current := generation == s.runtimepathGeneration && s.analysisContext.Err() == nil && ctx.Err() == nil
	if current {
		s.runtimePaths = append([]string(nil), paths...)
		s.workspaceResolver = nil
		s.resetWorkspaceGraphLocked()
		s.workspaceRevision++
	}
	s.workspaceMu.Unlock()
	s.publishMu.Unlock()
	if current {
		s.scheduleWorkspaceRebuild()
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

func remainingWorkspaceIndexCapacity(index *workspace.Index, workspaceFiles map[string]struct{}, openByPath map[string]*text.Snapshot, activeRoots []string, maxFiles, maxBytes int) (files, bytes int) {
	removed := make(map[string]struct{}, len(workspaceFiles)+len(openByPath))
	for path := range workspaceFiles {
		if !workspacePathInRoots(path, activeRoots) {
			removed[path] = struct{}{}
		}
	}
	for path := range openByPath {
		if !workspacePathInRoots(path, activeRoots) {
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
	generation := s.runtimepathGeneration
	workspaceRoots := append([]string(nil), s.workspaceRoots...)
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

	// Calculate how many bytes and files will be freed by removing files
	// belonging to deleted runtime paths. This ensures the capacity budget
	// accurately credits freed space when swapping runtime paths.
	remainingFiles, remainingBytes := remainingWorkspaceIndexCapacity(index, s.workspaceFiles, allOpenByPath, activeRoots, maxWorkspaceFiles, maxIndexBytes)

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
	newPathsToIndex := make([]string, 0)
	newSources := make([]string, 0)
	newDiskFiles := make([]bool, 0)
	for _, root := range newPaths {
		if _, retained := oldSet[root]; retained {
			continue
		}
		if remainingBytes <= 0 {
			complete = false
			break
		}
		remaining := remainingFiles - len(newPathsToIndex)
		if remaining <= 0 {
			complete = false
			break
		}
		files, truncated, err := s.discoverWorkspaceFilesContext(ctx, root, remaining)
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
	if ctx.Err() != nil {
		return false
	}
	var parsed []workspace.AnalyzedSource
	if len(newSources) > 0 {
		parsed = workspace.ParseAndAnalyzeSources(ctx, newSources, 0)
		if ctx.Err() != nil {
			return false
		}
	}
	resolvedFacts := make(map[string][]workspace.ImportFact, len(indexedSources)+len(parsed))
	for path := range indexedSources {
		if workspacePathInRoots(path, activeRoots) {
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
	if ctx.Err() != nil || s.analysisStopped || s.analysisContext.Err() != nil || generation != s.runtimepathGeneration || !s.workspaceIdentityCurrentLocked(identity) || !workspaceSnapshotsCurrent(s.documents.Snapshots(), openSnapshots) {
		return false
	}
	s.runtimePaths = append([]string(nil), newPaths...)
	s.workspaceResolver = resolver
	s.workspaceIndex.SetRuntimePaths(newPaths)
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

// DidChangeWatchedFiles consumes file events produced by the language client.
// The server deliberately does not create filesystem watchers or poll roots.
func (s *Server) DidChangeWatchedFiles(ctx context.Context, params *protocol.DidChangeWatchedFilesParams) error {
	if params == nil || len(params.Changes) == 0 {
		return nil
	}
	s.watchMu.Lock()
	if s.analysisContext.Err() != nil || ctx.Err() != nil {
		s.watchMu.Unlock()
		return nil
	}
	if s.watchedFilesRunning {
		s.watchedFilesDirty = true
		s.watchMu.Unlock()
		return nil
	}
	s.watchedFilesRunning = true
	s.watchWG.Add(1)
	s.watchMu.Unlock()

	defer func() {
		s.watchMu.Lock()
		dirty := s.watchedFilesDirty
		s.watchedFilesDirty = false
		s.watchedFilesRunning = false
		if dirty && s.analysisContext.Err() == nil {
			s.scheduleWorkspaceRebuild()
		}
		s.watchWG.Done()
		s.watchMu.Unlock()
	}()

	if !s.applyWatchedFileChanges(ctx, params.Changes) {
		if s.analysisContext.Err() == nil && ctx.Err() == nil {
			s.scheduleWorkspaceRebuild()
		}
	}
	return nil
}

func (s *Server) applyWatchedFileChanges(ctx context.Context, changes []protocol.FileEvent) bool {
	if ctx.Err() != nil || s.analysisContext.Err() != nil {
		return false
	}
	s.workspaceMu.Lock()
	built := s.workspaceBuilt
	index := s.workspaceIndex
	complete := index != nil && index.Complete()
	pendingCount := len(s.workspacePending)
	rebuilding := s.workspaceRunning
	roots := workspaceIndexRoots(s.workspaceRoots, s.runtimePaths)
	hasMissing := s.workspaceGraph != nil && s.workspaceGraph.HasMissingImports()
	s.workspaceMu.Unlock()

	if !built || index == nil || !complete || pendingCount > 0 || rebuilding {
		return false
	}

	actions := make(map[string]protocol.FileChangeType)
	var paths []string

	for _, event := range changes {
		path, ok := workspaceURIPath(event.URI)
		if !ok {
			return false
		}
		if !workspacePathInRoots(path, roots) {
			continue
		}
		ext := strings.ToLower(filepath.Ext(path))
		if event.Type != protocol.FileChangeTypeDeleted {
			info, err := os.Stat(path)
			if err == nil && info.IsDir() {
				return false
			}
			if err == nil && !info.Mode().IsRegular() {
				return false
			}
			if ext != ".vim" {
				continue
			}
		} else {
			if ext != ".vim" {
				return false
			}
		}

		if _, exists := actions[path]; !exists {
			paths = append(paths, path)
		}
		actions[path] = event.Type
	}

	if len(paths) == 0 {
		return true
	}

	if hasMissing {
		for _, action := range actions {
			if action == protocol.FileChangeTypeCreated {
				return false
			}
		}
	}

	sort.Strings(paths)

	var allDependents []string
	for _, path := range paths {
		if ctx.Err() != nil || s.analysisContext.Err() != nil {
			return false
		}
		if hook := s.testHooks.beforeWatchedFileProcess; hook != nil {
			hook(path)
		}

		s.publishMu.Lock()
		_, _, open := s.openWorkspaceSnapshotLocked(path)
		s.publishMu.Unlock()
		if open {
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				if hook := s.testHooks.beforeWatchedFileInstall; hook != nil {
					hook(path)
				}
				s.publishMu.Lock()
				_, _, open = s.openWorkspaceSnapshotLocked(path)
				if open {
					s.publishMu.Unlock()
					continue
				}
				_, deps := s.replaceWorkspaceFileWithAnalysisSnapshot(uri.File(path).String(), nil, nil)
				s.workspaceMu.Lock()
				delete(s.workspaceFiles, path)
				s.workspaceMu.Unlock()
				s.publishMu.Unlock()
				allDependents = append(allDependents, deps...)
				continue
			}
			return false
		}

		if info.IsDir() || !info.Mode().IsRegular() {
			return false
		}

		if info.Size() > maxFileBytes {
			s.publishMu.Lock()
			_, _, open = s.openWorkspaceSnapshotLocked(path)
			if open {
				s.publishMu.Unlock()
				continue
			}
			_, deps := s.replaceWorkspaceFileWithAnalysisSnapshot(uri.File(path).String(), nil, nil)
			s.workspaceMu.Lock()
			s.workspaceFiles[path] = struct{}{}
			s.workspaceMu.Unlock()
			s.publishMu.Unlock()
			allDependents = append(allDependents, deps...)
			continue
		}

		if hook := s.testHooks.beforeWatchedFileRead; hook != nil {
			hook(path)
		}

		contentBytes, ok := readRegularWorkspaceFile(path, maxFileBytes)
		if !ok {
			return false
		}
		content := string(contentBytes)

		s.workspaceMu.Lock()
		existingSource, indexed := s.workspaceIndex.Source(path)
		s.workspaceMu.Unlock()
		if indexed && existingSource == content {
			continue
		}

		if ctx.Err() != nil || s.analysisContext.Err() != nil {
			return false
		}

		file := syntax.Parse(content)
		fileAnalysis := analysis.Analyze(file)

		if ctx.Err() != nil || s.analysisContext.Err() != nil {
			return false
		}

		if hook := s.testHooks.beforeWatchedFileInstall; hook != nil {
			hook(path)
		}

		s.publishMu.Lock()
		_, _, open = s.openWorkspaceSnapshotLocked(path)
		if open {
			s.publishMu.Unlock()
			continue
		}
		_, deps := s.replaceWorkspaceFileWithAnalysisSnapshot(uri.File(path).String(), file, fileAnalysis)
		s.workspaceMu.Lock()
		s.workspaceFiles[path] = struct{}{}
		s.workspaceMu.Unlock()
		s.publishMu.Unlock()
		allDependents = append(allDependents, deps...)
	}

	s.workspaceMu.Lock()
	stillComplete := s.workspaceIndex != nil && s.workspaceIndex.Complete()
	s.workspaceMu.Unlock()
	if !stillComplete {
		return false
	}

	if len(allDependents) > 0 {
		s.startWorkspaceDependents(allDependents)
	}
	return true
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
	if err := s.waitForWorkspaceIndex(ctx); err != nil {
		return nil, err
	}
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
			matches = state.index.SearchInRoots(params.Query, state.workspaceRoots, maxWorkspaceSymbols)
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
		if hook := s.testHooks.beforeWorkspaceIdentityCheck; hook != nil {
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

func readRegularWorkspaceFile(path string, maxBytes int64) ([]byte, bool) {
	file, err := openNonBlockingFile(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.IsDir() {
		return nil, false
	}
	if info.Size() > maxBytes {
		return nil, false
	}
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil || int64(len(content)) > maxBytes {
		return nil, false
	}
	return content, true
}
