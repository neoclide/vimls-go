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

func main() {
	source := flag.String("vim-source", "/Users/chemzqm/lib/vim", "local Vim git checkout")
	output := flag.String("output", "testdata/official/v9.2.1015-parser-corpus.json.gz", "generated corpus path")
	testFilesOutput := flag.String("test-files-output", "testdata/official/v9.2.1015-test-files.json.gz", "lossless official test-file corpus path")
	licenseOutput := flag.String("license-output", "testdata/official/VIM-LICENSE", "upstream Vim license path")
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
	if err := os.MkdirAll(filepath.Dir(*licenseOutput), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*licenseOutput, license, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %d scripts from %s (%s) to %s\n", len(result.Cases), result.Tag, result.Commit, *output)
	fmt.Printf("wrote %d lossless test files (%d bytes) from %s (%s) to %s\n", len(testCorpus.Files), rawBytes, testCorpus.Tag, testCorpus.Commit, *testFilesOutput)
	fmt.Printf("wrote upstream Vim license to %s\n", *licenseOutput)
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
	closeFileErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeGzipErr != nil {
		return closeGzipErr
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
