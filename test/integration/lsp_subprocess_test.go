package integration_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/chemzqm/vimls-go/internal/jsonrpc"
)

func TestLSPSubprocess(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "run", "-mod=readonly", "./cmd/vimls")
	command.Dir = root
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}

	writer := jsonrpc.NewWriter(stdin)
	reader := jsonrpc.NewReader(stdout)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{},"initializationOptions":{"targetVersion":"9.1.1232"}}}`)
	initialize := readJSON(t, reader)
	if string(initialize["id"]) != "1" || !strings.Contains(string(initialize["result"]), `"name":"vimls"`) || !strings.Contains(string(initialize["result"]), `"documentSymbolProvider":true`) || !strings.Contains(string(initialize["result"]), `"foldingRangeProvider":true`) || !strings.Contains(string(initialize["result"]), `"selectionRangeProvider":true`) {
		t.Fatalf("initialize response = %s", initialize)
	}

	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"initialized","params":{}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///symbols.vim","languageId":"vim","version":1,"text":"vim9script\nvar value: number = 1\nclass Widget\n  def new()\n    if true\n      echo value\n    endif\n  enddef\nendclass\n"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":2,"method":"textDocument/documentSymbol","params":{"textDocument":{"uri":"file:///symbols.vim"}}}`)
	symbols := readJSON(t, reader)
	if string(symbols["id"]) != "2" || !strings.Contains(string(symbols["result"]), `"name":"value"`) || !strings.Contains(string(symbols["result"]), `"name":"Widget"`) || !strings.Contains(string(symbols["result"]), `"name":"new"`) {
		t.Fatalf("document symbols = %s", symbols)
	}

	writeJSON(t, writer, `{"jsonrpc":"2.0","id":3,"method":"textDocument/definition","params":{"textDocument":{"uri":"file:///symbols.vim"},"position":{"line":5,"character":12}}}`)
	definition := readJSON(t, reader)
	if string(definition["id"]) != "3" || !strings.Contains(string(definition["result"]), `"start":{"line":1,"character":4}`) {
		t.Fatalf("definition = %s", definition)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":4,"method":"textDocument/declaration","params":{"textDocument":{"uri":"file:///symbols.vim"},"position":{"line":5,"character":12}}}`)
	declaration := readJSON(t, reader)
	if string(declaration["id"]) != "4" || !strings.Contains(string(declaration["result"]), `"start":{"line":1,"character":4}`) {
		t.Fatalf("declaration = %s", declaration)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":5,"method":"textDocument/references","params":{"textDocument":{"uri":"file:///symbols.vim"},"position":{"line":5,"character":12},"context":{"includeDeclaration":true}}}`)
	references := readJSON(t, reader)
	if string(references["id"]) != "5" || !strings.Contains(string(references["result"]), `"line":1,"character":4`) || !strings.Contains(string(references["result"]), `"line":5,"character":11`) {
		t.Fatalf("references = %s", references)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":6,"method":"textDocument/documentHighlight","params":{"textDocument":{"uri":"file:///symbols.vim"},"position":{"line":5,"character":12}}}`)
	highlights := readJSON(t, reader)
	if string(highlights["id"]) != "6" || !strings.Contains(string(highlights["result"]), `"line":1,"character":4`) || !strings.Contains(string(highlights["result"]), `"line":5,"character":11`) {
		t.Fatalf("document highlights = %s", highlights)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":7,"method":"textDocument/hover","params":{"textDocument":{"uri":"file:///symbols.vim"},"position":{"line":5,"character":12}}}`)
	hover := readJSON(t, reader)
	if string(hover["id"]) != "7" || !strings.Contains(string(hover["result"]), `name: value`) || !strings.Contains(string(hover["result"]), `type: number`) {
		t.Fatalf("hover = %s", hover)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":8,"method":"textDocument/foldingRange","params":{"textDocument":{"uri":"file:///symbols.vim"}}}`)
	folding := readJSON(t, reader)
	if string(folding["id"]) != "8" || !strings.Contains(string(folding["result"]), `"startLine":2`) || !strings.Contains(string(folding["result"]), `"endLine":8`) {
		t.Fatalf("folding ranges = %s", folding)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":9,"method":"textDocument/selectionRange","params":{"textDocument":{"uri":"file:///symbols.vim"},"positions":[{"line":5,"character":12}]}}`)
	selection := readJSON(t, reader)
	if string(selection["id"]) != "9" || !strings.Contains(string(selection["result"]), `"start":{"line":5,"character":11}`) {
		t.Fatalf("selection ranges = %s", selection)
	}

	writeJSON(t, writer, `{"jsonrpc":"2.0","id":10,"method":"shutdown"}`)
	shutdown := readJSON(t, reader)
	if string(shutdown["id"]) != "10" || string(shutdown["result"]) != "null" {
		t.Fatalf("shutdown response = %s", shutdown)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"exit"}`)
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("server failed: %v, stderr: %s", err, stderr.String())
	}
	if ctx.Err() != nil {
		t.Fatalf("server timed out: %v", ctx.Err())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestVersionSubprocess(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "run", "-mod=readonly", "./cmd/vimls", "--version")
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("version failed: %v, output: %s", err, output)
	}
	if string(output) != "vimls dev\n" {
		t.Fatalf("version output = %q", output)
	}
}

func TestTCPSubprocess(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "run", "-mod=readonly", "./cmd/vimls", "--listen", "127.0.0.1:0")
	command.Dir = root
	stderr, err := command.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stderr).ReadString('\n')
	if err != nil {
		t.Fatalf("read listen address: %v", err)
	}
	const prefix = "vimls: listening on tcp://"
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("listen output = %q", line)
	}
	address := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	connection, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", address, err)
	}
	writer := jsonrpc.NewWriter(connection)
	reader := jsonrpc.NewReader(connection)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`)
	initialize := readJSON(t, reader)
	if string(initialize["id"]) != "1" {
		t.Fatalf("initialize response = %s", initialize)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":2,"method":"shutdown"}`)
	shutdown := readJSON(t, reader)
	if string(shutdown["id"]) != "2" {
		t.Fatalf("shutdown response = %s", shutdown)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"exit"}`)
	_ = connection.Close()
	if err := command.Wait(); err != nil {
		t.Fatalf("TCP server failed: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("TCP server timed out: %v", ctx.Err())
	}
}

func TestSignalSubprocess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM is not portable to Windows")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "vimls")
	build := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-o", binary, "./cmd/vimls")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v, output: %s", err, output)
	}

	command := exec.CommandContext(ctx, binary)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	writer := jsonrpc.NewWriter(stdin)
	reader := jsonrpc.NewReader(stdout)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	_ = readJSON(t, reader)
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("signal shutdown failed: %v, stderr: %s", err, stderr.String())
	}
	if ctx.Err() != nil {
		t.Fatalf("signal shutdown timed out: %v", ctx.Err())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestTCPListenerSignalSubprocess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM is not portable to Windows")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "vimls")
	build := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-o", binary, "./cmd/vimls")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v, output: %s", err, output)
	}

	command := exec.CommandContext(ctx, binary, "--listen", "127.0.0.1:0")
	stderr, err := command.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stderr).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "vimls: listening on tcp://") {
		t.Fatalf("listen output = %q, error = %v", line, err)
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("TCP listener signal shutdown failed: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("TCP listener signal shutdown timed out: %v", ctx.Err())
	}
}

func writeJSON(t *testing.T, writer *jsonrpc.Writer, body string) {
	t.Helper()
	if err := writer.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, reader *jsonrpc.Reader) map[string]json.RawMessage {
	t.Helper()
	body, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			t.Fatal("unexpected server EOF")
		}
		t.Fatal(err)
	}
	var message map[string]json.RawMessage
	if err := json.Unmarshal(body, &message); err != nil {
		t.Fatal(err)
	}
	return message
}
