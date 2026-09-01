package server

import (
	"bytes"
	"context"
	"testing"
)

func TestRunContextAndTransportTermination(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if code := New(nil, nil, nil).Run(cancelled); code != 0 {
		t.Fatalf("cancelled run = %d", code)
	}
	for _, test := range []struct {
		input []byte
		code  int
	}{
		{nil, 0},
		{[]byte("Content-Length: invalid\r\n\r\n{}"), 1},
		{[]byte("Content-Length: 8\r\n\r\n{}"), 1},
	} {
		var output, logs bytes.Buffer
		if code := New(bytes.NewReader(test.input), &output, &logs).Run(context.Background()); code != test.code {
			t.Fatalf("transport %q exit = %d, want %d; output=%q logs=%q", test.input, code, test.code, output.String(), logs.String())
		}
	}
}
