" Curated legacy Vim script parser fixture for Vim v9.2.1015.
if exists('g:loaded_example')
  finish
endif
let g:loaded_example = 1

function! s:Collect(items, ...) abort
  let result = []
  try
    for item in a:items
      call add(result, item)
    endfor
  catch /^Vim\%((\a\+)\)\=:E/
    echoerr v:exception
  finally
    let g:last_collect = localtime()
  endtry
  return result
endfunction

command! -nargs=* ExampleCall call s:Collect([<f-args>])
augroup example_group
  autocmd!
  autocmd BufWritePost *.vim echomsg 'saved'
augroup END
