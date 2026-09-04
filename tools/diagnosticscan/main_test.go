package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestRunReportsOnlyErrorsAndDeduplicatesRuntimepath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "plugin", "scan.vim")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("call len()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-runtimepath", root + "," + root}, &stdout, &stderr); code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, path+":1:6: vim/E119") || strings.Count(got, path) != 1 {
		t.Fatalf("stdout = %q", got)
	}
	if got := stderr.String(); !strings.Contains(got, "scanned 1 roots, 1 files, found 1 errors") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestIsErrorDiagnostic(t *testing.T) {
	if !isErrorDiagnostic(syntax.Diagnostic{Code: "vim/E695"}) {
		t.Fatal("E695 should be an error")
	}
	if isErrorDiagnostic(syntax.Diagnostic{Code: "vim/E117"}) {
		t.Fatal("E117 should use the server warning severity")
	}
	severity := syntax.DiagnosticHint
	if isErrorDiagnostic(syntax.Diagnostic{Code: "vim/E695", Severity: &severity}) {
		t.Fatal("occurrence severity override was ignored")
	}
}

func TestIsVimSourcePath(t *testing.T) {
	for _, path := range []string{"plugin/foo.vim", ".vimrc", "vimrc"} {
		if !isVimSourcePath(path) {
			t.Fatalf("isVimSourcePath(%q) = false", path)
		}
	}
	for _, path := range []string{"autoload/vimtex/complete/acro", "doc/tags", "LICENSE"} {
		if isVimSourcePath(path) {
			t.Fatalf("isVimSourcePath(%q) = true", path)
		}
	}
}
