package analysis

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

type officialAnalysisCorpus struct {
	Cases []officialAnalysisCase `json:"cases"`
}

type officialAnalysisCase struct {
	Origin string `json:"origin"`
	Source string `json:"source"`
}

type officialAnalysisFiles struct {
	Files []officialAnalysisFile `json:"files"`
}

type officialAnalysisFile struct {
	Path   string `json:"path"`
	Source []byte `json:"source"`
}

func TestOfficialVimCorpusAnalysisStability(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "official", "v9.2.1015-parser-corpus.json.gz")
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
	var corpus officialAnalysisCorpus
	if err := json.NewDecoder(reader).Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) != 3267 {
		t.Fatalf("official corpus cases = %d, want 3267", len(corpus.Cases))
	}
	for _, testCase := range corpus.Cases {
		t.Run(testCase.Origin, func(t *testing.T) {
			file := syntax.Parse(testCase.Source)
			result := Analyze(file)
			if result == nil || result.File != file {
				t.Fatalf("analysis result = %#v", result)
			}
			for _, diagnostic := range CombinedDiagnostics(file, result) {
				if diagnostic.Span.Start < 0 || diagnostic.Span.End < diagnostic.Span.Start || diagnostic.Span.End > len(testCase.Source) {
					t.Fatalf("out-of-bounds diagnostic %#v", diagnostic)
				}
			}
		})
	}
}

func TestOfficialVimTestFilesAnalysisStability(t *testing.T) {
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
	var corpus officialAnalysisFiles
	if err := json.NewDecoder(reader).Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Files) != 362 {
		t.Fatalf("official test files = %d, want 362", len(corpus.Files))
	}
	parsers := []struct {
		name  string
		parse func(string) *syntax.File
	}{
		{name: "legacy", parse: (syntax.LegacyParser{}).Parse},
		{name: "vim9", parse: (syntax.Vim9Parser{}).Parse},
	}
	for _, testFile := range corpus.Files {
		for _, parser := range parsers {
			t.Run(testFile.Path+"/"+parser.name, func(t *testing.T) {
				source := string(testFile.Source)
				file := parser.parse(source)
				result := Analyze(file)
				if result == nil || result.File != file {
					t.Fatalf("analysis result = %#v", result)
				}
				for _, diagnostic := range CombinedDiagnostics(file, result) {
					if diagnostic.Span.Start < 0 || diagnostic.Span.End < diagnostic.Span.Start || diagnostic.Span.End > len(source) {
						t.Fatalf("out-of-bounds diagnostic %#v", diagnostic)
					}
				}
			})
		}
	}
}
