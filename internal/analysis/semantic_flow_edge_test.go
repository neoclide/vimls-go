package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestAnalyzeDeterministicRecoveryCombinations(t *testing.T) {
	parts := []string{"var", "def", "class", "interface", "enum", "import", "return", "throw", "try", "catch", "for", "if", "echo", "(", "[", "{", "'text'", "->", "??", "=", "|", "\n"}
	state := uint32(0x9e3779b9)
	next := func() int {
		state = state*1103515245 + 12345
		return int(state % uint32(len(parts)))
	}
	for index := range 512 {
		var source strings.Builder
		if index&1 == 0 {
			source.WriteString("vim9script\n")
		}
		for count := range 9 {
			source.WriteString(parts[next()])
			if count%3 != 2 {
				source.WriteByte(' ')
			}
		}
		file := syntax.Parse(source.String())
		result := Analyze(file)
		if result == nil || result.File != file {
			t.Fatalf("case %d analysis = %#v", index, result)
		}
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Span.Start < 0 || diagnostic.Span.End < diagnostic.Span.Start || diagnostic.Span.End > len(file.Source) {
				t.Fatalf("case %d diagnostic = %#v", index, diagnostic)
			}
		}
	}
}

func TestSemanticFlowRecognizesCompleteAndIncompleteBranches(t *testing.T) {
	returns := []syntax.Command{{Canonical: "return"}}
	throws := []syntax.Command{{Canonical: "throw"}}
	if got := commandSequenceFlow(returns, nil, 0, len(returns)); got != functionFlowReturns || !got.terminates() {
		t.Fatalf("return flow = %v", got)
	}
	if got := commandSequenceFlow(throws, nil, 0, len(throws)); got != functionFlowThrows || !got.terminates() {
		t.Fatalf("throw flow = %v", got)
	}
	if got := commandSequenceFlow(returns, nil, -1, 1); got != functionFlowUnknown {
		t.Fatalf("invalid range flow = %v", got)
	}

	ifCommands := []syntax.Command{
		{Canonical: "if", Block: 0},
		{Canonical: "return"},
		{Canonical: "else"},
		{Canonical: "return"},
		{Canonical: "endif"},
	}
	ifBlock := syntax.Block{Kind: syntax.BlockIf, Header: 0, End: 4, Branches: []int{2}}
	if got := commandSequenceFlow(ifCommands, []syntax.Block{ifBlock}, 0, len(ifCommands)); got != functionFlowReturns {
		t.Fatalf("complete if flow = %v", got)
	}
	ifCommands[3].Canonical = "echo"
	if got := commandSequenceFlow(ifCommands, []syntax.Block{ifBlock}, 0, len(ifCommands)); got != functionFlowFallsThrough {
		t.Fatalf("incomplete if flow = %v", got)
	}

	tryCommands := []syntax.Command{
		{Canonical: "try", Block: 0},
		{Canonical: "echo"},
		{Canonical: "finally"},
		{Canonical: "return"},
		{Canonical: "endtry"},
	}
	tryBlock := syntax.Block{Kind: syntax.BlockTry, Header: 0, End: 4, Branches: []int{2}}
	if got := commandSequenceFlow(tryCommands, []syntax.Block{tryBlock}, 0, len(tryCommands)); got != functionFlowReturns {
		t.Fatalf("finally return flow = %v", got)
	}
	tryCommands[3].Canonical = "echo"
	if got := commandSequenceFlow(tryCommands, []syntax.Block{tryBlock}, 0, len(tryCommands)); got != functionFlowFallsThrough {
		t.Fatalf("finally fall-through flow = %v", got)
	}
}

func TestSemanticFlowBlockFamilyAndMalformedHeaderBoundaries(t *testing.T) {
	commands := []syntax.Command{{Canonical: "for", Block: 0}, {Canonical: "endfor"}}
	for _, kind := range []syntax.BlockKind{syntax.BlockFor, syntax.BlockWhile, syntax.BlockFunction, syntax.BlockDef, syntax.BlockClass, syntax.BlockInterface, syntax.BlockEnum, syntax.BlockCommand} {
		block := syntax.Block{Kind: kind, Header: 0, End: 1}
		if got := commandBlockFlow(commands, []syntax.Block{block}, block); got != functionFlowFallsThrough {
			t.Errorf("block flow %v = %v", kind, got)
		}
	}
	unknown := syntax.Block{Kind: syntax.BlockKind("unknown"), Header: 0, End: 1}
	if got := commandBlockFlow(commands, []syntax.Block{unknown}, unknown); got != functionFlowUnknown {
		t.Fatalf("unknown block flow = %v", got)
	}
	malformed := []syntax.Command{{Canonical: "if", Block: 0}, {Canonical: "echo"}}
	block := syntax.Block{Kind: syntax.BlockIf, Header: 0, End: 5}
	if got := commandSequenceFlow(malformed, []syntax.Block{block}, 0, len(malformed)); got != functionFlowUnknown {
		t.Fatalf("malformed block flow = %v", got)
	}
	if got := commandSequenceFlow([]syntax.Command{{Canonical: "echo"}}, nil, 0, 1); got != functionFlowFallsThrough {
		t.Fatalf("ordinary flow = %v", got)
	}
}

func TestSemanticSymbolHelpersRetainRecoveryAndContainment(t *testing.T) {
	file := &syntax.File{Source: "class Box\nendclass\n"}
	member := &syntax.Expression{Kind: syntax.ExpressionMember, Value: "Run", Span: syntax.Span{Start: 0, End: 9}}
	if got := memberNameSpan(file, member); got != member.Span {
		t.Fatalf("unmatched member value span = %#v", got)
	}
	file.Source = "object.Run"
	member.Span = syntax.Span{Start: 0, End: len(file.Source)}
	if got := memberNameSpan(file, member); got != (syntax.Span{Start: 7, End: 10}) {
		t.Fatalf("member value span = %#v", got)
	}
	for _, test := range []struct {
		literal, want string
	}{
		{"'Function'", "Function"},
		{"\"Function\"", "Function"},
		{"'Func''tion'", ""},
		{"unquoted", ""},
		{"'unterminated", ""},
	} {
		if got := simpleVimStringLiteral(test.literal); got != test.want {
			t.Errorf("simpleVimStringLiteral(%q) = %q, want %q", test.literal, got, test.want)
		}
	}

	blocks := []syntax.Block{
		{Kind: syntax.BlockClass, Header: 0, Parent: -1},
		{Kind: syntax.BlockDef, Header: 1, Parent: 0},
		{Kind: syntax.BlockIf, Header: 2, Parent: 1},
	}
	if got := parentBlock(blocks, 1, -1); got != 0 {
		t.Fatalf("parent block = %d", got)
	}
	if got := parentBlock(blocks, -1, 9); got != 9 {
		t.Fatalf("fallback parent block = %d", got)
	}
	if got := ownBlock(blocks, 1, 1); got != 1 {
		t.Fatalf("own def block = %d", got)
	}
	if got := ownBlock(blocks, 3, 1); got != -1 {
		t.Fatalf("unrelated def block = %d", got)
	}
	if got := aggregateBlock(blocks, 0, 0, syntax.BlockClass); got != 0 {
		t.Fatalf("class aggregate block = %d", got)
	}
	if got := aggregateBlock(blocks, 0, 1, syntax.BlockClass); got != -1 {
		t.Fatalf("wrong aggregate block = %d", got)
	}
}

func TestAnalyzeSearchpairCompiledExpressionStrings(t *testing.T) {
	for _, test := range []struct {
		name, expression, code string
	}{
		{name: "identifier", expression: "missing", code: "vim/E1001"},
		{name: "member root", expression: "missing.member", code: "vim/E1001"},
		{name: "dictionary value", expression: "{key: missing}", code: "vim/E1001"},
		{name: "unsupported namespace", expression: "a:missing", code: "vim/E1075"},
		{name: "call is resolved as a function", expression: "missing()", code: ""},
		{name: "generic call is resolved as a function", expression: "missing<number>()", code: ""},
		{name: "lambda is intentionally not compiled", expression: "() => missing", code: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "vim9script\ndef Search()\n  searchpair('(', ')', '', '', '" + test.expression + "')\nenddef\n"
			file := syntax.Parse(source)
			result := Analyze(file)
			found := false
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == test.code {
					found = true
				}
			}
			if test.code != "" && !found {
				t.Fatalf("diagnostics = %#v", result.Diagnostics)
			}
			if test.code == "" && found {
				t.Fatalf("lambda compiled unexpectedly: %#v", result.Diagnostics)
			}
		})
	}
}

func TestAnalyzeGenericAndSuperDiagnosticRecovery(t *testing.T) {
	for _, test := range []struct {
		name, source, code string
	}{
		{
			name:   "unknown generic function",
			source: "vim9script\nMissing<number>()\n",
			code:   "vim/E1558",
		},
		{
			name:   "non-generic function",
			source: "vim9script\ndef Plain()\nenddef\nPlain<number>()\n",
			code:   "vim/E1560",
		},
		{
			name:   "super outside class method",
			source: "vim9script\ndef F()\n  super.Run()\nenddef\n",
			code:   "vim/E1357",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == test.code {
					return
				}
			}
			t.Fatalf("diagnostics = %#v", result.Diagnostics)
		})
	}
}
