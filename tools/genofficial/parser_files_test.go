package main

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPinnedParserFileManifest(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "testdata", "official", "v9.2.1015-parser-files.json")
	manifest, err := readParserFileManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 68 {
		t.Fatalf("manifest records = %d, want 68", len(manifest.Files))
	}

	corpusPath := filepath.Join("..", "..", "testdata", "official", "v9.2.1015-test-files.json.gz")
	corpusFile, err := os.Open(corpusPath)
	if err != nil {
		t.Fatal(err)
	}
	defer corpusFile.Close()
	corpusReader, err := gzip.NewReader(corpusFile)
	if err != nil {
		t.Fatal(err)
	}
	defer corpusReader.Close()
	var corpus testFilesCorpus
	if err := json.NewDecoder(corpusReader).Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Files) != 362 {
		t.Fatalf("pinned test files = %d, want 362", len(corpus.Files))
	}
	allPaths := make(map[string]bool, len(corpus.Files))
	for _, file := range corpus.Files {
		allPaths[file.Path] = true
	}
	selected, err := selectParserMigrationFiles(corpus, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 44 {
		t.Fatalf("selected parser migration files = %d, want 44", len(selected))
	}

	inventoryPath := filepath.Join("..", "..", "testdata", "official", "v9.2.1015-helper-inventory.json.gz")
	file, err := os.Open(inventoryPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var inventory helperInventory
	if err := json.NewDecoder(reader).Decode(&inventory); err != nil {
		t.Fatal(err)
	}

	qualifiedByPath := make(map[string]int)
	for _, record := range inventory.Records {
		if record.Disposition == "pending-extraction" {
			qualifiedByPath[record.Path]++
		}
	}
	manifestByPath := make(map[string]parserFileRecord, len(manifest.Files))
	includedFiles := 0
	explicitlyExcludedFiles := 0
	includedCalls := 0
	excludedCalls := 0
	for _, file := range manifest.Files {
		if !allPaths[file.Path] {
			t.Fatalf("manifest path is absent from pinned corpus: %q", file.Path)
		}
		manifestByPath[file.Path] = file
		if file.Disposition == "include" {
			includedFiles++
			includedCalls += qualifiedByPath[file.Path]
		} else {
			explicitlyExcludedFiles++
		}
	}
	for path, calls := range qualifiedByPath {
		if _, ok := manifestByPath[path]; !ok {
			t.Fatalf("qualified helper file %q with %d calls lacks an explicit disposition", path, calls)
		}
	}
	for path, calls := range qualifiedByPath {
		if manifestByPath[path].Disposition != "include" {
			excludedCalls += calls
		}
	}
	if includedFiles != 44 || explicitlyExcludedFiles != 24 || includedCalls != 3844 || excludedCalls != 1364 {
		t.Fatalf("manifest summary: included files=%d calls=%d, explicit excludes=%d, excluded calls=%d", includedFiles, includedCalls, explicitlyExcludedFiles, excludedCalls)
	}
	if implicitExcluded := len(corpus.Files) - includedFiles - explicitlyExcludedFiles; implicitExcluded != 294 {
		t.Fatalf("implicit default excludes = %d, want 294", implicitExcluded)
	}
}

func TestParserFileManifestRejectsInvalidRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	source := `{"schemaVersion":1,"tag":"v9.2.1015","commit":"5ab969f719bb09555e90e8dff8c94fc37bcbf2ae","scope":"test","defaultDisposition":"exclude","files":[{"path":"b","disposition":"include","reason":"ok"},{"path":"a","disposition":"unknown","reason":""}]}`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readParserFileManifest(path); err == nil {
		t.Fatal("invalid manifest was accepted")
	}
}
