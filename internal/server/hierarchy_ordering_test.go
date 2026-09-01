package server

import (
	"context"
	"testing"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestWorkspaceMemberIdentityRequiresAllStableFields(t *testing.T) {
	target := workspaceNavigationTarget{match: workspace.SymbolMatch{Fact: workspace.SymbolFact{Path: "/tmp/member.vim", Name: "Member", SelectionRange: syntax.Span{Start: 2, End: 8}}}}
	if !sameWorkspaceMemberSymbol("/tmp/member.vim", &analysis.Symbol{Name: "Member", SelectionRange: syntax.Span{Start: 2, End: 8}}, target) {
		t.Fatal("matching member was rejected")
	}
	for _, symbol := range []*analysis.Symbol{nil, {Name: "Other", SelectionRange: syntax.Span{Start: 2, End: 8}}, {Name: "Member", SelectionRange: syntax.Span{Start: 3, End: 8}}} {
		if sameWorkspaceMemberSymbol("/tmp/member.vim", symbol, target) {
			t.Fatalf("mismatched member accepted: %#v", symbol)
		}
	}
}

func TestHierarchyOrderingAndRelationHelperBoundaries(t *testing.T) {
	makeSymbol := func(path string, start, end int) hierarchySymbol {
		return hierarchySymbol{fact: workspace.SymbolFact{Path: path, SelectionRange: syntax.Span{Start: start, End: end}}}
	}
	if !hierarchySymbolLess(makeSymbol("a", 9, 9), makeSymbol("b", 0, 0)) || !hierarchySymbolLess(makeSymbol("a", 1, 9), makeSymbol("a", 2, 0)) || !hierarchySymbolLess(makeSymbol("a", 1, 2), makeSymbol("a", 1, 3)) {
		t.Fatal("hierarchy ordering lost a tie breaker")
	}
	if hierarchySymbolLess(makeSymbol("a", 1, 2), makeSymbol("a", 1, 2)) {
		t.Fatal("equal hierarchy symbols sort before themselves")
	}
	for _, test := range []struct {
		child, parent analysis.SymbolKind
		kind          analysis.TypeRelationKind
		want          bool
	}{
		{analysis.SymbolKindClass, analysis.SymbolKindClass, analysis.TypeRelationExtends, true},
		{analysis.SymbolKindInterface, analysis.SymbolKindInterface, analysis.TypeRelationExtends, true},
		{analysis.SymbolKindClass, analysis.SymbolKindInterface, analysis.TypeRelationImplements, true},
		{analysis.SymbolKindEnum, analysis.SymbolKindInterface, analysis.TypeRelationImplements, true},
		{analysis.SymbolKindEnum, analysis.SymbolKindClass, analysis.TypeRelationExtends, false},
		{analysis.SymbolKindClass, analysis.SymbolKindClass, analysis.TypeRelationImplements, false},
		{analysis.SymbolKindClass, analysis.SymbolKindClass, analysis.TypeRelationKind(99), false},
	} {
		if got := validTypeRelation(test.child, test.parent, test.kind); got != test.want {
			t.Errorf("validTypeRelation(%v, %v, %v) = %t", test.child, test.parent, test.kind, got)
		}
	}
	if got := appendWarning([]string{"first"}, "first"); len(got) != 1 {
		t.Fatalf("duplicate warning = %#v", got)
	}
	if got := appendWarning(nil, "first"); len(got) != 1 || got[0] != "first" {
		t.Fatalf("new warning = %#v", got)
	}
}

func TestHierarchyRangeDeduplicationUsesAllPositionFields(t *testing.T) {
	ranges := []protocol.Range{
		{Start: protocol.Position{Line: 2, Character: 1}, End: protocol.Position{Line: 2, Character: 3}},
		{Start: protocol.Position{Line: 1, Character: 3}, End: protocol.Position{Line: 1, Character: 4}},
		{Start: protocol.Position{Line: 1, Character: 2}, End: protocol.Position{Line: 1, Character: 4}},
		{Start: protocol.Position{Line: 1, Character: 2}, End: protocol.Position{Line: 1, Character: 4}},
		{Start: protocol.Position{Line: 1, Character: 2}, End: protocol.Position{Line: 1, Character: 5}},
	}
	got := deduplicateRanges(ranges)
	if len(got) != 4 || got[0].Start.Character != 2 || got[1].Start.Character != 2 || got[1].End.Character != 5 || got[2].Start.Character != 3 || got[3].Start.Line != 2 {
		t.Fatalf("deduplicated ranges = %#v", got)
	}
}

func TestWorkspaceNavigationTargetIdentityAndAnalysisFallback(t *testing.T) {
	target := workspaceNavigationTarget{match: workspace.SymbolMatch{Fact: workspace.SymbolFact{Path: "/tmp/target.vim", Name: "Value", SelectionRange: syntax.Span{Start: 4, End: 9}}, Source: "vim9script\nvar Value = 1\n"}}
	if !sameWorkspaceMemberTarget(target, target) {
		t.Fatal("equal workspace target rejected")
	}
	for _, other := range []workspaceNavigationTarget{
		{match: workspace.SymbolMatch{Fact: workspace.SymbolFact{Path: "/tmp/other.vim", Name: "Value", SelectionRange: syntax.Span{Start: 4, End: 9}}}},
		{match: workspace.SymbolMatch{Fact: workspace.SymbolFact{Path: "/tmp/target.vim", Name: "Other", SelectionRange: syntax.Span{Start: 4, End: 9}}}},
		{match: workspace.SymbolMatch{Fact: workspace.SymbolFact{Path: "/tmp/target.vim", Name: "Value", SelectionRange: syntax.Span{Start: 5, End: 9}}}},
	} {
		if sameWorkspaceMemberTarget(target, other) {
			t.Fatalf("different target accepted: %#v", other)
		}
	}
	server := New(nil, nil, nil)
	t.Cleanup(server.stopAnalysis)
	result, declaration := server.analyzeWorkspaceTarget(target)
	if result == nil || declaration != nil {
		t.Fatalf("unmatched target analysis = %#v, %#v", result, declaration)
	}
	target.match.Fact.SelectionRange = syntax.Span{Start: 15, End: 20}
	_, declaration = server.analyzeWorkspaceTarget(target)
	if declaration == nil || declaration.Name != "Value" {
		t.Fatalf("workspace declaration = %#v", declaration)
	}
}

// Call hierarchy callers are grouped by target and sorted by the caller's
// stable symbol location. This is observable to clients that render incoming
// calls as an ordered list.
func TestIncomingCallsUseStableCallerLocationOrder(t *testing.T) {
	root := t.TempDir()
	targetPath := writeWorkspaceFile(t, root, "target.vim", "vim9script\nexport def Target()\nenddef\n")
	callerPath := writeWorkspaceFile(t, root, "caller.vim", "vim9script\nimport './target.vim' as target\ndef First()\n  target.Target()\nenddef\ndef Second()\n  target.Target()\nenddef\n")
	instance := initializeWorkspaceServer(t, root)
	callerURI := canonicalTestURI(t, callerPath)
	targetURI := uri.File(targetPath)
	targetSource := "vim9script\nexport def Target()\nenddef\n"
	instance.documents.Open(targetURI.String(), 1, targetSource)

	items, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: targetURI},
			Position:     protocol.Position{Line: 1, Character: 12},
		},
	})
	if err != nil || len(items) != 1 || items[0].Name != "Target" {
		t.Fatalf("target hierarchy items = %#v, %v", items, err)
	}

	incoming, err := instance.IncomingCalls(context.Background(), &protocol.CallHierarchyIncomingCallsParams{Item: items[0]})
	if err != nil || len(incoming) != 2 {
		t.Fatalf("incoming calls = %#v, %v", incoming, err)
	}
	for index, want := range []struct {
		name string
		line uint32
	}{
		{name: "First", line: 2},
		{name: "Second", line: 5},
	} {
		call := incoming[index]
		if call.From.Name != want.name || call.From.URI != callerURI || call.From.SelectionRange.Start.Line != want.line {
			t.Errorf("incoming call %d = %#v, want %s at %s:%d", index, call, want.name, callerURI, want.line)
		}
		if len(call.FromRanges) != 1 || call.FromRanges[0] != navigationRange(want.line+1, 9, 15) {
			t.Errorf("incoming ranges %d = %#v", index, call.FromRanges)
		}
	}
}
