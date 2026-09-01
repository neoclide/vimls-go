package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
)

func TestCompletionFunctionSnippetEscapesAndDefaultIsPlain(t *testing.T) {
	if got, snippet := completionFunctionSnippet("Call", []string{"a$", `b}\\`}, false); snippet || got != "Call" {
		t.Fatalf("default snippet = %q, %t", got, snippet)
	}
	got, snippet := completionFunctionSnippet("Call", []string{"a$", `b}\\`}, true)
	if !snippet || got != `Call(${1:a\$}, ${2:b\}\\\\})$0` {
		t.Fatalf("snippet = %q", got)
	}
	file := syntax.Parse("vim9script\ndef Call(first: string, second: number)\nenddef\n")
	if got, snippet = completionUserFunctionSnippet(file, "Call", true); !snippet || got != "Call(${1:first}, ${2:second})$0" {
		t.Fatalf("user function snippet = %q, %t", got, snippet)
	}
}

func TestCompletionDeterministicAndBudgetIsIncomplete(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar abs = 1\necho a\n")
	params := &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 6},
	}}
	first, err := instance.Completion(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	second, err := instance.Completion(context.Background(), params)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("determinism: %v\n%#v\n%#v", err, first, second)
	}
	items := completionItems(t, first)
	if len(items) == 0 || items[0].Label != "abs" {
		t.Fatalf("local declaration was not ranked first: %#v", items)
	}
	absCount := 0
	for _, item := range items {
		if item.Label == "abs" {
			absCount++
		}
	}
	if absCount != 1 {
		t.Fatalf("local declaration did not shadow builtin: %#v", items)
	}

	clockCalls := 0
	instance.completionNow = func() time.Time {
		clockCalls++
		if clockCalls == 1 {
			return time.Unix(0, 0)
		}
		return time.Unix(0, 0).Add(completionBudget)
	}
	result, err := instance.Completion(context.Background(), params)
	if err != nil || !result.(*protocol.CompletionList).IsIncomplete {
		t.Fatalf("budget result = %#v, %v", result, err)
	}
}

func TestCompletionBuiltinFunctionPrefixDoesNotStopAtEarlierNames(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\necho strl\n")
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 9},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !hasCompletion(completionItems(t, result), "strlen", protocol.CompletionItemKindFunction) {
		t.Fatalf("strlen missing from %#v", completionItems(t, result))
	}
}

func TestCompletionIncludesScopedDeclarationPrefix(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "let g:globalValue = 1\nlet s:scriptValue = 2\necho g:glo\n")
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 10},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range completionItems(t, result) {
		if item.Label != "g:globalValue" {
			continue
		}
		edit, ok := item.TextEdit.(*protocol.TextEdit)
		if !ok || edit.Range != navigationRange(2, 5, 10) || edit.NewText != "g:globalValue" {
			t.Fatalf("scoped completion edit = %#v", item.TextEdit)
		}
		return
	}
	t.Fatalf("scoped declaration missing from %#v", completionItems(t, result))
}

func TestCompletionIncludesLegacyArgumentAndLocalPrefixes(t *testing.T) {
	source := "function! Test(argument)\n  let localValue = 1\n  echo a:arg\n  echo l:loc\nendfunction\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	for _, test := range []struct {
		line  uint32
		label string
	}{
		{line: 2, label: "a:argument"},
		{line: 3, label: "l:localValue"},
	} {
		result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: test.line, Character: 12},
		}})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range completionItems(t, result) {
			if item.Label != test.label {
				continue
			}
			edit, ok := item.TextEdit.(*protocol.TextEdit)
			if !ok || edit.Range != navigationRange(test.line, 7, 12) || edit.NewText != test.label {
				t.Fatalf("%s completion edit = %#v", test.label, item.TextEdit)
			}
			found = true
			break
		}
		if !found {
			t.Errorf("%s missing from %#v", test.label, completionItems(t, result))
		}
	}
}

func TestCompletionRespectsForwardVisibility(t *testing.T) {
	source := "vim9script\necho fut\necho Lat\nvar future = 1\ndef Later()\nenddef\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	for _, test := range []struct {
		line  uint32
		label string
		want  bool
	}{
		{line: 1, label: "future", want: false},
		{line: 2, label: "Later", want: true},
	} {
		result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: test.line, Character: 8},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if got := hasCompletionLabel(completionItems(t, result), test.label); got != test.want {
			t.Errorf("completion for %s = %t, want %t", test.label, got, test.want)
		}
	}
}

func TestCompletionLimitAndCancellationDuringCollection(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	snapshot := text.NewSnapshot("file:///limit.vim", 1, nil, "")
	candidates := make(map[string]completionCandidate, maxCompletionItems+1)
	for index := 0; index <= maxCompletionItems; index++ {
		label := fmt.Sprintf("item%04d", index)
		candidates[label] = completionCandidate{item: protocol.CompletionItem{Label: label}, score: 1}
	}
	list := instance.completionList(snapshot, text.UTF16, completionSelection{}, candidates)
	if !list.IsIncomplete || len(list.Items) != maxCompletionItems || list.Items[0].Label != "item0000" || list.Items[len(list.Items)-1].Label != "item1999" {
		t.Fatalf("limited completion = incomplete:%t len:%d first:%q last:%q", list.IsIncomplete, len(list.Items), list.Items[0].Label, list.Items[len(list.Items)-1].Label)
	}

	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\necho a\n")
	ctx, cancel := context.WithCancel(context.Background())
	clockCalls := 0
	instance.completionNow = func() time.Time {
		clockCalls++
		if clockCalls == 2 {
			cancel()
		}
		return time.Unix(0, 0)
	}
	_, err := instance.Completion(ctx, &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 6},
	}})
	if !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Fatalf("completion cancellation during collection = %v", err)
	}
}

func TestCompletionResolveIsStatelessAndPreservesFields(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	item := &protocol.CompletionItem{Label: "&ignorecase", Detail: protocol.NewOptional("kept"), Documentation: protocol.String("kept"), Data: []byte(`{"kept":true}`)}
	resolved, err := instance.CompletionResolve(context.Background(), item)
	if err != nil || resolved == item || resolved.Detail != item.Detail || resolved.Documentation != item.Documentation || !reflect.DeepEqual(resolved.Data, item.Data) {
		t.Fatalf("resolve = %#v, %v", resolved, err)
	}
	for _, label := range []string{"abs", "&ignorecase", "v:count", "echo"} {
		resolved, err = instance.CompletionResolve(context.Background(), &protocol.CompletionItem{Label: label})
		if err != nil || resolved == nil {
			t.Fatalf("resolve %s = %#v, %v", label, resolved, err)
		}
		detail, ok := resolved.Detail.Get()
		if !ok || detail == "" {
			t.Fatalf("resolve %s detail = %q, %t", label, detail, ok)
		}
		if label != "echo" && resolved.Documentation == nil {
			t.Fatalf("resolve %s documentation is missing", label)
		}
	}
	resolved, _ = instance.CompletionResolve(context.Background(), &protocol.CompletionItem{Label: "&ignorecase"})
	detail, _ := resolved.Detail.Get()
	if !strings.Contains(detail, "bool") || !strings.Contains(detail, "global") {
		t.Fatalf("option detail = %q", detail)
	}
}
