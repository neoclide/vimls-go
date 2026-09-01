package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/jsonrpc"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestWorkspaceRootsFromInitializeLegacyFields(t *testing.T) {
	root := t.TempDir()
	rootPath := t.TempDir()
	for name, fields := range map[string]map[string]string{
		"rootUri takes precedence": {"rootUri": uri.File(root).String(), "rootPath": rootPath},
		"rootPath fallback":        {"rootPath": rootPath},
	} {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			var params protocol.InitializeParams
			if err := protocol.Unmarshal(encoded, &params); err != nil {
				t.Fatal(err)
			}
			roots := workspaceRootsFromInitialize(&params)
			wantRoot := rootPath
			if name == "rootUri takes precedence" {
				wantRoot = root
			}
			want, err := filepath.EvalSymlinks(wantRoot)
			if err != nil {
				t.Fatal(err)
			}
			if len(roots) != 1 || roots[0] != want {
				t.Fatalf("roots = %#v, want %#v", roots, []string{want})
			}
		})
	}
}

func TestNegotiatePositionEncoding(t *testing.T) {
	tests := []struct {
		name    string
		general *protocol.GeneralClientCapabilities
		want    text.Encoding
		wire    protocol.PositionEncodingKind
	}{
		{name: "omitted defaults to UTF-16", want: text.UTF16, wire: protocol.PositionEncodingKindUTF16},
		{name: "empty defaults to UTF-16", general: &protocol.GeneralClientCapabilities{}, want: text.UTF16, wire: protocol.PositionEncodingKindUTF16},
		{name: "client preference", general: &protocol.GeneralClientCapabilities{PositionEncodings: []protocol.PositionEncodingKind{protocol.PositionEncodingKindUTF8, protocol.PositionEncodingKindUTF16}}, want: text.UTF8, wire: protocol.PositionEncodingKindUTF8},
		{name: "UTF-32", general: &protocol.GeneralClientCapabilities{PositionEncodings: []protocol.PositionEncodingKind{protocol.PositionEncodingKindUTF32}}, want: text.UTF32, wire: protocol.PositionEncodingKindUTF32},
		{name: "unknown falls back to UTF-16", general: &protocol.GeneralClientCapabilities{PositionEncodings: []protocol.PositionEncodingKind{"custom"}}, want: text.UTF16, wire: protocol.PositionEncodingKindUTF16},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, wire := negotiatePositionEncoding(test.general)
			if got != test.want || wire != test.wire {
				t.Fatalf("encoding = %v/%q, want %v/%q", got, wire, test.want, test.wire)
			}
		})
	}
}

func TestInitializeDoesNotAdvertiseFormatting(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	result, err := instance.Initialize(context.Background(), &protocol.InitializeParams{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := protocol.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range []string{
		`"documentFormattingProvider"`,
		`"documentRangeFormattingProvider"`,
		`"documentOnTypeFormattingProvider"`,
	} {
		if bytes.Contains(encoded, []byte(capability)) {
			t.Fatalf("initialize result advertised %s: %s", capability, encoded)
		}
	}
}

func TestLogfSerializesConcurrentWriters(t *testing.T) {
	var output bytes.Buffer
	instance := New(nil, nil, &output)
	const workers = 16
	const messages = 40
	var group sync.WaitGroup
	for worker := range workers {
		group.Go(func() {
			for message := range messages {
				instance.logf("worker=%d message=%d", worker, message)
			}
		})
	}
	group.Wait()
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != workers*messages {
		t.Fatalf("log lines = %d, want %d", len(lines), workers*messages)
	}
}

func encodeFrames(t *testing.T, bodies ...string) bytes.Buffer {
	t.Helper()
	var input bytes.Buffer
	writer := jsonrpc.NewWriter(&input)
	for _, body := range bodies {
		writeFrame(t, writer, body)
	}
	return input
}

func writeFrame(t *testing.T, writer *jsonrpc.Writer, body string) {
	t.Helper()
	if err := writer.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
}

func readFrame(t *testing.T, reader *jsonrpc.Reader) map[string]json.RawMessage {
	t.Helper()
	body, err := reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	var message map[string]json.RawMessage
	if err := json.Unmarshal(body, &message); err != nil {
		t.Fatal(err)
	}
	return message
}

func decodeFrames(t *testing.T, input io.Reader) []map[string]json.RawMessage {
	t.Helper()
	reader := jsonrpc.NewReader(input)
	var messages []map[string]json.RawMessage
	for {
		body, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return messages
		}
		if err != nil {
			t.Fatal(err)
		}
		var message map[string]json.RawMessage
		if err := json.Unmarshal(body, &message); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
	}
}

func idNumber(t *testing.T, message map[string]json.RawMessage) int {
	t.Helper()
	var id int
	if err := json.Unmarshal(message["id"], &id); err != nil {
		t.Fatal(err)
	}
	return id
}

func errorCode(t *testing.T, message map[string]json.RawMessage) int {
	t.Helper()
	if len(message["error"]) == 0 {
		return 0
	}
	var responseError struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(message["error"], &responseError); err != nil {
		t.Fatal(err)
	}
	return responseError.Code
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

type diagnosticClient struct {
	protocol.UnimplementedClient
	published chan *protocol.PublishDiagnosticsParams
}

type configurationClient struct {
	protocol.UnimplementedClient
	settings protocol.LSPAny
	calls    int
}

func (c *configurationClient) Configuration(_ context.Context, params *protocol.ConfigurationParams) ([]protocol.LSPAny, error) {
	c.calls++
	if len(params.Items) != 1 || params.Items[0].Section == nil || *params.Items[0].Section != "vimls" {
		return nil, errors.New("unexpected configuration section")
	}
	return []protocol.LSPAny{c.settings}, nil
}

func (c *diagnosticClient) PublishDiagnostics(_ context.Context, params *protocol.PublishDiagnosticsParams) error {
	c.published <- params
	return nil
}

func waitForDiagnostics(t *testing.T, published <-chan *protocol.PublishDiagnosticsParams) *protocol.PublishDiagnosticsParams {
	t.Helper()
	select {
	case params := <-published:
		return params
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for diagnostics")
		return nil
	}
}
