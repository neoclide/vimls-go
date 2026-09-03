package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestTypeDefinitionCapabilityAndMethod(t *testing.T) {
	instance, _ := openNavigationDocument(t, text.UTF16, "vim9script\n")
	t.Cleanup(instance.stopAnalysis)
	result, err := instance.Initialize(context.Background(), &protocol.InitializeParams{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Capabilities.TypeDefinitionProvider == nil {
		t.Fatalf("type definition provider = %#v", result.Capabilities.TypeDefinitionProvider)
	}
	encoded, err := protocol.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"typeDefinitionProvider":true`)) {
		t.Fatalf("initialize result omitted typeDefinitionProvider: %s", encoded)
	}
	if !implementedMethod(protocol.MethodTextDocumentTypeDefinition) {
		t.Fatalf("method %q is not implemented", protocol.MethodTextDocumentTypeDefinition)
	}
}

func typeDefinitionAt(t *testing.T, instance *Server, documentURI uri.URI, line, character uint32) (protocol.LocationSlice, error) {
	t.Helper()
	result, err := instance.TypeDefinition(context.Background(), &protocol.TypeDefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Position:     protocol.Position{Line: line, Character: character},
		},
	})
	if err != nil {
		return nil, err
	}
	locations, ok := result.(protocol.LocationSlice)
	if !ok {
		t.Fatalf("type definition result type = %T", result)
	}
	return locations, nil
}

func wantSingleLocation(t *testing.T, locations protocol.LocationSlice, want protocol.Location) {
	t.Helper()
	if len(locations) != 1 {
		t.Fatalf("locations = %#v, want one", locations)
	}
	if locations[0] != want {
		t.Fatalf("location = %#v, want %#v", locations[0], want)
	}
}

func TestTypeDefinitionFromAnnotationDeclarationAndReference(t *testing.T) {
	source := "vim9script\nclass Point\nendclass\nvar p: Point = Point.new()\necho p\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	classLocation := protocol.Location{URI: documentURI, Range: navigationRange(1, 6, 11)}
	for name, position := range map[string]protocol.Position{
		"annotation":  {Line: 3, Character: 8},
		"declaration": {Line: 3, Character: 4},
		"reference":   {Line: 4, Character: 5},
	} {
		locations, err := typeDefinitionAt(t, instance, documentURI, position.Line, position.Character)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		wantSingleLocation(t, locations, classLocation)
	}
}

func TestTypeDefinitionGenericArgumentAndBuiltinContainer(t *testing.T) {
	source := "vim9script\nclass Item\nendclass\nvar items: list<Item> = []\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	itemLocation := protocol.Location{URI: documentURI, Range: navigationRange(1, 6, 10)}

	locations, err := typeDefinitionAt(t, instance, documentURI, 3, 17)
	if err != nil {
		t.Fatal(err)
	}
	wantSingleLocation(t, locations, itemLocation)

	// The cursor on the builtin container name itself has no type definition.
	locations, err = typeDefinitionAt(t, instance, documentURI, 3, 11)
	if err != nil {
		t.Fatal(err)
	}
	if len(locations) != 0 {
		t.Fatalf("builtin container locations = %#v", locations)
	}
}

func TestTypeDefinitionParameterAndReturnType(t *testing.T) {
	source := "vim9script\nclass Line\nendclass\ndef Render(mark: Line): Line\n  return mark\nenddef\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	lineLocation := protocol.Location{URI: documentURI, Range: navigationRange(1, 6, 10)}
	for name, position := range map[string]protocol.Position{
		"parameter":            {Line: 3, Character: 12},
		"parameter annotation": {Line: 3, Character: 18},
		"return type":          {Line: 3, Character: 24},
	} {
		locations, err := typeDefinitionAt(t, instance, documentURI, position.Line, position.Character)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		wantSingleLocation(t, locations, lineLocation)
	}
}

func TestTypeDefinitionTypeAliasAndImportedType(t *testing.T) {
	root := t.TempDir()
	libSource := "vim9script\nexport class Point\nendclass\n"
	mainSource := "vim9script\nimport './lib.vim' as lib\ntype Pair = lib.Point\nvar p: lib.Point\nvar q: Pair\n"
	libPath := writeWorkspaceFile(t, root, "lib.vim", libSource)
	mainPath := writeWorkspaceFile(t, root, "main.vim", mainSource)
	instance := initializeWorkspaceServer(t, root)
	libURI, mainURI := canonicalTestURI(t, libPath), canonicalTestURI(t, mainPath)
	instance.documents.Open(libURI.String(), 1, libSource)
	instance.documents.Open(mainURI.String(), 1, mainSource)

	importedLocation := protocol.Location{URI: libURI, Range: navigationRange(1, 13, 18)}
	locations, err := typeDefinitionAt(t, instance, mainURI, 3, 8)
	if err != nil {
		t.Fatal(err)
	}
	wantSingleLocation(t, locations, importedLocation)

	aliasLocation := protocol.Location{URI: mainURI, Range: navigationRange(2, 5, 9)}
	locations, err = typeDefinitionAt(t, instance, mainURI, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	wantSingleLocation(t, locations, aliasLocation)
}

func TestTypeDefinitionImportedNonTypeReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	libSource := "vim9script\nexport def Make(): number\n  return 42\nenddef\nexport var total = 100\n"
	mainSource := "vim9script\nimport './lib.vim' as lib\nvar value: lib.Make\nvar count: lib.total\n"
	libPath := writeWorkspaceFile(t, root, "lib.vim", libSource)
	mainPath := writeWorkspaceFile(t, root, "main.vim", mainSource)
	instance := initializeWorkspaceServer(t, root)
	libURI, mainURI := canonicalTestURI(t, libPath), canonicalTestURI(t, mainPath)
	instance.documents.Open(libURI.String(), 1, libSource)
	instance.documents.Open(mainURI.String(), 1, mainSource)

	// An imported function or variable in a type annotation must not resolve
	// as a type definition.
	for name, line := range map[string]uint32{
		"imported function": 2,
		"imported variable": 3,
	} {
		locations, err := typeDefinitionAt(t, instance, mainURI, line, 15)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(locations) != 0 {
			t.Fatalf("%s locations = %#v, want none", name, locations)
		}
	}
}

func TestTypeDefinitionExtendsClause(t *testing.T) {
	source := "vim9script\nclass Base\nendclass\nclass Child extends Base\nendclass\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	baseLocation := protocol.Location{URI: documentURI, Range: navigationRange(1, 6, 10)}
	locations, err := typeDefinitionAt(t, instance, documentURI, 3, 20)
	if err != nil {
		t.Fatal(err)
	}
	wantSingleLocation(t, locations, baseLocation)
}

func TestTypeDefinitionDynamicAndBuiltinValuesReturnEmpty(t *testing.T) {
	source := "vim9script\nvar anyValue: any = 1\nvar text = 'word'\nvar numbers = [1, 2]\necho anyValue\necho text\necho numbers\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	for name, position := range map[string]protocol.Position{
		"any annotation":   {Line: 1, Character: 14},
		"any declaration":  {Line: 1, Character: 4},
		"inferred string":  {Line: 2, Character: 4},
		"string reference": {Line: 5, Character: 5},
		"list value":       {Line: 3, Character: 4},
	} {
		locations, err := typeDefinitionAt(t, instance, documentURI, position.Line, position.Character)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(locations) != 0 {
			t.Fatalf("%s: locations = %#v, want none", name, locations)
		}
	}
}

func TestTypeDefinitionLegacyVariableReturnsEmpty(t *testing.T) {
	source := "let s:shape = Shape.new()\necho s:shape\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	for _, position := range []protocol.Position{{Line: 0, Character: 8}, {Line: 1, Character: 5}} {
		locations, err := typeDefinitionAt(t, instance, documentURI, position.Line, position.Character)
		if err != nil {
			t.Fatal(err)
		}
		if len(locations) != 0 {
			t.Fatalf("legacy locations at %d:%d = %#v, want none", position.Line, position.Character, locations)
		}
	}
}

func TestTypeDefinitionStaleWorkspaceRetriesOnce(t *testing.T) {
	source := "vim9script\nclass Point\nendclass\nvar p: Point\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)

	checks := 0
	instance.testHooks.beforeWorkspaceIdentityCheck = func() {
		checks++
		if checks == 1 {
			instance.workspaceMu.Lock()
			instance.workspaceRevision++
			instance.workspaceMu.Unlock()
		}
	}
	locations, err := typeDefinitionAt(t, instance, documentURI, 3, 7)
	if err != nil || checks != 2 {
		t.Fatalf("retry: locations = %#v, checks = %d, error = %v", locations, checks, err)
	}
	if len(locations) != 1 || locations[0].Range != navigationRange(1, 6, 11) {
		t.Fatalf("retry locations = %#v", locations)
	}

	checks = 0
	instance.testHooks.beforeWorkspaceIdentityCheck = func() {
		checks++
		instance.workspaceMu.Lock()
		instance.workspaceRevision++
		instance.workspaceMu.Unlock()
	}
	if _, err := typeDefinitionAt(t, instance, documentURI, 3, 7); !errors.Is(err, protocol.ErrContentModified) || checks != 2 {
		t.Fatalf("twice stale: checks = %d, error = %v", checks, err)
	}
}

func TestTypeDefinitionCanceledAndMissingDocument(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\n")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := instance.TypeDefinition(canceled, &protocol.TypeDefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Position:     protocol.Position{Line: 1, Character: 0},
		},
	}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Fatalf("canceled error = %v", err)
	}
	missing := uri.MustParse("file:///missing.vim")
	locations, err := typeDefinitionAt(t, instance, missing, 0, 0)
	if err != nil || len(locations) != 0 {
		t.Fatalf("missing document = %#v, %v", locations, err)
	}
	if _, err := instance.TypeDefinition(context.Background(), &protocol.TypeDefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Position:     protocol.Position{Line: 99, Character: 99},
		},
	}); err != nil {
		t.Fatalf("out of range position error = %v", err)
	}
}

func TestTypeDefinitionIgnoresOpaqueCommandText(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	if _, err := instance.TypeDefinition(context.Background(), &protocol.TypeDefinitionParams{}); err != nil {
		t.Fatalf("empty params error = %v", err)
	}
}
