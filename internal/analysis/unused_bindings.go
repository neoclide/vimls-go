package analysis

import (
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// Legacy assignments share a function or script namespace. Vim9 functions
// retain their lexical declaration scope. A nil scope identifies Legacy s: names.
type unusedBinding struct {
	name     string
	scope    *Scope
	function bool
}

const unusedCodePlaceholder = "VimlsUnknownCodeFragment"

// Keep the literal structure of generated code. Unknown substitutions remain
// visible to the parser: an expression/name hole suppresses hints, whereas a
// filename argument or text inside a quoted string does not read a binding.
func unusedCodeTemplate(result *FileAnalysis, expression *syntax.Expression) string {
	var code strings.Builder
	var appendExpression func(*syntax.Expression)
	appendExpression = func(expression *syntax.Expression) {
		if !result.analysisStep() {
			return
		}
		switch {
		case expression.Kind == syntax.ExpressionString:
			if value, ok := syntax.StaticDictionaryIndexKey(expression); ok {
				code.WriteString(value)
				return
			}
		case expression.Kind == syntax.ExpressionParenthesized && len(expression.Children) == 1:
			appendExpression(expression.Children[0])
			return
		case expression.Kind == syntax.ExpressionBinary && (expression.Value == "." || expression.Value == ".."):
			appendExpression(expression.Children[0])
			appendExpression(expression.Children[1])
			return
		case result.TypeOf(expression).Name == "number":
			code.WriteByte('0')
			return
		}
		code.WriteString(unusedCodePlaceholder)
	}
	appendExpression(expression)
	return code.String()
}

func legacyUnusedName(name string, scope *Scope, function bool) (unusedBinding, bool) {
	if scriptName, ok := scriptLocalName(name); ok {
		return unusedBinding{name: scriptName, function: function}, validScopeVariableName(scriptName) && (function || scriptName != "_")
	}
	if function {
		return unusedBinding{}, false
	}
	name = strings.TrimPrefix(name, "l:")
	if !validScopeVariableName(name) || strings.Contains(name, "#") || name == "_" {
		return unusedBinding{}, false
	}
	for current := scope; current != nil; current = current.Parent {
		if current.Kind == syntax.BlockFunction {
			return unusedBinding{name: name, scope: current}, true
		}
		if current.Kind == syntax.BlockDef {
			break
		}
	}
	return unusedBinding{}, false
}

func collectUnusedBindingDiagnostics(result *FileAnalysis) {
	candidates := make(map[syntax.Span]unusedBinding)
	functionBodies := make(map[unusedBinding]*Scope)
	for command, scope := range result.commandScopes {
		if !result.analysisStep() {
			return
		}
		if command.Function != nil {
			name := result.File.Text(command.Function.Name)
			key, ok := legacyUnusedName(name, scope, true)
			if command.Dialect == syntax.Vim9 {
				key = unusedBinding{name: name, scope: scope.Parent, function: true}
				ok = command.Canonical == "def" && scope.Kind == syntax.BlockDef && !commandHasModifier(command, "export") &&
					validScopeVariableName(name) && !strings.Contains(name, "#") && unusedVariableScope(scope.Parent)
			}
			if ok {
				candidates[command.Function.Name] = key
				functionBodies[key] = scope
			}
		}
		if command.Dialect != syntax.Legacy {
			continue
		}
		var bindings []syntax.Binding
		if command.Declaration != nil && !ordinaryContainerAssignment(command) {
			bindings = command.Declaration.Bindings
		} else if command.For != nil {
			bindings = command.For.Bindings
		}
		for _, binding := range bindings {
			if key, ok := legacyUnusedName(result.File.Text(binding.Name), scope, false); ok {
				candidates[binding.Name] = key
			}
		}
	}
	if len(candidates) == 0 {
		return
	}

	used := make(map[unusedBinding]bool)
	for _, reference := range result.References {
		if !result.analysisStep() {
			return
		}
		if reference.Declaration != nil && reference.Declaration.Parameter {
			continue
		}
		key, ok := legacyUnusedName(reference.Name, reference.scope, reference.functionCallee)
		if !ok && reference.functionCallee {
			key, ok = legacyUnusedName(reference.Name, reference.scope, false)
		}
		if reference.Declaration != nil {
			if resolved, exists := candidates[reference.Declaration.Span]; exists {
				key, ok = resolved, true
			}
		}
		if scriptName, scriptLocal := scriptLocalName(reference.Name); scriptLocal && reference.functionCallee {
			if declaration := resolve(result.Root, scriptName, reference.Span.Start, true, nil); declaration != nil {
				if resolved, exists := candidates[declaration.Span]; exists && resolved.function {
					key, ok = resolved, true
				}
			}
		}
		if ok {
			// A recursive call alone does not make a private function used.
			if body := functionBodies[key]; body != nil && reference.Span.Start >= body.Span.Start && reference.Span.End <= body.Span.End {
				continue
			}
			used[key] = true
		}
	}
	markFunctionName := func(name string) {
		if key, ok := legacyUnusedName(name, result.Root, true); ok {
			used[key] = true
			name = key.name
		}
		// String names address script functions. Nested Vim9 functions must
		// be passed as values, which are already covered by References.
		if declaration := resolve(result.Root, name, 0, true, nil); declaration != nil {
			if key, ok := candidates[declaration.Span]; ok && key.function {
				used[key] = true
			}
		}
	}

	// Runtime evaluation and scope dictionaries can read names that have no
	// lexical Reference. Keep those namespaces conservative, without hiding
	// unused locals in unrelated functions.
	dynamicVariables := make(map[*Scope]bool)
	dynamicFunctions := false
	markDynamic := func(scope *Scope) {
		dynamicVariables[nil] = true
		dynamicFunctions = true
		for current := scope; current != nil; current = current.Parent {
			if current.Kind == syntax.BlockFunction {
				dynamicVariables[current] = true
			}
		}
	}
	var inspectExpression func(*syntax.File, *syntax.Expression, *Scope, syntax.Dialect)
	var inspectCommand func(*syntax.File, *syntax.Command, *Scope)
	inspectCode := func(source string, scope *Scope, dialect syntax.Dialect) {
		if !result.analysisStep() {
			return
		}
		var file *syntax.File
		if dialect == syntax.Vim9 {
			file = (syntax.Vim9Parser{}).Parse(source)
		} else {
			file = (syntax.LegacyParser{}).Parse(source)
		}
		if len(file.Diagnostics) != 0 {
			markDynamic(scope)
			return
		}
		for index := range file.Commands {
			inspectCommand(file, &file.Commands[index], scope)
		}
	}
	inspectExpression = func(file *syntax.File, expression *syntax.Expression, scope *Scope, dialect syntax.Dialect) {
		if expression == nil || !result.analysisStep() {
			return
		}
		switch expression.Kind {
		case syntax.ExpressionCurlyName:
			markDynamic(scope)
		case syntax.ExpressionIdentifier:
			if file != result.File {
				if strings.Contains(expression.Value, unusedCodePlaceholder) {
					markDynamic(scope)
				}
				if key, ok := legacyUnusedName(expression.Value, scope, false); ok {
					used[key] = true
				}
				markFunctionName(expression.Value)
				if declaration := resolve(scope, expression.Value, scope.Span.End, true, nil); declaration != nil {
					if key, ok := candidates[declaration.Span]; ok && key.function {
						used[key] = true
					}
				}
			}
			if expression.Value == "s:" {
				dynamicVariables[nil] = true
			} else if expression.Value == "l:" {
				if key, ok := legacyUnusedName("local", scope, false); ok {
					dynamicVariables[key.scope] = true
				}
			}
		case syntax.ExpressionString:
			// Callback names also occur in option values and dictionaries.
			name := simpleVimStringLiteral(expression.Value)
			markFunctionName(name)
			if name == "<SID>" || strings.Contains(name, "<SNR>") {
				dynamicFunctions = true
			}
		case syntax.ExpressionCall:
			if builtin, arguments, ok := builtinCallArguments(file, expression); ok {
				switch builtin.Name {
				case "eval", "execute":
					if len(arguments) > 0 {
						code := unusedCodeTemplate(result, arguments[0])
						if builtin.Name == "eval" {
							code = "echo " + code
						}
						inspectCode(code, scope, dialect)
					}
				case "map", "mapnew", "filter":
					if len(arguments) > 1 && result.TypeOf(arguments[1]).Name != "func" {
						inspectCode("echo "+unusedCodeTemplate(result, arguments[1]), scope, dialect)
					}
				case "substitute":
					if len(arguments) > 2 && (arguments[2].Kind != syntax.ExpressionString || strings.Contains(arguments[2].Value, `\=`)) {
						code := unusedCodeTemplate(result, arguments[2])
						inspectCode("echo "+strings.TrimPrefix(code, `\=`), scope, dialect)
					}
				case "function", "funcref", "call":
					if len(arguments) > 0 {
						typ := result.TypeOf(arguments[0]).Name
						if typ != "func" && typ != "partial" && simpleVimStringLiteral(arguments[0].Value) == "" {
							dynamicFunctions = true
						}
					}
				case "exists":
					if len(arguments) > 0 {
						name := simpleVimStringLiteral(arguments[0].Value)
						function := strings.HasPrefix(name, "*")
						if function {
							markFunctionName(name[1:])
						}
						if key, ok := legacyUnusedName(strings.TrimPrefix(name, "*"), scope, function); ok {
							used[key] = true
						}
					}
				}
			}
		case syntax.ExpressionLambda:
			if file == result.File {
				scope = result.lambdaScopes[expression]
			}
		}
		for _, child := range expression.Children {
			inspectExpression(file, child, scope, dialect)
		}
	}
	inspectCommand = func(file *syntax.File, command *syntax.Command, scope *Scope) {
		if !result.analysisStep() {
			return
		}
		if file != result.File && strings.Contains(file.Text(command.Name), unusedCodePlaceholder) {
			markDynamic(scope)
		}
		if command.Canonical == "execute" {
			var parts []string
			for _, expression := range command.Expressions {
				parts = append(parts, unusedCodeTemplate(result, expression))
			}
			inspectCode(strings.Join(parts, " "), scope, command.Dialect)
		}
		for _, expression := range command.Expressions {
			inspectExpression(file, expression, scope, command.Dialect)
		}
		for _, target := range command.Targets {
			inspectExpression(file, target, scope, command.Dialect)
		}
		if command.Mapping != nil {
			inspectExpression(file, command.Mapping.RHSExpression, scope, command.Dialect)
			if file != result.File && strings.Contains(file.Text(command.Mapping.RHS), unusedCodePlaceholder) {
				markDynamic(scope)
			}
			// Keycode-separated mapping bodies are not all parsed as commands.
			// A literal script-local name there is still a possible use.
			for _, name := range strings.FieldsFunc(file.Text(command.Mapping.RHS), func(character rune) bool {
				return !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
					character >= '0' && character <= '9' || character >= 0x80 || strings.ContainsRune("_:<>", character))
			}) {
				if scriptName, ok := scriptLocalName(name); ok {
					// An adjacent <CR> or comparison operator ends the name.
					if end := strings.IndexAny(scriptName, "<>"); end >= 0 {
						scriptName = scriptName[:end]
					}
					if validScopeVariableName(scriptName) {
						markFunctionName("s:" + scriptName)
						used[unusedBinding{name: scriptName}] = true
					}
				} else {
					// <Cmd> can directly precede an implicit Vim9 call.
					if end := strings.LastIndexByte(name, '>'); end >= 0 {
						name = name[end+1:]
					}
					markFunctionName(name)
				}
			}
		}
		if command.Function != nil {
			for _, parameter := range command.Function.Parameters {
				inspectExpression(file, parameter.Default, scope, command.Dialect)
			}
		}
		if command.UserCommand != nil {
			for _, attribute := range command.UserCommand.Attributes {
				if file.Text(attribute.Name) == "complete" {
					kind, name, _ := strings.Cut(file.Text(attribute.Value), ",")
					if kind == "custom" || kind == "customlist" {
						if file != result.File && strings.Contains(name, unusedCodePlaceholder) {
							dynamicFunctions = true
						}
						markFunctionName(name)
					}
				}
			}
		}
		if file != result.File && command.Set != nil {
			for _, option := range command.Set.Options {
				if callbackFunctionOptions[normalizeOptionName(file.Text(option.Name))] {
					name := file.Text(option.Value)
					if strings.Contains(name, unusedCodePlaceholder) {
						dynamicFunctions = true
					}
					markFunctionName(name)
				}
			}
		}
		if file != result.File && command.Embedded != nil {
			for index := range command.Embedded.Commands {
				inspectCommand(file, &command.Embedded.Commands[index], scope)
			}
		}
	}
	for command, scope := range result.commandScopes {
		if !result.analysisStep() {
			return
		}
		inspectCommand(result.File, command, scope)
	}

	reported := make(map[unusedBinding]bool)
	for _, declaration := range result.Declarations {
		if !result.analysisStep() {
			return
		}
		key, ok := candidates[declaration.Span]
		if !ok || used[key] || reported[key] || key.function && dynamicFunctions || !key.function && dynamicVariables[key.scope] {
			continue
		}
		reported[key] = true
		code := "vimls/unused-variable"
		if key.function {
			code = "vimls/unused-function"
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: code, Message: declaration.Name + " is declared but never used", Span: declaration.Span,
		})
	}
}
