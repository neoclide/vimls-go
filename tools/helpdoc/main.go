// Command helpdoc measures runtime help extraction and writes a Markdown report.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/neoclide/vimls-go/internal/vimhelp"
	"github.com/neoclide/vimls-go/internal/workspace"
)

type measurement struct {
	DiscoverMS float64 `json:"discover_ms"`
	ReadMS     float64 `json:"read_ms"`
	ExtractMS  float64 `json:"extract_markdown_ms"`
	TotalMS    float64 `json:"load_total_ms"`
	AllocBytes uint64  `json:"allocated_bytes"`
}

type report struct {
	Roots         []string       `json:"roots"`
	Files         int            `json:"files"`
	InputBytes    int64          `json:"input_bytes"`
	Entries       int            `json:"entries"`
	UniqueNames   int            `json:"unique_names"`
	Kinds         map[string]int `json:"kinds"`
	MarkdownBytes int            `json:"markdown_body_bytes"`
	RetainedBytes int64          `json:"retained_heap_delta_bytes"`
	Warnings      []string       `json:"warnings"`
	Runs          []measurement  `json:"runs"`
	RenderMS      float64        `json:"report_render_ms"`
	WriteMS       float64        `json:"write_ms"`
	OutputBytes   int            `json:"output_bytes"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("helpdoc", flag.ContinueOnError)
	flags.SetOutput(stderr)
	rtp := flags.String("runtimepath", "", "comma-separated runtimepath (supports escaped commas)")
	rtpFile := flags.String("runtimepath-file", "", "read runtimepath from a file; pasted line wraps are removed")
	output := flags.String("output", "", "required destination Markdown file")
	repeat := flags.Int("repeat", 5, "number of complete discovery/read/extraction runs")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *output == "" || *repeat < 1 || (*rtp == "") == (*rtpFile == "") {
		fmt.Fprintln(stderr, "helpdoc: require -output, exactly one of -runtimepath/-runtimepath-file, and -repeat >= 1")
		return 2
	}
	if *rtpFile != "" {
		data, err := os.ReadFile(*rtpFile)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		*rtp = strings.NewReplacer("\r", "", "\n", "").Replace(string(data))
	}
	roots, err := runtimeRoots(*rtp)
	if err != nil || len(roots) == 0 {
		fmt.Fprintf(stderr, "helpdoc: invalid runtimepath: %v\n", err)
		return 2
	}
	var docs []vimhelp.SymbolDocumentation
	var summary report
	for range *repeat {
		docs = nil
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		paths, warnings := discover(roots)
		m := measurement{DiscoverMS: elapsedMS(start)}
		var inputBytes int64
		for _, path := range paths {
			readStart := time.Now()
			data, err := os.ReadFile(path)
			m.ReadMS += elapsedMS(readStart)
			if err != nil {
				fmt.Fprintf(stderr, "helpdoc: %s: %v\n", path, err)
				return 1
			}
			inputBytes += int64(len(data))
			extractStart := time.Now()
			docs = append(docs, vimhelp.ExtractSymbols(path, data)...)
			m.ExtractMS += elapsedMS(extractStart)
		}
		m.TotalMS = elapsedMS(start)
		runtime.ReadMemStats(&after)
		m.AllocBytes = after.TotalAlloc - before.TotalAlloc
		// GC and heap sampling are outside the measured load interval.
		runtime.GC()
		runtime.ReadMemStats(&after)
		summary.RetainedBytes = int64(after.HeapAlloc) - int64(before.HeapAlloc)
		runtime.KeepAlive(docs)
		summary.Roots, summary.Files, summary.InputBytes = roots, len(paths), inputBytes
		summary.Warnings = warnings
		summary.Runs = append(summary.Runs, m)
	}
	summary.Entries = len(docs)
	summary.Kinds = make(map[string]int)
	names := make(map[string]bool)
	for _, doc := range docs {
		summary.Kinds[doc.Kind]++
		names[doc.Name] = true
		summary.MarkdownBytes += len(doc.Markdown)
	}
	summary.UniqueNames = len(names)
	start := time.Now()
	content := render(docs, summary)
	summary.RenderMS = elapsedMS(start)
	summary.OutputBytes = len(content)
	start = time.Now()
	if err := os.WriteFile(*output, content, 0644); err != nil {
		fmt.Fprintf(stderr, "helpdoc: %v\n", err)
		return 1
	}
	summary.WriteMS = elapsedMS(start)
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func elapsedMS(start time.Time) float64 {
	return float64(time.Since(start)) / float64(time.Millisecond)
}

func runtimeRoots(value string) ([]string, error) {
	var roots []string
	var current strings.Builder
	seen := make(map[string]bool)
	add := func() error {
		root := strings.TrimSpace(current.String())
		current.Reset()
		if root == "" {
			return nil
		}
		path, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		if real, err := filepath.EvalSymlinks(path); err == nil {
			path = real
		}
		if !seen[path] {
			seen[path] = true
			roots = append(roots, path)
		}
		return nil
	}
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' && i+1 < len(value) && value[i+1] == ',' {
			i++
			current.WriteByte(',')
		} else if value[i] == ',' {
			if err := add(); err != nil {
				return nil, err
			}
		} else {
			current.WriteByte(value[i])
		}
	}
	if err := add(); err != nil {
		return nil, err
	}
	return roots, nil
}

func discover(roots []string) ([]string, []string) {
	paths, warnings, _ := workspace.DiscoverRuntimeHelpFiles(context.Background(), roots)
	return paths, warnings
}

func render(docs []vimhelp.SymbolDocumentation, summary report) []byte {
	var out bytes.Buffer
	fmt.Fprintln(&out, "# Runtimepath symbol documentation")
	fmt.Fprintf(&out, "\n%d roots · %d doc/*.txt files · %d entries · %d unique names\n", len(summary.Roots), summary.Files, summary.Entries, summary.UniqueNames)
	fmt.Fprintln(&out, "\nEntries follow runtimepath order, then filename and source line. Duplicate names are retained for inspection. Global functions include built-ins. This is tag-based extraction; untagged descriptions and pattern tags are not inferred.")
	fmt.Fprintln(&out, "\n## Load measurements\n\nEach run reads all files again. OS filesystem caches are not cleared. Build, forced GC, report assembly and output writing are excluded from load time. JSON on stdout includes allocation and output timings.\n\n| Run | Discover ms | Read ms | Extract + Markdown ms | Total ms |\n| --- | ---: | ---: | ---: | ---: |")
	for i, m := range summary.Runs {
		fmt.Fprintf(&out, "| %d | %.3f | %.3f | %.3f | %.3f |\n", i+1, m.DiscoverMS, m.ReadMS, m.ExtractMS, m.TotalMS)
	}
	if len(summary.Warnings) > 0 {
		fmt.Fprint(&out, "\n## Scan warnings\n\n")
		for _, warning := range summary.Warnings {
			fmt.Fprintf(&out, "- %s\n", warning)
		}
	}
	for _, doc := range docs {
		fmt.Fprintf(&out, "\n## `%s`\n\n%s · tag `%s`\n\nSource: `%s:%d`\n\n", doc.Name, doc.Kind, doc.Tag, doc.Source, doc.Line)
		if doc.Markdown == "" {
			fmt.Fprintln(&out, "_Tag has no description before the next section._")
		} else {
			fmt.Fprintln(&out, doc.Markdown)
		}
	}
	return out.Bytes()
}
