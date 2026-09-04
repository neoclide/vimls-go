package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestOfficialVimCompileCasesE0000(t *testing.T) {
	cases := []struct {
		ID     string
		Code   string
		Source string
	}{
		// E15
		// Vim: src/testdir/test_expr.vim:261:8317
		{
			ID:   "src/testdir/test_expr.vim:261:8317/def",
			Code: "vim/E15",
			Source: `def Func()
# comment
eval --3->range()
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1720:49034
		{
			ID:   "src/testdir/test_vim9_expr.vim:1720:49034/def",
			Code: "vim/E15",
			Source: `def Func()
# comment
var x = 1 is# 2
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1894:55984
		{
			ID:   "src/testdir/test_vim9_expr.vim:1894:55984/def",
			Code: "vim/E15",
			Source: `def Func()
# comment
echo 'abc' is? 'abc'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3786:110722
		{
			ID:   "src/testdir/test_vim9_expr.vim:3786:110722/def",
			Code: "vim/E15",
			Source: `def Func()
# comment
var n = 12
echo +-n
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:828:17920
		{
			ID:   "src/testdir/test_vim9_func.vim:828:17920/def",
			Code: "vim/E15",
			Source: `def Func()
# comment
def Func(x: number = )
enddef
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_expr.vim:261:8317
		{
			ID:   "src/testdir/test_expr.vim:261:8317/vim9-script",
			Code: "vim/E15",
			Source: `vim9script
eval --3->range()
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1721:49101
		{
			ID:   "src/testdir/test_vim9_expr.vim:1721:49101/vim9-script",
			Code: "vim/E15",
			Source: `vim9script
var x = 1 is? 2
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1904:56196
		{
			ID:   "src/testdir/test_vim9_expr.vim:1904:56196/vim9-script",
			Code: "vim/E15",
			Source: `vim9script
echo 'abc' isnot? 'abc'
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3781:110622
		{
			ID:   "src/testdir/test_vim9_expr.vim:3781:110622/vim9-script",
			Code: "vim/E15",
			Source: `vim9script
var n = 12
echo --n
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3069:64279
		{
			ID:   "src/testdir/test_vim9_script.vim:3069:64279/vim9-script",
			Code: "vim/E15",
			Source: `vim9script
for x in
`,
		},

		// E16
		// Vim: src/testdir/test_vim9_func.vim:3817:87815
		{
			ID:   "src/testdir/test_vim9_func.vim:3817:87815/def",
			Code: "vim/E16",
			Source: `def Func()
# comment
5tab echo 3
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:2521:52593
		{
			ID:   "src/testdir/test_vim9_script.vim:2521:52593/def",
			Code: "vim/E16",
			Source: `def Func()
# comment
def Foo()
  :$echowindow "foo"
enddef
defcompile
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:2521:52593
		{
			ID:   "src/testdir/test_vim9_script.vim:2521:52593/vim9-script",
			Code: "vim/E16",
			Source: `vim9script
def Foo()
  :$echowindow "foo"
enddef
defcompile
`,
		},

		// E46
		// Vim: src/testdir/test_vim9_expr.vim:2545:75816
		{
			ID:   "src/testdir/test_vim9_expr.vim:2545:75816/def",
			Code: "vim/E46",
			Source: `def Func()
# comment
v:true = true
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2546:75876
		{
			ID:   "src/testdir/test_vim9_expr.vim:2546:75876/def",
			Code: "vim/E46",
			Source: `def Func()
# comment
v:true = false
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2547:75937
		{
			ID:   "src/testdir/test_vim9_expr.vim:2547:75937/def",
			Code: "vim/E46",
			Source: `def Func()
# comment
v:false = true
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2548:75998
		{
			ID:   "src/testdir/test_vim9_expr.vim:2548:75998/def",
			Code: "vim/E46",
			Source: `def Func()
# comment
v:null = 11
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2549:76056
		{
			ID:   "src/testdir/test_vim9_expr.vim:2549:76056/def",
			Code: "vim/E46",
			Source: `def Func()
# comment
v:none = 22
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2545:75816
		{
			ID:   "src/testdir/test_vim9_expr.vim:2545:75816/vim9-script",
			Code: "vim/E46",
			Source: `vim9script
v:true = true
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2546:75876
		{
			ID:   "src/testdir/test_vim9_expr.vim:2546:75876/vim9-script",
			Code: "vim/E46",
			Source: `vim9script
v:true = false
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2547:75937
		{
			ID:   "src/testdir/test_vim9_expr.vim:2547:75937/vim9-script",
			Code: "vim/E46",
			Source: `vim9script
v:false = true
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2548:75998
		{
			ID:   "src/testdir/test_vim9_expr.vim:2548:75998/vim9-script",
			Code: "vim/E46",
			Source: `vim9script
v:null = 11
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3094:65403
		{
			ID:   "src/testdir/test_vim9_script.vim:3094:65403/vim9-script",
			Code: "vim/E46",
			Source: `vim9script
var d: list<dict<any>> = [{a: 0}]
for e in d
  e = {a: 0, b: ''}
endfor
`,
		},

		// E107
		// Vim: src/testdir/test_vim9_expr.vim:3862:112412
		{
			ID:   "src/testdir/test_vim9_expr.vim:3862:112412/def",
			Code: "vim/E107",
			Source: `def Func()
# comment
var x = 'yes'->g:Echo
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4171:120781
		{
			ID:   "src/testdir/test_vim9_expr.vim:4171:120781/def",
			Code: "vim/E107",
			Source: `def Func()
# comment
var x = 123->((x) => x + 5)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4479:129693
		{
			ID:   "src/testdir/test_vim9_expr.vim:4479:129693/def",
			Code: "vim/E107",
			Source: `def Func()
# comment
var l = [2]
l->((ll) => add(ll, 8))
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3862:112412
		{
			ID:   "src/testdir/test_vim9_expr.vim:3862:112412/vim9-script",
			Code: "vim/E107",
			Source: `vim9script
var x = 'yes'->g:Echo
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4171:120781
		{
			ID:   "src/testdir/test_vim9_expr.vim:4171:120781/vim9-script",
			Code: "vim/E107",
			Source: `vim9script
var x = 123->((x) => x + 5)
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4479:129693
		{
			ID:   "src/testdir/test_vim9_expr.vim:4479:129693/vim9-script",
			Code: "vim/E107",
			Source: `vim9script
var l = [2]
l->((ll) => add(ll, 8))
`,
		},

		// E109
		// Vim: src/testdir/test_vim9_script.vim:2323:48121
		{
			ID:   "src/testdir/test_vim9_script.vim:2323:48121/def",
			Code: "vim/E109",
			Source: `def Func()
# comment
if has('aaa') ? true false
#comment
enddef
defcompile
`,
		},

		// E110
		// Vim: src/testdir/test_vim9_assign.vim:2323:58151
		{
			ID:   "src/testdir/test_vim9_assign.vim:2323:58151/def",
			Code: "vim/E110",
			Source: `def Func()
# comment
var foo: func(number
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2328:58263
		{
			ID:   "src/testdir/test_vim9_assign.vim:2328:58263/def",
			Code: "vim/E110",
			Source: `def Func()
# comment
var foo: func(number): func(
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3744:109846
		{
			ID:   "src/testdir/test_vim9_expr.vim:3744:109846/def",
			Code: "vim/E110",
			Source: `def Func()
# comment
echo (123]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4495:130468
		{
			ID:   "src/testdir/test_vim9_expr.vim:4495:130468/def",
			Code: "vim/E110",
			Source: `def Func()
# comment
echo len('asdf'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:2086:46156
		{
			ID:   "src/testdir/test_vim9_func.vim:2086:46156/def",
			Code: "vim/E110",
			Source: `def Func()
# comment
var RefWrong: func(...list<string>, string)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:2087:46235
		{
			ID:   "src/testdir/test_vim9_func.vim:2087:46235/def",
			Code: "vim/E110",
			Source: `def Func()
# comment
var RefWrong: func(...list<string>, ?string)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:2322:48073
		{
			ID:   "src/testdir/test_vim9_script.vim:2322:48073/def",
			Code: "vim/E110",
			Source: `def Func()
# comment
if has('aaa'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2323:58151
		{
			ID:   "src/testdir/test_vim9_assign.vim:2323:58151/vim9-script",
			Code: "vim/E110",
			Source: `vim9script
var foo: func(number
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2328:58263
		{
			ID:   "src/testdir/test_vim9_assign.vim:2328:58263/vim9-script",
			Code: "vim/E110",
			Source: `vim9script
var foo: func(number): func(
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3744:109846
		{
			ID:   "src/testdir/test_vim9_expr.vim:3744:109846/vim9-script",
			Code: "vim/E110",
			Source: `vim9script
echo (123]
`,
		},

		// E111
		// Vim: src/testdir/test_vim9_expr.vim:4328:125740
		{
			ID:   "src/testdir/test_vim9_expr.vim:4328:125740/def",
			Code: "vim/E111",
			Source: `def Func()
# comment
var d = 'asdf'[1 : 2
echo d
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4328:125740
		{
			ID:   "src/testdir/test_vim9_expr.vim:4328:125740/vim9-script",
			Code: "vim/E111",
			Source: `vim9script
var d = 'asdf'[1 : 2
echo d
`,
		},

		// E113
		// Vim: src/testdir/test_vim9_assign.vim:1589:38687
		{
			ID:   "src/testdir/test_vim9_assign.vim:1589:38687/def",
			Code: "vim/E113",
			Source: `def Func()
# comment
&g:option = 5
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:188:4335
		{
			ID:   "src/testdir/test_vim9_assign.vim:188:4335/def",
			Code: "vim/E113",
			Source: `def Func()
# comment
&notex += 3
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1067:48931
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1067:48931/def",
			Code: "vim/E113",
			Source: `def Func()
# comment
if exists('+newoption')
  if &newoption == 'ok'
  endif
endif
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4173:120862
		{
			ID:   "src/testdir/test_vim9_expr.vim:4173:120862/def",
			Code: "vim/E113",
			Source: `def Func()
# comment
var x = &notexist
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4173:120862
		{
			ID:   "src/testdir/test_vim9_expr.vim:4173:120862/vim9-script",
			Code: "vim/E113",
			Source: `vim9script
var x = &notexist
`,
		},

		// E114
		// Vim: src/testdir/test_vim9_expr.vim:2462:72918
		{
			ID:   "src/testdir/test_vim9_expr.vim:2462:72918/def",
			Code: "vim/E114",
			Source: `def Func()
# comment
var x = "abc
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2473:73256
		{
			ID:   "src/testdir/test_vim9_expr.vim:2473:73256/def",
			Code: "vim/E114",
			Source: `def Func()
# comment
var x = $"foo
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:2320:47969
		{
			ID:   "src/testdir/test_vim9_script.vim:2320:47969/def",
			Code: "vim/E114",
			Source: `def Func()
# comment
if "aaa" == "bbb
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2462:72918
		{
			ID:   "src/testdir/test_vim9_expr.vim:2462:72918/vim9-script",
			Code: "vim/E114",
			Source: `vim9script
var x = "abc
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2473:73256
		{
			ID:   "src/testdir/test_vim9_expr.vim:2473:73256/vim9-script",
			Code: "vim/E114",
			Source: `vim9script
var x = $"foo
`,
		},

		// E115
		// Vim: src/testdir/test_vim9_expr.vim:2463:72978
		{
			ID:   "src/testdir/test_vim9_expr.vim:2463:72978/def",
			Code: "vim/E115",
			Source: `def Func()
# comment
var x = 'abc
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2464:73038
		{
			ID:   "src/testdir/test_vim9_expr.vim:2464:73038/def",
			Code: "vim/E115",
			Source: `def Func()
# comment
if 0
echo 'xx
endif
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:2321:48021
		{
			ID:   "src/testdir/test_vim9_script.vim:2321:48021/def",
			Code: "vim/E115",
			Source: `def Func()
# comment
if 'aaa' == 'bbb
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2463:72978
		{
			ID:   "src/testdir/test_vim9_expr.vim:2463:72978/vim9-script",
			Code: "vim/E115",
			Source: `vim9script
var x = 'abc
`,
		},

		// E116
		// Vim: src/testdir/test_vim9_expr.vim:3866:112596
		{
			ID:   "src/testdir/test_vim9_expr.vim:3866:112596/vim9-script",
			Code: "vim/E116",
			Source: `vim9script
var Ref = function('len' [1, 2])
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4475:129586
		{
			ID:   "src/testdir/test_vim9_expr.vim:4475:129586/vim9-script",
			Code: "vim/E116",
			Source: `vim9script
var d = {one: 33}
assert_equal(33, d.
      one)
`,
		},

		// E117
		// Only lower-case direct names are retained. Upper-case names can be
		// provided dynamically by user or plugin code and remain unknown.
		// Vim: src/testdir/test_vim9_expr.vim:4499:130880
		{
			ID:   "src/testdir/test_vim9_expr.vim:4499:130880/def",
			Code: "vim/E117",
			Source: `def Func()
# comment
echo doesnotexist()
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4499:130880
		{
			ID:   "src/testdir/test_vim9_expr.vim:4499:130880/vim9-script",
			Code: "vim/E117",
			Source: `vim9script
echo doesnotexist()
`,
		},

		// E118
		// Vim: src/testdir/test_vim9_func.vim:1658:36069
		{
			ID:   "src/testdir/test_vim9_func.vim:1658:36069/def",
			Code: "vim/E118",
			Source: `def Func()
# comment
echo ((a) => a)('aa', 'bb')
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:1663:36178
		{
			ID:   "src/testdir/test_vim9_func.vim:1663:36178/def",
			Code: "vim/E118",
			Source: `def Func()
# comment
echo 'aa'->((a) => a)('bb')
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:985:21283
		{
			ID:   "src/testdir/test_vim9_func.vim:985:21283/def",
			Code: "vim/E118",
			Source: `def Func()
# comment
def Nested()
enddef
Nested(66)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2351:109639
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2351:109639/vim9-script",
			Code: "vim/E118",
			Source: `vim9script
def TestIdx(v: dict<any>): bool
  return v.color == 'blue'
enddef

indexof([{color: "red"}], TestIdx)
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:1658:36069
		{
			ID:   "src/testdir/test_vim9_func.vim:1658:36069/vim9-script",
			Code: "vim/E118",
			Source: `vim9script
echo ((a) => a)('aa', 'bb')
`,
		},

		// E119
		// Vim: src/testdir/test_vim9_builtin.vim:1456:64946
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1456:64946/def",
			Code: "vim/E119",
			Source: `def Func()
# comment
atan2(1.2)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1495:68081
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1495:68081/def",
			Code: "vim/E119",
			Source: `def Func()
# comment
pow(1.1)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:76:2103
		{
			ID:   "src/testdir/test_vim9_builtin.vim:76:2103/def",
			Code: "vim/E119",
			Source: `def Func()
# comment
add([])
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3283:97622
		{
			ID:   "src/testdir/test_vim9_expr.vim:3283:97622/def",
			Code: "vim/E119",
			Source: `def Func()
# comment
def Failing()
  job_stop()
enddef
var dict = {name: Failing}
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:1593:34522
		{
			ID:   "src/testdir/test_vim9_func.vim:1593:34522/def",
			Code: "vim/E119",
			Source: `def Func()
# comment
echo ((i) => 0)()
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1456:64946
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1456:64946/vim9-script",
			Code: "vim/E119",
			Source: `vim9script
atan2(1.2)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1478:66718
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1478:66718/vim9-script",
			Code: "vim/E119",
			Source: `vim9script
fmod(1.1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1495:68081
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1495:68081/vim9-script",
			Code: "vim/E119",
			Source: `vim9script
pow(1.1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:76:2103
		{
			ID:   "src/testdir/test_vim9_builtin.vim:76:2103/vim9-script",
			Code: "vim/E119",
			Source: `vim9script
add([])
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3283:97622
		{
			ID:   "src/testdir/test_vim9_expr.vim:3283:97622/vim9-script",
			Code: "vim/E119",
			Source: `vim9script
def Failing()
  job_stop()
enddef
var dict = {name: Failing}
`,
		},

		// E121
		// Vim: src/testdir/test_expr.vim:895:35674
		{
			ID:   "src/testdir/test_expr.vim:895:35674/vim9-script",
			Code: "vim/E121",
			Source: `vim9script
var text = substitute ( 'some text' , 't' , 'T' , 'g' )
call assert_equal('some TexT', text)
`,
		},
		// Vim: src/testdir/test_tuple.vim:1168:32566
		{
			ID:   "src/testdir/test_tuple.vim:1168:32566/vim9-script",
			Code: "vim/E121",
			Source: `vim9script
var x = null_tupel
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2047:60582
		{
			ID:   "src/testdir/test_vim9_expr.vim:2047:60582/vim9-script",
			Code: "vim/E121",
			Source: `vim9script
var x = 6 + xxx
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2474:73317
		{
			ID:   "src/testdir/test_vim9_expr.vim:2474:73317/vim9-script",
			Code: "vim/E121",
			Source: `vim9script
var x = $"foo{xxx}"
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2775:82466
		{
			ID:   "src/testdir/test_vim9_expr.vim:2775:82466/vim9-script",
			Code: "vim/E121",
			Source: `vim9script
var Ref = (a) =< a + 1
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3139:93695
		{
			ID:   "src/testdir/test_vim9_expr.vim:3139:93695/vim9-script",
			Code: "vim/E121",
			Source: `vim9script
var x = {['a']: xxx}
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4168:120626
		{
			ID:   "src/testdir/test_vim9_expr.vim:4168:120626/vim9-script",
			Code: "vim/E121",
			Source: `vim9script
var x = [notfound]
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4181:121337
		{
			ID:   "src/testdir/test_vim9_expr.vim:4181:121337/vim9-script",
			Code: "vim/E121",
			Source: `vim9script
echo x:somevar
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4491:130201
		{
			ID:   "src/testdir/test_vim9_expr.vim:4491:130201/vim9-script",
			Code: "vim/E121",
			Source: `vim9script
v:nosuch += 3
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:69:2097
		{
			ID:   "src/testdir/test_vim9_expr.vim:69:2097/vim9-script",
			Code: "vim/E121",
			Source: `vim9script
var Z = g:cond ? FuncOne : FuncTwo
`,
		},

		// E170
		// Vim: src/testdir/test_vim9_script.vim:1538:31411
		{
			ID:   "src/testdir/test_vim9_script.vim:1538:31411/def",
			Code: "vim/E170",
			Source: `def Func()
# comment
while 1
endtry
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:1539:31464
		{
			ID:   "src/testdir/test_vim9_script.vim:1539:31464/def",
			Code: "vim/E170",
			Source: `def Func()
# comment
for i in range(5)
endtry
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3078:64889
		{
			ID:   "src/testdir/test_vim9_script.vim:3078:64889/def",
			Code: "vim/E170",
			Source: `def Func()
# comment
for i in range(3)
echo 3
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3469:74060
		{
			ID:   "src/testdir/test_vim9_script.vim:3469:74060/def",
			Code: "vim/E170",
			Source: `def Func()
# comment
while 1
echo 3
#comment
enddef
defcompile
`,
		},

		// E171
		// Vim: src/testdir/test_vim9_script.vim:1540:31527
		{
			ID:   "src/testdir/test_vim9_script.vim:1540:31527/def",
			Code: "vim/E171",
			Source: `def Func()
# comment
if 1
endtry
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:2081:44033
		{
			ID:   "src/testdir/test_vim9_script.vim:2081:44033/def",
			Code: "vim/E171",
			Source: `def Func()
# comment
if true
echo 1
#comment
enddef
defcompile
`,
		},

		// E176
		// Vim: src/testdir/test_vim9_builtin.vim:2351:109639
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2351:109639/def",
			Code: "vim/E176",
			Source: `def Func()
# comment
def TestIdx(v: dict<any>): bool
  return v.color == 'blue'
enddef

indexof([{color: "red"}], TestIdx)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2804:128259
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2804:128259/def",
			Code: "vim/E176",
			Source: `def Func()
# comment
range(3)->map((a, b, c) => a + b + c)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2808:128422
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2808:128422/def",
			Code: "vim/E176",
			Source: `def Func()
# comment
range(3)->map((a, b, c, d) => a + b + c + d)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:4401:100889
		{
			ID:   "src/testdir/test_vim9_func.vim:4401:100889/def",
			Code: "vim/E176",
			Source: `def Func()
# comment
echo [0, 1, 2]->map(() => 123)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:4406:101035
		{
			ID:   "src/testdir/test_vim9_func.vim:4406:101035/def",
			Code: "vim/E176",
			Source: `def Func()
# comment
echo [0, 1, 2]->map((_) => 123)
#comment
enddef
defcompile
`,
		},

		// E260
		// Vim: src/testdir/test_vim9_expr.vim:4190:121912
		{
			ID:   "src/testdir/test_vim9_expr.vim:4190:121912/vim9-script",
			Code: "vim/E260",
			Source: `vim9script
'yes'->
Echo()
`,
		},

		// E274
		// Vim: src/testdir/test_vim9_expr.vim:4480:129784
		{
			ID:   "src/testdir/test_vim9_expr.vim:4480:129784/def",
			Code: "vim/E274",
			Source: `def Func()
# comment
var l = [2]
l->((ll) => add(ll, 8)) ()
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4480:129784
		{
			ID:   "src/testdir/test_vim9_expr.vim:4480:129784/vim9-script",
			Code: "vim/E274",
			Source: `vim9script
var l = [2]
l->((ll) => add(ll, 8)) ()
`,
		},

		// E354
		// Vim: src/testdir/test_vim9_assign.vim:1596:38991
		{
			ID:   "src/testdir/test_vim9_assign.vim:1596:38991/def",
			Code: "vim/E354",
			Source: `def Func()
# comment
var @. = 5
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1598:39131
		{
			ID:   "src/testdir/test_vim9_assign.vim:1598:39131/def",
			Code: "vim/E354",
			Source: `def Func()
# comment
var @% = 5
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1603:39368
		{
			ID:   "src/testdir/test_vim9_assign.vim:1603:39368/def",
			Code: "vim/E354",
			Source: `def Func()
# comment
var @~ = 5
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3638:106933
		{
			ID:   "src/testdir/test_vim9_expr.vim:3638:106933/def",
			Code: "vim/E354",
			Source: `def Func()
# comment
@% = 'yes'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4163:120430
		{
			ID:   "src/testdir/test_vim9_expr.vim:4163:120430/def",
			Code: "vim/E354",
			Source: `def Func()
# comment
var x = @<
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3637:106875
		{
			ID:   "src/testdir/test_vim9_expr.vim:3637:106875/vim9-script",
			Code: "vim/E354",
			Source: `vim9script
@. = 'yes'
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3638:106933
		{
			ID:   "src/testdir/test_vim9_expr.vim:3638:106933/vim9-script",
			Code: "vim/E354",
			Source: `vim9script
@% = 'yes'
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3639:106991
		{
			ID:   "src/testdir/test_vim9_expr.vim:3639:106991/vim9-script",
			Code: "vim/E354",
			Source: `vim9script
@: = 'yes'
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3640:107049
		{
			ID:   "src/testdir/test_vim9_expr.vim:3640:107049/vim9-script",
			Code: "vim/E354",
			Source: `vim9script
@~ = 'yes'
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4163:120430
		{
			ID:   "src/testdir/test_vim9_expr.vim:4163:120430/vim9-script",
			Code: "vim/E354",
			Source: `vim9script
var x = @<
`,
		},

		// E475
		// Vim: src/testdir/test_vim9_assign.vim:217:5470
		{
			ID:   "src/testdir/test_vim9_assign.vim:217:5470/vim9-script",
			Code: "vim/E475",
			Source: `vim9script
var $VAR: number
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3924:188733
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3924:188733/vim9-script",
			Code: "vim/E475",
			Source: `vim9script
echo searchpair("a", "b", "c", "d", "f", 33)
`,
		},

		// E476
		// Vim: src/testdir/test_vim9_cmd.vim:2076:42938
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2076:42938/def",
			Code: "vim/E476",
			Source: `def Func()
# comment
notexist:repl
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:781:16158
		{
			ID:   "src/testdir/test_vim9_func.vim:781:16158/def",
			Code: "vim/E476",
			Source: `def Func()
# comment
call Test ('text')
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4836:108460
		{
			ID:   "src/testdir/test_vim9_script.vim:4836:108460/def",
			Code: "vim/E476",
			Source: `def Func()
# comment
ka
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4841:108556
		{
			ID:   "src/testdir/test_vim9_script.vim:4841:108556/def",
			Code: "vim/E476",
			Source: `def Func()
# comment
:1ka
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4881:109297
		{
			ID:   "src/testdir/test_vim9_script.vim:4881:109297/def",
			Code: "vim/E476",
			Source: `def Func()
# comment
mode 4
#comment
enddef
defcompile
`,
		},

		// E481
		// Vim: src/testdir/test_vim9_script.vim:4851:108739
		{
			ID:   "src/testdir/test_vim9_script.vim:4851:108739/def",
			Code: "vim/E481",
			Source: `def Func()
# comment
:1k a
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:72:1434
		{
			ID:   "src/testdir/test_vim9_script.vim:72:1434/def",
			Code: "vim/E481",
			Source: `def Func()
# comment
:123 eval 1 + 2
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:78:1542
		{
			ID:   "src/testdir/test_vim9_script.vim:78:1542/def",
			Code: "vim/E481",
			Source: `def Func()
# comment
:123 if true
endif
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:83:1641
		{
			ID:   "src/testdir/test_vim9_script.vim:83:1641/def",
			Code: "vim/E481",
			Source: `def Func()
# comment
:123 echo 'yes'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:88:1738
		{
			ID:   "src/testdir/test_vim9_script.vim:88:1738/def",
			Code: "vim/E481",
			Source: `def Func()
# comment
:123 cd there
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4851:108739
		{
			ID:   "src/testdir/test_vim9_script.vim:4851:108739/vim9-script",
			Code: "vim/E481",
			Source: `vim9script
:1k a
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:72:1434
		{
			ID:   "src/testdir/test_vim9_script.vim:72:1434/vim9-script",
			Code: "vim/E481",
			Source: `vim9script
:123 eval 1 + 2
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:78:1542
		{
			ID:   "src/testdir/test_vim9_script.vim:78:1542/vim9-script",
			Code: "vim/E481",
			Source: `vim9script
:123 if true
endif
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:83:1641
		{
			ID:   "src/testdir/test_vim9_script.vim:83:1641/vim9-script",
			Code: "vim/E481",
			Source: `vim9script
:123 echo 'yes'
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:88:1738
		{
			ID:   "src/testdir/test_vim9_script.vim:88:1738/vim9-script",
			Code: "vim/E481",
			Source: `vim9script
:123 cd there
`,
		},

		// E488
		// Vim: src/testdir/test_vim9_assign.vim:1202:28637
		{
			ID:   "src/testdir/test_vim9_assign.vim:1202:28637/def",
			Code: "vim/E488",
			Source: `def Func()
# comment
var dd = {one: 1}
dd.one) = 2
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1904:39418
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1904:39418/def",
			Code: "vim/E488",
			Source: `def Func()
# comment
s/from/\="x"/9
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4485:129977
		{
			ID:   "src/testdir/test_vim9_expr.vim:4485:129977/def",
			Code: "vim/E488",
			Source: `def Func()
# comment
var x = '1'isnot2
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:2459:51306
		{
			ID:   "src/testdir/test_vim9_script.vim:2459:51306/def",
			Code: "vim/E488",
			Source: `def Func()
# comment
echomsg "xxx"# comment
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3654:77678
		{
			ID:   "src/testdir/test_vim9_script.vim:3654:77678/def",
			Code: "vim/E488",
			Source: `def Func()
# comment
try
  echo "yes"
catch /pat/# comment
endtry
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2923:72274
		{
			ID:   "src/testdir/test_vim9_assign.vim:2923:72274/vim9-script",
			Code: "vim/E488",
			Source: `vim9script
var x: string  'string'
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:472:10765
		{
			ID:   "src/testdir/test_vim9_cmd.vim:472:10765/vim9-script",
			Code: "vim/E488",
			Source: `vim9script
g:cond = 0
if g:cond
elseif 'text' garbage
endif
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4170:120710
		{
			ID:   "src/testdir/test_vim9_expr.vim:4170:120710/vim9-script",
			Code: "vim/E488",
			Source: `vim9script
var X = () => 123)
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4484:129910
		{
			ID:   "src/testdir/test_vim9_expr.vim:4484:129910/vim9-script",
			Code: "vim/E488",
			Source: `vim9script
var x = '1'is2
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:1755:38517
		{
			ID:   "src/testdir/test_vim9_func.vim:1755:38517/vim9-script",
			Code: "vim/E488",
			Source: `vim9script
timer_start(0, (_) => { | echo
    echo 'yes'
  })
`,
		},

		// E492
		// Vim: src/testdir/test_vim9_cmd.vim:2076:42938
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2076:42938/vim9-script",
			Code: "vim/E492",
			Source: `vim9script
notexist:repl
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4836:108460
		{
			ID:   "src/testdir/test_vim9_script.vim:4836:108460/vim9-script",
			Code: "vim/E492",
			Source: `vim9script
ka
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4841:108556
		{
			ID:   "src/testdir/test_vim9_script.vim:4841:108556/vim9-script",
			Code: "vim/E492",
			Source: `vim9script
:1ka
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:4881:109297
		{
			ID:   "src/testdir/test_vim9_script.vim:4881:109297/vim9-script",
			Code: "vim/E492",
			Source: `vim9script
mode 4
`,
		},

		// E580
		// Vim: src/testdir/test_vim9_script.vim:2079:43931
		{
			ID:   "src/testdir/test_vim9_script.vim:2079:43931/def",
			Code: "vim/E580",
			Source: `def Func()
# comment
endif
#comment
enddef
defcompile
`,
		},

		// E581
		// Vim: src/testdir/test_vim9_script.vim:2078:43891
		{
			ID:   "src/testdir/test_vim9_script.vim:2078:43891/def",
			Code: "vim/E581",
			Source: `def Func()
# comment
else
#comment
enddef
defcompile
`,
		},

		// E582
		// Vim: src/testdir/test_vim9_script.vim:2077:43844
		{
			ID:   "src/testdir/test_vim9_script.vim:2077:43844/def",
			Code: "vim/E582",
			Source: `def Func()
# comment
elseif true
#comment
enddef
defcompile
`,
		},

		// E583
		// Vim: src/testdir/test_vim9_script.vim:2105:44408
		{
			ID:   "src/testdir/test_vim9_script.vim:2105:44408/def",
			Code: "vim/E583",
			Source: `def Func()
# comment
if true
else
else
endif
#comment
enddef
defcompile
`,
		},

		// E584
		// Vim: src/testdir/test_vim9_script.vim:2115:44563
		{
			ID:   "src/testdir/test_vim9_script.vim:2115:44563/def",
			Code: "vim/E584",
			Source: `def Func()
# comment
var a = 3
if a == 2
else
elseif true
else
endif
#comment
enddef
defcompile
`,
		},

		// E586
		// Vim: src/testdir/test_vim9_script.vim:3465:73868
		{
			ID:   "src/testdir/test_vim9_script.vim:3465:73868/def",
			Code: "vim/E586",
			Source: `def Func()
# comment
continue
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3466:73912
		{
			ID:   "src/testdir/test_vim9_script.vim:3466:73912/def",
			Code: "vim/E586",
			Source: `def Func()
# comment
if true
continue
#comment
enddef
defcompile
`,
		},

		// E587
		// Vim: src/testdir/test_vim9_script.vim:3467:73967
		{
			ID:   "src/testdir/test_vim9_script.vim:3467:73967/def",
			Code: "vim/E587",
			Source: `def Func()
# comment
break
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3468:74008
		{
			ID:   "src/testdir/test_vim9_script.vim:3468:74008/def",
			Code: "vim/E587",
			Source: `def Func()
# comment
if true
break
#comment
enddef
defcompile
`,
		},

		// E588
		// Vim: src/testdir/test_vim9_script.vim:3077:64847
		{
			ID:   "src/testdir/test_vim9_script.vim:3077:64847/def",
			Code: "vim/E588",
			Source: `def Func()
# comment
endfor
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3464:73824
		{
			ID:   "src/testdir/test_vim9_script.vim:3464:73824/def",
			Code: "vim/E588",
			Source: `def Func()
# comment
endwhile
#comment
enddef
defcompile
`,
		},

		// E600
		// Vim: src/testdir/test_vim9_script.vim:1542:31637
		{
			ID:   "src/testdir/test_vim9_script.vim:1542:31637/def",
			Code: "vim/E600",
			Source: `def Func()
# comment
try
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:1543:31676
		{
			ID:   "src/testdir/test_vim9_script.vim:1543:31676/def",
			Code: "vim/E600",
			Source: `def Func()
# comment
try
echo 0
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:1544:31725
		{
			ID:   "src/testdir/test_vim9_script.vim:1544:31725/def",
			Code: "vim/E600",
			Source: `def Func()
# comment
try
echo 0
catch
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:1545:31783
		{
			ID:   "src/testdir/test_vim9_script.vim:1545:31783/def",
			Code: "vim/E600",
			Source: `def Func()
# comment
try
echo 0
catch
echo 1
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:1546:31851
		{
			ID:   "src/testdir/test_vim9_script.vim:1546:31851/def",
			Code: "vim/E600",
			Source: `def Func()
# comment
try
echo 0
catch
echo 1
finally
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:1547:31930
		{
			ID:   "src/testdir/test_vim9_script.vim:1547:31930/def",
			Code: "vim/E600",
			Source: `def Func()
# comment
try
echo 0
catch
echo 1
finally
echo 2
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:1561:32219
		{
			ID:   "src/testdir/test_vim9_script.vim:1561:32219/def",
			Code: "vim/E600",
			Source: `def Func()
# comment
try
  echo 0
catch
  echo 1
try
finally
  echo 2
endtry
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:1562:32276
		{
			ID:   "src/testdir/test_vim9_script.vim:1562:32276/def",
			Code: "vim/E600",
			Source: `def Func()
# comment
try
  echo 0
catch
  echo 1
try
echo 10
finally
  echo 2
endtry
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:1563:32344
		{
			ID:   "src/testdir/test_vim9_script.vim:1563:32344/def",
			Code: "vim/E600",
			Source: `def Func()
# comment
try
  echo 0
catch
  echo 1
try
echo 10
catch
finally
  echo 2
endtry
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:1564:32421
		{
			ID:   "src/testdir/test_vim9_script.vim:1564:32421/def",
			Code: "vim/E600",
			Source: `def Func()
# comment
try
  echo 0
catch
  echo 1
try
echo 10
catch
echo 11
finally
  echo 2
endtry
#comment
enddef
defcompile
`,
		},

		// E602
		// Vim: src/testdir/test_vim9_script.vim:1537:31369
		{
			ID:   "src/testdir/test_vim9_script.vim:1537:31369/def",
			Code: "vim/E602",
			Source: `def Func()
# comment
endtry
#comment
enddef
defcompile
`,
		},

		// E603
		// Vim: src/testdir/test_vim9_script.vim:1532:31072
		{
			ID:   "src/testdir/test_vim9_script.vim:1532:31072/def",
			Code: "vim/E603",
			Source: `def Func()
# comment
catch
#comment
enddef
defcompile
`,
		},

		// E606
		// Vim: src/testdir/test_vim9_script.vim:1535:31245
		{
			ID:   "src/testdir/test_vim9_script.vim:1535:31245/def",
			Code: "vim/E606",
			Source: `def Func()
# comment
finally
#comment
enddef
defcompile
`,
		},

		// E607
		// Vim: src/testdir/test_vim9_script.vim:1536:31288
		{
			ID:   "src/testdir/test_vim9_script.vim:1536:31288/def",
			Code: "vim/E607",
			Source: `def Func()
# comment
try
echo 0
finally
echo 1
finally
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:1565:32509
		{
			ID:   "src/testdir/test_vim9_script.vim:1565:32509/def",
			Code: "vim/E607",
			Source: `def Func()
# comment
try
  echo 0
catch
  echo 1
try
echo 10
catch
echo 11
finally
finally
  echo 2
endtry
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:1566:32608
		{
			ID:   "src/testdir/test_vim9_script.vim:1566:32608/def",
			Code: "vim/E607",
			Source: `def Func()
# comment
try
  echo 0
catch
  echo 1
try
echo 10
catch
echo 11
finally
echo 12
finally
  echo 2
endtry
#comment
enddef
defcompile
`,
		},

		// E611
		// Vim: src/testdir/test_vim9_expr.vim:2056:61205
		{
			ID:   "src/testdir/test_vim9_expr.vim:2056:61205/vim9-script",
			Code: "vim/E611",
			Source: `vim9script
var x = 1 + v:none
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2057:61288
		{
			ID:   "src/testdir/test_vim9_expr.vim:2057:61288/vim9-script",
			Code: "vim/E611",
			Source: `vim9script
var x = 1 + v:null
`,
		},

		// E689
		// Vim: src/testdir/test_vim9_assign.vim:1850:47247
		{
			ID:   "src/testdir/test_vim9_assign.vim:1850:47247/vim9-script",
			Code: "vim/E689",
			Source: `vim9script
var s = 'abc'
s[1] += 'x'
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1856:47375
		{
			ID:   "src/testdir/test_vim9_assign.vim:1856:47375/vim9-script",
			Code: "vim/E689",
			Source: `vim9script
var s = 'abc'
s[1] ..= 'x'
`,
		},

		// E690
		// Vim: src/testdir/test_vim9_script.vim:3070:64343
		{
			ID:   "src/testdir/test_vim9_script.vim:3070:64343/def",
			Code: "vim/E690",
			Source: `def Func()
# comment
for # in range(5)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3071:64405
		{
			ID:   "src/testdir/test_vim9_script.vim:3071:64405/def",
			Code: "vim/E690",
			Source: `def Func()
# comment
for i In range(5)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3067:64156
		{
			ID:   "src/testdir/test_vim9_script.vim:3067:64156/vim9-script",
			Code: "vim/E690",
			Source: "vim9script\n" +
				"for \n",
		},
		// Vim: src/testdir/test_vim9_script.vim:3068:64217
		{
			ID:   "src/testdir/test_vim9_script.vim:3068:64217/vim9-script",
			Code: "vim/E690",
			Source: `vim9script
for x
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3070:64343
		{
			ID:   "src/testdir/test_vim9_script.vim:3070:64343/vim9-script",
			Code: "vim/E690",
			Source: `vim9script
for # in range(5)
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3071:64405
		{
			ID:   "src/testdir/test_vim9_script.vim:3071:64405/vim9-script",
			Code: "vim/E690",
			Source: `vim9script
for i In range(5)
`,
		},

		// E696
		// Vim: src/testdir/test_vim9_expr.vim:2794:83473
		{
			ID:   "src/testdir/test_vim9_expr.vim:2794:83473/def",
			Code: "vim/E696",
			Source: `def Func()
# comment
var Fx = (a) => [0
 1]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2795:83546
		{
			ID:   "src/testdir/test_vim9_expr.vim:2795:83546/def",
			Code: "vim/E696",
			Source: `def Func()
# comment
var l = [1 2]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2967:88763
		{
			ID:   "src/testdir/test_vim9_expr.vim:2967:88763/def",
			Code: "vim/E696",
			Source: `def Func()
# comment
var Fx = (a) => [0
 1]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2794:83473
		{
			ID:   "src/testdir/test_vim9_expr.vim:2794:83473/vim9-script",
			Code: "vim/E696",
			Source: `vim9script
var Fx = (a) => [0
 1]
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2795:83546
		{
			ID:   "src/testdir/test_vim9_expr.vim:2795:83546/vim9-script",
			Code: "vim/E696",
			Source: `vim9script
var l = [1 2]
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2967:88763
		{
			ID:   "src/testdir/test_vim9_expr.vim:2967:88763/vim9-script",
			Code: "vim/E696",
			Source: `vim9script
var Fx = (a) => [0
 1]
`,
		},

		// E697
		// Vim: src/testdir/test_vim9_expr.vim:4165:120494
		{
			ID:   "src/testdir/test_vim9_expr.vim:4165:120494/def",
			Code: "vim/E697",
			Source: `def Func()
# comment
var x = [1, 2
#comment
enddef
defcompile
`,
		},

		// E701
		// Vim: src/testdir/test_vim9_builtin.vim:2621:120494
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2621:120494/vim9-script",
			Code: "vim/E701",
			Source: `vim9script
len(true)
`,
		},

		// E703
		// Vim: src/testdir/test_listdict.vim:1525:47837
		{
			ID:   "src/testdir/test_listdict.vim:1525:47837/vim9-script",
			Code: "vim/E703",
			Source: `vim9script
var v = [4, 6][() => 1]
`,
		},

		// E704
		// Vim: src/testdir/test_vim9_assign.vim:2977:73523
		{
			ID:   "src/testdir/test_vim9_assign.vim:2977:73523/def",
			Code: "vim/E704",
			Source: `def Func()
# comment
var len = (s: string): number => len(s) + 1
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:72:1795
		{
			ID:   "src/testdir/test_vim9_assign.vim:72:1795/def",
			Code: "vim/E704",
			Source: `def Func()
# comment
var lambda = () => "lambda"
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:2876:65292
		{
			ID:   "src/testdir/test_vim9_func.vim:2876:65292/def",
			Code: "vim/E704",
			Source: `def Func()
# comment
var ref1: func()
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2977:73523
		{
			ID:   "src/testdir/test_vim9_assign.vim:2977:73523/vim9-script",
			Code: "vim/E704",
			Source: `vim9script
var len = (s: string): number => len(s) + 1
`,
		},

		// E716
		// Vim: src/testdir/test_vim9_expr.vim:3161:94986
		{
			ID:   "src/testdir/test_vim9_expr.vim:3161:94986/vim9-script",
			Code: "vim/E716",
			Source: `vim9script
var x = { "a#b": 1 }
x.a#b
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3162:95074
		{
			ID:   "src/testdir/test_vim9_expr.vim:3162:95074/vim9-script",
			Code: "vim/E716",
			Source: `vim9script
var x = { "a:b": 1 }
x.a:b
`,
		},

		// E720
		// Vim: src/testdir/test_vim9_expr.vim:2791:83327
		{
			ID:   "src/testdir/test_vim9_expr.vim:2791:83327/def",
			Code: "vim/E720",
			Source: `def Func()
# comment
var Fx = (a) => ({k1: 0,
 k2 1})
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2965:88679
		{
			ID:   "src/testdir/test_vim9_expr.vim:2965:88679/def",
			Code: "vim/E720",
			Source: `def Func()
# comment
var Fx = (a) => ({k1: 0,
 k2 1})
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3135:93431
		{
			ID:   "src/testdir/test_vim9_expr.vim:3135:93431/def",
			Code: "vim/E720",
			Source: `def Func()
# comment
var x = {xxx}
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:3539:81179
		{
			ID:   "src/testdir/test_vim9_func.vim:3539:81179/def",
			Code: "vim/E720",
			Source: `def Func()
# comment
echo {x -> 'hello ' .. x}('foo')
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2791:83327
		{
			ID:   "src/testdir/test_vim9_expr.vim:2791:83327/vim9-script",
			Code: "vim/E720",
			Source: `vim9script
var Fx = (a) => ({k1: 0,
 k2 1})
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2965:88679
		{
			ID:   "src/testdir/test_vim9_expr.vim:2965:88679/vim9-script",
			Code: "vim/E720",
			Source: `vim9script
var Fx = (a) => ({k1: 0,
 k2 1})
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3135:93431
		{
			ID:   "src/testdir/test_vim9_expr.vim:3135:93431/vim9-script",
			Code: "vim/E720",
			Source: `vim9script
var x = {xxx}
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:3539:81179
		{
			ID:   "src/testdir/test_vim9_func.vim:3539:81179/vim9-script",
			Code: "vim/E720",
			Source: `vim9script
echo {x -> 'hello ' .. x}('foo')
`,
		},

		// E721
		// Vim: src/testdir/test_vim9_expr.vim:3140:93775
		{
			ID:   "src/testdir/test_vim9_expr.vim:3140:93775/def",
			Code: "vim/E721",
			Source: `def Func()
# comment
var x = {a: 1, a: 2}
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3140:93775
		{
			ID:   "src/testdir/test_vim9_expr.vim:3140:93775/vim9-script",
			Code: "vim/E721",
			Source: `vim9script
var x = {a: 1, a: 2}
`,
		},

		// E722
		// Vim: src/testdir/test_vim9_expr.vim:2790:83244
		{
			ID:   "src/testdir/test_vim9_expr.vim:2790:83244/def",
			Code: "vim/E722",
			Source: `def Func()
# comment
var Fx = (a) => ({k1: 0
 k2: 1})
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2964:88596
		{
			ID:   "src/testdir/test_vim9_expr.vim:2964:88596/def",
			Code: "vim/E722",
			Source: `def Func()
# comment
var Fx = (a) => ({k1: 0
 k2: 1})
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3136:93492
		{
			ID:   "src/testdir/test_vim9_expr.vim:3136:93492/def",
			Code: "vim/E722",
			Source: `def Func()
# comment
var x = {xxx: 1
var y = 2
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2790:83244
		{
			ID:   "src/testdir/test_vim9_expr.vim:2790:83244/vim9-script",
			Code: "vim/E722",
			Source: `vim9script
var Fx = (a) => ({k1: 0
 k2: 1})
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2964:88596
		{
			ID:   "src/testdir/test_vim9_expr.vim:2964:88596/vim9-script",
			Code: "vim/E722",
			Source: `vim9script
var Fx = (a) => ({k1: 0
 k2: 1})
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3136:93492
		{
			ID:   "src/testdir/test_vim9_expr.vim:3136:93492/vim9-script",
			Code: "vim/E722",
			Source: `vim9script
var x = {xxx: 1
var y = 2
`,
		},

		// E723
		// Vim: src/testdir/test_vim9_expr.vim:3137:93568
		{
			ID:   "src/testdir/test_vim9_expr.vim:3137:93568/def",
			Code: "vim/E723",
			Source: `def Func()
# comment
var x = {xxx: 1,
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3156:94730
		{
			ID:   "src/testdir/test_vim9_expr.vim:3156:94730/def",
			Code: "vim/E723",
			Source: `def Func()
# comment
var x = ({
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3265:97250
		{
			ID:   "src/testdir/test_vim9_expr.vim:3265:97250/def",
			Code: "vim/E723",
			Source: `def Func()
# comment
var d = {'a':
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3274:97438
		{
			ID:   "src/testdir/test_vim9_expr.vim:3274:97438/def",
			Code: "vim/E723",
			Source: `def Func()
# comment
def Func()
  var d = {'a':
enddef
defcompile
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4195:122174
		{
			ID:   "src/testdir/test_vim9_expr.vim:4195:122174/def",
			Code: "vim/E723",
			Source: `def Func()
# comment
{a: 1->len()
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3274:97438
		{
			ID:   "src/testdir/test_vim9_expr.vim:3274:97438/vim9-script",
			Code: "vim/E723",
			Source: `vim9script
def Func()
  var d = {'a':
enddef
defcompile
`,
		},

		// E728
		// Vim: src/testdir/test_vim9_expr.vim:1879:55648
		{
			ID:   "src/testdir/test_vim9_expr.vim:1879:55648/vim9-script",
			Code: "vim/E728",
			Source: `vim9script
echo {} - 22
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2279:67869
		{
			ID:   "src/testdir/test_vim9_expr.vim:2279:67869/vim9-script",
			Code: "vim/E728",
			Source: `vim9script
var x = {one: 1} * {two: 2}
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2280:67961
		{
			ID:   "src/testdir/test_vim9_expr.vim:2280:67961/vim9-script",
			Code: "vim/E728",
			Source: `vim9script
var x = {one: 1} / {two: 2}
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2281:68053
		{
			ID:   "src/testdir/test_vim9_expr.vim:2281:68053/vim9-script",
			Code: "vim/E728",
			Source: `vim9script
var x = {one: 1} % {two: 2}
`,
		},

		// E729
		// Vim: src/testdir/test_vim9_expr.vim:1967:57699
		{
			ID:   "src/testdir/test_vim9_expr.vim:1967:57699/vim9-script",
			Code: "vim/E729",
			Source: `vim9script
echo 'a' .. function('len')
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2053:61007
		{
			ID:   "src/testdir/test_vim9_expr.vim:2053:61007/vim9-script",
			Code: "vim/E729",
			Source: `vim9script
var x = 'a' .. function('len')
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2054:61102
		{
			ID:   "src/testdir/test_vim9_expr.vim:2054:61102/vim9-script",
			Code: "vim/E729",
			Source: `vim9script
var x = 'a' .. function('len', ['a'])
`,
		},

		// E730
		// Vim: src/testdir/test_vim9_builtin.vim:3833:186072
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3833:186072/vim9-script",
			Code: "vim/E730",
			Source: `vim9script
search("a", "", 9, 0, [0])
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3986:192132
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3986:192132/vim9-script",
			Code: "vim/E730",
			Source: `vim9script
searchpos("a", "", 9, 0, [0])
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1947:57231
		{
			ID:   "src/testdir/test_vim9_expr.vim:1947:57231/vim9-script",
			Code: "vim/E730",
			Source: `vim9script
echo 'a' .. [1]
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2049:60663
		{
			ID:   "src/testdir/test_vim9_expr.vim:2049:60663/vim9-script",
			Code: "vim/E730",
			Source: `vim9script
var x = 'a' .. [1]
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3154:94648
		{
			ID:   "src/testdir/test_vim9_expr.vim:3154:94648/vim9-script",
			Code: "vim/E730",
			Source: `vim9script
var x = {[[1, 2]]: 0}
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4174:120932
		{
			ID:   "src/testdir/test_vim9_expr.vim:4174:120932/vim9-script",
			Code: "vim/E730",
			Source: `vim9script
&grepprg = [343]
`,
		},

		// E731
		// Vim: src/testdir/test_vim9_expr.vim:1952:57345
		{
			ID:   "src/testdir/test_vim9_expr.vim:1952:57345/vim9-script",
			Code: "vim/E731",
			Source: `vim9script
echo 'a' .. {a: 1}
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2050:60746
		{
			ID:   "src/testdir/test_vim9_expr.vim:2050:60746/vim9-script",
			Code: "vim/E731",
			Source: `vim9script
var x = 'a' .. {a: 1}
`,
		},

		// E734
		// Vim: src/testdir/test_vim9_assign.vim:282:6765
		{
			ID:   "src/testdir/test_vim9_assign.vim:282:6765/vim9-script",
			Code: "vim/E734",
			Source: `vim9script
var s = '-'
s ..= [1, 2]
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:287:6943
		{
			ID:   "src/testdir/test_vim9_assign.vim:287:6943/vim9-script",
			Code: "vim/E734",
			Source: `vim9script
var s = '-'
s ..= {a: 2}
`,
		},

		// E745
		// Vim: src/testdir/test_vim9_expr.vim:1884:55756
		{
			ID:   "src/testdir/test_vim9_expr.vim:1884:55756/vim9-script",
			Code: "vim/E745",
			Source: `vim9script
echo [] - 33
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2045:60408
		{
			ID:   "src/testdir/test_vim9_expr.vim:2045:60408/vim9-script",
			Code: "vim/E745",
			Source: `vim9script
var x = [3] + 0z1122
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2275:67622
		{
			ID:   "src/testdir/test_vim9_expr.vim:2275:67622/vim9-script",
			Code: "vim/E745",
			Source: `vim9script
var x = [1] * [2]
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2276:67704
		{
			ID:   "src/testdir/test_vim9_expr.vim:2276:67704/vim9-script",
			Code: "vim/E745",
			Source: `vim9script
var x = [1] / [2]
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2277:67786
		{
			ID:   "src/testdir/test_vim9_expr.vim:2277:67786/vim9-script",
			Code: "vim/E745",
			Source: `vim9script
var x = [1] % [2]
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:686:18748
		{
			ID:   "src/testdir/test_vim9_expr.vim:686:18748/vim9-script",
			Code: "vim/E745",
			Source: `vim9script
var x = [] || false
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:696:19283
		{
			ID:   "src/testdir/test_vim9_expr.vim:696:19283/vim9-script",
			Code: "vim/E745",
			Source: `vim9script
var x = [] || false
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:697:19409
		{
			ID:   "src/testdir/test_vim9_expr.vim:697:19409/vim9-script",
			Code: "vim/E745",
			Source: `vim9script
var x = $'{false || []}'
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:861:23660
		{
			ID:   "src/testdir/test_vim9_expr.vim:861:23660/vim9-script",
			Code: "vim/E745",
			Source: `vim9script
var x = $'{true && []}'
`,
		},

		// E804
		// Vim: src/testdir/test_vim9_expr.vim:2294:68582
		{
			ID:   "src/testdir/test_vim9_expr.vim:2294:68582/vim9-script",
			Code: "vim/E804",
			Source: `vim9script
var x = 1.0 % 2
`,
		},

		// E805
		// Vim: src/testdir/test_vim9_expr.vim:206:6185
		{
			ID:   "src/testdir/test_vim9_expr.vim:206:6185/def",
			Code: "vim/E805",
			Source: `def Func()
# comment
var x = 0.1 ? 'one' : 'two'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1350:59140
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1350:59140/vim9-script",
			Code: "vim/E805",
			Source: `vim9script
extendnew(0z0102, 0z03, 1.1)
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:206:6185
		{
			ID:   "src/testdir/test_vim9_expr.vim:206:6185/vim9-script",
			Code: "vim/E805",
			Source: `vim9script
var x = 0.1 ? 'one' : 'two'
`,
		},

		// E806
		// Vim: src/testdir/test_vim9_expr.vim:2284:68227
		{
			ID:   "src/testdir/test_vim9_expr.vim:2284:68227/vim9-script",
			Code: "vim/E806",
			Source: `vim9script
var x = 0.7[1]
`,
		},

		// E896
		// Vim: src/testdir/test_vim9_builtin.vim:1190:52918
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1190:52918/vim9-script",
			Code: "vim/E896",
			Source: `vim9script
extend("a", 1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1191:53112
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1191:53112/vim9-script",
			Code: "vim/E896",
			Source: `vim9script
extend([1, 2], 3)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1344:57886
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1344:57886/vim9-script",
			Code: "vim/E896",
			Source: `vim9script
extendnew({a: 1}, 42)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1345:58093
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1345:58093/vim9-script",
			Code: "vim/E896",
			Source: `vim9script
extendnew({a: 1}, [42])
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1346:58308
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1346:58308/vim9-script",
			Code: "vim/E896",
			Source: `vim9script
extendnew([1, 2], "x")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1347:58516
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1347:58516/vim9-script",
			Code: "vim/E896",
			Source: `vim9script
extendnew([1, 2], {x: 1})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1348:58733
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1348:58733/vim9-script",
			Code: "vim/E896",
			Source: `vim9script
extendnew(0z0102, "x")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1349:58933
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1349:58933/vim9-script",
			Code: "vim/E896",
			Source: `vim9script
extendnew(0z0102, [42])
`,
		},

		// E908
		// Vim: src/testdir/test_vim9_expr.vim:1957:57464
		{
			ID:   "src/testdir/test_vim9_expr.vim:1957:57464/vim9-script",
			Code: "vim/E908",
			Source: `vim9script
echo 'a' .. test_void()
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1993:58366
		{
			ID:   "src/testdir/test_vim9_expr.vim:1993:58366/vim9-script",
			Code: "vim/E908",
			Source: `vim9script
echo 'a' .. test_null_job()
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1997:58500
		{
			ID:   "src/testdir/test_vim9_expr.vim:1997:58500/vim9-script",
			Code: "vim/E908",
			Source: `vim9script
echo 'a' .. test_null_channel()
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2051:60832
		{
			ID:   "src/testdir/test_vim9_expr.vim:2051:60832/vim9-script",
			Code: "vim/E908",
			Source: `vim9script
var x = 'a' .. test_void()
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2067:61770
		{
			ID:   "src/testdir/test_vim9_expr.vim:2067:61770/vim9-script",
			Code: "vim/E908",
			Source: `vim9script
var x = 'a' .. test_null_job()
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2068:61865
		{
			ID:   "src/testdir/test_vim9_expr.vim:2068:61865/vim9-script",
			Code: "vim/E908",
			Source: `vim9script
var x = 'a' .. test_null_channel()
`,
		},

		// E973
		// Vim: src/testdir/test_vim9_expr.vim:2445:72344
		{
			ID:   "src/testdir/test_vim9_expr.vim:2445:72344/def",
			Code: "vim/E973",
			Source: `def Func()
# comment
var x = 0z123
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2445:72344
		{
			ID:   "src/testdir/test_vim9_expr.vim:2445:72344/vim9-script",
			Code: "vim/E973",
			Source: `vim9script
var x = 0z123
`,
		},

		// E974
		// Vim: src/testdir/test_vim9_expr.vim:194:5444
		{
			ID:   "src/testdir/test_vim9_expr.vim:194:5444/def",
			Code: "vim/E974",
			Source: `def Func()
# comment
var x = 0z1234 ? 'one' : 'two'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4158:120135
		{
			ID:   "src/testdir/test_vim9_expr.vim:4158:120135/def",
			Code: "vim/E974",
			Source: `def Func()
# comment
var x = -0z12
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1889:55868
		{
			ID:   "src/testdir/test_vim9_expr.vim:1889:55868/vim9-script",
			Code: "vim/E974",
			Source: `vim9script
echo 0z1234 - 44
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:194:5444
		{
			ID:   "src/testdir/test_vim9_expr.vim:194:5444/vim9-script",
			Code: "vim/E974",
			Source: `vim9script
var x = 0z1234 ? 'one' : 'two'
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2041:60068
		{
			ID:   "src/testdir/test_vim9_expr.vim:2041:60068/vim9-script",
			Code: "vim/E974",
			Source: `vim9script
var x = 0z1122 + 33
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2042:60152
		{
			ID:   "src/testdir/test_vim9_expr.vim:2042:60152/vim9-script",
			Code: "vim/E974",
			Source: `vim9script
var x = 0z1122 + [3]
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2044:60324
		{
			ID:   "src/testdir/test_vim9_expr.vim:2044:60324/vim9-script",
			Code: "vim/E974",
			Source: `vim9script
var x = 33 + 0z1122
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2271:67369
		{
			ID:   "src/testdir/test_vim9_expr.vim:2271:67369/vim9-script",
			Code: "vim/E974",
			Source: `vim9script
var x = 0z01 * 0z12
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2272:67453
		{
			ID:   "src/testdir/test_vim9_expr.vim:2272:67453/vim9-script",
			Code: "vim/E974",
			Source: `vim9script
var x = 0z01 / 0z12
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4158:120135
		{
			ID:   "src/testdir/test_vim9_expr.vim:4158:120135/vim9-script",
			Code: "vim/E974",
			Source: `vim9script
var x = -0z12
`,
		},

		// E976
		// Vim: src/testdir/test_vim9_expr.vim:1962:57576
		{
			ID:   "src/testdir/test_vim9_expr.vim:1962:57576/vim9-script",
			Code: "vim/E976",
			Source: `vim9script
echo 'a' .. 0z33
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2052:60923
		{
			ID:   "src/testdir/test_vim9_expr.vim:2052:60923/vim9-script",
			Code: "vim/E976",
			Source: `vim9script
var x = 'a' .. 0z32
`,
		},

		// E996
		// Vim: src/testdir/test_vim9_script.vim:272:6669
		{
			ID:   "src/testdir/test_vim9_script.vim:272:6669/def",
			Code: "vim/E996",
			Source: `def Func()
# comment
final &option
#comment
enddef
defcompile
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
