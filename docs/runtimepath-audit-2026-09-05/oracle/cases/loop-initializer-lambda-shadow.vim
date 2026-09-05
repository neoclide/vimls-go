vim9script
def Test()
  var line = '()'
  var count = 0
  for n in strpart(line, 0)->filter((_, n) => n =~ '[()]')->reverse()
    count += 1
  endfor
  assert_equal(2, count)
enddef
Test()
