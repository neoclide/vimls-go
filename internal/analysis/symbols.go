// Package analysis contains protocol-independent information derived from a
// Vim syntax tree.
package analysis

import (
	"regexp"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// SymbolKind identifies the source construct represented by a Symbol.  The
// values deliberately do not use LSP's numeric SymbolKind values; the server
// can map these stable language concepts to any protocol at its boundary.
type SymbolKind string

const (
	SymbolKindImport      SymbolKind = "import"
	SymbolKindClass       SymbolKind = "class"
	SymbolKindInterface   SymbolKind = "interface"
	SymbolKindEnum        SymbolKind = "enum"
	SymbolKindEnumMember  SymbolKind = "enumMember"
	SymbolKindTypeAlias   SymbolKind = "typeAlias"
	SymbolKindFunction    SymbolKind = "function"
	SymbolKindMethod      SymbolKind = "method"
	SymbolKindConstructor SymbolKind = "constructor"
	SymbolKindVariable    SymbolKind = "variable"
	SymbolKindConstant    SymbolKind = "constant"
)

// Symbol is a source symbol with byte spans from its original syntax.File.
// Range is the complete source extent of the symbol where the syntax tree
// provides one; SelectionRange is the name span used for navigation.
type Symbol struct {
	Name           string
	Kind           SymbolKind
	Range          syntax.Span
	SelectionRange syntax.Span
	Detail         string
	Deprecated     bool
	Children       []*Symbol
}

var deprecatedCommentPrefix = regexp.MustCompile(`(?i)^#[ \t]*@?deprecated(?:$|[^[:alnum:]_])`)

// CollectSymbols returns document symbols in source order.  A syntax block
// is a container only when it belongs to a declaration-bearing construct
// (class, interface, enum, function, or def).  Control-flow blocks are
// skipped while walking Block.Parent, so a declaration inside an if/try/for
// still belongs to its nearest declaration container.
func CollectSymbols(file *syntax.File) []*Symbol {
	if file == nil {
		return nil
	}

	var roots []*Symbol
	collectSymbolsInList(file, file.Commands, file.Blocks, &roots, nil)
	return roots
}

// collectSymbolsInList walks one command list. Block indexes are local to the
// list, so each recursive call owns a separate container map. inherited is
// the nearest declaration container from the enclosing command list; control
// blocks in an embedded body do not create document symbols of their own.
func collectSymbolsInList(file *syntax.File, commands []syntax.Command, blocks []syntax.Block, roots *[]*Symbol, inherited *Symbol) {
	containers := make(map[int]*Symbol)
	for commandIndex := range commands {
		command := &commands[commandIndex]
		commandContainer := nearestContainer(blocks, command.Block, containers)
		if commandContainer == nil {
			commandContainer = inherited
		}

		if command.Aggregate != nil {
			symbol := aggregateSymbol(file, command, blocks)
			if symbol != nil {
				blockIndex := aggregateBlock(blocks, commandIndex, command.Block, command.Aggregate.Kind)
				parent := nearestContainer(blocks, parentBlock(blocks, blockIndex, command.Block), containers)
				if parent == nil {
					parent = inherited
				}
				appendNestedSymbol(roots, parent, symbol)
				if blockIndex >= 0 {
					containers[blockIndex] = symbol
				}
			}
		}

		if command.TypeAlias != nil {
			symbol := typeAliasSymbol(file, command)
			if symbol != nil {
				appendNestedSymbol(roots, commandContainer, symbol)
			}
		}

		if command.Import != nil {
			symbol := importSymbol(file, command)
			if symbol != nil {
				appendNestedSymbol(roots, commandContainer, symbol)
			}
		}

		if command.Function != nil {
			symbol := functionSymbol(file, command, commandContainer, blocks)
			if symbol != nil {
				blockIndex := ownBlock(blocks, commandIndex, command.Block)
				parent := commandContainer
				if blockIndex >= 0 {
					parent = nearestContainer(blocks, parentBlock(blocks, blockIndex, command.Block), containers)
					if parent == nil {
						parent = inherited
					}
				}
				appendNestedSymbol(roots, parent, symbol)
				if blockIndex >= 0 {
					containers[blockIndex] = symbol
				}
			}
		}

		if command.Declaration != nil {
			for _, binding := range command.Declaration.Bindings {
				if ignoredDestructuringBinding(file, command, binding.Name) {
					continue
				}
				symbol := declarationSymbol(file, command, binding)
				if symbol != nil {
					appendNestedSymbol(roots, commandContainer, symbol)
				}
			}
		}

		for _, value := range command.EnumValues {
			symbol := enumMemberSymbol(file, value)
			if symbol != nil {
				appendNestedSymbol(roots, commandContainer, symbol)
			}
		}
		// The syntax parser attaches a logical comma-separated enum line to
		// EnumValues.  A plain one-value-per-line enum entry can instead remain
		// an otherwise opaque command; its enum block is still authoritative.
		if len(command.EnumValues) == 0 && command.Canonical != "" && command.Canonical != "endenum" && !isBlockHeader(blocks, commandIndex, command.Block) {
			if commandContainer != nil && commandContainer.Kind == SymbolKindEnum {
				symbol := enumMemberSymbol(file, syntax.EnumValue{Name: command.Name})
				if symbol != nil {
					appendNestedSymbol(roots, commandContainer, symbol)
				}
			}
		}
		if command.Embedded != nil {
			collectSymbolsInList(file, command.Embedded.Commands, command.Embedded.Blocks, roots, commandContainer)
		}
	}
}

func aggregateSymbol(file *syntax.File, command *syntax.Command, blocks []syntax.Block) *Symbol {
	name := command.Aggregate.Name
	if !validSymbolSpan(file, name) {
		return nil
	}
	kind := SymbolKind(command.Aggregate.Kind)
	if kind != SymbolKindClass && kind != SymbolKindInterface && kind != SymbolKindEnum {
		return nil
	}
	rangeSpan := command.Span
	if blockIndex := command.Block; validBlock(blocks, blockIndex) {
		block := blocks[blockIndex]
		if block.Kind == command.Aggregate.Kind {
			rangeSpan = block.Span
		}
	}
	return &Symbol{
		Name:           file.Text(name),
		Kind:           kind,
		Range:          rangeSpan,
		SelectionRange: name,
		Detail:         string(kind),
	}
}

func typeAliasSymbol(file *syntax.File, command *syntax.Command) *Symbol {
	name := command.TypeAlias.Name
	if !validSymbolSpan(file, name) {
		return nil
	}
	detail := "type"
	if !emptySpan(command.TypeAlias.TypeSpan) {
		detail = "type = " + file.Text(command.TypeAlias.TypeSpan)
	}
	return &Symbol{
		Name:           file.Text(name),
		Kind:           SymbolKindTypeAlias,
		Range:          command.Span,
		SelectionRange: name,
		Detail:         detail,
	}
}

func importSymbol(file *syntax.File, command *syntax.Command) *Symbol {
	importNode := command.Import
	selection := importNode.Alias
	name := strings.TrimSpace(file.Text(selection))
	if !validSymbolSpan(file, selection) || name == "" {
		selection = importNode.PathSpan
		name = strings.TrimSpace(file.Text(selection))
	}
	// Keep malformed/incomplete imports visible and, importantly, never return
	// an empty symbol name.  The command name is an original source span too.
	if name == "" && validSymbolSpan(file, command.Name) {
		selection = command.Name
		name = strings.TrimSpace(file.Text(selection))
	}
	if name == "" || !validSymbolSpan(file, selection) {
		return nil
	}
	detail := "import"
	if importNode.Autoload {
		detail = "autoload import"
	}
	return &Symbol{
		Name:           name,
		Kind:           SymbolKindImport,
		Range:          command.Span,
		SelectionRange: selection,
		Detail:         detail,
	}
}

func functionSymbol(file *syntax.File, command *syntax.Command, parent *Symbol, blocks []syntax.Block) *Symbol {
	name := command.Function.Name
	if !validSymbolSpan(file, name) {
		return nil
	}
	kind := SymbolKindFunction
	if parent != nil && (parent.Kind == SymbolKindClass || parent.Kind == SymbolKindInterface) {
		kind = SymbolKindMethod
		if last := strings.LastIndex(file.Text(name), "."); strings.TrimSpace(file.Text(name)[last+1:]) == "new" {
			kind = SymbolKindConstructor
		}
	}
	rangeSpan := command.Span
	if blockIndex := ownBlock(blocks, -1, command.Block); validBlock(blocks, blockIndex) {
		block := blocks[blockIndex]
		if block.Kind == syntax.BlockFunction || block.Kind == syntax.BlockDef {
			rangeSpan = block.Span
		}
	}
	detail := command.Canonical
	if detail == "" {
		detail = command.TypedName
	}
	return &Symbol{
		Name:           file.Text(name),
		Kind:           kind,
		Range:          rangeSpan,
		SelectionRange: name,
		Detail:         detail,
		Deprecated:     hasDeprecatedComment(file, command),
	}
}

func declarationSymbol(file *syntax.File, command *syntax.Command, binding syntax.Binding) *Symbol {
	if !validSymbolSpan(file, binding.Name) {
		return nil
	}
	kind := SymbolKindVariable
	if command.Canonical == "const" || command.Canonical == "final" {
		kind = SymbolKindConstant
	}
	detail := command.Canonical
	if !emptySpan(binding.Type) {
		detail += ": " + file.Text(binding.Type)
	}
	return &Symbol{
		Name:           file.Text(binding.Name),
		Kind:           kind,
		Range:          command.Span,
		SelectionRange: binding.Name,
		Detail:         detail,
		Deprecated:     hasDeprecatedComment(file, command),
	}
}

func hasDeprecatedComment(file *syntax.File, command *syntax.Command) bool {
	if file == nil || command == nil || file.Dialect != syntax.Vim9 || command.Dialect != syntax.Vim9 || command.Span.Start <= 0 {
		return false
	}
	lineStart := strings.LastIndexAny(file.Source[:command.Span.Start], "\r\n") + 1
	for lineStart > 0 {
		lineEnd := lineStart - 1
		if lineEnd > 0 && file.Source[lineEnd] == '\n' && file.Source[lineEnd-1] == '\r' {
			lineEnd--
		}
		previousStart := strings.LastIndexAny(file.Source[:lineEnd], "\r\n") + 1
		first := previousStart
		for first < lineEnd && (file.Source[first] == ' ' || file.Source[first] == '\t') {
			first++
		}
		if first == lineEnd || !isCommentToken(file, first, lineEnd) {
			return false
		}
		if deprecatedCommentPrefix.MatchString(file.Source[first:lineEnd]) {
			return true
		}
		lineStart = previousStart
	}
	return false
}

func isCommentToken(file *syntax.File, start, end int) bool {
	for _, token := range file.Tokens {
		if token.Kind == syntax.TokenComment && token.Span.Start == start && token.Span.End == end {
			return true
		}
	}
	return false
}

func enumMemberSymbol(file *syntax.File, value syntax.EnumValue) *Symbol {
	if !validSymbolSpan(file, value.Name) {
		return nil
	}
	return &Symbol{
		Name:           file.Text(value.Name),
		Kind:           SymbolKindEnumMember,
		Range:          value.Name,
		SelectionRange: value.Name,
		Detail:         "enum member",
	}
}

func appendNestedSymbol(roots *[]*Symbol, parent *Symbol, symbol *Symbol) {
	if symbol == nil {
		return
	}
	if parent != nil {
		parent.Children = append(parent.Children, symbol)
		return
	}
	*roots = append(*roots, symbol)
}

func nearestContainer(blocks []syntax.Block, blockIndex int, containers map[int]*Symbol) *Symbol {
	for validBlock(blocks, blockIndex) {
		if symbol := containers[blockIndex]; symbol != nil {
			return symbol
		}
		blockIndex = blocks[blockIndex].Parent
	}
	return nil
}

func parentBlock(blocks []syntax.Block, own, fallback int) int {
	if validBlock(blocks, own) {
		return blocks[own].Parent
	}
	return fallback
}

// ownBlock returns the declaration block for a command.  Passing -1 as the
// command index is supported for callers that only have command.Block; the
// block kind check still prevents a control-flow block from being mistaken
// for a function container.
func ownBlock(blocks []syntax.Block, commandIndex, blockIndex int) int {
	if !validBlock(blocks, blockIndex) {
		return -1
	}
	block := blocks[blockIndex]
	if block.Kind != syntax.BlockFunction && block.Kind != syntax.BlockDef {
		return -1
	}
	if commandIndex >= 0 && block.Header != commandIndex {
		return -1
	}
	return blockIndex
}

func aggregateBlock(blocks []syntax.Block, commandIndex, blockIndex int, kind syntax.BlockKind) int {
	if !validBlock(blocks, blockIndex) || blocks[blockIndex].Kind != kind {
		return -1
	}
	if commandIndex >= 0 && blocks[blockIndex].Header != commandIndex {
		return -1
	}
	return blockIndex
}

func isBlockHeader(blocks []syntax.Block, commandIndex, blockIndex int) bool {
	return validBlock(blocks, blockIndex) && blocks[blockIndex].Header == commandIndex
}

func validBlock(blocks []syntax.Block, index int) bool {
	return index >= 0 && index < len(blocks)
}

func emptySpan(span syntax.Span) bool {
	return span.Start >= span.End
}

func validSymbolSpan(file *syntax.File, span syntax.Span) bool {
	return file != nil && span.Start >= 0 && span.Start < span.End && span.End <= len(file.Source)
}
