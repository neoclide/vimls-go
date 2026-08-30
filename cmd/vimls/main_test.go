package main

import (
	"context"
	"testing"
)

func TestRunTCPStopsCleanlyWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := runTCP(ctx, "127.0.0.1:0"); code != 0 {
		t.Fatalf("runTCP exit code = %d, want 0", code)
	}
}
