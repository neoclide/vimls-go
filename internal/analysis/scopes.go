package analysis

import (
	"sort"
	"strings"

	"github.com/chemzqm/vimls-go/internal/syntax"
)

// FileAnalysis is the protocol-independent lexical information collected from
// one syntax tree.  All spans are byte spans in File.Source.
type FileAnalysis struct {
	File            *syntax.File
	Root            *Scope
	Scopes          []*Scope
	Declarations    []*Declaration
	References      []*Reference
	expressionTypes map[*syntax.Expression]ValueType
	commandScopes   map[*syntax.Command]*Scope
	lambdaScopes    map[*syntax.Expression]*Scope
	lambdaBodies    map[*syntax.Expression]bool
}

// Scope is a lexical region. Root has Block == -1 and an empty Kind. Other
// scopes correspond to a block in either the top-level syntax.File or an
// embedded CommandList; CommandList identifies the latter's local index.
type Scope struct {
	Block        int
	Kind         syntax.BlockKind
	Span         syntax.Span
	Parent       *Scope
	Children     []*Scope
	Declarations []*Declaration
	// CommandList identifies the syntax list whose local Block index is used
	// by this scope.  It is nil for blocks in the top-level syntax.File.
	CommandList *syntax.CommandList
	// Lambda identifies an expression-owned lexical scope.  Lambda scopes use
	// Block == -1 because they are not syntax command blocks.
	Lambda *syntax.Expression
}

// Declaration is a name introduced in a scope.  Mutable is false for
// constants and for declarations that cannot be assigned to (functions,
// types, imports, and aggregate members).
type Declaration struct {
	Name    string
	Kind    SymbolKind
	Span    syntax.Span
	Mutable bool
	Scope   *Scope
	Type    ValueType
}

// Reference is an identifier occurrence.  Declaration is nil when the name
// is dynamic, explicitly scoped to a different namespace, or not visible yet.
type Reference struct {
	Name        string
	Span        syntax.Span
	Declaration *Declaration
}

// Analyze collects lexical scopes, declarations, and same-file references.
// It deliberately does not report undefined names: an unresolved reference
// is a valid result for dynamic legacy Vim script and for incomplete input.
func Analyze(file *syntax.File) *FileAnalysis {
	result := &FileAnalysis{File: file}
	root := &Scope{Block: -1}
	if file != nil {
		root.Span = syntax.Span{End: len(file.Source)}
	}
	result.Root = root
	result.Scopes = []*Scope{root}
	if file == nil {
		return result
	}

	result.commandScopes = make(map[*syntax.Command]*Scope)
	result.lambdaScopes = make(map[*syntax.Expression]*Scope)
	result.lambdaBodies = make(map[*syntax.Expression]bool)
	collectCommandScopes(result, root, file.Commands, file.Blocks, nil)
	collectLambdaScopesCommands(result, root, file.Commands)

	// First collect every declaration.  This is separate from reference
	// walking so a function can be referenced before its definition, as Vim
	// permits, without making variables forward-visible.
	collectEmbeddedDeclarations(result, root, file.Commands)
	collectLambdaDeclarations(result, file.Commands)

	// A malformed or partially parsed enum value may remain an opaque command.
	// The enum block is still authoritative for its one-name-per-line members.
	collectOpaqueEnumDeclarations(result, file.Commands, file.Blocks)

	sortDeclarations(result)
	for index := range file.Commands {
		command := &file.Commands[index]
		scope := result.commandScopes[command]
		if scope == nil {
			scope = root
		}
		walkCommand(result, file, command, scope)
	}
	sort.SliceStable(result.References, func(i, j int) bool {
		return result.References[i].Span.Start < result.References[j].Span.Start
	})
	inferTypes(result)
	return result
}

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
	if result == nil || parent == nil {
		return
	}
	byBlock := make(map[int]*Scope, len(blocks))
	for index, block := range blocks {
		blockParent := parent
		if block.Parent >= 0 {
			if candidate := byBlock[block.Parent]; candidate != nil {
				blockParent = candidate
			}
		}
		scope := &Scope{Block: index, Kind: block.Kind, Span: block.Span, Parent: blockParent, CommandList: list}
		blockParent.Children = append(blockParent.Children, scope)
		byBlock[index] = scope
		result.Scopes = append(result.Scopes, scope)
	}
	for index := range commands {
		command := &commands[index]
		scope := parent
		if candidate := byBlock[command.Block]; candidate != nil {
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
			addDeclaration(result, lambdaScope, result.File, parameter.Name, SymbolKindVariable, true)
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

func collectLambdaDeclarations(result *FileAnalysis, commands []syntax.Command) {
	if result == nil {
		return
	}
	for index := range commands {
		command := &commands[index]
		collectLambdaDeclarationsExpressions(result, command.Expressions)
		if command.Mapping != nil {
			collectLambdaDeclarationsExpression(result, command.Mapping.RHSExpression)
		}
		collectLambdaDeclarationsExpressions(result, command.Targets)
		if command.Declaration != nil {
			collectLambdaDeclarationsExpression(result, command.Declaration.Initializer)
		}
		if command.For != nil {
			collectLambdaDeclarationsExpression(result, command.For.Iterable)
		}
		if command.Import != nil {
			collectLambdaDeclarationsExpression(result, command.Import.Path)
		}
		for _, value := range command.EnumValues {
			collectLambdaDeclarationsExpression(result, value.Initializer)
			collectLambdaDeclarationsExpressions(result, value.Arguments)
		}
		if command.Function != nil {
			for _, parameter := range command.Function.Parameters {
				collectLambdaDeclarationsExpression(result, parameter.Default)
			}
		}
		if command.Embedded != nil {
			collectLambdaDeclarations(result, command.Embedded.Commands)
		}
	}
}

func collectLambdaDeclarationsExpressions(result *FileAnalysis, expressions []*syntax.Expression) {
	for _, expression := range expressions {
		collectLambdaDeclarationsExpression(result, expression)
	}
}

func collectLambdaDeclarationsExpression(result *FileAnalysis, expression *syntax.Expression) {
	if expression == nil {
		return
	}
	if expression.Kind == syntax.ExpressionLambda {
		if scope := result.lambdaScopes[expression]; scope != nil && expression.LambdaBody != nil && !result.lambdaBodies[expression] {
			result.lambdaBodies[expression] = true
			collectEmbeddedDeclarations(result, scope, expression.LambdaBody.Commands)
			collectOpaqueEnumDeclarations(result, expression.LambdaBody.Commands, expression.LambdaBody.Blocks)
			collectLambdaDeclarations(result, expression.LambdaBody.Commands)
		}
		for index, child := range expression.Children {
			if index >= len(expression.Parameters) {
				collectLambdaDeclarationsExpression(result, child)
			}
		}
		return
	}
	for _, child := range expression.Children {
		collectLambdaDeclarationsExpression(result, child)
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
			addDeclaration(result, declarationScope, file, command.Function.Name, functionKind(file, command, declarationScope), false)
		}
		for _, parameter := range command.Function.Parameters {
			addDeclaration(result, functionScope, file, parameter.Name, SymbolKindVariable, true)
		}
	}
	if command.Aggregate != nil {
		if kind := aggregateSymbolKind(command.Aggregate.Kind); kind != "" {
			addDeclaration(result, declarationParent(commandScope), file, command.Aggregate.Name, kind, false)
		}
	}
	if command.TypeAlias != nil {
		addDeclaration(result, commandScope, file, command.TypeAlias.Name, SymbolKindTypeAlias, false)
	}
	if command.Import != nil {
		addDeclaration(result, commandScope, file, command.Import.Alias, SymbolKindImport, false)
	}
	if command.Declaration != nil {
		mutable := command.Canonical != "const" && command.Canonical != "final"
		kind := SymbolKindVariable
		if !mutable {
			kind = SymbolKindConstant
		}
		for _, binding := range command.Declaration.Bindings {
			addDeclaration(result, commandScope, file, binding.Name, kind, mutable)
		}
	}
	if command.For != nil {
		for _, binding := range command.For.Bindings {
			addDeclaration(result, commandScope, file, binding.Name, SymbolKindVariable, true)
		}
	}
	for _, value := range command.EnumValues {
		addDeclaration(result, commandScope, file, value.Name, SymbolKindEnumMember, false)
	}
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
	scope.Declarations = append(scope.Declarations, declaration)
	result.Declarations = append(result.Declarations, declaration)
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

func walkCommand(result *FileAnalysis, file *syntax.File, command *syntax.Command, scope *Scope) {
	if command == nil || scope == nil {
		return
	}
	if command.Function != nil {
		functionScope := scope
		if functionScope.Block >= 0 && functionScope.Kind != syntax.BlockFunction && functionScope.Kind != syntax.BlockDef {
			functionScope = scope
		}
		for _, parameter := range command.Function.Parameters {
			if parameter.Default != nil {
				walkExpression(result, file, parameter.Default, functionScope, nil, false)
			}
		}
	}
	if command.Import != nil && command.Import.Path != nil {
		walkExpression(result, file, command.Import.Path, scope, nil, false)
	}
	for _, value := range command.EnumValues {
		skip := map[syntax.Span]bool{value.Name: true}
		if value.Initializer != nil {
			walkExpression(result, file, value.Initializer, scope, skip, false)
		} else {
			// Arguments are also children of Initializer for constructor-style
			// enum values.  Walk them only when the recovering AST has no
			// initializer, otherwise references would be duplicated.
			for _, argument := range value.Arguments {
				walkExpression(result, file, argument, scope, skip, false)
			}
		}
	}
	for _, expression := range command.Expressions {
		skip := map[syntax.Span]bool(nil)
		if command.Declaration != nil {
			skip = make(map[syntax.Span]bool, len(command.Declaration.Bindings))
			for _, binding := range command.Declaration.Bindings {
				skip[binding.Name] = true
			}
		}
		walkExpression(result, file, expression, scope, skip, false)
	}
	if command.Mapping != nil {
		walkExpression(result, file, command.Mapping.RHSExpression, scope, nil, false)
	}
	if command.Canonical != "++" && command.Canonical != "--" {
		for _, target := range command.Targets {
			walkExpression(result, file, target, scope, nil, false)
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

func walkExpression(result *FileAnalysis, file *syntax.File, expression *syntax.Expression, scope *Scope, skipped map[syntax.Span]bool, preferFunction bool) {
	if expression == nil || scope == nil || file == nil {
		return
	}
	switch expression.Kind {
	case syntax.ExpressionIdentifier, syntax.ExpressionCurlyName:
		if expression.Kind == syntax.ExpressionIdentifier && !isLiteralIdentifier(expression.Value) && !skipped[expression.Span] && validNameSpan(file, expression.Span) {
			result.References = append(result.References, &Reference{
				Name: expression.Value, Span: expression.Span,
				Declaration: resolve(scope, expression.Value, expression.Span.Start, preferFunction, skipped),
			})
		}
		for _, child := range expression.Children {
			walkExpression(result, file, child, scope, skipped, false)
		}
	case syntax.ExpressionMember:
		// Value is the member spelling, not a lexical variable.  Only the
		// receiver expression participates in same-file resolution.
		if len(expression.Children) > 0 {
			walkExpression(result, file, expression.Children[0], scope, skipped, false)
		}
	case syntax.ExpressionDictionary:
		for index, child := range expression.Children {
			// A plain dictionary key is syntax, not a variable reference.  A
			// computed key has a non-identifier node and is walked normally.
			if index%2 == 0 && child != nil && child.Kind == syntax.ExpressionIdentifier {
				continue
			}
			walkExpression(result, file, child, scope, skipped, false)
		}
	case syntax.ExpressionCall:
		for index, child := range expression.Children {
			walkExpression(result, file, child, scope, skipped, index == 0)
		}
	case syntax.ExpressionGenericReference:
		for index, child := range expression.Children {
			walkExpression(result, file, child, scope, skipped, index == 0)
		}
	case syntax.ExpressionLambda:
		lambdaScope := result.lambdaScopes[expression]
		if lambdaScope == nil {
			lambdaScope = scope
		}
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
			walkExpression(result, file, child, lambdaScope, skipped, false)
		}
	default:
		for _, child := range expression.Children {
			walkExpression(result, file, child, scope, skipped, false)
		}
	}
}

func resolve(scope *Scope, name string, offset int, preferFunction bool, hidden map[syntax.Span]bool) *Declaration {
	if name == "" {
		return nil
	}
	for current := scope; current != nil; current = current.Parent {
		var latest *Declaration
		var forwardFunction *Declaration
		for _, declaration := range current.Declarations {
			if declaration.Name != name || hidden[declaration.Span] {
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
	}
	return nil
}

func validNameSpan(file *syntax.File, span syntax.Span) bool {
	return file != nil && span.Start >= 0 && span.Start < span.End && span.End <= len(file.Source)
}

func isLiteralIdentifier(name string) bool {
	switch strings.ToLower(name) {
	case "true", "false", "null", "null_blob", "null_channel", "null_class", "null_dict", "null_function", "null_job", "null_list", "null_object", "null_partial", "null_string":
		return true
	default:
		return false
	}
}

func emptySyntaxSpan(span syntax.Span) bool {
	return span.Start >= span.End
}
