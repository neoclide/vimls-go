package syntax

import (
	"strings"
	"testing"

	"github.com/chemzqm/vimls-go/internal/vimdata"
)

var benchmarkParsedFile *File
var benchmarkExpression *Expression
var benchmarkDiagnostics []Diagnostic
var benchmarkConsumed int
var benchmarkContinuationState vim9ContinuationScan
var benchmarkOpaqueEnd int
var benchmarkLongestOperator string

func BenchmarkLongestOperator(b *testing.B) {
	inputs := []struct {
		name   string
		source string
	}{
		{name: "plus", source: "+"},
		{name: "isnot-hash", source: "isnot# value"},
		{name: "question-question", source: "?? fallback"},
		{name: "arrow", source: "->method()"},
		{name: "miss", source: "@register"},
		{name: "utf8-miss", source: "中文"},
		{name: "empty", source: ""},
	}
	for _, input := range inputs {
		b.Run(input.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(input.source)))
			b.ResetTimer()
			for range b.N {
				benchmarkLongestOperator = longestOperator(input.source)
			}
		})
	}
	mixedInputs := make([]string, 256)
	var mixedBytes int
	for index := range mixedInputs {
		mixedInputs[index] = inputs[index%len(inputs)].source
		mixedBytes += len(mixedInputs[index])
	}
	b.Run("mixed-batch", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(mixedBytes))
		b.ResetTimer()
		for range b.N {
			for _, source := range mixedInputs {
				benchmarkLongestOperator = longestOperator(source)
			}
		}
	})
}

func BenchmarkScanVim9Continuation(b *testing.B) {
	source := strings.Repeat("value isnot# other && (items[index] ?? fallback) # comment\n", 32)
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for range b.N {
		benchmarkContinuationState = scanVim9Continuation(source, vim9ContinuationScan{})
	}
}

func BenchmarkVim9OpaqueHash(b *testing.B) {
	source := "colorscheme " + strings.Repeat("foo#{key}bar#adjacent ", 4096)
	start := len("colorscheme ")
	b.ReportAllocs()
	b.SetBytes(int64(len(source) - start))
	b.ResetTimer()
	for range b.N {
		benchmarkOpaqueEnd, _, _ = scanVim9OpaqueArgument(source, start, len(source), vimdata.Command{})
	}
}

func BenchmarkParseSingleExpressionCommands(b *testing.B) {
	statement := "if exists('*maparg') && len([1, 2, 3]) > 0\nendif\n"
	tests := []struct {
		name   string
		source string
	}{
		{name: "legacy", source: strings.Repeat(statement, 64)},
		{name: "vim9", source: "vim9script\n" + strings.Repeat(statement, 64)},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 0 {
				b.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(test.source)))
			b.ResetTimer()
			for range b.N {
				benchmarkParsedFile = Parse(test.source)
			}
		})
	}
}

func BenchmarkParseHighlightCommands(b *testing.B) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "legacy-valid", source: strings.Repeat("hi Group ctermfg=2 guifg='light blue' future=value | echo ok\n", 64)},
		{name: "vim9-valid", source: "vim9script\n" + strings.Repeat("hi default link Error ErrorMsg | hi String ctermfg=2\n", 64)},
		{name: "legacy-malformed", source: strings.Repeat("hi Group guifg='unterminated | echo same-line\n", 64)},
		{name: "vim9-malformed", source: "vim9script\n" + strings.Repeat("hi link Error ErrorMsg extra | echo same-line\n", 64)},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(test.source)))
			b.ResetTimer()
			for range b.N {
				benchmarkParsedFile = Parse(test.source)
			}
		})
	}
}

func BenchmarkParseSyntaxCommands(b *testing.B) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "legacy-keyword", source: strings.Repeat("syntax keyword VimGroup contained Foo Bar baz[qux] display\n", 64)},
		{name: "legacy-match", source: strings.Repeat("syntax match VimGroup contains=Foo,Bar /a[|]\\/b/ms=s+1,me=e-1 containedin=Other\n", 64)},
		{name: "legacy-region", source: strings.Repeat("syntax region VimGroup start=/foo/ skip=/bar/ end=/baz/ keepend\n", 64)},
		{name: "legacy-cluster", source: strings.Repeat("syntax cluster VimCluster contains=Foo,@Bar add=Baz remove=Old\n", 64)},
		{name: "legacy-case", source: strings.Repeat("syntax case ignore ignored-tail | syntax case match\n", 64)},
		{name: "legacy-modes", source: strings.Repeat("syntax conceal on ignored-tail | syntax spell notoplevel\n", 64)},
		{name: "legacy-include", source: strings.Repeat("syntax include @VimGroup `=printf('foo|bar')`.vim | echo next\n", 64)},
		{name: "legacy-list-clear", source: strings.Repeat("syntax list VimGroup @VimCluster | syntax clear VimGroup @VimCluster\n", 64)},
		{name: "legacy-sync-settings", source: strings.Repeat("syntax sync ccomment Comment minlines=10 maxlines=100 linebreaks=1 linecont /\\\\$/\n", 64)},
		{name: "legacy-sync-match", source: strings.Repeat("syntax sync match VimSync grouphere Start /foo[|]bar/me=e-1 contained\n", 64)},
		{name: "legacy-sync-region", source: strings.Repeat("syntax sync region VimSync start=/foo/ skip=/bar/ end=/baz/ keepend\n", 64)},
		{name: "legacy-iskeyword", source: strings.Repeat("syntax iskeyword @,48-57,_,192-255 | echo retained\n", 64)},
		{name: "legacy-foldlevel", source: strings.Repeat("syntax foldlevel minimum\n", 64)},
		{name: "legacy-runtime-modes", source: strings.Repeat("syntax manual | syntax reset\n", 64)},
		{name: "vim9-valid", source: "vim9script\n" + strings.Repeat("syntax match VimGroup /foo#bar/ contains=Foo,Bar\n", 64)},
		{name: "legacy-malformed", source: strings.Repeat("syntax match VimGroup /unterminated | echo same-line\n", 64)},
		{name: "vim9-malformed", source: "vim9script\n" + strings.Repeat("syntax region VimGroup start=/foo/ | echo same-line\n", 64)},
		{name: "cluster-malformed", source: strings.Repeat("syntax cluster VimCluster add=ALL | echo same-line\n", 64)},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(test.source)))
			b.ResetTimer()
			for range b.N {
				benchmarkParsedFile = Parse(test.source)
			}
		})
	}
}

func BenchmarkParseSetCommands(b *testing.B) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "legacy", source: strings.Repeat("setlocal ts=8 sw=2 noexpandtab path+=/tmp\\ path hls! termcap | setglobal shell=sh\n", 64)},
		{name: "vim9", source: "vim9script\n" + strings.Repeat("set ts=8 sw=2 invlistchars+=space\\: nohlsearch | setlocal hls?\n", 64)},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(test.source)))
			b.ResetTimer()
			for range b.N {
				benchmarkParsedFile = Parse(test.source)
			}
		})
	}
}

func BenchmarkParseExpressionLineShapes(b *testing.B) {
	tests := []struct {
		name    string
		source  string
		dialect Dialect
	}{
		{name: "legacy-short-line", source: "exists('*maparg') && len([1, 2, 3]) > 0", dialect: Legacy},
		{name: "vim9-short-line", source: "exists('*maparg') && len([1, 2, 3]) > 0", dialect: Vim9},
		{name: "legacy-continuation", source: continuationExpression("  \\ && value", 64), dialect: Legacy},
		{name: "vim9-continuation", source: continuationExpression("  && value", 64), dialect: Vim9},
		{name: "legacy-continuation-256", source: continuationExpression("  \\ && value", 256), dialect: Legacy},
		{name: "vim9-continuation-256", source: continuationExpression("  && value", 256), dialect: Vim9},
		{name: "legacy-nested-curly", source: nestedCurlyExpression(64), dialect: Legacy},
		{name: "legacy-nested-curly-256", source: nestedCurlyExpression(256), dialect: Legacy},
		{name: "legacy-malformed-line", source: "Func([1, 2, 3} | echo 'same-line'", dialect: Legacy},
		{name: "vim9-malformed-line", source: "$\"unterminated {value | echo 'same-line'", dialect: Vim9},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(test.source)))
			b.ResetTimer()
			for range b.N {
				benchmarkExpression, benchmarkDiagnostics, benchmarkConsumed = parseExpressionPrefix(test.source, 0, test.dialect)
			}
		})
	}
}

func BenchmarkParseExpressionListCommands(b *testing.B) {
	statement := "echo get(g:, 'enabled', 0) len([1, 2, 3]) exists('*maparg')\n"
	tests := []struct {
		name   string
		source string
	}{
		{name: "legacy", source: strings.Repeat(statement, 64)},
		{name: "vim9", source: "vim9script\n" + strings.Repeat(statement, 64)},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 0 {
				b.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(test.source)))
			b.ResetTimer()
			for range b.N {
				benchmarkParsedFile = Parse(test.source)
			}
		})
	}
}

func BenchmarkParseDeclarationInitializers(b *testing.B) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "legacy",
			source: strings.Repeat("let value = get(g:, 'enabled', 0) + len([1, 2, 3])\n", 64),
		},
		{
			name:   "vim9",
			source: "vim9script\n" + strings.Repeat("var value = get(g:, 'enabled', 0) + len([1, 2, 3])\n", 64),
		},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 0 {
				b.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(test.source)))
			b.ResetTimer()
			for range b.N {
				benchmarkParsedFile = Parse(test.source)
			}
		})
	}
}

func BenchmarkParseMalformedVim9DeclarationInitializers(b *testing.B) {
	statement := "var broken = get(items, [1, 2, 3}\nvar after = 1\n"
	source := "vim9script\n" + strings.Repeat(statement, 64)
	file := Parse(source)
	if len(file.Diagnostics) == 0 || len(file.Commands) != 129 || file.Commands[len(file.Commands)-1].Declaration == nil {
		b.Fatalf("commands = %d, diagnostics = %#v", len(file.Commands), file.Diagnostics)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for range b.N {
		benchmarkParsedFile = Parse(source)
	}
}

func BenchmarkParseIncompleteAutocmdBlocks(b *testing.B) {
	source := strings.Repeat("autocmd BufEnter * {\n", 4096)
	file := (LegacyParser{}).Parse(source)
	if len(file.Commands) != 4096 {
		b.Fatalf("commands = %d, diagnostics = %#v", len(file.Commands), file.Diagnostics)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for range b.N {
		benchmarkParsedFile = (LegacyParser{}).Parse(source)
	}
}

func BenchmarkParseForIterables(b *testing.B) {
	statement := "for item in get(g:, 'items', [1, 2, 3])\nendfor\n"
	tests := []struct {
		name   string
		source string
	}{
		{name: "legacy", source: strings.Repeat(statement, 64)},
		{name: "vim9", source: "vim9script\n" + strings.Repeat(statement, 64)},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 0 {
				b.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(test.source)))
			b.ResetTimer()
			for range b.N {
				benchmarkParsedFile = Parse(test.source)
			}
		})
	}
}

func BenchmarkParsePutExpressionCommands(b *testing.B) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "legacy",
			source: strings.Repeat("put = get(g:, 'items', [1, 2, 3]) . string(len([1, 2]))\n", 64),
		},
		{
			name:   "vim9",
			source: "vim9script\n" + strings.Repeat("put = get(g:, 'items', [1, 2, 3]) .. string(len([1, 2]))\n", 64),
		},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 0 {
				b.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(test.source)))
			b.ResetTimer()
			for range b.N {
				benchmarkParsedFile = Parse(test.source)
			}
		})
	}
}

func BenchmarkParseGlobalEmbeddedCommands(b *testing.B) {
	source := strings.Repeat("g/^foo$/echo foo\nv/^bar$/echo bar\n", 64)
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 128 {
		b.Fatalf("commands = %d, diagnostics = %#v", len(file.Commands), file.Diagnostics)
	}
	nested := 0
	for index := range file.Commands {
		if file.Commands[index].Embedded == nil {
			b.Fatalf("command %d has no embedded payload", index)
		}
		nested += len(file.Commands[index].Embedded.Commands)
	}
	if nested != 128 {
		b.Fatalf("nested commands = %d", nested)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for range b.N {
		benchmarkParsedFile = (LegacyParser{}).Parse(source)
	}
}

func BenchmarkParseSubstituteCommands(b *testing.B) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "legacy-valid", source: strings.Repeat("s#foo[[:alpha:]]#\\=submatch(0) .. '! '#&gcnerp#liI 2 | echo done\n", 64)},
		{name: "legacy-missing", source: strings.Repeat("s/foo/bar | echo same-line\necho next\n", 64)},
		{name: "vim9-valid", source: "vim9script\n" + strings.Repeat("s!foo\\!bar!\\=substitute(submatch(0), 'x', 'y', '')!g\n", 64)},
		{name: "vim9-malformed", source: "vim9script\n" + strings.Repeat("s/foo/\\=value + | echo same-line\nvar next = 1\n", 64)},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			file := Parse(test.source)
			if file == nil {
				b.Fatal("nil parse result")
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(test.source)))
			b.ResetTimer()
			for range b.N {
				benchmarkParsedFile = Parse(test.source)
			}
		})
	}
}

func BenchmarkParseCommandCapacityShapes(b *testing.B) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "legacy-short-single", source: "let g:value = 1\n"},
		{name: "legacy-128-lines", source: strings.Repeat("let g:value = 1\n", 128)},
		{name: "legacy-64-bar-commands", source: strings.Repeat("echo 1 | ", 63) + "echo 1\n"},
		{name: "legacy-long-comments-single", source: strings.Repeat("\" padding that is not a command\n", 128) + "let g:value = 1\n"},
		{name: "legacy-heredoc-single", source: "let g:text =<< END\n" + strings.Repeat("payload that is not a command\n", 128) + "END\n"},
		{name: "vim9-128-lines", source: "vim9script\n" + strings.Repeat("var value = 1\n", 128)},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 0 {
				b.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(test.source)))
			b.ResetTimer()
			for range b.N {
				benchmarkParsedFile = Parse(test.source)
			}
		})
	}
}

func BenchmarkParseVim9CommandStartCalls(b *testing.B) {
	source := "vim9script\n" + strings.Repeat("Result(get(g:, 'value', 0))\n", 64)
	file := Parse(source)
	if len(file.Diagnostics) != 0 {
		b.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for range b.N {
		benchmarkParsedFile = Parse(source)
	}
}

func BenchmarkParseVim9CommandStartAssignments(b *testing.B) {
	source := "vim9script\n" + strings.Repeat("value.member = get(g:, 'value', 0) + len([1, 2, 3])\n", 64)
	file := Parse(source)
	if len(file.Diagnostics) != 0 {
		b.Fatalf("unexpected diagnostics: %#v", file.Diagnostics)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for range b.N {
		benchmarkParsedFile = Parse(source)
	}
}

func continuationExpression(line string, count int) string {
	var builder strings.Builder
	builder.WriteString("value")
	for range count {
		builder.WriteByte('\n')
		builder.WriteString(line)
	}
	return builder.String()
}

func nestedCurlyExpression(count int) string {
	return strings.Repeat("{", count) + "value" + strings.Repeat("}name", count)
}
