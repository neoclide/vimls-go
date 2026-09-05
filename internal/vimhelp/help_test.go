package vimhelp

import (
	"strings"
	"testing"
)

func TestToMarkdownPreservesAngleBracketExamples(t *testing.T) {
	source := "Example: >html\n  <meta>\n  <C-D>\n  <>\n  <\nFollowing prose."
	want := "Example:\n```html\n  <meta>\n  <C-D>\n  <>\n```\n\nFollowing prose."
	if got := ToMarkdown(source); got != want {
		t.Fatalf("Markdown = %q, want %q", got, want)
	}
}

func TestToMarkdownTabIndentedGreaterThanIsNotExampleStart(t *testing.T) {
	// v9.2.1015 runtime/syntax/help.vim permits a space, not a tab,
	// before the marker. options.txt uses tab-indented > as literal text.
	if got := ToMarkdown("Display:\n\t>\n\t<>\nFollowing prose."); strings.Contains(got, "```") {
		t.Fatalf("spurious example: %s", got)
	}
}

func TestParseTags(t *testing.T) {
	tags, err := ParseTags([]byte("foo()\tbuiltin.txt\t/*foo()*\n'bar'\toptions.txt\t/*'bar'*\n"))
	if err != nil {
		t.Fatal(err)
	}
	if tags["foo()"] != "builtin.txt" || tags["'bar'"] != "options.txt" || len(tags) != 2 {
		t.Fatalf("tags = %#v", tags)
	}
}

func TestParseTagsRejectsDuplicates(t *testing.T) {
	for _, source := range []string{
		"foo()\tbuiltin.txt\t/*foo()*\nfoo()\tbuiltin.txt\t/*foo()*\n",
		"foo()\tbuiltin.txt\t/*foo()*\nfoo()\teval.txt\t/*foo()*\n",
	} {
		if _, err := ParseTags([]byte(source)); err == nil || !strings.Contains(err.Error(), "appears more than once") {
			t.Fatalf("duplicate tag error = %v", err)
		}
	}
}

func TestExtractAndConvertMarkdown(t *testing.T) {
	source := []byte(`foo({expr})                                      *foo()* *oldfoo()*
	Return a |Number| for {expr}.
	Example: >
		echo foo(1) | echo untouched
<		1

bar()                                             *bar()*
	See |foo()|.
==============================================================================
unrelated text
`)
	docs, err := Extract("runtime/doc/builtin.txt", source, []string{"foo()", "oldfoo()", "bar()"})
	if err != nil {
		t.Fatal(err)
	}
	foo := docs["foo()"]
	if foo.Source != "builtin.txt" || foo.Markdown != docs["oldfoo()"].Markdown {
		t.Fatalf("foo documentation = %#v, alias = %#v", foo, docs["oldfoo()"])
	}
	for _, want := range []string{"foo({expr})", "`Number`", "```vim", "echo foo(1) | echo untouched", "```"} {
		if !strings.Contains(foo.Markdown, want) {
			t.Errorf("foo Markdown missing %q:\n%s", want, foo.Markdown)
		}
	}
	if strings.Contains(foo.Markdown, "*foo()*") || strings.Contains(foo.Markdown, "unrelated text") {
		t.Fatalf("foo Markdown retained help markup or escaped its section:\n%s", foo.Markdown)
	}
	if got := docs["bar()"].Markdown; !strings.Contains(got, "`foo()`") || strings.Contains(got, "====") {
		t.Fatalf("bar Markdown = %q", got)
	}
}

func TestToMarkdownHandlesExpectedOutputAndReopensExample(t *testing.T) {
	markdown := ToMarkdown("Examples: >\n\t:echo 1\n<\t1  >\n\t:echo 2\n<\t2")
	if strings.Count(markdown, "```vim") != 2 || strings.Count(markdown, "```") != 4 || !strings.Contains(markdown, "1\n```vim") {
		t.Fatalf("Markdown =\n%s", markdown)
	}
}

func TestExtractSharesDocumentationAcrossAdjacentTagLines(t *testing.T) {
	source := []byte("\t*:one*\n\t*:two*\nShared documentation.\n\n\t*:three*\nThird documentation.\n")
	docs, err := Extract("commands.txt", source, []string{":one", ":two", ":three"})
	if err != nil {
		t.Fatal(err)
	}
	if docs[":one"].Markdown != "Shared documentation." || docs[":two"].Markdown != docs[":one"].Markdown || docs[":three"].Markdown != "Third documentation." {
		t.Fatalf("documentation = %#v", docs)
	}
}

func TestExtractRejectsDuplicateAndMissingTargets(t *testing.T) {
	source := []byte("foo() *foo()*\nDocumentation.\n")
	if _, err := Extract("builtin.txt", source, []string{"foo()", "foo()"}); err == nil || !strings.Contains(err.Error(), "duplicate target tag") {
		t.Fatalf("duplicate target error = %v", err)
	}
	if _, err := Extract("builtin.txt", source, []string{"missing()"}); err == nil || !strings.Contains(err.Error(), "tags missing") {
		t.Fatalf("missing target error = %v", err)
	}
}

func TestToMarkdownEndsExampleAtUnindentedProse(t *testing.T) {
	markdown := ToMarkdown("To disable encryption: >\n\t:set key=\n\nYou can select another method: >\n\t:setlocal cm=xchacha20v2\nUsing it requires support.")
	if strings.Count(markdown, "```vim") != 2 || strings.Count(markdown, "```") != 4 || !strings.Contains(markdown, "```\nYou can select") || !strings.Contains(markdown, "```\nUsing it requires support.") {
		t.Fatalf("Markdown =\n%s", markdown)
	}
}
