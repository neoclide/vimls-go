package syntax

import (
	"compress/gzip"
	"crypto/sha256"
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

	"github.com/neoclide/vimls-go/internal/vimdata"
)

const (
	officialParserCasesSchemaVersion = 1
	officialParserCasesManifest      = vimdata.VimSourceTag + "-parser-files.json"
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

type officialParserFileManifest struct {
	SchemaVersion      int                        `json:"schemaVersion"`
	Tag                string                     `json:"tag"`
	Commit             string                     `json:"commit"`
	Scope              string                     `json:"scope"`
	DefaultDisposition string                     `json:"defaultDisposition"`
	Files              []officialParserFileRecord `json:"files"`
}

type officialParserFileRecord struct {
	Path        string `json:"path"`
	Disposition string `json:"disposition"`
	Reason      string `json:"reason"`
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
	lock := readOfficialCorpusLock(t)
	corpus := readOfficialParserCases(t)
	checkOfficialParserCaseCorpus(t, corpus, lock)

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
	if accepted != lock.ParserCases.Summary.AcceptedCases || unclassified != lock.ParserCases.Summary.UnclassifiedCases {
		t.Fatalf("official parser-case expectations changed: accepted = %d, unclassified = %d", accepted, unclassified)
	}
}

func officialParserExpectedFailures() map[string]string {
	// The assertions were initially migrated from the v9.2.1015 corpus and
	// rebased through v9.2.1235 by matching path, helper, case context/name, and
	// source. Earlier corpora remain available in Git history for provenance.
	// Keep only failures proven to be decided by the parser; execution and
	// type-checking failures stay unclassified. The unchanged exit_cb source
	// retains its v9.2.1094 E476/E492 diagnostic override.
	return map[string]string{
		"src/testdir/test_vim9_func.vim:99:2271/script":            "vim/E1267",
		"src/testdir/test_vim9_func.vim:107:2406/script":           "vim/E1267",
		"src/testdir/test_vim9_func.vim:1049:22663/script":         "vim/E1267",
		"src/testdir/test_vim9_func.vim:1061:22873/script":         "vim/E1267",
		"src/testdir/test_vim9_func.vim:1069:23011/script":         "vim/E1267",
		"src/testdir/test_vim9_func.vim:1077:23147/script":         "vim/E1267",
		"src/testdir/test_vim9_func.vim:1298:27807/script":         "vim/E476",
		"src/testdir/test_vim9_enum.vim:84:2047/script":            "vim/E1420",
		"src/testdir/test_vim9_enum.vim:100:2430/script":           "vim/E1420",
		"src/testdir/test_vim9_enum.vim:214:5043/script":           "vim/E1419",
		"src/testdir/test_vim9_enum.vim:224:5261/script":           "vim/E1419",
		"src/testdir/test_vim9_enum.vim:264:6157/script":           "vim/E1419",
		"src/testdir/test_vim9_func.vim:392:8331/script":           "vim/E1173",
		"src/testdir/test_vim9_func.vim:73:1788/def":               "vim/E1170",
		"src/testdir/test_vim9_func.vim:1145:24549/script":         "vim/E1173",
		"src/testdir/test_vim9_func.vim:2361:52851/script":         "vim/E1173",
		"src/testdir/test_vim9_func.vim:2378:53272/script":         "vim/E1173",
		"src/testdir/test_vim9_func.vim:2388:53495/script":         "vim/E1173",
		"src/testdir/test_vim9_func.vim:2403:53812/script":         "vim/E488",
		"src/testdir/test_vim9_func.vim:373:8000/script":           "vim/E1151",
		"src/testdir/test_vim9_func.vim:382:8151/script":           "vim/E1152",
		"src/testdir/test_vim9_cmd.vim:1922:39946/def":             "vim/E488",
		"src/testdir/test_vim9_cmd.vim:1923:39996/def":             "vim/E488",
		"src/testdir/test_vim9_cmd.vim:472:10765/def":              "vim/E488",
		"src/testdir/test_vim9_cmd.vim:472:10765/vim9-script":      "vim/E488",
		"src/testdir/test_vim9_script.vim:5687:128128/def":         "vim/E476",
		"src/testdir/test_vim9_script.vim:5687:128128/vim9-script": "vim/E492",
		"src/testdir/test_vim9_import.vim:3151:77235/script":       "vim/E461",
		"src/testdir/test_vim9_script.vim:4022:86833/def":          "vim/E461",
		"src/testdir/test_vim9_script.vim:4030:87001/def":          "vim/E461",
		"src/testdir/test_vim9_typealias.vim:125:3484/script":      "vim/E488",
		"src/testdir/test_vim9_script.vim:3412:73088/def":          "vim/E16",
		"src/testdir/test_vim9_script.vim:3412:73088/vim9-script":  "vim/E16",
		"src/testdir/test_vim9_cmd.vim:388:9139/script":            "vim/E15",
		"src/testdir/test_vim9_script.vim:3016:65231/vim9-script":  "vim/E15",
		"src/testdir/test_vim9_script.vim:3016:65231/def":          "vim/E1143",
		"src/testdir/test_vim9_script.vim:4792:103779/script":      "vim/E129",
		"src/testdir/test_vim9_script.vim:72:1434/def":             "vim/E481",
		"src/testdir/test_vim9_script.vim:72:1434/vim9-script":     "vim/E481",
		"src/testdir/test_vim9_script.vim:78:1542/def":             "vim/E481",
		"src/testdir/test_vim9_script.vim:78:1542/vim9-script":     "vim/E481",
		"src/testdir/test_vim9_script.vim:83:1641/def":             "vim/E481",
		"src/testdir/test_vim9_script.vim:83:1641/vim9-script":     "vim/E481",
		"src/testdir/test_vim9_script.vim:88:1738/def":             "vim/E481",
		"src/testdir/test_vim9_script.vim:88:1738/vim9-script":     "vim/E481",
		"src/testdir/test_vim9_import.vim:591:15641/script":        "vim/E1261",
		"src/testdir/test_vim9_import.vim:603:15955/script":        "vim/E1257",
		"src/testdir/test_vim9_import.vim:1728:42236/script":       "vim/E1043",
		"src/testdir/test_vim9_import.vim:1729:42305/script":       "vim/E1044",
		"src/testdir/test_vim9_assign.vim:3384:84289/def":          "vim/E1279",
		"src/testdir/test_vim9_assign.vim:3384:84289/vim9-script":  "vim/E1279",
		"src/testdir/test_vim9_assign.vim:3391:84444/def":          "vim/E1279",
		"src/testdir/test_vim9_assign.vim:3391:84444/vim9-script":  "vim/E1279",
		"src/testdir/test_vim9_assign.vim:3398:84597/def":          "vim/E15",
		"src/testdir/test_vim9_assign.vim:3398:84597/vim9-script":  "vim/E15",
		"src/testdir/test_vim9_assign.vim:3406:84765/def":          "vim/E1278",
		"src/testdir/test_vim9_assign.vim:3406:84765/vim9-script":  "vim/E1278",
		"src/testdir/test_let.vim:789:18932/script":                "vim/E1279",
		"src/testdir/test_let.vim:796:19078/script":                "vim/E1279",
		"src/testdir/test_let.vim:803:19222/script":                "vim/E15",
		"src/testdir/test_vim9_assign.vim:217:5470/vim9-script":    "vim/E475",
		"src/testdir/test_vim9_assign.vim:434:11468/def":           "vim/E1059",
		"src/testdir/test_vim9_assign.vim:3219:80332/script":       "vim/E488",
		"src/testdir/test_vim9_class.vim:93:2494/script":           "vim/E488",
		"src/testdir/test_vim9_class.vim:102:2735/script":          "vim/E488",
		"src/testdir/test_vim9_assign.vim:434:11468/vim9-script":   "vim/E1059",
		"src/testdir/test_vim9_class.vim:261:7035/script":          "vim/E1059",
		"src/testdir/test_vim9_assign.vim:69:1636/def":             "vim/E1069",
		"src/testdir/test_vim9_assign.vim:70:1685/def":             "vim/E1069",
		"src/testdir/test_vim9_assign.vim:71:1740/def":             "vim/E1069",
		"src/testdir/test_vim9_assign.vim:2115:55095/def":          "vim/E1069",
		"src/testdir/test_vim9_assign.vim:2115:55095/vim9-script":  "vim/E1069",
		"src/testdir/test_vim9_assign.vim:2647:67640/script":       "vim/E1069",
		"src/testdir/test_vim9_assign.vim:2649:67682/def":          "vim/E1069",
		"src/testdir/test_vim9_assign.vim:2649:67682/vim9-script":  "vim/E1069",
		"src/testdir/test_vim9_class.vim:270:7286/script":          "vim/E1069",
		"src/testdir/test_vim9_assign.vim:2437:62073/def":          "vim/E110",
		"src/testdir/test_vim9_assign.vim:2437:62073/vim9-script":  "vim/E110",
		"src/testdir/test_vim9_assign.vim:2442:62185/def":          "vim/E110",
		"src/testdir/test_vim9_assign.vim:2442:62185/vim9-script":  "vim/E110",
		"src/testdir/test_vim9_generics.vim:3048:67627/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:805:17703/script":      "vim/E1553",
		"src/testdir/test_vim9_generics.vim:814:17961/script":      "vim/E1069",
		"src/testdir/test_vim9_generics.vim:3057:67814/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:3066:68011/script":     "vim/E1069",
		"src/testdir/test_vim9_generics.vim:3075:68222/script":     "vim/E1069",
		"src/testdir/test_vim9_generics.vim:3084:68421/script":     "vim/E1008",
		"src/testdir/test_vim9_assign.vim:3153:78765/def":          "vim/E1202",
		"src/testdir/test_vim9_assign.vim:3153:78765/vim9-script":  "vim/E1202",
		"src/testdir/test_vim9_class.vim:409:10761/script":         "vim/E1202",
		"src/testdir/test_vim9_class.vim:1929:44717/script":        "vim/E1202",
		"src/testdir/test_vim9_generics.vim:280:6221/script":       "vim/E1554",
		"src/testdir/test_vim9_generics.vim:288:6380/script":       "vim/E1008",
		"src/testdir/test_vim9_generics.vim:304:6695/script":       "vim/E116",
		"src/testdir/test_vim9_generics.vim:320:7038/script":       "vim/E1008",
		"src/testdir/test_vim9_generics.vim:373:8246/script":       "vim/E1069",
		"src/testdir/test_vim9_generics.vim:381:8423/script":       "vim/E1008",
		"src/testdir/test_vim9_generics.vim:256:5703/script":       "vim/E1555",
		"src/testdir/test_vim9_generics.vim:650:14312/script":      "vim/E1555",
		"src/testdir/test_vim9_generics.vim:702:15213/script":      "vim/E1008",
		"src/testdir/test_vim9_generics.vim:787:17195/script":      "vim/E1555",
		"src/testdir/test_vim9_generics.vim:796:17462/script":      "vim/E1008",
		"src/testdir/test_vim9_generics.vim:969:21961/script":      "vim/E1555",
		"src/testdir/test_vim9_generics.vim:1139:25810/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:1214:27205/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:1298:28752/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:1479:32151/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:1557:33623/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:1692:36240/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:2919:64579/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:3030:67202/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:4223:97123/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:336:7374/script":       "vim/E1202",
		"src/testdir/test_vim9_generics.vim:357:7889/script":       "vim/E1202",
		"src/testdir/test_vim9_generics.vim:365:8067/script":       "vim/E1202",
		"src/testdir/test_vim9_generics.vim:397:8780/script":       "vim/E1202",
		"src/testdir/test_vim9_generics.vim:728:15637/script":      "vim/E1202",
		"src/testdir/test_vim9_generics.vim:1706:36532/script":     "vim/E1554",
		"src/testdir/test_vim9_generics.vim:2087:45368/script":     "vim/E1554",
		"src/testdir/test_vim9_generics.vim:2099:45711/script":     "vim/E1554",
		"src/testdir/test_vim9_generics.vim:2105:45860/script":     "vim/E1554",
		"src/testdir/test_vim9_generics.vim:2145:46759/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:2226:48986/script":     "vim/E1554",
		"src/testdir/test_vim9_generics.vim:2244:49426/script":     "vim/E1554",
		"src/testdir/test_vim9_generics.vim:2254:49642/script":     "vim/E1554",
		"src/testdir/test_vim9_generics.vim:2304:50718/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:2354:52131/script":     "vim/E1554",
		"src/testdir/test_vim9_generics.vim:2364:52374/script":     "vim/E1554",
		"src/testdir/test_vim9_generics.vim:2374:52597/script":     "vim/E1554",
		"src/testdir/test_vim9_generics.vim:2424:53708/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:1720:36821/script":     "vim/E1554",
		"src/testdir/test_vim9_generics.vim:1734:37119/script":     "vim/E1554",
		"src/testdir/test_vim9_cmd.vim:50:1025/script":             "vim/E115",
		"src/testdir/test_vim9_cmd.vim:65:1303/script":             "vim/E15",
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
		"src/testdir/test_vim9_assign.vim:1710:42913/def":          "vim/E354",
		"src/testdir/test_vim9_assign.vim:1710:42913/vim9-script":  "vim/E1066",
		"src/testdir/test_vim9_assign.vim:1711:42983/def":          "vim/E354",
		"src/testdir/test_vim9_assign.vim:1711:42983/vim9-script":  "vim/E1066",
		"src/testdir/test_vim9_assign.vim:1712:43053/def":          "vim/E354",
		"src/testdir/test_vim9_assign.vim:1712:43053/vim9-script":  "vim/E1066",
		"src/testdir/test_vim9_assign.vim:1713:43123/def":          "vim/E354",
		"src/testdir/test_vim9_assign.vim:1713:43123/vim9-script":  "vim/E1066",
		"src/testdir/test_vim9_assign.vim:1715:43211/def":          "vim/E354",
		"src/testdir/test_vim9_assign.vim:1715:43211/vim9-script":  "vim/E1066",
		"src/testdir/test_vim9_assign.vim:1717:43290/def":          "vim/E354",
		"src/testdir/test_vim9_assign.vim:1717:43290/vim9-script":  "vim/E1066",
		"src/testdir/test_vim9_assign.vim:1720:43369/def":          "vim/E1066",
		"src/testdir/test_vim9_assign.vim:1721:43416/def":          "vim/E1066",
		"src/testdir/test_vim9_assign.vim:1722:43465/script":       "vim/E1066",
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
		"src/testdir/test_tuple.vim:143:3972/legacy":               "vim/E15",
		"src/testdir/test_tuple.vim:151:4245/def":                  "vim/E1068",
		"src/testdir/test_tuple.vim:151:4245/legacy":               "vim/E15",
		"src/testdir/test_tuple.vim:159:4498/legacy":               "vim/E15",
		"src/testdir/test_tuple.vim:159:4498/def":                  "vim/E1015",
		"src/testdir/test_tuple.vim:159:4498/vim9-script":          "vim/E15",
		"src/testdir/test_tuple.vim:167:4773/def":                  "vim/E1010",
		"src/testdir/test_tuple.vim:167:4773/vim9-script":          "vim/E1010",
		"src/testdir/test_vim9_assign.vim:629:16536/def":           "vim/E1097",
		"src/testdir/test_vim9_assign.vim:73:1858/script":          "vim/E1124",
		"src/testdir/test_vim9_assign.vim:2353:60347/script":       "vim/E1125",
		"src/testdir/test_vim9_assign.vim:2417:61618/script":       "vim/E1021",
		"src/testdir/test_vim9_assign.vim:2423:61723/script":       "vim/E488",
		"src/testdir/test_vim9_assign.vim:2427:61867/def":          "vim/E1021",
		"src/testdir/test_vim9_assign.vim:1685:41806/def":          "vim/E1080",
		"src/testdir/test_vim9_assign.vim:2425:61764/def":          "vim/E1087",
		"src/testdir/test_vim9_assign.vim:2426:61816/def":          "vim/E1087",
		"src/testdir/test_vim9_assign.vim:3037:76196/def":          "vim/E488",
		"src/testdir/test_vim9_assign.vim:3037:76196/vim9-script":  "vim/E488",
		"src/testdir/test_vim9_assign.vim:198:4982/def":            "vim/E1022",
		"src/testdir/test_vim9_assign.vim:1701:42508/def":          "vim/E1022",
		"src/testdir/test_vim9_assign.vim:2166:56306/def":          "vim/E488",
		"src/testdir/test_vim9_assign.vim:2167:56373/def":          "vim/E488",
		"src/testdir/test_vim9_class.vim:1210:27906/script":        "vim/E1022",
		"src/testdir/test_vim9_class.vim:8719:198636/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:8729:198880/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:8739:199124/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:8758:199611/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:8768:199854/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:9053:206897/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:9063:207141/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:9073:207385/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:9092:207872/script":       "vim/E1022",
		"src/testdir/test_vim9_class.vim:9102:208115/script":       "vim/E1022",
		"src/testdir/test_vim9_assign.vim:2747:70030/def":          "vim/E1126",
		"src/testdir/test_vim9_assign.vim:2747:70030/vim9-script":  "vim/E1126",
		"src/testdir/test_vim9_assign.vim:695:17996/def":           "vim/E1097",
		"src/testdir/test_vim9_assign.vim:1316:32559/def":          "vim/E488",
		"src/testdir/test_vim9_assign.vim:1746:44872/def":          "vim/E1068",
		"src/testdir/test_vim9_assign.vim:1747:44932/def":          "vim/E1009",
		"src/testdir/test_vim9_assign.vim:1750:45065/def":          "vim/E1009",
		"src/testdir/test_vim9_assign.vim:1665:40715/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1666:40764/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1667:40814/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1669:40865/script":       "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1671:41011/script":       "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1672:41078/script":       "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1673:41145/script":       "vim/E1004",
		"src/testdir/test_vim9_assign.vim:1675:41322/script":       "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2177:56578/script":       "vim/E1145",
		"src/testdir/test_vim9_assign.vim:2194:56860/script":       "vim/E1145",
		"src/testdir/test_vim9_assign.vim:2210:57229/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2210:57229/vim9-script":  "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2216:57413/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2216:57413/vim9-script":  "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2222:57595/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2222:57595/vim9-script":  "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2432:61968/def":          "vim/E1065",
		"src/testdir/test_vim9_assign.vim:2432:61968/vim9-script":  "vim/E1065",
		"src/testdir/test_vim9_class.vim:11148:252357/script":      "vim/E1356",
		"src/testdir/test_vim9_class.vim:434:11328/script":         "vim/E15",
		"src/testdir/test_vim9_class.vim:10177:231886/script":      "vim/E1429",
		"src/testdir/test_vim9_interface.vim:1416:30780/script":    "vim/E1436",
		"src/testdir/test_vim9_assign.vim:2822:71630/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2826:71717/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:2830:71805/def":          "vim/E1004",
		"src/testdir/test_vim9_assign.vim:3237:80829/script":       "vim/E488",
		"src/testdir/test_vim9_class.vim:11795:267115/def":         "vim/E1008",
		"src/testdir/test_vim9_class.vim:11795:267115/vim9-script": "vim/E1008",
		"src/testdir/test_vim9_class.vim:11800:267288/def":         "vim/E1068",
		"src/testdir/test_vim9_class.vim:11800:267288/vim9-script": "vim/E1068",
		"src/testdir/test_vim9_class.vim:11805:267491/def":         "vim/E1009",
		"src/testdir/test_vim9_class.vim:11805:267491/vim9-script": "vim/E1009",
		"src/testdir/test_vim9_class.vim:11810:267665/def":         "vim/E1009",
		"src/testdir/test_vim9_class.vim:11816:267811/script":      "vim/E488",
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
		"src/testdir/test_vim9_enum.vim:116:2820/script":           "vim/E488",
		"src/testdir/test_vim9_enum.vim:123:3013/script":           "vim/E488",
		"src/testdir/test_vim9_enum.vim:60:1409/script":            "vim/E1065",
		"src/testdir/test_vim9_enum.vim:68:1610/script":            "vim/E1065",
		"src/testdir/test_vim9_enum.vim:76:1834/script":            "vim/E1065",
		"src/testdir/test_vim9_enum.vim:132:3226/script":           "vim/E488",
		"src/testdir/test_vim9_enum.vim:28:569/script":             "vim/E1415",
		"src/testdir/test_vim9_enum.vim:36:799/script":             "vim/E1315",
		"src/testdir/test_vim9_enum.vim:298:6850/script":           "vim/E1418",
		"src/testdir/test_vim9_enum.vim:288:6615/script":           "vim/E1418",
		"src/testdir/test_vim9_enum.vim:978:22009/script":          "vim/E1170",
		"src/testdir/test_vim9_enum.vim:1706:39098/script":         "vim/E1435",
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
		"src/testdir/test_vim9_cmd.vim:1884:39107/script":          "vim/E179",
		"src/testdir/test_vim9_cmd.vim:1892:39233/script":          "vim/E179",
		"src/testdir/test_vim9_cmd.vim:2009:41706/def":             "vim/E1185",
		"src/testdir/test_vim9_cmd.vim:2076:42886/vim9-script":     "vim/E1069",
		"src/testdir/test_vim9_cmd.vim:2090:43316/vim9-script":     "vim/E1069",
		"src/testdir/test_vim9_cmd.vim:2100:43621/def":             "vim/E1241",
		"src/testdir/test_vim9_cmd.vim:2100:43621/vim9-script":     "vim/E1241",
		"src/testdir/test_vim9_cmd.vim:2126:44318/def":             "vim/E1242",
		"src/testdir/test_vim9_cmd.vim:2126:44318/vim9-script":     "vim/E1242",
		"src/testdir/test_vim9_cmd.vim:2130:44412/def":             "vim/E1242",
		"src/testdir/test_vim9_cmd.vim:2130:44412/vim9-script":     "vim/E1242",
		"src/testdir/test_vim9_cmd.vim:2147:44714/def":             "vim/E1242",
		"src/testdir/test_vim9_cmd.vim:2147:44714/vim9-script":     "vim/E1242",
		"src/testdir/test_vim9_cmd.vim:2151:44809/def":             "vim/E1242",
		"src/testdir/test_vim9_cmd.vim:2151:44809/vim9-script":     "vim/E1242",
		"src/testdir/test_vim9_cmd.vim:2081:43068/def":             "vim/E1241",
		"src/testdir/test_vim9_cmd.vim:2081:43068/vim9-script":     "vim/E1241",
		"src/testdir/test_vim9_generics.vim:22:376/script":         "vim/E1552",
		"src/testdir/test_vim9_generics.vim:30:565/script":         "vim/E1555",
		"src/testdir/test_vim9_generics.vim:264:5880/script":       "vim/E1555",
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
		"src/testdir/test_vim9_generics.vim:1394:30506/script":     "vim/E1069",
		"src/testdir/test_vim9_generics.vim:1457:31742/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:1468:31960/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:1971:42622/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:1982:42856/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:2584:56941/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:2594:57148/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:2605:57388/script":     "vim/E1553",
		"src/testdir/test_vim9_generics.vim:2615:57599/script":     "vim/E1552",
		"src/testdir/test_vim9_generics.vim:2626:57847/script":     "vim/E1561",
		"src/testdir/test_vim9_generics.vim:2937:65010/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:2946:65198/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:2955:65397/script":     "vim/E1069",
		"src/testdir/test_vim9_generics.vim:2964:65613/script":     "vim/E1069",
		"src/testdir/test_vim9_generics.vim:2973:65815/script":     "vim/E1008",
		"src/testdir/test_vim9_generics.vim:4233:97347/script":     "vim/E1555",
		"src/testdir/test_vim9_generics.vim:389:8598/script":       "vim/E1069",
		"src/testdir/test_vim9_import.vim:531:14329/script":        "vim/E1047",
		"src/testdir/test_vim9_import.vim:536:14451/script":        "vim/E1047",
		"src/testdir/test_vim9_import.vim:541:14571/script":        "vim/E1047",
		"src/testdir/test_vim9_import.vim:2972:73136/script":       "vim/E475",
		"src/testdir/test_vim9_class.vim:35:883/script":            "vim/E475",
		"src/testdir/test_vim9_class.vim:43:1104/script":           "vim/E475",
		"src/testdir/test_vim9_import.vim:2978:73280/script":       "vim/E983",
		"src/testdir/test_vim9_import.vim:2984:73418/script":       "vim/E475",
		"src/testdir/test_vim9_func.vim:398:8465/script":           "vim/E1065",
		"src/testdir/test_vim9_func.vim:408:8674/script":           "vim/E1057",
		"src/testdir/test_vim9_func.vim:416:8808/script":           "vim/E126",
		"src/testdir/test_vim9_func.vim:426:8986/script":           "vim/E1068",
		"src/testdir/test_vim9_func.vim:434:9120/script":           "vim/E1068",
		"src/testdir/test_vim9_func.vim:441:9237/script":           "vim/E1068",
		"src/testdir/test_vim9_func.vim:781:16158/def":             "vim/E476",
		"src/testdir/test_vim9_func.vim:781:16158/vim9-script":     "vim/E1068",
		"src/testdir/test_vim9_func.vim:948:20577/script":          "vim/E125",
		"src/testdir/test_vim9_func.vim:955:20687/script":          "vim/E125",
		"src/testdir/test_vim9_func.vim:971:20965/script":          "vim/E488",
		"src/testdir/test_vim9_func.vim:828:17920/def":             "vim/E15",
		"src/testdir/test_vim9_func.vim:1157:24812/script":         "vim/E1069",
		"src/testdir/test_vim9_func.vim:1626:35394/def":            "vim/E1172",
		"src/testdir/test_vim9_func.vim:1626:35394/vim9-script":    "vim/E1172",
		"src/testdir/test_vim9_func.vim:1689:36926/def":            "vim/E1157",
		"src/testdir/test_vim9_func.vim:1689:36926/vim9-script":    "vim/E1157",
		"src/testdir/test_vim9_func.vim:1695:37074/def":            "vim/E1069",
		"src/testdir/test_vim9_func.vim:1695:37074/vim9-script":    "vim/E1069",
		"src/testdir/test_vim9_func.vim:1748:38346/def":            "vim/E488",
		"src/testdir/test_vim9_func.vim:1748:38346/vim9-script":    "vim/E488",
		"src/testdir/test_vim9_func.vim:1755:38517/def":            "vim/E488",
		"src/testdir/test_vim9_func.vim:1755:38517/vim9-script":    "vim/E488",
		"src/testdir/test_vim9_func.vim:2034:44861/script":         "vim/E1160",
		"src/testdir/test_vim9_func.vim:2078:45879/def":            "vim/E1007",
		"src/testdir/test_vim9_func.vim:2077:45815/def":            "vim/E1010",
		"src/testdir/test_vim9_func.vim:2086:46156/def":            "vim/E110",
		"src/testdir/test_vim9_func.vim:2087:46235/def":            "vim/E110",
		"src/testdir/test_vim9_func.vim:2441:54782/script":         "vim/E1069",
		"src/testdir/test_vim9_func.vim:2448:54929/script":         "vim/E1059",
		"src/testdir/test_vim9_func.vim:2455:55077/script":         "vim/E1059",
		"src/testdir/test_vim9_func.vim:2462:55226/script":         "vim/E1008",
		"src/testdir/test_vim9_func.vim:2464:55349/script":         "vim/E1008",
		"src/testdir/test_vim9_func.vim:2466:55472/script":         "vim/E1057",
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
		"src/testdir/test_vim9_func.vim:3579:82048/def":            "vim/E720",
		"src/testdir/test_vim9_func.vim:3579:82048/vim9-script":    "vim/E720",
		"src/testdir/test_vim9_func.vim:3774:86547/script":         "vim/E129",
		"src/testdir/test_vim9_func.vim:3780:86662/script":         "vim/E884",
		"src/testdir/test_vim9_func.vim:3786:86772/script":         "vim/E488",
		"src/testdir/test_vim9_func.vim:3857:88684/def":            "vim/E16",
		"src/testdir/test_vim9_func.vim:4560:104640/script":        "vim/E476",
		"src/testdir/test_vim9_expr.vim:4516:130871/def":           "vim/E274",
		"src/testdir/test_vim9_expr.vim:4516:130871/vim9-script":   "vim/E274",
		"src/testdir/test_vim9_expr.vim:4525:131214/def":           "vim/E1069",
		"src/testdir/test_vim9_expr.vim:4525:131214/vim9-script":   "vim/E1069",
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
		"src/testdir/test_vim9_typealias.vim:69:2004/script":       "vim/E398",
		"src/testdir/test_vim9_typealias.vim:76:2174/script":       "vim/E1398",
		"src/testdir/test_vim9_typealias.vim:83:2341/script":       "vim/E1315",
		"src/testdir/test_vim9_typealias.vim:90:2522/script":       "vim/E1069",
		"src/testdir/test_vim9_typealias.vim:111:3055/script":      "vim/E1394",
		"src/testdir/test_vim9_typealias.vim:118:3270/script":      "vim/E1315",
		"src/testdir/test_vim9_typealias.vim:97:2698/script":       "vim/E398",
		"src/testdir/test_vim9_script.vim:195:4394/def":            "vim/E1009",
		"src/testdir/test_vim9_script.vim:196:4452/def":            "vim/E1009",
		"src/testdir/test_vim9_script.vim:271:6623/def":            "vim/E1125",
		"src/testdir/test_vim9_script.vim:365:9084/def":            "vim/E1050",
		"src/testdir/test_vim9_script.vim:366:9128/def":            "vim/E1050",
		"src/testdir/test_vim9_script.vim:367:9173/def":            "vim/E1050",
		"src/testdir/test_vim9_script.vim:368:9218/def":            "vim/E1050",
		"src/testdir/test_vim9_script.vim:2701:58052/script":       "vim/E1039",
		"src/testdir/test_vim9_script.vim:2702:58121/script":       "vim/E1040",
		"src/testdir/test_vim9_script.vim:2708:58399/def":          "vim/E1038",
		"src/testdir/test_vim9_script.vim:2934:63257/script":       "vim/E1100",
		"src/testdir/test_vim9_script.vim:2936:63370/script":       "vim/E1100",
		"src/testdir/test_vim9_script.vim:2938:63483/script":       "vim/E1100",
		"src/testdir/test_vim9_script.vim:2940:63597/script":       "vim/E1100",
		"src/testdir/test_vim9_script.vim:2942:63711/script":       "vim/E1100",
		"src/testdir/test_vim9_script.vim:2944:63825/script":       "vim/E1100",
		"src/testdir/test_vim9_script.vim:2946:63939/script":       "vim/E1100",
		"src/testdir/test_vim9_script.vim:4632:100073/script":      "vim/E474",
		"src/testdir/test_vim9_script.vim:5026:108689/script":      "vim/E477",
		"src/testdir/test_vim9_script.vim:4481:96857/script":       "vim/E1144",
		"src/testdir/test_vim9_script.vim:4432:95922/script":       "vim/E1170",
		"src/testdir/test_vim9_script.vim:4501:97299/script":       "vim/E1144",
		"src/testdir/test_vim9_script.vim:4509:97465/def":          "vim/E1144",
		"src/testdir/test_vim9_script.vim:4515:97582/script":       "vim/E1144",
		"src/testdir/test_vim9_script.vim:4520:97689/def":          "vim/E1144",
		"src/testdir/test_vim9_script.vim:4532:97916/def":          "vim/E1144",
		"src/testdir/test_vim9_script.vim:4538:98033/script":       "vim/E1144",
		"src/testdir/test_vim9_script.vim:4551:98295/def":          "vim/E1144",
		"src/testdir/test_vim9_script.vim:4557:98410/script":       "vim/E1144",
		"src/testdir/test_vim9_script.vim:4569:98628/script":       "vim/E1144",
		"src/testdir/test_vim9_script.vim:4615:99715/script":       "vim/E1144",
		"src/testdir/test_vim9_script.vim:4750:102916/script":      "vim/E1144",
		"src/testdir/test_vim9_script.vim:4796:103871/script":      "vim/E1144",
		"src/testdir/test_vim9_script.vim:4888:105824/script":      "vim/E1144",
		"src/testdir/test_vim9_script.vim:5693:128299/def":         "vim/E1144",
		"src/testdir/test_vim9_script.vim:5693:128299/vim9-script": "vim/E1144",
		"src/testdir/test_vim9_script.vim:3211:68464/def":          "vim/E114",
		"src/testdir/test_vim9_script.vim:3212:68516/def":          "vim/E115",
		"src/testdir/test_vim9_script.vim:3213:68568/def":          "vim/E110",
		"src/testdir/test_vim9_script.vim:3214:68616/def":          "vim/E109",
		"src/testdir/test_vim9_script.vim:6322:142411/script":      "vim/E107",
		"src/testdir/test_vim9_script.vim:6486:145949/script":      "vim/E107",
		"src/testdir/test_vim9_script.vim:1058:21851/script":       "vim/E1032",
		"src/testdir/test_vim9_script.vim:2423:51567/def":          "vim/E603",
		"src/testdir/test_vim9_script.vim:2425:51676/def":          "vim/E1067",
		"src/testdir/test_vim9_script.vim:2426:51740/def":          "vim/E606",
		"src/testdir/test_vim9_script.vim:2427:51783/def":          "vim/E607",
		"src/testdir/test_vim9_script.vim:2428:51864/def":          "vim/E602",
		"src/testdir/test_vim9_script.vim:2429:51906/def":          "vim/E170",
		"src/testdir/test_vim9_script.vim:2430:51959/def":          "vim/E170",
		"src/testdir/test_vim9_script.vim:2431:52022/def":          "vim/E171",
		"src/testdir/test_vim9_script.vim:2432:52072/def":          "vim/E1032",
		"src/testdir/test_vim9_cmd.vim:1673:35064/def":             "vim/E171",
		"src/testdir/test_vim9_script.vim:2433:52132/def":          "vim/E600",
		"src/testdir/test_vim9_script.vim:2434:52171/def":          "vim/E600",
		"src/testdir/test_vim9_script.vim:2435:52220/def":          "vim/E600",
		"src/testdir/test_vim9_script.vim:2436:52278/def":          "vim/E600",
		"src/testdir/test_vim9_script.vim:2437:52346/def":          "vim/E600",
		"src/testdir/test_vim9_script.vim:2438:52425/def":          "vim/E600",
		"src/testdir/test_vim9_script.vim:2452:52714/def":          "vim/E600",
		"src/testdir/test_vim9_script.vim:2453:52771/def":          "vim/E600",
		"src/testdir/test_vim9_script.vim:2454:52839/def":          "vim/E600",
		"src/testdir/test_vim9_script.vim:2455:52916/def":          "vim/E600",
		"src/testdir/test_vim9_script.vim:2456:53004/def":          "vim/E607",
		"src/testdir/test_vim9_script.vim:2457:53103/def":          "vim/E607",
		"src/testdir/test_vim9_script.vim:2968:64339/def":          "vim/E582",
		"src/testdir/test_vim9_script.vim:2969:64386/def":          "vim/E581",
		"src/testdir/test_vim9_script.vim:2970:64426/def":          "vim/E580",
		"src/testdir/test_vim9_script.vim:2972:64528/def":          "vim/E171",
		"src/testdir/test_vim9_script.vim:2996:64903/def":          "vim/E583",
		"src/testdir/test_vim9_script.vim:3006:65058/def":          "vim/E584",
		"src/testdir/test_vim9_script.vim:3968:85342/def":          "vim/E588",
		"src/testdir/test_vim9_script.vim:3969:85384/def":          "vim/E170",
		"src/testdir/test_vim9_script.vim:4355:94319/def":          "vim/E588",
		"src/testdir/test_vim9_script.vim:4360:94555/def":          "vim/E170",
		"src/testdir/test_vim9_script.vim:3958:84651/vim9-script":  "vim/E690",
		"src/testdir/test_vim9_script.vim:3959:84712/vim9-script":  "vim/E690",
		"src/testdir/test_vim9_script.vim:3958:84651/def":          "vim/E1097",
		"src/testdir/test_vim9_script.vim:3959:84712/def":          "vim/E1097",
		"src/testdir/test_vim9_script.vim:3961:84838/vim9-script":  "vim/E690",
		"src/testdir/test_vim9_script.vim:3961:84838/def":          "vim/E690",
		"src/testdir/test_vim9_script.vim:3962:84900/def":          "vim/E690",
		"src/testdir/test_vim9_script.vim:3962:84900/vim9-script":  "vim/E690",
		"src/testdir/test_vim9_script.vim:3998:86214/def":          "vim/E1059",
		"src/testdir/test_vim9_script.vim:3998:86214/vim9-script":  "vim/E1059",
		"src/testdir/test_vim9_script.vim:3273:69988/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:2979:64658/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:2987:64789/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:4367:94691/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:3328:71278/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:3350:71801/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:4448:96196/script":       "vim/E488",
		"src/testdir/test_vim9_script.vim:4452:96282/script":       "vim/E488",
		"src/testdir/test_vim9_script.vim:4455:96347/script":       "vim/E488",
		"src/testdir/test_vim9_script.vim:4461:96448/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:4464:96509/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:4470:96605/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:4490:97047/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:4526:97800/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:4545:98173/def":          "vim/E488",
		"src/testdir/test_vim9_script.vim:4577:98798/script":       "vim/E416",
		"src/testdir/test_vim9_script.vim:4585:98984/script":       "vim/E413",
		"src/testdir/test_vim9_script.vim:4596:99293/script":       "vim/E416",
		"src/testdir/test_vim9_script.vim:4669:100919/script":      "vim/E475",
		"src/testdir/test_vim9_script.vim:4678:101134/script":      "vim/E789",
		"src/testdir/test_vim9_script.vim:4687:101341/script":      "vim/E402",
		"src/testdir/test_vim9_script.vim:4696:101565/script":      "vim/E475",
		"src/testdir/test_vim9_script.vim:4700:101690/script":      "vim/E406",
		"src/testdir/test_vim9_script.vim:4704:101807/script":      "vim/E475",
		"src/testdir/test_vim9_script.vim:4713:102039/script":      "vim/E402",
		"src/testdir/test_vim9_script.vim:4722:102249/script":      "vim/E404",
		"src/testdir/test_vim9_script.vim:4730:102438/script":      "vim/E404",
		"src/testdir/test_vim9_script.vim:4739:102650/script":      "vim/E475",
		"src/testdir/test_vim9_script.vim:4817:104452/script":      "vim/E488",
		"src/testdir/test_vim9_script.vim:4828:104697/script":      "vim/E488",
		"src/testdir/test_vim9_script.vim:4833:104799/script":      "vim/E488",
		"src/testdir/test_vim9_script.vim:4837:104889/script":      "vim/E488",
		"src/testdir/test_vim9_script.vim:4858:105290/script":      "vim/E488",
		"src/testdir/test_vim9_script.vim:4968:107582/script":      "vim/E488",
		"src/testdir/test_vim9_script.vim:4882:105730/script":      "vim/E399",
		"src/testdir/test_vim9_script.vim:4982:107851/script":      "vim/E488",
		"src/testdir/test_vim9_script.vim:4989:107977/script":      "vim/E488",
		"src/testdir/test_vim9_script.vim:5003:108237/script":      "vim/E488",
		"src/testdir/test_vim9_script.vim:5017:108506/script":      "vim/E488",
		"src/testdir/test_vim9_script.vim:5739:129289/def":         "vim/E1100",
		"src/testdir/test_vim9_script.vim:5739:129289/vim9-script": "vim/E1100",
		"src/testdir/test_vim9_script.vim:5749:129456/def":         "vim/E1100",
		"src/testdir/test_vim9_script.vim:5749:129456/vim9-script": "vim/E1100",
		"src/testdir/test_vim9_script.vim:5754:129537/def":         "vim/E1100",
		"src/testdir/test_vim9_script.vim:5754:129537/vim9-script": "vim/E1100",
		"src/testdir/test_vim9_script.vim:5759:129618/def":         "vim/E1100",
		"src/testdir/test_vim9_script.vim:5759:129618/vim9-script": "vim/E1100",
		"src/testdir/test_vim9_script.vim:5764:129701/def":         "vim/E1100",
		"src/testdir/test_vim9_script.vim:5764:129701/vim9-script": "vim/E1100",
		"src/testdir/test_vim9_script.vim:5744:129376/def":         "vim/E481",
		"src/testdir/test_vim9_script.vim:5744:129376/vim9-script": "vim/E481",
		"src/testdir/test_vim9_script.vim:5827:131126/def":         "vim/E1205",
		"src/testdir/test_vim9_script.vim:5827:131126/vim9-script": "vim/E1205",
		"src/testdir/test_vim9_script.vim:5832:131275/def":         "vim/E1205",
		"src/testdir/test_vim9_script.vim:5832:131275/vim9-script": "vim/E1205",
		"src/testdir/test_vim9_script.vim:5837:131425/def":         "vim/E1205",
		"src/testdir/test_vim9_script.vim:5837:131425/vim9-script": "vim/E1205",
		"src/testdir/test_vim9_script.vim:5842:131575/def":         "vim/E1205",
		"src/testdir/test_vim9_script.vim:5842:131575/vim9-script": "vim/E1205",
		"src/testdir/test_vim9_script.vim:5883:132436/def":         "vim/E1205",
		"src/testdir/test_vim9_script.vim:5883:132436/vim9-script": "vim/E1205",
		"src/testdir/test_vim9_script.vim:5888:132583/def":         "vim/E1205",
		"src/testdir/test_vim9_script.vim:5888:132583/vim9-script": "vim/E1205",
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
		"src/testdir/test_vim9_expr.vim:2847:84944/def":            "vim/E488",
		"src/testdir/test_vim9_expr.vim:2847:84944/vim9-script":    "vim/E488",
		"src/testdir/test_vim9_expr.vim:2867:85384/def":            "vim/E1171",
		"src/testdir/test_vim9_expr.vim:2868:85454/script":         "vim/E1171",
		"src/testdir/test_vim9_expr.vim:2875:85631/def":            "vim/E1145",
		"src/testdir/test_vim9_expr.vim:2876:85706/script":         "vim/E1145",
		"src/testdir/test_vim9_expr.vim:2980:88648/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2980:88648/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2981:88714/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2981:88714/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2982:88781/def":            "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2982:88781/vim9-script":    "vim/E1004",
		"src/testdir/test_vim9_expr.vim:2988:89122/def":            "vim/E1069",
		"src/testdir/test_vim9_expr.vim:2988:89122/vim9-script":    "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3000:89675/def":            "vim/E722",
		"src/testdir/test_vim9_expr.vim:3000:89675/vim9-script":    "vim/E722",
		"src/testdir/test_vim9_expr.vim:3001:89758/def":            "vim/E720",
		"src/testdir/test_vim9_expr.vim:3001:89758/vim9-script":    "vim/E720",
		"src/testdir/test_vim9_expr.vim:3003:89842/def":            "vim/E696",
		"src/testdir/test_vim9_expr.vim:3003:89842/vim9-script":    "vim/E696",
		"src/testdir/test_vim9_expr.vim:3158:93832/def":            "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3158:93832/vim9-script":    "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3159:93898/def":            "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3159:93898/vim9-script":    "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3160:93966/def":            "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3160:93966/vim9-script":    "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3161:94037/def":            "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3161:94037/vim9-script":    "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3163:94109/def":            "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3163:94109/vim9-script":    "vim/E1170",
		"src/testdir/test_vim9_expr.vim:3165:94182/def":            "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3165:94182/vim9-script":    "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3166:94244/def":            "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3166:94244/vim9-script":    "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3167:94308/def":            "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3167:94308/vim9-script":    "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3168:94371/def":            "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3168:94371/vim9-script":    "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3169:94441/def":            "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3169:94441/vim9-script":    "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3171:94510/def":            "vim/E720",
		"src/testdir/test_vim9_expr.vim:3171:94510/vim9-script":    "vim/E720",
		"src/testdir/test_vim9_expr.vim:3172:94571/def":            "vim/E722",
		"src/testdir/test_vim9_expr.vim:3172:94571/vim9-script":    "vim/E722",
		"src/testdir/test_vim9_expr.vim:3173:94647/def":            "vim/E723",
		"src/testdir/test_vim9_expr.vim:3174:94702/script":         "vim/E723",
		"src/testdir/test_vim9_expr.vim:3192:95809/def":            "vim/E723",
		"src/testdir/test_vim9_expr.vim:3193:95858/script":         "vim/E723",
		"src/testdir/test_vim9_expr.vim:3197:96065/def":            "vim/E488",
		"src/testdir/test_vim9_expr.vim:3198:96153/def":            "vim/E488",
		"src/testdir/test_vim9_expr.vim:3243:97058/def":            "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3243:97058/vim9-script":    "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3248:97166/def":            "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3248:97166/vim9-script":    "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3253:97268/def":            "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3253:97268/vim9-script":    "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3258:97368/def":            "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3258:97368/vim9-script":    "vim/E1069",
		"src/testdir/test_vim9_expr.vim:3263:97478/def":            "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3263:97478/vim9-script":    "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3288:98076/def":            "vim/E1139",
		"src/testdir/test_vim9_expr.vim:3288:98076/vim9-script":    "vim/E1139",
		"src/testdir/test_vim9_expr.vim:3296:98233/def":            "vim/E1139",
		"src/testdir/test_vim9_expr.vim:3296:98233/vim9-script":    "vim/E1139",
		"src/testdir/test_vim9_expr.vim:3301:98329/def":            "vim/E723",
		"src/testdir/test_vim9_expr.vim:3302:98369/script":         "vim/E15",
		"src/testdir/test_vim9_expr.vim:3310:98517/def":            "vim/E723",
		"src/testdir/test_vim9_expr.vim:3310:98517/vim9-script":    "vim/E723",
		"src/testdir/test_vim9_expr.vim:3398:100389/def":           "vim/E1002",
		"src/testdir/test_vim9_expr.vim:3398:100389/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3646:107341/def":           "vim/E1002",
		"src/testdir/test_vim9_expr.vim:3646:107341/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3647:107411/def":           "vim/E1002",
		"src/testdir/test_vim9_expr.vim:3647:107411/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3673:107954/def":           "vim/E354",
		"src/testdir/test_vim9_expr.vim:3673:107954/vim9-script":   "vim/E354",
		"src/testdir/test_vim9_expr.vim:3674:108012/def":           "vim/E354",
		"src/testdir/test_vim9_expr.vim:3674:108012/vim9-script":   "vim/E354",
		"src/testdir/test_vim9_expr.vim:3675:108070/def":           "vim/E354",
		"src/testdir/test_vim9_expr.vim:3675:108070/vim9-script":   "vim/E354",
		"src/testdir/test_vim9_expr.vim:3676:108128/def":           "vim/E354",
		"src/testdir/test_vim9_expr.vim:3676:108128/vim9-script":   "vim/E354",
		"src/testdir/test_vim9_expr.vim:3779:110823/def":           "vim/E1097",
		"src/testdir/test_vim9_expr.vim:3779:110823/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3780:110925/def":           "vim/E110",
		"src/testdir/test_vim9_expr.vim:3780:110925/vim9-script":   "vim/E110",
		"src/testdir/test_vim9_expr.vim:3812:111601/def":           "vim/E15",
		"src/testdir/test_vim9_expr.vim:3812:111601/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3817:111701/def":           "vim/E15",
		"src/testdir/test_vim9_expr.vim:3817:111701/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3822:111801/def":           "vim/E15",
		"src/testdir/test_vim9_expr.vim:3822:111801/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3827:111901/def":           "vim/E15",
		"src/testdir/test_vim9_expr.vim:3827:111901/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3832:112002/def":           "vim/E15",
		"src/testdir/test_vim9_expr.vim:3832:112002/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3837:112103/def":           "vim/E15",
		"src/testdir/test_vim9_expr.vim:3837:112103/vim9-script":   "vim/E15",
		"src/testdir/test_vim9_expr.vim:3898:113491/def":           "vim/E107",
		"src/testdir/test_vim9_expr.vim:3898:113491/vim9-script":   "vim/E107",
		"src/testdir/test_vim9_expr.vim:3902:113675/def":           "vim/E1123",
		"src/testdir/test_vim9_expr.vim:3903:113767/def":           "vim/E1068",
		"src/testdir/test_vim9_expr.vim:3903:113767/vim9-script":   "vim/E1068",
		"src/testdir/test_vim9_expr.vim:4059:117757/def":           "vim/E15",
		"src/testdir/test_vim9_expr.vim:4189:120959/def":           "vim/E1097",
		"src/testdir/test_vim9_expr.vim:4190:121015/script":        "vim/E110",
		"src/testdir/test_vim9_expr.vim:4198:121454/def":           "vim/E1002",
		"src/testdir/test_vim9_expr.vim:4198:121454/vim9-script":   "vim/E1002",
		"src/testdir/test_vim9_expr.vim:4199:121517/def":           "vim/E354",
		"src/testdir/test_vim9_expr.vim:4199:121517/vim9-script":   "vim/E354",
		"src/testdir/test_vim9_expr.vim:4201:121581/def":           "vim/E697",
		"src/testdir/test_vim9_expr.vim:4202:121638/script":        "vim/E696",
		"src/testdir/test_vim9_expr.vim:4206:121797/def":           "vim/E488",
		"src/testdir/test_vim9_expr.vim:4206:121797/vim9-script":   "vim/E488",
		"src/testdir/test_vim9_expr.vim:4207:121868/def":           "vim/E107",
		"src/testdir/test_vim9_expr.vim:4207:121868/vim9-script":   "vim/E107",
		"src/testdir/test_vim9_expr.vim:4226:122999/def":           "vim/E488",
		"src/testdir/test_vim9_expr.vim:4226:122999/vim9-script":   "vim/E260",
		"src/testdir/test_vim9_expr.vim:4228:123127/def":           "vim/E697",
		"src/testdir/test_vim9_expr.vim:4229:123187/script":        "vim/E696",
		"src/testdir/test_vim9_expr.vim:4231:123261/def":           "vim/E723",
		"src/testdir/test_vim9_expr.vim:4232:123317/script":        "vim/E722",
		"src/testdir/test_vim9_expr.vim:4234:123391/def":           "vim/E723",
		"src/testdir/test_vim9_expr.vim:4235:123455/script":        "vim/E722",
		"src/testdir/test_vim9_expr.vim:4237:123533/def":           "vim/E1170",
		"src/testdir/test_vim9_expr.vim:4346:126384/def":           "vim/E1097",
		"src/testdir/test_vim9_expr.vim:4347:126425/script":        "vim/E15",
		"src/testdir/test_vim9_expr.vim:4357:126658/def":           "vim/E1097",
		"src/testdir/test_vim9_expr.vim:4358:126699/script":        "vim/E111",
		"src/testdir/test_vim9_expr.vim:4364:126827/vim9-script":   "vim/E111",
		"src/testdir/test_vim9_expr.vim:4364:126827/def":           "vim/E111",
		"src/testdir/test_vim9_expr.vim:4511:130673/def":           "vim/E1127",
		"src/testdir/test_vim9_expr.vim:4515:130780/def":           "vim/E107",
		"src/testdir/test_vim9_expr.vim:4515:130780/vim9-script":   "vim/E107",
		"src/testdir/test_vim9_expr.vim:4520:130997/def":           "vim/E488",
		"src/testdir/test_vim9_expr.vim:4520:130997/vim9-script":   "vim/E488",
		"src/testdir/test_vim9_expr.vim:4521:131064/def":           "vim/E488",
		"src/testdir/test_vim9_expr.vim:4521:131064/vim9-script":   "vim/E488",
		"src/testdir/test_vim9_interface.vim:159:3829/script":      "vim/E476",
		"src/testdir/test_vim9_interface.vim:167:4035/script":      "vim/E476",
		"src/testdir/test_vim9_expr.vim:4531:131555/def":           "vim/E110",
		"src/testdir/test_listdict.vim:529:14207/def":              "vim/E1004",
		"src/testdir/test_listdict.vim:1533:48036/legacy":          "vim/E15",
		"src/testdir/test_listdict.vim:1533:48036/def":             "vim/E1127",
		"src/testdir/test_listdict.vim:1533:48036/vim9-script":     "vim/E15",
		"src/testdir/test_listdict.vim:1542:48528/def":             "vim/E15",
		"src/testdir/test_listdict.vim:1542:48528/vim9-script":     "vim/E15",
		"src/testdir/test_listdict.vim:1544:48608/def":             "vim/E1097",
		"src/testdir/test_tuple.vim:138:3809/def":                  "vim/E1004",
		"src/testdir/test_tuple.vim:143:3972/vim9-script":          "vim/E1068",
		"src/testdir/test_tuple.vim:151:4245/vim9-script":          "vim/E1068",
		"src/testdir/test_usercommands.vim:328:8893/script":        "vim/E1208",
		"src/testdir/test_usercommands.vim:334:9034/script":        "vim/E1208",
		"src/testdir/test_usercommands.vim:1007:34285/script":      "vim/E1026",
		"src/testdir/test_vim9_script.vim:386:9535/def":            "vim/E1025",
		"src/testdir/test_vim9_script.vim:387:9573/def":            "vim/E1026",
		"src/testdir/test_usercommands.vim:1046:35079/script":      "vim/E1128",
		"src/testdir/test_vim9_script.vim:4777:103460/script":      "vim/E182",
		"src/testdir/test_vim9_script.vim:4781:103555/script":      "vim/E182",
		"src/testdir/test_vim9_script.vim:4356:94363/def":          "vim/E586",
		"src/testdir/test_vim9_script.vim:4357:94407/def":          "vim/E586",
		"src/testdir/test_vim9_script.vim:4358:94462/def":          "vim/E587",
		"src/testdir/test_vim9_script.vim:4359:94503/def":          "vim/E587",
		"src/testdir/test_vim9_script.vim:2459:53214/def":          "vim/E1143",
		"src/testdir/test_vim9_script.vim:3960:84774/def":          "vim/E1097",
		"src/testdir/test_vim9_script.vim:3960:84774/vim9-script":  "vim/E15",
		"src/testdir/test_vim9_script.vim:4606:99506/script":       "vim/E488",
		"src/testdir/test_vim9_script.vim:4623:99889/script":       "vim/E475",
		"src/testdir/test_vim9_script.vim:3714:78882/def":          "vim/E1059",
		"src/testdir/test_vim9_script.vim:3714:78882/vim9-script":  "vim/E1059",
		"src/testdir/test_vim9_script.vim:6514:146587/script":      "vim/E1059",
		"src/testdir/test_vim9_func.vim:2484:56080/script":         "vim/E1077",
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
	checkOfficialParserCaseCorpus(t, corpus, readOfficialCorpusLock(t))
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
	for part := range strings.SplitSeq(filter, ",") {
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
	path := filepath.Join("..", "..", "testdata", "official", vimdata.VimSourceTag+"-parser-cases.json.gz")
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

func checkOfficialParserCaseCorpus(t *testing.T, corpus officialParserCaseCorpus, lock officialCorpusLock) {
	t.Helper()
	if corpus.SchemaVersion != officialParserCasesSchemaVersion || corpus.Tag != lock.Tag || corpus.Commit != lock.Commit {
		t.Fatalf("unexpected official parser-case provenance: schema = %d, tag = %q, commit = %q", corpus.SchemaVersion, corpus.Tag, corpus.Commit)
	}
	if corpus.Manifest != officialParserCasesManifest || corpus.ManifestSHA256 != lock.ManifestSHA256 {
		t.Fatalf("unexpected parser-case manifest: name = %q, sha256 = %q", corpus.Manifest, corpus.ManifestSHA256)
	}
	manifestPath := filepath.Join("..", "..", "testdata", "official", corpus.Manifest)
	manifestSource, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest officialParserFileManifest
	if err := json.Unmarshal(manifestSource, &manifest); err != nil {
		t.Fatal(err)
	}
	encodedManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(encodedManifest)); got != corpus.ManifestSHA256 {
		t.Fatalf("parser-case manifest typed hash = %s, want %s", got, corpus.ManifestSHA256)
	}
	if len(corpus.Files) != lock.ParserCases.Files || !sort.StringsAreSorted(corpus.Files) {
		t.Fatalf("unexpected parser-case files: count = %d, sorted = %t", len(corpus.Files), sort.StringsAreSorted(corpus.Files))
	}
	for index := 1; index < len(corpus.Files); index++ {
		if corpus.Files[index] == corpus.Files[index-1] {
			t.Fatalf("duplicate parser-case file %q", corpus.Files[index])
		}
	}
	if len(corpus.Records) != lock.ParserCases.Records {
		t.Fatalf("unexpected parser-case records: %d", len(corpus.Records))
	}
	if corpus.Summary != lock.ParserCases.Summary {
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
