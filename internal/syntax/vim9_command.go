package syntax

import (
	"strings"
	"unicode/utf8"

	"github.com/chemzqm/vimls-go/internal/vimdata"
)

// scanVim9CommandArgument follows Vim9 command consumers.  Vim9 comments,
// logical operators, assignments and automatic continuation are not legacy
// Ex argument rules and stay in this parser.
func scanVim9CommandArgument(source string, start, end int, metadata vimdata.Command, parsed *Command) (int, Span, Span, *expressionBoundary) {
	if globalCommand(metadata.Name) {
		argumentEnd := scanGlobalCommandArgument(source, start, end)
		return argumentEnd, Span{}, Span{}, nil
	}
	if substituteCommand(metadata.Name) {
		return scanSubstituteArgument(source, start, end, Vim9, parsed)
	}
	if metadata.Name == "highlight" {
		return scanHighlightArgument(source, start, end, Vim9, parsed)
	}
	if metadata.Name == "catch" {
		argumentEnd, separator, comment := scanCatchArgument(source, start, end)
		return argumentEnd, separator, comment, nil
	}
	if metadata.Name == "syntax" {
		return scanSyntaxArgument(source, start, end, Vim9, parsed, metadata)
	}
	if metadata.Name == "set" || metadata.Name == "setlocal" || metadata.Name == "setglobal" {
		node, argumentEnd, separator, comment, diagnostics := parseSetCommand(source, start, end, Vim9)
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
		argumentEnd, separator, comment := scanEscapedExArgument(source, start, end, Vim9, metadata)
		return argumentEnd, separator, comment, nil
	}
	if metadata.Name == "source" {
		argumentEnd, separator, comment := scanSourceArgumentEnd(source, start, end, Vim9)
		return argumentEnd, separator, comment, nil
	}
	if metadata.Name == "" && metadata.Flags&vimdata.ExpressionArgument != 0 {
		return scanVim9CommandExpression(source, start, end, metadata)
	}
	if (metadata.Name == "put" || metadata.Name == "iput") && skipExpressionSpace(source, start) < end && source[skipExpressionSpace(source, start)] == '=' {
		return scanVim9PutExpression(source, start, end)
	}
	if allowsMultipleExpressionArguments(metadata.Name) {
		argumentEnd, separator, comment := scanVim9ExpressionList(source, start, end, parsed)
		return argumentEnd, separator, comment, nil
	}
	if vim9OneExpressionCommand(metadata.Name) {
		return scanVim9Expression(source, start, end)
	}
	var argumentEnd int
	var separator, comment Span
	var boundaryExpression *expressionBoundary
	switch metadata.Name {
	case "let", "var", "const", "final":
		argumentEnd, separator, comment, boundaryExpression = scanVim9Declaration(source, start, end, metadata)
	case "for":
		argumentEnd, separator, comment, boundaryExpression = scanVim9For(source, start, end, metadata)
	default:
		argumentEnd, separator, comment = scanVim9OpaqueArgument(source, start, end, metadata)
	}
	return argumentEnd, separator, comment, boundaryExpression
}

func scanVim9PutExpression(source string, start, end int) (int, Span, Span, *expressionBoundary) {
	argumentStart := skipExpressionSpace(source, start)
	if argumentStart >= end || source[argumentStart] != '=' {
		argumentEnd, separator, comment := scanVim9OpaqueArgument(source, start, end, vimdata.Command{})
		return argumentEnd, separator, comment, nil
	}
	right := skipExpressionSpace(source, argumentStart+1)
	// An empty RHS, or a bar immediately after '=', belongs to this malformed
	// put command.  Do not expose a separator or let the same-line tail become
	// another command; details will produce the normal expression diagnostic.
	if right >= end || source[right] == '|' {
		return trimSpaceEnd(source, start, end), Span{}, Span{}, nil
	}
	argumentEnd, separator, comment, boundary := scanVim9Expression(source, right, end)
	rhs := Span{Start: right, End: trimSpaceEnd(source, right, argumentEnd)}
	if boundary == nil || boundary.argument != rhs || boundary.argument.End <= boundary.argument.Start || boundary.expression == nil {
		return trimSpaceEnd(source, start, argumentEnd), separator, comment, nil
	}
	return trimSpaceEnd(source, start, argumentEnd), separator, comment, boundary
}

func promoteVim9ContinuationExpression(file *File, index int) bool {
	if file == nil || index < 0 || index >= len(file.Commands) {
		return false
	}
	command := &file.Commands[index]
	if command.Dialect != Vim9 || command.Kind == CommandExpression || command.Name.Start >= command.Name.End ||
		command.Argument.Start != command.Argument.End || command.Range.Start < command.Range.End ||
		command.Bang.Start < command.Bang.End || len(command.Modifiers) != 0 {
		return false
	}
	name := command.Name
	command.Kind = CommandExpression
	command.TypedName = ""
	command.Canonical = ""
	command.Argument = name
	command.Name = Span{}
	command.boundaryExpression = nil
	command.Expressions = nil
	command.expressionsParsed = false
	if command.logical != nil {
		logical := &command.logical.command
		logicalName := logical.Name
		logical.Kind = CommandExpression
		logical.TypedName = ""
		logical.Canonical = ""
		logical.Argument = logicalName
		logical.Name = Span{}
		logical.boundaryExpression = nil
		logical.Expressions = nil
		logical.expressionsParsed = false
	}
	for tokenIndex := len(file.Tokens) - 1; tokenIndex >= 0; tokenIndex-- {
		token := &file.Tokens[tokenIndex]
		if token.Kind == TokenCommand && token.Span == name {
			token.Kind = TokenArgument
			break
		}
	}
	return true
}

func vim9OneExpressionCommand(name string) bool {
	switch name {
	case "if", "elseif", "while", "return", "throw", "call", "eval", "defer",
		"caddexpr", "cexpr", "cgetexpr", "laddexpr", "lexpr", "lgetexpr":
		return true
	default:
		return false
	}
}

func scanVim9Expression(source string, start, end int) (int, Span, Span, *expressionBoundary) {
	commentStart := findVim9Comment(source, start, end)
	expressionEnd := end
	comment := Span{}
	if commentStart >= 0 {
		expressionEnd = commentStart
		comment = Span{Start: commentStart, End: end}
	}
	if start >= expressionEnd {
		return start, Span{}, comment, nil
	}
	if source[start] == '|' {
		return start, Span{Start: start, End: start + 1}, Span{}, nil
	}

	expression, diagnostics, consumed := parseExpressionPrefix(source[start:expressionEnd], start, Vim9)
	if len(diagnostics) > 0 {
		// Stop recovering within this malformed logical line.  Vim9 automatic
		// continuation is assembled before this result is final, and parsing
		// restarts normally at the next physical statement boundary.
		argumentEnd := trimSpaceEnd(source, start, expressionEnd)
		argument := Span{Start: start, End: argumentEnd}
		return argumentEnd, Span{}, comment, newExpressionBoundary(argument, expression, diagnostics, consumed)
	}
	position := start + consumed
	if position < start || position > expressionEnd {
		position = expressionEnd
	}
	position = skipSpace(source, position, expressionEnd)
	if position >= expressionEnd {
		argumentEnd := trimSpaceEnd(source, start, expressionEnd)
		argument := Span{Start: start, End: argumentEnd}
		return argumentEnd, Span{}, comment, newExpressionBoundary(argument, expression, diagnostics, consumed)
	}
	if source[position] == '|' {
		argumentEnd := trimSpaceEnd(source, start, position)
		argument := Span{Start: start, End: argumentEnd}
		return argumentEnd, Span{Start: position, End: position + 1}, Span{}, newExpressionBoundary(argument, expression, diagnostics, consumed)
	}
	argumentEnd := trimSpaceEnd(source, start, expressionEnd)
	argument := Span{Start: start, End: argumentEnd}
	return argumentEnd, Span{}, comment, newExpressionBoundary(argument, expression, diagnostics, consumed)
}

func scanVim9ExpressionList(source string, start, end int, command *Command) (int, Span, Span) {
	if command != nil {
		command.Expressions = nil
		command.expressionsParsed = false
	}
	var expressions []*Expression
	commentStart := findVim9Comment(source, start, end)
	expressionEnd := end
	comment := Span{}
	if commentStart >= 0 {
		expressionEnd = commentStart
		comment = Span{Start: commentStart, End: end}
	}
	position := start
	for position < expressionEnd {
		position = skipSpace(source, position, expressionEnd)
		if position >= expressionEnd {
			if command != nil {
				command.Expressions = expressions
				command.expressionsParsed = true
			}
			return trimSpaceEnd(source, start, expressionEnd), Span{}, comment
		}
		if source[position] == '|' {
			if command != nil {
				command.Expressions = expressions
				command.expressionsParsed = true
			}
			return trimSpaceEnd(source, start, position), Span{Start: position, End: position + 1}, Span{}
		}
		expression, diagnostics, consumed := parseExpressionPrefix(source[position:expressionEnd], position, Vim9)
		if len(diagnostics) > 0 {
			return trimSpaceEnd(source, start, expressionEnd), Span{}, comment
		}
		if consumed <= 0 {
			return trimSpaceEnd(source, start, expressionEnd), Span{}, comment
		}
		next := position + consumed
		if next <= position || next > expressionEnd {
			return trimSpaceEnd(source, start, expressionEnd), Span{}, comment
		}
		expressions = append(expressions, expression)
		position = next
	}
	if command != nil {
		command.Expressions = expressions
		command.expressionsParsed = true
	}
	return trimSpaceEnd(source, start, expressionEnd), Span{}, comment
}

func scanVim9CommandExpression(source string, start, end int, command vimdata.Command) (int, Span, Span, *expressionBoundary) {
	rawEnd, _, _ := scanVim9OpaqueArgument(source, start, end, command)
	assignment := findAssignment(source[start:rawEnd])
	if assignment.Start < 0 {
		return scanVim9Expression(source, start, end)
	}
	right := skipSpace(source, start+assignment.End, end)
	if right < end && source[right] == '|' {
		return trimSpaceEnd(source, start, end), Span{}, Span{}, nil
	}
	argumentEnd, expressionSeparator, expressionComment, boundary := scanVim9Expression(source, right, end)
	rhs := Span{Start: right, End: trimSpaceEnd(source, right, argumentEnd)}
	if boundary == nil || boundary.argument != rhs || boundary.argument.End <= boundary.argument.Start || boundary.expression == nil || len(boundary.diagnostics) > 0 {
		return trimSpaceEnd(source, start, argumentEnd), expressionSeparator, expressionComment, nil
	}
	return trimSpaceEnd(source, start, argumentEnd), expressionSeparator, expressionComment, boundary
}

func scanVim9Declaration(source string, start, end int, command vimdata.Command) (int, Span, Span, *expressionBoundary) {
	rawEnd, separator, comment := scanVim9OpaqueArgument(source, start, end, command)
	assignment := findAssignment(source[start:rawEnd])
	if assignment.Start < 0 || assignment.Start+start+3 <= end && source[assignment.Start+start:assignment.Start+start+3] == "=<<" {
		return rawEnd, separator, comment, nil
	}
	right := skipSpace(source, start+assignment.End, end)
	if right < end && source[right] == '|' {
		return trimSpaceEnd(source, start, end), Span{}, Span{}, nil
	}
	argumentEnd, expressionSeparator, expressionComment, boundary := scanVim9Expression(source, right, end)
	if boundary == nil || boundary.argument != (Span{Start: right, End: trimSpaceEnd(source, right, argumentEnd)}) || boundary.expression == nil {
		return trimSpaceEnd(source, start, argumentEnd), expressionSeparator, expressionComment, nil
	}
	return trimSpaceEnd(source, start, argumentEnd), expressionSeparator, expressionComment, boundary
}

func scanVim9For(source string, start, end int, command vimdata.Command) (int, Span, Span, *expressionBoundary) {
	rawEnd, separator, comment := scanVim9OpaqueArgument(source, start, end, command)
	in := findTopLevelKeyword(source, start, rawEnd, "in")
	if in < 0 {
		return rawEnd, separator, comment, nil
	}
	right := skipExpressionSpace(source, in+2)
	argumentEnd, expressionSeparator, expressionComment, boundary := scanVim9Expression(source, right, end)
	if boundary == nil || len(boundary.diagnostics) > 0 || boundary.argument != (Span{Start: right, End: trimSpaceEnd(source, right, argumentEnd)}) || boundary.expression == nil {
		return trimSpaceEnd(source, start, argumentEnd), expressionSeparator, expressionComment, nil
	}
	return trimSpaceEnd(source, start, argumentEnd), expressionSeparator, expressionComment, boundary
}

func scanVim9OpaqueArgument(source string, start, end int, command vimdata.Command) (int, Span, Span) {
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
		if character == 0x16 { // CTRL-V protects the following encoded character.
			if index+1 < end {
				index = nextEncodedCharacter(source, index+1, end) - 1
			}
			continue
		}
		if character == '\\' {
			index++
			continue
		}
		if character == '\'' && isDigitSeparator(source, index) {
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			continue
		}
		if character == '$' && index+1 < end && (source[index+1] == '\'' || source[index+1] == '"') {
			index = scanInterpolatedStringEnd(source, index+2, source[index+1]) - 1
			continue
		}
		switch character {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case '#':
			if isVim9OpaqueCommentStart(source, index, start, end) {
				if newline := strings.IndexByte(source[index:], '\n'); newline >= 0 {
					// The comment body is not Ex syntax.  Skip it in one jump so
					// braces and bars in the body cannot affect the argument's
					// nesting or command boundary; continue with the next line.
					index += newline
					continue
				}
				return trimSpaceEnd(source, start, index), Span{}, Span{Start: index, End: end}
			}
		case '|':
			if depth == 0 && command.Flags&(vimdata.AllowBar|vimdata.ExpressionArgument) != 0 && (index+1 >= end || source[index+1] != '|') && (index == start || source[index-1] != '|') {
				return trimSpaceEnd(source, start, index), Span{Start: index, End: index + 1}, Span{}
			}
		}
	}
	return trimSpaceEnd(source, start, end), Span{}, Span{}
}

// isVim9OpaqueCommentStart is the local equivalent of Vim's ends_excmd2()
// check for a generic Ex argument.  The scanner has already ruled out quotes,
// interpolated strings and escapes, so this must remain an O(1) predicate. In
// particular, the comment boundary is relative to the original argument start,
// not the current hash position.
func isVim9OpaqueCommentStart(source string, index, argumentStart, end int) bool {
	if source[index] != '#' || index+1 < end && source[index+1] == '{' && (index+2 >= end || source[index+2] != '{') {
		return false
	}
	return index == argumentStart || index > 0 && isSpace(source[index-1])
}

func findVim9Comment(source string, start, end int) int {
	quote := byte(0)
	for index := start; index < end; index++ {
		character := source[index]
		if quote != 0 {
			if character == '\\' && quote == '"' && index+1 < end {
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
		if character == '\'' || character == '"' {
			quote = character
			continue
		}
		if character == '$' && index+1 < end && (source[index+1] == '\'' || source[index+1] == '"') {
			index = scanInterpolatedStringEnd(source, index+2, source[index+1]) - 1
			continue
		}
		if character != '#' || index+1 < end && source[index+1] == '{' && (index+2 >= end || source[index+2] != '{') {
			continue
		}
		if index == start || isSpace(source[index-1]) {
			for lineEnd := index; lineEnd < end; lineEnd++ {
				if source[lineEnd] == '\n' {
					index = lineEnd
					goto nextByte
				}
			}
			return index
		}
	nextByte:
	}
	return -1
}
