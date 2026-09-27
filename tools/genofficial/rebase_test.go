package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRebaseParserAssertionsUniqueShiftWritesPreview(t *testing.T) {
	directory := t.TempDir()
	fromPath := writeRebaseCorpus(t, directory, "from.json.gz", rebaseCorpus("old", rebaseRecord("old:1:1", "source", "E1", "case")))
	toPath := writeRebaseCorpus(t, directory, "to.json.gz", rebaseCorpus("new", rebaseRecord("new:2:3", "source", "E1", "case")))
	assertionsPath := writeRebaseAssertions(t, directory, `// retained comment
func officialParserExpectedFailures() map[string]string {
	return map[string]string{
		"old:1:1/case": "vim/E1", // retained value and order
	}
}
`)
	outputPath := filepath.Join(directory, "preview.go")
	report, err := rebaseParserAssertions(fromPath, toPath, assertionsPath, outputPath, nil)
	if err != nil || report.Unique != 1 || report.Unresolved != 0 || len(report.Mappings) != 1 || report.Mappings[0].NewID != "new:2:3/case" {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"// retained comment", `"new:2:3/case": "vim/E1"`, "// retained value and order"} {
		if !strings.Contains(string(output), text) {
			t.Fatalf("preview lost %q:\n%s", text, output)
		}
	}
}

func TestRebaseCurrentCorpusIsByteIdentical(t *testing.T) {
	corpusPath := filepath.Join("..", "..", "testdata", "official", officialArtifactName("parser-cases"))
	assertionsPath := filepath.Join("..", "..", "internal", "syntax", "official_parser_cases_test.go")
	parsed, err := readParserAssertions(assertionsPath)
	if err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(t.TempDir(), "preview.go")
	report, err := rebaseParserAssertions(corpusPath, corpusPath, assertionsPath, outputPath, nil)
	if err != nil || report.Total != len(parsed.entries) || report.Unresolved != 0 {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != string(parsed.source) {
		t.Fatal("same-version rebase changed assertion bytes")
	}
}

func TestRebaseParserAssertionsOrdersMatchingDuplicateGroups(t *testing.T) {
	directory := t.TempDir()
	fromPath := writeRebaseCorpus(t, directory, "from.json.gz", rebaseCorpus("old", rebaseRecord("old:1:1", "source", "", "case"), rebaseRecord("old:2:2", "source", "", "case")))
	toPath := writeRebaseCorpus(t, directory, "to.json.gz", rebaseCorpus("new", rebaseRecord("new:5:5", "source", "", "case"), rebaseRecord("new:6:6", "source", "", "case")))
	assertionsPath := writeRebaseAssertions(t, directory, assertionsFor("old:1:1/case", "old:2:2/case"))
	report, err := rebaseParserAssertions(fromPath, toPath, assertionsPath, "", nil)
	if err != nil || report.Ordered != 2 || report.Mappings[0].NewID != "new:5:5/case" || report.Mappings[1].NewID != "new:6:6/case" {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
}

func TestRebaseParserAssertionsPreservesAssertionOrder(t *testing.T) {
	directory := t.TempDir()
	fromPath := writeRebaseCorpus(t, directory, "from.json.gz", rebaseCorpus("old", rebaseRecord("old:1:1", "one", "", "case"), rebaseRecord("old:2:2", "two", "", "case")))
	toPath := writeRebaseCorpus(t, directory, "to.json.gz", rebaseCorpus("new", rebaseRecord("new:3:3", "one", "", "case"), rebaseRecord("new:4:4", "two", "", "case")))
	assertionsPath := writeRebaseAssertions(t, directory, assertionsFor("old:2:2/case", "old:1:1/case"))
	outputPath := filepath.Join(directory, "preview.go")
	if _, err := rebaseParserAssertions(fromPath, toPath, assertionsPath, outputPath, nil); err != nil {
		t.Fatal(err)
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(output), "new:4:4/case") > strings.Index(string(output), "new:3:3/case") {
		t.Fatalf("preview changed assertion order:\n%s", output)
	}
}

func TestRebaseParserAssertionsReportsUnresolvedWithoutWritingPreview(t *testing.T) {
	directory := t.TempDir()
	fromPath := writeRebaseCorpus(t, directory, "from.json.gz", rebaseCorpus("old", rebaseRecord("old:1:1", "source", "", "case"), rebaseRecord("old:2:2", "source", "", "case")))
	toPath := writeRebaseCorpus(t, directory, "to.json.gz", rebaseCorpus("new", rebaseRecord("new:5:5", "source", "", "case")))
	assertionsPath := writeRebaseAssertions(t, directory, assertionsFor("old:1:1/case"))
	outputPath := filepath.Join(directory, "preview.go")
	if err := os.WriteFile(outputPath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := rebaseParserAssertions(fromPath, toPath, assertionsPath, outputPath, nil)
	if err == nil || report.Unresolved != 1 || report.UnresolvedCases[0].Reason == "" {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	output, readErr := os.ReadFile(outputPath)
	if readErr != nil || string(output) != "keep" {
		t.Fatalf("preview = %q, err = %v", output, readErr)
	}
}

func TestRebaseParserAssertionsRequiresReviewedErrorArgumentChange(t *testing.T) {
	directory := t.TempDir()
	fromPath := writeRebaseCorpus(t, directory, "from.json.gz", rebaseCorpus("old", rebaseRecord("old:1:1", "source", "E1", "case")))
	toPath := writeRebaseCorpus(t, directory, "to.json.gz", rebaseCorpus("new", rebaseRecord("new:2:2", "source", "E2", "case")))
	assertionsPath := writeRebaseAssertions(t, directory, assertionsFor("old:1:1/case"))
	report, err := rebaseParserAssertions(fromPath, toPath, assertionsPath, "", nil)
	if err == nil || report.Unresolved != 1 || len(report.UnresolvedCases[0].Candidates) != 1 || report.UnresolvedCases[0].Candidates[0].ErrorArgument != "E2" {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	report, err = rebaseParserAssertions(fromPath, toPath, assertionsPath, "", []string{"old:1:1/case=new:2:2/case"})
	if err != nil || report.Reviewed != 1 || report.Mappings[0].Method != "reviewed" {
		t.Fatalf("reviewed report = %#v, err = %v", report, err)
	}
}

func TestRebaseParserAssertionsRejectsInvalidReviewsAndCorpus(t *testing.T) {
	directory := t.TempDir()
	fromPath := writeRebaseCorpus(t, directory, "from.json.gz", rebaseCorpus("old", rebaseRecord("old:1:1", "source", "", "case"), rebaseRecord("old:2:2", "source", "", "other")))
	toPath := writeRebaseCorpus(t, directory, "to.json.gz", rebaseCorpus("new", rebaseRecord("new:2:2", "changed", "", "case")))
	assertionsPath := writeRebaseAssertions(t, directory, assertionsFor("old:1:1/case", "old:2:2/other"))
	report, err := rebaseParserAssertions(fromPath, toPath, assertionsPath, "", nil)
	if err == nil || report.Unresolved != 2 || len(report.UnresolvedCases[0].Candidates) != 0 {
		t.Fatalf("source-change report = %#v, err = %v", report, err)
	}
	if _, err := rebaseParserAssertions(fromPath, toPath, assertionsPath, "", []string{"old:1:1/case=new:2:2/case", "old:1:1/case=new:2:2/case"}); err == nil {
		t.Fatal("duplicate review was accepted")
	}
	if _, err := rebaseParserAssertions(fromPath, toPath, assertionsPath, "", []string{"old:1:1/case=new:2:2/case"}); err == nil {
		t.Fatal("source-changing review was accepted")
	}
	if _, err := rebaseParserAssertions(fromPath, toPath, assertionsPath, "", []string{"old:2:2/other=new:2:2/case"}); err == nil {
		t.Fatal("invalid review target was accepted")
	}
	if _, err := rebaseParserAssertions(fromPath, toPath, assertionsPath, "", []string{"old:1:1/case=missing:1:1/case"}); err == nil {
		t.Fatal("missing review target was accepted")
	}
	if _, err := rebaseParserAssertions(fromPath, toPath, assertionsPath, "", []string{"unused:1:1/case=new:2:2/case"}); err == nil {
		t.Fatal("unused review was accepted")
	}
	duplicateFrom := writeRebaseCorpus(t, directory, "duplicate-from.json.gz", rebaseCorpus("old", rebaseRecord("old:3:3", "same", "", "case"), rebaseRecord("old:4:4", "same", "", "case")))
	duplicateTo := writeRebaseCorpus(t, directory, "duplicate-to.json.gz", rebaseCorpus("new", rebaseRecord("new:3:3", "same", "", "case")))
	duplicateAssertions := writeRebaseAssertions(t, directory, assertionsFor("old:3:3/case", "old:4:4/case"))
	if _, err := rebaseParserAssertions(duplicateFrom, duplicateTo, duplicateAssertions, "", []string{"old:3:3/case=new:3:3/case", "old:4:4/case=new:3:3/case"}); err == nil {
		t.Fatal("duplicate review target was accepted")
	}
	bad := rebaseCorpus("bad", rebaseRecord("bad:1:1", "source", "", "case"))
	bad.SchemaVersion = 2
	badPath := writeRebaseCorpus(t, directory, "bad.json.gz", bad)
	if _, err := rebaseParserAssertions(badPath, toPath, assertionsPath, "", nil); err == nil {
		t.Fatal("bad corpus schema was accepted")
	}
	badIDs := rebaseCorpus("bad-ids", rebaseRecord("duplicate:1:1", "one", "", "case"), rebaseRecord("duplicate:1:1", "two", "", "other"))
	badIDsPath := writeRebaseCorpus(t, directory, "bad-ids.json.gz", badIDs)
	if _, err := rebaseParserAssertions(badIDsPath, toPath, assertionsPath, "", nil); err == nil {
		t.Fatal("duplicate corpus IDs were accepted")
	}
	corrupt, err := os.ReadFile(toPath)
	if err != nil {
		t.Fatal(err)
	}
	corrupt[len(corrupt)-1] ^= 1
	corruptPath := filepath.Join(directory, "corrupt.json.gz")
	if err := os.WriteFile(corruptPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := rebaseParserAssertions(fromPath, corruptPath, assertionsPath, "", nil); err == nil {
		t.Fatal("corrupt gzip trailer was accepted")
	}
}

func rebaseCorpus(tag string, records ...parserCaseRecord) parserCaseCorpus {
	return parserCaseCorpus{SchemaVersion: 1, Tag: tag, Commit: strings.Repeat("a", 40), Records: records}
}

func rebaseRecord(id, source, argument, name string) parserCaseRecord {
	return parserCaseRecord{
		ID: id, Path: "src/testdir/test.vim", Line: 1, Offset: 1, Helper: "CheckScriptFailure", ErrorArgument: argument,
		Cases: []parserCaseVariant{{Name: name, Context: "script", Source: source, VimOutcome: "failure", Expectation: "unclassified"}},
	}
}

func writeRebaseCorpus(t *testing.T, directory, name string, corpus parserCaseCorpus) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := writeJSONGzip(path, corpus); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeRebaseAssertions(t *testing.T, directory, source string) string {
	t.Helper()
	file, err := os.CreateTemp(directory, "assertions-*.go")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	if _, err := file.WriteString("package syntax\n\n" + source); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertionsFor(keys ...string) string {
	var source strings.Builder
	source.WriteString("func officialParserExpectedFailures() map[string]string {\n\treturn map[string]string{\n")
	for _, key := range keys {
		source.WriteString("\t\t\"")
		source.WriteString(key)
		source.WriteString("\": \"vim/E1\",\n")
	}
	source.WriteString("\t}\n}\n")
	return source.String()
}
