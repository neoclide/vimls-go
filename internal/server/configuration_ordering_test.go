package server

import (
	"context"
	"io"
	"testing"

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
