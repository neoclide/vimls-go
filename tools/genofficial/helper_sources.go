package main

import (
	"bytes"
	"strings"
)

type helperSourceLine struct {
	Start      int
	ContentEnd int
	End        int
}

type helperHeredoc struct {
	Name        string
	HeaderStart int
	BodyStart   int
	BodyEnd     int
	End         int
	Trim        bool
	Evaluate    bool
	Lines       []string
}

// scanHelperHeredocs finds only heredoc assignments in the test driver source.
// It skips their bodies, so text being tested is never mistaken for another
// assignment or a Check* invocation in the driver itself.
func scanHelperHeredocs(source []byte) []helperHeredoc {
	lines := splitHelperSourceLines(source)
	var result []helperHeredoc
	for lineIndex := 0; lineIndex < len(lines); lineIndex++ {
		line := lines[lineIndex]
		name, trim, evaluate, marker, ok := parseHelperHeredocHeader(source[line.Start:line.ContentEnd])
		if !ok {
			continue
		}
		headerIndent := leadingHelperIndent(source[line.Start:line.ContentEnd])
		endLine := lineIndex + 1
		for ; endLine < len(lines); endLine++ {
			content := source[lines[endLine].Start:lines[endLine].ContentEnd]
			if trim && bytes.HasPrefix(content, headerIndent) {
				content = content[len(headerIndent):]
			}
			if bytes.Equal(content, marker) {
				break
			}
		}
		if endLine == len(lines) {
			continue
		}
		bodyLines := make([][]byte, 0, endLine-lineIndex-1)
		for bodyLine := lineIndex + 1; bodyLine < endLine; bodyLine++ {
			bodyLines = append(bodyLines, source[lines[bodyLine].Start:lines[bodyLine].ContentEnd])
		}
		decoded := decodeHelperHeredocLines(bodyLines, trim)
		bodyStart := lines[endLine].Start
		if lineIndex+1 < endLine {
			bodyStart = lines[lineIndex+1].Start
		}
		result = append(result, helperHeredoc{
			Name: name, HeaderStart: line.Start, BodyStart: bodyStart,
			BodyEnd: lines[endLine].Start, End: lines[endLine].End,
			Trim: trim, Evaluate: evaluate, Lines: decoded,
		})
		lineIndex = endLine
	}
	return result
}

func splitHelperSourceLines(source []byte) []helperSourceLine {
	if len(source) == 0 {
		return nil
	}
	lines := make([]helperSourceLine, 0, bytes.Count(source, []byte{'\n'})+1)
	for start := 0; start < len(source); {
		newline := bytes.IndexByte(source[start:], '\n')
		end := len(source)
		contentEnd := end
		if newline >= 0 {
			contentEnd = start + newline
			end = contentEnd + 1
		}
		if contentEnd > start && source[contentEnd-1] == '\r' {
			contentEnd--
		}
		lines = append(lines, helperSourceLine{Start: start, ContentEnd: contentEnd, End: end})
		start = end
	}
	return lines
}

func parseHelperHeredocHeader(line []byte) (name string, trim, evaluate bool, marker []byte, ok bool) {
	operator := helperHeredocOperator(line)
	if operator < 0 {
		return "", false, false, nil, false
	}
	name = helperHeredocVariable(line[:operator])
	if name == "" {
		return "", false, false, nil, false
	}
	index := operator + 3
	for {
		index = skipHelperHorizontalSpace(line, index)
		start := index
		for index < len(line) && line[index] != ' ' && line[index] != '\t' {
			index++
		}
		word := line[start:index]
		if bytes.Equal(word, []byte("trim")) {
			trim = true
			continue
		}
		if bytes.Equal(word, []byte("eval")) {
			evaluate = true
			continue
		}
		marker = word
		break
	}
	if len(marker) == 0 || (marker[0] >= 'a' && marker[0] <= 'z') {
		return "", false, false, nil, false
	}
	index = skipHelperHorizontalSpace(line, index)
	if index < len(line) && line[index] != '#' && line[index] != '"' {
		return "", false, false, nil, false
	}
	return name, trim, evaluate, append([]byte(nil), marker...), true
}

func helperHeredocOperator(line []byte) int {
	for index := 0; index+2 < len(line); {
		switch line[index] {
		case '\'':
			next, ok := skipHelperString(line, index, len(line))
			if !ok {
				return -1
			}
			index = next
		case '"':
			next, ok := skipHelperString(line, index, len(line))
			if !ok {
				return -1
			}
			index = next
		case '#':
			if index == 0 || isHelperSpace(line[index-1]) {
				return -1
			}
			index++
		case '=':
			if line[index+1] == '<' && line[index+2] == '<' {
				return index
			}
			index++
		default:
			index++
		}
	}
	return -1
}

func helperHeredocVariable(prefix []byte) string {
	prefix = bytes.TrimSpace(prefix)
	for _, modifier := range []string{"legacy", "vim9cmd"} {
		if helperLeadingWord(prefix, modifier) {
			prefix = bytes.TrimSpace(prefix[len(modifier):])
		}
	}
	for _, command := range []string{"let", "var", "const", "final"} {
		if helperLeadingWord(prefix, command) {
			prefix = bytes.TrimSpace(prefix[len(command):])
			break
		}
	}
	if len(prefix) == 0 {
		return ""
	}
	start := 0
	if len(prefix) >= 2 && prefix[1] == ':' && strings.ContainsRune("gbwtslav", rune(prefix[0])) {
		start = 2
	}
	if start >= len(prefix) || !isHelperVariableStart(prefix[start]) {
		return ""
	}
	end := start + 1
	for end < len(prefix) && isHelperVariablePart(prefix[end]) {
		end++
	}
	nameEnd := end
	if start == 2 {
		nameEnd = end
	}
	rest := bytes.TrimSpace(prefix[end:])
	if len(rest) != 0 && rest[0] != ':' {
		return ""
	}
	return string(prefix[:nameEnd])
}

func helperLeadingWord(source []byte, word string) bool {
	return len(source) > len(word) && bytes.Equal(source[:len(word)], []byte(word)) && isHelperSpace(source[len(word)])
}

func isHelperVariableStart(value byte) bool {
	return value == '_' || (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z')
}

func isHelperVariablePart(value byte) bool {
	return isHelperVariableStart(value) || (value >= '0' && value <= '9') || value == '#'
}

func skipHelperHorizontalSpace(source []byte, index int) int {
	for index < len(source) && (source[index] == ' ' || source[index] == '\t') {
		index++
	}
	return index
}

func leadingHelperIndent(line []byte) []byte {
	end := 0
	for end < len(line) && (line[end] == ' ' || line[end] == '\t') {
		end++
	}
	return line[:end]
}

func decodeHelperHeredocLines(lines [][]byte, trim bool) []string {
	indent := []byte(nil)
	if trim {
		for _, line := range lines {
			if len(line) != 0 {
				indent = append([]byte(nil), leadingHelperIndent(line)...)
				break
			}
		}
	}
	result := make([]string, len(lines))
	for index, line := range lines {
		remove := 0
		for remove < len(indent) && remove < len(line) && line[remove] == indent[remove] {
			remove++
		}
		result[index] = string(line[remove:])
	}
	return result
}

func helperHeredocContaining(heredocs []helperHeredoc, offset int) (helperHeredoc, bool) {
	for _, heredoc := range heredocs {
		if offset >= heredoc.BodyStart && offset < heredoc.BodyEnd {
			return heredoc, true
		}
	}
	return helperHeredoc{}, false
}
