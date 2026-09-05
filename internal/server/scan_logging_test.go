package server

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestScanLogsOneTotalDurationPerBatch(t *testing.T) {
	for _, kind := range []string{"runtimepath", "runtime help"} {
		t.Run(kind, func(t *testing.T) {
			s := New(nil, nil, io.Discard)
			t.Cleanup(s.stopAnalysis)
			client := &indexingClient{}
			s.client = client
			roots := []string{t.TempDir(), t.TempDir()}
			for _, root := range roots {
				writeWorkspaceFile(t, root, "plugin/test.vim", "let g:example = 1\n")
				writeWorkspaceFile(t, root, "doc/test.txt", "*Example*\nExample help.\n")
			}
			if kind == "runtimepath" {
				s.discoverRuntimeRoots(context.Background(), roots, 100)
			} else {
				s.setRuntimePaths(roots)
				s.runtimeHelpWG.Wait()
			}
			client.mu.Lock()
			defer client.mu.Unlock()
			if len(client.logs) != 1 {
				t.Fatalf("batch logs = %v, want one total for both roots", client.logs)
			}
			message := client.logs[0]
			prefix, duration, ok := strings.Cut(message, "; total elapsed ")
			if !ok || !strings.HasPrefix(prefix, "vimls: scanned "+kind) {
				t.Fatalf("scan log = %q", message)
			}
			if elapsed, err := time.ParseDuration(duration); err != nil || elapsed < 0 {
				t.Fatalf("total elapsed = %q, error = %v", duration, err)
			}
			if kind == "runtimepath" && !strings.Contains(prefix, "2 roots, 2 Vim files, 0 colors") {
				t.Fatalf("scan totals = %q", prefix)
			}
		})
	}
}

func TestRuntimepathEmptyAndCancelledScansDoNotLogCompletion(t *testing.T) {
	s := New(nil, nil, io.Discard)
	t.Cleanup(s.stopAnalysis)
	client := &indexingClient{}
	s.client = client
	s.discoverRuntimeRoots(context.Background(), nil, 100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.discoverRuntimeRoots(ctx, []string{t.TempDir()}, 100)
	if len(client.logs) != 0 {
		t.Fatalf("completion logs for empty/cancelled scans = %v", client.logs)
	}
}
