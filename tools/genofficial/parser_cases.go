package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

const pinnedParserFileManifestSHA256 = "ecab4392e31df7afee323dc2b9be0b9487dba8fe4113cd0d2a81b1761c39b2df"

type parserCaseCorpus struct {
	SchemaVersion int                `json:"schemaVersion"`
	Tag           string             `json:"tag"`
	Commit        string             `json:"commit"`
	Manifest      string             `json:"manifest"`
	ManifestHash  string             `json:"manifestSHA256"`
	Files         []string           `json:"files"`
	Records       []parserCaseRecord `json:"records"`
	Summary       parserCaseSummary  `json:"summary"`
}

type parserCaseRecord struct {
	ID            string              `json:"id"`
	Path          string              `json:"path"`
	Line          int                 `json:"line"`
	Offset        int                 `json:"offset"`
	CallStart     int                 `json:"callStart"`
	CallEnd       int                 `json:"callEnd"`
	Helper        string              `json:"helper"`
	ArgumentKind  string              `json:"argumentKind"`
	InputKind     string              `json:"inputKind,omitempty"`
	InputStart    int                 `json:"inputStart,omitempty"`
	InputEnd      int                 `json:"inputEnd,omitempty"`
	ErrorArgument string              `json:"errorArgument,omitempty"`
	Disposition   string              `json:"disposition"`
	Reason        string              `json:"reason,omitempty"`
	Cases         []parserCaseVariant `json:"cases,omitempty"`
}

type parserCaseVariant struct {
	Name        string `json:"name"`
	Context     string `json:"context"`
	VimOutcome  string `json:"vimOutcome"`
	Expectation string `json:"parserExpectation"`
	Source      string `json:"source"`
}

type parserCaseSummary struct {
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

type helperSourceScope struct {
	Start int
	End   int
}

type helperSourceAssignment struct {
	Name       string
	Kind       string
	Start      int
	End        int
	ScopeStart int
	Lines      []string
	Reason     string
}

type helperSourceIndex struct {
	Source      []byte
	Scopes      []helperSourceScope
	Assignments []helperSourceAssignment
}

func validatePinnedParserCaseCorpus(corpus parserCaseCorpus) error {
	want := parserCaseSummary{
		Calls: 3844, ExtractedCalls: 3805, SkippedCalls: 39,
		Cases: 5261, AcceptedCases: 1761, UnclassifiedCases: 3500,
		DirectLists: 873, Heredocs: 2884, ListAssignments: 2, ListConcats: 46,
	}
	if corpus.SchemaVersion != 1 || corpus.Tag != vimTag || corpus.Commit != vimCommit || corpus.Manifest != "v9.2.1015-parser-files.json" || corpus.ManifestHash != pinnedParserFileManifestSHA256 || len(corpus.Files) != 44 || len(corpus.Records) != want.Calls || corpus.Summary != want {
		return fmt.Errorf("unexpected pinned parser case corpus: files=%d records=%d summary=%+v", len(corpus.Files), len(corpus.Records), corpus.Summary)
	}
	return nil
}

func buildParserCaseCorpus(files testFilesCorpus, inventory helperInventory, manifest parserFileManifest) (parserCaseCorpus, error) {
	result := parserCaseCorpus{SchemaVersion: 1, Tag: files.Tag, Commit: files.Commit, Manifest: "v9.2.1015-parser-files.json"}
	if files.Tag != vimTag || files.Commit != vimCommit || inventory.Tag != vimTag || inventory.Commit != vimCommit || manifest.Tag != vimTag || manifest.Commit != vimCommit {
		return result, fmt.Errorf("official parser case inputs have mismatched provenance")
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return result, fmt.Errorf("encode parser file manifest: %w", err)
	}
	result.ManifestHash = fmt.Sprintf("%x", sha256.Sum256(manifestJSON))
	selected, err := selectParserMigrationFiles(files, manifest)
	if err != nil {
		return result, err
	}
	selectedByPath := make(map[string]testFileRecord, len(selected))
	indexes := make(map[string]helperSourceIndex, len(selected))
	for _, file := range selected {
		result.Files = append(result.Files, file.Path)
		selectedByPath[file.Path] = file
		indexes[file.Path] = buildHelperSourceIndex(file.Source)
	}

	for _, helper := range inventory.Records {
		if helper.Disposition != "pending-extraction" {
			continue
		}
		file, ok := selectedByPath[helper.Path]
		if !ok {
			continue
		}
		record := parserCaseRecord{
			ID:   fmt.Sprintf("%s:%d:%d", helper.Path, helper.Line, helper.Offset),
			Path: helper.Path, Line: helper.Line, Offset: helper.Offset,
			CallStart: helper.CallStart, CallEnd: helper.CallEnd,
			Helper: helper.Name, ArgumentKind: helper.FirstArgument,
		}
		result.Summary.Calls++
		arguments, ok := parserHelperArguments(file.Source, helper)
		if !ok || len(arguments) == 0 {
			record.Disposition = "skipped"
			record.Reason = "helper call arguments are incomplete"
			result.Records = append(result.Records, record)
			result.Summary.SkippedCalls++
			continue
		}
		if len(arguments) > 1 && strings.Contains(helper.Name, "Failure") {
			record.ErrorArgument = string(file.Source[arguments[1].Start:arguments[1].End])
		}

		lines, binding, reason := resolveParserHelperSource(indexes[helper.Path], helper, arguments[0])
		if reason != "" {
			record.Disposition = "skipped"
			record.Reason = reason
			result.Records = append(result.Records, record)
			result.Summary.SkippedCalls++
			continue
		}
		record.InputKind = binding.Kind
		record.InputStart = binding.Start
		record.InputEnd = binding.End
		record.Cases, ok = expandParserHelper(helper.Name, lines)
		if !ok {
			record.Disposition = "skipped"
			record.Reason = "helper source transformation is not implemented"
			result.Records = append(result.Records, record)
			result.Summary.SkippedCalls++
			continue
		}
		record.Disposition = "extracted"
		result.Summary.ExtractedCalls++
		switch binding.Kind {
		case "direct-list":
			result.Summary.DirectLists++
		case "heredoc":
			result.Summary.Heredocs++
		case "list-assignment":
			result.Summary.ListAssignments++
		case "list-concat":
			result.Summary.ListConcats++
		}
		for _, parserCase := range record.Cases {
			result.Summary.Cases++
			if parserCase.Expectation == "accept" {
				result.Summary.AcceptedCases++
			} else {
				result.Summary.UnclassifiedCases++
			}
		}
		result.Records = append(result.Records, record)
	}
	return result, nil
}

func parserHelperArguments(source []byte, helper helperRecord) ([]helperArgument, bool) {
	open := helper.LexemeEnd
	for open < helper.CallEnd && isHelperSpace(source[open]) {
		open++
	}
	return splitHelperArguments(source, open, helper.CallEnd)
}

func resolveParserHelperSource(index helperSourceIndex, helper helperRecord, argument helperArgument) ([]string, helperSourceAssignment, string) {
	return resolveParserHelperExpression(index, helper.CallStart, helperScopeStart(index.Scopes, helper.CallStart), argument)
}

func resolveParserHelperExpression(index helperSourceIndex, before, scope int, argument helperArgument) ([]string, helperSourceAssignment, string) {
	if values, ok := decodeStaticStringList(index.Source, argument); ok {
		return values, helperSourceAssignment{Kind: "direct-list", Start: argument.Start, End: argument.End}, ""
	}
	name := strings.TrimSpace(string(index.Source[argument.Start:argument.End]))
	if helperArgumentIdentifier(name) {
		return resolveParserHelperIdentifier(index, before, scope, name)
	}
	terms, ok := splitHelperListConcat(index.Source, argument)
	if !ok {
		return nil, helperSourceAssignment{}, "first argument is not a static string list, identifier, or list concat"
	}
	var lines []string
	for _, term := range terms {
		values, _, reason := resolveParserHelperExpression(index, before, scope, term)
		if reason != "" {
			return nil, helperSourceAssignment{}, "list concat term is not static: " + reason
		}
		lines = append(lines, values...)
	}
	return lines, helperSourceAssignment{Kind: "list-concat", Start: argument.Start, End: argument.End}, ""
}

func resolveParserHelperIdentifier(index helperSourceIndex, before, scope int, name string) ([]string, helperSourceAssignment, string) {
	var best *helperSourceAssignment
	for assignmentIndex := range index.Assignments {
		assignment := &index.Assignments[assignmentIndex]
		if assignment.Name != name || assignment.End > before || assignment.ScopeStart != scope {
			continue
		}
		if best == nil || assignment.Start > best.Start {
			best = assignment
		}
	}
	if best == nil {
		return nil, helperSourceAssignment{}, "identifier has no preceding same-scope assignment"
	}
	if best.Reason != "" {
		return nil, *best, best.Reason
	}
	return append([]string(nil), best.Lines...), *best, ""
}

func splitHelperListConcat(source []byte, argument helperArgument) ([]helperArgument, bool) {
	if argument.Start < 0 || argument.End > len(source) || argument.Start >= argument.End {
		return nil, false
	}
	start := trimHelperSpace(source, argument.Start, argument.End)
	end := trimHelperSpaceRight(source, start, argument.End)
	var delimiters []byte
	var terms []helperArgument
	termStart := start
	for index := start; index < end; {
		switch source[index] {
		case '\'', '"':
			next, ok := skipHelperString(source, index, end)
			if !ok {
				return nil, false
			}
			index = next
		case '(', '[', '{':
			delimiters = append(delimiters, source[index])
			index++
		case ')', ']', '}':
			if len(delimiters) == 0 || !matchingHelperDelimiter(delimiters[len(delimiters)-1], source[index]) {
				return nil, false
			}
			delimiters = delimiters[:len(delimiters)-1]
			index++
		case '#':
			if len(delimiters) == 0 && (index == termStart || isHelperSpace(source[index-1])) {
				return nil, false
			}
			index++
		case '+':
			if len(delimiters) != 0 {
				index++
				continue
			}
			if (index+1 < end && (source[index+1] == '+' || source[index+1] == '=')) || (index > start && source[index-1] == '+') {
				return nil, false
			}
			termEnd := trimHelperSpaceRight(source, termStart, index)
			if termStart >= termEnd {
				return nil, false
			}
			terms = append(terms, helperArgument{Start: termStart, End: termEnd})
			termStart = trimHelperSpace(source, index+1, end)
			index++
		default:
			index++
		}
	}
	if len(delimiters) != 0 || len(terms) == 0 {
		return nil, false
	}
	termEnd := trimHelperSpaceRight(source, termStart, end)
	if termStart >= termEnd {
		return nil, false
	}
	terms = append(terms, helperArgument{Start: termStart, End: termEnd})
	return terms, true
}

func helperArgumentIdentifier(name string) bool {
	if name == "" {
		return false
	}
	start := 0
	if len(name) >= 2 && name[1] == ':' && strings.ContainsRune("gbwtslav", rune(name[0])) {
		start = 2
	}
	if start >= len(name) || !isHelperVariableStart(name[start]) {
		return false
	}
	for index := start + 1; index < len(name); index++ {
		if !isHelperVariablePart(name[index]) {
			return false
		}
	}
	return true
}

func buildHelperSourceIndex(source []byte) helperSourceIndex {
	heredocs := scanHelperHeredocs(source)
	scopes := scanHelperSourceScopes(source, heredocs)
	assignments := make([]helperSourceAssignment, 0, len(heredocs)+4)
	for _, heredoc := range heredocs {
		assignment := helperSourceAssignment{
			Name: heredoc.Name, Kind: "heredoc", Start: heredoc.HeaderStart, End: heredoc.End,
			ScopeStart: helperScopeStart(scopes, heredoc.HeaderStart), Lines: append([]string(nil), heredoc.Lines...),
		}
		if !heredoc.Complete {
			assignment.Reason = "identifier resolves to an incomplete heredoc"
		} else if heredoc.Evaluate {
			assignment.Reason = "identifier resolves to an eval heredoc"
		}
		assignments = append(assignments, assignment)
	}
	assignments = append(assignments, scanHelperListAssignments(source, heredocs, scopes)...)
	return helperSourceIndex{Source: source, Scopes: scopes, Assignments: assignments}
}

func scanHelperSourceScopes(source []byte, heredocs []helperHeredoc) []helperSourceScope {
	lines := splitHelperSourceLines(source)
	stack := []int{0}
	var scopes []helperSourceScope
	for lineIndex := 0; lineIndex < len(lines); lineIndex++ {
		line := lines[lineIndex]
		if heredoc, ok := helperHeredocAt(heredocs, line.Start); ok {
			for lineIndex+1 < len(lines) && lines[lineIndex+1].Start < heredoc.End {
				lineIndex++
			}
			continue
		}
		trimmed := bytes.TrimSpace(source[line.Start:line.ContentEnd])
		if len(trimmed) == 0 || trimmed[0] == '#' || trimmed[0] == '"' {
			continue
		}
		trimmed, _ = trimHelperCommandModifiers(trimmed, helperLegacy)
		if helperLeadingCommand(trimmed, "enddef") || helperLeadingCommand(trimmed, "endfunc") || helperLeadingCommand(trimmed, "endfunction") {
			if len(stack) > 1 {
				start := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				scopes = append(scopes, helperSourceScope{Start: start, End: line.End})
			}
			continue
		}
		trimmed = trimHelperDeclarationModifiers(trimmed)
		if helperLeadingCommand(trimmed, "def") || helperLeadingCommand(trimmed, "def!") ||
			helperLeadingCommand(trimmed, "func") || helperLeadingCommand(trimmed, "func!") ||
			helperLeadingCommand(trimmed, "function") || helperLeadingCommand(trimmed, "function!") {
			stack = append(stack, line.Start)
		}
	}
	for len(stack) > 1 {
		start := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		scopes = append(scopes, helperSourceScope{Start: start, End: len(source)})
	}
	scopes = append(scopes, helperSourceScope{Start: 0, End: len(source)})
	return scopes
}

func helperScopeStart(scopes []helperSourceScope, offset int) int {
	start := 0
	for _, scope := range scopes {
		if offset >= scope.Start && offset < scope.End && scope.Start >= start {
			start = scope.Start
		}
	}
	return start
}

func helperHeredocAt(heredocs []helperHeredoc, start int) (helperHeredoc, bool) {
	for _, heredoc := range heredocs {
		if heredoc.HeaderStart == start {
			return heredoc, true
		}
	}
	return helperHeredoc{}, false
}

func scanHelperListAssignments(source []byte, heredocs []helperHeredoc, scopes []helperSourceScope) []helperSourceAssignment {
	lines := splitHelperSourceLines(source)
	var assignments []helperSourceAssignment
	for lineIndex := 0; lineIndex < len(lines); lineIndex++ {
		line := lines[lineIndex]
		if heredoc, ok := helperHeredocAt(heredocs, line.Start); ok {
			for lineIndex+1 < len(lines) && lines[lineIndex+1].Start < heredoc.End {
				lineIndex++
			}
			continue
		}
		name, operator, valueStart, ok := parseHelperLineAssignment(source, line)
		if !ok {
			continue
		}
		assignment := helperSourceAssignment{
			Name: name, Kind: "dynamic-assignment", Start: line.Start, End: line.End,
			ScopeStart: helperScopeStart(scopes, line.Start), Reason: "identifier resolves to a dynamic or mutated assignment",
		}
		if operator == "=" && valueStart < len(source) && source[valueStart] == '[' {
			close, complete := helperMatchingDelimiter(source, valueStart, '[', ']')
			if complete && helperListHasOnlyTrailingSpace(source, close+1) {
				argument := helperArgument{Start: valueStart, End: close + 1}
				if values, decoded := decodeStaticStringList(source, argument); decoded {
					assignment.Kind = "list-assignment"
					assignment.End = close + 1
					assignment.Lines = values
					assignment.Reason = ""
				}
			}
		}
		assignments = append(assignments, assignment)
	}
	return assignments
}

func parseHelperLineAssignment(source []byte, line helperSourceLine) (string, string, int, bool) {
	content := source[line.Start:line.ContentEnd]
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 || trimmed[0] == '#' || trimmed[0] == '"' {
		return "", "", 0, false
	}
	trimmed, _ = trimHelperCommandModifiers(trimmed, helperLegacy)
	for _, command := range []string{"let", "var", "const", "final"} {
		if helperLeadingWord(trimmed, command) {
			trimmed = bytes.TrimSpace(trimmed[len(command):])
			break
		}
	}
	if len(trimmed) == 0 {
		return "", "", 0, false
	}
	nameEnd := 0
	if len(trimmed) >= 2 && trimmed[1] == ':' && strings.ContainsRune("gbwtslav", rune(trimmed[0])) {
		nameEnd = 2
	}
	if nameEnd >= len(trimmed) || !isHelperVariableStart(trimmed[nameEnd]) {
		return "", "", 0, false
	}
	nameEnd++
	for nameEnd < len(trimmed) && isHelperVariablePart(trimmed[nameEnd]) {
		nameEnd++
	}
	name := string(trimmed[:nameEnd])
	operatorStart, operator := helperAssignmentOperator(trimmed, nameEnd)
	if operator == "" {
		return "", "", 0, false
	}
	absoluteTrimmed := line.Start + bytes.Index(content, trimmed)
	valueStart := absoluteTrimmed + operatorStart + len(operator)
	for valueStart < line.ContentEnd && isHelperHorizontalSpace(source[valueStart]) {
		valueStart++
	}
	return name, operator, valueStart, true
}

func helperAssignmentOperator(source []byte, start int) (int, string) {
	for index := start; index < len(source); index++ {
		switch source[index] {
		case '\'', '"':
			next, ok := skipHelperString(source, index, len(source))
			if !ok {
				return 0, ""
			}
			index = next - 1
		case '#':
			if index == 0 || isHelperSpace(source[index-1]) {
				return 0, ""
			}
		case '+', '-':
			if index+1 < len(source) && source[index+1] == '=' {
				return index, string(source[index : index+2])
			}
		case '.':
			if index+1 < len(source) && source[index+1] == '=' {
				return index, ".="
			}
			if index+2 < len(source) && source[index+1] == '.' && source[index+2] == '=' {
				return index, "..="
			}
		case '=':
			if index+1 < len(source) && (source[index+1] == '=' || source[index+1] == '~' || source[index+1] == '>' || source[index+1] == '<') {
				continue
			}
			if index > 0 && (source[index-1] == '=' || source[index-1] == '!' || source[index-1] == '<' || source[index-1] == '>') {
				continue
			}
			return index, "="
		case '|':
			return 0, ""
		}
	}
	return 0, ""
}

func helperListHasOnlyTrailingSpace(source []byte, start int) bool {
	for start < len(source) && source[start] != '\n' && source[start] != '\r' {
		if source[start] != ' ' && source[start] != '\t' {
			return false
		}
		start++
	}
	return true
}

func expandParserHelper(name string, lines []string) ([]parserCaseVariant, bool) {
	script := func(caseName, outcome, expectation string, sourceLines []string) parserCaseVariant {
		return parserCaseVariant{Name: caseName, Context: "script", VimOutcome: outcome, Expectation: expectation, Source: helperLinesSource(sourceLines)}
	}
	def := func(caseName, outcome, expectation string, sourceLines []string, comments, compile bool) parserCaseVariant {
		return parserCaseVariant{Name: caseName, Context: "def", VimOutcome: outcome, Expectation: expectation, Source: helperDefSource(sourceLines, comments, compile)}
	}
	legacy := func(caseName, outcome, expectation string, sourceLines []string, call bool) parserCaseVariant {
		return parserCaseVariant{Name: caseName, Context: "legacy-function", VimOutcome: outcome, Expectation: expectation, Source: helperLegacySource(sourceLines, call)}
	}
	vim9Lines := func(sourceLines []string) []string { return append([]string{"vim9script"}, sourceLines...) }

	switch name {
	case "CheckDefSuccess", "CheckDefCompileSuccess":
		return []parserCaseVariant{def("def", "success", "accept", lines, false, true)}, true
	case "CheckDefFailure":
		return []parserCaseVariant{def("def", "failure", "unclassified", lines, true, true)}, true
	case "CheckDefExecFailure":
		return []parserCaseVariant{def("def", "failure", "unclassified", lines, true, false)}, true
	case "CheckScriptSuccess", "CheckSourceScriptSuccess", "CheckSourceSuccess":
		return []parserCaseVariant{script("script", "success", "accept", lines)}, true
	case "CheckScriptFailure", "CheckScriptFailureList", "CheckSourceScriptFailure", "CheckSourceScriptFailureList", "CheckSourceFailure", "CheckSourceFailureList":
		return []parserCaseVariant{script("script", "failure", "unclassified", lines)}, true
	case "CheckDefAndScriptSuccess":
		return []parserCaseVariant{
			def("def", "success", "accept", lines, false, true),
			script("vim9-script", "success", "accept", vim9Lines(lines)),
		}, true
	case "CheckDefAndScriptFailure":
		return []parserCaseVariant{
			def("def", "failure", "unclassified", lines, true, true),
			script("vim9-script", "failure", "unclassified", vim9Lines(lines)),
		}, true
	case "CheckDefExecAndScriptFailure":
		return []parserCaseVariant{
			def("def", "failure", "unclassified", lines, true, false),
			script("vim9-script", "failure", "unclassified", vim9Lines(lines)),
		}, true
	case "CheckLegacySuccess":
		return []parserCaseVariant{legacy("legacy", "success", "accept", lines, false)}, true
	case "CheckLegacyFailure":
		return []parserCaseVariant{legacy("legacy", "failure", "unclassified", lines, true)}, true
	case "CheckTransLegacySuccess":
		return []parserCaseVariant{legacy("legacy", "success", "accept", helperLegacyTransform(lines), false)}, true
	case "CheckTransDefSuccess":
		return []parserCaseVariant{def("def", "success", "accept", helperVim9Transform(lines), false, true)}, true
	case "CheckTransVim9Success":
		return []parserCaseVariant{script("vim9-script", "success", "accept", vim9Lines(helperVim9Transform(lines)))}, true
	case "CheckLegacyAndVim9Success":
		legacyLines := helperLegacyTransform(lines)
		modernLines := helperVim9Transform(lines)
		return []parserCaseVariant{
			legacy("legacy", "success", "accept", legacyLines, false),
			def("def", "success", "accept", modernLines, false, true),
			script("vim9-script", "success", "accept", vim9Lines(modernLines)),
		}, true
	case "CheckLegacyAndVim9Failure":
		legacyLines := helperLegacyTransform(lines)
		modernLines := helperVim9Transform(lines)
		return []parserCaseVariant{
			legacy("legacy", "failure", "unclassified", legacyLines, true),
			def("def", "failure", "unclassified", modernLines, true, false),
			script("vim9-script", "failure", "unclassified", vim9Lines(modernLines)),
		}, true
	case "CheckSourceLegacySuccess":
		return []parserCaseVariant{legacy("legacy", "success", "accept", lines, true)}, true
	case "CheckSourceLegacyFailure":
		return []parserCaseVariant{legacy("legacy", "failure", "unclassified", lines, true)}, true
	case "CheckSourceTransLegacySuccess":
		return []parserCaseVariant{legacy("legacy", "success", "accept", helperLegacyTransform(lines), true)}, true
	case "CheckSourceTransDefSuccess":
		return []parserCaseVariant{def("def", "success", "accept", helperVim9Transform(lines), false, true)}, true
	case "CheckSourceTransVim9Success":
		return []parserCaseVariant{script("vim9-script", "success", "accept", vim9Lines(helperVim9Transform(lines)))}, true
	case "CheckSourceLegacyAndVim9Success":
		legacyLines := helperLegacyTransform(lines)
		modernLines := helperVim9Transform(lines)
		return []parserCaseVariant{
			legacy("legacy", "success", "accept", legacyLines, true),
			def("def", "success", "accept", modernLines, false, true),
			script("vim9-script", "success", "accept", vim9Lines(modernLines)),
		}, true
	case "CheckSourceDefSuccess":
		return []parserCaseVariant{def("def", "success", "accept", lines, false, true)}, true
	case "CheckSourceDefCompileSuccess":
		return []parserCaseVariant{def("def", "success", "accept", lines, true, true)}, true
	case "CheckSourceDefFailure":
		return []parserCaseVariant{def("def", "failure", "unclassified", lines, true, true)}, true
	case "CheckSourceDefExecFailure":
		return []parserCaseVariant{def("def", "failure", "unclassified", lines, true, false)}, true
	case "CheckSourceDefAndScriptFailure":
		return []parserCaseVariant{
			def("def", "failure", "unclassified", lines, true, true),
			script("vim9-script", "failure", "unclassified", vim9Lines(lines)),
		}, true
	case "CheckSourceDefExecAndScriptFailure":
		return []parserCaseVariant{
			def("def", "failure", "unclassified", lines, true, false),
			script("vim9-script", "failure", "unclassified", vim9Lines(lines)),
		}, true
	case "CheckSourceDefAndScriptSuccess":
		return []parserCaseVariant{
			def("def", "success", "accept", lines, false, true),
			script("vim9-script", "success", "accept", vim9Lines(lines)),
		}, true
	case "CheckSourceLegacyAndVim9Failure":
		legacyLines := helperLegacyTransform(lines)
		modernLines := helperVim9Transform(lines)
		return []parserCaseVariant{
			legacy("legacy", "failure", "unclassified", legacyLines, true),
			def("def", "failure", "unclassified", modernLines, true, false),
			script("vim9-script", "failure", "unclassified", vim9Lines(modernLines)),
		}, true
	default:
		return nil, false
	}
}

func helperLinesSource(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func helperDefSource(lines []string, comments, compile bool) string {
	wrapped := []string{"def Func()"}
	if comments {
		wrapped = append(wrapped, "# comment")
	}
	wrapped = append(wrapped, lines...)
	if comments {
		wrapped = append(wrapped, "#comment")
	}
	wrapped = append(wrapped, "enddef")
	if compile {
		wrapped = append(wrapped, "defcompile")
	}
	return helperLinesSource(wrapped)
}

func helperLegacySource(lines []string, call bool) string {
	wrapped := append([]string{"func Func()"}, lines...)
	wrapped = append(wrapped, "endfunc")
	if call {
		wrapped = append(wrapped, "call Func()")
	}
	return helperLinesSource(wrapped)
}

func helperLegacyTransform(lines []string) []string {
	result := make([]string, len(lines))
	for index, line := range lines {
		line = replaceHelperWord(line, "VAR", "let")
		line = replaceHelperWord(line, "LET", "let")
		line = replaceHelperWord(line, "LSTART", "{")
		line = replaceHelperWord(line, "LMIDDLE", "->")
		line = replaceHelperWord(line, "LEND", "}")
		line = replaceHelperWord(line, "TRUE", "1")
		line = replaceHelperWord(line, "FALSE", "0")
		result[index] = strings.ReplaceAll(line, "#\"", " \"")
	}
	return result
}

func helperVim9Transform(lines []string) []string {
	result := make([]string, len(lines))
	for index, line := range lines {
		line = replaceHelperWord(line, "VAR", "var")
		line = removeHelperLetPrefix(line)
		line = replaceHelperWord(line, "LSTART", "(")
		line = replaceHelperWord(line, "LMIDDLE", ") =>")
		line = removeHelperLend(line)
		line = replaceHelperWord(line, "TRUE", "true")
		result[index] = replaceHelperWord(line, "FALSE", "false")
	}
	return result
}

func replaceHelperWord(source, word, replacement string) string {
	for start := 0; start+len(word) <= len(source); {
		position := strings.Index(source[start:], word)
		if position < 0 {
			break
		}
		position += start
		end := position + len(word)
		if (position == 0 || !isHelperWordByte(source[position-1])) && (end == len(source) || !isHelperWordByte(source[end])) {
			source = source[:position] + replacement + source[end:]
			start = position + len(replacement)
		} else {
			start = end
		}
	}
	return source
}

func removeHelperLetPrefix(source string) string {
	for start := 0; start+4 <= len(source); {
		position := strings.Index(source[start:], "LET ")
		if position < 0 {
			break
		}
		position += start
		if position == 0 || !isHelperWordByte(source[position-1]) {
			source = source[:position] + source[position+4:]
			start = position
		} else {
			start = position + 4
		}
	}
	return source
}

func removeHelperLend(source string) string {
	for start := 0; start+4 <= len(source); {
		position := strings.Index(source[start:], "LEND")
		if position < 0 {
			break
		}
		position += start
		end := position + 4
		if (position == 0 || !isHelperWordByte(source[position-1])) && (end == len(source) || !isHelperWordByte(source[end])) {
			left := position
			for left > 0 && source[left-1] == ' ' {
				left--
			}
			right := end
			for right < len(source) && source[right] == ' ' {
				right++
			}
			source = source[:left] + source[right:]
			start = left
		} else {
			start = end
		}
	}
	return source
}

func isHelperWordByte(value byte) bool {
	return value == '_' || (value >= '0' && value <= '9') || (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z')
}
