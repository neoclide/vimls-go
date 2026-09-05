vim9script
def Test(): bool
  var in_string = 0
  var c = 1
  var pos = 2
  return in_string || (c >= 0 && c <= pos)
enddef
def Other(): bool
  return get(g:, 'unused_oracle_key', 0)
    && (1 != 2 || 0 != -1)
enddef
assert_equal(true, Test())
assert_equal(false, Other())
var allfolds = [{level: 2}]
var firstfold = 1
if allfolds[0].level > 1 && firstfold
  assert_true(true)
endif
