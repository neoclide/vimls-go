package syntax

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/vimdata"
)

func TestFilePlusCommandBoundary(t *testing.T) {
	for _, prefix := range []string{"", "vim9script\n"} {
		for _, test := range []struct{ command, argument string }{
			{"new", `+setlocal\ previewwindow|setlocal\ buftype=nofile|setlocal\ noswapfile|setlocal\ wrap [Document]`},
			{"edit", `++enc=utf-8 ++ff=unix +setlocal\ ts=2|setlocal\ sw=2 name.vim`},
			{"split", `+echo\ '字|符' name.vim`},
			{"buffer", `+echo\ 1|echo\ 2 1`},
			{"next", `+setlocal\ nowrap name.vim`},
			{"new", `+ name.vim`},
			{"new", `+`},
		} {
			t.Run(prefix+test.command+test.argument, func(t *testing.T) {
				source := prefix + "keepalt " + test.command + " " + test.argument + " | echo 123\n"
				file := Parse(source)
				commands := file.Commands
				if prefix != "" {
					commands = commands[1:]
				}
				if len(file.Diagnostics) != 0 || len(commands) != 2 || commands[0].Canonical != test.command || commands[1].Canonical != "echo" {
					t.Fatalf("commands = %#v; diagnostics = %#v", commands, file.Diagnostics)
				}
				if file.Text(commands[0].Argument) != test.argument || file.Text(commands[1].Argument) != "123" || commands[1].Name.Start != strings.LastIndex(source, "echo") {
					t.Fatalf("original spans lost: %#v", commands)
				}
			})
		}
	}
}

func TestFileCommandPrefixFilterBoundaries(t *testing.T) {
	for _, test := range []struct {
		command, source string
		bang            bool
		want            int
	}{
		{"read", "+echo\\ 1|echo\\ 2", true, 0},
		{"read", "!echo +foo|cat", false, 0},
		{"write", "!echo +foo|cat", false, 0},
		{"echo", "+foo|echo 2", false, 0},
	} {
		metadata, _ := vimdata.Lookup(test.command)
		parsed := &Command{}
		if test.bang {
			parsed.Bang = Span{Start: 1, End: 2}
		}
		if got := skipFileCommandPrefix(test.source, 0, len(test.source), metadata, parsed); got != test.want {
			t.Fatalf("%s prefix ends at %d, want %d", test.command, got, test.want)
		}
	}
}

func TestLegacyCommandBoundaryOneExpressionCommentsAndBars(t *testing.T) {
	file := (LegacyParser{}).Parse("if !s:f() \" comment | not a command\n" +
		"endif | while 1 \" comment | not a command\n" +
		"endwhile\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if len(file.Commands) != 4 || countTokens(file, TokenSeparator) != 1 || countTokens(file, TokenComment) != 2 {
		t.Fatalf("commands = %#v, tokens = %#v", file.Commands, file.Tokens)
	}
	for index, want := range []string{"if", "endif", "while", "endwhile"} {
		if file.Commands[index].Canonical != want {
			t.Fatalf("command %d = %#v, want %q", index, file.Commands[index], want)
		}
	}
	if got := file.Text(file.Commands[0].Argument); got != "!s:f()" {
		t.Fatalf("if argument = %q", got)
	}
	if got := file.Text(file.Commands[2].Argument); got != "1" {
		t.Fatalf("while argument = %q", got)
	}
}

func TestLegacyCommandBoundaryOneExpressionAST(t *testing.T) {
	source := "if !s:f() | echo 'done' | endif\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || countTokens(file, TokenSeparator) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	expression := file.Commands[0].Expressions
	if len(expression) != 1 || expression[0].Kind != ExpressionUnary || expression[0].Value != "!" || len(expression[0].Children) != 1 || expression[0].Children[0].Kind != ExpressionCall {
		t.Fatalf("if expression = %#v", expression)
	}
	wantStart := strings.Index(source, "!")
	wantEnd := strings.Index(source, "()") + len("()")
	if expression[0].Span != (Span{Start: wantStart, End: wantEnd}) || file.Text(expression[0].Span) != "!s:f()" {
		t.Fatalf("if expression span = %#v, text = %q", expression[0].Span, file.Text(expression[0].Span))
	}
	assertFileSpans(t, file)
}

func TestLegacyCommandBoundaryMultipleExpressionStrings(t *testing.T) {
	file := (LegacyParser{}).Parse("echo \"a|b\" \"second\" | execute \"echo |\" \"tail\" | echomsg 'done'\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || countTokens(file, TokenSeparator) != 2 || countTokens(file, TokenComment) != 0 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	if len(file.Commands[0].Expressions) != 2 || file.Text(file.Commands[0].Expressions[0].Span) != "\"a|b\"" || file.Text(file.Commands[0].Expressions[1].Span) != "\"second\"" {
		t.Fatalf("echo expressions = %#v", file.Commands[0].Expressions)
	}
	if len(file.Commands[1].Expressions) != 2 || file.Text(file.Commands[1].Expressions[0].Span) != "\"echo |\"" || file.Text(file.Commands[1].Expressions[1].Span) != "\"tail\"" {
		t.Fatalf("execute expressions = %#v", file.Commands[1].Expressions)
	}
}

func TestExpressionListLogicalContinuationKeepsAbsoluteSpans(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		parse     func(string) *File
		command   int
		separator int
	}{
		{
			name:    "legacy",
			source:  "echo get(g:,\n  \\ 'enabled', 0) len([\n  \\ 1, 2]) | echomsg 'done'\n",
			parse:   func(source string) *File { return (LegacyParser{}).Parse(source) },
			command: 0, separator: 1,
		},
		{
			name:    "vim9",
			source:  "vim9script\necho [1,\n  2] [3,\n  4] | echomsg 'done'\n",
			parse:   func(source string) *File { return (Vim9Parser{}).Parse(source) },
			command: 1, separator: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			if len(file.Diagnostics) != 0 || len(file.Commands) != test.separator+1 || countTokens(file, TokenSeparator) != 1 {
				t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
			}
			command := file.Commands[test.command]
			if command.Canonical != "echo" || len(command.Expressions) != 2 || file.Commands[test.separator].Canonical != "echomsg" {
				t.Fatalf("echo = %#v, following = %#v", command, file.Commands[test.separator])
			}
			for _, expression := range command.Expressions {
				if expression.Span.Start < command.Argument.Start || expression.Span.End > command.Argument.End || file.Text(expression.Span) == "" {
					t.Fatalf("expression = %#v, argument = %#v", expression, command.Argument)
				}
			}
			assertFileSpans(t, file)
		})
	}
}

func TestMalformedSecondExpressionRecoversAtNextPhysicalLine(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		parse     func(string) *File
		command   int
		following int
	}{
		{
			name: "legacy", source: "echo 1 ) | echo 'same-line'\nlet g:after = 1\n",
			parse:   func(source string) *File { return (LegacyParser{}).Parse(source) },
			command: 0, following: 1,
		},
		{
			name: "vim9", source: "vim9script\necho 1 ) | echo 'same-line'\nvar after = 1\n",
			parse:   func(source string) *File { return (Vim9Parser{}).Parse(source) },
			command: 1, following: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			if len(file.Commands) != test.following+1 || countTokens(file, TokenSeparator) != 0 || !hasDiagnostic(file, "vimls/unexpected-token") {
				t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
			}
			broken := file.Commands[test.command]
			if broken.Canonical != "echo" || len(broken.Expressions) != 2 || file.Text(broken.Argument) != "1 ) | echo 'same-line'" {
				t.Fatalf("broken expression list = %#v", broken)
			}
			if file.Commands[test.following].Declaration == nil {
				t.Fatalf("following command = %#v", file.Commands[test.following])
			}
			assertFileSpans(t, file)
		})
	}
}

func TestExpressionListUsesOneCommandDialectOverride(t *testing.T) {
	file := Parse("vim9script\nlegacy echo 1 \"text\" | vim9cmd echo 2 # comment\nvar after = 1\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 || countTokens(file, TokenSeparator) != 1 || countTokens(file, TokenComment) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	legacy := file.Commands[1]
	vim9 := file.Commands[2]
	if legacy.Canonical != "echo" || legacy.Dialect != Legacy || len(legacy.Expressions) != 2 || legacy.Expressions[1].Kind != ExpressionString {
		t.Fatalf("legacy echo = %#v", legacy)
	}
	if vim9.Canonical != "echo" || vim9.Dialect != Vim9 || len(vim9.Expressions) != 1 || vim9.Expressions[0].Value != "2" {
		t.Fatalf("Vim9 echo = %#v", vim9)
	}
	if file.Commands[3].Declaration == nil || file.Text(file.Commands[3].Declaration.Name) != "after" {
		t.Fatalf("following declaration = %#v", file.Commands[3])
	}
	assertFileSpans(t, file)
}

func TestDeclarationInitializerBoundaryPreservesPublicAST(t *testing.T) {
	tests := []struct {
		name   string
		source string
		parse  func(string) *File
		index  int
		text   string
	}{
		{
			name:   "legacy bar",
			source: "let g:value = get(g:, 'value', 0) | echo 'after'\n",
			parse:  func(source string) *File { return (LegacyParser{}).Parse(source) },
			index:  0,
			text:   "get(g:, 'value', 0)",
		},
		{
			name:   "vim9 comment",
			source: "vim9script\nvar value = get(g:, 'value', 0) # comment | not a command\nvar after = 1\n",
			parse:  func(source string) *File { return (Vim9Parser{}).Parse(source) },
			index:  1,
			text:   "get(g:, 'value', 0)",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			command := file.Commands[test.index]
			if len(file.Diagnostics) != 0 || command.Declaration == nil || command.Declaration.Initializer == nil {
				t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
			}
			initializer := command.Declaration.Initializer
			if initializer.Kind != ExpressionCall || file.Text(initializer.Span) != test.text {
				t.Fatalf("initializer = %#v, text = %q", initializer, file.Text(initializer.Span))
			}
			if len(command.Expressions) != 1 || command.Expressions[0].Kind != ExpressionAssignment || len(command.Expressions[0].Children) != 2 {
				t.Fatalf("assignment expressions = %#v", command.Expressions)
			}
			if test.name == "legacy bar" && (len(file.Commands) != 2 || file.Commands[1].Canonical != "echo") {
				t.Fatalf("legacy bar commands = %#v", file.Commands)
			}
			if test.name == "vim9 comment" && (len(file.Commands) != 3 || file.Commands[2].Declaration == nil) {
				t.Fatalf("vim9 comment commands = %#v", file.Commands)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestDeclarationInitializerLogicalContinuationKeepsAbsoluteSpans(t *testing.T) {
	tests := []struct {
		name   string
		source string
		parse  func(string) *File
		index  int
	}{
		{
			name:   "legacy",
			source: "let g:value = get(g:,\n  \\ 'value', 0) | echo 'after'\n",
			parse:  func(source string) *File { return (LegacyParser{}).Parse(source) },
		},
		{
			name:   "vim9",
			source: "vim9script\nvar value = [1,\n  2] | echo 'after'\n",
			parse:  func(source string) *File { return (Vim9Parser{}).Parse(source) },
			index:  1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			command := file.Commands[test.index]
			if len(file.Diagnostics) != 0 || command.Declaration == nil || command.Declaration.Initializer == nil {
				t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
			}
			initializer := command.Declaration.Initializer
			if initializer.Span.Start < command.Argument.Start || initializer.Span.End > command.Argument.End || file.Text(initializer.Span) == "" {
				t.Fatalf("initializer = %#v, argument = %#v, text = %q", initializer, command.Argument, file.Text(initializer.Span))
			}
			if file.Commands[len(file.Commands)-1].Canonical != "echo" {
				t.Fatalf("commands = %#v", file.Commands)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestDeclarationInitializerMalformedRHSRecoversNextPhysicalLine(t *testing.T) {
	tests := []struct {
		name   string
		source string
		parse  func(string) *File
		index  int
		follow int
	}{
		{
			name:   "legacy",
			source: "let g:value = (1 | echo 'same-line'\nlet g:after = 1\n",
			parse:  func(source string) *File { return (LegacyParser{}).Parse(source) },
			index:  0,
			follow: 1,
		},
		{
			name:   "vim9",
			source: "vim9script\nvar value = (1 | echo 'same-line'\nvar after = 1\n",
			parse:  func(source string) *File { return (Vim9Parser{}).Parse(source) },
			index:  1,
			follow: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			if len(file.Commands) != test.follow+1 || countTokens(file, TokenSeparator) != 0 || len(file.Diagnostics) == 0 {
				t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
			}
			broken := file.Commands[test.index]
			if broken.Declaration == nil || broken.Declaration.Initializer == nil || file.Text(broken.Argument) == "" {
				t.Fatalf("broken declaration = %#v", broken)
			}
			if file.Commands[test.follow].Declaration == nil {
				t.Fatalf("following declaration = %#v", file.Commands[test.follow])
			}
			assertFileSpans(t, file)
		})
	}
}

func TestEmptyAssignmentRHSSuppressesSameLineTail(t *testing.T) {
	tests := []struct {
		name   string
		source string
		parse  func(string) *File
		broken int
		follow int
	}{
		{
			name:   "legacy declaration",
			source: "let g:value = | echo 'same-line'\nlet g:after = 1\n",
			parse:  func(source string) *File { return (LegacyParser{}).Parse(source) },
			follow: 1,
		},
		{
			name:   "Vim9 declaration",
			source: "vim9script\nvar value = | echo 'same-line'\nvar after = 1\n",
			parse:  func(source string) *File { return (Vim9Parser{}).Parse(source) },
			broken: 1,
			follow: 2,
		},
		{
			name:   "Vim9 command-start assignment",
			source: "vim9script\nvalue = | echo 'same-line'\nvar after = 1\n",
			parse:  func(source string) *File { return (Vim9Parser{}).Parse(source) },
			broken: 1,
			follow: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			if len(file.Commands) != test.follow+1 || len(file.Diagnostics) == 0 || countTokens(file, TokenSeparator) != 0 {
				t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
			}
			if got := file.Text(file.Commands[test.broken].Argument); !strings.Contains(got, "| echo 'same-line'") {
				t.Fatalf("broken argument = %q", got)
			}
			if file.Commands[test.follow].Declaration == nil {
				t.Fatalf("following declaration = %#v", file.Commands[test.follow])
			}
			assertFileSpans(t, file)
		})
	}
}

func TestDeclarationHeredocKeepsInitializerOpaque(t *testing.T) {
	tests := []struct {
		name  string
		parse func(string) *File
	}{
		{name: "legacy", parse: func(source string) *File { return (LegacyParser{}).Parse(source) }},
		{name: "vim9", parse: func(source string) *File { return (Vim9Parser{}).Parse(source) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := "let g:text =<< END\npayload | not a command\nEND\nlet g:after = 1\n"
			if test.name == "vim9" {
				source = "vim9script\nvar text =<< END\npayload | not a command\nEND\nvar after = 1\n"
			}
			file := test.parse(source)
			commandIndex := 0
			afterIndex := 1
			if test.name == "vim9" {
				commandIndex = 1
				afterIndex = 2
			}
			if len(file.Diagnostics) != 0 || len(file.Commands) != afterIndex+1 || file.Commands[commandIndex].Heredoc == nil || file.Commands[afterIndex].Declaration == nil {
				t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
			}
			declaration := file.Commands[commandIndex].Declaration
			if test.name == "legacy" {
				if declaration != nil {
					t.Fatalf("legacy heredoc declaration = %#v", declaration)
				}
			} else if declaration == nil || file.Text(declaration.Name) != "text" || file.Text(declaration.Assignment) != "=<<" || declaration.Initializer != nil {
				t.Fatalf("Vim9 heredoc declaration = %#v", declaration)
			}
			if file.Text(file.Commands[commandIndex].Argument) == "" {
				t.Fatal("empty heredoc argument")
			}
			assertFileSpans(t, file)
		})
	}
}

func TestForIterableBoundaryPreservesPublicAST(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		parse      func(string) *File
		index      int
		binding    string
		iterable   string
		commandNum int
	}{
		{
			name:       "legacy bar and destructuring",
			source:     "for [key, value] in get(g:, 'items', []) | echo value\nendfor\n",
			parse:      func(source string) *File { return (LegacyParser{}).Parse(source) },
			index:      0,
			binding:    "value",
			iterable:   "get(g:, 'items', [])",
			commandNum: 3,
		},
		{
			name:       "vim9 comment and typed bindings",
			source:     "vim9script\nfor [key: string, value: number; rest] in items # comment | not a command\nendfor\n",
			parse:      func(source string) *File { return (Vim9Parser{}).Parse(source) },
			index:      1,
			binding:    "value",
			iterable:   "items",
			commandNum: 3,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			if len(file.Diagnostics) != 0 || len(file.Commands) != test.commandNum {
				t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
			}
			command := file.Commands[test.index]
			loop := command.For
			if loop == nil || loop.Iterable == nil || file.Text(loop.Iterable.Span) != test.iterable || file.Text(loop.IterableSpan) != test.iterable {
				t.Fatalf("for loop = %#v, iterable text = %q, span text = %q", loop, file.Text(loop.Iterable.Span), file.Text(loop.IterableSpan))
			}
			if len(loop.Bindings) < 2 || file.Text(loop.Bindings[1].Name) != test.binding {
				t.Fatalf("bindings = %#v", loop.Bindings)
			}
			if test.name == "vim9 comment and typed bindings" && (file.Text(loop.Bindings[0].Type) != "string" || file.Text(loop.Bindings[1].Type) != "number" || !loop.Bindings[2].Rest) {
				t.Fatalf("typed bindings = %#v", loop.Bindings)
			}
			if file.Commands[test.commandNum-1].Canonical != "endfor" {
				t.Fatalf("following commands/tokens = %#v, %#v", file.Commands, file.Tokens)
			}
			if test.name == "legacy bar and destructuring" {
				if countTokens(file, TokenSeparator) != 1 || countTokens(file, TokenComment) != 0 {
					t.Fatalf("legacy boundary tokens = %#v", file.Tokens)
				}
			} else if countTokens(file, TokenSeparator) != 0 || countTokens(file, TokenComment) != 1 {
				t.Fatalf("Vim9 boundary tokens = %#v", file.Tokens)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestForNestedIterableContinuationKeepsAbsoluteSpans(t *testing.T) {
	tests := []struct {
		name         string
		source       string
		parse        func(string) *File
		index        int
		kind         ExpressionKind
		continuation int
	}{
		{
			name:         "legacy",
			source:       "for item in get(g:,\n  \\ 'items', [1,\n  \\ 2])\nendfor\n",
			parse:        func(source string) *File { return (LegacyParser{}).Parse(source) },
			kind:         ExpressionCall,
			continuation: 2,
		},
		{
			name:         "vim9",
			source:       "vim9script\nfor item in [\n  1,\n  2]\nendfor\n",
			parse:        func(source string) *File { return (Vim9Parser{}).Parse(source) },
			index:        1,
			kind:         ExpressionList,
			continuation: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			if len(file.Diagnostics) != 0 || countTokens(file, TokenContinuation) != test.continuation {
				t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
			}
			command := file.Commands[test.index]
			loop := command.For
			if loop == nil || loop.Iterable == nil || loop.Iterable.Kind != test.kind || loop.Iterable.Span != loop.IterableSpan {
				t.Fatalf("for loop = %#v", loop)
			}
			if loop.IterableSpan.Start < command.Argument.Start || loop.IterableSpan.End > command.Argument.End || file.Text(loop.IterableSpan) == "" {
				t.Fatalf("iterable span = %#v, argument = %#v, text = %q", loop.IterableSpan, command.Argument, file.Text(loop.IterableSpan))
			}
			assertFileSpans(t, file)
		})
	}
}

func TestVim9ForHeaderLineBreaksKeepIterableSpans(t *testing.T) {
	source := "vim9script\nfor one in\n  [1]\nendfor\nfor two\n  in [2]\nendfor\nfor three\n  in\n  [3]\nendfor\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 7 || countTokens(file, TokenContinuation) != 4 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	for index, want := range map[int]string{1: "[1]", 3: "[2]", 5: "[3]"} {
		loop := file.Commands[index].For
		if loop == nil || file.Text(loop.In) != "in" || loop.Iterable == nil || file.Text(loop.IterableSpan) != want || file.Text(loop.Iterable.Span) != want {
			t.Fatalf("for command %d = %#v, iterable span text = %q", index, file.Commands[index], file.Text(loop.IterableSpan))
		}
	}
	assertFileSpans(t, file)
}

func TestForIterableMalformedRHSRecoversNextPhysicalLine(t *testing.T) {
	tests := []struct {
		name   string
		source string
		parse  func(string) *File
		index  int
		follow int
	}{
		{
			name:   "legacy",
			source: "for item in (items | echo 'same-line'\nlet g:after = 1\n",
			parse:  func(source string) *File { return (LegacyParser{}).Parse(source) },
			index:  0,
			follow: 1,
		},
		{
			name:   "vim9",
			source: "vim9script\nfor item in (items | echo 'same-line'\nvar after = 1\n",
			parse:  func(source string) *File { return (Vim9Parser{}).Parse(source) },
			index:  1,
			follow: 2,
		},
		{
			name:   "legacy missing iterable",
			source: "for item in\nlet g:after = 1\n",
			parse:  func(source string) *File { return (LegacyParser{}).Parse(source) },
			index:  0,
			follow: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			if len(file.Commands) != test.follow+1 || countTokens(file, TokenSeparator) != 0 || len(file.Diagnostics) == 0 {
				t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
			}
			broken := file.Commands[test.index]
			if broken.For == nil || broken.For.Iterable == nil || file.Text(broken.Argument) == "" {
				t.Fatalf("broken for = %#v", broken)
			}
			if file.Commands[test.follow].Declaration == nil {
				t.Fatalf("following declaration = %#v", file.Commands[test.follow])
			}
			assertFileSpans(t, file)
		})
	}
}

func TestVim9CommandStartCallBoundaryPreservesPublicAST(t *testing.T) {
	file := Parse("vim9script\nResult(1) | echo 2\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || countTokens(file, TokenSeparator) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	call := file.Commands[1]
	if call.Kind != CommandExpression || len(call.Expressions) != 1 || call.Expressions[0].Kind != ExpressionCall || file.Text(call.Expressions[0].Span) != "Result(1)" {
		t.Fatalf("command-start call = %#v", call)
	}
	if file.Commands[2].Canonical != "echo" || file.Text(file.Commands[2].Argument) != "2" {
		t.Fatalf("following command = %#v", file.Commands[2])
	}
	assertFileSpans(t, file)
}

func TestVim9CommandStartAutoloadCallIsExpression(t *testing.T) {
	source := `vim9script
def Set_lines(bufnr: number, change_list: list<any>, maxEditCount: number,
    start_row: number, end_row: number, replace: list<string>)
  if !empty(change_list) && len(change_list) <= maxEditCount
    Apply_changes(bufnr, change_list)
  else
    coc#api#SetBufferLines(bufnr, start_row + 1, end_row, replace)
  endif
enddef
`
	file := Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	callStart := strings.Index(source, "coc#api#SetBufferLines")
	var call *Command
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Argument.Start == callStart {
			call = command
			break
		}
	}
	if call == nil {
		t.Fatalf("autoload call command not found: %#v", file.Commands)
	}
	if call.Kind != CommandExpression || call.Name != (Span{}) || call.TypedName != "" || call.Canonical != "" ||
		len(call.Expressions) != 1 || call.Expressions[0].Kind != ExpressionCall || len(call.Expressions[0].Children) == 0 ||
		call.Expressions[0].Children[0].Value != "coc#api#SetBufferLines" {
		t.Fatalf("autoload call = %#v", call)
	}
	for _, token := range file.Tokens {
		if token.Kind == TokenCommand && token.Span.Start == callStart {
			t.Fatalf("autoload call starts with command token: %#v", token)
		}
	}
	assertFileSpans(t, file)
}

func TestVim9AutoloadCallWithDigitsAndUnderscores(t *testing.T) {
	for _, name := range []string{"modula2#SetDialect", "foo_bar2#nested3#Run", "win2#Run"} {
		file := Parse("vim9script\ndef Test()\n  " + name + "(1) | echo 2\nenddef\n")
		if len(file.Diagnostics) != 0 || len(file.Commands) != 5 {
			t.Fatalf("%s: %#v", name, file.Diagnostics)
		}
		call := file.Commands[2]
		if call.Kind != CommandExpression || len(call.Expressions) != 1 || call.Expressions[0].Kind != ExpressionCall || call.Expressions[0].Children[0].Value != name {
			t.Fatalf("%s: call lost: %#v", name, call)
		}
		if file.Commands[3].Canonical != "echo" {
			t.Fatal("following command lost")
		}
		assertFileSpans(t, file)
		legacy := Parse(name + "(1)\n")
		if legacy.Commands[0].Kind == CommandExpression {
			t.Fatal("legacy implicit call incorrectly enabled")
		}
	}
}

func TestVim9BuiltinFunctionCallIsNotCommand(t *testing.T) {
	source := `vim9script
def AddProp(lnum: number, colStart: number, bufnr: number, type: string, propId: number, colEnd: number)
  try
    prop_add(lnum + 1, colStart + 1, {'bufnr': bufnr, 'type': type, 'id': propId, 'end_col': colEnd + 1})
  catch /^Vim\%((\a\+)\)\=:\(E967\|E964\)/
    # ignore 967
  endtry
enddef
`
	file := Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	callStart := strings.Index(source, "prop_add")
	var call *Command
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Argument.Start == callStart {
			call = command
			break
		}
	}
	if call == nil {
		t.Fatalf("prop_add call command not found: %#v", file.Commands)
	}
	if call.Kind != CommandExpression || call.Name != (Span{}) || call.TypedName != "" || call.Canonical != "" ||
		len(call.Expressions) != 1 || call.Expressions[0].Kind != ExpressionCall || len(call.Expressions[0].Children) == 0 ||
		call.Expressions[0].Children[0].Value != "prop_add" {
		t.Fatalf("prop_add call = %#v", call)
	}
	for _, token := range file.Tokens {
		if token.Kind == TokenCommand && token.Span.Start == callStart {
			t.Fatalf("prop_add call starts with command token: %#v", token)
		}
	}
	assertFileSpans(t, file)
}

func TestVim9CommandStartCallTrailingCommentIsOpaque(t *testing.T) {
	file := Parse("vim9script\nResult(1) # comment | not a command\necho 2\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || countTokens(file, TokenComment) != 1 || countTokens(file, TokenSeparator) != 0 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	call := file.Commands[1]
	if call.Kind != CommandExpression || len(call.Expressions) != 1 || file.Text(call.Argument) != "Result(1)" || file.Text(call.Expressions[0].Span) != "Result(1)" {
		t.Fatalf("command-start call = %#v", call)
	}
	if file.Commands[2].Canonical != "echo" {
		t.Fatalf("following command = %#v", file.Commands[2])
	}
	assertFileSpans(t, file)
}

func TestVim9CommandStartMethodChainContinuationKeepsAbsoluteSpans(t *testing.T) {
	source := "vim9script\nresult\n  ->map((_, value) => value + 1)\n  ->filter((_, value) => value > 1)\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || countTokens(file, TokenContinuation) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	command := file.Commands[1]
	if command.Kind != CommandExpression || len(command.Expressions) != 1 || command.Expressions[0].Kind != ExpressionCall {
		t.Fatalf("method-chain command = %#v", command)
	}
	expression := command.Expressions[0]
	if expression.Span.Start < command.Argument.Start || expression.Span.End > command.Argument.End || file.Text(expression.Span) != file.Text(command.Argument) {
		t.Fatalf("expression = %#v, argument = %#v, expression text = %q, argument text = %q", expression, command.Argument, file.Text(expression.Span), file.Text(command.Argument))
	}
	if expression.Span.Start != strings.Index(source, "result") || expression.Span.End != len(source)-1 {
		t.Fatalf("absolute expression span = %#v, source length = %d", expression.Span, len(source))
	}
	assertFileSpans(t, file)
}

func TestVim9MalformedCommandStartCallRecoversNextPhysicalLine(t *testing.T) {
	file := Parse("vim9script\nResult(1 | echo 2\nvar after = 3\n")
	if len(file.Commands) != 3 || countTokens(file, TokenSeparator) != 0 || len(file.Diagnostics) == 0 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	broken := file.Commands[1]
	if broken.Kind != CommandExpression || len(broken.Expressions) != 1 || file.Text(broken.Argument) != "Result(1 | echo 2" {
		t.Fatalf("broken call = %#v", broken)
	}
	if file.Commands[2].Canonical != "var" || file.Commands[2].Declaration == nil {
		t.Fatalf("following declaration = %#v", file.Commands[2])
	}
	assertFileSpans(t, file)
}

func TestVim9CommandStartSpacedCallRemainsExCommand(t *testing.T) {
	file := Parse("vim9script\nFunc ()\nvar after = 1\n")
	if len(file.Commands) != 3 || file.Commands[1].Kind != CommandUser || file.Commands[1].Canonical != "Func" || len(file.Diagnostics) != 0 || file.Commands[2].Declaration == nil {
		t.Fatalf("spaced call = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	assertFileSpans(t, file)
}

func TestVim9CommandStartAssignmentKeepsExistingAST(t *testing.T) {
	file := Parse("vim9script\nvalue = 1 | echo 2\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || countTokens(file, TokenSeparator) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	assignment := file.Commands[1]
	if assignment.Kind != CommandExpression || len(assignment.Expressions) != 1 || assignment.Expressions[0].Kind != ExpressionAssignment || len(assignment.Expressions[0].Children) != 2 {
		t.Fatalf("assignment = %#v", assignment)
	}
	if file.Text(assignment.Expressions[0].Children[0].Span) != "value" || file.Text(assignment.Expressions[0].Children[1].Span) != "1" {
		t.Fatalf("assignment children = %#v", assignment.Expressions[0].Children)
	}
	assertFileSpans(t, file)
}

func TestVim9CommandStartAssignmentBoundaryPreservesTargetsAndOperators(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		operator string
		left     string
		right    string
	}{
		{
			name:     "member and index",
			source:   "vim9script\nobj.field = get(g:, 'value', 0) | items[0] += 1\n",
			operator: "=",
			left:     "obj.field",
			right:    "get(g:, 'value', 0)",
		},
		{
			name:     "concatenate assignment",
			source:   "vim9script\nvalue ..= get(g:, 'value', 0) # comment\n",
			operator: "..=",
			left:     "value",
			right:    "get(g:, 'value', 0)",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if len(file.Diagnostics) != 0 || len(file.Commands) < 2 {
				t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
			}
			command := file.Commands[1]
			if command.Kind != CommandExpression || len(command.Expressions) != 1 || command.Expressions[0].Kind != ExpressionAssignment {
				t.Fatalf("assignment command = %#v", command)
			}
			assignment := command.Expressions[0]
			if file.Text(assignment.Operator) != test.operator || len(assignment.Children) != 2 || file.Text(assignment.Children[0].Span) != test.left || file.Text(assignment.Children[1].Span) != test.right {
				t.Fatalf("assignment = %#v, operator = %q, children = %q, %q", assignment, file.Text(assignment.Operator), file.Text(assignment.Children[0].Span), file.Text(assignment.Children[1].Span))
			}
			if command.Expressions[0].Span.Start < command.Argument.Start || command.Expressions[0].Span.End > command.Argument.End {
				t.Fatalf("assignment span = %#v, argument = %#v", assignment.Span, command.Argument)
			}
			if test.name == "member and index" && (len(file.Commands) != 3 || file.Commands[2].Kind != CommandExpression) {
				t.Fatalf("member/index commands = %#v", file.Commands)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestVim9CommandStartAssignmentLogicalContinuationKeepsAbsoluteSpans(t *testing.T) {
	source := "vim9script\nvalue ..= get(g:, 'value', [])\n  ->copy()\nvar after = 1\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || countTokens(file, TokenContinuation) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	command := file.Commands[1]
	if command.Kind != CommandExpression || len(command.Expressions) != 1 || command.Expressions[0].Kind != ExpressionAssignment {
		t.Fatalf("assignment command = %#v", command)
	}
	right := command.Expressions[0].Children[1]
	if right.Kind != ExpressionCall || file.Text(right.Span) != "get(g:, 'value', [])\n  ->copy()" || right.Span.Start != strings.Index(source, "get(") || right.Span.End != strings.Index(source, "\nvar after") {
		t.Fatalf("right = %#v, text = %q", right, file.Text(right.Span))
	}
	assertFileSpans(t, file)
}

func TestVim9CommandStartAssignmentMalformedRHSRecoversNextLine(t *testing.T) {
	file := Parse("vim9script\nvalue = (1 | echo 'same-line'\nvar after = 3\n")
	if len(file.Commands) != 3 || countTokens(file, TokenSeparator) != 0 || len(file.Diagnostics) == 0 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	broken := file.Commands[1]
	if broken.Kind != CommandExpression || len(broken.Expressions) != 1 || broken.Expressions[0].Kind != ExpressionAssignment || file.Text(broken.Argument) != "value = (1 | echo 'same-line'" {
		t.Fatalf("broken assignment = %#v", broken)
	}
	if file.Commands[2].Canonical != "var" || file.Commands[2].Declaration == nil {
		t.Fatalf("following declaration = %#v", file.Commands[2])
	}
	assertFileSpans(t, file)
}

func TestVim9PunctuationAssignmentsKeepFallbackAST(t *testing.T) {
	file := Parse("vim9script\n[a, b] = values\n&l:tabstop = 4\n$VIMLS_VALUE = 'ok'\n@r = value\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	for index, want := range []struct {
		left  string
		right string
	}{
		{left: "[a, b]", right: "values"},
		{left: "&l:tabstop", right: "4"},
		{left: "$VIMLS_VALUE", right: "'ok'"},
		{left: "@r", right: "value"},
	} {
		command := file.Commands[index+1]
		if command.Kind != CommandExpression || len(command.Expressions) != 1 || command.Expressions[0].Kind != ExpressionAssignment || len(command.Expressions[0].Children) != 2 {
			t.Fatalf("fallback assignment %d = %#v", index, command)
		}
		assignment := command.Expressions[0]
		if file.Text(assignment.Children[0].Span) != want.left || file.Text(assignment.Children[1].Span) != want.right {
			t.Fatalf("fallback assignment %d = %#v", index, assignment)
		}
	}
	assertFileSpans(t, file)
}

func TestVim9TypedDeclarationBoundaryProbeKeepsSingleLineDeclaration(t *testing.T) {
	file := Parse("vim9script\nvar value: number = 1 # comment\nvar after = value\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	declaration := file.Commands[1].Declaration
	if declaration == nil || declaration.Initializer == nil || file.Text(declaration.Initializer.Span) != "1" || file.Text(declaration.Type) != "number" {
		t.Fatalf("declaration = %#v", declaration)
	}
	if file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Initializer.Span) != "value" {
		t.Fatalf("following declaration = %#v", file.Commands[2])
	}
	assertFileSpans(t, file)
}

func TestVim9TypedDeclarationBoundaryProbeKeepsContinuations(t *testing.T) {
	source := "vim9script\nvar values: list<number> = [\n  1,\n  2,\n]\nvar text: string = 'one'\n  .. 'two'\nvar after = 3\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 || countTokens(file, TokenContinuation) != 4 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	values := file.Commands[1].Declaration
	text := file.Commands[2].Declaration
	if values == nil || values.Initializer == nil || values.Initializer.Kind != ExpressionList || text == nil || text.Initializer == nil || text.Initializer.Kind != ExpressionBinary {
		t.Fatalf("declarations = %#v, %#v", values, text)
	}
	if file.Text(values.Initializer.Span) != "[\n  1,\n  2,\n]" || file.Text(text.Initializer.Span) != "'one'\n  .. 'two'" {
		t.Fatalf("initializer texts = %q, %q", file.Text(values.Initializer.Span), file.Text(text.Initializer.Span))
	}
	assertFileSpans(t, file)
}

func TestVim9TypedDeclarationBoundaryProbeFallsBackOnMalformedRHS(t *testing.T) {
	file := Parse("vim9script\nvar value = (1\nvar after = 2\n")
	if len(file.Commands) != 3 || len(file.Diagnostics) == 0 || file.Commands[2].Declaration == nil {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[1].Declaration == nil || file.Commands[1].Declaration.Initializer == nil || file.Text(file.Commands[1].Argument) != "value = (1" {
		t.Fatalf("malformed declaration = %#v", file.Commands[1])
	}
	assertFileSpans(t, file)
}

func TestVim9TypedDeclarationBoundaryProbeMapsMalformedContinuation(t *testing.T) {
	source := "vim9script\nvar values: list<number> = [\n  1,\n  2,\nvar after = 3\n"
	file := Parse(source)
	if len(file.Commands) != 3 || len(file.Diagnostics) != 1 || countTokens(file, TokenContinuation) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	broken := file.Commands[1].Declaration
	if broken == nil || broken.Initializer == nil || broken.Initializer.Kind != ExpressionList || len(broken.Initializer.Children) != 2 {
		t.Fatalf("malformed declaration = %#v", file.Commands[1])
	}
	if file.Text(broken.Initializer.Span) != "[" || file.Text(broken.Initializer.Children[0].Span) != "1" || file.Text(broken.Initializer.Children[1].Span) != "2" {
		t.Fatalf("initializer = %#v", broken.Initializer)
	}
	if diagnostic := file.Diagnostics[0]; diagnostic.Code != "vimls/missing-delimiter" || diagnostic.Span != (Span{Start: 49, End: 49}) {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
	if after := file.Commands[2].Declaration; after == nil || file.Text(after.Name) != "after" || file.Text(after.Initializer.Span) != "3" {
		t.Fatalf("following declaration = %#v", file.Commands[2])
	}
	assertFileSpans(t, file)
}

func TestVim9TypedDeclarationBoundaryProbeOwnsMalformedLine(t *testing.T) {
	file := Parse("vim9script\nvar broken = [1, 2} | echo hidden\nvar after = 3\n")
	if len(file.Commands) != 3 || len(file.Diagnostics) != 2 || countTokens(file, TokenSeparator) != 0 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	if broken := file.Commands[1]; broken.Declaration == nil || broken.Declaration.Initializer == nil || file.Text(broken.Argument) != "broken = [1, 2} | echo hidden" {
		t.Fatalf("malformed declaration = %#v", broken)
	}
	if after := file.Commands[2].Declaration; after == nil || file.Text(after.Name) != "after" {
		t.Fatalf("following declaration = %#v", file.Commands[2])
	}
	assertFileSpans(t, file)
}

func TestVim9TypedDeclarationBoundaryProbeKeepsHeredocInitializerOpaque(t *testing.T) {
	source := "vim9script\nvar text =<< END\npayload | not a command\nEND\nvar after = 1\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || file.Commands[1].Heredoc == nil || file.Commands[2].Declaration == nil {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[1].Declaration == nil || file.Text(file.Commands[1].Declaration.Name) != "text" ||
		file.Commands[1].Declaration.Initializer != nil || file.Commands[1].Expressions != nil {
		t.Fatalf("heredoc declaration = %#v", file.Commands[1])
	}
	assertFileSpans(t, file)
}

func TestVim9PutExpressionBoundaryPreservesBarsCommentsAndRegisters(t *testing.T) {
	source := "vim9script\nput =1 | echo 2\niput =\"a|b\"\nput =1 # comment | not a command\nput 1\nvar after = 3\n"
	file := Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 7 || countTokens(file, TokenSeparator) != 1 || countTokens(file, TokenComment) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	if file.Commands[1].Canonical != "put" || len(file.Commands[1].Expressions) != 1 || file.Text(file.Commands[1].Expressions[0].Span) != "1" {
		t.Fatalf("put expression = %#v", file.Commands[1])
	}
	if file.Commands[2].Canonical != "echo" || file.Text(file.Commands[2].Argument) != "2" {
		t.Fatalf("following echo = %#v", file.Commands[2])
	}
	if file.Commands[3].Canonical != "iput" || len(file.Commands[3].Expressions) != 1 || file.Text(file.Commands[3].Expressions[0].Span) != "\"a|b\"" {
		t.Fatalf("iput expression = %#v", file.Commands[3])
	}
	if file.Commands[4].Canonical != "put" || len(file.Commands[4].Expressions) != 1 || file.Text(file.Commands[4].Argument) != "=1" || file.Text(file.Commands[4].Expressions[0].Span) != "1" {
		t.Fatalf("commented put = %#v", file.Commands[4])
	}
	if file.Commands[5].Canonical != "put" || len(file.Commands[5].Expressions) != 0 || file.Text(file.Commands[5].Argument) != "1" {
		t.Fatalf("register put = %#v", file.Commands[5])
	}
	if file.Commands[6].Canonical != "var" || file.Commands[6].Declaration == nil {
		t.Fatalf("following declaration = %#v", file.Commands[6])
	}
	assertFileSpans(t, file)
}

func TestVim9PutExpressionBoundaryMalformedRHSRecoversNextLine(t *testing.T) {
	tests := []string{
		"vim9script\nput = | echo 2\nvar after = 3\n",
		"vim9script\nput =1#comment | echo 2\nvar after = 3\n",
		"vim9script\nput =<< END\nvar after = 3\n",
	}
	for _, source := range tests {
		t.Run(source, func(t *testing.T) {
			file := Parse(source)
			if len(file.Commands) != 3 || len(file.Diagnostics) == 0 || countTokens(file, TokenSeparator) != 0 {
				t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
			}
			put := file.Commands[1]
			if put.Canonical != "put" || put.Heredoc != nil || file.Commands[2].Canonical != "var" || file.Commands[2].Declaration == nil {
				t.Fatalf("put/recovery commands = %#v", file.Commands)
			}
			if source == tests[0] && file.Text(put.Argument) != "= | echo 2" || source == tests[1] && file.Text(put.Argument) != "=1#comment | echo 2" || source == tests[2] && file.Text(put.Argument) != "=<< END" {
				t.Fatalf("put argument = %q", file.Text(put.Argument))
			}
			assertFileSpans(t, file)
		})
	}
}

func TestLegacyPutExpressionBoundaryNormalizesExEscapes(t *testing.T) {
	source := "put ='path' .. \\\",/test\\\"\n" +
		"iput ='a\\|b' | echo 'after'\n" +
		"put ='a|b' | echo 'not a command'\n" +
		"let g:after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) == 0 || len(file.Commands) != 5 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	first := file.Commands[0]
	if len(first.Expressions) != 1 || first.Expressions[0].Kind != ExpressionBinary || file.Text(first.Expressions[0].Span) != "'path' .. \\\",/test\\\"" {
		t.Fatalf("put expression = %#v, text = %q", first.Expressions, file.Text(first.Expressions[0].Span))
	}
	if file.Text(first.Expressions[0].Children[1].Span) != "\\\",/test\\\"" {
		t.Fatalf("normalized string span = %#v, text = %q", first.Expressions[0].Children[1], file.Text(first.Expressions[0].Children[1].Span))
	}
	stringStart := strings.Index(source, `\",/test\"`)
	if first.Expressions[0].Children[1].Span != (Span{Start: stringStart, End: stringStart + len(`\",/test\"`)}) {
		t.Fatalf("string span = %#v, want raw escaped span", first.Expressions[0].Children[1].Span)
	}
	if file.Commands[1].Canonical != "iput" || len(file.Commands[1].Expressions) != 1 || file.Text(file.Commands[1].Expressions[0].Span) != "'a\\|b'" {
		t.Fatalf("escaped bar expression = %#v", file.Commands[1])
	}
	if file.Commands[3].Canonical != "put" || file.Commands[4].Canonical != "let" || file.Commands[4].Declaration == nil {
		t.Fatalf("malformed put recovery = %#v", file.Commands)
	}
	if countTokens(file, TokenSeparator) != 1 || !hasDiagnostic(file, "vimls/missing-delimiter") {
		t.Fatalf("tokens/diagnostics = %#v / %#v", file.Tokens, file.Diagnostics)
	}
	brokenBar := strings.Index(source, "'a|b'") + len("'a")
	foundBroken := false
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code == "vimls/missing-delimiter" {
			foundBroken = diagnostic.Span == (Span{Start: brokenBar, End: brokenBar})
		}
	}
	if !foundBroken {
		t.Fatalf("malformed put diagnostic span = %#v", file.Diagnostics)
	}
	assertFileSpans(t, file)
}

func TestLegacyPutExpressionBoundaryMapsLeadingEscape(t *testing.T) {
	source := `put =\"head\"` + "\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || len(file.Commands[0].Expressions) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	expression := file.Commands[0].Expressions[0]
	want := Span{Start: strings.Index(source, `\"`), End: len(source) - 1}
	if expression.Kind != ExpressionString || expression.Span != want || file.Text(expression.Span) != `\"head\"` {
		t.Fatalf("expression = %#v, text = %q, want span = %#v", expression, file.Text(expression.Span), want)
	}
	assertFileSpans(t, file)
}

func TestLegacyPutExpressionBoundaryMapsDiagnosticAfterEscape(t *testing.T) {
	source := `put =\"ok\" + ) | echo 'same-line'` + "\nlet g:after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Commands) != 2 || countTokens(file, TokenSeparator) != 0 || len(file.Diagnostics) == 0 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	want := strings.Index(source, ")")
	found := false
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Span.Start == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("diagnostics = %#v, want one at raw offset %d", file.Diagnostics, want)
	}
	if file.Commands[1].Declaration == nil || file.Text(file.Commands[1].Declaration.Name) != "g:after" {
		t.Fatalf("next-line recovery = %#v", file.Commands[1])
	}
	assertFileSpans(t, file)
}

func TestLegacyPutExpressionBoundaryEmptyRHSAndComments(t *testing.T) {
	empty := (LegacyParser{}).Parse("put = | echo 'after'\niput =\n")
	if len(empty.Diagnostics) != 0 || len(empty.Commands) != 3 || len(empty.Commands[0].Expressions) != 0 || len(empty.Commands[2].Expressions) != 0 {
		t.Fatalf("empty RHS = %#v, diagnostics = %#v", empty.Commands, empty.Diagnostics)
	}
	assertFileSpans(t, empty)

	source := "put = | echo 'after'\n" +
		"iput =\n" +
		"put =1 \" comment | not a command\n" +
		"put =1 # comment | not a command\n" +
		"let g:after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Commands) != 6 || len(file.Diagnostics) == 0 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[0].Canonical != "put" || len(file.Commands[0].Expressions) != 0 || file.Text(file.Commands[0].Argument) != "=" {
		t.Fatalf("empty put = %#v", file.Commands[0])
	}
	if file.Commands[2].Canonical != "iput" || len(file.Commands[2].Expressions) != 0 || file.Text(file.Commands[2].Argument) != "=" {
		t.Fatalf("empty iput = %#v", file.Commands[2])
	}
	if file.Commands[3].Canonical != "put" || len(file.Commands[3].Expressions) != 1 || file.Text(file.Commands[3].Argument) != "=1" {
		t.Fatalf("legacy comment = %#v", file.Commands[3])
	}
	if file.Commands[4].Canonical != "put" || file.Text(file.Commands[4].Argument) != "=1 # comment | not a command" {
		t.Fatalf("hash trailing put = %#v", file.Commands[4])
	}
	if file.Commands[5].Canonical != "let" || file.Commands[5].Declaration == nil || countTokens(file, TokenSeparator) != 1 {
		t.Fatalf("recovery = %#v, tokens = %#v", file.Commands, file.Tokens)
	}
	hash := strings.Index(source, "# comment")
	foundTrailing := false
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code == "vimls/trailing-expression" && diagnostic.Span == (Span{Start: hash, End: hash + 1}) {
			foundTrailing = true
		}
	}
	if !foundTrailing {
		t.Fatalf("hash diagnostic span = %#v", file.Diagnostics)
	}
	assertFileSpans(t, file)
}

func TestLegacyPutExpressionBoundaryContinuationSpans(t *testing.T) {
	source := "put = [1,\n  \\ 2] | echo 'after'\nlet g:after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || countTokens(file, TokenSeparator) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	put := file.Commands[0]
	if len(put.Expressions) != 1 || put.Expressions[0].Kind != ExpressionList || file.Text(put.Expressions[0].Span) != "[1,\n  \\ 2]" {
		t.Fatalf("continuation expression = %#v, text = %q", put.Expressions, file.Text(put.Expressions[0].Span))
	}
	if file.Commands[1].Canonical != "echo" || file.Commands[2].Declaration == nil {
		t.Fatalf("following commands = %#v", file.Commands)
	}
	assertFileSpans(t, file)
}

func TestLegacySyntaxErrorStopsCurrentLineAndRecoversNextLine(t *testing.T) {
	file := (LegacyParser{}).Parse("if (left | echo 'same-line' | endif\nlet g:after = 1\n")
	if len(file.Commands) != 2 || countTokens(file, TokenSeparator) != 0 || !hasDiagnostic(file, "vimls/missing-delimiter") {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	if got := file.Text(file.Commands[0].Argument); got != "(left | echo 'same-line' | endif" {
		t.Fatalf("argument = %q", got)
	}
	if file.Commands[1].Canonical != "let" || file.Commands[1].Declaration == nil || file.Text(file.Commands[1].Declaration.Name) != "g:after" {
		t.Fatalf("next line = %#v", file.Commands[1])
	}
	counts := make(map[string]int)
	for _, diagnostic := range file.Diagnostics {
		counts[diagnostic.Code]++
	}
	for _, code := range []string{"vim/E171", "vimls/missing-delimiter", "vimls/trailing-expression"} {
		if counts[code] != 1 {
			t.Fatalf("diagnostic %q count = %d, diagnostics = %#v", code, counts[code], file.Diagnostics)
		}
	}
}

func TestVim9SyntaxErrorStopsCurrentLineAndRecoversNextLine(t *testing.T) {
	file := (Vim9Parser{}).Parse("vim9script\necho ) | echo 'same-line'\nvar after = 1\n")
	if len(file.Commands) != 3 || countTokens(file, TokenSeparator) != 0 || len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vimls/unexpected-token" {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	if file.Commands[1].Canonical != "echo" || file.Text(file.Commands[1].Argument) != ") | echo 'same-line'" {
		t.Fatalf("broken line = %#v, argument = %q", file.Commands[1], file.Text(file.Commands[1].Argument))
	}
	if file.Commands[2].Declaration == nil || file.Text(file.Commands[2].Declaration.Name) != "after" {
		t.Fatalf("next line = %#v", file.Commands[2])
	}
}

func TestVim9CommandDiagnosticMakesSameLineTailOpaque(t *testing.T) {
	file := (Vim9Parser{}).Parse("vim9script\nenu E | var same_line = 1\nendenum\nvar after = 2\n")
	if len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E1065" || len(file.Commands) != 4 || countTokens(file, TokenSeparator) != 0 || countTokens(file, TokenOpaque) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	if file.Commands[1].Canonical != "enum" || file.Commands[2].Canonical != "endenum" || file.Commands[3].Declaration == nil || file.Text(file.Commands[3].Declaration.Name) != "after" {
		t.Fatalf("recovered commands = %#v", file.Commands)
	}
}

func TestLegacyCommandBoundarySeparatorAfterContinuation(t *testing.T) {
	source := "if left\n" +
		"  \\ && right | echo 'done'\n" +
		"endif\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || countTokens(file, TokenContinuation) != 1 || countTokens(file, TokenSeparator) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	if file.Commands[0].Canonical != "if" || file.Commands[1].Canonical != "echo" || file.Commands[2].Canonical != "endif" {
		t.Fatalf("commands = %#v", file.Commands)
	}
	if got := file.Text(file.Commands[0].Argument); got != "left\n  \\ && right" {
		t.Fatalf("continued if argument = %q", got)
	}
	expression := file.Commands[0].Expressions
	if len(expression) != 1 || expression[0].Kind != ExpressionBinary || expression[0].Value != "&&" || len(expression[0].Children) != 2 || expression[0].Children[0].Value != "left" || expression[0].Children[1].Value != "right" {
		t.Fatalf("continued if expression = %#v", expression)
	}
	wantStart := strings.Index(source, "left")
	wantEnd := strings.Index(source, "right") + len("right")
	if expression[0].Span != (Span{Start: wantStart, End: wantEnd}) || file.Text(expression[0].Span) != "left\n  \\ && right" {
		t.Fatalf("continued if expression span = %#v, text = %q", expression[0].Span, file.Text(expression[0].Span))
	}
	assertFileSpans(t, file)
	for index := 1; index < len(file.Tokens); index++ {
		if file.Tokens[index].Span.Start < file.Tokens[index-1].Span.Start {
			t.Fatalf("unordered tokens = %#v", file.Tokens)
		}
	}
}

func TestVim9CommandBoundaryHashComment(t *testing.T) {
	file := (Vim9Parser{}).Parse("if true # comment | not a command\n  echo 'ok'\nendif\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || countTokens(file, TokenComment) != 1 || countTokens(file, TokenSeparator) != 0 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	if file.Commands[0].Canonical != "if" || file.Commands[1].Canonical != "echo" || file.Commands[2].Canonical != "endif" {
		t.Fatalf("commands = %#v", file.Commands)
	}
	if got := file.Text(file.Commands[0].Argument); got != "true" {
		t.Fatalf("if argument = %q", got)
	}
}

func TestVim9OpaqueHashCommentBoundaries(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		commands    int
		separators  int
		comments    int
		argument    string
		first, last string
	}{
		{
			name:       "adjacent hash",
			source:     "colorscheme foo#bar | colorscheme after\n",
			commands:   2,
			separators: 1,
			argument:   "foo#bar",
			first:      "colorscheme",
			last:       "colorscheme",
		},
		{
			name:       "escaped hash",
			source:     "colorscheme foo\\#bar | colorscheme after\n",
			commands:   2,
			separators: 1,
			argument:   "foo\\#bar",
			first:      "colorscheme",
			last:       "colorscheme",
		},
		{
			name:     "whitespace hash comment",
			source:   "colorscheme foo #bar | same-line\ncolorscheme after\n",
			commands: 2,
			comments: 1,
			argument: "foo",
			first:    "colorscheme",
			last:     "colorscheme",
		},
		{
			name:       "ctrl-v protects hash",
			source:     "colorscheme foo\x16#bar | colorscheme after\n",
			commands:   2,
			separators: 1,
			argument:   "foo\x16#bar",
			first:      "colorscheme",
			last:       "colorscheme",
		},
		{
			name:     "ctrl-v leaves raw whitespace before hash",
			source:   "colorscheme foo\x16 #bar | same-line\ncolorscheme after\n",
			commands: 2,
			comments: 1,
			argument: "foo\x16",
			first:    "colorscheme",
			last:     "colorscheme",
		},
		{
			name:       "hash dictionary opener",
			source:     "colorscheme #{key} | colorscheme after\n",
			commands:   2,
			separators: 1,
			argument:   "#{key}",
			first:      "colorscheme",
			last:       "colorscheme",
		},
		{
			name:     "hash fold comment",
			source:   "colorscheme #{{ fold | same-line\ncolorscheme after\n",
			commands: 2,
			comments: 1,
			argument: "",
			first:    "colorscheme",
			last:     "colorscheme",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := (Vim9Parser{}).Parse(test.source)
			if len(file.Diagnostics) != 0 || len(file.Commands) != test.commands || countTokens(file, TokenSeparator) != test.separators || countTokens(file, TokenComment) != test.comments {
				t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
			}
			commentToken, separatorToken := false, false
			for _, token := range file.Tokens {
				switch token.Kind {
				case TokenComment:
					commentToken = true
					if text := file.Text(token.Span); len(text) == 0 || text[0] != '#' {
						t.Fatalf("comment token = %#v, text = %q", token, text)
					}
				case TokenSeparator:
					separatorToken = true
					if file.Text(token.Span) != "|" {
						t.Fatalf("separator token = %#v, text = %q", token, file.Text(token.Span))
					}
				}
			}
			if commentToken != (test.comments > 0) || separatorToken != (test.separators > 0) {
				t.Fatalf("comment token = %v, separator token = %v", commentToken, separatorToken)
			}
			if file.Commands[0].Canonical != test.first || file.Commands[len(file.Commands)-1].Canonical != test.last || file.Text(file.Commands[0].Argument) != test.argument {
				t.Fatalf("first command = %#v, last command = %#v, argument = %q", file.Commands[0], file.Commands[len(file.Commands)-1], file.Text(file.Commands[0].Argument))
			}
			assertFileSpans(t, file)
		})
	}
}

func TestVim9OpaqueCommentInContinuedContainers(t *testing.T) {
	source := "var values = [\n" +
		"  1, # comment } | echo hidden\n" +
		"  2]\n" +
		"var mapping = {\n" +
		"  'one': 1, # comment } | echo hidden\n" +
		"  'two': 2}\n" +
		"var after = 3\n"
	file := (Vim9Parser{}).Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, commands = %#v", file.Diagnostics, file.Commands)
	}
	if len(file.Commands) != 3 || countTokens(file, TokenSeparator) != 0 || countTokens(file, TokenComment) != 2 {
		t.Fatalf("commands = %#v, tokens = %#v", file.Commands, file.Tokens)
	}
	if file.Commands[0].Declaration == nil || file.Commands[1].Declaration == nil || file.Commands[2].Declaration == nil {
		t.Fatalf("declarations = %#v", file.Commands)
	}
	if file.Text(file.Commands[0].Declaration.Name) != "values" || file.Text(file.Commands[1].Declaration.Name) != "mapping" || file.Text(file.Commands[2].Declaration.Name) != "after" {
		t.Fatalf("commands = %#v", file.Commands)
	}
	if len(file.Commands[0].Expressions) != 1 || len(file.Commands[1].Expressions) != 1 {
		t.Fatalf("expressions = %#v", file.Commands)
	}
	if file.Commands[0].Expressions[0].Kind != ExpressionAssignment || file.Commands[0].Expressions[0].Children[1].Kind != ExpressionList || len(file.Commands[0].Expressions[0].Children[1].Children) != 2 {
		t.Fatalf("list expression = %#v", file.Commands[0].Expressions)
	}
	if file.Commands[1].Expressions[0].Kind != ExpressionAssignment || file.Commands[1].Expressions[0].Children[1].Kind != ExpressionDictionary || len(file.Commands[1].Expressions[0].Children[1].Children) != 4 {
		t.Fatalf("dict expression = %#v", file.Commands[1].Expressions)
	}
	assertFileSpans(t, file)
}

func TestVim9CommandBoundaryLogicalOrIsNotSeparator(t *testing.T) {
	file := (Vim9Parser{}).Parse("if left || right | echo 'done' | endif\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || countTokens(file, TokenSeparator) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	if file.Commands[0].Canonical != "if" || file.Commands[1].Canonical != "echo" || file.Commands[2].Canonical != "endif" {
		t.Fatalf("commands = %#v", file.Commands)
	}
	if got := file.Text(file.Commands[0].Argument); got != "left || right" {
		t.Fatalf("if argument = %q", got)
	}
	expression := file.Commands[0].Expressions
	if len(expression) != 1 || expression[0].Kind != ExpressionBinary || expression[0].Value != "||" || len(expression[0].Children) != 2 || expression[0].Children[0].Value != "left" || expression[0].Children[1].Value != "right" {
		t.Fatalf("if expression = %#v", expression)
	}
	wantStart := strings.Index(file.Source, "left")
	wantEnd := strings.Index(file.Source, "right") + len("right")
	if expression[0].Span != (Span{Start: wantStart, End: wantEnd}) || file.Text(expression[0].Span) != "left || right" {
		t.Fatalf("if expression span = %#v, text = %q", expression[0].Span, file.Text(expression[0].Span))
	}
	assertFileSpans(t, file)
}

func TestVim9CommandBoundaryLogicalOrAndContinuation(t *testing.T) {
	source := "if left\n" +
		"  && right || third | echo 'done'\n" +
		"endif\n"
	file := (Vim9Parser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || countTokens(file, TokenContinuation) != 1 || countTokens(file, TokenSeparator) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
	}
	if file.Commands[0].Canonical != "if" || file.Commands[1].Canonical != "echo" || file.Commands[2].Canonical != "endif" {
		t.Fatalf("commands = %#v", file.Commands)
	}
	if got := file.Text(file.Commands[0].Argument); got != "left\n  && right || third" {
		t.Fatalf("continued if argument = %q", got)
	}
	expression := file.Commands[0].Expressions
	if len(expression) != 1 || expression[0].Kind != ExpressionBinary || expression[0].Value != "||" || len(expression[0].Children) != 2 || expression[0].Children[0].Value != "&&" {
		t.Fatalf("continued if expression = %#v", expression)
	}
	wantStart := strings.Index(source, "left")
	wantEnd := strings.Index(source, "third") + len("third")
	if expression[0].Span != (Span{Start: wantStart, End: wantEnd}) || file.Text(expression[0].Span) != "left\n  && right || third" {
		t.Fatalf("continued if expression span = %#v, text = %q", expression[0].Span, file.Text(expression[0].Span))
	}
	assertFileSpans(t, file)
}

func TestEmptyReturnDoesNotCreateExpression(t *testing.T) {
	tests := []struct {
		name         string
		source       string
		parse        func(string) *File
		returnIndex  int
		commandCount int
	}{
		{name: "legacy", source: "return | echo 1\n", parse: func(source string) *File { return (LegacyParser{}).Parse(source) }, returnIndex: 0, commandCount: 2},
		{name: "vim9", source: "vim9script\nreturn | echo 1\n", parse: func(source string) *File { return (Vim9Parser{}).Parse(source) }, returnIndex: 1, commandCount: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			if len(file.Diagnostics) != 0 || len(file.Commands) != test.commandCount || countTokens(file, TokenSeparator) != 1 {
				t.Fatalf("commands = %#v, diagnostics = %#v, tokens = %#v", file.Commands, file.Diagnostics, file.Tokens)
			}
			returnCommand := file.Commands[test.returnIndex]
			if returnCommand.Canonical != "return" || file.Commands[test.returnIndex+1].Canonical != "echo" || len(returnCommand.Expressions) != 0 || file.Text(returnCommand.Argument) != "" {
				t.Fatalf("commands = %#v, return = %#v", file.Commands, returnCommand)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestLegacySetArgumentEscapesBarsLikeVim(t *testing.T) {
	file := (LegacyParser{}).Parse("setlocal formatlistpat=one\\\\|two | echo 'done'\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[0].Canonical != "setlocal" || file.Commands[1].Canonical != "echo" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if got := file.Text(file.Commands[0].Argument); got != "formatlistpat=one\\\\|two" {
		t.Fatalf("set argument = %q", got)
	}
}

func TestLegacySetArgumentCtrlVProtectsBar(t *testing.T) {
	file := (LegacyParser{}).Parse("setlocal formatlistpat=one\x16|two\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Canonical != "setlocal" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
}

func TestLegacyMenuTranslationMultibyteTrailBar(t *testing.T) {
	source := "scriptencoding cp932\nmenutranslate &Sponsor \x83|text\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Canonical != "menutranslate" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
}

func TestLegacyCatchWithoutPatternSplitsNextCommand(t *testing.T) {
	file := (LegacyParser{}).Parse("try | echo 'one' | catch | echo 'two' | endtry\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 5 || file.Commands[2].Canonical != "catch" || file.Commands[4].Canonical != "endtry" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
}

func TestLegacySyntaxResetSplitsNextCommand(t *testing.T) {
	file := (LegacyParser{}).Parse("if exists('syntax_on') | syntax reset | endif\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || file.Commands[1].Canonical != "syntax" || file.Commands[2].Canonical != "endif" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
}

func TestLegacySubstituteBangIsDelimiterAndRecoversNextCommand(t *testing.T) {
	file := (LegacyParser{}).Parse(":%s!foo\\|bar!replacement!ge | echo 'done'\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[0].Canonical != "substitute" || file.Commands[1].Canonical != "echo" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[0].Bang != (Span{}) || file.Text(file.Commands[0].Argument) != "!foo\\|bar!replacement!ge" {
		t.Fatalf("substitute = %#v, argument = %q", file.Commands[0], file.Text(file.Commands[0].Argument))
	}
}

func TestLegacyDeleteRegisterStartsAfterCommandName(t *testing.T) {
	// find_ex_command() in Vim scans Ex command names as ASCII letters.  The
	// underscore in :delete_ is the black-hole register, not part of the name.
	file := (LegacyParser{}).Parse("1delete_ | echo 'after'\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[0].Canonical != "delete" || file.Commands[1].Canonical != "echo" {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if file.Text(file.Commands[0].Range) != "1" || file.Text(file.Commands[0].Argument) != "_" || countTokens(file, TokenSeparator) != 1 {
		t.Fatalf("delete = %#v, argument = %q, tokens = %#v", file.Commands[0], file.Text(file.Commands[0].Argument), file.Tokens)
	}
}

func TestVim9ExpressionNameIsIndependentFromExCommandName(t *testing.T) {
	file := (Vim9Parser{}).Parse("vim9script\nvar assert_match = (value: string) => value\nassert_match('ok')\necho_value = 'done'\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	for _, index := range []int{2, 3} {
		if file.Commands[index].Kind != CommandExpression {
			t.Fatalf("command %d = %#v", index, file.Commands[index])
		}
	}
}
