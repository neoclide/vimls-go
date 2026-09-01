package text

import (
	"errors"
	"testing"
)

func TestSnapshotEncodingBoundaryMatrix(t *testing.T) {
	version := int32(4)
	snapshot := NewSnapshot("file:///edge", 9, &version, "a\r\n\xffé𐐀\n")
	if snapshot.LineCount() != 3 || snapshot.ByteLen() == 0 {
		t.Fatal("snapshot metadata")
	}
	for _, encoding := range []Encoding{UTF8, UTF16, UTF32} {
		for _, position := range []Position{{0, 0}, {0, 1}, {1, 0}, {1, 1}, {1, 2}, {1, 3}, {1, 4}, {2, 0}} {
			offset, err := snapshot.Offset(position, encoding)
			if err != nil {
				continue
			}
			got, err := snapshot.Position(offset, encoding)
			if err != nil || got != position {
				t.Errorf("%s %#v => %d => %#v, %v", encoding, position, offset, got, err)
			}
		}
	}
	for _, test := range []struct {
		p    Position
		e    Encoding
		want error
	}{{Position{-1, 0}, UTF8, ErrInvalidPosition}, {Position{3, 0}, UTF8, ErrInvalidPosition}, {Position{1, -1}, UTF8, ErrInvalidPosition}, {Position{0, 0}, Encoding("bad"), ErrInvalidEncoding}} {
		if _, err := snapshot.Offset(test.p, test.e); !errors.Is(err, test.want) {
			t.Errorf("offset %#v/%s = %v", test.p, test.e, err)
		}
	}
	for _, offset := range []int{-1, snapshot.ByteLen() + 1} {
		if _, err := snapshot.Position(offset, UTF8); !errors.Is(err, ErrInvalidPosition) {
			t.Errorf("position %d = %v", offset, err)
		}
	}
	if _, err := snapshot.Position(0, Encoding("bad")); !errors.Is(err, ErrInvalidEncoding) {
		t.Errorf("invalid encoding = %v", err)
	}
	if _, err := ApplyChanges(nil, 1, nil, UTF8, nil); err == nil {
		t.Fatal("nil snapshot accepted")
	}
	if _, err := ApplyChanges(snapshot, 2, nil, UTF8, []Change{{Range: &Range{Start: Position{1, 3}, End: Position{1, 1}}, Text: "x"}}); !errors.Is(err, ErrInvalidRange) {
		t.Errorf("reverse range = %v", err)
	}
	if got, err := ApplyChanges(snapshot, 3, nil, UTF8, nil); err != nil || got.Revision() != 3 || got.Text() != snapshot.Text() {
		t.Errorf("empty changes = %#v, %v", got, err)
	}
}
