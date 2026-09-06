package parsecmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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
			if code := Run([]string{path}, nil, &stdout, &stderr); code != 0 {
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
			var piped bytes.Buffer
			if code := Run([]string{"-"}, strings.NewReader(test.source), &piped, &stderr); code != 0 {
				t.Fatalf("stdin code = %d, stderr = %q", code, stderr.String())
			}
			if !bytes.Equal(stdout.Bytes(), piped.Bytes()) {
				t.Fatal("stdin and file syntax trees differ")
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
	if code := Run([]string{path}, nil, failingWriter{}, &diagnostics); code != 1 {
		t.Fatalf("write code = %d", code)
	}
	if code := Run([]string{"does-not-exist.vim"}, nil, &output, &diagnostics); code != 1 {
		t.Fatalf("missing file code = %d", code)
	}
}

func TestRunRequiresOneFile(t *testing.T) {
	var output bytes.Buffer
	for _, args := range [][]string{nil, {"one", "two"}} {
		if code := Run(args, nil, &output, &output); code != 2 {
			t.Fatalf("args %q: code = %d", args, code)
		}
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failure") }

func TestRunRejectsInvalidInputWithoutJSON(t *testing.T) {
	root := t.TempDir()
	largePath := filepath.Join(root, "large.vim")
	if err := os.WriteFile(largePath, bytes.Repeat([]byte{' '}, maxInputBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, path string
		stdin      io.Reader
		want       string
	}{
		{name: "directory", path: root, want: "regular file"},
		{name: "large file", path: largePath, want: "4 MiB"},
		{name: "large stdin", path: "-", stdin: strings.NewReader(strings.Repeat(" ", maxInputBytes+1)), want: "4 MiB"},
		{name: "read failure", path: "-", stdin: failingReader{}, want: "read failure"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run([]string{test.path}, test.stdin, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("code = %d, stdout bytes = %d, stderr = %q", code, stdout.Len(), stderr.String())
			}
		})
	}
}

func TestReadSourceAcceptsEmptyAndExactLimit(t *testing.T) {
	for _, size := range []int{0, maxInputBytes} {
		want := strings.Repeat(" ", size)
		path := filepath.Join(t.TempDir(), "input.vim")
		if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, inputPath := range []string{path, "-"} {
			got, err := readSource(inputPath, strings.NewReader(want))
			if err != nil || string(got) != want {
				t.Fatalf("size %d, path %q: got %d bytes, error %v", size, inputPath, len(got), err)
			}
		}
	}
}

func TestReadSourceStopsAfterLimit(t *testing.T) {
	input := strings.NewReader(strings.Repeat(" ", maxInputBytes+100))
	if _, err := readSource("-", input); err == nil || !strings.Contains(err.Error(), "4 MiB") {
		t.Fatalf("oversized stdin error = %v", err)
	}
	if input.Len() != 99 {
		t.Fatalf("read %d bytes beyond the limit, want 1", 100-input.Len())
	}
}
