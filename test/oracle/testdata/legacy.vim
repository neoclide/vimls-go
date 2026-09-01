" Curated legacy behavior oracle for Vim v9.2.1015.
" Syntax provenance: runtime/doc/eval.txt and src/testdir/test_vimscript.vim.
scriptversion 4

function! s:Sum(values) abort
  let total = 0
  for value in a:values
    let total += value
  endfor
  return total
endfunction

call assert_equal(6, s:Sum([1, 2, 3]))
call assert_equal('fallback', get({}, 'missing', 'fallback'))
