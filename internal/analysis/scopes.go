package analysis

import (
	"sort"
	"strconv"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
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
	unknownOptions  map[syntax.Span]bool
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
	// Parameter distinguishes a function or lambda argument from an ordinary
	// mutable variable without changing its navigation symbol kind.
	Parameter bool
	Scope     *Scope
	Type      ValueType
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
	result.unknownOptions = make(map[syntax.Span]bool)
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
	collectArgumentRedeclarationDiagnostics(result)
	collectVim9RedeclarationDiagnostics(result)
	collectVim9NameAlreadyDefinedDiagnostics(result, file.Commands)
	collectVim9ScriptItemRedefinitionDiagnostics(result, file.Commands)
	collectVim9DestructuringDiagnostics(result, file.Commands)
	collectMissingReturnValueDiagnostics(result, file.Commands, file.Blocks)

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
	collectFuncrefVariableNameDiagnostics(result)
	collectMissingDictionaryKeyDiagnostics(result, file.Commands, root)
	collectOperatorDiagnostics(result, file.Commands, root)
	collectVoidValueDiagnostics(result, file.Commands)
	collectTypeMismatchDiagnostics(result, file.Commands, root)
	collectBuiltinArgumentTypeDiagnostics(result, file.Commands, root)
	collectAssignmentDiagnostics(result, file.Commands, root)
	sort.SliceStable(result.Diagnostics, func(i, j int) bool {
		return result.Diagnostics[i].Span.Start < result.Diagnostics[j].Span.Start
	})
	return result
}

func collectMissingReturnValueDiagnostics(result *FileAnalysis, commands []syntax.Command, blocks []syntax.Block) {
	if result == nil || result.File == nil {
		return
	}
	seenLambdas := make(map[*syntax.Expression]bool)
	var walkCommands func([]syntax.Command, []syntax.Block, []bool)
	var walkExpression func(*syntax.Expression, []bool)
	walkExpression = func(expression *syntax.Expression, functionNeedsValue []bool) {
		if expression == nil || seenLambdas[expression] {
			return
		}
		seenLambdas[expression] = true
		if expression.Kind == syntax.ExpressionLambda && expression.LambdaBody != nil {
			needsValue := returnTypeNeedsValue(expression.ReturnType)
			body := expression.LambdaBody
			if needsValue && len(body.Diagnostics) == 0 && !syntaxDiagnosticOverlaps(result.File.Diagnostics, expression.Span) &&
				commandSequenceFlow(body.Commands, body.Blocks, 0, len(body.Commands)) == functionFlowFallsThrough {
				span := expression.Span
				if span.End > span.Start {
					span.Start = span.End - 1
				}
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1027", Message: "Missing return statement", Span: span,
				})
			}
			walkCommands(body.Commands, body.Blocks, append(functionNeedsValue, needsValue))
		}
		for _, child := range expression.Children {
			walkExpression(child, functionNeedsValue)
		}
	}
	walkCommands = func(commands []syntax.Command, blocks []syntax.Block, functionNeedsValue []bool) {
		for index := range commands {
			command := &commands[index]
			switch command.Canonical {
			case "def":
				needsValue := command.Function != nil && returnTypeNeedsValue(command.Function.ReturnType)
				functionNeedsValue = append(functionNeedsValue, needsValue)
				if needsValue && validBlock(blocks, command.Block) {
					block := blocks[command.Block]
					if block.Kind == syntax.BlockDef && block.Header == index && block.End > index && block.End < len(commands) &&
						commands[block.End].Canonical == "enddef" && !syntaxDiagnosticOverlaps(result.File.Diagnostics, block.Span) &&
						commandSequenceFlow(commands, blocks, index+1, block.End) == functionFlowFallsThrough {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E1027", Message: "Missing return statement", Span: commands[block.End].Name,
						})
					}
				}
			case "function":
				functionNeedsValue = append(functionNeedsValue, false)
			}
			if command.Canonical == "return" && len(command.Expressions) == 0 && len(functionNeedsValue) > 0 && functionNeedsValue[len(functionNeedsValue)-1] {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1003", Message: "Missing return value", Span: command.Name,
				})
			}
			for _, expression := range command.Expressions {
				walkExpression(expression, functionNeedsValue)
			}
			for _, expression := range command.Targets {
				walkExpression(expression, functionNeedsValue)
			}
			if command.Mapping != nil {
				walkExpression(command.Mapping.RHSExpression, functionNeedsValue)
			}
			if command.Declaration != nil {
				walkExpression(command.Declaration.Initializer, functionNeedsValue)
			}
			if command.For != nil {
				walkExpression(command.For.Iterable, functionNeedsValue)
			}
			if command.Import != nil {
				walkExpression(command.Import.Path, functionNeedsValue)
			}
			for _, value := range command.EnumValues {
				walkExpression(value.Initializer, functionNeedsValue)
				for _, argument := range value.Arguments {
					walkExpression(argument, functionNeedsValue)
				}
			}
			if command.Function != nil {
				for _, parameter := range command.Function.Parameters {
					walkExpression(parameter.Default, functionNeedsValue)
				}
			}
			if command.Embedded != nil {
				walkCommands(command.Embedded.Commands, command.Embedded.Blocks, functionNeedsValue)
			}
			if (command.Canonical == "enddef" || command.Canonical == "endfunction") && len(functionNeedsValue) > 0 {
				functionNeedsValue = functionNeedsValue[:len(functionNeedsValue)-1]
			}
		}
	}
	walkCommands(commands, blocks, nil)
}

func returnTypeNeedsValue(returnType *syntax.Type) bool {
	return returnType != nil && returnType.Kind != syntax.TypeMissing && returnType.Name != "void"
}

func syntaxDiagnosticOverlaps(diagnostics []syntax.Diagnostic, span syntax.Span) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Span.Start <= span.End && diagnostic.Span.End >= span.Start {
			return true
		}
	}
	return false
}

type functionFlow uint8

const (
	functionFlowFallsThrough functionFlow = iota
	functionFlowReturns
	functionFlowThrows
	functionFlowUnknown
)

func (flow functionFlow) terminates() bool {
	return flow == functionFlowReturns || flow == functionFlowThrows
}

func commandSequenceFlow(commands []syntax.Command, blocks []syntax.Block, start, end int) functionFlow {
	if start < 0 || end < start || end > len(commands) {
		return functionFlowUnknown
	}
	unknown := false
	for index := start; index < end; {
		command := &commands[index]
		switch command.Canonical {
		case "return":
			return functionFlowReturns
		case "throw":
			return functionFlowThrows
		}
		if isBlockHeader(blocks, index, command.Block) {
			block := blocks[command.Block]
			if block.End <= index || block.End >= end || block.End >= len(commands) {
				unknown = true
				index++
				continue
			}
			flow := commandBlockFlow(commands, blocks, block)
			if flow.terminates() {
				return flow
			}
			if flow == functionFlowUnknown {
				unknown = true
			}
			index = block.End + 1
			continue
		}
		index++
	}
	if unknown {
		return functionFlowUnknown
	}
	return functionFlowFallsThrough
}

func commandBlockFlow(commands []syntax.Command, blocks []syntax.Block, block syntax.Block) functionFlow {
	switch block.Kind {
	case syntax.BlockIf:
		return ifBlockFlow(commands, blocks, block)
	case syntax.BlockTry:
		return tryBlockFlow(commands, blocks, block)
	case syntax.BlockScope, syntax.BlockAugroup:
		return commandSequenceFlow(commands, blocks, block.Header+1, block.End)
	case syntax.BlockFor, syntax.BlockWhile, syntax.BlockFunction, syntax.BlockDef,
		syntax.BlockClass, syntax.BlockInterface, syntax.BlockEnum, syntax.BlockCommand:
		return functionFlowFallsThrough
	default:
		return functionFlowUnknown
	}
}

func ifBlockFlow(commands []syntax.Command, blocks []syntax.Block, block syntax.Block) functionFlow {
	if len(block.Branches) == 0 || block.Branches[len(block.Branches)-1] <= block.Header ||
		block.Branches[len(block.Branches)-1] >= block.End || commands[block.Branches[len(block.Branches)-1]].Canonical != "else" {
		return functionFlowFallsThrough
	}
	headers := make([]int, 0, len(block.Branches)+1)
	headers = append(headers, block.Header)
	headers = append(headers, block.Branches...)
	for index, header := range headers {
		end := block.End
		if index+1 < len(headers) {
			end = headers[index+1]
		}
		flow := commandSequenceFlow(commands, blocks, header+1, end)
		if flow == functionFlowUnknown {
			return functionFlowUnknown
		}
		if !flow.terminates() {
			return functionFlowFallsThrough
		}
	}
	return functionFlowReturns
}

func tryBlockFlow(commands []syntax.Command, blocks []syntax.Block, block syntax.Block) functionFlow {
	if len(block.Branches) == 0 {
		return functionFlowUnknown
	}
	for index, branch := range block.Branches {
		if branch <= block.Header || branch >= block.End || index > 0 && branch <= block.Branches[index-1] {
			return functionFlowUnknown
		}
		if commands[branch].Canonical != "catch" && commands[branch].Canonical != "finally" {
			return functionFlowUnknown
		}
	}
	lastBranch := block.Branches[len(block.Branches)-1]
	if commands[lastBranch].Canonical == "finally" {
		flow := commandSequenceFlow(commands, blocks, lastBranch+1, block.End)
		if flow == functionFlowUnknown {
			return functionFlowUnknown
		}
		if flow == functionFlowReturns {
			return functionFlowReturns
		}
		return functionFlowFallsThrough
	}
	if commands[lastBranch].Canonical != "catch" || commands[lastBranch].Argument.Start < commands[lastBranch].Argument.End {
		return functionFlowFallsThrough
	}
	headers := make([]int, 0, len(block.Branches)+1)
	headers = append(headers, block.Header)
	headers = append(headers, block.Branches...)
	for index, header := range headers {
		end := block.End
		if index+1 < len(headers) {
			end = headers[index+1]
		}
		flow := commandSequenceFlow(commands, blocks, header+1, end)
		if flow == functionFlowUnknown {
			return functionFlowUnknown
		}
		if index+1 < len(headers) {
			if flow != functionFlowReturns {
				return functionFlowFallsThrough
			}
		} else if flow != functionFlowReturns {
			return functionFlowFallsThrough
		} else {
			return functionFlowReturns
		}
	}
	return functionFlowFallsThrough
}

func collectFuncrefVariableNameDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	for _, declaration := range result.Declarations {
		if declaration == nil || declaration.Kind != SymbolKindVariable && declaration.Kind != SymbolKindConstant ||
			declaration.Type.Name != "func" && declaration.Type.Name != "partial" || funcrefVariableNameAllowed(result.File.Dialect, declaration) {
			continue
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E704", Message: "Funcref variable name must start with a capital: " + declaration.Name, Span: declaration.Span,
		})
	}
}

func funcrefVariableNameAllowed(dialect syntax.Dialect, declaration *Declaration) bool {
	if declaration == nil {
		return false
	}
	// Class and interface members are resolved through an object or class,
	// rather than Vim's ordinary Funcref-variable namespace.
	if declaration.Scope != nil && (declaration.Scope.Kind == syntax.BlockClass || declaration.Scope.Kind == syntax.BlockInterface) {
		return true
	}
	name := declaration.Name
	if strings.Contains(name, "#") {
		return true
	}
	if len(name) >= 2 && name[1] == ':' {
		if name[0] == 'w' || name[0] == 'b' || name[0] == 't' || name[0] == 's' && dialect == syntax.Legacy {
			return true
		}
		name = name[2:]
	}
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
}

func collectMissingDictionaryKeyDiagnostics(result *FileAnalysis, commands []syntax.Command, parent *Scope) {
	if result == nil || result.File == nil {
		return
	}
	type dictionaryShape struct {
		keys       map[string]struct{}
		generation int
	}
	declarations := make(map[syntax.Span]*Declaration)
	for _, declaration := range result.Declarations {
		if declaration != nil {
			declarations[declaration.Span] = declaration
		}
	}
	staticDictionaryKeys := func(expression *syntax.Expression, dialect syntax.Dialect) (map[string]struct{}, bool) {
		if expression == nil || expression.Kind != syntax.ExpressionDictionary || len(expression.Children)%2 != 0 {
			return nil, false
		}
		keys := make(map[string]struct{}, len(expression.Children)/2)
		for index := 0; index < len(expression.Children); index += 2 {
			key, ok := syntax.StaticDictionaryKey(expression.Children[index], dialect)
			if !ok {
				return nil, false
			}
			keys[key] = struct{}{}
		}
		return keys, true
	}
	shapes := make(map[*Declaration]dictionaryShape)
	shapeForReceiver := func(scope *Scope, receiver *syntax.Expression, dialect syntax.Dialect, generation int) (map[string]struct{}, bool) {
		if keys, ok := staticDictionaryKeys(receiver, dialect); ok {
			return keys, true
		}
		if receiver == nil || receiver.Kind != syntax.ExpressionIdentifier {
			return nil, false
		}
		declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil)
		shape, ok := shapes[declaration]
		if !ok {
			return nil, false
		}
		if shape.generation != generation && shape.generation+1 != generation {
			return nil, false
		}
		return shape.keys, true
	}
	appendMissingKey := func(scope *Scope, expression *syntax.Expression, dialect syntax.Dialect, generation int) {
		if expression == nil || len(expression.Children) == 0 {
			return
		}
		for _, diagnostic := range result.File.Diagnostics {
			if diagnostic.Code == "vim/E488" && diagnostic.Span.Start <= expression.Span.End && diagnostic.Span.End >= expression.Span.Start {
				return
			}
		}
		var key string
		var ok bool
		switch expression.Kind {
		case syntax.ExpressionMember:
			if result.File.Text(expression.Operator) != "." {
				return
			}
			key = expression.Value
			if tail := strings.IndexAny(key, "#:"); tail >= 0 {
				key = key[:tail]
			}
			ok = key != ""
		case syntax.ExpressionIndex:
			if len(expression.Children) < 2 {
				return
			}
			key, ok = syntax.StaticDictionaryIndexKey(expression.Children[1])
		default:
			return
		}
		if !ok {
			return
		}
		keys, known := shapeForReceiver(scope, expression.Children[0], dialect, generation)
		if !known {
			return
		}
		if _, exists := keys[key]; exists {
			return
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E716", Message: "Key not present in Dictionary: \"" + key + "\"", Span: expression.Span,
		})
	}

	generation := 0
	var walkCommands func([]syntax.Command, *Scope)
	var walkExpression func(*syntax.Expression, *Scope, syntax.Dialect, bool, int)
	walkExpression = func(expression *syntax.Expression, scope *Scope, dialect syntax.Dialect, read bool, currentGeneration int) {
		if expression == nil || scope == nil {
			return
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
					walkExpression(child, lambdaScope, dialect, true, currentGeneration)
				}
			}
			return
		}
		if expression.Kind == syntax.ExpressionAssignment && len(expression.Children) >= 2 {
			walkExpression(expression.Children[1], scope, dialect, true, currentGeneration)
			plainAssignment := expression.Value == "=" && result.File.Text(expression.Operator) == "="
			walkExpression(expression.Children[0], scope, dialect, !plainAssignment, currentGeneration)
			for _, child := range expression.Children[2:] {
				walkExpression(child, scope, dialect, true, currentGeneration)
			}
			return
		}
		if read && (expression.Kind == syntax.ExpressionMember || expression.Kind == syntax.ExpressionIndex) {
			appendMissingKey(scope, expression, dialect, currentGeneration)
		}
		for _, child := range expression.Children {
			walkExpression(child, scope, dialect, true, currentGeneration)
		}
	}
	walkCommands = func(items []syntax.Command, inherited *Scope) {
		for index := range items {
			generation++
			currentGeneration := generation
			command := &items[index]
			scope := result.commandScopes[command]
			if scope == nil {
				scope = inherited
			}
			if command.Declaration != nil {
				walkExpression(command.Declaration.Initializer, scope, command.Dialect, true, currentGeneration)
				if len(command.Declaration.Bindings) == 1 {
					binding := command.Declaration.Bindings[0]
					declaration := declarations[binding.Name]
					keys, known := staticDictionaryKeys(command.Declaration.Initializer, command.Dialect)
					if !known && command.Dialect == syntax.Vim9 && command.Declaration.Initializer == nil && convertSyntaxType(binding.ParsedType).Name == "dict" {
						keys, known = map[string]struct{}{}, true
					}
					if declaration != nil && known {
						shapes[declaration] = dictionaryShape{keys: keys, generation: generation}
					}
				}
			} else {
				for _, expression := range command.Expressions {
					walkExpression(expression, scope, command.Dialect, true, currentGeneration)
				}
			}
			for _, target := range command.Targets {
				walkExpression(target, scope, command.Dialect, true, currentGeneration)
			}
			if command.Function != nil {
				for _, parameter := range command.Function.Parameters {
					walkExpression(parameter.Default, scope, command.Dialect, true, currentGeneration)
				}
			}
			if command.Embedded != nil {
				walkCommands(command.Embedded.Commands, scope)
			}
		}
	}
	walkCommands(commands, parent)
}

// collectVim9ScriptItemRedefinitionDiagnostics reports E1041 when two
// different kinds of script item use one name, and for duplicate variables,
// aggregates, and top-level loop bindings. Duplicate functions and duplicate
// type aliases retain their more specific diagnostics.
func collectVim9ScriptItemRedefinitionDiagnostics(result *FileAnalysis, commands []syntax.Command) {
	if result == nil || result.File == nil {
		return
	}
	eligible := make(map[syntax.Span]bool)
	var collect func([]syntax.Command)
	collect = func(items []syntax.Command) {
		for index := range items {
			command := &items[index]
			scope := result.commandScopes[command]
			topLevel := scope == result.Root || scope != nil && scope.Kind == syntax.BlockFor && scope.Parent == result.Root
			if command.Dialect == syntax.Vim9 && topLevel {
				if command.Declaration != nil {
					for _, binding := range command.Declaration.Bindings {
						eligible[binding.Name] = true
					}
				}
				if command.For != nil {
					for _, binding := range command.For.Bindings {
						eligible[binding.Name] = true
					}
				}
				if command.Aggregate != nil {
					eligible[command.Aggregate.Name] = true
				}
				if command.TypeAlias != nil {
					eligible[command.TypeAlias.Name] = true
				}
			}
			if command.Canonical == "def" && command.Function != nil {
				eligible[command.Function.Name] = true
			}
			if command.Aggregate != nil {
				eligible[command.Aggregate.Name] = true
			}
			if command.Embedded != nil {
				collect(command.Embedded.Commands)
			}
		}
	}
	collect(commands)
	// Declarations are sorted by source position by the collector's caller;
	// use the source order explicitly since duplicate reporting must point at
	// the later item.
	seen := make(map[string]bool)
	seenKind := make(map[string]SymbolKind)
	declarations := append([]*Declaration(nil), result.Declarations...)
	sort.SliceStable(declarations, func(i, j int) bool { return declarations[i].Span.Start < declarations[j].Span.Start })
	for _, declaration := range declarations {
		if declaration == nil || !eligible[declaration.Span] || declaration.Scope == nil {
			continue
		}
		topLevel := declaration.Scope == result.Root || declaration.Scope.Kind == syntax.BlockFor && declaration.Scope.Parent == result.Root
		if !topLevel {
			continue
		}
		previousKind := seenKind[declaration.Name]
		isFunction := functionSymbolKind(declaration.Kind)
		if seen[declaration.Name] && !(isFunction && functionSymbolKind(previousKind)) &&
			!(declaration.Kind == SymbolKindTypeAlias && previousKind == SymbolKindTypeAlias) {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1041", Message: `Redefining script item: "` + declaration.Name + `"`, Span: declaration.Span})
		}
		seen[declaration.Name] = true
		seenKind[declaration.Name] = declaration.Kind
	}
	rootNames := make(map[string]bool)
	for _, declaration := range result.Root.Declarations {
		if declaration != nil {
			rootNames[declaration.Name] = true
		}
	}
	var genericConflicts func([]syntax.Command)
	genericConflicts = func(items []syntax.Command) {
		for index := range items {
			command := &items[index]
			if command.Function != nil && command.Canonical == "def" {
				for _, parameter := range command.Function.TypeParameters {
					if rootNames[parameter.Name] {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1041", Message: `Redefining script item: "` + parameter.Name + `"`, Span: parameter.Span})
					}
				}
			}
			if command.Embedded != nil {
				genericConflicts(command.Embedded.Commands)
			}
		}
	}
	genericConflicts(commands)
}

func functionSymbolKind(kind SymbolKind) bool {
	return kind == SymbolKindFunction || kind == SymbolKindMethod || kind == SymbolKindConstructor
}

// collectVim9NameAlreadyDefinedDiagnostics covers Vim9 :def and :import names.
// Other declaration kinds retain their specific redeclaration diagnostics.
func collectVim9NameAlreadyDefinedDiagnostics(result *FileAnalysis, commands []syntax.Command) {
	if result == nil || result.File == nil {
		return
	}
	eligible := make(map[syntax.Span]bool)
	var collect func([]syntax.Command)
	collect = func(items []syntax.Command) {
		for index := range items {
			command := &items[index]
			if command.Canonical == "def" && command.Function != nil && !emptySyntaxSpan(command.Function.Name) {
				eligible[command.Function.Name] = true
			}
			if command.Dialect == syntax.Vim9 && command.Import != nil && !emptySyntaxSpan(command.Import.Alias) {
				eligible[command.Import.Alias] = true
			}
			if command.Embedded != nil {
				collect(command.Embedded.Commands)
			}
		}
	}
	collect(commands)
	for _, declaration := range result.Declarations {
		if declaration == nil || declaration.Scope == nil || !eligible[declaration.Span] {
			continue
		}
		if existing := resolve(declaration.Scope, declaration.Name, declaration.Span.Start, false, nil); existing != nil &&
			!(declaration.Scope == result.Root && functionSymbolKind(declaration.Kind) && !functionSymbolKind(existing.Kind)) {
			code := "vim/E1073"
			message := "Name already defined: " + declaration.Name
			if declaration.Kind == SymbolKindImport && existing.Kind != SymbolKindImport && !functionSymbolKind(existing.Kind) && declaration.Scope == result.Root {
				code = "vim/E1054"
				message = "Variable already declared in the script: " + declaration.Name
			}
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: code, Message: message, Span: declaration.Span,
			})
		}
	}
}

func collectVim9RedeclarationDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	first := make(map[*Scope]map[string]int)
	for _, declaration := range result.Declarations {
		if declaration.Scope == nil || declaration.Parameter || declaration.Kind != SymbolKindVariable && declaration.Kind != SymbolKindConstant {
			continue
		}
		names := first[declaration.Scope]
		if names == nil {
			names = make(map[string]int)
			first[declaration.Scope] = names
		}
		position, exists := names[declaration.Name]
		if !exists || declaration.Span.Start < position {
			names[declaration.Name] = declaration.Span.Start
		}
	}
	for _, declaration := range result.Declarations {
		if declaration.Scope == nil || declaration.Parameter || declaration.Kind != SymbolKindVariable && declaration.Kind != SymbolKindConstant {
			continue
		}
		duplicate := first[declaration.Scope][declaration.Name] < declaration.Span.Start &&
			(scopeUsesDefTypeRules(declaration.Scope) || declarationHasCompoundTarget(result.File, declaration.Span))
		if !duplicate && declaration.Scope.Kind == syntax.BlockFor && scopeUsesDefTypeRules(declaration.Scope) {
			for scope := declaration.Scope.Parent; scope != nil; scope = scope.Parent {
				if position, exists := first[scope][declaration.Name]; exists && position < declaration.Span.Start {
					duplicate = true
					break
				}
			}
		}
		if duplicate {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1017", Message: "Variable already declared: " + declaration.Name, Span: declaration.Span,
			})
		}
	}
}

func declarationHasCompoundTarget(file *syntax.File, span syntax.Span) bool {
	position := span.End
	for position < len(file.Source) && (file.Source[position] == ' ' || file.Source[position] == '\t') {
		position++
	}
	return position < len(file.Source) && (file.Source[position] == '.' || file.Source[position] == '[')
}

func collectVim9DestructuringDiagnostics(result *FileAnalysis, commands []syntax.Command) {
	for index := range commands {
		command := &commands[index]
		if command.Dialect == syntax.Vim9 && command.Declaration != nil && command.Declaration.Initializer != nil {
			bindings := command.Declaration.Bindings
			if len(bindings) > 0 && command.Declaration.Target != nil &&
				(command.Declaration.Target.Kind == syntax.ExpressionList || command.Declaration.Target.Kind == syntax.ExpressionTuple) {
				fixed := 0
				rest := false
				for _, binding := range bindings {
					if binding.Rest {
						rest = true
					} else {
						fixed++
					}
				}
				appendVim9CardinalityDiagnostic(result, fixed, rest, command.Declaration.Initializer)
			}
		}
		if command.Dialect == syntax.Vim9 {
			seen := make(map[*syntax.Expression]bool)
			var checkAssignment func(*syntax.Expression)
			checkAssignment = func(expression *syntax.Expression) {
				if expression == nil || seen[expression] {
					return
				}
				seen[expression] = true
				if expression.Kind == syntax.ExpressionAssignment && len(expression.Children) >= 2 {
					target, rhs := expression.Children[0], expression.Children[1]
					if target.Kind == syntax.ExpressionList || target.Kind == syntax.ExpressionTuple {
						rest := strings.Contains(result.File.Text(target.Span), ";")
						expected := len(target.Children)
						if rest {
							expected--
						}
						appendVim9CardinalityDiagnostic(result, expected, rest, rhs)
					}
				}
				if expression.Kind == syntax.ExpressionLambda && expression.LambdaBody != nil {
					collectVim9DestructuringDiagnostics(result, expression.LambdaBody.Commands)
				}
				for _, child := range expression.Children {
					checkAssignment(child)
				}
			}
			if command.Declaration != nil {
				checkAssignment(command.Declaration.Initializer)
			}
			for _, expression := range command.Expressions {
				if command.Declaration != nil && expression != nil && expression.Kind == syntax.ExpressionAssignment && len(expression.Children) >= 1 && expression.Children[0] == command.Declaration.Target {
					continue
				}
				checkAssignment(expression)
			}
			for _, expression := range command.Targets {
				checkAssignment(expression)
			}
		}
		if command.Embedded != nil {
			collectVim9DestructuringDiagnostics(result, command.Embedded.Commands)
		}
	}
}

func appendVim9CardinalityDiagnostic(result *FileAnalysis, expected int, rest bool, rhs *syntax.Expression) {
	if rhs == nil || expressionContainsMissing(rhs) || rhs.Kind != syntax.ExpressionList && rhs.Kind != syntax.ExpressionTuple {
		return
	}
	got := len(rhs.Children)
	if rest && got >= expected || !rest && got == expected {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1093", Message: "Expected " + strconv.Itoa(expected) + " items but got " + strconv.Itoa(got), Span: rhs.Span,
	})
}

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

func numericConversionDiagnostic(typ ValueType, span syntax.Span) (syntax.Diagnostic, bool) {
	switch typ.Name {
	case "special":
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

// collectOperatorDiagnostics keeps compiled Vim9 operator errors distinct
// from the historical conversion errors used by Legacy and script-level Vim9.
// Unknown values remain deliberately opaque.
func collectOperatorDiagnostics(result *FileAnalysis, commands []syntax.Command, parent *Scope) {
	if result == nil || result.File == nil {
		return
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
			if expression.Kind == syntax.ExpressionBinary || expression.Kind == syntax.ExpressionAssignment {
				op := expression.Value
				if expression.Kind == syntax.ExpressionAssignment {
					op = result.File.Text(expression.Operator)
				}
				compoundTypeError := false
				if expression.Kind == syntax.ExpressionAssignment && len(expression.Children) >= 2 && !expressionContainsMissing(expression) {
					targetType := assignmentTargetType(result, expressionScope, expression.Children[0])
					rightType := result.TypeOf(expression.Children[1])
					numericCompound := op == "+=" || op == "-=" || op == "*=" || op == "/=" || op == "%="
					invalidTarget := numericCompound && targetType.Name == "dict"
					invalidScriptConcat := (op == ".=" || op == "..=") && !scopeUsesDefTypeRules(expressionScope) && targetType.Name == "string" && (rightType.Name == "list" || rightType.Name == "dict")
					if invalidTarget || invalidScriptConcat {
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
				if !compoundTypeError && (op == "+" || op == "-" || op == "*" || op == "/" || op == "%" || op == "+=" || op == "-=" || op == "*=" || op == "/=" || op == "%=") && len(expression.Children) >= 2 && !expressionContainsMissing(expression) {
					left, right := result.TypeOf(expression.Children[0]), result.TypeOf(expression.Children[1])
					if expression.Kind == syntax.ExpressionAssignment {
						left = assignmentTargetType(result, expressionScope, expression.Children[0])
					}
					if command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope) {
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
					} else if expression.Kind == syntax.ExpressionBinary {
						leftOperand, rightOperand := expression.Children[0], expression.Children[1]
						containerConcat := op == "+" && left.Name == right.Name && (left.Name == "list" || left.Name == "tuple" || left.Name == "blob")
						diagnostic, ok := syntax.Diagnostic{}, false
						if !containerConcat && command.Dialect == syntax.Vim9 {
							diagnostic, ok = stringAsNumberDiagnostic(result, leftOperand)
						}
						if !containerConcat {
							if !ok {
								diagnostic, ok = numericConversionDiagnostic(left, leftOperand.Span)
							}
						}
						if !ok && !containerConcat {
							leftNumeric := left.Name == "number" || left.Name == "float"
							if (right.Name != "list" && right.Name != "blob") || leftNumeric {
								if command.Dialect == syntax.Vim9 {
									diagnostic, ok = stringAsNumberDiagnostic(result, rightOperand)
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
					if !ok && command.Dialect == syntax.Vim9 && (leftType.Name == "job" || leftType.Name == "channel") {
						diagnostic = syntax.Diagnostic{Code: "vim/E908", Message: "Using an invalid value as a String: " + leftType.Name, Span: left.Span}
						ok = true
					}
					if !ok {
						leftConvertible := leftType.Name == "bool" || leftType.Name == "float" || leftType.Name == "number" || leftType.Name == "special" || leftType.Name == "string"
						invalidRight := rightType.Name == "void" || rightType.Name == "job" || rightType.Name == "channel"
						if command.Dialect == syntax.Vim9 && leftConvertible && invalidRight {
							diagnostic = syntax.Diagnostic{Code: "vim/E908", Message: "Using an invalid value as a String: " + rightType.Name, Span: right.Span}
							ok = true
						} else {
							diagnostic, ok = stringConversionDiagnostic(rightType, right.Span)
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
					if diagnostic, ok := stringAsNumberDiagnostic(result, operand); ok {
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
					if diagnostic, ok := numberAsBoolDiagnostic(condition); ok {
						result.Diagnostics = append(result.Diagnostics, diagnostic)
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
			if (expression.Kind == syntax.ExpressionIndex || expression.Kind == syntax.ExpressionSlice) && len(expression.Children) >= 2 &&
				!(command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope)) {
				receiver := expression.Children[0]
				if resolvedExpressionType(result, expressionScope, receiver).Name == "float" {
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
			if expression.Kind == syntax.ExpressionDictionary && !(command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope)) {
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
		if command.Embedded != nil {
			collectOperatorDiagnostics(result, command.Embedded.Commands, scope)
		}
	}
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

// collectVoidValueDiagnostics reports E1031 only for the Vim9 contexts where
// a statically known void result must produce a value.  Effect-only calls are
// valid, and unknown values remain deliberately conservative.
func collectVoidValueDiagnostics(result *FileAnalysis, commands []syntax.Command) {
	if result == nil || result.File == nil {
		return
	}
	seen := make(map[syntax.Span]bool)
	appendDiagnostic := func(expression *syntax.Expression) {
		if expression == nil || expression.Span.End <= expression.Span.Start || seen[expression.Span] || result.TypeOf(expression).Name != "void" {
			return
		}
		seen[expression.Span] = true
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E1031", Message: "Cannot use void value", Span: expression.Span,
		})
	}
	var walkExpression func(*syntax.Expression)
	walkExpression = func(expression *syntax.Expression) {
		if expression == nil {
			return
		}
		if expression.Kind == syntax.ExpressionAssignment && expression.Value == "=" && len(expression.Children) >= 2 {
			target, value := expression.Children[0], expression.Children[1]
			if target != nil && (target.Kind == syntax.ExpressionList || target.Kind == syntax.ExpressionTuple) {
				appendDiagnostic(value)
			}
		}
		for _, child := range expression.Children {
			walkExpression(child)
		}
	}
	for index := range commands {
		command := &commands[index]
		if command.Dialect == syntax.Vim9 {
			if declaration := command.Declaration; declaration != nil && declaration.Initializer != nil && declaration.ParsedType == nil {
				appendDiagnostic(declaration.Initializer)
			}
			for _, expression := range command.Expressions {
				walkExpression(expression)
			}
			for _, target := range command.Targets {
				walkExpression(target)
			}
		}
		if command.Embedded != nil {
			collectVoidValueDiagnostics(result, command.Embedded.Commands)
		}
	}
}

func collectTypeMismatchDiagnostics(result *FileAnalysis, commands []syntax.Command, parent *Scope) {
	if result == nil || result.File == nil {
		return
	}
	for index := range commands {
		command := &commands[index]
		scope := result.commandScopes[command]
		if scope == nil {
			scope = parent
		}
		if command.Dialect == syntax.Vim9 {
			collectDeclarationTypeMismatchDiagnostic(result, command)
			collectForTypeMismatchDiagnostic(result, command)
			collectConditionTypeMismatchDiagnostic(result, scope, command)
			if command.Declaration != nil {
				collectAssignmentTypeMismatchDiagnostics(result, scope, command.Declaration.Initializer)
			} else {
				for _, expression := range command.Expressions {
					collectAssignmentTypeMismatchDiagnostics(result, scope, expression)
				}
			}
			for _, target := range command.Targets {
				collectAssignmentTypeMismatchDiagnostics(result, scope, target)
			}
		}
		if command.Embedded != nil {
			collectTypeMismatchDiagnostics(result, command.Embedded.Commands, scope)
		}
	}
}

func collectDeclarationTypeMismatchDiagnostic(result *FileAnalysis, command *syntax.Command) {
	declaration := command.Declaration
	if declaration == nil || declaration.Initializer == nil || expressionContainsMissing(declaration.Initializer) {
		return
	}
	for index, binding := range declaration.Bindings {
		expected := convertSyntaxType(binding.ParsedType)
		if isUnknownType(expected) {
			continue
		}
		value := initializerElement(declaration.Initializer, index, len(declaration.Bindings))
		appendTypeMismatchDiagnostic(result, expected, value)
	}
}

func collectForTypeMismatchDiagnostic(result *FileAnalysis, command *syntax.Command) {
	loop := command.For
	if loop == nil || loop.Iterable == nil || expressionContainsMissing(loop.Iterable) {
		return
	}
	actual := indexedType(result.TypeOf(loop.Iterable))
	for _, binding := range loop.Bindings {
		expected := convertSyntaxType(binding.ParsedType)
		if isUnknownType(expected) || assignmentTypesCompatible(expected, actual) {
			continue
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E1012", Message: "Type mismatch; expected " + valueTypeDisplay(expected) + " but got " + valueTypeDisplay(actual), Span: loop.Iterable.Span,
		})
		return
	}
}

func collectAssignmentTypeMismatchDiagnostics(result *FileAnalysis, scope *Scope, expression *syntax.Expression) {
	if expression == nil {
		return
	}
	if expression.Kind == syntax.ExpressionLambda {
		collectLambdaReturnTypeMismatchDiagnostic(result, expression)
		if expression.LambdaBody != nil {
			lambdaScope := result.lambdaScopes[expression]
			if lambdaScope == nil {
				lambdaScope = scope
			}
			collectTypeMismatchDiagnostics(result, expression.LambdaBody.Commands, lambdaScope)
		}
		for index, child := range expression.Children {
			if index >= len(expression.Parameters) {
				collectAssignmentTypeMismatchDiagnostics(result, scope, child)
			}
		}
		return
	}
	if expression.Kind == syntax.ExpressionCast && expression.CastType != nil && len(expression.Children) > 0 {
		appendTypeMismatchDiagnostic(result, convertSyntaxType(expression.CastType), expression.Children[0])
	}
	if expression.Kind == syntax.ExpressionBinary && (expression.Value == "&&" || expression.Value == "||") && scopeUsesDefTypeRules(scope) {
		collectLogicalTypeMismatchDiagnostic(result, expression)
	}
	if expression.Kind == syntax.ExpressionIndex || expression.Kind == syntax.ExpressionSlice {
		collectIndexTypeMismatchDiagnostic(result, scope, expression)
	}
	if expression.Kind == syntax.ExpressionMember && !scopeUsesDefTypeRules(scope) && len(expression.Children) > 0 && result.File.Text(expression.Operator) == "." {
		receiver := resolvedExpressionType(result, scope, expression.Children[0])
		if receiver.Name == "string" {
			span := syntax.Span{Start: expression.Operator.Start, End: expression.Span.End}
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E488", Message: "Trailing characters: " + result.File.Text(span), Span: span,
			})
		}
	}
	if expression.Kind == syntax.ExpressionAssignment && expression.Value == "=" && len(expression.Children) >= 2 && !expressionContainsMissing(expression) {
		target := expression.Children[0]
		if !isReadOnlyVimVariableTarget(target) {
			if expected := assignmentTargetType(result, scope, target); !isUnknownType(expected) {
				if !scopeUsesDefTypeRules(scope) && expected.Name == "string" && target.Kind == syntax.ExpressionIdentifier && strings.HasPrefix(target.Value, "&") && result.TypeOf(expression.Children[1]).Name == "list" {
					diagnostic, _ := stringConversionDiagnostic(result.TypeOf(expression.Children[1]), expression.Children[1].Span)
					result.Diagnostics = append(result.Diagnostics, diagnostic)
					return
				}
				appendTypeMismatchDiagnostic(result, expected, expression.Children[1])
			}
		}
	}
	if expression.Kind == syntax.ExpressionAssignment && expression.Value == "..=" && len(expression.Children) >= 2 && !expressionContainsMissing(expression) {
		target := expression.Children[0]
		// Vim9's concatenating assignment is only valid for a direct string
		// target.  Keep compound member/index assignments opaque: their
		// container type does not prove the assignable member's type.
		if target != nil && target.Kind == syntax.ExpressionIdentifier {
			if targetType := assignmentTargetType(result, scope, target); !isUnknownType(targetType) && targetType.Name != "string" {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1019", Message: "Can only concatenate to string", Span: target.Span,
				})
			}
		}
	}
	for _, child := range expression.Children {
		collectAssignmentTypeMismatchDiagnostics(result, scope, child)
	}
}

func collectConditionTypeMismatchDiagnostic(result *FileAnalysis, scope *Scope, command *syntax.Command) {
	if command == nil || command.Dialect != syntax.Vim9 || command.Canonical != "if" && command.Canonical != "elseif" && command.Canonical != "while" || len(command.Expressions) == 0 {
		return
	}
	condition := command.Expressions[0]
	if diagnostic, ok := numberAsBoolDiagnostic(condition); ok {
		result.Diagnostics = append(result.Diagnostics, diagnostic)
		return
	}
	if !scopeUsesDefTypeRules(scope) {
		return
	}
	actual := result.TypeOf(condition)
	if isUnknownType(actual) || actual.Name == "bool" || actual.Name == "number" {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1012", Message: "Type mismatch; expected bool but got " + valueTypeDisplay(actual), Span: condition.Span,
	})
}

func collectLogicalTypeMismatchDiagnostic(result *FileAnalysis, expression *syntax.Expression) {
	if len(expression.Children) < 2 {
		return
	}
	for _, operand := range expression.Children[:2] {
		actual := result.TypeOf(operand)
		if isUnknownType(actual) || actual.Name == "bool" {
			continue
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E1012", Message: "Type mismatch; expected bool but got " + valueTypeDisplay(actual), Span: operand.Span,
		})
		return
	}
}

func scopeUsesDefTypeRules(scope *Scope) bool {
	for current := scope; current != nil; current = current.Parent {
		if current.Kind == syntax.BlockDef || current.Lambda != nil {
			return true
		}
	}
	return false
}

func collectLambdaReturnTypeMismatchDiagnostic(result *FileAnalysis, expression *syntax.Expression) {
	expected := convertSyntaxType(expression.ReturnType)
	if isUnknownType(expected) {
		return
	}
	if expression.LambdaBody == nil {
		if len(expression.Children) > len(expression.Parameters) {
			appendTypeMismatchDiagnostic(result, expected, expression.Children[len(expression.Parameters)])
		}
		return
	}
	for index := range expression.LambdaBody.Commands {
		command := &expression.LambdaBody.Commands[index]
		if command.Canonical == "return" && len(command.Expressions) > 0 {
			before := len(result.Diagnostics)
			appendTypeMismatchDiagnostic(result, expected, command.Expressions[0])
			if len(result.Diagnostics) > before {
				return
			}
		}
	}
}

func collectIndexTypeMismatchDiagnostic(result *FileAnalysis, scope *Scope, expression *syntax.Expression) {
	if len(expression.Children) < 2 {
		return
	}
	receiver := resolvedExpressionType(result, scope, expression.Children[0])
	if receiver.Name != "blob" && receiver.Name != "list" && receiver.Name != "string" && receiver.Name != "tuple" {
		return
	}
	expected := ValueType{Name: "number"}
	for _, index := range expression.Children[1:] {
		if index == nil || index.Kind == syntax.ExpressionMissing {
			continue
		}
		if !scopeUsesDefTypeRules(scope) {
			if diagnostic, ok := stringAsNumberDiagnostic(result, index); ok {
				result.Diagnostics = append(result.Diagnostics, diagnostic)
				return
			}
			switch resolvedExpressionType(result, scope, index).Name {
			case "func":
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E703", Message: "Using a Funcref as a Number", Span: index.Span,
				})
				return
			case "float":
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E805", Message: "Using a Float as a Number", Span: index.Span,
				})
				return
			}
		}
		before := len(result.Diagnostics)
		appendTypeMismatchDiagnostic(result, expected, index)
		if len(result.Diagnostics) > before {
			return
		}
	}
}

func assignmentTargetType(result *FileAnalysis, scope *Scope, target *syntax.Expression) ValueType {
	if result == nil || target == nil {
		return UnknownValueType
	}
	switch target.Kind {
	case syntax.ExpressionIdentifier:
		if strings.HasPrefix(target.Value, "&") {
			if option, ok := vimdata.LookupOption(target.Value); ok {
				return builtinOptionValueType(option)
			}
			if vimdata.IsTerminalOptionName(target.Value) {
				return ValueType{Name: "string"}
			}
			return UnknownValueType
		}
		if strings.HasPrefix(target.Value, "$") || strings.HasPrefix(target.Value, "@") {
			return ValueType{Name: "string"}
		}
		if variable, ok := vimdata.LookupVariable(target.Value); ok {
			return builtinVariableValueType(variable)
		}
		return resolvedExpressionType(result, scope, target)
	case syntax.ExpressionIndex, syntax.ExpressionMember:
		if len(target.Children) > 0 {
			return indexedType(resolvedExpressionType(result, scope, target.Children[0]))
		}
	case syntax.ExpressionSlice:
		if len(target.Children) > 0 {
			return resolvedExpressionType(result, scope, target.Children[0])
		}
	}
	return UnknownValueType
}

func resolvedExpressionType(result *FileAnalysis, scope *Scope, expression *syntax.Expression) ValueType {
	if result == nil || expression == nil {
		return UnknownValueType
	}
	if typ := result.TypeOf(expression); !isUnknownType(typ) {
		return typ
	}
	if expression.Kind == syntax.ExpressionIdentifier {
		if declaration := resolve(scope, expression.Value, expression.Span.Start, false, nil); declaration != nil {
			return declaration.Type
		}
	}
	if (expression.Kind == syntax.ExpressionIndex || expression.Kind == syntax.ExpressionMember) && len(expression.Children) > 0 {
		return indexedType(resolvedExpressionType(result, scope, expression.Children[0]))
	}
	return UnknownValueType
}

func appendTypeMismatchDiagnostic(result *FileAnalysis, expected ValueType, expression *syntax.Expression) {
	span, actual, mismatch := assignmentExpressionMismatch(result, expected, expression)
	if !mismatch {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1012", Message: "Type mismatch; expected " + valueTypeDisplay(expected) + " but got " + valueTypeDisplay(actual), Span: span,
	})
}

func assignmentExpressionMismatch(result *FileAnalysis, expected ValueType, expression *syntax.Expression) (syntax.Span, ValueType, bool) {
	if result == nil || expression == nil || expressionContainsMissing(expression) || isUnknownType(expected) {
		return syntax.Span{}, UnknownValueType, false
	}
	if len(expected.Arguments) > 0 {
		switch expected.Name {
		case "list":
			if expression.Kind == syntax.ExpressionList {
				for _, child := range expression.Children {
					if span, actual, mismatch := assignmentExpressionMismatch(result, expected.Arguments[0], child); mismatch {
						return span, actual, true
					}
				}
				return syntax.Span{}, UnknownValueType, false
			}
		case "dict":
			if expression.Kind == syntax.ExpressionDictionary {
				for index := 1; index < len(expression.Children); index += 2 {
					if span, actual, mismatch := assignmentExpressionMismatch(result, expected.Arguments[0], expression.Children[index]); mismatch {
						return span, actual, true
					}
				}
				return syntax.Span{}, UnknownValueType, false
			}
		case "tuple":
			if expression.Kind == syntax.ExpressionTuple {
				fixed := len(expected.Arguments)
				if expected.Variadic {
					fixed--
				}
				if len(expression.Children) < fixed || !expected.Variadic && len(expression.Children) != fixed {
					return expression.Span, result.TypeOf(expression), true
				}
				for index, child := range expression.Children {
					var member ValueType
					if expected.Variadic && index >= fixed {
						member = indexedType(expected.Arguments[len(expected.Arguments)-1])
					} else {
						member = expected.Arguments[index]
					}
					if span, actual, mismatch := assignmentExpressionMismatch(result, member, child); mismatch {
						return span, actual, true
					}
				}
				return syntax.Span{}, UnknownValueType, false
			}
		}
	}
	actual := result.TypeOf(expression)
	if assignmentTypesCompatible(expected, actual) {
		return syntax.Span{}, actual, false
	}
	return expression.Span, actual, true
}

func assignmentTypesCompatible(expected, actual ValueType) bool {
	if isUnknownType(expected) || isUnknownType(actual) {
		return true
	}
	if !knownAssignmentType(expected.Name) || !knownAssignmentType(actual.Name) {
		return true
	}
	if expected.Name == "float" && actual.Name == "number" {
		return true
	}
	if expected.Name != actual.Name {
		return false
	}
	if expected.Name == "func" && expected.ArgumentCountKnown && actual.ArgumentCountKnown && !expected.Variadic && !actual.Variadic && len(expected.Arguments) != len(actual.Arguments) {
		return false
	}
	if len(expected.Arguments) > 0 && len(actual.Arguments) > 0 {
		if len(expected.Arguments) != len(actual.Arguments) {
			return false
		}
		for index := range expected.Arguments {
			if !assignmentTypesCompatible(expected.Arguments[index], actual.Arguments[index]) {
				return false
			}
		}
	}
	return expected.Return == nil || actual.Return == nil || assignmentTypesCompatible(*expected.Return, *actual.Return)
}

func knownAssignmentType(name string) bool {
	switch name {
	case "blob", "bool", "channel", "class", "dict", "enum", "float", "func", "job", "list", "number", "object", "partial", "special", "string", "tuple", "typealias", "void":
		return true
	default:
		return false
	}
}

// collectAssignmentDiagnostics reports statically provable assignment-target
// errors. Dynamic targets deliberately remain opaque here.
func collectAssignmentDiagnostics(result *FileAnalysis, commands []syntax.Command, parent *Scope) {
	if result == nil || result.File == nil {
		return
	}
	for index := range commands {
		command := &commands[index]
		scope := result.commandScopes[command]
		if scope == nil {
			scope = parent
		}
		if command.Declaration == nil || command.Dialect == syntax.Legacy && command.Canonical == "let" {
			for _, expression := range command.Expressions {
				collectAssignmentExpressionDiagnostics(result, scope, expression, command.Dialect)
			}
		}
		if command.Embedded != nil {
			collectAssignmentDiagnostics(result, command.Embedded.Commands, scope)
		}
	}
}

func collectAssignmentExpressionDiagnostics(result *FileAnalysis, scope *Scope, expression *syntax.Expression, dialect syntax.Dialect) {
	if expression == nil || scope == nil {
		return
	}
	if expression.Kind == syntax.ExpressionLambda {
		lambdaScope := result.lambdaScopes[expression]
		if lambdaScope == nil {
			lambdaScope = scope
		}
		if expression.LambdaBody != nil {
			collectAssignmentDiagnostics(result, expression.LambdaBody.Commands, lambdaScope)
		}
		for index, child := range expression.Children {
			if index >= len(expression.Parameters) {
				collectAssignmentExpressionDiagnostics(result, lambdaScope, child, dialect)
			}
		}
		return
	}
	if expression.Kind == syntax.ExpressionAssignment && len(expression.Children) >= 2 && expression.Children[1] != nil && expression.Children[1].Kind != syntax.ExpressionMissing {
		target := expression.Children[0]
		if target != nil && target.Kind == syntax.ExpressionIdentifier && validNameSpan(result.File, target.Span) {
			if isReadOnlyVimVariableTarget(target) || dialect == syntax.Legacy && isReadOnlyLegacyArgumentTarget(scope, target) {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E46", Message: "Cannot change read-only variable \"" + target.Value + "\"", Span: target.Span,
				})
			} else if dialect == syntax.Vim9 && expression.Value == "=" && result.File.Text(expression.Operator) == "=" && !strings.Contains(target.Value, ":") {
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
		if target != nil && (target.Kind == syntax.ExpressionIndex || target.Kind == syntax.ExpressionSlice) && len(target.Children) > 0 &&
			!(dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope)) && resolvedExpressionType(result, scope, target.Children[0]).Name == "string" {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E689", Message: "Index not allowed after a string: " + result.File.Text(expression.Span), Span: target.Span,
			})
		}
	}
	for _, child := range expression.Children {
		collectAssignmentExpressionDiagnostics(result, scope, child, dialect)
	}
}

func isReadOnlyVimVariableTarget(target *syntax.Expression) bool {
	if target == nil || target.Kind != syntax.ExpressionIdentifier {
		return false
	}
	variable, ok := vimdata.LookupVariable(target.Value)
	return ok && variable.Flags&vimdata.VariableReadOnly != 0
}

func isReadOnlyLegacyArgumentTarget(scope *Scope, target *syntax.Expression) bool {
	if target == nil || target.Kind != syntax.ExpressionIdentifier || !strings.HasPrefix(target.Value, "a:") {
		return false
	}
	var functionScope *Scope
	for current := scope; current != nil; current = current.Parent {
		if current.Kind == syntax.BlockDef {
			return false
		}
		if current.Kind == syntax.BlockFunction {
			functionScope = current
			break
		}
	}
	if functionScope == nil {
		return false
	}
	name := strings.TrimPrefix(target.Value, "a:")
	switch name {
	case "0", "000", "firstline", "lastline":
		return true
	}
	for _, declaration := range functionScope.Declarations {
		if declaration.Parameter && declaration.Name == name {
			return true
		}
	}
	return false
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
			addParameterDeclaration(result, functionScope, file, parameterDeclarationSpan(file, parameter))
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

func addParameterDeclaration(result *FileAnalysis, scope *Scope, file *syntax.File, span syntax.Span) *Declaration {
	declaration := addDeclaration(result, scope, file, span, SymbolKindVariable, true)
	if declaration != nil {
		declaration.Parameter = true
	}
	return declaration
}

func collectArgumentRedeclarationDiagnostics(result *FileAnalysis) {
	if result == nil {
		return
	}
	for _, declaration := range result.Declarations {
		if declaration.Parameter || declaration.Kind != SymbolKindVariable && declaration.Kind != SymbolKindConstant {
			continue
		}
		for scope := declaration.Scope; scope != nil; scope = scope.Parent {
			if scope.Kind != syntax.BlockDef {
				continue
			}
			for _, candidate := range scope.Declarations {
				if candidate.Parameter && candidate.Name == declaration.Name && candidate.Span.Start < declaration.Span.Start {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E1006", Message: declaration.Name + " is used as an argument", Span: declaration.Span,
					})
					break
				}
			}
			break
		}
	}
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
	if command.Set != nil {
		for _, option := range command.Set.Options {
			appendUnknownSetOptionDiagnostic(result, file.Text(option.Name), option.Name)
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
	if expression == nil || scope == nil || file == nil {
		return
	}
	switch expression.Kind {
	case syntax.ExpressionAssignment:
		if len(expression.Children) == 0 {
			return
		}
		walkAssignmentTarget(result, file, expression.Children[0], scope, skipped, dialect)
		for _, child := range expression.Children[1:] {
			walkExpression(result, file, child, scope, skipped, false, dialect)
		}
	case syntax.ExpressionIdentifier, syntax.ExpressionCurlyName:
		if expression.Kind == syntax.ExpressionIdentifier && !isLiteralIdentifier(expression.Value) && !skipped[expression.Span] && validNameSpan(file, expression.Span) {
			if strings.HasPrefix(expression.Value, "&") {
				appendUnknownOptionDiagnostic(result, expression.Value, expression.Span)
			}
			declaration := resolve(scope, expression.Value, expression.Span.Start, preferFunction, skipped)
			result.References = append(result.References, &Reference{
				Name: expression.Value, Span: expression.Span,
				Declaration: declaration,
			})
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
		if expression.Kind == syntax.ExpressionIdentifier && !isLiteralIdentifier(expression.Value) && !skipped[expression.Span] && validNameSpan(file, expression.Span) {
			if strings.HasPrefix(expression.Value, "&") {
				appendUnknownOptionDiagnostic(result, expression.Value, expression.Span)
			}
			declaration := resolve(scope, expression.Value, expression.Span.Start, false, skipped)
			result.References = append(result.References, &Reference{Name: expression.Value, Span: expression.Span, Declaration: declaration})
			if dialect == syntax.Vim9 && (vim9UnsupportedNamespace(expression.Value) || declaration == nil && isUnknownVimVariable(expression.Value)) {
				appendVim9UnresolvedReadDiagnostic(result, scope, expression.Value, expression.Span)
			} else if dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope) && assignmentTargetNeedsDeclaration(expression.Value) && declaration == nil {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1089", Message: "Unknown variable: " + expression.Value, Span: expression.Span,
				})
			}
		}
	case syntax.ExpressionMember:
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

func appendVim9UnresolvedReadDiagnostic(result *FileAnalysis, scope *Scope, name string, span syntax.Span) {
	code := "vim/E121"
	message := "Undefined variable: " + name
	if scopeUsesDefTypeRules(scope) {
		code = "vim/E1001"
		message = "Variable not found: " + name
		if vim9UnsupportedNamespace(name) {
			code = "vim/E1075"
			message = "Namespace not supported: " + name
		}
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: code, Message: message, Span: span})
}

func isUnknownVimVariable(name string) bool {
	if !strings.HasPrefix(name, "v:") {
		return false
	}
	_, known := vimdata.LookupVariable(name)
	return !known
}

func vim9UnsupportedNamespace(name string) bool {
	return strings.HasPrefix(name, "a:") || strings.HasPrefix(name, "l:") || strings.HasPrefix(name, "x:")
}

func appendUnknownOptionDiagnostic(result *FileAnalysis, name string, span syntax.Span) {
	display, ok := unknownOptionDisplay(result, name, span)
	if !ok {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E113", Message: "Unknown option: " + display, Span: span,
	})
}

func appendUnknownSetOptionDiagnostic(result *FileAnalysis, name string, span syntax.Span) {
	display, ok := unknownOptionDisplay(result, name, span)
	if !ok {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E518", Message: "Unknown option: " + display, Span: span,
	})
}

func unknownOptionDisplay(result *FileAnalysis, name string, span syntax.Span) (string, bool) {
	if result == nil || name == "" || span.End <= span.Start || result.unknownOptions[span] {
		return "", false
	}
	if _, ok := vimdata.LookupOption(name); ok || vimdata.IsTerminalOptionName(name) {
		return "", false
	}
	display := name
	if strings.HasPrefix(display, "&") {
		display = display[1:]
		if strings.HasPrefix(display, "g:") || strings.HasPrefix(display, "l:") {
			display = display[2:]
		}
	}
	if display == "" || display == "all" || display == "termcap" {
		return "", false
	}
	result.unknownOptions[span] = true
	return display, true
}

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
		if expression.Kind == syntax.ExpressionCall && !expressionContainsMissing(expression) && !syntaxDiagnosticTouchesCall(result.File.Diagnostics, expression.Span) {
			builtin, arguments, builtinCall := builtinCallArguments(result.File, expression)
			if builtinCall && (dialect == syntax.Vim9 || builtin.Name == "len") {
				actual := make([]ValueType, len(arguments))
				for index, argument := range arguments {
					actual[index] = result.TypeOf(argument)
				}
				extendMismatch := -1
				if !scopeUsesDefTypeRules(scope) && (builtin.Name == "extend" || builtin.Name == "extendnew") {
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
					}
					if scopeContainsDef(scope) && callbackCheckerUsesE176(checker) && builtinCallbackArgumentCountInvalid(actual[index], expected) {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E176", Message: "Invalid number of arguments", Span: argument.Span})
						continue
					}
					if builtinArgumentMismatch(actual[index], expected) {
						if extendMismatch >= 0 {
							continue
						}
						// At script level sign_undefine() converts each list item
						// to a sign name and consults Vim's mutable sign registry.
						// A def keeps the strict list<string> compile-time check.
						if builtin.Name == "sign_undefine" && index == 0 && actual[index].Name == "list" && !scopeUsesDefTypeRules(scope) {
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
						if !scopeUsesDefTypeRules(scope) {
							if diagnostic, ok := builtinArgumentDiagnostic(checker, index, argument.Span); ok {
								result.Diagnostics = append(result.Diagnostics, diagnostic)
								continue
							}
						}
						if expressionContainsInvalidPlus(result, argument) {
							continue
						}
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1013", Message: "Argument " + strconv.Itoa(index+1) + ": type mismatch, expected " + expected.display + " but got " + valueTypeDisplay(actual[index]), Span: argument.Span})
					}
				}
				collectMapCallbackReturnTypeDiagnostic(result, scope, builtin, arguments, actual)
				collectSearchpairFlagsDiagnostic(result, scope, builtin, arguments)
				collectSubstituteExpressionDiagnostic(result, builtin, arguments)
				collectBuiltinCompiledStringDiagnostics(result, scope, builtin, arguments, expression.Span.Start)
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

// builtinArgumentDiagnostic mirrors the concrete Vim compile-time checker
// errors for the simple argument checkers whose native diagnostic is useful to
// callers. E1013 remains the general static type mismatch for checkers without
// a specialized native diagnostic.
func builtinArgumentDiagnostic(checker string, index int, span syntax.Span) (syntax.Diagnostic, bool) {
	checker = strings.TrimSuffix(checker, "_mod")
	var code, required string
	switch checker {
	case "arg_len1":
		return syntax.Diagnostic{Code: "vim/E701", Message: "Invalid type for len()", Span: span}, true
	case "arg_string":
		code, required = "vim/E1174", "String"
	case "arg_dict_any":
		code, required = "vim/E1206", "Dictionary"
	default:
		return syntax.Diagnostic{}, false
	}
	return syntax.Diagnostic{
		Code: code, Message: required + " required for argument " + strconv.Itoa(index+1), Span: span,
	}, true
}

func collectMapCallbackReturnTypeDiagnostic(result *FileAnalysis, scope *Scope, builtin vimdata.BuiltinFunction, arguments []*syntax.Expression, actual []ValueType) {
	if result == nil || scopeContainsDef(scope) || builtin.Name != "map" || len(arguments) < 2 || len(actual) < 2 {
		return
	}
	container, callback := actual[0], actual[1]
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
	for _, child := range expression.Children {
		if expressionTreeContainsLambda(child) {
			return true
		}
	}
	return false
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
	if checkTypes && call.Value == "" && callee.Kind == syntax.ExpressionIdentifier {
		// compile_call() has a 200-byte direct-name buffer only while compiling
		// a def. At Vim9 script level the same unresolved spelling is E117.
		if scopeContainsDef(scope) && len(callee.Value) >= 200 {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1011", Message: "Name too long: " + callee.Value, Span: callee.Span})
			return
		}
		if unresolvedDirectFunction(scope, callee) {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E117", Message: "Unknown function: " + callee.Value, Span: callee.Span})
			return
		}
	}
	callable := result.TypeOf(callee)
	arguments := call.Children[1:]
	if callee.Kind == syntax.ExpressionMember && len(callee.Children) == 1 && result.File.Text(callee.Operator) == "->" {
		declaration := resolve(scope, callee.Value, call.Span.Start, true, nil)
		if declaration == nil || !checkTypes && declaration.Span.Start >= call.Span.Start {
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
	if scope == nil || callee == nil || callee.Kind != syntax.ExpressionIdentifier || callee.Value == "" || strings.ContainsAny(callee.Value, ":#&$@") {
		return false
	}
	return resolve(scope, callee.Value, callee.Span.Start, true, nil) == nil
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
func collectBuiltinCallArityDiagnostic(result *FileAnalysis, file *syntax.File, call *syntax.Expression) {
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
	_, span := functionDiagnosticTarget(file, callee)
	if !validNameSpan(file, span) {
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
