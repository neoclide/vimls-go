package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var compileErrorCodePattern = regexp.MustCompile(`E[0-9]+`)

// compileCaseCorpus contains every official helper case that explicitly runs
// :defcompile and expects compilation to fail.  Script-source and function-
// execution helpers are intentionally separate: they may depend on runtime
// values and are not evidence for a compile-time diagnostic.
type compileCaseCorpus struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Tag           string              `json:"tag"`
	Commit        string              `json:"commit"`
	Files         []string            `json:"files"`
	Records       []compileCaseRecord `json:"records"`
	Summary       compileCaseSummary  `json:"summary"`
}

type compileCaseRecord struct {
	ID            string `json:"id"`
	Path          string `json:"path"`
	Line          int    `json:"line"`
	Offset        int    `json:"offset"`
	CallStart     int    `json:"callStart"`
	CallEnd       int    `json:"callEnd"`
	Helper        string `json:"helper"`
	InputKind     string `json:"inputKind,omitempty"`
	InputStart    int    `json:"inputStart,omitempty"`
	InputEnd      int    `json:"inputEnd,omitempty"`
	ErrorArgument string `json:"errorArgument,omitempty"`
	ExpectedCode  string `json:"expectedCode,omitempty"`
	Source        string `json:"source,omitempty"`
	Disposition   string `json:"disposition"`
	Reason        string `json:"reason,omitempty"`
}

type compileCaseSummary struct {
	Calls           int `json:"calls"`
	ExtractedCalls  int `json:"extractedCalls"`
	SkippedCalls    int `json:"skippedCalls"`
	ExpectedCodes   int `json:"expectedCodes"`
	UnresolvedCode  int `json:"unresolvedCodes"`
	DirectLists     int `json:"directLists"`
	Heredocs        int `json:"heredocs"`
	ListAssignments int `json:"listAssignments"`
	ListConcats     int `json:"listConcats"`
}

func buildCompileCaseCorpus(files testFilesCorpus, inventory helperInventory) (compileCaseCorpus, error) {
	result := compileCaseCorpus{SchemaVersion: 1, Tag: files.Tag, Commit: files.Commit}
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
			ID:   fmt.Sprintf("%s:%d:%d/defcompile", helper.Path, helper.Line, helper.Offset),
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
		record.Source = helperDefSource(lines, true, true)
		record.ExpectedCode = compileExpectedCode(file.Source, arguments[1], helper.Name)
		record.Disposition = "extracted"
		result.Summary.ExtractedCalls++
		if record.ExpectedCode == "" {
			result.Summary.UnresolvedCode++
		} else {
			result.Summary.ExpectedCodes++
		}
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

func compileExpectedCode(source []byte, argument helperArgument, helper string) string {
	values, ok := compileErrorValues(source, argument)
	if !ok || len(values) == 0 {
		return ""
	}
	value := values[0]
	if helper == "CheckDefFailure" || helper == "CheckSourceDefFailure" {
		if len(values) != 1 {
			return ""
		}
	} else if len(values) != 1 && len(values) != 2 {
		return ""
	}
	codes := compileErrorCodePattern.FindAllString(value, -1)
	if len(codes) != 1 {
		return ""
	}
	return "vim/" + codes[0]
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

func checkCompileCaseCorpus(corpus compileCaseCorpus) error {
	if corpus.SchemaVersion != 1 || corpus.Tag != vimTag || corpus.Commit != vimCommit {
		return fmt.Errorf("unexpected compile case provenance: schema=%d tag=%q commit=%q", corpus.SchemaVersion, corpus.Tag, corpus.Commit)
	}
	if corpus.Summary.Calls != len(corpus.Records) || corpus.Summary.ExtractedCalls+corpus.Summary.SkippedCalls != corpus.Summary.Calls || corpus.Summary.ExpectedCodes+corpus.Summary.UnresolvedCode != corpus.Summary.ExtractedCalls {
		return fmt.Errorf("inconsistent compile case summary: records=%d summary=%+v", len(corpus.Records), corpus.Summary)
	}
	if !sort.StringsAreSorted(corpus.Files) {
		return fmt.Errorf("compile case files are not sorted")
	}
	for index, record := range corpus.Records {
		if index > 0 && (corpus.Records[index-1].Path > record.Path || corpus.Records[index-1].Path == record.Path && corpus.Records[index-1].Offset >= record.Offset) {
			return fmt.Errorf("compile case records are not strictly ordered at %d", index)
		}
		if !strings.HasSuffix(record.ID, "/defcompile") || record.Path == "" || record.Line < 1 || record.CallEnd <= record.CallStart {
			return fmt.Errorf("invalid compile case record %d: %#v", index, record)
		}
		switch record.Disposition {
		case "extracted":
			if record.Source == "" || record.InputKind == "" || record.InputEnd <= record.InputStart || record.Reason != "" {
				return fmt.Errorf("invalid extracted compile case %d: %#v", index, record)
			}
		case "skipped":
			if record.Source != "" || record.Reason == "" {
				return fmt.Errorf("invalid skipped compile case %d: %#v", index, record)
			}
		default:
			return fmt.Errorf("invalid compile case disposition %q", record.Disposition)
		}
	}
	return nil
}
