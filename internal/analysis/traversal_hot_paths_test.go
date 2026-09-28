package analysis

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestBuiltinUnknownCallTraversalStaysBounded(t *testing.T) {
	for _, depth := range []int{24, 32} {
		t.Run(fmt.Sprintf("depth-%d", depth), func(t *testing.T) {
			source := "vim9script\necho " + strings.Repeat("abs(", depth) + "1" + strings.Repeat(")", depth) + "\n"
			checkpoints := 0
			result, err := AnalyzeWithOptions(syntax.Parse(source), Options{Yield: func() error {
				checkpoints++
				if checkpoints > 128 {
					return context.Canceled
				}
				return nil
			}})
			if err != nil || result == nil {
				t.Fatalf("AnalyzeWithOptions() = %p, %v", result, err)
			}
		})
	}
}

func TestBuiltinArgumentsPreserveCallbackAndMethodTypes(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
	}{
		{name: "normal", source: "vim9script\nvar Identity = (value) => value\nvar source = Identity([1])\nvar result = map(source, (_, item) => {\n  source = [item]\n  return item\n})\n"},
		{name: "method", source: "vim9script\nvar Identity = (value) => value\nvar source = Identity([1])\nvar result = source->map((_, item) => {\n  source = [item]\n  return item\n})\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			if len(file.Diagnostics) != 0 {
				t.Fatalf("parse diagnostics = %#v", file.Diagnostics)
			}
			result := Analyze(file)
			declarations := declarationsByName(result)
			for _, name := range []string{"Identity", "source", "result", "item"} {
				if declarations[name] == nil {
					t.Fatalf("missing %s in %#v", name, result.Declarations)
				}
			}
			if declarations["Identity"].Type.Name != "func" || declarations["source"].Type.Name != "" || declarations["result"].Type.Name != "" {
				t.Fatalf("types = Identity:%#v source:%#v result:%#v", declarations["Identity"].Type, declarations["source"].Type, declarations["result"].Type)
			}
			if declarations["item"].Scope == nil || declarations["item"].Scope.Lambda == nil {
				t.Fatalf("callback parameter scope = %#v", declarations["item"].Scope)
			}
		})
	}
}

func TestBuiltinDirectAndMethodTypesRemainKnown(t *testing.T) {
	for _, test := range []struct{ name, expression string }{
		{name: "direct", expression: "copy(values)"},
		{name: "method", expression: "values->copy()"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse("vim9script\nvar values = [1]\nvar copied = " + test.expression + "\n")
			result := Analyze(file)
			declaration := declarationsByName(result)["copied"]
			if declaration == nil || declaration.Type.Name != "list" {
				t.Fatalf("copied = %#v", declaration)
			}
			facts := CollectCompletionFacts(file)
			if typ := NewCompletionTypes(facts).DeclarationType(declarationsByName(facts)["copied"]); typ.Name != "list" {
				t.Fatalf("completion copied type = %#v", typ)
			}
		})
	}
}

func TestForwardFunctionInferenceRemainsKnown(t *testing.T) {
	file := syntax.Parse("let result = Later()\nfunction! Later() abort\n  return 'later'\nendfunction\n")
	result := Analyze(file)
	declaration := declarationsByName(result)["result"]
	if declaration == nil || declaration.Type.Name != "string" {
		t.Fatalf("result = %#v", declaration)
	}
}

func TestLambdaDeclarationsMatchCompletionFacts(t *testing.T) {
	source := "vim9script\nvar Mapper = (value: number) => {\n  var local = value + 1\n  var Nested = (extra: number) => {\n    var total = local + extra\n    return total\n  }\n  return Nested(1)\n}\nvar result = Mapper(1)\n"
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("parse diagnostics = %#v", file.Diagnostics)
	}
	full := Analyze(file)
	completion := CollectCompletionFacts(file)
	fullDeclarations := declarationsByName(full)
	completionDeclarations := declarationsByName(completion)
	for _, name := range []string{"Mapper", "value", "local", "Nested", "extra", "total", "result"} {
		fullDeclaration := fullDeclarations[name]
		completionDeclaration := completionDeclarations[name]
		if fullDeclaration == nil || completionDeclaration == nil || fullDeclaration.Span != completionDeclaration.Span {
			t.Fatalf("%s declarations = %#v, %#v", name, fullDeclaration, completionDeclaration)
		}
	}
	if len(full.Scopes) != len(completion.Scopes) {
		t.Fatalf("scope count = %d, want %d", len(full.Scopes), len(completion.Scopes))
	}
	for _, name := range []string{"local", "Nested", "total"} {
		declaration := fullDeclarations[name]
		if declaration.Scope == nil || declaration.Scope.Lambda == nil {
			t.Fatalf("%s scope = %#v", name, declaration.Scope)
		}
	}
	if fullDeclarations["total"].Scope.Parent != fullDeclarations["local"].Scope {
		t.Fatalf("nested lambda parent = %#v, want %#v", fullDeclarations["total"].Scope.Parent, fullDeclarations["local"].Scope)
	}
	resolved := false
	for _, reference := range full.References {
		if reference.Name == "local" && reference.Declaration == fullDeclarations["local"] {
			resolved = true
		}
	}
	if !resolved {
		t.Fatal("nested lambda did not resolve local")
	}
	if typ := NewCompletionTypes(completion).DeclarationType(completionDeclarations["total"]); !reflect.DeepEqual(typ, (ValueType{Name: "number"})) {
		t.Fatalf("completion total type = %#v", typ)
	}
}

func declarationsByName(result *FileAnalysis) map[string]*Declaration {
	declarations := make(map[string]*Declaration)
	for _, declaration := range result.Declarations {
		declarations[declaration.Name] = declaration
	}
	return declarations
}
