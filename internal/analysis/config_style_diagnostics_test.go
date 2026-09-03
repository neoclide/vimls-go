package analysis

import (
	"reflect"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// collectVimlsCodes returns the vimls-owned diagnostic codes in source order
// for one analysis result.
func collectVimlsCodes(result *FileAnalysis) []string {
	var got []string
	for _, diagnostic := range result.Diagnostics {
		if strings.HasPrefix(diagnostic.Code, "vimls/") {
			got = append(got, diagnostic.Code)
		}
	}
	return got
}

// analyzeModeCodes parses source and returns its vimls-owned codes in the
// given role.
func analyzeModeCodes(t *testing.T, source string, configFile bool) []string {
	t.Helper()
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
	}
	var result *FileAnalysis
	if configFile {
		result = AnalyzeConfigFile(file)
	} else {
		result = Analyze(file)
	}
	return collectVimlsCodes(result)
}

// TestConfigFileModeDisablesPluginConfigurationRules checks the §4.1 rows for
// configuration-overwrite and global-internal-state: the plugin-oriented
// heuristics are disabled because top-level g: values are user configuration.
func TestConfigFileModeDisablesPluginConfigurationRules(t *testing.T) {
	source := "let g:plug_config = 1\nlet g:plug_internal_cache = {}\n"
	want := []string{"vimls/configuration-overwrite", "vimls/global-internal-state"}
	if got := analyzeModeCodes(t, source, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("plugin-mode diagnostics = %#v, want %#v", got, want)
	}
	if got := analyzeModeCodes(t, source, true); len(got) != 0 {
		t.Fatalf("config-mode diagnostics = %#v, want none", got)
	}
}

// TestConfigFileModeDisablesKeymapPluginRules checks that direct <Leader>
// mappings and the <unique> expectation are plugin-oriented and disabled for
// configuration files, while a recursive mapping is still reported as a hint.
func TestConfigFileModeDisablesKeymapPluginRules(t *testing.T) {
	source := "map <leader>x :echo 'x'<CR>\n"
	want := []string{"vimls/recursive-map", "vimls/direct-user-keymap", "vimls/mapping-without-unique"}
	if got := analyzeModeCodes(t, source, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("plugin-mode diagnostics = %#v, want %#v", got, want)
	}
	if got := analyzeModeCodes(t, source, true); !reflect.DeepEqual(got, []string{"vimls/recursive-map"}) {
		t.Fatalf("config-mode diagnostics = %#v, want only recursive-map", got)
	}
}

// TestConfigFileModeRecursiveMapSeverityIsHint checks that recursive mappings
// in a user configuration file keep code vimls/recursive-map but carry an
// occurrence severity of hint instead of the registered warning level.
func TestConfigFileModeRecursiveMapSeverityIsHint(t *testing.T) {
	for _, source := range []string{"map <leader>a :call F()<CR>\n", "nmap <leader>a <leader>b\n"} {
		file := syntax.Parse(source)
		if len(file.Diagnostics) != 0 {
			t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
		}
		result := AnalyzeConfigFile(file)
		found := false
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code != "vimls/recursive-map" {
				continue
			}
			found = true
			if diagnostic.Severity == nil || *diagnostic.Severity != syntax.DiagnosticHint {
				t.Fatalf("source = %q, recursive-map severity = %#v, want hint", source, diagnostic.Severity)
			}
		}
		if !found {
			t.Fatalf("source = %q: expected a recursive-map diagnostic", source)
		}
	}
}

// TestConfigFileModeSetVsSetlocal verifies the §4.1 set-vs-setlocal row: a
// vimrc top-level :set establishes defaults and is not reported, while a :set
// inside a buffer/window-targeted FileType/BufRead autocmd body still suggests
// :setlocal.
func TestConfigFileModeSetVsSetlocal(t *testing.T) {
	source := `set tabstop=4
autocmd FileType python set tabstop=4
autocmd BufReadPost *.md set wrap
`
	pluginWant := []string{
		"vimls/set-vs-setlocal", // top-level tabstop
		"vimls/autocmd-outside-augroup",
		"vimls/set-vs-setlocal", // FileType body
		"vimls/autocmd-outside-augroup",
		"vimls/set-vs-setlocal", // BufReadPost body
	}
	if got := analyzeModeCodes(t, source, false); !reflect.DeepEqual(got, pluginWant) {
		t.Fatalf("plugin-mode diagnostics = %#v, want %#v", got, pluginWant)
	}
	configWant := []string{
		"vimls/autocmd-outside-augroup",
		"vimls/set-vs-setlocal", // FileType body
		"vimls/autocmd-outside-augroup",
		"vimls/set-vs-setlocal", // BufReadPost body
	}
	if got := analyzeModeCodes(t, source, true); !reflect.DeepEqual(got, configWant) {
		t.Fatalf("config-mode diagnostics = %#v, want %#v", got, configWant)
	}
}

func TestConfigFileModeSetVsSetlocalRequiresLocalAutocmdEvent(t *testing.T) {
	source := "autocmd VimEnter * set tabstop=4\nautocmd ColorScheme * set tabstop=4\nautocmd FileType,VimEnter * set tabstop=4\nautocmd WinEnter * set tabstop=4\n"
	got := analyzeModeCodes(t, source, true)
	want := []string{
		"vimls/autocmd-outside-augroup",
		"vimls/autocmd-outside-augroup",
		"vimls/autocmd-outside-augroup",
		"vimls/autocmd-outside-augroup",
		"vimls/set-vs-setlocal",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config-mode diagnostics = %#v, want %#v", got, want)
	}
}

func TestAutocmdPatternsKeepEscapedAndBraceCommas(t *testing.T) {
	file := syntax.Parse(`autocmd BufRead foo\,bar,foo\\,baz,*.{go,mod} echomsg 'x'
`)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Autocmd == nil {
		t.Fatalf("parsed autocmd = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	got := autocmdPatterns(file, file.Commands[0].Autocmd.Pattern)
	want := []string{`foo\,bar`, `foo\\,baz`, `*.{go,mod}`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("autocmd patterns = %#v, want %#v", got, want)
	}
}

// TestConfigFileModeKeepsBehaviorRules verifies that the §4.1 rules that
// remain valuable in configuration files still fire in config mode.
func TestConfigFileModeKeepsBehaviorRules(t *testing.T) {
	tests := []struct {
		name, source string
		want         []string
	}{
		{
			name:   "normal-without-bang",
			source: "normal gg\n",
			want:   []string{"vimls/normal-without-bang"},
		},
		{
			name:   "function-without-abort legacy only",
			source: "function MyHelper()\nendfunction\n",
			want:   []string{"vimls/function-without-abort"},
		},
		{
			name:   "mapping-script-local-reference",
			source: "nnoremap <leader>q :call s:Q()<CR>\n",
			want:   []string{"vimls/mapping-script-local-reference"},
		},
		{
			name:   "implicit case",
			source: "let name = 'Foo'\nif name == 'foo'\nendif\n",
			want:   []string{"vimls/implicit-string-case"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := analyzeModeCodes(t, test.source, true); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("config-mode diagnostics = %#v, want %#v", got, test.want)
			}
		})
	}
}

// TestConfigFileModeAugroupSafeFormKeepsNoDiagnostic checks that the canonical
// reload-safe augroup form (clear before definitions) is not reported.
func TestConfigFileModeAugroupSafeFormKeepsNoDiagnostic(t *testing.T) {
	source := `augroup vimrc_files
  autocmd!
  autocmd BufReadPost * checktime
augroup END
`
	if got := analyzeModeCodes(t, source, true); len(got) != 0 {
		t.Fatalf("config-mode diagnostics = %#v, want none", got)
	}
}

// hasCode reports whether one role analysis contains code.
func hasCode(t *testing.T, source, code string, configFile bool) bool {
	t.Helper()
	for _, diagnostic := range analyzeModeDiagnostics(t, source, configFile) {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

// analyzeModeDiagnostics parses source and returns its diagnostics in the
// given role.
func analyzeModeDiagnostics(t *testing.T, source string, configFile bool) []syntax.Diagnostic {
	t.Helper()
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
	}
	if configFile {
		return AnalyzeConfigFile(file).Diagnostics
	}
	return Analyze(file).Diagnostics
}

// TestConfigFileModeAugroupReloadSafety verifies §4.3: targeted clears only
// prove the definitions they cover, ++once does not make registration safe,
// conditional clears are not proof, and replace-only groups are safe.
func TestConfigFileModeAugroupReloadSafety(t *testing.T) {
	code := "vimls/autocmd-group-not-cleared"
	tests := []struct {
		name, source string
		want         bool
	}{
		{
			name:   "bare clear covers every definition",
			source: "augroup g\n  autocmd!\n  autocmd BufReadPost *.vim echomsg 'x'\n  autocmd BufEnter * echomsg 'y'\naugroup END\n",
			want:   false,
		},
		{
			name:   "command chain keeps clear before embedded autocmd body",
			source: "augroup g | autocmd! | autocmd BufRead *.vim call F() | augroup END\n",
			want:   false,
		},
		{
			name:   "continued targeted clear covers embedded autocmd body",
			source: "augroup g\n  autocmd! BufRead\n  \\ *.vim\n  autocmd BufRead *.vim call F()\naugroup END\n",
			want:   false,
		},
		{
			name:   "event-targeted clear covers that event",
			source: "augroup g\n  autocmd! BufRead\n  autocmd BufRead *.vim echomsg 'x'\naugroup END\n",
			want:   false,
		},
		{
			name:   "event-targeted clear does not cover another event",
			source: "augroup g\n  autocmd! BufRead\n  autocmd BufEnter * echomsg 'x'\naugroup END\n",
			want:   true,
		},
		{
			name:   "pattern-targeted clear covers the same pattern",
			source: "augroup g\n  autocmd! BufRead *.md\n  autocmd BufRead *.md echomsg 'x'\naugroup END\n",
			want:   false,
		},
		{
			name:   "pattern-targeted clear does not cover a different pattern",
			source: "augroup g\n  autocmd! BufRead *.md\n  autocmd BufRead *.vim echomsg 'x'\naugroup END\n",
			want:   true,
		},
		{
			name:   "plus-plus-once registration still accumulates per source",
			source: "augroup g\n  autocmd BufRead *.md ++once echomsg 'x'\naugroup END\n",
			want:   true,
		},
		{
			name:   "conditional clear is not proof",
			source: "augroup g\n  if exists('g:clear')\n    autocmd!\n  endif\n  autocmd BufRead *.vim echomsg 'x'\naugroup END\n",
			want:   true,
		},
		{
			name:   "replace never accumulates duplicates",
			source: "augroup g\n  autocmd! BufRead *.vim echomsg 'x'\naugroup END\n",
			want:   false,
		},
		{
			name:   "conditional replace itself never accumulates duplicates",
			source: "augroup g\n  if exists('g:replace')\n    autocmd! BufRead *.vim echomsg 'x'\n  endif\naugroup END\n",
			want:   false,
		},
		{
			name:   "conditional replace does not cover a later definition",
			source: "augroup g\n  if exists('g:replace')\n    autocmd! BufRead *.vim echomsg 'x'\n  endif\n  autocmd BufRead *.vim echomsg 'y'\naugroup END\n",
			want:   true,
		},
		{
			name:   "query-only group is not reported",
			source: "augroup g\n  autocmd BufRead *.vim\naugroup END\n",
			want:   false,
		},
		{
			name:   "explicit group clear covers explicit group definition",
			source: "augroup g\n  autocmd! g BufRead\n  autocmd g BufRead *.vim echomsg 'x'\naugroup END\n",
			want:   false,
		},
		{
			name:   "clear after definitions retires them",
			source: "augroup g\n  autocmd BufRead *.vim echomsg 'x'\n  autocmd!\naugroup END\n",
			want:   false,
		},
		{
			name:   "explicit group commands outside a region are tracked",
			source: "autocmd g BufRead *.vim echomsg 'x'\nautocmd! g BufRead *.vim\n",
			want:   false,
		},
		{
			name:   "explicit group commands override the active region",
			source: "augroup other\n  autocmd g BufRead *.vim echomsg 'x'\n  autocmd! g BufRead *.vim\naugroup END\n",
			want:   false,
		},
		{
			name:   "case-insensitive events and comma-separated patterns are covered",
			source: "augroup g\n  autocmd! bufread *.md,*.vim\n  autocmd BufRead *.md,*.vim echomsg 'x'\naugroup END\n",
			want:   false,
		},
		{
			name:   "event aliases share coverage",
			source: "augroup g\n  autocmd! BufRead\n  autocmd BufReadPost *.vim echomsg 'x'\naugroup END\n",
			want:   false,
		},
		{
			name:   "wildcard event requires wildcard clear coverage",
			source: "augroup g\n  autocmd! BufRead *.vim\n  autocmd * *.vim echomsg 'x'\naugroup END\n",
			want:   true,
		},
		{
			name:   "wildcard event and pattern clear covers wildcard definition",
			source: "augroup g\n  autocmd! * *.vim\n  autocmd * *.vim echomsg 'x'\naugroup END\n",
			want:   false,
		},
		{
			name:   "separate later clears retire a pattern list",
			source: "augroup g\n  autocmd BufRead *.md,*.vim echomsg 'x'\n  autocmd! BufRead *.md\n  autocmd! BufRead *.vim\naugroup END\n",
			want:   false,
		},
		{
			name:   "prior coverage and later clear cover distinct patterns",
			source: "augroup g\n  autocmd! BufRead *.md\n  autocmd BufRead *.md,*.vim echomsg 'x'\n  autocmd! BufRead *.vim\naugroup END\n",
			want:   false,
		},
		{
			name:   "later clear cannot reuse prior coverage for another pattern",
			source: "augroup g\n  autocmd! BufRead *.md\n  autocmd BufRead *.md,*.vim echomsg 'x'\n  autocmd! BufRead *.md\naugroup END\n",
			want:   true,
		},
		{
			name:   "execute-based autocmd or clear stays unknown",
			source: "augroup g\n  execute 'au! BufRead'\n  autocmd BufRead *.vim echomsg 'x'\naugroup END\n",
			want:   false,
		},
		{
			name:   "execute prose beginning with au remains analyzable",
			source: "augroup g\n  execute 'author note'\n  autocmd BufRead *.vim echomsg 'x'\naugroup END\n",
			want:   true,
		},
		{
			name:   "execute outside a region makes explicit group state unknown",
			source: "execute 'au! BufRead'\nautocmd g BufRead *.vim echomsg 'x'\n",
			want:   false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasCode(t, test.source, code, true); got != test.want {
				gotDiags := analyzeModeDiagnostics(t, test.source, true)
				t.Fatalf("config-mode augroup diag = %v, want %v; diagnostics = %#v", got, test.want, gotDiags)
			}
		})
	}
}

// TestConfigFileModeAugroupReportPointsToUncoveredAutocmd verifies that the
// config-mode report carries same-file related information on the first
// uncovered persistent autocommand.
func TestConfigFileModeAugroupReportPointsToUncoveredAutocmd(t *testing.T) {
	source := `augroup g
  autocmd BufRead *.vim echomsg 'x'
  autocmd BufEnter * echomsg 'y'
augroup END
`
	for _, diagnostic := range analyzeModeDiagnostics(t, source, true) {
		if diagnostic.Code != "vimls/autocmd-group-not-cleared" {
			continue
		}
		if diagnostic.Related.Span.Start == diagnostic.Related.Span.End || diagnostic.Related.Message == "" {
			t.Fatalf("augroup diagnostic missing related location: %#v", diagnostic)
		}
		return
	}
	t.Fatalf("no augroup diagnostic reported for %q", source)
}

// countCode returns the diagnostics with code in one role analysis.
func countCode(t *testing.T, source, code string, configFile bool) int {
	t.Helper()
	file := syntax.Parse(source)
	var result *FileAnalysis
	if configFile {
		result = AnalyzeConfigFile(file)
	} else {
		result = Analyze(file)
	}
	count := 0
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == code {
			count++
		}
	}
	return count
}

// TestConfigFileModeRepeatSourceFunctionBang verifies §4.2 with the behavior
// reproduced on Vim v9.2.1015: re-sourcing the same script silently replaces
// its own functions and commands, so a single no-bang definition must not be
// reported. A statically provable duplicate (two unconditional definitions in
// one file) is still reported with vim/E122/vim/E174.
func TestConfigFileModeRepeatSourceFunctionBang(t *testing.T) {
	single := "function MyHelper()\nendfunction\n"
	if got := countCode(t, single, "vim/E122", true); got != 0 {
		t.Fatalf("config single function definition reported E122 %d times", got)
	}
	command := "command MyCommand echo 'ok'\n"
	if got := countCode(t, command, "vim/E174", true); got != 0 {
		t.Fatalf("config single user command definition reported E174 %d times", got)
	}
	duplicate := "function MyHelper()\nendfunction\nfunction MyHelper()\nendfunction\n"
	if got := countCode(t, duplicate, "vim/E122", true); got != 1 {
		t.Fatalf("config duplicate function definitions reported E122 %d times, want 1", got)
	}
	duplicateCommand := "command MyCommand echo 'ok'\ncommand MyCommand echo 'nope'\n"
	if got := countCode(t, duplicateCommand, "vim/E174", true); got != 1 {
		t.Fatalf("config duplicate command definitions reported E174 %d times, want 1", got)
	}
	guarded := "if has('gui')\n  function MyHelper()\n  endfunction\nendif\nif has('gui')\n  function MyHelper()\n  endfunction\nendif\n"
	if got := countCode(t, guarded, "vim/E122", true); got != 0 {
		t.Fatalf("config conditional function definitions reported E122 %d times, want 0", got)
	}
	// Plugin (non-config) mode keeps the existing conservative risk warning.
	if got := countCode(t, single, "vim/E122", false); got != 1 {
		t.Fatalf("plugin single function definition reported E122 %d times, want 1", got)
	}
}
