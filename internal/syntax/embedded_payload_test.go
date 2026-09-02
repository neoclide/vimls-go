package syntax

import "testing"

func TestEmbeddedDoCommandList(t *testing.T) {
	for _, name := range []string{"argdo", "bufdo", "cdo", "cfdo", "ldo", "lfdo", "tabdo", "windo", "folddoopen", "folddoclosed"} {
		source := name + " edit file.txt\n"
		file := (LegacyParser{}).Parse(source)
		if len(file.Diagnostics) != 0 || len(file.Commands) != 1 {
			t.Fatalf("%s commands = %#v, diagnostics = %#v", name, file.Commands, file.Diagnostics)
		}
		outer := &file.Commands[0]
		if outer.Embedded == nil || len(outer.Embedded.Commands) != 1 || outer.Embedded.Commands[0].Canonical != "edit" {
			t.Fatalf("%s embedded = %#v", name, outer.Embedded)
		}
		inner := outer.Embedded.Commands[0]
		if file.Text(inner.Span) != "edit file.txt" || inner.Span.Start != outer.Argument.Start || inner.Span.End != outer.Argument.End {
			t.Fatalf("%s spans = outer %#v, inner %#v", name, outer, inner)
		}
	}
}

func TestFoldDoEmbeddedCommandOwnership(t *testing.T) {
	source := "1,3foldd echo one | let g:value = 1\n" +
		"%folddoclose delete\n" +
		"folddoopen let g:x = 1 \" comment | let g:y = 2\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	open := &file.Commands[0]
	if open.Canonical != "folddoopen" || open.TypedName != "foldd" || file.Text(open.Range) != "1,3" || open.Embedded == nil || len(open.Embedded.Commands) != 2 {
		t.Fatalf("open = %#v", open)
	}
	if open.Embedded.Commands[0].Canonical != "echo" || open.Embedded.Commands[1].Declaration == nil || file.Text(open.Embedded.Commands[1].Declaration.Name) != "g:value" {
		t.Fatalf("open embedded = %#v", open.Embedded.Commands)
	}
	closed := &file.Commands[1]
	if closed.Canonical != "folddoclosed" || closed.TypedName != "folddoclose" || file.Text(closed.Range) != "%" || closed.Embedded == nil || len(closed.Embedded.Commands) != 1 || closed.Embedded.Commands[0].Canonical != "delete" {
		t.Fatalf("closed = %#v", closed)
	}
	commented := &file.Commands[2]
	if commented.Embedded == nil || len(commented.Embedded.Commands) != 1 || commented.Embedded.Commands[0].Declaration == nil || file.Text(commented.Embedded.Commands[0].Declaration.Name) != "g:x" {
		t.Fatalf("commented = %#v", commented.Embedded)
	}
	assertFileSpans(t, file)
}

func TestFoldDoEmbeddedVim9CommentNestedGlobalAndRecovery(t *testing.T) {
	source := "vim9script\n" +
		"folddoclosed var x = 1 # comment | var hidden = 2\n" +
		"folddoopen g/foo/echo matched | echo after\n" +
		"folddoopen | var after_bar = 1\n" +
		"folddoopen\n" +
		"var after = 1\n"
	file := Parse(source)
	if len(file.Commands) != 6 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	commented := file.Commands[1].Embedded
	if commented == nil || len(commented.Commands) != 1 || commented.Commands[0].Declaration == nil || file.Text(commented.Commands[0].Declaration.Name) != "x" {
		t.Fatalf("commented = %#v", commented)
	}
	nested := file.Commands[2].Embedded
	if nested == nil || len(nested.Commands) != 1 || nested.Commands[0].Canonical != "global" || nested.Commands[0].Embedded == nil || len(nested.Commands[0].Embedded.Commands) != 2 {
		t.Fatalf("nested = %#v", nested)
	}
	afterBar := file.Commands[3].Embedded
	if afterBar == nil || len(afterBar.Commands) != 2 || afterBar.Commands[0].Kind != CommandEmpty || afterBar.Commands[1].Declaration == nil || file.Text(afterBar.Commands[1].Declaration.Name) != "after_bar" {
		t.Fatalf("after bar = %#v", afterBar)
	}
	missing := &file.Commands[4]
	if missing.Embedded == nil || len(missing.Embedded.Commands) != 0 || !hasDiagnostic(file, "vim/E471") {
		t.Fatalf("missing = %#v, diagnostics = %#v", missing, file.Diagnostics)
	}
	if file.Commands[5].Declaration == nil || file.Text(file.Commands[5].Declaration.Name) != "after" {
		t.Fatalf("following = %#v", file.Commands[5])
	}
	assertFileSpans(t, file)
}

func TestFoldDoEmbeddedUsesEnclosingDialectAfterLocalModifier(t *testing.T) {
	legacy := (LegacyParser{}).Parse("vim9cmd folddoopen let g:old = 1\n")
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 1 || legacy.Commands[0].Dialect != Vim9 || legacy.Commands[0].Embedded == nil || len(legacy.Commands[0].Embedded.Commands) != 1 {
		t.Fatalf("legacy = %#v, diagnostics = %#v", legacy.Commands, legacy.Diagnostics)
	}
	legacyNested := &legacy.Commands[0].Embedded.Commands[0]
	if legacyNested.Dialect != Legacy || legacyNested.Declaration == nil || legacy.Text(legacyNested.Declaration.Name) != "g:old" {
		t.Fatalf("legacy nested = %#v", legacyNested)
	}

	vim9 := (Vim9Parser{}).Parse("vim9script\nlegacy folddoclosed var fresh = 1\n")
	if len(vim9.Diagnostics) != 0 || len(vim9.Commands) != 2 || vim9.Commands[1].Dialect != Legacy || vim9.Commands[1].Embedded == nil || len(vim9.Commands[1].Embedded.Commands) != 1 {
		t.Fatalf("Vim9 = %#v, diagnostics = %#v", vim9.Commands, vim9.Diagnostics)
	}
	vim9Nested := &vim9.Commands[1].Embedded.Commands[0]
	if vim9Nested.Dialect != Vim9 || vim9Nested.Declaration == nil || vim9.Text(vim9Nested.Declaration.Name) != "fresh" {
		t.Fatalf("Vim9 nested = %#v", vim9Nested)
	}
	assertFileSpans(t, legacy)
	assertFileSpans(t, vim9)
}

func TestEmbeddedDoCommandBlockAndAbsoluteSpans(t *testing.T) {
	source := "windo if cond | echo value | endif\n"
	file := (Vim9Parser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || len(file.Blocks) != 0 {
		t.Fatalf("file = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	outer := &file.Commands[0]
	if outer.Embedded == nil || len(outer.Embedded.Commands) != 3 || len(outer.Embedded.Blocks) != 1 {
		t.Fatalf("embedded = %#v", outer.Embedded)
	}
	if outer.Embedded.Blocks[0].Kind != BlockIf || outer.Embedded.Blocks[0].Header != 0 || outer.Embedded.Blocks[0].End != 2 {
		t.Fatalf("embedded blocks = %#v", outer.Embedded.Blocks)
	}
	for _, command := range outer.Embedded.Commands {
		if command.Span.Start < outer.Argument.Start || command.Span.End > outer.Argument.End || file.Text(command.Span) == "" {
			t.Fatalf("embedded command span = %#v", command)
		}
	}
	if file.Text(outer.Embedded.Commands[1].Argument) != "value" {
		t.Fatalf("echo argument = %q", file.Text(outer.Embedded.Commands[1].Argument))
	}
}

func TestEmbeddedDoCommandRecoveryAndDepthLimit(t *testing.T) {
	incomplete := (Vim9Parser{}).Parse("windo if cond | echo value\n")
	if incomplete.Commands[0].Embedded == nil || len(incomplete.Commands[0].Embedded.Commands) != 2 || len(incomplete.Commands[0].Embedded.Blocks) != 1 || !hasDiagnostic(incomplete, "vim/E171") {
		t.Fatalf("incomplete = %#v, diagnostics = %#v", incomplete.Commands[0].Embedded, incomplete.Diagnostics)
	}

	source := "windo "
	for index := 0; index <= maxEmbeddedCommandDepth; index++ {
		source += "windo "
	}
	source += "edit file.txt\n"
	file := (LegacyParser{}).Parse(source)
	if !hasDiagnostic(file, "vimls/embedded-command-depth") {
		t.Fatalf("depth diagnostics = %#v", file.Diagnostics)
	}
	if file.Commands[0].Embedded == nil || file.Commands[0].Embedded.Span.Start != len("windo ") {
		t.Fatalf("depth recovery = %#v", file.Commands[0].Embedded)
	}
}

func TestEmbeddedDoCommandDoesNotDuplicateTopLevelCommands(t *testing.T) {
	file := (LegacyParser{}).Parse("windo echo value | endif\nedit other.txt\n")
	if len(file.Commands) != 2 || file.Commands[0].Canonical != "windo" || file.Commands[1].Canonical != "edit" {
		t.Fatalf("top-level commands = %#v", file.Commands)
	}
	if file.Commands[0].Embedded == nil || len(file.Commands[0].Embedded.Commands) != 2 {
		t.Fatalf("embedded commands = %#v", file.Commands[0].Embedded)
	}
}
