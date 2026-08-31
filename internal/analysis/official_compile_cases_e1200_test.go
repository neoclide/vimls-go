package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestOfficialVimCompileCasesE1200(t *testing.T) {
	cases := []struct {
		ID     string
		Code   string
		Source string
	}{
		// E1202
		// Vim: src/testdir/test_vim9_assign.vim:3039:74843
		{
			ID:   "src/testdir/test_vim9_assign.vim:3039:74843/def",
			Code: "vim/E1202",
			Source: `def Func()
# comment
var nr = 7
++ nr
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:3039:74843
		{
			ID:   "src/testdir/test_vim9_assign.vim:3039:74843/vim9-script",
			Code: "vim/E1202",
			Source: `vim9script
var nr = 7
++ nr
`,
		},

		// E1206
		// Vim: src/testdir/test_vim9_builtin.vim:1884:86112
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1884:86112/vim9-script",
			Code: "vim/E1206",
			Source: `vim9script
getchar(1, 1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2953:134935
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2953:134935/vim9-script",
			Code: "vim/E1206",
			Source: `vim9script
mapset("a", false, [])
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3272:156012
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3272:156012/vim9-script",
			Code: "vim/E1206",
			Source: `vim9script
popup_menu("a", [1, 2])
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:333:13755
		{
			ID:   "src/testdir/test_vim9_builtin.vim:333:13755/vim9-script",
			Code: "vim/E1206",
			Source: `vim9script
autocmd_get(10)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3378:163015
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3378:163015/vim9-script",
			Code: "vim/E1206",
			Source: `vim9script
prop_list(1, [])
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3459:168486
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3459:168486/vim9-script",
			Code: "vim/E1206",
			Source: `vim9script
readdir("a", "1", [3])
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4199:203866
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4199:203866/vim9-script",
			Code: "vim/E1206",
			Source: `vim9script
setqflist([], "", [])
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4759:237896
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4759:237896/vim9-script",
			Code: "vim/E1206",
			Source: `vim9script
term_dumpload("a", ["b"])
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:5239:263891
		{
			ID:   "src/testdir/test_vim9_builtin.vim:5239:263891/vim9-script",
			Code: "vim/E1206",
			Source: `vim9script
win_splitmove(1, 2, [])
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:689:28302
		{
			ID:   "src/testdir/test_vim9_builtin.vim:689:28302/vim9-script",
			Code: "vim/E1206",
			Source: `vim9script
ch_readraw(test_null_channel(), [])
`,
		},

		// E1207
		// Vim: src/testdir/test_vim9_cmd.vim:763:16307
		{
			ID:   "src/testdir/test_vim9_cmd.vim:763:16307/def",
			Code: "vim/E1207",
			Source: `def Func()
# comment
@a = 'echo "text"'
@a
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:777:16552
		{
			ID:   "src/testdir/test_vim9_cmd.vim:777:16552/def",
			Code: "vim/E1207",
			Source: `def Func()
# comment
@a = 'echo "text"'
@a
    # comment
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:796:16921
		{
			ID:   "src/testdir/test_vim9_cmd.vim:796:16921/def",
			Code: "vim/E1207",
			Source: `def Func()
# comment
&l:showbreak = 'nothing'
&l:showbreak
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:810:17207
		{
			ID:   "src/testdir/test_vim9_cmd.vim:810:17207/def",
			Code: "vim/E1207",
			Source: `def Func()
# comment
$SomeEnv = 'value'
$SomeEnv
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:1859:38686
		{
			ID:   "src/testdir/test_vim9_script.vim:1859:38686/def",
			Code: "vim/E1207",
			Source: `def Func()
# comment
var undo = 1
undo
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:763:16307
		{
			ID:   "src/testdir/test_vim9_cmd.vim:763:16307/vim9-script",
			Code: "vim/E1207",
			Source: `vim9script
@a = 'echo "text"'
@a
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:777:16552
		{
			ID:   "src/testdir/test_vim9_cmd.vim:777:16552/vim9-script",
			Code: "vim/E1207",
			Source: `vim9script
@a = 'echo "text"'
@a
    # comment
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:796:16921
		{
			ID:   "src/testdir/test_vim9_cmd.vim:796:16921/vim9-script",
			Code: "vim/E1207",
			Source: `vim9script
&l:showbreak = 'nothing'
&l:showbreak
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:810:17207
		{
			ID:   "src/testdir/test_vim9_cmd.vim:810:17207/vim9-script",
			Code: "vim/E1207",
			Source: `vim9script
$SomeEnv = 'value'
$SomeEnv
`,
		},
		// Vim: src/testdir/test_vim9_script.vim:1859:38686
		{
			ID:   "src/testdir/test_vim9_script.vim:1859:38686/vim9-script",
			Code: "vim/E1207",
			Source: `vim9script
var undo = 1
undo
`,
		},

		// E1210
		// Vim: src/testdir/test_vim9_builtin.vim:207:5136
		{
			ID:   "src/testdir/test_vim9_builtin.vim:207:5136/vim9-script",
			Code: "vim/E1210",
			Source: `vim9script
and("x", 0x2)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2169:98143
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2169:98143/vim9-script",
			Code: "vim/E1210",
			Source: `vim9script
gettabwinvar("a", 2, "c")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2998:138545
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2998:138545/vim9-script",
			Code: "vim/E1210",
			Source: `vim9script
matchdelete("x")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3261:155067
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3261:155067/vim9-script",
			Code: "vim/E1210",
			Source: `vim9script
popup_hide("a")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3604:175615
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3604:175615/vim9-script",
			Code: "vim/E1210",
			Source: `vim9script
remote_read("a", "x")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4138:199739
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4138:199739/vim9-script",
			Code: "vim/E1210",
			Source: `vim9script
setcursorcharpos(1, 2, "3")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4562:225959
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4562:225959/vim9-script",
			Code: "vim/E1210",
			Source: `vim9script
strpart("a", "b")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4963:250818
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4963:250818/vim9-script",
			Code: "vim/E1210",
			Source: `vim9script
test_srand_seed("10")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:5292:265922
		{
			ID:   "src/testdir/test_vim9_builtin.vim:5292:265922/vim9-script",
			Code: "vim/E1210",
			Source: `vim9script
xor("x", 0x2)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:921:40508
		{
			ID:   "src/testdir/test_vim9_builtin.vim:921:40508/vim9-script",
			Code: "vim/E1210",
			Source: `vim9script
cursor(1, "2")
`,
		},

		// E1211
		// Vim: src/testdir/test_vim9_builtin.vim:1721:79196
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1721:79196/vim9-script",
			Code: "vim/E1211",
			Source: `vim9script
funcref("reverse", 2)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2665:123298
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2665:123298/vim9-script",
			Code: "vim/E1211",
			Source: `vim9script
list2blob(10)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3038:141380
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3038:141380/vim9-script",
			Code: "vim/E1211",
			Source: `vim9script
matchfuzzypos({}, "p")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:325:13343
		{
			ID:   "src/testdir/test_vim9_builtin.vim:325:13343/vim9-script",
			Code: "vim/E1211",
			Source: `vim9script
autocmd_add({})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3540:172540
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3540:172540/vim9-script",
			Code: "vim/E1211",
			Source: `vim9script
reltimefloat("x")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4113:197930
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4113:197930/vim9-script",
			Code: "vim/E1211",
			Source: `vim9script
setcharpos(".", 1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4293:211436
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4293:211436/vim9-script",
			Code: "vim/E1211",
			Source: `vim9script
sign_placelist("x")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4326:213894
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4326:213894/vim9-script",
			Code: "vim/E1211",
			Source: `vim9script
slice({"a": 10}, 1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:5453:269805
		{
			ID:   "src/testdir/test_vim9_builtin.vim:5453:269805/vim9-script",
			Code: "vim/E1211",
			Source: `vim9script
getregion(10, getpos("."))
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:813:35318
		{
			ID:   "src/testdir/test_vim9_builtin.vim:813:35318/vim9-script",
			Code: "vim/E1211",
			Source: `vim9script
complete(1, {})
`,
		},

		// E1212
		// Vim: src/testdir/test_vim9_builtin.vim:1127:50482
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1127:50482/vim9-script",
			Code: "vim/E1212",
			Source: `vim9script
expand("a", 2)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2203:100337
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2203:100337/vim9-script",
			Code: "vim/E1212",
			Source: `vim9script
glob("a", 2)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2227:102352
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2227:102352/vim9-script",
			Code: "vim/E1212",
			Source: `vim9script
has("a", "b")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2874:131093
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2874:131093/vim9-script",
			Code: "vim/E1212",
			Source: `vim9script
maparg("a", "b", true, 2)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3217:152376
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3217:152376/vim9-script",
			Code: "vim/E1212",
			Source: `vim9script
popup_clear(2)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:372:15271
		{
			ID:   "src/testdir/test_vim9_builtin.vim:372:15271/vim9-script",
			Code: "vim/E1212",
			Source: `vim9script
browse(2, "title", "dir", "file")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4484:221008
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4484:221008/vim9-script",
			Code: "vim/E1212",
			Source: `vim9script
str2list("a", 2)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4617:229338
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4617:229338/vim9-script",
			Code: "vim/E1212",
			Source: `vim9script
submatch(1, "a")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:5143:259180
		{
			ID:   "src/testdir/test_vim9_builtin.vim:5143:259180/vim9-script",
			Code: "vim/E1212",
			Source: `vim9script
visualmode("1")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:765:32514
		{
			ID:   "src/testdir/test_vim9_builtin.vim:765:32514/vim9-script",
			Code: "vim/E1212",
			Source: `vim9script
charidx("a", 1, "")
`,
		},

		// E1216
		// Vim: src/testdir/test_vim9_builtin.vim:1002:45380
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1002:45380/vim9-script",
			Code: "vim/E1216",
			Source: `vim9script
digraph_setlist("a")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1003:45594
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1003:45594/vim9-script",
			Code: "vim/E1216",
			Source: `vim9script
digraph_setlist({})
`,
		},

		// E1217
		// Vim: src/testdir/test_vim9_builtin.vim:563:22251
		{
			ID:   "src/testdir/test_vim9_builtin.vim:563:22251/vim9-script",
			Code: "vim/E1217",
			Source: `vim9script
ch_canread(10)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:579:22787
		{
			ID:   "src/testdir/test_vim9_builtin.vim:579:22787/vim9-script",
			Code: "vim/E1217",
			Source: `vim9script
ch_close_in(true)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:596:23536
		{
			ID:   "src/testdir/test_vim9_builtin.vim:596:23536/vim9-script",
			Code: "vim/E1217",
			Source: `vim9script
ch_evalraw(1, "")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:620:24886
		{
			ID:   "src/testdir/test_vim9_builtin.vim:620:24886/vim9-script",
			Code: "vim/E1217",
			Source: `vim9script
ch_getjob(1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:621:25062
		{
			ID:   "src/testdir/test_vim9_builtin.vim:621:25062/vim9-script",
			Code: "vim/E1217",
			Source: `vim9script
ch_getjob({"a": 10})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:639:25826
		{
			ID:   "src/testdir/test_vim9_builtin.vim:639:25826/vim9-script",
			Code: "vim/E1217",
			Source: `vim9script
ch_log("a", 1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:679:27655
		{
			ID:   "src/testdir/test_vim9_builtin.vim:679:27655/vim9-script",
			Code: "vim/E1217",
			Source: `vim9script
ch_readblob(1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:697:28594
		{
			ID:   "src/testdir/test_vim9_builtin.vim:697:28594/vim9-script",
			Code: "vim/E1217",
			Source: `vim9script
ch_sendexpr(1, "a")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:716:29748
		{
			ID:   "src/testdir/test_vim9_builtin.vim:716:29748/vim9-script",
			Code: "vim/E1217",
			Source: `vim9script
ch_setoptions(1, {})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:725:30225
		{
			ID:   "src/testdir/test_vim9_builtin.vim:725:30225/vim9-script",
			Code: "vim/E1217",
			Source: `vim9script
ch_status(1)
`,
		},

		// E1218
		// Vim: src/testdir/test_vim9_builtin.vim:2545:117184
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2545:117184/vim9-script",
			Code: "vim/E1218",
			Source: `vim9script
job_getchannel("a")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2554:117510
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2554:117510/vim9-script",
			Code: "vim/E1218",
			Source: `vim9script
job_info("a")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2565:117887
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2565:117887/vim9-script",
			Code: "vim/E1218",
			Source: `vim9script
job_setoptions(test_null_channel(), {})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2575:118413
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2575:118413/vim9-script",
			Code: "vim/E1218",
			Source: `vim9script
job_status("a")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2584:118712
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2584:118712/vim9-script",
			Code: "vim/E1218",
			Source: `vim9script
job_stop("a")
`,
		},

		// E1219
		// Vim: src/testdir/test_vim9_builtin.vim:1447:64094
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1447:64094/vim9-script",
			Code: "vim/E1219",
			Source: `vim9script
asin("a")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1455:64769
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1455:64769/vim9-script",
			Code: "vim/E1219",
			Source: `vim9script
atan2(1.2, "a")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1464:65472
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1464:65472/vim9-script",
			Code: "vim/E1219",
			Source: `vim9script
cosh("a")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1476:66366
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1476:66366/vim9-script",
			Code: "vim/E1219",
			Source: `vim9script
fmod(1.1, "a")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1484:67071
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1484:67071/vim9-script",
			Code: "vim/E1219",
			Source: `vim9script
isnan("a")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1493:67731
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1493:67731/vim9-script",
			Code: "vim/E1219",
			Source: `vim9script
pow("a", 1.1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1501:68432
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1501:68432/vim9-script",
			Code: "vim/E1219",
			Source: `vim9script
sin("a")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1513:69319
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1513:69319/vim9-script",
			Code: "vim/E1219",
			Source: `vim9script
tanh("a")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1516:69543
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1516:69543/vim9-script",
			Code: "vim/E1219",
			Source: `vim9script
trunc("a")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:299:11426
		{
			ID:   "src/testdir/test_vim9_builtin.vim:299:11426/vim9-script",
			Code: "vim/E1219",
			Source: `vim9script
assert_inrange(1, "b", 3)
`,
		},

		// E1220
		// Vim: src/testdir/test_vim9_builtin.vim:1677:77325
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1677:77325/vim9-script",
			Code: "vim/E1220",
			Source: `vim9script
foldtextresult(1.1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2034:93154
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2034:93154/vim9-script",
			Code: "vim/E1220",
			Source: `vim9script
getmarklist([])
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:226:5902
		{
			ID:   "src/testdir/test_vim9_builtin.vim:226:5902/vim9-script",
			Code: "vim/E1220",
			Source: `vim9script
append([1], "x")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2658:122861
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2658:122861/vim9-script",
			Code: "vim/E1220",
			Source: `vim9script
lispindent({})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3331:159724
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3331:159724/vim9-script",
			Code: "vim/E1220",
			Source: `vim9script
prompt_setcallback(true, "1")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4279:210065
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4279:210065/vim9-script",
			Code: "vim/E1220",
			Source: `vim9script
sign_jump(1, "b", true)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4780:239541
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4780:239541/vim9-script",
			Code: "vim/E1220",
			Source: `vim9script
term_getaltscreen(true)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4829:242172
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4829:242172/vim9-script",
			Code: "vim/E1220",
			Source: `vim9script
term_gettitle(1.1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4890:245812
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4890:245812/vim9-script",
			Code: "vim/E1220",
			Source: `vim9script
term_setsize(1.1, 2, 3)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:945:41893
		{
			ID:   "src/testdir/test_vim9_builtin.vim:945:41893/vim9-script",
			Code: "vim/E1220",
			Source: `vim9script
deletebufline([], 2)
`,
		},

		// E1221
		// Vim: src/testdir/test_vim9_builtin.vim:4242:206916
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4242:206916/vim9-script",
			Code: "vim/E1221",
			Source: `vim9script
sha256(100)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:597:23717
		{
			ID:   "src/testdir/test_vim9_builtin.vim:597:23717/vim9-script",
			Code: "vim/E1221",
			Source: `vim9script
ch_evalraw(test_null_channel(), 1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:707:29253
		{
			ID:   "src/testdir/test_vim9_builtin.vim:707:29253/vim9-script",
			Code: "vim/E1221",
			Source: `vim9script
ch_sendraw(test_null_channel(), 1)
`,
		},

		// E1222
		// Vim: src/testdir/test_vim9_builtin.vim:1043:47777
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1043:47777/vim9-script",
			Code: "vim/E1222",
			Source: `vim9script
execute(123)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:292:10652
		{
			ID:   "src/testdir/test_vim9_builtin.vim:292:10652/vim9-script",
			Code: "vim/E1222",
			Source: `vim9script
assert_fails("a", true)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3054:142421
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3054:142421/vim9-script",
			Code: "vim/E1222",
			Source: `vim9script
matchlist(0z12, "p")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3093:145143
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3093:145143/vim9-script",
			Code: "vim/E1222",
			Source: `vim9script
matchstrpos(0z12, "p")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:343:14095
		{
			ID:   "src/testdir/test_vim9_builtin.vim:343:14095/vim9-script",
			Code: "vim/E1222",
			Source: `vim9script
balloon_show(1.2)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4258:208096
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4258:208096/vim9-script",
			Code: "vim/E1222",
			Source: `vim9script
sign_define({"a": 10}, "b")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4713:234918
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4713:234918/vim9-script",
			Code: "vim/E1222",
			Source: `vim9script
system(1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:5118:258209
		{
			ID:   "src/testdir/test_vim9_builtin.vim:5118:258209/vim9-script",
			Code: "vim/E1222",
			Source: `vim9script
virtcol(1.1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:749:31363
		{
			ID:   "src/testdir/test_vim9_builtin.vim:749:31363/vim9-script",
			Code: "vim/E1222",
			Source: `vim9script
charcol(10)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:800:34204
		{
			ID:   "src/testdir/test_vim9_builtin.vim:800:34204/vim9-script",
			Code: "vim/E1222",
			Source: `vim9script
col({a: 10})
`,
		},

		// E1223
		// Vim: src/testdir/test_vim9_builtin.vim:2951:134582
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2951:134582/vim9-script",
			Code: "vim/E1223",
			Source: `vim9script
mapset(1, true, {})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:817:35522
		{
			ID:   "src/testdir/test_vim9_builtin.vim:817:35522/vim9-script",
			Code: "vim/E1223",
			Source: `vim9script
complete_add([])
`,
		},

		// E1224
		// Vim: src/testdir/test_vim9_builtin.vim:252:7693
		{
			ID:   "src/testdir/test_vim9_builtin.vim:252:7693/vim9-script",
			Code: "vim/E1224",
			Source: `vim9script
appendbufline(1, 1, {"a": 10})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3200:151141
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3200:151141/vim9-script",
			Code: "vim/E1224",
			Source: `vim9script
popup_atcursor({"a": 10}, {})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3211:151781
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3211:151781/vim9-script",
			Code: "vim/E1224",
			Source: `vim9script
popup_beval({"a": 10}, {})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3233:152998
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3233:152998/vim9-script",
			Code: "vim/E1224",
			Source: `vim9script
popup_dialog({"a": 10}, {})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3271:155812
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3271:155812/vim9-script",
			Code: "vim/E1224",
			Source: `vim9script
popup_menu({"a": 10}, {})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3281:156621
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3281:156621/vim9-script",
			Code: "vim/E1224",
			Source: `vim9script
popup_notification({"a": 10}, {})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4098:196898
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4098:196898/vim9-script",
			Code: "vim/E1224",
			Source: `vim9script
setbufline(1, 1, {"a": 10})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4714:235088
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4714:235088/vim9-script",
			Code: "vim/E1224",
			Source: `vim9script
system("a", {})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4720:235523
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4720:235523/vim9-script",
			Code: "vim/E1224",
			Source: `vim9script
systemlist("a", {})
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:920:40326
		{
			ID:   "src/testdir/test_vim9_builtin.vim:920:40326/vim9-script",
			Code: "vim/E1224",
			Source: `vim9script
cursor(0z10, 1)
`,
		},

		// E1225
		// Vim: src/testdir/test_vim9_builtin.vim:890:38767
		{
			ID:   "src/testdir/test_vim9_builtin.vim:890:38767/def",
			Code: "vim/E1225",
			Source: `def Func()
# comment
count(10, 1)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:890:38767
		{
			ID:   "src/testdir/test_vim9_builtin.vim:890:38767/vim9-script",
			Code: "vim/E1225",
			Source: `vim9script
count(10, 1)
`,
		},

		// E1226
		// Vim: src/testdir/test_vim9_builtin.vim:2437:113692
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2437:113692/vim9-script",
			Code: "vim/E1226",
			Source: `vim9script
insert("a", 1)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:75:1928
		{
			ID:   "src/testdir/test_vim9_builtin.vim:75:1928/vim9-script",
			Code: "vim/E1226",
			Source: `vim9script
add({}, 1)
`,
		},

		// E1228
		// Vim: src/testdir/test_vim9_builtin.vim:3671:178072
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3671:178072/vim9-script",
			Code: "vim/E1228",
			Source: `vim9script
remove("a", 1)
`,
		},

		// E1229
		// Vim: src/testdir/test_vim9_expr.vim:4188:121761
		{
			ID:   "src/testdir/test_vim9_expr.vim:4188:121761/def",
			Code: "vim/E1229",
			Source: `def Func()
# comment
var x = ''
var y = x.memb
#comment
enddef
defcompile
`,
		},

		// E1232
		// Vim: src/testdir/test_vim9_builtin.vim:1073:49098
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1073:49098/def",
			Code: "vim/E1232",
			Source: `def Func()
# comment
exists_compiled(10)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1074:49181
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1074:49181/def",
			Code: "vim/E1232",
			Source: `def Func()
# comment
exists_compiled(v:progname)
#comment
enddef
defcompile
`,
		},

		// E1233
		// Vim: src/testdir/test_vim9_builtin.vim:1073:49098
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1073:49098/vim9-script",
			Code: "vim/E1233",
			Source: `vim9script
exists_compiled(10)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1074:49181
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1074:49181/vim9-script",
			Code: "vim/E1233",
			Source: `vim9script
exists_compiled(v:progname)
`,
		},

		// E1235
		// Vim: src/testdir/test_vim9_builtin.vim:1883:85939
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1883:85939/vim9-script",
			Code: "vim/E1235",
			Source: `vim9script
getchar("1")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1902:87164
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1902:87164/vim9-script",
			Code: "vim/E1235",
			Source: `vim9script
getcharstr("1")
`,
		},

		// E1236
		// Vim: src/testdir/test_vim9_import.vim:506:13654
		{
			ID:   "src/testdir/test_vim9_import.vim:506:13654/vim9-script",
			Code: "vim/E1236",
			Source: `vim9script
import './Xfoo.vim' as foo
foo = 'bar'
`,
		},
		// Vim: src/testdir/test_vim9_import.vim:556:14850
		{
			ID:   "src/testdir/test_vim9_import.vim:556:14850/vim9-script",
			Code: "vim/E1236",
			Source: `vim9script
import './Xthat.vim' as That
That()
`,
		},

		// E1238
		// Vim: src/testdir/test_vim9_builtin.vim:356:14718
		{
			ID:   "src/testdir/test_vim9_builtin.vim:356:14718/vim9-script",
			Code: "vim/E1238",
			Source: `vim9script
blob2list(10)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:366:15059
		{
			ID:   "src/testdir/test_vim9_builtin.vim:366:15059/vim9-script",
			Code: "vim/E1238",
			Source: `vim9script
blob2str("ab")
`,
		},

		// E1241
		// Vim: src/testdir/test_vim9_cmd.vim:2062:42490
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2062:42490/def",
			Code: "vim/E1241",
			Source: `def Func()
# comment
g-pat-cmd
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:2081:43043
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2081:43043/def",
			Code: "vim/E1241",
			Source: `def Func()
# comment
s-pat-repl
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:2062:42490
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2062:42490/vim9-script",
			Code: "vim/E1241",
			Source: `vim9script
g-pat-cmd
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:2081:43043
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2081:43043/vim9-script",
			Code: "vim/E1241",
			Source: `vim9script
s-pat-repl
`,
		},

		// E1242
		// Vim: src/testdir/test_vim9_cmd.vim:2107:43740
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2107:43740/def",
			Code: "vim/E1242",
			Source: `def Func()
# comment
g /pat/cmd
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:2111:43834
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2111:43834/def",
			Code: "vim/E1242",
			Source: `def Func()
# comment
g #pat#cmd
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:2128:44136
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2128:44136/def",
			Code: "vim/E1242",
			Source: `def Func()
# comment
s /pat/repl
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:2132:44231
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2132:44231/def",
			Code: "vim/E1242",
			Source: `def Func()
# comment
s #pat#repl
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:2107:43740
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2107:43740/vim9-script",
			Code: "vim/E1242",
			Source: `vim9script
g /pat/cmd
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:2111:43834
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2111:43834/vim9-script",
			Code: "vim/E1242",
			Source: `vim9script
g #pat#cmd
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:2128:44136
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2128:44136/vim9-script",
			Code: "vim/E1242",
			Source: `vim9script
s /pat/repl
`,
		},
		// Vim: src/testdir/test_vim9_cmd.vim:2132:44231
		{
			ID:   "src/testdir/test_vim9_cmd.vim:2132:44231/vim9-script",
			Code: "vim/E1242",
			Source: `vim9script
s #pat#repl
`,
		},

		// E1251
		// Vim: src/testdir/test_vim9_builtin.vim:1686:77913
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1686:77913/def",
			Code: "vim/E1251",
			Source: `def Func()
# comment
foreach(test_null_job(), "")
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2523:116489
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2523:116489/def",
			Code: "vim/E1251",
			Source: `def Func()
# comment
123->items()
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1563:71478
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1563:71478/vim9-script",
			Code: "vim/E1251",
			Source: `vim9script
filter(1.1, "1")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1686:77913
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1686:77913/vim9-script",
			Code: "vim/E1251",
			Source: `vim9script
foreach(test_null_job(), "")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2712:125443
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2712:125443/vim9-script",
			Code: "vim/E1251",
			Source: `vim9script
map(test_null_channel(), "1")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2714:125670
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2714:125670/vim9-script",
			Code: "vim/E1251",
			Source: `vim9script
map(1, "1")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2945:134131
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2945:134131/vim9-script",
			Code: "vim/E1251",
			Source: `vim9script
mapnew(test_null_job(), "1")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2947:134353
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2947:134353/vim9-script",
			Code: "vim/E1251",
			Source: `vim9script
mapnew(1, "1")
`,
		},

		// E1253
		// Vim: src/testdir/test_vim9_builtin.vim:3514:171119
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3514:171119/vim9-script",
			Code: "vim/E1253",
			Source: `vim9script
reduce({a: 10}, "1")
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3736:181339
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3736:181339/vim9-script",
			Code: "vim/E1253",
			Source: `vim9script
reverse(10)
`,
		},

		// E1254
		// Vim: src/testdir/test_vim9_script.vim:3218:68649
		{
			ID:   "src/testdir/test_vim9_script.vim:3218:68649/def",
			Code: "vim/E1254",
			Source: `def Func()
# comment
for s:var in range(3)
echo 3
#comment
enddef
defcompile
`,
		},

		// E1256
		// Vim: src/testdir/test_listdict.vim:1022:29315
		{
			ID:   "src/testdir/test_listdict.vim:1022:29315/def",
			Code: "vim/E1256",
			Source: `def Func()
# comment
call sort(['a', 'b'], 0)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_listdict.vim:1027:29471
		{
			ID:   "src/testdir/test_listdict.vim:1027:29471/def",
			Code: "vim/E1256",
			Source: `def Func()
# comment
call sort(['a', 'b'], 1)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1564:71682
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1564:71682/def",
			Code: "vim/E1256",
			Source: `def Func()
# comment
filter([1, 2], 4)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2715:125870
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2715:125870/def",
			Code: "vim/E1256",
			Source: `def Func()
# comment
map([1, 2], 4)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_listdict.vim:1022:29315
		{
			ID:   "src/testdir/test_listdict.vim:1022:29315/vim9-script",
			Code: "vim/E1256",
			Source: `vim9script
call sort(['a', 'b'], 0)
`,
		},
		// Vim: src/testdir/test_listdict.vim:1027:29471
		{
			ID:   "src/testdir/test_listdict.vim:1027:29471/vim9-script",
			Code: "vim/E1256",
			Source: `vim9script
call sort(['a', 'b'], 1)
`,
		},

		// E1278
		// Vim: src/testdir/test_vim9_assign.vim:3292:80843
		{
			ID:   "src/testdir/test_vim9_assign.vim:3292:80843/def",
			Code: "vim/E1278",
			Source: `def Func()
# comment
var text =<< trim eval END
  aa}a
END
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:3292:80843
		{
			ID:   "src/testdir/test_vim9_assign.vim:3292:80843/vim9-script",
			Code: "vim/E1278",
			Source: `vim9script
var text =<< trim eval END
  aa}a
END
`,
		},

		// E1279
		// Vim: src/testdir/test_vim9_assign.vim:3270:80367
		{
			ID:   "src/testdir/test_vim9_assign.vim:3270:80367/def",
			Code: "vim/E1279",
			Source: `def Func()
# comment
var text =<< eval trim END
  let b = {
END
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:3277:80522
		{
			ID:   "src/testdir/test_vim9_assign.vim:3277:80522/def",
			Code: "vim/E1279",
			Source: `def Func()
# comment
var text =<< eval trim END
  let b = {abc
END
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:3270:80367
		{
			ID:   "src/testdir/test_vim9_assign.vim:3270:80367/vim9-script",
			Code: "vim/E1279",
			Source: `vim9script
var text =<< eval trim END
  let b = {
END
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:3277:80522
		{
			ID:   "src/testdir/test_vim9_assign.vim:3277:80522/vim9-script",
			Code: "vim/E1279",
			Source: `vim9script
var text =<< eval trim END
  let b = {abc
END
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
