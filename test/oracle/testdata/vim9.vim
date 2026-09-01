vim9script
# Curated Vim9 behavior oracle for Vim v9.2.1015.
# Syntax provenance: src/testdir/test_vim9_func.vim, test_vim9_class.vim,
# test_vim9_generics.vim and test_tuple.vim.

def Identity<T>(value: T): T
  return value
enddef

class Counter
  var value: number

  def new(value: number)
    this.value = value
  enddef

  def Double(): number
    return this.value * 2
  enddef
endclass

var pair: tuple<number, string> = (Identity<number>(3), 'vim')
var counter = Counter.new(pair[0])
assert_equal(6, counter.Double())
assert_equal('vim', pair[1])
