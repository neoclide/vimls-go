package vimhelp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractSymbolsVimRuntime scans every help file in the clean Vim runtime
// discovered from the Vim executable available to the test environment.  It
// retains a concrete assertion for the inline *E450* tag that previously
// truncated popup_create()'s documentation.
func TestExtractSymbolsVimRuntime(t *testing.T) {
	runtime := cleanVimRuntime(t)
	paths, err := filepath.Glob(filepath.Join(runtime, "doc", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Skipf("Vim runtime help is unavailable below %s", runtime)
	}

	var entries int
	var popupCreate SymbolDocumentation
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		docs := ExtractSymbols(path, source)
		entries += len(docs)
		for _, doc := range docs {
			if doc.Markdown == "" {
				t.Errorf("%s:%d: %s has empty documentation", path, doc.Line, doc.Tag)
			}
			if doc.Name == "popup_create" && doc.Kind == "global function" {
				popupCreate = doc
			}
		}
	}
	if entries == 0 {
		t.Fatal("no symbols extracted from pinned Vim runtime help")
	}
	if popupCreate.Markdown == "" || !strings.Contains(popupCreate.Markdown, "a list of text lines with text properties") || !strings.Contains(popupCreate.Markdown, "Returns a window-ID") {
		t.Fatalf("popup_create documentation was truncated by inline help tag: %#v", popupCreate)
	}
}

// cleanVimRuntime queries Vim without sourcing user configuration.  This keeps
// the corpus test portable across CI layouts instead of assuming a Homebrew
// installation path.
func cleanVimRuntime(t *testing.T) string {
	t.Helper()
	vim, err := exec.LookPath("vim")
	if err != nil {
		t.Skip("Vim executable is unavailable")
	}
	output := filepath.Join(t.TempDir(), "vimruntime")
	command := exec.Command(vim, "--clean", "-Nu", "NONE", "-n", "-es",
		"-c", fmt.Sprintf("call writefile([$VIMRUNTIME], %s)", vimSingleQuote(output)),
		"-c", "qa!")
	if commandOutput, err := command.CombinedOutput(); err != nil {
		t.Skipf("could not query clean Vim runtime: %v: %s", err, strings.TrimSpace(string(commandOutput)))
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Skipf("could not read clean Vim runtime: %v", err)
	}
	runtime := strings.TrimSpace(string(data))
	if runtime == "" {
		t.Skip("clean Vim reported an empty runtime directory")
	}
	return runtime
}

func vimSingleQuote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", "''") + "'"
}
