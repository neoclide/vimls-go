package syntax

import "strings"

func parseImport(file *File, command *Command) {
	source := file.Text(command.Argument)
	start := skipSpace(source, 0, len(source))
	importNode := &Import{}
	invalidAlias := Span{}
	hasInvalidAlias := false
	if wordEnd := scanWord(source, start, len(source)); source[start:wordEnd] == "autoload" && wordEnd < len(source) && isSpace(source[wordEnd]) {
		importNode.Autoload = true
		start = skipSpace(source, wordEnd, len(source))
	}
	aliasKeyword := findTopLevelKeyword(source, start, len(source), "as")
	pathEnd := len(source)
	if aliasKeyword >= 0 {
		pathEnd = trimSpaceEnd(source, start, aliasKeyword)
		aliasStart := skipSpace(source, aliasKeyword+2, len(source))
		aliasEnd := aliasStart
		if aliasEnd < len(source) && (isASCIIAlpha(source[aliasEnd]) || source[aliasEnd] == '_') {
			aliasEnd++
			for aliasEnd < len(source) && (isASCIIAlpha(source[aliasEnd]) || isExpressionDigit(source[aliasEnd]) || source[aliasEnd] == '_') {
				aliasEnd++
			}
		}
		importNode.Alias = Span{Start: command.Argument.Start + aliasStart, End: command.Argument.Start + aliasEnd}
		if command.Dialect == Vim9 && (aliasEnd == aliasStart || aliasEnd < len(source) && !isSpace(source[aliasEnd])) {
			invalidEnd := aliasStart
			for invalidEnd < len(source) && !isSpace(source[invalidEnd]) {
				invalidEnd++
			}
			invalidAlias = Span{Start: command.Argument.Start + aliasStart, End: command.Argument.Start + invalidEnd}
			hasInvalidAlias = true
		}
	}
	pathStart := skipSpace(source, start, pathEnd)
	pathEnd = trimSpaceEnd(source, pathStart, pathEnd)
	importNode.PathSpan = Span{Start: command.Argument.Start + pathStart, End: command.Argument.Start + pathEnd}
	if pathStart < pathEnd {
		importNode.Path, file.Diagnostics = appendExpressionDiagnostics(file.Diagnostics, source[pathStart:pathEnd], command.Argument.Start+pathStart, command.Dialect)
	}
	if command.Dialect == Vim9 && aliasKeyword < 0 && importNode.Path != nil && importNode.Path.Kind == ExpressionString {
		literal := importNode.Path.Value
		if len(literal) >= 2 && (literal[0] == '\'' || literal[0] == '"') && literal[0] == literal[len(literal)-1] {
			path := literal[1 : len(literal)-1]
			tail := path
			if separator := strings.LastIndexAny(path, "/\\"); separator >= 0 {
				tail = path[separator+1:]
			}
			extension := strings.Index(tail, ".vim")
			if extension == 0 && extension+len(".vim") == len(tail) {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E1261", Message: `Cannot import .vim without using "as"`, Span: importNode.PathSpan})
			} else if extension < 0 || extension+len(".vim") != len(tail) {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E1257", Message: `Imported script must use "as" or end in .vim: ` + tail, Span: importNode.PathSpan})
			}
		}
	}
	if hasInvalidAlias {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E1047", Message: "Syntax error in import: " + file.Text(invalidAlias), Span: invalidAlias})
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
	hasExtends := false
	hasImplements := false
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
		if command.Dialect == Vim9 && kind == BlockEnum && keyword == "extends" {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1416", Message: "Enum cannot extend a class or enum",
				Span: Span{Start: command.Argument.Start + keywordStart, End: command.Argument.Start + keywordEnd},
			})
			return
		}
		if command.Dialect == Vim9 && kind == BlockInterface && keyword == "implements" {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1381", Message: `Interface cannot use "implements"`,
				Span: Span{Start: command.Argument.Start + keywordStart, End: command.Argument.Start + keywordEnd},
			})
			return
		}
		if keyword == "extends" {
			if command.Dialect == Vim9 && hasExtends {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E1352", Message: `Duplicate "extends"`,
					Span: Span{Start: command.Argument.Start + keywordStart, End: command.Argument.Start + keywordEnd},
				})
				return
			}
			hasExtends = true
			valueStart := skipEnumSpace(source, keywordEnd, len(source))
			valueEnd := scanClassName(source, valueStart, len(source))
			if command.Dialect == Vim9 && valueEnd < len(source) && !isExpressionSpace(source[valueEnd]) {
				end := trimEnumSpaceEnd(source, valueStart, len(source))
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E1315", Message: "White space required after name: " + file.Source[command.Argument.Start+valueStart:command.Argument.Start+end],
					Span: Span{Start: command.Argument.Start + valueStart, End: command.Argument.Start + end},
				})
				return
			}
			if valueEnd > valueStart {
				aggregate.Extends = append(aggregate.Extends, Span{Start: command.Argument.Start + valueStart, End: command.Argument.Start + valueEnd})
			}
			remainder = valueEnd
			continue
		}

		if command.Dialect == Vim9 && hasImplements {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1350", Message: `Duplicate "implements"`,
				Span: Span{Start: command.Argument.Start + keywordStart, End: command.Argument.Start + keywordEnd},
			})
			return
		}
		hasImplements = true
		remainder = keywordEnd
		for {
			valueStart := skipEnumSpace(source, remainder, len(source))
			valueEnd := scanClassName(source, valueStart, len(source))
			invalidEnd := valueEnd < len(source) && !isExpressionSpace(source[valueEnd]) && source[valueEnd] != ','
			invalidComma := valueEnd < len(source) && source[valueEnd] == ',' && valueEnd+1 < len(source) && !isExpressionSpace(source[valueEnd+1])
			if command.Dialect == Vim9 && (invalidEnd || invalidComma) {
				end := trimEnumSpaceEnd(source, valueStart, len(source))
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E1315", Message: "White space required after name: " + file.Source[command.Argument.Start+valueStart:command.Argument.Start+end],
					Span: Span{Start: command.Argument.Start + valueStart, End: command.Argument.Start + end},
				})
				return
			}
			if valueEnd == valueStart {
				if command.Dialect == Vim9 && kind == BlockClass {
					file.Diagnostics = append(file.Diagnostics, Diagnostic{
						Code: "vim/E1389", Message: "missing name after implements",
						Span: Span{Start: command.Argument.Start + keywordStart, End: command.Argument.Start + keywordEnd},
					})
				}
				return
			}
			span := Span{Start: command.Argument.Start + valueStart, End: command.Argument.Start + valueEnd}
			for _, existing := range aggregate.Implements {
				if command.Dialect == Vim9 && file.Source[existing.Start:existing.End] == file.Source[span.Start:span.End] {
					file.Diagnostics = append(file.Diagnostics, Diagnostic{
						Code: "vim/E1351", Message: `Duplicate interface after "implements": ` + file.Source[span.Start:span.End], Span: span,
					})
					return
				}
			}
			aggregate.Implements = append(aggregate.Implements, span)
			remainder = valueEnd
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
		character := source[position]
		if character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' {
			position++
			continue
		}
		if character == '.' && position+1 < end {
			next := source[position+1]
			if next >= 'A' && next <= 'Z' || next >= 'a' && next <= 'z' || next >= '0' && next <= '9' || next == '_' {
				position++
				continue
			}
		}
		break
	}
	return position
}

func parseTypeAlias(file *File, command *Command) {
	source := file.Text(command.Argument)
	nameStart := skipSpace(source, 0, len(source))
	nameEnd := scanWord(source, nameStart, len(source))
	nameDiagnostic := false
	// ex_type() checks the alias name before looking for its assignment.  Keep
	// that ordering here, since otherwise malformed separators can turn into
	// a misleading missing-assignment or type diagnostic.
	if nameStart < len(source) && (source[nameStart] < 'A' || source[nameStart] > 'Z') {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1394", Message: "Type name must start with an uppercase letter: " + source[nameStart:],
			Span: Span{Start: command.Argument.Start + nameStart, End: command.Argument.Start + nameEnd},
		})
		nameDiagnostic = true
	}
	nameHasWhitespace := nameEnd >= len(source) || isSpace(source[nameEnd])
	if !nameDiagnostic && nameStart < nameEnd && !nameHasWhitespace {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1315", Message: "White space required after name: " + source[nameStart:],
			Span: Span{Start: command.Argument.Start + nameStart, End: command.Argument.Start + len(source)},
		})
		nameDiagnostic = true
		// A separator other than '=' makes the remainder opaque.  In
		// particular, do not let a colon consume the next physical command.
		if source[nameEnd] != '=' {
			command.TypeAlias = &TypeAlias{
				Name:       Span{Start: command.Argument.Start + nameStart, End: command.Argument.Start + nameEnd},
				Assignment: Span{Start: command.Argument.End, End: command.Argument.End},
				TypeSpan:   Span{Start: command.Argument.End, End: command.Argument.End},
			}
			return
		}
	}
	assignment := findAssignment(source)
	if assignment.Start < 0 {
		// Keep the alias name in the AST even when Vim9 reports E398.  The
		// remainder of this physical line is intentionally not interpreted as
		// a type: it may be incomplete or otherwise unrelated while editing.
		if nameStart < nameEnd {
			name := Span{Start: command.Argument.Start + nameStart, End: command.Argument.Start + nameEnd}
			if !nameDiagnostic {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E398", Message: "missing type alias assignment", Span: name,
				})
			}
			command.TypeAlias = &TypeAlias{
				Name:       name,
				Assignment: Span{Start: command.Argument.End, End: command.Argument.End},
				TypeSpan:   Span{Start: command.Argument.End, End: command.Argument.End},
			}
		}
		return
	}
	nameStart = skipSpace(source, 0, assignment.Start)
	nameEnd = trimSpaceEnd(source, nameStart, assignment.Start)
	typeStart := skipSpace(source, assignment.End, len(source))
	typeEnd := trimSpaceEnd(source, typeStart, len(source))
	if !nameDiagnostic && assignment.End < len(source) && !isSpace(source[assignment.End]) {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1069", Message: "white space required after '=': " + source[assignment.Start:],
			Span: Span{Start: command.Argument.Start + assignment.Start, End: command.Argument.Start + len(source)},
		})
		nameDiagnostic = true
	}
	operator := Span{Start: command.Argument.Start + assignment.Start, End: command.Argument.Start + assignment.End}
	typeSpan := Span{Start: command.Argument.Start + typeStart, End: command.Argument.Start + typeEnd}
	var typeNode *Type
	if command.Dialect == Vim9 && typeStart == typeEnd {
		typeNode = &Type{Kind: TypeMissing, Span: typeSpan}
		if !nameDiagnostic {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1398", Message: "Missing type alias type", Span: typeSpan,
			})
		}
	} else {
		var diagnostics []Diagnostic
		typeNode, diagnostics = parseTypeAt(source[typeStart:typeEnd], typeSpan.Start)
		if command.Dialect == Vim9 && len(diagnostics) == 1 && diagnostics[0].Code == "vimls/trailing-type" {
			diagnostics[0].Code = "vim/E488"
			diagnostics[0].Message = "trailing characters"
		}
		if !nameDiagnostic {
			file.Diagnostics = append(file.Diagnostics, diagnostics...)
		}
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
	for index, part := range parts {
		if part.End < trimmedEnd {
			comma := part.End
			afterComma := comma + 1
			if afterComma < trimmedEnd && !isExpressionSpace(source[afterComma]) {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E1069", Message: "white space required after ','",
					Span: Span{Start: start + comma, End: start + afterComma},
				})
				parts = parts[:index+1]
				more = false
				break
			} else if comma > part.Start && isExpressionSpace(source[comma-1]) {
				beforeComma := trimEnumSpaceEnd(source, part.Start, comma)
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E1068", Message: "no white space allowed before ','",
					Span: Span{Start: start + beforeComma, End: start + afterComma},
				})
				parts = parts[:index+1]
				more = false
				break
			}
		}
	}
	for _, part := range parts {
		valueStart := skipEnumSpace(source, part.Start, part.End)
		valueEnd := trimEnumSpaceEnd(source, valueStart, part.End)
		if valueStart >= valueEnd {
			continue
		}
		if command.Dialect == Vim9 && strings.HasPrefix(source[valueStart:valueEnd], "#{") && !strings.HasPrefix(source[valueStart:valueEnd], "#{{") {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1170", Message: "Cannot use #{ to start a comment", Span: Span{Start: start + valueStart, End: start + valueStart + 2},
			})
			return false
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
		dictionaryStart := index+1 < len(masked) && masked[index+1] == '{' && (index+2 >= len(masked) || masked[index+2] != '{')
		if character == '#' && !dictionaryStart && (lineStart || index > 0 && isSpace(masked[index-1])) {
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
