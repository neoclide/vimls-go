package syntax

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func parseFunctionSignature(file *File, command *Command) {
	if command.Argument.Start >= command.Argument.End {
		return
	}
	source := file.Text(command.Argument)
	if command.Dialect == Vim9 {
		source = maskVim9Comments(source)
	}
	offset := skipSyntaxSpace(source, 0, len(source))
	nameStart := offset
	for offset < len(source) {
		r, size := utf8.DecodeRuneInString(source[offset:])
		if r == '(' || r == '<' || unicode.IsSpace(r) {
			break
		}
		offset += size
	}
	if offset == nameStart {
		return
	}
	function := &Function{Name: Span{Start: command.Argument.Start + nameStart, End: command.Argument.Start + offset}}
	beforeSpace := offset
	offset = skipSyntaxSpace(source, offset, len(source))
	vim9Signature := command.Dialect == Vim9 || command.Canonical == "def"
	if vim9Signature && offset > beforeSpace && offset < len(source) && source[offset] == '(' {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1068", Message: "no white space allowed before function arguments",
			Span: Span{Start: command.Argument.Start + beforeSpace, End: command.Argument.Start + offset},
		})
	}
	if offset < len(source) && source[offset] == '<' {
		end := findMatching(source, offset, '<', '>')
		if end < 0 {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vimls/missing-generic-end", Message: "expected > after generic type parameters", Span: Span{Start: command.Argument.Start + offset, End: command.Argument.End}})
			command.Function = function
			return
		}
		for _, part := range splitTopLevel(source, offset+1, end, ',') {
			start := skipSyntaxSpace(source, part.Start, part.End)
			finish := trimSyntaxSpaceEnd(source, start, part.End)
			if start < finish {
				function.TypeParameters = append(function.TypeParameters, TypeParameter{Name: source[start:finish], Span: Span{Start: command.Argument.Start + start, End: command.Argument.Start + finish}})
			}
		}
		beforeSpace = end + 1
		offset = skipSyntaxSpace(source, beforeSpace, len(source))
		if vim9Signature && offset > beforeSpace && offset < len(source) && source[offset] == '(' {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1068", Message: "no white space allowed before function arguments",
				Span: Span{Start: command.Argument.Start + beforeSpace, End: command.Argument.Start + offset},
			})
		}
	}
	if offset >= len(source) || source[offset] != '(' {
		command.Function = function
		return
	}
	close := findMatching(source, offset, '(', ')')
	if close < 0 {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vimls/missing-parameter-end", Message: "expected ) after function parameters", Span: Span{Start: command.Argument.Start + offset, End: command.Argument.End}})
		close = len(source)
	}
	for _, part := range splitTopLevel(source, offset+1, close, ',') {
		parameter := parseParameter(file, command, source, part)
		if parameter != nil {
			function.Parameters = append(function.Parameters, *parameter)
		}
	}
	offset = close
	if offset < len(source) && source[offset] == ')' {
		offset++
	}
	offset = skipSyntaxSpace(source, offset, len(source))
	if offset < len(source) && source[offset] == ':' {
		typeStart := skipSyntaxSpace(source, offset+1, len(source))
		typeEnd := trimSyntaxSpaceEnd(source, typeStart, len(source))
		function.ReturnTypeSpan = Span{Start: command.Argument.Start + typeStart, End: command.Argument.Start + typeEnd}
		function.ReturnType, file.Diagnostics = appendTypeDiagnostics(file.Diagnostics, source[typeStart:typeEnd], command.Argument.Start+typeStart)
	} else if offset < len(source) {
		function.Attributes = Span{Start: command.Argument.Start + offset, End: command.Argument.End}
	}
	command.Function = function
}

func parseParameter(file *File, command *Command, source string, part Span) *Parameter {
	start := skipSyntaxSpace(source, part.Start, part.End)
	end := trimSyntaxSpaceEnd(source, start, part.End)
	if start >= end {
		return nil
	}
	parameter := &Parameter{}
	if strings.HasPrefix(source[start:end], "...") {
		parameter.Variadic = true
		start += 3
		start = skipSyntaxSpace(source, start, end)
	}
	colon := findTopLevelByte(source, start, end, ':')
	equals := findTopLevelByte(source, start, end, '=')
	nameEnd := end
	if colon >= 0 && (equals < 0 || colon < equals) {
		nameEnd = colon
	} else if equals >= 0 {
		nameEnd = equals
	}
	nameEnd = trimSyntaxSpaceEnd(source, start, nameEnd)
	parameter.Name = Span{Start: command.Argument.Start + start, End: command.Argument.Start + nameEnd}
	if colon >= 0 && (equals < 0 || colon < equals) {
		typeStart := skipSyntaxSpace(source, colon+1, end)
		typeEnd := end
		if equals >= 0 {
			typeEnd = trimSyntaxSpaceEnd(source, typeStart, equals)
		}
		parameter.TypeSpan = Span{Start: command.Argument.Start + typeStart, End: command.Argument.Start + typeEnd}
		parameter.Type, file.Diagnostics = appendTypeDiagnostics(file.Diagnostics, source[typeStart:typeEnd], command.Argument.Start+typeStart)
	}
	if equals >= 0 {
		defaultStart := skipSyntaxSpace(source, equals+1, end)
		parameter.DefaultSpan = Span{Start: command.Argument.Start + defaultStart, End: command.Argument.Start + end}
		parameter.Default, file.Diagnostics = appendExpressionDiagnostics(file.Diagnostics, source[defaultStart:end], command.Argument.Start+defaultStart, command.Dialect)
	}
	return parameter
}

func skipSyntaxSpace(source string, start, end int) int {
	for start < end {
		if isExpressionSpace(source[start]) || isLineLeadingBackslash(source, start) {
			start++
			continue
		}
		break
	}
	return start
}

func trimSyntaxSpaceEnd(source string, start, end int) int {
	for end > start && isExpressionSpace(source[end-1]) {
		end--
	}
	return end
}

func appendExpressionDiagnostics(diagnostics []Diagnostic, source string, base int, dialect Dialect) (*Expression, []Diagnostic) {
	expression, expressionDiagnostics := parseExpression(source, base, dialect)
	return expression, append(diagnostics, expressionDiagnostics...)
}

func findMatching(source string, start int, open, close byte) int {
	depth := 0
	quote := byte(0)
	for index := start; index < len(source); index++ {
		character := source[index]
		if quote != 0 {
			if character == '\\' && quote == '"' {
				index++
			} else if character == quote {
				quote = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			continue
		}
		if character == open {
			depth++
		} else if character == close {
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func splitTopLevel(source string, start, end int, separator byte) []Span {
	var parts []Span
	partStart := start
	var closings []byte
	quote := byte(0)
	for index := start; index < end; index++ {
		character := source[index]
		if quote != 0 {
			if character == '\\' && quote == '"' {
				index++
			} else if character == quote {
				quote = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			continue
		}
		switch character {
		case '(':
			closings = append(closings, ')')
		case '[':
			closings = append(closings, ']')
		case '{':
			closings = append(closings, '}')
		case '<':
			if likelyGenericAngle(source, index, end) {
				closings = append(closings, '>')
			}
		case ')', ']', '}', '>':
			if len(closings) > 0 && closings[len(closings)-1] == character {
				closings = closings[:len(closings)-1]
			}
		default:
			if character == separator && len(closings) == 0 {
				parts = append(parts, Span{Start: partStart, End: index})
				partStart = index + 1
			}
		}
	}
	return append(parts, Span{Start: partStart, End: end})
}

func likelyGenericAngle(source string, open, end int) bool {
	if open == 0 || open+1 >= end || isExpressionSpace(source[open-1]) || strings.ContainsRune("=<", rune(source[open+1])) {
		return false
	}
	return findGenericTypeEnd(source[:end], open) >= 0
}

func findTopLevelByte(source string, start, end int, wanted byte) int {
	for _, part := range splitTopLevel(source, start, end, wanted) {
		if part.End < end {
			return part.End
		}
	}
	return -1
}
