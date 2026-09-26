package analysis

import (
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// guardedCompletionType derives a request-local type fact from the immediate
// Vim9 if/elseif branch containing the completion cursor. It deliberately
// does not participate in full analysis or declaration inference.
func (query *CompletionTypes) guardedCompletionType(declaration *Declaration, scope *Scope, offset int, ordinary ValueType) (ValueType, bool) {
	for current := scope; current != nil; current = current.Parent {
		if current.Kind != syntax.BlockIf || !completionGuardContainsScope(scope, current) {
			continue
		}
		typ, guarded, guardCommand, ok := query.branchCompletionGuard(current, offset)
		if !ok || guarded != declaration || !query.completionGuardRemainsValid(current, guardCommand, declaration, offset) {
			continue
		}
		if ordinary.Name == typ.Name {
			return ordinary, true
		}
		return typ, true
	}
	return UnknownValueType, false
}

func completionGuardContainsScope(scope, guard *Scope) bool {
	for current := scope; current != nil; current = current.Parent {
		if current == guard {
			return true
		}
		if current.Lambda != nil || current.Kind == syntax.BlockDef || current.Kind == syntax.BlockFunction || current.Kind == syntax.BlockCommand || current.CommandList != nil && current.CommandList != guard.CommandList {
			return false
		}
	}
	return false
}

func (query *CompletionTypes) branchCompletionGuard(scope *Scope, offset int) (ValueType, *Declaration, int, bool) {
	commands, blocks := query.completionGuardCommandList(scope)
	if scope.Block < 0 || scope.Block >= len(blocks) {
		return UnknownValueType, nil, 0, false
	}
	block := blocks[scope.Block]
	if block.Header < 0 || block.Header >= len(commands) {
		return UnknownValueType, nil, 0, false
	}
	branch := block.Header
	for _, candidate := range block.Branches {
		if !query.state.result.analysisStep() {
			return UnknownValueType, nil, 0, false
		}
		if candidate >= 0 && candidate < len(commands) && commands[candidate].Span.Start <= offset {
			branch = candidate
		}
	}
	command := &commands[branch]
	commandScope := query.state.commandScopes[command]
	if commandScope == nil || command.Dialect != syntax.Vim9 || offset < command.Span.End {
		return UnknownValueType, nil, 0, false
	}
	if command.Canonical == "if" || command.Canonical == "elseif" {
		if len(command.Expressions) == 0 || !query.completionGuardExpressionComplete(command, command.Expressions[0]) {
			return UnknownValueType, nil, 0, false
		}
		typ, declaration, positive, ok := query.completionGuardCondition(command.Expressions[0], commandScope)
		if !ok || !positive {
			return UnknownValueType, nil, 0, false
		}
		return typ, declaration, branch, true
	}
	if command.Canonical != "else" || len(block.Branches) != 1 || syntaxDiagnosticOverlaps(query.state.result.File.Diagnostics, command.Span) {
		return UnknownValueType, nil, 0, false
	}
	if tail := strings.TrimSpace(query.state.result.File.Text(command.Argument)); tail != "" && !strings.HasPrefix(tail, "#") {
		return UnknownValueType, nil, 0, false
	}
	header := &commands[block.Header]
	headerScope := query.state.commandScopes[header]
	if header.Dialect != syntax.Vim9 || headerScope == nil || len(header.Expressions) == 0 || !query.completionGuardExpressionComplete(header, header.Expressions[0]) {
		return UnknownValueType, nil, 0, false
	}
	typ, declaration, positive, ok := query.completionGuardCondition(header.Expressions[0], headerScope)
	if !ok || positive {
		return UnknownValueType, nil, 0, false
	}
	return typ, declaration, branch, true
}

func (query *CompletionTypes) completionGuardExpressionComplete(command *syntax.Command, expression *syntax.Expression) bool {
	if expression == nil || expression.Span.End > command.Argument.End || expressionContainsMissing(expression) || syntaxDiagnosticOverlaps(query.state.result.File.Diagnostics, expression.Span) {
		return false
	}
	tail := strings.TrimSpace(query.state.result.File.Source[expression.Span.End:command.Argument.End])
	return tail == "" || strings.HasPrefix(tail, "#")
}

func (query *CompletionTypes) completionGuardCondition(condition *syntax.Expression, scope *Scope) (ValueType, *Declaration, bool, bool) {
	condition = unwrapParenthesizedExpression(condition)
	if condition == nil {
		return UnknownValueType, nil, false, false
	}
	if condition.Kind == syntax.ExpressionUnary && condition.Value == "!" && len(condition.Children) == 1 {
		typ, declaration, positive, ok := query.completionGuardCondition(condition.Children[0], scope)
		return typ, declaration, !positive, ok
	}
	if condition.Kind == syntax.ExpressionBinary {
		if len(condition.Children) != 2 {
			return UnknownValueType, nil, false, false
		}
		positive := condition.Value == "==" || condition.Value == "==#" || condition.Value == "==?"
		negative := condition.Value == "!=" || condition.Value == "!=#" || condition.Value == "!=?"
		if !positive && !negative {
			return UnknownValueType, nil, false, false
		}
		if declaration, ok := completionTypeCallDeclaration(condition.Children[0], scope); ok {
			if typ, ok := completionStaticTypeCode(condition.Children[1], scope); ok {
				return typ, declaration, positive, true
			}
		}
		if declaration, ok := completionTypeCallDeclaration(condition.Children[1], scope); ok {
			if typ, ok := completionStaticTypeCode(condition.Children[0], scope); ok {
				return typ, declaration, positive, true
			}
		}
		return UnknownValueType, nil, false, false
	}
	if condition.Kind != syntax.ExpressionCall || len(condition.Children) != 3 {
		return UnknownValueType, nil, false, false
	}
	callee := condition.Children[0]
	if callee == nil || callee.Kind != syntax.ExpressionIdentifier || callee.Value != "instanceof" || resolve(scope, callee.Value, callee.Span.Start, true, nil) != nil {
		return UnknownValueType, nil, false, false
	}
	target := unwrapParenthesizedExpression(condition.Children[1])
	class := unwrapParenthesizedExpression(condition.Children[2])
	if target == nil || target.Kind != syntax.ExpressionIdentifier || class == nil || class.Kind != syntax.ExpressionIdentifier {
		return UnknownValueType, nil, false, false
	}
	targetDeclaration := resolve(scope, target.Value, target.Span.Start, false, nil)
	classDeclaration := resolve(scope, class.Value, class.Span.Start, false, nil)
	if targetDeclaration == nil || classDeclaration == nil || classDeclaration.Kind != SymbolKindClass {
		return UnknownValueType, nil, false, false
	}
	return ValueType{Name: classDeclaration.Name}, targetDeclaration, true, true
}

func completionTypeCallDeclaration(expression *syntax.Expression, scope *Scope) (*Declaration, bool) {
	expression = unwrapParenthesizedExpression(expression)
	if expression == nil || expression.Kind != syntax.ExpressionCall || len(expression.Children) != 2 {
		return nil, false
	}
	callee := expression.Children[0]
	target := unwrapParenthesizedExpression(expression.Children[1])
	if callee == nil || callee.Kind != syntax.ExpressionIdentifier || callee.Value != "type" || target == nil || target.Kind != syntax.ExpressionIdentifier || resolve(scope, callee.Value, callee.Span.Start, true, nil) != nil {
		return nil, false
	}
	declaration := resolve(scope, target.Value, target.Span.Start, false, nil)
	return declaration, declaration != nil
}

func (query *CompletionTypes) completionGuardRemainsValid(scope *Scope, guardCommand int, declaration *Declaration, offset int) bool {
	commands, blocks := query.completionGuardCommandList(scope)
	for index := guardCommand + 1; index < len(commands); index++ {
		if !query.state.result.analysisStep() {
			return false
		}
		command := &commands[index]
		if command.Span.Start >= offset {
			break
		}
		commandScope := query.state.commandScopes[command]
		if !completionGuardContainsScope(commandScope, scope) {
			continue
		}
		if command.Block >= 0 && command.Block < len(blocks) {
			switch blocks[command.Block].Kind {
			case syntax.BlockFor, syntax.BlockWhile, syntax.BlockTry:
				return false
			}
		}
		if query.completionCommandInvalidatesGuard(command, commandScope, declaration, offset) {
			return false
		}
	}
	return true
}

func (query *CompletionTypes) completionCommandInvalidatesGuard(command *syntax.Command, scope *Scope, declaration *Declaration, offset int) bool {
	if command.For != nil || command.Autocmd != nil || command.Mapping != nil || command.Embedded != nil || command.Canonical == "try" || command.Canonical == "while" {
		return true
	}
	for _, expression := range command.Expressions {
		if query.completionExpressionInvalidatesGuard(expression, scope, declaration, offset) {
			return true
		}
	}
	for _, expression := range command.Targets {
		if query.completionExpressionInvalidatesGuard(expression, scope, declaration, offset) {
			return true
		}
	}
	// The receiver is evaluated before the command itself. A command containing
	// it can only invalidate the guard through an earlier expression above.
	if command.Span.Start < offset && offset <= command.Span.End {
		return false
	}
	switch command.Canonical {
	case "", "echo", "var", "const", "final", "if", "elseif", "else", "endif", "return":
		return false
	default:
		return true
	}
}

func (query *CompletionTypes) completionGuardCommandList(scope *Scope) ([]syntax.Command, []syntax.Block) {
	for current := scope; current != nil; current = current.Parent {
		if current.CommandList != nil {
			return current.CommandList.Commands, current.CommandList.Blocks
		}
		if current.Lambda != nil && current.Lambda.LambdaBody != nil {
			return current.Lambda.LambdaBody.Commands, current.Lambda.LambdaBody.Blocks
		}
	}
	return query.state.result.File.Commands, query.state.result.File.Blocks
}

func completionStaticTypeCode(expression *syntax.Expression, scope *Scope) (ValueType, bool) {
	expression = unwrapParenthesizedExpression(expression)
	if expression == nil {
		return UnknownValueType, false
	}
	if expression.Kind == syntax.ExpressionCall {
		if len(expression.Children) != 2 || expression.Children[0] == nil || expression.Children[0].Kind != syntax.ExpressionIdentifier || expression.Children[0].Value != "type" ||
			resolve(scope, "type", expression.Children[0].Span.Start, true, nil) != nil || completionExpressionHasCall(expression.Children[1]) {
			return UnknownValueType, false
		}
	} else if completionExpressionHasCall(expression) {
		return UnknownValueType, false
	}
	return staticTypeCode(expression)
}

func completionExpressionHasCall(expression *syntax.Expression) bool {
	if expression == nil {
		return false
	}
	if expression.Kind == syntax.ExpressionCall {
		return true
	}
	for _, child := range expression.Children {
		if completionExpressionHasCall(child) {
			return true
		}
	}
	return false
}

func (query *CompletionTypes) completionExpressionInvalidatesGuard(expression *syntax.Expression, scope *Scope, declaration *Declaration, offset int) bool {
	if !query.state.result.analysisStep() {
		return true
	}
	if expression == nil || expression.Span.Start >= offset {
		return false
	}
	if expression.Kind == syntax.ExpressionLambda {
		return false
	}
	if expression.Kind == syntax.ExpressionCall && expression.Span.End <= offset {
		return true
	}
	if expression.Kind == syntax.ExpressionAssignment && len(expression.Children) > 0 && expression.Span.End <= offset && completionAssignmentTargetsDeclaration(expression.Children[0], scope, declaration) {
		return true
	}
	for _, child := range expression.Children {
		if query.completionExpressionInvalidatesGuard(child, scope, declaration, offset) {
			return true
		}
	}
	return false
}

func completionAssignmentTargetsDeclaration(expression *syntax.Expression, scope *Scope, declaration *Declaration) bool {
	if expression == nil {
		return false
	}
	if expression.Kind == syntax.ExpressionIdentifier {
		return resolve(scope, expression.Value, expression.Span.Start, false, nil) == declaration
	}
	for _, child := range expression.Children {
		if completionAssignmentTargetsDeclaration(child, scope, declaration) {
			return true
		}
	}
	return false
}
