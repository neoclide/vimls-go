package server

import (
	"context"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/vimdata"
	"go.lsp.dev/protocol"
)

func TestMappingCommandHoverDocumentation(t *testing.T) {
	for _, tc := range []struct {
		typed, canonical string
		bang             bool
	}{
		{"map", "map", false}, {"map!", "map", true},
		{"nm", "nmap", false}, {"nmap", "nmap", false},
		{"vm", "vmap", false}, {"xm", "xmap", false},
		{"smap", "smap", false}, {"om", "omap", false},
		{"im", "imap", false}, {"lm", "lmap", false},
		{"lma", "lmap", false}, {"cm", "cmap", false}, {"tma", "tmap", false},
	} {
		t.Run(tc.typed, func(t *testing.T) {
			for _, kind := range []protocol.MarkupKind{protocol.MarkupKindMarkdown, protocol.MarkupKindPlainText} {
				source := "vim9script\nsilent " + tc.typed + " <F2> abc\n"
				s, uri := openNavigationDocument(t, text.UTF16, source)
				t.Cleanup(s.stopAnalysis)
				s.languageFeatures.hoverMarkup = kind
				hover, err := s.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{URI: uri}, Position: protocol.Position{Line: 1, Character: 8},
				}})
				if err != nil || hover == nil {
					t.Fatalf("hover = %#v, %v", hover, err)
				}
				content, ok := hover.Contents.(*protocol.MarkupContent)
				want, found := vimdata.LookupMappingCommandDocumentation(tc.canonical, tc.bang)
				if kind == protocol.MarkupKindPlainText {
					want = markdownToPlainText(want)
				}
				if !found || !ok || content.Kind != kind || content.Value != want {
					t.Fatalf("contents = %#v, want %q", hover.Contents, want)
				}
				if hover.Range == nil || hover.Range.Start != (protocol.Position{Line: 1, Character: 7}) || hover.Range.End != (protocol.Position{Line: 1, Character: uint32(7 + len(tc.typed))}) {
					t.Fatalf("range = %#v", hover.Range)
				}
			}
		})
	}
}

func TestMappingCommandDocumentationScope(t *testing.T) {
	for _, source := range []string{"set number\n", "echo 1\n"} {
		s, uri := openNavigationDocument(t, text.UTF16, source)
		t.Cleanup(s.stopAnalysis)
		hover, err := s.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri}, Position: protocol.Position{Character: 1},
		}})
		if err != nil || hover == nil {
			t.Fatalf("hover = %#v, %v", hover, err)
		}
		content, ok := joinedHoverMarkdown(hover.Contents)
		if !ok || !strings.Contains(content.Value, "An Ex command.") {
			t.Fatalf("unexpected documentation: %#v", hover.Contents)
		}
	}
}

func TestRemainingMapFileCommandHover(t *testing.T) {
	for _, tc := range []struct {
		source, canonical string
		bang              bool
	}{
		{"nunmap <buffer> K", "nunmap", false},
		{"nun <buffer> K", "nunmap", false},
		{"unm! K", "unmap", true},
		{"nn <buffer> K :help<CR>", "nnoremap", false},
		{"no! K abc", "noremap", true},
		{"mapc!", "mapclear", true},
		{"nmapc <buffer>", "nmapclear", false},
		{"ab", "abbreviate", false},
		{"ab teh", "abbreviate", false},
		{"ab <buffer> teh the", "abbreviate", false},
		{"una <buffer> teh", "unabbreviate", false},
		{"norea teh the", "noreabbrev", false},
		{"ca teh the", "cabbrev", false},
		{"ia teh the", "iabbrev", false},
		{"cnorea teh the", "cnoreabbrev", false},
		{"inorea teh the", "inoreabbrev", false},
		{"cuna teh", "cunabbrev", false},
		{"iuna teh", "iunabbrev", false},
		{"abc <buffer>", "abclear", false},
		{"iabc", "iabclear", false},
		{"cabc", "cabclear", false},
		{"com", "command", false},
		{"com Example", "command", false},
		{"com! -buffer Example echo 1", "command", true},
		{"delc -buffer Example", "delcommand", false},
		{"comc", "comclear", false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			for _, prefix := range []string{"", "vim9script\n"} {
				for _, kind := range []protocol.MarkupKind{protocol.MarkupKindMarkdown, protocol.MarkupKindPlainText} {
					s, uri := openNavigationDocument(t, text.UTF16, prefix+tc.source+"\n")
					t.Cleanup(s.stopAnalysis)
					s.languageFeatures.hoverMarkup = kind
					var line uint32
					if prefix != "" {
						line = 1
					}
					hover, err := s.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
						TextDocument: protocol.TextDocumentIdentifier{URI: uri}, Position: protocol.Position{Line: line, Character: 1},
					}})
					if err != nil || hover == nil {
						t.Fatalf("hover = %#v, %v", hover, err)
					}
					want, found := vimdata.LookupMappingCommandDocumentation(tc.canonical, tc.bang)
					if kind == protocol.MarkupKindPlainText {
						want = markdownToPlainText(want)
					}
					content, ok := hover.Contents.(*protocol.MarkupContent)
					if !found || !ok || content.Kind != kind || content.Value != want {
						t.Fatalf("contents = %#v, want %q", hover.Contents, want)
					}
				}
			}
		})
	}
}

func TestMappingDocumentationOverridesRuntimeHelp(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		s, uri := openNavigationDocument(t, text.UTF16, "nunmap <buffer> K\nnoremap! K abc\n\n")
		t.Cleanup(s.stopAnalysis)
		kind := protocol.MarkupKindPlainText
		if markdown {
			kind = protocol.MarkupKindMarkdown
		}
		s.languageFeatures.hoverMarkup = kind
		s.completion.docsMarkdown = markdown
		root := t.TempDir()
		writeWorkspaceFile(t, root, "doc/map.txt", "*:nunmap*\nRuntime-only nunmap text.\n*:noremap*\nRuntime-only noremap text.\n")
		for _, loaded := range []bool{false, true} {
			if loaded {
				s.setRuntimePaths([]string{root})
				s.runtimeHelpWG.Wait()
				if s.runtimeHelpMarkdown(":nunmap") == "" {
					t.Fatal("runtime help fixture was not indexed")
				}
			}
			for _, tc := range []struct {
				name string
				bang bool
				line uint32
			}{
				{"nunmap", false, 0}, {"noremap", true, 1},
			} {
				hover, err := s.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{URI: uri}, Position: protocol.Position{Line: tc.line, Character: 1},
				}})
				if err != nil || hover == nil {
					t.Fatalf("hover = %#v, %v", hover, err)
				}
				want, _ := vimdata.LookupMappingCommandDocumentation(tc.name, tc.bang)
				if !markdown {
					want = markdownToPlainText(want)
				}
				content, ok := hover.Contents.(*protocol.MarkupContent)
				if !ok || content.Kind != kind || content.Value != want {
					t.Fatalf("loaded=%v hover must contain only built-in documentation: %#v", loaded, hover.Contents)
				}
			}
			completions, err := s.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: uri}, Position: protocol.Position{Line: 2},
			}})
			if err != nil {
				t.Fatal(err)
			}
			item := completionItemWithLabel(completionItems(t, completions), "nunmap")
			if item == nil {
				t.Fatal("nunmap completion missing")
			}
			resolved, err := s.CompletionResolve(context.Background(), item)
			if err != nil {
				t.Fatal(err)
			}
			want, _ := vimdata.LookupMappingCommandDocumentation("nunmap", false)
			if markdown {
				doc, ok := resolved.Documentation.(*protocol.MarkupContent)
				if !ok || doc.Kind != kind || doc.Value != want {
					t.Fatalf("loaded=%v resolved docs = %#v", loaded, resolved.Documentation)
				}
			} else {
				doc, ok := resolved.Documentation.(protocol.String)
				if !ok || string(doc) != markdownToPlainText(want) {
					t.Fatalf("loaded=%v resolved docs = %#v", loaded, resolved.Documentation)
				}
			}
		}
	}
}

func TestMappingBangHoverRange(t *testing.T) {
	for _, name := range []string{"map", "noremap", "unmap", "mapclear", "command"} {
		source := name + "!"
		switch name {
		case "map", "noremap":
			source += " K abc"
		case "unmap":
			source += " K"
		case "command":
			source += " Demo echo 1"
		}
		s, uri := openNavigationDocument(t, text.UTF16, source+"\n")
		t.Cleanup(s.stopAnalysis)
		root := t.TempDir()
		writeWorkspaceFile(t, root, "doc/map.txt", "*:"+name+"*\nRuntime duplicate.\n")
		s.setRuntimePaths([]string{root})
		s.runtimeHelpWG.Wait()
		for _, offset := range []int{0, len(name)} {
			hover, err := s.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: uri}, Position: protocol.Position{Character: uint32(offset)},
			}})
			if err != nil || hover == nil {
				t.Fatalf("%s offset %d: %#v %v", name, offset, hover, err)
			}
			want, _ := vimdata.LookupMappingCommandDocumentation(name, true)
			contents, ok := hover.Contents.(*protocol.MarkupContent)
			if !ok || contents.Value != want {
				t.Fatalf("%s contents=%#v", name, hover.Contents)
			}
			if hover.Range == nil || *hover.Range != navigationRange(0, 0, uint32(len(name)+1)) {
				t.Fatalf("%s range=%v", name, hover.Range)
			}
		}
	}
}
