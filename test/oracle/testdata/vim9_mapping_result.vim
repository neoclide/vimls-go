vim9script
# Curated mapping conversion oracle for Vim v9.2.1015; same source chain as
# mapping_result.vim, with mappings defined in a Vim9 script context.
for [expression, expected] in [
    ["'text'", 'text'], ['42', '42'], ['1.5', '1.5'], ['true', 'true'],
    ['[1, 2]', '[1, 2]'], ["{'key': 1}", "{'key': 1}"],
    ["(1, 'two')", "(1, 'two')"]]
  execute 'inoremap <expr> <F5> ' .. expression
  setline(1, '')
  feedkeys("i\<F5>\<Esc>", 'xt')
  assert_equal(expected, getline(1))
endfor
for [expression, code] in [['0z01', 'E976:'], ["() => 'text'", 'E729:']]
  execute 'inoremap <expr> <F5> ' .. expression
  try
    feedkeys("i\<F5>\<Esc>", 'xt')
    assert_report('expected ' .. code)
  catch
    assert_match(code, v:exception)
  endtry
endfor
iunmap <F5>
