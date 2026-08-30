package syntax

import "testing"

const officialVimTag = "v9.2.1015"

func assertFileSpans(t *testing.T, file *File) {
	t.Helper()
	assertFileSpansAt(t, file, "file")
}

func assertFileSpansAt(t *testing.T, file *File, origin string) {
	t.Helper()
	if (file.OpaqueTail.Start != 0 || file.OpaqueTail.End != 0) && !validSpan(file.OpaqueTail, len(file.Source)) {
		t.Fatalf("%s: invalid opaque tail span %#v", origin, file.OpaqueTail)
	}
	check := func(name string, span Span) {
		if !validSpan(span, len(file.Source)) {
			t.Fatalf("%s: invalid %s span %#v", origin, name, span)
		}
	}
	previous := -1
	for _, command := range file.Commands {
		if command.Span.Start < previous || !validSpan(command.Span, len(file.Source)) {
			t.Fatalf("%s: invalid or unordered command span %#v", origin, command.Span)
		}
		previous = command.Span.Start
		check("command range", command.Range)
		check("command name", command.Name)
		check("command bang", command.Bang)
		check("command count", command.Count)
		check("command argument", command.Argument)
		for _, modifier := range command.Modifiers {
			check("modifier", modifier.Span)
			check("modifier bang", modifier.Bang)
			details := map[string]Span{"modifier bang": modifier.Bang}
			if modifier.Filter != nil {
				check("filter delimiter", modifier.Filter.Delimiter)
				check("filter pattern", modifier.Filter.Pattern)
				check("filter flags", modifier.Filter.Flags)
				details["filter delimiter"] = modifier.Filter.Delimiter
				details["filter pattern"] = modifier.Filter.Pattern
				details["filter flags"] = modifier.Filter.Flags
			}
			for name, span := range details {
				if span.Start < span.End && (span.Start < command.Span.Start || span.End > command.Span.End) {
					t.Fatalf("%s: %s span %#v is outside command %#v", origin, name, span, command.Span)
				}
			}
		}
		if command.Autocmd != nil {
			autocmd := command.Autocmd
			for name, span := range map[string]Span{
				"autocmd head": autocmd.Head, "autocmd group": autocmd.Group, "autocmd pattern": autocmd.Pattern,
			} {
				check(name, span)
				if span.Start < span.End && (span.Start < command.Argument.Start || span.End > command.Argument.End) {
					t.Fatalf("%s: %s span %#v is outside argument %#v", origin, name, span, command.Argument)
				}
			}
			for index, event := range autocmd.Events {
				check("autocmd event", event)
				if event.Start < command.Argument.Start || event.End > command.Argument.End {
					t.Fatalf("%s: autocmd event %d span %#v is outside argument %#v", origin, index, event, command.Argument)
				}
			}
			for index, modifier := range autocmd.Modifiers {
				check("autocmd modifier", modifier.Span)
				if modifier.Span.Start < command.Argument.Start || modifier.Span.End > command.Argument.End {
					t.Fatalf("%s: autocmd modifier %d span %#v is outside argument %#v", origin, index, modifier.Span, command.Argument)
				}
			}
		}
		if command.Declaration != nil {
			declaration := command.Declaration
			check("declaration name", declaration.Name)
			check("declaration type", declaration.Type)
			check("declaration assignment", declaration.Assignment)
			assertExpressionSpans(t, file, origin, declaration.Target)
			assertExpressionSpans(t, file, origin, declaration.Initializer)
			assertTypeSpans(t, file, origin, declaration.ParsedType)
			for _, binding := range declaration.Bindings {
				check("binding name", binding.Name)
				check("binding type", binding.Type)
				assertTypeSpans(t, file, origin, binding.ParsedType)
			}
		}
		for _, expression := range command.Expressions {
			assertExpressionSpans(t, file, origin, expression)
		}
		for _, target := range command.Targets {
			assertExpressionSpans(t, file, origin, target)
		}
		if command.Heredoc != nil {
			check("heredoc body", command.Heredoc.Body)
			check("heredoc end marker", command.Heredoc.EndMarker)
		}
		if command.TextBody != nil {
			check("text body separator", command.TextBody.Separator)
			check("text body", command.TextBody.Body)
			check("text body end marker", command.TextBody.EndMarker)
			for _, line := range command.TextBody.Lines {
				check("text body line", line)
			}
		}
		if command.Keymap != nil {
			check("keymap body", command.Keymap.Body)
			for _, entry := range command.Keymap.Entries {
				check("keymap from", entry.From)
				check("keymap to", entry.To)
			}
		}
		if command.Function != nil {
			function := command.Function
			check("function name", function.Name)
			check("function return type", function.ReturnTypeSpan)
			check("function attributes", function.Attributes)
			for _, parameter := range function.Parameters {
				assertParameterSpans(t, file, origin, parameter)
			}
			for _, parameter := range function.TypeParameters {
				check("type parameter", parameter.Span)
			}
			assertTypeSpans(t, file, origin, function.ReturnType)
		}
		if command.Aggregate != nil {
			check("aggregate name", command.Aggregate.Name)
			for _, span := range command.Aggregate.Extends {
				check("aggregate extends", span)
			}
			for _, span := range command.Aggregate.Implements {
				check("aggregate implements", span)
			}
		}
		if command.TypeAlias != nil {
			check("type alias name", command.TypeAlias.Name)
			check("type alias assignment", command.TypeAlias.Assignment)
			check("type alias type", command.TypeAlias.TypeSpan)
			assertTypeSpans(t, file, origin, command.TypeAlias.Type)
		}
		for _, value := range command.EnumValues {
			check("enum value name", value.Name)
			assertExpressionSpans(t, file, origin, value.Initializer)
			for _, argument := range value.Arguments {
				assertExpressionSpans(t, file, origin, argument)
			}
		}
		if command.Import != nil {
			check("import path", command.Import.PathSpan)
			check("import alias", command.Import.Alias)
			assertExpressionSpans(t, file, origin, command.Import.Path)
		}
		if command.For != nil {
			check("for iterable", command.For.IterableSpan)
			assertExpressionSpans(t, file, origin, command.For.Iterable)
			for _, binding := range command.For.Bindings {
				check("for binding name", binding.Name)
				check("for binding type", binding.Type)
				assertTypeSpans(t, file, origin, binding.ParsedType)
			}
		}
		if command.Mapping != nil {
			check("mapping lhs", command.Mapping.LHS)
			check("mapping rhs", command.Mapping.RHS)
			assertExpressionSpans(t, file, origin, command.Mapping.RHSExpression)
			for name, span := range map[string]Span{"mapping lhs": command.Mapping.LHS, "mapping rhs": command.Mapping.RHS} {
				if span != (Span{}) && (span.Start < command.Argument.Start || span.End > command.Argument.End) {
					t.Fatalf("%s: %s span %#v is outside argument %#v", origin, name, span, command.Argument)
				}
			}
			if expression := command.Mapping.RHSExpression; expression != nil &&
				(expression.Span.Start < command.Mapping.RHS.Start || expression.Span.End > command.Mapping.RHS.End) {
				t.Fatalf("%s: mapping expression span %#v is outside rhs %#v", origin, expression.Span, command.Mapping.RHS)
			}
		}
		if command.Substitute != nil {
			substitute := command.Substitute
			parts := map[string]Span{
				"substitute delimiter":              substitute.Delimiter,
				"substitute pattern":                substitute.Pattern,
				"substitute pattern delimiter":      substitute.PatternDelimiter,
				"substitute replacement":            substitute.Replacement,
				"substitute replacement delimiter":  substitute.ReplacementDelimiter,
				"substitute flags":                  substitute.Flags,
				"substitute count":                  substitute.Count,
				"substitute previous pattern":       substitute.PreviousPattern,
				"substitute replacement prefix":     substitute.ReplacementPrefix,
				"substitute replacement expression": substitute.ExpressionSpan,
			}
			for name, span := range parts {
				check(name, span)
				if span.Start < span.End && (span.Start < command.Argument.Start || span.End > command.Argument.End) {
					t.Fatalf("%s: %s span %#v is outside argument %#v", origin, name, span, command.Argument)
				}
			}
			assertExpressionSpans(t, file, origin, substitute.Expression)
		}
		if command.Highlight != nil {
			highlight := command.Highlight
			parts := map[string]Span{
				"highlight default":     highlight.Default,
				"highlight operation":   highlight.Operation,
				"highlight group":       highlight.Group,
				"highlight link target": highlight.LinkTarget,
			}
			for name, span := range parts {
				check(name, span)
				if span.Start < span.End && (span.Start < command.Argument.Start || span.End > command.Argument.End) {
					t.Fatalf("%s: %s span %#v is outside argument %#v", origin, name, span, command.Argument)
				}
			}
			for index, attribute := range highlight.Attributes {
				for name, span := range map[string]Span{
					"key": attribute.Key, "equal": attribute.Equal, "value": attribute.Value,
				} {
					check("highlight attribute "+name, span)
					if span != (Span{}) && (span.Start < command.Argument.Start || span.End > command.Argument.End) {
						t.Fatalf("%s: highlight attribute %d %s span %#v is outside argument %#v", origin, index, name, span, command.Argument)
					}
				}
			}
		}
		if command.Syntax != nil {
			syntax := command.Syntax
			parts := map[string]Span{"syntax subcommand": syntax.Subcommand, "syntax group": syntax.Group}
			for name, span := range parts {
				check(name, span)
				if span.Start < span.End && (span.Start < command.Argument.Start || span.End > command.Argument.End) {
					t.Fatalf("%s: %s span %#v is outside argument %#v", origin, name, span, command.Argument)
				}
			}
			for index, keyword := range syntax.Keywords {
				check("syntax keyword", keyword)
				if keyword.Start < command.Argument.Start || keyword.End > command.Argument.End {
					t.Fatalf("%s: syntax keyword %d span %#v is outside argument %#v", origin, index, keyword, command.Argument)
				}
			}
			for index, option := range syntax.Options {
				for name, span := range map[string]Span{"name": option.Name, "equal": option.Equal, "value": option.Value} {
					check("syntax option "+name, span)
					if span.Start < span.End && (span.Start < command.Argument.Start || span.End > command.Argument.End) {
						t.Fatalf("%s: syntax option %d %s span %#v is outside argument %#v", origin, index, name, span, command.Argument)
					}
				}
				for _, value := range option.Values {
					check("syntax option value", value)
					if value.Start < value.End && (value.Start < command.Argument.Start || value.End > command.Argument.End) {
						t.Fatalf("%s: syntax option %d value span %#v is outside argument %#v", origin, index, value, command.Argument)
					}
				}
			}
			for index, pattern := range syntax.Patterns {
				for name, span := range map[string]Span{"key": pattern.Key, "equal": pattern.Equal, "open": pattern.OpenDelimiter, "pattern": pattern.Pattern, "close": pattern.CloseDelimiter, "offsets": pattern.Offsets} {
					check("syntax pattern "+name, span)
					if span.Start < span.End && (span.Start < command.Argument.Start || span.End > command.Argument.End) {
						t.Fatalf("%s: syntax pattern %d %s span %#v is outside argument %#v", origin, index, name, span, command.Argument)
					}
				}
			}
		}
		if command.Set != nil {
			for index, option := range command.Set.Options {
				for name, span := range map[string]Span{
					"span": option.Span, "prefix": option.Prefix, "name": option.Name,
					"operator": option.Operator, "value": option.Value,
				} {
					check("set option "+name, span)
					if span.Start < span.End && (span.Start < command.Argument.Start || span.End > command.Argument.End) {
						t.Fatalf("%s: set option %d %s span %#v is outside argument %#v", origin, index, name, span, command.Argument)
					}
				}
			}
		}
		if command.Embedded != nil {
			check("embedded command list", command.Embedded.Span)
			if command.Embedded.Span.Start < command.Argument.Start || command.Embedded.Span.End > command.Argument.End {
				t.Fatalf("%s: embedded command list span %#v is outside argument %#v", origin, command.Embedded.Span, command.Argument)
			}
			assertFileSpansAt(t, &File{
				Dialect:  command.Dialect,
				Source:   file.Source,
				Commands: command.Embedded.Commands,
				Blocks:   command.Embedded.Blocks,
			}, origin+" embedded "+commandDisplayName(command))
		}
	}
	for _, token := range file.Tokens {
		if !validSpan(token.Span, len(file.Source)) {
			t.Fatalf("%s: invalid token span %#v", origin, token.Span)
		}
	}
	for _, block := range file.Blocks {
		if !validSpan(block.Span, len(file.Source)) {
			t.Fatalf("%s: invalid block span %#v", origin, block.Span)
		}
	}
	for _, diagnostic := range file.Diagnostics {
		if !validSpan(diagnostic.Span, len(file.Source)) {
			t.Fatalf("%s: invalid diagnostic span %#v", origin, diagnostic.Span)
		}
	}
}

func assertExpressionSpans(t *testing.T, file *File, origin string, expression *Expression) {
	t.Helper()
	if expression == nil {
		return
	}
	if !validSpan(expression.Span, len(file.Source)) || !validSpan(expression.Operator, len(file.Source)) || !validSpan(expression.ReturnTypeSpan, len(file.Source)) {
		t.Fatalf("%s: invalid expression spans %#v", origin, expression)
	}
	for _, child := range expression.Children {
		assertExpressionSpans(t, file, origin, child)
	}
	for _, typeArgument := range expression.TypeArguments {
		assertTypeSpans(t, file, origin, typeArgument)
	}
	assertTypeSpans(t, file, origin, expression.CastType)
	assertTypeSpans(t, file, origin, expression.ReturnType)
	for _, parameter := range expression.Parameters {
		assertParameterSpans(t, file, origin, parameter)
	}
	if expression.LambdaBody != nil {
		assertFileSpansAt(t, expression.LambdaBody, origin+" lambda")
	}
}

func assertTypeSpans(t *testing.T, file *File, origin string, node *Type) {
	t.Helper()
	if node == nil {
		return
	}
	if !validSpan(node.Span, len(file.Source)) {
		t.Fatalf("%s: invalid type span %#v", origin, node.Span)
	}
	for _, argument := range node.Arguments {
		assertTypeSpans(t, file, origin, argument)
	}
	assertTypeSpans(t, file, origin, node.ReturnType)
}

func assertParameterSpans(t *testing.T, file *File, origin string, parameter Parameter) {
	t.Helper()
	for name, span := range map[string]Span{
		"parameter name":    parameter.Name,
		"parameter type":    parameter.TypeSpan,
		"parameter default": parameter.DefaultSpan,
	} {
		if !validSpan(span, len(file.Source)) {
			t.Fatalf("%s: invalid %s span %#v", origin, name, span)
		}
	}
	assertTypeSpans(t, file, origin, parameter.Type)
	assertExpressionSpans(t, file, origin, parameter.Target)
	assertExpressionSpans(t, file, origin, parameter.Default)
}

func validSpan(span Span, sourceLength int) bool {
	return span.Start >= 0 && span.End >= span.Start && span.End <= sourceLength
}
