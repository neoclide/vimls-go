package syntax

import "testing"

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
	if codes["vimls/unexpected-bang"] != 1 || codes["vimls/unexpected-end"] != 1 {
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
	if codes["vimls/missing-end"] != 1 || codes["vimls/unexpected-end"] != 1 {
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

func TestVim9UnmatchedScopeEndRecovers(t *testing.T) {
	file := (Vim9Parser{}).Parse("}\nvar after = 1\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vimls/unexpected-end" || len(file.Commands) != 2 || file.Commands[1].Declaration == nil {
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
