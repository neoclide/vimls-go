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

const officialCompileRepresentativeLimit = 30

var officialCompileCaseFilter = flag.String("official-compile-case", "", "comma-separated exact error codes or substrings selecting official static compile variants, or all")

type officialCompileCorpus struct {
	Records []officialCompileRecord `json:"records"`
}

type officialCompileRecord struct {
	ID    string                   `json:"id"`
	Cases []officialCompileVariant `json:"cases"`
}

type officialCompileVariant struct {
	Name         string `json:"name"`
	Context      string `json:"context"`
	ExpectedCode string `json:"expectedCode"`
	Source       string `json:"source"`
}

type officialCompileCase struct {
	ID      string
	Context string
	Code    string
	Source  string
}

func TestOfficialCompileFilterMatchesErrorCodesExactly(t *testing.T) {
	tests := []struct {
		filter string
		id     string
		code   string
		want   bool
	}{
		{filter: "E113", code: "vim/E113", want: true},
		{filter: "E113", code: "vim/E1139", want: false},
		{filter: "vim/E113", code: "vim/E1139", want: false},
		{filter: "test_vim9_expr.vim:42", id: "src/testdir/test_vim9_expr.vim:42:100", code: "vim/E1139", want: true},
	}
	for _, test := range tests {
		if got := officialCompileFilterMatches(test.filter, test.id, test.code); got != test.want {
			t.Errorf("officialCompileFilterMatches(%q, %q, %q) = %t, want %t", test.filter, test.id, test.code, got, test.want)
		}
	}
}

func TestOfficialVimCompileInventory(t *testing.T) {
	statuses := officialCompileSupportedCodes()
	for code, reason := range officialCompileStaticAnalysisExcludedCodes {
		if reason == "" {
			t.Fatalf("static-analysis exclusion %s has no reason", code)
		}
		if _, tracked := statuses[code]; tracked {
			t.Fatalf("static-analysis exclusion %s is also in the support inventory", code)
		}
	}
	corpus := readOfficialCompileCases(t)
	for _, record := range corpus.Records {
		for _, variant := range record.Cases {
			code := variant.ExpectedCode
			if code == "" || officialCompileStaticAnalysisExcludedCodes[code] != "" {
				continue
			}
			if _, ok := statuses[code]; !ok {
				t.Errorf("compile code %s is missing from the support inventory", code)
			}
		}
	}
	counts := make(map[string]int)
	for _, testCase := range officialCompileRepresentativeCases(corpus, func(officialCompileCase) bool { return true }) {
		counts[testCase.Code]++
		if counts[testCase.Code] > officialCompileRepresentativeLimit {
			t.Fatalf("%s selected %d migrated cases, limit is %d", testCase.Code, counts[testCase.Code], officialCompileRepresentativeLimit)
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
		selected++
		diagnostics := analyzeOfficialCompileSource(t, testCase)
		status := byCode[testCase.Code]
		status.total++
		switch {
		case hasOfficialCompileCode(diagnostics, testCase.Code):
			status.ready++
		case len(diagnostics) == 0:
			status.missing++
		default:
			status.mapping++
		}
		byCode[testCase.Code] = status
		if *officialCompileCaseFilter != "all" && !hasOfficialCompileCode(diagnostics, testCase.Code) {
			t.Logf("%s want=%s got=%s source=%q", testCase.ID, testCase.Code, officialCompileCodes(diagnostics), compileSourcePreview(testCase.Source))
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
	include := func(testCase officialCompileCase) bool {
		if !supported[testCase.Code] {
			return false
		}
		return strings.TrimSpace(*officialCompileCaseFilter) == "" || officialCompileCaseSelected(testCase)
	}
	seen := 0
	for _, testCase := range officialCompileRepresentativeCases(readOfficialCompileCases(t), include) {
		diagnostics := analyzeOfficialCompileSource(t, testCase)
		if !hasOfficialCompileCode(diagnostics, testCase.Code) {
			t.Fatalf("%s diagnostics=%#v, want %s", testCase.ID, diagnostics, testCase.Code)
		}
		seen++
	}
	if strings.TrimSpace(*officialCompileCaseFilter) != "" && seen == 0 {
		t.Fatalf("no supported official compile case matches -official-compile-case=%q", *officialCompileCaseFilter)
	}
}

func officialCompileSupportedCodes() map[string]bool {
	// True means focused Go tests cover the static rule and the pinned
	// representatives produce the expected diagnostic. False is an explicit
	// pending item. Errors outside static analysis remain in the exclusion map.
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
		"vim/E1010": true,
		"vim/E1012": true,
		"vim/E1013": true,
		"vim/E1016": true,
		"vim/E1018": true,
		"vim/E1019": true,
		"vim/E1020": true,
		"vim/E1021": true,
		"vim/E1022": true,
		"vim/E1025": true,
		"vim/E1026": true,
		"vim/E1031": true,
		"vim/E1032": true,
		"vim/E1033": true,
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
		"vim/E1174": true,
		"vim/E1176": true,
		"vim/E1180": true,
		"vim/E1185": true,
		"vim/E1202": true,
		"vim/E1206": true,
		"vim/E1241": true,
		"vim/E1242": true,
		"vim/E1278": true,
		"vim/E1279": true,
		"vim/E1539": true,

		// Present in the pinned static corpus but not fully supported yet.
		"vim/E46":   false,
		"vim/E113":  false,
		"vim/E116":  false,
		"vim/E117":  false,
		"vim/E118":  false,
		"vim/E119":  false,
		"vim/E121":  false,
		"vim/E155":  false,
		"vim/E176":  false,
		"vim/E260":  false,
		"vim/E464":  false,
		"vim/E475":  false,
		"vim/E476":  false,
		"vim/E488":  false,
		"vim/E492":  false,
		"vim/E611":  false,
		"vim/E689":  false,
		"vim/E701":  false,
		"vim/E703":  false,
		"vim/E704":  false,
		"vim/E716":  false,
		"vim/E721":  false,
		"vim/E728":  false,
		"vim/E729":  false,
		"vim/E730":  false,
		"vim/E731":  false,
		"vim/E734":  false,
		"vim/E745":  false,
		"vim/E804":  false,
		"vim/E805":  false,
		"vim/E806":  false,
		"vim/E896":  false,
		"vim/E908":  false,
		"vim/E974":  false,
		"vim/E976":  false,
		"vim/E996":  false,
		"vim/E1011": false,
		"vim/E1017": false,
		"vim/E1023": false,
		"vim/E1024": false,
		"vim/E1027": false,
		"vim/E1030": false,
		"vim/E1034": false,
		"vim/E1035": false,
		"vim/E1036": false,
		"vim/E1041": false,
		"vim/E1051": false,
		"vim/E1052": false,
		"vim/E1062": false,
		"vim/E1072": false,
		"vim/E1073": false,
		"vim/E1075": false,
		"vim/E1085": false,
		"vim/E1089": false,
		"vim/E1092": false,
		"vim/E1093": false,
		"vim/E1094": false,
		"vim/E1095": false,
		"vim/E1101": false,
		"vim/E1105": false,
		"vim/E1106": false,
		"vim/E1107": false,
		"vim/E1117": false,
		"vim/E1135": false,
		"vim/E1138": false,
		"vim/E1141": false,
		"vim/E1158": false,
		"vim/E1163": false,
		"vim/E1165": false,
		"vim/E1166": false,
		"vim/E1167": false,
		"vim/E1177": false,
		"vim/E1178": false,
		"vim/E1181": false,
		"vim/E1190": false,
		"vim/E1207": false,
		"vim/E1210": false,
		"vim/E1211": false,
		"vim/E1212": false,
		"vim/E1216": false,
		"vim/E1217": false,
		"vim/E1218": false,
		"vim/E1219": false,
		"vim/E1220": false,
		"vim/E1221": false,
		"vim/E1222": false,
		"vim/E1223": false,
		"vim/E1224": false,
		"vim/E1225": false,
		"vim/E1226": false,
		"vim/E1228": false,
		"vim/E1229": false,
		"vim/E1232": false,
		"vim/E1233": false,
		"vim/E1235": false,
		"vim/E1236": false,
		"vim/E1238": false,
		"vim/E1251": false,
		"vim/E1253": false,
		"vim/E1254": false,
		"vim/E1256": false,
		"vim/E1301": false,
		"vim/E1306": false,
		"vim/E1307": false,
		"vim/E1330": false,
		"vim/E1353": false,
		"vim/E1528": false,
		"vim/E1529": false,
		"vim/E1530": false,
		"vim/E1531": false,
		"vim/E1532": false,
		"vim/E1533": false,
		"vim/E1535": false,
	}
}

// These upstream cases depend on functions defined elsewhere in Vim's test
// harness. Focused Go tests cover the static rules without recreating it.
var officialCompileMigrationExclusions = map[string]bool{
	"src/testdir/test_vim9_func.vim:1706:37318": true,
	"src/testdir/test_vim9_func.vim:1949:43074": true,
	"src/testdir/test_vim9_func.vim:1951:43200": true,
	"src/testdir/test_vim9_func.vim:2099:46590": true,
	"src/testdir/test_vim9_func.vim:2100:46703": true,
	"src/testdir/test_vim9_func.vim:2851:63748": true,
	"src/testdir/test_vim9_func.vim:2852:63899": true,
	"src/testdir/test_vim9_func.vim:2857:64141": true,
	"src/testdir/test_vim9_func.vim:2858:64281": true,
	"src/testdir/test_vim9_func.vim:2865:64558": true,
	"src/testdir/test_vim9_func.vim:2866:64703": true,
	"src/testdir/test_vim9_func.vim:2871:64951": true,
	"src/testdir/test_vim9_func.vim:2872:65098": true,
	"src/testdir/test_vim9_func.vim:2878:65345": true,
	"src/testdir/test_vim9_func.vim:2879:65483": true,
	"src/testdir/test_vim9_func.vim:2880:65616": true,
	"src/testdir/test_vim9_func.vim:2881:65761": true,
	"src/testdir/test_vim9_func.vim:2882:65908": true,
	"src/testdir/test_vim9_func.vim:2903:66816": true,
}

// These compiler errors are permanently outside pure language-server static
// analysis. Keep the reasons beside the official coverage gate; they are not
// pending syntax/type work and must not be added to the support inventory.
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

func analyzeOfficialCompileSource(t *testing.T, testCase officialCompileCase) []syntax.Diagnostic {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("%s: analysis panicked: %v", testCase.ID, recovered)
		}
	}()
	source := testCase.Source
	file := syntax.Parse(source)
	if file.Source != source || len(file.Commands) == 0 {
		t.Fatalf("%s: parser did not retain official compile source", testCase.ID)
	}
	diagnostics := append([]syntax.Diagnostic(nil), file.Diagnostics...)
	diagnostics = append(diagnostics, Analyze(file).Diagnostics...)
	for _, diagnostic := range diagnostics {
		if diagnostic.Span.Start < 0 || diagnostic.Span.End < diagnostic.Span.Start || diagnostic.Span.End > len(source) {
			t.Fatalf("%s: out-of-bounds diagnostic %#v", testCase.ID, diagnostic)
		}
	}
	return diagnostics
}

func officialCompileRepresentativeCases(corpus officialCompileCorpus, include func(officialCompileCase) bool) []officialCompileCase {
	byCode := make(map[string][]officialCompileCase)
	for _, record := range corpus.Records {
		if officialCompileMigrationExclusions[record.ID] {
			continue
		}
		for _, variant := range record.Cases {
			testCase := officialCompileCase{
				ID: record.ID + "/" + variant.Name, Context: variant.Context,
				Code: variant.ExpectedCode, Source: variant.Source,
			}
			if testCase.Code == "" || !include(testCase) {
				continue
			}
			byCode[testCase.Code] = append(byCode[testCase.Code], testCase)
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
		if candidate.Context == "def" {
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

func officialCompileCaseSelected(testCase officialCompileCase) bool {
	filter := strings.TrimSpace(*officialCompileCaseFilter)
	if filter == "all" {
		return true
	}
	for _, part := range strings.Split(filter, ",") {
		part = strings.TrimSpace(part)
		if part != "" && officialCompileFilterMatches(part, testCase.ID, testCase.Code) {
			return true
		}
	}
	return false
}

func officialCompileFilterMatches(filter, id, code string) bool {
	if strings.HasPrefix(filter, "E") && len(filter) > 1 && strings.Trim(filter[1:], "0123456789") == "" {
		return code == "vim/"+filter
	}
	if strings.HasPrefix(filter, "vim/E") && len(filter) > len("vim/E") && strings.Trim(filter[len("vim/E"):], "0123456789") == "" {
		return code == filter
	}
	return strings.Contains(id, filter) || strings.Contains(code, filter)
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
