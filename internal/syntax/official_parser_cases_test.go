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
		"src/testdir/test_vim9_assign.vim:1551:36793/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1552:36842/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1553:36892/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1555:36943/script":       "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1557:37089/script":       "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1558:37156/script":       "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1559:37223/script":       "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1561:37400/script":       "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2096:53307/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2096:53307/vim9-script":  "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2102:53491/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2102:53491/vim9-script":  "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2108:53673/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2108:53673/vim9-script":  "vim/E1004",
		"src/testdir/test_vim9_class.vim:11721:265332/def":         "vim/E1008",
		"src/testdir/test_vim9_class.vim:11721:265332/vim9-script": "vim/E1008",
		"src/testdir/test_vim9_class.vim:11726:265505/def":         "vim/E1068",
		"src/testdir/test_vim9_class.vim:11726:265505/vim9-script": "vim/E1068",
		"src/testdir/test_vim9_class.vim:11731:265708/def":         "vim/E1009",
		"src/testdir/test_vim9_class.vim:11731:265708/vim9-script": "vim/E1009",
		"src/testdir/test_vim9_class.vim:11736:265882/def":         "vim/E1009",
		"src/testdir/test_vim9_class.vim:11:199/script":            "vim/E1316",
		"src/testdir/test_vim9_class.vim:19:420/script":            "vim/E1314",
		"src/testdir/test_vim9_class.vim:27:664/script":            "vim/E1315",
		"src/testdir/test_vim9_class.vim:1388:32121/script":        "vim/E1331",
		"src/testdir/test_vim9_class.vim:1482:34515/script":        "vim/E1368",
		"src/testdir/test_vim9_class.vim:1907:44214/script":        "vim/E1004",
		"src/testdir/test_vim9_class.vim:1914:44393/script":        "vim/E1004",
		"src/testdir/test_vim9_class.vim:2386:54166/script":        "vim/E1352",
		"src/testdir/test_vim9_class.vim:2485:56344/script":        "vim/E1315",
		"src/testdir/test_vim9_class.vim:279:7538/script":          "vim/E1317",
		"src/testdir/test_vim9_class.vim:288:7810/script":          "vim/E1317",
		"src/testdir/test_vim9_class.vim:297:8073/script":          "vim/E1317",
		"src/testdir/test_vim9_class.vim:5967:133218/script":       "vim/E1329",
		"src/testdir/test_vim9_interface.vim:553:12767/script":     "vim/E1389",
		"src/testdir/test_vim9_interface.vim:29:581/script":        "vim/E1342",
		"src/testdir/test_vim9_interface.vim:224:5223/script":      "vim/E1350",
		"src/testdir/test_vim9_interface.vim:237:5473/script":      "vim/E1351",
		"src/testdir/test_vim9_interface.vim:308:7096/script":      "vim/E1315",
		"src/testdir/test_vim9_interface.vim:361:8375/script":      "vim/E1315",
		"src/testdir/test_vim9_interface.vim:49:1114/script":       "vim/E1343",
		"src/testdir/test_vim9_interface.vim:535:12343/script":     "vim/E1315",
		"src/testdir/test_vim9_interface.vim:545:12575/script":     "vim/E1389",
		"src/testdir/test_vim9_interface.vim:59:1387/script":       "vim/E1344",
		"src/testdir/test_vim9_interface.vim:79:1888/script":       "vim/E1065",
		"src/testdir/test_vim9_interface.vim:316:7304/script":      "vim/E488",
		"src/testdir/test_vim9_enum.vim:108:2615/script":           "vim/E488",
		"src/testdir/test_vim9_enum.vim:28:569/script":             "vim/E1415",
		"src/testdir/test_vim9_enum.vim:36:799/script":             "vim/E1315",
		"src/testdir/test_vim9_enum.vim:298:6850/script":           "vim/E1418",
		"src/testdir/test_vim9_enum.vim:329:7496/script":           "vim/E1416",
		"src/testdir/test_vim9_enum.vim:339:7706/script":           "vim/E1416",
		"src/testdir/test_vim9_enum.vim:369:8371/script":           "vim/E1417",
		"src/testdir/test_vim9_enum.vim:288:6615/script":           "vim/E1418",
		"src/testdir/test_vim9_interface.vim:1086:23527/script":    "vim/E1381",
		"src/testdir/test_vim9_interface.vim:1098:23808/script":    "vim/E1315",
		"src/testdir/test_trycatch.vim:2019:44596/script":          "vim/E690",
		"src/testdir/test_trycatch.vim:2029:44739/script":          "vim/E690",
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
		"src/testdir/test_vim9_import.vim:531:14329/script":        "vim/E1047",
		"src/testdir/test_vim9_import.vim:536:14451/script":        "vim/E1047",
		"src/testdir/test_vim9_import.vim:541:14571/script":        "vim/E1047",
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
		"src/testdir/test_vim9_func.vim:2114:47060/script":         "vim/E1180",
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
		"src/testdir/test_vim9_func.vim:2883:66057/def":            "vim/E1180",
		"src/testdir/test_vim9_func.vim:2885:66191/def":            "vim/E1068",
		"src/testdir/test_vim9_func.vim:2886:66262/def":            "vim/E1069",
		"src/testdir/test_vim9_func.vim:2887:66332/def":            "vim/E1005",
		"src/testdir/test_vim9_func.vim:2888:66507/def":            "vim/E1069",
		"src/testdir/test_vim9_func.vim:3746:85903/script":         "vim/E488",
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
		"src/testdir/test_vim9_typealias.vim:62:1839/script":       "vim/E1397",
		"src/testdir/test_vim9_typealias.vim:76:2174/script":       "vim/E1398",
		"src/testdir/test_vim9_script.vim:1810:37557/script":       "vim/E1039",
		"src/testdir/test_vim9_script.vim:1811:37626/script":       "vim/E1040",
		"src/testdir/test_vim9_script.vim:3942:84304/script":       "vim/E488",
		"src/testdir/test_vim9_script.vim:3946:84394/script":       "vim/E488",
		"src/testdir/test_vim9_script.vim:3967:84795/script":       "vim/E488",
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
		"src/testdir/test_vim9_expr.vim:520:14502/def":             "vim/E1004",
		"src/testdir/test_vim9_expr.vim:520:14502/vim9-script":     "vim/E1004",
		"src/testdir/test_vim9_expr.vim:661:17908/def":             "vim/E1004",
		"src/testdir/test_vim9_expr.vim:661:17908/vim9-script":     "vim/E1004",
		"src/testdir/test_vim9_expr.vim:666:18082/def":             "vim/E1004",
		"src/testdir/test_vim9_expr.vim:666:18082/vim9-script":     "vim/E1004",
		"src/testdir/test_vim9_expr.vim:671:18193/def":             "vim/E1004",
		"src/testdir/test_vim9_expr.vim:671:18193/vim9-script":     "vim/E1004",
		"src/testdir/test_vim9_expr.vim:804:21948/def":             "vim/E1004",
		"src/testdir/test_vim9_expr.vim:804:21948/vim9-script":     "vim/E1004",
		"src/testdir/test_vim9_expr.vim:809:22059/def":             "vim/E1004",
		"src/testdir/test_vim9_expr.vim:809:22059/vim9-script":     "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1910:56309/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:1910:56309/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2347:70004/def":            "vim/E1104",
		"src/testdir/test_vim9_expr.vim:2347:70004/vim9-script":    "vim/E1104",
		"src/testdir/test_vim9_expr.vim:4153:119872/def":           "vim/E1097",
		"src/testdir/test_vim9_expr.vim:4154:119928/script":        "vim/E110",
		"src/testdir/test_vim9_expr.vim:4162:120367/def":           "vim/E1002",
		"src/testdir/test_vim9_expr.vim:4162:120367/vim9-script":   "vim/E1002",
		"src/testdir/test_vim9_expr.vim:4163:120430/def":           "vim/E354",
		"src/testdir/test_vim9_expr.vim:4163:120430/vim9-script":   "vim/E354",
		"src/testdir/test_vim9_expr.vim:4170:120710/def":           "vim/E488",
		"src/testdir/test_vim9_expr.vim:4170:120710/vim9-script":   "vim/E488",
		"src/testdir/test_vim9_expr.vim:4171:120781/def":           "vim/E107",
		"src/testdir/test_vim9_expr.vim:4171:120781/vim9-script":   "vim/E107",
		"src/testdir/test_listdict.vim:523:14052/script":           "vim/E1004",
		"src/testdir/test_listdict.vim:529:14207/def":              "vim/E1004",
		"src/testdir/test_listdict.vim:1521:47598/legacy":          "vim/E15",
		"src/testdir/test_listdict.vim:1521:47598/def":             "vim/E1127",
		"src/testdir/test_listdict.vim:1521:47598/vim9-script":     "vim/E15",
		"src/testdir/test_listdict.vim:1532:48170/legacy":          "vim/E111",
		"src/testdir/test_listdict.vim:1532:48170/def":             "vim/E1097",
		"src/testdir/test_listdict.vim:1532:48170/vim9-script":     "vim/E111",
		"src/testdir/test_tuple.vim:138:3809/def":                  "vim/E1004",
		"src/testdir/test_tuple.vim:143:3972/vim9-script":          "vim/E1068",
		"src/testdir/test_tuple.vim:151:4245/vim9-script":          "vim/E1068",
		"src/testdir/test_usercommands.vim:328:8893/script":        "vim/E1208",
		"src/testdir/test_usercommands.vim:334:9034/script":        "vim/E1208",
		"src/testdir/test_usercommands.vim:1007:34285/script":      "vim/E1026",
		"src/testdir/test_usercommands.vim:1046:35079/script":      "vim/E1128",
	}

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
			if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != code {
				t.Fatalf("%s: diagnostics = %#v, want %s", key, file.Diagnostics, code)
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
