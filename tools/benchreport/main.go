package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

var benchLineRe = regexp.MustCompile(`^(Benchmark\S+)\s+(\d+)\s+([0-9.]+)\s+ns/op(?:\s+([0-9.]+)\s+MB/s)?(?:\s+(\d+)\s+B/op)?(?:\s+(\d+)\s+allocs/op)?`)

type sample struct {
	nsPerOp             float64
	bytesPerOp          int64
	allocsPerOp         int64
	allocationsReported bool
}

func main() {
	inputFile := flag.String("input", "", "input file containing benchmark output (defaults to stdin)")
	baselineFile := flag.String("baseline", "", "baseline output; enables the five-sample regression gate")
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

	var baseline io.Reader
	if *baselineFile != "" {
		f, err := os.Open(*baselineFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "benchreport: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		baseline = f
	}
	if err := runWithBaseline(r, baseline, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "benchreport: %v\n", err)
		os.Exit(1)
	}
}

func readSamples(r io.Reader) (map[string][]sample, []string, error) {
	samplesByName := make(map[string][]sample)
	var order []string

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		matches := benchLineRe.FindStringSubmatch(line)
		if len(matches) == 0 {
			if strings.HasPrefix(line, "Benchmark") {
				return nil, nil, fmt.Errorf("malformed benchmark sample: %s", line)
			}
			continue
		}
		name := matches[1]
		if idx := strings.LastIndex(name, "-"); idx != -1 {
			if _, err := strconv.Atoi(name[idx+1:]); err == nil {
				name = name[:idx]
			}
		}

		ns, err := strconv.ParseFloat(matches[3], 64)
		if err != nil || ns <= 0 || math.IsInf(ns, 0) {
			return nil, nil, fmt.Errorf("invalid timing for %s", name)
		}
		iterations, err := strconv.ParseUint(matches[2], 10, 64)
		if err != nil || iterations == 0 {
			return nil, nil, fmt.Errorf("invalid iteration count for %s", name)
		}
		var bytesOp, allocsOp int64
		if len(matches) > 5 && matches[5] != "" {
			bytesOp, err = strconv.ParseInt(matches[5], 10, 64)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid B/op for %s", name)
			}
		}
		if len(matches) > 6 && matches[6] != "" {
			allocsOp, err = strconv.ParseInt(matches[6], 10, 64)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid allocs/op for %s", name)
			}
		}

		if _, exists := samplesByName[name]; !exists {
			order = append(order, name)
		}
		samplesByName[name] = append(samplesByName[name], sample{
			nsPerOp:             ns,
			bytesPerOp:          bytesOp,
			allocsPerOp:         allocsOp,
			allocationsReported: matches[5] != "" && matches[6] != "",
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("scanner error: %w", err)
	}

	if len(order) == 0 {
		return nil, nil, fmt.Errorf("no benchmark samples found")
	}
	return samplesByName, order, nil
}

func runWithBaseline(r, baseline io.Reader, w io.Writer) error {
	samplesByName, order, err := readSamples(r)
	if err != nil {
		return err
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
		slices.Sort(bList)
		slices.Sort(aList)

		medianNs := percentileFloat(nsList, 0.50)
		p95Ns := percentileFloat(nsList, 0.95)
		medianB := percentileInt(bList, 0.50)
		medianA := percentileInt(aList, 0.50)

		fmt.Fprintf(w, "%-45s %8d %14s %14s %12d %12d\n",
			name, n, formatDuration(medianNs), formatDuration(p95Ns), medianB, medianA)
	}
	if baseline != nil {
		before, _, err := readSamples(baseline)
		if err != nil {
			return fmt.Errorf("baseline: %w", err)
		}
		return compareSamples(samplesByName, before, w)
	}
	return nil
}

func compareSamples(current, baseline map[string][]sample, w io.Writer) error {
	names := make([]string, 0, len(baseline))
	for name := range baseline {
		names = append(names, name)
	}
	for name := range current {
		if _, ok := baseline[name]; !ok {
			return fmt.Errorf("benchmark %s has no baseline; review the workload change", name)
		}
	}
	sort.Strings(names)
	var failures []string
	for _, name := range names {
		before, after := baseline[name], current[name]
		for _, samples := range [][]sample{before, after} {
			for _, sample := range samples {
				if !sample.allocationsReported {
					return fmt.Errorf("%s is missing allocation metrics; use -benchmem", name)
				}
			}
		}
		if len(before) < 5 || len(after) < 5 {
			failures = append(failures, fmt.Sprintf("%s needs five samples on both sides (baseline=%d current=%d)", name, len(before), len(after)))
			continue
		}
		metrics := []struct {
			name  string
			value func(sample) float64
			limit float64
		}{
			{"time", func(s sample) float64 { return s.nsPerOp }, 1.15},
			{"B/op", func(s sample) float64 { return float64(s.bytesPerOp) }, 1.20},
			{"allocs/op", func(s sample) float64 { return float64(s.allocsPerOp) }, 1.20},
		}
		for _, metric := range metrics {
			values := func(samples []sample) []float64 {
				result := make([]float64, len(samples))
				for i, sample := range samples {
					result[i] = metric.value(sample)
				}
				sort.Float64s(result)
				return result
			}
			oldValues, newValues := values(before), values(after)
			percentiles := []float64{0.5}
			if metric.name == "time" {
				percentiles = append(percentiles, 0.95)
			}
			for _, p := range percentiles {
				old, next := percentileFloat(oldValues, p), percentileFloat(newValues, p)
				fmt.Fprintf(w, "%s %s p%.0f: %.2f -> %.2f\n", name, metric.name, p*100, old, next)
				if next > old*metric.limit+1e-9 || metric.name == "time" && strings.HasPrefix(name, "BenchmarkCompletionLatency/") && next >= 100e6 {
					failures = append(failures, fmt.Sprintf("%s %s p%.0f exceeds budget", name, metric.name, p*100))
				}
			}
		}
	}
	if len(failures) != 0 {
		return fmt.Errorf("benchmark regression gate failed:\n%s", strings.Join(failures, "\n"))
	}
	fmt.Fprintln(w, "Benchmark regression gate passed.")
	return nil
}

func percentileIndex(n int, p float64) int {
	if n <= 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(n))) - 1
	idx = max(idx, 0)
	idx = min(idx, n-1)
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
