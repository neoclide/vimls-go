package jsonrpc

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestReaderReadsFragmentedAndCoalescedFrames(t *testing.T) {
	input := append(frame([]byte(`{"one":1}`)), frame([]byte(`{"two":2}`))...)
	reader := NewReader(&oneByteReader{reader: bytes.NewReader(input)})

	first, err := reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != `{"one":1}` {
		t.Fatalf("first body = %q", first)
	}
	second, err := reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != `{"two":2}` {
		t.Fatalf("second body = %q", second)
	}
	if _, err := reader.Read(); !errors.Is(err, io.EOF) {
		t.Fatalf("final error = %v, want EOF", err)
	}
}

func TestReaderAcceptsOtherHeadersAndCaseInsensitiveLength(t *testing.T) {
	reader := NewReader(bytes.NewBufferString("content-type: application/vscode-jsonrpc; charset=utf-8\r\ncontent-length: 2\r\n\r\n{}"))
	body, err := reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "{}" {
		t.Fatalf("body = %q", body)
	}
}

func TestReaderRejectsInvalidFrames(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  error
	}{
		{name: "missing length", input: "Content-Type: x\r\n\r\n", want: ErrInvalidHeader},
		{name: "duplicate length", input: "Content-Length: 0\r\nContent-Length: 0\r\n\r\n", want: ErrInvalidHeader},
		{name: "negative length", input: "Content-Length: -1\r\n\r\n", want: ErrInvalidHeader},
		{name: "signed length", input: "Content-Length: +1\r\n\r\nx", want: ErrInvalidHeader},
		{name: "non decimal", input: "Content-Length: nope\r\n\r\n", want: ErrInvalidHeader},
		{name: "malformed field", input: "Content-Length 0\r\n\r\n", want: ErrInvalidHeader},
		{name: "LF header", input: "Content-Length: 0\n\n", want: io.ErrUnexpectedEOF},
		{name: "partial header", input: "Content-Length: 2\r\n", want: io.ErrUnexpectedEOF},
		{name: "partial body", input: "Content-Length: 2\r\n\r\n{", want: io.ErrUnexpectedEOF},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewReader(bytes.NewBufferString(test.input)).Read()
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestReaderEnforcesLimitsBeforeBodyAllocation(t *testing.T) {
	reader := NewReaderWithLimits(bytes.NewBufferString("123456789"), 8, 16)
	if _, err := reader.Read(); !errors.Is(err, ErrHeaderTooLarge) {
		t.Fatalf("header error = %v", err)
	}

	reader = NewReaderWithLimits(bytes.NewBufferString("Content-Length: 17\r\n\r\n"), 64, 16)
	if _, err := reader.Read(); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("message error = %v", err)
	}
}

func TestReaderUsesDefaultsForNonPositiveLimits(t *testing.T) {
	reader := NewReaderWithLimits(bytes.NewReader(frame([]byte("{}"))), 0, 0)
	body, err := reader.Read()
	if err != nil || string(body) != "{}" {
		t.Fatalf("body = %q, error = %v", body, err)
	}
}

func TestReaderReturnsUnderlyingErrors(t *testing.T) {
	want := errors.New("read failed")
	if _, err := NewReader(errorReader{err: want}).Read(); !errors.Is(err, want) {
		t.Fatalf("header error = %v", err)
	}
	input := io.MultiReader(strings.NewReader("Content-Length: 1\r\n\r\n"), errorReader{err: want})
	if _, err := NewReader(input).Read(); !errors.Is(err, want) {
		t.Fatalf("body error = %v", err)
	}
}

func TestWriterProducesFramesAndEnforcesLimit(t *testing.T) {
	var output bytes.Buffer
	writer := NewWriterWithLimit(&output, 2)
	if err := writer.Write([]byte("{}")); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "Content-Length: 2\r\n\r\n{}" {
		t.Fatalf("frame = %q", got)
	}
	if err := writer.Write([]byte("too long")); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("limit error = %v", err)
	}
}

func TestWriterUsesDefaultAndReturnsUnderlyingErrors(t *testing.T) {
	writer := NewWriterWithLimit(errorWriter{}, 0)
	if err := writer.Write(bytes.Repeat([]byte("x"), 5000)); err == nil {
		t.Fatal("large write succeeded")
	}
	if err := writer.Write([]byte("{}")); err == nil {
		t.Fatal("write after failure succeeded")
	}
}

func TestWriterDoesNotInterleaveConcurrentFrames(t *testing.T) {
	var output bytes.Buffer
	writer := NewWriter(&output)
	const count = 32
	var group sync.WaitGroup
	for range count {
		group.Go(func() {
			if err := writer.Write([]byte("{}")); err != nil {
				t.Errorf("write: %v", err)
			}
		})
	}
	group.Wait()

	reader := NewReader(&output)
	for i := range count {
		body, err := reader.Read()
		if err != nil || string(body) != "{}" {
			t.Fatalf("frame %d = %q, %v", i, body, err)
		}
	}
}

func FuzzReader(f *testing.F) {
	f.Add(frame([]byte(`{"jsonrpc":"2.0"}`)))
	f.Add([]byte("Content-Length: -1\r\n\r\n"))
	f.Add([]byte("random bytes"))
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 4<<10 {
			input = input[:4<<10]
		}
		reader := NewReaderWithLimits(bytes.NewReader(input), 256, 1024)
		_, _ = reader.Read()
	})
}

func frame(body []byte) []byte {
	header := []byte("Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n")
	return append(header, body...)
}

type oneByteReader struct {
	reader *bytes.Reader
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (r *oneByteReader) Read(buffer []byte) (int, error) {
	if len(buffer) > 1 {
		buffer = buffer[:1]
	}
	return r.reader.Read(buffer)
}
