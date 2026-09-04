package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// Analysis is invoked while a document contains partially edited, frequently
// inconsistent declarations.  Exercise a deterministic cross-product of such
// fragments and retain the contract that every result is source-bounded.
func TestObjectTypeArgumentValidityBoundaries(t *testing.T) {
	result := &FileAnalysis{File: &syntax.File{Source: "object<number>"}}
	scope := &Scope{}
	for _, test := range []struct {
		name string
		kind syntax.TypeKind
		want objectTypeValidity
	}{
		{name: "any", kind: syntax.TypeNamed, want: objectTypeValid},
		{name: "bool", kind: syntax.TypeNamed, want: objectTypeInvalid},
		{name: "number", kind: syntax.TypeNamed, want: objectTypeInvalid},
		{name: "float", kind: syntax.TypeNamed, want: objectTypeInvalid},
		{name: "string", kind: syntax.TypeNamed, want: objectTypeInvalid},
		{name: "special", kind: syntax.TypeNamed, want: objectTypeInvalid},
		{name: "dict", kind: syntax.TypeNamed, want: objectTypeInvalid},
		{name: "list", kind: syntax.TypeNamed, want: objectTypeInvalid},
		{name: "tuple", kind: syntax.TypeNamed, want: objectTypeInvalid},
		{name: "blob", kind: syntax.TypeNamed, want: objectTypeInvalid},
		{name: "func", kind: syntax.TypeNamed, want: objectTypeInvalid},
		{name: "partial", kind: syntax.TypeNamed, want: objectTypeInvalid},
		{name: "job", kind: syntax.TypeNamed, want: objectTypeInvalid},
		{name: "channel", kind: syntax.TypeNamed, want: objectTypeInvalid},
		{name: "void", kind: syntax.TypeNamed, want: objectTypeInvalid},
		{name: "Unknown", kind: syntax.TypeNamed, want: objectTypeUnknown},
		{name: "list", kind: syntax.TypeGeneric, want: objectTypeInvalid},
	} {
		node := &syntax.Type{Kind: test.kind, Name: test.name, Span: syntax.Span{Start: 0, End: 1}}
		if got := objectTypeArgumentValidity(result, scope, node, nil, map[syntax.Span]bool{}); got != test.want {
			t.Errorf("object type %s/%d = %d, want %d", test.name, test.kind, got, test.want)
		}
	}
	optional := &syntax.Type{Kind: syntax.TypeOptional, Arguments: []*syntax.Type{{Kind: syntax.TypeNamed, Name: "number", Span: syntax.Span{Start: 0, End: 1}}}}
	if got := objectTypeArgumentValidity(result, scope, optional, nil, map[syntax.Span]bool{}); got != objectTypeInvalid {
		t.Fatalf("optional object type = %d", got)
	}
	if got := objectTypeArgumentValidity(result, scope, nil, nil, nil); got != objectTypeUnknown {
		t.Fatalf("nil object type = %d", got)
	}
}

func TestCallRelationTargetBoundaries(t *testing.T) {
	if name, span, identifier := callRelationTarget(nil); name != "" || span != (syntax.Span{}) || identifier {
		t.Fatalf("nil call target = %q, %#v, %v", name, span, identifier)
	}
	identifier := &syntax.Expression{Kind: syntax.ExpressionCall, Children: []*syntax.Expression{{Kind: syntax.ExpressionIdentifier, Value: "Func", Span: syntax.Span{Start: 1, End: 5}}}}
	if name, span, named := callRelationTarget(identifier); name != "Func" || span != (syntax.Span{Start: 1, End: 5}) || !named {
		t.Fatalf("identifier target = %q, %#v, %v", name, span, named)
	}
	member := &syntax.Expression{Kind: syntax.ExpressionCall, Children: []*syntax.Expression{{Kind: syntax.ExpressionMember, Value: "Run", Span: syntax.Span{Start: 0, End: 7}, Operator: syntax.Span{Start: 3, End: 4}}}}
	if name, span, named := callRelationTarget(member); name != "Run" || span != (syntax.Span{Start: 4, End: 7}) || named {
		t.Fatalf("member target = %q, %#v, %v", name, span, named)
	}
	callable := &Declaration{Name: "Func"}
	functionScope := &Scope{}
	if got := enclosingCallable(functionScope, map[*Scope]*Declaration{functionScope: callable}); got != callable {
		t.Fatalf("callable scope = %#v", got)
	}
	lambdaScope := &Scope{Parent: functionScope, Lambda: &syntax.Expression{}}
	if got := enclosingCallable(lambdaScope, map[*Scope]*Declaration{functionScope: callable}); got != nil {
		t.Fatalf("lambda callable = %#v", got)
	}
}

func TestAnalysisRecoveryMatrix(t *testing.T) {
	fragments := []string{
		"var value = 1", "var value: string = 1", "const value = null",
		"def Func(value: number): string\n  return value\nenddef",
		"def Func(value: string): number\n  return value\nenddef",
		"class Item\n  var value: number\nendclass",
		"class Item\n  static var value = 1\nendclass",
		"abstract class Item\n  abstract def Run()\nendclass",
		"interface Item\n  def Run(value: number): string\nendinterface",
		"enum Item\n  One\n  Two = 2\nenum",
		"type Alias = number", "type Alias = Missing", "import './missing.vim' as lib",
		"for value in [1, 'two']\n  echo value\nendfor",
		"if true\n  var nested = false\nendif",
		"try\n  throw 'failure'\ncatch\nendtry",
		"var callback = (value: number): number => value + 1",
		"var object = null\necho object.member",
		"value += 'text'", "echo 1 + 'text'", "echo true + 1",
		"class Parent\n  var inherited: number\n  def Run(value: number): number\n    return value\n  enddef\nendclass",
		"class Child extends Parent\n  var inherited: string\n  def Run(value: string): string\n    return value\n  enddef\nendclass",
		"interface Service\n  def Start(value: number): void\nendinterface",
		"class ServiceImpl implements Service\n  def Start(value: string): number\n    return value\n  enddef\nendclass",
		"enum Values\n  First\n  First\nendenum",
		"def Generic<T>(value: list<T>): T\n  return value[0]\nenddef",
		"var typed: dict<list<number>> = {key: [1]}\ntyped.key->add('wrong')",
		"var callback: func(number): string = (value) => value\necho callback(1, 2)",
		"var values: list<number> = [1]\necho values.missing\necho values[true]",
		"var nested = {one: {two: 1}}\necho nested.one.two",
		"try\n  var guarded = 1\ncatch /x/\n  var guarded = 'x'\nfinally\n  echo guarded\nendtry",
		"for [first, second] in [[1, 2]]\n  echo first\nendfor",
	}
	state := uint32(0x9e3779b9)
	next := func() int {
		state = state*1664525 + 1013904223
		return int(state % uint32(len(fragments)))
	}
	for caseNumber := range 1200 {
		var source strings.Builder
		source.WriteString("vim9script\n")
		for count := 0; count < 1+caseNumber%5; count++ {
			source.WriteString(fragments[next()])
			source.WriteByte('\n')
		}
		text := source.String()
		file := syntax.Parse(text)
		result := Analyze(file)
		if result == nil || result.File != file {
			t.Fatalf("case %d analysis result = %#v", caseNumber, result)
		}
		for _, diagnostic := range CombinedDiagnostics(file, result) {
			if diagnostic.Span.Start < 0 || diagnostic.Span.End < diagnostic.Span.Start || diagnostic.Span.End > len(text) {
				t.Fatalf("case %d diagnostic %#v is outside source length %d", caseNumber, diagnostic, len(text))
			}
		}
	}
}
