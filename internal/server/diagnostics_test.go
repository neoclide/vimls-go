package server

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
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
	var e1048, e1049 []protocol.Diagnostic
	for _, diagnostic := range params.Diagnostics {
		switch diagnostic.Code {
		case protocol.String("vim/E1048"):
			e1048 = append(e1048, diagnostic)
		case protocol.String("vim/E1049"):
			e1049 = append(e1049, diagnostic)
		}
	}
	if len(e1048) != 1 || e1048[0].Message != protocol.String("Item not found in script: Missing") || e1048[0].Range.Start.Line != 4 {
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
	_ = instance.DidChangeConfiguration(context.Background(), &protocol.DidChangeConfigurationParams{Settings: []byte(`{"targetVersion":"9.2.1015"}`)})
	cleared := waitForDiagnostics(t, client.published)
	if len(cleared.Diagnostics) != 0 {
		t.Fatalf("updated-target diagnostics = %#v", cleared)
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
