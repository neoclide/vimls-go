package parsecmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/chemzqm/vimls-go/internal/syntax"
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
