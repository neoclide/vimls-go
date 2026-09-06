package server

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"go.lsp.dev/protocol"
)

type orderedConfigurationClient struct {
	protocol.UnimplementedClient
	requests chan chan protocol.LSPAny
}

func (c *orderedConfigurationClient) Configuration(ctx context.Context, _ *protocol.ConfigurationParams) ([]protocol.LSPAny, error) {
	reply := make(chan protocol.LSPAny, 1)
	c.requests <- reply
	select {
	case value := <-reply:
		return []protocol.LSPAny{value}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestWorkspaceConfigurationRejectsOldResponses(t *testing.T) {
	for _, latest := range []string{"push", "pull", "shutdown"} {
		t.Run(latest, func(t *testing.T) {
			s := New(nil, nil, io.Discard)
			t.Cleanup(s.stopAnalysis)
			client := &orderedConfigurationClient{requests: make(chan chan protocol.LSPAny, 2)}
			s.client, s.workspaceConfiguration = client, true
			first := make(chan error, 1)
			go func() { first <- s.refreshWorkspaceConfiguration(context.Background()) }()
			oldReply := <-client.requests
			want := s.diagnosticMaxNumber
			if latest == "shutdown" {
				s.cancelAnalysis()
			} else {
				want = 99
				settings := protocol.LSPAny(`{"diagnostic":{"maxNumber":99}}`)
				if latest == "push" {
					if err := s.applyWorkspaceConfiguration(context.Background(), []byte(settings)); err != nil {
						t.Fatal(err)
					}
				} else {
					second := make(chan error, 1)
					go func() { second <- s.refreshWorkspaceConfiguration(context.Background()) }()
					(<-client.requests) <- settings
					if err := <-second; err != nil {
						t.Fatal(err)
					}
				}
			}
			oldReply <- protocol.LSPAny(`{"diagnostic":{"maxNumber":5}}`)
			if err := <-first; err != nil {
				t.Fatal(err)
			}
			if s.diagnosticMaxNumber != want {
				t.Fatalf("maxNumber=%d want %d", s.diagnosticMaxNumber, want)
			}
		})
	}
}

func TestWorkspaceConfigurationCancelsReplacedRequests(t *testing.T) {
	s := New(nil, nil, io.Discard)
	t.Cleanup(s.stopAnalysis)
	client := &orderedConfigurationClient{requests: make(chan chan protocol.LSPAny, 1)}
	s.client, s.workspaceConfiguration = client, true
	start := func() <-chan error {
		done := make(chan error, 1)
		go func() { done <- s.refreshWorkspaceConfiguration(context.Background()) }()
		<-client.requests // Client has accepted this request, but never replies.
		return done
	}
	wait := func(done <-chan error) {
		t.Helper()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("configuration waiter did not terminate")
		}
	}
	previous := start()
	for range 8 {
		next := start()
		wait(previous)
		previous = next
	}
	if err := s.DidChangeConfiguration(context.Background(), &protocol.DidChangeConfigurationParams{Settings: protocol.LSPAny(`{"diagnostic":{"maxNumber":77}}`)}); err != nil {
		t.Fatal(err)
	}
	wait(previous)
	if s.diagnosticMaxNumber != 77 {
		t.Fatal("direct settings were not installed")
	}
	last := start()
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	wait(last)
}

func TestWorkspaceConfigurationTimeoutPreservesSettingsAndRecovers(t *testing.T) {
	s := New(nil, nil, io.Discard)
	t.Cleanup(s.stopAnalysis)
	client := &orderedConfigurationClient{requests: make(chan chan protocol.LSPAny, 1)}
	s.client, s.workspaceConfiguration = client, true
	s.diagnosticMaxNumber = 77
	// An already-expired deadline exercises the timeout deterministically.
	s.testHooks.configurationTimeout = -time.Second
	if err := s.refreshWorkspaceConfiguration(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("configuration timeout = %v", err)
	}
	<-client.requests
	if s.diagnosticMaxNumber != 77 || s.configurationCancel != nil {
		t.Fatal("timeout changed settings or retained the active request")
	}
	s.testHooks.configurationTimeout = 0
	done := make(chan error, 1)
	go func() { done <- s.refreshWorkspaceConfiguration(context.Background()) }()
	(<-client.requests) <- protocol.LSPAny(`{"diagnostic":{"maxNumber":88}}`)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s.diagnosticMaxNumber != 88 {
		t.Fatal("configuration did not recover after timeout")
	}
}
