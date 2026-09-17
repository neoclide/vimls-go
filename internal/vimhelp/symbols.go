package vimhelp

import (
	"regexp"
	"strings"
)

// SymbolDocumentation is a tagged runtime help entry. Aliases retain separate
// names and source lines, but share the converted Markdown string.
type SymbolDocumentation struct {
	Name     string
	Tag      string
	Kind     string
	Source   string
	Line     int // one-based tag definition line
	Markdown string
}

var helpSymbolName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(#[A-Za-z_][A-Za-z0-9_]*)*$`)

// ExtractSymbols discovers Ex command, global variable, function, and <Plug>
// mapping documentation without requiring a generated tags file. Bare
// autoload tags are accepted because plugins such as VimTeX omit parentheses.
// Plain function tags require (). Untagged prose and pattern tags are not
// guessed into symbols.
func ExtractSymbols(sourceName string, source []byte) []SymbolDocumentation {
	lines := strings.Split(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n")
	var result []SymbolDocumentation
	start := -1
	var pending []SymbolDocumentation
	hasProse := false
	inExample := false
	flush := func(end int) {
		if start >= 0 && len(pending) > 0 {
			markdown := ToMarkdown(strings.Join(lines[start:end], "\n"))
			for _, entry := range pending {
				entry.Markdown = markdown
				result = append(result, entry)
			}
		}
		start, pending, hasProse = -1, nil, false
	}
	for number, line := range lines {
		trimmed := strings.TrimSpace(line)
		if inExample {
			left := strings.TrimLeft(line, " \t")
			if exampleEndMarker(line) || (left != "" && left == line) {
				inExample = false
			} else {
				continue
			}
		}
		if len(trimmed) >= 12 && (strings.Trim(trimmed, "=") == "" || strings.Trim(trimmed, "-") == "") || strings.HasPrefix(trimmed, "vim:") {
			flush(number)
			continue
		}
		tags := definitionTags(line)
		var symbols []SymbolDocumentation
		for _, tag := range tags {
			name, kind := symbolTag(tag)
			if kind != "" {
				symbols = append(symbols, SymbolDocumentation{Name: strings.Clone(name), Tag: strings.Clone(tag), Kind: kind, Source: sourceName, Line: number + 1})
			}
		}
		if len(symbols) > 0 {
			// Only tags that define an indexed symbol start a new entry.
			// Error and concept tags may occur inline in a symbol's prose
			// (for example, popup_create() documents *E450* in its first
			// sentence), so treating every help tag as a boundary truncates
			// otherwise valid documentation.
			if hasProse {
				flush(number)
			}
			if start < 0 {
				start = number
			}
			pending = append(pending, symbols...)
		}
		prose := trimmed
		if len(tags) > 0 {
			prose = strings.TrimSpace(helpDefinition.ReplaceAllString(line, ""))
		}
		if prose != "" {
			hasProse = true
		}
		if strings.Contains(line, ">") && exampleStart.MatchString(strings.TrimRight(line, " \t\r")) {
			inExample = true
		}
	}
	flush(len(lines))
	return result
}

func definitionTags(line string) []string {
	if !strings.Contains(line, "*") {
		return nil
	}
	var tags []string
	for _, span := range helpDefinition.FindAllStringIndex(line, -1) {
		// Stars embedded in words/expressions are not help definitions.
		if span[0] > 0 && line[span[0]-1] != ' ' && line[span[0]-1] != '\t' ||
			span[1] < len(line) && line[span[1]] != ' ' && line[span[1]] != '\t' && line[span[1]] != '\r' {
			continue
		}
		tags = append(tags, line[span[0]+1:span[1]-1])
	}
	return tags
}

func symbolTag(tag string) (string, string) {
	if plugMappingTag(tag) {
		return tag, "plug mapping"
	}
	if strings.HasPrefix(tag, ":") && helpSymbolName.MatchString(tag[1:]) {
		return tag, "Ex command"
	}
	function := strings.HasSuffix(tag, "()")
	name := strings.TrimSuffix(tag, "()")
	unscoped := strings.TrimPrefix(name, "g:")
	if !helpSymbolName.MatchString(unscoped) {
		return "", ""
	}
	if strings.HasPrefix(name, "g:") && !function {
		return name, "global variable"
	}
	if strings.Contains(unscoped, "#") {
		return name, "autoload function"
	}
	if function {
		return name, "global function"
	}
	return "", ""
}

func plugMappingTag(tag string) bool {
	if len(tag) <= len("<Plug>()") || !strings.EqualFold(tag[:len("<Plug>(")], "<Plug>(") || tag[len(tag)-1] != ')' {
		return false
	}
	return !strings.ContainsAny(tag[len("<Plug>("):len(tag)-1], "() \t\r\n")
}
