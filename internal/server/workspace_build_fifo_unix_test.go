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

func TestWorkspaceReadEntrypointsRejectFIFO(t *testing.T) {
	for _, entry := range []string{"restore", "diagnostics", "runtimepath-source", "runtimepath-root"} {
		t.Run(entry, func(t *testing.T) {
			root := t.TempDir()
			path := writeWorkspaceFile(t, root, "current.vim", "let value = 1\n")
			s := initializeWorkspaceServer(t, root)
			documentURI := uri.File(path)
			if entry == "restore" {
				s.documents.Open(documentURI.String(), 1, "let overlay = 2\n")
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
			// Also release a blocking regression before server cleanup joins work.
			t.Cleanup(func() {
				if writer, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0); err == nil {
					_ = writer.Close()
				}
			})
			runtimeRoot := t.TempDir()
			if entry == "runtimepath-source" {
				// Simulate a discovered regular file replaced before the read.
				s.testHooks.discoverWorkspaceFiles = func(context.Context, string, int) ([]string, bool, error) {
					return []string{path}, false, nil
				}
				s.workspaceIndex.Remove(mustWorkspaceCanonicalPath(t, path))
			}
			done := make(chan error, 1)
			go func() {
				switch entry {
				case "restore":
					done <- s.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
				case "diagnostics":
					_, err := s.DiagnosticWorkspace(context.Background(), &protocol.WorkspaceDiagnosticParams{})
					done <- err
				case "runtimepath-source":
					done <- s.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{runtimeRoot}})
				case "runtimepath-root":
					done <- s.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{path}})
				}
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("filesystem entrypoint blocked on FIFO")
			}
		})
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
