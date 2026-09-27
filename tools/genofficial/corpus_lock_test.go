package main

import (
	"path/filepath"
	"testing"
)

func TestPinnedCorpusLock(t *testing.T) {
	lock := readReviewedCorpusLock(t)
	files := readPinnedTestFiles(t)
	inventory := readPinnedHelperInventory(t)
	embedded := readPinnedEmbeddedCorpus(t)
	manifest, err := readParserFileManifest(filepath.Join("..", "..", "testdata", "official", parserFileManifestName))
	if err != nil {
		t.Fatal(err)
	}
	var parserCases parserCaseCorpus
	readPinnedGzipJSON(t, officialArtifactName("parser-cases"), &parserCases)
	candidate, err := buildCorpusLock(embedded, files, inventory, manifest, parserCases)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCorpusLock(lock, candidate); err != nil {
		t.Fatal(err)
	}
}

func TestCorpusLockRejectsChangedInventory(t *testing.T) {
	lock := readReviewedCorpusLock(t)
	candidate := lock
	candidate.TestFiles.RawBytes++
	if err := validateCorpusLock(lock, candidate); err == nil {
		t.Fatal("changed corpus lock was accepted")
	}
}
