// Package vimhelp extracts entries from Vim runtime help and converts their
// lightweight help markup to Markdown for runtime and generated documentation.
package vimhelp

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	helpDefinition = regexp.MustCompile(`\*[^*[:space:]]+\*`)
	helpLink       = regexp.MustCompile(`\|([^|\r\n]+)\|`)
	exampleStart   = regexp.MustCompile(`(?:^| )>([A-Za-z0-9_-]*)$`)
)

// Documentation is one generated Markdown help entry and its Vim runtime help
// filename. Source deliberately excludes a line number so pinned documentation
// edits do not create unstable public locations.
type Documentation struct {
	Markdown string
	Source   string
}

// ParseTags returns Vim help tag to runtime help filename mappings.
func ParseTags(source []byte) (map[string]string, error) {
	result := make(map[string]string)
	for lineNumber, line := range strings.Split(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 2 || fields[0] == "" || fields[1] == "" {
			return nil, fmt.Errorf("invalid Vim help tag at line %d", lineNumber+1)
		}
		if previous, duplicate := result[fields[0]]; duplicate {
			return nil, fmt.Errorf("Vim help tag %q appears more than once (%s and %s)", fields[0], previous, fields[1])
		}
		result[fields[0]] = fields[1]
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("Vim help tags are empty")
	}
	return result, nil
}

type boundary struct {
	line int
	tags []string
}

// Extract selects target help tags from one runtime help file. Multiple target
// tags on the same heading line share the same Markdown entry.
func Extract(sourceName string, source []byte, targets []string) (map[string]Documentation, error) {
	targetSet := make(map[string]bool, len(targets))
	for _, target := range targets {
		if target == "" || targetSet[target] {
			return nil, fmt.Errorf("invalid or duplicate target tag %q", target)
		}
		targetSet[target] = true
	}
	lines := strings.Split(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n")
	var boundaries []boundary
	found := make(map[string]bool, len(targets))
	for lineNumber, line := range lines {
		var tags []string
		for _, match := range helpDefinition.FindAllString(line, -1) {
			tag := strings.Trim(match, "*")
			if !targetSet[tag] {
				continue
			}
			if found[tag] {
				return nil, fmt.Errorf("Vim help tag %q is defined more than once in %s", tag, sourceName)
			}
			found[tag] = true
			tags = append(tags, tag)
		}
		if len(tags) > 0 {
			sort.Strings(tags)
			if len(boundaries) > 0 && boundaries[len(boundaries)-1].line == lineNumber {
				boundaries[len(boundaries)-1].tags = append(boundaries[len(boundaries)-1].tags, tags...)
			} else {
				boundaries = append(boundaries, boundary{line: lineNumber, tags: tags})
			}
		}
	}
	if len(found) != len(targetSet) {
		missing := make([]string, 0, len(targetSet)-len(found))
		for target := range targetSet {
			if !found[target] {
				missing = append(missing, target)
			}
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("Vim help tags missing from %s: %s", sourceName, strings.Join(missing, ", "))
	}
	for index := 0; index+1 < len(boundaries); {
		if ToMarkdown(strings.Join(lines[boundaries[index].line:boundaries[index+1].line], "\n")) != "" {
			index++
			continue
		}
		boundaries[index+1].tags = append(boundaries[index+1].tags, boundaries[index].tags...)
		boundaries = append(boundaries[:index], boundaries[index+1:]...)
	}

	result := make(map[string]Documentation, len(targets))
	for index := 0; index < len(boundaries); index++ {
		current := boundaries[index]
		end := boundaryEnd(lines, boundaries, index)

		markdown := ToMarkdown(strings.Join(lines[current.line:end], "\n"))
		if markdown == "" {
			return nil, fmt.Errorf("Vim help tags %s have empty documentation in %s", strings.Join(current.tags, ", "), sourceName)
		}
		documentation := Documentation{Markdown: markdown, Source: filepath.Base(sourceName)}
		for _, tag := range current.tags {
			result[tag] = documentation
		}
	}
	return result, nil
}

func boundaryEnd(lines []string, boundaries []boundary, index int) int {
	end := len(lines)
	if index+1 < len(boundaries) {
		end = boundaries[index+1].line
	}
	for line := boundaries[index].line + 1; line < end; line++ {
		trimmed := strings.TrimSpace(lines[line])
		if len(trimmed) >= 12 && (strings.Trim(trimmed, "=") == "" || strings.Trim(trimmed, "-") == "") {
			return line
		}
	}
	return end
}

// ToMarkdown converts the small set of Vim help markup used by generated
// catalog entries. It deliberately preserves prose line breaks and avoids
// guessing aligned text into Markdown tables.
func ToMarkdown(source string) string {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	output := make([]string, 0, len(lines)+4)
	inExample := false
	for _, original := range lines {
		line := strings.TrimRight(original, " \t\r")
		if inExample {
			trimmedLeft := strings.TrimLeft(line, " \t")
			if exampleEndMarker(line) {
				output = append(output, "```")
				inExample = false
				line = strings.TrimPrefix(trimmedLeft, "<")
			} else if line != "" && len(line) == len(trimmedLeft) {
				output = append(output, "```")
				inExample = false
			} else {
				output = append(output, strings.TrimPrefix(line, "\t"))
				continue
			}
		}

		line = helpDefinition.ReplaceAllString(line, "")
		line = helpLink.ReplaceAllString(line, "`$1`")
		exampleLine := line
		line = strings.TrimSpace(line)
		if line == "" {
			output = append(output, "")
			continue
		}
		if match := exampleStart.FindStringSubmatch(exampleLine); match != nil {
			marker := strings.TrimSpace(match[0])
			line = strings.TrimSpace(strings.TrimSuffix(line, marker))
			if line != "" {
				output = append(output, line)
			}
			language := match[1]
			if language == "" {
				language = "vim"
			}
			output = append(output, "```"+language)
			inExample = true
			continue
		}
		if strings.HasSuffix(line, "~") && len(strings.TrimSpace(strings.TrimSuffix(line, "~"))) > 0 {
			line = "### " + strings.TrimSpace(strings.TrimSuffix(line, "~"))
		}
		output = append(output, line)
	}
	if inExample {
		output = append(output, "```")
	}

	compacted := output[:0]
	blank := true
	for _, line := range output {
		if line == "" {
			if !blank {
				compacted = append(compacted, "")
			}
			blank = true
			continue
		}
		compacted = append(compacted, line)
		blank = false
	}
	for len(compacted) > 0 && compacted[len(compacted)-1] == "" {
		compacted = compacted[:len(compacted)-1]
	}
	return strings.Join(compacted, "\n")
}

func exampleEndMarker(line string) bool {
	// Preserve indented <tag> and <key> examples. Accept a standalone
	// indented marker too, as used by Vim's own options.txt documentation.
	if strings.HasPrefix(line, "<") {
		return true
	}
	trimmed := strings.TrimLeft(line, " \t")
	return trimmed == "<" || strings.HasPrefix(trimmed, "< ") || strings.HasPrefix(trimmed, "<\t")
}
