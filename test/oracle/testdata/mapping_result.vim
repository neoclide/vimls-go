" Curated mapping conversion oracle for Vim v9.2.1015.
" Provenance: map.c:eval_map_expr, eval.c:typval2string and
" typval.c:tv_get_string_buf_chk_strict. Only scratch-buffer input is changed.
for [s:expression, s:expected] in [
      \ ["'text'", 'text'], ['42', '42'], ['1.5', '1.5'], ['v:true', 'v:true'],
      \ ['[1, 2]', '[1, 2]'], ["{'key': 1}", "{'key': 1}"],
      \ ["(1, 'two')", "(1, 'two')"]]
  execute 'inoremap <expr> <F5> ' .. s:expression
  call setline(1, '')
  call feedkeys("i\<F5>\<Esc>", 'xt')
  call assert_equal(s:expected, getline(1))
endfor
for [s:expression, s:code] in [['0z01', 'E976:'], ["{-> 'text'}", 'E729:']]
  execute 'inoremap <expr> <F5> ' .. s:expression
  try
    call feedkeys("i\<F5>\<Esc>", 'xt')
    call assert_report('expected ' .. s:code)
  catch
    call assert_match(s:code, v:exception)
  endtry
endfor
iunmap <F5>
