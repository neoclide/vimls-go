package text

import (
	"strings"
	"testing"
)

func TestSnapshotPhysicalNewlineMatrix(t *testing.T) {
	for _, endings := range [][]string{{"\n"}, {"\r\n"}, {"\r"}, {"\r", "\r\n", "\n"}} {
		for _, trailing := range []bool{false, true} {
			lines := []string{"\ufeffa😀é", "", "\xffb", "last"}
			var source strings.Builder
			starts := make([]int, len(lines))
			for i, line := range lines {
				starts[i] = source.Len()
				source.WriteString(line)
				if i+1 < len(lines) || trailing {
					source.WriteString(endings[i%len(endings)])
				}
			}
			if trailing {
				starts = append(starts, source.Len())
				lines = append(lines, "")
			}
			input := source.String()
			snapshot := NewSnapshot("u", 1, nil, input)
			if snapshot.Text() != input || snapshot.ContentID() != ContentIDOf(input) || snapshot.LineCount() != len(lines) {
				t.Fatalf("endings=%q trailing=%v: snapshot changed bytes or line count", endings, trailing)
			}
			for _, encoding := range []Encoding{UTF8, UTF16, UTF32} {
				for i, line := range lines {
					for _, offset := range []int{starts[i], starts[i] + len(line)} {
						position, err := snapshot.Position(offset, encoding)
						if err != nil || position.Line != i {
							t.Fatalf("offset %d: %#v %v", offset, position, err)
						}
						if got, err := snapshot.Offset(position, encoding); err != nil || got != offset {
							t.Fatalf("roundtrip %d: %d %v", offset, got, err)
						}
					}
				}
				changes := []Change{
					{Range: &Range{Start: Position{Line: 1}, End: Position{Line: 2}}, Text: "new\rline\n"},
					{Range: &Range{Start: Position{Line: 2}, End: Position{Line: 3}}, Text: ""},
				}
				got, err := ApplyChanges(snapshot, 2, nil, encoding, changes)
				if err != nil {
					t.Fatal(err)
				}
				want := input[:starts[1]] + "new\r" + input[starts[2]:]
				assertSnapshotMatches(t, got, NewSnapshot("u", 2, nil, want))
			}
		}
	}
}
