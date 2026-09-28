package server

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

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
	identity := s.workspaceIdentityLocked()
	complete := index != nil && index.Complete()
	pendingCount := len(s.workspacePending)
	rebuilding := s.workspaceRunning
	workspaceRoots := append([]string(nil), s.workspaceRoots...)
	runtimePaths := append([]string(nil), s.runtimePaths...)
	roots := workspaceIndexRoots(workspaceRoots, runtimePaths)
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
		_, selected := index.Source(path) // Discovery may have canonicalized a .vim symlink.
		if workspacePathInRoots(path, workspaceRoots) {
			for _, root := range workspaceRoots {
				if workspace.IsVimSourcePath(root, path) {
					selected = true
					break
				}
			}
		} else if runtimePathSourceInRoots(path, runtimePaths) {
			selected = true
		} else {
			for _, root := range runtimePaths {
				if workspace.IsRuntimePathColorPath(root, path) {
					return false
				}
			}
		}
		if event.Type != protocol.FileChangeTypeDeleted {
			info, err := os.Stat(path)
			if err == nil && info.IsDir() {
				return false
			}
			if err == nil && !info.Mode().IsRegular() {
				return false
			}
			if !selected {
				continue
			}
		} else {
			if !selected {
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
	installed := false
	defer func() {
		if installed {
			s.workspaceMu.Lock()
			complete := s.workspaceIndex != nil && s.workspaceIndex.Complete()
			s.workspaceMu.Unlock()
			s.scheduleWorkspaceRefresh(complete)
		}
	}()
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
				if !s.watchedFileInstallCurrent(ctx, identity) {
					s.publishMu.Unlock()
					return false
				}
				_, hadSource := index.Source(path)
				_, deps := s.replaceWorkspaceFileWithAnalysisSnapshot(uri.File(path).String(), nil, nil)
				installed = installed || hadSource
				s.workspaceMu.Lock()
				delete(s.workspaceFiles, path)
				s.workspaceRevision++
				identity = s.workspaceIdentityLocked()
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
			if hook := s.testHooks.beforeWatchedFileInstall; hook != nil {
				hook(path)
			}
			s.publishMu.Lock()
			_, _, open = s.openWorkspaceSnapshotLocked(path)
			if open {
				s.publishMu.Unlock()
				continue
			}
			if !s.watchedFileInstallCurrent(ctx, identity) {
				s.publishMu.Unlock()
				return false
			}
			_, hadSource := index.Source(path)
			_, deps := s.replaceWorkspaceFileWithAnalysisSnapshot(uri.File(path).String(), nil, nil)
			installed = installed || hadSource
			s.workspaceMu.Lock()
			s.workspaceFiles[path] = struct{}{}
			s.workspaceRevision++
			identity = s.workspaceIdentityLocked()
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
		fileAnalysis := s.analyzeFile(ctx, file, false)

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
		if !s.watchedFileInstallCurrent(ctx, identity) {
			s.publishMu.Unlock()
			return false
		}
		_, deps := s.replaceWorkspaceFileWithAnalysisSnapshot(uri.File(path).String(), file, fileAnalysis)
		installed = true
		s.workspaceMu.Lock()
		s.workspaceFiles[path] = struct{}{}
		s.workspaceRevision++
		identity = s.workspaceIdentityLocked()
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

// The caller holds publishMu through the subsequent install. Advancing the
// revision after installation also invalidates a rebuild started in between.
func (s *Server) watchedFileInstallCurrent(ctx context.Context, identity workspaceIdentity) bool {
	s.workspaceMu.Lock()
	defer s.workspaceMu.Unlock()
	return ctx.Err() == nil && s.analysisContext.Err() == nil && !s.workspaceRunning && s.workspaceIdentityCurrentLocked(identity)
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
	filePattern := workspace.VimFileWatchPattern()
	for _, root := range roots {
		var pattern protocol.GlobPattern = protocol.Pattern(filepath.ToSlash(filepath.Join(root, filePattern)))
		if relative {
			pattern = &protocol.RelativePattern{BaseURI: protocol.URI(uri.File(root)), Pattern: protocol.Pattern(filePattern)}
		}
		watchers = append(watchers, protocol.FileSystemWatcher{GlobPattern: pattern, Kind: kind})
	}
	return watchers
}
