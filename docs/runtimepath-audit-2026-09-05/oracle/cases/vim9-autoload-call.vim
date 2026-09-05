vim9script
def Test()
modula2#SetDialect('test', 'mod')
modula2#SetDialect('test')
assert_equal(2, g:oracle_autoload_calls)
enddef
Test()
