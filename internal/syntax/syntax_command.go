package syntax

import (
	"strings"
	"unicode/utf8"

	"github.com/neoclide/vimls-go/internal/vimdata"
)

// scanSyntaxArgument gives the structured syntax item consumers their own Ex
// boundary.  A regexp delimiter is payload until its matching delimiter, and
// a malformed item owns the rest of the current logical line for recovery.
func scanSyntaxArgument(source string, start, end int, dialect Dialect, command *Command, metadata vimdata.Command) (int, Span, Span, *expressionBoundary) {
	result, ok := parseSyntaxCommand(source, start, end, dialect)
	if !ok {
		if dialect == Legacy {
			argumentEnd, separator, comment := scanLegacyOpaqueArgument(source, start, end, metadata)
			return argumentEnd, separator, comment, nil
		}
		argumentEnd, separator, comment := scanVim9OpaqueArgument(source, start, end, metadata)
		return argumentEnd, separator, comment, nil
	}
	command.Syntax = result.node
	if result.malformed {
		command.boundaryExpression = &expressionBoundary{argument: Span{Start: start, End: end}, diagnostics: result.diagnostics}
		return end, Span{}, Span{}, command.boundaryExpression
	}
	return result.argumentEnd, result.separator, result.comment, nil
}

type syntaxParseResult struct {
	node        *SyntaxCommand
	diagnostics []Diagnostic
	malformed   bool
	argumentEnd int
	separator   Span
	comment     Span
}

type syntaxParser struct {
	source    string
	start     int
	end       int
	dialect   Dialect
	position  int
	diags     []Diagnostic
	malformed bool
	// EX_NOTRLCOM on :syntax leaves trailing filename whitespace intact.
	preserveTrailing bool
	syncMatch        bool
	vim9Comment      bool
}

func parseSyntaxCommand(source string, start, end int, dialect Dialect) (syntaxParseResult, bool) {
	position := skipSpace(source, start, end)
	subEnd := position
	for subEnd < end && isASCIIAlpha(source[subEnd]) {
		subEnd++
	}
	// Vim dispatches the empty subcommand to syn_cmd_list.  This includes an
	// empty argument and any argument whose first byte is not ASCII alphabetic
	// (for example @Cluster and numeric group names).  Alphabetic unknown
	// subcommands remain opaque so future syntax commands are not misparsed.
	implicitList := subEnd == position || !isASCIIAlpha(source[position])
	subcommand := source[position:subEnd]
	if !implicitList && subcommand != "keyword" && subcommand != "match" && subcommand != "region" && subcommand != "cluster" && subcommand != "case" && subcommand != "conceal" && subcommand != "spell" && subcommand != "include" && subcommand != "clear" && subcommand != "list" && subcommand != "sync" && subcommand != "iskeyword" && subcommand != "foldlevel" && subcommand != "enable" && subcommand != "manual" && subcommand != "on" && subcommand != "off" && subcommand != "reset" {
		return syntaxParseResult{}, false
	}
	parsePosition := subEnd
	if implicitList {
		parsePosition = position
	}
	p := &syntaxParser{source: source, start: start, end: end, dialect: dialect, position: parsePosition}
	node := &SyntaxCommand{}
	if implicitList {
		node.Kind = SyntaxList
		p.parseList(node)
	} else {
		node.Subcommand = Span{Start: position, End: subEnd}
		switch subcommand {
		case "keyword":
			node.Kind = SyntaxKeyword
			p.parseKeyword(node)
		case "match":
			node.Kind = SyntaxMatch
			p.parseMatch(node)
		case "region":
			node.Kind = SyntaxRegion
			p.parseRegion(node)
		case "cluster":
			node.Kind = SyntaxCluster
			p.parseCluster(node)
		case "case":
			node.Kind = SyntaxCase
			p.parseMode(node, "match", "ignore")
		case "conceal":
			node.Kind = SyntaxConceal
			p.parseMode(node, "on", "off")
		case "spell":
			node.Kind = SyntaxSpell
			p.parseMode(node, "toplevel", "notoplevel", "default")
		case "include":
			node.Kind = SyntaxInclude
			p.parseInclude(node)
		case "clear":
			node.Kind = SyntaxClear
			p.parseList(node)
		case "list":
			node.Kind = SyntaxList
			p.parseList(node)
		case "sync":
			node.Kind = SyntaxSync
			p.parseSync(node)
		case "iskeyword":
			node.Kind = SyntaxIsKeyword
			p.parseIsKeyword(node)
		case "foldlevel":
			node.Kind = SyntaxFoldlevel
			p.parseFoldlevel(node)
		case "enable":
			node.Kind = SyntaxEnable
			p.parseRuntimeMode()
		case "manual":
			node.Kind = SyntaxManual
			p.parseRuntimeMode()
		case "on":
			node.Kind = SyntaxOn
			p.parseRuntimeMode()
		case "off":
			node.Kind = SyntaxOff
			p.parseRuntimeMode()
		case "reset":
			node.Kind = SyntaxReset
			p.parseRuntimeMode()
		}
	}
	argumentEnd, separator, comment := p.end, Span{}, Span{}
	if len(p.diags) == 0 && !p.malformed {
		argumentEnd, separator, comment = p.finish()
	}
	if len(p.diags) > 0 || p.malformed {
		return syntaxParseResult{node: node, diagnostics: p.diags, malformed: true}, true
	}
	return syntaxParseResult{node: node, argumentEnd: argumentEnd, separator: separator, comment: comment}, true
}

func isASCIIAlpha(character byte) bool {
	return character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
}

func (p *syntaxParser) parseKeyword(node *SyntaxCommand) {
	p.skip()
	if p.atTerminator() {
		p.error("vim/E475", "invalid argument", Span{Start: p.position, End: p.position})
		return
	}
	group, ok := p.token()
	if !ok {
		p.error("vim/E475", "invalid argument", Span{Start: p.position, End: p.position + 1})
		return
	}
	node.Group = group
	p.skip()
	// get_group_name() rejects a group followed only by NUL, but accepts an
	// explicit separator/comment and option-only definitions.
	if p.position >= p.end {
		p.error("vim/E475", "invalid argument", group)
		return
	}
	for {
		if p.atTerminator() {
			return
		}
		p.skip()
		if p.atTerminator() {
			return
		}
		if option, recognized := p.option(true); recognized {
			node.Options = append(node.Options, option)
			if len(p.diags) > 0 {
				return
			}
			continue
		}
		if keyword, ok := p.token(); ok {
			node.Keywords = append(node.Keywords, keyword)
			p.validateKeyword(keyword)
			if len(p.diags) > 0 {
				return
			}
			continue
		}
		return
	}
}

func (p *syntaxParser) parseCluster(node *SyntaxCommand) {
	p.skip()
	if p.atTerminator() {
		p.error("vim/E400", "no cluster specified", Span{Start: p.position, End: p.position})
		return
	}
	group, ok := p.token()
	if !ok {
		p.error("vim/E400", "no cluster specified", Span{Start: p.position, End: p.position + 1})
		return
	}
	node.Group = group
	gotOption := false
	for {
		p.skip()
		if p.atTerminator() {
			if !gotOption {
				p.error("vim/E400", "no cluster specified", group)
			}
			return
		}
		option, recognized := p.clusterOption()
		if !recognized {
			if !gotOption {
				p.error("vim/E400", "no cluster specified", group)
			} else {
				p.error("vim/E475", "invalid argument", Span{Start: p.position, End: p.end})
			}
			return
		}
		node.Options = append(node.Options, option)
		gotOption = true
		if len(p.diags) > 0 {
			return
		}
	}
}

func (p *syntaxParser) parseMode(node *SyntaxCommand, allowed ...string) {
	p.skip()
	if p.position >= p.end {
		return
	}
	value, ok := p.token()
	if !ok {
		return
	}
	text := p.source[value.Start:value.End]
	valid := false
	for _, item := range allowed {
		if strings.EqualFold(text, item) {
			valid = true
			break
		}
	}
	if !valid {
		p.error("vim/E390", "illegal argument", Span{Start: value.Start, End: p.end})
		return
	}
	node.Keywords = append(node.Keywords, value)
	// These Vim consumers validate only their first whitespace-delimited token
	// and find_nextcmd() independently selects the first bar. Bytes between
	// them are ignored payload, not a legacy/Vim9 comment.
	if offset := strings.IndexByte(p.source[p.position:p.end], '|'); offset >= 0 {
		p.position += offset
	} else {
		p.position = p.end
	}
}

// parseList parses the group/cluster operands of :syntax clear/list.  Vim
// checks ends_excmd2() at each operand start, then consumes the operand up to
// whitespace.  A bar is an Ex command boundary even when adjacent to a token;
// quotes and hashes already in a token remain payload bytes.
func (p *syntaxParser) parseList(node *SyntaxCommand) {
	for {
		p.skip()
		if p.atTerminator() {
			return
		}
		start := p.position
		for p.position < p.end && !isSpace(p.source[p.position]) && p.source[p.position] != '|' {
			p.position++
		}
		if p.position > start {
			node.Keywords = append(node.Keywords, Span{Start: start, End: p.position})
			continue
		}
		return
	}
}

// parseIsKeyword treats its argument as one opaque chartab specification.
// Vim does not split this command at Ex bars or comment bytes; retaining the
// whole remainder also preserves the raw "clear" spelling for consumers.
func (p *syntaxParser) parseIsKeyword(node *SyntaxCommand) {
	p.skip()
	if p.position >= p.end {
		return
	}
	node.Keywords = append(node.Keywords, Span{Start: p.position, End: p.end})
	p.position = p.end
}

func (p *syntaxParser) parseFoldlevel(node *SyntaxCommand) {
	p.skip()
	if p.position >= p.end {
		return
	}
	value, _ := p.token()
	valueText := p.source[value.Start:value.End]
	if !strings.EqualFold(valueText, "start") && !strings.EqualFold(valueText, "minimum") {
		p.error("vim/E390", "illegal argument", Span{Start: value.Start, End: p.end})
		return
	}
	node.Keywords = append(node.Keywords, value)
	p.skip()
	if p.position < p.end {
		p.error("vim/E390", "illegal argument", Span{Start: p.position, End: p.end})
	}
}

// parseRuntimeMode follows syn_cmd_onoff() and syn_cmd_reset().  set_nextcmd()
// recognizes only a separator at the start of the remaining argument (or a
// Vim9 # comment there); arbitrary trailing bytes are ignored as one payload.
// Loading Vim runtime scripts is deliberately outside syntax parsing.
func (p *syntaxParser) parseRuntimeMode() {
	p.skip()
	if p.position >= p.end || p.source[p.position] == '|' {
		return
	}
	if p.dialect == Vim9 && p.source[p.position] == '#' {
		p.vim9Comment = true
		return
	}
	p.position = p.end
}

func (p *syntaxParser) parseSync(node *SyntaxCommand) {
	p.skip()
	if p.atTerminator() {
		return
	}
	for {
		p.skip()
		if p.atTerminator() {
			return
		}
		start := p.position
		// syn_cmd_sync() uses skiptowhite() for its outer keyword.  A bar
		// already inside the token is payload, not an Ex separator.
		for p.position < p.end && !isSpace(p.source[p.position]) {
			p.position++
		}
		if p.position == start {
			p.error("vim/E404", "illegal arguments", Span{Start: start, End: start + 1})
			return
		}
		tokenEnd := p.position
		word := p.source[start:tokenEnd]
		lower := strings.ToLower(word)
		equalOffset := strings.IndexByte(lower, '=')
		if equalOffset >= 0 && syncNumericName(lower[:equalOffset]) {
			valueStart := start + equalOffset + 1
			if valueStart >= tokenEnd || p.source[valueStart] < '0' || p.source[valueStart] > '9' {
				p.error("vim/E404", "illegal arguments", Span{Start: start, End: tokenEnd})
				return
			}
			node.Options = append(node.Options, SyntaxOption{
				Name:  Span{Start: start, End: start + equalOffset},
				Equal: Span{Start: start + equalOffset, End: start + equalOffset + 1},
				Value: Span{Start: valueStart, End: tokenEnd},
			})
			continue
		}
		switch {
		case lower == "ccomment":
			option := SyntaxOption{Name: Span{Start: start, End: p.position}}
			p.skip()
			if !p.atTerminator() {
				valueStart := p.position
				// The optional group is another skiptowhite() token.  This
				// deliberately retains an adjacent bar in the group name.
				for p.position < p.end && !isSpace(p.source[p.position]) {
					p.position++
				}
				option.Value = Span{Start: valueStart, End: p.position}
			}
			node.Options = append(node.Options, option)
		case lower == "fromstart":
			node.Options = append(node.Options, SyntaxOption{Name: Span{Start: start, End: p.position}})
		case syncNumericName(lower):
			if p.position >= p.end || p.source[p.position] != '=' {
				p.error("vim/E404", "illegal arguments", Span{Start: start, End: p.position})
				return
			}
			equal := Span{Start: p.position, End: p.position + 1}
			p.position++
			valueStart := p.position
			if valueStart >= p.end || p.source[valueStart] < '0' || p.source[valueStart] > '9' {
				p.error("vim/E404", "illegal arguments", Span{Start: start, End: p.position})
				return
			}
			for p.position < p.end && !isSpace(p.source[p.position]) && p.source[p.position] != '|' {
				p.position++
			}
			node.Options = append(node.Options, SyntaxOption{Name: Span{Start: start, End: equal.Start}, Equal: equal, Value: Span{Start: valueStart, End: p.position}})
		case lower == "linecont":
			p.skip()
			if p.position >= p.end {
				p.error("vim/E404", "illegal arguments", Span{Start: p.position, End: p.position})
				return
			}
			// Vim's E403 depends on mutable buffer state and on whether an
			// earlier regexp compiled successfully.  Retain every syntactically
			// delimited pattern here; stateful analysis may decide whether a
			// particular execution would replace an existing pattern.
			pattern := p.lineContPattern(Span{Start: start, End: tokenEnd})
			node.Patterns = append(node.Patterns, pattern)
			if len(p.diags) > 0 {
				return
			}
		case lower == "match":
			node.Kind = SyntaxSyncMatch
			p.syncMatch = true
			p.position = tokenEnd
			p.parseMatch(node)
			return
		case lower == "region":
			node.Kind = SyntaxSyncRegion
			p.position = tokenEnd
			p.parseRegion(node)
			return
		case lower == "clear":
			node.Options = append(node.Options, SyntaxOption{Name: Span{Start: start, End: p.position}})
			node.Kind = SyntaxSync
			p.parseList(node)
			return
		default:
			p.error("vim/E404", "illegal arguments", Span{Start: start, End: p.position})
			return
		}
	}
}

func syncNumericName(name string) bool {
	return name == "lines" || name == "minlines" || name == "maxlines" || name == "linebreaks"
}

func (p *syntaxParser) lineContPattern(key Span) SyntaxPattern {
	pattern := SyntaxPattern{Kind: SyntaxLineContPattern, Key: key}
	open := p.position
	pattern.OpenDelimiter = Span{Start: open, End: open + 1}
	delimiter := p.source[open]
	close := scanRegexpEndWithMagic(p.source, open+1, p.end, delimiter, globalMagicOn)
	if close >= 0 {
		pattern.Pattern = Span{Start: open + 1, End: close}
		pattern.CloseDelimiter = Span{Start: close, End: close + 1}
		p.position = close + 1
		return pattern
	}
	pattern.Pattern = Span{Start: open + 1, End: p.end}
	p.error("vim/E404", "illegal arguments", pattern.OpenDelimiter)
	p.position = p.end
	return pattern
}

// parseInclude follows syn_cmd_include() and separate_nextcmd() in Vim.  An
// include filename is one EX_XFILE payload: whitespace is part of the
// filename, while the first unprotected bar starts the following command.
// The command's EX_NOTRLCOM flag makes both legacy quotes and Vim9 hashes
// ordinary filename bytes here.
func (p *syntaxParser) parseInclude(node *SyntaxCommand) {
	p.preserveTrailing = true
	p.skip()
	if p.position < p.end && p.source[p.position] == '@' {
		groupStart := p.position + 1
		groupEnd := groupStart
		for groupEnd < p.end && !isSpace(p.source[groupEnd]) {
			groupEnd++
		}
		node.Group = Span{Start: groupStart, End: groupEnd}
		p.position = skipSpace(p.source, groupEnd, p.end)
		// get_group_name() reports E397 only when @group is the true end of
		// the argument.  A bar is an empty filename followed by a command,
		// not a missing filename diagnostic.
		if p.position >= p.end {
			p.error("vim/E397", "filename required", node.Group)
			return
		}
	}
	pathStart := p.position
	boundary, _, _, _, malformed := scanXFileArgument(
		p.source, pathStart, p.end, p.dialect,
		vimdata.Command{Flags: vimdata.AllowBar | vimdata.NoTrailingComment},
	)
	pathEnd := boundary
	if pathEnd > pathStart {
		node.Keywords = append(node.Keywords, Span{Start: pathStart, End: pathEnd})
	}
	p.position = boundary
	if malformed {
		// Do not let a malformed EX_XFILE backtick expose a same-line bar as
		// another command.  The outer scanner will resume at the next physical
		// line (or next logical view) and keeps this recovery line-local.
		p.malformed = true
	}
}

// scanXFileArgument follows separate_nextcmd() for EX_XFILE payloads while
// retaining their raw bytes.  The returned expressions are the `=expr`
// portions of filename expansions; callers keep them for syntax consumers.
// A malformed expansion owns the remainder of the logical line so a bar in
// incomplete input cannot become another command.
func scanXFileArgument(source string, start, end int, dialect Dialect, command vimdata.Command) (int, Span, Span, []*Expression, bool) {
	var expressions []*Expression
	for position := start; position < end; {
		character := source[position]
		switch character {
		case '\x16': // Ctrl-V
			position++
			if position < end {
				position = nextEncodedCharacter(source, position, end)
			}
			continue
		case '|':
			if command.Flags&vimdata.AllowBar != 0 && (position == start || source[position-1] != '\\') {
				return position, Span{Start: position, End: position + 1}, Span{}, expressions, false
			}
		case '`':
			if position+1 >= end || source[position+1] != '=' {
				break
			}
			expressionStart := position + 2
			expression, diagnostics, consumed := parseExpressionPrefix(source[expressionStart:end], expressionStart, dialect)
			if expression != nil && expression.Kind != ExpressionMissing {
				expressions = append(expressions, expression)
			}
			close := expressionStart + consumed
			for close < end && isExpressionSpace(source[close]) {
				close++
			}
			if consumed > 0 && len(diagnostics) == 0 && close < end && source[close] == '`' {
				position = close + 1
				continue
			}
			// Vim's expression expansion cannot safely identify a command
			// boundary after this point.  Keep the remainder opaque.
			return end, Span{}, Span{}, expressions, true
		}
		if command.Flags&vimdata.NoTrailingComment == 0 && isCommentStart(source, position, start, end, dialect, command) {
			return position, Span{}, Span{Start: position, End: end}, expressions, false
		}
		if character >= utf8.RuneSelf {
			next := nextEncodedCharacter(source, position, end)
			if next > position+1 {
				position = next
				continue
			}
			if character >= 0x81 && position+1 < end {
				position += 2
				continue
			}
		}
		position++
	}
	return end, Span{}, Span{}, expressions, false
}

func (p *syntaxParser) parseMatch(node *SyntaxCommand) {
	p.skip()
	if p.atTerminator() {
		p.error("vim/E475", "invalid argument", Span{Start: p.position, End: p.position})
		return
	}
	group, ok := p.token()
	if !ok {
		p.error("vim/E475", "invalid argument", Span{Start: p.position, End: p.position + 1})
		return
	}
	node.Group = group
	for {
		p.skip()
		if option, recognized := p.option(false); recognized {
			node.Options = append(node.Options, option)
			if len(p.diags) > 0 {
				return
			}
			continue
		}
		break
	}
	pattern, ok := p.pattern(SyntaxMatchPattern, Span{}, Span{})
	if ok {
		node.Patterns = append(node.Patterns, pattern)
	}
	if len(p.diags) > 0 {
		return
	}
	for {
		p.skip()
		if p.atTerminator() {
			return
		}
		if option, recognized := p.option(false); recognized {
			node.Options = append(node.Options, option)
			if len(p.diags) > 0 {
				return
			}
			continue
		}
		if p.position < p.end {
			p.error("vim/E475", "invalid argument", Span{Start: p.position, End: p.end})
		}
		return
	}
}

func (p *syntaxParser) parseRegion(node *SyntaxCommand) {
	p.skip()
	if p.atTerminator() {
		p.error("vim/E475", "invalid argument", Span{Start: p.position, End: p.position})
		return
	}
	group, ok := p.token()
	if !ok {
		p.error("vim/E475", "invalid argument", Span{Start: p.position, End: p.position + 1})
		return
	}
	node.Group = group
	hasStart, hasEnd := false, false
	hasSkip := false
	for {
		p.skip()
		if p.atTerminator() {
			if !hasStart || !hasEnd {
				p.error("vim/E399", "not enough arguments for :syntax region", group)
			}
			return
		}
		if option, recognized := p.option(false); recognized {
			node.Options = append(node.Options, option)
			if len(p.diags) > 0 {
				return
			}
			continue
		}
		key, ok := p.wordOrEqualKey()
		if !ok {
			p.error("vim/E475", "invalid argument", Span{Start: p.position, End: p.position + 1})
			return
		}
		name := strings.ToLower(p.source[key.Start:key.End])
		kind := SyntaxMatchPattern
		switch name {
		case "matchgroup":
		case "start":
			kind = SyntaxStartPattern
		case "skip":
			kind = SyntaxSkipPattern
			if hasSkip {
				p.error("vim/E475", "invalid argument", key)
				return
			}
		case "end":
			kind = SyntaxEndPattern
		default:
			if !hasStart || !hasEnd {
				p.error("vim/E399", "not enough arguments for :syntax region", node.Group)
			} else {
				p.error("vim/E475", "invalid argument", key)
			}
			return
		}
		p.skip()
		if p.position >= p.end || p.source[p.position] != '=' {
			p.error("vim/E398", "missing =", key)
			return
		}
		equal := Span{Start: p.position, End: p.position + 1}
		p.position++
		p.skip()
		if name == "matchgroup" {
			if p.atTerminator() {
				p.error("vim/E399", "not enough arguments for :syntax region", node.Group)
				return
			}
			value, exists := p.token()
			if !exists {
				p.error("vim/E475", "invalid argument", equal)
				return
			}
			node.Options = append(node.Options, SyntaxOption{Name: key, Equal: equal, Value: value})
			continue
		}
		pattern, ok := p.pattern(kind, key, equal)
		if ok {
			node.Patterns = append(node.Patterns, pattern)
		}
		if len(p.diags) > 0 {
			return
		}
		switch kind {
		case SyntaxStartPattern:
			hasStart = true
		case SyntaxSkipPattern:
			hasSkip = true
		case SyntaxEndPattern:
			hasEnd = true
		}
	}
}

func (p *syntaxParser) skip() {
	p.position = skipSpace(p.source, p.position, p.end)
}

func (p *syntaxParser) token() (Span, bool) {
	start := p.position
	for p.position < p.end && !isSpace(p.source[p.position]) {
		p.position++
	}
	return Span{Start: start, End: p.position}, p.position > start
}

func (p *syntaxParser) validateKeyword(keyword Span) {
	open := false
	closed := false
	for position := keyword.Start; position < keyword.End; position++ {
		if p.source[position] == '\\' && position+1 < keyword.End {
			position++
		}
		character := p.source[position]
		if closed {
			p.error("vim/E890", "trailing character after ]", keyword)
			return
		}
		if !open {
			if character == '[' {
				open = true
			}
			continue
		}
		if character == ']' {
			closed = true
		}
	}
	if open && !closed {
		p.error("vim/E789", "missing ]", keyword)
	}
}

func (p *syntaxParser) wordOrEqualKey() (Span, bool) {
	start := p.position
	for p.position < p.end && !isSpace(p.source[p.position]) && p.source[p.position] != '=' {
		p.position++
	}
	return Span{Start: start, End: p.position}, p.position > start
}

func (p *syntaxParser) atTerminator() bool {
	return p.position >= p.end || syntaxTerminator(p.source, p.position, p.start, p.end, p.dialect)
}

func syntaxTerminator(source string, position, argumentStart, end int, dialect Dialect) bool {
	if position >= end {
		return true
	}
	if source[position] == '|' {
		return true
	}
	if dialect == Legacy {
		return source[position] == '"'
	}
	if source[position] != '#' {
		return false
	}
	if position != argumentStart && (position == 0 || !isSpace(source[position-1])) {
		return false
	}
	return position+1 >= end || source[position+1] != '{' || position+2 < end && source[position+2] == '{'
}

func (p *syntaxParser) option(keyword bool) (SyntaxOption, bool) {
	start := p.position
	lower, nameEnd, recognized := syntaxOptionAt(p.source, start, p.end, p.start, p.dialect, keyword)
	if !recognized {
		return SyntaxOption{}, false
	}
	isList := syntaxOptionNeedsList(lower)
	isCChar := lower == "cchar"
	option := SyntaxOption{Name: Span{Start: start, End: nameEnd}}
	p.position = nameEnd
	// grouphere/groupthere belong only to :syntax sync; direct items reject
	// them with Vim's dedicated structural error after the normal option-name
	// boundary has been established.
	if lower == "grouphere" || lower == "groupthere" {
		if !p.syncMatch {
			p.error("vim/E393", "groupthere/grouphere not accepted here", option.Name)
			return option, true
		}
		// Unlike ordinary options, the sync target is consumed with
		// skiptowhite(), so even a token beginning with |, #, or " is a
		// target.  Runtime region existence is deliberately not checked.
		p.position = skipSpace(p.source, p.position, p.end)
		if p.position >= p.end {
			p.error("vim/E475", "invalid argument", option.Name)
			return option, true
		}
		valueStart := p.position
		for p.position < p.end && !isSpace(p.source[p.position]) {
			p.position++
		}
		option.Value = Span{Start: valueStart, End: p.position}
		return option, true
	}
	if isList {
		p.position = skipSpace(p.source, p.position, p.end)
		if p.position >= p.end || p.source[p.position] != '=' {
			p.error("vim/E405", "missing equal sign", option.Name)
			return option, true
		}
	} else if isCChar {
		// Vim's cchar branch only consumes a value for the immediate
		// cchar= spelling.  `cchar =x` is a valueless option followed by
		// the raw keyword `=x`.
		if p.position >= p.end || p.source[p.position] != '=' {
			p.position = skipSpace(p.source, p.position, p.end)
			return option, true
		}
	} else {
		p.position = skipSpace(p.source, p.position, p.end)
		return option, true
	}
	option.Equal = Span{Start: p.position, End: p.position + 1}
	p.position++
	valueStart := p.position
	if isList {
		valueStart = skipSpace(p.source, valueStart, p.end)
	}
	p.position = valueStart
	if isCChar {
		// The byte following cchar= belongs to the option even when it is an
		// Ex separator or comment byte (for example cchar=|).
		if valueStart >= p.end {
			p.error("vim/E475", "invalid argument", option.Equal)
			return option, true
		}
		_, size := utf8.DecodeRuneInString(p.source[valueStart:p.end])
		if size < 1 {
			size = 1
		}
		p.position += size
	} else if isList {
		if !p.parseGroupListValue(&option, valueStart) {
			return option, true
		}
		p.validateGroupList(lower, option.Values)
	} else {
		for p.position < p.end && !isSpace(p.source[p.position]) {
			p.position++
		}
	}
	if option.Value == (Span{}) {
		option.Value = Span{Start: valueStart, End: p.position}
	}
	if lower == "contains" && keyword {
		p.error("vim/E395", "contains argument not accepted here", option.Name)
	}
	return option, true
}

func (p *syntaxParser) clusterOption() (SyntaxOption, bool) {
	start := p.position
	lower, nameEnd, recognized := syntaxClusterOptionAt(p.source, start, p.end)
	if !recognized {
		return SyntaxOption{}, false
	}
	option := SyntaxOption{Name: Span{Start: start, End: nameEnd}}
	p.position = skipSpace(p.source, nameEnd, p.end)
	if p.position >= p.end || p.source[p.position] != '=' {
		p.error("vim/E405", "missing equal sign", option.Name)
		return option, true
	}
	option.Equal = Span{Start: p.position, End: p.position + 1}
	p.position++
	valueStart := skipSpace(p.source, p.position, p.end)
	p.position = valueStart
	if !p.parseGroupListValue(&option, valueStart) {
		return option, true
	}
	p.validateGroupList(lower, option.Values)
	return option, true
}

func syntaxClusterOptionAt(source string, start, end int) (string, int, bool) {
	for _, name := range [...]string{"contains", "remove", "add"} {
		nameEnd := start + len(name)
		if nameEnd > end || !strings.EqualFold(source[start:nameEnd], name) {
			continue
		}
		if nameEnd < end && (isSpace(source[nameEnd]) || source[nameEnd] == '=') {
			return name, nameEnd, true
		}
	}
	return "", start, false
}

var syntaxOptionNames = [...]string{
	"concealends", "containedin", "transparent", "nextgroup", "skipwhite",
	"skipempty", "grouphere", "groupthere", "excludenl", "contained",
	"oneline", "keepend", "extend", "skipnl", "display", "conceal",
	"contains", "cchar", "fold",
}

func syntaxOptionAt(source string, start, end, argumentStart int, dialect Dialect, keyword bool) (string, int, bool) {
	if start >= end {
		return "", start, false
	}
	first := source[start]
	if first >= 'A' && first <= 'Z' {
		first += 'a' - 'A'
	}
	for _, name := range syntaxOptionNames {
		if name[0] != first {
			continue
		}
		if keyword && (name == "display" || name == "fold" || name == "extend") {
			continue
		}
		nameEnd := start + len(name)
		if nameEnd > end || !strings.EqualFold(source[start:nameEnd], name) {
			continue
		}
		if nameEnd < end && isSpace(source[nameEnd]) {
			return name, nameEnd, true
		}
		if syntaxOptionNeedsList(name) || name == "cchar" {
			if nameEnd < end && source[nameEnd] == '=' {
				return name, nameEnd, true
			}
			continue
		}
		if nameEnd >= end || syntaxTerminator(source, nameEnd, argumentStart, end, dialect) {
			return name, nameEnd, true
		}
	}
	return "", start, false
}

func syntaxOptionNeedsList(name string) bool {
	return name == "contains" || name == "containedin" || name == "nextgroup"
}

func (p *syntaxParser) parseGroupListValue(option *SyntaxOption, start int) bool {
	if start >= p.end || syntaxTerminator(p.source, start, p.start, p.end, p.dialect) {
		p.error("vim/E406", "empty group list", Span{Start: start, End: start})
		return false
	}
	valueEnd := start
	for {
		itemStart := skipSpace(p.source, p.position, p.end)
		if itemStart >= p.end || syntaxTerminator(p.source, itemStart, p.start, p.end, p.dialect) {
			p.error("vim/E406", "empty group list", Span{Start: itemStart, End: itemStart})
			return false
		}
		itemEnd := itemStart
		for itemEnd < p.end && !isSpace(p.source[itemEnd]) && p.source[itemEnd] != ',' {
			itemEnd++
		}
		// Vim accepts an empty item before a comma and counts it when
		// validating whether ALL/ALLBUT/TOP/CONTAINED came first. Keep its
		// zero-width span; only a wholly missing value is E406.
		option.Values = append(option.Values, Span{Start: itemStart, End: itemEnd})
		valueEnd = itemEnd
		p.position = skipSpace(p.source, itemEnd, p.end)
		if p.position >= p.end || syntaxTerminator(p.source, p.position, p.start, p.end, p.dialect) || p.source[p.position] != ',' {
			break
		}
		valueEnd = p.position + 1
		p.position = skipSpace(p.source, p.position+1, p.end)
		if p.position >= p.end || syntaxTerminator(p.source, p.position, p.start, p.end, p.dialect) {
			break
		}
	}
	option.Value = Span{Start: start, End: valueEnd}
	return true
}

func (p *syntaxParser) validateGroupList(option string, values []Span) {
	for index, value := range values {
		name := p.source[value.Start:value.End]
		if name != "ALL" && name != "ALLBUT" && name != "TOP" && name != "CONTAINED" {
			continue
		}
		if option != "contains" && option != "containedin" {
			p.error("vim/E407", name+" not allowed here", value)
			return
		}
		if index != 0 {
			p.error("vim/E408", name+" must be first in contains list", value)
			return
		}
	}
}

func (p *syntaxParser) pattern(kind SyntaxPatternKind, key, equal Span) (SyntaxPattern, bool) {
	if p.position >= p.end {
		p.error("vim/E475", "invalid argument", Span{Start: p.position, End: p.position})
		return SyntaxPattern{}, false
	}
	open := p.position
	delimiter := p.source[open]
	pattern := SyntaxPattern{Kind: kind, Key: key, Equal: equal, OpenDelimiter: Span{Start: open, End: open + 1}}
	// get_syn_pattern() requires three available bytes before looking for the
	// closing delimiter. Thus `//` at NUL is E475 while `// ` is a valid empty
	// pattern followed by whitespace.
	if p.end-open < 3 {
		pattern.Pattern = Span{Start: open + 1, End: p.end}
		p.position = p.end
		p.error("vim/E475", "invalid argument", pattern.OpenDelimiter)
		return pattern, true
	}
	close := scanRegexpEndWithMagic(p.source, open+1, p.end, delimiter, globalMagicOn)
	if close < 0 {
		pattern.Pattern = Span{Start: open + 1, End: p.end}
		p.position = p.end
		p.error("vim/E401", "pattern delimiter not found", Span{Start: open, End: open + 1})
		return pattern, true
	}
	pattern.Pattern = Span{Start: open + 1, End: close}
	pattern.CloseDelimiter = Span{Start: close, End: close + 1}
	p.position = close + 1
	offsetStart := p.position
	for {
		next := syntaxOffsetEnd(p.source, p.position, p.end)
		if next == p.position {
			break
		}
		p.position = next
		if p.position >= p.end || p.source[p.position] != ',' {
			break
		}
		p.position++
	}
	if p.position > offsetStart {
		pattern.Offsets = Span{Start: offsetStart, End: p.position}
	}
	if p.position < p.end && !isSpace(p.source[p.position]) && !syntaxTerminator(p.source, p.position, p.start, p.end, p.dialect) {
		p.error("vim/E402", "garbage after pattern", Span{Start: p.position, End: p.position + 1})
	}
	return pattern, true
}

func syntaxOffsetEnd(source string, start, end int) int {
	if start+2 >= end {
		return start
	}
	name := source[start : start+2]
	if name != "ms" && name != "me" && name != "hs" && name != "he" && name != "rs" && name != "re" && name != "lc" {
		return start
	}
	if source[start+2] != '=' {
		return start
	}
	position := start + 3
	if name == "lc" {
		for position < end && source[position] >= '0' && source[position] <= '9' {
			position++
		}
		return position
	}
	if position >= end || source[position] != 's' && source[position] != 'b' && source[position] != 'e' {
		return start
	}
	position++
	if position < end && (source[position] == '+' || source[position] == '-') {
		position++
	}
	for position < end && source[position] >= '0' && source[position] <= '9' {
		position++
	}
	return position
}

func (p *syntaxParser) finish() (int, Span, Span) {
	position := skipSpace(p.source, p.position, p.end)
	p.position = position
	if p.vim9Comment {
		return trimSpaceEnd(p.source, p.start, position), Span{}, Span{Start: position, End: p.end}
	}
	if position < p.end && syntaxTerminator(p.source, position, p.start, p.end, p.dialect) {
		argumentEnd := trimSpaceEnd(p.source, p.start, position)
		if p.preserveTrailing {
			argumentEnd = position
		}
		if p.source[position] == '|' {
			return argumentEnd, Span{Start: position, End: position + 1}, Span{}
		}
		return argumentEnd, Span{}, Span{Start: position, End: p.end}
	}
	if p.position >= p.end {
		if p.preserveTrailing {
			return p.end, Span{}, Span{}
		}
		return trimSpaceEnd(p.source, p.start, p.end), Span{}, Span{}
	}
	p.error("vim/E402", "garbage after pattern", Span{Start: p.position, End: p.position + 1})
	return p.end, Span{}, Span{}
}

func (p *syntaxParser) error(code, message string, span Span) {
	p.diags = append(p.diags, Diagnostic{Code: code, Message: message, Span: span})
}
