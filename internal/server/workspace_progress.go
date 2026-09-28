package server

import (
	"context"
	"fmt"
	"time"

	"go.lsp.dev/protocol"
)

const workspaceProgressCreateTimeout = 100 * time.Millisecond

const workspaceProgressNotificationTimeout = 100 * time.Millisecond

const workspaceProgressEndTimeout = workspaceProgressNotificationTimeout

const workspaceProgressReportLimit = 64

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

// reportWorkspaceIndexProgress identifies a workspace folder at the discovery
// boundary. It is called outside workspace mutexes and does not affect index
// completion when a client cannot receive a notification.
func (s *Server) reportWorkspaceIndexProgress(session *workspaceProgressSession, started bool, root string) bool {
	if !started || !s.workspaceProgressEnabled() {
		return false
	}
	message := fmt.Sprintf("Discovering workspace folder %s", root)
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
