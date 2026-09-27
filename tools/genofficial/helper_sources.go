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
	Complete    bool
	Lines       []string
}

type helperDialect byte

const (
	helperLegacy helperDialect = iota
	helperVim9
)

// scanHelperHeredocs finds only heredoc assignments in the test driver source.
// It skips their bodies, so text being tested is never mistaken for another
// assignment or a Check* invocation in the driver itself.
func scanHelperHeredocs(source []byte) []helperHeredoc {
	lines := splitHelperSourceLines(source)
	var result []helperHeredoc
	dialect := helperLegacy
	var dialectStack []helperDialect
	scriptCommandSeen := false
	for lineIndex := 0; lineIndex < len(lines); lineIndex++ {
		line := lines[lineIndex]
		content := source[line.Start:line.ContentEnd]
		effectiveDialect := helperCommandDialect(content, dialect)
		name, trim, evaluate, marker, ok := parseHelperHeredocHeader(content, effectiveDialect)
		if !ok {
			dialect, dialectStack, scriptCommandSeen = updateHelperDialect(content, dialect, dialectStack, scriptCommandSeen)
			continue
		}
		headerIndent := leadingHelperIndent(content)
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
		bodyLines := make([][]byte, 0, endLine-lineIndex-1)
		bodyLimit := min(endLine, len(lines))
		for bodyLine := lineIndex + 1; bodyLine < bodyLimit; bodyLine++ {
			bodyLines = append(bodyLines, source[lines[bodyLine].Start:lines[bodyLine].ContentEnd])
		}
		decoded := decodeHelperHeredocLines(bodyLines, trim)
		complete := endLine < len(lines)
		bodyEnd := len(source)
		end := len(source)
		if complete {
			bodyEnd = lines[endLine].Start
			end = lines[endLine].End
		}
		bodyStart := bodyEnd
		if lineIndex+1 < bodyLimit {
			bodyStart = lines[lineIndex+1].Start
		}
		result = append(result, helperHeredoc{
			Name: name, HeaderStart: line.Start, BodyStart: bodyStart,
			BodyEnd: bodyEnd, End: end, Trim: trim, Evaluate: evaluate,
			Complete: complete, Lines: decoded,
		})
		if !complete {
			break
		}
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

func parseHelperHeredocHeader(line []byte, dialect helperDialect) (name string, trim, evaluate bool, marker []byte, ok bool) {
	operator := helperHeredocOperator(line)
	if operator < 0 {
		return "", false, false, nil, false
	}
	if dialect == helperVim9 && (operator == 0 || !isHelperHorizontalSpace(line[operator-1]) || operator+3 >= len(line) || !isHelperHorizontalSpace(line[operator+3])) {
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
	if index < len(line) {
		comment := byte('"')
		if dialect == helperVim9 {
			comment = '#'
		}
		if line[index] != comment {
			return "", false, false, nil, false
		}
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
	prefix, _ = trimHelperCommandModifiers(prefix, helperLegacy)
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

func isHelperHorizontalSpace(value byte) bool {
	return value == ' ' || value == '\t'
}

func helperCommandDialect(line []byte, dialect helperDialect) helperDialect {
	_, dialect = trimHelperCommandModifiers(bytes.TrimSpace(line), dialect)
	return dialect
}

func updateHelperDialect(line []byte, dialect helperDialect, stack []helperDialect, scriptCommandSeen bool) (helperDialect, []helperDialect, bool) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 || trimmed[0] == '#' || trimmed[0] == '"' {
		return dialect, stack, scriptCommandSeen
	}
	trimmed, _ = trimHelperCommandModifiers(trimmed, dialect)
	if helperLeadingCommand(trimmed, "enddef") || helperLeadingCommand(trimmed, "endfunc") || helperLeadingCommand(trimmed, "endfunction") {
		if len(stack) == 0 {
			return dialect, stack, true
		}
		return stack[len(stack)-1], stack[:len(stack)-1], scriptCommandSeen
	}
	if len(stack) == 0 {
		if helperLeadingCommand(trimmed, "vim9script") {
			if !scriptCommandSeen {
				dialect = helperVim9
			}
			return dialect, stack, true
		}
		scriptCommandSeen = true
	}
	trimmed = trimHelperDeclarationModifiers(trimmed)
	if helperLeadingCommand(trimmed, "def") || helperLeadingCommand(trimmed, "def!") {
		return helperVim9, append(stack, dialect), scriptCommandSeen
	}
	if helperLeadingCommand(trimmed, "func") || helperLeadingCommand(trimmed, "func!") || helperLeadingCommand(trimmed, "function") || helperLeadingCommand(trimmed, "function!") {
		return helperLegacy, append(stack, dialect), scriptCommandSeen
	}
	return dialect, stack, scriptCommandSeen
}

func trimHelperDeclarationModifiers(line []byte) []byte {
	for {
		trimmed := false
		for _, modifier := range []string{"abstract", "export", "public", "static"} {
			if helperLeadingWord(line, modifier) {
				line = bytes.TrimSpace(line[len(modifier):])
				trimmed = true
				break
			}
		}
		if !trimmed {
			return line
		}
	}
}

func helperLeadingCommand(source []byte, command string) bool {
	if len(source) < len(command) || !bytes.Equal(source[:len(command)], []byte(command)) {
		return false
	}
	if len(source) == len(command) {
		return true
	}
	next := source[len(command)]
	return isHelperSpace(next) || next == '('
}

func trimHelperCommandModifiers(source []byte, dialect helperDialect) ([]byte, helperDialect) {
	skipNumber := false
	for len(source) != 0 {
		end := 0
		for end < len(source) && !isHelperSpace(source[end]) {
			end++
		}
		word := string(source[:end])
		if skipNumber && helperDecimalWord(word) {
			source = bytes.TrimSpace(source[end:])
			skipNumber = false
			continue
		}
		skipNumber = false
		switch {
		case helperCommandAbbreviation(word, "legacy", 3):
			dialect = helperLegacy
		case helperCommandAbbreviation(word, "vim9cmd", 4):
			dialect = helperVim9
		case helperCommandAbbreviation(word, "aboveleft", 3),
			helperCommandAbbreviation(word, "belowright", 3),
			helperCommandAbbreviation(word, "browse", 3),
			helperCommandAbbreviation(word, "botright", 2),
			helperCommandAbbreviation(word, "confirm", 4),
			helperCommandAbbreviation(word, "hide", 3),
			helperCommandAbbreviation(word, "horizontal", 3),
			helperCommandAbbreviation(word, "keepalt", 5),
			helperCommandAbbreviation(word, "keepjumps", 5),
			helperCommandAbbreviation(word, "keepmarks", 3),
			helperCommandAbbreviation(word, "keeppatterns", 5),
			helperCommandAbbreviation(word, "leftabove", 5),
			helperCommandAbbreviation(word, "lockmarks", 3),
			helperCommandAbbreviation(word, "noautocmd", 3),
			helperCommandAbbreviation(word, "noswapfile", 3),
			helperCommandAbbreviation(word, "rightbelow", 6),
			helperCommandAbbreviation(word, "sandbox", 3),
			helperCommandAbbreviation(word, "silent", 3),
			helperCommandAbbreviation(word, "silent!", 3),
			helperCommandAbbreviation(word, "topleft", 2),
			helperCommandAbbreviation(word, "unsilent", 3),
			helperCommandAbbreviation(word, "vertical", 4):
		case helperCommandAbbreviation(word, "tab", 3), helperCommandAbbreviation(word, "verbose", 4):
			skipNumber = true
		default:
			return source, dialect
		}
		source = bytes.TrimSpace(source[end:])
	}
	return source, dialect
}

func helperCommandAbbreviation(word, command string, minimum int) bool {
	return len(word) >= minimum && len(word) <= len(command) && command[:len(word)] == word
}

func helperDecimalWord(word string) bool {
	if word == "" {
		return false
	}
	for index := range word {
		if word[index] < '0' || word[index] > '9' {
			return false
		}
	}
	return true
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
