package syntax

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
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

	parsers := []struct {
		name  string
		parse func(string) *File
	}{
		{name: "legacy", parse: (LegacyParser{}).Parse},
		{name: "vim9", parse: (Vim9Parser{}).Parse},
	}
	for _, parser := range parsers {
		commands := 0
		for _, testFile := range corpus.Files {
			source := string(testFile.Source)
			file := parser.parse(source)
			if file.Source != source {
				t.Fatalf("%s %s parser did not retain source", testFile.Path, parser.name)
			}
			assertFileSpansAt(t, file, testFile.Path+" "+parser.name)
			commands += len(file.Commands)
		}
		t.Logf("%s official test files: commands=%d", parser.name, commands)
		if commands < 100000 {
			t.Fatalf("%s parser retained too few commands: %d", parser.name, commands)
		}
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
