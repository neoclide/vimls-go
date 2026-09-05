function! Test() abort
  let l:matcher = {'title': 'ok'}
  function! l:matcher.get_entry(context) abort dict
    return self.title . a:context
  endfunction
  call assert_equal('ok!', l:matcher.get_entry('!'))
  let l:popup = {}
  function l:popup.close() abort
    return 1
  endfunction
  call assert_equal(1, l:popup.close())
endfunction
call Test()
