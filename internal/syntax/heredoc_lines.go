package syntax

import "strings"

// heredocLineIndex answers whether recovery must keep looking for a real
// terminator. Only the last position of each exact line is needed, so repeated
// missing markers share one source scan without retaining every occurrence.
type heredocLineIndex struct {
	lastLine            map[string]int
	lastCommandBlockEnd int
}

func indexHeredocLines(source string) *heredocLineIndex {
	index := &heredocLineIndex{lastLine: make(map[string]int), lastCommandBlockEnd: -1}
	for start := 0; start < len(source); {
		end, next := physicalLineEnd(source, start)
		line := source[start:end]
		index.lastLine[line] = start
		if commandBlockEndLine(line) {
			index.lastCommandBlockEnd = start
		}
		start = next
	}
	return index
}

type heredocMarker struct{ name, indent string }

func (marker heredocMarker) matches(line string) bool {
	return strings.TrimPrefix(line, marker.indent) == marker.name
}

func (index *heredocLineIndex) hasMarkerAfter(marker heredocMarker, start int) bool {
	if last, found := index.lastLine[marker.indent+marker.name]; found && last >= start {
		return true
	}
	// Vim permits an unindented marker unless it starts with the exact
	// command indentation, in which case that prefix would be stripped.
	if marker.indent != "" && !strings.HasPrefix(marker.name, marker.indent) {
		last, found := index.lastLine[marker.name]
		return found && last >= start
	}
	return false
}

func commandHeredocMarker(source string, command *Command) heredocMarker {
	marker := heredocMarker{name: command.Heredoc.Marker}
	if command.Heredoc.Trim {
		start := strings.LastIndexAny(source[:command.Span.Start], "\r\n") + 1
		end := start
		for end < command.Span.Start && isSpace(source[end]) {
			end++
		}
		marker.indent = source[start:end]
	}
	return marker
}
