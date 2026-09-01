package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestSemanticHelpersHandleIncompleteAndQualifiedSyntax(t *testing.T) {
	if CollectSymbols(nil) != nil || CollectTypeRelations(nil) != nil || CollectCallRelations(nil) != nil {
		t.Fatal("nil semantic inputs must not produce facts")
	}
	if got := relationFinalName("  imported.Parent  "); got != "Parent" {
		t.Fatalf("qualified relation name = %q", got)
	}

	identifier := &syntax.Expression{Kind: syntax.ExpressionIdentifier, Value: "Fn", Span: syntax.Span{Start: 2, End: 4}}
	generic := &syntax.Expression{Kind: syntax.ExpressionGenericReference, Children: []*syntax.Expression{identifier}}
	call := &syntax.Expression{Kind: syntax.ExpressionCall, Children: []*syntax.Expression{generic}}
	if name, span, identifierTarget := callRelationTarget(call); name != "Fn" || span != identifier.Span || !identifierTarget {
		t.Fatalf("generic identifier call target = %q, %#v, %t", name, span, identifierTarget)
	}
	member := &syntax.Expression{Kind: syntax.ExpressionMember, Value: "Run", Span: syntax.Span{Start: 0, End: 9}, Operator: syntax.Span{Start: 3, End: 4}}
	call.Children[0] = member
	if name, span, identifierTarget := callRelationTarget(call); name != "Run" || span != (syntax.Span{Start: 4, End: 9}) || identifierTarget {
		t.Fatalf("member call target = %q, %#v, %t", name, span, identifierTarget)
	}
	call.Children = nil
	if name, _, _ := callRelationTarget(call); name != "" {
		t.Fatalf("incomplete call target = %q", name)
	}
}

func TestStaticNameDeclarationScopeAndValidityBoundaries(t *testing.T) {
	file := &syntax.File{Source: "g:Global s:Script <SID>Compat Plain bad-name"}
	span := func(text string) syntax.Span {
		start := strings.Index(file.Source, text)
		return syntax.Span{Start: start, End: start + len(text)}
	}
	for _, test := range []struct {
		text    string
		dialect syntax.Dialect
		kind    NameDeclarationKind
		inside  bool
		name    string
		scope   NameDeclarationScope
		ok      bool
	}{
		{"g:Global", syntax.Vim9, NameDeclarationVariable, false, "Global", NameDeclarationGlobal, true},
		{"s:Script", syntax.Vim9, NameDeclarationVariable, false, "Script", NameDeclarationScript, true},
		{"<SID>Compat", syntax.Vim9, NameDeclarationFunction, false, "Compat", NameDeclarationScript, true},
		{"Plain", syntax.Legacy, NameDeclarationFunction, false, "Plain", NameDeclarationGlobal, true},
		{"Plain", syntax.Legacy, NameDeclarationVariable, false, "Plain", NameDeclarationGlobal, true},
		{"Plain", syntax.Legacy, NameDeclarationVariable, true, "", 0, false},
		{"Plain", syntax.Vim9, NameDeclarationFunction, false, "", 0, false},
		{"bad-name", syntax.Vim9, NameDeclarationVariable, false, "", 0, false},
	} {
		event, ok := staticNameDeclaration(file, span(test.text), test.dialect, test.kind, test.inside)
		if ok != test.ok || ok && (event.Name != test.name || event.Scope != test.scope) {
			t.Errorf("%q = %#v, %t", test.text, event, ok)
		}
	}
	if _, ok := staticNameDeclaration(nil, syntax.Span{}, syntax.Legacy, NameDeclarationFunction, false); ok {
		t.Fatal("nil file produced declaration")
	}
	file.Diagnostics = []syntax.Diagnostic{{Span: span("Global")}}
	if _, ok := staticNameDeclaration(file, span("g:Global"), syntax.Vim9, NameDeclarationVariable, false); ok {
		t.Fatal("diagnostic-overlapping declaration accepted")
	}
}

func TestSemanticScopeAndEnumSymbolHelperBoundaries(t *testing.T) {
	parent := &Scope{Kind: syntax.BlockClass}
	child := &Scope{Parent: parent}
	if declarationParent(child) != parent || declarationParent(parent) != parent || declarationParent(nil) != nil {
		t.Fatal("declaration parent selection is unstable")
	}
	for _, test := range []struct {
		scope *Scope
		want  bool
	}{
		{&Scope{}, true},
		{&Scope{Kind: syntax.BlockDef}, true},
		{&Scope{Lambda: &syntax.Expression{}}, true},
		{&Scope{Kind: syntax.BlockClass}, false},
		{&Scope{Kind: syntax.BlockEnum}, false},
	} {
		if got := unusedVariableScope(test.scope); got != test.want {
			t.Errorf("unusedVariableScope(%#v) = %t", test.scope, got)
		}
	}
	file := &syntax.File{Source: "Choice"}
	value := syntax.EnumValue{Name: syntax.Span{Start: 0, End: 6}}
	if symbol := enumMemberSymbol(file, value); symbol == nil || symbol.Name != "Choice" || symbol.Kind != SymbolKindEnumMember {
		t.Fatalf("enum symbol = %#v", symbol)
	}
	value.Name = syntax.Span{Start: 6, End: 6}
	if enumMemberSymbol(file, value) != nil {
		t.Fatal("invalid enum span produced a symbol")
	}
}

func TestDeclarationCreationRedeclarationAndOrderingBoundaries(t *testing.T) {
	file := &syntax.File{Source: "arg local later"}
	def := &Scope{Kind: syntax.BlockDef}
	result := &FileAnalysis{Scopes: []*Scope{def}}
	parameter := addParameterDeclaration(result, def, file, syntax.Span{Start: 0, End: 3})
	local := addDeclaration(result, def, file, syntax.Span{Start: 4, End: 9}, SymbolKindVariable, true)
	late := addDeclaration(result, def, file, syntax.Span{Start: 10, End: 15}, SymbolKindVariable, true)
	if parameter == nil || !parameter.Parameter || local == nil || late == nil {
		t.Fatalf("declarations = %#v", result.Declarations)
	}
	if addDeclaration(nil, def, file, syntax.Span{Start: 0, End: 3}, SymbolKindVariable, true) != nil || addDeclaration(result, nil, file, syntax.Span{Start: 0, End: 3}, SymbolKindVariable, true) != nil || addDeclaration(result, def, file, syntax.Span{Start: 3, End: 3}, SymbolKindVariable, true) != nil {
		t.Fatal("invalid declaration input was accepted")
	}
	// Rename the later declaration to the parameter name while preserving its
	// source ordering; only it is an argument redeclaration.
	late.Name = "arg"
	collectArgumentRedeclarationDiagnostics(result)
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "vim/E1006" {
		t.Fatalf("redeclaration diagnostics = %#v", result.Diagnostics)
	}
	result.Declarations[0], result.Declarations[2] = result.Declarations[2], result.Declarations[0]
	def.Declarations[0], def.Declarations[2] = def.Declarations[2], def.Declarations[0]
	sortDeclarations(result)
	if result.Declarations[0] != parameter || result.Declarations[2] != late || def.Declarations[0] != parameter {
		t.Fatalf("sorted declarations = %#v", result.Declarations)
	}
}

func TestAggregateAndFunctionSymbolKindBoundaries(t *testing.T) {
	for _, test := range []struct {
		block syntax.BlockKind
		want  SymbolKind
	}{
		{syntax.BlockClass, SymbolKindClass},
		{syntax.BlockInterface, SymbolKindInterface},
		{syntax.BlockEnum, SymbolKindEnum},
		{syntax.BlockIf, ""},
	} {
		if got := aggregateSymbolKind(test.block); got != test.want {
			t.Errorf("aggregate kind %v = %q", test.block, got)
		}
	}
	file := &syntax.File{Source: "Class.new Run"}
	constructor := &syntax.Command{Function: &syntax.Function{Name: syntax.Span{Start: 0, End: 9}}}
	method := &syntax.Command{Function: &syntax.Function{Name: syntax.Span{Start: 10, End: 13}}}
	class := &Scope{Kind: syntax.BlockClass}
	iface := &Scope{Kind: syntax.BlockInterface}
	if functionKind(file, constructor, class) != SymbolKindConstructor || functionKind(file, method, class) != SymbolKindMethod || functionKind(file, method, iface) != SymbolKindMethod || functionKind(file, method, nil) != SymbolKindFunction {
		t.Fatal("function symbol kinds are incorrect")
	}
}

func TestCallRelationTargetRecoveryBoundaries(t *testing.T) {
	for _, call := range []*syntax.Expression{
		nil,
		{Kind: syntax.ExpressionCall},
		{Kind: syntax.ExpressionCall, Children: []*syntax.Expression{nil}},
		{Kind: syntax.ExpressionCall, Children: []*syntax.Expression{{Kind: syntax.ExpressionMember, Span: syntax.Span{Start: 0, End: 4}, Operator: syntax.Span{Start: 3, End: 4}}}},
		{Kind: syntax.ExpressionCall, Children: []*syntax.Expression{{Kind: syntax.ExpressionNumber, Value: "1"}}},
	} {
		if name, _, _ := callRelationTarget(call); name != "" {
			t.Errorf("invalid call target = %q", name)
		}
	}
	identifier := &syntax.Expression{Kind: syntax.ExpressionIdentifier, Value: "Target", Span: syntax.Span{Start: 2, End: 8}}
	generic := &syntax.Expression{Kind: syntax.ExpressionGenericReference, Children: []*syntax.Expression{identifier}}
	call := &syntax.Expression{Kind: syntax.ExpressionCall, Children: []*syntax.Expression{generic}}
	if name, span, direct := callRelationTarget(call); name != "Target" || span != identifier.Span || !direct {
		t.Fatalf("generic target = %q %#v %t", name, span, direct)
	}
}

func TestAggregateAndScopeHelperBoundaries(t *testing.T) {
	file := syntax.Parse("vim9script\nclass Parent\n  var value: number\n  static var count: number\n  def new()\n  enddef\nendclass\nclass Child extends Parent\n  def Run()\n  enddef\nendclass\n")
	result := Analyze(file)
	var parent, child, constructor, method *syntax.Command
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Aggregate != nil {
			switch file.Text(command.Aggregate.Name) {
			case "Parent":
				parent = command
			case "Child":
				child = command
			}
		}
		if command.Function != nil {
			switch file.Text(command.Function.Name) {
			case "new":
				constructor = command
			case "Run":
				method = command
			}
		}
	}
	if parent == nil || child == nil || constructor == nil || method == nil {
		t.Fatalf("aggregate commands missing: %#v", file.Commands)
	}
	if aggregateHeaderHasSyntaxDiagnostic(nil, child) || aggregateHeaderHasSyntaxDiagnostic(file, nil) {
		t.Fatal("nil aggregate header reported a diagnostic")
	}
	header := syntax.Span{Start: child.Name.Start, End: child.Argument.End}
	file.Diagnostics = append(file.Diagnostics, syntax.Diagnostic{Span: syntax.Span{Start: header.End, End: header.End}})
	if !aggregateHeaderHasSyntaxDiagnostic(file, child) {
		t.Fatal("header-end syntax diagnostic was not recognized")
	}
	file.Diagnostics = nil
	if owner, member, binding, ok := classObjectVariableOwner(result, child, "value"); !ok || owner != parent || member == nil || binding != 0 {
		t.Fatalf("inherited object variable = %#v %#v %d %t", owner, member, binding, ok)
	}
	if owner, span, ok := classStaticVariableOwner(result, child, "count"); !ok || owner != parent || file.Text(span) != "count" {
		t.Fatalf("inherited static variable = %#v %v %t", owner, span, ok)
	}
	if _, _, ok := aggregateVariableBinding(file, parent, "value", true); ok {
		t.Fatal("instance variable resolved as static")
	}
	if !commandIsClassMethod(file, constructor) || commandIsClassMethod(file, method) || commandIsClassMethod(nil, method) {
		t.Fatal("class method classification changed")
	}
	if got := aggregateEndSpan(file, child); file.Text(got) != "endclass" || aggregateEndSpan(nil, nil) != (syntax.Span{}) {
		t.Fatalf("aggregate end span = %v", got)
	}
}

func TestSemanticHelpersPreserveStaticValueRules(t *testing.T) {
	negative := &syntax.Expression{
		Kind:  syntax.ExpressionUnary,
		Value: "-",
		Span:  syntax.Span{Start: 0, End: 2},
		Children: []*syntax.Expression{{
			Kind: syntax.ExpressionNumber, Value: "0x2", Span: syntax.Span{Start: 1, End: 2},
		}},
	}
	if value, ok := staticNumberValue(negative); !ok || value != -2 {
		t.Fatalf("static unary number = %d, %t", value, ok)
	}
	if _, ok := staticNumberValue(&syntax.Expression{Kind: syntax.ExpressionNumber, Value: "1.5"}); ok {
		t.Fatal("float literal must not be used as a static boolean number")
	}
	if diagnostic, ok := numberAsBoolDiagnostic(negative); !ok || diagnostic.Code != "vim/E1023" || diagnostic.Message != "Using a Number as a Bool: -2" {
		t.Fatalf("number-to-bool diagnostic = %#v, %t", diagnostic, ok)
	}
	for _, test := range []struct {
		typ  ValueType
		code string
	}{
		{ValueType{Name: "list"}, "vim/E730"},
		{ValueType{Name: "dict"}, "vim/E731"},
		{ValueType{Name: "blob"}, "vim/E976"},
		{ValueType{Name: "func"}, "vim/E729"},
	} {
		if diagnostic, ok := stringConversionDiagnostic(test.typ, syntax.Span{}); !ok || diagnostic.Code != test.code {
			t.Fatalf("string conversion for %#v = %#v, %t", test.typ, diagnostic, ok)
		}
	}
	for _, test := range []struct {
		typ  ValueType
		code string
	}{
		{ValueType{Name: "special"}, "vim/E611"},
		{ValueType{Name: "func"}, "vim/E703"},
		{ValueType{Name: "dict"}, "vim/E728"},
		{ValueType{Name: "list"}, "vim/E745"},
		{ValueType{Name: "blob"}, "vim/E974"},
	} {
		if diagnostic, ok := numericConversionDiagnostic(test.typ, syntax.Span{}); !ok || diagnostic.Code != test.code {
			t.Fatalf("numeric conversion for %#v = %#v, %t", test.typ, diagnostic, ok)
		}
	}
}

func TestSemanticHelpersRespectBuiltinContainerAndCallbackShapes(t *testing.T) {
	if got := scopeDictionary(&syntax.Expression{Kind: syntax.ExpressionParenthesized, Children: []*syntax.Expression{{Kind: syntax.ExpressionIdentifier, Value: "g:"}}}); !got {
		t.Fatal("parenthesized scope dictionary was not recognized")
	}
	if scopeDictionary(&syntax.Expression{Kind: syntax.ExpressionIdentifier, Value: "invalid:"}) {
		t.Fatal("invalid scope dictionary was recognized")
	}

	list := ValueType{Name: "list", Arguments: []ValueType{{Name: "string"}}}
	if got, ok := builtinArgumentExpectation("arg_item_of_prev", []ValueType{list, UnknownValueType}, 1); !ok || got.display != "string" {
		t.Fatalf("list item argument type = %#v, %t", got, ok)
	}
	if got, ok := builtinArgumentExpectation("arg_extend3", []ValueType{{Name: "dict"}, UnknownValueType, UnknownValueType}, 2); !ok || got.display != "string" {
		t.Fatalf("dict extend argument type = %#v, %t", got, ok)
	}
	arguments, result := builtinCallbackSignature(ValueType{Name: "tuple", Arguments: []ValueType{{Name: "number"}, {Name: "string"}}}, "arg_sort_how")
	if len(arguments) != 2 || arguments[0].Name != "any" || result == nil || result.Name != "number" {
		t.Fatalf("tuple sort callback signature = %#v, %#v", arguments, result)
	}
	actual := ValueType{Name: "func", Arguments: []ValueType{{Name: "number"}}, ArgumentCountKnown: true}
	expected := builtinArgumentType{kinds: builtinFunc, functionArguments: []ValueType{{Name: "string"}}}
	if !builtinArgumentMismatch(actual, expected) {
		t.Fatal("incompatible callback signature was accepted")
	}
}

func TestSemanticHelpersPreserveAggregateAndConstructorSpans(t *testing.T) {
	file := &syntax.File{
		Source: "class C\nendclass",
		Commands: []syntax.Command{
			{Aggregate: &syntax.Aggregate{Kind: syntax.BlockClass, Name: syntax.Span{Start: 6, End: 7}}, Block: 0},
			{Name: syntax.Span{Start: 8, End: 16}},
		},
		Blocks: []syntax.Block{{Kind: syntax.BlockClass, Header: 0, End: 1}},
	}
	if got := aggregateEndSpan(file, &file.Commands[0]); got != file.Commands[1].Name {
		t.Fatalf("aggregate end span = %#v", got)
	}
	if got := aggregateEndSpan(nil, &file.Commands[0]); got != file.Commands[0].Name {
		t.Fatalf("incomplete aggregate end span = %#v", got)
	}
	if got := aggregateEndSpan(nil, nil); got != (syntax.Span{}) {
		t.Fatalf("nil aggregate end span = %#v", got)
	}

	constructor := &syntax.Command{Function: &syntax.Function{Name: syntax.Span{Start: 0, End: 5}}}
	methodFile := &syntax.File{Source: "C.new"}
	if got := functionKind(methodFile, constructor, &Scope{Kind: syntax.BlockClass}); got != SymbolKindConstructor {
		t.Fatalf("constructor kind = %q", got)
	}
	constructor.Function.Name = syntax.Span{Start: 0, End: 1}
	if got := functionKind(methodFile, constructor, &Scope{Kind: syntax.BlockClass}); got != SymbolKindMethod {
		t.Fatalf("method kind = %q", got)
	}
	if got := functionKind(methodFile, constructor, nil); got != SymbolKindFunction {
		t.Fatalf("function kind = %q", got)
	}

	parameterFile := &syntax.File{Source: "this.member"}
	parameter := syntax.Parameter{Target: &syntax.Expression{
		Kind:     syntax.ExpressionMember,
		Span:     syntax.Span{Start: 0, End: 11},
		Operator: syntax.Span{Start: 4, End: 5},
	}}
	if got := parameterDeclarationSpan(parameterFile, parameter); got != (syntax.Span{Start: 5, End: 11}) {
		t.Fatalf("constructor parameter declaration span = %#v", got)
	}
}

func TestAnalyzeAggregateContractFailures(t *testing.T) {
	for _, test := range []struct {
		name, source, code string
	}{
		{
			name:   "missing interface variable",
			source: "vim9script\ninterface Face\n  var value: number\nendinterface\nclass Impl implements Face\nendclass\n",
			code:   "vim/E1348",
		},
		{
			name:   "missing interface method",
			source: "vim9script\ninterface Face\n  def Run(): void\nendinterface\nclass Impl implements Face\nendclass\n",
			code:   "vim/E1349",
		},
		{
			name:   "constructor default must be none",
			source: "vim9script\nclass Box\n  def new(this.value = 1)\n  enddef\nendclass\n",
			code:   "vim/E1328",
		},
		{
			name:   "object primitive type",
			source: "vim9script\nvar value: object<number>\n",
			code:   "vim/E1353",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			found := false
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == test.code {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("diagnostics = %#v", result.Diagnostics)
			}
		})
	}
}

func TestSemanticHelpersUseRecoveringAssignmentAndTypeFacts(t *testing.T) {
	file := syntax.Parse("vim9script\nvar value = 1\nvalue = 2\n")
	var assignment *syntax.Command
	for index := range file.Commands {
		if file.Commands[index].Canonical == "" && len(file.Commands[index].Expressions) > 0 {
			assignment = &file.Commands[index]
		}
	}
	if assignment == nil || directAssignmentTarget(assignment) == nil {
		t.Fatalf("assignment target was not recovered: %#v", file.Commands)
	}
	if directAssignmentTarget(nil) != nil {
		t.Fatal("nil command has an assignment target")
	}

	expression := &syntax.Expression{Kind: syntax.ExpressionIdentifier}
	analysis := &FileAnalysis{expressionTypes: map[*syntax.Expression]ValueType{expression: {Name: "number"}}}
	if got := analysis.TypeOf(expression); got.Name != "number" {
		t.Fatalf("stored expression type = %#v", got)
	}
	if got := (*FileAnalysis)(nil).TypeOf(expression); !isUnresolvedType(got) {
		t.Fatalf("nil analysis type = %#v", got)
	}
}
