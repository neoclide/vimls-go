package main

import (
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

type rebaseReport struct {
	From            rebaseProvenance   `json:"from"`
	To              rebaseProvenance   `json:"to"`
	Total           int                `json:"total"`
	Unique          int                `json:"unique"`
	Ordered         int                `json:"ordered"`
	Reviewed        int                `json:"reviewed"`
	Unresolved      int                `json:"unresolved"`
	Mappings        []rebaseMapping    `json:"mappings"`
	UnresolvedCases []rebaseUnresolved `json:"unresolvedCases,omitempty"`
}

type rebaseProvenance struct {
	Tag    string `json:"tag"`
	Commit string `json:"commit"`
}

type rebaseMapping struct {
	OldID      string `json:"oldID"`
	NewID      string `json:"newID"`
	Diagnostic string `json:"diagnostic"`
	Method     string `json:"method"`
}

type rebaseUnresolved struct {
	OldID         string            `json:"oldID"`
	Diagnostic    string            `json:"diagnostic"`
	ErrorArgument string            `json:"errorArgument"`
	Reason        string            `json:"reason"`
	Candidates    []rebaseCandidate `json:"candidates,omitempty"`
}

type rebaseCandidate struct {
	ID            string `json:"id"`
	ErrorArgument string `json:"errorArgument"`
}

type rebaseCase struct {
	ID string
	rebaseFullFingerprint
}

func rebaseParserAssertions(fromPath, toPath, assertionsPath, outputPath string, reviews []string) (rebaseReport, error) {
	from, err := readRebaseCorpus(fromPath)
	if err != nil {
		return rebaseReport{}, fmt.Errorf("read -rebase-from: %w", err)
	}
	to, err := readRebaseCorpus(toPath)
	if err != nil {
		return rebaseReport{}, fmt.Errorf("read -rebase-to: %w", err)
	}
	assertions, err := readParserAssertions(assertionsPath)
	if err != nil {
		return rebaseReport{}, err
	}
	reviewed, err := parseRebaseReviews(reviews)
	if err != nil {
		return rebaseReport{}, err
	}
	oldCases, oldGroups, _, err := rebaseCases(from)
	if err != nil {
		return rebaseReport{}, fmt.Errorf("invalid -rebase-from: %w", err)
	}
	newCases, newGroups, newInputGroups, err := rebaseCases(to)
	if err != nil {
		return rebaseReport{}, fmt.Errorf("invalid -rebase-to: %w", err)
	}
	report := rebaseReport{
		From:  rebaseProvenance{Tag: from.Tag, Commit: from.Commit},
		To:    rebaseProvenance{Tag: to.Tag, Commit: to.Commit},
		Total: len(assertions.entries),
	}
	usedTargets := make(map[string]string, len(assertions.entries))
	usedReviews := make(map[string]bool, len(reviewed))
	changes := make([]assertionKeyChange, 0, len(assertions.entries))
	for _, assertion := range assertions.entries {
		oldCase, ok := oldCases[assertion.key]
		if !ok {
			return report, fmt.Errorf("assertion %q is absent from -rebase-from", assertion.key)
		}
		var target rebaseCase
		method := ""
		if reviewedID, ok := reviewed[assertion.key]; ok {
			usedReviews[assertion.key] = true
			var exists bool
			target, exists = newCases[reviewedID]
			if !exists {
				return report, fmt.Errorf("review target %q for %q is absent from -rebase-to", reviewedID, assertion.key)
			}
			if oldCase.rebaseInputFingerprint != target.rebaseInputFingerprint {
				return report, fmt.Errorf("review target %q for %q changes source identity", reviewedID, assertion.key)
			}
			method = "reviewed"
		} else {
			oldGroup := oldGroups[oldCase.rebaseFullFingerprint]
			newGroup := newGroups[oldCase.rebaseFullFingerprint]
			switch {
			case len(oldGroup) == 1 && len(newGroup) == 1:
				target, method = newGroup[0], "unique"
			case len(oldGroup) == len(newGroup) && len(oldGroup) > 1:
				for index, value := range oldGroup {
					if value.ID == oldCase.ID {
						target, method = newGroup[index], "ordered"
						break
					}
				}
			default:
				unresolved := rebaseUnresolved{OldID: assertion.key, Diagnostic: assertion.value, ErrorArgument: oldCase.ErrorArgument, Reason: "no one-to-one full fingerprint match"}
				for _, candidate := range newInputGroups[oldCase.rebaseInputFingerprint] {
					unresolved.Candidates = append(unresolved.Candidates, rebaseCandidate{ID: candidate.ID, ErrorArgument: candidate.ErrorArgument})
				}
				report.UnresolvedCases = append(report.UnresolvedCases, unresolved)
				continue
			}
		}
		if owner, exists := usedTargets[target.ID]; exists {
			return report, fmt.Errorf("target %q is selected by both %q and %q", target.ID, owner, assertion.key)
		}
		usedTargets[target.ID] = assertion.key
		report.Mappings = append(report.Mappings, rebaseMapping{OldID: assertion.key, NewID: target.ID, Diagnostic: assertion.value, Method: method})
		changes = append(changes, assertionKeyChange{start: assertion.start, end: assertion.end, value: target.ID})
		switch method {
		case "unique":
			report.Unique++
		case "ordered":
			report.Ordered++
		case "reviewed":
			report.Reviewed++
		}
	}
	report.Unresolved = len(report.UnresolvedCases)
	for oldID := range reviewed {
		if !usedReviews[oldID] {
			return report, fmt.Errorf("review for %q was not used", oldID)
		}
	}
	if report.Unresolved != 0 {
		return report, fmt.Errorf("%d assertions require review", report.Unresolved)
	}
	if outputPath == "" {
		return report, nil
	}
	updated := append([]byte(nil), assertions.source...)
	sort.Slice(changes, func(i, j int) bool { return changes[i].start > changes[j].start })
	for _, change := range changes {
		replacement := []byte(strconv.Quote(change.value))
		updated = append(append(append([]byte(nil), updated[:change.start]...), replacement...), updated[change.end:]...)
	}
	formatted, err := format.Source(updated)
	if err != nil {
		return report, fmt.Errorf("format assertion preview: %w", err)
	}
	if err := os.WriteFile(outputPath, formatted, 0o644); err != nil {
		return report, fmt.Errorf("write -rebase-output: %w", err)
	}
	return report, nil
}

func readRebaseCorpus(path string) (parserCaseCorpus, error) {
	file, err := os.Open(path)
	if err != nil {
		return parserCaseCorpus{}, err
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return parserCaseCorpus{}, err
	}
	defer reader.Close()
	source, err := io.ReadAll(reader)
	if err != nil {
		return parserCaseCorpus{}, err
	}
	var corpus parserCaseCorpus
	if err := json.Unmarshal(source, &corpus); err != nil {
		return corpus, err
	}
	if corpus.SchemaVersion != 1 || corpus.Tag == "" || len(corpus.Commit) != 40 {
		return corpus, fmt.Errorf("unexpected provenance: schema %d, tag %q, commit %q", corpus.SchemaVersion, corpus.Tag, corpus.Commit)
	}
	if _, err := hex.DecodeString(corpus.Commit); err != nil {
		return corpus, fmt.Errorf("unexpected commit %q: %w", corpus.Commit, err)
	}
	return corpus, nil
}

func rebaseCases(corpus parserCaseCorpus) (map[string]rebaseCase, map[rebaseFullFingerprint][]rebaseCase, map[rebaseInputFingerprint][]rebaseCase, error) {
	cases := make(map[string]rebaseCase)
	recordIDs := make(map[string]struct{}, len(corpus.Records))
	groups := make(map[rebaseFullFingerprint][]rebaseCase)
	inputs := make(map[rebaseInputFingerprint][]rebaseCase)
	for _, record := range corpus.Records {
		if record.ID == "" || record.Path == "" || record.Helper == "" || record.Line < 1 || record.Offset < 0 {
			return nil, nil, nil, fmt.Errorf("invalid record identity %#v", record)
		}
		if _, exists := recordIDs[record.ID]; exists {
			return nil, nil, nil, fmt.Errorf("duplicate record ID %q", record.ID)
		}
		recordIDs[record.ID] = struct{}{}
		for _, variant := range record.Cases {
			if variant.Name == "" || variant.Context == "" || variant.Source == "" || variant.VimOutcome == "" || variant.Expectation == "" {
				return nil, nil, nil, fmt.Errorf("invalid parser case in %q", record.ID)
			}
			input := rebaseInputFingerprint{Path: record.Path, Helper: record.Helper, Name: variant.Name, Context: variant.Context, Source: variant.Source, VimOutcome: variant.VimOutcome, Expectation: variant.Expectation}
			value := rebaseCase{ID: record.ID + "/" + variant.Name, rebaseFullFingerprint: rebaseFullFingerprint{rebaseInputFingerprint: input, ErrorArgument: record.ErrorArgument}}
			if _, exists := cases[value.ID]; exists {
				return nil, nil, nil, fmt.Errorf("duplicate target ID %q", value.ID)
			}
			cases[value.ID] = value
			groups[value.rebaseFullFingerprint] = append(groups[value.rebaseFullFingerprint], value)
			inputs[value.rebaseInputFingerprint] = append(inputs[value.rebaseInputFingerprint], value)
		}
	}
	return cases, groups, inputs, nil
}

type rebaseInputFingerprint struct {
	Path        string
	Helper      string
	Name        string
	Context     string
	Source      string
	VimOutcome  string
	Expectation string
}

type rebaseFullFingerprint struct {
	rebaseInputFingerprint
	ErrorArgument string
}

type parsedAssertions struct {
	source  []byte
	entries []parsedAssertion
}

type parsedAssertion struct {
	key   string
	value string
	start int
	end   int
}

type assertionKeyChange struct {
	start int
	end   int
	value string
}

func readParserAssertions(path string) (parsedAssertions, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return parsedAssertions{}, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, parser.ParseComments)
	if err != nil {
		return parsedAssertions{}, fmt.Errorf("parse -rebase-assertions: %w", err)
	}
	var function *ast.FuncDecl
	for _, declaration := range file.Decls {
		candidate, ok := declaration.(*ast.FuncDecl)
		if ok && candidate.Name.Name == "officialParserExpectedFailures" {
			if function != nil {
				return parsedAssertions{}, fmt.Errorf("multiple officialParserExpectedFailures functions")
			}
			function = candidate
		}
	}
	if function == nil || function.Body == nil || len(function.Body.List) != 1 {
		return parsedAssertions{}, fmt.Errorf("officialParserExpectedFailures must contain one direct return")
	}
	result, ok := function.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(result.Results) != 1 {
		return parsedAssertions{}, fmt.Errorf("officialParserExpectedFailures must contain one direct return")
	}
	literal, ok := result.Results[0].(*ast.CompositeLit)
	if !ok {
		return parsedAssertions{}, fmt.Errorf("officialParserExpectedFailures must return a map literal")
	}
	mapType, ok := literal.Type.(*ast.MapType)
	if !ok || !isStringType(mapType.Key) || !isStringType(mapType.Value) {
		return parsedAssertions{}, fmt.Errorf("officialParserExpectedFailures must return map[string]string")
	}
	parsed := parsedAssertions{source: source}
	seen := make(map[string]struct{}, len(literal.Elts))
	for _, element := range literal.Elts {
		entry, ok := element.(*ast.KeyValueExpr)
		if !ok {
			return parsedAssertions{}, fmt.Errorf("officialParserExpectedFailures map has a non-key/value entry")
		}
		key, keyLiteral, err := stringLiteral(entry.Key)
		if err != nil {
			return parsedAssertions{}, fmt.Errorf("officialParserExpectedFailures map key: %w", err)
		}
		value, _, err := stringLiteral(entry.Value)
		if err != nil {
			return parsedAssertions{}, fmt.Errorf("officialParserExpectedFailures map value: %w", err)
		}
		if _, exists := seen[key]; exists {
			return parsedAssertions{}, fmt.Errorf("duplicate assertion key %q", key)
		}
		seen[key] = struct{}{}
		parsed.entries = append(parsed.entries, parsedAssertion{key: key, value: value, start: fset.Position(keyLiteral.Pos()).Offset, end: fset.Position(keyLiteral.End()).Offset})
	}
	return parsed, nil
}

func isStringType(expression ast.Expr) bool {
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == "string"
}

func stringLiteral(expression ast.Expr) (string, *ast.BasicLit, error) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", nil, fmt.Errorf("must be a string literal")
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", nil, err
	}
	return value, literal, nil
}

func parseRebaseReviews(values []string) (map[string]string, error) {
	reviews := make(map[string]string, len(values))
	for _, value := range values {
		oldID, newID, ok := strings.Cut(value, "=")
		if !ok || oldID == "" || newID == "" {
			return nil, fmt.Errorf("-rebase-review must be OLD_ID=NEW_ID")
		}
		if _, exists := reviews[oldID]; exists {
			return nil, fmt.Errorf("duplicate review for %q", oldID)
		}
		reviews[oldID] = newID
	}
	return reviews, nil
}
