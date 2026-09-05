package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/jsonrpc"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func runtimepathTestSession(t *testing.T) (*Server, *jsonrpc.Writer, *jsonrpc.Reader) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	if err := clientConn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := New(serverConn, serverConn, io.Discard)
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = clientConn.Close()
		_ = serverConn.Close()
		waitForServerRace(t, done, "runtimepath session exit")
	})
	writer, reader := jsonrpc.NewWriter(clientConn), jsonrpc.NewReader(clientConn)
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"initializationOptions":{"runtimepath":[]}}}`)
	if response := readFrame(t, reader); idNumber(t, response) != 1 {
		t.Fatalf("initialize response: %v", response)
	}
	return s, writer, reader
}

func TestRuntimepathWireCancellationAndInput(t *testing.T) {
	s, writer, reader := runtimepathTestSession(t)
	root := t.TempDir()
	started := make(chan struct{})
	observation := make(chan bool, 1)
	s.testHooks.discoverWorkspaceFiles = func(ctx context.Context, _ string, _ int) ([]string, bool, error) {
		s.mu.Lock()
		registered := len(s.cancellations) == 1
		s.mu.Unlock()
		unlocked := s.publishMu.TryLock()
		if unlocked {
			s.publishMu.Unlock()
		}
		observation <- registered && unlocked
		close(started)
		<-ctx.Done()
		return nil, false, ctx.Err()
	}
	request := fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":%q,"params":{"runtimepath":[%q]}}`, MethodDidChangeRuntimepath, root)
	writeFrame(t, writer, request)
	waitForServerRace(t, started, "runtimepath discovery")
	if !<-observation {
		t.Fatal("request not registered or publication lock held during discovery")
	}
	writeFrame(t, writer, request)
	if message := readFrame(t, reader); errorCode(t, message) != -32600 {
		t.Fatalf("duplicate ID accepted: %v", message)
	}
	documentURI := uri.File(filepath.Join(root, "open.vim"))
	writeFrame(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%q,"languageId":"vim","version":1,"text":"let value = 1\n"}}}`, documentURI))
	writeFrame(t, writer, `{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":2}}`)
	for {
		message := readFrame(t, reader)
		if len(message["id"]) == 0 { // didOpen may publish diagnostics.
			continue
		}
		if idNumber(t, message) != 2 || errorCode(t, message) != int(protocol.LSPErrorCodesRequestCancelled) {
			t.Fatalf("cancel response: %v", message)
		}
		break
	}
	if _, ok := s.documents.Snapshot(documentURI.String()); !ok {
		t.Fatal("didOpen was not handled before cancellation")
	}
	s.workspaceMu.Lock()
	defer s.workspaceMu.Unlock()
	if len(s.runtimePaths) != 0 {
		t.Fatal("canceled request changed runtimepath")
	}
}

func TestRuntimepathWireShutdownCancelsDiscovery(t *testing.T) {
	s, writer, reader := runtimepathTestSession(t)
	started, finished := make(chan struct{}), make(chan struct{})
	s.testHooks.discoverWorkspaceFiles = func(ctx context.Context, _ string, _ int) ([]string, bool, error) {
		close(started)
		<-ctx.Done()
		close(finished)
		return nil, false, ctx.Err()
	}
	writeFrame(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":%q,"params":{"runtimepath":[%q]}}`, MethodDidChangeRuntimepath, t.TempDir()))
	waitForServerRace(t, started, "runtimepath discovery")
	writeFrame(t, writer, `{"jsonrpc":"2.0","id":3,"method":"shutdown"}`)
	for range 2 {
		message := readFrame(t, reader)
		if idNumber(t, message) == 3 {
			select {
			case <-finished:
			default:
				t.Fatal("shutdown replied before runtimepath work finished")
			}
		}
	}
}

func TestRuntimepathDeltaNewerEmptyUpdateWaitsForDiscovery(t *testing.T) {
	s := initializeWorkspaceServer(t, t.TempDir())
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	s.testHooks.discoverWorkspaceFiles = func(context.Context, string, int) ([]string, bool, error) {
		close(started)
		<-release
		return nil, false, nil
	}
	done := make(chan error, 1)
	go func() {
		done <- s.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{t.TempDir()}})
	}()
	waitForServerRace(t, started, "older runtimepath discovery")
	newer := make(chan error, 1)
	go func() {
		newer <- s.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{}})
	}()
	select {
	case err := <-newer:
		t.Fatalf("newer update completed before active batch: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-newer; err != nil {
		t.Fatal(err)
	}
	if len(s.runtimePaths) != 0 {
		t.Fatalf("old update replaced newer no-op: %v", s.runtimePaths)
	}
}

func TestRuntimepathDeltaRetriesNewOpenSnapshot(t *testing.T) {
	s := initializeWorkspaceServer(t, t.TempDir())
	root := t.TempDir()
	path := writeWorkspaceFile(t, root, "plugin/new.vim", "let disk = 1\n")
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	calls := 0
	s.testHooks.discoverWorkspaceFiles = func(context.Context, string, int) ([]string, bool, error) {
		calls++
		if calls == 1 {
			close(started)
			<-release
		}
		return []string{mustWorkspaceCanonicalPath(t, path)}, false, nil
	}
	done := make(chan error, 1)
	go func() {
		done <- s.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{root}})
	}()
	waitForServerRace(t, started, "runtimepath discovery")
	s.publishMu.Lock()
	s.documents.Open(uri.File(path).String(), 2, "let overlay = 2\n")
	s.publishMu.Unlock()
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	source, _ := s.workspaceIndex.Source(mustWorkspaceCanonicalPath(t, path))
	if calls != 2 || source != "let overlay = 2\n" {
		t.Fatalf("calls=%d source=%q, want retry with current overlay", calls, source)
	}
}
