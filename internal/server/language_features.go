package server

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const maxCompletionItems = 2000

func (s *Server) DocumentLink(ctx context.Context, params *protocol.DocumentLinkParams) ([]protocol.DocumentLink, error) {
	for attempt := range 2 {
		snapshot, file, encoding, err := s.structureDocument(ctx, params.TextDocument.URI.String())
		if err != nil {
			return nil, err
		}
		if snapshot == nil || file == nil {
			return []protocol.DocumentLink{}, nil
		}
		state := s.captureWorkspaceNavigationState()
		path, ok := workspaceURIPath(uri.URI(snapshot.URI()))
		links := make([]protocol.DocumentLink, 0)
		if ok && state.resolver != nil {
			walkCommands(file.Commands, func(command *syntax.Command) {
				var span syntax.Span
				var target string
				switch {
				case command.Import != nil:
					resolution := state.resolver.ResolveImport(path, file, command.Import)
					if resolution.Dynamic || resolution.Path == "" {
						return
					}
					span, target = command.Import.PathSpan, resolution.Path
				case command.Canonical == "source":
					resolution := state.resolver.ResolveSource(path, file.Text(command.Argument))
					if resolution.Dynamic || resolution.Path == "" {
						return
					}
					span, target = command.Argument, resolution.Path
				default:
					return
				}
				rangeValue, valid := protocolRange(snapshot, encoding, span)
				if !valid {
					return
				}
				targetURI := uri.File(target)
				links = append(links, protocol.DocumentLink{Range: rangeValue, Target: &targetURI})
			})
		}
		document := navigationDocument{server: s, snapshot: snapshot}
		current, err := document.workspaceNavigationCurrent(ctx, state, workspaceNavigationTarget{})
		if err != nil {
			return nil, err
		}
		if current {
			return links, nil
		}
		if attempt == 1 {
			return nil, protocol.ErrContentModified
		}
	}
	return nil, protocol.ErrContentModified
}

func importMemberContext(source string, offset int) (string, bool) {
	if offset < 0 || offset > len(source) {
		return "", false
	}
	lineStart := strings.LastIndexByte(source[:offset], '\n') + 1
	remaining := 256
	start := offset
	for start > lineStart && remaining > 0 && isCompletionIdentifierByte(source[start-1]) {
		start--
		remaining--
	}
	if start > lineStart && remaining == 0 && isCompletionIdentifierByte(source[start-1]) {
		return "", false
	}
	if start == lineStart || source[start-1] != '.' {
		return "", false
	}
	end := start - 1
	start = end
	remaining--
	for start > lineStart && remaining > 0 && isCompletionIdentifierByte(source[start-1]) {
		start--
		remaining--
	}
	if start > lineStart && remaining == 0 && isCompletionIdentifierByte(source[start-1]) {
		return "", false
	}
	if start == end {
		return "", false
	}
	return source[start:end], true
}

func isCompletionIdentifierByte(value byte) bool {
	return value == '_' || value >= '0' && value <= '9' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func (s *Server) importMemberCompletions(documentURI string, file *syntax.File, alias string) protocol.CompletionItemSlice {
	items, _ := s.importMemberCompletionsInState(documentURI, file, alias, s.captureWorkspaceNavigationState())
	return items
}

func (s *Server) importMemberCompletionsInState(documentURI string, file *syntax.File, alias string, state workspaceNavigationSnapshot) (protocol.CompletionItemSlice, workspaceNavigationTarget) {
	path, ok := workspaceURIPath(uri.URI(documentURI))
	if !ok || state.resolver == nil || state.index == nil {
		return protocol.CompletionItemSlice{}, workspaceNavigationTarget{}
	}
	var targetPath string
	walkCommands(file.Commands, func(command *syntax.Command) {
		if targetPath != "" || command.Import == nil || file.Text(command.Import.Alias) != alias {
			return
		}
		resolution := state.resolver.ResolveImport(path, file, command.Import)
		if !resolution.Dynamic {
			targetPath = resolution.Path
		}
	})
	if targetPath == "" {
		return protocol.CompletionItemSlice{}, workspaceNavigationTarget{}
	}
	var facts []workspace.SymbolFact
	var target workspaceNavigationTarget
	s.publishMu.Lock()
	snapshot, _, open := s.openWorkspaceSnapshotLocked(targetPath)
	s.publishMu.Unlock()
	if open {
		targetFile := s.parseSnapshot(snapshot)
		if targetFile == nil {
			return protocol.CompletionItemSlice{}, workspaceNavigationTarget{openSnapshot: snapshot}
		}
		target.openSnapshot = snapshot
		facts = workspace.CollectSymbolFacts(targetPath, targetFile)
	} else {
		matches := state.index.FileSymbols(targetPath)
		facts = make([]workspace.SymbolFact, 0, len(matches))
		for _, match := range matches {
			facts = append(facts, match.Fact)
		}
	}
	items := make(protocol.CompletionItemSlice, 0)
	seen := make(map[string]bool)
	for _, fact := range facts {
		if !fact.Exported || fact.Name == "" || seen[fact.Name] {
			continue
		}
		seen[fact.Name] = true
		item := protocol.CompletionItem{Label: fact.Name, Kind: completionSymbolKind(fact.Kind), Detail: protocol.NewOptional("exported " + string(fact.Kind))}
		if fact.Deprecated {
			item.Tags = []protocol.CompletionItemTag{protocol.CompletionItemTagDeprecated}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Label < items[j].Label })
	return items, target
}

type completionContext uint8

const (
	completionContextNone completionContext = iota
	completionContextCommand
	completionContextExpression
	completionContextModifier
	completionContextSetOption
	completionContextSyntaxSubcommand
	completionContextSyntaxGroup
	completionContextHighlight
	completionContextAutocmdHead
	completionContextAutocmdEvent
	completionContextImportPath
	completionContextMember
)

func completionContextAt(file *syntax.File, offset int) completionContext {
	if file == nil || offset < 0 || offset > len(file.Source) || spanContains(file.OpaqueTail, offset) {
		return completionContextNone
	}
	for _, token := range file.Tokens {
		if token.Kind == syntax.TokenComment && token.Span.Start <= offset && offset <= token.Span.End {
			return completionContextNone
		}
	}
	rejected := false
	walkCommands(file.Commands, func(command *syntax.Command) {
		if command.Heredoc != nil && (spanContains(command.Heredoc.Body, offset) || offset == command.Heredoc.Body.End) || command.TextBody != nil && (spanContains(command.TextBody.Body, offset) || offset == command.TextBody.Body.End) || command.Keymap != nil && (spanContains(command.Keymap.Body, offset) || offset == command.Keymap.Body.End) || command.Mapping != nil && (spanContains(command.Mapping.RHS, offset) || offset == command.Mapping.RHS.End) {
			rejected = true
		}
		walkCommandExpressions(command, func(expression *syntax.Expression) {
			if (expression.Kind == syntax.ExpressionString || expression.Kind == syntax.ExpressionInterpolatedString) && expression.Span.Start < offset && offset < expression.Span.End {
				rejected = true
			}
		})
	})
	if rejected {
		return completionContextNone
	}
	result := completionContextNone
	walkCommands(file.Commands, func(command *syntax.Command) {
		if !spanContains(command.Span, offset) && offset != command.Span.End {
			return
		}
		if command.Set != nil {
			for _, option := range command.Set.Options {
				if spanContains(option.Name, offset) || offset == option.Name.End {
					result = completionContextSetOption
					return
				}
			}
		}
		if command.Import != nil && command.Import.Path != nil && command.Import.Path.Kind == syntax.ExpressionString &&
			command.Import.PathSpan.Start < offset && offset < command.Import.PathSpan.End {
			result = completionContextImportPath
			return
		}
		if command.Syntax != nil {
			if spanContains(command.Syntax.Subcommand, offset) || offset == command.Syntax.Subcommand.End {
				result = completionContextSyntaxSubcommand
				return
			}
			if spanContains(command.Syntax.Group, offset) || offset == command.Syntax.Group.End {
				result = completionContextSyntaxGroup
				return
			}
			for _, span := range command.Syntax.Keywords {
				if spanContains(span, offset) || offset == span.End {
					result = completionContextSyntaxGroup
					return
				}
			}
		}
		if command.Canonical == "syntax" && (spanContains(command.Argument, offset) || offset == command.Argument.End) {
			if completionArgumentWord(file.Source, command.Argument, offset) == 0 {
				result = completionContextSyntaxSubcommand
			} else {
				result = completionContextSyntaxGroup
			}
			return
		}
		if command.Highlight != nil && (spanContains(command.Highlight.Group, offset) || offset == command.Highlight.Group.End || spanContains(command.Highlight.LinkTarget, offset) || offset == command.Highlight.LinkTarget.End) {
			result = completionContextHighlight
			return
		}
		if command.Autocmd != nil {
			if spanContains(command.Autocmd.Head, offset) || offset == command.Autocmd.Head.End {
				result = completionContextAutocmdHead
				return
			}
			for _, event := range command.Autocmd.Events {
				if spanContains(event, offset) || offset == event.End {
					result = completionContextAutocmdEvent
					return
				}
			}
		}
		if command.Canonical == "autocmd" && (spanContains(command.Argument, offset) || offset == command.Argument.End) && completionArgumentWord(file.Source, command.Argument, offset) > 0 {
			result = completionContextAutocmdEvent
			return
		}
		for _, modifier := range command.Modifiers {
			if spanContains(modifier.Span, offset) || offset == modifier.Span.End {
				result = completionContextModifier
				return
			}
		}
		if result != completionContextNone {
			return
		}
		if command.Span.Start <= offset && offset <= command.Name.End {
			result = completionContextCommand
			return
		}
		if spanContains(command.Argument, offset) || offset == command.Argument.End {
			result = completionContextExpression
		}
		for _, expression := range append(append([]*syntax.Expression(nil), command.Expressions...), command.Targets...) {
			if expression != nil && (spanContains(expression.Span, offset) || offset == expression.Span.End) {
				result = completionContextExpression
			}
			if memberExpressionAt(expression, offset) != nil {
				result = completionContextMember
			}
		}
		if command.Declaration != nil && command.Declaration.Initializer != nil && (spanContains(command.Declaration.Initializer.Span, offset) || offset == command.Declaration.Initializer.Span.End) {
			result = completionContextExpression
		}
	})
	if result != completionContextNone {
		return result
	}
	lineStart := strings.LastIndexByte(file.Source[:offset], '\n') + 1
	if strings.TrimSpace(file.Source[lineStart:offset]) == "" {
		return completionContextCommand
	}
	return completionContextNone
}

func memberExpressionAt(expression *syntax.Expression, offset int) *syntax.Expression {
	if expression == nil || offset < expression.Span.Start || offset > expression.Span.End {
		return nil
	}
	for _, child := range expression.Children {
		if member := memberExpressionAt(child, offset); member != nil {
			return member
		}
	}
	if expression.Kind == syntax.ExpressionMember && expression.Operator.Start < offset && offset <= expression.Span.End {
		return expression
	}
	return nil
}

func completionAggregateAt(file *syntax.File, offset int) bool {
	found := false
	walkCommands(file.Commands, func(command *syntax.Command) {
		if command.Aggregate != nil && (spanContains(command.Argument, offset) || offset == command.Argument.End) {
			found = true
		}
	})
	return found
}

func completionArgumentWord(source string, span syntax.Span, offset int) int {
	word, inWord := 0, false
	for i := span.Start; i < span.End && i < len(source); i++ {
		space := source[i] == ' ' || source[i] == '\t'
		if !space && !inWord {
			inWord = true
			if i > span.Start {
				word++
			}
		}
		if space {
			inWord = false
		}
		if i >= offset {
			break
		}
	}
	return word
}

func (s *Server) CompletionResolve(ctx context.Context, item *protocol.CompletionItem) (*protocol.CompletionItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, protocol.ErrRequestCancelled
	}
	if item == nil {
		return nil, nil
	}
	result := *item
	if function, ok := vimdata.LookupFunction(item.Label); ok {
		if _, set := result.Detail.Get(); !set {
			result.Detail = protocol.NewOptional(builtinFunctionDetail(function))
		}
		if result.Documentation == nil && function.Documentation != "" {
			result.Documentation = protocol.String(function.Documentation)
		}
		return &result, nil
	}
	if option, ok := vimdata.LookupOption(item.Label); ok {
		if _, set := result.Detail.Get(); !set {
			result.Detail = protocol.NewOptional(completionOptionDetail(option))
		}
		if result.Documentation == nil && option.Documentation != "" {
			result.Documentation = protocol.String(option.Documentation)
		}
		return &result, nil
	}
	if variable, ok := vimdata.LookupVariable(item.Label); ok {
		if _, set := result.Detail.Get(); !set {
			result.Detail = protocol.NewOptional("variable: " + variable.Type)
		}
		if result.Documentation == nil && variable.Documentation != "" {
			result.Documentation = protocol.String(variable.Documentation)
		}
		return &result, nil
	}
	if command, ok := vimdata.Lookup(item.Label); ok && command.Name == item.Label {
		if _, set := result.Detail.Get(); !set {
			result.Detail = protocol.NewOptional("Ex command")
		}
	}
	return &result, nil
}

func builtinFunctionDetail(function vimdata.BuiltinFunction) string {
	args := fmt.Sprintf("%d", function.MinArgs)
	if function.MaxArgs < 0 {
		args += "+"
	} else if function.MaxArgs != function.MinArgs {
		args += fmt.Sprintf("..%d", function.MaxArgs)
	}
	detail := "builtin function (" + args + " arguments)"
	if returnType := function.ReturnType.DisplayName(); returnType != "" {
		detail += ": " + returnType
	}
	return detail
}

func completionSymbolKind(kind analysis.SymbolKind) protocol.CompletionItemKind {
	switch kind {
	case analysis.SymbolKindFunction, analysis.SymbolKindMethod, analysis.SymbolKindConstructor:
		return protocol.CompletionItemKindFunction
	case analysis.SymbolKindClass, analysis.SymbolKindInterface, analysis.SymbolKindEnum, analysis.SymbolKindTypeAlias:
		return protocol.CompletionItemKindClass
	case analysis.SymbolKindImport:
		return protocol.CompletionItemKindModule
	case analysis.SymbolKindConstant, analysis.SymbolKindEnumMember:
		return protocol.CompletionItemKindConstant
	default:
		return protocol.CompletionItemKindVariable
	}
}

func (s *Server) SignatureHelp(ctx context.Context, params *protocol.SignatureHelpParams) (*protocol.SignatureHelp, error) {
	snapshot, file, encoding, err := s.structureDocument(ctx, params.TextDocument.URI.String())
	if err != nil {
		return nil, err
	}
	if snapshot == nil || file == nil {
		return nil, nil
	}
	offset, err := snapshot.Offset(fromProtocolPosition(params.Position), encoding)
	if err != nil {
		return nil, s.structureCurrent(ctx, snapshot)
	}
	call := callAt(file, offset)
	if call == nil || call.Value == "->" || len(call.Children) == 0 {
		return nil, s.structureCurrent(ctx, snapshot)
	}
	callable := call.Children[0]
	if callable.Kind != syntax.ExpressionIdentifier {
		return nil, s.structureCurrent(ctx, snapshot)
	}
	fileAnalysis := analysis.Analyze(file)
	declaration := declarationForExpression(fileAnalysis, callable)
	if declaration == nil {
		return nil, s.structureCurrent(ctx, snapshot)
	}
	function := functionForDeclaration(file.Commands, declaration)
	if function == nil {
		return nil, s.structureCurrent(ctx, snapshot)
	}
	label, parameters := formatFunctionSignature(file, declaration.Name, function)
	active := activeCallParameter(call, offset)
	if len(parameters) > 0 && active >= uint32(len(parameters)) {
		active = uint32(len(parameters) - 1)
	}
	zero := uint32(0)
	result := &protocol.SignatureHelp{
		Signatures:      []protocol.SignatureInformation{{Label: label, Parameters: parameters, ActiveParameter: protocol.NewNullable(active)}},
		ActiveSignature: &zero,
		ActiveParameter: protocol.NewNullable(active),
	}
	if err := s.structureCurrent(ctx, snapshot); err != nil {
		return nil, err
	}
	return result, nil
}

func declarationForExpression(result *analysis.FileAnalysis, expression *syntax.Expression) *analysis.Declaration {
	for _, reference := range result.References {
		if reference.Span == expression.Span {
			return reference.Declaration
		}
	}
	for _, declaration := range result.Declarations {
		if declaration.Span == expression.Span {
			return declaration
		}
	}
	return nil
}

func functionForDeclaration(commands []syntax.Command, declaration *analysis.Declaration) *syntax.Function {
	var found *syntax.Function
	walkCommands(commands, func(command *syntax.Command) {
		if found == nil && command.Function != nil && command.Function.Name == declaration.Span {
			found = command.Function
		}
	})
	return found
}

func formatFunctionSignature(file *syntax.File, name string, function *syntax.Function) (string, []protocol.ParameterInformation) {
	parts := make([]string, 0, len(function.Parameters))
	parameters := make([]protocol.ParameterInformation, 0, len(function.Parameters))
	for _, parameter := range function.Parameters {
		label := file.Text(parameter.Name)
		if parameter.Type != nil {
			label += ": " + file.Text(parameter.TypeSpan)
		}
		if parameter.Default != nil {
			label += " = " + file.Text(parameter.DefaultSpan)
		}
		if parameter.Variadic && !strings.HasPrefix(label, "...") {
			label = "..." + label
		}
		parts = append(parts, label)
		parameters = append(parameters, protocol.ParameterInformation{Label: protocol.String(label)})
	}
	label := name + "(" + strings.Join(parts, ", ") + ")"
	if function.ReturnType != nil {
		label += ": " + file.Text(function.ReturnTypeSpan)
	}
	return label, parameters
}

func activeCallParameter(call *syntax.Expression, offset int) uint32 {
	var active uint32
	for _, argument := range call.Children[1:] {
		if offset <= argument.Span.End {
			break
		}
		active++
	}
	return active
}

func callAt(file *syntax.File, offset int) *syntax.Expression {
	var found *syntax.Expression
	walkCommands(file.Commands, func(command *syntax.Command) {
		walkCommandExpressions(command, func(expression *syntax.Expression) {
			if expression.Kind != syntax.ExpressionCall || expression.Span.Start > offset || offset > expression.Span.End {
				return
			}
			if found == nil || expression.Span.End-expression.Span.Start < found.Span.End-found.Span.Start {
				found = expression
			}
		})
	})
	return found
}

func walkCommands(commands []syntax.Command, visit func(*syntax.Command)) {
	for index := range commands {
		command := &commands[index]
		visit(command)
		if command.Embedded != nil {
			walkCommands(command.Embedded.Commands, visit)
		}
	}
}

func walkCommandExpressions(command *syntax.Command, visit func(*syntax.Expression)) {
	walk := func(expression *syntax.Expression) {}
	walk = func(expression *syntax.Expression) {
		if expression == nil {
			return
		}
		visit(expression)
		for _, child := range expression.Children {
			walk(child)
		}
		if expression.LambdaBody != nil {
			walkCommands(expression.LambdaBody.Commands, func(command *syntax.Command) { walkCommandExpressions(command, visit) })
		}
	}
	for _, expression := range command.Expressions {
		walk(expression)
	}
	for _, expression := range command.Targets {
		walk(expression)
	}
	if command.Declaration != nil {
		walk(command.Declaration.Initializer)
	}
	if command.For != nil {
		walk(command.For.Iterable)
	}
	if command.Mapping != nil {
		walk(command.Mapping.RHSExpression)
	}
	for _, value := range command.EnumValues {
		walk(value.Initializer)
	}
}
