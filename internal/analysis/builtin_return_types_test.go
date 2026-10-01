package analysis

import (
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestBuiltinReturnTypes(t *testing.T) {
	result := Analyze(syntax.Parse(`vim9script
var added = autocmd_add([])
var deleted = autocmd_delete([])
var position = match('text', 'x')
var elapsed = reltime()
var counts = searchcount()
var fuzzy = matchfuzzypos(['text'], 't')
var integer = abs(-1)
var decimal = abs(-1.0)
var oneSign = sign_define('mark')
var manySigns = sign_define([{name: 'mark'}])
var patch = diff([], [])
var directory = finddir('runtime')
var file = findfile('runtime')
var servers = serverlist()
echo added deleted position elapsed counts fuzzy integer decimal oneSign manySigns patch directory file servers
`))
	declarations := declarationsByName(result)
	for name, want := range map[string]string{
		"added": "bool", "deleted": "bool", "position": "number", "elapsed": "list",
		"counts": "dict", "fuzzy": "list", "integer": "number", "decimal": "float",
		"oneSign": "number", "manySigns": "list", "patch": "string", "directory": "string",
		"file": "string", "servers": "string",
	} {
		if got := declarations[name]; got == nil || got.Type.Name != want {
			t.Fatalf("%s type = %#v, want %s", name, got, want)
		}
	}
	if got := declarations["elapsed"].Type.Arguments; len(got) != 1 || got[0].Name != "number" {
		t.Fatalf("reltime type = %#v", declarations["elapsed"].Type)
	}
	if got := declarations["counts"].Type.Arguments; len(got) != 1 || got[0].Name != "number" {
		t.Fatalf("searchcount type = %#v", declarations["counts"].Type)
	}
	if got := declarations["fuzzy"].Type.Arguments; len(got) != 1 || got[0].Name != "list" || len(got[0].Arguments) != 1 || !isUnknownType(got[0].Arguments[0]) {
		t.Fatalf("matchfuzzypos type = %#v", declarations["fuzzy"].Type)
	}
	if got := declarations["manySigns"].Type.Arguments; len(got) != 1 || got[0].Name != "number" {
		t.Fatalf("sign_define type = %#v", declarations["manySigns"].Type)
	}
	if diagnostics := CombinedDiagnostics(result.File, result); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestBuiltinDynamicReturnTypesAreUnknown(t *testing.T) {
	result := Analyze(syntax.Parse(`vim9script
var flag = true
var dynamicAbs = abs(g:unknown)
var dynamicSign = sign_define(g:unknown)
var options = diff(['before'], ['after'], {})
var counted = finddir('runtime', '', 1)
var listed = serverlist({})
var expanded = expand('*', false, flag)
var globbed = glob('*', false, flag)
var matched = submatch(1, flag)
var paths = globpath('.', '*', false, flag)
def AbsUnknown(value: any)
  var absAny = abs(value)
  echo absAny
enddef
AbsUnknown(g:unknown)
echo dynamicAbs dynamicSign options counted listed expanded globbed matched paths
`))
	declarations := declarationsByName(result)
	for _, name := range []string{"dynamicAbs", "dynamicSign", "options", "counted", "listed", "expanded", "globbed", "matched", "paths", "absAny"} {
		declaration := declarations[name]
		if declaration == nil || !isUnknownType(declaration.Type) {
			t.Fatalf("%s type = %#v, want unresolved", name, declaration)
		}
	}
	if diagnostics := CombinedDiagnostics(result.File, result); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestBuiltinStringListReturnTypesIncludeMethodArguments(t *testing.T) {
	file := syntax.Parse(`vim9script
var expanded = expand('*', false, (v:true))
var globbed = glob('*', false, (v:true))
var matched = submatch(1, (v:true))
var paths = '*'->globpath('.', false, true)
var legacyPaths = globpath('.', '*', false, false)
echo expanded globbed matched paths legacyPaths
`)
	result := Analyze(file)
	declarations := declarationsByName(result)
	for _, name := range []string{"expanded", "globbed", "matched", "paths"} {
		got := declarations[name]
		if got == nil || got.Type.Name != "list" || len(got.Type.Arguments) != 1 || got.Type.Arguments[0].Name != "string" {
			t.Fatalf("%s type = %#v, want list<string>", name, got)
		}
	}
	if got := declarations["legacyPaths"]; got == nil || got.Type.Name != "string" {
		t.Fatalf("legacy globpath type = %#v, want string", got)
	}
	facts := CollectCompletionFacts(file)
	completion := NewCompletionTypes(facts)
	if got := completion.DeclarationType(declarationsByName(facts)["paths"]); got.Name != "list" || len(got.Arguments) != 1 || got.Arguments[0].Name != "string" {
		t.Fatalf("completion globpath type = %#v, want list<string>", got)
	}
	if diagnostics := CombinedDiagnostics(result.File, result); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestBuiltinReturnTypesInLegacyScript(t *testing.T) {
	result := Analyze(syntax.Parse("let s:legacy_paths = globpath('.', '*', 0, 1)\nlet s:legacy_match = match('text', 't')\nlet true = 0\nlet s:legacy_glob_true = glob('*.txt', 0, true)\nlet s:legacy_glob_vtrue = glob('*.txt', 0, v:true)\necho s:legacy_paths s:legacy_match s:legacy_glob_true s:legacy_glob_vtrue\n"))
	declarations := declarationsByName(result)
	if got := declarations["s:legacy_paths"]; got == nil || got.Type.Name != "list" || len(got.Type.Arguments) != 1 || got.Type.Arguments[0].Name != "string" {
		t.Fatalf("legacy globpath type = %#v, want list<string>", got)
	}
	if got := declarations["s:legacy_match"]; got == nil || got.Type.Name != "number" {
		t.Fatalf("legacy match type = %#v, want number", got)
	}
	if got := declarations["s:legacy_glob_vtrue"]; got == nil || got.Type.Name != "list" || len(got.Type.Arguments) != 1 || got.Type.Arguments[0].Name != "string" {
		t.Fatalf("legacy glob v:true type = %#v, want list<string>", got)
	}
	if got := declarations["s:legacy_glob_true"]; got == nil || got.Type.Name != "" {
		t.Fatalf("legacy glob bare true type = %#v, want unknown", got)
	}
	if diagnostics := CombinedDiagnostics(result.File, result); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestBuiltinGetReturnTypeRequiresMatchingFallback(t *testing.T) {
	result := Analyze(syntax.Parse(`vim9script
var number = get([1], 0)
var text = get(['x'], 0, 'fallback')
var blobByte = get(0z12, 0)
var mismatch = get([1], 0, 'x')
var nullDefault = get([1], 0, null)
var vNullDefault = get([1], 0, v:null)
var noneDefault = get([1], 0, v:none)
var nestedSource: list<list<number>> = [[1]]
var nested = get(nestedSource, 0, [2])
var nestedMismatch = get(nestedSource, 0, ['x'])
var unknown = get(g:, 'missing', 1)
var functionSource: list<func(number): number> = [(n: number): number => n]
var FunctionMatch = get(functionSource, 0, (n: number): number => n)
var FunctionMismatch = get(functionSource, 0, (n: string): string => n)
class Item
endclass
class Other
endclass
var items: list<Item> = [Item.new()]
var nominalMatch = get(items, 0, Item.new())
var nominalMismatch = get(items, 0, Other.new())
var stringShouldFail: string = get([1], 0)
var mismatchString: string = get(['x'], 0, 1)
var mismatchNumber: number = get(['x'], 0, 1)
var listShouldPass: list<string> = submatch(1, true)
echo number text blobByte mismatch nullDefault vNullDefault noneDefault nested nestedMismatch unknown FunctionMatch FunctionMismatch nominalMatch nominalMismatch stringShouldFail mismatchString mismatchNumber listShouldPass
`))
	declarations := declarationsByName(result)
	for name, want := range map[string]string{"number": "number", "text": "string", "blobByte": "number", "nested": "list", "FunctionMatch": "func", "nominalMatch": "Item"} {
		if got := declarations[name]; got == nil || got.Type.Name != want {
			t.Fatalf("%s type = %#v, want %s", name, got, want)
		}
	}
	for _, name := range []string{"mismatch", "nullDefault", "vNullDefault", "noneDefault", "nestedMismatch", "unknown", "FunctionMismatch", "nominalMismatch"} {
		if got := declarations[name]; got == nil || !isUnknownType(got.Type) {
			t.Fatalf("%s type = %#v, want unresolved", name, got)
		}
	}
	var mismatches []syntax.Diagnostic
	for _, diagnostic := range CombinedDiagnostics(result.File, result) {
		if diagnostic.Code == "vim/E1012" {
			mismatches = append(mismatches, diagnostic)
		}
	}
	all := CombinedDiagnostics(result.File, result)
	if len(all) != 1 || len(mismatches) != 1 || result.File.Text(mismatches[0].Span) != "get([1], 0)" {
		t.Fatalf("diagnostics = %#v, want one E1012 on get([1], 0)", all)
	}
}

func TestBuiltinReturnTypeDiagnostics(t *testing.T) {
	result := Analyze(syntax.Parse(`vim9script
var matchString: string = match('text', 't')
var dynamicSubmatch: list<string> = submatch(1, g:flag)
`))
	diagnostics := CombinedDiagnostics(result.File, result)
	if len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1012" || result.File.Text(diagnostics[0].Span) != "match('text', 't')" {
		t.Fatalf("diagnostics = %#v, want E1012 on match() only", diagnostics)
	}
}
