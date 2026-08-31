package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestOfficialVimCompileCasesE1300(t *testing.T) {
	cases := []struct {
		ID     string
		Code   string
		Source string
	}{
		// E1301
		// Vim: src/testdir/test_vim9_builtin.vim:3714:180261
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3714:180261/vim9-script",
			Code: "vim/E1301",
			Source: `vim9script
repeat(1.1, 2)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3715:180456
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3715:180456/vim9-script",
			Code: "vim/E1301",
			Source: `vim9script
repeat({a: 10}, 2)
`,
		},

		// E1306
		// Vim: src/testdir/test_vim9_script.vim:3182:67686
		{
			ID:   "src/testdir/test_vim9_script.vim:3182:67686/def",
			Code: "vim/E1306",
			Source: `def Func()
# comment
for x in range(3)
for a in range(3)
  while a > 3
    for b in range(2)
      while b < 0
        for c in range(5)
          while c > 6
            while c < 0
              for d in range(1)
                for e in range(3)
                  while e > 3
                  endwhile
                endfor
              endfor
            endwhile
          endwhile
        endfor
      endwhile
    endfor
  endwhile
endfor
endfor
#comment
enddef
defcompile
`,
		},

		// E1307
		// Vim: src/testdir/test_vim9_builtin.vim:1301:56764
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1301:56764/def",
			Code: "vim/E1307",
			Source: `def Func()
# comment
const l = [1, 2]
extend(l, [3])
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1320:57263
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1320:57263/def",
			Code: "vim/E1307",
			Source: `def Func()
# comment
const b = 0z0102
extend(b, 0z03)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:1643:75324
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1643:75324/def",
			Code: "vim/E1307",
			Source: `def Func()
# comment
const l = [1, 2, 3]
filter(l, 'v:val == 2')
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:189:4727
		{
			ID:   "src/testdir/test_vim9_builtin.vim:189:4727/def",
			Code: "vim/E1307",
			Source: `def Func()
# comment
const l = [1, 2]
add(l, 3)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:2781:127622
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2781:127622/def",
			Code: "vim/E1307",
			Source: `def Func()
# comment
const l = [1, 2, 3]
map(l, 'SomeFunc')
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3655:177622
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3655:177622/def",
			Code: "vim/E1307",
			Source: `def Func()
# comment
const l = [1, 2, 3, 4]
remove(l, 1)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3661:177794
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3661:177794/def",
			Code: "vim/E1307",
			Source: `def Func()
# comment
const d = {a: 1, b: 2}
remove(d, 'a')
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3753:181777
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3753:181777/def",
			Code: "vim/E1307",
			Source: `def Func()
# comment
const l = [1, 2, 3, 4]
reverse(l)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:4405:217029
		{
			ID:   "src/testdir/test_vim9_builtin.vim:4405:217029/def",
			Code: "vim/E1307",
			Source: `def Func()
# comment
const l = [1, 2, 3, 4]
sort(l)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:5083:257365
		{
			ID:   "src/testdir/test_vim9_builtin.vim:5083:257365/def",
			Code: "vim/E1307",
			Source: `def Func()
# comment
const l = [1, 2, 3, 4]
uniq(l)
#comment
enddef
defcompile
`,
		},

		// E1330
		// Vim: src/testdir/test_vim9_assign.vim:2353:59012
		{
			ID:   "src/testdir/test_vim9_assign.vim:2353:59012/def",
			Code: "vim/E1330",
			Source: `def Func()
# comment
var x: void
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2368:59448
		{
			ID:   "src/testdir/test_vim9_assign.vim:2368:59448/def",
			Code: "vim/E1330",
			Source: `def Func()
# comment
var a: void = 10
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2380:59824
		{
			ID:   "src/testdir/test_vim9_assign.vim:2380:59824/def",
			Code: "vim/E1330",
			Source: `def Func()
# comment
var t: tuple<void> = ()
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2386:60011
		{
			ID:   "src/testdir/test_vim9_assign.vim:2386:60011/def",
			Code: "vim/E1330",
			Source: `def Func()
# comment
var l: dict<void> = {}
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2409:60691
		{
			ID:   "src/testdir/test_vim9_assign.vim:2409:60691/def",
			Code: "vim/E1330",
			Source: `def Func()
# comment
for [i: number, j: void] in ((1, 2), (3, 4))
endfor
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2353:59012
		{
			ID:   "src/testdir/test_vim9_assign.vim:2353:59012/vim9-script",
			Code: "vim/E1330",
			Source: `vim9script
var x: void
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2368:59448
		{
			ID:   "src/testdir/test_vim9_assign.vim:2368:59448/vim9-script",
			Code: "vim/E1330",
			Source: `vim9script
var a: void = 10
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2374:59635
		{
			ID:   "src/testdir/test_vim9_assign.vim:2374:59635/vim9-script",
			Code: "vim/E1330",
			Source: `vim9script
var l: list<void> = []
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2380:59824
		{
			ID:   "src/testdir/test_vim9_assign.vim:2380:59824/vim9-script",
			Code: "vim/E1330",
			Source: `vim9script
var t: tuple<void> = ()
`,
		},
		// Vim: src/testdir/test_vim9_assign.vim:2392:60210
		{
			ID:   "src/testdir/test_vim9_assign.vim:2392:60210/vim9-script",
			Code: "vim/E1330",
			Source: `vim9script
var Fn = (x: void) => x
`,
		},

		// E1353
		// Vim: src/testdir/test_vim9_class.vim:11747:266149
		{
			ID:   "src/testdir/test_vim9_class.vim:11747:266149/def",
			Code: "vim/E1353",
			Source: `def Func()
# comment
var x: object<number>
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_vim9_class.vim:11747:266149
		{
			ID:   "src/testdir/test_vim9_class.vim:11747:266149/vim9-script",
			Code: "vim/E1353",
			Source: `vim9script
var x: object<number>
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
