package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestCommandPatternAndGeneratedFlags(t *testing.T) {
	source := []byte(`
EXCMD(CMD_alpha, "alpha", ex_alpha, EX_BANG | EX_TRLBAR | EX_NEEDARG, ADDR_NONE)
EXCMD(CMD_beta, "beta", ex_beta,
    // retained source comment
    EX_NOTRLCOM | EX_EXPR_ARG | EX_WHOLE | EX_XFILE | EX_EXPORT | EX_NONWHITE_OK | EX_EXTRA, ADDR_NONE)
`)
	matches := commandPattern.FindAllSubmatch(source, -1)
	if len(matches) != 2 || string(matches[0][1]) != "alpha" || string(matches[1][1]) != "beta" {
		t.Fatalf("matches = %#v", matches)
	}
	if got := generatedFlagExpression(matches[0][2]); got != "AllowBang | AllowBar | NeedArgument" {
		t.Fatalf("alpha flags = %q", got)
	}
	if got := generatedFlagExpression(matches[1][2]); got != "NoTrailingComment | ExpressionArgument | ExactInVim9 | FileArgument | Exportable | AllowNonWhite" {
		t.Fatalf("beta flags = %q", got)
	}
	if got := generatedFlagExpression([]byte("EX_EXTRA")); got != "0" {
		t.Fatalf("unselected flags = %q", got)
	}
}

func TestPinnedCommandSourceCount(t *testing.T) {
	root := "/Users/chemzqm/lib/vim"
	resolved, err := exec.Command("git", "-C", root, "rev-list", "-n", "1", vimTag).Output()
	if err != nil || strings.TrimSpace(string(resolved)) != vimCommit {
		t.Skip("pinned Vim checkout is not available")
	}
	source, err := exec.Command("git", "-C", root, "show", vimTag+":src/ex_cmds.h").Output()
	if err != nil {
		t.Fatal(err)
	}
	if matches := commandPattern.FindAllSubmatch(source, -1); len(matches) != 600 {
		t.Fatalf("command count = %d, want 600", len(matches))
	}
}
