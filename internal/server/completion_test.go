package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
	runtimePath := filepath.Join(t.TempDir(), "runtime")
	for path := range map[string]struct{}{
		filepath.Join(runtimePath, "import", "pkg", "alpha.vim"): {},
		filepath.Join(runtimePath, "colors", "dark.vim"):         {},
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		content := []byte("vim9script\n")
		if strings.HasSuffix(path, "alpha.vim") {
			content = []byte("vim9script\nexport var RuntimeMember = 1\n")
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	rootURI := uri.File(root)
	options, err := json.Marshal(map[string]any{"runtimepath": []string{runtimePath}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{RootURI: &rootURI, InitializationOptions: protocol.LSPAny(options)}); err != nil {
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
	builtinSource := "vim9script\necho has('gui_')\necho expand('<cf')\necho has('nv')\n"
	builtinURI := uri.File(filepath.Join(root, "builtin.vim"))
	instance.documents.Open(builtinURI.String(), 1, builtinSource)
	for _, test := range []struct {
		line   uint32
		prefix string
		label  string
	}{
		{line: 1, prefix: "echo has('gui_", label: "gui_running"},
		{line: 2, prefix: "echo expand('<cf", label: "<cfile>"},
		{line: 3, prefix: "echo has('nv", label: "nvim"},
	} {
		result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: builtinURI}, Position: protocol.Position{Line: test.line, Character: uint32(len(test.prefix))}}})
		if err != nil || !hasCompletionLabel(completionItems(t, result), test.label) {
			t.Fatalf("builtin completion %q = %#v, %v", test.label, result, err)
		}
	}
	memberSource := "vim9script\nimport 'pkg/alpha.vim' as pkg\necho pkg.\n"
	memberURI := uri.File(filepath.Join(root, "member.vim"))
	instance.documents.Open(memberURI.String(), 1, memberSource)
	memberParams := &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: memberURI}, Position: protocol.Position{Line: 2, Character: 9}}}
	memberResult, err := instance.Completion(context.Background(), memberParams)
	if err != nil || !hasCompletionLabel(completionItems(t, memberResult), "RuntimeMember") {
		t.Fatalf("runtime member completion = %#v, %v", memberResult, err)
	}
	if err := instance.applyWorkspaceConfiguration(context.Background(), []byte(`{"suggest":{"excludeRuntimePath":true}}`)); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		position protocol.Position
		label    string
	}{
		{position: protocol.Position{Line: 1, Character: uint32(len("import 'pkg/al"))}, label: "pkg/alpha.vim"},
		{position: protocol.Position{Line: 2, Character: uint32(len("colorscheme dar"))}, label: "dark"},
	} {
		result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: test.position}})
		if err != nil {
			t.Fatal(err)
		}
		if hasCompletionLabel(completionItems(t, result), test.label) {
			t.Fatalf("excluded runtime completion %q = %#v", test.label, completionItems(t, result))
		}
	}
	memberResult, err = instance.Completion(context.Background(), memberParams)
	if err != nil {
		t.Fatal(err)
	}
	if hasCompletionLabel(completionItems(t, memberResult), "RuntimeMember") {
		t.Fatalf("excluded runtime member completion = %#v", memberResult)
	}
}

func TestCompletionExcludesRuntimePathOnlyItemsButKeepsWorkspaceItems(t *testing.T) {
	runtimeRoot := t.TempDir()
	workspaceRoot := filepath.Join(runtimeRoot, "project")
	writeWorkspaceFile(t, runtimeRoot, "plugin/runtime.vim", "function! RuntimeOnly()\nendfunction\nlet g:RuntimeValue = 1\ncommand RuntimeCommand echo 'runtime'\n")
	writeWorkspaceFile(t, workspaceRoot, "plugin/workspace.vim", "function! WorkspaceOnly()\nendfunction\nlet g:WorkspaceValue = 1\ncommand WorkspaceCommand echo 'workspace'\n")
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	rootURI := uri.File(workspaceRoot)
	options := protocol.LSPAny(fmt.Appendf(nil, `{"runtimepath":[%q]}`, runtimeRoot))
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{RootURI: &rootURI, InitializationOptions: options}); err != nil {
		t.Fatal(err)
	}
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	documentURI := uri.File(filepath.Join(workspaceRoot, "main.vim"))
	instance.documents.Open(documentURI.String(), 1, "echo Runtime\n")
	complete := func(source string, position protocol.Position) protocol.CompletionItemSlice {
		instance.documents.Open(documentURI.String(), 1, source)
		result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: position}})
		if err != nil {
			t.Fatal(err)
		}
		return completionItems(t, result)
	}
	if items := complete("echo Runtime\n", protocol.Position{Line: 0, Character: 12}); !hasCompletionLabel(items, "RuntimeOnly") {
		t.Fatalf("default runtime completion = %#v", items)
	}
	if err := instance.applyWorkspaceConfiguration(context.Background(), []byte(`{"vim":{"suggest":{"excludeRuntimePath":true}}}`)); err != nil {
		t.Fatal(err)
	}
	if items := complete("echo Runtime\n", protocol.Position{Line: 0, Character: 12}); hasCompletionLabel(items, "RuntimeOnly") {
		t.Fatalf("runtime-only function leaked = %#v", items)
	}
	if items := complete("echo Workspace\n", protocol.Position{Line: 0, Character: 14}); !hasCompletionLabel(items, "WorkspaceOnly") {
		t.Fatalf("workspace function missing = %#v", items)
	}
	if items := complete("Runtime\n", protocol.Position{Line: 0, Character: 7}); hasCompletionLabel(items, "RuntimeCommand") {
		t.Fatalf("runtime-only command leaked = %#v", items)
	}
	if items := complete("Workspace\n", protocol.Position{Line: 0, Character: 9}); !hasCompletionLabel(items, "WorkspaceCommand") {
		t.Fatalf("workspace command missing = %#v", items)
	}
	if err := instance.applyWorkspaceConfiguration(context.Background(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if items := complete("echo Runtime\n", protocol.Position{Line: 0, Character: 12}); !hasCompletionLabel(items, "RuntimeOnly") {
		t.Fatalf("empty settings did not restore runtime completion = %#v", items)
	}
}

func TestCompletionIncludesVim9ExplicitGlobalFunction(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "plugin/globals.vim", "vim9script\ndef g:Vim9GlobalRun(arg: number)\nenddef\n")
	mainPath := writeWorkspaceFile(t, root, "main.vim", "vim9script\ng:Vim9Glo\n")
	instance := initializeWorkspaceServer(t, root)
	documentURI := uri.File(mainPath)
	instance.documents.Open(documentURI.String(), 1, "vim9script\ng:Vim9Glo\n")
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: 1, Character: 9},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if items := completionItems(t, result); !hasCompletionLabel(items, "g:Vim9GlobalRun") {
		t.Fatalf("Vim9 global function completion = %#v", items)
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

func TestCompletionFunctionAtCallableCommandStart(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		line      uint32
		character uint32
		want      bool
	}{
		{
			name:      "Vim9 def without call",
			source:    "vim9script\nexport def Tabpage_ids(): void\n  for nr in range(1, tabpagenr('$'))\n    if gettabvar(nr, '__coc_tid', -1) == -1\n      settabvar(nr, '__coc_tid', tab_id)\n      T\n      tab_id += 1\n    endif\n  endfor\nenddef\n",
			line:      5,
			character: 7,
			want:      true,
		},
		{
			name:      "legacy function without call",
			source:    "vim9script\nexport def Tabpage_ids(): void\nenddef\nexport function Eval(expr) abort\n  T\nendfunction\n",
			line:      4,
			character: 3,
			want:      false,
		},
		{
			name:      "legacy function with call",
			source:    "vim9script\nexport def Tabpage_ids(): void\nenddef\nexport function Eval(expr) abort\n  call T\nendfunction\n",
			line:      4,
			character: 8,
			want:      true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			items := completionListRequest(t, instance, documentURI, test.line, test.character).Items
			if got := hasCompletion(items, "Tabpage_ids", protocol.CompletionItemKindFunction); got != test.want {
				t.Fatalf("Tabpage_ids completion = %t, want %t; items = %#v", got, test.want, items)
			}
		})
	}
}

func TestCompletionFunctionsInAutocmdBody(t *testing.T) {
	tests := []struct {
		name, source, label string
		kind                protocol.CompletionItemKind
	}{
		{name: "legacy call", source: "autocmd WinEnter          * call c", label: "ceil", kind: protocol.CompletionItemKindFunction},
		{name: "legacy expression", source: "autocmd WinEnter * echo c", label: "ceil", kind: protocol.CompletionItemKindFunction},
		{name: "legacy command", source: "autocmd WinEnter * ec", label: "echo", kind: protocol.CompletionItemKindKeyword},
		{name: "Vim9 direct call", source: "vim9script\nautocmd WinEnter * c", label: "ceil", kind: protocol.CompletionItemKindFunction},
		{name: "Vim9 expression", source: "vim9script\nautocmd WinEnter * echo c", label: "ceil", kind: protocol.CompletionItemKindFunction},
		{name: "Vim9 command", source: "vim9script\nautocmd WinEnter * ec", label: "echo", kind: protocol.CompletionItemKindKeyword},
		{name: "legacy empty body", source: "autocmd WinEnter          * ", label: "call", kind: protocol.CompletionItemKindKeyword},
		{name: "legacy empty body after modifier", source: "autocmd WinEnter * ++once ", label: "call", kind: protocol.CompletionItemKindKeyword},
		{name: "legacy empty body after bar", source: "autocmd WinEnter * | ", label: "call", kind: protocol.CompletionItemKindKeyword},
		{name: "Vim9 empty body", source: "vim9script\nautocmd WinEnter * ", label: "echo", kind: protocol.CompletionItemKindKeyword},
		{name: "Vim9 empty body after modifier", source: "vim9script\nautocmd WinEnter * ++once ", label: "echo", kind: protocol.CompletionItemKindKeyword},
		{name: "Vim9 empty body after bar", source: "vim9script\nautocmd WinEnter * | ", label: "echo", kind: protocol.CompletionItemKindKeyword},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			line := uint32(strings.Count(test.source, "\n"))
			character := uint32(len(test.source) - strings.LastIndexByte(test.source, '\n') - 1)
			items := completionListRequest(t, instance, documentURI, line, character).Items
			if !hasCompletion(items, test.label, test.kind) {
				t.Fatalf("%s completion missing; items = %#v", test.label, items)
			}
		})
	}
}

func TestCompletionFunctionsInMappingCommandBody(t *testing.T) {
	functionSource := "function! CocActionAsync(...) abort\nendfunction\n"
	tests := []struct {
		name, mapping, label string
		kind                 protocol.CompletionItemKind
		cursor               int
	}{
		{name: "legacy call", mapping: "vnoremap <silent> <Plug>(coc-range-select) :<C-u>call CocA('rangeSelect', visualmode(), v:true)<CR>", label: "CocActionAsync", kind: protocol.CompletionItemKindFunction, cursor: len("vnoremap <silent> <Plug>(coc-range-select) :<C-u>call CocA")},
		{name: "legacy command", mapping: "vnoremap <silent> <Plug>(coc-range-select) :<C-u>ec", label: "echo", kind: protocol.CompletionItemKindKeyword},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := functionSource + test.mapping
			instance, documentURI := openNavigationDocument(t, text.UTF16, source)
			cursor := test.cursor
			if cursor == 0 {
				cursor = len(test.mapping)
			}
			items := completionListRequest(t, instance, documentURI, 2, uint32(cursor)).Items
			if !hasCompletion(items, test.label, test.kind) {
				t.Fatalf("%s completion missing; items = %#v", test.label, items)
			}
		})
	}
	vim9Source := "vim9script\ndef LocalAction(): void\nenddef\nvnoremap <silent> <Plug>(local-action) :<C-u>Loc"
	instance, documentURI := openNavigationDocument(t, text.UTF16, vim9Source)
	items := completionListRequest(t, instance, documentURI, 3, uint32(len("vnoremap <silent> <Plug>(local-action) :<C-u>Loc"))).Items
	if !hasCompletion(items, "LocalAction", protocol.CompletionItemKindFunction) {
		t.Fatalf("Vim9 mapping function completion missing; items = %#v", items)
	}
	if items := mappingCompletionItems(t, "nmap lhs CocA", len("nmap lhs CocA")); len(items) != 0 {
		t.Fatalf("ordinary mapping RHS returned completion items: %#v", items)
	}
}

func TestCompletionVim9DefStatementHeadIncludesCommandsAndFunctions(t *testing.T) {
	tests := []struct {
		prefix, function string
	}{
		{"", "flattennew"},
		{"f", "flattennew"},
		{"fl", "flattennew"},
		{"flat", "flattennew"},
		{"v", "values"},
		{"val", "values"},
		{"g", "get"},
	}
	for _, test := range tests {
		t.Run("prefix "+test.prefix, func(t *testing.T) {
			source := "vim9script\nexport def GetNamespaceTypes(ns: number): list<string>\n  if ns == -1\n    return values({})->flattennew(1)\n  endif\n  " + test.prefix + "\nenddef\n"
			instance, documentURI := openNavigationDocument(t, text.UTF16, source)
			items := completionListRequest(t, instance, documentURI, 5, uint32(len("  ")+len(test.prefix))).Items
			if !hasCompletion(items, test.function, protocol.CompletionItemKindFunction) {
				t.Fatalf("%s completion missing for prefix %q; items = %#v", test.function, test.prefix, items)
			}
			if test.prefix == "" && !hasCompletion(items, "return", protocol.CompletionItemKindKeyword) {
				t.Fatalf("return command missing at empty statement head; items = %#v", items)
			}
		})
	}
}

func TestCompletionVim9DefStatementHeadIncludesUserCommands(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "plugin/commands.vim", "command! RunThing echo 1\n")
	instance := newRootedServer(t, root)
	source := "vim9script\ndef Main(): void\n  Ru\nenddef\n"
	documentURI := uri.File(filepath.Join(root, "main.vim"))
	instance.documents.Open(documentURI.String(), 1, source)
	items := completionListRequest(t, instance, documentURI, 2, uint32(len("  Ru"))).Items
	if !hasCompletion(items, "RunThing", protocol.CompletionItemKindFunction) {
		t.Fatalf("user command completion missing in Vim9 def statement context: %#v", items)
	}
}

func TestCompletionPreselectsCommonCommandByDialect(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		line      uint32
		character uint32
		want      string
	}{
		{name: "legacy call", source: "c\n", line: 0, character: 1, want: "call"},
		{name: "legacy endif", source: "e\n", line: 0, character: 1, want: "endif"},
		{name: "Vim9 const", source: "vim9script\ndef Main(): void\n  c\nenddef\n", line: 2, character: 3, want: "const"},
		{name: "legacy function in Vim9 file", source: "vim9script\nfunction Main() abort\n  c\nendfunction\n", line: 2, character: 3, want: "call"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			instance.completion.preselect = true
			items := completionListRequest(t, instance, documentURI, test.line, test.character).Items
			if len(items) == 0 || items[0].Label != test.want {
				t.Fatalf("first completion = %#v, want %q", items, test.want)
			}
			if preselected, ok := items[0].Preselect.Get(); !ok || !preselected {
				t.Fatalf("%s preselect = %t, %t", items[0].Label, preselected, ok)
			}
		})
	}
}

func TestCompletionFunctionAttributes(t *testing.T) {
	tests := []struct {
		name, source, prefix string
		line                 uint32
		want                 []string
		reject               []string
	}{
		{name: "legacy empty", source: "function! Foo() \nendfunction\n", line: 0, want: []string{"range", "abort", "dict", "closure"}},
		{name: "legacy prefix", source: "function! Foo() cl\nendfunction\n", prefix: "cl", line: 0, want: []string{"closure"}},
		{name: "legacy continuation", source: "function! Foo()\n  \\ cl\nendfunction\n", prefix: "cl", line: 1, want: []string{"closure"}},
		{name: "Vim9 legacy function", source: "vim9script\nfunction Foo() di\nendfunction\n", prefix: "di", line: 1, want: []string{"dict"}},
		{name: "used attributes", source: "function! Foo() range abort d\nendfunction\n", prefix: "d", line: 0, want: []string{"dict"}, reject: []string{"range", "abort"}},
		{name: "Vim9 def", source: "vim9script\ndef Foo() cl\nenddef\n", prefix: "cl", line: 1, reject: []string{"closure"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			lineStart := 0
			for line := uint32(0); line < test.line; line++ {
				lineStart += strings.IndexByte(test.source[lineStart:], '\n') + 1
			}
			lineEnd := strings.IndexByte(test.source[lineStart:], '\n')
			if lineEnd < 0 {
				lineEnd = len(test.source) - lineStart
			}
			items := completionListRequest(t, instance, documentURI, test.line, uint32(lineEnd)).Items
			for _, label := range test.want {
				if !hasCompletion(items, label, protocol.CompletionItemKindKeyword) {
					t.Fatalf("%s completion missing for prefix %q; items = %#v", label, test.prefix, items)
				}
			}
			for _, label := range test.reject {
				if hasCompletion(items, label, protocol.CompletionItemKindKeyword) {
					t.Fatalf("%s completion unexpectedly present for prefix %q; items = %#v", label, test.prefix, items)
				}
			}
		})
	}
}

func TestCompletionBuiltinFunctionsAfterMethodArrow(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		line      uint32
		character uint32
		start     uint32
	}{
		{"empty prefix", "vim9script\ndef GetNamespaceTypes(): list<string>\n  return values({})->\nenddef\n", 2, uint32(len("  return values({})->")), uint32(len("  return values({})->"))},
		{"partial prefix", "vim9script\ndef GetNamespaceTypes(): list<string>\n  return values({})->flat\nenddef\n", 2, uint32(len("  return values({})->flat")), uint32(len("  return values({})->"))},
		{"space recovery", "vim9script\ndef GetNamespaceTypes(): list<string>\n  return values({})->  flat\nenddef\n", 2, uint32(len("  return values({})->  flat")), uint32(len("  return values({})->  "))},
		{"continuation before arrow", "vim9script\ndef GetNamespaceTypes(): list<string>\n  return values({})\n    ->flat\nenddef\n", 3, uint32(len("    ->flat")), uint32(len("    ->"))},
		{"inside list literal", "vim9script\ndef GetNamespaceTypes(): list<string>\n  var x = [values({})->]\n  return x\nenddef\n", 2, uint32(len("  var x = [values({})->")), uint32(len("  var x = [values({})->"))},
		{"inside call argument", "vim9script\ndef GetNamespaceTypes(): list<string>\n  var x = len(values({})->)\n  return []\nenddef\n", 2, uint32(len("  var x = len(values({})->")), uint32(len("  var x = len(values({})->"))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := test.source
			instance, documentURI := openNavigationDocument(t, text.UTF16, source)
			instance.completion.snippet = true
			items := completionListRequest(t, instance, documentURI, test.line, test.character).Items
			item := completionItemWithLabel(items, "flattennew")
			if item == nil || item.Kind != protocol.CompletionItemKindFunction {
				t.Fatalf("flattennew completion = %#v; items = %#v", item, items)
			}
			if edit := completionMainEditFromItem(*item); edit.text != "flattennew()$0" {
				t.Fatalf("flattennew edit = %q", edit.text)
			} else if edit.replace.Start != (protocol.Position{Line: test.line, Character: test.start}) {
				t.Fatalf("flattennew edit start = %#v, want line %d character %d", edit.replace.Start, test.line, test.start)
			}
			if hasCompletion(items, "argc", protocol.CompletionItemKindFunction) {
				t.Fatalf("non-method builtin argc was offered: %#v", items)
			}
		})
	}
}

func TestCompletionVim9DefLeadingColonForcesCommands(t *testing.T) {
	source := "vim9script\ndef Main(): void\n  var foo_var = 1\n  :f\nenddef\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	items := completionListRequest(t, instance, documentURI, 3, uint32(len("  :f"))).Items
	if !hasCompletion(items, "finish", protocol.CompletionItemKindKeyword) {
		t.Fatalf("finish command missing after leading colon: %#v", items)
	}
	if hasCompletion(items, "foo_var", protocol.CompletionItemKindVariable) {
		t.Fatalf("local variable foo_var was offered after leading colon: %#v", items)
	}
}

func TestCompletionVim9DefEnddefPositionForcesCommand(t *testing.T) {
	source := "vim9script\ndef Main(): void\n  var foo_var = 1\n  enddef\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	items := completionListRequest(t, instance, documentURI, 3, uint32(len("  enddef"))).Items
	if !hasCompletion(items, "enddef", protocol.CompletionItemKindKeyword) {
		t.Fatalf("enddef command missing at enddef position: %#v", items)
	}
	if hasCompletion(items, "foo_var", protocol.CompletionItemKindVariable) {
		t.Fatalf("local variable foo_var was offered at enddef position: %#v", items)
	}
}

func TestCompletionMethodArrowQualifiedFunctionNames(t *testing.T) {
	tests := []struct {
		name, source, label string
		line                uint32
		character           uint32
	}{
		{
			name:      "script local",
			source:    "vim9script\ndef s:AddOne(value: number): number\n  return value + 1\nenddef\ndef Main(): number\n  return 1->s:A\nenddef\n",
			label:     "s:AddOne",
			line:      5,
			character: uint32(len("  return 1->s:A")),
		},
		{
			name:      "autoload",
			source:    "function! foo#bar#Transform(items) abort\n  return a:items\nendfunction\necho []->foo#bar#Tra\n",
			label:     "foo#bar#Transform",
			line:      3,
			character: uint32(len("echo []->foo#bar#Tra")),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			items := completionListRequest(t, instance, documentURI, test.line, test.character).Items
			item := completionItemWithLabel(items, test.label)
			if item == nil || item.Kind != protocol.CompletionItemKindFunction {
				t.Fatalf("%s completion missing; items = %#v", test.label, items)
			}
			edit := completionMainEditFromItem(*item)
			if edit.text != test.label || edit.replace.End != (protocol.Position{Line: test.line, Character: test.character}) {
				t.Fatalf("%s completion edit = %#v", test.label, edit)
			}
			wantStart := test.character - uint32(len("s:A"))
			if test.name == "autoload" {
				wantStart = test.character - uint32(len("foo#bar#Tra"))
			}
			if edit.replace.Start != (protocol.Position{Line: test.line, Character: wantStart}) {
				t.Fatalf("%s completion edit start = %#v, want character %d", test.label, edit.replace.Start, wantStart)
			}
		})
	}
}

func TestCompletionMethodArrowExcludesLocalZeroArgumentFunction(t *testing.T) {
	source := "vim9script\ndef NoArgs(): void\nenddef\ndef WithArg(value: any): void\nenddef\ndef Main(): void\n  []->\nenddef\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	items := completionListRequest(t, instance, documentURI, 6, uint32(len("  []->"))).Items
	if hasCompletion(items, "NoArgs", protocol.CompletionItemKindFunction) {
		t.Fatalf("zero-argument function was offered as a method: %#v", items)
	}
	if !hasCompletion(items, "WithArg", protocol.CompletionItemKindFunction) {
		t.Fatalf("method-compatible local function missing: %#v", items)
	}
}

func TestCompletionUserFunctionAfterMethodArrow(t *testing.T) {
	source := "vim9script\ndef Transform(items: list<any>, depth: number): list<any>\n  return items\nenddef\ndef Main(): list<any>\n  return values({})->Tra\nenddef\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	instance.completion.snippet = true
	items := completionListRequest(t, instance, documentURI, 5, uint32(len("  return values({})->Tra"))).Items
	item := completionItemWithLabel(items, "Transform")
	if item == nil || item.Kind != protocol.CompletionItemKindFunction {
		t.Fatalf("Transform completion = %#v; items = %#v", item, items)
	}
	if edit := completionMainEditFromItem(*item); edit.text != "Transform(${1:depth})$0" {
		t.Fatalf("Transform edit = %q", edit.text)
	}
}

func TestCompletionIndexedFunctionSnippets(t *testing.T) {
	root := t.TempDir()
	runtimePath := t.TempDir()
	writeWorkspaceFile(t, runtimePath, filepath.Join("autoload", "acme", "tool.vim"), "function! acme#tool#Run(first, second)\nendfunction\n")
	writeWorkspaceFile(t, runtimePath, filepath.Join("autoload", "pkg", "search.vim"), "vim9script\nexport def Find(query: string, count = 1): bool\n  return true\nenddef\n")
	options, err := json.Marshal(map[string]any{"runtimepath": []string{runtimePath}})
	if err != nil {
		t.Fatal(err)
	}
	snippets := true
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	rootURI := uri.File(root)
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI:               &rootURI,
		InitializationOptions: protocol.LSPAny(options),
		Capabilities: protocol.ClientCapabilities{TextDocument: &protocol.TextDocumentClientCapabilities{
			Completion: &protocol.CompletionClientCapabilities{CompletionItem: &protocol.ClientCompletionItemOptions{SnippetSupport: &snippets}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceDelay = 0
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()

	for _, test := range []struct {
		name, source, label, want string
		line, character           uint32
	}{
		{name: "legacy autoload", source: "echo acme#tool#R\n", label: "acme#tool#Run", want: "acme#tool#Run(${1:first}, ${2:second})$0", character: uint32(len("echo acme#tool#R"))},
		{name: "Vim9 autoload", source: "vim9script\necho pkg#search#F\n", label: "pkg#search#Find", want: "pkg#search#Find(${1:query}, ${2:count})$0", line: 1, character: uint32(len("echo pkg#search#F"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			documentURI := uri.File(filepath.Join(root, strings.ReplaceAll(test.name, " ", "-")+".vim"))
			instance.documents.Open(documentURI.String(), 1, test.source)
			result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: test.line, Character: test.character},
			}})
			if err != nil {
				t.Fatal(err)
			}
			item := completionItemWithLabel(completionItems(t, result), test.label)
			if item == nil || item.InsertTextFormat != protocol.InsertTextFormatSnippet || completionMainEditFromItem(*item).text != test.want {
				t.Fatalf("indexed function completion %q = %#v", test.label, item)
			}
		})
	}
}

func TestCompletionSnippetsRequireClientSupportAndMatchDialect(t *testing.T) {
	tests := []struct {
		name, source, label, want string
		line, character           uint32
	}{
		{name: "legacy block (config role standalone)", source: "fun\n", label: "function", want: "function ${1:Name}()\n\t$0\nendfunction", character: 3},
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

func TestCompletionPortableSnippetsAndVim9Blocks(t *testing.T) {
	tests := []struct {
		name, source, label, want string
		line, character           uint32
		command                   bool // command items stay available without snippet support
	}{
		{
			name:      "legacy function template",
			source:    "func\n",
			label:     "func",
			want:      "function ${1:Name}(${2}) ${3:abort}\n\t$0\nendfunction",
			character: 4,
		},
		{
			name:      "legacy try/catch template",
			source:    "tryc\n",
			label:     "tryc",
			want:      "try\n\t${1}\ncatch /.*/\n\t$0\nendtry",
			character: 4,
		},
		{name: "legacy try/finally template", source: "tryf\n", label: "tryf", want: "try\n\t${1}\nfinally\n\t$0\nendtry", character: 4},
		{name: "legacy try/catch/finally template", source: "trycf\n", label: "trycf", want: "try\n\t${1}\ncatch /.*/\n\t${2}\nfinally\n\t$0\nendtry", character: 5},
		{name: "legacy augroup template", source: "au\n", label: "aug", want: "augroup ${1:Start}\n\tautocmd!\n\t$0\naugroup END", character: 2},
		{name: "legacy autocmd template", source: "aut\n", label: "aut", want: "autocmd ${1:group-event} ${2:pat} ${3:once} ${4:nested} ${5:cmd}", character: 3},
		{name: "legacy command template (config role standalone)", source: "cmd\n", label: "cmd", want: "command ${1:attr} ${2:cmd} ${3:rep} $0", character: 3},
		{name: "legacy highlight template", source: "hi\n", label: "hi", want: "highlight ${1:default} ${2:group-name} ${3:args} $0", character: 2},
		{name: "if command", source: "if\n", label: "if", want: "if ${1:condition}\n\t$0\nendif", character: 2, command: true},
		{name: "for command", source: "for\n", label: "for", want: "for ${1:item} in ${2:list}\n\t$0\nendfor", character: 3, command: true},
		{name: "while command", source: "while\n", label: "while", want: "while ${1:condition}\n\t$0\nendwhile", character: 5, command: true},
		{name: "try command", source: "try\n", label: "try", want: "try\n\t$1\ncatch /.*/\n\t$0\nendtry", character: 3, command: true},
		{name: "legacy function command (config role standalone)", source: "function\n", label: "function", want: "function ${1:Name}()\n\t$0\nendfunction", character: 8, command: true},
		{name: "Vim9 def command", source: "vim9script\ndef\n", label: "def", want: "def ${1:Name}()\n\t$0\nenddef", line: 1, character: 3, command: true},
		{
			name:      "Vim9 class template",
			source:    "vim9script\nclass\n",
			label:     "class",
			want:      "class ${1:Name}\n\t$0\nendclass",
			line:      1,
			character: 5,
			command:   true,
		},
		{
			name:      "Vim9 interface template",
			source:    "vim9script\ninterface\n",
			label:     "interface",
			want:      "interface ${1:Name}\n\t$0\nendinterface",
			line:      1,
			character: 9,
			command:   true,
		},
		{
			name:      "Vim9 enum template",
			source:    "vim9script\nenum\n",
			label:     "enum",
			want:      "enum ${1:Name}\n${2:Value}\nendenum",
			line:      1,
			character: 4,
			command:   true,
		},
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
				items := completionItems(t, result)
				if !snippets && !test.command {
					if hasCompletionLabel(items, test.label) {
						t.Fatalf("non-command snippet %q present without snippet support", test.label)
					}
					continue
				}
				item := completionItemWithLabel(items, test.label)
				if item == nil {
					t.Fatalf("completion %q missing", test.label)
				}
				if !test.command {
					documentation, ok := item.Documentation.(*protocol.MarkupContent)
					if !ok || documentation.Kind != protocol.MarkupKindMarkdown || !strings.HasPrefix(documentation.Value, "```vim\n") || !strings.HasSuffix(documentation.Value, "\n```") {
						t.Fatalf("snippet %q documentation = %#v", test.label, item.Documentation)
					}
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

func TestCompletionSnippetDocumentationUsesPlainTextFallback(t *testing.T) {
	items := completionSnippetItems(syntax.Legacy, true, false, false)
	if len(items) == 0 {
		t.Fatal("snippet completion items are empty")
	}
	documentation, ok := items[0].Documentation.(protocol.String)
	if !ok || strings.Contains(string(documentation), "```") || !strings.Contains(string(documentation), "function") {
		t.Fatalf("plain snippet documentation = %#v", items[0].Documentation)
	}
	mapping := configMappingSkeleton(false)
	documentation, ok = mapping.Documentation.(protocol.String)
	if !ok || strings.Contains(string(documentation), "```") || !strings.Contains(string(documentation), "<Leader>") {
		t.Fatalf("plain mapping documentation = %#v", mapping.Documentation)
	}
}

func TestCompletionFuzzyMatchesOrderedSubsequence(t *testing.T) {
	for _, test := range []struct {
		prefix, label string
		want          bool
	}{
		{prefix: "&hada", label: "&shada", want: true},
		{prefix: "g:rp", label: "g:groupValue", want: true},
		{prefix: "aé", label: "aéclair", want: true},
		{prefix: "g:rp", label: "g:other", want: false},
		{prefix: "<f", label: "<cfile>", want: true},
		{prefix: "<cf", label: "<cfile>", want: true},
	} {
		if got := completionTextMatches(test.prefix, test.label); got != test.want {
			t.Fatalf("completionTextMatches(%q, %q) = %t, want %t", test.prefix, test.label, got, test.want)
		}
	}
	tests := []struct {
		name, source, label, prefix string
		line, character             uint32
	}{
		{
			name:      "fuzzy variable",
			source:    "vim9script\nvar groupValue = 1\nvar other = 2\necho grp\n",
			label:     "groupValue",
			prefix:    "grp",
			line:      3,
			character: 8,
		},
		{
			name:      "fuzzy builtin",
			source:    "vim9script\necho srn\n",
			label:     "strlen",
			prefix:    "srn",
			line:      1,
			character: 8,
		},
		{
			name:      "case insensitive fuzzy",
			source:    "vim9script\nvar groupValue = 1\necho GRP\n",
			label:     "groupValue",
			prefix:    "GRP",
			line:      2,
			character: 8,
		},
		{
			name:      "fuzzy from later character",
			source:    "vim9script\necho len\n",
			label:     "strlen",
			prefix:    "len",
			line:      1,
			character: 8,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
			result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: test.line, Character: test.character},
			}})
			if err != nil {
				t.Fatal(err)
			}
			items := completionItems(t, result)
			if !hasCompletionLabel(items, test.label) {
				t.Fatalf("completion %q for prefix %q missing from %#v", test.label, test.prefix, items)
			}
		})
	}

	// A non-matching subsequence must stay out.
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar groupValue = 1\necho zqx\n")
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 8},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range completionItems(t, result) {
		if item.Label == "groupValue" {
			t.Fatalf("non-matching fuzzy candidate returned: %#v", item)
		}
	}

	snapshot := text.NewSnapshot("file:///ranking.vim", 1, nil, "wl")
	candidates := map[string]completionCandidate{
		"wl":        {item: protocol.CompletionItem{Label: "wl"}, score: 1},
		"wl-theme":  {item: protocol.CompletionItem{Label: "wl-theme"}, score: 1},
		"wildcharm": {item: protocol.CompletionItem{Label: "wildcharm"}, score: 10000},
	}
	ordered := instance.completionList(snapshot, text.UTF16, completionSelection{start: 0, cursor: 2, end: 2, prefix: "wl"}, candidates).Items
	if len(ordered) != 3 {
		t.Fatalf("completion match order = %#v", ordered)
	}
	if got := []string{ordered[0].Label, ordered[1].Label, ordered[2].Label}; !slices.Equal(got, []string{"wl", "wl-theme", "wildcharm"}) {
		t.Fatalf("completion match order = %#v", got)
	}

	for _, test := range []struct {
		source, label string
		character     uint32
	}{
		{source: "vim9script\necho &g:sh\n", label: "&g:shell", character: 10},
		{source: "vim9script\necho &l:sh\n", label: "&l:shell", character: 10},
	} {
		instance, documentURI := openNavigationDocument(t, text.UTF16, test.source)
		result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: test.character},
		}})
		if err != nil {
			t.Fatal(err)
		}
		item := completionItemWithLabel(completionItems(t, result), test.label)
		if item == nil {
			t.Fatalf("scoped option completion %q missing from %#v", test.label, completionItems(t, result))
		}
		edit, ok := item.TextEdit.(*protocol.TextEdit)
		if !ok || edit.NewText != test.label {
			t.Fatalf("scoped option completion %q edit = %#v", test.label, item.TextEdit)
		}
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
	for _, test := range []struct {
		source, label string
	}{
		{"autocmd BufEnter <bu", "<buffer>"},
		{"autocmd BufEnter * +", "++once"},
		{"autocmd BufEnter * +", "++nested"},
		{"autocmd BufEnter * ++once +", "++nested"},
	} {
		items := commandPartCompletionItems(t, test.source, 0, uint32(len(test.source)))
		if completionItemWithLabel(items, test.label) == nil {
			t.Errorf("%q completion %q missing from %#v", test.source, test.label, items)
		}
	}

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
	items = commandPartCompletionItems(t, "command! -", 0, uint32(len("command! -")))
	for _, label := range []string{"-addr=", "-bang", "-bar", "-buffer", "-complete=", "-completeopt=", "-count", "-keepscript", "-nargs=", "-range", "-register"} {
		item := completionItemWithLabel(items, label)
		if item == nil {
			t.Errorf("command! - attribute %q missing from %#v", label, items)
			continue
		}
		if detail, ok := item.Detail.Get(); !ok || detail == "" {
			t.Errorf("command! - attribute %q missing detail: %#v", label, item)
		}
		if doc, ok := item.Documentation.(protocol.String); !ok || strings.TrimSpace(string(doc)) == "" {
			t.Errorf("command! - attribute %q missing doc: %#v", label, item)
		}
	}
	items = commandPartCompletionItems(t, "command! -nargs=+ -", 0, uint32(len("command! -nargs=+ -")))
	if completionItemWithLabel(items, "-nargs=") != nil || completionItemWithLabel(items, "-complete=") == nil {
		t.Fatalf("command! -nargs=+ - attributes = %#v", items)
	}
	complexSource := "command! -nargs=+ -complete=custom,s:GrepArgs -          Rg        :exe 'CocList grep '.<q-args>"
	cursorAt := uint32(strings.Index(complexSource, " - ") + 2)
	items = commandPartCompletionItems(t, complexSource, 0, cursorAt)
	if completionItemWithLabel(items, "-nargs=") != nil || completionItemWithLabel(items, "-complete=") != nil || completionItemWithLabel(items, "-buffer") == nil {
		t.Fatalf("inserted attribute completions = %#v", items)
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
			item := completionItemWithLabel(items, label)
			if item == nil {
				t.Errorf("%q completion %q missing from %#v", test.source, label, items)
				continue
			}
			if strings.HasPrefix(test.source, "command -") {
				if detail, ok := item.Detail.Get(); !ok || detail == "" {
					t.Errorf("%s item %q missing detail: %#v", test.source, label, item)
				}
				if doc, ok := item.Documentation.(protocol.String); !ok || strings.TrimSpace(string(doc)) == "" {
					t.Errorf("%s item %q missing doc: %#v", test.source, label, item)
				}
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

func TestCompletionAutocmdMultiPattern(t *testing.T) {
	for _, test := range []struct {
		source        string
		wantStartChar uint32
		wantEndChar   uint32
	}{
		{"autocmd User A,<bu", 15, 18},
		{"autocmd BufEnter *.vim,<bu", 23, 26},
		{`autocmd BufEnter foo\,bar,<bu`, 26, 29},
	} {
		items := commandPartCompletionItems(t, test.source, 0, uint32(len(test.source)))
		item := completionItemWithLabel(items, "<buffer>")
		if item == nil {
			t.Fatalf("%q: missing completion <buffer> in %#v", test.source, items)
		}
		edit, ok := item.TextEdit.(*protocol.TextEdit)
		if !ok {
			t.Fatalf("%q: missing textEdit on <buffer>: %#v", test.source, item.TextEdit)
		}
		expectedRange := navigationRange(0, test.wantStartChar, test.wantEndChar)
		if edit.Range != expectedRange {
			t.Fatalf("%q: range got %v, want %v", test.source, edit.Range, expectedRange)
		}
		if edit.NewText != "<buffer>" {
			t.Fatalf("%q: newText got %q, want <buffer>", test.source, edit.NewText)
		}
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
	resolved, err = instance.CompletionResolve(context.Background(), &protocol.CompletionItem{Label: "abs", Kind: protocol.CompletionItemKindFunction})
	if err != nil || resolved.Documentation != nil {
		t.Fatalf("label-only resolve = %#v, %v", resolved, err)
	}
	if _, ok := resolved.Detail.Get(); ok {
		t.Fatalf("label-only resolve added detail: %#v", resolved)
	}
	for _, test := range []struct {
		label string
		kind  completionResolveKind
		name  string
	}{
		{label: "abs", kind: completionResolveBuiltinFunction, name: "abs"},
		{label: "&ignorecase", kind: completionResolveOption, name: "ignorecase"},
		{label: "v:count", kind: completionResolveVariable, name: "v:count"},
		{label: "echo", kind: completionResolveCommand, name: "echo"},
	} {
		resolved, err = instance.CompletionResolve(context.Background(), &protocol.CompletionItem{Label: test.label, Data: completionResolveTargetData(test.kind, test.name)})
		if err != nil || resolved == nil {
			t.Fatalf("resolve %s = %#v, %v", test.label, resolved, err)
		}
		detail, ok := resolved.Detail.Get()
		if !ok || detail == "" {
			t.Fatalf("resolve %s detail = %q, %t", test.label, detail, ok)
		}
		if resolved.Documentation == nil {
			t.Fatalf("resolve %s documentation is missing", test.label)
		}
	}
	resolved, _ = instance.CompletionResolve(context.Background(), &protocol.CompletionItem{Label: "&ignorecase", Data: completionResolveTargetData(completionResolveOption, "ignorecase")})
	detail, _ := resolved.Detail.Get()
	if !strings.Contains(detail, "bool") || !strings.Contains(detail, "global") {
		t.Fatalf("option detail = %q", detail)
	}
}

// completionListRequest runs a completion request at a UTF-16 position and
// returns the list.
func completionListRequest(t *testing.T, instance *Server, documentURI uri.URI, line, character uint32) *protocol.CompletionList {
	t.Helper()
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: line, Character: character},
	}})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := result.(*protocol.CompletionList)
	if !ok {
		t.Fatalf("completion result = %T, want *protocol.CompletionList", result)
	}
	return list
}

// completionMainEdit is the explicit main edit a completion item implies:
// the accepted range (replace, plus insert when both are reported), the text
// inserted over that range, and the item's per-item insert text format.
type completionMainEdit struct {
	text    string
	replace protocol.Range
	insert  *protocol.Range // nil when the main edit reports only one range
	format  protocol.InsertTextFormat
}

// completionMainEditFromItem extracts the explicit main edit a completion
// item carries in the pre-itemDefaults response shape.
func completionMainEditFromItem(item protocol.CompletionItem) completionMainEdit {
	edit := completionMainEdit{format: item.InsertTextFormat}
	switch main := item.TextEdit.(type) {
	case *protocol.TextEdit:
		edit.text = main.NewText
		edit.replace = main.Range
	case *protocol.InsertReplaceEdit:
		insert := main.Insert
		edit.text = main.NewText
		edit.replace = main.Replace
		edit.insert = &insert
	}
	return edit
}

// expandCompletionListItemDefaults expands each item of a list produced with
// CompletionList.ItemDefaults.editRange into the explicit main edit it
// implies: the range comes from the shared default, the text from the item's
// textEditText, and per-item fields (format and friends) stay untouched.
func expandCompletionListItemDefaults(t *testing.T, list *protocol.CompletionList) []completionMainEdit {
	t.Helper()
	if list.ItemDefaults == nil {
		t.Fatalf("completion list carries no item defaults: %#v", list)
	}
	var insert *protocol.Range
	var replace protocol.Range
	switch editRange := list.ItemDefaults.EditRange.(type) {
	case *protocol.Range:
		replace = *editRange
	case *protocol.EditRangeWithInsertReplace:
		insertRange := editRange.Insert
		insert = &insertRange
		replace = editRange.Replace
	default:
		t.Fatalf("itemDefaults.editRange = %#v", list.ItemDefaults.EditRange)
	}
	expanded := make([]completionMainEdit, 0, len(list.Items))
	for _, item := range list.Items {
		text, ok := item.TextEditText.Get()
		if !ok {
			t.Fatalf("item %q in item-defaults list has no textEditText", item.Label)
		}
		if item.TextEdit != nil {
			t.Fatalf("item %q in item-defaults list still carries textEdit %#v", item.Label, item.TextEdit)
		}
		expanded = append(expanded, completionMainEdit{text: text, replace: replace, insert: insert, format: item.InsertTextFormat})
	}
	return expanded
}

func completionMainEditsEqual(left, right completionMainEdit) bool {
	if left.text != right.text || left.format != right.format || left.replace != right.replace {
		return false
	}
	if left.insert == nil || right.insert == nil {
		return left.insert == nil && right.insert == nil
	}
	return *left.insert == *right.insert
}

// applyCompletionMainEdit applies one explicit main edit over its replace
// range and returns the resulting document text.
func applyCompletionMainEdit(t *testing.T, source string, edit completionMainEdit) string {
	t.Helper()
	snapshot := text.NewSnapshot("file:///completion-apply.vim", 1, nil, source)
	start, startErr := snapshot.Offset(fromProtocolPosition(edit.replace.Start), text.UTF16)
	end, endErr := snapshot.Offset(fromProtocolPosition(edit.replace.End), text.UTF16)
	if startErr != nil || endErr != nil || end < start {
		t.Fatalf("cannot apply main edit %#v: %v, %v", edit, startErr, endErr)
	}
	return source[:start] + edit.text + source[end:]
}

// assertCompletionItemDefaultsEquivalent checks that a list produced with
// itemDefaults.editRange expands, item for item, into the same explicit main
// edit (range, text, and per-item insert text format) that the same source
// and selection yield with the defaults off, and that applying each explicit
// edit produces the same document text in both shapes.
func assertCompletionItemDefaultsEquivalent(t *testing.T, source string, oldList, newList *protocol.CompletionList) {
	t.Helper()
	if len(oldList.Items) != len(newList.Items) {
		t.Fatalf("item counts differ with and without itemDefaults: old %d, new %d", len(oldList.Items), len(newList.Items))
	}
	expanded := expandCompletionListItemDefaults(t, newList)
	for index := range oldList.Items {
		oldItem, newItem := oldList.Items[index], newList.Items[index]
		if oldItem.Label != newItem.Label || oldItem.SortText != newItem.SortText {
			t.Fatalf("item %d differs between shapes: old %#v, new %#v", index, oldItem, newItem)
		}
		oldEdit := completionMainEditFromItem(oldItem)
		if !completionMainEditsEqual(oldEdit, expanded[index]) {
			t.Fatalf("item %d %q explicit main edit = %#v, want old-shape edit %#v", index, oldItem.Label, expanded[index], oldEdit)
		}
		if newItem.TextEdit != nil {
			t.Fatalf("item %d %q retains textEdit %#v in item-defaults list", index, newItem.Label, newItem.TextEdit)
		}
		appliedNew := applyCompletionMainEdit(t, source, expanded[index])
		appliedOld := applyCompletionMainEdit(t, source, oldEdit)
		if appliedNew != appliedOld {
			t.Fatalf("item %d %q: applying shared-default edit yields %q, old shape yields %q", index, oldItem.Label, appliedNew, appliedOld)
		}
	}
}

func TestCompletionItemDefaultsEditRangePlainShape(t *testing.T) {
	const source = "vim9script\necho absc\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	oldList := completionListRequest(t, instance, documentURI, 1, 8)
	if len(oldList.Items) == 0 {
		t.Fatalf("no completion items for %q", source)
	}
	for _, item := range oldList.Items {
		if _, ok := item.TextEdit.(*protocol.TextEdit); !ok {
			t.Fatalf("old-shape item %q edit = %#v, want plain TextEdit", item.Label, item.TextEdit)
		}
	}
	first, ok := oldList.Items[0].TextEdit.(*protocol.TextEdit)
	if !ok {
		t.Fatalf("first item edit = %#v, want plain TextEdit", oldList.Items[0].TextEdit)
	}

	instance.completion.itemDefaultsEditRange = true
	newList := completionListRequest(t, instance, documentURI, 1, 8)
	if newList.ItemDefaults == nil {
		t.Fatal("itemDefaults.editRange declared but list carries no itemDefaults")
	}
	editRange, ok := newList.ItemDefaults.EditRange.(*protocol.Range)
	if !ok {
		t.Fatalf("itemDefaults.editRange = %T, want *protocol.Range", newList.ItemDefaults.EditRange)
	}
	if *editRange != first.Range {
		t.Fatalf("itemDefaults.editRange = %#v, want the selection replace range %#v", *editRange, first.Range)
	}
	for _, item := range newList.Items {
		if item.TextEdit != nil {
			t.Fatalf("item %q still carries textEdit %#v with itemDefaults", item.Label, item.TextEdit)
		}
		if _, ok := item.TextEditText.Get(); !ok {
			t.Fatalf("item %q has no textEditText", item.Label)
		}
	}
	assertCompletionItemDefaultsEquivalent(t, source, oldList, newList)
}

func TestCompletionItemDefaultsEditRangeInsertReplaceShape(t *testing.T) {
	const source = "vim9script\necho absc\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	instance.completion.insertReplace = true
	oldList := completionListRequest(t, instance, documentURI, 1, 8)
	if len(oldList.Items) == 0 {
		t.Fatalf("no completion items for %q", source)
	}
	first, ok := oldList.Items[0].TextEdit.(*protocol.InsertReplaceEdit)
	if !ok {
		t.Fatalf("first item edit = %#v, want InsertReplaceEdit", oldList.Items[0].TextEdit)
	}
	for _, item := range oldList.Items {
		edit, ok := item.TextEdit.(*protocol.InsertReplaceEdit)
		if !ok {
			t.Fatalf("old-shape item %q edit = %#v, want InsertReplaceEdit", item.Label, item.TextEdit)
		}
		if edit.Insert != first.Insert || edit.Replace != first.Replace {
			t.Fatalf("old-shape item %q edit range %#v diverges from first item %#v", item.Label, edit, first)
		}
	}

	instance.completion.itemDefaultsEditRange = true
	newList := completionListRequest(t, instance, documentURI, 1, 8)
	if newList.ItemDefaults == nil {
		t.Fatal("itemDefaults.editRange declared but list carries no itemDefaults")
	}
	editRange, ok := newList.ItemDefaults.EditRange.(*protocol.EditRangeWithInsertReplace)
	if !ok {
		t.Fatalf("itemDefaults.editRange = %T, want *protocol.EditRangeWithInsertReplace", newList.ItemDefaults.EditRange)
	}
	if editRange.Insert != first.Insert || editRange.Replace != first.Replace {
		t.Fatalf("itemDefaults.editRange = %#v, want old-shape insert %#v replace %#v", editRange, first.Insert, first.Replace)
	}
	for _, item := range newList.Items {
		if item.TextEdit != nil {
			t.Fatalf("item %q still carries textEdit %#v with itemDefaults", item.Label, item.TextEdit)
		}
		if _, ok := item.TextEditText.Get(); !ok {
			t.Fatalf("item %q has no textEditText", item.Label)
		}
	}
	assertCompletionItemDefaultsEquivalent(t, source, oldList, newList)
}

func TestCompletionItemDefaultsEditRangeMarshalShape(t *testing.T) {
	plainInstance, plainURI := openNavigationDocument(t, text.UTF16, "vim9script\necho absc\n")
	plainInstance.completion.itemDefaultsEditRange = true
	plain := completionListRequest(t, plainInstance, plainURI, 1, 8)
	encoded, err := protocol.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(encoded)
	if !strings.Contains(raw, `"itemDefaults":{"editRange":{"start":`) {
		t.Fatalf("plain marshal lacks itemDefaults.editRange range: %s", raw)
	}
	if strings.Contains(raw, `"itemDefaults":{"editRange":{"insert":`) {
		t.Fatalf("plain marshal carries an insert/replace default: %s", raw)
	}
	if strings.Contains(raw, `"textEdit":`) {
		t.Fatalf("item-defaults marshal still has a textEdit member: %s", raw)
	}
	if got := strings.Count(raw, `"textEditText":`); got != len(plain.Items) {
		t.Fatalf("textEditText members = %d, want %d: %s", got, len(plain.Items), raw)
	}

	irInstance, irURI := openNavigationDocument(t, text.UTF16, "vim9script\necho absc\n")
	irInstance.completion.insertReplace = true
	irInstance.completion.itemDefaultsEditRange = true
	irList := completionListRequest(t, irInstance, irURI, 1, 8)
	encoded, err = protocol.Marshal(irList)
	if err != nil {
		t.Fatal(err)
	}
	raw = string(encoded)
	if !strings.Contains(raw, `"itemDefaults":{"editRange":{"insert":{"start":`) || !strings.Contains(raw, `,"replace":{"start":`) {
		t.Fatalf("insert/replace marshal lacks the shared editRange: %s", raw)
	}
	if strings.Contains(raw, `"textEdit":`) {
		t.Fatalf("item-defaults marshal still has a textEdit member: %s", raw)
	}
	if got := strings.Count(raw, `"textEditText":`); got != len(irList.Items) {
		t.Fatalf("textEditText members = %d, want %d: %s", got, len(irList.Items), raw)
	}

	oldInstance, oldURI := openNavigationDocument(t, text.UTF16, "vim9script\necho absc\n")
	oldList := completionListRequest(t, oldInstance, oldURI, 1, 8)
	encoded, err = protocol.Marshal(oldList)
	if err != nil {
		t.Fatal(err)
	}
	raw = string(encoded)
	if strings.Contains(raw, `"itemDefaults"`) || strings.Contains(raw, `"textEditText"`) {
		t.Fatalf("old-shape marshal leaks item defaults: %s", raw)
	}
	if got := strings.Count(raw, `"textEdit":`); got != len(oldList.Items) {
		t.Fatalf("old-shape textEdit members = %d, want %d: %s", got, len(oldList.Items), raw)
	}
}

func TestCompletionItemDefaultsEditRangeSnippetAndPlainItems(t *testing.T) {
	const source = "vim9script\nvar strlenValue = 1\necho strl\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	instance.completion.snippet = true
	oldList := completionListRequest(t, instance, documentURI, 2, 9)
	oldSnippet := completionItemWithLabel(protocol.CompletionItemSlice(oldList.Items), "strlen")
	oldPlain := completionItemWithLabel(protocol.CompletionItemSlice(oldList.Items), "strlenValue")
	if oldSnippet == nil || oldPlain == nil {
		t.Fatalf("snippet/plain pair missing from %#v", oldList.Items)
	}
	snippetEdit, ok := oldSnippet.TextEdit.(*protocol.TextEdit)
	if !ok {
		t.Fatalf("snippet item edit = %#v, want plain TextEdit", oldSnippet.TextEdit)
	}
	plainEdit, ok := oldPlain.TextEdit.(*protocol.TextEdit)
	if !ok {
		t.Fatalf("plain item edit = %#v, want plain TextEdit", oldPlain.TextEdit)
	}

	instance.completion.itemDefaultsEditRange = true
	newList := completionListRequest(t, instance, documentURI, 2, 9)
	if newList.ItemDefaults == nil {
		t.Fatal("itemDefaults.editRange declared but list carries no itemDefaults")
	}
	newItems := protocol.CompletionItemSlice(newList.Items)
	snippetItem := completionItemWithLabel(newItems, "strlen")
	if snippetItem == nil {
		t.Fatalf("snippet item missing from %#v", newList.Items)
	}
	if snippetItem.InsertTextFormat != protocol.InsertTextFormatSnippet {
		t.Fatalf("snippet item format = %v, want Snippet", snippetItem.InsertTextFormat)
	}
	snippetText, ok := snippetItem.TextEditText.Get()
	if !ok || snippetText != snippetEdit.NewText {
		t.Fatalf("snippet textEditText = %q, %t, want %q", snippetText, ok, snippetEdit.NewText)
	}
	plainItem := completionItemWithLabel(newItems, "strlenValue")
	if plainItem == nil {
		t.Fatalf("plain item missing from %#v", newList.Items)
	}
	if plainItem.InsertTextFormat == protocol.InsertTextFormatSnippet {
		t.Fatal("plain item carries a snippet format")
	}
	plainText, ok := plainItem.TextEditText.Get()
	if !ok || plainText != plainEdit.NewText {
		t.Fatalf("plain textEditText = %q, %t, want %q", plainText, ok, plainEdit.NewText)
	}
	assertCompletionItemDefaultsEquivalent(t, source, oldList, newList)
}

func TestCompletionItemDefaultsEditRangeRequiresValidSharedRange(t *testing.T) {
	const source = "vim9script\necho ab"
	snapshot := text.NewSnapshot("file:///item-defaults-fallback.vim", 1, nil, source)
	candidates := map[string]completionCandidate{
		"ab":   {item: protocol.CompletionItem{Label: "ab", Kind: protocol.CompletionItemKindFunction}, score: 8000, source: completionSourceBuiltin},
		"abcd": {item: protocol.CompletionItem{Label: "abcd", Kind: protocol.CompletionItemKindFunction}, score: 7000, source: completionSourceBuiltin},
	}
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.completion.itemDefaultsEditRange = true

	// A replace end beyond the document makes the shared range invalid: the
	// response must fall back to today's per-item shape (no edit at all).
	invalid := completionSelection{start: len("vim9script\necho "), cursor: len(source), end: len(source) + 5, prefix: "ab"}
	list := instance.completionList(snapshot, text.UTF16, invalid, candidates)
	if list.ItemDefaults != nil {
		t.Fatalf("invalid-range list carries itemDefaults %#v", list.ItemDefaults)
	}
	if len(list.Items) != 2 {
		t.Fatalf("invalid-range items = %d, want 2", len(list.Items))
	}
	for _, item := range list.Items {
		if item.TextEdit != nil {
			t.Fatalf("invalid-range item %q carries textEdit %#v", item.Label, item.TextEdit)
		}
		if _, ok := item.TextEditText.Get(); ok {
			t.Fatalf("invalid-range item %q carries textEditText", item.Label)
		}
	}

	// An empty list cannot share one range, even though the selection is valid.
	empty := instance.completionList(snapshot, text.UTF16, completionSelection{start: len("vim9script\necho "), cursor: len(source), end: len(source), prefix: "zz"}, candidates)
	if empty.ItemDefaults != nil {
		t.Fatalf("empty list carries itemDefaults %#v", empty.ItemDefaults)
	}
	if len(empty.Items) != 0 {
		t.Fatalf("non-matching completion returned %#v", empty.Items)
	}
}

// TestCompletionItemDefaultsEditRangeSnippetWithInsertReplace exercises the
// realistic client combination of snippet support, insert/replace support and
// itemDefaults editRange: the snippet item's text moves into textEditText under
// a shared {insert,replace} edit range while its per-item Snippet format and
// the plain item in the same list stay intact.
func TestCompletionItemDefaultsEditRangeSnippetWithInsertReplace(t *testing.T) {
	const source = "vim9script\nvar strlenValue = 1\necho strl\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	instance.completion.snippet = true
	instance.completion.insertReplace = true
	oldList := completionListRequest(t, instance, documentURI, 2, 9)
	oldSnippet := completionItemWithLabel(protocol.CompletionItemSlice(oldList.Items), "strlen")
	oldPlain := completionItemWithLabel(protocol.CompletionItemSlice(oldList.Items), "strlenValue")
	if oldSnippet == nil || oldPlain == nil {
		t.Fatalf("snippet/plain pair missing from %#v", oldList.Items)
	}
	snippetEdit, ok := oldSnippet.TextEdit.(*protocol.InsertReplaceEdit)
	if !ok {
		t.Fatalf("snippet item edit = %#v, want InsertReplaceEdit", oldSnippet.TextEdit)
	}

	instance.completion.itemDefaultsEditRange = true
	newList := completionListRequest(t, instance, documentURI, 2, 9)
	if newList.ItemDefaults == nil {
		t.Fatal("itemDefaults.editRange declared but list carries no itemDefaults")
	}
	editRange, ok := newList.ItemDefaults.EditRange.(*protocol.EditRangeWithInsertReplace)
	if !ok {
		t.Fatalf("itemDefaults.editRange = %T, want *protocol.EditRangeWithInsertReplace", newList.ItemDefaults.EditRange)
	}
	if editRange.Insert != snippetEdit.Insert || editRange.Replace != snippetEdit.Replace {
		t.Fatalf("itemDefaults.editRange = %#v, want old-shape insert %#v replace %#v", editRange, snippetEdit.Insert, snippetEdit.Replace)
	}
	newItems := protocol.CompletionItemSlice(newList.Items)
	snippetItem := completionItemWithLabel(newItems, "strlen")
	if snippetItem == nil {
		t.Fatalf("snippet item missing from %#v", newList.Items)
	}
	if snippetItem.InsertTextFormat != protocol.InsertTextFormatSnippet {
		t.Fatalf("snippet item format = %v, want Snippet", snippetItem.InsertTextFormat)
	}
	snippetText, ok := snippetItem.TextEditText.Get()
	if !ok || snippetText != snippetEdit.NewText {
		t.Fatalf("snippet textEditText = %q, %t, want %q", snippetText, ok, snippetEdit.NewText)
	}
	plainItem := completionItemWithLabel(newItems, "strlenValue")
	if plainItem == nil {
		t.Fatalf("plain item missing from %#v", newList.Items)
	}
	if plainItem.InsertTextFormat == protocol.InsertTextFormatSnippet {
		t.Fatal("plain item carries a snippet format")
	}
	plainText, ok := plainItem.TextEditText.Get()
	if !ok || plainText != completionMainEditFromItem(*oldPlain).text {
		t.Fatalf("plain textEditText = %q, %t, want %q", plainText, ok, completionMainEditFromItem(*oldPlain).text)
	}
	assertCompletionItemDefaultsEquivalent(t, source, oldList, newList)
}

// TestCompletionItemDefaultsEditRangeFromInitializeCapabilities drives the
// full wiring from client capability JSON parsed in Initialize down to the
// completion list shape.
func TestCompletionItemDefaultsEditRangeFromInitializeCapabilities(t *testing.T) {
	enabled := true
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	_, err := instance.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: protocol.ClientCapabilities{
		TextDocument: &protocol.TextDocumentClientCapabilities{Completion: &protocol.CompletionClientCapabilities{
			CompletionItem: &protocol.ClientCompletionItemOptions{SnippetSupport: &enabled, InsertReplaceSupport: &enabled},
			CompletionList: &protocol.CompletionListCapabilities{ItemDefaults: []string{"editRange"}},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	instance.mu.Lock()
	if !instance.completion.itemDefaultsEditRange || !instance.completion.snippet || !instance.completion.insertReplace {
		t.Fatalf("completion capabilities after initialize = %#v", instance.completion)
	}
	instance.mu.Unlock()

	const source = "vim9script\nvar strlenValue = 1\necho strl\n"
	path := writeWorkspaceFile(t, t.TempDir(), "main.vim", source)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)
	list := completionListRequest(t, instance, documentURI, 2, 9)
	if list.ItemDefaults == nil {
		t.Fatal("list carries no itemDefaults after initialize capability parse")
	}
	if _, ok := list.ItemDefaults.EditRange.(*protocol.EditRangeWithInsertReplace); !ok {
		t.Fatalf("itemDefaults.editRange = %T, want *protocol.EditRangeWithInsertReplace", list.ItemDefaults.EditRange)
	}
	snippetItem := completionItemWithLabel(protocol.CompletionItemSlice(list.Items), "strlen")
	if snippetItem == nil {
		t.Fatalf("snippet item missing from %#v", list.Items)
	}
	if snippetItem.InsertTextFormat != protocol.InsertTextFormatSnippet || snippetItem.TextEdit != nil {
		t.Fatalf("snippet item = %#v, want Snippet format without textEdit", snippetItem)
	}
	if _, ok := snippetItem.TextEditText.Get(); !ok {
		t.Fatal("snippet item has no textEditText")
	}
}
