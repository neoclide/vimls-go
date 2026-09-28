package analysis

import (
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
)

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
