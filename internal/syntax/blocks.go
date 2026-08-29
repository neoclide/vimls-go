package syntax

import "strings"

func buildBlocks(file *File) {
	var stack []int
	enumValuesOpen := make(map[int]bool)
	for commandIndex := range file.Commands {
		command := &file.Commands[commandIndex]
		if len(stack) > 0 {
			blockIndex := stack[len(stack)-1]
			if file.Blocks[blockIndex].Kind == BlockInterface && command.Canonical == "def" {
				command.Block = blockIndex
				continue
			}
			if file.Blocks[blockIndex].Kind == BlockEnum && enumValuesOpen[blockIndex] {
				if closeKind, closing := closingBlock(file, command); !closing || closeKind != BlockEnum {
					command.Block = blockIndex
					enumValuesOpen[blockIndex] = parseEnumValues(file, command)
					continue
				}
			}
		}
		if kind, ok := openingBlock(file, command); ok {
			parent := -1
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			blockIndex := len(file.Blocks)
			file.Blocks = append(file.Blocks, Block{Kind: kind, Span: Span{Start: command.Span.Start, End: len(file.Source)}, Header: commandIndex, End: -1, Parent: parent})
			command.Block = blockIndex
			stack = append(stack, blockIndex)
			if kind == BlockEnum {
				enumValuesOpen[blockIndex] = true
			}
			continue
		}
		if branchKind, ok := branchBlock(command.Canonical); ok {
			if len(stack) > 0 && file.Blocks[stack[len(stack)-1]].Kind == branchKind {
				blockIndex := stack[len(stack)-1]
				file.Blocks[blockIndex].Branches = append(file.Blocks[blockIndex].Branches, commandIndex)
				command.Block = blockIndex
			} else {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vimls/unexpected-branch", Message: "branch command has no matching block", Span: command.Name})
			}
			continue
		}
		closeKind, closes := closingBlock(file, command)
		if command.Kind == CommandBlockEnd {
			for index := len(stack) - 1; index >= 0; index-- {
				kind := file.Blocks[stack[index]].Kind
				if kind == BlockCommand || kind == BlockScope {
					closeKind = kind
					closes = true
					break
				}
			}
			if !closes {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vimls/unexpected-end", Message: "end command has no matching block", Span: command.Name})
				continue
			}
		}
		if closes {
			match := -1
			for index := len(stack) - 1; index >= 0; index-- {
				if file.Blocks[stack[index]].Kind == closeKind {
					match = index
					break
				}
			}
			if match < 0 {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vimls/unexpected-end", Message: "end command has no matching block", Span: command.Name})
				continue
			}
			blockIndex := stack[match]
			for _, unclosed := range stack[match+1:] {
				block := &file.Blocks[unclosed]
				if closeKind == BlockFunction && implicitlyClosedByFunction(block.Kind) {
					// Vim ends an unfinished control block when :endfunction is
					// encountered.  Keep the block in the tree and use the
					// function terminator as its effective end, but do not report a
					// missing-end diagnostic for valid legacy function bodies.
					block.Span.End = command.Span.End
					block.End = commandIndex
					continue
				}
				file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vimls/missing-end", Message: "block is missing its end command", Span: file.Commands[block.Header].Name})
			}
			file.Blocks[blockIndex].Span.End = command.Span.End
			file.Blocks[blockIndex].End = commandIndex
			command.Block = blockIndex
			if closeKind == BlockFunction && command.Dialect == Legacy && command.Bang.Start < command.Bang.End {
				// A legacy function definition is collected before its body is
				// executed.  Vim recognizes `endfunction!` as the terminator there;
				// the trailing ! is only W22 when 'verbose' is non-zero.  Keep the
				// bang span, but do not turn that context-dependent warning into a
				// syntax error.  A top-level endfunction! never reaches this matched
				// block path and still reports E477-like unexpected-bang recovery.
				kept := file.Diagnostics[:0]
				for _, diagnostic := range file.Diagnostics {
					if diagnostic.Code == "vimls/unexpected-bang" && diagnostic.Span == command.Bang {
						continue
					}
					kept = append(kept, diagnostic)
				}
				file.Diagnostics = kept
			}
			stack = stack[:match]
			continue
		}
		if len(stack) > 0 {
			command.Block = stack[len(stack)-1]
		}
	}
	suppressMissing := make(map[int]bool)
	for commandIndex := range file.Commands {
		command := &file.Commands[commandIndex]
		incompleteTextBody := command.TextBody != nil && command.TextBody.Incomplete
		incompleteHeredoc := command.Heredoc != nil && command.Heredoc.Incomplete
		if !incompleteTextBody && !incompleteHeredoc {
			continue
		}
		functionBlock := -1
		commandBlock := -1
		for blockIndex := command.Block; blockIndex >= 0; blockIndex = file.Blocks[blockIndex].Parent {
			suppressMissing[blockIndex] = true
			if functionBlock < 0 && (file.Blocks[blockIndex].Kind == BlockFunction || file.Blocks[blockIndex].Kind == BlockDef) {
				functionBlock = blockIndex
			}
			if commandBlock < 0 && file.Blocks[blockIndex].Kind == BlockCommand {
				commandBlock = blockIndex
			}
		}
		if incompleteTextBody && functionBlock >= 0 {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1145", Message: "missing heredoc end marker: .", Span: command.Name,
			})
		}
		if incompleteHeredoc && commandBlock < 0 {
			code := "vim/E990"
			message := "missing end marker '" + command.Heredoc.Marker + "'"
			if functionBlock >= 0 {
				code = "vim/E1145"
				message = "missing heredoc end marker: " + command.Heredoc.Marker
			}
			file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: code, Message: message, Span: command.Argument})
		}
	}
	for _, blockIndex := range stack {
		if suppressMissing[blockIndex] {
			continue
		}
		block := &file.Blocks[blockIndex]
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vimls/missing-end", Message: "block is missing its end command", Span: file.Commands[block.Header].Name,
		})
	}
}

func implicitlyClosedByFunction(kind BlockKind) bool {
	switch kind {
	case BlockIf, BlockFor, BlockWhile, BlockTry:
		return true
	default:
		return false
	}
}

func openingBlock(file *File, command *Command) (BlockKind, bool) {
	switch command.Canonical {
	case "if":
		return BlockIf, true
	case "for":
		return BlockFor, true
	case "while":
		return BlockWhile, true
	case "try":
		return BlockTry, true
	case "function":
		return BlockFunction, isFunctionDefinition(file.Text(command.Argument))
	case "def":
		for _, modifier := range command.Modifiers {
			if modifier.Name == "abstract" {
				return "", false
			}
		}
		return BlockDef, true
	case "class":
		return BlockClass, true
	case "interface":
		return BlockInterface, true
	case "enum":
		return BlockEnum, true
	case "command":
		if command.Dialect == Vim9 {
			_, _, ok := collectedCommandBlockStart(file.Source, command, command.Argument.End)
			if ok {
				return BlockCommand, true
			}
		}
	case "{":
		if command.Dialect == Vim9 {
			return BlockScope, true
		}
	case "abstract":
		if strings.HasPrefix(strings.TrimSpace(file.Text(command.Argument)), "class") {
			return BlockClass, true
		}
	}
	return "", false
}

func isFunctionDefinition(source string) bool {
	source = strings.TrimSpace(source)
	return source != "" && source[0] != '/' && strings.ContainsRune(source, '(')
}

func branchBlock(command string) (BlockKind, bool) {
	switch command {
	case "else", "elseif":
		return BlockIf, true
	case "catch", "finally":
		return BlockTry, true
	default:
		return "", false
	}
}

func closingBlock(file *File, command *Command) (BlockKind, bool) {
	switch command.Canonical {
	case "endif":
		return BlockIf, true
	case "endfor":
		return BlockFor, true
	case "endwhile":
		return BlockWhile, true
	case "endtry":
		return BlockTry, true
	case "endfunction":
		return BlockFunction, true
	case "enddef":
		return BlockDef, true
	case "endclass":
		return BlockClass, true
	case "endinterface":
		return BlockInterface, true
	case "endenum":
		return BlockEnum, true
	default:
		return "", false
	}
}
