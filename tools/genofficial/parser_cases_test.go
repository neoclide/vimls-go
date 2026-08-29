package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExpandParserHelperVariants(t *testing.T) {
	cases, ok := expandParserHelper("CheckLegacyAndVim9Success", []string{"VAR n = TRUE", "LET n = FALSE", "echo LSTART x LMIDDLE n LEND"})
	if !ok || len(cases) != 3 {
		t.Fatalf("cases = %#v, %v", cases, ok)
	}
	want := []parserCaseVariant{
		{Name: "legacy", Context: "legacy-function", VimOutcome: "success", Expectation: "accept", Source: "func Func()\nlet n = 1\nlet n = 0\necho { x -> n }\nendfunc\n"},
		{Name: "def", Context: "def", VimOutcome: "success", Expectation: "accept", Source: "def Func()\nvar n = true\nn = false\necho ( x ) => n\nenddef\ndefcompile\n"},
		{Name: "vim9-script", Context: "script", VimOutcome: "success", Expectation: "accept", Source: "vim9script\nvar n = true\nn = false\necho ( x ) => n\n"},
	}
	if !reflect.DeepEqual(cases, want) {
		t.Fatalf("cases = %#v, want %#v", cases, want)
	}

	failure, ok := expandParserHelper("CheckDefExecAndScriptFailure", []string{"var value = missing"})
	if !ok || len(failure) != 2 || failure[0].Expectation != "unclassified" || failure[1].Expectation != "unclassified" {
		t.Fatalf("failure cases = %#v, %v", failure, ok)
	}
}

func TestResolveParserHelperSourceUsesSameScopeAndLatestAssignment(t *testing.T) {
	source := []byte("func First()\n" +
		"  let lines =<< END\nold\nEND\n" +
		"endfunc\n" +
		"func Second()\n" +
		"  let lines =<< END\nnew\nEND\n" +
		"  v9.CheckScriptSuccess(lines)\n" +
		"  lines += ['changed']\n" +
		"  v9.CheckScriptSuccess(lines)\n" +
		"endfunc\n")
	index := buildHelperSourceIndex(source)
	firstCall := strings.Index(string(source), "v9.CheckScriptSuccess(lines)")
	argumentStart := firstCall + len("v9.CheckScriptSuccess(")
	argument := helperArgument{Start: argumentStart, End: argumentStart + len("lines")}
	lines, binding, reason := resolveParserHelperSource(index, helperRecord{CallStart: firstCall}, argument)
	if reason != "" || binding.Kind != "heredoc" || !reflect.DeepEqual(lines, []string{"new"}) {
		t.Fatalf("first binding = %#v, lines=%#v, reason=%q", binding, lines, reason)
	}
	secondCall := strings.LastIndex(string(source), "v9.CheckScriptSuccess(lines)")
	argumentStart = secondCall + len("v9.CheckScriptSuccess(")
	argument = helperArgument{Start: argumentStart, End: argumentStart + len("lines")}
	_, binding, reason = resolveParserHelperSource(index, helperRecord{CallStart: secondCall}, argument)
	if binding.Kind != "dynamic-assignment" || reason != "identifier resolves to a dynamic or mutated assignment" {
		t.Fatalf("mutated binding = %#v, reason=%q", binding, reason)
	}
}

func TestHelperAssignmentOperatorIgnoresStringsAndComments(t *testing.T) {
	for _, source := range []string{" 'x=y'", ` "x=y"`, " # x=y"} {
		if _, operator := helperAssignmentOperator([]byte(source), 0); operator != "" {
			t.Fatalf("helperAssignmentOperator(%q) = %q, want no assignment", source, operator)
		}
	}
	if offset, operator := helperAssignmentOperator([]byte(": list<string> = ['ok']"), 0); operator != "=" || offset != 15 {
		t.Fatalf("typed assignment = %d, %q", offset, operator)
	}
}

func TestBuildPinnedParserCaseCorpus(t *testing.T) {
	files := readPinnedTestFiles(t)
	inventory := readPinnedHelperInventory(t)
	manifest, err := readParserFileManifest(filepath.Join("..", "..", "testdata", "official", "v9.2.1015-parser-files.json"))
	if err != nil {
		t.Fatal(err)
	}
	corpus, err := buildParserCaseCorpus(files, inventory, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePinnedParserCaseCorpus(corpus); err != nil {
		t.Fatal(err)
	}
	selected, err := selectParserMigrationFiles(files, manifest)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := make([]string, 0, len(selected))
	for _, file := range selected {
		wantFiles = append(wantFiles, file.Path)
	}
	if !reflect.DeepEqual(corpus.Files, wantFiles) {
		t.Fatalf("corpus files differ from parser allowlist\ngot:  %#v\nwant: %#v", corpus.Files, wantFiles)
	}
	included := includedParserFilePaths(manifest)
	wantCalls := make(map[string]helperRecord)
	for _, helper := range inventory.Records {
		if helper.Disposition != "pending-extraction" {
			continue
		}
		if _, ok := included[helper.Path]; !ok {
			continue
		}
		key := helper.Path + ":" + fmt.Sprint(helper.Offset)
		if _, duplicate := wantCalls[key]; duplicate {
			t.Fatalf("duplicate inventory coordinate %q", key)
		}
		wantCalls[key] = helper
	}
	seenIDs := make(map[string]bool, len(corpus.Records))
	var summary parserCaseSummary
	for index, record := range corpus.Records {
		if index > 0 && (corpus.Records[index-1].Path > record.Path || (corpus.Records[index-1].Path == record.Path && corpus.Records[index-1].Offset >= record.Offset)) {
			t.Fatalf("records are not strictly ordered at %d", index)
		}
		if _, ok := included[record.Path]; !ok {
			t.Fatalf("record %d uses non-allowlisted path %q", index, record.Path)
		}
		key := record.Path + ":" + fmt.Sprint(record.Offset)
		helper, ok := wantCalls[key]
		if !ok {
			t.Fatalf("record %d has no matching selected helper coordinate %q", index, key)
		}
		delete(wantCalls, key)
		if record.CallStart != helper.CallStart || record.CallEnd != helper.CallEnd || record.Helper != helper.Name {
			t.Fatalf("record %d lost helper call provenance: %#v, want %#v", index, record, helper)
		}
		if record.ID == "" || seenIDs[record.ID] {
			t.Fatalf("record has missing or duplicate id at %d: %q", index, record.ID)
		}
		seenIDs[record.ID] = true
		summary.Calls++
		switch record.Disposition {
		case "extracted":
			if record.Reason != "" || len(record.Cases) == 0 || record.InputKind == "" || record.InputEnd <= record.InputStart {
				t.Fatalf("invalid extracted record at %d: %#v", index, record)
			}
			summary.ExtractedCalls++
			switch record.InputKind {
			case "direct-list":
				summary.DirectLists++
			case "heredoc":
				summary.Heredocs++
			case "list-assignment":
				summary.ListAssignments++
			default:
				t.Fatalf("record %d has unknown input kind %q", index, record.InputKind)
			}
			for _, parserCase := range record.Cases {
				summary.Cases++
				switch parserCase.Expectation {
				case "accept":
					summary.AcceptedCases++
					if parserCase.VimOutcome != "success" {
						t.Fatalf("record %d accepts non-success case %#v", index, parserCase)
					}
				case "unclassified":
					summary.UnclassifiedCases++
					if parserCase.VimOutcome != "failure" {
						t.Fatalf("record %d leaves non-failure case unclassified %#v", index, parserCase)
					}
				default:
					t.Fatalf("record %d has unknown parser expectation %q", index, parserCase.Expectation)
				}
			}
		case "skipped":
			if record.Reason == "" || len(record.Cases) != 0 {
				t.Fatalf("invalid skipped record at %d: %#v", index, record)
			}
			summary.SkippedCalls++
		default:
			t.Fatalf("invalid disposition at %d: %#v", index, record)
		}
	}
	if len(wantCalls) != 0 {
		t.Fatalf("selected helper calls missing from parser corpus: %v", wantCalls)
	}
	if summary != corpus.Summary {
		t.Fatalf("independent summary = %+v, artifact summary = %+v", summary, corpus.Summary)
	}
	for _, helper := range inventory.HelperNames {
		if variants, ok := expandParserHelper(helper, []string{"echo 1"}); !ok || len(variants) == 0 {
			t.Fatalf("official helper %q has no source expansion", helper)
		}
	}
}

func TestPinnedParserCaseArtifact(t *testing.T) {
	var artifact parserCaseCorpus
	readPinnedGzipJSON(t, "v9.2.1015-parser-cases.json.gz", &artifact)
	files := readPinnedTestFiles(t)
	inventory := readPinnedHelperInventory(t)
	manifest, err := readParserFileManifest(filepath.Join("..", "..", "testdata", "official", "v9.2.1015-parser-files.json"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := buildParserCaseCorpus(files, inventory, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(artifact, want) {
		t.Fatal("generated parser-case artifact is stale; run make generate-official")
	}
}

func readPinnedTestFiles(t *testing.T) testFilesCorpus {
	t.Helper()
	var corpus testFilesCorpus
	readPinnedGzipJSON(t, "v9.2.1015-test-files.json.gz", &corpus)
	return corpus
}

func readPinnedHelperInventory(t *testing.T) helperInventory {
	t.Helper()
	var inventory helperInventory
	readPinnedGzipJSON(t, "v9.2.1015-helper-inventory.json.gz", &inventory)
	return inventory
}

func readPinnedGzipJSON(t *testing.T, name string, value any) {
	t.Helper()
	file, err := os.Open(filepath.Join("..", "..", "testdata", "official", name))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := json.NewDecoder(reader).Decode(value); err != nil {
		t.Fatal(err)
	}
}
