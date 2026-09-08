package server

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
)

func TestLocalCompletionResolvesDetailWithoutChangingInsertion(t *testing.T) {
	for _, test := range []struct{ source, label, detail string }{
		{"let local = expand('~')\necho loc§\n", "local", "variable: string"},
		{"vim9script\nvar local = 1\ndef Read()\n  var local = 'text'\n  echo loc§\nenddef\n", "local", "variable: string"},
		{"vim9script\nvar 𐐀local = 1\necho '😀é' | echo 𐐀lo§\n", "𐐀local", "variable: number"},
		{"vim9script\ndef Local(arg: number): string\n  return 'text'\nenddef\necho Loc§\n", "Local", "function: func(number): string"},
	} {
		t.Run(test.label+test.detail, func(t *testing.T) {
			offset := strings.Index(test.source, "§")
			source := strings.Replace(test.source, "§", "", 1)
			instance, documentURI := openNavigationDocument(t, text.UTF16, source)
			t.Cleanup(instance.stopAnalysis)
			instance.completion.snippet = true
			snapshot, _ := instance.documents.Snapshot(documentURI.String())
			position, _ := snapshot.Position(offset, text.UTF16)
			items := completionListRequest(t, instance, documentURI, uint32(position.Line), uint32(position.Character)).Items
			item := completionItemWithLabel(items, test.label)
			if item == nil {
				t.Fatalf("missing %q", test.label)
			}
			if detail, _ := item.Detail.Get(); strings.Contains(detail, ":") {
				t.Fatalf("eager detail: %q", detail)
			}
			resolved, err := instance.CompletionResolve(context.Background(), item)
			if err != nil {
				t.Fatal(err)
			}
			if detail, _ := resolved.Detail.Get(); detail != test.detail {
				t.Fatalf("detail = %q, want %q", detail, test.detail)
			}
			want := *item
			want.Detail = resolved.Detail
			if !reflect.DeepEqual(&want, resolved) {
				t.Fatal("resolve changed fields besides detail")
			}
			cached := instance.parsed[snapshot.URI()]
			if cached.analysis != nil {
				t.Fatal("resolve ran full analysis")
			}
		})
	}
}

func TestLocalCompletionResolveRejectsStaleOrInvalidTargets(t *testing.T) {
	for _, change := range []string{"edit", "same content reopen", "closed", "wrong content", "wrong span", "missing target"} {
		t.Run(change, func(t *testing.T) {
			source := "let local = 1\necho loc\n"
			instance, documentURI := openNavigationDocument(t, text.UTF16, source)
			t.Cleanup(instance.stopAnalysis)
			item := completionItemWithLabel(completionListRequest(t, instance, documentURI, 1, 8).Items, "local")
			if item == nil {
				t.Fatal("missing local")
			}
			switch change {
			case "edit":
				instance.documents.Change(documentURI.String(), 2, text.UTF16, []text.Change{{Text: "let local = 'new'\necho loc\n"}})
			case "same content reopen":
				instance.documents.Close(documentURI.String())
				instance.documents.Open(documentURI.String(), 1, source)
			case "closed":
				instance.documents.Close(documentURI.String())
			case "wrong content":
				item.Data = []byte(strings.Replace(string(item.Data), `"content":"`, `"content":"wrong`, 1))
			case "wrong span":
				item.Data = []byte(strings.Replace(string(item.Data), `"Start":4`, `"Start":5`, 1))
			case "missing target":
				item.Data = completionResolveTargetData(completionResolveLocal, "local")
			}
			resolved, err := instance.CompletionResolve(context.Background(), item)
			if err != nil || !reflect.DeepEqual(item, resolved) {
				t.Fatalf("stale resolve = %#v, %v", resolved, err)
			}
		})
	}
}

func TestLocalCompletionResolveChecksIdentityAndCancellationAfterWork(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		instance, documentURI := openNavigationDocument(t, text.UTF16, "let local = 1\necho loc\n")
		t.Cleanup(instance.stopAnalysis)
		item := completionItemWithLabel(completionListRequest(t, instance, documentURI, 1, 8).Items, "local")
		ctx, cancel := context.WithCancel(context.Background())
		instance.testHooks.beforeLocalCompletionResolve = func(*text.Snapshot) {
			if cancelRequest {
				cancel()
			} else {
				instance.documents.Change(documentURI.String(), 2, text.UTF16, []text.Change{{Text: "let local = 'changed'\necho loc\n"}})
			}
		}
		resolved, err := instance.CompletionResolve(ctx, item)
		cancel()
		if cancelRequest {
			if err != protocol.ErrRequestCancelled {
				t.Fatalf("cancellation = %v", err)
			}
		} else if err != nil || !reflect.DeepEqual(resolved, item) {
			t.Fatalf("stale result = %#v, %v", resolved, err)
		}
		if instance.completionPriority.active != 0 {
			t.Fatal("resolve retained completion priority")
		}
	}
}
