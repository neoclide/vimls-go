package analysis

import (
	"strconv"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
)

func stringConversionDiagnostic(typ ValueType, span syntax.Span) (syntax.Diagnostic, bool) {
	switch typ.Name {
	case "func", "partial":
		return syntax.Diagnostic{Code: "vim/E729", Message: "Using a Funcref as a String", Span: span}, true
	case "list":
		return syntax.Diagnostic{Code: "vim/E730", Message: "Using a List as a String", Span: span}, true
	case "dict":
		return syntax.Diagnostic{Code: "vim/E731", Message: "Using a Dictionary as a String", Span: span}, true
	case "blob":
		return syntax.Diagnostic{Code: "vim/E976", Message: "Using a Blob as a String", Span: span}, true
	default:
		return syntax.Diagnostic{}, false
	}
}

func knownObjectExpression(result *FileAnalysis, scope *Scope, expression *syntax.Expression) bool {
	if result == nil || expression == nil || expressionContainsMissing(expression) {
		return false
	}
	if expression.Kind == syntax.ExpressionIdentifier {
		if declaration := resolve(scope, expression.Value, expression.Span.Start, false, nil); declaration != nil && declaration.Kind == SymbolKindClass {
			return false
		}
	}
	name := result.TypeOf(expression).Name
	return name == "object" || result.classes[name] != nil
}

func objectAsNumberDiagnostic(result *FileAnalysis, scope *Scope, expression *syntax.Expression) (syntax.Diagnostic, bool) {
	if !knownObjectExpression(result, scope, expression) {
		return syntax.Diagnostic{}, false
	}
	return syntax.Diagnostic{Code: "vim/E1320", Message: "Using an Object as a Number", Span: expression.Span}, true
}

func objectAsStringDiagnostic(result *FileAnalysis, scope *Scope, expression *syntax.Expression) (syntax.Diagnostic, bool) {
	if !knownObjectExpression(result, scope, expression) {
		return syntax.Diagnostic{}, false
	}
	return syntax.Diagnostic{Code: "vim/E1324", Message: "Using an Object as a String", Span: expression.Span}, true
}

func strictStringConversionDiagnostic(result *FileAnalysis, scope *Scope, expression *syntax.Expression, interpolate bool) (syntax.Diagnostic, bool) {
	if result == nil || expression == nil {
		return syntax.Diagnostic{}, false
	}
	if diagnostic, ok := objectAsStringDiagnostic(result, scope, expression); ok {
		return diagnostic, true
	}
	typ := result.TypeOf(expression)
	name := typ.Name
	if expression.Kind == syntax.ExpressionIdentifier {
		if declaration := resolve(scope, expression.Value, expression.Span.Start, false, nil); declaration != nil && declaration.Kind == SymbolKindTypeAlias {
			name = "typealias"
		} else if declaration != nil && declaration.Kind == SymbolKindClass {
			name = "class"
		}
	}
	if expression.Kind == syntax.ExpressionCall && len(expression.Children) > 0 && expression.Children[0].Kind == syntax.ExpressionIdentifier && expression.Children[0].Value == "function" && len(expression.Children) > 2 {
		name = "partial"
	}
	if expression.Kind == syntax.ExpressionIdentifier && expression.Value == "null_partial" {
		name = "partial"
	}
	if isUnknownType(ValueType{Name: name}) || name == "string" || isSpecialType(ValueType{Name: name}) || name == "bool" || name == "number" || name == "float" || interpolate && (name == "list" || name == "tuple" || name == "dict") {
		return syntax.Diagnostic{}, false
	}
	switch name {
	case "list", "tuple", "dict", "void", "blob", "func", "partial", "job", "channel", "class", "typealias":
	default:
		return syntax.Diagnostic{}, false
	}
	return syntax.Diagnostic{Code: "vim/E1105", Message: "Cannot convert " + strings.ToLower(name) + " to string", Span: expression.Span}, true
}

func numericConversionDiagnostic(typ ValueType, span syntax.Span) (syntax.Diagnostic, bool) {
	switch valueTypeCategory(typ) {
	case ValueTypeSpecial:
		return syntax.Diagnostic{Code: "vim/E611", Message: "Using a Special as a Number", Span: span}, true
	case "func":
		return syntax.Diagnostic{Code: "vim/E703", Message: "Using a Funcref as a Number", Span: span}, true
	case "dict":
		return syntax.Diagnostic{Code: "vim/E728", Message: "Using a Dictionary as a Number", Span: span}, true
	case "list":
		return syntax.Diagnostic{Code: "vim/E745", Message: "Using a List as a Number", Span: span}, true
	case "blob":
		return syntax.Diagnostic{Code: "vim/E974", Message: "Using a Blob as a Number", Span: span}, true
	default:
		return syntax.Diagnostic{}, false
	}
}

func stringAsNumberDiagnostic(result *FileAnalysis, expression *syntax.Expression) (syntax.Diagnostic, bool) {
	if result == nil || expression == nil || result.TypeOf(expression).Name != "string" {
		return syntax.Diagnostic{}, false
	}
	message := "Using a String as a Number"
	literal := expression
	for literal.Kind == syntax.ExpressionParenthesized && len(literal.Children) == 1 {
		literal = literal.Children[0]
	}
	if literal.Kind == syntax.ExpressionString {
		if value, ok := syntax.StaticDictionaryIndexKey(literal); ok {
			message += `: "` + value + `"`
		}
	}
	return syntax.Diagnostic{Code: "vim/E1030", Message: message, Span: expression.Span}, true
}

func stringAsBoolDiagnostic(result *FileAnalysis, expression *syntax.Expression) (syntax.Diagnostic, bool) {
	if result == nil || expression == nil || expressionContainsMissing(expression) || result.TypeOf(expression).Name != "string" {
		return syntax.Diagnostic{}, false
	}
	message := "Using a String as a Bool"
	literal := expression
	for literal.Kind == syntax.ExpressionParenthesized && len(literal.Children) == 1 {
		literal = literal.Children[0]
	}
	if literal.Kind == syntax.ExpressionString {
		if value, ok := syntax.StaticDictionaryIndexKey(literal); ok {
			message += `: "` + value + `"`
		}
	}
	return syntax.Diagnostic{Code: "vim/E1135", Message: message, Span: expression.Span}, true
}

func boolAsNumberDiagnostic(typ ValueType, expression *syntax.Expression) (syntax.Diagnostic, bool) {
	if expression == nil || expressionContainsMissing(expression) || typ.Name != "bool" {
		return syntax.Diagnostic{}, false
	}
	return syntax.Diagnostic{Code: "vim/E1138", Message: "Using a Bool as a Number", Span: expression.Span}, true
}

func staticNumberValue(expression *syntax.Expression) (int64, bool) {
	if expression == nil {
		return 0, false
	}
	switch expression.Kind {
	case syntax.ExpressionParenthesized:
		if len(expression.Children) == 1 {
			return staticNumberValue(expression.Children[0])
		}
	case syntax.ExpressionUnary:
		if len(expression.Children) == 1 && (expression.Value == "+" || expression.Value == "-") {
			value, ok := staticNumberValue(expression.Children[0])
			if ok && expression.Value == "-" {
				value = -value
			}
			return value, ok
		}
	case syntax.ExpressionNumber:
		if isFloatLiteral(expression.Value) {
			return 0, false
		}
		literal := strings.ReplaceAll(expression.Value, "'", "")
		value, err := strconv.ParseInt(literal, 0, 64)
		if err != nil {
			value, err = strconv.ParseInt(literal, 10, 64)
		}
		return value, err == nil
	}
	return 0, false
}

func numberAsBoolDiagnostic(expression *syntax.Expression) (syntax.Diagnostic, bool) {
	value, ok := staticNumberValue(expression)
	if !ok || value == 0 || value == 1 {
		return syntax.Diagnostic{}, false
	}
	return syntax.Diagnostic{
		Code: "vim/E1023", Message: "Using a Number as a Bool: " + strconv.FormatInt(value, 10), Span: expression.Span,
	}, true
}

func logicalRightOperandIsSkipped(expression *syntax.Expression) bool {
	if expression == nil || len(expression.Children) < 2 {
		return false
	}
	left := expression.Children[0]
	for left != nil && left.Kind == syntax.ExpressionParenthesized && len(left.Children) == 1 {
		left = left.Children[0]
	}
	value, known := staticNumberValue(left)
	if left != nil && left.Kind == syntax.ExpressionIdentifier {
		switch left.Value {
		case "true", "v:true":
			value, known = 1, true
		case "false", "v:false":
			value, known = 0, true
		}
	}
	return known && (expression.Value == "&&" && value == 0 || expression.Value == "||" && value == 1)
}

func logicalRightOperandIsEvaluated(expression *syntax.Expression) bool {
	if expression == nil || expression.Kind != syntax.ExpressionBinary || len(expression.Children) < 2 {
		return false
	}
	left := expression.Children[0]
	if value, ok := staticNumberValue(left); ok {
		return expression.Value == "||" && value == 0 || expression.Value == "&&" && value == 1
	}
	if left != nil && left.Kind == syntax.ExpressionIdentifier {
		switch expression.Value {
		case "||":
			return left.Value == "false" || left.Value == "v:false"
		case "&&":
			return left.Value == "true" || left.Value == "v:true"
		}
	}
	return false
}

func extendArgumentMismatchIndex(actual []ValueType) int {
	if len(actual) < 2 || isUnknownType(actual[0]) || isUnknownType(actual[1]) {
		return -1
	}
	first := actual[0].Name
	if first != "blob" && first != "dict" && first != "list" {
		return 0
	}
	if actual[1].Name != first {
		return 1
	}
	return -1
}

func isStaticNullTuple(expression *syntax.Expression) bool {
	if expression == nil {
		return false
	}
	if expression.Kind == syntax.ExpressionIdentifier {
		return expression.Value == "null_tuple"
	}
	if expression.Kind == syntax.ExpressionParenthesized && len(expression.Children) == 1 {
		return isStaticNullTuple(expression.Children[0])
	}
	return expression.Kind == syntax.ExpressionCall && len(expression.Children) == 1 &&
		expression.Children[0] != nil && expression.Children[0].Kind == syntax.ExpressionIdentifier && expression.Children[0].Value == "test_null_tuple"
}

// collectOperatorDiagnostics keeps compiled Vim9 operator errors distinct
// from the historical conversion errors used by Legacy and script-level Vim9.
// Unknown values remain deliberately opaque.
func collectOperatorDiagnostics(result *FileAnalysis, commands []syntax.Command, parent *Scope) {
	if result == nil || result.File == nil {
		return
	}
	staticInitializers := make(map[*Declaration]*syntax.Expression)
	for index := range commands {
		command := &commands[index]
		if command.Declaration == nil || len(command.Declaration.Bindings) != 1 || command.Declaration.Initializer == nil {
			continue
		}
		scope := result.commandScopes[command]
		if scope == nil {
			scope = parent
		}
		binding := command.Declaration.Bindings[0]
		var declaration *Declaration
		for _, candidate := range scope.Declarations {
			if candidate.Span == binding.Name {
				declaration = candidate
				break
			}
		}
		if declaration != nil {
			staticInitializers[declaration] = command.Declaration.Initializer
		}
	}
	for index := range commands {
		command := &commands[index]
		scope := result.commandScopes[command]
		if scope == nil {
			scope = parent
		}
		seen := make(map[*syntax.Expression]bool)
		var walk func(*syntax.Expression, *Scope)
		walk = func(expression *syntax.Expression, expressionScope *Scope) {
			if expression == nil || seen[expression] {
				return
			}
			seen[expression] = true
			if expression.Kind == syntax.ExpressionLambda {
				if lambdaScope := result.lambdaScopes[expression]; lambdaScope != nil {
					expressionScope = lambdaScope
				}
				if expression.LambdaBody != nil {
					collectOperatorDiagnostics(result, expression.LambdaBody.Commands, expressionScope)
				}
			}
			if command.Dialect == syntax.Vim9 && expression.Kind == syntax.ExpressionMember {
				appendProtectedMethodAccessDiagnostic(result, expressionScope, expression)
				appendProtectedVariableAccessDiagnostic(result, expressionScope, expression)
				appendObjectVariableThroughClassDiagnostic(result, expressionScope, expression)
				appendClassVariableThroughObjectDiagnostic(result, expressionScope, expression)
				appendClassMethodThroughObjectDiagnostic(result, expressionScope, expression)
			}
			if expression.Kind == syntax.ExpressionCall && !expressionContainsMissing(expression) &&
				!(command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope)) {
				builtin, arguments, ok := builtinCallArguments(result.File, expression)
				if ok && (builtin.Name == "extend" || builtin.Name == "extendnew") {
					actual := make([]ValueType, len(arguments))
					for index, argument := range arguments {
						actual[index] = result.TypeOf(argument)
					}
					if bad := extendArgumentMismatchIndex(actual); bad >= 0 {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E896", Message: "Argument of " + builtin.Name + "() must be a List, Dictionary or Blob", Span: arguments[bad].Span,
						})
					}
				}
			}
			if target, ok := objectCompoundAssignment(result, expressionScope, expression); ok {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1411", Message: "Missing dot after object \"" + target.Value + "\"", Span: target.Span,
				})
				return
			}
			if expression.Kind == syntax.ExpressionBinary || expression.Kind == syntax.ExpressionAssignment {
				op := expression.Value
				if expression.Kind == syntax.ExpressionAssignment {
					op = result.File.Text(expression.Operator)
				}
				if expression.Kind == syntax.ExpressionBinary && command.Dialect == syntax.Vim9 &&
					len(expression.Children) >= 2 && !expressionContainsMissing(expression) {
					base := strings.TrimRight(op, "#?")
					comparison := base == "==" || base == "!=" || base == "=~" || base == "!~" || base == "is" || base == "isnot" ||
						base == ">" || base == ">=" || base == "<" || base == "<="
					if comparison && (base != "is" && base != "isnot" || base == op) {
						leftExpression, rightExpression := expression.Children[0], expression.Children[1]
						left, right := result.TypeOf(leftExpression), result.TypeOf(rightExpression)
						objectOperation := base == ">" || base == ">=" || base == "<" || base == "<=" || base == "=~" || base == "!~"
						if objectOperation && objectComparisonValue(result, expressionScope, leftExpression) && objectComparisonValue(result, expressionScope, rightExpression) {
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
								Code: "vim/E1153", Message: "Invalid operation for object", Span: expression.Operator,
							})
							return
						}
						leftKind, rightKind := builtinValueTypeKind(left), builtinValueTypeKind(right)
						invalid := false
						leftCategory, rightCategory := valueTypeCategory(left), valueTypeCategory(right)
						if !isUnknownType(left) && !isUnknownType(right) && leftKind != 0 && rightKind != 0 && left.Name != "void" && right.Name != "void" {
							if leftCategory == rightCategory {
								equality := base == "==" || base == "!=" || base == "is" || base == "isnot"
								invalid = !equality && (leftCategory == "bool" || leftCategory == ValueTypeSpecial || leftCategory == "blob" || leftCategory == "list")
							} else if (left.Name == "number" || left.Name == "float") && (right.Name == "number" || right.Name == "float") {
								// Number and Float comparisons use Vim's shared numeric path.
							} else if leftCategory == ValueTypeSpecial || rightCategory == ValueTypeSpecial {
								invalid = left.Name == ValueTypeNone && right.Name != "string" || right.Name == ValueTypeNone && left.Name != "string"
							} else {
								invalid = true
							}
						}
						if invalid {
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
								Code: "vim/E1072", Message: "Cannot compare " + leftCategory + " with " + rightCategory, Span: expression.Operator,
							})
						}
					}
				}
				if expression.Kind == syntax.ExpressionBinary && command.Dialect == syntax.Vim9 &&
					(op == "is" || op == "isnot") && len(expression.Children) >= 2 && !expressionContainsMissing(expression) {
					left, right := result.TypeOf(expression.Children[0]), result.TypeOf(expression.Children[1])
					leftCategory, rightCategory := valueTypeCategory(left), valueTypeCategory(right)
					if leftCategory == rightCategory && (leftCategory == "bool" || leftCategory == ValueTypeSpecial || leftCategory == "number" || leftCategory == "float") {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E1037", Message: `Cannot use "` + op + `" with ` + leftCategory, Span: expression.Operator,
						})
					}
				}
				compoundTypeError := false
				if expression.Kind == syntax.ExpressionAssignment && len(expression.Children) >= 2 && !expressionContainsMissing(expression) {
					targetType := assignmentTargetType(result, expressionScope, expression.Children[0])
					rightType := result.TypeOf(expression.Children[1])
					numericCompound := op == "+=" || op == "-=" || op == "*=" || op == "/=" || op == "%="
					invalidTarget := numericCompound && targetType.Name == "dict"
					invalidScriptConcat := (op == ".=" || op == "..=") && !scopeUsesDefTypeRules(expressionScope) && targetType.Name == "string" && (rightType.Name == "list" || rightType.Name == "dict")
					objectConcat := false
					if (op == ".=" || op == "..=") && !scopeUsesDefTypeRules(expressionScope) && targetType.Name == "string" {
						if diagnostic, ok := objectAsStringDiagnostic(result, expressionScope, expression.Children[1]); ok {
							result.Diagnostics = append(result.Diagnostics, diagnostic)
							objectConcat = true
						}
					}
					objectNumber := false
					if numericCompound && !scopeUsesDefTypeRules(expressionScope) && (targetType.Name == "number" || targetType.Name == "float") {
						if diagnostic, ok := objectAsNumberDiagnostic(result, expressionScope, expression.Children[1]); ok {
							result.Diagnostics = append(result.Diagnostics, diagnostic)
							objectNumber = true
							compoundTypeError = true
						}
					}
					if !objectConcat && !objectNumber && (invalidTarget || invalidScriptConcat) {
						symbol := strings.TrimSuffix(op, "=")
						if symbol == ".." {
							symbol = "."
						}
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E734", Message: "Wrong variable type for " + symbol + "=", Span: expression.Operator,
						})
						compoundTypeError = true
					}
				}
				compiled := command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope)
				if compiled && !expressionContainsMissing(expression) && len(expression.Children) >= 2 {
					if expression.Kind == syntax.ExpressionBinary && (op == "." || op == "..") {
						for _, operand := range expression.Children[:2] {
							if diagnostic, ok := strictStringConversionDiagnostic(result, expressionScope, operand, false); ok {
								result.Diagnostics = append(result.Diagnostics, diagnostic)
								break
							}
						}
					} else if expression.Kind == syntax.ExpressionAssignment && (op == ".=" || op == "..=") && assignmentTargetType(result, expressionScope, expression.Children[0]).Name == "string" {
						if diagnostic, ok := strictStringConversionDiagnostic(result, expressionScope, expression.Children[1], false); ok {
							result.Diagnostics = append(result.Diagnostics, diagnostic)
						}
					}
				}
				if expression.Kind == syntax.ExpressionBinary && (op == "<<" || op == ">>") && len(expression.Children) >= 2 && !expressionContainsMissing(expression) {
					leftShift := expression.Children[0]
					for leftShift.Kind == syntax.ExpressionParenthesized && len(leftShift.Children) == 1 {
						leftShift = leftShift.Children[0]
					}
					if leftShift.Kind == syntax.ExpressionBinary && (leftShift.Value == "<<" || leftShift.Value == ">>") {
						walk(leftShift, expressionScope)
						for _, diagnostic := range result.Diagnostics {
							if (diagnostic.Code == "vim/E1282" || diagnostic.Code == "vim/E1283" || diagnostic.Code == "vim/E1012") &&
								diagnostic.Span.Start >= leftShift.Span.Start && diagnostic.Span.End <= leftShift.Span.End {
								return
							}
						}
					}
					compiled := command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope)
					for _, operand := range expression.Children[:2] {
						actual := result.TypeOf(operand)
						if isUnknownType(actual) || actual.Name == "number" {
							continue
						}
						constant := operand
						for constant.Kind == syntax.ExpressionParenthesized && len(constant.Children) == 1 {
							constant = constant.Children[0]
						}
						precompiledConstant := constant.Kind == syntax.ExpressionString || constant.Kind == syntax.ExpressionBlob ||
							constant.Kind == syntax.ExpressionNumber && isFloatLiteral(constant.Value) ||
							constant.Kind == syntax.ExpressionIdentifier && (isLiteralIdentifier(constant.Value) || constant.Value == "v:true" || constant.Value == "v:false" || constant.Value == "v:null" || constant.Value == "v:none")
						if !compiled || precompiledConstant {
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
								Code: "vim/E1282", Message: "Bitshift operands must be numbers", Span: expression.Operator,
							})
							return
						}
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E1012", Message: "Type mismatch; expected number but got " + valueTypeDisplay(actual), Span: operand.Span,
						})
						return
					}
					amount, knownAmount := staticNumberValue(expression.Children[1])
					right := expression.Children[1]
					for right.Kind == syntax.ExpressionParenthesized && len(right.Children) == 1 {
						right = right.Children[0]
					}
					if !knownAmount && right.Kind == syntax.ExpressionIdentifier {
						declaration := resolve(expressionScope, right.Value, right.Span.Start, false, nil)
						initializer := staticInitializers[declaration]
						if declaration != nil && declaration.Scope == expressionScope && initializer != nil {
							unchanged := true
							for _, reference := range result.References {
								if reference.Declaration == declaration && reference.assignmentTarget && reference.Span.Start > declaration.Span.End && reference.Span.Start < expression.Children[1].Span.Start {
									unchanged = false
									break
								}
							}
							if unchanged {
								amount, knownAmount = staticNumberValue(initializer)
							}
						}
					}
					if knownAmount && amount < 0 {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E1283", Message: "Bitshift amount must be a positive number", Span: expression.Children[1].Span,
						})
						return
					}
				}
				if !compoundTypeError && (op == "+" || op == "-" || op == "*" || op == "/" || op == "%" || op == "+=" || op == "-=" || op == "*=" || op == "/=" || op == "%=") && len(expression.Children) >= 2 && !expressionContainsMissing(expression) {
					left, right := result.TypeOf(expression.Children[0]), result.TypeOf(expression.Children[1])
					if expression.Kind == syntax.ExpressionAssignment {
						left = assignmentTargetType(result, expressionScope, expression.Children[0])
					}
					boolAsNumber := false
					if command.Dialect == syntax.Vim9 && !scopeUsesDefTypeRules(expressionScope) {
						if diagnostic, ok := boolAsNumberDiagnostic(left, expression.Children[0]); ok {
							result.Diagnostics = append(result.Diagnostics, diagnostic)
							boolAsNumber = true
						} else if left.Name == "number" || left.Name == "float" {
							if diagnostic, ok := boolAsNumberDiagnostic(right, expression.Children[1]); ok {
								result.Diagnostics = append(result.Diagnostics, diagnostic)
								boolAsNumber = true
							}
						}
					}
					if !boolAsNumber && command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope) {
						base := strings.TrimSuffix(op, "=")
						invalid := false
						code, message := "", ""
						if !isUnknownType(left) && !isUnknownType(right) {
							leftNumeric := left.Name == "number" || left.Name == "float"
							rightNumeric := right.Name == "number" || right.Name == "float"
							switch base {
							case "%":
								invalid = left.Name != "number" || right.Name != "number"
								code, message = "vim/E1035", "requires number arguments"
							case "-", "*", "/":
								invalid = !leftNumeric || !rightNumeric
								code, message = "vim/E1036", base+" requires number or float arguments"
							case "+":
								containerConcat := left.Name == right.Name && (left.Name == "list" || left.Name == "tuple" || left.Name == "blob")
								invalid = (!leftNumeric || !rightNumeric) && !containerConcat
								code, message = "vim/E1051", "Wrong argument type for +"
							}
						}
						if invalid {
							span := expression.Operator
							if span.End <= span.Start {
								span = expression.Span
							}
							if code == "vim/E1035" {
								message = "% " + message
							}
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: code, Message: message, Span: span})
						}
					} else if !boolAsNumber && expression.Kind == syntax.ExpressionBinary {
						leftOperand, rightOperand := expression.Children[0], expression.Children[1]
						containerConcat := op == "+" && left.Name == right.Name && (left.Name == "list" || left.Name == "tuple" || left.Name == "blob")
						possibleContainerConcat := op == "+" && (isUnknownType(left) && (right.Name == "list" || right.Name == "tuple" || right.Name == "blob") ||
							isUnknownType(right) && (left.Name == "list" || left.Name == "tuple" || left.Name == "blob"))
						diagnostic, ok := syntax.Diagnostic{}, false
						if !containerConcat && !possibleContainerConcat && command.Dialect == syntax.Vim9 {
							diagnostic, ok = objectAsNumberDiagnostic(result, expressionScope, leftOperand)
							if !ok {
								diagnostic, ok = stringAsNumberDiagnostic(result, leftOperand)
							}
						}
						if !containerConcat && !possibleContainerConcat {
							if !ok {
								diagnostic, ok = numericConversionDiagnostic(left, leftOperand.Span)
							}
						}
						if !ok && !containerConcat && !possibleContainerConcat {
							leftNumeric := left.Name == "number" || left.Name == "float"
							if (right.Name != "list" && right.Name != "blob") || leftNumeric {
								if command.Dialect == syntax.Vim9 {
									diagnostic, ok = objectAsNumberDiagnostic(result, expressionScope, rightOperand)
									if !ok {
										diagnostic, ok = stringAsNumberDiagnostic(result, rightOperand)
									}
								}
								if !ok {
									diagnostic, ok = numericConversionDiagnostic(right, rightOperand.Span)
								}
							}
						}
						if !ok && op == "%" {
							leftNumeric := left.Name == "number" || left.Name == "float"
							rightNumeric := right.Name == "number" || right.Name == "float"
							if leftNumeric && rightNumeric && (left.Name == "float" || right.Name == "float") {
								diagnostic = syntax.Diagnostic{Code: "vim/E804", Message: "Cannot use '%' with Float", Span: expression.Operator}
								ok = true
							}
						}
						if ok {
							result.Diagnostics = append(result.Diagnostics, diagnostic)
						}
					}
				}
				if expression.Kind == syntax.ExpressionBinary && (op == "&&" || op == "||") && len(expression.Children) >= 2 && !expressionContainsMissing(expression) &&
					!(command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope)) {
					left, right := expression.Children[0], expression.Children[1]
					if command.Dialect == syntax.Vim9 {
						if diagnostic, ok := stringAsBoolDiagnostic(result, left); ok {
							result.Diagnostics = append(result.Diagnostics, diagnostic)
							return
						}
						if logicalRightOperandIsEvaluated(expression) {
							if diagnostic, ok := stringAsBoolDiagnostic(result, right); ok {
								result.Diagnostics = append(result.Diagnostics, diagnostic)
								return
							}
						}
						diagnostic, ok := numberAsBoolDiagnostic(left)
						if !ok && logicalRightOperandIsEvaluated(expression) {
							diagnostic, ok = numberAsBoolDiagnostic(right)
						}
						if ok {
							result.Diagnostics = append(result.Diagnostics, diagnostic)
							return
						}
					}
					operand := left
					if result.TypeOf(left).Name != "list" {
						operand = nil
						if logicalRightOperandIsEvaluated(expression) && result.TypeOf(right).Name == "list" {
							operand = right
						}
					}
					if operand != nil {
						diagnostic, _ := numericConversionDiagnostic(ValueType{Name: "list"}, operand.Span)
						result.Diagnostics = append(result.Diagnostics, diagnostic)
					}
				}
				if expression.Kind == syntax.ExpressionBinary && (op == "." || op == "..") && len(expression.Children) >= 2 && !expressionContainsMissing(expression) &&
					!(command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope)) {
					left, right := expression.Children[0], expression.Children[1]
					leftType, rightType := result.TypeOf(left), result.TypeOf(right)
					diagnostic, ok := stringConversionDiagnostic(leftType, left.Span)
					if !ok && command.Dialect == syntax.Vim9 {
						diagnostic, ok = objectAsStringDiagnostic(result, expressionScope, left)
					}
					if !ok && command.Dialect == syntax.Vim9 && (leftType.Name == "job" || leftType.Name == "channel") {
						diagnostic = syntax.Diagnostic{Code: "vim/E908", Message: "Using an invalid value as a String: " + leftType.Name, Span: left.Span}
						ok = true
					}
					if !ok {
						leftConvertible := leftType.Name == "bool" || leftType.Name == "float" || leftType.Name == "number" || isSpecialType(leftType) || leftType.Name == "string"
						invalidRight := rightType.Name == "void" || rightType.Name == "job" || rightType.Name == "channel"
						if command.Dialect == syntax.Vim9 && leftConvertible && invalidRight {
							diagnostic = syntax.Diagnostic{Code: "vim/E908", Message: "Using an invalid value as a String: " + rightType.Name, Span: right.Span}
							ok = true
						} else {
							diagnostic, ok = stringConversionDiagnostic(rightType, right.Span)
							if !ok && command.Dialect == syntax.Vim9 {
								diagnostic, ok = objectAsStringDiagnostic(result, expressionScope, right)
							}
						}
					}
					if ok {
						result.Diagnostics = append(result.Diagnostics, diagnostic)
					}
				}
			}
			if expression.Kind == syntax.ExpressionUnary && (expression.Value == "+" || expression.Value == "-") && len(expression.Children) == 1 && !expressionContainsMissing(expression) {
				operand := expression.Children[0]
				if command.Dialect == syntax.Vim9 {
					diagnostic, ok := syntax.Diagnostic{}, false
					if !scopeUsesDefTypeRules(expressionScope) {
						diagnostic, ok = objectAsNumberDiagnostic(result, expressionScope, operand)
					}
					if !ok {
						diagnostic, ok = stringAsNumberDiagnostic(result, operand)
					}
					if ok {
						result.Diagnostics = append(result.Diagnostics, diagnostic)
					}
				}
				if result.TypeOf(operand).Name == "blob" {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E974", Message: "Using a Blob as a Number", Span: operand.Span,
					})
				}
			}
			if expression.Kind == syntax.ExpressionTernary && len(expression.Children) == 3 && !expressionContainsMissing(expression) {
				condition := expression.Children[0]
				if command.Dialect == syntax.Vim9 {
					if diagnostic, ok := stringAsBoolDiagnostic(result, condition); ok {
						literal := condition
						for literal.Kind == syntax.ExpressionParenthesized && len(literal.Children) == 1 {
							literal = literal.Children[0]
						}
						if !scopeUsesDefTypeRules(expressionScope) || literal.Kind == syntax.ExpressionString {
							result.Diagnostics = append(result.Diagnostics, diagnostic)
						}
					}
					if diagnostic, ok := numberAsBoolDiagnostic(condition); ok {
						result.Diagnostics = append(result.Diagnostics, diagnostic)
					}
					if !scopeUsesDefTypeRules(expressionScope) {
						if diagnostic, ok := objectAsNumberDiagnostic(result, expressionScope, condition); ok {
							result.Diagnostics = append(result.Diagnostics, diagnostic)
						}
					}
				}
				switch result.TypeOf(condition).Name {
				case "float":
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E805", Message: "Using a Float as a Number", Span: condition.Span,
					})
				case "blob":
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E974", Message: "Using a Blob as a Number", Span: condition.Span,
					})
				}
			}
			if (expression.Kind == syntax.ExpressionIndex || expression.Kind == syntax.ExpressionSlice) && len(expression.Children) > 0 &&
				!syntaxDiagnosticOverlaps(result.File.Diagnostics, expression.Span) {
				receiver := expression.Children[0]
				for receiver != nil && receiver.Kind == syntax.ExpressionParenthesized && len(receiver.Children) == 1 {
					receiver = receiver.Children[0]
				}
				if receiver != nil && receiver.Kind == syntax.ExpressionIdentifier &&
					(receiver.Value == "v:true" || receiver.Value == "v:false" || receiver.Value == "v:null" || receiver.Value == "v:none") {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E909", Message: "Cannot index a special variable", Span: expression.Children[0].Span,
					})
				}
			}
			if expression.Kind == syntax.ExpressionIndex && len(expression.Children) >= 2 &&
				!expressionContainsMissing(expression) && !syntaxDiagnosticOverlaps(result.File.Diagnostics, expression.Span) {
				receiver := expression.Children[0]
				literal := receiver
				for literal.Kind == syntax.ExpressionParenthesized && len(literal.Children) == 1 {
					literal = literal.Children[0]
				}
				if literal.Kind == syntax.ExpressionList {
					if index, ok := staticNumberValue(expression.Children[1]); ok &&
						(index >= int64(len(literal.Children)) || index < -int64(len(literal.Children))) {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E684", Message: "List index out of range: " + strconv.FormatInt(index, 10), Span: expression.Children[1].Span,
						})
					}
				}
				receiverType := resolvedExpressionType(result, expressionScope, receiver)
				if receiverType.Name == "func" || receiverType.Name == "partial" {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E695", Message: "Cannot index a Funcref", Span: receiver.Span,
					})
				}
			}
			if expression.Kind == syntax.ExpressionSlice && len(expression.Children) > 0 && !syntaxDiagnosticOverlaps(result.File.Diagnostics, expression.Span) {
				writeTarget := false
				for _, root := range command.Expressions {
					if root != nil && root.Kind == syntax.ExpressionAssignment && len(root.Children) > 0 && root.Children[0] == expression {
						writeTarget = true
						break
					}
				}
				receiver := expression.Children[0]
				for receiver != nil && receiver.Kind == syntax.ExpressionParenthesized && len(receiver.Children) == 1 {
					receiver = receiver.Children[0]
				}
				dictionary := receiver != nil && receiver.Kind == syntax.ExpressionDictionary
				if receiver != nil && receiver.Kind == syntax.ExpressionIdentifier {
					if command.Dialect == syntax.Vim9 && resolvedExpressionType(result, expressionScope, receiver).Name == "dict" {
						dictionary = true
					} else if variable, ok := vimdata.LookupVariable(receiver.Value); ok && builtinVariableValueType(variable).Name == "dict" {
						dictionary = true
					}
				}
				vim9Unlet := command.Canonical == "unlet" && command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope)
				if dictionary && !writeTarget && !vim9Unlet {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E719", Message: "Cannot slice a Dictionary", Span: expression.Span,
					})
				}
			}
			if (expression.Kind == syntax.ExpressionIndex || expression.Kind == syntax.ExpressionSlice) && len(expression.Children) >= 2 &&
				command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope) && !expressionContainsMissing(expression) {
				receiver := expression.Children[0]
				receiverType := resolvedExpressionType(result, expressionScope, receiver)
				candidate := receiver
				for candidate.Kind == syntax.ExpressionParenthesized && len(candidate.Children) == 1 {
					candidate = candidate.Children[0]
				}
				isTypeAlias := false
				if candidate.Kind == syntax.ExpressionIdentifier {
					declaration := resolve(expressionScope, candidate.Value, candidate.Span.Start, false, nil)
					isTypeAlias = declaration != nil && declaration.Kind == SymbolKindTypeAlias
				}
				if !isTypeAlias && (receiverType.Name == "number" || receiverType.Name == "float") {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E1107", Message: "String, List, Dict or Blob required", Span: receiver.Span,
					})
				}
			} else if (expression.Kind == syntax.ExpressionIndex || expression.Kind == syntax.ExpressionSlice) && len(expression.Children) >= 2 &&
				!(command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope)) {
				receiver := expression.Children[0]
				switch resolvedExpressionType(result, expressionScope, receiver).Name {
				case "number":
					if command.Dialect == syntax.Vim9 {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E1062", Message: "Cannot index a Number", Span: receiver.Span,
						})
					}
				case "float":
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E806", Message: "Using a Float as a String", Span: receiver.Span,
					})
				}
			}
			if command.Dialect == syntax.Legacy && (expression.Kind == syntax.ExpressionIndex || expression.Kind == syntax.ExpressionSlice) && len(expression.Children) >= 2 {
				receiver := resolvedExpressionType(result, expressionScope, expression.Children[0])
				if receiver.Name == "blob" || receiver.Name == "list" || receiver.Name == "string" || receiver.Name == "tuple" {
					for _, index := range expression.Children[1:] {
						if resolvedExpressionType(result, expressionScope, index).Name == "float" {
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
								Code: "vim/E805", Message: "Using a Float as a Number", Span: index.Span,
							})
							break
						}
					}
				}
			}
			if expression.Kind == syntax.ExpressionDictionary && command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope) {
				for keyIndex := 0; keyIndex+1 < len(expression.Children); keyIndex += 2 {
					key := expression.Children[keyIndex]
					if key.Kind != syntax.ExpressionList || len(key.Children) != 1 {
						continue
					}
					key = key.Children[0]
					if diagnostic, ok := strictStringConversionDiagnostic(result, expressionScope, key, false); ok {
						result.Diagnostics = append(result.Diagnostics, diagnostic)
						break
					}
				}
			} else if expression.Kind == syntax.ExpressionDictionary {
				for keyIndex := 0; keyIndex+1 < len(expression.Children); keyIndex += 2 {
					key := expression.Children[keyIndex]
					value := key
					if command.Dialect == syntax.Vim9 {
						if key.Kind != syntax.ExpressionList || len(key.Children) != 1 {
							continue
						}
						value = key.Children[0]
					}
					valueType := resolvedExpressionType(result, expressionScope, value)
					if value.Kind == syntax.ExpressionList {
						valueType = ValueType{Name: "list"}
					}
					if valueType.Name == "list" {
						diagnostic, _ := stringConversionDiagnostic(valueType, value.Span)
						result.Diagnostics = append(result.Diagnostics, diagnostic)
						break
					}
				}
			}
			if expression.Kind == syntax.ExpressionInterpolatedString && command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope) {
				for _, child := range expression.Children {
					if diagnostic, ok := strictStringConversionDiagnostic(result, expressionScope, child, true); ok {
						result.Diagnostics = append(result.Diagnostics, diagnostic)
						break
					}
				}
			}
			for _, child := range expression.Children {
				walk(child, expressionScope)
			}
		}
		for _, expression := range command.Expressions {
			walk(expression, scope)
		}
		for _, expression := range command.Targets {
			walk(expression, scope)
		}
		if command.Declaration != nil {
			walk(command.Declaration.Initializer, scope)
		}
		if command.Mapping != nil {
			// Vim v9.2.1015 map.c:eval_map_expr uses eval_to_string,
			// which stringifies containers and accepts numbers and floats.
			// Only literal Blob/Funcref results are rejected here. Stored
			// variable and function types may change before a mapping runs.
			expression := command.Mapping.RHSExpression
			for expression != nil && expression.Kind == syntax.ExpressionParenthesized && len(expression.Children) == 1 {
				expression = expression.Children[0]
			}
			if expression != nil && !expressionContainsMissing(expression) {
				typ := ValueType{}
				switch expression.Kind {
				case syntax.ExpressionBlob:
					typ.Name = "blob"
				case syntax.ExpressionLambda:
					typ.Name = "func"
				}
				if diagnostic, ok := stringConversionDiagnostic(typ, expression.Span); ok {
					result.Diagnostics = append(result.Diagnostics, diagnostic)
				}
			}
		}
		if command.Embedded != nil {
			collectOperatorDiagnostics(result, command.Embedded.Commands, scope)
		}
	}
}

func objectComparisonValue(result *FileAnalysis, scope *Scope, expression *syntax.Expression) bool {
	if result == nil || scope == nil || expression == nil {
		return false
	}
	candidate := expression
	for candidate.Kind == syntax.ExpressionParenthesized && len(candidate.Children) == 1 {
		candidate = candidate.Children[0]
	}
	if candidate.Kind == syntax.ExpressionIdentifier {
		if declaration := resolve(scope, candidate.Value, candidate.Span.Start, false, nil); declaration != nil &&
			(declaration.Kind == SymbolKindClass || declaration.Kind == SymbolKindEnum || declaration.Kind == SymbolKindTypeAlias) {
			return false
		}
	}
	typ := resolvedExpressionType(result, scope, expression)
	return typ.Name == "object" || result.classes[typ.Name] != nil
}

func objectCompoundAssignment(result *FileAnalysis, scope *Scope, expression *syntax.Expression) (*syntax.Expression, bool) {
	if result == nil || result.File == nil || scope == nil || expression == nil || expression.Kind != syntax.ExpressionAssignment || expression.Value == "=" || len(expression.Children) < 2 || !scopeUsesDefTypeRules(scope) {
		return nil, false
	}
	target := expression.Children[0]
	if target == nil || target.Kind != syntax.ExpressionIdentifier {
		return nil, false
	}
	declaration := resolve(scope, target.Value, target.Span.Start, false, nil)
	if declaration == nil || declaration.Kind != SymbolKindVariable && declaration.Kind != SymbolKindConstant {
		return nil, false
	}
	className := assignmentTargetType(result, scope, target).Name
	if localAggregates(result.File, syntax.BlockClass)[className] == nil || !declarationCanHoldObjectClass(result.File, declaration, className) {
		return nil, false
	}
	return target, true
}

func declarationCanHoldObjectClass(file *syntax.File, declaration *Declaration, className string) bool {
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Declaration == nil {
			continue
		}
		for _, binding := range command.Declaration.Bindings {
			if binding.Name != declaration.Span || binding.ParsedType == nil {
				continue
			}
			return convertSyntaxType(binding.ParsedType).Name == className
		}
	}
	return true
}

func expressionContainsInvalidPlus(result *FileAnalysis, expression *syntax.Expression) bool {
	if expression == nil {
		return false
	}
	if expression.Kind == syntax.ExpressionBinary && expression.Value == "+" && len(expression.Children) >= 2 {
		left, right := result.TypeOf(expression.Children[0]), result.TypeOf(expression.Children[1])
		return !isUnknownType(left) && !isUnknownType(right) &&
			(left.Name != "number" && left.Name != "float" || right.Name != "number" && right.Name != "float")
	}
	for _, child := range expression.Children {
		if expressionContainsInvalidPlus(result, child) {
			return true
		}
	}
	return false
}
