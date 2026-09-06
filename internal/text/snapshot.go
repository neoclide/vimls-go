package text

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"unicode/utf16"
	"unicode/utf8"
)

type Encoding string

const (
	UTF8  Encoding = "utf-8"
	UTF16 Encoding = "utf-16"
	UTF32 Encoding = "utf-32"
)

var (
	ErrInvalidEncoding = errors.New("invalid position encoding")
	ErrInvalidPosition = errors.New("invalid text position")
	ErrInvalidRange    = errors.New("invalid text range")
)

type Position struct {
	Line      int
	Character int
}

type Range struct {
	Start Position
	End   Position
}

type Change struct {
	Range *Range
	Text  string
}

// ContentID is the stable SHA-256 identity of complete source text.
type ContentID [sha256.Size]byte

func ContentIDOf(source string) ContentID {
	return sha256.Sum256([]byte(source))
}

type line struct {
	start int
	end   int
}

// Snapshot is an immutable version of one text document.
type Snapshot struct {
	uri        string
	revision   uint64
	version    int32
	hasVersion bool
	text       string
	lines      []line
	contentID  ContentID
}

func NewSnapshot(uri string, revision uint64, version *int32, content string) *Snapshot {
	snapshot := &Snapshot{
		uri:       uri,
		revision:  revision,
		text:      content,
		lines:     indexLines(content),
		contentID: ContentIDOf(content),
	}
	if version != nil {
		snapshot.version = *version
		snapshot.hasVersion = true
	}
	return snapshot
}

func (s *Snapshot) URI() string            { return s.uri }
func (s *Snapshot) Revision() uint64       { return s.revision }
func (s *Snapshot) Text() string           { return s.text }
func (s *Snapshot) ContentID() ContentID   { return s.contentID }
func (s *Snapshot) LineCount() int         { return len(s.lines) }
func (s *Snapshot) ByteLen() int           { return len(s.text) }
func (s *Snapshot) Version() (int32, bool) { return s.version, s.hasVersion }

func (s *Snapshot) Offset(position Position, encoding Encoding) (int, error) {
	if !validEncoding(encoding) {
		return 0, ErrInvalidEncoding
	}
	if position.Line < 0 || position.Line >= len(s.lines) || position.Character < 0 {
		return 0, ErrInvalidPosition
	}
	current := s.lines[position.Line]
	segment := s.text[current.start:current.end]
	byteOffset, err := encodedOffset(segment, position.Character, encoding)
	if err != nil {
		return 0, err
	}
	return current.start + byteOffset, nil
}

func (s *Snapshot) Position(offset int, encoding Encoding) (Position, error) {
	if !validEncoding(encoding) {
		return Position{}, ErrInvalidEncoding
	}
	if offset < 0 || offset > len(s.text) {
		return Position{}, ErrInvalidPosition
	}
	lineIndex := sort.Search(len(s.lines), func(i int) bool {
		return s.lines[i].start > offset
	}) - 1
	if lineIndex < 0 {
		return Position{}, ErrInvalidPosition
	}
	current := s.lines[lineIndex]
	if offset > current.end {
		return Position{}, ErrInvalidPosition
	}
	character, err := encodedLengthAt(s.text[current.start:current.end], offset-current.start, encoding)
	if err != nil {
		return Position{}, err
	}
	return Position{Line: lineIndex, Character: character}, nil
}

func ApplyChanges(snapshot *Snapshot, revision uint64, version *int32, encoding Encoding, changes []Change) (*Snapshot, error) {
	if snapshot == nil {
		return nil, errors.New("nil text snapshot")
	}
	// Keep this editing state private until every ordered change succeeds.
	// Lines are rebuilt lazily: full replacements need no intermediate index.
	current := *snapshot
	for index, change := range changes {
		var content string
		if change.Range == nil {
			content = change.Text
		} else {
			if current.lines == nil {
				current.lines = indexLines(current.text)
			}
			start, err := current.Offset(change.Range.Start, encoding)
			if err != nil {
				return nil, fmt.Errorf("change %d start: %w", index, err)
			}
			end, err := current.Offset(change.Range.End, encoding)
			if err != nil {
				return nil, fmt.Errorf("change %d end: %w", index, err)
			}
			if start > end {
				return nil, fmt.Errorf("change %d: %w", index, ErrInvalidRange)
			}
			content = current.text[:start] + change.Text + current.text[end:]
		}
		current.text = content
		current.lines = nil
	}
	if current.text == snapshot.text {
		current.lines = snapshot.lines
	} else {
		if current.lines == nil {
			current.lines = indexLines(current.text)
		}
		current.contentID = ContentIDOf(current.text)
	}
	current.revision = revision
	current.version, current.hasVersion = 0, version != nil
	if version != nil {
		current.version = *version
	}
	return &current, nil
}

func indexLines(content string) []line {
	lines := make([]line, 0, 1)
	start := 0
	for index := 0; index < len(content); index++ {
		if content[index] != '\n' {
			continue
		}
		end := index
		if end > start && content[end-1] == '\r' {
			end--
		}
		lines = append(lines, line{start: start, end: end})
		start = index + 1
	}
	return append(lines, line{start: start, end: len(content)})
}

func encodedOffset(content string, character int, encoding Encoding) (int, error) {
	units := 0
	for offset := 0; offset < len(content); {
		if units == character {
			return offset, nil
		}
		r, size := utf8.DecodeRuneInString(content[offset:])
		width, err := encodedRuneLength(r, size, encoding)
		if err != nil {
			return 0, err
		}
		if units+width > character {
			return 0, ErrInvalidPosition
		}
		units += width
		offset += size
	}
	// LSP character offsets beyond the line are clamped to its end. Positions
	// inside an encoded rune were rejected above, not rounded to a boundary.
	return len(content), nil
}

func encodedLengthAt(content string, byteOffset int, encoding Encoding) (int, error) {
	if byteOffset < 0 || byteOffset > len(content) {
		return 0, ErrInvalidPosition
	}
	units := 0
	for offset := 0; offset < len(content); {
		if offset == byteOffset {
			return units, nil
		}
		r, size := utf8.DecodeRuneInString(content[offset:])
		if offset+size > byteOffset {
			return 0, ErrInvalidPosition
		}
		width, err := encodedRuneLength(r, size, encoding)
		if err != nil {
			return 0, err
		}
		units += width
		offset += size
	}
	if byteOffset == len(content) {
		return units, nil
	}
	return 0, ErrInvalidPosition
}

func validEncoding(encoding Encoding) bool {
	return encoding == UTF8 || encoding == UTF16 || encoding == UTF32
}

func encodedRuneLength(r rune, byteSize int, encoding Encoding) (int, error) {
	switch encoding {
	case UTF8:
		return byteSize, nil
	case UTF16:
		if r == utf8.RuneError && byteSize == 1 {
			return 1, nil
		}
		return len(utf16.Encode([]rune{r})), nil
	case UTF32:
		return 1, nil
	default:
		return 0, ErrInvalidEncoding
	}
}
