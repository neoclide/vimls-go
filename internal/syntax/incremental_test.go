package syntax

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type incrementalEditCase struct {
	name        string
	old         string
	new         string
	expectReuse bool
}

type incrementalEditPosition uint8

const (
	incrementalEditHead incrementalEditPosition = iota
	incrementalEditMiddle
	incrementalEditEOF
)

type incrementalEditKind uint8

const (
	incrementalInsert incrementalEditKind = iota
	incrementalDelete
	incrementalEqualReplace
	incrementalLengthReplace
	incrementalSequence
)

type incrementalTextEdit struct {
	start       int
	oldEnd      int
	replacement string
}

type incrementalTextEditResult struct {
	edit incrementalTextEdit
	old  string
	new  string
}

type incrementalMatrixCase struct {
	name  string
	kind  incrementalEditKind
	edits []incrementalTextEdit
	whole bool
}

type incrementalTextPositionGroup struct {
	name     string
	position incrementalEditPosition
	source   string
	cases    []incrementalMatrixCase
}

func applyIncrementalTextEdit(source string, edit incrementalTextEdit) string {
	if edit.start < 0 || edit.start > edit.oldEnd || edit.oldEnd > len(source) {
		panic(fmt.Sprintf("invalid incremental edit %#v for %d-byte source", edit, len(source)))
	}
	return source[:edit.start] + edit.replacement + source[edit.oldEnd:]
}

func incrementalTextEditResults(source string, edits []incrementalTextEdit) []incrementalTextEditResult {
	results := make([]incrementalTextEditResult, 0, len(edits))
	for _, edit := range edits {
		next := applyIncrementalTextEdit(source, edit)
		results = append(results, incrementalTextEditResult{edit: edit, old: source, new: next})
		source = next
	}
	return results
}

func incrementalTextEditAt(source, oldText, replacement string) incrementalTextEdit {
	start := strings.Index(source, oldText)
	if start < 0 || strings.Index(source[start+1:], oldText) >= 0 {
		panic(fmt.Sprintf("incremental edit text %q is not unique", oldText))
	}
	return incrementalTextEdit{start: start, oldEnd: start + len(oldText), replacement: replacement}
}

func incrementalTextInsertAt(source, target, replacement string) incrementalTextEdit {
	edit := incrementalTextEditAt(source, target, "")
	edit.oldEnd = edit.start
	edit.replacement = replacement
	return edit
}

func incrementalFixedWidthSequence(source string, start, width int, replacements []string) []incrementalTextEdit {
	if start < 0 || start+width > len(source) {
		panic(fmt.Sprintf("invalid fixed-width sequence range %d:%d", start, start+width))
	}
	edits := make([]incrementalTextEdit, 0, len(replacements))
	for _, replacement := range replacements {
		if len(replacement) != width {
			panic(fmt.Sprintf("fixed-width sequence replacement %q has %d bytes, want %d", replacement, len(replacement), width))
		}
		edit := incrementalTextEdit{start: start, oldEnd: start + width, replacement: replacement}
		if source[start:edit.oldEnd] == replacement {
			panic(fmt.Sprintf("fixed-width sequence step does not change source at %d", start))
		}
		edits = append(edits, edit)
		source = applyIncrementalTextEdit(source, edit)
	}
	return edits
}

func newIncrementalMatrixCase(groupName, name string, kind incrementalEditKind, edits []incrementalTextEdit) incrementalMatrixCase {
	return incrementalMatrixCase{name: groupName + ": " + name, kind: kind, edits: edits}
}

var incrementalTextPositionGroups = func() []incrementalTextPositionGroup {
	head := "\ufefflet head = \"中😀e\u0301\"\t\nlet stable = 2\nlet tail = 3\n"
	middle := "let head = \"中😀e\u0301\"\t\r\nlet middle = 2\r\nlet tail = 3\r\n"
	eof := "let head = \"中😀e\u0301\"\t\nlet middle = 2\nlet tail = 3"
	byteSequence := make([]string, 100)
	for index := range byteSequence {
		byteSequence[index] = string('0' + byte(index%10))
	}
	utf8Sequence := make([]string, 100)
	for index := range utf8Sequence {
		utf8Sequence[index] = []string{"🚀", "😺", "🦄", "😀"}[index%4]
	}
	return []incrementalTextPositionGroup{
		{
			name:     "head",
			position: incrementalEditHead,
			source:   head,
			cases: []incrementalMatrixCase{
				newIncrementalMatrixCase("head", "insert", incrementalInsert, []incrementalTextEdit{{start: strings.Index(head, "中"), oldEnd: strings.Index(head, "中"), replacement: "文"}}),
				newIncrementalMatrixCase("head", "delete", incrementalDelete, []incrementalTextEdit{incrementalTextEditAt(head, "\u0301", "")}),
				newIncrementalMatrixCase("head", "equal-length replace", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(head, "😀", "🚀")}),
				newIncrementalMatrixCase("head", "length-changing replace", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAt(head, "e\u0301", "e")}),
				newIncrementalMatrixCase("head", "sequence", incrementalSequence, incrementalFixedWidthSequence(head, strings.Index(head, "😀"), len("😀"), utf8Sequence)),
				{name: "head: whole replacement", kind: incrementalLengthReplace, edits: []incrementalTextEdit{{start: 0, oldEnd: len(head), replacement: "\ufefflet whole = \"新😀e\u0301\"\t\n"}}, whole: true},
			},
		},
		{
			name:     "middle",
			position: incrementalEditMiddle,
			source:   middle,
			cases: []incrementalMatrixCase{
				newIncrementalMatrixCase("middle", "insert", incrementalInsert, []incrementalTextEdit{{start: strings.Index(middle, "\t") + len("\t"), oldEnd: strings.Index(middle, "\t") + len("\t"), replacement: "x"}}),
				newIncrementalMatrixCase("middle", "delete", incrementalDelete, []incrementalTextEdit{incrementalTextEditAt(middle, "middle", "")}),
				newIncrementalMatrixCase("middle", "equal-length replace", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(middle, "middle", "center")}),
				newIncrementalMatrixCase("middle", "length-changing replace", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAt(middle, "middle", "the-middle")}),
				newIncrementalMatrixCase("middle", "sequence", incrementalSequence, incrementalFixedWidthSequence(middle, strings.Index(middle, "2"), len("2"), byteSequence)),
			},
		},
		{
			name:     "EOF",
			position: incrementalEditEOF,
			source:   eof,
			cases: []incrementalMatrixCase{
				newIncrementalMatrixCase("EOF", "insert", incrementalInsert, []incrementalTextEdit{{start: len(eof), oldEnd: len(eof), replacement: "\n"}}),
				newIncrementalMatrixCase("EOF", "delete", incrementalDelete, []incrementalTextEdit{{start: len(eof) - 1, oldEnd: len(eof), replacement: ""}}),
				newIncrementalMatrixCase("EOF", "equal-length replace", incrementalEqualReplace, []incrementalTextEdit{{start: len(eof) - 1, oldEnd: len(eof), replacement: "4"}}),
				newIncrementalMatrixCase("EOF", "length-changing replace", incrementalLengthReplace, []incrementalTextEdit{{start: len(eof) - 1, oldEnd: len(eof), replacement: "42"}}),
				newIncrementalMatrixCase("EOF", "sequence", incrementalSequence, incrementalFixedWidthSequence(eof, len(eof)-1, len("3"), byteSequence)),
			},
		},
	}
}()

type incrementalDialectStateScenario struct {
	name   string
	tags   []string
	source string
	cases  []incrementalMatrixCase
}

var incrementalDialectStateScenarios = []incrementalDialectStateScenario{
	{
		name: "vim9script insert", tags: []string{"first effective vim9script insert"}, source: "let before = 1\nvar value = 2\n",
		cases: []incrementalMatrixCase{{name: "vim9script insert", kind: incrementalInsert, edits: []incrementalTextEdit{incrementalTextInsertAt("let before = 1\nvar value = 2\n", "let before", "vim9script\n")}}},
	},
	{
		name: "vim9script delete", tags: []string{"first effective vim9script delete"}, source: "vim9script\nvar value = 2\n",
		cases: []incrementalMatrixCase{{name: "vim9script delete", kind: incrementalDelete, edits: []incrementalTextEdit{incrementalTextEditAt("vim9script\nvar value = 2\n", "vim9script\n", "")}}},
	},
	{
		name: "vim9script move", tags: []string{"first effective vim9script move"}, source: "vim9script\n\" comment\nlet before = 1\nvar value = 2\n",
		cases: []incrementalMatrixCase{{name: "vim9script move", kind: incrementalSequence, edits: []incrementalTextEdit{
			incrementalTextEditAt("vim9script\n\" comment\nlet before = 1\nvar value = 2\n", "vim9script\n", ""),
			incrementalTextInsertAt("\" comment\nlet before = 1\nvar value = 2\n", "let before", "vim9script\n"),
		}}},
	},
	{
		name: "cross dialect blocks", tags: []string{"mismatched enddef", "mismatched endfunction"},
		source: "def LegacyDef()\n  let value = 1\nendfunction\nfunction Vim9Function()\n  var value = 1\nenddef\nlet after = 2\n",
		cases:  []incrementalMatrixCase{{name: "cross dialect blocks", kind: incrementalEqualReplace, edits: []incrementalTextEdit{incrementalTextEditAt("def LegacyDef()\n  let value = 1\nendfunction\nfunction Vim9Function()\n  var value = 1\nenddef\nlet after = 2\n", "let value = 1\nendfunction", "let value = 2\nendfunction")}}},
	},
	{
		name: "legacy root def", tags: []string{"legacy root def"}, source: "def LegacyDef()\n  let value = 1\nenddef\nlet after = 2\n",
		cases: []incrementalMatrixCase{{name: "legacy root def", kind: incrementalEqualReplace, edits: []incrementalTextEdit{incrementalTextEditAt("def LegacyDef()\n  let value = 1\nenddef\nlet after = 2\n", "let value = 1", "let value = 2")}}},
	},
	{
		name: "Vim9 root function", tags: []string{"Vim9 root function"}, source: "vim9script\nfunction Vim9Function()\n  var value = 1\nendfunction\nvar after = 2\n",
		cases: []incrementalMatrixCase{{name: "Vim9 root function", kind: incrementalEqualReplace, edits: []incrementalTextEdit{incrementalTextEditAt("vim9script\nfunction Vim9Function()\n  var value = 1\nendfunction\nvar after = 2\n", "var value = 1", "var value = 2")}}},
	},
	{
		name: "vim9cmd one-shot", tags: []string{"vim9cmd next command"}, source: "vim9cmd var oneShotVim9 = 1\nlet afterVim9cmd = 2\n",
		cases: []incrementalMatrixCase{{name: "vim9cmd one-shot", kind: incrementalEqualReplace, edits: []incrementalTextEdit{incrementalTextEditAt("vim9cmd var oneShotVim9 = 1\nlet afterVim9cmd = 2\n", "oneShotVim9 = 1", "oneShotVim9 = 2")}}},
	},
	{
		name: "legacy one-shot", tags: []string{"legacy next command"}, source: "vim9script\nlegacy let oneShotLegacy = 1\nvar afterLegacy = 2\n",
		cases: []incrementalMatrixCase{{name: "legacy one-shot", kind: incrementalEqualReplace, edits: []incrementalTextEdit{incrementalTextEditAt("vim9script\nlegacy let oneShotLegacy = 1\nvar afterLegacy = 2\n", "oneShotLegacy = 1", "oneShotLegacy = 2")}}},
	},
	{
		name: "scriptversion recovery", tags: []string{"scriptversion 1-4", "invalid scriptversion recovery"}, source: "scriptversion 1\nlet value = 1\nlet after = 2\n",
		cases: []incrementalMatrixCase{{name: "scriptversion recovery", kind: incrementalSequence, edits: incrementalFixedWidthSequence("scriptversion 1\nlet value = 1\nlet after = 2\n", len("scriptversion "), 1, []string{"2", "3", "4", "9", "4"})}},
	},
	{
		name: "length-changing scanner edit", tags: []string{"scanner length-changing replace"}, source: "let before = 1\nlet value = 2\nlet after = 3\n",
		cases: []incrementalMatrixCase{{name: "length-changing scanner edit", kind: incrementalLengthReplace, edits: []incrementalTextEdit{incrementalTextEditAt("let before = 1\nlet value = 2\nlet after = 3\n", "value = 2", "value = 200")}}},
	},
}

type incrementalCommandBoundaryScenario struct {
	name   string
	tags   []string
	source string
	cases  []incrementalMatrixCase
}

var incrementalCommandBoundaryScenarios = func() []incrementalCommandBoundaryScenario {
	forms := "silent! %foldclose!\n1delete_ | echo one\n2delete_ | echo two\nsetlocal ts=8 | echo set\nnmap lhs a\\|b | echo map\ns#foo|bar#baz# | echo subst\necho \"a|b\" | echo string\necho value \" comment | no\nlockvar 2 g:items\nlet afterForms = 1\n"
	ordinary := "echo ordinary | echo tail\nlet afterBars = 1\n"
	regexp := "syntax match Foo /foo|bar/ | echo afterRegexp\nlet afterBars = 1\n"
	mapping := "nmap lhs a\\|b | echo afterMapping\nlet afterBars = 1\n"
	substitute := "substitute /foo|bar/baz/ | echo afterSubstitute\nlet afterBars = 1\n"
	stringSource := "echo \"a|b\" | echo afterString\nlet afterBars = 1\n"
	comment := "echo value\n\" comment | not command\nlet afterBars = 1\n"
	leading := "vim9script\nautocmd BufNewFile *.match if ok\n  echo 'match'\nvar afterLeading = 1\n"
	leadingBar := "vim9script\nautocmd BufNewFile *.match if ok\n  | echo 'match'\nvar afterLeading = 1\n"
	legacyContinuation := "let value =\n\\ 1\nlet afterLegacyContinuation = 2\n"
	operator := "vim9script\nvar operator = 1 +\n  2\nvar afterOperator = 3\n"
	paren := "vim9script\nvar paren = (\n  1\n)\nvar afterParen = 3\n"
	ternary := "vim9script\nvar ternary = true ?\n  1 :\n  2\nvar afterTernary = 3\n"
	lambda := "vim9script\nvar lambda = (x) =>\n  x + 1\nvar afterLambda = 3\n"
	return []incrementalCommandBoundaryScenario{
		{
			name: "command forms", tags: []string{"range", "modifier", "abbreviation", "bang", "register", "count"}, source: forms,
			cases: []incrementalMatrixCase{
				newIncrementalMatrixCase("command forms", "range", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(forms, "%", "$")}),
				newIncrementalMatrixCase("command forms", "modifier", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAt(forms, "silent!", "silent")}),
				newIncrementalMatrixCase("command forms", "abbreviation", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAt(forms, "setlocal", "setl")}),
				newIncrementalMatrixCase("command forms", "bang", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAt(forms, "foldclose!", "foldclose")}),
				newIncrementalMatrixCase("command forms", "register", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAt(forms, "1delete_", "1delete a")}),
				newIncrementalMatrixCase("command forms", "count", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(forms, "lockvar 2", "lockvar 3")}),
				newIncrementalMatrixCase("command forms", "insert", incrementalInsert, []incrementalTextEdit{incrementalTextInsertAt(forms, "echo string", "silent ")}),
				newIncrementalMatrixCase("command forms", "delete", incrementalDelete, []incrementalTextEdit{incrementalTextEditAt(forms, "one", "")}),
				newIncrementalMatrixCase("command forms", "equal-length replace", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(forms, "foo", "bar")}),
				newIncrementalMatrixCase("command forms", "sequence", incrementalSequence, incrementalFixedWidthSequence(forms, strings.Index(forms, "foo"), len("foo"), []string{"bar", "foo"})),
			},
		},
		{name: "bar ordinary argument", tags: []string{"bar ordinary argument"}, source: ordinary, cases: []incrementalMatrixCase{newIncrementalMatrixCase("bar ordinary argument", "replace argument", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(ordinary, "ordinary", "standard")})}},
		{name: "bar regexp", tags: []string{"bar regexp"}, source: regexp, cases: []incrementalMatrixCase{newIncrementalMatrixCase("bar regexp", "replace pattern", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(regexp, "foo|bar", "foo|baz")})}},
		{name: "bar mapping", tags: []string{"bar mapping"}, source: mapping, cases: []incrementalMatrixCase{newIncrementalMatrixCase("bar mapping", "replace lhs", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(mapping, "a\\|b", "a\\|c")})}},
		{name: "bar substitute", tags: []string{"bar substitute"}, source: substitute, cases: []incrementalMatrixCase{newIncrementalMatrixCase("bar substitute", "replace pattern", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(substitute, "foo|bar", "foo|baz")})}},
		{name: "bar string", tags: []string{"bar string"}, source: stringSource, cases: []incrementalMatrixCase{newIncrementalMatrixCase("bar string", "replace string", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(stringSource, "a|b", "a|c")})}},
		{name: "bar comment", tags: []string{"bar comment"}, source: comment, cases: []incrementalMatrixCase{newIncrementalMatrixCase("bar comment", "replace comment", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAt(comment, "comment | not command", "comment | still comment")})}},
		{
			name: "leading bar appears", tags: []string{"leading bar continuation appears"}, source: leading,
			cases: []incrementalMatrixCase{{name: "leading bar appears", kind: incrementalInsert, edits: []incrementalTextEdit{incrementalTextInsertAt(leading, "echo 'match'", "| ")}}},
		},
		{
			name: "leading bar disappears", tags: []string{"leading bar continuation disappears"}, source: leadingBar,
			cases: []incrementalMatrixCase{{name: "leading bar disappears", kind: incrementalDelete, edits: []incrementalTextEdit{incrementalTextEditAt(leadingBar, "|", "")}}},
		},
		{
			name: "legacy continuation", tags: []string{"legacy backslash continuation"}, source: legacyContinuation,
			cases: []incrementalMatrixCase{{name: "legacy continuation", kind: incrementalDelete, edits: []incrementalTextEdit{incrementalTextEditAt(legacyContinuation, "\\ ", "")}}},
		},
		{name: "Vim9 operator continuation", tags: []string{"Vim9 operator continuation"}, source: operator, cases: []incrementalMatrixCase{newIncrementalMatrixCase("Vim9 operator continuation", "replace operator", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(operator, "+", "-")})}},
		{name: "Vim9 parenthesis continuation", tags: []string{"Vim9 parenthesis continuation"}, source: paren, cases: []incrementalMatrixCase{newIncrementalMatrixCase("Vim9 parenthesis continuation", "replace opening delimiter", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(paren, "(", "[")})}},
		{name: "Vim9 ternary continuation", tags: []string{"Vim9 ternary continuation"}, source: ternary, cases: []incrementalMatrixCase{newIncrementalMatrixCase("Vim9 ternary continuation", "replace operator", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(ternary, "?", "+")})}},
		{name: "Vim9 lambda continuation", tags: []string{"Vim9 lambda continuation"}, source: lambda, cases: []incrementalMatrixCase{newIncrementalMatrixCase("Vim9 lambda continuation", "replace arrow", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(lambda, "=>", "->")})}},
	}
}()

func incrementalTextEditAtOffset(source string, start int, oldText, replacement string) incrementalTextEdit {
	if start < 0 || start+len(oldText) > len(source) || source[start:start+len(oldText)] != oldText {
		panic(fmt.Sprintf("incremental edit offset %d does not contain %q", start, oldText))
	}
	return incrementalTextEdit{start: start, oldEnd: start + len(oldText), replacement: replacement}
}

var incrementalHeredocOwnerScenarios = func() []incrementalCommandBoundaryScenario {
	complete := "let text =<< END\nbody A01\nEND\nlet afterHeredoc = 1\n"
	incomplete := "let text =<< END\nbody A01\nlet afterHeredoc = 1\n"
	headerEnd := strings.Index(complete, "<< END") + len("<< ")
	markerEnd := strings.LastIndex(complete, "END\n")
	return []incrementalCommandBoundaryScenario{
		{
			name: "complete heredoc", tags: []string{"complete heredoc", "heredoc header marker", "heredoc body", "heredoc end marker"}, source: complete,
			cases: []incrementalMatrixCase{
				newIncrementalMatrixCase("complete heredoc", "header marker", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAtOffset(complete, headerEnd, "END", "FIN")}),
				newIncrementalMatrixCase("complete heredoc", "body insert", incrementalInsert, []incrementalTextEdit{incrementalTextInsertAt(complete, "A01", "X")}),
				newIncrementalMatrixCase("complete heredoc", "body delete", incrementalDelete, []incrementalTextEdit{incrementalTextEditAt(complete, "A01", "")}),
				newIncrementalMatrixCase("complete heredoc", "body replace", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(complete, "A01", "B02")}),
				newIncrementalMatrixCase("complete heredoc", "body length replace", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAt(complete, "A01", "A010")}),
				newIncrementalMatrixCase("complete heredoc", "end marker", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAtOffset(complete, markerEnd, "END", "FIN")}),
				newIncrementalMatrixCase("complete heredoc", "marker rename sequence", incrementalSequence, []incrementalTextEdit{incrementalTextEditAtOffset(complete, headerEnd, "END", "FIN"), incrementalTextEditAtOffset(complete, markerEnd, "END", "FIN")}),
			},
		},
		{
			name: "incomplete heredoc", tags: []string{"incomplete heredoc", "heredoc body to EOF"}, source: incomplete,
			cases: []incrementalMatrixCase{
				newIncrementalMatrixCase("incomplete heredoc", "body replace", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(incomplete, "A01", "B02")}),
			},
		},
	}
}()

var incrementalTextBodyOwnerScenarios = func() []incrementalCommandBoundaryScenario {
	appendSource := "append\nbody A01\n.\nlet afterAppend = 1\n"
	changeSource := "change\nbody A01\n.\nlet afterChange = 1\n"
	insertSource := "insert\nbody A01\n.\nlet afterInsert = 1\n"
	dotSource := "append\nbody A01\n.\nlet afterDot = 1\n"
	keymapSource := "loadkeymap\na A01\nb B02\n"
	dotMarker := strings.Index(dotSource, ".\n")
	return []incrementalCommandBoundaryScenario{
		{name: "append text body", tags: []string{"append", "text body"}, source: appendSource, cases: []incrementalMatrixCase{newIncrementalMatrixCase("append text body", "body insert", incrementalInsert, []incrementalTextEdit{incrementalTextInsertAt(appendSource, "A01", "X")})}},
		{name: "change text body", tags: []string{"change", "text body"}, source: changeSource, cases: []incrementalMatrixCase{newIncrementalMatrixCase("change text body", "body delete", incrementalDelete, []incrementalTextEdit{incrementalTextEditAt(changeSource, "A01", "")})}},
		{name: "insert text body", tags: []string{"insert", "text body"}, source: insertSource, cases: []incrementalMatrixCase{newIncrementalMatrixCase("insert text body", "body replace", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(insertSource, "A01", "B02")})}},
		{name: "dot terminator", tags: []string{"dot terminator", "dot recovery"}, source: dotSource, cases: []incrementalMatrixCase{newIncrementalMatrixCase("dot terminator", "break and restore", incrementalSequence, []incrementalTextEdit{incrementalTextEditAtOffset(dotSource, dotMarker, ".", "!"), incrementalTextEditAtOffset(dotSource, dotMarker, ".", ".")})}},
		{name: "loadkeymap body", tags: []string{"loadkeymap", "keymap body to EOF"}, source: keymapSource, cases: []incrementalMatrixCase{newIncrementalMatrixCase("loadkeymap body", "body length replace", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAt(keymapSource, "A01", "ASCII")})}},
	}
}()

var incrementalEmbeddedOwnerScenarios = func() []incrementalCommandBoundaryScenario {
	vim9Command := "vim9script\ncommand Foo {\n  echo 'hello'\n}\nvar afterCommand = 1\n"
	autocmdBlock := "autocmd BufEnter * {\n  var value = 1\n}\nlet afterAutocmd = 1\n"
	global := "g/foo/ echo one | v/bar/ echo two | s/x/y/\nlet afterGlobal = 1\n"
	listDo := "ldo echo one\nlet afterListDo = 1\n"
	legacyBlock := "windo if cond\n  echo value\nendif\nlet afterLegacyBlock = 1\n"
	nestedNear := "windo " + strings.Repeat("windo ", maxEmbeddedCommandDepth-1) + "edit file.txt\nlet afterNestedNear = 1\n"
	nestedOver := "windo " + strings.Repeat("windo ", maxEmbeddedCommandDepth) + "edit file.txt\nlet afterNestedOver = 1\n"
	directFinish := "let before = 1\nfinish | echo 'dead'\nHELP TEXT *tag*\n"
	conditionalFinish := "if !has('vim9script')\n  finish\nendif\nvim9script\nvar value = 1\n"
	return []incrementalCommandBoundaryScenario{
		{name: "Vim9 command block", tags: []string{"Vim9 command block"}, source: vim9Command, cases: []incrementalMatrixCase{newIncrementalMatrixCase("Vim9 command block", "payload insert", incrementalInsert, []incrementalTextEdit{incrementalTextInsertAt(vim9Command, "hello", "hi ")})}},
		{name: "autocmd block", tags: []string{"autocmd block"}, source: autocmdBlock, cases: []incrementalMatrixCase{newIncrementalMatrixCase("autocmd block", "payload delete", incrementalDelete, []incrementalTextEdit{incrementalTextEditAt(autocmdBlock, "value", "")})}},
		{name: "global/vglobal", tags: []string{"global/vglobal"}, source: global, cases: []incrementalMatrixCase{newIncrementalMatrixCase("global/vglobal", "payload replace", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(global, "one", "two")})}},
		{name: "list-do", tags: []string{"list-do"}, source: listDo, cases: []incrementalMatrixCase{newIncrementalMatrixCase("list-do", "payload length replace", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAt(listDo, "one", "three")})}},
		{name: "Legacy embedded block", tags: []string{"Legacy embedded block"}, source: legacyBlock, cases: []incrementalMatrixCase{newIncrementalMatrixCase("Legacy embedded block", "payload sequence", incrementalSequence, incrementalFixedWidthSequence(legacyBlock, strings.Index(legacyBlock, "value"), len("value"), []string{"other", "value"}))}},
		{name: "nested embedded depth near", tags: []string{"nested embedded depth near"}, source: nestedNear, cases: []incrementalMatrixCase{newIncrementalMatrixCase("nested embedded depth near", "payload replace", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(nestedNear, "file.txt", "file.log")})}},
		{name: "nested embedded depth over", tags: []string{"nested embedded depth over"}, source: nestedOver, cases: []incrementalMatrixCase{newIncrementalMatrixCase("nested embedded depth over", "payload replace", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(nestedOver, "file.txt", "file.log")})}},
		{name: "direct finish", tags: []string{"direct finish", "OpaqueTail"}, source: directFinish, cases: []incrementalMatrixCase{newIncrementalMatrixCase("direct finish", "opaque tail replace", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(directFinish, "dead", "live")})}},
		{name: "conditional finish", tags: []string{"conditional finish"}, source: conditionalFinish, cases: []incrementalMatrixCase{newIncrementalMatrixCase("conditional finish", "following declaration length replace", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAt(conditionalFinish, "value", "values")})}},
	}
}()

var incrementalEditCases = []incrementalEditCase{
	{name: "legacy tail", old: "let first = 1\nlet last = 2\n", new: "let first = 1\nlet last = 20\n"},
	{name: "vim9 middle", old: "vim9script\nvar first = 1\nvar last = 2\n", new: "vim9script\nvar first = 10\nvar last = 2\n"},
	{name: "dialect", old: "vim9script\nvar value = 1\n", new: "var value = 1\n"},
	{name: "continuation", old: "vim9script\nvar value = [1,\n  2]\necho value\n", new: "vim9script\nvar value = [1,\n  20]\necho value\n"},
	{name: "heredoc", old: "let value =<< END\none\nEND\necho value\n", new: "let value =<< END\ntwo\nEND\necho value\n"},
	{name: "finish", old: "let before = 1\nfinish\ndead\n", new: "let before = 2\nfinish\ndead\n"},
}

var incrementalBoundaryEditCases = []incrementalEditCase{
	{name: "head insertion", old: "let one = 1\nlet stable = 2\nlet tail = 3\n", new: "let one = 10\nlet stable = 2\nlet tail = 3\n", expectReuse: true},
	{name: "head deletion", old: "let one = 10\nlet stable = 2\nlet tail = 3\n", new: "let one = 1\nlet stable = 2\nlet tail = 3\n", expectReuse: true},
	{name: "equal replacement", old: "let one = 1\nlet stable = 2\nlet tail = 3\n", new: "let one = 2\nlet stable = 2\nlet tail = 3\n", expectReuse: true},
	{name: "variable replacement", old: "let one = 1\nlet stable = 2\nlet tail = 3\n", new: "let one = 123\nlet stable = 2\nlet tail = 3\n", expectReuse: true},
	{name: "UTF-8 length change", old: "let greeting = \"中\"\nlet stable = 2\nlet tail = 3\n", new: "let greeting = \"中文\"\nlet stable = 2\nlet tail = 3\n", expectReuse: true},
	{name: "astral replacement", old: "let mark = \"😀\"\nlet stable = 2\nlet tail = 3\n", new: "let mark = \"🚀\"\nlet stable = 2\nlet tail = 3\n", expectReuse: true},
	{name: "combining replacement", old: "let name = \"e\u0301\"\nlet stable = 2\nlet tail = 3\n", new: "let name = \"e\u0301\u0302\"\nlet stable = 2\nlet tail = 3\n", expectReuse: true},
	{name: "CRLF middle", old: "let one = 1\r\nlet two = 2\r\nlet stable = 3\r\nlet tail = 4\r\n", new: "let one = 1\r\nlet two = 20\r\nlet stable = 3\r\nlet tail = 4\r\n", expectReuse: true},
	{name: "BOM insertion", old: "let one = 1\nlet stable = 2\nlet tail = 3\n", new: "\ufefflet one = 1\nlet stable = 2\nlet tail = 3\n", expectReuse: true},
	{name: "BOM deletion", old: "\ufefflet one = 1\nlet stable = 2\nlet tail = 3\n", new: "let one = 1\nlet stable = 2\nlet tail = 3\n", expectReuse: true},
	{name: "EOF without trailing newline", old: "let one = 1\nlet two = 2", new: "let one = 1\nlet two = 20"},
}

func TestIncrementalEditMatrixTextPosition(t *testing.T) {
	seenNames := make(map[string]bool)
	seenKinds := make(map[incrementalEditPosition]map[incrementalEditKind]bool)
	contexts := map[string]bool{"LF": false, "CRLF": false, "BOM": false, "no trailing newline": false, "UTF-8": false, "astral": false, "combining": false, "tab": false}
	for _, group := range incrementalTextPositionGroups {
		seenKinds[group.position] = make(map[incrementalEditKind]bool)
		contexts["LF"] = contexts["LF"] || strings.Contains(group.source, "\n")
		contexts["CRLF"] = contexts["CRLF"] || strings.Contains(group.source, "\r\n")
		contexts["BOM"] = contexts["BOM"] || strings.HasPrefix(group.source, "\ufeff")
		contexts["no trailing newline"] = contexts["no trailing newline"] || !strings.HasSuffix(group.source, "\n")
		if !strings.Contains(group.source, "中") || !strings.Contains(group.source, "😀") || !strings.Contains(group.source, "e\u0301") || !strings.Contains(group.source, "\t") {
			t.Fatalf("%s source does not cover required text boundaries: %q", group.name, group.source)
		}
		for _, test := range group.cases {
			if seenNames[test.name] {
				t.Fatalf("duplicate matrix case name %q", test.name)
			}
			seenNames[test.name] = true
			seenKinds[group.position][test.kind] = true
			if len(test.edits) == 0 {
				t.Fatalf("%s has no executable edits", test.name)
			}
			results := incrementalTextEditResults(group.source, test.edits)
			if test.whole {
				if len(results) != 1 || results[0].old == results[0].new || results[0].edit.start != 0 || results[0].edit.oldEnd != len(group.source) {
					t.Fatalf("%s is not a whole-document replacement: %#v", test.name, test.edits)
				}
				continue
			}
			if test.kind == incrementalSequence && len(results) != 100 {
				t.Fatalf("%s has %d sequence steps, want 100", test.name, len(results))
			}
			for _, result := range results {
				if result.old == result.new {
					t.Fatalf("%s has an unchanged edit: %#v", test.name, result.edit)
				}
				edit := result.edit
				old := result.old
				if !incrementalTextEditInPosition(group.position, old, edit) {
					t.Fatalf("%s edit is outside its position group: %#v", test.name, edit)
				}
				for _, boundary := range []struct {
					name string
					text string
				}{
					{"UTF-8", "中"}, {"astral", "😀"}, {"combining", "\u0301"},
				} {
					if editTouchesIncrementalText(old, edit, boundary.text) {
						contexts[boundary.name] = true
					}
				}
				if tab := strings.Index(old, "\t"); tab >= 0 && edit.start == tab+len("\t") {
					contexts["tab"] = true
				}
				if !incrementalTextEditKindValid(test.kind, edit) {
					t.Fatalf("%s does not match edit kind %d: %#v", test.name, test.kind, edit)
				}
			}
		}
	}
	for name, present := range contexts {
		if !present {
			t.Fatalf("matrix context %q is not represented by a source or edit range", name)
		}
	}
	if len(seenNames) != 16 {
		t.Fatalf("matrix has %d unique cases, want 16", len(seenNames))
	}
	for _, position := range []incrementalEditPosition{incrementalEditHead, incrementalEditMiddle, incrementalEditEOF} {
		if len(seenKinds[position]) != 5 {
			t.Fatalf("position %d expresses %d edit kinds, want 5", position, len(seenKinds[position]))
		}
	}
}

func incrementalTextEditInPosition(position incrementalEditPosition, source string, edit incrementalTextEdit) bool {
	if edit.start < 0 || edit.start > edit.oldEnd || edit.oldEnd > len(source) {
		return false
	}
	switch position {
	case incrementalEditHead:
		return edit.start <= len(source)/2
	case incrementalEditMiddle:
		return edit.start > len(source)/4 && edit.start < len(source)*3/4
	case incrementalEditEOF:
		return edit.start >= len(source)-1
	default:
		return false
	}
}

func incrementalTextEditKindValid(kind incrementalEditKind, edit incrementalTextEdit) bool {
	oldLength := edit.oldEnd - edit.start
	switch kind {
	case incrementalInsert:
		return oldLength == 0 && edit.replacement != ""
	case incrementalDelete:
		return oldLength > 0 && edit.replacement == ""
	case incrementalEqualReplace:
		return oldLength > 0 && oldLength == len(edit.replacement)
	case incrementalLengthReplace:
		return oldLength > 0 && oldLength != len(edit.replacement)
	case incrementalSequence:
		return oldLength > 0 && oldLength == len(edit.replacement)
	default:
		return false
	}
}

func editTouchesIncrementalText(source string, edit incrementalTextEdit, text string) bool {
	start := strings.Index(source, text)
	return start >= 0 && edit.start <= start && edit.oldEnd >= start
}

func TestReparseTextPositionMatrix(t *testing.T) {
	for _, group := range incrementalTextPositionGroups {
		for _, test := range group.cases {
			t.Run(test.name, func(t *testing.T) {
				runIncrementalMatrix(t, group.source, test)
			})
		}
	}
}

func TestIncrementalEditMatrixDialectState(t *testing.T) {
	tags := make(map[string]bool)
	kinds := make(map[incrementalEditKind]bool)
	names := make(map[string]bool)
	for _, scenario := range incrementalDialectStateScenarios {
		for _, tag := range scenario.tags {
			tags[tag] = true
		}
		for _, test := range scenario.cases {
			if names[test.name] {
				t.Fatalf("duplicate dialect matrix case %q", test.name)
			}
			names[test.name], kinds[test.kind] = true, true
			results := incrementalTextEditResults(scenario.source, test.edits)
			if len(results) == 0 {
				t.Fatalf("%s has no edits", test.name)
			}
			if test.kind == incrementalSequence && len(results) < 2 {
				t.Fatalf("%s sequence has %d steps, want at least 2", test.name, len(results))
			}
			for _, result := range results {
				if result.old == result.new || result.edit.start < 0 || result.edit.start > result.edit.oldEnd || result.edit.oldEnd > len(result.old) {
					t.Fatalf("%s has invalid or unchanged step: %#v", test.name, result)
				}
				if test.kind != incrementalSequence && !incrementalTextEditKindValid(test.kind, result.edit) || test.kind == incrementalSequence && !incrementalSequenceStepValid(result.edit) {
					t.Fatalf("%s step does not match edit kind %d: %#v", test.name, test.kind, result.edit)
				}
			}
			if scenario.name == "vim9script move" {
				lines := strings.Split(results[len(results)-1].new, "\n")
				if len(lines) < 4 || strings.TrimSpace(lines[0]) != `" comment` || strings.TrimSpace(lines[1]) != "vim9script" || strings.TrimSpace(lines[2]) != "let before = 1" {
					t.Fatalf("vim9script was not moved to the first effective command: %q", results[len(results)-1].new)
				}
			}
			if scenario.name == "scriptversion recovery" {
				want := []string{"2", "3", "4", "9", "4"}
				if len(results) != len(want) {
					t.Fatalf("scriptversion steps = %d, want %d", len(results), len(want))
				}
				for index, result := range results {
					fields := strings.Fields(strings.Split(result.new, "\n")[0])
					if len(fields) != 2 || fields[0] != "scriptversion" || fields[1] != want[index] {
						t.Fatalf("scriptversion step %d = %q, want %s", index, fields, want[index])
					}
				}
			}
		}
	}
	for _, tag := range []string{"first effective vim9script insert", "first effective vim9script delete", "first effective vim9script move", "legacy root def", "Vim9 root function", "vim9cmd next command", "legacy next command", "scriptversion 1-4", "invalid scriptversion recovery", "mismatched enddef", "mismatched endfunction"} {
		if !tags[tag] {
			t.Fatalf("missing dialect scenario %q", tag)
		}
	}
	if len(kinds) != 5 {
		t.Fatalf("dialect matrix has %d edit kinds, want 5", len(kinds))
	}
}

func TestDialectStateASTRecovery(t *testing.T) {
	for version := byte('1'); version <= '4'; version++ {
		file := Parse(fmt.Sprintf("scriptversion %c\nlet after = 2\n", version))
		if got := incrementalDeclaration(t, file, "after"); got.ScriptVersion != version-'0' {
			t.Fatalf("scriptversion %c after command version = %d", version, got.ScriptVersion)
		}
	}
	invalid := Parse("scriptversion 9\nlet after = 2\n")
	if got := incrementalDeclaration(t, invalid, "after"); got.ScriptVersion != 1 {
		t.Fatalf("invalid scriptversion after command version = %d, want 1", got.ScriptVersion)
	}
}

func TestIncrementalTextEditAtRejectsOverlappingMatches(t *testing.T) {
	for _, test := range []struct{ source, target string }{{"aaa", "aa"}, {"aaaa", "aaa"}} {
		t.Run(test.source+"/"+test.target, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("accepted overlapping target %q in %q", test.target, test.source)
				}
			}()
			_ = incrementalTextEditAt(test.source, test.target, "x")
		})
	}
}

func incrementalSequenceStepValid(edit incrementalTextEdit) bool {
	for _, kind := range []incrementalEditKind{incrementalInsert, incrementalDelete, incrementalEqualReplace, incrementalLengthReplace} {
		if incrementalTextEditKindValid(kind, edit) {
			return true
		}
	}
	return false
}

func TestReparseDialectStateMatrix(t *testing.T) {
	for _, scenario := range incrementalDialectStateScenarios {
		for _, test := range scenario.cases {
			t.Run(test.name, func(t *testing.T) {
				got := runIncrementalMatrix(t, scenario.source, test)
				switch scenario.name {
				case "vim9script insert", "vim9script move":
					if got.Dialect != Vim9 {
						t.Fatalf("dialect after %s = %v, want Vim9", scenario.name, got.Dialect)
					}
				case "vim9script delete":
					if got.Dialect != Legacy {
						t.Fatalf("dialect after delete = %v, want Legacy", got.Dialect)
					}
				case "vim9cmd one-shot":
					vim9 := incrementalDeclaration(t, got, "oneShotVim9")
					after := incrementalDeclaration(t, got, "afterVim9cmd")
					if vim9.Dialect != Vim9 || after.Dialect != Legacy {
						t.Fatalf("vim9cmd dialects = %v, %v", vim9.Dialect, after.Dialect)
					}
				case "legacy one-shot":
					legacy := incrementalDeclaration(t, got, "oneShotLegacy")
					after := incrementalDeclaration(t, got, "afterLegacy")
					if legacy.Dialect != Legacy || after.Dialect != Vim9 {
						t.Fatalf("legacy dialects = %v, %v", legacy.Dialect, after.Dialect)
					}
				case "cross dialect blocks":
					incrementalDeclaration(t, got, "after")
				}
			})
		}
	}
}

func TestIncrementalEditMatrixCommandBoundary(t *testing.T) {
	tags, kinds, names := map[string]bool{}, map[incrementalEditKind]bool{}, map[string]bool{}
	for _, scenario := range incrementalCommandBoundaryScenarios {
		for _, tag := range scenario.tags {
			tags[tag] = true
		}
		for _, test := range scenario.cases {
			if names[test.name] {
				t.Fatalf("duplicate command-boundary case %q", test.name)
			}
			names[test.name], kinds[test.kind] = true, true
			results := incrementalTextEditResults(scenario.source, test.edits)
			if len(results) == 0 || test.kind == incrementalSequence && len(results) < 2 {
				t.Fatalf("%s has insufficient edits: %d", test.name, len(results))
			}
			for _, result := range results {
				if result.old == result.new || result.edit.start < 0 || result.edit.start > result.edit.oldEnd || result.edit.oldEnd > len(result.old) {
					t.Fatalf("%s has invalid step: %#v", test.name, result)
				}
				valid := incrementalTextEditKindValid(test.kind, result.edit)
				if test.kind == incrementalSequence {
					valid = incrementalSequenceStepValid(result.edit)
				}
				if !valid {
					t.Fatalf("%s step does not match kind %d: %#v", test.name, test.kind, result.edit)
				}
			}
		}
	}
	for _, tag := range []string{"range", "modifier", "abbreviation", "bang", "register", "count", "bar ordinary argument", "bar regexp", "bar mapping", "bar substitute", "bar string", "bar comment", "leading bar continuation appears", "leading bar continuation disappears", "legacy backslash continuation", "Vim9 operator continuation", "Vim9 parenthesis continuation", "Vim9 ternary continuation", "Vim9 lambda continuation"} {
		if !tags[tag] {
			t.Fatalf("missing command-boundary scenario %q", tag)
		}
	}
	if len(kinds) != 5 {
		t.Fatalf("command-boundary matrix has %d edit kinds, want 5", len(kinds))
	}
}

func TestReparseCommandBoundaryMatrix(t *testing.T) {
	for _, scenario := range incrementalCommandBoundaryScenarios {
		for _, test := range scenario.cases {
			t.Run(test.name, func(t *testing.T) { runIncrementalMatrix(t, scenario.source, test) })
		}
	}
}

func TestCommandBoundaryMatrixASTRecovery(t *testing.T) {
	for _, scenario := range incrementalCommandBoundaryScenarios {
		for _, test := range scenario.cases {
			got := runIncrementalMatrix(t, scenario.source, test)
			switch scenario.name {
			case "command forms":
				canonical := make([]string, len(got.Commands))
				for index := range got.Commands {
					canonical[index] = got.Commands[index].Canonical
				}
				if len(canonical) != 16 || !slices.Equal(canonical, []string{"foldclose", "delete", "echo", "delete", "echo", "setlocal", "echo", "nmap", "echo", "substitute", "echo", "echo", "echo", "echo", "lockvar", "let"}) {
					t.Fatalf("command forms canonical = %#v", canonical)
				}
				first := got.Commands[0]
				switch strings.TrimPrefix(test.name, "command forms: ") {
				case "range":
					if got.Text(first.Range) != "$" {
						t.Fatalf("range = %q", got.Text(first.Range))
					}
				case "modifier":
					if len(first.Modifiers) != 1 || first.Modifiers[0].Name != "silent" || got.Text(first.Modifiers[0].Bang) != "" {
						t.Fatalf("modifier = %#v", first.Modifiers)
					}
				case "abbreviation":
					if got.Commands[5].TypedName != "setl" || got.Commands[5].Canonical != "setlocal" {
						t.Fatalf("abbreviation command = %#v", got.Commands[5])
					}
				case "bang":
					if first.Canonical != "foldclose" || got.Text(first.Bang) != "" {
						t.Fatalf("command bang = %#v", first)
					}
				case "register":
					if got.Commands[1].Canonical != "delete" || got.Text(got.Commands[1].Argument) != "a" {
						t.Fatalf("delete register = %#v", got.Commands[1])
					}
				case "count":
					if got.Text(got.Commands[14].Count) != "3" {
						t.Fatalf("lockvar count = %q", got.Text(got.Commands[14].Count))
					}
				case "insert", "delete", "equal-length replace", "sequence":
					if first.Canonical != "foldclose" {
						t.Fatalf("command forms first command = %#v", first)
					}
				}
				if strings.TrimPrefix(test.name, "command forms: ") == "range" && got.Text(first.Range) != "$" {
					t.Fatalf("range edit was not retained: %#v", first)
				}
				if first.Canonical != "foldclose" {
					t.Fatalf("foldclose bang = %#v", first)
				}
				incrementalDeclaration(t, got, "afterForms")
			case "bar ordinary argument":
				if gotCommands := []string{got.Commands[0].Canonical, got.Commands[1].Canonical, got.Commands[2].Canonical}; !slices.Equal(gotCommands, []string{"echo", "echo", "let"}) || countTokens(got, TokenSeparator) != 1 || got.Text(got.Commands[0].Argument) != "standard" || got.Text(got.Commands[1].Argument) != "tail" {
					t.Fatalf("ordinary bar commands = %#v", got.Commands)
				}
				incrementalDeclaration(t, got, "afterBars")
			case "bar regexp":
				if gotCommands := []string{got.Commands[0].Canonical, got.Commands[1].Canonical, got.Commands[2].Canonical}; !slices.Equal(gotCommands, []string{"syntax", "echo", "let"}) || countTokens(got, TokenSeparator) != 1 || got.Text(got.Commands[0].Argument) != "match Foo /foo|baz/" {
					t.Fatalf("regexp bar commands = %#v", got.Commands)
				}
				incrementalDeclaration(t, got, "afterBars")
			case "bar mapping":
				if gotCommands := []string{got.Commands[0].Canonical, got.Commands[1].Canonical, got.Commands[2].Canonical}; !slices.Equal(gotCommands, []string{"nmap", "echo", "let"}) || countTokens(got, TokenSeparator) != 1 || got.Text(got.Commands[0].Argument) != "lhs a\\|c " {
					t.Fatalf("mapping bar commands = %#v", got.Commands)
				}
				incrementalDeclaration(t, got, "afterBars")
			case "bar substitute":
				if gotCommands := []string{got.Commands[0].Canonical, got.Commands[1].Canonical, got.Commands[2].Canonical}; !slices.Equal(gotCommands, []string{"substitute", "echo", "let"}) || countTokens(got, TokenSeparator) != 1 || got.Text(got.Commands[0].Argument) != "/foo|baz/baz/" {
					t.Fatalf("substitute bar commands = %#v", got.Commands)
				}
				incrementalDeclaration(t, got, "afterBars")
			case "bar string":
				if gotCommands := []string{got.Commands[0].Canonical, got.Commands[1].Canonical, got.Commands[2].Canonical}; !slices.Equal(gotCommands, []string{"echo", "echo", "let"}) || countTokens(got, TokenSeparator) != 1 || got.Text(got.Commands[0].Argument) != "\"a|c\"" {
					t.Fatalf("string bar commands = %#v", got.Commands)
				}
				incrementalDeclaration(t, got, "afterBars")
			case "bar comment":
				if gotCommands := []string{got.Commands[0].Canonical, got.Commands[1].Canonical}; !slices.Equal(gotCommands, []string{"echo", "let"}) || countTokens(got, TokenSeparator) != 0 || countTokens(got, TokenComment) != 1 {
					t.Fatalf("comment bar commands = %#v", got.Commands)
				}
				incrementalDeclaration(t, got, "afterBars")
			case "leading bar appears":
				if len(got.Commands) != 3 || countTokens(got, TokenContinuation) != 1 || got.Commands[1].Canonical != "autocmd" || got.Text(got.Commands[1].Argument) != "BufNewFile *.match if ok\n  | echo 'match'" {
					t.Fatalf("leading bar appearance = %#v", got.Commands)
				}
				incrementalDeclaration(t, got, "afterLeading")
			case "leading bar disappears":
				if len(got.Commands) != 4 || countTokens(got, TokenContinuation) != 0 || got.Commands[1].Canonical != "autocmd" || got.Text(got.Commands[1].Argument) != "BufNewFile *.match if ok" || got.Commands[2].Canonical != "echo" || got.Text(got.Commands[2].Argument) != "'match'" {
					t.Fatalf("leading bar disappearance = %#v", got.Commands)
				}
				incrementalDeclaration(t, got, "afterLeading")
			case "legacy continuation":
				if len(got.Commands) != 3 || countTokens(got, TokenContinuation) != 0 || got.Commands[0].Canonical != "let" || got.Text(got.Commands[0].Argument) != "value =" {
					t.Fatalf("legacy continuation = %#v", got.Commands)
				}
				incrementalDeclaration(t, got, "afterLegacyContinuation")
			case "Vim9 operator continuation":
				declaration := incrementalDeclaration(t, got, "operator")
				if countTokens(got, TokenContinuation) != 1 || declaration.Declaration.Initializer == nil || declaration.Declaration.Initializer.Kind != ExpressionBinary || declaration.Declaration.Initializer.Value != "-" {
					t.Fatalf("operator continuation = %#v", declaration)
				}
				incrementalDeclaration(t, got, "afterOperator")
			case "Vim9 parenthesis continuation":
				declaration := incrementalDeclaration(t, got, "paren")
				if countTokens(got, TokenContinuation) != 2 || declaration.Declaration.Initializer == nil || declaration.Declaration.Initializer.Kind != ExpressionList {
					t.Fatalf("parenthesis continuation = %#v", declaration)
				}
				incrementalDeclaration(t, got, "afterParen")
			case "Vim9 ternary continuation":
				declaration := incrementalDeclaration(t, got, "ternary")
				if countTokens(got, TokenContinuation) != 2 || declaration.Declaration.Initializer == nil || declaration.Declaration.Initializer.Kind != ExpressionBinary || declaration.Declaration.Initializer.Value != "+" {
					t.Fatalf("ternary continuation = %#v", declaration)
				}
				incrementalDeclaration(t, got, "afterTernary")
			case "Vim9 lambda continuation":
				declaration := incrementalDeclaration(t, got, "lambda")
				if countTokens(got, TokenContinuation) != 0 || declaration.Declaration.Initializer == nil || declaration.Declaration.Initializer.Kind != ExpressionCall || got.Text(declaration.Declaration.Initializer.Operator) != "->" {
					t.Fatalf("lambda continuation = %#v", declaration)
				}
				incrementalDeclaration(t, got, "afterLambda")
			}
		}
	}
}

func TestIncrementalEditMatrixHeredocOwner(t *testing.T) {
	seenTags, seenKinds, seenNames := map[string]bool{}, map[incrementalEditKind]bool{}, map[string]bool{}
	for _, scenario := range incrementalHeredocOwnerScenarios {
		for _, tag := range scenario.tags {
			seenTags[tag] = true
		}
		for _, test := range scenario.cases {
			if seenNames[test.name] {
				t.Fatalf("duplicate heredoc matrix case %q", test.name)
			}
			seenNames[test.name], seenKinds[test.kind] = true, true
			results := incrementalTextEditResults(scenario.source, test.edits)
			if len(results) == 0 || test.kind == incrementalSequence && len(results) != 2 {
				t.Fatalf("%s has invalid edit count %d", test.name, len(results))
			}
			for step, result := range results {
				if result.old == result.new || result.edit.start < 0 || result.edit.start > result.edit.oldEnd || result.edit.oldEnd > len(result.old) {
					t.Fatalf("%s step %d is invalid: %#v", test.name, step, result)
				}
				valid := incrementalTextEditKindValid(test.kind, result.edit)
				if test.kind == incrementalSequence {
					valid = incrementalSequenceStepValid(result.edit)
				}
				if !valid {
					t.Fatalf("%s step %d does not match kind %d: %#v", test.name, step, test.kind, result.edit)
				}
				if strings.Contains(test.name, "header marker") || test.name == "complete heredoc: marker rename sequence" && step == 0 {
					want := strings.Index(result.old, "<< END") + len("<< ")
					if result.edit.start != want {
						t.Fatalf("%s header edit at %d, want %d", test.name, result.edit.start, want)
					}
				}
				if strings.Contains(test.name, "end marker") || test.name == "complete heredoc: marker rename sequence" && step == 1 {
					want := strings.LastIndex(result.old, "END\n")
					if result.edit.start != want {
						t.Fatalf("%s end edit at %d, want %d", test.name, result.edit.start, want)
					}
				}
				if strings.Contains(test.name, "body") && result.edit.start < strings.Index(result.old, "body ") {
					t.Fatalf("%s edit does not hit body: %#v", test.name, result.edit)
				}
			}
		}
	}
	for _, tag := range []string{"complete heredoc", "incomplete heredoc", "heredoc header marker", "heredoc body", "heredoc end marker", "heredoc body to EOF"} {
		if !seenTags[tag] {
			t.Fatalf("missing heredoc scenario %q", tag)
		}
	}
	if len(seenKinds) != 5 {
		t.Fatalf("heredoc matrix has %d edit kinds, want 5", len(seenKinds))
	}
}

func TestReparseHeredocOwnerMatrix(t *testing.T) {
	for _, scenario := range incrementalHeredocOwnerScenarios {
		for _, test := range scenario.cases {
			t.Run(test.name, func(t *testing.T) {
				runIncrementalMatrix(t, scenario.source, test)
			})
		}
	}
}

func TestHeredocOwnerMatrixASTRecovery(t *testing.T) {
	for _, scenario := range incrementalHeredocOwnerScenarios {
		for _, test := range scenario.cases {
			sequence := strings.Contains(test.name, "marker rename sequence")
			var got *File
			if sequence {
				got = runIncrementalMatrixSteps(t, scenario.source, test, func(step int, file *File) {
					command := file.Commands[0]
					if command.Heredoc == nil {
						t.Fatalf("%s step %d has no heredoc", test.name, step)
					}
					heredoc := command.Heredoc
					switch step {
					case 0:
						if heredoc.Marker != "FIN" || !heredoc.Incomplete || heredoc.EndMarker != (Span{}) || file.Text(heredoc.Body) != "body A01\nEND\nlet afterHeredoc = 1" || len(file.Commands) != 1 {
							t.Fatalf("%s step 0 heredoc = %#v body=%q commands=%d", test.name, heredoc, file.Text(heredoc.Body), len(file.Commands))
						}
					case 1:
						if heredoc.Marker != "FIN" || heredoc.Incomplete || file.Text(heredoc.Body) != "body A01" || file.Text(heredoc.EndMarker) != "FIN" {
							t.Fatalf("%s step 1 heredoc = %#v body=%q end=%q", test.name, heredoc, file.Text(heredoc.Body), file.Text(heredoc.EndMarker))
						}
						incrementalDeclaration(t, file, "afterHeredoc")
					}
				})
			} else {
				got = runIncrementalMatrix(t, scenario.source, test)
			}
			command := got.Commands[0]
			if command.Canonical != "let" || command.Heredoc == nil {
				t.Fatalf("%s command = %#v", test.name, command)
			}
			heredoc := command.Heredoc
			if scenario.name == "complete heredoc" {
				headerRename := strings.Contains(test.name, "header marker")
				endRename := strings.Contains(test.name, "end marker")
				sequence := strings.Contains(test.name, "marker rename sequence")
				if headerRename || endRename {
					marker := "END"
					body := "body A01\nFIN\nlet afterHeredoc = 1"
					if headerRename {
						marker, body = "FIN", "body A01\nEND\nlet afterHeredoc = 1"
					}
					if !heredoc.Incomplete || heredoc.Marker != marker || heredoc.EndMarker != (Span{}) || got.Text(heredoc.Body) != body || len(got.Commands) != 1 {
						t.Fatalf("%s incomplete marker rename = %#v body=%q", test.name, heredoc, got.Text(heredoc.Body))
					}
					continue
				}
				wantMarker, wantEnd := "END", "END"
				if sequence {
					wantMarker, wantEnd = "FIN", "FIN"
				}
				wantBody := "body A01"
				switch strings.TrimPrefix(test.name, "complete heredoc: ") {
				case "body insert":
					wantBody = "body XA01"
				case "body delete":
					wantBody = "body "
				case "body replace":
					wantBody = "body B02"
				case "body length replace":
					wantBody = "body A010"
				}
				if heredoc.Incomplete || heredoc.Marker != wantMarker || got.Text(heredoc.Body) != wantBody || got.Text(heredoc.EndMarker) != wantEnd {
					t.Fatalf("%s heredoc = %#v body=%q end=%q", test.name, heredoc, got.Text(heredoc.Body), got.Text(heredoc.EndMarker))
				}
				incrementalDeclaration(t, got, "afterHeredoc")
			} else {
				if !heredoc.Incomplete || heredoc.EndMarker != (Span{}) || got.Text(heredoc.Body) != "body B02\nlet afterHeredoc = 1" {
					t.Fatalf("incomplete heredoc = %#v body=%q", heredoc, got.Text(heredoc.Body))
				}
				if len(got.Commands) != 1 {
					t.Fatalf("incomplete heredoc commands = %#v", got.Commands)
				}
			}
		}
	}
}

func TestIncrementalEditMatrixTextBodyOwner(t *testing.T) {
	seenTags, seenKinds, seenNames := map[string]bool{}, map[incrementalEditKind]bool{}, map[string]bool{}
	for _, scenario := range incrementalTextBodyOwnerScenarios {
		for _, tag := range scenario.tags {
			seenTags[tag] = true
		}
		for _, test := range scenario.cases {
			if seenNames[test.name] {
				t.Fatalf("duplicate text-body matrix case %q", test.name)
			}
			seenNames[test.name], seenKinds[test.kind] = true, true
			results := incrementalTextEditResults(scenario.source, test.edits)
			if len(results) == 0 || test.kind == incrementalSequence && len(results) != 2 {
				t.Fatalf("%s has invalid edit count %d", test.name, len(results))
			}
			for step, result := range results {
				if result.old == result.new || result.edit.start < 0 || result.edit.start > result.edit.oldEnd || result.edit.oldEnd > len(result.old) {
					t.Fatalf("%s step %d is invalid: %#v", test.name, step, result)
				}
				valid := incrementalTextEditKindValid(test.kind, result.edit)
				if test.kind == incrementalSequence {
					valid = incrementalSequenceStepValid(result.edit)
				}
				if !valid {
					t.Fatalf("%s step %d does not match kind %d: %#v", test.name, step, test.kind, result.edit)
				}
				if strings.Contains(test.name, "body") && result.edit.start < strings.Index(result.old, "body ") {
					t.Fatalf("%s step %d does not hit body: %#v", test.name, step, result.edit)
				}
			}
		}
	}
	for _, tag := range []string{"append", "change", "insert", "text body", "dot terminator", "dot recovery", "loadkeymap", "keymap body to EOF"} {
		if !seenTags[tag] {
			t.Fatalf("missing text-body scenario %q", tag)
		}
	}
	if len(seenKinds) != 5 {
		t.Fatalf("text-body matrix has %d edit kinds, want 5", len(seenKinds))
	}
}

func TestReparseTextBodyOwnerMatrix(t *testing.T) {
	for _, scenario := range incrementalTextBodyOwnerScenarios {
		for _, test := range scenario.cases {
			t.Run(test.name, func(t *testing.T) {
				runIncrementalMatrix(t, scenario.source, test)
			})
		}
	}
}

func TestTextBodyOwnerMatrixASTRecovery(t *testing.T) {
	for _, scenario := range incrementalTextBodyOwnerScenarios {
		for _, test := range scenario.cases {
			sequence := scenario.name == "dot terminator"
			var got *File
			if sequence {
				got = runIncrementalMatrixSteps(t, scenario.source, test, func(step int, file *File) {
					command := file.Commands[0]
					body := file.Commands[0].TextBody
					if body == nil {
						t.Fatalf("%s step %d has no text body", test.name, step)
					}
					switch step {
					case 0:
						if !body.Incomplete || body.EndMarker != (Span{}) || file.Text(body.Body) != "body A01\n!\nlet afterDot = 1" || len(file.Commands) != 1 {
							t.Fatalf("%s step 0 body = %#v text=%q commands=%d", test.name, body, file.Text(body.Body), len(file.Commands))
						}
					case 1:
						if body.Incomplete || file.Text(body.Body) != "body A01" || file.Text(body.EndMarker) != "." || command.Span.Start != 0 || command.Span.End != body.EndMarker.End {
							t.Fatalf("%s step 1 body = %#v text=%q end=%q", test.name, body, file.Text(body.Body), file.Text(body.EndMarker))
						}
						incrementalDeclaration(t, file, "afterDot")
					}
				})
			} else {
				got = runIncrementalMatrix(t, scenario.source, test)
			}
			command := got.Commands[0]
			if scenario.name == "dot terminator" {
				body := command.TextBody
				if command.Canonical != "append" || body == nil || body.Incomplete || got.Text(body.Body) != "body A01" || got.Text(body.EndMarker) != "." || command.Span.Start != 0 || command.Span.End != body.EndMarker.End {
					t.Fatalf("%s final body = %#v text=%q end=%q", test.name, body, got.Text(body.Body), got.Text(body.EndMarker))
				}
				incrementalDeclaration(t, got, "afterDot")
				continue
			}
			if scenario.name == "loadkeymap body" {
				if command.Canonical != "loadkeymap" || command.Keymap == nil || command.Span.Start != 0 || command.Span.End != len(got.Source) || command.Keymap.Body.End != len(got.Source) || got.Text(command.Keymap.Body) != "a ASCII\nb B02\n" {
					t.Fatalf("%s keymap = %#v body=%q", test.name, command.Keymap, got.Text(command.Keymap.Body))
				}
				continue
			}
			body := command.TextBody
			wantCommand := map[string]string{"append text body": "append", "change text body": "change", "insert text body": "insert"}[scenario.name]
			if command.Canonical != wantCommand || body == nil || body.Incomplete || command.Span.Start != 0 || command.Span.End != body.EndMarker.End || got.Text(body.EndMarker) != "." {
				t.Fatalf("%s command/body = %#v/%#v", test.name, command, body)
			}
			wantBody := map[string]string{"append text body: body insert": "body XA01", "change text body: body delete": "body ", "insert text body: body replace": "body B02"}[test.name]
			if got.Text(body.Body) != wantBody {
				t.Fatalf("%s body = %q, want %q", test.name, got.Text(body.Body), wantBody)
			}
			after := map[string]string{"append text body": "afterAppend", "change text body": "afterChange", "insert text body": "afterInsert"}[scenario.name]
			incrementalDeclaration(t, got, after)
		}
	}
}

func TestIncrementalEditMatrixEmbeddedOwner(t *testing.T) {
	seenTags, seenKinds, seenNames := map[string]bool{}, map[incrementalEditKind]bool{}, map[string]bool{}
	for _, scenario := range incrementalEmbeddedOwnerScenarios {
		for _, tag := range scenario.tags {
			seenTags[tag] = true
		}
		for _, test := range scenario.cases {
			if seenNames[test.name] {
				t.Fatalf("duplicate embedded-owner matrix case %q", test.name)
			}
			seenNames[test.name], seenKinds[test.kind] = true, true
			results := incrementalTextEditResults(scenario.source, test.edits)
			if len(results) == 0 || test.kind == incrementalSequence && len(results) < 2 {
				t.Fatalf("%s has invalid edit count %d", test.name, len(results))
			}
			for step, result := range results {
				if result.old == result.new || result.edit.start < 0 || result.edit.start > result.edit.oldEnd || result.edit.oldEnd > len(result.old) {
					t.Fatalf("%s step %d is invalid: %#v", test.name, step, result)
				}
				valid := incrementalTextEditKindValid(test.kind, result.edit)
				if test.kind == incrementalSequence {
					valid = incrementalSequenceStepValid(result.edit)
				}
				if !valid {
					t.Fatalf("%s step %d does not match kind %d: %#v", test.name, step, test.kind, result.edit)
				}
			}
		}
	}
	for _, tag := range []string{"Vim9 command block", "autocmd block", "global/vglobal", "list-do", "Legacy embedded block", "nested embedded depth near", "nested embedded depth over", "direct finish", "OpaqueTail", "conditional finish"} {
		if !seenTags[tag] {
			t.Fatalf("missing embedded-owner scenario %q", tag)
		}
	}
	if len(seenKinds) != 5 {
		t.Fatalf("embedded-owner matrix has %d edit kinds, want 5", len(seenKinds))
	}
}

func TestReparseEmbeddedOwnerMatrix(t *testing.T) {
	for _, scenario := range incrementalEmbeddedOwnerScenarios {
		for _, test := range scenario.cases {
			t.Run(test.name, func(t *testing.T) {
				runIncrementalMatrix(t, scenario.source, test)
			})
		}
	}
}

func TestEmbeddedOwnerMatrixBoundaries(t *testing.T) {
	for _, scenario := range incrementalEmbeddedOwnerScenarios {
		for _, test := range scenario.cases {
			got := runIncrementalMatrix(t, scenario.source, test)
			switch scenario.name {
			case "direct finish":
				if len(got.Commands) != 2 || got.Commands[1].Canonical != "finish" || got.OpaqueTail.Start != strings.Index(got.Source, "|") || got.OpaqueTail.End != len(got.Source) || got.Text(got.OpaqueTail) != "| echo 'live'\nHELP TEXT *tag*\n" {
					t.Fatalf("direct finish = %#v tail=%q", got.Commands, got.Text(got.OpaqueTail))
				}
			case "conditional finish":
				if got.OpaqueTail != (Span{}) {
					t.Fatalf("conditional opaque tail = %#v", got.OpaqueTail)
				}
				incrementalDeclaration(t, got, "values")
			case "nested embedded depth near":
				if got.Commands[0].Embedded == nil || hasDiagnostic(got, "vimls/embedded-command-depth") {
					t.Fatalf("nested near depth = %#v diagnostics=%#v", got.Commands[0].Embedded, got.Diagnostics)
				}
				owner := &got.Commands[0]
				for depth := 0; depth < maxEmbeddedCommandDepth; depth++ {
					if owner.Embedded == nil || len(owner.Embedded.Commands) != 1 {
						t.Fatalf("nested near owner depth %d = %#v", depth, owner)
					}
					owner = &owner.Embedded.Commands[0]
				}
				if owner.Canonical != "edit" {
					t.Fatalf("nested near leaf = %#v", owner)
				}
				incrementalDeclaration(t, got, "afterNestedNear")
			case "nested embedded depth over":
				if got.Commands[0].Embedded == nil || !hasDiagnostic(got, "vimls/embedded-command-depth") {
					t.Fatalf("nested over depth = %#v diagnostics=%#v", got.Commands[0].Embedded, got.Diagnostics)
				}
				incrementalDeclaration(t, got, "afterNestedOver")
			case "Vim9 command block":
				if len(got.Commands) < 2 || got.Commands[1].Block < 0 || got.Blocks[got.Commands[1].Block].Kind != BlockCommand {
					t.Fatalf("Vim9 command block owner = %#v blocks=%#v", got.Commands, got.Blocks)
				}
				incrementalDeclaration(t, got, "afterCommand")
			case "global/vglobal":
				outer := got.Commands[0]
				if outer.Canonical != "global" || outer.Embedded == nil || len(outer.Embedded.Commands) < 2 || outer.Embedded.Commands[1].Canonical != "vglobal" || outer.Embedded.Commands[1].Embedded == nil {
					t.Fatalf("global/vglobal owner = %#v", outer)
				}
				incrementalDeclaration(t, got, "afterGlobal")
			case "Legacy embedded block":
				outer := got.Commands[0]
				if outer.Canonical != "windo" || outer.Embedded == nil || len(outer.Embedded.Commands) != 3 || outer.Embedded.Commands[0].Canonical != "if" || outer.Embedded.Commands[1].Canonical != "echo" || outer.Embedded.Commands[2].Canonical != "endif" || len(outer.Embedded.Blocks) != 1 || outer.Embedded.Blocks[0].Kind != BlockIf || outer.Embedded.Blocks[0].End < 0 {
					t.Fatalf("legacy embedded block owner = %#v", outer)
				}
				if len(got.Commands) != 2 || got.Commands[1].Declaration == nil {
					t.Fatalf("legacy embedded leaked commands = %#v", got.Commands)
				}
				incrementalDeclaration(t, got, "afterLegacyBlock")
			default:
				ownerIndex := 0
				if got.Commands[ownerIndex].Embedded == nil {
					t.Fatalf("%s has no embedded owner: %#v", scenario.name, got.Commands[ownerIndex])
				}
				after := map[string]string{"autocmd block": "afterAutocmd", "list-do": "afterListDo"}[scenario.name]
				incrementalDeclaration(t, got, after)
			}
		}
	}
}

var incrementalStructureStateScenarios = func() []incrementalCommandBoundaryScenario {
	structural := "vim9script\nif true\n  var ifValue = 1\nendif\nfor item in [1]\n  var forValue = item\nendfor\nwhile false\n  var whileValue = 1\nendwhile\ntry\n  var tryValue = 1\ncatch /one/\n  var caught = 2\nfinally\n  var finalized = 3\nendtry\nif true\n  var stable = 4\nendif\n"
	stableBlocks := "vim9script\nif true\n  var first = 1\nendif\nif true\n  var stable = 2\nendif\n"
	withoutUnclosedIf := "vim9script\nvar before = 1\nvar stable = 2\n"
	withUnclosedIf := "vim9script\nvar before = 1\nif true\nvar stable = 2\n"
	duplicateFinally := "vim9script\ntry\n  var work = 1\nfinally\nfinally\nendtry\nvar afterFinally = 2\n"
	catchAll := "vim9script\ntry\n  var work = 1\ncatch\ncatch /later/\nendtry\nvar afterCatch = 2\n"
	bareFor := "vim9script\nfor\nvar afterBareFor = 1\n"
	classRecovery := "vim9script\nclass Shape\n  def\n  enddef\nendclass\nvar afterClass = 1\n"
	interfaceRecovery := "vim9script\ninterface SomethingWrong\n  def GetCount(): number\n    return 5\n  enddef\nendinterface\nvar afterInterface = 1\n"
	redir := "vim9script\ndef Capture()\n  redir => message\n  echo 'hello'\n  redir END\nenddef\nvar afterRedir = 1\n"
	removeAndRestore := func(start int, text string) []incrementalTextEdit {
		remove := incrementalTextEditAtOffset(structural, start, text, "")
		changed := applyIncrementalTextEdit(structural, remove)
		return []incrementalTextEdit{remove, incrementalTextEditAtOffset(changed, remove.start, "", text)}
	}
	return []incrementalCommandBoundaryScenario{
		{
			name: "structural opener and closer edits", tags: []string{"if opener/closer", "for opener/closer", "while opener/closer", "try opener/closer"}, source: structural,
			cases: []incrementalMatrixCase{
				newIncrementalMatrixCase("structural opener and closer edits", "delete and restore if opener", incrementalSequence, removeAndRestore(strings.Index(structural, "if true\n  var ifValue"), "if true\n")),
				newIncrementalMatrixCase("structural opener and closer edits", "delete and restore if closer", incrementalSequence, removeAndRestore(strings.Index(structural, "endif\nfor item"), "endif\n")),
				newIncrementalMatrixCase("structural opener and closer edits", "delete and restore try opener", incrementalSequence, removeAndRestore(strings.Index(structural, "try\n  var tryValue"), "try\n")),
				newIncrementalMatrixCase("structural opener and closer edits", "delete and restore try closer", incrementalSequence, removeAndRestore(strings.Index(structural, "endtry\nif true"), "endtry\n")),
				newIncrementalMatrixCase("structural opener and closer edits", "delete and restore for opener", incrementalSequence, removeAndRestore(strings.Index(structural, "for item in [1]"), "for item in [1]\n")),
				newIncrementalMatrixCase("structural opener and closer edits", "delete and restore for closer", incrementalSequence, removeAndRestore(strings.Index(structural, "endfor\nwhile false"), "endfor\n")),
				newIncrementalMatrixCase("structural opener and closer edits", "delete and restore while opener", incrementalSequence, removeAndRestore(strings.Index(structural, "while false\n  var whileValue"), "while false\n")),
				newIncrementalMatrixCase("structural opener and closer edits", "delete and restore while closer", incrementalSequence, removeAndRestore(strings.Index(structural, "endwhile\ntry"), "endwhile\n")),
				newIncrementalMatrixCase("structural opener and closer edits", "replace while opener", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(structural, "while false", "while value")}),
				newIncrementalMatrixCase("structural opener and closer edits", "extend try catch", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAt(structural, "catch /one/", "catch /one|two/")}),
			},
		},
		{
			name: "duplicate finally recovery", tags: []string{"duplicate finally", "multiple finally"}, source: duplicateFinally,
			cases: []incrementalMatrixCase{
				newIncrementalMatrixCase("duplicate finally recovery", "remove and restore duplicate finally", incrementalSequence, func() []incrementalTextEdit {
					remove := incrementalTextEditAtOffset(duplicateFinally, strings.LastIndex(duplicateFinally, "finally\n"), "finally\n", "")
					changed := applyIncrementalTextEdit(duplicateFinally, remove)
					return []incrementalTextEdit{remove, incrementalTextInsertAt(changed, "endtry\n", "finally\n")}
				}()),
			},
		},
		{
			name: "catch-all change", tags: []string{"catch-all change"}, source: catchAll,
			cases: []incrementalMatrixCase{
				newIncrementalMatrixCase("catch-all change", "make first catch patterned", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAt(catchAll, "catch\ncatch /later/", "catch /first/\ncatch /later/")}),
			},
		},
		{
			name: "bare for recovery", tags: []string{"invalid bare-for recovery"}, source: bareFor,
			cases: []incrementalMatrixCase{
				newIncrementalMatrixCase("bare for recovery", "complete for header", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAt(bareFor, "for\n", "for item in [1]\n")}),
			},
		},
		{
			name: "class method recovery", tags: []string{"class method recovery"}, source: classRecovery,
			cases: []incrementalMatrixCase{
				newIncrementalMatrixCase("class method recovery", "recover missing method name", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAtOffset(classRecovery, strings.Index(classRecovery, "  def\n")+2, "def\n", "def Draw()\n")}),
			},
		},
		{
			name: "interface method recovery", tags: []string{"interface method-body recovery"}, source: interfaceRecovery,
			cases: []incrementalMatrixCase{
				newIncrementalMatrixCase("interface method recovery", "remove invalid method body", incrementalDelete, []incrementalTextEdit{incrementalTextEditAt(interfaceRecovery, "    return 5\n  enddef\n", "")}),
			},
		},
		{
			name: "redir break and restore", tags: []string{"Vim9 redir open/close", "redir break and restore"}, source: redir,
			cases: []incrementalMatrixCase{
				newIncrementalMatrixCase("redir break and restore", "close removal and restoration", incrementalSequence, func() []incrementalTextEdit {
					remove := incrementalTextEditAt(redir, "redir END\n", "")
					changed := applyIncrementalTextEdit(redir, remove)
					return []incrementalTextEdit{remove, incrementalTextInsertAt(changed, "enddef\n", "  redir END\n")}
				}()),
			},
		},
		{
			name: "structure index shift", tags: []string{"numeric Block index shift", "stable structurePath"}, source: stableBlocks,
			cases: []incrementalMatrixCase{
				newIncrementalMatrixCase("structure index shift", "insert preceding block", incrementalInsert, []incrementalTextEdit{incrementalTextInsertAt(stableBlocks, "if true\n  var stable", "if true\n  var inserted = 1\nendif\n")}),
			},
		},
		{
			name: "unclosed if structure recovery", tags: []string{"unclosed if before declaration", "downstream declaration survives"}, source: withoutUnclosedIf,
			cases: []incrementalMatrixCase{
				newIncrementalMatrixCase("unclosed if structure recovery", "add unclosed if", incrementalInsert, []incrementalTextEdit{incrementalTextInsertAt(withoutUnclosedIf, "var stable", "if true\n")}),
			},
		},
		{
			name: "remove unclosed if structure recovery", tags: []string{"remove unclosed if", "scanner state converges"}, source: withUnclosedIf,
			cases: []incrementalMatrixCase{
				newIncrementalMatrixCase("remove unclosed if structure recovery", "remove unclosed if", incrementalDelete, []incrementalTextEdit{incrementalTextEditAt(withUnclosedIf, "if true\n", "")}),
			},
		},
	}
}()

var incrementalRecoveryScenarios = func() []incrementalCommandBoundaryScenario {
	stringSource := "vim9script\nvar value = \"abc\nvar afterString = 1\n"
	listSource := "vim9script\nvar values: list<number> = [\n  1,\n  2,\nvar afterList = 1\n"
	dictionarySource := "vim9script\nvar value = {'a':\nvar afterDictionary = 1\n"
	typeSource := "vim9script\nvar value: tuple<number, string = (1, 'x')\nvar afterType = 1\n"
	genericSource := "vim9script\ndef Fn<T(value: T)\nenddef\nvar afterGeneric = 1\n"
	lambdaSource := "vim9script\nvar Func = (nr: number): int => {\n  return nr\nvar afterLambda = 1\n"
	malformedBar := "vim9script\nvar broken = [1, 2} | echo hidden\nvar afterBar = 1\n"
	unknown := "futurecmd arg\nlet afterUnknown = 1\n"
	closeString := incrementalTextInsertAt(stringSource, "\nvar afterString", "\"")
	cleanString := applyIncrementalTextEdit(stringSource, closeString)
	return []incrementalCommandBoundaryScenario{
		{
			name: "unterminated string transition", tags: []string{"unclosed string", "fragile to clean", "clean to fragile"}, source: stringSource,
			cases: []incrementalMatrixCase{newIncrementalMatrixCase("unterminated string transition", "close and reopen string", incrementalSequence, []incrementalTextEdit{
				closeString,
				incrementalTextEditAt(cleanString, "\"\nvar afterString", "\nvar afterString"),
			})},
		},
		{
			name: "unclosed list", tags: []string{"unclosed list"}, source: listSource,
			cases: []incrementalMatrixCase{newIncrementalMatrixCase("unclosed list", "replace item", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(listSource, "2", "3")})},
		},
		{
			name: "unclosed dictionary", tags: []string{"unclosed dict"}, source: dictionarySource,
			cases: []incrementalMatrixCase{newIncrementalMatrixCase("unclosed dictionary", "extend key", incrementalInsert, []incrementalTextEdit{incrementalTextInsertAt(dictionarySource, "':", "x")})},
		},
		{
			name: "incomplete type", tags: []string{"unclosed type"}, source: typeSource,
			cases: []incrementalMatrixCase{newIncrementalMatrixCase("incomplete type", "rename declaration", incrementalEqualReplace, []incrementalTextEdit{incrementalTextEditAt(typeSource, "value", "typed")})},
		},
		{
			name: "incomplete generic signature", tags: []string{"incomplete signature/generic"}, source: genericSource,
			cases: []incrementalMatrixCase{newIncrementalMatrixCase("incomplete generic signature", "lengthen function name", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAt(genericSource, "Fn", "Function")})},
		},
		{
			name: "unclosed lambda", tags: []string{"unclosed lambda"}, source: lambdaSource,
			cases: []incrementalMatrixCase{newIncrementalMatrixCase("unclosed lambda", "delete return keyword", incrementalDelete, []incrementalTextEdit{incrementalTextEditAt(lambdaSource, "return ", "")})},
		},
		{
			name: "malformed same-line bar", tags: []string{"malformed same-line bar payload", "next-line declaration recovery"}, source: malformedBar,
			cases: []incrementalMatrixCase{newIncrementalMatrixCase("malformed same-line bar", "lengthen hidden payload", incrementalLengthReplace, []incrementalTextEdit{incrementalTextEditAt(malformedBar, "hidden", "payload")})},
		},
		{
			name: "unknown command recovery", tags: []string{"unknown command opaque"}, source: unknown,
			cases: []incrementalMatrixCase{newIncrementalMatrixCase("unknown command recovery", "extend argument", incrementalInsert, []incrementalTextEdit{incrementalTextInsertAt(unknown, "arg", "more-")})},
		},
	}
}()

func TestIncrementalEditMatrixStructureState(t *testing.T) {
	seenTags, seenKinds, seenNames := map[string]bool{}, map[incrementalEditKind]bool{}, map[string]bool{}
	for _, scenario := range incrementalStructureStateScenarios {
		for _, tag := range scenario.tags {
			seenTags[tag] = true
		}
		for _, test := range scenario.cases {
			if seenNames[test.name] {
				t.Fatalf("duplicate structure-state matrix case %q", test.name)
			}
			seenNames[test.name], seenKinds[test.kind] = true, true
			results := incrementalTextEditResults(scenario.source, test.edits)
			if len(results) == 0 || test.kind == incrementalSequence && len(results) < 2 {
				t.Fatalf("%s has insufficient edits: %d", test.name, len(results))
			}
			for step, result := range results {
				if result.old == result.new || !incrementalTextEditKindValid(test.kind, result.edit) && test.kind != incrementalSequence || test.kind == incrementalSequence && !incrementalSequenceStepValid(result.edit) {
					t.Fatalf("%s step %d is invalid: %#v", test.name, step, result.edit)
				}
			}
		}
	}
	for _, tag := range []string{"if opener/closer", "for opener/closer", "while opener/closer", "try opener/closer", "catch-all change", "duplicate finally", "multiple finally", "invalid bare-for recovery", "class method recovery", "interface method-body recovery", "Vim9 redir open/close", "redir break and restore", "numeric Block index shift", "stable structurePath", "unclosed if before declaration", "downstream declaration survives", "remove unclosed if", "scanner state converges"} {
		if !seenTags[tag] {
			t.Fatalf("missing structure-state scenario %q", tag)
		}
	}
	if len(seenKinds) != 5 {
		t.Fatalf("structure-state matrix has %d edit kinds, want 5", len(seenKinds))
	}
}

func TestReparseStructureStateMatrix(t *testing.T) {
	for _, scenario := range incrementalStructureStateScenarios {
		for _, test := range scenario.cases {
			t.Run(test.name, func(t *testing.T) {
				runIncrementalMatrix(t, scenario.source, test)
			})
		}
	}
}

func TestStructureStateMatrixBoundaries(t *testing.T) {
	structuralChanges := map[string]struct {
		kind   BlockKind
		opener bool
	}{
		"structural opener and closer edits: delete and restore if opener":    {kind: BlockIf, opener: true},
		"structural opener and closer edits: delete and restore if closer":    {kind: BlockIf},
		"structural opener and closer edits: delete and restore try opener":   {kind: BlockTry, opener: true},
		"structural opener and closer edits: delete and restore try closer":   {kind: BlockTry},
		"structural opener and closer edits: delete and restore for opener":   {kind: BlockFor, opener: true},
		"structural opener and closer edits: delete and restore for closer":   {kind: BlockFor},
		"structural opener and closer edits: delete and restore while opener": {kind: BlockWhile, opener: true},
		"structural opener and closer edits: delete and restore while closer": {kind: BlockWhile},
	}
	blockCount := func(file *File, kind BlockKind) int {
		count := 0
		for _, block := range file.Blocks {
			if block.Kind == kind {
				count++
			}
		}
		return count
	}
	for _, scenario := range incrementalStructureStateScenarios {
		for _, test := range scenario.cases {
			var got *File
			if change, ok := structuralChanges[test.name]; ok {
				beforeCount := blockCount(Parse(scenario.source), change.kind)
				got = runIncrementalMatrixSteps(t, scenario.source, test, func(step int, file *File) {
					if step != 0 {
						return
					}
					if change.opener {
						if gotCount := blockCount(file, change.kind); gotCount >= beforeCount {
							t.Fatalf("%s opener removal kept %d %s blocks, want fewer than %d", test.name, gotCount, change.kind, beforeCount)
						}
						return
					}
					for _, block := range file.Blocks {
						if block.Kind == change.kind && block.End < 0 {
							return
						}
					}
					t.Fatalf("%s closer removal did not leave an unclosed %s block: %#v", test.name, change.kind, file.Blocks)
				})
			} else if scenario.name == "duplicate finally recovery" {
				got = runIncrementalMatrixSteps(t, scenario.source, test, func(step int, file *File) {
					if step == 0 && hasDiagnostic(file, "vim/E607") {
						t.Fatalf("duplicate finally removal still reported E607: %#v", file.Diagnostics)
					}
				})
			} else if scenario.name == "redir break and restore" {
				got = runIncrementalMatrixSteps(t, scenario.source, test, func(step int, file *File) {
					if step == 0 && !hasDiagnostic(file, "vim/E1185") {
						t.Fatalf("redir close removal did not report missing END: %#v", file.Diagnostics)
					}
				})
			} else {
				got = runIncrementalMatrix(t, scenario.source, test)
			}
			switch scenario.name {
			case "structural opener and closer edits":
				stable := incrementalDeclaration(t, got, "stable")
				if gotPath := structurePath(got, stable.Declaration.Name.Start); !slices.Equal(gotPath, []BlockKind{BlockIf}) {
					t.Fatalf("stable structure path = %#v", gotPath)
				}
			case "duplicate finally recovery":
				if !hasDiagnostic(got, "vim/E607") || len(got.Blocks) != 1 || got.Blocks[0].Kind != BlockTry {
					t.Fatalf("duplicate finally recovery = %#v blocks=%#v", got.Diagnostics, got.Blocks)
				}
				incrementalDeclaration(t, got, "afterFinally")
			case "catch-all change":
				if !hasDiagnostic(Parse(scenario.source), "vim/E1033") {
					t.Fatalf("catch-all baseline did not report E1033")
				}
				if hasDiagnostic(got, "vim/E1033") || len(got.Blocks) != 1 || got.Blocks[0].Kind != BlockTry {
					t.Fatalf("catch-all change = %#v blocks=%#v", got.Diagnostics, got.Blocks)
				}
				incrementalDeclaration(t, got, "afterCatch")
			case "bare for recovery":
				if !hasDiagnostic(Parse(scenario.source), "vim/E690") {
					t.Fatalf("bare-for baseline did not report E690")
				}
				if hasDiagnostic(got, "vim/E690") || len(got.Blocks) != 1 || got.Blocks[0].Kind != BlockFor {
					t.Fatalf("bare-for recovery = %#v blocks=%#v", got.Diagnostics, got.Blocks)
				}
				incrementalDeclaration(t, got, "afterBareFor")
			case "class method recovery":
				if !hasDiagnostic(Parse(scenario.source), "vim/E1318") {
					t.Fatalf("class recovery baseline did not report E1318")
				}
				if hasDiagnostic(got, "vim/E1318") || len(got.Blocks) < 1 || got.Blocks[0].Kind != BlockClass || got.Blocks[0].End < 0 {
					t.Fatalf("class recovery = %#v blocks=%#v", got.Diagnostics, got.Blocks)
				}
				incrementalDeclaration(t, got, "afterClass")
			case "interface method recovery":
				if !hasDiagnostic(Parse(scenario.source), "vim/E1345") {
					t.Fatalf("interface recovery baseline did not report E1345")
				}
				if hasDiagnostic(got, "vim/E1345") || len(got.Blocks) != 1 || got.Blocks[0].Kind != BlockInterface {
					t.Fatalf("interface recovery = %#v blocks=%#v", got.Diagnostics, got.Blocks)
				}
				incrementalDeclaration(t, got, "afterInterface")
			case "redir break and restore":
				if len(got.Commands) < 2 || got.Commands[1].Canonical != "def" || len(got.Diagnostics) != 0 {
					t.Fatalf("redir recovery = %#v commands=%#v", got.Diagnostics, got.Commands)
				}
				redirCount := 0
				redirArguments := make([]string, 0, 2)
				for _, command := range got.Commands {
					if command.Canonical == "redir" {
						redirCount++
						redirArguments = append(redirArguments, got.Text(command.Argument))
					}
				}
				if redirCount != 2 || !slices.Equal(redirArguments, []string{"=> message", "END"}) {
					t.Fatalf("redir commands = %d %#v, commands=%#v", redirCount, redirArguments, got.Commands)
				}
				incrementalDeclaration(t, got, "afterRedir")
			case "structure index shift":
				stable := incrementalDeclaration(t, got, "stable")
				before := Parse(scenario.source)
				beforeStable := incrementalDeclaration(t, before, "stable")
				beforePath := structurePath(before, beforeStable.Declaration.Name.Start)
				beforeIndex, afterIndex := -1, -1
				for index, block := range before.Blocks {
					if block.Kind == BlockIf && block.Span.Start <= beforeStable.Declaration.Name.Start && beforeStable.Declaration.Name.Start <= block.Span.End {
						beforeIndex = index
						break
					}
				}
				for index, block := range got.Blocks {
					if block.Kind == BlockIf && block.Span.Start <= stable.Declaration.Name.Start && stable.Declaration.Name.Start <= block.Span.End {
						afterIndex = index
						break
					}
				}
				afterPath := structurePath(got, stable.Declaration.Name.Start)
				if beforeIndex < 0 || afterIndex <= beforeIndex || !slices.Equal(beforePath, []BlockKind{BlockIf}) || !slices.Equal(afterPath, []BlockKind{BlockIf}) {
					t.Fatalf("stable block/path did not shift: before=%d/%#v after=%d/%#v blocks=%#v", beforeIndex, beforePath, afterIndex, afterPath, got.Blocks)
				}
			case "unclosed if structure recovery":
				stable := incrementalDeclaration(t, got, "stable")
				gotPath := structurePath(got, stable.Declaration.Name.Start)
				if !hasDiagnostic(got, "vimls/missing-end") || len(got.Blocks) != 1 || !slices.Equal(gotPath, []BlockKind{BlockIf}) {
					t.Fatalf("unclosed if recovery = %#v path=%#v", got.Diagnostics, gotPath)
				}
			case "remove unclosed if structure recovery":
				stable := incrementalDeclaration(t, got, "stable")
				if len(got.Blocks) != 0 || len(structurePath(got, stable.Declaration.Name.Start)) != 0 {
					t.Fatalf("removed unclosed if recovery = blocks=%#v path=%#v", got.Blocks, structurePath(got, stable.Declaration.Name.Start))
				}
			}
			if scenario.name == "unclosed if structure recovery" {
				old := Parse(scenario.source)
				newFile := got
				oldStable := incrementalDeclaration(t, old, "stable")
				newStable := incrementalDeclaration(t, newFile, "stable")
				unitFor := func(file *File, offset int) parseUnit {
					for _, unit := range file.incremental.units {
						if unit.span.Start <= offset && offset < unit.span.End {
							return unit
						}
					}
					t.Fatalf("no metadata unit contains offset %d", offset)
					return parseUnit{}
				}
				oldUnit := unitFor(old, oldStable.Declaration.Name.Start)
				newUnit := unitFor(newFile, newStable.Declaration.Name.Start)
				if !reflect.DeepEqual(oldUnit.entry, newUnit.entry) || !reflect.DeepEqual(oldUnit.exit, newUnit.exit) || reflect.DeepEqual(oldUnit.structureEntry, newUnit.structureEntry) || !slices.Equal(oldUnit.structureEntry, nil) || !slices.Equal(newUnit.structureEntry, []BlockKind{BlockIf}) {
					t.Fatalf("scanner/structure state did not diverge as expected: old=%#v new=%#v", oldUnit, newUnit)
				}
			}
		}
	}
}

func TestIncrementalEditMatrixRecovery(t *testing.T) {
	seenTags, seenKinds, seenNames := map[string]bool{}, map[incrementalEditKind]bool{}, map[string]bool{}
	for _, scenario := range incrementalRecoveryScenarios {
		for _, tag := range scenario.tags {
			seenTags[tag] = true
		}
		for _, test := range scenario.cases {
			if seenNames[test.name] {
				t.Fatalf("duplicate recovery matrix case %q", test.name)
			}
			seenNames[test.name], seenKinds[test.kind] = true, true
			results := incrementalTextEditResults(scenario.source, test.edits)
			if len(results) == 0 || test.kind == incrementalSequence && len(results) < 2 {
				t.Fatalf("%s has insufficient edits: %d", test.name, len(results))
			}
			for step, result := range results {
				if result.old == result.new || result.edit.start < 0 || result.edit.start > result.edit.oldEnd || result.edit.oldEnd > len(result.old) {
					t.Fatalf("%s step %d is invalid: %#v", test.name, step, result)
				}
				valid := incrementalTextEditKindValid(test.kind, result.edit)
				if test.kind == incrementalSequence {
					valid = incrementalSequenceStepValid(result.edit)
				}
				if !valid {
					t.Fatalf("%s step %d does not match kind %d: %#v", test.name, step, test.kind, result.edit)
				}
			}
		}
	}
	for _, tag := range []string{"unclosed string", "unclosed list", "unclosed dict", "unclosed type", "incomplete signature/generic", "unclosed lambda", "malformed same-line bar payload", "next-line declaration recovery", "unknown command opaque", "fragile to clean", "clean to fragile"} {
		if !seenTags[tag] {
			t.Fatalf("missing recovery scenario %q", tag)
		}
	}
	if len(seenKinds) != 5 {
		t.Fatalf("recovery matrix has %d edit kinds, want 5", len(seenKinds))
	}
}

func TestReparseRecoveryMatrix(t *testing.T) {
	for _, scenario := range incrementalRecoveryScenarios {
		for _, test := range scenario.cases {
			t.Run(test.name, func(t *testing.T) {
				runIncrementalMatrix(t, scenario.source, test)
			})
		}
	}
}

func TestRecoveryMatrixASTRecovery(t *testing.T) {
	for _, scenario := range incrementalRecoveryScenarios {
		for _, test := range scenario.cases {
			var got *File
			if scenario.name == "unterminated string transition" {
				got = runIncrementalMatrixSteps(t, scenario.source, test, func(step int, file *File) {
					if step == 0 {
						if hasDiagnostic(file, "vim/E114") {
							t.Fatalf("%s did not restore clean string: %#v", test.name, file.Diagnostics)
						}
					} else if !hasDiagnostic(file, "vim/E114") {
						t.Fatalf("%s did not restore fragile string: %#v", test.name, file.Diagnostics)
					}
					declaration := incrementalDeclaration(t, file, "value")
					if declaration.Declaration.Initializer == nil || declaration.Declaration.Initializer.Kind != ExpressionString {
						t.Fatalf("%s string initializer = %#v", test.name, declaration.Declaration.Initializer)
					}
					incrementalDeclaration(t, file, "afterString")
				})
			} else {
				got = runIncrementalMatrix(t, scenario.source, test)
			}
			switch scenario.name {
			case "unterminated string transition":
				if !hasDiagnostic(got, "vim/E114") {
					t.Fatalf("unterminated string diagnostics = %#v", got.Diagnostics)
				}
			case "unclosed list":
				declaration := incrementalDeclaration(t, got, "values")
				if !hasDiagnostic(got, "vimls/missing-delimiter") || declaration.Declaration.Initializer == nil || declaration.Declaration.Initializer.Kind != ExpressionList {
					t.Fatalf("unclosed list = %#v diagnostics=%#v", declaration, got.Diagnostics)
				}
				incrementalDeclaration(t, got, "afterList")
			case "unclosed dictionary":
				declaration := incrementalDeclaration(t, got, "value")
				if !hasDiagnostic(got, "vim/E15") || declaration.Declaration.Initializer == nil || declaration.Declaration.Initializer.Kind != ExpressionDictionary {
					t.Fatalf("unclosed dictionary = %#v diagnostics=%#v", declaration, got.Diagnostics)
				}
				incrementalDeclaration(t, got, "afterDictionary")
			case "incomplete type":
				if !hasDiagnostic(got, "vimls/missing-type-delimiter") {
					t.Fatalf("incomplete type diagnostics = %#v", got.Diagnostics)
				}
				incrementalDeclaration(t, got, "typed")
				incrementalDeclaration(t, got, "afterType")
			case "incomplete generic signature":
				if !hasDiagnostic(got, "vimls/missing-generic-end") || len(got.Commands) < 2 || got.Commands[1].Function == nil {
					t.Fatalf("incomplete generic signature = %#v diagnostics=%#v", got.Commands, got.Diagnostics)
				}
				incrementalDeclaration(t, got, "afterGeneric")
			case "unclosed lambda":
				declaration := incrementalDeclaration(t, got, "Func")
				initializer := declaration.Declaration.Initializer
				if !hasDiagnostic(got, "vim/E1171") || initializer == nil || initializer.Kind != ExpressionLambda || initializer.LambdaBody == nil {
					t.Fatalf("unclosed lambda = %#v diagnostics=%#v", initializer, got.Diagnostics)
				}
				incrementalDeclaration(t, got, "afterLambda")
			case "malformed same-line bar":
				broken := incrementalDeclaration(t, got, "broken")
				if !hasDiagnostic(got, "vim/E696") || countTokens(got, TokenSeparator) != 0 || got.Text(broken.Argument) != "broken = [1, 2} | echo payload" {
					t.Fatalf("malformed same-line bar diagnostics=%#v tokens=%#v", got.Diagnostics, got.Tokens)
				}
				incrementalDeclaration(t, got, "afterBar")
			case "unknown command recovery":
				if len(got.Commands) < 1 || got.Commands[0].Kind != CommandUnknown || got.Commands[0].Canonical != "futurecmd" {
					t.Fatalf("unknown command = %#v", got.Commands)
				}
				incrementalDeclaration(t, got, "afterUnknown")
			}
		}
	}
}

func incrementalDeclaration(t *testing.T, file *File, name string) *Command {
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Declaration != nil && file.Text(command.Declaration.Name) == name {
			return command
		}
	}
	t.Fatalf("declaration %q not found", name)
	return nil
}

func runIncrementalMatrix(t *testing.T, source string, test incrementalMatrixCase) *File {
	return runIncrementalMatrixSteps(t, source, test, nil)
}

func runIncrementalMatrixSteps(t *testing.T, source string, test incrementalMatrixCase, check func(int, *File)) *File {
	t.Helper()
	previous := Parse(source)
	for step, result := range incrementalTextEditResults(source, test.edits) {
		if previous.Source != result.old {
			t.Fatalf("step %d starts from %q, previous source is %q", step, result.old, previous.Source)
		}
		previous = checkIncrementalParserFromPrevious(t, Reparse, previous, fmt.Sprintf("%s step %d", test.name, step), result.new)
		if check != nil {
			check(step, previous)
		}
	}
	return previous
}

func checkIncrementalParser(t *testing.T, parse func(*File, string) *File, test incrementalEditCase) *File {
	t.Helper()
	previous := Parse(test.old)
	return checkIncrementalParserFromPrevious(t, parse, previous, test.name, test.new)
}

func checkIncrementalParserFromPrevious(t *testing.T, parse func(*File, string) *File, previous *File, name, source string) *File {
	t.Helper()
	beforeJSON := marshalSyntax(t, previous)
	beforeAliases := syntaxAliases(previous)

	got := parse(previous, source)
	want := Parse(source)
	if string(marshalSyntax(t, got)) != string(marshalSyntax(t, want)) {
		t.Fatalf("%s: incremental syntax differs from full parse\ngot:  %s\nwant: %s", name, marshalSyntax(t, got), marshalSyntax(t, want))
	}
	if gotAliases, wantAliases := syntaxAliases(got), syntaxAliases(want); !slices.Equal(gotAliases, wantAliases) {
		t.Fatalf("%s: alias topology differs\ngot:  %v\nwant: %v", name, gotAliases, wantAliases)
	}
	if syntaxSpansValid(want) {
		checkSyntaxSpans(t, got)
	}
	if afterJSON := marshalSyntax(t, previous); string(afterJSON) != string(beforeJSON) {
		t.Fatalf("%s: previous syntax changed\nbefore: %s\nafter:  %s", name, beforeJSON, afterJSON)
	}
	if afterAliases := syntaxAliases(previous); !slices.Equal(afterAliases, beforeAliases) {
		t.Fatalf("%s: previous alias topology changed\nbefore: %v\nafter:  %v", name, beforeAliases, afterAliases)
	}
	return got
}

func marshalSyntax(t *testing.T, file *File) []byte {
	t.Helper()
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

var (
	spanReflectionType        = reflect.TypeOf(Span{})
	expressionReflectionType  = reflect.TypeOf((*Expression)(nil))
	typeReflectionType        = reflect.TypeOf((*Type)(nil))
	fileReflectionType        = reflect.TypeOf((*File)(nil))
	commandListReflectionType = reflect.TypeOf((*CommandList)(nil))
)

func syntaxAliases(file *File) []string {
	return syntaxAliasesValue(reflect.ValueOf(file))
}

func syntaxAliasesValue(root reflect.Value) []string {
	aliases := make(map[uintptr][]string)
	pointerSeen := make(map[uintptr]bool)
	active := make(map[syntaxReflectionIdentity]bool)
	walkSyntaxReflection(root, "File", pointerSeen, active, true, func(value reflect.Value, path string) bool {
		if value.Kind() == reflect.Pointer && trackedAliasType(value.Type()) {
			aliases[value.Pointer()] = append(aliases[value.Pointer()], path)
		}
		return true
	})
	result := make([]string, 0, len(aliases))
	for _, paths := range aliases {
		if len(paths) > 1 {
			slices.Sort(paths)
			result = append(result, strings.Join(paths, "="))
		}
	}
	slices.Sort(result)
	return result
}

func trackedAliasType(value reflect.Type) bool {
	return value == expressionReflectionType || value == typeReflectionType || value == fileReflectionType || value == commandListReflectionType
}

func checkSyntaxSpans(t *testing.T, file *File) {
	checkSyntaxSpansValue(t, reflect.ValueOf(file), file.Source, file.Text)
}

func checkSyntaxSpansValue(t *testing.T, root reflect.Value, source string, textForSpan func(Span) string) {
	t.Helper()
	pointerSeen := make(map[uintptr]bool)
	active := make(map[syntaxReflectionIdentity]bool)
	walkSyntaxReflection(root, "File", pointerSeen, active, false, func(value reflect.Value, path string) bool {
		if value.Type() == spanReflectionType {
			span := value.Interface().(Span)
			if span.Start < 0 || span.End < span.Start || span.End > len(source) {
				t.Errorf("%s: span %#v outside source of %d bytes", path, span, len(source))
				return false
			}
			if text := textForSpan(span); len(text) != span.End-span.Start {
				t.Errorf("%s: Text(%#v) has %d bytes", path, span, len(text))
			}
			return false
		}
		return true
	})
}

func syntaxSpansValid(file *File) bool {
	return syntaxSpansValidValue(reflect.ValueOf(file), len(file.Source))
}

func syntaxSpansValidValue(root reflect.Value, sourceLength int) bool {
	valid := true
	pointerSeen := make(map[uintptr]bool)
	active := make(map[syntaxReflectionIdentity]bool)
	walkSyntaxReflection(root, "File", pointerSeen, active, false, func(value reflect.Value, _ string) bool {
		if value.Type() == spanReflectionType {
			span := value.Interface().(Span)
			if span.Start < 0 || span.End < span.Start || span.End > sourceLength {
				valid = false
			}
			return false
		}
		return true
	})
	return valid
}

type syntaxReflectionIdentity struct {
	kind     reflect.Kind
	typ      reflect.Type
	pointer  uintptr
	length   int
	capacity int
}

func walkSyntaxReflection(value reflect.Value, path string, pointerSeen map[uintptr]bool, active map[syntaxReflectionIdentity]bool, visitRepeatedPointers bool, visit func(reflect.Value, string) bool) {
	if !value.IsValid() {
		return
	}
	if value.Kind() == reflect.Interface {
		if !value.IsNil() {
			walkSyntaxReflection(value.Elem(), path, pointerSeen, active, visitRepeatedPointers, visit)
		}
		return
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		identity := syntaxReflectionIdentity{kind: value.Kind(), pointer: value.Pointer()}
		if pointerSeen[identity.pointer] {
			if visitRepeatedPointers {
				visit(value, path)
			}
			return
		}
		pointerSeen[identity.pointer] = true
		if !visit(value, path) {
			return
		}
		walkSyntaxReflection(value.Elem(), path, pointerSeen, active, visitRepeatedPointers, visit)
		return
	}
	if value.Kind() == reflect.Map || value.Kind() == reflect.Slice {
		if value.IsNil() {
			return
		}
		identity := syntaxReflectionIdentity{kind: value.Kind(), typ: value.Type(), pointer: value.Pointer(), length: value.Len()}
		if value.Kind() == reflect.Slice {
			identity.capacity = value.Cap()
		}
		if active[identity] {
			return
		}
		active[identity] = true
		defer delete(active, identity)
	}
	if !visit(value, path) {
		return
	}
	switch value.Kind() {
	case reflect.Struct:
		valueType := value.Type()
		for index := 0; index < value.NumField(); index++ {
			field := valueType.Field(index)
			if field.PkgPath == "" {
				walkSyntaxReflection(value.Field(index), path+"."+field.Name, pointerSeen, active, visitRepeatedPointers, visit)
			}
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			walkSyntaxReflection(value.Index(index), fmt.Sprintf("%s[%d]", path, index), pointerSeen, active, visitRepeatedPointers, visit)
		}
	case reflect.Map:
		type mapEntry struct {
			key   reflect.Value
			label string
		}
		keys := value.MapKeys()
		entries := make([]mapEntry, len(keys))
		for index, key := range keys {
			entries[index] = mapEntry{key: key, label: syntaxMapKey(key)}
		}
		slices.SortStableFunc(entries, func(left, right mapEntry) int {
			return strings.Compare(left.label, right.label)
		})
		labels := make(map[string]int, len(entries))
		for _, entry := range entries {
			index := labels[entry.label]
			labels[entry.label] = index + 1
			label := entry.label
			if index > 0 {
				label += "#" + strconv.Itoa(index)
			}
			walkSyntaxReflection(entry.key, path+"["+label+"]{key}", pointerSeen, active, visitRepeatedPointers, visit)
			walkSyntaxReflection(value.MapIndex(entry.key), path+"["+label+"]{value}", pointerSeen, active, visitRepeatedPointers, visit)
		}
	}
}

func syntaxMapKey(value reflect.Value) string {
	if !value.IsValid() {
		panic("invalid syntax reflection map key")
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return "nil"
		}
		return syntaxMapKey(value.Elem())
	case reflect.String:
		return value.Type().String() + ":" + strconv.Quote(value.String())
	case reflect.Bool:
		return value.Type().String() + ":" + strconv.FormatBool(value.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Type().String() + ":" + strconv.FormatInt(value.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Type().String() + ":" + strconv.FormatUint(value.Uint(), 10)
	case reflect.Struct:
		if value.Type() == spanReflectionType {
			span := value.Interface().(Span)
			return fmt.Sprintf("Span:{%d,%d}", span.Start, span.End)
		}
		panic(fmt.Sprintf("unsupported syntax reflection map key type %s", value.Type()))
	default:
		panic(fmt.Sprintf("unsupported syntax reflection map key type %s", value.Type()))
	}
}

type syntaxReflectionMapFixture struct {
	AliasesByValue map[string]*Expression
	Spans          map[Span]Span
	Cycle          map[string]any
	hidden         Span
}

func TestSyntaxReflectionMapTraversal(t *testing.T) {
	source := "0123456789"
	expression := &Expression{Kind: ExpressionNumber, Span: Span{Start: 1, End: 3}}
	fixture := syntaxReflectionMapFixture{
		AliasesByValue: map[string]*Expression{
			"a": expression,
			"b": expression,
		},
		Spans: map[Span]Span{
			{Start: 3, End: 5}: {Start: 5, End: 7},
			{Start: 7, End: 9}: {Start: 8, End: 9},
		},
		hidden: Span{Start: 100, End: 101},
	}
	fixture.Cycle = make(map[string]any)
	fixture.Cycle["self"] = fixture.Cycle

	aliases := syntaxAliasesValue(reflect.ValueOf(fixture))
	if len(aliases) != 1 || !strings.Contains(aliases[0], ".AliasesByValue[") {
		t.Fatalf("map pointer aliases = %v", aliases)
	}
	for index := 0; index < 10; index++ {
		if got := syntaxAliasesValue(reflect.ValueOf(fixture)); !slices.Equal(got, aliases) {
			t.Fatalf("map traversal is nondeterministic: first=%v, got=%v", aliases, got)
		}
	}

	if !syntaxSpansValidValue(reflect.ValueOf(fixture), len(source)) {
		t.Fatal("valid map key/value spans were not accepted")
	}
	checkSyntaxSpansValue(t, reflect.ValueOf(fixture), source, func(span Span) string {
		return source[span.Start:span.End]
	})

	invalid := fixture
	invalid.Spans = map[Span]Span{
		{Start: len(source) + 1, End: len(source) + 2}: {Start: 0, End: 1},
	}
	if syntaxSpansValidValue(reflect.ValueOf(invalid), len(source)) {
		t.Fatal("invalid map key span was not visited")
	}
	invalidValue := fixture
	invalidValue.Spans = map[Span]Span{
		{Start: 3, End: 5}: {Start: len(source) + 1, End: len(source) + 2},
	}
	if syntaxSpansValidValue(reflect.ValueOf(invalidValue), len(source)) {
		t.Fatal("invalid map value span was not visited")
	}
}

func TestSyntaxReflectionMapPointerKeysAreRejected(t *testing.T) {
	expressionA := &Expression{Kind: ExpressionNumber, Span: Span{Start: 1, End: 2}}
	expressionB := &Expression{Kind: ExpressionNumber, Span: Span{Start: 1, End: 2}}
	fixture := struct {
		Values map[*Expression]Span
	}{Values: map[*Expression]Span{
		expressionA: {Start: 2, End: 3},
		expressionB: {Start: 3, End: 4},
	}}
	defer func() {
		if recover() == nil {
			t.Fatal("pointer map key was not rejected")
		}
	}()
	_ = syntaxAliasesValue(reflect.ValueOf(fixture))
}

func TestSyntaxReflectionSliceCycle(t *testing.T) {
	cycle := make([]any, 1)
	cycle[0] = cycle
	fixture := struct {
		Cycle []any
	}{Cycle: cycle}
	root := reflect.ValueOf(fixture)
	if !syntaxSpansValidValue(root, 0) {
		t.Fatal("slice cycle changed span validity")
	}
	if aliases := syntaxAliasesValue(root); len(aliases) != 0 {
		t.Fatalf("slice cycle aliases = %v", aliases)
	}
	checkSyntaxSpansValue(t, root, "", func(Span) string { return "" })

	expression := &Expression{Kind: ExpressionNumber, Span: Span{Start: 0, End: 0}}
	sharedSlice := []any{expression}
	sharedMap := map[string]any{"expression": expression}
	shared := struct {
		SliceA []any
		SliceB []any
		MapA   map[string]any
		MapB   map[string]any
	}{SliceA: sharedSlice, SliceB: sharedSlice, MapA: sharedMap, MapB: sharedMap}
	aliases := syntaxAliasesValue(reflect.ValueOf(shared))
	if len(aliases) != 1 || !strings.Contains(aliases[0], ".SliceA[0]") || !strings.Contains(aliases[0], ".SliceB[0]") ||
		!strings.Contains(aliases[0], ".MapA[") || !strings.Contains(aliases[0], ".MapB[") {
		t.Fatalf("shared container aliases = %v", aliases)
	}
}

func TestIncrementalComparisonHelpers(t *testing.T) {
	for _, test := range incrementalEditCases {
		t.Run(test.name, func(t *testing.T) {
			checkIncrementalParser(t, func(_ *File, source string) *File { return Parse(source) }, test)
		})
	}
}

func TestReparseMatchesFullParse(t *testing.T) {
	for _, test := range incrementalEditCases {
		t.Run(test.name, func(t *testing.T) {
			checkIncrementalParser(t, Reparse, test)
		})
	}
}

func TestParseAndReparseClearTemporaryCommandState(t *testing.T) {
	file := Parse(`vim9script
var logical = 1 +
  2
autocmd BufEnter * echo embedded
var callback = (value: number): number => {
  var lambdaLogical = value +
    1
  return lambdaLogical
}
`)
	if file.incremental == nil {
		t.Fatal("full parse missing incremental metadata")
	}
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.logical != nil || command.boundaryExpression != nil {
			t.Fatalf("top-level command %d retained parser temp: %#v", index, command)
		}
	}
	if len(file.Commands) < 4 || file.Commands[2].Embedded == nil || len(file.Commands[2].Embedded.Commands) != 1 {
		t.Fatalf("embedded command = %#v", file.Commands)
	}
	embedded := &file.Commands[2].Embedded.Commands[0]
	if embedded.logical != nil || embedded.boundaryExpression != nil {
		t.Fatalf("embedded command retained parser temp: %#v", embedded)
	}
	if file.Commands[3].Declaration == nil {
		t.Fatalf("lambda declaration = %#v", file.Commands[3])
	}
	lambda := file.Commands[3].Declaration.Initializer
	if lambda == nil || lambda.LambdaBody == nil || len(lambda.LambdaBody.Commands) == 0 {
		t.Fatalf("lambda body = %#v", file.Commands[3].Declaration)
	}
	lambdaCommand := &lambda.LambdaBody.Commands[0]
	if lambdaCommand.logical != nil || lambdaCommand.boundaryExpression != nil {
		t.Fatalf("lambda command retained parser temp: %#v", lambdaCommand)
	}

	old := "vim9script\nvar first = 1\nvar changing = 2\nvar stable = 3\n"
	new := "vim9script\nvar first = 1\nvar changing = 4\nvar stable = 3\n"
	got := checkIncrementalParser(t, Reparse, incrementalEditCase{name: "temporary command state", old: old, new: new})
	if got.incremental == nil || got.incremental.parsed == 0 {
		t.Fatalf("expected incremental reparse, metadata = %#v", got.incremental)
	}
	for index := range got.Commands {
		command := &got.Commands[index]
		if command.logical != nil || command.boundaryExpression != nil {
			t.Fatalf("reparsed command %d retained parser temp: %#v", index, command)
		}
	}
}

func TestReparseBoundaryEdits(t *testing.T) {
	for _, test := range incrementalBoundaryEditCases {
		t.Run(test.name, func(t *testing.T) {
			got := checkIncrementalParser(t, Reparse, test)
			if test.expectReuse && (got.incremental == nil || got.incremental.reused == 0) {
				t.Fatalf("expected reusable suffix, metadata = %#v", got.incremental)
			}
		})
	}
}

func TestReparseWholeDocumentDialectChangeFallsBack(t *testing.T) {
	old := "let one = 1\nlet two = 2"
	new := "vim9script\nvar alpha = 10\nvar beta = 20"
	change := changedSource(old, new)
	if change.Start != 0 || change.OldEnd != len(old) || change.NewEnd != len(new) {
		t.Fatalf("whole replacement change = %#v", change)
	}
	previous := Parse(old)
	if previous.Dialect != Legacy || startsWithVim9Script(old) == startsWithVim9Script(new) {
		t.Fatalf("unexpected dialect setup: old=%v new=%v", previous.Dialect, startsWithVim9Script(new))
	}
	got := checkIncrementalParser(t, Reparse, incrementalEditCase{name: "whole-document dialect fallback", old: old, new: new})
	if got.Dialect != Vim9 {
		t.Fatalf("result dialect = %v", got.Dialect)
	}
	if got.incremental == nil || got.incremental.reused != 0 || got.incremental.parsed != 0 {
		t.Fatalf("expected whole-document dialect fallback, metadata = %#v", got.incremental)
	}
}

func TestReparseLogicalLineBarsKeepUnitBoundaries(t *testing.T) {
	old := "echo one | echo two\nlet after = 1\nlet untouched = 2\n"
	new := "echo one | echo three\nlet after = 1\nlet untouched = 2\n"
	previous := Parse(old)
	if len(previous.incremental.units) == 0 {
		t.Fatal("missing incremental metadata")
	}
	if previous.incremental.units[0].commandCount != 2 {
		t.Fatalf("first unit command count = %d", previous.incremental.units[0].commandCount)
	}
	got := checkIncrementalParser(t, Reparse, incrementalEditCase{name: "logical line bars", old: old, new: new})
	if got.incremental == nil || got.incremental.reused == 0 {
		t.Fatalf("expected unchanged third unit to be reused, metadata = %#v", got.incremental)
	}
}

func TestReparseUnknownCommandRemainsOpaque(t *testing.T) {
	cases := []incrementalEditCase{
		{name: "lowercase unknown command", old: "let value = 1\nfuturecmd arg1\nlet stable = 2\nlet tail = 3\n", new: "let value = 1\nfuturecmd arg2\nlet stable = 2\nlet tail = 3\n"},
		{name: "unknown command with short spelling", old: "let value = 1\nfutco 1\nlet stable = 2\nlet tail = 3\n", new: "let value = 1\nfutco 2\nlet stable = 2\nlet tail = 3\n"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := checkIncrementalParser(t, Reparse, test)
			if got.incremental == nil || got.incremental.reused == 0 {
				t.Fatalf("expected unknown-command edit to reuse suffix, metadata = %#v", got.incremental)
			}
			if len(got.Commands) < 2 {
				t.Fatalf("expected unknown command, got %d commands", len(got.Commands))
			}
			if got.Commands[1].Kind != CommandUnknown || (got.Commands[1].Canonical != "futurecmd" && got.Commands[1].Canonical != "futco") {
				t.Fatalf("unknown command changed: kind=%v canonical=%q", got.Commands[1].Kind, got.Commands[1].Canonical)
			}
		})
	}
}

func TestReparseModifierRangeCommandRemainsParsed(t *testing.T) {
	old := "silent! %foldclose!\nlet stable = 1\nlet tail = 2\n"
	new := "silent! $foldclose!\nlet stable = 1\nlet tail = 2\n"
	got := checkIncrementalParser(t, Reparse, incrementalEditCase{name: "modifier and range", old: old, new: new})
	if got.incremental == nil || got.incremental.reused == 0 {
		t.Fatalf("expected modifier/range edit to reuse suffix, metadata = %#v", got.incremental)
	}
	if len(got.Commands) < 3 {
		t.Fatalf("expected edited command and stable suffix, got %d commands", len(got.Commands))
	}
	command := got.Commands[0]
	if command.Canonical != "foldclose" {
		t.Fatalf("command canonical = %q", command.Canonical)
	}
	if got.Text(command.Range) != "$" {
		t.Fatalf("range text = %q", got.Text(command.Range))
	}
	if len(command.Modifiers) != 1 || command.Modifiers[0].Name != "silent" {
		t.Fatalf("modifiers = %#v", command.Modifiers)
	}
	if got.Text(command.Modifiers[0].Bang) != "!" {
		t.Fatalf("modifier bang = %q", got.Text(command.Modifiers[0].Bang))
	}
	if got.Text(command.Bang) != "!" {
		t.Fatalf("command bang = %q", got.Text(command.Bang))
	}
}

func TestReparseExAbbreviationRemainsParsed(t *testing.T) {
	test := incrementalEditCase{
		name: "setlocal abbreviation", old: "let one = 1\nsetl ts=8\nlet stable = 2\nlet tail = 3\n",
		new: "let one = 1\nsetl ts=16\nlet stable = 2\nlet tail = 3\n",
	}
	got := checkIncrementalParser(t, Reparse, test)
	if got.incremental == nil || got.incremental.reused == 0 {
		t.Fatalf("expected abbreviation edit to reuse suffix, metadata = %#v", got.incremental)
	}
	if len(got.Commands) < 2 || got.Commands[1].Canonical != "setlocal" {
		t.Fatalf("abbreviation command = %#v", got.Commands)
	}
}

func TestReparseReturnsPreviousForIdenticalSource(t *testing.T) {
	previous := Parse("let value = 1\n")
	if got := Reparse(previous, previous.Source); got != previous {
		t.Fatal("identical source did not return previous file")
	}
}

func TestReparsePreservesEmptySliceSemantics(t *testing.T) {
	checkIncrementalParser(t, Reparse, incrementalEditCase{
		name: "commands removed", old: "let value = 1\n", new: "# comment\n",
	})
}

func TestReparseReusesIndependentUnits(t *testing.T) {
	oldSource := "let one = 1\nlet two = 2\nlet three = 3\nlet four = 4\n"
	newSource := "let one = 1\nlet two = 20\nlet three = 3\nlet four = 4\n"
	result := Reparse(Parse(oldSource), newSource)
	if result.incremental == nil || result.incremental.reused == 0 {
		t.Fatalf("incremental metadata = %#v", result.incremental)
	}
	checkIncrementalParser(t, Reparse, incrementalEditCase{name: "reused units", old: oldSource, new: newSource})
}

func TestReparseFallsBackForStatefulSyntax(t *testing.T) {
	tests := []incrementalEditCase{
		{name: "scriptversion", old: "scriptversion 4\nlet value = 1\n", new: "scriptversion 4\nlet value = 2\n"},
		{name: "legacy function", old: "function Func()\n  let value = 1\nendfunction\n", new: "function Func()\n  let value = 2\nendfunction\n"},
		{name: "vim9 def", old: "vim9script\ndef Func()\n  var value = 1\nenddef\n", new: "vim9script\ndef Func()\n  var value = 2\nenddef\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			previous := Parse(test.old)
			if previous.incremental == nil || previous.incremental.eligible {
				t.Fatalf("incremental metadata = %#v", previous.incremental)
			}
			checkIncrementalParser(t, Reparse, test)
		})
	}
}

func TestReparseHundredEdits(t *testing.T) {
	random := rand.New(rand.NewSource(1))
	source := "let one = 1000\nlet two = 2000\nlet three = 3000\nlet four = 4000\n"
	previous := Parse(source)
	for step := 0; step < 100; step++ {
		positions := make([]int, 0)
		for index := range source {
			if source[index] >= '0' && source[index] <= '9' {
				positions = append(positions, index)
			}
		}
		position := positions[random.Intn(len(positions))]
		replacement := byte('0' + random.Intn(10))
		source = source[:position] + string(replacement) + source[position+1:]
		got := Reparse(previous, source)
		want := Parse(source)
		if string(marshalSyntax(t, got)) != string(marshalSyntax(t, want)) {
			t.Fatalf("step %d differs", step)
		}
		checkSyntaxSpans(t, got)
		previous = got
	}
}

func TestParseUnitsKeepMultilineOwnersTogether(t *testing.T) {
	tests := []struct {
		name   string
		source string
		text   string
	}{
		{name: "legacy continuation", source: "let value = [1,\n\\ 2]\necho value\n", text: "let value = [1,\n\\ 2]\n"},
		{name: "vim9 continuation", source: "vim9script\nvar value = [1,\n  2]\necho value\n", text: "var value = [1,\n  2]\n"},
		{name: "heredoc", source: "let value =<< END\none\nEND\necho value\n", text: "let value =<< END\none\nEND\n"},
		{name: "text body", source: "append\none\n.\necho 'after'\n", text: "append\none\n.\n"},
		{name: "keymap", source: "loadkeymap\na b\nc d\n", text: "loadkeymap\na b\nc d\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if file.incremental == nil {
				t.Fatal("missing incremental metadata")
			}
			found := false
			for _, unit := range file.incremental.units {
				if file.Text(unit.span) == test.text {
					found = true
					if unit.independent {
						t.Fatal("multiline unit marked independent")
					}
				}
			}
			if !found {
				t.Fatalf("units = %#v", file.incremental.units)
			}
		})
	}
}

func TestReparseFallsBackForLeadingContinuation(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
	}{
		{
			name: "legacy slash",
			old:  "echo 1\nlet stable = 2\nlet tail = 3\n",
			new:  "echo 1\n\\ 2\nlet stable = 2\nlet tail = 3\n",
		},
		{
			name: "legacy continuation comment",
			old:  "echo 1\nlet stable = 2\nlet tail = 3\n",
			new:  "echo 1\n\"\\ comment\nlet stable = 2\nlet tail = 3\n",
		},
		{
			name: "vim9 leading bar",
			old:  "vim9script\necho 1\nvar stable = 2\nvar tail = 3\n",
			new:  "vim9script\necho 1\n| echo 2\nvar stable = 2\nvar tail = 3\n",
		},
		{
			name: "vim9 continuation comment",
			old:  "vim9script\necho 1\nvar stable = 2\nvar tail = 3\n",
			new:  "vim9script\necho 1\n#\\ comment\nvar stable = 2\nvar tail = 3\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := checkIncrementalParser(t, Reparse, incrementalEditCase{name: test.name, old: test.old, new: test.new})
			if len(got.Diagnostics) != 0 {
				t.Fatalf("unexpected diagnostics: %#v", got.Diagnostics)
			}
			if got.incremental == nil || got.incremental.reused != 0 || got.incremental.parsed != 0 {
				t.Fatalf("expected full fallback, metadata = %#v", got.incremental)
			}
		})
	}
}

func TestLeadingContinuationDoesNotMatchDoubleBar(t *testing.T) {
	if leadingContinuation("|| echo 2", 0, len("|| echo 2"), Vim9) {
		t.Fatal("ordinary Vim9 || must not be treated as a leading-bar continuation")
	}
}

func TestParseUnitScannerState(t *testing.T) {
	file := Parse("scriptversion 4\ndef Func()\n  var value = 1\nenddef\necho 'after'\n")
	if len(file.incremental.units) < 5 {
		t.Fatalf("units = %#v", file.incremental.units)
	}
	def := file.incremental.units[1]
	if def.entry.scriptVersion != 4 || def.exit.activeDialect != Vim9 || len(def.exit.dialectStack) != 1 {
		t.Fatalf("def state = entry %#v exit %#v", def.entry, def.exit)
	}
	enddef := file.incremental.units[3]
	if enddef.exit.activeDialect != Legacy || len(enddef.exit.dialectStack) != 0 {
		t.Fatalf("enddef exit = %#v", enddef.exit)
	}
}

func TestReparseConcurrentReaders(t *testing.T) {
	oldSource := "let one = 1000\nlet two = 2000\nlet three = 3000\nlet four = 4000\n"
	previous := Parse(oldSource)
	before := marshalSyntax(t, previous)
	var wait sync.WaitGroup
	for digit := byte('1'); digit <= '8'; digit++ {
		wait.Add(1)
		go func(digit byte) {
			defer wait.Done()
			source := oldSource[:42] + string(digit) + oldSource[43:]
			got := Reparse(previous, source)
			want := Parse(source)
			gotJSON, gotError := json.Marshal(got)
			wantJSON, wantError := json.Marshal(want)
			if gotError != nil || wantError != nil || string(gotJSON) != string(wantJSON) {
				t.Errorf("digit %q differs: got error %v, want error %v", digit, gotError, wantError)
			}
		}(digit)
	}
	wait.Wait()
	if after := marshalSyntax(t, previous); string(after) != string(before) {
		t.Fatal("concurrent reparses changed previous file")
	}
}

func TestReparseInvalidByteSourceDoesNotHang(t *testing.T) {
	oldSource := "0?\x01N\xdfst"
	newSource := oldSource[:4] + "A\\" + oldSource[7:]

	previous := Parse(oldSource)
	if previous.incremental == nil {
		t.Fatal("missing old metadata")
	}
	got := Reparse(previous, newSource)
	if got == nil || got.incremental == nil || got.incremental.eligible {
		t.Fatalf("new metadata = %#v", got.incremental)
	}
	if string(marshalSyntax(t, got)) != string(marshalSyntax(t, Parse(newSource))) {
		t.Fatal("invalid byte source differs from full parse")
	}
}

func TestReparseOfficialCorpusEdits(t *testing.T) {
	corpus := readGeneratedOfficialCorpus(t)
	for _, test := range corpus.Cases {
		edits := []string{" " + test.Source, test.Source + "\n"}
		if len(test.Source) > 0 {
			middle := len(test.Source) / 2
			edits = append(edits, test.Source[:middle]+" "+test.Source[middle+1:])
		}
		previous := Parse(test.Source)
		for index, source := range edits {
			got, want := Reparse(previous, source), Parse(source)
			gotJSON, wantJSON := marshalSyntax(t, got), marshalSyntax(t, want)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("%s edit %d differs\ngot:  %s\nwant: %s", test.Origin, index, gotJSON, wantJSON)
			}
			checkSyntaxSpans(t, got)
		}
	}
}

func FuzzIncrementalParse(f *testing.F) {
	seeds := []string{
		"let one = 1\nlet two = 2\nlet three = 3\n",
		"vim9script\nvar one = 1\nvar two = 2\necho one + two\n",
		"vim9script\ndef Func()\n  var value = [1,\n    2]\nenddef\n",
		"let value =<< END\none\nEND\necho value\n",
		"if true\n  finish\nendif\necho 'after'\n",
	}
	for _, source := range seeds {
		f.Add(source, uint64(len(source)/2), uint64(len(source)/2), "x")
	}
	f.Fuzz(func(t *testing.T, oldSource string, rawStart, rawEnd uint64, replacement string) {
		if len(oldSource)+len(replacement) > 64<<10 {
			t.Skip()
		}
		start := int(rawStart % uint64(len(oldSource)+1))
		end := int(rawEnd % uint64(len(oldSource)+1))
		if start > end {
			start, end = end, start
		}
		newSource := oldSource[:start] + replacement + oldSource[end:]
		checkIncrementalParser(t, Reparse, incrementalEditCase{name: "fuzz", old: oldSource, new: newSource})
	})
}
