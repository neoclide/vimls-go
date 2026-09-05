package server

// Call only after installing workspace data, without holding server locks.
func (s *Server) scheduleWorkspaceRefresh(indexComplete bool) {
	s.scheduleDiagnosticRefresh()
	s.scheduleSemanticTokensRefresh()
	s.scheduleInlayHintRefresh()
	if indexComplete {
		s.scheduleCodeLensRefresh()
	}
}

// scheduleSemanticTokensRefresh merges changes occurring while the client
// request is in flight; all client calls are deliberately outside server locks.
func (s *Server) scheduleSemanticTokensRefresh() {
	s.mu.Lock()
	if !s.semanticTokensRefreshSupport || s.state == stateShutdown || s.client == nil {
		s.mu.Unlock()
		return
	}
	s.semanticTokensRefreshGeneration++
	if s.semanticTokensRefreshRunning {
		s.mu.Unlock()
		return
	}
	s.semanticTokensRefreshRunning = true
	s.mu.Unlock()
	go s.runSemanticTokensRefresh()
}

func (s *Server) runSemanticTokensRefresh() {
	for {
		s.mu.Lock()
		if s.state == stateShutdown || !s.semanticTokensRefreshSupport || s.client == nil {
			s.semanticTokensRefreshRunning = false
			s.mu.Unlock()
			return
		}
		client, generation := s.client, s.semanticTokensRefreshGeneration
		s.mu.Unlock()
		if err := client.SemanticTokensRefresh(s.analysisContext); err != nil && s.analysisContext.Err() == nil {
			s.logf("vimls: refresh semantic tokens: %v", err)
		}
		s.mu.Lock()
		if s.semanticTokensRefreshGeneration == generation {
			s.semanticTokensRefreshRunning = false
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}

// scheduleInlayHintRefresh merges changes occurring while the client request
// is in flight; all client calls are deliberately outside server locks.
func (s *Server) scheduleInlayHintRefresh() {
	s.mu.Lock()
	if !s.inlayHintRefreshSupport || s.state == stateShutdown || s.client == nil {
		s.mu.Unlock()
		return
	}
	s.inlayHintRefreshGeneration++
	if s.inlayHintRefreshRunning {
		s.mu.Unlock()
		return
	}
	s.inlayHintRefreshRunning = true
	s.mu.Unlock()
	go s.runInlayHintRefresh()
}

func (s *Server) runInlayHintRefresh() {
	for {
		s.mu.Lock()
		if s.state == stateShutdown || !s.inlayHintRefreshSupport || s.client == nil {
			s.inlayHintRefreshRunning = false
			s.mu.Unlock()
			return
		}
		client, generation := s.client, s.inlayHintRefreshGeneration
		s.mu.Unlock()
		if err := client.InlayHintRefresh(s.analysisContext); err != nil && s.analysisContext.Err() == nil {
			s.logf("vimls: refresh inlay hints: %v", err)
		}
		s.mu.Lock()
		if s.inlayHintRefreshGeneration == generation {
			s.inlayHintRefreshRunning = false
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}

// scheduleCodeLensRefresh merges changes occurring while the client request is
// in flight; all client calls are deliberately outside server locks.
func (s *Server) scheduleCodeLensRefresh() {
	s.mu.Lock()
	if !s.codeLensRefreshSupport || s.state == stateShutdown || s.client == nil {
		s.mu.Unlock()
		return
	}
	s.codeLensRefreshGeneration++
	if s.codeLensRefreshRunning {
		s.mu.Unlock()
		return
	}
	s.codeLensRefreshRunning = true
	s.mu.Unlock()
	go s.runCodeLensRefresh()
}

func (s *Server) runCodeLensRefresh() {
	for {
		s.mu.Lock()
		if s.state == stateShutdown || !s.codeLensRefreshSupport || s.client == nil {
			s.codeLensRefreshRunning = false
			s.mu.Unlock()
			return
		}
		client, generation := s.client, s.codeLensRefreshGeneration
		s.mu.Unlock()
		if err := client.CodeLensRefresh(s.analysisContext); err != nil && s.analysisContext.Err() == nil {
			s.logf("vimls: refresh code lenses: %v", err)
		}
		s.mu.Lock()
		if s.codeLensRefreshGeneration == generation {
			s.codeLensRefreshRunning = false
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}
