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
	defSignature := command.Canonical == "def"
	vim9Context := command.Dialect == Vim9 || defSignature
	source := rawSource
	if vim9Context {
		source = maskVim9Comments(source)
	}
	offset := skipSyntaxSpace(source, 0, len(source))
	nameStart := offset
	if vim9Context && strings.HasPrefix(source[nameStart:], "<SID>:") {
		function := &Function{Name: Span{Start: command.Argument.Start + nameStart, End: command.Argument.Start + nameStart + len("<SID>:")}}
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E884", Message: "function name cannot contain a colon", Span: function.Name,
		})
		command.Function = function
		return
	}
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
	name := source[nameStart:offset]
	nestedDefNamespace := false
	if vim9Context && !strings.Contains(name, ".") && command.Block >= 0 && command.Block < len(file.Blocks) {
		block := file.Blocks[command.Block]
		if block.Kind == BlockDef && block.Parent >= 0 && block.Parent < len(file.Blocks) && file.Blocks[block.Parent].Kind == BlockDef {
			for _, namespace := range []string{"s:", "b:"} {
				if strings.HasPrefix(name, namespace) {
					nestedDefNamespace = true
					file.Diagnostics = append(file.Diagnostics, Diagnostic{
						Code: "vim/E1075", Message: "Namespace not supported: " + name, Span: function.Name,
					})
					break
				}
			}
		}
	}
	if vim9Context && (name == "g:" || strings.HasSuffix(name, "#") && offset < len(source) && source[offset] == '(') {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E129", Message: "Function name required", Span: function.Name,
		})
		command.Function = function
		return
	}
	directAggregateMethod := false
	if command.Block >= 0 && command.Block < len(file.Blocks) {
		block := file.Blocks[command.Block]
		directAggregateMethod = block.Kind == BlockClass || block.Kind == BlockInterface || block.Kind == BlockEnum
		if block.Kind == BlockDef && block.Parent >= 0 && block.Parent < len(file.Blocks) {
			parent := file.Blocks[block.Parent].Kind
			directAggregateMethod = parent == BlockClass || parent == BlockInterface || parent == BlockEnum
		}
	}
	vim9ScriptNamespace := file.Dialect == Vim9 && command.Dialect == Vim9 && !nestedDefNamespace && strings.HasPrefix(name, "s:") && len(name) > len("s:")
	if vim9ScriptNamespace {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1268", Message: "Cannot use s: in Vim9 script: " + name, Span: function.Name,
		})
	}
	dictFunction := vim9Context && strings.Contains(name, ".")
	if dictFunction && !vim9ScriptNamespace {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1182", Message: "Cannot define a dict function in Vim9 script: " + name,
			Span: function.Name,
		})
	}
	if command.Dialect == Vim9 && !vim9ScriptNamespace && strings.Contains(name, "#") && !strings.HasSuffix(name, "#") {
		exported := false
		for _, modifier := range command.Modifiers {
			exported = exported || modifier.Name == "export"
		}
		if !exported {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1263", Message: "Cannot use name with # in Vim9 script, use export instead",
				Span: function.Name,
			})
		}
	}
	if command.Canonical == "function" && command.Dialect == Legacy && !strings.Contains(name, ".") && !strings.Contains(name, "#") {
		globalName := name
		if strings.HasPrefix(globalName, "g:") {
			globalName = globalName[2:]
		} else if strings.Contains(globalName, ":") {
			globalName = ""
		}
		if len(globalName) > 0 && globalName[0] >= 'a' && globalName[0] <= 'z' {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E128", Message: `Function name must start with a capital or "s:": ` + name, Span: function.Name,
			})
		}
	}
	// Vim9 script function names begin with an ASCII capital. Direct object-type
	// methods use different grammar, including private underscore names.
	if command.Dialect == Vim9 && !vim9ScriptNamespace && !directAggregateMethod && !dictFunction && !strings.Contains(name, "#") {
		capital := name[0]
		checkCapital := !strings.Contains(name, ":")
		if strings.HasPrefix(name, "g:") && len(name) > 2 {
			capital = name[2]
			checkCapital = true
		}
		if checkCapital && (capital < 'A' || capital > 'Z') {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1267", Message: "Function name must start with a capital: " + name,
				Span: function.Name,
			})
		}
	}
	beforeSpace := offset
	offset = skipSyntaxSpace(source, offset, len(source))
	spaceBeforeGeneric := defSignature && offset > beforeSpace && offset < len(source) && source[offset] == '<'
	genericInvalid := spaceBeforeGeneric
	if spaceBeforeGeneric {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1068", Message: "no white space allowed before '<'",
			Span: Span{Start: command.Argument.Start + beforeSpace, End: command.Argument.Start + offset},
		})
	}
	if vim9Context && offset > beforeSpace && offset < len(source) && source[offset] == '(' {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1068", Message: "no white space allowed before function arguments",
			Span: Span{Start: command.Argument.Start + beforeSpace, End: command.Argument.Start + offset},
		})
	}
	if defSignature && offset < len(source) && source[offset] == '<' {
		end := findMatching(source, offset, '<', '>')
		recoveredGeneric := false
		if end < 0 {
			// Vim recovers a missing generic terminator when the following
			// parenthesis is an empty argument list.  Keep the type list
			// bounded to this physical line; a non-empty parenthesis remains
			// the ordinary missing-terminator case.
			recovered := -1
			for index := offset + 1; index < len(source) && source[index] != '\n'; index++ {
				if source[index] != '(' {
					continue
				}
				close := index + 1
				for close < len(source) && source[close] != '\n' && isExpressionSpace(source[close]) {
					close++
				}
				if close < len(source) && source[close] == ')' {
					recovered = index
				}
				break
			}
			if recovered < 0 {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vimls/missing-generic-end", Message: "expected > after generic type parameters", Span: Span{Start: command.Argument.Start + offset, End: command.Argument.End}})
				command.Function = function
				return
			}
			end = recovered
			recoveredGeneric = true
		}
		var diagnostics []Diagnostic
		function.TypeParameters, diagnostics = parseFunctionTypeParameters(source, command.Argument.Start, offset, end, source[nameStart:beforeSpace])
		if recoveredGeneric && len(diagnostics) == 0 {
			span := Span{Start: command.Argument.Start + end, End: command.Argument.Start + end + 1}
			if count := len(function.TypeParameters); count > 0 {
				span = function.TypeParameters[count-1].Span
			}
			diagnostics = append(diagnostics, Diagnostic{
				Code: "vim/E1553", Message: "Missing comma after type in generic function: " + strings.TrimSpace(source[offset+1:]), Span: span,
			})
		}
		if !spaceBeforeGeneric {
			file.Diagnostics = append(file.Diagnostics, diagnostics...)
			genericInvalid = len(diagnostics) > 0
		}
		beforeSpace = end + 1
		if recoveredGeneric {
			beforeSpace = end
		}
		offset = skipSyntaxSpace(source, beforeSpace, len(source))
		if !genericInvalid && offset > beforeSpace && offset < len(source) && source[offset] == '(' {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1068", Message: "no white space allowed before function arguments",
				Span: Span{Start: command.Argument.Start + beforeSpace, End: command.Argument.Start + offset},
			})
		}
	}
	if offset >= len(source) || source[offset] != '(' {
		if offset < len(source) && source[offset] != '"' {
			end := trimSyntaxSpaceEnd(source, offset, len(source))
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E124", Message: "Missing '(': " + strings.TrimSpace(rawSource),
				Span: Span{Start: command.Argument.Start + offset, End: command.Argument.Start + end},
			})
		}
		command.Function = function
		return
	}
	open := offset
	if vim9Context && open+1 < len(rawSource) && rawSource[open+1] == '#' {
		end := strings.IndexByte(rawSource[open+1:], '\n')
		if end < 0 {
			end = len(rawSource)
		} else {
			end += open + 1
		}
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E125", Message: "Illegal argument: " + strings.TrimSpace(rawSource[open+1:end]),
			Span: Span{Start: command.Argument.Start + open + 1, End: command.Argument.Start + end},
		})
		command.Function = function
		return
	}
	close := findMatching(source, offset, '(', ')')
	if close < 0 {
		argumentStart := skipSyntaxSpace(source, offset+1, len(source))
		if command.Canonical == "function" && argumentStart >= len(source) {
			span := Span{Start: command.Argument.End, End: command.Argument.End}
			file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E125", Message: "Illegal argument: ", Span: span})
			command.Function = function
			return
		}
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
	if defSignature && offset < len(rawSource) && rawSource[offset] == ':' {
		spaceBeforeColon := spaceBeforeTail
		if spaceBeforeColon {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1059", Message: "no white space allowed before colon",
				Span: Span{Start: command.Argument.Start + tailStart, End: command.Argument.Start + offset + 1},
			})
		} else if offset+1 >= len(source) || !isExpressionSpace(source[offset+1]) {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1069", Message: "white space required after ':'",
				Span: Span{Start: command.Argument.Start + offset, End: command.Argument.Start + offset + 1},
			})
		}
		typeStart := skipSyntaxSpace(source, offset+1, len(source))
		typeEnd := trimSyntaxSpaceEnd(source, typeStart, len(source))
		function.ReturnTypeSpan = Span{Start: command.Argument.Start + typeStart, End: command.Argument.Start + typeEnd}
		if defSignature && typeStart >= typeEnd {
			// Vim reports the missing return type during :def compilation as
			// E1056. Keep the zero-width span at the point where the type would
			// begin so recovery can continue with the next physical line.
			function.ReturnType = &Type{Kind: TypeMissing, Span: function.ReturnTypeSpan}
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1056", Message: "Expected a type: ", Span: function.ReturnTypeSpan,
			})
		} else {
			function.ReturnType, file.Diagnostics = appendTypeDiagnostics(file.Diagnostics, source[typeStart:typeEnd], command.Argument.Start+typeStart)
		}
	} else if offset < len(rawSource) {
		if rawSource[offset] == '#' {
			validComment := spaceBeforeTail && vim9Context
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

// nestedGenericTypeParameterDiagnostic reports the Vim rule that a nested
// def may not reuse a type variable declared by an enclosing def.  The block
// chain is authoritative here: sibling and top-level functions are never
// considered, and legacy function blocks naturally do not contribute names.
func nestedGenericTypeParameterDiagnostic(file *File, command *Command) (Diagnostic, bool) {
	if command == nil || command.Function == nil {
		return Diagnostic{}, false
	}
	parameters := command.Function.TypeParameters
	if len(parameters) == 0 || command.Block < 0 || command.Block >= len(file.Blocks) {
		return Diagnostic{}, false
	}
	var seen map[string]struct{}
	for blockIndex := file.Blocks[command.Block].Parent; blockIndex >= 0 && blockIndex < len(file.Blocks); blockIndex = file.Blocks[blockIndex].Parent {
		block := file.Blocks[blockIndex]
		if block.Kind != BlockDef || block.Header < 0 || block.Header >= len(file.Commands) {
			continue
		}
		function := file.Commands[block.Header].Function
		if function == nil {
			continue
		}
		for _, parameter := range function.TypeParameters {
			if seen == nil {
				seen = make(map[string]struct{})
			}
			seen[parameter.Name] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return Diagnostic{}, false
	}
	for _, parameter := range parameters {
		if _, exists := seen[parameter.Name]; exists {
			return Diagnostic{Code: "vim/E1561", Message: "Duplicate type variable name: " + parameter.Name, Span: parameter.Span}, true
		}
	}
	return Diagnostic{}, false
}

func parseFunctionTypeParameters(source string, base, open, close int, functionName string) ([]TypeParameter, []Diagnostic) {
	var parameters []TypeParameter
	var diagnostics []Diagnostic
	report := func(code, message string, start, end int) {
		if len(diagnostics) == 0 {
			diagnostics = append(diagnostics, Diagnostic{Code: code, Message: message, Span: Span{Start: base + start, End: base + end}})
		}
	}
	if open+1 == close {
		report("vim/E1555", "Empty type list specified for generic function '"+functionName+"'", open, close+1)
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
				report("vim/E1552", "Type variable name must start with an uppercase letter: "+source[nameStart:], offset, offset+1)
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
			report("vim/E1561", "Duplicate type variable name: "+name, nameStart, offset)
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
			report("vim/E1553", "Missing comma after type in generic function: "+strings.TrimSpace(source[open+1:]), offset, offset+1)
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
	defSignature := command.Canonical == "def"
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
	typed := colon >= 0 && (equals < 0 || colon < equals)
	nameEnd := end
	if typed {
		nameEnd = colon
	} else if equals >= 0 {
		nameEnd = equals
	}
	nameEnd = trimSyntaxSpaceEnd(source, start, nameEnd)
	if defSignature && parameter.Variadic && nameEnd == start {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1055", Message: "Missing name after ...",
			Span: Span{Start: command.Argument.Start + variadicStart, End: command.Argument.Start + variadicStart + 3},
		})
	}
	parameter.Name = Span{Start: command.Argument.Start + start, End: command.Argument.Start + nameEnd}
	if defSignature && !parameter.Variadic {
		parameter.Target = parseConstructorParameterTarget(source, start, nameEnd, command.Argument.Start)
	}
	if nameEnd > start && parameter.Target == nil {
		name := source[start:nameEnd]
		valid := isASCIIIdentifierStart(name[0])
		for index := 1; valid && index < len(name); index++ {
			valid = isASCIIIdentifierContinue(name[index])
		}
		reservedLegacyName := !defSignature && (name == "firstline" || name == "lastline")
		if !valid || reservedLegacyName {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E125", Message: "Illegal argument: " + strings.TrimSpace(source[start:]), Span: parameter.Name,
			})
			return parameter
		}
	}
	if typed && !defSignature {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E475", Message: "invalid argument",
			Span: Span{Start: command.Argument.Start + colon, End: command.Argument.Start + end},
		})
	}
	if defSignature && !typed && equals < 0 && nameEnd > start && source[start:nameEnd] != "_" && parameter.Target == nil {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1077", Message: "Missing argument type for " + source[start:nameEnd],
			Span: Span{Start: command.Argument.Start + start, End: command.Argument.Start + nameEnd},
		})
	}
	if defSignature && parameter.Variadic && equals >= 0 {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1160", Message: "Cannot use a default for variable arguments",
			Span: Span{Start: command.Argument.Start + equals, End: command.Argument.Start + end},
		})
	}
	if defSignature && typed {
		if colon+1 >= end || !isExpressionSpace(source[colon+1]) {
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
			code, message := "vim/E125", "Illegal argument: "+strings.TrimSpace(source[start:end])
			span := parameter.DefaultSpan
			if defSignature && command.Dialect == Vim9 {
				if part.End < len(source) && source[part.End] == ')' && strings.Contains(source[equals+1:part.End], "\n") {
					message = "Illegal argument: )"
					span = Span{Start: command.Argument.Start + part.End, End: command.Argument.Start + part.End + 1}
				} else {
					code, message = "vim/E15", "invalid expression"
				}
			}
			file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: code, Message: message, Span: span})
		} else {
			dialect := Legacy
			if defSignature {
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
		switch character {
		case open:
			depth++
		case close:
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
