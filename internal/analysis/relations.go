package analysis

import (
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
)

// TypeRelationKind identifies a direct nominal aggregate relationship.
type TypeRelationKind uint8

const (
	TypeRelationExtends TypeRelationKind = iota + 1
	TypeRelationImplements
)

// TypeRelation is a direct extends or implements clause in one source file.
// ParentSpan retains the complete source spelling, including an import alias.
type TypeRelation struct {
	ChildName  string
	ChildKind  SymbolKind
	ChildSpan  syntax.Span
	ParentName string
	ParentSpan syntax.Span
	Kind       TypeRelationKind
}

// CallRelation is a statically named call owned by a named callable. Target
// resolution remains a server concern because imports and runtimepath are
// workspace state.
type CallRelation struct {
	CallerName string
	CallerKind SymbolKind
	CallerSpan syntax.Span
	CalleeName string
	CalleeSpan syntax.Span
}

// CollectTypeRelations returns direct nominal relationships in source order.
func CollectTypeRelations(file *syntax.File) []TypeRelation {
	if file == nil {
		return nil
	}
	result := make([]TypeRelation, 0)
	var collect func([]syntax.Command)
	collect = func(commands []syntax.Command) {
		for index := range commands {
			command := &commands[index]
			if aggregate := command.Aggregate; aggregate != nil {
				childKind := aggregateSymbolKind(aggregate.Kind)
				if childKind != "" && validNameSpan(file, aggregate.Name) {
					appendParent := func(span syntax.Span, kind TypeRelationKind) {
						name := relationFinalName(file.Text(span))
						if name == "" || !validNameSpan(file, span) {
							return
						}
						result = append(result, TypeRelation{
							ChildName: file.Text(aggregate.Name), ChildKind: childKind, ChildSpan: aggregate.Name,
							ParentName: name, ParentSpan: span, Kind: kind,
						})
					}
					for _, parent := range aggregate.Extends {
						appendParent(parent, TypeRelationExtends)
					}
					for _, parent := range aggregate.Implements {
						appendParent(parent, TypeRelationImplements)
					}
				}
			}
			if command.Embedded != nil {
				collect(command.Embedded.Commands)
			}
		}
	}
	collect(file.Commands)
	return result
}

// CollectCallRelations returns named calls owned by named functions, methods,
// or explicit constructors. Calls in deferred editor callbacks and lambdas do
// not inherit an outer caller.
func CollectCallRelations(result *FileAnalysis) []CallRelation {
	if result == nil || result.File == nil {
		return nil
	}
	callables := callableScopes(result)
	relations := make([]CallRelation, 0)
	var visitExpression func(*syntax.Expression, *Scope)
	visitExpression = func(expression *syntax.Expression, scope *Scope) {
		if expression == nil || scope == nil || expression.Kind == syntax.ExpressionLambda {
			return
		}
		if expression.Kind == syntax.ExpressionCall {
			if caller := enclosingCallable(scope, callables); caller != nil {
				name, span, identifier := callRelationTarget(expression)
				if name != "" && validNameSpan(result.File, span) {
					if _, builtin := vimdata.LookupFunction(name); !identifier || !builtin {
						relations = append(relations, CallRelation{
							CallerName: caller.Name, CallerKind: caller.Kind, CallerSpan: caller.Span,
							CalleeName: name, CalleeSpan: span,
						})
					}
				}
			}
		}
		for _, child := range expression.Children {
			visitExpression(child, scope)
		}
	}
	var visitCommands func([]syntax.Command, *Scope)
	visitCommands = func(commands []syntax.Command, inherited *Scope) {
		for index := range commands {
			command := &commands[index]
			scope := result.commandScopes[command]
			if scope == nil {
				scope = inherited
			}
			deferred := command.Mapping != nil || command.Autocmd != nil || command.UserCommand != nil || command.Canonical == "autocmd" || command.Canonical == "command"
			if !deferred {
				for _, expression := range command.Expressions {
					visitExpression(expression, scope)
				}
				for _, expression := range command.Targets {
					visitExpression(expression, scope)
				}
				if command.Declaration != nil {
					visitExpression(command.Declaration.Initializer, scope)
				}
				if command.For != nil {
					visitExpression(command.For.Iterable, scope)
				}
				for _, value := range command.EnumValues {
					visitExpression(value.Initializer, scope)
				}
				if command.Embedded != nil {
					visitCommands(command.Embedded.Commands, scope)
				}
			}
		}
	}
	visitCommands(result.File.Commands, result.Root)
	return relations
}

func relationFinalName(name string) string {
	name = strings.TrimSpace(name)
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		name = name[dot+1:]
	}
	return name
}

func callableScopes(result *FileAnalysis) map[*Scope]*Declaration {
	callables := make(map[*Scope]*Declaration)
	bySpan := make(map[syntax.Span]*Declaration)
	for _, declaration := range result.Declarations {
		if declaration != nil && functionSymbolKind(declaration.Kind) {
			bySpan[declaration.Span] = declaration
		}
	}
	var collect func([]syntax.Command)
	collect = func(commands []syntax.Command) {
		for index := range commands {
			command := &commands[index]
			if command.Function != nil {
				if declaration := bySpan[command.Function.Name]; declaration != nil {
					if scope := result.commandScopes[command]; scope != nil {
						callables[scope] = declaration
					}
				}
			}
			if command.Embedded != nil {
				collect(command.Embedded.Commands)
			}
		}
	}
	collect(result.File.Commands)
	return callables
}

func enclosingCallable(scope *Scope, callables map[*Scope]*Declaration) *Declaration {
	for current := scope; current != nil; current = current.Parent {
		if current.Lambda != nil {
			return nil
		}
		if callable := callables[current]; callable != nil {
			return callable
		}
	}
	return nil
}

func callRelationTarget(call *syntax.Expression) (string, syntax.Span, bool) {
	if call == nil || len(call.Children) == 0 {
		return "", syntax.Span{}, false
	}
	callee := call.Children[0]
	for callee != nil && callee.Kind == syntax.ExpressionGenericReference && len(callee.Children) > 0 {
		callee = callee.Children[0]
	}
	if callee == nil {
		return "", syntax.Span{}, false
	}
	switch callee.Kind {
	case syntax.ExpressionIdentifier:
		return callee.Value, callee.Span, true
	case syntax.ExpressionMember:
		if callee.Value == "" || callee.Operator.End >= callee.Span.End {
			return "", syntax.Span{}, false
		}
		return callee.Value, syntax.Span{Start: callee.Operator.End, End: callee.Span.End}, false
	default:
		return "", syntax.Span{}, false
	}
}
