package syntax

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/vimdata"
)

func TestLegacyExpressionTrailingDoubleQuoteComments(t *testing.T) {
	source := "function! T(foo)\n" +
		"  return -1 \" no matching \"try\"!\n" +
		"endfunction\n" +
		"if exists('x') \" balanced \"word\" in comment\n" +
		"endif\n" +
		"let value = 1 \" comment\n" +
		"call T('arg') \" comment\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || countTokens(file, TokenComment) != 4 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
}

func TestLegacyDoubleQuoteStringsRemainExpressions(t *testing.T) {
	source := "return \"value\"\n" +
		"return foo . \"bar\"\n" +
		"return foo ? \"yes\" : \"no\"\n" +
		"return foo is \"bar\"\n" +
		"let value = \"text\"\n" +
		"execute command \" second\"\n" +
		"echo 1 \"second\"\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || countTokens(file, TokenComment) != 0 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	if len(file.Commands[5].Expressions) != 2 || len(file.Commands[6].Expressions) != 2 {
		t.Fatalf("execute = %#v, echo = %#v", file.Commands[5].Expressions, file.Commands[6].Expressions)
	}
}

func TestLegacyContinuedStringAfterOperatorIsNotComment(t *testing.T) {
	source := "let value = \"first\" .\n  \\ \"second\" .\n  \\ \"third\"\n"
	second := strings.Index(source, "\"second\"")
	metadata, _ := vimdata.Lookup(":let")
	if !legacyExpressionNeedsOperand(source, 4, second) || isCommentStart(source, second, 4, strings.IndexByte(source[second:], '\n')+second, Legacy, metadata) {
		t.Fatalf("continued quote was classified as a comment")
	}
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Declaration == nil || countTokens(file, TokenComment) != 0 {
		t.Fatalf("file = %#v", file)
	}
}
