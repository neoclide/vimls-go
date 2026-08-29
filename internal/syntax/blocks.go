package syntax

import (
	"sort"
	"strings"
)

func buildBlocks(file *File) {
	const (
		classMethodAbstract uint8 = iota + 1
		classMethodRecovery
	)
	var stack []int
	enumValuesOpen := make(map[int]bool)
	invalidFor := make(map[int]bool)
	recoveryBlocks := make(map[int]bool)
	var multipleFinally map[int]bool
	interfaceMethod := make(map[int]bool)
	interfaceMethodInvalid := make(map[int]bool)
	// Vim9 :redir must be terminated before its enclosing :enddef. Keep this
	// state per definition so nested definitions do not affect one another.
	var redirOpen map[int]bool
	var classMethods map[int]uint8
	for commandIndex := range file.Commands {
		command := &file.Commands[commandIndex]
		defBlock := -1
		for index := len(stack) - 1; index >= 0; index-- {
			if file.Blocks[stack[index]].Kind == BlockDef {
				defBlock = stack[index]
				break
			}
		}
		if defBlock >= 0 && command.Dialect == Vim9 && command.Canonical == "redir" {
			argument := strings.TrimSpace(file.Text(command.Argument))
			if argument == "END" {
				delete(redirOpen, defBlock)
			} else if strings.HasPrefix(argument, "=>") {
				if redirOpen == nil {
					redirOpen = make(map[int]bool)
				}
				redirOpen[defBlock] = true
			}
		}
		if command.Dialect == Vim9 && command.Canonical == "enddef" && defBlock >= 0 && redirOpen[defBlock] {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1185", Message: "Missing :redir END", Span: command.Name,
			})
			delete(redirOpen, defBlock)
		}
		if len(stack) > 0 {
			blockIndex := stack[len(stack)-1]
			if file.Blocks[blockIndex].Kind == BlockClass && command.Dialect == Vim9 {
				classMethod := classMethods[blockIndex]
				if classMethod == classMethodRecovery {
					command.Block = blockIndex
					if command.Canonical == "endclass" {
						delete(classMethods, blockIndex)
						// Let the ordinary closer pop the class block.
					} else {
						if command.Canonical == "enddef" {
							delete(classMethods, blockIndex)
						}
						continue
					}
				}
				if classMethod == classMethodAbstract {
					if command.Canonical == "endclass" || isDirectAggregateMember(command) {
						delete(classMethods, blockIndex)
					} else {
						classMethods[blockIndex] = classMethodRecovery
						classBodyCommandDiagnostic(file, command)
						command.Block = blockIndex
						continue
					}
				}
				if command.Canonical == "def" && command.Argument.Start == command.Argument.End {
					command.Block = blockIndex
					if classMethods == nil {
						classMethods = make(map[int]uint8)
					}
					classMethods[blockIndex] = classMethodRecovery
					classBodyCommandDiagnostic(file, command)
					continue
				}
				if command.Canonical == "final" || command.Canonical == "const" {
					argument := file.Text(command.Argument)
					start := skipSpace(argument, 0, len(argument))
					if end := scanWord(argument, start, len(argument)); argument[start:end] == "def" {
						command.Block = blockIndex
						if classMethods == nil {
							classMethods = make(map[int]uint8)
						}
						classMethods[blockIndex] = classMethodRecovery
						continue
					}
				}
				if command.Canonical == "def" {
					for _, modifier := range command.Modifiers {
						if modifier.Name == "abstract" {
							if classMethods == nil {
								classMethods = make(map[int]uint8)
							}
							classMethods[blockIndex] = classMethodAbstract
							break
						}
					}
				}
				classModifierExpression := false
				if command.Kind == CommandExpression && len(command.Modifiers) > 0 {
					switch command.Modifiers[0].Name {
					case "abstract", "public", "static":
						classModifierExpression = true
					}
				}
				if command.Canonical != "endclass" && command.Canonical != "endinterface" && !isDirectAggregateMember(command) && !classModifierExpression {
					command.Block = blockIndex
					classBodyCommandDiagnostic(file, command)
					continue
				}
			}
			if file.Blocks[blockIndex].Kind == BlockInterface && command.Canonical != "endclass" {
				if command.Canonical == "def" {
					command.Block = blockIndex
					interfaceMethod[blockIndex] = true
					continue
				}
				if interfaceMethod[blockIndex] && command.Canonical == "enddef" {
					command.Block = blockIndex
					interfaceMethod[blockIndex] = false
					interfaceMethodInvalid[blockIndex] = false
					continue
				}
				if interfaceMethod[blockIndex] && interfaceMethodInvalid[blockIndex] {
					command.Block = blockIndex
					continue
				}
				if interfaceMethod[blockIndex] && command.Canonical != "endinterface" && !isDirectAggregateMember(command) {
					command.Block = blockIndex
					file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E1345", Message: "Not a valid command in an interface: " + file.Text(Span{Start: command.Name.Start, End: command.Argument.End}), Span: Span{Start: command.Name.Start, End: command.Argument.End}})
					interfaceMethodInvalid[blockIndex] = true
					continue
				}
				interfaceMethod[blockIndex] = false
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
			if isInvalidAbstractHeader(command) {
				recoveryBlocks[blockIndex] = true
			}
			if kind == BlockFor {
				argument := file.Text(command.Argument)
				invalidFor[blockIndex] = findTopLevelKeyword(argument, 0, len(argument), "in") < 0
			}
			stack = append(stack, blockIndex)
			if kind == BlockEnum {
				enumValuesOpen[blockIndex] = true
			}
			continue
		}
		if branchKind, ok := branchBlock(command.Canonical); ok {
			if len(stack) > 0 && file.Blocks[stack[len(stack)-1]].Kind == branchKind {
				blockIndex := stack[len(stack)-1]
				if command.Dialect == Vim9 && branchKind == BlockIf {
					seenElse := false
					for _, branch := range file.Blocks[blockIndex].Branches {
						if file.Commands[branch].Canonical == "else" {
							seenElse = true
							break
						}
					}
					if seenElse && command.Canonical == "else" {
						file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E583", Message: "multiple :else", Span: command.Name})
					} else if seenElse && command.Canonical == "elseif" {
						file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E584", Message: ":elseif after :else", Span: command.Name})
					}
				}
				if command.Canonical == "finally" {
					duplicate := false
					for _, branch := range file.Blocks[blockIndex].Branches {
						if file.Commands[branch].Canonical == "finally" {
							duplicate = true
							break
						}
					}
					if duplicate {
						file.Blocks[blockIndex].Branches = append(file.Blocks[blockIndex].Branches, commandIndex)
						command.Block = blockIndex
						file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E607", Message: "multiple :finally", Span: command.Name})
						if multipleFinally == nil {
							multipleFinally = make(map[int]bool)
						}
						multipleFinally[blockIndex] = true
						continue
					}
				}
				file.Blocks[blockIndex].Branches = append(file.Blocks[blockIndex].Branches, commandIndex)
				command.Block = blockIndex
			} else if match := recoverableBranchBlock(file, stack, branchKind, invalidFor); match >= 0 {
				blockIndex := stack[match]
				file.Blocks[blockIndex].Branches = append(file.Blocks[blockIndex].Branches, commandIndex)
				command.Block = blockIndex
				for _, recovered := range stack[match+1:] {
					file.Blocks[recovered].Span.End = command.Span.Start
				}
				recoveryBlocks[blockIndex] = true
				stack = stack[:match+1]
			} else {
				code := "vimls/unexpected-branch"
				message := "branch command has no matching block"
				standalone := len(stack) == 0
				if len(stack) > 0 {
					top := file.Blocks[stack[len(stack)-1]].Kind
					standalone = top == BlockDef || top == BlockFunction
				}
				if command.Dialect == Vim9 && standalone {
					switch command.Canonical {
					case "catch":
						code, message = "vim/E603", ":catch without :try"
					case "finally":
						code, message = "vim/E606", ":finally without :try"
					case "elseif":
						code, message = "vim/E582", ":elseif without :if"
					case "else":
						code, message = "vim/E581", ":else without :if"
					}
				}
				file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: code, Message: message, Span: command.Name})
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
				if stackHasInvalidFor(stack, invalidFor) {
					command.Block = stack[len(stack)-1]
					continue
				}
				if command.Dialect == Vim9 && command.Canonical == "endtry" && len(stack) > 0 {
					blockIndex := stack[len(stack)-1]
					if diagnostic, ok := vim9MissingBlockEndDiagnostic(file.Blocks[blockIndex].Kind, command.Name); ok {
						file.Diagnostics = append(file.Diagnostics, diagnostic)
						command.Block = blockIndex
						recoveryBlocks[blockIndex] = true
						continue
					}
				}
				// Vim9 reports a mismatched aggregate closer as E476 while
				// retaining the active aggregate. Ordinary unmatched closers keep
				// the generic recovery diagnostic.
				if command.Dialect == Vim9 && len(stack) > 0 {
					topKind := file.Blocks[stack[len(stack)-1]].Kind
					if (topKind == BlockClass && closeKind == BlockInterface) ||
						(topKind == BlockInterface && closeKind == BlockClass) {
						expected := "endclass"
						if topKind == BlockInterface {
							expected = "endinterface"
						}
						file.Diagnostics = append(file.Diagnostics, Diagnostic{
							Code: "vim/E476", Message: "Invalid command: " + command.Canonical + ", expected " + expected, Span: command.Name,
						})
						command.Block = stack[len(stack)-1]
						recoveryBlocks[stack[len(stack)-1]] = true
						continue
					}
				}
				code := "vimls/unexpected-end"
				message := "end command has no matching block"
				standalone := len(stack) == 0
				if len(stack) > 0 {
					top := file.Blocks[stack[len(stack)-1]].Kind
					standalone = top == BlockDef || top == BlockFunction
				}
				if command.Dialect == Vim9 && standalone {
					switch command.Canonical {
					case "endif":
						code, message = "vim/E580", ":endif without :if"
					case "endfor":
						code, message = "vim/E588", ":endfor without :for"
					case "endwhile":
						code, message = "vim/E588", ":endwhile without :while"
					case "endtry":
						code, message = "vim/E602", ":endtry without :try"
					}
				}
				file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: code, Message: message, Span: command.Name})
				continue
			}
			blockIndex := stack[match]
			recovering := stackHasInvalidFor(stack[match+1:], invalidFor)
			for _, unclosed := range stack[match+1:] {
				if recovering {
					file.Blocks[unclosed].Span.End = command.Span.Start
					continue
				}
				block := &file.Blocks[unclosed]
				if recoveryBlocks[unclosed] && closeKind == BlockDef {
					block.Span.End = command.Span.Start
					continue
				}
				if multipleFinally[unclosed] {
					block.Span.End = command.Span.Start
					continue
				}
				if closeKind == BlockFunction && implicitlyClosedByFunction(block.Kind) {
					// Vim ends an unfinished control block when :endfunction is
					// encountered.  Keep the block in the tree and use the
					// function terminator as its effective end, but do not report a
					// missing-end diagnostic for valid legacy function bodies.
					block.Span.End = command.Span.End
					block.End = commandIndex
					continue
				}
				span := file.Commands[block.Header].Name
				if command.Dialect == Vim9 && (closeKind == BlockDef || command.Canonical == "endtry") {
					if diagnostic, ok := vim9MissingBlockEndDiagnostic(block.Kind, span); ok {
						file.Diagnostics = append(file.Diagnostics, diagnostic)
						continue
					}
				}
				file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vimls/missing-end", Message: "block is missing its end command", Span: span})
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
		if recoveryBlocks[blockIndex] || multipleFinally[blockIndex] || blockWithinInvalidFor(file, blockIndex, invalidFor) || suppressMissing[blockIndex] {
			continue
		}
		block := &file.Blocks[blockIndex]
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vimls/missing-end", Message: "block is missing its end command", Span: file.Commands[block.Header].Name,
		})
	}
}

// isInvalidAbstractHeader identifies the Vim9 form where the abstract
// modifier is followed by neither a declaration nor an aggregate opener.
// Keeping this predicate shared by scanning and block construction ensures
// the retained command can still pair its following endclass during recovery.
func isInvalidAbstractHeader(command *Command) bool {
	if command == nil || command.Dialect != Vim9 || command.Kind != CommandUnknown {
		return false
	}
	for _, modifier := range command.Modifiers {
		if modifier.Name == "abstract" {
			return true
		}
	}
	return false
}

func stackHasInvalidFor(stack []int, invalidFor map[int]bool) bool {
	for _, blockIndex := range stack {
		if invalidFor[blockIndex] {
			return true
		}
	}
	return false
}

func blockWithinInvalidFor(file *File, blockIndex int, invalidFor map[int]bool) bool {
	for blockIndex >= 0 {
		if invalidFor[blockIndex] {
			return true
		}
		blockIndex = file.Blocks[blockIndex].Parent
	}
	return false
}

func recoverableBranchBlock(file *File, stack []int, kind BlockKind, invalidFor map[int]bool) int {
	for index := len(stack) - 1; index >= 0; index-- {
		if file.Blocks[stack[index]].Kind != kind {
			continue
		}
		if stackHasInvalidFor(stack[index:], invalidFor) {
			return index
		}
		break
	}
	return -1
}

func buildAggregateMembers(file *File) {
	for header := range file.Commands {
		aggregate := file.Commands[header].Aggregate
		if aggregate == nil {
			continue
		}
		aggregate.Members = aggregate.Members[:0]
		blockIndex := file.Commands[header].Block
		if blockIndex < 0 || blockIndex >= len(file.Blocks) || file.Blocks[blockIndex].Header != header {
			continue
		}
		end := len(file.Commands)
		if file.Blocks[blockIndex].End >= 0 {
			end = file.Blocks[blockIndex].End
		}
		for index := header + 1; index < end; index++ {
			command := &file.Commands[index]
			if command.Block == blockIndex {
				if isDirectAggregateMember(command) {
					aggregate.Members = append(aggregate.Members, index)
				}
				continue
			}
			if command.Block >= 0 && command.Block < len(file.Blocks) {
				block := file.Blocks[command.Block]
				if block.Kind == BlockDef && block.Parent == blockIndex && block.Header == index {
					aggregate.Members = append(aggregate.Members, index)
				}
			}
		}
	}
}

func isDirectAggregateMember(command *Command) bool {
	if len(command.EnumValues) > 0 {
		return true
	}
	switch command.Canonical {
	case "var", "const", "final", "def":
		return true
	default:
		return false
	}
}

func classBodyCommandDiagnostic(file *File, command *Command) {
	start := command.Span.Start
	end := command.Span.End
	file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E1318", Message: "Not a valid command in a class: " + file.Source[start:end], Span: Span{Start: start, End: end}})
}

func suppressClassBodyCommandDiagnostics(file *File) {
	var modifierCommands map[Span]Span
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Dialect != Vim9 || command.Block < 0 || command.Block >= len(file.Blocks) || file.Blocks[command.Block].Kind != BlockClass || len(command.Modifiers) == 0 {
			continue
		}
		if modifierCommands == nil {
			modifierCommands = make(map[Span]Span)
		}
		end := command.Span.End
		if command.Argument.End > end {
			end = command.Argument.End
		}
		modifierCommands[command.Modifiers[0].Span] = Span{Start: command.Span.Start, End: end}
	}

	var invalid []Span
	for _, diagnostic := range file.Diagnostics {
		switch diagnostic.Code {
		case "vim/E1318":
			invalid = append(invalid, diagnostic.Span)
		case "vim/E1331", "vim/E1368", "vim/E1371":
			if command, ok := modifierCommands[diagnostic.Span]; ok {
				invalid = append(invalid, command)
			}
		}
	}
	if len(invalid) == 0 {
		return
	}
	sort.Slice(invalid, func(left, right int) bool {
		if invalid[left].End == invalid[right].End {
			return invalid[left].Start < invalid[right].Start
		}
		return invalid[left].End < invalid[right].End
	})
	kept := file.Diagnostics[:0]
	for _, diagnostic := range file.Diagnostics {
		switch diagnostic.Code {
		case "vim/E1318", "vim/E1331", "vim/E1368", "vim/E1371":
			kept = append(kept, diagnostic)
			continue
		}
		index := sort.Search(len(invalid), func(index int) bool {
			return invalid[index].End >= diagnostic.Span.End
		})
		if index < len(invalid) && diagnostic.Span.Start >= invalid[index].Start {
			continue
		}
		kept = append(kept, diagnostic)
	}
	file.Diagnostics = kept
}

func suppressInvalidBlockMissingEnds(file *File) {
	var invalid map[Span]bool
	for _, block := range file.Blocks {
		if block.End < 0 && (block.Kind == BlockDef || file.Commands[block.Header].Dialect == Vim9) && blockHeaderHasVimDiagnostic(file, block.Header) {
			if invalid == nil {
				invalid = make(map[Span]bool)
			}
			invalid[file.Commands[block.Header].Name] = true
		}
	}
	if len(invalid) == 0 {
		return
	}
	kept := file.Diagnostics[:0]
	for _, diagnostic := range file.Diagnostics {
		if invalid[diagnostic.Span] {
			switch diagnostic.Code {
			case "vimls/missing-end", "vim/E170", "vim/E171", "vim/E600":
				continue
			}
		}
		kept = append(kept, diagnostic)
	}
	file.Diagnostics = kept
}

func suppressInvalidInterfaceInitializers(file *File) {
	var invalid []Block
	for _, block := range file.Blocks {
		if block.Kind != BlockInterface {
			continue
		}
		header := file.Commands[block.Header]
		for _, diagnostic := range file.Diagnostics {
			if strings.HasPrefix(diagnostic.Code, "vim/") && diagnostic.Span.Start >= header.Span.Start && diagnostic.Span.End <= header.Span.End {
				invalid = append(invalid, block)
				break
			}
		}
	}
	if len(invalid) == 0 {
		return
	}
	kept := file.Diagnostics[:0]
	for _, diagnostic := range file.Diagnostics {
		suppress := false
		if diagnostic.Code == "vim/E1344" {
			for _, block := range invalid {
				header := file.Commands[block.Header]
				if diagnostic.Span.Start >= header.Span.End && diagnostic.Span.End <= block.Span.End {
					suppress = true
					break
				}
			}
		}
		if !suppress {
			kept = append(kept, diagnostic)
		}
	}
	file.Diagnostics = kept
}

func blockHeaderHasVimDiagnostic(file *File, headerIndex int) bool {
	header := file.Commands[headerIndex]
	for _, diagnostic := range file.Diagnostics {
		if strings.HasPrefix(diagnostic.Code, "vim/") && diagnostic.Span.Start >= header.Argument.Start && diagnostic.Span.End <= header.Argument.End {
			return true
		}
	}
	return false
}

func implicitlyClosedByFunction(kind BlockKind) bool {
	switch kind {
	case BlockIf, BlockFor, BlockWhile, BlockTry:
		return true
	default:
		return false
	}
}

func vim9MissingBlockEndDiagnostic(kind BlockKind, span Span) (Diagnostic, bool) {
	switch kind {
	case BlockIf:
		return Diagnostic{Code: "vim/E171", Message: "missing :endif", Span: span}, true
	case BlockFor:
		return Diagnostic{Code: "vim/E170", Message: "missing :endfor", Span: span}, true
	case BlockWhile:
		return Diagnostic{Code: "vim/E170", Message: "missing :endwhile", Span: span}, true
	case BlockTry:
		return Diagnostic{Code: "vim/E600", Message: "missing :endtry", Span: span}, true
	default:
		return Diagnostic{}, false
	}
}

func openingBlock(file *File, command *Command) (BlockKind, bool) {
	if isInvalidAbstractHeader(command) {
		return BlockClass, true
	}
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
