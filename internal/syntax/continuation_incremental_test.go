package syntax

import (
	"reflect"
	"strings"
	"testing"
)

func TestContinuationStateResumesAcrossPhysicalLines(t *testing.T) {
	for _, source := range []string{
		"[\n1, # ] | ignored\n2,\n]",
		"true ?\n g:value\n : false ?\n1 : 2",
		"() =>\n{\necho @/\nreturn {key: [1]}\n}",
		"[\n1'000,\n@',\n@/\n]",
		"[\n\"escaped\\\nquote\",\n'literal\nquote',\n]",
		"() => { \"invalid tail\nreturn 1\n}",
	} {
		t.Run(source, func(t *testing.T) {
			var state vim9ContinuationScan
			previous := 0
			for end := 0; end <= len(source); end++ {
				if end != len(source) && source[end] != '\n' {
					continue
				}
				state = scanVim9ContinuationFrom(source[:end], previous, state)
				want := scanVim9Continuation(source[:end], vim9ContinuationScan{})
				if !reflect.DeepEqual(state, want) {
					t.Fatalf("prefix %q: incremental %#v; full %#v", source[:end], state, want)
				}
				previous = end
			}
		})
	}
}

func TestLongAutomaticContinuationBoundaries(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n", "\r"} {
		source := "vim9script\nvar values = [\n" + strings.Repeat("  'a|#', # closing ] is a comment\n", 512) + "] | echo len(values)\nvar after = 1\n"
		file := Parse(strings.ReplaceAll(source, "\n", newline))
		if len(file.Diagnostics) != 0 {
			t.Fatalf("newline %q: diagnostics = %#v", newline, file.Diagnostics)
		}
		if len(file.Commands) != 4 {
			t.Fatalf("commands = %d", len(file.Commands))
		}
		values := file.Commands[1].Declaration.Initializer
		if len(values.Children) != 512 || file.Commands[2].Canonical != "echo" || file.Commands[3].Canonical != "var" {
			t.Fatalf("lost items or following commands")
		}
		comments := 0
		for _, token := range file.Tokens {
			if token.Kind == TokenComment {
				comments++
			}
		}
		if comments != 512 {
			t.Fatalf("comments = %d", comments)
		}
	}
}

func TestContinuationBuilderKeepsPublishedPrefixes(t *testing.T) {
	view, _ := startLogicalView("first\nsecond", 0)
	view.appendSynthetic('\n', 5)
	view.appendSource("first\nsecond", 6, 12)
	view.finishText()
	prefix := view.Text
	for range 1024 {
		view.appendSynthetic(' ', 12)
		view.finishText()
	}
	if prefix != "first\nsecond" {
		t.Fatalf("published prefix changed: %q", prefix)
	}
	for i := range 5 {
		if view.byteSpan(i) != (Span{i, i + 1}) {
			t.Fatalf("identity segment at %d", i)
		}
	}
	if view.byteSpan(5) != (Span{5, 5}) || view.byteSpan(6) != (Span{6, 7}) {
		t.Fatal("mapped segment boundaries changed")
	}
}

func TestInterpolatedStringScanAcrossPhysicalLines(t *testing.T) {
	for _, source := range []string{
		`$"{[
1,
2
]}"`,
		`$'{{literal}} { {'key': 'value'} }'`,
		"$\"{[\n'inner',\n\"escaped\\\ntext\"\n]}\" tail",
		"$\"unterminated {\n{'key': 1}\n",
	} {
		// The outer quote is supplied by the expression lexer.
		source = strings.ReplaceAll(source, `\n`, "\n")
		scan := vim9InterpolationScan{next: 2, quote: source[1]}
		for end := 2; end <= len(source); end++ {
			if end != len(source) && source[end] != '\n' {
				continue
			}
			got, done := scan.scan(source[:end])
			full := vim9InterpolationScan{next: 2, quote: source[1]}
			want, complete := full.scan(source[:end])
			if got != want || done != complete {
				t.Fatalf("prefix %q: (%d,%v), want (%d,%v)", source[:end], got, done, want, complete)
			}
			if done {
				break
			}
		}
	}
}
