function! modula2#SetDialect(...) abort
  let g:oracle_autoload_calls = get(g:, 'oracle_autoload_calls', 0) + 1
endfunction
