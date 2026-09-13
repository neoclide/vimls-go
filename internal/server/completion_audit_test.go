package server

import (
	"context"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
)

func TestImportAsCompletionUsesPathExpression(t *testing.T) {
	for _, test := range []struct {
		line string
		want bool
	}{
		{"import 'foo.vim' a", true},
		{"import autoload 'foo.vim' ", true},
		{`import "foo'bar.vim" a`, true},
		{`import 'foo"bar.vim' a`, true},
		{`import "foo\"bar.vim" a`, true},
		{"import 'foo''bar.vim' a", true},
		{"import 'foo.vim' 'bar' a", false},
		{"import 'foo.vim' as Alias ", false},
		{"import 'foo.vim a", false},
		{"import 'foo.vim' # a", false},
	} {
		t.Run(test.line, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\n"+test.line+"\n")
			result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: uint32(len(test.line))}}})
			if err != nil {
				t.Fatal(err)
			}
			if got := hasCompletionLabel(completionItems(t, result), "as"); got != test.want {
				t.Fatalf("as = %v, want %v", got, test.want)
			}
		})
	}
}

func TestTypeCompletionRetriggerDoesNotDuplicateSpace(t *testing.T) {
	for _, prefix := range []string{":", ": ", ": f"} {
		line := "var value" + prefix
		instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\n"+line+"\n")
		trigger := ":"
		result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: uint32(len(line))}}, Context: protocol.CompletionContext{TriggerKind: protocol.CompletionTriggerKindTriggerCharacter, TriggerCharacter: &trigger}})
		if err != nil {
			t.Fatal(err)
		}
		item := completionItemWithLabel(completionItems(t, result), "float")
		if item == nil {
			t.Fatal("missing float")
		}
		want := "float"
		if prefix == ":" {
			want = " float"
		}
		if got := completionMainEditFromItem(*item).text; got != want {
			t.Fatalf("prefix %q edit %q, want %q", prefix, got, want)
		}
	}
}

func TestImportExpressionExcludesNamespaces(t *testing.T) {
	source := "vim9script\nimport 'libs.vim' as Lib\nvar Local = 'local.vim'\nimport L\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 3, Character: 8}}})
	if err != nil {
		t.Fatal(err)
	}
	items := completionItems(t, result)
	if hasCompletionLabel(items, "Lib") || !hasCompletionLabel(items, "Local") {
		t.Fatalf("items = %#v", items)
	}
}

func TestImportCommandContextExcludesComment(t *testing.T) {
	source := "vim9script\nimport 'foo#bar.vim' # comment\n"
	file := syntax.Parse(source)
	if isImportCommandAt(file, strings.Index(source, "comment")+2) {
		t.Fatal("comment is import expression")
	}
	if !isImportCommandAt(file, strings.Index(source, " # comment")) {
		t.Fatal("hash in filename ended import context")
	}
}

func TestEmptyTypeNodeDoesNotContainCursor(t *testing.T) {
	if typeNodeContainsOffset(&syntax.Type{Span: syntax.Span{Start: 4, End: 4}}, 4) {
		t.Fatal("empty type contains cursor")
	}
	if !typeNodeContainsOffset(&syntax.Type{Span: syntax.Span{Start: 4, End: 9}}, 9) {
		t.Fatal("type end must support continued typing")
	}
}

func TestTypeAnnotationContextBoundaries(t *testing.T) {
	for _, test := range []struct {
		source string
		want   bool
	}{
		{"var value: |", true},
		{"var value: number = |", false},
		{"var [first: number, second: |] = []", true},
		{"for [first: number, second: |] in []\nendfor", true},
		{"def Func(value: |)\nenddef", true},
		{"def Func(value: number = |)\nenddef", false},
		{"def Func(): |\nenddef", true},
		{"type Alias = |", true},
		{"var Lambda = (value: |) => value", true},
		{"var Lambda = (value: number): | => value", true},
	} {
		t.Run(test.source, func(t *testing.T) {
			source := "vim9script\n" + test.source
			offset := strings.IndexByte(source, '|')
			source = strings.Replace(source, "|", "", 1)
			file := syntax.Parse(source)
			got := completionContextAt(file, offset) == completionContextType
			if got != test.want {
				t.Fatalf("type context = %v, want %v", got, test.want)
			}
		})
	}
}
