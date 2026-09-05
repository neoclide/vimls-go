package server

import (
	"context"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
)

func TestMappingExpressionRegisterLanguageFeatures(t *testing.T) {
	source := "nnoremap <leader>e :e <C-R>=substitute(expand('%:p:h').'/', getcwd().'/', '', '')<CR>\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"substitute", "expand", "getcwd"} {
		start := uint32(strings.Index(source, name))
		hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Character: start + 1},
		}})
		if err != nil || hover == nil || hover.Range == nil || *hover.Range != navigationRange(0, start, start+uint32(len(name))) {
			t.Fatalf("%s hover = %#v, error = %v", name, hover, err)
		}
		contents, ok := joinedHoverMarkdown(hover.Contents)
		if !ok || !strings.Contains(contents.Value, name+"(") {
			t.Fatalf("%s hover contents = %#v", name, hover.Contents)
		}
		assertSemanticToken(t, tokens.Data, 0, start, semanticFunction, semanticDefaultLibrary)
	}
	help, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Character: uint32(strings.Index(source, "'%:p:h'") + 2)},
	}})
	if err != nil || help == nil || len(help.Signatures) != 1 || !strings.HasPrefix(help.Signatures[0].Label, "expand(") {
		t.Fatalf("signature help = %#v, error = %v", help, err)
	}
}

func TestMappingExpressionStaticCommandFeatures(t *testing.T) {
	for command, canonical := range map[string]string{"edit": "edit", "vs": "vsplit", "tabe": "tabedit"} {
		t.Run(command, func(t *testing.T) {
			source := "nnoremap <leader>e :" + command + " <C-R>=substitute(expand('%:p:h').'/', getcwd().'/', '', '')<CR>\n"
			instance, uri := openNavigationDocument(t, text.UTF16, source)
			start := uint32(strings.Index(source, ":") + 1)
			hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: uri}, Position: protocol.Position{Character: start + 1},
			}})
			if err != nil || hover == nil || hover.Range == nil || *hover.Range != navigationRange(0, start, start+uint32(len(command))) {
				t.Fatalf("command hover = %#v, error = %v", hover, err)
			}
			contents, ok := joinedHoverMarkdown(hover.Contents)
			if !ok || !strings.Contains(contents.Value, "**"+canonical+"**") {
				t.Fatalf("command hover contents = %#v", hover.Contents)
			}
			tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: uri}})
			if err != nil {
				t.Fatal(err)
			}
			assertSemanticToken(t, tokens.Data, 0, start, semanticKeyword, semanticDefaultLibrary)
			for _, name := range []string{"substitute", "expand", "getcwd"} {
				assertSemanticToken(t, tokens.Data, 0, uint32(strings.Index(source, name)), semanticFunction, semanticDefaultLibrary)
			}
		})
	}
}

func TestMappingExpressionRegisterDefinitionAndUTF16(t *testing.T) {
	source := "function! s:Path() abort\r\nendfunction\r\nnnoremap 😀 :e <C-R><C-O>=<SID>Path()<CR>\r\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	line := strings.Split(source, "\n")[2]
	start := uint32(strings.Index(line, "Path") - 2) // One astral rune is two UTF-16 units.
	position := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: start + 1}}
	definition, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: position})
	locations, ok := definition.(protocol.LocationSlice)
	if err != nil || !ok || len(locations) != 1 || locations[0].Range != navigationRange(0, 10, 16) {
		t.Fatalf("definition = %#v, error = %v", definition, err)
	}
	hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: position})
	if err != nil || hover == nil || hover.Range == nil || *hover.Range != navigationRange(2, start-5, start+4) {
		t.Fatalf("hover = %#v, error = %v", hover, err)
	}
	assertFunctionHoverContents(t, hover, "s:Path()", "")
}

func TestMappingExpressionRegisterCompletion(t *testing.T) {
	for _, source := range []string{
		"nnoremap <F5> :e <C-R>=getcw<CR>\n",
		"inoremap <F5> <C-R><C-P>=getcw<CR>\n",
		"cnoremap <F5> <C-\\>egetcw<CR>\n",
		"nnoremap <F5> \"=getcw<CR>p\n",
	} {
		instance, documentURI := openNavigationDocument(t, text.UTF16, source)
		result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Character: uint32(strings.Index(source, "getcw") + 5)},
		}})
		list, ok := result.(*protocol.CompletionList)
		if err != nil || !ok || !hasCompletion(list.Items, "getcwd", protocol.CompletionItemKindFunction) {
			t.Fatalf("%q completion = %#v, error = %v", source, result, err)
		}
		for _, item := range list.Items {
			if item.Label != "getcwd" {
				continue
			}
			edit, ok := item.TextEdit.(*protocol.TextEdit)
			start := uint32(strings.Index(source, "getcw"))
			if !ok || edit.Range != navigationRange(0, start, start+5) {
				t.Fatalf("%q completion edits prompt keys: %#v", source, item.TextEdit)
			}
		}
	}
}
