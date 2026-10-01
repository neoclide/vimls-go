package analysis

import (
	"reflect"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestAnalyzeUnusedVim9Functions(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         []string
		invalid      bool
	}{
		{
			name: "private script function",
			source: `def Unused()
enddef
export def Public()
enddef
def g:Global()
enddef
`,
			want: []string{"Unused"},
		},
		{
			name: "nested function",
			source: `export def Public()
  def Unused()
  enddef
  def Used()
  enddef
  Used()
enddef
`,
			want: []string{"Unused"},
		},
		{
			name: "nested function passed as a value",
			source: `export def Public(): func
  def Used()
  enddef
  def Unused()
  enddef
  return function(Used)
enddef
`,
			want: []string{"Unused"},
		},
		{
			name: "nested names retain declaration identity",
			source: `export def First()
  def Same()
  enddef
  Same()
enddef
export def Second()
  def Same()
  enddef
enddef
`,
			want: []string{"Same"},
		},
		{
			name: "class and interface methods are excluded",
			source: `class Box
  def Method()
  enddef
  static def StaticMethod()
  enddef
endclass
interface Service
  def Method()
endinterface
`,
		},
		{
			name: "forward function reference",
			source: `def Caller()
  Used()
enddef
def Used()
enddef
Caller()
`,
		},
		{
			name: "recursive call alone does not use a function",
			source: `def Unused()
  Unused()
enddef
`,
			want: []string{"Unused"},
		},
		{
			name:    "incomplete function suppresses hints",
			source:  "def Incomplete()\n",
			invalid: true,
		},
		{
			name:    "semantic error suppresses hints",
			source:  "def Unused()\nenddef\necho missing\n",
			invalid: true,
		},
		{
			name:   "dynamic command suppresses hints",
			source: "def Used()\nenddef\nexecute 'Used()'\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse("vim9script\n" + test.source)
			result := Analyze(file)
			invalid := len(file.Diagnostics) != 0
			var got []string
			for _, diagnostic := range result.Diagnostics {
				invalid = invalid || strings.HasPrefix(diagnostic.Code, "vim/E")
				if diagnostic.Code == "vimls/unused-function" {
					got = append(got, file.Text(diagnostic.Span))
				}
			}
			if invalid != test.invalid {
				t.Fatalf("invalid fixture = %v, want %v; syntax = %#v; analysis = %#v", invalid, test.invalid, file.Diagnostics, result.Diagnostics)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("unused functions = %v, want %v; syntax = %#v; analysis = %#v", got, test.want, file.Diagnostics, result.Diagnostics)
			}
		})
	}
}

func TestVim9UnusedFunctionReferences(t *testing.T) {
	for _, use := range []string{
		"echo Used()",
		"var Callback = Used\necho Callback",
		"var Callback = function(Used)\necho Callback",
		"var Callback = funcref(Used)\necho Callback",
		"var Callback = function('Used')\necho Callback",
		"var Callback = funcref('s:Used')\necho Callback",
		"echo call('Used', [])",
		"echo exists('*Used')",
		"nnoremap <unique> <Plug>(used) <Cmd>Used()<CR>",
		"nnoremap <unique> <Plug>(used) <Cmd>echo 1 <Bar> Used()<CR>",
		"nnoremap <unique> <Plug>(used) <Cmd>call <SID>Used()<CR>",
		"nnoremap <expr> <unique> <Plug>(used) Used()",
		"augroup UsedTest\n  autocmd!\n  autocmd User Used call Used()\naugroup END",
		"command! UsedCommand call Used()",
		"set operatorfunc=Used",
		"&operatorfunc = 'Used'",
	} {
		t.Run(use, func(t *testing.T) {
			source := "vim9script\ndef Used(): string\n  return 'x'\nenddef\ndef Unused()\nenddef\n" + use + "\n"
			file := syntax.Parse(source)
			if len(file.Diagnostics) != 0 {
				t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
			}
			result := Analyze(file)
			var got []string
			for _, diagnostic := range result.Diagnostics {
				if strings.HasPrefix(diagnostic.Code, "vim/E") {
					t.Fatalf("unexpected diagnostic = %#v", diagnostic)
				}
				if diagnostic.Code == "vimls/unused-function" {
					got = append(got, file.Text(diagnostic.Span))
				}
			}
			if !reflect.DeepEqual(got, []string{"Unused"}) {
				t.Fatalf("unused functions = %v, want [Unused]; all = %#v", got, result.Diagnostics)
			}
		})
	}
}
