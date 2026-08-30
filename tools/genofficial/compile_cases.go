package main

import (
	"fmt"
	"regexp"
	"sort"
)

var compileErrorCodePattern = regexp.MustCompile(`E[0-9]+`)

// compileCaseCorpus contains every official helper case that expects static
// Vim9 source or :def compilation to fail. Function-execution helpers remain
// separate because they may depend on runtime values.
type compileCaseCorpus struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Tag           string              `json:"tag"`
	Commit        string              `json:"commit"`
	Files         []string            `json:"files"`
	Records       []compileCaseRecord `json:"records"`
	Summary       compileCaseSummary  `json:"summary"`
}

type compileCaseRecord struct {
	ID            string               `json:"id"`
	Path          string               `json:"path"`
	Line          int                  `json:"line"`
	Offset        int                  `json:"offset"`
	CallStart     int                  `json:"callStart"`
	CallEnd       int                  `json:"callEnd"`
	Helper        string               `json:"helper"`
	InputKind     string               `json:"inputKind,omitempty"`
	InputStart    int                  `json:"inputStart,omitempty"`
	InputEnd      int                  `json:"inputEnd,omitempty"`
	ErrorArgument string               `json:"errorArgument,omitempty"`
	Disposition   string               `json:"disposition"`
	Reason        string               `json:"reason,omitempty"`
	Cases         []compileCaseVariant `json:"cases,omitempty"`
}

type compileCaseVariant struct {
	Name         string `json:"name"`
	Context      string `json:"context"`
	ExpectedCode string `json:"expectedCode,omitempty"`
	Source       string `json:"source"`
}

type compileCaseSummary struct {
	Calls           int `json:"calls"`
	ExtractedCalls  int `json:"extractedCalls"`
	SkippedCalls    int `json:"skippedCalls"`
	Cases           int `json:"cases"`
	ExpectedCodes   int `json:"expectedCodes"`
	UnresolvedCode  int `json:"unresolvedCodes"`
	DirectLists     int `json:"directLists"`
	Heredocs        int `json:"heredocs"`
	ListAssignments int `json:"listAssignments"`
	ListConcats     int `json:"listConcats"`
}

func buildCompileCaseCorpus(files testFilesCorpus, inventory helperInventory) (compileCaseCorpus, error) {
	result := compileCaseCorpus{SchemaVersion: 2, Tag: files.Tag, Commit: files.Commit}
	if files.Tag != vimTag || files.Commit != vimCommit || inventory.Tag != vimTag || inventory.Commit != vimCommit {
		return result, fmt.Errorf("official compile case inputs have mismatched provenance")
	}
	sources := make(map[string]testFileRecord, len(files.Files))
	indexes := make(map[string]helperSourceIndex, len(files.Files))
	for _, file := range files.Files {
		sources[file.Path] = file
		indexes[file.Path] = buildHelperSourceIndex(file.Source)
	}
	fileSet := make(map[string]struct{})
	for _, helper := range inventory.Records {
		if helper.Disposition != "pending-extraction" || !isCompileFailureHelper(helper.Name) {
			continue
		}
		file, ok := sources[helper.Path]
		if !ok {
			return result, fmt.Errorf("compile helper source %q is absent from pinned corpus", helper.Path)
		}
		fileSet[helper.Path] = struct{}{}
		record := compileCaseRecord{
			ID:   fmt.Sprintf("%s:%d:%d", helper.Path, helper.Line, helper.Offset),
			Path: helper.Path, Line: helper.Line, Offset: helper.Offset,
			CallStart: helper.CallStart, CallEnd: helper.CallEnd, Helper: helper.Name,
		}
		result.Summary.Calls++
		arguments, complete := parserHelperArguments(file.Source, helper)
		if !complete || len(arguments) < 2 {
			record.Disposition = "skipped"
			record.Reason = "helper call does not have complete source and error arguments"
			result.Summary.SkippedCalls++
			result.Records = append(result.Records, record)
			continue
		}
		record.ErrorArgument = string(file.Source[arguments[1].Start:arguments[1].End])
		lines, binding, reason := resolveParserHelperSource(indexes[helper.Path], helper, arguments[0])
		if reason != "" {
			record.Disposition = "skipped"
			record.Reason = reason
			result.Summary.SkippedCalls++
			result.Records = append(result.Records, record)
			continue
		}
		record.InputKind = binding.Kind
		record.InputStart = binding.Start
		record.InputEnd = binding.End
		parserCases, ok := expandParserHelper(helper.Name, lines)
		if !ok || len(parserCases) == 0 {
			record.Disposition = "skipped"
			record.Reason = "compile helper source transformation is not implemented"
			result.Summary.SkippedCalls++
			result.Records = append(result.Records, record)
			continue
		}
		expectedCodes := compileExpectedCodes(file.Source, arguments[1], helper.Name, len(parserCases))
		for index, parserCase := range parserCases {
			variant := compileCaseVariant{Name: parserCase.Name, Context: parserCase.Context, Source: parserCase.Source}
			if len(expectedCodes) == len(parserCases) {
				variant.ExpectedCode = expectedCodes[index]
			}
			record.Cases = append(record.Cases, variant)
			result.Summary.Cases++
			if variant.ExpectedCode == "" {
				result.Summary.UnresolvedCode++
			} else {
				result.Summary.ExpectedCodes++
			}
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
		result.Records = append(result.Records, record)
	}
	result.Files = make([]string, 0, len(fileSet))
	for path := range fileSet {
		result.Files = append(result.Files, path)
	}
	sort.Strings(result.Files)
	return result, nil
}

func isCompileFailureHelper(name string) bool {
	switch name {
	case "CheckDefFailure", "CheckDefAndScriptFailure", "CheckSourceDefFailure", "CheckSourceDefAndScriptFailure":
		return true
	default:
		return false
	}
}

func compileExpectedCodes(source []byte, argument helperArgument, helper string, caseCount int) []string {
	values, ok := compileErrorValues(source, argument)
	if !ok || len(values) == 0 {
		return nil
	}
	if helper == "CheckDefFailure" || helper == "CheckSourceDefFailure" {
		if len(values) != 1 || caseCount != 1 {
			return nil
		}
	} else {
		if len(values) == 1 && caseCount == 2 {
			values = []string{values[0], values[0]}
		} else if len(values) != caseCount {
			return nil
		}
	}
	result := make([]string, len(values))
	for index, value := range values {
		codes := compileErrorCodePattern.FindAllString(value, -1)
		if len(codes) != 1 {
			return nil
		}
		result[index] = "vim/" + codes[0]
	}
	return result
}

func compileErrorValues(source []byte, argument helperArgument) ([]string, bool) {
	start := trimHelperSpace(source, argument.Start, argument.End)
	end := trimHelperSpaceRight(source, start, argument.End)
	if start >= end {
		return nil, false
	}
	if source[start] == '[' {
		return decodeStaticStringList(source, helperArgument{Start: start, End: end})
	}
	var value string
	var next int
	var ok bool
	switch source[start] {
	case '\'':
		value, next, ok = decodeHelperSingleString(source, start, end)
	case '"':
		value, next, ok = decodeHelperDoubleString(source, start, end)
	default:
		return nil, false
	}
	if !ok || trimHelperSpace(source, next, end) != end {
		return nil, false
	}
	return []string{value}, true
}
