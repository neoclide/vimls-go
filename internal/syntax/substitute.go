package syntax

// The substitute command is an Ex command whose first byte is itself a
// grammar token.  Keep this scanner separate from the ordinary opaque Ex
// consumer: a bar in a pattern or replacement is data, while a bar after the
// completed flags/count is a command separator.

func scanSubstituteArgument(source string, start, end int, dialect Dialect, command *Command) (int, Span, Span, *expressionBoundary) {
	node := &Substitute{}
	command.Substitute = node
	switch command.Canonical {
	case "smagic":
		node.Magic = SubstituteMagicOn
	case "snomagic":
		node.Magic = SubstituteMagicOff
	}
	if start >= end {
		return start, Span{}, Span{}, nil
	}
	if command.Canonical == "~" {
		return scanSubstituteTail(source, start, end, node, start)
	}

	first := source[start]
	if substituteRepeatByte(first) {
		return scanSubstituteRepeat(source, start, end, node)
	}
	if isExpressionLetter(first) {
		node.diagnostics = append(node.diagnostics, Diagnostic{Code: "vim/E146", Message: "regular expressions cannot be delimited by letters", Span: Span{Start: start, End: start + 1}})
		return end, Span{}, Span{}, substituteBoundary(node)
	}
	// check_global_and_subst() runs only after Vim has selected the new-pattern
	// form.  In particular repeat flags (and an invalid letter delimiter) must
	// not be reported as E1242 just because the command had whitespace.
	if dialect == Vim9 && command != nil && command.TypedName == "s" {
		if command.Name.End < start {
			node.diagnostics = append(node.diagnostics, Diagnostic{
				Code: "vim/E1242", Message: "no white space allowed before separator",
				Span: Span{Start: command.Name.End, End: start},
			})
			return end, Span{}, Span{}, substituteBoundary(node)
		}
		if command.Name.End == start && (first == ':' || first == '-' || first == '.') {
			node.diagnostics = append(node.diagnostics, Diagnostic{
				Code: "vim/E1241", Message: "separator not supported",
				Span: Span{Start: start, End: start + 1},
			})
			return end, Span{}, Span{}, substituteBoundary(node)
		}
	}
	if first == '\\' {
		return scanSubstitutePrevious(source, start, end, dialect, node, command)
	}

	// This is the new-pattern form.  Vim rejects alphabetic delimiters; the
	// exclusion above also preserves repeat forms and the special initial bar
	// and quote behavior.
	node.Delimiter = Span{Start: start, End: start + 1}
	patternStart := start + 1
	magic := uint8(globalMagicOn)
	if node.Magic == SubstituteMagicOff {
		magic = globalMagicNone
	}
	patternEnd := scanRegexpEndWithMagic(source, patternStart, end, first, magic)
	if patternEnd < 0 {
		node.Pattern = Span{Start: patternStart, End: end}
		node.Replacement = Span{Start: end, End: end}
		node.MissingPattern = true
		node.MissingReplacement = true
		return end, Span{}, Span{}, substituteBoundary(node)
	}
	node.Pattern = Span{Start: patternStart, End: patternEnd}
	node.PatternDelimiter = Span{Start: patternEnd, End: patternEnd + 1}

	replacementStart := patternEnd + 1
	replacementEnd := scanSubstituteReplacementEnd(source, replacementStart, end, first)
	if replacementEnd < 0 {
		node.Replacement = Span{Start: replacementStart, End: end}
		node.MissingReplacement = true
		if replacementStart+2 <= end && source[replacementStart] == '\\' && source[replacementStart+1] == '=' {
			parseSubstituteExpression(source, replacementStart, end, dialect, node, command)
		}
		return end, Span{}, Span{}, substituteBoundary(node)
	}
	node.Replacement = Span{Start: replacementStart, End: replacementEnd}
	node.ReplacementDelimiter = Span{Start: replacementEnd, End: replacementEnd + 1}
	if replacementStart+2 <= replacementEnd && source[replacementStart] == '\\' && source[replacementStart+1] == '=' {
		parseSubstituteExpression(source, replacementStart, replacementEnd, dialect, node, command)
		if len(node.diagnostics) > 0 {
			// A malformed expression owns the rest of this physical/logical line;
			// a bar in its tail is not a reliable Ex separator.
			return end, Span{}, Span{}, substituteBoundary(node)
		}
	}

	return scanSubstituteTail(source, replacementEnd+1, end, node, start)
}

func substituteRepeatByte(character byte) bool {
	return character >= '0' && character <= '9' || character == 'c' || character == 'e' ||
		character == 'g' || character == 'r' || character == 'i' || character == 'I' || character == 'p' ||
		character == '|' || character == '"'
}

func scanSubstitutePrevious(source string, start, end int, dialect Dialect, node *Substitute, command *Command) (int, Span, Span, *expressionBoundary) {
	if start+1 >= end || source[start+1] != '/' && source[start+1] != '?' && source[start+1] != '&' {
		node.PreviousPattern = Span{Start: start, End: minSpanEnd(start+2, end)}
		if dialect == Vim9 {
			node.InvalidVim9Backslash = true
			node.diagnostics = append(node.diagnostics, Diagnostic{Code: "vim/E1270", Message: "cannot use :s\\ in Vim9 script", Span: node.PreviousPattern})
		} else {
			node.diagnostics = append(node.diagnostics, Diagnostic{Code: "vim/E10", Message: "\\ should be followed by /, ? or &", Span: node.PreviousPattern})
		}
		return end, Span{}, Span{}, substituteBoundary(node)
	}
	node.PreviousPattern = Span{Start: start, End: start + 2}
	node.LegacyPrevious = true
	node.Delimiter = Span{Start: start + 1, End: start + 2}
	if dialect == Vim9 {
		node.InvalidVim9Backslash = true
		node.diagnostics = append(node.diagnostics, Diagnostic{Code: "vim/E1270", Message: "cannot use :s\\ in Vim9 script", Span: node.PreviousPattern})
		return end, Span{}, Span{}, substituteBoundary(node)
	}
	replacementStart := start + 2
	replacementEnd := scanSubstituteReplacementEnd(source, replacementStart, end, source[start+1])
	if replacementEnd < 0 {
		node.Replacement = Span{Start: replacementStart, End: end}
		node.MissingReplacement = true
		if replacementStart+2 <= end && source[replacementStart] == '\\' && source[replacementStart+1] == '=' {
			parseSubstituteExpression(source, replacementStart, end, dialect, node, command)
		}
		return end, Span{}, Span{}, substituteBoundary(node)
	}
	node.Replacement = Span{Start: replacementStart, End: replacementEnd}
	node.ReplacementDelimiter = Span{Start: replacementEnd, End: replacementEnd + 1}
	if replacementStart+2 <= replacementEnd && source[replacementStart] == '\\' && source[replacementStart+1] == '=' {
		parseSubstituteExpression(source, replacementStart, replacementEnd, dialect, node, command)
		if len(node.diagnostics) > 0 {
			return end, Span{}, Span{}, substituteBoundary(node)
		}
	}
	return scanSubstituteTail(source, replacementEnd+1, end, node, start)
}

func scanSubstituteRepeat(source string, start, end int, node *Substitute) (int, Span, Span, *expressionBoundary) {
	// A leading bar is exposed as the separator and is not a delimiter.  A
	// leading quote is a comment.  Other repeat bytes are parsed as flags/count.
	if source[start] == '|' {
		return start, Span{Start: start, End: start + 1}, Span{}, substituteBoundary(node)
	}
	return scanSubstituteTail(source, start, end, node, start)
}

func scanSubstituteReplacementEnd(source string, start, end int, delimiter byte) int {
	for position := start; position < end; {
		if source[position] == '\\' && position+1 < end {
			position += 2
			continue
		}
		if source[position] == delimiter {
			return position
		}
		position++
	}
	return -1
}

func scanSubstituteTail(source string, start, end int, node *Substitute, argumentStart int) (int, Span, Span, *expressionBoundary) {
	position := start
	flagsStart := -1
	if position < end && source[position] == '&' {
		flagsStart = position
		node.FlagBits |= SubstituteFlagKeepOptions
		position++
	}
	for position < end {
		bit, ok := substituteFlagBit(source[position])
		if !ok {
			break
		}
		if flagsStart < 0 {
			flagsStart = position
		}
		node.FlagBits |= bit
		position++
	}
	if flagsStart >= 0 {
		node.Flags = Span{Start: flagsStart, End: position}
	}
	countStart := skipSpace(source, position, end)
	if countStart < end && source[countStart] >= '0' && source[countStart] <= '9' {
		countEnd := countStart + 1
		for countEnd < end && source[countEnd] >= '0' && source[countEnd] <= '9' {
			countEnd++
		}
		node.Count = Span{Start: countStart, End: countEnd}
		position = skipSpace(source, countEnd, end)
	} else {
		position = countStart
	}

	tail := position
	if tail < end && source[tail] == '|' {
		if len(node.diagnostics) > 0 {
			return end, Span{}, Span{}, substituteBoundary(node)
		}
		return trimSpaceEnd(source, argumentStart, tail), Span{Start: tail, End: tail + 1}, Span{}, substituteBoundary(node)
	}
	if tail < end && source[tail] == '"' {
		return trimSpaceEnd(source, argumentStart, tail), Span{}, Span{Start: tail, End: end}, substituteBoundary(node)
	}
	if tail < end {
		// Unknown trailing bytes are owned by substitute.  In particular, do not
		// let a later bar turn a malformed command into a same-line command.
		node.diagnostics = append(node.diagnostics, Diagnostic{
			Code: "vim/E488", Message: "trailing characters", Span: Span{Start: tail, End: end},
		})
		return end, Span{}, Span{}, substituteBoundary(node)
	}
	return trimSpaceEnd(source, argumentStart, position), Span{}, Span{}, substituteBoundary(node)
}

func parseSubstituteExpression(source string, replacementStart, expressionEnd int, dialect Dialect, node *Substitute, command *Command) {
	if replacementStart+2 > expressionEnd || source[replacementStart] != '\\' || source[replacementStart+1] != '=' {
		return
	}
	node.ReplacementPrefix = Span{Start: replacementStart, End: replacementStart + 2}
	node.ExpressionSpan = Span{Start: replacementStart + 2, End: expressionEnd}
	node.ReplacementExpression = true
	expression, diagnostics := parseExpression(source[node.ExpressionSpan.Start:node.ExpressionSpan.End], node.ExpressionSpan.Start, dialect)
	node.Expression = expression
	node.diagnostics = append(node.diagnostics, diagnostics...)
	if expression != nil && command != nil {
		command.Expressions = append(command.Expressions, expression)
		command.expressionsParsed = true
	}
}

func substituteFlagBit(character byte) (SubstituteFlags, bool) {
	switch character {
	case 'g':
		return SubstituteFlagAll, true
	case 'c':
		return SubstituteFlagConfirm, true
	case 'n':
		return SubstituteFlagCount, true
	case 'e':
		return SubstituteFlagError, true
	case 'r':
		return SubstituteFlagLastPattern, true
	case 'p':
		return SubstituteFlagPrint, true
	case '#':
		return SubstituteFlagNumber, true
	case 'l':
		return SubstituteFlagList, true
	case 'i':
		return SubstituteFlagIgnoreCase, true
	case 'I':
		return SubstituteFlagMatchCase, true
	default:
		return 0, false
	}
}

func substituteBoundary(node *Substitute) *expressionBoundary {
	if node == nil || len(node.diagnostics) == 0 {
		return nil
	}
	return &expressionBoundary{diagnostics: node.diagnostics}
}

func minSpanEnd(end, limit int) int {
	if end < limit {
		return end
	}
	return limit
}
