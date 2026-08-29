package benchlegacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"unsafe"

	"github.com/chemzqm/vimls-go/internal/syntax"
	"github.com/chemzqm/vimls-go/internal/workspace"
)

var (
	profileDir     string
	profileWorkers int
	profileRuns    int
)

func init() {
	flag.StringVar(&profileDir, "profile-dir", "", "existing empty directory for isolated parser profiles")
	flag.IntVar(&profileWorkers, "profile-workers", 1, "number of ParseSources workers used by TestProfileVimlsBatch")
	flag.IntVar(&profileRuns, "profile-runs", 5, "number of full corpus parses recorded in the CPU and allocation profiles")
}

// TestProfileVimlsBatch is an explicitly enabled local profiling harness. It
// reads and hashes the corpus before taking the baseline profiles, then records
// only workspace.ParseSources calls. In particular, it does not classify files
// or invoke go-vimlparser, which would make an RTP parser profile misleading.
func TestProfileVimlsBatch(t *testing.T) {
	if profileDir == "" {
		t.Skip("pass -profile-dir to enable the local parser profiler")
	}
	if profileWorkers < 1 || profileWorkers > 4 {
		t.Fatalf("profile-workers = %d, want 1 through 4", profileWorkers)
	}
	if profileRuns < 1 {
		t.Fatalf("profile-runs = %d, want at least 1", profileRuns)
	}
	info, err := os.Stat(profileDir)
	if err != nil {
		t.Fatalf("profile directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("profile-dir %q is not a directory", profileDir)
	}

	paths, err := discoverVimFiles(benchmarkRoots, benchmarkManifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no .vim files found; pass -root or -manifest")
	}
	sources := make([]string, len(paths))
	hash := sha256.New()
	var totalBytes int64
	for index, path := range paths {
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		sources[index] = string(content)
		totalBytes += int64(len(content))
		hash.Write([]byte(path))
		hash.Write([]byte{0})
		hash.Write(content)
		hash.Write([]byte{0})
	}

	runtime.GC()
	writeNamedProfile(t, "heap", "heap-before.pprof")
	writeNamedProfile(t, "allocs", "allocs-before.pprof")

	cpuPath := filepath.Join(profileDir, "cpu.pprof")
	cpuFile, err := openNewProfile(cpuPath)
	if err != nil {
		t.Fatalf("create CPU profile: %v", err)
	}
	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		_ = cpuFile.Close()
		t.Fatalf("start CPU profile: %v", err)
	}

	ctx := context.Background()
	var parsed []*syntax.File
	for range profileRuns {
		parsed = workspace.ParseSources(ctx, sources, profileWorkers)
		if len(parsed) != len(sources) {
			pprof.StopCPUProfile()
			_ = cpuFile.Close()
			t.Fatalf("parsed files = %d, want %d", len(parsed), len(sources))
		}
	}
	pprof.StopCPUProfile()
	if err := cpuFile.Close(); err != nil {
		t.Fatalf("close CPU profile: %v", err)
	}

	runtime.GC()
	runtime.KeepAlive(parsed)
	writeNamedProfile(t, "allocs", "allocs-after.pprof")
	writeNamedProfile(t, "heap", "heap-after.pprof")
	runtime.KeepAlive(parsed)

	topLevel, nested := commandCapacity(parsed)
	t.Logf(
		"profiled corpus sha256=%s files=%d bytes=%d workers=%d runs=%d dir=%s",
		hex.EncodeToString(hash.Sum(nil)), len(sources), totalBytes, profileWorkers, profileRuns, profileDir,
	)
	logCommandCapacity(t, "top-level", topLevel)
	logCommandCapacity(t, "nested", nested)
	logCommandCapacityHints(t, parsed)
}

type commandSliceStats struct {
	slices   int
	length   int
	capacity int
	lengths  [7]int
}

func commandCapacity(files []*syntax.File) (commandSliceStats, commandSliceStats) {
	var topLevel, nested commandSliceStats
	seen := make(map[*syntax.CommandList]bool)
	var addNested func([]syntax.Command)
	addNested = func(commands []syntax.Command) {
		nested.add(commands)
		for index := range commands {
			list := commands[index].Embedded
			if list == nil || seen[list] {
				continue
			}
			seen[list] = true
			addNested(list.Commands)
		}
	}
	for _, file := range files {
		if file == nil {
			continue
		}
		topLevel.add(file.Commands)
		for index := range file.Commands {
			list := file.Commands[index].Embedded
			if list == nil || seen[list] {
				continue
			}
			seen[list] = true
			addNested(list.Commands)
		}
	}
	return topLevel, nested
}

func (stats *commandSliceStats) add(commands []syntax.Command) {
	if cap(commands) == 0 {
		return
	}
	stats.slices++
	stats.length += len(commands)
	stats.capacity += cap(commands)
	switch length := len(commands); {
	case length == 0:
		stats.lengths[0]++
	case length == 1:
		stats.lengths[1]++
	case length <= 4:
		stats.lengths[2]++
	case length <= 8:
		stats.lengths[3]++
	case length <= 16:
		stats.lengths[4]++
	case length <= 64:
		stats.lengths[5]++
	default:
		stats.lengths[6]++
	}
}

func logCommandCapacity(t *testing.T, name string, stats commandSliceStats) {
	t.Helper()
	size := unsafe.Sizeof(syntax.Command{})
	t.Logf(
		"retained %s command slices=%d len=%d cap=%d waste=%d command_size=%d live_bytes=%d capacity_bytes=%d",
		name, stats.slices, stats.length, stats.capacity, stats.capacity-stats.length, size,
		uintptr(stats.length)*size, uintptr(stats.capacity)*size,
	)
	t.Logf(
		"retained %s command slice lengths 0=%d 1=%d 2-4=%d 5-8=%d 9-16=%d 17-64=%d 65+=%d",
		name, stats.lengths[0], stats.lengths[1], stats.lengths[2], stats.lengths[3],
		stats.lengths[4], stats.lengths[5], stats.lengths[6],
	)
}

type commandHintStats struct {
	capacity  int
	covered   int
	over      int
	overFiles int
}

func (stats *commandHintStats) add(actual, hint int) {
	stats.capacity += hint
	if hint > actual {
		stats.covered += actual
		stats.over += hint - actual
		stats.overFiles++
	} else {
		stats.covered += hint
	}
}

func logCommandCapacityHints(t *testing.T, files []*syntax.File) {
	t.Helper()
	var fixed16, bytes256, bytes128, bytes96, lines64, lines128 commandHintStats
	for _, file := range files {
		if file == nil {
			continue
		}
		actual := len(file.Commands)
		fixed16.add(actual, 16)
		bytes256.add(actual, boundedCommandHint(len(file.Source), 256, 64))
		bytes128.add(actual, boundedCommandHint(len(file.Source), 128, 64))
		bytes96.add(actual, boundedCommandHint(len(file.Source), 96, 64))
		lineCount := strings.Count(file.Source, "\n") + 1
		lines64.add(actual, min(lineCount, 64))
		lines128.add(actual, min(lineCount, 128))
	}
	for _, candidate := range []struct {
		name  string
		stats commandHintStats
	}{
		{name: "fixed-16", stats: fixed16},
		{name: "bytes/256 cap-64", stats: bytes256},
		{name: "bytes/128 cap-64", stats: bytes128},
		{name: "bytes/96 cap-64", stats: bytes96},
		{name: "physical-lines cap-64", stats: lines64},
		{name: "physical-lines cap-128", stats: lines128},
	} {
		t.Logf(
			"top-level command hint %s capacity=%d covered=%d over=%d over_files=%d",
			candidate.name, candidate.stats.capacity, candidate.stats.covered,
			candidate.stats.over, candidate.stats.overFiles,
		)
	}
}

func boundedCommandHint(sourceBytes, divisor, limit int) int {
	hint := sourceBytes / divisor
	if hint < 1 {
		return 1
	}
	if hint > limit {
		return limit
	}
	return hint
}

func writeNamedProfile(t *testing.T, name, filename string) {
	t.Helper()
	profile := pprof.Lookup(name)
	if profile == nil {
		t.Fatalf("runtime profile %q is unavailable", name)
	}
	path := filepath.Join(profileDir, filename)
	file, err := openNewProfile(path)
	if err != nil {
		t.Fatalf("create %s profile: %v", name, err)
	}
	if err := profile.WriteTo(file, 0); err != nil {
		_ = file.Close()
		t.Fatalf("write %s profile: %v", name, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close %s profile: %v", name, err)
	}
}

func openNewProfile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return file, nil
}
