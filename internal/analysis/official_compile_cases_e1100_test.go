package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestOfficialVimCompileCasesE1100(t *testing.T) {
	cases := []struct {
		ID     string
		Code   string
		Source string
	}{
		// E1100
		// Vim: src/testdir/test_vim9_script.vim:4846:108652
		{
			ID:   "src/testdir/test_vim9_script.vim:4846:108652/def",
			Code: "vim/E1100",
			Source: `def Func()
# comment
:k a
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4856:108819
		{
			ID:   "src/testdir/test_vim9_script.vim:4856:108819/def",
			Code: "vim/E1100",
			Source: `def Func()
# comment
o
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4861:108900
		{
			ID:   "src/testdir/test_vim9_script.vim:4861:108900/def",
			Code: "vim/E1100",
			Source: `def Func()
# comment
t
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4866:108981
		{
			ID:   "src/testdir/test_vim9_script.vim:4866:108981/def",
			Code: "vim/E1100",
			Source: `def Func()
# comment
x
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4871:109064
		{
			ID:   "src/testdir/test_vim9_script.vim:4871:109064/def",
			Code: "vim/E1100",
			Source: `def Func()
# comment
xit
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4846:108652
		{
			ID:   "src/testdir/test_vim9_script.vim:4846:108652/vim9-script",
			Code: "vim/E1100",
			Source: `vim9script
:k a
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4856:108819
		{
			ID:   "src/testdir/test_vim9_script.vim:4856:108819/vim9-script",
			Code: "vim/E1100",
			Source: `vim9script
o
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4861:108900
		{
			ID:   "src/testdir/test_vim9_script.vim:4861:108900/vim9-script",
			Code: "vim/E1100",
			Source: `vim9script
t
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4866:108981
		{
			ID:   "src/testdir/test_vim9_script.vim:4866:108981/vim9-script",
			Code: "vim/E1100",
			Source: `vim9script
x
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4871:109064
		{
			ID:   "src/testdir/test_vim9_script.vim:4871:109064/vim9-script",
			Code: "vim/E1100",
			Source: `vim9script
xit
`,
		},

		// E1101
		// Vim: src/testdir/test_vim9_assign.vim:214:5363
		{
			ID:   "src/testdir/test_vim9_assign.vim:214:5363/def",
			Code: "vim/E1101",
			Source: `def Func()
# comment
var s:var = 123
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:215:5415
		{
			ID:   "src/testdir/test_vim9_assign.vim:215:5415/def",
			Code: "vim/E1101",
			Source: `def Func()
# comment
var s:var: number
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:2071:42738
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2071:42738/def",
			Code: "vim/E1101",
			Source: `def Func()
# comment
s:notexist:repl
#comment
enddef
defcompile
`,
		},

		// E1104
		// Vim: src/testdir/test_vim9_expr.vim:2347:70004
		{
			ID:   "src/testdir/test_vim9_expr.vim:2347:70004/def",
			Code: "vim/E1104",
			Source: `def Func()
# comment
var x = <number 123
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2347:70004
		{
			ID:   "src/testdir/test_vim9_expr.vim:2347:70004/vim9-script",
			Code: "vim/E1104",
			Source: `vim9script
var x = <number 123
`,
		},

		// E1105
		// Vim: src/testdir/test_vim9_assign.vim:282:6765
		{
			ID:   "src/testdir/test_vim9_assign.vim:282:6765/def",
			Code: "vim/E1105",
			Source: `def Func()
# comment
var s = '-'
s ..= [1, 2]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1947:57231
		{
			ID:   "src/testdir/test_vim9_expr.vim:1947:57231/def",
			Code: "vim/E1105",
			Source: `def Func()
# comment
echo 'a' .. [1]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1957:57464
		{
			ID:   "src/testdir/test_vim9_expr.vim:1957:57464/def",
			Code: "vim/E1105",
			Source: `def Func()
# comment
echo 'a' .. test_void()
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1967:57699
		{
			ID:   "src/testdir/test_vim9_expr.vim:1967:57699/def",
			Code: "vim/E1105",
			Source: `def Func()
# comment
echo 'a' .. function('len')
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1997:58500
		{
			ID:   "src/testdir/test_vim9_expr.vim:1997:58500/def",
			Code: "vim/E1105",
			Source: `def Func()
# comment
echo 'a' .. test_null_channel()
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2050:60746
		{
			ID:   "src/testdir/test_vim9_expr.vim:2050:60746/def",
			Code: "vim/E1105",
			Source: `def Func()
# comment
var x = 'a' .. {a: 1}
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2052:60923
		{
			ID:   "src/testdir/test_vim9_expr.vim:2052:60923/def",
			Code: "vim/E1105",
			Source: `def Func()
# comment
var x = 'a' .. 0z32
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2054:61102
		{
			ID:   "src/testdir/test_vim9_expr.vim:2054:61102/def",
			Code: "vim/E1105",
			Source: `def Func()
# comment
var x = 'a' .. function('len', ['a'])
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2068:61865
		{
			ID:   "src/testdir/test_vim9_expr.vim:2068:61865/def",
			Code: "vim/E1105",
			Source: `def Func()
# comment
var x = 'a' .. test_null_channel()
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:287:6943
		{
			ID:   "src/testdir/test_vim9_assign.vim:287:6943/def",
			Code: "vim/E1105",
			Source: `def Func()
# comment
var s = '-'
s ..= {a: 2}
#comment
enddef
defcompile
`,
		},

		// E1106
		// Vim: src/testdir/test_vim9_func.vim:4401:100889
		{
			ID:   "src/testdir/test_vim9_func.vim:4401:100889/vim9-script",
			Code: "vim/E1106",
			Source: `vim9script
echo [0, 1, 2]->map(() => 123)
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:4406:101035
		{
			ID:   "src/testdir/test_vim9_func.vim:4406:101035/vim9-script",
			Code: "vim/E1106",
			Source: `vim9script
echo [0, 1, 2]->map((_) => 123)
`,
		},

		// E1107
		// Vim: src/testdir/test_vim9_expr.vim:2283:68146
		{
			ID:   "src/testdir/test_vim9_expr.vim:2283:68146/def",
			Code: "vim/E1107",
			Source: `def Func()
# comment
var x = 0xff[1]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2284:68227
		{
			ID:   "src/testdir/test_vim9_expr.vim:2284:68227/def",
			Code: "vim/E1107",
			Source: `def Func()
# comment
var x = 0.7[1]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2589:77132
		{
			ID:   "src/testdir/test_vim9_expr.vim:2589:77132/def",
			Code: "vim/E1107",
			Source: `def Func()
# comment
var x = 1234[3]
#comment
enddef
defcompile
`,
		},

		// E1117
		// Vim: src/testdir/test_vim9_func.vim:1011:21900
		{
			ID:   "src/testdir/test_vim9_func.vim:1011:21900/def",
			Code: "vim/E1117",
			Source: `def Func()
# comment
def Outer()
  def Inner()
    # comment
  enddef
  def! Inner()
  enddef
enddef
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:1022:22130
		{
			ID:   "src/testdir/test_vim9_func.vim:1022:22130/def",
			Code: "vim/E1117",
			Source: `def Func()
# comment
def Outer()
  function Inner()
    " comment
  endfunc
  function! Inner()
  endfunc
enddef
#comment
enddef
defcompile
`,
		},

		// E1123
		// Vim: src/testdir/test_vim9_expr.vim:3866:112596
		{
			ID:   "src/testdir/test_vim9_expr.vim:3866:112596/def",
			Code: "vim/E1123",
			Source: `def Func()
# comment
var Ref = function('len' [1, 2])
#comment
enddef
defcompile
`,
		},

		// E1125
		// Vim: src/testdir/test_vim9_script.vim:271:6623
		{
			ID:   "src/testdir/test_vim9_script.vim:271:6623/def",
			Code: "vim/E1125",
			Source: `def Func()
# comment
final two
#comment
enddef
defcompile
`,
		},

		// E1126
		// Vim: src/testdir/test_vim9_assign.vim:2633:66108
		{
			ID:   "src/testdir/test_vim9_assign.vim:2633:66108/def",
			Code: "vim/E1126",
			Source: `def Func()
# comment
let a = 34
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2633:66108
		{
			ID:   "src/testdir/test_vim9_assign.vim:2633:66108/vim9-script",
			Code: "vim/E1126",
			Source: `vim9script
let a = 34
`,
		},

		// E1127
		// Vim: src/testdir/test_vim9_expr.vim:2617:78496
		{
			ID:   "src/testdir/test_vim9_expr.vim:2617:78496/def",
			Code: "vim/E1127",
			Source: `def Func()
# comment
var datalist: list<string>
def Main()
  datalist += ['x'.
enddef
Main()
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4475:129586
		{
			ID:   "src/testdir/test_vim9_expr.vim:4475:129586/def",
			Code: "vim/E1127",
			Source: `def Func()
# comment
var d = {one: 33}
assert_equal(33, d.
      one)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2617:78496
		{
			ID:   "src/testdir/test_vim9_expr.vim:2617:78496/vim9-script",
			Code: "vim/E1127",
			Source: `vim9script
var datalist: list<string>
def Main()
  datalist += ['x'.
enddef
Main()
`,
		},

		// E1135
		// Vim: src/testdir/test_expr.vim:70:2288
		{
			ID:   "src/testdir/test_expr.vim:70:2288/def",
			Code: "vim/E1135",
			Source: `def Func()
# comment
'x' ? 'yes' : 'no'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_expr.vim:72:2405
		{
			ID:   "src/testdir/test_expr.vim:72:2405/def",
			Code: "vim/E1135",
			Source: `def Func()
# comment
'1x' ? 'yes' : 'no'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:442:10185
		{
			ID:   "src/testdir/test_vim9_cmd.vim:442:10185/def",
			Code: "vim/E1135",
			Source: `def Func()
# comment
if 'text'
endif
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:464:10614
		{
			ID:   "src/testdir/test_vim9_cmd.vim:464:10614/def",
			Code: "vim/E1135",
			Source: `def Func()
# comment
g:cond = 0
if g:cond
elseif 'text'
endif
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:193:5363
		{
			ID:   "src/testdir/test_vim9_expr.vim:193:5363/def",
			Code: "vim/E1135",
			Source: `def Func()
# comment
var x = 'x' ? 'one' : 'two'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_expr.vim:70:2288
		{
			ID:   "src/testdir/test_expr.vim:70:2288/vim9-script",
			Code: "vim/E1135",
			Source: `vim9script
'x' ? 'yes' : 'no'
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1572:71958
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1572:71958/vim9-script",
			Code: "vim/E1135",
			Source: `vim9script
def F(i: number, v: any): string
  return 'bad'
enddef
echo filter([1, 2, 3], F)
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:464:10614
		{
			ID:   "src/testdir/test_vim9_cmd.vim:464:10614/vim9-script",
			Code: "vim/E1135",
			Source: `vim9script
g:cond = 0
if g:cond
elseif 'text'
endif
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:688:18828
		{
			ID:   "src/testdir/test_vim9_expr.vim:688:18828/vim9-script",
			Code: "vim/E1135",
			Source: `vim9script
if 'yes' || 0
echo 0
endif
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:859:23523
		{
			ID:   "src/testdir/test_vim9_expr.vim:859:23523/vim9-script",
			Code: "vim/E1135",
			Source: `vim9script
var s = 'asdf'
echo true && s
`,
		},

		// E1138
		// Vim: src/testdir/test_vim9_builtin.vim:4434:218443
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4434:218443/vim9-script",
			Code: "vim/E1138",
			Source: `vim9script
sort([1, 2, 3], (a: number, b: number) => true)
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2058:61371
		{
			ID:   "src/testdir/test_vim9_expr.vim:2058:61371/vim9-script",
			Code: "vim/E1138",
			Source: `vim9script
var x = 1 + v:true
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2059:61455
		{
			ID:   "src/testdir/test_vim9_expr.vim:2059:61455/vim9-script",
			Code: "vim/E1138",
			Source: `vim9script
var x = 1 + v:false
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2060:61540
		{
			ID:   "src/testdir/test_vim9_expr.vim:2060:61540/vim9-script",
			Code: "vim/E1138",
			Source: `vim9script
var x = 1 + true
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2061:61622
		{
			ID:   "src/testdir/test_vim9_expr.vim:2061:61622/vim9-script",
			Code: "vim/E1138",
			Source: `vim9script
var x = 1 + false
`,
		},

		// E1139
		// Vim: src/testdir/test_vim9_expr.vim:3252:96997
		{
			ID:   "src/testdir/test_vim9_expr.vim:3252:96997/def",
			Code: "vim/E1139",
			Source: `def Func()
# comment
var d = {['a']: 234, ['b': 'x'}
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3260:97154
		{
			ID:   "src/testdir/test_vim9_expr.vim:3260:97154/def",
			Code: "vim/E1139",
			Source: `def Func()
# comment
def Func()
  var d = {['a']: 234, ['b': 'x'}
enddef
defcompile
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3252:96997
		{
			ID:   "src/testdir/test_vim9_expr.vim:3252:96997/vim9-script",
			Code: "vim/E1139",
			Source: `vim9script
var d = {['a']: 234, ['b': 'x'}
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3260:97154
		{
			ID:   "src/testdir/test_vim9_expr.vim:3260:97154/vim9-script",
			Code: "vim/E1139",
			Source: `vim9script
def Func()
  var d = {['a']: 234, ['b': 'x'}
enddef
defcompile
`,
		},

		// E1141
		// Vim: src/testdir/test_listdict.vim:473:12754
		{
			ID:   "src/testdir/test_listdict.vim:473:12754/def",
			Code: "vim/E1141",
			Source: `def Func()
# comment
var n = 0
n.key = 3
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1850:47247
		{
			ID:   "src/testdir/test_vim9_assign.vim:1850:47247/def",
			Code: "vim/E1141",
			Source: `def Func()
# comment
var s = 'abc'
s[1] += 'x'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1856:47375
		{
			ID:   "src/testdir/test_vim9_assign.vim:1856:47375/def",
			Code: "vim/E1141",
			Source: `def Func()
# comment
var s = 'abc'
s[1] ..= 'x'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:679:16173
		{
			ID:   "src/testdir/test_vim9_assign.vim:679:16173/def",
			Code: "vim/E1141",
			Source: `def Func()
# comment
var lines: string
lines[9] = 'asdf'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1997:41247
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1997:41247/def",
			Code: "vim/E1141",
			Source: `def Func()
# comment
var ls = 'asdf'
redir => ls[1]
redir END
#comment
enddef
defcompile
`,
		},

		// E1143
		// Vim: src/testdir/test_vim9_script.vim:1568:32719
		{
			ID:   "src/testdir/test_vim9_script.vim:1568:32719/def",
			Code: "vim/E1143",
			Source: `def Func()
# comment
throw
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:2125:44736
		{
			ID:   "src/testdir/test_vim9_script.vim:2125:44736/def",
			Code: "vim/E1143",
			Source: `def Func()
# comment
var cond = true
if cond
  echo 'true'
elseif
  echo 'false'
endif
#comment
enddef
defcompile
`,
		},

		// E1144
		// Vim: src/testdir/test_vim9_script.vim:3618:76970
		{
			ID:   "src/testdir/test_vim9_script.vim:3618:76970/def",
			Code: "vim/E1144",
			Source: `def Func()
# comment
try# comment
  echo "yes"
catch
endtry
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3629:77194
		{
			ID:   "src/testdir/test_vim9_script.vim:3629:77194/def",
			Code: "vim/E1144",
			Source: `def Func()
# comment
try
  throw#comment
catch
endtry
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3641:77421
		{
			ID:   "src/testdir/test_vim9_script.vim:3641:77421/def",
			Code: "vim/E1144",
			Source: `def Func()
# comment
try
  echo "yes"
catch# comment
endtry
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3660:77800
		{
			ID:   "src/testdir/test_vim9_script.vim:3660:77800/def",
			Code: "vim/E1144",
			Source: `def Func()
# comment
try
echo "yes"
catch
endtry# comment
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4795:107577
		{
			ID:   "src/testdir/test_vim9_script.vim:4795:107577/def",
			Code: "vim/E1144",
			Source: `def Func()
# comment
exit_cb: Func})
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4800:107662
		{
			ID:   "src/testdir/test_vim9_script.vim:4800:107662/def",
			Code: "vim/E1144",
			Source: `def Func()
# comment
e#
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4795:107577
		{
			ID:   "src/testdir/test_vim9_script.vim:4795:107577/vim9-script",
			Code: "vim/E1144",
			Source: `vim9script
exit_cb: Func})
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4800:107662
		{
			ID:   "src/testdir/test_vim9_script.vim:4800:107662/vim9-script",
			Code: "vim/E1144",
			Source: `vim9script
e#
`,
		},

		// E1145
		// Vim: src/testdir/test_vim9_expr.vim:2863:85195
		{
			ID:   "src/testdir/test_vim9_expr.vim:2863:85195/def",
			Code: "vim/E1145",
			Source: `def Func()
# comment
var Func = (nr: number): int => {
    var ll =<< ENDIT
       nothing
#comment
enddef
defcompile
`,
		},

		// E1157
		// Vim: src/testdir/test_vim9_func.vim:1689:36926
		{
			ID:   "src/testdir/test_vim9_func.vim:1689:36926/def",
			Code: "vim/E1157",
			Source: `def Func()
# comment
var Ref = (): => 123
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:1689:36926
		{
			ID:   "src/testdir/test_vim9_func.vim:1689:36926/vim9-script",
			Code: "vim/E1157",
			Source: `vim9script
var Ref = (): => 123
`,
		},

		// E1158
		// Vim: src/testdir/test_vim9_builtin.vim:1420:62921
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1420:62921/def",
			Code: "vim/E1158",
			Source: `def Func()
# comment
echo flatten([1, 2, 3])
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1420:62921
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1420:62921/vim9-script",
			Code: "vim/E1158",
			Source: `vim9script
echo flatten([1, 2, 3])
`,
		},

		// E1163
		// Vim: src/testdir/test_vim9_assign.vim:558:13564
		{
			ID:   "src/testdir/test_vim9_assign.vim:558:13564/def",
			Code: "vim/E1163",
			Source: `def Func()
# comment
var x: number
var y: number
var z: string
[x, y, z] = [1, 2, 3]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:558:13564
		{
			ID:   "src/testdir/test_vim9_assign.vim:558:13564/vim9-script",
			Code: "vim/E1163",
			Source: `vim9script
var x: number
var y: number
var z: string
[x, y, z] = [1, 2, 3]
`,
		},

		// E1165
		// Vim: src/testdir/test_vim9_assign.vim:1745:45177
		{
			ID:   "src/testdir/test_vim9_assign.vim:1745:45177/def",
			Code: "vim/E1165",
			Source: `def Func()
# comment
var d = {x: 1}
d[1 : 2] = {y: 2}
#comment
enddef
defcompile
`,
		},

		// E1166
		// Vim: src/testdir/test_vim9_assign.vim:2701:67540
		{
			ID:   "src/testdir/test_vim9_assign.vim:2701:67540/def",
			Code: "vim/E1166",
			Source: `def Func()
# comment
var dd = {a: 1}
unlet dd["a" : "a"]
#comment
enddef
defcompile
`,
		},

		// E1167
		// Vim: src/testdir/test_vim9_func.vim:1619:35205
		{
			ID:   "src/testdir/test_vim9_func.vim:1619:35205/def",
			Code: "vim/E1167",
			Source: `def Func()
# comment
var one = 1
var l = [1, 2, 3]
echo map(l, (one) => one)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:1645:35799
		{
			ID:   "src/testdir/test_vim9_func.vim:1645:35799/def",
			Code: "vim/E1167",
			Source: `def Func()
# comment
def ShadowLocal()
  var one = 1
  var l = [1, 2, 3]
  echo map(l, (one) => one)
enddef
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:1653:35971
		{
			ID:   "src/testdir/test_vim9_func.vim:1653:35971/def",
			Code: "vim/E1167",
			Source: `def Func()
# comment
def Shadowarg(one: number)
  var l = [1, 2, 3]
  echo map(l, (one) => one)
enddef
#comment
enddef
defcompile
`,
		},

		// E1170
		// Vim: src/testdir/test_vim9_expr.vim:3122:92753
		{
			ID:   "src/testdir/test_vim9_expr.vim:3122:92753/def",
			Code: "vim/E1170",
			Source: `def Func()
# comment
var x = #{key: 8}
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3123:92819
		{
			ID:   "src/testdir/test_vim9_expr.vim:3123:92819/def",
			Code: "vim/E1170",
			Source: `def Func()
# comment
var x = 'a' #{a: 1}
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3125:92958
		{
			ID:   "src/testdir/test_vim9_expr.vim:3125:92958/def",
			Code: "vim/E1170",
			Source: `def Func()
# comment
var x = true ? #{a: 1}
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3127:93030
		{
			ID:   "src/testdir/test_vim9_expr.vim:3127:93030/def",
			Code: "vim/E1170",
			Source: `def Func()
# comment
var x = 'a'
 #{a: 1}
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:73:1788
		{
			ID:   "src/testdir/test_vim9_func.vim:73:1788/def",
			Code: "vim/E1170",
			Source: `def Func()
# comment
#{ comment
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3122:92753
		{
			ID:   "src/testdir/test_vim9_expr.vim:3122:92753/vim9-script",
			Code: "vim/E1170",
			Source: `vim9script
var x = #{key: 8}
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3123:92819
		{
			ID:   "src/testdir/test_vim9_expr.vim:3123:92819/vim9-script",
			Code: "vim/E1170",
			Source: `vim9script
var x = 'a' #{a: 1}
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3124:92887
		{
			ID:   "src/testdir/test_vim9_expr.vim:3124:92887/vim9-script",
			Code: "vim/E1170",
			Source: `vim9script
var x = 'a' .. #{a: 1}
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3125:92958
		{
			ID:   "src/testdir/test_vim9_expr.vim:3125:92958/vim9-script",
			Code: "vim/E1170",
			Source: `vim9script
var x = true ? #{a: 1}
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3127:93030
		{
			ID:   "src/testdir/test_vim9_expr.vim:3127:93030/vim9-script",
			Code: "vim/E1170",
			Source: `vim9script
var x = 'a'
 #{a: 1}
`,
		},

		// E1171
		// Vim: src/testdir/test_vim9_expr.vim:2855:84948
		{
			ID:   "src/testdir/test_vim9_expr.vim:2855:84948/def",
			Code: "vim/E1171",
			Source: `def Func()
# comment
var Func = (nr: number): int => {
        return nr
#comment
enddef
defcompile
`,
		},

		// E1172
		// Vim: src/testdir/test_vim9_func.vim:1626:35394
		{
			ID:   "src/testdir/test_vim9_func.vim:1626:35394/def",
			Code: "vim/E1172",
			Source: `def Func()
# comment
var Ref: func(any, ?any): bool
Ref = (_, y = 1) => false
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:1626:35394
		{
			ID:   "src/testdir/test_vim9_func.vim:1626:35394/vim9-script",
			Code: "vim/E1172",
			Source: `vim9script
var Ref: func(any, ?any): bool
Ref = (_, y = 1) => false
`,
		},

		// E1174
		// Vim: src/testdir/test_float_func.vim:247:9921
		{
			ID:   "src/testdir/test_float_func.vim:247:9921/vim9-script",
			Code: "vim/E1174",
			Source: `vim9script
str2float(1.2)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1411:62444
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1411:62444/vim9-script",
			Code: "vim/E1174",
			Source: `vim9script
findfile("a", [])
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2296:106393
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2296:106393/vim9-script",
			Code: "vim/E1174",
			Source: `vim9script
hlget([])
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2936:133510
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2936:133510/vim9-script",
			Code: "vim/E1174",
			Source: `vim9script
mapcheck(1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3408:165571
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3408:165571/vim9-script",
			Code: "vim/E1174",
			Source: `vim9script
prop_type_get({"a": 10}, "b")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4000:192990
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4000:192990/vim9-script",
			Code: "vim/E1174",
			Source: `vim9script
shellescape(1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4438:218667
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4438:218667/vim9-script",
			Code: "vim/E1174",
			Source: `vim9script
spellbadword(100)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4744:236765
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4744:236765/vim9-script",
			Code: "vim/E1174",
			Source: `vim9script
tabpagewinnr(1, 2)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:5154:260187
		{
			ID:   "src/testdir/test_vim9_builtin.vim:5154:260187/vim9-script",
			Code: "vim/E1174",
			Source: `vim9script
win_execute(1, "b", 3)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:734:30720
		{
			ID:   "src/testdir/test_vim9_builtin.vim:734:30720/vim9-script",
			Code: "vim/E1174",
			Source: `vim9script
char2nr(10)
`,
		},

		// E1176
		// Vim: src/testdir/test_vim9_cmd.vim:1227:26455
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1227:26455/def",
			Code: "vim/E1176",
			Source: `def Func()
# comment
if g:maybe
silent endif
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1233:26572
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1233:26572/def",
			Code: "vim/E1176",
			Source: `def Func()
# comment
for i in [0]
silent endfor
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1249:26910
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1249:26910/def",
			Code: "vim/E1176",
			Source: `def Func()
# comment
silent try
finally
endtry
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1256:27030
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1256:27030/def",
			Code: "vim/E1176",
			Source: `def Func()
# comment
try
silent catch
endtry
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1270:27274
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1270:27274/def",
			Code: "vim/E1176",
			Source: `def Func()
# comment
try
finally
silent endtry
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1227:26455
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1227:26455/vim9-script",
			Code: "vim/E1176",
			Source: `vim9script
if g:maybe
silent endif
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1249:26910
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1249:26910/vim9-script",
			Code: "vim/E1176",
			Source: `vim9script
silent try
finally
endtry
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1256:27030
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1256:27030/vim9-script",
			Code: "vim/E1176",
			Source: `vim9script
try
silent catch
endtry
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1263:27152
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1263:27152/vim9-script",
			Code: "vim/E1176",
			Source: `vim9script
try
silent finally
endtry
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1270:27274
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1270:27274/vim9-script",
			Code: "vim/E1176",
			Source: `vim9script
try
finally
silent endtry
`,
		},

		// E1177
		// Vim: src/testdir/test_vim9_script.vim:3081:64993
		{
			ID:   "src/testdir/test_vim9_script.vim:3081:64993/def",
			Code: "vim/E1177",
			Source: `def Func()
# comment
for i in {a: 1}
echo 3
endfor
#comment
enddef
defcompile
`,
		},

		// E1178
		// Vim: src/testdir/test_vim9_cmd.vim:1817:37646
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1817:37646/def",
			Code: "vim/E1178",
			Source: `def Func()
# comment
var theList = [1, 2, 3]
lockvar theList
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1823:37769
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1823:37769/def",
			Code: "vim/E1178",
			Source: `def Func()
# comment
var theList = [1, 2, 3]
unlockvar theList
#comment
enddef
defcompile
`,
		},

		// E1180
		// Vim: src/testdir/test_vim9_func.vim:2883:66057
		{
			ID:   "src/testdir/test_vim9_func.vim:2883:66057/def",
			Code: "vim/E1180",
			Source: `def Func()
# comment
var Ref1: func(...bool)
Ref1 = g:FuncTwoArgNoRet
#comment
enddef
defcompile
`,
		},

		// E1181
		// Vim: src/testdir/test_vim9_func.vim:4389:100641
		{
			ID:   "src/testdir/test_vim9_func.vim:4389:100641/def",
			Code: "vim/E1181",
			Source: `def Func()
# comment
var _ = 1
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:4394:100735
		{
			ID:   "src/testdir/test_vim9_func.vim:4394:100735/def",
			Code: "vim/E1181",
			Source: `def Func()
# comment
var x = _
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:4389:100641
		{
			ID:   "src/testdir/test_vim9_func.vim:4389:100641/vim9-script",
			Code: "vim/E1181",
			Source: `vim9script
var _ = 1
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:4394:100735
		{
			ID:   "src/testdir/test_vim9_func.vim:4394:100735/vim9-script",
			Code: "vim/E1181",
			Source: `vim9script
var x = _
`,
		},

		// E1185
		// Vim: src/testdir/test_vim9_cmd.vim:1990:41128
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1990:41128/def",
			Code: "vim/E1185",
			Source: `def Func()
# comment
var text: string
redir => text
#comment
enddef
defcompile
`,
		},

		// E1190
		// Vim: src/testdir/test_vim9_builtin.vim:2804:128259
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2804:128259/vim9-script",
			Code: "vim/E1190",
			Source: `vim9script
range(3)->map((a, b, c) => a + b + c)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2808:128422
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2808:128422/vim9-script",
			Code: "vim/E1190",
			Source: `vim9script
range(3)->map((a, b, c, d) => a + b + c + d)
`,
		},
	}
	seenIDs := make(map[string]bool, len(cases))
	counts := make(map[string]int)
	for _, testCase := range cases {
		if testCase.ID == "" || testCase.Code == "" || testCase.Source == "" {
			t.Errorf("incomplete official compile case: %#v", testCase)
			continue
		}
		if seenIDs[testCase.ID] {
			t.Errorf("duplicate official compile case ID %q", testCase.ID)
			continue
		}
		seenIDs[testCase.ID] = true
		counts[testCase.Code]++
		if counts[testCase.Code] > 10 {
			t.Errorf("%s has %d official cases, limit is 10", testCase.Code, counts[testCase.Code])
			continue
		}
		name := strings.TrimPrefix(testCase.Code, "vim/") + "/" + strings.ReplaceAll(testCase.ID, "/", "_")
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("%s: analysis panicked: %v", testCase.ID, recovered)
				}
			}()
			file := syntax.Parse(testCase.Source)
			if file.Source != testCase.Source || len(file.Commands) == 0 {
				t.Fatalf("%s: parser did not retain official compile source", testCase.ID)
			}
			diagnostics := CombinedDiagnostics(file, Analyze(file))
			found := false
			for _, diagnostic := range diagnostics {
				if diagnostic.Span.Start < 0 || diagnostic.Span.End < diagnostic.Span.Start || diagnostic.Span.End > len(testCase.Source) {
					t.Fatalf("%s: out-of-bounds diagnostic %#v", testCase.ID, diagnostic)
				}
				if diagnostic.Code == testCase.Code {
					found = true
				}
			}
			if !found {
				t.Fatalf("diagnostics=%#v, want %s", diagnostics, testCase.Code)
			}
		})
	}
}
