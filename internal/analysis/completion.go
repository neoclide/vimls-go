package analysis

import (
	"slices"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// CollectCompletionFacts collects current lexical declarations and explicit
// type facts without reference indexing, inference or diagnostic passes. It does not call
// Analyze, and its result is not a substitute for a complete FileAnalysis.
// The caller owns the result; it shares only the immutable syntax tree with
// concurrent full analysis.
func CollectCompletionFacts(file *syntax.File) *FileAnalysis {
	result := newFileAnalysis(file, false)
	if file == nil {
		return result
	}
	collectCommandScopes(result, result.Root, file.Commands, file.Blocks, nil)
	collectLambdaScopesCommands(result, result.Root, file.Commands)
	collectEmbeddedDeclarations(result, result.Root, file.Commands)
	collectLambdaDeclarations(result, file.Commands)
	collectOpaqueEnumDeclarations(result, file.Commands, file.Blocks)
	sortDeclarations(result)
	state := newTypeState(result)
	state.collectFacts()
	// Some declaration collectors also flag malformed declaration sites.
	// Those diagnostics belong exclusively to the full analysis result.
	result.Diagnostics = nil
	return result
}

// CompletionTypes answers only the type queries needed by one completion
// request. Its memoization is request-owned; neither declarations nor inferred
// types in the shared lexical/full analysis cache are modified.
type CompletionTypes struct {
	state   *typeState
	sources map[syntax.Span]completionTypeSource
	types   map[*Declaration]ValueType
	active  map[*Declaration]bool
}

type completionTypeSource struct {
	command   *syntax.Command
	commands  []syntax.Command
	index     int
	binding   int
	parameter *syntax.Parameter
}

func NewCompletionTypes(facts *FileAnalysis) *CompletionTypes {
	query := &CompletionTypes{
		sources: make(map[syntax.Span]completionTypeSource),
		types:   make(map[*Declaration]ValueType),
		active:  make(map[*Declaration]bool),
	}
	if facts == nil || facts.File == nil {
		return query
	}
	view := *facts
	view.expressionTypes = make(map[*syntax.Expression]ValueType)
	query.state = &typeState{
		result: &view, completion: query, commandScopes: facts.commandScopes,
		references: make(map[syntax.Span]*Reference),
	}
	for _, reference := range facts.References {
		query.state.references[reference.Span] = reference
	}
	query.state.collectUserCommandBodies(facts.File.Commands)
	query.collectSources(facts.File.Commands)
	for _, scope := range facts.Scopes {
		if scope.Lambda != nil && scope.Lambda.LambdaBody != nil {
			query.collectSources(scope.Lambda.LambdaBody.Commands)
		}
	}
	return query
}

func (query *CompletionTypes) collectSources(commands []syntax.Command) {
	for index := range commands {
		command := &commands[index]
		source := completionTypeSource{command: command, commands: commands, index: index}
		if command.Function != nil {
			query.sources[command.Function.Name] = source
			for i := range command.Function.Parameters {
				parameter := &command.Function.Parameters[i]
				query.sources[parameterDeclarationSpan(query.state.result.File, *parameter)] = completionTypeSource{parameter: parameter}
			}
		}
		if command.Declaration != nil {
			for i, binding := range command.Declaration.Bindings {
				source.binding = i
				query.sources[binding.Name] = source
			}
		}
		if command.For != nil {
			for i, binding := range command.For.Bindings {
				source.binding = i
				query.sources[binding.Name] = source
			}
		}
		if command.Embedded != nil {
			query.collectSources(command.Embedded.Commands)
		}
	}
}

func (query *CompletionTypes) DeclarationType(declaration *Declaration) ValueType {
	if declaration == nil || query.state == nil {
		return UnknownValueType
	}
	if typ, ok := query.types[declaration]; ok {
		return typ
	}
	if query.active[declaration] || len(query.active) >= 64 {
		return UnknownValueType
	}
	typ := declaration.Type
	if !isUnresolvedType(typ) && (typ.Name != "func" || !functionSymbolKind(declaration.Kind)) {
		return typ
	}
	query.active[declaration] = true
	defer delete(query.active, declaration)
	source, found := query.sources[declaration.Span]
	if !found {
		return typ
	}
	state := query.state
	command := source.command
	switch {
	case source.parameter != nil:
		typ = state.infer(source.parameter.Default, declaration.Scope)
	case command.Function != nil:
		// Copy mutable slices/pointers before filling inferred defaults/returns.
		typ.Arguments = slices.Clone(typ.Arguments)
		for i, parameter := range command.Function.Parameters {
			if i < len(typ.Arguments) && isUnresolvedType(typ.Arguments[i]) && parameter.Default != nil {
				typ.Arguments[i] = state.infer(parameter.Default, state.commandScopes[command])
			}
		}
		if typ.Return != nil && isUnresolvedType(*typ.Return) &&
			(command.Function.ReturnType == nil || command.Function.ReturnType.Kind == syntax.TypeMissing && !syntaxDiagnosticOverlaps(state.result.File.Diagnostics, command.Function.ReturnType.Span)) {
			inferred := state.inferFunctionReturn(source.commands, source.index)
			typ.Return = &inferred
		}
	case command.Declaration != nil:
		initializer := command.Declaration.Initializer
		if target := command.Declaration.Target; target != nil && target.Kind == syntax.ExpressionList {
			binding := command.Declaration.Bindings[source.binding]
			typ = state.destructuredBindingType(initializer, source.binding, binding.Rest, declaration.Scope)
		} else {
			typ = state.infer(initializer, declaration.Scope)
		}
	case command.For != nil:
		if forLoopDestructures(state.result.File, command) {
			binding := command.For.Bindings[source.binding]
			typ = state.forDestructuredBindingType(command.For.Iterable, source.binding, binding.Rest, declaration.Scope)
		} else {
			typ = indexedType(state.infer(command.For.Iterable, declaration.Scope))
		}
	}
	query.types[declaration] = typ
	return typ
}

func (query *CompletionTypes) TypeOf(expression *syntax.Expression, offset int) ValueType {
	if query.state == nil {
		return UnknownValueType
	}
	scope := query.state.result.Root
	for _, candidate := range query.state.result.Scopes {
		if candidate.Span.Start <= offset && offset <= candidate.Span.End && candidate.Span.End-candidate.Span.Start < scope.Span.End-scope.Span.Start {
			scope = candidate
		}
	}
	return query.state.infer(expression, scope)
}

func (state *typeState) typeOfDeclaration(declaration *Declaration) ValueType {
	if state.completion != nil {
		return state.completion.DeclarationType(declaration)
	}
	return declaration.Type
}

// Full analysis already records function-callee resolution in References.
// Completion resolves only the callees consumed by type inference, including
// forward declarations, without constructing a file-wide reference index.
func (state *typeState) collectCompletionCallee(expression *syntax.Expression, scope *Scope) {
	if expression == nil {
		return
	}
	if expression.Kind == syntax.ExpressionGenericReference || expression.Kind == syntax.ExpressionParenthesized {
		if len(expression.Children) == 1 {
			state.collectCompletionCallee(expression.Children[0], scope)
		}
		return
	}
	if expression.Kind == syntax.ExpressionIdentifier && state.references[expression.Span] == nil {
		state.references[expression.Span] = &Reference{
			Name: expression.Value, Span: expression.Span,
			Declaration: resolve(scope, expression.Value, expression.Span.Start, true, nil),
		}
	}
}
