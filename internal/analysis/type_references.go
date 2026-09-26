package analysis

import (
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// walkTypeReference records names contained in a parsed Vim9 type. Types are not
// value expressions: only declarations that introduce a type, or the namespace
// of a named import, can be linked from here.
func walkTypeReference(result *FileAnalysis, file *syntax.File, typeNode *syntax.Type, scope *Scope, dialect syntax.Dialect, typeParameters []syntax.TypeParameter) {
	if typeNode == nil || !result.analysisStep() {
		return
	}
	if typeNode.Kind == syntax.TypeNamed || typeNode.Kind == syntax.TypeGeneric {
		span := syntax.Span{Start: typeNode.Span.Start, End: typeNode.Span.Start + len(typeNode.Name)}
		walkTypeNameReference(result, file, typeNode.Name, span, scope, dialect, typeParameters)
	}
	for _, argument := range typeNode.Arguments {
		walkTypeReference(result, file, argument, scope, dialect, typeParameters)
	}
	walkTypeReference(result, file, typeNode.ReturnType, scope, dialect, typeParameters)
}

func walkTypeNameReference(result *FileAnalysis, file *syntax.File, name string, span syntax.Span, scope *Scope, dialect syntax.Dialect, typeParameters []syntax.TypeParameter) {
	if !validNameSpan(file, span) || file.Text(span) != name || isKnownNonAggregateTypeName(name) || typeParameterName(typeParameters, name) || scopeHasTypeParameter(file, scope, name) {
		return
	}
	if declaration := resolve(scope, name, span.Start, false, nil); declaration != nil && typeDeclarationKind(declaration.Kind) {
		result.References = append(result.References, &Reference{Name: name, Span: span, Declaration: declaration, scope: scope, dialect: dialect})
		return
	}
	dot := strings.IndexByte(name, '.')
	if dot <= 0 {
		result.References = append(result.References, &Reference{Name: name, Span: span, scope: scope, dialect: dialect})
		return
	}
	qualifier := name[:dot]
	qualifierSpan := syntax.Span{Start: span.Start, End: span.Start + dot}
	if declaration := resolve(scope, qualifier, qualifierSpan.Start, false, nil); declaration != nil && declaration.Kind == SymbolKindImport {
		result.References = append(result.References, &Reference{Name: qualifier, Span: qualifierSpan, Declaration: declaration, scope: scope, dialect: dialect})
		return
	}
	result.References = append(result.References, &Reference{Name: qualifier, Span: qualifierSpan, scope: scope, dialect: dialect})
}

func scopeHasTypeParameter(file *syntax.File, scope *Scope, name string) bool {
	for current := scope; current != nil; current = current.Parent {
		if current.Block < 0 || current.Kind != syntax.BlockDef && current.Kind != syntax.BlockFunction {
			continue
		}
		commands, blocks := file.Commands, file.Blocks
		if current.CommandList != nil {
			commands, blocks = current.CommandList.Commands, current.CommandList.Blocks
		}
		if current.Block >= len(blocks) {
			continue
		}
		header := blocks[current.Block].Header
		if header < 0 || header >= len(commands) || commands[header].Function == nil {
			continue
		}
		if typeParameterName(commands[header].Function.TypeParameters, name) {
			return true
		}
	}
	return false
}

func typeParameterName(parameters []syntax.TypeParameter, name string) bool {
	for _, parameter := range parameters {
		if parameter.Name == name {
			return true
		}
	}
	return false
}

func typeDeclarationKind(kind SymbolKind) bool {
	switch kind {
	case SymbolKindClass, SymbolKindInterface, SymbolKindEnum, SymbolKindTypeAlias:
		return true
	default:
		return false
	}
}
