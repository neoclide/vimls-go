" Curated argument assignment oracle for Vim v9.2.1132.
" Provenance: src/evalvars.c:set_var_const(), src/testdir/test_let.vim:
" s:set_arg1(), s:set_arg2(), Test_let_arg_fail(); runtime/doc/userfunc.txt.
scriptversion 4

function! s:Undeclared(...) abort
  let a:foo = 3
endfunction

function! s:Declared(foo) abort
  let a:foo = 3
endfunction

function! s:Numbered(...) abort
  let a:1 = 3
endfunction

function! s:Compound(...) abort
  let a:foo += 3
endfunction

function! s:Mutate(foo) abort
  let a:foo[0] = 3
endfunction
let s:values = [1]
call s:Mutate(s:values)
call assert_equal([3], s:values)

function! s:Outer(foo) abort
  function! s:Closure() abort closure
    let a:foo = 3
  endfunction
  call s:Closure()
endfunction

for [s:command, s:error] in [
      \ ['call s:Undeclared()', 'E461: Illegal variable name: a:foo'],
      \ ['call s:Declared(1)', 'E46:'],
      \ ['call s:Numbered(1)', 'E46:'],
      \ ['call s:Numbered()', 'E461:'],
      \ ['call s:Compound()', 'E121:'],
      \ ['call s:Outer(1)', 'E46:']]
  try
    execute s:command
    call assert_report('expected ' .. s:error)
  catch
    call assert_match(s:error, v:exception)
  endtry
endfor
