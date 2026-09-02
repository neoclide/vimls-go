package analysis

import (
	"reflect"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestStyleDiagnosticsForCommandsAndMappings(t *testing.T) {
	source := `normal gg
function! s:Run()
endfunction
try
catch /Unknown function/
endtry
echoerr 'failed'
match Error /bad/
nmap <leader>x :call s:Run()<CR>
autocmd BufEnter * if 1 | call s:Run() | endif
command! Run if 1 | call s:Run() | endif
`
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
	}
	result := Analyze(file)
	var got []string
	for _, diagnostic := range result.Diagnostics {
		if strings.HasPrefix(diagnostic.Code, "vimls/") {
			got = append(got, diagnostic.Code)
		}
	}
	want := []string{
		"vimls/normal-without-bang",
		"vimls/function-without-abort",
		"vimls/catch-error-message",
		"vimls/echoerr",
		"vimls/match-command",
		"vimls/recursive-map",
		"vimls/direct-user-keymap",
		"vimls/mapping-without-unique",
		"vimls/mapping-script-local-reference",
		"vimls/autocmd-outside-augroup",
		"vimls/complex-autocmd",
		"vimls/complex-command",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("style diagnostics = %#v, want %#v", got, want)
	}
}

func TestStyleDiagnosticsAvoidDocumentedExceptions(t *testing.T) {
	source := `normal! gg
function! s:Run() abort
endfunction
try
catch /E117/
endtry
nnoremap <unique> <Plug>(run) :call <SID>Run()<CR>
augroup StyleDiagnosticsTest
  autocmd!
  autocmd BufEnter * call s:Run()
augroup END
command! Run call s:Run()
`
	result := Analyze(syntax.Parse(source))
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code != "vimls/unused-variable" {
			t.Fatalf("unexpected style diagnostic %#v", diagnostic)
		}
	}
}

func TestStyleDiagnosticsWithUnusedVim9Variable(t *testing.T) {
	result := Analyze(syntax.Parse("vim9script\ndef Run()\n  var item = 1\n  normal gg\nenddef\n"))
	var got []string
	for _, diagnostic := range result.Diagnostics {
		if strings.HasPrefix(diagnostic.Code, "vimls/") {
			got = append(got, diagnostic.Code)
		}
	}
	want := []string{"vimls/unused-variable", "vimls/normal-without-bang"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("style diagnostics = %#v, want %#v", got, want)
	}
}

func TestNoremapStyleDiagnostics(t *testing.T) {
	result := Analyze(syntax.Parse("nnoremap <leader>x :call s:Run()<CR>\n"))
	var got []string
	for _, diagnostic := range result.Diagnostics {
		if strings.HasPrefix(diagnostic.Code, "vimls/") {
			got = append(got, diagnostic.Code)
		}
	}
	want := []string{
		"vimls/direct-user-keymap",
		"vimls/mapping-without-unique",
		"vimls/mapping-script-local-reference",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("style diagnostics = %#v, want %#v", got, want)
	}
}

func TestAdditionalStyleDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		want         []string
	}{
		{
			name:   "autocommand placement and reset",
			source: "autocmd BufEnter * echo 'outside'\naugroup Test\n  autocmd BufLeave * echo 'inside'\naugroup END\n",
			want:   []string{"vimls/autocmd-outside-augroup", "vimls/autocmd-group-not-cleared"},
		},
		{
			name:   "local global option",
			source: "set tabstop=2\n",
			want:   []string{"vimls/set-vs-setlocal"},
		},
		{
			name:   "global configuration and internal state",
			source: "let g:plugin_timeout = 100\nlet g:plugin_internal_cache = {}\n",
			want:   []string{"vimls/configuration-overwrite", "vimls/global-internal-state"},
		},
		{
			name:   "explicit local scope",
			source: "function! s:Run() abort\n  let item = 1\nendfunction\n",
			want:   []string{"vimls/explicit-local-scope"},
		},
		{
			name:   "implicit string and pattern settings",
			source: "let name = 'Foo'\nif name == 'foo'\nendif\nif name =~ 'foo.*'\nendif\n",
			want:   []string{"vimls/implicit-string-case", "vimls/implicit-pattern-case", "vimls/implicit-regex-magic"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			if len(file.Diagnostics) != 0 {
				t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
			}
			result := Analyze(file)
			var got []string
			for _, diagnostic := range result.Diagnostics {
				if strings.HasPrefix(diagnostic.Code, "vimls/") {
					got = append(got, diagnostic.Code)
				}
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("style diagnostics = %#v, want %#v", got, test.want)
			}
		})
	}
}
