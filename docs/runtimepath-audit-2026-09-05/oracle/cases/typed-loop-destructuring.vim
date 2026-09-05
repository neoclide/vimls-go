vim9script
def Action(): any
  return 1
enddef
def IsEntry(first: string, second: string): bool
  return first == second
enddef
def Test(paths_action_pairs: list<list<any>>, lvl_and_test: list<list<any>>)
  for [paths: list<string>, Action: func: any] in paths_action_pairs
    assert_equal(['ok'], paths)
    assert_equal(1, Action())
  endfor
  for [lvl: string, IsEntry: func: bool] in lvl_and_test
    assert_equal('level', lvl)
    assert_equal(true, IsEntry('x', 'x'))
  endfor
enddef
Test([[['ok'], Action]], [['level', IsEntry]])
