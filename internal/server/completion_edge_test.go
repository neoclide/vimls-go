package server

import (
	"context"
	"testing"

	"go.lsp.dev/protocol"
)

// Completion must be safe at every cursor boundary, including incomplete
// commands.  Besides exercising recovery, this prevents a new completion
// context from assuming that its command payload is complete.
func TestCompletionAtEveryCursorBoundary(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Exported(value: number): string\n  return ''\nenddef\nexport class Thing\n  var member = 1\nendclass\n")
	source := "vim9script\n" +
		"import './lib.vim' as lib\n" +
		"var localValue = 1\n" +
		"def Local(argument: string): string\n" +
		"  var nested = argument\n" +
		"  echo nested\n" +
		"  echo v:vers\n" +
		"  echo &num\n" +
		"  echo has('gui_')\n" +
		"  echo expand('<cf')\n" +
		"  echo lib.Ex\n" +
		"  var object = lib.Thing.new()\n" +
		"  echo object.mem\n" +
		"enddef\n" +
		"set number?\n" +
		"command -nargs=? -bar Demo echo\n" +
		"augroup DemoGroup\n" +
		"augroup END\n" +
		"highlight Demo guifg=#fff\n" +
		"nnoremap <silent> <leader>x :echo local\n" +
		"colorscheme def\n" +
		"syntax keyword Demo alpha beta\n" +
		"syntax match Demo /alpha/ containedin=\n" +
		"autocmd BufEnter *.vim ech\n" +
		"augroup DemoGroup\n" +
		"wincmd \n" +
		"filter /Demo/ command Demo echo\n" +
		"command -complete=custom, -nargs=* Custom echo\n" +
		"highlight link Demo Normal\n" +
		"setlocal tabstop=\n" +
		"setglobal number\n" +
		"autocmd DemoGroup BufWritePost *.vim call Local()\n" +
		"source ./lib.vim\n" +
		"runtime plugin/\n"
	path := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)

	line := uint32(0)
	for _, text := range splitCompletionLines(source) {
		for character := 0; character <= len(text); character++ {
			params := &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
				Position:     protocol.Position{Line: line, Character: uint32(character)},
			}}
			if _, err := instance.Completion(context.Background(), params); err != nil {
				t.Fatalf("completion at %d:%d: %v", line, character, err)
			}
		}
		line++
	}
}

func splitCompletionLines(source string) []string {
	lines := []string{}
	start := 0
	for index := 0; index < len(source); index++ {
		if source[index] == '\n' {
			lines = append(lines, source[start:index])
			start = index + 1
		}
	}
	if start < len(source) {
		lines = append(lines, source[start:])
	}
	return lines
}
