package integration_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStandaloneLicenses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, vimlsBinary, "--licenses")
	// No checkout, external notice files or LSP input is needed at runtime.
	cmd.Dir = t.TempDir()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("--licenses: %v; stderr: %s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	for _, name := range []string{"MIT.txt", "VIM.txt", "VIM-DOC.txt", "NEOVIM.txt"} {
		want, err := os.ReadFile(filepath.Join("..", "..", "LICENSES", name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(output, want) || !strings.Contains(string(output), "=== "+name+" ===") {
			t.Errorf("standalone binary does not include complete %s", name)
		}
	}
}
