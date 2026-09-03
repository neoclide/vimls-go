" Representative checks for the shared validators migrated from opt_did_set_cb.
set bufhidden=
set bufhidden=hide
let v:errmsg = ''
silent! set bufhidden=bogus
call assert_match('^E474:', v:errmsg)
let v:errmsg = ''
silent! setlocal bufhidden=bogus
call assert_match('^E474:', v:errmsg)
let v:errmsg = ''
silent! setglobal background:bogus
call assert_match('^E474:', v:errmsg)
let v:errmsg = ''
silent! setglobal bufhidden=bogus
call assert_equal('', v:errmsg)
let v:errmsg = ''
silent! let &g:buftype = 'bogus'
call assert_equal('', v:errmsg)

set belloff=
set belloff=all,error
let v:errmsg = ''
silent! set belloff=all,bogus
call assert_match('^E474:', v:errmsg)

set cpoptions=
set cpoptions=aA
let v:errmsg = ''
silent! set cpoptions=@
call assert_match('^E539:', v:errmsg)

set maxsearchcount=1
set maxsearchcount=9999
let v:errmsg = ''
silent! set maxsearchcount=0
call assert_match('^E487:', v:errmsg)
let v:errmsg = ''
silent! set maxsearchcount=10000
call assert_match('^E474:', v:errmsg)
let v:errmsg = ''
silent! set maxsearchcount=099999
call assert_match('^E474:', v:errmsg)
let v:errmsg = ''
silent! set maxsearchcount=+0
call assert_match('^E521:', v:errmsg)
let v:errmsg = ''
silent! set maxsearchcount=0'0
call assert_match('^E521:', v:errmsg)
let v:errmsg = ''
silent! let &maxsearchcount = 010000
call assert_equal('', v:errmsg)
let v:errmsg = ''
silent! let &maxsearchcount = 099999
call assert_match('^E474:', v:errmsg)
let v:errmsg = ''
silent! vim9cmd &maxsearchcount = 010000
call assert_match('^E474:', v:errmsg)
let v:errmsg = ''
silent! vim9cmd &maxsearchcount = 0xBEEF
call assert_match('^E474:', v:errmsg)
let v:errmsg = ''
silent! vim9cmd &maxsearchcount = 0xbeef
call assert_match('^E474:', v:errmsg)

let v:errmsg = ''
silent! let &bufhidden = 'bogus'
call assert_match('^E474:', v:errmsg)
let v:errmsg = ''
silent! vim9cmd &l:bufhidden = 'bogus'
call assert_match('^E474:', v:errmsg)
let v:errmsg = ''
silent! vim9cmd &maxsearchcount = 0x0
call assert_match('^E487:', v:errmsg)
