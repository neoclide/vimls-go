package server

import (
	"context"
	"fmt"
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
			if kind == "runtimepath" {
				// Exceed the worker batch size to catch per-batch timing logs.
				for len(roots) <= runtimepathScanWorkers {
					roots = append(roots, t.TempDir())
				}
			}
			for _, root := range roots {
				writeWorkspaceFile(t, root, "plugin/test.vim", "let g:example = 1\n")
				writeWorkspaceFile(t, root, "doc/test.txt", "*Example*\nExample help.\n")
			}
			if kind == "runtimepath" {
				if err := s.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: roots}); err != nil {
					t.Fatal(err)
				}
				s.runtimeHelpWG.Wait()
			} else {
				s.setRuntimePaths(roots)
				s.runtimeHelpWG.Wait()
			}
			client.mu.Lock()
			defer client.mu.Unlock()
			var logs []string
			for _, message := range client.logs {
				if strings.HasPrefix(message, "vimls: scanned "+kind) {
					logs = append(logs, message)
				}
			}
			wantLogs := 1
			if kind == "runtimepath" {
				wantLogs += len(roots)
			}
			if len(logs) != wantLogs {
				t.Fatalf("batch logs = %v, want %d", logs, wantLogs)
			}
			if kind == "runtimepath" {
				for _, root := range roots {
					want := fmt.Sprintf("vimls: scanned runtimepath %s: 1 Vim files, 0 colors", mustWorkspaceCanonicalPath(t, root))
					count := 0
					for _, message := range logs[:len(roots)] {
						if message == want {
							count++
						}
					}
					if count != 1 {
						t.Fatalf("want one directory log without elapsed time %q, got %v", want, logs)
					}
				}
			}
			message := logs[len(logs)-1]
			prefix, duration, ok := strings.Cut(message, "; total elapsed ")
			if !ok || !strings.HasPrefix(prefix, "vimls: scanned "+kind) {
				t.Fatalf("scan log = %q", message)
			}
			if elapsed, err := time.ParseDuration(duration); err != nil || elapsed < 0 {
				t.Fatalf("total elapsed = %q, error = %v", duration, err)
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

func TestRuntimepathScanCompletesWhileRuntimeHelpIsBlocked(t *testing.T) {
	s := New(nil, nil, io.Discard)
	t.Cleanup(s.stopAnalysis)
	client := &indexingClient{}
	s.client = client
	helpRoot := mustWorkspaceCanonicalPath(t, t.TempDir())
	sourceRoot := mustWorkspaceCanonicalPath(t, t.TempDir())
	writeWorkspaceFile(t, helpRoot, "doc/test.txt", "*Example*\nExample help.\n")
	path := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, sourceRoot, "plugin/test.vim", "let g:example = 1\n"))
	started, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(release) })
	s.testHooks.beforeRuntimeHelpRead = func(ctx context.Context, _ string) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	s.setRuntimePaths([]string{helpRoot})
	waitForServerRace(t, started, "blocked runtime help read")
	done := make(chan error, 1)
	go func() {
		done <- s.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{helpRoot, sourceRoot}})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runtimepath scan waited for runtime help")
	}
	if _, ok := s.workspaceIndex.Source(path); !ok {
		t.Fatal("runtimepath total reported before source installation")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	totals := 0
	for _, message := range client.logs {
		if strings.Contains(message, "scanned runtimepath; total elapsed ") {
			totals++
		}
		if strings.Contains(message, "scanned runtime help") {
			t.Fatal("blocked help scan reported completion")
		}
	}
	if totals != 1 {
		t.Fatalf("runtimepath completion logs = %v", client.logs)
	}
}
