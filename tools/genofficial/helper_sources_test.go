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
	if got := heredocs[0]; got.Name != "lines" || !got.Trim || got.Evaluate || !got.Complete || !reflect.DeepEqual(got.Lines, []string{"vim9script", "  var value = 1"}) {
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
		if name, _, _, _, ok := parseHelperHeredocHeader([]byte(source), helperLegacy); ok {
			t.Fatalf("accepted %q as %q", source, name)
		}
	}
}

func TestParseHelperHeredocHeaderUsesDialectRules(t *testing.T) {
	legacyValid := []byte(`let lines=<< END " comment`)
	if _, _, _, _, ok := parseHelperHeredocHeader(legacyValid, helperLegacy); !ok {
		t.Fatal("legacy heredoc was rejected")
	}
	vim9Valid := []byte(`var lines =<< END # comment`)
	if _, _, _, _, ok := parseHelperHeredocHeader(vim9Valid, helperVim9); !ok {
		t.Fatal("Vim9 heredoc was rejected")
	}
	for _, source := range [][]byte{
		[]byte(`var lines=<< END`),
		[]byte(`var lines =<<END`),
		[]byte(`var lines =<< END " not a Vim9 comment`),
	} {
		if name, _, _, _, ok := parseHelperHeredocHeader(source, helperVim9); ok {
			t.Fatalf("accepted invalid Vim9 heredoc %q as %q", source, name)
		}
	}
	if name, _, _, _, ok := parseHelperHeredocHeader([]byte(`let lines =<< END # not a legacy comment`), helperLegacy); ok {
		t.Fatalf("accepted invalid legacy heredoc as %q", name)
	}
	legacyWrapped := []byte(`silent vim9cmd legacy let lines=<< END " comment`)
	if name, _, _, _, ok := parseHelperHeredocHeader(legacyWrapped, helperCommandDialect(legacyWrapped, helperLegacy)); !ok || name != "lines" {
		t.Fatalf("modifier-wrapped legacy heredoc = %q, %v", name, ok)
	}
	vim9Wrapped := []byte(`noautocmd legacy vim9cmd var lines =<< END # comment`)
	if name, _, _, _, ok := parseHelperHeredocHeader(vim9Wrapped, helperCommandDialect(vim9Wrapped, helperLegacy)); !ok || name != "lines" {
		t.Fatalf("modifier-wrapped Vim9 heredoc = %q, %v", name, ok)
	}
}

func TestScanHelperHeredocsTracksContextualDialect(t *testing.T) {
	source := []byte("let before = 1\n" +
		"vim9script\n" +
		"let legacy=<< END\nold\nEND\n" +
		"def Vim9Body()\n" +
		"  var modern =<< END\nnew\nEND\n" +
		"enddef\n" +
		"function! LegacyBody()\n" +
		"  let old=<< END\nlegacy\nEND\n" +
		"endfunction\n")
	heredocs := scanHelperHeredocs(source)
	if len(heredocs) != 3 || heredocs[0].Name != "legacy" || heredocs[1].Name != "modern" || heredocs[2].Name != "old" {
		t.Fatalf("contextual heredocs = %#v", heredocs)
	}
}

func TestScanHelperHeredocsRecoversUnclosedBodyAtEOF(t *testing.T) {
	source := []byte("vim9script\nvar lines =<< END\ntext\nother =<< NEXT\nv9.CheckScriptSuccess(lines)\n")
	heredocs := scanHelperHeredocs(source)
	if len(heredocs) != 1 || heredocs[0].Complete || heredocs[0].BodyEnd != len(source) || heredocs[0].End != len(source) || !reflect.DeepEqual(heredocs[0].Lines, []string{"text", "other =<< NEXT", "v9.CheckScriptSuccess(lines)"}) {
		t.Fatalf("heredocs = %#v", heredocs)
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
			if !heredoc.Complete || heredoc.HeaderStart < 0 || heredoc.BodyStart < heredoc.HeaderStart || heredoc.BodyEnd < heredoc.BodyStart || heredoc.End <= heredoc.BodyEnd || heredoc.End > len(sourceFile.Source) {
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
