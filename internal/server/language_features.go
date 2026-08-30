package server

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/chemzqm/vimls-go/internal/analysis"
	"github.com/chemzqm/vimls-go/internal/syntax"
	"github.com/chemzqm/vimls-go/internal/vimdata"
	"github.com/chemzqm/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const maxCompletionItems = 2000

func (s *Server) DocumentLink(ctx context.Context, params *protocol.DocumentLinkParams) ([]protocol.DocumentLink, error) {
	snapshot, file, encoding, err := s.structureDocument(ctx, params.TextDocument.URI.String())
	if err != nil {
		return nil, err
	}
	if snapshot == nil || file == nil {
		return []protocol.DocumentLink{}, nil
	}
	path, ok := workspaceURIPath(uri.URI(snapshot.URI()))
	resolver, _, _ := s.workspaceNavigationState()
	if !ok || resolver == nil {
		return []protocol.DocumentLink{}, s.structureCurrent(ctx, snapshot)
	}
	links := make([]protocol.DocumentLink, 0)
	walkCommands(file.Commands, func(command *syntax.Command) {
		var span syntax.Span
		var target string
		switch {
		case command.Import != nil:
			resolution := resolver.ResolveImport(path, file, command.Import)
			if resolution.Dynamic || resolution.Path == "" {
				return
			}
			span, target = command.Import.PathSpan, resolution.Path
		case command.Canonical == "source":
			resolution := resolver.ResolveSource(path, file.Text(command.Argument))
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
	if err := s.structureCurrent(ctx, snapshot); err != nil {
		return nil, err
	}
	return links, nil
}

func (s *Server) Completion(ctx context.Context, params *protocol.CompletionParams) (protocol.CompletionResult, error) {
	snapshot, file, encoding, err := s.structureDocument(ctx, params.TextDocument.URI.String())
	if err != nil {
		return nil, err
	}
	if snapshot == nil || file == nil {
		return protocol.CompletionItemSlice{}, nil
	}
	offset, err := snapshot.Offset(fromProtocolPosition(params.Position), encoding)
	if err != nil {
		return protocol.CompletionItemSlice{}, s.structureCurrent(ctx, snapshot)
	}
	fileAnalysis := analysis.Analyze(file)
	if alias, member := importMemberContext(snapshot.Text(), offset); member {
		items := s.importMemberCompletions(snapshot.URI(), file, alias)
		if err := s.structureCurrent(ctx, snapshot); err != nil {
			return nil, err
		}
		return items, nil
	}
	contextKind := completionContextAt(file, offset)
	if contextKind == completionContextNone {
		return protocol.CompletionItemSlice{}, s.structureCurrent(ctx, snapshot)
	}
	items := make(map[string]protocol.CompletionItem)
	add := func(item protocol.CompletionItem) {
		if item.Label == "" || len(items) >= maxCompletionItems {
			return
		}
		if _, exists := items[item.Label]; !exists {
			items[item.Label] = item
		}
	}
	if contextKind == completionContextExpression {
		for _, declaration := range visibleDeclarations(fileAnalysis, offset) {
			item := protocol.CompletionItem{Label: declaration.Name, Kind: completionSymbolKind(declaration.Kind)}
			item.Detail = protocol.NewOptional(string(declaration.Kind))
			if declaration.Type.Name != "" && declaration.Type.Name != analysis.ValueTypeAny {
				item.Detail = protocol.NewOptional(string(declaration.Kind) + ": " + formatValueType(declaration.Type))
			}
			add(item)
		}
		for _, function := range vimdata.BuiltinFunctions() {
			add(protocol.CompletionItem{Label: function.Name, Kind: protocol.CompletionItemKindFunction})
		}
	} else {
		for _, command := range vimdata.Commands() {
			add(protocol.CompletionItem{Label: command.Name, Kind: protocol.CompletionItemKindKeyword})
		}
	}
	result := make(protocol.CompletionItemSlice, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Label < result[j].Label })
	if err := s.structureCurrent(ctx, snapshot); err != nil {
		return nil, err
	}
	return result, nil
}

func importMemberContext(source string, offset int) (string, bool) {
	if offset < 0 || offset > len(source) {
		return "", false
	}
	start := offset
	for start > 0 && isCompletionIdentifierByte(source[start-1]) {
		start--
	}
	if start == 0 || source[start-1] != '.' {
		return "", false
	}
	end := start - 1
	start = end
	for start > 0 && isCompletionIdentifierByte(source[start-1]) {
		start--
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
	path, ok := workspaceURIPath(uri.URI(documentURI))
	resolver, index, _ := s.workspaceNavigationState()
	if !ok || resolver == nil || index == nil {
		return protocol.CompletionItemSlice{}
	}
	var targetPath string
	walkCommands(file.Commands, func(command *syntax.Command) {
		if targetPath != "" || command.Import == nil || file.Text(command.Import.Alias) != alias {
			return
		}
		resolution := resolver.ResolveImport(path, file, command.Import)
		if !resolution.Dynamic {
			targetPath = resolution.Path
		}
	})
	if targetPath == "" {
		return protocol.CompletionItemSlice{}
	}
	var facts []workspace.SymbolFact
	targetURI := uri.File(targetPath).String()
	s.publishMu.Lock()
	snapshot, open := s.documents.Snapshot(targetURI)
	parsed := s.parsed[targetURI]
	s.publishMu.Unlock()
	if open {
		targetFile := parsed.file
		if targetFile == nil || parsed.revision != snapshot.Revision() {
			targetFile = syntax.Parse(snapshot.Text())
		}
		facts = workspace.CollectSymbolFacts(targetPath, targetFile)
	} else {
		matches := index.FileSymbols(targetPath)
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
		items = append(items, protocol.CompletionItem{Label: fact.Name, Kind: completionSymbolKind(fact.Kind), Detail: protocol.NewOptional("exported " + string(fact.Kind))})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Label < items[j].Label })
	return items
}

type completionContext uint8

const (
	completionContextNone completionContext = iota
	completionContextCommand
	completionContextExpression
)

func completionContextAt(file *syntax.File, offset int) completionContext {
	for _, token := range file.Tokens {
		if token.Kind == syntax.TokenComment && token.Span.Start <= offset && offset <= token.Span.End {
			return completionContextNone
		}
	}
	insideString := false
	walkCommands(file.Commands, func(command *syntax.Command) {
		walkCommandExpressions(command, func(expression *syntax.Expression) {
			if (expression.Kind == syntax.ExpressionString || expression.Kind == syntax.ExpressionInterpolatedString) && expression.Span.Start < offset && offset < expression.Span.End {
				insideString = true
			}
		})
	})
	if insideString {
		return completionContextNone
	}
	lineStart := strings.LastIndexByte(file.Source[:offset], '\n') + 1
	if strings.TrimSpace(file.Source[lineStart:offset]) == "" {
		return completionContextCommand
	}
	contextKind := completionContextExpression
	walkCommands(file.Commands, func(command *syntax.Command) {
		if command.Span.Start <= offset && offset <= command.Name.End {
			contextKind = completionContextCommand
		}
	})
	return contextKind
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
		result.Detail = protocol.NewOptional(builtinFunctionDetail(function))
		return &result, nil
	}
	if command, ok := vimdata.Lookup(item.Label); ok && command.Name == item.Label {
		result.Detail = protocol.NewOptional("Ex command")
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

func visibleDeclarations(result *analysis.FileAnalysis, offset int) []*analysis.Declaration {
	if result == nil {
		return nil
	}
	var scope *analysis.Scope
	for _, candidate := range result.Scopes {
		if candidate.Span.Start <= offset && offset <= candidate.Span.End && (scope == nil || candidate.Span.End-candidate.Span.Start < scope.Span.End-scope.Span.Start) {
			scope = candidate
		}
	}
	if scope == nil {
		scope = result.Root
	}
	declarations := make([]*analysis.Declaration, 0)
	for current := scope; current != nil; current = current.Parent {
		for index := len(current.Declarations) - 1; index >= 0; index-- {
			declaration := current.Declarations[index]
			if declaration.Span.Start > offset && declaration.Kind != analysis.SymbolKindFunction && declaration.Kind != analysis.SymbolKindMethod && declaration.Kind != analysis.SymbolKindConstructor {
				continue
			}
			declarations = append(declarations, declaration)
		}
	}
	return declarations
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
