package server

import (
	"context"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
)

func TestMacVimOptionHoverAndSemanticTokens(t *testing.T) {
	for _, test := range []struct{ name, spelling string }{
		{"antialias", "noanti"}, {"blurradius", "blur"}, {"fullscreen", "invfu"},
		{"fuoptions", "fuopt"}, {"macligatures", "macligatures"}, {"macmeta", "nommta"},
		{"macthinstrokes", "macthinstrokes"}, {"transparency", "transp"},
	} {
		for _, expression := range []bool{false, true} {
			spelling, prefix := test.spelling, "set "
			if expression {
				spelling, prefix = "&l:"+test.name, "echo "
			}
			source := prefix + spelling + "\n"
			instance, documentURI := openNavigationDocument(t, text.UTF16, source)
			t.Cleanup(instance.stopAnalysis)
			hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Character: uint32(len(prefix) + 1)},
			}})
			if err != nil || hover == nil {
				t.Fatalf("%q hover=%#v error=%v", source, hover, err)
			}
			content, ok := joinedHoverMarkdown(hover.Contents)
			if !ok || !strings.HasPrefix(content.Value, "'"+test.name+"'") || !strings.Contains(content.Value, "MacVim GUI:") || !strings.Contains(content.Value, "https://macvim.org/docs/options.txt.html") || strings.Contains(content.Value, "unavailable in Vim") {
				t.Fatalf("%q hover=%#v", source, hover)
			}
			if test.name == "macmeta" && !strings.Contains(content.Value, "local to buffer") {
				t.Fatal(content.Value)
			}
			tokens, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, token := range decodeSemanticTokens(tokens.Data) {
				if token == [5]uint32{0, uint32(len(prefix)), uint32(len(spelling)), semanticVariable, semanticDefaultLibrary} {
					found = true
				}
			}
			if !found {
				t.Fatalf("%q tokens=%v", source, decodeSemanticTokens(tokens.Data))
			}
			resolved, err := instance.CompletionResolve(context.Background(), &protocol.CompletionItem{
				Label: test.name, Data: completionResolveTargetData(completionResolveOption, test.name),
			})
			if err != nil {
				t.Fatal(err)
			}
			docs, ok := resolved.Documentation.(*protocol.MarkupContent)
			if !ok || docs.Value != content.Value {
				t.Fatalf("resolved documentation = %#v", resolved.Documentation)
			}
			instance.languageFeatures.hoverMarkup = protocol.MarkupKindPlainText
			plain := runtimeHelpHover(t, instance, documentURI, spelling)
			plainContent, ok := joinedHoverMarkdown(plain.Contents)
			if !ok || !strings.Contains(plainContent.Value, "MacVim GUI:") || strings.Contains(plainContent.Value, "**") {
				t.Fatalf("plain=%#v", plain)
			}
		}
	}
}
