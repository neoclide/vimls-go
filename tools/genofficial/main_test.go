package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestHelperScannerPreservesLexicalCases(t *testing.T) {
	known := map[string]struct{}{"CheckDefSuccess": {}, "Check": {}}
	cases := []struct {
		name   string
		source string
		kind   string
		arg    string
	}{
		{"multiline", "v9.CheckDefSuccess(\n  lines\n)\n", "call", "identifier"},
		{"ascii-whitespace", "v9.CheckDefSuccess\v(1)\n", "call", "expression"},
		{"nested-list", "v9.CheckDefSuccess(['a', [\"b)\"]])\n", "call", "list"},
		{"single-quote-escape", "v9.CheckDefSuccess(['it''s'])\n", "call", "list"},
		{"double-quote-escape", "v9.CheckDefSuccess(\"a\\\"b\")\n", "call", "expression"},
		{"vim9-comment", "# v9.CheckDefSuccess(lines)\n", "comment", ""},
		{"legacy-comment", "  \" CheckDefSuccess(lines)\n", "comment", ""},
		{"definition", "export def Check(lines)\nenddef\n", "definition", ""},
		{"legacy-function-definition", "function Check(lines)\nendfunction\n", "definition", ""},
		{"legacy-function-bang-definition", "function! Check(lines)\nendfunction\n", "definition", ""},
		{"utility-call", "CheckDefSuccess(lines)\n", "call", "identifier"},
		{"non-v9-call", "CheckLocal(lines)\n", "call", ""},
		{"numeric-argument", "v9.CheckDefSuccess(1)\n", "call", "expression"},
		{"boundary", "NotCheckLocal(lines)\n", "", ""},
		{"unmatched", "v9.CheckDefSuccess([lines\n", "call", "expression"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			records := scanHelperFile("src/testdir/util/vim9.vim", []byte(testCase.source), known)
			if testCase.kind == "" {
				if len(records) != 0 {
					t.Fatalf("records = %#v", records)
				}
				return
			}
			if len(records) != 1 {
				t.Fatalf("records = %#v", records)
			}
			if records[0].Kind != testCase.kind || records[0].FirstArgument != testCase.arg {
				t.Fatalf("record = %#v", records[0])
			}
			if testCase.name == "unmatched" {
				if records[0].CallComplete || records[0].CallEnd != len(testCase.source) || records[0].Disposition != "out-of-scope" || records[0].Reason != "incomplete Check helper call" {
					t.Fatalf("incomplete contract = %#v", records[0])
				}
			}
		})
	}
}

func TestPinnedHelperInventoryArtifact(t *testing.T) {
	wantHelpers := []string{
		"CheckDefAndScriptFailure", "CheckDefAndScriptSuccess", "CheckDefCompileSuccess", "CheckDefExecAndScriptFailure", "CheckDefExecFailure", "CheckDefFailure", "CheckDefSuccess",
		"CheckLegacyAndVim9Failure", "CheckLegacyAndVim9Success", "CheckLegacyFailure", "CheckLegacySuccess", "CheckScriptFailure", "CheckScriptFailureList", "CheckScriptSuccess",
		"CheckSourceDefAndScriptFailure", "CheckSourceDefAndScriptSuccess", "CheckSourceDefCompileSuccess", "CheckSourceDefExecAndScriptFailure", "CheckSourceDefExecFailure", "CheckSourceDefFailure", "CheckSourceDefSuccess",
		"CheckSourceFailure", "CheckSourceFailureList", "CheckSourceLegacyAndVim9Failure", "CheckSourceLegacyAndVim9Success", "CheckSourceLegacyFailure", "CheckSourceLegacySuccess", "CheckSourceScriptFailure", "CheckSourceScriptFailureList", "CheckSourceScriptSuccess", "CheckSourceSuccess",
		"CheckSourceTransDefSuccess", "CheckSourceTransLegacySuccess", "CheckSourceTransVim9Success", "CheckTransDefSuccess", "CheckTransLegacySuccess", "CheckTransVim9Success",
	}
	path := filepath.Join("..", "..", "testdata", "official", "v9.2.1015-helper-inventory.json.gz")
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	var inventory helperInventory
	if err := json.NewDecoder(reader).Decode(&inventory); err != nil {
		t.Fatal(err)
	}
	if inventory.SchemaVersion != 1 || inventory.Tag != vimTag || inventory.Commit != vimCommit {
		t.Fatalf("provenance = %#v", inventory)
	}
	if !reflect.DeepEqual(inventory.HelperNames, wantHelpers) {
		t.Fatalf("helper names = %#v, want %#v", inventory.HelperNames, wantHelpers)
	}
	if len(inventory.Records) != expectedHelperLexemes {
		t.Fatalf("records = %d", len(inventory.Records))
	}
	if inventory.Summary.KnownHelperCalls != 5241 || inventory.Summary.QualifiedCalls != 5208 || inventory.Summary.UtilityBareCalls != 33 ||
		inventory.Summary.KnownDefinitions != 37 || inventory.Summary.KnownComments != 10 || inventory.Summary.NonV9Calls != 341 ||
		inventory.Summary.NonV9Definitions != 90 || inventory.Summary.NonV9Strings != 13 || inventory.Summary.NonV9Comments != 1 ||
		inventory.Summary.IdentifierArguments != 3174 || inventory.Summary.ListArguments != 2001 || inventory.Summary.ExpressionArguments != 66 ||
		inventory.Summary.QualifiedIdentifier != 3156 || inventory.Summary.QualifiedList != 2001 || inventory.Summary.QualifiedExpression != 51 ||
		inventory.Summary.BareIdentifier != 18 || inventory.Summary.BareExpression != 15 {
		t.Fatalf("summary = %#v", inventory.Summary)
	}
	fullPath := filepath.Join("..", "..", "testdata", "official", "v9.2.1015-test-files.json.gz")
	fullFile, err := os.Open(fullPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fullFile.Close()
	fullReader, err := gzip.NewReader(fullFile)
	if err != nil {
		t.Fatal(err)
	}
	var full testFilesCorpus
	if err := json.NewDecoder(fullReader).Decode(&full); err != nil {
		t.Fatal(err)
	}
	if full.Tag != vimTag || full.Commit != vimCommit || len(full.Files) != expectedTestFileCount {
		t.Fatalf("full corpus provenance = %#v", full)
	}
	sources := make(map[string][]byte, len(full.Files))
	for _, file := range full.Files {
		if _, exists := sources[file.Path]; exists {
			t.Fatalf("duplicate full corpus path %q", file.Path)
		}
		sources[file.Path] = file.Source
	}
	known := make(map[string]struct{}, len(inventory.HelperNames))
	for _, name := range inventory.HelperNames {
		known[name] = struct{}{}
	}
	var rescanned []helperRecord
	type coordinate struct {
		path   string
		offset int
	}
	rawPattern := regexp.MustCompile(`\bCheck[A-Za-z0-9_]*[ \t\r\n\f\x0b]*\(`)
	var rawCoordinates []coordinate
	for _, file := range full.Files {
		rescanned = append(rescanned, scanHelperFile(file.Path, file.Source, known)...)
		for _, match := range rawPattern.FindAllIndex(file.Source, -1) {
			rawCoordinates = append(rawCoordinates, coordinate{path: file.Path, offset: match[0]})
		}
	}
	if !reflect.DeepEqual(inventory.Records, rescanned) {
		t.Fatal("inventory records differ from an independent rescan")
	}
	actualCoordinates := make([]coordinate, len(inventory.Records))
	for index, record := range inventory.Records {
		actualCoordinates[index] = coordinate{path: record.Path, offset: record.Offset}
	}
	if !reflect.DeepEqual(actualCoordinates, rawCoordinates) {
		t.Fatal("inventory does not contain every raw Check helper candidate exactly once")
	}
	var recomputed helperInventorySummary
	for i, record := range inventory.Records {
		if i > 0 && (inventory.Records[i-1].Path > record.Path || (inventory.Records[i-1].Path == record.Path && inventory.Records[i-1].Offset >= record.Offset)) {
			t.Fatalf("records not sorted at %d", i)
		}
		source, ok := sources[record.Path]
		if !ok || record.LexemeStart != record.Offset || record.LexemeEnd <= record.LexemeStart || record.LexemeEnd > len(source) || string(source[record.LexemeStart:record.LexemeEnd]) != record.Lexeme || record.Name != record.Lexeme || record.Offset < 0 || record.CallStart < 0 || record.CallEnd < record.CallStart || record.CallEnd > len(source) || record.CallEnd == record.CallStart {
			t.Fatalf("invalid record %d: %#v", i, record)
		}
		if !record.CallComplete && record.CallEnd != len(source) {
			t.Fatalf("incomplete span %d: %#v", i, record)
		}
		if record.CallComplete && source[record.CallEnd-1] != ')' {
			t.Fatalf("complete call does not end at a closing parenthesis %d: %#v", i, record)
		}
		if record.Line != 1+strings.Count(string(source[:record.Offset]), "\n") {
			t.Fatalf("line mismatch %d: %#v", i, record)
		}
		if record.Qualification != "bare" && record.Qualification != "qualified" {
			t.Fatalf("qualification %q", record.Qualification)
		}
		if record.Kind != "call" && record.Kind != "definition" && record.Kind != "comment" && record.Kind != "string" {
			t.Fatalf("kind %q", record.Kind)
		}
		if record.Disposition != "pending-extraction" && record.Disposition != "out-of-scope" {
			t.Fatalf("disposition %q", record.Disposition)
		}
		validReasons := map[string]bool{"qualified Vim9 test helper call": true, "helper implementation call in util/vim9.vim": true, "known helper definition or comment": true, "non-Vim9 Check helper call": true, "non-Vim9 Check helper definition": true, "non-Vim9 Check text in string": true, "non-Vim9 Check text in comment": true, "incomplete Check helper call": true}
		if record.Reason == "" || !validReasons[record.Reason] {
			t.Fatalf("empty reason %d", i)
		}
		if record.Disposition == "pending-extraction" && record.FirstArgument != "identifier" && record.FirstArgument != "list" && record.FirstArgument != "expression" {
			t.Fatalf("argument kind %q", record.FirstArgument)
		}
		if err := addHelperSummary(&recomputed, record); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(inventory.Summary, recomputed) {
		t.Fatalf("summary does not match records: %#v != %#v", inventory.Summary, recomputed)
	}
}

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
