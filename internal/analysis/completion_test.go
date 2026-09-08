package analysis

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestCompletionFactsPreserveDeclarationsAndTypes(t *testing.T) {
	for _, source := range []string{
		"function! Read(arg)\n  let local = expand('~')\n  let [first, second] = [1, 'two']\n  return local\nendfunction\n",
		"vim9script\n# deprecated use Next\nvar Old = 1\ndef Read(arg: string): string\n  var local = arg\n  return local\nenddef\n",
		"vim9script\nclass Box\n  var value: number\nendclass\nvar box = Box.new()\necho box.value\n",
		"vim9script\nclass Box\n  var value: number\nendclass\ndef Make()\n  return Box.new()\nenddef\nvar box = Make()\necho box.value\n",
		"vim9script\nclass Box\n  var value: number\nendclass\nvar box = Make()\necho box.value\ndef Make()\n  return Box.new()\nenddef\n",
		"vim9script\nvar outer = 'text'\nvar Fn = (arg: number) => arg + 1\nfor item in [1, 2]\n  echo item\nendfor\n",
		"vim9script\ndef Read(flag: bool)\n  if flag\n    var local = 1\n  else\n    var local = 'text'\n  endif\nenddef\n",
		"vim9script\nvar Fn = (arg: string) => {\n  var local = arg\n  return local\n}\n",
		"vim9script\nimport './module.vim' as Mod\ntype Names = list<string>\nvar names: Names = []\n",
	} {
		t.Run(source, func(t *testing.T) {
			file := syntax.Parse(source)
			facts := CollectCompletionFacts(file)
			types := NewCompletionTypes(facts)
			full := Analyze(file)
			if len(facts.References) != 0 || len(facts.Diagnostics) != 0 {
				t.Fatal("completion collected reference/diagnostic results")
			}
			if len(facts.Declarations) != len(full.Declarations) || len(facts.Scopes) != len(full.Scopes) {
				t.Fatalf("declarations/scopes = %d/%d, full %d/%d", len(facts.Declarations), len(facts.Scopes), len(full.Declarations), len(full.Scopes))
			}
			for i, declaration := range facts.Declarations {
				want := full.Declarations[i]
				if declaration.Name != want.Name || declaration.Span != want.Span || declaration.Kind != want.Kind || declaration.Parameter != want.Parameter || declaration.Deprecated != want.Deprecated || !reflect.DeepEqual(types.DeclarationType(declaration), want.Type) {
					t.Errorf("declaration %q = %#v, full %#v", declaration.Name, declaration, want)
				}
				if declaration == want || declaration.Scope == want.Scope {
					t.Fatal("completion shares mutable state with full analysis")
				}
			}
		})
	}
}

func TestCompletionFactsDoNotContainDiagnostics(t *testing.T) {
	file := syntax.Parse("vim9script\nvar count: number = 'wrong'\nset nonexistentoption\n")
	if len(Analyze(file).Diagnostics) == 0 {
		t.Fatal("fixture must exercise diagnostic passes")
	}
	if facts := CollectCompletionFacts(file); len(facts.Diagnostics) != 0 || len(facts.References) != 0 {
		t.Fatalf("completion performed full analysis: %#v", facts)
	}
	if facts := CollectCompletionFacts(nil); facts.Root == nil || len(facts.Declarations) != 0 {
		t.Fatalf("nil completion facts = %#v", facts)
	}
}

func TestCompletionTypesAreDemandDrivenAndDoNotMutateSharedFacts(t *testing.T) {
	file := syntax.Parse("vim9script\nvar selected = expand('~')\nvar unrelated = [1, 2, 3]\ndef Recursive()\n  return Recursive()\nenddef\nvar Fn = (arg: number) => {\n  var local = arg + 1\n  return local\n}\n")
	facts := CollectCompletionFacts(file)
	before := CollectCompletionFacts(file)
	query := NewCompletionTypes(facts)
	byName := make(map[string]*Declaration)
	for _, declaration := range facts.Declarations {
		byName[declaration.Name] = declaration
	}
	if len(query.types) != 0 || len(facts.expressionTypes) != 0 {
		t.Fatal("constructing completion facts inferred expression types")
	}
	if typ := query.DeclarationType(byName["selected"]); typ.Name != "string" {
		t.Fatalf("selected type = %#v", typ)
	}
	if len(query.types) != 1 {
		t.Fatalf("query inferred unrelated declarations: %#v", query.types)
	}
	if typ := query.DeclarationType(byName["Recursive"]); typ.Name != "func" || typ.Return == nil || !isUnresolvedType(*typ.Return) {
		t.Fatalf("recursive return type = %#v", typ)
	}
	if typ := query.DeclarationType(byName["Fn"]); typ.Name != "func" || typ.Return == nil || typ.Return.Name != "number" {
		t.Fatalf("block lambda type = %#v", typ)
	}
	if !reflect.DeepEqual(facts, before) {
		t.Fatal("on-demand inference mutated shared completion facts")
	}
}

func TestAnalyzeYieldAbandonsPrivatePartialResult(t *testing.T) {
	file := syntax.Parse("vim9script\nvar count: number = 'wrong'\necho count\n")
	for _, stopAt := range []int{1, 8, 42} {
		calls := 0
		result, err := AnalyzeWithYield(file, false, func() error {
			calls++
			if calls == stopAt {
				return context.Canceled
			}
			return nil
		})
		if !errors.Is(err, context.Canceled) || result != nil || calls != stopAt {
			t.Fatalf("stop at %d: result=%p err=%v calls=%d", stopAt, result, err, calls)
		}
	}
	for _, configFile := range []bool{false, true} {
		calls := 0
		result, err := AnalyzeWithYield(file, configFile, func() error { calls++; return nil })
		if err != nil || calls < 42 || !reflect.DeepEqual(result, analyzeWithRole(file, configFile)) {
			t.Fatalf("yielding changed full analysis: config=%t calls=%d err=%v", configFile, calls, err)
		}
	}
}
