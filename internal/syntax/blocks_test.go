package syntax

import (
	"strings"
	"testing"
)

func TestBuildsNestedRecoveringBlocks(t *testing.T) {
	file := Parse("vim9script\ndef Build(): number\n  if true\n    return 1\n  else\n    return 2\n  endif\nenddef\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Blocks) != 2 || file.Blocks[0].Kind != BlockDef || file.Blocks[1].Kind != BlockIf || file.Blocks[1].Parent != 0 {
		t.Fatalf("blocks = %#v", file.Blocks)
	}
	if len(file.Blocks[1].Branches) != 1 || file.Commands[file.Blocks[1].Branches[0]].Canonical != "else" {
		t.Fatalf("if block = %#v", file.Blocks[1])
	}
	if file.Blocks[0].End < 0 || file.Text(file.Blocks[0].Span) != "def Build(): number\n  if true\n    return 1\n  else\n    return 2\n  endif\nenddef" {
		t.Fatalf("def block = %#v, text = %q", file.Blocks[0], file.Text(file.Blocks[0].Span))
	}
}

func TestVim9InvalidNestedDefHeaderStopsSignatureDiagnostics(t *testing.T) {
	// Vim v9.2.1015 src/testdir/test_vim9_func.vim:1290-1298 expects only
	// E476 for the invalid nested command; +Func+ is not a function signature.
	file := Parse("vim9script\ndef Func()\n  def +Func+\nenddef\ndefcompile\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E476" || file.Text(file.Diagnostics[0].Span) != "def" {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockDef || file.Blocks[0].End != 3 || len(file.Commands) != 5 || file.Commands[4].Canonical != "defcompile" {
		t.Fatalf("commands = %#v, blocks = %#v", file.Commands, file.Blocks)
	}
	assertFileSpans(t, file)
}

func TestVim9MissingEnddefDiagnostic(t *testing.T) {
	incomplete := Parse("vim9script\ndef Func()\n  return 1\n  var after = 1\n")
	if len(incomplete.Diagnostics) != 1 || incomplete.Diagnostics[0].Code != "vim/E1057" || incomplete.Diagnostics[0].Message != "Missing :enddef" || incomplete.Text(incomplete.Diagnostics[0].Span) != "def" {
		t.Fatalf("diagnostics = %#v", incomplete.Diagnostics)
	}
	if len(incomplete.Commands) != 4 || len(incomplete.Blocks) != 1 || incomplete.Blocks[0].Kind != BlockDef || incomplete.Blocks[0].End != -1 || incomplete.Commands[3].Declaration == nil || incomplete.Text(incomplete.Commands[3].Declaration.Name) != "after" {
		t.Fatalf("commands = %#v, blocks = %#v", incomplete.Commands, incomplete.Blocks)
	}
	assertFileSpans(t, incomplete)

	closed := Parse("vim9script\ndef Func()\n  return 1\nenddef\n")
	if hasDiagnostic(closed, "vim/E1057") {
		t.Fatalf("closed def diagnostics = %#v", closed.Diagnostics)
	}
	legacy := (LegacyParser{}).Parse("function! Func()\n  return 1\n")
	if hasDiagnostic(legacy, "vim/E1057") {
		t.Fatalf("legacy function diagnostics = %#v", legacy.Diagnostics)
	}
}

func TestMissingEndfunctionDiagnostic(t *testing.T) {
	for _, test := range []struct {
		name, source string
	}{
		{
			name:   "Vim9 root legacy function",
			source: "vim9script\nfunction Some()\n  echo 'test'\n  enfffunc\n",
		},
		{
			name:   "Legacy root function",
			source: "function Some()\n  echo 'test'\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E126" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != "Missing :endfunction" || file.Text(got[0].Span) != "function" {
				t.Fatalf("E126 diagnostics = %#v", file.Diagnostics)
			}
			if len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockFunction || file.Blocks[0].End != -1 {
				t.Fatalf("function recovery block = %#v", file.Blocks)
			}
		})
	}

	for _, source := range []string{
		"function Some()\nendfunction\n",
		"vim9script\nfunction Some()\nendfunction\n",
		"function Some\n",
		"function Some invalid tail\n",
		"vim9script\ndef Some()\nenddef\n",
	} {
		file := Parse(source)
		if hasDiagnostic(file, "vim/E126") {
			t.Fatalf("guard source unexpectedly received E126: %#v\n%s", file.Diagnostics, source)
		}
	}
}

func TestVim9NestedRedirDiagnostics(t *testing.T) {
	tests := []struct {
		name, source string
		wantE1092    int
		wantE1185    int
	}{
		{
			name: "official ordinary redir nested after variable redir",
			source: "vim9script\ndef Func()\n  redir => text\n  redir > Xnopfile\n" +
				"  redir END\nenddef\n",
			wantE1092: 1,
		},
		{
			name: "nested variable redir retains original state through END",
			source: "vim9script\ndef Func()\n  redir => text\n  redir =>> more\n" +
				"  redir END\nenddef\n",
			wantE1092: 1,
		},
		{
			name: "separate defs and ordinary redir remain independent",
			source: "vim9script\ndef First()\n  redir => text\n  redir END\nenddef\n" +
				"def Second()\n  redir > Xnopfile\n  redir END\nenddef\n",
		},
		{
			name: "legacy redir remains unaffected",
			source: "function Legacy()\n  redir => text\n  redir > Xnopfile\n" +
				"  redir END\nendfunction\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var e1092, e1185 []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				switch diagnostic.Code {
				case "vim/E1092":
					e1092 = append(e1092, diagnostic)
				case "vim/E1185":
					e1185 = append(e1185, diagnostic)
				}
			}
			if len(e1092) != test.wantE1092 || len(e1185) != test.wantE1185 {
				t.Fatalf("diagnostics = %#v, want E1092=%d E1185=%d", file.Diagnostics, test.wantE1092, test.wantE1185)
			}
			for _, diagnostic := range e1092 {
				if diagnostic.Message != "Cannot nest :redir" || file.Text(diagnostic.Span) != "redir" {
					t.Fatalf("E1092 diagnostic = %#v on %q", diagnostic, file.Text(diagnostic.Span))
				}
			}
		})
	}
}

func TestVim9MismatchedFunctionClosers(t *testing.T) {
	tests := []struct {
		name, source, code, message, span string
	}{
		{
			name:   "endfunction in def",
			source: "def Test()\n  echo 'test'\n  endfunc\nenddef\nvar after = 1\n",
			code:   "vim/E1151", message: "Mismatched endfunction", span: "endfunc",
		},
		{
			name:   "enddef in function",
			source: "def Test()\n  func Nested()\n    echo 'test'\n  enddef\nenddef\nvar after = 1\n",
			code:   "vim/E1152", message: "Mismatched enddef", span: "enddef",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := (Vim9Parser{}).Parse(test.source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == test.code {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, all = %#v", got, file.Diagnostics)
			}
			if file.Commands[len(file.Commands)-1].Declaration == nil || file.Text(file.Commands[len(file.Commands)-1].Declaration.Name) != "after" {
				t.Fatalf("next-line recovery = %#v", file.Commands)
			}
			assertFileSpans(t, file)
		})
	}

	for _, source := range []string{
		"def Test()\nenddef\n",
		"function Test()\nendfunction\n",
		"def Test()\nendfunction!\nenddef\n",
	} {
		file := (Vim9Parser{}).Parse(source)
		if hasDiagnostic(file, "vim/E1151") || hasDiagnostic(file, "vim/E1152") {
			t.Fatalf("valid or bang closer mismatch = %#v\n%s", file.Diagnostics, source)
		}
	}
}

func TestNestedFunctionBangDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, message string
		wantE1117, wantE477   int
		wantAfter             bool
	}{
		{
			name: "nested def bang",
			source: "vim9script\ndef Outer()\n  def Inner()\n  enddef\n" +
				"  def! Inner()\n  enddef\nenddef\nvar after = 1\n",
			message: "Cannot use ! with nested :def", wantE1117: 1, wantAfter: true,
		},
		{
			name: "nested function bang takes precedence over E477",
			source: "vim9script\ndef Outer()\n  def Inner()\n  enddef\n" +
				"  function! Inner()\n  endfunction\nenddef\nvar after = 1\n",
			message: "Cannot use ! with nested :function", wantE1117: 1, wantAfter: true,
		},
		{
			name: "deep and Legacy-root defs",
			source: "def Outer()\n  if true\n    def! Inner()\n    enddef\n" +
				"  endif\nenddef\nvar after = 1\n",
			message: "Cannot use ! with nested :def", wantE1117: 1, wantAfter: true,
		},
		{
			name:   "top-level Vim9 def bang",
			source: "vim9script\ndef! Top()\nenddef\n",
		},
		{
			name:     "top-level Vim9 function bang retains E477",
			source:   "vim9script\nfunction! TopFunction()\nendfunction\n",
			wantE477: 1,
		},
		{
			name:   "top-level Legacy function bang",
			source: "function! Legacy()\nendfunction\n",
		},
		{
			name:   "def inside Legacy function has no def ancestor",
			source: "function Legacy()\n  def! Inner()\n  enddef\nendfunction\n",
		},
		{
			name: "nested headers without bang",
			source: "vim9script\ndef Outer()\n  def Inner()\n  enddef\n" +
				"  function InnerFunction()\n  endfunction\nenddef\n",
		},
		{
			name: "bang closers are excluded",
			source: "vim9script\ndef Outer()\n  def Inner()\n  enddef!\n" +
				"  function InnerFunction()\n  endfunction!\nenddef\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			var got []Diagnostic
			var e477 []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1117" {
					got = append(got, diagnostic)
				}
				if diagnostic.Code == "vim/E477" {
					e477 = append(e477, diagnostic)
				}
			}
			if len(got) != test.wantE1117 || len(e477) != test.wantE477 {
				t.Fatalf("E1117=%#v E477=%#v, want E1117=%d E477=%d; all diagnostics = %#v", got, e477, test.wantE1117, test.wantE477, file.Diagnostics)
			}
			for _, diagnostic := range got {
				if diagnostic.Message != test.message || file.Text(diagnostic.Span) != "!" {
					t.Fatalf("E1117 diagnostic = %#v on %q", diagnostic, file.Text(diagnostic.Span))
				}
			}
			for _, diagnostic := range e477 {
				if diagnostic.Message != "No ! allowed" || file.Text(diagnostic.Span) != "!" {
					t.Fatalf("E477 diagnostic = %#v on %q", diagnostic, file.Text(diagnostic.Span))
				}
			}
			if test.wantAfter {
				found := false
				for index := range file.Commands {
					command := &file.Commands[index]
					if command.Declaration != nil && file.Text(command.Declaration.Name) == "after" {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("following declaration recovery = %#v", file.Commands)
				}
			}
		})
	}
}

func TestLegacyEndfunctionClosesUnterminatedControlBlocks(t *testing.T) {
	file := (LegacyParser{}).Parse("function! Complete()\n  if 1\n    while 1\n      return\nendfunction\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Blocks) != 3 {
		t.Fatalf("blocks = %#v", file.Blocks)
	}
	for index, block := range file.Blocks {
		if block.End != 4 || block.Span.End != file.Commands[4].Span.End {
			t.Fatalf("block %d = %#v, endfunction = %#v", index, block, file.Commands[4])
		}
	}
}

func TestLegacyEndfunctionBangClosesFunctionWithoutSyntaxError(t *testing.T) {
	source := "function! Complete()\n  return 1\nendfunction!\nlet g:after = 2\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 4 || len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockFunction || file.Blocks[0].End != 2 {
		t.Fatalf("commands = %#v, blocks = %#v", file.Commands, file.Blocks)
	}
	end := file.Commands[2]
	if end.Canonical != "endfunction" || file.Text(end.Bang) != "!" || end.Block != 0 {
		t.Fatalf("endfunction = %#v", end)
	}
	if file.Commands[3].Declaration == nil || file.Text(file.Commands[3].Declaration.Name) != "g:after" {
		t.Fatalf("following command = %#v", file.Commands[3])
	}
	assertFileSpans(t, file)
}

func TestTopLevelEndfunctionBangRetainsBothRecoveryDiagnostics(t *testing.T) {
	file := (LegacyParser{}).Parse("endfunction!\nlet g:after = 1\n")
	codes := map[string]int{}
	for _, diagnostic := range file.Diagnostics {
		codes[diagnostic.Code]++
	}
	if codes["vim/E477"] != 1 || codes["vimls/unexpected-end"] != 1 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 2 || file.Text(file.Commands[0].Bang) != "!" || file.Commands[1].Declaration == nil {
		t.Fatalf("commands = %#v", file.Commands)
	}
}

func TestLegacyEndfunctionBangRestoresVim9Dialect(t *testing.T) {
	file := Parse("vim9script\nfunction Old()\nendfunction!\nvar after = 1\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 || len(file.Blocks) != 1 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	// The :function header is read in the surrounding Vim9 command context;
	// only its body and terminator switch to legacy syntax.
	if file.Commands[1].Dialect != Vim9 || file.Commands[2].Dialect != Legacy || file.Text(file.Commands[2].Bang) != "!" {
		t.Fatalf("legacy function commands = %#v", file.Commands[1:3])
	}
	if file.Commands[3].Dialect != Vim9 || file.Commands[3].Declaration == nil {
		t.Fatalf("following Vim9 command = %#v", file.Commands[3])
	}
}

func TestLegacyWindoMultilineBlock(t *testing.T) {
	source := "function! s:Test()\n  if 1\n    keepjumps windo if getline(2) =~# \"Netrw\"\n      let s:count = s:count + 1\n    endif\n  endif\nendfunction\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	outer := &file.Commands[2]
	if outer.Canonical != "windo" || outer.Embedded == nil || len(outer.Embedded.Commands) != 3 || len(outer.Embedded.Blocks) != 1 {
		t.Fatalf("windo = %#v", outer)
	}
	if got := file.Text(outer.Embedded.Span); got != "if getline(2) =~# \"Netrw\"\n      let s:count = s:count + 1\n    endif" {
		t.Fatalf("embedded text = %q", got)
	}
	if outer.Embedded.Blocks[0].End != 2 || file.Text(outer.Embedded.Blocks[0].Span) != "if getline(2) =~# \"Netrw\"\n      let s:count = s:count + 1\n    endif" {
		t.Fatalf("embedded block = %#v", outer.Embedded.Blocks[0])
	}
	if file.Commands[3].Canonical != "endif" || file.Commands[4].Canonical != "endfunction" {
		t.Fatalf("outer commands = %#v", file.Commands)
	}
}

func TestLegacyWindoSimpleCommandDoesNotConsumeNextLine(t *testing.T) {
	file := (LegacyParser{}).Parse("windo echo value\nedit after.txt\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[0].Canonical != "windo" || file.Commands[1].Canonical != "edit" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
}

func TestLegacyWindoMultilineBlockInsideVim9File(t *testing.T) {
	source := "vim9script\nfunction Old()\n  windo if getline(1) ==# 'x'\n    let g:seen = 1\n  endif\nendfunction\nvar after = 1\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	windo := file.Commands[2]
	if windo.Dialect != Legacy || windo.Embedded == nil || len(windo.Embedded.Commands) != 3 || len(windo.Embedded.Blocks) != 1 {
		t.Fatalf("windo = %#v", windo)
	}
	if file.Commands[3].Canonical != "endfunction" || file.Commands[4].Dialect != Vim9 || file.Commands[4].Declaration == nil {
		t.Fatalf("following commands = %#v", file.Commands[3:])
	}
}

func TestEmbeddedDoCommandUsesEnclosingDialectAfterLocalModifier(t *testing.T) {
	legacy := (LegacyParser{}).Parse("vim9cmd windo let g:old = 1\n")
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 1 {
		t.Fatalf("legacy commands = %#v, diagnostics = %#v", legacy.Commands, legacy.Diagnostics)
	}
	legacyOuter := &legacy.Commands[0]
	if legacyOuter.Dialect != Vim9 || legacyOuter.Embedded == nil || len(legacyOuter.Embedded.Commands) != 1 {
		t.Fatalf("legacy outer = %#v", legacyOuter)
	}
	legacyNested := &legacyOuter.Embedded.Commands[0]
	if legacyNested.Dialect != Legacy || legacyNested.Declaration == nil || legacy.Text(legacyNested.Declaration.Name) != "g:old" {
		t.Fatalf("legacy nested = %#v", legacyNested)
	}

	vim9 := (Vim9Parser{}).Parse("vim9script\nlegacy windo var fresh = 1\n")
	if len(vim9.Diagnostics) != 0 || len(vim9.Commands) != 2 {
		t.Fatalf("Vim9 commands = %#v, diagnostics = %#v", vim9.Commands, vim9.Diagnostics)
	}
	vim9Outer := &vim9.Commands[1]
	if vim9Outer.Dialect != Legacy || vim9Outer.Embedded == nil || len(vim9Outer.Embedded.Commands) != 1 {
		t.Fatalf("Vim9 outer = %#v", vim9Outer)
	}
	vim9Nested := &vim9Outer.Embedded.Commands[0]
	if vim9Nested.Dialect != Vim9 || vim9Nested.Declaration == nil || vim9.Text(vim9Nested.Declaration.Name) != "fresh" {
		t.Fatalf("Vim9 nested = %#v", vim9Nested)
	}
	assertFileSpans(t, legacy)
	assertFileSpans(t, vim9)
}

func TestBlockRecoveryKeepsLaterCommands(t *testing.T) {
	file := (Vim9Parser{}).Parse("if true\n  while false\nendif\nvar after = 1\nendfor\n")
	if len(file.Commands) != 5 || file.Commands[3].Canonical != "var" || file.Commands[3].Declaration == nil {
		t.Fatalf("commands = %#v", file.Commands)
	}
	codes := map[string]int{}
	for _, diagnostic := range file.Diagnostics {
		codes[diagnostic.Code]++
	}
	if codes["vimls/missing-end"] != 1 || codes["vim/E588"] != 1 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
}

func TestVim9BareForRecovery(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantTryEnd int
	}{
		{name: "unfinished try", source: "vim9script\ntry\nfor\nif\nendwhile\nif\nfinally\n", wantTryEnd: -1},
		{name: "closed try", source: "vim9script\ntry\nfor\nif\nendwhile\nif\nendtry\n", wantTryEnd: 6},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E690" || file.Diagnostics[0].Message != `Missing "in" after :for` || file.Text(file.Diagnostics[0].Span) != "for" {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
			if len(file.Commands) != 7 {
				t.Fatalf("commands = %#v, blocks = %#v", file.Commands, file.Blocks)
			}
			last := "finally"
			if test.wantTryEnd >= 0 {
				last = "endtry"
			}
			for index, want := range []string{"vim9script", "try", "for", "if", "endwhile", "if", last} {
				if file.Commands[index].Canonical != want {
					t.Fatalf("command %d = %#v, want %q", index, file.Commands[index], want)
				}
			}
			loopCommand := file.Commands[2]
			if loopCommand.For == nil || len(loopCommand.For.Bindings) != 0 || loopCommand.For.Iterable != nil || loopCommand.For.IterableSpan.Start != loopCommand.Argument.End || loopCommand.For.IterableSpan.End != loopCommand.Argument.End {
				t.Fatalf("for loop = %#v", loopCommand.For)
			}
			if len(file.Blocks) != 4 || loopCommand.Block != 1 || file.Blocks[1].Kind != BlockFor || file.Blocks[1].Parent != 0 || file.Blocks[1].Span.End != file.Commands[6].Span.Start {
				t.Fatalf("for block = %#v, blocks = %#v", file.Blocks[1], file.Blocks)
			}
			if file.Blocks[0].Kind != BlockTry || file.Blocks[0].End != test.wantTryEnd {
				t.Fatalf("outer try block = %#v", file.Blocks)
			}
			if test.wantTryEnd < 0 {
				if file.Commands[6].Canonical != "finally" || file.Commands[6].Block != 0 {
					t.Fatalf("finally recovery = %#v", file.Commands[6])
				}
			} else if file.Commands[6].Canonical != "endtry" || file.Commands[6].Block != 0 {
				t.Fatalf("endtry recovery = %#v", file.Commands[6])
			}
			assertFileSpans(t, file)
		})
	}
}

func TestVim9E1033CatchAfterCatchAll(t *testing.T) {
	for _, source := range []string{
		"try\necho 0\ncatch\ncatch\nendtry\n",
		"try\necho 0\ncatch\ncatch /later/\nendtry\n",
	} {
		file := (Vim9Parser{}).Parse(source)
		wantSpan := file.Commands[3].Name
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1033" || file.Diagnostics[0].Message != "Catch unreachable after catch-all" || file.Diagnostics[0].Span != wantSpan {
			t.Fatalf("diagnostics = %#v", file.Diagnostics)
		}
		if len(file.Blocks) != 1 || len(file.Blocks[0].Branches) != 2 || file.Commands[file.Blocks[0].Branches[1]].Canonical != "catch" {
			t.Fatalf("try block = %#v, commands = %#v", file.Blocks, file.Commands)
		}
	}
}

func TestVim9E1033PatternedCatchesRemainReachable(t *testing.T) {
	file := (Vim9Parser{}).Parse("try\ncatch /first/\ncatch /second/\nendtry\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
}

func TestVim9E1033CatchAllStateIsScopedToTryBlock(t *testing.T) {
	file := (Vim9Parser{}).Parse("try\ncatch\n  try\n  catch /inner/\n  catch /inner-again/\n  endtry\nendtry\ntry\ncatch /later/\ncatch /later-again/\nendtry\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
}

func TestVim9E1033CatchAfterCatchAllSurvivesRecovery(t *testing.T) {
	file := (Vim9Parser{}).Parse("try\necho 0\ncatch\ncatch\n")
	wantSpan := file.Commands[3].Name
	var unreachable, missingEnd bool
	for _, diagnostic := range file.Diagnostics {
		switch diagnostic.Code {
		case "vim/E1033":
			unreachable = diagnostic.Message == "Catch unreachable after catch-all" && diagnostic.Span == wantSpan
		case "vimls/missing-end":
			missingEnd = true
		}
	}
	if !unreachable || !missingEnd {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
}

func TestVim9ClassMemberPrefixesExposeUnderlyingSyntax(t *testing.T) {
	file := (Vim9Parser{}).Parse("abstract class Shape\n  public static final Count: number = 1\n  abstract def Draw()\nendclass\n")
	if len(file.Diagnostics) != 0 || len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockClass {
		t.Fatalf("blocks = %#v, diagnostics = %#v", file.Blocks, file.Diagnostics)
	}
	if file.Commands[0].Canonical != "class" || len(file.Commands[0].Modifiers) != 1 || file.Commands[1].Canonical != "final" || len(file.Commands[1].Modifiers) != 2 || file.Commands[1].Declaration == nil {
		t.Fatalf("commands = %#v", file.Commands)
	}
	if file.Commands[2].Canonical != "def" || len(file.Commands[2].Modifiers) != 1 {
		t.Fatalf("abstract method = %#v", file.Commands[2])
	}
}

func TestOfficialVim9StandaloneScopeBlock(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_script.vim
	// Test_for_loop_with_closure.
	file := Parse("vim9script\nfor i in range(2)\n  {\n    var copy = i\n  } # scope end\nendfor\n")
	if len(file.Diagnostics) != 0 || len(file.Blocks) != 2 || file.Blocks[0].Kind != BlockFor || file.Blocks[1].Kind != BlockScope || file.Blocks[1].Parent != 0 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	if file.Commands[2].Kind != CommandBlockStart || file.Commands[4].Kind != CommandBlockEnd {
		t.Fatalf("scope commands = %#v, %#v", file.Commands[2], file.Commands[4])
	}
}

func TestOfficialFunctionListingDoesNotOpenBlock(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_import.vim
	// Test_vim9script_autoload_call.
	file := Parse("vim9script\nverbose function another.Getother\nverbose function another#Getother\n")
	if len(file.Diagnostics) != 0 || len(file.Blocks) != 0 || len(file.Commands) != 3 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	for _, command := range file.Commands[1:] {
		if command.Canonical != "function" || command.Function != nil {
			t.Fatalf("listing = %#v", command)
		}
	}
}

func TestOfficialLegacyFunctionDefinitionAllowsSpaceBeforeParameters(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_func.vim Test_func_command_in_legacy_context.
	file := (LegacyParser{}).Parse("func Test ()\n  echo 'test'\nendfunc\n")
	if len(file.Diagnostics) != 0 || len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockFunction || file.Blocks[0].End != 2 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
}

func TestFunctionNestingTooDeepDiagnostic(t *testing.T) {
	for _, test := range []struct {
		name, prefix, header, end, span string
	}{
		{name: "Legacy function", header: "function X()\n", end: "endfunction\n", span: "function"},
		{name: "Vim9 def", prefix: "vim9script\n", header: "def X()\n", end: "enddef\n", span: "def"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, depth := range []int{50, 51} {
				source := nestedNamedFunctionSource(test.prefix, test.header, test.end, depth)
				file := Parse(source)
				var got []Diagnostic
				for _, diagnostic := range file.Diagnostics {
					if diagnostic.Code == "vim/E1058" {
						got = append(got, diagnostic)
					}
				}
				if depth == 50 {
					if len(got) != 0 {
						t.Fatalf("depth 50 diagnostics = %#v", file.Diagnostics)
					}
					continue
				}
				wantStart := strings.LastIndex(source, strings.TrimSuffix(test.header, "\n"))
				if len(got) != 1 || got[0].Message != "Function nesting too deep" || file.Text(got[0].Span) != test.span || got[0].Span.Start != wantStart {
					t.Fatalf("depth 51 E1058 diagnostics = %#v, want one on final %s header; all diagnostics = %#v", got, test.span, file.Diagnostics)
				}
			}
		})
	}
}

func nestedNamedFunctionSource(prefix, header, end string, depth int) string {
	var source strings.Builder
	source.WriteString(prefix)
	for range depth {
		source.WriteString(header)
	}
	for range depth {
		source.WriteString(end)
	}
	return source.String()
}

func TestVim9ClassPublicMethodDiagnostic(t *testing.T) {
	for _, declaration := range []string{
		"public def Foo()",
		"public static def Foo()",
		"public def _Foo()",
		"public static def _Foo()",
	} {
		source := "vim9script\nclass A\n  " + declaration + "\n  enddef\nendclass\nvar after = 1\n"
		file := Parse(source)
		if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1388" || file.Diagnostics[0].Message != "public keyword not supported for a method" || file.Text(file.Diagnostics[0].Span) != "public" {
			t.Fatalf("declaration=%q diagnostics=%#v", declaration, file.Diagnostics)
		}
		if len(file.Commands) != 6 || file.Commands[5].Declaration == nil {
			t.Fatalf("declaration=%q commands=%#v", declaration, file.Commands)
		}
		assertFileSpans(t, file)
	}
}

func TestVim9InvalidClassBodyCommandReportsE1318(t *testing.T) {
	for _, test := range []struct{ name, command string }{
		{"this member type", "this.count: number"},
		{"this member assignment", "this.count = 42"},
		{"other member", "that.count"},
		{"variable command", "variable count: number"},
		{"unknown command", "aaa"},
		{"missing def name", "def"},
		{"missing static def name", "static def"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "vim9script\nclass Shape\n  " + test.command + "\nendclass\nvar after = 1\n"
			file := Parse(source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1318" {
					got = append(got, diagnostic)
				}
				switch diagnostic.Code {
				case "vimls/trailing-characters", "vim/E121", "vim/E488", "vim/E1001", "vimls/missing-delimiter":
					t.Fatalf("class body %q retained cascade %s: %#v", test.command, diagnostic.Code, file.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "Not a valid command in a class: "+test.command || file.Text(got[0].Span) != test.command {
				t.Fatalf("class body %q diagnostics = %#v", test.command, file.Diagnostics)
			}
			if len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockClass || file.Blocks[0].End < 0 || len(file.Commands) != 5 || file.Commands[4].Declaration == nil {
				t.Fatalf("class body %q recovery = %#v, blocks = %#v", test.command, file.Commands, file.Blocks)
			}
			assertFileSpans(t, file)
		})
	}

	abstract := Parse("vim9script\nabstract class Shape\n  abstract def Draw(): string\n    return 'X'\n  enddef\nendclass\nvar after = 1\n")
	var abstractGot []Diagnostic
	for _, diagnostic := range abstract.Diagnostics {
		if diagnostic.Code == "vim/E1318" {
			abstractGot = append(abstractGot, diagnostic)
		}
	}
	if len(abstractGot) != 1 || abstractGot[0].Message != "Not a valid command in a class: return 'X'" || abstract.Text(abstractGot[0].Span) != "return 'X'" || len(abstract.Blocks) != 1 || abstract.Blocks[0].End < 0 || abstract.Commands[len(abstract.Commands)-1].Declaration == nil {
		t.Fatalf("abstract class recovery = %#v, blocks = %#v", abstract.Commands, abstract.Blocks)
	}

	for _, source := range []string{
		"vim9script\nclass Shape\n  var count: number\n  const label = 'shape'\n  final size = 1\n  def Draw()\n  enddef\nendclass\n",
		"vim9script\nclass Shape\n  def Draw()\n    aaa\n  enddef\nendclass\n",
		"vim9script\ninterface Face\n  aaa\nendinterface\nenum Kind\n  aaa\nendenum\n",
	} {
		file := Parse(source)
		if hasDiagnostic(file, "vim/E1318") {
			t.Fatalf("valid/non-class body diagnostics = %#v", file.Diagnostics)
		}
	}
}

func TestVim9UnmatchedScopeEndRecovers(t *testing.T) {
	file := (Vim9Parser{}).Parse("}\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1025" || len(file.Commands) != 2 || file.Commands[1].Declaration == nil {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
}

func TestAugroupBangDoesNotOpenOrCloseBlock(t *testing.T) {
	file := (LegacyParser{}).Parse("augroup! Removed\naugroup Kept\naugroup! Other\naugroup END\n")
	if len(file.Diagnostics) != 0 || len(file.Blocks) != 0 {
		t.Fatalf("blocks = %#v, diagnostics = %#v", file.Blocks, file.Diagnostics)
	}
}

func TestAugroupEndIsCaseInsensitive(t *testing.T) {
	file := (LegacyParser{}).Parse("augroup lower\n  autocmd!\naugroup end\n")
	if len(file.Diagnostics) != 0 || len(file.Blocks) != 0 {
		t.Fatalf("file = %#v", file)
	}
}

func TestAugroupEndDoesNotCloseTryBlock(t *testing.T) {
	file := (LegacyParser{}).Parse("augroup Test\ntry\n  echo 1\naugroup END\nfinally\n  echo 2\nendtry\n")
	if len(file.Diagnostics) != 0 || len(file.Blocks) != 1 {
		t.Fatalf("blocks = %#v, diagnostics = %#v", file.Blocks, file.Diagnostics)
	}
	if file.Blocks[0].Kind != BlockTry || file.Blocks[0].Header != 1 || file.Blocks[0].End != 6 {
		t.Fatalf("try block = %#v", file.Blocks[0])
	}
	if file.Commands[3].Block != 0 || file.Commands[4].Block != 0 {
		t.Fatalf("commands around augroup END = %#v, %#v", file.Commands[3], file.Commands[4])
	}
}

func TestVim9LegacyFlowControlDiagnostic(t *testing.T) {
	for _, test := range []struct{ command string }{
		{"if true"}, {"elseif true"}, {"else"}, {"endif"},
		{"for item in []"}, {"endfor"}, {"continue"}, {"break"},
		{"while true"}, {"endwhile"}, {"try"}, {"catch /error/"}, {"finally"}, {"endtry"},
	} {
		t.Run(test.command, func(t *testing.T) {
			source := "vim9script\ndef Func()\n  legacy " + test.command + "\nenddef\nvar after = 1\n"
			file := Parse(source)
			var got []Diagnostic
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Code == "vim/E1189" {
					got = append(got, diagnostic)
				}
				switch diagnostic.Code {
				case "vimls/missing-end", "vimls/unexpected-branch", "vimls/unexpected-end", "vim/E580", "vim/E581", "vim/E582", "vim/E586", "vim/E587", "vim/E588", "vim/E602", "vim/E603", "vim/E606":
					t.Fatalf("legacy %s retained structural cascade %s: %#v", test.command, diagnostic.Code, file.Diagnostics)
				}
			}
			if len(got) != 1 || got[0].Message != "Cannot use :legacy with this command: "+test.command || file.Text(got[0].Span) != test.command {
				t.Fatalf("legacy %s diagnostics = %#v", test.command, file.Diagnostics)
			}
			if file.Commands[len(file.Commands)-1].Declaration == nil || file.Text(file.Commands[len(file.Commands)-1].Declaration.Name) != "after" {
				t.Fatalf("legacy %s did not retain following declaration: %#v", test.command, file.Commands)
			}
			assertFileSpans(t, file)
		})
	}

	for _, test := range []struct {
		name, source string
		want         bool
	}{
		{"Legacy-root def", "def Func()\n  legacy if true\nenddef\n", true},
		{"nearest legacy function", "vim9script\ndef Outer()\n  function Inner()\n    legacy if true\n    endif\n  endfunction\nenddef\n", false},
		{"top-level Vim9", "vim9script\nlegacy if true\nendif\n", false},
		{"allowed legacy call", "vim9script\ndef Func()\n  legacy call DoThing()\nenddef\n", false},
		{"final vim9cmd", "vim9script\ndef Func()\n  legacy vim9cmd if true\n  endif\nenddef\n", false},
		{"final legacy", "vim9script\ndef Func()\n  vim9cmd legacy if true\nenddef\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			got := hasDiagnostic(file, "vim/E1189")
			if got != test.want {
				t.Fatalf("E1189=%v, want %v: %#v", got, test.want, file.Diagnostics)
			}
		})
	}

	recovered := Parse("vim9script\ndef Func()\n  if true\n    legacy endif\nenddef\nvar after = 1\n")
	for _, diagnostic := range recovered.Diagnostics {
		if diagnostic.Code != "vim/E1189" {
			t.Fatalf("invalid legacy closer retained cascade %s: %#v", diagnostic.Code, recovered.Diagnostics)
		}
	}
	if len(recovered.Diagnostics) != 1 || recovered.Commands[len(recovered.Commands)-1].Declaration == nil || recovered.Text(recovered.Commands[len(recovered.Commands)-1].Declaration.Name) != "after" {
		t.Fatalf("invalid legacy closer recovery = %#v, commands = %#v", recovered.Diagnostics, recovered.Commands)
	}
	assertFileSpans(t, recovered)
}
