package analysis

import (
	"reflect"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func findDiagnostic(diagnostics []syntax.Diagnostic, code string) *syntax.Diagnostic {
	for i := range diagnostics {
		if diagnostics[i].Code == code {
			return &diagnostics[i]
		}
	}
	return nil
}

func collectCodes(diagnostics []syntax.Diagnostic) []string {
	var codes []string
	for _, d := range diagnostics {
		if strings.HasPrefix(d.Code, "vimls/") {
			codes = append(codes, d.Code)
		}
	}
	return codes
}

func TestSubstituteImplicitPatternCase(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantCase   bool
		wantSpan   string
		configFile bool
	}{
		{
			name:     "plain letters without case flag or override",
			source:   "s/foo/bar/\n",
			wantCase: true,
			wantSpan: "foo",
		},
		{
			name:     "substitute full command name with letters",
			source:   "substitute/abc/def/\n",
			wantCase: true,
			wantSpan: "abc",
		},
		{
			name:     "range with letters",
			source:   "1,10s/xyz/abc/\n",
			wantCase: true,
			wantSpan: "xyz",
		},
		{
			name:     "percent range with letters",
			source:   "%s/pattern/rep/\n",
			wantCase: true,
			wantSpan: "pattern",
		},
		{
			name:     "letters with character class",
			source:   "s/foo[0-9]/rep/\n",
			wantCase: true,
			wantSpan: "foo[0-9]",
		},
		{
			name:     "escaped escape sequence plus letters",
			source:   "s/\\d\\+foo/rep/\n",
			wantCase: true,
			wantSpan: "\\d\\+foo",
		},
		{
			name:     "escaped backslash followed by letter",
			source:   "s/\\\\d/rep/\n",
			wantCase: true,
			wantSpan: "\\\\d",
		},
		{
			name:     "vim9 script with letters",
			source:   "vim9script\ns/foo/bar/\n",
			wantCase: true,
			wantSpan: "foo",
		},
		// Suppressed cases
		{
			name:     "ignorecase flag i",
			source:   "s/foo/bar/i\n",
			wantCase: false,
		},
		{
			name:     "matchcase flag I",
			source:   "s/foo/bar/I\n",
			wantCase: false,
		},
		{
			name:     "combined flags gi",
			source:   "s/foo/bar/gi\n",
			wantCase: false,
		},
		{
			name:     "explicit lowercase c prefix",
			source:   "s/\\cfoo/bar/\n",
			wantCase: false,
		},
		{
			name:     "explicit uppercase C prefix",
			source:   "s/\\Cfoo/bar/\n",
			wantCase: false,
		},
		{
			name:     "mid-pattern lowercase c override",
			source:   "s/foo\\cbar/rep/\n",
			wantCase: false,
		},
		{
			name:     "mid-pattern uppercase C override",
			source:   "s/foo\\Cbar/rep/\n",
			wantCase: false,
		},
		{
			name:     "digits only",
			source:   "s/123/456/\n",
			wantCase: false,
		},
		{
			name:     "digit escape only",
			source:   "s/\\d\\+/123/\n",
			wantCase: false,
		},
		{
			name:     "whitespace escape only",
			source:   "s/\\s\\+/ /\n",
			wantCase: false,
		},
		{
			name:     "empty pattern",
			source:   "s//replacement/\n",
			wantCase: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := syntax.Parse(tt.source)
			var analysis *FileAnalysis
			if tt.configFile {
				analysis = AnalyzeConfigFile(file)
			} else {
				analysis = Analyze(file)
			}
			diag := findDiagnostic(analysis.Diagnostics, "vimls/implicit-pattern-case")
			if tt.wantCase {
				if diag == nil {
					t.Fatalf("expected vimls/implicit-pattern-case diagnostic, got none in %#v", collectCodes(analysis.Diagnostics))
				}
				wantMsg := "substitute pattern depends on 'ignorecase'; consider 'i' or 'I' flag, or '\\c' / '\\C'"
				if diag.Message != wantMsg {
					t.Errorf("diagnostic message = %q, want %q", diag.Message, wantMsg)
				}
				if file.Text(diag.Span) != tt.wantSpan {
					t.Errorf("diagnostic span text = %q, want %q", file.Text(diag.Span), tt.wantSpan)
				}
			} else if diag != nil {
				t.Fatalf("unexpected vimls/implicit-pattern-case diagnostic: %#v", diag)
			}
		})
	}
}

func TestSubstituteImplicitRegexMagic(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantMagic  bool
		wantSpan   string
		configFile bool
	}{
		{
			name:      "dot star metacharacters",
			source:    "s/foo.*bar/rep/\n",
			wantMagic: true,
			wantSpan:  "foo.*bar",
		},
		{
			name:      "plus metacharacter",
			source:    "s/foo+bar/rep/\n",
			wantMagic: true,
			wantSpan:  "foo+bar",
		},
		{
			name:      "question mark metacharacter",
			source:    "s/foo?bar/rep/\n",
			wantMagic: true,
			wantSpan:  "foo?bar",
		},
		{
			name:      "parentheses metacharacters",
			source:    "s/foo(bar)/rep/\n",
			wantMagic: true,
			wantSpan:  "foo(bar)",
		},
		{
			name:      "curly braces metacharacters",
			source:    "s/foo{1,2}/rep/\n",
			wantMagic: true,
			wantSpan:  "foo{1,2}",
		},
		{
			name:      "bracket character class metacharacters",
			source:    "s/foo[0-9]/rep/\n",
			wantMagic: true,
			wantSpan:  "foo[0-9]",
		},
		{
			name:      "tilde metacharacter",
			source:    "s/foo~bar/rep/\n",
			wantMagic: true,
			wantSpan:  "foo~bar",
		},
		{
			name:      "vim9 script with metacharacters",
			source:    "vim9script\ns/foo.*bar/rep/\n",
			wantMagic: true,
			wantSpan:  "foo.*bar",
		},
		// Suppressed cases
		{
			name:      "smagic command",
			source:    "smagic/foo.*bar/rep/\n",
			wantMagic: false,
		},
		{
			name:      "snomagic command",
			source:    "snomagic/foo.*bar/rep/\n",
			wantMagic: false,
		},
		{
			name:      "very magic prefix \\v",
			source:    "s/\\vfoo.*bar/rep/\n",
			wantMagic: false,
		},
		{
			name:      "magic prefix \\m",
			source:    "s/\\mfoo.*bar/rep/\n",
			wantMagic: false,
		},
		{
			name:      "nomagic prefix \\M",
			source:    "s/\\Mfoo.*bar/rep/\n",
			wantMagic: false,
		},
		{
			name:      "very nomagic prefix \\V",
			source:    "s/\\Vfoo.*bar/rep/\n",
			wantMagic: false,
		},
		{
			name:      "case prefix followed by very magic \\c\\v",
			source:    "s/\\c\\vfoo.*bar/rep/\n",
			wantMagic: false,
		},
		{
			name:      "case prefix followed by magic \\C\\m",
			source:    "s/\\C\\mfoo.*bar/rep/\n",
			wantMagic: false,
		},
		{
			name:      "no metacharacters",
			source:    "s/foo/rep/\n",
			wantMagic: false,
		},
		{
			name:      "digits only no metacharacters",
			source:    "s/123/456/\n",
			wantMagic: false,
		},
		{
			name:      "empty pattern",
			source:    "s//rep/\n",
			wantMagic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := syntax.Parse(tt.source)
			var analysis *FileAnalysis
			if tt.configFile {
				analysis = AnalyzeConfigFile(file)
			} else {
				analysis = Analyze(file)
			}
			diag := findDiagnostic(analysis.Diagnostics, "vimls/implicit-regex-magic")
			if tt.wantMagic {
				if diag == nil {
					t.Fatalf("expected vimls/implicit-regex-magic diagnostic, got none in %#v", collectCodes(analysis.Diagnostics))
				}
				wantMsg := "substitute pattern relies on Vim's magic setting; consider :smagic or an explicit magic prefix"
				if diag.Message != wantMsg {
					t.Errorf("diagnostic message = %q, want %q", diag.Message, wantMsg)
				}
				if file.Text(diag.Span) != tt.wantSpan {
					t.Errorf("diagnostic span text = %q, want %q", file.Text(diag.Span), tt.wantSpan)
				}
			} else if diag != nil {
				t.Fatalf("unexpected vimls/implicit-regex-magic diagnostic: %#v", diag)
			}
		})
	}
}

func TestSubstituteEmptyPattern(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		wantEmpty bool
		wantSpan  string
	}{
		{
			name:      "empty pattern with replacement",
			source:    "s//replacement/\n",
			wantEmpty: true,
			wantSpan:  "//",
		},
		{
			name:      "empty pattern with flags",
			source:    "s//replacement/g\n",
			wantEmpty: true,
			wantSpan:  "//",
		},
		{
			name:      "bare substitute command",
			source:    "s\n",
			wantEmpty: true,
			wantSpan:  "s",
		},
		{
			name:      "percent bare substitute",
			source:    "%s\n",
			wantEmpty: true,
			wantSpan:  "s",
		},
		{
			name:      "substitute with count",
			source:    "s 2\n",
			wantEmpty: true,
			wantSpan:  "s",
		},
		{
			name:      "substitute with repeat flags",
			source:    "sge\n",
			wantEmpty: true,
			wantSpan:  "s",
		},
		{
			name:      "substitute previous pattern form",
			source:    "s\\/replacement/\n",
			wantEmpty: true,
			wantSpan:  "\\/",
		},
		{
			name:      "tilde repeat substitute",
			source:    "~\n",
			wantEmpty: true,
			wantSpan:  "~",
		},
		{
			name:      "single delimiter without pattern",
			source:    "s/\n",
			wantEmpty: true,
			wantSpan:  "/",
		},
		{
			name:      "vim9 script empty pattern",
			source:    "vim9script\ns//replacement/\n",
			wantEmpty: true,
			wantSpan:  "//",
		},
		// Suppressed cases
		{
			name:      "pattern with letters",
			source:    "s/foo/bar/\n",
			wantEmpty: false,
		},
		{
			name:      "pattern with digits",
			source:    "s/123/456/\n",
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := syntax.Parse(tt.source)
			analysis := Analyze(file)
			diag := findDiagnostic(analysis.Diagnostics, "vimls/substitute-empty-pattern")
			if tt.wantEmpty {
				if diag == nil {
					t.Fatalf("expected vimls/substitute-empty-pattern diagnostic, got none in %#v", collectCodes(analysis.Diagnostics))
				}
				wantMsg := "substitute without pattern relies on the user's previous search pattern"
				if diag.Message != wantMsg {
					t.Errorf("diagnostic message = %q, want %q", diag.Message, wantMsg)
				}
				if file.Text(diag.Span) != tt.wantSpan {
					t.Errorf("diagnostic span text = %q, want %q", file.Text(diag.Span), tt.wantSpan)
				}
				def, ok := syntax.LookupVimlsDiagnostic("vimls/substitute-empty-pattern")
				if !ok || def.Severity != syntax.DiagnosticWarning {
					t.Errorf("diagnostic definition severity = %v, want DiagnosticWarning", def.Severity)
				}
			} else if diag != nil {
				t.Fatalf("unexpected vimls/substitute-empty-pattern diagnostic: %#v", diag)
			}
		})
	}
}

func TestSubstituteGdefault(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		configFile bool
		wantGdef   bool
		wantSpan   string
	}{
		// Plugin / script files (configFile = false, Legacy): should emit hint
		{
			name:       "legacy substitute without flags in script",
			source:     "s/foo/bar/\n",
			configFile: false,
			wantGdef:   true,
			wantSpan:   "s",
		},
		{
			name:       "legacy substitute with g flag in script",
			source:     "s/foo/bar/g\n",
			configFile: false,
			wantGdef:   true,
			wantSpan:   "s",
		},
		{
			name:       "full substitute name in script",
			source:     "substitute/foo/bar/\n",
			configFile: false,
			wantGdef:   true,
			wantSpan:   "substitute",
		},
		{
			name:       "empty pattern substitute in script",
			source:     "s//bar/\n",
			configFile: false,
			wantGdef:   true,
			wantSpan:   "s",
		},
		{
			name:       "bare substitute in script",
			source:     "s\n",
			configFile: false,
			wantGdef:   true,
			wantSpan:   "s",
		},
		{
			name:       "range substitute in script",
			source:     "%s/foo/bar/g\n",
			configFile: false,
			wantGdef:   true,
			wantSpan:   "s",
		},
		{
			name:       "smagic in script",
			source:     "smagic/foo/bar/g\n",
			configFile: false,
			wantGdef:   true,
			wantSpan:   "smagic",
		},
		{
			name:       "snomagic in script",
			source:     "snomagic/foo/bar/\n",
			configFile: false,
			wantGdef:   true,
			wantSpan:   "snomagic",
		},
		// User config files (configFile = true): should NOT emit hint
		{
			name:       "user config substitute without g",
			source:     "s/foo/bar/\n",
			configFile: true,
			wantGdef:   false,
		},
		{
			name:       "user config substitute with g",
			source:     "s/foo/bar/g\n",
			configFile: true,
			wantGdef:   false,
		},
		{
			name:       "user config bare substitute",
			source:     "s\n",
			configFile: true,
			wantGdef:   false,
		},
		// Vim9 script (gdefault is ignored in Vim9): should NOT emit hint
		{
			name:       "vim9 script substitute with g",
			source:     "vim9script\ns/foo/bar/g\n",
			configFile: false,
			wantGdef:   false,
		},
		{
			name:       "vim9 script substitute without g",
			source:     "vim9script\ns/foo/bar/\n",
			configFile: false,
			wantGdef:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := syntax.Parse(tt.source)
			var analysis *FileAnalysis
			if tt.configFile {
				analysis = AnalyzeConfigFile(file)
			} else {
				analysis = Analyze(file)
			}
			diag := findDiagnostic(analysis.Diagnostics, "vimls/substitute-gdefault")
			if tt.wantGdef {
				if diag == nil {
					t.Fatalf("expected vimls/substitute-gdefault diagnostic, got none in %#v", collectCodes(analysis.Diagnostics))
				}
				wantMsg := "substitute behavior may be affected by 'gdefault'"
				if diag.Message != wantMsg {
					t.Errorf("diagnostic message = %q, want %q", diag.Message, wantMsg)
				}
				if file.Text(diag.Span) != tt.wantSpan {
					t.Errorf("diagnostic span text = %q, want %q", file.Text(diag.Span), tt.wantSpan)
				}
				def, ok := syntax.LookupVimlsDiagnostic("vimls/substitute-gdefault")
				if !ok || def.Severity != syntax.DiagnosticHint {
					t.Errorf("diagnostic definition severity = %v, want DiagnosticHint", def.Severity)
				}
			} else if diag != nil {
				t.Fatalf("unexpected vimls/substitute-gdefault diagnostic: %#v", diag)
			}
		})
	}
}

func TestSubstituteCombinedDiagnostics(t *testing.T) {
	// A substitute command in a legacy plugin that triggers case, magic, and gdefault
	source := "s/foo.*bar/replacement/\n"
	file := syntax.Parse(source)
	analysis := Analyze(file)
	got := collectCodes(analysis.Diagnostics)
	want := []string{
		"vimls/substitute-gdefault",
		"vimls/implicit-pattern-case",
		"vimls/implicit-regex-magic",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("combined diagnostics = %#v, want %#v", got, want)
	}

	// Empty pattern in a legacy plugin triggers empty-pattern and gdefault
	emptySource := "s//replacement/\n"
	emptyFile := syntax.Parse(emptySource)
	emptyAnalysis := Analyze(emptyFile)
	emptyGot := collectCodes(emptyAnalysis.Diagnostics)
	emptyWant := []string{
		"vimls/substitute-gdefault",
		"vimls/substitute-empty-pattern",
	}
	if !reflect.DeepEqual(emptyGot, emptyWant) {
		t.Fatalf("empty pattern diagnostics = %#v, want %#v", emptyGot, emptyWant)
	}

	// Safe substitute with explicit \v and i flag in .vimrc (configFile = true): no diagnostics
	safeSource := "s/\\vfoo.*bar/rep/i\n"
	safeFile := syntax.Parse(safeSource)
	safeAnalysis := AnalyzeConfigFile(safeFile)
	safeGot := collectCodes(safeAnalysis.Diagnostics)
	if len(safeGot) != 0 {
		t.Fatalf("expected no diagnostics for safe substitute in config, got %#v", safeGot)
	}
}

func TestSubstituteInFunctionsAndBlocks(t *testing.T) {
	source := `function! s:Transform() abort
  s/foo/bar/
endfunction
if 1
  s/foo.*bar/rep/g
endif
`
	file := syntax.Parse(source)
	analysis := Analyze(file)
	got := collectCodes(analysis.Diagnostics)
	want := []string{
		"vimls/substitute-gdefault",
		"vimls/implicit-pattern-case",
		"vimls/substitute-gdefault",
		"vimls/implicit-pattern-case",
		"vimls/implicit-regex-magic",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("function/block diagnostics = %#v, want %#v", got, want)
	}
}

func TestGlobalCommandStyleDiagnostics(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		wantCodes []string
		wantSpans map[string]string
		wantMsgs  map[string]string
	}{
		{
			name:      "plain letters pattern in global",
			source:    "g/foo/d\n",
			wantCodes: []string{"vimls/implicit-pattern-case"},
			wantSpans: map[string]string{"vimls/implicit-pattern-case": "foo"},
			wantMsgs:  map[string]string{"vimls/implicit-pattern-case": "global pattern depends on 'ignorecase'; consider '\\c' or '\\C'"},
		},
		{
			name:      "plain letters pattern in vglobal",
			source:    "v/foo/d\n",
			wantCodes: []string{"vimls/implicit-pattern-case"},
			wantSpans: map[string]string{"vimls/implicit-pattern-case": "foo"},
			wantMsgs:  map[string]string{"vimls/implicit-pattern-case": "global pattern depends on 'ignorecase'; consider '\\c' or '\\C'"},
		},
		{
			name:      "plain letters pattern in bang global",
			source:    "g!/foo/d\n",
			wantCodes: []string{"vimls/implicit-pattern-case"},
			wantSpans: map[string]string{"vimls/implicit-pattern-case": "foo"},
		},
		{
			name:      "metacharacters pattern in global",
			source:    "g/foo.*bar/d\n",
			wantCodes: []string{"vimls/implicit-pattern-case", "vimls/implicit-regex-magic"},
			wantSpans: map[string]string{
				"vimls/implicit-pattern-case": "foo.*bar",
				"vimls/implicit-regex-magic":  "foo.*bar",
			},
			wantMsgs: map[string]string{
				"vimls/implicit-regex-magic": "global pattern relies on Vim's magic setting; consider an explicit magic prefix",
			},
		},
		{
			name:      "very magic prefix removes magic dependency",
			source:    "g/\\vfoo.*bar/d\n",
			wantCodes: []string{"vimls/implicit-pattern-case"},
		},
		{
			name:      "magic prefix removes magic dependency",
			source:    "g/\\mfoo.*bar/d\n",
			wantCodes: []string{"vimls/implicit-pattern-case"},
		},
		{
			name:      "case override prefix removes case dependency",
			source:    "g/\\Cfoo/d\n",
			wantCodes: nil,
		},
		{
			name:      "case override lowercase c removes case dependency",
			source:    "g/\\cfoo/d\n",
			wantCodes: nil,
		},
		{
			name:      "mid-pattern case override removes case dependency",
			source:    "g/foo\\cbar/d\n",
			wantCodes: nil,
		},
		{
			name:      "safe global with explicit magic and case override",
			source:    "g/\\v\\Cfoo.*bar/d\n",
			wantCodes: nil,
		},
		{
			name:      "digits only no case or magic dependency",
			source:    "g/123/d\n",
			wantCodes: nil,
		},
		{
			name:      "digit escape sequence no case dependency",
			source:    "g/\\d\\+/d\n",
			wantCodes: []string{"vimls/implicit-regex-magic"},
		},
		{
			name:      "empty pattern in global",
			source:    "g//d\n",
			wantCodes: []string{"vimls/global-empty-pattern"},
			wantSpans: map[string]string{"vimls/global-empty-pattern": "//"},
			wantMsgs:  map[string]string{"vimls/global-empty-pattern": "global without pattern relies on the user's previous search pattern"},
		},
		{
			name:      "empty pattern in vglobal",
			source:    "v//d\n",
			wantCodes: []string{"vimls/global-empty-pattern"},
			wantSpans: map[string]string{"vimls/global-empty-pattern": "//"},
		},
		{
			name:      "bare global command",
			source:    "g\n",
			wantCodes: []string{"vimls/global-empty-pattern"},
			wantSpans: map[string]string{"vimls/global-empty-pattern": "g"},
		},
		{
			name:      "bare vglobal command",
			source:    "v\n",
			wantCodes: []string{"vimls/global-empty-pattern"},
			wantSpans: map[string]string{"vimls/global-empty-pattern": "v"},
		},
		{
			name:      "global previous pattern form",
			source:    "g\\/d\n",
			wantCodes: []string{"vimls/global-empty-pattern"},
			wantSpans: map[string]string{"vimls/global-empty-pattern": "\\/"},
		},
		{
			name:      "global single delimiter",
			source:    "g/\n",
			wantCodes: []string{"vimls/global-empty-pattern"},
			wantSpans: map[string]string{"vimls/global-empty-pattern": "/"},
		},
		{
			name:      "vim9 script global pattern",
			source:    "vim9script\ng/foo/d\n",
			wantCodes: []string{"vimls/implicit-pattern-case"},
			wantSpans: map[string]string{"vimls/implicit-pattern-case": "foo"},
		},
		{
			name:      "vim9 script global empty pattern",
			source:    "vim9script\ng//d\n",
			wantCodes: []string{"vimls/global-empty-pattern"},
			wantSpans: map[string]string{"vimls/global-empty-pattern": "//"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := syntax.Parse(tt.source)
			analysis := Analyze(file)
			got := collectCodes(analysis.Diagnostics)
			if !reflect.DeepEqual(got, tt.wantCodes) {
				t.Fatalf("codes = %#v, want %#v", got, tt.wantCodes)
			}
			for code, wantSpan := range tt.wantSpans {
				d := findDiagnostic(analysis.Diagnostics, code)
				if d == nil {
					t.Fatalf("missing diagnostic %s", code)
				}
				if file.Text(d.Span) != wantSpan {
					t.Errorf("diagnostic %s span = %q, want %q", code, file.Text(d.Span), wantSpan)
				}
			}
			for code, wantMsg := range tt.wantMsgs {
				d := findDiagnostic(analysis.Diagnostics, code)
				if d == nil {
					t.Fatalf("missing diagnostic %s", code)
				}
				if d.Message != wantMsg {
					t.Errorf("diagnostic %s message = %q, want %q", code, d.Message, wantMsg)
				}
			}
		})
	}
}
