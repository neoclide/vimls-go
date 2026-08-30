package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestCompileExpectedCode(t *testing.T) {
	tests := []struct {
		name   string
		source string
		helper string
		want   string
	}{
		{name: "direct single quote", source: "'E1012: Type mismatch'", helper: "CheckDefFailure", want: "vim/E1012"},
		{name: "direct double quote", source: `"E1001: Variable not found"`, helper: "CheckSourceDefFailure", want: "vim/E1001"},
		{name: "shared pair", source: "['E1013: def', 'E121: script']", helper: "CheckDefAndScriptFailure", want: "vim/E1013"},
		{name: "same code", source: "'E1097:'", helper: "CheckSourceDefAndScriptFailure", want: "vim/E1097"},
		{name: "direct helper rejects list", source: "['E1012:', 'E121:']", helper: "CheckDefFailure"},
		{name: "dynamic identifier", source: "msg", helper: "CheckDefAndScriptFailure"},
		{name: "message without code", source: "'expected number but got string'", helper: "CheckDefFailure"},
		{name: "ambiguous pattern", source: "'E1012:.*E1191:'", helper: "CheckDefFailure"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := []byte(test.source)
			got := compileExpectedCode(source, helperArgument{Start: 0, End: len(source)}, test.helper)
			if got != test.want {
				t.Fatalf("compileExpectedCode(%q) = %q, want %q", test.source, got, test.want)
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
		ExpectedCodes: 1662, UnresolvedCode: 100,
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
		if record.Disposition == "extracted" && !strings.HasSuffix(record.Source, "defcompile\n") {
			t.Fatalf("%s does not end in defcompile: %q", record.ID, record.Source)
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
