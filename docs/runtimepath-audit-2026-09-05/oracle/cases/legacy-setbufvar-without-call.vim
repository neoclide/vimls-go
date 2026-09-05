function! Test(bufnr) abort
  setbufvar(a:bufnr, '&shellslash', 0)
endfunction
call Test(bufnr())
