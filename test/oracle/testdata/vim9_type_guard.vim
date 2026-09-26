vim9script
# Curated regression for Vim v9.2.1015: a runtime type() guard does not
# change the compiled type of an any parameter. Compare script inference below.
# Source: src/vim9expr.c and runtime/doc/vim9.txt (variable-types).

def Guarded(value: any): number
  if type(value) == v:t_string
    var copied = value
    copied = 1
    return copied
  endif
  return 0
enddef
defcompile Guarded
assert_equal(1, Guarded('hello'))

var GuardedLambda = (value: any): number => {
  if type(value) == v:t_string
    var copied = value
    copied = 1
    return copied
  endif
  return 0
}
assert_equal(1, GuardedLambda('hello'))

var value: any = 'hello'
if type(value) == v:t_string
  var Callback = () => value
  assert_equal('func(): any', typename(Callback))

  var copied = value
  var rejected = false
  try
    copied = 1
  catch /E1012:/
    rejected = true
  endtry
  assert_true(rejected, 'script copy must retain its inferred string type')
endif
