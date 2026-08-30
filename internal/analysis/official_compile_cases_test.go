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
	officialCompileSchemaVersion       = 2
	officialCompileVimTag              = "v9.2.1015"
	officialCompileVimCommit           = "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae"
	officialCompileFileCount           = 14
	officialCompileRecordCount         = 1770
	officialCompileExtracted           = 1762
	officialCompileSkipped             = 8
	officialCompileCaseCount           = 3171
	officialCompileExpected            = 2964
	officialCompileUnresolved          = 207
	officialCompileRepresentativeLimit = 30
)

var officialCompileCaseFilter = flag.String("official-compile-case", "", "comma-separated substrings selecting official static compile variants, or all")

type officialCompileCorpus struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Tag           string                  `json:"tag"`
	Commit        string                  `json:"commit"`
	Files         []string                `json:"files"`
	Records       []officialCompileRecord `json:"records"`
	Summary       officialCompileSummary  `json:"summary"`
}

type officialCompileRecord struct {
	ID            string                   `json:"id"`
	Path          string                   `json:"path"`
	Line          int                      `json:"line"`
	Offset        int                      `json:"offset"`
	CallStart     int                      `json:"callStart"`
	CallEnd       int                      `json:"callEnd"`
	Helper        string                   `json:"helper"`
	InputKind     string                   `json:"inputKind"`
	InputStart    int                      `json:"inputStart"`
	InputEnd      int                      `json:"inputEnd"`
	ErrorArgument string                   `json:"errorArgument"`
	Disposition   string                   `json:"disposition"`
	Reason        string                   `json:"reason"`
	Cases         []officialCompileVariant `json:"cases"`
}

type officialCompileVariant struct {
	Name         string `json:"name"`
	Context      string `json:"context"`
	ExpectedCode string `json:"expectedCode"`
	Source       string `json:"source"`
}

type officialCompileCase struct {
	Record  officialCompileRecord
	Variant officialCompileVariant
}

type officialCompileSummary struct {
	Calls           int `json:"calls"`
	ExtractedCalls  int `json:"extractedCalls"`
	SkippedCalls    int `json:"skippedCalls"`
	Cases           int `json:"cases"`
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
		Cases: officialCompileCaseCount, ExpectedCodes: officialCompileExpected, UnresolvedCodes: officialCompileUnresolved,
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
		if record.ID == "" || record.Path == "" || record.Line < 1 || record.CallEnd <= record.CallStart {
			t.Fatalf("invalid official compile record %d: %#v", index, record)
		}
		switch record.Disposition {
		case "extracted":
			if len(record.Cases) == 0 || record.InputKind == "" || record.InputEnd <= record.InputStart || record.Reason != "" {
				t.Fatalf("invalid extracted official compile record %s: %#v", record.ID, record)
			}
			for _, variant := range record.Cases {
				validContext := variant.Context == "def" && strings.HasSuffix(variant.Source, "defcompile\n") || variant.Context == "script" && strings.HasPrefix(variant.Source, "vim9script\n")
				if variant.Name == "" || variant.Source == "" || !validContext {
					t.Fatalf("%s: invalid compile variant %#v", record.ID, variant)
				}
				if variant.ExpectedCode != "" && !strings.HasPrefix(variant.ExpectedCode, "vim/E") {
					t.Fatalf("%s/%s: invalid expected code %q", record.ID, variant.Name, variant.ExpectedCode)
				}
			}
		case "skipped":
			if len(record.Cases) != 0 || record.Reason == "" {
				t.Fatalf("invalid skipped official compile record %s: %#v", record.ID, record)
			}
		default:
			t.Fatalf("%s: unsupported disposition %q", record.ID, record.Disposition)
		}
	}
}

func TestOfficialVimCompileStaticAnalysisExclusions(t *testing.T) {
	supported := officialCompileSupportedCodes()
	for code, reason := range officialCompileStaticAnalysisExcludedCodes {
		if reason == "" {
			t.Fatalf("static-analysis exclusion %s has no reason", code)
		}
		if supported[code] {
			t.Fatalf("static-analysis exclusion %s is also marked supported", code)
		}
	}
}

func TestOfficialVimCompileRepresentativeCases(t *testing.T) {
	corpus := readOfficialCompileCases(t)
	cases := officialCompileRepresentativeCases(corpus, func(officialCompileRecord, officialCompileVariant) bool { return true })
	counts := make(map[string]int)
	contexts := make(map[string]map[string]bool)
	selectedRecords := make(map[string]bool)
	for _, testCase := range cases {
		code := testCase.Variant.ExpectedCode
		counts[code]++
		if counts[code] > officialCompileRepresentativeLimit {
			t.Fatalf("%s selected %d representatives, limit is %d", code, counts[code], officialCompileRepresentativeLimit)
		}
		if contexts[code] == nil {
			contexts[code] = make(map[string]bool)
		}
		contexts[code][testCase.Variant.Context] = true
		selectedRecords[testCase.Record.ID] = true
	}
	if counts["vim/E1013"] != officialCompileRepresentativeLimit {
		t.Fatalf("E1013 representatives = %d, want %d", counts["vim/E1013"], officialCompileRepresentativeLimit)
	}
	if !contexts["vim/E1013"]["def"] || !contexts["vim/E1013"]["script"] {
		t.Fatalf("E1013 representative contexts = %#v, want def and script", contexts["vim/E1013"])
	}
	knownRecords := make(map[string]bool, len(corpus.Records))
	for _, record := range corpus.Records {
		knownRecords[record.ID] = true
	}
	for id, reason := range officialCompileMigrationExclusions {
		if reason == "" || !knownRecords[id] {
			t.Fatalf("invalid official compile migration exclusion %q: %q", id, reason)
		}
		if selectedRecords[id] {
			t.Fatalf("excluded official compile record %q was selected", id)
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
	for _, testCase := range officialCompileRepresentativeCases(readOfficialCompileCases(t), officialCompileCaseSelected) {
		record, variant := testCase.Record, testCase.Variant
		selected++
		diagnostics := analyzeOfficialCompileSource(t, record, variant)
		status := byCode[variant.ExpectedCode]
		status.total++
		switch {
		case hasOfficialCompileCode(diagnostics, variant.ExpectedCode):
			status.ready++
		case len(diagnostics) == 0:
			status.missing++
		default:
			status.mapping++
		}
		byCode[variant.ExpectedCode] = status
		if *officialCompileCaseFilter != "all" && !hasOfficialCompileCode(diagnostics, variant.ExpectedCode) {
			t.Logf("%s want=%s got=%s source=%q", officialCompileVariantID(record, variant), variant.ExpectedCode, officialCompileCodes(diagnostics), compileSourcePreview(variant.Source))
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
		t.Skip("no official compile diagnostic family has representative coverage")
	}
	seen := make(map[string]int, len(supported))
	include := func(record officialCompileRecord, variant officialCompileVariant) bool {
		return supported[variant.ExpectedCode] && officialCompileCaseSelectedByDefault(record, variant)
	}
	for _, testCase := range officialCompileRepresentativeCases(readOfficialCompileCases(t), include) {
		record, variant := testCase.Record, testCase.Variant
		diagnostics := analyzeOfficialCompileSource(t, record, variant)
		if !hasOfficialCompileCode(diagnostics, variant.ExpectedCode) {
			t.Fatalf("%s diagnostics=%#v, want %s", officialCompileVariantID(record, variant), diagnostics, variant.ExpectedCode)
		}
		seen[variant.ExpectedCode]++
	}
	if strings.TrimSpace(*officialCompileCaseFilter) == "" {
		for code := range supported {
			if seen[code] == 0 {
				t.Fatalf("supported compile code %s has no pinned official case", code)
			}
		}
	} else if len(seen) == 0 {
		t.Fatalf("no supported official compile case matches -official-compile-case=%q", *officialCompileCaseFilter)
	}
}

func officialCompileSupportedCodes() map[string]bool {
	// A code is added after focused Go tests cover its static rule and the pinned
	// self-contained representatives below produce the expected diagnostic.
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
		"vim/E1001": true,
		"vim/E1002": true,
		"vim/E1004": true,
		"vim/E1005": true,
		"vim/E1006": true,
		"vim/E1007": true,
		"vim/E1008": true,
		"vim/E1009": true,
		"vim/E1012": true,
		"vim/E1013": true,
		"vim/E1018": true,
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

// Keep non-self-contained upstream cases in the generated inventory, but do
// not recreate Vim's runtime test environment in language-server tests. Add a
// focused Go fixture for the underlying static rule instead.
var officialCompileMigrationExclusions = map[string]string{
	"src/testdir/test_vim9_func.vim:1706:37318": "depends on test-defined g:FilterWithCond()",
	"src/testdir/test_vim9_func.vim:1949:43074": "depends on test-defined g:MyDefVarargs()",
	"src/testdir/test_vim9_func.vim:1951:43200": "depends on test-defined g:MyDefVarargs()",
	"src/testdir/test_vim9_func.vim:2099:46590": "depends on test-defined g:MyVarargsOnly()",
	"src/testdir/test_vim9_func.vim:2100:46703": "depends on test-defined g:MyVarargsOnly()",
	"src/testdir/test_vim9_func.vim:2851:63748": "depends on a test-defined global function",
	"src/testdir/test_vim9_func.vim:2852:63899": "depends on a test-defined global function",
	"src/testdir/test_vim9_func.vim:2857:64141": "depends on a test-defined global function",
	"src/testdir/test_vim9_func.vim:2858:64281": "depends on a test-defined global function",
	"src/testdir/test_vim9_func.vim:2865:64558": "depends on a test-defined global function",
	"src/testdir/test_vim9_func.vim:2866:64703": "depends on a test-defined global function",
	"src/testdir/test_vim9_func.vim:2871:64951": "depends on a test-defined global function",
	"src/testdir/test_vim9_func.vim:2872:65098": "depends on a test-defined global function",
	"src/testdir/test_vim9_func.vim:2878:65345": "depends on a test-defined global function",
	"src/testdir/test_vim9_func.vim:2879:65483": "depends on a test-defined global function",
	"src/testdir/test_vim9_func.vim:2880:65616": "depends on a test-defined global function",
	"src/testdir/test_vim9_func.vim:2881:65761": "depends on a test-defined global function",
	"src/testdir/test_vim9_func.vim:2882:65908": "depends on a test-defined global function",
	"src/testdir/test_vim9_func.vim:2903:66816": "depends on a test-defined global function",
}

// These compiler errors are intentionally outside pure language-server
// analysis. Keep the reasons beside the official coverage gate so future
// implementation batches do not treat them as missing syntax/type work.
var officialCompileStaticAnalysisExcludedCodes = map[string]string{
	"vim/E1028": "compiler fallback after another compilation failure",
	"vim/E1146": "internal command-dispatch fallback",
	"vim/E1154": "requires compile-time constant evaluation",
	"vim/E1191": "depends on another function's lazy compiler state",
	"vim/E1271": "internal closure compiler invariant",
	"vim/E1277": "depends on Vim build features",
	"vim/E1362": "depends on compile-time object evaluation",
	"vim/E1412": "internal builtin object-method fallback",
	"vim/E1413": "internal builtin class-method fallback",
}

func analyzeOfficialCompileSource(t *testing.T, record officialCompileRecord, variant officialCompileVariant) []syntax.Diagnostic {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("%s: analysis panicked: %v", officialCompileVariantID(record, variant), recovered)
		}
	}()
	source := variant.Source
	file := syntax.Parse(source)
	if file.Source != source || len(file.Commands) == 0 {
		t.Fatalf("%s: parser did not retain official compile source", officialCompileVariantID(record, variant))
	}
	diagnostics := append([]syntax.Diagnostic(nil), file.Diagnostics...)
	diagnostics = append(diagnostics, Analyze(file).Diagnostics...)
	for _, diagnostic := range diagnostics {
		if diagnostic.Span.Start < 0 || diagnostic.Span.End < diagnostic.Span.Start || diagnostic.Span.End > len(source) {
			t.Fatalf("%s: out-of-bounds diagnostic %#v", officialCompileVariantID(record, variant), diagnostic)
		}
	}
	return diagnostics
}

func officialCompileRepresentativeCases(corpus officialCompileCorpus, include func(officialCompileRecord, officialCompileVariant) bool) []officialCompileCase {
	byCode := make(map[string][]officialCompileCase)
	for _, record := range corpus.Records {
		if record.Disposition != "extracted" || officialCompileMigrationExclusions[record.ID] != "" {
			continue
		}
		for _, variant := range record.Cases {
			if variant.ExpectedCode == "" || !include(record, variant) {
				continue
			}
			byCode[variant.ExpectedCode] = append(byCode[variant.ExpectedCode], officialCompileCase{Record: record, Variant: variant})
		}
	}
	codes := make([]string, 0, len(byCode))
	for code := range byCode {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	var selected []officialCompileCase
	for _, code := range codes {
		selected = append(selected, sampleOfficialCompileCases(byCode[code], officialCompileRepresentativeLimit)...)
	}
	return selected
}

func sampleOfficialCompileCases(candidates []officialCompileCase, limit int) []officialCompileCase {
	if limit <= 0 || len(candidates) == 0 {
		return nil
	}
	if len(candidates) <= limit {
		return append([]officialCompileCase(nil), candidates...)
	}
	var defCases, scriptCases []officialCompileCase
	for _, candidate := range candidates {
		if candidate.Variant.Context == "def" {
			defCases = append(defCases, candidate)
		} else {
			scriptCases = append(scriptCases, candidate)
		}
	}
	defLimit, scriptLimit := limit/2, limit-limit/2
	if len(defCases) < defLimit {
		scriptLimit += defLimit - len(defCases)
		defLimit = len(defCases)
	}
	if len(scriptCases) < scriptLimit {
		defLimit += scriptLimit - len(scriptCases)
		scriptLimit = len(scriptCases)
	}
	selected := evenlySampleOfficialCompileCases(defCases, defLimit)
	selected = append(selected, evenlySampleOfficialCompileCases(scriptCases, scriptLimit)...)
	return selected
}

func evenlySampleOfficialCompileCases(candidates []officialCompileCase, limit int) []officialCompileCase {
	if limit <= 0 || len(candidates) == 0 {
		return nil
	}
	if len(candidates) <= limit {
		return candidates
	}
	if limit == 1 {
		return candidates[:1]
	}
	selected := make([]officialCompileCase, 0, limit)
	for index := 0; index < limit; index++ {
		candidate := index * (len(candidates) - 1) / (limit - 1)
		selected = append(selected, candidates[candidate])
	}
	return selected
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

func officialCompileVariantID(record officialCompileRecord, variant officialCompileVariant) string {
	return record.ID + "/" + variant.Name
}

func officialCompileCaseSelected(record officialCompileRecord, variant officialCompileVariant) bool {
	filter := strings.TrimSpace(*officialCompileCaseFilter)
	if filter == "all" {
		return true
	}
	for _, part := range strings.Split(filter, ",") {
		part = strings.TrimSpace(part)
		if part != "" && (strings.Contains(officialCompileVariantID(record, variant), part) || strings.Contains(variant.ExpectedCode, part)) {
			return true
		}
	}
	return false
}

func officialCompileCaseSelectedByDefault(record officialCompileRecord, variant officialCompileVariant) bool {
	if strings.TrimSpace(*officialCompileCaseFilter) == "" {
		return true
	}
	return officialCompileCaseSelected(record, variant)
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
