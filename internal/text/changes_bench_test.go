package text

import (
	"fmt"
	"strings"
	"testing"
)

var benchmarkChangedSnapshot *Snapshot

func BenchmarkApplyChangesBatch(b *testing.B) {
	for _, count := range []int{0, 1, 32} {
		for _, full := range []bool{false, true} {
			b.Run(fmt.Sprintf("changes=%d/full=%v", count, full), func(b *testing.B) {
				base := NewSnapshot("file:///bench.vim", 1, nil, strings.Repeat("let value = '😀é'\r\n", 4096))
				changes := make([]Change, count)
				for i := range changes {
					changes[i] = Change{Text: "x", Range: &Range{Start: Position{}, End: Position{Character: 1}}}
					if full {
						changes[i] = Change{Text: base.Text()}
					}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					var err error
					benchmarkChangedSnapshot, err = ApplyChanges(base, 2, nil, UTF16, changes)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
