package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// TestStaticCallbackReferences verifies §7 P1 static navigation sources:
// calls inside autocmd bodies, <expr> mapping RHS, <Cmd>...<CR> mapping RHS,
// and callback option values (:set name=Func, :let &name='Func') all produce
// function references that resolve to the same-file definition.
func TestStaticCallbackReferences(t *testing.T) {
	source := `function! Target() abort
endfunction
autocmd BufReadPost * call Target()
nnoremap <expr> <F1> Target()
nnoremap <F2> <Cmd>call Target()<CR>
set omnifunc=Target
let &tagfunc = 'Target'
`
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
	}
	result := Analyze(file)
	resolved := 0
	for _, reference := range result.References {
		if reference == nil || reference.Name != "Target" || reference.Declaration == nil {
			continue
		}
		text := file.Text(reference.Span)
		if text != "Target" && text != "'Target'" {
			t.Fatalf("unexpected Target reference span %q", text)
		}
		resolved++
	}
	if resolved < 5 {
		t.Fatalf("resolved Target references = %d, want at least 5", resolved)
	}
}

// TestStaticCallbackReferencesStayOpaque verifies dynamic or unknown callback
// forms produce no references and no noise.
func TestStaticCallbackReferencesStayOpaque(t *testing.T) {
	source := `function! Target() abort
endfunction
nnoremap <F1> <Cmd>echo 'plain payload'<CR>
nnoremap <F2> <Cmd>call SomeOtherUnknown()<CR>
set omnifunc=notlower
set omnifunc+=Suffix
let &tagfunc = 'plain' . 'concatenated'
`
	file := syntax.Parse(source)
	result := Analyze(file)
	for _, reference := range result.References {
		if reference.Name == "notlower" || reference.Name == "Suffix" || reference.Name == "plain" || reference.Name == "concatenated" {
			t.Fatalf("opaque callback value produced reference %#v", reference)
		}
		if strings.Contains(file.Text(reference.Span), "echo 'plain") {
			t.Fatalf("plain <Cmd> payload produced reference %#v", reference)
		}
	}
}
