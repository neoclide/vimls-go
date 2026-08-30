package analysis

import (
	"strings"

	"github.com/chemzqm/vimls-go/internal/syntax"
	"github.com/chemzqm/vimls-go/internal/vimdata"
)

// ValueType is the small, protocol-independent type fact used by analysis.
// any is deliberate: Vim expressions are dynamic and an unknown fact must
// never be turned into a guessed type.  Return is set for function values.
type ValueType struct {
	Name               string
	Arguments          []ValueType
	Return             *ValueType
	ArgumentCountKnown bool
	Variadic           bool
}

const ValueTypeAny = "any"

// UnknownValueType is returned when an expression cannot be inferred safely.
var UnknownValueType = ValueType{Name: ValueTypeAny}

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
	references    map[syntax.Span]*Reference
	commandScopes map[*syntax.Command]*Scope
}

func inferTypes(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	result.expressionTypes = make(map[*syntax.Expression]ValueType)
	state := &typeState{
		result:        result,
		declarations:  make(map[syntax.Span]*Declaration),
		references:    make(map[syntax.Span]*Reference),
		commandScopes: result.commandScopes,
	}
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
	for pass := 0; pass < 2; pass++ {
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
					if isUnknownType(typ) {
						typ = UnknownValueType
					}
					arguments = append(arguments, typ)
					if parameterDeclaration := state.declarations[parameterDeclarationSpan(file, parameter)]; parameterDeclaration != nil {
						parameterDeclaration.Type = typ
					}
				}
				returnType := convertSyntaxType(command.Function.ReturnType)
				declaration.Type = ValueType{Name: "func", Arguments: arguments, Return: valueTypePointer(returnType), ArgumentCountKnown: true, Variadic: parametersAreVariadic(command.Function.Parameters)}
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
				if !isUnknownType(typ) {
					declaration.Type = typ
				}
			}
		}
		if command.For != nil {
			state.collectLambdaFactsExpression(command.For.Iterable)
			for _, binding := range command.For.Bindings {
				if declaration := state.declarations[binding.Name]; declaration != nil {
					if typ := convertSyntaxType(binding.ParsedType); !isUnknownType(typ) {
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
				if declaration == nil || !isUnknownType(declaration.Type) {
					continue
				}
				expression := initializerElement(command.Declaration.Initializer, bindingIndex, len(command.Declaration.Bindings))
				declaration.Type = state.infer(expression, declaration.Scope)
			}
		}
		if command.For != nil {
			iterable := state.infer(command.For.Iterable, scope)
			for _, binding := range command.For.Bindings {
				if declaration := state.declarations[binding.Name]; declaration != nil && isUnknownType(declaration.Type) {
					declaration.Type = indexedType(iterable)
				}
			}
		}
		if command.Function != nil {
			functionDeclaration := state.declarations[command.Function.Name]
			for parameterIndex, parameter := range command.Function.Parameters {
				if parameter.Default != nil {
					defaultType := state.infer(parameter.Default, scope)
					if functionDeclaration != nil && parameterIndex < len(functionDeclaration.Type.Arguments) && isUnknownType(functionDeclaration.Type.Arguments[parameterIndex]) && !isUnknownType(defaultType) {
						functionDeclaration.Type.Arguments[parameterIndex] = defaultType
					}
					if parameterDeclaration := state.declarations[parameterDeclarationSpan(file, parameter)]; parameterDeclaration != nil && isUnknownType(parameterDeclaration.Type) && !isUnknownType(defaultType) {
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
		if command.Function != nil && command.Function.ReturnType == nil {
			declaration := state.declarations[command.Function.Name]
			if declaration != nil && declaration.Type.Return != nil && isUnknownType(*declaration.Type.Return) {
				inferred := UnknownValueType
				hasValueReturn := false
				for bodyIndex := index + 1; bodyIndex < len(commands); bodyIndex++ {
					body := &commands[bodyIndex]
					if body.Canonical == "endfunction" || body.Canonical == "enddef" {
						break
					}
					if body.Canonical == "return" && len(body.Expressions) > 0 {
						hasValueReturn = true
						current := state.infer(body.Expressions[0], state.commandScopes[body])
						if isUnknownType(inferred) {
							inferred = current
						} else {
							inferred = mergeTypes(inferred, current)
						}
					}
				}
				if !hasValueReturn {
					inferred = ValueType{Name: "void"}
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
	if typ, ok := state.result.expressionTypes[expression]; ok && !isUnknownType(typ) {
		return typ
	}
	unknown := UnknownValueType
	var typ ValueType
	switch expression.Kind {
	case syntax.ExpressionIdentifier:
		switch strings.ToLower(expression.Value) {
		case "true", "false":
			typ = ValueType{Name: "bool"}
		case "null":
			typ = ValueType{Name: "special"}
		case "null_blob":
			typ = ValueType{Name: "blob"}
		case "null_channel":
			typ = ValueType{Name: "channel"}
		case "null_class":
			typ = ValueType{Name: "class"}
		case "null_dict":
			typ = ValueType{Name: "dict", Arguments: []ValueType{UnknownValueType}}
		case "null_function", "null_partial":
			typ = ValueType{Name: "func", Return: valueTypePointer(UnknownValueType)}
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
			if strings.HasPrefix(expression.Value, "$") || strings.HasPrefix(expression.Value, "@") || terminalOptionName(expression.Value) {
				typ = ValueType{Name: "string"}
			} else if variable, ok := vimdata.LookupVariable(expression.Value); ok {
				typ = builtinVariableValueType(variable)
			} else if reference := state.references[expression.Span]; reference != nil && reference.Declaration != nil {
				typ = reference.Declaration.Type
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
			if index%2 == 1 {
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
			left := state.infer(expression.Children[1], scope)
			right := state.infer(expression.Children[2], scope)
			typ = mergeTypes(left, right)
		} else {
			typ = unknown
		}
	case syntax.ExpressionAssignment:
		if len(expression.Children) > 1 {
			typ = state.infer(expression.Children[1], scope)
			if len(expression.Children) > 0 {
				state.assign(expression.Children[0], typ)
			}
		} else {
			typ = unknown
		}
	case syntax.ExpressionCall:
		if len(expression.Children) > 0 {
			callee := state.infer(expression.Children[0], scope)
			for _, argument := range expression.Children[1:] {
				state.infer(argument, scope)
			}
			if builtin, arguments, ok := builtinCallArguments(state.result.File, expression); ok {
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
			typ = indexedType(state.infer(expression.Children[0], scope))
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

func terminalOptionName(name string) bool {
	if !strings.HasPrefix(name, "&") {
		return false
	}
	name = strings.TrimPrefix(name[1:], "l:")
	name = strings.TrimPrefix(name, "g:")
	return strings.HasPrefix(name, "t_")
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
		if variable.Name == "v:null" || variable.Name == "v:none" {
			return ValueType{Name: "special"}
		}
	}
	return UnknownValueType
}

func builtinReturnValueType(function vimdata.BuiltinFunction, arguments []ValueType) ValueType {
	switch function.ReturnHelper {
	case "ret_copy", "ret_first_arg", "ret_extend", "ret_slice":
		if len(arguments) > 0 {
			return arguments[0]
		}
	case "ret_first_cont":
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
		return ValueType{Name: "func", Return: valueTypePointer(UnknownValueType)}
	default:
		return UnknownValueType
	}
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
		if isUnknownType(typ) {
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
	if !isUnknownType(explicitType) {
		if isUnknownType(returnType) || compatibleTypes(explicitType, returnType) {
			returnType = explicitType
		} else {
			returnType = UnknownValueType
		}
	}
	return ValueType{Name: "func", Arguments: arguments, Return: valueTypePointer(returnType), ArgumentCountKnown: true, Variadic: parametersAreVariadic(expression.Parameters)}
}

func parametersAreVariadic(parameters []syntax.Parameter) bool {
	return len(parameters) > 0 && parameters[len(parameters)-1].Variadic
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
			if isUnknownType(result) {
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
	if expression == nil || isUnknownType(typ) {
		return
	}
	if declaration := state.declarations[expression.Span]; declaration != nil && isUnknownType(declaration.Type) {
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
	typ := ValueType{Name: typeNode.Name, ArgumentCountKnown: typeNode.Kind == syntax.TypeFunction}
	for _, argument := range typeNode.Arguments {
		typ.Arguments = append(typ.Arguments, convertSyntaxType(argument))
	}
	if (typeNode.Kind == syntax.TypeFunction || typeNode.Name == "tuple") && len(typeNode.Arguments) > 0 {
		typ.Variadic = typeNode.Arguments[len(typeNode.Arguments)-1].Kind == syntax.TypeVariadic
	}
	if typeNode.ReturnType != nil {
		typ.Return = valueTypePointer(convertSyntaxType(typeNode.ReturnType))
	}
	return typ
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
	if isUnknownType(left) || isUnknownType(right) {
		return UnknownValueType
	}
	if left.Name != right.Name || len(left.Arguments) != len(right.Arguments) {
		return UnknownValueType
	}
	result := ValueType{Name: left.Name, Return: left.Return, Arguments: append([]ValueType(nil), left.Arguments...), ArgumentCountKnown: left.ArgumentCountKnown && right.ArgumentCountKnown, Variadic: left.Variadic && right.Variadic}
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
	return typ.Name == "" || typ.Name == ValueTypeAny
}

func valueTypePointer(typ ValueType) *ValueType {
	return &typ
}
