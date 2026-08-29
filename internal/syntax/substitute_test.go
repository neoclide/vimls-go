package syntax

import (
	"strings"
	"testing"
)

func TestSubstituteStructuredParts(t *testing.T) {
	tests := []struct {
		name, source, pattern, replacement, flags, count string
		dialect                                          Dialect
	}{
		{name: "legacy slash", source: "s/foo/bar/&gcnerp#liI 12 | echo done\n", pattern: "foo", replacement: "bar", flags: "&gcnerp#liI", count: "12", dialect: Legacy},
		{name: "bang delimiter", source: "%s!foo\\!bar!replacement!ge | echo done\n", pattern: "foo\\!bar", replacement: "replacement", flags: "ge", dialect: Legacy},
		{name: "hash delimiter", source: "s#foo#bar#p\n", pattern: "foo", replacement: "bar", flags: "p", dialect: Legacy},
		{name: "question delimiter", source: "s?foo?bar?i\n", pattern: "foo", replacement: "bar", flags: "i", dialect: Legacy},
		{name: "ampersand delimiter", source: "s&foo&bar&g\n", pattern: "foo", replacement: "bar", flags: "g", dialect: Legacy},
		{name: "underscore delimiter and collection", source: "s_[[:alpha:]_]_bar_g\n", pattern: "[[:alpha:]_]", replacement: "bar", flags: "g", dialect: Legacy},
		{name: "escaped replacement delimiter", source: "s/foo/bar\\//\n", pattern: "foo", replacement: "bar\\/", dialect: Legacy},
		{name: "vim9", source: "vim9script\ns/foo/bar/gi\n", pattern: "foo", replacement: "bar", flags: "gi", dialect: Vim9},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			command := file.Commands[0]
			if test.dialect == Vim9 {
				command = file.Commands[1]
			}
			if command.Substitute == nil {
				t.Fatalf("command = %#v", command)
			}
			substitute := command.Substitute
			if file.Text(substitute.Pattern) != test.pattern || file.Text(substitute.Replacement) != test.replacement || file.Text(substitute.Flags) != test.flags || file.Text(substitute.Count) != test.count {
				t.Fatalf("substitute = %#v, parts = %q/%q/%q/%q", substitute, file.Text(substitute.Pattern), file.Text(substitute.Replacement), file.Text(substitute.Flags), file.Text(substitute.Count))
			}
			if substitute.Delimiter.Start >= substitute.Delimiter.End || substitute.PatternDelimiter.Start >= substitute.PatternDelimiter.End || substitute.ReplacementDelimiter.Start >= substitute.ReplacementDelimiter.End {
				t.Fatalf("delimiter spans = %#v", substitute)
			}
			if file.Text(command.Argument) == "" || !strings.Contains(file.Text(command.Argument), test.pattern) {
				t.Fatalf("argument = %q", file.Text(command.Argument))
			}
			assertFileSpans(t, file)
		})
	}
}

func TestSubstituteMissingPartsOwnTheLine(t *testing.T) {
	for _, test := range []struct {
		name, source                       string
		patternMissing, replacementMissing bool
	}{
		{name: "missing pattern", source: "s/foo | echo same-line\necho next\n", patternMissing: true, replacementMissing: true},
		{name: "empty replacement", source: "s/foo/ | echo same-line\necho next\n", replacementMissing: true},
		{name: "missing replacement", source: "s/foo/bar | echo same-line\necho next\n", replacementMissing: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := (LegacyParser{}).Parse(test.source)
			if len(file.Commands) != 2 || file.Commands[1].Canonical != "echo" {
				t.Fatalf("commands = %#v", file.Commands)
			}
			substitute := file.Commands[0].Substitute
			if substitute == nil || substitute.MissingPattern != test.patternMissing || substitute.MissingReplacement != test.replacementMissing {
				t.Fatalf("substitute = %#v", substitute)
			}
			if file.Text(file.Commands[0].Argument) != strings.TrimPrefix(strings.TrimSuffix(strings.Split(test.source, "\n")[0], "\r"), "s") {
				t.Fatalf("argument = %q", file.Text(file.Commands[0].Argument))
			}
			assertFileSpans(t, file)
		})
	}
}

func TestSubstituteRepeatAndPreviousPatternForms(t *testing.T) {
	legacy := (LegacyParser{}).Parse("sge2 | echo next\ns\\/replacement/ | echo after\ns\\/\\=submatch(0)/\n")
	if len(legacy.Commands) != 5 || legacy.Commands[0].Substitute == nil || legacy.Commands[1].Canonical != "echo" || legacy.Commands[2].Substitute == nil || legacy.Commands[3].Canonical != "echo" || legacy.Commands[4].Substitute == nil {
		t.Fatalf("commands = %#v", legacy.Commands)
	}
	if legacy.Commands[0].Substitute.Flags == (Span{}) || legacy.Commands[0].Substitute.Count == (Span{}) {
		t.Fatalf("repeat = %#v", legacy.Commands[0].Substitute)
	}
	previous := legacy.Commands[2].Substitute
	if !previous.LegacyPrevious || legacy.Text(previous.PreviousPattern) != "\\/" || legacy.Text(previous.Replacement) != "replacement" {
		t.Fatalf("previous = %#v", previous)
	}
	if !legacy.Commands[4].Substitute.ReplacementExpression || legacy.Commands[4].Substitute.Expression == nil || len(legacy.Commands[4].Expressions) != 1 {
		t.Fatalf("previous expression = %#v", legacy.Commands[4])
	}

	vim9 := (Vim9Parser{}).Parse("vim9script\ns\\/replacement/ | echo same-line\nvar next = 1\n")
	if len(vim9.Commands) != 3 || vim9.Commands[1].Substitute == nil || vim9.Commands[2].Declaration == nil {
		t.Fatalf("vim9 commands = %#v", vim9.Commands)
	}
	if !vim9.Commands[1].Substitute.InvalidVim9Backslash || !hasDiagnostic(vim9, "vim/E1270") {
		t.Fatalf("vim9 substitute = %#v, diagnostics = %#v", vim9.Commands[1].Substitute, vim9.Diagnostics)
	}
	assertFileSpans(t, legacy)
	assertFileSpans(t, vim9)
}

func TestSubstituteVim9SeparatorChecksAndInvalidDelimiters(t *testing.T) {
	file := (Vim9Parser{}).Parse("vim9script\ns /foo/bar/\ns-foo-bar\ns.\ns \\/replacement/\ns x/foo/bar/ | echo same-line\nvar after = 1\n")
	if len(file.Commands) != 7 || file.Commands[6].Declaration == nil {
		t.Fatalf("commands = %#v", file.Commands)
	}
	if !hasDiagnostic(file, "vim/E1242") || !hasDiagnostic(file, "vim/E1241") || !hasDiagnostic(file, "vim/E146") {
		t.Fatalf("diagnostics = %#v", file.Diagnostics)
	}
	if hasDiagnostic(file, "vim/E1270") {
		t.Fatalf("whitespace error must take precedence over E1270: %#v", file.Diagnostics)
	}
	if file.Commands[5].Canonical != "substitute" || file.Commands[5].Substitute == nil || file.Commands[5].Substitute.MissingPattern {
		t.Fatalf("invalid letter delimiter = %#v", file.Commands[5])
	}
	if file.Commands[5].Span.End != strings.Index(file.Source, "\nvar after") {
		t.Fatalf("invalid delimiter recovery span = %#v", file.Commands[5].Span)
	}
	assertFileSpans(t, file)

	accepted := (Vim9Parser{}).Parse("vim9script\nsubstitute /foo/bar/\nvar after = 1\n")
	if len(accepted.Diagnostics) != 0 || accepted.Commands[1].Substitute == nil {
		t.Fatalf("full-name substitute = %#v", accepted)
	}
}

func TestSubstituteVim9ExpressionDisambiguation(t *testing.T) {
	file := (Vim9Parser{}).Parse("vim9script\ns:Func()\ns(value)\ns[0]\ns.member\ns ->Func()\nsubstitute('x', 'x', 'y', '')\nsmagic(value)\nsubstitute:foo:bar\ns.\n")
	if len(file.Commands) != 10 {
		t.Fatalf("commands = %#v", file.Commands)
	}
	for index := 1; index <= 7; index++ {
		if file.Commands[index].Kind != CommandExpression || file.Commands[index].Substitute != nil {
			t.Fatalf("command %d is not an expression: %#v", index, file.Commands[index])
		}
	}
	if file.Commands[8].Substitute == nil || file.Text(file.Commands[8].Substitute.Pattern) != "foo" {
		t.Fatalf("full-name colon substitute = %#v", file.Commands[8])
	}
	if file.Commands[9].Substitute == nil || !hasDiagnostic(file, "vim/E1241") {
		t.Fatalf("isolated dot = %#v, diagnostics = %#v", file.Commands[9], file.Diagnostics)
	}
	assertFileSpans(t, file)
}

func TestLegacyOneLetterSubstituteDisambiguation(t *testing.T) {
	file := (LegacyParser{}).Parse("sge2\nsc\nsi\nsr\nsetbufvar(1, '&option', 0)\nsfoo\n")
	if len(file.Commands) != 6 {
		t.Fatalf("commands = %#v", file.Commands)
	}
	for index := 0; index < 4; index++ {
		if file.Commands[index].Canonical != "substitute" || file.Commands[index].Substitute == nil || file.Commands[index].Name.End-file.Commands[index].Name.Start != 1 {
			t.Fatalf("legacy one-letter command %d = %#v", index, file.Commands[index])
		}
	}
	for index := 4; index < 6; index++ {
		if file.Commands[index].Substitute != nil || file.Commands[index].Name.End-file.Commands[index].Name.Start == 1 {
			t.Fatalf("unknown s-name %d became substitute: %#v", index, file.Commands[index])
		}
	}
	assertFileSpans(t, file)

	vim9 := (Vim9Parser{}).Parse("vim9script\nsge2\n")
	if len(vim9.Commands) != 2 || vim9.Commands[1].Substitute != nil || vim9.Commands[1].Name.End-vim9.Commands[1].Name.Start != 3 {
		t.Fatalf("Vim9 sge2 = %#v", vim9.Commands)
	}
}

func TestLegacyOneLetterSubstituteSourceCases(t *testing.T) {
	for source, want := range map[string]bool{
		"sc": true, "sg": true, "si": true, "sI": true, "sr": true,
		"scscope": false, "scriptnames": false, "simalt": false,
		"sign": false, "silent": false, "srewind": false,
		"setbufvar": false, "sfoo": false, "smagic": false,
	} {
		if got := legacyOneLetterSubstitute(source, 0, len(source)); got != want {
			t.Errorf("legacyOneLetterSubstitute(%q) = %t, want %t", source, got, want)
		}
	}
}

func TestSubstituteRepeatCommandsAndTrailingGarbage(t *testing.T) {
	file := (LegacyParser{}).Parse("~&c 2 | echo tilde\ns/foo/bar/garbage | echo same-line\necho next\n")
	if len(file.Commands) != 4 || file.Commands[1].Canonical != "echo" || file.Commands[3].Canonical != "echo" {
		t.Fatalf("commands = %#v", file.Commands)
	}
	for _, index := range []int{0, 2} {
		if file.Commands[index].Substitute == nil {
			t.Fatalf("command %d has no substitute syntax: %#v", index, file.Commands[index])
		}
	}
	if file.Text(file.Commands[0].Substitute.Flags) != "&c" || file.Text(file.Commands[0].Substitute.Count) != "2" {
		t.Fatalf("tilde repeat = %#v", file.Commands[0].Substitute)
	}
	if !hasDiagnostic(file, "vim/E488") || file.Commands[2].Span.End != strings.Index(file.Source, "\necho next") {
		t.Fatalf("trailing recovery = %#v, diagnostics = %#v", file.Commands[2], file.Diagnostics)
	}
	assertFileSpans(t, file)
}

func TestSubstituteContinuationMapsEmptySpans(t *testing.T) {
	legacy := (LegacyParser{}).Parse("s/foo/\n  \\ bar/\necho next\n")
	if len(legacy.Commands) != 2 || legacy.Commands[0].Substitute == nil || legacy.Commands[1].Canonical != "echo" {
		t.Fatalf("legacy commands = %#v", legacy.Commands)
	}
	if legacy.Text(legacy.Commands[0].Substitute.Pattern) != "foo" || !strings.Contains(legacy.Text(legacy.Commands[0].Substitute.Replacement), "bar") {
		t.Fatalf("legacy continuation = %#v", legacy.Commands[0].Substitute)
	}
	assertFileSpans(t, legacy)

	missing := (LegacyParser{}).Parse("s/foo\n  \\ tail\necho next\n")
	if len(missing.Commands) != 2 || missing.Commands[0].Substitute == nil || !missing.Commands[0].Substitute.MissingPattern {
		t.Fatalf("missing continuation = %#v", missing.Commands)
	}
	substitute := missing.Commands[0].Substitute
	if substitute.Replacement.Start != strings.Index(missing.Source, "\necho next") || substitute.Replacement.Start != substitute.Replacement.End || substitute.PatternDelimiter != (Span{}) {
		t.Fatalf("mapped missing spans = %#v", substitute)
	}
	assertFileSpans(t, missing)
}

func TestSubstituteExpressionReplacementParsedOnce(t *testing.T) {
	tests := []struct {
		name, source string
		parse        func(string) *File
		index        int
	}{
		{name: "legacy", source: "s/foo/\\=get(g:, 'value', 0)/ | echo done\n", parse: func(source string) *File { return (LegacyParser{}).Parse(source) }},
		{name: "vim9", source: "vim9script\ns#foo#\\=value .. '! '#g\nvar after = 1\n", parse: func(source string) *File { return (Vim9Parser{}).Parse(source) }, index: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			command := file.Commands[test.index]
			substitute := command.Substitute
			if substitute == nil || !substitute.ReplacementExpression || substitute.Expression == nil || len(command.Expressions) != 1 || command.Expressions[0] != substitute.Expression {
				t.Fatalf("command = %#v", command)
			}
			if file.Text(substitute.ReplacementPrefix) != "\\=" || file.Text(substitute.ExpressionSpan) == "" {
				t.Fatalf("expression parts = %#v", substitute)
			}
			assertFileSpans(t, file)
		})
	}
	missingClose := (Vim9Parser{}).Parse("vim9script\ns/foo/\\=to\nvar after = 1\n")
	command := missingClose.Commands[1]
	if command.Substitute == nil || !command.Substitute.MissingReplacement || command.Substitute.Expression == nil || len(command.Expressions) != 1 {
		t.Fatalf("missing replacement expression = %#v", command)
	}
}

func TestSubstituteMagicOverrideAndTrailingRecovery(t *testing.T) {
	legacy := (LegacyParser{}).Parse("smagic/foo/bar/\nsnomagic/foo/bar/\nsmagic/[x/]/bar/\nsnomagic/\\[x/]/bar/\ns/foo/bar/garbage | echo same-line\necho next\n")
	if legacy.Commands[0].Substitute.Magic != SubstituteMagicOn || legacy.Commands[1].Substitute.Magic != SubstituteMagicOff || len(legacy.Commands) != 6 || legacy.Commands[5].Canonical != "echo" {
		t.Fatalf("legacy commands = %#v", legacy.Commands)
	}
	if legacy.Commands[2].Substitute == nil || legacy.Text(legacy.Commands[2].Substitute.Pattern) != "[x/]" || legacy.Commands[3].Substitute == nil || legacy.Text(legacy.Commands[3].Substitute.Pattern) != "\\[x/]" {
		t.Fatalf("magic collections = %#v (%q, %q; magic=%v,%v)", legacy.Commands[2:4], legacy.Text(legacy.Commands[2].Substitute.Pattern), legacy.Text(legacy.Commands[3].Substitute.Pattern), legacy.Commands[2].Substitute.Magic, legacy.Commands[3].Substitute.Magic)
	}
	if legacy.Commands[4].Substitute == nil || legacy.Text(legacy.Commands[4].Substitute.Flags) != "g" {
		t.Fatalf("trailing garbage = %#v", legacy.Commands[4].Substitute)
	}
	assertFileSpans(t, legacy)
}
