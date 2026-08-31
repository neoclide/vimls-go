package syntax

type sourceChange struct {
	Start  int
	OldEnd int
	NewEnd int
}

type incrementalMetadata struct {
	units    []parseUnit
	eligible bool
	reused   int
	parsed   int
}

type parseUnit struct {
	span            Span
	entry           parserState
	exit            parserState
	structureEntry  []BlockKind
	structureExit   []BlockKind
	firstCommand    int
	commandCount    int
	firstToken      int
	tokenCount      int
	firstDiagnostic int
	diagnosticCount int
	fragile         bool
	independent     bool
}

type parserState struct {
	initialDialect Dialect
	activeDialect  Dialect
	scriptVersion  uint8
	vim9Prologue   bool
	lambdaBody     bool
	dialectStack   []Dialect
	aggregateStack []BlockKind
}

// Reparse incrementally rebuilds flat, independent syntax units. Stateful or
// incomplete input deliberately falls back to Parse; full parsing remains the
// correctness oracle and is never called after an incremental result is built.
func Reparse(previous *File, source string) *File {
	if previous == nil {
		return Parse(source)
	}
	if previous.Source == source {
		return previous
	}
	change := changedSource(previous.Source, source)
	metadata := previous.incremental
	if metadata == nil || !metadata.eligible || startsWithVim9Script(previous.Source) != startsWithVim9Script(source) {
		return Parse(source)
	}

	restart := restartUnit(metadata.units, change.Start)
	if restart < 0 {
		return Parse(source)
	}
	delta := change.NewEnd - change.OldEnd
	suffix := convergedSuffix(previous, source, change, restart, delta)
	if suffix < 0 {
		return Parse(source)
	}

	result := &File{Dialect: previous.Dialect, Source: source, lambdaBody: previous.lambdaBody}
	// Keep empty slices nil while giving the common unchanged-document case
	// enough room for the cloned command/token arrays.
	if len(previous.Commands) > 0 {
		result.Commands = make([]Command, 0, len(previous.Commands))
	}
	if len(previous.Tokens) > 0 {
		result.Tokens = make([]Token, 0, len(previous.Tokens))
	}
	if len(previous.Diagnostics) > 0 {
		result.Diagnostics = make([]Diagnostic, 0, len(previous.Diagnostics))
	}
	cloner := newASTCloner()
	for index := 0; index < restart; index++ {
		appendClonedUnit(result, previous, metadata.units[index], 0, cloner)
	}
	start := metadata.units[restart].span.Start
	end := len(source)
	if suffix < len(metadata.units) {
		end = metadata.units[suffix].span.Start + delta
	}
	if !appendParsedUnits(result, source, previous.Dialect, start, end) {
		return Parse(source)
	}
	for index := suffix; index < len(metadata.units); index++ {
		appendClonedUnit(result, previous, metadata.units[index], delta, cloner)
	}
	if len(result.Commands) == 0 {
		result.Commands = nil
	}
	if len(result.Tokens) == 0 {
		result.Tokens = nil
	}
	if len(result.Diagnostics) == 0 {
		result.Diagnostics = nil
	}
	result.incremental = buildIncrementalMetadata(result)
	if result.incremental == nil || !result.incremental.eligible {
		return Parse(source)
	}
	result.incremental.reused = restart + len(metadata.units) - suffix
	result.incremental.parsed = len(result.incremental.units) - result.incremental.reused
	return result
}

func changedSource(oldSource, source string) sourceChange {
	start := 0
	for start < len(oldSource) && start < len(source) && oldSource[start] == source[start] {
		start++
	}
	oldEnd, newEnd := len(oldSource), len(source)
	for oldEnd > start && newEnd > start && oldSource[oldEnd-1] == source[newEnd-1] {
		oldEnd--
		newEnd--
	}
	return sourceChange{Start: start, OldEnd: oldEnd, NewEnd: newEnd}
}

func restartUnit(units []parseUnit, offset int) int {
	for index, unit := range units {
		if offset <= unit.span.End {
			if index > 0 {
				return index - 1
			}
			return 0
		}
	}
	if len(units) > 0 {
		return len(units) - 1
	}
	return -1
}

func convergedSuffix(previous *File, source string, change sourceChange, restart, delta int) int {
	units := previous.incremental.units
	for index := restart + 1; index < len(units); index++ {
		unit := units[index]
		if unit.span.Start < change.OldEnd || unit.fragile {
			continue
		}
		start, end := unit.span.Start+delta, unit.span.End+delta
		if start < change.NewEnd || start < 0 || end > len(source) || !physicalLineStart(source, start) || previous.Source[unit.span.Start:unit.span.End] != source[start:end] {
			continue
		}
		// Reparse the first matching clean unit as the convergence guard.
		// Only units after it are cloned from the previous tree.
		return index + 1
	}
	return len(units)
}

func physicalLineStart(source string, offset int) bool {
	return offset == 0 || offset <= len(source) && source[offset-1] == '\n'
}

func appendParsedUnits(result *File, source string, dialect Dialect, start, end int) bool {
	firstLine := true
	for start < end {
		lineEnd, next := physicalLineEnd(source, start)
		if !firstLine && leadingContinuation(source, skipSpace(source, start, lineEnd), lineEnd, dialect) {
			return false
		}
		if next > end {
			next = end
		}
		parsed := parseSourceRange(source[:next], dialect, false, start)
		if !parsedRangeIndependent(parsed, start, next) {
			return false
		}
		parsed.Source = source
		result.Commands = append(result.Commands, parsed.Commands...)
		result.Tokens = append(result.Tokens, parsed.Tokens...)
		result.Diagnostics = append(result.Diagnostics, parsed.Diagnostics...)
		start = next
		firstLine = false
	}
	return true
}

func leadingContinuation(source string, first, end int, dialect Dialect) bool {
	if first >= end {
		return false
	}
	if source[first] == '\\' {
		return true
	}
	if dialect == Vim9 {
		return vim9ContinuationComment(source, first, end) || source[first] == '|' && (first+1 >= end || source[first+1] != '|')
	}
	return legacyContinuationComment(source, first, end)
}

func parsedRangeIndependent(file *File, start, end int) bool {
	if file == nil || len(file.Blocks) != 0 || len(file.Diagnostics) != 0 || file.OpaqueTail != (Span{}) {
		return false
	}
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Span.Start < start || command.Span.End > end || !independentCommand(file, command) {
			return false
		}
	}
	for _, token := range file.Tokens {
		if token.Span.Start < start || token.Span.End > end || token.Kind == TokenContinuation || token.Kind == TokenHeredoc {
			return false
		}
	}
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Span.Start < start || diagnostic.Span.End > end {
			return false
		}
	}
	if len(file.Commands) > 0 {
		last := len(file.Commands) - 1
		if usesVim9Continuation(file.Commands[last]) {
			state := scanVim9Continuation(logicalArgumentText(file, &file.Commands[last]), vim9ContinuationScan{})
			if needsVim9CommandContinuation(file, last, state) {
				return false
			}
		}
	}
	return true
}

func appendClonedUnit(result, previous *File, unit parseUnit, delta int, cloner *astCloner) {
	for index := unit.firstCommand; index < unit.firstCommand+unit.commandCount; index++ {
		command := cloner.command(previous.Commands[index])
		if delta != 0 {
			rebaseCommand(&command, result.Source, delta)
		}
		result.Commands = append(result.Commands, command)
	}
	for _, token := range previous.Tokens[unit.firstToken : unit.firstToken+unit.tokenCount] {
		token.Span = shiftedSpan(token.Span, delta)
		result.Tokens = append(result.Tokens, token)
	}
	for _, diagnostic := range previous.Diagnostics[unit.firstDiagnostic : unit.firstDiagnostic+unit.diagnosticCount] {
		diagnostic.Span = shiftedSpan(diagnostic.Span, delta)
		result.Diagnostics = append(result.Diagnostics, diagnostic)
	}
}

func buildIncrementalMetadata(file *File) *incrementalMetadata {
	if file == nil {
		return nil
	}
	metadata := &incrementalMetadata{
		units:    make([]parseUnit, 0, len(file.Commands)+1),
		eligible: len(file.Blocks) == 0 && file.OpaqueTail == (Span{}),
	}
	command, token, diagnostic := 0, 0, 0
	state := parserState{
		initialDialect: file.Dialect, activeDialect: file.Dialect, scriptVersion: 1,
		vim9Prologue: file.Dialect == Vim9 && startsWithVim9Script(file.Source), lambdaBody: file.lambdaBody,
	}
	if state.vim9Prologue {
		state.activeDialect = Legacy
	}
	for start := 0; start < len(file.Source); {
		_, end := physicalLineEnd(file.Source, start)
		for {
			extended := end
			for index := command; index < len(file.Commands) && file.Commands[index].Span.Start < end; index++ {
				if file.Commands[index].Span.End > extended {
					extended = file.Commands[index].Span.End
				}
			}
			for index := token; index < len(file.Tokens) && file.Tokens[index].Span.Start < end; index++ {
				if file.Tokens[index].Span.End > extended {
					extended = file.Tokens[index].Span.End
				}
			}
			if extended <= end {
				break
			}
			_, end = physicalLineEnd(file.Source, max(start, extended-1))
		}
		unit := parseUnit{
			span: Span{Start: start, End: end}, entry: cloneParserState(state), structureEntry: structurePath(file, start),
			firstCommand: command, firstToken: token, firstDiagnostic: diagnostic, independent: true,
		}
		for command < len(file.Commands) && file.Commands[command].Span.Start < end {
			current := &file.Commands[command]
			if current.Span.Start < start || current.Span.End > end {
				metadata.eligible = false
				unit.independent = false
			} else if !independentCommand(file, current) {
				unit.independent = false
				if current.Canonical != "vim9script" || file.Dialect != Vim9 {
					metadata.eligible = false
				}
			}
			applyParserState(file, &state, current)
			command++
		}
		unit.commandCount = command - unit.firstCommand
		unit.exit = cloneParserState(state)
		unit.structureExit = structurePath(file, end)
		for token < len(file.Tokens) && file.Tokens[token].Span.Start < end {
			if file.Tokens[token].Span.Start < start || file.Tokens[token].Span.End > end || file.Tokens[token].Kind == TokenContinuation || file.Tokens[token].Kind == TokenHeredoc {
				metadata.eligible = false
				unit.independent = false
			}
			token++
		}
		unit.tokenCount = token - unit.firstToken
		for diagnostic < len(file.Diagnostics) && file.Diagnostics[diagnostic].Span.Start < end {
			if file.Diagnostics[diagnostic].Span.Start < start || file.Diagnostics[diagnostic].Span.End > end {
				unit.independent = false
			}
			diagnostic++
		}
		unit.diagnosticCount = diagnostic - unit.firstDiagnostic
		unit.fragile = unit.diagnosticCount > 0
		metadata.units = append(metadata.units, unit)
		start = end
	}
	if len(file.Source) == 0 {
		metadata.units = append(metadata.units, parseUnit{independent: true})
	}
	if command != len(file.Commands) || token != len(file.Tokens) || diagnostic != len(file.Diagnostics) {
		metadata.eligible = false
	}
	return metadata
}

func cloneParserState(state parserState) parserState {
	state.dialectStack = append([]Dialect(nil), state.dialectStack...)
	state.aggregateStack = append([]BlockKind(nil), state.aggregateStack...)
	return state
}

func applyParserState(file *File, state *parserState, command *Command) {
	switch command.Canonical {
	case "vim9script":
		if state.vim9Prologue {
			state.activeDialect = Vim9
			state.vim9Prologue = false
		}
	case "def":
		state.dialectStack = append(state.dialectStack, state.activeDialect)
		state.activeDialect = Vim9
	case "function":
		if isFunctionDefinition(file.Text(command.Argument)) {
			state.dialectStack = append(state.dialectStack, state.activeDialect)
			state.activeDialect = Legacy
		}
	case "enddef", "endfunction":
		if len(state.dialectStack) > 0 {
			state.activeDialect = state.dialectStack[len(state.dialectStack)-1]
			state.dialectStack = state.dialectStack[:len(state.dialectStack)-1]
		}
	case "class", "interface", "enum":
		if command.Dialect == Vim9 {
			kind := BlockClass
			if command.Canonical == "interface" {
				kind = BlockInterface
			} else if command.Canonical == "enum" {
				kind = BlockEnum
			}
			state.aggregateStack = append(state.aggregateStack, kind)
		}
	case "endclass", "endinterface", "endenum":
		if len(state.aggregateStack) > 0 {
			state.aggregateStack = state.aggregateStack[:len(state.aggregateStack)-1]
		}
	case "scriptversion":
		if command.Dialect == Legacy {
			if version, ok := parseScriptVersion(logicalArgumentText(file, command)); ok {
				state.scriptVersion = version
			}
		}
	}
}

func structurePath(file *File, offset int) []BlockKind {
	var path []BlockKind
	for _, block := range file.Blocks {
		if block.Span.Start <= offset && offset <= block.Span.End {
			path = append(path, block.Kind)
		}
	}
	return path
}

func independentCommand(file *File, command *Command) bool {
	if command == nil || command.Embedded != nil || command.Heredoc != nil || command.TextBody != nil || command.Keymap != nil || command.Block >= 0 || command.logical != nil && command.logical.view.Source.Start != command.Span.Start && command.logical.view.Source.End > command.Span.End {
		return false
	}
	if _, opening := openingBlock(file, command); opening {
		return false
	}
	if _, closing := closingBlock(command); closing {
		return false
	}
	switch command.Canonical {
	case "vim9script", "scriptversion", "finish", "loadkeymap", "append", "change", "insert":
		return false
	}
	return !commandHasLambda(command)
}

func commandHasLambda(command *Command) bool {
	seen := make(map[*Expression]bool)
	var has func(*Expression) bool
	has = func(expression *Expression) bool {
		if expression == nil || seen[expression] {
			return false
		}
		seen[expression] = true
		if expression.LambdaBody != nil {
			return true
		}
		for _, child := range expression.Children {
			if has(child) {
				return true
			}
		}
		return false
	}
	for _, expression := range command.Expressions {
		if has(expression) {
			return true
		}
	}
	for _, expression := range command.Targets {
		if has(expression) {
			return true
		}
	}
	return command.Declaration != nil && (has(command.Declaration.Target) || has(command.Declaration.Initializer))
}

func shiftedSpan(span Span, delta int) Span {
	if span == (Span{}) {
		return span
	}
	return Span{Start: span.Start + delta, End: span.End + delta}
}

func rebaseCommand(command *Command, source string, delta int) {
	mapper := logicalSpanMapper{
		source: source, delta: delta, direct: true,
		expressions: make(map[*Expression]bool), types: make(map[*Type]bool),
		files: make(map[*File]bool), lists: make(map[*CommandList]bool),
	}
	mapper.command(command)
	if command.Substitute != nil {
		for index := range command.Substitute.diagnostics {
			command.Substitute.diagnostics[index].Span = shiftedSpan(command.Substitute.diagnostics[index].Span, delta)
		}
	}
}

type astCloner struct {
	expressions map[*Expression]*Expression
	types       map[*Type]*Type
	files       map[*File]*File
	lists       map[*CommandList]*CommandList
}

func newASTCloner() *astCloner {
	return &astCloner{}
}

func (cloner *astCloner) file(file *File) *File {
	if file == nil {
		return nil
	}
	if cloned := cloner.files[file]; cloned != nil {
		return cloned
	}
	if cloner.files == nil {
		cloner.files = make(map[*File]*File)
	}
	cloned := *file
	cloner.files[file] = &cloned
	if file.Commands != nil {
		cloned.Commands = make([]Command, len(file.Commands))
		for index := range file.Commands {
			cloned.Commands[index] = cloner.command(file.Commands[index])
		}
	}
	cloned.Tokens = append([]Token(nil), file.Tokens...)
	cloned.Blocks = cloneBlocks(file.Blocks)
	cloned.Diagnostics = append([]Diagnostic(nil), file.Diagnostics...)
	cloned.incremental = nil
	return &cloned
}

func (cloner *astCloner) command(command Command) Command {
	cloned := command
	cloned.logical = nil
	cloned.boundaryExpression = nil
	cloned.Modifiers = append([]Modifier(nil), command.Modifiers...)
	for index := range cloned.Modifiers {
		if command.Modifiers[index].Filter != nil {
			filter := *command.Modifiers[index].Filter
			cloned.Modifiers[index].Filter = &filter
		}
	}
	cloned.Embedded = cloner.commandList(command.Embedded)
	cloned.Declaration = cloner.declaration(command.Declaration)
	cloned.Expressions = cloner.expressionsSlice(command.Expressions)
	cloned.Targets = cloner.expressionsSlice(command.Targets)
	if command.Heredoc != nil {
		value := *command.Heredoc
		cloned.Heredoc = &value
	}
	if command.TextBody != nil {
		value := *command.TextBody
		value.Lines = append([]Span(nil), command.TextBody.Lines...)
		cloned.TextBody = &value
	}
	cloned.Function = cloner.function(command.Function)
	if command.Aggregate != nil {
		value := *command.Aggregate
		value.Extends = append([]Span(nil), command.Aggregate.Extends...)
		value.Implements = append([]Span(nil), command.Aggregate.Implements...)
		value.Members = append([]int(nil), command.Aggregate.Members...)
		cloned.Aggregate = &value
	}
	if command.TypeAlias != nil {
		value := *command.TypeAlias
		value.Type = cloner.typeNode(command.TypeAlias.Type)
		cloned.TypeAlias = &value
	}
	cloned.EnumValues = append([]EnumValue(nil), command.EnumValues...)
	for index := range cloned.EnumValues {
		cloned.EnumValues[index].Initializer = cloner.expression(command.EnumValues[index].Initializer)
		cloned.EnumValues[index].Arguments = cloner.expressionsSlice(command.EnumValues[index].Arguments)
	}
	if command.Import != nil {
		value := *command.Import
		value.Path = cloner.expression(command.Import.Path)
		cloned.Import = &value
	}
	if command.For != nil {
		value := *command.For
		value.Bindings = cloner.bindings(command.For.Bindings)
		value.Iterable = cloner.expression(command.For.Iterable)
		cloned.For = &value
	}
	if command.Keymap != nil {
		value := *command.Keymap
		value.Entries = append([]KeymapEntry(nil), command.Keymap.Entries...)
		cloned.Keymap = &value
	}
	if command.Mapping != nil {
		value := *command.Mapping
		value.RHSExpression = cloner.expression(command.Mapping.RHSExpression)
		cloned.Mapping = &value
	}
	if command.Highlight != nil {
		value := *command.Highlight
		value.Attributes = append([]HighlightAttribute(nil), command.Highlight.Attributes...)
		cloned.Highlight = &value
	}
	if command.Syntax != nil {
		value := *command.Syntax
		value.Keywords = append([]Span(nil), command.Syntax.Keywords...)
		value.Options = append([]SyntaxOption(nil), command.Syntax.Options...)
		for index := range value.Options {
			value.Options[index].Values = append([]Span(nil), command.Syntax.Options[index].Values...)
		}
		value.Patterns = append([]SyntaxPattern(nil), command.Syntax.Patterns...)
		cloned.Syntax = &value
	}
	if command.Set != nil {
		value := *command.Set
		value.Options = append([]SetOption(nil), command.Set.Options...)
		cloned.Set = &value
	}
	if command.Substitute != nil {
		value := *command.Substitute
		value.Expression = cloner.expression(command.Substitute.Expression)
		value.diagnostics = append([]Diagnostic(nil), command.Substitute.diagnostics...)
		cloned.Substitute = &value
	}
	if command.Autocmd != nil {
		value := *command.Autocmd
		value.Events = append([]Span(nil), command.Autocmd.Events...)
		value.Modifiers = append([]AutocmdModifier(nil), command.Autocmd.Modifiers...)
		cloned.Autocmd = &value
	}
	return cloned
}

func (cloner *astCloner) commandList(list *CommandList) *CommandList {
	if list == nil {
		return nil
	}
	if cloned := cloner.lists[list]; cloned != nil {
		return cloned
	}
	if cloner.lists == nil {
		cloner.lists = make(map[*CommandList]*CommandList)
	}
	cloned := *list
	cloner.lists[list] = &cloned
	if list.Commands != nil {
		cloned.Commands = make([]Command, len(list.Commands))
		for index := range list.Commands {
			cloned.Commands[index] = cloner.command(list.Commands[index])
		}
	}
	cloned.Blocks = cloneBlocks(list.Blocks)
	return &cloned
}

func cloneBlocks(blocks []Block) []Block {
	cloned := append([]Block(nil), blocks...)
	for index := range cloned {
		cloned[index].Branches = append([]int(nil), blocks[index].Branches...)
	}
	return cloned
}

func (cloner *astCloner) expression(expression *Expression) *Expression {
	if expression == nil {
		return nil
	}
	if cloned := cloner.expressions[expression]; cloned != nil {
		return cloned
	}
	if cloner.expressions == nil {
		cloner.expressions = make(map[*Expression]*Expression)
	}
	cloned := *expression
	cloner.expressions[expression] = &cloned
	cloned.Children = cloner.expressionsSlice(expression.Children)
	if expression.TypeArguments != nil {
		cloned.TypeArguments = make([]*Type, len(expression.TypeArguments))
		for index, argument := range expression.TypeArguments {
			cloned.TypeArguments[index] = cloner.typeNode(argument)
		}
	}
	cloned.CastType = cloner.typeNode(expression.CastType)
	cloned.Parameters = cloner.parameters(expression.Parameters)
	cloned.ReturnType = cloner.typeNode(expression.ReturnType)
	cloned.LambdaBody = cloner.file(expression.LambdaBody)
	return &cloned
}

func (cloner *astCloner) expressionsSlice(expressions []*Expression) []*Expression {
	if expressions == nil {
		return nil
	}
	cloned := make([]*Expression, len(expressions))
	for index, expression := range expressions {
		cloned[index] = cloner.expression(expression)
	}
	return cloned
}

func (cloner *astCloner) typeNode(node *Type) *Type {
	if node == nil {
		return nil
	}
	if cloned := cloner.types[node]; cloned != nil {
		return cloned
	}
	if cloner.types == nil {
		cloner.types = make(map[*Type]*Type)
	}
	cloned := *node
	cloner.types[node] = &cloned
	if node.Arguments != nil {
		cloned.Arguments = make([]*Type, len(node.Arguments))
		for index, argument := range node.Arguments {
			cloned.Arguments[index] = cloner.typeNode(argument)
		}
	}
	cloned.ReturnType = cloner.typeNode(node.ReturnType)
	return &cloned
}

func (cloner *astCloner) declaration(declaration *Declaration) *Declaration {
	if declaration == nil {
		return nil
	}
	cloned := *declaration
	cloned.Target = cloner.expression(declaration.Target)
	cloned.Initializer = cloner.expression(declaration.Initializer)
	cloned.ParsedType = cloner.typeNode(declaration.ParsedType)
	cloned.Bindings = cloner.bindings(declaration.Bindings)
	return &cloned
}

func (cloner *astCloner) bindings(bindings []Binding) []Binding {
	cloned := append([]Binding(nil), bindings...)
	for index := range cloned {
		cloned[index].ParsedType = cloner.typeNode(bindings[index].ParsedType)
	}
	return cloned
}

func (cloner *astCloner) parameters(parameters []Parameter) []Parameter {
	cloned := append([]Parameter(nil), parameters...)
	for index := range cloned {
		cloned[index].Target = cloner.expression(parameters[index].Target)
		cloned[index].Type = cloner.typeNode(parameters[index].Type)
		cloned[index].Default = cloner.expression(parameters[index].Default)
	}
	return cloned
}

func (cloner *astCloner) function(function *Function) *Function {
	if function == nil {
		return nil
	}
	cloned := *function
	cloned.TypeParameters = append([]TypeParameter(nil), function.TypeParameters...)
	cloned.Parameters = cloner.parameters(function.Parameters)
	cloned.ReturnType = cloner.typeNode(function.ReturnType)
	return &cloned
}
