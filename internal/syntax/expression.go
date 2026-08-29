package syntax

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type ExpressionKind uint8

const (
	ExpressionMissing ExpressionKind = iota
	ExpressionIdentifier
	ExpressionNumber
	ExpressionString
	ExpressionList
	ExpressionDictionary
	ExpressionUnary
	ExpressionBinary
	ExpressionTernary
	ExpressionCast
	ExpressionAssignment
	ExpressionCall
	ExpressionIndex
	ExpressionSlice
	ExpressionMember
	ExpressionLambda
	ExpressionParenthesized
	ExpressionTuple
	ExpressionLambdaBlock
	ExpressionBlob
	ExpressionInterpolatedString
	ExpressionCurlyName
	ExpressionGenericReference
)

// Expression is a recovering expression node. Children remain ordered and all
// spans refer to the original document bytes.
type Expression struct {
	Kind           ExpressionKind
	Span           Span
	Operator       Span
	Value          string
	Children       []*Expression
	TypeArguments  []*Type
	CastType       *Type
	Parameters     []Parameter
	ReturnType     *Type
	ReturnTypeSpan Span
	LambdaBody     *File
}

// LegacyExpressionParser parses one legacy Vim expression.
type LegacyExpressionParser struct{}

func (LegacyExpressionParser) Parse(source string) (*Expression, []Diagnostic) {
	return parseExpression(source, 0, Legacy)
}

// Vim9ExpressionParser parses one Vim9 expression.
type Vim9ExpressionParser struct{}

func (Vim9ExpressionParser) Parse(source string) (*Expression, []Diagnostic) {
	return parseExpression(source, 0, Vim9)
}

type expressionTokenKind uint8

const (
	expressionEOF expressionTokenKind = iota
	expressionIdentifier
	expressionNumber
	expressionString
	expressionBlob
	expressionInterpolatedString
	expressionOperator
	expressionPunctuation
)

type expressionToken struct {
	kind      expressionTokenKind
	span      Span
	text      string
	malformed bool
}

// expressionLexer advances within one command argument or normalized logical
// expression.  The file scanner owns physical-line recovery; this is not a
// whole-file JavaScript-style token stream.
type expressionLexer struct {
	source  string
	base    int
	dialect Dialect
	offset  int
	current expressionToken
}

type expressionParser struct {
	source      string
	base        int
	dialect     Dialect
	lexer       expressionLexer
	diagnostics []Diagnostic
	depth       int
}

// expressionBoundary retains the expression already parsed while finding an
// Ex command boundary.  Its spans use the same source coordinates as the
// owning command: logical coordinates for a logical view, physical otherwise.
type expressionBoundary struct {
	argument    Span
	expression  *Expression
	diagnostics []Diagnostic
}

func parseExpression(source string, base int, dialect Dialect) (*Expression, []Diagnostic) {
	expression, diagnostics, consumed := parseExpressionPrefix(source, base, dialect)
	diagnostics = appendTrailingExpressionDiagnostic(diagnostics, base, consumed, len(source))
	if dialect == Vim9 {
		diagnostics = mapVim9LambdaTrailingParen(diagnostics, expression, source, base)
	}
	return expression, diagnostics
}

func newExpressionBoundary(argument Span, expression *Expression, diagnostics []Diagnostic, consumed int) *expressionBoundary {
	diagnostics = appendTrailingExpressionDiagnostic(diagnostics, argument.Start, consumed, argument.End-argument.Start)
	return &expressionBoundary{argument: argument, expression: expression, diagnostics: diagnostics}
}

func appendTrailingExpressionDiagnostic(diagnostics []Diagnostic, base, consumed, length int) []Diagnostic {
	if consumed < length {
		for _, diagnostic := range diagnostics {
			if diagnostic.Code == "vim/E1004" {
				// Vim stops the current expression at an operator-spacing error.
				// Keep the remaining bytes opaque instead of reporting a cascade.
				return diagnostics
			}
		}
		diagnostics = append(diagnostics, Diagnostic{Code: "vimls/trailing-expression", Message: "unexpected text after expression", Span: Span{Start: base + consumed, End: base + consumed + 1}})
	}
	return diagnostics
}

func mapVim9LambdaTrailingParen(diagnostics []Diagnostic, expression *Expression, source string, base int) []Diagnostic {
	if expression == nil || expression.Kind != ExpressionLambda {
		return diagnostics
	}
	for index := range diagnostics {
		diagnostic := &diagnostics[index]
		offset := diagnostic.Span.Start - base
		if diagnostic.Code == "vimls/trailing-expression" && diagnostic.Span.End == diagnostic.Span.Start+1 && offset >= 0 && offset < len(source) && source[offset] == ')' {
			diagnostic.Code = "vim/E488"
			diagnostic.Message = "trailing characters"
		}
	}
	return diagnostics
}

func parseExpressionPrefix(source string, base int, dialect Dialect) (*Expression, []Diagnostic, int) {
	parser := &expressionParser{source: source, base: base, dialect: dialect, lexer: newExpressionLexer(source, base, dialect)}
	expression := parser.parse(0)
	return expression, parser.diagnostics, parser.current().span.Start - base
}

func (p *expressionParser) parse(minimumBinding int) *Expression {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 512 {
		token := p.current()
		p.advance()
		p.diagnostics = append(p.diagnostics, Diagnostic{Code: "vimls/expression-too-deep", Message: "expression nesting exceeds parser limit", Span: token.span})
		return &Expression{Kind: ExpressionMissing, Span: token.span}
	}
	left := p.parsePrefix()
	for {
		token := p.current()
		if token.text == ":" && left.Kind == ExpressionIdentifier && len(left.Value) == 1 && strings.ContainsRune("abglstvw", rune(left.Value[0])) && token.span.Start == left.Span.End {
			next := p.peek(1)
			if next.span.Start == token.span.End && ((next.text == "[" && (p.dialect == Legacy || left.Value == "g")) || (next.text == "{" && p.dialect == Legacy) || (next.text == "." && p.dialect == Legacy)) {
				p.advance()
				left.Span.End = token.span.End
				left.Value += ":"
				continue
			}
		}
		if token.text == "?" && minimumBinding <= 10 {
			p.validateBinarySpacing(token)
			p.advance()
			whenTrue := p.parse(0)
			if p.current().text != ":" {
				code := "vimls/missing-ternary-colon"
				message := "expected : in ternary expression"
				if p.dialect == Vim9 {
					code = "vim/E109"
					message = "Missing ':' after '?'"
				}
				p.diagnostics = append(p.diagnostics, Diagnostic{Code: code, Message: message, Span: p.current().span})
				return &Expression{Kind: ExpressionTernary, Span: Span{Start: left.Span.Start, End: whenTrue.Span.End}, Operator: token.span, Children: []*Expression{left, whenTrue}}
			}
			p.validateBinarySpacing(p.current())
			p.advance()
			whenFalse := p.parse(0)
			left = &Expression{Kind: ExpressionTernary, Span: Span{Start: left.Span.Start, End: whenFalse.Span.End}, Operator: token.span, Children: []*Expression{left, whenTrue, whenFalse}}
			continue
		}
		if token.kind == expressionNumber && strings.HasPrefix(token.text, ".") && len(token.text) > 1 && p.adjacentOrContinuationLine(left.Span.End, token.span.Start) {
			p.advance()
			left = &Expression{
				Kind: ExpressionMember, Span: Span{Start: left.Span.Start, End: token.span.End},
				Operator: Span{Start: token.span.Start, End: token.span.Start + 1}, Value: token.text[1:], Children: []*Expression{left},
			}
			continue
		}
		if token.text == "->" && p.isArrowCallable(left) {
			if 100 < minimumBinding {
				break
			}
			left = p.parseArrowCallable(left)
			continue
		}
		// Legacy Vim accepts whitespace between a named function and its
		// argument list (for example, exists ("x")).  Vim9 deliberately
		// rejects that form; its expression grammar requires adjacency.
		legacyCall := p.dialect == Legacy && legacyNamedCallable(left) && p.onlyWhitespace(left.Span.End, token.span.Start)
		missingMember := token.text == "." && token.span.Start == left.Span.End && p.peek(1).kind == expressionEOF
		if (token.text == "(" && (token.span.Start == left.Span.End || legacyCall)) || token.text == "[" && token.span.Start == left.Span.End || token.text == "." && (p.isMember(left) || missingMember) || token.text == "->" && p.isMethod(left) {
			if 100 < minimumBinding {
				break
			}
			left = p.parsePostfix(left)
			continue
		}
		if token.text == "{" && p.dialect == Legacy && token.span.Start == left.Span.End && minimumBinding <= 100 {
			left = p.parseCurlyName(left)
			continue
		}
		if token.text == "<" && minimumBinding <= 100 {
			if call, ok := p.parseGenericCall(left); ok {
				left = call
				continue
			}
		}
		leftBinding, rightBinding, ok := infixBinding(token.text, p.dialect)
		if !ok || leftBinding < minimumBinding {
			break
		}
		p.advance()
		p.validateBinarySpacing(token)
		if p.dialect == Vim9 && (token.text == "is#" || token.text == "is?" || token.text == "isnot#" || token.text == "isnot?") {
			p.diagnostics = append(p.diagnostics, Diagnostic{Code: "vim/E15", Message: "case modifiers are not allowed with is or isnot in Vim9 script", Span: token.span})
		}
		right := p.parse(rightBinding)
		left = &Expression{
			Kind: ExpressionBinary, Span: Span{Start: left.Span.Start, End: right.Span.End},
			Operator: token.span, Value: token.text, Children: []*Expression{left, right},
		}
	}
	return left
}

func legacyNamedCallable(expression *Expression) bool {
	if expression == nil {
		return false
	}
	switch expression.Kind {
	case ExpressionIdentifier, ExpressionCurlyName:
		return true
	case ExpressionMember:
		return len(expression.Children) == 1 && legacyNamedCallable(expression.Children[0])
	default:
		return false
	}
}

func (p *expressionParser) parseArrowCallable(left *Expression) *Expression {
	arrow := p.current()
	p.advance()
	callable := p.parsePrefix()
	if p.current().text != "(" || p.current().span.Start != callable.Span.End {
		code := "vimls/missing-method-call"
		message := "expected argument list after callable"
		if p.dialect == Vim9 && callable.Kind == ExpressionParenthesized && len(callable.Children) == 1 && callable.Children[0].Kind == ExpressionLambda {
			code = "vim/E107"
			message = "Missing parentheses: lambda"
		}
		p.diagnostics = append(p.diagnostics, Diagnostic{Code: code, Message: message, Span: p.current().span})
		return &Expression{Kind: ExpressionCall, Span: Span{Start: left.Span.Start, End: callable.Span.End}, Operator: arrow.span, Value: "->", Children: []*Expression{callable, left}}
	}
	open := p.current()
	p.advance()
	children := []*Expression{callable, left}
	for p.current().kind != expressionEOF && p.current().text != ")" {
		children = append(children, p.parse(0))
		if p.current().text != "," {
			break
		}
		p.advance()
	}
	end := p.consumeClosing(")", open.span.End)
	return &Expression{Kind: ExpressionCall, Span: Span{Start: left.Span.Start, End: end}, Operator: arrow.span, Value: "->", Children: children}
}

func (p *expressionParser) parseCurlyName(left *Expression) *Expression {
	open := p.current()
	p.advance()
	dynamic := p.parse(0)
	end := p.consumeClosing("}", dynamic.Span.End)
	children := []*Expression{left, dynamic}
	if p.current().kind == expressionIdentifier && p.current().span.Start == end {
		suffix := p.current()
		p.advance()
		children = append(children, &Expression{Kind: ExpressionIdentifier, Span: suffix.span, Value: suffix.text})
		end = suffix.span.End
	} else if p.current().text == "#" && p.current().span.Start == end {
		// A function-name component following a dynamic component is lexed
		// as '#' plus an identifier (for example #{method}#init).  Vim
		// treats both bytes as one name component.
		hash := p.current()
		suffix := p.peek(1)
		if suffix.kind == expressionIdentifier && suffix.span.Start == p.current().span.End {
			p.advance()
			p.advance()
			children = append(children, &Expression{Kind: ExpressionIdentifier, Span: Span{Start: hash.span.Start, End: suffix.span.End}, Value: p.source[hash.span.Start-p.base : suffix.span.End-p.base]})
			end = suffix.span.End
		}
	}
	return &Expression{Kind: ExpressionCurlyName, Span: Span{Start: left.Span.Start, End: end}, Operator: open.span, Children: children}
}

func (p *expressionParser) parseGenericCall(left *Expression) (*Expression, bool) {
	if p.dialect != Vim9 || p.current().span.Start != left.Span.End {
		return nil, false
	}
	open := p.current().span.Start - p.base
	close := findGenericTypeEnd(p.source, open)
	if close < 0 {
		return nil, false
	}
	closingEnd := p.base + close + 1
	if !p.advancePastSourceEnd(closingEnd) {
		return nil, false
	}

	var types []*Type
	for _, part := range splitTopLevel(p.source, open+1, close, ',') {
		start := skipSpace(p.source, part.Start, part.End)
		end := trimSpaceEnd(p.source, start, part.End)
		typeNode, diagnostics := parseTypeAt(p.source[start:end], p.base+start)
		types = append(types, typeNode)
		p.diagnostics = append(p.diagnostics, diagnostics...)
	}
	operator := Span{Start: p.base + open, End: closingEnd}
	if p.current().text != "(" || p.current().span.Start != closingEnd {
		return &Expression{
			Kind: ExpressionGenericReference, Span: Span{Start: left.Span.Start, End: closingEnd},
			Operator: operator, Children: []*Expression{left}, TypeArguments: types,
		}, true
	}
	call := p.parsePostfix(left)
	call.Operator = operator
	call.TypeArguments = types
	return call, true
}

func findGenericTypeEnd(source string, open int) int {
	if open >= len(source) || source[open] != '<' {
		return -1
	}
	depth := 0
	for index := open; index < len(source); index++ {
		switch source[index] {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func (p *expressionParser) parsePrefix() *Expression {
	token := p.current()
	if token.kind == expressionEOF {
		p.diagnostics = append(p.diagnostics, Diagnostic{Code: "vimls/missing-expression", Message: "expected expression", Span: token.span})
		return &Expression{Kind: ExpressionMissing, Span: token.span}
	}
	if token.text == "!" || token.text == "+" || token.text == "-" {
		p.advance()
		operand := p.parse(90)
		return &Expression{Kind: ExpressionUnary, Span: Span{Start: token.span.Start, End: operand.Span.End}, Operator: token.span, Value: token.text, Children: []*Expression{operand}}
	}
	if p.dialect == Vim9 && token.text == "<" {
		openOffset := token.span.Start - p.base
		typeStart := openOffset + 1
		if typeStart < len(p.source) {
			first, _ := utf8.DecodeRuneInString(p.source[typeStart:])
			if first == '_' || unicode.IsLetter(first) {
				typeParser := typeParser{source: p.source[typeStart:], base: p.base + typeStart}
				typeNode := typeParser.parseType()
				p.diagnostics = append(p.diagnostics, typeParser.diagnostics...)
				closeOffset := typeStart + typeParser.offset
				if closeOffset < len(p.source) && p.source[closeOffset] == '>' && typeNode.Span.End == p.base+closeOffset && p.advancePastSourceEnd(p.base+closeOffset+1) {
					value := p.parse(90)
					return &Expression{Kind: ExpressionCast, Span: Span{Start: token.span.Start, End: value.Span.End}, Operator: Span{Start: token.span.Start, End: p.base + closeOffset + 1}, Value: typeNode.Name, CastType: typeNode, Children: []*Expression{value}}
				}
				code := "vim/E1104"
				message := "missing >"
				diagnosticSpan := Span{Start: typeNode.Span.End, End: typeNode.Span.End}
				if closeOffset < len(p.source) && p.source[closeOffset] == '>' {
					code = "vim/E1068"
					message = "no white space allowed before '>'"
					diagnosticSpan = Span{Start: typeNode.Span.End, End: p.base + closeOffset + 1}
				}
				p.diagnostics = append(p.diagnostics, Diagnostic{Code: code, Message: message, Span: diagnosticSpan})
				for p.current().kind != expressionEOF {
					p.advance()
				}
				operator := Span{Start: token.span.Start, End: typeNode.Span.End}
				return &Expression{Kind: ExpressionCast, Span: operator, Operator: operator, Value: typeNode.Name, CastType: typeNode}
			}
		}
	}
	switch token.kind {
	case expressionIdentifier:
		p.advance()
		if p.dialect == Vim9 && strings.HasPrefix(token.text, "@") {
			if len(token.text) == 1 {
				p.diagnostics = append(p.diagnostics, Diagnostic{Code: "vim/E1002", Message: "Syntax error at @", Span: token.span})
			} else {
				name, size := utf8.DecodeRuneInString(token.text[1:])
				if !validRegisterName(name) {
					p.diagnostics = append(p.diagnostics, Diagnostic{
						Code: "vim/E354", Message: "Invalid register name: '" + string(name) + "'",
						Span: Span{Start: token.span.Start + 1, End: token.span.Start + 1 + size},
					})
				}
			}
		}
		return &Expression{Kind: ExpressionIdentifier, Span: token.span, Value: token.text}
	case expressionNumber:
		p.advance()
		if token.malformed {
			p.diagnostics = append(p.diagnostics, Diagnostic{Code: "vim/E15", Message: "invalid expression", Span: token.span})
		}
		return &Expression{Kind: ExpressionNumber, Span: token.span, Value: token.text}
	case expressionString:
		p.advance()
		return &Expression{Kind: ExpressionString, Span: token.span, Value: token.text}
	case expressionBlob:
		p.advance()
		if blobLiteralHasIncompleteByte(token.text) {
			p.diagnostics = append(p.diagnostics, Diagnostic{
				Code: "vim/E973", Message: "Blob literal should have an even number of hex characters", Span: token.span,
			})
		}
		return &Expression{Kind: ExpressionBlob, Span: token.span, Value: token.text}
	case expressionInterpolatedString:
		p.advance()
		return p.parseInterpolatedString(token)
	}
	switch token.text {
	case "(":
		return p.parseParenthesized()
	case "[":
		return p.parseList()
	case "{":
		if p.dialect == Legacy && p.startsLeadingCurlyName() {
			return p.parseLeadingCurlyName()
		}
		return p.parseDictionaryOrLambda()
	case "#{":
		return p.parseDictionaryOrLambda()
	}
	p.advance()
	p.diagnostics = append(p.diagnostics, Diagnostic{Code: "vimls/unexpected-token", Message: "unexpected token in expression", Span: token.span})
	return &Expression{Kind: ExpressionMissing, Span: token.span}
}

func validRegisterName(name rune) bool {
	return name >= 'A' && name <= 'Z' || name >= 'a' && name <= 'z' || name >= '0' && name <= '9' || strings.ContainsRune(`"-_/#.%:=*+~`, name)
}

func blobLiteralHasIncompleteByte(literal string) bool {
	for index := 2; index < len(literal) && isLiteralDigit(literal[index]); {
		if index+1 >= len(literal) || !isLiteralDigit(literal[index+1]) {
			return true
		}
		index += 2
		if index+1 < len(literal) && literal[index] == '.' && isLiteralDigit(literal[index+1]) {
			index++
		}
	}
	return false
}

// startsLeadingCurlyName distinguishes a legacy curly-braces variable name
// such as {prefix}name from a dictionary or lambda literal.  The latter has
// no identifier immediately following its matching closing brace.  Looking
// at expression tokens keeps nested brace expressions and quoted strings out
// of the delimiter scan while preserving the original byte spans.
func (p *expressionParser) startsLeadingCurlyName() bool {
	if p.current().text != "{" {
		return false
	}
	depth := 0
	cursor := p.lexer
	for cursor.current.kind != expressionEOF {
		token := cursor.current
		switch token.text {
		case "{":
			depth++
		case "}":
			depth--
			if depth == 0 {
				nextCursor := cursor
				nextCursor.advance()
				next := nextCursor.current
				if next.span.Start != token.span.End {
					return false
				}
				if next.kind == expressionIdentifier {
					return true
				}
				// find_name_end() accepts a computed namespace followed by
				// a scope colon, for example {scope[i]}:{name}.
				nextCursor.advance()
				after := nextCursor.current
				return next.text == ":" && (after.text == "{" || after.kind == expressionIdentifier) && after.span.Start == next.span.End
			}
		}
		cursor.advance()
	}
	return false
}

func (p *expressionParser) parseLeadingCurlyName() *Expression {
	open := p.current()
	p.advance()
	dynamic := p.parse(0)
	end := p.consumeClosing("}", dynamic.Span.End)
	children := []*Expression{dynamic}
	if p.current().kind == expressionIdentifier && p.current().span.Start == end {
		suffix := p.current()
		p.advance()
		children = append(children, &Expression{Kind: ExpressionIdentifier, Span: suffix.span, Value: suffix.text})
		end = suffix.span.End
	} else if p.current().text == ":" && p.current().span.Start == end {
		colon := p.current()
		p.advance()
		children = append(children, &Expression{Kind: ExpressionIdentifier, Span: colon.span, Value: colon.text})
		end = colon.span.End
		if p.current().kind == expressionIdentifier && p.current().span.Start == end {
			suffix := p.current()
			p.advance()
			children = append(children, &Expression{Kind: ExpressionIdentifier, Span: suffix.span, Value: suffix.text})
			end = suffix.span.End
		}
	}
	return &Expression{Kind: ExpressionCurlyName, Span: Span{Start: open.span.Start, End: end}, Operator: open.span, Children: children}
}

func (p *expressionParser) advancePastSourceEnd(end int) bool {
	cursor := p.lexer
	for cursor.current.kind != expressionEOF && cursor.current.span.End < end {
		cursor.advance()
	}
	if cursor.current.span.End != end {
		return false
	}
	cursor.advance()
	p.lexer = cursor
	return true
}

func (p *expressionParser) parseInterpolatedString(token expressionToken) *Expression {
	node := &Expression{Kind: ExpressionInterpolatedString, Span: token.span, Value: token.text}
	start := token.span.Start - p.base + 2
	end := token.span.End - p.base - 1
	for index := start; index < end; index++ {
		if p.source[index] == '{' && index+1 < end && p.source[index+1] == '{' {
			index++
			continue
		}
		if p.source[index] != '{' {
			continue
		}
		close := findInterpolationEnd(p.source, index, end)
		if close < 0 {
			p.diagnostics = append(p.diagnostics, Diagnostic{Code: "vimls/missing-interpolation-end", Message: "expected } in interpolated string", Span: Span{Start: p.base + index, End: p.base + index + 1}})
			break
		}
		expressionStart := skipExpressionSpace(p.source, index+1)
		expressionEnd := trimExpressionSpaceEnd(p.source, expressionStart, close)
		child, diagnostics := parseExpression(p.source[expressionStart:expressionEnd], p.base+expressionStart, p.dialect)
		node.Children = append(node.Children, child)
		p.diagnostics = append(p.diagnostics, diagnostics...)
		index = close
	}
	return node
}

func findInterpolationEnd(source string, open, end int) int {
	depth := 1
	quote := byte(0)
	for index := open + 1; index < end; index++ {
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
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func (p *expressionParser) parsePostfix(left *Expression) *Expression {
	token := p.current()
	switch token.text {
	case "(":
		p.advance()
		children := []*Expression{left}
		for p.current().kind != expressionEOF && p.current().text != ")" {
			children = append(children, p.parse(0))
			if p.current().text != "," {
				break
			}
			p.advance()
		}
		end := p.consumeClosing(")", token.span.End)
		return &Expression{Kind: ExpressionCall, Span: Span{Start: left.Span.Start, End: end}, Children: children}
	case "[":
		p.advance()
		children := []*Expression{left}
		kind := ExpressionIndex
		hasStart := false
		if p.current().text != "]" && p.current().text != ":" {
			children = append(children, p.parse(0))
			hasStart = true
		}
		if p.current().text == ":" {
			kind = ExpressionSlice
			colon := p.current()
			p.advance()
			hasEnd := p.current().text != "]"
			p.validateSliceSpacing(colon, hasStart, hasEnd)
			if hasEnd {
				children = append(children, p.parse(0))
			}
		}
		fallback := token.span.End
		if p.current().span.Start > fallback {
			fallback = p.current().span.Start
		}
		end := p.consumeClosing("]", fallback)
		return &Expression{Kind: kind, Span: Span{Start: left.Span.Start, End: end}, Children: children}
	case ".", "->":
		p.advance()
		member := p.current()
		if member.kind != expressionIdentifier && member.kind != expressionNumber {
			p.diagnostics = append(p.diagnostics, Diagnostic{Code: "vimls/missing-member", Message: "expected member name", Span: member.span})
			missing := &Expression{Kind: ExpressionMissing, Span: member.span}
			return &Expression{Kind: ExpressionMember, Span: Span{Start: left.Span.Start, End: token.span.End}, Operator: token.span, Children: []*Expression{left, missing}}
		}
		p.advance()
		return &Expression{Kind: ExpressionMember, Span: Span{Start: left.Span.Start, End: member.span.End}, Operator: token.span, Value: member.text, Children: []*Expression{left}}
	default:
		return left
	}
}

func (p *expressionParser) validateSliceSpacing(colon expressionToken, hasStart, hasEnd bool) {
	if p.dialect != Vim9 {
		return
	}
	offset := colon.span.Start - p.base
	validBefore := !hasStart || offset > 0 && isExpressionSpace(p.source[offset-1])
	validAfter := !hasEnd || offset+1 < len(p.source) && isExpressionSpace(p.source[offset+1])
	if !validBefore || !validAfter {
		p.diagnostics = append(p.diagnostics, Diagnostic{
			Code: "vim/E1004", Message: "white space required around : in list slice", Span: colon.span,
		})
	}
}

func (p *expressionParser) parseParenthesized() *Expression {
	open := p.current()
	if lambda, ok := p.parseVim9Lambda(open); ok {
		return lambda
	}
	p.advance()
	var children []*Expression
	hadComma := false
	tupleDiagnostic := false
	reportTupleDiagnostic := func(code, message string, token expressionToken) {
		if tupleDiagnostic {
			return
		}
		tupleDiagnostic = true
		p.diagnostics = append(p.diagnostics, Diagnostic{Code: code, Message: message, Span: token.span})
	}
	if p.current().text != ")" {
		children = append(children, p.parse(0))
		for {
			if p.current().text != "," {
				if hadComma && p.current().kind != expressionEOF && p.current().text != ")" {
					reportTupleDiagnostic("vim/E1527", "missing comma in tuple", p.current())
					for p.current().kind != expressionEOF && p.current().text != ")" {
						p.advance()
					}
				}
				break
			}
			hadComma = true
			comma := p.current()
			commaOffset := comma.span.Start - p.base
			if p.dialect == Vim9 && commaOffset > 0 && isExpressionSpace(p.source[commaOffset-1]) {
				reportTupleDiagnostic("vim/E1068", "no white space allowed before ','", comma)
			}
			p.advance()
			if p.current().text == "," {
				// A second comma denotes an empty tuple item.  Consume it so
				// recovery can continue with the next value or closing paren.
				if p.dialect == Vim9 {
					reportTupleDiagnostic("vim/E1068", "no white space allowed before ','", p.current())
				}
				p.advance()
			}
			if p.current().text == ")" {
				break
			}
			if p.current().kind == expressionEOF {
				break
			}
			if p.dialect == Vim9 && (comma.span.End-p.base >= len(p.source) || !isExpressionSpace(p.source[comma.span.End-p.base])) {
				reportTupleDiagnostic("vim/E1069", "white space required after ','", comma)
			}
			children = append(children, p.parse(0))
		}
	}
	fallback := open.span.End
	if len(children) > 0 {
		fallback = children[len(children)-1].Span.End
	}
	end := fallback
	if p.current().text == ")" {
		end = p.current().span.End
		p.advance()
	} else if hadComma {
		if !tupleDiagnostic {
			reportTupleDiagnostic("vim/E1526", "missing end of tuple ')'", p.current())
		}
	} else {
		end = p.consumeClosing(")", fallback)
	}
	if p.dialect == Vim9 && p.current().text == "=>" {
		arrow := p.current()
		p.advance()
		body := p.parse(0)
		children = append(children, body)
		return &Expression{Kind: ExpressionLambda, Span: Span{Start: open.span.Start, End: body.Span.End}, Operator: arrow.span, Children: children}
	}
	if hadComma || len(children) == 0 {
		return &Expression{Kind: ExpressionTuple, Span: Span{Start: open.span.Start, End: end}, Children: children}
	}
	if len(children) != 1 {
		p.diagnostics = append(p.diagnostics, Diagnostic{Code: "vimls/invalid-parenthesized-expression", Message: "parenthesized expression requires one value", Span: Span{Start: open.span.Start, End: end}})
	}
	return &Expression{Kind: ExpressionParenthesized, Span: Span{Start: open.span.Start, End: end}, Children: children}
}

func (p *expressionParser) parseVim9Lambda(open expressionToken) (*Expression, bool) {
	if p.dialect != Vim9 {
		return nil, false
	}
	openOffset := open.span.Start - p.base
	closeOffset := findMatching(p.source, openOffset, '(', ')')
	if closeOffset < 0 {
		return nil, false
	}
	position := skipVim9LambdaSpace(p.source, closeOffset+1)
	returnStart := -1
	returnEnd := -1
	if position < len(p.source) && p.source[position] == ':' {
		returnStart = skipVim9LambdaSpace(p.source, position+1)
		arrowOffset := strings.Index(p.source[returnStart:], "=>")
		if arrowOffset < 0 {
			return nil, false
		}
		position = returnStart + arrowOffset
		returnEnd = trimExpressionSpaceEnd(p.source, returnStart, position)
	}
	if position+1 >= len(p.source) || p.source[position:position+2] != "=>" {
		return nil, false
	}
	if hasUnescapedVim9LambdaLineBreak(p.source, openOffset, position+2) {
		p.diagnostics = append(p.diagnostics, Diagnostic{
			Code: "vim/E488", Message: "line break is not allowed in Vim9 lambda arguments", Span: Span{Start: open.span.Start, End: p.base + position + 2},
		})
	}

	lambda := &Expression{Kind: ExpressionLambda, Operator: Span{Start: p.base + position, End: p.base + position + 2}}
	for _, part := range splitTopLevel(p.source, openOffset+1, closeOffset, ',') {
		start := skipVim9LambdaSpace(p.source, part.Start)
		end := trimExpressionSpaceEnd(p.source, start, part.End)
		if start >= end {
			continue
		}
		parameter := Parameter{}
		if strings.HasPrefix(p.source[start:end], "...") {
			parameter.Variadic = true
			start += 3
		}
		nameEnd := scanWord(p.source, start, end)
		parameter.Name = Span{Start: p.base + start, End: p.base + nameEnd}
		lambda.Children = append(lambda.Children, &Expression{Kind: ExpressionIdentifier, Span: parameter.Name, Value: p.source[start:nameEnd]})
		typeColon := findTopLevelByte(p.source, nameEnd, end, ':')
		if typeColon >= 0 {
			typeStart := skipVim9LambdaSpace(p.source, typeColon+1)
			typeEnd := trimExpressionSpaceEnd(p.source, typeStart, end)
			parameter.TypeSpan = Span{Start: p.base + typeStart, End: p.base + typeEnd}
			parameter.Type, p.diagnostics = appendTypeDiagnostics(p.diagnostics, p.source[typeStart:typeEnd], p.base+typeStart)
		}
		lambda.Parameters = append(lambda.Parameters, parameter)
	}
	if returnStart >= 0 {
		lambda.ReturnTypeSpan = Span{Start: p.base + returnStart, End: p.base + returnEnd}
		if returnStart == returnEnd {
			// Vim reports a missing return type specifically for a lambda whose
			// return-type colon is followed immediately by the arrow.  Keep the
			// missing type in the AST at the zero-width insertion point so the
			// arrow and body remain available for recovery and tooling.
			lambda.ReturnType = &Type{Kind: TypeMissing, Span: lambda.ReturnTypeSpan}
			p.diagnostics = append(p.diagnostics, Diagnostic{
				Code: "vim/E1157", Message: "missing return type", Span: lambda.ReturnTypeSpan,
			})
		} else {
			lambda.ReturnType, p.diagnostics = appendTypeDiagnostics(p.diagnostics, p.source[returnStart:returnEnd], p.base+returnStart)
		}
	}

	arrowEnd := p.base + position + 2
	for p.current().kind != expressionEOF && p.current().span.End <= arrowEnd {
		p.advance()
	}
	if p.current().text == "{" {
		blockStart := p.current().span.Start - p.base
		blockEnd := findVim9LambdaBlockEnd(p.source, blockStart)
		if blockEnd >= 0 {
			bodyStart := blockStart + 1
			body := (Vim9Parser{}).Parse(p.source[bodyStart:blockEnd])
			// Vim9Parser parses the block slice independently.  Rebase the
			// complete result once before exposing it: commands, tokens, blocks,
			// diagnostics, typed AST, embedded command lists, and nested lambdas
			// must all use the containing expression's byte coordinates.
			bodyOffset, ok := safeLambdaOffset(p.base, bodyStart)
			if !ok {
				// A caller-supplied base should always describe a real source
				// position.  Keep recovery non-panicking even for malformed input
				// supplied by an embedding package.
				bodyOffset = 0
			}
			rebaseLambdaFile(body, p.source, bodyOffset)
			lambda.LambdaBody = body
			p.diagnostics = append(p.diagnostics, body.Diagnostics...)
			block := &Expression{Kind: ExpressionLambdaBlock, Span: Span{Start: p.base + blockStart, End: p.base + blockEnd + 1}}
			lambda.Children = append(lambda.Children, block)
			lambda.Span = Span{Start: open.span.Start, End: block.Span.End}
			closingEnd := p.base + blockEnd + 1
			for p.current().kind != expressionEOF && p.current().span.End <= closingEnd {
				p.advance()
			}
			return lambda, true
		}
	}
	body := p.parse(0)
	lambda.Children = append(lambda.Children, body)
	lambda.Span = Span{Start: open.span.Start, End: body.Span.End}
	return lambda, true
}

// rebaseLambdaFile moves a parser result produced from a block slice back to
// the source coordinates of the containing expression.  The parser graph has
// shared expression pointers (for example an assignment target is present in
// both Targets and Expressions), so the walk is identity-aware and each node
// is shifted at most once.
func rebaseLambdaFile(file *File, source string, offset int) {
	if file == nil {
		return
	}
	state := lambdaRebaseState{
		expressions: make(map[*Expression]bool),
		types:       make(map[*Type]bool),
		files:       make(map[*File]bool),
		lists:       make(map[*CommandList]bool),
	}
	state.file(file, source, offset)
}

// normalizeLambdaBodySources makes every lambda body a view of the complete
// containing file.  Expression parsing is also used on command-argument
// slices, so the parser cannot know that outer source by itself; parseSource
// calls this after all command details have been attached.
func normalizeLambdaBodySources(file *File) {
	if file == nil {
		return
	}
	state := lambdaRebaseState{
		expressions: make(map[*Expression]bool),
		types:       make(map[*Type]bool),
		files:       make(map[*File]bool),
		lists:       make(map[*CommandList]bool),
	}
	state.sourceFile(file, file.Source)
}

type lambdaRebaseState struct {
	expressions map[*Expression]bool
	types       map[*Type]bool
	files       map[*File]bool
	lists       map[*CommandList]bool
}

func (s *lambdaRebaseState) sourceFile(file *File, source string) {
	if file == nil || s.files[file] {
		return
	}
	s.files[file] = true
	file.Source = source
	for index := range file.Commands {
		s.sourceCommand(&file.Commands[index], source)
	}
}

func (s *lambdaRebaseState) sourceCommand(command *Command, source string) {
	if command == nil {
		return
	}
	if command.Embedded != nil {
		s.sourceList(command.Embedded, source)
	}
	if command.Declaration != nil {
		s.sourceExpression(command.Declaration.Target, source)
		s.sourceExpression(command.Declaration.Initializer, source)
	}
	for _, expression := range command.Expressions {
		s.sourceExpression(expression, source)
	}
	for _, target := range command.Targets {
		s.sourceExpression(target, source)
	}
	for _, value := range command.EnumValues {
		s.sourceExpression(value.Initializer, source)
		for _, argument := range value.Arguments {
			s.sourceExpression(argument, source)
		}
	}
	if command.Import != nil {
		s.sourceExpression(command.Import.Path, source)
	}
	if command.For != nil {
		s.sourceExpression(command.For.Iterable, source)
	}
	if command.Function != nil {
		for _, parameter := range command.Function.Parameters {
			s.sourceExpression(parameter.Target, source)
			s.sourceExpression(parameter.Default, source)
		}
	}
}

func (s *lambdaRebaseState) sourceList(list *CommandList, source string) {
	if list == nil || s.lists[list] {
		return
	}
	s.lists[list] = true
	for index := range list.Commands {
		s.sourceCommand(&list.Commands[index], source)
	}
}

func (s *lambdaRebaseState) sourceExpression(expression *Expression, source string) {
	if expression == nil || s.expressions[expression] {
		return
	}
	s.expressions[expression] = true
	if expression.LambdaBody != nil {
		s.sourceFile(expression.LambdaBody, source)
	}
	for _, child := range expression.Children {
		s.sourceExpression(child, source)
	}
	for _, parameter := range expression.Parameters {
		s.sourceExpression(parameter.Default, source)
	}
}

func (s *lambdaRebaseState) file(file *File, source string, offset int) {
	if file == nil || s.files[file] {
		return
	}
	s.files[file] = true
	file.Source = source
	for index := range file.Commands {
		s.command(&file.Commands[index], source, offset)
	}
	for index := range file.Tokens {
		file.Tokens[index].Span = shiftLambdaSpan(file.Tokens[index].Span, offset)
	}
	for index := range file.Blocks {
		file.Blocks[index].Span = shiftLambdaSpan(file.Blocks[index].Span, offset)
	}
	for index := range file.Diagnostics {
		file.Diagnostics[index].Span = shiftLambdaSpan(file.Diagnostics[index].Span, offset)
	}
}

func (s *lambdaRebaseState) command(command *Command, source string, offset int) {
	if command == nil {
		return
	}
	command.Span = shiftLambdaSpan(command.Span, offset)
	command.Range = shiftLambdaSpan(command.Range, offset)
	command.Name = shiftLambdaSpan(command.Name, offset)
	command.Bang = shiftLambdaOptionalSpan(command.Bang, offset)
	command.Count = shiftLambdaOptionalSpan(command.Count, offset)
	command.Argument = shiftLambdaSpan(command.Argument, offset)
	for index := range command.Modifiers {
		modifier := &command.Modifiers[index]
		modifier.Span = shiftLambdaSpan(modifier.Span, offset)
		modifier.Bang = shiftLambdaOptionalSpan(modifier.Bang, offset)
		if modifier.Filter != nil {
			modifier.Filter.Delimiter = shiftLambdaOptionalSpan(modifier.Filter.Delimiter, offset)
			modifier.Filter.Pattern = shiftLambdaOptionalSpan(modifier.Filter.Pattern, offset)
			modifier.Filter.Flags = shiftLambdaOptionalSpan(modifier.Filter.Flags, offset)
		}
	}
	if command.Autocmd != nil {
		autocmd := command.Autocmd
		autocmd.Head = shiftLambdaOptionalSpan(autocmd.Head, offset)
		autocmd.Group = shiftLambdaOptionalSpan(autocmd.Group, offset)
		autocmd.Pattern = shiftLambdaOptionalSpan(autocmd.Pattern, offset)
		for index := range autocmd.Events {
			autocmd.Events[index] = shiftLambdaSpan(autocmd.Events[index], offset)
		}
		for index := range autocmd.Modifiers {
			autocmd.Modifiers[index].Span = shiftLambdaSpan(autocmd.Modifiers[index].Span, offset)
		}
	}
	if command.Embedded != nil {
		s.commandList(command.Embedded, source, offset)
	}
	if command.Declaration != nil {
		declaration := command.Declaration
		declaration.Name = shiftLambdaSpan(declaration.Name, offset)
		declaration.Type = shiftLambdaOptionalSpan(declaration.Type, offset)
		declaration.Assignment = shiftLambdaOptionalSpan(declaration.Assignment, offset)
		s.expression(declaration.Target, source, offset)
		s.expression(declaration.Initializer, source, offset)
		s.typeNode(declaration.ParsedType, source, offset)
		for index := range declaration.Bindings {
			s.binding(&declaration.Bindings[index], source, offset)
		}
	}
	for _, expression := range command.Expressions {
		s.expression(expression, source, offset)
	}
	for _, target := range command.Targets {
		s.expression(target, source, offset)
	}
	if command.Function != nil {
		function := command.Function
		function.Name = shiftLambdaSpan(function.Name, offset)
		function.ReturnTypeSpan = shiftLambdaOptionalSpan(function.ReturnTypeSpan, offset)
		function.Attributes = shiftLambdaOptionalSpan(function.Attributes, offset)
		s.typeNode(function.ReturnType, source, offset)
		for index := range function.TypeParameters {
			function.TypeParameters[index].Span = shiftLambdaSpan(function.TypeParameters[index].Span, offset)
		}
		for index := range function.Parameters {
			s.parameter(&function.Parameters[index], source, offset)
		}
	}
	if command.Aggregate != nil {
		aggregate := command.Aggregate
		aggregate.Name = shiftLambdaSpan(aggregate.Name, offset)
		for index := range aggregate.Extends {
			aggregate.Extends[index] = shiftLambdaSpan(aggregate.Extends[index], offset)
		}
		for index := range aggregate.Implements {
			aggregate.Implements[index] = shiftLambdaSpan(aggregate.Implements[index], offset)
		}
	}
	if command.TypeAlias != nil {
		alias := command.TypeAlias
		alias.Name = shiftLambdaSpan(alias.Name, offset)
		alias.Assignment = shiftLambdaOptionalSpan(alias.Assignment, offset)
		alias.TypeSpan = shiftLambdaOptionalSpan(alias.TypeSpan, offset)
		s.typeNode(alias.Type, source, offset)
	}
	for index := range command.EnumValues {
		value := &command.EnumValues[index]
		value.Name = shiftLambdaSpan(value.Name, offset)
		s.expression(value.Initializer, source, offset)
		for _, argument := range value.Arguments {
			s.expression(argument, source, offset)
		}
	}
	if command.Import != nil {
		importNode := command.Import
		importNode.PathSpan = shiftLambdaOptionalSpan(importNode.PathSpan, offset)
		importNode.Alias = shiftLambdaOptionalSpan(importNode.Alias, offset)
		s.expression(importNode.Path, source, offset)
	}
	if command.For != nil {
		loop := command.For
		loop.IterableSpan = shiftLambdaOptionalSpan(loop.IterableSpan, offset)
		s.expression(loop.Iterable, source, offset)
		for index := range loop.Bindings {
			s.binding(&loop.Bindings[index], source, offset)
		}
	}
	if command.Heredoc != nil {
		command.Heredoc.Body = shiftLambdaOptionalSpan(command.Heredoc.Body, offset)
		command.Heredoc.EndMarker = shiftLambdaOptionalSpan(command.Heredoc.EndMarker, offset)
	}
	if command.Keymap != nil {
		command.Keymap.Body = shiftLambdaSpan(command.Keymap.Body, offset)
		for index := range command.Keymap.Entries {
			command.Keymap.Entries[index].From = shiftLambdaSpan(command.Keymap.Entries[index].From, offset)
			command.Keymap.Entries[index].To = shiftLambdaSpan(command.Keymap.Entries[index].To, offset)
		}
	}
	if command.Substitute != nil {
		substitute := command.Substitute
		substitute.Delimiter = shiftLambdaOptionalSpan(substitute.Delimiter, offset)
		substitute.Pattern = shiftLambdaOptionalSpan(substitute.Pattern, offset)
		substitute.PatternDelimiter = shiftLambdaOptionalSpan(substitute.PatternDelimiter, offset)
		substitute.Replacement = shiftLambdaOptionalSpan(substitute.Replacement, offset)
		substitute.ReplacementDelimiter = shiftLambdaOptionalSpan(substitute.ReplacementDelimiter, offset)
		substitute.Flags = shiftLambdaOptionalSpan(substitute.Flags, offset)
		substitute.Count = shiftLambdaOptionalSpan(substitute.Count, offset)
		substitute.PreviousPattern = shiftLambdaOptionalSpan(substitute.PreviousPattern, offset)
		substitute.ReplacementPrefix = shiftLambdaOptionalSpan(substitute.ReplacementPrefix, offset)
		substitute.ExpressionSpan = shiftLambdaOptionalSpan(substitute.ExpressionSpan, offset)
	}
	if command.Highlight != nil {
		highlight := command.Highlight
		highlight.Default = shiftLambdaOptionalSpan(highlight.Default, offset)
		highlight.Operation = shiftLambdaOptionalSpan(highlight.Operation, offset)
		highlight.Group = shiftLambdaOptionalSpan(highlight.Group, offset)
		highlight.LinkTarget = shiftLambdaOptionalSpan(highlight.LinkTarget, offset)
		for index := range highlight.Attributes {
			attribute := &highlight.Attributes[index]
			attribute.Key = shiftLambdaOptionalSpan(attribute.Key, offset)
			attribute.Equal = shiftLambdaOptionalSpan(attribute.Equal, offset)
			attribute.Value = shiftLambdaOptionalSpan(attribute.Value, offset)
		}
	}
	if command.Syntax != nil {
		syntax := command.Syntax
		syntax.Subcommand = shiftLambdaOptionalSpan(syntax.Subcommand, offset)
		syntax.Group = shiftLambdaOptionalSpan(syntax.Group, offset)
		for index := range syntax.Keywords {
			syntax.Keywords[index] = shiftLambdaOptionalSpan(syntax.Keywords[index], offset)
		}
		for index := range syntax.Options {
			option := &syntax.Options[index]
			option.Name = shiftLambdaOptionalSpan(option.Name, offset)
			option.Equal = shiftLambdaOptionalSpan(option.Equal, offset)
			option.Value = shiftLambdaOptionalSpan(option.Value, offset)
			for item := range option.Values {
				option.Values[item] = shiftLambdaOptionalSpan(option.Values[item], offset)
			}
		}
		for index := range syntax.Patterns {
			pattern := &syntax.Patterns[index]
			pattern.Key = shiftLambdaOptionalSpan(pattern.Key, offset)
			pattern.Equal = shiftLambdaOptionalSpan(pattern.Equal, offset)
			pattern.OpenDelimiter = shiftLambdaOptionalSpan(pattern.OpenDelimiter, offset)
			pattern.Pattern = shiftLambdaOptionalSpan(pattern.Pattern, offset)
			pattern.CloseDelimiter = shiftLambdaOptionalSpan(pattern.CloseDelimiter, offset)
			pattern.Offsets = shiftLambdaOptionalSpan(pattern.Offsets, offset)
		}
	}
	if command.Set != nil {
		for index := range command.Set.Options {
			option := &command.Set.Options[index]
			option.Span = shiftLambdaSpan(option.Span, offset)
			option.Prefix = shiftLambdaOptionalSpan(option.Prefix, offset)
			option.Name = shiftLambdaOptionalSpan(option.Name, offset)
			option.Operator = shiftLambdaOptionalSpan(option.Operator, offset)
			option.Value = shiftLambdaOptionalSpan(option.Value, offset)
		}
	}
}

func (s *lambdaRebaseState) commandList(list *CommandList, source string, offset int) {
	if list == nil || s.lists[list] {
		return
	}
	s.lists[list] = true
	list.Span = shiftLambdaSpan(list.Span, offset)
	for index := range list.Commands {
		s.command(&list.Commands[index], source, offset)
	}
	for index := range list.Blocks {
		list.Blocks[index].Span = shiftLambdaSpan(list.Blocks[index].Span, offset)
	}
}

func (s *lambdaRebaseState) binding(binding *Binding, source string, offset int) {
	if binding == nil {
		return
	}
	binding.Name = shiftLambdaSpan(binding.Name, offset)
	binding.Type = shiftLambdaOptionalSpan(binding.Type, offset)
	s.typeNode(binding.ParsedType, source, offset)
}

func (s *lambdaRebaseState) parameter(parameter *Parameter, source string, offset int) {
	if parameter == nil {
		return
	}
	parameter.Name = shiftLambdaSpan(parameter.Name, offset)
	parameter.TypeSpan = shiftLambdaOptionalSpan(parameter.TypeSpan, offset)
	parameter.DefaultSpan = shiftLambdaOptionalSpan(parameter.DefaultSpan, offset)
	s.typeNode(parameter.Type, source, offset)
	s.expression(parameter.Target, source, offset)
	s.expression(parameter.Default, source, offset)
}

func (s *lambdaRebaseState) typeNode(node *Type, source string, offset int) {
	if node == nil || s.types[node] {
		return
	}
	s.types[node] = true
	node.Span = shiftLambdaSpan(node.Span, offset)
	for _, argument := range node.Arguments {
		s.typeNode(argument, source, offset)
	}
	s.typeNode(node.ReturnType, source, offset)
}

func (s *lambdaRebaseState) expression(expression *Expression, source string, offset int) {
	if expression == nil || s.expressions[expression] {
		return
	}
	s.expressions[expression] = true
	// Lambda bodies are rebased when they are created.  Their spans therefore
	// already use the containing parser's coordinates; when that containing
	// parser is itself a block slice, only the enclosing slice offset remains.
	if expression.LambdaBody != nil {
		s.file(expression.LambdaBody, source, offset)
	}
	expression.Span = shiftLambdaSpan(expression.Span, offset)
	expression.Operator = shiftLambdaOptionalSpan(expression.Operator, offset)
	expression.ReturnTypeSpan = shiftLambdaOptionalSpan(expression.ReturnTypeSpan, offset)
	for _, child := range expression.Children {
		s.expression(child, source, offset)
	}
	for _, typeArgument := range expression.TypeArguments {
		s.typeNode(typeArgument, source, offset)
	}
	s.typeNode(expression.CastType, source, offset)
	s.typeNode(expression.ReturnType, source, offset)
	for index := range expression.Parameters {
		s.parameter(&expression.Parameters[index], source, offset)
	}
}

func shiftLambdaOptionalSpan(span Span, offset int) Span {
	if span.Start == 0 && span.End == 0 {
		return span
	}
	return shiftLambdaSpan(span, offset)
}

func shiftLambdaSpan(span Span, offset int) Span {
	start, ok := safeLambdaOffset(span.Start, offset)
	if !ok {
		return span
	}
	end, ok := safeLambdaOffset(span.End, offset)
	if !ok {
		return span
	}
	return Span{Start: start, End: end}
}

func safeLambdaOffset(value, offset int) (int, bool) {
	if offset > 0 && value > int(^uint(0)>>1)-offset {
		return value, false
	}
	if offset < 0 && value < -int(^uint(0)>>1)-1-offset {
		return value, false
	}
	return value + offset, true
}

func findVim9LambdaBlockEnd(source string, open int) int {
	if open >= len(source) || source[open] != '{' {
		return -1
	}
	depth := 0
	quote := byte(0)
	for index := open; index < len(source); index++ {
		character := source[index]
		if quote != 0 {
			if character == '\\' && quote == '"' && index+1 < len(source) {
				index++
			} else if character == quote {
				if quote == '\'' && index+1 < len(source) && source[index+1] == '\'' {
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
		if character == '#' {
			for index < len(source) && source[index] != '\n' {
				index++
			}
			continue
		}
		switch character {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func skipExpressionSpace(source string, offset int) int {
	for offset < len(source) && isExpressionSpace(source[offset]) {
		offset++
	}
	return offset
}

func skipVim9LambdaSpace(source string, offset int) int {
	for offset < len(source) {
		if isExpressionSpace(source[offset]) {
			offset++
			continue
		}
		if isLineLeadingBackslash(source, offset) {
			offset++
			continue
		}
		break
	}
	return offset
}

func hasUnescapedVim9LambdaLineBreak(source string, start, end int) bool {
	for index := start; index < end; index++ {
		if source[index] != '\n' {
			continue
		}
		next := index + 1
		for next < end && (source[next] == ' ' || source[next] == '\t' || source[next] == '\r') {
			next++
		}
		if next >= end || !isLineLeadingBackslash(source, next) {
			return true
		}
	}
	return false
}

func trimExpressionSpaceEnd(source string, start, end int) int {
	for end > start && isExpressionSpace(source[end-1]) {
		end--
	}
	return end
}

func (p *expressionParser) parseList() *Expression {
	open := p.current()
	p.advance()
	var children []*Expression
	end := open.span.End
	malformed := false
	for p.current().kind != expressionEOF && p.current().text != "]" {
		child := p.parse(0)
		children = append(children, child)
		if child.Span.End > end {
			end = child.Span.End
		}
		if p.current().text != "," && p.current().text != ";" {
			if p.dialect == Vim9 && p.current().kind != expressionEOF && p.current().text != "]" && p.current().text != ")" && p.current().text != "}" {
				p.diagnostics = append(p.diagnostics, Diagnostic{Code: "vim/E696", Message: "Missing comma in List", Span: p.current().span})
				malformed = true
				// Vim stops at the first structural List error.  Keep the
				// children parsed before it and make the malformed remainder
				// opaque so it cannot produce a trailing-expression cascade.
				for p.current().kind != expressionEOF && p.current().text != "]" && p.current().text != ")" && p.current().text != "}" {
					p.advance()
				}
			}
			break
		}
		p.advance()
	}
	if p.dialect != Vim9 {
		end = p.consumeClosing("]", end)
		return &Expression{Kind: ExpressionList, Span: Span{Start: open.span.Start, End: end}, Children: children}
	}
	if p.current().text == "]" {
		end = p.current().span.End
		p.advance()
	} else if !malformed {
		p.diagnostics = append(p.diagnostics, Diagnostic{Code: "vimls/missing-list-end", Message: "Missing end of List ']'", Span: p.current().span})
	}
	return &Expression{Kind: ExpressionList, Span: Span{Start: open.span.Start, End: end}, Children: children}
}

func (p *expressionParser) parseDictionaryOrLambda() *Expression {
	open := p.current()
	if arrowOffset := findLegacyLambdaArrow(p.source, open.span.Start-p.base); arrowOffset >= 0 {
		return p.parseLegacyLambda(open, arrowOffset)
	}
	p.advance()
	var children []*Expression
	last := open.span.End
	hadValue := false
	trailingComma := false
	// Keep the first dictionary error as the recovery point.  Vim reports one
	// structural error for a malformed dictionary and stops compiling that
	// expression; continuing to parse the rest of the physical line would
	// manufacture trailing/missing diagnostics and lose the useful children
	// already parsed before the error.
	malformed := false
	for p.current().kind != expressionEOF && p.current().text != "}" {
		key := p.parseDictionaryKey()
		children = append(children, key)
		if key.Span.End > last {
			last = key.Span.End
		}
		if p.current().text != ":" {
			if p.dialect == Vim9 {
				p.diagnostics = append(p.diagnostics, Diagnostic{
					Code: "vim/E720", Message: "Missing colon in Dictionary", Span: p.current().span,
				})
				malformed = true
				p.consumeDictionaryRemainder()
				break
			}
			if p.current().text != "," {
				break
			}
			p.advance()
			trailingComma = true
			continue
		}
		p.advance()
		diagnosticsBeforeValue := len(p.diagnostics)
		value := p.parse(0)
		children = append(children, value)
		hadValue = true
		if value.Span.End > last {
			last = value.Span.End
		}
		if len(p.diagnostics) > diagnosticsBeforeValue && p.dialect == Vim9 {
			// A malformed nested value already owns the diagnostic.  Retain its
			// AST and avoid adding a second dictionary-level error.
			malformed = true
			p.consumeDictionaryRemainder()
			break
		}
		if p.current().text != "," && p.current().text != "}" {
			if p.dialect == Vim9 {
				p.diagnostics = append(p.diagnostics, Diagnostic{
					Code: "vim/E722", Message: "Missing comma in Dictionary", Span: p.current().span,
				})
				malformed = true
				p.consumeDictionaryRemainder()
			}
			break
		}
		if p.current().text == "," {
			trailingComma = true
			p.advance()
		} else {
			trailingComma = false
		}
	}
	if p.dialect != Vim9 {
		end := p.consumeClosing("}", open.span.End)
		return &Expression{Kind: ExpressionDictionary, Span: Span{Start: open.span.Start, End: end}, Children: children}
	}
	end := last
	if p.current().text == "}" {
		end = p.current().span.End
		p.advance()
	} else if !malformed && p.dialect == Vim9 {
		// A value followed by a missing separator is E722.  A trailing comma,
		// or an empty/incomplete dictionary, is instead the missing-end case.
		code := "vim/E723"
		message := "Missing end of Dictionary '}'"
		if hadValue && !trailingComma {
			code = "vim/E722"
			message = "Missing comma in Dictionary"
		}
		p.diagnostics = append(p.diagnostics, Diagnostic{Code: code, Message: message, Span: p.current().span})
	}
	return &Expression{Kind: ExpressionDictionary, Span: Span{Start: open.span.Start, End: end}, Children: children}
}

// consumeDictionaryRemainder makes a malformed dictionary opaque through the
// end of the current expression.  It deliberately does not consume a valid
// closing delimiter belonging to an enclosing expression when one is absent;
// callers still retain all children parsed before the error.
func (p *expressionParser) consumeDictionaryRemainder() {
	for p.current().kind != expressionEOF && p.current().text != "}" {
		p.advance()
	}
}

func (p *expressionParser) parseDictionaryKey() *Expression {
	first := p.current()
	if first.kind != expressionIdentifier && first.kind != expressionNumber {
		return p.parse(0)
	}
	last := first
	cursor := p.lexer
	cursor.advance()
	for cursor.current.text == "-" && cursor.current.span.Start == last.span.End {
		nextCursor := cursor
		nextCursor.advance()
		next := nextCursor.current
		if (next.kind != expressionIdentifier && next.kind != expressionNumber) || next.span.Start != cursor.current.span.End {
			break
		}
		last = next
		cursor = nextCursor
		cursor.advance()
	}
	if cursor.current.text == ":" {
		p.lexer = cursor
		start := first.span.Start - p.base
		end := last.span.End - p.base
		return &Expression{Kind: ExpressionIdentifier, Span: Span{Start: first.span.Start, End: last.span.End}, Value: p.source[start:end]}
	}
	return p.parse(0)
}

func (p *expressionParser) parseLegacyLambda(open expressionToken, arrowOffset int) *Expression {
	openOffset := open.span.Start - p.base
	lambda := &Expression{Kind: ExpressionLambda, Operator: Span{Start: p.base + arrowOffset, End: p.base + arrowOffset + 2}}
	for _, part := range splitTopLevel(p.source, openOffset+1, arrowOffset, ',') {
		start := skipExpressionSpace(p.source, part.Start)
		end := trimExpressionSpaceEnd(p.source, start, part.End)
		if start >= end {
			continue
		}
		nameEnd := scanWord(p.source, start, end)
		parameter := Parameter{Name: Span{Start: p.base + start, End: p.base + nameEnd}}
		lambda.Parameters = append(lambda.Parameters, parameter)
		lambda.Children = append(lambda.Children, &Expression{Kind: ExpressionIdentifier, Span: parameter.Name, Value: p.source[start:nameEnd]})
	}
	arrowEnd := p.base + arrowOffset + 2
	for p.current().kind != expressionEOF && p.current().span.End <= arrowEnd {
		p.advance()
	}
	body := p.parse(0)
	end := p.consumeClosing("}", body.Span.End)
	lambda.Children = append(lambda.Children, body)
	lambda.Span = Span{Start: open.span.Start, End: end}
	return lambda
}

func findLegacyLambdaArrow(source string, open int) int {
	depth := 0
	quote := byte(0)
	for index := open; index < len(source); index++ {
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
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return -1
			}
		case '-':
			if depth == 1 && index+1 < len(source) && source[index+1] == '>' {
				return index
			}
		case ':':
			// A dictionary key can be followed by an arrow expression.  Once a
			// top-level key separator is seen this cannot be a legacy lambda.
			if depth == 1 {
				return -1
			}
		}
	}
	return -1
}

func (p *expressionParser) consumeClosing(expected string, fallback int) int {
	if p.current().text == expected {
		end := p.current().span.End
		p.advance()
		return end
	}
	// If an unterminated List is nested in another unterminated construct,
	// retain the established delimiter diagnostics for both nesting levels.
	// The List-specific Vim diagnostic applies when the List itself is the
	// expression recovery boundary.
	for index := len(p.diagnostics) - 1; index >= 0; index-- {
		if p.diagnostics[index].Code == "vimls/missing-list-end" {
			p.diagnostics[index].Code = "vimls/missing-delimiter"
			p.diagnostics[index].Message = "expected ]"
			break
		}
	}
	p.diagnostics = append(p.diagnostics, Diagnostic{Code: "vimls/missing-delimiter", Message: "expected " + expected, Span: p.current().span})
	return fallback
}

func (p *expressionParser) current() expressionToken {
	return p.lexer.current
}

func (p *expressionParser) advance() {
	p.lexer.advance()
}

func (p *expressionParser) peek(distance int) expressionToken {
	cursor := p.lexer
	for range distance {
		cursor.advance()
	}
	return cursor.current
}

func (p *expressionParser) isMember(left *Expression) bool {
	if !p.adjacentOrContinuationLine(left.Span.End, p.current().span.Start) {
		return false
	}
	next := p.peek(1)
	if p.dialect == Legacy && next.kind == expressionIdentifier && len(next.text) == 1 &&
		strings.ContainsRune("abglstvw", rune(next.text[0])) {
		colon := p.peek(2)
		if colon.text == ":" && colon.span.Start == next.span.End {
			// In legacy Vim, .a:{name} is concatenation followed by a scoped
			// curly name, not member access named "a".
			return false
		}
	}
	return (next.kind == expressionIdentifier || next.kind == expressionNumber) && next.span.Start == p.current().span.End
}

func (p *expressionParser) adjacentOrContinuationLine(left, right int) bool {
	if left == right {
		return true
	}
	start := left - p.base
	end := right - p.base
	return start >= 0 && end <= len(p.source) && start < end && strings.ContainsRune(p.source[start:end], '\n') && continuationGap(p.source[start:end])
}

func continuationGap(source string) bool {
	for index := 0; index < len(source); index++ {
		if isExpressionSpace(source[index]) {
			continue
		}
		if source[index] == '\\' && isLineLeadingBackslash(source, index) {
			continue
		}
		if source[index] != '#' {
			return false
		}
		for index < len(source) && source[index] != '\n' {
			index++
		}
	}
	return true
}

func (p *expressionParser) onlyWhitespace(left, right int) bool {
	start := left - p.base
	end := right - p.base
	if start < 0 || end > len(p.source) || start > end {
		return false
	}
	for _, character := range p.source[start:end] {
		if character != ' ' && character != '\t' && character != '\r' && character != '\n' {
			return false
		}
	}
	return true
}

func (p *expressionParser) isMethod(left *Expression) bool {
	name := p.peek(1)
	return p.arrowGap(left.Span.End, p.current().span.Start) && name.kind == expressionIdentifier && p.onlyWhitespace(p.current().span.End, name.span.Start)
}

func (p *expressionParser) isArrowCallable(left *Expression) bool {
	next := p.peek(1)
	return p.arrowGap(left.Span.End, p.current().span.Start) && next.text == "(" && p.onlyWhitespace(p.current().span.End, next.span.Start)
}

func (p *expressionParser) arrowGap(left, right int) bool {
	return p.onlyWhitespace(left, right) || p.adjacentOrContinuationLine(left, right)
}

func (p *expressionParser) validateBinarySpacing(token expressionToken) {
	if p.dialect != Vim9 || token.text == "" {
		return
	}
	start := token.span.Start - p.base
	end := token.span.End - p.base
	if start <= 0 || end >= len(p.source) || !isExpressionSpace(p.source[start-1]) || !isExpressionSpace(p.source[end]) {
		p.diagnostics = append(p.diagnostics, Diagnostic{Code: "vim/E1004", Message: "white space required before and after " + token.text, Span: token.span})
	}
}

func infixBinding(operator string, dialect Dialect) (int, int, bool) {
	switch operator {
	case "??":
		return 10, 10, true
	case "||":
		return 20, 21, true
	case "&&":
		return 30, 31, true
	case "==", "!=", "==#", "==?", "!=#", "!=?", "=~", "=~#", "=~?", "!~", "!~#", "!~?", "is", "isnot", "is#", "is?", "isnot#", "isnot?", ">", ">#", ">?", ">=", ">=#", ">=?", "<", "<#", "<?", "<=", "<=#", "<=?":
		return 40, 41, true
	case "<<", ">>":
		return 50, 51, true
	case "..":
		return 60, 61, true
	case ".":
		if dialect == Legacy {
			return 60, 61, true
		}
	case "+", "-":
		return 60, 61, true
	case "*", "/", "%":
		return 70, 71, true
	}
	return 0, 0, false
}

func newExpressionLexer(source string, base int, dialect Dialect) expressionLexer {
	lexer := expressionLexer{source: source, base: base, dialect: dialect}
	lexer.current = lexer.scan()
	return lexer
}

func (lexer *expressionLexer) advance() {
	if lexer.current.kind != expressionEOF {
		lexer.current = lexer.scan()
	}
}

func (lexer *expressionLexer) scan() expressionToken {
	source := lexer.source
	for index := lexer.offset; index < len(source); {
		if source[index] == ' ' || source[index] == '\t' || source[index] == '\r' || source[index] == '\n' || isLineLeadingBackslash(source, index) {
			index++
			continue
		}
		if lexer.dialect == Vim9 && source[index] == '#' && (!strings.HasPrefix(source[index:], "#{") || strings.HasPrefix(source[index:], "#{{")) && (index == 0 || isExpressionSpace(source[index-1])) {
			for index < len(source) && source[index] != '\n' {
				index++
			}
			continue
		}
		if lexer.dialect == Legacy && source[index] == '"' && isLineLeadingLegacyComment(source, index) {
			for index < len(source) && source[index] != '\n' {
				index++
			}
			continue
		}
		start := index
		if source[index] == '@' {
			index++
			if index < len(source) && !isExpressionSpace(source[index]) {
				_, size := utf8.DecodeRuneInString(source[index:])
				index += size
			}
			return lexer.finish(expressionIdentifier, start, index)
		}
		if source[index] == '\'' || source[index] == '"' || index+1 < len(source) && source[index] == '$' && (source[index+1] == '\'' || source[index+1] == '"') {
			interpolated := source[index] == '$'
			if interpolated {
				index++
			}
			quote := source[index]
			index++
			if interpolated {
				index = scanInterpolatedStringEnd(source, index, quote)
				return lexer.finish(expressionInterpolatedString, start, index)
			}
			for index < len(source) {
				if quote == '"' && source[index] == '\\' {
					index += minInt(2, len(source)-index)
					continue
				}
				if source[index] == quote {
					index++
					if quote == '\'' && index < len(source) && source[index] == '\'' {
						index++
						continue
					}
					break
				}
				_, size := utf8.DecodeRuneInString(source[index:])
				index += size
			}
			return lexer.finish(expressionString, start, index)
		}
		if isExpressionDigit(source[index]) || source[index] == '.' && index+1 < len(source) && isExpressionDigit(source[index+1]) {
			var malformed bool
			index, malformed = scanExpressionNumber(source, index)
			kind := expressionNumber
			if len(source[start:index]) >= 2 && source[start] == '0' && (source[start+1] == 'z' || source[start+1] == 'Z') {
				kind = expressionBlob
			}
			token := lexer.finish(kind, start, index)
			token.malformed = malformed
			return token
		}
		if strings.HasPrefix(source[index:], "#{") {
			return lexer.finish(expressionPunctuation, start, index+2)
		}
		if end, ok := scanScriptLocalFunctionName(source, index); ok {
			return lexer.finish(expressionIdentifier, start, end)
		}
		r, size := utf8.DecodeRuneInString(source[index:])
		sigilIdentifier := strings.ContainsRune("&$@", r) && !(r == '&' && index+1 < len(source) && source[index+1] == '&')
		if r == '_' || unicode.IsLetter(r) || sigilIdentifier {
			index += size
			if r == '&' && index+1 < len(source) && (source[index] == 'g' || source[index] == 'l') && source[index+1] == ':' {
				index += 2
			}
			for index < len(source) {
				next, nextSize := utf8.DecodeRuneInString(source[index:])
				if next != '_' && next != '#' && !unicode.IsLetter(next) && !unicode.IsDigit(next) {
					break
				}
				index += nextSize
			}
			prefix := source[start:index]
			if index < len(source) && source[index] == ':' && isScopePrefix(prefix) && (scopeNameStartsAt(source, index+1, prefix, lexer.dialect) || scopePrefixIsStandalone(source, index+1) || lexer.dialect == Legacy && index+1 < len(source) && isExpressionSpace(source[index+1])) {
				index++
				for index < len(source) {
					next, nextSize := utf8.DecodeRuneInString(source[index:])
					if next != '_' && next != '#' && !unicode.IsLetter(next) && !unicode.IsDigit(next) {
						break
					}
					index += nextSize
				}
			}
			if index < len(source) && source[index] == '?' && (source[start:index] == "is" || source[start:index] == "isnot") {
				index++
			}
			kind := expressionIdentifier
			if isComparisonWord(source[start:index]) {
				kind = expressionOperator
			}
			return lexer.finish(kind, start, index)
		}
		operator := longestOperator(source[index:])
		if operator != "" {
			return lexer.finish(expressionOperator, start, index+len(operator))
		}
		return lexer.finish(expressionPunctuation, start, index+size)
	}
	lexer.offset = len(source)
	return expressionToken{kind: expressionEOF, span: Span{Start: lexer.base + len(source), End: lexer.base + len(source)}}
}

func (lexer *expressionLexer) finish(kind expressionTokenKind, start, end int) expressionToken {
	lexer.offset = end
	return expressionToken{kind: kind, span: Span{Start: lexer.base + start, End: lexer.base + end}, text: lexer.source[start:end]}
}

// lexExpression is retained for diagnostics and narrow callers that need the
// complete token list.  The expression parser itself advances the lexer on
// demand and does not allocate this slice.
func lexExpression(source string, base int, dialect Dialect) []expressionToken {
	lexer := newExpressionLexer(source, base, dialect)
	var tokens []expressionToken
	for {
		tokens = append(tokens, lexer.current)
		if lexer.current.kind == expressionEOF {
			return tokens
		}
		lexer.advance()
	}
}

// scanInterpolatedStringEnd follows Vim's eval_interp_string() segmentation:
// a single { starts an expression, which consumes balanced braces and its own
// quoted strings before the outer string quote is considered again.  This is
// especially important for $'...' where single quotes are literal inside the
// interpolation expression, not terminators of the outer string.
func scanInterpolatedStringEnd(source string, start int, quote byte) int {
	for index := start; index < len(source); {
		character := source[index]
		if quote == '"' && character == '\\' {
			index += minInt(2, len(source)-index)
			continue
		}
		if character == quote {
			if quote == '\'' && index+1 < len(source) && source[index+1] == '\'' {
				index += 2
				continue
			}
			return index + 1
		}
		if character == '{' {
			if index+1 < len(source) && source[index+1] == '{' {
				index += 2
				continue
			}
			close := findInterpolationEnd(source, index, len(source))
			if close < 0 {
				return len(source)
			}
			index = close + 1
			continue
		}
		if character == '}' && index+1 < len(source) && source[index+1] == '}' {
			index += 2
			continue
		}
		index++
	}
	return len(source)
}

// scanScriptLocalFunctionName recognizes the textual script-local function
// prefixes that Vim accepts in an expression.  Vim translates both <SID> and
// <SNR> (case-insensitively) before looking up the function; the source span
// and value here intentionally retain the spelling supplied by the user.
// The suffix uses the same name characters as a Vim function name, including
// '#' for autoload-style components.  <SNR> is not restricted to a numeric
// suffix here: Vim's expression parser accepts the whole name syntactically,
// while resolving whether that script ID exists is a runtime concern.
func scanScriptLocalFunctionName(source string, start int) (int, bool) {
	if start+5 > len(source) || source[start] != '<' {
		return start, false
	}
	prefix := source[start : start+5]
	if !strings.EqualFold(prefix, "<SID>") && !strings.EqualFold(prefix, "<SNR>") {
		return start, false
	}
	index := start + 5
	if index >= len(source) {
		return start, false
	}
	r, size := utf8.DecodeRuneInString(source[index:])
	if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
		return start, false
	}
	index += size
	for index < len(source) {
		next, nextSize := utf8.DecodeRuneInString(source[index:])
		if next != '_' && next != '#' && next != ':' && !unicode.IsLetter(next) && !unicode.IsDigit(next) {
			break
		}
		index += nextSize
	}
	return index, true
}

func scopeNameStartsAt(source string, index int, prefix string, dialect Dialect) bool {
	if index >= len(source) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(source[index:])
	if r == '_' || unicode.IsLetter(r) {
		return true
	}
	return dialect == Legacy && prefix == "a" && unicode.IsDigit(r)
}

func scopePrefixIsStandalone(source string, index int) bool {
	index = skipExpressionSpace(source, index)
	return index >= len(source) || strings.ContainsRune(",)]}", rune(source[index])) || strings.HasPrefix(source[index:], "->")
}

func isLineLeadingBackslash(source string, index int) bool {
	if source[index] != '\\' {
		return false
	}
	for previous := index - 1; previous >= 0 && source[previous] != '\n'; previous-- {
		if source[previous] != ' ' && source[previous] != '\t' {
			return false
		}
	}
	return true
}

func isLineLeadingLegacyComment(source string, index int) bool {
	if index == 0 || source[index] != '"' {
		return false
	}
	for previous := index - 1; previous >= 0; previous-- {
		if source[previous] == '\n' {
			return true
		}
		if source[previous] != ' ' && source[previous] != '\t' && source[previous] != '\r' {
			return false
		}
	}
	return false
}

func longestOperator(source string) string {
	for _, operator := range []string{"isnot#", "isnot?", "isnot", ">=#", ">=?", "<=#", "<=?", "==#", "==?", "!=#", "!=?", "=~#", "=~?", "!~#", "!~?", "->", "=>", "..", "&&", "||", "??", ">#", ">?", "<#", "<?", "<=", ">=", "==", "!=", "=~", "!~", "+=", "-=", "*=", "/=", "%=", "<<", ">>", "**", "is#", "is?", "is", "+", "-", "*", "/", "%", ".", "!", "<", ">", "?", ":", "="} {
		if strings.HasPrefix(source, operator) {
			return operator
		}
	}
	return ""
}

func isExpressionDigit(character byte) bool { return character >= '0' && character <= '9' }
func isExpressionSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n'
}
func isExpressionLetter(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

func scanExpressionNumber(source string, start int) (int, bool) {
	index := start
	if source[index] == '.' {
		index++
		for index < len(source) && (isExpressionDigit(source[index]) || isDigitSeparator(source, index)) {
			index++
		}
		end := scanExpressionExponent(source, index)
		candidateEnd := scanExpressionNumberSuffix(source, end)
		return candidateEnd, candidateEnd > end
	}
	if index+1 < len(source) && source[index] == '0' && strings.ContainsRune("xXbBoOzZ", rune(source[index+1])) {
		blob := source[index+1] == 'z' || source[index+1] == 'Z'
		base := source[index+1]
		malformed := false
		index += 2
		for index < len(source) && (isExpressionDigit(source[index]) || isExpressionLetter(source[index]) || isDigitSeparator(source, index) || blob && source[index] == '.') {
			if !blob && !validPrefixedNumberByte(source, index, base) {
				malformed = true
			}
			index++
		}
		return index, malformed || !blob && index == start+2
	}
	for index < len(source) && (isExpressionDigit(source[index]) || isDigitSeparator(source, index)) {
		index++
	}
	if index+1 < len(source) && source[index] == '.' && isExpressionDigit(source[index+1]) {
		index++
		for index < len(source) && (isExpressionDigit(source[index]) || isDigitSeparator(source, index)) {
			index++
		}
	}
	end := scanExpressionExponent(source, index)
	candidateEnd := scanExpressionNumberSuffix(source, end)
	return candidateEnd, candidateEnd > end
}

func scanExpressionNumberSuffix(source string, index int) int {
	for index < len(source) && (isExpressionDigit(source[index]) || isExpressionLetter(source[index])) {
		index++
	}
	return index
}

func validPrefixedNumberByte(source string, index int, base byte) bool {
	character := source[index]
	if character == '\'' {
		return index > 1 && index+1 < len(source) && validPrefixedDigit(source[index-1], base) && validPrefixedDigit(source[index+1], base)
	}
	return validPrefixedDigit(character, base)
}

func validPrefixedDigit(character, base byte) bool {
	switch base {
	case 'x', 'X':
		return isLiteralDigit(character)
	case 'o', 'O':
		return character >= '0' && character <= '7'
	default:
		return character == '0' || character == '1'
	}
}

func scanExpressionExponent(source string, index int) int {
	if index < len(source) && (source[index] == 'e' || source[index] == 'E') {
		index++
		if index < len(source) && (source[index] == '+' || source[index] == '-') {
			index++
		}
		for index < len(source) && (isExpressionDigit(source[index]) || isDigitSeparator(source, index)) {
			index++
		}
	}
	return index
}

func isDigitSeparator(source string, index int) bool {
	return source[index] == '\'' && index > 0 && index+1 < len(source) && isLiteralDigit(source[index-1]) && isLiteralDigit(source[index+1])
}

func isLiteralDigit(character byte) bool {
	return isExpressionDigit(character) || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F'
}

func isComparisonWord(value string) bool {
	switch value {
	case "is", "is#", "is?", "isnot", "isnot#", "isnot?":
		return true
	default:
		return false
	}
}

func isScopePrefix(value string) bool {
	switch value {
	case "a", "b", "g", "l", "s", "t", "v", "w", "&g", "&l":
		return true
	default:
		return false
	}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
