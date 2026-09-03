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
