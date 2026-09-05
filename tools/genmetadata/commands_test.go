package main

import (
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
	if got := generatedFlagExpression([]byte("EX_CMDARG | EX_ARGOPT")); got != "AllowCommandArgument | AllowArgumentOptions" {
		t.Fatalf("file prefix flags = %q", got)
	}
}
