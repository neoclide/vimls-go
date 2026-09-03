package server

import (
	"context"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
)

// TestStaticCallbackNavigation verifies §7 P1: definition navigation reaches
// the callback function from an autocmd body, an <expr> mapping RHS, a
// <Cmd>...<CR> mapping RHS, and a callback option value.
func TestStaticCallbackNavigation(t *testing.T) {
	source := `function! Target() abort
endfunction
autocmd BufReadPost * call Target()
nnoremap <expr> <F1> Target()
nnoremap <F2> <Cmd>call Target()<CR>
set omnifunc=Target
let &tagfunc = 'Target'
`
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	targets := []struct {
		name         string
		line, column uint32
	}{
		{name: "autocmd body", line: 2, column: uint32(len("autocmd BufReadPost * call "))},
		{name: "expr mapping RHS", line: 3, column: uint32(len("nnoremap <expr> <F1> "))},
		{name: "Cmd mapping RHS", line: 4, column: uint32(len("nnoremap <F2> <Cmd>call "))},
		{name: "set callback option value", line: 5, column: uint32(len("set omnifunc="))},
		{name: "let callback option value", line: 6, column: uint32(len("let &tagfunc = '"))},
	}
	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			document, err := instance.navigationAt(context.Background(), documentURI.String(), protocol.Position{Line: target.line, Character: target.column})
			if err != nil || document == nil || document.declaration == nil {
				t.Fatalf("navigation at %d:%d = %#v, error = %v", target.line, target.column, document, err)
			}
			if document.declaration.Name != "Target" {
				t.Fatalf("navigation declaration = %#v, want Target", document.declaration)
			}
		})
	}
}

func TestStaticCallbackNavigationScriptLocalNames(t *testing.T) {
	tests := []struct {
		name, source, declaration string
		line, column              uint32
	}{
		{
			name:        "legacy s lowercase",
			source:      "function! s:lowercase() abort\nendfunction\nnnoremap <F1> <Cmd>call s:lowercase()<CR>\n",
			declaration: "s:lowercase",
			line:        2,
			column:      uint32(len("nnoremap <F1> <Cmd>call ")),
		},
		{
			name:        "legacy SID name",
			source:      "function! s:Named() abort\nendfunction\nnnoremap <F1> <Cmd>call <SID>Named()<CR>\n",
			declaration: "s:Named",
			line:        2,
			column:      uint32(len("nnoremap <F1> <Cmd>call ")),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			document, err := instance.navigationAt(context.Background(), documentURI.String(), protocol.Position{Line: test.line, Character: test.column})
			if err != nil || document == nil || document.declaration == nil {
				t.Fatalf("navigation = %#v, error = %v", document, err)
			}
			if document.declaration.Name != test.declaration {
				t.Fatalf("navigation declaration = %q, want %q", document.declaration.Name, test.declaration)
			}
		})
	}
}

// TestStaticCallbackNavigationHandlers keeps the handler contract separate
// from navigationAt: static callback sites must be usable through definition,
// references, and hover, while an execute-generated call remains opaque.
func TestStaticCallbackNavigationHandlers(t *testing.T) {
	source := "function! Target() abort\nendfunction\nautocmd BufReadPost * call Target()\nnnoremap <expr> <F1> Target()\nnnoremap <F2> <Cmd>call Target()<CR>\nset omnifunc=Target\nexecute 'call Target()'\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	position := protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: 2, Character: uint32(len("autocmd BufReadPost * call "))},
	}
	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: position})
	locations, ok := definition.(protocol.LocationSlice)
	if err != nil || !ok || len(locations) != 1 || locations[0].Range != navigationRange(0, 10, 16) {
		t.Fatalf("callback definition = %#v, %v", definition, err)
	}
	references, err := instance.References(context.Background(), &protocol.ReferenceParams{
		TextDocumentPositionParams: position,
		Context:                    protocol.ReferenceContext{IncludeDeclaration: true},
	})
	if err != nil || len(references) != 5 {
		t.Fatalf("callback references = %#v, %v", references, err)
	}
	for _, location := range references {
		if location.Range.Start.Line == 6 {
			t.Fatalf("dynamic execute call was treated as a callback reference: %#v", references)
		}
	}
	hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: position})
	if err != nil || hover == nil || hover.Range == nil || *hover.Range != navigationRange(2, uint32(len("autocmd BufReadPost * call ")), uint32(len("autocmd BufReadPost * call Target"))) {
		t.Fatalf("callback hover = %#v, %v", hover, err)
	}
	dynamic := position
	dynamic.Position = protocol.Position{Line: 6, Character: uint32(len("execute 'call "))}
	if result, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: dynamic}); err != nil || len(result.(protocol.LocationSlice)) != 0 {
		t.Fatalf("dynamic callback definition = %#v, %v", result, err)
	}
}
