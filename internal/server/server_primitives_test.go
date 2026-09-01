package server

import (
	"context"
	"errors"
	"testing"

	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

type warningClient struct {
	protocol.Client
	err error
}

func (c warningClient) LogMessage(context.Context, *protocol.LogMessageParams) error { return c.err }

type valueKey string

func TestServerProtocolPrimitives(t *testing.T) {
	s := New(nil, nil, nil)
	if value, err := s.Request(context.Background(), "future/method", nil); value != nil || !errors.Is(err, jsonrpc2.ErrMethodNotFound) {
		t.Fatalf("request = %#v, %v", value, err)
	}
	if err := s.sendWarning(context.Background(), "warning"); err != nil {
		t.Fatalf("nil-client warning = %v", err)
	}
	s.client = warningClient{}
	if err := s.sendWarning(context.Background(), "delivered"); err != nil {
		t.Fatalf("delivered warning = %v", err)
	}
	s.client = warningClient{err: errors.New("client disconnected")}
	if err := s.sendWarning(context.Background(), "failed"); err == nil {
		t.Fatal("warning failure was swallowed")
	}
	outer := context.WithValue(context.Background(), valueKey("outer"), "outer")
	inner := context.WithValue(context.Background(), valueKey("inner"), "inner")
	wrapped := valueContext{Context: inner, values: outer}
	if got := wrapped.Value(valueKey("outer")); got != "outer" {
		t.Fatalf("outer value = %#v", got)
	}
	if got := wrapped.Value(valueKey("inner")); got != nil {
		t.Fatalf("inner value leaked = %#v", got)
	}
	if got := wrapped.Value(valueKey("missing")); got != nil {
		t.Fatalf("missing value = %#v", got)
	}
}
