package parsecmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestRunUsesIndependentDialectParser(t *testing.T) {
	for _, dialect := range []syntax.Dialect{syntax.Legacy, syntax.Vim9} {
		var stdout, stderr bytes.Buffer
		if code := Run(nil, bytes.NewBufferString("echo '<' # comment\n"), &stdout, &stderr, dialect); code != 0 {
			t.Fatalf("dialect %s: code = %d, stderr = %q", dialect, code, stderr.String())
		}
		var file syntax.File
		if err := json.Unmarshal(stdout.Bytes(), &file); err != nil {
			t.Fatal(err)
		}
		if file.Dialect != dialect || len(file.Commands) != 1 {
			t.Fatalf("dialect %s: file = %#v", dialect, file)
		}
		if bytes.Contains(stdout.Bytes(), []byte(`\u003c`)) {
			t.Fatalf("dialect %s: syntax text was HTML-escaped", dialect)
		}
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failure") }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failure") }

func TestRunReadsFilesAndReportsIOFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.vim")
	if err := os.WriteFile(path, []byte("vim9script\necho 'ok'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output, diagnostics bytes.Buffer
	if code := Run([]string{path}, nil, &output, &diagnostics, syntax.Vim9); code != 0 {
		t.Fatalf("file code = %d: %s", code, diagnostics.String())
	}
	if code := Run(nil, failingReader{}, io.Discard, &diagnostics, syntax.Legacy); code != 1 {
		t.Fatalf("read code = %d", code)
	}
	if code := Run(nil, bytes.NewBufferString("echo 1"), failingWriter{}, &diagnostics, syntax.Legacy); code != 1 {
		t.Fatalf("write code = %d", code)
	}
}

func TestRunRejectsExtraArgumentsAndMissingFile(t *testing.T) {
	var output bytes.Buffer
	if code := Run([]string{"one", "two"}, bytes.NewReader(nil), &output, &output, syntax.Legacy); code != 2 {
		t.Fatalf("extra argument code = %d", code)
	}
	output.Reset()
	if code := Run([]string{"does-not-exist.vim"}, bytes.NewReader(nil), &output, &output, syntax.Vim9); code != 1 {
		t.Fatalf("missing file code = %d", code)
	}
}
