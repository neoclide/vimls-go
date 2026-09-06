package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func compatibilityDiagnostics(source string, config bool) []syntax.Diagnostic {
	file := syntax.Parse(source)
	result := Analyze(file)
	if config {
		result = AnalyzeConfigFile(file)
	}
	var diagnostics []syntax.Diagnostic
	for _, diagnostic := range CombinedDiagnostics(file, result) {
		switch diagnostic.Code {
		case "vimls/neovim-only-option", "vim/E474", "vim/E487", "vim/E521", "vim/E539", "vim/E730", "vim/E1511", "vim/E1012", "vim/E113", "vim/E518":
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	return diagnostics
}

func TestNeovimOptionSettingsRequireGuard(t *testing.T) {
	for _, setting := range []string{
		"set signcolumn=auto:2", "setlocal scl=yes:9", "setglobal scl:auto:1-9",
		"let &l:scl = 'auto:2-5'", "vim9cmd &g:scl = 'yes:3'",
		"set fdc=auto:2", "let &foldcolumn = 'auto'", "vim9cmd &fdc = 'auto:3'",
		"set ch=0", "let &cmdheight = 0", "set ls=3", "vim9cmd &ls = 3",
		"set cot=menu,preselect", "set cot+=preselect", "set cot^=preselect",
		"set fcs=vert:│,horiz:─,verthoriz:┼,msgsep:─", "set fcs+=wbr:─",
		"let &fillchars = 'horiz:─'", "set jop=stack,view,clean", "set cpo+=_",
		"set inccommand=split", "set pb=25", "setlocal winbl=30", "set stc=%s",
		"let &g:icm = 'nosplit'", "let &winbar = Value()", "set shadafile=NONE",
		"set scbk=10000", "set tpf=BS,HT", "set rdb=compositor,line", "set winborder=rounded",
		"set mousescroll=ver:3,hor:6", "let &winbar .= 'suffix'",
	} {
		t.Run(setting, func(t *testing.T) {
			for _, config := range []bool{false, true} {
				for _, wrapper := range []struct {
					before, after string
					hint          bool
				}{
					{"", "", true},
					{"if has('nvim')\n", "\nendif", false},
					{"if !has('nvim')\necho 1\nelse\n", "\nendif", false},
					{"if has('nvim') || other\n", "\nendif", true},
					{"if !has('nvim')\n", "\nendif", true},
				} {
					source := wrapper.before + setting + wrapper.after + "\n"
					diagnostics := compatibilityDiagnostics(source, config)
					if !wrapper.hint && len(diagnostics) != 0 || wrapper.hint && (len(diagnostics) != 1 || diagnostics[0].Code != "vimls/neovim-only-option") {
						t.Fatalf("config=%v diagnostics %#v in %q", config, diagnostics, source)
					}
				}
			}
		})
	}
}

func TestNeovimOptionInvalidValuesRemainErrors(t *testing.T) {
	for _, setting := range []string{
		"set scl=auto:10", "set scl=yes:0", "set scl=auto:2-2", "set scl=auto:9-1",
		"set fdc=auto:0", "set fdc=bogus", "set fdc=13", "set ch=-1",
		"set cot=preselect,bogus", "set fcs=horiz:xx", "set fcs=horiz:─,bogus:x",
		"set jop=view,bogus", "set cpo=_@", "set icm=splti", "set pb=abc", "set winbl=101",
		"let &inccommand = 'bogus'", "let &scl = 'auto:10'", "set scbk=0", "set tpf=BS,BOGUS",
		"set nosigncolumn", "set missingoption=auto:2", "let &missingoption = 'auto:2'",
		"set mousescroll=vertical:3", "set mousescroll=ver:1,ver:2", "set mousescroll=hor:-1",
		"set winborder=round", "vim9cmd &pumblend = 'bad'",
		"vim9cmd &foldcolumn = []", "vim9cmd &pumblend = []", "vim9cmd &inccommand = []",
	} {
		t.Run(setting, func(t *testing.T) {
			for _, wrapper := range []string{"", "if has('nvim')\n"} {
				source := wrapper + setting + "\n"
				if wrapper != "" {
					source += "endif\n"
				}
				diagnostics := compatibilityDiagnostics(source, false)
				if len(diagnostics) != 1 || !strings.HasPrefix(diagnostics[0].Code, "vim/E") || diagnostics[0].Severity != nil {
					t.Fatalf("expected one error, got %#v in %q", diagnostics, source)
				}
			}
		})
	}
}

func TestNeovimCompatibilityPreservesVimAndDynamicSettings(t *testing.T) {
	for _, setting := range []string{
		"set scl=auto", "set fdc=12", "set ch=1", "set ls=2", "set cot=menu,popuphidden",
		"set fcs=tpl_vert:x", "set cpo=gH", "set jop=stack", "set cot-=preselect",
		"set scl+=:2", "set ch+=1", "let &signcolumn = Value()", "let &signcolumn = 'auto:' . Value()",
		"let &signcolumn = \"auto:\\x32\"",
		"set pb=single,margin", "set pb=custom:x;x;x;x;x;x;x;x", "let &scl .= ':2'",
		"set ls=4", "set ls=-1",
	} {
		if diagnostics := compatibilityDiagnostics(setting+"\n", false); len(diagnostics) != 0 {
			t.Errorf("%q: %#v", setting, diagnostics)
		}
	}
}

func TestNeovimOptionTypesUseEditorContext(t *testing.T) {
	source := `vim9script
if has('nvim')
  var column: string = &foldcolumn
  &foldcolumn = 'auto:2'
  &foldcolumn = 2
else
  var column: number = &foldcolumn
endif
&foldcolumn = 'auto:2'
`
	diagnostics := compatibilityDiagnostics(source, false)
	if len(diagnostics) != 1 || diagnostics[0].Code != "vimls/neovim-only-option" {
		t.Fatalf("diagnostics %#v", diagnostics)
	}
}

func TestNeovimOptionGuardDoesNotLeak(t *testing.T) {
	source := "if has('nvim')\nfunction! F()\nset scl=auto:2\nendfunction\nautocmd BufEnter * set scl=auto:3\nendif\nset scl=auto:4\n"
	diagnostics := compatibilityDiagnostics(source, false)
	if len(diagnostics) != 1 || diagnostics[0].Code != "vimls/neovim-only-option" || syntax.Parse(source).Text(diagnostics[0].Span) != "auto:4" {
		t.Fatalf("diagnostics %#v", diagnostics)
	}
}
