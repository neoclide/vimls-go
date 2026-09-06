//go:build unix || darwin || linux

package parsecmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunRejectsSpecialFiles(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "input.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{fifo, os.DevNull} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			type result struct {
				code   int
				output string
				error  string
			}
			done := make(chan result, 1)
			go func() {
				var stdout, stderr bytes.Buffer
				code := Run([]string{path}, nil, &stdout, &stderr)
				done <- result{code, stdout.String(), stderr.String()}
			}()
			select {
			case got := <-done:
				if got.code != 1 || got.output != "" || !strings.Contains(got.error, "regular file") {
					t.Fatalf("result = %+v", got)
				}
			case <-time.After(2 * time.Second):
				// Unblock an accidental FIFO open before reporting the regression.
				if file, err := os.OpenFile(fifo, os.O_RDWR|syscall.O_NONBLOCK, 0); err == nil {
					file.Close()
				}
				t.Fatal("special file input blocked")
			}
		})
	}
}

func TestOpenInputFileDoesNotBlockOnFIFO(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "input.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	// Exercise the open after a preflight Stat could have seen a regular file.
	done := make(chan error, 1)
	go func() {
		file, err := openInputFile(fifo)
		if err == nil {
			file.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		if file, err := os.OpenFile(fifo, os.O_RDWR|syscall.O_NONBLOCK, 0); err == nil {
			file.Close()
		}
		t.Fatal("FIFO open blocked")
	}
}
