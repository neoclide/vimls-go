package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	profile := flag.String("profile", "coverage.out", "Go coverage profile")
	minimum := flag.Float64("min", 85, "minimum statement coverage percentage")
	flag.Parse()

	covered, total, err := readProfile(*profile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "covercheck: %v\n", err)
		os.Exit(2)
	}
	percentage := 100 * float64(covered) / float64(total)
	fmt.Printf("internal statement coverage: %.2f%% (%d/%d)\n", percentage, covered, total)
	if percentage+1e-9 < *minimum {
		fmt.Fprintf(os.Stderr, "covercheck: %.2f%% is below %.2f%%\n", percentage, *minimum)
		os.Exit(1)
	}
}

func readProfile(path string) (covered, total int64, err error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "mode: ") {
		return 0, 0, fmt.Errorf("invalid coverage profile header")
	}
	type block struct {
		statements int64
		covered    bool
	}
	blocks := make(map[string]block)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 {
			return 0, 0, fmt.Errorf("invalid coverage row %q", scanner.Text())
		}
		statements, parseErr := strconv.ParseInt(fields[1], 10, 64)
		if parseErr != nil || statements < 0 {
			return 0, 0, fmt.Errorf("invalid statement count %q", fields[1])
		}
		count, parseErr := strconv.ParseInt(fields[2], 10, 64)
		if parseErr != nil || count < 0 {
			return 0, 0, fmt.Errorf("invalid execution count %q", fields[2])
		}
		key := fields[0]
		current, exists := blocks[key]
		if exists && current.statements != statements {
			return 0, 0, fmt.Errorf("inconsistent statement count for %q", key)
		}
		blocks[key] = block{statements: statements, covered: current.covered || count > 0}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	for _, current := range blocks {
		total += current.statements
		if current.covered {
			covered += current.statements
		}
	}
	if total == 0 {
		return 0, 0, fmt.Errorf("coverage profile contains no statements")
	}
	return covered, total, nil
}
