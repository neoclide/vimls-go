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
		memberStart := p.offset
		p.skipSpace()
		if p.offset >= len(p.source) || strings.ContainsRune(",>)", rune(p.source[p.offset])) {
			missing := &Type{Kind: TypeMissing, Span: p.span(p.offset, p.offset)}
			p.diagnostics = append(p.diagnostics, Diagnostic{
				Code: "vim/E1010", Message: "type not recognized",
				Span: p.span(memberStart, p.offset),
			})
			return &Type{Kind: TypeVariadic, Span: p.span(start, p.offset), Name: "...", Arguments: []*Type{missing}}
		}
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
	singleMemberContainer := name == "list" || name == "dict" || name == "object"
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
		if name == "tuple" && p.offset < len(p.source) && isExpressionSpace(p.source[p.offset]) {
			p.diagnostics = append(p.diagnostics, Diagnostic{
				Code: "vim/E1010", Message: "type not recognized",
				Span: p.span(p.offset, p.offset+1),
			})
		}
		p.skipSpace()
		for p.offset < len(p.source) && p.source[p.offset] != '>' {
			argument := p.parseType()
			node.Arguments = append(node.Arguments, argument)
			spaceBeforeComma := false
			invalidVariadic := false
			if name == "tuple" {
				if argument.Kind == TypeVariadic && len(argument.Arguments) > 0 && isKnownNonListType(argument.Arguments[0]) {
					invalidVariadic = true
					p.diagnostics = append(p.diagnostics, Diagnostic{
						Code: "vim/E1539", Message: "variadic tuple must end with a list type",
						Span: argument.Arguments[0].Span,
					})
				}
				// A tuple type has stricter separator whitespace than the
				// other generic types.  Keep the argument node's end so a
				// space before the comma or closing angle is observable.
				argumentEnd := argument.Span.End - p.base
				p.skipSpace()
				if p.offset < len(p.source) && p.source[p.offset] == ',' {
					if argument.Kind == TypeVariadic && !invalidVariadic {
						p.diagnostics = append(p.diagnostics, Diagnostic{
							Code: "vim/E1008", Message: "missing <type> after variadic tuple member",
							Span: p.span(p.offset, p.offset+1),
						})
					} else if p.offset > argumentEnd && argument.Kind != TypeVariadic {
						spaceBeforeComma = true
						p.diagnostics = append(p.diagnostics, Diagnostic{
							Code: "vim/E1068", Message: "no white space allowed before ','",
							Span: p.span(argumentEnd, p.offset+1),
						})
					}
				} else if p.offset < len(p.source) && p.source[p.offset] == '>' && p.offset > argumentEnd {
					p.diagnostics = append(p.diagnostics, Diagnostic{
						Code: "vim/E488", Message: "trailing characters",
						Span: p.span(argumentEnd, p.offset),
					})
				}
			} else {
				p.skipSpace()
			}
			if p.offset >= len(p.source) || p.source[p.offset] != ',' {
				break
			}
			p.offset++
			if name == "tuple" && argument.Kind != TypeVariadic && !spaceBeforeComma && (p.offset >= len(p.source) || !isExpressionSpace(p.source[p.offset])) {
				p.diagnostics = append(p.diagnostics, Diagnostic{
					Code: "vim/E1069", Message: "white space required after ','",
					Span: p.span(p.offset-1, p.offset),
				})
			}
			p.skipSpace()
		}
		if containerType && len(node.Arguments) == 0 {
			p.diagnostics = append(p.diagnostics, Diagnostic{
				Code: "vim/E1008", Message: "missing <type> after " + name,
				Span: p.span(nameStart, p.offset),
			})
		}
		tooManyMembers := singleMemberContainer && len(node.Arguments) > 1
		if tooManyMembers {
			p.diagnostics = append(p.diagnostics, Diagnostic{
				Code: "vim/E1009", Message: "missing > after type",
				Span: Span{Start: node.Arguments[1].Span.Start, End: node.Arguments[len(node.Arguments)-1].Span.End},
			})
		}
		if p.offset < len(p.source) && p.source[p.offset] == '>' {
			p.offset++
		} else if singleMemberContainer {
			if len(node.Arguments) > 0 && !tooManyMembers {
				p.diagnostics = append(p.diagnostics, Diagnostic{
					Code: "vim/E1009", Message: "missing > after type",
					Span: p.span(p.offset, p.offset),
				})
			}
		} else {
			p.consumeTypeClosing('>')
		}
		node.Span.End = p.base + p.offset
	}
	if name == "func" {
		node.Kind = TypeFunction
		nameEnd := node.Span.End - p.base
		if p.offset > nameEnd && p.offset < len(p.source) && (p.source[p.offset] == '(' || p.source[p.offset] == ':') {
			p.diagnostics = append(p.diagnostics, Diagnostic{
				Code: "vim/E488", Message: "trailing characters",
				Span: p.span(nameEnd, p.offset),
			})
		}
		if p.offset < len(p.source) && p.source[p.offset] == '(' {
			p.offset++
			if p.offset < len(p.source) && isExpressionSpace(p.source[p.offset]) {
				p.diagnostics = append(p.diagnostics, Diagnostic{
					Code: "vim/E1010", Message: "type not recognized",
					Span: p.span(p.offset, p.offset+1),
				})
			}
			p.skipSpace()
			optionalSeen := false
			for p.offset < len(p.source) && p.source[p.offset] != ')' {
				argument := p.parseType()
				node.Arguments = append(node.Arguments, argument)
				if argument.Kind == TypeOptional {
					optionalSeen = true
				} else if argument.Kind == TypeVariadic {
					if len(argument.Arguments) > 0 && isKnownNonListType(argument.Arguments[0]) {
						p.diagnostics = append(p.diagnostics, Diagnostic{
							Code: "vim/E1180", Message: "variable arguments type must be a list",
							Span: argument.Arguments[0].Span,
						})
					}
				} else if optionalSeen {
					p.diagnostics = append(p.diagnostics, Diagnostic{
						Code: "vim/E1007", Message: "mandatory argument after optional argument",
						Span: argument.Span,
					})
				}
				argumentEnd := argument.Span.End - p.base
				p.skipSpace()
				spaceBeforeComma := p.offset < len(p.source) && p.source[p.offset] == ',' && p.offset > argumentEnd
				variadicHasFollower := argument.Kind == TypeVariadic && p.offset < len(p.source) && p.source[p.offset] == ','
				if variadicHasFollower {
					p.diagnostics = append(p.diagnostics, Diagnostic{
						Code: "vim/E110", Message: "missing ')' after function type arguments",
						Span: p.span(p.offset, p.offset+1),
					})
				} else if spaceBeforeComma {
					p.diagnostics = append(p.diagnostics, Diagnostic{
						Code: "vim/E1068", Message: "no white space allowed before ','",
						Span: p.span(argumentEnd, p.offset+1),
					})
				} else if p.offset < len(p.source) && p.source[p.offset] == ')' && p.offset > argumentEnd {
					p.diagnostics = append(p.diagnostics, Diagnostic{
						Code: "vim/E488", Message: "trailing characters",
						Span: p.span(argumentEnd, p.offset),
					})
				}
				if p.offset >= len(p.source) || p.source[p.offset] != ',' {
					break
				}
				p.offset++
				if !variadicHasFollower && !spaceBeforeComma && (p.offset >= len(p.source) || !isExpressionSpace(p.source[p.offset])) {
					p.diagnostics = append(p.diagnostics, Diagnostic{
						Code: "vim/E1069", Message: "white space required after ','",
						Span: p.span(p.offset-1, p.offset),
					})
				}
				p.skipSpace()
			}
			p.consumeTypeClosing(')')
		}
		beforeSpace := p.offset
		p.skipSpace()
		if p.offset < len(p.source) && p.source[p.offset] == ':' {
			if p.offset > beforeSpace {
				p.diagnostics = append(p.diagnostics, Diagnostic{
					Code: "vim/E488", Message: "trailing characters",
					Span: p.span(beforeSpace, p.offset),
				})
			}
			p.offset++
			if p.offset >= len(p.source) || !isExpressionSpace(p.source[p.offset]) {
				p.diagnostics = append(p.diagnostics, Diagnostic{
					Code: "vim/E1069", Message: "white space required after ':'",
					Span: p.span(p.offset-1, p.offset),
				})
			}
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

func isKnownNonListType(node *Type) bool {
	switch node.Name {
	case "any", "bool", "blob", "channel", "float", "func", "job", "number", "string", "void":
		return true
	case "dict", "object", "tuple":
		return node.Kind == TypeGeneric
	default:
		return false
	}
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
