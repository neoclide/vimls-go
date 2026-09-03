package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var benchLineRe = regexp.MustCompile(`^(Benchmark\S+)\s+(\d+)\s+([0-9.]+)\s+ns/op(?:\s+([0-9.]+)\s+MB/s)?(?:\s+(\d+)\s+B/op)?(?:\s+(\d+)\s+allocs/op)?`)

type sample struct {
	nsPerOp     float64
	bytesPerOp  int64
	allocsPerOp int64
}

func main() {
	inputFile := flag.String("input", "", "input file containing benchmark output (defaults to stdin)")
	flag.Parse()

	var r io.Reader = os.Stdin
	if *inputFile != "" {
		f, err := os.Open(*inputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "benchreport: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		r = f
	}

	if err := run(r, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "benchreport: %v\n", err)
		os.Exit(1)
	}
}

func run(r io.Reader, w io.Writer) error {
	samplesByName := make(map[string][]sample)
	var order []string

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		matches := benchLineRe.FindStringSubmatch(line)
		if len(matches) == 0 {
			continue
		}
		name := matches[1]
		if idx := strings.LastIndex(name, "-"); idx != -1 {
			if _, err := strconv.Atoi(name[idx+1:]); err == nil {
				name = name[:idx]
			}
		}

		ns, _ := strconv.ParseFloat(matches[3], 64)
		var bytesOp, allocsOp int64
		if len(matches) > 5 && matches[5] != "" {
			bytesOp, _ = strconv.ParseInt(matches[5], 10, 64)
		}
		if len(matches) > 6 && matches[6] != "" {
			allocsOp, _ = strconv.ParseInt(matches[6], 10, 64)
		}

		if _, exists := samplesByName[name]; !exists {
			order = append(order, name)
		}
		samplesByName[name] = append(samplesByName[name], sample{
			nsPerOp:     ns,
			bytesPerOp:  bytesOp,
			allocsPerOp: allocsOp,
		})
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	if len(order) == 0 {
		fmt.Fprintln(w, "No benchmark samples found.")
		return nil
	}

	fmt.Fprintln(w, "=== Benchmark Summary (Median & P95) ===")
	fmt.Fprintf(w, "%-45s %8s %14s %14s %12s %12s\n", "Benchmark", "Samples", "Median (time)", "P95 (time)", "Median (B/op)", "Median (allocs)")
	fmt.Fprintln(w, strings.Repeat("-", 115))

	for _, name := range order {
		samples := samplesByName[name]
		n := len(samples)

		nsList := make([]float64, n)
		bList := make([]int64, n)
		aList := make([]int64, n)
		for i, s := range samples {
			nsList[i] = s.nsPerOp
			bList[i] = s.bytesPerOp
			aList[i] = s.allocsPerOp
		}
		sort.Float64s(nsList)
		sort.Slice(bList, func(i, j int) bool { return bList[i] < bList[j] })
		sort.Slice(aList, func(i, j int) bool { return aList[i] < aList[j] })

		medianNs := percentileFloat(nsList, 0.50)
		p95Ns := percentileFloat(nsList, 0.95)
		medianB := percentileInt(bList, 0.50)
		medianA := percentileInt(aList, 0.50)

		fmt.Fprintf(w, "%-45s %8d %14s %14s %12d %12d\n",
			name, n, formatDuration(medianNs), formatDuration(p95Ns), medianB, medianA)
	}
	return nil
}

func percentileIndex(n int, p float64) int {
	if n <= 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(n))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return idx
}

func percentileFloat(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[percentileIndex(len(sorted), p)]
}

func percentileInt(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[percentileIndex(len(sorted), p)]
}

func formatDuration(ns float64) string {
	if ns >= 1e9 {
		return fmt.Sprintf("%.2fs", ns/1e9)
	}
	if ns >= 1e6 {
		return fmt.Sprintf("%.2fms", ns/1e6)
	}
	if ns >= 1e3 {
		return fmt.Sprintf("%.2fµs", ns/1e3)
	}
	return fmt.Sprintf("%.1fns", ns)
}
