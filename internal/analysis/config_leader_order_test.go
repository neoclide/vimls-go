package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// TestConfigMapleaderOrderDiagnostics verifies §5.2: a mapping that uses
// <Leader>/<LocalLeader> before the leader variable is statically assigned is
// reported; ordered, conditional, dynamic, and out-of-context cases are not.
func TestConfigMapleaderOrderDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		want         int
	}{
		{
			name:   "leader mapping before static assignment",
			source: "map <Leader>a :echo 1<CR>\nlet g:mapleader = ','\n",
			want:   1,
		},
		{
			name:   "local leader mapping before static assignment",
			source: "map <LocalLeader>b :echo 2<CR>\nlet g:maplocalleader = '\\\\'\n",
			want:   1,
		},
		{
			name:   "assignment before mapping is fine",
			source: "let g:mapleader = ','\nmap <Leader>a :echo 1<CR>\n",
			want:   0,
		},
		{
			name:   "unprefixed legacy assignment counts",
			source: "map <Leader>a :echo 1<CR>\nlet mapleader = ','\n",
			want:   1,
		},
		{
			name:   "dynamic assignment is unknown",
			source: "map <Leader>a :echo 1<CR>\nlet g:mapleader = get(g:, 'ml', ',')\n",
			want:   0,
		},
		{
			name:   "mapping inside function is unknown order",
			source: "function! F()\n  map <Leader>a :echo 1<CR>\nendfunction\nlet g:mapleader = ','\n",
			want:   0,
		},
		{
			name:   "assignment inside conditional is unknown",
			source: "map <Leader>a :echo 1<CR>\nif has('gui')\n  let g:mapleader = ','\nendif\n",
			want:   0,
		},
		{
			name:   "two mappings before assignment each reported",
			source: "map <Leader>a :echo 1<CR>\nnmap <Leader>b :echo 2<CR>\nlet g:mapleader = ','\n",
			want:   2,
		},
		{
			name:   "assignment between mappings resets state",
			source: "map <Leader>a :echo 1<CR>\nlet g:mapleader = ','\nmap <Leader>b :echo 2<CR>\n",
			want:   1,
		},
		{
			name:   "noremap and insert mappings also expand",
			source: "nnoremap <Leader>a :echo 1<CR>\nlet g:mapleader = ','\n",
			want:   1,
		},
		{
			name:   "no later assignment is not reported",
			source: "map <Leader>a :echo 1<CR>\n",
			want:   0,
		},
		{
			name:   "mapping without leader is not reported",
			source: "map x :echo 1<CR>\nlet g:mapleader = ','\n",
			want:   0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			if len(file.Diagnostics) != 0 {
				t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
			}
			var got []syntax.Diagnostic
			for _, diagnostic := range AnalyzeConfigFile(file).Diagnostics {
				if diagnostic.Code == "vimls/config-mapleader-order" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != test.want {
				t.Fatalf("config-mapleader-order diagnostics = %#v, want %d", got, test.want)
			}
			for _, diagnostic := range got {
				if diagnostic.Span.Start == diagnostic.Span.End {
					t.Fatalf("diagnostic span is empty: %#v", diagnostic)
				}
				if !strings.Contains(diagnostic.Message, "leader") {
					t.Fatalf("diagnostic message does not name the leader: %#v", diagnostic)
				}
			}
		})
	}
}

// TestConfigMapleaderOrderNotInPluginMode checks the rule is configuration
// mode only: the same source analyzed without the config role stays quiet.
func TestConfigMapleaderOrderNotInPluginMode(t *testing.T) {
	source := "map <Leader>a :echo 1<CR>\nlet g:mapleader = ','\n"
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
	}
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vimls/config-mapleader-order" {
			t.Fatalf("plugin-mode reported config-mapleader-order: %#v", diagnostic)
		}
	}
}
