// Command diagnosticscan reports error-level vimls-go diagnostics for Vim
// scripts found below a comma-separated runtimepath.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
)

type finding struct {
	path      string
	line      int
	character int
	code      string
	message   string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("diagnosticscan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	runtimepath := flags.String("runtimepath", "", "comma-separated Vim runtimepath")
	outputPath := flags.String("output", "", "write diagnostics to this file instead of stdout")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*runtimepath) == "" {
		fmt.Fprintln(stderr, "diagnosticscan: -runtimepath is required")
		return 2
	}

	roots := splitRuntimepath(*runtimepath)
	files := make(map[string]struct{})
	failed := false
	for _, root := range roots {
		discovered, _, err := workspace.DiscoverFiles(root, 0)
		if err != nil {
			fmt.Fprintf(stderr, "diagnosticscan: %v\n", err)
			failed = true
			continue
		}
		for _, path := range discovered {
			if isVimSourcePath(path) {
				files[path] = struct{}{}
			}
		}
	}

	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	findings := make([]finding, 0)
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(stderr, "diagnosticscan: read %s: %v\n", path, err)
			failed = true
			continue
		}
		source := string(content)
		file := syntax.Parse(source)
		var result *analysis.FileAnalysis
		if workspace.IsConfigFile(path, nil, nil, roots) {
			result = analysis.AnalyzeConfigFile(file)
		} else {
			result = analysis.Analyze(file)
		}
		snapshot := text.NewSnapshot(path, 0, nil, source)
		for _, diagnostic := range analysis.CombinedDiagnostics(file, result) {
			if !isErrorDiagnostic(diagnostic) {
				continue
			}
			position, err := snapshot.Position(diagnostic.Span.Start, text.UTF8)
			if err != nil {
				fmt.Fprintf(stderr, "diagnosticscan: position %s byte %d: %v\n", path, diagnostic.Span.Start, err)
				failed = true
				continue
			}
			findings = append(findings, finding{
				path: path, line: position.Line + 1, character: position.Character + 1,
				code: diagnostic.Code, message: diagnostic.Message,
			})
		}
	}

	output := stdout
	if *outputPath != "" {
		file, err := os.Create(*outputPath)
		if err != nil {
			fmt.Fprintf(stderr, "diagnosticscan: create output: %v\n", err)
			return 2
		}
		defer file.Close()
		output = file
	}
	for _, item := range findings {
		fmt.Fprintf(output, "%s:%d:%d: %s %s\n", item.path, item.line, item.character, item.code, item.message)
	}
	fmt.Fprintf(stderr, "diagnosticscan: scanned %d roots, %d files, found %d errors\n", len(roots), len(paths), len(findings))
	if failed {
		return 2
	}
	return 0
}

func isVimSourcePath(path string) bool {
	if strings.EqualFold(filepath.Ext(path), ".vim") {
		return true
	}
	switch strings.ToLower(filepath.Base(path)) {
	case ".exrc", ".gvimrc", ".vimrc", "_exrc", "_gvimrc", "_vimrc", "exrc", "gvimrc", "vimrc":
		return true
	default:
		return false
	}
}

func splitRuntimepath(value string) []string {
	seen := make(map[string]struct{})
	roots := make([]string, 0)
	for root := range strings.SplitSeq(value, ",") {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		root = filepath.Clean(root)
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		roots = append(roots, root)
	}
	return roots
}

func isErrorDiagnostic(diagnostic syntax.Diagnostic) bool {
	if diagnostic.Severity != nil {
		return *diagnostic.Severity == syntax.DiagnosticError
	}
	switch diagnostic.Code {
	case "vim/E117", "vim/E121", "vim/E122", "vim/E174", "vim/E464", "vim/E705", "vim/E707", "vim/E1001", "vim/E1089":
		return false
	}
	if definition, ok := syntax.LookupVimlsDiagnostic(diagnostic.Code); ok {
		return definition.Severity == syntax.DiagnosticError
	}
	return true
}
