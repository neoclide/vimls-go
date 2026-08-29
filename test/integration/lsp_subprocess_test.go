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
	if string(initialize["id"]) != "1" || !strings.Contains(string(initialize["result"]), `"name":"vimls"`) || !strings.Contains(string(initialize["result"]), `"documentSymbolProvider":true`) {
		t.Fatalf("initialize response = %s", initialize)
	}

	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"initialized","params":{}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///symbols.vim","languageId":"vim","version":1,"text":"vim9script\nclass Widget\n  def new()\n  enddef\nendclass\n"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":2,"method":"textDocument/documentSymbol","params":{"textDocument":{"uri":"file:///symbols.vim"}}}`)
	symbols := readJSON(t, reader)
	if string(symbols["id"]) != "2" || !strings.Contains(string(symbols["result"]), `"name":"Widget"`) || !strings.Contains(string(symbols["result"]), `"name":"new"`) {
		t.Fatalf("document symbols = %s", symbols)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":3,"method":"shutdown"}`)
	shutdown := readJSON(t, reader)
	if string(shutdown["id"]) != "3" || string(shutdown["result"]) != "null" {
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
