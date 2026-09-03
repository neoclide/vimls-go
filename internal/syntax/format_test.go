package syntax

import (
	"reflect"
	"strings"
	"testing"
)

func TestIndentEditsMatchPinnedOwnedCases(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "Legacy blocks comments brackets and continuations",
			source: "function! Some()\n" +
				"let x = 1\n" +
				"if 1\n" +
				"echo 2\n" +
				"else\n" +
				"\" branch\n" +
				"let values = [\n" +
				"\\ 1,\n" +
				"\\ ]\n" +
				"endif\n" +
				"endfunction\n" +
				"let cmd =\n" +
				"\\ 'some ' .\n" +
				"\\ 'string'\n",
			want: "function! Some()\n" +
				"    let x = 1\n" +
				"    if 1\n" +
				"        echo 2\n" +
				"    else\n" +
				"        \" branch\n" +
				"        let values = [\n" +
				"                    \\ 1,\n" +
				"                    \\ ]\n" +
				"    endif\n" +
				"endfunction\n" +
				"let cmd =\n" +
				"            \\ 'some ' .\n" +
				"            \\ 'string'\n",
		},
		{
			name: "Vim9 blocks signatures expressions and continuations",
			source: "vim9script\n" +
				"def MyFunc(\n" +
				"text: string,\n" +
				"separator = '-'\n" +
				"): string\n" +
				"if true\n" +
				"var values = [\n" +
				"1,\n" +
				"]\n" +
				"var text = lead\n" +
				".. middle\n" +
				"else\n" +
				"# branch\n" +
				"echo separator\n" +
				"endif\n" +
				"enddef\n",
			want: "vim9script\n" +
				"def MyFunc(\n" +
				"        text: string,\n" +
				"        separator = '-'\n" +
				"        ): string\n" +
				"    if true\n" +
				"        var values = [\n" +
				"            1,\n" +
				"        ]\n" +
				"        var text = lead\n" +
				"            .. middle\n" +
				"    else\n" +
				"        # branch\n" +
				"        echo separator\n" +
				"    endif\n" +
				"enddef\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			edits := IndentEdits(file, IndentOptions{TabSize: 4, InsertSpaces: true})
			assertIndentEdits(t, test.source, edits)
			got := applyIndentEdits(t, test.source, edits)
			if got != test.want {
				t.Fatalf("formatted source:\n%s\nwant:\n%s", got, test.want)
			}
			assertSameFormatShape(t, file, Parse(got))
			if second := IndentEdits(Parse(got), IndentOptions{TabSize: 4, InsertSpaces: true}); len(second) != 0 {
				t.Fatalf("second formatting returned %#v", second)
			}
		})
	}
}

func TestIndentEditsPreserveProtectedSource(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "heredoc mapping substitute and unknown command",
			source: "if 1\n" +
				"  let text =<< trim END\n" +
				" body  \n" +
				"  END\n" +
				" nnoremap x  y   \n" +
				" substitute/pat /rep /\n" +
				"  FutureCommand payload\n" +
				" echo text\n" +
				"endif\n",
			want: "if 1\n" +
				"  let text =<< trim END\n" +
				" body  \n" +
				"  END\n" +
				"    nnoremap x  y   \n" +
				"    substitute/pat /rep /\n" +
				"  FutureCommand payload\n" +
				"    echo text\n" +
				"endif\n",
		},
		{
			name:   "text body",
			source: "if 1\n  append\n literal  \n.\n echo 1\nendif\n",
			want:   "if 1\n  append\n literal  \n.\n    echo 1\nendif\n",
		},
		{
			name:   "loadkeymap body",
			source: "  loadkeymap\n a b  \n  # body\n",
			want:   "  loadkeymap\n a b  \n  # body\n",
		},
		{
			name:   "opaque tail",
			source: "finish\n  unknown tail\n\tmore\n",
			want:   "finish\n  unknown tail\n\tmore\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := applyIndentEdits(t, test.source, IndentEdits(Parse(test.source), IndentOptions{TabSize: 4, InsertSpaces: true}))
			if got != test.want {
				t.Fatalf("formatted source = %q, want %q", got, test.want)
			}
		})
	}
}

func TestIndentEditsPreserveBOMCRLFAndUseTabs(t *testing.T) {
	source := "\ufeffif 1\r\n  echo 'x'\r\nendif"
	edits := IndentEdits(Parse(source), IndentOptions{TabSize: 4})
	assertIndentEdits(t, source, edits)
	got := applyIndentEdits(t, source, edits)
	want := "\ufeffif 1\r\n\techo 'x'\r\nendif"
	if got != want {
		t.Fatalf("formatted source = %q, want %q", got, want)
	}
	if strings.Count(got, "\r\n") != 2 || strings.HasSuffix(got, "\n") {
		t.Fatalf("newline spelling changed in %q", got)
	}
}

func TestIndentEditsCoverSharedBlocks(t *testing.T) {
	source := "vim9script\n" +
		"class Box\n" +
		"def Method()\n" +
		"try\n" +
		"for item in [1]\n" +
		"while item > 0\n" +
		"if item\n" +
		"echo item\n" +
		"elseif item == 0\n" +
		"echo 0\n" +
		"else\n" +
		"echo -1\n" +
		"endif\n" +
		"endwhile\n" +
		"endfor\n" +
		"catch\n" +
		"echo 'error'\n" +
		"finally\n" +
		"echo 'done'\n" +
		"endtry\n" +
		"enddef\n" +
		"endclass\n"
	want := "vim9script\n" +
		"class Box\n" +
		"  def Method()\n" +
		"    try\n" +
		"      for item in [1]\n" +
		"        while item > 0\n" +
		"          if item\n" +
		"            echo item\n" +
		"          elseif item == 0\n" +
		"            echo 0\n" +
		"          else\n" +
		"            echo -1\n" +
		"          endif\n" +
		"        endwhile\n" +
		"      endfor\n" +
		"    catch\n" +
		"      echo 'error'\n" +
		"    finally\n" +
		"      echo 'done'\n" +
		"    endtry\n" +
		"  enddef\n" +
		"endclass\n"
	file := Parse(source)
	got := applyIndentEdits(t, source, IndentEdits(file, IndentOptions{TabSize: 2, InsertSpaces: true}))
	if got != want {
		t.Fatalf("formatted blocks:\n%s\nwant:\n%s", got, want)
	}
	assertSameFormatShape(t, file, Parse(got))
}

func TestIndentEditsCoverAugroupAndVim9AggregateBlocks(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "Legacy augroup",
			source: "augroup Name\nautocmd!\n\" comment\naugroup END\n",
			want:   "augroup Name\n    autocmd!\n    \" comment\naugroup END\n",
		},
		{
			name: "Vim9 aggregates command and scope",
			source: "vim9script\n" +
				"interface Face\ndef Run()\nendinterface\n" +
				"enum Color\nRed,\nGreen\nendenum\n" +
				"command Foo {\necho 1\n}\n" +
				"{\nvar value = 1\n}\n",
			want: "vim9script\n" +
				"interface Face\n    def Run()\nendinterface\n" +
				"enum Color\n    Red,\n    Green\nendenum\n" +
				"command Foo {\n    echo 1\n}\n" +
				"{\n    var value = 1\n}\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 0 {
				t.Fatalf("fixture diagnostics = %#v", file.Diagnostics)
			}
			got := applyIndentEdits(t, test.source, IndentEdits(file, IndentOptions{TabSize: 4, InsertSpaces: true}))
			if got != test.want {
				t.Fatalf("formatted source:\n%s\nwant:\n%s", got, test.want)
			}
		})
	}
}

func TestIndentEditsCoverVim9ExpressionDelimiters(t *testing.T) {
	source := "vim9script\n" +
		"var list = [\n1,\n]\n" +
		"var dict = {\none: [\n2,\n],\n}\n" +
		"var call = Func(\n1,\n2\n)\n" +
		"var indexed = values[\n0\n]\n" +
		"var grouped = (\n1 +\n2\n)\n" +
		"var result = GetBuilder()\n->SetWidth(3)\n.Build()\n"
	want := "vim9script\n" +
		"var list = [\n    1,\n]\n" +
		"var dict = {\n    one: [\n        2,\n    ],\n}\n" +
		"var call = Func(\n    1,\n    2\n)\n" +
		"var indexed = values[\n    0\n]\n" +
		"var grouped = (\n    1 +\n    2\n)\n" +
		"var result = GetBuilder()\n    ->SetWidth(3)\n    .Build()\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("fixture diagnostics = %#v", file.Diagnostics)
	}
	got := applyIndentEdits(t, source, IndentEdits(file, IndentOptions{TabSize: 4, InsertSpaces: true}))
	if got != want {
		t.Fatalf("formatted expressions:\n%s\nwant:\n%s", got, want)
	}
	assertSameFormatShape(t, file, Parse(got))
}

func TestIndentEditsNestExpressionDelimiters(t *testing.T) {
	source := "vim9script\n" +
		"var config = {\nactive: {\nitems: [\n1,\n],\n},\n}\n" +
		"var value = Outer(\nInner(\n1\n)\n)\n"
	want := "vim9script\n" +
		"var config = {\n    active: {\n        items: [\n            1,\n        ],\n    },\n}\n" +
		"var value = Outer(\n    Inner(\n        1\n    )\n)\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("fixture diagnostics = %#v", file.Diagnostics)
	}
	got := applyIndentEdits(t, source, IndentEdits(file, IndentOptions{TabSize: 4, InsertSpaces: true}))
	if got != want {
		t.Fatalf("formatted nested expressions:\n%s\nwant:\n%s", got, want)
	}
	assertSameFormatShape(t, file, Parse(got))
}

func TestIndentEditsCoverEmbeddedCommandsAndLambdaBody(t *testing.T) {
	source := "vim9script\n" +
		"autocmd BufNewFile *.match if true\n" +
		"| echo 1\n" +
		"| endif\n" +
		"filter(list, (k, v) => {\n" +
		"if v\n" +
		"echo v\n" +
		"endif\n" +
		"})\n"
	want := "vim9script\n" +
		"autocmd BufNewFile *.match if true\n" +
		"    | echo 1\n" +
		"    | endif\n" +
		"filter(list, (k, v) => {\n" +
		"    if v\n" +
		"        echo v\n" +
		"    endif\n" +
		"})\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("fixture diagnostics = %#v", file.Diagnostics)
	}
	got := applyIndentEdits(t, source, IndentEdits(file, IndentOptions{TabSize: 4, InsertSpaces: true}))
	if got != want {
		t.Fatalf("formatted embedded source:\n%s\nwant:\n%s", got, want)
	}
}

func TestIndentEditsKeepAmbiguousIncompleteLines(t *testing.T) {
	source := "vim9script\ndef Demo()\nif true\necho 1\n"
	want := "vim9script\ndef Demo()\nif true\n        echo 1\n"
	file := Parse(source)
	if len(file.Diagnostics) == 0 {
		t.Fatal("expected incomplete-input diagnostics")
	}
	got := applyIndentEdits(t, source, IndentEdits(file, IndentOptions{TabSize: 4, InsertSpaces: true}))
	if got != want {
		t.Fatalf("formatted incomplete source = %q, want %q", got, want)
	}
}

func TestIndentEditsProtectEveryDiagnosticLine(t *testing.T) {
	source := "if 1\n echo 1\n  echo 2\nendif\n"
	file := Parse(source)
	file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "test", Span: Span{Start: 5, End: 24}})
	if edits := IndentEdits(file, IndentOptions{TabSize: 4, InsertSpaces: true}); len(edits) != 0 {
		t.Fatalf("diagnostic lines produced edits %#v", edits)
	}
}

func assertIndentEdits(t *testing.T, source string, edits []IndentEdit) {
	t.Helper()
	previousEnd := -1
	for _, edit := range edits {
		if edit.Span.Start < previousEnd || edit.Span.Start < 0 || edit.Span.End < edit.Span.Start || edit.Span.End > len(source) {
			t.Fatalf("invalid or overlapping edits %#v", edits)
		}
		lineStart := strings.LastIndexByte(source[:edit.Span.Start], '\n') + 1
		prefixStart := lineStart
		if lineStart == 0 && strings.HasPrefix(source, "\ufeff") {
			prefixStart = len("\ufeff")
		}
		if edit.Span.Start != prefixStart || strings.Trim(source[edit.Span.Start:edit.Span.End], " \t") != "" {
			t.Fatalf("edit is not a complete leading prefix: %#v in %q", edit, source)
		}
		previousEnd = edit.Span.End
	}
}

func applyIndentEdits(t *testing.T, source string, edits []IndentEdit) string {
	t.Helper()
	for index := len(edits) - 1; index >= 0; index-- {
		edit := edits[index]
		if edit.Span.Start < 0 || edit.Span.End < edit.Span.Start || edit.Span.End > len(source) {
			t.Fatalf("invalid edit %#v", edit)
		}
		source = source[:edit.Span.Start] + edit.NewText + source[edit.Span.End:]
	}
	return source
}

type formatShape struct {
	dialect     Dialect
	commands    []formatCommandShape
	blocks      []Block
	diagnostics []string
}

type formatCommandShape struct {
	kind        CommandKind
	dialect     Dialect
	canonical   string
	block       int
	expressions []ExpressionKind
}

func formatFileShape(file *File) formatShape {
	shape := formatShape{dialect: file.Dialect}
	for index := range file.Commands {
		command := &file.Commands[index]
		item := formatCommandShape{kind: command.Kind, dialect: command.Dialect, canonical: command.Canonical, block: command.Block}
		var collect func(*Expression)
		collect = func(expression *Expression) {
			if expression == nil {
				return
			}
			item.expressions = append(item.expressions, expression.Kind)
			for _, child := range expression.Children {
				collect(child)
			}
		}
		for _, expression := range command.Expressions {
			collect(expression)
		}
		shape.commands = append(shape.commands, item)
	}
	for _, block := range file.Blocks {
		block.Span = Span{}
		shape.blocks = append(shape.blocks, block)
	}
	for _, diagnostic := range file.Diagnostics {
		shape.diagnostics = append(shape.diagnostics, diagnostic.Code)
	}
	return shape
}

func assertSameFormatShape(t *testing.T, before, after *File) {
	t.Helper()
	if left, right := formatFileShape(before), formatFileShape(after); !reflect.DeepEqual(left, right) {
		t.Fatalf("parse shape changed:\n%#v\n%#v", left, right)
	}
}

func TestIndentForLine(t *testing.T) {
	options := IndentOptions{TabSize: 4, InsertSpaces: true}
	tests := []struct {
		name   string
		source string
		line   int
		want   string
		ok     bool
	}{
		{
			name:   "Legacy closed function",
			source: "function! Foo()\n\nendfunction\n",
			line:   1,
			want:   "    ",
			ok:     true,
		},
		{
			name:   "Legacy unclosed function",
			source: "function! Foo()\n\n",
			line:   1,
			want:   "    ",
			ok:     true,
		},
		{
			name:   "Vim9 closed def",
			source: "vim9script\ndef Foo()\n\nenddef\n",
			line:   2,
			want:   "    ",
			ok:     true,
		},
		{
			name:   "Vim9 unclosed def",
			source: "vim9script\ndef Foo()\n\n",
			line:   2,
			want:   "    ",
			ok:     true,
		},
		{
			name:   "Vim9 unclosed if inside def",
			source: "vim9script\ndef Foo()\n    if true\n\n",
			line:   3,
			want:   "        ",
			ok:     true,
		},
		{
			name:   "Legacy unclosed if",
			source: "if 1\n\n",
			line:   1,
			want:   "    ",
			ok:     true,
		},
		{
			name:   "Vim9 closed list bracket",
			source: "vim9script\nvar list = [\n\n]\n",
			line:   2,
			want:   "    ",
			ok:     true,
		},
		{
			name:   "Vim9 unclosed list bracket",
			source: "vim9script\nvar list = [\n\n",
			line:   2,
			want:   "    ",
			ok:     true,
		},
		{
			name:   "Vim9 closed dict bracket",
			source: "vim9script\nvar d = {\n\n}\n",
			line:   2,
			want:   "    ",
			ok:     true,
		},
		{
			name:   "Vim9 unclosed dict bracket",
			source: "vim9script\nvar d = {\n\n",
			line:   2,
			want:   "    ",
			ok:     true,
		},
		{
			name:   "Vim9 closed paren in call",
			source: "vim9script\nFunc(\n\n)\n",
			line:   2,
			want:   "    ",
			ok:     true,
		},
		{
			name:   "Vim9 unclosed paren in call",
			source: "vim9script\nFunc(\n\n",
			line:   2,
			want:   "    ",
			ok:     true,
		},
		{
			name:   "Vim9 multiline function signature unclosed",
			source: "vim9script\ndef Func(\n\n",
			line:   2,
			want:   "        ",
			ok:     true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file := Parse(tc.source)
			got, ok := IndentForLine(file, options, tc.line)
			if ok != tc.ok || got != tc.want {
				t.Logf("commands: %#v", file.Commands)
				if len(file.Commands) > 1 && file.Commands[1].Declaration != nil {
					t.Logf("init: %#v", file.Commands[1].Declaration.Initializer)
				}
				t.Logf("diagnostics: %#v", file.Diagnostics)
				t.Fatalf("IndentForLine() = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}
