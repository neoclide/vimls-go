package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestOfficialVimCompileCasesE1400(t *testing.T) {
	cases := []struct {
		ID     string
		Code   string
		Source string
	}{
		// E1528
		// Vim: src/testdir/test_vim9_builtin.vim:2323:108134
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2323:108134/vim9-script",
			Code: "vim/E1528",
			Source: `vim9script
index("a", "a")
`,
		},

		// E1529
		// Vim: src/testdir/test_vim9_builtin.vim:2590:119098
		{
			ID:   "src/testdir/test_vim9_builtin.vim:2590:119098/vim9-script",
			Code: "vim/E1529",
			Source: `vim9script
join("abc")
`,
		},

		// E1530
		// Vim: src/testdir/test_vim9_builtin.vim:3124:146876
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3124:146876/vim9-script",
			Code: "vim/E1530",
			Source: `vim9script
max(5)
`,
		},
		// Vim: src/testdir/test_vim9_builtin.vim:3146:147909
		{
			ID:   "src/testdir/test_vim9_builtin.vim:3146:147909/vim9-script",
			Code: "vim/E1530",
			Source: `vim9script
min(5)
`,
		},

		// E1531
		// Vim: src/testdir/test_vim9_builtin.vim:1798:81984
		{
			ID:   "src/testdir/test_vim9_builtin.vim:1798:81984/vim9-script",
			Code: "vim/E1531",
			Source: `vim9script
get("a", 1)
`,
		},

		// E1532
		// Vim: src/testdir/test_tuple.vim:1119:31047
		{
			ID:   "src/testdir/test_tuple.vim:1119:31047/def",
			Code: "vim/E1532",
			Source: `def Func()
# comment
var t = (1, 2)
t[0] = 3
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_tuple.vim:1119:31047
		{
			ID:   "src/testdir/test_tuple.vim:1119:31047/vim9-script",
			Code: "vim/E1532",
			Source: `vim9script
var t = (1, 2)
t[0] = 3
`,
		},

		// E1533
		// Vim: src/testdir/test_tuple.vim:1244:34760
		{
			ID:   "src/testdir/test_tuple.vim:1244:34760/def",
			Code: "vim/E1533",
			Source: `def Func()
# comment
var t: tuple<...list<string>> = ('a', 'b', 'c', 'd')
t[1 : 2] = ('x', 'y')
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_tuple.vim:1252:35007
		{
			ID:   "src/testdir/test_tuple.vim:1252:35007/def",
			Code: "vim/E1533",
			Source: `def Func()
# comment
var t: tuple<...list<string>> = ('a', 'b', 'c', 'd')
t[ : 2] = ('x', 'y')
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_tuple.vim:1266:35413
		{
			ID:   "src/testdir/test_tuple.vim:1266:35413/def",
			Code: "vim/E1533",
			Source: `def Func()
# comment
var t: tuple<...list<string>> = ('a', 'b', 'c', 'd')
t[ : ] = ('x', 'y')
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_tuple.vim:1244:34760
		{
			ID:   "src/testdir/test_tuple.vim:1244:34760/vim9-script",
			Code: "vim/E1533",
			Source: `vim9script
var t: tuple<...list<string>> = ('a', 'b', 'c', 'd')
t[1 : 2] = ('x', 'y')
`,
		},
		// Vim: src/testdir/test_tuple.vim:1252:35007
		{
			ID:   "src/testdir/test_tuple.vim:1252:35007/vim9-script",
			Code: "vim/E1533",
			Source: `vim9script
var t: tuple<...list<string>> = ('a', 'b', 'c', 'd')
t[ : 2] = ('x', 'y')
`,
		},
		// Vim: src/testdir/test_tuple.vim:1266:35413
		{
			ID:   "src/testdir/test_tuple.vim:1266:35413/vim9-script",
			Code: "vim/E1533",
			Source: `vim9script
var t: tuple<...list<string>> = ('a', 'b', 'c', 'd')
t[ : ] = ('x', 'y')
`,
		},

		// E1535
		// Vim: src/testdir/test_vim9_assign.vim:542:13173
		{
			ID:   "src/testdir/test_vim9_assign.vim:542:13173/def",
			Code: "vim/E1535",
			Source: `def Func()
# comment
var v1: number
var v2: number
[v1, v2] = ''
#comment
enddef
defcompile
`,
		},

		// E1539
		// Vim: src/testdir/test_tuple.vim:127:3486
		{
			ID:   "src/testdir/test_tuple.vim:127:3486/def",
			Code: "vim/E1539",
			Source: `def Func()
# comment
var t: tuple<number, ...number> = (1, 2, 3)
#comment
enddef
defcompile
`,
		},
		// Vim: src/testdir/test_tuple.vim:127:3486
		{
			ID:   "src/testdir/test_tuple.vim:127:3486/vim9-script",
			Code: "vim/E1539",
			Source: `vim9script
var t: tuple<number, ...number> = (1, 2, 3)
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

// Vim v9.2.1235 src/testdir/test_vim9_generics.vim
// Test_generic_interface_method_type_var_count(), patch v9.2.1209.
func TestVim921235InterfaceGenericMethodCount(t *testing.T) {
	for _, test := range []struct {
		required, actual string
		inherited        bool
		want             bool
	}{
		{"<T>(t: T): T", "(t: number): number", false, true},
		{"(t: number): number", "<T>(t: T): T", false, true},
		{"<T>(t: T): T", "<X, Y>(t: X): X", false, true},
		{"<T>(t: T): T", "<X, Y>(t: X): X", true, true},
		{"<T>(t: T): T", "<X>(t: X): X", false, false},
	} {
		classHeader := "class A implements I\n"
		suffix := ""
		if test.inherited {
			classHeader = "class Parent\n"
			suffix = "class A extends Parent implements I\nendclass\n"
		}
		source := "vim9script\ninterface I\n def Fn" + test.required + "\nendinterface\n" + classHeader + " def Fn" + test.actual + "\n  return t\n enddef\nendclass\n" + suffix
		file := syntax.Parse(source)
		result := Analyze(file)
		var got []syntax.Diagnostic
		for _, d := range result.Diagnostics {
			if d.Code == "vim/E1438" {
				got = append(got, d)
			}
		}
		if (len(got) == 1) != test.want || len(got) > 1 {
			t.Fatalf("E1438=%#v; all=%#v\n%s", got, result.Diagnostics, source)
		}
		if len(got) == 1 && (got[0].Message != `Number of type variables of method "Fn" differs from interface "I"` || file.Text(got[0].Span) != "endclass") {
			t.Fatalf("E1438=%#v", got)
		}
	}
}
