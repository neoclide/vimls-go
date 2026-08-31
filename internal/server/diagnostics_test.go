package server

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestProtocolDiagnosticSeverity(t *testing.T) {
	for _, code := range []string{"vimls/missing-end", "vim/E113", "vim/E518", "vim/E1012", "future/source"} {
		if got := protocolDiagnosticSeverity(code, syntax.DiagnosticWarning); got != protocol.DiagnosticSeverityError {
			t.Errorf("%s severity = %v, want error", code, got)
		}
	}
	for _, code := range []string{"vim/E117", "vim/E121", "vim/E1001", "vim/E1089"} {
		if got := protocolDiagnosticSeverity(code, syntax.DiagnosticWarning); got != protocol.DiagnosticSeverityWarning {
			t.Errorf("%s severity = %v, want warning", code, got)
		}
		if got := protocolDiagnosticSeverity(code, syntax.DiagnosticHint); got != protocol.DiagnosticSeverityHint {
			t.Errorf("%s configured severity = %v, want hint", code, got)
		}
	}
	if got := protocolDiagnosticSeverity("vim/E122", syntax.DiagnosticError); got != protocol.DiagnosticSeverityWarning {
		t.Errorf("vim/E122 severity = %v, want warning", got)
	}
	if got := protocolDiagnosticSeverity("vim/E174", syntax.DiagnosticError); got != protocol.DiagnosticSeverityWarning {
		t.Errorf("vim/E174 severity = %v, want warning", got)
	}
	if got := protocolDiagnosticSeverity("vim/E464", syntax.DiagnosticError); got != protocol.DiagnosticSeverityWarning {
		t.Errorf("vim/E464 severity = %v, want warning", got)
	}
	for _, code := range []string{"vim/E705", "vim/E707"} {
		if got := protocolDiagnosticSeverity(code, syntax.DiagnosticError); got != protocol.DiagnosticSeverityWarning {
			t.Errorf("%s severity = %v, want warning", code, got)
		}
	}
	if got := protocolDiagnosticSeverity("vimls/deprecated", syntax.DiagnosticError); got != protocol.DiagnosticSeverityHint {
		t.Errorf("vimls/deprecated severity = %v, want hint", got)
	}
	if got := protocolDiagnosticSeverity("vimls/unused-variable", syntax.DiagnosticError); got != protocol.DiagnosticSeverityHint {
		t.Errorf("vimls/unused-variable severity = %v, want hint", got)
	}
}

func TestServerPublishesDeprecatedReferenceHint(t *testing.T) {
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 1)}
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.client = client
	documentURI := uri.MustParse("file:///deprecated.vim")
	source := "vim9script\n# deprecated\nvar Old = 1\necho Old\n"
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: source,
	}}); err != nil {
		t.Fatal(err)
	}
	params := waitForDiagnostics(t, client.published)
	if len(params.Diagnostics) != 1 {
		t.Fatalf("deprecated diagnostics = %#v", params.Diagnostics)
	}
	diagnostic := params.Diagnostics[0]
	tags := diagnostic.Tags.Slice()
	if diagnostic.Code != protocol.String("vimls/deprecated") || diagnostic.Severity != protocol.DiagnosticSeverityHint ||
		diagnostic.Message != protocol.String("Old is deprecated") || len(tags) != 1 || tags[0] != protocol.DiagnosticTagDeprecated ||
		diagnostic.Range.Start.Line != 3 {
		t.Fatalf("deprecated diagnostic = %#v tags=%#v", diagnostic, tags)
	}
}

func TestServerPublishesUnusedVariableHint(t *testing.T) {
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 1)}
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.client = client
	documentURI := uri.MustParse("file:///unused.vim")
	source := "vim9script\nvar Unused = 1\n"
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: source,
	}}); err != nil {
		t.Fatal(err)
	}
	params := waitForDiagnostics(t, client.published)
	if len(params.Diagnostics) != 1 {
		t.Fatalf("unused variable diagnostics = %#v", params.Diagnostics)
	}
	diagnostic := params.Diagnostics[0]
	tags := diagnostic.Tags.Slice()
	if diagnostic.Code != protocol.String("vimls/unused-variable") || diagnostic.Severity != protocol.DiagnosticSeverityHint ||
		diagnostic.Message != protocol.String("Unused is declared but never used") || len(tags) != 1 || tags[0] != protocol.DiagnosticTagUnnecessary ||
		diagnostic.Range.Start.Line != 1 {
		t.Fatalf("unused variable diagnostic = %#v tags=%#v", diagnostic, tags)
	}
}

func TestE464DiagnosticsUseCompleteRuntimepathCommandIndex(t *testing.T) {
	runtimeRoot := t.TempDir()
	writeWorkspaceFile(t, runtimeRoot, "plugin/build.vim", "command! BuildProject echo 'build'\n")
	writeWorkspaceFile(t, runtimeRoot, "after/plugin/local.vim", "command! LocalCommand echo 'local'\n")
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	index, graph, files, warnings := instance.buildWorkspaceIndex(context.Background(), []string{runtimeRoot}, workspacePathResolver(nil, []string{runtimeRoot}), nil)
	if len(warnings) != 0 || !index.Complete() || index.FileCount() != 2 {
		t.Fatalf("runtimepath index: files=%d complete=%v warnings=%#v", index.FileCount(), index.Complete(), warnings)
	}
	instance.workspaceMu.Lock()
	instance.workspaceIndex = index
	instance.workspaceGraph = graph
	instance.workspaceGraphView = graph.Snapshot()
	instance.workspaceFiles = files
	instance.workspaceBuilt = true
	instance.workspaceMu.Unlock()

	file := syntax.Parse("BuildP\nLocalC\nBuildProject\n")
	diagnostics := instance.userCommandAbbreviationDiagnostics(file)
	if len(diagnostics) != 2 || file.Text(diagnostics[0].Span) != "BuildP" || file.Text(diagnostics[1].Span) != "LocalC" {
		t.Fatalf("E464 diagnostics = %#v", diagnostics)
	}
	index.SetComplete(false)
	if diagnostics := instance.userCommandAbbreviationDiagnostics(file); len(diagnostics) != 0 {
		t.Fatalf("incomplete runtimepath index produced E464: %#v", diagnostics)
	}
}

func TestE705E707DiagnosticsUseInitialGlobalNameIndex(t *testing.T) {
	runtimeRoot := t.TempDir()
	writeWorkspaceFile(t, runtimeRoot, "plugin/function.vim", "function Shared()\nendfunction\n")
	writeWorkspaceFile(t, runtimeRoot, "plugin/variable.vim", "let g:Other = 1\n")
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	index, graph, files, warnings := instance.buildWorkspaceIndex(context.Background(), []string{runtimeRoot}, workspacePathResolver(nil, []string{runtimeRoot}), nil)
	if len(warnings) != 0 || index.FileCount() != 2 {
		t.Fatalf("runtimepath index: files=%d warnings=%#v", index.FileCount(), warnings)
	}
	instance.workspaceMu.Lock()
	instance.workspaceIndex = index
	instance.workspaceGraph = graph
	instance.workspaceGraphView = graph.Snapshot()
	instance.workspaceFiles = files
	instance.workspaceBuilt = true
	instance.workspaceMu.Unlock()

	variablePath := filepath.Join(runtimeRoot, "plugin/current-variable.vim")
	variableFile := syntax.Parse("let g:Shared = 1\n")
	if got := instance.globalNameConflictDiagnostics(uri.File(variablePath).String(), variableFile); len(got) != 1 || got[0].Code != "vim/E705" {
		t.Fatalf("E705 diagnostics = %#v", got)
	}
	functionPath := filepath.Join(runtimeRoot, "plugin/current-function.vim")
	functionFile := syntax.Parse("function Other()\nendfunction\n")
	if got := instance.globalNameConflictDiagnostics(uri.File(functionPath).String(), functionFile); len(got) != 1 || got[0].Code != "vim/E707" {
		t.Fatalf("E707 diagnostics = %#v", got)
	}

	instance.workspaceMu.Lock()
	instance.workspaceBuilt = false
	instance.workspaceMu.Unlock()
	if got := instance.globalNameConflictDiagnostics(uri.File(variablePath).String(), variableFile); len(got) != 0 {
		t.Fatalf("unready index produced global conflict warning: %#v", got)
	}
}

func TestAutoloadExportedFunctionVariableConflictUsesE707(t *testing.T) {
	root := t.TempDir()
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.setWorkspaceRoots([]string{root})
	source := "vim9script\n\nvar Clash = 'value'\n\nexport def Clash()\nenddef\n"

	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{"autoload script", filepath.Join(root, "autoload", "clash.vim"), "vim/E707"},
		{"ordinary script", filepath.Join(root, "plugin", "clash.vim"), "vim/E1041"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(source)
			result := analysis.Analyze(file)
			diagnostics := instance.autoloadExportedFunctionDiagnostics(uri.File(test.path).String(), file, result, result.Diagnostics)
			if len(diagnostics) != 1 || diagnostics[0].Code != test.want || file.Text(diagnostics[0].Span) != "Clash" {
				t.Fatalf("diagnostics = %#v, want one %s for Clash", diagnostics, test.want)
			}
		})
	}
}

func TestAutoloadE707RequiresVariableBeforeExportedDef(t *testing.T) {
	root := t.TempDir()
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.setRuntimePaths([]string{root})
	documentURI := uri.File(filepath.Join(root, "autoload", "clash.vim")).String()

	for _, test := range []struct {
		name   string
		source string
	}{
		{"non-exported def", "vim9script\nvar Clash = 'value'\ndef Clash()\nenddef\n"},
		{"function before variable", "vim9script\nexport def Clash()\nenddef\nvar Clash = 'value'\n"},
		{"exported variable before def", "vim9script\nexport var Clash = 'value'\ndef Clash()\nenddef\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := analysis.Analyze(file)
			diagnostics := instance.autoloadExportedFunctionDiagnostics(documentURI, file, result, result.Diagnostics)
			if len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1041" {
				t.Fatalf("diagnostics = %#v, want one vim/E1041", diagnostics)
			}
		})
	}
}

func TestServerPublishesConfigurableUnresolvedSeverity(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
	instance.mu.Lock()
	instance.client = client
	instance.mu.Unlock()
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		InitializationOptions: protocol.LSPAny([]byte(`{"runtimepath":[]}`)),
	}); err != nil {
		t.Fatal(err)
	}
	documentURI := uri.MustParse("file:///unresolved.vim")
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI: documentURI, Version: 1,
			Text: "vim9script\ndoesnotexist()\necho missingScript\ndef Test()\n  echo missingValue\nenddef\n",
		},
	}); err != nil {
		t.Fatal(err)
	}
	first := waitForDiagnostics(t, client.published)
	if len(first.Diagnostics) != 3 || first.Diagnostics[0].Code != protocol.String("vim/E117") || first.Diagnostics[1].Code != protocol.String("vim/E121") || first.Diagnostics[2].Code != protocol.String("vim/E1001") {
		t.Fatalf("default unresolved diagnostics = %#v", first.Diagnostics)
	}
	for _, diagnostic := range first.Diagnostics {
		if diagnostic.Severity != protocol.DiagnosticSeverityWarning {
			t.Fatalf("default unresolved severity = %v, want warning; diagnostics = %#v", diagnostic.Severity, first.Diagnostics)
		}
	}

	if err := instance.DidChangeConfiguration(context.Background(), &protocol.DidChangeConfigurationParams{
		Settings: protocol.LSPAny([]byte(`{"vimls":{"unresolvedSeverity":"error"}}`)),
	}); err != nil {
		t.Fatal(err)
	}
	updated := waitForDiagnostics(t, client.published)
	if len(updated.Diagnostics) != 3 {
		t.Fatalf("configured unresolved diagnostics = %#v", updated.Diagnostics)
	}
	for _, diagnostic := range updated.Diagnostics {
		if diagnostic.Severity != protocol.DiagnosticSeverityError {
			t.Fatalf("configured unresolved severity = %v, want error; diagnostics = %#v", diagnostic.Severity, updated.Diagnostics)
		}
	}
}

func TestInitializationUnresolvedSeverity(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		InitializationOptions: protocol.LSPAny([]byte(`{"runtimepath":[],"unresolvedSeverity":"hint"}`)),
	}); err != nil {
		t.Fatal(err)
	}
	instance.mu.Lock()
	severity := instance.unresolvedSeverity
	instance.mu.Unlock()
	if severity != syntax.DiagnosticHint {
		t.Fatalf("initial unresolved severity = %v, want hint", severity)
	}
}

func TestServerTruncatesDiagnosticsDeterministically(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 1)}
	instance.mu.Lock()
	instance.client = client
	instance.mu.Unlock()
	_, _ = instance.Initialize(context.Background(), &protocol.InitializeParams{})
	documentURI := uri.MustParse("file:///many-diagnostics.vim")
	_ = instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: strings.Repeat("if true\n", maxDiagnosticsPerDocument+25)},
	})
	result := waitForDiagnostics(t, client.published)
	if len(result.Diagnostics) != maxDiagnosticsPerDocument || result.Diagnostics[len(result.Diagnostics)-1].Code != protocol.String("vimls/diagnostics-truncated") {
		t.Fatalf("diagnostic count = %d, last = %#v", len(result.Diagnostics), result.Diagnostics[len(result.Diagnostics)-1])
	}
}

func TestInitializationTargetOverrideHasPrecedence(t *testing.T) {
	client := &configurationClient{settings: protocol.LSPAny([]byte(`{"targetVersion":"9.2.4","unresolvedSeverity":"hint"}`))}
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.client = client
	supported := true
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		InitializationOptions: []byte(`{"targetVersion":"9.1.1232"}`),
		Capabilities:          protocol.ClientCapabilities{Workspace: &protocol.WorkspaceClientCapabilities{Configuration: &supported}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	if got := instance.TargetVersion().String(); got != "9.1.1232" {
		t.Fatalf("target version = %q", got)
	}
	instance.mu.Lock()
	severity := instance.unresolvedSeverity
	instance.mu.Unlock()
	if client.calls != 1 || severity != syntax.DiagnosticHint {
		t.Fatalf("configuration calls = %d, unresolved severity = %v", client.calls, severity)
	}
	if revision := instance.documents.ConfigRevision(); revision != 2 {
		t.Fatalf("config revision = %d, want 2", revision)
	}
}

func TestServerPublishesVersionedSyntaxDiagnosticsAndClearsThem(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
	instance.mu.Lock()
	instance.client = client
	instance.mu.Unlock()
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{}); err != nil {
		t.Fatal(err)
	}
	documentURI := uri.MustParse("file:///diagnostics.vim")
	_ = instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: "if true\n"},
	})
	first := waitForDiagnostics(t, client.published)
	if version, ok := first.Version.Get(); !ok || version != 1 || len(first.Diagnostics) != 1 || first.Diagnostics[0].Code != protocol.String("vimls/missing-end") {
		t.Fatalf("first diagnostics = %#v", first)
	}
	_ = instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{Text: "let g:value = 1\n"}},
	})
	cleared := waitForDiagnostics(t, client.published)
	if version, ok := cleared.Version.Get(); !ok || version != 2 || len(cleared.Diagnostics) != 0 {
		t.Fatalf("cleared diagnostics = %#v", cleared)
	}
	instance.publishMu.Lock()
	parsed := instance.parsed[documentURI.String()].file
	instance.publishMu.Unlock()
	if parsed == nil || parsed.Dialect != syntax.Legacy || len(parsed.Commands) != 1 {
		t.Fatalf("parsed file = %#v", parsed)
	}
}

func TestServerPublishesVersionedSemanticDiagnosticsAndClearsThem(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
	instance.mu.Lock()
	instance.client = client
	instance.mu.Unlock()
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{}); err != nil {
		t.Fatal(err)
	}
	documentURI := uri.MustParse("file:///semantic-diagnostics.vim")
	_ = instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI: documentURI, Version: 1,
			Text: "vim9script\nconst value = 1\nvalue = 2\n",
		},
	})
	first := waitForDiagnostics(t, client.published)
	if version, ok := first.Version.Get(); !ok || version != 1 || len(first.Diagnostics) != 1 {
		t.Fatalf("first diagnostics = %#v", first)
	}
	diagnostic := first.Diagnostics[0]
	if diagnostic.Code != protocol.String("vim/E46") || diagnostic.Message != protocol.String(`Cannot change read-only variable "value"`) ||
		diagnostic.Range != (protocol.Range{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 2, Character: 5}}) {
		t.Fatalf("semantic diagnostic = %#v", diagnostic)
	}

	_ = instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{
			Text: "vim9script\nvar value = 1\nvalue = 2\n",
		}},
	})
	cleared := waitForDiagnostics(t, client.published)
	if version, ok := cleared.Version.Get(); !ok || version != 2 || len(cleared.Diagnostics) != 0 {
		t.Fatalf("cleared diagnostics = %#v", cleared)
	}
}

func TestServerPublishesImportMemberDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "lib.vim", `vim9script
export var Public = 1
# deprecated use Public
export var Old = 0
# @deprecated use PublicFunc
export def OldFunc(): number
  return 0
enddef
var Private = 2
type PrivateType = number
def Holder()
  var Missing = 3
enddef
`)
	instance, published := initializeWorkspaceDiagnosticServer(t, root)
	importerPath := filepath.Join(root, "main.vim")
	importerURI := uri.File(importerPath)
	source := `vim9script
import './lib.vim' as Lib
echo Lib.Public
echo Lib.Old
echo Lib.OldFunc()
echo Lib.Private
echo Lib.Missing
Lib.Private = 4
var typed: Lib.PrivateType
`
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: importerURI, Version: 1, Text: source,
	}}); err != nil {
		t.Fatal(err)
	}
	params := waitForDiagnosticsForURI(t, published, importerURI)
	var e1048, e1049, deprecated []protocol.Diagnostic
	for _, diagnostic := range params.Diagnostics {
		switch diagnostic.Code {
		case protocol.String("vim/E1048"):
			e1048 = append(e1048, diagnostic)
		case protocol.String("vim/E1049"):
			e1049 = append(e1049, diagnostic)
		case protocol.String("vimls/deprecated"):
			deprecated = append(deprecated, diagnostic)
		}
	}
	if len(e1048) != 1 || e1048[0].Message != protocol.String("Item not found in script: Missing") || e1048[0].Range.Start.Line != 6 {
		t.Fatalf("E1048 diagnostics = %#v; all=%#v", e1048, params.Diagnostics)
	}
	if len(e1049) != 3 {
		t.Fatalf("E1049 diagnostics = %#v; all=%#v", e1049, params.Diagnostics)
	}
	wantMessages := map[string]int{
		"Item not exported in script: Private":     2,
		"Item not exported in script: PrivateType": 1,
	}
	for _, diagnostic := range e1049 {
		message, ok := diagnostic.Message.(protocol.String)
		if !ok {
			t.Fatalf("E1049 message = %#v", diagnostic.Message)
		}
		wantMessages[string(message)]--
	}
	for message, remaining := range wantMessages {
		if remaining != 0 {
			t.Fatalf("E1049 message %q remaining=%d diagnostics=%#v", message, remaining, e1049)
		}
	}
	if len(deprecated) != 2 {
		t.Fatalf("deprecated import diagnostics = %#v; all=%#v", deprecated, params.Diagnostics)
	}
	for _, diagnostic := range deprecated {
		tags := diagnostic.Tags.Slice()
		if diagnostic.Severity != protocol.DiagnosticSeverityHint || len(tags) != 1 || tags[0] != protocol.DiagnosticTagDeprecated {
			t.Fatalf("deprecated import diagnostic = %#v tags=%#v", diagnostic, tags)
		}
	}
}

func TestServerDoesNotBindMemberToLaterImport(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "lib.vim", "vim9script\nvar Private = 1\n")
	instance, published := initializeWorkspaceDiagnosticServer(t, root)
	importerURI := uri.File(filepath.Join(root, "forward.vim"))
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: importerURI, Version: 1, Text: "vim9script\nimport './lib.vim' as Lib\necho Lib.Private\n",
	}}); err != nil {
		t.Fatal(err)
	}
	initial := waitForDiagnosticsForURI(t, published, importerURI)
	if len(initial.Diagnostics) != 1 || initial.Diagnostics[0].Code != protocol.String("vim/E1049") {
		t.Fatalf("initial import diagnostics = %#v", initial.Diagnostics)
	}
	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: importerURI}, Version: 2},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: "vim9script\necho Lib.Private\nimport './lib.vim' as Lib\n"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	params := waitForDiagnosticsForURI(t, published, importerURI)
	for _, diagnostic := range params.Diagnostics {
		if diagnostic.Code == protocol.String("vim/E1048") || diagnostic.Code == protocol.String("vim/E1049") {
			t.Fatalf("forward member was bound to later import: %#v", params.Diagnostics)
		}
	}
}

func TestServerPublishesOnlyProvableE1053ImportFailures(t *testing.T) {
	root := t.TempDir()
	instance, published := initializeWorkspaceDiagnosticServer(t, root)
	importerURI := uri.File(filepath.Join(root, "missing-imports.vim"))
	source := `vim9script
import './missing.vim' as Missing
import autoload 'runtimeMissing.vim' as Runtime
import autoload './relativeMissing.vim' as Relative
var dynamicPath = './dynamic.vim'
import dynamicPath as Dynamic
`
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: importerURI, Version: 1, Text: source,
	}}); err != nil {
		t.Fatal(err)
	}
	params := waitForDiagnosticsForURI(t, published, importerURI)
	var imports []protocol.Diagnostic
	for _, diagnostic := range params.Diagnostics {
		if diagnostic.Code == protocol.String("vim/E1053") {
			imports = append(imports, diagnostic)
		}
	}
	if len(imports) != 2 || imports[0].Message != protocol.String(`Could not import "./missing.vim"`) || imports[1].Message != protocol.String(`Could not import "runtimeMissing.vim"`) {
		t.Fatalf("E1053 diagnostics = %#v; all=%#v", imports, params.Diagnostics)
	}
	var autoloadPaths []protocol.Diagnostic
	for _, diagnostic := range params.Diagnostics {
		if diagnostic.Code == protocol.String("vim/E1264") {
			autoloadPaths = append(autoloadPaths, diagnostic)
		}
	}
	if len(autoloadPaths) != 1 || autoloadPaths[0].Message != protocol.String("Autoload import cannot use absolute or relative path: ./relativeMissing.vim") || autoloadPaths[0].Range.Start.Line != 3 {
		t.Fatalf("E1264 diagnostics = %#v; all=%#v", autoloadPaths, params.Diagnostics)
	}
}

func TestServerPublishesE1088ForSelfImport(t *testing.T) {
	root := t.TempDir()
	path := writeWorkspaceFile(t, root, "self.vim", "vim9script\nimport './self.vim' as Self\n")
	instance, published := initializeWorkspaceDiagnosticServer(t, root)
	documentURI := uri.File(path)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: "vim9script\nimport './self.vim' as Self\n",
	}}); err != nil {
		t.Fatal(err)
	}
	params := waitForDiagnosticsForURI(t, published, documentURI)
	var selfImports []protocol.Diagnostic
	for _, diagnostic := range params.Diagnostics {
		if diagnostic.Code == protocol.String("vim/E1088") {
			selfImports = append(selfImports, diagnostic)
		}
	}
	if len(selfImports) != 1 || selfImports[0].Message != protocol.String("Script cannot import itself") || selfImports[0].Range.Start.Line != 1 {
		t.Fatalf("E1088 diagnostics = %#v; all=%#v", selfImports, params.Diagnostics)
	}
}

func TestServerPublishesE1262ForResolvedDuplicateImport(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport var Value = 1\n")
	instance, published := initializeWorkspaceDiagnosticServer(t, root)
	importerURI := uri.File(filepath.Join(root, "duplicate-import.vim"))
	source := "vim9script\nimport './lib.vim' as First\nimport './lib.vim' as Second\n"
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: importerURI, Version: 1, Text: source,
	}}); err != nil {
		t.Fatal(err)
	}
	params := waitForDiagnosticsForURI(t, published, importerURI)
	var duplicates []protocol.Diagnostic
	for _, diagnostic := range params.Diagnostics {
		if diagnostic.Code == protocol.String("vim/E1262") {
			duplicates = append(duplicates, diagnostic)
		}
	}
	if len(duplicates) != 1 || duplicates[0].Message != protocol.String("Cannot import the same script twice: ./lib.vim") || duplicates[0].Range.Start.Line != 2 {
		t.Fatalf("E1262 diagnostics = %#v; all=%#v", duplicates, params.Diagnostics)
	}
}

func TestServerKeepsDeferredAutoloadMembersConservative(t *testing.T) {
	root := t.TempDir()
	autoloadRoot := filepath.Join(root, "autoload")
	if err := os.MkdirAll(autoloadRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceFile(t, autoloadRoot, "lib.vim", "vim9script\nvar Private = 1\n")
	instance, published := initializeWorkspaceDiagnosticServer(t, root)
	importerURI := uri.File(filepath.Join(root, "main.vim"))
	source := `vim9script
import autoload 'lib.vim' as Lib
echo Lib.Private
echo Lib.Missing
def Deferred()
  echo Lib.Private
  echo Lib.Missing
enddef
`
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: importerURI, Version: 1, Text: source,
	}}); err != nil {
		t.Fatal(err)
	}
	params := waitForDiagnosticsForURI(t, published, importerURI)
	var imports []protocol.Diagnostic
	for _, diagnostic := range params.Diagnostics {
		if diagnostic.Code == protocol.String("vim/E1048") || diagnostic.Code == protocol.String("vim/E1049") {
			imports = append(imports, diagnostic)
		}
	}
	if len(imports) != 2 || imports[0].Code != protocol.String("vim/E1049") || imports[0].Range.Start.Line != 2 || imports[1].Code != protocol.String("vim/E1048") || imports[1].Range.Start.Line != 3 {
		t.Fatalf("autoload member diagnostics = %#v; all=%#v", imports, params.Diagnostics)
	}
}

func TestImportTargetChangeReanalyzesReverseDependent(t *testing.T) {
	root := t.TempDir()
	targetPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nvar Value = 1\n")
	instance, published := initializeWorkspaceDiagnosticServer(t, root)
	importerURI := uri.File(filepath.Join(root, "main.vim"))
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: importerURI, Version: 1, Text: "vim9script\nimport './lib.vim' as Lib\necho Lib.Value\n",
	}}); err != nil {
		t.Fatal(err)
	}
	first := waitForDiagnosticsForURI(t, published, importerURI)
	if len(first.Diagnostics) != 1 || first.Diagnostics[0].Code != protocol.String("vim/E1049") {
		t.Fatalf("initial import diagnostics = %#v", first.Diagnostics)
	}

	targetURI := uri.File(targetPath)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: targetURI, Version: 1, Text: "vim9script\nexport var Value = 1\n",
	}}); err != nil {
		t.Fatal(err)
	}
	cleared := waitForDiagnosticsForURI(t, published, importerURI)
	if len(cleared.Diagnostics) != 0 {
		t.Fatalf("exported target diagnostics = %#v", cleared.Diagnostics)
	}

	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: targetURI}, Version: 2},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: "vim9script\nvar Other = 1\n"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	missing := waitForDiagnosticsForURI(t, published, importerURI)
	if len(missing.Diagnostics) != 1 || missing.Diagnostics[0].Code != protocol.String("vim/E1048") || missing.Diagnostics[0].Message != protocol.String("Item not found in script: Value") {
		t.Fatalf("changed target diagnostics = %#v", missing.Diagnostics)
	}
}

func TestGraphRevisionRejectsStaleDiagnostics(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	published := make(chan *protocol.PublishDiagnosticsParams, 1)
	instance.client = &diagnosticClient{published: published}
	instance.analysisMu.Lock()
	instance.analysisStopped = true
	instance.analysisMu.Unlock()
	documentURI := uri.MustParse("file:///stale-graph.vim")
	instance.documents.Open(documentURI.String(), 1, "if true\n")
	work, ok := instance.documents.BeginAnalysis(context.Background(), documentURI.String())
	if !ok {
		t.Fatal("analysis did not start")
	}
	file := syntax.Parse(work.Snapshot.Text())
	if len(file.Diagnostics) == 0 {
		t.Fatal("test source has no diagnostic")
	}
	staleRevision := currentImportGraph(instance).Revision()
	instance.workspaceMu.Lock()
	if err := instance.workspaceGraph.Replace(filepath.Join(t.TempDir(), "changed.vim"), nil); err != nil {
		instance.workspaceMu.Unlock()
		t.Fatal(err)
	}
	instance.workspaceGraphView = instance.workspaceGraph.Snapshot()
	currentRevision := instance.workspaceGraphView.Revision()
	instance.workspaceMu.Unlock()

	instance.publishSyntax(work, file, staleRevision)
	select {
	case params := <-published:
		t.Fatalf("stale diagnostics were published: %#v", params)
	default:
	}
	instance.publishSyntax(work, file, currentRevision)
	params := waitForDiagnostics(t, published)
	if len(params.Diagnostics) == 0 {
		t.Fatalf("current diagnostics = %#v", params)
	}
}

func TestServerPublishesDiagnosticsForNonFileURIWithWorkspaceGraph(t *testing.T) {
	instance, published := initializeWorkspaceDiagnosticServer(t, t.TempDir())
	documentURI := uri.MustParse("untitled:vimls-buffer")
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: "if true\n",
	}}); err != nil {
		t.Fatal(err)
	}
	params := waitForDiagnosticsForURI(t, published, documentURI)
	if len(params.Diagnostics) != 1 || params.Diagnostics[0].Code != protocol.String("vimls/missing-end") {
		t.Fatalf("non-file diagnostics = %#v", params.Diagnostics)
	}
}

func TestOpenDocumentConsumersPreservePureParserCache(t *testing.T) {
	root := t.TempDir()
	targetPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport var Value = 1\nif true\n")
	mainPath := writeWorkspaceFile(t, root, "main.vim", "vim9script\nimport './lib.vim' as Lib\necho Lib.Value\nif true\n")
	instance, _ := initializeWorkspaceDiagnosticServer(t, root)
	instance.stopAnalysis()

	open := func(path, source string) (*text.Snapshot, *syntax.File, []syntax.Diagnostic) {
		t.Helper()
		snapshot := instance.documents.Open(uri.File(path).String(), 1, source)
		file := instance.parseSnapshot(snapshot)
		if file == nil || len(file.Diagnostics) == 0 {
			t.Fatalf("raw parser file for %s = %#v", path, file)
		}
		return snapshot, file, append([]syntax.Diagnostic(nil), file.Diagnostics...)
	}
	assertCached := func(label string, snapshot *text.Snapshot, want *syntax.File, diagnostics []syntax.Diagnostic) {
		t.Helper()
		instance.publishMu.Lock()
		cached := instance.parsed[snapshot.URI()].file
		instance.publishMu.Unlock()
		if cached != want || !reflect.DeepEqual(cached.Diagnostics, diagnostics) {
			t.Fatalf("%s parser cache = %#v, want pointer %p diagnostics %#v", label, cached, want, diagnostics)
		}
	}

	targetSource := "vim9script\nexport var Value = 1\nif true\n"
	mainSource := "vim9script\nimport './lib.vim' as Lib\necho Lib.Value\nif true\n"
	targetSnapshot, targetFile, targetDiagnostics := open(targetPath, targetSource)
	mainSnapshot, mainFile, mainDiagnostics := open(mainPath, mainSource)
	navigationPath := filepath.Join(root, "navigation.vim")
	navigationSnapshot, navigationFile, navigationDiagnostics := open(navigationPath, "vim9script\nvar Current = 1\nif true\n")

	if _, err := instance.navigationAt(context.Background(), navigationSnapshot.URI(), protocol.Position{Line: 1, Character: 5}); err != nil {
		t.Fatal(err)
	}
	assertCached("navigation", navigationSnapshot, navigationFile, navigationDiagnostics)

	var targetFact workspace.SymbolFact
	for _, fact := range workspace.CollectSymbolFacts(targetPath, targetFile) {
		if fact.Name == "Value" && fact.Exported {
			targetFact = fact
			break
		}
	}
	if targetFact.Name == "" {
		t.Fatalf("target facts = %#v", workspace.CollectSymbolFacts(targetPath, targetFile))
	}
	target := workspaceNavigationTarget{
		match:        workspace.SymbolMatch{Fact: targetFact, Source: targetSource},
		openSnapshot: targetSnapshot,
	}
	if _, err := instance.openWorkspaceReferenceLocations(context.Background(), target, text.UTF16); err != nil {
		t.Fatal(err)
	}
	assertCached("rename open references", mainSnapshot, mainFile, mainDiagnostics)

	items := instance.importMemberCompletions(mainSnapshot.URI(), mainFile, "Lib")
	if len(items) != 1 || items[0].Label != "Value" {
		t.Fatalf("open import completion = %#v", items)
	}
	assertCached("completion open import target", targetSnapshot, targetFile, targetDiagnostics)

	if _, ready, _ := instance.workspaceImportDiagnostics(mainSnapshot.URI(), mainFile, analysis.Analyze(mainFile)); !ready {
		t.Fatal("workspace import diagnostics were not ready")
	}
	assertCached("import diagnostics open target", targetSnapshot, targetFile, targetDiagnostics)
}

func TestTargetVersionCompatibilityDiagnosticsReanalyze(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	defer instance.stopAnalysis()
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
	instance.mu.Lock()
	instance.client = client
	instance.mu.Unlock()
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{}); err != nil {
		t.Fatal(err)
	}
	documentURI := uri.MustParse("file:///versioned-syntax.vim")
	_ = instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: "vim9script\nenum Color\n  Red\nendenum\n"},
	})
	first := waitForDiagnostics(t, client.published)
	if len(first.Diagnostics) != 1 || first.Diagnostics[0].Code != protocol.String("vimls/target-version") {
		t.Fatalf("default-target diagnostics = %#v", first)
	}
	snapshot, ok := instance.documents.Snapshot(documentURI.String())
	if !ok {
		t.Fatal("snapshot is missing")
	}
	raw := instance.parseSnapshot(snapshot)
	_ = instance.DidChangeConfiguration(context.Background(), &protocol.DidChangeConfigurationParams{Settings: []byte(`{"targetVersion":"9.2.1015"}`)})
	cleared := waitForDiagnostics(t, client.published)
	if len(cleared.Diagnostics) != 0 {
		t.Fatalf("updated-target diagnostics = %#v", cleared)
	}
	current, ok := instance.documents.Snapshot(documentURI.String())
	if !ok || current != snapshot || instance.parseSnapshot(current) != raw {
		t.Fatalf("configuration changed snapshot/cache = %p/%p, want %p/%p", current, instance.parseSnapshot(current), snapshot, raw)
	}
}

func TestE1406DiagnosticHonorsTargetVersion(t *testing.T) {
	file := syntax.Parse("vim9script\nclass C\n  var value = 1\n  var _value = 2\nendclass\n")
	diagnostics := analysis.Analyze(file).Diagnostics
	if len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1406" {
		t.Fatalf("analysis diagnostics = %#v", diagnostics)
	}

	old := analysisDiagnosticsForTarget(file, diagnostics, TargetVersion{Major: 9, Minor: 2, Patch: 506})
	if len(old) != 1 || old[0].Code != "vim/E1369" || old[0].Message != "Duplicate variable: _value" {
		t.Fatalf("9.2.0506 diagnostics = %#v", old)
	}
	current := analysisDiagnosticsForTarget(file, diagnostics, TargetVersion{Major: 9, Minor: 2, Patch: 507})
	if len(current) != 1 || current[0].Code != "vim/E1406" {
		t.Fatalf("9.2.0507 diagnostics = %#v", current)
	}
}

func TestWorkspaceConfigurationRequestOnInitializedAndNullChange(t *testing.T) {
	client := &configurationClient{settings: protocol.LSPAny([]byte(`{"targetVersion":"9.2.4"}`))}
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.client = client
	supported := true
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		InitializationOptions: protocol.LSPAny([]byte(`{"runtimepath":[]}`)),
		Capabilities:          protocol.ClientCapabilities{Workspace: &protocol.WorkspaceClientCapabilities{Configuration: &supported}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	if client.calls != 1 || instance.TargetVersion().String() != "9.2.0004" {
		t.Fatalf("initialized configuration calls=%d target=%s", client.calls, instance.TargetVersion().String())
	}
	client.settings = protocol.LSPAny([]byte(`{"targetVersion":"latest"}`))
	if err := instance.DidChangeConfiguration(context.Background(), &protocol.DidChangeConfigurationParams{Settings: protocol.LSPAny([]byte("null"))}); err != nil {
		t.Fatal(err)
	}
	if client.calls != 2 || instance.TargetVersion().String() != "latest" {
		t.Fatalf("changed configuration calls=%d target=%s", client.calls, instance.TargetVersion().String())
	}
}

func initializeWorkspaceDiagnosticServer(t *testing.T, root string) (*Server, <-chan *protocol.PublishDiagnosticsParams) {
	t.Helper()
	published := make(chan *protocol.PublishDiagnosticsParams, 16)
	client := &diagnosticClient{published: published}
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.client = client
	rootURI := uri.File(root)
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI: &rootURI, InitializationOptions: protocol.LSPAny([]byte(`{"runtimepath":[]}`)),
	}); err != nil {
		t.Fatal(err)
	}
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	return instance, published
}

func waitForDiagnosticsForURI(t *testing.T, published <-chan *protocol.PublishDiagnosticsParams, documentURI uri.URI) *protocol.PublishDiagnosticsParams {
	t.Helper()
	for {
		params := waitForDiagnostics(t, published)
		if params.URI == documentURI {
			return params
		}
	}
}
