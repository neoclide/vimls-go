package syntax

import (
	"strings"
	"testing"
)

func TestSyntaxKeywordPreservesWordsOptionsAndBoundary(t *testing.T) {
	source := "syntax keyword VimComment contained foo[bar] display fold extend | echo done\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	node := file.Commands[0].Syntax
	if node == nil || node.Kind != SyntaxKeyword || file.Text(node.Group) != "VimComment" || len(node.Keywords) != 4 {
		t.Fatalf("syntax=%#v", node)
	}
	for index, want := range []string{"foo[bar]", "display", "fold", "extend"} {
		if got := file.Text(node.Keywords[index]); got != want {
			t.Fatalf("keyword %d=%q want %q", index, got, want)
		}
	}
	if len(node.Options) != 1 || file.Text(node.Options[0].Name) != "contained" {
		t.Fatalf("options=%#v", node.Options)
	}
	if file.Commands[1].Canonical != "echo" || countTokens(file, TokenSeparator) != 1 {
		t.Fatalf("following command=%#v tokens=%#v", file.Commands[1], file.Tokens)
	}
	assertFileSpans(t, file)
}

func TestSyntaxKeywordDialectBoundaries(t *testing.T) {
	legacy := (LegacyParser{}).Parse("syntax keyword Group foo|bar word\"tail contained\" comment | echo hidden\nlet g:after = 1\n")
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 2 || legacy.Commands[1].Canonical != "let" {
		t.Fatalf("legacy commands=%#v diagnostics=%#v", legacy.Commands, legacy.Diagnostics)
	}
	legacySyntax := legacy.Commands[0].Syntax
	if legacySyntax == nil || len(legacySyntax.Keywords) != 2 || legacy.Text(legacySyntax.Keywords[0]) != "foo|bar" || legacy.Text(legacySyntax.Keywords[1]) != `word"tail` || len(legacySyntax.Options) != 1 {
		t.Fatalf("legacy syntax=%#v", legacySyntax)
	}
	if countTokens(legacy, TokenComment) != 1 || countTokens(legacy, TokenSeparator) != 0 {
		t.Fatalf("legacy tokens=%#v", legacy.Tokens)
	}

	vim9 := (Vim9Parser{}).Parse("vim9script\nsyntax keyword Group foo|bar word#tail contained # comment | echo hidden\nvar after = 1\n")
	if len(vim9.Diagnostics) != 0 || len(vim9.Commands) != 3 || vim9.Commands[2].Canonical != "var" {
		t.Fatalf("vim9 commands=%#v diagnostics=%#v", vim9.Commands, vim9.Diagnostics)
	}
	vim9Syntax := vim9.Commands[1].Syntax
	if vim9Syntax == nil || len(vim9Syntax.Keywords) != 2 || vim9.Text(vim9Syntax.Keywords[0]) != "foo|bar" || vim9.Text(vim9Syntax.Keywords[1]) != "word#tail" || len(vim9Syntax.Options) != 1 {
		t.Fatalf("vim9 syntax=%#v", vim9Syntax)
	}
	if countTokens(vim9, TokenComment) != 1 || countTokens(vim9, TokenSeparator) != 0 {
		t.Fatalf("vim9 tokens=%#v", vim9.Tokens)
	}

	adjacent := (Vim9Parser{}).Parse("vim9script\nsyntax keyword Group contained#tail | echo next\n")
	if len(adjacent.Diagnostics) != 0 || len(adjacent.Commands) != 3 || adjacent.Commands[2].Canonical != "echo" || len(adjacent.Commands[1].Syntax.Options) != 0 || adjacent.Text(adjacent.Commands[1].Syntax.Keywords[0]) != "contained#tail" {
		t.Fatalf("adjacent commands=%#v syntax=%#v diagnostics=%#v", adjacent.Commands, adjacent.Commands[1].Syntax, adjacent.Diagnostics)
	}
}

func TestSyntaxKeywordExplicitTerminatorAllowsNoKeyword(t *testing.T) {
	for _, test := range []struct {
		name  string
		parse func(string) *File
		tail  string
	}{
		{name: "bar", parse: func(s string) *File { return (LegacyParser{}).Parse(s) }, tail: " | echo next\nlet g:after = 1\n"},
		{name: "comment", parse: func(s string) *File { return (LegacyParser{}).Parse(s) }, tail: " \" comment\nlet g:after = 1\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse("syntax keyword Group" + test.tail)
			wantCommands := 2
			letIndex := 1
			if test.name == "bar" {
				wantCommands = 3
				letIndex = 2
			}
			if len(file.Diagnostics) != 0 || len(file.Commands) != wantCommands || file.Commands[letIndex].Canonical != "let" {
				t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
			}
			if file.Commands[0].Syntax == nil || len(file.Commands[0].Syntax.Keywords) != 0 {
				t.Fatalf("syntax=%#v", file.Commands[0].Syntax)
			}
		})
	}
	file := (LegacyParser{}).Parse("syntax keyword Group\nlet g:after = 1\n")
	if !hasDiagnostic(file, "vim/E475") || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" {
		t.Fatalf("missing keyword commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
}

func TestSyntaxMatchRegexpOptionsAndOffsets(t *testing.T) {
	source := "syntax match String contains=Foo,Bar /a[|]\\/b/ms=s+1,me=e-1 containedin=Other | echo ok\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	node := file.Commands[0].Syntax
	if node == nil || node.Kind != SyntaxMatch || file.Text(node.Group) != "String" || len(node.Patterns) != 1 {
		t.Fatalf("syntax=%#v", node)
	}
	pattern := node.Patterns[0]
	if file.Text(pattern.Pattern) != `a[|]\/b` || file.Text(pattern.Offsets) != "ms=s+1,me=e-1" {
		t.Fatalf("pattern=%#v text=%q offsets=%q", pattern, file.Text(pattern.Pattern), file.Text(pattern.Offsets))
	}
	if len(node.Options) != 2 || file.Text(node.Options[0].Name) != "contains" || len(node.Options[0].Values) != 2 || file.Text(node.Options[1].Name) != "containedin" {
		t.Fatalf("options=%#v", node.Options)
	}
	assertFileSpans(t, file)
}

func TestSyntaxMatchPatternOwnsCommentBytes(t *testing.T) {
	legacy := (LegacyParser{}).Parse("syntax match Group \"foo|bar\" contained\" comment | echo hidden\nlet g:after = 1\n")
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 2 || legacy.Commands[1].Canonical != "let" || legacy.Text(legacy.Commands[0].Syntax.Patterns[0].Pattern) != "foo|bar" {
		t.Fatalf("legacy commands=%#v syntax=%#v diagnostics=%#v", legacy.Commands, legacy.Commands[0].Syntax, legacy.Diagnostics)
	}

	valid := (Vim9Parser{}).Parse("vim9script\nsyntax match Group #foo|bar# contained # comment | echo hidden\nvar after = 1\n")
	if len(valid.Diagnostics) != 0 || len(valid.Commands) != 3 || valid.Commands[2].Canonical != "var" || valid.Text(valid.Commands[1].Syntax.Patterns[0].Pattern) != "foo|bar" {
		t.Fatalf("valid commands=%#v syntax=%#v diagnostics=%#v", valid.Commands, valid.Commands[1].Syntax, valid.Diagnostics)
	}

	invalid := (Vim9Parser{}).Parse("vim9script\nsyntax match Group /pat/# comment | echo same\nvar after = 1\n")
	if !hasDiagnostic(invalid, "vim/E402") || len(invalid.Commands) != 3 || invalid.Commands[2].Canonical != "var" || countTokens(invalid, TokenSeparator) != 0 {
		t.Fatalf("invalid commands=%#v diagnostics=%#v tokens=%#v", invalid.Commands, invalid.Diagnostics, invalid.Tokens)
	}
}

func TestSyntaxPatternOffsetZeroForms(t *testing.T) {
	file := (LegacyParser{}).Parse("syntax match Group /x/ms=s,me=e+,lc= | echo next\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Canonical != "echo" {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	pattern := file.Commands[0].Syntax.Patterns[0]
	if got := file.Text(pattern.Offsets); got != "ms=s,me=e+,lc=" {
		t.Fatalf("offsets=%q pattern=%#v", got, pattern)
	}
}

func TestSyntaxPatternCollectionFallbacks(t *testing.T) {
	source := "syntax match One /[^\\\\][][.*?+]\\+/\n" +
		"syntax match Two \"[^[:space:]]/[^[:space]]\"ms=s+1,me=e-1\n" +
		"syntax match Three /[| \\t([.,=\\]]\\@<=x/\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	for index := range file.Commands {
		if file.Commands[index].Syntax == nil || len(file.Commands[index].Syntax.Patterns) != 1 || file.Commands[index].Syntax.Patterns[0].CloseDelimiter == (Span{}) {
			t.Fatalf("command %d syntax=%#v", index, file.Commands[index].Syntax)
		}
	}
}

func TestSyntaxCCharOwnsSeparatorByte(t *testing.T) {
	file := (LegacyParser{}).Parse("syntax match Group /x/ cchar=| | echo next\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Canonical != "echo" {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	option := file.Commands[0].Syntax.Options[0]
	if file.Text(option.Name) != "cchar" || file.Text(option.Value) != "|" || countTokens(file, TokenSeparator) != 1 {
		t.Fatalf("option=%#v tokens=%#v", option, file.Tokens)
	}
}

func TestSyntaxMatchBarIsPatternDelimiterAndMalformedOwnsLine(t *testing.T) {
	file := (LegacyParser{}).Parse("syntax match Group | next\nlet g:after = 1\n")
	if !hasDiagnostic(file, "vim/E401") || len(file.Commands) != 2 || file.Commands[0].Canonical != "syntax" || file.Commands[1].Canonical != "let" {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	if file.Commands[0].Syntax == nil || len(file.Commands[0].Syntax.Patterns) != 1 || file.Text(file.Commands[0].Syntax.Patterns[0].OpenDelimiter) != "|" {
		t.Fatalf("syntax=%#v", file.Commands[0].Syntax)
	}
	if countTokens(file, TokenSeparator) != 0 {
		t.Fatalf("tokens=%#v", file.Tokens)
	}
}

func TestSyntaxOptionSourceExactWhitespaceAndTrailingComma(t *testing.T) {
	file := (LegacyParser{}).Parse("syntax match Group /x/ contains = Foo, Bar cchar=😀\nsyntax match Trailing /x/ contains=Foo,Bar,\nsyntax keyword Group contained cchar =x =x\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	match := file.Commands[0].Syntax
	if match == nil || len(match.Options) != 2 || len(match.Options[0].Values) != 2 || file.Text(match.Options[0].Value) != "Foo, Bar" || file.Text(match.Options[1].Value) != "😀" {
		t.Fatalf("match=%#v", match)
	}
	trailing := file.Commands[1].Syntax
	if trailing == nil || len(trailing.Options) != 1 || file.Text(trailing.Options[0].Value) != "Foo,Bar," || len(trailing.Options[0].Values) != 2 {
		t.Fatalf("trailing=%#v", trailing)
	}
	keyword := file.Commands[2].Syntax
	if keyword == nil || len(keyword.Options) != 2 || keyword.Options[1].Value != (Span{}) || len(keyword.Keywords) != 2 || file.Text(keyword.Keywords[0]) != "=x" || file.Text(keyword.Keywords[1]) != "=x" {
		t.Fatalf("keyword=%#v", keyword)
	}
}

func TestSyntaxKeywordOptionalSuffixDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		word string
		code string
	}{
		{name: "missing close", word: "a[", code: "vim/E789"},
		{name: "trailing", word: "a[bc]d", code: "vim/E890"},
		{name: "escaped open still expands", word: `a\[`, code: "vim/E789"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := (LegacyParser{}).Parse("syntax keyword Group " + test.word + " | echo same\nlet g:after = 1\n")
			if !hasDiagnostic(file, test.code) || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" || countTokens(file, TokenSeparator) != 0 {
				t.Fatalf("commands=%#v diagnostics=%#v tokens=%#v", file.Commands, file.Diagnostics, file.Tokens)
			}
		})
	}
}

func TestSyntaxEmptyMatchPatternNeedsThirdByte(t *testing.T) {
	for _, source := range []string{
		"syntax match Group //\nlet g:after = 1\n",
		"syntax match Group // \nlet g:after = 1\n",
		"syntax match Group // | let g:after = 1\n",
	} {
		file := (LegacyParser{}).Parse(source)
		if strings.HasSuffix(source, "//\nlet g:after = 1\n") {
			if !hasDiagnostic(file, "vim/E475") || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" {
				t.Fatalf("source=%q commands=%#v diagnostics=%#v", source, file.Commands, file.Diagnostics)
			}
			continue
		}
		if len(file.Diagnostics) != 0 {
			t.Fatalf("source=%q diagnostics=%#v", source, file.Diagnostics)
		}
	}
}

func TestSyntaxRegionPatternsAndCaseInsensitiveKeys(t *testing.T) {
	source := "syntax region Here matchGROUP = NONE start=+foo|bar+ skip = /skip/ END=\"end\" contains=Inner,Other keepend | echo ok\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	node := file.Commands[0].Syntax
	if node == nil || node.Kind != SyntaxRegion || len(node.Patterns) != 3 {
		t.Fatalf("syntax=%#v", node)
	}
	wantKinds := []SyntaxPatternKind{SyntaxStartPattern, SyntaxSkipPattern, SyntaxEndPattern}
	for index, want := range wantKinds {
		if node.Patterns[index].Kind != want {
			t.Fatalf("pattern %d=%#v want kind %d", index, node.Patterns[index], want)
		}
	}
	if file.Text(node.Patterns[0].Pattern) != "foo|bar" || file.Text(node.Patterns[1].Pattern) != "skip" || file.Text(node.Patterns[2].Pattern) != "end" {
		t.Fatalf("patterns=%#v", node.Patterns)
	}
	if len(node.Options) != 3 || file.Text(node.Options[0].Name) != "matchGROUP" || file.Text(node.Options[0].Value) != "NONE" || file.Text(node.Options[1].Name) != "contains" {
		t.Fatalf("options=%#v", node.Options)
	}
	assertFileSpans(t, file)
}

func TestSyntaxRegionAllowsRepeatedStartEndInAnyOrder(t *testing.T) {
	file := (LegacyParser{}).Parse("syntax region Group end=/one/ start=/two/ end=/three/ start=/four/\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Syntax == nil {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	want := []SyntaxPatternKind{SyntaxEndPattern, SyntaxStartPattern, SyntaxEndPattern, SyntaxStartPattern}
	for index, kind := range want {
		if file.Commands[0].Syntax.Patterns[index].Kind != kind {
			t.Fatalf("patterns=%#v", file.Commands[0].Syntax.Patterns)
		}
	}
}

func TestSyntaxStructuralErrorsRecoverNextLine(t *testing.T) {
	tests := []struct {
		name string
		src  string
		code string
	}{
		{name: "keyword contains", src: "syntax keyword Group contains=Other | echo same\nlet g:after = 1\n", code: "vim/E395"},
		{name: "match trailing", src: "syntax match Group /x/ garbage | echo same\nlet g:after = 1\n", code: "vim/E475"},
		{name: "region missing equal", src: "syntax region Group start /x/ end=/y/ | echo same\nlet g:after = 1\n", code: "vim/E398"},
		{name: "region incomplete", src: "syntax region Group start=/x/ | echo same\nlet g:after = 1\n", code: "vim/E399"},
		{name: "sync-only option", src: "syntax match Group grouphere /x/ | echo same\nlet g:after = 1\n", code: "vim/E393"},
		{name: "empty list", src: "syntax match Group contains= | echo same\nlet g:after = 1\n", code: "vim/E406"},
		{name: "list missing equal", src: "syntax match Group contains /x/ | echo same\nlet g:after = 1\n", code: "vim/E405"},
		{name: "immediate pattern garbage", src: "syntax match Group /x/a | echo same\nlet g:after = 1\n", code: "vim/E402"},
		{name: "second skip", src: "syntax region Group start=/x/ skip=/a/ skip=/b/ end=/y/ | echo same\nlet g:after = 1\n", code: "vim/E475"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := (LegacyParser{}).Parse(test.src)
			if !hasDiagnostic(file, test.code) || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" {
				t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
			}
			if countTokens(file, TokenSeparator) != 0 {
				t.Fatalf("tokens=%#v", file.Tokens)
			}
		})
	}
}

func TestSyntaxClusterPreservesOperationsAndBoundary(t *testing.T) {
	source := "syntax cluster Outer ConTains = One,, @Inner add=Three remove = Missing | echo ok\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Canonical != "echo" {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	node := file.Commands[0].Syntax
	if node == nil || node.Kind != SyntaxCluster || file.Text(node.Group) != "Outer" || len(node.Options) != 3 {
		t.Fatalf("cluster=%#v", node)
	}
	wantNames := []string{"ConTains", "add", "remove"}
	wantValues := [][]string{{"One", "", "@Inner"}, {"Three"}, {"Missing"}}
	for index, option := range node.Options {
		if got := file.Text(option.Name); got != wantNames[index] {
			t.Fatalf("option %d name=%q want %q", index, got, wantNames[index])
		}
		if len(option.Values) != len(wantValues[index]) {
			t.Fatalf("option %d values=%#v", index, option.Values)
		}
		for valueIndex, value := range option.Values {
			if got := file.Text(value); got != wantValues[index][valueIndex] {
				t.Fatalf("option %d value %d=%q want %q", index, valueIndex, got, wantValues[index][valueIndex])
			}
		}
	}
	if got := file.Text(node.Options[0].Value); got != "One,, @Inner" {
		t.Fatalf("contains value=%q", got)
	}
	assertFileSpans(t, file)

	special := (LegacyParser{}).Parse("syntax cluster All contains=ALL,One\n")
	if len(special.Diagnostics) != 0 || special.Commands[0].Syntax == nil || len(special.Commands[0].Syntax.Options[0].Values) != 2 {
		t.Fatalf("special cluster=%#v diagnostics=%#v", special.Commands[0].Syntax, special.Diagnostics)
	}
}

func TestSyntaxClusterVim9CommentBoundary(t *testing.T) {
	valid := (Vim9Parser{}).Parse("vim9script\nsyntax cluster Some contains=Word # comment\nvar after = 1\n")
	if len(valid.Diagnostics) != 0 || len(valid.Commands) != 3 || valid.Commands[2].Canonical != "var" || countTokens(valid, TokenComment) != 1 {
		t.Fatalf("valid commands=%#v diagnostics=%#v tokens=%#v", valid.Commands, valid.Diagnostics, valid.Tokens)
	}
	if node := valid.Commands[1].Syntax; node == nil || node.Kind != SyntaxCluster || valid.Text(node.Options[0].Values[0]) != "Word" {
		t.Fatalf("valid cluster=%#v", valid.Commands[1].Syntax)
	}

	invalid := (Vim9Parser{}).Parse("vim9script\nsyntax cluster Some contains=Word# comment | echo same\nvar after = 1\n")
	if !hasDiagnostic(invalid, "vim/E475") || len(invalid.Commands) != 3 || invalid.Commands[2].Canonical != "var" || countTokens(invalid, TokenSeparator) != 0 {
		t.Fatalf("invalid commands=%#v diagnostics=%#v tokens=%#v", invalid.Commands, invalid.Diagnostics, invalid.Tokens)
	}
	node := invalid.Commands[1].Syntax
	if node == nil || len(node.Options) != 1 || invalid.Text(node.Options[0].Values[0]) != "Word#" {
		t.Fatalf("invalid cluster=%#v", node)
	}
}

func TestSyntaxClusterStructuralRecovery(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		code   string
		option int
	}{
		{name: "missing name", line: "syntax cluster", code: "vim/E400"},
		{name: "missing operation", line: "syntax cluster Outer", code: "vim/E400"},
		{name: "operation as name", line: "syntax cluster contains=Abc", code: "vim/E400"},
		{name: "missing equal", line: "syntax cluster Outer add One", code: "vim/E405", option: 1},
		{name: "empty repeated value", line: "syntax cluster Outer add=A add=", code: "vim/E406", option: 2},
		{name: "special in add", line: "syntax cluster Outer add=ALL | echo hidden", code: "vim/E407", option: 1},
		{name: "special not first", line: "syntax cluster Outer contains=A,ALL", code: "vim/E408", option: 1},
		{name: "empty item counts", line: "syntax cluster Outer contains=,ALL", code: "vim/E408", option: 1},
		{name: "trailing argument", line: "syntax cluster Outer contains=A trailing | echo hidden", code: "vim/E475", option: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := (LegacyParser{}).Parse(test.line + "\nlet g:after = 1\n")
			if !hasDiagnostic(file, test.code) || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" || countTokens(file, TokenSeparator) != 0 {
				t.Fatalf("commands=%#v diagnostics=%#v tokens=%#v", file.Commands, file.Diagnostics, file.Tokens)
			}
			node := file.Commands[0].Syntax
			if node == nil || node.Kind != SyntaxCluster || len(node.Options) != test.option {
				t.Fatalf("cluster=%#v want options=%d", node, test.option)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestSyntaxGroupListStructuralDiagnosticsAreConservative(t *testing.T) {
	for _, test := range []struct {
		name string
		line string
		code string
	}{
		{name: "nextgroup special", line: "syntax match Group /x/ nextgroup=ALLBUT,F", code: "vim/E407"},
		{name: "contains special position", line: "syntax region Group start=/x/ end=/y/ contains=F,ALL", code: "vim/E408"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := (LegacyParser{}).Parse(test.line + " | echo hidden\nlet g:after = 1\n")
			if !hasDiagnostic(file, test.code) || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" || countTokens(file, TokenSeparator) != 0 {
				t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
			}
		})
	}
	file := (LegacyParser{}).Parse("syntax match Group /x/ contains=a.*x\n")
	if len(file.Diagnostics) != 0 || file.Commands[0].Syntax == nil {
		t.Fatalf("dynamic group diagnostics=%#v syntax=%#v", file.Diagnostics, file.Commands[0].Syntax)
	}
}

func TestSyntaxClusterLogicalContinuationSpans(t *testing.T) {
	source := "syntax cluster Outer\n  \\ contains=One,\n  \\ @Inner add=Three\nlet g:after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	node := file.Commands[0].Syntax
	if node == nil || node.Kind != SyntaxCluster || len(node.Options) != 2 || len(node.Options[0].Values) != 2 || file.Text(node.Options[0].Values[1]) != "@Inner" {
		t.Fatalf("cluster=%#v", node)
	}
	assertFileSpans(t, file)
}

func TestSyntaxCasePreservesModeAndBoundary(t *testing.T) {
	file := (LegacyParser{}).Parse("syntax case IgNoRe ignored bytes | echo ok\nsyntax case\nlet g:after = 1\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 || file.Commands[1].Canonical != "echo" || file.Commands[3].Canonical != "let" {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	set := file.Commands[0].Syntax
	if set == nil || set.Kind != SyntaxCase || len(set.Keywords) != 1 || file.Text(set.Keywords[0]) != "IgNoRe" || file.Text(file.Commands[0].Argument) != "case IgNoRe ignored bytes" {
		t.Fatalf("case set=%#v argument=%q", set, file.Text(file.Commands[0].Argument))
	}
	query := file.Commands[2].Syntax
	if query == nil || query.Kind != SyntaxCase || len(query.Keywords) != 0 {
		t.Fatalf("case query=%#v", query)
	}
	assertFileSpans(t, file)
}

func TestSyntaxCaseTrailingBytesAreNotComments(t *testing.T) {
	legacy := (LegacyParser{}).Parse("syntax case match \" ignored | echo ok\n")
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 2 || legacy.Commands[1].Canonical != "echo" || countTokens(legacy, TokenComment) != 0 {
		t.Fatalf("legacy commands=%#v diagnostics=%#v tokens=%#v", legacy.Commands, legacy.Diagnostics, legacy.Tokens)
	}
	vim9 := (Vim9Parser{}).Parse("vim9script\nsyntax case ignore # ignored | echo ok\n")
	if len(vim9.Diagnostics) != 0 || len(vim9.Commands) != 3 || vim9.Commands[2].Canonical != "echo" || countTokens(vim9, TokenComment) != 0 {
		t.Fatalf("vim9 commands=%#v diagnostics=%#v tokens=%#v", vim9.Commands, vim9.Diagnostics, vim9.Tokens)
	}
}

func TestSyntaxCaseInvalidArgumentOwnsLine(t *testing.T) {
	for _, line := range []string{
		"syntax case invalid | echo hidden",
		"syntax case | echo hidden",
		"syntax case match|echo hidden",
	} {
		file := (LegacyParser{}).Parse(line + "\nlet g:after = 1\n")
		if !hasDiagnostic(file, "vim/E390") || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" || countTokens(file, TokenSeparator) != 0 {
			t.Fatalf("line=%q commands=%#v diagnostics=%#v tokens=%#v", line, file.Commands, file.Diagnostics, file.Tokens)
		}
		if node := file.Commands[0].Syntax; node == nil || node.Kind != SyntaxCase || len(node.Keywords) != 0 {
			t.Fatalf("line=%q case=%#v", line, node)
		}
	}
}

func TestSyntaxConcealAndSpellModes(t *testing.T) {
	tests := []struct {
		name    string
		command string
		value   string
		kind    SyntaxKind
	}{
		{name: "conceal", command: "conceal", value: "On", kind: SyntaxConceal},
		{name: "spell", command: "spell", value: "NoTopLevel", kind: SyntaxSpell},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := "syntax " + test.command + " " + test.value + " ignored # bytes | echo ok\nsyntax " + test.command + "\n"
			file := (Vim9Parser{}).Parse("vim9script\n" + source)
			if len(file.Diagnostics) != 0 || len(file.Commands) != 4 || file.Commands[2].Canonical != "echo" || countTokens(file, TokenComment) != 0 {
				t.Fatalf("commands=%#v diagnostics=%#v tokens=%#v", file.Commands, file.Diagnostics, file.Tokens)
			}
			set := file.Commands[1].Syntax
			if set == nil || set.Kind != test.kind || len(set.Keywords) != 1 || file.Text(set.Keywords[0]) != test.value {
				t.Fatalf("set=%#v", set)
			}
			query := file.Commands[3].Syntax
			if query == nil || query.Kind != test.kind || len(query.Keywords) != 0 {
				t.Fatalf("query=%#v", query)
			}
			assertFileSpans(t, file)
		})
	}

	for _, command := range []string{"conceal invalid", "spell invalid"} {
		file := (LegacyParser{}).Parse("syntax " + command + " | echo hidden\nlet g:after = 1\n")
		if !hasDiagnostic(file, "vim/E390") || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" || countTokens(file, TokenSeparator) != 0 {
			t.Fatalf("command=%q commands=%#v diagnostics=%#v", command, file.Commands, file.Diagnostics)
		}
	}
}

func TestSyntaxIncludeFilenameAndBoundary(t *testing.T) {
	file := (LegacyParser{}).Parse("syntax include @Pod runtime path with spaces.vim | echo next\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Canonical != "echo" {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	node := file.Commands[0].Syntax
	if node == nil || node.Kind != SyntaxInclude || file.Text(node.Group) != "Pod" || len(node.Keywords) != 1 || file.Text(node.Keywords[0]) != "runtime path with spaces.vim " {
		t.Fatalf("include=%#v filename=%q", node, file.Text(node.Keywords[0]))
	}
	if countTokens(file, TokenSeparator) != 1 {
		t.Fatalf("tokens=%#v", file.Tokens)
	}

	noGroup := (LegacyParser{}).Parse("syntax include runtime path with spaces.vim\n")
	if len(noGroup.Diagnostics) != 0 || noGroup.Commands[0].Syntax == nil || noGroup.Text(noGroup.Commands[0].Syntax.Keywords[0]) != "runtime path with spaces.vim" {
		t.Fatalf("no-group include=%#v diagnostics=%#v", noGroup.Commands[0].Syntax, noGroup.Diagnostics)
	}
	trailing := (LegacyParser{}).Parse("syntax include runtime/file.vim  \t\n")
	if len(trailing.Diagnostics) != 0 || trailing.Text(trailing.Commands[0].Argument) != "include runtime/file.vim  \t" || trailing.Text(trailing.Commands[0].Syntax.Keywords[0]) != "runtime/file.vim  \t" {
		t.Fatalf("trailing argument=%q include=%#v diagnostics=%#v", trailing.Text(trailing.Commands[0].Argument), trailing.Commands[0].Syntax, trailing.Diagnostics)
	}
	assertFileSpans(t, file)
	assertFileSpans(t, noGroup)
	assertFileSpans(t, trailing)
}

func TestSyntaxIncludeDoesNotTreatQuotesOrHashesAsComments(t *testing.T) {
	legacy := (LegacyParser{}).Parse("syntax include runtime/quote\"hash#file.vim | echo next\n")
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 2 || legacy.Commands[1].Canonical != "echo" || countTokens(legacy, TokenComment) != 0 {
		t.Fatalf("legacy commands=%#v diagnostics=%#v tokens=%#v", legacy.Commands, legacy.Diagnostics, legacy.Tokens)
	}
	if got := legacy.Text(legacy.Commands[0].Syntax.Keywords[0]); got != "runtime/quote\"hash#file.vim " {
		t.Fatalf("legacy filename=%q", got)
	}

	vim9 := (Vim9Parser{}).Parse("vim9script\nsyntax include runtime/quote\"hash#file.vim | echo next\n")
	if len(vim9.Diagnostics) != 0 || len(vim9.Commands) != 3 || vim9.Commands[2].Canonical != "echo" || countTokens(vim9, TokenComment) != 0 {
		t.Fatalf("vim9 commands=%#v diagnostics=%#v tokens=%#v", vim9.Commands, vim9.Diagnostics, vim9.Tokens)
	}
	if got := vim9.Text(vim9.Commands[1].Syntax.Keywords[0]); got != "runtime/quote\"hash#file.vim " {
		t.Fatalf("vim9 filename=%q", got)
	}
}

func TestSyntaxIncludeEscapedAndCtrlVBar(t *testing.T) {
	legacy := (LegacyParser{}).Parse("syntax include runtime/foo\\|bar.vim | echo next\n")
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 2 || legacy.Commands[1].Canonical != "echo" {
		t.Fatalf("escaped bar commands=%#v diagnostics=%#v", legacy.Commands, legacy.Diagnostics)
	}
	if got := legacy.Text(legacy.Commands[0].Syntax.Keywords[0]); got != "runtime/foo\\|bar.vim " {
		t.Fatalf("escaped filename=%q", got)
	}

	ctrlV := (LegacyParser{}).Parse("syntax include runtime/foo\x16|bar.vim | echo next\n")
	if len(ctrlV.Diagnostics) != 0 || len(ctrlV.Commands) != 2 || ctrlV.Commands[1].Canonical != "echo" {
		t.Fatalf("ctrl-v commands=%#v diagnostics=%#v", ctrlV.Commands, ctrlV.Diagnostics)
	}
	if got := ctrlV.Text(ctrlV.Commands[0].Syntax.Keywords[0]); got != "runtime/foo\x16|bar.vim " {
		t.Fatalf("ctrl-v filename=%q", got)
	}
}

func TestSyntaxIncludeBacktickExpressionBoundary(t *testing.T) {
	file := (LegacyParser{}).Parse("syntax include `=fname`.vim | echo next\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Canonical != "echo" {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	if got := file.Text(file.Commands[0].Syntax.Keywords[0]); got != "`=fname`.vim " {
		t.Fatalf("filename=%q", got)
	}

	withBar := (LegacyParser{}).Parse("syntax include `=printf('a|b')`.vim | echo next\n")
	if len(withBar.Diagnostics) != 0 || len(withBar.Commands) != 2 || withBar.Commands[1].Canonical != "echo" {
		t.Fatalf("expression bar commands=%#v diagnostics=%#v", withBar.Commands, withBar.Diagnostics)
	}
	if got := withBar.Text(withBar.Commands[0].Syntax.Keywords[0]); got != "`=printf('a|b')`.vim " {
		t.Fatalf("expression filename=%q", got)
	}
}

func TestSyntaxIncludeRecoveryAndEmptyFilename(t *testing.T) {
	end := (LegacyParser{}).Parse("syntax include @Group\nlet g:after = 1\n")
	if !hasDiagnostic(end, "vim/E397") || len(end.Commands) != 2 || end.Commands[1].Canonical != "let" || countTokens(end, TokenSeparator) != 0 {
		t.Fatalf("end commands=%#v diagnostics=%#v tokens=%#v", end.Commands, end.Diagnostics, end.Tokens)
	}
	if node := end.Commands[0].Syntax; node == nil || node.Kind != SyntaxInclude || end.Text(node.Group) != "Group" {
		t.Fatalf("end syntax=%#v", node)
	}

	empty := (LegacyParser{}).Parse("syntax include @Group | echo next\n")
	if len(empty.Diagnostics) != 0 || len(empty.Commands) != 2 || empty.Commands[1].Canonical != "echo" || len(empty.Commands[0].Syntax.Keywords) != 0 {
		t.Fatalf("empty commands=%#v diagnostics=%#v", empty.Commands, empty.Diagnostics)
	}

	malformed := (LegacyParser{}).Parse("syntax include @Group `=fname | echo hidden\nlet g:after = 1\n")
	if len(malformed.Diagnostics) != 0 || len(malformed.Commands) != 2 || malformed.Commands[1].Canonical != "let" || countTokens(malformed, TokenSeparator) != 0 {
		t.Fatalf("malformed commands=%#v diagnostics=%#v tokens=%#v", malformed.Commands, malformed.Diagnostics, malformed.Tokens)
	}
	if got := malformed.Text(malformed.Commands[0].Syntax.Keywords[0]); got != "`=fname | echo hidden" {
		t.Fatalf("malformed filename=%q", got)
	}

	vim9 := (Vim9Parser{}).Parse("vim9script\nsyntax include @Group `=fname | echo hidden\nvar after = 1\n")
	if len(vim9.Diagnostics) != 0 || len(vim9.Commands) != 3 || vim9.Commands[2].Canonical != "var" || countTokens(vim9, TokenSeparator) != 0 {
		t.Fatalf("vim9 malformed commands=%#v diagnostics=%#v tokens=%#v", vim9.Commands, vim9.Diagnostics, vim9.Tokens)
	}
}

func TestSyntaxIncludeLogicalContinuationSpans(t *testing.T) {
	source := "syntax include @Group runtime/one\n  \\ runtime/two | echo next\nlet g:after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 3 || file.Commands[1].Canonical != "echo" || file.Commands[2].Canonical != "let" {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	node := file.Commands[0].Syntax
	if node == nil || len(node.Keywords) != 1 || file.Text(node.Keywords[0]) != "runtime/one\n  \\ runtime/two " {
		t.Fatalf("include=%#v filename=%q", node, file.Text(node.Keywords[0]))
	}
	assertFileSpans(t, file)
}

func TestSyntaxClearAndListGroupOperands(t *testing.T) {
	clear := (LegacyParser{}).Parse("syntax clear Foo @Cluster Bar\"tail | echo next\n")
	if len(clear.Diagnostics) != 0 || len(clear.Commands) != 2 || clear.Commands[1].Canonical != "echo" {
		t.Fatalf("clear commands=%#v diagnostics=%#v", clear.Commands, clear.Diagnostics)
	}
	node := clear.Commands[0].Syntax
	if node == nil || node.Kind != SyntaxClear || len(node.Keywords) != 3 {
		t.Fatalf("clear syntax=%#v", node)
	}
	for index, want := range []string{"Foo", "@Cluster", `Bar"tail`} {
		if got := clear.Text(node.Keywords[index]); got != want {
			t.Fatalf("clear operand %d=%q want %q", index, got, want)
		}
	}

	list := (Vim9Parser{}).Parse("vim9script\nsyntax list Foo @Cluster Bar#tail | echo next\n")
	if len(list.Diagnostics) != 0 || len(list.Commands) != 3 || list.Commands[2].Canonical != "echo" {
		t.Fatalf("list commands=%#v diagnostics=%#v", list.Commands, list.Diagnostics)
	}
	node = list.Commands[1].Syntax
	if node == nil || node.Kind != SyntaxList || len(node.Keywords) != 3 {
		t.Fatalf("list syntax=%#v", node)
	}
	for index, want := range []string{"Foo", "@Cluster", "Bar#tail"} {
		if got := list.Text(node.Keywords[index]); got != want {
			t.Fatalf("list operand %d=%q want %q", index, got, want)
		}
	}
	assertFileSpans(t, clear)
	assertFileSpans(t, list)
}

func TestSyntaxImplicitListQueryAndNonAlphabeticOperand(t *testing.T) {
	for _, test := range []struct {
		name   string
		parse  func(string) *File
		source string
		want   []string
	}{
		{name: "bare legacy", parse: func(s string) *File { return (LegacyParser{}).Parse(s) }, source: "syntax\n", want: nil},
		{name: "cluster", parse: func(s string) *File { return (LegacyParser{}).Parse(s) }, source: "syntax @Cluster\n", want: []string{"@Cluster"}},
		{name: "numeric", parse: func(s string) *File { return (LegacyParser{}).Parse(s) }, source: "syntax 123\n", want: []string{"123"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			if len(file.Diagnostics) != 0 || len(file.Commands) != 1 {
				t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
			}
			node := file.Commands[0].Syntax
			if node == nil || node.Kind != SyntaxList || node.Subcommand != (Span{}) || len(node.Keywords) != len(test.want) {
				t.Fatalf("syntax=%#v want=%#v", node, test.want)
			}
			for index, want := range test.want {
				if got := file.Text(node.Keywords[index]); got != want {
					t.Fatalf("operand %d=%q want %q", index, got, want)
				}
			}
			assertFileSpans(t, file)
		})
	}
	vim9 := (Vim9Parser{}).Parse("vim9script\nsyntax\n")
	if len(vim9.Diagnostics) != 0 || len(vim9.Commands) != 2 || vim9.Commands[1].Syntax == nil || vim9.Commands[1].Syntax.Kind != SyntaxList || vim9.Commands[1].Syntax.Subcommand != (Span{}) || len(vim9.Commands[1].Syntax.Keywords) != 0 {
		t.Fatalf("bare vim9 commands=%#v diagnostics=%#v", vim9.Commands, vim9.Diagnostics)
	}
}

func TestSyntaxListCommentAndSeparatorBoundaries(t *testing.T) {
	legacyComment := (LegacyParser{}).Parse("syntax list Foo \" comment\nlet g:after = 1\n")
	if len(legacyComment.Diagnostics) != 0 || len(legacyComment.Commands) != 2 || legacyComment.Commands[1].Canonical != "let" || countTokens(legacyComment, TokenComment) != 1 {
		t.Fatalf("legacy comment commands=%#v diagnostics=%#v tokens=%#v", legacyComment.Commands, legacyComment.Diagnostics, legacyComment.Tokens)
	}
	vim9Comment := (Vim9Parser{}).Parse("vim9script\nsyntax list Foo # comment\nvar after = 1\n")
	if len(vim9Comment.Diagnostics) != 0 || len(vim9Comment.Commands) != 3 || vim9Comment.Commands[2].Canonical != "var" || countTokens(vim9Comment, TokenComment) != 1 {
		t.Fatalf("vim9 comment commands=%#v diagnostics=%#v tokens=%#v", vim9Comment.Commands, vim9Comment.Diagnostics, vim9Comment.Tokens)
	}

	separator := (LegacyParser{}).Parse("syntax list Foo|echo next\n")
	if len(separator.Diagnostics) != 0 || len(separator.Commands) != 2 || separator.Commands[1].Canonical != "echo" || len(separator.Commands[0].Syntax.Keywords) != 1 || separator.Text(separator.Commands[0].Syntax.Keywords[0]) != "Foo" {
		t.Fatalf("separator commands=%#v diagnostics=%#v", separator.Commands, separator.Diagnostics)
	}
}

func TestSyntaxListUnknownGroupsAndFutureSubcommand(t *testing.T) {
	for _, source := range []string{
		"syntax clear DoesNotExist @MissingCluster\n",
		"syntax list DoesNotExist @MissingCluster\n",
	} {
		file := (LegacyParser{}).Parse(source)
		if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Syntax == nil || len(file.Commands[0].Syntax.Keywords) != 2 {
			t.Fatalf("source=%q commands=%#v diagnostics=%#v", source, file.Commands, file.Diagnostics)
		}
	}
	future := (LegacyParser{}).Parse("syntax future Group | echo next\n")
	if len(future.Diagnostics) != 0 || len(future.Commands) != 1 || future.Commands[0].Syntax != nil {
		t.Fatalf("future commands=%#v diagnostics=%#v", future.Commands, future.Diagnostics)
	}
}

func TestSyntaxListLogicalContinuationSpans(t *testing.T) {
	source := "syntax list Foo\n  \\ @Cluster Bar\"tail\nlet g:after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	node := file.Commands[0].Syntax
	if node == nil || node.Kind != SyntaxList || len(node.Keywords) != 3 {
		t.Fatalf("syntax=%#v", node)
	}
	want := []string{"Foo", "@Cluster", `Bar"tail`}
	for index := range want {
		if got := file.Text(node.Keywords[index]); got != want[index] {
			t.Fatalf("operand %d=%q want %q", index, got, want[index])
		}
	}
	assertFileSpans(t, file)
}

func TestSyntaxSyncSettingsAndQuery(t *testing.T) {
	query := (LegacyParser{}).Parse("syntax sync\n")
	if len(query.Diagnostics) != 0 || len(query.Commands) != 1 || query.Commands[0].Syntax == nil || query.Commands[0].Syntax.Kind != SyntaxSync {
		t.Fatalf("query commands=%#v diagnostics=%#v", query.Commands, query.Diagnostics)
	}

	source := "syntax sync ccomment Comment fromstart lines=3 minlines=5tail MAXLINES=9 linebreaks=2 | echo next\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Canonical != "echo" {
		t.Fatalf("settings commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	node := file.Commands[0].Syntax
	if node == nil || node.Kind != SyntaxSync || len(node.Options) != 6 {
		t.Fatalf("settings syntax=%#v", node)
	}
	wantNames := []string{"ccomment", "fromstart", "lines", "minlines", "MAXLINES", "linebreaks"}
	wantValues := []string{"Comment", "", "3", "5tail", "9", "2"}
	for index, option := range node.Options {
		if got := file.Text(option.Name); got != wantNames[index] || file.Text(option.Value) != wantValues[index] {
			t.Fatalf("option %d name=%q value=%q", index, got, file.Text(option.Value))
		}
		if index >= 2 && (option.Equal.Start == option.Equal.End || file.Text(option.Equal) != "=") {
			t.Fatalf("option %d equal=%#v", index, option)
		}
	}
	assertFileSpans(t, file)
}

func TestSyntaxSyncInvalidSettingsRecover(t *testing.T) {
	for _, source := range []string{
		"syntax sync lines= | echo hidden\nlet g:after = 1\n",
		"syntax sync lines =5 | echo hidden\nlet g:after = 1\n",
		"syntax sync future | echo hidden\nlet g:after = 1\n",
	} {
		file := (LegacyParser{}).Parse(source)
		if !hasDiagnostic(file, "vim/E404") || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" || countTokens(file, TokenSeparator) != 0 {
			t.Fatalf("source=%q commands=%#v diagnostics=%#v tokens=%#v", source, file.Commands, file.Diagnostics, file.Tokens)
		}
	}

	for _, source := range []string{
		"syntax sync fromstart|echo hidden\nlet g:after = 1\n",
		"syntax sync clear|echo hidden\nlet g:after = 1\n",
	} {
		file := (LegacyParser{}).Parse(source)
		if !hasDiagnostic(file, "vim/E404") || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" || countTokens(file, TokenSeparator) != 0 {
			t.Fatalf("adjacent bar source=%q commands=%#v diagnostics=%#v tokens=%#v", source, file.Commands, file.Diagnostics, file.Tokens)
		}
	}

	numeric := (LegacyParser{}).Parse("syntax sync lines=3|echo\nlet g:after = 1\n")
	if len(numeric.Diagnostics) != 0 || len(numeric.Commands) != 2 || numeric.Commands[1].Canonical != "let" || countTokens(numeric, TokenSeparator) != 0 || numeric.Text(numeric.Commands[0].Syntax.Options[0].Value) != "3|echo" {
		t.Fatalf("numeric adjacent bar commands=%#v diagnostics=%#v", numeric.Commands, numeric.Diagnostics)
	}

	ccomment := (LegacyParser{}).Parse("syntax sync ccomment Group|echo\nlet g:after = 1\n")
	if len(ccomment.Diagnostics) != 0 || len(ccomment.Commands) != 2 || ccomment.Commands[1].Canonical != "let" || countTokens(ccomment, TokenSeparator) != 0 || ccomment.Text(ccomment.Commands[0].Syntax.Options[0].Value) != "Group|echo" {
		t.Fatalf("ccomment adjacent bar commands=%#v diagnostics=%#v", ccomment.Commands, ccomment.Diagnostics)
	}
}

func TestSyntaxSyncLineContPatternsAndRecovery(t *testing.T) {
	legacy := (LegacyParser{}).Parse("syntax sync linecont /foo|bar/ | echo next\n")
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 2 || legacy.Commands[1].Canonical != "echo" {
		t.Fatalf("legacy linecont commands=%#v diagnostics=%#v", legacy.Commands, legacy.Diagnostics)
	}
	pattern := legacy.Commands[0].Syntax.Patterns[0]
	if legacy.Commands[0].Syntax.Kind != SyntaxSync || pattern.Kind != SyntaxLineContPattern || legacy.Text(pattern.Key) != "linecont" || legacy.Text(pattern.Pattern) != "foo|bar" {
		t.Fatalf("legacy pattern=%#v kind=%d key=%q value=%q", pattern, legacy.Commands[0].Syntax.Kind, legacy.Text(pattern.Key), legacy.Text(pattern.Pattern))
	}

	quoted := (LegacyParser{}).Parse("syntax sync linecont \"foo|bar\" | echo next\n")
	if len(quoted.Diagnostics) != 0 || len(quoted.Commands) != 2 || quoted.Commands[1].Canonical != "echo" || quoted.Text(quoted.Commands[0].Syntax.Patterns[0].Pattern) != "foo|bar" {
		t.Fatalf("quoted commands=%#v diagnostics=%#v", quoted.Commands, quoted.Diagnostics)
	}
	vim9 := (Vim9Parser{}).Parse("vim9script\nsyntax sync linecont #foo|bar# | echo next\n")
	if len(vim9.Diagnostics) != 0 || len(vim9.Commands) != 3 || vim9.Commands[2].Canonical != "echo" || vim9.Text(vim9.Commands[1].Syntax.Patterns[0].Pattern) != "foo|bar" {
		t.Fatalf("vim9 commands=%#v diagnostics=%#v", vim9.Commands, vim9.Diagnostics)
	}
	collection := (LegacyParser{}).Parse("syntax sync linecont /[/]/ | echo next\n")
	if len(collection.Diagnostics) != 0 || len(collection.Commands) != 2 || collection.Commands[1].Canonical != "echo" || collection.Text(collection.Commands[0].Syntax.Patterns[0].Pattern) != "[/]" {
		t.Fatalf("collection commands=%#v diagnostics=%#v", collection.Commands, collection.Diagnostics)
	}

	for _, source := range []string{
		"syntax sync linecont | echo hidden\nlet g:after = 1\n",
		"syntax sync linecont /unterminated | echo hidden\nlet g:after = 1\n",
	} {
		file := (LegacyParser{}).Parse(source)
		if !hasDiagnostic(file, "vim/E404") || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" || countTokens(file, TokenSeparator) != 0 {
			t.Fatalf("source=%q commands=%#v diagnostics=%#v", source, file.Commands, file.Diagnostics)
		}
	}
	// E403 depends on the buffer's existing compiled line-continuation
	// pattern, including whether the first regexp compiled successfully.  The
	// syntax parser has no runtime state, so it conservatively retains both
	// patterns and leaves that diagnostic to stateful analysis.
	repeated := (LegacyParser{}).Parse("syntax sync linecont /one/ linecont /two/ | echo next\n")
	if len(repeated.Diagnostics) != 0 || len(repeated.Commands) != 2 || repeated.Commands[1].Canonical != "echo" || len(repeated.Commands[0].Syntax.Patterns) != 2 {
		t.Fatalf("repeated commands=%#v diagnostics=%#v", repeated.Commands, repeated.Diagnostics)
	}
	repeatedMissing := (LegacyParser{}).Parse("syntax sync linecont /one/ linecont\nlet g:after = 1\n")
	if !hasDiagnostic(repeatedMissing, "vim/E404") || hasDiagnostic(repeatedMissing, "vim/E403") || len(repeatedMissing.Commands) != 2 || repeatedMissing.Commands[1].Canonical != "let" || len(repeatedMissing.Commands[0].Syntax.Patterns) != 1 {
		t.Fatalf("repeated missing commands=%#v diagnostics=%#v", repeatedMissing.Commands, repeatedMissing.Diagnostics)
	}
}

func TestSyntaxSyncClearAndMatch(t *testing.T) {
	clear := (LegacyParser{}).Parse("syntax sync clear Foo @Cluster | echo next\n")
	if len(clear.Diagnostics) != 0 || len(clear.Commands) != 2 || clear.Commands[1].Canonical != "echo" {
		t.Fatalf("clear commands=%#v diagnostics=%#v", clear.Commands, clear.Diagnostics)
	}
	node := clear.Commands[0].Syntax
	if node == nil || node.Kind != SyntaxSync || len(node.Options) != 1 || clear.Text(node.Options[0].Name) != "clear" || len(node.Keywords) != 2 || clear.Text(node.Keywords[1]) != "@Cluster" {
		t.Fatalf("clear syntax=%#v", node)
	}

	source := "syntax sync match Group grouphere Start groupthere NONE /foo/ms=s+1,me=e-1 contained grouphere After groupthere NONE | echo next\n"
	match := (LegacyParser{}).Parse(source)
	if len(match.Diagnostics) != 0 || len(match.Commands) != 2 || match.Commands[1].Canonical != "echo" {
		t.Fatalf("match commands=%#v diagnostics=%#v", match.Commands, match.Diagnostics)
	}
	node = match.Commands[0].Syntax
	if node == nil || node.Kind != SyntaxSyncMatch || len(node.Options) != 5 || len(node.Patterns) != 1 || match.Text(node.Patterns[0].Pattern) != "foo" {
		t.Fatalf("match syntax=%#v", node)
	}
	for _, want := range []struct {
		index int
		name  string
	}{{0, "grouphere"}, {1, "groupthere"}, {2, "contained"}, {3, "grouphere"}, {4, "groupthere"}} {
		if got := match.Text(node.Options[want.index].Name); got != want.name {
			t.Fatalf("sync option %d=%q want %q", want.index, got, want.name)
		}
	}
	if match.Text(node.Options[0].Value) != "Start" || match.Text(node.Options[1].Value) != "NONE" || match.Text(node.Options[3].Value) != "After" || match.Text(node.Options[4].Value) != "NONE" {
		t.Fatalf("sync targets=%#v", node.Options)
	}
	quotedTarget := (LegacyParser{}).Parse("syntax sync match Group grouphere \"target\" /x/ | echo next\n")
	if len(quotedTarget.Diagnostics) != 0 || len(quotedTarget.Commands) != 2 || quotedTarget.Commands[1].Canonical != "echo" || quotedTarget.Commands[0].Syntax == nil || quotedTarget.Text(quotedTarget.Commands[0].Syntax.Options[0].Value) != `"target"` {
		t.Fatalf("quoted target commands=%#v diagnostics=%#v", quotedTarget.Commands, quotedTarget.Diagnostics)
	}
	assertFileSpans(t, clear)
	assertFileSpans(t, match)
}

func TestSyntaxSyncRegionAndLogicalContinuation(t *testing.T) {
	region := (LegacyParser{}).Parse("syntax sync region Group grouphere Start start=/foo/ end=/bar/ | echo hidden\nlet g:after = 1\n")
	if !hasDiagnostic(region, "vim/E393") || len(region.Commands) != 2 || region.Commands[1].Canonical != "let" || countTokens(region, TokenSeparator) != 0 {
		t.Fatalf("region commands=%#v diagnostics=%#v", region.Commands, region.Diagnostics)
	}
	if region.Commands[0].Syntax == nil || region.Commands[0].Syntax.Kind != SyntaxSyncRegion {
		t.Fatalf("region syntax=%#v", region.Commands[0].Syntax)
	}

	source := "syntax sync lines=3\n  \\ minlines=1 linecont /x/\n  \\ match Group /foo/\nlet g:after = 1\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" {
		t.Fatalf("continuation commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	node := file.Commands[0].Syntax
	if node == nil || node.Kind != SyntaxSyncMatch || len(node.Options) != 2 || len(node.Patterns) != 2 || file.Text(node.Patterns[1].Pattern) != "foo" {
		t.Fatalf("continuation syntax=%#v", node)
	}
	if file.Text(node.Options[0].Name) != "lines" || file.Text(node.Options[1].Name) != "minlines" || file.Text(node.Patterns[1].Pattern) != "foo" {
		t.Fatalf("continuation spans=%#v", node)
	}
	assertFileSpans(t, file)
}

func TestSyntaxIsKeywordOpaqueArgument(t *testing.T) {
	for _, test := range []struct {
		name   string
		parse  func(string) *File
		source string
		want   string
	}{
		{name: "query", parse: func(s string) *File { return (LegacyParser{}).Parse(s) }, source: "syntax iskeyword\n", want: ""},
		{name: "legacy", parse: func(s string) *File { return (LegacyParser{}).Parse(s) }, source: "syntax iskeyword clearFoo | echo hidden\n", want: "clearFoo | echo hidden"},
		{name: "vim9", parse: func(s string) *File { return (Vim9Parser{}).Parse(s) }, source: "vim9script\nsyntax iskeyword #foo | echo hidden\n", want: "#foo | echo hidden"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			if len(file.Diagnostics) != 0 || (test.name == "vim9" && len(file.Commands) != 2) || (test.name != "vim9" && len(file.Commands) != 1) {
				t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
			}
			index := 0
			if test.name == "vim9" {
				index = 1
			}
			node := file.Commands[index].Syntax
			if node == nil || node.Kind != SyntaxIsKeyword || len(node.Keywords) != boolToInt(test.want != "") {
				t.Fatalf("syntax=%#v", node)
			}
			if test.want != "" && file.Text(node.Keywords[0]) != test.want {
				t.Fatalf("argument=%q want %q", file.Text(node.Keywords[0]), test.want)
			}
			assertFileSpans(t, file)
		})
	}
	for _, source := range []string{"syntax iskeyword123\n", "syntax iskeyword clear\n"} {
		file := (LegacyParser{}).Parse(source)
		if len(file.Diagnostics) != 0 || file.Commands[0].Syntax == nil || file.Commands[0].Syntax.Kind != SyntaxIsKeyword {
			t.Fatalf("source=%q commands=%#v diagnostics=%#v", source, file.Commands, file.Diagnostics)
		}
	}
	utf8 := (LegacyParser{}).Parse("syntax iskeyword α β\r\n")
	if len(utf8.Diagnostics) != 0 || len(utf8.Commands) != 1 || utf8.Commands[0].Syntax == nil || utf8.Text(utf8.Commands[0].Syntax.Keywords[0]) != "α β" {
		t.Fatalf("utf8 commands=%#v diagnostics=%#v", utf8.Commands, utf8.Diagnostics)
	}
	assertFileSpans(t, utf8)
	trailing := (LegacyParser{}).Parse("syntax iskeyword Foo  \n")
	if len(trailing.Diagnostics) != 0 || trailing.Text(trailing.Commands[0].Syntax.Keywords[0]) != "Foo  " {
		t.Fatalf("trailing commands=%#v diagnostics=%#v", trailing.Commands, trailing.Diagnostics)
	}
	continued := (LegacyParser{}).Parse("syntax iskeyword Foo\n  \\ Bar Baz\n")
	if len(continued.Diagnostics) != 0 || len(continued.Commands) != 1 || continued.Commands[0].Syntax == nil || continued.Text(continued.Commands[0].Syntax.Keywords[0]) != "Foo\n  \\ Bar Baz" {
		t.Fatalf("continued commands=%#v diagnostics=%#v", continued.Commands, continued.Diagnostics)
	}
	assertFileSpans(t, continued)
}

func TestSyntaxFoldlevelModesAndRecovery(t *testing.T) {
	for _, test := range []struct {
		name   string
		parse  func(string) *File
		source string
		index  int
	}{
		{name: "legacy", parse: func(s string) *File { return (LegacyParser{}).Parse(s) }, source: "syntax foldlevel START\n", index: 0},
		{name: "vim9", parse: func(s string) *File { return (Vim9Parser{}).Parse(s) }, source: "vim9script\nsyntax foldlevel minimum\n", index: 1},
		{name: "query", parse: func(s string) *File { return (LegacyParser{}).Parse(s) }, source: "syntax foldlevel\n", index: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			if len(file.Diagnostics) != 0 || file.Commands[test.index].Syntax == nil || file.Commands[test.index].Syntax.Kind != SyntaxFoldlevel {
				t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
			}
			node := file.Commands[test.index].Syntax
			if test.name == "query" && len(node.Keywords) != 0 || test.name != "query" && len(node.Keywords) != 1 {
				t.Fatalf("syntax=%#v", node)
			}
			assertFileSpans(t, file)
		})
	}
	for _, source := range []string{
		"syntax foldlevel other | echo hidden\nlet g:after = 1\n",
		"syntax foldlevel start | echo hidden\nlet g:after = 1\n",
		"syntax foldlevel start \" comment\nlet g:after = 1\n",
	} {
		file := (LegacyParser{}).Parse(source)
		if !hasDiagnostic(file, "vim/E390") || len(file.Commands) != 2 || file.Commands[1].Canonical != "let" || countTokens(file, TokenSeparator) != 0 {
			t.Fatalf("source=%q commands=%#v diagnostics=%#v tokens=%#v", source, file.Commands, file.Diagnostics, file.Tokens)
		}
	}
	vim9 := (Vim9Parser{}).Parse("vim9script\nsyntax foldlevel minimum # trailing\nvar after = 1\n")
	if !hasDiagnostic(vim9, "vim/E390") || len(vim9.Commands) != 3 || vim9.Commands[2].Canonical != "var" || countTokens(vim9, TokenSeparator) != 0 {
		t.Fatalf("vim9 commands=%#v diagnostics=%#v tokens=%#v", vim9.Commands, vim9.Diagnostics, vim9.Tokens)
	}
	spaces := (LegacyParser{}).Parse("syntax foldlevel start  \r\n")
	if len(spaces.Diagnostics) != 0 || len(spaces.Commands) != 1 || spaces.Commands[0].Syntax == nil || spaces.Text(spaces.Commands[0].Syntax.Keywords[0]) != "start" {
		t.Fatalf("spaces commands=%#v diagnostics=%#v", spaces.Commands, spaces.Diagnostics)
	}
}

func TestSyntaxRuntimeModesBoundaries(t *testing.T) {
	for _, test := range []struct {
		name string
		kind SyntaxKind
	}{
		{name: "enable", kind: SyntaxEnable},
		{name: "manual", kind: SyntaxManual},
		{name: "on", kind: SyntaxOn},
		{name: "off", kind: SyntaxOff},
		{name: "reset", kind: SyntaxReset},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := (LegacyParser{}).Parse("syntax " + test.name + " | echo next\n")
			if len(file.Diagnostics) != 0 || len(file.Commands) != 2 || file.Commands[1].Canonical != "echo" || file.Commands[0].Syntax == nil || file.Commands[0].Syntax.Kind != test.kind {
				t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
			}
			assertFileSpans(t, file)
		})
	}

	legacy := (LegacyParser{}).Parse("syntax manual trailing | echo hidden\nsyntax on \" ignored\nlet g:after = 1\n")
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 3 || legacy.Commands[2].Canonical != "let" || countTokens(legacy, TokenSeparator) != 0 || countTokens(legacy, TokenComment) != 0 {
		t.Fatalf("legacy commands=%#v diagnostics=%#v tokens=%#v", legacy.Commands, legacy.Diagnostics, legacy.Tokens)
	}

	vim9 := (Vim9Parser{}).Parse("vim9script\nsyntax off # spaced\nsyntax manual#adjacent\nsyntax reset #{also-comment\nsyntax enable trailing # ignored\nvar after = 1\n")
	if len(vim9.Diagnostics) != 0 || len(vim9.Commands) != 6 || vim9.Commands[5].Canonical != "var" || countTokens(vim9, TokenComment) != 3 {
		t.Fatalf("vim9 commands=%#v diagnostics=%#v tokens=%#v", vim9.Commands, vim9.Diagnostics, vim9.Tokens)
	}
	for index, kind := range []SyntaxKind{SyntaxOff, SyntaxManual, SyntaxReset, SyntaxEnable} {
		if node := vim9.Commands[index+1].Syntax; node == nil || node.Kind != kind {
			t.Fatalf("vim9 command %d syntax=%#v", index+1, node)
		}
	}
	assertFileSpans(t, legacy)
	assertFileSpans(t, vim9)
}

func TestSyntaxASCIIExSubcommandBoundary(t *testing.T) {
	keyword := (LegacyParser{}).Parse("syntax keyword123\nlet g:after = 1\n")
	if !hasDiagnostic(keyword, "vim/E475") || len(keyword.Commands) != 2 || keyword.Commands[0].Syntax == nil || keyword.Commands[0].Syntax.Kind != SyntaxKeyword || keyword.Text(keyword.Commands[0].Syntax.Group) != "123" || keyword.Commands[1].Canonical != "let" {
		t.Fatalf("keyword commands=%#v diagnostics=%#v", keyword.Commands, keyword.Diagnostics)
	}
	clear := (LegacyParser{}).Parse("syntax clear@Cluster | echo next\n")
	if len(clear.Diagnostics) != 0 || len(clear.Commands) != 2 || clear.Commands[0].Syntax == nil || clear.Commands[0].Syntax.Kind != SyntaxClear || clear.Text(clear.Commands[0].Syntax.Keywords[0]) != "@Cluster" || clear.Commands[1].Canonical != "echo" {
		t.Fatalf("clear commands=%#v diagnostics=%#v", clear.Commands, clear.Diagnostics)
	}
	include := (LegacyParser{}).Parse("syntax include@Group file.vim | echo next\n")
	if len(include.Diagnostics) != 0 || len(include.Commands) != 2 || include.Commands[0].Syntax == nil || include.Commands[0].Syntax.Kind != SyntaxInclude || include.Text(include.Commands[0].Syntax.Group) != "Group" || include.Text(include.Commands[0].Syntax.Keywords[0]) != "file.vim " || include.Commands[1].Canonical != "echo" {
		t.Fatalf("include commands=%#v diagnostics=%#v", include.Commands, include.Diagnostics)
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestSyntaxUnknownSubcommandRemainsOpaque(t *testing.T) {
	file := (LegacyParser{}).Parse("syntax future Group /x/ | echo next\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Syntax != nil || file.Text(file.Commands[0].Argument) != "future Group /x/ | echo next" {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
}

func TestSyntaxLogicalContinuationAndLambdaSpans(t *testing.T) {
	legacySource := "syntax region Group\n  \\ start=/foo|bar/\n  \\ skip=/\\\\./\n  \\ end=/baz/ contains=One,Two\nlet g:after = 1\n"
	legacy := (LegacyParser{}).Parse(legacySource)
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 2 || legacy.Commands[1].Canonical != "let" {
		t.Fatalf("legacy commands=%#v diagnostics=%#v", legacy.Commands, legacy.Diagnostics)
	}
	region := legacy.Commands[0].Syntax
	if region == nil || len(region.Patterns) != 3 || legacy.Text(region.Patterns[0].Pattern) != "foo|bar" || len(region.Options) != 1 || legacy.Text(region.Options[0].Values[1]) != "Two" {
		t.Fatalf("legacy region=%#v", region)
	}
	assertFileSpans(t, legacy)

	lambdaSource := "# 前缀 😀\n() => {\n  syntax match Group /foo|bar/ contains=One,Two\n}"
	expression, diagnostics := (Vim9ExpressionParser{}).Parse(lambdaSource)
	if len(diagnostics) != 0 || expression == nil || expression.LambdaBody == nil || len(expression.LambdaBody.Commands) != 1 {
		t.Fatalf("lambda=%#v diagnostics=%#v", expression, diagnostics)
	}
	body := expression.LambdaBody
	match := body.Commands[0].Syntax
	if match == nil || body.Text(match.Group) != "Group" || body.Text(match.Patterns[0].Pattern) != "foo|bar" || body.Text(match.Options[0].Values[1]) != "Two" {
		t.Fatalf("lambda match=%#v", match)
	}
	assertFileSpansAt(t, body, "syntax lambda")
}

func TestSyntaxVim9HashBoundaryAndLogicalSpans(t *testing.T) {
	source := "vim9script\nsyntax match Group /foo#bar/ contains=One,Two\n  | echo done\nvar after = 1\n"
	file := (Vim9Parser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 || file.Commands[1].Canonical != "syntax" || file.Commands[3].Canonical != "var" {
		t.Fatalf("commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
	node := file.Commands[1].Syntax
	if node == nil || file.Text(node.Patterns[0].Pattern) != "foo#bar" || file.Text(node.Patterns[0].Pattern) == "" {
		t.Fatalf("syntax=%#v", node)
	}
	if strings.Contains(file.Text(file.Commands[1].Argument), "echo") {
		t.Fatalf("leading-bar continuation leaked into argument=%q", file.Text(file.Commands[1].Argument))
	}
	assertFileSpans(t, file)
}
