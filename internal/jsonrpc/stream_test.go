package jsonrpc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	jsonrpc2 "go.lsp.dev/jsonrpc2"
)

func TestStreamReadsWritesAndCloses(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","method":"ready"}`)
	input := &closeReader{Reader: bytes.NewReader(frame(body))}
	var output bytes.Buffer
	stream := NewStream(input, &output)
	message, size, err := stream.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	request, ok := message.(jsonrpc2.RequestMessage)
	if !ok || request.Method() != "ready" || size != frameSize(len(body)) {
		t.Fatalf("message = %#v, size = %d", message, size)
	}

	written, err := stream.Write(context.Background(), jsonrpc2.NewNotification("done", nil))
	if err != nil {
		t.Fatal(err)
	}
	writtenBody, err := NewReader(&output).Read()
	if err != nil {
		t.Fatal(err)
	}
	if written != frameSize(len(writtenBody)) || !bytes.Contains(writtenBody, []byte(`"method":"done"`)) {
		t.Fatalf("body = %s, size = %d", writtenBody, written)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if input.closes != 1 {
		t.Fatalf("close count = %d", input.closes)
	}
}

func TestStreamHonorsContextAndBounds(t *testing.T) {
	stream := NewStream(bytes.NewReader(nil), io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := stream.ReadFrame(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadFrame error = %v", err)
	}
	if _, err := stream.WriteFrame(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("WriteFrame error = %v", err)
	}
	if _, err := stream.Write(ctx, jsonrpc2.NewNotification("x", nil)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Write error = %v", err)
	}

	params := jsonrpc2.RawMessage(bytes.Repeat([]byte(" "), DefaultMaxMessageBytes+1))
	if _, err := stream.Write(context.Background(), jsonrpc2.NewNotification("large", params)); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("large Write error = %v", err)
	}
}

func TestStreamSkipsMalformedMessageAndReturnsNextDecodedMessage(t *testing.T) {
	valid := []byte(`{"jsonrpc":"2.0","method":"ready"}`)
	input := append(frame([]byte(`{`)), frame(valid)...)
	var output bytes.Buffer
	stream := NewStream(bytes.NewReader(input), &output)
	message, size, err := stream.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	request, ok := message.(jsonrpc2.RequestMessage)
	if !ok || request.Method() != "ready" || size != frameSize(len(valid)) {
		t.Fatalf("message = %#v, size = %d", message, size)
	}
	responseBody, err := NewReader(&output).Read()
	if err != nil {
		t.Fatal(err)
	}
	response, err := jsonrpc2.DecodeMessage(responseBody)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := response.(*jsonrpc2.Response); !ok {
		t.Fatalf("malformed-message response = %#v", response)
	}
}

type closeReader struct {
	*bytes.Reader
	closes int
}

func (r *closeReader) Close() error {
	r.closes++
	return nil
}

type blockedStreamInput struct {
	*bytes.Reader
	entered chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (r *blockedStreamInput) Read(p []byte) (int, error) {
	if r.Len() != 0 {
		return r.Reader.Read(p)
	}
	close(r.entered)
	<-r.closed
	return 0, io.ErrClosedPipe
}

func (r *blockedStreamInput) Close() error {
	r.once.Do(func() { close(r.closed) })
	return nil
}

func TestStreamCloseReleasesBlockedRead(t *testing.T) {
	for _, partial := range []string{"", "Content-Length: 99\r\n", "Content-Length: 99\r\n\r\n{"} {
		t.Run(partial, func(t *testing.T) {
			input := &blockedStreamInput{Reader: bytes.NewReader([]byte(partial)), entered: make(chan struct{}), closed: make(chan struct{})}
			stream := NewStream(input, io.Discard)
			t.Cleanup(func() { _ = stream.Close() })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, _, err := stream.Read(ctx); done <- err }()
			<-input.entered // Prove Read has entered the underlying transport.
			cancel()
			if err := stream.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("closed partial frame unexpectedly succeeded")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Close did not release the read")
			}
		})
	}
}
