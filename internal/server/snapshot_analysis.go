package server

import (
	"context"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
)

// parseSnapshotContext shares only syntax work. A completion or folding request
// can consume the immutable tree while full analysis of that tree is running.
// Cancellation of a waiter does not cancel the shared parse.
func (s *Server) parseSnapshotContext(ctx context.Context, snapshot *text.Snapshot) *syntax.File {
	if snapshot == nil || snapshot.ByteLen() > maxFileBytes || ctx.Err() != nil {
		return nil
	}
	key := parseInFlightKey{uri: snapshot.URI(), contentID: snapshot.ContentID(), configFile: s.configFileRoleForURI(snapshot.URI())}
	source := snapshot.Text()
	s.publishMu.Lock()
	if cached := s.parsed[key.uri]; cached.file != nil && cached.contentID == key.contentID && cached.file.Source == source {
		s.publishMu.Unlock()
		return cached.file
	}
	// An old snapshot may no longer occupy the URI cache but still have a
	// shared analysis in flight. Reuse its tree as well (including A/B/A edits).
	for analysisKey, running := range s.analysisInFlight {
		if analysisKey.parseInFlightKey == key && running.file.Source == source {
			s.publishMu.Unlock()
			return running.file
		}
	}
	if running := s.parseInFlight[key]; running != nil && running.source == source {
		s.publishMu.Unlock()
		if hook := s.testHooks.beforeInFlightWait; hook != nil {
			hook(snapshot)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-running.done:
			return running.file
		}
	}
	running := &inFlightParse{source: source, done: make(chan struct{})}
	s.parseInFlight[key] = running
	s.publishMu.Unlock()
	if hook := s.testHooks.beforeParseSnapshotCacheMiss; hook != nil {
		hook(snapshot)
	}
	running.file = syntax.Parse(source)
	importCache := newImportTypeCache(running.file)
	s.publishMu.Lock()
	if s.parseInFlight[key] == running {
		delete(s.parseInFlight, key)
	}
	if current, ok := s.documents.Snapshot(key.uri); ok && current == snapshot && s.analysisContext.Err() == nil {
		s.parsed[key.uri] = parsedDocument{contentID: key.contentID, configFile: key.configFile, file: running.file, imports: importCache}
	}
	close(running.done)
	s.publishMu.Unlock()
	return running.file
}

// analyzeSnapshotContext deduplicates full analysis separately from parsing.
// A waiting request can cancel without interrupting another consumer's work.
func (s *Server) analyzeSnapshotContext(ctx context.Context, snapshot *text.Snapshot) (*syntax.File, *analysis.FileAnalysis) {
	file := s.parseSnapshotContext(ctx, snapshot)
	if file == nil {
		return nil, nil
	}
	return s.snapshotFacts(ctx, snapshot, file, false)
}

// completionSnapshotFacts never waits for or initiates full file analysis.
// Current full results can be reused, but a cold completion builds exclusively
// owned lexical/type facts and never installs those as diagnostic analysis.
func (s *Server) completionSnapshotFacts(ctx context.Context, snapshot *text.Snapshot, file *syntax.File) (*syntax.File, *analysis.FileAnalysis) {
	return s.snapshotFacts(ctx, snapshot, file, true)
}

func (s *Server) snapshotFacts(ctx context.Context, snapshot *text.Snapshot, file *syntax.File, completion bool) (*syntax.File, *analysis.FileAnalysis) {
	if ctx.Err() != nil {
		return nil, nil
	}
	key := analysisInFlightKey{parseInFlightKey: parseInFlightKey{uri: snapshot.URI(), contentID: snapshot.ContentID(), configFile: s.configFileRoleForURI(snapshot.URI())}, completion: completion}
	s.publishMu.Lock()
	cache := s.parsed[key.uri].imports
	if cached := s.parsed[key.uri]; cached.file != file {
		cache = newImportTypeCache(file)
	}
	var imports analysis.ImportTypes
	if cache != nil {
		s.publishMu.Unlock()
		var err error
		imports, err = cache.load(ctx, s, snapshot.URI())
		if err != nil {
			return nil, nil
		}
		s.publishMu.Lock()
	}
	key.imports = imports
	if cached := s.parsed[key.uri]; cached.file == file && cached.contentID == key.contentID && cached.configFile == key.configFile {
		facts := cached.analysis
		if facts != nil && facts.ImportTypes() != imports {
			facts = nil
		}
		if completion && facts == nil {
			facts = cached.completion
		}
		if facts != nil && facts.ImportTypes() == imports && cache.current(s, imports) {
			s.publishMu.Unlock()
			return file, facts
		}
	}
	if running := s.analysisInFlight[key]; running != nil && running.file.Source == file.Source && (running.context == nil || running.context.Err() == nil) {
		s.publishMu.Unlock()
		if hook := s.testHooks.beforeAnalysisInFlightWait; hook != nil {
			hook(snapshot)
		}
		select {
		case <-ctx.Done():
			return nil, nil
		case <-running.done:
			if running.analysis == nil {
				return nil, nil
			}
			if current, err := cache.load(ctx, s, snapshot.URI()); err != nil || current != imports {
				return nil, nil
			}
			return running.file, running.analysis
		}
	}
	if cache != nil {
		for oldKey, old := range s.analysisInFlight {
			if oldKey.parseInFlightKey == key.parseInFlightKey && oldKey.imports != imports && old.cancel != nil {
				old.cancel()
			}
		}
	}
	running := &inFlightAnalysis{file: file, done: make(chan struct{})}
	if !completion {
		running.context, running.cancel = context.WithCancel(s.analysisContext)
		defer running.cancel()
		current, ok := s.documents.Snapshot(key.uri)
		if !ok || current.ContentID() != key.contentID || current.Text() != file.Source {
			running.cancel()
		}
	}
	s.analysisInFlight[key] = running
	s.publishMu.Unlock()
	if completion {
		running.analysis = analysis.CollectCompletionFactsWithImports(file, imports)
	} else {
		if hook := s.testHooks.beforeAnalyze; hook != nil {
			hook(file)
		}
		// Owned by this content computation, never by an individual waiter.
		// Edits/close and shutdown also wake a pass paused inside a loop.
		running.analysis, _ = analysis.AnalyzeWithOptions(file, analysis.Options{ConfigFile: key.configFile, Imports: imports, Yield: func() error {
			if err := s.analysisCheckpoint(running.context); err != nil {
				return err
			}
			current, err := cache.load(running.context, s, snapshot.URI())
			if err != nil {
				return err
			}
			if current != imports {
				return context.Canceled
			}
			return nil
		}})
	}
	validationContext := s.analysisContext
	if running.context != nil {
		validationContext = running.context
	}
	currentImports, loadErr := cache.load(validationContext, s, snapshot.URI())
	s.publishMu.Lock()
	if loadErr != nil || currentImports != imports || !cache.current(s, imports) {
		running.analysis = nil
	}
	if s.analysisInFlight[key] == running {
		delete(s.analysisInFlight, key)
	}
	if current, ok := s.documents.Snapshot(key.uri); ok && current == snapshot && s.analysisContext.Err() == nil {
		cached := s.parsed[key.uri]
		if cached.file == file && cached.contentID == key.contentID {
			if cached.configFile != key.configFile {
				cached.analysis, cached.completion = nil, nil
				cached.configFile = key.configFile
			}
			if completion {
				cached.completion = running.analysis
			} else {
				cached.analysis = running.analysis
			}
			s.parsed[key.uri] = cached
		}
	}
	close(running.done)
	s.publishMu.Unlock()
	if running.analysis == nil {
		return nil, nil
	}
	return file, running.analysis
}

// Caller holds publishMu. Identical content may still share useful computation,
// but canceled A/B/A work is never resurrected or joined by a later request.
func (s *Server) cancelStaleAnalysisLocked(documentURI string, current *text.Snapshot) {
	for key, running := range s.analysisInFlight {
		if key.uri != documentURI || running.cancel == nil {
			continue
		}
		if current == nil || current.ContentID() != key.contentID || current.Text() != running.file.Source {
			running.cancel()
		}
	}
}
