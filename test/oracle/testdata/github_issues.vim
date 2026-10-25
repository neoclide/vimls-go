" Curated regressions for issues #11, #12 and #14, Vim v9.2.1132.
" Rules: src/userfunc.c and runtime/doc/vim9class.txt at the pinned tag.
if v:version >= 704
  function! s:Wrap(name) abort
    return function(a:name)
  endfunction
else
  function! s:Wrap(name) abort
    return function(a:name)
  endfunction
endif
call assert_equal('ABC', s:Wrap('toupper')('abc'))

let s:dir = tempname()
call mkdir(s:dir .. '/plugin', 'p')
try
  call writefile(['function! mylib#run() abort', 'return 42', 'endfunction'], s:dir .. '/plugin/mylib.vim')
  execute 'source ' .. fnameescape(s:dir .. '/plugin/mylib.vim')
  call assert_equal(42, mylib#run())
  call writefile(['function! wrong#run() abort', 'endfunction'], s:dir .. '/plugin/other.vim')
  try
    execute 'source ' .. fnameescape(s:dir .. '/plugin/other.vim')
    call assert_report('expected E746')
  catch /E746:/
  endtry
  call writefile(['vim9script', 'function! vim9name#run()', 'endfunction'], s:dir .. '/plugin/vim9name.vim')
  try
    execute 'source ' .. fnameescape(s:dir .. '/plugin/vim9name.vim')
    call assert_report('expected E1263')
  catch /E1263:/
  endtry
  call writefile(['vim9script', 'export class Foo', '  def Baz(): string', "    return 'here'", '  enddef', 'endclass'], s:dir .. '/Foo.vim')
  call writefile(['vim9script', "import autoload './Foo.vim'", 'class Bar extends Foo.Foo', '  def Qux(): string', '    return this.Baz()', '  enddef', 'endclass', "assert_equal('here', Bar.new().Qux())"], s:dir .. '/Bar.vim')
  execute 'source ' .. fnameescape(s:dir .. '/Bar.vim')
finally
  call delete(s:dir, 'rf')
endtry
