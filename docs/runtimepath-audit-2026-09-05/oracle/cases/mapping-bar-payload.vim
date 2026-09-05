nnoremap <buffer> <silent> <Plug>ManBS :setl ma<Bar>%s/.\b//g
      \ <Bar>setl noma<CR>
call assert_match('|setl noma', maparg('<Plug>ManBS', 'n'))
