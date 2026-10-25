package workspace

import (
	"path/filepath"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestAutoloadNameDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, path, source string
		count              int
	}{
		{"plugin valid", "plugin/mylib.vim", "function! mylib#run()\nendfunction\n", 0},
		{"autoload valid", "autoload/foo/bar.vim", "function! foo#bar#run()\nendfunction\n", 0},
		{"wrong basename", "plugin/other.vim", "function! mylib#run()\nendfunction\n", 1},
		{"wrong namespace", "autoload/bar.vim", "function! foo#bar#run()\nendfunction\n", 1},
		{"global prefix", "plugin/other.vim", "function! g:mylib#run()\nendfunction\n", 1},
		{"vim9 uses E1263", "plugin/other.vim", "vim9script\nfunction! mylib#run()\nendfunction\n", 0},
		{"conditional", "plugin/other.vim", "if 0\nfunction! mylib#run()\nendfunction\nendif\n", 0},
		{"dynamic", "plugin/other.vim", "function! mylib#{name}()\nendfunction\n", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			got := AutoloadNameDiagnostics(filepath.Join(t.TempDir(), filepath.FromSlash(test.path)), file)
			if len(got) != test.count {
				t.Fatalf("diagnostics=%#v; parse=%#v", got, file.Diagnostics)
			}
			if len(got) > 0 && (got[0].Code != "vim/E746" || file.Text(got[0].Span) == "") {
				t.Fatalf("diagnostics=%#v", got)
			}
		})
	}
}
