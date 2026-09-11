package server

import (
	"context"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/vimdata"
	"go.lsp.dev/protocol"
	"reflect"
	"strings"
	"testing"
)

func TestCommandCompletionRetainsBangDocumentation(t *testing.T) {
	for _, tc := range []struct {
		source, name string
		bang         bool
	}{
		{"m§ap! K abc", "map", true},
		{"n§o! K abc", "noremap", true},
		{"u§nm! K", "unmap", true},
		{"m§apc!", "mapclear", true},
		{"c§om! Demo echo 1", "command", true},
		{"silent m§ap! K abc", "map", true},
		{"silent! m§ap K abc", "map", false},
		{"map! K abc\nm§ap K abc", "map", false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			for _, markdown := range []bool{false, true} {
				offset := strings.Index(tc.source, "§")
				source := strings.Replace(tc.source, "§", "", 1) + "\n"
				s, uri := openNavigationDocument(t, text.UTF16, source)
				t.Cleanup(s.stopAnalysis)
				s.completion.docsMarkdown = markdown
				snapshot, _ := s.documents.Snapshot(uri.String())
				position, err := snapshot.Position(offset, text.UTF16)
				if err != nil {
					t.Fatal(err)
				}
				result, err := s.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{URI: uri}, Position: toProtocolPosition(position),
				}})
				if err != nil {
					t.Fatal(err)
				}
				item := completionItemWithLabel(completionItems(t, result), tc.name)
				if item == nil {
					t.Fatalf("missing %s", tc.name)
				}
				target, ok := completionResolveTargetFromData(item.Data)
				if !ok || target.Bang != tc.bang {
					t.Fatalf("target=%#v", target)
				}
				edit, ok := item.TextEdit.(*protocol.TextEdit)
				if !ok {
					t.Fatalf("edit=%#v", item.TextEdit)
				}
				end, err := snapshot.Offset(fromProtocolPosition(edit.Range.End), text.UTF16)
				if err != nil {
					t.Fatal(err)
				}
				if tc.bang && (end >= len(source) || source[end] != '!') {
					t.Fatalf("edit consumes bang: %#v", edit)
				}
				before := *item
				resolved, err := s.CompletionResolve(context.Background(), item)
				if err != nil {
					t.Fatal(err)
				}
				want, _ := vimdata.LookupMappingCommandDocumentation(tc.name, tc.bang)
				if markdown {
					doc, ok := resolved.Documentation.(*protocol.MarkupContent)
					if !ok || doc.Value != want {
						t.Fatalf("documentation=%#v", resolved.Documentation)
					}
				} else if doc, ok := resolved.Documentation.(protocol.String); !ok || string(doc) != markdownToPlainText(want) {
					t.Fatalf("documentation=%#v", resolved.Documentation)
				}
				if resolved.Label != before.Label || !reflect.DeepEqual(resolved.TextEdit, before.TextEdit) || resolved.InsertText != before.InsertText {
					t.Fatal("resolve changed insertion fields")
				}
			}
		})
	}
}
