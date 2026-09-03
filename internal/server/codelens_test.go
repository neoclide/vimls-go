package server

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/workspace"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestCodeLensCallableDeclarationsAndResolve(t *testing.T) {
	root := t.TempDir()
	source := "function Legacy()\nendfunction\nvim9script\ndef Modern()\nenddef\nclass Widget\n  def new()\n  enddef\n  def Run()\n  enddef\nendclass\n"
	path := writeWorkspaceFile(t, root, "main.vim", source)
	instance := New(nil, nil, io.Discard)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)
	lenses, err := instance.CodeLens(context.Background(), &protocol.CodeLensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil || len(lenses) != 4 {
		t.Fatalf("lenses = %#v, %v", lenses, err)
	}
	for _, lens := range lenses {
		data := decodeTestCodeLensData(t, lens)
		if data.Lens != codeLensReferences || lens.Command.Title != "" || lens.Range.Start.Line != lens.Range.End.Line {
			t.Fatalf("unresolved lens = %#v, data = %#v", lens, data)
		}
	}
}

func TestCodeLensResolveReferenceTitlesAndCommand(t *testing.T) {
	for name, test := range map[string]struct {
		source        string
		wantTitle     string
		wantLocations int
	}{
		"zero":   {"vim9script\ndef Target()\nenddef\n", "0 references", 0},
		"single": {"vim9script\ndef Target()\nenddef\ndef Use()\n  Target()\nenddef\n", "1 reference", 1},
		"plural": {"vim9script\ndef Target()\nenddef\ndef Use()\n  Target()\n  Target()\nenddef\n", "2 references", 2},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := writeWorkspaceFile(t, root, "main.vim", test.source)
			instance := New(nil, nil, io.Discard)
			documentURI := canonicalTestURI(t, path)
			instance.documents.Open(documentURI.String(), 1, test.source)
			lens := findCodeLens(t, instance, documentURI, "Target", codeLensReferences)
			resolved, err := instance.CodeLensResolve(context.Background(), &lens)
			if err != nil || resolved.Command.Title != test.wantTitle || resolved.Command.Command != "editor.action.showReferences" {
				t.Fatalf("resolved = %#v, %v", resolved, err)
			}
			if resolved.Command.Tooltip == nil || *resolved.Command.Tooltip != "Show references" {
				t.Fatalf("reference tooltip = %#v, want fixed %q independent of count", resolved.Command.Tooltip, "Show references")
			}
			if len(resolved.Command.Arguments) != 3 {
				t.Fatalf("command arguments = %#v", resolved.Command.Arguments)
			}
			var commandURI string
			var position protocol.Position
			var locations []protocol.Location
			if protocol.Unmarshal(resolved.Command.Arguments[0], &commandURI) != nil || protocol.Unmarshal(resolved.Command.Arguments[1], &position) != nil || protocol.Unmarshal(resolved.Command.Arguments[2], &locations) != nil {
				t.Fatalf("command arguments = %#v", resolved.Command.Arguments)
			}
			if commandURI != documentURI.String() || position != lens.Range.Start || len(locations) != test.wantLocations {
				t.Fatalf("command = %q, %#v, %#v", commandURI, position, locations)
			}
			for _, location := range locations {
				if location.Range == lens.Range {
					t.Fatalf("declaration included in references: %#v", locations)
				}
			}
		})
	}
}

func TestCodeLensResolveCrossFileReferencesInNavigationOrder(t *testing.T) {
	root := t.TempDir()
	libSource := "vim9script\nexport def Target()\nenddef\n"
	libPath := writeWorkspaceFile(t, root, "lib.vim", libSource)
	mainPath := writeWorkspaceFile(t, root, "main.vim", "vim9script\nimport './lib.vim' as lib\nlib.Target()\nlib.Target()\n")
	instance := initializeWorkspaceServer(t, root)
	libURI := canonicalTestURI(t, libPath)
	instance.documents.Open(libURI.String(), 1, libSource)
	lens := findCodeLens(t, instance, libURI, "Target", codeLensReferences)
	resolved, err := instance.CodeLensResolve(context.Background(), &lens)
	if err != nil {
		t.Fatal(err)
	}
	var locations []protocol.Location
	if err := protocol.Unmarshal(resolved.Command.Arguments[2], &locations); err != nil {
		t.Fatal(err)
	}
	want := []protocol.Location{
		{URI: canonicalTestURI(t, mainPath), Range: navigationRange(2, 4, 10)},
		{URI: canonicalTestURI(t, mainPath), Range: navigationRange(3, 4, 10)},
	}
	if len(locations) != len(want) {
		t.Fatalf("locations = %#v", locations)
	}
	for index := range want {
		if locations[index] != want[index] {
			t.Errorf("location %d = %#v, want %#v", index, locations[index], want[index])
		}
	}
}

func TestCodeLensImplementationEligibilityAndResolve(t *testing.T) {
	root := t.TempDir()
	source := "vim9script\nabstract class Base\n  abstract def Required()\nendclass\nclass Child extends Base\n  def Required()\n  enddef\n  def Concrete()\n  enddef\nendclass\ninterface Runnable\n  def Run()\nendinterface\nclass Runner implements Runnable\n  def Run()\n  enddef\nendclass\ndef TopLevel()\nenddef\n"
	path := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)
	lenses, err := instance.CodeLens(context.Background(), &protocol.CodeLensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	var implementations int
	for _, lens := range lenses {
		data := decodeTestCodeLensData(t, lens)
		if data.Lens == codeLensImplementations {
			implementations++
			if data.Kind != analysis.SymbolKindMethod {
				t.Fatalf("implementation lens kind = %#v", data)
			}
		}
	}
	if implementations != 2 {
		t.Fatalf("implementation lenses = %d, all = %#v", implementations, lenses)
	}
	for _, name := range []string{"Required", "Run"} {
		lens := findCodeLens(t, instance, documentURI, name, codeLensImplementations)
		resolved, err := instance.CodeLensResolve(context.Background(), &lens)
		if err != nil || resolved.Command.Title != "1 implementation" {
			t.Fatalf("%s resolve = %#v, %v", name, resolved, err)
		}
		if resolved.Command.Tooltip == nil || *resolved.Command.Tooltip != "Show implementations" {
			t.Fatalf("%s tooltip = %#v, want %q", name, resolved.Command.Tooltip, "Show implementations")
		}
	}
}

func TestCodeLensRejectsTamperingAndStaleDocuments(t *testing.T) {
	root := t.TempDir()
	source := "vim9script\ndef Target()\nenddef\n"
	path := writeWorkspaceFile(t, root, "main.vim", source)
	instance := New(nil, nil, io.Discard)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)
	lens := findCodeLens(t, instance, documentURI, "Target", codeLensReferences)

	tampered := lens
	tampered.Data = protocol.LSPAny([]byte(`{"v":1,"u":"file:///tmp/other.vim","c":"bad","k":"function","s":0,"e":1,"l":1}`))
	resolved, err := instance.CodeLensResolve(context.Background(), &tampered)
	if err != nil || resolved.Command.Title != "" {
		t.Fatalf("tampered resolve = %#v, %v", resolved, err)
	}
	for name, data := range map[string]protocol.LSPAny{
		"malformed": []byte(`{`),
		"oversized": make([]byte, maxCodeLensData+1),
	} {
		t.Run(name, func(t *testing.T) {
			invalid := lens
			invalid.Data = data
			resolved, err := instance.CodeLensResolve(context.Background(), &invalid)
			if err != nil || resolved.Command.Title != "" {
				t.Fatalf("resolve = %#v, %v", resolved, err)
			}
		})
	}
	instance.documents.Open(documentURI.String(), 2, source+"\n")
	if _, err := instance.CodeLensResolve(context.Background(), &lens); !errors.Is(err, protocol.ErrContentModified) {
		t.Fatalf("stale resolve error = %v", err)
	}
}

func TestCodeLensNegotiatesDeclarationPosition(t *testing.T) {
	for _, test := range []struct {
		encoding  protocol.PositionEncodingKind
		character uint32
	}{
		{protocol.PositionEncodingKindUTF8, 16},
		{protocol.PositionEncodingKindUTF16, 14},
		{protocol.PositionEncodingKindUTF32, 13},
	} {
		t.Run(string(test.encoding), func(t *testing.T) {
			root := t.TempDir()
			source := "vim9script\ndef Target()\nenddef\ndef Use()\n  echo '😀' | Target()\nenddef\n"
			path := writeWorkspaceFile(t, root, "main.vim", source)
			instance := New(nil, nil, io.Discard)
			result, err := instance.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: protocol.ClientCapabilities{General: &protocol.GeneralClientCapabilities{PositionEncodings: []protocol.PositionEncodingKind{test.encoding}}}})
			if err != nil || result.Capabilities.PositionEncoding != test.encoding {
				t.Fatalf("initialize = %#v, %v", result, err)
			}
			documentURI := uri.File(path)
			instance.documents.Open(documentURI.String(), 1, source)
			lens := findCodeLens(t, instance, documentURI, "Target", codeLensReferences)
			if lens.Range.Start != (protocol.Position{Line: 1, Character: 4}) {
				t.Fatalf("range = %#v", lens.Range)
			}
			resolved, err := instance.CodeLensResolve(context.Background(), &lens)
			if err != nil {
				t.Fatal(err)
			}
			var locations []protocol.Location
			if len(resolved.Command.Arguments) != 3 {
				t.Fatalf("resolved = %#v", resolved)
			}
			if err := protocol.Unmarshal(resolved.Command.Arguments[2], &locations); err != nil || len(locations) != 1 || locations[0].Range.Start != (protocol.Position{Line: 4, Character: test.character}) {
				t.Fatalf("locations = %#v, error = %v", locations, err)
			}
		})
	}
}

func TestCodeLensResolveImplementationErrors(t *testing.T) {
	newInstance := func(t *testing.T) (*Server, protocol.CodeLens, string, string) {
		t.Helper()
		root := t.TempDir()
		source := "vim9script\ninterface I\n  def Run()\nendinterface\nclass A implements I\n  def Run()\n  enddef\nendclass\nclass B implements I\n  def Run()\n  enddef\nendclass\n"
		path := writeWorkspaceFile(t, root, "main.vim", source)
		instance := initializeWorkspaceServer(t, root)
		documentURI := canonicalTestURI(t, path)
		instance.documents.Open(documentURI.String(), 1, source)
		return instance, findCodeLens(t, instance, documentURI, "Run", codeLensImplementations), path, source
	}
	requestFailed := func(err error) bool {
		var rpcError *jsonrpc2.Error
		return errors.As(err, &rpcError) && rpcError.Code == jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed)
	}
	t.Run("result limit", func(t *testing.T) {
		instance, lens, _, _ := newInstance(t)
		instance.hierarchyLimit = 1
		if _, err := instance.CodeLensResolve(context.Background(), &lens); !requestFailed(err) {
			t.Fatalf("error = %#v", err)
		}
	})
	t.Run("incomplete relationships", func(t *testing.T) {
		instance, lens, path, source := newInstance(t)
		limited := workspace.NewIndex(maxWorkspaceFiles, maxIndexBytes, 1, 10)
		if err := limited.Replace(path, syntax.Parse(source)); err != nil {
			t.Fatal(err)
		}
		limited.SetComplete(true)
		instance.workspaceMu.Lock()
		instance.workspaceIndex = limited
		instance.workspaceMu.Unlock()
		if _, err := instance.CodeLensResolve(context.Background(), &lens); !requestFailed(err) {
			t.Fatalf("error = %#v", err)
		}
	})
}

func findCodeLens(t *testing.T, instance *Server, documentURI uri.URI, name string, kind codeLensKind) protocol.CodeLens {
	t.Helper()
	lenses, err := instance.CodeLens(context.Background(), &protocol.CodeLensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok := instance.documents.Snapshot(documentURI.String())
	if !ok {
		t.Fatal("missing snapshot")
	}
	for _, lens := range lenses {
		data := decodeTestCodeLensData(t, lens)
		if data.Lens != kind {
			continue
		}
		if snapshot.Text()[data.Start:data.End] == name {
			return lens
		}
	}
	t.Fatalf("missing %d lens for %q: %#v", kind, name, lenses)
	return protocol.CodeLens{}
}

func decodeTestCodeLensData(t *testing.T, lens protocol.CodeLens) codeLensData {
	t.Helper()
	var data codeLensData
	if err := protocol.Unmarshal(lens.Data, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCodeLensMissingAndOversizedDocuments(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	missing := uri.File(filepath.Join(t.TempDir(), "missing.vim"))
	if lenses, err := instance.CodeLens(context.Background(), &protocol.CodeLensParams{TextDocument: protocol.TextDocumentIdentifier{URI: missing}}); err != nil || len(lenses) != 0 {
		t.Fatalf("missing lenses = %#v, %v", lenses, err)
	}
	over := uri.File(filepath.Join(t.TempDir(), "over.vim"))
	instance.documents.Open(over.String(), 1, string(make([]byte, maxFileBytes+1)))
	if lenses, err := instance.CodeLens(context.Background(), &protocol.CodeLensParams{TextDocument: protocol.TextDocumentIdentifier{URI: over}}); err != nil || len(lenses) != 0 {
		t.Fatalf("oversized lenses = %#v, %v", lenses, err)
	}
}

func TestCodeLensCancellationBeforeResolution(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := instance.CodeLensResolve(ctx, &protocol.CodeLens{}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Fatalf("resolve error = %v", err)
	}
}

func TestCodeLensCancellationDuringResolution(t *testing.T) {
	root := t.TempDir()
	libSource := "vim9script\nexport def Target()\nenddef\n"
	libPath := writeWorkspaceFile(t, root, "lib.vim", libSource)
	writeWorkspaceFile(t, root, "main.vim", "vim9script\nimport './lib.vim' as lib\nlib.Target()\n")
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, libPath)
	instance.documents.Open(documentURI.String(), 1, libSource)
	lens := findCodeLens(t, instance, documentURI, "Target", codeLensReferences)
	ctx, cancel := context.WithCancel(context.Background())
	instance.testHooks.beforeWorkspaceIdentityCheck = cancel
	if _, err := instance.CodeLensResolve(ctx, &lens); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Fatalf("resolve error = %v", err)
	}
}
