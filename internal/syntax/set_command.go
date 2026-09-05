package syntax

import (
	"unicode/utf8"

	"github.com/neoclide/vimls-go/internal/vimdata"
)

func scanSetCommandArgument(source string, start, end int, dialect Dialect, parsed *Command) (int, Span, Span, *expressionBoundary) {
	node, argumentEnd, separator, comment, diagnostics := parseSetCommand(source, start, end, dialect)
	parsed.Set = node
	if len(diagnostics) > 0 {
		parsed.boundaryExpression = &expressionBoundary{argument: Span{Start: start, End: end}, diagnostics: diagnostics}
		return argumentEnd, Span{}, Span{}, parsed.boundaryExpression
	}
	return argumentEnd, separator, comment, nil
}

// parseSetCommand parses the small, syntax-only part of Vim's :set family.
// Option names are deliberately not looked up: Vim's option table is mutable
// across versions and user-defined terminal options are valid input here.
func parseSetCommand(source string, start, end int, dialect Dialect) (*SetCommand, int, Span, Span, []Diagnostic) {
	node := &SetCommand{}
	argumentEnd, separator, comment := scanEscapedExArgument(source, start, end, dialect, vimdata.Command{})
	position := start
	previousSpecial := false
	for position < argumentEnd {
		beforeSpace := position
		position = skipSpace(source, position, argumentEnd)
		if position >= argumentEnd {
			break
		}
		if previousSpecial && dialect == Vim9 && (source[position] == '!' || source[position] == '&') {
			operatorEnd := scanSetOperator(source, position, argumentEnd)
			badEnd := scanSetItemEnd(source, position, argumentEnd)
			node.Options = append(node.Options, SetOption{
				Span: Span{Start: position, End: badEnd}, Operator: Span{Start: position, End: operatorEnd},
			})
			message := "No white space allowed between option and: " + source[position:badEnd]
			return node, end, Span{}, Span{}, []Diagnostic{{
				Code: "vim/E1205", Message: message, Span: Span{Start: beforeSpace, End: operatorEnd},
			}}
		}
		previousSpecial = false

		itemStart := position
		prefixLength := setPrefixLength(source, position, argumentEnd)
		prefix := Span{}
		if prefixLength > 0 {
			prefix = Span{Start: position, End: position + prefixLength}
		}
		nameStart := position + prefixLength
		nameEnd, unterminated := scanSetName(source, nameStart, argumentEnd)
		if unterminated {
			option := SetOption{Span: Span{Start: itemStart, End: argumentEnd}, Prefix: prefix, Name: Span{Start: nameStart, End: argumentEnd}}
			node.Options = append(node.Options, option)
			diagnostic := Diagnostic{Code: "vim/E474", Message: "Invalid argument", Span: Span{Start: nameStart, End: argumentEnd}}
			return node, end, Span{}, Span{}, []Diagnostic{diagnostic}
		}
		specialAll := false
		specialImmediate := false
		if prefixLength == 0 && setAllPrefix(source, nameStart, argumentEnd) {
			specialAll = true
			nameEnd = nameStart + 3
			specialImmediate = nameEnd >= argumentEnd || source[nameEnd] != '&'
		}
		if prefixLength == 0 && setTermcapPrefix(source, nameStart, argumentEnd) {
			nameEnd = nameStart + 7
			specialImmediate = true
		}
		if nameEnd == nameStart {
			// Keep an unknown or incomplete item opaque while still retaining a
			// useful option span. This also guarantees forward progress.
			nameEnd = scanSetOpaqueItem(source, nameStart, argumentEnd)
		}
		name := Span{Start: nameStart, End: nameEnd}
		operatorStart := nameEnd
		operatorPosition := operatorStart
		if !specialImmediate {
			operatorPosition = skipSpace(source, operatorStart, argumentEnd)
		}
		hadWhitespace := operatorPosition > operatorStart
		operatorEnd := scanSetOperator(source, operatorPosition, argumentEnd)
		if specialImmediate {
			operatorEnd = operatorPosition
		} else if specialAll {
			// do_set() consumes exactly the one ampersand in `all&`; the
			// option-specific &vi/&vim spellings do not apply to this special.
			operatorEnd = operatorPosition + 1
		}
		if hadWhitespace && dialect == Vim9 && setVim9WhitespaceError(source, operatorPosition, operatorEnd, argumentEnd) {
			badEnd := scanSetItemEnd(source, operatorPosition, argumentEnd)
			value := Span{}
			if operatorEnd > operatorPosition && setAssignmentOperator(source, operatorPosition, operatorEnd) {
				value = Span{Start: operatorEnd, End: badEnd}
			}
			node.Options = append(node.Options, SetOption{
				Span: Span{Start: itemStart, End: badEnd}, Prefix: prefix, Name: name,
				Operator: Span{Start: operatorPosition, End: operatorEnd}, Value: value,
			})
			message := "No white space allowed between option and: " + source[operatorPosition:badEnd]
			return node, end, Span{}, Span{}, []Diagnostic{{
				Code: "vim/E1205", Message: message, Span: Span{Start: operatorStart, End: operatorEnd},
			}}
		}
		// Vim9 never skips whitespace between an option name and its
		// following bytes. Non-E1205 forms such as `opt ?`, `opt :value`
		// and `opt <t_xx>` therefore begin a new item instead of attaching an
		// operator to the preceding option.
		if hadWhitespace && dialect == Vim9 {
			operatorPosition = operatorStart
			operatorEnd = operatorStart
		}

		itemEnd := nameEnd
		operator := Span{}
		value := Span{}
		if operatorEnd > operatorPosition {
			operator = Span{Start: operatorPosition, End: operatorEnd}
			if setAssignmentOperator(source, operatorPosition, operatorEnd) {
				valueStart := operatorEnd
				itemEnd = scanSetItemEnd(source, valueStart, argumentEnd)
				value = Span{Start: valueStart, End: itemEnd}
			} else {
				itemEnd = operatorEnd
			}
		} else if specialImmediate {
			itemEnd = nameEnd
		} else {
			itemEnd = scanSetItemEnd(source, nameEnd, argumentEnd)
		}
		if itemEnd < itemStart {
			itemEnd = itemStart
		}
		node.Options = append(node.Options, SetOption{
			Span: Span{Start: itemStart, End: itemEnd}, Prefix: prefix, Name: name,
			Operator: operator, Value: value,
		})
		position = itemEnd
		previousSpecial = specialImmediate || specialAll
	}
	return node, argumentEnd, separator, comment, nil
}

func setAllPrefix(source string, start, end int) bool {
	if start+3 > end || source[start:start+3] != "all" {
		return false
	}
	return start+3 == end || !isASCIIAlpha(source[start+3])
}

func setTermcapPrefix(source string, start, end int) bool {
	return start+7 <= end && source[start:start+7] == "termcap"
}

func scanSetName(source string, start, end int) (int, bool) {
	if start >= end {
		return start, false
	}
	if source[start] == '<' {
		if start+4 < end && source[start+1] == 't' && source[start+2] == '_' && source[start+3] != 0 && source[start+4] != 0 {
			if start+5 < end && source[start+5] == '>' {
				return start + 6, false
			}
			return end, true
		}
		for index := start + 1; index < end; index++ {
			if source[index] == '>' {
				return index + 1, false
			}
		}
		return end, true
	}
	if start+2 <= end && source[start] == 't' && source[start+1] == '_' {
		if start+4 <= end {
			return start + 4, false
		}
		return end, false
	}
	position := start
	for position < end && isSetNameByte(source[position]) {
		position++
	}
	return position, false
}

func isSetNameByte(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' || character == '_'
}

func setPrefixLength(source string, start, end int) int {
	if start+6 <= end && source[start:start+6] == "novice" {
		return 0
	}
	if start+2 <= end && source[start:start+2] == "no" {
		return 2
	}
	if start+3 <= end && source[start:start+3] == "inv" {
		return 3
	}
	return 0
}

func scanSetOperator(source string, start, end int) int {
	if start >= end {
		return start
	}
	if source[start] == '+' || source[start] == '^' || source[start] == '-' {
		if start+1 < end && source[start+1] == '=' {
			return start + 2
		}
		// Retain an incomplete assignment operator for editor completion.
		if start+1 == end || isSpace(source[start+1]) {
			return start + 1
		}
		return start
	}
	if source[start] == '&' {
		switch {
		case start+4 <= end && source[start:start+4] == "&vim":
			return start + 4
		case start+3 <= end && source[start:start+3] == "&vi":
			return start + 3
		case start+2 <= end && source[start:start+2] == "&v" && (start+2 == end || isSpace(source[start+2])):
			return start + 2
		default:
			return start + 1
		}
	}
	switch source[start] {
	case '?', '!', '&', '<', '=', ':':
		return start + 1
	default:
		return start
	}
}

func setAssignmentOperator(source string, start, end int) bool {
	if start >= end {
		return false
	}
	if source[start] == '=' || source[start] == ':' {
		return end == start+1
	}
	return end == start+2 && (source[start] == '+' || source[start] == '^' || source[start] == '-') && source[start+1] == '='
}

func setVim9WhitespaceError(source string, start, end, limit int) bool {
	if start >= limit || end <= start {
		return false
	}
	if source[start] == '=' || end == start+2 && source[start+1] == '=' &&
		(source[start] == '+' || source[start] == '-' || source[start] == '^') {
		return true
	}
	return source[start] == '!' || source[start] == '&'
}

func scanSetOpaqueItem(source string, start, end int) int {
	return scanSetItemEnd(source, start, end)
}

func scanSetItemEnd(source string, start, end int) int {
	position := start
	for position < end {
		character := source[position]
		if character == '\\' {
			if position+1 < end {
				position += 2
				continue
			}
			position++
			continue
		}
		if character == 0x16 {
			position++
			if position < end && source[position] >= utf8.RuneSelf {
				// Session files may quote each UTF-8 byte independently. Do not
				// reinterpret a quoted trail byte as an encoded lead byte and
				// accidentally consume a following backslash escape.
				position = nextEncodedCharacter(source, position, end)
			} else if position < end && (source[position] == '|' || source[position] == '"' || source[position] == '#') {
				position++
			}
			continue
		}
		if character >= utf8.RuneSelf {
			next := nextEncodedCharacter(source, position, end)
			if next > position+1 {
				position = next
				continue
			}
			if character >= 0x81 && position+1 < end {
				position += 2
				continue
			}
		}
		if isSpace(character) {
			break
		}
		position++
	}
	return position
}
