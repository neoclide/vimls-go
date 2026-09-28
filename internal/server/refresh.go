package server

type refreshKind uint8

const (
	refreshDiagnostic refreshKind = iota
	refreshSemanticTokens
	refreshInlayHint
	refreshCodeLens
	refreshKindCount
)

type refreshState struct {
	supported  bool
	generation uint64
	running    bool
}

// Both workspace and runtimepath installations change cross-file name/type
// resolution (diagnostics, semantic tokens and inlay hints) and reference/
// implementation counts (Code Lens). Each sender checks client capabilities;
// diagnostic refresh additionally requires pull diagnostics. Hover, completion
// and navigation read the new index on their next request and have no refresh.
// Call only after installing workspace data, without holding server locks.
func (s *Server) scheduleWorkspaceRefresh(indexComplete bool) {
	s.scheduleRefresh(refreshDiagnostic)
	s.scheduleRefresh(refreshSemanticTokens)
	s.scheduleRefresh(refreshInlayHint)
	if indexComplete {
		s.scheduleRefresh(refreshCodeLens)
	}
}

// scheduleRefresh merges changes occurring while the client request is in
// flight; all client calls are deliberately outside server locks.
func (s *Server) scheduleRefresh(kind refreshKind) {
	s.mu.Lock()
	refresh := &s.refreshes[kind]
	if !refresh.supported || s.state == stateShutdown || s.client == nil || (kind == refreshDiagnostic && !s.pullDiagnostics) {
		s.mu.Unlock()
		return
	}
	refresh.generation++
	if refresh.running {
		s.mu.Unlock()
		return
	}
	refresh.running = true
	s.mu.Unlock()
	go s.runRefresh(kind)
}

func (s *Server) runRefresh(kind refreshKind) {
	for {
		s.mu.Lock()
		refresh := &s.refreshes[kind]
		if s.state == stateShutdown || !refresh.supported || s.client == nil || (kind == refreshDiagnostic && !s.pullDiagnostics) {
			refresh.running = false
			s.mu.Unlock()
			return
		}
		client, generation := s.client, refresh.generation
		s.mu.Unlock()
		var err error
		switch kind {
		case refreshDiagnostic:
			err = client.DiagnosticRefresh(s.analysisContext)
		case refreshSemanticTokens:
			err = client.SemanticTokensRefresh(s.analysisContext)
		case refreshInlayHint:
			err = client.InlayHintRefresh(s.analysisContext)
		case refreshCodeLens:
			err = client.CodeLensRefresh(s.analysisContext)
		}
		if err != nil && s.analysisContext.Err() == nil {
			switch kind {
			case refreshDiagnostic:
				s.logf("vimls: refresh diagnostics: %v", err)
			case refreshSemanticTokens:
				s.logf("vimls: refresh semantic tokens: %v", err)
			case refreshInlayHint:
				s.logf("vimls: refresh inlay hints: %v", err)
			case refreshCodeLens:
				s.logf("vimls: refresh code lenses: %v", err)
			}
		}
		s.mu.Lock()
		if refresh.generation == generation {
			refresh.running = false
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}
