function! s:fnameescape(file) abort
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
