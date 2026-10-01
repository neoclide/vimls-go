package analysis

import (
	"reflect"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestAnalyzeUnusedLegacyBindings(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         []string
	}{
		{
			name: "script variables and shared namespaces",
			source: `let s:unused = 1
let s:used = 2
echo s:used
let global = 1
let g:global = 1
let b:buffer = 1
let w:window = 1
let t:tab = 1
`,
			want: []string{"vimls/unused-variable:s:unused"},
		},
		{
			name: "function locals and arguments",
			source: `function! Global(arg) abort
  let unused = 1
  let l:explicit = 2
  let used = 3
  let l:alias = 4
  let _ = 5
  let l:_ = 6
  let g:shared = 7
  echo l:used alias
endfunction
`,
			want: []string{"vimls/unused-variable:unused", "vimls/unused-variable:l:explicit"},
		},
		{
			name: "destructuring and for bindings",
			source: `function! Global() abort
  let [used, unused] = [1, 2]
  echo used
  for item in [1]
  endfor
  for [key, value] in items({})
    echo key
  endfor
endfunction
`,
			want: []string{"vimls/unused-variable:unused", "vimls/unused-variable:item", "vimls/unused-variable:value"},
		},
		{
			name: "repeated writes share one binding",
			source: `function! Global() abort
  let item = 1
  let l:item = 2
  let used = 3
  let used = 4
  echo used
endfunction
`,
			want: []string{"vimls/unused-variable:item"},
		},
		{
			name: "branches and loop variables retain function scope",
			source: `function! Global(arg) abort
  if a:arg
    let item = 1
  else
    let item = 2
  endif
  for value in [1]
  endfor
  echo l:item value
endfunction
`,
		},
		{
			name: "separate functions and script namespace",
			source: `let s:item = 0
function! First() abort
  let item = 1
endfunction
function! Second() abort
  let item = 2
  echo item s:item
endfunction
`,
			want: []string{"vimls/unused-variable:item"},
		},
		{
			name: "script declarations inside a function and after a reader",
			source: `function! Read() abort
  echo s:item
endfunction
function! Write() abort
  let s:item = 1
endfunction
`,
		},
		{
			name: "local funcrefs and closures",
			source: `function! Global() abort
  let l:Callback = {-> 1}
  call Callback()
  let captured = 2
  let l:Closure = {-> captured}
  call Closure()
  let shadowed = 3
  let l:Shadow = {shadowed -> shadowed}
  call Shadow(4)
endfunction
`,
			want: []string{"vimls/unused-variable:shadowed"},
		},
		{
			name: "lambda callbacks do not hide unrelated names",
			source: `let s:unused = 1
function! Public() abort
  let unused = 2
  call map([], {_, value -> value})
endfunction
`,
			want: []string{"vimls/unused-variable:s:unused", "vimls/unused-variable:unused"},
		},
		{
			name: "execute templates retain unrelated unused bindings",
			source: `let s:unused = 1
function! Public() abort
  let l:unused = 3
  let files = ['file']
  for fname in files
    exe 'edit '.fname
  endfor
  let nr = 1
  exe nr . 'wincmd w'
  execute 'wincmd p'
endfunction
`,
			want: []string{"vimls/unused-variable:s:unused", "vimls/unused-variable:l:unused"},
		},
		{
			name: "string callbacks retain unrelated unused bindings",
			source: `let s:unused = 1
let s:used = 2
function! Public() abort
  let unused = 3
  let used = []
  call map([1], 'extend(used, [v:val + s:used])')
  call filter([1], 'index(used, v:val) >= 0')
  call map([1], "substitute(v:val, '" . expand('~') . "/path', '', '')")
endfunction
`,
			want: []string{"vimls/unused-variable:s:unused", "vimls/unused-variable:unused"},
		},
		{
			name: "literal runtime expressions read only named bindings",
			source: `let s:unused = 1
let s:used = 2
function! s:Unused() abort
endfunction
function! s:Used() abort
endfunction
function! Public() abort
  let unused = 3
  let used = 4
  execute 'echo s:used used | call s:Used()'
  echo execute('echo used')
  echo eval('s:used + used')
  echo substitute('x', 'x', '\=s:used + used', '')
endfunction
`,
			want: []string{"vimls/unused-variable:s:unused", "vimls/unused-function:s:Unused", "vimls/unused-variable:unused"},
		},
		{
			name: "adjacent string concatenation reads local variables",
			source: `function! Public() abort
  let file = 'file'
  let name = 'name'
  echo 'open '.file.' with '.name
endfunction
`,
		},
		{
			name: "existence checks use the binding",
			source: `if !exists('s:loaded')
  let s:loaded = 1
endif
function! Public() abort
  let item = 1
  echo exists('l:item')
endfunction
`,
		},
		{
			name: "script functions only",
			source: `function! s:unused() abort
endfunction
function! s:Other() abort
endfunction
function! Public() abort
endfunction
function! package#Public() abort
endfunction
`,
			want: []string{"vimls/unused-function:s:unused", "vimls/unused-function:s:Other"},
		},
		{
			name: "script variable in mapping text",
			source: `let s:keys = 'x'
nnoremap <unique> <Plug>(keys) <Cmd>echo s:keys<CR>
`,
		},
		{
			name: "mapping names do not match a shorter function name",
			source: `function! s:Run() abort
endfunction
function! s:RunLong() abort
endfunction
nnoremap <unique> <Plug>(run) <Cmd>echo 1 <Bar> call <SID>RunLong()<CR>
`,
			want: []string{"vimls/unused-function:s:Run"},
		},
		{
			name: "direct recursive call alone is unused",
			source: `function! s:Recurse() abort
  call s:Recurse()
endfunction
`,
			want: []string{"vimls/unused-function:s:Recurse"},
		},
		{
			name:   "syntax recovery suppresses unused hints",
			source: "let s:item = 1\nfunction s:Unused() abort\n",
		},
		{
			name:   "semantic errors suppress unused hints",
			source: "let s:item = 1\nreturn 1\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			var got []string
			for _, diagnostic := range result.Diagnostics {
				if strings.HasPrefix(diagnostic.Code, "vimls/unused-") {
					got = append(got, diagnostic.Code+":"+file.Text(diagnostic.Span))
				}
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("unused diagnostics = %v, want %v; syntax = %#v; analysis = %#v", got, test.want, file.Diagnostics, result.Diagnostics)
			}
		})
	}
}

func TestLegacyUnusedRecognizesFunctionUses(t *testing.T) {
	for _, use := range []string{
		"call s:Used()",
		"call <SID>Used()",
		"let g:Callback = function('s:Used')",
		"let g:Callback = funcref('<SID>Used')",
		"let g:Callback = 's:Used'->function()",
		`let g:Callback = function("\x73:Used")`,
		"let g:Callback = function(expand('<SID>') . 'Used')",
		"call call('s:Used', [])",
		"call timer_start(1, function('s:Used'))",
		"let g:options = {'callback': 's:Used'}",
		"for g:Callback in [function('s:Used')]\nendfor",
		`s/a/\=s:Used()/`,
		"nnoremap <unique> <Plug>(used) :call <SID>Used()<CR>",
		"nnoremap <unique> <Plug>(used) <Cmd>call <SID>Used()<CR>",
		"nnoremap <unique> <Plug>(used) <Cmd>echo 1 <Bar> call <SID>Used()<CR>",
		"nnoremap <expr> <unique> <Plug>(used) <SID>Used()",
		"augroup UsedTest\n  autocmd!\n  autocmd User Used call s:Used()\naugroup END",
		"command! Used call s:Used()",
		"command! -nargs=* -complete=custom,s:Used Used echo <q-args>",
		"command! -nargs=* -complete=customlist,<SID>Used Used echo <q-args>",
		"set operatorfunc=s:Used",
		"let &operatorfunc = '<SID>Used'",
		"execute 'set operatorfunc=s:Used'",
		"execute 'nnoremap x :call <SID>Used()<CR>'",
	} {
		t.Run(use, func(t *testing.T) {
			source := "function! s:Used(...) abort\nendfunction\n" + use + "\n"
			file := syntax.Parse(source)
			if len(file.Diagnostics) != 0 {
				t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
			}
			for _, diagnostic := range Analyze(file).Diagnostics {
				if strings.HasPrefix(diagnostic.Code, "vimls/unused-") || strings.HasPrefix(diagnostic.Code, "vim/E") {
					t.Fatalf("unexpected diagnostic = %#v", diagnostic)
				}
			}
		})
	}
}

func TestLegacyUnusedDynamicAccess(t *testing.T) {
	for _, use := range []string{
		"execute a:expression",
		"echo eval(a:expression)",
		"echo execute(a:expression)",
		"execute 'echo ' . a:expression",
		"execute 'call s:' . a:expression . '()'",
		"echo eval('s:' . a:expression)",
		"call map([], 'v:val + ' . a:expression)",
		"execute 'nnoremap x :echo ' . a:expression . '<CR>'",
		"echo s: l:",
		"echo s:{a:expression} l:{a:expression}",
		"call map([], 's:item + item')",
		"call []->map('s:item + item')",
	} {
		t.Run(use, func(t *testing.T) {
			source := "let s:item = 0\nfunction! Dynamic(expression) abort\n  let item = 1\n  " + use + "\nendfunction\n" +
				"function! Other() abort\n  let other = 2\nendfunction\n"
			file := syntax.Parse(source)
			if len(file.Diagnostics) != 0 {
				t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
			}
			result := Analyze(file)
			var got []string
			for _, diagnostic := range result.Diagnostics {
				if strings.HasPrefix(diagnostic.Code, "vimls/unused-") {
					got = append(got, file.Text(diagnostic.Span))
				}
			}
			if !reflect.DeepEqual(got, []string{"other"}) {
				t.Fatalf("unused diagnostics = %v; all = %#v", got, result.Diagnostics)
			}
		})
	}
}
