//go:build unix || darwin || linux

package integration_test

import (
	"bufio"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSignalSubprocess(t *testing.T) {
	ctx, cancel := subprocessContext(t, 15*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, vimlsBinary)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr safeBuffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	client := newTestClient(t, stdout, stdin, &stderr, ctx)
	writer := client
	reader := client
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	_ = readJSON(t, reader)
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitCommand(t, ctx, command, 5*time.Second, &stderr)
	if ctx.Err() != nil {
		t.Fatalf("signal shutdown timed out: %v", ctx.Err())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestTCPListenerSignalSubprocess(t *testing.T) {
	ctx, cancel := subprocessContext(t, 15*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, vimlsBinary, "--listen", "127.0.0.1:0")
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
