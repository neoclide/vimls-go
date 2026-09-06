package syntax

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkParseAutomaticContinuation(b *testing.B) {
	for _, fixture := range []struct{ name, head, line, tail string }{
		{"list", "var values = [", "  1,", "]"},
		{"strings", "var values = [", "  'value|#',", "]"},
		{"leading", "var value = 1", "  + 1", ""},
		{"trailing", "var value = 1 +", "  1 +", "1"},
		{"method", "var value = [1]", "  ->copy()", ""},
		{"signature", "def F(", "  arg: number,", "): number\nreturn 1\nenddef"},
		{"for", "for value in [", "  1,", "]\nendfor"},
		{"lambda", "var F = () => {", "  echo 1", "}"},
		{"lambda_bar", "var F = () => {", "  echo 1 | echo 2", "}"},
		{"ternary", "var value = false ? 1 :", "  false ? 1 :", "0"},
		{"types", "var value: list<", "  list<", "number"},
	} {
		for _, count := range []int{128, 256, 512, 1024} {
			b.Run(fixture.name+"/"+fmt.Sprint(count), func(b *testing.B) {
				tail := fixture.tail
				if fixture.name == "types" {
					tail += strings.Repeat(">\n", count+1)
				}
				source := "vim9script\n" + fixture.head + "\n" + strings.Repeat(fixture.line+"\n", count) + tail + "\n"
				b.ReportAllocs()
				b.SetBytes(int64(len(source)))
				b.ResetTimer()
				for b.Loop() {
					benchmarkParsedFile = Parse(source)
				}
			})
		}
	}
}
