package syntax

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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

var (
	officialParserCaseFilter = flag.String("official-case", "", "comma-separated substrings selecting official parser cases")
	officialErrorCodePattern = regexp.MustCompile(`E[0-9]+`)
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

func officialParserExpectedFailures() map[string]string {
	// These failures are statically decided by the v9.2.1015 parser. Keep
	// execution, type-checking, and other unclassified failures out of this
	// allowlist until their parser phase is proven independently.
	return map[string]string{
		"src/testdir/test_vim9_import.vim:591:15641/script":        "vim/E1261",
		"src/testdir/test_vim9_import.vim:603:15955/script":        "vim/E1257",
		"src/testdir/test_vim9_import.vim:1728:42236/script":       "vim/E1043",
		"src/testdir/test_vim9_import.vim:1729:42305/script":       "vim/E1044",
		"src/testdir/test_vim9_assign.vim:3270:80367/def":          "vim/E1279",
		"src/testdir/test_vim9_assign.vim:3270:80367/vim9-script":  "vim/E1279",
		"src/testdir/test_vim9_assign.vim:3277:80522/def":          "vim/E1279",
		"src/testdir/test_vim9_assign.vim:3277:80522/vim9-script":  "vim/E1279",
		"src/testdir/test_vim9_assign.vim:3284:80675/def":          "vim/E15",
		"src/testdir/test_vim9_assign.vim:3284:80675/vim9-script":  "vim/E15",
		"src/testdir/test_vim9_assign.vim:3292:80843/def":          "vim/E1278",
		"src/testdir/test_vim9_assign.vim:3292:80843/vim9-script":  "vim/E1278",
		"src/testdir/test_let.vim:789:18932/script":                "vim/E1279",
		"src/testdir/test_let.vim:796:19078/script":                "vim/E1279",
		"src/testdir/test_let.vim:803:19222/script":                "vim/E15",
		"src/testdir/test_vim9_assign.vim:217:5470/vim9-script":    "vim/E475",
		"src/testdir/test_vim9_assign.vim:385:9704/def":            "vim/E1059",
		"src/testdir/test_vim9_assign.vim:3105:76410/script":       "vim/E488",
		"src/testdir/test_vim9_class.vim:93:2494/script":           "vim/E488",
		"src/testdir/test_vim9_class.vim:102:2735/script":          "vim/E488",
		"src/testdir/test_vim9_assign.vim:385:9704/vim9-script":    "vim/E1059",
		"src/testdir/test_vim9_class.vim:261:7035/script":          "vim/E1059",
		"src/testdir/test_vim9_assign.vim:69:1636/def":             "vim/E1069",
		"src/testdir/test_vim9_assign.vim:70:1685/def":             "vim/E1069",
		"src/testdir/test_vim9_assign.vim:71:1740/def":             "vim/E1069",
		"src/testdir/test_vim9_assign.vim:2001:51173/def":          "vim/E1069",
		"src/testdir/test_vim9_assign.vim:2001:51173/vim9-script":  "vim/E1069",
		"src/testdir/test_vim9_assign.vim:2533:63718/script":       "vim/E1069",
		"src/testdir/test_vim9_assign.vim:2535:63760/def":          "vim/E1069",
		"src/testdir/test_vim9_assign.vim:2535:63760/vim9-script":  "vim/E1069",
		"src/testdir/test_vim9_class.vim:270:7286/script":          "vim/E1069",
		"src/testdir/test_vim9_assign.vim:2323:58151/def":          "vim/E110",
		"src/testdir/test_vim9_assign.vim:2323:58151/vim9-script":  "vim/E110",
		"src/testdir/test_vim9_assign.vim:2328:58263/def":          "vim/E110",
		"src/testdir/test_vim9_assign.vim:2328:58263/vim9-script":  "vim/E110",
		"src/testdir/test_vim9_generics.vim:3048:67627/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:3057:67814/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:3066:68011/script":     "vim/E1069",
		"src/testdir/test_vim9_generics.vim:3075:68222/script":     "vim/E1069",
		"src/testdir/test_vim9_generics.vim:3084:68421/script":     "vim/E1008",
		"src/testdir/test_vim9_assign.vim:3039:74843/def":          "vim/E1202",
		"src/testdir/test_vim9_assign.vim:3039:74843/vim9-script":  "vim/E1202",
		"src/testdir/test_vim9_class.vim:409:10761/script":         "vim/E1202",
		"src/testdir/test_vim9_class.vim:1929:44717/script":        "vim/E1202",
		"src/testdir/test_vim9_generics.vim:280:6221/script":       "vim/E1554",
		"src/testdir/test_vim9_generics.vim:304:6695/script":       "vim/E116",
		"src/testdir/test_vim9_generics.vim:256:5703/script":       "vim/E1555",
		"src/testdir/test_vim9_generics.vim:650:14312/script":      "vim/E1555",
		"src/testdir/test_vim9_generics.vim:787:17195/script":      "vim/E1555",
		"src/testdir/test_vim9_generics.vim:969:21961/script":      "vim/E1555",
		"src/testdir/test_vim9_generics.vim:1139:25810/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:1214:27205/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:1298:28752/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:1479:32151/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:1557:33623/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:1692:36240/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:2919:64579/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:3030:67202/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:3536:79340/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:336:7374/script":       "vim/E1202",
		"src/testdir/test_vim9_generics.vim:357:7889/script":       "vim/E1202",
		"src/testdir/test_vim9_generics.vim:365:8067/script":       "vim/E1202",
		"src/testdir/test_vim9_generics.vim:397:8780/script":       "vim/E1202",
		"src/testdir/test_vim9_generics.vim:728:15637/script":      "vim/E1202",
		"src/testdir/test_vim9_generics.vim:1706:36532/script":     "vim/E1554",
		"src/testdir/test_vim9_generics.vim:1720:36821/script":     "vim/E1554",
		"src/testdir/test_vim9_generics.vim:1734:37119/script":     "vim/E1554",
		"src/testdir/test_vim9_class.vim:149:3907/script":          "vim/E1318",
		"src/testdir/test_vim9_class.vim:158:4194/script":          "vim/E1318",
		"src/testdir/test_vim9_class.vim:167:4472/script":          "vim/E1318",
		"src/testdir/test_vim9_class.vim:176:4699/script":          "vim/E1318",
		"src/testdir/test_vim9_class.vim:185:4917/script":          "vim/E1318",
		"src/testdir/test_vim9_class.vim:194:5136/script":          "vim/E1318",
		"src/testdir/test_vim9_class.vim:203:5373/script":          "vim/E1318",
		"src/testdir/test_vim9_class.vim:213:5611/script":          "vim/E1318",
		"src/testdir/test_vim9_class.vim:222:5896/script":          "vim/E1318",
		"src/testdir/test_vim9_class.vim:231:6195/script":          "vim/E1318",
		"src/testdir/test_vim9_class.vim:240:6485/script":          "vim/E1318",
		"src/testdir/test_vim9_class.vim:499:12885/script":         "vim/E1318",
		"src/testdir/test_vim9_class.vim:509:13117/script":         "vim/E1318",
		"src/testdir/test_vim9_class.vim:2134:48889/script":        "vim/E1318",
		"src/testdir/test_vim9_class.vim:5681:127073/script":       "vim/E1318",
		"src/testdir/test_vim9_assign.vim:1596:38991/def":          "vim/E354",
		"src/testdir/test_vim9_assign.vim:1596:38991/vim9-script":  "vim/E1066",
		"src/testdir/test_vim9_assign.vim:1597:39061/def":          "vim/E354",
		"src/testdir/test_vim9_assign.vim:1597:39061/vim9-script":  "vim/E1066",
		"src/testdir/test_vim9_assign.vim:1598:39131/def":          "vim/E354",
		"src/testdir/test_vim9_assign.vim:1598:39131/vim9-script":  "vim/E1066",
		"src/testdir/test_vim9_assign.vim:1599:39201/def":          "vim/E354",
		"src/testdir/test_vim9_assign.vim:1599:39201/vim9-script":  "vim/E1066",
		"src/testdir/test_vim9_assign.vim:1601:39289/def":          "vim/E354",
		"src/testdir/test_vim9_assign.vim:1601:39289/vim9-script":  "vim/E1066",
		"src/testdir/test_vim9_assign.vim:1603:39368/def":          "vim/E354",
		"src/testdir/test_vim9_assign.vim:1603:39368/vim9-script":  "vim/E1066",
		"src/testdir/test_vim9_assign.vim:1606:39447/def":          "vim/E1066",
		"src/testdir/test_vim9_assign.vim:1607:39494/def":          "vim/E1066",
		"src/testdir/test_vim9_assign.vim:1608:39543/script":       "vim/E1066",
		"src/testdir/test_expr.vim:250:7987/legacy":                "vim/E15",
		"src/testdir/test_expr.vim:250:7987/def":                   "vim/E1004",
		"src/testdir/test_expr.vim:250:7987/vim9-script":           "vim/E1004",
		"src/testdir/test_expr.vim:261:8317/def":                   "vim/E15",
		"src/testdir/test_expr.vim:261:8317/vim9-script":           "vim/E15",
		"src/testdir/test_expr.vim:264:8414/def":                   "vim/E15",
		"src/testdir/test_expr.vim:264:8414/vim9-script":           "vim/E15",
		"src/testdir/test_expr.vim:770:31551/legacy":               "vim/E15",
		"src/testdir/test_expr.vim:770:31551/def":                  "vim/E15",
		"src/testdir/test_expr.vim:770:31551/vim9-script":          "vim/E15",
		"src/testdir/test_expr.vim:771:31625/legacy":               "vim/E15",
		"src/testdir/test_expr.vim:771:31625/def":                  "vim/E15",
		"src/testdir/test_expr.vim:771:31625/vim9-script":          "vim/E15",
		"src/testdir/test_expr.vim:772:31701/legacy":               "vim/E15",
		"src/testdir/test_expr.vim:772:31701/def":                  "vim/E15",
		"src/testdir/test_expr.vim:772:31701/vim9-script":          "vim/E15",
		"src/testdir/test_expr.vim:773:31760/legacy":               "vim/E15",
		"src/testdir/test_expr.vim:773:31760/def":                  "vim/E15",
		"src/testdir/test_expr.vim:773:31760/vim9-script":          "vim/E15",
		"src/testdir/test_expr.vim:774:31836/legacy":               "vim/E15",
		"src/testdir/test_expr.vim:774:31836/def":                  "vim/E15",
		"src/testdir/test_expr.vim:774:31836/vim9-script":          "vim/E15",
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
		"src/testdir/test_vim9_assign.vim:515:12614/def":           "vim/E1097",
		"src/testdir/test_vim9_assign.vim:73:1858/script":          "vim/E1124",
		"src/testdir/test_vim9_assign.vim:2239:56425/script":       "vim/E1125",
		"src/testdir/test_vim9_assign.vim:2303:57696/script":       "vim/E1021",
		"src/testdir/test_vim9_assign.vim:2309:57801/script":       "vim/E488",
		"src/testdir/test_vim9_assign.vim:2313:57945/def":          "vim/E1021",
		"src/testdir/test_vim9_assign.vim:1571:37884/def":          "vim/E1080",
		"src/testdir/test_vim9_assign.vim:2311:57842/def":          "vim/E1087",
		"src/testdir/test_vim9_assign.vim:2312:57894/def":          "vim/E1087",
		"src/testdir/test_vim9_assign.vim:2923:72274/def":          "vim/E488",
		"src/testdir/test_vim9_assign.vim:2923:72274/vim9-script":  "vim/E488",
		"src/testdir/test_vim9_assign.vim:198:4982/def":            "vim/E1022",
		"src/testdir/test_vim9_assign.vim:1587:38586/def":          "vim/E1022",
		"src/testdir/test_vim9_assign.vim:2052:52384/def":          "vim/E488",
		"src/testdir/test_vim9_assign.vim:2053:52451/def":          "vim/E488",
		"src/testdir/test_vim9_class.vim:1210:27906/script":        "vim/E1022",
		"src/testdir/test_vim9_class.vim:8645:196853/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:8655:197097/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:8665:197341/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:8684:197828/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:8694:198071/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:8979:205114/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:8989:205358/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:8999:205602/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:9018:206089/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:9028:206332/script":       "vim/E1022",
		"src/testdir/test_vim9_assign.vim:2633:66108/def":          "vim/E1126",
		"src/testdir/test_vim9_assign.vim:2633:66108/vim9-script":  "vim/E1126",
		"src/testdir/test_vim9_assign.vim:581:14074/def":           "vim/E1097",
		"src/testdir/test_vim9_assign.vim:1202:28637/def":          "vim/E488",
		"src/testdir/test_vim9_assign.vim:1632:40950/def":          "vim/E1068",
		"src/testdir/test_vim9_assign.vim:1633:41010/def":          "vim/E1009",
		"src/testdir/test_vim9_assign.vim:1636:41143/def":          "vim/E1009",
		"src/testdir/test_vim9_assign.vim:1551:36793/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1552:36842/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1553:36892/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1555:36943/script":       "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1557:37089/script":       "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1558:37156/script":       "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1559:37223/script":       "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1561:37400/script":       "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2063:52656/script":       "vim/E1145",
		"src/testdir/test_vim9_assign.vim:2080:52938/script":       "vim/E1145",
		"src/testdir/test_vim9_assign.vim:2096:53307/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2096:53307/vim9-script":  "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2102:53491/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2102:53491/vim9-script":  "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2108:53673/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2108:53673/vim9-script":  "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2318:58046/def":          "vim/E1065",
		"src/testdir/test_vim9_assign.vim:2318:58046/vim9-script":  "vim/E1065",
		"src/testdir/test_vim9_class.vim:11074:250574/script":      "vim/E1356",
		"src/testdir/test_vim9_class.vim:434:11328/script":         "vim/E15",
		"src/testdir/test_vim9_class.vim:10103:230103/script":      "vim/E1429",
		"src/testdir/test_vim9_interface.vim:1416:30780/script":    "vim/E1436",
		"src/testdir/test_vim9_assign.vim:2708:67708/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2712:67795/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2716:67883/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:3123:76907/script":       "vim/E488",
		"src/testdir/test_vim9_class.vim:11721:265332/def":         "vim/E1008",
		"src/testdir/test_vim9_class.vim:11721:265332/vim9-script": "vim/E1008",
		"src/testdir/test_vim9_class.vim:11726:265505/def":         "vim/E1068",
		"src/testdir/test_vim9_class.vim:11726:265505/vim9-script": "vim/E1068",
		"src/testdir/test_vim9_class.vim:11731:265708/def":         "vim/E1009",
		"src/testdir/test_vim9_class.vim:11731:265708/vim9-script": "vim/E1009",
		"src/testdir/test_vim9_class.vim:11736:265882/def":         "vim/E1009",
		"src/testdir/test_vim9_class.vim:11742:266028/script":      "vim/E488",
		"src/testdir/test_vim9_class.vim:11:199/script":            "vim/E1316",
		"src/testdir/test_vim9_class.vim:27:664/script":            "vim/E1315",
		"src/testdir/test_vim9_class.vim:1482:34515/script":        "vim/E1368",
		"src/testdir/test_vim9_class.vim:3611:79232/script":        "vim/E1365",
		"src/testdir/test_vim9_class.vim:3629:79631/script":        "vim/E1365",
		"src/testdir/test_vim9_class.vim:5594:125030/script":       "vim/E1371",
		"src/testdir/test_vim9_class.vim:5603:125259/script":       "vim/E1371",
		"src/testdir/test_vim9_class.vim:5937:132470/script":       "vim/E1368",
		"src/testdir/test_vim9_class.vim:5947:132721/script":       "vim/E1368",
		"src/testdir/test_vim9_class.vim:1907:44214/script":        "vim/E1004",
		"src/testdir/test_vim9_class.vim:1914:44393/script":        "vim/E1004",
		"src/testdir/test_vim9_class.vim:2485:56344/script":        "vim/E1315",
		"src/testdir/test_vim9_class.vim:279:7538/script":          "vim/E1317",
		"src/testdir/test_vim9_class.vim:288:7810/script":          "vim/E1317",
		"src/testdir/test_vim9_class.vim:297:8073/script":          "vim/E1317",
		"src/testdir/test_vim9_class.vim:307:8307/script":          "vim/E1170",
		"src/testdir/test_vim9_class.vim:2848:63764/script":        "vim/E1127",
		"src/testdir/test_vim9_class.vim:5967:133218/script":       "vim/E1329",
		"src/testdir/test_vim9_class.vim:52:1401/script":           "vim/E1065",
		"src/testdir/test_vim9_class.vim:60:1626/script":           "vim/E1065",
		"src/testdir/test_vim9_class.vim:84:2250/script":           "vim/E488",
		"src/testdir/test_vim9_class.vim:112:2975/script":          "vim/E488",
		"src/testdir/test_vim9_class.vim:68:1833/script":           "vim/E488",
		"src/testdir/test_vim9_class.vim:76:2041/script":           "vim/E488",
		"src/testdir/test_vim9_class.vim:422:11065/script":         "vim/E15",
		"src/testdir/test_vim9_class.vim:1379:31866/script":        "vim/E1065",
		"src/testdir/test_vim9_class.vim:1473:34263/script":        "vim/E1065",
		"src/testdir/test_vim9_class.vim:2955:66130/script":        "vim/E1316",
		"src/testdir/test_vim9_class.vim:2964:66432/script":        "vim/E1065",
		"src/testdir/test_vim9_class.vim:2972:66662/script":        "vim/E488",
		"src/testdir/test_vim9_class.vim:5957:132958/script":       "vim/E1368",
		"src/testdir/test_vim9_class.vim:5977:133493/script":       "vim/E1329",
		"src/testdir/test_vim9_class.vim:5987:133759/script":       "vim/E1329",
		"src/testdir/test_vim9_interface.vim:553:12767/script":     "vim/E1389",
		"src/testdir/test_vim9_interface.vim:29:581/script":        "vim/E1342",
		"src/testdir/test_vim9_interface.vim:308:7096/script":      "vim/E1315",
		"src/testdir/test_vim9_interface.vim:361:8375/script":      "vim/E1315",
		"src/testdir/test_vim9_interface.vim:535:12343/script":     "vim/E1315",
		"src/testdir/test_vim9_interface.vim:545:12575/script":     "vim/E1389",
		"src/testdir/test_vim9_interface.vim:59:1387/script":       "vim/E1344",
		"src/testdir/test_vim9_interface.vim:79:1888/script":       "vim/E1065",
		"src/testdir/test_vim9_interface.vim:316:7304/script":      "vim/E488",
		"src/testdir/test_vim9_interface.vim:87:2103/script":       "vim/E1065",
		"src/testdir/test_vim9_interface.vim:95:2332/script":       "vim/E1065",
		"src/testdir/test_vim9_interface.vim:103:2569/script":      "vim/E488",
		"src/testdir/test_vim9_interface.vim:111:2790/script":      "vim/E488",
		"src/testdir/test_vim9_interface.vim:324:7508/script":      "vim/E488",
		"src/testdir/test_vim9_enum.vim:108:2615/script":           "vim/E488",
		"src/testdir/test_vim9_enum.vim:60:1409/script":            "vim/E1065",
		"src/testdir/test_vim9_enum.vim:68:1610/script":            "vim/E1065",
		"src/testdir/test_vim9_enum.vim:76:1834/script":            "vim/E1065",
		"src/testdir/test_vim9_enum.vim:132:3226/script":           "vim/E488",
		"src/testdir/test_vim9_enum.vim:28:569/script":             "vim/E1415",
		"src/testdir/test_vim9_enum.vim:36:799/script":             "vim/E1315",
		"src/testdir/test_vim9_enum.vim:298:6850/script":           "vim/E1418",
		"src/testdir/test_vim9_enum.vim:288:6615/script":           "vim/E1418",
		"src/testdir/test_vim9_interface.vim:1098:23808/script":    "vim/E1315",
		"src/testdir/test_trycatch.vim:2019:44596/script":          "vim/E690",
		"src/testdir/test_trycatch.vim:2029:44739/script":          "vim/E690",
		"src/testdir/test_vim9_cmd.vim:267:6525/def":               "vim/E1083",
		"src/testdir/test_vim9_cmd.vim:1202:25888/script":          "vim/E1050",
		"src/testdir/test_vim9_cmd.vim:1227:26455/def":             "vim/E1176",
		"src/testdir/test_vim9_cmd.vim:1227:26455/vim9-script":     "vim/E1176",
		"src/testdir/test_vim9_cmd.vim:1233:26572/def":             "vim/E1176",
		"src/testdir/test_vim9_cmd.vim:1240:26731/def":             "vim/E1176",
		"src/testdir/test_vim9_cmd.vim:1249:26910/def":             "vim/E1176",
		"src/testdir/test_vim9_cmd.vim:1249:26910/vim9-script":     "vim/E1176",
		"src/testdir/test_vim9_cmd.vim:1256:27030/def":             "vim/E1176",
		"src/testdir/test_vim9_cmd.vim:1256:27030/vim9-script":     "vim/E1176",
		"src/testdir/test_vim9_cmd.vim:1263:27152/def":             "vim/E1176",
		"src/testdir/test_vim9_cmd.vim:1263:27152/vim9-script":     "vim/E1176",
		"src/testdir/test_vim9_cmd.vim:1270:27274/def":             "vim/E1176",
		"src/testdir/test_vim9_cmd.vim:1270:27274/vim9-script":     "vim/E1176",
		"src/testdir/test_vim9_cmd.vim:1275:27368/def":             "vim/E1082",
		"src/testdir/test_vim9_cmd.vim:1275:27368/vim9-script":     "vim/E1082",
		"src/testdir/test_vim9_cmd.vim:1280:27472/def":             "vim/E1082",
		"src/testdir/test_vim9_cmd.vim:1280:27472/vim9-script":     "vim/E1082",
		"src/testdir/test_vim9_cmd.vim:1990:41128/def":             "vim/E1185",
		"src/testdir/test_vim9_cmd.vim:2081:43043/def":             "vim/E1241",
		"src/testdir/test_vim9_cmd.vim:2081:43043/vim9-script":     "vim/E1241",
		"src/testdir/test_vim9_cmd.vim:2107:43740/def":             "vim/E1242",
		"src/testdir/test_vim9_cmd.vim:2107:43740/vim9-script":     "vim/E1242",
		"src/testdir/test_vim9_cmd.vim:2111:43834/def":             "vim/E1242",
		"src/testdir/test_vim9_cmd.vim:2111:43834/vim9-script":     "vim/E1242",
		"src/testdir/test_vim9_cmd.vim:2128:44136/def":             "vim/E1242",
		"src/testdir/test_vim9_cmd.vim:2128:44136/vim9-script":     "vim/E1242",
		"src/testdir/test_vim9_cmd.vim:2132:44231/def":             "vim/E1242",
		"src/testdir/test_vim9_cmd.vim:2132:44231/vim9-script":     "vim/E1242",
		"src/testdir/test_vim9_cmd.vim:2062:42490/def":             "vim/E1241",
		"src/testdir/test_vim9_cmd.vim:2062:42490/vim9-script":     "vim/E1241",
		"src/testdir/test_vim9_generics.vim:22:376/script":         "vim/E1552",
		"src/testdir/test_vim9_generics.vim:30:565/script":         "vim/E1555",
		"src/testdir/test_vim9_generics.vim:38:743/script":         "vim/E1008",
		"src/testdir/test_vim9_generics.vim:46:898/script":         "vim/E1008",
		"src/testdir/test_vim9_generics.vim:54:1052/script":        "vim/E1553",
		"src/testdir/test_vim9_generics.vim:62:1232/script":        "vim/E1008",
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
		"src/testdir/test_vim9_generics.vim:1191:26790/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:1202:27001/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:1457:31742/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:1468:31960/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:1971:42622/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:1982:42856/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:2584:56941/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:2594:57148/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:2605:57388/script":     "vim/E1553",
		"src/testdir/test_vim9_generics.vim:2615:57599/script":     "vim/E1552",
		"src/testdir/test_vim9_generics.vim:2626:57847/script":     "vim/E1561",
		"src/testdir/test_vim9_generics.vim:2964:65613/script":     "vim/E1069",
		"src/testdir/test_vim9_generics.vim:2973:65815/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:3546:79564/script":     "vim/E1555",
		"src/testdir/test_vim9_import.vim:531:14329/script":        "vim/E1047",
		"src/testdir/test_vim9_import.vim:536:14451/script":        "vim/E1047",
		"src/testdir/test_vim9_import.vim:541:14571/script":        "vim/E1047",
		"src/testdir/test_vim9_import.vim:2972:73136/script":       "vim/E475",
		"src/testdir/test_vim9_class.vim:35:883/script":            "vim/E475",
		"src/testdir/test_vim9_class.vim:43:1104/script":           "vim/E475",
		"src/testdir/test_vim9_import.vim:2978:73280/script":       "vim/E983",
		"src/testdir/test_vim9_import.vim:2984:73418/script":       "vim/E475",
		"src/testdir/test_vim9_func.vim:398:8465/script":           "vim/E1065",
		"src/testdir/test_vim9_func.vim:426:8986/script":           "vim/E1068",
		"src/testdir/test_vim9_func.vim:434:9120/script":           "vim/E1068",
		"src/testdir/test_vim9_func.vim:441:9237/script":           "vim/E1068",
		"src/testdir/test_vim9_func.vim:948:20577/script":          "vim/E125",
		"src/testdir/test_vim9_func.vim:955:20687/script":          "vim/E125",
		"src/testdir/test_vim9_func.vim:971:20965/script":          "vim/E488",
		"src/testdir/test_vim9_func.vim:1157:24812/script":         "vim/E1069",
		"src/testdir/test_vim9_func.vim:1689:36926/def":            "vim/E1157",
		"src/testdir/test_vim9_func.vim:1689:36926/vim9-script":    "vim/E1157",
		"src/testdir/test_vim9_func.vim:2078:45879/def":            "vim/E1007",
		"src/testdir/test_vim9_func.vim:2086:46156/def":            "vim/E110",
		"src/testdir/test_vim9_func.vim:2087:46235/def":            "vim/E110",
		"src/testdir/test_vim9_func.vim:2441:54782/script":         "vim/E1069",
		"src/testdir/test_vim9_func.vim:2448:54929/script":         "vim/E1059",
		"src/testdir/test_vim9_func.vim:2455:55077/script":         "vim/E1059",
		"src/testdir/test_vim9_func.vim:2462:55226/script":         "vim/E1008",
		"src/testdir/test_vim9_func.vim:2464:55349/script":         "vim/E1008",
		"src/testdir/test_vim9_func.vim:2481:55796/script":         "vim/E1008",
		"src/testdir/test_vim9_func.vim:2482:55906/script":         "vim/E1055",
		"src/testdir/test_vim9_func.vim:2483:56005/script":         "vim/E1069",
		"src/testdir/test_vim9_func.vim:2485:56148/script":         "vim/E1069",
		"src/testdir/test_vim9_func.vim:2495:56457/script":         "vim/E1068",
		"src/testdir/test_vim9_func.vim:2505:56703/script":         "vim/E1069",
		"src/testdir/test_vim9_func.vim:2885:66191/def":            "vim/E1068",
		"src/testdir/test_vim9_func.vim:2886:66262/def":            "vim/E1069",
		"src/testdir/test_vim9_func.vim:2887:66332/def":            "vim/E1005",
		"src/testdir/test_vim9_func.vim:2888:66507/def":            "vim/E1069",
		"src/testdir/test_vim9_func.vim:3746:85903/script":         "vim/E488",
		"src/testdir/test_vim9_expr.vim:4480:129784/def":           "vim/E274",
		"src/testdir/test_vim9_expr.vim:4480:129784/vim9-script":   "vim/E274",
		"src/testdir/test_vim9_expr.vim:4489:130127/def":           "vim/E1069",
		"src/testdir/test_vim9_expr.vim:4489:130127/vim9-script":   "vim/E1069",
		"src/testdir/test_vim9_enum.vim:151:3717/script":           "vim/E1123",
		"src/testdir/test_vim9_enum.vim:12:230/script":             "vim/E1414",
		"src/testdir/test_vim9_enum.vim:161:3943/script":           "vim/E1123",
		"src/testdir/test_vim9_enum.vim:172:4190/script":           "vim/E1123",
		"src/testdir/test_vim9_enum.vim:182:4421/script":           "vim/E1123",
		"src/testdir/test_vim9_enum.vim:194:4637/script":           "vim/E1123",
		"src/testdir/test_vim9_enum.vim:204:4829/script":           "vim/E1123",
		"src/testdir/test_vim9_enum.vim:252:5905/script":           "vim/E1123",
		"src/testdir/test_vim9_enum.vim:378:8621/script":           "vim/E1123",
		"src/testdir/test_vim9_enum.vim:1527:34659/script":         "vim/E1068",
		"src/testdir/test_vim9_enum.vim:1541:34927/script":         "vim/E1068",
		"src/testdir/test_vim9_enum.vim:233:5465/script":           "vim/E1068",
		"src/testdir/test_vim9_enum.vim:242:5659/script":           "vim/E1069",
		"src/testdir/test_vim9_typealias.vim:55:1659/script":       "vim/E1065",
		"src/testdir/test_vim9_typealias.vim:62:1839/script":       "vim/E1397",
		"src/testdir/test_vim9_typealias.vim:76:2174/script":       "vim/E1398",
		"src/testdir/test_vim9_script.vim:195:4394/def":            "vim/E1009",
		"src/testdir/test_vim9_script.vim:196:4452/def":            "vim/E1009",
		"src/testdir/test_vim9_script.vim:271:6623/def":            "vim/E1125",
		"src/testdir/test_vim9_script.vim:365:9084/def":            "vim/E1050",
		"src/testdir/test_vim9_script.vim:366:9128/def":            "vim/E1050",
		"src/testdir/test_vim9_script.vim:367:9173/def":            "vim/E1050",
		"src/testdir/test_vim9_script.vim:368:9218/def":            "vim/E1050",
		"src/testdir/test_vim9_script.vim:1810:37557/script":       "vim/E1039",
		"src/testdir/test_vim9_script.vim:1811:37626/script":       "vim/E1040",
		"src/testdir/test_vim9_script.vim:2043:42762/script":       "vim/E1100",
		"src/testdir/test_vim9_script.vim:2045:42875/script":       "vim/E1100",
		"src/testdir/test_vim9_script.vim:2047:42988/script":       "vim/E1100",
		"src/testdir/test_vim9_script.vim:2049:43102/script":       "vim/E1100",
		"src/testdir/test_vim9_script.vim:2051:43216/script":       "vim/E1100",
		"src/testdir/test_vim9_script.vim:2053:43330/script":       "vim/E1100",
		"src/testdir/test_vim9_script.vim:2055:43444/script":       "vim/E1100",
		"src/testdir/test_vim9_script.vim:2320:47969/def":          "vim/E114",
		"src/testdir/test_vim9_script.vim:2321:48021/def":          "vim/E115",
		"src/testdir/test_vim9_script.vim:2322:48073/def":          "vim/E110",
		"src/testdir/test_vim9_script.vim:2323:48121/def":          "vim/E109",
		"src/testdir/test_vim9_script.vim:1532:31072/def":          "vim/E603",
		"src/testdir/test_vim9_script.vim:1535:31245/def":          "vim/E606",
		"src/testdir/test_vim9_script.vim:1536:31288/def":          "vim/E607",
		"src/testdir/test_vim9_script.vim:1537:31369/def":          "vim/E602",
		"src/testdir/test_vim9_script.vim:3067:64156/vim9-script":  "vim/E690",
		"src/testdir/test_vim9_script.vim:3068:64217/vim9-script":  "vim/E690",
		"src/testdir/test_vim9_script.vim:3070:64343/vim9-script":  "vim/E690",
		"src/testdir/test_vim9_script.vim:3071:64405/def":          "vim/E690",
		"src/testdir/test_vim9_script.vim:3071:64405/vim9-script":  "vim/E690",
		"src/testdir/test_vim9_script.vim:3107:65719/def":          "vim/E1059",
		"src/testdir/test_vim9_script.vim:3107:65719/vim9-script":  "vim/E1059",
		"src/testdir/test_vim9_script.vim:2382:49493/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:2437:50783/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:2459:51306/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:3579:76110/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:3599:76552/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:3635:77305/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:3686:78303/script":       "vim/E416",
		"src/testdir/test_vim9_script.vim:3694:78489/script":       "vim/E413",
		"src/testdir/test_vim9_script.vim:3705:78798/script":       "vim/E416",
		"src/testdir/test_vim9_script.vim:3778:80424/script":       "vim/E475",
		"src/testdir/test_vim9_script.vim:3787:80639/script":       "vim/E789",
		"src/testdir/test_vim9_script.vim:3796:80846/script":       "vim/E402",
		"src/testdir/test_vim9_script.vim:3805:81070/script":       "vim/E475",
		"src/testdir/test_vim9_script.vim:3809:81195/script":       "vim/E406",
		"src/testdir/test_vim9_script.vim:3813:81312/script":       "vim/E475",
		"src/testdir/test_vim9_script.vim:3822:81544/script":       "vim/E402",
		"src/testdir/test_vim9_script.vim:3831:81754/script":       "vim/E404",
		"src/testdir/test_vim9_script.vim:3839:81943/script":       "vim/E404",
		"src/testdir/test_vim9_script.vim:3848:82155/script":       "vim/E475",
		"src/testdir/test_vim9_script.vim:3926:83957/script":       "vim/E488",
		"src/testdir/test_vim9_script.vim:3937:84202/script":       "vim/E488",
		"src/testdir/test_vim9_script.vim:3942:84304/script":       "vim/E488",
		"src/testdir/test_vim9_script.vim:3946:84394/script":       "vim/E488",
		"src/testdir/test_vim9_script.vim:3967:84795/script":       "vim/E488",
		"src/testdir/test_vim9_script.vim:3991:85235/script":       "vim/E399",
		"src/testdir/test_vim9_script.vim:4091:87356/script":       "vim/E488",
		"src/testdir/test_vim9_script.vim:4098:87482/script":       "vim/E488",
		"src/testdir/test_vim9_script.vim:4112:87742/script":       "vim/E488",
		"src/testdir/test_vim9_script.vim:4126:88011/script":       "vim/E488",
		"src/testdir/test_vim9_script.vim:4846:108652/def":         "vim/E1100",
		"src/testdir/test_vim9_script.vim:4846:108652/vim9-script": "vim/E1100",
		"src/testdir/test_vim9_script.vim:4856:108819/def":         "vim/E1100",
		"src/testdir/test_vim9_script.vim:4856:108819/vim9-script": "vim/E1100",
		"src/testdir/test_vim9_script.vim:4861:108900/def":         "vim/E1100",
		"src/testdir/test_vim9_script.vim:4861:108900/vim9-script": "vim/E1100",
		"src/testdir/test_vim9_script.vim:4866:108981/def":         "vim/E1100",
		"src/testdir/test_vim9_script.vim:4866:108981/vim9-script": "vim/E1100",
		"src/testdir/test_vim9_script.vim:4871:109064/def":         "vim/E1100",
		"src/testdir/test_vim9_script.vim:4871:109064/vim9-script": "vim/E1100",
		"src/testdir/test_vim9_script.vim:4851:108739/def":         "vim/E481",
		"src/testdir/test_vim9_script.vim:4851:108739/vim9-script": "vim/E481",
		"src/testdir/test_vim9_script.vim:4934:110489/def":         "vim/E1205",
		"src/testdir/test_vim9_script.vim:4934:110489/vim9-script": "vim/E1205",
		"src/testdir/test_vim9_script.vim:4939:110638/def":         "vim/E1205",
		"src/testdir/test_vim9_script.vim:4939:110638/vim9-script": "vim/E1205",
		"src/testdir/test_vim9_script.vim:4944:110788/def":         "vim/E1205",
		"src/testdir/test_vim9_script.vim:4944:110788/vim9-script": "vim/E1205",
		"src/testdir/test_vim9_script.vim:4949:110938/def":         "vim/E1205",
		"src/testdir/test_vim9_script.vim:4949:110938/vim9-script": "vim/E1205",
		"src/testdir/test_vim9_script.vim:4990:111799/def":         "vim/E1205",
		"src/testdir/test_vim9_script.vim:4990:111799/vim9-script": "vim/E1205",
		"src/testdir/test_vim9_script.vim:4995:111946/def":         "vim/E1205",
		"src/testdir/test_vim9_script.vim:4995:111946/vim9-script": "vim/E1205",
		"src/testdir/test_vim9_expr.vim:111:2965/def":              "vim/E1004",
		"src/testdir/test_vim9_expr.vim:111:2965/vim9-script":      "vim/E1004",
		"src/testdir/test_vim9_interface.vim:71:1672/script":       "vim/E1345",
		"src/testdir/test_vim9_expr.vim:198:5694/def":              "vim/E1097",
		"src/testdir/test_vim9_expr.vim:199:5759/script":           "vim/E15",
		"src/testdir/test_vim9_expr.vim:116:3128/def":              "vim/E1004",
		"src/testdir/test_vim9_expr.vim:116:3128/vim9-script":      "vim/E1004",
		"src/testdir/test_vim9_expr.vim:121:3237/def":              "vim/E1004",
		"src/testdir/test_vim9_expr.vim:121:3237/vim9-script":      "vim/E1004",
		"src/testdir/test_vim9_expr.vim:126:3346/def":              "vim/E1004",
		"src/testdir/test_vim9_expr.vim:126:3346/vim9-script":      "vim/E1004",
		"src/testdir/test_vim9_expr.vim:131:3508/def":              "vim/E1004",
		"src/testdir/test_vim9_expr.vim:131:3508/vim9-script":      "vim/E1004",
		"src/testdir/test_vim9_expr.vim:169:4320/def":              "vim/E109",
		"src/testdir/test_vim9_expr.vim:169:4320/vim9-script":      "vim/E109",
		"src/testdir/test_vim9_expr.vim:172:4463/def":              "vim/E1004",
		"src/testdir/test_vim9_expr.vim:172:4463/vim9-script":      "vim/E1004",
		"src/testdir/test_vim9_expr.vim:173:4536/def":              "vim/E1004",
		"src/testdir/test_vim9_expr.vim:173:4536/vim9-script":      "vim/E1004",
		"src/testdir/test_vim9_expr.vim:174:4609/def":              "vim/E1004",
		"src/testdir/test_vim9_expr.vim:174:4609/vim9-script":      "vim/E1004",
		"src/testdir/test_vim9_expr.vim:180:4761/def":              "vim/E1004",
		"src/testdir/test_vim9_expr.vim:180:4761/vim9-script":      "vim/E1004",
		"src/testdir/test_vim9_expr.vim:183:4941/def":              "vim/E1004",
		"src/testdir/test_vim9_expr.vim:183:4941/vim9-script":      "vim/E1004",
		"src/testdir/test_vim9_expr.vim:184:5014/def":              "vim/E1004",
		"src/testdir/test_vim9_expr.vim:184:5014/vim9-script":      "vim/E1004",
		"src/testdir/test_vim9_expr.vim:185:5087/def":              "vim/E1004",
		"src/testdir/test_vim9_expr.vim:185:5087/vim9-script":      "vim/E1004",
		"src/testdir/test_vim9_expr.vim:191:5249/def":              "vim/E1004",
		"src/testdir/test_vim9_expr.vim:191:5249/vim9-script":      "vim/E1004",
		"src/testdir/test_vim9_expr.vim:200:5835/def":              "vim/E1097",
		"src/testdir/test_vim9_expr.vim:201:5908/script":           "vim/E15",
		"src/testdir/test_vim9_expr.vim:520:14502/def":             "vim/E1004",
		"src/testdir/test_vim9_expr.vim:520:14502/vim9-script":     "vim/E1004",
		"src/testdir/test_vim9_expr.vim:661:17908/def":             "vim/E1004",
		"src/testdir/test_vim9_expr.vim:661:17908/vim9-script":     "vim/E1004",
		"src/testdir/test_vim9_expr.vim:666:18082/def":             "vim/E1004",
		"src/testdir/test_vim9_expr.vim:666:18082/vim9-script":     "vim/E1004",
		"src/testdir/test_vim9_expr.vim:671:18193/def":             "vim/E1004",
		"src/testdir/test_vim9_expr.vim:671:18193/vim9-script":     "vim/E1004",
		"src/testdir/test_vim9_expr.vim:680:18502/def":             "vim/E1097",
		"src/testdir/test_vim9_expr.vim:681:18559/script":          "vim/E15",
		"src/testdir/test_vim9_expr.vim:705:19629/script":          "vim/E1004",
		"src/testdir/test_vim9_expr.vim:804:21948/def":             "vim/E1004",
		"src/testdir/test_vim9_expr.vim:804:21948/vim9-script":     "vim/E1004",
		"src/testdir/test_vim9_expr.vim:809:22059/def":             "vim/E1004",
		"src/testdir/test_vim9_expr.vim:809:22059/vim9-script":     "vim/E1004",
		"src/testdir/test_vim9_expr.vim:814:22229/def":             "vim/E1004",
		"src/testdir/test_vim9_expr.vim:814:22229/vim9-script":     "vim/E1004",
		"src/testdir/test_vim9_expr.vim:827:22606/def":             "vim/E1004",
		"src/testdir/test_vim9_expr.vim:827:22606/vim9-script":     "vim/E1004",
		"src/testdir/test_vim9_expr.vim:984:27804/def":             "vim/E1097",
		"src/testdir/test_vim9_expr.vim:985:27859/script":          "vim/E15",
		"src/testdir/test_vim9_expr.vim:1654:46916/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1654:46916/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1659:47060/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1659:47060/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1664:47152/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1664:47152/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1669:47244/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1669:47244/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1674:47337/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1674:47337/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1679:47484/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1679:47484/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1701:48086/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1701:48086/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1702:48146/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1702:48146/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1703:48207/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1703:48207/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1706:48326/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1706:48326/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1707:48387/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1707:48387/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1708:48449/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1708:48449/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1711:48569/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1711:48569/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1712:48634/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1712:48634/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1713:48700/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1713:48700/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1716:48827/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1716:48827/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1717:48895/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1717:48895/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1718:48964/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1718:48964/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1720:49034/def":            "vim/E15",
		"src/testdir/test_vim9_expr.vim:1720:49034/vim9-script":    "vim/E15",
		"src/testdir/test_vim9_expr.vim:1721:49101/def":            "vim/E15",
		"src/testdir/test_vim9_expr.vim:1721:49101/vim9-script":    "vim/E15",
		"src/testdir/test_vim9_expr.vim:1722:49168/def":            "vim/E15",
		"src/testdir/test_vim9_expr.vim:1722:49168/vim9-script":    "vim/E15",
		"src/testdir/test_vim9_expr.vim:1723:49238/def":            "vim/E15",
		"src/testdir/test_vim9_expr.vim:1723:49238/vim9-script":    "vim/E15",
		"src/testdir/test_vim9_expr.vim:1874:55553/def":            "vim/E15",
		"src/testdir/test_vim9_expr.vim:1874:55553/vim9-script":    "vim/E15",
		"src/testdir/test_vim9_expr.vim:1894:55984/def":            "vim/E15",
		"src/testdir/test_vim9_expr.vim:1894:55984/vim9-script":    "vim/E15",
		"src/testdir/test_vim9_expr.vim:1899:56090/def":            "vim/E15",
		"src/testdir/test_vim9_expr.vim:1899:56090/vim9-script":    "vim/E15",
		"src/testdir/test_vim9_expr.vim:1904:56196/def":            "vim/E15",
		"src/testdir/test_vim9_expr.vim:1904:56196/vim9-script":    "vim/E15",
		"src/testdir/test_vim9_expr.vim:1910:56309/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1910:56309/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1914:56402/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1914:56402/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1919:56496/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1919:56496/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1924:56594/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1924:56594/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1929:56751/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1929:56751/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1934:56850/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1934:56850/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1941:57034/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1941:57034/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1977:57863/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1977:57863/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2141:63873/def":            "vim/E1097",
		"src/testdir/test_vim9_expr.vim:2142:63925/script":         "vim/E15",
		"src/testdir/test_vim9_expr.vim:2209:65308/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2209:65308/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2214:65402/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2214:65402/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2219:65496/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2219:65496/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2343:69743/def":            "vim/E1097",
		"src/testdir/test_vim9_expr.vim:2345:69864/script":         "vim/E15",
		"src/testdir/test_vim9_expr.vim:2346:69935/def":            "vim/E1068",
		"src/testdir/test_vim9_expr.vim:2346:69935/vim9-script":    "vim/E1068",
		"src/testdir/test_vim9_expr.vim:2347:70004/def":            "vim/E1104",
		"src/testdir/test_vim9_expr.vim:2347:70004/vim9-script":    "vim/E1104",
		"src/testdir/test_vim9_expr.vim:2445:72344/def":            "vim/E973",
		"src/testdir/test_vim9_expr.vim:2445:72344/vim9-script":    "vim/E973",
		"src/testdir/test_vim9_expr.vim:2462:72918/def":            "vim/E114",
		"src/testdir/test_vim9_expr.vim:2462:72918/vim9-script":    "vim/E114",
		"src/testdir/test_vim9_expr.vim:2463:72978/def":            "vim/E115",
		"src/testdir/test_vim9_expr.vim:2463:72978/vim9-script":    "vim/E115",
		"src/testdir/test_vim9_expr.vim:2464:73038/def":            "vim/E115",
		"src/testdir/test_vim9_expr.vim:2473:73256/def":            "vim/E114",
		"src/testdir/test_vim9_expr.vim:2473:73256/vim9-script":    "vim/E114",
		"src/testdir/test_vim9_expr.vim:2594:77366/def":            "vim/E1069",
		"src/testdir/test_vim9_expr.vim:2594:77366/vim9-script":    "vim/E1069",
		"src/testdir/test_vim9_expr.vim:2595:77430/def":            "vim/E1068",
		"src/testdir/test_vim9_expr.vim:2595:77430/vim9-script":    "vim/E1068",
		"src/testdir/test_vim9_expr.vim:2600:77674/def":            "vim/E1097",
		"src/testdir/test_vim9_expr.vim:2601:77735/script":         "vim/E15",
		"src/testdir/test_vim9_expr.vim:2602:77811/def":            "vim/E1097",
		"src/testdir/test_vim9_expr.vim:2603:77873/script":         "vim/E111",
		"src/testdir/test_vim9_expr.vim:2617:78496/def":            "vim/E1127",
		"src/testdir/test_vim9_expr.vim:2617:78496/vim9-script":    "vim/E1127",
		"src/testdir/test_vim9_expr.vim:2624:78636/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2624:78636/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2626:78769/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2626:78769/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2627:78844/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2627:78844/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2660:79392/def":            "vim/E1069",
		"src/testdir/test_vim9_expr.vim:2660:79392/vim9-script":    "vim/E1069",
		"src/testdir/test_vim9_expr.vim:2665:79494/def":            "vim/E1068",
		"src/testdir/test_vim9_expr.vim:2665:79494/vim9-script":    "vim/E1068",
		"src/testdir/test_vim9_expr.vim:2772:82207/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2772:82207/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2773:82273/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2773:82273/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2774:82399/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2774:82399/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2779:82692/def":            "vim/E1069",
		"src/testdir/test_vim9_expr.vim:2779:82692/vim9-script":    "vim/E1069",
		"src/testdir/test_vim9_expr.vim:2790:83244/def":            "vim/E722",
		"src/testdir/test_vim9_expr.vim:2790:83244/vim9-script":    "vim/E722",
		"src/testdir/test_vim9_expr.vim:2791:83327/def":            "vim/E720",
		"src/testdir/test_vim9_expr.vim:2791:83327/vim9-script":    "vim/E720",
		"src/testdir/test_vim9_expr.vim:2794:83473/def":            "vim/E696",
		"src/testdir/test_vim9_expr.vim:2794:83473/vim9-script":    "vim/E696",
		"src/testdir/test_vim9_expr.vim:2795:83546/def":            "vim/E696",
		"src/testdir/test_vim9_expr.vim:2795:83546/vim9-script":    "vim/E696",
		"src/testdir/test_vim9_expr.vim:2835:84508/def":            "vim/E488",
		"src/testdir/test_vim9_expr.vim:2835:84508/vim9-script":    "vim/E488",
		"src/testdir/test_vim9_expr.vim:2855:84948/def":            "vim/E1171",
		"src/testdir/test_vim9_expr.vim:2856:85018/script":         "vim/E1171",
		"src/testdir/test_vim9_expr.vim:2863:85195/def":            "vim/E1145",
		"src/testdir/test_vim9_expr.vim:2864:85270/script":         "vim/E1145",
		"src/testdir/test_vim9_expr.vim:2944:87569/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2944:87569/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2945:87635/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2945:87635/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2946:87702/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2946:87702/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2952:88043/def":            "vim/E1069",
		"src/testdir/test_vim9_expr.vim:2952:88043/vim9-script":    "vim/E1069",
		"src/testdir/test_vim9_expr.vim:2964:88596/def":            "vim/E722",
		"src/testdir/test_vim9_expr.vim:2964:88596/vim9-script":    "vim/E722",
		"src/testdir/test_vim9_expr.vim:2965:88679/def":            "vim/E720",
		"src/testdir/test_vim9_expr.vim:2965:88679/vim9-script":    "vim/E720",
		"src/testdir/test_vim9_expr.vim:2967:88763/def":            "vim/E696",
		"src/testdir/test_vim9_expr.vim:2967:88763/vim9-script":    "vim/E696",
		"src/testdir/test_vim9_expr.vim:3122:92753/def":            "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3122:92753/vim9-script":    "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3123:92819/def":            "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3123:92819/vim9-script":    "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3124:92887/def":            "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3124:92887/vim9-script":    "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3125:92958/def":            "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3125:92958/vim9-script":    "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3127:93030/def":            "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3127:93030/vim9-script":    "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3129:93103/def":            "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3129:93103/vim9-script":    "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3130:93165/def":            "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3130:93165/vim9-script":    "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3131:93229/def":            "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3131:93229/vim9-script":    "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3132:93292/def":            "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3132:93292/vim9-script":    "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3133:93362/def":            "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3133:93362/vim9-script":    "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3135:93431/def":            "vim/E720",
		"src/testdir/test_vim9_expr.vim:3135:93431/vim9-script":    "vim/E720",
		"src/testdir/test_vim9_expr.vim:3136:93492/def":            "vim/E722",
		"src/testdir/test_vim9_expr.vim:3136:93492/vim9-script":    "vim/E722",
		"src/testdir/test_vim9_expr.vim:3137:93568/def":            "vim/E723",
		"src/testdir/test_vim9_expr.vim:3138:93623/script":         "vim/E723",
		"src/testdir/test_vim9_expr.vim:3156:94730/def":            "vim/E723",
		"src/testdir/test_vim9_expr.vim:3157:94779/script":         "vim/E723",
		"src/testdir/test_vim9_expr.vim:3161:94986/def":            "vim/E488",
		"src/testdir/test_vim9_expr.vim:3162:95074/def":            "vim/E488",
		"src/testdir/test_vim9_expr.vim:3207:95979/def":            "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3207:95979/vim9-script":    "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3212:96087/def":            "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3212:96087/vim9-script":    "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3217:96189/def":            "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3217:96189/vim9-script":    "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3222:96289/def":            "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3222:96289/vim9-script":    "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3227:96399/def":            "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3227:96399/vim9-script":    "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3252:96997/def":            "vim/E1139",
		"src/testdir/test_vim9_expr.vim:3252:96997/vim9-script":    "vim/E1139",
		"src/testdir/test_vim9_expr.vim:3260:97154/def":            "vim/E1139",
		"src/testdir/test_vim9_expr.vim:3260:97154/vim9-script":    "vim/E1139",
		"src/testdir/test_vim9_expr.vim:3265:97250/def":            "vim/E723",
		"src/testdir/test_vim9_expr.vim:3266:97290/script":         "vim/E15",
		"src/testdir/test_vim9_expr.vim:3274:97438/def":            "vim/E723",
		"src/testdir/test_vim9_expr.vim:3274:97438/vim9-script":    "vim/E723",
		"src/testdir/test_vim9_expr.vim:3362:99310/def":            "vim/E1002",
		"src/testdir/test_vim9_expr.vim:3362:99310/vim9-script":    "vim/E15",
		"src/testdir/test_vim9_expr.vim:3610:106262/def":           "vim/E1002",
		"src/testdir/test_vim9_expr.vim:3610:106262/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3611:106332/def":           "vim/E1002",
		"src/testdir/test_vim9_expr.vim:3611:106332/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3637:106875/def":           "vim/E354",
		"src/testdir/test_vim9_expr.vim:3637:106875/vim9-script":   "vim/E354",
		"src/testdir/test_vim9_expr.vim:3638:106933/def":           "vim/E354",
		"src/testdir/test_vim9_expr.vim:3638:106933/vim9-script":   "vim/E354",
		"src/testdir/test_vim9_expr.vim:3639:106991/def":           "vim/E354",
		"src/testdir/test_vim9_expr.vim:3639:106991/vim9-script":   "vim/E354",
		"src/testdir/test_vim9_expr.vim:3640:107049/def":           "vim/E354",
		"src/testdir/test_vim9_expr.vim:3640:107049/vim9-script":   "vim/E354",
		"src/testdir/test_vim9_expr.vim:3743:109744/def":           "vim/E1097",
		"src/testdir/test_vim9_expr.vim:3743:109744/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3744:109846/def":           "vim/E110",
		"src/testdir/test_vim9_expr.vim:3744:109846/vim9-script":   "vim/E110",
		"src/testdir/test_vim9_expr.vim:3776:110522/def":           "vim/E15",
		"src/testdir/test_vim9_expr.vim:3776:110522/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3781:110622/def":           "vim/E15",
		"src/testdir/test_vim9_expr.vim:3781:110622/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3786:110722/def":           "vim/E15",
		"src/testdir/test_vim9_expr.vim:3786:110722/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3791:110822/def":           "vim/E15",
		"src/testdir/test_vim9_expr.vim:3791:110822/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3796:110923/def":           "vim/E15",
		"src/testdir/test_vim9_expr.vim:3796:110923/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3801:111024/def":           "vim/E15",
		"src/testdir/test_vim9_expr.vim:3801:111024/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3862:112412/def":           "vim/E107",
		"src/testdir/test_vim9_expr.vim:3862:112412/vim9-script":   "vim/E107",
		"src/testdir/test_vim9_expr.vim:3866:112596/def":           "vim/E1123",
		"src/testdir/test_vim9_expr.vim:3867:112688/def":           "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3867:112688/vim9-script":   "vim/E1068",
		"src/testdir/test_vim9_expr.vim:4023:116670/def":           "vim/E15",
		"src/testdir/test_vim9_expr.vim:4153:119872/def":           "vim/E1097",
		"src/testdir/test_vim9_expr.vim:4154:119928/script":        "vim/E110",
		"src/testdir/test_vim9_expr.vim:4162:120367/def":           "vim/E1002",
		"src/testdir/test_vim9_expr.vim:4162:120367/vim9-script":   "vim/E1002",
		"src/testdir/test_vim9_expr.vim:4163:120430/def":           "vim/E354",
		"src/testdir/test_vim9_expr.vim:4163:120430/vim9-script":   "vim/E354",
		"src/testdir/test_vim9_expr.vim:4165:120494/def":           "vim/E697",
		"src/testdir/test_vim9_expr.vim:4166:120551/script":        "vim/E696",
		"src/testdir/test_vim9_expr.vim:4170:120710/def":           "vim/E488",
		"src/testdir/test_vim9_expr.vim:4170:120710/vim9-script":   "vim/E488",
		"src/testdir/test_vim9_expr.vim:4171:120781/def":           "vim/E107",
		"src/testdir/test_vim9_expr.vim:4171:120781/vim9-script":   "vim/E107",
		"src/testdir/test_vim9_expr.vim:4190:121912/def":           "vim/E488",
		"src/testdir/test_vim9_expr.vim:4190:121912/vim9-script":   "vim/E260",
		"src/testdir/test_vim9_expr.vim:4192:122040/def":           "vim/E697",
		"src/testdir/test_vim9_expr.vim:4193:122100/script":        "vim/E696",
		"src/testdir/test_vim9_expr.vim:4195:122174/def":           "vim/E723",
		"src/testdir/test_vim9_expr.vim:4196:122230/script":        "vim/E722",
		"src/testdir/test_vim9_expr.vim:4198:122304/def":           "vim/E723",
		"src/testdir/test_vim9_expr.vim:4199:122368/script":        "vim/E722",
		"src/testdir/test_vim9_expr.vim:4201:122446/def":           "vim/E1170",
		"src/testdir/test_vim9_expr.vim:4310:125297/def":           "vim/E1097",
		"src/testdir/test_vim9_expr.vim:4311:125338/script":        "vim/E15",
		"src/testdir/test_vim9_expr.vim:4321:125571/def":           "vim/E1097",
		"src/testdir/test_vim9_expr.vim:4322:125612/script":        "vim/E111",
		"src/testdir/test_vim9_expr.vim:4328:125740/vim9-script":   "vim/E111",
		"src/testdir/test_vim9_expr.vim:4328:125740/def":           "vim/E111",
		"src/testdir/test_vim9_expr.vim:4475:129586/def":           "vim/E1127",
		"src/testdir/test_vim9_expr.vim:4479:129693/def":           "vim/E107",
		"src/testdir/test_vim9_expr.vim:4479:129693/vim9-script":   "vim/E107",
		"src/testdir/test_vim9_expr.vim:4484:129910/def":           "vim/E488",
		"src/testdir/test_vim9_expr.vim:4484:129910/vim9-script":   "vim/E488",
		"src/testdir/test_vim9_expr.vim:4485:129977/def":           "vim/E488",
		"src/testdir/test_vim9_expr.vim:4485:129977/vim9-script":   "vim/E488",
		"src/testdir/test_vim9_expr.vim:4487:130048/def":           "vim/E476",
		"src/testdir/test_vim9_interface.vim:159:3829/script":      "vim/E476",
		"src/testdir/test_vim9_interface.vim:167:4035/script":      "vim/E476",
		"src/testdir/test_vim9_expr.vim:4487:130048/vim9-script":   "vim/E492",
		"src/testdir/test_vim9_expr.vim:4495:130468/def":           "vim/E110",
		"src/testdir/test_listdict.vim:529:14207/def":              "vim/E1004",
		"src/testdir/test_listdict.vim:1521:47598/legacy":          "vim/E15",
		"src/testdir/test_listdict.vim:1521:47598/def":             "vim/E1127",
		"src/testdir/test_listdict.vim:1521:47598/vim9-script":     "vim/E15",
		"src/testdir/test_listdict.vim:1532:48170/def":             "vim/E1097",
		"src/testdir/test_tuple.vim:138:3809/def":                  "vim/E1004",
		"src/testdir/test_tuple.vim:143:3972/vim9-script":          "vim/E1068",
		"src/testdir/test_tuple.vim:151:4245/vim9-script":          "vim/E1068",
		"src/testdir/test_usercommands.vim:328:8893/script":        "vim/E1208",
		"src/testdir/test_usercommands.vim:334:9034/script":        "vim/E1208",
		"src/testdir/test_usercommands.vim:1007:34285/script":      "vim/E1026",
		"src/testdir/test_usercommands.vim:1046:35079/script":      "vim/E1128",
		"src/testdir/test_vim9_script.vim:3886:82965/script":       "vim/E182",
		"src/testdir/test_vim9_script.vim:3890:83060/script":       "vim/E182",
	}
}

func TestOfficialVimParserFailures(t *testing.T) {
	expected := officialParserExpectedFailures()

	selected := 0
	for key := range expected {
		if officialParserCaseSelected(key) {
			selected++
		}
	}
	if selected == 0 {
		t.Skipf("no migrated official parser failure matches -official-case=%q", *officialParserCaseFilter)
	}

	corpus := readOfficialParserCases(t)
	seen := make(map[string]struct{}, selected)
	for _, record := range corpus.Records {
		for _, testCase := range record.Cases {
			key := record.ID + "/" + testCase.Name
			code, ok := expected[key]
			if !ok || !officialParserCaseSelected(key) {
				continue
			}
			if testCase.VimOutcome != "failure" || testCase.ParserExpectation != "unclassified" {
				t.Fatalf("%s: official case classification changed: outcome=%q expectation=%q", key, testCase.VimOutcome, testCase.ParserExpectation)
			}
			file := Parse(testCase.Source)
			if file.Source != testCase.Source || len(file.Commands) == 0 {
				t.Fatalf("%s: parser did not retain and recover the official source", key)
			}
			if len(file.Diagnostics) == 0 || file.Diagnostics[0].Code != code {
				t.Fatalf("%s: diagnostics = %#v, want %s", key, file.Diagnostics, code)
			}
			for index := 1; index < len(file.Diagnostics); index++ {
				previous := file.Diagnostics[index-1]
				current := file.Diagnostics[index]
				_, nextLine := physicalLineEnd(file.Source, previous.Span.Start)
				if current.Span.Start < nextLine || current.Span.Start < previous.Span.End || !strings.HasPrefix(current.Code, "vim/E") {
					t.Fatalf("%s: non-line-local diagnostic cascade = %#v", key, file.Diagnostics)
				}
			}
			assertFileSpansAt(t, file, key)
			seen[key] = struct{}{}
		}
	}
	if len(seen) != selected {
		var missing []string
		for key := range expected {
			if officialParserCaseSelected(key) {
				if _, ok := seen[key]; ok {
					continue
				}
				missing = append(missing, key)
			}
		}
		sort.Strings(missing)
		t.Fatalf("official parser failures missing from artifact: %v", missing)
	}
}

func TestOfficialVimParserMigrationReport(t *testing.T) {
	expected := officialParserExpectedFailures()
	groups := map[string]string{
		"src/testdir/test_vim9_expr.vim":      "A",
		"src/testdir/test_vim9_class.vim":     "B",
		"src/testdir/test_vim9_assign.vim":    "B",
		"src/testdir/test_vim9_interface.vim": "B",
		"src/testdir/test_eval_stuff.vim":     "B",
		"src/testdir/test_user_func.vim":      "B",
		"src/testdir/test_usercommands.vim":   "B",
		"src/testdir/test_let.vim":            "B",
		"src/testdir/test_trycatch.vim":       "B",
		"src/testdir/test_autocmd.vim":        "B",
		"src/testdir/test_vim9_script.vim":    "C",
		"src/testdir/test_vim9_generics.vim":  "C",
		"src/testdir/test_vim9_cmd.vim":       "C",
		"src/testdir/test_vim9_import.vim":    "C",
		"src/testdir/test_vim9_typealias.vim": "C",
		"src/testdir/test_tuple.vim":          "D",
		"src/testdir/test_vim9_func.vim":      "D",
		"src/testdir/test_expr.vim":           "D",
		"src/testdir/test_blob.vim":           "D",
		"src/testdir/test_listdict.vim":       "D",
		"src/testdir/test_vim9_enum.vim":      "D",
	}
	corpus := readOfficialParserCases(t)
	checkOfficialParserCaseCorpus(t, corpus)
	counts := map[string]int{"A": 0, "B": 0, "C": 0, "D": 0}
	seen := make(map[string]struct{}, len(expected))
	for _, record := range corpus.Records {
		for _, testCase := range record.Cases {
			key := record.ID + "/" + testCase.Name
			if _, ok := expected[key]; !ok {
				continue
			}
			group, ok := groups[record.Path]
			if !ok {
				t.Fatalf("%s: migrated case is outside source groups A-D", key)
			}
			counts[group]++
			seen[key] = struct{}{}
		}
	}
	if len(seen) != len(expected) {
		stale := make([]string, 0, len(expected)-len(seen))
		for key := range expected {
			if _, ok := seen[key]; !ok {
				stale = append(stale, key)
			}
		}
		sort.Strings(stale)
		t.Fatalf("migration migrated=%d A=%d B=%d C=%d D=%d stale=%d keys=%v", len(expected), counts["A"], counts["B"], counts["C"], counts["D"], len(stale), stale)
	}
	t.Logf("migration migrated=%d A=%d B=%d C=%d D=%d stale=0", len(expected), counts["A"], counts["B"], counts["C"], counts["D"])
}

func TestOfficialVimParserFailureTriage(t *testing.T) {
	if strings.TrimSpace(*officialParserCaseFilter) == "" {
		t.Skip("use -args -official-case=<path,record,error> to select failure cases")
	}
	corpus := readOfficialParserCases(t)
	counts := map[string]int{"ready": 0, "mapping": 0, "missing": 0, "recovery": 0, "unknown": 0}
	total := 0
	for _, record := range corpus.Records {
		for index, testCase := range record.Cases {
			key := record.ID + "/" + testCase.Name
			if !officialParserCaseSelected(key) || testCase.VimOutcome != "failure" || testCase.ParserExpectation != "unclassified" {
				continue
			}
			total++
			want, known := officialParserExpectedCode(record, index)
			if !known {
				counts["unknown"]++
				t.Logf("%s category=unknown errorArgument=%q source=%q", key, record.ErrorArgument, officialParserSourcePreview(testCase.Source))
				continue
			}
			file := Parse(testCase.Source)
			got := make([]string, 0, len(file.Diagnostics))
			for _, diagnostic := range file.Diagnostics {
				got = append(got, diagnostic.Code)
			}
			category := "recovery"
			switch {
			case len(got) == 0:
				category = "missing"
			case len(got) == 1 && got[0] == want:
				category = "ready"
			case len(got) == 1:
				category = "mapping"
			}
			counts[category]++
			t.Logf("%s category=%s want=%s got=%s source=%q", key, category, want, strings.Join(got, ","), officialParserSourcePreview(testCase.Source))
		}
	}
	if total == 0 {
		t.Fatalf("no unclassified official failure matches -official-case=%q", *officialParserCaseFilter)
	}
	t.Logf("triage total=%d ready=%d mapping=%d missing=%d recovery=%d unknown=%d", total, counts["ready"], counts["mapping"], counts["missing"], counts["recovery"], counts["unknown"])
}

func officialParserCaseSelected(key string) bool {
	filter := strings.TrimSpace(*officialParserCaseFilter)
	if filter == "" {
		return true
	}
	for _, part := range strings.Split(filter, ",") {
		if part = strings.TrimSpace(part); part != "" && strings.Contains(key, part) {
			return true
		}
	}
	return false
}

func officialParserExpectedCode(record officialParserCaseRecord, caseIndex int) (string, bool) {
	codes := officialErrorCodePattern.FindAllString(record.ErrorArgument, -1)
	if len(codes) == 1 {
		return "vim/" + codes[0], true
	}
	if len(codes) == len(record.Cases) && caseIndex >= 0 && caseIndex < len(codes) {
		return "vim/" + codes[caseIndex], true
	}
	return "", false
}

func officialParserSourcePreview(source string) string {
	const limit = 400
	if len(source) <= limit {
		return source
	}
	return source[:limit] + "..."
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
