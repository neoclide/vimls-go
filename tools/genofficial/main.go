package main

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	vimTag                = "v9.2.1015"
	vimCommit             = "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae"
	expectedFileCount     = 17
	expectedCorpusCount   = 3267
	expectedTestFileCount = 362
	expectedTestRawBytes  = 8558061
	expectedHelperLexemes = 5733
	expectedHelperCalls   = 5241
)

type corpus struct {
	Tag    string       `json:"tag"`
	Commit string       `json:"commit"`
	Files  []string     `json:"files"`
	Cases  []corpusCase `json:"cases"`
}

type corpusCase struct {
	Origin  string `json:"origin"`
	Source  string `json:"source"`
	Outcome string `json:"outcome,omitempty"`
}

type testFilesCorpus struct {
	Tag    string           `json:"tag"`
	Commit string           `json:"commit"`
	Files  []testFileRecord `json:"files"`
}

type testFileRecord struct {
	Path   string `json:"path"`
	Source []byte `json:"source"`
}

type helperInventory struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Tag           string                 `json:"tag"`
	Commit        string                 `json:"commit"`
	HelperNames   []string               `json:"helperNames"`
	Records       []helperRecord         `json:"records"`
	Summary       helperInventorySummary `json:"summary"`
}

type helperRecord struct {
	Path          string `json:"path"`
	Line          int    `json:"line"`
	Offset        int    `json:"offset"`
	Name          string `json:"name"`
	Lexeme        string `json:"lexeme"`
	LexemeStart   int    `json:"lexemeStart"`
	LexemeEnd     int    `json:"lexemeEnd"`
	CallStart     int    `json:"callStart"`
	CallEnd       int    `json:"callEnd"`
	CallComplete  bool   `json:"callComplete"`
	Qualification string `json:"qualification"`
	Kind          string `json:"kind"`
	FirstArgument string `json:"firstArgumentKind"`
	Disposition   string `json:"disposition"`
	Reason        string `json:"reason"`
}

type helperInventorySummary struct {
	Lexemes             int `json:"lexemes"`
	KnownHelperCalls    int `json:"knownHelperCalls"`
	QualifiedCalls      int `json:"qualifiedCalls"`
	UtilityBareCalls    int `json:"utilityBareCalls"`
	KnownDefinitions    int `json:"knownDefinitions"`
	KnownComments       int `json:"knownComments"`
	NonV9Calls          int `json:"nonV9Calls"`
	NonV9Definitions    int `json:"nonV9Definitions"`
	NonV9Strings        int `json:"nonV9Strings"`
	NonV9Comments       int `json:"nonV9Comments"`
	IdentifierArguments int `json:"identifierArguments"`
	ListArguments       int `json:"listLiteralArguments"`
	ExpressionArguments int `json:"expressionArguments"`
	QualifiedIdentifier int `json:"qualifiedIdentifierArguments"`
	QualifiedList       int `json:"qualifiedListLiteralArguments"`
	QualifiedExpression int `json:"qualifiedExpressionArguments"`
	BareIdentifier      int `json:"bareIdentifierArguments"`
	BareExpression      int `json:"bareExpressionArguments"`
}

func main() {
	source := flag.String("vim-source", "/Users/chemzqm/lib/vim", "local Vim git checkout")
	output := flag.String("output", "testdata/official/v9.2.1015-parser-corpus.json.gz", "generated corpus path")
	testFilesOutput := flag.String("test-files-output", "testdata/official/v9.2.1015-test-files.json.gz", "lossless official test-file corpus path")
	licenseOutput := flag.String("license-output", "testdata/official/VIM-LICENSE", "upstream Vim license path")
	helperOutput := flag.String("helper-inventory-output", "testdata/official/v9.2.1015-helper-inventory.json.gz", "generated Check helper inventory path")
	flag.Parse()

	commit, err := gitOutput(*source, "rev-list", "-n", "1", vimTag)
	if err != nil {
		fatal(err)
	}
	resolvedCommit := strings.TrimSpace(string(commit))
	if resolvedCommit != vimCommit {
		fatal(fmt.Errorf("%s resolves to %s, want pinned commit %s", vimTag, resolvedCommit, vimCommit))
	}
	testFiles, err := listTestFiles(*source)
	if err != nil {
		fatal(err)
	}
	allTestFiles, err := listAllTestFiles(*source)
	if err != nil {
		fatal(err)
	}
	testCorpus := testFilesCorpus{Tag: vimTag, Commit: resolvedCommit}
	for _, path := range allTestFiles {
		contents, err := gitOutput(*source, "show", vimTag+":"+path)
		if err != nil {
			fatal(err)
		}
		testCorpus.Files = append(testCorpus.Files, testFileRecord{Path: path, Source: append([]byte(nil), contents...)})
	}
	if len(testCorpus.Files) != expectedTestFileCount {
		fatal(fmt.Errorf("read %d official Vim test files, want %d from pinned source", len(testCorpus.Files), expectedTestFileCount))
	}
	rawBytes := 0
	for _, file := range testCorpus.Files {
		rawBytes += len(file.Source)
	}
	if rawBytes != expectedTestRawBytes {
		fatal(fmt.Errorf("read %d raw bytes from official Vim test files, want %d from pinned source", rawBytes, expectedTestRawBytes))
	}
	license, err := gitOutput(*source, "show", vimTag+":LICENSE")
	if err != nil {
		fatal(err)
	}
	result := corpus{Tag: vimTag, Commit: resolvedCommit, Files: testFiles}
	for _, path := range testFiles {
		contents, err := gitOutput(*source, "show", vimTag+":"+path)
		if err != nil {
			fatal(err)
		}
		result.Cases = append(result.Cases, extract(path, string(contents))...)
	}
	if len(result.Cases) != expectedCorpusCount {
		fatal(fmt.Errorf("extracted %d official scripts, want %d from pinned source", len(result.Cases), expectedCorpusCount))
	}
	if err := writeCorpus(*output, result); err != nil {
		fatal(err)
	}
	if err := writeTestFilesCorpus(*testFilesOutput, testCorpus); err != nil {
		fatal(err)
	}
	inventory, err := buildHelperInventory(testCorpus)
	if err != nil {
		fatal(err)
	}
	if err := writeJSONGzip(*helperOutput, inventory); err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*licenseOutput), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*licenseOutput, license, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %d scripts from %s (%s) to %s\n", len(result.Cases), result.Tag, result.Commit, *output)
	fmt.Printf("wrote %d lossless test files (%d bytes) from %s (%s) to %s\n", len(testCorpus.Files), rawBytes, testCorpus.Tag, testCorpus.Commit, *testFilesOutput)
	fmt.Printf("wrote upstream Vim license to %s\n", *licenseOutput)
	fmt.Printf("wrote %d Check helper lexemes from %s (%s) to %s\n", len(inventory.Records), inventory.Tag, inventory.Commit, *helperOutput)
}

func listTestFiles(root string) ([]string, error) {
	output, err := gitOutput(root, "ls-tree", "-r", "--name-only", vimTag, "--", "src/testdir")
	if err != nil {
		return nil, err
	}
	return selectTestFiles(output)
}

func selectTestFiles(output []byte) ([]string, error) {
	var files []string
	for _, path := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if path == "src/testdir/test_vimscript.vim" || path == "src/testdir/test_tuple.vim" ||
			(strings.HasPrefix(path, "src/testdir/test_vim9") && strings.HasSuffix(path, ".vim")) {
			files = append(files, path)
		}
	}
	sort.Strings(files)
	if len(files) != expectedFileCount {
		return nil, fmt.Errorf("found %d Vim9 parser test files at %s, want %d", len(files), vimTag, expectedFileCount)
	}
	return files, nil
}

func listAllTestFiles(root string) ([]string, error) {
	output, err := gitOutput(root, "ls-tree", "-r", "--name-only", vimTag, "--", "src/testdir")
	if err != nil {
		return nil, err
	}
	return selectAllTestFiles(output)
}

func selectAllTestFiles(output []byte) ([]string, error) {
	seen := make(map[string]struct{})
	for _, path := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.HasPrefix(path, "src/testdir/") && strings.HasSuffix(path, ".vim") {
			seen[path] = struct{}{}
		}
	}
	files := make([]string, 0, len(seen))
	for path := range seen {
		files = append(files, path)
	}
	sort.Strings(files)
	if len(files) != expectedTestFileCount {
		return nil, fmt.Errorf("found %d tracked .vim test files at %s, want %d", len(files), vimTag, expectedTestFileCount)
	}
	return files, nil
}

func gitOutput(root string, arguments ...string) ([]byte, error) {
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(arguments, " "), err)
	}
	return output, nil
}

func extract(path, source string) []corpusCase {
	lines := strings.Split(source, "\n")
	var cases []corpusCase
	for line := 0; line < len(lines); line++ {
		trimmedHeader := strings.TrimSpace(lines[line])
		if strings.HasPrefix(trimmedHeader, "#") || strings.HasPrefix(trimmedHeader, "\"") {
			continue
		}
		operator := strings.Index(lines[line], "=<<")
		if operator < 0 {
			continue
		}
		marker, trim, evaluate := heredocMarker(lines[line][operator+3:])
		if marker == "" {
			continue
		}
		end := line + 1
		for end < len(lines) && strings.TrimSpace(lines[end]) != marker {
			end++
		}
		if end == len(lines) {
			continue
		}
		body := append([]string(nil), lines[line+1:end]...)
		if trim {
			body = trimHeredoc(body)
		}
		bodySource := strings.Join(body, "\n") + "\n"
		outcome := ""
		if !evaluate && !containsTestTemplate(bodySource) {
			outcome = officialOutcome(lines, end+1, assignmentName(lines[line][:operator]))
		}
		cases = append(cases, corpusCase{
			Origin:  fmt.Sprintf("%s:%d", path, line+2),
			Source:  bodySource,
			Outcome: outcome,
		})
		line = end
	}
	return cases
}

func containsTestTemplate(source string) bool {
	for _, marker := range []string{"LSTART", "LMIDDLE", "LEND", "VAR ", "LET "} {
		if strings.Contains(source, marker) {
			return true
		}
	}
	return false
}

func assignmentName(source string) string {
	end := len(strings.TrimRight(source, " \t"))
	start := end
	for start > 0 {
		character := source[start-1]
		if character != '_' && (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			break
		}
		start--
	}
	return source[start:end]
}

func officialOutcome(lines []string, start int, variable string) string {
	if variable == "" {
		return ""
	}
	for line := start; line < len(lines); line++ {
		trimmed := strings.TrimSpace(lines[line])
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "\"") {
			continue
		}
		if strings.Contains(lines[line], "=<<") {
			return ""
		}
		compact := strings.NewReplacer(" ", "", "\t", "").Replace(lines[line])
		if checkArgument(compact, "Success(", variable) {
			return "success"
		}
		if checkArgument(compact, "Failure(", variable) {
			return "failure"
		}
		if changesScript(compact, variable) {
			return ""
		}
	}
	return ""
}

func changesScript(source, variable string) bool {
	if strings.HasPrefix(source, variable+"=") {
		return true
	}
	if !strings.HasPrefix(source, variable+"[") {
		return false
	}
	close := strings.IndexByte(source[len(variable)+1:], ']')
	if close < 0 {
		return false
	}
	rest := source[len(variable)+close+2:]
	return strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, "+=") || strings.HasPrefix(rest, "-=") || strings.HasPrefix(rest, "..=")
}

func checkArgument(source, result, variable string) bool {
	position := strings.Index(source, result+variable)
	if position < 0 || !strings.Contains(source[:position], "Check") {
		return false
	}
	end := position + len(result) + len(variable)
	return end < len(source) && (source[end] == ')' || source[end] == ',')
}

func heredocMarker(source string) (string, bool, bool) {
	trim := false
	evaluate := false
	for _, field := range strings.Fields(source) {
		if field == "trim" {
			trim = true
			continue
		}
		if field == "eval" {
			evaluate = true
			continue
		}
		marker := field
		if len(marker) >= 3 && marker[0] == '[' && marker[len(marker)-1] == ']' {
			marker = marker[1 : len(marker)-1]
		}
		if marker == "" {
			return "", false, false
		}
		for _, character := range marker {
			if character < 'A' || character > 'Z' {
				return "", false, false
			}
		}
		return field, trim, evaluate
	}
	return "", false, false
}

func trimHeredoc(lines []string) []string {
	indent := ""
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent = line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		break
	}
	if indent == "" {
		return lines
	}
	for index, line := range lines {
		if strings.HasPrefix(line, indent) {
			lines[index] = line[len(indent):]
		}
	}
	return lines
}

func writeCorpus(path string, value corpus) error {
	return writeJSONGzip(path, value)
}

func writeTestFilesCorpus(path string, value testFilesCorpus) error {
	return writeJSONGzip(path, value)
}

func buildHelperInventory(files testFilesCorpus) (helperInventory, error) {
	var result helperInventory
	result.Tag = files.Tag
	result.Commit = files.Commit
	result.SchemaVersion = 1
	for _, file := range files.Files {
		if file.Path == "src/testdir/util/vim9.vim" {
			result.HelperNames = exportedCheckHelpers(file.Source)
			break
		}
	}
	if len(result.HelperNames) != 37 {
		return result, fmt.Errorf("found %d exported Check helpers, want 37", len(result.HelperNames))
	}
	known := make(map[string]struct{}, len(result.HelperNames))
	for _, name := range result.HelperNames {
		known[name] = struct{}{}
	}
	for _, file := range files.Files {
		result.Records = append(result.Records, scanHelperFile(file.Path, file.Source, known)...)
	}
	if len(result.Records) != expectedHelperLexemes {
		return result, fmt.Errorf("found %d Check lexemes, want %d from pinned source", len(result.Records), expectedHelperLexemes)
	}
	for index := range result.Records {
		if index > 0 && (result.Records[index-1].Path > result.Records[index].Path ||
			(result.Records[index-1].Path == result.Records[index].Path && result.Records[index-1].Offset >= result.Records[index].Offset)) {
			return result, fmt.Errorf("helper records are not strictly ordered at %d", index)
		}
		if err := addHelperSummary(&result.Summary, result.Records[index]); err != nil {
			return result, err
		}
	}
	if result.Summary.KnownHelperCalls != expectedHelperCalls || result.Summary.QualifiedCalls != 5208 || result.Summary.UtilityBareCalls != 33 ||
		result.Summary.KnownDefinitions != 37 || result.Summary.KnownComments != 10 || result.Summary.NonV9Calls != 341 ||
		result.Summary.NonV9Definitions != 90 || result.Summary.NonV9Strings != 13 || result.Summary.NonV9Comments != 1 ||
		result.Summary.IdentifierArguments != 3174 || result.Summary.ListArguments != 2001 || result.Summary.ExpressionArguments != 66 {
		return result, fmt.Errorf("unexpected Check inventory summary: %+v", result.Summary)
	}
	if result.Summary.QualifiedIdentifier != 3156 || result.Summary.QualifiedList != 2001 || result.Summary.QualifiedExpression != 51 || result.Summary.BareIdentifier != 18 || result.Summary.BareExpression != 15 {
		return result, fmt.Errorf("unexpected Check argument split: %+v", result.Summary)
	}
	return result, nil
}

func exportedCheckHelpers(source []byte) []string {
	var names []string
	for _, line := range strings.Split(string(source), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "export" || (fields[1] != "func" && fields[1] != "def") {
			continue
		}
		name := fields[2]
		if position := strings.IndexByte(name, '('); position >= 0 {
			name = name[:position]
		}
		if strings.HasPrefix(name, "Check") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

type helperLexState byte

const (
	helperCode helperLexState = iota
	helperSingleQuote
	helperDoubleQuote
	helperComment
)

func scanHelperFile(path string, source []byte, known map[string]struct{}) []helperRecord {
	states := make([]helperLexState, len(source))
	state := helperCode
	lineStart := true
	for index := 0; index < len(source); index++ {
		character := source[index]
		states[index] = state
		switch state {
		case helperComment:
			if character == '\n' {
				state = helperCode
				lineStart = true
			}
		case helperSingleQuote:
			if character == '\'' {
				if index+1 < len(source) && source[index+1] == '\'' {
					states[index+1] = state
					index++
				} else {
					state = helperCode
				}
			}
		case helperDoubleQuote:
			if character == '\\' && index+1 < len(source) {
				states[index+1] = state
				index++
			} else if character == '"' {
				state = helperCode
			}
		default:
			if character == '\r' || character == '\n' {
				lineStart = true
				continue
			}
			if character == ' ' || character == '\t' {
				continue
			}
			if lineStart && (character == '#' || character == '"') {
				state = helperComment
				states[index] = state
				lineStart = false
				continue
			}
			lineStart = false
			if character == '\'' {
				state = helperSingleQuote
				states[index] = state
			} else if character == '"' {
				state = helperDoubleQuote
				states[index] = state
			}
		}
	}

	var records []helperRecord
	recordLine := 1
	for index := 0; index < len(source); index++ {
		if source[index] == '\n' {
			recordLine++
		}
		if !isIdentifierStart(source[index]) || (index > 0 && isIdentifierPart(source[index-1])) {
			continue
		}
		end := index + 5
		if end > len(source) || string(source[index:end]) != "Check" {
			continue
		}
		for end < len(source) && isIdentifierPart(source[end]) {
			end++
		}
		open := end
		for open < len(source) && isHelperWhitespace(source[open]) {
			open++
		}
		if open >= len(source) || source[open] != '(' {
			continue
		}
		name := string(source[index:end])
		qualification := "bare"
		if qualifiedHelper(source, index) {
			qualification = "qualified"
		}
		kind := "call"
		if states[index] == helperComment {
			kind = "comment"
		} else if states[index] == helperSingleQuote || states[index] == helperDoubleQuote {
			kind = "string"
		} else if helperDefinition(source, index) {
			kind = "definition"
		}
		_, isKnown := known[name]
		utilityCall := path == "src/testdir/util/vim9.vim" && qualification == "bare" && kind == "call" && isKnown
		if kind == "string" && isKnown && qualification == "qualified" {
			kind = "call"
		}
		if kind == "string" && embeddedHelperCall(path, recordLine) {
			kind = "call"
		}
		genuine := isKnown && kind == "call" && (qualification == "qualified" || utilityCall)
		firstArgument := ""
		if genuine {
			firstArgument = helperFirstArgument(source, open+1)
		}
		callEnd, complete := helperCallEnd(source, open)
		disposition, reason := helperDisposition(path, name, qualification, kind, isKnown, genuine, complete)
		callStart := index
		if qualification == "qualified" {
			callStart = helperQualificationStart(source, index)
		}
		records = append(records, helperRecord{
			Path: path, Line: recordLine, Offset: index, Name: name, Lexeme: string(source[index:end]),
			LexemeStart: index, LexemeEnd: end, CallStart: callStart, CallEnd: callEnd, CallComplete: complete,
			Qualification: qualification, Kind: kind, FirstArgument: firstArgument,
			Disposition: disposition, Reason: reason,
		})
		index = end - 1
	}
	return records
}

func isIdentifierStart(character byte) bool {
	return character >= 'A' && character <= 'Z'
}

func isHelperWhitespace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n' || character == '\v' || character == '\f'
}

func isIdentifierPart(character byte) bool {
	return (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') ||
		(character >= '0' && character <= '9') || character == '_'
}

func embeddedHelperCall(path string, line int) bool {
	if path == "src/testdir/test_windows_home.vim" {
		return false
	}
	if path == "src/testdir/test_vim9_script.vim" {
		if line == 99 || line == 103 {
			return false
		}
	}
	if path == "src/testdir/test_cmdline.vim" {
		if line == 1974 || line == 1984 || line == 1985 || line == 1988 {
			return false
		}
	}
	return true
}

func qualifiedHelper(source []byte, index int) bool {
	for index > 0 && isHelperWhitespace(source[index-1]) {
		index--
	}
	return index >= 3 && source[index-1] == '.' && source[index-2] == '9' && source[index-3] == 'v'
}

func helperQualificationStart(source []byte, index int) int {
	start := index
	for start > 0 && isHelperWhitespace(source[start-1]) {
		start--
	}
	if start >= 3 && source[start-1] == '.' && source[start-2] == '9' && source[start-3] == 'v' {
		return start - 3
	}
	return index
}

func helperDefinition(source []byte, index int) bool {
	lineStart := index
	for lineStart > 0 && source[lineStart-1] != '\n' {
		lineStart--
	}
	prefix := strings.TrimLeft(string(source[lineStart:index]), " \t")
	for _, candidate := range []string{"func ", "func! ", "function ", "function! ", "def ", "export func ", "export def ", "func! s:"} {
		if strings.HasSuffix(prefix, candidate) || strings.HasSuffix(prefix, "! "+candidate) || strings.HasSuffix(prefix, "static "+candidate) {
			return true
		}
	}
	return false
}

func helperFirstArgument(source []byte, index int) string {
	for index < len(source) && isHelperWhitespace(source[index]) {
		index++
	}
	if index >= len(source) || source[index] == ')' {
		return "empty"
	}
	if source[index] == '[' {
		close, complete := helperMatchingDelimiter(source, index, '[', ']')
		if !complete {
			return "expression"
		}
		next := close + 1
		for next < len(source) && isHelperWhitespace(source[next]) {
			next++
		}
		if next >= len(source) || source[next] == ',' || source[next] == ')' {
			return "list"
		}
		return "expression"
	}
	if isArgumentIdentifier(source[index]) {
		end := index + 1
		for end < len(source) && (isIdentifierPart(source[end]) || source[end] == ':') {
			end++
		}
		next := end
		for next < len(source) && (source[next] == ' ' || source[next] == '\t' || source[next] == '\r' || source[next] == '\n') {
			next++
		}
		if next >= len(source) || source[next] == ',' || source[next] == ')' {
			return "identifier"
		}
		return "expression"
	}
	return "expression"
}

func isArgumentIdentifier(character byte) bool {
	return (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || character == '_'
}

func helperCallEnd(source []byte, open int) (int, bool) {
	depth := 0
	state := helperCode
	for index := open; index < len(source); index++ {
		switch state {
		case helperSingleQuote:
			if source[index] == '\'' {
				if index+1 < len(source) && source[index+1] == '\'' {
					index++
					continue
				}
				state = helperCode
			}
		case helperDoubleQuote:
			if source[index] == '\\' {
				index++
			} else if source[index] == '"' {
				state = helperCode
			}
		default:
			if source[index] == '\'' {
				state = helperSingleQuote
				continue
			}
			if source[index] == '"' {
				state = helperDoubleQuote
				continue
			}
			if source[index] == '(' {
				depth++
			}
			if source[index] == ')' {
				depth--
				if depth == 0 {
					return index + 1, true
				}
			}
		}
	}
	return len(source), false
}

func helperMatchingDelimiter(source []byte, open int, opening, closing byte) (int, bool) {
	depth := 0
	state := helperCode
	for index := open; index < len(source); index++ {
		switch state {
		case helperSingleQuote:
			if source[index] == '\'' {
				if index+1 < len(source) && source[index+1] == '\'' {
					index++
					continue
				}
				state = helperCode
			}
		case helperDoubleQuote:
			if source[index] == '\\' {
				index++
			} else if source[index] == '"' {
				state = helperCode
			}
		default:
			if source[index] == '\'' {
				state = helperSingleQuote
				continue
			}
			if source[index] == '"' {
				state = helperDoubleQuote
				continue
			}
			if source[index] == opening {
				depth++
			}
			if source[index] == closing {
				depth--
				if depth == 0 {
					return index, true
				}
			}
		}
	}
	return len(source), false
}

func helperDisposition(path, name, qualification, kind string, known, genuine, complete bool) (string, string) {
	if genuine {
		if !complete {
			return "out-of-scope", "incomplete Check helper call"
		}
		if qualification == "qualified" {
			return "pending-extraction", "qualified Vim9 test helper call"
		}
		return "out-of-scope", "helper implementation call in util/vim9.vim"
	}
	if known {
		return "out-of-scope", "known helper definition or comment"
	}
	if kind == "call" {
		return "out-of-scope", "non-Vim9 Check helper call"
	}
	if kind == "definition" {
		return "out-of-scope", "non-Vim9 Check helper definition"
	}
	if kind == "string" {
		return "out-of-scope", "non-Vim9 Check text in string"
	}
	return "out-of-scope", "non-Vim9 Check text in comment"
}

func addHelperSummary(summary *helperInventorySummary, record helperRecord) error {
	summary.Lexemes++
	if record.Disposition == "pending-extraction" || record.Reason == "helper implementation call in util/vim9.vim" {
		summary.KnownHelperCalls++
		if record.Qualification == "qualified" {
			summary.QualifiedCalls++
		} else {
			summary.UtilityBareCalls++
		}
		if record.FirstArgument == "identifier" {
			summary.IdentifierArguments++
			if record.Qualification == "qualified" {
				summary.QualifiedIdentifier++
			} else {
				summary.BareIdentifier++
			}
		}
		if record.FirstArgument == "list" {
			summary.ListArguments++
			if record.Qualification == "qualified" {
				summary.QualifiedList++
			}
		}
		if record.FirstArgument == "expression" {
			summary.ExpressionArguments++
			if record.Qualification == "qualified" {
				summary.QualifiedExpression++
			} else {
				summary.BareExpression++
			}
		}
	}
	switch record.Reason {
	case "known helper definition or comment":
		if record.Kind == "definition" {
			summary.KnownDefinitions++
		} else {
			summary.KnownComments++
		}
	case "non-Vim9 Check helper call":
		summary.NonV9Calls++
	case "non-Vim9 Check helper definition":
		summary.NonV9Definitions++
	case "non-Vim9 Check text in string":
		summary.NonV9Strings++
	case "non-Vim9 Check text in comment":
		summary.NonV9Comments++
	}
	return nil
}

func writeJSONGzip(path string, value any) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	file := temporary
	compressor, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		file.Close()
		return err
	}
	compressor.Header.ModTime = time.Unix(0, 0)
	compressor.Header.OS = 255
	encoder := json.NewEncoder(compressor)
	encoder.SetEscapeHTML(false)
	writeErr := encoder.Encode(value)
	closeGzipErr := compressor.Close()
	chmodErr := file.Chmod(0o644)
	closeFileErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeGzipErr != nil {
		return closeGzipErr
	}
	if chmodErr != nil {
		return chmodErr
	}
	if closeFileErr != nil {
		return closeFileErr
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
