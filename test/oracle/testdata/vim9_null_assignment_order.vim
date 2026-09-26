vim9script
# Curated assignment-order cases for Vim v9.2.1015. The null-object behavior
# is covered by Test_object_not_set in src/testdir/test_vim9_class.vim.

class TimingObject
  def Value(): number
    return 7
  enddef
  def Copy(): TimingObject
    return TimingObject.new()
  enddef
endclass

var top: TimingObject
def Initialize()
  top = TimingObject.new()
enddef
var InitializeLambda = () => {
  top = TimingObject.new()
}
command VimlsNullInitialize {
  top = TimingObject.new()
}

# Defining writers does not execute them, and a later write cannot repair an
# earlier null access.
var rejected = false
try
  top.Value()
catch /E1360:/
  rejected = true
endtry
assert_true(rejected, 'deferred definitions must not initialize top')
top = TimingObject.new()
assert_equal(7, top.Value())

def ReadBeforeWrite()
  var local: TimingObject
  local.Value()
  local = TimingObject.new()
enddef
var ReadBeforeWriteLambda = () => {
  var local: TimingObject
  local.Value()
  local = TimingObject.new()
}
for Callback in [ReadBeforeWrite, ReadBeforeWriteLambda]
  rejected = false
  try
    Callback()
  catch /E1360:/
    rejected = true
  endtry
  assert_true(rejected, 'callable must read its local before the later write')
endfor

# Invoking a deferred writer does initialize the captured variable.
top = null_object
Initialize()
assert_equal(7, top.Value())
top = null_object
InitializeLambda()
assert_equal(7, top.Value())
top = null_object
VimlsNullInitialize
assert_equal(7, top.Value())
delcommand VimlsNullInitialize

# A captured read runs at invocation time, not at its textual definition.
top = null_object
var ReadCaptured = () => top.Value()
top = TimingObject.new()
assert_equal(7, ReadCaptured())

# The previous iteration can establish a value for a textually earlier read.
var across: TimingObject
for index in [0, 1]
  if index == 1
    assert_equal(7, across.Value())
  endif
  across = TimingObject.new()
endfor

# Assignment evaluates the right-hand side before replacing the receiver.
var rhs: TimingObject
rejected = false
try
  rhs = rhs.Copy()
catch /E1360:/
  rejected = true
endtry
assert_true(rejected, 'a write must not hide a null read in its own RHS')
