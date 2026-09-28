package analysis

import (
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func collectNullReceiverDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	declarations := make(map[syntax.Span]*Declaration)
	for _, declaration := range result.Declarations {
		if declaration != nil {
			declarations[declaration.Span] = declaration
		}
	}
	aliases := localTypeAliases(file)
	var typeDefaultsToNullObject func(*syntax.Type, *Scope, map[syntax.Span]bool) bool
	typeDefaultsToNullObject = func(typeNode *syntax.Type, scope *Scope, seen map[syntax.Span]bool) bool {
		if typeNode == nil || scope == nil || typeNode.Kind == syntax.TypeMissing || syntaxDiagnosticOverlaps(file.Diagnostics, typeNode.Span) {
			return false
		}
		if typeNode.Kind == syntax.TypeOptional || typeNode.Kind == syntax.TypeVariadic {
			return len(typeNode.Arguments) == 1 && typeDefaultsToNullObject(typeNode.Arguments[0], scope, seen)
		}
		if typeNode.Kind == syntax.TypeGeneric {
			return typeNode.Name == "object" && len(typeNode.Arguments) == 1 &&
				objectTypeArgumentValidity(result, scope, typeNode.Arguments[0], aliases, make(map[syntax.Span]bool)) == objectTypeValid
		}
		if typeNode.Kind != syntax.TypeNamed || typeNode.Name == "any" {
			return false
		}
		declaration := resolve(scope, typeNode.Name, typeNode.Span.Start, false, nil)
		if declaration == nil {
			return false
		}
		switch declaration.Kind {
		case SymbolKindClass, SymbolKindInterface, SymbolKindEnum:
			return true
		case SymbolKindTypeAlias:
			if seen[declaration.Span] || aliases[declaration.Span] == nil {
				return false
			}
			seen[declaration.Span] = true
			valid := typeDefaultsToNullObject(aliases[declaration.Span], declaration.Scope, seen)
			delete(seen, declaration.Span)
			return valid
		}
		return false
	}
	isNullLiteral := func(expression *syntax.Expression, name string) bool {
		for expression != nil && (expression.Kind == syntax.ExpressionParenthesized || expression.Kind == syntax.ExpressionCast) && len(expression.Children) == 1 {
			expression = expression.Children[0]
		}
		return expression != nil && expression.Kind == syntax.ExpressionIdentifier && strings.EqualFold(expression.Value, name)
	}
	isNullObject := func(expression *syntax.Expression) bool { return isNullLiteral(expression, "null_object") }

	type scopedExpression struct {
		expression *syntax.Expression
		scope      *Scope
	}
	var assignments []scopedExpression
	candidates := make(map[*Declaration]bool)
	written := make(map[*Declaration]bool)
	seenLambdas := make(map[*syntax.Expression]bool)
	var collectCommands func([]syntax.Command, *Scope)
	var collectLambdaBodies func(*syntax.Expression, *Scope)
	collectLambdaBodies = func(expression *syntax.Expression, scope *Scope) {
		if expression == nil || !result.analysisStep() {
			return
		}
		expressionScope := scope
		if expression.Kind == syntax.ExpressionLambda {
			lambdaScope := result.lambdaScopes[expression]
			if lambdaScope == nil {
				lambdaScope = scope
			}
			expressionScope = lambdaScope
			first := !seenLambdas[expression]
			if first {
				seenLambdas[expression] = true
			}
			if first && expression.LambdaBody != nil {
				collectCommands(expression.LambdaBody.Commands, lambdaScope)
			}
		}
		if expression.Kind == syntax.ExpressionAssignment {
			assignments = append(assignments, scopedExpression{expression: expression, scope: expressionScope})
		}
		for index, child := range expression.Children {
			if expression.Kind != syntax.ExpressionLambda || index >= len(expression.Parameters) {
				collectLambdaBodies(child, expressionScope)
			}
		}
	}
	collectCommands = func(commands []syntax.Command, fallback *Scope) {
		for index := range commands {
			if !result.analysisStep() {
				return
			}
			command := &commands[index]
			scope := result.commandScopes[command]
			if scope == nil {
				scope = fallback
			}
			if command.Dialect == syntax.Vim9 && command.Declaration != nil && len(command.Declaration.Bindings) == 1 {
				binding := command.Declaration.Bindings[0]
				declaration := declarations[binding.Name]
				aggregateMember := declaration != nil && declaration.Scope != nil &&
					(declaration.Scope.Kind == syntax.BlockClass || declaration.Scope.Kind == syntax.BlockInterface || declaration.Scope.Kind == syntax.BlockEnum)
				if declaration != nil && declaration.Kind == SymbolKindVariable && !aggregateMember &&
					!syntaxDiagnosticOverlaps(file.Diagnostics, command.Declaration.Name) {
					initializer := command.Declaration.Initializer
					if initializer == nil && typeDefaultsToNullObject(binding.ParsedType, scope, make(map[syntax.Span]bool)) ||
						isNullObject(initializer) && (binding.ParsedType == nil || convertSyntaxType(binding.ParsedType).Name == "any" || typeDefaultsToNullObject(binding.ParsedType, scope, make(map[syntax.Span]bool))) {
						candidates[declaration] = true
					}
				}
			}
			for _, expression := range command.Expressions {
				collectLambdaBodies(expression, scope)
			}
			for _, expression := range command.Targets {
				collectLambdaBodies(expression, scope)
			}
			if command.Mapping != nil {
				collectLambdaBodies(command.Mapping.RHSExpression, scope)
			}
			if command.Declaration != nil {
				collectLambdaBodies(command.Declaration.Initializer, scope)
			}
			if command.For != nil {
				collectLambdaBodies(command.For.Iterable, scope)
			}
			if command.Import != nil {
				collectLambdaBodies(command.Import.Path, scope)
			}
			for _, value := range command.EnumValues {
				collectLambdaBodies(value.Initializer, scope)
				for _, argument := range value.Arguments {
					collectLambdaBodies(argument, scope)
				}
			}
			if command.Function != nil {
				for _, parameter := range command.Function.Parameters {
					collectLambdaBodies(parameter.Default, scope)
				}
			}
			if command.Embedded != nil {
				collectCommands(command.Embedded.Commands, scope)
			}
		}
	}
	collectCommands(file.Commands, result.Root)

	// Preserve initial null declarations while marking any visible write. The
	// write-free subset remains the stable candidate baseline; the final walk
	// adds only source-ordered facts for declarations that are later written.
	var markWrittenTarget func(*syntax.Expression, *Scope)
	markWrittenTarget = func(target *syntax.Expression, scope *Scope) {
		if target == nil || scope == nil {
			return
		}
		switch target.Kind {
		case syntax.ExpressionIdentifier:
			if declaration := resolve(scope, target.Value, target.Span.Start, false, nil); candidates[declaration] {
				written[declaration] = true
			}
		case syntax.ExpressionList, syntax.ExpressionTuple, syntax.ExpressionParenthesized:
			for _, child := range target.Children {
				markWrittenTarget(child, scope)
			}
		}
	}
	for _, assignment := range assignments {
		if len(assignment.expression.Children) > 0 {
			markWrittenTarget(assignment.expression.Children[0], assignment.scope)
		}
	}

	// Write-free candidates remain stable across the file. The environment below
	// additionally tracks initial null declarations with writes in source order;
	// it does not infer non-null assignments or change types.
	type nullGuard struct {
		declaration *Declaration
		whenTrue    bool
	}
	seenExpressions := make(map[*syntax.Expression]bool)
	active := make(map[*Declaration]int)
	known := make(map[*Declaration]bool)
	var appendDiagnostics func(*syntax.Expression, *Scope, syntax.Dialect)
	var walkCommands func([]syntax.Command, []syntax.Block, *Scope, bool)
	var walkSequence func([]syntax.Command, []syntax.Block, int, int, *Scope)

	unwrapParenthesized := func(expression *syntax.Expression) *syntax.Expression {
		for expression != nil && expression.Kind == syntax.ExpressionParenthesized && len(expression.Children) == 1 {
			if !result.analysisStep() {
				return nil
			}
			expression = expression.Children[0]
		}
		return expression
	}
	identifier := func(expression *syntax.Expression, scope *Scope) *Declaration {
		expression = unwrapParenthesized(expression)
		if expression == nil || expression.Kind != syntax.ExpressionIdentifier {
			return nil
		}
		declaration := resolve(scope, expression.Value, expression.Span.Start, false, nil)
		if !candidates[declaration] {
			return nil
		}
		return declaration
	}
	nullLiteral := func(expression *syntax.Expression, name string) bool {
		expression = unwrapParenthesized(expression)
		return expression != nil && expression.Kind == syntax.ExpressionIdentifier && expression.Value == name
	}
	headerComplete := func(command *syntax.Command) bool {
		if syntaxDiagnosticOverlaps(file.Diagnostics, command.Span) {
			return false
		}
		if command.Canonical != "else" && command.Canonical != "endif" {
			return true
		}
		argument := strings.TrimSpace(file.Text(command.Argument))
		return argument == "" || strings.HasPrefix(argument, "#")
	}
	guardFor := func(expression *syntax.Expression, scope *Scope) nullGuard {
		if expression == nil || scope == nil || !result.analysisStep() {
			return nullGuard{}
		}
		negated := false
		for {
			if !result.analysisStep() {
				return nullGuard{}
			}
			if expression.Kind == syntax.ExpressionParenthesized && len(expression.Children) == 1 {
				expression = expression.Children[0]
				continue
			}
			if expression.Kind == syntax.ExpressionUnary && expression.Value == "!" && len(expression.Children) == 1 {
				negated = !negated
				expression = expression.Children[0]
				continue
			}
			break
		}
		if expression == nil {
			return nullGuard{}
		}
		if expression.Kind == syntax.ExpressionBinary && len(expression.Children) == 2 {
			left, right := expression.Children[0], expression.Children[1]
			declaration := identifier(left, scope)
			nullName := ""
			if declaration != nil {
				if nullLiteral(right, "null_object") {
					nullName = "null_object"
				} else if nullLiteral(right, "null") {
					nullName = "null"
				}
			} else {
				declaration = identifier(right, scope)
				if declaration != nil {
					if nullLiteral(left, "null_object") {
						nullName = "null_object"
					} else if nullLiteral(left, "null") {
						nullName = "null"
					}
				}
			}
			if declaration == nil || nullName == "" {
				return nullGuard{}
			}
			whenTrue := false
			switch expression.Value {
			case "!=":
				whenTrue = true
			case "==":
				whenTrue = false
			case "isnot":
				if nullName != "null_object" {
					return nullGuard{}
				}
				whenTrue = true
			case "is":
				if nullName != "null_object" {
					return nullGuard{}
				}
				whenTrue = false
			default:
				return nullGuard{}
			}
			if negated {
				whenTrue = !whenTrue
			}
			return nullGuard{declaration: declaration, whenTrue: whenTrue}
		}
		if expression.Kind != syntax.ExpressionCall || expression.Value != "" || len(expression.Children) != 3 ||
			expression.Children[0] == nil || expression.Children[0].Kind != syntax.ExpressionIdentifier || expression.Children[0].Value != "instanceof" {
			return nullGuard{}
		}
		if resolve(scope, "instanceof", expression.Children[0].Span.Start, true, nil) != nil {
			return nullGuard{}
		}
		declaration := identifier(expression.Children[1], scope)
		className := expression.Children[2]
		if declaration == nil || className == nil || className.Kind != syntax.ExpressionIdentifier {
			return nullGuard{}
		}
		classDeclaration := resolve(scope, className.Value, className.Span.Start, false, nil)
		class := result.classes[className.Value]
		if classDeclaration == nil || classDeclaration.Kind != SymbolKindClass || class == nil || class.Aggregate == nil || class.Aggregate.Name != classDeclaration.Span {
			return nullGuard{}
		}
		return nullGuard{declaration: declaration, whenTrue: !negated}
	}
	var invalidateKnownTarget func(*syntax.Expression, *Scope)
	invalidateKnownTarget = func(target *syntax.Expression, scope *Scope) {
		if target == nil || scope == nil {
			return
		}
		switch target.Kind {
		case syntax.ExpressionIdentifier:
			delete(known, resolve(scope, target.Value, target.Span.Start, false, nil))
		case syntax.ExpressionList, syntax.ExpressionTuple, syntax.ExpressionParenthesized:
			for _, child := range target.Children {
				invalidateKnownTarget(child, scope)
			}
		}
	}
	commandKeepsKnown := func(command *syntax.Command) bool {
		if command.Kind == syntax.CommandBlockEnd {
			return true
		}
		if command.Autocmd != nil {
			return true
		}
		if command.Canonical == "" {
			return command.Kind == syntax.CommandExpression
		}
		switch command.Canonical {
		case "vim9script", "var", "const", "final", "echo", "if", "elseif", "else", "endif", "def", "enddef", "function", "endfunction", "class", "endclass", "interface", "endinterface", "enum", "endenum", "command", "augroup", "endaugroup":
			return true
		default:
			return false
		}
	}
	appendDiagnostics = func(expression *syntax.Expression, scope *Scope, dialect syntax.Dialect) {
		if expression == nil || scope == nil || seenExpressions[expression] || !result.analysisStep() {
			return
		}
		seenExpressions[expression] = true
		if dialect == syntax.Vim9 && expression.Kind == syntax.ExpressionMember && len(expression.Children) == 1 &&
			expression.Children[0] != nil && file.Text(expression.Operator) == "." && expression.Value != "" &&
			!expressionContainsMissing(expression) && !syntaxDiagnosticOverlaps(file.Diagnostics, expression.Span) {
			receiver := expression.Children[0]
			if isNullLiteral(receiver, "null_class") && !scopeUsesDefTypeRules(scope) {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1363", Message: "Incomplete type", Span: receiver.Span,
				})
			} else {
				null := isNullObject(receiver)
				if !null && receiver.Kind == syntax.ExpressionIdentifier {
					declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil)
					null = (candidates[declaration] && !written[declaration] || known[declaration]) && active[declaration] == 0
				}
				if null {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E1360", Message: "Using a null object", Span: receiver.Span,
					})
				}
			}
		}
		expressionScope := scope
		if expression.Kind == syntax.ExpressionLambda {
			if lambdaScope := result.lambdaScopes[expression]; lambdaScope != nil {
				expressionScope = lambdaScope
			}
			savedActive, savedKnown := active, known
			active = make(map[*Declaration]int)
			known = make(map[*Declaration]bool)
			defer func() {
				active = savedActive
				known = savedKnown
			}()
			if expression.LambdaBody != nil {
				walkCommands(expression.LambdaBody.Commands, expression.LambdaBody.Blocks, expressionScope, false)
			}
		}
		for index, child := range expression.Children {
			if expression.Kind != syntax.ExpressionLambda || index >= len(expression.Parameters) {
				appendDiagnostics(child, expressionScope, dialect)
			}
		}
		if expression.Kind == syntax.ExpressionAssignment && len(expression.Children) > 0 {
			invalidateKnownTarget(expression.Children[0], expressionScope)
		}
		if expression.Kind == syntax.ExpressionCall {
			clear(known)
		}
	}
	walkCommandExpressions := func(command *syntax.Command, scope *Scope) {
		if command == nil || scope == nil || !result.analysisStep() {
			return
		}
		if command.Mapping != nil {
			savedActive, savedKnown := active, known
			active = make(map[*Declaration]int)
			known = make(map[*Declaration]bool)
			defer func() {
				active = savedActive
				known = savedKnown
			}()
		}
		for _, expression := range command.Expressions {
			appendDiagnostics(expression, scope, command.Dialect)
		}
		for _, expression := range command.Targets {
			appendDiagnostics(expression, scope, command.Dialect)
		}
		if command.Mapping != nil {
			appendDiagnostics(command.Mapping.RHSExpression, scope, command.Dialect)
		}
		if command.Declaration != nil {
			appendDiagnostics(command.Declaration.Initializer, scope, command.Dialect)
			if len(command.Declaration.Bindings) == 1 {
				if declaration := declarations[command.Declaration.Bindings[0].Name]; candidates[declaration] && written[declaration] {
					known[declaration] = true
				}
			}
		}
		if command.For != nil {
			appendDiagnostics(command.For.Iterable, scope, command.Dialect)
		}
		if command.Import != nil {
			appendDiagnostics(command.Import.Path, scope, command.Dialect)
		}
		for _, value := range command.EnumValues {
			appendDiagnostics(value.Initializer, scope, command.Dialect)
			for _, argument := range value.Arguments {
				appendDiagnostics(argument, scope, command.Dialect)
			}
		}
		if command.Function != nil {
			savedActive, savedKnown := active, known
			active = make(map[*Declaration]int)
			known = make(map[*Declaration]bool)
			for _, parameter := range command.Function.Parameters {
				appendDiagnostics(parameter.Default, scope, command.Dialect)
			}
			active = savedActive
			known = savedKnown
		}
		keepsKnown := commandKeepsKnown(command)
		if command.Embedded != nil {
			if !keepsKnown {
				clear(known)
			}
			walkCommands(command.Embedded.Commands, command.Embedded.Blocks, scope, command.UserCommand != nil || command.Autocmd != nil)
		}
		if !keepsKnown {
			clear(known)
		}
	}
	walkSequence = func(commands []syntax.Command, blocks []syntax.Block, start, end int, fallback *Scope) {
		for index := start; index < end; {
			if !result.analysisStep() {
				return
			}
			command := &commands[index]
			scope := result.commandScopes[command]
			if scope == nil {
				scope = fallback
			}
			if !isBlockHeader(blocks, index, command.Block) {
				walkCommandExpressions(command, scope)
				index++
				continue
			}
			block := blocks[command.Block]
			resetsGuard := block.Kind == syntax.BlockDef || block.Kind == syntax.BlockFunction || block.Kind == syntax.BlockClass || block.Kind == syntax.BlockInterface || block.Kind == syntax.BlockEnum || block.Kind == syntax.BlockCommand
			if block.Kind != syntax.BlockIf {
				if block.Kind == syntax.BlockFor || block.Kind == syntax.BlockWhile || block.Kind == syntax.BlockTry {
					clear(known)
					walkCommandExpressions(command, scope)
					if block.End > index && block.End < len(commands) {
						walkSequence(commands, blocks, index+1, block.End, scope)
						clear(known)
						index = block.End
					} else {
						walkSequence(commands, blocks, index+1, end, scope)
						clear(known)
						index = end
					}
					continue
				}
				if !resetsGuard {
					walkCommandExpressions(command, scope)
					index++
					continue
				}
				savedActive, savedKnown := active, known
				active = make(map[*Declaration]int)
				known = make(map[*Declaration]bool)
				walkCommandExpressions(command, scope)
				if block.End > index && block.End < len(commands) {
					walkSequence(commands, blocks, index+1, block.End, scope)
					index = block.End
				} else {
					walkSequence(commands, blocks, index+1, end, scope)
					index = end
				}
				active = savedActive
				if block.Kind == syntax.BlockClass || block.Kind == syntax.BlockInterface || block.Kind == syntax.BlockEnum {
					clear(savedKnown)
				}
				known = savedKnown
				continue
			}
			valid := block.End > index && block.End < len(commands) && commands[block.End].Canonical == "endif" && headerComplete(command) && headerComplete(&commands[block.End])
			for branchIndex, branchHeader := range block.Branches {
				if !result.analysisStep() {
					return
				}
				if branchHeader < 0 || branchHeader >= block.End || !headerComplete(&commands[branchHeader]) ||
					commands[branchHeader].Canonical != "elseif" && commands[branchHeader].Canonical != "else" ||
					commands[branchHeader].Canonical == "else" && branchIndex != len(block.Branches)-1 {
					valid = false
					break
				}
			}
			if !valid {
				walkCommandExpressions(command, scope)
				index++
				continue
			}
			branchHeader := index
			for branchIndex := 0; ; branchIndex++ {
				branch := &commands[branchHeader]
				branchScope := result.commandScopes[branch]
				if branchScope == nil {
					branchScope = scope
				}
				walkCommandExpressions(branch, branchScope)
				branchEnd := block.End
				if branchIndex < len(block.Branches) {
					branchEnd = block.Branches[branchIndex]
				}
				guard := nullGuard{}
				if branch.Canonical == "if" || branch.Canonical == "elseif" {
					if branch.Dialect == syntax.Vim9 && len(branch.Expressions) == 1 {
						guard = guardFor(branch.Expressions[0], branchScope)
					}
				} else if len(block.Branches) == 1 && commands[block.Branches[0]].Canonical == "else" && len(commands[index].Expressions) == 1 && commands[index].Dialect == syntax.Vim9 {
					guard = guardFor(commands[index].Expressions[0], scope)
					guard.whenTrue = !guard.whenTrue
				}
				if guard.declaration != nil && guard.whenTrue {
					active[guard.declaration]++
				}
				walkSequence(commands, blocks, branchHeader+1, branchEnd, branchScope)
				if guard.declaration != nil && guard.whenTrue {
					active[guard.declaration]--
					if active[guard.declaration] == 0 {
						delete(active, guard.declaration)
					}
				}
				if branchIndex == len(block.Branches) {
					break
				}
				branchHeader = block.Branches[branchIndex]
			}
			index = block.End
			continue
		}
	}
	walkCommands = func(commands []syntax.Command, blocks []syntax.Block, fallback *Scope, reset bool) {
		if fallback == nil {
			fallback = result.Root
		}
		if reset {
			savedActive, savedKnown := active, known
			active = make(map[*Declaration]int)
			known = make(map[*Declaration]bool)
			defer func() {
				active = savedActive
				known = savedKnown
			}()
		}
		walkSequence(commands, blocks, 0, len(commands), fallback)
	}
	walkCommands(file.Commands, file.Blocks, result.Root, false)
}
