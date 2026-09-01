package workspace

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

type indexCorpus struct {
	Files []struct {
		Path   string `json:"path"`
		Source []byte `json:"source"`
	} `json:"files"`
}

// Indexing the pinned Vim test corpus verifies that index construction remains
// total for the same broad input set accepted by the parser.
func TestIndexPinnedVimCorpus(t *testing.T) {
	file, err := os.Open(filepath.Join("..", "..", "testdata", "official", "v9.2.1015-test-files.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var corpus indexCorpus
	if err := json.NewDecoder(reader).Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Files) != 362 {
		t.Fatalf("corpus file count = %d, want 362", len(corpus.Files))
	}

	index := NewIndex(len(corpus.Files)+1, 64<<20)
	root := t.TempDir()
	for number, entry := range corpus.Files {
		path := filepath.Join(root, fmt.Sprintf("%03d.vim", number))
		if err := index.Replace(path, syntax.Parse(string(entry.Source))); err != nil {
			t.Fatalf("index %s: %v", entry.Path, err)
		}
	}
	index.SetComplete(true)
	if !index.Complete() {
		t.Fatal("corpus index is not complete")
	}
}
