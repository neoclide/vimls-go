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

func TestAnalyzeInfersShiftAndDestructuredElementTypes(t *testing.T) {
	result := Analyze(syntax.Parse("vim9script\nvar shifted = 8 << 1\nvar [count, label] = [1, 'one']\n"))
	declarations := make(map[string]*Declaration)
	for _, declaration := range result.Declarations {
		declarations[declaration.Name] = declaration
	}
	for name, want := range map[string]string{"shifted": "number", "count": "number", "label": "string"} {
		if declarations[name] == nil || declarations[name].Type.Name != want {
			t.Fatalf("%s type = %#v, want %s", name, declarations[name], want)
		}
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
`
	result := Analyze(syntax.Parse(source))
	declarations := make(map[string]*Declaration)
	for _, declaration := range result.Root.Declarations {
		declarations[declaration.Name] = declaration
	}
	for name, want := range map[string]string{
		"count": "number", "pieces": "list", "copied": "tuple", "channel": "channel",
		"truth": "bool", "files": "list", "nullValue": "special",
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
}

func TestAnalyzeUnknownTypesStayConservativeAndNilSafe(t *testing.T) {
	result := Analyze(nil)
	if got := result.TypeOf(nil); got.Name != ValueTypeAny {
		t.Fatalf("nil type = %#v", got)
	}
	result = Analyze(syntax.Parse("vim9script\nvar value = object.member\nvar dynamic = UnknownCall(value)\n"))
	for _, declaration := range result.Root.Declarations {
		if declaration.Type.Name != ValueTypeAny {
			t.Fatalf("%s type = %#v", declaration.Name, declaration.Type)
		}
	}
	if got := result.TypeOf(nil); got.Name != ValueTypeAny {
		t.Fatalf("missing expression type = %#v", got)
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
	if len(result.Root.Declarations) != 1 || result.Root.Declarations[0].Type.Name != "func" || result.Root.Declarations[0].Type.Return == nil || result.Root.Declarations[0].Type.Return.Name != "any" {
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
