package analysis

import (
	"fmt"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func configDiagnosticsBenchmarkSource() string {
	var source strings.Builder
	source.WriteString("if exists('g:loaded_bench')\n  finish\nendif\nscriptencoding utf-8\n")
	for index := range 128 {
		fmt.Fprintf(&source, "nnoremap <Leader>x%d <Cmd>echo 1<CR>\n", index)
		fmt.Fprintf(&source, "nnoremap <Leader>x%d <Cmd>echo 2<CR>\n", index)
		fmt.Fprintf(&source, "if has('gui')\n  nunmap <Leader>y%d\nendif\nset encoding=utf-8\n", index)
	}
	source.WriteString("let g:mapleader = ','\nlet g:loaded_bench = 1\n")
	return source.String()
}

func nestedDiagnosticsBenchmarkSource() string {
	var source strings.Builder
	for index := range 128 {
		fmt.Fprintf(&source, "autocmd User Bench%d echo len([1, 2, 3])\n", index)
	}
	return source.String()
}

func BenchmarkDiagnosticAnalysis(b *testing.B) {
	for _, test := range []struct {
		name, source string
		config       bool
	}{
		{"Small", "let s:items = [1, 2]\necho len(s:items)\n", false},
		{"Config", configDiagnosticsBenchmarkSource(), true},
		{"NestedCommands", nestedDiagnosticsBenchmarkSource(), false},
		{"Calls", "vim9script\ndef Bench(value: number): number\n" + strings.Repeat("  echo len([abs(value), abs(value + 1)])\n", 64) + "  return value\nenddef\n", false},
		{"ExpressionChain", "vim9script\n" + strings.Repeat("echo "+strings.Repeat("1 + ", 95)+"1\n", 16), false},
	} {
		b.Run(test.name, func(b *testing.B) {
			file := syntax.Parse(test.source)
			if len(file.Diagnostics) != 0 {
				b.Fatalf("invalid benchmark source: %v", file.Diagnostics)
			}
			b.ReportAllocs()
			for b.Loop() {
				benchmarkOptionAnalysis, _ = AnalyzeWithOptions(file, Options{ConfigFile: test.config})
			}
		})
	}
}

func BenchmarkConfigDiagnostics(b *testing.B) {
	file := syntax.Parse(configDiagnosticsBenchmarkSource())
	result := &FileAnalysis{File: file}
	b.ReportAllocs()
	for b.Loop() {
		result.Diagnostics = nil
		collectConfigFileDiagnostics(result)
	}
	benchmarkOptionAnalysis = result
}

func BenchmarkNestedAssignmentDiagnostics(b *testing.B) {
	file := syntax.Parse(nestedDiagnosticsBenchmarkSource())
	result := Analyze(file)
	b.ReportAllocs()
	for b.Loop() {
		result.Diagnostics = nil
		collectAssignmentDiagnostics(result, file.Commands, result.Root)
	}
	benchmarkOptionAnalysis = result
}
