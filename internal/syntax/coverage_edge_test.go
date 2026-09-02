package syntax

import (
	"strings"
	"testing"
)

func TestLogicalViewBoundaryAndMappingEdges(t *testing.T) {
	negative, next := startLogicalView("one\r\ntwo", -4)
	if negative.Source != (Span{Start: 0, End: 3}) || negative.Next != 5 || next != 5 || negative.identity || len(negative.buffer) != 3 {
		t.Fatalf("negative start view = %#v, next = %d", negative, next)
	}
	past, next := startLogicalView("one", 8)
	if past.Source != (Span{Start: 8, End: 8}) || past.Next != 8 || next != 8 {
		t.Fatalf("past-end view = %#v, next = %d", past, next)
	}

	view, _ := startLogicalView("one\ntwo", 0)
	view.appendSynthetic(' ', 3)
	view.appendSource("one\ntwo", 4, 7)
	view.finishText()
	if view.Text != "one two" {
		t.Fatalf("text = %q", view.Text)
	}
	if got := view.mapSpan(Span{Start: -2, End: 99}); got != (Span{Start: 0, End: 7}) {
		t.Fatalf("clamped span = %#v", got)
	}
	if got := view.mapSpan(Span{Start: 3, End: 3}); got != (Span{Start: 3, End: 3}) {
		t.Fatalf("synthetic boundary = %#v", got)
	}
	if got := view.boundary(-1); got != 0 {
		t.Fatalf("negative boundary = %d", got)
	}
	if got := view.boundary(99); got != 7 {
		t.Fatalf("end boundary = %d", got)
	}
	view.flushNewline()
	view.flushNewline()
	if len(view.Physical) != 1 || view.Physical[0].Kind != TokenNewline {
		t.Fatalf("physical = %#v", view.Physical)
	}
}

func TestVim9PutExpressionBoundaryVariants(t *testing.T) {
	for _, test := range []struct {
		name, source string
		separator    bool
	}{
		{name: "not expression", source: " value"},
		{name: "empty", source: " =  "},
		{name: "bar rhs", source: " = | echo later"},
		{name: "valid separator", source: " = value | echo later", separator: true},
		{name: "invalid expression", source: " = ("},
	} {
		t.Run(test.name, func(t *testing.T) {
			end, separator, _, boundary := scanVim9PutExpression(test.source, 0, len(test.source))
			if end <= 0 || end > len(test.source) {
				t.Fatalf("end = %d", end)
			}
			if (separator != (Span{})) != test.separator {
				t.Fatalf("separator = %#v", separator)
			}
			if test.name == "invalid expression" && boundary == nil {
				t.Fatal("invalid expression did not retain a diagnostic boundary")
			}
		})
	}
}

func TestScannerRecoveryPrimitiveBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, source string
		nameEnd      int
		want         bool
	}{
		{name: "scoped assignment", source: "item.value += 1", nameEnd: len("item"), want: true},
		{name: "scoped concat", source: "item:value ..= 'x'", nameEnd: len("item"), want: true},
		{name: "scoped comparison", source: "item.value == 1", nameEnd: len("item"), want: false},
		{name: "unscoped name", source: "value = 1", nameEnd: 0, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := looksLikeScopedVim9Assignment(test.source, test.nameEnd, len(test.source)); got != test.want {
				t.Fatalf("looksLikeScopedVim9Assignment(%q) = %v, want %v", test.source, got, test.want)
			}
		})
	}

	keymap := &LoadKeymap{}
	file := &File{Source: "from \t"}
	parseLoadKeymapLine(file, keymap, 0, len(file.Source))
	if len(keymap.Entries) != 0 || len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vim/E791" {
		t.Fatalf("empty keymap entry = %#v diagnostics=%#v", keymap.Entries, file.Diagnostics)
	}

	for _, parse := range []func(*File, Span, int) *CommandList{
		parseVim9AutocmdBlockCommandList,
		parseLegacyDoCommandList,
		parseLegacyAutocmdCommandList,
	} {
		file := &File{Source: "echo value"}
		if list := parse(file, Span{End: len(file.Source)}, maxEmbeddedCommandDepth); len(list.Commands) != 0 || len(file.Diagnostics) != 1 || file.Diagnostics[0].Code != "vimls/embedded-command-depth" {
			t.Fatalf("depth recovery list=%#v diagnostics=%#v", list, file.Diagnostics)
		}
	}
}

func TestLambdaRebaseRetainsNestedCommandSpans(t *testing.T) {
	span := func(start, end int) Span { return Span{Start: start, End: end} }
	file := &File{Source: "abcdefghij", Commands: []Command{{
		Modifiers:  []Modifier{{Filter: &FilterModifier{Delimiter: span(0, 1), Pattern: span(1, 2), Flags: span(2, 3)}}},
		TypeAlias:  &TypeAlias{Name: span(0, 1), Assignment: span(1, 2), TypeSpan: span(2, 3), Type: &Type{Kind: TypeNamed, Span: span(2, 3)}},
		EnumValues: []EnumValue{{Name: span(3, 4), Initializer: &Expression{Span: span(4, 5)}, Arguments: []*Expression{{Span: span(5, 6)}}}},
		Import:     &Import{PathSpan: span(6, 7), Alias: span(7, 8), Path: &Expression{Span: span(6, 7)}},
		Heredoc:    &Heredoc{Body: span(0, 1), EndMarker: span(1, 2)},
		Keymap:     &LoadKeymap{Body: span(2, 3), Entries: []KeymapEntry{{From: span(3, 4), To: span(4, 5)}}},
		Substitute: &Substitute{Delimiter: span(0, 1), Pattern: span(1, 2), PatternDelimiter: span(2, 3), Replacement: span(3, 4), ReplacementDelimiter: span(4, 5), Flags: span(5, 6), Count: span(6, 7), PreviousPattern: span(7, 8), ReplacementPrefix: span(8, 9), ExpressionSpan: span(9, 10)},
		Highlight:  &Highlight{Default: span(0, 1), Operation: span(1, 2), Group: span(2, 3), LinkTarget: span(3, 4), Attributes: []HighlightAttribute{{Key: span(4, 5), Equal: span(5, 6), Value: span(6, 7)}}},
	}}}
	rebaseLambdaFile(file, "0123456789abcdefghij", 10)
	command := file.Commands[0]
	if command.TypeAlias.Name != span(10, 11) || command.EnumValues[0].Arguments[0].Span != span(15, 16) || command.Import.Alias != span(17, 18) || command.Modifiers[0].Filter.Pattern != span(11, 12) || command.Substitute.ExpressionSpan != span(19, 20) || command.Highlight.Attributes[0].Value != span(16, 17) || command.Keymap.Entries[0].To != span(14, 15) {
		t.Fatalf("rebased command = %#v", command)
	}
}

func TestExpressionDepthAndSyntaxSyncNumberRecovery(t *testing.T) {
	deep := strings.Repeat("(", 513) + "value" + strings.Repeat(")", 513)
	_, diagnostics := parseExpression(deep, 0, Vim9)
	if len(diagnostics) == 0 || diagnostics[0].Code != "vimls/expression-too-deep" {
		t.Fatalf("deep expression diagnostics = %#v", diagnostics)
	}

	file := (LegacyParser{}).Parse("syntax sync maxlines=20\nsyntax sync minlines=bad\necho after\n")
	if len(file.Commands) != 3 || len(file.Diagnostics) == 0 || file.Diagnostics[0].Code != "vim/E404" || file.Commands[2].Canonical != "echo" {
		t.Fatalf("syntax sync recovery commands=%#v diagnostics=%#v", file.Commands, file.Diagnostics)
	}
}

func TestSyntaxCommandRecoveryMatrix(t *testing.T) {
	for _, source := range []string{
		"syntax case ignore\n",
		"syntax conceal on\n",
		"syntax clear Group\n",
		"syntax keyword Group contained nextgroup=Other skipwhite one two\n",
		"syntax match Group /foo\\/bar/ containedin=ALLBUT,Other\n",
		"syntax region Group start=/a/ skip=/b/ end=/c/ keepend extend\n",
		"syntax cluster Group contains=One,Two add=Three remove=Two\n",
		"syntax include @Cluster syntax/test.vim\n",
		"syntax sync ccomment Group minlines=2 maxlines=10\n",
		"syntax sync linecont /\\$/\n",
		"syntax sync match Group grouphere Other /begin/\n",
		"syntax iskeyword @,48-57,_\n",
		"syntax spell notoplevel\n",
	} {
		file := (LegacyParser{}).Parse(source + "echo after\n")
		if len(file.Commands) < 2 || file.Commands[len(file.Commands)-1].Canonical != "echo" {
			t.Fatalf("syntax recovery lost following command: %#v", file.Commands)
		}
		assertFileSpansAt(t, file, source)
	}
}

func TestVim9TypeParserEdgeForms(t *testing.T) {
	for _, test := range []struct {
		source string
		kind   TypeKind
		code   string
	}{
		{source: "...", kind: TypeVariadic, code: "vim/E1010"},
		{source: "?", kind: TypeOptional, code: "vimls/missing-type"},
		{source: "!", kind: TypeMissing, code: "vimls/missing-type"},
		{source: "number trailing", kind: TypeNamed, code: "vimls/trailing-type"},
	} {
		t.Run(test.source, func(t *testing.T) {
			node, diagnostics := (Vim9TypeParser{}).Parse(test.source)
			if node == nil || node.Kind != test.kind || !hasDiagnostic(&File{Diagnostics: diagnostics}, test.code) {
				t.Fatalf("node = %#v, diagnostics = %#v", node, diagnostics)
			}
		})
	}
	deep := strings.Repeat("?", 130) + "number"
	_, diagnostics := (Vim9TypeParser{}).Parse(deep)
	if !hasDiagnostic(&File{Diagnostics: diagnostics}, "vimls/type-too-deep") {
		t.Fatalf("deep diagnostics = %#v", diagnostics)
	}
}

func TestSyntaxParserMalformedOperands(t *testing.T) {
	for _, test := range []struct {
		source, code string
	}{
		{"syntax keyword\n", "vim/E475"},
		{"syntax cluster Group\n", "vim/E400"},
		{"syntax spell not-a-mode\n", "vim/E390"},
		{"syntax foldlevel neither\n", "vim/E390"},
	} {
		t.Run(test.source, func(t *testing.T) {
			file := (LegacyParser{}).Parse(test.source)
			if !hasDiagnostic(file, test.code) || len(file.Commands) != 1 {
				t.Fatalf("commands = %#v diagnostics = %#v", file.Commands, file.Diagnostics)
			}
			assertFileSpans(t, file)
		})
	}
}

func TestScannerAndFormattingHelperEdges(t *testing.T) {
	if got := autocmdBlockBodyStart("{ | echo 'same line'", 0, len("{ | echo 'same line'"), Vim9); got != 4 {
		t.Fatalf("bar block body = %d", got)
	}
	if got := autocmdBlockBodyStart("{ # comment\necho next", 0, len("{ # comment\necho next"), Vim9); got != len("{ # comment\n") {
		t.Fatalf("comment block body = %d", got)
	}
	if !collectedBlockBarConflict("def Fn() | echo x", &Command{Canonical: "def", Argument: Span{Start: 4, End: 12}}) {
		t.Fatal("function bar conflict was missed")
	}
	if got := skipEnumSpace(" \\\n\tValue", 0, len(" \\\n\tValue")); got != len(" \\\n\t") {
		t.Fatalf("enum space = %d", got)
	}
	planner := newIndentPlanner("if true\n  bad\nendif\n")
	planner.protectFollowingLines(Span{Start: 0, End: len("if true\n  bad")}, 0)
	if !planner.protected[1] || planner.protected[0] {
		t.Fatalf("protected lines = %#v", planner.protected)
	}
}

func TestScannerAndLambdaOverflowHelperBoundaries(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if value, ok := safeLambdaOffset(maxInt, 1); ok || value != maxInt {
		t.Fatalf("positive overflow = %d, %t", value, ok)
	}
	minInt := -maxInt - 1
	if value, ok := safeLambdaOffset(minInt, -1); ok || value != minInt {
		t.Fatalf("negative overflow = %d, %t", value, ok)
	}
	if value, ok := safeLambdaOffset(4, -2); !ok || value != 2 {
		t.Fatalf("ordinary offset = %d, %t", value, ok)
	}
	if got := shiftLambdaSpan(Span{Start: maxInt, End: maxInt}, 1); got != (Span{Start: maxInt, End: maxInt}) {
		t.Fatalf("overflow span = %#v", got)
	}
	for _, test := range []struct {
		source string
		open   bool
	}{
		{"enum Choice\n  One\n", true},
		{"enum Choice\nendenum\n", false},
		{"echo enum\n", false},
	} {
		file := (Vim9Parser{}).Parse(test.source)
		if got := hasOpenEnum(file); got != test.open {
			t.Errorf("hasOpenEnum(%q) = %t", test.source, got)
		}
	}
	for _, test := range []struct {
		source, nameEnd string
		want            bool
	}{{"name = value", "name", true}, {"name += value", "name", true}, {"g:name = value", "g:name", true}, {"object.member ..= value", "object", true}, {"name == value", "name", true}, {"name => value", "name", true}, {"name", "name", false}} {
		if got := looksLikeVim9Expression(test.source, len(test.nameEnd), len(test.source)); got != test.want {
			t.Errorf("assignment %q = %t", test.source, got)
		}
	}
}

func TestIndentBracketHelperBoundaries(t *testing.T) {
	planner := newIndentPlanner("  (one) ")
	full := Span{Start: 0, End: len(planner.source)}
	if open, close := planner.edgePair(full, '(', ')'); open != 2 || close != 6 {
		t.Fatalf("edge pair = %d, %d", open, close)
	}
	if open, close := planner.edgePair(Span{Start: 0, End: 5}, '(', ')'); open != -1 || close != -1 {
		t.Fatalf("incomplete edge pair = %d, %d", open, close)
	}
	trailing := newIndentPlanner("tail [two] ")
	if open, close := trailing.trailingPair(Span{Start: 0, End: len(trailing.source)}, 0, '[', ']'); open != 5 || close != 9 {
		t.Fatalf("trailing pair = %d, %d", open, close)
	}
	if open, close := trailing.trailingPair(Span{Start: 0, End: len(trailing.source)}, len(trailing.source), '[', ']'); open != -1 || close != -1 {
		t.Fatalf("past-end pair = %d, %d", open, close)
	}
	planner.addBracket(2, 6, 1, 0)
	planner.addBracket(2, 6, 1, 0)
	planner.addBracket(5, 9, 2, 1)
	if len(planner.brackets) != 2 {
		t.Fatalf("brackets = %#v", planner.brackets)
	}
}

func TestStaticDictionaryKeyDialectAndLiteralBoundaries(t *testing.T) {
	identifier := &Expression{Kind: ExpressionIdentifier, Value: "name"}
	if key, ok := StaticDictionaryKey(identifier, Vim9); !ok || key != "name" {
		t.Fatalf("vim9 identifier key = %q, %t", key, ok)
	}
	if _, ok := StaticDictionaryKey(identifier, Legacy); ok {
		t.Fatal("legacy identifier became a literal key")
	}
	for _, test := range []struct {
		expression *Expression
		want       string
		ok         bool
	}{
		{&Expression{Kind: ExpressionNumber, Value: "0x10"}, "16", true},
		{&Expression{Kind: ExpressionList, Children: []*Expression{{Kind: ExpressionString, Value: "'literal'"}}}, "", false},
		{&Expression{Kind: ExpressionString, Value: "\"literal\""}, "literal", true},
		{&Expression{Kind: ExpressionString, Value: "'can''t'"}, "", false},
		{&Expression{Kind: ExpressionString, Value: "\"escape\\n\""}, "", false},
		{&Expression{Kind: ExpressionString, Value: "unterminated"}, "", false},
		{&Expression{Kind: ExpressionIdentifier, Value: "runtime"}, "", false},
	} {
		key, ok := StaticDictionaryIndexKey(test.expression)
		if ok != test.ok || key != test.want {
			t.Errorf("index key %#v = %q, %t", test.expression, key, ok)
		}
	}
	if _, ok := StaticDictionaryKey(nil, Vim9); ok {
		t.Fatal("nil dictionary key accepted")
	}
}

func TestLogicalSpanMapperHandlesSharedAndNestedNodesOnce(t *testing.T) {
	source := "one\ntwo"
	view, _ := startLogicalView(source, 0)
	view.appendSynthetic(' ', 3)
	view.appendSource(source, 4, 7)
	view.finishText()
	sharedType := &Type{Span: Span{Start: 0, End: 7}}
	shared := &Expression{Span: Span{Start: 0, End: 7}, CastType: sharedType, ReturnType: sharedType}
	list := &CommandList{Span: Span{Start: 0, End: 7}, Commands: []Command{{Span: Span{Start: 0, End: 7}, Argument: Span{Start: 0, End: 7}, Expressions: []*Expression{shared, shared}}}}
	mapper := logicalSpanMapper{view: &view, source: source, expressions: make(map[*Expression]bool), types: make(map[*Type]bool), files: make(map[*File]bool), lists: make(map[*CommandList]bool)}
	mapper.commandList(list)
	if list.Span != (Span{Start: 0, End: 7}) || list.Commands[0].Argument != (Span{Start: 0, End: 7}) || shared.Span != (Span{Start: 0, End: 7}) || sharedType.Span != (Span{Start: 0, End: 7}) {
		t.Fatalf("mapped nodes = %#v %#v %#v", list, shared, sharedType)
	}
	mapper.commandList(list)
	if len(mapper.lists) != 1 || len(mapper.expressions) != 1 || len(mapper.types) != 1 {
		t.Fatalf("shared nodes remapped: lists=%d expressions=%d types=%d", len(mapper.lists), len(mapper.expressions), len(mapper.types))
	}
}

func TestKnownNonListAndBuiltinTypeNameBoundaries(t *testing.T) {
	for _, test := range []struct {
		name string
		kind TypeKind
		want bool
	}{
		{"number", TypeNamed, true}, {"void", TypeNamed, true}, {"dict", TypeNamed, false}, {"dict", TypeGeneric, true}, {"object", TypeGeneric, true}, {"tuple", TypeGeneric, true}, {"tuple", TypeNamed, false}, {"unknown", TypeNamed, false},
	} {
		if got := isKnownNonListType(&Type{Name: test.name, Kind: test.kind}); got != test.want {
			t.Errorf("isKnownNonListType(%s, %v) = %t", test.name, test.kind, got)
		}
	}
	for _, name := range []string{"any", "blob", "bool", "channel", "dict", "float", "func", "job", "list", "number", "object", "string", "tuple", "void"} {
		if !isBuiltinTypeName(name) {
			t.Errorf("builtin type %q rejected", name)
		}
	}
	for _, name := range []string{"", "integer", "map", "unknown"} {
		if isBuiltinTypeName(name) {
			t.Errorf("unknown type %q accepted", name)
		}
	}
}

func TestScannerRegexpAndHeredocPrimitiveBoundaries(t *testing.T) {
	for _, test := range []struct {
		pattern string
		want    int
	}{
		{pattern: "[[:alpha:]/]x/", want: len("[[:alpha:]/]x")},
		{pattern: "[.ch/.]x/", want: len("[.ch/.]x")},
		{pattern: "[=a/=]x/", want: len("[=a/=]x")},
		{pattern: "\\V\\[/]x/", want: len("\\V\\[/]x")},
		{pattern: "[abc", want: -1},
	} {
		if got := scanGlobalRegexpEnd(test.pattern, 0, len(test.pattern), '/'); got != test.want {
			t.Errorf("regexp %q end = %d, want %d", test.pattern, got, test.want)
		}
	}
	if got := scanGlobalRegexpCollection("^]-x]", 0, len("^]-x]")); got != len("^]-x") {
		t.Fatalf("collection end = %d", got)
	}
	for _, literal := range []struct {
		value          string
		marker         string
		trim, eval, ok bool
	}{
		{value: "'<< END'", marker: "END", ok: true},
		{value: "\"<< trim eval MARK\"", marker: "MARK", trim: true, eval: true, ok: true},
		{value: "'<< trim trim END'", marker: "END", trim: true, ok: true},
		{value: "'<< bad marker'", ok: false},
		{value: "'dynamic\\n'", ok: false},
	} {
		marker, trim, eval, ok := staticHeredocMarker(literal.value)
		if marker != literal.marker || trim != literal.trim || eval != literal.eval || ok != literal.ok {
			t.Errorf("marker %q = %q, %t, %t, %t", literal.value, marker, trim, eval, ok)
		}
	}
	for _, marker := range []string{"", "trim", "eval", "two words", "'END'"} {
		if validHeredocMarker(marker) {
			t.Errorf("invalid marker accepted: %q", marker)
		}
	}
}

func TestExpressionFixedSeedRecoveryStress(t *testing.T) {
	parts := []string{"name", "g:value", "s:value", "1", "0xFF", "0zAB", "'text'", "\"text\"", "$'value {name}'", "[", "]", "{", "}", "(", ")", ",", ":", ".", "->", "=>", "?", "??", "&&", "||", "==#", "=~?", "+", "-", "*", "/", "%", "!", "#", "\n", " "}
	state := uint32(0x4f1bbcdc)
	next := func() string {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		return parts[state%uint32(len(parts))]
	}
	for index := 0; index < 32768; index++ {
		var source strings.Builder
		for count := 0; count < 18; count++ {
			source.WriteString(next())
		}
		input := source.String()
		for _, parser := range []func(string) (*Expression, []Diagnostic){(LegacyExpressionParser{}).Parse, (Vim9ExpressionParser{}).Parse} {
			_, diagnostics := parser(input)
			for _, diagnostic := range diagnostics {
				if diagnostic.Span.Start < 0 || diagnostic.Span.End < diagnostic.Span.Start || diagnostic.Span.End > len(input) {
					t.Fatalf("case %d diagnostic = %#v", index, diagnostic)
				}
			}
		}
	}
}
