package server

import (
	"context"
	"sync"
	"time"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/workspace"
)

const analysisInputBackoff = 150 * time.Millisecond

// completionPriority suspends background semantic work at cooperative
// checkpoints. Syntax parsing is deliberately outside this gate: completion
// may itself be waiting for the same in-flight parse.
type completionPriority struct {
	mu        sync.Mutex
	active    int
	resumed   chan struct{}
	idleAfter time.Time
}

// Edits postpone background work through the gaps between completion requests.
// No timer goroutine survives the waiter; shutdown cancels every wait directly.
func (p *completionPriority) userActivity() {
	p.mu.Lock()
	p.idleAfter = time.Now().Add(analysisInputBackoff)
	p.mu.Unlock()
}

func (p *completionPriority) begin() func() {
	p.mu.Lock()
	if p.active == 0 {
		p.resumed = make(chan struct{})
	}
	p.active++
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		p.active--
		if p.active == 0 {
			close(p.resumed)
			p.resumed = nil
		}
		p.mu.Unlock()
	}
}

func (p *completionPriority) wait(ctx context.Context, paused func()) error {
	notified := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		p.mu.Lock()
		resumed := p.resumed
		delay := time.Until(p.idleAfter)
		p.mu.Unlock()
		if resumed == nil && delay <= 0 {
			return nil
		}
		if paused != nil && !notified {
			paused()
			notified = true
		}
		if resumed == nil {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			// An edit can extend the deadline or a completion can start while
			// the timer is running. Recheck both before allowing work through.
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-resumed:
		}
	}
}

func (s *Server) analysisCheckpoint(ctx context.Context) error {
	if hook := s.testHooks.beforeAnalysisCheckpoint; hook != nil {
		hook()
	}
	return s.completionPriority.wait(ctx, s.testHooks.analysisPaused)
}

func (s *Server) analyzeFile(ctx context.Context, file *syntax.File, configFile bool) *analysis.FileAnalysis {
	result, _ := analysis.AnalyzeWithYield(file, configFile, func() error {
		return s.analysisCheckpoint(ctx)
	})
	return result
}

func (s *Server) parseAndAnalyzeSources(ctx context.Context, sources []string) []workspace.AnalyzedSource {
	return workspace.ParseAndAnalyzeSourcesWithYield(ctx, sources, 0, func(workerContext context.Context) error {
		return s.analysisCheckpoint(workerContext)
	})
}
