package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

func TestSelectTestFilesUsesAllPinnedVim9Tests(t *testing.T) {
	files, err := selectTestFiles([]byte("src/testdir/test_vim9_script.vim\n" +
		"src/testdir/test_vim9_builtin.vim\n" +
		"src/testdir/test_vim9_class.vim\n" +
		"src/testdir/test_vim9_expr.vim\n" +
		"src/testdir/test_vim9_func.vim\n" +
		"src/testdir/test_vim9_generics.vim\n" +
		"src/testdir/test_vim9_import.vim\n" +
		"src/testdir/test_vim9_interface.vim\n" +
		"src/testdir/test_vim9_typealias.vim\n" +
		"src/testdir/test_vim9_python3.vim\n" +
		"src/testdir/test_vimscript.vim\n" +
		"src/testdir/test_tuple.vim\n" +
		"src/testdir/test_vim9_cmd.vim\n" +
		"src/testdir/test_vim9_assign.vim\n" +
		"src/testdir/test_vim9_disassemble.vim\n" +
		"src/testdir/test_vim9_enum.vim\n" +
		"src/testdir/test_vim9_fails.vim\n" +
		"src/testdir/other.vim\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"src/testdir/test_tuple.vim",
		"src/testdir/test_vim9_assign.vim",
		"src/testdir/test_vim9_builtin.vim",
		"src/testdir/test_vim9_class.vim",
		"src/testdir/test_vim9_cmd.vim",
		"src/testdir/test_vim9_disassemble.vim",
		"src/testdir/test_vim9_enum.vim",
		"src/testdir/test_vim9_expr.vim",
		"src/testdir/test_vim9_fails.vim",
		"src/testdir/test_vim9_func.vim",
		"src/testdir/test_vim9_generics.vim",
		"src/testdir/test_vim9_import.vim",
		"src/testdir/test_vim9_interface.vim",
		"src/testdir/test_vim9_python3.vim",
		"src/testdir/test_vim9_script.vim",
		"src/testdir/test_vim9_typealias.vim",
		"src/testdir/test_vimscript.vim",
	}
	if len(files) != len(want) {
		t.Fatalf("files = %#v, want %#v", files, want)
	}
	for index := range want {
		if files[index] != want[index] {
			t.Fatalf("file %d = %q, want %q", index, files[index], want[index])
		}
	}
}

func TestSelectAllTestFilesSortsAndDeduplicatesTrackedVimFiles(t *testing.T) {
	var manifest strings.Builder
	for index := range expectedTestFileCount {
		fmt.Fprintf(&manifest, "src/testdir/test_%03d.vim\n", index)
	}
	manifest.WriteString("src/testdir/test_000.vim\n")
	manifest.WriteString("src/testdir/not-vim.txt\n")
	files, err := selectAllTestFiles([]byte(manifest.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != expectedTestFileCount || !sort.StringsAreSorted(files) {
		t.Fatalf("files = %#v", files)
	}
	for index := 1; index < len(files); index++ {
		if files[index] == files[index-1] {
			t.Fatalf("duplicate file %q", files[index])
		}
	}
}

func TestOfficialOutcomeIgnoresMutatedHeredoc(t *testing.T) {
	lines := []string{
		"lines[2] = 'var l: list<any>'",
		"v9.CheckScriptSuccess(lines)",
	}
	if outcome := officialOutcome(lines, 0, "lines"); outcome != "" {
		t.Fatalf("outcome = %q", outcome)
	}
}

func TestOfficialOutcomeReadsDirectResult(t *testing.T) {
	if outcome := officialOutcome([]string{"v9.CheckDefAndScriptSuccess(lines)"}, 0, "lines"); outcome != "success" {
		t.Fatalf("outcome = %q", outcome)
	}
	if outcome := officialOutcome([]string{"v9.CheckSourceFailure(lines, 'E123')"}, 0, "lines"); outcome != "failure" {
		t.Fatalf("outcome = %q", outcome)
	}
}

func TestOfficialTemplateBodyIsNotDirectSource(t *testing.T) {
	for _, source := range []string{
		"VAR value = 1\n",
		"call map(items, LSTART _, value LMIDDLE value LEND)\n",
	} {
		if !containsTestTemplate(source) {
			t.Fatalf("template not detected: %q", source)
		}
	}
	if containsTestTemplate("var value = 1\n") {
		t.Fatal("ordinary Vim9 declaration detected as template")
	}
}
