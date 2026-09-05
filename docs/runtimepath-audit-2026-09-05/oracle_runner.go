// Run only curated, side-effect-free reproductions; never source plugin files.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type testCase struct {
	Name string
	IDs  []int
	Body string
	Want string
	Path string
	Line int
}
type result struct {
	Name     string          `json:"name"`
	IDs      []int           `json:"diagnostic_ids"`
	Expected string          `json:"expected_exception"`
	Exit     int             `json:"exit_code"`
	Stdout   string          `json:"stdout"`
	Stderr   string          `json:"stderr"`
	Report   json.RawMessage `json:"vim_report"`
	Verified bool            `json:"verified"`
}

func main() {
	output := flag.String("output", "docs/runtimepath-audit-2026-09-05/oracle", "evidence directory")
	vim := flag.String("vim", "/usr/local/bin/vim", "pinned Vim executable")
	flag.Parse()
	must(os.MkdirAll(filepath.Join(*output, "cases"), 0o755))
	base, err := filepath.Abs(*output)
	must(err)
	cases := []testCase{
		{Name: "initializer-lambda-shadow", IDs: []int{1, 2}, Body: `vim9script
def Test()
  var cmds = ['ok']
  const cmd = join(map(cmds, (_, cmd: string) => toupper(cmd)), "\n")
  assert_equal('OK', cmd)
enddef
Test()
`},
		{Name: "command-args-expansion", IDs: []int{3, 4, 5, 6, 7, 8, 9, 10, 28}, Body: `command -nargs=* CompilerSet setlocal <args>
CompilerSet shiftwidth=3
call assert_equal(3, &l:shiftwidth)
`},
		{Name: "function-variable-separate-namespaces", IDs: []int{11, 12, 13, 14}, Body: `function! s:fnameescape(file) abort
  return a:file
endfunction
if has('win32')
  let s:fnameescape = 'abc'
else
  let s:fnameescape = 'abc'
endif
function! s:Use() abort
  return '[' . s:fnameescape . ']'
endfunction
call assert_equal('[abc]', s:Use())
call assert_equal('ok', s:fnameescape('ok'))
`},
		{Name: "fixture-help-text", IDs: []int{15, 16, 17}, Want: "E492:", Body: "account. Thus in\n"},
		{Name: "fixture-unclosed-echo-string", IDs: []int{18}, Want: "E114:", Body: `echom "hello andy" " CURSOR
`},
		{Name: "fixture-top-level-return", IDs: []int{19, 20, 21, 22, 23, 25, 26, 27}, Want: "E133:", Body: "return 1\n"},
		{Name: "fixture-unmatched-endwhile", IDs: []int{24}, Want: "E588:", Body: "endwhile\n"},
		{Name: "local-dictionary-method", IDs: []int{34, 35, 36, 37, 38}, Body: `function! Test() abort
  let l:matcher = {'title': 'ok'}
  function! l:matcher.get_entry(context) abort dict
    return self.title . a:context
  endfunction
  call assert_equal('ok!', l:matcher.get_entry('!'))
  let l:popup = {}
  function l:popup.close() abort
    return 1
  endfunction
  call assert_equal(1, l:popup.close())
endfunction
call Test()
`},
		{Name: "session-fillchars-msgsep", IDs: []int{39, 42, 44, 46}, Want: "E474:", Body: "setlocal fillchars=msgsep:-\n"},
		{Name: "session-modifier-address", IDs: []int{41}, Body: `vim9script
setline(1, ['one', 'two', 'three'])
keepjumps :3
assert_equal(3, line('.'))
`},
		{Name: "view-macmeta", IDs: []int{48}, Want: "E518:", Body: "setlocal macmeta\n"},
		{Name: "new-plus-command-payload", IDs: []int{49, 50, 51, 52}, Body: `keepalt new +setlocal\ previewwindow|setlocal\ buftype=nofile|setlocal\ noswapfile|setlocal\ wrap [Document]
call assert_equal('nofile', &l:buftype)
call assert_equal(0, &l:swapfile)
call assert_equal(1, &l:previewwindow)
call assert_equal(1, &l:wrap)
`},
		{Name: "required-health-argument", IDs: []int{53, 54, 55, 56, 57}, Want: "E119:", Body: `function! s:report_error(report, advises) abort
  return a:report
endfunction
call s:report_error('test')
`},
		{Name: "vim9-autoload-call", IDs: []int{58, 84}, Body: `vim9script
def Test()
modula2#SetDialect('test', 'mod')
modula2#SetDialect('test')
assert_equal(2, g:oracle_autoload_calls)
enddef
Test()
`},
		{Name: "logical-expression-bool", IDs: []int{59, 70, 86}, Body: `vim9script
def Test(): bool
  var in_string = 0
  var c = 1
  var pos = 2
  return in_string || (c >= 0 && c <= pos)
enddef
def Other(): bool
  return get(g:, 'unused_oracle_key', 0)
    && (1 != 2 || 0 != -1)
enddef
assert_equal(true, Test())
assert_equal(false, Other())
var allfolds = [{level: 2}]
var firstfold = 1
if allfolds[0].level > 1 && firstfold
  assert_true(true)
endif
`},
		{Name: "map-changing-container-value-type", IDs: []int{60, 61}, Body: `vim9script
const patterns = {class: ['export', 'abstract']}
  ->map((_, mods: list<string>): string =>
    '\%(' .. mods
    ->join('\|')
    .. '\)')
assert_equal('\%(export\|abstract\)', patterns.class)
const BLOCKS: list<list<string>> = [['if', 'endif'], ['for', 'endfor']]
const ENDS_BLOCK: string = '^'
  .. BLOCKS
  ->copy()
  ->map((_, kwds: list<string>): string => kwds[-1])
  ->join('\|')
assert_equal('^endif\|endfor', ENDS_BLOCK)
`},
		{Name: "repeated-destructuring-ignore", IDs: []int{62, 63}, Body: `vim9script
def Test()
const [_, matched_line, matched_col, _, _] = [0, 1, 2, 3, 4]
assert_equal([1, 2], [matched_line, matched_col])
enddef
Test()
`},
		{Name: "typed-loop-destructuring", IDs: []int{64, 78}, Body: `vim9script
def Action(): any
  return 1
enddef
def IsEntry(first: string, second: string): bool
  return first == second
enddef
def Test(paths_action_pairs: list<list<any>>, lvl_and_test: list<list<any>>)
  for [paths: list<string>, Action: func: any] in paths_action_pairs
    assert_equal(['ok'], paths)
    assert_equal(1, Action())
  endfor
  for [lvl: string, IsEntry: func: bool] in lvl_and_test
    assert_equal('level', lvl)
    assert_equal(true, IsEntry('x', 'x'))
  endfor
enddef
Test([[['ok'], Action]], [['level', IsEntry]])
`},
		{Name: "delete-then-redefine-function", IDs: []int{65}, Body: `function s:LuaFold() abort
  return 'old'
endfunction
delfunction! s:LuaFold
def s:LuaFold(): string
  return 'new'
enddef
call assert_equal('new', s:LuaFold())
`},
		{Name: "counterexample-vim9-delete", Want: "E1084:", Body: `vim9script
def LuaFold(): string
  return 'old'
enddef
delfunction! LuaFold
def LuaFold(): string
  return 'new'
enddef
assert_equal('new', LuaFold())
`},
		{Name: "mapping-bar-payload", IDs: []int{66, 67}, Body: `nnoremap <buffer> <silent> <Plug>ManBS :setl ma<Bar>%s/.\b//g
      \ <Bar>setl noma<CR>
call assert_match('|setl noma', maparg('<Plug>ManBS', 'n'))
`},
		{Name: "map-funcref-arguments", IDs: []int{68, 79, 80, 81, 85}, Body: `vim9script
def Test()
assert_equal(['User1', 'User2'], range(1, 2)->map((_, n: number) => $'User{n}'))
var entries = [{text: 'abc'}]
assert_equal(['abc'], entries->copy()->map((_, entry: dict<any>): string => entry.text))
var pos = [[0, 1]]
var props = pos[0]->copy()->map((_, col: number) => ({col: col + 1, length: 1, type: 'help-fuzzy-toc'}))
assert_equal(2, props[1].col)
var HELP_TEXT = ['one', 'four']
var longest_line: number = HELP_TEXT->copy()->map((_, line: string) => line->strcharlen())->max()
assert_equal(4, longest_line)
var rgb = map(['ff', '00', '10'][: 2], (_, v) => str2nr(v, 16))
assert_equal([255, 0, 16], rgb)
enddef
Test()
`},
		{Name: "loop-initializer-lambda-shadow", IDs: []int{69}, Body: `vim9script
def Test()
  var line = '()'
  var count = 0
  for n in strpart(line, 0)->filter((_, n) => n =~ '[()]')->reverse()
    count += 1
  endfor
  assert_equal(2, count)
enddef
Test()
`},
		{Name: "legacy-call-without-call", IDs: []int{71}, Want: "E492:", Body: `function! Test() abort
  let lnum = 1
  cursor(lnum,1)
endfunction
call Test()
`},
		{Name: "vim9-command-quoted-args", IDs: []int{72, 73}, Body: `vim9script
def Qf_filter(qf: bool, searchpat: string, bang: string)
  assert_equal('needle', searchpat)
  assert_equal('!', bang)
enddef
command! -nargs=+ -bang Cfilter Qf_filter(true, <q-args>, <q-bang>)
command! -nargs=+ -bang Lfilter Qf_filter(false, <q-args>, <q-bang>)
Cfilter! needle
Lfilter! needle
`},
		{Name: "operatorfunc-lambda", IDs: []int{74}, Body: `vim9script
def Test()
  &opfunc = (_) => {
    return
  }
enddef
Test()
assert_equal(v:t_string, type(&opfunc))
assert_notequal('', &opfunc)
`},
		{Name: "legacy-setbufvar-without-call", IDs: []int{75, 76}, Want: "E492:", Body: `function! Test(bufnr) abort
  setbufvar(a:bufnr, '&shellslash', 0)
endfunction
call Test(bufnr())
`},
		{Name: "vim9-trailing-comma", IDs: []int{77}, Body: `vim9script
var b = {toc: {maxlvl: 1}}
var newtitle: string = printf(' %d',
  b.toc.maxlvl,
)
assert_equal(' 1', newtitle)
`},
		{Name: "legacy-readonly-argument", IDs: []int{82, 83}, Want: "E46:", Body: `function! Test(newdir) abort
  let a:newdir = 'replacement'
endfunction
call Test('original')
`},
	}
	for _, item := range []struct {
		path     string
		line, id int
	}{
		{"/Users/chemzqm/.vim/sessions/coc-list.vim", 96, 40},
		{"/Users/chemzqm/.vim/sessions/mini.vim", 95, 43},
		{"/Users/chemzqm/.vim/sessions/python.vim", 91, 45},
		{"/Users/chemzqm/.vim/sessions/someroject-rust.vim", 103, 47},
	} {
		data, err := os.ReadFile(item.path)
		must(err)
		lines := strings.Split(string(data), "\n")
		body := lines[item.line-1]
		if !strings.HasPrefix(body, "setlocal listchars=") {
			panic("unexpected source command")
		}
		cases = append(cases, testCase{Name: fmt.Sprintf("session-listchars-%d", item.id), IDs: []int{item.id}, Body: body + "\n", Path: item.path, Line: item.line})
	}
	must(os.MkdirAll(filepath.Join(base, "runtime", "autoload"), 0o755))
	must(os.WriteFile(filepath.Join(base, "runtime", "autoload", "modula2.vim"), []byte("function! modula2#SetDialect(...) abort\n  let g:oracle_autoload_calls = get(g:, 'oracle_autoload_calls', 0) + 1\nendfunction\n"), 0o644))
	var results []result
	failed := false
	for _, c := range cases {
		casePath := filepath.Join(base, "cases", c.Name+".vim")
		must(os.WriteFile(casePath, []byte(c.Body), 0o644))
		reportPath := filepath.Join(base, c.Name+".json")
		driver := "set nomore\nset encoding=utf-8\nlet &runtimepath = " + quote(filepath.Join(base, "runtime")) + "\nlet g:oracle_exception = ''\nlet g:oracle_throwpoint = ''\ntry\n  execute 'source ' . fnameescape(" + quote(casePath) + ")\ncatch\n  let g:oracle_exception = v:exception\n  let g:oracle_throwpoint = v:throwpoint\nendtry\ncall writefile([json_encode({'version': v:version, 'patch1015': has('patch-9.2.1015'), 'patch1016': has('patch-9.2.1016'), 'errors': v:errors, 'messages': execute('messages'), 'exception': g:oracle_exception, 'throwpoint': g:oracle_throwpoint})], " + quote(reportPath) + ")\nif empty(g:oracle_exception) && empty(v:errors)\n  qa!\nelse\n  cquit 1\nendif\n"
		driverPath := filepath.Join(base, "driver.vim")
		must(os.WriteFile(driverPath, []byte(driver), 0o644))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, *vim, "-Nu", "NONE", "-U", "NONE", "-n", "-es", "-X", "-i", "NONE", "-S", driverPath)
		cmd.Dir = base
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		cancel()
		exit := 0
		if err != nil {
			exit = 1
			if e, ok := err.(*exec.ExitError); ok {
				exit = e.ExitCode()
			}
		}
		data, readErr := os.ReadFile(reportPath)
		var report struct {
			Exception string   `json:"exception"`
			Errors    []string `json:"errors"`
			Patch1015 int      `json:"patch1015"`
			Patch1016 int      `json:"patch1016"`
		}
		parseErr := json.Unmarshal(data, &report)
		verified := readErr == nil && parseErr == nil && len(report.Errors) == 0 && report.Patch1015 == 1 && report.Patch1016 == 0
		if c.Want == "" {
			verified = verified && exit == 0 && report.Exception == ""
		} else {
			verified = verified && exit != 0 && strings.Contains(report.Exception, c.Want)
		}
		if !verified {
			failed = true
		}
		if !json.Valid(data) {
			data = []byte("null")
		}
		results = append(results, result{c.Name, c.IDs, c.Want, exit, stdout.String(), stderr.String(), data, verified})
		fmt.Printf("%s verified=%t exit=%d exception=%s\n", c.Name, verified, exit, report.Exception)
	}
	data, err := json.MarshalIndent(results, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(base, "results.json"), append(data, '\n'), 0o644))
	if failed {
		os.Exit(1)
	}
}
func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
func must(err error) {
	if err != nil {
		panic(err)
	}
}
