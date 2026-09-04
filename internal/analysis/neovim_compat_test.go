package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestAnalyzeNeovimCompatNamesDoNotReportUnknown(t *testing.T) {
	sources := []string{
		// Direct Neovim API calls in both script and def contexts.
		"vim9script\nnvim_buf_get_lines(0, 0, -1, false)\n",
		"vim9script\ndef F()\n  nvim_buf_get_lines(0, 0, -1, false)\nenddef\n",
		"call nvim_buf_get_lines(0, 0, -1, 0)\n",
		// Neovim-only options, variables, and commands from the compatibility
		// lists.
		"vim9script\necho &shada\nset shada=''\n",
		"vim9script\necho v:lua\n",
		"rshada\n",
		"rshada!\n",
		"wshada viminfo\n",
		"rshada | echo 1\n",
		// Neovim-only autocmd events.
		"augroup coc_nvim\n  autocmd!\n  autocmd TermOpen * call s:Autocmd('TermOpen', +expand('<abuf>'))\naugroup end\n",
		"augroup nvim_test\n  autocmd!\n  autocmd LspAttach,TermClose * echo 1\naugroup END\n",
	}
	for _, source := range sources {
		file := syntax.Parse(source)
		result := Analyze(file)
		if len(file.Diagnostics) != 0 || len(result.Diagnostics) != 0 {
			t.Fatalf("Neovim compatibility source %q diagnostics: syntax=%#v, analysis=%#v", source, file.Diagnostics, result.Diagnostics)
		}
	}
}

func TestAnalyzeNeovimCompatNamesStillReportUnknownNames(t *testing.T) {
	file := syntax.Parse("vim9script\ndoesnotexist_nvim_any()\n")
	found := false
	for _, diagnostic := range Analyze(file).Diagnostics {
		if strings.HasPrefix(diagnostic.Code, "vim/E117") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unknown function did not report E117; diagnostics=%#v", Analyze(file).Diagnostics)
	}
}
