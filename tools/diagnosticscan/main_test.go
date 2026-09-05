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

func TestRunIgnoresXPTemplateFiles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"ftplugin/scala.xpt.vim", "nested/other.XPT.VIM", "plugin/normal.vim", "directory.xpt.vim/normal.vim"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("call len()\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-runtimepath", root + "," + root}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	if got := stdout.String(); strings.Count(got, "vim/E119") != 2 || strings.Contains(got, "scala.xpt.vim") || strings.Contains(got, "other.XPT.VIM") {
		t.Fatalf("wrong files scanned: %s", got)
	}
	if !strings.Contains(stderr.String(), "scanned 1 roots, 2 files, found 2 errors") {
		t.Fatalf("stderr = %s", &stderr)
	}
}

func TestRunRejectsRemovedExcludeOption(t *testing.T) {
	output := filepath.Join(t.TempDir(), "existing.txt")
	if err := os.WriteFile(output, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-runtimepath", t.TempDir(), "-output", output, "-exclude", "*.xpt.vim"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit = %d, stderr = %s", code, &stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "keep" {
		t.Fatalf("output changed: %q, %v", data, err)
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
	for _, path := range []string{"plugin/foo.vim", ".vimrc", "vimrc", "template.xpt.vim/normal.vim", "xpt.vim", "test.xpt.vim.extra.vim"} {
		if !isVimSourcePath(path) {
			t.Fatalf("isVimSourcePath(%q) = false", path)
		}
	}
	for _, path := range []string{"autoload/vimtex/complete/acro", "doc/tags", "LICENSE", "scala.xpt.vim", "nested/scala.XPT.VIM", ".xpt.vim"} {
		if isVimSourcePath(path) {
			t.Fatalf("isVimSourcePath(%q) = true", path)
		}
	}
}
