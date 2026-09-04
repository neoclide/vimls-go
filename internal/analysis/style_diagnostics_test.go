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
try
catch
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

func TestConfigFileAbbreviationsDoNotReportRecursiveMap(t *testing.T) {
	source := "iabbrev cosnt const\ncabbrev nao noa\n"
	for _, diagnostic := range AnalyzeConfigFile(syntax.Parse(source)).Diagnostics {
		if diagnostic.Code == "vimls/recursive-map" {
			t.Fatalf("config abbreviation diagnostic = %#v", diagnostic)
		}
	}

	pluginCount := 0
	for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
		if diagnostic.Code == "vimls/recursive-map" {
			pluginCount++
		}
	}
	if pluginCount != 2 {
		t.Fatalf("plugin recursive-map count = %d, want 2", pluginCount)
	}

	configMap := AnalyzeConfigFile(syntax.Parse("map <leader>x :echo 'x'<CR>\n"))
	for _, diagnostic := range configMap.Diagnostics {
		if diagnostic.Code == "vimls/recursive-map" {
			return
		}
	}
	t.Fatalf("config recursive map diagnostic missing: %#v", configMap.Diagnostics)
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

func TestConfigurationOverwriteSkipsSelfPreservingGet(t *testing.T) {
	tests := []struct {
		name, source string
		want         bool
	}{
		{
			name:   "dictionary default",
			source: "let g:coc_user_config = get(g:, 'coc_user_config', {})\n",
		},
		{
			name:   "list default without matching heuristic name",
			source: "let g:coc_global_extensions = get(g:, 'coc_global_extensions', [])\n",
		},
		{
			name:   "no default",
			source: "let g:plugin_config = get(g:, 'plugin_config')\n",
		},
		{
			name:   "different key",
			source: "let g:plugin_config = get(g:, 'other_config', {})\n",
			want:   true,
		},
		{
			name:   "dynamic key",
			source: "let key = 'plugin_config'\nlet g:plugin_config = get(g:, key, {})\n",
			want:   true,
		},
		{
			name:   "different dictionary",
			source: "let s:values = {}\nlet g:plugin_config = get(s:values, 'plugin_config', {})\n",
			want:   true,
		},
		{
			name:   "direct assignment",
			source: "let g:plugin_config = {}\n",
			want:   true,
		},
		{
			name:   "compound assignment",
			source: "let g:plugin_config += get(g:, 'plugin_config', {})\n",
			want:   true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			got := false
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vimls/configuration-overwrite" {
					got = true
				}
			}
			if got != test.want {
				t.Fatalf("configuration-overwrite = %t, want %t; diagnostics = %#v", got, test.want, result.Diagnostics)
			}
		})
	}
}

func TestPluginGlobalAssignmentsReportDebuggingHint(t *testing.T) {
	source := "let g:x = 33\nfunction! coc#expandable() abort\n  let g:y = 44\n  if exists('g:z')\n    let g:z = 55\n  endif\n  try\n    let g:w = 66\n  endtry\nendfunction\n"
	file := syntax.Parse(source)
	result := Analyze(file)
	var got []string
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vimls/global-internal-state" {
			got = append(got, file.Text(diagnostic.Span))
		}
	}
	want := []string{"g:x", "g:y", "g:z", "g:w"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("global-internal-state spans = %#v, want %#v; diagnostics = %#v", got, want, result.Diagnostics)
	}
}

func TestPluginGlobalAssignmentHintExclusions(t *testing.T) {
	source := "let g:coc_user_config = get(g:, 'coc_user_config', {})\nlet g:coc_global_extensions = get(g:, 'coc_global_extensions', [])\nlet g:loaded_example = 1\n"
	if got := collectVimlsCodes(Analyze(syntax.Parse(source))); len(got) != 0 {
		t.Fatalf("plugin diagnostics = %#v, want none", got)
	}
	configSource := "let g:x = 33\nfunction! F() abort\n  let g:y = 44\nendfunction\n"
	if got := collectVimlsCodes(AnalyzeConfigFile(syntax.Parse(configSource))); len(got) != 0 {
		t.Fatalf("config diagnostics = %#v, want none", got)
	}
}
