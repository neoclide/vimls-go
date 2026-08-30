package jsonrpc

import (
	"context"
	"io"
	"sync"

	jsonrpc2 "go.lsp.dev/jsonrpc2"
)

// Stream adapts the repository's bounded LSP framing to jsonrpc2.Stream.
type Stream struct {
	reader    *Reader
	writer    *Writer
	closer    io.Closer
	closeOnce sync.Once
	closeErr  error
}

func NewStream(input io.Reader, output io.Writer) *Stream {
	stream := &Stream{
		reader: NewReader(input),
		writer: NewWriter(output),
	}
	stream.closer, _ = input.(io.Closer)
	return stream
}

func (s *Stream) Read(ctx context.Context) (jsonrpc2.Message, int64, error) {
	_, message, size, err := s.read(ctx)
	return message, size, err
}

func (s *Stream) ReadFrame(ctx context.Context) ([]byte, int64, error) {
	body, _, size, err := s.read(ctx)
	return body, size, err
}

func (s *Stream) read(ctx context.Context) ([]byte, jsonrpc2.Message, int64, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, 0, err
		}
		body, size, err := s.reader.read()
		if err != nil {
			return nil, nil, 0, err
		}
		if message, decodeErr := jsonrpc2.DecodeMessage(body); decodeErr == nil {
			return body, message, size, nil
		} else {
			response := jsonrpc2.NewResponse(jsonrpc2.ID{}, nil, decodeErr)
			if _, err := s.Write(ctx, response); err != nil {
				return nil, nil, 0, err
			}
		}
	}
}

func (s *Stream) Write(ctx context.Context, message jsonrpc2.Message) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	body, err := jsonrpc2.EncodeMessage(message)
	if err != nil {
		return 0, err
	}
	return s.WriteFrame(ctx, body)
}

func (s *Stream) WriteFrame(ctx context.Context, body []byte) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := s.writer.Write(body); err != nil {
		return 0, err
	}
	return frameSize(len(body)), nil
}

func (s *Stream) Close() error {
	s.closeOnce.Do(func() {
		if s.closer != nil {
			s.closeErr = s.closer.Close()
		}
	})
	return s.closeErr
}

var _ jsonrpc2.Stream = (*Stream)(nil)
