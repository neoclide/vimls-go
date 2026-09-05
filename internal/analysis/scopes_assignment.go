package analysis

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
)

func directAssignmentTarget(command *syntax.Command) *syntax.Expression {
	if command == nil {
		return nil
	}
	if command.Declaration != nil && command.Declaration.Target != nil && command.Declaration.Initializer != nil {
		return command.Declaration.Target
	}
	for _, expression := range command.Expressions {
		if expression != nil && expression.Kind == syntax.ExpressionAssignment && expression.Value == "=" && len(expression.Children) > 0 {
			return expression.Children[0]
		}
	}
	return nil
}

func immediateLockedAssignment(file *syntax.File, previous, current *syntax.Command) *syntax.Expression {
	if file == nil || previous == nil || current == nil {
		return nil
	}
	locked := (*syntax.Expression)(nil)
	if previous.Canonical == "lockvar" && file.Text(previous.Count) == "0" && len(previous.Targets) == 1 {
		locked = previous.Targets[0]
	} else if previous.Canonical == "final" && previous.Declaration != nil {
		locked = previous.Declaration.Target
		if locked == nil || !strings.Contains(locked.Value, ":") {
			return nil
		}
	}
	assigned := directAssignmentTarget(current)
	if locked == nil || assigned == nil || locked.Kind != syntax.ExpressionIdentifier || assigned.Kind != syntax.ExpressionIdentifier || locked.Value != assigned.Value {
		return nil
	}
	return assigned
}

func immediateLockedValueDiagnostic(file *syntax.File, previous, current *syntax.Command) (syntax.Diagnostic, bool) {
	if file == nil || previous == nil || current == nil || previous.Canonical != "lockvar" || len(previous.Targets) != 1 {
		return syntax.Diagnostic{}, false
	}
	locked := previous.Targets[0]
	assigned := directAssignmentTarget(current)
	if locked == nil || assigned == nil || expressionContainsMissing(locked) || expressionContainsMissing(assigned) {
		return syntax.Diagnostic{}, false
	}
	if locked.Kind == syntax.ExpressionIdentifier && assigned.Kind == syntax.ExpressionIdentifier && locked.Value == assigned.Value {
		return syntax.Diagnostic{Code: "vim/E741", Message: "Value is locked: " + assigned.Value, Span: assigned.Span}, true
	}
	if previous.Count.Start != previous.Count.End || locked.Kind != syntax.ExpressionIdentifier ||
		(assigned.Kind != syntax.ExpressionMember && assigned.Kind != syntax.ExpressionIndex && assigned.Kind != syntax.ExpressionSlice) ||
		len(assigned.Children) == 0 || assigned.Children[0] == nil || assigned.Children[0].Kind != syntax.ExpressionIdentifier ||
		assigned.Children[0].Value != locked.Value {
		return syntax.Diagnostic{}, false
	}
	return syntax.Diagnostic{Code: "vim/E741", Message: "Value is locked: " + file.Text(assigned.Span), Span: assigned.Span}, true
}

func immediateLegacyNumberMemberDiagnostic(file *syntax.File, previous, current *syntax.Command) (syntax.Diagnostic, bool) {
	if file == nil || previous == nil || current == nil || previous.Dialect != syntax.Legacy || current.Dialect != syntax.Legacy ||
		previous.Declaration == nil || previous.Declaration.Target == nil || previous.Declaration.Initializer == nil ||
		previous.Declaration.Target.Kind != syntax.ExpressionIdentifier || previous.Declaration.Initializer.Kind != syntax.ExpressionNumber {
		return syntax.Diagnostic{}, false
	}
	target := directAssignmentTarget(current)
	if target == nil || target.Kind != syntax.ExpressionMember || len(target.Children) != 1 || target.Children[0] == nil ||
		target.Children[0].Kind != syntax.ExpressionIdentifier || target.Children[0].Value != previous.Declaration.Target.Value ||
		file.Text(target.Operator) != "." || target.Value == "" || expressionContainsMissing(target) {
		return syntax.Diagnostic{}, false
	}
	return syntax.Diagnostic{
		Code: "vim/E1203", Message: "Dot not allowed after a number: " + file.Text(current.Argument), Span: target.Span,
	}, true
}

// Only adjacent assignments prove a legacy receiver's value: a stored type
// fact alone cannot rule out intervening dynamic replacement. Follow literal
// tuple indexes, never mutable containers or runtime-computed indexes.
func immediateLegacyTupleMutationDiagnostic(previous, current *syntax.Command) (syntax.Diagnostic, bool) {
	if previous == nil || current == nil || previous.Dialect != syntax.Legacy || current.Dialect != syntax.Legacy ||
		previous.Declaration == nil || previous.Declaration.Target == nil || previous.Declaration.Initializer == nil ||
		previous.Declaration.Target.Kind != syntax.ExpressionIdentifier {
		return syntax.Diagnostic{}, false
	}
	if current.Declaration != nil && expressionContainsMissing(current.Declaration.Initializer) {
		return syntax.Diagnostic{}, false
	}
	target := directAssignmentTarget(current)
	if target == nil || target.Kind != syntax.ExpressionIndex || len(target.Children) != 2 || expressionContainsMissing(target) {
		return syntax.Diagnostic{}, false
	}
	var literalReceiver func(*syntax.Expression) *syntax.Expression
	literalReceiver = func(expression *syntax.Expression) *syntax.Expression {
		if expression == nil {
			return nil
		}
		if expression.Kind == syntax.ExpressionIdentifier && expression.Value == previous.Declaration.Target.Value {
			return previous.Declaration.Initializer
		}
		if expression.Kind == syntax.ExpressionIndex && len(expression.Children) == 2 {
			receiver := literalReceiver(expression.Children[0])
			index, ok := staticTupleIndex(expression.Children[1])
			if receiver != nil && receiver.Kind == syntax.ExpressionTuple && ok {
				if index < 0 {
					index += len(receiver.Children)
				}
				if index >= 0 && index < len(receiver.Children) {
					return receiver.Children[index]
				}
			}
		}
		return nil
	}
	index, ok := staticTupleIndex(target.Children[1])
	receiver := literalReceiver(target.Children[0])
	if !ok || receiver == nil || receiver.Kind != syntax.ExpressionTuple || expressionContainsMissing(receiver) {
		return syntax.Diagnostic{}, false
	}
	if index < 0 {
		index += len(receiver.Children)
	}
	if index < 0 || index >= len(receiver.Children) {
		return syntax.Diagnostic{}, false
	}
	return syntax.Diagnostic{Code: "vim/E1532", Message: "Cannot modify a tuple", Span: target.Span}, true
}

func immediateLockedItemDiagnostic(result *FileAnalysis, scope *Scope, previous, current *syntax.Command) (syntax.Diagnostic, bool) {
	if result == nil || result.File == nil || scope == nil || previous == nil || current == nil {
		return syntax.Diagnostic{}, false
	}
	file := result.File
	assigned := directAssignmentTarget(current)
	if assigned == nil || assigned.Kind != syntax.ExpressionMember && assigned.Kind != syntax.ExpressionIndex {
		return syntax.Diagnostic{}, false
	}
	if scopeUsesDefTypeRules(scope) && previous.Canonical == "lockvar" && previous.Count.Start == previous.Count.End && len(previous.Targets) == 1 &&
		len(assigned.Children) > 0 && file.Text(previous.Targets[0].Span) == file.Text(assigned.Span) {
		switch resolvedExpressionType(result, scope, assigned.Children[0]).Name {
		case "dict":
			return syntax.Diagnostic{Code: "vim/E1121", Message: "Cannot change dict item", Span: assigned.Span}, true
		case "list":
			if assigned.Kind == syntax.ExpressionIndex {
				return syntax.Diagnostic{Code: "vim/E1119", Message: "Cannot change locked list item", Span: assigned.Span}, true
			}
		}
	}
	if !scopeUsesDefTypeRules(scope) || previous.Canonical != "const" || previous.Declaration == nil || previous.Declaration.Target == nil ||
		previous.Declaration.Target.Kind != syntax.ExpressionIdentifier || previous.Declaration.Initializer == nil ||
		len(assigned.Children) == 0 || assigned.Children[0] == nil || assigned.Children[0].Kind != syntax.ExpressionIdentifier ||
		assigned.Children[0].Value != previous.Declaration.Target.Value {
		return syntax.Diagnostic{}, false
	}
	if previous.Declaration.Initializer.Kind == syntax.ExpressionList && assigned.Kind == syntax.ExpressionIndex && len(assigned.Children) > 1 {
		index, ok := staticTupleIndex(assigned.Children[1])
		if !ok {
			return syntax.Diagnostic{}, false
		}
		if index < 0 {
			index += len(previous.Declaration.Initializer.Children)
		}
		if index >= 0 && index < len(previous.Declaration.Initializer.Children) {
			return syntax.Diagnostic{Code: "vim/E1119", Message: "Cannot change locked list item", Span: assigned.Span}, true
		}
		return syntax.Diagnostic{Code: "vim/E1118", Message: "Cannot change locked list", Span: assigned.Span}, true
	}
	if previous.Declaration.Initializer.Kind != syntax.ExpressionDictionary {
		return syntax.Diagnostic{}, false
	}
	key := assigned.Value
	if assigned.Kind == syntax.ExpressionIndex {
		if len(assigned.Children) < 2 {
			return syntax.Diagnostic{}, false
		}
		var ok bool
		key, ok = syntax.StaticDictionaryIndexKey(assigned.Children[1])
		if !ok {
			return syntax.Diagnostic{}, false
		}
	}
	for index := 0; index+1 < len(previous.Declaration.Initializer.Children); index += 2 {
		candidate, ok := syntax.StaticDictionaryKey(previous.Declaration.Initializer.Children[index], previous.Dialect)
		if !ok {
			return syntax.Diagnostic{}, false
		}
		if candidate == key {
			return syntax.Diagnostic{Code: "vim/E1121", Message: "Cannot change dict item", Span: assigned.Span}, true
		}
	}
	return syntax.Diagnostic{Code: "vim/E1120", Message: "Cannot change dict", Span: assigned.Span}, true
}

func illegalVariableNameAssignmentDiagnostic(target *syntax.Expression) (syntax.Diagnostic, bool) {
	if target == nil {
		return syntax.Diagnostic{}, false
	}
	if scopeDictionary(target) {
		return syntax.Diagnostic{Code: "vim/E461", Message: "Illegal variable name: ", Span: target.Span}, true
	}
	if target.Kind != syntax.ExpressionIndex || len(target.Children) < 2 || !scopeDictionary(target.Children[0]) {
		return syntax.Diagnostic{}, false
	}
	key, ok := syntax.StaticDictionaryIndexKey(target.Children[1])
	if !ok || validScopeVariableName(key) {
		return syntax.Diagnostic{}, false
	}
	return syntax.Diagnostic{Code: "vim/E461", Message: "Illegal variable name: " + key, Span: target.Children[1].Span}, true
}

// collectAssignmentDiagnostics reports statically provable assignment-target
// errors. Dynamic targets deliberately remain opaque here.
func collectAssignmentDiagnostics(result *FileAnalysis, commands []syntax.Command, parent *Scope) {
	if result == nil || result.File == nil {
		return
	}
	var expressionUsesExecute func(*syntax.Expression) bool
	var commandsUseExecute func([]syntax.Command) bool
	expressionUsesExecute = func(expression *syntax.Expression) bool {
		if expression == nil {
			return false
		}
		if expression.Kind == syntax.ExpressionCall && len(expression.Children) > 0 && expression.Children[0] != nil &&
			expression.Children[0].Kind == syntax.ExpressionIdentifier && expression.Children[0].Value == "execute" {
			return true
		}
		if expression.LambdaBody != nil && commandsUseExecute(expression.LambdaBody.Commands) {
			return true
		}
		return slices.ContainsFunc(expression.Children, expressionUsesExecute)
	}
	commandsUseExecute = func(commands []syntax.Command) bool {
		for index := range commands {
			command := &commands[index]
			if command.Canonical == "execute" {
				return true
			}
			if slices.ContainsFunc(command.Expressions, expressionUsesExecute) {
				return true
			}
			if slices.ContainsFunc(command.Targets, expressionUsesExecute) || command.For != nil && expressionUsesExecute(command.For.Iterable) ||
				command.Mapping != nil && expressionUsesExecute(command.Mapping.RHSExpression) {
				return true
			}
			if command.Declaration != nil && expressionUsesExecute(command.Declaration.Initializer) {
				return true
			}
			if command.Embedded != nil && commandsUseExecute(command.Embedded.Commands) {
				return true
			}
		}
		return false
	}
	dynamicVariableCreation := commandsUseExecute(result.File.Commands)
	var previous *syntax.Command
	var previousScope *Scope
	recentGlobalAssignments := make(map[string]*syntax.Expression)
	for index := range commands {
		command := &commands[index]
		scope := result.commandScopes[command]
		if scope == nil {
			scope = parent
		}
		if command.Declaration != nil && command.Declaration.Assignment.Start < command.Declaration.Assignment.End {
			if diagnostic, ok := illegalVariableNameAssignmentDiagnostic(command.Declaration.Target); ok &&
				!syntaxDiagnosticOverlaps(result.File.Diagnostics, diagnostic.Span) && !syntaxDiagnosticOverlaps(result.Diagnostics, diagnostic.Span) {
				result.Diagnostics = append(result.Diagnostics, diagnostic)
			}
		}
		if previousScope != nil && previousScope != scope {
			clear(recentGlobalAssignments)
		}
		if previousScope == scope {
			if diagnostic, ok := immediateLegacyTupleMutationDiagnostic(previous, command); ok &&
				!syntaxDiagnosticOverlaps(result.File.Diagnostics, diagnostic.Span) && !syntaxDiagnosticOverlaps(result.Diagnostics, diagnostic.Span) {
				result.Diagnostics = append(result.Diagnostics, diagnostic)
			}
			if diagnostic, ok := immediateLegacyNumberMemberDiagnostic(result.File, previous, command); ok &&
				!syntaxDiagnosticOverlaps(result.File.Diagnostics, diagnostic.Span) {
				blocked := false
				for _, existing := range result.Diagnostics {
					if existing.Span.Start <= diagnostic.Span.End && existing.Span.End >= diagnostic.Span.Start && existing.Code != "vim/E1017" {
						blocked = true
						break
					}
				}
				if !blocked {
					filtered := result.Diagnostics[:0]
					for _, existing := range result.Diagnostics {
						if existing.Code == "vim/E1017" && existing.Span.Start >= diagnostic.Span.Start && existing.Span.End <= diagnostic.Span.End {
							continue
						}
						filtered = append(filtered, existing)
					}
					result.Diagnostics = append(filtered, diagnostic)
				}
			}
			if command.Dialect == syntax.Legacy {
				assigned := (*syntax.Expression)(nil)
				if previous.Canonical == "lockvar" {
					assigned = immediateLockedAssignment(result.File, previous, command)
				}
				if assigned != nil && !syntaxDiagnosticOverlaps(result.File.Diagnostics, assigned.Span) {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E1122", Message: "Variable is locked: " + assigned.Value, Span: assigned.Span,
					})
				} else if diagnostic, ok := immediateLockedValueDiagnostic(result.File, previous, command); ok && !syntaxDiagnosticOverlaps(result.File.Diagnostics, diagnostic.Span) {
					result.Diagnostics = append(result.Diagnostics, diagnostic)
				}
			}
			if command.Dialect == syntax.Vim9 {
				if assigned := immediateLockedAssignment(result.File, previous, command); assigned != nil && !syntaxDiagnosticOverlaps(result.File.Diagnostics, assigned.Span) {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E1122", Message: "Variable is locked: " + assigned.Value, Span: assigned.Span,
					})
				}
			}
			if diagnostic, ok := immediateLockedItemDiagnostic(result, scope, previous, command); ok &&
				!syntaxDiagnosticOverlaps(result.File.Diagnostics, diagnostic.Span) {
				result.Diagnostics = append(result.Diagnostics, diagnostic)
			}
		}
		previous, previousScope = command, scope
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
				invalidIndex := false
				if target != nil && (target.Kind == syntax.ExpressionIndex || target.Kind == syntax.ExpressionSlice) && len(target.Children) > 1 && !expressionContainsMissing(target) {
					receiverExpression := target.Children[0]
					if receiverExpression != nil && receiverExpression.Kind == syntax.ExpressionIdentifier {
						if initializer := recentGlobalAssignments[receiverExpression.Value]; initializer != nil {
							receiver := result.TypeOf(initializer)
							for _, index := range target.Children[1:] {
								actual := resolvedExpressionType(result, scope, index)
								if index != nil && index.Kind == syntax.ExpressionIdentifier {
									if initializer := recentGlobalAssignments[index.Value]; initializer != nil {
										actual = result.TypeOf(initializer)
									}
								}
								if isUnknownType(actual) {
									continue
								}
								expected := ""
								if receiver.Name == "list" && actual.Name != "number" {
									expected = "number"
								} else if receiver.Name == "dict" && (actual.Name == "blob" || actual.Name == "list" || actual.Name == "dict") {
									expected = "string"
								}
								if expected != "" {
									result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
										Code: "vim/E1029", Message: "Expected " + expected + " but got " + actual.Name, Span: index.Span,
									})
									invalidIndex = true
									break
								}
							}
						}
					}
				}
				if invalidIndex {
					continue
				}
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
		if command.Canonical == "unlet" && command.Dialect == syntax.Vim9 {
			for _, target := range command.Targets {
				if appendCannotIndexRuntimeDiagnostic(result, scope, target) {
					break
				}
			}
		}
		if (command.Canonical == "lockvar" || command.Canonical == "unlockvar") && command.Dialect == syntax.Vim9 {
			for _, target := range command.Targets {
				before := len(result.Diagnostics)
				appendProtectedVariableAccessDiagnostic(result, scope, target)
				protectedVariableAccess := false
				if target != nil {
					for _, diagnostic := range result.Diagnostics {
						if diagnostic.Code == "vim/E1333" && diagnostic.Span.Start >= target.Span.Start && diagnostic.Span.End <= target.Span.End {
							protectedVariableAccess = true
							break
						}
					}
				}
				if protectedVariableAccess {
					break
				}
				if appendClassVariableLockDiagnostic(result, scope, target) {
					break
				}
				if className, memberName, ok := nonWritableClassMemberAssignment(result, scope, target); ok {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E1335", Message: `Variable "` + memberName + `" in class "` + className + `" is not writable`, Span: memberNameSpan(result.File, target),
					})
					break
				}
				if appendObjectVariableLockDiagnostic(result, scope, target) {
					break
				}
				if len(result.Diagnostics) != before {
					break
				}
				if target == nil || target.Kind != syntax.ExpressionIdentifier || target.Value == "this" || syntaxDiagnosticOverlaps(result.File.Diagnostics, target.Span) {
					continue
				}
				blocked := false
				for _, diagnostic := range result.Diagnostics {
					if diagnostic.Span.Start <= target.Span.End && diagnostic.Span.End >= target.Span.Start &&
						diagnostic.Code != "vim/E121" && diagnostic.Code != "vim/E1001" {
						blocked = true
						break
					}
				}
				if blocked {
					continue
				}
				declaration := resolve(scope, target.Value, target.Span.Start, false, nil)
				declared := declaration != nil
				if !declared {
					for current := scope; current != nil && !declared; current = current.Parent {
						for _, candidate := range current.Declarations {
							if candidate.Name == target.Value {
								declared = true
								break
							}
						}
					}
				}
				if !dynamicVariableCreation && !declared && !strings.Contains(target.Value, ":") && !isLiteralIdentifier(target.Value) {
					filtered := result.Diagnostics[:0]
					for _, diagnostic := range result.Diagnostics {
						if diagnostic.Span == target.Span && (diagnostic.Code == "vim/E121" || diagnostic.Code == "vim/E1001") {
							continue
						}
						filtered = append(filtered, diagnostic)
					}
					result.Diagnostics = filtered
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E1246", Message: "Cannot find variable to (un)lock: " + target.Value, Span: target.Span,
					})
					break
				}
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
		recordedGlobalAssignment := false
		for _, expression := range command.Expressions {
			if expression == nil || expression.Kind != syntax.ExpressionAssignment || expression.Value != "=" || len(expression.Children) < 2 ||
				expressionContainsMissing(expression) || expression.Children[0] == nil || expression.Children[0].Kind != syntax.ExpressionIdentifier ||
				!strings.HasPrefix(expression.Children[0].Value, "g:") || isUnknownType(result.TypeOf(expression.Children[1])) {
				continue
			}
			initializer := expression.Children[1]
			switch initializer.Kind {
			case syntax.ExpressionBlob, syntax.ExpressionDictionary, syntax.ExpressionList, syntax.ExpressionNumber, syntax.ExpressionString:
			default:
				continue
			}
			recentGlobalAssignments[expression.Children[0].Value] = initializer
			recordedGlobalAssignment = true
			break
		}
		if !recordedGlobalAssignment {
			clear(recentGlobalAssignments)
		}
		if command.Embedded != nil {
			collectAssignmentDiagnostics(result, command.Embedded.Commands, scope)
		}
	}
}

func appendObjectVariableLockDiagnostic(result *FileAnalysis, scope *Scope, target *syntax.Expression) bool {
	if result == nil || result.File == nil || scope == nil || target == nil || target.Kind != syntax.ExpressionMember ||
		len(target.Children) != 1 || target.Children[0] == nil || result.File.Text(target.Operator) != "." || target.Value == "" ||
		expressionContainsMissing(target) || syntaxDiagnosticOverlaps(result.File.Diagnostics, target.Span) || syntaxDiagnosticOverlaps(result.Diagnostics, target.Span) {
		return false
	}
	_, class, _, found := objectAggregateReceiver(result, scope, target.Children[0], make(map[*syntax.Expression]bool))
	if !found || class == nil || class.Aggregate == nil || class.Aggregate.Kind != syntax.BlockClass {
		return false
	}
	owner, _, _, found := classObjectVariableOwner(result, class, target.Value)
	if !found || owner == nil || owner.Aggregate == nil {
		return false
	}
	file := result.File
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1391", Message: `Cannot (un)lock variable "` + file.Text(target.Span) + `" in class "` + file.Text(owner.Aggregate.Name) + `"`, Span: target.Span,
	})
	return true
}

func appendClassVariableLockDiagnostic(result *FileAnalysis, scope *Scope, target *syntax.Expression) bool {
	if result == nil || result.File == nil || scope == nil || target == nil || expressionContainsMissing(target) ||
		syntaxDiagnosticOverlaps(result.File.Diagnostics, target.Span) || syntaxDiagnosticOverlaps(result.Diagnostics, target.Span) {
		return false
	}
	file := result.File
	var class *syntax.Command
	name := ""
	switch target.Kind {
	case syntax.ExpressionIdentifier:
		if target.Value == "" {
			return false
		}
		class = directMethodAggregate(file, scope)
		name = target.Value
	case syntax.ExpressionMember:
		if len(target.Children) != 1 || target.Children[0] == nil || target.Children[0].Kind != syntax.ExpressionIdentifier ||
			file.Text(target.Operator) != "." || target.Value == "" {
			return false
		}
		receiver := target.Children[0]
		declaration := resolve(scope, receiver.Value, receiver.Span.Start, false, nil)
		if declaration == nil {
			return false
		}
		className := ""
		switch declaration.Kind {
		case SymbolKindClass:
			className = declaration.Name
		case SymbolKindTypeAlias:
			className = result.classAliases[declaration.Name]
		}
		class = result.classes[className]
		name = target.Value
	default:
		return false
	}
	if class == nil || class.Aggregate == nil || class.Aggregate.Kind != syntax.BlockClass {
		return false
	}
	owner, declarationSpan, found := classStaticVariableOwner(result, class, name)
	if !found || owner == nil || owner.Aggregate == nil {
		return false
	}
	if target.Kind == syntax.ExpressionIdentifier {
		if declaration := resolve(scope, target.Value, target.Span.Start, false, nil); declaration != nil && declaration.Span != declarationSpan {
			return false
		}
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1392", Message: `Cannot (un)lock class variable "` + file.Text(target.Span) + `" in class "` + file.Text(owner.Aggregate.Name) + `"`, Span: target.Span,
	})
	return true
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
		if dialect == syntax.Legacy && target != nil && (target.Kind == syntax.ExpressionMember || target.Kind == syntax.ExpressionIndex || target.Kind == syntax.ExpressionSlice) &&
			len(target.Children) > 0 && target.Children[0] != nil && target.Children[0].Kind == syntax.ExpressionIdentifier {
			receiver := target.Children[0]
			if receiver.Value == "v:event" || (receiver.Value == "a:000" && isReadOnlyLegacyArgumentTarget(scope, receiver)) {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E742", Message: "Cannot change value of " + receiver.Value, Span: target.Span,
				})
			}
		}
		if dialect == syntax.Legacy && expression.Value == "=" && target != nil && target.Kind == syntax.ExpressionIdentifier {
			if variable, ok := vimdata.LookupVariable(target.Value); ok && variable.Flags&vimdata.VariableReadOnly == 0 {
				expected := builtinVariableValueType(variable)
				actual := result.TypeOf(expression.Children[1])
				if !isUnknownType(expected) && expected.Name != "string" && expected.Name != "number" && !isUnknownType(actual) && expected.Name != actual.Name {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vim/E963", Message: "Setting v:" + strings.TrimPrefix(target.Value, "v:") + " to value with wrong type", Span: expression.Children[1].Span,
					})
				}
			}
		}
		appendDotNotAllowedAfterNumberDiagnostic(result, scope, expression, target)
		if dialect == syntax.Vim9 {
			appendProtectedVariableAccessDiagnostic(result, scope, target)
		}
		protectedVariableAccess := false
		if target != nil {
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1333" && diagnostic.Span.Start >= target.Span.Start && diagnostic.Span.End <= target.Span.End {
					protectedVariableAccess = true
					break
				}
			}
		}
		if className, memberName, ok := nonWritableClassMemberAssignment(result, scope, target); dialect == syntax.Vim9 && !protectedVariableAccess && ok {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1335", Message: `Variable "` + memberName + `" in class "` + className + `" is not writable`, Span: memberNameSpan(result.File, target),
			})
		} else if enumName, memberName, ok := enumAssignmentTarget(result, scope, target); !protectedVariableAccess && ok {
			if target.Children[0].Kind == syntax.ExpressionMember && !scopeContainsDef(scope) {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1335", Message: `Variable "` + memberName + `" in class "` + enumName + `" is not writable`, Span: memberNameSpan(result.File, target),
				})
			} else if target.Children[0].Kind != syntax.ExpressionMember || !scopeWithinVim9Enum(result.File, scope, enumName) {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vim/E1423", Message: "Enum value \"" + enumName + "." + memberName + "\" cannot be modified", Span: target.Span,
				})
			}
		} else if className, memberName, ok := readOnlyClassMemberAssignment(result, scope, target); !protectedVariableAccess && ok {
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
			(dialect == syntax.Legacy || dialect == syntax.Vim9 && !scopeUsesDefTypeRules(scope) && expression.Value != "=") &&
			resolvedExpressionType(result, scope, target.Children[0]).Name == "string" {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E689", Message: "Index not allowed after a string: " + result.File.Text(expression.Span), Span: target.Span,
			})
			if dialect == syntax.Vim9 {
				return
			}
		}
		if dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope) && expression.Value != "=" && target != nil && target.Kind == syntax.ExpressionSlice && !expressionContainsMissing(expression) {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E1183", Message: "Cannot use a range with an assignment operator: " + result.File.Text(expression.Span), Span: target.Span,
			})
			return
		}
		cannotIndex := false
		if dialect == syntax.Vim9 {
			receiver := invalidAssignmentReceiver(result, scope, target)
			if !scopeUsesDefTypeRules(scope) || receiver == nil || receiver.Kind != syntax.ExpressionIdentifier {
				cannotIndex = appendCannotIndexRuntimeDiagnostic(result, scope, target)
			}
		}
		if dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope) && !cannotIndex && !sliceAssignmentNeedsE1165(result, scope, expression) {
			appendIndexableAssignmentDiagnostic(result, scope, target)
		}
	}
	for _, child := range expression.Children {
		collectAssignmentExpressionDiagnostics(result, scope, child, dialect)
	}
}

func appendCannotIndexRuntimeDiagnostic(result *FileAnalysis, scope *Scope, target *syntax.Expression) bool {
	receiver := invalidAssignmentReceiver(result, scope, target)
	if receiver == nil || resolvedExpressionType(result, scope, receiver).Name != "string" {
		return false
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1148", Message: "Cannot index a string", Span: receiver.Span,
	})
	return true
}

func appendDotNotAllowedAfterNumberDiagnostic(result *FileAnalysis, scope *Scope, assignment, target *syntax.Expression) {
	if result == nil || result.File == nil || scope == nil || assignment == nil || target == nil || scopeUsesDefTypeRules(scope) ||
		target.Kind != syntax.ExpressionMember || len(target.Children) != 1 || target.Children[0] == nil || result.File.Text(target.Operator) != "." ||
		target.Value == "" || expressionContainsMissing(target) || syntaxDiagnosticOverlaps(result.File.Diagnostics, target.Span) ||
		syntaxDiagnosticOverlaps(result.Diagnostics, target.Span) {
		return
	}
	receiver := target.Children[0]
	if resolvedExpressionType(result, scope, receiver).Name != "number" && !uninitializedAnyVariable(result, scope, receiver) {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vim/E1203", Message: "Dot not allowed after a number: " + result.File.Text(assignment.Span), Span: target.Span,
	})
}

func uninitializedAnyVariable(result *FileAnalysis, scope *Scope, expression *syntax.Expression) bool {
	if result == nil || result.File == nil || scope == nil || expression == nil || expression.Kind != syntax.ExpressionIdentifier {
		return false
	}
	declaration := resolve(scope, expression.Value, expression.Span.Start, false, nil)
	if declaration == nil {
		return false
	}
	for index := range result.File.Commands {
		command := &result.File.Commands[index]
		if command.Dialect != syntax.Vim9 || command.Canonical != "var" || command.Declaration == nil || command.Declaration.Initializer != nil {
			continue
		}
		for _, binding := range command.Declaration.Bindings {
			if binding.Name == declaration.Span && binding.ParsedType != nil && convertSyntaxType(binding.ParsedType).Name == ValueTypeAny {
				return true
			}
		}
	}
	return false
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

func nonWritableClassMemberAssignment(result *FileAnalysis, scope *Scope, target *syntax.Expression) (string, string, bool) {
	if result == nil || result.File == nil || scope == nil || target == nil || target.Kind != syntax.ExpressionMember ||
		len(target.Children) != 1 || target.Children[0] == nil || target.Value == "" || result.File.Text(target.Operator) != "." ||
		expressionContainsMissing(target) || syntaxDiagnosticOverlaps(result.File.Diagnostics, target.Span) {
		return "", "", false
	}
	file := result.File
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1333" && diagnostic.Span.Start >= target.Span.Start && diagnostic.Span.End <= target.Span.End {
			return "", "", false
		}
	}
	owner := (*syntax.Command)(nil)
	objectVariable := false
	if receiver := target.Children[0]; receiver.Kind == syntax.ExpressionIdentifier {
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
				if member, _, found := aggregateVariableBinding(file, aggregate, target.Value, true); found && !commandHasModifier(member, "public") {
					owner = aggregate
				}
			}
		}
	}
	if owner == nil {
		_, aggregate, _, found := objectAggregateReceiver(result, scope, target.Children[0], make(map[*syntax.Expression]bool))
		if !found || aggregate == nil || aggregate.Aggregate == nil {
			return "", "", false
		}
		switch aggregate.Aggregate.Kind {
		case syntax.BlockClass:
			candidate, member, _, exists := classObjectVariableOwner(result, aggregate, target.Value)
			if exists && !commandHasModifier(member, "public") {
				owner = candidate
				objectVariable = true
			}
		case syntax.BlockEnum:
			if target.Value == "name" || target.Value == "ordinal" {
				owner = aggregate
				objectVariable = true
			} else if member, _, exists := aggregateVariableBinding(file, aggregate, target.Value, false); exists && !commandHasModifier(member, "public") {
				owner = aggregate
				objectVariable = true
			}
			if scopeContainsDef(scope) {
				return "", "", false
			}
			if owner == nil {
				return "", "", false
			}
		}
	}
	if owner == nil || owner.Aggregate == nil {
		return "", "", false
	}
	current := enclosingAggregateCommand(file, scope)
	allowed := current == owner
	if objectVariable && !allowed && owner.Aggregate.Kind == syntax.BlockClass && current != nil && current.Aggregate != nil && current.Aggregate.Kind == syntax.BlockClass {
		for seen := make(map[*syntax.Command]bool); current != nil && !seen[current]; current = extendedClass(file, result.classes, current) {
			seen[current] = true
			if current == owner {
				allowed = true
				break
			}
		}
	}
	if allowed {
		return "", "", false
	}
	return file.Text(owner.Aggregate.Name), target.Value, true
}

func readOnlyClassMemberAssignment(result *FileAnalysis, scope *Scope, target *syntax.Expression) (string, string, bool) {
	if result == nil || result.File == nil || scope == nil || target == nil {
		return "", "", false
	}
	file := result.File
	classes := localAggregates(file, syntax.BlockClass)
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
		if current.Aggregate == nil {
			continue
		}
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
