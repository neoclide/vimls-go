function s:LuaFold() abort
  return 'old'
endfunction
delfunction! s:LuaFold
def s:LuaFold(): string
  return 'new'
enddef
call assert_equal('new', s:LuaFold())
