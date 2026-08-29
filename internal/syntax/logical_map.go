package syntax

// parseLogicalCommandDetails parses a command against Vim's normalized
// logical-line text, then maps every produced node back to the original
// source.  The public Command header has already been mapped by
// scanLogicalCommands().
func parseLogicalCommandDetails(file *File, command *Command) {
	logical := command.logical
	if logical == nil {
		parseCommandDetails(file, command)
		return
	}
	if len(command.EnumValues) > 0 {
		return
	}

	temporaryCommand := logical.command
	temporaryCommand.ScriptVersion = command.ScriptVersion
	temporaryCommand.Block = command.Block
	temporaryCommand.hasNextStatement = command.hasNextStatement
	temporary := &File{
		Dialect: file.Dialect,
		Source:  logical.view.Text,
		Blocks:  file.Blocks,
	}
	parseCommandDetails(temporary, &temporaryCommand)
	if declaration := temporaryCommand.Declaration; declaration != nil && declaration.Initializer != nil &&
		declaration.Initializer.Kind == ExpressionList && len(temporary.Diagnostics) == 1 &&
		(temporary.Diagnostics[0].Code == "vim/E696" || temporary.Diagnostics[0].Code == "vim/E697") &&
		temporary.Diagnostics[0].Span.Start == temporaryCommand.Argument.End && logical.view.Next < len(file.Source) {
		end := trimExpressionSpaceEnd(logical.view.Text, temporaryCommand.Argument.Start, temporaryCommand.Argument.End)
		nextEnd, _ := physicalLineEnd(file.Source, logical.view.Next)
		nextStart := skipSpace(file.Source, logical.view.Next, nextEnd)
		if end > temporaryCommand.Argument.Start && logical.view.Text[end-1] == ',' &&
			startsVim9RecoveryCommand(file.Source, nextStart, nextEnd) {
			// A following statement ends this incomplete continuation.  Retain
			// the parsed items, but keep the unterminated List itself on its '['.
			declaration.Initializer.Span.End = declaration.Initializer.Span.Start + 1
			for _, expression := range temporaryCommand.Expressions {
				if expression != nil && expression.Kind == ExpressionAssignment && len(expression.Children) == 2 && expression.Children[1] == declaration.Initializer {
					expression.Span.End = declaration.Initializer.Span.End
				}
			}
			temporary.Diagnostics[0].Code = "vimls/missing-delimiter"
			temporary.Diagnostics[0].Message = "expected ] before following command"
		}
	}
	logical.command.boundaryExpression = nil
	command.boundaryExpression = nil
	normalizeLambdaBodySources(temporary)

	mapper := logicalSpanMapper{
		view:        logical.view,
		source:      file.Source,
		expressions: make(map[*Expression]bool),
		types:       make(map[*Type]bool),
		files:       make(map[*File]bool),
		lists:       make(map[*CommandList]bool),
	}
	mapper.commandDetails(&temporaryCommand)
	for index := range temporary.Diagnostics {
		temporary.Diagnostics[index].Span = logical.view.mapSpan(temporary.Diagnostics[index].Span)
	}
	file.Diagnostics = append(file.Diagnostics, temporary.Diagnostics...)

	command.Embedded = temporaryCommand.Embedded
	command.Declaration = temporaryCommand.Declaration
	command.Expressions = temporaryCommand.Expressions
	command.Targets = temporaryCommand.Targets
	command.Count = temporaryCommand.Count
	command.Function = temporaryCommand.Function
	command.Aggregate = temporaryCommand.Aggregate
	command.TypeAlias = temporaryCommand.TypeAlias
	command.Import = temporaryCommand.Import
	command.For = temporaryCommand.For
	command.Mapping = temporaryCommand.Mapping
	command.Highlight = temporaryCommand.Highlight
	command.Syntax = temporaryCommand.Syntax
	command.Set = temporaryCommand.Set
	command.Substitute = temporaryCommand.Substitute
	command.Autocmd = temporaryCommand.Autocmd
}

type logicalSpanMapper struct {
	view        *logicalView
	source      string
	expressions map[*Expression]bool
	types       map[*Type]bool
	files       map[*File]bool
	lists       map[*CommandList]bool
}

func (mapper *logicalSpanMapper) commandDetails(command *Command) {
	if command == nil {
		return
	}
	command.Count = mapper.optional(command.Count)
	mapper.commandList(command.Embedded)
	if command.Declaration != nil {
		declaration := command.Declaration
		declaration.Name = mapper.span(declaration.Name)
		declaration.Type = mapper.optional(declaration.Type)
		declaration.Assignment = mapper.optional(declaration.Assignment)
		mapper.expression(declaration.Target)
		mapper.expression(declaration.Initializer)
		mapper.typeNode(declaration.ParsedType)
		for index := range declaration.Bindings {
			mapper.binding(&declaration.Bindings[index])
		}
	}
	for _, expression := range command.Expressions {
		mapper.expression(expression)
	}
	for _, target := range command.Targets {
		mapper.expression(target)
	}
	if command.Function != nil {
		function := command.Function
		function.Name = mapper.span(function.Name)
		function.ReturnTypeSpan = mapper.optional(function.ReturnTypeSpan)
		function.Attributes = mapper.optional(function.Attributes)
		mapper.typeNode(function.ReturnType)
		for index := range function.TypeParameters {
			function.TypeParameters[index].Span = mapper.span(function.TypeParameters[index].Span)
		}
		for index := range function.Parameters {
			mapper.parameter(&function.Parameters[index])
		}
	}
	if command.Aggregate != nil {
		aggregate := command.Aggregate
		aggregate.Name = mapper.span(aggregate.Name)
		for index := range aggregate.Extends {
			aggregate.Extends[index] = mapper.span(aggregate.Extends[index])
		}
		for index := range aggregate.Implements {
			aggregate.Implements[index] = mapper.span(aggregate.Implements[index])
		}
	}
	if command.TypeAlias != nil {
		alias := command.TypeAlias
		alias.Name = mapper.span(alias.Name)
		alias.Assignment = mapper.optional(alias.Assignment)
		alias.TypeSpan = mapper.optional(alias.TypeSpan)
		mapper.typeNode(alias.Type)
	}
	for index := range command.EnumValues {
		value := &command.EnumValues[index]
		value.Name = mapper.span(value.Name)
		mapper.expression(value.Initializer)
		for _, argument := range value.Arguments {
			mapper.expression(argument)
		}
	}
	if command.Import != nil {
		command.Import.PathSpan = mapper.optional(command.Import.PathSpan)
		command.Import.Alias = mapper.optional(command.Import.Alias)
		mapper.expression(command.Import.Path)
	}
	if command.For != nil {
		command.For.IterableSpan = mapper.optional(command.For.IterableSpan)
		mapper.expression(command.For.Iterable)
		for index := range command.For.Bindings {
			mapper.binding(&command.For.Bindings[index])
		}
	}
	if command.Mapping != nil {
		command.Mapping.LHS = mapper.optional(command.Mapping.LHS)
		command.Mapping.RHS = mapper.optional(command.Mapping.RHS)
		mapper.expression(command.Mapping.RHSExpression)
	}
	if command.Substitute != nil {
		substitute := command.Substitute
		substitute.Delimiter = mapper.optional(substitute.Delimiter)
		substitute.Pattern = mapper.optional(substitute.Pattern)
		substitute.PatternDelimiter = mapper.optional(substitute.PatternDelimiter)
		substitute.Replacement = mapper.optional(substitute.Replacement)
		substitute.ReplacementDelimiter = mapper.optional(substitute.ReplacementDelimiter)
		substitute.Flags = mapper.optional(substitute.Flags)
		substitute.Count = mapper.optional(substitute.Count)
		substitute.PreviousPattern = mapper.optional(substitute.PreviousPattern)
		substitute.ReplacementPrefix = mapper.optional(substitute.ReplacementPrefix)
		substitute.ExpressionSpan = mapper.optional(substitute.ExpressionSpan)
		mapper.expression(substitute.Expression)
	}
	if command.Highlight != nil {
		highlight := command.Highlight
		highlight.Default = mapper.optional(highlight.Default)
		highlight.Operation = mapper.optional(highlight.Operation)
		highlight.Group = mapper.optional(highlight.Group)
		highlight.LinkTarget = mapper.optional(highlight.LinkTarget)
		for index := range highlight.Attributes {
			attribute := &highlight.Attributes[index]
			attribute.Key = mapper.optional(attribute.Key)
			attribute.Equal = mapper.optional(attribute.Equal)
			attribute.Value = mapper.optional(attribute.Value)
		}
	}
	if command.Syntax != nil {
		syntax := command.Syntax
		syntax.Subcommand = mapper.optional(syntax.Subcommand)
		syntax.Group = mapper.optional(syntax.Group)
		for index := range syntax.Keywords {
			syntax.Keywords[index] = mapper.optional(syntax.Keywords[index])
		}
		for index := range syntax.Options {
			option := &syntax.Options[index]
			option.Name = mapper.optional(option.Name)
			option.Equal = mapper.optional(option.Equal)
			option.Value = mapper.optional(option.Value)
			for item := range option.Values {
				option.Values[item] = mapper.optional(option.Values[item])
			}
		}
		for index := range syntax.Patterns {
			pattern := &syntax.Patterns[index]
			pattern.Key = mapper.optional(pattern.Key)
			pattern.Equal = mapper.optional(pattern.Equal)
			pattern.OpenDelimiter = mapper.optional(pattern.OpenDelimiter)
			pattern.Pattern = mapper.optional(pattern.Pattern)
			pattern.CloseDelimiter = mapper.optional(pattern.CloseDelimiter)
			pattern.Offsets = mapper.optional(pattern.Offsets)
		}
	}
	if command.Set != nil {
		for index := range command.Set.Options {
			option := &command.Set.Options[index]
			option.Span = mapper.span(option.Span)
			option.Prefix = mapper.optional(option.Prefix)
			option.Name = mapper.optional(option.Name)
			option.Operator = mapper.optional(option.Operator)
			option.Value = mapper.optional(option.Value)
		}
	}
	if command.Autocmd != nil {
		autocmd := command.Autocmd
		autocmd.Head = mapper.optional(autocmd.Head)
		autocmd.Group = mapper.optional(autocmd.Group)
		autocmd.Pattern = mapper.optional(autocmd.Pattern)
		for index := range autocmd.Events {
			autocmd.Events[index] = mapper.span(autocmd.Events[index])
		}
		for index := range autocmd.Modifiers {
			autocmd.Modifiers[index].Span = mapper.span(autocmd.Modifiers[index].Span)
		}
	}
}

func (mapper *logicalSpanMapper) command(command *Command) {
	if command == nil {
		return
	}
	command.Span = mapper.span(command.Span)
	command.Range = mapper.optional(command.Range)
	command.Name = mapper.optional(command.Name)
	command.Bang = mapper.optional(command.Bang)
	command.Count = mapper.optional(command.Count)
	command.Argument = mapper.span(command.Argument)
	for index := range command.Modifiers {
		modifier := &command.Modifiers[index]
		modifier.Span = mapper.span(modifier.Span)
		modifier.Bang = mapper.optional(modifier.Bang)
		if modifier.Filter != nil {
			modifier.Filter.Delimiter = mapper.optional(modifier.Filter.Delimiter)
			modifier.Filter.Pattern = mapper.optional(modifier.Filter.Pattern)
			modifier.Filter.Flags = mapper.optional(modifier.Filter.Flags)
		}
	}
	mapper.commandDetails(command)
	if command.Heredoc != nil {
		command.Heredoc.Body = mapper.optional(command.Heredoc.Body)
		command.Heredoc.EndMarker = mapper.optional(command.Heredoc.EndMarker)
	}
	if command.Keymap != nil {
		command.Keymap.Body = mapper.span(command.Keymap.Body)
		for index := range command.Keymap.Entries {
			command.Keymap.Entries[index].From = mapper.span(command.Keymap.Entries[index].From)
			command.Keymap.Entries[index].To = mapper.span(command.Keymap.Entries[index].To)
		}
	}
}

func (mapper *logicalSpanMapper) commandList(list *CommandList) {
	if list == nil || mapper.lists[list] {
		return
	}
	mapper.lists[list] = true
	list.Span = mapper.span(list.Span)
	for index := range list.Commands {
		mapper.command(&list.Commands[index])
	}
	for index := range list.Blocks {
		list.Blocks[index].Span = mapper.span(list.Blocks[index].Span)
	}
}

func (mapper *logicalSpanMapper) expression(expression *Expression) {
	if expression == nil || mapper.expressions[expression] {
		return
	}
	mapper.expressions[expression] = true
	expression.Span = mapper.span(expression.Span)
	expression.Operator = mapper.optional(expression.Operator)
	expression.ReturnTypeSpan = mapper.optional(expression.ReturnTypeSpan)
	for _, child := range expression.Children {
		mapper.expression(child)
	}
	for _, argument := range expression.TypeArguments {
		mapper.typeNode(argument)
	}
	mapper.typeNode(expression.CastType)
	mapper.typeNode(expression.ReturnType)
	for index := range expression.Parameters {
		mapper.parameter(&expression.Parameters[index])
	}
	mapper.file(expression.LambdaBody)
}

func (mapper *logicalSpanMapper) typeNode(node *Type) {
	if node == nil || mapper.types[node] {
		return
	}
	mapper.types[node] = true
	node.Span = mapper.span(node.Span)
	for _, argument := range node.Arguments {
		mapper.typeNode(argument)
	}
	mapper.typeNode(node.ReturnType)
}

func (mapper *logicalSpanMapper) binding(binding *Binding) {
	if binding == nil {
		return
	}
	binding.Name = mapper.span(binding.Name)
	binding.Type = mapper.optional(binding.Type)
	mapper.typeNode(binding.ParsedType)
}

func (mapper *logicalSpanMapper) parameter(parameter *Parameter) {
	if parameter == nil {
		return
	}
	parameter.Name = mapper.span(parameter.Name)
	parameter.TypeSpan = mapper.optional(parameter.TypeSpan)
	parameter.DefaultSpan = mapper.optional(parameter.DefaultSpan)
	mapper.typeNode(parameter.Type)
	mapper.expression(parameter.Target)
	mapper.expression(parameter.Default)
}

func (mapper *logicalSpanMapper) file(file *File) {
	if file == nil || mapper.files[file] {
		return
	}
	mapper.files[file] = true
	file.Source = mapper.source
	file.OpaqueTail = mapper.optional(file.OpaqueTail)
	for index := range file.Commands {
		mapper.command(&file.Commands[index])
	}
	for index := range file.Tokens {
		file.Tokens[index].Span = mapper.span(file.Tokens[index].Span)
	}
	for index := range file.Blocks {
		file.Blocks[index].Span = mapper.span(file.Blocks[index].Span)
	}
	for index := range file.Diagnostics {
		file.Diagnostics[index].Span = mapper.span(file.Diagnostics[index].Span)
	}
}

func (mapper *logicalSpanMapper) span(span Span) Span {
	return mapper.view.mapSpan(span)
}

func (mapper *logicalSpanMapper) optional(span Span) Span {
	return mapOptionalLogicalSpan(mapper.view, span)
}
