package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestVariableTypeChange(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         int
	}{
		{"string to number", "let g:local = 'abc'\nlet g:local = 55\n", 1},
		{"same type", "let g:local = 'abc'\nlet g:local = 'def'\n", 0},
		{"latest assignment", "let g:local = 'abc'\nlet g:local = 55\nlet g:local = 66\nlet g:local = []\n", 2},
		{"unknown before", "let g:local = Missing()\nlet g:local = 55\n", 0},
		{"unknown after", "let g:local = 'abc'\nlet g:local = Missing()\n", 0},
		{"unknown clears history", "let g:local = 'abc'\nlet g:local = g:unknown\nlet g:local = 55\n", 0},
		{"known resumes", "let g:local = g:unknown\nlet g:local = 55\nlet g:local = 'abc'\n", 1},
		{"known builtin initializer", "let g:local = expand('~')\nlet g:local = 55\n", 1},
		{"list elements", "let g:local = ['abc']\nlet g:local = [55]\n", 0},
		{"unknown list elements", "let g:local = [g:unknown]\nlet g:local = 55\n", 1},
		{"dict elements", "let g:local = {'x': 'abc'}\nlet g:local = {'x': 55}\n", 0},
		{"unrelated assignment", "let g:local = 'abc'\nlet g:other = 1\nlet g:local = 55\n", 1},
		{"global alias", "let local = 'abc'\nlet g:local = 55\n", 1},
		{"script scope differs", "let s:local = 'abc'\nlet g:local = 55\n", 0},
		{"function local", "function! F() abort\nlet local = 'abc'\nlet l:local = 55\nendfunction\n", 1},
		{"separate functions", "function! F() abort\nlet g:local = 'abc'\nendfunction\nfunction! G() abort\nlet g:local = 55\nendfunction\n", 0},
		{"branches", "if g:flag\nlet g:local = 'abc'\nelse\nlet g:local = 55\nendif\n", 0},
		{"branch exit", "if g:flag\nlet g:local = 'abc'\nendif\nlet g:local = 55\n", 0},
		{"inside branch", "if g:flag\nlet g:local = 'abc'\nlet g:local = 55\nendif\n", 1},
		{"unlet", "let g:local = 'abc'\nunlet g:local\nlet g:local = 55\n", 0},
		{"call barrier", "let g:local = 'abc'\ncall Change()\nlet g:local = 55\n", 0},
		{"rhs call barrier", "let g:local = 'abc'\nlet g:other = Change()\nlet g:local = 55\n", 0},
		{"stale reference", "let g:other = 'abc'\nlet g:other = g:unknown\nlet g:local = g:other\nlet g:local = 55\n", 0},
		{"current reference", "let g:other = 'abc'\nlet g:other = 55\nlet g:local = g:other\nlet g:local = 66\n", 1},
		{"stale call argument", "let g:other = 'abc'\nlet g:other = g:unknown\nlet g:local = copy(g:other)\nlet g:local = 55\n", 0},
		{"stale element type", "let g:other = ['abc']\nlet g:other = [55]\nlet g:local = g:other[0]\nlet g:local = 66\n", 0},
		{"stale nested element", "let g:other = 'abc'\nlet g:other = 55\nlet g:items = [g:other]\nlet g:local = g:items[0]\nlet g:local = 66\n", 1},
		{"vim9 error only", "vim9script\nvar local = 'abc'\nlocal = 55\n", 0},
		{"vim9 any", "vim9script\nvar local: any = 'abc'\nlocal = 55\n", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			var got []syntax.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vimls/variable-type-change" {
					got = append(got, diagnostic)
					if !strings.Contains(diagnostic.Message, "changes type from") || diagnostic.Related.Span.Start >= diagnostic.Span.Start {
						t.Fatalf("invalid diagnostic: %#v", diagnostic)
					}
					if test.name == "string to number" && (diagnostic.Message != "Variable g:local changes type from string to number" || diagnostic.Span.Start != strings.LastIndex(test.source, "g:local") || result.File.Text(diagnostic.Span) != "g:local") {
						t.Fatalf("wrong warning location/message: %#v", diagnostic)
					}
				}
			}
			if len(got) != test.want {
				t.Fatalf("got %d warnings, want %d; diagnostics = %#v", len(got), test.want, result.Diagnostics)
			}
		})
	}
}
