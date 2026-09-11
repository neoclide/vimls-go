package server

import (
	"context"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/vimdata"
	"go.lsp.dev/protocol"
)

func toProtocolPosition(position text.Position) protocol.Position {
	return protocol.Position{Line: uint32(position.Line), Character: uint32(position.Character)}
}

func TestAutocmdEventHoverDocumentation(t *testing.T) {
	for _, tc := range []struct{ name, source, event string }{
		{"reported", "autocmd §FileType typescript let b:coc_pairs_disabled = ['<']", "FileType"},
		{"group", "augroup Example\naugroup END\nautocmd Example §FileType typescript echo 1", "FileType"},
		{"second event", "autocmd BufRead,§FileType * echo 1", "FileType"},
		{"case insensitive", "autocmd §filetype typescript echo 1", "FileType"},
		{"alias", "autocmd §BufRead * echo 1", "BufRead"},
		{"clear", "autocmd! §FileType", "FileType"},
		{"vim9", "vim9script\nautocmd §FileType typescript echo 1", "FileType"},
		{"modifier", "silent au §FileType typescript echo 1", "FileType"},
		{"neovim", "autocmd §LspAttach * echo 1", "LspAttach"},
		{"neovim group", "autocmd Example §PackChangedPre * echo 1", "PackChangedPre"},
		{"continuation", "\" 😀\r\nautocmd BufRead,\r\n  \\ §FileType * echo 1", "FileType"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, kind := range []protocol.MarkupKind{protocol.MarkupKindMarkdown, protocol.MarkupKindPlainText} {
				offset := strings.Index(tc.source, "§")
				source := strings.Replace(tc.source, "§", "", 1)
				s, uri := openNavigationDocument(t, text.UTF16, source)
				t.Cleanup(s.stopAnalysis)
				s.languageFeatures.hoverMarkup = kind
				snapshot, _ := s.documents.Snapshot(uri.String())
				position, err := snapshot.Position(offset, text.UTF16)
				if err != nil {
					t.Fatal(err)
				}
				hover, err := s.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: uri}, Position: toProtocolPosition(position)}})
				if err != nil || hover == nil {
					t.Fatalf("hover = %+v, %v", hover, err)
				}
				contents, ok := hover.Contents.(*protocol.MarkupContent)
				if !ok || contents.Kind != kind {
					t.Fatalf("contents = %#v", hover.Contents)
				}
				doc, _ := vimdata.LookupAutocmdEventDocumentation(tc.event)
				want := doc.Documentation
				if kind == protocol.MarkupKindPlainText {
					want = markdownToPlainText(want)
				}
				if !strings.Contains(contents.Value, want) {
					t.Fatalf("missing %s documentation: %s", tc.event, contents.Value)
				}
				end, _ := snapshot.Position(offset+len(tc.event), text.UTF16)
				if hover.Range == nil || hover.Range.Start != toProtocolPosition(position) || hover.Range.End != toProtocolPosition(end) {
					t.Fatalf("range = %+v", hover.Range)
				}
			}
		})
	}
}

func TestAutocmdEventHoverExcludesOtherPositions(t *testing.T) {
	for _, marked := range []string{
		"autocmd User §FileType echo 1",
		"autocmd FileType §FileType echo 1",
		"autocmd FileType * echo '§FileType'",
		"\" autocmd §FileType * echo 1",
		"autocmd §UnknownEvent * echo 1",
	} {
		offset := strings.Index(marked, "§")
		source := strings.Replace(marked, "§", "", 1)
		s, uri := openNavigationDocument(t, text.UTF16, source)
		t.Cleanup(s.stopAnalysis)
		hover, err := s.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: uri}, Position: protocol.Position{Character: uint32(offset)}}})
		if err != nil || hover != nil {
			t.Fatalf("%s: hover=%+v err=%v", source, hover, err)
		}
	}
}

func TestAutocmdEventCompletionDocumentation(t *testing.T) {
	for _, tc := range []struct{ source, event string }{
		{"autocmd Fi", "FileType"},
		{"autocmd Example Fi", "FileType"},
		{"autocmd BufRead,Fi", "FileType"},
		{"autocmd Lsp", "LspAttach"},
		{"autocmd Example Pack", "PackChangedPre"},
	} {
		for _, markdown := range []bool{true, false} {
			s, uri := openNavigationDocument(t, text.UTF16, tc.source)
			t.Cleanup(s.stopAnalysis)
			s.completion.docsMarkdown = markdown
			list := completionListRequest(t, s, uri, 0, uint32(len(tc.source)))
			var item *protocol.CompletionItem
			for i := range list.Items {
				if list.Items[i].Label == tc.event {
					item = &list.Items[i]
					break
				}
			}
			if item == nil {
				t.Fatalf("%s missing %s", tc.source, tc.event)
			}
			resolved, err := s.CompletionResolve(context.Background(), item)
			if err != nil || resolved == nil || resolved.Documentation == nil {
				t.Fatalf("%s: %+v %v", tc.source, resolved, err)
			}
			doc, _ := vimdata.LookupAutocmdEventDocumentation(tc.event)
			if markdown {
				content, ok := resolved.Documentation.(*protocol.MarkupContent)
				if !ok || content.Kind != protocol.MarkupKindMarkdown || !strings.Contains(content.Value, doc.Documentation) {
					t.Fatalf("%#v", resolved.Documentation)
				}
			} else {
				content, ok := resolved.Documentation.(protocol.String)
				if !ok || !strings.Contains(string(content), markdownToPlainText(doc.Documentation)) {
					t.Fatalf("%#v", resolved.Documentation)
				}
			}
			if resolved.Label != item.Label || resolved.TextEdit != item.TextEdit {
				t.Fatal("resolve changed insertion fields")
			}
		}
	}
}
