package main

import (
	"unicode/utf8"
)

// helperArgument is an absolute, half-open byte span in a Check* call.
type helperArgument struct {
	Start int
	End   int
}

// splitHelperArguments splits the contents of the parenthesized call at
// top-level commas.  It deliberately only understands expression delimiters
// and strings: callers that need to interpret an argument can do so without
// making this scanner accept arbitrary Vim expressions.
func splitHelperArguments(source []byte, open, close int) ([]helperArgument, bool) {
	if open < 0 || close <= open || open >= len(source) || close > len(source) || source[open] != '(' || source[close-1] != ')' {
		return nil, false
	}

	end := close - 1
	start := trimHelperSpace(source, open+1, end)
	if start == end {
		return []helperArgument{}, true
	}
	arguments := make([]helperArgument, 0, 2)
	var delimiters []byte
	for i := open + 1; i < end; {
		switch source[i] {
		case '\'', '"':
			next, ok := skipHelperString(source, i, end)
			if !ok {
				return nil, false
			}
			i = next
		case '(', '[', '{':
			delimiters = append(delimiters, source[i])
			i++
		case ')', ']', '}':
			if len(delimiters) == 0 || !matchingHelperDelimiter(delimiters[len(delimiters)-1], source[i]) {
				return nil, false
			}
			delimiters = delimiters[:len(delimiters)-1]
			i++
		case ',':
			if len(delimiters) == 0 {
				argumentStart := trimHelperSpace(source, start, i)
				argumentEnd := trimHelperSpaceRight(source, argumentStart, i)
				if argumentStart == argumentEnd {
					return nil, false
				}
				arguments = append(arguments, helperArgument{Start: argumentStart, End: argumentEnd})
				start = trimHelperSpace(source, i+1, end)
			}
			i++
		default:
			i++
		}
	}
	if len(delimiters) != 0 {
		return nil, false
	}
	argumentStart := trimHelperSpace(source, start, end)
	argumentEnd := trimHelperSpaceRight(source, argumentStart, end)
	if argumentStart != argumentEnd {
		arguments = append(arguments, helperArgument{Start: argumentStart, End: argumentEnd})
	} else if len(arguments) == 0 {
		// The only way to have no final argument after a non-empty body is an
		// empty argument or a leading comma, both of which are invalid.  The
		// empty-call case returned above.
		return nil, false
	}
	return arguments, true
}

func trimHelperSpace(source []byte, start, end int) int {
	for start < end && isHelperSpace(source[start]) {
		start++
	}
	return start
}

func trimHelperSpaceRight(source []byte, start, end int) int {
	for end > start && isHelperSpace(source[end-1]) {
		end--
	}
	return end
}

func isHelperSpace(value byte) bool {
	switch value {
	case ' ', '\t', '\r', '\n', '\f', '\v':
		return true
	default:
		return false
	}
}

func matchingHelperDelimiter(open, close byte) bool {
	return (open == '(' && close == ')') || (open == '[' && close == ']') || (open == '{' && close == '}')
}

func skipHelperString(source []byte, start, end int) (int, bool) {
	quote := source[start]
	for i := start + 1; i < end; i++ {
		switch source[i] {
		case '\'':
			if quote != '\'' {
				continue
			}
			if i+1 < end && source[i+1] == '\'' {
				i++
				continue
			}
			return i + 1, true
		case '"':
			if quote != '"' {
				continue
			}
			return i + 1, true
		case '\\':
			if quote == '"' {
				if i+1 >= end {
					return 0, false
				}
				i++
			}
		}
	}
	return 0, false
}

// decodeStaticStringList decodes a list whose elements are literal strings.
// It intentionally rejects every expression form, including concatenation,
// interpolation, comments, and nested containers, so its result is safe to
// use as source text without evaluating Vim code.
func decodeStaticStringList(source []byte, argument helperArgument) ([]string, bool) {
	if argument.Start < 0 || argument.End < argument.Start || argument.End > len(source) {
		return nil, false
	}
	start := argument.Start
	end := argument.End
	for start < end && isHelperSpace(source[start]) {
		start++
	}
	for end > start && isHelperSpace(source[end-1]) {
		end--
	}
	if end-start < 2 || source[start] != '[' || source[end-1] != ']' {
		return nil, false
	}

	limit := end - 1
	i := skipListWhitespace(source, start+1, limit, start+1)
	values := make([]string, 0, 2)
	if i == limit {
		return values, true
	}
	for {
		if i >= limit || (source[i] != '\'' && source[i] != '"') {
			return nil, false
		}
		var value string
		var next int
		var ok bool
		if source[i] == '\'' {
			value, next, ok = decodeHelperSingleString(source, i, limit)
		} else {
			value, next, ok = decodeHelperDoubleString(source, i, limit)
		}
		if !ok {
			return nil, false
		}
		values = append(values, value)
		i = skipListWhitespace(source, next, limit, start+1)
		if i == limit {
			return values, true
		}
		if source[i] != ',' {
			return nil, false
		}
		i = skipListWhitespace(source, i+1, limit, start+1)
		if i == limit {
			return values, true
		}
	}
}

// skipListWhitespace also removes Vim's legacy continuation marker when it
// is the first non-whitespace byte on a physical line after the list began.
func skipListWhitespace(source []byte, start, end, listContentStart int) int {
	for start < end {
		for start < end && isHelperSpace(source[start]) {
			start++
		}
		if start >= end || source[start] != '\\' || !isListContinuation(source, start, listContentStart) {
			return start
		}
		start++
	}
	return start
}

func isListContinuation(source []byte, index, listContentStart int) bool {
	lineStart := index
	for lineStart > listContentStart && source[lineStart-1] != '\n' {
		lineStart--
	}
	if lineStart == listContentStart {
		return false
	}
	for i := lineStart; i < index; i++ {
		if !isHelperSpace(source[i]) {
			return false
		}
	}
	return true
}

func decodeHelperSingleString(source []byte, start, end int) (string, int, bool) {
	value := make([]byte, 0, end-start)
	for i := start + 1; i < end; {
		if source[i] != '\'' {
			value = append(value, source[i])
			i++
			continue
		}
		if i+1 < end && source[i+1] == '\'' {
			value = append(value, '\'')
			i += 2
			continue
		}
		return string(value), i + 1, true
	}
	return "", 0, false
}

func decodeHelperDoubleString(source []byte, start, end int) (string, int, bool) {
	value := make([]byte, 0, end-start)
	for i := start + 1; i < end; {
		switch source[i] {
		case '"':
			return string(value), i + 1, true
		case '\\':
			if i+1 >= end {
				return "", 0, false
			}
			next := source[i+1]
			switch next {
			case 'b':
				value = append(value, '\b')
				i += 2
			case 'e':
				value = append(value, 0x1b)
				i += 2
			case 'f':
				value = append(value, '\f')
				i += 2
			case 'n':
				value = append(value, '\n')
				i += 2
			case 'r':
				value = append(value, '\r')
				i += 2
			case 't':
				value = append(value, '\t')
				i += 2
			case '\\', '"':
				value = append(value, next)
				i += 2
			case 'x', 'X':
				decoded, nextIndex, ok := decodeHelperHex(source, i+2, end, 2)
				if !ok || decoded == 0 {
					return "", 0, false
				}
				value = append(value, byte(decoded))
				i = nextIndex
			case 'u', 'U':
				maxDigits := 4
				if next == 'U' {
					maxDigits = 8
				}
				decoded, nextIndex, ok := decodeHelperHex(source, i+2, end, maxDigits)
				if !ok || decoded == 0 || decoded > utf8.MaxRune || (decoded >= 0xd800 && decoded <= 0xdfff) {
					return "", 0, false
				}
				var encoded [utf8.UTFMax]byte
				width := utf8.EncodeRune(encoded[:], rune(decoded))
				value = append(value, encoded[:width]...)
				i = nextIndex
			default:
				if next < '0' || next > '7' {
					return "", 0, false
				}
				decoded := uint32(next - '0')
				nextIndex := i + 2
				for nextIndex < end && nextIndex < i+4 && source[nextIndex] >= '0' && source[nextIndex] <= '7' {
					decoded = decoded*8 + uint32(source[nextIndex]-'0')
					nextIndex++
				}
				if decoded == 0 {
					return "", 0, false
				}
				value = append(value, byte(decoded))
				i = nextIndex
			}
		default:
			value = append(value, source[i])
			i++
		}
	}
	return "", 0, false
}

func decodeHelperHex(source []byte, start, end, maxDigits int) (uint32, int, bool) {
	if start >= end || !isHelperHex(source[start]) {
		return 0, 0, false
	}
	value := uint32(0)
	i := start
	for i < end && i < start+maxDigits && isHelperHex(source[i]) {
		value = value*16 + uint32(helperHexValue(source[i]))
		i++
	}
	return value, i, true
}

func isHelperHex(value byte) bool {
	return (value >= '0' && value <= '9') || (value >= 'a' && value <= 'f') || (value >= 'A' && value <= 'F')
}

func helperHexValue(value byte) byte {
	switch {
	case value >= '0' && value <= '9':
		return value - '0'
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10
	default:
		return value - 'A' + 10
	}
}
