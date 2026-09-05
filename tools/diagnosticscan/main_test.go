package main

import (
	"bytes"
	"fmt"
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

func TestRunExcludeBasenamePatterns(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"ftplugin/scala.xpt.vim", "nested/other.xpt.vim", "plugin/normal.vim", "plugin/skip.vim"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("call len()\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name                     string
		patterns                 []string
		wantErrors, wantExcluded int
	}{
		{"default", nil, 4, 0},
		{"templates", []string{"*.xpt.vim"}, 2, 2},
		{"repeat", []string{"*.xpt.vim", "skip.vim"}, 1, 3},
		{"overlap", []string{"*.xpt.vim", "scala.xpt.vim"}, 2, 2},
		{"all", []string{"*.vim"}, 0, 4},
		{"unmatched", []string{"absent.vim"}, 4, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := []string{"-runtimepath", root + "," + root}
			for _, pattern := range test.patterns {
				args = append(args, "-exclude", pattern)
			}
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != 0 {
				t.Fatalf("exit %d: %s", code, &stderr)
			}
			if got := strings.Count(stdout.String(), "vim/E119"); got != test.wantErrors {
				t.Fatalf("errors = %d, want %d: %s", got, test.wantErrors, &stdout)
			}
			if len(test.patterns) > 0 && !strings.Contains(stderr.String(), fmt.Sprintf("excluded %d files", test.wantExcluded)) {
				t.Fatalf("exclusion count: %s", &stderr)
			}
			if test.name == "templates" && (!strings.Contains(stdout.String(), "normal.vim") || strings.Contains(stdout.String(), ".xpt.vim")) {
				t.Fatalf("wrong files excluded: %s", &stdout)
			}
		})
	}
}

func TestRunRejectsInvalidExcludeBeforeOpeningOutput(t *testing.T) {
	output := filepath.Join(t.TempDir(), "existing.txt")
	for _, pattern := range []string{"[", ""} {
		if err := os.WriteFile(output, []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := run([]string{"-runtimepath", t.TempDir(), "-output", output, "-exclude", pattern}, &stdout, &stderr); code != 2 {
			t.Fatalf("exit = %d, stderr = %s", code, &stderr)
		}
		data, err := os.ReadFile(output)
		if err != nil || string(data) != "keep" {
			t.Fatalf("output changed: %q, %v", data, err)
		}
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
