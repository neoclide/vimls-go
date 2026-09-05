function! Test(newdir) abort
  let a:newdir = 'replacement'
endfunction
call Test('original')
