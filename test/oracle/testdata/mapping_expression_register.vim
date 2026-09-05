" Curated interactive-expression mappings for Vim v9.2.1015.
" Provenance: insert.txt i_CTRL-R_= / i_CTRL-R_CTRL-R / i_CTRL-R_CTRL-O /
" i_CTRL-R_CTRL-P; cmdline.txt c_CTRL-R_= / c_CTRL-R_CTRL-O / c_CTRL-\_e;
" change.txt quote_=; repeat.txt @. Only scratch buffers/variables are changed.
set nocompatible
for s:keys in ['<C-R>=', '<C-R><C-R>=', '<C-R><C-O>=', '<C-R><C-P>=']
  execute 'inoremap <F5> ' .. s:keys .. 'string(123)<CR>'
  call setline(1, '')
  call feedkeys("i\<F5>\<Esc>", 'xt')
  call assert_equal('123', getline(1), s:keys)
endfor
iunmap <F5>
for s:keys in ['<C-R>=', '<C-R><C-R>=', '<C-R><C-O>=']
  execute 'cnoremap <F5> ' .. s:keys .. 'string(123)<CR>'
  let g:vimls_register_result = ''
  call feedkeys(":let g:vimls_register_result = '\<F5>'\<CR>", 'xt')
  call assert_equal('123', g:vimls_register_result, s:keys)
endfor
cnoremap <F5> <C-\>e'let g:vimls_register_result = "replaced"'<CR>
call feedkeys(":ignored\<F5>\<CR>", 'xt')
call assert_equal('replaced', g:vimls_register_result)
cunmap <F5>
call setline(1, '')
nnoremap <F5> "=string(123)<CR>p
call feedkeys("\<F5>", 'xt')
call assert_equal('123', getline(1))
call setline(1, '')
nnoremap <F5> @='i123' . nr2char(27)<CR>
call feedkeys("\<F5>", 'xt')
call assert_equal('123', getline(1))
nunmap <F5>
unlet g:vimls_register_result
