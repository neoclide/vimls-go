package syntax

import (
	"strings"
	"unicode/utf8"
)

// scanHighlightArgument gives :highlight ownership of its Ex boundary before
// parsing the command payload.  Highlight values are not quoted at the Ex
// layer: a bar or a comment marker inside a single quoted value still ends
// the command, just as it does in Vim's separate_nextcmd().
func scanHighlightArgument(source string, start, end int, dialect Dialect, command *Command) (int, Span, Span, *expressionBoundary) {
	argumentEnd, separator, comment := scanHighlightBoundary(source, start, end, dialect)
	node, diagnostics := parseHighlight(source, start, argumentEnd)
	command.Highlight = node
	if len(diagnostics) == 0 {
		return argumentEnd, separator, comment, nil
	}
	// A structural error makes the rest of this physical/logical line
	// unreliable.  Keep the partial node, but suppress any apparent Ex tail so
	// the normal scanner resumes at the next physical line.
	return end, Span{}, Span{}, &expressionBoundary{
		argument:    Span{Start: start, End: end},
		diagnostics: diagnostics,
	}
}

func scanHighlightBoundary(source string, start, end int, dialect Dialect) (int, Span, Span) {
	for index := start; index < end; index++ {
		character := source[index]
		if character == 0x16 { // CTRL-V protects the following encoded byte.
			if index+1 < end {
				_, size := utf8.DecodeRuneInString(source[index+1 : end])
				if size < 1 {
					size = 1
				}
				index += size
			}
			continue
		}
		if character >= utf8.RuneSelf {
			next := nextEncodedCharacter(source, index, end)
			if next > index+1 {
				index = next - 1
				continue
			}
			// Keep the same invalid-byte lead/trail handling as the regular
			// escaped Ex scanner.  Source spans remain byte based.
			if character >= 0x81 && index+1 < end {
				index++
				continue
			}
		}
		escaped := index > start && source[index-1] == '\\'
		if character == '|' && !escaped {
			return trimSpaceEnd(source, start, index), Span{Start: index, End: index + 1}, Span{}
		}
		if escaped {
			continue
		}
		if dialect == Legacy && character == '"' {
			return trimSpaceEnd(source, start, index), Span{}, Span{Start: index, End: end}
		}
		if dialect == Vim9 && character == '#' && vim9HighlightComment(source, index, start, end) {
			return trimSpaceEnd(source, start, index), Span{}, Span{Start: index, End: end}
		}
	}
	return trimSpaceEnd(source, start, end), Span{}, Span{}
}

func vim9HighlightComment(source string, index, argumentStart, end int) bool {
	if index != argumentStart && (index == 0 || !isSpace(source[index-1])) {
		return false
	}
	// #{ starts a dictionary literal.  Match the existing Vim9 Ex boundary
	// rule, including its special #{{ spelling.
	return index+1 >= end || source[index+1] != '{' || index+2 < end && source[index+2] == '{'
}

type highlightToken struct {
	span Span
	text string
}

func nextHighlightToken(source string, position, end int) (highlightToken, int) {
	position = skipSpace(source, position, end)
	if position >= end {
		return highlightToken{}, position
	}
	start := position
	for position < end && !isSpace(source[position]) {
		position++
	}
	return highlightToken{span: Span{Start: start, End: position}, text: source[start:position]}, position
}

func highlightKeyword(token string, keyword string) bool {
	return len(token) > 0 && len(token) <= len(keyword) && keyword[:len(token)] == token
}

func parseHighlight(source string, start, end int) (*Highlight, []Diagnostic) {
	node := &Highlight{}
	position := skipSpace(source, start, end)
	if position >= end {
		node.Kind = HighlightList
		return node, nil
	}
	first, position := nextHighlightToken(source, position, end)
	if first.text == "default" || highlightKeyword(first.text, "default") {
		node.Default = first.span
		next, nextPosition := nextHighlightToken(source, position, end)
		if next.span.Start == next.span.End {
			// Vim's zero-length prefix check selects the link branch after a
			// standalone "default", which then reports E412.
			node.Kind = HighlightLink
			return node, []Diagnostic{{Code: "vim/E412", Message: "not enough arguments for :highlight link", Span: first.span}}
		}
		first, position = next, nextPosition
	}
	if highlightKeyword(first.text, "clear") {
		node.Kind = HighlightClear
		node.Operation = first.span
		group, _ := nextHighlightToken(source, position, end)
		if group.span.Start != group.span.End {
			node.Group = group.span
		}
		return node, nil
	}
	if highlightKeyword(first.text, "link") {
		node.Kind = HighlightLink
		node.Operation = first.span
		from, nextPosition := nextHighlightToken(source, position, end)
		if from.span.Start == from.span.End {
			return node, []Diagnostic{{Code: "vim/E412", Message: "not enough arguments for :highlight link", Span: first.span}}
		}
		node.Group = from.span
		to, nextPosition := nextHighlightToken(source, nextPosition, end)
		if to.span.Start == to.span.End {
			return node, []Diagnostic{{Code: "vim/E412", Message: "not enough arguments for :highlight link", Span: from.span}}
		}
		node.LinkTarget = to.span
		extra, _ := nextHighlightToken(source, nextPosition, end)
		if extra.span.Start != extra.span.End {
			return node, []Diagnostic{{Code: "vim/E413", Message: "too many arguments for :highlight link", Span: extra.span}}
		}
		return node, nil
	}

	node.Group = first.span
	next := skipSpace(source, position, end)
	if next >= end {
		node.Kind = HighlightQuery
		return node, nil
	}
	node.Kind = HighlightDefine
	diagnostics := parseHighlightAttributes(source, next, end, node)
	return node, diagnostics
}

func parseHighlightAttributes(source string, position, end int, node *Highlight) []Diagnostic {
	var diagnostics []Diagnostic
	for position < end {
		tokenStart := skipSpace(source, position, end)
		if tokenStart >= end {
			break
		}
		keyStart := tokenStart
		keyEnd := keyStart
		for keyEnd < end && !isSpace(source[keyEnd]) && source[keyEnd] != '=' {
			keyEnd++
		}
		if keyEnd == keyStart {
			// A leading '=' is a structural error.  Consume the rest as an
			// opaque key so the partial AST remains useful to clients.
			for keyEnd < end && !isSpace(source[keyEnd]) {
				keyEnd++
			}
			key := Span{Start: keyStart, End: keyEnd}
			node.Attributes = append(node.Attributes, HighlightAttribute{Key: key})
			diagnostics = append(diagnostics, Diagnostic{Code: "vim/E415", Message: "unexpected equal sign", Span: key})
			return diagnostics
		}
		key := Span{Start: keyStart, End: keyEnd}
		afterKey := keyEnd
		for afterKey < end && isSpace(source[afterKey]) {
			afterKey++
		}
		// Vim recognizes NONE before checking for '='.  Thus NONE=... first
		// clears the group and then reports E415 when the next iteration sees
		// the still-unconsumed equal sign.
		if strings.EqualFold(source[key.Start:key.End], "NONE") {
			node.Attributes = append(node.Attributes, HighlightAttribute{Key: key})
			position = afterKey
			continue
		}
		if afterKey >= end || source[afterKey] != '=' {
			node.Attributes = append(node.Attributes, HighlightAttribute{Key: key})
			diagnostics = append(diagnostics, Diagnostic{Code: "vim/E416", Message: "missing equal sign", Span: key})
			return diagnostics
		}
		equal := Span{Start: afterKey, End: afterKey + 1}
		valueStart := skipSpace(source, equal.End, end)
		attribute := HighlightAttribute{Key: key, Equal: equal}
		if valueStart >= end {
			node.Attributes = append(node.Attributes, attribute)
			diagnostics = append(diagnostics, Diagnostic{Code: "vim/E417", Message: "missing value", Span: equal})
			return diagnostics
		}
		if source[valueStart] == '\'' {
			valueEnd := valueStart + 1
			for valueEnd < end && source[valueEnd] != '\'' {
				valueEnd++
			}
			attribute.Quoted = true
			attribute.Value = Span{Start: valueStart + 1, End: valueEnd}
			if valueEnd >= end {
				node.Attributes = append(node.Attributes, attribute)
				diagnostics = append(diagnostics, Diagnostic{Code: "vim/E475", Message: "invalid argument: unmatched quote", Span: Span{Start: valueStart, End: end}})
				return diagnostics
			}
			if valueEnd == valueStart+1 {
				node.Attributes = append(node.Attributes, attribute)
				diagnostics = append(diagnostics, Diagnostic{Code: "vim/E417", Message: "missing value", Span: equal})
				return diagnostics
			}
			position = valueEnd + 1
			node.Attributes = append(node.Attributes, attribute)
			continue
		}
		valueEnd := valueStart
		for valueEnd < end && !isSpace(source[valueEnd]) {
			valueEnd++
		}
		attribute.Value = Span{Start: valueStart, End: valueEnd}
		node.Attributes = append(node.Attributes, attribute)
		position = valueEnd
	}
	return diagnostics
}
