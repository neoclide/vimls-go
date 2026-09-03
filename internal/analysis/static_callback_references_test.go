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
nnoremap <F3> <Cmd>echo 'call Target()'<CR>
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
		if reference.Name == "Target" {
			t.Fatalf("quoted <Cmd> text produced Target reference %#v", reference)
		}
	}
}

func TestStaticCallbackReferencesResolveScriptLocalAndVim9Names(t *testing.T) {
	tests := []struct {
		name, source string
		references   []string
	}{
		{
			name:       "legacy script-local forms",
			source:     "function! s:lowercase() abort\nendfunction\nfunction! s:Named() abort\nendfunction\nnnoremap <F1> <Cmd>call s:lowercase()<CR>\nnnoremap <F2> <Cmd>call <SID>Named()<CR>\nset omnifunc=s:lowercase\nlet &tagfunc = '<SID>Named'\n",
			references: []string{"s:lowercase", "<SID>Named"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			found := make(map[string]bool)
			for _, reference := range Analyze(file).References {
				if reference.Declaration != nil {
					found[reference.Name] = true
				}
			}
			for _, name := range test.references {
				if !found[name] {
					t.Fatalf("resolved %q reference is missing", name)
				}
			}
		})
	}
}

func TestStaticCallbackReferencesRejectVim9LowercaseNames(t *testing.T) {
	file := syntax.Parse("vim9script\ndef Callback()\nenddef\nnnoremap <F1> <Cmd>call lowercase()<CR>\nset omnifunc=lowercase\n")
	for _, reference := range Analyze(file).References {
		if reference.Name == "lowercase" {
			t.Fatalf("illegal Vim9 lowercase callback produced reference %#v", reference)
		}
	}
}
