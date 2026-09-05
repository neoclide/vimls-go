command -nargs=* CompilerSet setlocal <args>
CompilerSet shiftwidth=3
call assert_equal(3, &l:shiftwidth)
