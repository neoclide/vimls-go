package syntax

import (
	"strings"
	"testing"
)

func TestExecuteStaticHeredocIsOpaqueAndKeepsExpressions(t *testing.T) {
	source := "let g:prefix = '😀'\n" +
		"function! s:update_python()\n" +
		"  let py_exe = 'python3'\n" +
		"  execute py_exe \"<< EOF\"\n" +
		"  try:\n" +
		"    class NotVim(Exception):\n" +
		"      def run(self):\n" +
		"        if True:\n" +
		"          return 1\n" +
		"EOF\n" +
		"  endfunction\n" +
		"let g:after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 6 || len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockFunction {
		t.Fatalf("commands = %#v, blocks = %#v", file.Commands, file.Blocks)
	}
	execute := &file.Commands[3]
	if execute.Canonical != "execute" || execute.Heredoc == nil || execute.Heredoc.Marker != "EOF" || execute.Heredoc.Trim {
		t.Fatalf("execute = %#v", execute)
	}
	if got := file.Text(execute.Heredoc.Body); got != "  try:\n    class NotVim(Exception):\n      def run(self):\n        if True:\n          return 1" {
		t.Fatalf("heredoc body = %q", got)
	}
	if got := file.Text(execute.Heredoc.EndMarker); got != "EOF" {
		t.Fatalf("heredoc end marker = %q", got)
	}
	if len(execute.Expressions) != 2 || execute.Expressions[0].Kind != ExpressionIdentifier || execute.Expressions[1].Kind != ExpressionString {
		t.Fatalf("execute expressions = %#v", execute.Expressions)
	}
	if file.Text(execute.Expressions[0].Span) != "py_exe" || file.Text(execute.Expressions[1].Span) != "\"<< EOF\"" {
		t.Fatalf("execute expression spans = %q, %q", file.Text(execute.Expressions[0].Span), file.Text(execute.Expressions[1].Span))
	}
	if file.Commands[4].Canonical != "endfunction" || file.Blocks[0].End != 4 || file.Commands[5].Canonical != "let" {
		t.Fatalf("marker recovery commands = %#v, block = %#v", file.Commands, file.Blocks[0])
	}
	if bodyStart := strings.Index(source, "  try:\n"); execute.Heredoc.Body.Start != bodyStart {
		t.Fatalf("body start = %d, want %d", execute.Heredoc.Body.Start, bodyStart)
	}
	if markerStart := strings.Index(source, "EOF\n"); execute.Heredoc.EndMarker.Start != markerStart {
		t.Fatalf("marker start = %d, want %d", execute.Heredoc.EndMarker.Start, markerStart)
	}
	assertFileSpans(t, file)
}

func TestExecuteDynamicHeredocIsNotGuessed(t *testing.T) {
	source := "function! s:dynamic()\n" +
		"  execute py_exe ('<< ' .. marker)\n" +
		"  if true\n" +
		"    echo 'inside'\n" +
		"  endif\n" +
		"endfunction\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 6 || len(file.Blocks) != 2 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	if file.Commands[1].Canonical != "execute" || file.Commands[1].Heredoc != nil || len(file.Commands[1].Expressions) != 1 {
		t.Fatalf("dynamic execute = %#v", file.Commands[1])
	}
	if file.Commands[2].Canonical != "if" || file.Commands[3].Canonical != "echo" || file.Commands[4].Canonical != "endif" || file.Commands[5].Canonical != "endfunction" {
		t.Fatalf("commands after dynamic execute = %#v", file.Commands)
	}
	assertFileSpans(t, file)
}

func TestHeredocPayloadIsOpaqueSource(t *testing.T) {
	source := "vim9script\nvar text =<< trim END\n  one | not a command\n  # payload, not a Vim9 comment\nEND\necho text\n"
	file := Parse(source)
	if len(file.Commands) != 3 {
		t.Fatalf("commands = %#v", file.Commands)
	}
	heredoc := file.Commands[1].Heredoc
	if heredoc == nil || heredoc.Marker != "END" || !heredoc.Trim {
		t.Fatalf("heredoc = %#v", heredoc)
	}
	if file.Text(heredoc.Body) != "  one | not a command\n  # payload, not a Vim9 comment" || file.Text(heredoc.EndMarker) != "END" {
		t.Fatalf("body = %q, end = %q", file.Text(heredoc.Body), file.Text(heredoc.EndMarker))
	}
	if len(file.Commands[1].Expressions) != 0 || countTokens(file, TokenHeredoc) != 3 {
		t.Fatalf("heredoc command = %#v, tokens = %#v", file.Commands[1], file.Tokens)
	}
}

func TestEmptyHeredocBodyHasSourceLocalZeroWidthSpan(t *testing.T) {
	source := "let g:text =<< END\nEND\nlet g:after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[0].Heredoc == nil {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	heredoc := file.Commands[0].Heredoc
	markerStart := strings.Index(source, "END\nlet")
	if heredoc.Body != (Span{Start: markerStart, End: markerStart}) || file.Text(heredoc.EndMarker) != "END" {
		t.Fatalf("heredoc = %#v", heredoc)
	}
	assertFileSpans(t, file)
}

func TestTrimHeredocMarkerMatchesCommandIndentExactly(t *testing.T) {
	source := "vim9script\n\tvar text =<< trim END\n END\n\tEND \n\tEND\necho text\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	heredoc := file.Commands[1].Heredoc
	if heredoc == nil || !heredoc.Trim || heredoc.Incomplete {
		t.Fatalf("heredoc = %#v", heredoc)
	}
	if got := file.Text(heredoc.Body); got != " END\n\tEND " || file.Text(heredoc.EndMarker) != "\tEND" {
		t.Fatalf("body = %q, end = %q", got, file.Text(heredoc.EndMarker))
	}
	if file.Commands[2].Canonical != "echo" {
		t.Fatalf("following command = %#v", file.Commands[2])
	}
	assertFileSpans(t, file)
}

func TestHeredocHeaderFlagsAndValidation(t *testing.T) {
	for _, header := range []string{"trim eval", "eval trim"} {
		file := (LegacyParser{}).Parse("let g:text =<< " + header + " END\nvalue\nEND\n")
		if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Heredoc == nil || !file.Commands[0].Heredoc.Trim || !file.Commands[0].Heredoc.Eval {
			t.Fatalf("header = %q, command = %#v, diagnostics = %#v", header, file.Commands, file.Diagnostics)
		}
	}

	for _, test := range []struct {
		header string
		code   string
	}{
		{header: "END extra", code: "vim/E488"},
		{header: "lower", code: "vim/E221"},
		{header: "trim", code: "vim/E172"},
	} {
		file := (LegacyParser{}).Parse("let g:text =<< " + test.header + "\nlet g:after = 1\n")
		if len(file.Commands) != 2 || file.Commands[0].Heredoc != nil || !hasDiagnostic(file, test.code) {
			t.Fatalf("header = %q, commands = %#v, diagnostics = %#v", test.header, file.Commands, file.Diagnostics)
		}
		if file.Commands[1].Canonical != "let" || file.Commands[1].Declaration == nil {
			t.Fatalf("header = %q, following command = %#v", test.header, file.Commands[1])
		}
		assertFileSpans(t, file)
	}
}

func TestEmbeddedLanguageHeredocAndMissingEndRecover(t *testing.T) {
	complete := (LegacyParser{}).Parse("python3 << PY\nprint('not Vim')\nPY\nlet g:after = 1\n")
	if len(complete.Commands) != 2 || complete.Commands[0].Heredoc == nil || complete.Commands[1].Canonical != "let" {
		t.Fatalf("complete = %#v", complete)
	}
	incomplete := (LegacyParser{}).Parse("let g:text =<< END\npayload\n")
	if len(incomplete.Commands) != 1 || incomplete.Commands[0].Heredoc == nil || !incomplete.Commands[0].Heredoc.Incomplete || len(incomplete.Diagnostics) != 1 || incomplete.Diagnostics[0].Code != "vim/E990" {
		t.Fatalf("incomplete = %#v", incomplete)
	}
}

func TestEmbeddedLanguageHeredocDefaultsToDotMarker(t *testing.T) {
	file := (LegacyParser{}).Parse("python3 <<\nprint('not Vim')\n.\nlet g:after = 1\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	heredoc := file.Commands[0].Heredoc
	if heredoc == nil || heredoc.Marker != "." || file.Text(heredoc.Body) != "print('not Vim')" || file.Text(heredoc.EndMarker) != "." {
		t.Fatalf("heredoc = %#v", heredoc)
	}
	if file.Commands[1].Canonical != "let" {
		t.Fatalf("following command = %#v", file.Commands[1])
	}
}

func TestPythonInterpreterAliasesConsumeHeredoc(t *testing.T) {
	for _, command := range []string{"python", "py3", "python3", "pyx", "pythonx"} {
		source := command + " << PY\nprint('not Vim')\nPY\nlet g:after = 1\n"
		file := (LegacyParser{}).Parse(source)
		if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[0].Canonical != command || file.Commands[0].Heredoc == nil || file.Commands[1].Canonical != "let" {
			t.Fatalf("%s commands = %#v, diagnostics = %#v", command, file.Commands, file.Diagnostics)
		}
		if got := file.Text(file.Commands[0].Heredoc.Body); got != "print('not Vim')" {
			t.Fatalf("%s heredoc body = %q", command, got)
		}
	}
}

func TestOfficialVim9CommandBlockWithHeredoc(t *testing.T) {
	// v9.2.1015 runtime/doc/vim9.txt *command-block* and
	// src/testdir/test_vim9_script.vim Test_command_block_heredoc.
	source := "vim9script\ncommand SomeCommand {\n  g:someVar =<< trim END\n    aaa\n    bbb\n  END\n}\nSomeCommand\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 || len(file.Blocks) != 1 || file.Blocks[0].Kind != BlockCommand {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	heredoc := file.Commands[2].Heredoc
	if file.Commands[2].Kind != CommandExpression || heredoc == nil || heredoc.Marker != "END" || file.Text(heredoc.Body) != "    aaa\n    bbb" || file.Commands[3].Kind != CommandBlockEnd {
		t.Fatalf("assignment = %#v, end = %#v", file.Commands[2], file.Commands[3])
	}
}

func TestCommandBlockDefersMissingHeredocMarkerToExecution(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_script.vim Test_command_block_heredoc
	// defines this successfully and catches E990 only when SomeCommand runs.
	source := "vim9script\ncommand SomeCommand {\n  g:someVar =<< trim END\n    value\n}\nvar after = 1\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 || len(file.Blocks) != 1 || file.Blocks[0].End != 3 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	heredoc := file.Commands[2].Heredoc
	if heredoc == nil || !heredoc.Incomplete || file.Text(heredoc.Body) != "    value\n}" || heredoc.EndMarker != (Span{}) {
		t.Fatalf("heredoc = %#v, body = %q", heredoc, file.Text(heredoc.Body))
	}
	if file.Commands[3].Kind != CommandBlockEnd || file.Commands[4].Canonical != "var" || file.Commands[4].Declaration == nil {
		t.Fatalf("following commands = %#v", file.Commands[3:])
	}
	assertFileSpans(t, file)
}

func TestCommandBlockClosingBraceIsNotADeferredHeredocMarker(t *testing.T) {
	// may_get_cmd_block() does not parse heredocs while collecting a user
	// command definition. The first line-leading brace closes the block even
	// when the stored command would use that same text as its runtime marker.
	file := Parse("vim9script\ncommand SomeCommand {\n  g:value =<< }\n}\nvar after = 1\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 || len(file.Blocks) != 1 || file.Blocks[0].End != 3 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	heredoc := file.Commands[2].Heredoc
	if heredoc == nil || heredoc.Marker != "}" || !heredoc.Incomplete || file.Text(heredoc.Body) != "}" || heredoc.EndMarker != (Span{}) || file.Commands[3].Kind != CommandBlockEnd || file.Commands[4].Declaration == nil {
		t.Fatalf("heredoc = %#v, following commands = %#v", heredoc, file.Commands[3:])
	}
	assertFileSpans(t, file)
}

func TestCommandBlockClosesBeforeTrailingReplacementText(t *testing.T) {
	file := Parse("vim9script\ncommand SomeCommand {\n  echo 'inside'\n  } trailing runtime text\nvar after = 1\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 || len(file.Blocks) != 1 || file.Blocks[0].End != 3 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	if file.Commands[3].Kind != CommandBlockEnd || file.Commands[3].ScriptVersion != 1 || file.Text(file.Commands[3].Name) != "}" || file.Commands[4].Declaration == nil {
		t.Fatalf("following commands = %#v", file.Commands[3:])
	}
	if countTokens(file, TokenOpaque) != 1 {
		t.Fatalf("tokens = %#v", file.Tokens)
	}
	assertFileSpans(t, file)
}

func TestHeredocBodyKeepsClosingBraceOutsideCommandBlock(t *testing.T) {
	file := Parse("vim9script\ndef F()\n  var text =<< END\n}\nEND\nenddef\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 || len(file.Blocks) != 1 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	heredoc := file.Commands[2].Heredoc
	if heredoc == nil || file.Text(heredoc.Body) != "}" || file.Text(heredoc.EndMarker) != "END" || file.Commands[3].Canonical != "enddef" {
		t.Fatalf("heredoc = %#v, commands = %#v", heredoc, file.Commands)
	}
	assertFileSpans(t, file)
}

func TestIncompleteFunctionHeredocRecoversAtFunctionEnd(t *testing.T) {
	for _, test := range []struct {
		name   string
		parser func(string) *File
		source string
		end    string
		count  int
		body   int
		close  int
		after  int
	}{
		{
			name:   "legacy function",
			parser: func(source string) *File { return (LegacyParser{}).Parse(source) },
			source: "function! F()\n  let text =<< END\npayload | still text\nendfunction\nlet g:after = 1\n",
			end:    "endfunction",
			count:  4,
			body:   1,
			close:  2,
			after:  3,
		},
		{
			name:   "Vim9 def",
			parser: Parse,
			source: "vim9script\ndef F()\n  var text =<< END\npayload | still text\nenddef\nvar after = 1\n",
			end:    "enddef",
			count:  5,
			body:   2,
			close:  3,
			after:  4,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := test.parser(test.source)
			if len(file.Commands) != test.count || len(file.Blocks) != 1 || file.Blocks[0].End != test.close || len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1145" {
				t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
			}
			heredoc := file.Commands[test.body].Heredoc
			if heredoc == nil || !heredoc.Incomplete || file.Text(heredoc.Body) != "payload | still text" || heredoc.EndMarker != (Span{}) {
				t.Fatalf("heredoc = %#v, body = %q", heredoc, file.Text(heredoc.Body))
			}
			if file.Commands[test.close].Canonical != test.end || file.Commands[test.after].Declaration == nil {
				t.Fatalf("following commands = %#v", file.Commands[test.close:])
			}
			assertFileSpans(t, file)
		})
	}
}

func TestCompleteFunctionHeredocCanContainFunctionEndLine(t *testing.T) {
	source := "function! F()\n  let text =<< END\nendfunction\nstill text\nEND\nendfunction\nlet g:after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 || len(file.Blocks) != 1 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	heredoc := file.Commands[1].Heredoc
	if heredoc == nil || heredoc.Incomplete || file.Text(heredoc.Body) != "endfunction\nstill text" || file.Text(heredoc.EndMarker) != "END" {
		t.Fatalf("heredoc = %#v, body = %q", heredoc, file.Text(heredoc.Body))
	}
	if file.Commands[2].Canonical != "endfunction" || file.Commands[3].Canonical != "let" {
		t.Fatalf("following commands = %#v", file.Commands[2:])
	}
	assertFileSpans(t, file)
}

func TestModifiedHeredocInsideFunctionDoesNotConsumeDefinitionLines(t *testing.T) {
	file := Parse("vim9script\ndef F()\n  legacy let text =<< END\npayload\nEND\nenddef\nvar after = 1\n")
	if len(file.Commands) != 7 || len(file.Blocks) != 1 || file.Blocks[0].End != 5 {
		t.Fatalf("commands = %#v, blocks = %#v, diagnostics = %#v", file.Commands, file.Blocks, file.Diagnostics)
	}
	heredoc := file.Commands[2].Heredoc
	if heredoc == nil || !heredoc.Deferred || heredoc.Incomplete || heredoc.Body != (Span{}) || heredoc.EndMarker != (Span{}) {
		t.Fatalf("heredoc = %#v", heredoc)
	}
	if file.Commands[3].TypedName != "payload" || file.Commands[4].TypedName != "END" || file.Commands[5].Canonical != "enddef" || file.Commands[6].Declaration == nil {
		t.Fatalf("following commands = %#v", file.Commands[3:])
	}
	assertFileSpans(t, file)
}

func TestOfficialHeredocAssignmentIgnoresStringContents(t *testing.T) {
	// v9.2.1015 src/testdir/test_vim9_assign.vim Test_assignment_vim9script.
	source := "vim9script\nvar text = ['']\ntext[ : ] =<< trim TEXT\n  var foo =<< trim FOO\nTEXT\nassert_equal(['var foo =<< trim FOO'], text)\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 || file.Commands[2].Heredoc == nil || file.Commands[3].Heredoc != nil {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
}
