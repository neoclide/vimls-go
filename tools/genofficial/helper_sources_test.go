package main

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanHelperHeredocs(t *testing.T) {
	source := []byte("def Test()\n" +
		"  var lines: list<string> =<< trim END # driver comment\n" +
		"      vim9script\n" +
		"        var value = 1\n" +
		"  END\n" +
		"  let raw =<< eval [DONE]\n" +
		"one {value}\n" +
		"[DONE]\n" +
		"enddef\n")
	heredocs := scanHelperHeredocs(source)
	if len(heredocs) != 2 {
		t.Fatalf("heredocs = %#v", heredocs)
	}
	if got := heredocs[0]; got.Name != "lines" || !got.Trim || got.Evaluate || !reflect.DeepEqual(got.Lines, []string{"vim9script", "  var value = 1"}) {
		t.Fatalf("trimmed heredoc = %#v", got)
	}
	if got := heredocs[1]; got.Name != "raw" || got.Trim || !got.Evaluate || !reflect.DeepEqual(got.Lines, []string{"one {value}"}) {
		t.Fatalf("evaluated heredoc = %#v", got)
	}
}

func TestScanHelperHeredocsIgnoresTestText(t *testing.T) {
	source := []byte("var quoted = 'lines =<< trim END'\n" +
		"# ignored =<< END\n" +
		"var snippets =<< trim OUTER\n" +
		"  inner =<< trim INNER\n" +
		"  v9.CheckScriptSuccess(lines)\n" +
		"  INNER\n" +
		"OUTER\n" +
		"var after =<< END\n" +
		"ok\n" +
		"END\n")
	heredocs := scanHelperHeredocs(source)
	if len(heredocs) != 2 || heredocs[0].Name != "snippets" || heredocs[1].Name != "after" {
		t.Fatalf("heredocs = %#v", heredocs)
	}
	offset := len("var quoted = 'lines =<< trim END'\n# ignored =<< END\nvar snippets =<< trim OUTER\n  inner =<< trim INNER\n  ")
	if heredoc, ok := helperHeredocContaining(heredocs, offset); !ok || heredoc.Name != "snippets" {
		t.Fatalf("containing heredoc = %#v, %v", heredoc, ok)
	}
}

func TestScanHelperHeredocsRequiresExactMarker(t *testing.T) {
	source := []byte("lines =<< END\nvalue\n END\nEND \nEND\n")
	heredocs := scanHelperHeredocs(source)
	if len(heredocs) != 1 || !reflect.DeepEqual(heredocs[0].Lines, []string{"value", " END", "END "}) {
		t.Fatalf("heredocs = %#v", heredocs)
	}
}

func TestScanHelperHeredocsSupportsCRLF(t *testing.T) {
	source := []byte("let g:lines =<< trim END\r\n  one\r\nEND\r\n")
	heredocs := scanHelperHeredocs(source)
	if len(heredocs) != 1 || heredocs[0].Name != "g:lines" || !reflect.DeepEqual(heredocs[0].Lines, []string{"one"}) {
		t.Fatalf("heredocs = %#v", heredocs)
	}
}

func TestParseHelperHeredocHeaderRejectsMalformedForms(t *testing.T) {
	for _, source := range []string{
		"echo '=<< END'",
		"var [one, two] =<< END",
		"var lines =<< lower",
		"var lines =<< END trailing",
		"var lines =<<",
	} {
		if name, _, _, _, ok := parseHelperHeredocHeader([]byte(source)); ok {
			t.Fatalf("accepted %q as %q", source, name)
		}
	}
}

func TestPinnedHelperHeredocScan(t *testing.T) {
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
	var corpus testFilesCorpus
	if err := json.NewDecoder(reader).Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	heredocsByPath := make(map[string][]helperHeredoc, len(corpus.Files))
	total := 0
	evaluated := 0
	for _, sourceFile := range corpus.Files {
		heredocs := scanHelperHeredocs(sourceFile.Source)
		heredocsByPath[sourceFile.Path] = heredocs
		for index, heredoc := range heredocs {
			if heredoc.HeaderStart < 0 || heredoc.BodyStart < heredoc.HeaderStart || heredoc.BodyEnd < heredoc.BodyStart || heredoc.End <= heredoc.BodyEnd || heredoc.End > len(sourceFile.Source) {
				t.Fatalf("%s heredoc %d has invalid spans: %#v", sourceFile.Path, index, heredoc)
			}
			if index > 0 && heredocs[index-1].End > heredoc.HeaderStart {
				t.Fatalf("%s heredocs overlap at %d", sourceFile.Path, index)
			}
			if heredoc.Evaluate {
				evaluated++
			}
		}
		total += len(heredocs)
	}
	if total != 4939 || evaluated != 41 {
		t.Fatalf("official heredocs: total=%d evaluated=%d, want 4939 and 41", total, evaluated)
	}

	inventoryPath := filepath.Join("..", "..", "testdata", "official", "v9.2.1015-helper-inventory.json.gz")
	inventoryFile, err := os.Open(inventoryPath)
	if err != nil {
		t.Fatal(err)
	}
	defer inventoryFile.Close()
	inventoryReader, err := gzip.NewReader(inventoryFile)
	if err != nil {
		t.Fatal(err)
	}
	defer inventoryReader.Close()
	var inventory helperInventory
	if err := json.NewDecoder(inventoryReader).Decode(&inventory); err != nil {
		t.Fatal(err)
	}
	pending := 0
	for _, record := range inventory.Records {
		if record.Disposition != "pending-extraction" {
			continue
		}
		pending++
		if heredoc, ok := helperHeredocContaining(heredocsByPath[record.Path], record.Offset); ok {
			t.Fatalf("%s:%d helper call is embedded in heredoc %q: %#v", record.Path, record.Line, heredoc.Name, record)
		}
	}
	if pending != 5208 {
		t.Fatalf("pending helper calls = %d, want 5208", pending)
	}
}
