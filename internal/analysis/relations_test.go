package analysis

import (
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestCollectTypeRelations(t *testing.T) {
	file := syntax.Parse("vim9script\ninterface Root\nendinterface\ninterface Child extends Root\nendinterface\nclass Base\nendclass\nclass Item extends Base implements Root, mod.Other\nendclass\n")
	got := CollectTypeRelations(file)
	want := []struct {
		child  string
		parent string
		kind   TypeRelationKind
	}{
		{"Child", "Root", TypeRelationExtends},
		{"Item", "Base", TypeRelationExtends},
		{"Item", "Root", TypeRelationImplements},
		{"Item", "Other", TypeRelationImplements},
	}
	if len(got) != len(want) {
		t.Fatalf("relations = %#v", got)
	}
	for index, relation := range got {
		if relation.ChildName != want[index].child || relation.ParentName != want[index].parent || relation.Kind != want[index].kind || file.Text(relation.ParentSpan) == "" {
			t.Errorf("relation %d = %#v, want %#v", index, relation, want[index])
		}
	}
}

func TestCollectCallRelationsUsesNamedCallersAndSkipsDeferredBodies(t *testing.T) {
	file := syntax.Parse("vim9script\ndef Target<T>()\nenddef\nclass C\n  static def Build()\n  enddef\nendclass\ndef Outer()\n  Target<number>()\n  C.Build()\n  defer Target()\n  var Lambda = () => Target()\n  nnoremap <expr> x Target()\n  autocmd BufEnter * Target()\n  command Run Target()\n  len([])\nenddef\n")
	got := CollectCallRelations(Analyze(file))
	if len(got) != 3 {
		t.Fatalf("calls = %#v", got)
	}
	want := []string{"Target", "Build", "Target"}
	for index, relation := range got {
		if relation.CallerName != "Outer" || relation.CallerKind != SymbolKindFunction || relation.CalleeName != want[index] || file.Text(relation.CalleeSpan) != want[index] {
			t.Errorf("call %d = %#v", index, relation)
		}
	}
}

func TestCollectCallRelationsUsesInnermostNamedFunction(t *testing.T) {
	file := syntax.Parse("function! Target()\nendfunction\nfunction! Outer()\n  function! Inner()\n    call Target()\n  endfunction\nendfunction\n")
	got := CollectCallRelations(Analyze(file))
	if len(got) != 1 || got[0].CallerName != "Inner" || got[0].CalleeName != "Target" {
		t.Fatalf("calls = %#v", got)
	}
}
