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
	classAliases                map[string]string
	classes                     map[string]*syntax.Command
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
	TypeParameterCount int
	constBinding       bool
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
	result := &FileAnalysis{File: file, suppressedSyntaxDiagnostics: make(map[syntax.Diagnostic]bool)}
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
	result.classAliases = localClassAliases(file)
	result.classes = localClasses(file)
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
	collectDuplicateEnumValueDiagnostics(result)
	collectArgumentRedeclarationDiagnostics(result)
	collectArgumentShadowDiagnostics(result)
	collectVim9RedeclarationDiagnostics(result)
	collectVim9NameAlreadyDefinedDiagnostics(result, file.Commands)
	collectImportedItemRedefinitionDiagnostics(result, file.Commands)
	collectVim9ScriptItemRedefinitionDiagnostics(result, file.Commands)
	collectDuplicateTypeAliasDiagnostics(result)
	collectUnimplementedAbstractMethodDiagnostics(result)
	collectMethodAccessLevelDiagnostics(result)
	collectGenericMethodOverrideDiagnostics(result)
	collectMethodTypeMismatchDiagnostics(result)
	collectDuplicateClassVariableDiagnostics(result)
	collectPublicProtectedMemberNameDiagnostics(result)
	collectInterfaceVariableAccessDiagnostics(result)
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
	collectImportNamespaceDiagnostics(result)
	inferTypes(result)
	collectVariableTypeMismatchDiagnostics(result)
	collectVim9DestructuringDiagnostics(result, file.Commands)
	collectFuncrefVariableNameDiagnostics(result)
	collectMissingDictionaryKeyDiagnostics(result, file.Commands, root)
	collectOperatorDiagnostics(result, file.Commands, root)
	collectAggregateAccessDiagnostics(result)
	collectVoidValueDiagnostics(result, file.Commands)
	collectTypeMismatchDiagnostics(result, file.Commands, root)
	collectBuiltinArgumentTypeDiagnostics(result, file.Commands, root)
	collectAssignmentDiagnostics(result, file.Commands, root)
	collectNameOnlyExpressionDiagnostics(result, file.Commands, root)
	sort.SliceStable(result.Diagnostics, func(i, j int) bool {
		return result.Diagnostics[i].Span.Start < result.Diagnostics[j].Span.Start
	})
	return result
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
			return ok
		}
		if isLiteralIdentifier(name) {
			return true
		}
		if strings.HasPrefix(name, "v:") {
			_, ok := vimdata.LookupVariable(name)
			return ok
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
				if command.Kind == syntax.CommandExpression || command.Canonical == "eval" {
					for _, expression := range command.Expressions {
						if expression != nil && expression.Span == command.Argument && nameOnly(expression, scope, command.Canonical == "eval") {
							appendDiagnostic(expression)
						}
					}
				}
				if command.Argument.Start == command.Argument.End && command.Range.Start == command.Range.End && command.Bang.Start == command.Bang.End && command.Kind == syntax.CommandBuiltin {
					name := result.File.Text(command.Name)
					if declaration := resolve(scope, name, command.Name.Start, false, nil); declaration != nil && (declaration.Kind == SymbolKindVariable || declaration.Kind == SymbolKindConstant || declaration.Parameter) {
						appendDiagnostic(&syntax.Expression{Kind: syntax.ExpressionIdentifier, Span: command.Name, Value: name})
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
		compiled := scope.Kind == syntax.BlockDef || scope.Lambda != nil && result.File.Text(scope.Lambda.Operator) == "=>"
		if !compiled || syntaxDiagnosticTouchesCall(result.File.Diagnostics, declaration.Span) {
			continue
		}
		if scriptArgumentConflict(result, declaration) {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1168", Message: "Argument already declared in the script: " + argumentScriptMessageTail(result.File, declaration), Span: declaration.Span})
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
	classes := localClasses(file)
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
	classes := localClasses(file)
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

func collectDuplicateClassVariableDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	classes := localClasses(file)
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
	classes := localClasses(file)
	for index := range file.Commands {
		class := &file.Commands[index]
		if class.Dialect != syntax.Vim9 || class.Aggregate == nil || class.Aggregate.Kind != syntax.BlockClass || len(class.Aggregate.Extends) == 0 {
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
		if class.Dialect != syntax.Vim9 || class.Aggregate == nil || class.Aggregate.Kind != syntax.BlockClass || commandHasModifier(class, "abstract") {
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
		if class.Dialect != syntax.Vim9 || class.Aggregate == nil || class.Aggregate.Kind != syntax.BlockClass {
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
	interfaces := localInterfaces(file)
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
	interfaces := localInterfaces(file)
	for index := range file.Commands {
		class := &file.Commands[index]
		if class.Dialect != syntax.Vim9 || class.Aggregate == nil || class.Aggregate.Kind != syntax.BlockClass {
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

func localInterfaces(file *syntax.File) map[string]*syntax.Command {
	interfaces := make(map[string]*syntax.Command)
	if file == nil {
		return interfaces
	}
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Dialect == syntax.Vim9 && command.Aggregate != nil && command.Aggregate.Kind == syntax.BlockInterface {
			interfaces[file.Text(command.Aggregate.Name)] = command
		}
	}
	return interfaces
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
	seen := make(map[*syntax.Command]bool)
	for current := class; current != nil; current = extendedClass(result.File, result.classes, current) {
		if seen[current] {
			return nil, 0, false
		}
		seen[current] = true
		for _, memberIndex := range current.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(result.File.Commands) {
				continue
			}
			member := &result.File.Commands[memberIndex]
			if member.Declaration == nil || commandHasModifier(member, "static") {
				continue
			}
			for bindingIndex, binding := range member.Declaration.Bindings {
				if result.File.Text(binding.Name) == name {
					return member, bindingIndex, true
				}
			}
		}
	}
	return nil, 0, false
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
	interfaces := localInterfaces(file)
	for index := range file.Commands {
		class := &file.Commands[index]
		if class.Dialect != syntax.Vim9 || class.Aggregate == nil || class.Aggregate.Kind != syntax.BlockClass {
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
		if dot < len(source) && source[dot] == '.' {
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
	if aggregate.Aggregate.Kind == syntax.BlockClass {
		if _, _, found := classObjectVariableBinding(result, aggregate, name); found || objectMethodInClassHierarchy(file, result.classes, aggregate, name) != nil {
			return
		}
		if !super && aggregateHasClassMember(file, aggregate, name) {
			return
		}
	} else if aggregate.Aggregate.Kind == syntax.BlockEnum {
		if enumObjectAccessExists(file, aggregate, name) || !super && aggregateHasClassMember(file, aggregate, name) {
			return
		}
	} else {
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
	aggregate := enclosingClassCommand(file, scope)
	if aggregate == nil || aggregate.Aggregate == nil {
		return nil
	}
	for current := scope; current != nil; current = current.Parent {
		if current.Kind != syntax.BlockDef {
			continue
		}
		if current.CommandList != nil || current.Block < 0 || current.Block >= len(file.Blocks) {
			return nil
		}
		header := file.Blocks[current.Block].Header
		if header < 0 || header >= len(file.Commands) {
			return nil
		}
		for _, member := range aggregate.Aggregate.Members {
			if member != header {
				continue
			}
			method := &file.Commands[header]
			if method.Function == nil {
				return nil
			}
			name := file.Text(method.Function.Name)
			if !commandHasModifier(method, "static") || strings.HasPrefix(name, "new") || strings.HasPrefix(name, "_new") {
				return aggregate
			}
			return nil
		}
		return nil
	}
	return nil
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
		scope := result.commandScopes[command]
		defRules := scopeUsesDefTypeRules(scope)
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
				appendVim9CardinalityDiagnostic(result, fixed, rest, command.Declaration.Initializer, defRules)
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

func strictStringConversionDiagnostic(result *FileAnalysis, scope *Scope, expression *syntax.Expression, interpolate bool) (syntax.Diagnostic, bool) {
	if result == nil || expression == nil {
		return syntax.Diagnostic{}, false
	}
	typ := result.TypeOf(expression)
	name := typ.Name
	if expression.Kind == syntax.ExpressionIdentifier {
		if declaration := resolve(scope, expression.Value, expression.Span.Start, false, nil); declaration != nil && declaration.Kind == SymbolKindTypeAlias {
			name = "typealias"
		} else if declaration != nil && declaration.Kind == SymbolKindClass {
			name = "class"
		}
	}
	if expression.Kind == syntax.ExpressionCall && len(expression.Children) > 0 && expression.Children[0].Kind == syntax.ExpressionIdentifier && expression.Children[0].Value == "function" && len(expression.Children) > 2 {
		name = "partial"
	}
	if expression.Kind == syntax.ExpressionIdentifier && expression.Value == "null_partial" {
		name = "partial"
	}
	if isUnknownType(ValueType{Name: name}) || name == "string" || name == "special" || name == "bool" || name == "number" || name == "float" || interpolate && (name == "list" || name == "tuple" || name == "dict") {
		return syntax.Diagnostic{}, false
	}
	if result.classes[name] != nil {
		name = "object"
	}
	switch name {
	case "list", "tuple", "dict", "void", "blob", "func", "partial", "job", "channel", "class", "object", "typealias":
	default:
		return syntax.Diagnostic{}, false
	}
	return syntax.Diagnostic{Code: "vim/E1105", Message: "Cannot convert " + strings.ToLower(name) + " to string", Span: expression.Span}, true
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

func stringAsBoolDiagnostic(result *FileAnalysis, expression *syntax.Expression) (syntax.Diagnostic, bool) {
	if result == nil || expression == nil || expressionContainsMissing(expression) || result.TypeOf(expression).Name != "string" {
		return syntax.Diagnostic{}, false
	}
	message := "Using a String as a Bool"
	literal := expression
	for literal.Kind == syntax.ExpressionParenthesized && len(literal.Children) == 1 {
		literal = literal.Children[0]
	}
	if literal.Kind == syntax.ExpressionString {
		if value, ok := syntax.StaticDictionaryIndexKey(literal); ok {
			message += `: "` + value + `"`
		}
	}
	return syntax.Diagnostic{Code: "vim/E1135", Message: message, Span: expression.Span}, true
}

func boolAsNumberDiagnostic(typ ValueType, expression *syntax.Expression) (syntax.Diagnostic, bool) {
	if expression == nil || expressionContainsMissing(expression) || typ.Name != "bool" {
		return syntax.Diagnostic{}, false
	}
	return syntax.Diagnostic{Code: "vim/E1138", Message: "Using a Bool as a Number", Span: expression.Span}, true
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

func isStaticNullTuple(expression *syntax.Expression) bool {
	if expression == nil {
		return false
	}
	if expression.Kind == syntax.ExpressionIdentifier {
		return expression.Value == "null_tuple"
	}
	if expression.Kind == syntax.ExpressionParenthesized && len(expression.Children) == 1 {
		return isStaticNullTuple(expression.Children[0])
	}
	return expression.Kind == syntax.ExpressionCall && len(expression.Children) == 1 &&
		expression.Children[0] != nil && expression.Children[0].Kind == syntax.ExpressionIdentifier && expression.Children[0].Value == "test_null_tuple"
}

// collectOperatorDiagnostics keeps compiled Vim9 operator errors distinct
// from the historical conversion errors used by Legacy and script-level Vim9.
// Unknown values remain deliberately opaque.
func collectOperatorDiagnostics(result *FileAnalysis, commands []syntax.Command, parent *Scope) {
	if result == nil || result.File == nil {
		return
	}
	staticInitializers := make(map[*Declaration]*syntax.Expression)
	for index := range commands {
		command := &commands[index]
		if command.Declaration == nil || len(command.Declaration.Bindings) != 1 || command.Declaration.Initializer == nil {
			continue
		}
		scope := result.commandScopes[command]
		if scope == nil {
			scope = parent
		}
		binding := command.Declaration.Bindings[0]
		var declaration *Declaration
		for _, candidate := range scope.Declarations {
			if candidate.Span == binding.Name {
				declaration = candidate
				break
			}
		}
		if declaration != nil {
			staticInitializers[declaration] = command.Declaration.Initializer
		}
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
			if command.Dialect == syntax.Vim9 && expression.Kind == syntax.ExpressionMember {
				appendProtectedMethodAccessDiagnostic(result, expressionScope, expression)
				appendObjectVariableThroughClassDiagnostic(result, expressionScope, expression)
				appendClassVariableThroughObjectDiagnostic(result, expressionScope, expression)
				appendClassMethodThroughObjectDiagnostic(result, expressionScope, expression)
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
			if target, ok := objectCompoundAssignment(result, expressionScope, expression); ok {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1411", Message: "Missing dot after object \"" + target.Value + "\"", Span: target.Span,
				})
				return
			}
			if expression.Kind == syntax.ExpressionBinary || expression.Kind == syntax.ExpressionAssignment {
				op := expression.Value
				if expression.Kind == syntax.ExpressionAssignment {
					op = result.File.Text(expression.Operator)
				}
				if expression.Kind == syntax.ExpressionBinary && command.Dialect == syntax.Vim9 &&
					len(expression.Children) >= 2 && !expressionContainsMissing(expression) {
					base := strings.TrimRight(op, "#?")
					comparison := base == "==" || base == "!=" || base == "=~" || base == "!~" || base == "is" || base == "isnot" ||
						base == ">" || base == ">=" || base == "<" || base == "<="
					if comparison && (base != "is" && base != "isnot" || base == op) {
						leftExpression, rightExpression := expression.Children[0], expression.Children[1]
						left, right := result.TypeOf(leftExpression), result.TypeOf(rightExpression)
						objectOperation := base == ">" || base == ">=" || base == "<" || base == "<=" || base == "=~" || base == "!~"
						if objectOperation && objectComparisonValue(result, expressionScope, leftExpression) && objectComparisonValue(result, expressionScope, rightExpression) {
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
								Code: "vim/E1153", Message: "Invalid operation for object", Span: expression.Operator,
							})
							return
						}
						leftKind, rightKind := builtinValueTypeKind(left), builtinValueTypeKind(right)
						invalid := false
						if !isUnknownType(left) && !isUnknownType(right) && leftKind != 0 && rightKind != 0 && left.Name != "void" && right.Name != "void" {
							if left.Name == right.Name {
								equality := base == "==" || base == "!=" || base == "is" || base == "isnot"
								invalid = !equality && (left.Name == "bool" || left.Name == "special" || left.Name == "blob" || left.Name == "list")
							} else if (left.Name == "number" || left.Name == "float") && (right.Name == "number" || right.Name == "float") {
								// Number and Float comparisons use Vim's shared numeric path.
							} else if left.Name == "special" || right.Name == "special" {
								leftValue, rightValue := leftExpression, rightExpression
								for leftValue.Kind == syntax.ExpressionParenthesized && len(leftValue.Children) == 1 {
									leftValue = leftValue.Children[0]
								}
								for rightValue.Kind == syntax.ExpressionParenthesized && len(rightValue.Children) == 1 {
									rightValue = rightValue.Children[0]
								}
								leftNone := leftValue.Kind == syntax.ExpressionIdentifier && leftValue.Value == "v:none"
								rightNone := rightValue.Kind == syntax.ExpressionIdentifier && rightValue.Value == "v:none"
								invalid = leftNone && right.Name != "string" || rightNone && left.Name != "string"
							} else {
								invalid = true
							}
						}
						if invalid {
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
								Code: "vim/E1072", Message: "Cannot compare " + left.Name + " with " + right.Name, Span: expression.Operator,
							})
						}
					}
				}
				if expression.Kind == syntax.ExpressionBinary && command.Dialect == syntax.Vim9 &&
					(op == "is" || op == "isnot") && len(expression.Children) >= 2 && !expressionContainsMissing(expression) {
					left, right := result.TypeOf(expression.Children[0]), result.TypeOf(expression.Children[1])
					if left.Name == right.Name && (left.Name == "bool" || left.Name == "special" || left.Name == "number" || left.Name == "float") {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E1037", Message: `Cannot use "` + op + `" with ` + left.Name, Span: expression.Operator,
						})
					}
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
				compiled := command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope)
				if compiled && !expressionContainsMissing(expression) && len(expression.Children) >= 2 {
					if expression.Kind == syntax.ExpressionBinary && (op == "." || op == "..") {
						for _, operand := range expression.Children[:2] {
							if diagnostic, ok := strictStringConversionDiagnostic(result, expressionScope, operand, false); ok {
								result.Diagnostics = append(result.Diagnostics, diagnostic)
								break
							}
						}
					} else if expression.Kind == syntax.ExpressionAssignment && (op == ".=" || op == "..=") && assignmentTargetType(result, expressionScope, expression.Children[0]).Name == "string" {
						if diagnostic, ok := strictStringConversionDiagnostic(result, expressionScope, expression.Children[1], false); ok {
							result.Diagnostics = append(result.Diagnostics, diagnostic)
						}
					}
				}
				if expression.Kind == syntax.ExpressionBinary && (op == "<<" || op == ">>") && len(expression.Children) >= 2 && !expressionContainsMissing(expression) {
					leftShift := expression.Children[0]
					for leftShift.Kind == syntax.ExpressionParenthesized && len(leftShift.Children) == 1 {
						leftShift = leftShift.Children[0]
					}
					if leftShift.Kind == syntax.ExpressionBinary && (leftShift.Value == "<<" || leftShift.Value == ">>") {
						walk(leftShift, expressionScope)
						for _, diagnostic := range result.Diagnostics {
							if (diagnostic.Code == "vim/E1282" || diagnostic.Code == "vim/E1283" || diagnostic.Code == "vim/E1012") &&
								diagnostic.Span.Start >= leftShift.Span.Start && diagnostic.Span.End <= leftShift.Span.End {
								return
							}
						}
					}
					compiled := command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope)
					for _, operand := range expression.Children[:2] {
						actual := result.TypeOf(operand)
						if isUnknownType(actual) || actual.Name == "number" {
							continue
						}
						constant := operand
						for constant.Kind == syntax.ExpressionParenthesized && len(constant.Children) == 1 {
							constant = constant.Children[0]
						}
						precompiledConstant := constant.Kind == syntax.ExpressionString || constant.Kind == syntax.ExpressionBlob ||
							constant.Kind == syntax.ExpressionNumber && isFloatLiteral(constant.Value) ||
							constant.Kind == syntax.ExpressionIdentifier && (isLiteralIdentifier(constant.Value) || constant.Value == "v:true" || constant.Value == "v:false" || constant.Value == "v:null" || constant.Value == "v:none")
						if !compiled || precompiledConstant {
							result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
								Code: "vim/E1282", Message: "Bitshift operands must be numbers", Span: expression.Operator,
							})
							return
						}
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E1012", Message: "Type mismatch; expected number but got " + valueTypeDisplay(actual), Span: operand.Span,
						})
						return
					}
					amount, knownAmount := staticNumberValue(expression.Children[1])
					right := expression.Children[1]
					for right.Kind == syntax.ExpressionParenthesized && len(right.Children) == 1 {
						right = right.Children[0]
					}
					if !knownAmount && right.Kind == syntax.ExpressionIdentifier {
						declaration := resolve(expressionScope, right.Value, right.Span.Start, false, nil)
						initializer := staticInitializers[declaration]
						if declaration != nil && declaration.Scope == expressionScope && initializer != nil {
							unchanged := true
							for _, reference := range result.References {
								if reference.Declaration == declaration && reference.assignmentTarget && reference.Span.Start > declaration.Span.End && reference.Span.Start < expression.Children[1].Span.Start {
									unchanged = false
									break
								}
							}
							if unchanged {
								amount, knownAmount = staticNumberValue(initializer)
							}
						}
					}
					if knownAmount && amount < 0 {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E1283", Message: "Bitshift amount must be a positive number", Span: expression.Children[1].Span,
						})
						return
					}
				}
				if !compoundTypeError && (op == "+" || op == "-" || op == "*" || op == "/" || op == "%" || op == "+=" || op == "-=" || op == "*=" || op == "/=" || op == "%=") && len(expression.Children) >= 2 && !expressionContainsMissing(expression) {
					left, right := result.TypeOf(expression.Children[0]), result.TypeOf(expression.Children[1])
					if expression.Kind == syntax.ExpressionAssignment {
						left = assignmentTargetType(result, expressionScope, expression.Children[0])
					}
					boolAsNumber := false
					if command.Dialect == syntax.Vim9 && !scopeUsesDefTypeRules(expressionScope) {
						if diagnostic, ok := boolAsNumberDiagnostic(left, expression.Children[0]); ok {
							result.Diagnostics = append(result.Diagnostics, diagnostic)
							boolAsNumber = true
						} else if left.Name == "number" || left.Name == "float" {
							if diagnostic, ok := boolAsNumberDiagnostic(right, expression.Children[1]); ok {
								result.Diagnostics = append(result.Diagnostics, diagnostic)
								boolAsNumber = true
							}
						}
					}
					if !boolAsNumber && command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope) {
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
					} else if !boolAsNumber && expression.Kind == syntax.ExpressionBinary {
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
						if diagnostic, ok := stringAsBoolDiagnostic(result, left); ok {
							result.Diagnostics = append(result.Diagnostics, diagnostic)
							return
						}
						if logicalRightOperandIsEvaluated(expression) {
							if diagnostic, ok := stringAsBoolDiagnostic(result, right); ok {
								result.Diagnostics = append(result.Diagnostics, diagnostic)
								return
							}
						}
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
					if diagnostic, ok := stringAsBoolDiagnostic(result, condition); ok {
						literal := condition
						for literal.Kind == syntax.ExpressionParenthesized && len(literal.Children) == 1 {
							literal = literal.Children[0]
						}
						if !scopeUsesDefTypeRules(expressionScope) || literal.Kind == syntax.ExpressionString {
							result.Diagnostics = append(result.Diagnostics, diagnostic)
						}
					}
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
				command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope) && !expressionContainsMissing(expression) {
				receiver := expression.Children[0]
				receiverType := resolvedExpressionType(result, expressionScope, receiver)
				candidate := receiver
				for candidate.Kind == syntax.ExpressionParenthesized && len(candidate.Children) == 1 {
					candidate = candidate.Children[0]
				}
				isTypeAlias := false
				if candidate.Kind == syntax.ExpressionIdentifier {
					declaration := resolve(expressionScope, candidate.Value, candidate.Span.Start, false, nil)
					isTypeAlias = declaration != nil && declaration.Kind == SymbolKindTypeAlias
				}
				if !isTypeAlias && (receiverType.Name == "number" || receiverType.Name == "float") {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E1107", Message: "String, List, Dict or Blob required", Span: receiver.Span,
					})
				}
			} else if (expression.Kind == syntax.ExpressionIndex || expression.Kind == syntax.ExpressionSlice) && len(expression.Children) >= 2 &&
				!(command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope)) {
				receiver := expression.Children[0]
				switch resolvedExpressionType(result, expressionScope, receiver).Name {
				case "number":
					if command.Dialect == syntax.Vim9 {
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
							Code: "vim/E1062", Message: "Cannot index a Number", Span: receiver.Span,
						})
					}
				case "float":
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
			if expression.Kind == syntax.ExpressionDictionary && command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope) {
				for keyIndex := 0; keyIndex+1 < len(expression.Children); keyIndex += 2 {
					key := expression.Children[keyIndex]
					if key.Kind != syntax.ExpressionList || len(key.Children) != 1 {
						continue
					}
					key = key.Children[0]
					if diagnostic, ok := strictStringConversionDiagnostic(result, expressionScope, key, false); ok {
						result.Diagnostics = append(result.Diagnostics, diagnostic)
						break
					}
				}
			} else if expression.Kind == syntax.ExpressionDictionary {
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
			if expression.Kind == syntax.ExpressionInterpolatedString && command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(expressionScope) {
				for _, child := range expression.Children {
					if diagnostic, ok := strictStringConversionDiagnostic(result, expressionScope, child, true); ok {
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

func objectComparisonValue(result *FileAnalysis, scope *Scope, expression *syntax.Expression) bool {
	if result == nil || scope == nil || expression == nil {
		return false
	}
	candidate := expression
	for candidate.Kind == syntax.ExpressionParenthesized && len(candidate.Children) == 1 {
		candidate = candidate.Children[0]
	}
	if candidate.Kind == syntax.ExpressionIdentifier {
		if declaration := resolve(scope, candidate.Value, candidate.Span.Start, false, nil); declaration != nil &&
			(declaration.Kind == SymbolKindClass || declaration.Kind == SymbolKindEnum || declaration.Kind == SymbolKindTypeAlias) {
			return false
		}
	}
	typ := resolvedExpressionType(result, scope, expression)
	return typ.Name == "object" || result.classes[typ.Name] != nil
}

func objectCompoundAssignment(result *FileAnalysis, scope *Scope, expression *syntax.Expression) (*syntax.Expression, bool) {
	if result == nil || result.File == nil || scope == nil || expression == nil || expression.Kind != syntax.ExpressionAssignment || expression.Value == "=" || len(expression.Children) < 2 || !scopeUsesDefTypeRules(scope) {
		return nil, false
	}
	target := expression.Children[0]
	if target == nil || target.Kind != syntax.ExpressionIdentifier {
		return nil, false
	}
	declaration := resolve(scope, target.Value, target.Span.Start, false, nil)
	if declaration == nil || declaration.Kind != SymbolKindVariable && declaration.Kind != SymbolKindConstant {
		return nil, false
	}
	className := assignmentTargetType(result, scope, target).Name
	if localClasses(result.File)[className] == nil || !declarationCanHoldObjectClass(result.File, declaration, className) {
		return nil, false
	}
	return target, true
}

func declarationCanHoldObjectClass(file *syntax.File, declaration *Declaration, className string) bool {
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Declaration == nil {
			continue
		}
		for _, binding := range command.Declaration.Bindings {
			if binding.Name != declaration.Span || binding.ParsedType == nil {
				continue
			}
			return convertSyntaxType(binding.ParsedType).Name == className
		}
	}
	return true
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
		if expression == nil || expression.Span.End <= expression.Span.Start || expressionContainsMissing(expression) || seenE1186[expression.Span] || result.TypeOf(expression).Name != "void" {
			return
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
		if command.Dialect == syntax.Vim9 {
			collectDeclarationTypeMismatchDiagnostic(result, command)
			collectForTypeMismatchDiagnostic(result, scope, command)
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
	iterable := result.TypeOf(loop.Iterable)
	if !isUnknownType(iterable) && iterable.Name != "list" && iterable.Name != "tuple" && iterable.Name != "string" && iterable.Name != "blob" {
		name := iterable.Name
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
		if target != nil && target.Kind == syntax.ExpressionIndex && len(target.Children) > 0 && resolvedExpressionType(result, scope, target.Children[0]).Name == "tuple" {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1532", Message: "Cannot modify a tuple", Span: target.Span,
			})
			return
		}
		if target != nil && target.Kind == syntax.ExpressionSlice && len(target.Children) > 0 && resolvedExpressionType(result, scope, target.Children[0]).Name == "tuple" {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1533", Message: "Cannot slice a tuple", Span: target.Span,
			})
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
	interfaces := localInterfaces(result.File)
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
		if command.Canonical == "redir" && command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope) {
			for _, target := range command.Targets {
				appendIndexableAssignmentDiagnostic(result, scope, target)
			}
		}
		if command.Canonical == "unlet" && result.File.Dialect == syntax.Vim9 {
			for _, target := range command.Targets {
				if target == nil || target.Kind != syntax.ExpressionMember || len(target.Children) != 1 || target.Children[0] == nil ||
					target.Children[0].Kind != syntax.ExpressionIdentifier || result.File.Text(target.Operator) != "." || target.Value == "" ||
					syntaxDiagnosticOverlaps(result.File.Diagnostics, target.Span) || syntaxDiagnosticOverlaps(result.Diagnostics, target.Span) {
					continue
				}
				member := syntax.Span{Start: target.Span.End - len(target.Value), End: target.Span.End}
				if !validNameSpan(result.File, member) {
					continue
				}
				first := result.File.Source[member.Start]
				if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_' || first >= utf8.RuneSelf) {
					continue
				}
				receiver := target.Children[0]
				declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil)
				if declaration == nil || declaration.Kind != SymbolKindImport {
					continue
				}
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1260", Message: "Cannot unlet an imported item: " + target.Value, Span: member})
				break
			}
		}
		if command.Canonical == "unlet" && command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope) {
			for _, target := range command.Targets {
				if target == nil || target.Kind != syntax.ExpressionSlice || expressionContainsMissing(target) || len(target.Children) == 0 || target.Children[0] == nil {
					continue
				}
				receiver := target.Children[0]
				if receiver.Kind != syntax.ExpressionIdentifier || syntaxDiagnosticTouchesCall(result.File.Diagnostics, target.Span) || syntaxDiagnosticTouchesCall(result.Diagnostics, target.Span) || resolvedExpressionType(result, scope, receiver).Name != "dict" {
					continue
				}
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1166", Message: "Cannot use a range with a dictionary", Span: target.Span})
				break
			}
		}
		if (command.Canonical == "lockvar" || command.Canonical == "unlockvar") && command.Dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope) {
			for _, target := range command.Targets {
				if target == nil || target.Kind != syntax.ExpressionIdentifier || target.Value == "this" || syntaxDiagnosticOverlaps(result.File.Diagnostics, target.Span) || syntaxDiagnosticOverlaps(result.Diagnostics, target.Span) {
					continue
				}
				declaration := resolve(scope, target.Value, target.Span.Start, false, nil)
				if declaration == nil || declaration.Parameter || declaration.Kind != SymbolKindVariable && declaration.Kind != SymbolKindConstant || declaration.Scope == nil || !scopeUsesDefTypeRules(declaration.Scope) {
					continue
				}
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1178", Message: "Cannot lock or unlock a local variable", Span: target.Span})
				break
			}
		}
		if command.Declaration != nil && command.Declaration.Initializer != nil && command.Declaration.Initializer.Kind == syntax.ExpressionLambda {
			collectAssignmentExpressionDiagnostics(result, scope, command.Declaration.Initializer, command.Dialect)
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
		if dialect == syntax.Vim9 && target != nil && target.Kind == syntax.ExpressionIdentifier && target.Value == "_" {
			return
		}
		if enumName, memberName, ok := enumAssignmentTarget(result, scope, target); ok &&
			(target.Children[0].Kind != syntax.ExpressionMember || scopeContainsDef(scope) && !scopeWithinVim9Enum(result.File, scope, enumName)) {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1423", Message: "Enum value \"" + enumName + "." + memberName + "\" cannot be modified", Span: target.Span,
			})
		} else if className, memberName, ok := readOnlyClassMemberAssignment(result, scope, target); ok {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1409", Message: "Cannot change read-only variable \"" + memberName + "\" in class \"" + className + "\"", Span: memberNameSpan(result.File, target),
			})
		} else if target != nil && target.Kind == syntax.ExpressionMember && len(target.Children) == 1 && target.Children[0] != nil && target.Children[0].Kind == syntax.ExpressionIdentifier && target.Children[0].Value == "this" {
			if enumName, ok := enclosingVim9EnumName(result.File, scope); ok {
				diagnostic := syntax.Diagnostic{Span: target.Span}
				switch target.Value {
				case "name":
					diagnostic.Code = "vim/E1427"
					diagnostic.Message = "Enum \"" + enumName + "\" name cannot be modified"
				case "ordinal":
					diagnostic.Code = "vim/E1426"
					diagnostic.Message = "Enum \"" + enumName + "\" ordinal value cannot be modified"
				}
				if diagnostic.Code != "" {
					result.Diagnostics = append(result.Diagnostics, diagnostic)
				}
			}
		} else if target != nil && target.Kind == syntax.ExpressionIdentifier && validNameSpan(result.File, target.Span) {
			if isReadOnlyVimVariableTarget(target) || dialect == syntax.Legacy && isReadOnlyLegacyArgumentTarget(scope, target) {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E46", Message: "Cannot change read-only variable \"" + target.Value + "\"", Span: target.Span,
				})
			} else if dialect == syntax.Vim9 && !strings.Contains(target.Value, ":") {
				declaration := resolve(scope, target.Value, target.Span.Start, false, nil)
				if declaration != nil && declaration.Parameter {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E1090", Message: "Cannot assign to argument " + target.Value, Span: target.Span,
					})
				} else if declaration != nil && declaration.Kind == SymbolKindTypeAlias {
					diagnostic := syntax.Diagnostic{Span: target.Span}
					if scopeUsesDefTypeRules(scope) {
						diagnostic.Code = "vim/E46"
						diagnostic.Message = "Cannot change read-only variable \"" + target.Value + "\""
					} else {
						diagnostic.Code = "vim/E1403"
						diagnostic.Message = "Type alias \"" + target.Value + "\" cannot be used as a value"
					}
					result.Diagnostics = append(result.Diagnostics, diagnostic)
				} else if expression.Value == "=" && result.File.Text(expression.Operator) == "=" && declaration != nil && declaration.Kind == SymbolKindConstant {
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
		if dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope) && expression.Value != "=" && target != nil && target.Kind == syntax.ExpressionSlice && !expressionContainsMissing(expression) {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1183", Message: "Cannot use a range with an assignment operator: " + result.File.Text(expression.Span), Span: target.Span,
			})
			return
		}
		if dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope) && !sliceAssignmentNeedsE1165(result, scope, expression) {
			appendIndexableAssignmentDiagnostic(result, scope, target)
		}
	}
	for _, child := range expression.Children {
		collectAssignmentExpressionDiagnostics(result, scope, child, dialect)
	}
}

func sliceAssignmentNeedsE1165(result *FileAnalysis, scope *Scope, expression *syntax.Expression) bool {
	if result == nil || scope == nil || expression == nil || expression.Kind != syntax.ExpressionAssignment || expression.Value != "=" ||
		len(expression.Children) < 2 || expressionContainsMissing(expression) || !scopeUsesDefTypeRules(scope) {
		return false
	}
	target := expression.Children[0]
	if target == nil || target.Kind != syntax.ExpressionSlice || len(target.Children) == 0 || target.Children[0] == nil {
		return false
	}
	receiver := resolvedExpressionType(result, scope, target.Children[0])
	return !isUnknownType(receiver) && receiver.Name != "list" && receiver.Name != "blob" && receiver.Name != "tuple"
}

func appendIndexableAssignmentDiagnostic(result *FileAnalysis, scope *Scope, target *syntax.Expression) {
	receiver := invalidAssignmentReceiver(result, scope, target)
	if receiver == nil {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1141", Message: "Indexable type required", Span: receiver.Span,
	})
}

func invalidAssignmentReceiver(result *FileAnalysis, scope *Scope, target *syntax.Expression) *syntax.Expression {
	if result == nil || scope == nil || target == nil || expressionContainsMissing(target) {
		return nil
	}
	for target.Kind == syntax.ExpressionMember || target.Kind == syntax.ExpressionIndex || target.Kind == syntax.ExpressionSlice {
		if len(target.Children) == 0 || target.Children[0] == nil {
			return nil
		}
		receiver := target.Children[0]
		candidate := receiver
		for candidate.Kind == syntax.ExpressionParenthesized && len(candidate.Children) == 1 {
			candidate = candidate.Children[0]
		}
		if candidate.Kind == syntax.ExpressionIdentifier {
			if declaration := resolve(scope, candidate.Value, candidate.Span.Start, false, nil); declaration != nil {
				switch declaration.Kind {
				case SymbolKindClass, SymbolKindEnum, SymbolKindTypeAlias:
					return nil
				}
			}
		}
		typ := resolvedExpressionType(result, scope, receiver)
		if isUnknownType(typ) {
			if candidate.Kind == syntax.ExpressionMember || candidate.Kind == syntax.ExpressionIndex || candidate.Kind == syntax.ExpressionSlice {
				target = candidate
				continue
			}
			return nil
		}
		if typ.Name == "tuple" {
			return nil
		}
		switch typ.Name {
		case "list", "dict", "blob", "class", "object", "enum":
			return nil
		}
		if result.classes[typ.Name] != nil {
			return nil
		}
		if declaration := resolve(scope, typ.Name, receiver.Span.Start, false, nil); declaration != nil &&
			(declaration.Kind == SymbolKindClass || declaration.Kind == SymbolKindEnum) {
			return nil
		}
		return receiver
	}
	return nil
}

func readOnlyClassMemberAssignment(result *FileAnalysis, scope *Scope, target *syntax.Expression) (string, string, bool) {
	if result == nil || result.File == nil || scope == nil || target == nil {
		return "", "", false
	}
	file := result.File
	classes := localClasses(file)
	var class *syntax.Command
	static := false
	if target.Kind == syntax.ExpressionIdentifier {
		class = enclosingClassCommand(file, scope)
		static = true
	} else if target.Kind != syntax.ExpressionMember || len(target.Children) != 1 || target.Children[0] == nil || file.Text(target.Operator) != "." {
		return "", "", false
	} else if receiver := target.Children[0]; receiver.Kind == syntax.ExpressionIdentifier && receiver.Value == "this" {
		if scopeWithinConstructor(file, scope) {
			return "", "", false
		}
		class = enclosingClassCommand(file, scope)
	} else if receiver.Kind == syntax.ExpressionIdentifier {
		declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil)
		if declaration != nil && declaration.Kind == SymbolKindClass {
			class = classes[receiver.Value]
			static = true
		} else {
			class = classes[resolvedExpressionType(result, scope, receiver).Name]
		}
	}
	for current := class; current != nil; current = extendedClass(file, classes, current) {
		for _, memberIndex := range current.Aggregate.Members {
			if memberIndex < 0 || memberIndex >= len(file.Commands) {
				continue
			}
			member := &file.Commands[memberIndex]
			if member.Declaration == nil || file.Text(member.Declaration.Name) != target.Value || commandHasModifier(member, "static") != static {
				continue
			}
			if member.Canonical == "const" || member.Canonical == "final" {
				return file.Text(current.Aggregate.Name), target.Value, true
			}
			return "", "", false
		}
	}
	return "", "", false
}

func scopeWithinConstructor(file *syntax.File, scope *Scope) bool {
	for current := scope; file != nil && current != nil; current = current.Parent {
		if current.Lambda != nil {
			return false
		}
		if current.Kind != syntax.BlockDef || current.Block < 0 {
			continue
		}
		commands, blocks := file.Commands, file.Blocks
		if current.CommandList != nil {
			commands, blocks = current.CommandList.Commands, current.CommandList.Blocks
		}
		if current.Block >= len(blocks) {
			return false
		}
		header := blocks[current.Block].Header
		if header < 0 || header >= len(commands) || commands[header].Function == nil {
			return false
		}
		name := file.Text(commands[header].Function.Name)
		if dot := strings.LastIndex(name, "."); dot >= 0 {
			name = name[dot+1:]
		}
		return name == "new"
	}
	return false
}

func scopeWithinVim9Enum(file *syntax.File, scope *Scope, name string) bool {
	enumName, ok := enclosingVim9EnumName(file, scope)
	return ok && enumName == name
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

func enclosingDefHeaderSpan(file *syntax.File, scope *Scope) syntax.Span {
	for current := scope; file != nil && current != nil; current = current.Parent {
		if current.Kind != syntax.BlockDef || current.Block < 0 {
			continue
		}
		commands, blocks := file.Commands, file.Blocks
		if current.CommandList != nil {
			commands, blocks = current.CommandList.Commands, current.CommandList.Blocks
		}
		if current.Block >= len(blocks) {
			return syntax.Span{}
		}
		header := blocks[current.Block].Header
		if header >= 0 && header < len(commands) {
			return commands[header].Span
		}
		return syntax.Span{}
	}
	return syntax.Span{}
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
			if declaration != nil && command.Canonical == "const" {
				declaration.constBinding = true
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
		if expression.Kind == syntax.ExpressionIdentifier && !isLiteralIdentifier(expression.Value) && !skipped[expression.Span] && validNameSpan(file, expression.Span) {
			if strings.HasPrefix(expression.Value, "&") {
				appendUnknownOptionDiagnostic(result, expression.Value, expression.Span)
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
		// receiver expression participates in same-file resolution.
		if dialect == syntax.Vim9 {
			appendMissingEnumValueDiagnostic(result, scope, expression)
			appendObjectMethodThroughClassDiagnostic(result, scope, expression)
		}
		if len(expression.Children) > 0 {
			if expression.Children[0] != nil && expression.Children[0].Kind == syntax.ExpressionIdentifier {
				result.enumValueExempt[expression.Children[0].Span] = true
				result.typeAliasExempt[expression.Children[0].Span] = true
				result.classValueExempt[expression.Children[0].Span] = true
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
	classes := localClasses(file)
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

func localClasses(file *syntax.File) map[string]*syntax.Command {
	classes := make(map[string]*syntax.Command)
	if file == nil {
		return classes
	}
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Dialect == syntax.Vim9 && command.Aggregate != nil && command.Aggregate.Kind == syntax.BlockClass {
			classes[file.Text(command.Aggregate.Name)] = command
		}
	}
	return classes
}

func localClassAliases(file *syntax.File) map[string]string {
	classes := localClasses(file)
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
	if declaration == nil || !functionSymbolKind(declaration.Kind) || declaration.TypeParameterCount > 0 {
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
				appendUnknownOptionDiagnostic(result, expression.Value, expression.Span)
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
			if builtinCall && dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope) && builtin.Name == "exists_compiled" {
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
						if extendMismatch >= 0 {
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
						if !scopeUsesDefTypeRules(scope) || trimmedChecker == "arg_string_list_tuple_or_dict" || trimmedChecker == "arg_list_tuple_dict_blob_or_string" {
							if diagnostic, ok := builtinArgumentDiagnostic(checker, index, actual, argument.Span, dialect == syntax.Vim9); ok {
								result.Diagnostics = append(result.Diagnostics, diagnostic)
								continue
							}
						}
						if expressionContainsInvalidPlus(result, argument) {
							continue
						}
						result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1013", Message: "Argument " + strconv.Itoa(index+1) + ": type mismatch, expected " + expected.display + " but got " + valueTypeDisplay(actual[index]), Span: argument.Span})
						continue
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
	case "true", "false", "null", "null_blob", "null_channel", "null_class", "null_dict", "null_function", "null_job", "null_list", "null_object", "null_partial", "null_string", "null_tuple":
		return true
	default:
		return false
	}
}

func emptySyntaxSpan(span syntax.Span) bool {
	return span.Start >= span.End
}
