package analysis

import (
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
)

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

func collectOverwriteRiskDiagnostics(result *FileAnalysis, commands []syntax.Command) {
	if result == nil || result.File == nil {
		return
	}
	var severity *syntax.DiagnosticSeverity
	if result.configFile {
		s := syntax.DiagnosticHint
		severity = &s
	}
	// E122 historically runs as a complete phase before E174.
	var collect func([]syntax.Command)
	collect = func(list []syntax.Command) {
		for index := range list {
			if !result.analysisStep() {
				return
			}
			command := &list[index]
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
				collect(command.Embedded.Commands)
			}
		}
	}
	collect(commands)

	type userCommandKey struct {
		name   string
		buffer bool
	}
	defined := make(map[userCommandKey]bool)
	for index := range commands {
		if !result.analysisStep() {
			return
		}
		command := &commands[index]
		if !unconditionalAt(commands, result.File.Blocks, index) {
			clear(defined)
			continue
		}
		switch command.Canonical {
		case "command":
			name, span, buffer, ok := syntax.DefinedUserCommand(result.File, command)
			if !ok {
				clear(defined)
				continue
			}
			key := userCommandKey{name: name, buffer: buffer}
			if emptySyntaxSpan(command.Bang) && defined[key] {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E174", Message: "Command " + name + " may already exist in this source; add ! to replace it", Span: span,
					Severity: severity,
				})
			}
			defined[key] = true
		case "delcommand":
			arguments := strings.Fields(result.File.Text(command.Argument))
			if len(arguments) == 1 {
				delete(defined, userCommandKey{name: arguments[0]})
			} else if len(arguments) == 2 && arguments[0] == "-buffer" {
				delete(defined, userCommandKey{name: arguments[1], buffer: true})
			} else {
				clear(defined)
			}
		default:
			clear(defined)
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

// UserCommandDiagnostics warns about abbreviated or unknown user-command calls
// against the complete workspace/runtimepath command index.
// Exact matches always win, matching Vim's user-command lookup rule.
func UserCommandDiagnostics(file *syntax.File, indexedNames []string) []syntax.Diagnostic {
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
	diagnostics := make([]syntax.Diagnostic, 0)
	var diagnose func([]syntax.Command)
	diagnose = func(commands []syntax.Command) {
		for index := range commands {
			command := &commands[index]
			if command.Kind == syntax.CommandUser && !names[command.TypedName] {
				matched := false
				for name := range names {
					if len(command.TypedName) < len(name) && strings.HasPrefix(name, command.TypedName) {
						diagnostics = append(diagnostics, syntax.Diagnostic{
							Code: "vim/E464", Message: "User-defined command " + command.TypedName + " is abbreviated; use the full command name to avoid ambiguity", Span: command.Name,
						})
						matched = true
						break
					}
				}
				if !matched {
					severity := syntax.DiagnosticWarning
					diagnostics = append(diagnostics, syntax.Diagnostic{
						Code: "vim/E492", Message: "Not an editor command: " + command.TypedName,
						Span: command.Name, Severity: &severity,
					})
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
			return vimdata.IsOption(optionName) || vimdata.IsNeovimCompatOption(optionName) || vimdata.IsMacVimCompatOption(optionName)
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
	// Declaration targets are not visible while their initializer (or loop
	// iterable) is being evaluated. Keep the same boundary as reference binding.
	initializers := make(map[syntax.Span]syntax.Span)
	for command := range result.commandScopes {
		if declaration := command.Declaration; declaration != nil && declaration.Initializer != nil && !ordinaryContainerAssignment(command) {
			for _, binding := range declaration.Bindings {
				initializers[binding.Name] = declaration.Initializer.Span
			}
		}
		if loop := command.For; loop != nil && loop.Iterable != nil {
			for _, binding := range loop.Bindings {
				initializers[binding.Name] = loop.Iterable.Span
			}
		}
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
		if scriptArgumentConflict(result, declaration, initializers) {
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
					if initializer := initializers[outer.Span]; initializer.Start <= declaration.Span.Start && declaration.Span.Start < initializer.End {
						continue
					}
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
		if aggregate == nil || scriptArgumentConflict(result, declaration, nil) || !aggregateHasVisibleStaticVariable(result, aggregate, declaration.Name) {
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

func scriptArgumentConflict(result *FileAnalysis, parameter *Declaration, initializers map[syntax.Span]syntax.Span) bool {
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
			if initializer := initializers[declaration.Span]; initializer.Start <= parameter.Span.Start && parameter.Span.Start < initializer.End {
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
	classes := result.classes
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

func collectFuncrefVariableNameDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	for _, declaration := range result.Declarations {
		if declaration == nil || declaration.Kind != SymbolKindVariable && declaration.Kind != SymbolKindConstant ||
			declaration.Type.Name != "func" && declaration.Type.Name != "partial" || strings.HasPrefix(declaration.Name, "&") || funcrefVariableNameAllowed(result.File.Dialect, declaration) {
			continue
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vim/E704", Message: "Funcref variable name must start with a capital: " + declaration.Name, Span: declaration.Span,
		})
	}
}

func ordinaryContainerAssignment(command *syntax.Command) bool {
	if command == nil || command.Dialect != syntax.Legacy || command.Canonical != "let" || command.Declaration == nil || command.Declaration.Target == nil {
		return false
	}
	target := command.Declaration.Target
	return (target.Kind == syntax.ExpressionMember || target.Kind == syntax.ExpressionIndex || target.Kind == syntax.ExpressionSlice) &&
		(len(target.Children) == 0 || !scopeDictionary(target.Children[0]))
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
	seen := make(map[string]*Declaration)
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
		previous := seen[declaration.Name]
		isFunction := functionSymbolKind(declaration.Kind)
		if previous != nil && previous.Scope.Span.Start <= declaration.Span.Start && declaration.Span.End <= previous.Scope.Span.End &&
			!(isFunction && functionSymbolKind(previous.Kind)) &&
			!(declaration.Kind == SymbolKindTypeAlias && previous.Kind == SymbolKindTypeAlias) {
			if importRedefinitions[declaration.Name] {
				seen[declaration.Name] = declaration
				continue
			}
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E1041", Message: `Redefining script item: "` + declaration.Name + `"`, Span: declaration.Span})
		}
		seen[declaration.Name] = declaration
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
	// A script-level deletion invalidates proof that an old legacy function
	// still exists, including conditional deletions whose branch is unknown.
	// Deletions in deferred function bodies do not affect this source sequence.
	var deletions []*syntax.Expression
	for index := range result.File.Commands {
		command := &result.File.Commands[index]
		if command.Canonical == "delfunction" && !syntax.CommandInsideFunction(command, result.File.Blocks) {
			for _, target := range command.Targets {
				if target != nil && target.Kind == syntax.ExpressionIdentifier {
					deletions = append(deletions, target)
				}
			}
		}
	}
	for _, declaration := range result.Declarations {
		if declaration == nil || declaration.Scope == nil || !eligible[declaration.Span] {
			continue
		}
		if existing := resolve(declaration.Scope, declaration.Name, declaration.Span.Start, false, nil); existing != nil &&
			!(declaration.Scope == result.Root && functionSymbolKind(declaration.Kind) && !functionSymbolKind(existing.Kind)) {
			deleted := false
			if existing.Scope == result.Root && functionSymbolKind(existing.Kind) && !eligible[existing.Span] {
				for _, target := range deletions {
					if existing.Span.End < target.Span.Start && target.Span.End < declaration.Span.Start && resolvedNameEqual(target.Value, existing.Name) {
						deleted = true
						break
					}
				}
			}
			if deleted {
				continue
			}
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
	if isUnderGuiGuard(result, scope, span) {
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
	if isUnderGuiGuard(result, scope, span) {
		severity := syntax.DiagnosticWarning
		diag.Severity = &severity
	}
	result.Diagnostics = append(result.Diagnostics, diag)
}

func isUnderGuiGuard(result *FileAnalysis, scope *Scope, span syntax.Span) bool {
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
					if isGuiCondition(expr) {
						return true
					}
				}
			}
		}
	}
	return false
}

func isGuiCondition(expr *syntax.Expression) bool {
	if expr == nil {
		return false
	}
	switch expr.Kind {
	case syntax.ExpressionParenthesized:
		for _, child := range expr.Children {
			if isGuiCondition(child) {
				return true
			}
		}
		return false
	case syntax.ExpressionCall:
		return isHasGuiCall(expr)
	case syntax.ExpressionBinary:
		op := expr.Value
		if (op == "&&" || op == "and") && len(expr.Children) == 2 {
			return isGuiCondition(expr.Children[0]) || isGuiCondition(expr.Children[1])
		}
		if (op == "||" || op == "or") && len(expr.Children) == 2 {
			return isGuiCondition(expr.Children[0]) && isGuiCondition(expr.Children[1])
		}
		if (op == "==" || op == "!=") && len(expr.Children) == 2 {
			return isComparisonToHasFeature(expr)
		}
	}
	return false
}

func isHasGuiCall(expr *syntax.Expression) bool {
	if expr == nil || expr.Kind != syntax.ExpressionCall {
		return false
	}
	if len(expr.Children) >= 2 {
		callee := expr.Children[0]
		arg := expr.Children[1]
		if callee != nil && callee.Kind == syntax.ExpressionIdentifier && callee.Value == "has" && arg != nil && arg.Kind == syntax.ExpressionString {
			feature, ok := unquoteFeatureString(arg.Value)
			if ok && strings.EqualFold(feature, "gui_running") {
				return true
			}
		}
	}
	if expr.Value == "->" && len(expr.Children) == 2 {
		for _, child := range expr.Children {
			if child.Kind == syntax.ExpressionCall && isHasGuiCall(child) {
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
		if !isHasGuiCall(callExpr) || constExpr == nil || constExpr.Kind != syntax.ExpressionNumber {
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
	if vimdata.IsOption(name) || vimdata.IsNeovimCompatOption(name) || vimdata.IsMacVimCompatOption(name) || vimdata.IsTerminalOptionName(name) {
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
