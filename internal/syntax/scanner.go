package syntax

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/chemzqm/vimls-go/internal/vimdata"
)

type modifierInfo struct {
	name string
	min  int
}

var modifiers = []modifierInfo{
	{name: "aboveleft", min: 3},
	{name: "belowright", min: 3},
	{name: "browse", min: 3},
	{name: "botright", min: 2},
	{name: "confirm", min: 4},
	{name: "keepmarks", min: 3},
	{name: "keepalt", min: 5},
	{name: "keeppatterns", min: 5},
	{name: "keepjumps", min: 5},
	{name: "filter", min: 4},
	{name: "horizontal", min: 3},
	{name: "hide", min: 3},
	{name: "lockmarks", min: 3},
	{name: "legacy", min: 3},
	{name: "leftabove", min: 5},
	{name: "noautocmd", min: 3},
	{name: "noswapfile", min: 3},
	{name: "rightbelow", min: 6},
	{name: "sandbox", min: 3},
	{name: "silent", min: 3},
	{name: "tab", min: 3},
	{name: "topleft", min: 2},
	{name: "unsilent", min: 3},
	{name: "vertical", min: 4},
	{name: "vim9cmd", min: 4},
	{name: "verbose", min: 4},
	{name: "export", min: 6},
	{name: "public", min: 3},
	{name: "abstract", min: 3},
	{name: "static", min: 4},
}

func parseSource(source string, initial Dialect) *File {
	file := &File{Dialect: initial, Source: source}
	active := initial
	scriptVersion := uint8(1)
	vim9Prologue := initial == Vim9 && startsWithVim9Script(source)
	if vim9Prologue {
		active = Legacy
	}
	var dialectStack []Dialect
	heredocCommand := -1
	heredocRecoveryCommand := ""
	heredocRecoveryOffset := -1
	var heredocRecoveryBody Span
	heredocRecoverySpanEnd := 0
	heredocRecoveryTokenCount := 0
	vim9Continuation := -1
	var vim9ContinuationState vim9ContinuationScan
	offset := 0
	if strings.HasPrefix(source, "\ufeff") {
		offset = len("\ufeff")
		file.Tokens = append(file.Tokens, Token{Kind: TokenBOM, Span: Span{End: offset}})
	}
	applyCommandState := func(before int) (int, int) {
		loadKeymapCommand := -1
		textBodyCommand := -1
		for index := before; index < len(file.Commands); index++ {
			command := &file.Commands[index]
			command.ScriptVersion = scriptVersion
			if command.logical != nil {
				command.logical.command.ScriptVersion = scriptVersion
			}
			switch command.Canonical {
			case "vim9script":
				if vim9Prologue {
					if code, message, span, valid := vim9ScriptArgumentDiagnostic(file.Source, command.Argument); !valid {
						file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: code, Message: message, Span: span})
					}
					active = Vim9
					vim9Prologue = false
				} else if initial == Legacy {
					file.Diagnostics = append(file.Diagnostics, Diagnostic{
						Code: "vim/E1039", Message: "vim9script must be the first command in the file", Span: command.Name,
					})
				}
			case "def":
				dialectStack = append(dialectStack, active)
				active = Vim9
			case "function":
				dialectStack = append(dialectStack, active)
				active = Legacy
			case "enddef", "endfunction":
				if len(dialectStack) > 0 {
					active = dialectStack[len(dialectStack)-1]
					dialectStack = dialectStack[:len(dialectStack)-1]
				}
			case "scriptversion":
				if command.Dialect == Legacy {
					if version, ok := parseScriptVersion(logicalArgumentText(file, command)); ok {
						scriptVersion = version
					}
				} else {
					file.Diagnostics = append(file.Diagnostics, Diagnostic{
						Code: "vim/E1040", Message: "cannot use :scriptversion after :vim9script", Span: command.Name,
					})
				}
			case "loadkeymap":
				// :loadkeymap consumes every following physical line as
				// keymap data. It is not an Ex heredoc.
				if command.Argument.Start == command.Argument.End && len(dialectStack) == 0 {
					loadKeymapCommand = index
				}
			}
			if command.Dialect == Legacy && legacyTextCommand(command.Canonical) && legacyTextCommandHeaderValid(file, command) {
				if len(dialectStack) > 0 && len(command.Modifiers) > 0 {
					parseLegacyInlineTextBody(file, command)
				} else {
					textBodyCommand = index
				}
			}
		}
		return loadKeymapCommand, textBodyCommand
	}
	for offset < len(source) {
		contentEnd, nextOffset := physicalLineEnd(source, offset)
		if heredocCommand >= 0 {
			command := &file.Commands[heredocCommand]
			if commandBlockEndLine(source[offset:contentEnd]) && insideVim9CommandBlock(file, heredocCommand) {
				// may_get_cmd_block() collects a :command { ... } definition
				// before it is executed and explicitly does not understand
				// heredocs. Its first line-leading } closes the definition even
				// when the stored command later reports a missing marker.
				command.Heredoc.Body.End = contentEnd
				command.Span.End = contentEnd
				command.Heredoc.Incomplete = true
				heredocCommand = -1
				heredocRecoveryCommand = ""
				heredocRecoveryOffset = -1
			} else if heredocEndMarkerMatches(source, command, offset, contentEnd) {
				command.Heredoc.EndMarker = Span{Start: offset, End: contentEnd}
				command.Span.End = contentEnd
				file.Tokens = append(file.Tokens, Token{Kind: TokenHeredoc, Span: command.Heredoc.EndMarker})
				if contentEnd < nextOffset {
					file.Tokens = append(file.Tokens, Token{Kind: TokenNewline, Span: Span{Start: contentEnd, End: nextOffset}})
				}
				heredocCommand = -1
				heredocRecoveryCommand = ""
				heredocRecoveryOffset = -1
				offset = nextOffset
				continue
			} else {
				if heredocRecoveryOffset < 0 && heredocRecoveryCommand != "" && isPayloadRecoveryLine(source, offset, contentEnd, heredocRecoveryCommand) {
					heredocRecoveryOffset = offset
					heredocRecoveryBody = command.Heredoc.Body
					heredocRecoverySpanEnd = command.Span.End
					heredocRecoveryTokenCount = len(file.Tokens)
				}
				if command.Heredoc.Body.Start == 0 && command.Heredoc.Body.End == 0 {
					command.Heredoc.Body.Start = offset
				}
				command.Heredoc.Body.End = contentEnd
				command.Span.End = contentEnd
				file.Tokens = append(file.Tokens, Token{Kind: TokenHeredoc, Span: Span{Start: offset, End: contentEnd}})
				if contentEnd < nextOffset {
					file.Tokens = append(file.Tokens, Token{Kind: TokenNewline, Span: Span{Start: contentEnd, End: nextOffset}})
				}
				if nextOffset >= len(source) && heredocRecoveryOffset >= 0 {
					command.Heredoc.Body = heredocRecoveryBody
					command.Heredoc.Incomplete = true
					command.Span.End = heredocRecoverySpanEnd
					file.Tokens = file.Tokens[:heredocRecoveryTokenCount]
					heredocCommand = -1
					heredocRecoveryCommand = ""
					offset = heredocRecoveryOffset
					heredocRecoveryOffset = -1
					continue
				}
				offset = nextOffset
				continue
			}
		}

		first := skipSpace(source, offset, contentEnd)
		if first < contentEnd && source[first] == '}' && hasOpenVim9CommandBlock(file) {
			// may_get_cmd_block() stops at any physical line whose first
			// non-white byte is }, regardless of trailing replacement text.
			if offset < first {
				file.Tokens = append(file.Tokens, Token{Kind: TokenWhitespace, Span: Span{Start: offset, End: first}})
			}
			name := Span{Start: first, End: first + 1}
			file.Tokens = append(file.Tokens, Token{Kind: TokenCommand, Span: name})
			file.Commands = append(file.Commands, Command{
				Kind: CommandBlockEnd, Dialect: Vim9, ScriptVersion: scriptVersion,
				Span: name, Name: name, TypedName: "}", Canonical: "}", Block: -1,
			})
			if first+1 < contentEnd {
				file.Tokens = append(file.Tokens, Token{Kind: TokenOpaque, Span: Span{Start: first + 1, End: contentEnd}})
			}
			if contentEnd < nextOffset {
				file.Tokens = append(file.Tokens, Token{Kind: TokenNewline, Span: Span{Start: contentEnd, End: nextOffset}})
			}
			vim9Continuation = -1
			vim9ContinuationState = vim9ContinuationScan{}
			offset = nextOffset
			continue
		}
		if vim9Continuation >= 0 && first < contentEnd && source[first:scanWord(source, first, contentEnd)] == "endenum" && hasOpenEnum(file) {
			// A trailing comma is valid after the final enum value.  It does not
			// make the following endenum part of the value expression.
			vim9Continuation = -1
		}
		lambdaRecoveryBoundary := len(vim9ContinuationState.lambdaDepth) > 0 && vim9ContinuationState.lambdaBodyStarted
		if vim9Continuation >= 0 && (len(vim9ContinuationState.lambdaDepth) == 0 || lambdaRecoveryBoundary) && first < contentEnd &&
			!(source[first] == '}' && vim9ContinuationState.depth > 0) && startsVim9RecoveryCommand(source, first, contentEnd) &&
			!(vim9ContinuationState.depth > 0 && looksLikeVim9NamedItem(source, first, contentEnd)) &&
			!continuesVim9FunctionSignature(file, vim9Continuation, vim9ContinuationState, source, first, contentEnd) &&
			!(source[first] == ':' && (len(vim9ContinuationState.ternaryDepth) > 0 || vim9ContinuationState.bracketDepth > 0)) {
			// Static analysis must recover after an incomplete expression.  A
			// clear statement boundary belongs to the following command even if
			// the previous line left a delimiter or operator open.
			vim9Continuation = -1
			vim9ContinuationState = vim9ContinuationScan{}
		}
		automaticLeadingContinuation := false
		if len(file.Commands) > 0 {
			last := len(file.Commands) - 1
			continuation := source[first:contentEnd]
			if startsVim9Continuation(continuation) && file.Commands[last].Dialect == Vim9 {
				if !allowsVim9AutomaticContinuation(file, last) {
					promoteVim9ContinuationExpression(file, last)
				}
				automaticLeadingContinuation = allowsVim9AutomaticContinuation(file, last)
			}
		}
		if first < contentEnd && len(file.Commands) > 0 && (vim9Continuation >= 0 || automaticLeadingContinuation) {
			commandIndex := vim9Continuation
			if commandIndex < 0 {
				commandIndex = len(file.Commands) - 1
			}
			command := &file.Commands[commandIndex]
			extendVim9LogicalCommand(command, source, first, contentEnd, nextOffset)
			logical := command.logical
			metadata := scanMetadataForParsedCommand(*command)
			logical.command.Expressions = nil
			logical.command.expressionsParsed = false
			argumentEnd, separator, comment, boundaryExpression := scanVim9CommandArgument(
				logical.view.Text, logical.command.Argument.Start, len(logical.view.Text), metadata, &logical.command,
			)
			logical.command.Argument.End = argumentEnd
			logical.command.Span.End = argumentEnd
			logical.command.boundaryExpression = boundaryExpression
			if logical.command.Span.End < logical.command.Name.End {
				logical.command.Span.End = logical.command.Name.End
			}
			command.Argument = logical.view.mapSpan(logical.command.Argument)
			command.Span = logical.view.mapSpan(logical.command.Span)
			command.boundaryExpression = boundaryExpression
			continuationEnd := command.Argument.End
			if continuationEnd < first {
				continuationEnd = first
			}
			file.Tokens = append(file.Tokens, Token{Kind: TokenContinuation, Span: Span{Start: first, End: continuationEnd}})

			newCommands := len(file.Commands)
			if separator.Start < separator.End {
				mapped := logical.view.mapSpan(separator)
				file.Tokens = append(file.Tokens, Token{Kind: TokenSeparator, Span: mapped})
				scanLogicalCommandRange(file, logical.view, separator.End, len(logical.view.Text), Vim9)
			} else if comment.Start < comment.End {
				file.Tokens = append(file.Tokens, Token{Kind: TokenComment, Span: logical.view.mapSpan(comment)})
			}
			loadKeymapCommand, textBodyCommand := applyCommandState(newCommands)
			if loadKeymapCommand >= 0 {
				offset = parseLoadKeymapBody(file, &file.Commands[loadKeymapCommand], nextOffset, hasOpenVim9CommandBlock(file))
				continue
			}
			if textBodyCommand >= 0 {
				if contentEnd < nextOffset {
					file.Tokens = append(file.Tokens, Token{Kind: TokenNewline, Span: Span{Start: contentEnd, End: nextOffset}})
				}
				offset = parseLegacyTextBody(file, &file.Commands[textBodyCommand], nextOffset, textBodyRecoveryCommand(active, dialectStack), hasOpenVim9CommandBlock(file))
				continue
			}

			vim9Continuation = -1
			last := len(file.Commands) - 1
			if last >= commandIndex && file.Commands[last].Heredoc != nil {
				if len(dialectStack) > 0 && len(file.Commands[last].Modifiers) > 0 {
					file.Commands[last].Heredoc.Deferred = true
				} else {
					heredocCommand = last
					if file.Commands[last].Heredoc.Body == (Span{}) {
						file.Commands[last].Heredoc.Body = Span{Start: nextOffset, End: nextOffset}
					}
					heredocRecoveryCommand = enclosingFunctionEnd(active, dialectStack)
					heredocRecoveryOffset = -1
				}
			} else if last >= commandIndex && usesVim9Continuation(file.Commands[last]) {
				vim9ContinuationState = scanVim9Continuation(logicalArgumentText(file, &file.Commands[last]), vim9ContinuationScan{})
				if needsVim9CommandContinuation(file, last, vim9ContinuationState) {
					vim9Continuation = last
				}
			}
			if contentEnd < nextOffset {
				file.Tokens = append(file.Tokens, Token{Kind: TokenNewline, Span: Span{Start: contentEnd, End: nextOffset}})
			}
			offset = nextOffset
			continue
		}

		var view logicalView
		if active == Legacy {
			view = readLegacyLogicalView(source, offset)
		} else {
			view = readVim9LogicalView(source, offset)
		}
		before := scanLogicalCommands(file, &view, active)
		loadKeymapCommand, textBodyCommand := applyCommandState(before)
		if loadKeymapCommand >= 0 {
			offset = parseLoadKeymapBody(file, &file.Commands[loadKeymapCommand], view.Next, hasOpenVim9CommandBlock(file))
			continue
		}
		if textBodyCommand >= 0 {
			offset = parseLegacyTextBody(file, &file.Commands[textBodyCommand], view.Next, textBodyRecoveryCommand(active, dialectStack), hasOpenVim9CommandBlock(file))
			continue
		}
		if len(file.Commands) > before && file.Commands[len(file.Commands)-1].Heredoc != nil {
			last := len(file.Commands) - 1
			if len(dialectStack) > 0 && len(file.Commands[last].Modifiers) > 0 {
				file.Commands[last].Heredoc.Deferred = true
			} else {
				heredocCommand = last
				if file.Commands[heredocCommand].Heredoc.Body == (Span{}) {
					file.Commands[heredocCommand].Heredoc.Body = Span{Start: view.Next, End: view.Next}
				}
				heredocRecoveryCommand = enclosingFunctionEnd(active, dialectStack)
				heredocRecoveryOffset = -1
			}
		} else if len(file.Commands) > before {
			last := len(file.Commands) - 1
			if usesVim9Continuation(file.Commands[last]) {
				vim9ContinuationState = scanVim9Continuation(logicalArgumentText(file, &file.Commands[last]), vim9ContinuationScan{})
				vim9Continuation = -1
				if needsVim9CommandContinuation(file, last, vim9ContinuationState) {
					vim9Continuation = last
				}
			} else {
				vim9Continuation = -1
			}
		}
		offset = view.Next
	}
	if heredocCommand >= 0 {
		file.Commands[heredocCommand].Heredoc.Incomplete = true
	}
	coalesceLegacyEmbeddedBlocks(file)
	coalesceCollectedCommandBlocks(file, len(file.Source))
	scannerDiagnostics := len(file.Diagnostics)
	buildBlocks(file)
	if truncateAfterDirectFinish(file, scannerDiagnostics) {
		buildBlocks(file)
	}
	normalizeVim9SpacedCallDiagnostics(file)
	for index := range file.Commands {
		if !file.Commands[index].detailsOpaque && (file.Commands[index].Heredoc == nil || file.Commands[index].Canonical == "execute") {
			parseLogicalCommandDetails(file, &file.Commands[index])
		}
	}
	buildAggregateMembers(file)
	suppressInvalidDefMissingEnds(file)
	suppressInvalidInterfaceInitializers(file)
	normalizeLambdaBodySources(file)
	sort.SliceStable(file.Tokens, func(left, right int) bool {
		return file.Tokens[left].Span.Start < file.Tokens[right].Span.Start
	})
	return file
}

func normalizeVim9SpacedCallDiagnostics(file *File) {
	for diagnosticIndex := range file.Diagnostics {
		diagnostic := &file.Diagnostics[diagnosticIndex]
		if diagnostic.Code != "vim/E476" || diagnostic.Message != "invalid command: whitespace before function arguments" {
			continue
		}
		commandIndex := sort.Search(len(file.Commands), func(index int) bool {
			return file.Commands[index].Name.Start >= diagnostic.Span.Start
		})
		if commandIndex == len(file.Commands) || file.Commands[commandIndex].Name != diagnostic.Span {
			continue
		}
		command := &file.Commands[commandIndex]
		inDef := false
		for block := command.Block; block >= 0 && block < len(file.Blocks); block = file.Blocks[block].Parent {
			if file.Blocks[block].Kind == BlockDef {
				inDef = true
				break
			}
		}
		if inDef {
			diagnostic.Message = "Invalid command"
		} else {
			diagnostic.Code = "vim/E492"
			diagnostic.Message = "Not an editor command"
		}
	}
}

// coalesceLegacyEmbeddedBlocks models the source-line callback used by
// commands such as :windo.  The command itself is parsed on its physical
// line, but do_cmdline() reads following lines when its argument starts a
// block.  Keep those lines out of the outer command stream and let the
// command's embedded parser own the complete block.
func coalesceLegacyEmbeddedBlocks(file *File) {
	if file == nil {
		return
	}
	for index := 0; index < len(file.Commands); index++ {
		command := &file.Commands[index]
		if !listDoCommand(command.Canonical) || command.baseDialect != Legacy {
			continue
		}
		bodyStart := skipSpace(file.Source, command.Argument.Start, command.Argument.End)
		if bodyStart >= command.Argument.End {
			continue
		}
		bodyEnd, ok := legacyEmbeddedBlockEnd(file, index, bodyStart)
		if !ok || bodyEnd <= command.Span.End {
			continue
		}
		command.Argument.End = bodyEnd
		command.Span.End = bodyEnd
		// The original logical view only covered the first physical line.  The
		// expanded argument must be parsed against the original source spans.
		command.logical = nil
		kept := file.Commands[:index+1]
		for _, candidate := range file.Commands[index+1:] {
			if candidate.Span.Start >= bodyStart && candidate.Span.End <= bodyEnd {
				continue
			}
			kept = append(kept, candidate)
		}
		file.Commands = kept
	}
}

// coalesceCollectedCommandBlocks models find_cmd_block_start() and the source
// callback used by legacy :autocmd and :command definitions. A block consumes
// physical lines up to the first line whose first non-white byte is }. Braces
// do not nest this collector. A direct user-command block without a close owns
// the remaining source, matching Vim's getline() collector at EOF.
func coalesceCollectedCommandBlocks(file *File, limit int) {
	if file == nil {
		return
	}
	if limit > len(file.Source) {
		limit = len(file.Source)
	}
	var closes []collectedBlockClose
	closesReady := false
	for index := 0; index < len(file.Commands); index++ {
		command := &file.Commands[index]
		if command.Canonical != "autocmd" && (command.Canonical != "command" || command.Dialect == Vim9) {
			continue
		}
		open, direct, ok := collectedCommandBlockStart(file.Source, command, limit)
		if !ok {
			continue
		}
		if !closesReady {
			closes = collectBlockCloseLines(file.Source, limit)
			closesReady = true
		}
		_, closeEnd, found := findCollectedBlockClose(closes, open.End)
		if !found {
			if command.Canonical == "command" && direct {
				command.Argument.End = limit
				command.Span.End = limit
				command.logical = nil
				command.collectedBlockVim9 = true
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E1026", Message: "missing }", Span: open,
				})
				kept := file.Commands[:index+1]
				for _, candidate := range file.Commands[index+1:] {
					if candidate.Span.Start >= open.End && candidate.Span.End <= limit {
						continue
					}
					kept = append(kept, candidate)
				}
				file.Commands = kept
			}
			continue
		}
		command.Argument.End = closeEnd
		command.Span.End = closeEnd
		command.logical = nil
		command.collectedBlockVim9 = direct
		bodyStart := open.End
		kept := file.Commands[:index+1]
		for _, candidate := range file.Commands[index+1:] {
			if candidate.Span.Start >= bodyStart && candidate.Span.End <= closeEnd {
				continue
			}
			kept = append(kept, candidate)
		}
		file.Commands = kept
	}
}

type collectedBlockClose struct {
	start int
	end   int
}

func collectBlockCloseLines(source string, end int) []collectedBlockClose {
	var closes []collectedBlockClose
	for lineStart := 0; lineStart < end; {
		contentEnd, next := physicalLineEnd(source, lineStart)
		if contentEnd > end {
			contentEnd = end
			next = end
		}
		first := skipSpace(source, lineStart, contentEnd)
		if first < contentEnd && source[first] == '}' {
			closes = append(closes, collectedBlockClose{start: lineStart, end: contentEnd})
		}
		if next <= lineStart || next >= end {
			break
		}
		lineStart = next
	}
	return closes
}

func findCollectedBlockClose(closes []collectedBlockClose, after int) (int, int, bool) {
	index := sort.Search(len(closes), func(index int) bool { return closes[index].start >= after })
	if index >= len(closes) {
		return 0, 0, false
	}
	return closes[index].start, closes[index].end, true
}

func collectedCommandBlockStart(source string, command *Command, limit int) (Span, bool, bool) {
	if command == nil {
		return Span{}, false, false
	}
	if command.Canonical == "autocmd" {
		return autocmdBlockStart(source, command.Argument, command.Dialect, limit)
	}
	if command.Canonical != "command" {
		return Span{}, false, false
	}
	body, ok := userCommandBodySpan(source, command.Argument)
	if !ok {
		return Span{}, false, false
	}
	if open, ok := commandBlockOpen(source, body, command.Dialect); ok {
		return open, true, true
	}
	open, ok := nestedCommandBlockOpen(source, body, command.Dialect, limit)
	return open, false, ok
}

// autocmdBlockStart mirrors find_cmd_block_start(). The first autocmd may
// directly own the block, or its command body may begin with another
// :autocmd or :command whose replacement eventually owns it. Only a direct
// owner receives Vim9 block syntax; enclosing commands retain their dialect.
func autocmdBlockStart(source string, argument Span, dialect Dialect, limit int) (Span, bool, bool) {
	if open, ok := autocmdBlockOpen(source, argument, dialect); ok {
		return open, true, true
	}
	body, ok := autocmdBodyCommandSpan(source, argument, dialect)
	if !ok {
		return Span{}, false, false
	}
	open, ok := nestedCommandBlockOpen(source, body, dialect, limit)
	return open, false, ok
}

func autocmdBlockOpen(source string, argument Span, dialect Dialect) (Span, bool) {
	start := skipSpace(source, argument.Start, argument.End)
	if start >= argument.End {
		return Span{}, false
	}
	// Reuse the header scanner so a group, event list, pattern and ordered
	// ++ modifiers are consumed exactly as they are for ordinary autocmds.
	_, bodyStart, hasBody, block := scanAutocmdHeader(source, argument, dialect)
	if block {
		for position := bodyStart - 1; position >= argument.Start; position-- {
			if source[position] == '{' {
				bodyStart = position
				break
			}
		}
	}
	if !hasBody || !block || bodyStart < 0 || bodyStart >= argument.End || source[bodyStart] != '{' {
		return Span{}, false
	}
	return Span{Start: bodyStart, End: bodyStart + 1}, true
}

func autocmdBodyCommandSpan(source string, argument Span, dialect Dialect) (Span, bool) {
	_, body, hasBody, block := parseAutocmdHeader(source, argument, dialect, false)
	if !hasBody || block || body.Start >= argument.End {
		return Span{}, false
	}
	return body, true
}

func nestedCommandBlockOpen(source string, body Span, dialect Dialect, limit int) (Span, bool) {
	for depth := 0; depth < maxEmbeddedCommandDepth; depth++ {
		start := skipSpace(source, body.Start, body.End)
		if start >= body.End || start >= limit {
			return Span{}, false
		}
		nested := &File{Dialect: dialect, Source: source}
		scanCommands(nested, start, min(body.End, limit), dialect)
		if len(nested.Commands) == 0 || nested.Commands[0].Span.Start != start {
			return Span{}, false
		}
		command := &nested.Commands[0]
		switch command.Canonical {
		case "autocmd":
			if open, ok := autocmdBlockOpen(source, command.Argument, command.Dialect); ok {
				return open, true
			}
			var ok bool
			body, ok = autocmdBodyCommandSpan(source, command.Argument, command.Dialect)
			if !ok {
				return Span{}, false
			}
			dialect = command.Dialect
		case "command":
			var ok bool
			body, ok = userCommandBodySpan(source, command.Argument)
			if !ok {
				return Span{}, false
			}
			if open, ok := commandBlockOpen(source, body, command.Dialect); ok {
				return open, true
			}
			dialect = command.Dialect
		default:
			return Span{}, false
		}
	}
	return Span{}, false
}

func commandBlockOpen(source string, body Span, dialect Dialect) (Span, bool) {
	start := skipSpace(source, body.Start, body.End)
	if start >= body.End || source[start] != '{' || !autocmdBlockLineOnly(source, start, body.End, dialect) {
		return Span{}, false
	}
	return Span{Start: start, End: start + 1}, true
}

func autocmdBlockClose(source string, start, end int) (int, int, bool) {
	for lineStart := start; lineStart < end; {
		contentEnd, next := physicalLineEnd(source, lineStart)
		if contentEnd > end {
			contentEnd = end
			next = end
		}
		first := skipSpace(source, lineStart, contentEnd)
		if first < contentEnd && source[first] == '}' {
			return lineStart, contentEnd, true
		}
		if next <= lineStart || next >= end {
			break
		}
		lineStart = next
	}
	return 0, 0, false
}

func legacyEmbeddedBlockEnd(file *File, outerIndex, bodyStart int) (int, bool) {
	outer := file.Commands[outerIndex]
	first := &File{Dialect: Legacy, Source: file.Source}
	scanCommands(first, bodyStart, outer.Argument.End, Legacy)
	if len(first.Commands) == 0 {
		return 0, false
	}
	kind, ok := openingBlock(first, &first.Commands[0])
	if !ok || kind == BlockFunction || kind == BlockDef {
		return 0, false
	}
	stack := []BlockKind{kind}
	consume := func(commands []Command) (int, bool) {
		for _, command := range commands {
			if len(stack) == 0 {
				return command.Span.End, true
			}
			if len(stack) > 0 {
				if opened, opening := openingBlock(first, &command); opening && opened != BlockFunction && opened != BlockDef {
					stack = append(stack, opened)
					continue
				}
			}
			if closed, closing := closingBlock(file, &command); closing {
				match := -1
				for index := len(stack) - 1; index >= 0; index-- {
					if stack[index] == closed {
						match = index
						break
					}
				}
				if match >= 0 {
					stack = stack[:match]
					if len(stack) == 0 {
						return command.Span.End, true
					}
				}
			}
		}
		return 0, false
	}
	if end, found := consume(first.Commands[1:]); found {
		return end, true
	}
	for index := outerIndex + 1; index < len(file.Commands); index++ {
		command := file.Commands[index]
		if command.Span.Start < outer.Span.End {
			continue
		}
		if end, found := consume([]Command{command}); found {
			return end, true
		}
	}
	return 0, false
}

// truncateAfterDirectFinish keeps source following an unconditional script
// :finish as one opaque span.  A finish inside an if, loop, try, function, or
// other block is conditional or belongs to a definition and must not hide the
// rest of the file from static analysis.
func truncateAfterDirectFinish(file *File, scannerDiagnostics int) bool {
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Canonical != "finish" || command.Block >= 0 || command.Bang.Start < command.Bang.End ||
			command.Range.Start < command.Range.End || command.Argument.Start < command.Argument.End ||
			command.Dialect == Vim9 && command.TypedName != "finish" {
			continue
		}
		tailStart := command.Span.End
		if tailStart >= len(file.Source) {
			return false
		}

		file.Commands = file.Commands[:index+1]
		file.Blocks = nil
		for commandIndex := range file.Commands {
			file.Commands[commandIndex].Block = -1
		}

		keptDiagnostics := file.Diagnostics[:0]
		if scannerDiagnostics > len(file.Diagnostics) {
			scannerDiagnostics = len(file.Diagnostics)
		}
		for _, diagnostic := range file.Diagnostics[:scannerDiagnostics] {
			if diagnostic.Span.Start < tailStart {
				keptDiagnostics = append(keptDiagnostics, diagnostic)
			}
		}
		file.Diagnostics = keptDiagnostics

		keptTokens := file.Tokens[:0]
		for _, token := range file.Tokens {
			if token.Span.End <= tailStart {
				keptTokens = append(keptTokens, token)
			}
		}
		file.OpaqueTail = Span{Start: tailStart, End: len(file.Source)}
		file.Tokens = append(keptTokens, Token{Kind: TokenOpaque, Span: file.OpaqueTail})
		return true
	}
	return false
}

func hasOpenEnum(file *File) bool {
	for index := len(file.Commands) - 1; index >= 0; index-- {
		switch file.Commands[index].Canonical {
		case "endenum":
			return false
		case "enum":
			return true
		}
	}
	return false
}

func commandBlockEndLine(source string) bool {
	return strings.HasPrefix(strings.TrimLeft(source, " \t\r"), "}")
}

func hasOpenVim9CommandBlock(file *File) bool {
	return len(file.Commands) > 0 && insideVim9CommandBlock(file, len(file.Commands)-1)
}

func insideVim9CommandBlock(file *File, commandIndex int) bool {
	depth := 0
	for index := 0; index <= commandIndex; index++ {
		command := file.Commands[index]
		if command.Canonical == "command" && command.Dialect == Vim9 && strings.HasSuffix(strings.TrimSpace(file.Text(command.Argument)), "{") {
			depth++
		} else if command.Canonical == "}" && depth > 0 {
			depth--
		}
	}
	return depth > 0
}

func parseScriptVersion(source string) (uint8, bool) {
	source = strings.TrimSpace(source)
	if len(source) != 1 || source[0] < '1' || source[0] > '4' {
		return 0, false
	}
	return source[0] - '0', true
}

// vim9ScriptArgumentDiagnostic mirrors ex_vim9script()'s small argument
// parser.  The command accepts no argument or one whitespace-delimited ASCII
// word, "noclear".  Keep the offending word span so diagnostics remain useful
// while the caller can recover at the next physical line.
func vim9ScriptArgumentDiagnostic(source string, argument Span) (string, string, Span, bool) {
	start, end := argument.Start, argument.End
	foundNoClear := false
	for start < end {
		wordStart := start
		for start < end && !isSpace(source[start]) {
			start++
		}
		word := source[wordStart:start]
		if word == "noclear" {
			if foundNoClear {
				return "vim/E983", "duplicate argument: noclear", Span{Start: wordStart, End: start}, false
			}
			foundNoClear = true
		} else {
			return "vim/E475", "invalid argument: " + source[argument.Start:argument.End], Span{Start: wordStart, End: start}, false
		}
		start = skipSpace(source, start, end)
	}
	return "", "", Span{}, true
}

func scanCommands(file *File, start, end int, baseDialect Dialect) {
	for start < end {
		diagnosticsBeforeCommand := len(file.Diagnostics)
		start = skipSpaceToken(file, start, end)
		if start >= end {
			return
		}
		if isCommentStart(file.Source, start, start, end, baseDialect, vimdata.Command{}) {
			file.Tokens = append(file.Tokens, Token{Kind: TokenComment, Span: Span{Start: start, End: end}})
			return
		}
		if baseDialect == Vim9 && strings.HasPrefix(file.Source[start:end], "#{") && !strings.HasPrefix(file.Source[start:end], "#{{") {
			// In Vim9 `#{` used to look like a dictionary opener, but it is an
			// invalid attempt to begin a comment.  Keep the physical line opaque
			// so recovery resumes with the next command.
			file.Tokens = append(file.Tokens, Token{Kind: TokenOpaque, Span: Span{Start: start, End: end}})
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1170", Message: "Cannot use #{ to start a comment", Span: Span{Start: start, End: start + 2},
			})
			return
		}

		commandStart := start
		if file.Source[start] == ':' {
			file.Tokens = append(file.Tokens, Token{Kind: TokenColon, Span: Span{Start: start, End: start + 1}})
			start = skipSpaceToken(file, start+1, end)
		}
		rangeStart := start
		if baseDialect == Legacy || commandStart < end && file.Source[commandStart] == ':' {
			start = scanRange(file.Source, start, end)
		}
		commandRange := Span{}
		if start > rangeStart {
			commandRange = Span{Start: rangeStart, End: start}
			file.Tokens = append(file.Tokens, Token{Kind: TokenRange, Span: commandRange})
			start = skipSpaceToken(file, start, end)
		}

		dialect := baseDialect
		var parsedModifiers []Modifier
		for {
			wordEnd := scanWord(file.Source, start, end)
			if wordEnd == start {
				break
			}
			name, ok := lookupModifier(file.Source[start:wordEnd])
			if !ok || name != "filter" && wordEnd < end && !isSpace(file.Source[wordEnd]) && file.Source[wordEnd] != '!' {
				break
			}
			if dialect == Vim9 && looksLikeVim9AssignmentAfterName(file.Source, wordEnd, end) {
				break
			}
			spanEnd := wordEnd
			modifierBang := Span{}
			if (name == "silent" || name == "filter") && spanEnd < end && file.Source[spanEnd] == '!' {
				modifierBang = Span{Start: spanEnd, End: spanEnd + 1}
				spanEnd++
			}
			// Vim9's command modifier parser explicitly refuses filter(arg) and
			// any filter pattern that is not separated from the modifier.  The
			// legacy parser accepts a delimiter immediately after the name.
			if name == "filter" && dialect == Vim9 && spanEnd < end && !isSpace(file.Source[spanEnd]) {
				break
			}
			filterPatternStart := skipSpace(file.Source, spanEnd, end)
			filterBangStart := -1
			if name == "filter" && modifierBang.Start == modifierBang.End && filterPatternStart < end && file.Source[filterPatternStart] == '!' {
				filterBangStart = filterPatternStart
				// Vim9 requires whitespace after the force-bang as well.  This
				// keeps `filter !foo` from becoming a modifier while accepting
				// the documented `filter ! /foo/` form.
				if dialect == Vim9 && (filterBangStart+1 >= end || !isSpace(file.Source[filterBangStart+1])) {
					break
				}
				filterPatternStart = skipSpace(file.Source, filterBangStart+1, end)
			}
			if name == "filter" && filterModifierEndsCommand(file.Source, filterPatternStart, end, dialect) {
				// parse_command_modifiers() does not claim a filter whose first
				// pattern byte is an Ex command terminator.  Keep start at the
				// modifier so the normal command scanner can recover it.
				break
			}
			modifier := Modifier{Name: name, Span: Span{Start: start, End: spanEnd}, Bang: modifierBang}
			if filterBangStart >= 0 {
				modifier.Bang = Span{Start: filterBangStart, End: filterBangStart + 1}
			}
			if name == "filter" {
				modifier.Filter = &FilterModifier{}
			}
			parsedModifiers = append(parsedModifiers, modifier)
			file.Tokens = append(file.Tokens, Token{Kind: TokenModifier, Span: modifier.Span})
			if dialect == Vim9 && (name == "export" || name == "public" || name == "abstract" || name == "static") && file.Source[start:wordEnd] != name {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E1065", Message: "command cannot be shortened in Vim9 script", Span: Span{Start: start, End: wordEnd}})
			}
			if name == "legacy" {
				dialect = Legacy
			} else if name == "vim9cmd" {
				dialect = Vim9
			}
			start = skipSpaceToken(file, spanEnd, end)
			if name == "filter" {
				filter := &parsedModifiers[len(parsedModifiers)-1]
				if filterBangStart >= 0 {
					file.Tokens = append(file.Tokens, Token{Kind: TokenBang, Span: filter.Bang})
					start = skipSpaceToken(file, filter.Bang.End, end)
				}
				filterEnd, malformed := scanFilterModifier(file, filter.Filter, start, end)
				if malformed {
					// A missing closing delimiter makes the complete remainder of
					// this physical/logical command the filter pattern.  Keep it
					// opaque and recover at the next physical line instead of
					// treating a bar in the unfinished pattern as a command.
					start = end
					break
				}
				start = skipSpaceToken(file, filterEnd, end)
			}
		}
		invalidModifierRange := false
		if len(parsedModifiers) > 0 {
			// Legacy permits an optional colon before a range after modifiers.
			// Vim9 requires the colon unless the bytes start an expression.
			hasRangeColon := false
			if start < end && file.Source[start] == ':' {
				hasRangeColon = true
				file.Tokens = append(file.Tokens, Token{Kind: TokenColon, Span: Span{Start: start, End: start + 1}})
				start = skipSpaceToken(file, start+1, end)
			}
			rangeStart = start
			start = scanRange(file.Source, start, end)
			if start > rangeStart {
				commandRange = Span{Start: rangeStart, End: start}
				file.Tokens = append(file.Tokens, Token{Kind: TokenRange, Span: commandRange})
				if dialect == Vim9 && !hasRangeColon && vim9ModifierRangeRequiresColon(file.Source, rangeStart, start, end) {
					file.Diagnostics = append(file.Diagnostics, Diagnostic{
						Code: "vim/E1050", Message: "colon required before a range", Span: Span{Start: rangeStart, End: end},
					})
					invalidModifierRange = true
				}
				start = skipSpaceToken(file, start, end)
			}
		}
		missingVim9ModifierCommand := !invalidModifierRange && baseDialect == Vim9 && len(parsedModifiers) > 0 && (start >= end || start < end && isCommentStart(file.Source, start, start, end, Vim9, vimdata.Command{}))
		emptyPrefix := commandRange.Start < commandRange.End || len(parsedModifiers) > 0 || commandStart < end && file.Source[commandStart] == ':'
		if start < end && file.Source[start] == '|' || start >= end && emptyPrefix {
			if missingVim9ModifierCommand {
				last := parsedModifiers[len(parsedModifiers)-1]
				file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E1082", Message: "command modifier without command", Span: last.Span})
			}
			file.Commands = append(file.Commands, Command{
				Kind: CommandEmpty, Dialect: dialect, baseDialect: baseDialect, Span: Span{Start: commandStart, End: start}, Range: commandRange, Modifiers: parsedModifiers, Block: -1,
			})
			if start < end {
				separator := Span{Start: start, End: start + 1}
				file.Tokens = append(file.Tokens, Token{Kind: TokenSeparator, Span: separator})
				start = separator.End
				continue
			}
			return
		}
		if start < end && (isCommentStart(file.Source, start, start, end, dialect, vimdata.Command{}) || missingVim9ModifierCommand) {
			file.Tokens = append(file.Tokens, Token{Kind: TokenComment, Span: Span{Start: start, End: end}})
			if commandRange.Start < commandRange.End || len(parsedModifiers) > 0 || commandStart < start {
				if missingVim9ModifierCommand {
					last := parsedModifiers[len(parsedModifiers)-1]
					file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E1082", Message: "command modifier without command", Span: last.Span})
				}
				file.Commands = append(file.Commands, Command{
					Kind: CommandEmpty, Dialect: dialect, Span: Span{Start: commandStart, End: start},
					Range: commandRange, Modifiers: parsedModifiers, Block: -1,
				})
			}
			return
		}
		if dialect == Vim9 && (file.Source[start] == '{' || file.Source[start] == '}') {
			after := skipSpace(file.Source, start+1, end)
			if after == end || isCommentStart(file.Source, after, after, end, dialect, vimdata.Command{}) {
				name := Span{Start: start, End: start + 1}
				kind := CommandBlockEnd
				if file.Source[start] == '{' {
					kind = CommandBlockStart
				}
				file.Tokens = append(file.Tokens, Token{Kind: TokenCommand, Span: name})
				file.Commands = append(file.Commands, Command{
					Kind: kind, Dialect: dialect, Span: name, Name: name, TypedName: file.Source[start : start+1], Canonical: file.Source[start : start+1], Block: -1,
				})
				if after < end {
					file.Tokens = append(file.Tokens, Token{Kind: TokenComment, Span: Span{Start: after, End: end}})
				}
				return
			}
			if file.Source[start] == '{' {
				file.Tokens = append(file.Tokens, Token{Kind: TokenArgument, Span: Span{Start: start, End: end}})
				file.Commands = append(file.Commands, Command{
					Kind: CommandExpression, Dialect: dialect, Span: Span{Start: commandStart, End: end}, Range: commandRange,
					Modifiers: parsedModifiers, Argument: Span{Start: start, End: end}, Block: -1,
				})
				return
			}
		}

		nameStart := start
		nameEnd := scanCommandName(file.Source, start, end)
		if nameEnd == nameStart {
			file.Tokens = append(file.Tokens, Token{Kind: TokenArgument, Span: Span{Start: start, End: end}})
			file.Commands = append(file.Commands, Command{Kind: CommandExpression, Dialect: dialect, Span: Span{Start: commandStart, End: end}, Range: commandRange, Modifiers: parsedModifiers, Argument: Span{Start: start, End: end}, Block: -1})
			return
		}
		typedName := file.Source[nameStart:nameEnd]
		metadata, builtIn := vimdata.Lookup(typedName)
		// Legacy Vim recognizes a deliberately narrow set of alphabetic bytes
		// after the one-byte :s command before it performs normal command lookup.
		// This is what makes :sge2 a repeat command without turning an unknown
		// name such as setbufvar(...) into :substitute followed by garbage.
		if dialect == Legacy && legacyOneLetterSubstitute(file.Source, nameStart, end) {
			if substituteMetadata, substituteBuiltIn := vimdata.Lookup("s"); substituteBuiltIn {
				nameEnd = nameStart + 1
				typedName = file.Source[nameStart:nameEnd]
				metadata, builtIn = substituteMetadata, true
			}
		}
		kind := CommandUnknown
		canonical := typedName
		if builtIn {
			kind = CommandBuiltin
			canonical = metadata.Name
		} else if startsUpper(typedName) {
			kind = CommandUser
		}
		expressionNameEnd := nameEnd
		if dialect == Vim9 {
			// Vim recognizes a command-start assignment or call using the full
			// variable name before it falls back to the ASCII Ex command name.
			// Keep these two boundaries independent so that both foo_bar() and
			// :delete_ are classified like Vim.
			if wordEnd := scanWord(file.Source, nameStart, end); wordEnd > expressionNameEnd {
				expressionNameEnd = wordEnd
			}
		}
		nameExpression := false
		malformedDeclaration := false
		if dialect == Vim9 && builtIn && canonical == "var" {
			position := skipSpace(file.Source, nameEnd, end)
			malformedDeclaration = position < end && (file.Source[position] == ':' || file.Source[position] == '=')
		}
		if !builtIn || expressionNameEnd > nameEnd {
			nameExpression = looksLikeVim9Expression(file.Source, nameStart, expressionNameEnd, end)
		} else if !malformedDeclaration {
			nameExpression = (canonical != "substitute" && canonical != "smagic" && canonical != "snomagic" && looksLikeImmediateVim9Expression(file.Source, nameEnd, end)) ||
				(canonical == "substitute" || canonical == "smagic" || canonical == "snomagic") && looksLikeSubstituteVim9Expression(file.Source, typedName, nameEnd, end) ||
				(canonical != "substitute" && canonical != "smagic" && canonical != "snomagic" && strings.HasPrefix(file.Source[skipSpace(file.Source, nameEnd, end):end], "->")) ||
				(canonical != "substitute" && canonical != "smagic" && canonical != "snomagic" && canonical != "iput" && canonical != "put" && looksLikeVim9AssignmentAfterName(file.Source, nameEnd, end))
		}
		expressionAtCommandStart := dialect == Vim9 && (looksLikeVim9SigilExpression(file.Source, nameStart, end) || nameExpression)
		if expressionAtCommandStart {
			kind = CommandExpression
			canonical = ""
			builtIn = false
			metadata = vimdata.Command{Flags: vimdata.ExpressionArgument}
		}
		if builtIn && dialect == Vim9 && metadata.Flags&vimdata.ExactInVim9 != 0 && typedName != canonical {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E1065", Message: "command cannot be shortened in Vim9 script", Span: Span{Start: nameStart, End: nameEnd}})
		}
		nameSpan := Span{Start: nameStart, End: nameEnd}
		file.Tokens = append(file.Tokens, Token{Kind: TokenCommand, Span: nameSpan})
		start = nameEnd
		bang := Span{}
		if start < end && file.Source[start] == '!' && !(builtIn && substitutePatternCommand(canonical)) {
			bang = Span{Start: start, End: start + 1}
			file.Tokens = append(file.Tokens, Token{Kind: TokenBang, Span: bang})
			start++
			if builtIn && metadata.Flags&vimdata.AllowBang == 0 {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vimls/unexpected-bang", Message: "command does not accept !", Span: bang})
			}
		}
		argumentStart := skipSpaceToken(file, start, end)
		if expressionAtCommandStart {
			argumentStart = nameStart
		}
		// These commands parse their variable list themselves and call
		// set_nextcmd() when ex_unletlock() reaches a top-level bar.  Vim's
		// command table does not mark them EX_TRLBAR, so make that command-local
		// behavior explicit for scanning without changing the generated table.
		scanMetadata := metadata
		if selfSplittingVariableCommand(canonical) {
			scanMetadata.Flags |= vimdata.AllowBar
		}
		// These commands evaluate their argument and consume a trailing bar
		// themselves in Vim.  Most carry EX_EXPR_ARG in the command table, but
		// :return, :throw, :final and a few related commands intentionally do
		// not.  The expression scanner still knows their grammar, so give it
		// the same top-level bar boundary without applying this to special
		// command syntaxes such as :substitute or :syntax.
		if expressionCommand(canonical) {
			scanMetadata.Flags |= vimdata.ExpressionArgument
		}
		parsedCommand := Command{
			Kind: kind, Dialect: dialect, baseDialect: baseDialect, detailsOpaque: invalidModifierRange, Span: Span{Start: commandStart, End: nameEnd}, Range: commandRange,
			Name: nameSpan, TypedName: typedName, Canonical: canonical, Modifiers: parsedModifiers, Bang: bang,
			Argument: Span{Start: argumentStart, End: end}, Block: -1,
		}
		var argumentEnd int
		var separator, comment Span
		var boundaryExpression *expressionBoundary
		if canonical == "autocmd" {
			argumentEnd, separator = scanAutocmdCommandArgument(file.Source, argumentStart, end)
		} else if dialect == Legacy {
			argumentEnd, separator, comment, boundaryExpression = scanLegacyCommandArgument(file.Source, argumentStart, end, scanMetadata, &parsedCommand)
		} else {
			argumentEnd, separator, comment, boundaryExpression = scanVim9CommandArgument(file.Source, argumentStart, end, scanMetadata, &parsedCommand)
		}
		if argumentStart < argumentEnd {
			file.Tokens = append(file.Tokens, Token{Kind: TokenArgument, Span: Span{Start: argumentStart, End: argumentEnd}})
		}
		commandEnd := argumentEnd
		if commandEnd < nameEnd {
			commandEnd = nameEnd
		}
		parsedCommand.Span.End = commandEnd
		parsedCommand.Argument.End = argumentEnd
		parsedCommand.boundaryExpression = boundaryExpression
		if builtIn && canonical == "loadkeymap" && argumentStart < argumentEnd {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E488", Message: "trailing characters", Span: Span{Start: argumentStart, End: argumentEnd},
			})
		}
		if dialect == Vim9 {
			if builtIn {
				switch canonical {
				case "k":
					if commandRange.Start < commandRange.End {
						file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E481", Message: "no range allowed", Span: commandRange})
					} else {
						file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E1100", Message: "command not supported in Vim9 script (missing :var?): " + typedName, Span: nameSpan})
					}
				case "append", "change", "insert":
					if legacyTextCommandHeaderSourceValid(file.Source, canonical, argumentStart, argumentEnd) {
						file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E1100", Message: "command not supported in Vim9 script (missing :var?): " + typedName, Span: nameSpan})
					} else {
						file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E488", Message: "trailing characters", Span: Span{Start: argumentStart, End: argumentEnd}})
					}
				case "open", "t", "xit":
					file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E1100", Message: "command not supported in Vim9 script (missing :var?): " + typedName, Span: nameSpan})
				}
			}
			if !builtIn && startsUpper(typedName) && spacedVim9Call(file.Source, nameEnd, end) {
				// At command start Vim still treats this as an Ex command, not
				// an expression.  Block construction below determines whether
				// Vim reports script-level E492 or compiled-def E476.
				file.Diagnostics = append(file.Diagnostics, Diagnostic{
					Code: "vim/E476", Message: "invalid command: whitespace before function arguments",
					Span: nameSpan,
				})
			}
			if canonical == "call" {
				if before, open, ok := spacedVim9CallInArgument(file.Source, argumentStart, argumentEnd); ok {
					file.Diagnostics = append(file.Diagnostics, Diagnostic{
						Code: "vim/E1068", Message: "no white space allowed before function arguments",
						Span: Span{Start: argumentStart + before, End: argumentStart + open},
					})
				}
			}
		}
		if parsedCommand.Dialect == Legacy && legacyTextCommand(parsedCommand.Canonical) {
			parseLegacyTextCommandHeader(file, &parsedCommand)
		}
		detectHeredoc(file, &parsedCommand)
		dynamicHeredoc := parsedCommand.Heredoc != nil && parsedCommand.Canonical == "execute"
		if !dynamicHeredoc && parsedCommand.Canonical == "execute" {
			dynamicHeredoc = detectExecuteHeredoc(file, &parsedCommand)
		}
		file.Commands = append(file.Commands, parsedCommand)
		if builtIn && metadata.Flags&vimdata.NeedArgument != 0 && argumentStart == argumentEnd {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vimls/missing-argument", Message: "command requires an argument", Span: nameSpan})
		}
		if len(file.Diagnostics) > diagnosticsBeforeCommand && separator.Start < separator.End {
			// A known source error makes the rest of this physical command range
			// opaque.  Do not guess that a later bar begins another command; the
			// next physical line is the recovery boundary.
			file.Tokens = append(file.Tokens, Token{Kind: TokenOpaque, Span: Span{Start: separator.Start, End: end}})
			return
		}
		if len(file.Diagnostics) > diagnosticsBeforeCommand {
			// Some command-specific consumers do not expose a separator (for
			// example an invalid Vim9 command abbreviation).  The remainder of
			// the physical range is still recovery data, not another command.
			// Mark it opaque so token consumers do not mistake the retained
			// argument bytes for successfully scanned syntax.
			if argumentStart < end {
				file.Tokens = append(file.Tokens, Token{Kind: TokenOpaque, Span: Span{Start: argumentStart, End: end}})
			}
			return
		}
		if comment.Start < comment.End {
			file.Tokens = append(file.Tokens, Token{Kind: TokenComment, Span: comment})
			return
		}
		if separator.Start == separator.End {
			return
		}
		file.Tokens = append(file.Tokens, Token{Kind: TokenSeparator, Span: separator})
		start = separator.End
	}
}

func selfSplittingVariableCommand(name string) bool {
	switch name {
	case "unlet", "lockvar", "unlockvar":
		return true
	default:
		return false
	}
}

func substituteCommand(name string) bool {
	return substitutePatternCommand(name) || name == "~"
}

func substitutePatternCommand(name string) bool {
	return name == "substitute" || name == "smagic" || name == "snomagic"
}

// legacyOneLetterSubstitute mirrors Vim's one_letter_cmd() exception for :s.
// It runs before ordinary command abbreviation lookup in legacy script only.
func legacyOneLetterSubstitute(source string, start, end int) bool {
	if start >= end || source[start] != 's' {
		return false
	}
	at := func(offset int) byte {
		position := start + offset
		if position >= end {
			return 0
		}
		return source[position]
	}
	second := at(1)
	switch second {
	case 'g', 'I':
		return true
	case 'i':
		third := at(2)
		return third != 'm' && third != 'l' && third != 'g'
	case 'r':
		return at(2) != 'e'
	case 'c':
		third := at(2)
		return third == 0 || third != 's' && third != 'r' && (at(3) == 0 || at(3) != 'i' && at(4) != 'p')
	default:
		return false
	}
}

func globalCommand(name string) bool {
	return name == "global" || name == "vglobal"
}

// scanFilterModifier consumes the pattern owned by :filter.  Vim uses the
// same boundary helper as :vimgrep: an identifier byte starts an
// undelimited pattern, while every other byte is the regexp delimiter.  The
// returned offset is the first byte of the following command (or end for an
// incomplete modifier).
func scanFilterModifier(file *File, modifier *FilterModifier, start, end int) (int, bool) {
	if start >= end {
		return start, false
	}
	if isVimIdentifierByte(file.Source[start]) {
		patternEnd := start
		for patternEnd < end && !isSpace(file.Source[patternEnd]) {
			patternEnd++
		}
		modifier.Pattern = Span{Start: start, End: patternEnd}
		file.Tokens = append(file.Tokens, Token{Kind: TokenArgument, Span: modifier.Pattern})
		return patternEnd, false
	}
	delimiter := file.Source[start]
	modifier.Delimiter = Span{Start: start, End: start + 1}
	// Keep the incomplete regexp as syntax.  In particular, callers need the
	// delimiter and pattern spans even when the closing byte has not yet been
	// typed and the rest of the line must remain opaque.
	closing := scanGlobalRegexpEnd(file.Source, start+1, end, delimiter)
	if closing < 0 {
		modifier.Pattern = Span{Start: start + 1, End: end}
		file.Tokens = append(file.Tokens, Token{Kind: TokenOpaque, Span: Span{Start: start, End: end}})
		return end, true
	}
	modifier.Pattern = Span{Start: start + 1, End: closing}
	flagsEnd := closing + 1
	for flagsEnd < end && (file.Source[flagsEnd] == 'g' || file.Source[flagsEnd] == 'j' || file.Source[flagsEnd] == 'f') {
		flagsEnd++
	}
	if flagsEnd > closing+1 {
		modifier.Flags = Span{Start: closing + 1, End: flagsEnd}
	}
	file.Tokens = append(file.Tokens, Token{Kind: TokenArgument, Span: Span{Start: start, End: flagsEnd}})
	return flagsEnd, false
}

// filterModifierEndsCommand mirrors the ends_excmd exception in Vim's
// parse_command_modifiers().  Legacy :filter never claims a leading | or ",
// while Vim9 permits those bytes as delimiters only when the next byte is not
// whitespace.  Vim9's # follows the same rule so that #pat# is a pattern but
// # pat is a comment/end-of-command boundary.
func filterModifierEndsCommand(source string, start, end int, dialect Dialect) bool {
	if start >= end {
		return false
	}
	character := source[start]
	if character != '|' && character != '"' && !(dialect == Vim9 && character == '#') {
		return false
	}
	if dialect == Legacy {
		return true
	}
	// Double quote is not an ends_excmd byte in Vim9, so it remains a valid
	// delimiter even when the first pattern byte after it is whitespace.
	if character == '"' {
		return false
	}
	return start+1 >= end || isSpace(source[start+1])
}

// isVimIdentifierByte is the byte-level equivalent of vim_isIDc() used by
// skip_vimgrep_pat_ext().  Multibyte characters are identifier bytes in Vim;
// the scanner keeps their source spans byte-oriented throughout.
func isVimIdentifierByte(character byte) bool {
	return character >= 0x80 || character == '_' || character >= '0' && character <= '9' ||
		character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
}

// scanGlobalCommandArgument gives :global and :vglobal ownership of the
// complete physical/logical command range. Their pattern and payload share
// the same Ex argument, so bars and dialect comments must not be exposed as
// outer command boundaries.
func scanGlobalCommandArgument(source string, start, end int) int {
	return trimSpaceEnd(source, start, end)
}

// globalCommandBodySpan returns only the embedded [cmd] span following a
// global regexp (or a previous-pattern marker). A missing or untrusted
// regexp boundary deliberately produces no body; callers then retain the
// complete argument as opaque syntax.
func globalCommandBodySpan(source string, start, end int) (Span, bool) {
	position := skipSpace(source, start, end)
	if position >= end {
		return Span{}, false
	}

	// Undocumented previous-pattern forms use exactly two bytes: g\/, g\?,
	// or g\&. Other leading backslash forms are not safely extractable.
	if source[position] == '\\' {
		if position+1 >= end || (source[position+1] != '/' && source[position+1] != '?' && source[position+1] != '&') {
			return Span{}, false
		}
		bodyStart := skipSpace(source, position+2, end)
		if bodyStart >= end {
			return Span{}, false
		}
		return Span{Start: bodyStart, End: trimSpaceEnd(source, bodyStart, end)}, true
	}

	delimiter := source[position]
	if (delimiter >= 'A' && delimiter <= 'Z') || (delimiter >= 'a' && delimiter <= 'z') {
		return Span{}, false
	}
	closing := scanGlobalRegexpEnd(source, position+1, end, delimiter)
	if closing < 0 {
		return Span{}, false
	}
	bodyStart := skipSpace(source, closing+1, end)
	if bodyStart >= end {
		return Span{}, false
	}
	return Span{Start: bodyStart, End: trimSpaceEnd(source, bodyStart, end)}, true
}

const (
	globalMagicOn uint8 = iota
	globalMagicAll
	globalMagicNone
)

// scanGlobalRegexpEnd follows Vim's skip_regexp_ex boundary rules for the
// byte-oriented spans used by this parser. It recognizes magic switches and
// skips character collections, including POSIX/class, collating, and
// equivalence elements, so an inner delimiter cannot close the pattern.
func scanGlobalRegexpEnd(source string, start, end int, delimiter byte) int {
	return scanRegexpEndWithMagic(source, start, end, delimiter, globalMagicOn)
}

func scanRegexpEndWithMagic(source string, start, end int, delimiter byte, magic uint8) int {
	for position := start; position < end; {
		character := source[position]
		if character == delimiter {
			return position
		}
		if character == '\\' {
			if position+1 >= end {
				position++
				continue
			}
			next := source[position+1]
			switch next {
			case 'v':
				magic = globalMagicAll
			case 'V':
				magic = globalMagicNone
			}
			if next == '[' && magic == globalMagicNone {
				collectionEnd := scanGlobalRegexpCollection(source, position+2, end)
				if collectionEnd < 0 {
					return -1
				}
				position = collectionEnd + 1
				continue
			}
			position += 2
			continue
		}
		if character == '[' && (magic == globalMagicOn || magic == globalMagicAll) {
			collectionEnd := scanGlobalRegexpCollection(source, position+1, end)
			if collectionEnd < 0 {
				return -1
			}
			position = collectionEnd + 1
			continue
		}
		position++
	}
	return -1
}

func scanGlobalRegexpCollection(source string, start, end int) int {
	position := start
	if position < end && source[position] == '^' {
		position++
	}
	if position < end && (source[position] == ']' || source[position] == '-') {
		position++
	}
	for position < end {
		if source[position] == ']' {
			return position
		}
		if source[position] == '\\' {
			if position+1 < end {
				position += 2
			} else {
				position++
			}
			continue
		}
		if source[position] == '[' && position+1 < end {
			marker := source[position+1]
			if marker == ':' || marker == '.' || marker == '=' {
				candidate := position + 2
				for candidate+1 < end && !(source[candidate] == marker && source[candidate+1] == ']') {
					if source[candidate] == '\\' && candidate+1 < end {
						candidate += 2
					} else {
						candidate++
					}
				}
				if candidate+1 < end {
					position = candidate + 2
					continue
				}
				// Vim's get_char_class()/get_equi_class()/get_coll_element()
				// leave the pointer at the inner '[' when the apparent class is
				// incomplete. Treat it as a literal and keep looking for the
				// outer collection's closing bracket.
			}
		}
		position++
	}
	return -1
}

func legacyTextCommand(name string) bool {
	switch name {
	case "append", "change", "insert":
		return true
	default:
		return false
	}
}

func parseLegacyTextCommandHeader(file *File, command *Command) {
	start := command.Argument.Start
	if command.Canonical == "change" {
		countEnd := start
		for countEnd < command.Argument.End && file.Source[countEnd] >= '0' && file.Source[countEnd] <= '9' {
			countEnd++
		}
		if countEnd > start {
			command.Count = Span{Start: start, End: countEnd}
			start = skipSpace(file.Source, countEnd, command.Argument.End)
		}
	}
	if start >= command.Argument.End || file.Source[start] == '|' {
		return
	}
	file.Diagnostics = append(file.Diagnostics, Diagnostic{
		Code: "vim/E488", Message: "trailing characters", Span: Span{Start: start, End: command.Argument.End},
	})
}

func legacyTextCommandHeaderValid(file *File, command *Command) bool {
	start := command.Argument.Start
	if command.Count.Start < command.Count.End {
		start = skipSpace(file.Source, command.Count.End, command.Argument.End)
	}
	return start >= command.Argument.End || file.Source[start] == '|'
}

func legacyTextCommandHeaderSourceValid(source, canonical string, start, end int) bool {
	if canonical == "change" {
		for start < end && source[start] >= '0' && source[start] <= '9' {
			start++
		}
		start = skipSpace(source, start, end)
	}
	return start >= end || source[start] == '|'
}

func textBodyRecoveryCommand(active Dialect, dialectStack []Dialect) string {
	if active != Legacy {
		return ""
	}
	return enclosingFunctionEnd(active, dialectStack)
}

func enclosingFunctionEnd(active Dialect, dialectStack []Dialect) string {
	if len(dialectStack) == 0 {
		return ""
	}
	if active == Legacy {
		return "endfunction"
	}
	return "enddef"
}

func scanMetadataForParsedCommand(command Command) vimdata.Command {
	if command.Kind == CommandExpression {
		return vimdata.Command{Flags: vimdata.ExpressionArgument}
	}
	metadata, _ := vimdata.Lookup(command.Canonical)
	if selfSplittingVariableCommand(command.Canonical) {
		metadata.Flags |= vimdata.AllowBar
	}
	if expressionCommand(command.Canonical) {
		metadata.Flags |= vimdata.ExpressionArgument
	}
	return metadata
}

func detectHeredoc(file *File, command *Command) bool {
	argument := file.Text(command.Argument)
	suffixOffset := -1
	assignmentOffset := -1
	scriptGet := false
	if command.Kind == CommandExpression {
		index := findHeredocAssignment(argument)
		if index < 0 {
			return false
		}
		assignmentOffset = index
		suffixOffset = index + 3
	}
	switch command.Canonical {
	case "let", "var", "const", "final":
		index := findHeredocAssignment(argument)
		if index < 0 {
			return false
		}
		assignmentOffset = index
		suffixOffset = index + 3
	case "python", "py3", "python3", "pyx", "pythonx", "ruby", "perl", "lua", "mzscheme", "tcl":
		index := strings.Index(argument, "<<")
		if index < 0 {
			return false
		}
		suffixOffset = index + 2
		scriptGet = true
	default:
		if suffixOffset < 0 {
			return false
		}
	}
	if assignmentOffset >= 0 {
		diagnostics := len(file.Diagnostics)
		diagnoseVim9AssignmentSpacing(file, command, Span{
			Start: command.Argument.Start + assignmentOffset,
			End:   command.Argument.Start + assignmentOffset + 3,
		})
		if len(file.Diagnostics) > diagnostics {
			command.detailsOpaque = true
			return false
		}
	}

	position := skipSpace(argument, suffixOffset, len(argument))
	trim := false
	eval := false
	for position < len(argument) {
		wordEnd := scanWord(argument, position, len(argument))
		word := argument[position:wordEnd]
		if word != "trim" && word != "eval" {
			break
		}
		trim = trim || word == "trim"
		eval = eval || word == "eval"
		position = skipSpace(argument, wordEnd, len(argument))
	}
	if position >= len(argument) {
		if !scriptGet {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E172", Message: "missing marker", Span: command.Argument,
			})
			return false
		}
		command.Heredoc = &Heredoc{Marker: ".", Trim: trim, Eval: eval}
		return true
	}
	markerStart := position
	for position < len(argument) && !isSpace(argument[position]) {
		position++
	}
	marker := argument[markerStart:position]
	trailing := skipSpace(argument, position, len(argument))
	if trailing < len(argument) {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E488", Message: "trailing characters", Span: Span{Start: command.Argument.Start + trailing, End: command.Argument.End},
		})
		return false
	}
	if !scriptGet && marker[0] >= 'a' && marker[0] <= 'z' {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E221", Message: "marker cannot start with lower case letter", Span: Span{Start: command.Argument.Start + markerStart, End: command.Argument.Start + position},
		})
		return false
	}
	command.Heredoc = &Heredoc{Marker: marker, Trim: trim, Eval: eval}
	return true
}

func heredocEndMarkerMatches(source string, command *Command, lineStart, lineEnd int) bool {
	if command == nil || command.Heredoc == nil {
		return false
	}
	markerStart := lineStart
	if command.Heredoc.Trim {
		commandLineStart := strings.LastIndexByte(source[:command.Span.Start], '\n') + 1
		indentEnd := commandLineStart
		for indentEnd < command.Span.Start && isSpace(source[indentEnd]) {
			indentEnd++
		}
		indent := source[commandLineStart:indentEnd]
		if strings.HasPrefix(source[lineStart:lineEnd], indent) {
			markerStart += len(indent)
		}
	}
	return source[markerStart:lineEnd] == command.Heredoc.Marker
}

// detectExecuteHeredoc recognizes only a statically visible heredoc marker in
// :execute's argument.  The command evaluates each expression at runtime, so
// guessing from a variable or a concatenation would hide Vim source that may
// not actually be a heredoc.  The plug.vim form, for example,
// `execute py_exe "<< EOF"`, has a final plain string literal whose value is
// sufficient to determine the physical marker without evaluating the script.
func detectExecuteHeredoc(file *File, command *Command) bool {
	if file == nil || command == nil || command.Canonical != "execute" || command.Argument.Start >= command.Argument.End {
		return false
	}
	source := file.Text(command.Argument)
	lexer := newExpressionLexer(source, command.Argument.Start, Legacy)
	if lexer.current.kind == expressionEOF {
		return false
	}
	last := lexer.current
	for lexer.advance(); lexer.current.kind != expressionEOF; lexer.advance() {
		last = lexer.current
	}
	if last.kind != expressionString || last.span.End-command.Argument.Start != len(source) {
		return false
	}
	marker, trim, eval, ok := staticHeredocMarker(last.text)
	if !ok {
		return false
	}
	command.Heredoc = &Heredoc{Marker: marker, Trim: trim, Eval: eval}
	return true
}

func staticHeredocMarker(literal string) (string, bool, bool, bool) {
	if len(literal) < 2 {
		return "", false, false, false
	}
	quote := literal[0]
	if (quote != '\'' && quote != '"') || literal[len(literal)-1] != quote {
		return "", false, false, false
	}
	inner := literal[1 : len(literal)-1]
	// Only plain literals are statically decoded here.  Backslash escapes in
	// double-quoted strings and doubled quotes in single-quoted strings would
	// require evaluating Vim's string grammar; leaving those dynamic is safer.
	if strings.ContainsRune(inner, '\\') || strings.Contains(inner, "''") || strings.ContainsRune(inner, rune(quote)) {
		return "", false, false, false
	}
	fields := strings.Fields(inner)
	if len(fields) < 2 || fields[0] != "<<" {
		return "", false, false, false
	}
	trim := false
	eval := false
	position := 1
	for position < len(fields)-1 && (fields[position] == "trim" || fields[position] == "eval") {
		trim = trim || fields[position] == "trim"
		eval = eval || fields[position] == "eval"
		position++
	}
	if position != len(fields)-1 {
		return "", false, false, false
	}
	marker := fields[position]
	if validHeredocMarker(marker) {
		return marker, trim, eval, true
	}
	return "", false, false, false
}

func validHeredocMarker(marker string) bool {
	if marker == "" || marker == "trim" || marker == "eval" || strings.ContainsAny(marker, "'\"") {
		return false
	}
	return strings.IndexFunc(marker, func(character rune) bool {
		return unicode.IsSpace(character)
	}) < 0
}

// parseLoadKeymapBody consumes the physical lines following :loadkeymap.
// Vim's ex_loadkeymap() switches 'cpoptions' to "C" and reads to EOF, so
// bars, continuations, quotes, and apparent Ex commands in this region are
// keymap data rather than script syntax.
func parseLoadKeymapBody(file *File, command *Command, start int, commandBlock bool) int {
	if command == nil {
		return start
	}
	if start < 0 {
		start = 0
	}
	if start > len(file.Source) {
		start = len(file.Source)
	}
	keymap := &LoadKeymap{Body: Span{Start: start, End: start}}
	for lineStart := start; lineStart < len(file.Source); {
		contentEnd, next := physicalLineEnd(file.Source, lineStart)
		if commandBlock && commandBlockEndLine(file.Source[lineStart:contentEnd]) {
			keymap.Body.End = contentEnd
			command.Span.End = contentEnd
			command.Keymap = keymap
			return lineStart
		}
		parseLoadKeymapLine(file, keymap, lineStart, contentEnd)
		keymap.Body.End = next
		if lineStart < contentEnd {
			file.Tokens = append(file.Tokens, Token{Kind: TokenOpaque, Span: Span{Start: lineStart, End: contentEnd}})
		}
		if contentEnd < next {
			file.Tokens = append(file.Tokens, Token{Kind: TokenNewline, Span: Span{Start: contentEnd, End: next}})
		}
		lineStart = next
	}
	command.Span.End = keymap.Body.End
	command.Keymap = keymap
	return len(file.Source)
}

// parseLegacyTextBody consumes physical input lines for :append, :change, and
// :insert. It intentionally uses exact physical lines: Vim's function reader
// also requires an exact "." source line, while runtime autoindent is buffer
// state that static analysis cannot safely guess.
func parseLegacyTextBody(file *File, command *Command, start int, recoveryCommand string, commandBlock bool) int {
	if file == nil || command == nil {
		return start
	}
	if start < 0 {
		start = 0
	}
	if start > len(file.Source) {
		start = len(file.Source)
	}

	body := &TextBody{Body: Span{Start: start, End: start}}
	inlineStart := command.Argument.Start
	if command.Count.Start < command.Count.End {
		inlineStart = skipSpace(file.Source, command.Count.End, command.Argument.End)
	}
	if inlineStart < command.Argument.End && inlineStart >= 0 && command.Argument.End <= len(file.Source) && file.Source[inlineStart] == '|' {
		body.Separator = Span{Start: inlineStart, End: inlineStart + 1}
		inline := Span{Start: body.Separator.End, End: command.Argument.End}
		body.Body = inline
		body.Lines = append(body.Lines, inline)
	}

	recoveryOffset := -1
	recoveryLineCount := 0
	recoveryBodyEnd := body.Body.End
	recoveryCommandEnd := command.Span.End
	recoveryTokenCount := len(file.Tokens)
	for offset := start; offset < len(file.Source); {
		contentEnd, next := physicalLineEnd(file.Source, offset)
		line := Span{Start: offset, End: contentEnd}
		if commandBlock && commandBlockEndLine(file.Source[offset:contentEnd]) {
			// The source collector for :command { ... } copies this line into
			// the replacement and then closes the definition without knowing
			// that :append/:change/:insert wanted more input. Represent both
			// roles: the runtime text body contains the line, while the outer
			// scanner will parse the same source byte as CommandBlockEnd.
			if len(body.Lines) == 0 {
				body.Body.Start = offset
			}
			body.Body.End = contentEnd
			body.Lines = append(body.Lines, line)
			body.Incomplete = true
			command.Span.End = contentEnd
			command.TextBody = body
			return offset
		}
		if file.Source[offset:contentEnd] == "." {
			body.EndMarker = line
			command.Span.End = contentEnd
			file.Tokens = append(file.Tokens, Token{Kind: TokenOpaque, Span: line})
			if contentEnd < next {
				file.Tokens = append(file.Tokens, Token{Kind: TokenNewline, Span: Span{Start: contentEnd, End: next}})
			}
			command.TextBody = body
			return next
		}
		if recoveryOffset < 0 && recoveryCommand != "" && isPayloadRecoveryLine(file.Source, offset, contentEnd, recoveryCommand) {
			recoveryOffset = offset
			recoveryLineCount = len(body.Lines)
			recoveryBodyEnd = body.Body.End
			recoveryCommandEnd = command.Span.End
			recoveryTokenCount = len(file.Tokens)
		}
		if len(body.Lines) == 0 {
			body.Body.Start = offset
		}
		body.Body.End = contentEnd
		body.Lines = append(body.Lines, line)
		command.Span.End = contentEnd
		if offset < contentEnd {
			file.Tokens = append(file.Tokens, Token{Kind: TokenOpaque, Span: line})
		}
		if contentEnd < next {
			file.Tokens = append(file.Tokens, Token{Kind: TokenNewline, Span: Span{Start: contentEnd, End: next}})
		}
		offset = next
	}
	if recoveryOffset >= 0 {
		body.Lines = body.Lines[:recoveryLineCount]
		body.Body.End = recoveryBodyEnd
		command.Span.End = recoveryCommandEnd
		file.Tokens = file.Tokens[:recoveryTokenCount]
		body.Incomplete = true
		command.TextBody = body
		return recoveryOffset
	}
	body.Incomplete = true
	command.TextBody = body
	return len(file.Source)
}

func parseLegacyInlineTextBody(file *File, command *Command) {
	if file == nil || command == nil {
		return
	}
	inlineStart := command.Argument.Start
	if command.Count.Start < command.Count.End {
		inlineStart = skipSpace(file.Source, command.Count.End, command.Argument.End)
	}
	if inlineStart >= command.Argument.End || file.Source[inlineStart] != '|' {
		return
	}
	separator := Span{Start: inlineStart, End: inlineStart + 1}
	line := Span{Start: separator.End, End: command.Argument.End}
	command.TextBody = &TextBody{Separator: separator, Body: line, Lines: []Span{line}}
}

func isPayloadRecoveryLine(source string, start, end int, canonical string) bool {
	start = skipSpace(source, start, end)
	if start < end && source[start] == ':' {
		start = skipSpace(source, start+1, end)
	}
	nameEnd := scanCommandName(source, start, end)
	if nameEnd == start {
		return false
	}
	metadata, ok := vimdata.Lookup(source[start:nameEnd])
	if !ok || metadata.Name != canonical {
		return false
	}
	position := nameEnd
	if canonical == "endfunction" && position < end && source[position] == '!' {
		position++
	}
	position = skipSpace(source, position, end)
	if position == end {
		return true
	}
	if canonical == "enddef" {
		return source[position] == '#'
	}
	return source[position] == '"'
}

func parseLoadKeymapLine(file *File, keymap *LoadKeymap, lineStart, lineEnd int) {
	start := skipSpace(file.Source, lineStart, lineEnd)
	if start >= lineEnd || file.Source[start] == '"' {
		return
	}
	fromEnd := scanKeymapWord(file.Source, start, lineEnd)
	if fromEnd == start {
		return
	}
	toStart := skipSpace(file.Source, fromEnd, lineEnd)
	if toStart >= lineEnd {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E791", Message: "empty keymap entry", Span: Span{Start: start, End: fromEnd},
		})
		return
	}
	toEnd := scanKeymapWord(file.Source, toStart, lineEnd)
	if toEnd == toStart {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E791", Message: "empty keymap entry", Span: Span{Start: start, End: fromEnd},
		})
		return
	}
	// Vim silently ignores entries whose two fields exceed KMAP_LLEN (200)
	// bytes. Keep that behavior while retaining valid entries structurally.
	if fromEnd-start+toEnd-toStart >= 200 {
		return
	}
	keymap.Entries = append(keymap.Entries, KeymapEntry{
		From: Span{Start: start, End: fromEnd},
		To:   Span{Start: toStart, End: toEnd},
	})
}

func scanKeymapWord(source string, start, end int) int {
	for start < end && !isSpace(source[start]) {
		start++
	}
	return start
}

func findHeredocAssignment(source string) int {
	quote := byte(0)
	depth := 0
	for index := 0; index+2 < len(source); index++ {
		character := source[index]
		if quote != 0 {
			if character == '\\' && quote == '"' {
				index++
			} else if character == quote {
				if quote == '\'' && index+1 < len(source) && source[index+1] == '\'' {
					index++
				} else {
					quote = 0
				}
			}
			continue
		}
		if character == '@' && index+1 < len(source) {
			_, size := utf8.DecodeRuneInString(source[index+1:])
			index += size
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			continue
		}
		switch character {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case '=':
			if depth == 0 && strings.HasPrefix(source[index:], "=<<") {
				return index
			}
		}
	}
	return -1
}

func parseCommandDetails(file *File, command *Command) {
	parseCommandDetailsDepth(file, command, 0)
}

func parseCommandDetailsDepth(file *File, command *Command, depth int) {
	if len(command.EnumValues) > 0 {
		return
	}
	if command.Dialect == Vim9 && len(command.Modifiers) > 0 {
		switch command.Canonical {
		case "endif", "endfor", "endwhile", "try", "catch", "finally", "endtry":
			allowSilentLoopEnd := command.Canonical == "endfor" || command.Canonical == "endwhile"
			for _, modifier := range command.Modifiers {
				if modifier.Name != "silent" && modifier.Name != "unsilent" {
					allowSilentLoopEnd = false
					break
				}
			}
			if allowSilentLoopEnd {
				for block := command.Block; block >= 0 && block < len(file.Blocks); block = file.Blocks[block].Parent {
					if file.Blocks[block].Kind == BlockDef {
						allowSilentLoopEnd = false
						break
					}
				}
			}
			if allowSilentLoopEnd {
				break
			}
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1176", Message: "misplaced command modifier", Span: command.Modifiers[0].Span,
			})
			return
		}
	}
	diagnoseEnumEndTrailingCharacters(file, command)
	diagnoseEnumAbstractMember(file, command)
	if isMappingCommand(command.Canonical) {
		parseMapping(file, command)
		return
	}
	if globalCommand(command.Canonical) {
		if boundary := command.boundaryExpression; boundary != nil && len(boundary.diagnostics) > 0 {
			file.Diagnostics = append(file.Diagnostics, boundary.diagnostics...)
			command.boundaryExpression = nil
			return
		}
		if body, ok := globalCommandBodySpan(file.Source, command.Argument.Start, command.Argument.End); ok {
			command.Embedded = parseEmbeddedCommandList(file, body, command.baseDialect, depth)
		}
		return
	}
	if command.Substitute != nil {
		file.Diagnostics = append(file.Diagnostics, command.Substitute.diagnostics...)
		command.Substitute.diagnostics = nil
		command.boundaryExpression = nil
		return
	}
	if command.Highlight != nil {
		if boundary := command.boundaryExpression; boundary != nil {
			file.Diagnostics = append(file.Diagnostics, boundary.diagnostics...)
		}
		command.boundaryExpression = nil
		return
	}
	if command.Syntax != nil {
		if boundary := command.boundaryExpression; boundary != nil {
			file.Diagnostics = append(file.Diagnostics, boundary.diagnostics...)
		}
		command.boundaryExpression = nil
		return
	}
	if command.Set != nil {
		if boundary := command.boundaryExpression; boundary != nil {
			file.Diagnostics = append(file.Diagnostics, boundary.diagnostics...)
		}
		command.boundaryExpression = nil
		return
	}
	if command.Dialect == Vim9 {
		if metadata, ok := vimdata.Lookup(command.Canonical); ok && metadata.Flags&vimdata.FileArgument != 0 {
			if boundary := command.boundaryExpression; boundary != nil {
				file.Diagnostics = append(file.Diagnostics, boundary.diagnostics...)
				command.boundaryExpression = nil
			}
		}
	}
	if command.Argument.Start >= command.Argument.End {
		if command.Dialect == Vim9 && command.Canonical == "type" {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1397", Message: "missing type alias name", Span: command.Name,
			})
		}
		if command.Canonical == "for" {
			parseForLoop(file, command)
		}
		if command.Canonical == "autocmd" {
			command.Autocmd, _, _, _ = parseAutocmdHeader(file.Source, command.Argument, command.Dialect, command.Bang.Start < command.Bang.End)
		}
		if isEmbeddedCommand(command.Canonical) {
			command.Embedded = &CommandList{Span: command.Argument}
		}
		return
	}
	source := file.Text(command.Argument)
	diagnoseClassMemberModifierOrder(file, command)
	if isEmbeddedCommand(command.Canonical) {
		if listDoCommand(command.Canonical) && command.baseDialect == Legacy && strings.Contains(source, "\n") {
			command.Embedded = parseLegacyDoCommandList(file, command.Argument, depth)
		} else {
			command.Embedded = parseEmbeddedCommandList(file, command.Argument, command.baseDialect, depth)
		}
		return
	}
	if command.Canonical == "autocmd" {
		autocmd, body, ok, block := parseAutocmdHeader(file.Source, command.Argument, command.Dialect, command.Bang.Start < command.Bang.End)
		command.Autocmd = autocmd
		if command.Dialect == Vim9 {
			for _, modifier := range autocmd.Modifiers {
				if modifier.Kind == AutocmdNested && file.Text(modifier.Span) == "nested" {
					file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E1078", Message: "nested is not supported in Vim9 script; use ++nested", Span: modifier.Span})
				}
			}
		}
		seenOnce, seenNested, duplicateReported := false, false, false
		for _, modifier := range autocmd.Modifiers {
			duplicate := modifier.Kind == AutocmdOnce && seenOnce || modifier.Kind == AutocmdNested && seenNested
			if modifier.Kind == AutocmdOnce {
				seenOnce = true
			} else {
				seenNested = true
			}
			if duplicate && !duplicateReported {
				file.Diagnostics = append(file.Diagnostics, Diagnostic{Code: "vim/E983", Message: "duplicate ++once/++nested", Span: modifier.Span})
				duplicateReported = true
			}
		}
		if ok {
			bodyDialect := command.Dialect
			if block && (command.collectedBlockVim9 || command.Dialect == Vim9) {
				// Vim collects an autocmd block with the Vim9 command reader even
				// when the command itself occurs in a legacy script.
				bodyDialect = Vim9
			}
			if bodyDialect == Legacy {
				command.Embedded = parseLegacyAutocmdCommandList(file, body, depth)
			} else if block {
				command.Embedded = parseVim9AutocmdBlockCommandList(file, body, depth)
			} else {
				command.Embedded = parseEmbeddedCommandList(file, body, bodyDialect, depth)
			}
		}
		return
	}
	if command.Canonical == "command" {
		diagnoseUserCommandAttributes(file, command)
		// A Vim9 command block is already represented by the top-level command
		// stream and its BlockCommand. Attaching a second CommandList would make
		// semantic consumers visit the same declarations and references twice.
		if command.Block >= 0 && command.Block < len(file.Blocks) && file.Blocks[command.Block].Kind == BlockCommand {
			return
		}
		if body, ok := userCommandBodySpan(file.Source, command.Argument); ok {
			if command.collectedBlockVim9 {
				open, direct, found := collectedCommandBlockStart(file.Source, command, command.Argument.End)
				if direct && found {
					strayClose, hasStrayClose := collectedCommandStrayClose(file.Source, command.Argument.End)
					diagnosticsBeforeBody := len(file.Diagnostics)
					bodyStart := autocmdBlockBodyStart(file.Source, open.Start, command.Argument.End, command.Dialect)
					if closeStart, _, closed := autocmdBlockClose(file.Source, open.End, command.Argument.End); closed {
						command.Embedded = parseVim9AutocmdBlockCommandList(file, Span{Start: bodyStart, End: closeStart}, depth)
					} else {
						command.Embedded = parseVim9AutocmdBlockCommandList(file, Span{Start: bodyStart, End: command.Argument.End}, depth)
					}
					if hasStrayClose {
						// The collector has already consumed the first line-leading }.
						// A second one is parsed by the command-block reader and is
						// Vim's E1128, not an incomplete nested expression in the
						// stored replacement body.
						kept := file.Diagnostics[:diagnosticsBeforeBody]
						for _, diagnostic := range file.Diagnostics[diagnosticsBeforeBody:] {
							if (diagnostic.Code == "vimls/missing-delimiter" || diagnostic.Code == "vim/E723") && diagnostic.Span.Start >= bodyStart && diagnostic.Span.End <= command.Argument.End {
								continue
							}
							kept = append(kept, diagnostic)
						}
						file.Diagnostics = append(kept, Diagnostic{
							Code: "vim/E1128", Message: "} without {", Span: strayClose,
						})
					}
					return
				}
			}
			command.Embedded = parseEmbeddedCommandList(file, body, command.Dialect, depth)
		}
		return
	}
	switch command.Canonical {
	case "class":
		parseAggregate(file, command, BlockClass)
		return
	case "interface":
		parseAggregate(file, command, BlockInterface)
		return
	case "enum":
		parseAggregate(file, command, BlockEnum)
		return
	case "type":
		parseTypeAlias(file, command)
		return
	case "import":
		parseImport(file, command)
		return
	}
	if command.Canonical == "def" || command.Canonical == "function" && isFunctionDefinition(source) {
		parseFunctionSignature(file, command)
		return
	}
	if command.Kind == CommandExpression {
		if assignment := findAssignment(source); assignment.Start >= 0 {
			leftEnd := trimSpaceEnd(source, 0, assignment.Start)
			rightStart := skipSpace(source, assignment.End, len(source))
			left, leftDiagnostics := parseExpression(source[:leftEnd], command.Argument.Start, command.Dialect)
			if command.Dialect == Vim9 && left != nil && left.Kind == ExpressionIdentifier && strings.HasPrefix(left.Value, "@") {
				name, size := utf8.DecodeRuneInString(left.Value[1:])
				if size > 0 && 1+size == len(left.Value) {
					nameSpan := Span{Start: left.Span.Start + 1, End: left.Span.Start + 1 + size}
					if name == '@' {
						// @@ is the assignment-only alias for the unnamed register.
						// Keep it invalid in the ordinary register-read parser.
						kept := leftDiagnostics[:0]
						for _, diagnostic := range leftDiagnostics {
							if diagnostic.Code != "vim/E354" || diagnostic.Span != nameSpan {
								kept = append(kept, diagnostic)
							}
						}
						leftDiagnostics = kept
					} else if !validRegisterName(name) || strings.ContainsRune(".%:~", name) {
						diagnosed := false
						for _, diagnostic := range leftDiagnostics {
							diagnosed = diagnosed || diagnostic.Code == "vim/E354" && diagnostic.Span == nameSpan
						}
						if !diagnosed {
							leftDiagnostics = append(leftDiagnostics, Diagnostic{
								Code: "vim/E354", Message: "Invalid register name: '" + string(name) + "'", Span: nameSpan,
							})
						}
					}
				}
			}
			rhs := Span{Start: command.Argument.Start + rightStart, End: command.Argument.End}
			right, rightDiagnostics, reused := takeValidBoundaryExpression(command, rhs)
			if !reused {
				right, rightDiagnostics = parseExpression(source[rightStart:], command.Argument.Start+rightStart, command.Dialect)
			}
			operator := Span{Start: command.Argument.Start + assignment.Start, End: command.Argument.Start + assignment.End}
			diagnoseVim9AssignmentSpacing(file, command, operator)
			command.Expressions = append(command.Expressions, &Expression{
				Kind: ExpressionAssignment, Span: Span{Start: left.Span.Start, End: right.Span.End}, Operator: operator,
				Value: file.Text(operator), Children: []*Expression{left, right},
			})
			file.Diagnostics = append(file.Diagnostics, mapIncompleteExpressionDiagnostics(file, command, leftDiagnostics)...)
			file.Diagnostics = append(file.Diagnostics, mapIncompleteExpressionDiagnostics(file, command, rightDiagnostics)...)
			return
		}
		expression, diagnostics, reused := takeBoundaryExpression(command)
		if !reused {
			expression, diagnostics = parseExpression(source, command.Argument.Start, command.Dialect)
		}
		command.Expressions = append(command.Expressions, expression)
		if len(diagnostics) == 1 && (diagnostics[0].Code == "vimls/missing-list-end" || diagnostics[0].Code == "vimls/missing-member" || diagnostics[0].Code == "vimls/invalid-member-tail" || diagnostics[0].Code == "vim/E722" || diagnostics[0].Code == "vim/E260") {
			diagnostics = mapIncompleteExpressionDiagnostics(file, command, diagnostics)
		}
		file.Diagnostics = append(file.Diagnostics, diagnostics...)
		return
	}
	switch command.Canonical {
	case "let", "var", "const", "final":
		assignment := findAssignment(source)
		if assignment.Start < 0 {
			declaration := parseDeclarationHead(file, source, command.Argument.Start, command.Dialect)
			diagnoseInvalidClassDeclaration(file, command, declaration)
			command.Declaration = declaration
			return
		}
		assignment.Start += command.Argument.Start
		assignment.End += command.Argument.Start
		left := file.Source[command.Argument.Start:assignment.Start]
		declaration := parseDeclarationHead(file, left, command.Argument.Start, command.Dialect)
		invalidClassDeclaration := diagnoseInvalidClassDeclaration(file, command, declaration)
		if !invalidClassDeclaration {
			diagnoseVim9AssignmentSpacing(file, command, assignment)
		}
		rightStart := skipSpace(file.Source, assignment.End, command.Argument.End)
		rhs := Span{Start: rightStart, End: command.Argument.End}
		expression, diagnostics, reused := takeRecoveringBoundaryExpression(command, rhs)
		if !reused {
			expression, diagnostics = parseExpression(file.Source[rightStart:command.Argument.End], rightStart, command.Dialect)
		}
		declaration.Target, diagnostics = parseDeclarationTarget(file, command, declaration, left, diagnostics)
		declaration.Assignment = assignment
		declaration.Initializer = expression
		diagnoseInvalidInterfaceDeclaration(file, command, declaration)
		command.Declaration = declaration
		command.Expressions = append(command.Expressions, &Expression{
			Kind: ExpressionAssignment, Span: Span{Start: declaration.Target.Span.Start, End: expression.Span.End}, Operator: assignment,
			Value: file.Text(assignment), Children: []*Expression{declaration.Target, expression},
		})
		file.Diagnostics = append(file.Diagnostics, mapIncompleteExpressionDiagnostics(file, command, diagnostics)...)
	case "for":
		parseForLoop(file, command)
	case "delfunction":
		// :delfunction consumes one function-name expression.  Preserve useful
		// member/index structure while treating any remaining bytes as Vim's
		// E488 trailing-character error.
		target, diagnostics, consumed := parseExpressionPrefix(source, command.Argument.Start, command.Dialect)
		if target != nil && target.Kind != ExpressionMissing {
			command.Targets = append(command.Targets, target)
		}
		if len(diagnostics) == 0 && consumed < len(source) {
			diagnostics = append(diagnostics, Diagnostic{
				Code: "vim/E488", Message: "trailing characters",
				Span: Span{Start: command.Argument.Start + consumed, End: command.Argument.End},
			})
		}
		file.Diagnostics = append(file.Diagnostics, diagnostics...)
	case "++", "--":
		target, diagnostics := parseExpression(source, command.Argument.Start, command.Dialect)
		command.Targets = append(command.Targets, target)
		command.Expressions = append(command.Expressions, &Expression{Kind: ExpressionUnary, Span: Span{Start: command.Name.Start, End: target.Span.End}, Operator: command.Name, Value: command.Canonical, Children: []*Expression{target}})
		file.Diagnostics = append(file.Diagnostics, diagnostics...)
	case "unlet", "lockvar", "unlockvar":
		parseVariableTargets(file, command)
	case "if", "elseif", "while", "return", "throw", "call", "eval", "defer", "caddexpr", "cexpr", "cgetexpr", "laddexpr", "lexpr", "lgetexpr":
		expression, diagnostics, ok := takeBoundaryExpression(command)
		if !ok {
			expression, diagnostics = parseExpression(source, command.Argument.Start, command.Dialect)
		}
		command.Expressions = append(command.Expressions, expression)
		file.Diagnostics = append(file.Diagnostics, mapVim9AttachedHashDiagnostics(file, command, diagnostics)...)
	case "echo", "echon", "echomsg", "echoerr", "echoconsole", "echowindow", "execute":
		if command.expressionsParsed {
			if boundary := command.boundaryExpression; boundary != nil {
				file.Diagnostics = append(file.Diagnostics, boundary.diagnostics...)
				command.boundaryExpression = nil
			}
			return
		}
		for consumed := 0; consumed < len(source); {
			consumed = skipSpace(source, consumed, len(source))
			if consumed == len(source) {
				break
			}
			expression, diagnostics, length := parseExpressionPrefix(source[consumed:], command.Argument.Start+consumed, command.Dialect)
			command.Expressions = append(command.Expressions, expression)
			file.Diagnostics = append(file.Diagnostics, mapIncompleteExpressionDiagnostics(file, command, diagnostics)...)
			if len(diagnostics) > 0 || length <= 0 {
				break
			}
			consumed += length
		}
	case "put", "iput":
		if command.expressionsParsed {
			return
		}
		start := skipSpace(source, 0, len(source))
		if start < len(source) && source[start] == '=' {
			start = skipExpressionSpace(source, start+1)
			if command.Dialect == Legacy && start >= len(source) {
				return
			}
			var expression *Expression
			var diagnostics []Diagnostic
			if command.Dialect == Legacy {
				rhs := Span{Start: command.Argument.Start + start, End: command.Argument.End}
				expression, diagnostics, _ = takeLegacyPutExpressionBoundary(command, rhs)
			} else {
				rhs := Span{Start: command.Argument.Start + start, End: command.Argument.End}
				expression, diagnostics, _ = takeRecoveringBoundaryExpression(command, rhs)
			}
			if expression == nil {
				expression, diagnostics = parseExpression(source[start:], command.Argument.Start+start, command.Dialect)
			}
			command.Expressions = append(command.Expressions, expression)
			file.Diagnostics = append(file.Diagnostics, diagnostics...)
		}
	}
}

func mapIncompleteExpressionDiagnostics(file *File, command *Command, diagnostics []Diagnostic) []Diagnostic {
	diagnostics = mapVim9AttachedHashDiagnostics(file, command, diagnostics)
	if command.Declaration != nil && command.Declaration.Initializer != nil && command.Declaration.Initializer.Kind == ExpressionLambda {
		for index := range diagnostics {
			if diagnostics[index].Code == "vim/E1145" {
				diagnostics[index].Span = command.Declaration.Name
			}
		}
	}
	inDef := false
	for block := command.Block; block >= 0 && block < len(file.Blocks); block = file.Blocks[block].Parent {
		if file.Blocks[block].Kind == BlockDef {
			inDef = true
			break
		}
	}
	assignmentExpression := false
	for _, expression := range command.Expressions {
		if expression != nil && expression.Kind == ExpressionAssignment {
			assignmentExpression = true
			break
		}
	}
	hasAssignment := assignmentExpression || command.Declaration != nil && command.Declaration.Assignment.End > command.Declaration.Assignment.Start
	commandDictionary := len(command.Expressions) == 1 && command.Expressions[0] != nil && command.Expressions[0].Kind == ExpressionDictionary
	commandCall := len(command.Expressions) == 1 && command.Expressions[0] != nil && command.Expressions[0].Kind == ExpressionCall
	kept := diagnostics[:0]
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != "vimls/invalid-member-tail" {
			kept = append(kept, diagnostic)
			continue
		}
		if inDef {
			diagnostic.Code = "vim/E488"
			diagnostic.Message = "trailing characters"
			kept = append(kept, diagnostic)
		}
	}
	diagnostics = kept
	if command.Dialect == Vim9 && command.Declaration != nil {
		diagnostics = mapVim9LambdaTrailingParen(diagnostics, command.Declaration.Initializer, file.Source, 0)
		initializer := command.Declaration.Initializer
		if len(diagnostics) == 1 && diagnostics[0].Code == "vimls/missing-expression" && initializer != nil {
			switch initializer.Kind {
			case ExpressionBinary, ExpressionTernary, ExpressionCast:
				diagnostics[0].Code = "vim/E15"
				diagnostics[0].Message = "invalid expression"
				if inDef {
					diagnostics[0].Code = "vim/E1097"
					diagnostics[0].Message = "line incomplete"
				}
			case ExpressionDictionary:
				diagnostics[0].Code = "vim/E15"
				diagnostics[0].Message = "invalid expression"
				if inDef {
					diagnostics[0].Code = "vim/E723"
					diagnostics[0].Message = "Missing end of Dictionary '}'"
				}
			}
		}
	}
	for index := range diagnostics {
		diagnostic := &diagnostics[index]
		switch {
		case command.Dialect == Vim9 && command.Declaration != nil && diagnostic.Code == "vimls/trailing-expression" && vim9InvalidIsSuffix(file.Source, diagnostic.Span.Start):
			diagnostic.Code = "vim/E488"
			diagnostic.Message = "trailing characters"
		case command.Dialect == Vim9 && inDef && !assignmentExpression && commandDictionary && diagnostic.Code == "vim/E722":
			diagnostic.Code = "vim/E723"
			diagnostic.Message = "Missing end of Dictionary '}'"
		case command.Dialect == Vim9 && inDef && hasAssignment && len(diagnostics) == 1 && diagnostic.Code == "vimls/missing-expression":
			diagnostic.Code = "vim/E1097"
			diagnostic.Message = "line incomplete"
		case command.Dialect == Vim9 && inDef && assignmentExpression && diagnostic.Code == "vimls/trailing-expression":
			diagnostic.Code = "vim/E488"
			diagnostic.Message = "trailing characters"
		case command.Dialect == Vim9 && inDef && diagnostic.Code == "vim/E260":
			diagnostic.Code = "vim/E488"
			diagnostic.Message = "trailing characters"
		case command.Dialect == Vim9 && diagnostic.Code == "vimls/missing-call-comma":
			diagnostic.Code = "vim/E116"
			diagnostic.Message = "Invalid arguments for function"
			if inDef {
				diagnostic.Code = "vim/E1123"
				diagnostic.Message = "Missing comma before argument"
			}
		case diagnostic.Code == "vimls/missing-list-end":
			diagnostic.Code = "vim/E696"
			diagnostic.Message = "Missing comma in List"
			if inDef {
				diagnostic.Code = "vim/E697"
				diagnostic.Message = "Missing end of List ']'"
			}
		case command.Dialect == Vim9 && diagnostic.Code == "vimls/missing-member" && diagnostic.Message == "member name cannot follow white space":
			diagnostic.Code = "vim/E15"
			diagnostic.Message = "invalid expression"
			if commandCall {
				diagnostic.Code = "vim/E116"
				diagnostic.Message = "Invalid arguments for function"
			}
			if inDef {
				diagnostic.Code = "vim/E1127"
				diagnostic.Message = "Missing name after dot"
			}
		case diagnostic.Code == "vimls/missing-member" && diagnostic.Message == "expected member name":
			diagnostic.Code = "vim/E15"
			diagnostic.Message = "invalid expression"
			if inDef {
				diagnostic.Code = "vim/E1127"
				diagnostic.Message = "missing name after dot"
			}
		case diagnostic.Code == "vimls/missing-delimiter" && diagnostic.Message == "expected ]":
			diagnostic.Code = "vim/E111"
			diagnostic.Message = "missing ']'"
			if inDef {
				diagnostic.Code = "vim/E1097"
				diagnostic.Message = "line incomplete"
			}
		case diagnostic.Code == "vimls/missing-delimiter" && diagnostic.Message == "expected )":
			diagnostic.Code = "vim/E110"
			diagnostic.Message = "missing ')'"
			if inDef {
				diagnostic.Code = "vim/E1097"
				diagnostic.Message = "line incomplete"
			}
		}
	}
	return diagnostics
}

// A non-delimited is/isnot token is an E488 tail, not a binary operator.
func vim9InvalidIsSuffix(source string, start int) bool {
	if start < 0 || start >= len(source) || source[start] != 'i' {
		return false
	}
	if strings.HasPrefix(source[start:], "isnot") {
		return start+5 < len(source) && !isExpressionSpace(source[start+5])
	}
	return strings.HasPrefix(source[start:], "is") && start+2 < len(source) && !isExpressionSpace(source[start+2])
}

func mapVim9AttachedHashDiagnostics(file *File, command *Command, diagnostics []Diagnostic) []Diagnostic {
	if command.Dialect != Vim9 {
		return diagnostics
	}
	for index := range diagnostics {
		diagnostic := &diagnostics[index]
		if diagnostic.Code == "vimls/trailing-expression" && diagnostic.Span.End == diagnostic.Span.Start+1 &&
			diagnostic.Span.Start >= 0 && diagnostic.Span.End <= len(file.Source) && file.Source[diagnostic.Span.Start] == '#' {
			diagnostic.Code = "vim/E488"
			diagnostic.Message = "trailing characters"
		}
	}
	return diagnostics
}

func takeBoundaryExpression(command *Command) (*Expression, []Diagnostic, bool) {
	boundary := command.boundaryExpression
	command.boundaryExpression = nil
	if boundary == nil || boundary.argument != command.Argument {
		return nil, nil, false
	}
	return boundary.expression, boundary.diagnostics, true
}

func takeValidBoundaryExpression(command *Command, argument Span) (*Expression, []Diagnostic, bool) {
	boundary := command.boundaryExpression
	command.boundaryExpression = nil
	if boundary == nil || boundary.argument != argument || boundary.expression == nil || len(boundary.diagnostics) > 0 {
		return nil, nil, false
	}
	return boundary.expression, boundary.diagnostics, true
}

func takeRecoveringBoundaryExpression(command *Command, argument Span) (*Expression, []Diagnostic, bool) {
	boundary := command.boundaryExpression
	command.boundaryExpression = nil
	if boundary == nil || boundary.argument != argument || boundary.argument.End <= boundary.argument.Start || boundary.expression == nil {
		return nil, nil, false
	}
	return boundary.expression, boundary.diagnostics, true
}

func takeLegacyPutExpressionBoundary(command *Command, argument Span) (*Expression, []Diagnostic, bool) {
	boundary := command.boundaryExpression
	command.boundaryExpression = nil
	if command.Dialect != Legacy || boundary == nil || boundary.expression == nil ||
		boundary.argument.Start != argument.Start || boundary.argument.End > argument.End ||
		boundary.argument.End <= boundary.argument.Start {
		return nil, nil, false
	}
	return boundary.expression, boundary.diagnostics, true
}

func isMappingCommand(name string) bool {
	switch name {
	case "map", "nmap", "vmap", "xmap", "smap", "omap", "imap", "cmap", "lmap", "tmap",
		"noremap", "nnoremap", "vnoremap", "xnoremap", "snoremap", "onoremap", "inoremap", "cnoremap", "lnoremap", "tnoremap",
		"unmap", "nunmap", "vunmap", "xunmap", "sunmap", "ounmap", "iunmap", "cunmap", "lunmap", "tunmap",
		"abbreviate", "iabbrev", "cabbrev", "noreabbrev", "inoreabbrev", "cnoreabbrev", "unabbreviate", "iunabbrev", "cunabbrev",
		"mapclear", "nmapclear", "vmapclear", "xmapclear", "smapclear", "omapclear", "imapclear", "cmapclear", "lmapclear", "tmapclear",
		"abclear", "iabclear", "cabclear":
		return true
	default:
		return false
	}
}

func mappingInfo(name string, bang bool) (MappingKind, MappingMode, bool, bool) {
	abbreviation := false
	clear := false
	kind := MappingDefine
	switch {
	case strings.HasSuffix(name, "clear"):
		clear = true
		kind = MappingClear
	case strings.Contains(name, "nore"):
		kind = MappingNoremap
	case strings.Contains(name, "unmap"), name == "unabbreviate", name == "iunabbrev", name == "cunabbrev":
		kind = MappingUnmap
	}
	for _, value := range []string{"abbreviate", "abbrev", "abclear", "unabbreviate", "unabbrev", "noreabbrev"} {
		if strings.Contains(name, value) {
			abbreviation = true
			break
		}
	}
	if strings.HasSuffix(name, "abbrev") || strings.HasSuffix(name, "abbreviate") {
		abbreviation = true
	}
	if strings.HasSuffix(name, "abclear") {
		abbreviation = true
	}

	var mode MappingMode
	if abbreviation {
		// The unprefixed abbreviation commands (including noreabbrev and
		// unabbreviate) use both insert and command-line modes.  i*/c*
		// variants still select one mode, just like their mapping variants.
		switch name {
		case "abbreviate", "noreabbrev", "unabbreviate", "abclear":
			mode = MappingModeInsertCommandLine
		default:
			switch {
			case strings.HasPrefix(name, "i"):
				mode = MappingModeInsert
			case strings.HasPrefix(name, "c"):
				mode = MappingModeCommandLine
			default:
				mode = MappingModeInsertCommandLine
			}
		}
	} else {
		switch {
		case name == "map" || name == "noremap" || name == "unmap" || name == "mapclear":
			mode = MappingModeNormalVisualSelectOperator
			if bang {
				mode = MappingModeInsertCommandLine
			}
		case strings.HasPrefix(name, "n"):
			mode = MappingModeNormal
		case strings.HasPrefix(name, "v"):
			mode = MappingModeVisual | MappingModeSelect
		case strings.HasPrefix(name, "x"):
			mode = MappingModeVisual
		case strings.HasPrefix(name, "s"):
			mode = MappingModeSelect
		case strings.HasPrefix(name, "o"):
			mode = MappingModeOperator
		case strings.HasPrefix(name, "i"):
			mode = MappingModeInsert
		case strings.HasPrefix(name, "c"):
			mode = MappingModeCommandLine
		case strings.HasPrefix(name, "l"):
			mode = MappingModeLangmap
		case strings.HasPrefix(name, "t"):
			mode = MappingModeTerminal
		}
	}
	return kind, mode, abbreviation, clear
}

func parseMapping(file *File, command *Command) {
	kind, mode, abbreviation, clear := mappingInfo(command.Canonical, command.Bang.Start < command.Bang.End)
	mapping := &Mapping{
		Kind: kind, Mode: mode, Bang: command.Bang.Start < command.Bang.End,
		Abbreviation: abbreviation, Clear: clear,
	}
	argument := command.Argument
	if clear {
		if file.Text(argument) == "<buffer>" {
			mapping.Buffer = true
		}
		command.Mapping = mapping
		return
	}

	position := argument.Start
	for position < argument.End {
		name, size := mappingModifierAt(file.Source, position, argument.End)
		if size == 0 {
			break
		}
		switch name {
		case "buffer":
			mapping.Buffer = true
		case "nowait":
			mapping.Nowait = true
		case "silent":
			mapping.Silent = true
		case "special":
			mapping.Special = true
		case "script":
			mapping.Script = true
		case "expr":
			mapping.Expr = true
		case "unique":
			mapping.Unique = true
		}
		position = skipSpace(file.Source, position+size, argument.End)
	}
	if kind == MappingUnmap {
		mapping.LHS = Span{Start: position, End: argument.End}
		command.Mapping = mapping
		return
	}

	lhsEnd := mappingLHSBoundary(file.Source, position, argument.End)
	if lhsEnd > position {
		mapping.LHS = Span{Start: position, End: lhsEnd}
	}
	rhsStart := skipSpace(file.Source, lhsEnd, argument.End)
	if mapping.LHS.Start == mapping.LHS.End && rhsStart == argument.End {
		mapping.Query = true
	} else if rhsStart < argument.End {
		mapping.RHS = Span{Start: rhsStart, End: argument.End}
		if mapping.Expr {
			var diagnostics []Diagnostic
			mapping.RHSExpression, diagnostics = parseExpression(file.Source[mapping.RHS.Start:mapping.RHS.End], mapping.RHS.Start, command.Dialect)
			file.Diagnostics = append(file.Diagnostics, diagnostics...)
		}
	} else {
		mapping.Query = true
	}
	command.Mapping = mapping
}

func mappingModifierAt(source string, start, end int) (string, int) {
	for _, name := range []string{"buffer", "nowait", "silent", "special", "script", "expr", "unique"} {
		text := "<" + name + ">"
		if start+len(text) <= end && source[start:start+len(text)] == text {
			return name, len(text)
		}
	}
	return "", 0
}

func mappingLHSBoundary(source string, start, end int) int {
	for position := start; position < end; position++ {
		if isSpace(source[position]) {
			return position
		}
		// CTRL-V always quotes the following byte. A backslash only does so
		// when 'cpoptions' omits B; Vim's default includes B, and syntax does
		// not execute preceding :set commands, so use the deterministic default.
		if source[position] == 0x16 {
			if position+1 < end {
				position++
			}
		}
	}
	return end
}

// userCommandBodySpan returns the replacement text in a :command definition.
// The command argument still contains the attributes and user-command name,
// since Ex parses those as part of :command itself.  Listing/query forms such
// as `:command`, `:command Foo`, and `:command -nargs=* Foo` therefore return
// no body.  Attribute values are deliberately treated as opaque words: Vim
// validates them in ex_command(), while syntax only needs their boundary.
func userCommandBodySpan(source string, argument Span) (Span, bool) {
	start := skipSpace(source, argument.Start, argument.End)
	for start < argument.End && source[start] == '-' {
		end := start
		for end < argument.End && !isSpace(source[end]) {
			end++
		}
		start = skipSpace(source, end, argument.End)
	}
	nameEnd := scanWord(source, start, argument.End)
	if nameEnd == start {
		return Span{}, false
	}
	bodyStart := skipSpace(source, nameEnd, argument.End)
	if bodyStart >= argument.End {
		return Span{}, false
	}
	return Span{Start: bodyStart, End: argument.End}, true
}

func diagnoseUserCommandAttributes(file *File, command *Command) {
	if command.Dialect != Vim9 {
		return
	}
	allowArguments := false
	complete := Span{}
	start := skipSpace(file.Source, command.Argument.Start, command.Argument.End)
	for start < command.Argument.End && file.Source[start] == '-' {
		end := start
		for end < command.Argument.End && !isSpace(file.Source[end]) {
			end++
		}
		attribute := file.Source[start:end]
		if strings.HasPrefix(attribute, "-nargs=") {
			switch attribute[len("-nargs="):] {
			case "1", "_", "*", "?", "+":
				allowArguments = true
			case "0":
				allowArguments = false
			}
		} else if strings.HasPrefix(attribute, "-complete=") && len(attribute) > len("-complete=") {
			complete = Span{Start: start, End: end}
		}
		start = skipSpace(file.Source, end, command.Argument.End)
	}
	if complete.Start < complete.End && !allowArguments {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1208", Message: "-complete used without allowing arguments", Span: complete,
		})
	}
}

func collectedCommandStrayClose(source string, collectorEnd int) (Span, bool) {
	_, lineStart := physicalLineEnd(source, collectorEnd)
	if lineStart >= len(source) {
		return Span{}, false
	}
	lineEnd, _ := physicalLineEnd(source, lineStart)
	first := skipSpace(source, lineStart, lineEnd)
	if first >= lineEnd || source[first] != '}' || skipSpace(source, first+1, lineEnd) != lineEnd {
		return Span{}, false
	}
	return Span{Start: first, End: first + 1}, true
}

const maxEmbeddedCommandDepth = 32

func listDoCommand(name string) bool {
	switch name {
	case "argdo", "bufdo", "cdo", "cfdo", "ldo", "lfdo", "tabdo", "windo":
		return true
	default:
		return false
	}
}

func isEmbeddedCommand(name string) bool {
	return listDoCommand(name) || name == "folddoopen" || name == "folddoclosed"
}

var autocmdEventNames = []string{
	"BufAdd", "BufCreate", "BufDelete", "BufEnter", "BufFilePost", "BufFilePre", "BufHidden", "BufLeave", "BufNew", "BufNewFile", "BufRead", "BufReadCmd", "BufReadPost", "BufReadPre", "BufUnload", "BufWinEnter", "BufWinLeave", "BufWipeout", "BufWrite", "BufWriteCmd", "BufWritePost", "BufWritePre",
	"CmdlineChanged", "CmdlineEnter", "CmdlineLeave", "CmdlineLeavePre", "CmdUndefined", "CmdwinEnter", "CmdwinLeave", "ColorScheme", "ColorSchemePre", "CompleteChanged", "CompleteDone", "CompleteDonePre", "CursorHold", "CursorHoldI", "CursorMoved", "CursorMovedC", "CursorMovedI", "DiffUpdated", "DirChanged", "DirChangedPre", "EncodingChanged", "ExitPre",
	"FileAppendCmd", "FileAppendPost", "FileAppendPre", "FileChangedRO", "FileChangedShell", "FileChangedShellPost", "FileEncoding", "FileReadCmd", "FileReadPost", "FileReadPre", "FileType", "FileWriteCmd", "FileWritePost", "FileWritePre", "FilterReadPost", "FilterReadPre", "FilterWritePost", "FilterWritePre", "FocusGained", "FocusLost", "FuncUndefined", "GUIEnter", "GUIFailed",
	"InsertChange", "InsertCharPre", "InsertEnter", "InsertLeave", "InsertLeavePre", "KeyInputPre", "MenuPopup", "ModeChanged", "OptionSet", "QuickFixCmdPost", "QuickFixCmdPre", "QuitPre", "RemoteReply", "SafeState", "SafeStateAgain", "SessionLoadPost", "SessionLoadPre", "SessionWritePost", "ShellCmdPost", "ShellFilterPost", "SigUSR1", "SourceCmd", "SourcePost", "SourcePre",
	"SpellFileMissing", "StdinReadPost", "StdinReadPre", "SwapExists", "Syntax", "TabClosed", "TabClosedPre", "TabEnter", "TabLeave", "TabNew", "TermChanged", "TerminalOpen", "TerminalWinOpen", "TermResponse", "TermResponseAll", "TextChanged", "TextChangedI", "TextChangedP", "TextChangedT", "TextPutPost", "TextPutPre", "TextYankPost", "User", "VimEnter", "VimLeave", "VimLeavePre", "VimResized", "VimResume", "VimSuspend", "WinClosed", "WinEnter", "WinLeave", "WinNew", "WinNewPre", "WinResized", "WinScrolled",
}

func isAutocmdEventToken(token string) bool {
	if token == "*" {
		return true
	}
	for _, event := range strings.Split(token, ",") {
		if event == "" {
			return false
		}
		found := false
		for _, name := range autocmdEventNames {
			if strings.EqualFold(event, name) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func scanAutocmdWord(source string, start, end int) int {
	for index := start; index < end; index++ {
		if source[index] == '\\' && index+1 < end {
			index++
			continue
		}
		if isSpace(source[index]) {
			return index
		}
	}
	return end
}

// scanAutocmdCommandArgument gives a bar to the outer Ex scanner only when
// autocmd has no pattern. Once a pattern has started, autocmd.c owns every
// subsequent bar as command-body text (including a bar adjacent to pattern
// bytes).
func scanAutocmdCommandArgument(source string, start, end int) (int, Span) {
	position := skipSpace(source, start, end)
	if position >= end {
		return end, Span{}
	}
	if source[position] == '|' {
		return trimSpaceEnd(source, start, position), Span{Start: position, End: position + 1}
	}
	firstEnd := scanAutocmdWord(source, position, end)
	if firstEnd == position {
		return end, Span{}
	}
	eventEnd := firstEnd
	if !isAutocmdEventToken(source[position:firstEnd]) {
		candidate := skipSpace(source, firstEnd, end)
		candidateEnd := scanAutocmdWord(source, candidate, end)
		if candidate < candidateEnd && isAutocmdEventToken(source[candidate:candidateEnd]) {
			eventEnd = candidateEnd
		}
	}
	patternStart := skipSpace(source, eventEnd, end)
	if patternStart < end && source[patternStart] == '|' {
		return trimSpaceEnd(source, start, patternStart), Span{Start: patternStart, End: patternStart + 1}
	}
	return end, Span{}
}

func parseAutocmdHeader(source string, argument Span, dialect Dialect, bang bool) (*AutocmdCommand, Span, bool, bool) {
	header := &AutocmdCommand{}
	start := skipSpace(source, argument.Start, argument.End)
	if start >= argument.End || source[start] == '|' {
		if bang {
			header.Operation = AutocmdClear
		}
		return header, Span{}, false, false
	}
	firstEnd := scanAutocmdWord(source, start, argument.End)
	if firstEnd == start {
		return header, Span{}, false, false
	}
	header.Head = Span{Start: start, End: firstEnd}
	eventStart := start
	if !isAutocmdEventToken(source[start:firstEnd]) {
		candidate := skipSpace(source, firstEnd, argument.End)
		candidateEnd := scanAutocmdWord(source, candidate, argument.End)
		// A known second word is the only static evidence that the first word
		// is a group. Unknown groups and future events remain ambiguous and are
		// retained as the event head rather than being diagnosed here.
		if candidate < candidateEnd && isAutocmdEventToken(source[candidate:candidateEnd]) {
			header.Group = Span{Start: start, End: firstEnd}
			eventStart = candidate
		} else {
			// A user-defined event and an existing augroup have identical
			// spelling at this boundary. Without the mutable augroup table,
			// retaining a guessed pattern/body would create false structure.
			// Keep the head and the first event token, and leave the remainder
			// opaque for a later resolver.
			header.Events = []Span{{Start: start, End: firstEnd}}
			if bang {
				header.Operation = AutocmdClear
			}
			return header, Span{}, false, false
		}
	}
	eventEnd := scanAutocmdWord(source, eventStart, argument.End)
	for itemStart := eventStart; itemStart < eventEnd; {
		itemEnd := itemStart
		for itemEnd < eventEnd && source[itemEnd] != ',' {
			itemEnd++
		}
		if itemEnd > itemStart {
			header.Events = append(header.Events, Span{Start: itemStart, End: itemEnd})
		}
		itemStart = itemEnd + 1
	}
	patternStart := skipSpace(source, eventEnd, argument.End)
	if patternStart >= argument.End || source[patternStart] == '|' {
		if bang {
			header.Operation = AutocmdClear
		}
		return header, Span{}, false, false
	}
	patternEnd := scanAutocmdWord(source, patternStart, argument.End)
	header.Pattern = Span{Start: patternStart, End: patternEnd}
	bodyStart := skipSpace(source, patternEnd, argument.End)
	for bodyStart < argument.End {
		modifierEnd := scanAutocmdWord(source, bodyStart, argument.End)
		modifier := source[bodyStart:modifierEnd]
		kind := AutocmdOnce
		if modifierEnd >= argument.End || !isSpace(source[modifierEnd]) {
			break
		}
		if modifier == "++nested" || modifier == "nested" && dialect == Legacy {
			kind = AutocmdNested
		} else if modifier != "++once" {
			if modifier == "nested" && dialect == Vim9 {
				// Vim9 accepts only ++nested. Keep the word in the header so the
				// following body remains recoverable and report the source error.
				header.Modifiers = append(header.Modifiers, AutocmdModifier{Kind: AutocmdNested, Span: Span{Start: bodyStart, End: modifierEnd}})
				bodyStart = skipSpace(source, modifierEnd, argument.End)
				continue
			}
			break
		}
		header.Modifiers = append(header.Modifiers, AutocmdModifier{Kind: kind, Span: Span{Start: bodyStart, End: modifierEnd}})
		bodyStart = skipSpace(source, modifierEnd, argument.End)
	}
	if bodyStart >= argument.End {
		if bang {
			header.Operation = AutocmdClear
		} else {
			header.Operation = AutocmdQuery
		}
		return header, Span{}, false, false
	}
	if source[bodyStart] == '|' {
		if bang {
			header.Operation = AutocmdReplace
		} else {
			header.Operation = AutocmdDefine
		}
		bodyStart = skipSpace(source, bodyStart+1, argument.End)
		return header, Span{Start: bodyStart, End: argument.End}, true, false
	}
	if source[bodyStart] == '{' && autocmdBlockLineOnly(source, bodyStart, argument.End, dialect) {
		if bang {
			header.Operation = AutocmdReplace
		} else {
			header.Operation = AutocmdDefine
		}
		bodyStart = autocmdBlockBodyStart(source, bodyStart, argument.End, dialect)
		if closeStart, _, found := autocmdBlockClose(source, bodyStart, argument.End); found {
			return header, Span{Start: bodyStart, End: closeStart}, true, true
		}
		return header, Span{Start: bodyStart, End: bodyStart}, true, true
	}
	if bang {
		header.Operation = AutocmdReplace
	} else {
		header.Operation = AutocmdDefine
	}
	return header, Span{Start: bodyStart, End: argument.End}, true, false
}

func scanAutocmdHeader(source string, argument Span, dialect Dialect) (*AutocmdCommand, int, bool, bool) {
	header, body, ok, block := parseAutocmdHeader(source, argument, dialect, false)
	return header, body.Start, ok, block
}

func autocmdBlockLineOnly(source string, open, end int, dialect Dialect) bool {
	lineEnd, _ := physicalLineEnd(source, open)
	if lineEnd > end {
		lineEnd = end
	}
	rest := skipSpace(source, open+1, lineEnd)
	if rest >= lineEnd || source[rest] == '|' {
		return true
	}
	if dialect == Legacy {
		return source[rest] == '"'
	}
	if source[rest] != '#' || rest == open+1 || !isSpace(source[rest-1]) {
		return false
	}
	return rest+1 >= lineEnd || source[rest+1] != '{' || rest+2 < lineEnd && source[rest+2] == '{'
}

func autocmdBlockBodyStart(source string, open, end int, dialect Dialect) int {
	lineEnd, next := physicalLineEnd(source, open)
	if lineEnd > end {
		lineEnd = end
		next = end
	}
	rest := skipSpace(source, open+1, lineEnd)
	if rest >= lineEnd || dialect == Legacy && source[rest] == '"' || dialect == Vim9 && source[rest] == '#' {
		return next
	}
	// A bar is an Ex terminator accepted by ends_excmd2(). Preserve the
	// following text as same-line block content.
	if source[rest] == '|' {
		return skipSpace(source, rest+1, lineEnd)
	}
	return open + 1
}

func parseEmbeddedCommandList(file *File, span Span, dialect Dialect, depth int) *CommandList {
	list := &CommandList{Span: span}
	if depth >= maxEmbeddedCommandDepth {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vimls/embedded-command-depth", Message: "embedded command nesting exceeds parser limit", Span: span,
		})
		return list
	}
	embedded := &File{Dialect: dialect, Source: file.Source}
	scanCommands(embedded, span.Start, span.End, dialect)
	coalesceCollectedCommandBlocks(embedded, span.End)
	trimEmbeddedCommandSpans(embedded)
	buildBlocks(embedded)
	for index := range embedded.Commands {
		if embedded.Commands[index].Heredoc == nil {
			diagnosticsStart := len(embedded.Diagnostics)
			parseCommandDetailsDepth(embedded, &embedded.Commands[index], depth+1)
			// A user-command replacement is a template, not the final Vim
			// expression.  Placeholders such as <f-args> are intentionally not
			// valid expression tokens until the command is invoked.  Keep the
			// command structure but do not report expression diagnostics caused
			// solely by that template syntax.
			if hasUserCommandReplacement(embedded.Source, embedded.Commands[index].Argument) {
				embedded.Diagnostics = embedded.Diagnostics[:diagnosticsStart]
			}
		}
	}
	buildAggregateMembers(embedded)
	file.Diagnostics = append(file.Diagnostics, embedded.Diagnostics...)
	list.Commands = embedded.Commands
	list.Blocks = embedded.Blocks
	return list
}

// parseVim9AutocmdBlockCommandList uses the regular Vim9 source reader for the
// collected block body, including automatic continuation and logical views.
func parseVim9AutocmdBlockCommandList(file *File, span Span, depth int) *CommandList {
	list := &CommandList{Span: span}
	if depth >= maxEmbeddedCommandDepth {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vimls/embedded-command-depth", Message: "embedded command nesting exceeds parser limit", Span: span,
		})
		return list
	}
	// Parse a source slice with the regular Vim9 reader so automatic
	// continuation, logical views, and nested autocmd owners stay identical to
	// top-level Vim9 parsing. Rebase the complete result back to the containing
	// file after parsing.
	embedded := parseSource(file.Source[span.Start:span.End], Vim9)
	rebaseLambdaFile(embedded, file.Source, span.Start)
	file.Diagnostics = append(file.Diagnostics, embedded.Diagnostics...)
	list.Commands = embedded.Commands
	list.Blocks = embedded.Blocks
	return list
}

// parseLegacyDoCommandList keeps the physical command boundaries that
// do_cmdline() uses for a multi-line block passed to :windo and its siblings.
// A plain :windo echo only has one line and continues through the regular
// embedded parser, so a following source line is never consumed accidentally.
func parseLegacyDoCommandList(file *File, span Span, depth int) *CommandList {
	list := &CommandList{Span: span}
	if depth >= maxEmbeddedCommandDepth {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vimls/embedded-command-depth", Message: "embedded command nesting exceeds parser limit", Span: span,
		})
		return list
	}
	embedded := &File{Dialect: Legacy, Source: file.Source}
	lineStart := span.Start
	for lineStart < span.End {
		lineEnd, next := physicalLineEnd(file.Source, lineStart)
		if lineEnd > span.End {
			lineEnd = span.End
			next = span.End
		}
		if lineStart < lineEnd {
			scanCommands(embedded, lineStart, lineEnd, Legacy)
		}
		if next <= lineStart || next >= span.End {
			break
		}
		lineStart = next
	}
	trimEmbeddedCommandSpans(embedded)
	buildBlocks(embedded)
	for index := range embedded.Commands {
		if embedded.Commands[index].Heredoc == nil {
			parseCommandDetailsDepth(embedded, &embedded.Commands[index], depth+1)
		}
	}
	buildAggregateMembers(embedded)
	file.Diagnostics = append(file.Diagnostics, embedded.Diagnostics...)
	list.Commands = embedded.Commands
	list.Blocks = embedded.Blocks
	return list
}

func hasUserCommandReplacement(source string, span Span) bool {
	for index := span.Start; index < span.End; index++ {
		if source[index] != '<' {
			continue
		}
		end := index + 1
		for end < span.End && source[end] != '>' {
			end++
		}
		if end >= span.End {
			continue
		}
		name := source[index+1 : end]
		// Vim's uc_check_code() accepts q/Q/f/F as an optional prefix,
		// then matches the replacement name case-insensitively.  Keep the
		// accepted names finite; arbitrary angle-bracket text is not a user
		// command replacement.
		if len(name) >= 2 && name[1] == '-' && strings.ContainsRune("qQfF", rune(name[0])) {
			name = name[2:]
		}
		switch strings.ToLower(name) {
		case "line1", "line2", "range", "count", "bang", "mods", "reg", "register", "args", "lt":
			return true
		}
		index = end
	}
	return false
}

// parseLegacyAutocmdCommandList handles the legacy source-file continuation
// form used by runtime/filetype.vim and plugins.  In an autocmd body, a line
// beginning with "\\" is part of the same Ex command; "\\|" keeps the bar as
// a command separator.  Parse a normalized body slice and rebase the result
// so all resulting spans continue to point into the original File.Source.
func parseLegacyAutocmdCommandList(file *File, span Span, depth int) *CommandList {
	list := &CommandList{Span: span}
	if depth >= maxEmbeddedCommandDepth {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vimls/embedded-command-depth", Message: "embedded command nesting exceeds parser limit", Span: span,
		})
		return list
	}
	// A legacy autocmd body may itself be an autocmd block. Keep newlines for
	// find_cmd_block_start() in this case; the normal continuation view below
	// intentionally replaces newlines with spaces and cannot identify the
	// line-leading close of that one owning block.
	raw := &File{Dialect: Legacy, Source: file.Source}
	scanCommands(raw, span.Start, span.End, Legacy)
	if legacyAutocmdHasBlock(raw, span.Start, span.End) {
		coalesceCollectedCommandBlocks(raw, span.End)
		trimEmbeddedCommandSpans(raw)
		buildBlocks(raw)
		for index := range raw.Commands {
			if raw.Commands[index].Heredoc == nil {
				parseCommandDetailsDepth(raw, &raw.Commands[index], depth+1)
			}
		}
		buildAggregateMembers(raw)
		file.Diagnostics = append(file.Diagnostics, raw.Diagnostics...)
		list.Commands = raw.Commands
		list.Blocks = raw.Blocks
		return list
	}
	// Only the autocmd body needs normalization.  Keeping this view relative
	// to span avoids copying the complete source file, while the parser result
	// is rebased below so all public spans remain absolute.
	view := []byte(file.Source[span.Start:span.End])
	for lineStart := 0; lineStart < len(view); {
		lineEnd := lineStart
		for lineEnd < len(view) && view[lineEnd] != '\n' {
			lineEnd++
		}
		prefix := lineStart
		for prefix < lineEnd && (view[prefix] == ' ' || view[prefix] == '\t' || view[prefix] == '\r') {
			prefix++
		}
		if prefix < lineEnd && view[prefix] == '\\' {
			for index := lineStart; index <= prefix; index++ {
				view[index] = ' '
			}
		}
		if lineEnd < len(view) && view[lineEnd] == '\n' {
			view[lineEnd] = ' '
			lineStart = lineEnd + 1
		} else {
			lineStart = len(view)
		}
	}
	// A leading `\|` is the continuation spelling of the separator, but
	// there is no preceding command inside this embedded list to separate
	// from.  Treat it as the start of the first body command.  Separators on
	// subsequent lines remain intact and are handled by scanCommands.
	first := 0
	for first < len(view) && isSpace(view[first]) {
		first++
	}
	if first < len(view) && view[first] == '|' {
		view[first] = ' '
	}
	embedded := &File{Dialect: Legacy, Source: string(view)}
	scanCommands(embedded, 0, len(view), Legacy)
	coalesceCollectedCommandBlocks(embedded, len(embedded.Source))
	trimEmbeddedCommandSpans(embedded)
	buildBlocks(embedded)
	for index := range embedded.Commands {
		if embedded.Commands[index].Heredoc == nil {
			parseCommandDetailsDepth(embedded, &embedded.Commands[index], depth+1)
		}
	}
	buildAggregateMembers(embedded)
	rebaseLambdaFile(embedded, file.Source, span.Start)
	file.Diagnostics = append(file.Diagnostics, embedded.Diagnostics...)
	list.Commands = embedded.Commands
	list.Blocks = embedded.Blocks
	return list
}

func legacyAutocmdHasBlock(file *File, start, end int) bool {
	start = skipSpace(file.Source, start, end)
	if len(file.Commands) == 0 || file.Commands[0].Span.Start != start {
		return false
	}
	command := &file.Commands[0]
	switch command.Canonical {
	case "autocmd":
		_, _, ok := autocmdBlockStart(file.Source, command.Argument, command.Dialect, end)
		return ok
	case "command":
		body, ok := userCommandBodySpan(file.Source, command.Argument)
		if !ok {
			return false
		}
		if _, ok := commandBlockOpen(file.Source, body, command.Dialect); ok {
			return true
		}
		_, ok = nestedCommandBlockOpen(file.Source, body, command.Dialect, end)
		return ok
	}
	return false
}

func trimEmbeddedCommandSpans(file *File) {
	for index := range file.Commands {
		command := &file.Commands[index]
		command.Span.End = trimEmbeddedCommandEnd(file.Source, command.Span.Start, command.Span.End)
		command.Argument.End = trimEmbeddedCommandEnd(file.Source, command.Argument.Start, command.Argument.End)
	}
}

func trimEmbeddedCommandEnd(source string, start, end int) int {
	for end > start && (source[end-1] == ' ' || source[end-1] == '\t' || source[end-1] == '\r' || source[end-1] == '\n') {
		end--
	}
	return end
}

func parseVariableTargets(file *File, command *Command) {
	source := file.Text(command.Argument)
	consumed := skipSpace(source, 0, len(source))
	if command.Canonical == "lockvar" || command.Canonical == "unlockvar" {
		end := consumed
		for end < len(source) && source[end] >= '0' && source[end] <= '9' {
			end++
		}
		if end > consumed && end < len(source) && isSpace(source[end]) {
			command.Count = Span{Start: command.Argument.Start + consumed, End: command.Argument.Start + end}
			consumed = skipSpace(source, end, len(source))
		}
	}
	for consumed < len(source) {
		target, diagnostics, length := parseExpressionPrefix(source[consumed:], command.Argument.Start+consumed, command.Dialect)
		command.Targets = append(command.Targets, target)
		file.Diagnostics = append(file.Diagnostics, diagnostics...)
		if len(diagnostics) > 0 || length <= 0 {
			break
		}
		consumed += length
		consumed = skipSpace(source, consumed, len(source))
	}
}

func diagnoseInvalidClassDeclaration(file *File, command *Command, declaration *Declaration) bool {
	if command.Canonical != "var" || declaration == nil || declaration.Name.Start < declaration.Name.End || command.Block < 0 || command.Block >= len(file.Blocks) || file.Blocks[command.Block].Kind != BlockClass {
		return false
	}
	code := "vim/E1317"
	message := "invalid object variable declaration"
	start := command.Name.Start
	for _, modifier := range command.Modifiers {
		if modifier.Name == "static" {
			code = "vim/E1329"
			message = "invalid class variable declaration"
			start = modifier.Span.Start
			break
		}
	}
	file.Diagnostics = append(file.Diagnostics, Diagnostic{
		Code: code, Message: message,
		Span: Span{Start: start, End: command.Argument.End},
	})
	return true
}

func diagnoseClassMemberModifierOrder(file *File, command *Command) {
	if command == nil || command.Dialect != Vim9 || command.Block < 0 || command.Block >= len(file.Blocks) || file.Blocks[command.Block].Kind != BlockClass || len(command.Modifiers) == 0 {
		return
	}
	modifier := command.Modifiers[0]
	next := command.Canonical
	if len(command.Modifiers) > 1 {
		next = command.Modifiers[1].Name
	}
	if modifier.Name == "public" {
		valid := next == "var" || next == "static" || next == "final" || next == "const"
		if !valid {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1331", Message: `public must be followed by "var" or "static" or "final" or "const"`, Span: modifier.Span,
			})
		}
		return
	}
	if modifier.Name != "static" {
		return
	}
	valid := next == "var" || next == "def" || next == "final" || next == "const"
	if !valid {
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E1368", Message: `Static must be followed by "var" or "def" or "final" or "const"`, Span: modifier.Span,
		})
	}
}

func diagnoseInvalidInterfaceDeclaration(file *File, command *Command, declaration *Declaration) {
	if command == nil || command.Dialect != Vim9 || command.Canonical != "var" || declaration == nil || declaration.Assignment.Start >= declaration.Assignment.End || command.Block < 0 || command.Block >= len(file.Blocks) || file.Blocks[command.Block].Kind != BlockInterface {
		return
	}
	file.Diagnostics = append(file.Diagnostics, Diagnostic{
		Code: "vim/E1344", Message: "Cannot initialize a variable in an interface",
		Span: Span{Start: command.Name.Start, End: command.Argument.End},
	})
}

func diagnoseEnumEndTrailingCharacters(file *File, command *Command) {
	if command == nil || command.Dialect != Vim9 || command.Canonical != "endenum" || command.Block < 0 || command.Block >= len(file.Blocks) || file.Blocks[command.Block].Kind != BlockEnum {
		return
	}
	start := skipSpace(file.Source, command.Argument.Start, command.Argument.End)
	end := trimSpaceEnd(file.Source, start, command.Argument.End)
	if start >= end {
		return
	}
	file.Diagnostics = append(file.Diagnostics, Diagnostic{
		Code: "vim/E488", Message: "trailing characters", Span: Span{Start: start, End: end},
	})
}

func diagnoseEnumAbstractMember(file *File, command *Command) {
	if command == nil || command.Dialect != Vim9 || command.Block < 0 || command.Block >= len(file.Blocks) || file.Blocks[command.Block].Kind != BlockEnum {
		return
	}
	for _, modifier := range command.Modifiers {
		if modifier.Name == "abstract" {
			file.Diagnostics = append(file.Diagnostics, Diagnostic{
				Code: "vim/E1417", Message: "Abstract cannot be used in an Enum", Span: modifier.Span,
			})
			return
		}
	}
}

func diagnoseVim9AssignmentSpacing(file *File, command *Command, assignment Span) {
	if command == nil || command.Dialect != Vim9 || assignment.Start < command.Argument.Start || assignment.End > command.Argument.End {
		return
	}
	spaceBefore := assignment.Start > command.Argument.Start && isExpressionSpace(file.Source[assignment.Start-1])
	spaceAfter := assignment.End >= command.Argument.End || isExpressionSpace(file.Source[assignment.End])
	if spaceBefore && spaceAfter {
		return
	}
	file.Diagnostics = append(file.Diagnostics, Diagnostic{
		Code: "vim/E1004", Message: "white space required before and after assignment operator", Span: assignment,
	})
}

func parseDeclarationTarget(file *File, command *Command, declaration *Declaration, source string, diagnostics []Diagnostic) (*Expression, []Diagnostic) {
	if declaration.Name.Start < declaration.Name.End && file.Source[declaration.Name.Start] == '[' {
		target := &Expression{Kind: ExpressionList, Span: declaration.Name}
		for _, binding := range declaration.Bindings {
			target.Children = append(target.Children, &Expression{Kind: ExpressionIdentifier, Span: binding.Name, Value: file.Text(binding.Name)})
		}
		return target, diagnostics
	}
	if command.Canonical == "let" {
		start := skipSpace(source, 0, len(source))
		end := trimSpaceEnd(source, start, len(source))
		target, targetDiagnostics := parseExpression(source[start:end], command.Argument.Start+start, command.Dialect)
		return target, append(diagnostics, targetDiagnostics...)
	}
	return &Expression{Kind: ExpressionIdentifier, Span: declaration.Name, Value: file.Text(declaration.Name)}, diagnostics
}

func parseDeclarationHead(file *File, source string, base int, dialect Dialect) *Declaration {
	if dialect == Vim9 {
		source = maskVim9Comments(source)
	}
	name, typeSpan := declarationSpans(source, base)
	declaration := &Declaration{Name: name, Type: typeSpan}
	trimmedStart := skipSpace(source, 0, len(source))
	if trimmedStart < len(source) && source[trimmedStart] == '[' {
		if close := findMatching(source, trimmedStart, '[', ']'); close >= 0 {
			declaration.Name = Span{Start: base + trimmedStart, End: base + close + 1}
			declaration.Bindings = parseBindings(file, source, base, trimmedStart+1, close)
		}
	} else if name.Start < name.End {
		binding := Binding{Name: name, Type: typeSpan}
		if typeSpan.Start < typeSpan.End {
			binding.ParsedType, file.Diagnostics = appendTypeDiagnostics(file.Diagnostics, file.Source[typeSpan.Start:typeSpan.End], typeSpan.Start)
			declaration.ParsedType = binding.ParsedType
		}
		declaration.Bindings = append(declaration.Bindings, binding)
	} else if typeSpan.Start < typeSpan.End {
		declaration.ParsedType, file.Diagnostics = appendTypeDiagnostics(file.Diagnostics, file.Source[typeSpan.Start:typeSpan.End], typeSpan.Start)
	}
	return declaration
}

func parseBindings(file *File, source string, base, start, end int) []Binding {
	var bindings []Binding
	partStart := start
	rest := false
	depth := 0
	quote := byte(0)
	for index := start; index <= end; index++ {
		separator := index == end
		if index < end {
			character := source[index]
			if quote != 0 {
				if character == '\\' && quote == '"' {
					index++
				} else if character == quote {
					quote = 0
				}
				continue
			}
			if character == '\'' || character == '"' {
				quote = character
				continue
			}
			switch character {
			case '(', '[', '{', '<':
				depth++
			case ')', ']', '}', '>':
				if depth > 0 {
					depth--
				}
			default:
				separator = depth == 0 && (character == ',' || character == ';')
			}
		}
		if !separator {
			continue
		}
		segmentStart := skipSpace(source, partStart, index)
		segmentEnd := trimSpaceEnd(source, segmentStart, index)
		if segmentStart < segmentEnd {
			name, typeSpan := declarationSpans(source[segmentStart:segmentEnd], base+segmentStart)
			binding := Binding{Name: name, Type: typeSpan, Rest: rest}
			if typeSpan.Start < typeSpan.End {
				binding.ParsedType, file.Diagnostics = appendTypeDiagnostics(file.Diagnostics, file.Source[typeSpan.Start:typeSpan.End], typeSpan.Start)
			}
			bindings = append(bindings, binding)
		}
		if index < end && source[index] == ';' {
			rest = true
		}
		partStart = index + 1
	}
	return bindings
}

func parseForLoop(file *File, command *Command) {
	source := file.Text(command.Argument)
	in := findTopLevelKeyword(source, 0, len(source), "in")
	if in < 0 {
		command.For = &ForLoop{IterableSpan: Span{Start: command.Argument.End, End: command.Argument.End}}
		file.Diagnostics = append(file.Diagnostics, Diagnostic{
			Code: "vim/E690", Message: `Missing "in" after :for`, Span: command.Name,
		})
		return
	}
	leftEnd := trimExpressionSpaceEnd(source, 0, in)
	rightStart := skipExpressionSpace(source, in+2)
	iterableSpan := Span{Start: command.Argument.Start + rightStart, End: command.Argument.End}
	loop := &ForLoop{IterableSpan: iterableSpan}
	leftStart := skipExpressionSpace(source, 0)
	if leftStart < leftEnd && source[leftStart] == '[' {
		if close := findMatching(source, leftStart, '[', ']'); close >= 0 {
			loop.Bindings = parseBindings(file, source, command.Argument.Start, leftStart+1, close)
		}
	} else {
		name, typeSpan := declarationSpans(source[leftStart:leftEnd], command.Argument.Start+leftStart)
		binding := Binding{Name: name, Type: typeSpan}
		if typeSpan.Start < typeSpan.End {
			binding.ParsedType, file.Diagnostics = appendTypeDiagnostics(file.Diagnostics, file.Source[typeSpan.Start:typeSpan.End], typeSpan.Start)
		}
		loop.Bindings = append(loop.Bindings, binding)
	}
	var iterableDiagnostics []Diagnostic
	loop.Iterable, iterableDiagnostics, _ = takeValidBoundaryExpression(command, iterableSpan)
	if loop.Iterable == nil {
		loop.Iterable, iterableDiagnostics = appendExpressionDiagnostics(nil, source[rightStart:], command.Argument.Start+rightStart, command.Dialect)
	}
	file.Diagnostics = append(file.Diagnostics, iterableDiagnostics...)
	command.For = loop
	command.Expressions = append(command.Expressions, loop.Iterable)
}

func appendTypeDiagnostics(diagnostics []Diagnostic, source string, base int) (*Type, []Diagnostic) {
	typeNode, typeDiagnostics := parseTypeAt(source, base)
	return typeNode, append(diagnostics, typeDiagnostics...)
}

func findAssignment(source string) Span {
	quote := byte(0)
	depth := 0
	for index := 0; index < len(source); index++ {
		character := source[index]
		if quote != 0 {
			if character == '\\' && quote == '"' {
				index++
			} else if character == quote {
				quote = 0
			}
			continue
		}
		if character == '@' && index+1 < len(source) {
			_, size := utf8.DecodeRuneInString(source[index+1:])
			index += size
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			continue
		}
		switch character {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case '=':
			if depth != 0 || index+1 < len(source) && (source[index+1] == '=' || source[index+1] == '~' || source[index+1] == '>') || index > 0 && strings.ContainsRune("=!<>~", rune(source[index-1])) {
				continue
			}
			start := index
			if index > 0 && strings.ContainsRune("+-*/%.", rune(source[index-1])) {
				start--
				if start > 0 && source[start] == '.' && source[start-1] == '.' {
					start--
				}
			}
			return Span{Start: start, End: index + 1}
		}
	}
	return Span{Start: -1, End: -1}
}

func declarationSpans(source string, base int) (Span, Span) {
	start := skipSpace(source, 0, len(source))
	end := start
	for end < len(source) {
		r, size := utf8.DecodeRuneInString(source[end:])
		if r == ':' {
			isScope := end == start+1 && strings.ContainsRune("abglstvw", rune(source[start]))
			if isScope && end+size < len(source) {
				next, _ := utf8.DecodeRuneInString(source[end+size:])
				isScope = !unicode.IsSpace(next)
			}
			if !isScope {
				break
			}
		} else if r != '_' && r != '#' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		end += size
	}
	name := Span{Start: base + start, End: base + end}
	typeSpan := Span{}
	typeStart := skipSpace(source, end, len(source))
	if typeStart < len(source) && source[typeStart] == ':' {
		typeStart = skipSpace(source, typeStart+1, len(source))
		typeEnd := trimSyntaxSpaceEnd(source, typeStart, len(source))
		typeSpan = Span{Start: base + typeStart, End: base + typeEnd}
	}
	return name, typeSpan
}

func looksLikeVim9Expression(source string, nameStart, nameEnd, end int) bool {
	if nameEnd >= end {
		return false
	}
	if strings.ContainsRune(":([.", rune(source[nameEnd])) {
		return true
	}
	position := skipSpace(source, nameEnd, end)
	if position >= end {
		return false
	}
	if strings.HasPrefix(source[position:end], "..=") {
		return true
	}
	if strings.HasPrefix(source[position:end], "->") {
		return true
	}
	return source[position] == '=' || position+1 < end && strings.ContainsRune("+-*/%.", rune(source[position])) && source[position+1] == '='
}

// spacedVim9Call reports the command-start form that Vim9 rejects as a
// function call: the opening parenthesis is separated from the name by
// whitespace.  Keeping this separate from looksLikeVim9Expression is
// intentional: a capitalized legacy/user command with spaced arguments must
// remain an opaque command.
func spacedVim9Call(source string, nameEnd, end int) bool {
	return vim9CallOpen(source, nameEnd, end) >= 0
}

func vim9CallOpen(source string, nameEnd, end int) int {
	if nameEnd >= end || !isSpace(source[nameEnd]) {
		return -1
	}
	open := skipSpace(source, nameEnd, end)
	if open < end && source[open] == '(' {
		return open
	}
	return -1
}

func spacedVim9CallInArgument(source string, start, end int) (int, int, bool) {
	nameStart := skipSpace(source, start, end)
	nameEnd := scanWord(source, nameStart, end)
	if nameEnd == nameStart || nameEnd >= end || !isSpace(source[nameEnd]) {
		return 0, 0, false
	}
	open := skipSpace(source, nameEnd, end)
	if open >= end || source[open] != '(' {
		return 0, 0, false
	}
	return nameEnd - start, open - start, true
}

func looksLikeImmediateVim9Expression(source string, nameEnd, end int) bool {
	return nameEnd < end && strings.ContainsRune(":([.", rune(source[nameEnd]))
}

// A one-byte :s is ambiguous with a Vim9 variable named s. Vim resolves the
// syntactically complete call, index, member, scope and method-call shapes as
// expressions before it falls back to :substitute. An isolated '.', '-' or a
// separated delimiter still belongs to :substitute and is diagnosed there.
func looksLikeSubstituteVim9Expression(source, typedName string, nameEnd, end int) bool {
	if nameEnd >= end {
		return false
	}
	switch source[nameEnd] {
	case ':':
		return typedName == "s"
	case '(', '[':
		return true
	case '.':
		return nameEnd+1 < end && (isExpressionLetter(source[nameEnd+1]) || source[nameEnd+1] == '_')
	}
	position := skipSpace(source, nameEnd, end)
	if position+1 < end && source[position] == '-' && source[position+1] == '>' {
		return true
	}
	return looksLikeVim9AssignmentAfterName(source, nameEnd, end) || looksLikeScopedVim9Assignment(source, nameEnd, end)
}

func looksLikeVim9AssignmentAfterName(source string, nameEnd, end int) bool {
	position := skipSpace(source, nameEnd, end)
	if position >= end {
		return false
	}
	if strings.HasPrefix(source[position:end], "..=") {
		return true
	}
	if source[position] == '=' {
		return position+1 >= end || source[position+1] != '>' && source[position+1] != '=' && source[position+1] != '~'
	}
	return position+1 < end && strings.ContainsRune("+-*/%.", rune(source[position])) && source[position+1] == '='
}

func looksLikeScopedVim9Assignment(source string, nameEnd, end int) bool {
	position := nameEnd
	if position >= end || source[position] != ':' && source[position] != '.' {
		return false
	}
	position = scanWord(source, position+1, end)
	return looksLikeVim9AssignmentAfterName(source, position, end)
}

// looksLikeVim9SigilExpression recognizes a complete sigil expression before
// command lookup.  In a Vim9 function, a bare option, environment variable,
// or register is an expression command even when it has no assignment.  Keep
// the token boundary strict so an incomplete sigil remains recoverable as an
// opaque command instead of swallowing the following line.
func looksLikeVim9SigilExpression(source string, start, end int) bool {
	nameEnd, ok := scanVim9Sigil(source, start, end)
	if !ok {
		return false
	}
	position := skipSpace(source, nameEnd, end)
	if position >= end || source[position] == '#' && (position == start || isSpace(source[position-1])) {
		return true
	}
	return looksLikeVim9AssignmentAfterName(source, nameEnd, end) || looksLikeVim9Expression(source, start, nameEnd, end)
}

func scanVim9Sigil(source string, start, end int) (int, bool) {
	if start >= end {
		return start, false
	}
	nameEnd := start + 1
	nameStart := nameEnd
	switch source[start] {
	case '@':
		if nameEnd >= end || isSpace(source[nameEnd]) {
			return start, false
		}
		_, size := utf8.DecodeRuneInString(source[nameEnd:end])
		nameEnd += size
	case '&':
		if nameEnd+1 < end && (source[nameEnd] == 'g' || source[nameEnd] == 'l') && source[nameEnd+1] == ':' {
			nameEnd += 2
		}
		nameStart = nameEnd
		nameEnd = scanWord(source, nameEnd, end)
	case '$':
		nameEnd = scanWord(source, nameEnd, end)
	default:
		return start, false
	}
	if nameEnd <= nameStart {
		return start, false
	}
	// @r denotes exactly one register byte/rune.  Without this check an
	// invalid @name would be accepted as the @n register followed by trailing
	// text, which defeats the loose-recovery boundary.
	if source[start] == '@' {
		_, size := utf8.DecodeRuneInString(source[start+1 : end])
		if nameEnd != start+1+size {
			return start, false
		}
	}
	return nameEnd, true
}

func startsVim9Continuation(source string) bool {
	if strings.HasPrefix(source, "++") || strings.HasPrefix(source, "--") {
		return false
	}
	for _, prefix := range []string{"isnot#", "isnot?", "isnot", "==#", "==?", "!=#", "!=?", "=~#", "=~?", "!~#", "!~?", ">=#", ">=?", "<=#", "<=?", "..", "&&", "||", "??", "->", "==", "!=", "=~", "!~", ">=", "<=", "<<", ">>", "is#", "is?", "is", ".", "+", "-", "*", "/", "%", ">", "<", "?"} {
		if strings.HasPrefix(source, prefix) {
			end := len(prefix)
			return prefix[0] < 'a' || prefix[0] > 'z' || end == len(source) || isExpressionSpace(source[end])
		}
	}
	return false
}

func startsVim9RecoveryCommand(source string, start, end int) bool {
	if start >= end {
		return false
	}
	switch source[start] {
	case ':', '}':
		return true
	case '%':
		return true
	case '&', '$', '@':
		return looksLikeVim9SigilExpression(source, start, end)
	}
	wordEnd := scanWord(source, start, end)
	if wordEnd == start {
		return false
	}
	if looksLikeVim9AssignmentAfterName(source, wordEnd, end) {
		return true
	}
	switch source[start:wordEnd] {
	case "abstract", "break", "catch", "class", "const", "continue", "def", "defer",
		"echo", "echoconsole", "echoerr", "echomsg", "echon", "echowindow",
		"else", "elseif", "endclass", "enddef", "endenum", "endfor", "endfunction",
		"endif", "endinterface", "endtry", "endwhile", "enum", "export", "final",
		"finally", "for", "function", "if", "import", "interface", "legacy", "let",
		"public", "return", "static", "throw", "try", "type", "var", "vim9cmd",
		"vim9script", "while":
		return true
	default:
		return false
	}
}

func allowsVim9AutomaticContinuation(file *File, commandIndex int) bool {
	command := file.Commands[commandIndex]
	if command.Kind == CommandExpression {
		return true
	}
	if metadata, ok := vimdata.Lookup(command.Canonical); ok && metadata.Flags&vimdata.ExpressionArgument != 0 {
		return true
	}
	if expressionCommand(command.Canonical) {
		return true
	}
	if command.Canonical == "def" || command.Canonical == "function" {
		return true
	}
	if command.Canonical == "put" || command.Canonical == "iput" {
		source := file.Text(command.Argument)
		start := skipSpace(source, 0, len(source))
		return start < len(source) && source[start] == '='
	}
	for index := commandIndex - 1; index >= 0; index-- {
		switch file.Commands[index].Canonical {
		case "endenum":
			return false
		case "enum":
			return command.Canonical != "endenum"
		}
	}
	return false
}

func needsVim9CommandContinuation(file *File, commandIndex int, state vim9ContinuationScan) bool {
	if !allowsVim9AutomaticContinuation(file, commandIndex) {
		return false
	}
	command := file.Commands[commandIndex]
	if command.Canonical == "def" && completeVim9DefHeader(file.Text(command.Argument)) {
		return false
	}
	if completeVim9TypedDeclaration(command, file.Text(command.Argument)) {
		return false
	}
	if state.needsContinuation() {
		return true
	}
	if command.Canonical != "for" {
		return false
	}
	source := file.Text(command.Argument)
	in := findTopLevelKeyword(source, 0, len(source), "in")
	return in < 0 || skipExpressionSpace(source, in+2) == len(source)
}

func usesVim9Continuation(command Command) bool {
	return command.Dialect == Vim9 || command.Canonical == "def"
}

func completeVim9TypedDeclaration(command Command, source string) bool {
	switch command.Canonical {
	case "let", "var", "const", "final":
	default:
		return false
	}
	// A heredoc initializer is deliberately kept on the heredoc path.  It is
	// not an expression boundary, even though the assignment marker is visible
	// to this lightweight continuation probe.
	if command.Heredoc != nil {
		return false
	}
	argumentStart := command.Argument.Start
	argumentEnd := command.Argument.End
	if command.logical != nil {
		// The public command header is mapped back to physical source, while the
		// scanner boundary belongs to the logical view.  Read both from that
		// view so a continuation never compares unlike coordinate systems.
		logical := command.logical
		command = logical.command
		source = logical.view.Text[command.Argument.Start:command.Argument.End]
		argumentStart = command.Argument.Start
		argumentEnd = command.Argument.End
		if command.Heredoc != nil {
			return false
		}
	}
	if assignment := findAssignment(source); assignment.Start >= 0 {
		rightStart := skipExpressionSpace(source, assignment.End)
		if rightStart == len(source) {
			return false
		}
		if boundary := command.boundaryExpression; boundary != nil {
			rhs := Span{Start: argumentStart + rightStart, End: argumentEnd}
			return boundary.argument == rhs && boundary.argument.End > boundary.argument.Start &&
				boundary.expression != nil && boundary.expression.Span.End > boundary.expression.Span.Start &&
				len(boundary.diagnostics) == 0
		}
		_, diagnostics := parseExpression(source[rightStart:], rightStart, Vim9)
		return len(diagnostics) == 0
	}
	_, typeSpan := declarationSpans(source, 0)
	if typeSpan.Start >= typeSpan.End {
		return false
	}
	_, diagnostics := parseTypeAt(source[typeSpan.Start:typeSpan.End], 0)
	return len(diagnostics) == 0
}

func completeVim9DefHeader(source string) bool {
	open := strings.IndexByte(source, '(')
	if open < 0 {
		return false
	}
	close := findMatching(source, open, '(', ')')
	if close < 0 {
		return false
	}
	position := skipExpressionSpace(source, close+1)
	if position == len(source) {
		return true
	}
	if source[position] != ':' {
		return false
	}
	position = skipExpressionSpace(source, position+1)
	if position == len(source) {
		return false
	}
	_, diagnostics := parseTypeAt(source[position:], 0)
	return len(diagnostics) == 0
}

type vim9ContinuationScan struct {
	depth             int
	quote             byte
	inComment         bool
	pendingSpace      bool
	tail              [7]byte
	tailStart         uint8
	tailLen           uint8
	ternaryDepth      []int
	lambdaDepth       []int
	lambdaBodyStarted bool
	bracketDepth      int
	braceDepth        int
}

func scanVim9Continuation(source string, state vim9ContinuationScan) vim9ContinuationScan {
	for index := 0; index < len(source); index++ {
		character := source[index]
		if state.inComment {
			if character == '\n' {
				state.inComment = false
				state.pendingSpace = true
			}
			continue
		}
		if state.quote != 0 {
			state.appendTail(character)
			if character == '\\' && state.quote == '"' && index+1 < len(source) {
				index++
				state.appendTail(source[index])
			} else if character == state.quote {
				if state.quote == '\'' && index+1 < len(source) && source[index+1] == '\'' {
					index++
					state.appendTail(source[index])
				} else {
					state.quote = 0
				}
			}
			continue
		}
		if character == '@' && index+1 < len(source) && !isExpressionSpace(source[index+1]) {
			if len(state.lambdaDepth) > 0 {
				state.lambdaBodyStarted = true
			}
			index++
			// A register name is one expression atom.  Its second byte may be
			// an operator character (notably @/); it must not make the command
			// look as if it ended in that operator.
			state.appendTail('@')
			state.appendTail('r')
			continue
		}
		if character == '#' && (!strings.HasPrefix(source[index:], "#{") || strings.HasPrefix(source[index:], "#{{")) && (state.pendingSpace || index == 0 && state.tailLen == 0) {
			state.inComment = true
			continue
		}
		if character == ' ' || character == '\t' || character == '\r' || character == '\n' {
			state.pendingSpace = true
			continue
		}
		if len(state.lambdaDepth) > 0 {
			state.lambdaBodyStarted = true
		}
		state.appendTail(character)
		if character == '\'' && isDigitSeparator(source, index) {
			continue
		}
		if character == '\'' || character == '"' {
			state.quote = character
			continue
		}
		switch character {
		case '(':
			state.depth++
		case '[':
			state.depth++
			state.bracketDepth++
		case '{':
			state.depth++
			state.braceDepth++
			if vim9LambdaBlockOpen(source, index) {
				if len(state.lambdaDepth) == 0 {
					state.lambdaBodyStarted = false
				}
				state.lambdaDepth = append(state.lambdaDepth, state.depth)
			}
		case ')':
			if state.depth > 0 {
				state.depth--
			}
		case ']':
			if state.depth > 0 {
				state.depth--
			}
			if state.bracketDepth > 0 {
				state.bracketDepth--
			}
		case '}':
			if len(state.lambdaDepth) > 0 && state.lambdaDepth[len(state.lambdaDepth)-1] == state.depth {
				state.lambdaDepth = state.lambdaDepth[:len(state.lambdaDepth)-1]
			}
			if state.depth > 0 {
				state.depth--
			}
			if state.braceDepth > 0 {
				state.braceDepth--
			}
		case '?':
			if vim9TernaryQuestion(source, index) {
				state.ternaryDepth = append(state.ternaryDepth, state.depth)
			}
		case ':':
			if len(state.ternaryDepth) > 0 && state.ternaryDepth[len(state.ternaryDepth)-1] == state.depth && !vim9ScopeColon(source, index) {
				state.ternaryDepth = state.ternaryDepth[:len(state.ternaryDepth)-1]
			}
		}
	}
	return state
}

func vim9LambdaBlockOpen(source string, index int) bool {
	position := index
	for position > 0 && isExpressionSpace(source[position-1]) {
		position--
	}
	return position >= 2 && source[position-2:position] == "=>"
}

func looksLikeVim9NamedItem(source string, start, end int) bool {
	wordEnd := scanWord(source, start, end)
	if wordEnd == start {
		return false
	}
	position := skipSpace(source, wordEnd, end)
	return position < end && source[position] == ':'
}

func continuesVim9FunctionSignature(file *File, commandIndex int, state vim9ContinuationScan, source string, start, end int) bool {
	if file == nil || commandIndex < 0 || commandIndex >= len(file.Commands) || state.depth == 0 {
		return false
	}
	command := file.Commands[commandIndex].Canonical
	if command != "def" && command != "function" {
		return false
	}
	wordEnd := scanWord(source, start, end)
	closing := source[start:wordEnd]
	return closing != "enddef" && closing != "endfunction"
}

func (state *vim9ContinuationScan) appendTail(character byte) {
	if state.pendingSpace && state.tailLen != 0 {
		state.appendTailByte(' ')
	}
	state.pendingSpace = false
	state.appendTailByte(character)
}

func (state *vim9ContinuationScan) appendTailByte(character byte) {
	if int(state.tailLen) < len(state.tail) {
		index := int(state.tailStart) + int(state.tailLen)
		if index >= len(state.tail) {
			index -= len(state.tail)
		}
		state.tail[index] = character
		state.tailLen++
		return
	}
	state.tail[state.tailStart] = character
	state.tailStart++
	if int(state.tailStart) == len(state.tail) {
		state.tailStart = 0
	}
}

func (state vim9ContinuationScan) tailHasSuffix(suffix string) bool {
	if len(suffix) > int(state.tailLen) {
		return false
	}
	start := int(state.tailLen) - len(suffix)
	for index := range len(suffix) {
		if state.tailByte(start+index) != suffix[index] {
			return false
		}
	}
	return true
}

func (state vim9ContinuationScan) tailByte(index int) byte {
	position := int(state.tailStart) + index
	if position >= len(state.tail) {
		position -= len(state.tail)
	}
	return state.tail[position]
}

func (state vim9ContinuationScan) needsContinuation() bool {
	if state.depth > 0 || len(state.ternaryDepth) > 0 {
		return true
	}
	for _, suffix := range []string{"isnot#", "isnot?", "isnot", "is#", "is?", "is"} {
		if state.tailHasSuffix(suffix) {
			start := int(state.tailLen) - len(suffix)
			if start == 0 || isExpressionSpace(state.tailByte(start-1)) {
				return true
			}
		}
	}
	for _, suffix := range []string{"==#", "==?", "!=#", "!=?", "=~#", "=~?", "!~#", "!~?", ">=#", ">=?", "<=#", "<=?", "..", "&&", "||", "??", "=>", "==", "!=", "=~", "!~", ">=", "<=", "<<", ">>", "+", "-", "*", "/", "%", ">", "<", "=", ",", "?", ":"} {
		// The greater-than operator is also a suffix of the method
		// operator. A line ending in "->" is not a legal automatic
		// continuation; Vim reports E260/E488 before reading the next line.
		if suffix == ">" && state.tailHasSuffix("->") {
			continue
		}
		if state.tailHasSuffix(suffix) {
			return true
		}
	}
	return false
}

func vim9TernaryQuestion(source string, index int) bool {
	if index+1 < len(source) && source[index+1] == '?' || index > 0 && source[index-1] == '?' {
		return false
	}
	if index > 0 && strings.ContainsRune("=!~<>", rune(source[index-1])) {
		return false
	}
	for _, word := range []string{"is", "isnot"} {
		start := index - len(word)
		if start >= 0 && source[start:index] == word && (start == 0 || isExpressionSpace(source[start-1])) {
			return false
		}
	}
	return true
}

func vim9ScopeColon(source string, index int) bool {
	if index == 0 || !strings.ContainsRune("abglstvw", rune(source[index-1])) {
		return false
	}
	return index == 1 || !isExpressionLetter(source[index-2]) && !isExpressionDigit(source[index-2]) && source[index-2] != '_'
}

// scanSourceArgumentEnd follows :source's EX_FILE1 command-line grammar.
// Its argument is a filename, not an expression: single quotes never group,
// and a double quote starts a comment only in legacy script. Backslash and
// CTRL-V protect the next delimiter. Escaped trailing whitespace is retained
// because Vim removes only unprotected trailing whitespace.
func scanSourceArgumentEnd(source string, start, end int, dialect Dialect) (int, Span, Span) {
	for index := start; index < end; index++ {
		character := source[index]
		if character == '\\' || character == 0x16 {
			if index+1 < end {
				index++
			}
			continue
		}
		if character == '"' && dialect == Legacy {
			return trimSourceArgumentEnd(source, start, index), Span{}, Span{Start: index, End: end}
		}
		if character == '#' && dialect == Vim9 && (index == start || index > 0 && isSpace(source[index-1])) {
			return trimSourceArgumentEnd(source, start, index), Span{}, Span{Start: index, End: end}
		}
		if character == '|' {
			return trimSourceArgumentEnd(source, start, index), Span{Start: index, End: index + 1}, Span{}
		}
	}
	return trimSourceArgumentEnd(source, start, end), Span{}, Span{}
}

func trimSourceArgumentEnd(source string, start, end int) int {
	for end > start && isSpace(source[end-1]) {
		if end-1 > start && (source[end-2] == '\\' || source[end-2] == 0x16) {
			break
		}
		end--
	}
	return end
}

// scanMappingArgumentEnd mirrors the default :map command-line boundary
// rules. Quotes and brackets have no grouping meaning in a mapping RHS. A
// literal CTRL-V always protects the next byte, and with Vim's default
// 'cpoptions' a backslash protects a following bar.
func scanMappingArgumentEnd(source string, start, end int) (int, Span, Span) {
	for index := start; index < end; index++ {
		switch source[index] {
		case 0x16:
			if index+1 < end {
				index++
			}
		case '\\':
			if index+1 < end && source[index+1] == '|' {
				index++
			}
		case '|':
			return index, Span{Start: index, End: index + 1}, Span{}
		}
	}
	// Mapping RHS and :unmap LHS trailing whitespace is significant.
	return end, Span{}, Span{}
}

func usesEscapedExArgument(name string) bool {
	return name == "set" || name == "setlocal" || name == "setglobal" ||
		name == "menutranslate" || strings.HasSuffix(name, "menu")
}

func scanCatchArgument(source string, start, end int) (int, Span, Span) {
	position := skipSpace(source, start, end)
	if position >= end {
		return trimSpaceEnd(source, start, end), Span{}, Span{}
	}
	if source[position] == '|' {
		return trimSpaceEnd(source, start, position), Span{Start: position, End: position + 1}, Span{}
	}
	delimiter := source[position]
	position++
	for position < end {
		if source[position] == '\\' && position+1 < end {
			position += 2
			continue
		}
		if source[position] == delimiter {
			position++
			break
		}
		position++
	}
	position = skipSpace(source, position, end)
	if position < end && source[position] == '|' {
		return trimSpaceEnd(source, start, position), Span{Start: position, End: position + 1}, Span{}
	}
	return trimSpaceEnd(source, start, end), Span{}, Span{}
}

// scanEscapedExArgument mirrors separate_nextcmd() for commands whose
// arguments are opaque to the expression parser.  A backslash immediately
// before a delimiter and CTRL-V protect that delimiter.  Vim advances by a
// whole character, which matters for legacy scriptencoding files where a
// multibyte trail byte can equal ASCII '|'.
func scanEscapedExArgument(source string, start, end int, dialect Dialect, command vimdata.Command) (int, Span, Span) {
	for index := start; index < end; {
		character := source[index]
		if character == 0x16 {
			index++
			if index < end {
				index = nextEncodedCharacter(source, index, end)
			}
			continue
		}
		if character >= utf8.RuneSelf {
			next := nextEncodedCharacter(source, index, end)
			if next > index+1 {
				index = next
				continue
			}
			// Vim converts scriptencoding input before Ex parsing.  Retaining
			// raw source offsets means an invalid UTF-8 byte may still be a
			// CP932/Big5 lead byte; skip its trail byte as one source character.
			if character >= 0x81 && index+1 < end {
				index += 2
				continue
			}
		}
		escaped := index > start && source[index-1] == '\\'
		if character == '|' && !escaped {
			return trimSpaceEnd(source, start, index), Span{Start: index, End: index + 1}, Span{}
		}
		if !escaped && command.Flags&vimdata.NoTrailingComment == 0 {
			if dialect == Legacy && character == '"' || dialect == Vim9 && character == '#' && (index == start || isSpace(source[index-1])) {
				return trimSpaceEnd(source, start, index), Span{}, Span{Start: index, End: end}
			}
		}
		index++
	}
	return trimSpaceEnd(source, start, end), Span{}, Span{}
}

func nextEncodedCharacter(source string, start, end int) int {
	_, size := utf8.DecodeRuneInString(source[start:end])
	if size < 1 {
		size = 1
	}
	return start + size
}

func isCommentStart(source string, index, argumentStart, end int, dialect Dialect, command vimdata.Command) bool {
	if dialect == Vim9 {
		if source[index] != '#' || index+1 < end && source[index+1] == '{' && (index+2 >= end || source[index+2] != '{') {
			return false
		}
		return index == argumentStart || index > 0 && isSpace(source[index-1])
	}
	if source[index] != '"' {
		return false
	}
	// Expression commands have EX_NOTRLCOM in Vim's command table because
	// their evaluator consumes the expression itself.  The evaluator still
	// stops at a legacy trailing double-quote comment, so let the scanner find
	// that boundary while retaining the flag for non-expression commands.
	if !expressionCommand(command.Name) && command.Flags&vimdata.NoTrailingComment != 0 {
		return false
	}
	if expressionCommand(command.Name) {
		// :echo and :execute consume a sequence of expressions, so a double
		// quote after one value starts the next string expression.  Commands
		// such as :return, :if and :let consume one expression; once that
		// expression is complete, a whitespace-separated quote starts the
		// trailing comment even when the comment itself contains quotes.
		if allowsMultipleExpressionArguments(command.Name) || index == argumentStart || index == 0 || !isSpace(source[index-1]) || legacyExpressionNeedsOperand(source, argumentStart, index) {
			return false
		}
		return true
	}
	if index == argumentStart {
		return true
	}
	if index == 0 || !isSpace(source[index-1]) {
		return false
	}
	return true
}

func allowsMultipleExpressionArguments(name string) bool {
	switch name {
	case "echo", "echoconsole", "echoerr", "echomsg", "echon", "echowindow", "execute":
		return true
	default:
		return false
	}
}

func legacyExpressionNeedsOperand(source string, start, end int) bool {
	for {
		end = trimSpaceEnd(source, start, end)
		lineStart := strings.LastIndexByte(source[start:end], '\n')
		if lineStart < 0 {
			break
		}
		lineStart += start + 1
		marker := skipSpace(source, lineStart, end)
		if marker >= end || source[marker] != '\\' || skipSpace(source, marker+1, end) != end {
			break
		}
		// A legacy continuation marker is removed before expression
		// evaluation.  When the quote is the first token on the continued
		// line, determine its role from the preceding logical token.
		end = lineStart - 1
		if end > start && source[end-1] == '\r' {
			end--
		}
	}
	end = trimSpaceEnd(source, start, end)
	if end <= start {
		return true
	}
	if strings.ContainsRune("=+-*/%.!?<>&|^~#([{,:", rune(source[end-1])) {
		return true
	}
	wordEnd := end
	wordStart := wordEnd
	for wordStart > start {
		character := source[wordStart-1]
		if character != '_' && (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') {
			break
		}
		wordStart--
	}
	switch source[wordStart:wordEnd] {
	case "in", "is", "isnot":
		return true
	default:
		return false
	}
}

func expressionCommand(name string) bool {
	switch name {
	case "call", "caddexpr", "cexpr", "cgetexpr", "const", "defer", "echo", "echoconsole", "echoerr", "echomsg", "echon", "echowindow", "eval", "execute", "final", "for", "if", "elseif", "laddexpr", "let", "lexpr", "lgetexpr", "return", "throw", "var", "while":
		return true
	default:
		return false
	}
}

func startsWithVim9Script(source string) bool {
	_, found := findVim9ScriptPrologue(source)
	return found
}

func findVim9ScriptPrologue(source string) (Command, bool) {
	file := &File{Source: source}
	state := 0
	guardDepth := 0
	var blockStack []BlockKind
	dynamicGuard := false
	directFinish := false
	for start := 0; start < len(source); {
		end := strings.IndexByte(source[start:], '\n')
		if end < 0 {
			end = len(source)
		} else {
			end += start
		}
		commandStart := start
		if commandStart == 0 && strings.HasPrefix(source, "\ufeff") {
			commandStart = len("\ufeff")
		}
		before := len(file.Commands)
		scanCommands(file, commandStart, end, Legacy)
		for index := before; index < len(file.Commands); index++ {
			command := file.Commands[index]
			switch state {
			case 0:
				if command.Canonical == "vim9script" {
					return command, true
				}
				if command.Canonical != "if" {
					return Command{}, false
				}
				guardSource := file.Text(command.Argument)
				if isVim9AlwaysActiveGuard(guardSource) {
					return Command{}, false
				}
				dynamicGuard = !isVim9CompatibilityGuard(guardSource)
				state = 1
				guardDepth = 1
				blockStack = []BlockKind{BlockIf}
			case 1:
				switch command.Canonical {
				case "if":
					guardDepth++
					blockStack = append(blockStack, BlockIf)
				case "elseif", "else":
					if guardDepth == 1 {
						return Command{}, false
					}
				case "finish":
					if dynamicGuard && guardDepth == 1 && len(blockStack) == 1 && directVim9PrologueFinish(command) {
						directFinish = true
					}
				case "endif":
					guardDepth--
					for block := len(blockStack) - 1; block >= 0; block-- {
						if blockStack[block] == BlockIf {
							blockStack = blockStack[:block]
							break
						}
					}
					if guardDepth == 0 {
						if dynamicGuard && !directFinish {
							return Command{}, false
						}
						state = 2
					}
				default:
					if kind, opening := openingBlock(file, &command); opening {
						blockStack = append(blockStack, kind)
					} else if kind, closing := closingBlock(file, &command); closing {
						for block := len(blockStack) - 1; block >= 0; block-- {
							if blockStack[block] == kind {
								blockStack = blockStack[:block]
								break
							}
						}
					}
				}
			case 2:
				if command.Canonical == "vim9script" {
					return command, true
				}
				return Command{}, false
			}
		}
		if end == len(source) {
			break
		}
		start = end + 1
	}
	return Command{}, false
}

func directVim9PrologueFinish(command Command) bool {
	return command.Canonical == "finish" && command.Argument.Start == command.Argument.End &&
		command.Range.Start == command.Range.End && command.Bang.Start == command.Bang.End
}

func isVim9CompatibilityGuard(source string) bool {
	expression, diagnostics := (LegacyExpressionParser{}).Parse(source)
	if len(diagnostics) != 0 || expression == nil {
		return false
	}
	negated := false
	call := expression
	if expression.Kind == ExpressionUnary && expression.Value == "!" && len(expression.Children) == 1 {
		negated = true
		call = expression.Children[0]
	}
	if call.Kind != ExpressionCall || len(call.Children) != 2 || call.Children[0].Kind != ExpressionIdentifier || call.Children[0].Value != "has" || call.Children[1].Kind != ExpressionString {
		return false
	}
	feature := call.Children[1].Value
	if len(feature) < 2 || feature[0] != feature[len(feature)-1] || feature[0] != '\'' && feature[0] != '"' {
		return false
	}
	feature = feature[1 : len(feature)-1]
	if negated {
		return feature == "vim9script" || strings.HasPrefix(feature, "patch-")
	}
	// On Vim this branch is skipped; on Neovim it executes :finish before
	// reaching :vim9script.  Thus :vim9script is the first command executed
	// on every path where sourcing continues.
	return feature == "nvim"
}

func isVim9AlwaysActiveGuard(source string) bool {
	expression, diagnostics := (LegacyExpressionParser{}).Parse(source)
	if len(diagnostics) != 0 || expression == nil {
		return false
	}
	call := expression
	if call.Kind != ExpressionCall || len(call.Children) != 2 || call.Children[0].Kind != ExpressionIdentifier || call.Children[1].Kind != ExpressionString {
		return false
	}
	feature := call.Children[1].Value
	if len(feature) < 2 || feature[0] != feature[len(feature)-1] || feature[0] != '\'' && feature[0] != '"' {
		return false
	}
	feature = feature[1 : len(feature)-1]
	return call.Children[0].Value == "exists" && feature == "*has" || call.Children[0].Value == "has" && feature == "vim9script"
}

func lookupModifier(word string) (string, bool) {
	for _, modifier := range modifiers {
		if len(word) >= modifier.min && strings.HasPrefix(modifier.name, word) {
			return modifier.name, true
		}
	}
	return "", false
}

func scanRange(source string, start, end int) int {
	index := start
	if index < end && source[index] == '%' {
		return index + 1
	}
	for index < end {
		character := source[index]
		switch {
		case character >= '0' && character <= '9', strings.ContainsRune(".$,;+-", rune(character)):
			index++
		case character == '\'' && index+1 < end:
			index += 2
		case character == '/' || character == '?':
			delimiter := character
			index++
			for index < end {
				if source[index] == '\\' {
					index += 2
				} else if source[index] == delimiter {
					index++
					break
				} else {
					index++
				}
			}
		default:
			return index
		}
	}
	return index
}

func vim9ModifierRangeRequiresColon(source string, start, rangeEnd, end int) bool {
	if rangeEnd <= start || start >= end {
		return false
	}
	// These prefixes are expressions or marks in Vim9, not Ex ranges.
	if source[start] == '$' || source[start] == '\'' || start+1 < end && (source[start:start+2] == "0z" || source[start:start+2] == "++" || source[start:start+2] == "--") {
		return false
	}
	position := start
	for position < end && source[position] >= '0' && source[position] <= '9' {
		position++
	}
	if position > start {
		position = skipSpace(source, position, end)
		if position+1 < end && source[position:position+2] == "->" {
			return false
		}
	}
	return true
}

func scanCommandName(source string, start, end int) int {
	if start >= end {
		return start
	}
	if strings.ContainsRune("!#&*<=>@~{}", rune(source[start])) {
		return start + 1
	}
	if start+1 < end && (source[start:start+2] == "++" || source[start:start+2] == "--") {
		return start + 2
	}
	index := start
	for index < end && (source[index] >= 'A' && source[index] <= 'Z' || source[index] >= 'a' && source[index] <= 'z') {
		index++
	}
	// Vim accepts digits in the Python command family (for example :py3 and
	// :python3file), and the digit in :vim9cmd and :vim9script.
	if start+2 <= index && source[start:start+2] == "py" {
		for index < end && (source[index] >= 'A' && source[index] <= 'Z' || source[index] >= 'a' && source[index] <= 'z' || source[index] >= '0' && source[index] <= '9') {
			index++
		}
	} else if index == start+3 && source[start:index] == "vim" && index < end && source[index] == '9' {
		index++
		for index < end && (source[index] >= 'A' && source[index] <= 'Z' || source[index] >= 'a' && source[index] <= 'z') {
			index++
		}
	}
	return index
}

func scanWord(source string, start, end int) int {
	index := start
	for index < end {
		r, size := utf8.DecodeRuneInString(source[index:end])
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		index += size
	}
	return index
}

func startsUpper(value string) bool {
	r, _ := utf8.DecodeRuneInString(value)
	return unicode.IsUpper(r)
}

func skipSpaceToken(file *File, start, end int) int {
	next := skipSpace(file.Source, start, end)
	if next > start {
		file.Tokens = append(file.Tokens, Token{Kind: TokenWhitespace, Span: Span{Start: start, End: next}})
	}
	return next
}

func skipSpace(source string, start, end int) int {
	for start < end && isSpace(source[start]) {
		start++
	}
	return start
}

func trimSpaceEnd(source string, start, end int) int {
	for end > start && isSpace(source[end-1]) {
		end--
	}
	return end
}

func isSpace(character byte) bool {
	return character == ' ' || character == '\t'
}
