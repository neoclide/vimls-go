package jsonrpc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

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

type closeReader struct {
	*bytes.Reader
	closes int
}

func (r *closeReader) Close() error {
	r.closes++
	return nil
}
