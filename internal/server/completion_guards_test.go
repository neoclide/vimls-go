package server

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
)

func TestCompletionUsesVim9GuardedReceiverTypes(t *testing.T) {
	const header = "vim9script\nclass Base\n  def BaseMember()\n  enddef\nendclass\nclass Derived extends Base\n  def DerivedMember()\n  enddef\nendclass\ndef F(value: Base)\n"
	for _, test := range []struct {
		name, body string
		derived    bool
	}{
		{"inside branch and UTF16 cursor", "  if instanceof(value, Derived)\n    echo '😀' value.<cursor>\n  endif\n", true},
		{"outside branch", "  if instanceof(value, Derived)\n  endif\n  echo value.<cursor>\n", false},
		{"call discards branch fact", "  if instanceof(value, Derived)\n    Change()\n    echo value.<cursor>\n  endif\n", false},
		{"negative guard else", "  if !instanceof(value, Derived)\n  else\n    echo value.<cursor>\n  endif\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			items := guardCompletionItems(t, header+test.body+"enddef\n")
			if !hasCompletion(items, "BaseMember", protocol.CompletionItemKindMethod) || hasCompletion(items, "DerivedMember", protocol.CompletionItemKindMethod) != test.derived {
				t.Fatalf("guarded members = %#v, want derived=%t and inherited base member", items, test.derived)
			}
		})
	}
}

func TestCompletionTypeGuardsFilterMethodCandidates(t *testing.T) {
	for _, test := range []struct {
		name, source, candidate string
		present                 bool
	}{
		{"string guard excludes list method", "vim9script\ndef F(value: any): any\n  if type(value) == v:t_string\n    return value->ad<cursor>\n  endif\nenddef\n", "add", false},
		{"list guard keeps list method", "vim9script\ndef F(value: any): any\n  if type(value) == v:t_list\n    return value->ad<cursor>\n  endif\nenddef\n", "add", true},
		{"incomplete arrow does not invalidate guard", "vim9script\ndef F(value: any): any\n  if type(value) == v:t_string\n    return value-><cursor>\n  endif\nenddef\n", "add", false},
		{"unclosed branch supports completion", "vim9script\ndef F(value: any): any\n  if type(value) == v:t_string\n    return value->ad<cursor>", "add", false},
		{"outside guard retains unknown methods", "vim9script\ndef F(value: any): any\n  if type(value) == v:t_string\n  endif\n  return value->ad<cursor>\nenddef\n", "add", true},
		{"assignment discards guard", "vim9script\ndef F(value: any): any\n  if type(value) == v:t_string\n    value = []\n    return value->ad<cursor>\n  endif\nenddef\n", "add", true},
		{"later call has not executed", "vim9script\ndef F(value: any): any\n  if type(value) == v:t_string\n    return Use(value->ad<cursor>, Change())\n  endif\nenddef\n", "add", false},
		{"earlier argument call discards guard", "vim9script\ndef F(value: any): any\n  if type(value) == v:t_string\n    return Use(Change(), value->ad<cursor>)\n  endif\nenddef\n", "add", true},
		{"lambda local guard", "vim9script\nvar Callback = (value: any) => {\n  if type(value) == v:t_string\n    return value->ad<cursor>\n  endif\n}\n", "add", false},
		{"custom functions stay available", "vim9script\ndef Custom(value: list<number>): any\n  return value\nenddef\ndef F(value: any): any\n  if type(value) == v:t_string\n    return value->Custom<cursor>\n  endif\nenddef\n", "Custom", true},
		{"legacy command retains methods", "vim9script\nlegacy echo 1->blob<cursor>\n", "blob2list", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			items := guardCompletionItems(t, test.source)
			if hasCompletionLabel(items, test.candidate) != test.present {
				t.Fatalf("%s present=%t, want %t; items = %#v", test.candidate, hasCompletionLabel(items, test.candidate), test.present, items)
			}
		})
	}
}

func guardCompletionItems(t *testing.T, source string) []protocol.CompletionItem {
	t.Helper()
	offset := strings.Index(source, "<cursor>")
	if offset < 0 {
		t.Fatal("missing cursor marker")
	}
	source = strings.Replace(source, "<cursor>", "", 1)
	instance, uri := openNavigationDocument(t, text.UTF16, source)
	position, err := text.NewSnapshot(string(uri), 1, nil, source).Position(offset, text.UTF16)
	if err != nil {
		t.Fatal(err)
	}
	return completionListRequest(t, instance, uri, uint32(position.Line), uint32(position.Character)).Items
}

func TestCompletionFiltersBuiltinMethodReceiversByCommandDialect(t *testing.T) {
	for _, test := range []struct {
		name, source, typed, candidate string
		present                        bool
	}{
		{
			name:      "number excludes blob receiver",
			source:    "vim9script\ndef F()\n  return 1->blob\nenddef\n",
			typed:     "blob",
			candidate: "blob2list",
			present:   false,
		},
		{
			name:      "number remains bool compatible",
			source:    "vim9script\ndef F()\n  return 1->digraph\nenddef\n",
			typed:     "digraph",
			candidate: "digraph_getlist",
			present:   true,
		},
		{
			name:      "nonfirst receiver uses its declared argument",
			source:    "vim9script\ndef F()\n  return []->globpath\nenddef\n",
			typed:     "globpath",
			candidate: "globpath",
			present:   false,
		},
		{
			name:      "nonfirst receiver keeps matching category",
			source:    "vim9script\ndef F()\n  return 'x'->globpath\nenddef\n",
			typed:     "globpath",
			candidate: "globpath",
			present:   true,
		},
		{
			name:      "unknown receiver remains available",
			source:    "vim9script\ndef F(value: any)\n  return value->blob\nenddef\n",
			typed:     "blob",
			candidate: "blob2list",
			present:   true,
		},
		{
			name:      "vim9cmd filters in legacy root",
			source:    "vim9cmd echo 1->blob\n",
			typed:     "blob",
			candidate: "blob2list",
			present:   false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			start := strings.LastIndex(test.source, "->"+test.typed)
			if start < 0 {
				t.Fatalf("missing typed receiver %q", test.typed)
			}
			offset := start + len("->"+test.typed)
			line := uint32(strings.Count(test.source[:offset], "\n"))
			character := uint32(offset - strings.LastIndex(test.source[:offset], "\n") - 1)
			items := completionListRequest(t, instance, documentURI, line, character).Items
			if got := hasCompletionLabel(items, test.candidate); got != test.present {
				t.Fatalf("%s completion = %t, want %t; items = %#v", test.candidate, got, test.present, items)
			}
		})
	}
}
