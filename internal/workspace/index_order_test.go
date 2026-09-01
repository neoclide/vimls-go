package workspace

import (
	"testing"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestFactOrderingUsesEveryStableTieBreaker(t *testing.T) {
	base := SymbolFact{Path: "b", SelectionRange: syntax.Span{Start: 2, End: 4}, Range: syntax.Span{Start: 1, End: 5}, Name: "b", Kind: analysis.SymbolKindVariable, Exported: true, Detail: "b"}
	less := []SymbolFact{
		{Path: "a"}, {Path: "b", SelectionRange: syntax.Span{Start: 1, End: 4}}, {Path: "b", SelectionRange: syntax.Span{Start: 2, End: 3}},
		{Path: "b", SelectionRange: base.SelectionRange, Range: syntax.Span{Start: 0, End: 5}}, {Path: "b", SelectionRange: base.SelectionRange, Range: syntax.Span{Start: 1, End: 4}},
		{Path: "b", SelectionRange: base.SelectionRange, Range: base.Range, Name: "a"}, {Path: "b", SelectionRange: base.SelectionRange, Range: base.Range, Name: "b", Kind: analysis.SymbolKindFunction},
		{Path: "b", SelectionRange: base.SelectionRange, Range: base.Range, Name: "b", Kind: base.Kind, Exported: false},
		{Path: "b", SelectionRange: base.SelectionRange, Range: base.Range, Name: "b", Kind: base.Kind, Exported: true, Detail: "a"},
	}
	for number, fact := range less {
		if !factLess(fact, base) {
			t.Errorf("tie breaker %d did not order less: %#v", number, fact)
		}
	}
	if factLess(base, base) {
		t.Fatal("fact sorts before itself")
	}
	sortFacts(less)
	if len(namesIn([]SymbolFact{{Name: "a"}, {Name: "a"}, {Name: "b"}})) != 2 {
		t.Fatal("names not deduplicated")
	}
	if len(referenceNamesIn([]ExternalReferenceFact{{Name: "a"}, {Name: "a"}, {Name: "b"}})) != 2 {
		t.Fatal("reference names not deduplicated")
	}

	// Relationship ordering is persisted in snapshots, so every ordering key
	// must be deterministic even when facts originate in different files.
	leftRelation := TypeRelationFact{Child: SymbolKey{Path: "b", SelectionRange: syntax.Span{Start: 2}}, ParentSpan: syntax.Span{Start: 2}, Kind: analysis.TypeRelationKind(1)}
	for _, other := range []TypeRelationFact{
		{Child: SymbolKey{Path: "a"}},
		{Child: SymbolKey{Path: "b", SelectionRange: syntax.Span{Start: 1}}},
		{Child: leftRelation.Child, ParentSpan: syntax.Span{Start: 1}},
		{Child: leftRelation.Child, ParentSpan: leftRelation.ParentSpan, Kind: analysis.TypeRelationKind(0)},
	} {
		if !relationFactLess(other, leftRelation) {
			t.Errorf("relation %#v does not sort before %#v", other, leftRelation)
		}
	}
	if relationFactLess(leftRelation, leftRelation) {
		t.Fatal("relation sorts before itself")
	}
	leftAlias := TypeAliasFact{Alias: SymbolKey{Path: "b", SelectionRange: syntax.Span{Start: 2}}, TargetSpan: syntax.Span{Start: 2}}
	for _, other := range []TypeAliasFact{
		{Alias: SymbolKey{Path: "a"}},
		{Alias: SymbolKey{Path: "b", SelectionRange: syntax.Span{Start: 1}}},
		{Alias: leftAlias.Alias, TargetSpan: syntax.Span{Start: 1}},
	} {
		if !aliasFactLess(other, leftAlias) {
			t.Errorf("alias %#v does not sort before %#v", other, leftAlias)
		}
	}
	if aliasFactLess(leftAlias, leftAlias) {
		t.Fatal("alias sorts before itself")
	}
	leftCall := CallFact{Caller: SymbolKey{Path: "b", SelectionRange: syntax.Span{Start: 2}}, CalleeSpan: syntax.Span{Start: 2}, CalleeName: "b"}
	for _, other := range []CallFact{
		{Caller: SymbolKey{Path: "a"}},
		{Caller: SymbolKey{Path: "b", SelectionRange: syntax.Span{Start: 1}}},
		{Caller: leftCall.Caller, CalleeSpan: syntax.Span{Start: 1}},
		{Caller: leftCall.Caller, CalleeSpan: leftCall.CalleeSpan, CalleeName: "a"},
	} {
		if !callFactLess(other, leftCall) {
			t.Errorf("call %#v does not sort before %#v", other, leftCall)
		}
	}
	if callFactLess(leftCall, leftCall) {
		t.Fatal("call sorts before itself")
	}
}
