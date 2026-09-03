package parsecmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestRunChoosesDialectFromSource(t *testing.T) {
	for _, test := range []struct {
		name    string
		source  string
		dialect syntax.Dialect
	}{
		{name: "legacy", source: "echo '<'\n", dialect: syntax.Legacy},
		{name: "Vim9", source: "vim9script\necho '<' # comment\n", dialect: syntax.Vim9},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input.vim")
			if err := os.WriteFile(path, []byte(test.source), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := Run([]string{path}, &stdout, &stderr); code != 0 {
				t.Fatalf("code = %d, stderr = %q", code, stderr.String())
			}
			var file syntax.File
			if err := json.Unmarshal(stdout.Bytes(), &file); err != nil {
				t.Fatal(err)
			}
			if file.Dialect != test.dialect || len(file.Commands) == 0 {
				t.Fatalf("file = %#v", file)
			}
			if bytes.Contains(stdout.Bytes(), []byte(`\u003c`)) {
				t.Fatal("syntax text was HTML-escaped")
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failure") }

func TestRunReportsIOFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.vim")
	if err := os.WriteFile(path, []byte("vim9script\necho 'ok'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output, diagnostics bytes.Buffer
	if code := Run([]string{path}, failingWriter{}, &diagnostics); code != 1 {
		t.Fatalf("write code = %d", code)
	}
	if code := Run([]string{"does-not-exist.vim"}, &output, &diagnostics); code != 1 {
		t.Fatalf("missing file code = %d", code)
	}
}

func TestRunRequiresOneFile(t *testing.T) {
	var output bytes.Buffer
	for _, args := range [][]string{nil, {"one", "two"}} {
		if code := Run(args, &output, &output); code != 2 {
			t.Fatalf("args %q: code = %d", args, code)
		}
	}
}
