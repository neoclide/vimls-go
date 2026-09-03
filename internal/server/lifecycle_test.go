package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/jsonrpc"
	"github.com/neoclide/vimls-go/internal/text"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

func TestServerLifecycle(t *testing.T) {
	input := encodeFrames(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`,
		`{"jsonrpc":"2.0","method":"initialized","params":{}}`,
		`{"jsonrpc":"2.0","method":"custom/notification"}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var output, logs bytes.Buffer
	instance := New(&input, &output, &logs)
	if code := instance.Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d, output = %q, logs = %s", code, output.String(), logs.String())
	}
	messages := decodeFrames(t, &output)
	if len(messages) != 2 {
		t.Fatalf("responses = %d, want 2", len(messages))
	}
	if idNumber(t, messages[0]) != 1 || string(messages[0]["result"]) == "null" {
		t.Fatalf("initialize response = %s", messages[0])
	}
	if idNumber(t, messages[1]) != 2 || string(messages[1]["result"]) != "null" {
		t.Fatalf("shutdown response = %s", messages[1])
	}
	if logs.Len() != 0 {
		t.Fatalf("logs = %q", logs.String())
	}
}

func TestServerLifecycleErrors(t *testing.T) {
	input := encodeFrames(t,
		`{"jsonrpc":"2.0","id":0,"method":"textDocument/hover"}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"unknown"}`,
		`{"jsonrpc":"2.0","id":3,"method":"exit"}`,
		`{"jsonrpc":"2.0","id":4,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","id":5,"method":"textDocument/hover"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var output bytes.Buffer
	var logs bytes.Buffer
	if code := New(&input, &output, &logs).Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d, output = %q, logs = %q", code, output.String(), logs.String())
	}
	messages := decodeFrames(t, &output)
	wantCodes := []int{int(jsonrpc2.ServerNotInitialized), 0, int(jsonrpc2.InvalidRequest), int(jsonrpc2.MethodNotFound), int(jsonrpc2.InvalidRequest), 0, int(jsonrpc2.InvalidRequest)}
	if len(messages) != len(wantCodes) {
		t.Fatalf("responses = %d, want %d", len(messages), len(wantCodes))
	}
	for i, want := range wantCodes {
		if got := errorCode(t, messages[i]); got != want {
			t.Fatalf("response %d code = %d, want %d: %s", i, got, want, messages[i])
		}
	}
}

func TestServerLimitsPendingRequests(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	for index := range maxPendingRequests {
		_, cancel := context.WithCancel(context.Background())
		if err := instance.registerCancellation(jsonrpc2.NewNumberID(int64(index)), cancel); err != nil {
			t.Fatalf("request %d was rejected before the limit: %v", index, err)
		}
	}
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := instance.registerCancellation(jsonrpc2.NewNumberID(maxPendingRequests), cancel); err == nil {
		t.Fatal("request above the pending limit was accepted")
	}
}

func TestServerRejectsInvalidLifecycleShapes(t *testing.T) {
	input := encodeFrames(t,
		`{"jsonrpc":"2.0","method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":[]}`,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"initialized"}`,
		`{"jsonrpc":"2.0","id":4,"method":"$/cancelRequest"}`,
		`{"jsonrpc":"2.0","method":"shutdown"}`,
		`{"jsonrpc":"2.0","id":5,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	var output bytes.Buffer
	if code := New(&input, &output, io.Discard).Run(context.Background()); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	messages := decodeFrames(t, &output)
	wantCodes := []int{int(jsonrpc2.ParseError), 0, int(jsonrpc2.InvalidRequest), int(jsonrpc2.InvalidRequest), 0}
	if len(messages) != len(wantCodes) {
		t.Fatalf("responses = %d, want %d: %#v", len(messages), len(wantCodes), messages)
	}
	for i, want := range wantCodes {
		if got := errorCode(t, messages[i]); got != want {
			t.Fatalf("response %d code = %d, want %d", i, got, want)
		}
	}
}

func TestServerReadsWorkspaceConfigurationResponse(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { _ = serverConn.Close() })
	t.Cleanup(func() { _ = clientConn.Close() })
	var logs bytes.Buffer
	instance := New(serverConn, serverConn, &logs)
	done := make(chan int, 1)
	go func() { done <- instance.Run(context.Background()) }()
	writer := jsonrpc.NewWriter(clientConn)
	reader := jsonrpc.NewReader(clientConn)
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{"workspace":{"configuration":true}},"initializationOptions":{"runtimepath":[]}}}`)
	if message := readFrame(t, reader); string(message["id"]) != "1" {
		t.Fatalf("initialize response = %#v", message)
	}
	writeFrame(t, writer, `{"jsonrpc":"2.0","method":"initialized","params":{}}`)
	configuration := readFrame(t, reader)
	if string(configuration["method"]) != `"workspace/configuration"` {
		t.Fatalf("configuration request = %#v", configuration)
	}
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":1,"result":[{"workspace":{"rebuildDebounce":0}}]}`)
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":2,"method":"shutdown"}`)
	if message := readFrame(t, reader); string(message["id"]) != "2" {
		t.Fatalf("shutdown response = %#v", message)
	}
	writeFrame(t, writer, `{"jsonrpc":"2.0","method":"exit"}`)
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d, logs = %q", code, logs.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not exit")
	}
}

func TestServerCancelsInFlightRequest(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { _ = serverConn.Close() })
	t.Cleanup(func() { _ = clientConn.Close() })
	instance := New(serverConn, serverConn, io.Discard)
	waiting := make(chan struct{})
	instance.beforeWorkspaceIndexWaitForTest = func() {
		select {
		case <-waiting:
		default:
			close(waiting)
		}
	}
	done := make(chan int, 1)
	go func() { done <- instance.Run(context.Background()) }()
	writer := jsonrpc.NewWriter(clientConn)
	reader := jsonrpc.NewReader(clientConn)
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if message := readFrame(t, reader); idNumber(t, message) != 1 {
		t.Fatalf("initialize response = %#v", message)
	}
	instance.workspaceMu.Lock()
	instance.workspaceRunning = true
	instance.workspaceMu.Unlock()
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":2,"method":"textDocument/documentSymbol","params":{"textDocument":{"uri":"file:///cancel.vim"}}}`)
	waitForServerRace(t, waiting, "document symbol request")
	if err := clientConn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	writeFrame(t, writer, `{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":2}}`)
	if err := clientConn.SetWriteDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := clientConn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	message := readFrame(t, reader)
	if err := clientConn.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if idNumber(t, message) != 2 || errorCode(t, message) != int(protocol.LSPErrorCodesRequestCancelled) {
		t.Fatalf("cancelled response = %#v", message)
	}
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":3,"method":"shutdown"}`)
	if message := readFrame(t, reader); idNumber(t, message) != 3 {
		t.Fatalf("shutdown response = %#v", message)
	}
	writeFrame(t, writer, `{"jsonrpc":"2.0","method":"exit"}`)
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not exit")
	}
}

func TestServerExitBeforeShutdownFails(t *testing.T) {
	input := encodeFrames(t, `{"jsonrpc":"2.0","method":"exit"}`)
	if code := New(&input, io.Discard, io.Discard).Run(context.Background()); code != 1 {
		t.Fatalf("exit code = %d", code)
	}
}

func TestServerRejectsDuplicateRequestID(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { _ = serverConn.Close() })
	t.Cleanup(func() { _ = clientConn.Close() })
	instance := New(serverConn, serverConn, io.Discard)
	waiting := make(chan struct{})
	instance.beforeWorkspaceIndexWaitForTest = func() {
		select {
		case <-waiting:
		default:
			close(waiting)
		}
	}
	done := make(chan int, 1)
	go func() { done <- instance.Run(context.Background()) }()
	writer := jsonrpc.NewWriter(clientConn)
	reader := jsonrpc.NewReader(clientConn)
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if message := readFrame(t, reader); idNumber(t, message) != 1 {
		t.Fatalf("initialize response = %#v", message)
	}
	instance.workspaceMu.Lock()
	instance.workspaceRunning = true
	instance.workspaceMu.Unlock()

	writeFrame(t, writer, `{"jsonrpc":"2.0","id":2,"method":"textDocument/documentSymbol","params":{"textDocument":{"uri":"file:///duplicate.vim"}}}`)
	waitForServerRace(t, waiting, "document symbol request")

	writeFrame(t, writer, `{"jsonrpc":"2.0","id":2,"method":"textDocument/hover","params":{"textDocument":{"uri":"file:///duplicate.vim"},"position":{"line":0,"character":0}}}`)

	dupMsg := readFrame(t, reader)
	if idNumber(t, dupMsg) != 2 || errorCode(t, dupMsg) != int(jsonrpc2.InvalidRequest) {
		t.Fatalf("duplicate request response = %#v, want InvalidRequest error", dupMsg)
	}

	writeFrame(t, writer, `{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":2}}`)
	cancelMsg := readFrame(t, reader)
	if idNumber(t, cancelMsg) != 2 || errorCode(t, cancelMsg) != int(protocol.LSPErrorCodesRequestCancelled) {
		t.Fatalf("cancelled response = %#v, want RequestCancelled error", cancelMsg)
	}

	writeFrame(t, writer, `{"jsonrpc":"2.0","id":3,"method":"shutdown"}`)
	if message := readFrame(t, reader); idNumber(t, message) != 3 {
		t.Fatalf("shutdown response = %#v", message)
	}
	writeFrame(t, writer, `{"jsonrpc":"2.0","method":"exit"}`)
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not exit")
	}

	instance.mu.Lock()
	remaining := len(instance.cancellations)
	instance.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("cancellations map len = %d, want 0", remaining)
	}
}

func TestServerShutdownWaitsForBackgroundWork(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { _ = serverConn.Close() })
	t.Cleanup(func() { _ = clientConn.Close() })
	instance := New(serverConn, serverConn, io.Discard)
	instance.workspaceDelay = 0

	workerBlocked := make(chan struct{})
	releaseWorker := make(chan struct{})
	instance.beforeWorkspaceBuildForTest = func([]*text.Snapshot) {
		select {
		case <-workerBlocked:
		default:
			close(workerBlocked)
		}
		<-releaseWorker
	}

	done := make(chan int, 1)
	go func() { done <- instance.Run(context.Background()) }()
	writer := jsonrpc.NewWriter(clientConn)
	reader := jsonrpc.NewReader(clientConn)

	writeFrame(t, writer, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if message := readFrame(t, reader); idNumber(t, message) != 1 {
		t.Fatalf("initialize response = %#v", message)
	}

	// Trigger background workspace rebuild
	instance.scheduleWorkspaceRebuild()

	select {
	case <-workerBlocked:
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not start")
	}

	// Send shutdown while worker is blocked
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":2,"method":"shutdown"}`)

	// Verify that shutdown response does not arrive before worker is released
	responseChan := make(chan map[string]json.RawMessage, 1)
	go func() {
		responseChan <- readFrame(t, reader)
	}()

	select {
	case msg := <-responseChan:
		t.Fatalf("shutdown returned before background worker finished: %#v", msg)
	case <-time.After(50 * time.Millisecond):
		// Expected: shutdown is still waiting for background work
	}

	// Release the worker
	close(releaseWorker)

	select {
	case msg := <-responseChan:
		if idNumber(t, msg) != 2 || string(msg["result"]) != "null" {
			t.Fatalf("shutdown response = %#v", msg)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not complete after worker release")
	}

	writeFrame(t, writer, `{"jsonrpc":"2.0","method":"exit"}`)
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not exit")
	}
}
