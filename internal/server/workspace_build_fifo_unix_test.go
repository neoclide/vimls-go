//go:build unix || darwin || linux

package server

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestDidChangeWatchedFilesFIFOReplacesRegularFileClearsStaleFacts(t *testing.T) {
	root := t.TempDir()
	probePath := filepath.Join(root, "probe.fifo")
	if err := syscall.Mkfifo(probePath, 0666); err != nil {
		t.Skipf("mkfifo not supported: %v", err)
	}
	_ = os.Remove(probePath)

	filePath := writeWorkspaceFile(t, root, "fifo.vim", "vim9script\nvar staleSymbol = 1\n")
	instance := initializeWorkspaceServer(t, root)
	instance.workspaceWG.Wait()

	if len(workspaceSymbols(t, instance, "staleSymbol")) != 1 {
		t.Fatal("staleSymbol not initially indexed")
	}

	// Replace regular file with FIFO
	if err := os.Remove(filePath); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filePath, 0666); err != nil {
		t.Fatalf("mkfifo failed: %v", err)
	}

	// Sending Changed event on FIFO must not block and must clear stale facts via full rebuild
	done := make(chan error, 1)
	go func() {
		done <- instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
			Changes: []protocol.FileEvent{
				{URI: uri.File(filePath), Type: protocol.FileChangeTypeChanged},
			},
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DidChangeWatchedFiles blocked on non-regular FIFO file")
	}

	// Wait for full rebuild fallback to finish
	instance.workspaceWG.Wait()

	// Stale symbol must be gone
	if len(workspaceSymbols(t, instance, "staleSymbol")) != 0 {
		t.Fatal("staleSymbol still indexed after file replaced by FIFO")
	}
}

func TestDidChangeWatchedFilesTOCTOUFIFODoesNotBlock(t *testing.T) {
	root := t.TempDir()
	probePath := filepath.Join(root, "probe.fifo")
	if err := syscall.Mkfifo(probePath, 0666); err != nil {
		t.Skipf("mkfifo not supported: %v", err)
	}
	_ = os.Remove(probePath)

	filePath := writeWorkspaceFile(t, root, "toctou_fifo.vim", "vim9script\nvar toctouSymbol = 1\n")
	instance := initializeWorkspaceServer(t, root)
	instance.workspaceWG.Wait()

	if len(workspaceSymbols(t, instance, "toctouSymbol")) != 1 {
		t.Fatal("toctouSymbol not initially indexed")
	}

	// Between os.Stat and reading, replace file with a FIFO
	fifoPath := filePath
	var hookErr atomic.Value
	instance.testHooks.beforeWatchedFileRead = func(p string) {
		if filepath.Base(p) == "toctou_fifo.vim" {
			_ = os.Remove(filePath)
			if err := syscall.Mkfifo(fifoPath, 0666); err != nil {
				hookErr.Store(err)
			}
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- instance.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
			Changes: []protocol.FileEvent{
				{URI: uri.File(filePath), Type: protocol.FileChangeTypeChanged},
			},
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DidChangeWatchedFiles blocked on FIFO during TOCTOU window")
	}

	if val := hookErr.Load(); val != nil {
		t.Fatalf("hook failed to create FIFO: %v", val)
	}

	// Wait for full rebuild fallback to finish
	instance.workspaceWG.Wait()

	// Stale symbol must be gone
	if len(workspaceSymbols(t, instance, "toctouSymbol")) != 0 {
		t.Fatal("toctouSymbol still indexed after file replaced by FIFO in TOCTOU window")
	}
}
