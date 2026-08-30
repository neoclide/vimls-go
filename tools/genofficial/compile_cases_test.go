package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestCompileExpectedCodes(t *testing.T) {
	tests := []struct {
		name   string
		source string
		helper string
		cases  int
		want   []string
	}{
		{name: "direct single quote", source: "'E1012: Type mismatch'", helper: "CheckDefFailure", cases: 1, want: []string{"vim/E1012"}},
		{name: "direct double quote", source: `"E1001: Variable not found"`, helper: "CheckSourceDefFailure", cases: 1, want: []string{"vim/E1001"}},
		{name: "separate lane codes", source: "['E1013: def', 'E121: script']", helper: "CheckDefAndScriptFailure", cases: 2, want: []string{"vim/E1013", "vim/E121"}},
		{name: "shared lane code", source: "'E1097:'", helper: "CheckSourceDefAndScriptFailure", cases: 2, want: []string{"vim/E1097", "vim/E1097"}},
		{name: "direct helper rejects list", source: "['E1012:', 'E121:']", helper: "CheckDefFailure", cases: 1},
		{name: "dynamic identifier", source: "msg", helper: "CheckDefAndScriptFailure", cases: 2},
		{name: "message without code", source: "'expected number but got string'", helper: "CheckDefFailure", cases: 1},
		{name: "ambiguous pattern", source: "'E1012:.*E1191:'", helper: "CheckDefFailure", cases: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := []byte(test.source)
			got := compileExpectedCodes(source, helperArgument{Start: 0, End: len(source)}, test.helper, test.cases)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("compileExpectedCodes(%q) = %q, want %q", test.source, got, test.want)
			}
		})
	}
}

func TestBuildPinnedCompileCaseCorpus(t *testing.T) {
	files := readPinnedTestFiles(t)
	inventory := readPinnedHelperInventory(t)
	corpus, err := buildCompileCaseCorpus(files, inventory)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkCompileCaseCorpus(corpus); err != nil {
		t.Fatal(err)
	}
	wantSummary := compileCaseSummary{
		Calls: 1770, ExtractedCalls: 1762, SkippedCalls: 8,
		Cases: 3171, ExpectedCodes: 2964, UnresolvedCode: 207,
		DirectLists: 1403, Heredocs: 349, ListConcats: 10,
	}
	if corpus.Summary != wantSummary || len(corpus.Files) != 14 {
		t.Fatalf("compile corpus files=%d summary=%+v, want files=14 summary=%+v", len(corpus.Files), corpus.Summary, wantSummary)
	}

	wantCalls := 0
	for _, helper := range inventory.Records {
		if helper.Disposition == "pending-extraction" && isCompileFailureHelper(helper.Name) {
			wantCalls++
		}
	}
	if len(corpus.Records) != wantCalls {
		t.Fatalf("compile records=%d, selected helper calls=%d", len(corpus.Records), wantCalls)
	}
	for _, record := range corpus.Records {
		for _, variant := range record.Cases {
			if variant.Context == "def" && !strings.HasSuffix(variant.Source, "defcompile\n") {
				t.Fatalf("%s/%s does not end in defcompile: %q", record.ID, variant.Name, variant.Source)
			}
			if variant.Context == "script" && !strings.HasPrefix(variant.Source, "vim9script\n") {
				t.Fatalf("%s/%s does not begin in vim9script: %q", record.ID, variant.Name, variant.Source)
			}
		}
	}
}

func TestPinnedCompileCaseArtifact(t *testing.T) {
	var artifact compileCaseCorpus
	readPinnedGzipJSON(t, "v9.2.1015-compile-cases.json.gz", &artifact)
	want, err := buildCompileCaseCorpus(readPinnedTestFiles(t), readPinnedHelperInventory(t))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(artifact, want) {
		t.Fatal("generated compile-case artifact is stale; run make generate-official")
	}
}
