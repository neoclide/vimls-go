package analysis

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
)

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
	if result == nil || result.File == nil || target == nil || rhs == nil || (rhs.Kind != syntax.ExpressionList && rhs.Kind != syntax.ExpressionTuple) ||
		expressionContainsMissing(target) || expressionContainsMissing(rhs) {
		return
	}
	fixed := len(target.Children)
	rest := strings.Contains(result.File.Text(target.Span), ";")
	if rest {
		fixed--
	}
	if rhs.Kind == syntax.ExpressionTuple {
		appendVim9CardinalityDiagnostic(result, fixed, rest, rhs, false)
		return
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
		appendDestructuringTypeMismatchDiagnostic(result, result.commandScopes[command], declaration.Target, declaration.Initializer, declaration.Bindings)
		return
	}
	for index, binding := range declaration.Bindings {
		expected := result.typeFromSyntax(binding.ParsedType, result.commandScopes[command])
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
		if forLoopDestructures(result.File, command) {
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
	destructures := forLoopDestructures(result.File, command)
	for index, binding := range loop.Bindings {
		expected := result.typeFromSyntax(binding.ParsedType, scope)
		if isUnknownType(expected) || ignoredDestructuringBinding(result.File, command, binding.Name) {
			continue
		}
		rows := []*syntax.Expression{nil}
		if destructures && (loop.Iterable.Kind == syntax.ExpressionList || loop.Iterable.Kind == syntax.ExpressionTuple) {
			rows = loop.Iterable.Children
		}
		for _, row := range rows {
			member := actual
			span := loop.Iterable.Span
			if destructures {
				member = destructuredValueType(actual, index, binding.Rest)
				if row != nil {
					member = destructuredValueType(result.TypeOf(row), index, binding.Rest)
					span = row.Span
					if !binding.Rest && (row.Kind == syntax.ExpressionList || row.Kind == syntax.ExpressionTuple) && index < len(row.Children) {
						member = result.TypeOf(row.Children[index])
						span = row.Children[index].Span
					}
				}
			}
			if isUnknownType(member) || assignmentTypesCompatible(expected, member) {
				continue
			}
			diagnostic := syntax.Diagnostic{
				Code: "vim/E1012", Message: "Type mismatch; expected " + valueTypeDisplay(expected) + " but got " + valueTypeDisplay(member), Span: span,
			}
			if destructures {
				diagnostic.Code = "vim/E1163"
				diagnostic.Message = "Variable " + strconv.Itoa(index+1) + ": type mismatch, expected " + valueTypeDisplay(expected) + " but got " + valueTypeDisplay(member)
			}
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			return
		}
	}
}

func collectAssignmentTypeMismatchDiagnostics(result *FileAnalysis, scope *Scope, expression *syntax.Expression) {
	if expression == nil {
		return
	}
	if _, ok := optionAssignment(expression); ok {
		for _, child := range expression.Children {
			collectAssignmentTypeMismatchDiagnostics(result, scope, child)
		}
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
		appendTypeMismatchDiagnostic(result, result.TypeOf(expression), expression.Children[0])
	}
	if expression.Kind == syntax.ExpressionBinary && (expression.Value == "&&" || expression.Value == "||") && scopeUsesDefTypeRules(scope) {
		collectLogicalTypeMismatchDiagnostic(result, expression)
		if logicalRightOperandIsSkipped(expression) {
			collectAssignmentTypeMismatchDiagnostics(result, scope, expression.Children[0])
			return
		}
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
				// @# accepts a buffer number with plain assignment only.
				if target.Kind == syntax.ExpressionIdentifier && target.Value == "@#" && result.TypeOf(expression.Children[1]).Name == "number" {
					expected = ValueType{Name: "number"}
				}
				if !scopeUsesDefTypeRules(scope) && expected.Name == "string" && target.Kind == syntax.ExpressionIdentifier && strings.HasPrefix(target.Value, "&") && result.TypeOf(expression.Children[1]).Name == "list" {
					diagnostic, _ := stringConversionDiagnostic(result.TypeOf(expression.Children[1]), expression.Children[1].Span)
					result.Diagnostics = append(result.Diagnostics, diagnostic)
					return
				}
				if target.Kind != syntax.ExpressionIdentifier ||
					(!optionAcceptsFunction(target.Value) || result.TypeOf(expression.Children[1]).Name != "func") &&
						!optionAcceptsCompatibleType(target.Value, result.TypeOf(expression.Children[1])) {
					appendTypeMismatchDiagnostic(result, expected, expression.Children[1])
				}
			}
		}
	}
	if expression.Kind == syntax.ExpressionAssignment && expression.Value == "..=" && len(expression.Children) >= 2 && !expressionContainsMissing(expression) {
		target := expression.Children[0]
		// Vim9's concatenating assignment is only valid for a direct string
		// target.  Keep compound member/index assignments opaque: their
		// container type does not prove the assignable member's type.
		if target != nil && target.Kind == syntax.ExpressionIdentifier {
			if stringOnlyAssignmentTarget(target) {
				appendTypeMismatchDiagnostic(result, ValueType{Name: "string"}, expression.Children[1])
			} else if targetType := assignmentTargetType(result, scope, target); !isUnknownType(targetType) && targetType.Name != "string" {
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
	invalid := !isUnknownType(receiver) && receiver.Nominal == (NominalType{}) && receiver.Name != "dict" && receiver.Name != "object" && result.classes[receiver.Name] == nil &&
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
		// Interpreted Vim9 checks the string value before writing a register;
		// compiled code rejects a read-only register before checking the value.
		var invalidRegister *syntax.Diagnostic
		invalidSyntax := false
		for diagnosticIndex := range result.File.Diagnostics {
			diagnostic := &result.File.Diagnostics[diagnosticIndex]
			if diagnostic.Span.Start > targetItem.Span.End || diagnostic.Span.End < targetItem.Span.Start {
				continue
			}
			if !scopeUsesDefTypeRules(scope) && targetItem.Kind == syntax.ExpressionIdentifier && len(targetItem.Value) == 2 &&
				targetItem.Value[0] == '@' && strings.ContainsRune(".%:~", rune(targetItem.Value[1])) && diagnostic.Code == "vim/E354" &&
				diagnostic.Span == (syntax.Span{Start: targetItem.Span.Start + 1, End: targetItem.Span.End}) {
				invalidRegister = diagnostic
				continue
			}
			invalidSyntax = true
			break
		}
		if invalidSyntax {
			continue
		}
		expected := UnknownValueType
		if len(bindings) > index && bindings[index].ParsedType != nil {
			expected = result.typeFromSyntax(bindings[index].ParsedType, scope)
		} else if invalidRegister != nil {
			expected = ValueType{Name: "string"}
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
		if targetItem.Kind == syntax.ExpressionIdentifier && targetItem.Value == "@#" && actual.Name == "number" {
			continue
		}
		if isUnknownType(expected) || isUnknownType(actual) || assignmentTypesCompatible(expected, actual) {
			continue
		}
		diagnostic := syntax.Diagnostic{
			Code: "vim/E1163", Message: "Variable " + strconv.Itoa(index+1) + ": type mismatch, expected " + valueTypeDisplay(expected) + " but got " + valueTypeDisplay(actual), Span: span,
		}
		if stringOnlyAssignmentTarget(targetItem) || invalidRegister != nil {
			diagnostic.Code = "vim/E1012"
			diagnostic.Message = "Type mismatch; expected string but got " + valueTypeDisplay(actual)
		}
		if invalidRegister != nil {
			result.suppressedSyntaxDiagnostics[*invalidRegister] = true
		}
		result.Diagnostics = append(result.Diagnostics, diagnostic)
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
	for index, operand := range expression.Children[:2] {
		if index == 1 && logicalRightOperandIsSkipped(expression) {
			break
		}
		actual := result.TypeOf(operand)
		if isUnknownType(actual) || actual.Name == "bool" {
			continue
		}
		if actual.Name == "number" {
			// Variables and calls are checked by Vim at runtime. A known
			// literal other than 0/1 is a compiled type error instead.
			if value, known := staticNumberValue(operand); !known || value == 0 || value == 1 {
				continue
			}
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
	expected := result.typeFromSyntax(expression.ReturnType, result.lambdaScopes[expression])
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

// Vim v9.2.1130 requires strings for these external targets, including ..=.
func stringOnlyAssignmentTarget(target *syntax.Expression) bool {
	if target == nil || target.Kind != syntax.ExpressionIdentifier {
		return false
	}
	if strings.HasPrefix(target.Value, "$") {
		return true
	}
	if strings.HasPrefix(target.Value, "@") {
		name, size := utf8.DecodeRuneInString(target.Value[1:])
		return size > 0 && 1+size == len(target.Value) && (name == '@' || syntax.ValidRegisterName(name) && !strings.ContainsRune(".%:~", name))
	}
	variable, ok := vimdata.LookupVariable(target.Value)
	return ok && variable.Type == "string" && variable.Flags&vimdata.VariableReadOnly == 0
}

func assignmentTargetType(result *FileAnalysis, scope *Scope, target *syntax.Expression) ValueType {
	if result == nil || target == nil {
		return UnknownValueType
	}
	switch target.Kind {
	case syntax.ExpressionIdentifier:
		if strings.HasPrefix(target.Value, "&") {
			typ := optionExpressionValueType(target)
			if isUnknownType(typ) {
				// An ambiguous read type must not disable assignment checking.
				// Compatible scalar alternatives are accepted at the call site;
				// containers still need to fail the ordinary type check.
				if option, ok := vimdata.LookupOption(target.Value); ok {
					return builtinOptionValueType(option)
				}
			}
			return typ
		}
		if stringOnlyAssignmentTarget(target) {
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
	if expected.Nominal != (NominalType{}) || actual.Nominal != (NominalType{}) {
		return compatibleNominalTypes(expected, actual)
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

// A missing nominal identity can be an unresolved annotation or the generic
// object/class type of a null value. Neither establishes a distinct class.
func compatibleNominalTypes(expected, actual ValueType) bool {
	if expected.Nominal == (NominalType{}) {
		return !knownAssignmentType(expected) || expected.Name == "object" && !actual.TypeValue || expected.Name == "class" && actual.TypeValue
	}
	if actual.Nominal == (NominalType{}) {
		return !knownAssignmentType(actual) || actual.Name == "object" && !expected.TypeValue || actual.Name == "class" && expected.TypeValue
	}
	if expected.TypeValue != actual.TypeValue {
		return false
	}
	return actual.HasNominal(expected.Nominal) || actual.IncompleteParents && (expected.Nominal.Kind == SymbolKindClass || expected.Nominal.Kind == SymbolKindInterface)
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
			if !vimdata.IsFunction(callee.Value) && !vimdata.IsNeovimCompatFunction(callee.Value) {
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
