package analysis

import (
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
)

type variableAssignmentType struct {
	typ  ValueType
	span syntax.Span
}

// Compare straight-line legacy assignments, not declaration types: legacy
// declarations retain their initial type even after a later unknown write.
// Every other command is a barrier, including branch/function boundaries,
// unlet, source, execute, compound assignments and deferred command bodies.
func collectVariableTypeChangeDiagnostics(result *FileAnalysis) {
	recent := make(map[string]variableAssignmentType)
	var previousScope *Scope
	for index := range result.File.Commands {
		command := &result.File.Commands[index]
		scope := result.commandScopes[command]
		if scope != previousScope {
			clear(recent)
		}
		previousScope = scope
		declaration := command.Declaration
		if command.Dialect != syntax.Legacy || command.Canonical != "let" || declaration == nil ||
			declaration.Target == nil || declaration.Target.Kind != syntax.ExpressionIdentifier ||
			declaration.Initializer == nil || result.File.Text(declaration.Assignment) != "=" ||
			command.Range.Start != command.Range.End || len(command.Modifiers) != 0 ||
			syntaxDiagnosticOverlaps(result.File.Diagnostics, command.Span) {
			clear(recent)
			continue
		}
		target := declaration.Target
		name := assignmentTypeName(target.Value, scope)
		if name == "" {
			clear(recent)
			continue
		}
		typ, calls := currentAssignmentType(result, declaration.Initializer, scope, recent)
		// A call can change even an unrelated global while evaluating the RHS.
		if calls {
			clear(recent)
		}
		previous, exists := recent[name]
		if exists && knownVariableAssignmentType(previous.typ) && knownVariableAssignmentType(typ) &&
			valueTypeCategory(previous.typ) != valueTypeCategory(typ) &&
			!variableAssignmentHasError(result.Diagnostics, command.Span) {
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code:    "vimls/variable-type-change",
				Message: "Variable " + target.Value + " changes type from " + valueTypeCategory(previous.typ) + " to " + valueTypeCategory(typ),
				Span:    target.Span,
				Related: syntax.RelatedDiagnostic{Message: "Previous assignment has type " + valueTypeCategory(previous.typ), Span: previous.span},
			})
		}
		// Unknown/any must replace, rather than preserve, the preceding fact.
		recent[name] = variableAssignmentType{typ: typ, span: target.Span}
	}
}

func variableAssignmentHasError(diagnostics []syntax.Diagnostic, span syntax.Span) bool {
	for _, diagnostic := range diagnostics {
		if strings.HasPrefix(diagnostic.Code, "vim/E") && diagnostic.Span.Start < span.End && diagnostic.Span.End > span.Start {
			return true
		}
	}
	return false
}

func knownVariableAssignmentType(typ ValueType) bool {
	return typ.Name != "" && typ.Name != ValueTypeAny
}

func assignmentTypeName(name string, scope *Scope) string {
	if name == "" || strings.ContainsAny(name, "$@&{}") {
		return ""
	}
	if strings.Contains(name, ":") {
		return name
	}
	for current := scope; current != nil; current = current.Parent {
		if current.Kind == syntax.BlockFunction {
			return "l:" + name
		}
	}
	return "g:" + name
}

// Reuse inferred expression types only when their variable reads still agree
// with this assignment sequence. Declaration facts alone can be stale here.
func currentAssignmentType(result *FileAnalysis, expression *syntax.Expression, scope *Scope, recent map[string]variableAssignmentType) (ValueType, bool) {
	if expression == nil {
		return UnknownValueType, false
	}
	if expression.Kind == syntax.ExpressionIdentifier && !isLiteralIdentifier(expression.Value) &&
		expression.Value != "v:true" && expression.Value != "v:false" && expression.Value != "v:null" && expression.Value != "v:none" {
		return recent[assignmentTypeName(expression.Value, scope)].typ, false
	}
	if expression.Kind == syntax.ExpressionParenthesized && len(expression.Children) == 1 {
		return currentAssignmentType(result, expression.Children[0], scope, recent)
	}
	if expression.Kind == syntax.ExpressionLambda {
		return result.TypeOf(expression), false
	}
	typ := result.TypeOf(expression)
	calls := expression.Kind == syntax.ExpressionCall
	children := expression.Children
	if calls && len(children) > 0 {
		// The callee identifier is a function name, not a variable read. Its
		// receiver (for method calls) still needs the same stale-fact check.
		if children[0] != nil && children[0].Kind == syntax.ExpressionMember {
			children = append(append([]*syntax.Expression(nil), children[0].Children...), children[1:]...)
		} else {
			children = children[1:]
		}
	}
	for _, child := range children {
		current, childCalls := currentAssignmentType(result, child, scope, recent)
		calls = calls || childCalls
		if !knownVariableAssignmentType(current) || !sameMethodType(current, result.TypeOf(child)) {
			if expression.Kind == syntax.ExpressionList || expression.Kind == syntax.ExpressionDictionary {
				// The container kind is certain even when inferred element
				// types are stale. Do not carry those element facts forward.
				typ.Arguments = []ValueType{UnknownValueType}
			} else {
				typ = UnknownValueType
			}
		}
	}
	return typ, calls
}
