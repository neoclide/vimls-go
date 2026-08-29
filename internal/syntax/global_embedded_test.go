package syntax

import "testing"

func TestGlobalEmbeddedAliasesAndNestedPayload(t *testing.T) {
	source := "g/foo/ echo one | v/bar/ echo two | s/x/y/\n"
	file := (LegacyParser{}).Parse(source)
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	outer := &file.Commands[0]
	if outer.Canonical != "global" || outer.TypedName != "g" || outer.Embedded == nil {
		t.Fatalf("outer = %#v", outer)
	}
	if len(outer.Embedded.Commands) != 2 {
		t.Fatalf("embedded commands = %#v", outer.Embedded.Commands)
	}
	if outer.Argument != (Span{Start: 1, End: len(source) - 1}) ||
		outer.Embedded.Span != (Span{Start: 7, End: len(source) - 1}) ||
		outer.Embedded.Commands[0].Span != (Span{Start: 7, End: 15}) {
		t.Fatalf("spans: outer=%#v embedded=%#v first=%#v", outer.Argument, outer.Embedded.Span, outer.Embedded.Commands[0].Span)
	}
	want := []string{"echo", "vglobal"}
	for index, name := range want {
		command := &outer.Embedded.Commands[index]
		if command.Canonical != name || command.Dialect != Legacy {
			t.Fatalf("embedded command %d = %#v", index, command)
		}
	}
	if outer.Embedded.Commands[1].Embedded == nil || len(outer.Embedded.Commands[1].Embedded.Commands) != 2 ||
		outer.Embedded.Commands[1].Embedded.Commands[0].Canonical != "echo" {
		t.Fatalf("nested vglobal = %#v", outer.Embedded.Commands[1].Embedded)
	}
	if file.Text(outer.Embedded.Commands[1].Embedded.Commands[0].Argument) != "two" {
		t.Fatalf("nested argument = %q", file.Text(outer.Embedded.Commands[1].Embedded.Commands[0].Argument))
	}
	if outer.Embedded.Commands[1].Embedded.Commands[1].Canonical != "substitute" ||
		outer.Embedded.Commands[1].Embedded.Commands[1].Dialect != Legacy {
		t.Fatalf("nested substitute = %#v", outer.Embedded.Commands[1].Embedded.Commands)
	}
	assertFileSpans(t, file)
}

func TestGlobalEmbeddedRegexpBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		payload string
		want    bool
	}{
		{name: "escaped delimiter", source: "g/foo\\/bar/ echo value\n", payload: "echo value"},
		{name: "collection", source: "g/[a/b]/ echo value\n", payload: "echo value"},
		{name: "posix collection", source: "g/[[:digit:]/]/ echo value\n", payload: "echo value"},
		{name: "collating collection", source: "g/[ [.a.] /]/ echo value\n", payload: "echo value"},
		{name: "equivalence collection", source: "g/[ [=a=] /]/ echo value\n", payload: "echo value"},
		{name: "very magic collection", source: "g/\\v[a/]/ echo value\n", payload: "echo value"},
		{name: "very nomagic collection", source: "g/\\V\\[a/]/ echo value\n", payload: "echo value"},
		{name: "nomagic switch does not change boundary scan", source: "g/\\M[a/b]/ echo value\n", payload: "echo value"},
		{name: "no closing delimiter", source: "g/foo echo value\n", want: false},
		{name: "alphabetic delimiter", source: "global foo/bar echo value\n", want: false},
		{name: "default print", source: "g/foo/   \n", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := (LegacyParser{}).Parse(test.source)
			if len(file.Commands) != 1 {
				t.Fatalf("commands = %#v", file.Commands)
			}
			body := file.Commands[0].Embedded
			if test.want || test.payload != "" {
				if body == nil || len(body.Commands) != 1 || file.Text(body.Commands[0].Span) != test.payload {
					t.Fatalf("embedded = %#v, text = %q", body, bodyText(file, body))
				}
			} else if body != nil {
				t.Fatalf("unexpected embedded = %#v", body)
			}
		})
	}
}

func TestGlobalEmbeddedPreviousPatternAndDialects(t *testing.T) {
	for _, marker := range []string{"g\\/", "g\\?", "g\\&", "v\\/"} {
		source := marker + " echo value\n"
		file := (LegacyParser{}).Parse(source)
		if len(file.Commands) != 1 || file.Commands[0].Embedded == nil || len(file.Commands[0].Embedded.Commands) != 1 {
			t.Fatalf("marker %q: commands = %#v", marker, file.Commands)
		}
		body := file.Commands[0].Embedded
		if file.Text(body.Commands[0].Argument) != "value" || body.Commands[0].Dialect != Legacy {
			t.Fatalf("marker %q: embedded = %#v", marker, body.Commands)
		}
	}
	for _, command := range []string{"function Legacy()", "def Vim9()"} {
		source := "vim9script\ng/foo/ " + command + "\n"
		file := Parse(source)
		if len(file.Commands) != 2 || file.Commands[1].Embedded == nil || len(file.Commands[1].Embedded.Commands) != 1 {
			t.Fatalf("command %q: commands = %#v, diagnostics = %#v", command, file.Commands, file.Diagnostics)
		}
		if file.Commands[1].Embedded.Commands[0].Dialect != Vim9 {
			t.Fatalf("command %q: embedded = %#v", command, file.Commands[1].Embedded.Commands)
		}
	}
}

func TestGlobalEmbeddedMixedDialectBodies(t *testing.T) {
	source := "vim9script\n" +
		"function Old()\n" +
		"  g/foo/ let g:old = 1\n" +
		"endfunction\n" +
		"def New()\n" +
		"  g/foo/ var fresh = 1\n" +
		"enddef\n"
	file := Parse(source)
	var globals []*Command
	for index := range file.Commands {
		if file.Commands[index].Canonical == "global" {
			globals = append(globals, &file.Commands[index])
		}
	}
	if len(file.Diagnostics) != 0 || len(globals) != 2 {
		t.Fatalf("globals = %#v, diagnostics = %#v", globals, file.Diagnostics)
	}
	for index, want := range []Dialect{Legacy, Vim9} {
		outer := globals[index]
		if outer.Dialect != want || outer.Embedded == nil || len(outer.Embedded.Commands) != 1 || outer.Embedded.Commands[0].Dialect != want {
			t.Fatalf("global %d = %#v", index, outer)
		}
	}
	if declaration := globals[0].Embedded.Commands[0].Declaration; declaration == nil || file.Text(declaration.Name) != "g:old" {
		t.Fatalf("legacy declaration = %#v", globals[0].Embedded.Commands[0])
	}
	if declaration := globals[1].Embedded.Commands[0].Declaration; declaration == nil || file.Text(declaration.Name) != "fresh" {
		t.Fatalf("Vim9 declaration = %#v", globals[1].Embedded.Commands[0])
	}
	assertFileSpans(t, file)
}

func TestGlobalEmbeddedCommandLocalDialectModifiers(t *testing.T) {
	tests := []struct {
		name            string
		source          string
		parse           func(string) *File
		index           int
		outerDialect    Dialect
		embeddedDialect Dialect
		declaration     string
	}{
		{
			name:            "vim9cmd in legacy file",
			source:          "vim9cmd global!/foo/ let g:old = 1\n",
			parse:           func(source string) *File { return (LegacyParser{}).Parse(source) },
			outerDialect:    Vim9,
			embeddedDialect: Legacy,
			declaration:     "g:old",
		},
		{
			name:            "legacy in Vim9 file",
			source:          "vim9script\nlegacy g/foo/ var fresh = 1\n",
			parse:           func(source string) *File { return (Vim9Parser{}).Parse(source) },
			index:           1,
			outerDialect:    Legacy,
			embeddedDialect: Vim9,
			declaration:     "fresh",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parse(test.source)
			if len(file.Diagnostics) != 0 || len(file.Commands) != test.index+1 {
				t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
			}
			outer := &file.Commands[test.index]
			if outer.Canonical != "global" || outer.Dialect != test.outerDialect || outer.Embedded == nil || len(outer.Embedded.Commands) != 1 {
				t.Fatalf("outer = %#v", outer)
			}
			nested := &outer.Embedded.Commands[0]
			if nested.Dialect != test.embeddedDialect || nested.Declaration == nil || file.Text(nested.Declaration.Name) != test.declaration {
				t.Fatalf("nested = %#v", nested)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestGlobalEmbeddedNonAlphabeticDelimitersAndEscapeParity(t *testing.T) {
	legacy := (LegacyParser{}).Parse("g|foo| echo one | echo two\ng\"foo\" echo quoted\n")
	if len(legacy.Diagnostics) != 0 || len(legacy.Commands) != 2 {
		t.Fatalf("commands = %#v, diagnostics = %#v", legacy.Commands, legacy.Diagnostics)
	}
	if body := legacy.Commands[0].Embedded; body == nil || len(body.Commands) != 2 || body.Commands[0].Canonical != "echo" || body.Commands[1].Canonical != "echo" {
		t.Fatalf("bar delimiter body = %#v", body)
	}
	if body := legacy.Commands[1].Embedded; body == nil || len(body.Commands) != 1 || legacy.Text(body.Commands[0].Argument) != "quoted" {
		t.Fatalf("quote delimiter body = %#v", body)
	}
	assertFileSpans(t, legacy)

	vim9 := Parse("vim9script\ng#foo# echo value # nested comment\n")
	if len(vim9.Diagnostics) != 0 || len(vim9.Commands) != 2 || vim9.Commands[1].Embedded == nil || len(vim9.Commands[1].Embedded.Commands) != 1 || vim9.Text(vim9.Commands[1].Embedded.Commands[0].Argument) != "value" {
		t.Fatalf("Vim9 hash delimiter = %#v, diagnostics = %#v", vim9.Commands, vim9.Diagnostics)
	}
	assertFileSpans(t, vim9)

	odd := `g/foo\// echo value`
	oddBody, ok := globalCommandBodySpan(odd, 1, len(odd))
	if !ok || odd[oddBody.Start:oddBody.End] != "echo value" {
		t.Fatalf("odd escaped delimiter body = %#v, ok = %t", oddBody, ok)
	}
	even := `g/foo\\// echo value`
	evenBody, ok := globalCommandBodySpan(even, 1, len(even))
	if !ok || even[evenBody.Start:evenBody.End] != "/ echo value" {
		t.Fatalf("even escaped delimiter body = %#v, text = %q, ok = %t", evenBody, even[evenBody.Start:evenBody.End], ok)
	}
}

func TestGlobalEmbeddedVim9PatternAndRecovery(t *testing.T) {
	source := "vim9script\ng/foo#bar/ echo value # comment\ng/foo/ var broken = | echo same-line\nvar after = 1\n"
	file := Parse(source)
	if len(file.Commands) != 4 || file.Commands[1].Embedded == nil || file.Commands[2].Embedded == nil {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	if len(file.Commands[1].Embedded.Commands) != 1 || file.Text(file.Commands[1].Embedded.Commands[0].Argument) != "value" {
		t.Fatalf("vim9 payload = %#v", file.Commands[1].Embedded.Commands)
	}
	if len(file.Commands[2].Embedded.Commands) != 1 || file.Commands[2].Embedded.Commands[0].Canonical != "var" || file.Text(file.Commands[2].Embedded.Commands[0].Argument) != "broken = | echo same-line" {
		t.Fatalf("malformed nested payload = %#v", file.Commands[2].Embedded.Commands)
	}
	if file.Commands[3].Canonical != "var" || file.Commands[3].Declaration == nil || len(file.Diagnostics) == 0 {
		t.Fatalf("recovery commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	assertFileSpans(t, file)
}

func TestGlobalEmbeddedUnknownLeadingBackslashIsOpaque(t *testing.T) {
	file := (LegacyParser{}).Parse("g\\# echo value\n")
	if len(file.Commands) != 1 || file.Commands[0].Embedded != nil {
		t.Fatalf("commands = %#v", file.Commands)
	}
}

func bodyText(file *File, list *CommandList) string {
	if list == nil || len(list.Commands) == 0 {
		return ""
	}
	return file.Text(list.Commands[0].Span)
}
