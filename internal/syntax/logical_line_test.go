package syntax

import (
	"strings"
	"testing"
)

func TestLegacyLogicalLineNoIndentContinuation(t *testing.T) {
	source := "let g:value =\n\\ 1\nlet g:after = 2\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	command := file.Commands[0]
	if command.Canonical != "let" || command.Declaration == nil || command.Declaration.Initializer == nil || command.Declaration.Initializer.Kind != ExpressionNumber {
		t.Fatalf("command = %#v", command)
	}
	continuation := strings.Index(source, "\\ 1")
	if continuation < 0 || command.Span.Start != 0 || command.Span.End != continuation+len("\\ 1") {
		t.Fatalf("command span = %#v, continuation = %d", command.Span, continuation)
	}
	if got := file.Text(command.Argument); got != "g:value =\n\\ 1" {
		t.Fatalf("argument = %q", got)
	}
	if countTokens(file, TokenContinuation) != 1 {
		t.Fatalf("tokens = %#v", file.Tokens)
	}
}

func TestLegacyLogicalLineEmptyPayload(t *testing.T) {
	source := "echo \n  \\\n  \\1\nlet g:after = 2\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[0].Canonical != "echo" || len(file.Commands[0].Expressions) != 1 || file.Commands[0].Expressions[0].Kind != ExpressionNumber {
		t.Fatalf("echo = %#v", file.Commands[0])
	}
	if file.Commands[1].Canonical != "let" || countTokens(file, TokenContinuation) != 2 {
		t.Fatalf("commands = %#v, tokens = %#v", file.Commands, file.Tokens)
	}
	if file.Commands[0].Span.Start != 0 || file.Commands[0].Span.End != strings.Index(source, "\\1")+2 {
		t.Fatalf("echo span = %#v", file.Commands[0].Span)
	}
}

func TestLegacyLogicalLineModifiersAcrossContinuations(t *testing.T) {
	source := "aboveleft belowright botright browse confirm hide keepalt keepjumps\n" +
		"\t\t\t\\ keepmarks keeppatterns lockmarks noswapfile silent tab\n" +
		"\t\t\t\\ topleft verbose vertical call Func()\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	command := file.Commands[0]
	if command.Canonical != "call" || len(command.Modifiers) != 17 || len(command.Expressions) != 1 || command.Expressions[0].Kind != ExpressionCall {
		t.Fatalf("call = %#v", command)
	}
	if file.Text(command.Argument) != "Func()" || countTokens(file, TokenContinuation) != 2 {
		t.Fatalf("argument = %q, tokens = %#v", file.Text(command.Argument), file.Tokens)
	}
	if command.Span.Start != 0 || command.Span.End != len(strings.TrimSuffix(source, "\n")) {
		t.Fatalf("command span = %#v", command.Span)
	}
}

func TestLegacyLogicalLineUTF8TabAndStringContinuation(t *testing.T) {
	source := "let g:text = \"\n" +
		"  \\one\n" +
		"\t\\two 鸡\n" +
		"  \\three\"\n" +
		"let g:after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	command := file.Commands[0]
	if command.Canonical != "let" || command.Declaration == nil || command.Declaration.Initializer == nil || command.Declaration.Initializer.Kind != ExpressionString {
		t.Fatalf("string command = %#v", command)
	}
	initializer := command.Declaration.Initializer.Span
	if initializer.Start != strings.Index(source, "\"") || initializer.End != strings.Index(source, "\"\nlet g:after")+1 {
		t.Fatalf("initializer span = %#v", initializer)
	}
	if !strings.Contains(file.Text(command.Span), "\t\\two 鸡") || countTokens(file, TokenContinuation) != 3 {
		t.Fatalf("command = %q, tokens = %#v", file.Text(command.Span), file.Tokens)
	}
}

func TestLegacyLogicalLineTrailingTokenKeepsOriginalByteSpan(t *testing.T) {
	source := "let x = y\n  \\ z\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Commands) != 1 || len(file.Diagnostics) == 0 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	trailing := strings.Index(source, "z")
	if trailing < 0 {
		t.Fatal("test source has no trailing token")
	}
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Span.Start == trailing && diagnostic.Span.End == trailing+1 {
			return
		}
	}
	t.Fatalf("diagnostics = %#v, want trailing token span [%d,%d)", file.Diagnostics, trailing, trailing+1)
}

func TestLogicalViewAppendsOrdinaryNewlineDirectly(t *testing.T) {
	source := "echo value\n"
	view := readLegacyLogicalView(source, 0)
	if view.Physical != nil || !view.hasPendingNewline {
		t.Fatalf("ordinary line physical tokens = %#v, pending = %v", view.Physical, view.hasPendingNewline)
	}
	file := &File{Dialect: Legacy, Source: source}
	scanLogicalCommandsWithContext(file, &view, Legacy, "", false, 1)
	want := []Token{
		{Kind: TokenCommand, Span: Span{Start: 0, End: 4}},
		{Kind: TokenWhitespace, Span: Span{Start: 4, End: 5}},
		{Kind: TokenArgument, Span: Span{Start: 5, End: 10}},
		{Kind: TokenNewline, Span: Span{Start: 10, End: 11}},
	}
	if len(file.Tokens) != len(want) {
		t.Fatalf("file tokens = %#v, want %#v", file.Tokens, want)
	}
	for index, token := range want {
		if file.Tokens[index] != token {
			t.Fatalf("file token %d = %#v, want %#v", index, file.Tokens[index], token)
		}
	}
	if view.Physical != nil || view.hasPendingNewline {
		t.Fatalf("ordinary line retained physical allocation: %#v, pending = %v", view.Physical, view.hasPendingNewline)
	}
}

func TestLogicalViewPublicTokenTable(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		dialect Dialect
		want    []Token
	}{
		{
			name:    "legacy lf",
			source:  "echo value\n",
			dialect: Legacy,
			want: []Token{
				{Kind: TokenCommand, Span: Span{Start: 0, End: 4}},
				{Kind: TokenWhitespace, Span: Span{Start: 4, End: 5}},
				{Kind: TokenArgument, Span: Span{Start: 5, End: 10}},
				{Kind: TokenNewline, Span: Span{Start: 10, End: 11}},
			},
		},
		{
			name:    "legacy crlf",
			source:  "echo value\r\n",
			dialect: Legacy,
			want: []Token{
				{Kind: TokenCommand, Span: Span{Start: 0, End: 4}},
				{Kind: TokenWhitespace, Span: Span{Start: 4, End: 5}},
				{Kind: TokenArgument, Span: Span{Start: 5, End: 10}},
				{Kind: TokenNewline, Span: Span{Start: 10, End: 12}},
			},
		},
		{
			name:    "legacy continuation",
			source:  "echo value\n\\ 2\r\n",
			dialect: Legacy,
			want: []Token{
				{Kind: TokenCommand, Span: Span{Start: 0, End: 4}},
				{Kind: TokenWhitespace, Span: Span{Start: 4, End: 5}},
				{Kind: TokenArgument, Span: Span{Start: 5, End: 14}},
				{Kind: TokenNewline, Span: Span{Start: 10, End: 11}},
				{Kind: TokenContinuation, Span: Span{Start: 11, End: 12}},
				{Kind: TokenNewline, Span: Span{Start: 14, End: 16}},
			},
		},
		{
			name:    "vim9 explicit continuation",
			source:  "var x = 1 +\n  \\ 2\n",
			dialect: Vim9,
			want: []Token{
				{Kind: TokenCommand, Span: Span{Start: 0, End: 3}},
				{Kind: TokenWhitespace, Span: Span{Start: 3, End: 4}},
				{Kind: TokenArgument, Span: Span{Start: 4, End: 17}},
				{Kind: TokenNewline, Span: Span{Start: 11, End: 12}},
				{Kind: TokenWhitespace, Span: Span{Start: 12, End: 14}},
				{Kind: TokenContinuation, Span: Span{Start: 14, End: 15}},
				{Kind: TokenNewline, Span: Span{Start: 17, End: 18}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := parseSource(test.source, test.dialect)
			if len(file.Diagnostics) != 0 {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			if len(file.Tokens) != len(test.want) {
				t.Fatalf("file tokens = %#v, want %#v", file.Tokens, test.want)
			}
			for index, want := range test.want {
				if file.Tokens[index] != want {
					t.Fatalf("file token %d = %#v, want %#v", index, file.Tokens[index], want)
				}
			}
		})
	}
}

func TestLogicalViewNewlineSpansAndContinuationOrder(t *testing.T) {
	tests := []struct {
		name   string
		read   func(string, int) logicalView
		source string
		want   []Token
	}{
		{
			name:   "legacy crlf",
			read:   readLegacyLogicalView,
			source: "echo value\r\n",
			want:   []Token{{Kind: TokenNewline, Span: Span{Start: 10, End: 12}}},
		},
		{
			name:   "legacy continuation",
			read:   readLegacyLogicalView,
			source: "echo value\n\\ 2\r\n",
			want: []Token{
				{Kind: TokenNewline, Span: Span{Start: 10, End: 11}},
				{Kind: TokenContinuation, Span: Span{Start: 11, End: 12}},
				{Kind: TokenNewline, Span: Span{Start: 14, End: 16}},
			},
		},
		{
			name:   "vim9 explicit continuation",
			read:   readVim9LogicalView,
			source: "var x = 1 +\n  \\ 2\n",
			want: []Token{
				{Kind: TokenNewline, Span: Span{Start: 11, End: 12}},
				{Kind: TokenWhitespace, Span: Span{Start: 12, End: 14}},
				{Kind: TokenContinuation, Span: Span{Start: 14, End: 15}},
				{Kind: TokenNewline, Span: Span{Start: 17, End: 18}},
			},
		},
		{
			name:   "vim9 leading bar continuation",
			read:   readVim9LogicalView,
			source: "autocmd BufNewFile *.match if ok\n  | echo 'match'\n",
			want: []Token{
				{Kind: TokenNewline, Span: Span{Start: 32, End: 33}},
				{Kind: TokenWhitespace, Span: Span{Start: 33, End: 35}},
				{Kind: TokenContinuation, Span: Span{Start: 35, End: 49}},
				{Kind: TokenNewline, Span: Span{Start: 49, End: 50}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			view := test.read(test.source, 0)
			view.flushNewline()
			if len(view.Physical) != len(test.want) {
				t.Fatalf("physical tokens = %#v, want %#v", view.Physical, test.want)
			}
			for index, want := range test.want {
				if view.Physical[index] != want {
					t.Fatalf("physical token %d = %#v, want %#v", index, view.Physical[index], want)
				}
			}
		})
	}
}

func TestLogicalViewPreservesEOFAndBOMNewlineBehavior(t *testing.T) {
	if file := (LegacyParser{}).Parse("echo value"); countTokens(file, TokenNewline) != 0 {
		t.Fatalf("EOF newline tokens = %#v", file.Tokens)
	}
	source := "\ufeffecho value\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Tokens) < 2 || file.Tokens[0] != (Token{Kind: TokenBOM, Span: Span{End: 3}}) {
		t.Fatalf("BOM tokens = %#v", file.Tokens)
	}
	newline := file.Tokens[len(file.Tokens)-1]
	if newline != (Token{Kind: TokenNewline, Span: Span{Start: 13, End: 14}}) {
		t.Fatalf("BOM newline = %#v", newline)
	}
}

func TestVim9LogicalViewContinuationParserKeepsNewlines(t *testing.T) {
	sources := []string{
		"vim9script\nvar values = [\n  1,\n  2,\n]\n",
		"vim9script\nautocmd BufNewFile *.match if ok\n  | echo 'match'\n  | endif\n",
		"def Pick(ok: bool): string\n  return ok\n    ? 'yes'\n    : 'no'\nenddef\n",
	}
	for _, source := range sources {
		file := (Vim9Parser{}).Parse(source)
		if len(file.Diagnostics) != 0 || countTokens(file, TokenNewline) == 0 {
			t.Fatalf("source = %q, diagnostics = %#v, tokens = %#v", source, file.Diagnostics, file.Tokens)
		}
	}
}
