package text

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSnapshotPositions(t *testing.T) {
	version := int32(7)
	content := "\ufeffa\t𐐀e\u0301\r\nlast\n"
	snapshot := NewSnapshot("file:///test.vim", 11, &version, content)
	if snapshot.URI() != "file:///test.vim" || snapshot.Revision() != 11 || snapshot.Text() != content || snapshot.LineCount() != 3 || snapshot.ByteLen() != len(content) {
		t.Fatalf("snapshot metadata is incorrect")
	}
	if got, ok := snapshot.Version(); !ok || got != version {
		t.Fatalf("version = %d, %v", got, ok)
	}

	tests := []struct {
		name      string
		encoding  Encoding
		character int
		offset    int
	}{
		{name: "utf8 bom", encoding: UTF8, character: 3, offset: 3},
		{name: "utf8 astral", encoding: UTF8, character: 9, offset: 9},
		{name: "utf16 astral", encoding: UTF16, character: 5, offset: 9},
		{name: "utf16 combining", encoding: UTF16, character: 7, offset: 12},
		{name: "utf32 combining", encoding: UTF32, character: 6, offset: 12},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			offset, err := snapshot.Offset(Position{Character: test.character}, test.encoding)
			if err != nil || offset != test.offset {
				t.Fatalf("offset = %d, %v; want %d", offset, err, test.offset)
			}
			position, err := snapshot.Position(test.offset, test.encoding)
			if err != nil || position != (Position{Character: test.character}) {
				t.Fatalf("position = %#v, %v", position, err)
			}
		})
	}

	lineStart := len("\ufeffa\t𐐀e\u0301\r\n")
	position, err := snapshot.Position(lineStart, UTF16)
	if err != nil || position != (Position{Line: 1}) {
		t.Fatalf("second line position = %#v, %v", position, err)
	}
}

func TestSnapshotContentID(t *testing.T) {
	content := "\ufeffa𐐀e\u0301\r\n"
	version := int32(7)
	first := NewSnapshot("file:///first.vim", 1, &version, content)
	second := NewSnapshot("file:///second.vim", 2, nil, content)

	if got, want := first.ContentID(), ContentIDOf(content); got != want {
		t.Fatalf("first content ID = %x, want %x", got, want)
	}
	if first.ContentID() != second.ContentID() {
		t.Fatalf("equal content IDs differ: %x != %x", first.ContentID(), second.ContentID())
	}
	if first.ContentID() == ContentIDOf(content+"!") {
		t.Fatal("changed content has the same ID")
	}
}

func BenchmarkContentID(b *testing.B) {
	source := strings.Repeat("vim9script\n", 64*1024/len("vim9script\n")+1)[:64*1024]
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	for b.Loop() {
		ContentIDOf(source)
	}
}

func TestSnapshotConcurrentReaders(t *testing.T) {
	const content = "\ufeffa\u0301𐐀\xff\r\nlast"
	version := int32(7)
	snapshot := NewSnapshot("file:///concurrent.vim", 11, &version, content)
	firstLineEnd := len("\ufeffa\u0301𐐀\xff")
	secondLineStart := firstLineEnd + len("\r\n")
	expectedID := ContentIDOf(content)
	tests := []struct {
		encoding Encoding
		position Position
	}{
		{UTF8, Position{Character: 11}},
		{UTF16, Position{Character: 6}},
		{UTF32, Position{Character: 5}},
	}

	ready := make(chan struct{}, len(tests))
	start := make(chan struct{})
	errs := make(chan error, len(tests))
	for _, test := range tests {
		go func() {
			ready <- struct{}{}
			<-start
			if snapshot.URI() != "file:///concurrent.vim" || snapshot.Revision() != 11 || snapshot.Text() != content || snapshot.ByteLen() != len(content) || snapshot.LineCount() != 2 {
				errs <- fmt.Errorf("%s metadata changed", test.encoding)
				return
			}
			if got, ok := snapshot.Version(); !ok || got != version {
				errs <- fmt.Errorf("%s version = %d, %v", test.encoding, got, ok)
				return
			}
			if snapshot.ContentID() != expectedID {
				errs <- fmt.Errorf("%s content ID changed", test.encoding)
				return
			}
			position, err := snapshot.Position(firstLineEnd, test.encoding)
			if err != nil || position != test.position {
				errs <- fmt.Errorf("%s position = %#v, %v", test.encoding, position, err)
				return
			}
			offset, err := snapshot.Offset(position, test.encoding)
			if err != nil || offset != firstLineEnd {
				errs <- fmt.Errorf("%s offset = %d, %v", test.encoding, offset, err)
				return
			}
			position, err = snapshot.Position(secondLineStart, test.encoding)
			if err != nil || position != (Position{Line: 1}) {
				errs <- fmt.Errorf("%s second line = %#v, %v", test.encoding, position, err)
				return
			}
			offset, err = snapshot.Offset(position, test.encoding)
			if err != nil || offset != secondLineStart {
				errs <- fmt.Errorf("%s second line offset = %d, %v", test.encoding, offset, err)
				return
			}
			errs <- nil
		}()
	}
	for range tests {
		<-ready
	}
	close(start)
	for range tests {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	if snapshot.Text() != content || snapshot.ContentID() != expectedID {
		t.Fatal("snapshot changed during concurrent reads")
	}
}

func TestSnapshotRejectsInvalidPositions(t *testing.T) {
	snapshot := NewSnapshot("u", 1, nil, "a𐐀\r\n")
	tests := []struct {
		name     string
		position Position
		encoding Encoding
	}{
		{name: "negative line", position: Position{Line: -1}, encoding: UTF16},
		{name: "missing line", position: Position{Line: 3}, encoding: UTF16},
		{name: "negative character", position: Position{Character: -1}, encoding: UTF16},
		{name: "past end", position: Position{Character: 4}, encoding: UTF16},
		{name: "middle utf8", position: Position{Character: 2}, encoding: UTF8},
		{name: "middle utf16", position: Position{Character: 2}, encoding: UTF16},
		{name: "unknown encoding", position: Position{}, encoding: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := snapshot.Offset(test.position, test.encoding); err == nil {
				t.Fatal("invalid position succeeded")
			}
		})
	}
	for _, offset := range []int{-1, 2, 6, len(snapshot.Text()) + 1} {
		if _, err := snapshot.Position(offset, UTF16); !errors.Is(err, ErrInvalidPosition) {
			t.Fatalf("Position(%d) error = %v", offset, err)
		}
	}
	if _, err := snapshot.Position(0, "unknown"); !errors.Is(err, ErrInvalidEncoding) {
		t.Fatalf("unknown encoding error = %v", err)
	}
}

func TestApplyChangesInOrder(t *testing.T) {
	oldVersion := int32(1)
	newVersion := int32(2)
	snapshot := NewSnapshot("file:///x.vim", 1, &oldVersion, "one 𐐀\r\ntwo")
	updated, err := ApplyChanges(snapshot, 2, &newVersion, UTF16, []Change{
		{Range: &Range{Start: Position{Character: 4}, End: Position{Character: 6}}, Text: "X"},
		{Range: &Range{Start: Position{Line: 1}, End: Position{Line: 1, Character: 3}}, Text: "three"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Text() != "one X\r\nthree" || updated.Revision() != 2 {
		t.Fatalf("updated = %q revision %d", updated.Text(), updated.Revision())
	}
	if got, _ := updated.Version(); got != 2 {
		t.Fatalf("version = %d", got)
	}
	if snapshot.Text() != "one 𐐀\r\ntwo" {
		t.Fatal("original snapshot was mutated")
	}
	if snapshot.LineCount() != 2 {
		t.Fatalf("original line count = %d, want 2", snapshot.LineCount())
	}
	if position, err := snapshot.Position(len("one 𐐀\r\n"), UTF16); err != nil || position != (Position{Line: 1}) {
		t.Fatalf("original line index = %#v, %v", position, err)
	}
	if got := snapshot.ContentID(); got != ContentIDOf("one 𐐀\r\ntwo") {
		t.Fatalf("original content ID = %x", got)
	}

	replaced, err := ApplyChanges(updated, 3, &newVersion, UTF16, []Change{{Text: "all"}, {Range: &Range{Start: Position{Character: 3}, End: Position{Character: 3}}, Text: "!"}})
	if err != nil || replaced.Text() != "all!" {
		t.Fatalf("full replacement = %q, %v", replaced.Text(), err)
	}
	if got, want := replaced.ContentID(), ContentIDOf("all!"); got != want {
		t.Fatalf("replacement content ID = %x, want %x", got, want)
	}
	empty, err := ApplyChanges(replaced, 4, nil, UTF16, nil)
	if err != nil || empty.Text() != "all!" || empty.Revision() != 4 {
		t.Fatalf("empty changes = %#v, %v", empty, err)
	}
}

func TestApplyChangesRejectsInvalidRanges(t *testing.T) {
	snapshot := NewSnapshot("u", 1, nil, "abc")
	_, err := ApplyChanges(snapshot, 2, nil, UTF16, []Change{{Range: &Range{Start: Position{Character: 2}, End: Position{Character: 1}}}})
	if !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("reversed range error = %v", err)
	}
	_, err = ApplyChanges(snapshot, 2, nil, UTF16, []Change{{Range: &Range{Start: Position{Character: 4}}}})
	if !errors.Is(err, ErrInvalidPosition) {
		t.Fatalf("invalid range error = %v", err)
	}
	if _, err := ApplyChanges(nil, 1, nil, UTF16, nil); err == nil {
		t.Fatal("nil snapshot succeeded")
	}
}

func TestIncrementalChangesMatchFullReplacement(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		encoding Encoding
		change   Change
		want     string
	}{
		{name: "ASCII and tab", content: "a\tb\n", encoding: UTF16, change: Change{Range: &Range{Start: Position{Character: 1}, End: Position{Character: 2}}, Text: "X"}, want: "aXb\n"},
		{name: "UTF-8 astral", content: "a𐐀b", encoding: UTF8, change: Change{Range: &Range{Start: Position{Character: 1}, End: Position{Character: 5}}, Text: "X"}, want: "aXb"},
		{name: "UTF-16 astral", content: "a𐐀b", encoding: UTF16, change: Change{Range: &Range{Start: Position{Character: 1}, End: Position{Character: 3}}, Text: "X"}, want: "aXb"},
		{name: "UTF-32 astral", content: "a𐐀b", encoding: UTF32, change: Change{Range: &Range{Start: Position{Character: 1}, End: Position{Character: 2}}, Text: "X"}, want: "aXb"},
		{name: "combining mark", content: "e\u0301", encoding: UTF16, change: Change{Range: &Range{Start: Position{Character: 1}, End: Position{Character: 2}}}, want: "e"},
		{name: "CRLF second line", content: "one\r\ntwo", encoding: UTF16, change: Change{Range: &Range{Start: Position{Line: 1}, End: Position{Line: 1, Character: 3}}, Text: "2"}, want: "one\r\n2"},
		{name: "BOM", content: "\ufeffvim9script", encoding: UTF16, change: Change{Range: &Range{End: Position{Character: 1}}}, want: "vim9script"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := NewSnapshot("u", 1, nil, test.content)
			incremental, err := ApplyChanges(base, 2, nil, test.encoding, []Change{test.change})
			if err != nil {
				t.Fatal(err)
			}
			full, err := ApplyChanges(base, 2, nil, test.encoding, []Change{{Text: test.want}})
			if err != nil {
				t.Fatal(err)
			}
			if incremental.Text() != full.Text() {
				t.Fatalf("incremental = %q, full = %q", incremental.Text(), full.Text())
			}
			if incremental.ContentID() != full.ContentID() {
				t.Fatalf("incremental content ID = %x, full = %x", incremental.ContentID(), full.ContentID())
			}
		})
	}
}

func TestInvalidUTF8MakesProgress(t *testing.T) {
	snapshot := NewSnapshot("u", 1, nil, "a\xffb")
	for _, encoding := range []Encoding{UTF8, UTF16, UTF32} {
		position, err := snapshot.Position(2, encoding)
		if err != nil || position != (Position{Character: 2}) {
			t.Fatalf("%s position = %#v, %v", encoding, position, err)
		}
		offset, err := snapshot.Offset(position, encoding)
		if err != nil || offset != 2 {
			t.Fatalf("%s offset = %d, %v", encoding, offset, err)
		}
	}
}

func FuzzPositionRoundTrip(f *testing.F) {
	f.Add("ascii\n𐐀e\u0301", uint8(1), uint16(0))
	f.Add("\ufeff\r\n", uint8(0), uint16(3))
	f.Fuzz(func(t *testing.T, content string, encodingIndex uint8, rawOffset uint16) {
		encodings := []Encoding{UTF8, UTF16, UTF32}
		encoding := encodings[int(encodingIndex)%len(encodings)]
		snapshot := NewSnapshot("fuzz", 1, nil, content)
		offset := int(rawOffset) % (len(content) + 1)
		position, err := snapshot.Position(offset, encoding)
		if err != nil {
			return
		}
		got, err := snapshot.Offset(position, encoding)
		if err != nil || got != offset {
			t.Fatalf("round trip offset %d -> %#v -> %d, %v", offset, position, got, err)
		}
	})
}

func TestApplyChangesDeterministicSequence(t *testing.T) {
	current := NewSnapshot("file:///sequence.vim", 1, nil, "\ufeffAe\u0301B𐐀C\r\nline")
	reference := current.Text()
	edits := []struct {
		encoding   Encoding
		change     Change
		start, end int
	}{
		{UTF8, Change{Range: &Range{Start: Position{}, End: Position{Character: 3}}}, 0, 3},
		{UTF32, Change{Range: &Range{Start: Position{Character: 2}, End: Position{Character: 3}}, Text: "\u0300"}, 2, 4},
		{UTF16, Change{Range: &Range{Start: Position{Character: 4}, End: Position{Character: 6}}, Text: "XY"}, 5, 9},
		{UTF32, Change{Range: &Range{Start: Position{Character: 7}, End: Position{Line: 1}}}, 8, 10},
		{UTF8, Change{Range: &Range{Start: Position{Character: 12}, End: Position{Character: 12}}, Text: "!"}, 12, 12},
	}

	for step, edit := range edits {
		reference = reference[:edit.start] + edit.change.Text + reference[edit.end:]
		version := int32(step + 2)
		updated, err := ApplyChanges(current, uint64(step+2), &version, edit.encoding, []Change{edit.change})
		if err != nil {
			t.Fatalf("step %d: ApplyChanges: %v", step, err)
		}
		full := NewSnapshot(current.URI(), uint64(step+2), &version, reference)
		assertSnapshotMatches(t, updated, full)
		switch step {
		case 0:
			if strings.HasPrefix(updated.Text(), "\ufeff") {
				t.Fatal("step 0: BOM was not removed")
			}
		case 1:
			if got, want := updated.Text(), "Ae\u0300B𐐀C\r\nline"; got != want {
				t.Fatalf("step 1: combining mark replacement = %q, want %q", got, want)
			}
		case 2:
			if got, want := updated.Text(), "Ae\u0300BXYC\r\nline"; got != want {
				t.Fatalf("step 2: UTF-16 astral replacement = %q, want %q", got, want)
			}
		case 3:
			position, err := updated.Position(updated.ByteLen(), UTF32)
			if err != nil || updated.LineCount() != 1 || position != (Position{Character: 11}) {
				t.Fatalf("step 3: CRLF removal line count = %d, end position = %#v, %v", updated.LineCount(), position, err)
			}
		case 4:
			eof := updated.ByteLen()
			position, err := updated.Position(eof, UTF8)
			if err != nil || position != (Position{Character: 13}) {
				t.Fatalf("step 4: EOF position = %#v, %v", position, err)
			}
			offset, err := updated.Offset(position, UTF8)
			if err != nil || offset != eof {
				t.Fatalf("step 4: EOF offset = %d, %v; want %d", offset, err, eof)
			}
		}
		current = updated
	}
}

func FuzzApplyChanges(f *testing.F) {
	f.Add([]byte("a\nb\n"), []byte{utf8Index, 1, 2}, []byte("l"))
	f.Add([]byte("a\r\nb\r\n"), []byte{utf16Index, 1, 2}, []byte("c"))
	f.Add([]byte("\ufeffno final newline"), []byte{utf32Index, 0, 1}, []byte("b"))
	f.Add([]byte("e\u0301"), []byte{utf16Index, 1, 2}, []byte("m"))
	f.Add([]byte("a𐐀b"), []byte{utf32Index, 1, 2}, []byte("a"))
	f.Add([]byte("a\xffb"), []byte{utf8Index, 1, 2}, []byte("i"))

	f.Fuzz(func(t *testing.T, source, operations, replacements []byte) {
		const (
			maxSource       = 256
			maxOperations   = 96
			maxReplacements = 128
			maxSteps        = 32
			replacementSize = 16
		)
		if len(source) > maxSource {
			source = source[:maxSource]
		}
		if len(operations) > maxOperations {
			operations = operations[:maxOperations]
		}
		if len(replacements) > maxReplacements {
			replacements = replacements[:maxReplacements]
		}

		current := NewSnapshot("file:///fuzz.vim", 1, nil, string(source))
		reference := current.Text()
		steps := min(len(operations)/3, maxSteps)
		encodings := []Encoding{UTF8, UTF16, UTF32}
		for step := range steps {
			operation := operations[step*3 : step*3+3]
			encoding := encodings[int(operation[0])%len(encodings)]
			boundaries := fuzzBoundaries(reference, encoding)
			if len(boundaries) == 0 {
				t.Fatal("no valid edit boundaries")
			}
			start := boundaries[int(operation[1])%len(boundaries)]
			end := boundaries[int(operation[2])%len(boundaries)]
			if start.offset > end.offset {
				start, end = end, start
			}
			replacement := fuzzReplacement(replacements, operation[0], replacementSize)
			change := Change{Range: &Range{Start: start.position, End: end.position}, Text: replacement}

			reference = reference[:start.offset] + replacement + reference[end.offset:]
			version := int32(step + 2)
			updated, err := ApplyChanges(current, uint64(step+2), &version, encoding, []Change{change})
			if err != nil {
				t.Fatalf("step %d: ApplyChanges: %v", step, err)
			}
			full := NewSnapshot(current.URI(), uint64(step+2), &version, reference)
			assertSnapshotMatches(t, updated, full)
			current = updated
		}
	})
}

func assertSnapshotMatches(t testing.TB, incremental, full *Snapshot) {
	t.Helper()
	if incremental.Text() != full.Text() {
		t.Fatalf("text = %q, want %q", incremental.Text(), full.Text())
	}
	want := ContentID(sha256.Sum256([]byte(full.Text())))
	if incremental.ContentID() != want {
		t.Fatalf("content ID = %x, want %x", incremental.ContentID(), want)
	}
}

const (
	utf8Index = iota
	utf16Index
	utf32Index
)

type fuzzBoundary struct {
	offset   int
	position Position
}

func fuzzBoundaries(content string, encoding Encoding) []fuzzBoundary {
	boundaries := make([]fuzzBoundary, 0, len(content)+1)
	if position, ok := fuzzPosition(content, 0, encoding); ok {
		boundaries = append(boundaries, fuzzBoundary{position: position})
	}
	for offset := 0; offset < len(content); {
		_, size := utf8.DecodeRuneInString(content[offset:])
		offset += size
		if position, ok := fuzzPosition(content, offset, encoding); ok {
			boundaries = append(boundaries, fuzzBoundary{offset: offset, position: position})
		}
	}
	return boundaries
}

func fuzzPosition(content string, target int, encoding Encoding) (Position, bool) {
	if target < 0 || target > len(content) || (encoding != UTF8 && encoding != UTF16 && encoding != UTF32) {
		return Position{}, false
	}
	for line, start := 0, 0; ; line++ {
		end := len(content)
		next := len(content)
		if newline := strings.IndexByte(content[start:], '\n'); newline >= 0 {
			next = start + newline
			end = next
			if end > start && content[end-1] == '\r' {
				end--
			}
		}
		if target >= start && target <= end {
			character, ok := fuzzCharacterOffset(content[start:end], target-start, encoding)
			return Position{Line: line, Character: character}, ok
		}
		if next == len(content) {
			return Position{}, false
		}
		start = next + 1
	}
}

func fuzzCharacterOffset(content string, target int, encoding Encoding) (int, bool) {
	units := 0
	for offset := 0; offset < len(content); {
		if offset == target {
			return units, true
		}
		r, size := utf8.DecodeRuneInString(content[offset:])
		if offset+size > target {
			return 0, false
		}
		switch encoding {
		case UTF8:
			units += size
		case UTF16:
			if r > 0xffff && !(r == utf8.RuneError && size == 1) {
				units += 2
			} else {
				units++
			}
		case UTF32:
			units++
		}
		offset += size
	}
	return units, target == len(content)
}

func fuzzReplacement(source []byte, seed byte, maxSize int) string {
	if len(source) == 0 {
		return ""
	}
	start := int(seed) % len(source)
	end := min(start+maxSize, len(source))
	return string(source[start:end])
}
