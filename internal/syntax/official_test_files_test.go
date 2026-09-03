package syntax

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
)

const (
	officialTestFilesCount = 362
	officialTestFilesBytes = 8558061
	officialVimCommit      = "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae"
)

type generatedOfficialTestFiles struct {
	Tag    string                      `json:"tag"`
	Commit string                      `json:"commit"`
	Files  []generatedOfficialTestFile `json:"files"`
}

type generatedOfficialTestFile struct {
	Path   string `json:"path"`
	Source []byte `json:"source"`
}

func TestGeneratedOfficialVimTestFiles(t *testing.T) {
	corpus := readGeneratedOfficialTestFiles(t)
	if corpus.Tag != officialVimTag || corpus.Commit != officialVimCommit || len(corpus.Files) != officialTestFilesCount {
		t.Fatalf("unexpected official test-file provenance: tag = %q, commit = %q, files = %d", corpus.Tag, corpus.Commit, len(corpus.Files))
	}
	paths := make([]string, len(corpus.Files))
	rawBytes := 0
	for index, file := range corpus.Files {
		paths[index] = file.Path
		rawBytes += len(file.Source)
	}
	if !sort.StringsAreSorted(paths) {
		t.Fatalf("official test-file manifest is not sorted")
	}
	for index := 1; index < len(paths); index++ {
		if paths[index] == paths[index-1] {
			t.Fatalf("official test-file manifest contains duplicate %q", paths[index])
		}
	}
	if rawBytes != officialTestFilesBytes {
		t.Fatalf("official test-file corpus has %d raw bytes, want %d", rawBytes, officialTestFilesBytes)
	}

	var commands atomic.Int64
	t.Run("parse", func(t *testing.T) {
		for _, testFile := range corpus.Files {
			t.Run(testFile.Path, func(t *testing.T) {
				t.Parallel()
				source := string(testFile.Source)
				file := Parse(source)
				if file.Source != source {
					t.Fatal("parser did not retain source")
				}
				assertFileSpansAt(t, file, testFile.Path)
				commands.Add(int64(len(file.Commands)))
			})
		}
	})
	commandCount := commands.Load()
	t.Logf("official test files: commands=%d", commandCount)
	if commandCount < 100000 {
		t.Fatalf("parser retained too few commands: %d", commandCount)
	}
}

func readGeneratedOfficialTestFiles(t *testing.T) generatedOfficialTestFiles {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "official", "v9.2.1015-test-files.json.gz")
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var corpus generatedOfficialTestFiles
	if err := json.NewDecoder(reader).Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	return corpus
}
