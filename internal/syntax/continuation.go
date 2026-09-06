package syntax

import (
	"strings"

	"github.com/neoclide/vimls-go/internal/vimdata"
)

// Allocate collection state only when a command actually gains an automatic
// continuation. Ordinary single-line and legacy commands keep the small header.
type vim9CommandCollection struct {
	continuation                       vim9ContinuationScan
	continuationEnd, continuationStart int
	boundaryScan                       vim9ArgumentScan
	opaqueScan                         *vim9OpaqueScan
	pendingBoundary                    bool
	declarationScanEnd                 int
	declarationHasAssignment           bool
}

// vim9ArgumentScan tracks the comment lexer's position independently from the
// continuation lexer: register names and numeric separators intentionally have
// different treatment in the existing boundary and continuation grammars.
type vim9ArgumentScan struct {
	interpolation vim9InterpolationScan
	depth         int
	ambiguous     bool
	start, next   int
	quote         byte
	initialized   bool
}

// scan returns the final physical line's comment and whether an Ex boundary
// may need the expression parser. Quoted bars do not require parsing prefixes.
func (scan *vim9ArgumentScan) scan(source string, start int) (comment int, bar bool) {
	if !scan.initialized || scan.start != start || scan.next > len(source) {
		*scan = vim9ArgumentScan{start: start, next: start, initialized: true}
	}
	comment = -1
	for scan.next < len(source) {
		index := scan.next
		c := source[index]
		if scan.quote == 0 && (c == '@' || c == '\'' && isDigitSeparator(source, index)) {
			scan.ambiguous = true
		}
		if c == '|' && scan.ambiguous {
			bar = true
		}
		if scan.quote != 0 {
			if c == '\\' && scan.quote == '"' {
				if index+1 == len(source) {
					return
				}
				scan.next += 2
				continue
			}
			if c == scan.quote {
				if scan.quote == '\'' && index+1 < len(source) && source[index+1] == '\'' {
					scan.next += 2
					continue
				}
				scan.quote = 0
			}
			scan.next++
			continue
		}
		if c == '\'' || c == '"' {
			scan.quote = c
			scan.next++
			continue
		}
		if c == '$' && index+1 < len(source) && (source[index+1] == '\'' || source[index+1] == '"') {
			if scan.interpolation.quote == 0 {
				scan.interpolation = vim9InterpolationScan{next: index + 2, quote: source[index+1]}
			}
			end, complete := scan.interpolation.scan(source)
			if !complete {
				return
			}
			scan.interpolation = vim9InterpolationScan{}
			scan.next = end
			continue
		}
		if c == '#' && isVim9OpaqueCommentStart(source, index, start, len(source)) {
			if newline := strings.IndexAny(source[index:], "\r\n"); newline >= 0 {
				scan.next = index + newline + 1
				continue
			}
			comment = index
			// Leave the comment opener pending: the next extension supplies its newline.
			return
		}
		switch c {
		case '(', '[', '{':
			scan.depth++
		case ')', ']', '}':
			if scan.depth > 0 {
				scan.depth--
			}
		}
		if c == '|' && scan.depth == 0 && (index == start || source[index-1] != '|') && (index+1 == len(source) || source[index+1] != '|') {
			bar = true
		}
		scan.next++
	}
	return
}

func commandVim9Continuation(file *File, command *Command) vim9ContinuationScan {
	logical := command.logical
	source := logicalArgumentText(file, command)
	collection := logical.collection
	if collection == nil {
		return scanVim9Continuation(source, vim9ContinuationScan{})
	}
	start, end := logical.command.Argument.Start, logical.command.Argument.End
	if collection.continuationStart != start || collection.continuationEnd < start || collection.continuationEnd > end {
		collection.continuation = vim9ContinuationScan{}
		collection.continuationEnd = start
	}
	collection.continuation = scanVim9ContinuationFrom(source, collection.continuationEnd-start, collection.continuation)
	collection.continuationStart, collection.continuationEnd = start, end
	return collection.continuation
}

func scanExtendedVim9Argument(logical *logicalCommandView, metadata vimdata.Command) (int, Span, Span, *expressionBoundary) {
	if logical.collection == nil {
		logical.collection = new(vim9CommandCollection)
	}
	collection := logical.collection
	source := logical.view.Text
	start, end := logical.command.Argument.Start, len(source)
	expressionStart := start
	supported := logical.command.Kind == CommandExpression || allowsMultipleExpressionArguments(metadata.Name) || vim9OneExpressionCommand(metadata.Name)
	switch metadata.Name {
	case "for":
		if collection.boundaryScan.initialized {
			expressionStart, supported = collection.boundaryScan.start, true
		} else if in := findTopLevelKeyword(source, start, end, "in"); in >= 0 {
			expressionStart, supported = skipExpressionSpace(source, in+2), true
		}
	case "put", "iput":
		if position := skipExpressionSpace(source, start); position < end && source[position] == '=' {
			expressionStart, supported = skipExpressionSpace(source, position+1), true
		}
	case "let", "var", "const", "final":
		// This search is needed only until the initializer starts; once initialized,
		// the comment cursor retains the RHS boundary for all following lines.
		if collection.boundaryScan.initialized {
			expressionStart = collection.boundaryScan.start
			supported = true
		} else {
			checked := max(start, collection.declarationScanEnd)
			if strings.ContainsRune(source[checked:end], '=') {
				assignment := findAssignment(source[start:end])
				if assignment.Start >= 0 && !strings.HasPrefix(source[start+assignment.Start:], "=<<") {
					expressionStart, supported = skipSpace(source, start+assignment.End, end), true
					collection.declarationHasAssignment = true
				}
			}
			collection.declarationScanEnd = end
		}
	}
	if supported {
		comment, bar := collection.boundaryScan.scan(source, expressionStart)
		if !bar {
			commentSpan := Span{}
			if comment >= 0 {
				end = comment
				commentSpan = Span{Start: comment, End: len(source)}
			}
			collection.pendingBoundary = true
			return trimSpaceEnd(source, start, end), Span{}, commentSpan, nil
		}
	}
	if !supported && (metadata.Name == "def" || metadata.Name == "function" || collection.declarationScanEnd == end && !collection.declarationHasAssignment) {
		if collection.opaqueScan == nil {
			collection.opaqueScan = &vim9OpaqueScan{next: start}
		}
		argumentEnd, separator, comment := collection.opaqueScan.scan(source, start, len(source), metadata)
		collection.pendingBoundary = false
		return argumentEnd, separator, comment, nil
	}
	collection.pendingBoundary = false
	return scanVim9CommandArgument(source, start, len(source), metadata, &logical.command)
}

func finalizeVim9Argument(command *Command) {
	logical := command.logical
	collection := logical.collection
	metadata := scanMetadataForParsedCommand(*command)
	end, _, _, boundary := scanVim9CommandArgument(logical.view.Text, logical.command.Argument.Start, len(logical.view.Text), metadata, &logical.command)
	logical.command.Argument.End = end
	logical.command.Span.End = max(end, logical.command.Name.End)
	logical.command.boundaryExpression = boundary
	command.Argument = logical.view.mapSpan(logical.command.Argument)
	command.Span = logical.view.mapSpan(logical.command.Span)
	command.boundaryExpression = boundary
	collection.pendingBoundary = false
}

// vim9InterpolationScan resumes the same string/interpolation grammar used by
// scanInterpolatedStringEnd, including an unfinished embedded expression.
// Physical extensions insert a newline, so doubled quotes/braces cannot grow
// retroactively across an already scanned physical boundary.
type vim9InterpolationScan struct {
	next, depth       int
	quote, innerQuote byte
}

func (scan *vim9InterpolationScan) scan(source string) (int, bool) {
	for scan.next < len(source) {
		index := scan.next
		c := source[index]
		if scan.depth > 0 {
			if scan.innerQuote != 0 {
				if c == '\\' && scan.innerQuote == '"' {
					scan.next += 2
					continue
				}
				if c == scan.innerQuote {
					if c == '\'' && index+1 < len(source) && source[index+1] == '\'' {
						scan.next += 2
						continue
					}
					scan.innerQuote = 0
				}
			} else if c == '\'' || c == '"' {
				scan.innerQuote = c
			} else if c == '{' {
				scan.depth++
			} else if c == '}' {
				scan.depth--
			}
			scan.next++
			continue
		}
		if c == '\\' && scan.quote == '"' {
			scan.next += 2
			continue
		}
		if c == scan.quote {
			if c == '\'' && index+1 < len(source) && source[index+1] == '\'' {
				scan.next += 2
				continue
			}
			scan.next++
			return scan.next, true
		}
		if c == '{' || c == '}' {
			if index+1 < len(source) && source[index+1] == c {
				scan.next += 2
				continue
			}
			if c == '{' {
				scan.depth = 1
			}
		}
		scan.next++
	}
	return len(source), false
}
