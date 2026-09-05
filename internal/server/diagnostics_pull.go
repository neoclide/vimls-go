package server

import (
	"context"
	"fmt"
	"sort"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type pullDiagnosticKey struct {
	snapshot       *text.Snapshot
	contentID      text.ContentID
	closed         bool
	configRevision uint64
	workspace      workspaceIdentity
}

type pullDiagnosticResult struct {
	key      pullDiagnosticKey
	resultID string
	items    []protocol.Diagnostic
}

func (s *Server) installPullDiagnosticResultLocked(analysis workspace.Analysis, identity workspaceIdentity, items []protocol.Diagnostic) {
	key := pullDiagnosticKey{snapshot: analysis.Snapshot, configRevision: analysis.ConfigRevision, workspace: identity}
	s.installPullDiagnosticResultForKeyLocked(analysis.Snapshot.URI(), key, items)
}

func (s *Server) installPullDiagnosticResultForKeyLocked(documentURI string, key pullDiagnosticKey, items []protocol.Diagnostic) {
	if current, ok := s.pullDiagnosticResults[documentURI]; ok && current.key == key {
		return
	}
	s.nextDiagnosticResultID++
	s.pullDiagnosticResults[documentURI] = pullDiagnosticResult{
		key:      key,
		resultID: fmt.Sprintf("vimls-diagnostic-%d", s.nextDiagnosticResultID),
		items:    append([]protocol.Diagnostic(nil), items...),
	}
}

func fullDiagnosticReport(resultID string, items []protocol.Diagnostic) protocol.DocumentDiagnosticReport {
	return &protocol.RelatedFullDocumentDiagnosticReport{FullDocumentDiagnosticReport: protocol.FullDocumentDiagnosticReport{
		Kind: string(protocol.DocumentDiagnosticReportKindFull), ResultID: &resultID, Items: append([]protocol.Diagnostic{}, items...),
	}}
}

func unchangedDiagnosticReport(resultID string) protocol.DocumentDiagnosticReport {
	return &protocol.RelatedUnchangedDocumentDiagnosticReport{UnchangedDocumentDiagnosticReport: protocol.UnchangedDocumentDiagnosticReport{
		Kind: string(protocol.DocumentDiagnosticReportKindUnchanged), ResultID: resultID,
	}}
}

func (s *Server) Diagnostic(ctx context.Context, params *protocol.DocumentDiagnosticParams) (protocol.DocumentDiagnosticReport, error) {
	if params == nil || params.TextDocument.URI == "" {
		return nil, jsonrpc2.ErrInvalidParams
	}
	for attempt := 0; attempt != 2; attempt++ {
		if ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		// The JSON-RPC request values are borrowed and become invalid when the
		// handler returns. Keep its cancellation signal, but never retain those
		// values in the document's longer-lived analysis context.
		analysisContext := valueContext{Context: ctx, values: s.analysisContext}
		work, open := s.documents.BeginAnalysis(analysisContext, params.TextDocument.URI.String())
		if !open {
			s.publishMu.Lock()
			s.nextDiagnosticResultID++
			resultID := fmt.Sprintf("vimls-diagnostic-%d", s.nextDiagnosticResultID)
			s.publishMu.Unlock()
			return fullDiagnosticReport(resultID, nil), nil
		}
		s.workspaceMu.Lock()
		identity := s.workspaceIdentityLocked()
		s.workspaceMu.Unlock()
		key := pullDiagnosticKey{snapshot: work.Snapshot, configRevision: work.ConfigRevision, workspace: identity}
		s.publishMu.Lock()
		cached, ok := s.pullDiagnosticResults[work.Snapshot.URI()]
		s.workspaceMu.Lock()
		currentWorkspace := s.workspaceIdentityCurrentLocked(identity)
		s.workspaceMu.Unlock()
		currentDocument := s.documents.IsCurrent(work)
		s.publishMu.Unlock()
		if ok && cached.key == key && currentDocument && currentWorkspace {
			if params.PreviousResultID != nil && *params.PreviousResultID == cached.resultID {
				return unchangedDiagnosticReport(cached.resultID), nil
			}
			return fullDiagnosticReport(cached.resultID, cached.items), nil
		}
		file, resultIdentity, computed := s.computeDocumentDiagnostics(work)
		if ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		if !computed || work.Context.Err() != nil {
			if attempt == 1 {
				return nil, protocol.ErrContentModified
			}
			continue
		}
		s.mu.Lock()
		encoding := s.encoding
		related := s.languageFeatures.diagnosticRelatedInformation
		overrides := s.overrideDiagnostics
		maxNumber := s.diagnosticMaxNumber
		s.mu.Unlock()
		items := protocolDiagnostics(work.Snapshot, file, encoding, related, overrides, maxNumber)
		s.publishMu.Lock()
		s.workspaceMu.Lock()
		currentWorkspace = s.workspaceIdentityCurrentLocked(resultIdentity)
		s.workspaceMu.Unlock()
		if s.documents.IsCurrent(work) && currentWorkspace && s.analysisContext.Err() == nil {
			s.installPullDiagnosticResultLocked(work, resultIdentity, items)
			result := s.pullDiagnosticResults[work.Snapshot.URI()]
			s.publishMu.Unlock()
			return fullDiagnosticReport(result.resultID, result.items), nil
		}
		s.publishMu.Unlock()
		if attempt == 1 {
			return nil, protocol.ErrContentModified
		}
	}
	return nil, protocol.ErrContentModified
}

const workspaceDiagnosticIncompleteMessage = "workspace index is incomplete"
const workspaceDiagnosticPartialBatchSize = 100

type workspaceDiagnosticDocument struct {
	path     string
	snapshot *text.Snapshot
	open     bool
	work     workspace.Analysis
}

// DiagnosticWorkspace computes pull diagnostics for every indexed workspace
// Vim file and every open workspace snapshot. It deliberately does not publish
// diagnostics: pull clients own publication through this request.
func (s *Server) DiagnosticWorkspace(ctx context.Context, params *protocol.WorkspaceDiagnosticParams) (*protocol.WorkspaceDiagnosticReport, error) {
	if params == nil {
		return nil, jsonrpc2.ErrInvalidParams
	}
	previous := make(map[string]string, len(params.PreviousResultIds))
	for _, item := range params.PreviousResultIds {
		previous[item.URI.String()] = item.Value
	}
	partialToken := params.PartialResultToken
	for attempt := 0; attempt != 2; attempt++ {
		if ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		if err := s.waitForWorkspaceIndex(ctx); err != nil {
			if ctx.Err() != nil {
				return nil, protocol.ErrRequestCancelled
			}
			return nil, err
		}

		s.workspaceMu.Lock()
		identity := s.workspaceIdentityLocked()
		complete := s.workspaceIndexReadyLocked() && identity.index.Complete()
		roots := append([]string(nil), s.workspaceRoots...)
		diskPaths := make([]string, 0, len(s.workspaceFiles))
		for path := range s.workspaceFiles {
			if workspacePathInRoots(path, roots) {
				diskPaths = append(diskPaths, path)
			}
		}
		s.workspaceMu.Unlock()
		if !complete {
			return nil, jsonrpc2.NewError(jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed), workspaceDiagnosticIncompleteMessage)
		}

		configRevision := s.documents.ConfigRevision()
		documents := make(map[string]workspaceDiagnosticDocument, len(diskPaths))
		for _, path := range diskPaths {
			documents[path] = workspaceDiagnosticDocument{path: path}
		}
		for _, snapshot := range s.documents.Snapshots() {
			path, ok := workspaceURIPath(uri.URI(snapshot.URI()))
			if !ok || !workspacePathInRoots(path, roots) {
				continue
			}
			documents[path] = workspaceDiagnosticDocument{path: path, snapshot: snapshot, open: true}
		}
		ordered := make([]workspaceDiagnosticDocument, 0, len(documents))
		stale := false
		for _, document := range documents {
			if !document.open {
				content, ok := readRegularWorkspaceFile(document.path, maxFileBytes)
				if !ok { // Files can disappear, grow or change type after installation.
					continue
				}
				document.snapshot = text.NewSnapshot(uri.File(document.path).String(), 0, nil, string(content))
			} else {
				work, ok := s.documents.BeginAnalysis(valueContext{Context: ctx, values: s.analysisContext}, document.snapshot.URI())
				if !ok || work.Snapshot != document.snapshot || work.ConfigRevision != configRevision {
					stale = true
					break
				}
				document.work = work
			}
			ordered = append(ordered, document)
		}
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].snapshot.URI() < ordered[j].snapshot.URI() })
		if stale {
			if attempt == 1 {
				return nil, protocol.ErrContentModified
			}
			continue
		}
		enumerated := make(map[string]struct{}, len(ordered))
		for _, document := range ordered {
			enumerated[document.snapshot.URI()] = struct{}{}
		}
		s.publishMu.Lock()
		for documentURI, cached := range s.pullDiagnosticResults {
			if cached.key.closed {
				if _, ok := enumerated[documentURI]; !ok {
					delete(s.pullDiagnosticResults, documentURI)
				}
			}
		}
		s.publishMu.Unlock()

		items := make([]protocol.WorkspaceDocumentDiagnosticReport, 0, len(ordered))
		for _, document := range ordered {
			if ctx.Err() != nil {
				return nil, protocol.ErrRequestCancelled
			}
			key := pullDiagnosticKey{configRevision: configRevision, workspace: identity}
			if document.open {
				key.snapshot = document.snapshot
			} else {
				key.closed = true
				key.contentID = document.snapshot.ContentID()
			}
			s.publishMu.Lock()
			cached, cachedOK := s.pullDiagnosticResults[document.snapshot.URI()]
			s.publishMu.Unlock()
			if !cachedOK || cached.key != key {
				var file *syntax.File
				var resultIdentity workspaceIdentity
				var computed bool
				if document.open {
					file, resultIdentity, computed = s.computeDocumentDiagnostics(document.work)
				} else {
					file, resultIdentity, computed = s.computeClosedWorkspaceDiagnostics(ctx, document.snapshot)
				}
				if !computed || resultIdentity != identity || ctx.Err() != nil {
					stale = true
					break
				}
				s.mu.Lock()
				encoding := s.encoding
				related := s.languageFeatures.diagnosticRelatedInformation
				overrides := s.overrideDiagnostics
				maxNumber := s.diagnosticMaxNumber
				s.mu.Unlock()
				diagnostics := protocolDiagnostics(document.snapshot, file, encoding, related, overrides, maxNumber)
				s.publishMu.Lock()
				s.workspaceMu.Lock()
				currentWorkspace := s.workspaceIdentityCurrentLocked(identity)
				s.workspaceMu.Unlock()
				currentDocuments := s.documents.ConfigRevision() == configRevision
				for _, candidate := range ordered {
					if candidate.open && !s.documents.IsCurrent(candidate.work) {
						currentDocuments = false
						break
					}
				}
				if !currentWorkspace || !currentDocuments {
					s.publishMu.Unlock()
					stale = true
					break
				}
				s.installPullDiagnosticResultForKeyLocked(document.snapshot.URI(), key, diagnostics)
				cached = s.pullDiagnosticResults[document.snapshot.URI()]
				s.publishMu.Unlock()
			}
			version, hasVersion := document.snapshot.Version()
			if previous[document.snapshot.URI()] == cached.resultID {
				items = append(items, &protocol.WorkspaceUnchangedDocumentDiagnosticReport{
					UnchangedDocumentDiagnosticReport: protocol.UnchangedDocumentDiagnosticReport{Kind: string(protocol.DocumentDiagnosticReportKindUnchanged), ResultID: cached.resultID},
					URI:                               uri.URI(document.snapshot.URI()), Version: workspaceDiagnosticVersion(document.open && hasVersion, version),
				})
				continue
			}
			resultID := cached.resultID
			items = append(items, &protocol.WorkspaceFullDocumentDiagnosticReport{
				FullDocumentDiagnosticReport: protocol.FullDocumentDiagnosticReport{Kind: string(protocol.DocumentDiagnosticReportKindFull), ResultID: &resultID, Items: append([]protocol.Diagnostic(nil), cached.items...)},
				URI:                          uri.URI(document.snapshot.URI()), Version: workspaceDiagnosticVersion(document.open && hasVersion, version),
			})
		}
		if ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		if hook := s.testHooks.beforeWorkspaceIdentityCheck; hook != nil {
			hook()
		}
		s.publishMu.Lock()
		s.workspaceMu.Lock()
		currentWorkspace := s.workspaceIdentityCurrentLocked(identity)
		s.workspaceMu.Unlock()
		currentDocuments := s.documents.ConfigRevision() == configRevision
		for _, document := range ordered {
			if document.open && !s.documents.IsCurrent(document.work) {
				currentDocuments = false
				break
			}
		}
		s.publishMu.Unlock()
		if stale || !currentWorkspace || !currentDocuments {
			if attempt == 1 {
				return nil, protocol.ErrContentModified
			}
			continue
		}
		if partialToken != nil && len(items) > 0 {
			s.mu.Lock()
			client := s.client
			s.mu.Unlock()
			if client == nil {
				return nil, jsonrpc2.NewError(jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed), "diagnostic progress unavailable")
			}
			for start := 0; start < len(items); start += workspaceDiagnosticPartialBatchSize {
				end := min(start+workspaceDiagnosticPartialBatchSize, len(items))
				value, err := protocol.Marshal(&protocol.WorkspaceDiagnosticReportPartialResult{Items: append([]protocol.WorkspaceDocumentDiagnosticReport(nil), items[start:end]...)})
				if err == nil {
					err = client.Progress(ctx, &protocol.ProgressParams{Token: partialToken, Value: protocol.LSPAny(value)})
				}
				if err != nil {
					if ctx.Err() != nil {
						return nil, protocol.ErrRequestCancelled
					}
					return nil, err
				}
			}
			return &protocol.WorkspaceDiagnosticReport{Items: []protocol.WorkspaceDocumentDiagnosticReport{}}, nil
		}
		return &protocol.WorkspaceDiagnosticReport{Items: items}, nil
	}
	return nil, protocol.ErrContentModified
}

func workspaceDiagnosticVersion(open bool, version int32) *int32 {
	if !open {
		return nil
	}
	return &version
}

// computeClosedWorkspaceDiagnostics mirrors computeDocumentDiagnostics without
// installing parser or workspace state for a document that is not open.
func (s *Server) computeClosedWorkspaceDiagnostics(ctx context.Context, snapshot *text.Snapshot) (*syntax.File, workspaceIdentity, bool) {
	disabledDiagnostics := s.disabledDiagnosticsSnapshot()
	path, ok := workspaceURIPath(uri.URI(snapshot.URI()))
	if !ok {
		return nil, workspaceIdentity{}, false
	}
	var file *syntax.File
	var fileAnalysis *analysis.FileAnalysis
	if snapshot.ByteLen() > maxFileBytes {
		file = &syntax.File{Diagnostics: []syntax.Diagnostic{{Code: "vimls/file-too-large", Message: "file exceeds the 4 MiB analysis limit"}}}
	} else {
		file = syntax.Parse(snapshot.Text())
		if hook := s.testHooks.beforeAnalyze; hook != nil {
			hook(file)
		}
		fileAnalysis = analyzeWithRole(file, s.IsConfigFile(path))
	}
	if ctx.Err() != nil {
		return nil, workspaceIdentity{}, false
	}
	s.workspaceMu.Lock()
	workspaceSnapshot := s.workspaceAnalysisSnapshotLocked(path, file, fileAnalysis)
	s.workspaceMu.Unlock()
	return s.composeDocumentDiagnostics(ctx, snapshot, file, fileAnalysis, workspaceSnapshot, disabledDiagnostics)
}

// scheduleDiagnosticRefresh merges changes occurring while the client request
// is in flight; all client calls are deliberately outside server locks.
func (s *Server) scheduleDiagnosticRefresh() {
	s.mu.Lock()
	if !s.pullDiagnostics || !s.diagnosticRefreshSupport || s.state == stateShutdown || s.client == nil {
		s.mu.Unlock()
		return
	}
	s.diagnosticRefreshGeneration++
	if s.diagnosticRefreshRunning {
		s.mu.Unlock()
		return
	}
	s.diagnosticRefreshRunning = true
	s.mu.Unlock()
	go s.runDiagnosticRefresh()
}

func (s *Server) runDiagnosticRefresh() {
	for {
		s.mu.Lock()
		if s.state == stateShutdown || !s.diagnosticRefreshSupport || !s.pullDiagnostics || s.client == nil {
			s.diagnosticRefreshRunning = false
			s.mu.Unlock()
			return
		}
		client, generation := s.client, s.diagnosticRefreshGeneration
		s.mu.Unlock()
		if err := client.DiagnosticRefresh(s.analysisContext); err != nil && s.analysisContext.Err() == nil {
			s.logf("vimls: refresh diagnostics: %v", err)
		}
		s.mu.Lock()
		if s.diagnosticRefreshGeneration == generation {
			s.diagnosticRefreshRunning = false
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}
