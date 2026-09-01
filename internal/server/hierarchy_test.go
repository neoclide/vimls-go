package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestTypeHierarchyCapabilitiesAndMethods(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	result, err := instance.Initialize(context.Background(), &protocol.InitializeParams{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Capabilities.TypeHierarchyProvider == nil || result.Capabilities.ImplementationProvider == nil || result.Capabilities.CallHierarchyProvider == nil {
		t.Fatalf("hierarchy capabilities = %#v", result.Capabilities)
	}
	encoded, err := protocol.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range []string{`"typeHierarchyProvider":true`, `"implementationProvider":true`, `"callHierarchyProvider":true`} {
		if !bytes.Contains(encoded, []byte(capability)) {
			t.Fatalf("initialize result omitted %s: %s", capability, encoded)
		}
	}
	for _, method := range []string{
		protocol.MethodTextDocumentImplementation,
		protocol.MethodTextDocumentPrepareCallHierarchy,
		protocol.MethodCallHierarchyIncomingCalls,
		protocol.MethodCallHierarchyOutgoingCalls,
		protocol.MethodTextDocumentPrepareTypeHierarchy,
		protocol.MethodTypeHierarchySupertypes,
		protocol.MethodTypeHierarchySubtypes,
	} {
		if !implementedMethod(method) {
			t.Errorf("method %q is not implemented", method)
		}
	}
}

func TestCallHierarchyGroupsIncomingAndOutgoingRanges(t *testing.T) {
	root := t.TempDir()
	source := "vim9script\ndef Target()\nenddef\ndef Other()\nenddef\ndef Caller()\n  Target()\n  Target()\n  Other()\nenddef\ndef Recursive()\n  Recursive()\nenddef\n"
	path := writeWorkspaceFile(t, root, "calls.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)

	caller, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 5, Character: 5},
	}})
	if err != nil || len(caller) != 1 || caller[0].Name != "Caller" || len(caller[0].Data) == 0 {
		t.Fatalf("caller prepare = %#v, %v", caller, err)
	}
	outgoing, err := instance.OutgoingCalls(context.Background(), &protocol.CallHierarchyOutgoingCallsParams{Item: caller[0]})
	if err != nil || len(outgoing) != 2 || outgoing[0].To.Name != "Target" || len(outgoing[0].FromRanges) != 2 || outgoing[1].To.Name != "Other" || len(outgoing[1].FromRanges) != 1 {
		t.Fatalf("outgoing calls = %#v, %v", outgoing, err)
	}

	target, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 6, Character: 4},
	}})
	if err != nil || len(target) != 1 || target[0].Name != "Target" {
		t.Fatalf("callee prepare = %#v, %v", target, err)
	}
	incoming, err := instance.IncomingCalls(context.Background(), &protocol.CallHierarchyIncomingCallsParams{Item: target[0]})
	if err != nil || len(incoming) != 1 || incoming[0].From.Name != "Caller" || len(incoming[0].FromRanges) != 2 {
		t.Fatalf("incoming calls = %#v, %v", incoming, err)
	}

	recursive, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 11, Character: 6},
	}})
	if err != nil || len(recursive) != 1 || recursive[0].Name != "Recursive" {
		t.Fatalf("recursive prepare = %#v, %v", recursive, err)
	}
	recursiveOutgoing, err := instance.OutgoingCalls(context.Background(), &protocol.CallHierarchyOutgoingCallsParams{Item: recursive[0]})
	if err != nil || len(recursiveOutgoing) != 1 || recursiveOutgoing[0].To.Name != "Recursive" {
		t.Fatalf("recursive outgoing = %#v, %v", recursiveOutgoing, err)
	}
}

func TestImplementationTypesAndCompatibleMemberProviders(t *testing.T) {
	root := t.TempDir()
	source := "vim9script\ninterface I\n  def Run(value: number): string\nendinterface\nclass Base implements I\n  def Run(value: number): string\n    return ''\n  enddef\nendclass\nclass Inherited extends Base\nendclass\nclass Override extends Base\n  def Run(value: number): string\n    return ''\n  enddef\nendclass\nclass Bad implements I\n  def Run(value: string): string\n    return ''\n  enddef\nendclass\n"
	path := writeWorkspaceFile(t, root, "implementations.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)

	typeResult, err := instance.Implementation(context.Background(), &protocol.ImplementationParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 10},
	}})
	typeLocations, ok := typeResult.(protocol.LocationSlice)
	if err != nil || !ok || len(typeLocations) != 4 {
		t.Fatalf("interface implementations = %#v, %v", typeResult, err)
	}

	memberResult, err := instance.Implementation(context.Background(), &protocol.ImplementationParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 6},
	}})
	memberLocations, ok := memberResult.(protocol.LocationSlice)
	if err != nil || !ok || len(memberLocations) != 2 || memberLocations[0].Range.Start.Line != 5 || memberLocations[1].Range.Start.Line != 12 {
		t.Fatalf("member implementations = %#v, %v", memberResult, err)
	}
	overrideResult, err := instance.Implementation(context.Background(), &protocol.ImplementationParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 5, Character: 6},
	}})
	overrides, ok := overrideResult.(protocol.LocationSlice)
	if err != nil || !ok || len(overrides) != 1 || overrides[0].Range.Start.Line != 12 {
		t.Fatalf("concrete method overrides = %#v, %v", overrideResult, err)
	}
}

func TestImplementationAbstractClassAndMemberAccess(t *testing.T) {
	root := t.TempDir()
	source := "vim9script\nabstract class Base\n  abstract def Run()\nendclass\nabstract class Middle extends Base\nendclass\nclass Child extends Middle\n  def Run()\n  enddef\nendclass\ninterface Values\n  var value: number\nendinterface\nclass Good implements Values\n  var value = 1\nendclass\nclass Bad implements Values\n  public var value = 1\nendclass\nclass Static implements Values\n  static var value = 1\nendclass\n"
	path := writeWorkspaceFile(t, root, "abstract.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)

	baseResult, err := instance.Implementation(context.Background(), &protocol.ImplementationParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 17},
	}})
	baseLocations := baseResult.(protocol.LocationSlice)
	if err != nil || len(baseLocations) != 1 || baseLocations[0].Range.Start.Line != 6 {
		t.Fatalf("abstract class implementations = %#v, %v", baseResult, err)
	}
	abstractMember, err := instance.Implementation(context.Background(), &protocol.ImplementationParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 15},
	}})
	abstractLocations := abstractMember.(protocol.LocationSlice)
	if err != nil || len(abstractLocations) != 1 || abstractLocations[0].Range.Start.Line != 7 {
		t.Fatalf("abstract member implementations = %#v, %v", abstractMember, err)
	}
	valueResult, err := instance.Implementation(context.Background(), &protocol.ImplementationParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 11, Character: 7},
	}})
	valueLocations := valueResult.(protocol.LocationSlice)
	if err != nil || len(valueLocations) != 1 || valueLocations[0].Range.Start.Line != 14 {
		t.Fatalf("member access implementations = %#v, %v", valueResult, err)
	}
}

func TestImplementationDoesNotReturnSubinterfaceRequirement(t *testing.T) {
	root := t.TempDir()
	source := "vim9script\ninterface Root\n  def Run()\nendinterface\ninterface Child extends Root\n  def Run()\nendinterface\nclass Concrete implements Child\n  def Run()\n  enddef\nendclass\n"
	path := writeWorkspaceFile(t, root, "subinterface.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)
	result, err := instance.Implementation(context.Background(), &protocol.ImplementationParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 6},
	}})
	locations := result.(protocol.LocationSlice)
	if err != nil || len(locations) != 1 || locations[0].Range.Start.Line != 8 {
		t.Fatalf("subinterface member implementations = %#v, %v", result, err)
	}
}

func TestTypeHierarchyDirectRelationshipsAndParentPrepare(t *testing.T) {
	root := t.TempDir()
	source := "vim9script\ninterface Root\nendinterface\ninterface Child extends Root\nendinterface\nclass Base\nendclass\nclass Item extends Base implements Root\nendclass\n"
	path := writeWorkspaceFile(t, root, "types.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)

	rootItems, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 12},
	}})
	if err != nil || len(rootItems) != 1 || rootItems[0].Name != "Root" || len(rootItems[0].Data) == 0 {
		t.Fatalf("root prepare = %#v, %v", rootItems, err)
	}
	subtypes, err := instance.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: rootItems[0]})
	if err != nil || len(subtypes) != 2 || subtypes[0].Name != "Child" || subtypes[1].Name != "Item" {
		t.Fatalf("root subtypes = %#v, %v", subtypes, err)
	}

	itemItems, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 7, Character: 7},
	}})
	if err != nil || len(itemItems) != 1 || itemItems[0].Name != "Item" {
		t.Fatalf("item prepare = %#v, %v", itemItems, err)
	}
	supertypes, err := instance.Supertypes(context.Background(), &protocol.TypeHierarchySupertypesParams{Item: itemItems[0]})
	if err != nil || len(supertypes) != 2 || supertypes[0].Name != "Root" || supertypes[1].Name != "Base" {
		t.Fatalf("item supertypes = %#v, %v", supertypes, err)
	}

	parentItems, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 7, Character: 21},
	}})
	if err != nil || len(parentItems) != 1 || parentItems[0].Name != "Base" {
		t.Fatalf("parent prepare = %#v, %v", parentItems, err)
	}
}

func TestTypeHierarchyResolvesTypeAliasChains(t *testing.T) {
	root := t.TempDir()
	source := "vim9script\nclass Base\nendclass\ntype Alias = Base\ntype Alias2 = Alias\nclass Child extends Alias2\nendclass\ntype LoopA = LoopB\ntype LoopB = LoopA\n"
	path := writeWorkspaceFile(t, root, "aliases.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)

	base, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 7},
	}})
	if err != nil || len(base) != 1 || base[0].Name != "Base" {
		t.Fatalf("base prepare = %#v, %v", base, err)
	}
	subtypes, err := instance.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: base[0]})
	if err != nil || len(subtypes) != 1 || subtypes[0].Name != "Child" {
		t.Fatalf("alias subtypes = %#v, %v", subtypes, err)
	}
	alias, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 3, Character: 7},
	}})
	if err != nil || len(alias) != 1 || alias[0].Name != "Base" {
		t.Fatalf("alias prepare = %#v, %v", alias, err)
	}
	child, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 5, Character: 7},
	}})
	if err != nil || len(child) != 1 {
		t.Fatalf("child prepare = %#v, %v", child, err)
	}
	supertypes, err := instance.Supertypes(context.Background(), &protocol.TypeHierarchySupertypesParams{Item: child[0]})
	if err != nil || len(supertypes) != 1 || supertypes[0].Name != "Base" {
		t.Fatalf("alias supertypes = %#v, %v", supertypes, err)
	}
	cycle, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 7, Character: 6},
	}})
	if err != nil || len(cycle) != 0 {
		t.Fatalf("cyclic alias prepare = %#v, %v", cycle, err)
	}
}

func TestTypeHierarchyResolvesImportedTypeAlias(t *testing.T) {
	root := t.TempDir()
	libSource := "vim9script\nexport class Base\nendclass\n"
	mainSource := "vim9script\nimport './lib.vim' as lib\ntype Alias = lib.Base\nclass Child extends Alias\nendclass\n"
	libPath := writeWorkspaceFile(t, root, "lib.vim", libSource)
	mainPath := writeWorkspaceFile(t, root, "main.vim", mainSource)
	instance := initializeWorkspaceServer(t, root)
	libURI, mainURI := canonicalTestURI(t, libPath), canonicalTestURI(t, mainPath)
	instance.documents.Open(libURI.String(), 1, libSource)
	instance.documents.Open(mainURI.String(), 1, mainSource)
	base, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: libURI}, Position: protocol.Position{Line: 1, Character: 13},
	}})
	if err != nil || len(base) != 1 {
		t.Fatalf("base prepare = %#v, %v", base, err)
	}
	subtypes, err := instance.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: base[0]})
	if err != nil || len(subtypes) != 1 || subtypes[0].Name != "Child" || subtypes[0].URI != mainURI {
		t.Fatalf("imported alias subtypes = %#v, %v", subtypes, err)
	}
}

func TestTypeHierarchyAndImplementationIncludeEnumImplementors(t *testing.T) {
	root := t.TempDir()
	source := "vim9script\ninterface Named\nendinterface\nenum Value implements Named\n  One\nendenum\n"
	path := writeWorkspaceFile(t, root, "enum.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)
	items, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 11},
	}})
	if err != nil || len(items) != 1 {
		t.Fatalf("prepare = %#v, %v", items, err)
	}
	subtypes, err := instance.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: items[0]})
	if err != nil || len(subtypes) != 1 || subtypes[0].Name != "Value" || subtypes[0].Kind != protocol.SymbolKindEnum {
		t.Fatalf("enum subtypes = %#v, %v", subtypes, err)
	}
	result, err := instance.Implementation(context.Background(), &protocol.ImplementationParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 11},
	}})
	locations := result.(protocol.LocationSlice)
	if err != nil || len(locations) != 1 || locations[0].Range.Start.Line != 3 {
		t.Fatalf("enum implementations = %#v, %v", result, err)
	}
}

func TestCallHierarchyMethodsConstructorsAndDeferredPrepare(t *testing.T) {
	root := t.TempDir()
	source := "vim9script\nclass Item\n  def new()\n  enddef\n  def Run()\n  enddef\nendclass\ndef Caller()\n  var item = Item.new()\n  item.Run()\n  var Callback = () => item.Run()\nenddef\n"
	path := writeWorkspaceFile(t, root, "members.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)
	caller, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 7, Character: 6},
	}})
	if err != nil || len(caller) != 1 || caller[0].Name != "Caller" {
		t.Fatalf("caller prepare = %#v, %v", caller, err)
	}
	outgoing, err := instance.OutgoingCalls(context.Background(), &protocol.CallHierarchyOutgoingCallsParams{Item: caller[0]})
	if err != nil || len(outgoing) != 2 || outgoing[0].To.Name != "new" || outgoing[0].To.Kind != protocol.SymbolKindConstructor || outgoing[1].To.Name != "Run" {
		t.Fatalf("member outgoing = %#v, %v", outgoing, err)
	}
	constructor, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 8, Character: 20},
	}})
	if err != nil || len(constructor) != 1 || constructor[0].Kind != protocol.SymbolKindConstructor {
		t.Fatalf("constructor prepare = %#v, %v", constructor, err)
	}
	incoming, err := instance.IncomingCalls(context.Background(), &protocol.CallHierarchyIncomingCallsParams{Item: constructor[0]})
	if err != nil || len(incoming) != 1 || incoming[0].From.Name != "Caller" {
		t.Fatalf("constructor incoming = %#v, %v", incoming, err)
	}
	deferred, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 10, Character: 29},
	}})
	if err != nil || len(deferred) != 0 {
		t.Fatalf("lambda prepare = %#v, %v", deferred, err)
	}
}

func TestCallHierarchyResolvesImportedAndExternalCallables(t *testing.T) {
	t.Run("imported members", func(t *testing.T) {
		root := t.TempDir()
		libSource := "vim9script\nexport class Service\n  def new()\n  enddef\n  static def Build()\n  enddef\n  def Run()\n  enddef\nendclass\n"
		mainSource := "vim9script\nimport './lib.vim' as lib\ndef Caller()\n  var service = lib.Service.new()\n  lib.Service.Build()\n  service.Run()\nenddef\n"
		libPath := writeWorkspaceFile(t, root, "lib.vim", libSource)
		mainPath := writeWorkspaceFile(t, root, "main.vim", mainSource)
		instance := initializeWorkspaceServer(t, root)
		libURI, mainURI := canonicalTestURI(t, libPath), canonicalTestURI(t, mainPath)
		instance.documents.Open(libURI.String(), 1, libSource)
		instance.documents.Open(mainURI.String(), 1, mainSource)
		caller, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: mainURI}, Position: protocol.Position{Line: 2, Character: 6},
		}})
		if err != nil || len(caller) != 1 {
			t.Fatalf("prepare caller = %#v, %v", caller, err)
		}
		outgoing, err := instance.OutgoingCalls(context.Background(), &protocol.CallHierarchyOutgoingCallsParams{Item: caller[0]})
		if err != nil || len(outgoing) != 3 || outgoing[0].To.Name != "new" || outgoing[1].To.Name != "Build" || outgoing[2].To.Name != "Run" {
			t.Fatalf("imported member outgoing = %#v, %v", outgoing, err)
		}
		build, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: mainURI}, Position: protocol.Position{Line: 4, Character: 15},
		}})
		if err != nil || len(build) != 1 || build[0].Name != "Build" || build[0].URI != libURI {
			t.Fatalf("prepare imported method = %#v, %v", build, err)
		}
		incoming, err := instance.IncomingCalls(context.Background(), &protocol.CallHierarchyIncomingCallsParams{Item: build[0]})
		if err != nil || len(incoming) != 1 || incoming[0].From.Name != "Caller" || incoming[0].From.URI != mainURI {
			t.Fatalf("imported method incoming = %#v, %v", incoming, err)
		}
	})

	t.Run("legacy autoload", func(t *testing.T) {
		root := t.TempDir()
		autoloadSource := "function! api#Run()\nendfunction\n"
		callerSource := "function! Caller()\n  call api#Run()\nendfunction\n"
		autoloadPath := writeWorkspaceFile(t, root, "autoload/api.vim", autoloadSource)
		callerPath := writeWorkspaceFile(t, root, "plugin/caller.vim", callerSource)
		instance := initializeWorkspaceServer(t, root)
		autoloadURI, callerURI := canonicalTestURI(t, autoloadPath), canonicalTestURI(t, callerPath)
		instance.documents.Open(autoloadURI.String(), 1, autoloadSource)
		instance.documents.Open(callerURI.String(), 1, callerSource)
		callee, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: callerURI}, Position: protocol.Position{Line: 1, Character: 12},
		}})
		if err != nil || len(callee) != 1 || callee[0].Name != "api#Run" || callee[0].URI != autoloadURI {
			t.Fatalf("autoload prepare = %#v, %v", callee, err)
		}
		incoming, err := instance.IncomingCalls(context.Background(), &protocol.CallHierarchyIncomingCallsParams{Item: callee[0]})
		if err != nil || len(incoming) != 1 || incoming[0].From.Name != "Caller" || incoming[0].From.URI != callerURI {
			t.Fatalf("autoload incoming = %#v, %v", incoming, err)
		}
	})

	t.Run("legacy global", func(t *testing.T) {
		root := t.TempDir()
		targetSource := "function! GlobalTarget()\nendfunction\n"
		callerSource := "function! Caller()\n  call GlobalTarget()\nendfunction\n"
		targetPath := writeWorkspaceFile(t, root, "plugin/target.vim", targetSource)
		callerPath := writeWorkspaceFile(t, root, "plugin/caller.vim", callerSource)
		instance := initializeWorkspaceServer(t, root)
		targetURI, callerURI := canonicalTestURI(t, targetPath), canonicalTestURI(t, callerPath)
		instance.documents.Open(targetURI.String(), 1, targetSource)
		instance.documents.Open(callerURI.String(), 1, callerSource)
		callee, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: callerURI}, Position: protocol.Position{Line: 1, Character: 11},
		}})
		if err != nil || len(callee) != 1 || callee[0].Name != "GlobalTarget" || callee[0].URI != targetURI {
			t.Fatalf("global prepare = %#v, %v", callee, err)
		}
		incoming, err := instance.IncomingCalls(context.Background(), &protocol.CallHierarchyIncomingCallsParams{Item: callee[0]})
		if err != nil || len(incoming) != 1 || incoming[0].From.Name != "Caller" || incoming[0].From.URI != callerURI {
			t.Fatalf("global incoming = %#v, %v", incoming, err)
		}
	})
}

func TestTypeHierarchyRejectsTamperedItemData(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nclass Item\nendclass\n")
	items, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 7},
	}})
	if err != nil || len(items) != 1 {
		t.Fatalf("prepare = %#v, %v", items, err)
	}
	item := items[0]
	item.URI = uri.MustParse("file:///tampered.vim")
	got, err := instance.Supertypes(context.Background(), &protocol.TypeHierarchySupertypesParams{Item: item})
	if err != nil || len(got) != 0 {
		t.Fatalf("tampered item = %#v, %v", got, err)
	}
}

func TestHierarchyResolvesImportedTypesImplementationsAndCalls(t *testing.T) {
	root := t.TempDir()
	libSource := "vim9script\nexport interface I\n  def Run(value: number): string\nendinterface\nexport def Target()\nenddef\n"
	mainSource := "vim9script\nimport './lib.vim' as lib\nclass C implements lib.I\n  def Run(value: number): string\n    return ''\n  enddef\nendclass\ndef Caller()\n  lib.Target()\nenddef\n"
	libPath := writeWorkspaceFile(t, root, "lib.vim", libSource)
	mainPath := writeWorkspaceFile(t, root, "main.vim", mainSource)
	instance := initializeWorkspaceServer(t, root)
	mainURI := canonicalTestURI(t, mainPath)
	libURI := canonicalTestURI(t, libPath)
	instance.documents.Open(mainURI.String(), 1, mainSource)
	instance.documents.Open(libURI.String(), 1, libSource)

	parent, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: mainURI}, Position: protocol.Position{Line: 2, Character: 23},
	}})
	if err != nil || len(parent) != 1 || parent[0].Name != "I" || parent[0].URI != libURI {
		t.Fatalf("imported parent prepare = %#v, %v", parent, err)
	}
	subtypes, err := instance.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: parent[0]})
	if err != nil || len(subtypes) != 1 || subtypes[0].Name != "C" || subtypes[0].URI != mainURI {
		t.Fatalf("imported subtypes = %#v, %v", subtypes, err)
	}
	implementations, err := instance.Implementation(context.Background(), &protocol.ImplementationParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: libURI}, Position: protocol.Position{Line: 2, Character: 7},
	}})
	locations := implementations.(protocol.LocationSlice)
	if err != nil || len(locations) != 1 || locations[0].URI != mainURI || locations[0].Range.Start.Line != 3 {
		t.Fatalf("imported member implementations = %#v, %v", implementations, err)
	}

	callee, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: mainURI}, Position: protocol.Position{Line: 8, Character: 8},
	}})
	if err != nil || len(callee) != 1 || callee[0].Name != "Target" || callee[0].URI != libURI {
		t.Fatalf("imported call prepare = %#v, %v", callee, err)
	}
	incoming, err := instance.IncomingCalls(context.Background(), &protocol.CallHierarchyIncomingCallsParams{Item: callee[0]})
	if err != nil || len(incoming) != 1 || incoming[0].From.Name != "Caller" || incoming[0].From.URI != mainURI {
		t.Fatalf("imported incoming calls = %#v, %v", incoming, err)
	}
}

func TestHierarchyOpenOverlayReplacesIndexedRelations(t *testing.T) {
	root := t.TempDir()
	rootPath := writeWorkspaceFile(t, root, "root.vim", "vim9script\nexport interface Root\nendinterface\n")
	childPath := writeWorkspaceFile(t, root, "child.vim", "vim9script\nimport './root.vim' as root\nclass Old implements root.Root\nendclass\n")
	instance := initializeWorkspaceServer(t, root)
	rootURI := canonicalTestURI(t, rootPath)
	childURI := canonicalTestURI(t, childPath)
	rootSource := "vim9script\nexport interface Root\nendinterface\n"
	instance.documents.Open(rootURI.String(), 1, rootSource)
	instance.documents.Open(childURI.String(), 1, "vim9script\nimport './root.vim' as root\nclass New implements root.Root\nendclass\n")
	rootItems, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: rootURI}, Position: protocol.Position{Line: 1, Character: 18},
	}})
	if err != nil || len(rootItems) != 1 {
		t.Fatalf("root prepare = %#v, %v", rootItems, err)
	}
	subtypes, err := instance.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: rootItems[0]})
	if err != nil || len(subtypes) != 1 || subtypes[0].Name != "New" {
		t.Fatalf("overlay subtypes = %#v, %v", subtypes, err)
	}
}

func TestHierarchyItemDetectsChangedContent(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nclass Item\nendclass\n")
	items, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 7},
	}})
	if err != nil || len(items) != 1 {
		t.Fatalf("prepare = %#v, %v", items, err)
	}
	instance.documents.Open(documentURI.String(), 2, "vim9script\nclass Changed\nendclass\n")
	_, err = instance.Supertypes(context.Background(), &protocol.TypeHierarchySupertypesParams{Item: items[0]})
	if !errors.Is(err, protocol.ErrContentModified) {
		t.Fatalf("changed item error = %v", err)
	}
}

func TestHierarchyReverseQueryFailsWhenRelationshipIndexIsIncomplete(t *testing.T) {
	root := t.TempDir()
	source := "vim9script\ninterface Root\nendinterface\nclass One implements Root\nendclass\nclass Two implements Root\nendclass\n"
	path := writeWorkspaceFile(t, root, "types.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)
	items, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 11},
	}})
	if err != nil || len(items) != 1 {
		t.Fatalf("prepare = %#v, %v", items, err)
	}
	limited := workspace.NewIndex(maxWorkspaceFiles, maxIndexBytes, 1, 10)
	if err := limited.Replace(path, syntax.Parse(source)); err != nil {
		t.Fatal(err)
	}
	limited.SetComplete(true)
	instance.workspaceMu.Lock()
	instance.workspaceIndex = limited
	instance.workspaceMu.Unlock()
	_, err = instance.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: items[0]})
	var rpcError *jsonrpc2.Error
	if !errors.As(err, &rpcError) || rpcError.Code != jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed) {
		t.Fatalf("incomplete index error = %#v", err)
	}
}

func TestHierarchyResultLimitsCancellationAndWorkspaceRetry(t *testing.T) {
	root := t.TempDir()
	source := "vim9script\ninterface I\nendinterface\nclass A implements I\nendclass\nclass B implements I\nendclass\ndef First()\nenddef\ndef Second()\nenddef\ndef Caller()\n  First()\n  Second()\nenddef\n"
	path := writeWorkspaceFile(t, root, "limits.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)
	typeItems, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 10},
	}})
	if err != nil || len(typeItems) != 1 {
		t.Fatalf("type prepare = %#v, %v", typeItems, err)
	}
	callerItems, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 11, Character: 6},
	}})
	if err != nil || len(callerItems) != 1 {
		t.Fatalf("call prepare = %#v, %v", callerItems, err)
	}
	requestFailed := func(err error) bool {
		var rpcError *jsonrpc2.Error
		return errors.As(err, &rpcError) && rpcError.Code == jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed)
	}
	instance.hierarchyLimit = 1
	if _, err := instance.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: typeItems[0]}); !requestFailed(err) {
		t.Fatalf("subtype limit error = %#v", err)
	}
	if _, err := instance.Implementation(context.Background(), &protocol.ImplementationParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 10},
	}}); !requestFailed(err) {
		t.Fatalf("implementation limit error = %#v", err)
	}
	if _, err := instance.OutgoingCalls(context.Background(), &protocol.CallHierarchyOutgoingCallsParams{Item: callerItems[0]}); !requestFailed(err) {
		t.Fatalf("call limit error = %#v", err)
	}

	instance.hierarchyLimit = maxHierarchyResults
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := instance.Subtypes(canceled, &protocol.TypeHierarchySubtypesParams{Item: typeItems[0]}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Fatalf("canceled subtype error = %v", err)
	}

	checks := 0
	instance.beforeWorkspaceIdentityCheck = func() {
		checks++
		if checks == 1 {
			instance.workspaceMu.Lock()
			instance.workspaceRevision++
			instance.workspaceMu.Unlock()
		}
	}
	if result, err := instance.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: typeItems[0]}); err != nil || checks != 2 || len(result) != 2 {
		t.Fatalf("retry subtypes = %#v, checks=%d, error=%v", result, checks, err)
	}
	checks = 0
	instance.beforeWorkspaceIdentityCheck = func() {
		checks++
		instance.workspaceMu.Lock()
		instance.workspaceRevision++
		instance.workspaceMu.Unlock()
	}
	if _, err := instance.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: typeItems[0]}); !errors.Is(err, protocol.ErrContentModified) || checks != 2 {
		t.Fatalf("twice stale subtype checks=%d error=%v", checks, err)
	}
}

func TestCallHierarchyOpenOverlayReplacesIndexedCalls(t *testing.T) {
	root := t.TempDir()
	disk := "vim9script\ndef Old()\nenddef\ndef New()\nenddef\ndef Caller()\n  Old()\nenddef\n"
	open := "vim9script\ndef Old()\nenddef\ndef New()\nenddef\ndef Caller()\n  New()\nenddef\n"
	path := writeWorkspaceFile(t, root, "calls.vim", disk)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, open)
	caller, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 5, Character: 6},
	}})
	if err != nil || len(caller) != 1 {
		t.Fatalf("prepare = %#v, %v", caller, err)
	}
	outgoing, err := instance.OutgoingCalls(context.Background(), &protocol.CallHierarchyOutgoingCallsParams{Item: caller[0]})
	if err != nil || len(outgoing) != 1 || outgoing[0].To.Name != "New" {
		t.Fatalf("overlay outgoing = %#v, %v", outgoing, err)
	}
}

func TestCallHierarchyRangesUseNegotiatedEncoding(t *testing.T) {
	source := "vim9script\r\ndef Target()\r\nenddef\r\ndef Caller()\r\n  echo '😀é' | Target()\r\nenddef\r\n"
	callOffset := strings.LastIndex(source, "Target()")
	for _, encoding := range []text.Encoding{text.UTF8, text.UTF16, text.UTF32} {
		t.Run(string(encoding), func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, encoding, source)
			caller, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 3, Character: 6},
			}})
			if err != nil || len(caller) != 1 {
				t.Fatalf("prepare = %#v, %v", caller, err)
			}
			outgoing, err := instance.OutgoingCalls(context.Background(), &protocol.CallHierarchyOutgoingCallsParams{Item: caller[0]})
			if err != nil || len(outgoing) != 1 || len(outgoing[0].FromRanges) != 1 {
				t.Fatalf("outgoing = %#v, %v", outgoing, err)
			}
			snapshot := text.NewSnapshot(documentURI.String(), 1, nil, source)
			position, err := snapshot.Position(callOffset, encoding)
			if err != nil {
				t.Fatal(err)
			}
			want := protocol.Position{Line: uint32(position.Line), Character: uint32(position.Character)}
			if outgoing[0].FromRanges[0].Start != want {
				t.Fatalf("from range start = %#v, want %#v", outgoing[0].FromRanges[0].Start, want)
			}
		})
	}
}

func TestCallHierarchyInterfaceReceiverTargetsInterfaceMethod(t *testing.T) {
	root := t.TempDir()
	source := "vim9script\ninterface I\n  def Run()\nendinterface\nclass C implements I\n  def Run()\n  enddef\nendclass\ndef Caller(value: I)\n  value.Run()\nenddef\n"
	path := writeWorkspaceFile(t, root, "interface-call.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)
	caller, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 8, Character: 6},
	}})
	if err != nil || len(caller) != 1 {
		t.Fatalf("caller prepare = %#v, %v", caller, err)
	}
	outgoing, err := instance.OutgoingCalls(context.Background(), &protocol.CallHierarchyOutgoingCallsParams{Item: caller[0]})
	if err != nil || len(outgoing) != 1 || outgoing[0].To.Name != "Run" || outgoing[0].To.SelectionRange.Start.Line != 2 {
		t.Fatalf("interface outgoing = %#v, %v", outgoing, err)
	}
	interfaceMethod, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 9, Character: 9},
	}})
	if err != nil || len(interfaceMethod) != 1 || interfaceMethod[0].SelectionRange.Start.Line != 2 {
		t.Fatalf("interface call prepare = %#v, %v", interfaceMethod, err)
	}
}

func TestCallHierarchyExcludesDynamicCalls(t *testing.T) {
	root := t.TempDir()
	source := "vim9script\ndef Target()\nenddef\ndef Caller()\n  var Fn = function('Target')\n  Fn()\n  var Dict = {Run: function('Target')}\n  Dict.Run()\n  execute('Target()')\nenddef\n"
	path := writeWorkspaceFile(t, root, "dynamic.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)
	caller, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 3, Character: 6},
	}})
	if err != nil || len(caller) != 1 || caller[0].Name != "Caller" {
		t.Fatalf("prepare = %#v, %v", caller, err)
	}
	outgoing, err := instance.OutgoingCalls(context.Background(), &protocol.CallHierarchyOutgoingCallsParams{Item: caller[0]})
	if err != nil || len(outgoing) != 0 {
		t.Fatalf("dynamic outgoing = %#v, %v", outgoing, err)
	}
}

func TestHierarchyReverseQueriesDoNotMixSameNamesAcrossFiles(t *testing.T) {
	root := t.TempDir()
	libSource := "vim9script\nexport interface I\nendinterface\nexport def Target()\nenddef\n"
	firstPath := writeWorkspaceFile(t, root, "first.vim", libSource)
	secondPath := writeWorkspaceFile(t, root, "second.vim", libSource)
	mainSource := "vim9script\nimport './first.vim' as lib\nclass C implements lib.I\nendclass\ndef Caller()\n  lib.Target()\nenddef\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", mainSource)
	instance := initializeWorkspaceServer(t, root)
	firstURI, secondURI, mainURI := canonicalTestURI(t, firstPath), canonicalTestURI(t, secondPath), canonicalTestURI(t, mainPath)
	instance.documents.Open(firstURI.String(), 1, libSource)
	instance.documents.Open(secondURI.String(), 1, libSource)
	instance.documents.Open(mainURI.String(), 1, mainSource)

	prepareType := func(documentURI uri.URI) protocol.TypeHierarchyItem {
		items, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 17},
		}})
		if err != nil || len(items) != 1 {
			t.Fatalf("type prepare %s = %#v, %v", documentURI, items, err)
		}
		return items[0]
	}
	firstSubtypes, err := instance.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: prepareType(firstURI)})
	if err != nil || len(firstSubtypes) != 1 || firstSubtypes[0].Name != "C" {
		t.Fatalf("first subtypes = %#v, %v", firstSubtypes, err)
	}
	secondSubtypes, err := instance.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: prepareType(secondURI)})
	if err != nil || len(secondSubtypes) != 0 {
		t.Fatalf("second subtypes = %#v, %v", secondSubtypes, err)
	}

	prepareCall := func(documentURI uri.URI) protocol.CallHierarchyItem {
		items, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 3, Character: 11},
		}})
		if err != nil || len(items) != 1 {
			t.Fatalf("call prepare %s = %#v, %v", documentURI, items, err)
		}
		return items[0]
	}
	firstIncoming, err := instance.IncomingCalls(context.Background(), &protocol.CallHierarchyIncomingCallsParams{Item: prepareCall(firstURI)})
	if err != nil || len(firstIncoming) != 1 || firstIncoming[0].From.Name != "Caller" {
		t.Fatalf("first incoming = %#v, %v", firstIncoming, err)
	}
	secondIncoming, err := instance.IncomingCalls(context.Background(), &protocol.CallHierarchyIncomingCallsParams{Item: prepareCall(secondURI)})
	if err != nil || len(secondIncoming) != 0 {
		t.Fatalf("second incoming = %#v, %v", secondIncoming, err)
	}
}

func TestHierarchyHelperBoundaries(t *testing.T) {
	span := func(start, end int) syntax.Span { return syntax.Span{Start: start, End: end} }
	for _, test := range []struct {
		child  analysis.SymbolKind
		parent analysis.SymbolKind
		kind   analysis.TypeRelationKind
		want   bool
	}{
		{analysis.SymbolKindClass, analysis.SymbolKindClass, analysis.TypeRelationExtends, true},
		{analysis.SymbolKindInterface, analysis.SymbolKindInterface, analysis.TypeRelationExtends, true},
		{analysis.SymbolKindEnum, analysis.SymbolKindInterface, analysis.TypeRelationImplements, true},
		{analysis.SymbolKindClass, analysis.SymbolKindInterface, analysis.TypeRelationImplements, true},
		{analysis.SymbolKindClass, analysis.SymbolKindInterface, analysis.TypeRelationExtends, false},
		{analysis.SymbolKindInterface, analysis.SymbolKindClass, analysis.TypeRelationImplements, false},
	} {
		if got := validTypeRelation(test.child, test.parent, test.kind); got != test.want {
			t.Errorf("validTypeRelation(%q, %q, %d) = %v, want %v", test.child, test.parent, test.kind, got, test.want)
		}
	}
	if !implementationMemberCategory(analysis.SymbolKindVariable, analysis.SymbolKindConstant) || implementationMemberCategory(analysis.SymbolKindMethod, analysis.SymbolKindVariable) || !callHierarchyKind(analysis.SymbolKindConstructor) || callHierarchyKind(analysis.SymbolKindClass) || !typeHierarchyKind(analysis.SymbolKindTypeAlias) || aggregateHierarchyKind(analysis.SymbolKindTypeAlias) {
		t.Fatal("hierarchy kind classification is incorrect")
	}

	leftType := &syntax.Type{Kind: syntax.TypeGeneric, Name: "T", ArgumentCountKnown: true, Arguments: []*syntax.Type{{Kind: syntax.TypeNamed, Name: "number"}}, ReturnType: &syntax.Type{Kind: syntax.TypeNamed, Name: "string"}}
	rightType := &syntax.Type{Kind: syntax.TypeGeneric, Name: "U", ArgumentCountKnown: true, Arguments: []*syntax.Type{{Kind: syntax.TypeNamed, Name: "number"}}, ReturnType: &syntax.Type{Kind: syntax.TypeNamed, Name: "string"}}
	if !equalSyntaxType(leftType, rightType, map[string]string{"T": "U"}) || equalSyntaxType(leftType, rightType, nil) || !compatibleFunctionSignature(&syntax.Command{Function: &syntax.Function{TypeParameters: []syntax.TypeParameter{{Name: "T"}}, Parameters: []syntax.Parameter{{Type: leftType}}, ReturnType: leftType}}, &syntax.Command{Function: &syntax.Function{TypeParameters: []syntax.TypeParameter{{Name: "U"}}, Parameters: []syntax.Parameter{{Type: rightType}}, ReturnType: rightType}}) {
		t.Fatal("generic function signature comparison is incorrect")
	}
	if !equalValueType(analysis.ValueType{Name: "float"}, analysis.ValueType{Name: "number"}) || !equalValueType(analysis.ValueType{Name: analysis.ValueTypeAny}, analysis.ValueType{Name: "dict"}) || equalValueType(analysis.ValueType{}, analysis.ValueType{Name: "number"}) || equalValueType(analysis.ValueType{Name: "list", Arguments: []analysis.ValueType{{Name: "number"}}}, analysis.ValueType{Name: "list", Arguments: []analysis.ValueType{{Name: "string"}}}) {
		t.Fatal("value type comparison is incorrect")
	}

	ranges := deduplicateRanges([]protocol.Range{navigationRange(1, 0, 2), navigationRange(0, 0, 1), navigationRange(1, 0, 2)})
	if len(ranges) != 2 || ranges[0] != navigationRange(0, 0, 1) || ranges[1] != navigationRange(1, 0, 2) {
		t.Fatalf("deduplicated ranges = %#v", ranges)
	}
	source := "class Item\nendclass\n"
	symbol := hierarchySymbol{fact: workspace.SymbolFact{Path: "/tmp/item.vim", Name: "Item", Kind: analysis.SymbolKindClass, Range: span(0, 19), SelectionRange: span(6, 10)}, source: source}
	items, err := typeHierarchyItems([]hierarchySymbol{symbol, symbol}, text.UTF16, 1)
	if err != nil || len(items) != 1 || items[0].Name != "Item" || len(items[0].Data) == 0 {
		t.Fatalf("type hierarchy items = %#v, %v", items, err)
	}
	if _, err := typeHierarchyItems([]hierarchySymbol{symbol, {fact: workspace.SymbolFact{Path: "/tmp/other.vim", Name: "Other", Kind: analysis.SymbolKindClass, Range: span(0, 1), SelectionRange: span(0, 1)}, source: "x"}}, text.UTF16, 1); err == nil {
		t.Fatal("type hierarchy limit succeeded")
	}
	if item, ok := callHierarchyItem(hierarchySymbol{fact: workspace.SymbolFact{Path: "/tmp/bad.vim", Name: "Bad", Kind: analysis.SymbolKindFunction, Range: span(0, 1), SelectionRange: span(2, 3)}, source: "x"}, text.UTF16); ok || item.Name != "" {
		t.Fatalf("invalid call item = %#v", item)
	}
}
