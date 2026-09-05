vim9script
def Test()
const [_, matched_line, matched_col, _, _] = [0, 1, 2, 3, 4]
assert_equal([1, 2], [matched_line, matched_col])
enddef
Test()
