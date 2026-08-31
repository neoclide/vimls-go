package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestOfficialVimCompileCasesE1000(t *testing.T) {
	cases := []struct {
		ID     string
		Code   string
		Source string
	}{
		// E1001
		// Vim: src/testdir/test_expr.vim:895:35674
		{
			ID:   "src/testdir/test_expr.vim:895:35674/def",
			Code: "vim/E1001",
			Source: `def Func()
# comment
var text = substitute ( 'some text' , 't' , 'T' , 'g' )
call assert_equal('some TexT', text)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1618:40130
		{
			ID:   "src/testdir/test_vim9_assign.vim:1618:40130/def",
			Code: "vim/E1001",
			Source: `def Func()
# comment
var xnr = xnr + 1
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:2066:42583
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2066:42583/def",
			Code: "vim/E1001",
			Source: `def Func()
# comment
g.pat.cmd
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2592:77280
		{
			ID:   "src/testdir/test_vim9_expr.vim:2592:77280/def",
			Code: "vim/E1001",
			Source: `def Func()
# comment
var x = g:list_mixed[xxx]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2962:88529
		{
			ID:   "src/testdir/test_vim9_expr.vim:2962:88529/def",
			Code: "vim/E1001",
			Source: `def Func()
# comment
echo 'asdf'->((a) => a)(x)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4316:125455
		{
			ID:   "src/testdir/test_vim9_expr.vim:4316:125455/def",
			Code: "vim/E1001",
			Source: `def Func()
# comment
var d = 'asdf'[1 : xxx]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:983:27726
		{
			ID:   "src/testdir/test_vim9_expr.vim:983:27726/def",
			Code: "vim/E1001",
			Source: `def Func()
# comment
var x = 'a' == xxx
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3463:73778
		{
			ID:   "src/testdir/test_vim9_script.vim:3463:73778/def",
			Code: "vim/E1001",
			Source: `def Func()
# comment
while xxx
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:385:9461
		{
			ID:   "src/testdir/test_vim9_script.vim:385:9461/def",
			Code: "vim/E1001",
			Source: `def Func()
# comment
{
var inner = 1
}
echo inner
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2781:82808
		{
			ID:   "src/testdir/test_vim9_expr.vim:2781:82808/vim9-script",
			Code: "vim/E1001",
			Source: `vim9script
var L = (a) => a + b
`,
		},

		// E1002
		// Vim: src/testdir/test_vim9_expr.vim:3362:99310
		{
			ID:   "src/testdir/test_vim9_expr.vim:3362:99310/def",
			Code: "vim/E1002",
			Source: `def Func()
# comment
var x = g:dict_one.#$!
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3610:106262
		{
			ID:   "src/testdir/test_vim9_expr.vim:3610:106262/def",
			Code: "vim/E1002",
			Source: `def Func()
# comment
var x = $$$
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3611:106332
		{
			ID:   "src/testdir/test_vim9_expr.vim:3611:106332/def",
			Code: "vim/E1002",
			Source: `def Func()
# comment
$
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4162:120367
		{
			ID:   "src/testdir/test_vim9_expr.vim:4162:120367/def",
			Code: "vim/E1002",
			Source: `def Func()
# comment
var x = @
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4162:120367
		{
			ID:   "src/testdir/test_vim9_expr.vim:4162:120367/vim9-script",
			Code: "vim/E1002",
			Source: `vim9script
var x = @
`,
		},

		// E1003
		// Vim: src/testdir/test_vim9_func.vim:2434
		{
			ID:   "src/testdir/test_vim9_func.vim:2434/CheckScriptFailure",
			Code: "vim/E1003",
			Source: `vim9script
def Func(): number
  return
enddef
defcompile
`,
		},

		// E1004
		// Vim: src/testdir/test_expr.vim:1051:41636
		{
			ID:   "src/testdir/test_expr.vim:1051:41636/def",
			Code: "vim/E1004",
			Source: `def Func()
# comment
echo 1<< 2
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2712:67795
		{
			ID:   "src/testdir/test_vim9_assign.vim:2712:67795/def",
			Code: "vim/E1004",
			Source: `def Func()
# comment
var ll = [1, 2]
unlet ll[0 :1]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1934:56850
		{
			ID:   "src/testdir/test_vim9_expr.vim:1934:56850/def",
			Code: "vim/E1004",
			Source: `def Func()
# comment
echo 'a'.. 'b'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2946:87702
		{
			ID:   "src/testdir/test_vim9_expr.vim:2946:87702/def",
			Code: "vim/E1004",
			Source: `def Func()
# comment
var Ref = (a) =>a + 1
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:814:22229
		{
			ID:   "src/testdir/test_vim9_expr.vim:814:22229/def",
			Code: "vim/E1004",
			Source: `def Func()
# comment
var name = v:true&& v:true
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_expr.vim:1051:41636
		{
			ID:   "src/testdir/test_expr.vim:1051:41636/vim9-script",
			Code: "vim/E1004",
			Source: `vim9script
echo 1<< 2
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:131:3508
		{
			ID:   "src/testdir/test_vim9_expr.vim:131:3508/vim9-script",
			Code: "vim/E1004",
			Source: `vim9script
var name = v:true ? 1 :2
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1664:47152
		{
			ID:   "src/testdir/test_vim9_expr.vim:1664:47152/vim9-script",
			Code: "vim/E1004",
			Source: `vim9script
echo 2> 3
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1977:57863
		{
			ID:   "src/testdir/test_vim9_expr.vim:1977:57863/vim9-script",
			Code: "vim/E1004",
			Source: `vim9script
new
['']->setline(1)
/pattern

eval 0
bwipe!
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2946:87702
		{
			ID:   "src/testdir/test_vim9_expr.vim:2946:87702/vim9-script",
			Code: "vim/E1004",
			Source: `vim9script
var Ref = (a) =>a + 1
`,
		},

		// E1005
		// Vim: src/testdir/test_vim9_func.vim:2887:66332
		{
			ID:   "src/testdir/test_vim9_func.vim:2887:66332/def",
			Code: "vim/E1005",
			Source: `def Func()
# comment
var RefWrong: func(bool, bool, bool, bool, bool, bool, bool, bool, bool, bool, bool, bool, bool, bool, bool, bool, bool, bool, bool, bool)
#comment
enddef
defcompile
`,
		},

		// E1006
		// Vim: src/testdir/test_vim9_func.vim:2123:47271
		{
			ID:   "src/testdir/test_vim9_func.vim:2123:47271/def",
			Code: "vim/E1006",
			Source: `def Func()
# comment
def Func(x: number)
  var x = 234
enddef
#comment
enddef
defcompile
`,
		},

		// E1007
		// Vim: src/testdir/test_vim9_func.vim:2078:45879
		{
			ID:   "src/testdir/test_vim9_func.vim:2078:45879/def",
			Code: "vim/E1007",
			Source: `def Func()
# comment
var RefWrong: func(?string, string)
#comment
enddef
defcompile
`,
		},

		// E1008
		// Vim: src/testdir/test_tuple.vim:62:1444
		{
			ID:   "src/testdir/test_tuple.vim:62:1444/def",
			Code: "vim/E1008",
			Source: `def Func()
# comment
var t: tuple<> = ('a', 'b')
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_tuple.vim:67:1600
		{
			ID:   "src/testdir/test_tuple.vim:67:1600/def",
			Code: "vim/E1008",
			Source: `def Func()
# comment
var t: tuple = ('a', 'b')
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_class.vim:11721:265332
		{
			ID:   "src/testdir/test_vim9_class.vim:11721:265332/def",
			Code: "vim/E1008",
			Source: `def Func()
# comment
var x: object
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_tuple.vim:62:1444
		{
			ID:   "src/testdir/test_tuple.vim:62:1444/vim9-script",
			Code: "vim/E1008",
			Source: `vim9script
var t: tuple<> = ('a', 'b')
`,
		},
		// Vim: src/testdir/test_tuple.vim:67:1600
		{
			ID:   "src/testdir/test_tuple.vim:67:1600/vim9-script",
			Code: "vim/E1008",
			Source: `vim9script
var t: tuple = ('a', 'b')
`,
		},
		// Vim: src/testdir/test_vim9_class.vim:11721:265332
		{
			ID:   "src/testdir/test_vim9_class.vim:11721:265332/vim9-script",
			Code: "vim/E1008",
			Source: `vim9script
var x: object
`,
		},

		// E1009
		// Vim: src/testdir/test_vim9_assign.vim:1633:41010
		{
			ID:   "src/testdir/test_vim9_assign.vim:1633:41010/def",
			Code: "vim/E1009",
			Source: `def Func()
# comment
var name: dict<number
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1636:41143
		{
			ID:   "src/testdir/test_vim9_assign.vim:1636:41143/def",
			Code: "vim/E1009",
			Source: `def Func()
# comment
var name: dict<number
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_class.vim:11731:265708
		{
			ID:   "src/testdir/test_vim9_class.vim:11731:265708/def",
			Code: "vim/E1009",
			Source: `def Func()
# comment
var x: object<any
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_class.vim:11736:265882
		{
			ID:   "src/testdir/test_vim9_class.vim:11736:265882/def",
			Code: "vim/E1009",
			Source: `def Func()
# comment
var x: object<any,any>
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:195:4394
		{
			ID:   "src/testdir/test_vim9_script.vim:195:4394/def",
			Code: "vim/E1009",
			Source: `def Func()
# comment
var name: dict<number
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:196:4452
		{
			ID:   "src/testdir/test_vim9_script.vim:196:4452/def",
			Code: "vim/E1009",
			Source: `def Func()
# comment
var name: dict<list<number>
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_class.vim:11731:265708
		{
			ID:   "src/testdir/test_vim9_class.vim:11731:265708/vim9-script",
			Code: "vim/E1009",
			Source: `vim9script
var x: object<any
`,
		},

		// E1010
		// Vim: src/testdir/test_tuple.vim:120:3271
		{
			ID:   "src/testdir/test_tuple.vim:120:3271/def",
			Code: "vim/E1010",
			Source: `def Func()
# comment
var t: tuple<number, ...> = (1, 2, 3)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2842:84650
		{
			ID:   "src/testdir/test_vim9_expr.vim:2842:84650/def",
			Code: "vim/E1010",
			Source: `def Func()
# comment
var Func = (nr: int) => {
        echo nr
      }
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:193:4327
		{
			ID:   "src/testdir/test_vim9_script.vim:193:4327/def",
			Code: "vim/E1010",
			Source: `def Func()
# comment
var name: dict<dict<nothing>>
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:202:4721
		{
			ID:   "src/testdir/test_vim9_script.vim:202:4721/def",
			Code: "vim/E1010",
			Source: `def Func()
# comment
var name: freddy
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:208:5027
		{
			ID:   "src/testdir/test_vim9_script.vim:208:5027/def",
			Code: "vim/E1010",
			Source: `def Func()
# comment
var name: vim
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_tuple.vim:120:3271
		{
			ID:   "src/testdir/test_tuple.vim:120:3271/vim9-script",
			Code: "vim/E1010",
			Source: `vim9script
var t: tuple<number, ...> = (1, 2, 3)
`,
		},
		// Vim: src/testdir/test_tuple.vim:167:4773
		{
			ID:   "src/testdir/test_tuple.vim:167:4773/vim9-script",
			Code: "vim/E1010",
			Source: `vim9script
var t: tupel<number> = (1,)
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2776:82545
		{
			ID:   "src/testdir/test_vim9_expr.vim:2776:82545/vim9-script",
			Code: "vim/E1010",
			Source: `vim9script
var Ref = (a: int) => a + 1
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2777:82618
		{
			ID:   "src/testdir/test_vim9_expr.vim:2777:82618/vim9-script",
			Code: "vim/E1010",
			Source: `vim9script
var Ref = (a): int => a + 1
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2849:84806
		{
			ID:   "src/testdir/test_vim9_expr.vim:2849:84806/vim9-script",
			Code: "vim/E1010",
			Source: `vim9script
var Func = (nr: number): int => {
        return nr
      }
`,
		},

		// E1011
		// Vim: src/testdir/test_vim9_expr.vim:4498:130604
		{
			ID:   "src/testdir/test_vim9_expr.vim:4498:130604/def",
			Code: "vim/E1011",
			Source: `def Func()
# comment
echo Func01234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789()
#comment
enddef
defcompile
`,
		},

		// E1012
		// Vim: src/testdir/test_listdict.vim:222:5865
		{
			ID:   "src/testdir/test_listdict.vim:222:5865/def",
			Code: "vim/E1012",
			Source: `def Func()
# comment
var l = [7]
l[:] = ['text']
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:58:1382
		{
			ID:   "src/testdir/test_vim9_assign.vim:58:1382/def",
			Code: "vim/E1012",
			Source: `def Func()
# comment
var x: bool = "x"
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:5066:256343
		{
			ID:   "src/testdir/test_vim9_builtin.vim:5066:256343/def",
			Code: "vim/E1012",
			Source: `def Func()
# comment
var l: list<number> = uniq(["a", "b"])
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2344:69799
		{
			ID:   "src/testdir/test_vim9_expr.vim:2344:69799/def",
			Code: "vim/E1012",
			Source: `def Func()
# comment
var x = <number>string(1)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3100:65536
		{
			ID:   "src/testdir/test_vim9_script.vim:3100:65536/def",
			Code: "vim/E1012",
			Source: `def Func()
# comment
for nr: number in ['foo']
endfor
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_listdict.vim:222:5865
		{
			ID:   "src/testdir/test_listdict.vim:222:5865/vim9-script",
			Code: "vim/E1012",
			Source: `vim9script
var l = [7]
l[:] = ['text']
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:58:1382
		{
			ID:   "src/testdir/test_vim9_assign.vim:58:1382/vim9-script",
			Code: "vim/E1012",
			Source: `vim9script
var x: bool = "x"
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3498:170078
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3498:170078/vim9-script",
			Code: "vim/E1012",
			Source: `vim9script
var read: dict<string> = readfile('Xreadfile')
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2685:79954
		{
			ID:   "src/testdir/test_vim9_expr.vim:2685:79954/vim9-script",
			Code: "vim/E1012",
			Source: `vim9script
var l: list<string> = [234, 'x']
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3100:65536
		{
			ID:   "src/testdir/test_vim9_script.vim:3100:65536/vim9-script",
			Code: "vim/E1012",
			Source: `vim9script
for nr: number in ['foo']
endfor
`,
		},

		// E1013
		// Vim: src/testdir/test_float_func.vim:247:9921
		{
			ID:   "src/testdir/test_float_func.vim:247:9921/def",
			Code: "vim/E1013",
			Source: `def Func()
# comment
str2float(1.2)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1584:72595
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1584:72595/def",
			Code: "vim/E1013",
			Source: `def Func()
# comment
var d = {a: 1}
filter(d, (i: number, v: number) => true)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3075:143842
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3075:143842/def",
			Code: "vim/E1013",
			Source: `def Func()
# comment
matchstr(0z12, "p")
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4242:206916
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4242:206916/def",
			Code: "vim/E1013",
			Source: `def Func()
# comment
sha256(100)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:1600:34740
		{
			ID:   "src/testdir/test_vim9_func.vim:1600:34740/def",
			Code: "vim/E1013",
			Source: `def Func()
# comment
var Ref = (x: number, y: number) => x + y
echo Ref(1, 'x')
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_tuple.vim:1069:29610
		{
			ID:   "src/testdir/test_tuple.vim:1069:29610/vim9-script",
			Code: "vim/E1013",
			Source: `vim9script
var nll: tuple<list<number>> = ([1, 2],)
nll->copy()[0]->extend(['x'])
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1232:55119
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1232:55119/vim9-script",
			Code: "vim/E1013",
			Source: `vim9script
var d: dict<bool>
extend(d, {b: 0})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1584:72595
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1584:72595/vim9-script",
			Code: "vim/E1013",
			Source: `vim9script
var d = {a: 1}
filter(d, (i: number, v: number) => true)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1606:73899
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1606:73899/vim9-script",
			Code: "vim/E1013",
			Source: `vim9script
var d = {a: 1}
filter(d, (i: string, v: string) => true)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4429:218134
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4429:218134/vim9-script",
			Code: "vim/E1013",
			Source: `vim9script
var l = ['a', 'b', 'c']
sort(l, (a: string, b: number) => 1)
`,
		},

		// E1016
		// Vim: src/testdir/test_vim9_assign.vim:1592:38807
		{
			ID:   "src/testdir/test_vim9_assign.vim:1592:38807/def",
			Code: "vim/E1016",
			Source: `def Func()
# comment
var $VAR = 5
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1610:39612
		{
			ID:   "src/testdir/test_vim9_assign.vim:1610:39612/def",
			Code: "vim/E1016",
			Source: `def Func()
# comment
var g:var = 5
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1612:39780
		{
			ID:   "src/testdir/test_vim9_assign.vim:1612:39780/def",
			Code: "vim/E1016",
			Source: `def Func()
# comment
var b:var = 5
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1613:39864
		{
			ID:   "src/testdir/test_vim9_assign.vim:1613:39864/def",
			Code: "vim/E1016",
			Source: `def Func()
# comment
var t:var = 5
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1638:41202
		{
			ID:   "src/testdir/test_vim9_assign.vim:1638:41202/def",
			Code: "vim/E1016",
			Source: `def Func()
# comment
w:foo: number = 10
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1642:41425
		{
			ID:   "src/testdir/test_vim9_assign.vim:1642:41425/def",
			Code: "vim/E1016",
			Source: `def Func()
# comment
b:foo: string = "x"
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1644:41539
		{
			ID:   "src/testdir/test_vim9_assign.vim:1644:41539/def",
			Code: "vim/E1016",
			Source: `def Func()
# comment
g:foo: number = 123
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:217:5470
		{
			ID:   "src/testdir/test_vim9_assign.vim:217:5470/def",
			Code: "vim/E1016",
			Source: `def Func()
# comment
var $VAR: number
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4492:130279
		{
			ID:   "src/testdir/test_vim9_expr.vim:4492:130279/def",
			Code: "vim/E1016",
			Source: `def Func()
# comment
var v:statusmsg = ''
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4492:130279
		{
			ID:   "src/testdir/test_vim9_expr.vim:4492:130279/vim9-script",
			Code: "vim/E1016",
			Source: `vim9script
var v:statusmsg = ''
`,
		},

		// E1017
		// Vim: src/testdir/test_vim9_assign.vim:1208:28746
		{
			ID:   "src/testdir/test_vim9_assign.vim:1208:28746/def",
			Code: "vim/E1017",
			Source: `def Func()
# comment
var dd = {one: 1}
var dd.one = 2
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:269:6478
		{
			ID:   "src/testdir/test_vim9_script.vim:269:6478/def",
			Code: "vim/E1017",
			Source: `def Func()
# comment
final one = 234
var one = 99
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:270:6546
		{
			ID:   "src/testdir/test_vim9_script.vim:270:6546/def",
			Code: "vim/E1017",
			Source: `def Func()
# comment
final list = [1, 2]
var list = [3, 4]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3072:64467
		{
			ID:   "src/testdir/test_vim9_script.vim:3072:64467/def",
			Code: "vim/E1017",
			Source: `def Func()
# comment
var x = 5
for x in range(5)
endfor
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1208:28746
		{
			ID:   "src/testdir/test_vim9_assign.vim:1208:28746/vim9-script",
			Code: "vim/E1017",
			Source: `vim9script
var dd = {one: 1}
var dd.one = 2
`,
		},

		// E1018
		// Vim: src/testdir/test_vim9_script.vim:268:6412
		{
			ID:   "src/testdir/test_vim9_script.vim:268:6412/def",
			Code: "vim/E1018",
			Source: `def Func()
# comment
final name = 234
name = 99
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3094:65403
		{
			ID:   "src/testdir/test_vim9_script.vim:3094:65403/def",
			Code: "vim/E1018",
			Source: `def Func()
# comment
var d: list<dict<any>> = [{a: 0}]
for e in d
  e = {a: 0, b: ''}
endfor
#comment
enddef
defcompile
`,
		},

		// E1019
		// Vim: src/testdir/test_vim9_assign.vim:1615:39946
		{
			ID:   "src/testdir/test_vim9_assign.vim:1615:39946/def",
			Code: "vim/E1019",
			Source: `def Func()
# comment
var anr = 4
anr ..= "text"
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:189:4382
		{
			ID:   "src/testdir/test_vim9_assign.vim:189:4382/def",
			Code: "vim/E1019",
			Source: `def Func()
# comment
&ts ..= "xxx"
#comment
enddef
defcompile
`,
		},

		// E1020
		// Vim: src/testdir/test_vim9_assign.vim:1616:40012
		{
			ID:   "src/testdir/test_vim9_assign.vim:1616:40012/def",
			Code: "vim/E1020",
			Source: `def Func()
# comment
var xnr += 4
#comment
enddef
defcompile
`,
		},

		// E1021
		// Vim: src/testdir/test_vim9_assign.vim:2313:57945
		{
			ID:   "src/testdir/test_vim9_assign.vim:2313:57945/def",
			Code: "vim/E1021",
			Source: `def Func()
# comment
const foo: number
#comment
enddef
defcompile
`,
		},

		// E1022
		// Vim: src/testdir/test_vim9_assign.vim:1587:38586
		{
			ID:   "src/testdir/test_vim9_assign.vim:1587:38586/def",
			Code: "vim/E1022",
			Source: `def Func()
# comment
var somevar
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:198:4982
		{
			ID:   "src/testdir/test_vim9_assign.vim:198:4982/def",
			Code: "vim/E1022",
			Source: `def Func()
# comment
&ts = 3
var asdf
#comment
enddef
defcompile
`,
		},

		// E1023
		// Vim: src/testdir/test_vim9_expr.vim:693:19150
		{
			ID:   "src/testdir/test_vim9_expr.vim:693:19150/def",
			Code: "vim/E1023",
			Source: `def Func()
# comment
if 3
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:690:18991
		{
			ID:   "src/testdir/test_vim9_expr.vim:690:18991/vim9-script",
			Code: "vim/E1023",
			Source: `vim9script
var x = 3 || false
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:691:19070
		{
			ID:   "src/testdir/test_vim9_expr.vim:691:19070/vim9-script",
			Code: "vim/E1023",
			Source: `vim9script
var x = false || 3
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:693:19150
		{
			ID:   "src/testdir/test_vim9_expr.vim:693:19150/vim9-script",
			Code: "vim/E1023",
			Source: `vim9script
if 3
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:839:23100
		{
			ID:   "src/testdir/test_vim9_expr.vim:839:23100/vim9-script",
			Code: "vim/E1023",
			Source: `vim9script
if 3
    && true
endif
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:846:23231
		{
			ID:   "src/testdir/test_vim9_expr.vim:846:23231/vim9-script",
			Code: "vim/E1023",
			Source: `vim9script
if true
    && 3
endif
`,
		},

		// E1024
		// Vim: src/testdir/test_vim9_builtin.vim:1564:71682
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1564:71682/vim9-script",
			Code: "vim/E1024",
			Source: `vim9script
filter([1, 2], 4)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2715:125870
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2715:125870/vim9-script",
			Code: "vim/E1024",
			Source: `vim9script
map([1, 2], 4)
`,
		},

		// E1025
		// Vim: src/testdir/test_vim9_script.vim:386:9535
		{
			ID:   "src/testdir/test_vim9_script.vim:386:9535/def",
			Code: "vim/E1025",
			Source: `def Func()
# comment
}
#comment
enddef
defcompile
`,
		},

		// E1026
		// Vim: src/testdir/test_vim9_script.vim:387:9573
		{
			ID:   "src/testdir/test_vim9_script.vim:387:9573/def",
			Code: "vim/E1026",
			Source: `def Func()
# comment
{
echo 1
#comment
enddef
defcompile
`,
		},

		// E1027
		// Vim: src/testdir/test_vim9_func.vim:520:10717
		{
			ID:   "src/testdir/test_vim9_func.vim:520:10717/def",
			Code: "vim/E1027",
			Source: `def Func()
# comment
def Missing(): number
  if g:cond
    echo "no return"
  else
    return 0
  endif
enddef
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:527:10975
		{
			ID:   "src/testdir/test_vim9_func.vim:527:10975/def",
			Code: "vim/E1027",
			Source: `def Func()
# comment
def Missing(): number
  if g:cond
    return 1
  else
    echo "no return"
  endif
enddef
#comment
enddef
defcompile
`,
		},

		// E1030
		// Vim: src/testdir/test_vim9_expr.vim:4156:120001
		{
			ID:   "src/testdir/test_vim9_expr.vim:4156:120001/def",
			Code: "vim/E1030",
			Source: `def Func()
# comment
var x = -'xx'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4157:120068
		{
			ID:   "src/testdir/test_vim9_expr.vim:4157:120068/def",
			Code: "vim/E1030",
			Source: `def Func()
# comment
var x = +'xx'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2046:60493
		{
			ID:   "src/testdir/test_vim9_expr.vim:2046:60493/vim9-script",
			Code: "vim/E1030",
			Source: `vim9script
var x = 'asdf' + 0z1122
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2267:67119
		{
			ID:   "src/testdir/test_vim9_expr.vim:2267:67119/vim9-script",
			Code: "vim/E1030",
			Source: `vim9script
var x = '1' * '2'
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2268:67202
		{
			ID:   "src/testdir/test_vim9_expr.vim:2268:67202/vim9-script",
			Code: "vim/E1030",
			Source: `vim9script
var x = '1' / '2'
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2269:67285
		{
			ID:   "src/testdir/test_vim9_expr.vim:2269:67285/vim9-script",
			Code: "vim/E1030",
			Source: `vim9script
var x = '1' % '2'
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4156:120001
		{
			ID:   "src/testdir/test_vim9_expr.vim:4156:120001/vim9-script",
			Code: "vim/E1030",
			Source: `vim9script
var x = -'xx'
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4157:120068
		{
			ID:   "src/testdir/test_vim9_expr.vim:4157:120068/vim9-script",
			Code: "vim/E1030",
			Source: `vim9script
var x = +'xx'
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4334:125856
		{
			ID:   "src/testdir/test_vim9_expr.vim:4334:125856/vim9-script",
			Code: "vim/E1030",
			Source: `vim9script
var d = 'asdf'['1']
echo d
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4346:126278
		{
			ID:   "src/testdir/test_vim9_expr.vim:4346:126278/vim9-script",
			Code: "vim/E1030",
			Source: `vim9script
var d = 'asdf'[1 : '2']
echo d
`,
		},

		// E1031
		// Vim: src/testdir/test_vim9_assign.vim:1629:40797
		{
			ID:   "src/testdir/test_vim9_assign.vim:1629:40797/def",
			Code: "vim/E1031",
			Source: `def Func()
# comment
var name = feedkeys("0")
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:529:12888
		{
			ID:   "src/testdir/test_vim9_assign.vim:529:12888/def",
			Code: "vim/E1031",
			Source: `def Func()
# comment
var v1: number
var v2: number
[v1, v2] = popup_clear()
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2359:109882
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2359:109882/vim9-script",
			Code: "vim/E1031",
			Source: `vim9script
def TestIdx(k: number, v: dict<any>)
enddef

indexof([{color: "red"}], TestIdx)
`,
		},

		// E1032
		// Vim: src/testdir/test_vim9_script.vim:1541:31577
		{
			ID:   "src/testdir/test_vim9_script.vim:1541:31577/def",
			Code: "vim/E1032",
			Source: `def Func()
# comment
try
echo 1
endtry
#comment
enddef
defcompile
`,
		},

		// E1033
		// Vim: src/testdir/test_vim9_script.vim:1533:31113
		{
			ID:   "src/testdir/test_vim9_script.vim:1533:31113/def",
			Code: "vim/E1033",
			Source: `def Func()
# comment
try
echo 0
catch
catch
#comment
enddef
defcompile
`,
		},

		// E1034
		// Vim: src/testdir/test_vim9_assign.vim:1564:37581
		{
			ID:   "src/testdir/test_vim9_assign.vim:1564:37581/def",
			Code: "vim/E1034",
			Source: `def Func()
# comment
var true = 1
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1565:37630
		{
			ID:   "src/testdir/test_vim9_assign.vim:1565:37630/def",
			Code: "vim/E1034",
			Source: `def Func()
# comment
var false = 1
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1566:37680
		{
			ID:   "src/testdir/test_vim9_assign.vim:1566:37680/def",
			Code: "vim/E1034",
			Source: `def Func()
# comment
var null = 1
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1567:37729
		{
			ID:   "src/testdir/test_vim9_assign.vim:1567:37729/def",
			Code: "vim/E1034",
			Source: `def Func()
# comment
var this = 1
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1568:37778
		{
			ID:   "src/testdir/test_vim9_assign.vim:1568:37778/def",
			Code: "vim/E1034",
			Source: `def Func()
# comment
var super = 1
#comment
enddef
defcompile
`,
		},

		// E1035
		// Vim: src/testdir/test_vim9_expr.vim:2269:67285
		{
			ID:   "src/testdir/test_vim9_expr.vim:2269:67285/def",
			Code: "vim/E1035",
			Source: `def Func()
# comment
var x = '1' % '2'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2273:67537
		{
			ID:   "src/testdir/test_vim9_expr.vim:2273:67537/def",
			Code: "vim/E1035",
			Source: `def Func()
# comment
var x = 0z01 % 0z12
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2277:67786
		{
			ID:   "src/testdir/test_vim9_expr.vim:2277:67786/def",
			Code: "vim/E1035",
			Source: `def Func()
# comment
var x = [1] % [2]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2281:68053
		{
			ID:   "src/testdir/test_vim9_expr.vim:2281:68053/def",
			Code: "vim/E1035",
			Source: `def Func()
# comment
var x = {one: 1} % {two: 2}
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2294:68582
		{
			ID:   "src/testdir/test_vim9_expr.vim:2294:68582/def",
			Code: "vim/E1035",
			Source: `def Func()
# comment
var x = 1.0 % 2
#comment
enddef
defcompile
`,
		},

		// E1036
		// Vim: src/testdir/test_vim9_expr.vim:1879:55648
		{
			ID:   "src/testdir/test_vim9_expr.vim:1879:55648/def",
			Code: "vim/E1036",
			Source: `def Func()
# comment
echo {} - 22
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1884:55756
		{
			ID:   "src/testdir/test_vim9_expr.vim:1884:55756/def",
			Code: "vim/E1036",
			Source: `def Func()
# comment
echo [] - 33
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1889:55868
		{
			ID:   "src/testdir/test_vim9_expr.vim:1889:55868/def",
			Code: "vim/E1036",
			Source: `def Func()
# comment
echo 0z1234 - 44
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2267:67119
		{
			ID:   "src/testdir/test_vim9_expr.vim:2267:67119/def",
			Code: "vim/E1036",
			Source: `def Func()
# comment
var x = '1' * '2'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2268:67202
		{
			ID:   "src/testdir/test_vim9_expr.vim:2268:67202/def",
			Code: "vim/E1036",
			Source: `def Func()
# comment
var x = '1' / '2'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2271:67369
		{
			ID:   "src/testdir/test_vim9_expr.vim:2271:67369/def",
			Code: "vim/E1036",
			Source: `def Func()
# comment
var x = 0z01 * 0z12
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2272:67453
		{
			ID:   "src/testdir/test_vim9_expr.vim:2272:67453/def",
			Code: "vim/E1036",
			Source: `def Func()
# comment
var x = 0z01 / 0z12
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2275:67622
		{
			ID:   "src/testdir/test_vim9_expr.vim:2275:67622/def",
			Code: "vim/E1036",
			Source: `def Func()
# comment
var x = [1] * [2]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2279:67869
		{
			ID:   "src/testdir/test_vim9_expr.vim:2279:67869/def",
			Code: "vim/E1036",
			Source: `def Func()
# comment
var x = {one: 1} * {two: 2}
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2280:67961
		{
			ID:   "src/testdir/test_vim9_expr.vim:2280:67961/def",
			Code: "vim/E1036",
			Source: `def Func()
# comment
var x = {one: 1} / {two: 2}
#comment
enddef
defcompile
`,
		},

		// E1037
		// Vim: src/testdir/test_vim9_expr.vim:1736/E1037-bool
		{
			ID:   "src/testdir/test_vim9_expr.vim:1736/E1037-bool/def",
			Code: "vim/E1037",
			Source: `def Func()
  var x = true is false
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1737/E1037-bool
		{
			ID:   "src/testdir/test_vim9_expr.vim:1737/E1037-bool/script",
			Code: "vim/E1037",
			Source: `vim9script
var x = true isnot false
`,
		},

		// E1038
		// Vim: src/testdir/test_vim9_script.vim:1817:37904
		{
			ID:   "src/testdir/test_vim9_script.vim:1817:37904/def",
			Code: "vim/E1038",
			Source: `def Func()
# comment
vim9script
#comment
enddef
defcompile
`,
		},

		// E1041
		// Vim: src/testdir/test_vim9_script.vim:3072:64467
		{
			ID:   "src/testdir/test_vim9_script.vim:3072:64467/vim9-script",
			Code: "vim/E1041",
			Source: `vim9script
var x = 5
for x in range(5)
endfor
`,
		},

		// E1050
		// Vim: src/testdir/test_vim9_script.vim:365:9084
		{
			ID:   "src/testdir/test_vim9_script.vim:365:9084/def",
			Code: "vim/E1050",
			Source: `def Func()
# comment
%s/a/b/
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:366:9128
		{
			ID:   "src/testdir/test_vim9_script.vim:366:9128/def",
			Code: "vim/E1050",
			Source: `def Func()
# comment
+ s/a/b/
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:367:9173
		{
			ID:   "src/testdir/test_vim9_script.vim:367:9173/def",
			Code: "vim/E1050",
			Source: `def Func()
# comment
- s/a/b/
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:368:9218
		{
			ID:   "src/testdir/test_vim9_script.vim:368:9218/def",
			Code: "vim/E1050",
			Source: `def Func()
# comment
. s/a/b/
#comment
enddef
defcompile
`,
		},

		// E1051
		// Vim: src/testdir/test_vim9_assign.vim:1890:48358
		{
			ID:   "src/testdir/test_vim9_assign.vim:1890:48358/def",
			Code: "vim/E1051",
			Source: `def Func()
# comment
echo filter([1, 2, 3], (_, v: string) => v + 1)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:237:5863
		{
			ID:   "src/testdir/test_vim9_assign.vim:237:5863/def",
			Code: "vim/E1051",
			Source: `def Func()
# comment
$SOME_ENV_VAR += "more"
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:243:6062
		{
			ID:   "src/testdir/test_vim9_assign.vim:243:6062/def",
			Code: "vim/E1051",
			Source: `def Func()
# comment
v:errmsg += "more"
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2042:60152
		{
			ID:   "src/testdir/test_vim9_expr.vim:2042:60152/def",
			Code: "vim/E1051",
			Source: `def Func()
# comment
var x = 0z1122 + [3]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2044:60324
		{
			ID:   "src/testdir/test_vim9_expr.vim:2044:60324/def",
			Code: "vim/E1051",
			Source: `def Func()
# comment
var x = 33 + 0z1122
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2046:60493
		{
			ID:   "src/testdir/test_vim9_expr.vim:2046:60493/def",
			Code: "vim/E1051",
			Source: `def Func()
# comment
var x = 'asdf' + 0z1122
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2057:61288
		{
			ID:   "src/testdir/test_vim9_expr.vim:2057:61288/def",
			Code: "vim/E1051",
			Source: `def Func()
# comment
var x = 1 + v:null
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2059:61455
		{
			ID:   "src/testdir/test_vim9_expr.vim:2059:61455/def",
			Code: "vim/E1051",
			Source: `def Func()
# comment
var x = 1 + v:false
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2061:61622
		{
			ID:   "src/testdir/test_vim9_expr.vim:2061:61622/def",
			Code: "vim/E1051",
			Source: `def Func()
# comment
var x = 1 + false
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1890:48358
		{
			ID:   "src/testdir/test_vim9_assign.vim:1890:48358/vim9-script",
			Code: "vim/E1051",
			Source: `vim9script
echo filter([1, 2, 3], (_, v: string) => v + 1)
`,
		},

		// E1052
		// Vim: src/testdir/test_vim9_assign.vim:1588:38634
		{
			ID:   "src/testdir/test_vim9_assign.vim:1588:38634/def",
			Code: "vim/E1052",
			Source: `def Func()
# comment
var &tabstop = 4
#comment
enddef
defcompile
`,
		},

		// E1058
		// Vim: src/testdir/test_vimscript.vim:7367-7403
		{
			ID:   "src/testdir/test_vimscript.vim:7367-7403/E1058",
			Code: "vim/E1058",
			Source: `function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
function X()
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
endfunction
`,
		},

		// E1059
		// Vim: src/testdir/test_vim9_script.vim:2823:58387
		{
			ID:   "src/testdir/test_vim9_script.vim:2823:58387/def",
			Code: "vim/E1059",
			Source: `def Func()
# comment
for i : number : [1, 2]
  echo i
endfor
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3107:65719
		{
			ID:   "src/testdir/test_vim9_script.vim:3107:65719/def",
			Code: "vim/E1059",
			Source: `def Func()
# comment
for n : number in [1, 2]
  echo n
endfor
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:2823:58387
		{
			ID:   "src/testdir/test_vim9_script.vim:2823:58387/vim9-script",
			Code: "vim/E1059",
			Source: `vim9script
for i : number : [1, 2]
  echo i
endfor
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3107:65719
		{
			ID:   "src/testdir/test_vim9_script.vim:3107:65719/vim9-script",
			Code: "vim/E1059",
			Source: `vim9script
for n : number in [1, 2]
  echo n
endfor
`,
		},

		// E1060
		// Vim: src/testdir/test_vim9_import.vim:191-200
		{
			ID:   "src/testdir/test_vim9_import.vim:191-200/E1060",
			Code: "vim/E1060",
			Source: `vim9script
import './Xexport.vim' as Export
def Func()
  var dummy = 1
  var imported = Export + dummy
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_import.vim:151-159
		{
			ID:   "src/testdir/test_vim9_import.vim:151-159/E1060",
			Code: "vim/E1060",
			Source: `vim9script
import './Xexport.vim' as expo
g:exported = expo
  .exported
`,
		},

		// E1062
		// Vim: src/testdir/test_vim9_expr.vim:2283:68146
		{
			ID:   "src/testdir/test_vim9_expr.vim:2283:68146/vim9-script",
			Code: "vim/E1062",
			Source: `vim9script
var x = 0xff[1]
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2589:77132
		{
			ID:   "src/testdir/test_vim9_expr.vim:2589:77132/vim9-script",
			Code: "vim/E1062",
			Source: `vim9script
var x = 1234[3]
`,
		},

		// E1065
		// Vim: src/testdir/test_vim9_assign.vim:2318:58046
		{
			ID:   "src/testdir/test_vim9_assign.vim:2318:58046/def",
			Code: "vim/E1065",
			Source: `def Func()
# comment
va foo = 123
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2318:58046
		{
			ID:   "src/testdir/test_vim9_assign.vim:2318:58046/vim9-script",
			Code: "vim/E1065",
			Source: `vim9script
va foo = 123
`,
		},

		// E1066
		// Vim: src/testdir/test_vim9_assign.vim:1606:39447
		{
			ID:   "src/testdir/test_vim9_assign.vim:1606:39447/def",
			Code: "vim/E1066",
			Source: `def Func()
# comment
var @a = 5
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1607:39494
		{
			ID:   "src/testdir/test_vim9_assign.vim:1607:39494/def",
			Code: "vim/E1066",
			Source: `def Func()
# comment
var @/ = "x"
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1596:38991
		{
			ID:   "src/testdir/test_vim9_assign.vim:1596:38991/vim9-script",
			Code: "vim/E1066",
			Source: `vim9script
var @. = 5
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1597:39061
		{
			ID:   "src/testdir/test_vim9_assign.vim:1597:39061/vim9-script",
			Code: "vim/E1066",
			Source: `vim9script
var @. = 5
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1598:39131
		{
			ID:   "src/testdir/test_vim9_assign.vim:1598:39131/vim9-script",
			Code: "vim/E1066",
			Source: `vim9script
var @% = 5
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1599:39201
		{
			ID:   "src/testdir/test_vim9_assign.vim:1599:39201/vim9-script",
			Code: "vim/E1066",
			Source: `vim9script
var @: = 5
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1601:39289
		{
			ID:   "src/testdir/test_vim9_assign.vim:1601:39289/vim9-script",
			Code: "vim/E1066",
			Source: `vim9script
var @~ = 5
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1603:39368
		{
			ID:   "src/testdir/test_vim9_assign.vim:1603:39368/vim9-script",
			Code: "vim/E1066",
			Source: `vim9script
var @~ = 5
`,
		},

		// E1067
		// Vim: src/testdir/test_vim9_script.vim:1534:31181
		{
			ID:   "src/testdir/test_vim9_script.vim:1534:31181/def",
			Code: "vim/E1067",
			Source: `def Func()
# comment
try
echo 0
catch /pat
#comment
enddef
defcompile
`,
		},

		// E1068
		// Vim: src/testdir/test_tuple.vim:82:2083
		{
			ID:   "src/testdir/test_tuple.vim:82:2083/def",
			Code: "vim/E1068",
			Source: `def Func()
# comment
var t: tuple <number> = ()
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:1632:40950
		{
			ID:   "src/testdir/test_vim9_assign.vim:1632:40950/def",
			Code: "vim/E1068",
			Source: `def Func()
# comment
var name: dict <number>
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2665:79494
		{
			ID:   "src/testdir/test_vim9_expr.vim:2665:79494/def",
			Code: "vim/E1068",
			Source: `def Func()
# comment
var l = [11 , 22]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3132:93292
		{
			ID:   "src/testdir/test_vim9_expr.vim:3132:93292/def",
			Code: "vim/E1068",
			Source: `def Func()
# comment
var x = {a: 8 , b: 9}
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:2885:66191
		{
			ID:   "src/testdir/test_vim9_func.vim:2885:66191/def",
			Code: "vim/E1068",
			Source: `def Func()
# comment
var RefWrong: func(string ,number)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_tuple.vim:82:2083
		{
			ID:   "src/testdir/test_tuple.vim:82:2083/vim9-script",
			Code: "vim/E1068",
			Source: `vim9script
var t: tuple <number> = ()
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2346:69935
		{
			ID:   "src/testdir/test_vim9_expr.vim:2346:69935/vim9-script",
			Code: "vim/E1068",
			Source: `vim9script
var x = <number >123
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3130:93165
		{
			ID:   "src/testdir/test_vim9_expr.vim:3130:93165/vim9-script",
			Code: "vim/E1068",
			Source: `vim9script
var x = {a : 8}
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3217:96189
		{
			ID:   "src/testdir/test_vim9_expr.vim:3217:96189/vim9-script",
			Code: "vim/E1068",
			Source: `vim9script
var d = {one : 1}
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:781:16158
		{
			ID:   "src/testdir/test_vim9_func.vim:781:16158/vim9-script",
			Code: "vim/E1068",
			Source: `vim9script
call Test ('text')
`,
		},

		// E1069
		// Vim: src/testdir/test_tuple.vim:72:1754
		{
			ID:   "src/testdir/test_tuple.vim:72:1754/def",
			Code: "vim/E1069",
			Source: `def Func()
# comment
var t: tuple<number> = ('a','b')
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:71:1740
		{
			ID:   "src/testdir/test_vim9_assign.vim:71:1740/def",
			Code: "vim/E1069",
			Source: `def Func()
# comment
var a:string = "x"
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2779:82692
		{
			ID:   "src/testdir/test_vim9_expr.vim:2779:82692/def",
			Code: "vim/E1069",
			Source: `def Func()
# comment
filter([1, 2], (k,v) => 1)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3212:96087
		{
			ID:   "src/testdir/test_vim9_expr.vim:3212:96087/def",
			Code: "vim/E1069",
			Source: `def Func()
# comment
var d = {one: 1,two: 2}
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:2888:66507
		{
			ID:   "src/testdir/test_vim9_func.vim:2888:66507/def",
			Code: "vim/E1069",
			Source: `def Func()
# comment
var RefWrong: func(bool):string
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_tuple.vim:72:1754
		{
			ID:   "src/testdir/test_tuple.vim:72:1754/vim9-script",
			Code: "vim/E1069",
			Source: `vim9script
var t: tuple<number> = ('a','b')
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2535:63760
		{
			ID:   "src/testdir/test_vim9_assign.vim:2535:63760/vim9-script",
			Code: "vim/E1069",
			Source: `vim9script
var n:number = 42
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2660:79392
		{
			ID:   "src/testdir/test_vim9_expr.vim:2660:79392/vim9-script",
			Code: "vim/E1069",
			Source: `vim9script
var l = [11,22]
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3133:93362
		{
			ID:   "src/testdir/test_vim9_expr.vim:3133:93362/vim9-script",
			Code: "vim/E1069",
			Source: `vim9script
var x = {a: 1,b: 2}
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:1695:37074
		{
			ID:   "src/testdir/test_vim9_func.vim:1695:37074/vim9-script",
			Code: "vim/E1069",
			Source: `vim9script
var Ref = (x):number => x + 1
`,
		},

		// E1071
		// Vim: src/testdir/test_vim9_import.vim:610-612
		{
			ID:   "src/testdir/test_vim9_import.vim:610-612/E1071-empty",
			Code: "vim/E1071",
			Source: `vim9script
import "" as abc
`,
		},
		// Vim: src/testdir/test_vim9_import.vim:615-616
		{
			ID:   "src/testdir/test_vim9_import.vim:615-616/E1071-null",
			Code: "vim/E1071",
			Source: `vim9script
import test_null_string() as abc
`,
		},

		// E1072
		// Vim: src/testdir/test_vim9_expr.vim:1304:37538
		{
			ID:   "src/testdir/test_vim9_expr.vim:1304:37538/def",
			Code: "vim/E1072",
			Source: `def Func()
# comment
echo [] == v:none
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1305:37634
		{
			ID:   "src/testdir/test_vim9_expr.vim:1305:37634/def",
			Code: "vim/E1072",
			Source: `def Func()
# comment
echo 123 == v:none
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1313:37901
		{
			ID:   "src/testdir/test_vim9_expr.vim:1313:37901/def",
			Code: "vim/E1072",
			Source: `def Func()
# comment
echo [] == v:none

eval 0 + 0
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1334:38502
		{
			ID:   "src/testdir/test_vim9_expr.vim:1334:38502/def",
			Code: "vim/E1072",
			Source: `def Func()
# comment
echo v:none == true
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1638:46545
		{
			ID:   "src/testdir/test_vim9_expr.vim:1638:46545/def",
			Code: "vim/E1072",
			Source: `def Func()
# comment
echo '' == 0
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1304:37538
		{
			ID:   "src/testdir/test_vim9_expr.vim:1304:37538/vim9-script",
			Code: "vim/E1072",
			Source: `vim9script
echo [] == v:none
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1305:37634
		{
			ID:   "src/testdir/test_vim9_expr.vim:1305:37634/vim9-script",
			Code: "vim/E1072",
			Source: `vim9script
echo 123 == v:none
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1313:37901
		{
			ID:   "src/testdir/test_vim9_expr.vim:1313:37901/vim9-script",
			Code: "vim/E1072",
			Source: `vim9script
echo [] == v:none

eval 0 + 0
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1334:38502
		{
			ID:   "src/testdir/test_vim9_expr.vim:1334:38502/vim9-script",
			Code: "vim/E1072",
			Source: `vim9script
echo v:none == true
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:1638:46545
		{
			ID:   "src/testdir/test_vim9_expr.vim:1638:46545/vim9-script",
			Code: "vim/E1072",
			Source: `vim9script
echo '' == 0
`,
		},

		// E1073
		// Vim: src/testdir/test_vim9_func.vim:1000:21712
		{
			ID:   "src/testdir/test_vim9_func.vim:1000:21712/def",
			Code: "vim/E1073",
			Source: `def Func()
# comment
def Outer()
  def Inner()
    # comment
  enddef
  def Inner()
  enddef
enddef
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:2131:47411
		{
			ID:   "src/testdir/test_vim9_func.vim:2131:47411/def",
			Code: "vim/E1073",
			Source: `def Func()
# comment
def Func(Ref: number)
  def Ref()
  enddef
enddef
#comment
enddef
defcompile
`,
		},

		// E1074
		// Vim: src/testdir/test_vim9_import.vim:202-211
		{
			ID:   "src/testdir/test_vim9_import.vim:202-211/E1074-def",
			Code: "vim/E1074",
			Source: `vim9script
import './Xexport.vim' as Export
def Func()
  var imported = Export . exported
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_import.vim:161-168
		{
			ID:   "src/testdir/test_vim9_import.vim:161-168/E1074-line-break",
			Code: "vim/E1074",
			Source: `vim9script
import './Xexport.vim' as expo
g:exported = expo.
  exported
`,
		},

		// E1075
		// Vim: src/testdir/test_vim9_expr.vim:4179:121179
		{
			ID:   "src/testdir/test_vim9_expr.vim:4179:121179/def",
			Code: "vim/E1075",
			Source: `def Func()
# comment
echo a:somevar
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4180:121258
		{
			ID:   "src/testdir/test_vim9_expr.vim:4180:121258/def",
			Code: "vim/E1075",
			Source: `def Func()
# comment
echo l:somevar
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4181:121337
		{
			ID:   "src/testdir/test_vim9_expr.vim:4181:121337/def",
			Code: "vim/E1075",
			Source: `def Func()
# comment
echo x:somevar
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:988:21437
		{
			ID:   "src/testdir/test_vim9_func.vim:988:21437/def",
			Code: "vim/E1075",
			Source: `def Func()
# comment
def s:Nested()
enddef
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_func.vim:989:21498
		{
			ID:   "src/testdir/test_vim9_func.vim:989:21498/def",
			Code: "vim/E1075",
			Source: `def Func()
# comment
def b:Nested()
enddef
#comment
enddef
defcompile
`,
		},

		// E1080
		// Vim: src/testdir/test_vim9_assign.vim:1571:37884
		{
			ID:   "src/testdir/test_vim9_assign.vim:1571:37884/def",
			Code: "vim/E1080",
			Source: `def Func()
# comment
var [a; b; c] = g:list
#comment
enddef
defcompile
`,
		},

		// E1081
		// Vim: src/testdir/test_vim9_assign.vim:2643-2647
		{
			ID:   "src/testdir/test_vim9_assign.vim:2643-2647/E1081-script-local",
			Code: "vim/E1081",
			Source: `vim9script
def Func()
  unlet s:somevar
enddef
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2802-2806
		{
			ID:   "src/testdir/test_vim9_assign.vim:2802-2806/E1081-script",
			Code: "vim/E1081",
			Source: `vim9script
var svar = 123
unlet svar
`,
		},

		// E1082
		// Vim: src/testdir/test_vim9_cmd.vim:1275:27368
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1275:27368/def",
			Code: "vim/E1082",
			Source: `def Func()
# comment
leftabove
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1280:27472
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1280:27472/def",
			Code: "vim/E1082",
			Source: `def Func()
# comment
leftabove # comment
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1275:27368
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1275:27368/vim9-script",
			Code: "vim/E1082",
			Source: `vim9script
leftabove
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1280:27472
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1280:27472/vim9-script",
			Code: "vim/E1082",
			Source: `vim9script
leftabove # comment
`,
		},

		// E1083
		// Vim: src/testdir/test_vim9_cmd.vim:267:6525
		{
			ID:     "src/testdir/test_vim9_cmd.vim:267:6525/def",
			Code:   "vim/E1083",
			Source: "def Func()\n# comment\nedit `=\"foo\"\n#comment\nenddef\ndefcompile\n",
		},

		// E1085
		// Vim: src/testdir/test_vim9_script.vim:210:5078
		{
			ID:   "src/testdir/test_vim9_script.vim:210:5078/def",
			Code: "vim/E1085",
			Source: `def Func()
# comment
var Ref: number
Ref()
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:211:5139
		{
			ID:   "src/testdir/test_vim9_script.vim:211:5139/def",
			Code: "vim/E1085",
			Source: `def Func()
# comment
var Ref: string
var res = Ref()
#comment
enddef
defcompile
`,
		},

		// E1087
		// Vim: src/testdir/test_vim9_assign.vim:2311:57842
		{
			ID:   "src/testdir/test_vim9_assign.vim:2311:57842/def",
			Code: "vim/E1087",
			Source: `def Func()
# comment
var foo.bar = 2
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2312:57894
		{
			ID:   "src/testdir/test_vim9_assign.vim:2312:57894/def",
			Code: "vim/E1087",
			Source: `def Func()
# comment
var foo[3] = 2
#comment
enddef
defcompile
`,
		},

		// E1089
		// Vim: src/testdir/test_vim9_assign.vim:534:12981
		{
			ID:   "src/testdir/test_vim9_assign.vim:534:12981/def",
			Code: "vim/E1089",
			Source: `def Func()
# comment
[v1, v2] = [1, 2]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1554:32769
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1554:32769/def",
			Code: "vim/E1089",
			Source: `def Func()
# comment
d.key = 'asdf'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1559:32880
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1559:32880/def",
			Code: "vim/E1089",
			Source: `def Func()
# comment
d['key'] = 'asdf'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:1984:41023
		{
			ID:   "src/testdir/test_vim9_cmd.vim:1984:41023/def",
			Code: "vim/E1089",
			Source: `def Func()
# comment
redir => notexist
#comment
enddef
defcompile
`,
		},

		// E1092
		// Vim: src/testdir/test_vim9_cmd.vim:2006:41418
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2006:41418/def",
			Code: "vim/E1092",
			Source: `def Func()
# comment
var text: string
redir => text
  echo 'hello'
  redir > Xnopfile
redir END
#comment
enddef
defcompile
`,
		},

		// E1093
		// Vim: src/testdir/test_vim9_assign.vim:494:12137
		{
			ID:   "src/testdir/test_vim9_assign.vim:494:12137/def",
			Code: "vim/E1093",
			Source: `def Func()
# comment
var v1: number
var v2: number
[v1, v2] = [1, 2, 3]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:501:12296
		{
			ID:   "src/testdir/test_vim9_assign.vim:501:12296/def",
			Code: "vim/E1093",
			Source: `def Func()
# comment
var v1: number
var v2: number
[v1, v2] = [1]
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:508:12458
		{
			ID:   "src/testdir/test_vim9_assign.vim:508:12458/def",
			Code: "vim/E1093",
			Source: `def Func()
# comment
var v1: number
var v2: number
[v1, v2; _] = [1]
#comment
enddef
defcompile
`,
		},

		// E1094
		// Vim: src/testdir/test_vim9_import.vim:506:13654
		{
			ID:   "src/testdir/test_vim9_import.vim:506:13654/def",
			Code: "vim/E1094",
			Source: `def Func()
# comment
import './Xfoo.vim' as foo
foo = 'bar'
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_import.vim:556:14850
		{
			ID:   "src/testdir/test_vim9_import.vim:556:14850/def",
			Code: "vim/E1094",
			Source: `def Func()
# comment
import './Xthat.vim' as That
That()
#comment
enddef
defcompile
`,
		},

		// E1095
		// Vim: src/testdir/test_vim9_func.vim:534:11233
		{
			ID:   "src/testdir/test_vim9_func.vim:534:11233/def",
			Code: "vim/E1095",
			Source: `def Func()
# comment
def Missing(): number
  if g:cond
    return 1
  else
    return 2
  endif
  return 3
enddef
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:1109:23010
		{
			ID:   "src/testdir/test_vim9_script.vim:1109:23010/def",
			Code: "vim/E1095",
			Source: `def Func()
# comment
try
  throw 'Error'
  echo 'not reached'
catch /Error/
endtry
#comment
enddef
defcompile
`,
		},

		// E1097
		// Vim: src/testdir/test_vim9_assign.vim:515:12614
		{
			ID:   "src/testdir/test_vim9_assign.vim:515:12614/def",
			Code: "vim/E1097",
			Source: "def Func()\n" +
				"# comment\n" +
				"var v1: number\n" +
				"var v2: number\n" +
				"[v1, v2] = \n" +
				"#comment\n" +
				"enddef\n" +
				"defcompile\n",
		},
		// Vim: src/testdir/test_vim9_expr.vim:2343:69743
		{
			ID:   "src/testdir/test_vim9_expr.vim:2343:69743/def",
			Code: "vim/E1097",
			Source: `def Func()
# comment
var x = <number>
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:2600:77674
		{
			ID:   "src/testdir/test_vim9_expr.vim:2600:77674/def",
			Code: "vim/E1097",
			Source: `def Func()
# comment
var x = g:list_mixed[
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:3743:109744
		{
			ID:   "src/testdir/test_vim9_expr.vim:3743:109744/def",
			Code: "vim/E1097",
			Source: `def Func()
# comment
echo (
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4153:119872
		{
			ID:   "src/testdir/test_vim9_expr.vim:4153:119872/def",
			Code: "vim/E1097",
			Source: `def Func()
# comment
var x = (12
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:4321:125571
		{
			ID:   "src/testdir/test_vim9_expr.vim:4321:125571/def",
			Code: "vim/E1097",
			Source: `def Func()
# comment
var d = 'asdf'[1 : 2
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_expr.vim:680:18502
		{
			ID:   "src/testdir/test_vim9_expr.vim:680:18502/def",
			Code: "vim/E1097",
			Source: "def Func()\n" +
				"# comment\n" +
				"var x = false || \n" +
				"#comment\n" +
				"enddef\n" +
				"defcompile\n",
		},
		// Vim: src/testdir/test_vim9_expr.vim:984:27804
		{
			ID:   "src/testdir/test_vim9_expr.vim:984:27804/def",
			Code: "vim/E1097",
			Source: "def Func()\n" +
				"# comment\n" +
				"var x = 'a' == \n" +
				"#comment\n" +
				"enddef\n" +
				"defcompile\n",
		},
		// Vim: src/testdir/test_vim9_script.vim:3068:64217
		{
			ID:   "src/testdir/test_vim9_script.vim:3068:64217/def",
			Code: "vim/E1097",
			Source: `def Func()
# comment
for x
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:3069:64279
		{
			ID:   "src/testdir/test_vim9_script.vim:3069:64279/def",
			Code: "vim/E1097",
			Source: `def Func()
# comment
for x in
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
