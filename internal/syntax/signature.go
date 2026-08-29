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
	rawSource := file.Text(command.Argument)
	vim9Signature := command.Dialect == Vim9 || command.Canonical == "def"
	source := rawSource
	if vim9Signature {
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
	spaceBeforeGeneric := vim9Signature && offset > beforeSpace && offset < len(source) && source[offset] == '<'
	genericInvalid := spaceBeforeGeneric
	if spaceBeforeGeneric {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1068", Message: "no white space allowed before '<'",
			Span: Span{Start: command.Argument.Start + beforeSpace, End: command.Argument.Start + offset},
		})
	}
	if vim9Signature && offset > beforeSpace && offset < len(source) && source[offset] == '(' {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1068", Message: "no white space allowed before function arguments",
			Span: Span{Start: command.Argument.Start + beforeSpace, End: command.Argument.Start + offset},
		})
	}
	if vim9Signature && offset < len(source) && source[offset] == '<' {
		end := findMatching(source, offset, '<', '>')
		if end < 0 {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vimls/missing-generic-end", Message: "expected > after generic type parameters", Span: Span{Start: command.Argument.Start + offset, End: command.Argument.End}})
			command.Function = function
			return
		}
		var diagnostics []Diagnostic
		function.TypeParameters, diagnostics = parseFunctionTypeParameters(source, command.Argument.Start, offset, end)
		if !spaceBeforeGeneric {
			file.Diagnostics = append(file.Diagnostics, diagnostics...)
			genericInvalid = len(diagnostics) > 0
		}
		beforeSpace = end + 1
		offset = skipSyntaxSpace(source, beforeSpace, len(source))
		if vim9Signature && !genericInvalid && offset > beforeSpace && offset < len(source) && source[offset] == '(' {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1068", Message: "no white space allowed before function arguments",
				Span: Span{Start: command.Argument.Start + beforeSpace, End: command.Argument.Start + offset},
			})
		}
	}
	if offset >= len(source) || source[offset] != '(' {
		if vim9Signature && offset < len(source) {
			end := trimSyntaxSpaceEnd(source, offset, len(source))
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E488", Message: "trailing characters",
				Span: Span{Start: command.Argument.Start + offset, End: command.Argument.Start + end},
			})
		}
		command.Function = function
		return
	}
	open := offset
	if vim9Signature && open+1 < len(rawSource) && rawSource[open+1] == '#' {
		end := strings.IndexByte(rawSource[open+1:], '\n')
		if end < 0 {
			end = len(rawSource)
		} else {
			end += open + 1
		}
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E125", Message: "illegal argument",
			Span: Span{Start: command.Argument.Start + open + 1, End: command.Argument.Start + end},
		})
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
		if command.Canonical == "def" && part.End < close {
			beforeComma := trimSyntaxSpaceEnd(source, part.Start, part.End)
			spaceBeforeComma := beforeComma < part.End
			if spaceBeforeComma {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E1068", Message: "no white space allowed before ','",
					Span: Span{Start: command.Argument.Start + beforeComma, End: command.Argument.Start + part.End + 1},
				})
			}
			afterComma := part.End + 1
			if !spaceBeforeComma && (afterComma >= close || !isExpressionSpace(source[afterComma])) {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E1069", Message: "white space required after ','",
					Span: Span{Start: command.Argument.Start + part.End, End: command.Argument.Start + afterComma},
				})
			}
		}
	}
	offset = close
	if offset < len(source) && source[offset] == ')' {
		offset++
	}
	tailStart := offset
	offset = skipSyntaxSpace(rawSource, offset, len(rawSource))
	spaceBeforeTail := offset > tailStart
	if offset < len(rawSource) && rawSource[offset] == ':' {
		spaceBeforeColon := vim9Signature && spaceBeforeTail
		if spaceBeforeColon {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1059", Message: "no white space allowed before colon",
				Span: Span{Start: command.Argument.Start + tailStart, End: command.Argument.Start + offset + 1},
			})
		} else if vim9Signature && (offset+1 >= len(source) || !isExpressionSpace(source[offset+1])) {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1069", Message: "white space required after ':'",
				Span: Span{Start: command.Argument.Start + offset, End: command.Argument.Start + offset + 1},
			})
		}
		typeStart := skipSyntaxSpace(source, offset+1, len(source))
		typeEnd := trimSyntaxSpaceEnd(source, typeStart, len(source))
		function.ReturnTypeSpan = Span{Start: command.Argument.Start + typeStart, End: command.Argument.Start + typeEnd}
		function.ReturnType, file.Diagnostics = appendTypeDiagnostics(file.Diagnostics, source[typeStart:typeEnd], command.Argument.Start+typeStart)
	} else if offset < len(rawSource) {
		if rawSource[offset] == '#' {
			validComment := spaceBeforeTail && (command.Canonical == "def" || command.Dialect == Vim9)
			if !validComment {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E488", Message: "trailing characters",
					Span: Span{Start: command.Argument.Start + offset, End: command.Argument.End},
				})
			}
		} else if rawSource[offset] == '"' {
			if command.Canonical == "def" {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E488", Message: "trailing characters",
					Span: Span{Start: command.Argument.Start + offset, End: command.Argument.End},
				})
			}
		} else if command.Canonical == "def" {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E488", Message: "trailing characters",
				Span: Span{Start: command.Argument.Start + offset, End: command.Argument.End},
			})
		} else {
			attributeStart := offset
			attributeEnd := offset
			for offset < len(rawSource) {
				wordEnd := scanWord(rawSource, offset, len(rawSource))
				word := rawSource[offset:wordEnd]
				if word != "range" && word != "dict" && word != "abort" && word != "closure" {
					break
				}
				attributeEnd = wordEnd
				offset = skipSyntaxSpace(rawSource, wordEnd, len(rawSource))
			}
			if attributeEnd > attributeStart {
				function.Attributes = Span{Start: command.Argument.Start + attributeStart, End: command.Argument.Start + attributeEnd}
			}
			if offset < len(rawSource) && rawSource[offset] != '"' && !(rawSource[offset] == '#' && command.Dialect == Vim9 && offset > tailStart && isExpressionSpace(rawSource[offset-1])) {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E488", Message: "trailing characters",
					Span: Span{Start: command.Argument.Start + offset, End: command.Argument.End},
				})
			}
		}
	} else if command.Canonical == "def" {
		lineEnd := command.Argument.End
		for lineEnd < len(file.Source) && file.Source[lineEnd] != '\n' {
			lineEnd++
		}
		trailing := skipSpace(file.Source, command.Argument.End, lineEnd)
		if trailing < lineEnd && file.Source[trailing] == '"' {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E488", Message: "trailing characters", Span: Span{Start: trailing, End: lineEnd},
			})
		}
	}
	command.Function = function
}

func parseFunctionTypeParameters(source string, base, open, close int) ([]TypeParameter, []Diagnostic) {
	var parameters []TypeParameter
	var diagnostics []Diagnostic
	report := func(code, message string, start, end int) {
		if len(diagnostics) == 0 {
			diagnostics = append(diagnostics, Diagnostic{Code: code, Message: message, Span: Span{Start: base + start, End: base + end}})
		}
	}
	if open+1 == close {
		report("vim/E1555", "empty type list for generic function", open, close+1)
		return parameters, diagnostics
	}

	offset := open + 1
	if offset < close && isExpressionSpace(source[offset]) {
		report("vim/E1202", "no white space allowed after '<'", offset, offset+1)
		offset = skipSyntaxSpace(source, offset, close)
	}
	seen := make(map[string]struct{})
	for offset < close {
		if source[offset] == ',' {
			report("vim/E1008", "missing type after ','", offset, offset+1)
			offset++
			offset = skipSyntaxSpace(source, offset, close)
			continue
		}

		nameStart := offset
		if source[offset] < 'A' || source[offset] > 'Z' {
			if source[offset] >= 'a' && source[offset] <= 'z' {
				report("vim/E1552", "type variable name must start with an uppercase letter", offset, offset+1)
			} else {
				report("vim/E1008", "missing generic type", offset, offset+1)
			}
		}
		for offset < close && isASCIIIdentifierContinue(source[offset]) {
			offset++
		}
		if offset == nameStart {
			offset++
			continue
		}
		name := source[nameStart:offset]
		parameter := TypeParameter{Name: name, Span: Span{Start: base + nameStart, End: base + offset}}
		parameters = append(parameters, parameter)
		if _, exists := seen[name]; exists {
			report("vim/E1561", "duplicate type variable name", nameStart, offset)
		}
		seen[name] = struct{}{}

		if offset < close && isExpressionSpace(source[offset]) {
			report("vim/E1202", "no white space allowed after generic type name", offset, offset+1)
			offset = skipSyntaxSpace(source, offset, close)
		}
		if offset >= close {
			break
		}
		if source[offset] != ',' {
			report("vim/E1553", "missing comma in generic function", offset, offset+1)
			for offset < close && source[offset] != ',' {
				offset++
			}
		}
		if offset >= close {
			break
		}
		offset++
		if offset >= close {
			report("vim/E1069", "white space required after ','", offset-1, offset)
			break
		}
		if !isExpressionSpace(source[offset]) {
			report("vim/E1069", "white space required after ','", offset-1, offset)
		} else {
			offset = skipSyntaxSpace(source, offset, close)
			if offset >= close {
				report("vim/E1008", "missing generic type after ','", close, close)
			}
		}
	}
	return parameters, diagnostics
}

func parseParameter(file *File, command *Command, source string, part Span) *Parameter {
	vim9Signature := command.Dialect == Vim9 || command.Canonical == "def"
	start := skipSyntaxSpace(source, part.Start, part.End)
	end := trimSyntaxSpaceEnd(source, start, part.End)
	if start >= end {
		return nil
	}
	parameter := &Parameter{}
	variadicStart := -1
	if strings.HasPrefix(source[start:end], "...") {
		parameter.Variadic = true
		variadicStart = start
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
	if command.Canonical == "def" && parameter.Variadic && nameEnd == start {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1055", Message: "missing name after ...",
			Span: Span{Start: command.Argument.Start + variadicStart, End: command.Argument.Start + variadicStart + 3},
		})
	}
	parameter.Name = Span{Start: command.Argument.Start + start, End: command.Argument.Start + nameEnd}
	if vim9Signature && !parameter.Variadic {
		parameter.Target = parseConstructorParameterTarget(source, start, nameEnd, command.Argument.Start)
	}
	if colon >= 0 && (equals < 0 || colon < equals) {
		if vim9Signature && (colon+1 >= end || !isExpressionSpace(source[colon+1])) {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1069", Message: "white space required after ':'",
				Span: Span{Start: command.Argument.Start + colon, End: command.Argument.Start + colon + 1},
			})
		}
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
		if defaultStart >= end {
			parameter.Default = &Expression{Kind: ExpressionMissing, Span: parameter.DefaultSpan}
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E125", Message: "illegal argument", Span: parameter.DefaultSpan,
			})
		} else {
			dialect := command.Dialect
			if vim9Signature {
				dialect = Vim9
			}
			parameter.Default, file.Diagnostics = appendExpressionDiagnostics(file.Diagnostics, source[defaultStart:end], command.Argument.Start+defaultStart, dialect)
		}
	}
	return parameter
}

// parseConstructorParameterTarget recognizes only the constructor shorthand
// accepted by Vim9: an ASCII `this.` prefix followed by one ASCII identifier.
// The incomplete `this.` form is retained as a partial member node so edits do
// not make the following parameter or line disappear during recovery.
func parseConstructorParameterTarget(source string, start, end, base int) *Expression {
	if !strings.HasPrefix(source[start:end], "this.") {
		return nil
	}
	memberStart := start + len("this.")
	member := source[memberStart:end]
	if member != "" {
		for index := 0; index < len(member); index++ {
			character := member[index]
			if index == 0 {
				if !isASCIIIdentifierStart(character) {
					return nil
				}
			} else if !isASCIIIdentifierContinue(character) {
				return nil
			}
		}
	}
	return &Expression{
		Kind:  ExpressionMember,
		Span:  Span{Start: base + start, End: base + end},
		Value: member,
		Operator: Span{
			Start: base + start + len("this"),
			End:   base + start + len("this."),
		},
		Children: []*Expression{{
			Kind:  ExpressionIdentifier,
			Span:  Span{Start: base + start, End: base + start + len("this")},
			Value: "this",
		}},
	}
}

func isASCIIIdentifierStart(character byte) bool {
	return character == '_' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
}

func isASCIIIdentifierContinue(character byte) bool {
	return isASCIIIdentifierStart(character) || character >= '0' && character <= '9'
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
