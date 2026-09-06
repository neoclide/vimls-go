package analysis

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
)

type builtinArgumentType struct {
	display           string
	kinds             builtinValueKind
	elementKind       builtinValueKind
	functionArguments []ValueType
	functionReturn    *ValueType
}

type builtinValueKind uint16

const (
	builtinNumber builtinValueKind = 1 << iota
	builtinFloat
	builtinString
	builtinBool
	builtinBlob
	builtinList
	builtinTuple
	builtinDict
	builtinFunc
	builtinChannel
	builtinJob
	builtinObject
	builtinSpecial
	builtinVoid
)

func builtinValueTypeKind(typ ValueType) builtinValueKind {
	switch valueTypeCategory(typ) {
	case "number":
		return builtinNumber
	case "float":
		return builtinFloat
	case "string":
		return builtinString
	case "bool":
		return builtinBool
	case "blob":
		return builtinBlob
	case "list":
		return builtinList
	case "tuple":
		return builtinTuple
	case "dict":
		return builtinDict
	case "func":
		return builtinFunc
	case "channel":
		return builtinChannel
	case "job":
		return builtinJob
	case "object":
		return builtinObject
	case "special":
		return builtinSpecial
	case "void":
		return builtinVoid
	default:
		return 0
	}
}

func builtinArgumentExpectation(checker string, actual []ValueType, index int) (builtinArgumentType, bool) {
	checker = strings.TrimSuffix(checker, "_mod")
	makeType := func(display string, kinds builtinValueKind) (builtinArgumentType, bool) {
		return builtinArgumentType{display: display, kinds: kinds}, true
	}
	switch checker {
	case "arg_number":
		return makeType("number", builtinNumber)
	case "arg_bool":
		return makeType("bool", builtinBool)
	case "arg_string":
		return makeType("string", builtinString)
	case "arg_blob":
		return makeType("blob", builtinBlob)
	case "arg_object":
		return makeType("object", builtinObject)
	case "arg_float_or_nr":
		return makeType("float or number", builtinFloat|builtinNumber)
	case "arg_buffer", "arg_lnum":
		return makeType("number or string", builtinNumber|builtinString)
	case "arg_buffer_or_dict_any":
		return makeType("number, string or dict", builtinNumber|builtinString|builtinDict)
	case "arg_list_any":
		return makeType("list", builtinList)
	case "arg_dict_any":
		return makeType("dict", builtinDict)
	case "arg_tuple_any":
		return makeType("tuple", builtinTuple)
	case "arg_job":
		return makeType("job", builtinJob)
	case "arg_chan_or_job":
		return makeType("channel or job", builtinChannel|builtinJob)
	case "arg_list_number":
		return builtinArgumentType{display: "list<number>", kinds: builtinList, elementKind: builtinNumber}, true
	case "arg_list_string":
		return builtinArgumentType{display: "list<string>", kinds: builtinList, elementKind: builtinString}, true
	case "arg_list_or_blob":
		return makeType("list or blob", builtinList|builtinBlob)
	case "arg_list_or_tuple":
		return makeType("list or tuple", builtinList|builtinTuple)
	case "arg_list_or_tuple_or_blob":
		return makeType("list, tuple or blob", builtinList|builtinTuple|builtinBlob)
	case "arg_list_or_tuple_or_dict":
		return makeType("list, tuple or dict", builtinList|builtinTuple|builtinDict)
	case "arg_list_or_dict_or_blob":
		return makeType("list, dict or blob", builtinList|builtinDict|builtinBlob)
	case "arg_list_or_dict_or_blob_or_string":
		return makeType("list, dict, blob or string", builtinList|builtinDict|builtinBlob|builtinString)
	case "arg_list_tuple_dict_blob_or_string":
		return makeType("list, tuple, dict, blob or string", builtinList|builtinTuple|builtinDict|builtinBlob|builtinString)
	case "arg_string_list_tuple_or_blob":
		return makeType("string, list, tuple or blob", builtinString|builtinList|builtinTuple|builtinBlob)
	case "arg_string_list_tuple_or_dict":
		return makeType("string, list, tuple or dict", builtinString|builtinList|builtinTuple|builtinDict)
	case "arg_string_or_blob":
		return makeType("string or blob", builtinString|builtinBlob)
	case "arg_string_or_nr":
		return makeType("string or number", builtinString|builtinNumber)
	case "arg_string_or_list_any":
		return makeType("string or list", builtinString|builtinList)
	case "arg_string_or_list_string":
		return builtinArgumentType{display: "string or list<string>", kinds: builtinString | builtinList, elementKind: builtinString}, true
	case "arg_string_or_dict_any", "arg_dict_any_or_string":
		return makeType("string or dict", builtinString|builtinDict)
	case "arg_str_or_nr_or_list":
		return makeType("string, number or list", builtinString|builtinNumber|builtinList)
	case "arg_string_or_func":
		return makeType("string or function", builtinString|builtinFunc|builtinBool|builtinNumber)
	case "arg_filter_func", "arg_map_func", "arg_foreach_func", "arg_sort_how":
		expected, _ := makeType("string or function", builtinString|builtinFunc)
		if len(actual) > 0 {
			expected.functionArguments, expected.functionReturn = builtinCallbackSignature(actual[0], checker)
		}
		return expected, true
	case "arg_bool_or_nr":
		return makeType("bool or number", builtinBool|builtinNumber)
	case "arg_bool_or_dict_any":
		return makeType("bool or dict", builtinBool|builtinDict)
	case "arg_reverse":
		return makeType("string, list, tuple or blob", builtinString|builtinList|builtinTuple|builtinBlob)
	case "arg_get1":
		return makeType("blob, list, tuple, dict or function", builtinBlob|builtinList|builtinTuple|builtinDict|builtinFunc)
	case "arg_len1":
		return makeType("string, number, blob, list, tuple, dict or object", builtinString|builtinNumber|builtinBlob|builtinList|builtinTuple|builtinDict|builtinObject)
	case "arg_repeat1":
		return makeType("string, number, blob, list or tuple", builtinString|builtinNumber|builtinBlob|builtinList|builtinTuple)
	case "arg_slice1":
		return makeType("string, blob, list or tuple", builtinString|builtinBlob|builtinList|builtinTuple)
	case "arg_cursor1":
		return makeType("number, string or list", builtinNumber|builtinString|builtinList)
	case "arg_same_as_prev", "arg_same_struct_as_prev":
		if index == 0 || index > len(actual)-1 || isUnknownType(actual[index-1]) {
			return builtinArgumentType{}, false
		}
		expected := actual[index-1]
		kind := builtinValueTypeKind(expected)
		if kind == 0 {
			return builtinArgumentType{}, false
		}
		result := builtinArgumentType{display: valueTypeDisplay(expected), kinds: kind}
		if checker == "arg_same_as_prev" && len(expected.Arguments) > 0 && !isUnknownType(expected.Arguments[0]) {
			result.elementKind = builtinValueTypeKind(expected.Arguments[0])
		}
		return result, true
	case "arg_item_of_prev":
		if index == 0 || index > len(actual)-1 {
			return builtinArgumentType{}, false
		}
		previous := actual[index-1]
		if previous.Name == "blob" {
			return makeType("number", builtinNumber)
		}
		if previous.Name != "list" || len(previous.Arguments) == 0 || isUnknownType(previous.Arguments[0]) {
			return builtinArgumentType{}, false
		}
		kind := builtinValueTypeKind(previous.Arguments[0])
		if kind == 0 {
			return builtinArgumentType{}, false
		}
		return makeType(valueTypeDisplay(previous.Arguments[0]), kind)
	case "arg_extend3":
		if index < 2 || len(actual) < 1 {
			return builtinArgumentType{}, false
		}
		switch actual[0].Name {
		case "list", "blob":
			return makeType("number", builtinNumber)
		case "dict":
			return makeType("string", builtinString)
		default:
			return builtinArgumentType{}, false
		}
	case "arg_remove2":
		if index == 0 || len(actual) < 1 {
			return builtinArgumentType{}, false
		}
		switch actual[0].Name {
		case "list", "blob":
			return makeType("number", builtinNumber)
		case "dict":
			return makeType("string or number", builtinString|builtinNumber)
		default:
			return builtinArgumentType{}, false
		}
	case "arg_any", "varargs_class":
		return builtinArgumentType{}, false
	default:
		return builtinArgumentType{}, false
	}
}

func builtinArgumentMismatch(actual ValueType, expected builtinArgumentType) bool {
	if isUnknownType(actual) {
		return false
	}
	kind := builtinValueTypeKind(actual)
	if kind == 0 {
		return false
	}
	if expected.kinds&kind == 0 {
		return true
	}
	if kind == builtinFunc && builtinFunctionSignatureMismatch(actual, expected) {
		return true
	}
	if expected.elementKind == 0 || actual.Name == "string" || len(actual.Arguments) == 0 || isUnknownType(actual.Arguments[0]) {
		return false
	}
	actualElement := builtinValueTypeKind(actual.Arguments[0])
	return actualElement != 0 && expected.elementKind&actualElement == 0
}

func builtinCallbackSignature(container ValueType, checker string) ([]ValueType, *ValueType) {
	index := ValueType{Name: "number"}
	item := UnknownValueType
	switch container.Name {
	case "list", "dict":
		if container.Name == "dict" {
			index = ValueType{Name: "string"}
		}
		if len(container.Arguments) > 0 {
			item = container.Arguments[0]
		}
	case "tuple":
		item = indexedType(container)
	case "string":
		item = ValueType{Name: "string"}
	case "blob":
		item = ValueType{Name: "number"}
	default:
		return nil, nil
	}
	if checker == "arg_sort_how" {
		result := ValueType{Name: "number"}
		return []ValueType{item, item}, &result
	}
	var result *ValueType
	switch checker {
	case "arg_filter_func":
		value := ValueType{Name: "bool"}
		result = &value
	case "arg_map_func":
		value := item
		result = &value
	}
	return []ValueType{index, item}, result
}

func builtinFunctionSignatureMismatch(actual ValueType, expected builtinArgumentType) bool {
	if len(expected.functionArguments) == 0 || !actual.ArgumentCountKnown || actual.Variadic {
		return false
	}
	if len(actual.Arguments) != len(expected.functionArguments) {
		return true
	}
	for index := range expected.functionArguments {
		if !compatibleTypes(actual.Arguments[index], expected.functionArguments[index]) {
			return true
		}
	}
	return expected.functionReturn != nil && actual.Return != nil && !compatibleTypes(*expected.functionReturn, *actual.Return)
}

func builtinCallbackParametersMatch(actual ValueType, expected builtinArgumentType) bool {
	if actual.Name != "func" || !actual.ArgumentCountKnown || actual.Variadic || len(actual.Arguments) != len(expected.functionArguments) {
		return false
	}
	for index := range expected.functionArguments {
		if !compatibleTypes(actual.Arguments[index], expected.functionArguments[index]) {
			return false
		}
	}
	return true
}

func valueTypeDisplay(typ ValueType) string {
	if isUnknownType(typ) {
		return "any"
	}
	if len(typ.Arguments) == 0 {
		return typ.Name
	}
	arguments := make([]string, 0, len(typ.Arguments))
	for _, argument := range typ.Arguments {
		arguments = append(arguments, valueTypeDisplay(argument))
	}
	return typ.Name + "<" + strings.Join(arguments, ", ") + ">"
}

func builtinCallArguments(file *syntax.File, call *syntax.Expression) (vimdata.BuiltinFunction, []*syntax.Expression, bool) {
	if file == nil || call == nil || call.Kind != syntax.ExpressionCall || len(call.Children) == 0 {
		return vimdata.BuiltinFunction{}, nil, false
	}
	callee := call.Children[0]
	var name string
	var receiver *syntax.Expression
	var explicit []*syntax.Expression
	switch {
	case call.Value == "" && callee != nil && callee.Kind == syntax.ExpressionIdentifier:
		name = callee.Value
		explicit = call.Children[1:]
	case call.Value == "" && callee != nil && callee.Kind == syntax.ExpressionMember && len(callee.Children) == 1 && file.Text(callee.Operator) == "->":
		name = callee.Value
		receiver = callee.Children[0]
		explicit = call.Children[1:]
	case call.Value == "->" && callee != nil && callee.Kind == syntax.ExpressionIdentifier && len(call.Children) >= 2:
		name = callee.Value
		receiver = call.Children[1]
		explicit = call.Children[2:]
	default:
		return vimdata.BuiltinFunction{}, nil, false
	}
	if strings.Contains(name, ":") {
		return vimdata.BuiltinFunction{}, nil, false
	}
	builtin, ok := vimdata.LookupFunction(name)
	if !ok {
		return vimdata.BuiltinFunction{}, nil, false
	}
	if receiver == nil {
		return builtin, explicit, true
	}
	if builtin.MethodArgument == 0 {
		return vimdata.BuiltinFunction{}, nil, false
	}
	receiverIndex := builtin.MethodArgument - 1
	if receiverIndex > len(explicit) {
		return vimdata.BuiltinFunction{}, nil, false
	}
	arguments := make([]*syntax.Expression, 0, len(explicit)+1)
	arguments = append(arguments, explicit[:receiverIndex]...)
	arguments = append(arguments, receiver)
	arguments = append(arguments, explicit[receiverIndex:]...)
	return builtin, arguments, true
}

func emptyRequiredStringDiagnostic(function vimdata.BuiltinFunction, arguments []*syntax.Expression, dialect syntax.Dialect) (syntax.Diagnostic, bool) {
	var indexes []int
	switch function.Name {
	case "bindtextdomain":
		indexes = []int{0, 1}
	case "gettext":
		indexes = []int{0}
	case "ngettext":
		indexes = []int{0, 1}
	case "exepath", "exists", "finddir", "findfile", "mkdir", "readfile":
		if dialect != syntax.Vim9 {
			return syntax.Diagnostic{}, false
		}
		indexes = []int{0}
	default:
		return syntax.Diagnostic{}, false
	}
	for _, index := range indexes {
		if index >= len(arguments) {
			continue
		}
		literal := arguments[index]
		for literal != nil && literal.Kind == syntax.ExpressionParenthesized && len(literal.Children) == 1 {
			literal = literal.Children[0]
		}
		if literal != nil && literal.Kind == syntax.ExpressionString && (literal.Value == "''" || literal.Value == `""`) {
			return syntax.Diagnostic{
				Code: "vim/E1175", Message: "Non-empty string required for argument " + strconv.Itoa(index+1), Span: literal.Span,
			}, true
		}
	}
	return syntax.Diagnostic{}, false
}

func validScopeVariableName(name string) bool {
	if name == "" {
		return false
	}
	for index := 0; index < len(name); index++ {
		character := name[index]
		if character >= utf8.RuneSelf {
			continue
		}
		letter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		if letter || character == '_' || index > 0 && (character >= '0' && character <= '9' || character == '#') {
			continue
		}
		return false
	}
	return true
}

func scopeDictionary(expression *syntax.Expression) bool {
	for expression != nil && expression.Kind == syntax.ExpressionParenthesized && len(expression.Children) == 1 {
		expression = expression.Children[0]
	}
	return expression != nil && expression.Kind == syntax.ExpressionIdentifier && len(expression.Value) == 2 &&
		expression.Value[1] == ':' && strings.ContainsRune("gslabwtv", rune(expression.Value[0]))
}

func illegalVariableNameBuiltinDiagnostic(function vimdata.BuiltinFunction, arguments []*syntax.Expression, dialect syntax.Dialect) (syntax.Diagnostic, bool) {
	nameIndex := -1
	switch function.Name {
	case "setbufvar", "settabvar", "setwinvar":
		nameIndex = 1
	case "settabwinvar":
		nameIndex = 2
	}
	if nameIndex >= 0 && nameIndex < len(arguments) {
		if name, ok := syntax.StaticDictionaryIndexKey(arguments[nameIndex]); ok && !strings.HasPrefix(name, "&") && !validScopeVariableName(name) {
			return syntax.Diagnostic{Code: "vim/E461", Message: "Illegal variable name: " + name, Span: arguments[nameIndex].Span}, true
		}
	}
	if function.Name != "extend" || len(arguments) < 2 || !scopeDictionary(arguments[0]) || arguments[1] == nil || arguments[1].Kind != syntax.ExpressionDictionary {
		return syntax.Diagnostic{}, false
	}
	for index := 0; index+1 < len(arguments[1].Children); index += 2 {
		keyExpression := arguments[1].Children[index]
		key, ok := syntax.StaticDictionaryKey(keyExpression, dialect)
		if ok && !validScopeVariableName(key) {
			return syntax.Diagnostic{Code: "vim/E461", Message: "Illegal variable name: " + key, Span: keyExpression.Span}, true
		}
	}
	return syntax.Diagnostic{}, false
}

func collectBuiltinArgumentTypeDiagnostics(result *FileAnalysis, commands []syntax.Command, parent *Scope) {
	seen := make(map[*syntax.Expression]bool)
	var walkCommands func([]syntax.Command, *Scope)
	var walk func(*syntax.Expression, *Scope, syntax.Dialect)
	walk = func(expression *syntax.Expression, scope *Scope, dialect syntax.Dialect) {
		if expression == nil || seen[expression] {
			return
		}
		seen[expression] = true
		if expression.Kind == syntax.ExpressionCall && !expressionContainsMissing(expression) && !syntaxDiagnosticTouchesCall(result.File.Diagnostics, expression.Span) {
			builtin, arguments, builtinCall := builtinCallArguments(result.File, expression)
			callee := (*syntax.Expression)(nil)
			if len(expression.Children) > 0 {
				callee = expression.Children[0]
			}
			callValueError := false
			if builtinCall {
				if diagnostic, ok := illegalVariableNameBuiltinDiagnostic(builtin, arguments, dialect); ok {
					result.Diagnostics = append(result.Diagnostics, diagnostic)
					callValueError = true
				} else if diagnostic, ok := emptyRequiredStringDiagnostic(builtin, arguments, dialect); ok {
					result.Diagnostics = append(result.Diagnostics, diagnostic)
					callValueError = true
				}
			}
			sortFloatFuncref := false
			if builtinCall && builtin.Name == "sort" && len(arguments) >= 2 {
				list, how := arguments[0], arguments[1]
				for list != nil && list.Kind == syntax.ExpressionParenthesized && len(list.Children) == 1 {
					list = list.Children[0]
				}
				for how != nil && how.Kind == syntax.ExpressionParenthesized && len(how.Children) == 1 {
					how = how.Children[0]
				}
				if list != nil && list.Kind == syntax.ExpressionList && how != nil && how.Kind == syntax.ExpressionString && simpleVimStringLiteral(how.Value) == "f" {
					for _, item := range list.Children {
						candidate := item
						for candidate != nil && candidate.Kind == syntax.ExpressionParenthesized && len(candidate.Children) == 1 {
							candidate = candidate.Children[0]
						}
						directFuncref := candidate != nil && candidate.Kind == syntax.ExpressionLambda
						if candidate != nil && candidate.Kind == syntax.ExpressionCall && len(candidate.Children) > 1 {
							callee, nameExpression := candidate.Children[0], candidate.Children[1]
							if callee != nil && callee.Kind == syntax.ExpressionIdentifier && (callee.Value == "function" || callee.Value == "funcref") &&
								nameExpression != nil && nameExpression.Kind == syntax.ExpressionString {
								name := simpleVimStringLiteral(nameExpression.Value)
								builtin := vimdata.IsFunction(name)
								declaration := resolve(scope, name, nameExpression.Span.Start, true, nil)
								directFuncref = name != "" && (builtin || declaration != nil && functionSymbolKind(declaration.Kind))
							}
						}
						actual := result.TypeOf(item)
						if directFuncref && (actual.Name == "func" || actual.Name == "partial") {
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
								Code: "vim/E891", Message: "Using a Funcref as a Float", Span: item.Span,
							})
							sortFloatFuncref = true
							break
						}
					}
				}
			}
			if callValueError {
				// The value error owns the call before ordinary type checks.
			} else if sortFloatFuncref {
				// The item conversion error owns the call.
			} else if builtinCall && dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope) && builtin.Name == "exists_compiled" {
				explicit := arguments
				switch {
				case expression.Value == "" && callee != nil && callee.Kind == syntax.ExpressionMember:
					explicit = expression.Children[1:]
				case expression.Value == "->" && callee != nil && callee.Kind == syntax.ExpressionIdentifier && len(expression.Children) >= 2:
					explicit = expression.Children[2:]
				}
				valid := len(explicit) == 1 && explicit[0] != nil && explicit[0].Kind == syntax.ExpressionString
				if !valid {
					_, span := functionDiagnosticTarget(result.File, callee)
					if len(explicit) == 1 && explicit[0] != nil {
						span = explicit[0].Span
					} else if len(explicit) > 1 && explicit[1] != nil {
						span = explicit[1].Span
					}
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1232", Message: "Argument of exists_compiled() must be a literal string", Span: span})
				}
			} else if builtinCall && builtin.Name == "exists_compiled" && len(arguments) == 1 {
				// Outside compiled Vim9 code exists_compiled() reaches its runtime
				// builtin implementation, which rejects the otherwise valid arity.
				_, span := functionDiagnosticTarget(result.File, callee)
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1233", Message: "exists_compiled() can only be used in a :def function", Span: span})
			} else if builtinCall && dialect == syntax.Vim9 && builtin.Name == "flatten" {
				// E1158 is emitted while walking the call and owns this builtin
				// before its ordinary arity and argument-type checks.
			} else if builtinCall && (dialect == syntax.Vim9 || builtin.Name == "len") {
				actual := make([]ValueType, len(arguments))
				for index, argument := range arguments {
					actual[index] = result.TypeOf(argument)
				}
				constMutation := false
				extendMismatch := -1
				if builtin.Name == "extend" || builtin.Name == "extendnew" {
					extendMismatch = extendArgumentMismatchIndex(actual)
				}
				for index, argument := range arguments {
					checkerIndex := index
					if checkerIndex >= len(builtin.ArgumentChecks) {
						if builtin.MaxArgs < 0 && len(builtin.ArgumentChecks) > 0 {
							checkerIndex = len(builtin.ArgumentChecks) - 1
						} else {
							break
						}
					}
					checker := builtin.ArgumentChecks[checkerIndex]
					expected, ok := builtinArgumentExpectation(checker, actual, index)
					if !ok {
						continue
					}
					if checker == "arg_map_func" && !scopeContainsDef(scope) {
						expected.functionReturn = nil
					} else if checker == "arg_map_func" && len(arguments) > 0 {
						constraint := containerDeclaredType(result, arguments[0])
						if (constraint.Name == "list" || constraint.Name == "dict") && len(constraint.Arguments) > 0 {
							expected.functionReturn = &constraint.Arguments[0]
						}
					}
					if builtin.Name == "digraph_setlist" && index == 0 && dialect == syntax.Vim9 {
						invalid, knownList := digraphSetlistArgumentInvalid(result, argument, actual[index])
						if invalid {
							if !scopeUsesDefTypeRules(scope) || knownList {
								result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1216", Message: "digraph_setlist() argument must be a list of lists with two items", Span: argument.Span})
								continue
							}
						}
						if knownList {
							continue
						}
					}
					if scopeContainsDef(scope) && callbackCheckerUsesE176(checker) && builtinCallbackArgumentCountInvalid(actual[index], expected) {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E176", Message: "Invalid number of arguments", Span: argument.Span})
						continue
					}
					if result.File.Dialect == syntax.Vim9 && dialect == syntax.Vim9 && !scopeUsesDefTypeRules(scope) && (builtin.Name == "map" || builtin.Name == "filter" || builtin.Name == "foreach") &&
						argument.Kind == syntax.ExpressionLambda && actual[index].Name == "func" && actual[index].ArgumentCountKnown && len(expected.functionArguments) == 2 && actual[index].RequiredArguments > 2 {
						difference := actual[index].RequiredArguments - 2
						message := "One argument too few"
						if difference > 1 {
							message = strconv.Itoa(difference) + " arguments too few"
						}
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1190", Message: message, Span: argument.Span})
						continue
					}
					if dialect == syntax.Vim9 && !scopeUsesDefTypeRules(scope) && (builtin.Name == "map" || builtin.Name == "filter" || builtin.Name == "foreach") &&
						argument.Kind == syntax.ExpressionLambda && actual[index].Name == "func" && actual[index].ArgumentCountKnown && !actual[index].Variadic &&
						len(actual[index].Arguments) < len(expected.functionArguments) {
						difference := len(expected.functionArguments) - len(actual[index].Arguments)
						message := "One argument too many"
						if difference > 1 {
							message = strconv.Itoa(difference) + " arguments too many"
						}
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1106", Message: message, Span: argument.Span})
						continue
					}
					if builtinArgumentMismatch(actual[index], expected) {
						if dialect == syntax.Vim9 && strings.TrimSuffix(checker, "_mod") == "arg_bool" && actual[index].Name == "number" {
							if value, ok := staticNumberValue(argument); ok && (value == 0 || value == 1) {
								continue
							} else if !ok && !scopeUsesDefTypeRules(scope) {
								continue
							}
						}
						if extendMismatch >= 0 && (!scopeUsesDefTypeRules(scope) || index != extendMismatch) {
							continue
						}
						// At script level sign_undefine() converts each list item
						// to a sign name and consults Vim's mutable sign registry.
						// A def keeps the strict list<string> compile-time check.
						if builtin.Name == "sign_undefine" && index == 0 && actual[index].Name == "list" && !scopeUsesDefTypeRules(scope) {
							continue
						}
						trimmedChecker := strings.TrimSuffix(checker, "_mod")
						compiledCallbackChecker := scopeUsesDefTypeRules(scope) &&
							(trimmedChecker == "arg_filter_func" || trimmedChecker == "arg_map_func" || trimmedChecker == "arg_foreach_func" || trimmedChecker == "arg_sort_how")
						runtimeCallbackChecker := !scopeUsesDefTypeRules(scope) && dialect == syntax.Vim9 &&
							((trimmedChecker == "arg_sort_how" && (builtin.Name == "sort" || builtin.Name == "uniq")) ||
								trimmedChecker == "arg_filter_func" && builtin.Name == "indexof")
						if (compiledCallbackChecker || runtimeCallbackChecker) && !isUnknownType(actual[index]) && actual[index].Name != "string" && actual[index].Name != "func" && actual[index].Name != "partial" {
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1256", Message: "String or function required for argument " + strconv.Itoa(index+1), Span: argument.Span})
							continue
						}
						// Vim9 script compilation reports the historical E118 when
						// indexof() will pass more arguments than a statically known
						// callback accepts. A def uses E176 for the same mismatch, so
						// keep that context on its separate diagnostic path.
						if builtin.Name == "indexof" && index == 1 && !scopeContainsDef(scope) && callbackReceivesTooManyArguments(actual[index], expected) {
							name, span := functionDiagnosticTarget(result.File, argument)
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E118", Message: "Too many arguments for function: " + name, Span: span})
							continue
						}
						// indexof() requires a predicate returning bool.  A known
						// void function is a value-use error at the callback itself,
						// rather than a generic callback signature mismatch.  Keep
						// def-local callback checks on their existing E1013 path.
						if builtin.Name == "indexof" && index == 1 && !scopeContainsDef(scope) && actual[index].Name == "func" && actual[index].Return != nil && actual[index].Return.Name == "void" {
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1031", Message: "Cannot use void value", Span: argument.Span})
							continue
						}
						if dialect == syntax.Vim9 && !scopeUsesDefTypeRules(scope) && (builtin.Name == "filter" || builtin.Name == "indexof") && checker == "arg_filter_func" &&
							actual[index].Return != nil && actual[index].Return.Name == "string" && builtinCallbackParametersMatch(actual[index], expected) {
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1135", Message: "Using a String as a Bool", Span: argument.Span})
							continue
						}
						if dialect == syntax.Vim9 && !scopeUsesDefTypeRules(scope) && builtin.Name == "sort" && checker == "arg_sort_how" &&
							actual[index].Return != nil && actual[index].Return.Name == "bool" && builtinCallbackParametersMatch(actual[index], expected) {
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1138", Message: "Using a Bool as a Number", Span: argument.Span})
							continue
						}
						if !scopeUsesDefTypeRules(scope) && index == 1 && actual[index].Name == "number" && (builtin.Name == "filter" || builtin.Name == "map") && len(actual) > 0 {
							switch actual[0].Name {
							case "list", "dict", "blob", "string":
								result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1024", Message: "Using a Number as a String", Span: argument.Span})
								continue
							}
						}
						if !scopeUsesDefTypeRules(scope) && strings.TrimSuffix(checker, "_mod") == "arg_string_or_func" && actual[index].Name == "list" {
							diagnostic, _ := stringConversionDiagnostic(actual[index], argument.Span)
							result.Diagnostics = append(result.Diagnostics, diagnostic)
							continue
						}
						if !scopeUsesDefTypeRules(scope) && expected.kinds == builtinNumber && actual[index].Name == "float" {
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
								Code: "vim/E805", Message: "Using a Float as a Number", Span: argument.Span,
							})
							continue
						}
						if !scopeUsesDefTypeRules(scope) && trimmedChecker == "arg_list_or_dict_or_blob_or_string" && actual[index].Name == "tuple" {
							continue
						}
						diagnosticSpan := argument.Span
						if actual[index].Name == "void" {
							if methodSpan, ok := methodReceiverDiagnosticSpan(result.File, expression, argument); ok {
								diagnosticSpan = methodSpan
							}
						}
						if !scopeUsesDefTypeRules(scope) || trimmedChecker == "arg_string_list_tuple_or_dict" || trimmedChecker == "arg_list_tuple_dict_blob_or_string" {
							if diagnostic, ok := builtinArgumentDiagnostic(checker, index, actual, diagnosticSpan, dialect == syntax.Vim9); ok {
								result.Diagnostics = append(result.Diagnostics, diagnostic)
								continue
							}
						}
						if expressionContainsInvalidPlus(result, argument) {
							continue
						}
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1013", Message: "Argument " + strconv.Itoa(index+1) + ": type mismatch, expected " + expected.display + " but got " + valueTypeDisplay(actual[index]), Span: diagnosticSpan})
						continue
					}
					if dialect == syntax.Vim9 && !scopeUsesDefTypeRules(scope) && strings.TrimSuffix(checker, "_mod") == "arg_bool_or_nr" && actual[index].Name == "number" {
						if diagnostic, ok := numberAsBoolDiagnostic(argument); ok {
							result.Diagnostics = append(result.Diagnostics, diagnostic)
							continue
						}
					}
					if scopeUsesDefTypeRules(scope) && index == 0 && !isUnknownType(actual[index]) && (strings.HasSuffix(checker, "_mod") || checker == "arg_reverse") {
						candidate := argument
						for candidate.Kind == syntax.ExpressionParenthesized && len(candidate.Children) == 1 {
							candidate = candidate.Children[0]
						}
						if candidate.Kind == syntax.ExpressionIdentifier {
							declaration := resolve(scope, candidate.Value, candidate.Span.Start, false, nil)
							if declaration != nil && declaration.constBinding {
								result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1307", Message: "Argument " + strconv.Itoa(index+1) + ": Trying to modify a const " + valueTypeDisplay(actual[index]), Span: argument.Span})
								constMutation = true
								break
							}
						}
					}
				}
				if !constMutation {
					collectMapCallbackReturnTypeDiagnostic(result, scope, builtin, arguments, actual)
					collectSearchpairFlagsDiagnostic(result, scope, builtin, arguments)
					collectSubstituteExpressionDiagnostic(result, builtin, arguments)
					collectBuiltinCompiledStringDiagnostics(result, scope, builtin, arguments, expression.Span.Start)
				}
			} else if !builtinCall {
				collectFunctionCallDiagnostics(result, scope, expression, dialect == syntax.Vim9)
			}
		}
		if expression.Kind == syntax.ExpressionLambda {
			lambdaScope := result.lambdaScopes[expression]
			if lambdaScope == nil {
				lambdaScope = scope
			}
			if expression.LambdaBody != nil {
				walkCommands(expression.LambdaBody.Commands, lambdaScope)
			}
			for index, child := range expression.Children {
				if index >= len(expression.Parameters) {
					walk(child, lambdaScope, dialect)
				}
			}
			return
		}
		for _, child := range expression.Children {
			walk(child, scope, dialect)
		}
	}
	walkCommands = func(list []syntax.Command, fallback *Scope) {
		for index := range list {
			command := &list[index]
			scope := result.commandScopes[command]
			if scope == nil {
				scope = fallback
			}
			for _, expression := range command.Expressions {
				walk(expression, scope, command.Dialect)
			}
			if command.Mapping != nil {
				walk(command.Mapping.RHSExpression, scope, command.Dialect)
			}
			for _, expression := range command.Targets {
				walk(expression, scope, command.Dialect)
			}
			if command.Declaration != nil {
				walk(command.Declaration.Initializer, scope, command.Dialect)
			}
			if command.For != nil {
				walk(command.For.Iterable, scope, command.Dialect)
			}
			if command.Import != nil {
				walk(command.Import.Path, scope, command.Dialect)
			}
			if command.Embedded != nil {
				walkCommands(command.Embedded.Commands, scope)
			}
		}
	}
	walkCommands(commands, parent)
}

func methodReceiverDiagnosticSpan(file *syntax.File, call, receiver *syntax.Expression) (syntax.Span, bool) {
	if file == nil || call == nil || receiver == nil || len(call.Children) == 0 || call.Children[0] == nil {
		return syntax.Span{}, false
	}
	callee := call.Children[0]
	if call.Value == "->" && len(call.Children) >= 2 && call.Children[1] == receiver && callee.Kind == syntax.ExpressionIdentifier {
		return syntax.Span{Start: call.Operator.Start, End: callee.Span.End}, true
	}
	if call.Value == "" && callee.Kind == syntax.ExpressionMember && file.Text(callee.Operator) == "->" && len(callee.Children) == 1 && callee.Children[0] == receiver {
		return syntax.Span{Start: callee.Operator.Start, End: callee.Span.End}, true
	}
	return syntax.Span{}, false
}

func digraphSetlistArgumentInvalid(result *FileAnalysis, expression *syntax.Expression, actual ValueType) (bool, bool) {
	for expression != nil && expression.Kind == syntax.ExpressionParenthesized && len(expression.Children) == 1 {
		expression = expression.Children[0]
	}
	if expression == nil || isUnknownType(actual) {
		return false, false
	}
	if expression.Kind == syntax.ExpressionIdentifier && expression.Value == "null_list" {
		return false, true
	}
	if actual.Name != "list" {
		return true, false
	}
	if expression.Kind != syntax.ExpressionList {
		return false, true
	}
	for _, item := range expression.Children {
		candidate := item
		for candidate != nil && candidate.Kind == syntax.ExpressionParenthesized && len(candidate.Children) == 1 {
			candidate = candidate.Children[0]
		}
		if candidate == nil {
			return false, true
		}
		if candidate.Kind == syntax.ExpressionIdentifier && candidate.Value == "null_list" {
			return true, true
		}
		if candidate.Kind == syntax.ExpressionList {
			if len(candidate.Children) != 2 {
				return true, true
			}
			continue
		}
		candidateType := result.TypeOf(candidate)
		if !isUnknownType(candidateType) && candidateType.Name != "list" {
			return true, true
		}
	}
	return false, true
}

// builtinArgumentDiagnostic mirrors the concrete Vim compile-time checker
// errors for the simple argument checkers whose native diagnostic is useful to
// callers. E1013 remains the general static type mismatch for checkers without
// a specialized native diagnostic.
func builtinArgumentDiagnostic(checker string, index int, actual []ValueType, span syntax.Span, vim9 bool) (syntax.Diagnostic, bool) {
	checker = strings.TrimSuffix(checker, "_mod")
	var code, required string
	switch checker {
	case "arg_bool":
		if vim9 {
			code, required = "vim/E1212", "Bool"
		}
	case "arg_bool_or_nr":
		if vim9 {
			code, required = "vim/E1235", "Bool or Number"
		}
	case "arg_blob":
		if vim9 {
			code, required = "vim/E1238", "Blob"
		}
	case "arg_list_any":
		if vim9 {
			code, required = "vim/E1211", "List"
		}
	case "arg_list_number", "arg_list_string":
		if vim9 && index >= 0 && index < len(actual) && actual[index].Name != "list" {
			code, required = "vim/E1211", "List"
		}
	case "arg_slice1":
		if vim9 {
			code, required = "vim/E1211", "List"
		}
	case "arg_number":
		if vim9 {
			code, required = "vim/E1210", "Number"
		}
	case "arg_chan_or_job":
		if vim9 {
			code, required = "vim/E1217", "Channel or Job"
		}
	case "arg_job":
		if vim9 {
			code, required = "vim/E1218", "Job"
		}
	case "arg_float_or_nr":
		if vim9 {
			code, required = "vim/E1219", "Float or Number"
		}
	case "arg_string_or_nr", "arg_buffer", "arg_lnum":
		if vim9 {
			code, required = "vim/E1220", "String or Number"
		}
	case "arg_string_or_blob":
		if vim9 {
			code, required = "vim/E1221", "String or Blob"
		}
	case "arg_string_or_list_any":
		if vim9 {
			code, required = "vim/E1222", "String or List"
		}
	case "arg_string_or_list_string":
		if vim9 && index >= 0 && index < len(actual) && actual[index].Name != "string" && actual[index].Name != "list" {
			code, required = "vim/E1222", "String or List"
		}
	case "arg_string_or_dict_any", "arg_dict_any_or_string":
		if vim9 {
			code, required = "vim/E1223", "String or Dictionary"
		}
	case "arg_str_or_nr_or_list", "arg_cursor1":
		if vim9 {
			code, required = "vim/E1224", "String, Number or List"
		}
	case "arg_string_list_tuple_or_dict":
		if vim9 {
			code, required = "vim/E1225", "String, List, Tuple or Dictionary"
		}
	case "arg_list_or_blob":
		if vim9 {
			code, required = "vim/E1226", "List or Blob"
		}
	case "arg_list_or_dict_or_blob":
		if vim9 {
			code, required = "vim/E1228", "List, Dictionary or Blob"
		}
	case "arg_list_or_dict_or_blob_or_string":
		if vim9 && index >= 0 && index < len(actual) && actual[index].Name != "tuple" {
			code, required = "vim/E1251", "List, Tuple, Dictionary, Blob or String"
		}
	case "arg_list_tuple_dict_blob_or_string":
		if vim9 {
			code, required = "vim/E1251", "List, Tuple, Dictionary, Blob or String"
		}
	case "arg_string_list_tuple_or_blob", "arg_reverse":
		if vim9 {
			code, required = "vim/E1253", "String, List, Tuple or Blob"
		}
	case "arg_repeat1":
		if vim9 {
			code, required = "vim/E1301", "String, Number, List, Tuple or Blob"
		}
	case "arg_item_of_prev":
		if vim9 && index > 0 && index <= len(actual)-1 && actual[index-1].Name == "blob" {
			code, required = "vim/E1210", "Number"
		}
	case "arg_remove2":
		if vim9 && index > 0 && len(actual) > 0 {
			switch actual[0].Name {
			case "list", "blob":
				code, required = "vim/E1210", "Number"
			case "dict":
				code, required = "vim/E1220", "String or Number"
			}
		}
	case "arg_len1":
		return syntax.Diagnostic{Code: "vim/E701", Message: "Invalid type for len()", Span: span}, true
	case "arg_string":
		code, required = "vim/E1174", "String"
	case "arg_dict_any":
		code, required = "vim/E1206", "Dictionary"
	case "arg_list_or_tuple_or_blob":
		code, required = "vim/E1528", "List or Tuple or Blob"
	case "arg_list_or_tuple":
		code, required = "vim/E1529", "List or Tuple"
	case "arg_list_or_tuple_or_dict":
		code, required = "vim/E1530", "List or Tuple or Dictionary"
	case "arg_get1":
		return syntax.Diagnostic{
			Code: "vim/E1531", Message: "Argument of get() must be a List, Tuple, Dictionary or Blob", Span: span,
		}, true
	default:
		return syntax.Diagnostic{}, false
	}
	if code == "" {
		return syntax.Diagnostic{}, false
	}
	return syntax.Diagnostic{
		Code: code, Message: required + " required for argument " + strconv.Itoa(index+1), Span: span,
	}, true
}

// Vim keeps a container's current element type separate from the declared
// mutation constraint (evalfunc.c type2_T). Fresh literals and selected builtin
// results can change element type in map(); binding a typed variable cannot.
func containerDeclaredType(result *FileAnalysis, expression *syntax.Expression) ValueType {
	if expression == nil {
		return UnknownValueType
	}
	for expression.Kind == syntax.ExpressionParenthesized && len(expression.Children) == 1 {
		expression = expression.Children[0]
	}
	current := result.TypeOf(expression)
	if current.Name != "list" && current.Name != "dict" {
		return current
	}
	relaxed := false
	switch expression.Kind {
	case syntax.ExpressionList, syntax.ExpressionDictionary, syntax.ExpressionSlice:
		relaxed = true
	case syntax.ExpressionCall:
		if builtin, arguments, ok := builtinCallArguments(result.File, expression); ok {
			switch builtin.ReturnHelper {
			case "ret_first_arg", "ret_extend":
				if len(arguments) > 0 {
					return containerDeclaredType(result, arguments[0])
				}
			case "ret_copy", "ret_slice", "ret_first_cont", "ret_list_number", "ret_list_string", "ret_list_dict_any", "ret_list_items", "ret_list_string_items", "ret_list_regionpos", "ret_getline", "ret_job_info":
				relaxed = true
			}
		}
	}
	if relaxed {
		current.Arguments = []ValueType{UnknownValueType}
	}
	return current
}

func collectMapCallbackReturnTypeDiagnostic(result *FileAnalysis, scope *Scope, builtin vimdata.BuiltinFunction, arguments []*syntax.Expression, actual []ValueType) {
	if result == nil || scopeContainsDef(scope) || builtin.Name != "map" || len(arguments) < 2 || len(actual) < 2 {
		return
	}
	container, callback := containerDeclaredType(result, arguments[0]), actual[1]
	if (container.Name != "list" && container.Name != "dict") || len(container.Arguments) == 0 || callback.Name != "func" || callback.Return == nil {
		return
	}
	element := container.Arguments[0]
	if isUnknownType(element) || isUnknownType(*callback.Return) {
		return
	}
	expectedArguments, _ := builtinCallbackSignature(container, "arg_map_func")
	if builtinFunctionSignatureMismatch(callback, builtinArgumentType{functionArguments: expectedArguments}) || assignmentTypesCompatible(element, *callback.Return) {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1012", Message: "Type mismatch; expected " + valueTypeDisplay(element) + " but got " + valueTypeDisplay(*callback.Return), Span: arguments[1].Span,
	})
}

func collectSearchpairFlagsDiagnostic(result *FileAnalysis, scope *Scope, function vimdata.BuiltinFunction, arguments []*syntax.Expression) {
	if result == nil || scope != result.Root || function.Name != "searchpair" && function.Name != "searchpairpos" || len(arguments) < 4 {
		return
	}
	literal := arguments[3]
	if literal == nil || literal.Kind != syntax.ExpressionString || len(literal.Value) < 2 {
		return
	}
	quote := literal.Value[0]
	if literal.Value[len(literal.Value)-1] != quote || quote != '\'' && quote != '"' {
		return
	}
	flags := literal.Value[1 : len(literal.Value)-1]
	if quote == '"' && strings.Contains(flags, "\\") || quote == '\'' && strings.Contains(flags, "''") {
		return
	}
	nomove, setmark, invalid := false, false, false
	for index := 0; index < len(flags); index++ {
		switch flags[index] {
		case 'b', 'c', 'm', 'r', 'w', 'W', 'z':
		case 'n':
			nomove = true
		case 's':
			setmark = true
		default:
			invalid = true
		}
	}
	if !invalid && !(nomove && setmark) {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E475", Message: "Invalid argument: " + flags, Span: literal.Span,
	})
}

func collectSubstituteExpressionDiagnostic(result *FileAnalysis, function vimdata.BuiltinFunction, arguments []*syntax.Expression) {
	if result == nil || result.File == nil || function.Name != "substitute" || len(arguments) < 3 {
		return
	}
	literal := arguments[2]
	if literal == nil || literal.Kind != syntax.ExpressionString || len(literal.Value) < 4 || literal.Value[0] != '\'' || literal.Value[len(literal.Value)-1] != '\'' {
		return
	}
	content := literal.Value[1 : len(literal.Value)-1]
	if !strings.HasPrefix(content, `\=`) || strings.Contains(content, "''") {
		return
	}
	_, diagnostics := (syntax.Vim9ExpressionParser{}).Parse(content[2:])
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != "vimls/trailing-expression" {
			continue
		}
		base := literal.Span.Start + 3
		span := syntax.Span{Start: base + diagnostic.Span.Start, End: base + diagnostic.Span.End}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E488", Message: "Trailing characters: " + result.File.Text(span), Span: span,
		})
		return
	}
}

func collectBuiltinCompiledStringDiagnostics(result *FileAnalysis, scope *Scope, function vimdata.BuiltinFunction, arguments []*syntax.Expression, visibilityOffset int) {
	if result == nil || result.File == nil || scope == nil || !scopeContainsDef(scope) || function.Name != "searchpair" && function.Name != "searchpairpos" || len(arguments) < 5 {
		return
	}
	literal := arguments[4]
	if literal == nil || literal.Kind != syntax.ExpressionString || len(literal.Value) < 2 {
		return
	}
	quote := literal.Value[0]
	if literal.Value[len(literal.Value)-1] != quote || quote != '\'' && quote != '"' {
		return
	}
	content := literal.Value[1 : len(literal.Value)-1]
	if content == "" || quote == '"' && strings.Contains(content, "\\") || quote == '\'' && strings.Contains(content, "''") {
		return
	}
	expression, diagnostics := (syntax.Vim9ExpressionParser{}).Parse(content)
	if expression == nil || len(diagnostics) != 0 || expressionTreeContainsLambda(expression) {
		return
	}
	walkCompiledStringExpression(result, scope, expression, literal.Span.Start+1, visibilityOffset, false)
}

func expressionTreeContainsLambda(expression *syntax.Expression) bool {
	if expression == nil {
		return false
	}
	if expression.Kind == syntax.ExpressionLambda {
		return true
	}
	return slices.ContainsFunc(expression.Children, expressionTreeContainsLambda)
}

func walkCompiledStringExpression(result *FileAnalysis, scope *Scope, expression *syntax.Expression, base, visibilityOffset int, preferFunction bool) {
	if expression == nil {
		return
	}
	switch expression.Kind {
	case syntax.ExpressionIdentifier:
		if !isLiteralIdentifier(expression.Value) {
			declaration := resolve(scope, expression.Value, visibilityOffset, preferFunction, nil)
			unscoped := !strings.Contains(expression.Value, ":")
			unknownVimVariable := isUnknownVimVariable(expression.Value)
			unsupportedNamespace := vim9UnsupportedNamespace(expression.Value)
			if !preferFunction && (unsupportedNamespace || declaration == nil && (unscoped || unknownVimVariable)) && expression.Value != "this" && expression.Value != "super" {
				appendVim9UnresolvedReadDiagnostic(result, scope, expression.Value, syntax.Span{Start: base + expression.Span.Start, End: base + expression.Span.End})
			}
		}
	case syntax.ExpressionMember:
		if len(expression.Children) > 0 {
			walkCompiledStringExpression(result, scope, expression.Children[0], base, visibilityOffset, false)
		}
	case syntax.ExpressionDictionary:
		for index, child := range expression.Children {
			if index%2 == 0 && child != nil && child.Kind == syntax.ExpressionIdentifier {
				continue
			}
			walkCompiledStringExpression(result, scope, child, base, visibilityOffset, false)
		}
	case syntax.ExpressionCall, syntax.ExpressionGenericReference:
		for index, child := range expression.Children {
			walkCompiledStringExpression(result, scope, child, base, visibilityOffset, index == 0)
		}
	default:
		for _, child := range expression.Children {
			walkCompiledStringExpression(result, scope, child, base, visibilityOffset, false)
		}
	}
}

func collectFunctionCallDiagnostics(result *FileAnalysis, scope *Scope, call *syntax.Expression, checkTypes bool) {
	if result == nil || result.File == nil || call == nil || call.Value != "" && call.Value != "->" || len(call.Children) == 0 {
		return
	}
	callee := call.Children[0]
	if callee == nil {
		return
	}
	if checkTypes && callee.Kind == syntax.ExpressionIdentifier && callee.Value == "_" {
		return
	}
	if call.Value == "" && callee.Kind == syntax.ExpressionIdentifier {
		// compile_call() has a 200-byte direct-name buffer only while compiling
		// a def. At Vim9 script level the same unresolved spelling is E117.
		if checkTypes && scopeContainsDef(scope) && len(callee.Value) >= 200 {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1011", Message: "Name too long: " + callee.Value, Span: callee.Span})
			return
		}
		if unresolvedDirectFunction(scope, callee) && !vimdata.IsNeovimCompatFunction(callee.Value) {
			name, span := callee.Value, callee.Span
			if local, scriptLocal := scriptLocalName(name); scriptLocal {
				span.Start += len(name) - len(local)
				name = "s:" + local
			}
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E117", Message: "Unknown function: " + name, Span: span})
			return
		}
	}
	callable := result.TypeOf(callee)
	arguments := call.Children[1:]
	if callee.Kind == syntax.ExpressionMember && len(callee.Children) == 1 && result.File.Text(callee.Operator) == "->" {
		declaration := resolve(scope, callee.Value, call.Span.Start, true, nil)
		if declaration == nil {
			builtin := vimdata.IsFunction(callee.Value)
			if !builtin && unresolvedFunctionName(callee.Value) && !vimdata.IsNeovimCompatFunction(callee.Value) {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E117", Message: "Unknown function: " + callee.Value, Span: memberNameSpan(result.File, callee)})
			}
			return
		}
		if !checkTypes && declaration.Span.Start >= call.Span.Start {
			return
		}
		callable = declaration.Type
		arguments = append([]*syntax.Expression{callee.Children[0]}, arguments...)
	} else if !checkTypes && callee.Kind == syntax.ExpressionIdentifier {
		declaration := resolve(scope, callee.Value, call.Span.Start, true, nil)
		if declaration == nil || declaration.Span.Start >= call.Span.Start {
			return
		}
		callable = declaration.Type
	}
	if checkTypes && callable.Name != "func" && !isUnknownType(callable) {
		name, span := functionDiagnosticTarget(result.File, callee)
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1085", Message: "Not a callable type: " + name, Span: span})
		return
	}
	if callable.Name != "func" || !callable.ArgumentCountKnown {
		return
	}
	name, span := functionDiagnosticTarget(result.File, callee)
	if len(arguments) < callable.RequiredArguments {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E119", Message: "Not enough arguments for function: " + name, Span: span})
		return
	}
	if !callable.Variadic && len(arguments) > len(callable.Arguments) {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E118", Message: "Too many arguments for function: " + name, Span: span})
		return
	}
	if !checkTypes {
		return
	}
	for index, argument := range arguments {
		expectedIndex := index
		if expectedIndex >= len(callable.Arguments) {
			if !callable.Variadic || len(callable.Arguments) == 0 {
				break
			}
			expectedIndex = len(callable.Arguments) - 1
		}
		expected := callable.Arguments[expectedIndex]
		if callable.Variadic && expectedIndex == len(callable.Arguments)-1 {
			expected = indexedType(expected)
		}
		actual := result.TypeOf(argument)
		if compatibleTypes(expected, actual) {
			continue
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E1013", Message: "Argument " + strconv.Itoa(index+1) + ": type mismatch, expected " + valueTypeDisplay(expected) + " but got " + valueTypeDisplay(actual), Span: argument.Span,
		})
	}
}

func unresolvedDirectFunction(scope *Scope, callee *syntax.Expression) bool {
	if scope == nil || callee == nil || callee.Kind != syntax.ExpressionIdentifier {
		return false
	}
	if name, scriptLocal := scriptLocalName(callee.Value); scriptLocal {
		if !validScopeVariableName(name) {
			return false
		}
	} else if !unresolvedFunctionName(callee.Value) {
		return false
	}
	return resolve(scope, callee.Value, callee.Span.Start, true, nil) == nil
}

func unresolvedFunctionName(name string) bool {
	return name != "" && name[0] >= 'a' && name[0] <= 'z' && !strings.ContainsAny(name, ":#&$@")
}

func callbackReceivesTooManyArguments(actual ValueType, expected builtinArgumentType) bool {
	return len(expected.functionArguments) > 0 && actual.Name == "func" && actual.ArgumentCountKnown && !actual.Variadic && len(expected.functionArguments) > len(actual.Arguments)
}

func callbackCheckerUsesE176(checker string) bool {
	switch checker {
	case "arg_filter_func", "arg_map_func", "arg_foreach_func":
		return true
	default:
		return false
	}
}

func builtinCallbackArgumentCountInvalid(actual ValueType, expected builtinArgumentType) bool {
	if len(expected.functionArguments) == 0 || actual.Name != "func" || !actual.ArgumentCountKnown {
		return false
	}
	actualCount := len(actual.Arguments)
	expectedCount := len(expected.functionArguments)
	return actualCount != expectedCount && !(actual.Variadic && actualCount == expectedCount-1)
}

func functionDiagnosticTarget(file *syntax.File, expression *syntax.Expression) (string, syntax.Span) {
	if expression == nil {
		return "<function>", syntax.Span{}
	}
	span := expression.Span
	if expression.Kind == syntax.ExpressionIdentifier && expression.Value != "" {
		return expression.Value, span
	}
	if expression.Kind == syntax.ExpressionMember && expression.Value != "" && file.Text(expression.Operator) == "->" {
		span.Start = span.End - len(expression.Value)
		return expression.Value, span
	}
	for expression.Kind == syntax.ExpressionParenthesized && len(expression.Children) == 1 {
		expression = expression.Children[0]
	}
	if expression.Kind == syntax.ExpressionLambda {
		return "<lambda>", span
	}
	if name := strings.TrimSpace(file.Text(span)); name != "" {
		return name, span
	}
	return "<function>", span
}

// collectBuiltinCallArityDiagnostic reports arity errors only where the
// callable is a statically named builtin. A method receiver counts as one
// argument, and any explicit arguments required before the receiver must still
// be present. Scoped and dynamically named calls deliberately remain unknown.
func collectBuiltinCallArityDiagnostic(result *FileAnalysis, file *syntax.File, call *syntax.Expression, scope *Scope, dialect syntax.Dialect) {
	if result == nil || file == nil || call == nil || len(call.Children) == 0 || expressionContainsMissing(call) || syntaxDiagnosticTouchesCall(file.Diagnostics, call.Span) {
		return
	}
	callee := call.Children[0]
	if callee == nil {
		return
	}
	name := ""
	argumentCount := 0
	explicitCount := 0
	method := false
	switch {
	case call.Value == "" && callee.Kind == syntax.ExpressionIdentifier:
		name = callee.Value
		argumentCount = len(call.Children) - 1
	case call.Value == "" && callee.Kind == syntax.ExpressionMember && len(callee.Children) == 1 && file.Text(callee.Operator) == "->":
		name = callee.Value
		explicitCount = len(call.Children) - 1
		argumentCount = explicitCount + 1
		method = true
	case call.Value == "->" && callee.Kind == syntax.ExpressionIdentifier && len(call.Children) >= 2:
		name = callee.Value
		explicitCount = len(call.Children) - 2
		argumentCount = explicitCount + 1
		method = true
	default:
		return
	}
	if strings.Contains(name, ":") {
		return
	}
	builtin, ok := vimdata.LookupFunction(name)
	if !ok {
		return
	}
	if dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope) && builtin.Name == "exists_compiled" {
		// The Vim9 compiler handles this builtin before ordinary call arity
		// checks and reports E1232 for every non-literal argument form.
		return
	}
	_, span := functionDiagnosticTarget(file, callee)
	if !validNameSpan(file, span) {
		return
	}
	if dialect == syntax.Vim9 && builtin.Name == "flatten" {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E1158", Message: "Cannot use flatten() in Vim9 script, use flattennew()", Span: span,
		})
		return
	}
	if method && (builtin.MethodArgument == 0 || builtin.MethodArgument-1 > explicitCount) {
		if builtin.MethodArgument == 0 {
			return
		}
		_, span := functionDiagnosticTarget(file, callee)
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E119", Message: "Not enough arguments for function: " + builtin.Name, Span: span,
		})
		return
	}
	if argumentCount < builtin.MinArgs {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E119", Message: "Not enough arguments for function: " + builtin.Name, Span: span,
		})
		return
	}
	if builtin.MaxArgs >= 0 && argumentCount > builtin.MaxArgs {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E118", Message: "Too many arguments for function: " + builtin.Name, Span: span,
		})
	}
}

func expressionContainsMissing(expression *syntax.Expression) bool {
	if expression == nil {
		return false
	}
	if expression.Kind == syntax.ExpressionMissing {
		return true
	}
	return slices.ContainsFunc(expression.Children, expressionContainsMissing)
}

// Syntax diagnostics use point spans for some missing delimiters. Treating a
// point on the call boundary as touching suppresses arity cascades while a
// document is being edited.
func syntaxDiagnosticTouchesCall(diagnostics []syntax.Diagnostic, call syntax.Span) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Span.Start <= call.End && diagnostic.Span.End >= call.Start {
			return true
		}
	}
	return false
}
