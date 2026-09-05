vim9script
def LuaFold(): string
  return 'old'
enddef
delfunction! LuaFold
def LuaFold(): string
  return 'new'
enddef
assert_equal('new', LuaFold())
