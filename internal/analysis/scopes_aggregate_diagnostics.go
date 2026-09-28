package analysis

import (
	"slices"
	"sort"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
)

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
					if methodSignaturesMismatch(result, required.Function, actual.Function) {
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

func collectPublicProtectedMemberNameDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil {
		return
	}
	file := result.File
	classes := result.classes
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
	classes := result.classes
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
	classes := result.classes
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
				expected, actual := aggregateBindingType(result, member, bindingIndex), result.TypeOf(value)
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
		return result.typeFromSyntax(binding.ParsedType, result.commandScopes[command])
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
	if expected.Nominal != (NominalType{}) || actual.Nominal != (NominalType{}) {
		return compatibleNominalTypes(expected, actual)
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
				if methodSignaturesMismatch(result, expected.Function, method.Function) {
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
				if actual != nil && methodSignaturesMismatch(result, required.Function, actual.Function) {
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
			if expected != nil && methodSignaturesMismatch(result, expected, method.Function) {
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

func methodSignaturesMismatch(result *FileAnalysis, expected, actual *syntax.Function) bool {
	if expected == nil || actual == nil || len(expected.TypeParameters) > 0 || len(actual.TypeParameters) > 0 {
		return false
	}
	if len(expected.Parameters) != len(actual.Parameters) || requiredParameterCount(expected.Parameters) != requiredParameterCount(actual.Parameters) ||
		parametersAreVariadic(expected.Parameters) != parametersAreVariadic(actual.Parameters) {
		return true
	}
	for index := range expected.Parameters {
		if !sameMethodType(result.typeFromSyntax(expected.Parameters[index].Type, result.Root), result.typeFromSyntax(actual.Parameters[index].Type, result.Root)) {
			return true
		}
	}
	expectedReturn, actualReturn := ValueType{Name: "void"}, ValueType{Name: "void"}
	if expected.ReturnType != nil {
		expectedReturn = result.typeFromSyntax(expected.ReturnType, result.Root)
	}
	if actual.ReturnType != nil {
		actualReturn = result.typeFromSyntax(actual.ReturnType, result.Root)
	}
	return !sameMethodType(expectedReturn, actualReturn)
}

func sameMethodType(expected, actual ValueType) bool {
	if expected.Nominal != (NominalType{}) || actual.Nominal != (NominalType{}) {
		if expected.Nominal == (NominalType{}) || actual.Nominal == (NominalType{}) {
			return compatibleNominalTypes(expected, actual)
		}
		return expected.Nominal == actual.Nominal && expected.TypeValue == actual.TypeValue
	}
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
	classes := result.classes
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

func localClassAliases(file *syntax.File, classes map[string]*syntax.Command) map[string]string {
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
