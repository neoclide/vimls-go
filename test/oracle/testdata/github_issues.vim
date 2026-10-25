" Curated regressions for issue #11, Vim v9.2.1132.
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
