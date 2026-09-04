package server

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestCompletionContextSpecificAndRejectedSyntax(t *testing.T) {
	cases := []struct {
		source, at string
		want       completionContext
	}{
		{"set noig", "noig", completionContextSetOption}, {"syntax key", "key", completionContextSyntaxSubcommand}, {"syntax keyword Group key", "Group", completionContextSyntaxGroup}, {"highlight link One Two", "Two", completionContextHighlight}, {"autocmd BufE *.vim echo 1", "BufE", completionContextAutocmdHead}, {"autocmd Group BufE *.vim echo 1", "BufE", completionContextAutocmdEvent}, {"autocmd BufEnter <bu", "<bu", completionContextAutocmdPattern}, {"autocmd BufEnter * +", "+", completionContextAutocmdModifier}, {"silent echo 1", "silent", completionContextModifier},
		{"set path+", "path+", completionContextSetOperator}, {"set ff=un", "un", completionContextSetValue},
		{"augroup Pro", "Pro", completionContextAugroup}, {"command -co", "-co", completionContextUserCommandAttribute}, {"command -nargs=", "-nargs=", completionContextUserCommandAttributeValue},
		{"command Build echo -nargs=", "-nargs=", completionContextExpression}, {"augroup One extra", "extra", completionContextExpression},
		{":", ":", completionContextCommand},
		{"vim9script\ndef Run()\n  \nenddef", "Run()\n  ", completionContextVim9Statement},
		{"vim9script\ndef Run()\n  T\nenddef", "T", completionContextVim9Statement},
		{"vim9script\ndef Run()\n  f\nenddef", "f", completionContextVim9Statement},
		{"vim9script\nfunction Run()\n  T\nendfunction", "T", completionContextCommand},
		{"vim9script\ndef Run()\n  return values({})->\nenddef", "->", completionContextMethod},
		{"vim9script\ndef Run()\n  return values({})->flat\nenddef", "flat", completionContextMethod},
		{"vim9script\ndef Run()\n  return values({})->  flat\nenddef", "flat", completionContextMethod},
		{"vim9script\ndef Run()\n  return values({})\n    ->flat\nenddef", "flat", completionContextMethod},
		{"vim9script\ndef Run()\n  return 1->s:A\nenddef", "s:A", completionContextMethod},
		{"vim9script\ndef Run()\n  return []->foo#bar#Tra\nenddef", "foo#bar#Tra", completionContextMethod},
		{"nmap <bu", "<bu", completionContextMappingArgument}, {"nmap <buffer> <si", "<si", completionContextMappingArgument},
		{"highlight Normal cte", "cte", completionContextHighlightKey}, {"highlight Normal cterm=bo", "bo", completionContextHighlightValue}, {"highlight Normal cterm=bold,un", "un", completionContextHighlightValue},
		{"highlight Normal cterm = under", "under", completionContextHighlightValue},
		{"highlight Normal ", "Normal ", completionContextHighlightKey}, {"highlight Normal guifg='salmon pink'", "pink", completionContextNone},
		{"colorscheme ", "colorscheme ", completionContextColorscheme}, {"colorscheme def", "def", completionContextColorscheme}, {"colorscheme default extra", "extra", completionContextExpression},
		{"echo has('gui_')", "gui_", completionContextHasFeature}, {"echo has(\"patch-9.2\")", "patch-9.2", completionContextHasFeature},
		{"echo expand('<cf')", "<cf", completionContextExpandSpecial}, {"echo expand(\"%\")", "%", completionContextExpandSpecial},
		{"echo '<cf'->expand()", "<cf", completionContextExpandSpecial},
		{"echo expand('<cfile>:p')", ":p", completionContextNone}, {"echo other('<cf')", "<cf", completionContextNone}, {"echo has('gui_')", "'gui_'", completionContextExpression},
		{"nmap lhs", "lhs", completionContextNone}, {"nmap lhs <bu", "<bu", completionContextNone},
		{"echo 'value'", "value", completionContextNone}, {"\" echo value", "value", completionContextNone}, {"map x value", "value", completionContextNone}, {"loadkeymap\na a", "a a", completionContextNone},
		{"let x =<< END\nvalue\nEND", "value", completionContextNone}, {"append\nvalue\n.", "value", completionContextNone}, {"finish\nvalue", "value", completionContextNone},
	}
	for _, test := range cases {
		file := syntax.Parse(test.source)
		offset := len(test.source)
		for i := 0; i+len(test.at) <= len(test.source); i++ {
			if test.source[i:i+len(test.at)] == test.at {
				offset = i + len(test.at)
				break
			}
		}
		if got := completionContextAt(file, offset); got != test.want {
			t.Errorf("%q at %q = %d, want %d", test.source, test.at, got, test.want)
		}
	}
}

func TestCompletionMethodContextRequiresArrowExpression(t *testing.T) {
	for _, test := range []struct {
		source, at string
	}{
		{"s/foo/->", "->"},
		{"Unknown ->", "->"},
		{"echo '->", "->"},
		{"vim9script\ndef Run()\n  return values({})->\n  flat\nenddef", "flat"},
	} {
		file := syntax.Parse(test.source)
		offset := strings.Index(test.source, test.at) + len(test.at)
		if got := completionContextAt(file, offset); got == completionContextMethod {
			t.Errorf("%q unexpectedly returned method context", test.source)
		}
	}
}

func TestCompletionSelectionsClampInvalidOffsets(t *testing.T) {
	for _, offset := range []int{-10, 10} {
		selection := completionSelectionAt("abc", offset)
		if selection.start < 0 || selection.start > selection.cursor || selection.cursor > selection.end || selection.end > 3 {
			t.Fatalf("identifier selection at %d = %#v", offset, selection)
		}
		selection = completionImportPathSelection("abc", offset)
		if selection.start < 0 || selection.start > selection.cursor || selection.cursor > selection.end || selection.end > 3 {
			t.Fatalf("path selection at %d = %#v", offset, selection)
		}
		selection = completionMappingArgumentSelection("abc", offset)
		if selection.start < 0 || selection.start > selection.cursor || selection.cursor > selection.end || selection.end > 3 {
			t.Fatalf("mapping selection at %d = %#v", offset, selection)
		}
		selection = completionColorschemeSelection("abc", offset)
		if selection.start < 0 || selection.start > selection.cursor || selection.cursor > selection.end || selection.end > 3 {
			t.Fatalf("colorscheme selection at %d = %#v", offset, selection)
		}
		file := syntax.Parse("echo has('abc')")
		selection = completionBuiltinStringSelection(file, offset, completionContextHasFeature)
		if selection.start < 0 || selection.start > selection.cursor || selection.cursor > selection.end || selection.end > len(file.Source) {
			t.Fatalf("builtin string selection at %d = %#v", offset, selection)
		}
	}
}

func FuzzCompletionContext(f *testing.F) {
	for _, source := range []string{"", "vim9script\necho value", "import './lib.vim' as lib", "echo obj.member", "let x =<< END\nbody\nEND"} {
		f.Add(source, 0)
		f.Add(source, len(source))
	}
	f.Fuzz(func(t *testing.T, source string, offset int) {
		file := syntax.Parse(source)
		_ = completionContextAt(file, offset)
		selection := completionSelectionAt(source, offset)
		if selection.start < 0 || selection.start > selection.cursor || selection.cursor > selection.end || selection.end > len(source) {
			t.Fatalf("selection = %#v for %d bytes at %d", selection, len(source), offset)
		}
		path := completionImportPathSelection(source, offset)
		if path.start < 0 || path.start > path.cursor || path.cursor > path.end || path.end > len(source) {
			t.Fatalf("path selection = %#v for %d bytes at %d", path, len(source), offset)
		}
		colorscheme := completionColorschemeSelection(source, offset)
		if colorscheme.start < 0 || colorscheme.start > colorscheme.cursor || colorscheme.cursor > colorscheme.end || colorscheme.end > len(source) {
			t.Fatalf("colorscheme selection = %#v for %d bytes at %d", colorscheme, len(source), offset)
		}
		contextKind := completionContextAt(file, offset)
		if contextKind == completionContextHasFeature || contextKind == completionContextExpandSpecial {
			selection := completionBuiltinStringSelection(file, offset, contextKind)
			if selection.start < 0 || selection.start > selection.cursor || selection.cursor > selection.end || selection.end > len(source) {
				t.Fatalf("builtin string selection = %#v for %d bytes at %d", selection, len(source), offset)
			}
		}
	})
}

func BenchmarkCompletionContext(b *testing.B) {
	contexts := map[string]string{
		"command":     "sil",
		"expression":  "echo strl",
		"member":      "echo obj.mem",
		"import_path": "vim9script\nimport './li' as lib",
	}
	for _, size := range []int{1024, 100 * 1024} {
		for name, tail := range contexts {
			b.Run(name+"/"+benchmarkSizeName(size), func(b *testing.B) {
				prefix := strings.Repeat("\" padding\n", size/10)
				source := prefix + tail
				file := syntax.Parse(source)
				offset := len(source)
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					_ = completionContextAt(file, offset)
					_ = completionSelectionAt(source, offset)
				}
			})
		}
	}
}

func benchmarkSizeName(size int) string {
	if size == 1024 {
		return "1KiB"
	}
	return "100KiB"
}

func TestImportMemberContextIsPhysicalLineBounded(t *testing.T) {
	if _, ok := importMemberContext("alias.\nRun", len("alias.\nRun")); ok {
		t.Fatal("cross-line member accepted")
	}
	long := "alias." + strings.Repeat("x", 257)
	if _, ok := importMemberContext(long, len(long)); ok {
		t.Fatal("long member scan accepted")
	}
	longPrefix := strings.Repeat("x", 300) + " alias.Ru"
	if alias, ok := importMemberContext(longPrefix, len(longPrefix)); !ok || alias != "alias" {
		t.Fatalf("nearby member after long line prefix = %q, %t", alias, ok)
	}
}
