package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const maxLanguageFeatureDocumentationBytes = 16 << 10

const maxCompletionItems = 2000

func (s *Server) DocumentLink(ctx context.Context, params *protocol.DocumentLinkParams) ([]protocol.DocumentLink, error) {
	if err := s.waitForWorkspaceIndex(ctx); err != nil {
		return nil, err
	}
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
					resolution := resolveImportInState(state, path, file, command.Import)
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
		resolution := resolveImportInState(state, path, file, command.Import)
		if !resolution.Dynamic {
			targetPath = resolution.Path
		}
	})
	if targetPath == "" {
		return protocol.CompletionItemSlice{}, workspaceNavigationTarget{}
	}
	var facts []workspace.SymbolFact
	target := workspaceNavigationTarget{match: workspace.SymbolMatch{Fact: workspace.SymbolFact{Path: targetPath}}}
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
	completionContextSetOperator
	completionContextSetValue
	completionContextSyntaxSubcommand
	completionContextSyntaxGroup
	completionContextHighlight
	completionContextHighlightKey
	completionContextHighlightValue
	completionContextAutocmdHead
	completionContextAutocmdEvent
	completionContextAutocmdPattern
	completionContextAutocmdModifier
	completionContextAugroup
	completionContextUserCommandAttribute
	completionContextUserCommandAttributeValue
	completionContextColorscheme
	completionContextImportPath
	completionContextMember
	completionContextMappingArgument
	completionContextHasFeature
	completionContextExpandSpecial
)

func completionContextAt(file *syntax.File, offset int) completionContext {
	if file == nil || offset < 0 || offset > len(file.Source) || spanContains(file.OpaqueTail, offset) {
		return completionContextNone
	}
	if contextKind, _ := completionBuiltinStringAt(file, offset); contextKind != completionContextNone {
		return contextKind
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
			if contextKind, ok := completionHighlightContextAt(file, command, offset); ok {
				result = contextKind
			}
			return
		}
		if command.Set != nil {
			for _, option := range command.Set.Options {
				if option.Value.Start <= offset && offset <= option.Value.End && option.Operator.Start < option.Operator.End {
					result = completionContextSetValue
					return
				}
				if option.Name.End < offset && offset <= option.Span.End {
					result = completionContextSetOperator
					return
				}
				if spanContains(option.Name, offset) || offset == option.Name.End {
					result = completionContextSetOption
					return
				}
			}
		}
		if command.Canonical == "augroup" && (spanContains(command.Augroup, offset) || offset == command.Augroup.End) {
			result = completionContextAugroup
			return
		}
		if command.UserCommand != nil {
			for _, attribute := range command.UserCommand.Attributes {
				if attribute.Equal.Start < attribute.Equal.End && attribute.Value.Start <= offset && offset <= attribute.Value.End {
					result = completionContextUserCommandAttributeValue
					return
				}
				nameStart := attribute.Span.Start
				nameEnd := attribute.Name.End
				if nameStart <= offset && offset <= nameEnd {
					result = completionContextUserCommandAttribute
					return
				}
			}
		}
		if completionMappingArgumentAt(file, command, offset) {
			result = completionContextMappingArgument
			return
		}
		if command.Mapping != nil && command.Argument.Start <= offset && offset <= command.Argument.End {
			result = completionContextNone
			return
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
		if contextKind, ok := completionHighlightContextAt(file, command, offset); ok {
			result = contextKind
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
			if spanContains(command.Autocmd.Pattern, offset) || offset == command.Autocmd.Pattern.End {
				result = completionContextAutocmdPattern
				return
			}
			if command.Bang.Start == command.Bang.End && autocmdModifierPrefixAt(file.Source, command.Autocmd, offset) {
				result = completionContextAutocmdModifier
				return
			}
		}
		if command.Canonical == "colorscheme" && offset > command.Name.End &&
			(spanContains(command.Argument, offset) || offset == command.Argument.End) &&
			completionArgumentWord(file.Source, command.Argument, offset) == 0 {
			result = completionContextColorscheme
			return
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
	linePrefix := strings.TrimSpace(file.Source[lineStart:offset])
	if linePrefix == "" || linePrefix == ":" {
		return completionContextCommand
	}
	return completionContextNone
}

func completionBuiltinStringAt(file *syntax.File, offset int) (completionContext, *syntax.Expression) {
	if file == nil || offset < 0 || offset > len(file.Source) {
		return completionContextNone, nil
	}
	contextKind := completionContextNone
	var argument *syntax.Expression
	walkCommands(file.Commands, func(command *syntax.Command) {
		walkCommandExpressions(command, func(expression *syntax.Expression) {
			if expression == nil {
				return
			}
			name := ""
			var candidate *syntax.Expression
			direct := false
			switch {
			case expression.Kind == syntax.ExpressionCall && expression.Value == "" && len(expression.Children) >= 2 && expression.Children[0] != nil && expression.Children[0].Kind == syntax.ExpressionIdentifier:
				name, candidate, direct = expression.Children[0].Value, expression.Children[1], true
			case expression.Kind == syntax.ExpressionMember && file.Text(expression.Operator) == "->" && len(expression.Children) == 1:
				name, candidate = expression.Value, expression.Children[0]
			default:
				return
			}
			if candidate == nil || candidate.Kind != syntax.ExpressionString {
				return
			}
			content, ok := completionStringContent(file, candidate)
			if !ok || offset < content.Start || offset > content.End {
				return
			}
			switch name {
			case "has":
				if !direct {
					return
				}
				contextKind = completionContextHasFeature
			case "expand":
				if colon := strings.IndexByte(file.Source[content.Start:content.End], ':'); colon >= 0 && offset > content.Start+colon {
					return
				}
				contextKind = completionContextExpandSpecial
			default:
				return
			}
			argument = candidate
		})
	})
	return contextKind, argument
}

func completionStringContent(file *syntax.File, expression *syntax.Expression) (syntax.Span, bool) {
	if file == nil || expression == nil || expression.Kind != syntax.ExpressionString || expression.Span.Start >= expression.Span.End || expression.Span.End > len(file.Source) {
		return syntax.Span{}, false
	}
	quote := file.Source[expression.Span.Start]
	if quote != '\'' && quote != '"' {
		return syntax.Span{}, false
	}
	content := syntax.Span{Start: expression.Span.Start + 1, End: expression.Span.End}
	if content.End > content.Start && file.Source[content.End-1] == quote {
		content.End--
	}
	return content, true
}

// completionBuiltinStringValueSpan returns the hover span for a value inside
// has() or expand(). For expand("<cfile>:p") the span excludes the modifier so
// hover can still describe the base special token.
func completionBuiltinStringValueSpan(file *syntax.File, expression *syntax.Expression, contextKind completionContext) (syntax.Span, bool) {
	content, ok := completionStringContent(file, expression)
	if !ok || content.Start >= content.End {
		return syntax.Span{}, false
	}
	span := content
	if contextKind == completionContextExpandSpecial {
		if colon := strings.IndexByte(file.Source[content.Start:content.End], ':'); colon >= 0 {
			span.End = content.Start + colon
		}
	}
	return span, span.Start < span.End
}

func completionHighlightContextAt(file *syntax.File, command *syntax.Command, offset int) (completionContext, bool) {
	if file == nil || command == nil || command.Highlight == nil || command.Highlight.Group.Start == command.Highlight.Group.End ||
		offset <= command.Highlight.Group.End {
		return completionContextNone, false
	}
	if command.Highlight.Kind == syntax.HighlightClear || command.Highlight.Kind == syntax.HighlightLink {
		return completionContextNone, true
	}
	if offset > command.Span.End {
		for position := command.Span.End; position < offset; position++ {
			if file.Source[position] != ' ' && file.Source[position] != '\t' {
				return completionContextNone, false
			}
		}
	}
	if attribute := completionHighlightAttributeAt(command, offset, false); attribute != nil {
		return completionContextHighlightKey, true
	}
	if attribute := completionHighlightAttributeAt(command, offset, true); attribute != nil {
		if attribute.Quoted {
			return completionContextNone, true
		}
		return completionContextHighlightValue, true
	}
	start := offset
	for start > command.Highlight.Group.End && !isCompletionSpace(file.Source[start-1]) {
		start--
	}
	equal := strings.IndexByte(file.Source[start:offset], '=')
	if equal >= 0 {
		return completionContextHighlightValue, true
	}
	return completionContextHighlightKey, true
}

func completionHighlightAttributeAt(command *syntax.Command, offset int, value bool) *syntax.HighlightAttribute {
	if command == nil || command.Highlight == nil {
		return nil
	}
	for index := range command.Highlight.Attributes {
		attribute := &command.Highlight.Attributes[index]
		if value {
			end := attribute.Value.End
			if attribute.Quoted {
				end++
			}
			if attribute.Equal.Start < attribute.Equal.End && attribute.Equal.End <= offset &&
				(attribute.Value.Start == attribute.Value.End || offset <= end) {
				return attribute
			}
			continue
		}
		end := attribute.Key.End
		if attribute.Equal.Start < attribute.Equal.End {
			end = attribute.Equal.Start
		}
		if attribute.Key.Start <= offset && offset <= end {
			return attribute
		}
	}
	return nil
}

func isCompletionSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\n' || character == '\r'
}

func completionMappingArgumentAt(file *syntax.File, command *syntax.Command, offset int) bool {
	if file == nil || command == nil || command.Mapping == nil || offset < command.Argument.Start || offset > command.Argument.End {
		return false
	}
	if command.Mapping.LHS.Start < command.Mapping.LHS.End && command.Mapping.LHS.Start <= offset && offset <= command.Mapping.LHS.End && !completionMappingArgumentPrefix(file.Text(command.Mapping.LHS)) {
		return false
	}
	position := command.Argument.Start
	for position < offset {
		for position < offset && (file.Source[position] == ' ' || file.Source[position] == '\t') {
			position++
		}
		if position == offset {
			return true
		}
		if file.Source[position] != '<' {
			return false
		}
		close := strings.IndexByte(file.Source[position:offset], '>')
		if close < 0 {
			return true
		}
		name := strings.ToLower(file.Source[position+1 : position+close])
		if name != "buffer" && name != "nowait" && name != "silent" && name != "special" && name != "script" && name != "expr" && name != "unique" {
			return false
		}
		position += close + 1
	}
	return true
}

func completionMappingArgumentPrefix(value string) bool {
	value = strings.ToLower(value)
	for _, argument := range []string{"<buffer>", "<nowait>", "<silent>", "<special>", "<script>", "<expr>", "<unique>"} {
		if strings.HasPrefix(argument, value) {
			return true
		}
	}
	return false
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
	applyMetadata := func(detail, documentation string) {
		if _, set := result.Detail.Get(); !set {
			result.Detail = protocol.NewOptional(detail)
		}
		if result.Documentation == nil && documentation != "" {
			result.Documentation = protocol.String(documentation)
		}
	}
	if function, ok := vimdata.LookupFunction(item.Label); ok {
		applyMetadata(builtinFunctionDetail(function), function.Documentation)
		return &result, nil
	}
	if option, ok := vimdata.LookupOption(item.Label); ok {
		applyMetadata(completionOptionDetail(option), option.Documentation)
		return &result, nil
	}
	if variable, ok := vimdata.LookupVariable(item.Label); ok {
		applyMetadata("variable: "+variable.Type, variable.Documentation)
		return &result, nil
	}
	if command, ok := vimdata.Lookup(item.Label); ok && command.Name == item.Label && !vimdata.IsNeovimCompatCommand(command.Name) {
		applyMetadata("Ex command", command.Documentation)
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
	if err := s.waitForWorkspaceIndex(ctx); err != nil {
		return nil, err
	}
	snapshot, file, fileAnalysis, encoding, err := s.structureDocumentWithAnalysis(ctx, params.TextDocument.URI.String())
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
	if call == nil || len(call.Children) == 0 {
		return nil, s.structureCurrent(ctx, snapshot)
	}
	callable := call.Children[0]
	var label string
	var parameters []protocol.ParameterInformation
	var documentation string
	switch callable.Kind {
	case syntax.ExpressionIdentifier:
		if fileAnalysis == nil {
			fileAnalysis = analysis.Analyze(file)
		}
		declaration := declarationForExpression(fileAnalysis, callable)
		if declaration != nil {
			function := functionForDeclaration(file.Commands, declaration)
			if function != nil {
				label, parameters = formatFunctionSignature(file, declaration.Name, function)
			} else if declaration.Type.Name == "func" && declaration.Type.ArgumentCountKnown {
				label, parameters = formatFunctionValueSignature(declaration.Name, declaration.Type)
			} else {
				return nil, s.structureCurrent(ctx, snapshot)
			}
		} else {
			function, ok := vimdata.LookupFunction(file.Text(callable.Span))
			if !ok {
				return nil, s.structureCurrent(ctx, snapshot)
			}
			label, parameters = formatBuiltinFunctionSignature(function)
			documentation = function.Documentation
		}
	case syntax.ExpressionMember:
		operator := file.Text(callable.Operator)
		if operator == "." {
			if fileAnalysis == nil {
				fileAnalysis = analysis.Analyze(file)
			}
			if function, ok := memberFunctionForSignature(file, fileAnalysis, callable); ok {
				if function == nil {
					label = callable.Value + "()"
				} else {
					label, parameters = formatFunctionSignature(file, callable.Value, function)
				}
				break
			}
			return s.importedFunctionSignatureHelp(ctx, params)
		}
		if operator != "->" {
			return nil, s.structureCurrent(ctx, snapshot)
		}
		function, ok := vimdata.LookupFunction(callable.Value)
		if !ok || function.MethodArgument == 0 {
			return nil, s.structureCurrent(ctx, snapshot)
		}
		label, parameters = formatBuiltinMethodSignature(function)
		documentation = function.Documentation
	default:
		return nil, s.structureCurrent(ctx, snapshot)
	}
	active := activeCallParameter(call, offset)
	if len(parameters) > 0 && active >= uint32(len(parameters)) {
		active = uint32(len(parameters) - 1)
	}
	if err := s.structureCurrent(ctx, snapshot); err != nil {
		return nil, err
	}
	return s.newSignatureHelp(label, parameters, active, documentation), nil
}

func (s *Server) importedFunctionSignatureHelp(ctx context.Context, params *protocol.SignatureHelpParams) (*protocol.SignatureHelp, error) {
	for attempt := range 2 {
		snapshot, file, fileAnalysis, encoding, err := s.structureDocumentWithAnalysis(ctx, params.TextDocument.URI.String())
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
		if call == nil || len(call.Children) == 0 {
			return nil, s.structureCurrent(ctx, snapshot)
		}
		callable := call.Children[0]
		if callable.Kind != syntax.ExpressionMember || file.Text(callable.Operator) != "." {
			return nil, s.structureCurrent(ctx, snapshot)
		}
		path, ok := workspaceURIPath(uri.URI(snapshot.URI()))
		if !ok {
			return nil, s.structureCurrent(ctx, snapshot)
		}
		if fileAnalysis == nil {
			fileAnalysis = analysis.Analyze(file)
		}
		externalFacts := workspace.CollectExternalReferencesFromAnalysis(path, file, fileAnalysis)
		referenceMember := callable
		aggregateMember := false
		aggregateClassReceiver := false
		if len(callable.Children) == 1 && callable.Children[0] != nil && callable.Children[0].Kind == syntax.ExpressionMember && file.Text(callable.Children[0].Operator) == "." {
			referenceMember = callable.Children[0]
			aggregateMember = true
			aggregateClassReceiver = true
		}
		memberSpan := syntax.Span{Start: referenceMember.Operator.End, End: referenceMember.Span.End}
		var external *workspace.ExternalReferenceFact
		for _, fact := range externalFacts {
			if fact.Kind == workspace.ExternalReferenceImportMember && fact.Span == memberSpan && fact.Name == referenceMember.Value {
				fact := fact
				external = &fact
				break
			}
		}
		if external == nil && len(callable.Children) == 1 {
			external = importedAggregateReferenceForReceiver(path, file, fileAnalysis, callable.Children[0], externalFacts)
			aggregateMember = external != nil
		}
		if external == nil {
			return nil, s.structureCurrent(ctx, snapshot)
		}
		document := navigationDocument{server: s, snapshot: snapshot, encoding: encoding, analysis: fileAnalysis, occurrence: external.Span, external: external}
		state := s.captureWorkspaceNavigationState()
		target, resolved := document.workspaceTargetInState(state)
		if !resolved {
			current, err := document.workspaceNavigationCurrent(ctx, state, workspaceNavigationTarget{})
			if err != nil {
				return nil, err
			}
			if current {
				return nil, nil
			}
			if attempt == 1 {
				return nil, protocol.ErrContentModified
			}
			continue
		}
		targetFile := s.fileForWorkspaceTarget(target)
		current, err := document.workspaceNavigationCurrent(ctx, state, target)
		if err != nil {
			return nil, err
		}
		if !current {
			if attempt == 1 {
				return nil, protocol.ErrContentModified
			}
			continue
		}
		if targetFile == nil {
			return nil, nil
		}
		var label string
		var parameters []protocol.ParameterInformation
		if aggregateMember {
			targetSymbols := analysis.CollectSymbols(targetFile)
			container := symbolForWorkspaceTarget(targetSymbols, target.match.Fact)
			if container != nil && container.Kind == analysis.SymbolKindFunction {
				function := functionForWorkspaceTargetFile(targetFile, target)
				if function == nil || function.ReturnType == nil || strings.Contains(function.ReturnType.Name, ".") {
					return nil, nil
				}
				container = completionContainer(targetSymbols, function.ReturnType.Name)
			}
			if container == nil || strings.HasPrefix(callable.Value, "_") {
				return nil, nil
			}
			function, ok := memberFunctionInContainer(targetFile, targetSymbols, container, callable.Value, aggregateClassReceiver)
			if !ok {
				return nil, nil
			}
			if function == nil {
				label = callable.Value + "()"
			} else {
				label, parameters = formatFunctionSignature(targetFile, callable.Value, function)
			}
		} else {
			function := functionForWorkspaceTargetFile(targetFile, target)
			if function == nil {
				return nil, nil
			}
			label, parameters = formatFunctionSignature(targetFile, callable.Value, function)
		}
		active := activeCallParameter(call, offset)
		if len(parameters) > 0 && active >= uint32(len(parameters)) {
			active = uint32(len(parameters) - 1)
		}
		return s.newSignatureHelp(label, parameters, active, ""), nil
	}
	return nil, protocol.ErrContentModified
}

func importedAggregateReferenceForReceiver(path string, file *syntax.File, result *analysis.FileAnalysis, receiver *syntax.Expression, facts []workspace.ExternalReferenceFact) *workspace.ExternalReferenceFact {
	if file != nil && receiver != nil && receiver.Kind == syntax.ExpressionMember && file.Text(receiver.Operator) == "." {
		span := syntax.Span{Start: receiver.Operator.End, End: receiver.Span.End}
		if fact := matchingExternalReference(facts, span, receiver.Value); fact != nil {
			return fact
		}
	}
	if fact := importedConstructorReference(file, receiver, facts); fact != nil {
		return fact
	}
	if fact := importedCallReference(file, receiver, facts); fact != nil {
		return fact
	}
	typeName := result.TypeOf(receiver).Name
	if !strings.Contains(typeName, ".") {
		if alias := typeAliasNode(file.Commands, result, typeName); alias != nil {
			typeName = alias.Name
		}
	}
	if fact := importedQualifiedTypeReference(path, file, typeName, receiver.Span); fact != nil {
		return fact
	}
	declaration := declarationForExpression(result, receiver)
	if declaration == nil {
		return nil
	}
	typeNode, initializer := declarationSyntax(file.Commands, declaration.Span)
	if fact := importedTypeReference(file, typeNode, facts); fact != nil {
		return fact
	}
	if fact := importedConstructorReference(file, initializer, facts); fact != nil {
		return fact
	}
	if fact := importedAssignmentReference(path, file, result, declaration, receiver, facts); fact != nil {
		return fact
	}
	typeName = declaration.Type.Name
	if !strings.Contains(typeName, ".") {
		if alias := typeAliasNode(file.Commands, result, typeName); alias != nil {
			typeName = alias.Name
		}
	}
	return importedQualifiedTypeReference(path, file, typeName, receiver.Span)
}

func importedAssignmentReference(path string, file *syntax.File, result *analysis.FileAnalysis, declaration *analysis.Declaration, receiver *syntax.Expression, facts []workspace.ExternalReferenceFact) *workspace.ExternalReferenceFact {
	if file == nil || result == nil || declaration == nil || receiver == nil {
		return nil
	}
	receiverBlock := -1
	receiverCommand := false
	walkCommands(file.Commands, func(command *syntax.Command) {
		if command.Span.Start <= receiver.Span.Start && receiver.Span.End <= command.Span.End {
			receiverBlock = command.Block
			receiverCommand = true
		}
	})
	if !receiverCommand {
		return nil
	}
	var reference *workspace.ExternalReferenceFact
	walkCommands(file.Commands, func(command *syntax.Command) {
		if command.Block != receiverBlock || command.Span.Start <= declaration.Span.Start || command.Span.End > receiver.Span.Start {
			return
		}
		for _, expression := range command.Expressions {
			if expression == nil || expression.Kind != syntax.ExpressionAssignment || expression.Value != "=" || len(expression.Children) != 2 || declarationForExpression(result, expression.Children[0]) != declaration {
				continue
			}
			reference = nil
			value := expression.Children[1]
			if fact := importedConstructorReference(file, value, facts); fact != nil {
				reference = fact
				continue
			}
			if fact := importedCallReference(file, value, facts); fact != nil {
				reference = fact
				continue
			}
			typeName := result.TypeOf(value).Name
			if !strings.Contains(typeName, ".") {
				if alias := typeAliasNode(file.Commands, result, typeName); alias != nil {
					typeName = alias.Name
				}
			}
			reference = importedQualifiedTypeReference(path, file, typeName, value.Span)
		}
	})
	return reference
}

func importedQualifiedTypeReference(path string, file *syntax.File, name string, span syntax.Span) *workspace.ExternalReferenceFact {
	separator := strings.IndexByte(name, '.')
	if file == nil || separator <= 0 || separator == len(name)-1 {
		return nil
	}
	alias, member := name[:separator], name[separator+1:]
	var importNode *syntax.Import
	for index := range file.Commands {
		candidate := file.Commands[index].Import
		if candidate == nil || candidate.PathSpan.End > span.Start || workspace.ImportAlias(file, candidate) != alias {
			continue
		}
		if importNode != nil {
			return nil
		}
		importNode = candidate
	}
	if importNode == nil {
		return nil
	}
	return &workspace.ExternalReferenceFact{Path: path, Name: member, Span: span, Kind: workspace.ExternalReferenceImportMember, ImportPath: file.Text(importNode.PathSpan), ImportAutoload: importNode.Autoload}
}

func typeAliasNode(commands []syntax.Command, result *analysis.FileAnalysis, name string) *syntax.Type {
	var span syntax.Span
	for _, declaration := range result.Declarations {
		if declaration.Kind != analysis.SymbolKindTypeAlias || declaration.Name != name {
			continue
		}
		if span.Start < span.End {
			return nil
		}
		span = declaration.Span
	}
	if span.Start >= span.End {
		return nil
	}
	var typeNode *syntax.Type
	walkCommands(commands, func(command *syntax.Command) {
		if typeNode == nil && command.TypeAlias != nil && command.TypeAlias.Name == span {
			typeNode = command.TypeAlias.Type
		}
	})
	return typeNode
}

func importedTypeReference(file *syntax.File, typeNode *syntax.Type, facts []workspace.ExternalReferenceFact) *workspace.ExternalReferenceFact {
	if file == nil || typeNode == nil {
		return nil
	}
	separator := strings.IndexByte(typeNode.Name, '.')
	if separator <= 0 || separator == len(typeNode.Name)-1 {
		return nil
	}
	span := syntax.Span{Start: typeNode.Span.Start + separator + 1, End: typeNode.Span.Start + len(typeNode.Name)}
	return matchingExternalReference(facts, span, typeNode.Name[separator+1:])
}

func importedConstructorReference(file *syntax.File, expression *syntax.Expression, facts []workspace.ExternalReferenceFact) *workspace.ExternalReferenceFact {
	for expression != nil && expression.Kind == syntax.ExpressionParenthesized && len(expression.Children) == 1 {
		expression = expression.Children[0]
	}
	if file == nil || expression == nil || expression.Kind != syntax.ExpressionCall || len(expression.Children) == 0 {
		return nil
	}
	callee := expression.Children[0]
	if callee == nil || callee.Kind != syntax.ExpressionMember || callee.Value != "new" || file.Text(callee.Operator) != "." || len(callee.Children) != 1 {
		return nil
	}
	class := callee.Children[0]
	if class == nil || class.Kind != syntax.ExpressionMember || file.Text(class.Operator) != "." {
		return nil
	}
	span := syntax.Span{Start: class.Operator.End, End: class.Span.End}
	return matchingExternalReference(facts, span, class.Value)
}

func importedCallReference(file *syntax.File, expression *syntax.Expression, facts []workspace.ExternalReferenceFact) *workspace.ExternalReferenceFact {
	for expression != nil && expression.Kind == syntax.ExpressionParenthesized && len(expression.Children) == 1 {
		expression = expression.Children[0]
	}
	if file == nil || expression == nil || expression.Kind != syntax.ExpressionCall || len(expression.Children) == 0 {
		return nil
	}
	callee := expression.Children[0]
	if callee == nil || callee.Kind != syntax.ExpressionMember || file.Text(callee.Operator) != "." {
		return nil
	}
	span := syntax.Span{Start: callee.Operator.End, End: callee.Span.End}
	return matchingExternalReference(facts, span, callee.Value)
}

func matchingExternalReference(facts []workspace.ExternalReferenceFact, span syntax.Span, name string) *workspace.ExternalReferenceFact {
	for _, fact := range facts {
		if fact.Kind == workspace.ExternalReferenceImportMember && fact.Span == span && fact.Name == name {
			fact := fact
			return &fact
		}
	}
	return nil
}

func declarationSyntax(commands []syntax.Command, span syntax.Span) (*syntax.Type, *syntax.Expression) {
	var typeNode *syntax.Type
	var initializer *syntax.Expression
	walkCommands(commands, func(command *syntax.Command) {
		if typeNode != nil || initializer != nil {
			return
		}
		if command.Declaration != nil {
			for _, binding := range command.Declaration.Bindings {
				if binding.Name == span {
					typeNode, initializer = binding.ParsedType, command.Declaration.Initializer
					return
				}
			}
		}
		if command.For != nil {
			for _, binding := range command.For.Bindings {
				if binding.Name == span {
					typeNode = binding.ParsedType
					return
				}
			}
		}
		if command.Function != nil {
			for _, parameter := range command.Function.Parameters {
				if parameter.Name == span {
					typeNode = parameter.Type
					return
				}
			}
		}
	})
	return typeNode, initializer
}

func (s *Server) fileForWorkspaceTarget(target workspaceNavigationTarget) *syntax.File {
	var file *syntax.File
	if target.openSnapshot != nil && target.openSnapshot.Text() == target.match.Source {
		file = s.parseSnapshot(target.openSnapshot)
	}
	if file == nil {
		file = syntax.Parse(target.match.Source)
	}
	return file
}

func functionForWorkspaceTargetFile(file *syntax.File, target workspaceNavigationTarget) *syntax.Function {
	if file == nil {
		return nil
	}
	var function *syntax.Function
	walkCommands(file.Commands, func(command *syntax.Command) {
		if function == nil && command.Function != nil && command.Function.Name == target.match.Fact.SelectionRange && file.Text(command.Function.Name) == target.match.Fact.Name {
			function = command.Function
		}
	})
	return function
}

func (s *Server) newSignatureHelp(label string, parameters []protocol.ParameterInformation, active uint32, documentation string) *protocol.SignatureHelp {
	zero := uint32(0)
	signature := protocol.SignatureInformation{Label: label, Parameters: parameters, ActiveParameter: protocol.NewNullable(active)}
	if documentation != "" {
		signature.Documentation = boundedMarkupContent(s.languageFeatures.signatureMarkup, documentation)
	}
	return &protocol.SignatureHelp{
		Signatures:      []protocol.SignatureInformation{signature},
		ActiveSignature: &zero,
		ActiveParameter: protocol.NewNullable(active),
	}
}

func boundedMarkupContent(kind protocol.MarkupKind, value string) *protocol.MarkupContent {
	if kind != protocol.MarkupKindMarkdown {
		kind = protocol.MarkupKindPlainText
	}
	if len(value) > maxLanguageFeatureDocumentationBytes {
		end := maxLanguageFeatureDocumentationBytes - len("…")
		for end > 0 && !utf8.ValidString(value[:end]) {
			end--
		}
		value = value[:end] + "…"
	}
	return &protocol.MarkupContent{Kind: kind, Value: value}
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

func memberFunctionForSignature(file *syntax.File, result *analysis.FileAnalysis, member *syntax.Expression) (*syntax.Function, bool) {
	symbol, command, ok := memberSymbolForStaticReceiver(file, result, member)
	if !ok {
		return nil, false
	}
	if command == nil && symbol.Kind == analysis.SymbolKindClass && member.Value == "new" {
		return nil, true
	}
	if command == nil || command.Function == nil {
		return nil, false
	}
	return command.Function, true
}

func memberSymbolForStaticReceiver(file *syntax.File, result *analysis.FileAnalysis, member *syntax.Expression) (*analysis.Symbol, *syntax.Command, bool) {
	symbols, container, classReceiver, ok := memberContainerForStaticReceiver(file, result, member)
	if !ok {
		return nil, nil, false
	}
	return memberSymbolInContainer(file, symbols, container, member.Value, classReceiver)
}

func memberContainerForStaticReceiver(file *syntax.File, result *analysis.FileAnalysis, member *syntax.Expression) ([]*analysis.Symbol, *analysis.Symbol, bool, bool) {
	if file == nil || result == nil || member == nil || len(member.Children) != 1 || member.Children[0] == nil {
		return nil, nil, false, false
	}
	memberSpan := syntax.Span{Start: member.Operator.End, End: member.Span.End}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Span == memberSpan {
			return nil, nil, false, false
		}
	}
	receiver := member.Children[0]
	className := ""
	classReceiver := false
	symbols := analysis.CollectSymbols(file)
	if declaration := declarationForExpression(result, receiver); declaration != nil {
		switch declaration.Kind {
		case analysis.SymbolKindClass, analysis.SymbolKindInterface, analysis.SymbolKindEnum, analysis.SymbolKindTypeAlias:
			className, classReceiver = declaration.Type.Name, true
		}
	}
	if receiver.Kind == syntax.ExpressionIdentifier && (receiver.Value == "this" || receiver.Value == "super") {
		if current := enclosingAggregateContainer(symbols, member.Span); current != nil {
			className = current.Name
			if receiver.Value == "super" {
				aggregate := commandForAggregateSpan(file.Commands, current.SelectionRange)
				if aggregate == nil || aggregate.Aggregate == nil || len(aggregate.Aggregate.Extends) == 0 {
					return nil, nil, false, false
				}
				className = file.Text(aggregate.Aggregate.Extends[0])
			}
		}
	}
	if className == "" {
		className = result.TypeOf(receiver).Name
	}
	container := completionContainer(symbols, className)
	if container == nil {
		return nil, nil, false, false
	}
	return symbols, container, classReceiver, true
}

func memberFunctionInContainer(file *syntax.File, symbols []*analysis.Symbol, container *analysis.Symbol, name string, classReceiver bool) (*syntax.Function, bool) {
	symbol, command, ok := memberSymbolInContainer(file, symbols, container, name, classReceiver)
	if !ok {
		return nil, false
	}
	if command == nil && symbol.Kind == analysis.SymbolKindClass && name == "new" {
		return nil, true
	}
	if command == nil || command.Function == nil {
		return nil, false
	}
	return command.Function, true
}

func memberSymbolInContainer(file *syntax.File, symbols []*analysis.Symbol, container *analysis.Symbol, name string, classReceiver bool) (*analysis.Symbol, *syntax.Command, bool) {
	receiverContainer := container
	seen := make(map[string]bool)
	for container != nil && !seen[container.Name] {
		seen[container.Name] = true
		for _, child := range container.Children {
			if child.Name != name {
				continue
			}
			command := commandForSymbolSpan(file.Commands, child.SelectionRange)
			switch child.Kind {
			case analysis.SymbolKindMethod, analysis.SymbolKindConstructor:
				if command == nil || classReceiver && child.Kind != analysis.SymbolKindConstructor && !hasCommandModifier(command, "static") || !classReceiver && (child.Kind == analysis.SymbolKindConstructor || hasCommandModifier(command, "static")) {
					return nil, nil, false
				}
			case analysis.SymbolKindVariable, analysis.SymbolKindConstant:
				if command == nil || classReceiver != hasCommandModifier(command, "static") {
					return nil, nil, false
				}
			case analysis.SymbolKindEnumMember:
				if !classReceiver {
					return nil, nil, false
				}
			default:
				continue
			}
			return child, command, true
		}
		if name == "new" {
			break
		}
		aggregate := commandForAggregateSpan(file.Commands, container.SelectionRange)
		if aggregate == nil || aggregate.Aggregate == nil || len(aggregate.Aggregate.Extends) == 0 {
			break
		}
		container = completionContainer(symbols, file.Text(aggregate.Aggregate.Extends[0]))
	}
	if classReceiver && name == "new" && receiverContainer.Kind == analysis.SymbolKindClass {
		aggregate := commandForAggregateSpan(file.Commands, receiverContainer.SelectionRange)
		hasConstructor := false
		for _, child := range receiverContainer.Children {
			if child.Kind == analysis.SymbolKindConstructor && (child.Name == "new" || child.Name == "_new") {
				hasConstructor = true
				break
			}
		}
		if aggregate != nil && !hasConstructor && !hasCommandModifier(aggregate, "abstract") {
			return receiverContainer, nil, true
		}
	}
	return nil, nil, false
}

func symbolForWorkspaceTarget(symbols []*analysis.Symbol, fact workspace.SymbolFact) *analysis.Symbol {
	for _, symbol := range symbols {
		if symbol != nil && symbol.SelectionRange == fact.SelectionRange && symbol.Name == fact.Name && symbol.Kind == fact.Kind {
			return symbol
		}
		if symbol != nil {
			if found := symbolForWorkspaceTarget(symbol.Children, fact); found != nil {
				return found
			}
		}
	}
	return nil
}

func enclosingAggregateContainer(symbols []*analysis.Symbol, span syntax.Span) *analysis.Symbol {
	var result *analysis.Symbol
	var visit func([]*analysis.Symbol)
	visit = func(candidates []*analysis.Symbol) {
		for _, candidate := range candidates {
			if candidate == nil || candidate.Range.Start > span.Start || span.End > candidate.Range.End {
				continue
			}
			if candidate.Kind == analysis.SymbolKindClass || candidate.Kind == analysis.SymbolKindInterface || candidate.Kind == analysis.SymbolKindEnum {
				if result == nil || candidate.Range.End-candidate.Range.Start < result.Range.End-result.Range.Start {
					result = candidate
				}
			}
			visit(candidate.Children)
		}
	}
	visit(symbols)
	return result
}

func commandForSymbolSpan(commands []syntax.Command, span syntax.Span) *syntax.Command {
	var result *syntax.Command
	walkCommands(commands, func(command *syntax.Command) {
		if result != nil {
			return
		}
		if command.Function != nil && command.Function.Name == span {
			result = command
			return
		}
		if command.Declaration != nil {
			for _, binding := range command.Declaration.Bindings {
				if binding.Name == span {
					result = command
					return
				}
			}
		}
	})
	return result
}

func commandForAggregateSpan(commands []syntax.Command, span syntax.Span) *syntax.Command {
	var result *syntax.Command
	walkCommands(commands, func(command *syntax.Command) {
		if result == nil && command.Aggregate != nil && command.Aggregate.Name == span {
			result = command
		}
	})
	return result
}

func hasCommandModifier(command *syntax.Command, name string) bool {
	if command == nil {
		return false
	}
	for _, modifier := range command.Modifiers {
		if modifier.Name == name {
			return true
		}
	}
	return false
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

func formatFunctionValueSignature(name string, typ analysis.ValueType) (string, []protocol.ParameterInformation) {
	parts := make([]string, 0, len(typ.Arguments))
	parameters := make([]protocol.ParameterInformation, 0, len(typ.Arguments))
	for index, argument := range typ.Arguments {
		part := formatValueType(argument)
		if typ.Variadic && index == len(typ.Arguments)-1 {
			part = "..." + part
		} else if index >= typ.RequiredArguments {
			part = "?" + part
		}
		parts = append(parts, part)
		parameters = append(parameters, protocol.ParameterInformation{Label: protocol.String(part)})
	}
	label := name + "(" + strings.Join(parts, ", ") + ")"
	if typ.Return != nil {
		label += ": " + formatValueType(*typ.Return)
	}
	return label, parameters
}

func formatBuiltinFunctionSignature(function vimdata.BuiltinFunction) (string, []protocol.ParameterInformation) {
	label := ""
	for line := range strings.SplitSeq(function.Documentation, "\n") {
		if strings.HasPrefix(line, function.Name+"(") && strings.HasSuffix(line, ")") {
			label = line
			continue
		}
		if label != "" {
			break
		}
	}
	if label == "" {
		count := function.MaxArgs
		if count < 0 {
			count = len(function.ArgumentChecks)
		}
		if count < function.MinArgs {
			count = function.MinArgs
		}
		parts := make([]string, 0, count+1)
		for index := 0; index < count; index++ {
			part := fmt.Sprintf("{arg%d}", index+1)
			if index >= function.MinArgs {
				part = "[" + part + "]"
			}
			parts = append(parts, part)
		}
		label = function.Name + "(" + strings.Join(parts, ", ") + ")"
	}
	if function.MaxArgs < 0 && !strings.Contains(label, "...") {
		label = strings.TrimSuffix(label, ")") + ", ...)"
	}
	parameters := make([]protocol.ParameterInformation, 0, len(function.ArgumentChecks)+1)
	for start := 0; start < len(label); {
		open := strings.IndexByte(label[start:], '{')
		if open < 0 {
			break
		}
		open += start
		close := strings.IndexByte(label[open+1:], '}')
		if close < 0 {
			break
		}
		close += open + 1
		parameters = append(parameters, protocol.ParameterInformation{Label: protocol.String(label[open : close+1])})
		start = close + 1
	}
	if strings.Contains(label, "...") {
		parameters = append(parameters, protocol.ParameterInformation{Label: protocol.String("...")})
	}
	if returnType := function.ReturnType.DisplayName(); returnType != "" {
		label += ": " + returnType
	}
	return label, parameters
}

func formatBuiltinMethodSignature(function vimdata.BuiltinFunction) (string, []protocol.ParameterInformation) {
	_, parameters := formatBuiltinFunctionSignature(function)
	receiver := function.MethodArgument - 1
	if receiver < 0 || receiver >= len(parameters) {
		return "", nil
	}
	parameters = append(parameters[:receiver:receiver], parameters[receiver+1:]...)
	required := function.MinArgs - 1
	if beforeReceiver := function.MethodArgument - 1; required < beforeReceiver {
		required = beforeReceiver
	}
	parts := make([]string, 0, len(parameters))
	for index, parameter := range parameters {
		part, ok := parameter.Label.(protocol.String)
		if !ok {
			continue
		}
		if index >= required && part != "..." {
			part = "[" + part + "]"
		}
		parts = append(parts, string(part))
	}
	label := function.Name + "(" + strings.Join(parts, ", ") + ")"
	if returnType := function.ReturnType.DisplayName(); returnType != "" {
		label += ": " + returnType
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
