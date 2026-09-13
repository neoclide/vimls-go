package server

import (
	"context"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
)

func TestOptionHoverHistory(t *testing.T) {
	for _, tc := range []struct {
		name      string
		source    string
		char      int
		wantSince string
		wantPlain string
	}{
		{
			name:      "option statement",
			source:    "set smoothscroll\n",
			char:      5,
			wantSince: "Since Vim 9.0.0640",
		},
		{
			name:      "option short name",
			source:    "set sms\n",
			char:      5,
			wantSince: "Since Vim 9.0.0640",
		},
		{
			name:      "option expression",
			source:    "echo &smoothscroll\n",
			char:      7,
			wantSince: "Since Vim 9.0.0640",
		},
		{
			name:      "pre-9.0 option has no history",
			source:    "set number\n",
			char:      5,
			wantSince: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, kind := range []protocol.MarkupKind{protocol.MarkupKindMarkdown, protocol.MarkupKindPlainText} {
				instance, documentURI := openNavigationDocument(t, text.UTF16, tc.source)
				t.Cleanup(instance.stopAnalysis)
				instance.languageFeatures.hoverMarkup = kind

				hover, err := instance.Hover(context.Background(), &protocol.HoverParams{
					TextDocumentPositionParams: protocol.TextDocumentPositionParams{
						TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
						Position:     protocol.Position{Line: 0, Character: uint32(tc.char)},
					},
				})
				if err != nil || hover == nil {
					t.Fatalf("hover failed: %#v, err: %v", hover, err)
				}
				content, ok := joinedHoverMarkdown(hover.Contents)
				if !ok {
					t.Fatalf("joinedHoverMarkdown failed: %#v", hover.Contents)
				}
				if tc.wantSince != "" {
					if !strings.Contains(content.Value, tc.wantSince) {
						t.Errorf("hover content %q does not contain %q", content.Value, tc.wantSince)
					}
				} else {
					if strings.Contains(content.Value, "Since Vim") {
						t.Errorf("hover content %q unexpectedly contains Since Vim", content.Value)
					}
				}
			}
		})
	}
}

func TestCommandHoverHistory(t *testing.T) {
	for _, tc := range []struct {
		name      string
		source    string
		char      int
		wantSince string
	}{
		{
			name:      "defer command",
			source:    "defer Close()\n",
			char:      2,
			wantSince: "Since Vim 9.0.0370",
		},
		{
			name:      "echowindow command",
			source:    "echowindow 'msg'\n",
			char:      3,
			wantSince: "Since Vim 9.0.0321",
		},
		{
			name:      "pre-9.0 command has no history",
			source:    "echo 'msg'\n",
			char:      2,
			wantSince: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, kind := range []protocol.MarkupKind{protocol.MarkupKindMarkdown, protocol.MarkupKindPlainText} {
				instance, documentURI := openNavigationDocument(t, text.UTF16, tc.source)
				t.Cleanup(instance.stopAnalysis)
				instance.languageFeatures.hoverMarkup = kind

				hover, err := instance.Hover(context.Background(), &protocol.HoverParams{
					TextDocumentPositionParams: protocol.TextDocumentPositionParams{
						TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
						Position:     protocol.Position{Line: 0, Character: uint32(tc.char)},
					},
				})
				if err != nil || hover == nil {
					t.Fatalf("hover failed: %#v, err: %v", hover, err)
				}
				content, ok := joinedHoverMarkdown(hover.Contents)
				if !ok {
					t.Fatalf("joinedHoverMarkdown failed: %#v", hover.Contents)
				}
				if tc.wantSince != "" {
					if !strings.Contains(content.Value, tc.wantSince) {
						t.Errorf("hover content %q does not contain %q", content.Value, tc.wantSince)
					}
				} else {
					if strings.Contains(content.Value, "Since Vim") {
						t.Errorf("hover content %q unexpectedly contains Since Vim", content.Value)
					}
				}
			}
		})
	}
}

func TestFunctionHoverHistory(t *testing.T) {
	for _, tc := range []struct {
		name      string
		source    string
		char      int
		wantSince string
	}{
		{
			name:      "indexof call",
			source:    "vim9script\necho indexof([1, 2], 'v:val == 2')\n",
			char:      8,
			wantSince: "Since Vim 9.0.0196",
		},
		{
			name:      "indexof method call",
			source:    "vim9script\necho [1, 2]->indexof('v:val == 2')\n",
			char:      15,
			wantSince: "Since Vim 9.0.0196",
		},
		{
			name:      "foreach call",
			source:    "vim9script\necho foreach([1, 2], (k, v) => v)\n",
			char:      8,
			wantSince: "Since Vim 9.1.0027",
		},
		{
			name:      "pre-9.0 function has no history",
			source:    "vim9script\necho len([1, 2])\n",
			char:      7,
			wantSince: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, kind := range []protocol.MarkupKind{protocol.MarkupKindMarkdown, protocol.MarkupKindPlainText} {
				instance, documentURI := openNavigationDocument(t, text.UTF16, tc.source)
				t.Cleanup(instance.stopAnalysis)
				instance.languageFeatures.hoverMarkup = kind

				hover, err := instance.Hover(context.Background(), &protocol.HoverParams{
					TextDocumentPositionParams: protocol.TextDocumentPositionParams{
						TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
						Position:     protocol.Position{Line: 1, Character: uint32(tc.char)},
					},
				})
				if err != nil || hover == nil {
					t.Fatalf("hover failed: %#v, err: %v", hover, err)
				}
				content, ok := joinedHoverMarkdown(hover.Contents)
				if !ok {
					t.Fatalf("joinedHoverMarkdown failed: %#v", hover.Contents)
				}
				if tc.wantSince != "" {
					if !strings.Contains(content.Value, tc.wantSince) {
						t.Errorf("hover content %q does not contain %q", content.Value, tc.wantSince)
					}
				} else {
					if strings.Contains(content.Value, "Since Vim") {
						t.Errorf("hover content %q unexpectedly contains Since Vim", content.Value)
					}
				}
			}
		})
	}
}

func TestAutocmdEventHoverHistory(t *testing.T) {
	for _, tc := range []struct {
		name      string
		source    string
		char      int
		wantSince string
	}{
		{
			name:      "WinResized event",
			source:    "autocmd WinResized * echo 1\n",
			char:      10,
			wantSince: "Since Vim 9.0.0917",
		},
		{
			name:      "case-insensitive winresized event",
			source:    "autocmd winresized * echo 1\n",
			char:      10,
			wantSince: "Since Vim 9.0.0917",
		},
		{
			name:      "TextChangedT event",
			source:    "autocmd TextChangedT * echo 1\n",
			char:      10,
			wantSince: "Since Vim 9.0.0756",
		},
		{
			name:      "pre-9.0 event has no history",
			source:    "autocmd BufRead * echo 1\n",
			char:      10,
			wantSince: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, kind := range []protocol.MarkupKind{protocol.MarkupKindMarkdown, protocol.MarkupKindPlainText} {
				instance, documentURI := openNavigationDocument(t, text.UTF16, tc.source)
				t.Cleanup(instance.stopAnalysis)
				instance.languageFeatures.hoverMarkup = kind

				hover, err := instance.Hover(context.Background(), &protocol.HoverParams{
					TextDocumentPositionParams: protocol.TextDocumentPositionParams{
						TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
						Position:     protocol.Position{Line: 0, Character: uint32(tc.char)},
					},
				})
				if err != nil || hover == nil {
					t.Fatalf("hover failed: %#v, err: %v", hover, err)
				}
				content, ok := joinedHoverMarkdown(hover.Contents)
				if !ok {
					t.Fatalf("joinedHoverMarkdown failed: %#v", hover.Contents)
				}
				if tc.wantSince != "" {
					if !strings.Contains(content.Value, tc.wantSince) {
						t.Errorf("hover content %q does not contain %q", content.Value, tc.wantSince)
					}
				} else {
					if strings.Contains(content.Value, "Since Vim") {
						t.Errorf("hover content %q unexpectedly contains Since Vim", content.Value)
					}
				}
			}
		})
	}
}
