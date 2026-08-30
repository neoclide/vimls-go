package workspace

import (
	"context"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestParseSourcesMatchesSequentialResultsInInputOrder(t *testing.T) {
	sources := []string{
		"let first = 1\n",
		"vim9script\nvar second: number = 2\n",
		"function! Third(value) abort\n  return a:value + 3\nendfunction\n",
		"def Fourth(value: number): number\n  return value + 4\nenddef\n",
		"not a complete command {{{\n",
	}
	want := make([]*syntax.File, len(sources))
	for index, source := range sources {
		want[index] = syntax.Parse(source)
	}

	for _, workers := range []int{1, 2, 4, 0, -1, 100} {
		got := ParseSources(context.Background(), sources, workers)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("workers=%d results differ from sequential parse:\n got: %#v\nwant: %#v", workers, got, want)
		}
		if len(got) != len(sources) {
			t.Fatalf("workers=%d result length = %d, want %d", workers, len(got), len(sources))
		}
	}
}

func TestParseSourcesReturnsNonNilEmptyResults(t *testing.T) {
	got := ParseSources(context.Background(), nil, 0)
	if got == nil || len(got) != 0 {
		t.Fatalf("empty result = %#v, want non-nil empty slice", got)
	}
}

func TestParseWorkerCountCapsRequestedWorkers(t *testing.T) {
	maximum := runtime.GOMAXPROCS(0)
	if maximum > 4 {
		maximum = 4
	}
	cases := []struct {
		name      string
		requested int
		sources   int
		want      int
	}{
		{name: "empty", requested: 4, sources: 0, want: 0},
		{name: "requested one", requested: 1, sources: 10, want: 1},
		{name: "requested two", requested: 2, sources: 10, want: minInt(2, maximum)},
		{name: "default", requested: 0, sources: 10, want: maximum},
		{name: "negative default", requested: -1, sources: 10, want: maximum},
		{name: "source count cap", requested: 100, sources: 2, want: 2},
		{name: "requested cap", requested: 100, sources: 10, want: maximum},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := parseWorkerCount(test.requested, test.sources); got != test.want {
				t.Fatalf("parseWorkerCount(%d, %d) = %d, want %d", test.requested, test.sources, got, test.want)
			}
		})
	}
}

func TestParseSourcesPreCanceledContextLeavesEverySlotNil(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sources := []string{"let first = 1\n", "let second = 2\n", "vim9script\nvar third = 3\n"}

	got := ParseSources(ctx, sources, 4)
	if len(got) != len(sources) {
		t.Fatalf("result length = %d, want %d", len(got), len(sources))
	}
	for index, file := range got {
		if file != nil {
			t.Fatalf("slot %d = %#v, want nil", index, file)
		}
	}
}

func TestParseSourcesCancellationDoesNotBlockProducer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sources := make([]string, 128)
	for index := range sources {
		sources[index] = "let value = " + string(rune('0'+index%10)) + "\n"
	}

	done := make(chan []*syntax.File, 1)
	go func() {
		done <- ParseSources(ctx, sources, 1)
	}()
	select {
	case got := <-done:
		if len(got) != len(sources) {
			t.Fatalf("result length = %d, want %d", len(got), len(sources))
		}
	case <-time.After(time.Second):
		t.Fatal("ParseSources blocked after cancellation")
	}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
