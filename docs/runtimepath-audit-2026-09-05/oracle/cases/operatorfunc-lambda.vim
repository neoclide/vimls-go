vim9script
def Test()
  &opfunc = (_) => {
    return
  }
enddef
Test()
assert_equal(v:t_string, type(&opfunc))
assert_notequal('', &opfunc)
