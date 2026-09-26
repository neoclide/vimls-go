vim9script
# Curated regressions pinned to Vim v9.2.1015. See Test_instanceof and
# Test_null_object_assign_compare in src/testdir/test_vim9_class.vim,
# f_instanceof in src/vim9class.c, and runtime/doc/vim9.txt (variable-types).

class GuardObject
  def Value(): number
    return 7
  enddef
endclass

def Guarded(value: GuardObject): number
  var total = 0
  if value != null_object
    total += value.Value()
  endif
  if value isnot null_object
    total += value.Value()
  endif
  if value != null
    total += value.Value()
  endif
  if value == null_object
  else
    total += value.Value()
  endif
  if value is null_object
  else
    total += value.Value()
  endif
  if value == null
  else
    total += value.Value()
  endif
  if instanceof(value, GuardObject)
    total += value.Value()
  endif
  if !(null_object == (value))
    total += value.Value()
  endif
  return total
enddef

var missing: GuardObject
var present = GuardObject.new()
assert_equal(0, Guarded(missing))
assert_equal(56, Guarded(present))

# The interpreted script comparisons have the same null-excluding outcomes.
for value in [missing, present]
  var expected = value is present
  assert_equal(expected, value != null_object)
  assert_equal(expected, value isnot null_object)
  assert_equal(expected, value != null)
  assert_equal(!expected, value == null_object)
  assert_equal(!expected, value is null_object)
  assert_equal(!expected, value == null)
  assert_equal(expected, null_object != value)
  assert_equal(expected, null_object isnot value)
  assert_equal(expected, null != value)
  assert_equal(!expected, null_object == value)
  assert_equal(!expected, null_object is value)
  assert_equal(!expected, null == value)
  assert_equal(expected, instanceof(value, GuardObject) != 0)
  assert_equal(expected, !(null_object == (value)))
endfor

# Identity with null and instanceof with null_class do not exclude null_object.
assert_true(missing isnot null)
assert_false(missing is null)
assert_true(instanceof(missing, null_class))
assert_true(instanceof(missing, null_class, GuardObject))
assert_false(instanceof(missing, GuardObject, null_class))

def InvalidIdentityGuard(value: GuardObject)
  if value isnot null
    value.Value()
  endif
enddef
var rejected = false
try
  InvalidIdentityGuard(missing)
catch /E1360:/
  rejected = true
endtry
assert_true(rejected, 'isnot null must not hide a null object error')

rejected = false
try
  if instanceof(missing, null_class)
    missing.Value()
  endif
catch /E1360:/
  rejected = true
endtry
assert_true(rejected, 'instanceof with null_class can match a null object')

# A guard does not change the compiled Base type into Derived.
class Base
endclass
class Derived extends Base
  def OnlyDerived(): number
    return 11
  enddef
endclass
def NeedsDerived(value: Derived): number
  return value.OnlyDerived()
enddef
def InvalidGuardedMember(value: Base): number
  if instanceof(value, Derived)
    return value.OnlyDerived()
  endif
  return 0
enddef
def InvalidGuardedArgument(value: Base): number
  if instanceof(value, Derived)
    return NeedsDerived(value)
  endif
  return 0
enddef
rejected = false
try
  defcompile InvalidGuardedMember
catch /E1325:/
  rejected = true
endtry
assert_true(rejected, 'instanceof must retain compiled member checks')
rejected = false
try
  defcompile InvalidGuardedArgument
catch /E1013:/
  rejected = true
endtry
assert_true(rejected, 'instanceof must retain compiled argument checks')

def ExplicitCast(value: Base): number
  if instanceof(value, Derived)
    return NeedsDerived(<Derived>value)
  endif
  return 0
enddef
assert_equal(11, ExplicitCast(Derived.new()))
