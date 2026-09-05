package syntax

import "strings"

// logicalView is the text Vim passes from its source-line reader to the Ex
// parser.  Text contains no physical continuation prefix.  bytes keeps the
// corresponding half-open byte range in the original source for every byte
// in Text, so syntax nodes can keep using original-source Span values.
//
// A zero-width byte range is used only for text inserted by Vim itself (the
// separating space before a Vim9 leading-| continuation).
type logicalView struct {
	Text     string
	Source   Span
	Next     int
	Physical []Token

	// A newline is normally the only physical token produced for a logical
	// view's first line. Keep it out of Physical until another physical token
	// makes the slice necessary (or the view is emitted), so identity lines do
	// not allocate a one-token backing array.
	pendingNewline    Span
	hasPendingNewline bool

	// A normal physical line maps directly to Source.Start and needs no map
	// allocation.  Only a view that removes continuation prefixes or inserts
	// synthetic text is materialized into mapped source segments.
	identity bool
	buffer   []byte
	segments []logicalSegment
}

type logicalSegment struct {
	logical Span
	source  Span
}

type logicalCommandView struct {
	view    *logicalView
	command Command
}

func readLegacyLogicalView(source string, start int) (view logicalView) {
	view, next := startLogicalView(source, start)
	defer view.finishText()
	for next < len(source) {
		lineStart := next
		lineEnd, afterLine := physicalLineEnd(source, lineStart)
		first := skipSpace(source, lineStart, lineEnd)

		switch {
		case first < lineEnd && source[first] == '\\':
			view.flushNewline()
			view.addContinuationPrefix(source, lineStart, first, lineEnd)
		case legacyContinuationComment(source, first, lineEnd):
			view.flushNewline()
			view.addContinuationComment(lineStart, first, lineEnd)
		default:
			return
		}

		view.Source.End = lineEnd
		view.addNewline(lineEnd, afterLine)
		view.Next = afterLine
		next = afterLine
	}
	return
}

func readVim9LogicalView(source string, start int) (view logicalView) {
	view, next := startLogicalView(source, start)
	defer view.finishText()
	for next < len(source) {
		lineStart := next
		lineEnd, afterLine := physicalLineEnd(source, lineStart)
		first := skipSpace(source, lineStart, lineEnd)

		switch {
		case first < lineEnd && source[first] == '\\':
			view.flushNewline()
			view.addContinuationPrefix(source, lineStart, first, lineEnd)
		case vim9ContinuationComment(source, first, lineEnd):
			view.flushNewline()
			view.addContinuationComment(lineStart, first, lineEnd)
		case first < lineEnd && source[first] == '|' && (first+1 >= lineEnd || source[first+1] != '|'):
			view.flushNewline()
			if first > lineStart {
				view.Physical = append(view.Physical, Token{Kind: TokenWhitespace, Span: Span{Start: lineStart, End: first}})
			}
			view.Physical = append(view.Physical, Token{Kind: TokenContinuation, Span: Span{Start: first, End: lineEnd}})
			// getsourceline() inserts one space before a Vim9 leading bar.
			view.appendSynthetic(' ', first)
			view.appendSource(source, first, lineEnd)
		default:
			return
		}

		view.Source.End = lineEnd
		view.addNewline(lineEnd, afterLine)
		view.Next = afterLine
		next = afterLine
	}
	return
}

func startLogicalView(source string, start int) (logicalView, int) {
	view := logicalView{Source: Span{Start: start, End: start}, Next: start, identity: true}
	if start < 0 {
		start = 0
	}
	if start >= len(source) {
		view.Source = Span{Start: start, End: start}
		view.Next = start
		return view, start
	}

	contentEnd, next := physicalLineEnd(source, start)
	view.appendSource(source, start, contentEnd)
	view.Source = Span{Start: start, End: contentEnd}
	view.addNewline(contentEnd, next)
	view.Next = next
	return view, next
}

func physicalLineEnd(source string, start int) (contentEnd int, next int) {
	newline := start
	for newline < len(source) && source[newline] != '\n' {
		newline++
	}
	contentEnd = newline
	if contentEnd > start && source[contentEnd-1] == '\r' {
		contentEnd--
	}
	if newline < len(source) {
		next = newline + 1
	} else {
		next = len(source)
	}
	return contentEnd, next
}

func legacyContinuationComment(source string, first, end int) bool {
	if first+2 >= end || source[first+1] != '\\' || source[first+2] != ' ' {
		return false
	}
	return source[first] == '"'
}

func vim9ContinuationComment(source string, first, end int) bool {
	return first+2 < end && source[first] == '#' && source[first+1] == '\\' && source[first+2] == ' '
}

func (view *logicalView) addContinuationPrefix(source string, lineStart, first, lineEnd int) {
	if first > lineStart {
		view.Physical = append(view.Physical, Token{Kind: TokenWhitespace, Span: Span{Start: lineStart, End: first}})
	}
	view.Physical = append(view.Physical, Token{Kind: TokenContinuation, Span: Span{Start: first, End: first + 1}})
	view.makeMapped(lineEnd - first - 1)
	view.appendSource(source, first+1, lineEnd)
}

func (view *logicalView) addContinuationComment(lineStart, first, lineEnd int) {
	if first > lineStart {
		view.Physical = append(view.Physical, Token{Kind: TokenWhitespace, Span: Span{Start: lineStart, End: first}})
	}
	view.Physical = append(view.Physical, Token{Kind: TokenComment, Span: Span{Start: first, End: lineEnd}})
	view.makeMapped(0)
}

func (view *logicalView) appendSource(source string, start, end int) {
	if start >= end {
		return
	}
	if view.identity && len(view.Text) == 0 && start == view.Source.Start {
		view.Text = source[start:end]
		return
	}
	view.makeMapped(end - start)
	logicalStart := len(view.buffer)
	view.buffer = append(view.buffer, source[start:end]...)
	view.appendSegment(Span{Start: logicalStart, End: len(view.buffer)}, Span{Start: start, End: end})
}

func (view *logicalView) appendSynthetic(character byte, original int) {
	view.makeMapped(1)
	logicalStart := len(view.buffer)
	view.buffer = append(view.buffer, character)
	view.appendSegment(Span{Start: logicalStart, End: logicalStart + 1}, Span{Start: original, End: original})
}

func (view *logicalView) makeMapped(extra int) {
	if view.identity {
		capacity := len(view.Text) + extra
		view.buffer = make([]byte, len(view.Text), capacity)
		copy(view.buffer, view.Text)
		if len(view.Text) > 0 {
			view.segments = append(view.segments, logicalSegment{
				logical: Span{Start: 0, End: len(view.Text)},
				source:  Span{Start: view.Source.Start, End: view.Source.Start + len(view.Text)},
			})
		}
		view.identity = false
		return
	}
	if view.buffer == nil {
		view.buffer = make([]byte, len(view.Text), len(view.Text)+extra)
		copy(view.buffer, view.Text)
	}
}

func (view *logicalView) appendSegment(logical, source Span) {
	if len(view.segments) > 0 {
		last := &view.segments[len(view.segments)-1]
		if last.logical.End == logical.Start && last.source.End == source.Start &&
			last.source.Start < last.source.End && source.Start < source.End &&
			last.logical.End-last.logical.Start == last.source.End-last.source.Start &&
			logical.End-logical.Start == source.End-source.Start {
			last.logical.End = logical.End
			last.source.End = source.End
			return
		}
	}
	view.segments = append(view.segments, logicalSegment{logical: logical, source: source})
}

func (view *logicalView) finishText() {
	if !view.identity && view.buffer != nil {
		view.Text = string(view.buffer)
		view.buffer = nil
	}
}

func (view *logicalView) addNewline(contentEnd, next int) {
	if contentEnd < next {
		view.pendingNewline = Span{Start: contentEnd, End: next}
		view.hasPendingNewline = true
	}
}

func (view *logicalView) flushNewline() {
	if !view.hasPendingNewline {
		return
	}
	view.Physical = append(view.Physical, Token{Kind: TokenNewline, Span: view.pendingNewline})
	view.pendingNewline = Span{}
	view.hasPendingNewline = false
}

func (view logicalView) mapSpan(span Span) Span {
	if span.Start < 0 {
		span.Start = 0
	}
	if span.End < span.Start {
		span.End = span.Start
	}
	if span.Start > len(view.Text) {
		span.Start = len(view.Text)
	}
	if span.End > len(view.Text) {
		span.End = len(view.Text)
	}
	if span.Start == span.End {
		position := view.boundary(span.Start)
		return Span{Start: position, End: position}
	}
	return Span{Start: view.byteSpan(span.Start).Start, End: view.byteSpan(span.End - 1).End}
}

func (view logicalView) boundary(index int) int {
	if index < 0 {
		index = 0
	}
	if index < len(view.Text) {
		return view.byteSpan(index).Start
	}
	if len(view.Text) > 0 {
		return view.byteSpan(len(view.Text) - 1).End
	}
	return view.Source.Start
}

func (view logicalView) byteSpan(index int) Span {
	if view.identity {
		start := view.Source.Start + index
		return Span{Start: start, End: start + 1}
	}
	for _, segment := range view.segments {
		if index < segment.logical.Start || index >= segment.logical.End {
			continue
		}
		if segment.source.Start == segment.source.End {
			return segment.source
		}
		if segment.logical.End-segment.logical.Start == 1 {
			return segment.source
		}
		start := segment.source.Start + index - segment.logical.Start
		return Span{Start: start, End: start + 1}
	}
	return Span{Start: view.Source.End, End: view.Source.End}
}

func scanLogicalCommandsWithContext(file *File, view *logicalView, dialect Dialect, directAggregateKind BlockKind, nestedFunction bool, scriptVersion uint8) int {
	if dialect == Legacy && view.identity && !strings.Contains(view.Text, "vim9") && !strings.Contains(view.Text, "def ") && !strings.Contains(view.Text, "def\t") {
		first := len(file.Commands)
		scanCommandsWithContext(file, view.Source.Start, view.Source.End, dialect, directAggregateKind, nestedFunction, scriptVersion)
		file.Tokens = append(file.Tokens, view.Physical...)
		view.appendPendingNewline(file)
		return first
	}
	first := scanLogicalCommandRangeWithContext(file, view, 0, len(view.Text), dialect, directAggregateKind, nestedFunction, scriptVersion)
	file.Tokens = append(file.Tokens, view.Physical...)
	view.appendPendingNewline(file)
	return first
}

func (view *logicalView) appendPendingNewline(file *File) {
	if !view.hasPendingNewline {
		return
	}
	file.Tokens = append(file.Tokens, Token{Kind: TokenNewline, Span: view.pendingNewline})
	view.pendingNewline = Span{}
	view.hasPendingNewline = false
}

func scanLogicalCommandRange(file *File, view *logicalView, start, end int, dialect Dialect) int {
	return scanLogicalCommandRangeWithContext(file, view, start, end, dialect, "", false, 1)
}

func scanLogicalCommandRangeWithContext(file *File, view *logicalView, start, end int, dialect Dialect, directAggregateKind BlockKind, nestedFunction bool, scriptVersion uint8) int {
	temporary := &File{Dialect: dialect, Source: view.Text}
	scanCommandsWithContext(temporary, start, end, dialect, directAggregateKind, nestedFunction, scriptVersion)
	first := len(file.Commands)
	for index := range temporary.Commands {
		logical := temporary.Commands[index]
		mapped := logical
		mapped.Modifiers = append([]Modifier(nil), logical.Modifiers...)
		for index := range mapped.Modifiers {
			if logical.Modifiers[index].Filter != nil {
				filter := *logical.Modifiers[index].Filter
				mapped.Modifiers[index].Filter = &filter
			}
		}
		mapCommandHeader(view, &mapped)
		mapped.logical = &logicalCommandView{view: view, command: logical}
		file.Commands = append(file.Commands, mapped)
	}
	for _, token := range temporary.Tokens {
		token.Span = view.mapSpan(token.Span)
		file.Tokens = append(file.Tokens, token)
	}
	for _, diagnostic := range temporary.Diagnostics {
		diagnostic.Span = view.mapSpan(diagnostic.Span)
		file.Diagnostics = append(file.Diagnostics, diagnostic)
	}
	return first
}

func mapCommandHeader(view *logicalView, command *Command) {
	command.Span = view.mapSpan(command.Span)
	command.Range = mapOptionalLogicalSpan(view, command.Range)
	command.Name = mapOptionalLogicalSpan(view, command.Name)
	command.Bang = mapOptionalLogicalSpan(view, command.Bang)
	command.Count = mapOptionalLogicalSpan(view, command.Count)
	command.Argument = view.mapSpan(command.Argument)
	if command.Heredoc != nil {
		heredoc := *command.Heredoc
		heredoc.Header = mapOptionalLogicalSpan(view, heredoc.Header)
		command.Heredoc = &heredoc
	}
	for index := range command.Modifiers {
		modifier := &command.Modifiers[index]
		modifier.Span = view.mapSpan(modifier.Span)
		modifier.Bang = mapOptionalLogicalSpan(view, modifier.Bang)
		if modifier.Filter != nil {
			modifier.Filter.Delimiter = mapOptionalLogicalSpan(view, modifier.Filter.Delimiter)
			modifier.Filter.Pattern = mapOptionalLogicalSpan(view, modifier.Filter.Pattern)
			modifier.Filter.Flags = mapOptionalLogicalSpan(view, modifier.Filter.Flags)
		}
	}
}

func mapOptionalLogicalSpan(view *logicalView, span Span) Span {
	if span.Start == 0 && span.End == 0 {
		return span
	}
	return view.mapSpan(span)
}

func logicalArgumentText(file *File, command *Command) string {
	if command != nil && command.logical != nil {
		logical := command.logical
		return logical.view.Text[logical.command.Argument.Start:logical.command.Argument.End]
	}
	if file == nil || command == nil {
		return ""
	}
	return file.Text(command.Argument)
}

func extendVim9LogicalCommand(command *Command, source string, start, end, next int) {
	if command == nil || command.logical == nil || start > end {
		return
	}
	logical := command.logical
	logical.view.appendSynthetic('\n', start)
	logical.view.appendSource(source, start, end)
	logical.view.finishText()
	logical.view.Source.End = end
	logical.view.Next = next
}
