package syntax

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/neoclide/vimls-go/internal/vimdata"
)

type officialCorpusLock struct {
	SchemaVersion  int    `json:"schemaVersion"`
	Tag            string `json:"tag"`
	Commit         string `json:"commit"`
	ManifestSHA256 string `json:"manifestSHA256"`
	Embedded       struct {
		Files      int `json:"files"`
		Cases      int `json:"cases"`
		Successes  int `json:"successes"`
		Failures   int `json:"failures"`
		Structural int `json:"structural"`
	} `json:"embedded"`
	TestFiles struct {
		Files    int `json:"files"`
		RawBytes int `json:"rawBytes"`
	} `json:"testFiles"`
	ParserCases struct {
		Files   int                       `json:"files"`
		Records int                       `json:"records"`
		Summary officialParserCaseSummary `json:"summary"`
	} `json:"parserCases"`
}

type generatedOfficialCorpus struct {
	Tag    string                        `json:"tag"`
	Commit string                        `json:"commit"`
	Files  []string                      `json:"files"`
	Cases  []generatedOfficialCorpusCase `json:"cases"`
}

type generatedOfficialCorpusCase struct {
	Origin  string `json:"origin"`
	Source  string `json:"source"`
	Outcome string `json:"outcome,omitempty"`
}

func TestGeneratedOfficialVimEmbeddedCorpus(t *testing.T) {
	lock := readOfficialCorpusLock(t)
	corpus := readGeneratedOfficialCorpus(t)
	if corpus.Tag != lock.Tag || corpus.Commit != lock.Commit || len(corpus.Files) != lock.Embedded.Files || len(corpus.Cases) != lock.Embedded.Cases {
		t.Fatalf("unexpected corpus provenance: tag = %q, commit = %q, files = %d, cases = %d", corpus.Tag, corpus.Commit, len(corpus.Files), len(corpus.Cases))
	}
	if !sort.StringsAreSorted(corpus.Files) {
		t.Fatalf("official file manifest is not sorted: %#v", corpus.Files)
	}
	for index := 1; index < len(corpus.Files); index++ {
		if corpus.Files[index] == corpus.Files[index-1] {
			t.Fatalf("official file manifest contains duplicate %q", corpus.Files[index])
		}
	}
	parsers := []struct {
		name  string
		parse func(string) *File
	}{
		{name: "legacy", parse: (LegacyParser{}).Parse},
		{name: "vim9", parse: (Vim9Parser{}).Parse},
	}
	statistics := make([]officialParseStatistics, len(parsers))
	successes := 0
	failures := 0
	structural := 0
	for _, test := range corpus.Cases {
		for index, parser := range parsers {
			file := parser.parse(test.Source)
			if file.Source != test.Source {
				t.Fatalf("%s %s parser did not retain source", test.Origin, parser.name)
			}
			if len(file.Commands) == 0 && len(test.Source) != 0 {
				t.Fatalf("%s %s parser produced no commands", test.Origin, parser.name)
			}
			assertFileSpansAt(t, file, test.Origin+" "+parser.name)
			statistics[index].add(file)
		}
		switch test.Outcome {
		case "success":
			successes++
			file := Parse(test.Source)
			if len(file.Diagnostics) != 0 {
				vim9 := (Vim9Parser{}).Parse(test.Source)
				if len(vim9.Diagnostics) != 0 {
					t.Fatalf("%s is accepted by Vim %s but automatic diagnostics = %#v and Vim9 diagnostics = %#v", test.Origin, corpus.Tag, file.Diagnostics, vim9.Diagnostics)
				}
			}
		case "failure":
			failures++
		default:
			structural++
		}
	}
	if successes != lock.Embedded.Successes || failures != lock.Embedded.Failures || structural != lock.Embedded.Structural {
		t.Fatalf("official outcome metadata changed: successes = %d, failures = %d, structural = %d", successes, failures, structural)
	}
	for index, stats := range statistics {
		t.Logf("%s official corpus: commands=%d declarations=%d functions=%d aggregates=%d aliases=%d imports=%d expression roots=%d", parsers[index].name, stats.commands, stats.declarations, stats.functions, stats.aggregates, stats.aliases, stats.imports, stats.expressions)
		if stats.commands < 20_000 || stats.declarations < 2_500 || stats.functions < 2_000 || stats.aggregates < 1_000 || stats.aliases < 80 || stats.imports < 150 || stats.expressions < 5_000 {
			t.Fatalf("%s parser did not retain the expected breadth of official syntax: %#v", parsers[index].name, stats)
		}
	}
}

func readOfficialCorpusLock(t *testing.T) officialCorpusLock {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "official", vimdata.VimSourceTag+"-corpus-lock.json")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lock officialCorpusLock
	if err := json.Unmarshal(source, &lock); err != nil {
		t.Fatal(err)
	}
	if lock.SchemaVersion != 1 || lock.Tag != officialVimTag || lock.Commit != vimdata.VimSourceCommit || lock.ManifestSHA256 == "" {
		t.Fatalf("unexpected official corpus lock: %#v", lock)
	}
	return lock
}

type officialParseStatistics struct {
	commands     int
	declarations int
	functions    int
	aggregates   int
	aliases      int
	imports      int
	expressions  int
}

func (stats *officialParseStatistics) add(file *File) {
	stats.commands += len(file.Commands)
	for _, command := range file.Commands {
		if command.Declaration != nil {
			stats.declarations++
		}
		if command.Function != nil {
			stats.functions++
		}
		if command.Aggregate != nil {
			stats.aggregates++
		}
		if command.TypeAlias != nil {
			stats.aliases++
		}
		if command.Import != nil {
			stats.imports++
		}
		stats.expressions += len(command.Expressions)
	}
}

func readGeneratedOfficialCorpus(t *testing.T) generatedOfficialCorpus {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "official", vimdata.VimSourceTag+"-parser-corpus.json.gz")
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
	var corpus generatedOfficialCorpus
	if err := json.NewDecoder(reader).Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	return corpus
}
