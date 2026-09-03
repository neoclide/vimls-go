package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// TestConfigDuplicateMappingDiagnostics verifies §5.1 vimls/duplicate-mapping:
// only statically determined later definitions in overlapping modes are
// reported, with related information pointing at the earlier definition.
func TestConfigDuplicateMappingDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		want         int
	}{
		{
			name:   "identical nnoremap defined twice",
			source: "nnoremap <F1> :echo 1<CR>\nnnoremap <F1> :echo 2<CR>\n",
			want:   1,
		},
		{
			name:   "mode-covering map after nmap",
			source: "nmap x :echo 1<CR>\nmap x :echo 2<CR>\n",
			want:   1,
		},
		{
			name:   "disjoint modes are not duplicates",
			source: "nnoremap x :echo 1<CR>\ninoremap x :echo 2<CR>\n",
			want:   0,
		},
		{
			name:   "vmap vs xmap overlap on visual",
			source: "vmap x :echo 1<CR>\nxmap x :echo 2<CR>\n",
			want:   1,
		},
		{
			name:   "global and buffer-local mappings are independent",
			source: "nnoremap x :echo 1<CR>\nnnoremap <buffer> x :echo 2<CR>\n",
			want:   0,
		},
		{
			name:   "mapping and abbreviation are independent categories",
			source: "inoremap teh teh\ninoreabbrev teh the\n",
			want:   0,
		},
		{
			name:   "unmap removes the earlier definition",
			source: "nnoremap x :echo 1<CR>\nnunmap x\nnnoremap x :echo 2<CR>\n",
			want:   0,
		},
		{
			name:   "mapclear removes the earlier definition",
			source: "nnoremap x :echo 1<CR>\nnmapclear\nnnoremap x :echo 2<CR>\n",
			want:   0,
		},
		{
			name:   "conditional unmap invalidates definite state",
			source: "nnoremap x :echo 1<CR>\nif exists('g:maybe')\n  nunmap x\nendif\nnnoremap x :echo 2<CR>\n",
			want:   0,
		},
		{
			name:   "conditional mapclear invalidates definite state",
			source: "nnoremap x :echo 1<CR>\nif exists('g:maybe')\n  nmapclear\nendif\nnnoremap x :echo 2<CR>\n",
			want:   0,
		},
		{
			name:   "execute abbreviated unmap invalidates definite state",
			source: "nnoremap x :echo 1<CR>\nexecute 'nunm x'\nnnoremap x :echo 2<CR>\n",
			want:   0,
		},
		{
			name:   "abclear does not clear mappings",
			source: "inoremap x :echo 1<CR>\ninoreabbrev x x\niabclear\ninoremap x :echo 2<CR>\n",
			want:   1,
		},
		{
			name:   "mapclear does not clear abbreviations",
			source: "inoreabbrev x x\ninoremap x :echo 1<CR>\nimapclear\ninoreabbrev x y\n",
			want:   1,
		},
		{
			name:   "leader spelling duplicates are reported",
			source: "nnoremap <Leader>x :echo 1<CR>\nnnoremap <Leader>x :echo 2<CR>\n",
			want:   1,
		},
		{
			name:   "vim9 mappings use the same static overlap rule",
			source: "vim9script\nnnoremap <F1> <Cmd>echo 1<CR>\nnnoremap <F1> <Cmd>echo 2<CR>\n",
			want:   1,
		},
		{
			name:   "unicode lhs is compared as one literal mapping",
			source: "nnoremap 你 <Cmd>echo 1<CR>\nnnoremap 你 <Cmd>echo 2<CR>\n",
			want:   1,
		},
		{
			name:   "definitions inside a conditional are not proven",
			source: "if has('gui')\n  nnoremap x :echo 1<CR>\nendif\nif has('gui')\n  nnoremap x :echo 2<CR>\nendif\n",
			want:   0,
		},
		{
			name:   "three definitions report the second and third",
			source: "nnoremap x :echo 1<CR>\nnnoremap x :echo 2<CR>\nnnoremap x :echo 3<CR>\n",
			want:   2,
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
				if diagnostic.Code == "vimls/duplicate-mapping" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != test.want {
				t.Fatalf("duplicate-mapping diagnostics = %#v, want %d", got, test.want)
			}
			for _, diagnostic := range got {
				if diagnostic.Span.Start == diagnostic.Span.End || diagnostic.Related.Span.Start == diagnostic.Related.Span.End {
					t.Fatalf("duplicate-mapping diagnostic or related span is empty: %#v", diagnostic)
				}
				if !strings.Contains(diagnostic.Message, "overwrites") {
					t.Fatalf("duplicate-mapping message = %q", diagnostic.Message)
				}
			}
		})
	}
}

func TestConfigDuplicateMappingRelatedDefinitionOverlapsMode(t *testing.T) {
	tests := []struct {
		name, source string
		wantOffset   int
	}{
		{
			name:       "ignore later non-overlapping mode",
			source:     "nmap x :echo 'normal'<CR>\nimap x :echo 'insert'<CR>\nnmap x :echo 'normal again'<CR>\n",
			wantOffset: 5,
		},
		{
			name:       "choose latest overlapping mode",
			source:     "vmap x :echo 'visual'<CR>\nnmap x :echo 'normal'<CR>\nmap x :echo 'all'<CR>\n",
			wantOffset: strings.Index("vmap x :echo 'visual'<CR>\nnmap x", "nmap x") + len("nmap "),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			if len(file.Diagnostics) != 0 {
				t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
			}
			for _, diagnostic := range AnalyzeConfigFile(file).Diagnostics {
				if diagnostic.Code != "vimls/duplicate-mapping" {
					continue
				}
				if got := file.Text(diagnostic.Related.Span); got != "x" || diagnostic.Related.Span.Start != test.wantOffset {
					t.Fatalf("related definition = %q at %#v, want offset %d", got, diagnostic.Related.Span, test.wantOffset)
				}
				return
			}
			t.Fatalf("no duplicate-mapping diagnostic reported for %q", test.source)
		})
	}
}

// TestConfigDuplicateMappingNotInPluginMode checks duplicate-mapping is
// config mode only.
func TestConfigDuplicateMappingNotInPluginMode(t *testing.T) {
	source := "nnoremap x :echo 1<CR>\nnnoremap x :echo 2<CR>\n"
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
	}
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vimls/duplicate-mapping" {
			t.Fatalf("plugin-mode reported duplicate-mapping: %#v", diagnostic)
		}
	}
}
