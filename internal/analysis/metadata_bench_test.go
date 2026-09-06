package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

var benchmarkOptionAnalysis *FileAnalysis

func BenchmarkOptionHeavyAnalysis(b *testing.B) {
	file := syntax.Parse("vim9script\n" + strings.Repeat("set number tabstop=4 ambiwidth=single\necho &number &l:tabstop &ambiwidth\necho abs(-1) len(v:argv)\n", 100))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		benchmarkOptionAnalysis = Analyze(file)
	}
}
