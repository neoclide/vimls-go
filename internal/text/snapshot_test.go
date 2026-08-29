package text

import (
	"errors"
	"testing"
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

	replaced, err := ApplyChanges(updated, 3, &newVersion, UTF16, []Change{{Text: "all"}, {Range: &Range{Start: Position{Character: 3}, End: Position{Character: 3}}, Text: "!"}})
	if err != nil || replaced.Text() != "all!" {
		t.Fatalf("full replacement = %q, %v", replaced.Text(), err)
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
