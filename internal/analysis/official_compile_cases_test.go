package analysis

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/chemzqm/vimls-go/internal/syntax"
)

const (
	officialCompileSchemaVersion = 1
	officialCompileVimTag        = "v9.2.1015"
	officialCompileVimCommit     = "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae"
	officialCompileFileCount     = 14
	officialCompileRecordCount   = 1770
	officialCompileExtracted     = 1762
	officialCompileSkipped       = 8
	officialCompileExpected      = 1662
	officialCompileUnresolved    = 100
)

var officialCompileCaseFilter = flag.String("official-compile-case", "", "comma-separated substrings selecting official defcompile cases, or all")

type officialCompileCorpus struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Tag           string                  `json:"tag"`
	Commit        string                  `json:"commit"`
	Files         []string                `json:"files"`
	Records       []officialCompileRecord `json:"records"`
	Summary       officialCompileSummary  `json:"summary"`
}

type officialCompileRecord struct {
	ID            string `json:"id"`
	Path          string `json:"path"`
	Line          int    `json:"line"`
	Offset        int    `json:"offset"`
	CallStart     int    `json:"callStart"`
	CallEnd       int    `json:"callEnd"`
	Helper        string `json:"helper"`
	InputKind     string `json:"inputKind"`
	InputStart    int    `json:"inputStart"`
	InputEnd      int    `json:"inputEnd"`
	ErrorArgument string `json:"errorArgument"`
	ExpectedCode  string `json:"expectedCode"`
	Source        string `json:"source"`
	Disposition   string `json:"disposition"`
	Reason        string `json:"reason"`
}

type officialCompileSummary struct {
	Calls           int `json:"calls"`
	ExtractedCalls  int `json:"extractedCalls"`
	SkippedCalls    int `json:"skippedCalls"`
	ExpectedCodes   int `json:"expectedCodes"`
	UnresolvedCodes int `json:"unresolvedCodes"`
	DirectLists     int `json:"directLists"`
	Heredocs        int `json:"heredocs"`
	ListAssignments int `json:"listAssignments"`
	ListConcats     int `json:"listConcats"`
}

func TestOfficialVimCompileCases(t *testing.T) {
	corpus := readOfficialCompileCases(t)
	if corpus.SchemaVersion != officialCompileSchemaVersion || corpus.Tag != officialCompileVimTag || corpus.Commit != officialCompileVimCommit {
		t.Fatalf("unexpected official compile provenance: schema=%d tag=%q commit=%q", corpus.SchemaVersion, corpus.Tag, corpus.Commit)
	}
	wantSummary := officialCompileSummary{
		Calls: officialCompileRecordCount, ExtractedCalls: officialCompileExtracted, SkippedCalls: officialCompileSkipped,
		ExpectedCodes: officialCompileExpected, UnresolvedCodes: officialCompileUnresolved,
		DirectLists: 1403, Heredocs: 349, ListConcats: 10,
	}
	if len(corpus.Files) != officialCompileFileCount || !sort.StringsAreSorted(corpus.Files) || len(corpus.Records) != officialCompileRecordCount || corpus.Summary != wantSummary {
		t.Fatalf("official compile corpus files=%d records=%d summary=%+v", len(corpus.Files), len(corpus.Records), corpus.Summary)
	}
	seen := make(map[string]struct{}, len(corpus.Records))
	for index, record := range corpus.Records {
		if _, duplicate := seen[record.ID]; duplicate {
			t.Fatalf("duplicate official compile id %q", record.ID)
		}
		seen[record.ID] = struct{}{}
		if index > 0 && (corpus.Records[index-1].Path > record.Path || corpus.Records[index-1].Path == record.Path && corpus.Records[index-1].Offset >= record.Offset) {
			t.Fatalf("official compile records are not ordered at %d", index)
		}
		if record.Path == "" || record.Line < 1 || record.CallEnd <= record.CallStart || !strings.HasSuffix(record.ID, "/defcompile") {
			t.Fatalf("invalid official compile record %d: %#v", index, record)
		}
		switch record.Disposition {
		case "extracted":
			if record.Source == "" || !strings.HasSuffix(record.Source, "defcompile\n") || record.InputKind == "" || record.InputEnd <= record.InputStart || record.Reason != "" {
				t.Fatalf("invalid extracted official compile record %s: %#v", record.ID, record)
			}
			if record.ExpectedCode != "" && !strings.HasPrefix(record.ExpectedCode, "vim/E") {
				t.Fatalf("%s: invalid expected code %q", record.ID, record.ExpectedCode)
			}
		case "skipped":
			if record.Source != "" || record.Reason == "" {
				t.Fatalf("invalid skipped official compile record %s: %#v", record.ID, record)
			}
		default:
			t.Fatalf("%s: unsupported disposition %q", record.ID, record.Disposition)
		}
	}
}

func TestOfficialVimCompileFailureTriage(t *testing.T) {
	if strings.TrimSpace(*officialCompileCaseFilter) == "" {
		t.Skip("use -args -official-compile-case=<path,line,code,all> to select cases")
	}
	type coverage struct{ total, ready, mapping, missing int }
	byCode := make(map[string]coverage)
	selected := 0
	for _, record := range readOfficialCompileCases(t).Records {
		if record.Disposition != "extracted" || record.ExpectedCode == "" || !officialCompileCaseSelected(record) {
			continue
		}
		selected++
		diagnostics := analyzeOfficialCompileSource(t, record)
		status := byCode[record.ExpectedCode]
		status.total++
		switch {
		case hasOfficialCompileCode(diagnostics, record.ExpectedCode):
			status.ready++
		case len(diagnostics) == 0:
			status.missing++
		default:
			status.mapping++
		}
		byCode[record.ExpectedCode] = status
		if *officialCompileCaseFilter != "all" && !hasOfficialCompileCode(diagnostics, record.ExpectedCode) {
			t.Logf("%s want=%s got=%s source=%q", record.ID, record.ExpectedCode, officialCompileCodes(diagnostics), compileSourcePreview(record.Source))
		}
	}
	if selected == 0 {
		t.Fatalf("no official compile case matches -official-compile-case=%q", *officialCompileCaseFilter)
	}
	codes := make([]string, 0, len(byCode))
	for code := range byCode {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	total := coverage{}
	for _, code := range codes {
		status := byCode[code]
		total.total += status.total
		total.ready += status.ready
		total.mapping += status.mapping
		total.missing += status.missing
		t.Logf("%s total=%d ready=%d mapping=%d missing=%d", code, status.total, status.ready, status.mapping, status.missing)
	}
	t.Logf("compile triage codes=%d total=%d ready=%d mapping=%d missing=%d", len(codes), total.total, total.ready, total.mapping, total.missing)
}

func TestOfficialVimCompileFailures(t *testing.T) {
	supported := officialCompileSupportedCodes()
	if len(supported) == 0 {
		t.Skip("no complete official compile diagnostic family has been migrated")
	}
	seen := make(map[string]int, len(supported))
	for _, record := range readOfficialCompileCases(t).Records {
		if record.Disposition != "extracted" || !supported[record.ExpectedCode] || !officialCompileCaseSelectedByDefault(record) {
			continue
		}
		diagnostics := analyzeOfficialCompileSource(t, record)
		if !hasOfficialCompileCode(diagnostics, record.ExpectedCode) {
			t.Fatalf("%s diagnostics=%#v, want %s", record.ID, diagnostics, record.ExpectedCode)
		}
		seen[record.ExpectedCode]++
	}
	for code := range supported {
		if seen[code] == 0 {
			t.Fatalf("supported compile code %s has no pinned official case", code)
		}
	}
}

func officialCompileSupportedCodes() map[string]bool {
	// A code is added only when every explicit :defcompile record carrying that
	// code is detected. This prevents a convenient example from standing in for
	// unimplemented forms of the same Vim compiler diagnostic.
	return map[string]bool{
		"vim/E15":   true,
		"vim/E16":   true,
		"vim/E107":  true,
		"vim/E109":  true,
		"vim/E110":  true,
		"vim/E111":  true,
		"vim/E114":  true,
		"vim/E115":  true,
		"vim/E170":  true,
		"vim/E171":  true,
		"vim/E274":  true,
		"vim/E354":  true,
		"vim/E481":  true,
		"vim/E580":  true,
		"vim/E581":  true,
		"vim/E582":  true,
		"vim/E583":  true,
		"vim/E584":  true,
		"vim/E586":  true,
		"vim/E587":  true,
		"vim/E588":  true,
		"vim/E600":  true,
		"vim/E602":  true,
		"vim/E603":  true,
		"vim/E606":  true,
		"vim/E607":  true,
		"vim/E690":  true,
		"vim/E696":  true,
		"vim/E697":  true,
		"vim/E720":  true,
		"vim/E722":  true,
		"vim/E723":  true,
		"vim/E973":  true,
		"vim/E1002": true,
		"vim/E1004": true,
		"vim/E1005": true,
		"vim/E1007": true,
		"vim/E1008": true,
		"vim/E1009": true,
		"vim/E1021": true,
		"vim/E1022": true,
		"vim/E1025": true,
		"vim/E1026": true,
		"vim/E1032": true,
		"vim/E1038": true,
		"vim/E1050": true,
		"vim/E1059": true,
		"vim/E1065": true,
		"vim/E1066": true,
		"vim/E1067": true,
		"vim/E1068": true,
		"vim/E1069": true,
		"vim/E1080": true,
		"vim/E1082": true,
		"vim/E1083": true,
		"vim/E1087": true,
		"vim/E1097": true,
		"vim/E1100": true,
		"vim/E1104": true,
		"vim/E1123": true,
		"vim/E1125": true,
		"vim/E1126": true,
		"vim/E1127": true,
		"vim/E1139": true,
		"vim/E1143": true,
		"vim/E1144": true,
		"vim/E1145": true,
		"vim/E1157": true,
		"vim/E1170": true,
		"vim/E1171": true,
		"vim/E1172": true,
		"vim/E1176": true,
		"vim/E1180": true,
		"vim/E1185": true,
		"vim/E1202": true,
		"vim/E1241": true,
		"vim/E1242": true,
		"vim/E1278": true,
		"vim/E1279": true,
		"vim/E1539": true,
	}
}

func analyzeOfficialCompileSource(t *testing.T, record officialCompileRecord) []syntax.Diagnostic {
	t.Helper()
	file := syntax.Parse(record.Source)
	if file.Source != record.Source || len(file.Commands) == 0 {
		t.Fatalf("%s: parser did not retain official compile source", record.ID)
	}
	diagnostics := append([]syntax.Diagnostic(nil), file.Diagnostics...)
	diagnostics = append(diagnostics, Analyze(file).Diagnostics...)
	for _, diagnostic := range diagnostics {
		if diagnostic.Span.Start < 0 || diagnostic.Span.End < diagnostic.Span.Start || diagnostic.Span.End > len(record.Source) {
			t.Fatalf("%s: out-of-bounds diagnostic %#v", record.ID, diagnostic)
		}
	}
	return diagnostics
}

func hasOfficialCompileCode(diagnostics []syntax.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func officialCompileCodes(diagnostics []syntax.Diagnostic) string {
	codes := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		codes = append(codes, diagnostic.Code)
	}
	return strings.Join(codes, ",")
}

func officialCompileCaseSelected(record officialCompileRecord) bool {
	filter := strings.TrimSpace(*officialCompileCaseFilter)
	if filter == "all" {
		return true
	}
	for _, part := range strings.Split(filter, ",") {
		part = strings.TrimSpace(part)
		if part != "" && (strings.Contains(record.ID, part) || strings.Contains(record.ExpectedCode, part)) {
			return true
		}
	}
	return false
}

func officialCompileCaseSelectedByDefault(record officialCompileRecord) bool {
	if strings.TrimSpace(*officialCompileCaseFilter) == "" {
		return true
	}
	return officialCompileCaseSelected(record)
}

func compileSourcePreview(source string) string {
	const limit = 300
	if len(source) <= limit {
		return source
	}
	return source[:limit] + "..."
}

func readOfficialCompileCases(t *testing.T) officialCompileCorpus {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "official", "v9.2.1015-compile-cases.json.gz")
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
	var corpus officialCompileCorpus
	if err := json.NewDecoder(io.LimitReader(reader, 64<<20)).Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	return corpus
}
