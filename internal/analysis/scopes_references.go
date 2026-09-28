package analysis

import (
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func walkCommand(result *FileAnalysis, file *syntax.File, command *syntax.Command, scope *Scope) {
	if !result.analysisStep() {
		return
	}
	if command == nil || scope == nil {
		return
	}
	if command.Declaration != nil {
		for _, binding := range command.Declaration.Bindings {
			walkTypeReference(result, file, binding.ParsedType, scope, command.Dialect, nil)
		}
	}
	if command.For != nil {
		for _, binding := range command.For.Bindings {
			walkTypeReference(result, file, binding.ParsedType, scope, command.Dialect, nil)
		}
	}
	if command.Function != nil {
		typeParameters := command.Function.TypeParameters
		for _, parameter := range command.Function.Parameters {
			walkTypeReference(result, file, parameter.Type, scope, command.Dialect, typeParameters)
		}
		walkTypeReference(result, file, command.Function.ReturnType, scope, command.Dialect, typeParameters)
	}
	if command.TypeAlias != nil {
		walkTypeReference(result, file, command.TypeAlias.Type, scope, command.Dialect, nil)
	}
	if command.Aggregate != nil {
		for _, span := range command.Aggregate.Extends {
			walkTypeNameReference(result, file, file.Text(span), span, scope, command.Dialect, nil)
		}
		for _, span := range command.Aggregate.Implements {
			walkTypeNameReference(result, file, file.Text(span), span, scope, command.Dialect, nil)
		}
	}
	invalidUnderscoreDeclaration := false
	if command.Dialect == syntax.Vim9 && command.Declaration != nil &&
		(command.Canonical == "var" || command.Canonical == "const" || command.Canonical == "final") &&
		len(command.Declaration.Bindings) == 1 && command.Declaration.Name == command.Declaration.Bindings[0].Name && file.Text(command.Declaration.Bindings[0].Name) == "_" &&
		(command.Declaration.Target == nil || command.Declaration.Target.Kind == syntax.ExpressionIdentifier) && !syntaxDiagnosticOverlaps(file.Diagnostics, command.Span) {
		span := command.Declaration.Bindings[0].Name
		invalidUnderscoreDeclaration = appendUnderscoreDiagnostic(result, &syntax.Expression{Kind: syntax.ExpressionIdentifier, Value: "_", Span: span}, command.Dialect)
	}
	if command.Set != nil {
		for _, option := range command.Set.Options {
			appendUnknownSetOptionDiagnostic(result, file.Text(option.Name), option.Name, scope)
			appendSetOptionValueDiagnostic(result, file, command, option)
		}
	}
	if command.Function != nil {
		functionScope := scope
		if functionScope.Block >= 0 && functionScope.Kind != syntax.BlockFunction && functionScope.Kind != syntax.BlockDef {
			functionScope = scope
		}
		for _, parameter := range command.Function.Parameters {
			if parameter.Default != nil {
				walkExpression(result, file, parameter.Default, functionScope, nil, false, command.Dialect)
			}
		}
	}
	if command.Import != nil && command.Import.Path != nil {
		walkExpression(result, file, command.Import.Path, scope, nil, false, command.Dialect)
	}
	for _, value := range command.EnumValues {
		skip := map[syntax.Span]bool{value.Name: true}
		if value.Initializer != nil {
			walkExpression(result, file, value.Initializer, scope, skip, false, command.Dialect)
		} else {
			// Arguments are also children of Initializer for constructor-style
			// enum values.  Walk them only when the recovering AST has no
			// initializer, otherwise references would be duplicated.
			for _, argument := range value.Arguments {
				walkExpression(result, file, argument, scope, skip, false, command.Dialect)
			}
		}
	}
	for _, expression := range command.Expressions {
		if invalidUnderscoreDeclaration {
			continue
		}
		skip := map[syntax.Span]bool(nil)
		if command.Declaration != nil && !ordinaryContainerAssignment(command) {
			skip = make(map[syntax.Span]bool, len(command.Declaration.Bindings))
			for _, binding := range command.Declaration.Bindings {
				if strings.HasPrefix(file.Text(binding.Name), "&") {
					continue
				}
				skip[binding.Name] = true
			}
		}
		walkExpression(result, file, expression, scope, skip, false, command.Dialect)
	}
	if command.Mapping != nil {
		walkExpression(result, file, command.Mapping.RHSExpression, scope, nil, false, command.Dialect)
		appendMappingCmdReferences(result, file, scope, command.Mapping)
	}
	if command.Set != nil || command.Declaration != nil {
		appendCallbackOptionReferences(result, file, scope, command)
	}
	if command.Canonical != "++" && command.Canonical != "--" {
		for _, target := range command.Targets {
			if command.Canonical == "redir" {
				walkAssignmentTarget(result, file, target, scope, nil, command.Dialect)
			} else {
				walkExpression(result, file, target, scope, nil, false, command.Dialect)
			}
		}
	}
	if command.Embedded != nil {
		for index := range command.Embedded.Commands {
			nested := &command.Embedded.Commands[index]
			nestedScope := result.commandScopes[nested]
			if nestedScope == nil {
				nestedScope = scope
			}
			walkCommand(result, file, nested, nestedScope)
		}
	}
}

func walkExpression(result *FileAnalysis, file *syntax.File, expression *syntax.Expression, scope *Scope, skipped map[syntax.Span]bool, preferFunction bool, dialect syntax.Dialect) {
	if !result.analysisStep() {
		return
	}
	if expression == nil || scope == nil || file == nil {
		return
	}
	for _, typeArgument := range expression.TypeArguments {
		walkTypeReference(result, file, typeArgument, scope, dialect, nil)
	}
	walkTypeReference(result, file, expression.CastType, scope, dialect, nil)
	switch expression.Kind {
	case syntax.ExpressionInterpolatedString:
		for _, child := range expression.Children {
			if dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope) && child.Kind == syntax.ExpressionIdentifier {
				if declaration := resolve(scope, child.Value, child.Span.Start, false, skipped); declaration != nil && declaration.Kind == SymbolKindTypeAlias {
					result.typeAliasExempt[child.Span] = true
				}
			}
			walkExpression(result, file, child, scope, skipped, false, dialect)
		}
		return
	case syntax.ExpressionAssignment:
		if len(expression.Children) == 0 {
			return
		}
		appendOptionAssignmentValueDiagnostic(result, file, expression, dialect)
		diagnosticsBefore := len(result.Diagnostics)
		for _, child := range expression.Children[1:] {
			walkExpression(result, file, child, scope, skipped, false, dialect)
		}
		walkAssignmentTarget(result, file, expression.Children[0], scope, skipped, dialect)
		rhsUsesEnumAsValue := false
		for _, diagnostic := range result.Diagnostics[diagnosticsBefore:] {
			if diagnostic.Code == "vim/E1421" {
				rhsUsesEnumAsValue = true
				break
			}
		}
		if !rhsUsesEnumAsValue && !scopeContainsDef(scope) {
			appendEnumAsValueDiagnostic(result, scope, expression.Children[0], dialect)
		}
	case syntax.ExpressionIdentifier, syntax.ExpressionCurlyName:
		if appendUnderscoreDiagnostic(result, expression, dialect) {
			return
		}
		appendSuperMustBeFollowedByDotDiagnostic(result, file, scope, expression, dialect)
		if expression.Kind == syntax.ExpressionIdentifier && !isLiteralIdentifier(expression.Value) && !skipped[expression.Span] && validNameSpan(file, expression.Span) {
			if strings.HasPrefix(expression.Value, "&") {
				appendUnknownOptionDiagnostic(result, expression.Value, expression.Span, scope)
			}
			var declaration *Declaration
			if preferFunction {
				declaration = resolve(scope, expression.Value, expression.Span.Start, true, skipped)
			} else {
				declaration = resolveValue(scope, expression.Value, expression.Span.Start, skipped, dialect)
			}
			result.References = append(result.References, &Reference{
				Name: expression.Value, Span: expression.Span,
				Declaration: declaration, functionCallee: preferFunction, scope: scope, dialect: dialect,
			})
			if !preferFunction && declaration != nil && functionSymbolKind(declaration.Kind) && declaration.TypeParameterCount > 0 {
				appendMissingGenericTypeArgumentsDiagnostic(result, expression.Value, expression.Span)
			}
			if !preferFunction {
				appendEnumAsValueDiagnostic(result, scope, expression, dialect)
			}
			classAsValue := appendClassAsValueDiagnostic(result, declaration, expression, dialect)
			if !classAsValue && dialect == syntax.Vim9 && declaration != nil && declaration.Kind == SymbolKindTypeAlias && !result.typeAliasExempt[expression.Span] {
				diagnostic := syntax.Diagnostic{Span: expression.Span}
				if scopeUsesDefTypeRules(scope) {
					diagnostic.Code = "vim/E1407"
					diagnostic.Message = "Cannot use a Typealias as a variable or value"
				} else {
					diagnostic.Code = "vim/E1403"
					diagnostic.Message = "Type alias \"" + declaration.Name + "\" cannot be used as a value"
				}
				result.Diagnostics = append(result.Diagnostics, diagnostic)
			}
			unscoped := !strings.Contains(expression.Value, ":") && !strings.HasPrefix(expression.Value, "&") && !strings.HasPrefix(expression.Value, "$") && !strings.HasPrefix(expression.Value, "@")
			unknownVimVariable := isUnknownVimVariable(expression.Value)
			unsupportedNamespace := vim9UnsupportedNamespace(expression.Value)
			if !preferFunction && dialect == syntax.Vim9 && (unsupportedNamespace || declaration == nil && (unscoped || unknownVimVariable)) && expression.Value != "this" && expression.Value != "super" {
				appendVim9UnresolvedReadDiagnostic(result, scope, expression.Value, expression.Span)
			}
		}
		for _, child := range expression.Children {
			walkExpression(result, file, child, scope, skipped, false, dialect)
		}
	case syntax.ExpressionMember:
		// Value is the member spelling, not a lexical variable.  Only the
		// receiver expression participates in same-file resolution, except
		// when an arrow member is the callable of a function call.
		if preferFunction && file.Text(expression.Operator) == "->" {
			span := memberNameSpan(file, expression)
			if validNameSpan(file, span) && file.Text(span) == expression.Value {
				result.References = append(result.References, &Reference{
					Name: expression.Value, Span: span,
					Declaration: resolve(scope, expression.Value, span.Start, true, skipped), functionCallee: true, scope: scope, dialect: dialect,
				})
			}
		}
		if dialect == syntax.Vim9 {
			appendSuperOutsideClassMethodDiagnostic(result, file, scope, expression, dialect)
			appendSuperNotInChildClassDiagnostic(result, file, scope, expression, dialect)
			appendMissingEnumValueDiagnostic(result, scope, expression)
			appendObjectMethodThroughClassDiagnostic(result, scope, expression)
		}
		if len(expression.Children) > 0 {
			if expression.Children[0] != nil && expression.Children[0].Kind == syntax.ExpressionIdentifier {
				result.enumValueExempt[expression.Children[0].Span] = true
				result.typeAliasExempt[expression.Children[0].Span] = true
				result.classValueExempt[expression.Children[0].Span] = true
				if expression.Children[0].Value == "super" && file.Text(expression.Operator) == "." {
					result.superMemberExempt[expression.Children[0].Span] = true
				}
			}
			walkExpression(result, file, expression.Children[0], scope, skipped, false, dialect)
		}
	case syntax.ExpressionDictionary:
		for index, child := range expression.Children {
			// A plain dictionary key is syntax, not a variable reference.  A
			// computed key has a non-identifier node and is walked normally.
			if index%2 == 0 && child != nil && child.Kind == syntax.ExpressionIdentifier {
				continue
			}
			walkExpression(result, file, child, scope, skipped, false, dialect)
		}
	case syntax.ExpressionCall:
		collectBuiltinCallArityDiagnostic(result, file, expression, scope, dialect)
		appendUnqualifiedClassMethodDiagnostic(result, file, expression, scope, dialect)
		appendAbstractSuperMethodDiagnostic(result, file, expression, scope, dialect)
		appendNonGenericFunctionDiagnostic(result, expression, scope, skipped)
		appendTooManyGenericTypeArgumentsDiagnostic(result, expression, scope, skipped)
		appendNotEnoughGenericTypeArgumentsDiagnostic(result, expression, scope, skipped)
		appendGenericFunctionCallWithoutTypesDiagnostic(result, expression, scope, skipped)
		appendQuotedGenericFunctionDiagnostic(result, expression, scope, skipped)
		if len(expression.Children) > 1 && expression.Children[0] != nil && expression.Children[0].Kind == syntax.ExpressionIdentifier &&
			(expression.Children[0].Value == "type" || expression.Children[0].Value == "typename") {
			for _, argument := range expression.Children[1:] {
				if argument != nil && argument.Kind == syntax.ExpressionIdentifier {
					result.enumValueExempt[argument.Span] = true
				}
			}
		}
		if len(expression.Children) > 1 && expression.Children[0] != nil && expression.Children[0].Kind == syntax.ExpressionIdentifier {
			firstArgument := 1
			switch expression.Children[0].Value {
			case "type", "typename", "string":
			case "instanceof":
				firstArgument = 2
			default:
				firstArgument = len(expression.Children)
			}
			for _, argument := range expression.Children[firstArgument:] {
				if argument != nil && argument.Kind == syntax.ExpressionIdentifier {
					result.typeAliasExempt[argument.Span] = true
					result.classValueExempt[argument.Span] = true
				}
			}
		}
		for index, child := range expression.Children {
			walkExpression(result, file, child, scope, skipped, index == 0, dialect)
		}
	case syntax.ExpressionGenericReference:
		appendNonGenericFunctionDiagnostic(result, expression, scope, skipped)
		appendTooManyGenericTypeArgumentsDiagnostic(result, expression, scope, skipped)
		appendNotEnoughGenericTypeArgumentsDiagnostic(result, expression, scope, skipped)
		for index, child := range expression.Children {
			walkExpression(result, file, child, scope, skipped, index == 0, dialect)
		}
	case syntax.ExpressionLambda:
		lambdaScope := result.lambdaScopes[expression]
		if lambdaScope == nil {
			lambdaScope = scope
		}
		for _, parameter := range expression.Parameters {
			walkTypeReference(result, file, parameter.Type, lambdaScope, dialect, nil)
		}
		walkTypeReference(result, file, expression.ReturnType, lambdaScope, dialect, nil)
		if expression.LambdaBody != nil {
			for index := range expression.LambdaBody.Commands {
				command := &expression.LambdaBody.Commands[index]
				commandScope := result.commandScopes[command]
				if commandScope == nil {
					commandScope = lambdaScope
				}
				walkCommand(result, file, command, commandScope)
			}
		}
		for index, child := range expression.Children {
			if index < len(expression.Parameters) {
				continue
			}
			walkExpression(result, file, child, lambdaScope, skipped, false, dialect)
		}
	default:
		for _, child := range expression.Children {
			walkExpression(result, file, child, scope, skipped, false, dialect)
		}
	}
}

// walkAssignmentTarget resolves references contained in an assignment lhs,
// while keeping the lhs binding itself separate from ordinary rhs reads.  Vim9
// reports E1089 for a statically unknown root binding; index expressions remain
// ordinary reads and can still produce E1001.
func walkAssignmentTarget(result *FileAnalysis, file *syntax.File, expression *syntax.Expression, scope *Scope, skipped map[syntax.Span]bool, dialect syntax.Dialect) {
	if expression == nil || scope == nil || file == nil {
		return
	}
	switch expression.Kind {
	case syntax.ExpressionIdentifier, syntax.ExpressionCurlyName:
		if appendUnderscoreDiagnostic(result, expression, dialect) {
			return
		}
		if expression.Kind == syntax.ExpressionIdentifier && !isLiteralIdentifier(expression.Value) && !skipped[expression.Span] && validNameSpan(file, expression.Span) {
			if strings.HasPrefix(expression.Value, "&") {
				appendUnknownOptionDiagnostic(result, expression.Value, expression.Span, scope)
			}
			declaration := resolveValue(scope, expression.Value, expression.Span.Start, skipped, dialect)
			result.References = append(result.References, &Reference{
				Name: expression.Value, Span: expression.Span, Declaration: declaration, assignmentTarget: true, scope: scope, dialect: dialect,
			})
			if dialect == syntax.Vim9 && (vim9UnsupportedNamespace(expression.Value) || declaration == nil && isUnknownVimVariable(expression.Value)) {
				appendVim9UnresolvedReadDiagnostic(result, scope, expression.Value, expression.Span)
			} else if dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope) && assignmentTargetNeedsDeclaration(expression.Value) && declaration == nil {
				if !appendInheritedClassVariableDiagnostic(result, scope, expression.Value, expression.Span) {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E1089", Message: "Unknown variable: " + expression.Value, Span: expression.Span,
					})
				}
			}
		}
	case syntax.ExpressionMember:
		if dialect == syntax.Vim9 {
			appendMissingEnumValueDiagnostic(result, scope, expression)
		}
		if len(expression.Children) > 0 {
			walkAssignmentTarget(result, file, expression.Children[0], scope, skipped, dialect)
		}
	case syntax.ExpressionIndex, syntax.ExpressionSlice:
		if len(expression.Children) > 0 {
			walkAssignmentTarget(result, file, expression.Children[0], scope, skipped, dialect)
		}
		for _, child := range expression.Children[1:] {
			walkExpression(result, file, child, scope, skipped, false, dialect)
		}
	case syntax.ExpressionList, syntax.ExpressionTuple:
		for _, child := range expression.Children {
			if dialect == syntax.Vim9 && child != nil && child.Kind == syntax.ExpressionIdentifier && child.Value == "_" {
				continue
			}
			walkAssignmentTarget(result, file, child, scope, skipped, dialect)
		}
	default:
		// A recovering or otherwise non-assignable lhs is still an expression.
		// Walk it normally so a call name or operand is not mislabeled E1089.
		walkExpression(result, file, expression, scope, skipped, false, dialect)
	}
}

func assignmentTargetNeedsDeclaration(name string) bool {
	return name != "this" && name != "super" && !strings.Contains(name, ":") && !strings.HasPrefix(name, "&") && !strings.HasPrefix(name, "$") && !strings.HasPrefix(name, "@")
}

// Legacy variable reads do not produce a Funcref merely because a function
// has the same name. Vim9, in contrast, permits function names as values.
func resolveValue(scope *Scope, name string, offset int, hidden map[syntax.Span]bool, dialect syntax.Dialect) *Declaration {
	declaration := resolve(scope, name, offset, false, hidden)
	if dialect != syntax.Legacy || declaration == nil || !functionSymbolKind(declaration.Kind) {
		return declaration
	}
	skipped := make(map[syntax.Span]bool, len(hidden)+1)
	for span, value := range hidden {
		skipped[span] = value
	}
	for declaration != nil && functionSymbolKind(declaration.Kind) {
		skipped[declaration.Span] = true
		declaration = resolve(scope, name, offset, false, skipped)
	}
	return declaration
}

func resolve(scope *Scope, name string, offset int, preferFunction bool, hidden map[syntax.Span]bool) *Declaration {
	if name == "" {
		return nil
	}
	explicitArgument := strings.HasPrefix(name, "a:") && len(name) > 2
	explicitLocal := strings.HasPrefix(name, "l:") && len(name) > 2
	explicitName := name
	if explicitArgument || explicitLocal {
		explicitName = name[2:]
		insideFunction := false
		for current := scope; current != nil; current = current.Parent {
			if current.Kind == syntax.BlockFunction {
				insideFunction = true
				break
			}
		}
		if !insideFunction {
			return nil
		}
	}
	for current := scope; current != nil; current = current.Parent {
		var latest *Declaration
		var forwardFunction *Declaration
		for _, declaration := range current.Declarations {
			matches := resolvedNameEqual(declaration.Name, name)
			if explicitArgument {
				matches = declaration.Parameter && declaration.Name == explicitName
			} else if explicitLocal {
				matches = !declaration.Parameter && declaration.Name == explicitName
			}
			if !matches || hidden[declaration.Span] {
				continue
			}
			if preferFunction && declaration.Kind == SymbolKindFunction || preferFunction && declaration.Kind == SymbolKindMethod || preferFunction && declaration.Kind == SymbolKindConstructor {
				if forwardFunction == nil || declaration.Span.Start < forwardFunction.Span.Start {
					forwardFunction = declaration
				}
				continue
			}
			if declaration.Span.Start < offset && (latest == nil || declaration.Span.Start > latest.Span.Start) {
				latest = declaration
			}
		}
		if forwardFunction != nil {
			return forwardFunction
		}
		if latest != nil {
			return latest
		}
		if (explicitArgument || explicitLocal) && current.Kind == syntax.BlockFunction {
			return nil
		}
	}
	return nil
}

func resolvedNameEqual(left, right string) bool {
	if left == right {
		return true
	}
	leftScript, leftOK := scriptLocalName(left)
	rightScript, rightOK := scriptLocalName(right)
	return leftOK && rightOK && leftScript == rightScript
}

func scriptLocalName(name string) (string, bool) {
	if strings.HasPrefix(name, "s:") && len(name) > 2 {
		return name[2:], true
	}
	if len(name) > len("<SID>") && strings.EqualFold(name[:len("<SID>")], "<SID>") {
		return name[len("<SID>"):], true
	}
	return "", false
}

func validNameSpan(file *syntax.File, span syntax.Span) bool {
	return file != nil && span.Start >= 0 && span.Start < span.End && span.End <= len(file.Source)
}

func isLiteralIdentifier(name string) bool {
	switch strings.ToLower(name) {
	case "true", "false", "null", "null_blob", "null_channel", "null_class", "null_dict", "null_function", "null_job", "null_list", "null_object", "null_partial", "null_string", "null_tuple":
		return true
	default:
		return false
	}
}

func emptySyntaxSpan(span syntax.Span) bool {
	return span.Start >= span.End
}
