package syntax

func parseImport(file *File, command *Command) {
	source := file.Text(command.Argument)
	start := skipSpace(source, 0, len(source))
	importNode := &Import{}
	if wordEnd := scanWord(source, start, len(source)); source[start:wordEnd] == "autoload" && wordEnd < len(source) && isSpace(source[wordEnd]) {
		importNode.Autoload = true
		start = skipSpace(source, wordEnd, len(source))
	}
	aliasKeyword := findTopLevelKeyword(source, start, len(source), "as")
	pathEnd := len(source)
	if aliasKeyword >= 0 {
		pathEnd = trimSpaceEnd(source, start, aliasKeyword)
		aliasStart := skipSpace(source, aliasKeyword+2, len(source))
		aliasEnd := scanWord(source, aliasStart, len(source))
		importNode.Alias = Span{Start: command.Argument.Start + aliasStart, End: command.Argument.Start + aliasEnd}
	}
	pathStart := skipSpace(source, start, pathEnd)
	pathEnd = trimSpaceEnd(source, pathStart, pathEnd)
	importNode.PathSpan = Span{Start: command.Argument.Start + pathStart, End: command.Argument.Start + pathEnd}
	if pathStart < pathEnd {
		importNode.Path, file.Diagnostics = appendExpressionDiagnostics(file.Diagnostics, source[pathStart:pathEnd], command.Argument.Start+pathStart, command.Dialect)
	}
	command.Import = importNode
}

func findTopLevelKeyword(source string, start, end int, keyword string) int {
	depth := 0
	quote := byte(0)
	for index := start; index+len(keyword) <= end; index++ {
		character := source[index]
		if quote != 0 {
			if character == '\\' && quote == '"' {
				index++
			} else if character == quote {
				if quote == '\'' && index+1 < end && source[index+1] == '\'' {
					index++
				} else {
					quote = 0
				}
			}
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			continue
		}
		switch character {
		case '(', '[', '{':
			depth++
			continue
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth == 0 && source[index:index+len(keyword)] == keyword && (index == start || isExpressionSpace(source[index-1])) && (index+len(keyword) == end || isExpressionSpace(source[index+len(keyword)])) {
			return index
		}
	}
	return -1
}

func parseAggregate(file *File, command *Command, kind BlockKind) {
	if command.Dialect == Legacy {
		diagnostic := Diagnostic{Span: command.Name}
		switch kind {
		case BlockClass:
			diagnostic.Code = "vim/E1316"
			diagnostic.Message = "Class can only be defined in Vim9 script"
		case BlockInterface:
			diagnostic.Code = "vim/E1342"
			diagnostic.Message = "Interface can only be defined in Vim9 script"
		case BlockEnum:
			diagnostic.Code = "vim/E1414"
			diagnostic.Message = "Enum can only be defined in Vim9 script"
		}
		file.Diagnostics = append(file.Diagnostics, diagnostic)
	}
	source := maskVim9Comments(file.Text(command.Argument))
	nameStart := skipEnumSpace(source, 0, len(source))
	nameEnd := scanWord(source, nameStart, len(source))
	if nameEnd == nameStart {
		return
	}
	aggregate := &Aggregate{Kind: kind, Name: Span{Start: command.Argument.Start + nameStart, End: command.Argument.Start + nameEnd}}
	command.Aggregate = aggregate
	if command.Dialect == Vim9 {
		if source[nameStart] < 'A' || source[nameStart] > 'Z' {
			diagnostic := Diagnostic{Span: aggregate.Name}
			argumentEnd := trimEnumSpaceEnd(source, nameStart, len(source))
			argument := file.Source[command.Argument.Start+nameStart : command.Argument.Start+argumentEnd]
			switch kind {
			case BlockClass:
				diagnostic.Code = "vim/E1314"
				diagnostic.Message = "Class name must start with an uppercase letter: " + argument
			case BlockInterface:
				diagnostic.Code = "vim/E1343"
				diagnostic.Message = "Interface name must start with an uppercase letter: " + argument
			case BlockEnum:
				diagnostic.Code = "vim/E1415"
				diagnostic.Message = "Enum name must start with an uppercase letter: " + argument
			}
			file.Diagnostics = append(file.Diagnostics, diagnostic)
			return
		}
		if nameEnd < len(source) && !isExpressionSpace(source[nameEnd]) {
			argumentEnd := trimEnumSpaceEnd(source, nameStart, len(source))
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1315", Message: "White space required after name: " + file.Source[command.Argument.Start+nameStart:command.Argument.Start+argumentEnd],
				Span: Span{Start: command.Argument.Start + nameStart, End: command.Argument.Start + argumentEnd},
			})
			return
		}
	}
	remainder := nameEnd
	for remainder < len(source) {
		remainder = skipEnumSpace(source, remainder, len(source))
		if remainder >= len(source) {
			break
		}
		keywordEnd := scanWord(source, remainder, len(source))
		if keywordEnd == remainder {
			if command.Dialect == Vim9 {
				end := trimEnumSpaceEnd(source, remainder, len(source))
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E488", Message: "trailing characters",
					Span: Span{Start: command.Argument.Start + remainder, End: command.Argument.Start + end},
				})
			}
			break
		}
		keyword := source[remainder:keywordEnd]
		if keyword != "extends" && keyword != "implements" {
			if command.Dialect == Vim9 {
				end := trimEnumSpaceEnd(source, remainder, len(source))
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E488", Message: "trailing characters",
					Span: Span{Start: command.Argument.Start + remainder, End: command.Argument.Start + end},
				})
			}
			break
		}
		keywordStart := remainder
		if command.Dialect == Vim9 && kind == BlockInterface && keyword == "implements" {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1381", Message: `Interface cannot use "implements"`,
				Span: Span{Start: command.Argument.Start + keywordStart, End: command.Argument.Start + keywordEnd},
			})
			return
		}
		remainder = keywordEnd
		for {
			valueStart := skipEnumSpace(source, remainder, len(source))
			valueEnd := scanClassName(source, valueStart, len(source))
			if valueEnd == valueStart {
				if kind == BlockClass && keyword == "implements" {
					file.Diagnostics = append(file.Diagnostics, Diagnostic{
						Code: "vim/E1389", Message: "missing name after implements",
						Span: Span{Start: command.Argument.Start + keywordStart, End: command.Argument.Start + keywordEnd},
					})
				}
				break
			}
			span := Span{Start: command.Argument.Start + valueStart, End: command.Argument.Start + valueEnd}
			if keyword == "extends" {
				aggregate.Extends = append(aggregate.Extends, span)
			} else {
				aggregate.Implements = append(aggregate.Implements, span)
			}
			remainder = skipEnumSpace(source, valueEnd, len(source))
			if remainder >= len(source) || source[remainder] != ',' {
				break
			}
			remainder++
		}
	}
}
func scanClassName(source string, start, end int) int {
	position := start
	for position < end {
		wordEnd := scanWord(source, position, end)
		if wordEnd == position {
			break
		}
		position = wordEnd
		if position >= end || source[position] != '.' {
			break
		}
		position++
	}
	return position
}

func parseTypeAlias(file *File, command *Command) {
	source := file.Text(command.Argument)
	assignment := findAssignment(source)
	if assignment.Start < 0 {
		return
	}
	nameStart := skipSpace(source, 0, assignment.Start)
	nameEnd := trimSpaceEnd(source, nameStart, assignment.Start)
	typeStart := skipSpace(source, assignment.End, len(source))
	typeEnd := trimSpaceEnd(source, typeStart, len(source))
	operator := Span{Start: command.Argument.Start + assignment.Start, End: command.Argument.Start + assignment.End}
	typeSpan := Span{Start: command.Argument.Start + typeStart, End: command.Argument.Start + typeEnd}
	var typeNode *Type
	if command.Dialect == Vim9 && typeStart == typeEnd {
		typeNode = &Type{Kind: TypeMissing, Span: typeSpan}
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1398", Message: "missing type alias type", Span: typeSpan,
		})
	} else {
		var diagnostics []Diagnostic
		typeNode, diagnostics = parseTypeAt(source[typeStart:typeEnd], typeSpan.Start)
		file.Diagnostics = append(file.Diagnostics, diagnostics...)
	}
	command.TypeAlias = &TypeAlias{
		Name:       Span{Start: command.Argument.Start + nameStart, End: command.Argument.Start + nameEnd},
		Assignment: operator, Type: typeNode, TypeSpan: typeSpan,
	}
}

// parseEnumValues parses one logical line at the beginning of an enum. Vim
// concatenates physical lines while a comma or constructor delimiter is open,
// so a Command may own several values. The return value says whether more enum
// value lines are required.
func parseEnumValues(file *File, command *Command) bool {
	start := command.Name.Start
	end := command.Span.End
	if start >= end {
		return false
	}
	source := maskVim9Comments(file.Source[start:end])
	trimmedEnd := trimEnumSpaceEnd(source, 0, len(source))
	more := trimmedEnd > 0 && source[trimmedEnd-1] == ','
	parts := splitTopLevel(source, 0, trimmedEnd, ',')
	commaDiagnosticReported := false
	for _, part := range parts {
		if !commaDiagnosticReported && part.End < trimmedEnd {
			comma := part.End
			afterComma := comma + 1
			if afterComma < trimmedEnd && !isExpressionSpace(source[afterComma]) {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E1069", Message: "white space required after ','",
					Span: Span{Start: start + comma, End: start + afterComma},
				})
				commaDiagnosticReported = true
			} else if comma > part.Start && isExpressionSpace(source[comma-1]) {
				beforeComma := trimEnumSpaceEnd(source, part.Start, comma)
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E1068", Message: "no white space allowed before ','",
					Span: Span{Start: start + beforeComma, End: start + afterComma},
				})
				commaDiagnosticReported = true
			}
		}
	}
	for _, part := range parts {
		valueStart := skipEnumSpace(source, part.Start, part.End)
		valueEnd := trimEnumSpaceEnd(source, valueStart, part.End)
		if valueStart >= valueEnd {
			continue
		}
		nameEnd := scanWord(source, valueStart, valueEnd)
		if nameEnd == valueStart {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1418", Message: "Invalid enum value declaration: " + file.Source[start+valueStart:start+valueEnd],
				Span: Span{Start: start + valueStart, End: start + valueEnd},
			})
			return false
		}
		value := EnumValue{Name: Span{Start: start + valueStart, End: start + nameEnd}}
		payloadStart := skipEnumSpace(source, nameEnd, valueEnd)
		if payloadStart < valueEnd {
			if source[payloadStart] != '(' && source[payloadStart] != '<' {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E1123", Message: "missing comma before enum value argument",
					Span: Span{Start: start + payloadStart, End: start + valueEnd},
				})
			} else if payloadStart > nameEnd {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E1068", Message: "no white space allowed before '('",
					Span: Span{Start: start + nameEnd, End: start + payloadStart},
				})
			} else {
				initializer, diagnostics := parseExpression(source[valueStart:valueEnd], start+valueStart, Vim9)
				value.Initializer = initializer
				if initializer.Kind == ExpressionCall && len(initializer.Children) > 1 {
					value.Arguments = append(value.Arguments, initializer.Children[1:]...)
				}
				file.Diagnostics = append(file.Diagnostics, diagnostics...)
			}
		}
		command.EnumValues = append(command.EnumValues, value)
	}
	return more
}

func skipEnumSpace(source string, start, end int) int {
	for start < end {
		switch source[start] {
		case ' ', '\t', '\r', '\n':
			start++
		case '\\':
			if isLineLeadingBackslash(source, start) {
				start++
				continue
			}
			return start
		default:
			return start
		}
	}
	return start
}

func trimEnumSpaceEnd(source string, start, end int) int {
	for end > start {
		switch source[end-1] {
		case ' ', '\t', '\r', '\n':
			end--
		default:
			return end
		}
	}
	return end
}

func maskVim9Comments(source string) string {
	masked := []byte(source)
	quote := byte(0)
	lineStart := true
	for index := 0; index < len(masked); index++ {
		character := masked[index]
		if quote != 0 {
			if character == '\\' && quote == '"' && index+1 < len(masked) {
				index++
			} else if character == quote {
				if quote == '\'' && index+1 < len(masked) && masked[index+1] == '\'' {
					index++
				} else {
					quote = 0
				}
			}
			continue
		}
		if character == '\n' {
			lineStart = true
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			lineStart = false
			continue
		}
		if character == '#' {
			for index < len(masked) && masked[index] != '\n' {
				masked[index] = ' '
				index++
			}
			index--
			continue
		}
		if lineStart && (character == ' ' || character == '\t' || character == '\r' || character == '\\') {
			continue
		}
		lineStart = false
	}
	return string(masked)
}
