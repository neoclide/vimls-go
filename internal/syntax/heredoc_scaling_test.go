package syntax

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkParseMissingHeredocs(b *testing.B) {
	for _, count := range []int{256, 512, 1024, 2048} {
		for _, distinct := range []bool{false, true} {
			b.Run(fmt.Sprintf("count=%d/distinct=%v", count, distinct), func(b *testing.B) {
				var source strings.Builder
				for i := range count {
					marker := 0
					if distinct {
						marker = i
					}
					fmt.Fprintf(&source, "let value =<< END%d\n\n", marker)
				}
				input := source.String()
				b.ReportAllocs()
				b.SetBytes(int64(len(input)))
				b.ResetTimer()
				for b.Loop() {
					benchmarkParsedFile = Parse(input)
				}
			})
		}
	}
}

func TestMissingHeredocsRecoverIndependently(t *testing.T) {
	for _, distinct := range []bool{false, true} {
		var source strings.Builder
		for i := range 512 {
			marker := 0
			if distinct {
				marker = i
			}
			fmt.Fprintf(&source, "  let value =<< trim END%d\npayload\n\n", marker)
		}
		source.WriteString("echo 'done'\n")
		file := Parse(source.String())
		assertFileSpans(t, file)
		if len(file.Commands) != 513 || file.Commands[512].Canonical != "echo" {
			t.Fatalf("commands=%d", len(file.Commands))
		}
		for i := range 512 {
			command := file.Commands[i]
			if command.Heredoc == nil || !command.Heredoc.Incomplete || file.Text(command.Heredoc.Body) != "payload" {
				t.Fatalf("command %d: heredoc=%#v", i, command.Heredoc)
			}
		}
		if len(file.Diagnostics) != 512 {
			t.Fatalf("diagnostics=%d", len(file.Diagnostics))
		}
		for _, diagnostic := range file.Diagnostics {
			if diagnostic.Code != "vim/E990" {
				t.Fatalf("diagnostic=%#v", diagnostic)
			}
		}
	}
}

func TestHeredocRecoveryKeepsDistantExactTrimMarker(t *testing.T) {
	source := "  let value =<< trim END\n" + strings.Repeat("\npayload\n", 1500) + "  END\necho value\n"
	file := Parse(source)
	if len(file.Commands) != 2 || len(file.Diagnostics) != 0 || file.Commands[0].Heredoc == nil || file.Commands[0].Heredoc.Incomplete {
		t.Fatalf("commands=%d diagnostics=%#v", len(file.Commands), file.Diagnostics)
	}
	if got := file.Text(file.Commands[0].Heredoc.EndMarker); got != "  END" {
		t.Fatalf("end marker=%q", got)
	}
}

func TestHeredocLineIndexUsesPositionsAndExactIndent(t *testing.T) {
	source := "END\r\n END\r\tEND\n  END\n}\n"
	index := indexHeredocLines(source)
	for _, test := range []struct {
		marker heredocMarker
		start  int
		want   bool
	}{
		{heredocMarker{name: "END"}, 0, true},
		{heredocMarker{name: "END"}, 5, false},
		{heredocMarker{name: "END", indent: " "}, 5, true},
		{heredocMarker{name: "END", indent: " "}, 10, false},
		{heredocMarker{name: "END", indent: "\t"}, 10, true},
		{heredocMarker{name: "END", indent: "  "}, 15, true},
		{heredocMarker{name: "END", indent: "  "}, len(source), false},
		{heredocMarker{name: "MISSING", indent: "  "}, 0, false},
	} {
		if got := index.hasMarkerAfter(test.marker, test.start); got != test.want {
			t.Fatalf("%#v: got %v", test, got)
		}
	}
}
