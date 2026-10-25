package analysis

import (
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// Vim v9.2.1235: test_vim9_script.vim Test_try_catch_throw() and
// Test_unreachable_code(), updated by patch 9.2.1155 (GitHub issue #13).
func TestVim921235TryTermination(t *testing.T) {
	for _, body := range []string{
		"try\n return ''\ncatch /.*/\n return ''\nendtry\n",
		"try\n throw 'x'\ncatch /x/\n return ''\nendtry\n",
		"try\n return ''\ncatch\n throw 'x'\nendtry\n",
		"try\n return ''\nfinally\n echo 'done'\nendtry\n",
		"try\n echo 'work'\nfinally\n throw 'x'\nendtry\n",
	} {
		for _, trailing := range []bool{false, true} {
			source := "vim9script\ndef F(): string\n" + body
			if trailing {
				source += "return ''\n"
			}
			source += "enddef\n"
			result := Analyze(syntax.Parse(source))
			unreachable := 0
			for _, d := range result.Diagnostics {
				if d.Code == "vim/E1027" {
					t.Fatalf("unexpected missing return: %#v\n%s", result.Diagnostics, source)
				}
				if d.Code == "vim/E1095" {
					unreachable++
				}
			}
			if (unreachable == 1) != trailing {
				t.Fatalf("unreachable=%d, trailing=%v; all=%#v", unreachable, trailing, result.Diagnostics)
			}
		}
	}
	// A nonterminating try/catch still requires a return after endtry.
	for _, body := range []string{
		"try\n echo 'work'\ncatch\n return ''\nendtry\n",
		"try\n return ''\ncatch\n echo 'caught'\nfinally\n echo 'done'\nendtry\n",
	} {
		result := Analyze(syntax.Parse("vim9script\ndef F(): string\n" + body + "enddef\n"))
		found := false
		for _, d := range result.Diagnostics {
			found = found || d.Code == "vim/E1027"
		}
		if !found {
			t.Fatalf("missing E1027: %#v", result.Diagnostics)
		}
	}
	for _, jump := range []string{"break", "continue"} {
		result := Analyze(syntax.Parse("vim9script\ndef F()\n for n in [1]\n  " + jump + "\n  echo n\n endfor\nenddef\n"))
		found := false
		for _, d := range result.Diagnostics {
			found = found || d.Code == "vim/E1095"
		}
		if !found {
			t.Fatalf("missing loop-jump unreachable: %#v", result.Diagnostics)
		}
	}
}

// Vim v9.2.1206 test_vim9_interface.vim Test_method_returning_own_class().
func TestVim921235MethodReturnsImplementingClass(t *testing.T) {
	for _, returned := range []string{"A", "Other"} {
		source := "vim9script\ninterface I\n def Copy(): I\nendinterface\nclass Other\nendclass\nclass A implements I\n def Copy(): " + returned + "\n  return " + returned + ".new()\n enddef\nendclass\n"
		result := Analyze(syntax.Parse(source))
		mismatch := false
		for _, d := range result.Diagnostics {
			mismatch = mismatch || d.Code == "vim/E1383"
		}
		if mismatch != (returned == "Other") {
			t.Fatalf("return %s: %#v", returned, result.Diagnostics)
		}
	}
}
