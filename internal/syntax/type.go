package syntax

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type TypeKind uint8

const (
	TypeMissing TypeKind = iota
	TypeNamed
	TypeGeneric
	TypeFunction
	TypeVariadic
	TypeOptional
)

type Type struct {
	Kind       TypeKind
	Span       Span
	Name       string
	Arguments  []*Type
	ReturnType *Type
}

// Vim9TypeParser parses the type grammar used by declarations, functions,
// tuples, classes, enums, aliases, and generic functions through Vim 9.2.1015.
type Vim9TypeParser struct{}

func (Vim9TypeParser) Parse(source string) (*Type, []Diagnostic) {
	parser := typeParser{source: source}
	typeNode := parser.parseType()
	parser.skipSpace()
	if parser.offset < len(source) {
		parser.diagnostics = append(parser.diagnostics, Diagnostic{Code: "vimls/trailing-type", Message: "unexpected text after type", Span: Span{Start: parser.offset, End: parser.offset + 1}})
	}
	return typeNode, parser.diagnostics
}

type typeParser struct {
	source      string
	base        int
	offset      int
	depth       int
	diagnostics []Diagnostic
}

func parseTypeAt(source string, base int) (*Type, []Diagnostic) {
	parser := typeParser{source: source, base: base}
	typeNode := parser.parseType()
	parser.skipSpace()
	if parser.offset < len(source) {
		parser.diagnostics = append(parser.diagnostics, Diagnostic{Code: "vimls/trailing-type", Message: "unexpected text after type", Span: Span{Start: base + parser.offset, End: base + parser.offset + 1}})
	}
	return typeNode, parser.diagnostics
}

func (p *typeParser) parseType() *Type {
	p.depth++
	defer func() { p.depth-- }()
	p.skipSpace()
	start := p.offset
	if p.depth > 128 {
		p.diagnostics = append(p.diagnostics, Diagnostic{Code: "vimls/type-too-deep", Message: "type nesting exceeds parser limit", Span: p.span(start, start)})
		return &Type{Kind: TypeMissing, Span: p.span(start, start)}
	}
	if strings.HasPrefix(p.source[p.offset:], "...") {
		p.offset += 3
		member := p.parseType()
		return &Type{Kind: TypeVariadic, Span: p.span(start, member.Span.End-p.base), Name: "...", Arguments: []*Type{member}}
	}
	if p.offset < len(p.source) && p.source[p.offset] == '?' {
		p.offset++
		member := p.parseType()
		return &Type{Kind: TypeOptional, Span: p.span(start, member.Span.End-p.base), Name: "?", Arguments: []*Type{member}}
	}
	nameStart := p.offset
	for {
		segmentStart := p.offset
		for p.offset < len(p.source) {
			r, size := utf8.DecodeRuneInString(p.source[p.offset:])
			if r != '_' && r != '#' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				break
			}
			p.offset += size
		}
		if p.offset == segmentStart || p.offset >= len(p.source) || p.source[p.offset] != '.' {
			break
		}
		p.offset++
	}
	if p.offset == nameStart {
		end := p.offset
		if end < len(p.source) {
			end++
			p.offset = end
		}
		span := p.span(start, end)
		p.diagnostics = append(p.diagnostics, Diagnostic{Code: "vimls/missing-type", Message: "expected Vim9 type", Span: span})
		return &Type{Kind: TypeMissing, Span: span}
	}
	name := p.source[nameStart:p.offset]
	node := &Type{Kind: TypeNamed, Span: p.span(start, p.offset), Name: name}
	containerType := name == "list" || name == "dict" || name == "tuple" || name == "object"
	// Vim9 does not allow whitespace between a container type name and its
	// type argument opener.  Whitespace inside the angle brackets is fine,
	// and retaining the opener lets recovery consume the complete type.
	if p.offset >= len(p.source) || p.source[p.offset] != '<' {
		beforeSpace := p.offset
		p.skipSpace()
		if p.offset < len(p.source) && p.source[p.offset] == '<' {
			if containerType {
				p.diagnostics = append(p.diagnostics, Diagnostic{
					Code: "vim/E1068", Message: "no white space allowed before '<'",
					Span: p.span(beforeSpace, p.offset+1),
				})
			}
		} else if containerType {
			p.diagnostics = append(p.diagnostics, Diagnostic{
				Code: "vim/E1008", Message: "missing <type> after " + name,
				Span: p.span(nameStart, p.offset),
			})
		}
	}
	if p.offset < len(p.source) && p.source[p.offset] == '<' {
		node.Kind = TypeGeneric
		p.offset++
		p.skipSpace()
		for p.offset < len(p.source) && p.source[p.offset] != '>' {
			node.Arguments = append(node.Arguments, p.parseType())
			p.skipSpace()
			if p.offset >= len(p.source) || p.source[p.offset] != ',' {
				break
			}
			p.offset++
			p.skipSpace()
		}
		if containerType && len(node.Arguments) == 0 {
			p.diagnostics = append(p.diagnostics, Diagnostic{
				Code: "vim/E1008", Message: "missing <type> after " + name,
				Span: p.span(nameStart, p.offset),
			})
		}
		p.consumeTypeClosing('>')
		node.Span.End = p.base + p.offset
	}
	if name == "func" {
		node.Kind = TypeFunction
		p.skipSpace()
		if p.offset < len(p.source) && p.source[p.offset] == '(' {
			p.offset++
			p.skipSpace()
			for p.offset < len(p.source) && p.source[p.offset] != ')' {
				node.Arguments = append(node.Arguments, p.parseType())
				p.skipSpace()
				if p.offset >= len(p.source) || p.source[p.offset] != ',' {
					break
				}
				p.offset++
				p.skipSpace()
			}
			p.consumeTypeClosing(')')
		}
		p.skipSpace()
		if p.offset < len(p.source) && p.source[p.offset] == ':' {
			p.offset++
			node.ReturnType = p.parseType()
		}
		node.Span.End = p.base + p.offset
	}
	return node
}

func (p *typeParser) consumeTypeClosing(expected byte) {
	if p.offset < len(p.source) && p.source[p.offset] == expected {
		p.offset++
		return
	}
	span := p.span(p.offset, p.offset)
	p.diagnostics = append(p.diagnostics, Diagnostic{Code: "vimls/missing-type-delimiter", Message: "expected " + string(expected), Span: span})
}

func (p *typeParser) skipSpace() {
	for p.offset < len(p.source) {
		if isExpressionSpace(p.source[p.offset]) || isLineLeadingBackslash(p.source, p.offset) {
			p.offset++
			continue
		}
		break
	}
}

func (p *typeParser) span(start, end int) Span {
	return Span{Start: p.base + start, End: p.base + end}
}
