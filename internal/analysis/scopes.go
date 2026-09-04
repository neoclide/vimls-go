package analysis

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

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
	// configFile marks a user configuration file (vimrc or an explicit
	// configFiles document). The role is decided outside analysis from the
	// document path; it only adjusts vimls-owned configuration diagnostics and
	// never changes the syntax tree or lexical/semantic structures.
	configFile bool
	// Diagnostics contains protocol-independent semantic diagnostics with byte
	// spans in File.Source.
	Diagnostics     []syntax.Diagnostic
	expressionTypes map[*syntax.Expression]ValueType
	commandScopes   map[*syntax.Command]*Scope
	lambdaScopes    map[*syntax.Expression]*Scope
	lambdaBodies    map[*syntax.Expression]bool
	unknownOptions  map[syntax.Span]bool
	// suppressedSyntaxDiagnostics contains provisional parser diagnostics that
	// Vim replaces after resolving an import namespace.
	suppressedSyntaxDiagnostics map[syntax.Diagnostic]bool
	enumValueExempt             map[syntax.Span]bool
	typeAliasExempt             map[syntax.Span]bool
	classValueExempt            map[syntax.Span]bool
	superMemberExempt           map[syntax.Span]bool
	classAliases                map[string]string
	classes                     map[string]*syntax.Command
}

type NameDeclarationKind uint8

const (
	NameDeclarationFunction NameDeclarationKind = iota + 1
	NameDeclarationVariable
)

type NameDeclarationScope uint8

const (
	NameDeclarationScript NameDeclarationScope = iota + 1
	NameDeclarationGlobal
)

// NameDeclarationEvent is a statically visible change to Vim's script-local
// or global function/variable tables. Delete events retain their source span
// so callers can replay one file in source order without executing it.
type NameDeclarationEvent struct {
	Name   string
	Span   syntax.Span
	Kind   NameDeclarationKind
	Scope  NameDeclarationScope
	Delete bool
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
	Name               string
	Kind               SymbolKind
	Span               syntax.Span
	Mutable            bool
	Deprecated         bool
	TypeParameterCount int
	constBinding       bool
	unusedCandidate    bool
	// Parameter distinguishes a function or lambda argument from an ordinary
	// mutable variable without changing its navigation symbol kind.
	Parameter bool
	Scope     *Scope
	Type      ValueType
}

// Reference is an identifier occurrence.  Declaration is nil when the name
// is dynamic, explicitly scoped to a different namespace, or not visible yet.
type Reference struct {
	Name             string
	Span             syntax.Span
	Declaration      *Declaration
	functionCallee   bool
	assignmentTarget bool
	scope            *Scope
	dialect          syntax.Dialect
}

// Analyze collects lexical scopes, declarations, and same-file references.
// It deliberately does not report undefined names: an unresolved reference
// is a valid result for dynamic legacy Vim script and for incomplete input.
func Analyze(file *syntax.File) *FileAnalysis {
	return analyzeWithRole(file, false)
}

// AnalyzeConfigFile analyzes one document in user-configuration-file mode.
// Semantic structures are identical to Analyze; only the vimls-owned
// configuration diagnostics differ (see style_diagnostics.go). Callers that
// know from the document path that IsConfigFile is true use this entry point.
func AnalyzeConfigFile(file *syntax.File) *FileAnalysis {
	return analyzeWithRole(file, true)
}

func analyzeWithRole(file *syntax.File, configFile bool) *FileAnalysis {
	result := &FileAnalysis{File: file, configFile: configFile, suppressedSyntaxDiagnostics: make(map[syntax.Diagnostic]bool)}
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
	result.enumValueExempt = make(map[syntax.Span]bool)
	result.typeAliasExempt = make(map[syntax.Span]bool)
	result.classValueExempt = make(map[syntax.Span]bool)
	result.superMemberExempt = make(map[syntax.Span]bool)
	result.classAliases = localClassAliases(file)
	result.classes = localAggregates(file, syntax.BlockClass)
	collectCommandScopes(result, root, file.Commands, file.Blocks, nil)
	collectLambdaScopesCommands(result, root, file.Commands)

	// First collect every declaration.  This is separate from reference
	// walking so a function can be referenced before its definition, as Vim
	// permits, without making variables forward-visible.
	collectEmbeddedDeclarations(result, root, file.Commands)
	collectLambdaDeclarations(result, file.Commands)
	collectLegacyFunctionOverwriteRiskDiagnostics(result, file.Commands)
	collectUserCommandOverwriteRiskDiagnostics(result, file.Commands)
	collectNameDeclarationConflictDiagnostics(result)
	collectVim9ScriptFunctionDeletionDiagnostics(result, file.Commands, root)
	collectVim9LegacyScriptVariableDiagnostics(result)

	// A malformed or partially parsed enum value may remain an opaque command.
	// The enum block is still authoritative for its one-name-per-line members.
	collectOpaqueEnumDeclarations(result, file.Commands, file.Blocks)
	collectDuplicateEnumValueDiagnostics(result)
	collectArgumentRedeclarationDiagnostics(result)
	collectArgumentShadowDiagnostics(result)
	collectLegacyConstExistingVariableDiagnostics(result, file.Commands)
	collectVim9RedeclarationDiagnostics(result)
	collectVim9NameAlreadyDefinedDiagnostics(result, file.Commands)
	collectImportedItemRedefinitionDiagnostics(result, file.Commands)
	collectVim9ScriptItemRedefinitionDiagnostics(result, file.Commands)
	collectAggregateLocalRedeclarationDiagnostics(result)
	collectDuplicateTypeAliasDiagnostics(result)
	collectAbstractConstructorDiagnostics(result)
	collectDuplicateMethodDiagnostics(result)
	collectUnimplementedAbstractMethodDiagnostics(result)
	collectMethodAccessLevelDiagnostics(result)
	collectGenericMethodOverrideDiagnostics(result)
	collectMethodTypeMismatchDiagnostics(result)
	collectDuplicateClassVariableDiagnostics(result)
	collectPublicUnderscoreVariableDiagnostics(result)
	collectPublicProtectedMemberNameDiagnostics(result)
	collectConstructorDefaultValueDiagnostics(result)
	collectUninitializedObjectVariableDiagnostics(result)
	collectTypeDiagnostics(result)
	collectInterfaceVariableAccessDiagnostics(result)
	collectReturnOutsideFunctionDiagnostics(result)
	collectMissingReturnValueDiagnostics(result, file.Commands, file.Blocks)
	collectUnreachableCodeDiagnostics(result)
	collectLoopNestingDiagnostics(result)

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
	collectDeprecatedReferenceDiagnostics(result)
	collectImportNamespaceDiagnostics(result)
	inferTypes(result)
	collectNullReceiverDiagnostics(result)
	collectExtendedAggregateDiagnostics(result)
	collectImplementedInterfaceNameDiagnostics(result)
	collectImplementedInterfaceMembersDiagnostics(result)
	collectVariableTypeMismatchDiagnostics(result)
	collectVim9DestructuringDiagnostics(result, file.Commands)
	collectLegacyListCardinalityDiagnostics(result, file.Commands)
	collectFuncrefVariableNameDiagnostics(result)
	collectMissingDictionaryKeyDiagnostics(result, file.Commands, root)
	collectDeferDiagnostics(result, file.Commands, root)
	collectOperatorDiagnostics(result, file.Commands, root)
	collectAggregateAccessDiagnostics(result)
	collectVoidValueDiagnostics(result, file.Commands)
	collectTypeMismatchDiagnostics(result, file.Commands, root)
	collectBuiltinArgumentTypeDiagnostics(result, file.Commands, root)
	collectAssignmentDiagnostics(result, file.Commands, root)
	collectNameOnlyExpressionDiagnostics(result, file.Commands, root)
	collectUnusedVariableDiagnostics(result)
	collectStyleDiagnostics(result)
	if result.configFile {
		collectConfigLeaderOrderDiagnostics(result)
		collectConfigDuplicateMappingDiagnostics(result)
		collectConfigLoadedGuardDiagnostics(result)
	}
	sort.SliceStable(result.Diagnostics, func(i, j int) bool {
		return result.Diagnostics[i].Span.Start < result.Diagnostics[j].Span.Start
	})
	return result
}

func collectDeprecatedReferenceDiagnostics(result *FileAnalysis) {
	for _, reference := range result.References {
		if reference == nil || reference.Declaration == nil || !reference.Declaration.Deprecated {
			continue
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vimls/deprecated", Message: reference.Declaration.Name + " is deprecated", Span: reference.Span,
		})
	}
}

func collectUnusedVariableDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil || len(result.File.Diagnostics) != 0 {
		return
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code != "vimls/deprecated" {
			return
		}
	}
	used := make(map[*Declaration]bool)
	for _, reference := range result.References {
		if reference != nil && reference.Declaration != nil {
			used[reference.Declaration] = true
		}
	}
	for _, declaration := range result.Declarations {
		if declaration == nil || !declaration.unusedCandidate || declaration.Name == "_" || used[declaration] {
			continue
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vimls/unused-variable", Message: declaration.Name + " is declared but never used", Span: declaration.Span,
		})
	}
}

func staticNameDeclaration(file *syntax.File, span syntax.Span, dialect syntax.Dialect, kind NameDeclarationKind, insideFunction bool) (NameDeclarationEvent, bool) {
	if file == nil || span.Start < 0 || span.Start >= span.End || span.End > len(file.Source) || syntaxDiagnosticOverlaps(file.Diagnostics, span) {
		return NameDeclarationEvent{}, false
	}
	raw := file.Text(span)
	scope := NameDeclarationScope(0)
	name := raw
	switch {
	case strings.HasPrefix(raw, "g:"):
		scope, name = NameDeclarationGlobal, raw[2:]
	case strings.HasPrefix(raw, "s:"):
		scope, name = NameDeclarationScript, raw[2:]
	case len(raw) > len("<SID>") && strings.EqualFold(raw[:len("<SID>")], "<SID>"):
		scope, name = NameDeclarationScript, raw[len("<SID>"):]
	case kind == NameDeclarationFunction && dialect == syntax.Legacy:
		scope = NameDeclarationGlobal
	case kind == NameDeclarationVariable && dialect == syntax.Legacy && !insideFunction:
		scope = NameDeclarationGlobal
	default:
		return NameDeclarationEvent{}, false
	}
	if !validScopeVariableName(name) {
		return NameDeclarationEvent{}, false
	}
	return NameDeclarationEvent{Name: strings.Clone(name), Span: span, Kind: kind, Scope: scope}, true
}

// CollectNameDeclarationEvents returns direct, statically named script-local
// and global function/variable declarations and deletions in source order.
// Deferred command bodies and dynamic names remain opaque.
func CollectNameDeclarationEvents(file *syntax.File) []NameDeclarationEvent {
	if file == nil {
		return nil
	}
	events := make([]NameDeclarationEvent, 0)
	for index := range file.Commands {
		command := &file.Commands[index]
		insideFunction := syntax.CommandInsideFunction(command, file.Blocks)
		if command.Function != nil {
			if event, ok := staticNameDeclaration(file, command.Function.Name, command.Dialect, NameDeclarationFunction, false); ok {
				events = append(events, event)
			}
		}
		if command.Declaration != nil {
			for _, binding := range command.Declaration.Bindings {
				if event, ok := staticNameDeclaration(file, binding.Name, command.Dialect, NameDeclarationVariable, insideFunction); ok {
					events = append(events, event)
				}
			}
		}
		if command.Canonical == "delfunction" {
			for _, target := range command.Targets {
				if target != nil && target.Kind == syntax.ExpressionIdentifier {
					if event, ok := staticNameDeclaration(file, target.Span, command.Dialect, NameDeclarationFunction, false); ok {
						event.Delete = true
						events = append(events, event)
					}
				}
			}
		}
		if command.Canonical == "unlet" {
			for _, target := range command.Targets {
				if target != nil && target.Kind == syntax.ExpressionIdentifier {
					if event, ok := staticNameDeclaration(file, target.Span, command.Dialect, NameDeclarationVariable, insideFunction); ok {
						event.Delete = true
						events = append(events, event)
					}
				}
			}
		}
	}
	sort.SliceStable(events, func(left, right int) bool { return events[left].Span.Start < events[right].Span.Start })
	return events
}

func nameDeclarationConflictDiagnostic(event NameDeclarationEvent) syntax.Diagnostic {
	if event.Kind == NameDeclarationVariable {
		return syntax.Diagnostic{
			Code: "vim/E705", Message: "Variable " + event.Name + " conflicts with a function declared in the same scope; rename one to avoid runtime conflicts", Span: event.Span,
		}
	}
	return syntax.Diagnostic{
		Code: "vim/E707", Message: "Function " + event.Name + " conflicts with a variable declared in the same scope; rename one to avoid runtime conflicts", Span: event.Span,
	}
}

func collectNameDeclarationConflictDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	type tableKey struct {
		scope NameDeclarationScope
		name  string
	}
	tables := make(map[tableKey]map[NameDeclarationKind]bool)
	for _, event := range CollectNameDeclarationEvents(result.File) {
		key := tableKey{scope: event.Scope, name: event.Name}
		if tables[key] == nil {
			tables[key] = make(map[NameDeclarationKind]bool)
		}
		if event.Delete {
			delete(tables[key], event.Kind)
			continue
		}
		opposite := NameDeclarationVariable
		if event.Kind == NameDeclarationVariable {
			opposite = NameDeclarationFunction
		}
		if tables[key][opposite] {
			result.Diagnostics = append(result.Diagnostics, nameDeclarationConflictDiagnostic(event))
			continue
		}
		tables[key][event.Kind] = true
	}
}

func collectVim9ScriptFunctionDeletionDiagnostics(result *FileAnalysis, commands []syntax.Command, parent *Scope) {
	if result == nil || result.File == nil || result.File.Dialect != syntax.Vim9 {
		return
	}
	var walk func([]syntax.Command, *Scope)
	walk = func(commands []syntax.Command, parent *Scope) {
		for index := range commands {
			command := &commands[index]
			scope := result.commandScopes[command]
			if scope == nil {
				scope = parent
			}
			if command.Canonical == "delfunction" && len(command.Targets) == 1 {
				target := command.Targets[0]
				if target != nil && target.Kind == syntax.ExpressionIdentifier {
					declaration := resolve(scope, target.Value, target.Span.Start, true, nil)
					if vim9ScriptFunctionDeclaration(result, declaration, target.Span.Start) {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E1084", Message: "Cannot delete Vim9 script function " + target.Value, Span: target.Span,
						})
					}
				}
			}
			if command.Embedded != nil {
				walk(command.Embedded.Commands, scope)
			}
		}
	}
	walk(commands, parent)
}

func vim9ScriptFunctionDeclaration(result *FileAnalysis, declaration *Declaration, before int) bool {
	if declaration == nil || declaration.Kind != SymbolKindFunction || declaration.Scope != result.Root || declaration.Span.Start >= before {
		return false
	}
	for index := range result.File.Commands {
		command := &result.File.Commands[index]
		if command.Function == nil || command.Function.Name != declaration.Span || command.Dialect != syntax.Vim9 {
			continue
		}
		name := result.File.Text(command.Function.Name)
		return validScopeVariableName(name) && !strings.Contains(name, "#")
	}
	return false
}

func collectLegacyFunctionOverwriteRiskDiagnostics(result *FileAnalysis, commands []syntax.Command) {
	if result == nil || result.File == nil {
		return
	}
	var severity *syntax.DiagnosticSeverity
	if result.configFile {
		s := syntax.DiagnosticHint
		severity = &s
	}
	for index := range commands {
		command := &commands[index]
		if command.Canonical == "function" && command.Function != nil && emptySyntaxSpan(command.Bang) &&
			!emptySyntaxSpan(command.Function.Name) {
			name := result.File.Text(command.Function.Name)
			if command.Dialect == syntax.Legacy || strings.HasPrefix(name, "g:") {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E122", Message: "Function " + name + " may already exist when this script is sourced again; add ! to replace it", Span: command.Function.Name,
					Severity: severity,
				})
			}
		}
		if command.Embedded != nil {
			collectLegacyFunctionOverwriteRiskDiagnostics(result, command.Embedded.Commands)
		}
	}
}

// unconditionalAt reports whether the command at index in list runs
// unconditionally in its own block scope: it may open a function/def/command
// block (the header is that block's own command) but must not be nested inside
// a conditional, loop, try, or another function definition. When a function or
// user command appears twice under mutually exclusive conditions, neither
// occurrence is statically provable as a duplicate.
func unconditionalAt(list []syntax.Command, blocks []syntax.Block, index int) bool {
	for blockIndex := list[index].Block; blockIndex >= 0 && blockIndex < len(blocks); {
		block := &blocks[blockIndex]
		if (block.Kind == syntax.BlockFunction || block.Kind == syntax.BlockDef || block.Kind == syntax.BlockCommand) && block.Header == index {
			blockIndex = block.Parent
			continue
		}
		return false
	}
	return true
}

// rootScopedCommand reports whether the command at index in list is a header
// whose own block is nested directly under the file root (or is not inside any
// block). Unlike unconditionalAt it accepts every header kind, which is what
// top-level structural scans such as loaded-guard detection need.
func rootScopedCommand(list []syntax.Command, blocks []syntax.Block, index int) bool {
	for blockIndex := list[index].Block; blockIndex >= 0 && blockIndex < len(blocks); {
		block := &blocks[blockIndex]
		if block.Header == index {
			blockIndex = block.Parent
			continue
		}
		return false
	}
	return true
}

func collectUserCommandOverwriteRiskDiagnostics(result *FileAnalysis, commands []syntax.Command) {
	if result == nil || result.File == nil {
		return
	}
	var severity *syntax.DiagnosticSeverity
	if result.configFile {
		s := syntax.DiagnosticHint
		severity = &s
	}
	for index := range commands {
		command := &commands[index]
		if command.Canonical == "command" && emptySyntaxSpan(command.Bang) {
			if name, span, _, definition := syntax.DefinedUserCommand(result.File, command); definition {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E174", Message: "Command " + name + " may already exist when this script is sourced again; add ! to replace it", Span: span,
					Severity: severity,
				})
			}
		}
		if command.Embedded != nil {
			collectUserCommandOverwriteRiskDiagnostics(result, command.Embedded.Commands)
		}
	}
}

// UserCommandAbbreviationDiagnostics warns when a parsed user-command call is
// a proper prefix of a full name from the complete runtimepath command index.
// Exact matches always win, matching Vim's user-command lookup rule.
func UserCommandAbbreviationDiagnostics(file *syntax.File, indexedNames []string) []syntax.Diagnostic {
	if file == nil {
		return nil
	}
	names := make(map[string]bool, len(indexedNames))
	for _, name := range indexedNames {
		if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
			names[name] = true
		}
	}
	var collectDefinitions func([]syntax.Command)
	collectDefinitions = func(commands []syntax.Command) {
		for index := range commands {
			command := &commands[index]
			if name, _, _, ok := syntax.DefinedUserCommand(file, command); ok && len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
				names[name] = true
			}
			if command.Embedded != nil {
				collectDefinitions(command.Embedded.Commands)
			}
		}
	}
	collectDefinitions(file.Commands)
	if len(names) == 0 {
		return nil
	}
	diagnostics := make([]syntax.Diagnostic, 0)
	var diagnose func([]syntax.Command)
	diagnose = func(commands []syntax.Command) {
		for index := range commands {
			command := &commands[index]
			if command.Kind == syntax.CommandUser && !names[command.TypedName] {
				for name := range names {
					if len(command.TypedName) < len(name) && strings.HasPrefix(name, command.TypedName) {
						diagnostics = append(diagnostics, syntax.Diagnostic{
							Code: "vim/E464", Message: "User-defined command " + command.TypedName + " is abbreviated; use the full command name to avoid ambiguity", Span: command.Name,
						})
						break
					}
				}
			}
			if command.Embedded != nil {
				diagnose(command.Embedded.Commands)
			}
		}
	}
	diagnose(file.Commands)
	return diagnostics
}

func collectDeferDiagnostics(result *FileAnalysis, commands []syntax.Command, parent *Scope) {
	if result == nil || result.File == nil {
		return
	}
	initializers := make(map[*Declaration]*syntax.Expression)
	var collectInitializers func([]syntax.Command, *Scope)
	collectInitializers = func(commands []syntax.Command, parent *Scope) {
		for index := range commands {
			command := &commands[index]
			scope := result.commandScopes[command]
			if scope == nil {
				scope = parent
			}
			if command.Declaration != nil && len(command.Declaration.Bindings) == 1 && command.Declaration.Initializer != nil {
				binding := command.Declaration.Bindings[0]
				for _, declaration := range scope.Declarations {
					if declaration.Span == binding.Name {
						initializers[declaration] = command.Declaration.Initializer
						break
					}
				}
			}
			if command.Embedded != nil {
				collectInitializers(command.Embedded.Commands, scope)
			}
		}
	}
	collectInitializers(commands, parent)

	dictionaryBoundPartial := func(expression *syntax.Expression, scope *Scope, useAt int) bool {
		for expression != nil && expression.Kind == syntax.ExpressionParenthesized && len(expression.Children) == 1 {
			expression = expression.Children[0]
		}
		if expression == nil {
			return false
		}
		if expression.Kind == syntax.ExpressionIdentifier {
			declaration := resolve(scope, expression.Value, expression.Span.Start, false, nil)
			initializer := initializers[declaration]
			if declaration == nil || initializer == nil {
				return false
			}
			for _, reference := range result.References {
				if reference.Declaration == declaration && reference.assignmentTarget && reference.Span.Start > declaration.Span.End && reference.Span.Start < useAt {
					return false
				}
			}
			expression = initializer
		}
		if expression.Kind != syntax.ExpressionCall || len(expression.Children) < 3 {
			return false
		}
		callee := expression.Children[0]
		if callee == nil || callee.Kind != syntax.ExpressionIdentifier || callee.Value != "function" && callee.Value != "funcref" {
			return false
		}
		dictionary := expression.Children[2]
		if len(expression.Children) >= 4 {
			dictionary = expression.Children[3]
		}
		return dictionary != nil && dictionary.Kind == syntax.ExpressionDictionary
	}

	var walk func([]syntax.Command, *Scope)
	walk = func(commands []syntax.Command, parent *Scope) {
		for index := range commands {
			command := &commands[index]
			scope := result.commandScopes[command]
			if scope == nil {
				scope = parent
			}
			insideFunction := false
			for current := scope; current != nil; current = current.Parent {
				if current.Kind == syntax.BlockFunction || current.Kind == syntax.BlockDef || current.Lambda != nil {
					insideFunction = true
					break
				}
			}
			if insideFunction && command.Canonical == "defer" && len(command.Expressions) > 0 {
				expression := command.Expressions[0]
				if expression != nil && expression.Kind == syntax.ExpressionCall && len(expression.Children) > 0 {
					callee := expression.Children[0]
					if dictionaryBoundPartial(callee, scope, callee.Span.Start) {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E1300", Message: "Cannot use a partial with dictionary for :defer", Span: callee.Span,
						})
					}
				}
			}
			if command.Embedded != nil {
				walk(command.Embedded.Commands, scope)
			}
		}
	}
	walk(commands, parent)
}

func collectVim9LegacyScriptVariableDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil || result.File.Dialect != syntax.Vim9 || result.Root == nil {
		return
	}
	file := result.File
	scriptItems := make(map[string]bool)
	for _, declaration := range result.Root.Declarations {
		if declaration != nil {
			scriptItems[declaration.Name] = true
		}
	}
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Dialect != syntax.Legacy || command.Declaration == nil || command.Declaration.Target == nil {
			continue
		}
		switch command.Canonical {
		case "let", "const", "final":
		default:
			continue
		}
		target := command.Declaration.Target
		if target.Kind != syntax.ExpressionIdentifier || !validNameSpan(file, target.Span) {
			continue
		}
		name := file.Text(target.Span)
		if len(name) <= len("s:") || !strings.HasPrefix(name, "s:") || scriptItems[name[len("s:"):]] {
			continue
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E1269", Message: "Cannot create a Vim9 script variable in a function: " + name, Span: target.Span,
		})
	}
}

func collectReturnOutsideFunctionDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Canonical != "return" {
			continue
		}
		valid := false
		ownedByContainer := false
		for blockIndex := command.Block; blockIndex >= 0 && blockIndex < len(file.Blocks); blockIndex = file.Blocks[blockIndex].Parent {
			switch file.Blocks[blockIndex].Kind {
			case syntax.BlockFunction, syntax.BlockDef:
				valid = true
			case syntax.BlockClass, syntax.BlockInterface, syntax.BlockEnum, syntax.BlockCommand:
				ownedByContainer = true
			}
		}
		if valid || ownedByContainer {
			continue
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E133", Message: ":return not inside a function", Span: command.Name,
		})
	}
}

func collectExtendedAggregateDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	aliases := localTypeAliases(file)
	for index := range file.Commands {
		aggregate := &file.Commands[index]
		if aggregate.Dialect != syntax.Vim9 || aggregate.Aggregate == nil || aggregate.Aggregate.Kind != syntax.BlockInterface && aggregate.Aggregate.Kind != syntax.BlockClass ||
			len(aggregate.Aggregate.Extends) == 0 || aggregate.Block < 0 || aggregate.Block >= len(file.Blocks) || file.Blocks[aggregate.Block].End < 0 ||
			commandHasModifier(aggregate, "legacy") {
			continue
		}
		if aggregateHeaderHasSyntaxDiagnostic(file, aggregate) {
			continue
		}
		extendsName := file.Text(aggregate.Aggregate.Extends[0])
		scope := result.commandScopes[aggregate]
		if scope == nil {
			scope = result.Root
		}
		declaration := resolve(scope, extendsName, aggregate.Aggregate.Extends[0].Start, false, nil)
		if declaration == nil {
			if dot := strings.IndexByte(extendsName, '.'); dot > 0 {
				if prefix := resolve(scope, extendsName[:dot], aggregate.Aggregate.Extends[0].Start, false, nil); prefix != nil && prefix.Kind == SymbolKindImport {
					continue
				}
			}
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1353", Message: "Class name not found: " + extendsName, Span: aggregateEndSpan(file, aggregate),
			})
			continue
		}
		if declaration.Kind == SymbolKindVariable || declaration.Kind == SymbolKindConstant {
			if isUnknownType(declaration.Type) {
				continue
			}
		}
		kind, known := extendedAggregateTargetKind(result, scope, declaration, aliases, make(map[syntax.Span]bool))
		if !known {
			continue
		}
		valid := aggregate.Aggregate.Kind == syntax.BlockClass && kind == SymbolKindClass ||
			aggregate.Aggregate.Kind == syntax.BlockInterface && kind == SymbolKindInterface
		if valid && extendsName != file.Text(aggregate.Aggregate.Name) {
			continue
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code:    "vim/E1354",
			Message: "Cannot extend " + extendsName,
			Span:    aggregateEndSpan(file, aggregate),
		})
	}
}

func extendedAggregateTargetKind(result *FileAnalysis, scope *Scope, declaration *Declaration, aliases map[syntax.Span]*syntax.Type, seen map[syntax.Span]bool) (SymbolKind, bool) {
	if result == nil || result.File == nil || scope == nil || declaration == nil {
		return "", false
	}
	switch declaration.Kind {
	case SymbolKindClass, SymbolKindInterface, SymbolKindEnum:
		return declaration.Kind, true
	case SymbolKindTypeAlias:
		if seen[declaration.Span] {
			return "", false
		}
		typeNode := aliases[declaration.Span]
		if typeNode == nil || typeNode.Kind == syntax.TypeMissing || syntaxDiagnosticOverlaps(result.File.Diagnostics, typeNode.Span) {
			return "", false
		}
		if typeNode.Kind != syntax.TypeNamed {
			return "", true
		}
		if isKnownNonAggregateTypeName(typeNode.Name) {
			return "", true
		}
		target := resolve(scope, typeNode.Name, typeNode.Span.Start, false, nil)
		if target == nil {
			return "", false
		}
		seen[declaration.Span] = true
		kind, known := extendedAggregateTargetKind(result, scope, target, aliases, seen)
		delete(seen, declaration.Span)
		return kind, known
	default:
		return "", true
	}
}

func isKnownNonAggregateTypeName(name string) bool {
	switch name {
	case "any", "blob", "bool", "channel", "dict", "float", "func", "job", "list", "number", "object", "string", "tuple", "void":
		return true
	default:
		return false
	}
}

func collectImplementedInterfaceNameDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	for index := range file.Commands {
		class := &file.Commands[index]
		if class.Dialect != syntax.Vim9 || class.Aggregate == nil || class.Aggregate.Kind != syntax.BlockClass || len(class.Aggregate.Implements) == 0 ||
			class.Block < 0 || class.Block >= len(file.Blocks) || file.Blocks[class.Block].End < 0 || aggregateHasDuplicateMethodDiagnostic(result, class) {
			continue
		}
		if aggregateHeaderHasSyntaxDiagnostic(file, class) {
			continue
		}
		scope := result.commandScopes[class]
		if scope == nil {
			scope = result.Root
		}
		for _, implemented := range class.Aggregate.Implements {
			name := file.Text(implemented)
			if declaration := resolve(scope, name, implemented.Start, false, nil); declaration != nil {
				if declaration.Kind == SymbolKindInterface {
					continue
				}
				if declaration.Kind == SymbolKindVariable || declaration.Kind == SymbolKindConstant {
					if isUnknownType(declaration.Type) {
						break
					}
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E1347", Message: "Not a valid interface: " + name, Span: aggregateEndSpan(file, class),
					})
					break
				}
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1347", Message: "Not a valid interface: " + name, Span: aggregateEndSpan(file, class),
				})
				break
			}
			if dot := strings.IndexByte(name, '.'); dot > 0 {
				if prefix := resolve(scope, name[:dot], implemented.Start, false, nil); prefix != nil && prefix.Kind == SymbolKindImport {
					break
				}
			}
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1346", Message: "Interface name not found: " + name, Span: aggregateEndSpan(file, class),
			})
			break
		}
	}
}

func aggregateHeaderHasSyntaxDiagnostic(file *syntax.File, aggregate *syntax.Command) bool {
	if file == nil || aggregate == nil {
		return false
	}
	header := syntax.Span{Start: aggregate.Name.Start, End: aggregate.Argument.End}
	return slices.ContainsFunc(file.Diagnostics, func(diagnostic syntax.Diagnostic) bool {
		if diagnostic.Span.Start == diagnostic.Span.End {
			return diagnostic.Span.Start >= header.Start && diagnostic.Span.Start <= header.End
		}
		return diagnostic.Span.Start < header.End && diagnostic.Span.End > header.Start
	})
}

func collectImplementedInterfaceMembersDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	interfaces := localAggregates(file, syntax.BlockInterface)
	for index := range file.Commands {
		class := &file.Commands[index]
		if class.Dialect != syntax.Vim9 || class.Aggregate == nil || class.Aggregate.Kind != syntax.BlockClass || len(class.Aggregate.Implements) == 0 ||
			class.Block < 0 || class.Block >= len(file.Blocks) || file.Blocks[class.Block].End < 0 || aggregateHasDuplicateMethodDiagnostic(result, class) {
			continue
		}
		if aggregateHeaderHasSyntaxDiagnostic(file, class) {
			continue
		}
		scope := result.commandScopes[class]
		if scope == nil {
			scope = result.Root
		}
		resolvedImplements := make([]*syntax.Command, 0, len(class.Aggregate.Implements))
		for _, implemented := range class.Aggregate.Implements {
			name := file.Text(implemented)
			declaration := resolve(scope, name, implemented.Start, false, nil)
			if declaration == nil {
				if dot := strings.IndexByte(name, '.'); dot > 0 {
					if prefix := resolve(scope, name[:dot], implemented.Start, false, nil); prefix != nil && prefix.Kind == SymbolKindImport {
						resolvedImplements = nil
						break
					}
				}
				resolvedImplements = nil
				break
			}
			if declaration.Kind != SymbolKindInterface {
				resolvedImplements = nil
				break
			}
			resolved := interfaces[name]
			if resolved == nil {
				resolvedImplements = nil
				break
			}
			resolvedImplements = append(resolvedImplements, resolved)
		}
		if len(resolvedImplements) != len(class.Aggregate.Implements) {
			continue
		}
		implementedToName := make(map[*syntax.Command]string, len(resolvedImplements))
		for _, implemented := range class.Aggregate.Implements {
			interfaceName := file.Text(implemented)
			if iface := interfaces[interfaceName]; iface != nil {
				implementedToName[iface] = interfaceName
			}
		}
		resolveParentInterface := func(current *syntax.Command, parent syntax.Span) (*syntax.Command, bool) {
			name := file.Text(parent)
			scope := result.commandScopes[current]
			if scope == nil {
				scope = result.Root
			}
			declaration := resolve(scope, name, parent.Start, false, nil)
			if declaration == nil || declaration.Kind != SymbolKindInterface {
				return nil, false
			}
			resolved := interfaces[name]
			return resolved, resolved != nil
		}
		validateInterface := func(iface *syntax.Command, directInterfaceName string) bool {
			seenInterfaces := make(map[*syntax.Command]bool)
			var checkVariable func(current *syntax.Command) bool
			checkVariable = func(current *syntax.Command) bool {
				if current == nil || current.Aggregate == nil || current.Aggregate.Kind != syntax.BlockInterface || seenInterfaces[current] {
					return true
				}
				seenInterfaces[current] = true
				for _, parent := range current.Aggregate.Extends {
					parentInterface, ok := resolveParentInterface(current, parent)
					if !ok {
						return false
					}
					if !checkVariable(parentInterface) {
						return false
					}
				}
				for _, memberIndex := range current.Aggregate.Members {
					if memberIndex < 0 || memberIndex >= len(file.Commands) {
						continue
					}
					member := &file.Commands[memberIndex]
					if member.Declaration == nil || commandHasModifier(member, "static") || commandHasModifier(member, "public") {
						continue
					}
					for bindingIndex, binding := range member.Declaration.Bindings {
						name := file.Text(binding.Name)
						if name == "" || strings.HasPrefix(name, "_") {
							continue
						}
						expected := aggregateBindingType(result, member, bindingIndex)
						actualCommand, _, found := classObjectVariableBinding(result, class, name)
						if !found {
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
								Code: "vim/E1348", Message: `Variable "` + name + `" of interface "` + directInterfaceName + `" is not implemented`, Span: aggregateEndSpan(file, class),
							})
							return false
						}
						if classHasDuplicateVariableDiagnostic(result, class, name) || commandHasModifier(actualCommand, "public") {
							return false
						}
						actual, _ := classObjectVariableType(result, class, name)
						if !isUnknownType(expected) && !isUnknownType(actual) && !memberTypesCompatible(result, expected, actual) {
							return false
						}
					}
				}
				return true
			}
			if !checkVariable(iface) {
				return false
			}

			seenMethods := make(map[*syntax.Command]bool)
			var checkMethod func(current *syntax.Command) bool
			checkMethod = func(current *syntax.Command) bool {
				if current == nil || current.Aggregate == nil || current.Aggregate.Kind != syntax.BlockInterface || seenMethods[current] {
					return true
				}
				seenMethods[current] = true
				for _, parent := range current.Aggregate.Extends {
					parentInterface, ok := resolveParentInterface(current, parent)
					if !ok {
						return false
					}
					if !checkMethod(parentInterface) {
						return false
					}
				}
				for _, memberIndex := range current.Aggregate.Members {
					if memberIndex < 0 || memberIndex >= len(file.Commands) {
						continue
					}
					required := &file.Commands[memberIndex]
					if required.Function == nil || commandHasModifier(required, "static") {
						continue
					}
					name := file.Text(required.Function.Name)
					if name == "" || strings.HasPrefix(name, "_") {
						continue
					}
					actual := objectMethodInClassHierarchy(file, result.classes, class, name)
					if actual == nil {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E1349", Message: `Method "` + name + `" of interface "` + directInterfaceName + `" is not implemented`, Span: aggregateEndSpan(file, class),
						})
						return false
					}
					if methodSignaturesMismatch(required.Function, actual.Function) {
						return false
					}
				}
				return true
			}
			return checkMethod(iface)
		}
		for _, implemented := range resolvedImplements {
			interfaceName := implementedToName[implemented]
			if interfaceName == "" {
				continue
			}
			if !validateInterface(implemented, interfaceName) {
				break
			}
		}
	}
}

func collectConstructorDefaultValueDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	for index := range file.Commands {
		aggregate := &file.Commands[index]
		if aggregate.Dialect != syntax.Vim9 || aggregate.Aggregate == nil ||
			(aggregate.Aggregate.Kind != syntax.BlockClass && aggregate.Aggregate.Kind != syntax.BlockInterface && aggregate.Aggregate.Kind != syntax.BlockEnum) {
			continue
		}
		for _, memberIndex := range aggregate.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			member := &file.Commands[memberIndex]
			if member.Canonical != "def" || member.Function == nil || !strings.HasPrefix(file.Text(member.Function.Name), "new") {
				continue
			}
			for _, parameter := range member.Function.Parameters {
				if parameter.Target == nil || parameter.Target.Kind != syntax.ExpressionMember || len(parameter.Target.Children) != 1 || parameter.Target.Children[0] == nil ||
					parameter.Target.Children[0].Kind != syntax.ExpressionIdentifier || parameter.Target.Children[0].Value != "this" || parameter.Target.Value == "" ||
					file.Text(parameter.Target.Operator) != "." || parameter.Type != nil || parameter.Default == nil || parameter.Default.Kind == syntax.ExpressionMissing {
					continue
				}
				defaultText := file.Text(parameter.DefaultSpan)
				if strings.HasPrefix(strings.TrimLeft(defaultText, " \t"), "v:none") {
					continue
				}
				span := syntax.Span{Start: parameter.Name.End, End: parameter.DefaultSpan.End}
				if span.Start > span.End || span.End > len(file.Source) {
					continue
				}
				tail := file.Text(span)
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1328", Message: "Constructor default value must be v:none: " + tail, Span: span,
				})
			}
		}
	}
}

func collectTypeDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	aliases := localTypeAliases(file)
	appendTypeDiagnostic := func(typeNode *syntax.Type, scope *Scope, allowVoid bool) {
		if invalid := invalidObjectValueType(result, scope, typeNode, aliases, make(map[syntax.Span]bool)); invalid != (syntax.Span{}) {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1353", Message: "Class name not found: " + file.Text(invalid), Span: invalid,
			})
		} else if invalid := invalidVoidValueType(result, scope, typeNode, allowVoid, aliases, make(map[syntax.Span]bool)); invalid != nil {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1330", Message: "Invalid type used in variable declaration: void", Span: invalid.Span,
			})
		}
	}
	seen := make(map[*syntax.Expression]bool)
	var walkCommands func([]syntax.Command, *Scope)
	var walkExpression func(*syntax.Expression, *Scope, syntax.Dialect)
	walkExpression = func(expression *syntax.Expression, scope *Scope, dialect syntax.Dialect) {
		if expression == nil || seen[expression] {
			return
		}
		seen[expression] = true
		if dialect != syntax.Vim9 {
			return
		}
		for _, typeNode := range expression.TypeArguments {
			appendTypeDiagnostic(typeNode, scope, false)
		}
		if expression.Kind == syntax.ExpressionCast {
			appendTypeDiagnostic(expression.CastType, scope, false)
		}
		expressionScope := scope
		if expression.Kind == syntax.ExpressionLambda {
			if lambdaScope := result.lambdaScopes[expression]; lambdaScope != nil {
				expressionScope = lambdaScope
			}
			for _, parameter := range expression.Parameters {
				appendTypeDiagnostic(parameter.Type, expressionScope, false)
			}
			appendTypeDiagnostic(expression.ReturnType, expressionScope, true)
			if expression.LambdaBody != nil {
				walkCommands(expression.LambdaBody.Commands, expressionScope)
			}
		}
		for _, child := range expression.Children {
			walkExpression(child, expressionScope, dialect)
		}
	}
	walkCommands = func(commands []syntax.Command, fallback *Scope) {
		for index := range commands {
			command := &commands[index]
			scope := result.commandScopes[command]
			if scope == nil {
				scope = fallback
			}
			if command.Dialect == syntax.Vim9 {
				if command.TypeAlias != nil {
					appendTypeDiagnostic(command.TypeAlias.Type, scope, true)
				}
				if command.Declaration != nil {
					for _, binding := range command.Declaration.Bindings {
						appendTypeDiagnostic(binding.ParsedType, scope, false)
					}
				}
				if command.For != nil {
					for _, binding := range command.For.Bindings {
						appendTypeDiagnostic(binding.ParsedType, scope, false)
					}
				}
			}
			if command.Canonical == "def" && command.Function != nil {
				for _, parameter := range command.Function.Parameters {
					appendTypeDiagnostic(parameter.Type, scope, false)
				}
				appendTypeDiagnostic(command.Function.ReturnType, scope, true)
			}
			expressionDialect := command.Dialect
			if command.Canonical == "def" {
				expressionDialect = syntax.Vim9
			}
			for _, expression := range command.Expressions {
				walkExpression(expression, scope, expressionDialect)
			}
			for _, expression := range command.Targets {
				walkExpression(expression, scope, expressionDialect)
			}
			if command.Mapping != nil {
				walkExpression(command.Mapping.RHSExpression, scope, expressionDialect)
			}
			if command.Declaration != nil {
				walkExpression(command.Declaration.Initializer, scope, expressionDialect)
			}
			if command.For != nil {
				walkExpression(command.For.Iterable, scope, expressionDialect)
			}
			if command.Import != nil {
				walkExpression(command.Import.Path, scope, expressionDialect)
			}
			for _, value := range command.EnumValues {
				walkExpression(value.Initializer, scope, expressionDialect)
				for _, argument := range value.Arguments {
					walkExpression(argument, scope, expressionDialect)
				}
			}
			if command.Function != nil {
				for _, parameter := range command.Function.Parameters {
					walkExpression(parameter.Default, scope, expressionDialect)
				}
			}
			if command.Embedded != nil {
				walkCommands(command.Embedded.Commands, scope)
			}
		}
	}
	walkCommands(file.Commands, result.Root)
}

func localTypeAliases(file *syntax.File) map[syntax.Span]*syntax.Type {
	aliases := make(map[syntax.Span]*syntax.Type)
	if file == nil {
		return aliases
	}
	var collect func([]syntax.Command)
	collect = func(commands []syntax.Command) {
		for index := range commands {
			command := &commands[index]
			if command.TypeAlias != nil {
				aliases[command.TypeAlias.Name] = command.TypeAlias.Type
			}
			if command.Embedded != nil {
				collect(command.Embedded.Commands)
			}
		}
	}
	collect(file.Commands)
	return aliases
}

func invalidVoidValueType(result *FileAnalysis, scope *Scope, typeNode *syntax.Type, allowVoid bool, aliases map[syntax.Span]*syntax.Type, seen map[syntax.Span]bool) *syntax.Type {
	if result == nil || scope == nil || typeNode == nil || typeNode.Kind == syntax.TypeMissing {
		return nil
	}
	if typeNode.Kind == syntax.TypeNamed {
		if typeNode.Name == "void" {
			if allowVoid {
				return nil
			}
			return typeNode
		}
		if declaration := resolve(scope, typeNode.Name, typeNode.Span.Start, false, nil); declaration != nil && declaration.Kind == SymbolKindTypeAlias && !seen[declaration.Span] {
			if alias := aliases[declaration.Span]; alias != nil {
				seen[declaration.Span] = true
				invalid := invalidVoidValueType(result, scope, alias, allowVoid, aliases, seen)
				delete(seen, declaration.Span)
				if invalid != nil {
					return typeNode
				}
			}
		}
	}
	for _, argument := range typeNode.Arguments {
		if invalid := invalidVoidValueType(result, scope, argument, false, aliases, seen); invalid != nil {
			return invalid
		}
	}
	if invalid := invalidVoidValueType(result, scope, typeNode.ReturnType, true, aliases, seen); invalid != nil {
		return invalid
	}
	return nil
}

func invalidObjectValueType(result *FileAnalysis, scope *Scope, typeNode *syntax.Type, aliases map[syntax.Span]*syntax.Type, seen map[syntax.Span]bool) syntax.Span {
	if result == nil || result.File == nil || scope == nil || typeNode == nil || typeNode.Kind == syntax.TypeMissing {
		return syntax.Span{}
	}
	file := result.File
	if syntaxDiagnosticOverlaps(file.Diagnostics, typeNode.Span) {
		return syntax.Span{}
	}
	for _, argument := range typeNode.Arguments {
		if invalid := invalidObjectValueType(result, scope, argument, aliases, seen); invalid != (syntax.Span{}) {
			return invalid
		}
	}
	if invalid := invalidObjectValueType(result, scope, typeNode.ReturnType, aliases, seen); invalid != (syntax.Span{}) {
		return invalid
	}
	if typeNode.Kind != syntax.TypeGeneric || typeNode.Name != "object" {
		return syntax.Span{}
	}
	if len(typeNode.Arguments) != 1 || typeNode.Arguments[0] == nil {
		return syntax.Span{}
	}
	inner := typeNode.Arguments[0]
	switch inner.Kind {
	case syntax.TypeGeneric, syntax.TypeFunction, syntax.TypeVariadic, syntax.TypeOptional, syntax.TypeNamed:
	default:
		return syntax.Span{}
	}
	switch objectTypeArgumentValidity(result, scope, inner, aliases, seen) {
	case objectTypeValid, objectTypeUnknown:
		return syntax.Span{}
	}
	return objectTypeSuffixSpan(file, typeNode)
}

type objectTypeValidity uint8

const (
	objectTypeUnknown objectTypeValidity = iota
	objectTypeInvalid
	objectTypeValid
)

func objectTypeArgumentValidity(result *FileAnalysis, scope *Scope, typeNode *syntax.Type, aliases map[syntax.Span]*syntax.Type, seen map[syntax.Span]bool) objectTypeValidity {
	if result == nil || result.File == nil || scope == nil || typeNode == nil || typeNode.Kind == syntax.TypeMissing {
		return objectTypeUnknown
	}
	if syntaxDiagnosticOverlaps(result.File.Diagnostics, typeNode.Span) {
		return objectTypeUnknown
	}
	if typeNode.Kind == syntax.TypeNamed {
		name := typeNode.Name
		if name == "any" {
			return objectTypeValid
		}
		switch name {
		case "bool", "number", "float", "string", "special", "dict", "list", "tuple", "blob", "func", "partial", "job", "channel", "void":
			return objectTypeInvalid
		}
		declaration := resolve(scope, name, typeNode.Span.Start, false, nil)
		if declaration == nil {
			if dot := strings.IndexByte(name, '.'); dot > 0 {
				if prefix := resolve(scope, name[:dot], typeNode.Span.Start, false, nil); prefix != nil && prefix.Kind == SymbolKindImport {
					return objectTypeUnknown
				}
			}
			return objectTypeUnknown
		}
		switch declaration.Kind {
		case SymbolKindClass, SymbolKindInterface, SymbolKindEnum:
			return objectTypeValid
		case SymbolKindTypeAlias:
			alias := aliases[declaration.Span]
			if alias == nil || seen[declaration.Span] {
				return objectTypeUnknown
			}
			seen[declaration.Span] = true
			valid := objectTypeArgumentValidity(result, scope, alias, aliases, seen)
			delete(seen, declaration.Span)
			return valid
		default:
			return objectTypeUnknown
		}
	}
	if typeNode.Kind == syntax.TypeOptional || typeNode.Kind == syntax.TypeVariadic {
		if len(typeNode.Arguments) == 0 {
			return objectTypeUnknown
		}
		return objectTypeArgumentValidity(result, scope, typeNode.Arguments[0], aliases, seen)
	}
	if typeNode.Kind == syntax.TypeGeneric {
		if typeNode.Name == "object" && len(typeNode.Arguments) == 1 {
			return objectTypeArgumentValidity(result, scope, typeNode.Arguments[0], aliases, seen)
		}
		return objectTypeInvalid
	}
	return objectTypeInvalid
}

func objectTypeSuffixSpan(file *syntax.File, typeNode *syntax.Type) syntax.Span {
	if file == nil || typeNode == nil || len(typeNode.Arguments) == 0 {
		return typeNode.Span
	}
	start := typeNode.Span.Start
	if start < 0 || typeNode.Span.End <= start || typeNode.Span.End > len(file.Source) {
		return typeNode.Span
	}
	depth := 0
	for index := typeNode.Span.Start; index < typeNode.Span.End; index++ {
		switch file.Source[index] {
		case '<':
			if depth == 0 {
				start = index
			}
			depth++
		case '>':
			depth--
			if depth == 0 {
				return syntax.Span{Start: start, End: index + 1}
			}
		}
	}
	return typeNode.Arguments[0].Span
}

func collectNameOnlyExpressionDiagnostics(result *FileAnalysis, commands []syntax.Command, parent *Scope) {
	if result == nil || result.File == nil {
		return
	}
	seen := make(map[syntax.Span]bool)
	var walkCommands func([]syntax.Command, *Scope)
	var walkExpression func(*syntax.Expression, *Scope)
	appendDiagnostic := func(expression *syntax.Expression) {
		if expression == nil || expressionContainsMissing(expression) || syntaxDiagnosticOverlaps(result.File.Diagnostics, expression.Span) || seen[expression.Span] {
			return
		}
		seen[expression.Span] = true
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1207", Message: "Expression without an effect: " + result.File.Text(expression.Span), Span: expression.Span})
	}
	appendUnknownCommandDiagnostic := func(command *syntax.Command, scope *Scope) {
		span := command.Name
		if command.Argument.End > command.Argument.Start {
			span.End = command.Argument.End
		}
		if syntaxDiagnosticOverlaps(result.File.Diagnostics, span) || syntaxDiagnosticOverlaps(result.Diagnostics, span) {
			return
		}
		code := "vim/E492"
		message := "Not an editor command: " + result.File.Text(span)
		for current := scope; current != nil; current = current.Parent {
			if current.Kind == syntax.BlockDef {
				code = "vim/E476"
				message = "Invalid command: " + result.File.Text(span)
				break
			}
			if current.Kind == syntax.BlockFunction {
				break
			}
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: code, Message: message, Span: span})
	}
	nameOnly := func(expression *syntax.Expression, scope *Scope, eval bool) bool {
		if expression == nil || expressionContainsMissing(expression) {
			return false
		}
		if eval && expression.Kind == syntax.ExpressionString {
			return true
		}
		if expression.Kind != syntax.ExpressionIdentifier {
			return false
		}
		name := expression.Value
		if strings.HasPrefix(name, "@") || strings.HasPrefix(name, "$") {
			return true
		}
		if strings.HasPrefix(name, "&") {
			optionName := name
			if strings.HasPrefix(optionName, "&l:") || strings.HasPrefix(optionName, "&g:") {
				optionName = "&" + optionName[3:]
			}
			_, ok := vimdata.LookupOption(optionName)
			return ok || vimdata.IsNeovimCompatOption(optionName)
		}
		if isLiteralIdentifier(name) {
			return true
		}
		if strings.HasPrefix(name, "v:") {
			_, ok := vimdata.LookupVariable(name)
			return ok || vimdata.IsNeovimCompatVariable(name)
		}
		declaration := resolve(scope, name, expression.Span.Start, false, nil)
		return declaration != nil && (declaration.Kind == SymbolKindVariable || declaration.Kind == SymbolKindConstant || declaration.Parameter)
	}
	walkExpression = func(expression *syntax.Expression, scope *Scope) {
		if expression == nil {
			return
		}
		if expression.Kind == syntax.ExpressionLambda && expression.LambdaBody != nil {
			lambdaScope := result.lambdaScopes[expression]
			if lambdaScope == nil {
				lambdaScope = scope
			}
			walkCommands(expression.LambdaBody.Commands, lambdaScope)
		}
		for _, child := range expression.Children {
			walkExpression(child, scope)
		}
	}
	walkCommands = func(commands []syntax.Command, inherited *Scope) {
		for index := range commands {
			command := &commands[index]
			scope := result.commandScopes[command]
			if scope == nil {
				scope = inherited
			}
			if command.Dialect == syntax.Vim9 {
				if command.Canonical == "defcompile" && scope == result.Root {
					raw := result.File.Text(command.Argument)
					name := strings.TrimSpace(raw)
					if name != "" && !strings.ContainsAny(name, " \t\r\n") {
						start := command.Argument.Start + len(raw) - len(strings.TrimLeft(raw, " \t\r\n"))
						span := syntax.Span{Start: start, End: start + len(name)}
						declaration := resolve(scope, name, span.Start, false, nil)
						if declaration != nil && declaration.Scope == result.Root && (declaration.Kind == SymbolKindVariable || declaration.Kind == SymbolKindConstant) {
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1061", Message: "Cannot find function " + name, Span: span})
						}
					}
				}
				if command.Kind == syntax.CommandExpression || command.Canonical == "eval" {
					for _, expression := range command.Expressions {
						if expression != nil && expression.Span == command.Argument && nameOnly(expression, scope, command.Canonical == "eval") {
							appendDiagnostic(expression)
						}
					}
				}
				if command.Argument.Start == command.Argument.End && command.Range.Start == command.Range.End && command.Bang.Start == command.Bang.End &&
					(command.Kind == syntax.CommandBuiltin || command.Kind == syntax.CommandUnknown) {
					name := result.File.Text(command.Name)
					if declaration := resolve(scope, name, command.Name.Start, false, nil); declaration != nil && (declaration.Kind == SymbolKindVariable || declaration.Kind == SymbolKindConstant || declaration.Parameter) {
						appendDiagnostic(&syntax.Expression{Kind: syntax.ExpressionIdentifier, Span: command.Name, Value: name})
					} else if command.Kind == syntax.CommandUnknown && len(command.EnumValues) == 0 && len(name) > 0 && name[0] >= 'a' && name[0] <= 'z' {
						appendUnknownCommandDiagnostic(command, scope)
					}
				} else if command.Kind == syntax.CommandUnknown && len(command.EnumValues) == 0 && len(command.TypedName) > 0 &&
					command.TypedName[0] >= 'a' && command.TypedName[0] <= 'z' {
					declaration := resolve(scope, command.TypedName, command.Name.Start, false, nil)
					if declaration == nil || declaration.Kind != SymbolKindVariable && declaration.Kind != SymbolKindConstant && !declaration.Parameter {
						appendUnknownCommandDiagnostic(command, scope)
					}
				}
			}
			for _, expression := range command.Expressions {
				walkExpression(expression, scope)
			}
			if command.Declaration != nil {
				walkExpression(command.Declaration.Initializer, scope)
			}
			if command.Embedded != nil {
				walkCommands(command.Embedded.Commands, scope)
			}
		}
	}
	walkCommands(commands, parent)
}

func collectArgumentShadowDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	for _, declaration := range result.Declarations {
		if declaration == nil || !declaration.Parameter || declaration.Name == "_" || declaration.Scope == nil {
			continue
		}
		scope := declaration.Scope
		aggregateConflict := aggregateArgumentConflict(result, declaration)
		compiled := scope.Kind == syntax.BlockDef || scope.Lambda != nil && result.File.Text(scope.Lambda.Operator) == "=>" || aggregateConflict
		if !compiled || syntaxDiagnosticTouchesCall(result.File.Diagnostics, declaration.Span) {
			continue
		}
		if scriptArgumentConflict(result, declaration) {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1168", Message: "Argument already declared in the script: " + argumentScriptMessageTail(result.File, declaration), Span: declaration.Span})
			continue
		}
		if aggregateConflict {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1340", Message: "Argument already declared in the class: " + declaration.Name, Span: declaration.Span})
			continue
		}
		for parent := scope.Parent; parent != nil && parent != result.Root; parent = parent.Parent {
			if parent.Kind == syntax.BlockClass || parent.Kind == syntax.BlockInterface || parent.Kind == syntax.BlockEnum {
				continue
			}
			for _, outer := range parent.Declarations {
				if outer.Name == declaration.Name && outer.Span.Start < declaration.Span.Start && (outer.Kind == SymbolKindVariable || outer.Kind == SymbolKindConstant || outer.Parameter) {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1167", Message: "Argument name shadows existing variable: " + declaration.Name, Span: declaration.Span})
					goto next
				}
			}
		}
	next:
	}
}

func aggregateArgumentConflict(result *FileAnalysis, parameter *Declaration) bool {
	if result == nil || result.File == nil || parameter == nil || parameter.Scope == nil || parameter.Scope.Lambda != nil {
		return false
	}
	file := result.File
	aggregate := enclosingAggregateCommand(file, parameter.Scope)
	if aggregate == nil || aggregate.Aggregate == nil || (aggregate.Aggregate.Kind != syntax.BlockClass && aggregate.Aggregate.Kind != syntax.BlockEnum) {
		return false
	}
	var method *syntax.Command
	for _, memberIndex := range aggregate.Aggregate.Members {
		if memberIndex < 0 || memberIndex >= len(file.Commands) {
			continue
		}
		candidate := &file.Commands[memberIndex]
		if candidate.Dialect != syntax.Vim9 || candidate.Canonical != "def" || candidate.Function == nil {
			continue
		}
		for _, candidateParameter := range candidate.Function.Parameters {
			if parameterDeclarationSpan(file, candidateParameter) == parameter.Span {
				method = candidate
				break
			}
		}
		if method != nil {
			break
		}
	}
	if method == nil || syntaxDiagnosticOverlaps(file.Diagnostics, method.Span) {
		return false
	}
	seenParameters := make(map[string]bool)
	for _, candidate := range method.Function.Parameters {
		name := file.Text(parameterDeclarationSpan(file, candidate))
		if name == "_" {
			continue
		}
		if seenParameters[name] {
			return false
		}
		seenParameters[name] = true
	}
	return aggregateHasVisibleStaticVariable(result, aggregate, parameter.Name)
}

func collectAggregateLocalRedeclarationDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	eligible := make(map[syntax.Span]bool)
	var collectEligible func([]syntax.Command)
	collectEligible = func(commands []syntax.Command) {
		for index := range commands {
			command := &commands[index]
			if command.Dialect == syntax.Vim9 {
				if command.Declaration != nil && (command.Canonical == "var" || command.Canonical == "final" || command.Canonical == "const") {
					for _, binding := range command.Declaration.Bindings {
						eligible[binding.Name] = true
					}
				}
				if command.For != nil {
					for _, binding := range command.For.Bindings {
						eligible[binding.Name] = true
					}
				}
				if command.Canonical == "def" && command.Function != nil {
					eligible[command.Function.Name] = true
				}
			}
			if command.Embedded != nil {
				collectEligible(command.Embedded.Commands)
			}
		}
	}
	collectEligible(result.File.Commands)
	for _, declaration := range result.Declarations {
		if declaration == nil || declaration.Parameter || declaration.Name == "_" || declaration.Scope == nil || !eligible[declaration.Span] ||
			syntaxDiagnosticOverlaps(result.File.Diagnostics, declaration.Span) || syntaxDiagnosticTouchesCall(result.File.Diagnostics, declaration.Span) ||
			(declaration.Kind != SymbolKindVariable && declaration.Kind != SymbolKindConstant && declaration.Kind != SymbolKindFunction) {
			continue
		}
		aggregate := directMethodAggregate(result.File, declaration.Scope)
		if aggregate == nil || scriptArgumentConflict(result, declaration) || !aggregateHasVisibleStaticVariable(result, aggregate, declaration.Name) {
			continue
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1341", Message: "Variable already declared in the class: " + declaration.Name, Span: declaration.Span})
		filtered := result.Diagnostics[:0]
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Span == declaration.Span && (diagnostic.Code == "vim/E1006" || diagnostic.Code == "vim/E1017") {
				continue
			}
			filtered = append(filtered, diagnostic)
		}
		result.Diagnostics = filtered
	}
}

func directMethodAggregate(file *syntax.File, scope *Scope) *syntax.Command {
	for current := scope; file != nil && current != nil; current = current.Parent {
		if current.Lambda != nil || current.Kind == syntax.BlockFunction {
			return nil
		}
		if current.Kind != syntax.BlockDef || current.CommandList != nil || current.Block < 0 || current.Block >= len(file.Blocks) {
			continue
		}
		header := file.Blocks[current.Block].Header
		if header < 0 || header >= len(file.Commands) {
			return nil
		}
		method := &file.Commands[header]
		if method.Dialect != syntax.Vim9 || method.Canonical != "def" || method.Function == nil {
			return nil
		}
		aggregate := enclosingAggregateCommand(file, current)
		if aggregate == nil || aggregate.Aggregate == nil {
			return nil
		}
		switch aggregate.Aggregate.Kind {
		case syntax.BlockClass, syntax.BlockEnum:
		default:
			return nil
		}
		if slices.Contains(aggregate.Aggregate.Members, header) {
			return aggregate
		}
		return nil
	}
	return nil
}

func aggregateHasVisibleStaticVariable(result *FileAnalysis, aggregate *syntax.Command, name string) bool {
	if result == nil || result.File == nil || aggregate == nil || aggregate.Aggregate == nil {
		return false
	}
	if aggregate.Aggregate.Kind == syntax.BlockEnum {
		_, _, found := aggregateVariableBinding(result.File, aggregate, name, true)
		return found
	}
	for current, seen := aggregate, make(map[*syntax.Command]bool); current != nil && !seen[current]; current = extendedClass(result.File, result.classes, current) {
		seen[current] = true
		if _, _, found := aggregateVariableBinding(result.File, current, name, true); found {
			return true
		}
	}
	return false
}

func scriptArgumentConflict(result *FileAnalysis, parameter *Declaration) bool {
	deferred := scopeContainsDef(parameter.Scope)
	for scope := parameter.Scope.Parent; scope != nil; scope = scope.Parent {
		scriptLevel := true
		for parent := scope; parent != nil && parent != result.Root; parent = parent.Parent {
			if parent.Kind == syntax.BlockDef || parent.Kind == syntax.BlockFunction || parent.Lambda != nil || parent.Kind == syntax.BlockClass || parent.Kind == syntax.BlockInterface || parent.Kind == syntax.BlockEnum {
				scriptLevel = false
				break
			}
		}
		if !scriptLevel {
			continue
		}
		for _, declaration := range scope.Declarations {
			if declaration.Name != parameter.Name || !deferred && declaration.Span.Start >= parameter.Span.Start {
				continue
			}
			switch declaration.Kind {
			case SymbolKindVariable, SymbolKindConstant, SymbolKindTypeAlias, SymbolKindClass, SymbolKindInterface, SymbolKindEnum:
				return true
			}
		}
	}
	return false
}

func argumentScriptMessageTail(file *syntax.File, parameter *Declaration) string {
	if file == nil || parameter == nil || parameter.Scope == nil {
		return ""
	}
	end := parameter.Span.End
	if parameter.Scope.Kind == syntax.BlockDef {
		end = enclosingDefHeaderSpan(file, parameter.Scope).End
	} else if parameter.Scope.Lambda != nil {
		end = parameter.Scope.Lambda.Operator.Start
	}
	if parameter.Span.Start < 0 || end < parameter.Span.Start || end > len(file.Source) {
		return parameter.Name
	}
	return strings.TrimRight(file.Source[parameter.Span.Start:end], " \t")
}

func collectDuplicateTypeAliasDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	classes := localAggregates(file, syntax.BlockClass)
	seen := make(map[string]bool)
	classAliases := make(map[string]bool)
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Dialect != syntax.Vim9 || command.TypeAlias == nil {
			continue
		}
		scope := result.commandScopes[command]
		if scopeContainsDef(scope) || scopeInsideAggregate(scope) {
			continue
		}
		name := file.Text(command.TypeAlias.Name)
		if seen[name] {
			diagnostic := syntax.Diagnostic{Span: command.TypeAlias.Name}
			if classAliases[name] {
				diagnostic.Code = "vim/E1041"
				diagnostic.Message = `Redefining script item: "` + name + `"`
			} else {
				diagnostic.Code = "vim/E1396"
				diagnostic.Message = `Type alias "` + name + `" already exists`
			}
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			continue
		}
		seen[name] = true
		typeNode := command.TypeAlias.Type
		if typeNode != nil && typeNode.Kind == syntax.TypeNamed && (classes[typeNode.Name] != nil || classAliases[typeNode.Name]) {
			classAliases[name] = true
		}
	}
}

// collectLegacyConstExistingVariableDiagnostics reports E995 only while a
// straight-line sequence of legacy declarations proves that a function-local
// variable still exists. Any other command discards the fact.
func collectLegacyConstExistingVariableDiagnostics(result *FileAnalysis, commands []syntax.Command) {
	if result == nil || result.File == nil {
		return
	}
	seen := make(map[string]bool)
	var previousScope *Scope
	for index := range commands {
		command := &commands[index]
		scope := result.commandScopes[command]
		if scope != previousScope {
			clear(seen)
		}
		previousScope = scope
		if command.Dialect != syntax.Legacy || scope == nil || scope.Kind != syntax.BlockFunction || command.Declaration == nil ||
			(command.Canonical != "let" && command.Canonical != "const") {
			clear(seen)
			continue
		}
		for _, binding := range command.Declaration.Bindings {
			name := result.File.Text(binding.Name)
			if strings.Contains(name, "{") || !assignmentTargetNeedsDeclaration(name) {
				continue
			}
			if command.Canonical == "const" && seen[name] {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E995", Message: "Cannot modify existing variable", Span: binding.Name,
				})
			}
			seen[name] = true
		}
	}
}

func scopeInsideAggregate(scope *Scope) bool {
	for current := scope; current != nil; current = current.Parent {
		switch current.Kind {
		case syntax.BlockClass, syntax.BlockInterface, syntax.BlockEnum:
			return true
		}
	}
	return false
}

func collectPublicProtectedMemberNameDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	classes := localAggregates(file, syntax.BlockClass)
	for index := range file.Commands {
		class := &file.Commands[index]
		if class.Dialect != syntax.Vim9 || class.Aggregate == nil || class.Aggregate.Kind != syntax.BlockClass {
			continue
		}
		var seen []classVariableMember
		for _, memberIndex := range class.Aggregate.Members {
			member, ok := classVariableAt(file, memberIndex)
			if !ok {
				continue
			}
			if member.protected && commandHasModifier(&file.Commands[memberIndex], "public") {
				continue
			}
			conflict := false
			for _, previous := range seen {
				if member.base == previous.base && member.protected != previous.protected {
					appendPublicProtectedMemberNameDiagnostic(result, member)
					conflict = true
					break
				}
			}
			seen = append(seen, member)
			if conflict || member.static {
				continue
			}
			visited := map[*syntax.Command]bool{class: true}
			for parent := extendedClass(file, classes, class); parent != nil && !visited[parent]; parent = extendedClass(file, classes, parent) {
				visited[parent] = true
				for _, parentMemberIndex := range parent.Aggregate.Members {
					parentMember, ok := classVariableAt(file, parentMemberIndex)
					if ok && !parentMember.static && member.base == parentMember.base && member.protected != parentMember.protected {
						appendPublicProtectedMemberNameDiagnostic(result, member)
						conflict = true
						break
					}
				}
				if conflict {
					break
				}
			}
		}
	}
}

func collectPublicUnderscoreVariableDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	for index := range file.Commands {
		aggregate := &file.Commands[index]
		if aggregate.Dialect != syntax.Vim9 || aggregate.Aggregate == nil ||
			(aggregate.Aggregate.Kind != syntax.BlockClass && aggregate.Aggregate.Kind != syntax.BlockInterface && aggregate.Aggregate.Kind != syntax.BlockEnum) {
			continue
		}
		for _, memberIndex := range aggregate.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			command := &file.Commands[memberIndex]
			if command.Declaration == nil || command.Canonical != "var" && command.Canonical != "final" && command.Canonical != "const" ||
				!commandHasModifier(command, "public") || !strings.HasPrefix(file.Text(command.Declaration.Name), "_") {
				continue
			}
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1332", Message: "public variable name cannot start with underscore: " + file.Text(command.Span), Span: command.Span,
			})
		}
	}
}

func collectUninitializedObjectVariableDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	for index := range file.Commands {
		class := &file.Commands[index]
		if class.Dialect != syntax.Vim9 || class.Aggregate == nil || class.Aggregate.Kind != syntax.BlockClass {
			continue
		}
		for _, memberIndex := range class.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			member := &file.Commands[memberIndex]
			declaration := member.Declaration
			if declaration == nil || declaration.Initializer == nil || commandHasModifier(member, "static") ||
				syntaxDiagnosticOverlaps(file.Diagnostics, declaration.Initializer.Span) {
				continue
			}
			name := file.Text(declaration.Name)
			if span, found := uninitializedSelfReference(file, declaration.Initializer, name); found {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1430", Message: "Uninitialized object variable '" + name + "' referenced", Span: span,
				})
			}
		}
	}
}

func uninitializedSelfReference(file *syntax.File, expression *syntax.Expression, name string) (syntax.Span, bool) {
	if file == nil || expression == nil || name == "" || expression.Kind == syntax.ExpressionLambda {
		return syntax.Span{}, false
	}
	if expression.Kind == syntax.ExpressionMember && expression.Value == name && len(expression.Children) > 0 {
		receiver := expression.Children[0]
		for receiver != nil && receiver.Kind == syntax.ExpressionParenthesized && len(receiver.Children) == 1 {
			receiver = receiver.Children[0]
		}
		if receiver != nil && receiver.Kind == syntax.ExpressionIdentifier && receiver.Value == "this" && file.Text(expression.Operator) == "." {
			return memberNameSpan(file, expression), true
		}
	}
	start := 0
	if expression.Kind == syntax.ExpressionAssignment {
		start = 1
	}
	for _, child := range expression.Children[start:] {
		if span, found := uninitializedSelfReference(file, child, name); found {
			return span, true
		}
	}
	return syntax.Span{}, false
}

func collectDuplicateMethodDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	for index := range file.Commands {
		aggregate := &file.Commands[index]
		if aggregate.Dialect != syntax.Vim9 || aggregate.Aggregate == nil ||
			aggregate.Aggregate.Kind != syntax.BlockClass && aggregate.Aggregate.Kind != syntax.BlockInterface && aggregate.Aggregate.Kind != syntax.BlockEnum {
			continue
		}
		seen := make(map[string]bool)
		for _, memberIndex := range aggregate.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			member := &file.Commands[memberIndex]
			if member.Dialect != syntax.Vim9 || member.Canonical != "def" || member.Function == nil {
				continue
			}
			diagnosticSpan, complete := completedAggregateMethodSpan(file, aggregate, member)
			if !complete {
				continue
			}
			name := file.Text(member.Function.Name)
			if commandHasModifier(aggregate, "abstract") && (strings.HasPrefix(name, "new") || strings.HasPrefix(name, "_new")) {
				continue
			}
			base := strings.TrimPrefix(name, "_")
			if base == "" {
				continue
			}
			if seen[base] {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1355", Message: "Duplicate function: " + name, Span: diagnosticSpan,
				})
				filtered := result.Diagnostics[:0]
				for _, diagnostic := range result.Diagnostics {
					if diagnostic.Code == "vim/E1073" && diagnostic.Span == member.Function.Name {
						continue
					}
					filtered = append(filtered, diagnostic)
				}
				result.Diagnostics = filtered
				break
			}
			seen[base] = true
		}
	}
}

func collectAbstractConstructorDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	for index := range file.Commands {
		aggregate := &file.Commands[index]
		if aggregate.Dialect != syntax.Vim9 || aggregate.Aggregate == nil || aggregate.Aggregate.Kind != syntax.BlockClass || !commandHasModifier(aggregate, "abstract") {
			continue
		}
		for _, memberIndex := range aggregate.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			member := &file.Commands[memberIndex]
			if member.Dialect != syntax.Vim9 || member.Canonical != "def" || member.Function == nil {
				continue
			}
			name := file.Text(member.Function.Name)
			if !strings.HasPrefix(name, "new") && !strings.HasPrefix(name, "_new") {
				continue
			}
			span, complete := completedAggregateMethodSpan(file, aggregate, member)
			if !complete {
				continue
			}
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1359", Message: `Cannot define a "new" method in an abstract class`, Span: span,
			})
			break
		}
	}
}

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
		dialect    syntax.Dialect
	}
	var expressions []scopedExpression
	candidates := make(map[*Declaration]bool)
	seenLambdas := make(map[*syntax.Expression]bool)
	var collectCommands func([]syntax.Command, *Scope)
	var collectLambdaBodies func(*syntax.Expression, *Scope)
	collectLambdaBodies = func(expression *syntax.Expression, scope *Scope) {
		if expression == nil {
			return
		}
		expressionScope := scope
		if expression.Kind == syntax.ExpressionLambda && !seenLambdas[expression] {
			seenLambdas[expression] = true
			lambdaScope := result.lambdaScopes[expression]
			if lambdaScope == nil {
				lambdaScope = scope
			}
			expressionScope = lambdaScope
			if expression.LambdaBody != nil {
				collectCommands(expression.LambdaBody.Commands, lambdaScope)
			}
		}
		for index, child := range expression.Children {
			if expression.Kind != syntax.ExpressionLambda || index >= len(expression.Parameters) {
				collectLambdaBodies(child, expressionScope)
			}
		}
	}
	collectCommands = func(commands []syntax.Command, fallback *Scope) {
		for index := range commands {
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
			appendExpression := func(expression *syntax.Expression) {
				if expression != nil {
					expressions = append(expressions, scopedExpression{expression: expression, scope: scope, dialect: command.Dialect})
					collectLambdaBodies(expression, scope)
				}
			}
			for _, expression := range command.Expressions {
				appendExpression(expression)
			}
			for _, expression := range command.Targets {
				appendExpression(expression)
			}
			if command.Mapping != nil {
				appendExpression(command.Mapping.RHSExpression)
			}
			if command.Declaration != nil {
				appendExpression(command.Declaration.Initializer)
			}
			if command.For != nil {
				appendExpression(command.For.Iterable)
			}
			if command.Import != nil {
				appendExpression(command.Import.Path)
			}
			for _, value := range command.EnumValues {
				appendExpression(value.Initializer)
				for _, argument := range value.Arguments {
					appendExpression(argument)
				}
			}
			if command.Function != nil {
				for _, parameter := range command.Function.Parameters {
					appendExpression(parameter.Default)
				}
			}
			if command.Embedded != nil {
				collectCommands(command.Embedded.Commands, scope)
			}
		}
	}
	collectCommands(file.Commands, result.Root)

	var invalidateAssignments func(*syntax.Expression, *Scope)
	invalidateAssignments = func(expression *syntax.Expression, scope *Scope) {
		if expression == nil || scope == nil {
			return
		}
		if expression.Kind == syntax.ExpressionAssignment && len(expression.Children) > 0 {
			var invalidateTarget func(*syntax.Expression)
			invalidateTarget = func(target *syntax.Expression) {
				if target == nil {
					return
				}
				switch target.Kind {
				case syntax.ExpressionIdentifier:
					delete(candidates, resolve(scope, target.Value, target.Span.Start, false, nil))
				case syntax.ExpressionList, syntax.ExpressionTuple, syntax.ExpressionParenthesized:
					for _, child := range target.Children {
						invalidateTarget(child)
					}
				}
			}
			invalidateTarget(expression.Children[0])
		}
		expressionScope := scope
		if expression.Kind == syntax.ExpressionLambda {
			if lambdaScope := result.lambdaScopes[expression]; lambdaScope != nil {
				expressionScope = lambdaScope
			}
		}
		for index, child := range expression.Children {
			if expression.Kind != syntax.ExpressionLambda || index >= len(expression.Parameters) {
				invalidateAssignments(child, expressionScope)
			}
		}
	}
	for _, item := range expressions {
		invalidateAssignments(item.expression, item.scope)
	}

	seenExpressions := make(map[*syntax.Expression]bool)
	var appendDiagnostics func(*syntax.Expression, *Scope, syntax.Dialect)
	appendDiagnostics = func(expression *syntax.Expression, scope *Scope, dialect syntax.Dialect) {
		if expression == nil || scope == nil || seenExpressions[expression] {
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
					null = candidates[resolve(scope, receiver.Value, receiver.Span.Start, false, nil)]
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
		}
		for index, child := range expression.Children {
			if expression.Kind != syntax.ExpressionLambda || index >= len(expression.Parameters) {
				appendDiagnostics(child, expressionScope, dialect)
			}
		}
	}
	for _, item := range expressions {
		appendDiagnostics(item.expression, item.scope, item.dialect)
	}
}

func completedAggregateMethodSpan(file *syntax.File, aggregate, method *syntax.Command) (syntax.Span, bool) {
	if file == nil || aggregate == nil || aggregate.Aggregate == nil || method == nil || method.Function == nil {
		return syntax.Span{}, false
	}
	if method.Block >= 0 && method.Block < len(file.Blocks) && file.Blocks[method.Block].Kind == syntax.BlockDef && file.Blocks[method.Block].End >= 0 {
		if syntaxDiagnosticOverlaps(file.Diagnostics, file.Blocks[method.Block].Span) {
			return syntax.Span{}, false
		}
		return aggregateEndSpan(file, method), true
	}
	if aggregate.Aggregate.Kind == syntax.BlockInterface || commandHasModifier(method, "abstract") {
		if syntaxDiagnosticOverlaps(file.Diagnostics, method.Span) {
			return syntax.Span{}, false
		}
		return method.Function.Name, true
	}
	return syntax.Span{}, false
}

func aggregateHasDuplicateMethodDiagnostic(result *FileAnalysis, aggregate *syntax.Command) bool {
	if result == nil || result.File == nil || aggregate == nil || aggregate.Block < 0 || aggregate.Block >= len(result.File.Blocks) {
		return false
	}
	span := result.File.Blocks[aggregate.Block].Span
	return slices.ContainsFunc(result.Diagnostics, func(diagnostic syntax.Diagnostic) bool {
		return diagnostic.Code == "vim/E1355" && diagnostic.Span.Start >= span.Start && diagnostic.Span.End <= span.End
	})
}

func collectDuplicateClassVariableDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	classes := localAggregates(file, syntax.BlockClass)
	for index := range file.Commands {
		aggregate := &file.Commands[index]
		if aggregate.Dialect != syntax.Vim9 || aggregate.Aggregate == nil ||
			aggregate.Aggregate.Kind != syntax.BlockClass && aggregate.Aggregate.Kind != syntax.BlockInterface && aggregate.Aggregate.Kind != syntax.BlockEnum {
			continue
		}
		seen := make(map[string]bool)
		if aggregate.Aggregate.Kind == syntax.BlockEnum {
			seen["name"] = true
			seen["ordinal"] = true
		}
		for _, memberIndex := range aggregate.Aggregate.Members {
			member, ok := classVariableAt(file, memberIndex)
			if !ok {
				continue
			}
			name := file.Text(member.name)
			if seen[name] {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1369", Message: "Duplicate variable: " + name, Span: member.name,
				})
				continue
			}
			seen[name] = true
			if aggregate.Aggregate.Kind != syntax.BlockClass || member.static {
				continue
			}
			visited := map[*syntax.Command]bool{aggregate: true}
			reported := false
			for parent := extendedClass(file, classes, aggregate); parent != nil && !visited[parent]; parent = extendedClass(file, classes, parent) {
				visited[parent] = true
				for _, parentMemberIndex := range parent.Aggregate.Members {
					parentMember, ok := classVariableAt(file, parentMemberIndex)
					if ok && !parentMember.static && file.Text(parentMember.name) == name {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E1369", Message: "Duplicate variable: " + name, Span: aggregateEndSpan(file, aggregate),
						})
						reported = true
						break
					}
				}
				if reported {
					break
				}
			}
		}
	}
}

type classVariableMember struct {
	base      string
	name      syntax.Span
	protected bool
	static    bool
}

func classVariableAt(file *syntax.File, index int) (classVariableMember, bool) {
	if file == nil || index < 0 || index >= len(file.Commands) {
		return classVariableMember{}, false
	}
	command := &file.Commands[index]
	if command.Declaration == nil || command.Canonical != "var" && command.Canonical != "final" && command.Canonical != "const" {
		return classVariableMember{}, false
	}
	name := file.Text(command.Declaration.Name)
	protected := strings.HasPrefix(name, "_")
	base := strings.TrimPrefix(name, "_")
	if base == "" {
		return classVariableMember{}, false
	}
	return classVariableMember{base: base, name: command.Declaration.Name, protected: protected, static: commandHasModifier(command, "static")}, true
}

func appendPublicProtectedMemberNameDiagnostic(result *FileAnalysis, member classVariableMember) {
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1406", Message: "Public and protected member have the same name: " + member.base + " and _" + member.base, Span: member.name,
	})
}

func collectDuplicateEnumValueDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	declarations := append([]*Declaration(nil), result.Declarations...)
	sort.SliceStable(declarations, func(i, j int) bool { return declarations[i].Span.Start < declarations[j].Span.Start })
	seen := make(map[*Scope]map[string]bool)
	reported := make(map[*Scope]bool)
	for _, declaration := range declarations {
		if declaration == nil || declaration.Kind != SymbolKindEnumMember || reported[declaration.Scope] || !vim9EnumScope(result.File, declaration.Scope) {
			continue
		}
		names := seen[declaration.Scope]
		if names == nil {
			names = make(map[string]bool)
			seen[declaration.Scope] = names
		}
		if names[declaration.Name] {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1428", Message: "Duplicate enum value: " + declaration.Name, Span: declaration.Span,
			})
			reported[declaration.Scope] = true
			continue
		}
		names[declaration.Name] = true
	}
}

func vim9EnumScope(file *syntax.File, scope *Scope) bool {
	_, ok := enclosingVim9EnumName(file, scope)
	return ok && scope.Kind == syntax.BlockEnum
}

func enclosingVim9EnumName(file *syntax.File, scope *Scope) (string, bool) {
	for current := scope; file != nil && current != nil; current = current.Parent {
		if current.Kind != syntax.BlockEnum || current.Block < 0 {
			continue
		}
		commands, blocks := file.Commands, file.Blocks
		if current.CommandList != nil {
			commands, blocks = current.CommandList.Commands, current.CommandList.Blocks
		}
		if current.Block >= len(blocks) {
			return "", false
		}
		header := blocks[current.Block].Header
		if header < 0 || header >= len(commands) || commands[header].Dialect != syntax.Vim9 || commands[header].Aggregate == nil {
			return "", false
		}
		return file.Text(commands[header].Aggregate.Name), true
	}
	return "", false
}

func enumAssignmentTarget(result *FileAnalysis, scope *Scope, target *syntax.Expression) (string, string, bool) {
	if result == nil || result.File == nil || scope == nil || target == nil || target.Kind != syntax.ExpressionMember || len(target.Children) != 1 || target.Children[0] == nil {
		return "", "", false
	}
	file := result.File
	receiver := target.Children[0]
	if receiver.Kind == syntax.ExpressionIdentifier {
		declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil)
		if declaration == nil || declaration.Kind != SymbolKindEnum {
			return "", "", false
		}
		enum := localEnum(file, receiver.Value)
		if enum != nil && enumHasValue(file, enum, target.Value) {
			return receiver.Value, target.Value, true
		}
		return "", "", false
	}
	if receiver.Kind != syntax.ExpressionMember || len(receiver.Children) != 1 || receiver.Children[0] == nil || receiver.Children[0].Kind != syntax.ExpressionIdentifier {
		return "", "", false
	}
	enumName := receiver.Children[0].Value
	declaration := resolve(scope, enumName, receiver.Children[0].Span.Start, false, nil)
	if declaration == nil || declaration.Kind != SymbolKindEnum {
		return "", "", false
	}
	enum := localEnum(file, enumName)
	if enum == nil || !enumHasValue(file, enum, receiver.Value) || !enumHasObjectMember(file, enum, target.Value) {
		return "", "", false
	}
	return enumName, target.Value, true
}

func localEnum(file *syntax.File, name string) *syntax.Command {
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Dialect == syntax.Vim9 && command.Aggregate != nil && command.Aggregate.Kind == syntax.BlockEnum && file.Text(command.Aggregate.Name) == name {
			return command
		}
	}
	return nil
}

func enumHasValue(file *syntax.File, enum *syntax.Command, name string) bool {
	for _, memberIndex := range enum.Aggregate.Members {
		if memberIndex < 0 || memberIndex >= len(file.Commands) {
			continue
		}
		for _, value := range file.Commands[memberIndex].EnumValues {
			if file.Text(value.Name) == name {
				return true
			}
		}
	}
	return false
}

func enumHasObjectMember(file *syntax.File, enum *syntax.Command, name string) bool {
	if name == "name" || name == "ordinal" {
		return true
	}
	for _, memberIndex := range enum.Aggregate.Members {
		if memberIndex < 0 || memberIndex >= len(file.Commands) {
			continue
		}
		declaration := file.Commands[memberIndex].Declaration
		if declaration == nil {
			continue
		}
		for _, binding := range declaration.Bindings {
			if file.Text(binding.Name) == name {
				return true
			}
		}
	}
	return false
}

func enumHasClassSelector(file *syntax.File, enum *syntax.Command, name string) bool {
	if name == "values" || enumHasValue(file, enum, name) || enumHasObjectMember(file, enum, name) {
		return true
	}
	for _, memberIndex := range enum.Aggregate.Members {
		if memberIndex < 0 || memberIndex >= len(file.Commands) {
			continue
		}
		function := file.Commands[memberIndex].Function
		if function != nil && file.Text(function.Name) == name {
			return true
		}
	}
	return false
}

func appendMissingEnumValueDiagnostic(result *FileAnalysis, scope *Scope, expression *syntax.Expression) {
	if result == nil || result.File == nil || scope == nil || expression == nil || expression.Kind != syntax.ExpressionMember ||
		len(expression.Children) != 1 || expression.Children[0] == nil || expression.Children[0].Kind != syntax.ExpressionIdentifier || expression.Value == "" {
		return
	}
	receiver := expression.Children[0]
	declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil)
	if declaration == nil || declaration.Kind != SymbolKindEnum {
		return
	}
	enum := localEnum(result.File, receiver.Value)
	if enum == nil || enumHasClassSelector(result.File, enum, expression.Value) {
		return
	}
	span := syntax.Span{Start: expression.Span.End - len(expression.Value), End: expression.Span.End}
	if !validNameSpan(result.File, span) {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1422", Message: "Enum value \"" + expression.Value + "\" not found in enum \"" + receiver.Value + "\"", Span: span,
	})
}

func appendEnumAsValueDiagnostic(result *FileAnalysis, scope *Scope, expression *syntax.Expression, dialect syntax.Dialect) {
	if result == nil || expression == nil || dialect != syntax.Vim9 || expression.Kind != syntax.ExpressionIdentifier || result.enumValueExempt[expression.Span] {
		return
	}
	declaration := resolve(scope, expression.Value, expression.Span.Start, false, nil)
	if declaration == nil || declaration.Kind != SymbolKindEnum {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1421", Message: "Enum \"" + expression.Value + "\" cannot be used as a value", Span: expression.Span,
	})
}

func collectGenericMethodOverrideDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	classes := localAggregates(file, syntax.BlockClass)
	for index := range file.Commands {
		class := &file.Commands[index]
		if class.Dialect != syntax.Vim9 || class.Aggregate == nil || class.Aggregate.Kind != syntax.BlockClass || len(class.Aggregate.Extends) == 0 ||
			aggregateHasDuplicateMethodDiagnostic(result, class) {
			continue
		}
		super := classes[file.Text(class.Aggregate.Extends[0])]
		for _, memberIndex := range class.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			member := &file.Commands[memberIndex]
			method := member.Function
			if method == nil || commandHasModifier(member, "static") {
				continue
			}
			name := file.Text(method.Name)
			seen := make(map[*syntax.Command]bool)
			for current := super; current != nil; current = extendedClass(file, classes, current) {
				if seen[current] {
					break
				}
				seen[current] = true
				inherited := aggregateMethod(file, current, name)
				if inherited == nil {
					continue
				}
				inheritedCount := len(inherited.Function.TypeParameters)
				childCount := len(method.TypeParameters)
				if inheritedCount > 0 && childCount == 0 {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code:    "vim/E1432",
						Message: "Overriding generic method \"" + name + "\" in class \"" + file.Text(current.Aggregate.Name) + "\" with a concrete method",
						Span:    aggregateEndSpan(file, class),
					})
				} else if inheritedCount == 0 && childCount > 0 {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code:    "vim/E1433",
						Message: "Overriding concrete method \"" + name + "\" in class \"" + file.Text(current.Aggregate.Name) + "\" with a generic method",
						Span:    aggregateEndSpan(file, class),
					})
				} else if inheritedCount > 0 && inheritedCount != childCount {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code:    "vim/E1434",
						Message: "Mismatched number of type variables for generic method  \"" + name + "\" in class \"" + file.Text(current.Aggregate.Name) + "\"",
						Span:    aggregateEndSpan(file, class),
					})
				}
				break
			}
		}
	}
}

func collectUnimplementedAbstractMethodDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	for index := range file.Commands {
		class := &file.Commands[index]
		if class.Dialect != syntax.Vim9 || class.Aggregate == nil || class.Aggregate.Kind != syntax.BlockClass || commandHasModifier(class, "abstract") ||
			aggregateHasDuplicateMethodDiagnostic(result, class) {
			continue
		}
		parent := extendedClass(file, result.classes, class)
		if parent == nil || !commandHasModifier(parent, "abstract") {
			continue
		}
		seenNames := make(map[string]bool)
		invalidClassMethod := false
		for _, memberIndex := range class.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			member := &file.Commands[memberIndex]
			if commandHasModifier(member, "abstract") || member.Function != nil && commandHasModifier(member, "public") {
				invalidClassMethod = true
				break
			}
			if member.Function != nil && !commandHasModifier(member, "static") && !commandHasModifier(member, "public") {
				seenNames[strings.TrimPrefix(file.Text(member.Function.Name), "_")] = true
			}
		}
		if invalidClassMethod {
			continue
		}
		visited := make(map[*syntax.Command]bool)
		reported := false
		for current := parent; current != nil && !visited[current] && !reported; current = extendedClass(file, result.classes, current) {
			visited[current] = true
			for _, memberIndex := range current.Aggregate.Members {
				if memberIndex < 0 || memberIndex >= len(file.Commands) {
					continue
				}
				member := &file.Commands[memberIndex]
				if member.Function == nil || commandHasModifier(member, "static") || commandHasModifier(member, "public") {
					continue
				}
				name := file.Text(member.Function.Name)
				base := strings.TrimPrefix(name, "_")
				if seenNames[base] {
					continue
				}
				seenNames[base] = true
				if commandHasModifier(member, "abstract") {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E1373", Message: `Abstract method "` + name + `" is not implemented`, Span: aggregateEndSpan(file, class),
					})
					reported = true
					break
				}
			}
		}
	}
}

func collectMethodAccessLevelDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	for index := range file.Commands {
		class := &file.Commands[index]
		if class.Dialect != syntax.Vim9 || class.Aggregate == nil || class.Aggregate.Kind != syntax.BlockClass || aggregateHasDuplicateMethodDiagnostic(result, class) {
			continue
		}
		for _, memberIndex := range class.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			member := &file.Commands[memberIndex]
			if member.Function == nil || commandHasModifier(member, "static") || commandHasModifier(member, "public") {
				continue
			}
			name := file.Text(member.Function.Name)
			base := strings.TrimPrefix(name, "_")
			protected := strings.HasPrefix(name, "_")
			visited := make(map[*syntax.Command]bool)
			reported := false
			for parent := extendedClass(file, result.classes, class); parent != nil && !visited[parent]; parent = extendedClass(file, result.classes, parent) {
				visited[parent] = true
				for _, parentMemberIndex := range parent.Aggregate.Members {
					if parentMemberIndex < 0 || parentMemberIndex >= len(file.Commands) {
						continue
					}
					parentMember := &file.Commands[parentMemberIndex]
					if parentMember.Function == nil || commandHasModifier(parentMember, "static") || commandHasModifier(parentMember, "public") {
						continue
					}
					parentName := file.Text(parentMember.Function.Name)
					if strings.TrimPrefix(parentName, "_") == base && strings.HasPrefix(parentName, "_") != protected {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E1377", Message: `Access level of method "` + name + `" is different in class "` + file.Text(parent.Aggregate.Name) + `"`,
							Span: aggregateEndSpan(file, class),
						})
						reported = true
						break
					}
				}
				if reported {
					break
				}
			}
		}
	}
}

func collectVariableTypeMismatchDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	interfaces := localAggregates(file, syntax.BlockInterface)
	for _, iface := range interfaces {
		reported := make(map[string]bool)
		for _, memberIndex := range iface.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			member := &file.Commands[memberIndex]
			if member.Declaration == nil || commandHasModifier(member, "static") {
				continue
			}
			for bindingIndex, binding := range member.Declaration.Bindings {
				name := file.Text(binding.Name)
				actual := aggregateBindingType(result, member, bindingIndex)
				if name == "" || reported[name] || isUnknownType(actual) {
					continue
				}
				seen := make(map[*syntax.Command]bool)
				var checkParents func(*syntax.Command) bool
				checkParents = func(current *syntax.Command) bool {
					if current == nil || current.Aggregate == nil || seen[current] {
						return false
					}
					seen[current] = true
					if expected, found := aggregateObjectVariableType(result, current, name); found {
						if !memberTypesCompatible(result, expected, actual) {
							appendVariableTypeMismatchDiagnostic(result, name, expected, actual, aggregateEndSpan(file, iface))
							reported[name] = true
						}
						return true
					}
					for _, parent := range current.Aggregate.Extends {
						if checkParents(interfaces[file.Text(parent)]) {
							return true
						}
					}
					return false
				}
				for _, parent := range iface.Aggregate.Extends {
					if checkParents(interfaces[file.Text(parent)]) {
						break
					}
				}
			}
		}
	}
	for _, class := range result.classes {
		reported := make(map[string]bool)
		for _, memberIndex := range class.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			member := &file.Commands[memberIndex]
			if member.Declaration == nil {
				continue
			}
			for bindingIndex, binding := range member.Declaration.Bindings {
				if binding.ParsedType == nil {
					continue
				}
				value := initializerElement(member.Declaration.Initializer, bindingIndex, len(member.Declaration.Bindings))
				if value == nil || expressionContainsMissing(value) {
					continue
				}
				expected, actual := convertSyntaxType(binding.ParsedType), result.TypeOf(value)
				if isUnknownType(actual) || memberTypesCompatible(result, expected, actual) {
					continue
				}
				name := file.Text(binding.Name)
				appendVariableTypeMismatchDiagnostic(result, name, expected, actual, value.Span)
				reported[name] = true
			}
		}
		seenInterfaces := make(map[*syntax.Command]bool)
		var checkInterface func(*syntax.Command)
		checkInterface = func(iface *syntax.Command) {
			if iface == nil || iface.Aggregate == nil || seenInterfaces[iface] {
				return
			}
			seenInterfaces[iface] = true
			for _, memberIndex := range iface.Aggregate.Members {
				if memberIndex < 0 || memberIndex >= len(file.Commands) {
					continue
				}
				member := &file.Commands[memberIndex]
				if member.Declaration == nil || commandHasModifier(member, "static") {
					continue
				}
				for bindingIndex, binding := range member.Declaration.Bindings {
					name := file.Text(binding.Name)
					if name == "" || strings.HasPrefix(name, "_") || reported[name] {
						continue
					}
					implementation, _, found := classObjectVariableBinding(result, class, name)
					if found && commandHasModifier(implementation, "public") {
						reported[name] = true
						continue
					}
					expected := aggregateBindingType(result, member, bindingIndex)
					actual, found := classObjectVariableType(result, class, name)
					if found && !isUnknownType(expected) && !isUnknownType(actual) && !memberTypesCompatible(result, expected, actual) {
						appendVariableTypeMismatchDiagnostic(result, name, expected, actual, aggregateEndSpan(file, class))
						reported[name] = true
					}
				}
			}
			for _, parent := range iface.Aggregate.Extends {
				checkInterface(interfaces[file.Text(parent)])
			}
		}
		for _, implemented := range class.Aggregate.Implements {
			checkInterface(interfaces[file.Text(implemented)])
		}
	}
}

func collectInterfaceVariableAccessDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	interfaces := localAggregates(file, syntax.BlockInterface)
	for index := range file.Commands {
		class := &file.Commands[index]
		if class.Dialect != syntax.Vim9 || class.Aggregate == nil || class.Aggregate.Kind != syntax.BlockClass || aggregateHasDuplicateMethodDiagnostic(result, class) {
			continue
		}
		reported := make(map[string]bool)
		for _, implemented := range class.Aggregate.Implements {
			interfaceName := file.Text(implemented)
			seenInterfaces := make(map[*syntax.Command]bool)
			var checkInterface func(*syntax.Command)
			checkInterface = func(iface *syntax.Command) {
				if iface == nil || iface.Aggregate == nil || seenInterfaces[iface] {
					return
				}
				seenInterfaces[iface] = true
				for _, memberIndex := range iface.Aggregate.Members {
					if memberIndex < 0 || memberIndex >= len(file.Commands) {
						continue
					}
					member := &file.Commands[memberIndex]
					if member.Declaration == nil || commandHasModifier(member, "static") {
						continue
					}
					for _, binding := range member.Declaration.Bindings {
						name := file.Text(binding.Name)
						if name == "" || strings.HasPrefix(name, "_") || reported[name] {
							continue
						}
						implementation, _, found := classObjectVariableBinding(result, class, name)
						if found && commandHasModifier(implementation, "public") && !classHasDuplicateVariableDiagnostic(result, class, name) {
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
								Code: "vim/E1367", Message: `Access level of variable "` + name + `" of interface "` + interfaceName + `" is different`, Span: aggregateEndSpan(file, class),
							})
							reported[name] = true
						}
					}
				}
				for _, parent := range iface.Aggregate.Extends {
					checkInterface(interfaces[file.Text(parent)])
				}
			}
			checkInterface(interfaces[interfaceName])
		}
	}
}

func classHasDuplicateVariableDiagnostic(result *FileAnalysis, class *syntax.Command, name string) bool {
	if result == nil || result.File == nil || class == nil || class.Block < 0 || class.Block >= len(result.File.Blocks) {
		return false
	}
	classSpan := result.File.Blocks[class.Block].Span
	message := "Duplicate variable: " + name
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1369" && diagnostic.Message == message && diagnostic.Span.Start >= classSpan.Start && diagnostic.Span.End <= classSpan.End {
			return true
		}
	}
	return false
}

func localAggregates(file *syntax.File, kind syntax.BlockKind) map[string]*syntax.Command {
	aggregates := make(map[string]*syntax.Command)
	if file == nil {
		return aggregates
	}
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Dialect == syntax.Vim9 && command.Aggregate != nil && command.Aggregate.Kind == kind {
			aggregates[file.Text(command.Aggregate.Name)] = command
		}
	}
	return aggregates
}

func aggregateBindingType(result *FileAnalysis, command *syntax.Command, bindingIndex int) ValueType {
	if result == nil || command == nil || command.Declaration == nil || bindingIndex < 0 || bindingIndex >= len(command.Declaration.Bindings) {
		return UnknownValueType
	}
	binding := command.Declaration.Bindings[bindingIndex]
	if binding.ParsedType != nil {
		return convertSyntaxType(binding.ParsedType)
	}
	value := initializerElement(command.Declaration.Initializer, bindingIndex, len(command.Declaration.Bindings))
	return result.TypeOf(value)
}

func aggregateObjectVariableType(result *FileAnalysis, aggregate *syntax.Command, name string) (ValueType, bool) {
	if result == nil || result.File == nil || aggregate == nil || aggregate.Aggregate == nil {
		return UnknownValueType, false
	}
	file := result.File
	for _, memberIndex := range aggregate.Aggregate.Members {
		if memberIndex < 0 || memberIndex >= len(file.Commands) {
			continue
		}
		member := &file.Commands[memberIndex]
		if member.Declaration == nil || commandHasModifier(member, "static") {
			continue
		}
		for bindingIndex, binding := range member.Declaration.Bindings {
			if file.Text(binding.Name) == name {
				return aggregateBindingType(result, member, bindingIndex), true
			}
		}
	}
	return UnknownValueType, false
}

func classObjectVariableType(result *FileAnalysis, class *syntax.Command, name string) (ValueType, bool) {
	member, bindingIndex, found := classObjectVariableBinding(result, class, name)
	if !found {
		return UnknownValueType, false
	}
	return aggregateBindingType(result, member, bindingIndex), true
}

func classObjectVariableBinding(result *FileAnalysis, class *syntax.Command, name string) (*syntax.Command, int, bool) {
	if result == nil || result.File == nil {
		return nil, 0, false
	}
	_, member, bindingIndex, found := classObjectVariableOwner(result, class, name)
	return member, bindingIndex, found
}

func aggregateVariableBinding(file *syntax.File, aggregate *syntax.Command, name string, static bool) (*syntax.Command, int, bool) {
	if file == nil || aggregate == nil || aggregate.Aggregate == nil {
		return nil, 0, false
	}
	for _, memberIndex := range aggregate.Aggregate.Members {
		if memberIndex < 0 || memberIndex >= len(file.Commands) {
			continue
		}
		member := &file.Commands[memberIndex]
		if member.Declaration == nil || commandHasModifier(member, "static") != static {
			continue
		}
		for bindingIndex, binding := range member.Declaration.Bindings {
			if file.Text(binding.Name) == name {
				return member, bindingIndex, true
			}
		}
	}
	return nil, 0, false
}

func classObjectVariableOwner(result *FileAnalysis, class *syntax.Command, name string) (*syntax.Command, *syntax.Command, int, bool) {
	if result == nil || result.File == nil {
		return nil, nil, 0, false
	}
	seen := make(map[*syntax.Command]bool)
	for current := class; current != nil; current = extendedClass(result.File, result.classes, current) {
		if seen[current] {
			return nil, nil, 0, false
		}
		seen[current] = true
		if member, bindingIndex, found := aggregateVariableBinding(result.File, current, name, false); found {
			return current, member, bindingIndex, true
		}
	}
	return nil, nil, 0, false
}

func classStaticVariableOwner(result *FileAnalysis, class *syntax.Command, name string) (*syntax.Command, syntax.Span, bool) {
	if result == nil || result.File == nil {
		return nil, syntax.Span{}, false
	}
	seen := make(map[*syntax.Command]bool)
	for current := class; current != nil && !seen[current]; current = extendedClass(result.File, result.classes, current) {
		seen[current] = true
		if member, bindingIndex, found := aggregateVariableBinding(result.File, current, name, true); found {
			return current, member.Declaration.Bindings[bindingIndex].Name, true
		}
	}
	return nil, syntax.Span{}, false
}

func memberTypesCompatible(result *FileAnalysis, expected, actual ValueType) bool {
	if isUnknownType(expected) || isUnknownType(actual) {
		return true
	}
	if expected.Name == "float" && actual.Name == "number" || expected.Name == "bool" && actual.Name == "number" {
		return true
	}
	if expected.Name != actual.Name {
		if result != nil && result.classes[expected.Name] != nil && result.classes[actual.Name] != nil {
			seen := make(map[*syntax.Command]bool)
			for class := result.classes[actual.Name]; class != nil; class = extendedClass(result.File, result.classes, class) {
				if seen[class] {
					return false
				}
				seen[class] = true
				if result.File.Text(class.Aggregate.Name) == expected.Name {
					return true
				}
			}
			return false
		}
		if isASCIIUpperName(expected.Name) || isASCIIUpperName(actual.Name) {
			return true
		}
		return false
	}
	if len(expected.Arguments) != len(actual.Arguments) {
		return false
	}
	if expected.ArgumentCountKnown != actual.ArgumentCountKnown || expected.RequiredArguments != actual.RequiredArguments || expected.Variadic != actual.Variadic ||
		(expected.Return == nil) != (actual.Return == nil) {
		return false
	}
	for index := range expected.Arguments {
		if !memberTypesCompatible(result, expected.Arguments[index], actual.Arguments[index]) {
			return false
		}
	}
	if expected.Return != nil && !memberTypesCompatible(result, *expected.Return, *actual.Return) {
		return false
	}
	return true
}

func isASCIIUpperName(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

func appendVariableTypeMismatchDiagnostic(result *FileAnalysis, name string, expected, actual ValueType, span syntax.Span) {
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1382", Message: `Variable "` + name + `": type mismatch, expected ` + methodTypeDisplay(result, expected) + ` but got ` + methodTypeDisplay(result, actual), Span: span,
	})
}

func collectMethodTypeMismatchDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	interfaces := localAggregates(file, syntax.BlockInterface)
	for index := range file.Commands {
		class := &file.Commands[index]
		if class.Dialect != syntax.Vim9 || class.Aggregate == nil || class.Aggregate.Kind != syntax.BlockClass || aggregateHasDuplicateMethodDiagnostic(result, class) {
			continue
		}
		reported := make(map[string]bool)
		for _, memberIndex := range class.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			method := &file.Commands[memberIndex]
			if method.Function == nil || commandIsClassMethod(file, method) {
				continue
			}
			name := file.Text(method.Function.Name)
			seenParents := make(map[*syntax.Command]bool)
			for parent := extendedClass(file, result.classes, class); parent != nil; parent = extendedClass(file, result.classes, parent) {
				if seenParents[parent] {
					break
				}
				seenParents[parent] = true
				expected := aggregateMethod(file, parent, name)
				if expected == nil {
					continue
				}
				if methodSignaturesMismatch(expected.Function, method.Function) {
					appendMethodTypeMismatchDiagnostic(result, class, name, expected.Function, method.Function)
					reported[name] = true
				}
				break
			}
		}
		seenInterfaces := make(map[*syntax.Command]bool)
		var checkInterface func(*syntax.Command)
		checkInterface = func(iface *syntax.Command) {
			if iface == nil || iface.Aggregate == nil || seenInterfaces[iface] {
				return
			}
			seenInterfaces[iface] = true
			for _, memberIndex := range iface.Aggregate.Members {
				if memberIndex < 0 || memberIndex >= len(file.Commands) {
					continue
				}
				required := &file.Commands[memberIndex]
				if required.Function == nil {
					continue
				}
				name := file.Text(required.Function.Name)
				if reported[name] {
					continue
				}
				actual := objectMethodInClassHierarchy(file, result.classes, class, name)
				if actual != nil && methodSignaturesMismatch(required.Function, actual.Function) {
					appendMethodTypeMismatchDiagnostic(result, class, name, required.Function, actual.Function)
					reported[name] = true
				}
			}
			for _, parent := range iface.Aggregate.Extends {
				checkInterface(interfaces[file.Text(parent)])
			}
		}
		for _, implemented := range class.Aggregate.Implements {
			checkInterface(interfaces[file.Text(implemented)])
		}
		for _, memberIndex := range class.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			method := &file.Commands[memberIndex]
			if method.Function == nil || commandIsClassMethod(file, method) {
				continue
			}
			name := file.Text(method.Function.Name)
			if reported[name] {
				continue
			}
			var expected *syntax.Function
			switch name {
			case "empty":
				expected = builtinObjectMethodSignature("bool")
			case "len":
				expected = builtinObjectMethodSignature("number")
			case "string":
				expected = builtinObjectMethodSignature("string")
			}
			if expected != nil && methodSignaturesMismatch(expected, method.Function) {
				appendMethodTypeMismatchDiagnostic(result, class, name, expected, method.Function)
			}
		}
	}
}

func objectMethodInClassHierarchy(file *syntax.File, classes map[string]*syntax.Command, class *syntax.Command, name string) *syntax.Command {
	seen := make(map[*syntax.Command]bool)
	for current := class; current != nil; current = extendedClass(file, classes, current) {
		if seen[current] {
			return nil
		}
		seen[current] = true
		if method := aggregateMethod(file, current, name); method != nil {
			return method
		}
	}
	return nil
}

func methodSignaturesMismatch(expected, actual *syntax.Function) bool {
	if expected == nil || actual == nil || len(expected.TypeParameters) > 0 || len(actual.TypeParameters) > 0 {
		return false
	}
	if len(expected.Parameters) != len(actual.Parameters) || requiredParameterCount(expected.Parameters) != requiredParameterCount(actual.Parameters) ||
		parametersAreVariadic(expected.Parameters) != parametersAreVariadic(actual.Parameters) {
		return true
	}
	for index := range expected.Parameters {
		if !sameMethodType(convertSyntaxType(expected.Parameters[index].Type), convertSyntaxType(actual.Parameters[index].Type)) {
			return true
		}
	}
	expectedReturn, actualReturn := ValueType{Name: "void"}, ValueType{Name: "void"}
	if expected.ReturnType != nil {
		expectedReturn = convertSyntaxType(expected.ReturnType)
	}
	if actual.ReturnType != nil {
		actualReturn = convertSyntaxType(actual.ReturnType)
	}
	return !sameMethodType(expectedReturn, actualReturn)
}

func sameMethodType(expected, actual ValueType) bool {
	if expected.Name != actual.Name || len(expected.Arguments) != len(actual.Arguments) || expected.ArgumentCountKnown != actual.ArgumentCountKnown ||
		expected.RequiredArguments != actual.RequiredArguments || expected.Variadic != actual.Variadic || (expected.Return == nil) != (actual.Return == nil) {
		return false
	}
	for index := range expected.Arguments {
		if !sameMethodType(expected.Arguments[index], actual.Arguments[index]) {
			return false
		}
	}
	if expected.Return != nil && !sameMethodType(*expected.Return, *actual.Return) {
		return false
	}
	return true
}

func methodSignatureDisplay(result *FileAnalysis, function *syntax.Function) string {
	arguments := make([]string, 0, len(function.Parameters))
	for _, parameter := range function.Parameters {
		argument := methodTypeDisplay(result, convertSyntaxType(parameter.Type))
		if parameter.Variadic {
			argument = "..." + argument
		} else if parameter.Default != nil {
			argument = "?" + argument
		}
		arguments = append(arguments, argument)
	}
	signature := "func(" + strings.Join(arguments, ", ") + ")"
	if function.ReturnType != nil {
		signature += ": " + methodTypeDisplay(result, convertSyntaxType(function.ReturnType))
	}
	return signature
}

func methodTypeDisplay(result *FileAnalysis, typ ValueType) string {
	if typ.Name == "func" && typ.ArgumentCountKnown {
		arguments := make([]string, 0, len(typ.Arguments))
		for index, argumentType := range typ.Arguments {
			argument := methodTypeDisplay(result, argumentType)
			if typ.Variadic && index == len(typ.Arguments)-1 {
				argument = "..." + argument
			} else if index >= typ.RequiredArguments {
				argument = "?" + argument
			}
			arguments = append(arguments, argument)
		}
		display := "func(" + strings.Join(arguments, ", ") + ")"
		if typ.Return != nil && typ.Return.Name != "void" {
			display += ": " + methodTypeDisplay(result, *typ.Return)
		}
		return display
	}
	if result != nil && result.classes[typ.Name] != nil {
		return "object<" + typ.Name + ">"
	}
	if len(typ.Arguments) == 0 {
		return typ.Name
	}
	arguments := make([]string, 0, len(typ.Arguments))
	for _, argument := range typ.Arguments {
		if typ.Name == "object" || typ.Name == "class" {
			arguments = append(arguments, argument.Name)
		} else {
			arguments = append(arguments, methodTypeDisplay(result, argument))
		}
	}
	return typ.Name + "<" + strings.Join(arguments, ", ") + ">"
}

func appendMethodTypeMismatchDiagnostic(result *FileAnalysis, class *syntax.Command, name string, expected, actual *syntax.Function) {
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1383", Message: `Method "` + name + `": type mismatch, expected ` + methodSignatureDisplay(result, expected) + ` but got ` + methodSignatureDisplay(result, actual),
		Span: aggregateEndSpan(result.File, class),
	})
}

func builtinObjectMethodSignature(returnType string) *syntax.Function {
	return &syntax.Function{ReturnType: &syntax.Type{Kind: syntax.TypeNamed, Name: returnType}}
}

func extendedClass(file *syntax.File, classes map[string]*syntax.Command, class *syntax.Command) *syntax.Command {
	if file == nil || class == nil || class.Aggregate == nil || len(class.Aggregate.Extends) == 0 {
		return nil
	}
	return classes[file.Text(class.Aggregate.Extends[0])]
}

func aggregateMethod(file *syntax.File, class *syntax.Command, name string) *syntax.Command {
	if file == nil || class == nil || class.Aggregate == nil {
		return nil
	}
	for _, memberIndex := range class.Aggregate.Members {
		if memberIndex < 0 || memberIndex >= len(file.Commands) {
			continue
		}
		member := &file.Commands[memberIndex]
		method := member.Function
		if method != nil && !commandHasModifier(member, "static") && file.Text(method.Name) == name {
			return member
		}
	}
	return nil
}

func commandHasModifier(command *syntax.Command, name string) bool {
	if command == nil {
		return false
	}
	for _, modifier := range command.Modifiers {
		if modifier.Name == name {
			return true
		}
	}
	return false
}

func commandIsClassMethod(file *syntax.File, command *syntax.Command) bool {
	if file == nil || command == nil || command.Function == nil {
		return false
	}
	name := file.Text(command.Function.Name)
	return commandHasModifier(command, "static") || strings.HasPrefix(name, "new") || strings.HasPrefix(name, "_new")
}

func aggregateEndSpan(file *syntax.File, command *syntax.Command) syntax.Span {
	if file != nil && command != nil && command.Block >= 0 && command.Block < len(file.Blocks) {
		end := file.Blocks[command.Block].End
		if end >= 0 && end < len(file.Commands) {
			return file.Commands[end].Name
		}
	}
	if command != nil {
		return command.Name
	}
	return syntax.Span{}
}

// CombinedDiagnostics returns parser and semantic diagnostics with the narrow
// semantic replacements that require resolved names. It does not mutate file.
func CombinedDiagnostics(file *syntax.File, result *FileAnalysis) []syntax.Diagnostic {
	if file == nil {
		return nil
	}
	diagnostics := make([]syntax.Diagnostic, 0, len(file.Diagnostics))
	for _, diagnostic := range file.Diagnostics {
		if result != nil && result.suppressedSyntaxDiagnostics[diagnostic] {
			continue
		}
		diagnostics = append(diagnostics, diagnostic)
	}
	if result != nil {
		diagnostics = append(diagnostics, result.Diagnostics...)
	}
	return diagnostics
}

func collectImportNamespaceDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	source := result.File.Source
	seen := make(map[syntax.Span]bool)
	for _, reference := range result.References {
		if reference == nil || reference.dialect != syntax.Vim9 || reference.Declaration == nil || reference.Declaration.Kind != SymbolKindImport || seen[reference.Span] {
			continue
		}
		seen[reference.Span] = true
		dot := reference.Span.End
		if scopeUsesDefTypeRules(reference.scope) {
			for dot < len(source) && (source[dot] == ' ' || source[dot] == '\t') {
				dot++
			}
		}
		if dot < len(source) && source[dot] == '.' && (dot+1 >= len(source) || source[dot+1] != '.') {
			if diagnostic, ok := importMemberWhitespaceSyntaxDiagnostic(result.File, dot); ok {
				result.suppressedSyntaxDiagnostics[diagnostic] = true
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1074", Message: "No white space allowed after dot", Span: importMemberWhitespaceSpan(source, dot),
				})
				continue
			}
			memberStart := dot + 1
			for memberStart < len(source) && (source[memberStart] == ' ' || source[memberStart] == '\t') {
				memberStart++
			}
			if reference.assignmentTarget && scopeUsesDefTypeRules(reference.scope) &&
				(memberStart >= len(source) || !(source[memberStart] >= 'a' && source[memberStart] <= 'z' || source[memberStart] >= 'A' && source[memberStart] <= 'Z' || source[memberStart] == '_' || source[memberStart] >= utf8.RuneSelf)) {
				for _, diagnostic := range result.File.Diagnostics {
					if diagnostic.Code == "vimls/missing-member" && diagnostic.Span.Start >= dot && diagnostic.Span.Start <= memberStart {
						result.suppressedSyntaxDiagnostics[diagnostic] = true
					}
				}
				end := reference.Span.End
				for end < len(source) && source[end] != '\n' && source[end] != '\r' {
					end++
				}
				tail := strings.TrimRight(source[reference.Span.Start:end], " \t")
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1259", Message: "Missing name after imported name: " + tail, Span: reference.Span})
			}
			continue
		}
		// Script evaluation does not skip white space before the namespace dot,
		// so E1060 wins over a provisional generic member-spacing diagnostic.
		spacedDot := reference.Span.End
		for spacedDot < len(source) && (source[spacedDot] == ' ' || source[spacedDot] == '\t') {
			spacedDot++
		}
		if spacedDot > reference.Span.End && spacedDot < len(source) && source[spacedDot] == '.' {
			if diagnostic, ok := importMemberWhitespaceSyntaxDiagnostic(result.File, spacedDot); ok {
				result.suppressedSyntaxDiagnostics[diagnostic] = true
			}
		}
		end := reference.Span.End
		for end < len(source) && source[end] != '\n' && source[end] != '\r' {
			end++
		}
		after := strings.TrimLeft(source[reference.Span.End:end], " \t")
		plainAssignment := strings.HasPrefix(after, "=") && !strings.HasPrefix(after, "==") && !strings.HasPrefix(after, "=~")
		compoundAssignment := false
		for _, operator := range []string{"+=", "-=", "*=", "/=", "%=", "..="} {
			if strings.HasPrefix(after, operator) {
				compoundAssignment = true
				break
			}
		}
		if scopeUsesDefTypeRules(reference.scope) && (plainAssignment || compoundAssignment) {
			tail := strings.TrimRight(source[reference.Span.Start:end], " \t")
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1258", Message: "No '.' after imported name: " + tail, Span: reference.Span,
			})
			continue
		}
		if plainAssignment {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1236", Message: "Cannot use " + reference.Name + " itself, it is imported", Span: reference.Span,
			})
			continue
		}
		if reference.functionCallee {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1236", Message: "Cannot use " + reference.Name + " itself, it is imported", Span: reference.Span,
			})
			continue
		}
		tail := strings.TrimRight(source[reference.Span.Start:end], " \t")
		for _, operator := range []string{"+=", "-=", "*=", "/=", "%=", "..="} {
			if strings.HasPrefix(after, operator) {
				tail = reference.Name
				break
			}
		}
		if tail == "" {
			tail = reference.Name
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E1060", Message: "Expected dot after name: " + tail, Span: reference.Span,
		})
	}
}

func importMemberWhitespaceSyntaxDiagnostic(file *syntax.File, dot int) (syntax.Diagnostic, bool) {
	if file == nil || dot < 0 || dot+1 >= len(file.Source) || file.Source[dot] != '.' || !strings.ContainsRune(" \t\r\n", rune(file.Source[dot+1])) {
		return syntax.Diagnostic{}, false
	}
	afterDot := dot + 1
	for _, diagnostic := range file.Diagnostics {
		switch diagnostic.Code {
		case "vim/E15", "vim/E116", "vim/E1202", "vim/E1127", "vimls/missing-member":
			if diagnostic.Span.Start == afterDot && diagnostic.Span.End >= afterDot &&
				(diagnostic.Span.End > afterDot && strings.Trim(file.Text(diagnostic.Span), " \t\r\n") == "" ||
					diagnostic.Span.End == afterDot && continuedImportMember(file.Source, afterDot)) {
				return diagnostic, true
			}
		case "vim/E488", "vimls/trailing-expression":
			if diagnostic.Span.Start == dot && diagnostic.Span.End == dot+1 {
				return diagnostic, true
			}
		}
	}
	return syntax.Diagnostic{}, false
}

func importMemberWhitespaceSpan(source string, dot int) syntax.Span {
	span := syntax.Span{Start: dot + 1, End: dot + 1}
	for span.End < len(source) && strings.ContainsRune(" \t\r\n", rune(source[span.End])) {
		span.End++
	}
	return span
}

func continuedImportMember(source string, start int) bool {
	if start >= len(source) || source[start] != '\r' && source[start] != '\n' {
		return false
	}
	if source[start] == '\r' && start+1 < len(source) && source[start+1] == '\n' {
		start++
	}
	start++
	indented := false
	for start < len(source) && (source[start] == ' ' || source[start] == '\t') {
		indented = true
		start++
	}
	return indented && start < len(source) && (source[start] == '_' || source[start] >= 'A' && source[start] <= 'Z' || source[start] >= 'a' && source[start] <= 'z')
}

func collectMissingReturnValueDiagnostics(result *FileAnalysis, commands []syntax.Command, blocks []syntax.Block) {
	if result == nil || result.File == nil {
		return
	}
	seenLambdas := make(map[*syntax.Expression]bool)
	var walkCommands func([]syntax.Command, []syntax.Block, []bool, []bool)
	var walkExpression func(*syntax.Expression, []bool, []bool)
	walkExpression = func(expression *syntax.Expression, functionNeedsValue, functionRejectsValue []bool) {
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
			walkCommands(body.Commands, body.Blocks, append(functionNeedsValue, needsValue), append(functionRejectsValue, false))
		}
		for _, child := range expression.Children {
			walkExpression(child, functionNeedsValue, functionRejectsValue)
		}
	}
	walkCommands = func(commands []syntax.Command, blocks []syntax.Block, functionNeedsValue, functionRejectsValue []bool) {
		for index := range commands {
			command := &commands[index]
			switch command.Canonical {
			case "def":
				needsValue := command.Function != nil && returnTypeNeedsValue(command.Function.ReturnType)
				functionNeedsValue = append(functionNeedsValue, needsValue)
				functionScope := result.commandScopes[command]
				declarationScope := functionScope
				if functionScope != nil && (functionScope.Kind == syntax.BlockFunction || functionScope.Kind == syntax.BlockDef) && functionScope.Parent != nil {
					declarationScope = functionScope.Parent
				}
				constructorLike := declarationScope != nil && (declarationScope.Kind == syntax.BlockClass || declarationScope.Kind == syntax.BlockInterface || declarationScope.Kind == syntax.BlockEnum) && command.Function != nil &&
					(strings.HasPrefix(result.File.Text(command.Function.Name), "new") || strings.HasPrefix(result.File.Text(command.Function.Name), "_new"))
				rejectsValue := command.Function != nil && (command.Function.ReturnType == nil || command.Function.ReturnType.Kind != syntax.TypeMissing && command.Function.ReturnType.Name == "void") &&
					!constructorLike
				functionRejectsValue = append(functionRejectsValue, rejectsValue)
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
				functionRejectsValue = append(functionRejectsValue, false)
			}
			if command.Canonical == "return" && len(command.Expressions) == 0 && len(functionNeedsValue) > 0 && functionNeedsValue[len(functionNeedsValue)-1] {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1003", Message: "Missing return value", Span: command.Name,
				})
			} else if command.Canonical == "return" && len(command.Expressions) > 0 && len(functionRejectsValue) > 0 && functionRejectsValue[len(functionRejectsValue)-1] {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1096", Message: "Returning a value in a function without a return type", Span: command.Name,
				})
			}
			for _, expression := range command.Expressions {
				walkExpression(expression, functionNeedsValue, functionRejectsValue)
			}
			for _, expression := range command.Targets {
				walkExpression(expression, functionNeedsValue, functionRejectsValue)
			}
			if command.Mapping != nil {
				walkExpression(command.Mapping.RHSExpression, functionNeedsValue, functionRejectsValue)
			}
			if command.Declaration != nil {
				walkExpression(command.Declaration.Initializer, functionNeedsValue, functionRejectsValue)
			}
			if command.For != nil {
				walkExpression(command.For.Iterable, functionNeedsValue, functionRejectsValue)
			}
			if command.Import != nil {
				walkExpression(command.Import.Path, functionNeedsValue, functionRejectsValue)
			}
			for _, value := range command.EnumValues {
				walkExpression(value.Initializer, functionNeedsValue, functionRejectsValue)
				for _, argument := range value.Arguments {
					walkExpression(argument, functionNeedsValue, functionRejectsValue)
				}
			}
			if command.Function != nil {
				for _, parameter := range command.Function.Parameters {
					walkExpression(parameter.Default, functionNeedsValue, functionRejectsValue)
				}
			}
			if command.Embedded != nil {
				walkCommands(command.Embedded.Commands, command.Embedded.Blocks, functionNeedsValue, functionRejectsValue)
			}
			if (command.Canonical == "enddef" || command.Canonical == "endfunction") && len(functionNeedsValue) > 0 {
				functionNeedsValue = functionNeedsValue[:len(functionNeedsValue)-1]
				functionRejectsValue = functionRejectsValue[:len(functionRejectsValue)-1]
			}
		}
	}
	walkCommands(commands, blocks, nil, nil)
}

func returnTypeNeedsValue(returnType *syntax.Type) bool {
	return returnType != nil && returnType.Kind != syntax.TypeMissing && returnType.Name != "void"
}

func collectUnreachableCodeDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	seenLambdas := make(map[*syntax.Expression]bool)
	var walkSequence func([]syntax.Command, []syntax.Block, int, int, bool)
	var walkExpression func(*syntax.Expression, bool)
	var walkCommandExpressions func(*syntax.Command, bool)
	var walkBlockBodies func([]syntax.Command, []syntax.Block, syntax.Block, bool)
	walkExpression = func(expression *syntax.Expression, compiled bool) {
		if expression == nil {
			return
		}
		if expression.Kind == syntax.ExpressionLambda && expression.LambdaBody != nil && !seenLambdas[expression] {
			seenLambdas[expression] = true
			walkSequence(expression.LambdaBody.Commands, expression.LambdaBody.Blocks, 0, len(expression.LambdaBody.Commands), compiled)
		}
		for _, child := range expression.Children {
			walkExpression(child, compiled)
		}
	}
	walkCommandExpressions = func(command *syntax.Command, compiled bool) {
		if command == nil {
			return
		}
		for _, expression := range command.Expressions {
			walkExpression(expression, compiled && command.Dialect == syntax.Vim9)
		}
		for _, expression := range command.Targets {
			walkExpression(expression, compiled && command.Dialect == syntax.Vim9)
		}
		if command.Declaration != nil {
			walkExpression(command.Declaration.Initializer, compiled && command.Dialect == syntax.Vim9)
		}
		if command.Mapping != nil {
			walkExpression(command.Mapping.RHSExpression, compiled && command.Dialect == syntax.Vim9)
		}
		if command.For != nil {
			walkExpression(command.For.Iterable, compiled && command.Dialect == syntax.Vim9)
		}
		if command.Import != nil {
			walkExpression(command.Import.Path, compiled && command.Dialect == syntax.Vim9)
		}
		for _, value := range command.EnumValues {
			walkExpression(value.Initializer, compiled && command.Dialect == syntax.Vim9)
			for _, argument := range value.Arguments {
				walkExpression(argument, compiled && command.Dialect == syntax.Vim9)
			}
		}
		if command.Function != nil {
			for _, parameter := range command.Function.Parameters {
				walkExpression(parameter.Default, compiled && command.Dialect == syntax.Vim9)
			}
		}
	}
	walkBlockBodies = func(commands []syntax.Command, blocks []syntax.Block, block syntax.Block, compiled bool) {
		if block.Header < 0 || block.End <= block.Header || block.End >= len(commands) {
			return
		}
		if block.Kind == syntax.BlockIf || block.Kind == syntax.BlockTry {
			headers := append([]int{block.Header}, block.Branches...)
			for index, header := range headers {
				end := block.End
				if index+1 < len(headers) {
					end = headers[index+1]
				}
				walkSequence(commands, blocks, header+1, end, compiled)
			}
			return
		}
		walkSequence(commands, blocks, block.Header+1, block.End, compiled)
	}
	walkSequence = func(commands []syntax.Command, blocks []syntax.Block, start, end int, compiled bool) {
		if start < 0 || end < start || end > len(commands) {
			return
		}
		for index := start; index < end; {
			command := &commands[index]
			walkCommandExpressions(command, compiled)
			next := index + 1
			flow := functionFlowFallsThrough
			if isBlockHeader(blocks, index, command.Block) {
				block := blocks[command.Block]
				switch block.Kind {
				case syntax.BlockDef:
					walkBlockBodies(commands, blocks, block, true)
				case syntax.BlockFunction:
					walkBlockBodies(commands, blocks, block, false)
				default:
					walkBlockBodies(commands, blocks, block, compiled)
				}
				if block.End > index && block.End < end {
					next = block.End + 1
					if compiled && command.Dialect == syntax.Vim9 && !syntaxDiagnosticOverlaps(file.Diagnostics, block.Span) {
						flow = commandBlockFlow(commands, blocks, block)
					}
				} else {
					return
				}
			} else if compiled && command.Dialect == syntax.Vim9 && !syntaxDiagnosticOverlaps(file.Diagnostics, command.Span) {
				switch command.Canonical {
				case "return":
					flow = functionFlowReturns
				case "throw":
					flow = functionFlowThrows
				}
			}
			if flow.terminates() {
				if next < end {
					message := "Unreachable code after :return"
					if flow == functionFlowThrows {
						message = "Unreachable code after :throw"
					}
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1095", Message: message, Span: commands[next].Name})
				}
				return
			}
			index = next
		}
	}
	walkSequence(file.Commands, file.Blocks, 0, len(file.Commands), file.Dialect == syntax.Vim9)
}

func collectLoopNestingDiagnostics(result *FileAnalysis) {
	if result == nil {
		return
	}
	for command, scope := range result.commandScopes {
		if command == nil || command.Dialect != syntax.Vim9 || (command.Canonical != "for" && command.Canonical != "while") || !scopeUsesDefTypeRules(scope) {
			continue
		}
		depth := 0
		for current := scope; current != nil; current = current.Parent {
			if current.Kind == syntax.BlockFor || current.Kind == syntax.BlockWhile {
				depth++
			}
			if current.Kind == syntax.BlockDef || current.Lambda != nil {
				break
			}
		}
		if depth == 11 {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1306", Message: "Loop nesting too deep", Span: command.Name,
			})
		}
	}
}

func collectAggregateAccessDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	seen := make(map[*syntax.Expression]bool)
	methodCallees := make(map[*syntax.Expression]bool)
	var walkCommands func([]syntax.Command, *Scope)
	var walkExpression func(*syntax.Expression, *Scope, syntax.Dialect)
	walkExpression = func(expression *syntax.Expression, scope *Scope, dialect syntax.Dialect) {
		if expression == nil || seen[expression] {
			return
		}
		seen[expression] = true
		if dialect == syntax.Vim9 {
			if expression.Kind == syntax.ExpressionCall && len(expression.Children) > 0 {
				callee := expression.Children[0]
				if callee != nil && callee.Kind == syntax.ExpressionGenericReference && len(callee.Children) == 1 {
					callee = callee.Children[0]
				}
				if callee != nil && callee.Kind == syntax.ExpressionMember {
					methodCallees[callee] = true
				}
			}
			appendMissingAggregateMethodDiagnostic(result, scope, expression)
			if !methodCallees[expression] {
				appendMissingObjectVariableDiagnostic(result, scope, expression)
				appendMissingClassVariableDiagnostic(result, scope, expression)
			}
		}
		expressionScope := scope
		if expression.Kind == syntax.ExpressionLambda && expression.LambdaBody != nil {
			if lambdaScope := result.lambdaScopes[expression]; lambdaScope != nil {
				expressionScope = lambdaScope
			}
			walkCommands(expression.LambdaBody.Commands, expressionScope)
		}
		for _, child := range expression.Children {
			walkExpression(child, expressionScope, dialect)
		}
	}
	walkCommands = func(items []syntax.Command, fallback *Scope) {
		for index := range items {
			command := &items[index]
			scope := result.commandScopes[command]
			if scope == nil {
				scope = fallback
			}
			for _, expression := range command.Expressions {
				walkExpression(expression, scope, command.Dialect)
			}
			for _, expression := range command.Targets {
				walkExpression(expression, scope, command.Dialect)
			}
			if command.Declaration != nil {
				walkExpression(command.Declaration.Initializer, scope, command.Dialect)
			}
			if command.For != nil {
				walkExpression(command.For.Iterable, scope, command.Dialect)
			}
			if command.Import != nil {
				walkExpression(command.Import.Path, scope, command.Dialect)
			}
			if command.Function != nil {
				for _, parameter := range command.Function.Parameters {
					walkExpression(parameter.Default, scope, command.Dialect)
				}
			}
			if command.Embedded != nil {
				walkCommands(command.Embedded.Commands, scope)
			}
		}
	}
	walkCommands(result.File.Commands, result.Root)
}

func appendMissingAggregateMethodDiagnostic(result *FileAnalysis, scope *Scope, call *syntax.Expression) {
	if result == nil || result.File == nil || call == nil || call.Kind != syntax.ExpressionCall || call.Value != "" || len(call.Children) == 0 ||
		expressionContainsMissing(call) || syntaxDiagnosticOverlaps(result.File.Diagnostics, call.Span) {
		return
	}
	file := result.File
	callee := call.Children[0]
	if callee != nil && callee.Kind == syntax.ExpressionGenericReference && len(callee.Children) == 1 {
		callee = callee.Children[0]
	}
	if callee == nil || callee.Kind != syntax.ExpressionMember || file.Text(callee.Operator) != "." || callee.Value == "" || len(callee.Children) != 1 || callee.Children[0] == nil {
		return
	}
	memberSpan := memberNameSpan(file, callee)
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Span == memberSpan && (diagnostic.Code == "vim/E1366" || diagnostic.Code == "vim/E1385" || diagnostic.Code == "vim/E1386") {
			return
		}
	}

	receiver := callee.Children[0]
	className := ""
	classReceiver := false
	super := false
	var aggregate *syntax.Command
	if receiver.Kind == syntax.ExpressionIdentifier {
		switch receiver.Value {
		case "super":
			aggregate = enclosingClassCommand(file, scope)
			if aggregate == nil || aggregate.Aggregate == nil || len(aggregate.Aggregate.Extends) == 0 {
				return
			}
			className = file.Text(aggregate.Aggregate.Name)
			super = true
		case "this":
			aggregate = enclosingObjectMethodClass(file, scope)
			if aggregate == nil || aggregate.Aggregate == nil {
				return
			}
			className = file.Text(aggregate.Aggregate.Name)
		default:
			declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil)
			if declaration != nil {
				switch declaration.Kind {
				case SymbolKindClass:
					className, classReceiver = declaration.Name, true
				case SymbolKindEnum:
					className, classReceiver = declaration.Name, true
				case SymbolKindTypeAlias:
					className = result.classAliases[declaration.Name]
					classReceiver = className != ""
				}
			}
		}
	}
	if className == "" && !super {
		className = resolvedExpressionType(result, scope, receiver).Name
		if target := result.classAliases[className]; target != "" {
			className = target
		}
	}
	if aggregate == nil {
		aggregate = result.classes[className]
		if aggregate == nil {
			aggregate = localEnum(file, className)
		}
	}
	if aggregate == nil || aggregate.Aggregate == nil {
		return
	}

	methodName := callee.Value
	if super {
		seenClasses := make(map[*syntax.Command]bool)
		for current := extendedClass(file, result.classes, aggregate); current != nil && !seenClasses[current]; current = extendedClass(file, result.classes, current) {
			seenClasses[current] = true
			if aggregateMethod(file, current, methodName) != nil {
				return
			}
		}
	} else if classReceiver {
		enumConstructor := aggregate.Aggregate.Kind == syntax.BlockEnum && (strings.HasPrefix(methodName, "new") || strings.HasPrefix(methodName, "_new"))
		for _, index := range aggregate.Aggregate.Members {
			if index < 0 || index >= len(file.Commands) {
				continue
			}
			member := &file.Commands[index]
			if !enumConstructor && member.Function != nil && commandIsClassMethod(file, member) && file.Text(member.Function.Name) == methodName {
				return
			}
			if member.Declaration != nil && commandHasModifier(member, "static") {
				for bindingIndex, binding := range member.Declaration.Bindings {
					if file.Text(binding.Name) == methodName && aggregateBindingType(result, member, bindingIndex).Name == "func" {
						return
					}
				}
			}
		}
		if aggregate.Aggregate.Kind == syntax.BlockClass && methodName == "new" && !commandHasModifier(aggregate, "abstract") {
			hasConstructor := false
			for _, index := range aggregate.Aggregate.Members {
				if index < 0 || index >= len(file.Commands) {
					continue
				}
				member := &file.Commands[index]
				if member.Function != nil && (file.Text(member.Function.Name) == "new" || file.Text(member.Function.Name) == "_new") {
					hasConstructor = true
					break
				}
			}
			if !hasConstructor {
				return
			}
		}
		if aggregateMethod(file, aggregate, methodName) != nil || aggregate.Aggregate.Kind == syntax.BlockClass && objectMethodInClassHierarchy(file, result.classes, aggregate, methodName) != nil {
			return
		}
	} else {
		if aggregate.Aggregate.Kind == syntax.BlockClass {
			if objectMethodInClassHierarchy(file, result.classes, aggregate, methodName) != nil {
				return
			}
			if typ, found := classObjectVariableType(result, aggregate, methodName); found && typ.Name == "func" {
				return
			}
		} else {
			if aggregateMethod(file, aggregate, methodName) != nil {
				return
			}
			if typ, found := aggregateObjectVariableType(result, aggregate, methodName); found && typ.Name == "func" {
				return
			}
		}
		enumConstructor := aggregate.Aggregate.Kind == syntax.BlockEnum && (strings.HasPrefix(methodName, "new") || strings.HasPrefix(methodName, "_new"))
		for _, index := range aggregate.Aggregate.Members {
			if index < 0 || index >= len(file.Commands) {
				continue
			}
			member := &file.Commands[index]
			if !enumConstructor && member.Function != nil && commandIsClassMethod(file, member) && file.Text(member.Function.Name) == methodName {
				return
			}
		}
	}

	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1325", Message: `Method "` + methodName + `" not found in class "` + className + `"`, Span: memberSpan,
	})
}

func appendMissingObjectVariableDiagnostic(result *FileAnalysis, scope *Scope, member *syntax.Expression) {
	if result == nil || result.File == nil || scope == nil || member == nil || member.Kind != syntax.ExpressionMember ||
		len(member.Children) != 1 || member.Children[0] == nil || result.File.Text(member.Operator) != "." || member.Value == "" ||
		expressionContainsMissing(member) || syntaxDiagnosticOverlaps(result.File.Diagnostics, member.Span) {
		return
	}
	file := result.File
	className, aggregate, super, found := objectAggregateReceiver(result, scope, member.Children[0], make(map[*syntax.Expression]bool))
	if !found || aggregate == nil || aggregate.Aggregate == nil {
		return
	}
	name := member.Value
	switch aggregate.Aggregate.Kind {
	case syntax.BlockClass:
		if _, _, found := classObjectVariableBinding(result, aggregate, name); found || objectMethodInClassHierarchy(file, result.classes, aggregate, name) != nil {
			return
		}
		if !super && aggregateHasClassMember(file, aggregate, name) {
			return
		}
	case syntax.BlockEnum:
		if enumObjectAccessExists(file, aggregate, name) || !super && aggregateHasClassMember(file, aggregate, name) {
			return
		}
	default:
		return
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Span == memberNameSpan(file, member) && (diagnostic.Code == "vim/E1333" || diagnostic.Code == "vim/E1335" || diagnostic.Code == "vim/E1375" || diagnostic.Code == "vim/E1385" || diagnostic.Code == "vim/E1386" || diagnostic.Code == "vim/E1409") {
			return
		}
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1326", Message: `Variable "` + name + `" not found in object "` + className + `"`, Span: memberNameSpan(file, member),
	})
}

func appendMissingClassVariableDiagnostic(result *FileAnalysis, scope *Scope, member *syntax.Expression) {
	if result == nil || result.File == nil || scope == nil || member == nil || member.Kind != syntax.ExpressionMember ||
		len(member.Children) != 1 || member.Children[0] == nil || result.File.Text(member.Operator) != "." || member.Value == "" ||
		expressionContainsMissing(member) || syntaxDiagnosticOverlaps(result.File.Diagnostics, member.Span) {
		return
	}
	file := result.File
	receiver := member.Children[0]
	if receiver.Kind != syntax.ExpressionIdentifier {
		return
	}
	declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil)
	if declaration == nil {
		return
	}
	className := ""
	switch declaration.Kind {
	case SymbolKindClass:
		className = declaration.Name
	case SymbolKindTypeAlias:
		className = result.classAliases[declaration.Name]
	case SymbolKindEnum:
		return
	}
	class := result.classes[className]
	if class == nil || class.Aggregate == nil {
		return
	}
	if aggregateHasClassMember(file, class, member.Value) {
		return
	}
	if _, _, found := classObjectVariableBinding(result, class, member.Value); found || objectMethodInClassHierarchy(file, result.classes, class, member.Value) != nil {
		return
	}
	memberSpan := memberNameSpan(file, member)
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Span != memberSpan {
			continue
		}
		switch diagnostic.Code {
		case "vim/E1333", "vim/E1335", "vim/E1366", "vim/E1375", "vim/E1376", "vim/E1385", "vim/E1386", "vim/E1409", "vim/E1422", "vim/E1423":
			return
		}
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1337", Message: `Class variable "` + member.Value + `" not found in class "` + className + `"`, Span: memberSpan,
	})
}

func objectAggregateReceiver(result *FileAnalysis, scope *Scope, receiver *syntax.Expression, seen map[*syntax.Expression]bool) (string, *syntax.Command, bool, bool) {
	if result == nil || result.File == nil || scope == nil || receiver == nil || seen[receiver] {
		return "", nil, false, false
	}
	seen[receiver] = true
	file := result.File
	for receiver.Kind == syntax.ExpressionParenthesized && len(receiver.Children) == 1 && receiver.Children[0] != nil {
		receiver = receiver.Children[0]
		if seen[receiver] {
			return "", nil, false, false
		}
		seen[receiver] = true
	}
	if receiver.Kind == syntax.ExpressionIdentifier {
		switch receiver.Value {
		case "this":
			if aggregate := enclosingObjectMethodClass(file, scope); aggregate != nil && aggregate.Aggregate != nil {
				return file.Text(aggregate.Aggregate.Name), aggregate, false, true
			}
			return "", nil, false, false
		case "super":
			current := enclosingObjectMethodClass(file, scope)
			if current == nil || current.Aggregate == nil {
				return "", nil, false, false
			}
			parent := extendedClass(file, result.classes, current)
			if parent == nil {
				return "", nil, false, false
			}
			return file.Text(current.Aggregate.Name), parent, true, true
		default:
			if declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil); declaration != nil {
				switch declaration.Kind {
				case SymbolKindClass, SymbolKindEnum, SymbolKindTypeAlias:
					return "", nil, false, false
				}
			}
		}
	}
	if receiver.Kind == syntax.ExpressionMember && len(receiver.Children) == 1 && receiver.Children[0] != nil {
		_, aggregate, _, found := objectAggregateReceiver(result, scope, receiver.Children[0], seen)
		if found && aggregate != nil {
			var typ ValueType
			var hasType bool
			if aggregate.Aggregate != nil && aggregate.Aggregate.Kind == syntax.BlockClass {
				typ, hasType = classObjectVariableType(result, aggregate, receiver.Value)
			} else {
				typ, hasType = aggregateObjectVariableType(result, aggregate, receiver.Value)
			}
			if hasType {
				if target := result.classAliases[typ.Name]; target != "" {
					typ.Name = target
				}
				if class := result.classes[typ.Name]; class != nil {
					return typ.Name, class, false, true
				}
				if enum := localEnum(file, typ.Name); enum != nil {
					return typ.Name, enum, false, true
				}
			}
		}
	}
	typ := resolvedExpressionType(result, scope, receiver)
	if target := result.classAliases[typ.Name]; target != "" {
		typ.Name = target
	}
	if class := result.classes[typ.Name]; class != nil {
		return typ.Name, class, false, true
	}
	if enum := localEnum(file, typ.Name); enum != nil {
		return typ.Name, enum, false, true
	}
	return "", nil, false, false
}

func aggregateHasClassMember(file *syntax.File, aggregate *syntax.Command, name string) bool {
	if file == nil || aggregate == nil || aggregate.Aggregate == nil {
		return false
	}
	for _, index := range aggregate.Aggregate.Members {
		if index < 0 || index >= len(file.Commands) {
			continue
		}
		member := &file.Commands[index]
		if member.Function != nil && commandIsClassMethod(file, member) && file.Text(member.Function.Name) == name {
			return true
		}
		if member.Declaration != nil && commandHasModifier(member, "static") {
			for _, binding := range member.Declaration.Bindings {
				if file.Text(binding.Name) == name {
					return true
				}
			}
		}
	}
	return false
}

func enumObjectAccessExists(file *syntax.File, enum *syntax.Command, name string) bool {
	if file == nil || enum == nil || enum.Aggregate == nil {
		return false
	}
	if name == "name" || name == "ordinal" {
		return true
	}
	for _, index := range enum.Aggregate.Members {
		if index < 0 || index >= len(file.Commands) {
			continue
		}
		member := &file.Commands[index]
		if member.Declaration != nil && !commandHasModifier(member, "static") {
			for _, binding := range member.Declaration.Bindings {
				if file.Text(binding.Name) == name {
					return true
				}
			}
		}
		if member.Function != nil && !commandIsClassMethod(file, member) && file.Text(member.Function.Name) == name {
			return true
		}
	}
	return false
}

func enclosingObjectMethodClass(file *syntax.File, scope *Scope) *syntax.Command {
	aggregate, method := enclosingClassMethod(file, scope)
	if aggregate == nil || method == nil || method.Function == nil {
		return nil
	}
	name := file.Text(method.Function.Name)
	if !commandHasModifier(method, "static") || strings.HasPrefix(name, "new") || strings.HasPrefix(name, "_new") {
		return aggregate
	}
	return nil
}

func enclosingClassMethod(file *syntax.File, scope *Scope) (*syntax.Command, *syntax.Command) {
	return enclosingAggregateMethod(file, scope, false)
}

func enclosingSuperMethod(file *syntax.File, scope *Scope) (*syntax.Command, *syntax.Command) {
	return enclosingAggregateMethod(file, scope, true)
}

func enclosingAggregateMethod(file *syntax.File, scope *Scope, allowEnum bool) (*syntax.Command, *syntax.Command) {
	aggregate := enclosingAggregateCommand(file, scope)
	if aggregate == nil || aggregate.Aggregate == nil || aggregate.Aggregate.Kind != syntax.BlockClass && (!allowEnum || aggregate.Aggregate.Kind != syntax.BlockEnum) {
		return nil, nil
	}
	for current := scope; current != nil; current = current.Parent {
		if current.Kind != syntax.BlockDef {
			continue
		}
		if current.CommandList != nil || current.Block < 0 || current.Block >= len(file.Blocks) {
			return nil, nil
		}
		header := file.Blocks[current.Block].Header
		if header < 0 || header >= len(file.Commands) {
			return nil, nil
		}
		for _, member := range aggregate.Aggregate.Members {
			if member != header {
				continue
			}
			method := &file.Commands[header]
			if method.Function == nil {
				return nil, nil
			}
			return aggregate, method
		}
		return nil, nil
	}
	return nil, nil
}

func appendSuperMustBeFollowedByDotDiagnostic(result *FileAnalysis, file *syntax.File, scope *Scope, expression *syntax.Expression, dialect syntax.Dialect) {
	if result == nil || file == nil || scope == nil || expression == nil || dialect != syntax.Vim9 ||
		expression.Kind != syntax.ExpressionIdentifier || expression.Value != "super" || result.superMemberExempt[expression.Span] ||
		syntaxDiagnosticOverlaps(file.Diagnostics, expression.Span) {
		return
	}
	if aggregate, method := enclosingSuperMethod(file, scope); aggregate == nil || method == nil {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1356", Message: `"super" must be followed by a dot`, Span: expression.Span,
	})
}

func appendSuperOutsideClassMethodDiagnostic(result *FileAnalysis, file *syntax.File, scope *Scope, expression *syntax.Expression, dialect syntax.Dialect) {
	if result == nil || file == nil || scope == nil || expression == nil || dialect != syntax.Vim9 || expression.Kind != syntax.ExpressionMember ||
		len(expression.Children) == 0 || expression.Children[0] == nil || expression.Children[0].Kind != syntax.ExpressionIdentifier ||
		expression.Children[0].Value != "super" || file.Text(expression.Operator) != "." || syntaxDiagnosticOverlaps(file.Diagnostics, expression.Span) {
		return
	}
	if aggregate, method := enclosingSuperMethod(file, scope); aggregate != nil && method != nil {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1357", Message: `Using "super" not in a class method`, Span: expression.Children[0].Span,
	})
}

func appendSuperNotInChildClassDiagnostic(result *FileAnalysis, file *syntax.File, scope *Scope, expression *syntax.Expression, dialect syntax.Dialect) {
	if result == nil || file == nil || scope == nil || expression == nil || dialect != syntax.Vim9 || expression.Kind != syntax.ExpressionMember ||
		len(expression.Children) == 0 || expression.Children[0] == nil || expression.Children[0].Kind != syntax.ExpressionIdentifier ||
		expression.Children[0].Value != "super" || file.Text(expression.Operator) != "." || syntaxDiagnosticOverlaps(file.Diagnostics, expression.Span) {
		return
	}
	aggregate, method := enclosingSuperMethod(file, scope)
	if aggregate == nil || aggregate.Aggregate == nil || method == nil || method.Function == nil ||
		aggregate.Block < 0 || aggregate.Block >= len(file.Blocks) || file.Blocks[aggregate.Block].End < 0 {
		return
	}
	name := file.Text(method.Function.Name)
	if commandHasModifier(method, "static") && !strings.HasPrefix(name, "new") && !strings.HasPrefix(name, "_new") {
		return
	}
	if aggregate.Aggregate.Kind == syntax.BlockClass && len(aggregate.Aggregate.Extends) > 0 {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1358", Message: `Using "super" not in a child class`, Span: expression.Children[0].Span,
	})
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
	importRedefinitions := make(map[string]bool)
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code != "vim/E1213" {
			continue
		}
		for _, declaration := range declarations {
			if declaration != nil && declaration.Span == diagnostic.Span {
				importRedefinitions[declaration.Name] = true
				break
			}
		}
	}
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
			if importRedefinitions[declaration.Name] {
				seen[declaration.Name] = true
				seenKind[declaration.Name] = declaration.Kind
				continue
			}
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

func collectImportedItemRedefinitionDiagnostics(result *FileAnalysis, commands []syntax.Command) {
	if result == nil || result.Root == nil {
		return
	}
	eligibleImports := make(map[syntax.Span]bool)
	var collectImports func([]syntax.Command)
	collectImports = func(items []syntax.Command) {
		for index := range items {
			command := &items[index]
			if command.Dialect == syntax.Vim9 && command.Import != nil && !emptySyntaxSpan(command.Import.Alias) {
				eligibleImports[command.Import.Alias] = true
			}
			if command.Embedded != nil {
				collectImports(command.Embedded.Commands)
			}
		}
	}
	collectImports(commands)
	declarations := append([]*Declaration(nil), result.Declarations...)
	sort.SliceStable(declarations, func(i, j int) bool { return declarations[i].Span.Start < declarations[j].Span.Start })
	occupied := make(map[string]bool)
	imports := make(map[string]*Declaration)
	for _, declaration := range declarations {
		if declaration == nil || declaration.Scope == nil {
			continue
		}
		if declaration.Kind == SymbolKindImport {
			if declaration.Scope == result.Root && eligibleImports[declaration.Span] && !occupied[declaration.Name] {
				imports[declaration.Name] = declaration
			}
			if declaration.Scope == result.Root {
				occupied[declaration.Name] = true
			}
			continue
		}
		if imported := imports[declaration.Name]; imported != nil && imported.Span.Start < declaration.Span.Start && declaration.Kind == SymbolKindFunction &&
			scopeContainsDef(declaration.Scope) && resolve(declaration.Scope, declaration.Name, declaration.Span.Start, false, nil) == imported {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1236", Message: "Cannot use " + declaration.Name + " itself, it is imported", Span: declaration.Span})
		}
		if imported := imports[declaration.Name]; imported != nil && imported.Span.Start < declaration.Span.Start && importedItemScriptScope(result.Root, declaration.Scope) &&
			(declaration.Kind == SymbolKindVariable || declaration.Kind == SymbolKindConstant || functionSymbolKind(declaration.Kind)) && !declaration.Parameter {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1213", Message: `Redefining imported item "` + declaration.Name + `"`, Span: declaration.Span})
			delete(imports, declaration.Name)
		}
		if importedItemScriptScope(result.Root, declaration.Scope) {
			occupied[declaration.Name] = true
		}
	}
}

func importedItemScriptScope(root, scope *Scope) bool {
	for current := scope; current != nil; current = current.Parent {
		if current.Lambda != nil {
			return false
		}
		switch current.Kind {
		case syntax.BlockDef, syntax.BlockFunction, syntax.BlockClass, syntax.BlockInterface, syntax.BlockEnum:
			return false
		}
		if current == root {
			return true
		}
	}
	return false
}

func collectVim9RedeclarationDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	first := make(map[*Scope]map[string]int)
	commandDialects := make(map[syntax.Span]syntax.Dialect)
	for command := range result.commandScopes {
		if command.Declaration == nil {
			continue
		}
		for _, binding := range command.Declaration.Bindings {
			commandDialects[binding.Name] = command.Dialect
		}
	}
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
		legacyFunction := false
		for scope := declaration.Scope; scope != nil; scope = scope.Parent {
			if scope.Kind == syntax.BlockDef || scope.Lambda != nil {
				break
			}
			if scope.Kind == syntax.BlockFunction {
				legacyFunction = true
				break
			}
		}
		if legacyFunction {
			continue
		}
		vim9Context := scopeUsesDefTypeRules(declaration.Scope) ||
			(commandDialects[declaration.Span] == syntax.Vim9 && declarationHasCompoundTarget(result.File, declaration.Span))
		if !vim9Context {
			continue
		}
		duplicate := first[declaration.Scope][declaration.Name] < declaration.Span.Start
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
		scope := result.commandScopes[command]
		defRules := scopeUsesDefTypeRules(scope)
		if command.Dialect == syntax.Vim9 && command.Declaration != nil && command.Declaration.Initializer != nil {
			bindings := command.Declaration.Bindings
			if len(bindings) > 0 && command.Declaration.Target != nil &&
				(command.Declaration.Target.Kind == syntax.ExpressionList || command.Declaration.Target.Kind == syntax.ExpressionTuple) {
				fixed := 0
				rest := false
				cardinalityDefRules := defRules
				for _, binding := range bindings {
					if binding.Rest {
						rest = true
					} else {
						fixed++
					}
					if binding.ParsedType != nil {
						cardinalityDefRules = true
					}
				}
				appendVim9CardinalityDiagnostic(result, fixed, rest, command.Declaration.Initializer, cardinalityDefRules)
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
						appendVim9CardinalityDiagnostic(result, expected, rest, rhs, defRules)
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

func collectLegacyListCardinalityDiagnostics(result *FileAnalysis, commands []syntax.Command) {
	for index := range commands {
		command := &commands[index]
		if command.Dialect == syntax.Legacy {
			if command.Declaration != nil && command.Declaration.Target != nil && command.Declaration.Initializer != nil &&
				command.Declaration.Target.Kind == syntax.ExpressionList {
				appendLegacyListCardinalityDiagnostic(result, command.Declaration.Target, command.Declaration.Initializer)
			}
			var check func(*syntax.Expression)
			check = func(expression *syntax.Expression) {
				if expression == nil {
					return
				}
				if expression.Kind == syntax.ExpressionAssignment && len(expression.Children) >= 2 {
					target, rhs := expression.Children[0], expression.Children[1]
					if target.Kind == syntax.ExpressionList {
						appendLegacyListCardinalityDiagnostic(result, target, rhs)
					}
				}
				for _, child := range expression.Children {
					check(child)
				}
			}
			for _, expression := range command.Expressions {
				if command.Declaration != nil && expression != nil && expression.Kind == syntax.ExpressionAssignment &&
					len(expression.Children) > 0 && expression.Children[0] == command.Declaration.Target {
					continue
				}
				check(expression)
			}
		}
		if command.Embedded != nil {
			collectLegacyListCardinalityDiagnostics(result, command.Embedded.Commands)
		}
	}
}

func appendLegacyListCardinalityDiagnostic(result *FileAnalysis, target, rhs *syntax.Expression) {
	if result == nil || result.File == nil || target == nil || rhs == nil || rhs.Kind != syntax.ExpressionList ||
		expressionContainsMissing(target) || expressionContainsMissing(rhs) {
		return
	}
	fixed := len(target.Children)
	if strings.Contains(result.File.Text(target.Span), ";") {
		fixed--
	}
	if len(rhs.Children) < fixed {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E688", Message: "More targets than List items", Span: rhs.Span,
		})
	} else if !strings.Contains(result.File.Text(target.Span), ";") && len(rhs.Children) > fixed {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E687", Message: "Less targets than List items", Span: rhs.Span,
		})
	}
}

func appendVim9CardinalityDiagnostic(result *FileAnalysis, expected int, rest bool, rhs *syntax.Expression, defRules bool) {
	if rhs == nil || expressionContainsMissing(rhs) {
		return
	}
	if !defRules && isStaticNullTuple(rhs) {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E1536", Message: "Tuple required", Span: rhs.Span,
		})
		return
	}
	rhsType := result.TypeOf(rhs)
	if !isUnknownType(rhsType) && rhsType.Name != "list" && rhsType.Name != "tuple" {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E1535", Message: "List or Tuple required", Span: rhs.Span,
		})
		return
	}
	if rhs.Kind != syntax.ExpressionList && rhs.Kind != syntax.ExpressionTuple {
		return
	}
	got := len(rhs.Children)
	if rest && got >= expected || !rest && got == expected {
		return
	}
	if !defRules && rhs.Kind == syntax.ExpressionTuple && got < expected {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E1538", Message: "More targets than Tuple items", Span: rhs.Span,
		})
		return
	}
	if !defRules && rhs.Kind == syntax.ExpressionList && got < expected {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E688", Message: "More targets than List items", Span: rhs.Span,
		})
		return
	}
	if !defRules && rhs.Kind == syntax.ExpressionList && !rest && got > expected {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E687", Message: "Less targets than List items", Span: rhs.Span,
		})
		return
	}
	if !defRules && rhs.Kind == syntax.ExpressionTuple && !rest && got > expected {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E1537", Message: "Less targets than Tuple items", Span: rhs.Span,
		})
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1093", Message: "Expected " + strconv.Itoa(expected) + " items but got " + strconv.Itoa(got), Span: rhs.Span,
	})
}

// collectVoidValueDiagnostics reports E1031 and E1186 where a statically known
// void result must produce a value. Effect-only calls are valid, and unknown
// values remain deliberately conservative.
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
	seenE1186 := make(map[syntax.Span]bool)
	appendE1186 := func(expression *syntax.Expression) {
		if expression == nil || expression.Span.End <= expression.Span.Start || expressionContainsMissing(expression) ||
			syntaxDiagnosticTouchesCall(result.File.Diagnostics, expression.Span) || seenE1186[expression.Span] || result.TypeOf(expression).Name != "void" {
			return
		}
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E1186" && diagnostic.Span == expression.Span {
				return
			}
		}
		seenE1186[expression.Span] = true
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E1186", Message: "Expression does not result in a value: " + result.File.Text(expression.Span), Span: expression.Span,
		})
	}
	var walkExpression func(*syntax.Expression)
	walkExpression = func(expression *syntax.Expression) {
		if expression == nil {
			return
		}
		if expression.LambdaBody != nil {
			collectVoidValueDiagnostics(result, expression.LambdaBody.Commands)
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
		scope := result.commandScopes[command]
		if command.Dialect == syntax.Vim9 {
			if declaration := command.Declaration; declaration != nil && declaration.Initializer != nil {
				if declaration.ParsedType == nil {
					appendDiagnostic(declaration.Initializer)
				}
				walkExpression(declaration.Initializer)
			}
			for _, expression := range command.Expressions {
				walkExpression(expression)
			}
			for _, target := range command.Targets {
				walkExpression(target)
			}
		}
		multiExpression := command.Canonical == "echo" || command.Canonical == "echon"
		if !multiExpression && command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope) {
			switch command.Canonical {
			case "echomsg", "echoerr", "echoconsole", "echowindow", "execute":
				multiExpression = true
			}
		}
		if multiExpression {
			for _, expression := range command.Expressions {
				appendE1186(expression)
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
		collectForTypeMismatchDiagnostic(result, scope, command)
		if command.Dialect == syntax.Vim9 {
			collectDeclarationTypeMismatchDiagnostic(result, command)
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
	if command.Block >= 0 && command.Block < len(result.File.Blocks) && result.File.Blocks[command.Block].Kind == syntax.BlockClass {
		return
	}
	if declaration.Target != nil && (declaration.Target.Kind == syntax.ExpressionList || declaration.Target.Kind == syntax.ExpressionTuple) {
		appendDestructuringTypeMismatchDiagnostic(result, nil, declaration.Target, declaration.Initializer, declaration.Bindings)
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

func collectForTypeMismatchDiagnostic(result *FileAnalysis, scope *Scope, command *syntax.Command) {
	loop := command.For
	if loop == nil {
		return
	}
	if command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope) {
		for _, binding := range loop.Bindings {
			if strings.HasPrefix(result.File.Text(binding.Name), "s:") {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1254", Message: "Cannot use script variable in for loop", Span: binding.Name})
			}
		}
	}
	if loop.Iterable == nil || expressionContainsMissing(loop.Iterable) {
		return
	}
	if syntaxDiagnosticOverlaps(result.File.Diagnostics, loop.Iterable.Span) || syntaxDiagnosticOverlaps(result.Diagnostics, loop.Iterable.Span) {
		return
	}
	if command.Dialect == syntax.Vim9 && loop.Iterable.Kind == syntax.ExpressionList {
		targetStart := command.Argument.Start
		for targetStart < loop.Iterable.Span.Start && (result.File.Source[targetStart] == ' ' || result.File.Source[targetStart] == '\t') {
			targetStart++
		}
		destructuring := targetStart < loop.Iterable.Span.Start && result.File.Source[targetStart] == '['
		if destructuring {
			fixed := 0
			rest := false
			for _, binding := range loop.Bindings {
				if binding.Rest {
					rest = true
				} else {
					fixed++
				}
			}
			for _, item := range loop.Iterable.Children {
				if item != nil && item.Kind == syntax.ExpressionList && len(item.Children) < fixed {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E711", Message: "List value does not have enough items", Span: item.Span,
					})
					return
				}
				if item != nil && item.Kind == syntax.ExpressionList && !rest && len(item.Children) > fixed {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E710", Message: "List value has more items than targets", Span: item.Span,
					})
					return
				}
			}
		}
	}
	iterable := result.TypeOf(loop.Iterable)
	if command.Dialect != syntax.Vim9 {
		if !isUnknownType(iterable) && iterable.Name != "list" && iterable.Name != "tuple" && iterable.Name != "string" && iterable.Name != "blob" {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1523", Message: "String, List, Tuple or Blob required", Span: loop.Iterable.Span,
			})
		}
		return
	}
	if !isUnknownType(iterable) && iterable.Name != "list" && iterable.Name != "tuple" && iterable.Name != "string" && iterable.Name != "blob" {
		name := valueTypeCategory(iterable)
		if result.classes[name] != nil || result.classAliases[name] != "" || name == "enum" {
			name = "object"
		}
		if name == "partial" {
			name = "func"
		}
		switch name {
		case "number", "float", "bool", "dict", "func", "void", "job", "channel", "class", "object", "special":
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1177", Message: "For loop on " + name + " not supported", Span: loop.Iterable.Span})
			return
		}
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
	if _, ok := objectCompoundAssignment(result, scope, expression); ok {
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
	if receiver, invalid := compiledMemberReceiverType(result, scope, expression); invalid {
		receiverExpression := expression.Children[0]
		for receiverExpression != nil && receiverExpression.Kind == syntax.ExpressionParenthesized && len(receiverExpression.Children) == 1 {
			receiverExpression = receiverExpression.Children[0]
		}
		if _, nestedInvalid := compiledMemberReceiverType(result, scope, receiverExpression); !nestedInvalid {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1229", Message: "Expected dictionary for using key \"" + expression.Value + "\", but got " + valueTypeDisplay(receiver), Span: expression.Span,
			})
		}
	}
	if expression.Kind == syntax.ExpressionAssignment && expression.Value == "=" && len(expression.Children) >= 2 && !expressionContainsMissing(expression) {
		target := expression.Children[0]
		if (target.Kind == syntax.ExpressionList || target.Kind == syntax.ExpressionTuple) && appendDestructuringTypeMismatchDiagnostic(result, scope, target, expression.Children[1], nil) {
			return
		}
		if target != nil && len(target.Children) > 0 && (target.Kind == syntax.ExpressionIndex || target.Kind == syntax.ExpressionSlice) &&
			resolvedExpressionType(result, scope, target.Children[0]).Name == "tuple" {
			diagnostic := syntax.Diagnostic{Code: "vim/E1532", Message: "Cannot modify a tuple", Span: target.Span}
			if target.Kind == syntax.ExpressionSlice {
				diagnostic.Code = "vim/E1533"
				diagnostic.Message = "Cannot slice a tuple"
			}
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			return
		}
		if sliceAssignmentNeedsE1165(result, scope, expression) {
			if target != nil {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1165", Message: "Cannot use a range with an assignment: " + result.File.Text(expression.Span), Span: target.Span,
				})
			}
			return
		}
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

func compiledMemberReceiverType(result *FileAnalysis, scope *Scope, expression *syntax.Expression) (ValueType, bool) {
	if result == nil || result.File == nil || expression == nil || expression.Kind != syntax.ExpressionMember || !scopeUsesDefTypeRules(scope) ||
		expression.Value == "" || len(expression.Children) != 1 || result.File.Text(expression.Operator) != "." || expressionContainsMissing(expression) ||
		syntaxDiagnosticOverlaps(result.File.Diagnostics, expression.Span) {
		return UnknownValueType, false
	}
	receiverExpression := expression.Children[0]
	var declaration *Declaration
	if receiverExpression != nil && receiverExpression.Kind == syntax.ExpressionIdentifier {
		declaration = resolve(scope, receiverExpression.Value, receiverExpression.Span.Start, false, nil)
	}
	receiver := resolvedExpressionType(result, scope, receiverExpression)
	interfaces := localAggregates(result.File, syntax.BlockInterface)
	invalid := !isUnknownType(receiver) && receiver.Name != "dict" && receiver.Name != "object" && result.classes[receiver.Name] == nil &&
		result.classAliases[receiver.Name] == "" && interfaces[receiver.Name] == nil && receiver.Name != "enum" && localEnum(result.File, receiver.Name) == nil &&
		(declaration == nil || declaration.Kind != SymbolKindClass && declaration.Kind != SymbolKindInterface && declaration.Kind != SymbolKindEnum && declaration.Kind != SymbolKindTypeAlias)
	return receiver, invalid
}

func appendDestructuringTypeMismatchDiagnostic(result *FileAnalysis, scope *Scope, target, rhs *syntax.Expression, bindings []syntax.Binding) bool {
	if result == nil || result.File == nil || target == nil || rhs == nil || expressionContainsMissing(target) || expressionContainsMissing(rhs) {
		return false
	}
	rhsType := result.TypeOf(rhs)
	literal := rhs.Kind == syntax.ExpressionList || rhs.Kind == syntax.ExpressionTuple
	rest := strings.Contains(result.File.Text(target.Span), ";")
	fixed := len(target.Children)
	if rest {
		fixed--
	}
	if literal && (rest && len(rhs.Children) < fixed || !rest && len(target.Children) != len(rhs.Children)) {
		return false
	}
	if !literal && (isUnknownType(rhsType) || rhsType.Name != "list" && rhsType.Name != "tuple") {
		return false
	}
	if !literal && rhsType.Name == "tuple" && (rest && len(rhsType.Arguments) < fixed || !rest && len(target.Children) != len(rhsType.Arguments)) {
		return false
	}
	for index, targetItem := range target.Children {
		if rest && index == fixed {
			break
		}
		if targetItem == nil || targetItem.Kind == syntax.ExpressionIdentifier && targetItem.Value == "_" {
			continue
		}
		expected := UnknownValueType
		if len(bindings) > index && bindings[index].ParsedType != nil {
			expected = convertSyntaxType(bindings[index].ParsedType)
		} else if scope != nil {
			expected = assignmentTargetType(result, scope, targetItem)
		}
		actual := UnknownValueType
		span := rhs.Span
		if literal {
			actual = result.TypeOf(rhs.Children[index])
			span = rhs.Children[index].Span
		} else if rhsType.Name == "list" && len(rhsType.Arguments) > 0 {
			actual = rhsType.Arguments[0]
		} else if rhsType.Name == "tuple" && index < len(rhsType.Arguments) {
			actual = rhsType.Arguments[index]
		}
		if isUnknownType(expected) || isUnknownType(actual) || assignmentTypesCompatible(expected, actual) {
			continue
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E1163", Message: "Variable " + strconv.Itoa(index+1) + ": type mismatch, expected " + valueTypeDisplay(expected) + " but got " + valueTypeDisplay(actual), Span: span,
		})
		return true
	}
	return false
}

func collectConditionTypeMismatchDiagnostic(result *FileAnalysis, scope *Scope, command *syntax.Command) {
	if command == nil || command.Dialect != syntax.Vim9 || command.Canonical != "if" && command.Canonical != "elseif" && command.Canonical != "while" || len(command.Expressions) == 0 {
		return
	}
	condition := command.Expressions[0]
	if diagnostic, ok := stringAsBoolDiagnostic(result, condition); ok {
		literal := condition
		for literal.Kind == syntax.ExpressionParenthesized && len(literal.Children) == 1 {
			literal = literal.Children[0]
		}
		if !scopeUsesDefTypeRules(scope) || command.Canonical != "while" && literal.Kind == syntax.ExpressionString {
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			return
		}
	}
	if diagnostic, ok := numberAsBoolDiagnostic(condition); ok {
		result.Diagnostics = append(result.Diagnostics, diagnostic)
		return
	}
	if !scopeUsesDefTypeRules(scope) {
		if diagnostic, ok := objectAsNumberDiagnostic(result, scope, condition); ok {
			result.Diagnostics = append(result.Diagnostics, diagnostic)
		}
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
	if expression.Kind == syntax.ExpressionIndex && len(expression.Children) > 0 {
		receiver := resolvedExpressionType(result, scope, expression.Children[0])
		if receiver.Name == "tuple" && len(expression.Children) > 1 {
			if index, ok := staticTupleIndex(expression.Children[1]); ok {
				if index < 0 {
					index += len(receiver.Arguments)
				}
				if index >= 0 && index < len(receiver.Arguments) {
					return receiver.Arguments[index]
				}
			}
		}
		return indexedType(receiver)
	}
	if expression.Kind == syntax.ExpressionMember && len(expression.Children) > 0 {
		return indexedType(resolvedExpressionType(result, scope, expression.Children[0]))
	}
	return UnknownValueType
}

func staticTupleIndex(expression *syntax.Expression) (int, bool) {
	if expression == nil {
		return 0, false
	}
	if expression.Kind == syntax.ExpressionNumber {
		index, err := strconv.Atoi(expression.Value)
		return index, err == nil
	}
	if expression.Kind == syntax.ExpressionUnary && expression.Value == "-" && len(expression.Children) == 1 && expression.Children[0] != nil && expression.Children[0].Kind == syntax.ExpressionNumber {
		index, err := strconv.Atoi(expression.Children[0].Value)
		return -index, err == nil
	}
	return 0, false
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
	if !knownAssignmentType(expected) || !knownAssignmentType(actual) {
		return true
	}
	if expected.Name == "float" && actual.Name == "number" {
		return true
	}
	if valueTypeCategory(expected) != valueTypeCategory(actual) {
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

func knownAssignmentType(typ ValueType) bool {
	switch valueTypeCategory(typ) {
	case "blob", "bool", "channel", "class", "dict", "enum", "float", "func", "job", "list", "number", "object", "partial", "special", "string", "tuple", "typealias", "void":
		return true
	default:
		return false
	}
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
		addDeclaration(result, commandScope, file, command.Import.Alias, SymbolKindImport, false)
	}
	if command.Declaration != nil {
		mutable := command.Canonical != "const" && command.Canonical != "final"
		kind := SymbolKindVariable
		if !mutable {
			kind = SymbolKindConstant
		}
		for _, binding := range command.Declaration.Bindings {
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

func unusedVariableScope(scope *Scope) bool {
	for current := scope; current != nil; current = current.Parent {
		if current.Lambda != nil || current.Kind == syntax.BlockDef {
			return true
		}
		switch current.Kind {
		case syntax.BlockClass, syntax.BlockInterface, syntax.BlockEnum:
			return false
		}
	}
	return true
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
		if declaration.Parameter || declaration.Name == "_" || declaration.Kind != SymbolKindVariable && declaration.Kind != SymbolKindConstant {
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
		if command.Declaration != nil {
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
	if expression == nil || scope == nil || file == nil {
		return
	}
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
			declaration := resolve(scope, expression.Value, expression.Span.Start, preferFunction, skipped)
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

func appendClassAsValueDiagnostic(result *FileAnalysis, declaration *Declaration, expression *syntax.Expression, dialect syntax.Dialect) bool {
	if result == nil || declaration == nil || expression == nil || dialect != syntax.Vim9 || result.classValueExempt[expression.Span] {
		return false
	}
	className := ""
	switch declaration.Kind {
	case SymbolKindClass:
		className = declaration.Name
	case SymbolKindTypeAlias:
		className = result.classAliases[declaration.Name]
	}
	if className == "" {
		return false
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1405", Message: "Class \"" + className + "\" cannot be used as a value", Span: expression.Span,
	})
	return true
}

func appendObjectMethodThroughClassDiagnostic(result *FileAnalysis, scope *Scope, member *syntax.Expression) {
	if result == nil || result.File == nil || scope == nil || member == nil || member.Kind != syntax.ExpressionMember ||
		len(member.Children) != 1 || member.Children[0] == nil || member.Children[0].Kind != syntax.ExpressionIdentifier ||
		result.File.Text(member.Operator) != "." || member.Value == "" || strings.HasPrefix(member.Value, "_") ||
		strings.HasPrefix(member.Value, "new") {
		return
	}
	receiver := member.Children[0]
	declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil)
	if declaration == nil {
		return
	}
	className := ""
	switch declaration.Kind {
	case SymbolKindClass:
		className = declaration.Name
	case SymbolKindTypeAlias:
		className = result.classAliases[declaration.Name]
	}
	if className == "" {
		return
	}
	file := result.File
	seen := make(map[*syntax.Command]bool)
	for class := result.classes[className]; class != nil; class = extendedClass(file, result.classes, class) {
		if seen[class] {
			return
		}
		seen[class] = true
		for _, memberIndex := range class.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			method := &file.Commands[memberIndex]
			if method.Function == nil || file.Text(method.Function.Name) != member.Value {
				continue
			}
			if !commandHasModifier(method, "static") {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1386", Message: `Object method "` + member.Value + `" accessible only using class "` + className + `" object`, Span: memberNameSpan(file, member),
				})
			}
			return
		}
	}
}

func appendProtectedMethodAccessDiagnostic(result *FileAnalysis, scope *Scope, member *syntax.Expression) {
	if result == nil || result.File == nil || scope == nil || member == nil || member.Kind != syntax.ExpressionMember ||
		len(member.Children) != 1 || member.Children[0] == nil || result.File.Text(member.Operator) != "." ||
		!strings.HasPrefix(member.Value, "_") {
		return
	}

	receiver := member.Children[0]
	className := ""
	classReceiver := false
	if receiver.Kind == syntax.ExpressionIdentifier {
		if receiver.Value == "this" {
			if current := enclosingClassCommand(result.File, scope); current != nil && current.Aggregate != nil {
				className = result.File.Text(current.Aggregate.Name)
			}
		} else if declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil); declaration != nil {
			switch declaration.Kind {
			case SymbolKindClass:
				className, classReceiver = declaration.Name, true
			case SymbolKindTypeAlias:
				className = result.classAliases[declaration.Name]
				classReceiver = className != ""
			}
		}
	}
	if className == "" {
		className = resolvedExpressionType(result, scope, receiver).Name
	}
	class := result.classes[className]
	if class == nil {
		return
	}

	file := result.File
	owner, classMethod := (*syntax.Command)(nil), false
	seen := make(map[*syntax.Command]bool)
	for current := class; current != nil; current = extendedClass(file, result.classes, current) {
		if seen[current] {
			return
		}
		seen[current] = true
		for _, memberIndex := range current.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			method := &file.Commands[memberIndex]
			if method.Function == nil || file.Text(method.Function.Name) != member.Value {
				continue
			}
			classMethod = commandIsClassMethod(file, method)
			if current != class && classMethod {
				continue
			}
			owner = current
			break
		}
		if owner != nil {
			break
		}
	}
	if owner == nil {
		return
	}

	current := enclosingClassCommand(file, scope)
	allowed := classReceiver == classMethod && current == owner
	if classReceiver == classMethod && !classMethod {
		allowed = false
		for seen := make(map[*syntax.Command]bool); !allowed && current != nil; current = extendedClass(file, result.classes, current) {
			if seen[current] {
				return
			}
			seen[current] = true
			allowed = current == class
		}
	}
	if !allowed {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E1366", Message: "Cannot access protected method: " + member.Value, Span: memberNameSpan(file, member),
		})
	}
}

func appendProtectedVariableAccessDiagnostic(result *FileAnalysis, scope *Scope, member *syntax.Expression) {
	if result == nil || result.File == nil || scope == nil || member == nil || member.Kind != syntax.ExpressionMember ||
		len(member.Children) != 1 || member.Children[0] == nil || result.File.Text(member.Operator) != "." || member.Value == "" ||
		!strings.HasPrefix(member.Value, "_") || expressionContainsMissing(member) || syntaxDiagnosticOverlaps(result.File.Diagnostics, member.Span) {
		return
	}
	file := result.File
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1333" && diagnostic.Span == memberNameSpan(file, member) {
			return
		}
	}
	owner := (*syntax.Command)(nil)
	objectVariable := false
	if receiver := member.Children[0]; receiver.Kind == syntax.ExpressionIdentifier {
		if declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil); declaration != nil {
			className := ""
			switch declaration.Kind {
			case SymbolKindClass, SymbolKindEnum:
				className = declaration.Name
			case SymbolKindTypeAlias:
				className = result.classAliases[declaration.Name]
			}
			if className != "" {
				aggregate := result.classes[className]
				if aggregate == nil {
					aggregate = localEnum(file, className)
				}
				if variable, _, found := aggregateVariableBinding(file, aggregate, member.Value, true); found && !commandHasModifier(variable, "public") {
					owner = aggregate
				}
			}
		}
	}
	if owner == nil {
		_, aggregate, _, found := objectAggregateReceiver(result, scope, member.Children[0], make(map[*syntax.Expression]bool))
		if !found || aggregate == nil || aggregate.Aggregate == nil {
			return
		}
		switch aggregate.Aggregate.Kind {
		case syntax.BlockClass:
			candidate, variable, _, exists := classObjectVariableOwner(result, aggregate, member.Value)
			if exists && !commandHasModifier(variable, "public") {
				owner = candidate
				objectVariable = true
			}
		case syntax.BlockEnum:
			if variable, _, exists := aggregateVariableBinding(file, aggregate, member.Value, false); exists && !commandHasModifier(variable, "public") {
				owner = aggregate
				objectVariable = true
			}
		}
	}
	if owner == nil || owner.Aggregate == nil {
		return
	}
	current := enclosingAggregateCommand(file, scope)
	allowed := current == owner
	if objectVariable && !allowed && owner.Aggregate.Kind == syntax.BlockClass && current != nil && current.Aggregate != nil && current.Aggregate.Kind == syntax.BlockClass {
		for seen := make(map[*syntax.Command]bool); current != nil && !seen[current]; current = extendedClass(file, result.classes, current) {
			seen[current] = true
			allowed = current == owner
			if allowed {
				break
			}
		}
	}
	if !allowed {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E1333", Message: `Cannot access protected variable "` + member.Value + `" in class "` + file.Text(owner.Aggregate.Name) + `"`, Span: memberNameSpan(file, member),
		})
	}
}

func enclosingAggregateCommand(file *syntax.File, scope *Scope) *syntax.Command {
	for current := scope; file != nil && current != nil; current = current.Parent {
		if current.CommandList != nil || current.Block < 0 || current.Block >= len(file.Blocks) {
			continue
		}
		switch current.Kind {
		case syntax.BlockClass, syntax.BlockInterface, syntax.BlockEnum:
			header := file.Blocks[current.Block].Header
			if header >= 0 && header < len(file.Commands) {
				return &file.Commands[header]
			}
		}
	}
	return nil
}

func appendObjectVariableThroughClassDiagnostic(result *FileAnalysis, scope *Scope, member *syntax.Expression) {
	if result == nil || result.File == nil || scope == nil || member == nil || member.Kind != syntax.ExpressionMember ||
		len(member.Children) != 1 || member.Children[0] == nil || member.Children[0].Kind != syntax.ExpressionIdentifier ||
		result.File.Text(member.Operator) != "." || member.Value == "" {
		return
	}
	receiver := member.Children[0]
	declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil)
	if declaration == nil {
		return
	}
	className := ""
	switch declaration.Kind {
	case SymbolKindClass:
		className = declaration.Name
	case SymbolKindTypeAlias:
		className = result.classAliases[declaration.Name]
	}
	class := result.classes[className]
	if class == nil {
		return
	}
	if _, found := classObjectVariableType(result, class, member.Value); found {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E1376", Message: `Object variable "` + member.Value + `" accessible only using class "` + className + `" object`, Span: memberNameSpan(result.File, member),
		})
	}
}

func appendClassMethodThroughObjectDiagnostic(result *FileAnalysis, scope *Scope, member *syntax.Expression) {
	if result == nil || result.File == nil || scope == nil || member == nil || member.Kind != syntax.ExpressionMember ||
		len(member.Children) != 1 || member.Children[0] == nil || result.File.Text(member.Operator) != "." ||
		member.Value == "" || strings.HasPrefix(member.Value, "_") {
		return
	}
	receiver := member.Children[0]
	if receiver.Kind == syntax.ExpressionIdentifier {
		declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil)
		if declaration != nil && (declaration.Kind == SymbolKindClass || declaration.Kind == SymbolKindTypeAlias && result.classAliases[declaration.Name] != "") {
			return
		}
	}
	className := resolvedExpressionType(result, scope, receiver).Name
	class := result.classes[className]
	if class == nil || class.Aggregate == nil {
		return
	}
	file := result.File
	for _, memberIndex := range class.Aggregate.Members {
		if memberIndex < 0 || memberIndex >= len(file.Commands) {
			continue
		}
		method := &file.Commands[memberIndex]
		if method.Function == nil || file.Text(method.Function.Name) != member.Value {
			continue
		}
		if commandIsClassMethod(file, method) {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1385", Message: `Class method "` + member.Value + `" accessible only using class "` + className + `"`, Span: memberNameSpan(file, member),
			})
		}
		return
	}
}

func appendClassVariableThroughObjectDiagnostic(result *FileAnalysis, scope *Scope, member *syntax.Expression) {
	if result == nil || result.File == nil || scope == nil || member == nil || member.Kind != syntax.ExpressionMember ||
		len(member.Children) != 1 || member.Children[0] == nil || result.File.Text(member.Operator) != "." || member.Value == "" {
		return
	}
	receiver := member.Children[0]
	if receiver.Kind == syntax.ExpressionIdentifier {
		declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil)
		if declaration != nil && (declaration.Kind == SymbolKindClass || declaration.Kind == SymbolKindTypeAlias && result.classAliases[declaration.Name] != "") {
			return
		}
	}
	file := result.File
	class := result.classes[resolvedExpressionType(result, scope, receiver).Name]
	if class == nil || class.Aggregate == nil {
		return
	}
	for _, memberIndex := range class.Aggregate.Members {
		if memberIndex < 0 || memberIndex >= len(file.Commands) {
			continue
		}
		declaration := &file.Commands[memberIndex]
		if declaration.Declaration == nil || !commandHasModifier(declaration, "static") {
			continue
		}
		for _, binding := range declaration.Declaration.Bindings {
			if file.Text(binding.Name) == member.Value {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1375", Message: `Class variable "` + member.Value + `" accessible only using class "` + file.Text(class.Aggregate.Name) + `"`, Span: memberNameSpan(file, member),
				})
				return
			}
		}
	}
}

func appendUnqualifiedClassMethodDiagnostic(result *FileAnalysis, file *syntax.File, call *syntax.Expression, scope *Scope, dialect syntax.Dialect) {
	if result == nil || file == nil || call == nil || scope == nil || dialect != syntax.Vim9 || call.Kind != syntax.ExpressionCall ||
		len(call.Children) == 0 || call.Children[0] == nil || call.Children[0].Kind != syntax.ExpressionIdentifier {
		return
	}
	callee := call.Children[0]
	if callee.Value == "" || strings.HasPrefix(callee.Value, "_") || resolve(scope, callee.Value, callee.Span.Start, true, nil) != nil {
		return
	}
	current := enclosingClassCommand(file, scope)
	if current == nil || current.Aggregate == nil {
		return
	}
	seen := make(map[*syntax.Command]bool)
	for class := current; class != nil; class = extendedClass(file, result.classes, class) {
		if seen[class] {
			return
		}
		seen[class] = true
		for _, memberIndex := range class.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			method := &file.Commands[memberIndex]
			if method.Function == nil || file.Text(method.Function.Name) != callee.Value || !commandIsClassMethod(file, method) {
				continue
			}
			if class != current {
				owner := file.Text(class.Aggregate.Name)
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1384", Message: `Class method "` + callee.Value + `" accessible only inside class "` + owner + `"`, Span: callee.Span,
				})
			}
			return
		}
	}
}

func appendAbstractSuperMethodDiagnostic(result *FileAnalysis, file *syntax.File, call *syntax.Expression, scope *Scope, dialect syntax.Dialect) {
	if result == nil || file == nil || call == nil || dialect != syntax.Vim9 || call.Kind != syntax.ExpressionCall || len(call.Children) == 0 || expressionContainsMissing(call) {
		return
	}
	callee := call.Children[0]
	if callee == nil || callee.Kind != syntax.ExpressionMember || file.Text(callee.Operator) != "." || len(callee.Children) != 1 || callee.Value == "" {
		return
	}
	receiver := callee.Children[0]
	if receiver == nil || receiver.Kind != syntax.ExpressionIdentifier || receiver.Value != "super" {
		return
	}
	class := enclosingClassCommand(file, scope)
	if class == nil || class.Aggregate == nil || len(class.Aggregate.Extends) == 0 {
		return
	}
	classes := localAggregates(file, syntax.BlockClass)
	seen := make(map[*syntax.Command]bool)
	for current := classes[file.Text(class.Aggregate.Extends[0])]; current != nil; current = extendedClass(file, classes, current) {
		if seen[current] {
			return
		}
		seen[current] = true
		method := aggregateMethod(file, current, callee.Value)
		if method == nil {
			continue
		}
		if commandHasModifier(method, "abstract") {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1431", Message: "Abstract method \"" + callee.Value + "\" in class \"" + file.Text(current.Aggregate.Name) + "\" cannot be accessed directly",
				Span: memberNameSpan(file, callee),
			})
		}
		return
	}
}

func localClassAliases(file *syntax.File) map[string]string {
	classes := localAggregates(file, syntax.BlockClass)
	targets := make(map[string]string)
	for index := range file.Commands {
		alias := file.Commands[index].TypeAlias
		if alias != nil && alias.Type != nil && alias.Type.Kind == syntax.TypeNamed {
			targets[file.Text(alias.Name)] = alias.Type.Name
		}
	}
	aliases := make(map[string]string)
	for alias, target := range targets {
		seen := make(map[string]bool)
		for target != "" && !seen[target] {
			seen[target] = true
			if classes[target] != nil {
				aliases[alias] = target
				break
			}
			target = targets[target]
		}
	}
	return aliases
}

func enclosingClassCommand(file *syntax.File, scope *Scope) *syntax.Command {
	for current := scope; file != nil && current != nil; current = current.Parent {
		if current.Kind != syntax.BlockClass || current.CommandList != nil || current.Block < 0 || current.Block >= len(file.Blocks) {
			continue
		}
		header := file.Blocks[current.Block].Header
		if header >= 0 && header < len(file.Commands) {
			return &file.Commands[header]
		}
	}
	return nil
}

func memberNameSpan(file *syntax.File, member *syntax.Expression) syntax.Span {
	if file == nil || member == nil || member.Value == "" {
		return syntax.Span{}
	}
	text := file.Text(member.Span)
	if offset := strings.LastIndex(text, member.Value); offset >= 0 {
		return syntax.Span{Start: member.Span.Start + offset, End: member.Span.Start + offset + len(member.Value)}
	}
	return member.Span
}

func appendNonGenericFunctionDiagnostic(result *FileAnalysis, expression *syntax.Expression, scope *Scope, hidden map[syntax.Span]bool) {
	if result == nil || expression == nil || len(expression.TypeArguments) == 0 || len(expression.Children) == 0 {
		return
	}
	for _, argument := range expression.TypeArguments {
		if argument == nil || argument.Kind == syntax.TypeMissing {
			return
		}
	}
	callee := expression.Children[0]
	if callee == nil || callee.Kind != syntax.ExpressionIdentifier {
		return
	}
	declaration := resolve(scope, callee.Value, callee.Span.Start, true, hidden)
	if declaration == nil {
		if result.File.Dialect == syntax.Vim9 && validScopeVariableName(callee.Value) && !strings.Contains(callee.Value, "#") &&
			!syntaxDiagnosticOverlaps(result.File.Diagnostics, callee.Span) {
			if _, builtin := vimdata.LookupFunction(callee.Value); !builtin && !vimdata.IsNeovimCompatFunction(callee.Value) {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1558", Message: "Unknown generic function: " + callee.Value, Span: callee.Span,
				})
			}
		}
		return
	}
	if !functionSymbolKind(declaration.Kind) || declaration.TypeParameterCount > 0 {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1560", Message: "Not a generic function: " + callee.Value, Span: callee.Span,
	})
}

func appendNotEnoughGenericTypeArgumentsDiagnostic(result *FileAnalysis, expression *syntax.Expression, scope *Scope, hidden map[syntax.Span]bool) {
	if result == nil || expression == nil || len(expression.TypeArguments) == 0 || len(expression.Children) == 0 {
		return
	}
	for _, argument := range expression.TypeArguments {
		if argument == nil || argument.Kind == syntax.TypeMissing {
			return
		}
	}
	callee := expression.Children[0]
	if callee == nil || callee.Kind != syntax.ExpressionIdentifier {
		return
	}
	declaration := resolve(scope, callee.Value, callee.Span.Start, true, hidden)
	if declaration == nil || !functionSymbolKind(declaration.Kind) || len(expression.TypeArguments) >= declaration.TypeParameterCount {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1557", Message: "Not enough types specified for generic function '" + callee.Value + "'", Span: callee.Span,
	})
}

func appendTooManyGenericTypeArgumentsDiagnostic(result *FileAnalysis, expression *syntax.Expression, scope *Scope, hidden map[syntax.Span]bool) {
	if result == nil || expression == nil || len(expression.TypeArguments) == 0 || len(expression.Children) == 0 {
		return
	}
	for _, argument := range expression.TypeArguments {
		if argument == nil || argument.Kind == syntax.TypeMissing {
			return
		}
	}
	callee := expression.Children[0]
	if callee == nil || callee.Kind != syntax.ExpressionIdentifier {
		return
	}
	declaration := resolve(scope, callee.Value, callee.Span.Start, true, hidden)
	if declaration == nil || !functionSymbolKind(declaration.Kind) || declaration.TypeParameterCount == 0 || len(expression.TypeArguments) <= declaration.TypeParameterCount {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1556", Message: "Too many types specified for generic function '" + callee.Value + "'", Span: callee.Span,
	})
}

func appendGenericFunctionCallWithoutTypesDiagnostic(result *FileAnalysis, expression *syntax.Expression, scope *Scope, hidden map[syntax.Span]bool) {
	if result == nil || expression == nil || len(expression.TypeArguments) > 0 || len(expression.Children) == 0 {
		return
	}
	callee := expression.Children[0]
	if callee == nil || callee.Kind != syntax.ExpressionIdentifier {
		return
	}
	declaration := resolve(scope, callee.Value, callee.Span.Start, true, hidden)
	if declaration != nil && functionSymbolKind(declaration.Kind) && declaration.TypeParameterCount > 0 {
		appendMissingGenericTypeArgumentsDiagnostic(result, callee.Value, callee.Span)
	}
}

func appendQuotedGenericFunctionDiagnostic(result *FileAnalysis, expression *syntax.Expression, scope *Scope, hidden map[syntax.Span]bool) {
	if result == nil || expression == nil || len(expression.Children) < 2 {
		return
	}
	callee, argument := expression.Children[0], expression.Children[1]
	if callee == nil || callee.Kind != syntax.ExpressionIdentifier || callee.Value != "function" && callee.Value != "funcref" && callee.Value != "call" || argument == nil || argument.Kind != syntax.ExpressionString {
		return
	}
	name := simpleVimStringLiteral(argument.Value)
	if name == "" {
		return
	}
	declaration := resolve(scope, name, argument.Span.Start, true, hidden)
	if declaration != nil && functionSymbolKind(declaration.Kind) && declaration.TypeParameterCount > 0 {
		appendMissingGenericTypeArgumentsDiagnostic(result, name, argument.Span)
	}
}

func simpleVimStringLiteral(value string) string {
	if len(value) < 2 || value[0] != value[len(value)-1] || value[0] != '\'' && value[0] != '"' {
		return ""
	}
	value = value[1 : len(value)-1]
	if strings.ContainsAny(value, "\\\"'") {
		return ""
	}
	return value
}

func appendMissingGenericTypeArgumentsDiagnostic(result *FileAnalysis, name string, span syntax.Span) {
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1559", Message: "Type arguments missing for generic function '" + name + "'", Span: span,
	})
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
			declaration := resolve(scope, expression.Value, expression.Span.Start, false, skipped)
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

func appendUnderscoreDiagnostic(result *FileAnalysis, expression *syntax.Expression, dialect syntax.Dialect) bool {
	if result == nil || expression == nil || expression.Kind != syntax.ExpressionIdentifier || expression.Value != "_" || dialect != syntax.Vim9 {
		return false
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1181" && diagnostic.Span == expression.Span {
			return true
		}
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1181", Message: "Cannot use an underscore here", Span: expression.Span})
	return true
}

func assignmentTargetNeedsDeclaration(name string) bool {
	return name != "this" && name != "super" && !strings.Contains(name, ":") && !strings.HasPrefix(name, "&") && !strings.HasPrefix(name, "$") && !strings.HasPrefix(name, "@")
}

func appendVim9UnresolvedReadDiagnostic(result *FileAnalysis, scope *Scope, name string, span syntax.Span) {
	if appendInheritedClassVariableDiagnostic(result, scope, name, span) {
		return
	}
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

func appendInheritedClassVariableDiagnostic(result *FileAnalysis, scope *Scope, name string, span syntax.Span) bool {
	if result == nil || result.File == nil || scope == nil {
		return false
	}
	file := result.File
	current := enclosingClassCommand(file, scope)
	if current == nil {
		return false
	}
	seen := make(map[*syntax.Command]bool)
	for class := extendedClass(file, result.classes, current); class != nil && !seen[class]; class = extendedClass(file, result.classes, class) {
		seen[class] = true
		for _, memberIndex := range class.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			member := &file.Commands[memberIndex]
			if member.Declaration == nil || !commandHasModifier(member, "static") {
				continue
			}
			for _, binding := range member.Declaration.Bindings {
				if file.Text(binding.Name) == name {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E1374", Message: `Class variable "` + name + `" accessible only inside class "` + file.Text(class.Aggregate.Name) + `"`, Span: span,
					})
					return true
				}
			}
		}
	}
	return false
}

func isUnknownVimVariable(name string) bool {
	if !strings.HasPrefix(name, "v:") {
		return false
	}
	_, known := vimdata.LookupVariable(name)
	return !known && !vimdata.IsNeovimCompatVariable(name)
}

func vim9UnsupportedNamespace(name string) bool {
	return strings.HasPrefix(name, "a:") || strings.HasPrefix(name, "l:") || strings.HasPrefix(name, "x:")
}

func appendUnknownOptionDiagnostic(result *FileAnalysis, name string, span syntax.Span, scope *Scope) {
	display, ok := unknownOptionDisplay(result, name, span)
	if !ok {
		return
	}
	diag := syntax.Diagnostic{
		Code: "vim/E113", Message: "Unknown option: " + display, Span: span,
	}
	if isUnderGuiOrNvimGuard(result, scope, span) {
		severity := syntax.DiagnosticWarning
		diag.Severity = &severity
	}
	result.Diagnostics = append(result.Diagnostics, diag)
}

func appendUnknownSetOptionDiagnostic(result *FileAnalysis, name string, span syntax.Span, scope *Scope) {
	display, ok := unknownOptionDisplay(result, name, span)
	if !ok {
		return
	}
	diag := syntax.Diagnostic{
		Code: "vim/E518", Message: "Unknown option: " + display, Span: span,
	}
	if isUnderGuiOrNvimGuard(result, scope, span) {
		severity := syntax.DiagnosticWarning
		diag.Severity = &severity
	}
	result.Diagnostics = append(result.Diagnostics, diag)
}

func isUnderGuiOrNvimGuard(result *FileAnalysis, scope *Scope, span syntax.Span) bool {
	if result == nil || result.File == nil || scope == nil {
		return false
	}
	for s := scope; s != nil; s = s.Parent {
		if s.Kind != syntax.BlockIf || s.Block < 0 {
			continue
		}
		var commands []syntax.Command
		var blocks []syntax.Block
		if s.CommandList != nil {
			commands = s.CommandList.Commands
			blocks = s.CommandList.Blocks
		} else {
			commands = result.File.Commands
			blocks = result.File.Blocks
		}
		if s.Block >= len(blocks) {
			continue
		}
		block := blocks[s.Block]
		if block.Header < 0 || block.Header >= len(commands) {
			continue
		}
		if block.End >= 0 && block.End < len(commands) && span.Start >= commands[block.End].Span.Start {
			continue
		}
		branchCmdIdx := -1
		if len(block.Branches) == 0 {
			if span.Start >= commands[block.Header].Span.End {
				branchCmdIdx = block.Header
			}
		} else {
			if span.Start < commands[block.Branches[0]].Span.Start {
				if span.Start >= commands[block.Header].Span.End {
					branchCmdIdx = block.Header
				}
			} else {
				for i := len(block.Branches) - 1; i >= 0; i-- {
					branchIdx := block.Branches[i]
					if branchIdx >= 0 && branchIdx < len(commands) && span.Start >= commands[branchIdx].Span.End {
						branchCmdIdx = branchIdx
						break
					}
				}
			}
		}
		if branchCmdIdx >= 0 && branchCmdIdx < len(commands) {
			guardCmd := &commands[branchCmdIdx]
			if guardCmd.Canonical == "if" || guardCmd.Canonical == "elseif" {
				for _, expr := range guardCmd.Expressions {
					if isGuiOrNvimCondition(expr) {
						return true
					}
				}
			}
		}
	}
	return false
}

func isGuiOrNvimCondition(expr *syntax.Expression) bool {
	if expr == nil {
		return false
	}
	switch expr.Kind {
	case syntax.ExpressionParenthesized:
		for _, child := range expr.Children {
			if isGuiOrNvimCondition(child) {
				return true
			}
		}
		return false
	case syntax.ExpressionCall:
		return isHasGuiOrNvimCall(expr)
	case syntax.ExpressionBinary:
		op := expr.Value
		if (op == "&&" || op == "and") && len(expr.Children) == 2 {
			return isGuiOrNvimCondition(expr.Children[0]) || isGuiOrNvimCondition(expr.Children[1])
		}
		if (op == "||" || op == "or") && len(expr.Children) == 2 {
			return isGuiOrNvimCondition(expr.Children[0]) && isGuiOrNvimCondition(expr.Children[1])
		}
		if (op == "==" || op == "!=") && len(expr.Children) == 2 {
			return isComparisonToHasFeature(expr)
		}
	}
	return false
}

func isHasGuiOrNvimCall(expr *syntax.Expression) bool {
	if expr == nil || expr.Kind != syntax.ExpressionCall {
		return false
	}
	if len(expr.Children) >= 2 {
		callee := expr.Children[0]
		arg := expr.Children[1]
		if callee != nil && callee.Kind == syntax.ExpressionIdentifier && callee.Value == "has" && arg != nil && arg.Kind == syntax.ExpressionString {
			feature, ok := unquoteFeatureString(arg.Value)
			if ok && (strings.EqualFold(feature, "gui_running") || strings.EqualFold(feature, "nvim")) {
				return true
			}
		}
	}
	if expr.Value == "->" && len(expr.Children) == 2 {
		for _, child := range expr.Children {
			if child.Kind == syntax.ExpressionCall && isHasGuiOrNvimCall(child) {
				return true
			}
		}
	}
	return false
}

func unquoteFeatureString(s string) (string, bool) {
	if len(s) >= 2 && ((s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"')) {
		return s[1 : len(s)-1], true
	}
	return "", false
}

func isComparisonToHasFeature(expr *syntax.Expression) bool {
	if expr == nil || len(expr.Children) != 2 {
		return false
	}
	left, right := expr.Children[0], expr.Children[1]
	op := expr.Value
	check := func(callExpr, constExpr *syntax.Expression) bool {
		if !isHasGuiOrNvimCall(callExpr) || constExpr == nil || constExpr.Kind != syntax.ExpressionNumber {
			return false
		}
		val := constExpr.Value
		if op == "==" && val != "0" {
			return true
		}
		if op == "!=" && val == "0" {
			return true
		}
		return false
	}
	return check(left, right) || check(right, left)
}

func unknownOptionDisplay(result *FileAnalysis, name string, span syntax.Span) (string, bool) {
	if result == nil || name == "" || span.End <= span.Start || result.unknownOptions[span] {
		return "", false
	}
	if _, ok := vimdata.LookupOption(name); ok || vimdata.IsNeovimCompatOption(name) || vimdata.IsTerminalOptionName(name) {
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
