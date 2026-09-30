vim9script

var values = [
  'a|#', # ] | is comment text
  'b',
]
assert_equal(['a|#', 'b'], values)

var leading = 1
  + 2
  + 3
var trailing = 1 +
  2 +
  3
assert_equal(6, leading)
assert_equal(6, trailing)

var copied = values
  ->copy()
  ->copy()
assert_equal(values, copied)

var total = 0
for value in [
  1,
  2,
]
  total += value
endfor
assert_equal(3, total)

def Add(
  left: number,
  right: number,
): number
  return left + right
enddef
assert_equal(3, Add(1, 2))

var Calculate = () => {
  var local = 1 | local += 2
  return local
}
assert_equal(3, Calculate())

var choice = true ?
  (false ? 1 : 2)
  : 3
assert_equal(2, choice)

var kinds = [
  type(1),
  type('text'),
]
assert_equal([v:t_number, v:t_string], kinds)
var typed = {kind:
  type(1),
}
assert_equal(v:t_number, typed.kind)
const kind = false ? 1 :
  type('text')
assert_equal(v:t_string, kind)
assert_equal(function('len'),
  function('len')
)
var Functions = [
  function('len'),
]
assert_equal(3, Functions[0]('abc'))
