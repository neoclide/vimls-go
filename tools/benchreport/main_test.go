package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestPercentileIndexNearestRank(t *testing.T) {
	tests := []struct {
		n    int
		p    float64
		want int
	}{
		{n: 1, p: 0.95, want: 0},
		{n: 1, p: 0.50, want: 0},
		{n: 1, p: 0.00, want: 0},
		{n: 1, p: 1.00, want: 0},

		{n: 5, p: 0.50, want: 2}, // 3rd item
		{n: 5, p: 0.95, want: 4}, // 5th item

		{n: 10, p: 0.50, want: 4}, // 5th item
		{n: 10, p: 0.90, want: 8}, // 9th item
		{n: 10, p: 0.95, want: 9}, // 10th item
		{n: 10, p: 1.00, want: 9},

		{n: 20, p: 0.50, want: 9},  // 10th item
		{n: 20, p: 0.95, want: 18}, // 19th item (not 20th!)
		{n: 20, p: 1.00, want: 19},

		{n: 100, p: 0.95, want: 94}, // 95th item
		{n: 100, p: 0.99, want: 98}, // 99th item
	}

	for _, tt := range tests {
		got := percentileIndex(tt.n, tt.p)
		if got != tt.want {
			t.Errorf("percentileIndex(%d, %.2f) = %d, want %d", tt.n, tt.p, got, tt.want)
		}
	}
}

func TestPercentileCalculations(t *testing.T) {
	// N = 5 samples: 1.0, 2.0, 3.0, 4.0, 5.0 (count=5 scheduled benchmark lane)
	fiveFloats := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
	fiveInts := []int64{1, 2, 3, 4, 5}
	if got := percentileFloat(fiveFloats, 0.95); got != 5.0 {
		t.Errorf("five samples percentileFloat(0.95) = %f, want 5.0", got)
	}
	if got := percentileInt(fiveInts, 0.95); got != 5 {
		t.Errorf("five samples percentileInt(0.95) = %d, want 5", got)
	}
	if got := percentileFloat(fiveFloats, 0.50); got != 3.0 {
		t.Errorf("five samples percentileFloat(0.50) = %f, want 3.0", got)
	}
	if got := percentileInt(fiveInts, 0.50); got != 3 {
		t.Errorf("five samples percentileInt(0.50) = %d, want 3", got)
	}

	// N = 20 samples: 1.0, 2.0, ..., 20.0
	floatVals := make([]float64, 20)
	intVals := make([]int64, 20)
	for i := range 20 {
		floatVals[i] = float64(i + 1)
		intVals[i] = int64(i + 1)
	}

	// P95 of 1..20 with nearest-rank should be 19.0 (19th element)
	if got := percentileFloat(floatVals, 0.95); got != 19.0 {
		t.Errorf("percentileFloat(0.95) = %f, want 19.0", got)
	}
	if got := percentileInt(intVals, 0.95); got != 19 {
		t.Errorf("percentileInt(0.95) = %d, want 19", got)
	}

	// Median of 1..20 should be 10.0 (10th element)
	if got := percentileFloat(floatVals, 0.50); got != 10.0 {
		t.Errorf("percentileFloat(0.50) = %f, want 10.0", got)
	}
	if got := percentileInt(intVals, 0.50); got != 10 {
		t.Errorf("percentileInt(0.50) = %d, want 10", got)
	}

	// Empty slices return 0
	if got := percentileFloat(nil, 0.95); got != 0 {
		t.Errorf("empty percentileFloat = %f, want 0", got)
	}
	if got := percentileInt(nil, 0.95); got != 0 {
		t.Errorf("empty percentileInt = %d, want 0", got)
	}
}

func TestRunSuccess(t *testing.T) {
	rawInput := `
BenchmarkParseLargeFile-8         	      10	  15234567 ns/op	 5000000 B/op	   12000 allocs/op
BenchmarkParseLargeFile-8         	      10	  14234567 ns/op	 5000000 B/op	   12000 allocs/op
BenchmarkParseLargeFile-8         	      10	  16234567 ns/op	 5000000 B/op	   12000 allocs/op
`
	var out bytes.Buffer
	err := runWithBaseline(strings.NewReader(rawInput), nil, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	res := out.String()
	if !strings.Contains(res, "BenchmarkParseLargeFile") {
		t.Fatalf("expected output to contain benchmark name, got: %s", res)
	}
	if !strings.Contains(res, "15.23ms") {
		t.Fatalf("expected output to contain median duration 15.23ms, got: %s", res)
	}
}

func TestRunNoSamples(t *testing.T) {
	var out bytes.Buffer
	err := runWithBaseline(strings.NewReader("some unrelated text\nPASS\n"), nil, &out)
	if err == nil || !strings.Contains(err.Error(), "no benchmark samples") {
		t.Fatalf("missing samples error = %v", err)
	}
}

func TestRegressionGate(t *testing.T) {
	input := func(name string, count int, ns float64, bytes, allocations int) string {
		return strings.Repeat(fmt.Sprintf("%s-8 10 %.2f ns/op %d B/op %d allocs/op\n", name, ns, bytes, allocations), count)
	}
	base := input("BenchmarkParseLargeFile", 5, 100, 100, 10)
	for _, test := range []struct {
		name, current, baseline string
		fail                    bool
	}{
		{"unchanged", base, base, false},
		{"threshold", input("BenchmarkParseLargeFile", 5, 115, 120, 12), base, false},
		{"time", input("BenchmarkParseLargeFile", 5, 116, 100, 10), base, true},
		{"bytes", input("BenchmarkParseLargeFile", 5, 100, 121, 10), base, true},
		{"allocations", input("BenchmarkParseLargeFile", 5, 100, 100, 13), base, true},
		{"p95", base + input("BenchmarkParseLargeFile", 1, 1000, 100, 10), base, true},
		{"missing samples", input("BenchmarkParseLargeFile", 4, 100, 100, 10), base, true},
		{"missing workload", input("BenchmarkOther", 5, 100, 100, 10), base, true},
		{"empty baseline", base, "PASS\n", true},
		{"empty current", "PASS\n", base, true},
		{"malformed sample", base + "BenchmarkParseLargeFile broken\n", base, true},
		{"missing allocation metrics", strings.Repeat("BenchmarkParseLargeFile-8 10 100 ns/op\n", 5), base, true},
		{"completion budget", input("BenchmarkCompletionLatency/small", 5, 100e6, 0, 0), input("BenchmarkCompletionLatency/small", 5, 100e6, 0, 0), true},
		{"zero allocation baseline", input("BenchmarkParseLargeFile", 5, 100, 1, 1), input("BenchmarkParseLargeFile", 5, 100, 0, 0), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := runWithBaseline(strings.NewReader(test.current), strings.NewReader(test.baseline), io.Discard)
			if (err != nil) != test.fail {
				t.Fatalf("error = %v, want failure %v", err, test.fail)
			}
		})
	}
}

func TestRunScannerError(t *testing.T) {
	var out bytes.Buffer
	errReader := iotest.ErrReader(errors.New("simulated read failure"))
	err := runWithBaseline(errReader, nil, &out)
	if err == nil {
		t.Fatal("expected scanner error, got nil")
	}
	if !strings.Contains(err.Error(), "scanner error") {
		t.Fatalf("expected 'scanner error' in message, got: %v", err)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		ns   float64
		want string
	}{
		{50.4, "50.4ns"},
		{1234.0, "1.23µs"},
		{2500000.0, "2.50ms"},
		{3200000000.0, "3.20s"},
	}
	for _, tc := range cases {
		if got := formatDuration(tc.ns); got != tc.want {
			t.Errorf("formatDuration(%f) = %q, want %q", tc.ns, got, tc.want)
		}
	}
}
