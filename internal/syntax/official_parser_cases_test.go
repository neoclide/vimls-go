package syntax

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	officialParserCasesSchemaVersion = 1
	officialParserCasesManifest      = "v9.2.1015-parser-files.json"
	officialParserCasesManifestHash  = "ecab4392e31df7afee323dc2b9be0b9487dba8fe4113cd0d2a81b1761c39b2df"
	officialParserCasesFileCount     = 44
	officialParserCasesRecordCount   = 3844
	officialParserCasesExtracted     = 3805
	officialParserCasesSkipped       = 39
	officialParserCasesCount         = 5261
	officialParserCasesAcceptCount   = 1761
	officialParserCasesUnclassified  = 3500
)

type officialParserCaseCorpus struct {
	SchemaVersion  int                        `json:"schemaVersion"`
	Tag            string                     `json:"tag"`
	Commit         string                     `json:"commit"`
	Manifest       string                     `json:"manifest"`
	ManifestSHA256 string                     `json:"manifestSHA256"`
	Files          []string                   `json:"files"`
	Records        []officialParserCaseRecord `json:"records"`
	Summary        officialParserCaseSummary  `json:"summary"`
}

type officialParserCaseSummary struct {
	Calls             int `json:"calls"`
	ExtractedCalls    int `json:"extractedCalls"`
	SkippedCalls      int `json:"skippedCalls"`
	Cases             int `json:"cases"`
	AcceptedCases     int `json:"acceptedCases"`
	UnclassifiedCases int `json:"unclassifiedCases"`
	DirectLists       int `json:"directLists"`
	Heredocs          int `json:"heredocs"`
	ListAssignments   int `json:"listAssignments"`
	ListConcats       int `json:"listConcats"`
}

type officialParserCaseRecord struct {
	ID            string               `json:"id"`
	Path          string               `json:"path"`
	Line          int                  `json:"line"`
	Offset        int                  `json:"offset"`
	CallStart     int                  `json:"callStart"`
	CallEnd       int                  `json:"callEnd"`
	Helper        string               `json:"helper"`
	ArgumentKind  string               `json:"argumentKind"`
	InputKind     string               `json:"inputKind"`
	InputStart    int                  `json:"inputStart"`
	InputEnd      int                  `json:"inputEnd"`
	ErrorArgument string               `json:"errorArgument"`
	Disposition   string               `json:"disposition"`
	Reason        string               `json:"reason"`
	Cases         []officialParserCase `json:"cases"`
}

type officialParserCase struct {
	Name              string `json:"name"`
	Context           string `json:"context"`
	VimOutcome        string `json:"vimOutcome"`
	ParserExpectation string `json:"parserExpectation"`
	Source            string `json:"source"`
}

func TestOfficialVimParserCases(t *testing.T) {
	corpus := readOfficialParserCases(t)
	checkOfficialParserCaseCorpus(t, corpus)

	accepted := 0
	unclassified := 0
	extracted := 0
	skipped := 0
	cases := 0
	ids := make(map[string]struct{}, len(corpus.Records))
	for _, record := range corpus.Records {
		if _, exists := ids[record.ID]; exists {
			t.Fatalf("duplicate parser-case record id %q", record.ID)
		}
		ids[record.ID] = struct{}{}
		checkOfficialParserCaseRecord(t, record, corpus.Files)
		cases += len(record.Cases)
		switch record.Disposition {
		case "extracted":
			extracted++
		case "skipped":
			skipped++
		}
		for _, testCase := range record.Cases {
			if testCase.Name == "" || testCase.Context == "" || testCase.Source == "" {
				t.Fatalf("%s: parser case is missing name, context, or source", record.ID)
			}
			if testCase.VimOutcome != "success" && testCase.VimOutcome != "failure" {
				t.Fatalf("%s/%s: unsupported Vim outcome %q", record.ID, testCase.Name, testCase.VimOutcome)
			}
			switch testCase.ParserExpectation {
			case "accept":
				if testCase.VimOutcome != "success" {
					t.Fatalf("%s/%s: accepted parser case has Vim outcome %q", record.ID, testCase.Name, testCase.VimOutcome)
				}
				accepted++
				file := Parse(testCase.Source)
				origin := fmt.Sprintf("%s:%d:%d %s/%s", record.Path, record.Line, record.Offset, record.Helper, testCase.Name)
				if file.Source != testCase.Source {
					t.Fatalf("%s: parser did not retain source", origin)
				}
				if len(file.Commands) == 0 {
					t.Fatalf("%s: parser produced no commands", origin)
				}
				if len(file.Diagnostics) != 0 {
					t.Fatalf("%s: unexpected diagnostics: %#v", origin, file.Diagnostics)
				}
				assertFileSpansAt(t, file, origin)
			case "unclassified":
				if testCase.VimOutcome != "failure" {
					t.Fatalf("%s/%s: unclassified parser case has Vim outcome %q", record.ID, testCase.Name, testCase.VimOutcome)
				}
				// Failure helpers may fail during compilation, type checking, or
				// execution. They are retained as provenance, but are not parser
				// negative assertions.
				unclassified++
			default:
				t.Fatalf("%s: unsupported parser expectation %q", record.ID, testCase.ParserExpectation)
			}
		}
	}
	if extracted != corpus.Summary.ExtractedCalls || skipped != corpus.Summary.SkippedCalls || cases != corpus.Summary.Cases || accepted != corpus.Summary.AcceptedCases || unclassified != corpus.Summary.UnclassifiedCases {
		t.Fatalf("official parser-case summary does not match records: extracted = %d, skipped = %d, cases = %d, accepted = %d, unclassified = %d; summary = %#v", extracted, skipped, cases, accepted, unclassified, corpus.Summary)
	}
	if accepted != officialParserCasesAcceptCount || unclassified != officialParserCasesUnclassified {
		t.Fatalf("official parser-case expectations changed: accepted = %d, unclassified = %d", accepted, unclassified)
	}
}

func TestOfficialVimParserFailures(t *testing.T) {
	// These failures are statically decided by the v9.2.1015 parser. Keep
	// execution, type-checking, and other unclassified failures out of this
	// allowlist until their parser phase is proven independently.
	expected := map[string]string{
		"src/testdir/test_expr.vim:1051:41636/def":                 "vim/E1004",
		"src/testdir/test_expr.vim:1051:41636/vim9-script":         "vim/E1004",
		"src/testdir/test_expr.vim:1052:41709/def":                 "vim/E1004",
		"src/testdir/test_expr.vim:1052:41709/vim9-script":         "vim/E1004",
		"src/testdir/test_expr.vim:1053:41782/def":                 "vim/E1004",
		"src/testdir/test_expr.vim:1053:41782/vim9-script":         "vim/E1004",
		"src/testdir/test_expr.vim:1054:41855/def":                 "vim/E1004",
		"src/testdir/test_expr.vim:1054:41855/vim9-script":         "vim/E1004",
		"src/testdir/test_tuple.vim:62:1444/def":                   "vim/E1008",
		"src/testdir/test_tuple.vim:62:1444/vim9-script":           "vim/E1008",
		"src/testdir/test_tuple.vim:67:1600/def":                   "vim/E1008",
		"src/testdir/test_tuple.vim:67:1600/vim9-script":           "vim/E1008",
		"src/testdir/test_tuple.vim:72:1754/def":                   "vim/E1069",
		"src/testdir/test_tuple.vim:72:1754/vim9-script":           "vim/E1069",
		"src/testdir/test_tuple.vim:77:1924/def":                   "vim/E1069",
		"src/testdir/test_tuple.vim:77:1924/vim9-script":           "vim/E1069",
		"src/testdir/test_tuple.vim:82:2083/def":                   "vim/E1068",
		"src/testdir/test_tuple.vim:82:2083/vim9-script":           "vim/E1068",
		"src/testdir/test_tuple.vim:87:2239/def":                   "vim/E1069",
		"src/testdir/test_tuple.vim:87:2239/vim9-script":           "vim/E1069",
		"src/testdir/test_tuple.vim:92:2394/def":                   "vim/E1068",
		"src/testdir/test_tuple.vim:97:2538/def":                   "vim/E1068",
		"src/testdir/test_tuple.vim:97:2538/vim9-script":           "vim/E1068",
		"src/testdir/test_tuple.vim:104:2756/legacy":               "vim/E1527",
		"src/testdir/test_tuple.vim:104:2756/def":                  "vim/E1527",
		"src/testdir/test_tuple.vim:104:2756/vim9-script":          "vim/E1527",
		"src/testdir/test_tuple.vim:112:3010/legacy":               "vim/E1526",
		"src/testdir/test_tuple.vim:112:3010/def":                  "vim/E1526",
		"src/testdir/test_tuple.vim:112:3010/vim9-script":          "vim/E1526",
		"src/testdir/test_tuple.vim:120:3271/def":                  "vim/E1010",
		"src/testdir/test_tuple.vim:120:3271/vim9-script":          "vim/E1010",
		"src/testdir/test_tuple.vim:127:3486/def":                  "vim/E1539",
		"src/testdir/test_tuple.vim:127:3486/vim9-script":          "vim/E1539",
		"src/testdir/test_tuple.vim:143:3972/def":                  "vim/E1068",
		"src/testdir/test_tuple.vim:151:4245/def":                  "vim/E1068",
		"src/testdir/test_vim9_assign.vim:1632:40950/def":          "vim/E1068",
		"src/testdir/test_vim9_assign.vim:1633:41010/def":          "vim/E1009",
		"src/testdir/test_vim9_assign.vim:1636:41143/def":          "vim/E1009",
		"src/testdir/test_vim9_class.vim:11721:265332/def":         "vim/E1008",
		"src/testdir/test_vim9_class.vim:11721:265332/vim9-script": "vim/E1008",
		"src/testdir/test_vim9_class.vim:11726:265505/def":         "vim/E1068",
		"src/testdir/test_vim9_class.vim:11726:265505/vim9-script": "vim/E1068",
		"src/testdir/test_vim9_class.vim:11731:265708/def":         "vim/E1009",
		"src/testdir/test_vim9_class.vim:11731:265708/vim9-script": "vim/E1009",
		"src/testdir/test_vim9_class.vim:11736:265882/def":         "vim/E1009",
		"src/testdir/test_vim9_interface.vim:553:12767/script":     "vim/E1389",
		"src/testdir/test_vim9_cmd.vim:1275:27368/def":             "vim/E1082",
		"src/testdir/test_vim9_cmd.vim:1275:27368/vim9-script":     "vim/E1082",
		"src/testdir/test_vim9_generics.vim:22:376/script":         "vim/E1552",
		"src/testdir/test_vim9_generics.vim:30:565/script":         "vim/E1555",
		"src/testdir/test_vim9_generics.vim:83:1769/script":        "vim/E1552",
		"src/testdir/test_vim9_generics.vim:92:2032/script":        "vim/E1553",
		"src/testdir/test_vim9_generics.vim:133:3045/script":       "vim/E1068",
		"src/testdir/test_vim9_generics.vim:141:3216/script":       "vim/E1068",
		"src/testdir/test_vim9_generics.vim:149:3384/script":       "vim/E1202",
		"src/testdir/test_vim9_generics.vim:157:3555/script":       "vim/E1202",
		"src/testdir/test_vim9_generics.vim:165:3725/script":       "vim/E1069",
		"src/testdir/test_vim9_generics.vim:173:3893/script":       "vim/E1008",
		"src/testdir/test_vim9_generics.vim:181:4050/script":       "vim/E1008",
		"src/testdir/test_vim9_generics.vim:189:4209/script":       "vim/E1008",
		"src/testdir/test_vim9_generics.vim:197:4369/script":       "vim/E1202",
		"src/testdir/test_vim9_generics.vim:205:4543/script":       "vim/E1069",
		"src/testdir/test_vim9_generics.vim:213:4713/script":       "vim/E1202",
		"src/testdir/test_vim9_generics.vim:221:4889/script":       "vim/E1202",
		"src/testdir/test_vim9_generics.vim:229:5072/script":       "vim/E1202",
		"src/testdir/test_vim9_generics.vim:2456:54444/script":     "vim/E1561",
		"src/testdir/test_vim9_func.vim:1157:24812/script":         "vim/E1069",
		"src/testdir/test_vim9_func.vim:1689:36926/def":            "vim/E1157",
		"src/testdir/test_vim9_func.vim:1689:36926/vim9-script":    "vim/E1157",
		"src/testdir/test_vim9_func.vim:2078:45879/def":            "vim/E1007",
		"src/testdir/test_vim9_func.vim:2086:46156/def":            "vim/E110",
		"src/testdir/test_vim9_func.vim:2087:46235/def":            "vim/E110",
		"src/testdir/test_vim9_func.vim:2114:47060/script":         "vim/E1180",
		"src/testdir/test_vim9_func.vim:2441:54782/script":         "vim/E1069",
		"src/testdir/test_vim9_func.vim:2462:55226/script":         "vim/E1008",
		"src/testdir/test_vim9_func.vim:2464:55349/script":         "vim/E1008",
		"src/testdir/test_vim9_func.vim:2481:55796/script":         "vim/E1008",
		"src/testdir/test_vim9_func.vim:2482:55906/script":         "vim/E1055",
		"src/testdir/test_vim9_func.vim:2483:56005/script":         "vim/E1069",
		"src/testdir/test_vim9_func.vim:2485:56148/script":         "vim/E1069",
		"src/testdir/test_vim9_func.vim:2495:56457/script":         "vim/E1068",
		"src/testdir/test_vim9_func.vim:2505:56703/script":         "vim/E1069",
		"src/testdir/test_vim9_func.vim:2883:66057/def":            "vim/E1180",
		"src/testdir/test_vim9_func.vim:2885:66191/def":            "vim/E1068",
		"src/testdir/test_vim9_func.vim:2886:66262/def":            "vim/E1069",
		"src/testdir/test_vim9_func.vim:2887:66332/def":            "vim/E1005",
		"src/testdir/test_vim9_func.vim:2888:66507/def":            "vim/E1069",
		"src/testdir/test_vim9_enum.vim:151:3717/script":           "vim/E1123",
		"src/testdir/test_vim9_enum.vim:161:3943/script":           "vim/E1123",
		"src/testdir/test_vim9_enum.vim:172:4190/script":           "vim/E1123",
		"src/testdir/test_vim9_enum.vim:182:4421/script":           "vim/E1123",
		"src/testdir/test_vim9_enum.vim:194:4637/script":           "vim/E1123",
		"src/testdir/test_vim9_enum.vim:204:4829/script":           "vim/E1123",
		"src/testdir/test_vim9_enum.vim:252:5905/script":           "vim/E1123",
		"src/testdir/test_vim9_enum.vim:378:8621/script":           "vim/E1123",
		"src/testdir/test_vim9_enum.vim:1527:34659/script":         "vim/E1068",
		"src/testdir/test_vim9_enum.vim:1541:34927/script":         "vim/E1068",
		"src/testdir/test_vim9_typealias.vim:62:1839/script":       "vim/E1397",
		"src/testdir/test_vim9_typealias.vim:76:2174/script":       "vim/E1398",
		"src/testdir/test_vim9_script.vim:1810:37557/script":       "vim/E1039",
		"src/testdir/test_vim9_script.vim:1811:37626/script":       "vim/E1040",
	}

	corpus := readOfficialParserCases(t)
	seen := make(map[string]struct{}, len(expected))
	for _, record := range corpus.Records {
		for _, testCase := range record.Cases {
			key := record.ID + "/" + testCase.Name
			code, ok := expected[key]
			if !ok {
				continue
			}
			if testCase.VimOutcome != "failure" || testCase.ParserExpectation != "unclassified" {
				t.Fatalf("%s: official case classification changed: outcome=%q expectation=%q", key, testCase.VimOutcome, testCase.ParserExpectation)
			}
			file := Parse(testCase.Source)
			if file.Source != testCase.Source || len(file.Commands) == 0 {
				t.Fatalf("%s: parser did not retain and recover the official source", key)
			}
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != code {
				t.Fatalf("%s: diagnostics = %#v, want %s", key, file.Diagnostics, code)
			}
			assertFileSpansAt(t, file, key)
			seen[key] = struct{}{}
		}
	}
	if len(seen) != len(expected) {
		var missing []string
		for key := range expected {
			if _, ok := seen[key]; !ok {
				missing = append(missing, key)
			}
		}
		sort.Strings(missing)
		t.Fatalf("official parser failures missing from artifact: %v", missing)
	}
}

func readOfficialParserCases(t *testing.T) officialParserCaseCorpus {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "official", "v9.2.1015-parser-cases.json.gz")
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
	var corpus officialParserCaseCorpus
	if err := json.NewDecoder(io.LimitReader(reader, 64<<20)).Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	return corpus
}

func checkOfficialParserCaseCorpus(t *testing.T, corpus officialParserCaseCorpus) {
	t.Helper()
	if corpus.SchemaVersion != officialParserCasesSchemaVersion || corpus.Tag != officialVimTag || corpus.Commit != officialVimCommit {
		t.Fatalf("unexpected official parser-case provenance: schema = %d, tag = %q, commit = %q", corpus.SchemaVersion, corpus.Tag, corpus.Commit)
	}
	if corpus.Manifest != officialParserCasesManifest || corpus.ManifestSHA256 != officialParserCasesManifestHash {
		t.Fatalf("unexpected parser-case manifest: name = %q, sha256 = %q", corpus.Manifest, corpus.ManifestSHA256)
	}
	if len(corpus.Files) != officialParserCasesFileCount || !sort.StringsAreSorted(corpus.Files) {
		t.Fatalf("unexpected parser-case files: count = %d, sorted = %t", len(corpus.Files), sort.StringsAreSorted(corpus.Files))
	}
	for index := 1; index < len(corpus.Files); index++ {
		if corpus.Files[index] == corpus.Files[index-1] {
			t.Fatalf("duplicate parser-case file %q", corpus.Files[index])
		}
	}
	if len(corpus.Records) != officialParserCasesRecordCount {
		t.Fatalf("unexpected parser-case records: %d", len(corpus.Records))
	}
	wantSummary := officialParserCaseSummary{
		Calls: officialParserCasesRecordCount, ExtractedCalls: officialParserCasesExtracted, SkippedCalls: officialParserCasesSkipped,
		Cases: officialParserCasesCount, AcceptedCases: officialParserCasesAcceptCount, UnclassifiedCases: officialParserCasesUnclassified,
		DirectLists: 873, Heredocs: 2884, ListAssignments: 2, ListConcats: 46,
	}
	if corpus.Summary != wantSummary {
		t.Fatalf("unexpected parser-case summary: %#v", corpus.Summary)
	}
}

func checkOfficialParserCaseRecord(t *testing.T, record officialParserCaseRecord, files []string) {
	t.Helper()
	fileIndex := sort.SearchStrings(files, record.Path)
	if record.Path == "" || fileIndex == len(files) || files[fileIndex] != record.Path {
		t.Fatalf("%s: record path is not in parser-case file list", record.ID)
	}
	if record.Line < 1 || record.Offset < 0 || record.CallStart < 0 || record.CallEnd <= record.CallStart || record.CallStart > record.Offset || record.CallEnd <= record.Offset {
		t.Fatalf("%s: invalid source coordinates: line=%d offset=%d call=[%d,%d)", record.ID, record.Line, record.Offset, record.CallStart, record.CallEnd)
	}
	wantID := fmt.Sprintf("%s:%d:%d", record.Path, record.Line, record.Offset)
	if record.ID != wantID {
		t.Fatalf("record has unstable id %q, want %q", record.ID, wantID)
	}
	switch record.Disposition {
	case "skipped":
		if len(record.Cases) != 0 || strings.TrimSpace(record.Reason) == "" {
			t.Fatalf("%s: skipped record must have a reason and no cases", record.ID)
		}
	case "extracted":
		if record.ArgumentKind == "" || record.InputKind == "" || record.InputEnd <= record.InputStart || record.InputStart < 0 {
			t.Fatalf("%s: extracted record has invalid input coordinates or kind", record.ID)
		}
		if len(record.Cases) == 0 {
			t.Fatalf("%s: extracted record has no cases", record.ID)
		}
	default:
		t.Fatalf("%s: unsupported disposition %q", record.ID, record.Disposition)
	}
}
