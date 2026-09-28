package analysis

import (
	"sort"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func collectOpaqueEnumDeclarations(result *FileAnalysis, commands []syntax.Command, blocks []syntax.Block) {
	if result == nil || result.File == nil {
		return
	}
	for index := range commands {
		command := &commands[index]
		if len(command.EnumValues) == 0 && command.Canonical != "" && command.Canonical != "endenum" && command.Block >= 0 && command.Block < len(blocks) && blocks[command.Block].Kind == syntax.BlockEnum && blocks[command.Block].Header != index {
			if scope := result.commandScopes[command]; scope != nil && validNameSpan(result.File, command.Name) {
				addDeclaration(result, scope, result.File, command.Name, SymbolKindEnumMember, false)
			}
		}
		if command.Embedded != nil {
			collectOpaqueEnumDeclarations(result, command.Embedded.Commands, command.Embedded.Blocks)
		}
	}
}

func collectCommandScopes(result *FileAnalysis, parent *Scope, commands []syntax.Command, blocks []syntax.Block, list *syntax.CommandList) {
	if !result.analysisStep() {
		return
	}
	if result == nil || parent == nil {
		return
	}
	byBlock := make(map[int][]*Scope, len(blocks))
	blockScope := func(block, offset int) *Scope {
		scopes := byBlock[block]
		for index := len(scopes) - 1; index >= 0; index-- {
			if offset >= scopes[index].Span.Start {
				return scopes[index]
			}
		}
		return nil
	}
	for index, block := range blocks {
		if !result.analysisStep() {
			return
		}
		blockParent := parent
		if block.Parent >= 0 {
			if candidate := blockScope(block.Parent, block.Span.Start); candidate != nil {
				blockParent = candidate
			}
		}
		// Vim9 ends the previous branch's local scope at elseif/else and
		// catch/finally. These regions are siblings, including for nested
		// blocks whose parent must be the branch containing their header.
		starts := []int{block.Span.Start}
		if block.Header >= 0 && block.Header < len(commands) && commands[block.Header].Dialect == syntax.Vim9 {
			for _, branch := range block.Branches {
				if branch >= 0 && branch < len(commands) && commands[branch].Span.Start > starts[len(starts)-1] {
					starts = append(starts, commands[branch].Span.Start)
				}
			}
		}
		for branch, start := range starts {
			end := block.Span.End
			if branch+1 < len(starts) {
				end = starts[branch+1]
			}
			scope := &Scope{Block: index, Kind: block.Kind, Span: syntax.Span{Start: start, End: end}, Parent: blockParent, CommandList: list}
			blockParent.Children = append(blockParent.Children, scope)
			byBlock[index] = append(byBlock[index], scope)
			result.Scopes = append(result.Scopes, scope)
		}
	}
	for index := range commands {
		command := &commands[index]
		scope := parent
		if !result.analysisStep() {
			return
		}
		if candidate := blockScope(command.Block, command.Span.Start); candidate != nil {
			scope = candidate
		}
		result.commandScopes[command] = scope
		if command.Embedded != nil {
			collectCommandScopes(result, scope, command.Embedded.Commands, command.Embedded.Blocks, command.Embedded)
		}
	}
}

func collectEmbeddedDeclarations(result *FileAnalysis, parent *Scope, commands []syntax.Command) {
	if result == nil || parent == nil {
		return
	}
	for index := range commands {
		command := &commands[index]
		scope := result.commandScopes[command]
		if scope == nil {
			scope = parent
		}
		collectCommandDeclarations(result, command, scope)
		if command.Embedded != nil {
			collectEmbeddedDeclarations(result, scope, command.Embedded.Commands)
		}
	}
}

// collectLambdaScopesCommands discovers expression-owned lexical regions before
// declarations and references are collected.  Lambda block files have already
// been rebased by syntax to the containing source, so their command scopes can
// be attached directly to the lambda scope.
func collectLambdaScopesCommands(result *FileAnalysis, parent *Scope, commands []syntax.Command) {
	if result == nil || parent == nil {
		return
	}
	for index := range commands {
		command := &commands[index]
		commandScope := result.commandScopes[command]
		if commandScope == nil {
			commandScope = parent
		}
		for _, expression := range command.Expressions {
			collectLambdaScopes(result, commandScope, expression)
		}
		if command.Mapping != nil {
			collectLambdaScopes(result, commandScope, command.Mapping.RHSExpression)
		}
		for _, expression := range command.Targets {
			collectLambdaScopes(result, commandScope, expression)
		}
		if command.Declaration != nil {
			collectLambdaScopes(result, commandScope, command.Declaration.Initializer)
		}
		if command.For != nil {
			collectLambdaScopes(result, commandScope, command.For.Iterable)
		}
		if command.Import != nil {
			collectLambdaScopes(result, commandScope, command.Import.Path)
		}
		for _, value := range command.EnumValues {
			collectLambdaScopes(result, commandScope, value.Initializer)
			for _, argument := range value.Arguments {
				collectLambdaScopes(result, commandScope, argument)
			}
		}
		if command.Function != nil {
			for _, parameter := range command.Function.Parameters {
				collectLambdaScopes(result, commandScope, parameter.Default)
			}
		}
		if command.Embedded != nil {
			collectLambdaScopesCommands(result, commandScope, command.Embedded.Commands)
		}
	}
}

func collectLambdaScopes(result *FileAnalysis, parent *Scope, expression *syntax.Expression) {
	if result == nil || parent == nil || expression == nil {
		return
	}
	if expression.Kind == syntax.ExpressionLambda {
		if existing := result.lambdaScopes[expression]; existing != nil {
			return
		}
		lambdaScope := &Scope{Block: -1, Span: expression.Span, Parent: parent, Lambda: expression}
		parent.Children = append(parent.Children, lambdaScope)
		result.Scopes = append(result.Scopes, lambdaScope)
		result.lambdaScopes[expression] = lambdaScope
		for _, parameter := range expression.Parameters {
			addParameterDeclaration(result, lambdaScope, result.File, parameter.Name)
		}
		if expression.LambdaBody != nil {
			collectCommandScopes(result, lambdaScope, expression.LambdaBody.Commands, expression.LambdaBody.Blocks, nil)
			collectLambdaScopesCommands(result, lambdaScope, expression.LambdaBody.Commands)
		}
		// Parameter children are declaration sites.  Walking them would only
		// rediscover the same names, while the remaining children contain the
		// expression body or the marker for a block body.
		for index, child := range expression.Children {
			if index < len(expression.Parameters) {
				continue
			}
			collectLambdaScopes(result, lambdaScope, child)
		}
		return
	}
	for _, child := range expression.Children {
		collectLambdaScopes(result, parent, child)
	}
}

func collectLambdaDeclarations(result *FileAnalysis) {
	if result == nil {
		return
	}
	for _, scope := range result.Scopes {
		if !result.analysisStep() {
			return
		}
		lambda := scope.Lambda
		if lambda == nil || lambda.LambdaBody == nil || result.lambdaBodies[lambda] {
			continue
		}
		result.lambdaBodies[lambda] = true
		collectEmbeddedDeclarations(result, scope, lambda.LambdaBody.Commands)
		collectOpaqueEnumDeclarations(result, lambda.LambdaBody.Commands, lambda.LambdaBody.Blocks)
	}
}

func collectCommandDeclarations(result *FileAnalysis, command *syntax.Command, commandScope *Scope) {
	file := result.File
	if command == nil || commandScope == nil || file == nil {
		return
	}
	if command.Function != nil {
		functionScope := commandScope
		declarationScope := functionScope
		if functionScope.Kind == syntax.BlockFunction || functionScope.Kind == syntax.BlockDef {
			declarationScope = functionScope.Parent
			if declarationScope == nil {
				declarationScope = functionScope
			}
		}
		if !emptySyntaxSpan(command.Function.Name) {
			declaration := addDeclaration(result, declarationScope, file, command.Function.Name, functionKind(file, command, declarationScope), false)
			if declaration != nil {
				declaration.Deprecated = hasDeprecatedComment(file, command)
				declaration.TypeParameterCount = len(command.Function.TypeParameters)
			}
		}
		for _, parameter := range command.Function.Parameters {
			addParameterDeclaration(result, functionScope, file, parameterDeclarationSpan(file, parameter))
		}
	}
	if command.Aggregate != nil {
		if kind := aggregateSymbolKind(command.Aggregate.Kind); kind != "" {
			addDeclaration(result, declarationParent(commandScope), file, command.Aggregate.Name, kind, false)
		}
	}
	if command.TypeAlias != nil {
		if command.Dialect == syntax.Vim9 && scopeContainsDef(commandScope) {
			span := enclosingDefHeaderSpan(file, commandScope)
			if emptySyntaxSpan(span) {
				span = command.Name
			}
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1399", Message: "Type can only be used in a script", Span: span,
			})
			return
		}
		addDeclaration(result, commandScope, file, command.TypeAlias.Name, SymbolKindTypeAlias, false)
	}
	if command.Import != nil {
		addDeclaration(result, commandScope, file, ImportDeclarationSpan(file, command.Import), SymbolKindImport, false)
	}
	if command.Declaration != nil && !ordinaryContainerAssignment(command) {
		mutable := command.Canonical != "const" && command.Canonical != "final"
		kind := SymbolKindVariable
		if !mutable {
			kind = SymbolKindConstant
		}
		for _, binding := range command.Declaration.Bindings {
			if ignoredDestructuringBinding(file, command, binding.Name) {
				continue
			}
			declaration := addDeclaration(result, commandScope, file, binding.Name, kind, mutable)
			if declaration != nil {
				declaration.Deprecated = hasDeprecatedComment(file, command)
				declaration.unusedCandidate = command.Dialect == syntax.Vim9 && !commandHasModifier(command, "export") && unusedVariableScope(commandScope)
				if command.Canonical == "const" {
					declaration.constBinding = true
				}
			}
		}
	}
	if command.For != nil {
		mutable := command.Dialect != syntax.Vim9
		kind := SymbolKindVariable
		if !mutable {
			kind = SymbolKindConstant
		}
		for _, binding := range command.For.Bindings {
			if ignoredDestructuringBinding(file, command, binding.Name) {
				continue
			}
			declaration := addDeclaration(result, commandScope, file, binding.Name, kind, mutable)
			if declaration != nil {
				declaration.unusedCandidate = command.Dialect == syntax.Vim9 && unusedVariableScope(commandScope)
			}
		}
	}
	for _, value := range command.EnumValues {
		addDeclaration(result, commandScope, file, value.Name, SymbolKindEnumMember, false)
	}
}

func ignoredDestructuringBinding(file *syntax.File, command *syntax.Command, name syntax.Span) bool {
	if command.Dialect != syntax.Vim9 || file.Text(name) != "_" {
		return false
	}
	if forLoopDestructures(file, command) {
		return true
	}
	return command.Declaration != nil && command.Declaration.Target != nil &&
		(command.Declaration.Target.Kind == syntax.ExpressionList || command.Declaration.Target.Kind == syntax.ExpressionTuple)
}

// parameterDeclarationSpan returns the lexical name introduced by a function
// parameter.  Vim9 constructor shorthand spells this as this.member, but the
// local parameter is named member; Target is a declaration target, not an
// expression reference.
func parameterDeclarationSpan(file *syntax.File, parameter syntax.Parameter) syntax.Span {
	if target := parameter.Target; target != nil && target.Kind == syntax.ExpressionMember &&
		validNameSpan(file, target.Span) && validNameSpan(file, target.Operator) &&
		target.Operator.Start >= target.Span.Start && target.Operator.End <= target.Span.End {
		return syntax.Span{Start: target.Operator.End, End: target.Span.End}
	}
	return parameter.Name
}

func declarationParent(scope *Scope) *Scope {
	if scope != nil && scope.Parent != nil {
		return scope.Parent
	}
	return scope
}

func functionKind(file *syntax.File, command *syntax.Command, parent *Scope) SymbolKind {
	if parent != nil && (parent.Kind == syntax.BlockClass || parent.Kind == syntax.BlockInterface) {
		name := file.Text(command.Function.Name)
		if dot := strings.LastIndex(name, "."); dot >= 0 {
			name = name[dot+1:]
		}
		if name == "new" {
			return SymbolKindConstructor
		}
		return SymbolKindMethod
	}
	return SymbolKindFunction
}

func aggregateSymbolKind(kind syntax.BlockKind) SymbolKind {
	switch kind {
	case syntax.BlockClass:
		return SymbolKindClass
	case syntax.BlockInterface:
		return SymbolKindInterface
	case syntax.BlockEnum:
		return SymbolKindEnum
	default:
		return ""
	}
}

func addDeclaration(result *FileAnalysis, scope *Scope, file *syntax.File, span syntax.Span, kind SymbolKind, mutable bool) *Declaration {
	if result == nil || scope == nil || file == nil || !validNameSpan(file, span) {
		return nil
	}
	declaration := &Declaration{Name: file.Text(span), Kind: kind, Span: span, Mutable: mutable, Scope: scope}
	if kind == SymbolKindImport {
		result.hasImports = true
	}
	scope.Declarations = append(scope.Declarations, declaration)
	result.Declarations = append(result.Declarations, declaration)
	return declaration
}

func addParameterDeclaration(result *FileAnalysis, scope *Scope, file *syntax.File, span syntax.Span) *Declaration {
	declaration := addDeclaration(result, scope, file, span, SymbolKindVariable, true)
	if declaration != nil {
		declaration.Parameter = true
	}
	return declaration
}

func sortDeclarations(result *FileAnalysis) {
	less := func(left, right *Declaration) bool {
		if left.Span.Start != right.Span.Start {
			return left.Span.Start < right.Span.Start
		}
		return left.Span.End < right.Span.End
	}
	sort.SliceStable(result.Declarations, func(i, j int) bool { return less(result.Declarations[i], result.Declarations[j]) })
	for _, scope := range result.Scopes {
		sort.SliceStable(scope.Declarations, func(i, j int) bool { return less(scope.Declarations[i], scope.Declarations[j]) })
	}
}
