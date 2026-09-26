package analysis

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func BenchmarkTraversalAnalysis(b *testing.B) {
	var lambdas, nullReceivers, augroups strings.Builder
	lambdas.WriteString("vim9script\n")
	nullReceivers.WriteString("vim9script\nclass Item\n  var value: number\nendclass\n")
	for index := range 64 {
		fmt.Fprintf(&lambdas, "var Callback%d = () => {\n  var value = 1\n  return value\n}\n", index)
		fmt.Fprintf(&nullReceivers, "var item%d: Item\necho item%d.value\nitem%d = Item.new()\n", index, index, index)
		fmt.Fprintf(&augroups, "augroup group%d\n  autocmd!\n  autocmd User * echo 1\naugroup END\nautocmd group%d\n", index, index)
	}
	for _, test := range []struct{ name, source string }{
		{"NestedBuiltins", "vim9script\n" + strings.Repeat("echo "+strings.Repeat("abs(", 12)+"1"+strings.Repeat(")", 12)+"\n", 4)},
		{"LambdaBodies", lambdas.String()},
		{"NullReceivers", nullReceivers.String()},
		{"Augroups", augroups.String()},
	} {
		b.Run(test.name, func(b *testing.B) {
			file := syntax.Parse(test.source)
			if len(file.Diagnostics) != 0 {
				b.Fatalf("invalid benchmark source: %v", file.Diagnostics)
			}
			b.ReportAllocs()
			for b.Loop() {
				benchmarkOptionAnalysis = Analyze(file)
			}
		})
	}
}

// Set VIMLS_BENCH_SOURCE to the same fixed input on both sides of a comparison.
func BenchmarkRuntimeAnalysis(b *testing.B) {
	path := os.Getenv("VIMLS_BENCH_SOURCE")
	if path == "" {
		b.Skip("VIMLS_BENCH_SOURCE is not set")
	}
	source, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	file := syntax.Parse(string(source))
	if len(file.Diagnostics) != 0 {
		b.Fatalf("invalid benchmark source: %v", file.Diagnostics)
	}
	b.ReportAllocs()
	for b.Loop() {
		benchmarkOptionAnalysis = Analyze(file)
	}
}
