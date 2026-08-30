package jsonrpc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

const (
	DefaultMaxHeaderBytes  = 8 << 10
	DefaultMaxMessageBytes = 16 << 20
)

var (
	ErrInvalidHeader   = errors.New("invalid JSON-RPC header")
	ErrHeaderTooLarge  = errors.New("JSON-RPC header is too large")
	ErrMessageTooLarge = errors.New("JSON-RPC message is too large")
)

// Reader reads LSP-style JSON-RPC frames from a byte stream.
type Reader struct {
	reader          *bufio.Reader
	maxHeaderBytes  int
	maxMessageBytes int
}

func NewReader(input io.Reader) *Reader {
	return NewReaderWithLimits(input, DefaultMaxHeaderBytes, DefaultMaxMessageBytes)
}

func NewReaderWithLimits(input io.Reader, maxHeaderBytes, maxMessageBytes int) *Reader {
	if maxHeaderBytes <= 0 {
		maxHeaderBytes = DefaultMaxHeaderBytes
	}
	if maxMessageBytes <= 0 {
		maxMessageBytes = DefaultMaxMessageBytes
	}
	return &Reader{
		reader:          bufio.NewReader(input),
		maxHeaderBytes:  maxHeaderBytes,
		maxMessageBytes: maxMessageBytes,
	}
}

func (r *Reader) Read() ([]byte, error) {
	body, _, err := r.read()
	return body, err
}

func (r *Reader) read() ([]byte, int64, error) {
	header, err := r.readHeader()
	if err != nil {
		return nil, 0, err
	}

	contentLength, err := parseContentLength(header)
	if err != nil {
		return nil, 0, err
	}
	if contentLength > uint64(r.maxMessageBytes) {
		return nil, 0, fmt.Errorf("%w: %d bytes exceeds %d", ErrMessageTooLarge, contentLength, r.maxMessageBytes)
	}

	body := make([]byte, int(contentLength))
	if _, err := io.ReadFull(r.reader, body); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, 0, io.ErrUnexpectedEOF
		}
		return nil, 0, err
	}
	return body, int64(len(header)+4) + int64(contentLength), nil
}

func (r *Reader) readHeader() ([]byte, error) {
	header := make([]byte, 0, 128)
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) && len(header) == 0 {
				return nil, io.EOF
			}
			if errors.Is(err, io.EOF) {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		header = append(header, b)
		if len(header) > r.maxHeaderBytes {
			return nil, fmt.Errorf("%w: exceeds %d bytes", ErrHeaderTooLarge, r.maxHeaderBytes)
		}
		if len(header) >= 4 && string(header[len(header)-4:]) == "\r\n\r\n" {
			return header[:len(header)-4], nil
		}
	}
}

func parseContentLength(header []byte) (uint64, error) {
	var value string
	found := false
	for line := range strings.SplitSeq(string(header), "\r\n") {
		name, rawValue, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(name) == "" {
			return 0, fmt.Errorf("%w: malformed field", ErrInvalidHeader)
		}
		if !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			continue
		}
		if found {
			return 0, fmt.Errorf("%w: duplicate Content-Length", ErrInvalidHeader)
		}
		found = true
		value = strings.TrimSpace(rawValue)
	}
	if !found || value == "" {
		return 0, fmt.Errorf("%w: missing Content-Length", ErrInvalidHeader)
	}
	for _, b := range []byte(value) {
		if b < '0' || b > '9' {
			return 0, fmt.Errorf("%w: invalid Content-Length", ErrInvalidHeader)
		}
	}
	length, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid Content-Length: %v", ErrInvalidHeader, err)
	}
	return length, nil
}

// Writer writes complete LSP-style JSON-RPC frames. It is safe for concurrent use.
type Writer struct {
	mu              sync.Mutex
	writer          *bufio.Writer
	maxMessageBytes int
}

func NewWriter(output io.Writer) *Writer {
	return NewWriterWithLimit(output, DefaultMaxMessageBytes)
}

func NewWriterWithLimit(output io.Writer, maxMessageBytes int) *Writer {
	if maxMessageBytes <= 0 {
		maxMessageBytes = DefaultMaxMessageBytes
	}
	return &Writer{writer: bufio.NewWriter(output), maxMessageBytes: maxMessageBytes}
}

func (w *Writer) Write(body []byte) error {
	if len(body) > w.maxMessageBytes {
		return fmt.Errorf("%w: %d bytes exceeds %d", ErrMessageTooLarge, len(body), w.maxMessageBytes)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := fmt.Fprintf(w.writer, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	if _, err := w.writer.Write(body); err != nil {
		return err
	}
	return w.writer.Flush()
}

func frameSize(bodyBytes int) int64 {
	return int64(len("Content-Length: ") + len(strconv.Itoa(bodyBytes)) + 4 + bodyBytes)
}
