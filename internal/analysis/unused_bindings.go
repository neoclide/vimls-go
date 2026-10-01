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
	var inspectExpression func(*syntax.Expression, *Scope)
	inspectExpression = func(expression *syntax.Expression, scope *Scope) {
		if expression == nil || !result.analysisStep() {
			return
		}
		switch expression.Kind {
		case syntax.ExpressionCurlyName:
			markDynamic(scope)
		case syntax.ExpressionIdentifier:
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
			if builtin, arguments, ok := builtinCallArguments(result.File, expression); ok {
				switch builtin.Name {
				case "eval", "execute":
					markDynamic(scope)
				case "map", "mapnew", "filter":
					if len(arguments) > 1 && result.TypeOf(arguments[1]).Name != "func" {
						markDynamic(scope)
					}
				case "substitute":
					if len(arguments) > 2 && (arguments[2].Kind != syntax.ExpressionString || strings.Contains(arguments[2].Value, `\=`)) {
						markDynamic(scope)
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
			scope = result.lambdaScopes[expression]
		}
		for _, child := range expression.Children {
			inspectExpression(child, scope)
		}
	}
	for command, scope := range result.commandScopes {
		if !result.analysisStep() {
			return
		}
		if command.Canonical == "execute" {
			markDynamic(scope)
		}
		for _, expression := range command.Expressions {
			inspectExpression(expression, scope)
		}
		for _, target := range command.Targets {
			inspectExpression(target, scope)
		}
		if command.Mapping != nil {
			inspectExpression(command.Mapping.RHSExpression, scope)
			// Keycode-separated mapping bodies are not all parsed as commands.
			// A literal script-local name there is still a possible use.
			for _, name := range strings.FieldsFunc(result.File.Text(command.Mapping.RHS), func(character rune) bool {
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
				inspectExpression(parameter.Default, scope)
			}
		}
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
