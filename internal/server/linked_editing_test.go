package server

import (
	"context"
	"io"
	"slices"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestLinkedEditingRangeCapability(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	result, err := instance.Initialize(context.Background(), &protocol.InitializeParams{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Capabilities.LinkedEditingRangeProvider == nil {
		t.Fatalf("linked editing capability = %#v", result.Capabilities)
	}
	if !implementedMethod(protocol.MethodTextDocumentLinkedEditingRange) {
		t.Errorf("method %q is not implemented", protocol.MethodTextDocumentLinkedEditingRange)
	}
}

func TestLinkedEditingRangeVim9Variable(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar value: number = 1\nvar copy = value\necho value\n")
	ranges := linkedEditingRanges(t, instance, documentURI, protocol.Position{Line: 2, Character: 12})
	if ranges == nil {
		t.Fatal("linked editing ranges = nil")
	}
	want := []protocol.Range{navigationRange(1, 4, 9), navigationRange(2, 11, 16), navigationRange(3, 5, 10)}
	if !slices.Equal(ranges.Ranges, want) {
		t.Fatalf("ranges = %#v, want %#v", ranges.Ranges, want)
	}
	pattern := `(?:[sglabwtv]:)?[A-Za-z_][A-Za-z0-9_]*`
	if ranges.WordPattern == nil || *ranges.WordPattern != pattern {
		t.Fatalf("word pattern = %v, want %q", ranges.WordPattern, pattern)
	}
}

func TestLinkedEditingRangeScriptLocalFunction(t *testing.T) {
	source := "function! s:helper() abort\nendfunction\ncall s:helper()\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	ranges := linkedEditingRanges(t, instance, documentURI, protocol.Position{Line: 2, Character: 6})
	if ranges == nil {
		t.Fatal("linked editing ranges = nil")
	}
	want := []protocol.Range{navigationRange(0, 10, 18), navigationRange(2, 5, 13)}
	if !slices.Equal(ranges.Ranges, want) {
		t.Fatalf("ranges = %#v, want %#v", ranges.Ranges, want)
	}
	if ranges.WordPattern == nil || *ranges.WordPattern != linkedEditingWordPattern {
		t.Fatalf("word pattern = %v, want %q", ranges.WordPattern, linkedEditingWordPattern)
	}
}

func TestLinkedEditingRangeRequiresMoreThanDeclaration(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar value: number = 1\n")
	ranges := linkedEditingRanges(t, instance, documentURI, protocol.Position{Line: 1, Character: 5})
	if ranges != nil {
		t.Fatalf("ranges = %#v, want nil", ranges)
	}
}

func TestLinkedEditingRangeRejectsUnknownSymbol(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar value: number = 1\necho value\n")
	ranges := linkedEditingRanges(t, instance, documentURI, protocol.Position{Line: 0, Character: 1})
	if ranges != nil {
		t.Fatalf("ranges = %#v, want nil", ranges)
	}
}

func TestLinkedEditingRangeRejectsMixedScriptLocalSpelling(t *testing.T) {
	source := "function! s:helper() abort\nendfunction\ncall s:helper()\ncall <SID>helper()\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	ranges := linkedEditingRanges(t, instance, documentURI, protocol.Position{Line: 2, Character: 6})
	if ranges != nil {
		t.Fatalf("ranges = %#v, want nil for differing occurrence text", ranges)
	}
}

func TestLinkedEditingRangeVim9ScriptLocalFunction(t *testing.T) {
	source := "vim9script\ndef Helper(): number\n  return 1\nenddef\nvar n = Helper()\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	ranges := linkedEditingRanges(t, instance, documentURI, protocol.Position{Line: 4, Character: 9})
	if ranges == nil {
		t.Fatal("linked editing ranges = nil for a file-local symbol")
	}
	want := []protocol.Range{navigationRange(1, 4, 10), navigationRange(4, 8, 14)}
	if !slices.Equal(ranges.Ranges, want) {
		t.Fatalf("ranges = %#v, want %#v", ranges.Ranges, want)
	}
}

// A global or exported symbol can be referenced from other files, so editing
// only the open document would leave the rest of the workspace inconsistent.
func TestLinkedEditingRangeWithholdsWorkspaceVisibleSymbols(t *testing.T) {
	for _, tc := range []struct {
		name     string
		source   string
		position protocol.Position
	}{
		{
			name:     "legacy global function",
			source:   "function! Helper() abort\nendfunction\ncall Helper()\n",
			position: protocol.Position{Line: 2, Character: 6},
		},
		{
			name:     "vim9 exported function",
			source:   "vim9script\nexport def Helper(): number\n  return 1\nenddef\nvar n = Helper()\n",
			position: protocol.Position{Line: 4, Character: 9},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, tc.source)
			ranges := linkedEditingRanges(t, instance, documentURI, tc.position)
			if ranges != nil {
				t.Fatalf("ranges = %#v, want nil for a workspace-visible symbol", ranges)
			}
		})
	}
}

// A member of an exported aggregate is reachable from other files, but the
// member itself is not reported as exported, so no member is offered.
func TestLinkedEditingRangeWithholdsMembers(t *testing.T) {
	source := "vim9script\nclass Counter\n  var count: number = 0\n  def Inc(): number\n    this.count += 1\n    return this.count\n  enddef\nendclass\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	ranges := linkedEditingRanges(t, instance, documentURI, protocol.Position{Line: 4, Character: 10})
	if ranges != nil {
		t.Fatalf("ranges = %#v, want nil for a class member", ranges)
	}
}

func linkedEditingRanges(t *testing.T, instance *Server, documentURI uri.URI, position protocol.Position) *protocol.LinkedEditingRanges {
	t.Helper()
	ranges, err := instance.LinkedEditingRange(context.Background(), &protocol.LinkedEditingRangeParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Position:     position,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return ranges
}
