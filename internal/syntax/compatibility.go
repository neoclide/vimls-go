package syntax

import (
	"fmt"
	"strings"
)

type Version struct {
	Major int
	Minor int
	Patch int
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%04d", v.Major, v.Minor, v.Patch)
}

func (v Version) before(required Version) bool {
	if v.Major != required.Major {
		return v.Major < required.Major
	}
	if v.Minor != required.Minor {
		return v.Minor < required.Minor
	}
	return v.Patch < required.Patch
}

var (
	versionEnum            = Version{Major: 9, Minor: 1, Patch: 219}
	versionIPut            = Version{Major: 9, Minor: 1, Patch: 1224}
	versionTuple           = Version{Major: 9, Minor: 1, Patch: 1232}
	versionObjectType      = Version{Major: 9, Minor: 1, Patch: 1274}
	versionRedrawTabPanel  = Version{Major: 9, Minor: 1, Patch: 1391}
	versionUniq            = Version{Major: 9, Minor: 1, Patch: 1477}
	versionWaylandCommands = Version{Major: 9, Minor: 1, Patch: 1485}
	versionGenericFunction = Version{Major: 9, Minor: 1, Patch: 1577}
	versionCommandHeredoc  = Version{Major: 9, Minor: 1, Patch: 312}
	versionInlineTextBody  = Version{Major: 9, Minor: 1, Patch: 574}
	versionCTermFont       = Version{Major: 9, Minor: 1, Patch: 30}
)

var commandVersions = map[string]Version{
	"clipreset":      versionWaylandCommands,
	"iput":           versionIPut,
	"pbuffer":        {Major: 9, Minor: 1, Patch: 934},
	"redrawtabpanel": versionRedrawTabPanel,
	"uniq":           versionUniq,
	"wlrestore":      versionWaylandCommands,
}

// CompatibilityDiagnostics reports syntax recognized by this parser that is
// newer than target. Parsing remains version-neutral and always builds the
// latest v9.2.1015 tree.
func CompatibilityDiagnostics(file *File, target Version) []Diagnostic {
	var diagnostics []Diagnostic
	visitCompatibilityFile(file, target, &diagnostics)
	return diagnostics
}

func visitCompatibilityFile(file *File, target Version, diagnostics *[]Diagnostic) {
	if file == nil {
		return
	}
	visitCompatibilityCommands(file.Source, file.Commands, file.Blocks, target, diagnostics)
}

func visitCompatibilityCommands(source string, commands []Command, blocks []Block, target Version, diagnostics *[]Diagnostic) {
	for index := range commands {
		command := &commands[index]
		if required, ok := commandVersions[command.Canonical]; ok {
			addCompatibility(target, required, "command "+command.Canonical, command.Name, diagnostics)
		}
		if command.Canonical == "enum" {
			addCompatibility(target, versionEnum, "enum", command.Name, diagnostics)
		}
		if command.TextBody != nil && command.TextBody.Separator.Start < command.TextBody.Separator.End {
			addCompatibility(target, versionInlineTextBody, "inline text after "+command.Canonical, command.TextBody.Separator, diagnostics)
		}
		if command.Heredoc != nil && commandInsideBlock(command, blocks, BlockCommand) {
			addCompatibility(target, versionCommandHeredoc, "heredoc in command block", command.Argument, diagnostics)
		}
		if command.Highlight != nil {
			for _, attribute := range command.Highlight.Attributes {
				if spanTextEqualFold(source, attribute.Key, "ctermfont") {
					addCompatibility(target, versionCTermFont, "highlight ctermfont attribute", attribute.Key, diagnostics)
				}
			}
		}
		if command.Function != nil {
			if len(command.Function.TypeParameters) > 0 {
				addCompatibility(target, versionGenericFunction, "generic function", command.Function.Name, diagnostics)
			}
			for _, parameter := range command.Function.Parameters {
				visitCompatibilityType(parameter.Type, target, diagnostics)
				visitCompatibilityExpression(parameter.Default, target, diagnostics)
			}
			visitCompatibilityType(command.Function.ReturnType, target, diagnostics)
		}
		if command.Declaration != nil {
			if len(command.Declaration.Bindings) == 0 {
				visitCompatibilityType(command.Declaration.ParsedType, target, diagnostics)
			} else {
				for _, binding := range command.Declaration.Bindings {
					visitCompatibilityType(binding.ParsedType, target, diagnostics)
				}
			}
		}
		if command.TypeAlias != nil {
			visitCompatibilityType(command.TypeAlias.Type, target, diagnostics)
		}
		for _, expression := range command.Expressions {
			visitCompatibilityExpression(expression, target, diagnostics)
		}
		for _, value := range command.EnumValues {
			visitCompatibilityExpression(value.Initializer, target, diagnostics)
		}
		if command.Embedded != nil {
			visitCompatibilityCommands(source, command.Embedded.Commands, command.Embedded.Blocks, target, diagnostics)
		}
	}
}

func spanTextEqualFold(source string, span Span, text string) bool {
	return span.Start >= 0 && span.Start <= span.End && span.End <= len(source) && strings.EqualFold(source[span.Start:span.End], text)
}

func commandInsideBlock(command *Command, blocks []Block, kind BlockKind) bool {
	if command == nil {
		return false
	}
	for blockIndex := command.Block; blockIndex >= 0 && blockIndex < len(blocks); blockIndex = blocks[blockIndex].Parent {
		if blocks[blockIndex].Kind == kind {
			return true
		}
	}
	return false
}

func visitCompatibilityExpression(expression *Expression, target Version, diagnostics *[]Diagnostic) {
	if expression == nil {
		return
	}
	if expression.Kind == ExpressionTuple {
		addCompatibility(target, versionTuple, "tuple value", expression.Span, diagnostics)
	}
	if len(expression.TypeArguments) > 0 {
		addCompatibility(target, versionGenericFunction, "generic function call", expression.Operator, diagnostics)
		for _, typeArgument := range expression.TypeArguments {
			visitCompatibilityType(typeArgument, target, diagnostics)
		}
	}
	visitCompatibilityType(expression.CastType, target, diagnostics)
	for _, parameter := range expression.Parameters {
		visitCompatibilityType(parameter.Type, target, diagnostics)
	}
	visitCompatibilityType(expression.ReturnType, target, diagnostics)
	for _, child := range expression.Children {
		visitCompatibilityExpression(child, target, diagnostics)
	}
	if expression.LambdaBody != nil {
		visitCompatibilityFile(expression.LambdaBody, target, diagnostics)
	}
}

func visitCompatibilityType(typeNode *Type, target Version, diagnostics *[]Diagnostic) {
	if typeNode == nil {
		return
	}
	if typeNode.Name == "tuple" {
		addCompatibility(target, versionTuple, "tuple type", typeNode.Span, diagnostics)
	}
	if typeNode.Name == "object" && typeNode.Kind == TypeGeneric {
		addCompatibility(target, versionObjectType, "object type", typeNode.Span, diagnostics)
	}
	for _, argument := range typeNode.Arguments {
		visitCompatibilityType(argument, target, diagnostics)
	}
	visitCompatibilityType(typeNode.ReturnType, target, diagnostics)
}

func addCompatibility(target, required Version, feature string, span Span, diagnostics *[]Diagnostic) {
	if !target.before(required) {
		return
	}
	*diagnostics = append(*diagnostics, Diagnostic{
		Code: "vimls/target-version", Message: feature + " requires Vim " + required.String() + " (target is " + target.String() + ")", Span: span,
	})
}
