package analysis

import (
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
)

// ValueType is the small, protocol-independent type fact used by analysis.
// The zero value is the analyzer's unknown state; Name "any" is reserved for
// Vim9's explicit source type.  The concrete null and none names share the
// special category through valueTypeCategory. Return is set for function values.
type ValueType struct {
	Name               string
	Arguments          []ValueType
	Return             *ValueType
	ArgumentCountKnown bool
	RequiredArguments  int
	Variadic           bool
}

const (
	ValueTypeAny     = "any"
	ValueTypeNull    = "null"
	ValueTypeNone    = "none"
	ValueTypeSpecial = "special"
)

// UnknownValueType is returned when an expression cannot be inferred safely.
var UnknownValueType = ValueType{}

// TypeOf returns the best conservative type fact for expression.  The zero
// value and nil expressions are treated as unknown for convenient callers.
func (analysis *FileAnalysis) TypeOf(expression *syntax.Expression) ValueType {
	if analysis == nil || expression == nil {
		return UnknownValueType
	}
	if typ, ok := analysis.expressionTypes[expression]; ok {
		return typ
	}
	return UnknownValueType
}

type typeState struct {
	result        *FileAnalysis
	declarations  map[syntax.Span]*Declaration
	explicitTypes map[syntax.Span]bool
	references    map[syntax.Span]*Reference
	commandScopes map[*syntax.Command]*Scope
	commandBodies []syntax.Span
}

func inferTypes(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	result.expressionTypes = make(map[*syntax.Expression]ValueType)
	state := &typeState{
		result:        result,
		declarations:  make(map[syntax.Span]*Declaration),
		explicitTypes: make(map[syntax.Span]bool),
		references:    make(map[syntax.Span]*Reference),
		commandScopes: result.commandScopes,
	}
	state.collectUserCommandBodies(result.File.Commands)
	for _, scope := range result.Scopes {
		for _, declaration := range scope.Declarations {
			state.declarations[declaration.Span] = declaration
			declaration.Type = UnknownValueType
		}
	}
	for _, reference := range result.References {
		state.references[reference.Span] = reference
	}
	state.collectFacts()

	// A small fixed number of source-order passes lets a function return fact
	// propagate through a forward call without creating a recursive solver.
	for pass := range 2 {
		if pass > 0 {
			result.expressionTypes = make(map[*syntax.Expression]ValueType)
		}
		state.walkCommands()
		state.inferFunctionReturns()
	}
}

func (state *typeState) collectFacts() {
	file := state.result.File
	state.collectFactsCommands(file.Commands)
}

func (state *typeState) collectFactsCommands(commands []syntax.Command) {
	file := state.result.File
	for index := range commands {
		command := &commands[index]
		if command.Aggregate != nil {
			if declaration := state.declarations[command.Aggregate.Name]; declaration != nil {
				declaration.Type = ValueType{Name: file.Text(command.Aggregate.Name)}
			}
		}
		if command.TypeAlias != nil {
			if declaration := state.declarations[command.TypeAlias.Name]; declaration != nil {
				declaration.Type = convertSyntaxType(command.TypeAlias.Type)
			}
		}
		if command.Function != nil {
			declaration := state.declarations[command.Function.Name]
			if declaration != nil {
				arguments := make([]ValueType, 0, len(command.Function.Parameters))
				for _, parameter := range command.Function.Parameters {
					typ := convertSyntaxType(parameter.Type)
					if isUnresolvedType(typ) {
						typ = UnknownValueType
					}
					arguments = append(arguments, typ)
					if parameterDeclaration := state.declarations[parameterDeclarationSpan(file, parameter)]; parameterDeclaration != nil {
						parameterDeclaration.Type = typ
					}
				}
				returnType := convertSyntaxType(command.Function.ReturnType)
				declaration.Type = ValueType{Name: "func", Arguments: arguments, Return: new(returnType), ArgumentCountKnown: true, RequiredArguments: requiredParameterCount(command.Function.Parameters), Variadic: parametersAreVariadic(command.Function.Parameters)}
			}
		}
		state.collectLambdaFactsCommands(command.Expressions)
		state.collectLambdaFactsCommands(command.Targets)
		if command.Declaration != nil {
			state.collectLambdaFactsExpression(command.Declaration.Initializer)
			for _, binding := range command.Declaration.Bindings {
				declaration := state.declarations[binding.Name]
				if declaration == nil {
					continue
				}
				typ := convertSyntaxType(binding.ParsedType)
				if binding.ParsedType != nil || len(command.Declaration.Bindings) == 1 && command.Declaration.ParsedType != nil {
					state.explicitTypes[binding.Name] = true
				}
				if !isUnresolvedType(typ) {
					declaration.Type = typ
				} else if command.Heredoc != nil && len(command.Declaration.Bindings) == 1 &&
					binding.ParsedType == nil && command.Declaration.ParsedType == nil {
					declaration.Type = ValueType{Name: "list", Arguments: []ValueType{{Name: "string"}}}
				}
			}
		}
		if command.For != nil {
			state.collectLambdaFactsExpression(command.For.Iterable)
			for _, binding := range command.For.Bindings {
				if declaration := state.declarations[binding.Name]; declaration != nil {
					if typ := convertSyntaxType(binding.ParsedType); !isUnresolvedType(typ) {
						declaration.Type = typ
					}
				}
			}
		}
		if command.Import != nil {
			state.collectLambdaFactsExpression(command.Import.Path)
		}
		for _, value := range command.EnumValues {
			state.collectLambdaFactsExpression(value.Initializer)
			state.collectLambdaFactsCommands(value.Arguments)
			if declaration := state.declarations[value.Name]; declaration != nil {
				declaration.Type = ValueType{Name: "enum"}
			}
		}
		if command.Embedded != nil {
			state.collectFactsCommands(command.Embedded.Commands)
		}
	}
}

func (state *typeState) collectLambdaFactsCommands(commands []*syntax.Expression) {
	for _, expression := range commands {
		state.collectLambdaFactsExpression(expression)
	}
}

func (state *typeState) collectLambdaFactsExpression(expression *syntax.Expression) {
	if expression == nil {
		return
	}
	if expression.Kind == syntax.ExpressionLambda {
		for _, parameter := range expression.Parameters {
			if declaration := state.declarations[parameter.Name]; declaration != nil {
				declaration.Type = convertSyntaxType(parameter.Type)
			}
		}
		if expression.LambdaBody != nil {
			state.collectFactsCommands(expression.LambdaBody.Commands)
		}
	}
	for index, child := range expression.Children {
		if expression.Kind == syntax.ExpressionLambda && index < len(expression.Parameters) {
			continue
		}
		state.collectLambdaFactsExpression(child)
	}
}

func (state *typeState) walkCommands() {
	file := state.result.File
	state.walkCommandList(file.Commands)
}

func (state *typeState) collectUserCommandBodies(commands []syntax.Command) {
	for index := range commands {
		command := &commands[index]
		if command.Canonical == "command" && command.Embedded != nil {
			state.commandBodies = append(state.commandBodies, command.Embedded.Span)
		}
		if command.Embedded != nil {
			state.collectUserCommandBodies(command.Embedded.Commands)
		}
	}
}

func (state *typeState) walkCommandList(commands []syntax.Command) {
	file := state.result.File
	for index := range commands {
		command := &commands[index]
		scope := state.commandScopes[command]
		if scope == nil {
			scope = state.result.Root
		}
		if command.Declaration != nil && command.Declaration.Initializer != nil {
			for bindingIndex, binding := range command.Declaration.Bindings {
				declaration := state.declarations[binding.Name]
				explicitType := binding.ParsedType != nil || len(command.Declaration.Bindings) == 1 && command.Declaration.ParsedType != nil
				if declaration == nil || explicitType || !isUnresolvedType(declaration.Type) {
					continue
				}
				typ := UnknownValueType
				if command.Declaration.Target != nil && command.Declaration.Target.Kind == syntax.ExpressionList {
					typ = state.destructuredBindingType(command.Declaration.Initializer, bindingIndex, binding.Rest, declaration.Scope)
				} else {
					typ = state.infer(command.Declaration.Initializer, declaration.Scope)
				}
				declaration.Type = typ
			}
		}
		if command.For != nil {
			iterable := state.infer(command.For.Iterable, scope)
			destructuring := forLoopDestructures(file, command)
			for bindingIndex, binding := range command.For.Bindings {
				if declaration := state.declarations[binding.Name]; declaration != nil && isUnresolvedType(declaration.Type) {
					if destructuring {
						declaration.Type = state.forDestructuredBindingType(command.For.Iterable, bindingIndex, binding.Rest, scope)
					} else {
						declaration.Type = indexedType(iterable)
					}
				}
			}
		}
		if command.Function != nil {
			functionDeclaration := state.declarations[command.Function.Name]
			for parameterIndex, parameter := range command.Function.Parameters {
				if parameter.Default != nil {
					defaultType := state.infer(parameter.Default, scope)
					if functionDeclaration != nil && parameterIndex < len(functionDeclaration.Type.Arguments) && isUnresolvedType(functionDeclaration.Type.Arguments[parameterIndex]) && !isUnresolvedType(defaultType) {
						functionDeclaration.Type.Arguments[parameterIndex] = defaultType
					}
					if parameterDeclaration := state.declarations[parameterDeclarationSpan(file, parameter)]; parameterDeclaration != nil && isUnresolvedType(parameterDeclaration.Type) && !isUnresolvedType(defaultType) {
						parameterDeclaration.Type = defaultType
					}
				}
			}
		}
		for _, value := range command.EnumValues {
			state.infer(value.Initializer, scope)
		}
		for _, expression := range command.Expressions {
			state.infer(expression, scope)
		}
		for _, target := range command.Targets {
			state.infer(target, scope)
		}
		if command.Embedded != nil {
			state.walkCommandList(command.Embedded.Commands)
		}
		state.walkLambdaExpressions(command.Expressions)
		state.walkLambdaExpressions(command.Targets)
		if command.Declaration != nil {
			state.walkLambdaExpression(command.Declaration.Initializer)
		}
		if command.For != nil {
			state.walkLambdaExpression(command.For.Iterable)
		}
		if command.Import != nil {
			state.walkLambdaExpression(command.Import.Path)
		}
		for _, value := range command.EnumValues {
			state.walkLambdaExpression(value.Initializer)
			state.walkLambdaExpressions(value.Arguments)
		}
	}
}

func (state *typeState) walkLambdaExpressions(expressions []*syntax.Expression) {
	for _, expression := range expressions {
		state.walkLambdaExpression(expression)
	}
}

func (state *typeState) walkLambdaExpression(expression *syntax.Expression) {
	if expression == nil {
		return
	}
	if expression.Kind == syntax.ExpressionLambda && expression.LambdaBody != nil {
		state.walkCommandList(expression.LambdaBody.Commands)
	}
	for index, child := range expression.Children {
		if expression.Kind == syntax.ExpressionLambda && index < len(expression.Parameters) {
			continue
		}
		state.walkLambdaExpression(child)
	}
}

func (state *typeState) inferFunctionReturns() {
	file := state.result.File
	state.inferFunctionReturnsCommands(file.Commands)
}

func (state *typeState) inferFunctionReturnsCommands(commands []syntax.Command) {
	for index := range commands {
		command := &commands[index]
		if command.Function != nil && (command.Function.ReturnType == nil || command.Function.ReturnType.Kind == syntax.TypeMissing) {
			declaration := state.declarations[command.Function.Name]
			if declaration != nil && declaration.Type.Return != nil && isUnresolvedType(*declaration.Type.Return) &&
				(command.Function.ReturnType == nil || !syntaxDiagnosticOverlaps(state.result.File.Diagnostics, command.Function.ReturnType.Span)) {
				inferred := UnknownValueType
				hasValueReturn := false
				functionScope := state.commandScopes[command]
				for bodyIndex := index + 1; bodyIndex < len(commands); bodyIndex++ {
					body := &commands[bodyIndex]
					if functionScope == nil || body.Span.Start >= functionScope.Span.End {
						break
					}
					owner := state.commandScopes[body]
					for owner != nil && owner.Kind != syntax.BlockFunction && owner.Kind != syntax.BlockDef && owner.Lambda == nil {
						owner = owner.Parent
					}
					if owner != functionScope {
						continue
					}
					if body.Canonical == "return" && (len(body.Expressions) > 0 || command.Canonical == "function") {
						hasValueReturn = true
						current := ValueType{Name: "number"} // Legacy bare return yields zero.
						if len(body.Expressions) > 0 {
							current = state.infer(body.Expressions[0], state.commandScopes[body])
						}
						if isUnresolvedType(inferred) {
							inferred = current
						} else {
							inferred = mergeTypes(inferred, current)
						}
					}
				}
				if !hasValueReturn {
					inferred = ValueType{Name: "void"}
					if command.Canonical == "function" {
						inferred = ValueType{Name: "number"}
					}
				}
				*declaration.Type.Return = inferred
			}
		}
		if command.Embedded != nil {
			state.inferFunctionReturnsCommands(command.Embedded.Commands)
		}
	}
}

func (state *typeState) infer(expression *syntax.Expression, scope *Scope) ValueType {
	if expression == nil {
		return UnknownValueType
	}
	if state.isUserCommandReplacement(expression) {
		state.result.expressionTypes[expression] = UnknownValueType
		return UnknownValueType
	}
	if typ, ok := state.result.expressionTypes[expression]; ok && !isUnresolvedType(typ) {
		return typ
	}
	unknown := UnknownValueType
	var typ ValueType
	switch expression.Kind {
	case syntax.ExpressionIdentifier:
		if guarded, ok := state.guardedIdentifierType(expression, scope); ok {
			typ = guarded
			break
		}
		switch strings.ToLower(expression.Value) {
		case "true", "false":
			typ = ValueType{Name: "bool"}
		case "null":
			typ = ValueType{Name: ValueTypeNull}
		case "null_blob":
			typ = ValueType{Name: "blob"}
		case "null_channel":
			typ = ValueType{Name: "channel"}
		case "null_class":
			typ = ValueType{Name: "class"}
		case "null_dict":
			typ = ValueType{Name: "dict", Arguments: []ValueType{UnknownValueType}}
		case "null_function", "null_partial":
			typ = ValueType{Name: "func", Return: new(UnknownValueType)}
		case "null_job":
			typ = ValueType{Name: "job"}
		case "null_list":
			typ = ValueType{Name: "list", Arguments: []ValueType{UnknownValueType}}
		case "null_object":
			typ = ValueType{Name: "object"}
		case "null_string":
			typ = ValueType{Name: "string"}
		case "null_tuple":
			typ = ValueType{Name: "tuple"}
		default:
			if strings.HasPrefix(expression.Value, "&") {
				if option, ok := vimdata.LookupOption(expression.Value); ok {
					typ = builtinOptionValueType(option)
				} else if vimdata.IsTerminalOptionName(expression.Value) {
					typ = ValueType{Name: "string"}
				} else {
					typ = unknown
				}
			} else if strings.HasPrefix(expression.Value, "$") || strings.HasPrefix(expression.Value, "@") {
				typ = ValueType{Name: "string"}
			} else if variable, ok := vimdata.LookupVariable(expression.Value); ok {
				typ = builtinVariableValueType(variable)
			} else if implicit, ok := legacyImplicitArgumentType(scope, expression.Value); ok {
				typ = implicit
			} else if reference := state.references[expression.Span]; reference != nil {
				typ = unknown
				if reference.Declaration != nil {
					typ = reference.Declaration.Type
				}
			} else if declaration := resolve(scope, expression.Value, expression.Span.Start, false, nil); declaration != nil {
				typ = declaration.Type
			} else {
				typ = unknown
			}
		}
	case syntax.ExpressionNumber:
		if isFloatLiteral(expression.Value) {
			typ = ValueType{Name: "float"}
		} else {
			typ = ValueType{Name: "number"}
		}
	case syntax.ExpressionString:
		typ = ValueType{Name: "string"}
	case syntax.ExpressionInterpolatedString:
		for _, child := range expression.Children {
			state.infer(child, scope)
		}
		typ = ValueType{Name: "string"}
	case syntax.ExpressionBlob:
		typ = ValueType{Name: "blob"}
	case syntax.ExpressionList:
		typ = ValueType{Name: "list", Arguments: []ValueType{state.commonElementType(expression.Children, scope)}}
	case syntax.ExpressionDictionary:
		values := make([]*syntax.Expression, 0, len(expression.Children)/2)
		for index, child := range expression.Children {
			if index%2 == 0 && child != nil && child.Kind != syntax.ExpressionIdentifier {
				state.infer(child, scope)
			} else if index%2 == 1 {
				values = append(values, child)
			}
		}
		typ = ValueType{Name: "dict", Arguments: []ValueType{state.commonElementType(values, scope)}}
	case syntax.ExpressionMember:
		if len(expression.Children) > 0 {
			typ = indexedType(state.infer(expression.Children[0], scope))
		} else {
			typ = unknown
		}
	case syntax.ExpressionTuple:
		typ = ValueType{Name: "tuple", Arguments: state.childTypes(expression.Children, scope)}
	case syntax.ExpressionParenthesized:
		if len(expression.Children) == 1 {
			typ = state.infer(expression.Children[0], scope)
		} else {
			typ = unknown
		}
	case syntax.ExpressionUnary:
		if expression.Value == "!" {
			typ = ValueType{Name: "bool"}
		} else if len(expression.Children) > 0 {
			typ = state.infer(expression.Children[0], scope)
		} else {
			typ = unknown
		}
	case syntax.ExpressionBinary:
		typ = state.binaryType(expression, scope)
	case syntax.ExpressionTernary:
		if len(expression.Children) == 3 {
			state.infer(expression.Children[0], scope)
			left := state.infer(expression.Children[1], scope)
			right := state.infer(expression.Children[2], scope)
			typ = mergeTypes(left, right)
		} else {
			typ = unknown
		}
	case syntax.ExpressionAssignment:
		if len(expression.Children) > 1 {
			typ = state.infer(expression.Children[1], scope)
			target := expression.Children[0]
			for _, child := range target.Children {
				state.infer(child, scope)
			}
			state.assign(target, typ)
		} else {
			typ = unknown
		}
	case syntax.ExpressionCall:
		if len(expression.Children) > 0 {
			callee := state.infer(expression.Children[0], scope)
			for _, argument := range expression.Children[1:] {
				state.infer(argument, scope)
			}
			if className, ok := state.constructorObjectClass(expression, scope); ok {
				typ = ValueType{Name: className}
			} else if builtin, arguments, ok := builtinCallArguments(state.result.File, expression); ok {
				argumentTypes := make([]ValueType, 0, len(arguments))
				for _, argument := range arguments {
					argumentTypes = append(argumentTypes, state.infer(argument, scope))
				}
				typ = builtinReturnValueType(builtin, argumentTypes)
			} else if callee.Name == "func" && callee.Return != nil {
				typ = *callee.Return
			} else {
				typ = unknown
			}
		} else {
			typ = unknown
		}
	case syntax.ExpressionIndex, syntax.ExpressionSlice:
		if len(expression.Children) > 0 {
			base := state.infer(expression.Children[0], scope)
			if expression.Kind == syntax.ExpressionIndex {
				typ = indexedType(base)
			} else {
				switch base.Name {
				case "list", "string", "blob":
					typ = base
				case "tuple":
					// Bounds may be dynamic; do not retain the original tuple's
					// element positions or cardinality for an arbitrary slice.
					typ = ValueType{Name: "tuple"}
				default:
					typ = unknown
				}
			}
			for _, index := range expression.Children[1:] {
				state.infer(index, scope)
			}
		} else {
			typ = unknown
		}
	case syntax.ExpressionCast:
		if len(expression.Children) > 0 {
			state.infer(expression.Children[0], scope)
		}
		typ = convertSyntaxType(expression.CastType)
	case syntax.ExpressionLambda:
		typ = state.lambdaType(expression, scope)
	case syntax.ExpressionGenericReference:
		if len(expression.Children) > 0 {
			typ = state.infer(expression.Children[0], scope)
		} else {
			typ = unknown
		}
	default:
		typ = unknown
	}
	state.result.expressionTypes[expression] = typ
	return typ
}

func (state *typeState) isUserCommandReplacement(expression *syntax.Expression) bool {
	if state == nil || state.result == nil || state.result.File == nil || expression == nil {
		return false
	}
	for _, body := range state.commandBodies {
		if expression.Span.Start >= body.Start && expression.Span.Start < body.End {
			return syntax.IsUserCommandReplacementAt(state.result.File.Source, expression.Span.Start, body.End)
		}
	}
	return false
}

// guardedIdentifierType returns the type established by a surrounding
// type(name) == type-code condition.  Vim plugins commonly use this form to
// migrate a global option from an older representation before using it in the
// guarded branch.  Keep this deliberately local to if/elseif bodies: it is not
// a general control-flow solver.
func (state *typeState) guardedIdentifierType(expression *syntax.Expression, scope *Scope) (ValueType, bool) {
	if state == nil || state.result == nil || state.result.File == nil || expression == nil || expression.Kind != syntax.ExpressionIdentifier {
		return UnknownValueType, false
	}
	for current := scope; current != nil; current = current.Parent {
		if current.Kind != syntax.BlockIf || current.Block < 0 {
			continue
		}
		commands, blocks := state.scopeCommandList(current)
		if current.Block >= len(blocks) {
			continue
		}
		block := blocks[current.Block]
		if block.Header < 0 || block.Header >= len(commands) {
			continue
		}
		headers := make([]int, 0, len(block.Branches)+1)
		headers = append(headers, block.Header)
		headers = append(headers, block.Branches...)
		branchHeader := -1
		branchEnd := block.End
		for index, header := range headers {
			if header < 0 || header >= len(commands) {
				continue
			}
			end := block.End
			if index+1 < len(headers) {
				end = headers[index+1]
			}
			if expression.Span.Start >= commands[header].Span.End && (end < 0 || end >= len(commands) || expression.Span.Start < commands[end].Span.Start) {
				branchHeader = header
				branchEnd = end
				break
			}
		}
		if branchHeader < 0 {
			continue
		}
		guard := &commands[branchHeader]
		if guard.Canonical != "if" && guard.Canonical != "elseif" {
			continue
		}
		if state.identifierAssignedInBranch(expression.Value, expression.Span.Start, current, commands, branchHeader+1, branchEnd) {
			return UnknownValueType, false
		}
		for _, condition := range guard.Expressions {
			if typ, ok := typeGuardForIdentifier(condition, expression.Value); ok {
				return typ, true
			}
		}
	}
	return UnknownValueType, false
}

func (state *typeState) scopeCommandList(scope *Scope) ([]syntax.Command, []syntax.Block) {
	if scope.CommandList != nil {
		return scope.CommandList.Commands, scope.CommandList.Blocks
	}
	return state.result.File.Commands, state.result.File.Blocks
}

func (state *typeState) identifierAssignedInBranch(name string, offset int, scope *Scope, commands []syntax.Command, start, end int) bool {
	if end < 0 || end > len(commands) {
		end = len(commands)
	}
	for index := start; index < end; index++ {
		command := &commands[index]
		if command.Span.End > offset {
			break
		}
		if state.commandScopes[command] != scope {
			continue
		}
		if command.Declaration != nil {
			for _, binding := range command.Declaration.Bindings {
				if state.result.File.Text(binding.Name) == name {
					return true
				}
			}
		}
		for _, candidate := range command.Expressions {
			if expressionAssignsIdentifier(candidate, name) {
				return true
			}
		}
	}
	return false
}

func expressionAssignsIdentifier(expression *syntax.Expression, name string) bool {
	if expression == nil {
		return false
	}
	if expression.Kind == syntax.ExpressionAssignment && len(expression.Children) > 0 {
		target := expression.Children[0]
		return target != nil && target.Kind == syntax.ExpressionIdentifier && target.Value == name
	}
	for _, child := range expression.Children {
		if expressionAssignsIdentifier(child, name) {
			return true
		}
	}
	return false
}

func typeGuardForIdentifier(condition *syntax.Expression, name string) (ValueType, bool) {
	condition = unwrapParenthesizedExpression(condition)
	if condition == nil {
		return UnknownValueType, false
	}
	if condition.Kind == syntax.ExpressionBinary && condition.Value == "&&" && len(condition.Children) == 2 {
		if typ, ok := typeGuardForIdentifier(condition.Children[0], name); ok {
			return typ, true
		}
		return typeGuardForIdentifier(condition.Children[1], name)
	}
	if condition.Kind != syntax.ExpressionBinary || len(condition.Children) != 2 ||
		(condition.Value != "==" && condition.Value != "==#" && condition.Value != "==?") {
		return UnknownValueType, false
	}
	if typeCallTargetsIdentifier(condition.Children[0], name) {
		typ, _ := staticTypeCode(condition.Children[1])
		return typ, true
	}
	if typeCallTargetsIdentifier(condition.Children[1], name) {
		typ, _ := staticTypeCode(condition.Children[0])
		return typ, true
	}
	return UnknownValueType, false
}

func typeCallTargetsIdentifier(expression *syntax.Expression, name string) bool {
	expression = unwrapParenthesizedExpression(expression)
	if expression == nil || expression.Kind != syntax.ExpressionCall || len(expression.Children) != 2 {
		return false
	}
	callee := expression.Children[0]
	target := unwrapParenthesizedExpression(expression.Children[1])
	return callee != nil && callee.Kind == syntax.ExpressionIdentifier && callee.Value == "type" &&
		target != nil && target.Kind == syntax.ExpressionIdentifier && target.Value == name
}

func staticTypeCode(expression *syntax.Expression) (ValueType, bool) {
	expression = unwrapParenthesizedExpression(expression)
	if expression == nil {
		return UnknownValueType, false
	}
	if value, ok := staticNumberValue(expression); ok {
		switch value {
		case 0:
			return ValueType{Name: "number"}, true
		case 1:
			return ValueType{Name: "string"}, true
		case 2:
			return ValueType{Name: "func", Return: new(UnknownValueType)}, true
		case 3:
			return ValueType{Name: "list", Arguments: []ValueType{UnknownValueType}}, true
		case 4:
			return ValueType{Name: "dict", Arguments: []ValueType{UnknownValueType}}, true
		case 5:
			return ValueType{Name: "float"}, true
		case 6:
			return ValueType{Name: "bool"}, true
		case 7:
			return ValueType{Name: ValueTypeSpecial}, true
		case 10:
			return ValueType{Name: "blob"}, true
		default:
			return UnknownValueType, false
		}
	}
	if expression.Kind == syntax.ExpressionIdentifier {
		switch strings.ToLower(expression.Value) {
		case "v:t_number":
			return ValueType{Name: "number"}, true
		case "v:t_string":
			return ValueType{Name: "string"}, true
		case "v:t_func":
			return ValueType{Name: "func", Return: new(UnknownValueType)}, true
		case "v:t_list":
			return ValueType{Name: "list", Arguments: []ValueType{UnknownValueType}}, true
		case "v:t_dict":
			return ValueType{Name: "dict", Arguments: []ValueType{UnknownValueType}}, true
		case "v:t_float":
			return ValueType{Name: "float"}, true
		case "v:t_bool":
			return ValueType{Name: "bool"}, true
		case "v:t_none":
			return ValueType{Name: ValueTypeSpecial}, true
		case "v:t_blob":
			return ValueType{Name: "blob"}, true
		}
	}
	if expression.Kind == syntax.ExpressionCall && len(expression.Children) == 2 {
		callee := expression.Children[0]
		if callee != nil && callee.Kind == syntax.ExpressionIdentifier && callee.Value == "type" {
			return staticSampleType(expression.Children[1])
		}
	}
	return UnknownValueType, false
}

func staticSampleType(expression *syntax.Expression) (ValueType, bool) {
	expression = unwrapParenthesizedExpression(expression)
	if expression == nil {
		return UnknownValueType, false
	}
	switch expression.Kind {
	case syntax.ExpressionNumber:
		if isFloatLiteral(expression.Value) {
			return ValueType{Name: "float"}, true
		}
		return ValueType{Name: "number"}, true
	case syntax.ExpressionString:
		return ValueType{Name: "string"}, true
	case syntax.ExpressionBlob:
		return ValueType{Name: "blob"}, true
	case syntax.ExpressionList:
		return ValueType{Name: "list", Arguments: []ValueType{UnknownValueType}}, true
	case syntax.ExpressionDictionary:
		return ValueType{Name: "dict", Arguments: []ValueType{UnknownValueType}}, true
	case syntax.ExpressionLambda:
		return ValueType{Name: "func", Return: new(UnknownValueType)}, true
	case syntax.ExpressionIdentifier:
		switch strings.ToLower(expression.Value) {
		case "true", "false", "v:true", "v:false":
			return ValueType{Name: "bool"}, true
		case "v:none", "v:null":
			return ValueType{Name: ValueTypeSpecial}, true
		}
	case syntax.ExpressionCall:
		if len(expression.Children) > 0 && expression.Children[0] != nil && expression.Children[0].Kind == syntax.ExpressionIdentifier {
			switch expression.Children[0].Value {
			case "function", "funcref":
				return ValueType{Name: "func", Return: new(UnknownValueType)}, true
			}
		}
	}
	return UnknownValueType, false
}

func unwrapParenthesizedExpression(expression *syntax.Expression) *syntax.Expression {
	for expression != nil && expression.Kind == syntax.ExpressionParenthesized && len(expression.Children) == 1 {
		expression = expression.Children[0]
	}
	return expression
}

func (state *typeState) constructorObjectClass(expression *syntax.Expression, scope *Scope) (string, bool) {
	if state == nil || state.result == nil || state.result.File == nil || expression == nil || expression.Kind != syntax.ExpressionCall || len(expression.Children) == 0 {
		return "", false
	}
	callee := expression.Children[0]
	if callee == nil || callee.Kind != syntax.ExpressionMember || callee.Value != "new" || state.result.File.Text(callee.Operator) != "." || len(callee.Children) != 1 {
		return "", false
	}
	receiver := callee.Children[0]
	if receiver == nil || receiver.Kind != syntax.ExpressionIdentifier {
		return "", false
	}
	declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil)
	if declaration == nil || declaration.Kind != SymbolKindClass {
		return "", false
	}
	return receiver.Value, true
}

func builtinVariableValueType(variable vimdata.Variable) ValueType {
	switch variable.Type {
	case "number", "string", "bool":
		return ValueType{Name: variable.Type}
	case "list<string>":
		return ValueType{Name: "list", Arguments: []ValueType{{Name: "string"}}}
	case "dict<string>":
		return ValueType{Name: "dict", Arguments: []ValueType{{Name: "string"}}}
	case "dict<any>":
		return ValueType{Name: "dict", Arguments: []ValueType{UnknownValueType}}
	case "list<dict<any>>":
		return ValueType{Name: "list", Arguments: []ValueType{{Name: "dict", Arguments: []ValueType{UnknownValueType}}}}
	case "special":
		switch variable.Name {
		case "v:null":
			return ValueType{Name: ValueTypeNull}
		case "v:none":
			return ValueType{Name: ValueTypeNone}
		}
	}
	return UnknownValueType
}

func builtinOptionValueType(option vimdata.Option) ValueType {
	switch option.Type {
	case vimdata.OptionBool:
		return ValueType{Name: "bool"}
	case vimdata.OptionNumber:
		return ValueType{Name: "number"}
	case vimdata.OptionString:
		return ValueType{Name: "string"}
	default:
		return UnknownValueType
	}
}

func builtinReturnValueType(function vimdata.BuiltinFunction, arguments []ValueType) ValueType {
	if function.Name == "get" && len(arguments) == 3 {
		if isSpecialType(arguments[2]) {
			return indexedType(arguments[0])
		}
		return arguments[2]
	}
	switch function.ReturnHelper {
	case "ret_first_arg", "ret_extend", "ret_slice":
		if len(arguments) > 0 {
			return arguments[0]
		}
	case "ret_copy", "ret_first_cont":
		if len(arguments) > 0 {
			first := arguments[0]
			if first.Name == "list" || first.Name == "dict" {
				first.Arguments = []ValueType{UnknownValueType}
			}
			return first
		}
	case "ret_remove":
		if len(arguments) > 0 {
			return indexedType(arguments[0])
		}
	case "ret_repeat":
		if len(arguments) > 0 {
			if arguments[0].Name == "number" || arguments[0].Name == "string" {
				return ValueType{Name: "string"}
			}
			return arguments[0]
		}
	case "ret_list_number":
		return ValueType{Name: "list", Arguments: []ValueType{{Name: "number"}}}
	case "ret_list_string":
		return ValueType{Name: "list", Arguments: []ValueType{{Name: "string"}}}
	case "ret_list_dict_any":
		return ValueType{Name: "list", Arguments: []ValueType{{Name: "dict", Arguments: []ValueType{UnknownValueType}}}}
	case "ret_list_any":
		return ValueType{Name: "list", Arguments: []ValueType{UnknownValueType}}
	case "ret_dict_number":
		return ValueType{Name: "dict", Arguments: []ValueType{{Name: "number"}}}
	case "ret_dict_string":
		return ValueType{Name: "dict", Arguments: []ValueType{{Name: "string"}}}
	case "ret_dict_any":
		return ValueType{Name: "dict", Arguments: []ValueType{UnknownValueType}}
	}
	switch function.ReturnType {
	case vimdata.ReturnVoid:
		return ValueType{Name: "void"}
	case vimdata.ReturnBool:
		return ValueType{Name: "bool"}
	case vimdata.ReturnNumber:
		return ValueType{Name: "number"}
	case vimdata.ReturnFloat:
		return ValueType{Name: "float"}
	case vimdata.ReturnString:
		return ValueType{Name: "string"}
	case vimdata.ReturnBlob:
		return ValueType{Name: "blob"}
	case vimdata.ReturnList:
		return ValueType{Name: "list", Arguments: []ValueType{UnknownValueType}}
	case vimdata.ReturnDict:
		return ValueType{Name: "dict", Arguments: []ValueType{UnknownValueType}}
	case vimdata.ReturnChannel:
		return ValueType{Name: "channel"}
	case vimdata.ReturnJob:
		return ValueType{Name: "job"}
	case vimdata.ReturnTuple:
		return ValueType{Name: "tuple"}
	case vimdata.ReturnFunction:
		return ValueType{Name: "func", Return: new(UnknownValueType)}
	default:
		return UnknownValueType
	}
}

func legacyImplicitArgumentType(scope *Scope, name string) (ValueType, bool) {
	if !strings.HasPrefix(name, "a:") {
		return UnknownValueType, false
	}
	for current := scope; current != nil; current = current.Parent {
		if current.Kind == syntax.BlockDef {
			return UnknownValueType, false
		}
		if current.Kind != syntax.BlockFunction {
			continue
		}
		switch name {
		case "a:000":
			return ValueType{Name: "list", Arguments: []ValueType{UnknownValueType}}, true
		case "a:0", "a:firstline", "a:lastline":
			return ValueType{Name: "number"}, true
		default:
			return UnknownValueType, false
		}
	}
	return UnknownValueType, false
}

func (state *typeState) binaryType(expression *syntax.Expression, scope *Scope) ValueType {
	if len(expression.Children) < 2 {
		return UnknownValueType
	}
	left := state.infer(expression.Children[0], scope)
	right := state.infer(expression.Children[1], scope)
	operator := strings.ToLower(expression.Value)
	comparison := strings.TrimSuffix(strings.TrimSuffix(operator, "#"), "?")
	switch comparison {
	case "==", "!=", ">", ">=", "<", "<=", "=~", "!~", "is", "isnot":
		return ValueType{Name: "bool"}
	}
	if operator == "&&" || operator == "||" {
		return ValueType{Name: "bool"}
	}
	if operator == "." || operator == ".." {
		return ValueType{Name: "string"}
	}
	if operator == "+" && left.Name == right.Name {
		switch left.Name {
		case "list":
			return mergeTypes(left, right)
		case "tuple":
			return ValueType{Name: "tuple", Arguments: append(append([]ValueType(nil), left.Arguments...), right.Arguments...)}
		case "blob":
			return ValueType{Name: "blob"}
		}
	}
	if operator == "+" || operator == "-" || operator == "*" || operator == "/" || operator == "%" || operator == "**" || operator == "<<" || operator == ">>" {
		if left.Name == "float" || right.Name == "float" {
			return ValueType{Name: "float"}
		}
		if left.Name == "number" && right.Name == "number" {
			return ValueType{Name: "number"}
		}
	}
	return UnknownValueType
}

func (state *typeState) lambdaType(expression *syntax.Expression, scope *Scope) ValueType {
	arguments := make([]ValueType, 0, len(expression.Parameters))
	for _, parameter := range expression.Parameters {
		typ := convertSyntaxType(parameter.Type)
		if isUnresolvedType(typ) {
			typ = UnknownValueType
		}
		arguments = append(arguments, typ)
	}
	lambdaScope := scope
	if candidate := state.result.lambdaScopes[expression]; candidate != nil {
		lambdaScope = candidate
	}
	returnType := UnknownValueType
	if expression.LambdaBody != nil {
		// Infer declarations in source order before looking at returns.  This is
		// important for block lambdas whose return expression names a local
		// declared earlier in the body.
		state.walkCommandList(expression.LambdaBody.Commands)
		state.inferFunctionReturnsCommands(expression.LambdaBody.Commands)
		returnType = state.lambdaBodyReturnType(expression.LambdaBody.Commands, lambdaScope)
	} else if len(expression.Children) > len(expression.Parameters) {
		returnType = state.infer(expression.Children[len(expression.Parameters)], lambdaScope)
	}
	explicitType := convertSyntaxType(expression.ReturnType)
	if !isUnresolvedType(explicitType) {
		if isUnresolvedType(returnType) || compatibleTypes(explicitType, returnType) {
			returnType = explicitType
		} else {
			returnType = UnknownValueType
		}
	}
	return ValueType{Name: "func", Arguments: arguments, Return: new(returnType), ArgumentCountKnown: true, RequiredArguments: requiredParameterCount(expression.Parameters), Variadic: parametersAreVariadic(expression.Parameters)}
}

func parametersAreVariadic(parameters []syntax.Parameter) bool {
	return len(parameters) > 0 && parameters[len(parameters)-1].Variadic
}

func requiredParameterCount(parameters []syntax.Parameter) int {
	for index, parameter := range parameters {
		if parameter.Default != nil || parameter.Variadic {
			return index
		}
	}
	return len(parameters)
}

func (state *typeState) lambdaBodyReturnType(commands []syntax.Command, scope *Scope) ValueType {
	result := UnknownValueType
	for index := range commands {
		command := &commands[index]
		commandScope := state.commandScopes[command]
		if commandScope == nil {
			commandScope = scope
		}
		if command.Canonical == "return" {
			if len(command.Expressions) == 0 {
				continue
			}
			current := state.infer(command.Expressions[0], commandScope)
			if isUnresolvedType(result) {
				result = current
			} else {
				result = mergeTypes(result, current)
			}
		}
	}
	return result
}

func compatibleTypes(expected, actual ValueType) bool {
	if isUnknownType(expected) || isUnknownType(actual) {
		return true
	}
	if (expected.Name == "float" && actual.Name == "number") || (expected.Name == "bool" && actual.Name == "number") {
		return true
	}
	if isSpecialType(expected) && isSpecialType(actual) {
		return true
	}
	if expected.Name != actual.Name {
		return false
	}
	if expected.Return != nil && actual.Return != nil && !compatibleTypes(*expected.Return, *actual.Return) {
		return false
	}
	if len(expected.Arguments) == 0 || len(actual.Arguments) == 0 {
		return true
	}
	if len(expected.Arguments) != len(actual.Arguments) {
		return false
	}
	for index := range expected.Arguments {
		if !compatibleTypes(expected.Arguments[index], actual.Arguments[index]) {
			return false
		}
	}
	return true
}

func (state *typeState) assign(expression *syntax.Expression, typ ValueType) {
	if expression == nil || isUnresolvedType(typ) {
		return
	}
	if declaration := state.declarations[expression.Span]; declaration != nil && !state.explicitTypes[expression.Span] && isUnresolvedType(declaration.Type) {
		declaration.Type = typ
	}
}

func (state *typeState) childTypes(children []*syntax.Expression, scope *Scope) []ValueType {
	result := make([]ValueType, 0, len(children))
	for _, child := range children {
		result = append(result, state.infer(child, scope))
	}
	return result
}

func (state *typeState) commonElementType(children []*syntax.Expression, scope *Scope) ValueType {
	if len(children) == 0 {
		return UnknownValueType
	}
	common := state.infer(children[0], scope)
	for _, child := range children[1:] {
		common = mergeTypes(common, state.infer(child, scope))
	}
	return common
}

func convertSyntaxType(typeNode *syntax.Type) ValueType {
	if typeNode == nil || typeNode.Kind == syntax.TypeMissing || typeNode.Name == "" {
		return UnknownValueType
	}
	if typeNode.Kind == syntax.TypeOptional || typeNode.Kind == syntax.TypeVariadic {
		if len(typeNode.Arguments) == 0 {
			return UnknownValueType
		}
		return convertSyntaxType(typeNode.Arguments[0])
	}
	typ := ValueType{Name: typeNode.Name, ArgumentCountKnown: typeNode.Kind == syntax.TypeFunction && typeNode.ArgumentCountKnown}
	for _, argument := range typeNode.Arguments {
		typ.Arguments = append(typ.Arguments, convertSyntaxType(argument))
	}
	if typ.ArgumentCountKnown {
		typ.RequiredArguments = requiredTypeArgumentCount(typeNode.Arguments)
	}
	if (typeNode.Kind == syntax.TypeFunction || typeNode.Name == "tuple") && len(typeNode.Arguments) > 0 {
		typ.Variadic = typeNode.Arguments[len(typeNode.Arguments)-1].Kind == syntax.TypeVariadic
	}
	if typeNode.ReturnType != nil {
		typ.Return = new(convertSyntaxType(typeNode.ReturnType))
	} else if typeNode.Kind == syntax.TypeFunction && typeNode.ArgumentCountKnown {
		typ.Return = new(ValueType{Name: "void"})
	}
	return typ
}

func requiredTypeArgumentCount(arguments []*syntax.Type) int {
	for index, argument := range arguments {
		if argument.Kind == syntax.TypeOptional || argument.Kind == syntax.TypeVariadic {
			return index
		}
	}
	return len(arguments)
}

func initializerElement(initializer *syntax.Expression, index, count int) *syntax.Expression {
	if initializer == nil {
		return nil
	}
	if count > 1 && (initializer.Kind == syntax.ExpressionTuple || initializer.Kind == syntax.ExpressionList) && index < len(initializer.Children) {
		return initializer.Children[index]
	}
	return initializer
}

func (state *typeState) destructuredBindingType(initializer *syntax.Expression, index int, rest bool, scope *Scope) ValueType {
	if initializer == nil {
		return UnknownValueType
	}
	if initializer.Kind == syntax.ExpressionList || initializer.Kind == syntax.ExpressionTuple {
		if rest {
			if initializer.Kind == syntax.ExpressionList {
				return state.infer(initializer, scope)
			}
			if index > len(initializer.Children) {
				return UnknownValueType
			}
			return ValueType{Name: "tuple", Arguments: state.childTypes(initializer.Children[index:], scope)}
		}
		if index >= len(initializer.Children) {
			return UnknownValueType
		}
		return state.infer(initializer.Children[index], scope)
	}

	return destructuredValueType(state.infer(initializer, scope), index, rest)
}

func (state *typeState) forDestructuredBindingType(iterable *syntax.Expression, index int, rest bool, scope *Scope) ValueType {
	if iterable == nil {
		return UnknownValueType
	}
	if (iterable.Kind == syntax.ExpressionList || iterable.Kind == syntax.ExpressionTuple) && len(iterable.Children) > 0 {
		var merged ValueType
		for childIndex, child := range iterable.Children {
			current := state.destructuredBindingType(child, index, rest, scope)
			if childIndex == 0 {
				merged = current
			} else {
				merged = mergeTypes(merged, current)
			}
		}
		return merged
	}
	return destructuredValueType(indexedType(state.infer(iterable, scope)), index, rest)
}

func destructuredValueType(typ ValueType, index int, rest bool) ValueType {
	if typ.Name == ValueTypeAny {
		return typ
	}
	if typ.Name == "list" {
		if rest {
			return typ
		}
		return indexedType(typ)
	}
	if typ.Name != "tuple" {
		return UnknownValueType
	}

	fixed := len(typ.Arguments)
	if typ.Variadic && fixed > 0 {
		fixed--
	}
	if rest {
		if index > fixed && !typ.Variadic {
			return UnknownValueType
		}
		arguments := []ValueType(nil)
		if index < fixed {
			arguments = append(arguments, typ.Arguments[index:]...)
		} else if typ.Variadic && len(typ.Arguments) > 0 {
			arguments = append(arguments, typ.Arguments[len(typ.Arguments)-1])
		}
		return ValueType{Name: "tuple", Arguments: arguments, Variadic: typ.Variadic}
	}
	if index < fixed {
		return typ.Arguments[index]
	}
	if typ.Variadic && len(typ.Arguments) > 0 {
		return indexedType(typ.Arguments[len(typ.Arguments)-1])
	}
	return UnknownValueType
}

func forLoopDestructures(file *syntax.File, command *syntax.Command) bool {
	if file == nil || command == nil || command.For == nil {
		return false
	}
	start := command.Argument.Start
	end := command.For.In.Start
	for start < end && (file.Source[start] == ' ' || file.Source[start] == '\t') {
		start++
	}
	return start < end && file.Source[start] == '['
}

func indexedType(typ ValueType) ValueType {
	if len(typ.Arguments) > 0 && (typ.Name == "list" || typ.Name == "dict") {
		return typ.Arguments[0]
	}
	if typ.Name == "tuple" && len(typ.Arguments) > 0 {
		common := typ.Arguments[0]
		for _, argument := range typ.Arguments[1:] {
			common = mergeTypes(common, argument)
		}
		return common
	}
	if typ.Name == "string" {
		return ValueType{Name: "string"}
	}
	return UnknownValueType
}

func mergeTypes(left, right ValueType) ValueType {
	if isUnresolvedType(left) || isUnresolvedType(right) {
		return UnknownValueType
	}
	if left.Name == ValueTypeAny || right.Name == ValueTypeAny {
		return ValueType{Name: ValueTypeAny}
	}
	if isSpecialType(left) && isSpecialType(right) {
		return ValueType{Name: ValueTypeSpecial}
	}
	if left.Name != right.Name || len(left.Arguments) != len(right.Arguments) {
		return ValueType{Name: ValueTypeAny}
	}
	argumentCountKnown := left.ArgumentCountKnown && right.ArgumentCountKnown && left.RequiredArguments == right.RequiredArguments && left.Variadic == right.Variadic
	result := ValueType{Name: left.Name, Return: left.Return, Arguments: append([]ValueType(nil), left.Arguments...), ArgumentCountKnown: argumentCountKnown}
	if argumentCountKnown {
		result.RequiredArguments = left.RequiredArguments
		result.Variadic = left.Variadic
	}
	for index := range result.Arguments {
		result.Arguments[index] = mergeTypes(result.Arguments[index], right.Arguments[index])
	}
	return result
}

func isFloatLiteral(value string) bool {
	normalized := strings.ReplaceAll(value, "'", "")
	lower := strings.ToLower(normalized)
	if strings.HasPrefix(lower, "0x") || strings.HasPrefix(lower, "0b") || strings.HasPrefix(lower, "0o") {
		return false
	}
	return strings.ContainsAny(normalized, ".eE")
}

func isUnknownType(typ ValueType) bool {
	return isUnresolvedType(typ) || typ.Name == ValueTypeAny
}

func isSpecialType(typ ValueType) bool {
	return valueTypeCategory(typ) == ValueTypeSpecial
}

func valueTypeCategory(typ ValueType) string {
	switch typ.Name {
	case ValueTypeNull, ValueTypeNone, ValueTypeSpecial:
		return ValueTypeSpecial
	default:
		return typ.Name
	}
}

func isUnresolvedType(typ ValueType) bool {
	return typ.Name == ""
}
