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

	"github.com/neoclide/vimls-go/internal/syntax"
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
	statuses := officialCompileCodeStatuses()
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
	supported := officialCompileCodeStatuses()
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

// Completed codes have focused Go coverage. Pending codes are statically
// analyzable Vim compile errors that still need implementation or test review.
const officialCompileCompletedCodes = `
E15 E16 E46 E107 E109 E110 E111 E113 E114 E115 E116 E117 E118 E119 E121 E170 E171 E176 E260 E274 E354 E475 E476 E488 E492
E481 E518 E580 E581 E582 E583 E584 E586 E587 E588 E600 E602 E603
E606 E607 E611 E689 E690 E696 E697 E701 E703 E704 E716 E720 E721 E722 E723 E728 E729 E730 E731 E734 E745 E804 E805 E806 E896 E908 E973 E974 E976 E996 E1001 E1002 E1003 E1004
E1005 E1006 E1007 E1008 E1009 E1010 E1011 E1012 E1013 E1015 E1016 E1017 E1018 E1019 E1020
E1021 E1022 E1023 E1024 E1025 E1026 E1027 E1030 E1031 E1032 E1033 E1034 E1035 E1036 E1037 E1038 E1039 E1040 E1043
E1041 E1042 E1044 E1047 E1048 E1049 E1050 E1051 E1052 E1053 E1054 E1055 E1056 E1057 E1058 E1059 E1060 E1062 E1065 E1066 E1067 E1068 E1069 E1071 E1072 E1073 E1074 E1075 E1077 E1080 E1081 E1082 E1083 E1087 E1089
E1093 E1097 E1100 E1104 E1123 E1125 E1126 E1127 E1128 E1139 E1143 E1144 E1145 E1151 E1152 E1157
E1170 E1171 E1172 E1173 E1174 E1176 E1180 E1185 E1202 E1206 E1208 E1241 E1242 E1257 E1261 E1267 E1278
E1279 E1316 E1317 E1329 E1331 E1342 E1344 E1345 E1368 E1389 E1397 E1398
E1414 E1526 E1527 E1531 E1532 E1533 E1535 E1536 E1537 E1538 E1539 E1552 E1553 E1554 E1555 E1556 E1557 E1559 E1560 E1561 E1579
`

const officialCompilePendingCodes = `
E1085 E1088
E1090 E1092 E1094 E1095 E1096 E1101 E1105 E1106 E1107 E1117
E1135 E1138 E1141 E1153 E1158 E1160 E1163 E1164 E1165 E1166
E1167 E1168 E1177 E1178 E1181 E1182 E1183 E1186 E1189 E1190 E1207
E1210 E1211 E1212 E1213 E1216 E1217 E1218 E1219 E1220 E1221 E1222
E1223 E1224 E1225 E1226 E1228 E1229 E1231 E1232 E1233 E1234 E1235 E1236
E1238 E1247 E1251 E1253 E1254 E1256 E1258 E1259 E1260 E1262
E1263 E1264 E1268 E1274 E1282 E1283 E1301 E1306 E1307 E1314 E1315
E1318 E1325 E1326 E1328 E1330 E1332 E1333 E1335
E1337 E1340 E1341 E1343 E1346 E1347 E1348 E1349 E1350
E1351 E1352 E1353 E1354 E1355 E1356 E1357 E1358 E1359 E1360 E1363 E1365
E1366 E1367 E1369 E1370 E1371 E1372 E1373 E1374 E1375 E1376 E1377
E1378 E1379 E1380 E1381 E1382 E1383 E1384 E1385 E1386 E1387 E1388
E1390 E1393 E1394 E1396 E1399 E1403 E1404 E1405 E1406 E1407
E1408 E1409 E1410 E1411 E1415 E1416 E1417 E1418 E1419 E1420 E1421
E1422 E1423 E1426 E1427 E1428 E1429 E1431 E1432 E1433 E1434 E1435 E1436
E1528 E1529 E1530
`

func officialCompileCodeStatuses() map[string]bool {
	statuses := make(map[string]bool)
	for _, code := range strings.Fields(officialCompileCompletedCodes) {
		statuses["vim/"+code] = true
	}
	for _, code := range strings.Fields(officialCompilePendingCodes) {
		statuses["vim/"+code] = false
	}
	return statuses
}

// These upstream cases depend on dynamic values, runtimepath user commands, or
// functions defined elsewhere in Vim's test harness. Focused Go tests cover the
// static rules without inventing facts absent from an isolated source variant.
var officialCompileMigrationExclusions = map[string]bool{
	"src/testdir/test_vim9_expr.vim:209:6326":   true, // dynamic g: function values and condition
	"src/testdir/test_vim9_expr.vim:2696:80191": true, // job_stop() is E117 only in builds without +channel
	"src/testdir/test_vim9_expr.vim:3285:97680": true, // job_stop() is E117 only in builds without +channel

	"src/testdir/test_vim9_expr.vim:4487:130048": true, // CallMe may resolve to a runtimepath user command

	"src/testdir/test_vim9_func.vim:1408:30078": true, // g:TakesOneArg is defined by the test harness
	"src/testdir/test_vim9_func.vim:1409:30129": true, // g:TakesOneArg is defined by the test harness
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

	"src/testdir/test_vim9_script.vim:4876:109149": true, // :Print may be overruled by a runtimepath user command
}

// These compiler errors are permanently outside pure language-server static
// analysis. Keep the reasons beside the official coverage gate; they are not
// pending syntax/type work and must not be added to the support inventory.
var officialCompileStaticAnalysisExcludedCodes = map[string]string{
	"vim/E155":  "depends on the runtime sign-definition registry",
	"vim/E464":  "depends on mutable global and buffer-local user-command tables",
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
	diagnostics := CombinedDiagnostics(file, Analyze(file))
	for _, diagnostic := range diagnostics {
		if diagnostic.Span.Start < 0 || diagnostic.Span.End < diagnostic.Span.Start || diagnostic.Span.End > len(source) {
			t.Fatalf("%s: out-of-bounds diagnostic %#v", testCase.ID, diagnostic)
		}
	}
	return diagnostics
}

func officialCompileRepresentativeCases(corpus officialCompileCorpus, include func(officialCompileCase) bool) []officialCompileCase {
	byCode := make(map[string][]officialCompileCase)
	// CheckScriptFailure is intentionally outside the generated compile corpus.
	// Keep its one statically analyzable E1003 case tied to the upstream line.
	testCase := officialCompileCase{
		ID:      "src/testdir/test_vim9_func.vim:2434/CheckScriptFailure",
		Context: "script",
		Code:    "vim/E1003",
		Source:  "vim9script\ndef Func(): number\n  return\nenddef\ndefcompile\n",
	}
	if include(testCase) {
		byCode[testCase.Code] = append(byCode[testCase.Code], testCase)
	}
	// These upstream assertions spell out the native message without its E1037
	// prefix, so the generated corpus cannot recover the code. Keep both helper
	// contexts tied directly to the official lines.
	for _, testCase := range []officialCompileCase{
		{ID: "src/testdir/test_vim9_expr.vim:1736/E1037-bool/def", Context: "def", Code: "vim/E1037", Source: "def Func()\n  var x = true is false\nenddef\ndefcompile\n"},
		{ID: "src/testdir/test_vim9_expr.vim:1737/E1037-bool/script", Context: "script", Code: "vim/E1037", Source: "vim9script\nvar x = true isnot false\n"},
		{ID: "src/testdir/test_vim9_expr.vim:1739/E1037-special/def", Context: "def", Code: "vim/E1037", Source: "def Func()\n  var x = v:none is v:null\nenddef\ndefcompile\n"},
		{ID: "src/testdir/test_vim9_expr.vim:1740/E1037-special/script", Context: "script", Code: "vim/E1037", Source: "vim9script\nvar x = v:none isnot v:null\n"},
		{ID: "src/testdir/test_vim9_expr.vim:1741/E1037-number/def", Context: "def", Code: "vim/E1037", Source: "def Func()\n  var x = 123 is 123\nenddef\ndefcompile\n"},
		{ID: "src/testdir/test_vim9_expr.vim:1742/E1037-number/script", Context: "script", Code: "vim/E1037", Source: "vim9script\nvar x = 123 isnot 123\n"},
		{ID: "src/testdir/test_vim9_expr.vim:1743/E1037-float/def", Context: "def", Code: "vim/E1037", Source: "def Func()\n  var x = 1.3 is 1.3\nenddef\ndefcompile\n"},
		{ID: "src/testdir/test_vim9_expr.vim:1744/E1037-float/script", Context: "script", Code: "vim/E1037", Source: "vim9script\nvar x = 1.3 isnot 1.3\n"},
	} {
		if include(testCase) {
			byCode[testCase.Code] = append(byCode[testCase.Code], testCase)
		}
	}
	testCase = officialCompileCase{
		ID:      "src/testdir/test_vimscript.vim:7367-7403/E1058",
		Context: "legacy",
		Code:    "vim/E1058",
		Source:  strings.Repeat("function X()\n", 51) + strings.Repeat("endfunction\n", 51),
	}
	if include(testCase) {
		byCode[testCase.Code] = append(byCode[testCase.Code], testCase)
	}
	for _, testCase := range []officialCompileCase{
		{
			ID: "src/testdir/test_vim9_import.vim:151-159/E1060", Context: "script", Code: "vim/E1060",
			Source: "vim9script\nimport './Xexport.vim' as expo\ng:exported = expo\n  .exported\n",
		},
		{
			ID: "src/testdir/test_vim9_import.vim:191-200/E1060", Context: "def", Code: "vim/E1060",
			Source: "vim9script\nimport './Xexport.vim' as Export\ndef Func()\n  var dummy = 1\n  var imported = Export + dummy\nenddef\ndefcompile\n",
		},
		{
			ID: "src/testdir/test_vim9_import.vim:223-229/E1060", Context: "script", Code: "vim/E1060",
			Source: "vim9script\nimport './Xexport.vim' as Export\ng:imported_script = Export exported\n",
		},
		{
			ID: "src/testdir/test_vim9_import.vim:507-519/E1060", Context: "script", Code: "vim/E1060",
			Source: "vim9script\nimport './Xfoo.vim' as foo\nvar that: any\nthat += foo\n",
		},
		{
			ID: "src/testdir/test_vim9_import.vim:520-525/E1060", Context: "script", Code: "vim/E1060",
			Source: "vim9script\nimport './Xfoo.vim' as foo\nfoo += 9\n",
		},
	} {
		if include(testCase) {
			byCode[testCase.Code] = append(byCode[testCase.Code], testCase)
		}
	}
	for _, testCase := range []officialCompileCase{
		{
			ID: "src/testdir/test_vim9_import.vim:610-612/E1071-empty", Context: "script", Code: "vim/E1071",
			Source: "vim9script\nimport \"\" as abc\n",
		},
		{
			ID: "src/testdir/test_vim9_import.vim:613-614/E1071-list", Context: "script", Code: "vim/E1071",
			Source: "vim9script\nimport [] as abc\n",
		},
		{
			ID: "src/testdir/test_vim9_import.vim:615-616/E1071-null", Context: "script", Code: "vim/E1071",
			Source: "vim9script\nimport test_null_string() as abc\n",
		},
	} {
		if include(testCase) {
			byCode[testCase.Code] = append(byCode[testCase.Code], testCase)
		}
	}
	for _, testCase := range []officialCompileCase{
		{
			ID: "src/testdir/test_vim9_import.vim:161-168/E1074-line-break", Context: "script", Code: "vim/E1074",
			Source: "vim9script\nimport './Xexport.vim' as expo\ng:exported = expo.\n  exported\n",
		},
		{
			ID: "src/testdir/test_vim9_import.vim:202-211/E1074-def", Context: "def", Code: "vim/E1074",
			Source: "vim9script\nimport './Xexport.vim' as Export\ndef Func()\n  var imported = Export . exported\nenddef\ndefcompile\n",
		},
		{
			ID: "src/testdir/test_vim9_import.vim:231-237/E1074-script", Context: "script", Code: "vim/E1074",
			Source: "vim9script\nimport './Xexport.vim' as Export\ng:imported_script = Export. exported\n",
		},
	} {
		if include(testCase) {
			byCode[testCase.Code] = append(byCode[testCase.Code], testCase)
		}
	}
	for _, testCase := range []officialCompileCase{
		{
			ID: "src/testdir/test_vim9_assign.vim:2643-2647/E1081-script-local", Context: "def", Code: "vim/E1081",
			Source: "vim9script\ndef Func()\n  unlet s:somevar\nenddef\n",
		},
		{
			ID: "src/testdir/test_vim9_assign.vim:2655-2658/E1081-local", Context: "def", Code: "vim/E1081",
			Source: "def Func()\n  var dd = 111\n  unlet dd\nenddef\n",
		},
		{
			ID: "src/testdir/test_vim9_assign.vim:2802-2806/E1081-script", Context: "script", Code: "vim/E1081",
			Source: "vim9script\nvar svar = 123\nunlet svar\n",
		},
		{
			ID: "src/testdir/test_vim9_assign.vim:2812-2819/E1081-closed-over", Context: "def", Code: "vim/E1081",
			Source: "vim9script\nvar svar = 123\ndef Func()\n  unlet svar\nenddef\ndefcompile\n",
		},
	} {
		if include(testCase) {
			byCode[testCase.Code] = append(byCode[testCase.Code], testCase)
		}
	}
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
