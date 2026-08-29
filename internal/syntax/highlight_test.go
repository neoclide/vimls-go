package syntax

import (
	"strings"
	"testing"
)

func TestHighlightModesAndAttributes(t *testing.T) {
	source := "highlight\nhi Normal\nhi! clear Normal extra bytes | echo after\nhi default link Error ErrorMsg\nhi d l A B\nhi Group ctermfg = 1 guifg='light blue' NONE future=value ctermfont=F\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 7 || file.Commands[3].Canonical != "echo" {
		t.Fatalf("commands = %#v", file.Commands)
	}
	if file.Commands[0].Highlight == nil || file.Commands[0].Highlight.Kind != HighlightList {
		t.Fatalf("list = %#v", file.Commands[0])
	}
	if file.Commands[1].Highlight == nil || file.Commands[1].Highlight.Kind != HighlightQuery || file.Text(file.Commands[1].Highlight.Group) != "Normal" {
		t.Fatalf("query = %#v", file.Commands[1])
	}
	clear := file.Commands[2].Highlight
	if clear == nil || clear.Kind != HighlightClear || file.Text(clear.Group) != "Normal" || file.Text(file.Commands[2].Argument) != "clear Normal extra bytes" {
		t.Fatalf("clear = %#v argument=%q", clear, file.Text(file.Commands[2].Argument))
	}
	link := file.Commands[4].Highlight
	if link == nil || link.Kind != HighlightLink || file.Text(link.Group) != "Error" || file.Text(link.LinkTarget) != "ErrorMsg" || file.Text(link.Default) != "default" || file.Text(link.Operation) != "link" {
		t.Fatalf("abbreviated link = %#v", link)
	}
	abbreviated := file.Commands[5].Highlight
	if abbreviated == nil || abbreviated.Kind != HighlightLink || file.Text(abbreviated.Group) != "A" || file.Text(abbreviated.LinkTarget) != "B" || file.Text(abbreviated.Default) != "d" || file.Text(abbreviated.Operation) != "l" {
		t.Fatalf("abbreviated link = %#v", abbreviated)
	}
	define := file.Commands[6].Highlight
	if define == nil || define.Kind != HighlightDefine || len(define.Attributes) != 5 {
		t.Fatalf("define = %#v", define)
	}
	if got := file.Text(define.Attributes[0].Key); got != "ctermfg" || file.Text(define.Attributes[0].Value) != "1" {
		t.Fatalf("spaced equals = %#v", define.Attributes[0])
	}
	quoted := define.Attributes[1]
	if !quoted.Quoted || file.Text(quoted.Value) != "light blue" || strings.Contains(file.Text(quoted.Value), "'") {
		t.Fatalf("quoted value = %#v", quoted)
	}
	if file.Text(define.Attributes[2].Key) != "NONE" || define.Attributes[2].Equal != (Span{}) || define.Attributes[2].Value != (Span{}) {
		t.Fatalf("NONE = %#v", define.Attributes[2])
	}
	if file.Text(define.Attributes[3].Key) != "future" || file.Text(define.Attributes[4].Key) != "ctermfont" {
		t.Fatalf("forward-compatible attrs = %#v", define.Attributes)
	}
	assertFileSpans(t, file)
}

func TestHighlightKeywordPrefixesAreCaseSensitive(t *testing.T) {
	valid := (LegacyParser{}).Parse("hi c Foo\nhi l A B\nhi default c Foo\n")
	if len(valid.Diagnostics) != 0 || len(valid.Commands) != 3 {
		t.Fatalf("valid = commands %#v diagnostics %#v", valid.Commands, valid.Diagnostics)
	}
	if valid.Commands[0].Highlight.Kind != HighlightClear || valid.Commands[1].Highlight.Kind != HighlightLink || valid.Commands[2].Highlight.Kind != HighlightClear {
		t.Fatalf("prefix modes = %#v", valid.Commands)
	}
	upper := (LegacyParser{}).Parse("hi Clear\nhi Link\nhi Default\n")
	if len(upper.Diagnostics) != 0 || upper.Commands[0].Highlight.Kind != HighlightQuery || upper.Commands[1].Highlight.Kind != HighlightQuery || upper.Commands[2].Highlight.Kind != HighlightQuery {
		t.Fatalf("case-sensitive keywords = commands %#v diagnostics %#v", upper.Commands, upper.Diagnostics)
	}
}

func TestHighlightVim9MixedAndBang(t *testing.T) {
	source := "vim9script\nhi! default link Error ErrorMsg | hi String ctermfg=2\nlegacy hi clear Comment\n"
	file := (Vim9Parser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 {
		t.Fatalf("commands = %#v diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[1].Dialect != Vim9 || file.Commands[1].Bang == (Span{}) || file.Commands[1].Highlight.Kind != HighlightLink {
		t.Fatalf("vim9 highlight = %#v", file.Commands[1])
	}
	if file.Commands[2].Highlight == nil || file.Commands[2].Highlight.Kind != HighlightDefine || file.Text(file.Commands[2].Highlight.Group) != "String" {
		t.Fatalf("second highlight = %#v", file.Commands[2])
	}
	if file.Commands[3].Dialect != Legacy || file.Commands[3].Highlight == nil || file.Commands[3].Highlight.Kind != HighlightClear {
		t.Fatalf("legacy override = %#v", file.Commands)
	}
	assertFileSpans(t, file)
}

func TestHighlightBoundaryAndRecovery(t *testing.T) {
	cases := []struct {
		name, source, code string
	}{
		{"missing link target", "hi link A | echo lost\necho next\n", "vim/E412"},
		{"extra link target", "hi link A B C | echo lost\necho next\n", "vim/E413"},
		{"unexpected equal", "hi Group =value | echo lost\necho next\n", "vim/E415"},
		{"missing equal", "hi Group ctermfg | echo lost\necho next\n", "vim/E416"},
		{"missing value", "hi Group ctermfg= | echo lost\necho next\n", "vim/E417"},
		{"unmatched quote", "hi Group guifg='blue | echo lost\necho next\n", "vim/E475"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			file := (LegacyParser{}).Parse(test.source)
			if !hasDiagnostic(file, test.code) || len(file.Commands) != 2 || file.Commands[1].Canonical != "echo" {
				t.Fatalf("commands = %#v diagnostics = %#v", file.Commands, file.Diagnostics)
			}
			if len(file.Diagnostics) != 1 {
				t.Fatalf("expected one structural diagnostic, got %#v", file.Diagnostics)
			}
			if file.Text(file.Commands[0].Argument) != strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(strings.Split(test.source, "\n")[0], "\r"), "hi")) {
				t.Fatalf("malformed argument = %q", file.Text(file.Commands[0].Argument))
			}
			assertFileSpans(t, file)
		})
	}
	valid := (LegacyParser{}).Parse("hi Group guifg='blue|green' | echo after\n")
	if !hasDiagnostic(valid, "vim/E475") || len(valid.Commands) != 1 {
		t.Fatalf("quote Ex boundary = commands %#v diagnostics %#v", valid.Commands, valid.Diagnostics)
	}
	escaped := (LegacyParser{}).Parse("hi Group guifg=blue\\|green | echo after\n")
	if len(escaped.Diagnostics) != 0 || len(escaped.Commands) != 2 || escaped.Commands[1].Canonical != "echo" {
		t.Fatalf("escaped bar = commands %#v diagnostics %#v", escaped.Commands, escaped.Diagnostics)
	}
	vim9 := (Vim9Parser{}).Parse("vim9script\nhi link A B C | echo lost\necho next\n")
	if !hasDiagnostic(vim9, "vim/E413") || len(vim9.Commands) != 3 || vim9.Commands[2].Canonical != "echo" {
		t.Fatalf("vim9 malformed recovery = commands %#v diagnostics %#v", vim9.Commands, vim9.Diagnostics)
	}
	empty := (LegacyParser{}).Parse("hi Group guifg='' | echo lost\necho next\n")
	if !hasDiagnostic(empty, "vim/E417") || len(empty.Commands) != 2 || empty.Commands[0].Highlight == nil || len(empty.Commands[0].Highlight.Attributes) != 1 || !empty.Commands[0].Highlight.Attributes[0].Quoted {
		t.Fatalf("empty quoted value = commands %#v diagnostics %#v", empty.Commands, empty.Diagnostics)
	}
	attribute := empty.Commands[0].Highlight.Attributes[0]
	if attribute.Value.Start == 0 || attribute.Value.Start != attribute.Value.End {
		t.Fatalf("empty quoted value span = %#v", attribute)
	}
	noneEqual := (LegacyParser{}).Parse("hi Group NONE=value | echo lost\necho next\n")
	if !hasDiagnostic(noneEqual, "vim/E415") || len(noneEqual.Commands) != 2 || len(noneEqual.Commands[0].Highlight.Attributes) != 2 || noneEqual.Text(noneEqual.Commands[0].Highlight.Attributes[0].Key) != "NONE" {
		t.Fatalf("NONE with equal = commands %#v diagnostics %#v", noneEqual.Commands, noneEqual.Diagnostics)
	}
	assertFileSpans(t, empty)
	assertFileSpans(t, noneEqual)
}

func TestHighlightVim9BoundaryComments(t *testing.T) {
	valid := (Vim9Parser{}).Parse("vim9script\nhi Group guifg=blue # trailing\nhi Other guifg=red | echo after\n")
	if len(valid.Diagnostics) != 0 || len(valid.Commands) != 4 || valid.Commands[3].Canonical != "echo" {
		t.Fatalf("vim9 boundary = commands %#v diagnostics %#v", valid.Commands, valid.Diagnostics)
	}
	if fileText := valid.Text(valid.Commands[1].Argument); fileText != "Group guifg=blue" {
		t.Fatalf("vim9 argument = %q", fileText)
	}
	dict := (Vim9Parser{}).Parse("vim9script\nhi Group guifg=#{'x':1}\n")
	if len(dict.Diagnostics) != 0 || dict.Commands[1].Highlight == nil {
		t.Fatalf("dictionary boundary = commands %#v diagnostics %#v", dict.Commands, dict.Diagnostics)
	}
	quoted := (Vim9Parser{}).Parse("vim9script\nhi Group guifg='blue # still boundary'\n")
	if !hasDiagnostic(quoted, "vim/E475") || len(quoted.Commands) != 2 {
		t.Fatalf("vim9 quote boundary = commands %#v diagnostics %#v", quoted.Commands, quoted.Diagnostics)
	}
}

func TestHighlightLogicalContinuationSpans(t *testing.T) {
	source := "hi Group\n  \\ guifg='light blue' ctermfg=2\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 {
		t.Fatalf("commands = %#v diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	highlight := file.Commands[0].Highlight
	if highlight == nil || file.Text(highlight.Group) != "Group" || len(highlight.Attributes) != 2 || file.Text(highlight.Attributes[0].Value) != "light blue" {
		t.Fatalf("continuation highlight = %#v", highlight)
	}
	if file.Text(highlight.Attributes[0].Key) != "guifg" || source[highlight.Attributes[0].Key.Start:highlight.Attributes[0].Key.End] != "guifg" {
		t.Fatalf("mapped key = %#v", highlight.Attributes[0])
	}
	assertFileSpans(t, file)
}

func TestHighlightLambdaBodySpans(t *testing.T) {
	source := "# 前缀 😀\n() => {\n  hi Group guifg='light blue'\n}"
	expression, diagnostics := (Vim9ExpressionParser{}).Parse(source)
	if len(diagnostics) != 0 || expression == nil || expression.LambdaBody == nil || len(expression.LambdaBody.Commands) != 1 {
		t.Fatalf("lambda = %#v diagnostics = %#v", expression, diagnostics)
	}
	body := expression.LambdaBody
	highlight := body.Commands[0].Highlight
	if highlight == nil || body.Text(highlight.Group) != "Group" || len(highlight.Attributes) != 1 || body.Text(highlight.Attributes[0].Value) != "light blue" {
		t.Fatalf("lambda highlight = %#v", highlight)
	}
	assertFileSpansAt(t, body, "highlight lambda")
}
