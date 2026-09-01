package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteArchiveIsDeterministic(t *testing.T) {
	stamp := time.Unix(1700000000, 0).UTC()
	for _, zipped := range []bool{false, true} {
		first := filepath.Join(t.TempDir(), "first")
		second := filepath.Join(t.TempDir(), "second")
		if err := writeArchive(first, "vimls-go/vimls", []byte("binary"), stamp, zipped); err != nil {
			t.Fatal(err)
		}
		if err := writeArchive(second, "vimls-go/vimls", []byte("binary"), stamp, zipped); err != nil {
			t.Fatal(err)
		}
		left, _ := os.ReadFile(first)
		right, _ := os.ReadFile(second)
		if !bytes.Equal(left, right) {
			t.Fatal("archives differ")
		}
	}
}
