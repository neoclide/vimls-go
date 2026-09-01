package syntax

import (
	"sort"
	"strings"
)

// IndentOptions are the standard LSP indentation options used by IndentEdits.
type IndentOptions struct {
	TabSize      int
	InsertSpaces bool
}

// IndentEdit replaces one leading-whitespace span in the original source.
type IndentEdit struct {
	Span    Span
	NewText string
}

// IndentEdits returns source-ordered edits for leading whitespace whose
// ownership is proven by the parsed file. All other source bytes are retained.
func IndentEdits(file *File, options IndentOptions) []IndentEdit {
	if file == nil || options.TabSize <= 0 {
		return nil
	}
	planner := newIndentPlanner(file.Source)
	planner.collectFile(file, 0)
	planner.normalizeBrackets()
	edits := make([]IndentEdit, 0)
	for index, line := range planner.lines {
		if planner.protected[index] || line.indentEnd == line.contentEnd {
			continue
		}
		level := planner.lineLevels[index]
		if level < 0 && planner.continuation[index] {
			level = planner.continuationLevel(index)
		}
		if level < 0 && planner.comments[index] {
			level = max(0, planner.structuralLevels[index], planner.bracketLevel(index))
		}
		if level < 0 {
			continue
		}
		wanted := indentText(level, options)
		if file.Source[line.indentStart:line.indentEnd] != wanted {
			edits = append(edits, IndentEdit{Span: Span{Start: line.indentStart, End: line.indentEnd}, NewText: wanted})
		}
	}
	return edits
}

type indentLine struct {
	start       int
	contentEnd  int
	next        int
	indentStart int
	indentEnd   int
}

type indentCommand struct {
	start   int
	end     int
	level   int
	dialect Dialect
}

type indentBracket struct {
	open         int
	close        int
	contentLevel int
	closeLevel   int
}

type indentPlanner struct {
	source           string
	lines            []indentLine
	lineLevels       []int
	structuralLevels []int
	protected        []bool
	continuation     []bool
	comments         []bool
	commands         []indentCommand
	brackets         []indentBracket
	bracketSeen      map[[4]int]bool
	seenFiles        map[*File]bool
	seenLists        map[*CommandList]bool
}

func newIndentPlanner(source string) *indentPlanner {
	planner := &indentPlanner{
		source:      source,
		bracketSeen: make(map[[4]int]bool),
		seenFiles:   make(map[*File]bool),
		seenLists:   make(map[*CommandList]bool),
	}
	for start := 0; ; {
		contentEnd, next := physicalLineEnd(source, start)
		indentStart := start
		if start == 0 && strings.HasPrefix(source, "\xef\xbb\xbf") {
			indentStart = 3
		}
		indentEnd := skipSpace(source, indentStart, contentEnd)
		planner.lines = append(planner.lines, indentLine{start: start, contentEnd: contentEnd, next: next, indentStart: indentStart, indentEnd: indentEnd})
		if next >= len(source) {
			break
		}
		start = next
	}
	planner.lineLevels = make([]int, len(planner.lines))
	planner.structuralLevels = make([]int, len(planner.lines))
	for index := range planner.lines {
		planner.lineLevels[index] = -1
		planner.structuralLevels[index] = -1
	}
	planner.protected = make([]bool, len(planner.lines))
	planner.continuation = make([]bool, len(planner.lines))
	planner.comments = make([]bool, len(planner.lines))
	return planner
}

func (planner *indentPlanner) collectFile(file *File, base int) {
	if file == nil || planner.seenFiles[file] {
		return
	}
	planner.seenFiles[file] = true
	planner.collectCommands(file.Commands, file.Blocks, base)
	for _, token := range file.Tokens {
		switch token.Kind {
		case TokenContinuation:
			planner.continuation[planner.lineAt(token.Span.Start)] = true
		case TokenComment:
			line := planner.lineAt(token.Span.Start)
			planner.comments[line] = true
			first := planner.lines[line].indentEnd
			end := planner.lines[line].contentEnd
			if legacyContinuationComment(planner.source, first, end) || vim9ContinuationComment(planner.source, first, end) {
				planner.continuation[line] = true
			}
		case TokenOpaque:
			planner.protectSpan(token.Span)
		}
	}
	planner.protectSpan(file.OpaqueTail)
	for _, diagnostic := range file.Diagnostics {
		planner.protectDiagnostic(diagnostic.Span)
	}
}

func (planner *indentPlanner) collectList(list *CommandList, base int) {
	if list == nil || planner.seenLists[list] {
		return
	}
	planner.seenLists[list] = true
	planner.collectCommands(list.Commands, list.Blocks, base)
}

func (planner *indentPlanner) collectCommands(commands []Command, blocks []Block, base int) {
	for blockIndex, block := range blocks {
		if block.Header < 0 || block.Header >= len(commands) {
			continue
		}
		startLine := planner.lineAt(commands[block.Header].Span.Start) + 1
		endLine := len(planner.lines)
		if block.End >= 0 && block.End < len(commands) {
			endLine = planner.lineAt(commands[block.End].Span.Start)
		} else if block.Span.End < len(planner.source) {
			endLine = planner.lineAt(block.Span.End)
		}
		level := base + blockDepth(blocks, blockIndex) + 1
		for line := startLine; line < endLine; line++ {
			planner.structuralLevels[line] = max(planner.structuralLevels[line], level)
		}
	}
	augroupExtra := planner.augroupIndent(commands, blocks, base)

	for index := range commands {
		command := &commands[index]
		level := base + commandIndentLevel(commands, blocks, index)
		if augroupExtra[index] {
			level++
		}
		line := planner.lineAt(command.Span.Start)
		known := command.Kind != CommandUnknown && command.Kind != CommandEmpty &&
			(command.Kind != CommandUser || len(command.EnumValues) > 0) && !command.detailsOpaque
		if known {
			planner.commands = append(planner.commands, indentCommand{start: command.Span.Start, end: command.Span.End, level: level, dialect: command.Dialect})
			if planner.lines[line].indentEnd == command.Span.Start {
				planner.lineLevels[line] = max(planner.lineLevels[line], level)
			}
			for _, value := range command.EnumValues {
				valueLine := planner.lineAt(value.Name.Start)
				if planner.lines[valueLine].indentEnd == value.Name.Start {
					planner.lineLevels[valueLine] = max(planner.lineLevels[valueLine], level)
				}
			}
		}
		planner.protectCommand(command)
		planner.collectCommandExpressions(command, level)
		planner.collectList(command.Embedded, level)
	}
	sort.SliceStable(planner.commands, func(left, right int) bool {
		return planner.commands[left].start < planner.commands[right].start
	})
}

func (planner *indentPlanner) augroupIndent(commands []Command, blocks []Block, base int) []bool {
	extra := make([]bool, len(commands))
	openLine, openLevel := -1, -1
	finish := func(endLine int) {
		if openLine < 0 {
			return
		}
		for line := openLine + 1; line < endLine; line++ {
			if planner.structuralLevels[line] >= 0 {
				planner.structuralLevels[line]++
			} else {
				planner.structuralLevels[line] = openLevel + 1
			}
		}
	}
	for index := range commands {
		command := &commands[index]
		if command.Canonical == "augroup" {
			name := planner.source[command.Augroup.Start:command.Augroup.End]
			if strings.EqualFold(name, "END") {
				finish(planner.lineAt(command.Span.Start))
				openLine, openLevel = -1, -1
			} else if name != "" {
				finish(planner.lineAt(command.Span.Start))
				openLine = planner.lineAt(command.Span.Start)
				openLevel = base + commandIndentLevel(commands, blocks, index)
			}
			continue
		}
		if openLine >= 0 {
			extra[index] = true
		}
	}
	finish(len(planner.lines))
	return extra
}

func commandIndentLevel(commands []Command, blocks []Block, commandIndex int) int {
	command := &commands[commandIndex]
	if command.Block < 0 || command.Block >= len(blocks) {
		return 0
	}
	block := blocks[command.Block]
	level := blockDepth(blocks, command.Block)
	if block.Header == commandIndex || block.End == commandIndex {
		return level
	}
	for _, branch := range block.Branches {
		if branch == commandIndex {
			return level
		}
	}
	return level + 1
}

func blockDepth(blocks []Block, blockIndex int) int {
	depth := 0
	for seen := 0; blockIndex >= 0 && blockIndex < len(blocks) && seen <= len(blocks); seen++ {
		blockIndex = blocks[blockIndex].Parent
		if blockIndex >= 0 {
			depth++
		}
	}
	return depth
}

func (planner *indentPlanner) protectCommand(command *Command) {
	if command == nil {
		return
	}
	if command.Kind == CommandUnknown || (command.Kind == CommandUser && len(command.EnumValues) == 0) || command.detailsOpaque {
		planner.protectSpan(command.Span)
	}
	if command.Heredoc != nil {
		planner.protectUnit(command.Span.Start, command.Span, command.Heredoc.Body, command.Heredoc.EndMarker)
	}
	if command.TextBody != nil {
		planner.protectUnit(command.Span.Start, command.Span, command.TextBody.Body, command.TextBody.EndMarker)
	}
	if command.Keymap != nil {
		planner.protectUnit(command.Span.Start, command.Span, command.Keymap.Body)
	}
	headerLine := planner.lineAt(command.Span.Start)
	if command.Mapping != nil {
		planner.protectFollowingLines(command.Mapping.RHS, headerLine)
	}
	if command.Substitute != nil {
		planner.protectFollowingLines(command.Argument, headerLine)
	}
}

func (planner *indentPlanner) protectUnit(start int, spans ...Span) {
	end := start
	for _, span := range spans {
		end = max(end, span.End)
	}
	startLine := planner.lineAt(start)
	endLine := startLine
	if end > start {
		endLine = planner.lineAt(end - 1)
	}
	for line := startLine; line <= endLine; line++ {
		planner.protected[line] = true
	}
}

func (planner *indentPlanner) protectFollowingLines(span Span, headerLine int) {
	if span.End <= span.Start {
		return
	}
	for line := max(headerLine+1, planner.lineAt(span.Start)); line <= planner.lineAt(span.End-1); line++ {
		planner.protected[line] = true
	}
}

func (planner *indentPlanner) protectSpan(span Span) {
	if span.End <= span.Start {
		return
	}
	for line := planner.lineAt(span.Start); line <= planner.lineAt(span.End-1); line++ {
		planner.protected[line] = true
	}
}

func (planner *indentPlanner) protectDiagnostic(span Span) {
	if span.Start < 0 || span.Start > len(planner.source) {
		return
	}
	end := min(max(span.End, span.Start+1), len(planner.source))
	endLine := planner.lineAt(span.Start)
	if end > span.Start {
		endLine = planner.lineAt(end - 1)
	}
	for line := planner.lineAt(span.Start); line <= endLine; line++ {
		planner.protected[line] = true
	}
}

func (planner *indentPlanner) collectCommandExpressions(command *Command, level int) {
	seen := make(map[*Expression]bool)
	var collect func(*Expression)
	collect = func(expression *Expression) {
		if expression == nil || seen[expression] {
			return
		}
		seen[expression] = true
		planner.addExpressionBracket(expression, level)
		for _, child := range expression.Children {
			collect(child)
		}
		for _, parameter := range expression.Parameters {
			collect(parameter.Target)
			collect(parameter.Default)
		}
		if expression.LambdaBody != nil {
			planner.collectFile(expression.LambdaBody, level+1)
		}
	}
	for _, expression := range command.Expressions {
		collect(expression)
	}
	for _, expression := range command.Targets {
		collect(expression)
	}
	if command.Declaration != nil {
		collect(command.Declaration.Target)
		collect(command.Declaration.Initializer)
	}
	if command.For != nil {
		collect(command.For.Iterable)
	}
	if command.Import != nil {
		collect(command.Import.Path)
	}
	if command.Mapping != nil {
		collect(command.Mapping.RHSExpression)
	}
	if command.Substitute != nil {
		collect(command.Substitute.Expression)
	}
	for _, value := range command.EnumValues {
		collect(value.Initializer)
		for _, argument := range value.Arguments {
			collect(argument)
		}
	}
	if command.Function != nil {
		for _, parameter := range command.Function.Parameters {
			collect(parameter.Target)
			collect(parameter.Default)
		}
		planner.addFunctionBracket(command, level)
	}
}

func (planner *indentPlanner) addExpressionBracket(expression *Expression, level int) {
	span := expression.Span
	if span.Start < 0 || span.End <= span.Start || span.End > len(planner.source) {
		return
	}
	open, close := -1, -1
	switch expression.Kind {
	case ExpressionList:
		open, close = planner.edgePair(span, '[', ']')
	case ExpressionDictionary:
		open, close = planner.edgePair(span, '{', '}')
	case ExpressionParenthesized, ExpressionTuple:
		open, close = planner.edgePair(span, '(', ')')
	case ExpressionLambdaBlock:
		open, close = planner.edgePair(span, '{', '}')
	case ExpressionCall:
		start := span.Start
		if len(expression.Children) > 0 {
			start = expression.Children[0].Span.End
		}
		open, close = planner.trailingPair(span, start, '(', ')')
	case ExpressionIndex, ExpressionSlice:
		start := span.Start
		if len(expression.Children) > 0 {
			start = expression.Children[0].Span.End
		}
		open, close = planner.trailingPair(span, start, '[', ']')
	case ExpressionCurlyName:
		open, close = planner.edgePair(span, '{', '}')
	case ExpressionLambda:
		if expression.Operator.Start > span.Start {
			candidate := strings.IndexByte(planner.source[span.Start:expression.Operator.Start], '(')
			if candidate >= 0 {
				open = span.Start + candidate
				close = trimSpaceEnd(planner.source, open+1, expression.Operator.Start)
				if close <= open || planner.source[close-1] != ')' {
					open, close = -1, -1
				} else {
					close--
				}
			}
		}
	}
	if open >= 0 && close > open {
		planner.addBracket(open, close, level+1, level)
	}
}

func (planner *indentPlanner) edgePair(span Span, opening, closing byte) (int, int) {
	open := strings.IndexByte(planner.source[span.Start:min(span.End, span.Start+3)], opening)
	if open < 0 {
		return -1, -1
	}
	open += span.Start
	close := trimSpaceEnd(planner.source, open+1, span.End) - 1
	if close <= open || planner.source[close] != closing {
		return -1, -1
	}
	return open, close
}

func (planner *indentPlanner) trailingPair(span Span, start int, opening, closing byte) (int, int) {
	start = max(start, span.Start)
	if start >= span.End {
		return -1, -1
	}
	open := strings.IndexByte(planner.source[start:span.End], opening)
	if open < 0 {
		return -1, -1
	}
	open += start
	close := trimSpaceEnd(planner.source, open+1, span.End) - 1
	if close <= open || planner.source[close] != closing {
		return -1, -1
	}
	return open, close
}

func (planner *indentPlanner) addFunctionBracket(command *Command, level int) {
	function := command.Function
	start := function.Name.End
	end := command.Span.End
	if function.ReturnTypeSpan.Start > start {
		end = min(end, function.ReturnTypeSpan.Start)
	}
	if function.Attributes.Start > start {
		end = min(end, function.Attributes.Start)
	}
	if start >= end {
		return
	}
	open := strings.IndexByte(planner.source[start:end], '(')
	close := strings.LastIndexByte(planner.source[start:end], ')')
	if open < 0 || close < 0 {
		return
	}
	planner.addBracket(start+open, start+close, level+2, level+2)
}

func (planner *indentPlanner) addBracket(open, close, contentLevel, closeLevel int) {
	key := [4]int{open, close, contentLevel, closeLevel}
	if planner.bracketSeen[key] {
		return
	}
	planner.bracketSeen[key] = true
	planner.brackets = append(planner.brackets, indentBracket{open: open, close: close, contentLevel: contentLevel, closeLevel: closeLevel})
}

func (planner *indentPlanner) normalizeBrackets() {
	sort.SliceStable(planner.brackets, func(left, right int) bool {
		if planner.brackets[left].open == planner.brackets[right].open {
			return planner.brackets[left].close > planner.brackets[right].close
		}
		return planner.brackets[left].open < planner.brackets[right].open
	})
	for index := range planner.brackets {
		bracket := &planner.brackets[index]
		parent := -1
		for candidate := index - 1; candidate >= 0; candidate-- {
			outer := &planner.brackets[candidate]
			if outer.open < bracket.open && outer.close > bracket.close && planner.lineAt(outer.open) < planner.lineAt(bracket.open) {
				parent = candidate
				break
			}
		}
		if parent < 0 {
			continue
		}
		delta := bracket.contentLevel - bracket.closeLevel
		bracket.closeLevel = max(bracket.closeLevel, planner.brackets[parent].contentLevel)
		bracket.contentLevel = bracket.closeLevel + delta
	}
}

func (planner *indentPlanner) continuationLevel(lineIndex int) int {
	line := planner.lines[lineIndex]
	owner, ok := planner.continuationOwner(line)
	if !ok {
		return -1
	}
	first := line.indentEnd
	if planner.source[first] == '\\' || legacyContinuationComment(planner.source, first, line.contentEnd) {
		return owner.level + 3
	}
	if first+1 < line.contentEnd && planner.source[first] == '#' && planner.source[first+1] == '\\' && lineIndex > 0 {
		previous := planner.lines[lineIndex-1]
		if previous.indentEnd < previous.contentEnd && planner.source[previous.indentEnd] == '\\' {
			return owner.level + 3
		}
	}
	if level := planner.bracketLevel(lineIndex); level >= 0 {
		return level
	}
	if planner.source[first] == '|' {
		return owner.level + 1
	}
	if owner.dialect == Legacy {
		return owner.level + 3
	}
	return owner.level + 1
}

func (planner *indentPlanner) continuationOwner(line indentLine) (indentCommand, bool) {
	position := line.indentEnd
	best := -1
	for index, command := range planner.commands {
		if command.start >= line.start {
			break
		}
		if command.end >= position {
			best = index
		}
	}
	if best < 0 {
		for index, command := range planner.commands {
			if command.start >= line.start {
				break
			}
			best = index
		}
	}
	if best < 0 {
		return indentCommand{}, false
	}
	return planner.commands[best], true
}

func (planner *indentPlanner) bracketLevel(lineIndex int) int {
	line := planner.lines[lineIndex]
	first := line.indentEnd
	level := -1
	for _, bracket := range planner.brackets {
		if bracket.open >= first || bracket.close < first {
			continue
		}
		if bracket.close <= line.contentEnd && first < len(planner.source) && isClosingDelimiter(planner.source[first]) {
			level = max(level, bracket.closeLevel)
			continue
		}
		level = max(level, bracket.contentLevel)
	}
	return level
}

func isClosingDelimiter(character byte) bool {
	return character == ')' || character == ']' || character == '}'
}

func (planner *indentPlanner) lineAt(offset int) int {
	offset = max(0, min(offset, len(planner.source)))
	line := sort.Search(len(planner.lines), func(index int) bool {
		return planner.lines[index].next > offset || planner.lines[index].next == len(planner.source)
	})
	if line >= len(planner.lines) {
		return len(planner.lines) - 1
	}
	return line
}

func indentText(level int, options IndentOptions) string {
	if options.InsertSpaces {
		return strings.Repeat(" ", level*options.TabSize)
	}
	return strings.Repeat("\t", level)
}
