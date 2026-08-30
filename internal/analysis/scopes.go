package analysis

import (
	"sort"
	"strconv"
	"strings"

	"github.com/chemzqm/vimls-go/internal/syntax"
	"github.com/chemzqm/vimls-go/internal/vimdata"
)

// FileAnalysis is the protocol-independent lexical information collected from
// one syntax tree.  All spans are byte spans in File.Source.
type FileAnalysis struct {
	File         *syntax.File
	Root         *Scope
	Scopes       []*Scope
	Declarations []*Declaration
	References   []*Reference
	// Diagnostics contains protocol-independent semantic diagnostics with byte
	// spans in File.Source.
	Diagnostics     []syntax.Diagnostic
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
	collectBuiltinArgumentTypeDiagnostics(result, file.Commands, root)
	collectImmutableAssignmentDiagnostics(result, file.Commands, root)
	sort.SliceStable(result.Diagnostics, func(i, j int) bool {
		return result.Diagnostics[i].Span.Start < result.Diagnostics[j].Span.Start
	})
	return result
}

// collectImmutableAssignmentDiagnostics reports only direct Vim9 assignment to
// a lexically resolved const/final name.  Other read-only declarations have
// distinct Vim rules, and dynamic targets deliberately remain opaque here.
func collectImmutableAssignmentDiagnostics(result *FileAnalysis, commands []syntax.Command, parent *Scope) {
	if result == nil || result.File == nil {
		return
	}
	for index := range commands {
		command := &commands[index]
		scope := result.commandScopes[command]
		if scope == nil {
			scope = parent
		}
		if command.Dialect == syntax.Vim9 && command.Declaration == nil {
			for _, expression := range command.Expressions {
				collectImmutableAssignmentExpressionDiagnostics(result, scope, expression)
			}
		}
		if command.Embedded != nil {
			collectImmutableAssignmentDiagnostics(result, command.Embedded.Commands, scope)
		}
	}
}

func collectImmutableAssignmentExpressionDiagnostics(result *FileAnalysis, scope *Scope, expression *syntax.Expression) {
	if expression == nil || scope == nil {
		return
	}
	if expression.Kind == syntax.ExpressionLambda {
		lambdaScope := result.lambdaScopes[expression]
		if lambdaScope == nil {
			lambdaScope = scope
		}
		if expression.LambdaBody != nil {
			collectImmutableAssignmentDiagnostics(result, expression.LambdaBody.Commands, lambdaScope)
		}
		for index, child := range expression.Children {
			if index >= len(expression.Parameters) {
				collectImmutableAssignmentExpressionDiagnostics(result, lambdaScope, child)
			}
		}
		return
	}
	if expression.Kind == syntax.ExpressionAssignment && expression.Value == "=" && result.File.Text(expression.Operator) == "=" && len(expression.Children) >= 2 && expression.Children[1] != nil && expression.Children[1].Kind != syntax.ExpressionMissing {
		target := expression.Children[0]
		if target != nil && target.Kind == syntax.ExpressionIdentifier && !strings.Contains(target.Value, ":") && validNameSpan(result.File, target.Span) {
			if declaration := resolve(scope, target.Value, target.Span.Start, false, nil); declaration != nil && declaration.Kind == SymbolKindConstant {
				diagnostic := syntax.Diagnostic{Span: target.Span}
				if scopeContainsDef(scope) {
					diagnostic.Code = "vim/E1018"
					diagnostic.Message = "Cannot assign to a constant: " + target.Value
				} else {
					diagnostic.Code = "vim/E46"
					diagnostic.Message = "Cannot change read-only variable \"" + target.Value + "\""
				}
				result.Diagnostics = append(result.Diagnostics, diagnostic)
			}
		}
	}
	for _, child := range expression.Children {
		collectImmutableAssignmentExpressionDiagnostics(result, scope, child)
	}
}

func scopeContainsDef(scope *Scope) bool {
	for current := scope; current != nil; current = current.Parent {
		if current.Kind == syntax.BlockDef {
			return true
		}
	}
	return false
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
			addDeclaration(result, functionScope, file, parameterDeclarationSpan(file, parameter), SymbolKindVariable, true)
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
		mutable := command.Dialect != syntax.Vim9
		kind := SymbolKindVariable
		if !mutable {
			kind = SymbolKindConstant
		}
		for _, binding := range command.For.Bindings {
			addDeclaration(result, commandScope, file, binding.Name, kind, mutable)
		}
	}
	for _, value := range command.EnumValues {
		addDeclaration(result, commandScope, file, value.Name, SymbolKindEnumMember, false)
	}
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
		skip := map[syntax.Span]bool(nil)
		if command.Declaration != nil {
			skip = make(map[syntax.Span]bool, len(command.Declaration.Bindings))
			for _, binding := range command.Declaration.Bindings {
				skip[binding.Name] = true
			}
		}
		walkExpression(result, file, expression, scope, skip, false, command.Dialect)
	}
	if command.Mapping != nil {
		walkExpression(result, file, command.Mapping.RHSExpression, scope, nil, false, command.Dialect)
	}
	if command.Canonical != "++" && command.Canonical != "--" {
		for _, target := range command.Targets {
			walkExpression(result, file, target, scope, nil, false, command.Dialect)
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
	if expression == nil || scope == nil || file == nil {
		return
	}
	switch expression.Kind {
	case syntax.ExpressionIdentifier, syntax.ExpressionCurlyName:
		if expression.Kind == syntax.ExpressionIdentifier && !isLiteralIdentifier(expression.Value) && !skipped[expression.Span] && validNameSpan(file, expression.Span) {
			declaration := resolve(scope, expression.Value, expression.Span.Start, preferFunction, skipped)
			result.References = append(result.References, &Reference{
				Name: expression.Value, Span: expression.Span,
				Declaration: declaration,
			})
			if declaration == nil && !preferFunction && dialect == syntax.Vim9 && scopeContainsDef(scope) && !strings.Contains(expression.Value, ":") && expression.Value != "this" && expression.Value != "super" {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1001", Message: "Variable not found: " + expression.Value, Span: expression.Span,
				})
			}
		}
		for _, child := range expression.Children {
			walkExpression(result, file, child, scope, skipped, false, dialect)
		}
	case syntax.ExpressionMember:
		// Value is the member spelling, not a lexical variable.  Only the
		// receiver expression participates in same-file resolution.
		if len(expression.Children) > 0 {
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
		collectBuiltinCallArityDiagnostic(result, file, expression)
		for index, child := range expression.Children {
			walkExpression(result, file, child, scope, skipped, index == 0, dialect)
		}
	case syntax.ExpressionGenericReference:
		for index, child := range expression.Children {
			walkExpression(result, file, child, scope, skipped, index == 0, dialect)
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
			walkExpression(result, file, child, lambdaScope, skipped, false, dialect)
		}
	default:
		for _, child := range expression.Children {
			walkExpression(result, file, child, scope, skipped, false, dialect)
		}
	}
}

type builtinArgumentType struct {
	display     string
	kinds       builtinValueKind
	elementKind builtinValueKind
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
)

func builtinValueTypeKind(typ ValueType) builtinValueKind {
	switch typ.Name {
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
		return makeType("string or function", builtinString|builtinFunc)
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
	if expected.elementKind == 0 || actual.Name == "string" || len(actual.Arguments) == 0 || isUnknownType(actual.Arguments[0]) {
		return false
	}
	actualElement := builtinValueTypeKind(actual.Arguments[0])
	return actualElement != 0 && expected.elementKind&actualElement == 0
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

func collectBuiltinArgumentTypeDiagnostics(result *FileAnalysis, commands []syntax.Command, parent *Scope) {
	seen := make(map[*syntax.Expression]bool)
	var walkCommands func([]syntax.Command, *Scope)
	var walk func(*syntax.Expression, *Scope, syntax.Dialect)
	walk = func(expression *syntax.Expression, scope *Scope, dialect syntax.Dialect) {
		if expression == nil || seen[expression] {
			return
		}
		seen[expression] = true
		if expression.Kind == syntax.ExpressionCall && dialect == syntax.Vim9 && !expressionContainsMissing(expression) && !syntaxDiagnosticTouchesCall(result.File.Diagnostics, expression.Span) {
			if builtin, arguments, ok := builtinCallArguments(result.File, expression); ok {
				actual := make([]ValueType, len(arguments))
				for index, argument := range arguments {
					actual[index] = result.TypeOf(argument)
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
					expected, ok := builtinArgumentExpectation(builtin.ArgumentChecks[checkerIndex], actual, index)
					if !ok {
						continue
					}
					if builtinArgumentMismatch(actual[index], expected) {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1013", Message: "Argument " + strconv.Itoa(index+1) + ": type mismatch, expected " + expected.display + " but got " + valueTypeDisplay(actual[index]), Span: argument.Span})
					}
				}
			}
		}
		if expression.Kind == syntax.ExpressionLambda && expression.LambdaBody != nil {
			walkCommands(expression.LambdaBody.Commands, result.lambdaScopes[expression])
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

// collectBuiltinCallArityDiagnostic reports arity errors only where the
// callable is a plain, statically named builtin. Scoped, member, method, and
// dynamically named calls deliberately remain unknown.
func collectBuiltinCallArityDiagnostic(result *FileAnalysis, file *syntax.File, call *syntax.Expression) {
	if result == nil || file == nil || call == nil || call.Value != "" || len(call.Children) == 0 || expressionContainsMissing(call) || syntaxDiagnosticTouchesCall(file.Diagnostics, call.Span) {
		return
	}
	callee := call.Children[0]
	if callee == nil || callee.Kind != syntax.ExpressionIdentifier || strings.Contains(callee.Value, ":") || !validNameSpan(file, callee.Span) {
		return
	}
	builtin, ok := vimdata.LookupFunction(callee.Value)
	if !ok {
		return
	}
	argumentCount := len(call.Children) - 1
	if argumentCount < builtin.MinArgs {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E119", Message: "Not enough arguments for function: " + builtin.Name, Span: callee.Span,
		})
		return
	}
	if builtin.MaxArgs >= 0 && argumentCount > builtin.MaxArgs {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E118", Message: "Too many arguments for function: " + builtin.Name, Span: callee.Span,
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
	for _, child := range expression.Children {
		if expressionContainsMissing(child) {
			return true
		}
	}
	return false
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
