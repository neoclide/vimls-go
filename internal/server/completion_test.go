package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

var benchmarkCompletionResult protocol.CompletionResult

func TestCompletionRuntimeImportAndColorschemePaths(t *testing.T) {
	root := t.TempDir()
	runtimePath := filepath.Join(root, "runtime")
	for path := range map[string]struct{}{
		filepath.Join(runtimePath, "import", "pkg", "alpha.vim"): {},
		filepath.Join(runtimePath, "colors", "dark.vim"):         {},
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("vim9script\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	rootURI := uri.File(root)
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{RootURI: &rootURI, InitializationOptions: protocol.LSPAny([]byte(fmt.Sprintf(`{"runtimepath":[%q]}`, runtimePath)))}); err != nil {
		t.Fatal(err)
	}
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	source := "vim9script\nimport 'pkg/al' as pkg\ncolorscheme dar\n"
	documentURI := uri.File(filepath.Join(root, "main.vim"))
	instance.documents.Open(documentURI.String(), 1, source)
	for _, test := range []struct {
		position protocol.Position
		label    string
	}{
		{position: protocol.Position{Line: 1, Character: uint32(len("import 'pkg/al"))}, label: "pkg/alpha.vim"},
		{position: protocol.Position{Line: 2, Character: uint32(len("colorscheme dar"))}, label: "dark"},
	} {
		result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: test.position}})
		if err != nil || !hasCompletionLabel(completionItems(t, result), test.label) {
			t.Fatalf("completion at %#v = %#v, %v", test.position, result, err)
		}
	}
	builtinSource := "vim9script\necho has('gui_')\necho expand('<cf')\n"
	builtinURI := uri.File(filepath.Join(root, "builtin.vim"))
	instance.documents.Open(builtinURI.String(), 1, builtinSource)
	for _, test := range []struct {
		line   uint32
		prefix string
		label  string
	}{
		{line: 1, prefix: "echo has('gui_", label: "gui_running"},
		{line: 2, prefix: "echo expand('<cf", label: "<cfile>"},
	} {
		result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: builtinURI}, Position: protocol.Position{Line: test.line, Character: uint32(len(test.prefix))}}})
		if err != nil || !hasCompletionLabel(completionItems(t, result), test.label) {
			t.Fatalf("builtin completion %q = %#v, %v", test.label, result, err)
		}
	}
}

func BenchmarkCompletionLatency(b *testing.B) {
	for _, size := range []struct {
		name  string
		bytes int
	}{{"1KiB", 1 << 10}, {"100KiB", 100 << 10}} {
		b.Run(size.name, func(b *testing.B) {
			instance := New(nil, nil, io.Discard)
			b.Cleanup(instance.stopAnalysis)
			line := "# completion benchmark padding keeps the symbol set realistic\n"
			source := "vim9script\nvar benchmark_value = 1\n" + strings.Repeat(line, max(1, (size.bytes-len("vim9script\nvar benchmark_value = 1\n"))/len(line))) + "echo strl"
			documentURI := uri.MustParse("file:///completion-benchmark.vim")
			snapshot := instance.documents.Open(documentURI.String(), 1, source)
			position, err := snapshot.Position(len(source), text.UTF16)
			if err != nil {
				b.Fatal(err)
			}
			params := &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
				Position:     protocol.Position{Line: uint32(position.Line), Character: uint32(position.Character)},
			}}
			result, err := instance.Completion(context.Background(), params)
			list, ok := result.(*protocol.CompletionList)
			if err != nil || !ok || !hasCompletion(list.Items, "strlen", protocol.CompletionItemKindFunction) {
				b.Fatalf("completion preflight = %#v, %v", result, err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(source)))
			b.ResetTimer()
			for b.Loop() {
				benchmarkCompletionResult, err = instance.Completion(context.Background(), params)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

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
	if got, snippet = completionFunctionSnippet("Done", nil, true); !snippet || got != "Done()$0" {
		t.Fatalf("zero-argument snippet = %q, %t", got, snippet)
	}
	legacy := syntax.Parse("function! Legacy(argument)\nendfunction\n")
	if got, snippet = completionUserFunctionSnippet(legacy, "Legacy", true); !snippet || got != "Legacy(${1:argument})$0" {
		t.Fatalf("legacy function snippet = %q, %t", got, snippet)
	}
}

func TestCompletionSnippetsRequireClientSupportAndMatchDialect(t *testing.T) {
	tests := []struct {
		name, source, label, want string
		line, character           uint32
	}{
		{name: "legacy block", source: "fun\n", label: "function", want: "function! ${1:Name}()\n\t$0\nendfunction", character: 3},
		{name: "Vim9 block", source: "vim9script\nde\n", label: "def", want: "def ${1:Name}()\n\t$0\nenddef", line: 1, character: 2},
		{name: "user function", source: "vim9script\ndef Call(value: number)\nenddef\necho Cal\n", label: "Call", want: "Call(${1:value})$0", line: 3, character: 8},
		{name: "builtin function", source: "vim9script\necho strl\n", label: "strlen", want: `strlen(${1:{string\}})$0`, line: 1, character: 9},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, snippets := range []bool{false, true} {
				instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
				instance.completion.snippet = snippets
				result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: test.line, Character: test.character},
				}})
				if err != nil {
					t.Fatal(err)
				}
				item := completionItemWithLabel(completionItems(t, result), test.label)
				if item == nil {
					t.Fatalf("completion %q missing", test.label)
				}
				edit, ok := item.TextEdit.(*protocol.TextEdit)
				if !ok {
					t.Fatalf("completion edit = %#v", item.TextEdit)
				}
				want := test.label
				if snippets {
					want = test.want
					if item.InsertTextFormat != protocol.InsertTextFormatSnippet {
						t.Fatalf("snippet format = %v", item.InsertTextFormat)
					}
				} else if item.InsertTextFormat == protocol.InsertTextFormatSnippet {
					t.Fatal("snippet returned without client support")
				}
				if edit.NewText != want {
					t.Fatalf("snippet support %t edit = %q, want %q", snippets, edit.NewText, want)
				}
			}
		})
	}
}

func completionItemWithLabel(items protocol.CompletionItemSlice, label string) *protocol.CompletionItem {
	for index := range items {
		if items[index].Label == label {
			return &items[index]
		}
	}
	return nil
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

func TestCompletionMappingArgumentsStayBeforeLHS(t *testing.T) {
	all := []string{"<buffer>", "<nowait>", "<silent>", "<special>", "<script>", "<expr>", "<unique>"}
	for _, command := range []string{"nmap ", "unmap "} {
		items := mappingCompletionItems(t, command, len(command))
		for _, label := range all {
			if item := completionItemWithLabel(items, label); item == nil || item.Kind != protocol.CompletionItemKindKeyword {
				t.Errorf("%q missing %q from %#v", command, label, items)
			}
		}
		if len(items) != len(all) {
			t.Errorf("%q returned %d items, want %d: %#v", command, len(items), len(all), items)
		}
	}

	clear := mappingCompletionItems(t, "mapclear ", len("mapclear "))
	if len(clear) != 1 || clear[0].Label != "<buffer>" {
		t.Fatalf("mapclear arguments = %#v", clear)
	}

	for _, label := range all {
		source := "nmap " + label + " "
		items := mappingCompletionItems(t, source, len(source))
		if completionItemWithLabel(items, label) != nil {
			t.Errorf("used mapping argument %q returned in %#v", label, items)
		}
	}

	for _, cursor := range []int{len("nmap <bu"), len("nmap <buffer>")} {
		source := "nmap <buffer>"
		items := mappingCompletionItems(t, source, cursor)
		item := completionItemWithLabel(items, "<buffer>")
		if item == nil {
			t.Fatalf("current mapping argument missing at %d from %#v", cursor, items)
		}
		edit, ok := item.TextEdit.(*protocol.TextEdit)
		if !ok || edit.Range != navigationRange(0, 5, 13) || edit.NewText != "<buffer>" {
			t.Fatalf("mapping argument edit at %d = %#v", cursor, item.TextEdit)
		}
	}

	for _, source := range []string{"nmap lhs", "nmap lhs rhs"} {
		if items := mappingCompletionItems(t, source, len(source)); len(items) != 0 {
			t.Errorf("mapping LHS/RHS %q returned %#v", source, items)
		}
	}
	if items := mappingCompletionItems(t, "nmap lhs", len("nmap ")); len(items) != 0 {
		t.Errorf("mapping LHS start returned %#v", items)
	}
	for _, source := range []string{"nmap <buffer> lhs", "nmap <buffer> <F5>"} {
		if items := mappingCompletionItems(t, source, len("nmap <buffer> ")); len(items) != 0 {
			t.Errorf("mapping LHS start in %q returned %#v", source, items)
		}
	}
}

func mappingCompletionItems(t *testing.T, source string, cursor int) protocol.CompletionItemSlice {
	t.Helper()
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Character: uint32(cursor)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return completionItems(t, result)
}

func TestCompletionHighlightArgumentsAndValues(t *testing.T) {
	keys := []string{"term=", "start=", "stop=", "cterm=", "ctermfg=", "ctermbg=", "ctermul=", "ctermfont=", "gui=", "guifg=", "guibg=", "guisp=", "font=", "NONE"}
	items := highlightCompletionItems(t, "highlight Normal ", len("highlight Normal "))
	for _, label := range keys {
		if item := completionItemWithLabel(items, label); item == nil || item.Kind != protocol.CompletionItemKindProperty {
			t.Errorf("highlight key %q missing from %#v", label, items)
		}
	}
	if len(items) != len(keys) {
		t.Fatalf("highlight keys = %d items, want %d: %#v", len(items), len(keys), items)
	}
	if item := completionItemWithLabel(items, "blend="); item != nil {
		t.Fatalf("unsupported highlight key returned: %#v", item)
	}
	items = highlightCompletionItems(t, "highlight link MyGroup My", len("highlight link MyGroup My"))
	if completionItemWithLabel(items, "MyGroup") == nil || completionItemWithLabel(items, "term=") != nil {
		t.Fatalf("highlight link group completion = %#v", items)
	}

	source := "highlight Normal cterm=bold"
	items = highlightCompletionItems(t, source, len("highlight Normal cterm"))
	item := completionItemWithLabel(items, "cterm=")
	if item == nil {
		t.Fatalf("current highlight key missing from %#v", items)
	}
	edit, ok := item.TextEdit.(*protocol.TextEdit)
	if !ok || edit.Range != navigationRange(0, 17, 23) || edit.NewText != "cterm=" {
		t.Fatalf("highlight key edit = %#v", item.TextEdit)
	}

	attributes := []string{"bold", "standout", "underline", "undercurl", "underdouble", "underdotted", "underdashed", "italic", "reverse", "inverse", "nocombine", "strikethrough", "NONE"}
	items = highlightCompletionItems(t, "highlight Normal cterm=", len("highlight Normal cterm="))
	for _, label := range attributes {
		if completionItemWithLabel(items, label) == nil {
			t.Errorf("highlight attribute %q missing from %#v", label, items)
		}
	}
	if len(items) != len(attributes) {
		t.Fatalf("highlight attributes = %d items, want %d: %#v", len(items), len(attributes), items)
	}

	source = "highlight Normal cterm=bold,underd"
	items = highlightCompletionItems(t, source, len(source))
	item = completionItemWithLabel(items, "underdouble")
	if item == nil {
		t.Fatalf("comma-separated highlight value missing from %#v", items)
	}
	edit, ok = item.TextEdit.(*protocol.TextEdit)
	if !ok || edit.Range != navigationRange(0, 28, 34) || edit.NewText != "underdouble" {
		t.Fatalf("highlight value edit = %#v", item.TextEdit)
	}
	source = "highlight Normal cterm = under"
	items = highlightCompletionItems(t, source, len(source))
	item = completionItemWithLabel(items, "underline")
	if item == nil {
		t.Fatalf("spaced highlight value missing from %#v", items)
	}
	edit, ok = item.TextEdit.(*protocol.TextEdit)
	if !ok || edit.Range != navigationRange(0, 25, 30) || edit.NewText != "underline" {
		t.Fatalf("spaced highlight value edit = %#v", item.TextEdit)
	}

	for _, test := range []struct {
		source string
		want   []string
	}{
		{source: "highlight Normal ctermfg=", want: []string{"fg", "bg", "ul", "Black", "DarkGrey", "LightYellow", "NONE"}},
		{source: "highlight Normal ctermfont=", want: []string{"NONE"}},
		{source: "highlight Normal guifg=", want: []string{"NONE", "background", "foreground", "SeaGreen", "SlateBlue", "Orange", "Violet"}},
		{source: "highlight Normal font=", want: []string{"NONE"}},
	} {
		items = highlightCompletionItems(t, test.source, len(test.source))
		for _, label := range test.want {
			if completionItemWithLabel(items, label) == nil {
				t.Errorf("%q value %q missing from %#v", test.source, label, items)
			}
		}
		if strings.Contains(test.source, "ctermfont") && len(items) != 1 {
			t.Errorf("ctermfont finite values = %#v", items)
		}
	}
	items = highlightCompletionItems(t, "highlight Normal cterm = ", len("highlight Normal cterm = "))
	if completionItemWithLabel(items, "bold") == nil {
		t.Fatalf("empty spaced highlight value = %#v", items)
	}
	for _, source := range []string{"highlight Normal start=", "highlight Normal stop=", "highlight Normal guifg='salmon pink'"} {
		if items := highlightCompletionItems(t, source, len(source)); len(items) != 0 {
			t.Errorf("arbitrary highlight value %q returned %#v", source, items)
		}
	}
}

func TestCompletionHighlightValueUsesUTF16EditRange(t *testing.T) {
	source := "vim9script\r\nhighlight Ω cterm=und"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 21},
	}})
	if err != nil {
		t.Fatal(err)
	}
	item := completionItemWithLabel(completionItems(t, result), "underline")
	if item == nil {
		t.Fatalf("UTF-16 highlight completion missing from %#v", completionItems(t, result))
	}
	edit, ok := item.TextEdit.(*protocol.TextEdit)
	if !ok || edit.Range != navigationRange(1, 18, 21) || edit.NewText != "underline" {
		t.Fatalf("UTF-16 highlight edit = %#v", item.TextEdit)
	}
}

func TestCompletionCommandSpecificFiniteValues(t *testing.T) {
	items := commandPartCompletionItems(t, "augroup Existing\naugroup END\naugroup Exi", 2, uint32(len("augroup Exi")))
	if completionItemWithLabel(items, "Existing") == nil || completionItemWithLabel(items, "END") != nil {
		t.Fatalf("augroup completions = %#v", items)
	}

	items = commandPartCompletionItems(t, "command -co", 0, uint32(len("command -co")))
	for _, label := range []string{"-complete=", "-completeopt=", "-count"} {
		if completionItemWithLabel(items, label) == nil {
			t.Errorf("user-command attribute %q missing from %#v", label, items)
		}
	}
	items = commandPartCompletionItems(t, "command -bang -ba", 0, uint32(len("command -bang -ba")))
	if completionItemWithLabel(items, "-bar") == nil || completionItemWithLabel(items, "-bang") != nil {
		t.Fatalf("used user-command attributes = %#v", items)
	}

	for _, test := range []struct {
		source string
		want   []string
	}{
		{source: "command -addr=lo", want: []string{"loaded_buffers"}},
		{source: "command -nargs=", want: []string{"0", "1", "_", "*", "?", "+"}},
		{source: "command -complete=fi", want: []string{"file", "file_in_path", "filetype", "filetypecmd"}},
		{source: "command -completeopt=", want: []string{"escape"}},
		{source: "command -compl=fi", want: []string{"file", "filetype"}},
		{source: "set ff=do", want: []string{"dos"}},
		{source: "set background=", want: []string{"light", "dark"}},
		{source: "set path+", want: []string{"+="}},
	} {
		items = commandPartCompletionItems(t, test.source, 0, uint32(len(test.source)))
		for _, label := range test.want {
			if completionItemWithLabel(items, label) == nil {
				t.Errorf("%q completion %q missing from %#v", test.source, label, items)
			}
		}
	}

	items = commandPartCompletionItems(t, "set ignorecase!", 0, uint32(len("set ignorecase!")))
	if completionItemWithLabel(items, "!") == nil || completionItemWithLabel(items, "=") != nil {
		t.Fatalf("boolean set operators = %#v", items)
	}
	items = commandPartCompletionItems(t, "set ignorecase<", 0, uint32(len("set ignorecase<")))
	if completionItemWithLabel(items, "<") == nil {
		t.Fatalf("boolean local-reset operator = %#v", items)
	}
	for _, source := range []string{"set encoding=utf", "command -complete=custom,Func"} {
		if items := commandPartCompletionItems(t, source, 0, uint32(len(source))); len(items) != 0 {
			t.Errorf("dynamic/body completion %q returned %#v", source, items)
		}
	}
}

func TestCompletionSetValueUsesUTF16CRLFRange(t *testing.T) {
	source := "augroup Ω\r\nset ff=do"
	items := commandPartCompletionItems(t, source, 1, uint32(len("set ff=do")))
	item := completionItemWithLabel(items, "dos")
	if item == nil {
		t.Fatalf("set value completion = %#v", items)
	}
	edit, ok := item.TextEdit.(*protocol.TextEdit)
	if !ok || edit.Range != navigationRange(1, 7, 9) || edit.NewText != "dos" {
		t.Fatalf("set value edit = %#v", item.TextEdit)
	}
}

func commandPartCompletionItems(t *testing.T, source string, line, character uint32) protocol.CompletionItemSlice {
	t.Helper()
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: line, Character: character},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return completionItems(t, result)
}

func highlightCompletionItems(t *testing.T, source string, cursor int) protocol.CompletionItemSlice {
	t.Helper()
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Character: uint32(cursor)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return completionItems(t, result)
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
