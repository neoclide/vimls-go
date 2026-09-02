package server

import (
	"context"
	"fmt"

	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

type pullDiagnosticKey struct {
	snapshot       *text.Snapshot
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
	if current, ok := s.pullDiagnosticResults[analysis.Snapshot.URI()]; ok && current.key == key {
		return
	}
	s.nextDiagnosticResultID++
	s.pullDiagnosticResults[analysis.Snapshot.URI()] = pullDiagnosticResult{
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
		if err := s.waitForWorkspaceIndex(ctx); err != nil {
			if ctx.Err() != nil {
				return nil, protocol.ErrRequestCancelled
			}
			return nil, err
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
		s.mu.Unlock()
		items := protocolDiagnostics(work.Snapshot, file, encoding, related, overrides)
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
