package main

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPinnedParserFileManifest(t *testing.T) {
	lock := readReviewedCorpusLock(t)
	manifestPath := filepath.Join("..", "..", "testdata", "official", parserFileManifestName)
	manifest, err := readParserFileManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	corpusPath := filepath.Join("..", "..", "testdata", "official", officialArtifactName("test-files"))
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
	selected, err := selectParserMigrationFiles(corpus, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != lock.ParserFiles.IncludedFiles {
		t.Fatalf("selected parser migration files = %d, want %d", len(selected), lock.ParserFiles.IncludedFiles)
	}

	inventoryPath := filepath.Join("..", "..", "testdata", "official", officialArtifactName("helper-inventory"))
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

	summary, err := summarizeParserFiles(corpus, inventory, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if summary != lock.ParserFiles {
		t.Fatalf("manifest summary = %#v, want %#v", summary, lock.ParserFiles)
	}
}

func TestParserFileManifestRejectsInvalidRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	source := `{"schemaVersion":1,"tag":"` + vimTag + `","commit":"` + vimCommit + `","scope":"test","defaultDisposition":"exclude","files":[{"path":"b","disposition":"include","reason":"ok"},{"path":"a","disposition":"unknown","reason":""}]}`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readParserFileManifest(path); err == nil {
		t.Fatal("invalid manifest was accepted")
	}
}
