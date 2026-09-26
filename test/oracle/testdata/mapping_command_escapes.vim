" Curated mapping command escapes for Vim v9.2.1015.
" Provenance: map.txt map_bar/map_backslash; src/term.c replace_termcodes().
" Only temporary mappings and variables are changed.
set nocompatible
set cpoptions&vim
let g:vimls_mapping_values = []
nnoremap <F5> :call add(g:vimls_mapping_values, 'left\|right') \| call add(g:vimls_mapping_values, 'second<Bar>end')<bAr>call add(g:vimls_mapping_values, 'third')<CR>
call feedkeys("\<F5>", 'xt')
call assert_equal(['left|right', 'second|end', 'third'], g:vimls_mapping_values)

let g:vimls_mapping_values = []
nnoremap <F5> :<C-U>call add(g:vimls_mapping_values, 'first')
      \ \| call add(g:vimls_mapping_values, 'second')<Bar>call add(g:vimls_mapping_values, 'third')<CR>
call feedkeys("\<F5>", 'xt')
call assert_equal(['first', 'second', 'third'], g:vimls_mapping_values)

" Default cpoptions includes B: backslashes before <Bar> remain literal.
let g:vimls_mapping_values = []
nnoremap <F5> :call add(g:vimls_mapping_values, '\<Bar>')<CR>
call feedkeys("\<F5>", 'xt')
call assert_equal(['\|'], g:vimls_mapping_values)

let g:vimls_mapping_values = []
nnoremap <F5> :call add(g:vimls_mapping_values, '\\<Bar>')<CR>
call feedkeys("\<F5>", 'xt')
call assert_equal(['\\|'], g:vimls_mapping_values)

" The Ex boundary removes only the backslash immediately before a bar.
let g:vimls_mapping_values = []
nnoremap <F5> :call add(g:vimls_mapping_values, '\\|')<CR>
call feedkeys("\<F5>", 'xt')
call assert_equal(['\|'], g:vimls_mapping_values)

" Literal CTRL-V quotes the following byte, including an opening bracket.
let g:vimls_mapping_values = []
execute 'nnoremap <F5> :call add(g:vimls_mapping_values, ''' .. nr2char(22) .. '<Bar>'')<CR>'
call feedkeys("\<F5>", 'xt')
call assert_equal(['<Bar>'], g:vimls_mapping_values)

" Command-line editing can discard otherwise valid calls before execution.
let g:vimls_mapping_values = []
nnoremap <F5> :call add(g:vimls_mapping_values, 'discarded')<C-U>call add(g:vimls_mapping_values, 'kept')<Bar>call add(g:vimls_mapping_values, 'last')<CR>
call feedkeys("\<F5>", 'xt')
call assert_equal(['kept', 'last'], g:vimls_mapping_values)
nunmap <F5>
unlet g:vimls_mapping_values
