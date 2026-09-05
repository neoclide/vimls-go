package analysis

import (
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestAnalyzeInfersVim9LiteralAndContainerTypes(t *testing.T) {
	source := `vim9script
var numberValue = 1
var floatValue = 1.5
var stringValue = 'text'
var boolValue = true
var blobValue = 0zFF
var numbers = [1, 2, 3]
var mixed = [1, 'two']
var dictionary = {one: 1, two: 2}
var tupleValue = (1, 'two')
var explicit: list<string> = []
var hexadecimal = 0xDEAD
`
	result := Analyze(syntax.Parse(source))
	for _, declaration := range result.Root.Declarations {
		if declaration.Name == "explicit" {
			if declaration.Type.Name != "list" || len(declaration.Type.Arguments) != 1 || declaration.Type.Arguments[0].Name != "string" {
				t.Fatalf("explicit type = %#v", declaration.Type)
			}
			continue
		}
		want := map[string]ValueType{
			"numberValue": {Name: "number"}, "floatValue": {Name: "float"},
			"stringValue": {Name: "string"}, "boolValue": {Name: "bool"},
			"blobValue":   {Name: "blob"},
			"hexadecimal": {Name: "number"},
			"numbers":     {Name: "list", Arguments: []ValueType{{Name: "number"}}},
			"mixed":       {Name: "list", Arguments: []ValueType{{Name: "any"}}},
			"dictionary":  {Name: "dict", Arguments: []ValueType{{Name: "number"}}},
			"tupleValue":  {Name: "tuple", Arguments: []ValueType{{Name: "number"}, {Name: "string"}}},
		}
		if declaration.Type.Name != want[declaration.Name].Name || len(declaration.Type.Arguments) != len(want[declaration.Name].Arguments) {
			t.Fatalf("%s type = %#v, want %#v", declaration.Name, declaration.Type, want[declaration.Name])
		}
		for index := range declaration.Type.Arguments {
			if declaration.Type.Arguments[index].Name != want[declaration.Name].Arguments[index].Name {
				t.Fatalf("%s type = %#v, want %#v", declaration.Name, declaration.Type, want[declaration.Name])
			}
		}
	}
}

func TestSliceTypesPreserveContainers(t *testing.T) {
	for _, test := range []struct{ expression, name, element string }{
		{"values[0 : 1]", "list", "number"},
		{"values[:]", "list", "number"},
		{"values[-2 :]", "list", "number"},
		{"values[2 : 1]", "list", "number"},
		{"values[0]", "number", ""},
		{"('abc')[0 : 1]", "string", ""},
		{"(0z0102)[0 : 1]", "blob", ""},
		{"(1, 'two')[0 : 0]", "tuple", ""},
	} {
		t.Run(test.expression, func(t *testing.T) {
			result := Analyze(syntax.Parse("vim9script\nvar values = [1, 2, 3]\nvar result = " + test.expression + "\n"))
			for _, declaration := range result.Declarations {
				if declaration.Name != "result" {
					continue
				}
				typ := declaration.Type
				if typ.Name != test.name || test.element != "" && (len(typ.Arguments) != 1 || typ.Arguments[0].Name != test.element) {
					t.Fatalf("type=%#v", typ)
				}
				return
			}
			t.Fatal("missing result declaration")
		})
	}
}

func TestAnalyzeInfersShiftAndDestructuredElementTypes(t *testing.T) {
	result := Analyze(syntax.Parse("vim9script\nvar shifted = 8 << 1\nvar [count, label] = [1, 'one']\nconst winid = win_getid()\nconst [row, col] = win_screenpos(winid)\n"))
	declarations := make(map[string]*Declaration)
	for _, declaration := range result.Declarations {
		declarations[declaration.Name] = declaration
	}
	for name, want := range map[string]string{"shifted": "number", "count": "number", "label": "string", "row": "number", "col": "number"} {
		if declarations[name] == nil || declarations[name].Type.Name != want {
			t.Fatalf("%s type = %#v, want %s", name, declarations[name], want)
		}
	}
}

func TestAnalyzeInfersTupleAndRestDestructuringTypes(t *testing.T) {
	source := `vim9script
var pair: tuple<number, string> = (1, 'one')
var [pairNumber, pairString] = pair
def GetPair(): tuple<bool, float>
  return (true, 1.5)
enddef
var [callBool, callFloat] = GetPair()
var [tupleHead; tupleRest] = (1, 'two', true)
var [listHead; listRest] = [1, 2, 3]
var [only] = [42]
var variadic: tuple<number, ...list<string>> = (1, 'a', 'b')
var [fixed, variable; variadicRest] = variadic
`
	result := Analyze(syntax.Parse(source))
	declarations := make(map[string]*Declaration)
	for _, declaration := range result.Declarations {
		declarations[declaration.Name] = declaration
	}
	for name, want := range map[string]string{
		"pairNumber": "number", "pairString": "string",
		"callBool": "bool", "callFloat": "float",
		"tupleHead": "number", "listHead": "number", "only": "number",
		"fixed": "number", "variable": "string",
	} {
		if declarations[name] == nil || declarations[name].Type.Name != want {
			t.Fatalf("%s type = %#v, want %s", name, declarations[name], want)
		}
	}
	if typ := declarations["tupleRest"].Type; typ.Name != "tuple" || len(typ.Arguments) != 2 || typ.Arguments[0].Name != "string" || typ.Arguments[1].Name != "bool" {
		t.Fatalf("tupleRest type = %#v", typ)
	}
	if typ := declarations["listRest"].Type; typ.Name != "list" || len(typ.Arguments) != 1 || typ.Arguments[0].Name != "number" {
		t.Fatalf("listRest type = %#v", typ)
	}
	if typ := declarations["variadicRest"].Type; typ.Name != "tuple" || !typ.Variadic || len(typ.Arguments) != 1 || typ.Arguments[0].Name != "list" || len(typ.Arguments[0].Arguments) != 1 || typ.Arguments[0].Arguments[0].Name != "string" {
		t.Fatalf("variadicRest type = %#v", typ)
	}
}

func TestAnalyzeInfersContainerConcatenationTypes(t *testing.T) {
	result := Analyze(syntax.Parse("vim9script\nvar listValue = [1] + [2]\nvar tupleValue = (1, 'one') + (2, 'two')\nvar blobValue = 0z01 + 0z02\n"))
	declarations := make(map[string]*Declaration)
	for _, declaration := range result.Declarations {
		declarations[declaration.Name] = declaration
	}
	if declarations["listValue"].Type.Name != "list" || len(declarations["listValue"].Type.Arguments) != 1 || declarations["listValue"].Type.Arguments[0].Name != "number" {
		t.Fatalf("list concatenation type = %#v", declarations["listValue"].Type)
	}
	if declarations["tupleValue"].Type.Name != "tuple" || len(declarations["tupleValue"].Type.Arguments) != 4 {
		t.Fatalf("tuple concatenation type = %#v", declarations["tupleValue"].Type)
	}
	if declarations["blobValue"].Type.Name != "blob" {
		t.Fatalf("blob concatenation type = %#v", declarations["blobValue"].Type)
	}
}

func TestAnalyzeInfersLegacyImplicitArgumentTypes(t *testing.T) {
	result := Analyze(syntax.Parse("function! F(key, ...) abort\n  let values = [a:key] + a:000\n  let count = a:0\n  let first = a:firstline\n  let last = a:lastline\nendfunction\nfunction! Empty() abort\n  let rest = a:000\nendfunction\n"))
	declarations := make(map[string]*Declaration)
	for _, declaration := range result.Declarations {
		declarations[declaration.Name] = declaration
	}
	if typ := declarations["key"].Type; !isUnknownType(typ) {
		t.Fatalf("key type = %#v, want unknown", typ)
	}
	for _, name := range []string{"values", "rest"} {
		typ := declarations[name].Type
		if typ.Name != "list" || len(typ.Arguments) != 1 || !isUnknownType(typ.Arguments[0]) {
			t.Fatalf("%s type = %#v, want list<unknown>", name, typ)
		}
	}
	for _, name := range []string{"count", "first", "last"} {
		if typ := declarations[name].Type; typ.Name != "number" {
			t.Fatalf("%s type = %#v, want number", name, typ)
		}
	}
}

func TestAnalyzeNarrowsLegacyTypeGuards(t *testing.T) {
	for _, test := range []struct {
		name      string
		typeCode  string
		want      string
		wantKnown bool
	}{
		{"number constant", "v:t_number", "number", true},
		{"string constant", "v:t_string", "string", true},
		{"function constant", "v:t_func", "func", true},
		{"list constant", "v:t_list", "list", true},
		{"dictionary constant", "v:t_dict", "dict", true},
		{"float constant", "v:t_float", "float", true},
		{"boolean constant", "v:t_bool", "bool", true},
		{"none constant", "v:t_none", ValueTypeSpecial, true},
		{"blob constant", "v:t_blob", "blob", true},
		{"numeric number code", "0", "number", true},
		{"numeric string code", "1", "string", true},
		{"numeric function code", "2", "func", true},
		{"numeric list code", "3", "list", true},
		{"numeric dictionary code", "4", "dict", true},
		{"numeric float code", "5", "float", true},
		{"numeric boolean code", "6", "bool", true},
		{"numeric none code", "7", ValueTypeSpecial, true},
		{"numeric blob code", "10", "blob", true},
		{"list sample", "type([])", "list", true},
		{"string sample", "type('')", "string", true},
		{"null sample", "type(v:null)", ValueTypeSpecial, true},
		{"unsupported type code", "v:t_job", "", false},
		{"dynamic type code", "wanted_type", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "let value = {}\nlet wanted_type = 3\nif type(value) == " + test.typeCode + "\n  let narrowed = value\nendif\n"
			result := Analyze(syntax.Parse(source))
			var narrowed *Declaration
			for _, declaration := range result.Declarations {
				if declaration.Name == "narrowed" {
					narrowed = declaration
					break
				}
			}
			if narrowed == nil {
				t.Fatal("narrowed declaration not found")
			}
			if test.wantKnown {
				if narrowed.Type.Name != test.want {
					t.Fatalf("narrowed type = %#v, want %s", narrowed.Type, test.want)
				}
			} else if !isUnknownType(narrowed.Type) {
				t.Fatalf("narrowed type = %#v, want unknown", narrowed.Type)
			}
		})
	}
}

func TestAnalyzeLegacyTypeGuardAssignmentInvalidatesNarrowing(t *testing.T) {
	result := Analyze(syntax.Parse("let value = {}\nif type(value) == v:t_list\n  let value = {}\n  let narrowed = value\nendif\n"))
	for _, declaration := range result.Declarations {
		if declaration.Name == "narrowed" && declaration.Type.Name != "dict" {
			t.Fatalf("narrowed type = %#v, want dict", declaration.Type)
		}
	}
}

func TestAnalyzeInfersForDestructuredBindingTypes(t *testing.T) {
	source := "for [s:kind, s:body] in [[\"Style\", '@markoCSS'], [\"Script\", '@markoTS']]\n  echo s:kind . s:body\nendfor\n"
	result := Analyze(syntax.Parse(source))
	seen := make(map[string]bool)
	for _, declaration := range result.Declarations {
		if declaration.Name != "s:kind" && declaration.Name != "s:body" {
			continue
		}
		seen[declaration.Name] = true
		if declaration.Type.Name != "string" {
			t.Fatalf("%s type = %#v, want string", declaration.Name, declaration.Type)
		}
	}
	if !seen["s:kind"] || !seen["s:body"] {
		t.Fatalf("destructured declarations = %#v", result.Declarations)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E730" {
			t.Fatalf("destructured string received E730: %#v", result.Diagnostics)
		}
	}
}

func TestAnalyzeInfersOperatorsReferencesAndFunctions(t *testing.T) {
	source := `vim9script
var n = 1
var propagated = n
var comparison = n > 0
var concatenated = 'a' .. 'b'
var arithmetic = n + 2
var lambda = (value: number): number => value + 1
var forward = Later(1)
def Later(value: number): string
  return 'done'
enddef
var after = forward
`
	result := Analyze(syntax.Parse(source))
	declarations := make(map[string]*Declaration)
	for _, declaration := range result.Root.Declarations {
		declarations[declaration.Name] = declaration
	}
	for _, name := range []string{"n", "propagated", "arithmetic"} {
		if declarations[name].Type.Name != "number" {
			t.Fatalf("%s type = %#v", name, declarations[name].Type)
		}
	}
	for _, name := range []string{"comparison", "concatenated"} {
		if declarations[name].Type.Name != map[string]string{"comparison": "bool", "concatenated": "string"}[name] {
			t.Fatalf("%s type = %#v", name, declarations[name].Type)
		}
	}
	if declarations["lambda"].Type.Name != "func" || declarations["lambda"].Type.Arguments[0].Name != "number" || declarations["lambda"].Type.Return == nil || declarations["lambda"].Type.Return.Name != "number" {
		t.Fatalf("lambda type = %#v", declarations["lambda"].Type)
	}
	later := declarations["Later"]
	if later == nil || later.Type.Name != "func" || later.Type.Arguments[0].Name != "number" || later.Type.Return == nil || later.Type.Return.Name != "string" {
		t.Fatalf("Later type = %#v", later)
	}
	if declarations["forward"].Type.Name != "string" || declarations["after"].Type.Name != "string" {
		t.Fatalf("forward types = %#v %#v", declarations["forward"].Type, declarations["after"].Type)
	}
}

func TestAnalyzeInfersBuiltinAndVimVariableTypes(t *testing.T) {
	source := `vim9script
var count = len('text')
var pieces = split('a b')
var copied = ([1, 2],)->copy()
var channel = test_null_channel()
var truth = v:true
var files = v:oldfiles
var nullValue = v:null
var literalNull = null
var noneValue = v:none
`
	result := Analyze(syntax.Parse(source))
	declarations := make(map[string]*Declaration)
	for _, declaration := range result.Root.Declarations {
		declarations[declaration.Name] = declaration
	}
	for name, want := range map[string]string{
		"count": "number", "pieces": "list", "copied": "tuple", "channel": "channel",
		"truth": "bool", "files": "list", "nullValue": "null", "literalNull": "null", "noneValue": "none",
	} {
		if declarations[name] == nil || declarations[name].Type.Name != want {
			t.Fatalf("%s type = %#v, want %s", name, declarations[name], want)
		}
	}
	if got := declarations["pieces"].Type.Arguments; len(got) != 1 || got[0].Name != "string" {
		t.Fatalf("pieces type = %#v", declarations["pieces"].Type)
	}
	if got := declarations["copied"].Type.Arguments; len(got) != 1 || got[0].Name != "list" || len(got[0].Arguments) != 1 || got[0].Arguments[0].Name != "number" {
		t.Fatalf("copied type = %#v", declarations["copied"].Type)
	}
	if got := declarations["files"].Type.Arguments; len(got) != 1 || got[0].Name != "string" {
		t.Fatalf("files type = %#v", declarations["files"].Type)
	}
	for _, name := range []string{"nullValue", "literalNull", "noneValue"} {
		if !isSpecialType(declarations[name].Type) {
			t.Fatalf("%s category = %q, want special", name, valueTypeCategory(declarations[name].Type))
		}
	}
	if merged := mergeTypes(declarations["nullValue"].Type, declarations["noneValue"].Type); merged.Name != ValueTypeSpecial {
		t.Fatalf("merged special type = %#v", merged)
	}
}

func TestAnalyzeInfersGetDefaultType(t *testing.T) {
	source := `vim9script
var maxEditCount = get(g:, 'coc_edits_maximum_count', 200)
var label = get(g:, 'label', 'fallback')
var methodCount = g:->get('method_count', 100)
var enabled = get(g:, 'enabled', true)
var unknown = get(g:, 'missing')
`
	result := Analyze(syntax.Parse(source))
	declarations := make(map[string]*Declaration)
	for _, declaration := range result.Root.Declarations {
		declarations[declaration.Name] = declaration
	}
	for name, want := range map[string]string{
		"maxEditCount": "number",
		"label":        "string",
		"methodCount":  "number",
		"enabled":      "bool",
	} {
		if declarations[name] == nil || declarations[name].Type.Name != want {
			t.Fatalf("%s type = %#v, want %s", name, declarations[name], want)
		}
	}
	if declarations["unknown"] == nil || !isUnresolvedType(declarations["unknown"].Type) {
		t.Fatalf("unknown type = %#v, want unresolved", declarations["unknown"])
	}
}

func TestAnalyzeGetWithSpecialDefaultUsesContainerElementType(t *testing.T) {
	source := `vim9script
const nullDefault = get(g:, 'null_default', null)
const vNullDefault = get(g:, 'v_null_default', v:null)
const noneDefault = get(g:, 'none_default', v:none)
echo nullDefault vNullDefault noneDefault
def BufferLineCount(bufnr: number): number
  const info = get(getbufinfo(bufnr), 0, null)
  if empty(info)
    throw $'Invalid buffer id: {bufnr}'
  endif
  return info.loaded == 0 ? 0 : info.linecount
enddef
`
	result := Analyze(syntax.Parse(source))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
	declarations := make(map[string]*Declaration)
	for _, declaration := range result.Declarations {
		declarations[declaration.Name] = declaration
	}
	for _, name := range []string{"nullDefault", "vNullDefault", "noneDefault"} {
		if declarations[name] == nil || !isUnresolvedType(declarations[name].Type) {
			t.Fatalf("%s type = %#v, want unresolved", name, declarations[name])
		}
	}
	info := declarations["info"]
	if info == nil || info.Type.Name != "dict" || len(info.Type.Arguments) != 1 || !isUnresolvedType(info.Type.Arguments[0]) {
		t.Fatalf("info type = %#v, want dict<any>", info)
	}
}

func TestAnalyzeInfersVim9HeredocType(t *testing.T) {
	for _, source := range []string{
		"vim9script\nconst call_function =<< trim CALL_FUNCTION_END\n  function! coc#api#call(method, args) abort\n  endfunction\nCALL_FUNCTION_END\n",
		"vim9script\nconst call_function =<< trim CALL_FUNCTION_END\n  function! coc#api#call(method, args) abort\n  endfunction\n\necho call_function\n",
	} {
		result := Analyze(syntax.Parse(source))
		if len(result.Root.Declarations) != 1 {
			t.Fatalf("declarations = %#v", result.Root.Declarations)
		}
		declaration := result.Root.Declarations[0]
		if declaration.Name != "call_function" || declaration.Type.Name != "list" || len(declaration.Type.Arguments) != 1 || declaration.Type.Arguments[0].Name != "string" {
			t.Fatalf("heredoc type = %#v", declaration)
		}
	}
}

func TestAnalyzeUnknownTypesStayConservativeAndNilSafe(t *testing.T) {
	result := Analyze(nil)
	if got := result.TypeOf(nil); !isUnresolvedType(got) {
		t.Fatalf("nil type = %#v", got)
	}
	result = Analyze(syntax.Parse("vim9script\nvar value = object.member\nvar dynamic = UnknownCall(value)\n"))
	for _, declaration := range result.Root.Declarations {
		if !isUnresolvedType(declaration.Type) {
			t.Fatalf("%s type = %#v", declaration.Name, declaration.Type)
		}
	}
	if got := result.TypeOf(nil); !isUnresolvedType(got) {
		t.Fatalf("missing expression type = %#v", got)
	}
}

func TestAnalyzeExplicitAnyIsDistinctFromUnknown(t *testing.T) {
	result := Analyze(syntax.Parse("vim9script\nvar explicit: any\nvar inferred = explicit\nvar unknown = Dynamic()\n"))
	declarations := make(map[string]*Declaration)
	for _, declaration := range result.Root.Declarations {
		declarations[declaration.Name] = declaration
	}
	for _, name := range []string{"explicit", "inferred"} {
		if declarations[name] == nil || declarations[name].Type.Name != ValueTypeAny {
			t.Fatalf("%s type = %#v, want explicit any", name, declarations[name])
		}
	}
	if declarations["unknown"] == nil || !isUnresolvedType(declarations["unknown"].Type) {
		t.Fatalf("unknown type = %#v", declarations["unknown"])
	}
}

func TestAnalyzeLambdaTypesUseLexicalParametersAndCaptures(t *testing.T) {
	source := `vim9script
var outer = 1
var maker = (value: number) => (outer: number) => value + outer
var block = (value: number): number => {
  var local = value + outer
  return local
}
var inferredBlock = (value: number) => {
  var local = value + outer
  return local
}
`
	result := Analyze(syntax.Parse(source))
	var maker, block, inferredBlock *Declaration
	for _, declaration := range result.Root.Declarations {
		switch declaration.Name {
		case "maker":
			maker = declaration
		case "block":
			block = declaration
		case "inferredBlock":
			inferredBlock = declaration
		}
	}
	if maker == nil || maker.Type.Name != "func" || maker.Type.Return == nil || maker.Type.Return.Name != "func" {
		t.Fatalf("nested lambda type = %#v", maker)
	}
	if got := maker.Type.Return.Arguments; len(got) != 1 || got[0].Name != "number" || maker.Type.Return.Return == nil || maker.Type.Return.Return.Name != "number" {
		t.Fatalf("nested lambda signature = %#v", maker.Type)
	}
	if block == nil || block.Type.Name != "func" || block.Type.Return == nil || block.Type.Return.Name != "number" {
		t.Fatalf("block lambda type = %#v", block)
	}
	if inferredBlock == nil || inferredBlock.Type.Name != "func" || inferredBlock.Type.Return == nil || inferredBlock.Type.Return.Name != "number" {
		t.Fatalf("inferred block lambda type = %#v", inferredBlock)
	}
	var local *Declaration
	for _, declaration := range result.Declarations {
		if declaration.Name == "local" {
			local = declaration
		}
	}
	if local == nil || local.Type.Name != "number" {
		t.Fatalf("block local type = %#v", local)
	}
}

func TestAnalyzeLambdaExplicitReturnTypeRejectsIncompatibleInference(t *testing.T) {
	result := Analyze(syntax.Parse("vim9script\nvar f = (value: number): string => value\n"))
	if len(result.Root.Declarations) != 1 || result.Root.Declarations[0].Type.Name != "func" || result.Root.Declarations[0].Type.Return == nil || !isUnresolvedType(*result.Root.Declarations[0].Type.Return) {
		t.Fatalf("incompatible lambda type = %+v return=%+v", *result.Root.Declarations[0], *result.Root.Declarations[0].Type.Return)
	}
}

func TestAnalyzeFunctionTypeImplicitVoidReturn(t *testing.T) {
	result := Analyze(syntax.Parse("vim9script\nvar Any: func\nvar AnyReturn: func: string\nvar VoidReturn: func(number)\n"))
	declarations := make(map[string]*Declaration)
	for _, declaration := range result.Root.Declarations {
		declarations[declaration.Name] = declaration
	}
	if declarations["Any"].Type.ArgumentCountKnown || declarations["Any"].Type.Return != nil {
		t.Fatalf("bare func type = %+v", declarations["Any"].Type)
	}
	if declarations["AnyReturn"].Type.ArgumentCountKnown || declarations["AnyReturn"].Type.Return == nil || declarations["AnyReturn"].Type.Return.Name != "string" {
		t.Fatalf("func return-only type = %+v", declarations["AnyReturn"].Type)
	}
	if !declarations["VoidReturn"].Type.ArgumentCountKnown || declarations["VoidReturn"].Type.Return == nil || declarations["VoidReturn"].Type.Return.Name != "void" {
		t.Fatalf("parenthesized func type = %+v", declarations["VoidReturn"].Type)
	}
}

func TestAnalyzeFunctionArityFacts(t *testing.T) {
	tests := []struct {
		name, source, declaration string
		required, arguments       int
		variadic                  bool
	}{
		{
			name:        "legacy defaults and varargs",
			source:      "function! Flexible(required, optional = 1, ...)\nendfunction\n",
			declaration: "Flexible", required: 1, arguments: 3, variadic: true,
		},
		{
			name:        "Vim9 defaults and varargs",
			source:      "vim9script\ndef Flexible(required: number, optional: string = 'x', ...rest: list<any>)\nenddef\n",
			declaration: "Flexible", required: 1, arguments: 3, variadic: true,
		},
		{
			name:        "lambda",
			source:      "vim9script\nvar Callback = (first: number, second: string) => first\n",
			declaration: "Callback", required: 2, arguments: 2,
		},
		{
			name:        "optional function type",
			source:      "vim9script\nvar Callback: func(number, ?string)\n",
			declaration: "Callback", required: 1, arguments: 2,
		},
		{
			name:        "variadic function type",
			source:      "vim9script\nvar Callback: func(number, ...list<any>)\n",
			declaration: "Callback", required: 1, arguments: 2, variadic: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			var declaration *Declaration
			for _, candidate := range result.Declarations {
				if candidate.Name == test.declaration {
					declaration = candidate
					break
				}
			}
			if declaration == nil {
				t.Fatalf("missing declaration %q: %#v", test.declaration, result.Declarations)
			}
			typ := declaration.Type
			if typ.Name != "func" || !typ.ArgumentCountKnown || typ.RequiredArguments != test.required || len(typ.Arguments) != test.arguments || typ.Variadic != test.variadic {
				t.Fatalf("arity facts = %#v, want required=%d arguments=%d variadic=%t", typ, test.required, test.arguments, test.variadic)
			}
		})
	}
}
