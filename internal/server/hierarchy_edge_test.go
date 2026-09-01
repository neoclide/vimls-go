package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestHierarchyTargetResolutionAcrossDeclarationsReferencesAndImports(t *testing.T) {
	root := t.TempDir()
	library := "vim9script\nexport class Base\n  var Value = 1\n  def Run()\n  enddef\nendclass\n"
	writeWorkspaceFile(t, root, "lib.vim", library)
	source := "vim9script\nimport './lib.vim' as lib\ntype Alias = lib.Base\nclass Local extends lib.Base\n  var Value = 2\n  def Run()\n  enddef\nendclass\nvar item = Local.new()\necho item.Value\n"
	path := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)
	state := instance.captureWorkspaceNavigationState()
	file := syntax.Parse(source)

	for _, test := range []struct {
		name string
		text string
		want string
	}{
		{name: "local declaration", text: "Local extends", want: "Local"},
		{name: "imported parent", text: "lib.Base", want: "Base"},
		{name: "type alias", text: "Alias", want: "Base"},
		{name: "member expression", text: "item.Value", want: "Value"},
	} {
		offset := strings.Index(source, test.text)
		if offset < 0 {
			t.Fatalf("missing %s source", test.name)
		}
		offset += strings.LastIndex(test.text, ".") + 1
		target, ok := instance.implementationTargetAt(state, path, source, file, offset)
		if !ok || target.fact.Name != test.want {
			t.Fatalf("%s target = %#v, %v", test.name, target, ok)
		}
	}
	var local hierarchySymbol
	for _, test := range []struct {
		text string
		want string
	}{
		{text: "Local extends", want: "Local"},
		{text: "lib.Base", want: "Base"},
		{text: "Alias", want: "Base"},
	} {
		offset := strings.Index(source, test.text) + strings.LastIndex(test.text, ".") + 1
		target, ok := instance.typeHierarchyTargetAt(state, path, source, file, offset)
		if !ok || target.fact.Name != test.want {
			t.Fatalf("type target %q = %#v, %v", test.text, target, ok)
		}
		if test.want == "Local" {
			local = target
		}
	}
	if aliases, snapshots := instance.typeAliasCandidates(state, "Base"); len(aliases) != 1 || aliases[0].Fact.AliasName != "Alias" || len(snapshots) != 1 {
		t.Fatalf("open alias candidates = %#v, %#v", aliases, snapshots)
	}
	if relations := instance.typeRelationsForSymbol(state, local); len(relations) != 1 || relations[0].Fact.ParentName != "Base" {
		t.Fatalf("indexed type relations = %#v", relations)
	}
	if snapshot, ok := instance.documents.Snapshot(documentURI.String()); ok {
		local.snapshot = snapshot
		if relations := instance.typeRelationsForSymbol(state, local); len(relations) != 1 || relations[0].Fact.ParentName != "Base" {
			t.Fatalf("open type relations = %#v", relations)
		}
	} else {
		t.Fatal("open document snapshot missing")
	}
}

func TestHierarchyRejectsInvalidRequestsAndItems(t *testing.T) {
	instance := New(nil, nil, nil)
	state := workspaceNavigationSnapshot{}
	path := "/tmp/hierarchy-invalid.vim"
	source := "vim9script\nclass Item\nendclass\ndef Run()\nenddef\n"
	file := syntax.Parse(source)
	facts := workspace.CollectSymbolFacts(path, file)
	if len(facts) != 2 {
		t.Fatalf("facts = %#v", facts)
	}
	class := facts[0]
	symbol := hierarchySymbol{fact: class, source: source}
	item, ok := typeHierarchyItem(symbol, text.UTF16)
	if !ok {
		t.Fatal("type hierarchy item")
	}
	callItem, ok := callHierarchyItem(hierarchySymbol{fact: facts[1], source: source}, text.UTF16)
	if !ok {
		t.Fatal("call hierarchy item")
	}

	if target, _, err := instance.validateTypeHierarchyItem(item, state); target != nil || !errors.Is(err, protocol.ErrContentModified) {
		t.Fatalf("missing item source = %#v, %v", target, err)
	}
	if target, _, err := instance.validateCallHierarchyItem(callItem, state); target != nil || !errors.Is(err, protocol.ErrContentModified) {
		t.Fatalf("missing call source = %#v, %v", target, err)
	}
	wrongKind := item
	wrongKind.Kind = protocol.SymbolKindFunction
	if target, _, err := instance.validateTypeHierarchyItem(wrongKind, state); target != nil || !errors.Is(err, protocol.ErrContentModified) {
		t.Fatalf("missing source precedes item kind check = %#v, %v", target, err)
	}

	for _, raw := range []protocol.LSPAny{
		protocol.LSPAny(`{"v":2}`),
		protocol.LSPAny(`{"v":1,"u":"","k":"class","s":0,"e":1,"c":""}`),
	} {
		if target, malformed, err := instance.validateHierarchyData(raw, item.URI, item.Name, item.Kind, item.Range, item.SelectionRange, state, text.UTF16); target.fact.Name != "" || !malformed || err != nil {
			t.Fatalf("invalid raw data %s = %#v, malformed=%v, err=%v", raw, target, malformed, err)
		}
	}
	invalidURI := item
	invalidURI.URI = uri.URI("untitled:invalid")
	if target, malformed, err := instance.validateHierarchyData(invalidURI.Data, invalidURI.URI, invalidURI.Name, invalidURI.Kind, invalidURI.Range, invalidURI.SelectionRange, state, text.UTF16); target.fact.Name != "" || !malformed || err != nil {
		t.Fatalf("invalid URI = %#v, malformed=%v, err=%v", target, malformed, err)
	}

	if _, ok := instance.hierarchyTargetAtSpan(state, path, source, syntax.Span{Start: -1, End: 1}); ok {
		t.Fatal("negative target span resolved")
	}
	if _, ok := instance.hierarchyTargetAtSpan(state, path, source, syntax.Span{Start: 0, End: len(source) + 1}); ok {
		t.Fatal("oversized target span resolved")
	}
	if target, ok := instance.hierarchyTargetAtSpan(state, path, source, class.SelectionRange); !ok || target.fact.Name != "Item" {
		t.Fatalf("class target = %#v, %v", target, ok)
	}
	if _, ok := instance.resolveTypeName(state, path, source, file, syntax.Span{Start: 0, End: 0}); ok {
		t.Fatal("empty type name resolved")
	}
	unknownStart := strings.Index(source, "endclass")
	if _, ok := instance.resolveTypeName(state, path, source, file, syntax.Span{Start: unknownStart, End: unknownStart + len("endclass")}); ok {
		t.Fatal("unknown type name resolved")
	}
	if _, ok := instance.resolveAggregateAlias(state, hierarchySymbol{fact: facts[1], source: source}, nil); ok {
		t.Fatal("non-alias resolved as aggregate alias")
	}
	if _, ok := instance.hierarchySymbolForKey(state, class.Key()); ok {
		t.Fatal("symbol resolved without a workspace source")
	}
	if source, snapshot, ok := instance.hierarchySource(state, path); source != "" || snapshot != nil || ok {
		t.Fatalf("missing hierarchy source = %q, %#v, %v", source, snapshot, ok)
	}
	if instance.relationshipQueriesComplete(state) {
		t.Fatal("nil index reported complete")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if current, err := instance.hierarchyCurrent(cancelled, state); current || !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Fatalf("cancelled hierarchy current = %v, %v", current, err)
	}
	if _, ok := localHierarchyDeclaration(facts, nil); ok {
		t.Fatal("nil declaration resolved")
	}
	if _, ok := localHierarchySymbol(facts, nil); ok {
		t.Fatal("nil symbol resolved")
	}
	if hierarchyCommandHasModifier(nil, "public") {
		t.Fatal("nil command has a modifier")
	}
	if compatibleFunctionSignature(nil, nil) {
		t.Fatal("nil functions have compatible signatures")
	}
	if equalSyntaxType(&syntax.Type{Kind: syntax.TypeNamed, Name: "number"}, &syntax.Type{Kind: syntax.TypeNamed, Name: "string"}, nil) {
		t.Fatal("different syntax types compare equal")
	}
	if equalValueType(analysis.ValueType{Name: "number", Arguments: []analysis.ValueType{{Name: "number"}}}, analysis.ValueType{Name: "number"}) {
		t.Fatal("types with different argument counts compare equal")
	}
}
