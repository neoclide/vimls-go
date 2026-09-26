package analysis

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestConfigConsolidationKeepsRuleAndAssignmentOrder(t *testing.T) {
	source := "nnoremap <Leader><LocalLeader>x :echo 1<CR>\n" +
		"nnoremap <Leader><LocalLeader>x :echo 2<CR>\n" +
		"let g:maplocalleader = ','\nlet g:mapleader = ';'\n"
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
	}
	result := AnalyzeConfigFile(file)
	second := strings.Index(source, "nnoremap <Leader><LocalLeader>x :echo 2") + len("nnoremap ")
	var codes, messages []string
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Span.Start == second && (diagnostic.Code == "vimls/config-mapleader-order" || diagnostic.Code == "vimls/duplicate-mapping") {
			codes = append(codes, diagnostic.Code)
			messages = append(messages, diagnostic.Message)
		}
	}
	want := []string{"vimls/config-mapleader-order", "vimls/config-mapleader-order", "vimls/duplicate-mapping"}
	if len(codes) != len(want) {
		t.Fatalf("same-span codes = %v, want %v", codes, want)
	}
	for index := range want {
		if codes[index] != want[index] {
			t.Fatalf("same-span codes = %v, want %v", codes, want)
		}
	}
	if !strings.Contains(messages[0], "g:maplocalleader") || !strings.Contains(messages[1], "g:mapleader") {
		t.Fatalf("leader assignment order changed: %v", messages)
	}
}

func TestConfigConsolidationConditionalExecuteInvalidatesMapping(t *testing.T) {
	source := "nnoremap x :echo 1<CR>\nif has('gui')\n  execute 'nunmap x'\nendif\nnnoremap x :echo 2<CR>\n"
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
	}
	for _, diagnostic := range AnalyzeConfigFile(file).Diagnostics {
		if diagnostic.Code == "vimls/duplicate-mapping" {
			t.Fatalf("conditional dynamic invalidation was ignored: %#v", diagnostic)
		}
	}
}

func TestConfigConsolidationLangmapHighestModeBit(t *testing.T) {
	source := "lmap x a\nlmap x b\nlunmap x\nlmap x c\nlmapclear\nlmap x d\n"
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
	}
	var duplicates []syntax.Diagnostic
	for _, diagnostic := range AnalyzeConfigFile(file).Diagnostics {
		if diagnostic.Code == "vimls/duplicate-mapping" {
			duplicates = append(duplicates, diagnostic)
		}
	}
	if len(duplicates) != 1 {
		t.Fatalf("duplicate-mapping diagnostics = %#v, want one", duplicates)
	}
	second := strings.Index(source, "lmap x b") + len("lmap ")
	first := strings.Index(source, "lmap x a") + len("lmap ")
	if duplicates[0].Span.Start != second || duplicates[0].Related.Span.Start != first || file.Text(duplicates[0].Span) != "x" || file.Text(duplicates[0].Related.Span) != "x" {
		t.Fatalf("duplicate and related spans = %#v, want second and first lmap definitions", duplicates[0])
	}
}

func TestConfigConsolidationGuardBlockAndLaterMarker(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         int
	}{
		{"later marker", "if exists('g:loaded_later')\n  finish\nendif\nlet g:loaded_later = 1\n", 1},
		{"nested finish", "if exists('g:loaded_nested')\n  if has('gui')\n    finish\n  endif\nendif\nlet g:loaded_nested = 1\n", 0},
		{"else before finish", "if exists('g:loaded_else')\nelse\n  finish\nendif\nlet g:loaded_else = 1\n", 0},
		{"finish before else", "if exists('g:loaded_early')\n  finish\nelse\n  echo 1\nendif\nlet g:loaded_early = 1\n", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			if len(file.Diagnostics) != 0 {
				t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
			}
			count := 0
			for _, diagnostic := range AnalyzeConfigFile(file).Diagnostics {
				if diagnostic.Code == "vimls/config-loaded-guard" {
					count++
				}
			}
			if count != test.want {
				t.Fatalf("loaded guard diagnostics = %d, want %d", count, test.want)
			}
		})
	}
}

func TestConfigConsolidationStopsAtProgressCancellation(t *testing.T) {
	file := syntax.Parse(strings.Repeat("nnoremap x :echo 1<CR>\n", 128))
	if len(file.Diagnostics) != 0 {
		t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
	}
	calls := 0
	result := &FileAnalysis{File: file, progress: &analysisProgress{yield: func() error {
		calls++
		return context.Canceled
	}}}
	collectConfigFileDiagnostics(result)
	if calls != 1 || result.progress.steps != 64 || !errors.Is(result.progress.err, context.Canceled) || len(result.Diagnostics) != 0 {
		t.Fatalf("cancellation: calls=%d steps=%d err=%v diagnostics=%d", calls, result.progress.steps, result.progress.err, len(result.Diagnostics))
	}
}
