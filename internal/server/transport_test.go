package server

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/jsonrpc"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
)

func TestServerRecoversAfterMalformedJSONBody(t *testing.T) {
	input := encodeFrames(t,
		`{`,
		`{"jsonrpc":"2.0","id":true,"method":"bad-id"}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var output, logs bytes.Buffer
	if code := New(&input, &output, &logs).Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d, output = %q, logs = %q", code, output.String(), logs.String())
	}
	messages := decodeFrames(t, &output)
	if len(messages) != 4 || errorCode(t, messages[0]) != int(jsonrpc2.ParseError) || string(messages[1]["id"]) != "null" {
		t.Fatalf("responses = %#v", messages)
	}
}

func TestServerDoesNotRespondToInvalidNotificationParams(t *testing.T) {
	input := encodeFrames(t,
		`{"jsonrpc":"2.0","method":"ignored","params":"invalid"}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var output bytes.Buffer
	if code := New(&input, &output, io.Discard).Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if messages := decodeFrames(t, &output); len(messages) != 2 {
		t.Fatalf("responses = %d, want 2", len(messages))
	}
}

func TestServerFramingAndOutputFailuresAreControlled(t *testing.T) {
	var logs bytes.Buffer
	code := New(strings.NewReader("invalid"), io.Discard, &logs).Run(context.Background())
	if code != 1 || !strings.Contains(logs.String(), "connection error") {
		t.Fatalf("code = %d, logs = %q", code, logs.String())
	}

	input := encodeFrames(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	logs.Reset()
	code = New(&input, errorWriter{}, &logs).Run(context.Background())
	if code != 1 || !strings.Contains(logs.String(), "connection error") {
		t.Fatalf("code = %d, logs = %q", code, logs.String())
	}
}

func TestServerCancellationAndEOFExitCleanly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := New(strings.NewReader("blocked input is not read"), io.Discard, io.Discard).Run(ctx); code != 0 {
		t.Fatalf("canceled exit code = %d", code)
	}
	if code := New(strings.NewReader(""), io.Discard, io.Discard).Run(context.Background()); code != 0 {
		t.Fatalf("EOF exit code = %d", code)
	}

	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel = context.WithCancel(context.Background())
	result := make(chan int, 1)
	go func() {
		result <- New(reader, io.Discard, io.Discard).Run(ctx)
	}()
	cancel()
	select {
	case code := <-result:
		if code != 0 {
			t.Fatalf("blocked cancellation exit code = %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked read did not stop after cancellation")
	}
}

func TestServerSoakNotifications(t *testing.T) {
	const notifications = 10_000
	var input bytes.Buffer
	writer := jsonrpc.NewWriter(&input)
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	for range notifications {
		writeFrame(t, writer, `{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":999}}`)
	}
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":2,"method":"shutdown"}`)
	writeFrame(t, writer, `{"jsonrpc":"2.0","method":"exit"}`)

	var output bytes.Buffer
	if code := New(&input, &output, io.Discard).Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if messages := decodeFrames(t, &output); len(messages) != 2 {
		t.Fatalf("responses = %d", len(messages))
	}
}
