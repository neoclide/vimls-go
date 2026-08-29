package syntax

import (
	"unicode/utf8"

	"github.com/chemzqm/vimls-go/internal/vimdata"
)

// scanLegacyCommandArgument follows the command-specific legacy Vim
// consumers.  It deliberately does not share comment or expression-boundary
// rules with Vim9 script.
func scanLegacyCommandArgument(source string, start, end int, metadata vimdata.Command, parsed *Command) (int, Span, Span, *expressionBoundary) {
	scriptVersion := uint8(1)
	if parsed != nil && parsed.ScriptVersion != 0 {
		scriptVersion = parsed.ScriptVersion
	}
	if globalCommand(metadata.Name) {
		argumentEnd := scanGlobalCommandArgument(source, start, end)
		return argumentEnd, Span{}, Span{}, nil
	}
	if findPatternCommand(metadata.Name) {
		return scanFindPatternArgument(source, start, end, Legacy, metadata)
	}
	if legacyTextCommand(metadata.Name) {
		argumentEnd, separator, comment := scanLegacyTextCommandArgument(source, start, end, metadata)
		return argumentEnd, separator, comment, nil
	}
	if substituteCommand(metadata.Name) {
		return scanSubstituteArgument(source, start, end, Legacy, parsed)
	}
	if metadata.Name == "highlight" {
		return scanHighlightArgument(source, start, end, Legacy, parsed)
	}
	if metadata.Name == "catch" {
		argumentEnd, separator, comment := scanCatchArgument(source, start, end)
		return argumentEnd, separator, comment, nil
	}
	if metadata.Name == "syntax" {
		return scanSyntaxArgument(source, start, end, Legacy, parsed, metadata)
	}
	if metadata.Name == "set" || metadata.Name == "setlocal" || metadata.Name == "setglobal" {
		node, argumentEnd, separator, comment, diagnostics := parseSetCommand(source, start, end, Legacy)
		parsed.Set = node
		if len(diagnostics) > 0 {
			parsed.boundaryExpression = &expressionBoundary{argument: Span{Start: start, End: end}, diagnostics: diagnostics}
			return argumentEnd, Span{}, Span{}, parsed.boundaryExpression
		}
		return argumentEnd, separator, comment, nil
	}
	if isMappingCommand(metadata.Name) {
		argumentEnd, separator, comment := scanMappingArgumentEnd(source, start, end)
		return argumentEnd, separator, comment, nil
	}
	if usesEscapedExArgument(metadata.Name) {
		argumentEnd, separator, comment := scanEscapedExArgument(source, start, end, Legacy, metadata)
		return argumentEnd, separator, comment, nil
	}
	if metadata.Name == "source" {
		argumentEnd, separator, comment := scanSourceArgumentEnd(source, start, end, Legacy)
		return argumentEnd, separator, comment, nil
	}
	if metadata.Name == "put" || metadata.Name == "iput" {
		argumentStart := skipSpace(source, start, end)
		if argumentStart < end && source[argumentStart] == '=' {
			return scanLegacyPutExpression(source, argumentStart, end, parsed)
		}
	}
	if allowsMultipleExpressionArguments(metadata.Name) {
		argumentEnd, separator, comment := scanLegacyExpressionList(source, start, end, parsed)
		return argumentEnd, separator, comment, nil
	}
	if legacyOneExpressionCommand(metadata.Name) {
		return scanLegacyExpression(source, start, end, scriptVersion)
	}
	var argumentEnd int
	var separator, comment Span
	var boundaryExpression *expressionBoundary
	switch metadata.Name {
	case "let", "var", "const", "final":
		argumentEnd, separator, comment, boundaryExpression = scanLegacyDeclaration(source, start, end, metadata, scriptVersion)
	case "for":
		argumentEnd, separator, comment, boundaryExpression = scanLegacyFor(source, start, end, metadata, scriptVersion)
	default:
		argumentEnd, separator, comment = scanLegacyOpaqueArgument(source, start, end, metadata)
	}
	return argumentEnd, separator, comment, boundaryExpression
}

// scanLegacyPutExpression models the Ex preprocessing applied to the
// expression register form of :put and :iput.  In particular, Ex finds bars
// and double quotes before Vim's expression lexer sees them; only a single
// backslash immediately before either delimiter protects it.  Protected
// delimiters lose that one backslash in the temporary expression source.
func scanLegacyPutExpression(source string, start, end int, command *Command) (int, Span, Span, *expressionBoundary) {
	right := skipExpressionSpace(source, start+1)
	if right >= end {
		return trimSpaceEnd(source, start, end), Span{}, Span{}, nil
	}

	delimiter := -1
	for index := right; index < end; index++ {
		if source[index] != '|' && source[index] != '"' {
			continue
		}
		if index > right && source[index-1] == '\\' {
			continue
		}
		delimiter = index
		break
	}

	parseEnd := end
	if delimiter >= 0 {
		parseEnd = delimiter
	}
	rawEnd := trimSpaceEnd(source, right, parseEnd)
	var separator, comment Span
	if delimiter >= 0 {
		if source[delimiter] == '|' {
			separator = Span{Start: delimiter, End: delimiter + 1}
		} else {
			comment = Span{Start: delimiter, End: end}
		}
	}

	boundary := scanLegacyPutRHS(source, right, rawEnd, command.ScriptVersion)
	if boundary.expression == nil {
		return trimSpaceEnd(source, start, parseEnd), separator, comment, nil
	}
	if len(boundary.diagnostics) == 0 {
		command.Expressions = append(command.Expressions, boundary.expression)
		command.expressionsParsed = true
		return trimSpaceEnd(source, start, parseEnd), separator, comment, nil
	}
	if delimiter >= 0 {
		// A delimiter in an incomplete expression is not a trustworthy command
		// separator.  Keep the partial expression/diagnostics, but make the
		// remainder of this physical line opaque so recovery starts next line.
		return trimSpaceEnd(source, start, end), Span{}, Span{}, &expressionBoundary{
			argument: boundary.argument, expression: boundary.expression, diagnostics: boundary.diagnostics,
		}
	}
	return trimSpaceEnd(source, start, parseEnd), separator, comment, &expressionBoundary{
		argument: boundary.argument, expression: boundary.expression, diagnostics: boundary.diagnostics,
	}
}

func scanLegacyPutRHS(source string, start, end int, scriptVersion uint8) expressionBoundary {
	if start >= end {
		return expressionBoundary{}
	}
	hasEscapedDelimiter := false
	for index := start; index+1 < end; index++ {
		if source[index] == '\\' && (source[index+1] == '|' || source[index+1] == '"') {
			hasEscapedDelimiter = true
			break
		}
	}
	if !hasEscapedDelimiter {
		argument := Span{Start: start, End: end}
		expression, diagnostics, consumed := parseExpressionPrefixWithVersion(source[start:end], start, Legacy, scriptVersion)
		diagnostics = appendTrailingExpressionDiagnostic(diagnostics, start, consumed, end-start)
		if legacyPutHasUnclosedQuote(source[start:end]) {
			diagnostics = append(diagnostics, Diagnostic{
				Code: "vimls/missing-delimiter", Message: "missing string delimiter",
				Span: Span{Start: end, End: end},
			})
		}
		return expressionBoundary{argument: argument, expression: expression, diagnostics: diagnostics}
	}

	normalized := make([]byte, 0, end-start)
	view := logicalView{Source: Span{Start: start, End: end}, identity: false}
	for index := start; index < end; index++ {
		sourceStart := index
		if source[index] == '\\' && index+1 < end && (source[index+1] == '|' || source[index+1] == '"') {
			index++
		}
		logicalStart := len(normalized)
		normalized = append(normalized, source[index])
		view.appendSegment(
			Span{Start: logicalStart, End: logicalStart + 1},
			Span{Start: sourceStart, End: index + 1},
		)
	}
	view.Text = string(normalized)
	expression, diagnostics, consumed := parseExpressionPrefixWithVersion(view.Text, 0, Legacy, scriptVersion)
	diagnostics = appendTrailingExpressionDiagnostic(diagnostics, 0, consumed, len(view.Text))
	if legacyPutHasUnclosedQuote(view.Text) {
		diagnostics = append(diagnostics, Diagnostic{
			Code: "vimls/missing-delimiter", Message: "missing string delimiter",
			Span: Span{Start: len(view.Text), End: len(view.Text)},
		})
	}
	mapper := logicalSpanMapper{
		view:        &view,
		source:      source,
		expressions: make(map[*Expression]bool),
		types:       make(map[*Type]bool),
		files:       make(map[*File]bool),
		lists:       make(map[*CommandList]bool),
	}
	mapper.expression(expression)
	for index := range diagnostics {
		diagnostics[index].Span = view.mapSpan(diagnostics[index].Span)
	}
	return expressionBoundary{
		argument:    Span{Start: start, End: end},
		expression:  expression,
		diagnostics: diagnostics,
	}
}

func legacyPutHasUnclosedQuote(source string) bool {
	var quote byte
	for index := 0; index < len(source); index++ {
		character := source[index]
		if quote != 0 {
			if quote == '"' && character == '\\' && index+1 < len(source) {
				index++
				continue
			}
			if character == quote {
				if quote == '\'' && index+1 < len(source) && source[index+1] == '\'' {
					index++
					continue
				}
				quote = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
		}
	}
	return quote != 0
}

func scanLegacyTextCommandArgument(source string, start, end int, command vimdata.Command) (int, Span, Span) {
	if start < end && source[start] == '|' {
		// Vim does not treat this bar as an Ex separator. Since 9.1.0574 the
		// bytes after it are the first literal input line; on older supported
		// versions they are still owned by this command and must not become a
		// second command.
		return end, Span{}, Span{}
	}
	command.Flags &^= vimdata.AllowBar
	return scanLegacyOpaqueArgument(source, start, end, command)
}

func legacyOneExpressionCommand(name string) bool {
	switch name {
	case "if", "elseif", "while", "return", "throw", "call", "eval", "defer",
		"caddexpr", "cexpr", "cgetexpr", "laddexpr", "lexpr", "lgetexpr":
		return true
	default:
		return false
	}
}

func scanLegacyExpression(source string, start, end int, scriptVersion uint8) (int, Span, Span, *expressionBoundary) {
	if start >= end || source[start] == '|' {
		if start < end {
			return start, Span{Start: start, End: start + 1}, Span{}, nil
		}
		return start, Span{}, Span{}, nil
	}

	expression, diagnostics, consumed := parseExpressionPrefixWithVersion(source[start:end], start, Legacy, scriptVersion)
	if len(diagnostics) > 0 {
		// Once this logical line is known to be malformed, do not guess that a
		// later bar starts another command.  The next physical line is a much
		// stronger recovery boundary for an editor buffer.
		argumentEnd := trimSpaceEnd(source, start, end)
		argument := Span{Start: start, End: argumentEnd}
		return argumentEnd, Span{}, Span{}, newExpressionBoundary(argument, expression, diagnostics, consumed)
	}
	position := start + consumed
	if position < start || position > end {
		position = end
	}
	position = skipSpace(source, position, end)
	if position >= end {
		argumentEnd := trimSpaceEnd(source, start, end)
		argument := Span{Start: start, End: argumentEnd}
		return argumentEnd, Span{}, Span{}, newExpressionBoundary(argument, expression, diagnostics, consumed)
	}
	if source[position] == '|' {
		argumentEnd := trimSpaceEnd(source, start, position)
		argument := Span{Start: start, End: argumentEnd}
		return argumentEnd, Span{Start: position, End: position + 1}, Span{}, newExpressionBoundary(argument, expression, diagnostics, consumed)
	}
	if source[position] == '"' {
		argumentEnd := trimSpaceEnd(source, start, position)
		argument := Span{Start: start, End: argumentEnd}
		return argumentEnd, Span{}, Span{Start: position, End: end}, newExpressionBoundary(argument, expression, diagnostics, consumed)
	}
	// Vim only considers a following command when the first unconsumed byte
	// is itself a command separator.  Invalid trailing text before a later bar
	// remains part of this command.
	argumentEnd := trimSpaceEnd(source, start, end)
	argument := Span{Start: start, End: argumentEnd}
	return argumentEnd, Span{}, Span{}, newExpressionBoundary(argument, expression, diagnostics, consumed)
}

func scanLegacyExpressionList(source string, start, end int, command *Command) (int, Span, Span) {
	if command != nil {
		command.Expressions = nil
		command.expressionsParsed = false
	}
	var expressions []*Expression
	scriptVersion := uint8(1)
	if command != nil && command.ScriptVersion != 0 {
		scriptVersion = command.ScriptVersion
	}
	position := start
	for position < end {
		position = skipSpace(source, position, end)
		if position >= end {
			if command != nil {
				command.Expressions = expressions
				command.expressionsParsed = true
			}
			return trimSpaceEnd(source, start, end), Span{}, Span{}
		}
		if source[position] == '|' {
			if command != nil {
				command.Expressions = expressions
				command.expressionsParsed = true
			}
			return trimSpaceEnd(source, start, position), Span{Start: position, End: position + 1}, Span{}
		}
		expression, diagnostics, consumed := parseExpressionPrefixWithVersion(source[position:end], position, Legacy, scriptVersion)
		if len(diagnostics) > 0 {
			return trimSpaceEnd(source, start, end), Span{}, Span{}
		}
		if consumed <= 0 {
			return trimSpaceEnd(source, start, end), Span{}, Span{}
		}
		next := position + consumed
		if next <= position || next > end {
			return trimSpaceEnd(source, start, end), Span{}, Span{}
		}
		expressions = append(expressions, expression)
		position = next
	}
	if command != nil {
		command.Expressions = expressions
		command.expressionsParsed = true
	}
	return trimSpaceEnd(source, start, end), Span{}, Span{}
}

func scanLegacyDeclaration(source string, start, end int, command vimdata.Command, scriptVersion uint8) (int, Span, Span, *expressionBoundary) {
	rawEnd, separator, comment := scanLegacyOpaqueArgument(source, start, end, command)
	assignment := findAssignment(source[start:rawEnd])
	if assignment.Start < 0 || assignment.Start+start+3 <= end && source[assignment.Start+start:assignment.Start+start+3] == "=<<" {
		return rawEnd, separator, comment, nil
	}
	right := skipSpace(source, start+assignment.End, end)
	if right < end && source[right] == '|' {
		return trimSpaceEnd(source, start, end), Span{}, Span{}, nil
	}
	argumentEnd, expressionSeparator, expressionComment, boundary := scanLegacyExpression(source, right, end, scriptVersion)
	if boundary == nil || len(boundary.diagnostics) > 0 || boundary.argument != (Span{Start: right, End: trimSpaceEnd(source, right, argumentEnd)}) || boundary.expression == nil {
		return trimSpaceEnd(source, start, argumentEnd), expressionSeparator, expressionComment, nil
	}
	return trimSpaceEnd(source, start, argumentEnd), expressionSeparator, expressionComment, boundary
}

func scanLegacyFor(source string, start, end int, command vimdata.Command, scriptVersion uint8) (int, Span, Span, *expressionBoundary) {
	rawEnd, separator, comment := scanLegacyOpaqueArgument(source, start, end, command)
	in := findTopLevelKeyword(source, start, rawEnd, "in")
	if in < 0 {
		return rawEnd, separator, comment, nil
	}
	right := skipExpressionSpace(source, in+2)
	argumentEnd, expressionSeparator, expressionComment, boundary := scanLegacyExpression(source, right, end, scriptVersion)
	if boundary == nil || len(boundary.diagnostics) > 0 || boundary.argument != (Span{Start: right, End: trimSpaceEnd(source, right, argumentEnd)}) || boundary.expression == nil {
		return trimSpaceEnd(source, start, argumentEnd), expressionSeparator, expressionComment, nil
	}
	return trimSpaceEnd(source, start, argumentEnd), expressionSeparator, expressionComment, boundary
}

func scanLegacyOpaqueArgument(source string, start, end int, command vimdata.Command) (int, Span, Span) {
	depth := 0
	quote := byte(0)
	for index := start; index < end; index++ {
		character := source[index]
		if quote != 0 {
			if character == '\\' && quote == '"' {
				index++
				continue
			}
			if character == quote {
				if quote == '\'' && index+1 < end && source[index+1] == '\'' {
					index++
					continue
				}
				quote = 0
			}
			continue
		}
		if command.Flags&vimdata.ExpressionArgument != 0 && character == '@' && index+1 < end && !isExpressionSpace(source[index+1]) {
			_, size := utf8.DecodeRuneInString(source[index+1:])
			index += size
			continue
		}
		if character == '\\' {
			index++
			continue
		}
		if character == '\'' && isDigitSeparator(source, index) {
			continue
		}
		if character == '\'' || character == '"' && !isCommentStart(source, index, start, end, Legacy, command) {
			quote = character
			continue
		}
		switch character {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case '"':
			if depth == 0 && isCommentStart(source, index, start, end, Legacy, command) {
				return trimSpaceEnd(source, start, index), Span{}, Span{Start: index, End: end}
			}
		case '|':
			if depth == 0 && command.Flags&(vimdata.AllowBar|vimdata.ExpressionArgument) != 0 {
				return trimSpaceEnd(source, start, index), Span{Start: index, End: index + 1}, Span{}
			}
		}
	}
	return trimSpaceEnd(source, start, end), Span{}, Span{}
}
