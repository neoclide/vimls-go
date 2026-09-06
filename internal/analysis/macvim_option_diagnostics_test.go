package analysis

import (
	"strings"
	"testing"
)

func TestMacVimOptionSettings(t *testing.T) {
	for _, setting := range []string{
		"set antialias", "set noanti", "set blur=3", "set fullscreen", "set invfu", "set fu!",
		"set fuoptions=maxvert,maxhorz", "set fuopt=background:#112233",
		"set macligatures", "setlocal nommta", "set macthinstrokes", "set transp=50",
		"let &l:macmeta = 1", "vim9cmd &macmeta = true", "let &transparency = 30",
		"vim9cmd &transp = 40", "let &fuopt = 'maxvert'", "set fullscreen&",
	} {
		for _, config := range []bool{false, true} {
			for _, wrapper := range []struct {
				before, after string
				hint          bool
			}{
				{"", "", true},
				{"if has('gui_macvim')\n", "\nendif", false},
				{"if !has('gui_macvim')\necho 1\nelse\n", "\nendif", false},
				{"if has('nvim')\necho 1\nelseif has('gui_macvim')\n", "\nendif", false},
				{"if !has('nvim')\n", "\nendif", true},
				{"if has('gui_macvim') || other\n", "\nendif", true},
				{"if has('gui_macvim') && has('nvim')\n", "\nendif", true},
			} {
				source := wrapper.before + setting + wrapper.after + "\n"
				diagnostics := compatibilityDiagnostics(source, config)
				if !wrapper.hint && len(diagnostics) != 0 || wrapper.hint && (len(diagnostics) != 1 || diagnostics[0].Code != "vimls/macvim-only-option") {
					t.Fatalf("config=%v %q: %#v", config, source, diagnostics)
				}
			}
		}
	}
}

func TestMacVimOptionInvalidSettings(t *testing.T) {
	for _, setting := range []string{
		"set transp=101", "set transparency=-1", "set blur=abc", "set nofuopt",
		"set fullscreen=1", "let &transp = 101", "vim9cmd &transp = 'bad'",
		"vim9cmd &macmeta = []", "set missingmacoption=1",
	} {
		for _, guard := range []bool{false, true} {
			source := setting + "\n"
			if guard {
				source = "if has('gui_macvim')\n" + source + "endif\n"
			}
			diagnostics := compatibilityDiagnostics(source, false)
			if len(diagnostics) != 1 || !strings.HasPrefix(diagnostics[0].Code, "vim/E") {
				t.Fatalf("%q: %#v", source, diagnostics)
			}
		}
	}
}

func TestMacVimDoesNotSuppressNeovimHints(t *testing.T) {
	diagnostics := compatibilityDiagnostics("if has('gui_macvim')\nset scl=auto:2\nendif\n", false)
	if len(diagnostics) != 1 || diagnostics[0].Code != "vimls/neovim-only-option" {
		t.Fatal(diagnostics)
	}
}
