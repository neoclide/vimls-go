package analysis

import (
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestAnalyzeNestedShadowingAndControlScopes(t *testing.T) {
	source := `function Outer(value)
  let outer = value
  if outer
    let value = outer
    echo value
  endif
  echo value outer
endfunction
`
	result := Analyze(syntax.Parse(source))
	if result.Root == nil || len(result.Scopes) != 3 {
		t.Fatalf("scopes = %#v", result.Scopes)
	}
	if result.Scopes[1].Kind != syntax.BlockFunction || result.Scopes[1].Parent != result.Root {
		t.Fatalf("function scope = %#v", result.Scopes[1])
	}
	if len(result.Scopes[1].Children) != 1 || result.Scopes[1].Children[0].Kind != syntax.BlockIf {
		t.Fatalf("nested scopes = %#v", result.Scopes[1].Children)
	}
	functionScope := result.Scopes[1]
	if got, want := declarationNames(functionScope), []string{"value", "outer"}; !equalNames(got, want) {
		t.Fatalf("function declarations = %v, want %v", got, want)
	}
	if got, want := declarationNames(functionScope.Children[0]), []string{"value"}; !equalNames(got, want) {
		t.Fatalf("if declarations = %v, want %v", got, want)
	}
	if len(result.References) != 6 {
		t.Fatalf("references = %#v", result.References)
	}
	// The parameter is visible in the initializer; the nested value shadows it
	// only after its declaration, and the final value resolves to the parameter.
	if result.References[0].Name != "value" || result.References[0].Declaration != functionScope.Declarations[0] {
		t.Fatalf("initializer reference = %#v", result.References[0])
	}
	if result.References[1].Name != "outer" || result.References[1].Declaration != functionScope.Declarations[1] {
		t.Fatalf("nested initializer reference = %#v", result.References[1])
	}
	if result.References[2].Name != "outer" || result.References[2].Declaration != functionScope.Declarations[1] {
		t.Fatalf("shadowed reference = %#v", result.References[2])
	}
	if result.References[3].Name != "value" || result.References[3].Declaration != functionScope.Children[0].Declarations[0] {
		t.Fatalf("nested shadowed reference = %#v", result.References[3])
	}
	if result.References[4].Declaration != functionScope.Declarations[0] || result.References[5].Declaration != functionScope.Declarations[1] {
		t.Fatalf("outer references = %#v", result.References[4:])
	}
}

func TestAnalyzeLegacyScriptLocalFunctionPrefixesShareBinding(t *testing.T) {
	result := Analyze(syntax.Parse("function! s:Run()\nendfunction\ncall s:Run()\ncall <SID>Run()\n"))
	var declaration *Declaration
	for _, candidate := range result.Declarations {
		if candidate.Name == "s:Run" {
			declaration = candidate
			break
		}
	}
	if declaration == nil {
		t.Fatal("script-local declaration is missing")
	}
	bound := 0
	for _, reference := range result.References {
		if reference.Declaration == declaration {
			bound++
		}
	}
	if bound != 2 {
		t.Fatalf("script-local references = %#v", result.References)
	}
}

func TestAnalyzeLegacyArgumentAndLocalPrefixesStayInFunction(t *testing.T) {
	result := Analyze(syntax.Parse("let local = 0\nfunction! Run(arg)\n  let local = a:arg\n  echo l:local a:arg\nendfunction\necho l:local\n"))
	functionScope := result.Scopes[1]
	if len(functionScope.Declarations) != 2 || !functionScope.Declarations[0].Parameter {
		t.Fatalf("function declarations = %#v", functionScope.Declarations)
	}
	if len(result.References) != 4 {
		t.Fatalf("references = %#v", result.References)
	}
	if result.References[0].Declaration != functionScope.Declarations[0] || result.References[1].Declaration != functionScope.Declarations[1] || result.References[2].Declaration != functionScope.Declarations[0] {
		t.Fatalf("prefixed references = %#v", result.References[:3])
	}
	if result.References[3].Declaration != nil {
		t.Fatalf("script-level l: reference escaped function scope: %#v", result.References[3])
	}
	vim9 := Analyze(syntax.Parse("vim9script\ndef Run(arg: number)\n  var local = 1\n  echo a:arg l:local\nenddef\n"))
	if len(vim9.References) != 2 || vim9.References[0].Declaration != nil || vim9.References[1].Declaration != nil {
		t.Fatalf("legacy prefixes resolved inside :def: %#v", vim9.References)
	}
}

func TestAnalyzeVariableUseBeforeDeclarationStaysUnresolved(t *testing.T) {
	source := "echo value\nvar value = value\necho value\n"
	result := Analyze(syntax.Parse("vim9script\n" + source))
	if len(result.References) != 3 {
		t.Fatalf("references = %#v", result.References)
	}
	if result.References[0].Declaration != nil {
		t.Fatalf("use-before declaration resolved to %#v", result.References[0].Declaration)
	}
	if result.References[1].Declaration != nil {
		t.Fatalf("self-reference in declaration initializer resolved to %#v", result.References[1].Declaration)
	}
	if result.References[2].Declaration == nil || result.References[2].Declaration.Name != "value" {
		t.Fatalf("later reference = %#v", result.References[2])
	}
}

func TestAnalyzeFunctionDefaultReferencesEarlierParameter(t *testing.T) {
	result := Analyze(syntax.Parse("vim9script\ndef Build(first: number, second = first)\n  echo second\nenddef\n"))
	if len(result.Scopes) != 2 || len(result.Scopes[1].Declarations) != 2 {
		t.Fatalf("function scope = %#v", result.Scopes)
	}
	first := result.Scopes[1].Declarations[0]
	second := result.Scopes[1].Declarations[1]
	if len(result.References) != 2 || result.References[0].Name != "first" || result.References[0].Declaration != first {
		t.Fatalf("default reference = %#v", result.References)
	}
	if result.References[1].Name != "second" || result.References[1].Declaration != second {
		t.Fatalf("body reference = %#v", result.References[1])
	}
}

func TestAnalyzeVim9HeredocConstDeclaration(t *testing.T) {
	source := `vim9script
const call_function =<< trim END
  function! coc#api#call(method, args) abort
    return coc#api#Call(a:method, a:args)
  endfunction
END

execute $'legacy execute "{join(call_function, '\n')}"'
`
	result := Analyze(syntax.Parse(source))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
	if len(result.Root.Declarations) != 1 || result.Root.Declarations[0].Name != "call_function" || result.Root.Declarations[0].Kind != SymbolKindConstant {
		t.Fatalf("declarations = %#v", result.Root.Declarations)
	}
	if typ := result.Root.Declarations[0].Type; typ.Name != "list" || len(typ.Arguments) != 1 || typ.Arguments[0].Name != "string" {
		t.Fatalf("call_function type = %#v", typ)
	}
	var reference *Reference
	for _, candidate := range result.References {
		if candidate.Name == "call_function" && candidate.Span.Start > result.Root.Declarations[0].Span.End {
			reference = candidate
			break
		}
	}
	if reference == nil || reference.Declaration != result.Root.Declarations[0] {
		t.Fatalf("call_function reference = %#v", reference)
	}
}

func TestAnalyzeIncompleteVim9HeredocConstDeclaration(t *testing.T) {
	source := `vim9script
const call_function =<< trim CALL_FUNCTION_END
  function! coc#api#call(method, args) abort
    return coc#api#Call(a:method, a:args)
  endfunction

execute $'legacy execute "{join(call_function, '\n')}"'
`
	result := Analyze(syntax.Parse(source))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
	if len(result.Root.Declarations) != 1 || result.Root.Declarations[0].Name != "call_function" || result.Root.Declarations[0].Kind != SymbolKindConstant {
		t.Fatalf("declarations = %#v", result.Root.Declarations)
	}
	var reference *Reference
	for _, candidate := range result.References {
		if candidate.Name == "call_function" && candidate.Span.Start > result.Root.Declarations[0].Span.End {
			reference = candidate
			break
		}
	}
	if reference == nil || reference.Declaration != result.Root.Declarations[0] {
		t.Fatalf("call_function reference = %#v", reference)
	}
}

func TestAnalyzeInitializerUsesOuterShadowedDeclaration(t *testing.T) {
	result := Analyze(syntax.Parse("vim9script\nvar value = 1\nif true\n  var value = value\n  echo value\nendif\n"))
	if len(result.References) != 2 {
		t.Fatalf("references = %#v", result.References)
	}
	outer := result.Root.Declarations[0]
	inner := result.Scopes[1].Declarations[0]
	if result.References[0].Name != "value" || result.References[0].Declaration != outer {
		t.Fatalf("initializer reference = %#v, outer = %#v", result.References[0], outer)
	}
	if result.References[1].Name != "value" || result.References[1].Declaration != inner {
		t.Fatalf("body reference = %#v, inner = %#v", result.References[1], inner)
	}
}

func TestAnalyzeDoesNotTreatVim9LiteralsAsReferences(t *testing.T) {
	result := Analyze(syntax.Parse("vim9script\nvar values = [true, false, null, null_job, null_list]\n"))
	if len(result.References) != 0 {
		t.Fatalf("literal references = %#v", result.References)
	}
}

func TestAnalyzeLambdaScopesCaptureAndShadow(t *testing.T) {
	source := `vim9script
var outer = 1
var make = (value: number) => (outer: number) => value + outer
var result = make(outer)
`
	result := Analyze(syntax.Parse(source))
	if len(result.Scopes) != 3 {
		t.Fatalf("lambda scopes = %#v", result.Scopes)
	}
	outer := result.Root.Declarations[0]
	make := result.Root.Declarations[1]
	if result.Scopes[1].Lambda == nil || result.Scopes[1].Parent != result.Root || len(result.Scopes[1].Declarations) != 1 {
		t.Fatalf("outer lambda scope = %#v", result.Scopes[1])
	}
	if result.Scopes[2].Lambda == nil || result.Scopes[2].Parent != result.Scopes[1] || len(result.Scopes[2].Declarations) != 1 {
		t.Fatalf("inner lambda scope = %#v", result.Scopes[2])
	}
	if result.Scopes[1].Declarations[0].Name != "value" || result.Scopes[2].Declarations[0].Name != "outer" {
		t.Fatalf("lambda declarations = %#v", result.Scopes[1:])
	}
	var valueRef, innerOuterRef, capturedOuterRef *Reference
	for _, reference := range result.References {
		switch reference.Name {
		case "value":
			valueRef = reference
		case "outer":
			switch reference.Declaration {
			case result.Scopes[2].Declarations[0]:
				innerOuterRef = reference
			case outer:
				capturedOuterRef = reference
			}
		}
	}
	if valueRef == nil || valueRef.Declaration != result.Scopes[1].Declarations[0] {
		t.Fatalf("lambda parameter reference = %#v", valueRef)
	}
	if innerOuterRef == nil || capturedOuterRef == nil {
		t.Fatalf("lambda capture/shadow references = %#v", result.References)
	}
	if make == nil {
		t.Fatal("missing make declaration")
	}
}

func TestAnalyzeLegacyLambdaScopeDoesNotLeakParameter(t *testing.T) {
	result := Analyze(syntax.Parse("let f = {item -> item + 1}\necho item\n"))
	if len(result.Scopes) != 2 || result.Scopes[1].Lambda == nil {
		t.Fatalf("legacy lambda scopes = %#v", result.Scopes)
	}
	if len(result.Scopes[1].Declarations) != 1 || result.Scopes[1].Declarations[0].Name != "item" {
		t.Fatalf("legacy lambda declarations = %#v", result.Scopes[1].Declarations)
	}
	if len(result.References) != 2 || result.References[0].Declaration != result.Scopes[1].Declarations[0] || result.References[1].Declaration != nil {
		t.Fatalf("legacy lambda references = %#v", result.References)
	}
}

func TestAnalyzeMappingExprReferencesFunctionAndVariable(t *testing.T) {
	source := "function Fn()\nendfunction\nlet value = 1\nnmap <expr> lhs Fn(value)\n"
	result := Analyze(syntax.Parse(source))
	if len(result.References) != 2 {
		t.Fatalf("mapping expression references = %#v", result.References)
	}
	if result.References[0].Name != "Fn" || result.References[0].Declaration == nil || result.References[0].Declaration.Kind != SymbolKindFunction {
		t.Fatalf("mapping function reference = %#v", result.References[0])
	}
	if result.References[1].Name != "value" || result.References[1].Declaration == nil || result.References[1].Declaration.Name != "value" {
		t.Fatalf("mapping variable reference = %#v", result.References[1])
	}
}

func TestAnalyzeOrdinaryMappingDoesNotCreateReferences(t *testing.T) {
	source := "function Fn()\nendfunction\nlet value = 1\nnmap lhs Fn(value)\n"
	result := Analyze(syntax.Parse(source))
	if len(result.References) != 0 {
		t.Fatalf("ordinary mapping references = %#v", result.References)
	}
}

func TestAnalyzeMappingExprLambdaParameterScope(t *testing.T) {
	source := "let outer = 1\nnmap <expr> lhs {item -> item + outer}\n"
	result := Analyze(syntax.Parse(source))
	if len(result.Scopes) != 2 || result.Scopes[1].Lambda == nil {
		t.Fatalf("mapping lambda scopes = %#v", result.Scopes)
	}
	if len(result.Scopes[1].Declarations) != 1 || result.Scopes[1].Declarations[0].Name != "item" {
		t.Fatalf("mapping lambda declarations = %#v", result.Scopes[1].Declarations)
	}
	if len(result.References) != 2 {
		t.Fatalf("mapping lambda references = %#v", result.References)
	}
	if result.References[0].Name != "item" || result.References[0].Declaration != result.Scopes[1].Declarations[0] {
		t.Fatalf("mapping lambda parameter reference = %#v", result.References[0])
	}
	if result.References[1].Name != "outer" || result.References[1].Declaration != result.Root.Declarations[0] {
		t.Fatalf("mapping lambda capture reference = %#v", result.References[1])
	}
}

func TestAnalyzeConstructorParameterTarget(t *testing.T) {
	source := `vim9script
class Person
  var name: string
  def new(this.name: string)
    return name
  enddef
endclass
`
	result := Analyze(syntax.Parse(source))
	var functionScope *Scope
	for _, scope := range result.Scopes {
		if scope.Kind == syntax.BlockDef {
			functionScope = scope
			break
		}
	}
	if functionScope == nil {
		t.Fatalf("missing constructor scope: %#v", result.Scopes)
	}
	if len(functionScope.Declarations) != 1 {
		t.Fatalf("constructor declarations = %#v", functionScope.Declarations)
	}
	parameter := functionScope.Declarations[0]
	if parameter.Name != "name" {
		t.Fatalf("constructor parameter name = %q, want name", parameter.Name)
	}
	if got, want := result.File.Text(parameter.Span), "name"; got != want {
		t.Fatalf("constructor parameter span text = %q, want %q", got, want)
	}
	if parameter.Type.Name != "string" {
		t.Fatalf("constructor parameter type = %#v, want string", parameter.Type)
	}
	if len(result.References) != 1 || result.References[0].Name != "name" || result.References[0].Declaration != parameter {
		t.Fatalf("constructor parameter reference = %#v, want declaration %#v", result.References, parameter)
	}
	for _, declaration := range result.Declarations {
		if declaration.Name == "this.name" {
			t.Fatalf("constructor shorthand leaked as declaration: %#v", declaration)
		}
	}
}

func TestAnalyzeLambdaBlockWalksBodyReferencesOnce(t *testing.T) {
	result := Analyze(syntax.Parse("vim9script\nvar outer = 1\nvar f = (value: number) => {\n  var local = value + outer\n  return local\n}\n"))
	if len(result.Scopes) != 2 {
		t.Fatalf("block lambda scopes = %#v", result.Scopes)
	}
	if len(result.References) != 3 {
		t.Fatalf("block lambda references = %#v", result.References)
	}
	want := []string{"value", "outer", "local"}
	for index, name := range want {
		if result.References[index].Name != name || result.References[index].Declaration == nil {
			t.Fatalf("reference %d = %#v, want %q with declaration", index, result.References[index], name)
		}
	}
	lambdaScope := result.Scopes[1]
	if result.References[0].Declaration != lambdaScope.Declarations[0] || result.References[1].Declaration != result.Root.Declarations[0] || result.References[2].Declaration != lambdaScope.Declarations[1] {
		t.Fatalf("block lambda declaration links = %#v", result.References)
	}
}

func TestAnalyzeForwardFunctionReference(t *testing.T) {
	source := "call Later()\nfunction Later()\nendfunction\n"
	result := Analyze(syntax.Parse(source))
	if len(result.References) != 1 || result.References[0].Name != "Later" {
		t.Fatalf("references = %#v", result.References)
	}
	if result.References[0].Declaration == nil || result.References[0].Declaration.Kind != SymbolKindFunction {
		t.Fatalf("forward function reference = %#v", result.References[0])
	}
}

func TestAnalyzeDestructuringAndForBindings(t *testing.T) {
	source := `vim9script
var [left, right] = values
for [item, other] in [left, right]
  echo item other left
endfor
`
	result := Analyze(syntax.Parse(source))
	if len(result.Root.Declarations) != 2 || result.Root.Declarations[0].Name != "left" || result.Root.Declarations[1].Name != "right" {
		t.Fatalf("root declarations = %#v", result.Root.Declarations)
	}
	if len(result.Scopes) != 2 || result.Scopes[1].Kind != syntax.BlockFor {
		t.Fatalf("for scopes = %#v", result.Scopes)
	}
	forScope := result.Scopes[1]
	if got, want := declarationNames(forScope), []string{"item", "other"}; !equalNames(got, want) {
		t.Fatalf("for declarations = %v, want %v", got, want)
	}
	if len(result.References) != 6 {
		t.Fatalf("references = %#v", result.References)
	}
	if result.References[0].Name != "values" || result.References[0].Declaration != nil {
		t.Fatalf("declaration initializer = %#v", result.References[0])
	}
	if result.References[1].Name != "left" || result.References[1].Declaration != result.Root.Declarations[0] {
		t.Fatalf("for iterable left = %#v", result.References[1])
	}
	if result.References[2].Name != "right" || result.References[2].Declaration != result.Root.Declarations[1] {
		t.Fatalf("for iterable right = %#v", result.References[2])
	}
	if result.References[3].Declaration != forScope.Declarations[0] || result.References[4].Declaration != forScope.Declarations[1] {
		t.Fatalf("for body bindings = %#v", result.References[3:])
	}
	// The reference to the script variable follows the loop bindings.
	if result.References[5].Name != "left" || result.References[5].Declaration != result.Root.Declarations[0] {
		t.Fatalf("for body outer reference = %#v", result.References[5])
	}
}

func TestAnalyzeScopedNamesAndNonReferences(t *testing.T) {
	source := `let x = 1
let g:x = x
echo g:x x
echo object.field
echo {key: x}
`
	result := Analyze(syntax.Parse(source))
	if len(result.Root.Declarations) != 2 || result.Root.Declarations[0].Name != "x" || result.Root.Declarations[1].Name != "g:x" {
		t.Fatalf("declarations = %#v", result.Root.Declarations)
	}
	if len(result.References) != 5 {
		t.Fatalf("references = %#v", result.References)
	}
	if result.References[0].Name != "x" || result.References[0].Declaration != result.Root.Declarations[0] {
		t.Fatalf("g:x initializer = %#v", result.References[0])
	}
	if result.References[1].Name != "g:x" || result.References[1].Declaration != result.Root.Declarations[1] {
		t.Fatalf("scoped reference = %#v", result.References[1])
	}
	if result.References[2].Name != "x" || result.References[2].Declaration != result.Root.Declarations[0] {
		t.Fatalf("local reference = %#v", result.References[2])
	}
	if result.References[3].Name != "object" || result.References[3].Declaration != nil {
		t.Fatalf("member receiver = %#v", result.References[3])
	}
	if result.References[4].Name != "x" || result.References[4].Declaration != result.Root.Declarations[0] {
		t.Fatalf("dictionary value = %#v", result.References[4])
	}
}

func TestAnalyzeLegacyExplicitScopesBindOnlyProvenDeclarations(t *testing.T) {
	source := "let g:item = 1\nlet b:item = 2\nlet w:item = 3\nlet t:item = 4\nlet s:item = 5\necho g:item b:item w:item t:item s:item b:missing\n"
	result := Analyze(syntax.Parse(source))
	if len(result.Declarations) != 5 || len(result.References) != 6 {
		t.Fatalf("declarations=%#v references=%#v", result.Declarations, result.References)
	}
	for index, name := range []string{"g:item", "b:item", "w:item", "t:item", "s:item"} {
		if result.Declarations[index].Name != name || result.References[index].Name != name || result.References[index].Declaration != result.Declarations[index] {
			t.Errorf("scope %s declaration=%#v reference=%#v", name, result.Declarations[index], result.References[index])
		}
	}
	if result.References[5].Name != "b:missing" || result.References[5].Declaration != nil {
		t.Fatalf("unproven editor state = %#v", result.References[5])
	}
}

func TestAnalyzeDoesNotDuplicateTargetsOrEnumArguments(t *testing.T) {
	source := "vim9script\nvar input = 1\nenum E\n  One(input)\nendenum\n"
	vim9 := Analyze(syntax.Parse(source))
	if len(vim9.References) != 1 || vim9.References[0].Name != "input" || vim9.References[0].Declaration == nil {
		t.Fatalf("enum references = %#v", vim9.References)
	}
}

func TestAnalyzeEmbeddedCommandsUseOuterScopeAndNestedBlockScope(t *testing.T) {
	source := `vim9script
var outer = 1
autocmd BufEnter * if outer
  | var inner = outer
  | echo inner outer
  | endif
echo outer
`
	result := Analyze(syntax.Parse(source))
	if len(result.Declarations) != 2 {
		t.Fatalf("declarations = %#v", result.Declarations)
	}
	if result.Declarations[0].Name != "outer" || result.Declarations[1].Name != "inner" {
		t.Fatalf("declaration order = %#v", result.Declarations)
	}
	if result.Declarations[1].Scope == result.Root || result.Declarations[1].Scope.Kind != syntax.BlockIf {
		t.Fatalf("embedded declaration scope = %#v", result.Declarations[1].Scope)
	}
	if len(result.Scopes) != 2 || result.Scopes[1].CommandList == nil {
		t.Fatalf("scopes = %#v", result.Scopes)
	}
	if len(result.References) != 5 {
		t.Fatalf("references = %v (%#v)", referenceNames(result.References), result.References)
	}
	for index, want := range []string{"outer", "outer", "inner", "outer", "outer"} {
		if result.References[index].Name != want {
			t.Fatalf("reference %d = %#v, want %q", index, result.References[index], want)
		}
	}
	if result.References[0].Declaration != result.Root.Declarations[0] || result.References[1].Declaration != result.Root.Declarations[0] || result.References[2].Declaration != result.Declarations[1] || result.References[3].Declaration != result.Root.Declarations[0] || result.References[4].Declaration != result.Root.Declarations[0] {
		t.Fatalf("embedded reference declarations = %#v", result.References)
	}
}

func TestAnalyzeEmbeddedDeclarationsDoNotLeakFromBlock(t *testing.T) {
	file := syntax.Parse("vim9script\nvar outer = 1\nwindo if outer | var inner = outer | echo inner | endif\necho inner\n")
	result := Analyze(file)
	if len(result.Declarations) != 2 || result.Declarations[1].Name != "inner" {
		t.Fatalf("declarations = %#v", result.Declarations)
	}
	if result.Declarations[1].Scope == result.Root || result.Declarations[1].Scope.Kind != syntax.BlockIf {
		t.Fatalf("inner scope = %#v", result.Declarations[1].Scope)
	}
	if len(result.References) != 4 {
		t.Fatalf("references = %#v", result.References)
	}
	if result.References[0].Declaration != result.Root.Declarations[0] || result.References[1].Declaration != result.Root.Declarations[0] || result.References[2].Declaration != result.Declarations[1] || result.References[3].Declaration != nil {
		t.Fatalf("reference declarations = %#v", result.References)
	}
}

func TestAnalyzeEmbeddedTypesAndFunctions(t *testing.T) {
	source := `vim9script
var value = 1
windo def Embedded(value: number): string
  return 'ok'
enddef
var result = Embedded(value)
`
	result := Analyze(syntax.Parse(source))
	var function, value, resultValue *Declaration
	for _, declaration := range result.Declarations {
		switch declaration.Name {
		case "Embedded":
			function = declaration
		case "value":
			if declaration.Scope == result.Root {
				value = declaration
			}
		case "result":
			resultValue = declaration
		}
	}
	if function == nil || function.Type.Name != "func" || function.Type.Return == nil || function.Type.Return.Name != "string" {
		t.Fatalf("embedded function = %#v", function)
	}
	if value == nil || resultValue == nil || resultValue.Type.Name != "string" {
		t.Fatalf("embedded types = value %#v result %#v", value, resultValue)
	}
}

func TestAnalyzeVim9UserCommandBlockOnce(t *testing.T) {
	result := Analyze(syntax.Parse("vim9script\nvar outer = 1\ncommand Foo {\n  var inner = outer\n  echo inner\n}\n"))
	if len(result.Declarations) != 2 || result.Declarations[0].Name != "outer" || result.Declarations[1].Name != "inner" {
		t.Fatalf("declarations = %#v", result.Declarations)
	}
	if result.Declarations[1].Scope.Kind != syntax.BlockCommand {
		t.Fatalf("inner scope = %#v", result.Declarations[1].Scope)
	}
	if len(result.References) != 2 || result.References[0].Declaration != result.Declarations[0] || result.References[1].Declaration != result.Declarations[1] {
		t.Fatalf("references = %#v", result.References)
	}
}

func TestAnalyzeCollectsKindsMutabilityAndOrderedSpans(t *testing.T) {
	source := `vim9script
import autoload './lib.vim' as lib
type Alias = number
class Box
  const ID = 1
  def new()
  enddef
endclass
enum Color
  Red
endenum
`
	result := Analyze(syntax.Parse(source))
	if len(result.Declarations) != 7 {
		t.Fatalf("declarations = %#v", result.Declarations)
	}
	wantKinds := []SymbolKind{SymbolKindImport, SymbolKindTypeAlias, SymbolKindClass, SymbolKindConstant, SymbolKindConstructor, SymbolKindEnum, SymbolKindEnumMember}
	for index, want := range wantKinds {
		if result.Declarations[index].Kind != want {
			t.Fatalf("declaration %d kind = %q, want %q", index, result.Declarations[index].Kind, want)
		}
		if index > 0 && result.Declarations[index-1].Span.Start >= result.Declarations[index].Span.Start {
			t.Fatalf("declarations are not source ordered: %#v", result.Declarations)
		}
	}
	if result.Declarations[0].Mutable || result.Declarations[1].Mutable || result.Declarations[2].Mutable || result.Declarations[3].Mutable {
		t.Fatalf("immutable declarations marked mutable: %#v", result.Declarations[:4])
	}
	if result.Declarations[4].Mutable || result.Declarations[5].Mutable || result.Declarations[6].Mutable {
		t.Fatalf("non-variable declarations marked mutable: %#v", result.Declarations[4:])
	}
}

func TestAnalyzeHandlesNilAndMalformedSyntax(t *testing.T) {
	result := Analyze(nil)
	if result == nil || result.Root == nil || len(result.Declarations) != 0 || len(result.References) != 0 {
		t.Fatalf("nil analysis = %#v", result)
	}
	file := &syntax.File{
		Source: "x",
		Commands: []syntax.Command{{Block: 99, Expressions: []*syntax.Expression{
			nil,
			{Kind: syntax.ExpressionIdentifier, Span: syntax.Span{Start: -1, End: 99}, Value: "bad"},
		}}},
		Blocks: []syntax.Block{{Parent: 99, Span: syntax.Span{Start: -1, End: 99}}},
	}
	result = Analyze(file)
	if result.Root == nil || len(result.Declarations) != 0 || len(result.References) != 0 {
		t.Fatalf("malformed analysis = %#v", result)
	}
	ordered := Analyze(syntax.Parse("echo b + a\n"))
	if len(ordered.References) != 2 || ordered.References[0].Span.Start >= ordered.References[1].Span.Start {
		t.Fatalf("reference order = %#v", ordered.References)
	}
}

func declarationNames(scope *Scope) []string {
	result := make([]string, 0, len(scope.Declarations))
	for _, declaration := range scope.Declarations {
		result = append(result, declaration.Name)
	}
	return result
}

func equalNames(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func referenceNames(references []*Reference) []string {
	result := make([]string, 0, len(references))
	for _, reference := range references {
		result = append(result, reference.Name)
	}
	return result
}
