package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// guardCodeCount returns the number of vimls/config-loaded-guard diagnostics.
func guardCodeCount(t *testing.T, source string) (int, string) {
	t.Helper()
	file := syntax.Parse(source)
	result := AnalyzeConfigFile(file)
	count := 0
	message := ""
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vimls/config-loaded-guard" {
			count++
			message = diagnostic.Message
		}
	}
	return count, message
}

// TestConfigLoadedGuardDiagnostics verifies §4.4: the classic plugin-style
// loaded guard at the top of a configuration file is reported as a hint.
func TestConfigLoadedGuardDiagnostics(t *testing.T) {
	pattern := `if exists('g:loaded_my_vimrc')
  finish
endif
let g:loaded_my_vimrc = 1
set tabstop=2
`
	count, message := guardCodeCount(t, pattern)
	if count != 1 {
		t.Fatalf("loaded-guard diagnostics = %d, want 1", count)
	}
	if !strings.Contains(message, "loaded") || !strings.Contains(message, "source") {
		t.Fatalf("loaded-guard message = %q", message)
	}
}

// TestConfigLoadedGuardExclusions verifies the §4.4 non-goals: capability
// checks, feature detection, !exists initialization, function-scoped finish,
// missing marker assignment, and deliberate vim9script noclear are not
// reported.
func TestConfigLoadedGuardExclusions(t *testing.T) {
	tests := []struct {
		name, source string
	}{
		{
			name:   "capability and feature checks are not guards",
			source: "if exists('*MyFunc')\n  finish\nendif\nif has('gui')\n  finish\nendif\nlet g:loaded_my_vimrc = 1\n",
		},
		{
			name:   "non-guard exists argument",
			source: "if exists(':MyCommand')\n  finish\nendif\nlet g:loaded_my_vimrc = 1\n",
		},
		{
			name:   "initialization under !exists is not a guard",
			source: "if !exists('g:loaded_my_vimrc')\n  let g:loaded_my_vimrc = 1\n  set tabstop=2\nendif\n",
		},
		{
			name:   "finish inside a function is not a file guard",
			source: "function! s:Early()\n  if exists('g:loaded_my_vimrc')\n    finish\n  endif\nendfunction\nlet g:loaded_my_vimrc = 1\n",
		},
		{
			name:   "missing marker assignment",
			source: "if exists('g:loaded_my_vimrc')\n  finish\nendif\nset tabstop=2\n",
		},
		{
			name:   "bare loaded variable query is not a marker assignment",
			source: "if exists('g:loaded_my_vimrc')\n  finish\nendif\nlet g:loaded_my_vimrc\n",
		},
		{
			name:   "vim9script noclear explicit single load",
			source: "vim9script noclear\nif exists('g:loaded_vimrc')\n  finish\nendif\nvar g:loaded_vimrc = 1\n",
		},
		{
			name:   "else branch finish is not a guard",
			source: "if has('gui')\n  set tabstop=2\nelse\n  finish\nendif\nlet g:loaded_my_vimrc = 1\n",
		},
		{
			name:   "guard needs an exists condition",
			source: "if 1\n  finish\nendif\nlet g:loaded_my_vimrc = 1\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			count, _ := guardCodeCount(t, test.source)
			if count != 0 {
				t.Fatalf("unexpected loaded-guard diagnostics = %d for source:\n%s", count, test.source)
			}
		})
	}
}

// TestConfigLoadedGuardNotInPluginMode checks config-loaded-guard is only
// reported in the config role.
func TestConfigLoadedGuardNotInPluginMode(t *testing.T) {
	source := "if exists('g:loaded_plugin')\n  finish\nendif\nlet g:loaded_plugin = 1\n"
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
	}
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vimls/config-loaded-guard" {
			t.Fatalf("plugin-mode reported config-loaded-guard: %#v", diagnostic)
		}
	}
}

// TestConfigLoadedGuardVim9WithoutNoClear verifies that a Vim9 script without
// noclear still gets the reload guard hint (§4.4's Vim9 note).
func TestConfigLoadedGuardVim9WithoutNoClear(t *testing.T) {
	source := "vim9script\nif exists('g:loaded_vimrc')\n  finish\nendif\n"
	count, message := guardCodeCount(t, source)
	if count != 1 {
		t.Fatalf("vim9 loaded-guard diagnostics = %d, want 1", count)
	}
	if !strings.Contains(message, "script-local") {
		t.Fatalf("vim9 loaded-guard message = %q, want the reload note", message)
	}
}
