// Option metadata generation.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/format"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/neoclide/vimls-go/tools/internal/vimhelp"
)

type option struct {
	Name                string
	ShortName           string
	Type                string
	Scope               string
	Flags               []string
	Variants            []optionVariant
	CompletionValues    []string
	Validation          optionValidation
	AvailableWhen       string
	RequiredFeatures    []string
	DefinitionSource    string
	DefinitionLine      int
	Documentation       string
	DocumentationSource string
}

type optionValidation struct {
	Kind            string
	Values          []string
	AllowEmpty      bool
	AllowDuplicates bool
	Separator       string
	ErrorCode       string
	HasMin          bool
	Min             int64
	MinErrorCode    string
	HasMax          bool
	Max             int64
	MaxErrorCode    string
	Callback        string
	Sources         []callbackSource
}

type callbackSource struct {
	Source string
	Line   int
}

type optionVariant struct {
	Condition      string
	Variable       string
	Indirect       string
	DidSetCallback string
	ExpandCallback string
	ViDefault      string
	VimDefault     string
}

type optionEntry struct {
	Text      string
	Condition string
	Line      int
}

type conditionalText struct {
	Condition string
	Text      string
}

var pvDefinitionPattern = regexp.MustCompile(`(?m)^[ \t]*#[ \t]*define[ \t]+(PV_[A-Z0-9_]+)[ \t]+([^\r\n]+)`)
var termPattern = regexp.MustCompile(`(?m)^[ \t]*p_term\("([^"]+)"\s*,\s*([A-Za-z0-9_]+)\s*\)`)
var cStringArrayPattern = regexp.MustCompile(`(?m)\bchar\s+\*\s*(?:\(\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\[\s*\]\s*(?:\))?\s*=\s*\{`)
var cStringMacroPattern = regexp.MustCompile(`(?m)^[ \t]*#[ \t]*define[ \t]+([A-Za-z_][A-Za-z0-9_]*)[ \t]+("(?:\\.|[^"])*")`)
var definedIdentifierPattern = regexp.MustCompile(`\bdefined\(([A-Za-z_][A-Za-z0-9_]*)\)`)
var positiveDefinedConjunctionPattern = regexp.MustCompile(`^defined\([A-Za-z_][A-Za-z0-9_]*\)(?:\s*&&\s*defined\([A-Za-z_][A-Za-z0-9_]*\))*$`)
var vimFeaturePattern = regexp.MustCompile(`(?m)^[ \t]*#[ \t]*(?:ifdef[ \t]+([A-Za-z_][A-Za-z0-9_]*)|if[ \t]+defined\(([A-Za-z_][A-Za-z0-9_]*)\))[ \t]*\r?\n[ \t]*"\+([^"]+)"`)

func generateOptions(root, output, oracleOutput string) error {
	source, err := readRevisionFile(root, "src/optiondefs.h")
	if err != nil {
		return err
	}
	options, err := parseOptionSource(source)
	if err != nil {
		return fmt.Errorf("parse optiondefs.h: %w", err)
	}
	if len(options) != 469 {
		return fmt.Errorf("found %d ordinary options, want 469", len(options))
	}
	terms, err := parseTerms(source)
	if err != nil {
		return fmt.Errorf("parse terminal options: %w", err)
	}
	if len(terms) != 93 {
		return fmt.Errorf("found %d p_term options, want 93", len(terms))
	}
	options = append(options, terms...)
	if err := addOptionRequiredFeatures(root, options); err != nil {
		return fmt.Errorf("read option build features: %w", err)
	}
	if err := addOptionCompletionValues(root, options); err != nil {
		return fmt.Errorf("read option completion values: %w", err)
	}
	if err := addOptionValidations(root, options); err != nil {
		return fmt.Errorf("read option validations: %w", err)
	}
	if err := validateOptions(options); err != nil {
		return fmt.Errorf("validate options: %w", err)
	}
	sort.Slice(options, func(i, j int) bool { return options[i].Name < options[j].Name })
	if err := addOptionDocumentation(root, options); err != nil {
		return fmt.Errorf("read option documentation: %w", err)
	}
	if err := writeOptions(output, options); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if err := writeOptionSetOracle(oracleOutput, options); err != nil {
		return fmt.Errorf("write option set oracle: %w", err)
	}
	return nil
}

func parseOptionSource(source []byte) ([]option, error) {
	entries, err := extractOptionEntries(source)
	if err != nil {
		return nil, err
	}
	pvDefinitions := make(map[string]string)
	for _, match := range pvDefinitionPattern.FindAllSubmatch(source, -1) {
		pvDefinitions[string(match[1])] = string(match[2])
	}
	result := make([]option, 0, len(entries))
	for _, entry := range entries {
		o, err := parseOptionEntry(entry, pvDefinitions)
		if err != nil {
			return nil, fmt.Errorf("optiondefs.h:%d: %w", entry.Line, err)
		}
		result = append(result, o)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func extractOptionEntries(source []byte) ([]optionEntry, error) {
	lines := joinCPreprocessorLines(strings.Split(string(source), "\n"))
	inTable := false
	inEntry := false
	entryLine := 0
	entryCondition := ""
	depth := 0
	var entry strings.Builder
	var conditions []conditionFrame
	var result []optionEntry
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inTable {
			if strings.Contains(line, "static struct vimoption options[]") {
				inTable = true
			}
			continue
		}
		if !inEntry {
			if kind, expression, ok := preprocessorDirective(trimmed); ok {
				if err := updateConditionFrames(&conditions, kind, expression); err != nil {
					return nil, fmt.Errorf("optiondefs.h:%d: %w", index+1, err)
				}
				continue
			}
			if strings.HasPrefix(trimmed, "// terminal output codes") {
				break
			}
			if strings.HasPrefix(trimmed, "{NULL,") {
				break
			}
			if !strings.HasPrefix(trimmed, "{\"") {
				continue
			}
			inEntry = true
			entryLine = index + 1
			entryCondition = activeCondition(conditions)
			entry.Reset()
			depth = 0
		}
		entry.WriteString(line)
		entry.WriteByte('\n')
		depth += braceDelta(line)
		if depth == 0 {
			result = append(result, optionEntry{Text: entry.String(), Condition: entryCondition, Line: entryLine})
			inEntry = false
		}
	}
	if inEntry {
		return nil, fmt.Errorf("optiondefs.h:%d: unterminated option initializer", entryLine)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("options table is empty")
	}
	return result, nil
}

type conditionFrame struct {
	expressions []string
	active      string
}

func preprocessorDirective(line string) (kind, expression string, ok bool) {
	if !strings.HasPrefix(line, "#") {
		return "", "", false
	}
	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "#")))
	if len(fields) == 0 {
		return "", "", false
	}
	kind = fields[0]
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "#")), kind))
	switch kind {
	case "ifdef":
		return "if", "defined(" + rest + ")", true
	case "ifndef":
		return "if", "!defined(" + rest + ")", true
	case "if", "elif":
		return kind, normalizeExpression(rest), true
	case "else", "endif":
		return kind, "", true
	default:
		return "", "", false
	}
}

func updateConditionFrames(frames *[]conditionFrame, kind, expression string) error {
	switch kind {
	case "if":
		*frames = append(*frames, conditionFrame{expressions: []string{expression}, active: expression})
	case "elif":
		if len(*frames) == 0 {
			return fmt.Errorf("#elif without #if")
		}
		frame := &(*frames)[len(*frames)-1]
		frame.active = conditionAnd(conditionNot(conditionOr(frame.expressions)), expression)
		frame.expressions = append(frame.expressions, expression)
	case "else":
		if len(*frames) == 0 {
			return fmt.Errorf("#else without #if")
		}
		frame := &(*frames)[len(*frames)-1]
		frame.active = conditionNot(conditionOr(frame.expressions))
	case "endif":
		if len(*frames) == 0 {
			return fmt.Errorf("#endif without #if")
		}
		*frames = (*frames)[:len(*frames)-1]
	}
	return nil
}

func activeCondition(frames []conditionFrame) string {
	condition := ""
	for _, frame := range frames {
		condition = conditionAnd(condition, frame.active)
	}
	return condition
}

func conditionAnd(left, right string) string {
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	return "(" + left + ") && (" + right + ")"
}

func conditionOr(expressions []string) string {
	if len(expressions) == 1 {
		return expressions[0]
	}
	return "(" + strings.Join(expressions, ") || (") + ")"
}

func conditionNot(expression string) string {
	return "!(" + expression + ")"
}

func normalizeExpression(expression string) string {
	return strings.Join(strings.Fields(expression), " ")
}

func braceDelta(line string) int {
	delta := 0
	inString := false
	escaped := false
	for i := 0; i < len(line); i++ {
		character := line[i]
		if inString {
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == '"' {
				inString = false
			}
			continue
		}
		if character == '"' {
			inString = true
			continue
		}
		if character == '/' && i+1 < len(line) && line[i+1] == '/' {
			break
		}
		switch character {
		case '{':
			delta++
		case '}':
			delta--
		}
	}
	return delta
}

func parseOptionEntry(entry optionEntry, definitions map[string]string) (option, error) {
	expanded, err := expandConditionalText(entry.Text)
	if err != nil {
		return option{}, err
	}
	var result option
	unconditionallyAvailable := entry.Condition == ""
	for _, form := range expanded {
		fields, err := optionInitializerFields(form.Text)
		if err != nil {
			return option{}, err
		}
		name, err := cString(fields[0])
		if err != nil {
			return option{}, fmt.Errorf("name: %w", err)
		}
		shortName, err := cStringOrNull(fields[1])
		if err != nil {
			return option{}, fmt.Errorf("%s short name: %w", name, err)
		}
		flags := splitFlags(fields[2])
		typ, err := optionType(strings.Join(flags, "|"))
		if err != nil {
			return option{}, fmt.Errorf("%s: %w", name, err)
		}
		defaults, err := initializerValues(strings.TrimSuffix(strings.TrimSpace(fields[7]), "SCTX_INIT"))
		if err != nil || len(defaults) != 2 {
			return option{}, fmt.Errorf("%s defaults: got %q: %v", name, fields[7], err)
		}
		if result.Name == "" {
			result = option{Name: name, ShortName: shortName, Type: typ, Flags: flags, DefinitionSource: "src/optiondefs.h", DefinitionLine: entry.Line}
		} else if result.Name != name || result.ShortName != shortName || result.Type != typ || strings.Join(result.Flags, "|") != strings.Join(flags, "|") {
			return option{}, fmt.Errorf("conditional forms disagree on declaration for %s", name)
		}
		variable := normalizeCExpression(fields[3])
		if isNullExpression(variable) {
			unconditionallyAvailable = false
		}
		variant := optionVariant{
			Condition:      conditionAnd(entry.Condition, form.Condition),
			Variable:       variable,
			Indirect:       normalizeCExpression(fields[4]),
			DidSetCallback: normalizeCExpression(fields[5]),
			ExpandCallback: normalizeCExpression(fields[6]),
			ViDefault:      normalizeCExpression(defaults[0]),
			VimDefault:     normalizeCExpression(defaults[1]),
		}
		result.Variants = append(result.Variants, variant)
	}
	result.AvailableWhen = optionAvailability(result.Variants)
	if unconditionallyAvailable {
		result.AvailableWhen = "1"
	}
	scope, err := optionVariantScope(result.Variants, definitions)
	if err != nil {
		return option{}, fmt.Errorf("%s: %w", result.Name, err)
	}
	result.Scope = scope
	return result, nil
}

func expandConditionalText(source string) ([]conditionalText, error) {
	lines := joinCPreprocessorLines(strings.Split(source, "\n"))
	position := 0
	result, stop, _, err := expandConditionalSequence(lines, &position)
	if err != nil {
		return nil, err
	}
	if stop != "" {
		return nil, fmt.Errorf("unexpected #%s", stop)
	}
	return result, nil
}

func joinCPreprocessorLines(lines []string) []string {
	lines = append([]string(nil), lines...)
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			continue
		}
		for next := i + 1; strings.HasSuffix(strings.TrimSpace(lines[i]), "\\") && next < len(lines); next++ {
			lines[i] = strings.TrimSuffix(strings.TrimSpace(lines[i]), "\\") + " " + strings.TrimSpace(lines[next])
			lines[next] = ""
		}
	}
	return lines
}

func expandConditionalSequence(lines []string, position *int) ([]conditionalText, string, string, error) {
	result := []conditionalText{{}}
	for *position < len(lines) {
		line := lines[*position]
		kind, expression, directive := preprocessorDirective(strings.TrimSpace(line))
		if directive && (kind == "elif" || kind == "else" || kind == "endif") {
			return result, kind, expression, nil
		}
		if directive && kind == "if" {
			*position++
			var branches []conditionalText
			expressions := []string{expression}
			branchCondition := expression
			for {
				body, stop, nextExpression, err := expandConditionalSequence(lines, position)
				if err != nil {
					return nil, "", "", err
				}
				for _, form := range body {
					form.Condition = conditionAnd(branchCondition, form.Condition)
					branches = append(branches, form)
				}
				if stop == "" {
					return nil, "", "", fmt.Errorf("unterminated #if")
				}
				*position++
				if stop == "endif" {
					break
				}
				if stop == "elif" {
					branchCondition = conditionAnd(conditionNot(conditionOr(expressions)), nextExpression)
					expressions = append(expressions, nextExpression)
				} else {
					branchCondition = conditionNot(conditionOr(expressions))
				}
			}
			result = combineConditionalText(result, branches)
			continue
		}
		if directive {
			return nil, "", "", fmt.Errorf("unsupported preprocessor directive in initializer: %s", line)
		}
		for i := range result {
			result[i].Text += line + "\n"
		}
		*position++
	}
	return result, "", "", nil
}

func combineConditionalText(prefixes, suffixes []conditionalText) []conditionalText {
	result := make([]conditionalText, 0, len(prefixes)*len(suffixes))
	for _, prefix := range prefixes {
		for _, suffix := range suffixes {
			result = append(result, conditionalText{Condition: conditionAnd(prefix.Condition, suffix.Condition), Text: prefix.Text + suffix.Text})
		}
	}
	return result
}

func optionInitializerFields(source string) ([]string, error) {
	source = strings.TrimSpace(stripCComments(source))
	if len(source) < 2 || source[0] != '{' {
		return nil, fmt.Errorf("invalid option initializer %q", source)
	}
	end := matchingBrace(source, 0)
	if end < 0 {
		return nil, fmt.Errorf("unterminated option initializer")
	}
	fields := splitTopLevel(source[1:end], ',')
	if len(fields) != 8 {
		return nil, fmt.Errorf("option initializer has %d fields, want 8: %q", len(fields), source)
	}
	return fields, nil
}

func initializerValues(source string) ([]string, error) {
	source = strings.TrimSpace(source)
	if len(source) < 2 || source[0] != '{' {
		return nil, fmt.Errorf("invalid nested initializer %q", source)
	}
	end := matchingBrace(source, 0)
	if end < 0 || strings.TrimSpace(source[end+1:]) != "" {
		return nil, fmt.Errorf("invalid nested initializer %q", source)
	}
	return splitTopLevel(source[1:end], ','), nil
}

func splitTopLevel(source string, separator byte) []string {
	var result []string
	start := 0
	paren, brace, bracket := 0, 0, 0
	inString, escaped := false, false
	for i := 0; i < len(source); i++ {
		character := source[i]
		if inString {
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == '"' {
				inString = false
			}
			continue
		}
		switch character {
		case '"':
			inString = true
		case '(':
			paren++
		case ')':
			paren--
		case '{':
			brace++
		case '}':
			brace--
		case '[':
			bracket++
		case ']':
			bracket--
		default:
			if character == separator && paren == 0 && brace == 0 && bracket == 0 {
				result = append(result, strings.TrimSpace(source[start:i]))
				start = i + 1
			}
		}
	}
	result = append(result, strings.TrimSpace(source[start:]))
	return result
}

func matchingBrace(source string, start int) int {
	depth := 0
	inString, escaped := false, false
	for i := start; i < len(source); i++ {
		character := source[i]
		if inString {
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == '"' {
				inString = false
			}
			continue
		}
		switch character {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func stripCComments(source string) string {
	var result strings.Builder
	inString, escaped, lineComment, blockComment := false, false, false, false
	for i := 0; i < len(source); i++ {
		character := source[i]
		if lineComment {
			if character == '\n' {
				lineComment = false
				result.WriteByte(character)
			}
			continue
		}
		if blockComment {
			if character == '*' && i+1 < len(source) && source[i+1] == '/' {
				blockComment = false
				i++
			}
			continue
		}
		if inString {
			result.WriteByte(character)
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == '"' {
				inString = false
			}
			continue
		}
		if character == '"' {
			inString = true
			result.WriteByte(character)
		} else if character == '/' && i+1 < len(source) && source[i+1] == '/' {
			lineComment = true
			i++
		} else if character == '/' && i+1 < len(source) && source[i+1] == '*' {
			blockComment = true
			i++
		} else {
			result.WriteByte(character)
		}
	}
	return result.String()
}

func cString(source string) (string, error) {
	source = strings.TrimSpace(source)
	if len(source) < 2 || source[0] != '"' || source[len(source)-1] != '"' {
		return "", fmt.Errorf("expected C string, got %q", source)
	}
	return source[1 : len(source)-1], nil
}

func cStringOrNull(source string) (string, error) {
	if strings.TrimSpace(source) == "NULL" {
		return "", nil
	}
	return cString(source)
}

func splitFlags(source string) []string {
	parts := strings.Split(source, "|")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		result = append(result, strings.TrimSpace(part))
	}
	return result
}

func normalizeCExpression(source string) string {
	return strings.Join(strings.Fields(source), " ")
}

func isNullExpression(source string) bool {
	return strings.ReplaceAll(source, " ", "") == "(char_u*)NULL" || strings.TrimSpace(source) == "NULL"
}

func optionAvailability(variants []optionVariant) string {
	var conditions []string
	for _, variant := range variants {
		if isNullExpression(variant.Variable) {
			continue
		}
		if variant.Condition == "" {
			return "1"
		}
		conditions = append(conditions, variant.Condition)
	}
	if len(conditions) == 0 {
		return "0"
	}
	return simplifyConditions(conditions)
}

func simplifyConditions(conditions []string) string {
	if len(conditions) <= 1 {
		if len(conditions) == 1 {
			return stripOuterParens(conditions[0])
		}
		return "0"
	}
	if len(conditions) == 2 {
		firstLeft, firstRight, firstOK := splitConditionAnd(conditions[0])
		secondLeft, secondRight, secondOK := splitConditionAnd(conditions[1])
		if firstOK && secondOK && firstLeft == secondLeft && complementaryConditions(firstRight, secondRight) {
			return stripOuterParens(firstLeft)
		}
	}
	return conditionOr(conditions)
}

func splitConditionAnd(s string) (string, string, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "(") {
		return "", "", false
	}
	closeIndex := matchingCDelimiter(s, 0, '(', ')')
	if closeIndex < 0 || closeIndex+len(" && (") >= len(s) {
		return "", "", false
	}
	if !strings.HasPrefix(s[closeIndex+1:], " && (") || !strings.HasSuffix(s, ")") {
		return "", "", false
	}
	rightStart := closeIndex + 1 + len(" && ")
	if matchingCDelimiter(s, rightStart, '(', ')') != len(s)-1 {
		return "", "", false
	}
	left := s[1:closeIndex]
	right := s[rightStart+1 : len(s)-1]
	return left, right, true
}

func stripOuterParens(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") && matchingCDelimiter(s, 0, '(', ')') == len(s)-1 {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

func complementaryConditions(left, right string) bool {
	left = stripOuterParens(left)
	right = stripOuterParens(right)
	if negated, ok := negatedCondition(left); ok && negated == right {
		return true
	}
	negated, ok := negatedCondition(right)
	return ok && negated == left
}

func negatedCondition(condition string) (string, bool) {
	condition = strings.TrimSpace(condition)
	if !strings.HasPrefix(condition, "!(") || matchingCDelimiter(condition, 1, '(', ')') != len(condition)-1 {
		return "", false
	}
	return stripOuterParens(condition[1:]), true
}

func optionType(flags string) (string, error) {
	result := ""
	for f := range strings.SplitSeq(flags, "|") {
		switch strings.TrimSpace(f) {
		case "P_BOOL":
			result = "OptionBool"
		case "P_NUM":
			result = "OptionNumber"
		case "P_STRING":
			result = "OptionString"
		}
	}
	if result == "" {
		return "", fmt.Errorf("missing option type in %s", flags)
	}
	return result, nil
}

func optionVariantScope(variants []optionVariant, definitions map[string]string) (string, error) {
	scopes := make(map[string]bool)
	for _, variant := range variants {
		if isNullExpression(variant.Variable) {
			continue
		}
		name := variant.Indirect
		if name == "PV_NONE" {
			scopes["OptionGlobal"] = true
			continue
		}
		body, ok := definitions[name]
		if !ok {
			return "", fmt.Errorf("scope macro %s is not defined", name)
		}
		scope := ""
		switch {
		case strings.Contains(body, "OPT_BOTH"):
			scope = "OptionGlobalLocal"
		case strings.Contains(body, "OPT_WIN"):
			scope = "OptionWindow"
		case strings.Contains(body, "OPT_BUF"):
			scope = "OptionBuffer"
		default:
			return "", fmt.Errorf("scope macro %s has unsupported definition %q", name, body)
		}
		scopes[scope] = true
	}
	if len(scopes) == 0 || (len(scopes) == 2 && scopes["OptionGlobal"] && (scopes["OptionBuffer"] || scopes["OptionWindow"] || scopes["OptionGlobalLocal"])) {
		delete(scopes, "OptionGlobal")
	}
	if len(scopes) == 0 {
		return "OptionGlobal", nil
	}
	if len(scopes) != 1 {
		return "", fmt.Errorf("conflicting scopes %v", scopes)
	}
	for scope := range scopes {
		return scope, nil
	}
	panic("unreachable")
}

func parseTerms(source []byte) ([]option, error) {
	matches := termPattern.FindAllStringSubmatchIndex(string(source), -1)
	result := make([]option, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		name := string(source[m[2]:m[3]])
		if seen[name] {
			return nil, fmt.Errorf("duplicate terminal option %s", name)
		}
		seen[name] = true
		line := bytes.Count(source[:m[0]], []byte{'\n'}) + 1
		result = append(result, option{
			Name: name, Type: "OptionString", Scope: "OptionGlobal",
			Flags:            []string{"P_STRING", "P_VI_DEF", "P_RALL", "P_SECURE"},
			Variants:         []optionVariant{{Variable: "(char_u *)&" + string(source[m[4]:m[5]]), Indirect: "PV_NONE", DidSetCallback: "did_set_term_option", ExpandCallback: "NULL", ViDefault: `(char_u *)""`, VimDefault: "(char_u *)0L"}},
			AvailableWhen:    "1",
			DefinitionSource: "src/optiondefs.h", DefinitionLine: line,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func addOptionCompletionValues(root string, options []option) error {
	optionstr, err := readRevisionFile(root, "src/optionstr.c")
	if err != nil {
		return err
	}
	optionHeader, err := readRevisionFile(root, "src/option.h")
	if err != nil {
		return err
	}
	macros, err := parseCStringMacros(optionHeader)
	if err != nil {
		return err
	}
	arrays, err := parseCStringArrays(optionstr, macros)
	if err != nil {
		return err
	}
	callbackValues := make(map[string][]string)
	for i := range options {
		var values []string
		for _, variant := range options[i].Variants {
			callback := variant.ExpandCallback
			if callback == "NULL" {
				continue
			}
			fixed, ok := callbackValues[callback]
			if !ok {
				fixed, err = parseExpandCallbackValues(optionstr, callback, arrays, macros)
				if err != nil {
					return fmt.Errorf("%s: %w", callback, err)
				}
				callbackValues[callback] = fixed
			}
			values = appendUnique(values, fixed...)
		}
		options[i].CompletionValues = values
	}
	return nil
}

type callbackDefinition struct {
	Source string
	Line   int
	Body   string
}

type structuredOptionEvidence struct {
	chars              string
	encodedChar        string
	statusline         string
	winhighlight       string
	winhighlightParser string
}

const (
	didSetCharsOptionFingerprint    = "d803c055cb29b64268999f922cbe83402792a16ce9f2334aa0248065888f3898"
	setCharsOptionFingerprint       = "2f5f8f373532734e583f2155e3d3f472379a403ead1251a108fb351b166e794f"
	getEncodedCharAdvFingerprint    = "4cede8f66c769e544ad345600437028902ee708374bb43c9bcebc0561d9d43e5"
	didSetStatuslineoptFingerprint  = "32a53f2f5a7d41b20620112e75fbf0ab1aeb0099ab5c8ef7002eb9b6d58df061"
	statuslineoptChangedFingerprint = "52cf056bc4f53c056d2a2ffe81114118914d556d31df4bedd2539cda5bc0d6c2"
	didSetWinhighlightFingerprint   = "8325095ec6fdf4b9ac67c3deb95248c923e3ed6b57a793154fd69c4589e6d57d"
	updateWinhighlightFingerprint   = "d6aac8fd8304824f20081e6a92626539c16e1523a25945aa50cf0fd895d9646e"
	parseWinhighlightFingerprint    = "be6806d1b985de21ff08122a3bd7f4ff407fd9b3ac2db4ca710848efe7de720a"
)

func addOptionValidations(root string, options []option) error {
	optionHeader, err := readRevisionFile(root, "src/option.h")
	if err != nil {
		return err
	}
	macros, err := parseCStringMacros(optionHeader)
	if err != nil {
		return err
	}
	optionstr, err := readRevisionFile(root, "src/optionstr.c")
	if err != nil {
		return err
	}
	arrays, err := parseCStringArrays(optionstr, macros)
	if err != nil {
		return err
	}
	screen, err := readRevisionFile(root, "src/screen.c")
	if err != nil {
		return err
	}
	chars, ok := cFunctionBody(string(screen), "set_chars_option")
	if !ok {
		return fmt.Errorf("set_chars_option was not found")
	}
	encodedChar, ok := cFunctionBody(string(screen), "get_encoded_char_adv")
	if !ok {
		return fmt.Errorf("get_encoded_char_adv was not found")
	}
	window, err := readRevisionFile(root, "src/window.c")
	if err != nil {
		return err
	}
	statusline, ok := cFunctionBody(string(window), "statuslineopt_changed")
	if !ok {
		return fmt.Errorf("statuslineopt_changed was not found")
	}
	highlight, err := readRevisionFile(root, "src/highlight.c")
	if err != nil {
		return err
	}
	winhighlight, ok := cFunctionBody(string(highlight), "update_winhighlight")
	if !ok {
		return fmt.Errorf("update_winhighlight was not found")
	}
	winhighlightParser, ok := cFunctionBody(string(highlight), "parse_winhighlight")
	if !ok {
		return fmt.Errorf("parse_winhighlight was not found")
	}
	structured := structuredOptionEvidence{chars: chars, encodedChar: encodedChar, statusline: statusline, winhighlight: winhighlight, winhighlightParser: winhighlightParser}
	listChars, err := parseCharsOptionNames(screen, "lcstab")
	if err != nil {
		return err
	}
	fillChars, err := parseCharsOptionNames(screen, "filltab")
	if err != nil {
		return err
	}
	statuslineOpt, ok := arrays["p_stlo_values"]
	if !ok || len(statuslineOpt) == 0 {
		return fmt.Errorf("p_stlo_values not found")
	}
	errorsSource, err := readRevisionFile(root, "src/errors.h")
	if err != nil {
		return err
	}
	errorCodes := parseVimErrorCodes(errorsSource)
	vimHeader, err := readRevisionFile(root, "src/vim.h")
	if err != nil {
		return err
	}
	numericConstants := parseCNumericMacros(vimHeader)

	definitions := make(map[string][]callbackDefinition)
	callbackSources, err := revisionFilesMatching(root, `^(did_set_[A-Za-z0-9_]+|parse_print(options|mbfont))\(`, "src/*.c")
	if err != nil {
		return err
	}
	for _, path := range callbackSources {
		source, err := readRevisionFile(root, path)
		if err != nil {
			return err
		}
		text := string(source)
		for _, option := range options {
			for _, variant := range option.Variants {
				callback := variant.DidSetCallback
				if callback == "NULL" {
					continue
				}
				body, definitionStart, ok := cFunctionBodyAt(text, callback)
				if !ok {
					continue
				}
				definition := callbackDefinition{Source: path, Line: 1 + strings.Count(text[:definitionStart], "\n"), Body: body}
				if slices.Contains(definitions[callback], definition) {
					continue
				}
				definitions[callback] = append(definitions[callback], definition)
			}
		}
	}

	for i := range options {
		callback := ""
		for _, variant := range options[i].Variants {
			if variant.DidSetCallback == "NULL" {
				continue
			}
			if callback != "" && callback != variant.DidSetCallback {
				return fmt.Errorf("%s has multiple validation callbacks: %s and %s", options[i].Name, callback, variant.DidSetCallback)
			}
			callback = variant.DidSetCallback
		}
		if callback == "" {
			continue
		}
		callbackDefinitions := definitions[callback]
		if len(callbackDefinitions) == 0 {
			return fmt.Errorf("callback %s for %s was not found in pinned Vim sources", callback, options[i].Name)
		}
		validation, err := parseOptionValidation(callback, callbackDefinitions, arrays, macros, optionVariables(options[i]), errorCodes, numericConstants, options[i].Type)
		if err != nil {
			return fmt.Errorf("%s for %s: %w", callback, options[i].Name, err)
		}
		validation, err = parseStructuredOptionValidation(options[i].Name, validation, callbackDefinitions, listChars, fillChars, statuslineOpt, errorCodes, structured)
		if err != nil {
			return fmt.Errorf("%s for %s: %w", callback, options[i].Name, err)
		}
		options[i].Validation = validation
	}
	return nil
}

func parseCharsOptionNames(source []byte, table string) ([]string, error) {
	tablePattern := regexp.MustCompile(`(?s)static struct charstab\s+` + regexp.QuoteMeta(table) + `\[\]\s*=\s*\{(.*?)\n\};`)
	match := tablePattern.FindSubmatch(source)
	if len(match) != 2 {
		return nil, fmt.Errorf("%s table not found", table)
	}
	entryPattern := regexp.MustCompile(`CHARSTAB_ENTRY\([^,]+,\s*"([^"]+)"\)`)
	var names []string
	for _, entry := range entryPattern.FindAllSubmatch(match[1], -1) {
		names = appendUnique(names, string(entry[1]))
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%s has no CHARSTAB_ENTRY values", table)
	}
	return names, nil
}

func parseStructuredOptionValidation(name string, validation optionValidation, definitions []callbackDefinition, listChars, fillChars, statuslineOpt []string, errorCodes map[string]string, evidence structuredOptionEvidence) (optionValidation, error) {
	switch name {
	case "listchars":
		if len(definitions) != 1 || definitions[0].Source != "src/optionstr.c" || validation.Callback != "did_set_chars_option" || bodyFingerprint(definitions[0].Body) != didSetCharsOptionFingerprint {
			return validation, fmt.Errorf("unrecognized listchars callback body")
		}
		if errorCodes["e_invalid_argument"] != "E474" || errorCodes["e_wrong_number_of_characters_for_field_str"] != "E1511" || errorCodes["e_leadtab_requires_tab"] != "E1572" || !structuredOptionEvidenceMatchesPin(evidence) {
			return validation, fmt.Errorf("unrecognized listchars helper semantics")
		}
		validation.Kind, validation.Values, validation.ErrorCode = "ValidationListChars", listChars, errorCodes["e_invalid_argument"]
	case "fillchars":
		if len(definitions) != 1 || definitions[0].Source != "src/optionstr.c" || validation.Callback != "did_set_chars_option" || bodyFingerprint(definitions[0].Body) != didSetCharsOptionFingerprint {
			return validation, fmt.Errorf("unrecognized fillchars callback body")
		}
		if errorCodes["e_invalid_argument"] != "E474" || errorCodes["e_wrong_number_of_characters_for_field_str"] != "E1511" || errorCodes["e_leadtab_requires_tab"] != "E1572" || !structuredOptionEvidenceMatchesPin(evidence) {
			return validation, fmt.Errorf("unrecognized fillchars helper semantics")
		}
		validation.Kind, validation.Values, validation.ErrorCode = "ValidationFillChars", fillChars, errorCodes["e_invalid_argument"]
	case "statuslineopt":
		if len(definitions) != 1 || definitions[0].Source != "src/optionstr.c" || validation.Callback != "did_set_statuslineopt" || bodyFingerprint(definitions[0].Body) != didSetStatuslineoptFingerprint {
			return validation, fmt.Errorf("unrecognized statuslineopt callback body")
		}
		if errorCodes["e_invalid_argument"] != "E474" || !slices.Equal(statuslineOpt, []string{"fixedheight", "maxheight:"}) || !structuredOptionEvidenceMatchesPin(evidence) {
			return validation, fmt.Errorf("unrecognized statuslineopt helper semantics")
		}
		validation.Kind, validation.Values, validation.ErrorCode = "ValidationStatuslineOpt", statuslineOpt, errorCodes["e_invalid_argument"]
	case "winhighlight":
		if len(definitions) != 1 || definitions[0].Source != "src/optionstr.c" || validation.Callback != "did_set_winhighlight" || bodyFingerprint(definitions[0].Body) != didSetWinhighlightFingerprint {
			return validation, fmt.Errorf("unrecognized winhighlight callback body")
		}
		if errorCodes["e_invalid_argument"] != "E474" || !structuredOptionEvidenceMatchesPin(evidence) {
			return validation, fmt.Errorf("unrecognized winhighlight helper semantics")
		}
		// Highlight groups and ! occasions depend on runtime state; only the
		// delimiter structure above is statically diagnosed.
		validation.Kind, validation.ErrorCode = "ValidationWinHighlight", errorCodes["e_invalid_argument"]
	}
	return validation, nil
}

func bodyFingerprint(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func structuredOptionEvidenceMatchesPin(evidence structuredOptionEvidence) bool {
	return bodyMatchesFingerprint(evidence.chars, setCharsOptionFingerprint) &&
		bodyMatchesFingerprint(evidence.encodedChar, getEncodedCharAdvFingerprint) &&
		bodyMatchesFingerprint(evidence.statusline, statuslineoptChangedFingerprint) &&
		bodyMatchesFingerprint(evidence.winhighlight, updateWinhighlightFingerprint) &&
		bodyMatchesFingerprint(evidence.winhighlightParser, parseWinhighlightFingerprint)
}

func bodyMatchesFingerprint(body, fingerprint string) bool {
	return bodyFingerprint(body) == fingerprint
}

func parseOptionValidation(callback string, definitions []callbackDefinition, arrays map[string][]string, macros map[string]string, variables []string, errorCodes map[string]string, numericConstants map[string]int64, optionType string) (optionValidation, error) {
	validation := optionValidation{
		Kind:     "ValidationNone",
		Callback: callback,
	}
	for _, definition := range definitions {
		validation.Sources = append(validation.Sources, callbackSource{Source: definition.Source, Line: definition.Line})
	}
	if optionType == "OptionBool" {
		return validation, nil
	}
	var common *optionValidation
	for _, definition := range definitions {
		derived := optionValidation{Kind: "ValidationNone"}
		var err error
		if optionType == "" || optionType == "OptionString" {
			derived, err = parseOptionValidationBody(definition.Body, arrays, macros, variables)
			if err != nil {
				return validation, err
			}
		}
		if (optionType == "" || optionType == "OptionNumber") && derived.Kind == "ValidationNone" {
			derived, err = parseNumberValidationBody(definition.Body, variables, errorCodes, numericConstants)
			if err != nil {
				return validation, err
			}
		}
		if derived.Kind == "ValidationNone" {
			return validation, nil
		}
		if common == nil {
			copy := derived
			common = &copy
			continue
		}
		if common.Kind != derived.Kind || !slices.Equal(common.Values, derived.Values) || common.AllowEmpty != derived.AllowEmpty || common.AllowDuplicates != derived.AllowDuplicates || common.Separator != derived.Separator || common.ErrorCode != derived.ErrorCode || common.HasMin != derived.HasMin || common.Min != derived.Min || common.MinErrorCode != derived.MinErrorCode || common.HasMax != derived.HasMax || common.Max != derived.Max || common.MaxErrorCode != derived.MaxErrorCode {
			return validation, nil
		}
	}
	if common != nil {
		common.Callback = callback
		common.Sources = validation.Sources
		return *common, nil
	}
	return validation, nil
}

func parseOptionValidationBody(body string, arrays map[string][]string, macros map[string]string, variables []string) (optionValidation, error) {
	validation := optionValidation{Kind: "ValidationNone"}
	for _, helper := range []string{"did_set_opt_strings", "did_set_opt_flags", "did_set_option_listflag"} {
		for _, call := range findCCalls(body, helper) {
			if !isDirectReturnCall(body, call.position) {
				continue
			}
			if len(call.args) > 0 && !cOptionVariableMatches(call.args[0], variables) {
				continue
			}
			switch helper {
			case "did_set_opt_strings", "did_set_opt_flags":
				listIndex := 2
				if helper == "did_set_opt_flags" {
					listIndex = 3
				}
				if len(call.args) <= listIndex {
					return validation, fmt.Errorf("incomplete %s call", helper)
				}
				arrayName := cExpressionIdentifier(call.args[1])
				values, ok := arrays[arrayName]
				if arrayName == "" || !ok {
					return validation, fmt.Errorf("unknown values array %q", strings.TrimSpace(call.args[1]))
				}
				list, ok := cBoolean(call.args[listIndex])
				if !ok {
					return validation, fmt.Errorf("non-static list argument %q", strings.TrimSpace(call.args[listIndex]))
				}
				validation.Kind = "ValidationExact"
				validation.Values = append([]string(nil), values...)
				validation.AllowEmpty = true
				validation.ErrorCode = "E474"
				if list {
					validation.Kind = "ValidationCommaList"
					validation.AllowDuplicates = true
					validation.Separator = ","
				}
				return validation, nil
			case "did_set_option_listflag":
				if len(call.args) < 2 {
					return validation, fmt.Errorf("incomplete %s call", helper)
				}
				flags := cStaticStringExpression(call.args[1], macros)
				if flags == "" {
					return validation, fmt.Errorf("non-static flag expression %q", strings.TrimSpace(call.args[1]))
				}
				validation.Kind = "ValidationFlagList"
				validation.Values = splitRunes(flags)
				validation.AllowEmpty = true
				validation.AllowDuplicates = true
				validation.ErrorCode = "E539"
				return validation, nil
			}
		}
	}
	for _, call := range findCCalls(body, "check_opt_strings") {
		if !isUnconditionalInvalidArgumentGuard(body, call) {
			continue
		}
		if len(call.args) < 3 {
			return validation, fmt.Errorf("incomplete check_opt_strings call")
		}
		if !cOptionVariableMatches(call.args[0], variables) {
			continue
		}
		arrayName := cExpressionIdentifier(call.args[1])
		values, ok := arrays[arrayName]
		if arrayName == "" || !ok {
			return validation, fmt.Errorf("unknown values array %q", strings.TrimSpace(call.args[1]))
		}
		list, ok := cBoolean(call.args[2])
		if !ok {
			return validation, fmt.Errorf("non-static list argument %q", strings.TrimSpace(call.args[2]))
		}
		validation.Kind = "ValidationExact"
		validation.Values = append([]string(nil), values...)
		validation.AllowEmpty = true
		validation.ErrorCode = "E474"
		if list {
			validation.Kind = "ValidationCommaList"
			validation.AllowDuplicates = true
			validation.Separator = ","
		}
		return validation, nil
	}
	return validation, nil
}

func cOptionVariableMatches(arg string, variables []string) bool {
	if len(variables) == 0 {
		return true
	}
	arg = strings.TrimSpace(arg)
	ident := cExpressionIdentifier(arg)
	if strings.HasPrefix(ident, "p_") {
		return slices.Contains(variables, ident)
	}
	return true
}

func isUnconditionalInvalidArgumentGuard(body string, call cCall) bool {
	lineStart := strings.LastIndexByte(body[:call.position], '\n') + 1
	prefix := strings.TrimSpace(body[lineStart:call.position])
	if prefix != "if (" && prefix != "if(" {
		return false
	}
	closing := matchingCDelimiter(body, call.position+len("check_opt_strings"), '(', ')')
	if closing < 0 {
		return false
	}
	suffix := body[closing+1:]
	pattern := regexp.MustCompile(`^\s*(?:!=\s*OK|==\s*FAIL)\s*\)\s*(?:\{\s*)?return\s+e_invalid_argument\s*;`)
	return pattern.MatchString(suffix)
}

func isDirectReturnCall(body string, position int) bool {
	prefix := strings.TrimSpace(body[:position])
	return strings.HasSuffix(prefix, "return") && (len(prefix) == len("return") || !isCIdentifierByte(prefix[len(prefix)-len("return")-1]))
}

func isCIdentifierByte(character byte) bool {
	return character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
}

func cBoolean(expression string) (bool, bool) {
	switch strings.TrimSpace(expression) {
	case "TRUE":
		return true, true
	case "FALSE":
		return false, true
	default:
		return false, false
	}
}

func cStaticStringExpression(expression string, macros map[string]string) string {
	tokenPattern := regexp.MustCompile(`"(?:\\.|[^"])*"|[A-Za-z_][A-Za-z0-9_]*`)
	var result strings.Builder
	for _, token := range tokenPattern.FindAllString(expression, -1) {
		if value, ok := macros[token]; ok {
			result.WriteString(value)
			continue
		}
		if token[0] != '"' {
			continue
		}
		value, err := strconv.Unquote(token)
		if err != nil {
			return ""
		}
		result.WriteString(value)
	}
	return result.String()
}

func splitRunes(value string) []string {
	result := make([]string, 0, len(value))
	for _, character := range value {
		result = append(result, string(character))
	}
	return result
}

func optionVariables(option option) []string {
	pattern := regexp.MustCompile(`&([A-Za-z_][A-Za-z0-9_]*)`)
	var result []string
	for _, variant := range option.Variants {
		match := pattern.FindStringSubmatch(variant.Variable)
		if len(match) == 2 {
			result = appendUnique(result, match[1])
		}
	}
	return result
}

func parseVimErrorCodes(source []byte) map[string]string {
	pattern := regexp.MustCompile(`(?s)EXTERN\s+char\s+(e_[A-Za-z0-9_]+)\[\]\s*\n?\s*INIT\(=.*?"(E[0-9]+):`)
	result := make(map[string]string)
	for _, match := range pattern.FindAllSubmatch(source, -1) {
		result[string(match[1])] = string(match[2])
	}
	return result
}

type cIfStatement struct {
	condition string
	body      string
}

func parseNumberValidationBody(body string, variables []string, errorCodes map[string]string, numericConstants map[string]int64) (optionValidation, error) {
	validation := optionValidation{Kind: "ValidationNone"}
	if len(variables) == 0 {
		return validation, nil
	}
	defines := make(map[string]int64, len(numericConstants))
	maps.Copy(defines, numericConstants)
	definePattern := regexp.MustCompile(`(?m)^\s*#\s*define\s+([A-Za-z_][A-Za-z0-9_]*)\s+(-?[0-9]+)\s*$`)
	for _, match := range definePattern.FindAllStringSubmatch(body, -1) {
		value, err := strconv.ParseInt(match[2], 10, 64)
		if err != nil {
			return validation, err
		}
		defines[match[1]] = value
	}
	comparisonPattern := regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*(<=|>=|<|>)\s*([A-Za-z_][A-Za-z0-9_]*|-?[0-9]+)\s*$`)
	errorPattern := regexp.MustCompile(`(?:errmsg\s*=\s*|return\s+)(e_[A-Za-z0-9_]+)\s*;`)
	for _, statement := range findCIfStatements(body) {
		comparison := comparisonPattern.FindStringSubmatch(statement.condition)
		if len(comparison) != 4 || !slices.Contains(variables, comparison[1]) {
			continue
		}
		errorMatch := errorPattern.FindStringSubmatch(statement.body)
		if len(errorMatch) != 2 {
			continue
		}
		code := errorCodes[errorMatch[1]]
		if code == "" {
			return validation, fmt.Errorf("error code for %s not found", errorMatch[1])
		}
		bound, err := strconv.ParseInt(comparison[3], 10, 64)
		if err != nil {
			var ok bool
			bound, ok = defines[comparison[3]]
			if !ok {
				continue
			}
		}
		switch comparison[2] {
		case "<":
			validation.HasMin, validation.Min, validation.MinErrorCode = true, bound, code
		case "<=":
			validation.HasMin, validation.Min, validation.MinErrorCode = true, bound+1, code
		case ">":
			validation.HasMax, validation.Max, validation.MaxErrorCode = true, bound, code
		case ">=":
			validation.HasMax, validation.Max, validation.MaxErrorCode = true, bound-1, code
		}
	}
	if validation.HasMin || validation.HasMax {
		validation.Kind = "ValidationNumberRange"
	}
	return validation, nil
}

func parseCNumericMacros(source []byte) map[string]int64 {
	pattern := regexp.MustCompile(`(?m)^\s*#\s*define\s+([A-Za-z_][A-Za-z0-9_]*)\s+(-?[0-9]+)(?:\s|$)`)
	result := make(map[string]int64)
	for _, match := range pattern.FindAllSubmatch(source, -1) {
		value, err := strconv.ParseInt(string(match[2]), 10, 64)
		if err == nil {
			result[string(match[1])] = value
		}
	}
	return result
}

func findCIfStatements(source string) []cIfStatement {
	pattern := regexp.MustCompile(`\bif\s*\(`)
	var result []cIfStatement
	for offset := 0; offset < len(source); {
		location := pattern.FindStringIndex(source[offset:])
		if location == nil {
			break
		}
		start := offset + location[0]
		opening := offset + location[1] - 1
		closing := matchingCDelimiter(source, opening, '(', ')')
		if closing < 0 {
			break
		}
		bodyStart := closing + 1
		for bodyStart < len(source) && (source[bodyStart] == ' ' || source[bodyStart] == '\t' || source[bodyStart] == '\r' || source[bodyStart] == '\n') {
			bodyStart++
		}
		bodyEnd := bodyStart
		if bodyStart < len(source) && source[bodyStart] == '{' {
			bodyEnd = matchingCBrace(source, bodyStart)
			if bodyEnd < 0 {
				break
			}
			result = append(result, cIfStatement{condition: source[opening+1 : closing], body: source[bodyStart+1 : bodyEnd]})
			bodyEnd++
		} else {
			semicolon := strings.IndexByte(source[bodyStart:], ';')
			if semicolon < 0 {
				break
			}
			bodyEnd = bodyStart + semicolon + 1
			result = append(result, cIfStatement{condition: source[opening+1 : closing], body: source[bodyStart:bodyEnd]})
		}
		offset = max(bodyEnd, start+1)
	}
	return result
}

func addOptionRequiredFeatures(root string, options []option) error {
	versionSource, err := readRevisionFile(root, "src/version.c")
	if err != nil {
		return err
	}
	features := parseVimFeatures(versionSource)
	for i := range options {
		options[i].RequiredFeatures = optionRequiredFeatures(options[i].AvailableWhen, features)
	}
	return nil
}

func optionRequiredFeatures(condition string, features map[string]string) []string {
	condition = stripOuterParens(condition)
	if !positiveDefinedConjunctionPattern.MatchString(condition) {
		return nil
	}
	var result []string
	for _, match := range definedIdentifierPattern.FindAllStringSubmatch(condition, -1) {
		feature := features[match[1]]
		if feature == "" {
			return nil
		}
		if !slices.Contains(result, feature) {
			result = append(result, feature)
		}
	}
	return result
}

func parseVimFeatures(source []byte) map[string]string {
	features := make(map[string]string)
	for _, match := range vimFeaturePattern.FindAllSubmatch(source, -1) {
		macro := string(match[1])
		if macro == "" {
			macro = string(match[2])
		}
		feature := string(match[3])
		if previous, ok := features[macro]; ok && previous != feature {
			features[macro] = ""
		} else if !ok {
			features[macro] = feature
		}
	}
	return features
}

func parseCStringMacros(source []byte) (map[string]string, error) {
	result := make(map[string]string)
	for _, match := range cStringMacroPattern.FindAllSubmatch(source, -1) {
		value, err := strconv.Unquote(string(match[2]))
		if err != nil {
			return nil, fmt.Errorf("decode string macro %s: %w", match[1], err)
		}
		result[string(match[1])] = value
	}
	return result, nil
}

func parseCStringArrays(source []byte, macros map[string]string) (map[string][]string, error) {
	text := string(source)
	result := make(map[string][]string)
	for _, match := range cStringArrayPattern.FindAllStringSubmatchIndex(text, -1) {
		name := text[match[2]:match[3]]
		opening := match[1] - 1
		closing := matchingCBrace(text, opening)
		if closing < 0 {
			return nil, fmt.Errorf("unterminated string array %s", name)
		}
		forms, err := expandConditionalText(text[opening : closing+1])
		if err != nil {
			return nil, fmt.Errorf("expand string array %s: %w", name, err)
		}
		var values []string
		for _, form := range forms {
			items, err := initializerValues(strings.TrimSpace(stripCComments(form.Text)))
			if err != nil {
				return nil, fmt.Errorf("parse string array %s: %w", name, err)
			}
			for _, item := range items {
				item = strings.TrimSpace(item)
				if item == "NULL" {
					continue
				}
				value, ok := macros[item]
				if !ok {
					if len(item) < 2 || item[0] != '"' {
						return nil, fmt.Errorf("string array %s has unsupported value %q", name, item)
					}
					value, err = strconv.Unquote(item)
					if err != nil {
						return nil, fmt.Errorf("decode string array %s value %q: %w", name, item, err)
					}
				}
				values = appendUnique(values, value)
			}
		}
		if previous, exists := result[name]; exists && !slices.Equal(previous, values) {
			return nil, fmt.Errorf("string array %s has conflicting definitions", name)
		}
		result[name] = values
	}
	return result, nil
}

type cCall struct {
	position int
	args     []string
}

func parseExpandCallbackValues(source []byte, callback string, arrays map[string][]string, macros map[string]string) ([]string, error) {
	body, ok := cFunctionBody(string(source), callback)
	if !ok {
		return nil, fmt.Errorf("function body not found")
	}
	var selected cCall
	kind := ""
	for _, call := range findCCalls(body, "expand_set_opt_string") {
		if call.position >= selected.position {
			selected, kind = call, "strings"
		}
	}
	for _, call := range findCCalls(body, "expand_set_opt_listflag") {
		if call.position >= selected.position {
			selected, kind = call, "flags"
		}
	}
	if kind == "" || len(selected.args) < 2 {
		return nil, nil
	}
	name := cExpressionIdentifier(selected.args[1])
	if name == "" {
		return nil, nil
	}
	if kind == "strings" {
		return append([]string(nil), arrays[name]...), nil
	}
	flags, ok := macros[name]
	if !ok {
		return nil, nil
	}
	values := make([]string, 0, len(flags))
	for _, flag := range flags {
		values = append(values, string(flag))
	}
	return values, nil
}

func cFunctionBody(source, name string) (string, bool) {
	body, _, ok := cFunctionBodyAt(source, name)
	return body, ok
}

func cFunctionBodyAt(source, name string) (body string, definitionStart int, ok bool) {
	pattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s*\(`)
	location := pattern.FindStringIndex(source)
	if location == nil {
		return "", 0, false
	}
	openingOffset := strings.IndexByte(source[location[1]:], '{')
	if openingOffset < 0 {
		return "", 0, false
	}
	opening := location[1] + openingOffset
	closing := matchingCBrace(source, opening)
	if closing < 0 {
		return "", 0, false
	}
	return source[opening+1 : closing], location[0], true
}

func findCCalls(source, name string) []cCall {
	var result []cCall
	needle := name + "("
	for offset := 0; offset < len(source); {
		relative := strings.Index(source[offset:], needle)
		if relative < 0 {
			break
		}
		position := offset + relative
		opening := position + len(name)
		closing := matchingCDelimiter(source, opening, '(', ')')
		if closing < 0 {
			break
		}
		result = append(result, cCall{position: position, args: splitTopLevel(source[opening+1:closing], ',')})
		offset = closing + 1
	}
	return result
}

func cExpressionIdentifier(expression string) string {
	expression = strings.TrimSpace(expression)
	for strings.HasPrefix(expression, "(") {
		closing := matchingCDelimiter(expression, 0, '(', ')')
		if closing <= 0 {
			break
		}
		expression = strings.TrimSpace(expression[closing+1:])
	}
	for i, character := range expression {
		if !(character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || i > 0 && character >= '0' && character <= '9') {
			return ""
		}
	}
	return expression
}

func matchingCBrace(source string, start int) int {
	return matchingCDelimiter(source, start, '{', '}')
}

func matchingCDelimiter(source string, start int, opening, closing byte) int {
	depth := 0
	inString, inCharacter, escaped, lineComment, blockComment := false, false, false, false, false
	for i := start; i < len(source); i++ {
		character := source[i]
		if lineComment {
			if character == '\n' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if character == '*' && i+1 < len(source) && source[i+1] == '/' {
				blockComment = false
				i++
			}
			continue
		}
		if inString || inCharacter {
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if inString && character == '"' {
				inString = false
			} else if inCharacter && character == '\'' {
				inCharacter = false
			}
			continue
		}
		if character == '/' && i+1 < len(source) && source[i+1] == '/' {
			lineComment = true
			i++
			continue
		}
		if character == '/' && i+1 < len(source) && source[i+1] == '*' {
			blockComment = true
			i++
			continue
		}
		if character == '"' {
			inString = true
			continue
		}
		if character == '\'' {
			inCharacter = true
			continue
		}
		switch character {
		case opening:
			depth++
		case closing:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func appendUnique(values []string, additions ...string) []string {
	for _, addition := range additions {
		if !slices.Contains(values, addition) {
			values = append(values, addition)
		}
	}
	return values
}

func validateOptions(options []option) error {
	canonical := make(map[string]bool, len(options))
	abbreviations := make(map[string]string)
	for _, option := range options {
		if option.Name == "" {
			return fmt.Errorf("empty canonical option name")
		}
		if canonical[option.Name] {
			return fmt.Errorf("duplicate canonical option %s", option.Name)
		}
		canonical[option.Name] = true
		if option.DefinitionSource != "src/optiondefs.h" || option.DefinitionLine <= 0 {
			return fmt.Errorf("%s has incomplete definition provenance", option.Name)
		}
		if len(option.Flags) == 0 || len(option.Variants) == 0 || option.AvailableWhen == "" {
			return fmt.Errorf("%s has incomplete flags, variants, or availability", option.Name)
		}
		seenVariants := make(map[optionVariant]bool, len(option.Variants))
		for _, variant := range option.Variants {
			if variant.Variable == "" || variant.Indirect == "" || variant.DidSetCallback == "" || variant.ExpandCallback == "" || variant.ViDefault == "" || variant.VimDefault == "" {
				return fmt.Errorf("%s has an incomplete variant: %#v", option.Name, variant)
			}
			if seenVariants[variant] {
				return fmt.Errorf("%s has a duplicate variant: %#v", option.Name, variant)
			}
			seenVariants[variant] = true
		}
		if option.Validation.Callback != "" {
			if len(option.Validation.Sources) == 0 || option.Validation.Kind == "" {
				return fmt.Errorf("%s has incomplete callback provenance: %#v", option.Name, option.Validation)
			}
			for _, source := range option.Validation.Sources {
				if source.Source == "" || source.Line <= 0 {
					return fmt.Errorf("%s has incomplete callback source: %#v", option.Name, source)
				}
			}
		}
		if option.Validation.Kind == "ValidationNumberRange" {
			if !option.Validation.HasMin && !option.Validation.HasMax {
				return fmt.Errorf("%s has empty number validation: %#v", option.Name, option.Validation)
			}
		} else if option.Validation.Kind != "" && option.Validation.Kind != "ValidationNone" {
			if option.Validation.ErrorCode == "" {
				return fmt.Errorf("%s has incomplete validation rule: %#v", option.Name, option.Validation)
			}
			switch option.Validation.Kind {
			case "ValidationExact", "ValidationCommaList", "ValidationFlagList", "ValidationListChars", "ValidationFillChars", "ValidationStatuslineOpt":
				if len(option.Validation.Values) == 0 {
					return fmt.Errorf("%s has validation without values: %#v", option.Name, option.Validation)
				}
			case "ValidationWinHighlight":
				// Syntax is structural; highlight names remain runtime state.
			default:
				return fmt.Errorf("%s has unknown validation kind: %#v", option.Name, option.Validation)
			}
		}
		if option.ShortName != "" {
			if previous := abbreviations[option.ShortName]; previous != "" {
				return fmt.Errorf("duplicate abbreviation %s for %s and %s", option.ShortName, previous, option.Name)
			}
			abbreviations[option.ShortName] = option.Name
		}
	}
	for abbreviation, name := range abbreviations {
		if canonical[abbreviation] && abbreviation != name {
			return fmt.Errorf("abbreviation %s for %s conflicts with a canonical option", abbreviation, name)
		}
	}
	return nil
}

func writeOptionSetOracle(path string, options []option) error {
	options = append([]option(nil), options...)
	sort.Slice(options, func(i, j int) bool { return options[i].Name < options[j].Name })
	var output bytes.Buffer
	fmt.Fprintf(&output, "\" Code generated by tools/genmetadata from Vim %s (%s); DO NOT EDIT.\n", vimTag, vimCommit)
	output.WriteString("\" Exercise every migrated option through :set in one pinned Vim process.\n")
	output.WriteString("set nocompatible\n")
	output.WriteString("let s:option_set_failures = []\n")
	output.WriteString("let g:vimls_unsupported_options = []\n")
	output.WriteString("let g:vimls_missing_features = []\n")
	output.WriteString("function! s:CheckMigratedOption(name, command, available_when, required_features, source) abort\n")
	output.WriteString("  let supported = exists('+' .. a:name)\n")
	output.WriteString("  if !supported\n")
	output.WriteString("    call add(g:vimls_unsupported_options, a:name)\n")
	output.WriteString("    for feature in a:required_features\n")
	output.WriteString("      if !has(feature) && index(g:vimls_missing_features, '-' .. feature) < 0\n")
	output.WriteString("        call add(g:vimls_missing_features, '-' .. feature)\n")
	output.WriteString("      endif\n")
	output.WriteString("    endfor\n")
	output.WriteString("  endif\n")
	output.WriteString("  if a:available_when ==# '1' && !supported\n")
	output.WriteString("    call add(s:option_set_failures, a:name .. ' [' .. a:source .. ']: unconditional option is unavailable')\n")
	output.WriteString("  elseif a:available_when ==# '0' && supported\n")
	output.WriteString("    call add(s:option_set_failures, a:name .. ' [' .. a:source .. ']: permanently unavailable option is supported')\n")
	output.WriteString("  endif\n")
	output.WriteString("  try\n")
	output.WriteString("    execute a:command\n")
	output.WriteString("  catch\n")
	output.WriteString("    call add(s:option_set_failures, a:name .. ' [' .. a:source .. ']: ' .. v:exception .. ' @ ' .. v:throwpoint)\n")
	output.WriteString("  endtry\n")
	output.WriteString("endfunction\n")
	for _, option := range options {
		command := "set " + option.Name + "&"
		if option.Type == "OptionString" {
			command = "execute 'set " + option.Name + "=' .. escape(&" + option.Name + ", \" \\t\\\\|\\\"\")"
		}
		if slices.Contains(option.Flags, "P_NODEFAULT") {
			command = "silent set " + option.Name + "?"
		}
		fmt.Fprintf(&output, "call s:CheckMigratedOption(%s, %s, %s, %s, %s)\n", vimString(option.Name), vimString(command), vimString(option.AvailableWhen), vimList(option.RequiredFeatures), vimString(fmt.Sprintf("%s:%d", option.DefinitionSource, option.DefinitionLine)))
	}
	output.WriteString("function! s:CheckMigratedCompletion(name, expected, source) abort\n")
	output.WriteString("  if !exists('+' .. a:name)\n")
	output.WriteString("    return\n")
	output.WriteString("  endif\n")
	output.WriteString("  try\n")
	output.WriteString("    let actual = getcompletion('set ' .. a:name .. '=', 'cmdline')\n")
	output.WriteString("    let current = eval('&' .. a:name)\n")
	output.WriteString("    if current !=# '' && !empty(actual) && actual[0] ==# current\n")
	output.WriteString("      call remove(actual, 0)\n")
	output.WriteString("      let expected_index = index(a:expected, current)\n")
	output.WriteString("      if expected_index >= 0\n")
	output.WriteString("        call insert(actual, current, expected_index)\n")
	output.WriteString("      endif\n")
	output.WriteString("    endif\n")
	output.WriteString("    let expected_available = []\n")
	output.WriteString("    for value in a:expected\n")
	output.WriteString("      if index(actual, value) >= 0\n")
	output.WriteString("        call add(expected_available, value)\n")
	output.WriteString("      endif\n")
	output.WriteString("    endfor\n")
	output.WriteString("    if actual !=# expected_available\n")
	output.WriteString("      call add(s:option_set_failures, a:name .. ' completion [' .. a:source .. ']: expected available subset ' .. string(expected_available) .. ', got ' .. string(actual))\n")
	output.WriteString("    endif\n")
	output.WriteString("  catch\n")
	output.WriteString("    call add(s:option_set_failures, a:name .. ' completion [' .. a:source .. ']: ' .. v:exception .. ' @ ' .. v:throwpoint)\n")
	output.WriteString("  endtry\n")
	output.WriteString("endfunction\n")
	for _, option := range options {
		if len(option.CompletionValues) == 0 {
			continue
		}
		fmt.Fprintf(&output, "call s:CheckMigratedCompletion(%s, %s, %s)\n", vimString(option.Name), vimList(option.CompletionValues), vimString(fmt.Sprintf("%s:%d", option.DefinitionSource, option.DefinitionLine)))
	}
	output.WriteString("call extend(v:errors, s:option_set_failures)\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, output.Bytes(), 0o644)
}

func vimString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func vimList(values []string) string {
	items := make([]string, len(values))
	for i, value := range values {
		items[i] = vimString(value)
	}
	return "[" + strings.Join(items, ", ") + "]"
}

func addOptionDocumentation(root string, options []option) error {
	tagsSource, err := readRevisionFile(root, "runtime/doc/tags")
	if err != nil {
		return err
	}
	tagFiles, err := vimhelp.ParseTags(tagsSource)
	if err != nil {
		return err
	}
	files := map[string][]option{}
	for i := range options {
		file, ok := tagFiles["'"+options[i].Name+"'"]
		if !ok {
			return fmt.Errorf("documentation tag for %s is missing", options[i].Name)
		}
		files[file] = append(files[file], options[i])
	}
	filenames := make([]string, 0, len(files))
	for filename := range files {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)
	for _, file := range filenames {
		list := files[file]
		source, err := readRevisionFile(root, "runtime/doc/"+file)
		if err != nil {
			return err
		}
		tags := make([]string, 0, len(list))
		for _, o := range list {
			tags = append(tags, "'"+o.Name+"'")
		}
		docs, err := vimhelp.Extract(file, source, tags)
		if err != nil {
			return err
		}
		for i := range options {
			if tagFiles["'"+options[i].Name+"'"] != file {
				continue
			}
			d, ok := docs["'"+options[i].Name+"'"]
			if !ok || d.Markdown == "" || d.Source == "" {
				return fmt.Errorf("documentation for %s is empty", options[i].Name)
			}
			options[i].Documentation = d.Markdown
			options[i].DocumentationSource = d.Source
		}
	}
	return nil
}

func writeOptions(path string, options []option) error {
	options = append([]option(nil), options...)
	sort.Slice(options, func(i, j int) bool { return options[i].Name < options[j].Name })
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by tools/genmetadata from Vim %s (%s); DO NOT EDIT.\n", vimTag, vimCommit)
	fmt.Fprintln(&b, "// Documentation is derived from Vim runtime help; see Vim's LICENSE.")
	fmt.Fprintf(&b, "package vimdata\n\nconst (\n\tOptionVimTag = %q\n\tOptionVimCommit = %q\n)\n\nvar builtinOptions = [...]Option{\n", vimTag, vimCommit)
	for _, o := range options {
		validationKind := o.Validation.Kind
		if validationKind == "" {
			validationKind = "ValidationNone"
		}
		fmt.Fprintf(&b, "\t{Name: %q, ShortName: %q, Type: %s, Scope: %s, Flags: %#v, Variants: []OptionVariant{", o.Name, o.ShortName, o.Type, o.Scope, o.Flags)
		for _, variant := range o.Variants {
			fmt.Fprintf(&b, "{Condition: %q, Variable: %q, Indirect: %q, DidSetCallback: %q, ExpandCallback: %q, ViDefault: %q, VimDefault: %q},", variant.Condition, variant.Variable, variant.Indirect, variant.DidSetCallback, variant.ExpandCallback, variant.ViDefault, variant.VimDefault)
		}
		fmt.Fprintf(&b, "}, CompletionValues: %#v, Validation: OptionValidation{Kind: %s, Values: %#v, AllowEmpty: %t, AllowDuplicates: %t, Separator: %q, ErrorCode: %q, HasMin: %t, Min: %d, MinErrorCode: %q, HasMax: %t, Max: %d, MaxErrorCode: %q, Callback: %q, Sources: []OptionCallbackSource{", o.CompletionValues, validationKind, o.Validation.Values, o.Validation.AllowEmpty, o.Validation.AllowDuplicates, o.Validation.Separator, o.Validation.ErrorCode, o.Validation.HasMin, o.Validation.Min, o.Validation.MinErrorCode, o.Validation.HasMax, o.Validation.Max, o.Validation.MaxErrorCode, o.Validation.Callback)
		for _, source := range o.Validation.Sources {
			fmt.Fprintf(&b, "{Source: %q, Line: %d},", source.Source, source.Line)
		}
		fmt.Fprintf(&b, "}}, AvailableWhen: %q, RequiredFeatures: %#v, DefinitionSource: %q, DefinitionLine: %d, Documentation: %q, DocumentationSource: %q},\n", o.AvailableWhen, o.RequiredFeatures, o.DefinitionSource, o.DefinitionLine, o.Documentation, o.DocumentationSource)
	}
	b.WriteString("}\n")
	out, err := format.Source(b.Bytes())
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}
