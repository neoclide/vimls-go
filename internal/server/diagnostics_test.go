package server

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type refreshDiagnosticClient struct {
	protocol.UnimplementedClient
	calls   chan struct{}
	release chan struct{}
}

func (c *refreshDiagnosticClient) DiagnosticRefresh(ctx context.Context) error {
	c.calls <- struct{}{}
	select {
	case <-c.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestProtocolDiagnosticSeverity(t *testing.T) {
	for _, code := range []string{"vim/E171", "vim/E113", "vim/E518", "vim/E1012", "future/source"} {
		if got := protocolDiagnosticSeverity(code); got != protocol.DiagnosticSeverityError {
			t.Errorf("%s severity = %v, want error", code, got)
		}
	}
	for _, code := range []string{"vim/E117", "vim/E121", "vim/E1001", "vim/E1089"} {
		if got := protocolDiagnosticSeverity(code); got != protocol.DiagnosticSeverityWarning {
			t.Errorf("%s severity = %v, want warning", code, got)
		}
	}
	if got := protocolDiagnosticSeverity("vim/E122"); got != protocol.DiagnosticSeverityWarning {
		t.Errorf("vim/E122 severity = %v, want warning", got)
	}
	if got := protocolDiagnosticSeverity("vim/E174"); got != protocol.DiagnosticSeverityWarning {
		t.Errorf("vim/E174 severity = %v, want warning", got)
	}
	if got := protocolDiagnosticSeverity("vim/E464"); got != protocol.DiagnosticSeverityWarning {
		t.Errorf("vim/E464 severity = %v, want warning", got)
	}
	for _, code := range []string{"vim/E705", "vim/E707"} {
		if got := protocolDiagnosticSeverity(code); got != protocol.DiagnosticSeverityWarning {
			t.Errorf("%s severity = %v, want warning", code, got)
		}
	}
	if got := protocolDiagnosticSeverity("vimls/deprecated"); got != protocol.DiagnosticSeverityHint {
		t.Errorf("vimls/deprecated severity = %v, want hint", got)
	}
	if got := protocolDiagnosticSeverity("vimls/unused-variable"); got != protocol.DiagnosticSeverityHint {
		t.Errorf("vimls/unused-variable severity = %v, want hint", got)
	}
	for _, definition := range syntax.VimlsDiagnosticDefinitions {
		if got := protocolDiagnosticSeverity(definition.Code); got == protocol.DiagnosticSeverityError {
			t.Errorf("%s severity = %v, vimls-owned diagnostics must not be errors", definition.Code, got)
		}
	}
}

func TestServerPublishesConfiguredDiagnosticSeverities(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 1)}
	instance.mu.Lock()
	instance.client = client
	instance.overrideDiagnostics = map[string]protocol.DiagnosticSeverity{
		"vim/E121":         protocol.DiagnosticSeverityError,
		"vimls/deprecated": protocol.DiagnosticSeverityInformation,
		"vim/E117":         protocol.DiagnosticSeverityError,
	}
	instance.mu.Unlock()
	documentURI := uri.MustParse("file:///configured.vim")
	snapshot := instance.documents.Open(documentURI.String(), 1, "x\n")
	work, ok := instance.documents.BeginAnalysis(instance.analysisContext, documentURI.String())
	if !ok {
		t.Fatal("analysis did not start")
	}
	instance.workspaceMu.Lock()
	identity := instance.workspaceIdentityLocked()
	instance.workspaceMu.Unlock()
	instance.publishSyntax(work, &syntax.File{Source: snapshot.Text(), Diagnostics: []syntax.Diagnostic{
		{Code: "vim/E117", Message: "unresolved", Span: syntax.Span{Start: 0, End: 1}},
		{Code: "vim/E121", Message: "unknown variable", Span: syntax.Span{Start: 0, End: 1}},
		{Code: "vimls/deprecated", Message: "deprecated", Span: syntax.Span{Start: 0, End: 1}},
	}}, identity)
	params := waitForDiagnostics(t, client.published)
	if len(params.Diagnostics) != 3 {
		t.Fatalf("configured diagnostics = %#v", params.Diagnostics)
	}
	if params.Diagnostics[0].Code != protocol.String("vim/E117") || params.Diagnostics[0].Severity != protocol.DiagnosticSeverityError ||
		params.Diagnostics[1].Code != protocol.String("vim/E121") || params.Diagnostics[1].Severity != protocol.DiagnosticSeverityError ||
		params.Diagnostics[2].Code != protocol.String("vimls/deprecated") || params.Diagnostics[2].Severity != protocol.DiagnosticSeverityInformation {
		t.Fatalf("configured diagnostics = %#v", params.Diagnostics)
	}
}

func TestDisabledDiagnosticsAreFilteredBeforeTruncationAndRepublished(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
	instance.mu.Lock()
	instance.client = client
	instance.mu.Unlock()
	documentURI := uri.MustParse("file:///filtered.vim")
	_ = instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, Version: 1, Text: strings.Repeat("if true\n", maxDiagnosticsPerDocument+25),
	}})
	first := waitForDiagnostics(t, client.published)
	if len(first.Diagnostics) != maxDiagnosticsPerDocument {
		t.Fatalf("initial diagnostics = %d, want cap", len(first.Diagnostics))
	}
	if err := instance.applyWorkspaceConfiguration(context.Background(), []byte(`{"vim":{"diagnostic":{"disabled":["vim/E171"],"override":{"vim/E171":"error"}}}}`)); err != nil {
		t.Fatal(err)
	}
	filtered := waitForDiagnostics(t, client.published)
	if len(filtered.Diagnostics) != 0 {
		t.Fatalf("disabled diagnostics consumed cap: %#v", filtered.Diagnostics)
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
	index, graph, files, warnings := instance.buildWorkspaceIndex(context.Background(), []string{runtimeRoot}, []string{runtimeRoot}, workspacePathResolver(nil, []string{runtimeRoot}), nil)
	if len(warnings) != 0 || !index.Complete() || index.FileCount() != 2 {
		t.Fatalf("runtimepath index: files=%d complete=%v warnings=%#v", index.FileCount(), index.Complete(), warnings)
	}
	instance.workspaceMu.Lock()
	instance.workspaceIndex = index
	instance.workspaceGraph = graph
	instance.workspaceGraphView = graph.Snapshot()
	instance.workspaceFiles = files
	instance.workspaceBuilt = true
	instance.workspaceRoots = []string{mustWorkspaceCanonicalPath(t, runtimeRoot)}
	instance.workspaceMu.Unlock()

	file := syntax.Parse("BuildP\nLocalC\nBuildProject\n")
	currentPath := mustWorkspaceCanonicalPath(t, filepath.Join(runtimeRoot, "plugin/current.vim"))
	instance.workspaceMu.Lock()
	snapshot := instance.workspaceAnalysisSnapshotLocked(currentPath, file)
	instance.workspaceMu.Unlock()
	if !snapshot.indexComplete {
		t.Fatal("complete runtimepath index was not captured")
	}
	diagnostics := analysis.UserCommandAbbreviationDiagnostics(file, snapshot.userCommandNames)
	if len(diagnostics) != 2 || file.Text(diagnostics[0].Span) != "BuildP" || file.Text(diagnostics[1].Span) != "LocalC" {
		t.Fatalf("E464 diagnostics = %#v", diagnostics)
	}
	index.SetComplete(false)
	instance.workspaceMu.Lock()
	snapshot = instance.workspaceAnalysisSnapshotLocked(currentPath, file)
	instance.workspaceMu.Unlock()
	if snapshot.indexComplete || len(snapshot.userCommandNames) != 0 {
		t.Fatalf("incomplete runtimepath index was exposed: %#v", snapshot.userCommandNames)
	}
}

func TestE705E707DiagnosticsUseInitialGlobalNameIndex(t *testing.T) {
	runtimeRoot := t.TempDir()
	writeWorkspaceFile(t, runtimeRoot, "plugin/function.vim", "function Shared()\nendfunction\n")
	writeWorkspaceFile(t, runtimeRoot, "plugin/variable.vim", "let g:Other = 1\n")
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	canonicalRuntimeRoot := mustWorkspaceCanonicalPath(t, runtimeRoot)
	index, graph, files, warnings := instance.buildWorkspaceIndex(context.Background(), []string{canonicalRuntimeRoot}, []string{canonicalRuntimeRoot}, workspacePathResolver(nil, []string{canonicalRuntimeRoot}), nil)
	if len(warnings) != 0 || index.FileCount() != 2 {
		t.Fatalf("runtimepath index: files=%d warnings=%#v", index.FileCount(), warnings)
	}
	instance.workspaceMu.Lock()
	instance.workspaceIndex = index
	instance.workspaceGraph = graph
	instance.workspaceGraphView = graph.Snapshot()
	instance.workspaceFiles = files
	instance.workspaceBuilt = true
	instance.workspaceRoots = []string{canonicalRuntimeRoot}
	instance.workspaceMu.Unlock()

	variablePath := filepath.Join(runtimeRoot, "plugin/current-variable.vim")
	variableFile := syntax.Parse("let g:Shared = 1\n")
	instance.workspaceMu.Lock()
	variableSnapshot := instance.workspaceAnalysisSnapshotLocked(mustWorkspaceCanonicalPath(t, variablePath), variableFile)
	instance.workspaceMu.Unlock()
	if got := variableSnapshot.globalDiagnostics; len(got) != 1 || got[0].Code != "vim/E705" {
		t.Fatalf("E705 diagnostics = %#v", got)
	}
	functionPath := filepath.Join(runtimeRoot, "plugin/current-function.vim")
	functionFile := syntax.Parse("function Other()\nendfunction\n")
	instance.workspaceMu.Lock()
	functionSnapshot := instance.workspaceAnalysisSnapshotLocked(mustWorkspaceCanonicalPath(t, functionPath), functionFile)
	instance.workspaceMu.Unlock()
	if got := functionSnapshot.globalDiagnostics; len(got) != 1 || got[0].Code != "vim/E707" {
		t.Fatalf("E707 diagnostics = %#v", got)
	}

	instance.workspaceMu.Lock()
	instance.workspaceBuilt = false
	variableSnapshot = instance.workspaceAnalysisSnapshotLocked(mustWorkspaceCanonicalPath(t, variablePath), variableFile)
	instance.workspaceMu.Unlock()
	if got := variableSnapshot.globalDiagnostics; len(got) != 0 {
		t.Fatalf("unready index produced global conflict warning: %#v", got)
	}
	instance.workspaceMu.Lock()
	instance.workspaceBuilt = true
	instance.workspaceIndex.SetComplete(false)
	snapshot := instance.workspaceAnalysisSnapshotLocked(mustWorkspaceCanonicalPath(t, functionPath), functionFile)
	instance.workspaceMu.Unlock()
	if snapshot.indexComplete || len(snapshot.globalDiagnostics) != 1 || snapshot.globalDiagnostics[0].Code != "vim/E707" {
		t.Fatalf("incomplete index global snapshot = %#v", snapshot)
	}
}

func TestServerPublishesUnresolvedDiagnosticsAsWarnings(t *testing.T) {
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
	source := strings.Repeat("if true\n", maxDiagnosticsPerDocument+25)
	_ = instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source},
	})
	first := waitForDiagnostics(t, client.published)
	if len(first.Diagnostics) != maxDiagnosticsPerDocument {
		t.Fatalf("diagnostic count = %d, want %d", len(first.Diagnostics), maxDiagnosticsPerDocument)
	}
	marker := first.Diagnostics[len(first.Diagnostics)-1]
	wantEOF := protocol.Position{Line: uint32(maxDiagnosticsPerDocument + 25)}
	if marker.Code != protocol.String("vimls/diagnostics-truncated") || marker.Range.Start != wantEOF || marker.Range.End != wantEOF {
		t.Fatalf("diagnostic count = %d, last = %#v", len(first.Diagnostics), first.Diagnostics[len(first.Diagnostics)-1])
	}
	for index, diagnostic := range first.Diagnostics[:len(first.Diagnostics)-1] {
		if diagnostic.Code == protocol.String("vimls/diagnostics-truncated") || index > 0 && first.Diagnostics[index-1].Range.Start.Line > diagnostic.Range.Start.Line {
			t.Fatalf("retained diagnostic %d = %#v", index, diagnostic)
		}
	}
	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: source},
		},
	}); err != nil {
		t.Fatal(err)
	}
	second := waitForDiagnostics(t, client.published)
	if !reflect.DeepEqual(first.Diagnostics, second.Diagnostics) {
		t.Fatalf("diagnostic truncation changed across identical analysis:\nfirst=%#v\nsecond=%#v", first.Diagnostics, second.Diagnostics)
	}
}

func TestInitializationConfigurationAppliesWorkspaceSettings(t *testing.T) {
	client := &configurationClient{settings: protocol.LSPAny([]byte(`{"workspace":{"rebuildDebounce":250}}`))}
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.client = client
	supported := true
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		InitializationOptions: []byte(`{}`),
		Capabilities:          protocol.ClientCapabilities{Workspace: &protocol.WorkspaceClientCapabilities{Configuration: &supported}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceMu.Lock()
	delay := instance.workspaceDelay
	instance.workspaceMu.Unlock()
	if client.calls != 1 || delay != 250*time.Millisecond {
		t.Fatalf("configuration calls = %d, workspace delay = %s", client.calls, delay)
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
	if version, ok := first.Version.Get(); !ok || version != 1 || len(first.Diagnostics) != 1 || first.Diagnostics[0].Code != protocol.String("vim/E171") {
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

func TestServerPublishesSemanticDiagnosticsAndClearsThem(t *testing.T) {
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
	libPath := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "lib.vim", `vim9script
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
`))
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
	if len(e1048[0].RelatedInformation) != 0 {
		t.Fatalf("missing member has related declaration: %#v", e1048[0].RelatedInformation)
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
		wantLine := map[string]uint32{
			"Item not exported in script: Private":     8,
			"Item not exported in script: PrivateType": 9,
		}[string(message)]
		if len(diagnostic.RelatedInformation) != 1 || diagnostic.RelatedInformation[0].Location.URI != uri.File(libPath) ||
			diagnostic.RelatedInformation[0].Location.Range.Start.Line != wantLine || diagnostic.RelatedInformation[0].Message == "" {
			t.Fatalf("E1049 related information = %#v", diagnostic.RelatedInformation)
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
		message, ok := diagnostic.Message.(protocol.String)
		if !ok {
			t.Fatalf("deprecated message = %#v", diagnostic.Message)
		}
		wantLine := map[string]uint32{"Old is deprecated": 3, "OldFunc is deprecated": 5}[string(message)]
		if diagnostic.Severity != protocol.DiagnosticSeverityHint || len(tags) != 1 || tags[0] != protocol.DiagnosticTagDeprecated ||
			len(diagnostic.RelatedInformation) != 1 || diagnostic.RelatedInformation[0].Location.URI != uri.File(libPath) ||
			diagnostic.RelatedInformation[0].Location.Range.Start.Line != wantLine || diagnostic.RelatedInformation[0].Message == "" {
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
	newWork := func(t *testing.T) (*Server, workspace.Analysis, *syntax.File, <-chan *protocol.PublishDiagnosticsParams) {
		t.Helper()
		instance := New(nil, nil, io.Discard)
		t.Cleanup(instance.stopAnalysis)
		published := make(chan *protocol.PublishDiagnosticsParams, 1)
		instance.client = &diagnosticClient{published: published}
		// Keep stale-result requeue visible without a background worker consuming it.
		instance.analysisMu.Lock()
		instance.analysisWorkers = 1
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
		return instance, work, file, published
	}
	assertRequeued := func(t *testing.T, instance *Server, documentURI string) {
		t.Helper()
		instance.analysisMu.Lock()
		_, queued := instance.analysisPending[documentURI]
		delete(instance.analysisPending, documentURI)
		instance.analysisMu.Unlock()
		if !queued {
			t.Fatal("current document was not requeued after stale workspace identity")
		}
	}
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *Server)
	}{
		{"generation", func(_ *testing.T, instance *Server) {
			instance.workspaceMu.Lock()
			instance.workspaceRevision++
			instance.workspaceMu.Unlock()
		}},
		{"index revision", func(_ *testing.T, instance *Server) {
			instance.workspaceMu.Lock()
			instance.workspaceIndex.SetComplete(true)
			instance.workspaceMu.Unlock()
		}},
		{"new index same revision", func(_ *testing.T, instance *Server) {
			instance.workspaceMu.Lock()
			instance.workspaceIndex = workspace.NewIndex(maxWorkspaceFiles, maxIndexBytes)
			instance.workspaceMu.Unlock()
		}},
		{"graph revision", func(t *testing.T, instance *Server) {
			instance.workspaceMu.Lock()
			err := instance.workspaceGraph.Replace(filepath.Join(t.TempDir(), "changed.vim"), nil)
			instance.workspaceGraphView = instance.workspaceGraph.Snapshot()
			instance.workspaceMu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance, work, file, published := newWork(t)
			instance.workspaceMu.Lock()
			identity := instance.workspaceIdentityLocked()
			instance.workspaceMu.Unlock()
			test.mutate(t, instance)
			instance.publishSyntax(work, file, identity)
			select {
			case params := <-published:
				t.Fatalf("stale diagnostics were published: %#v", params)
			default:
			}
			assertRequeued(t, instance, work.Snapshot.URI())
		})
	}
	t.Run("document and configuration stale do not requeue", func(t *testing.T) {
		for _, mutate := range []func(*Server, workspace.Analysis){
			func(instance *Server, _ workspace.Analysis) {
				_, _, _ = instance.documents.Change("file:///stale-graph.vim", 2, text.UTF16, []text.Change{{Text: "if true\n"}})
			},
			func(instance *Server, _ workspace.Analysis) { instance.documents.ConfigurationChanged() },
		} {
			instance, work, file, published := newWork(t)
			instance.workspaceMu.Lock()
			identity := instance.workspaceIdentityLocked()
			instance.workspaceMu.Unlock()
			mutate(instance, work)
			instance.publishSyntax(work, file, identity)
			select {
			case params := <-published:
				t.Fatalf("stale document diagnostics were published: %#v", params)
			default:
			}
			instance.analysisMu.Lock()
			_, queued := instance.analysisPending[work.Snapshot.URI()]
			instance.analysisMu.Unlock()
			if queued {
				t.Fatal("stale document/config result was requeued")
			}
		}
	})
}

func TestWorkspaceIdentityCapturesCrossFileInputs(t *testing.T) {
	root := t.TempDir()
	targetPath := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "target.vim", "vim9script\nexport var Value = 1\ncommand! BuildProject echo 'build'\nlet g:Shared = 1\n"))
	conflictPath := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "conflict.vim", "function Shared()\nendfunction\n"))
	mainPath := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "main.vim", "vim9script\nimport './target.vim' as Target\necho Target.Value\nBuildP\nlet g:Shared = 1\nif true\n"))
	root = mustWorkspaceCanonicalPath(t, root)
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	published := make(chan *protocol.PublishDiagnosticsParams, 1)
	instance.client = &diagnosticClient{published: published}
	instance.setWorkspaceRoots([]string{root})
	instance.workspaceMu.Lock()
	instance.workspaceBuilt = true
	instance.workspaceResolver = workspacePathResolver([]string{root}, nil)
	instance.workspaceIndex.SetComplete(true)
	instance.workspaceGraph.SetReady(true)
	instance.workspaceGraphView = instance.workspaceGraph.Snapshot()
	instance.workspaceMu.Unlock()
	instance.replaceWorkspaceFile(uri.File(targetPath).String(), syntax.Parse("vim9script\nexport var Value = 1\ncommand! BuildProject echo 'build'\nlet g:Shared = 1\n"))
	instance.replaceWorkspaceFile(uri.File(conflictPath).String(), syntax.Parse("function Shared()\nendfunction\n"))
	documentURI := uri.File(mainPath)
	mainSource := "vim9script\nimport './target.vim' as Target\necho Target.Value\nBuildP\nlet g:Shared = 1\nif true\n"
	instance.documents.Open(documentURI.String(), 1, mainSource)
	work, ok := instance.documents.BeginAnalysis(context.Background(), documentURI.String())
	if !ok {
		t.Fatal("analysis did not start")
	}
	mainFile := syntax.Parse(mainSource)
	instance.publishMu.Lock()
	snapshot, _ := instance.replaceWorkspaceFileWithSnapshot(documentURI.String(), mainFile)
	instance.publishMu.Unlock()
	if snapshot.targets[targetPath].source == "" || len(snapshot.targets[targetPath].symbols) == 0 || !snapshot.indexComplete || len(snapshot.userCommandNames) != 1 || len(snapshot.globalDiagnostics) != 1 || len(snapshot.roots) != 1 || snapshot.roots[0] != root {
		t.Fatalf("captured workspace snapshot = %#v", snapshot)
	}
	if diagnostics := instance.workspaceImportDiagnostics(snapshot, mainFile, analysis.Analyze(mainFile)); len(diagnostics) != 0 {
		t.Fatalf("captured import diagnostics = %#v", diagnostics)
	}
	instance.workspaceMu.Lock()
	if err := instance.workspaceIndex.Replace(targetPath, syntax.Parse("vim9script\nexport var Other = 1\n")); err != nil {
		instance.workspaceMu.Unlock()
		t.Fatal(err)
	}
	if err := instance.workspaceGraph.Replace(targetPath, nil); err != nil {
		instance.workspaceMu.Unlock()
		t.Fatal(err)
	}
	instance.workspaceGraphView = instance.workspaceGraph.Snapshot()
	instance.workspaceRoots = []string{t.TempDir()}
	instance.workspaceRevision++
	instance.workspaceMu.Unlock()
	if diagnostics := instance.workspaceImportDiagnostics(snapshot, mainFile, analysis.Analyze(mainFile)); len(diagnostics) != 0 {
		t.Fatalf("later workspace mutation changed captured diagnostics = %#v", diagnostics)
	}
	instance.analysisMu.Lock()
	instance.analysisWorkers = 1
	instance.analysisMu.Unlock()
	instance.publishSyntax(work, mainFile, snapshot.identity)
	select {
	case params := <-published:
		t.Fatalf("stale workspace snapshot published diagnostics: %#v", params)
	default:
	}
	instance.analysisMu.Lock()
	_, queued := instance.analysisPending[documentURI.String()]
	instance.analysisMu.Unlock()
	if !queued {
		t.Fatal("stale workspace snapshot did not requeue current document")
	}
}

func TestWorkspaceIdentityOversizedTransitionRejectsOldPublish(t *testing.T) {
	root := t.TempDir()
	mainPath := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "main.vim", "if true\n"))
	dependentPath := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "dependent.vim", "vim9script\nimport './main.vim' as Main\n"))
	file := syntax.Parse("if true\n")
	root = mustWorkspaceCanonicalPath(t, root)
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	published := make(chan *protocol.PublishDiagnosticsParams, 1)
	instance.client = &diagnosticClient{published: published}
	instance.setWorkspaceRoots([]string{root})
	instance.workspaceMu.Lock()
	instance.workspaceBuilt = true
	instance.workspaceResolver = workspacePathResolver([]string{root}, nil)
	instance.workspaceGraph.SetReady(true)
	instance.workspaceGraphView = instance.workspaceGraph.Snapshot()
	instance.workspaceMu.Unlock()
	instance.replaceWorkspaceFile(uri.File(mainPath).String(), file)
	instance.replaceWorkspaceFile(uri.File(dependentPath).String(), syntax.Parse("vim9script\nimport './main.vim' as Main\n"))
	documentURI := uri.File(mainPath)
	instance.documents.Open(documentURI.String(), 1, file.Source)
	work, ok := instance.documents.BeginAnalysis(context.Background(), documentURI.String())
	if !ok {
		t.Fatal("analysis did not start")
	}
	instance.publishMu.Lock()
	oldSnapshot, _ := instance.replaceWorkspaceFileWithSnapshot(documentURI.String(), file)
	_, dependents := instance.replaceWorkspaceFileWithSnapshot(documentURI.String(), nil)
	instance.publishMu.Unlock()
	if len(dependents) != 1 || dependents[0] != dependentPath {
		t.Fatalf("oversized transition dependents = %#v, want %q", dependents, dependentPath)
	}
	instance.workspaceMu.Lock()
	stale := !instance.workspaceIdentityCurrentLocked(oldSnapshot.identity)
	_, indexed := instance.workspaceIndex.Source(mainPath)
	graph := instance.workspaceGraphView
	instance.workspaceMu.Unlock()
	if !stale || indexed || graph.Has(mainPath) {
		t.Fatalf("oversized transition stale=%t indexed=%t graph=%t", stale, indexed, graph.Has(mainPath))
	}
	instance.analysisMu.Lock()
	instance.analysisWorkers = 1
	instance.analysisMu.Unlock()
	instance.publishSyntax(work, file, oldSnapshot.identity)
	select {
	case params := <-published:
		t.Fatalf("oversized transition published stale diagnostics: %#v", params)
	default:
	}
	instance.analysisMu.Lock()
	_, queued := instance.analysisPending[documentURI.String()]
	instance.analysisMu.Unlock()
	if !queued {
		t.Fatal("oversized transition did not requeue current document")
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
	if len(params.Diagnostics) != 1 || params.Diagnostics[0].Code != protocol.String("vim/E171") {
		t.Fatalf("non-file diagnostics = %#v", params.Diagnostics)
	}
}

func TestOpenDocumentConsumersPreservePureParserCache(t *testing.T) {
	root := t.TempDir()
	targetPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport var Value = 1\nif true\n")
	mainPath := writeWorkspaceFile(t, root, "main.vim", "vim9script\nimport './lib.vim' as Lib\necho Lib.Value\nif true\n")
	targetCanonical := mustWorkspaceCanonicalPath(t, targetPath)
	mainCanonical := mustWorkspaceCanonicalPath(t, mainPath)
	instance, _ := initializeWorkspaceDiagnosticServer(t, root)

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
	if _, _, err := instance.openWorkspaceReferenceLocationsInState(context.Background(), instance.captureWorkspaceNavigationState(), target, text.UTF16); err != nil {
		t.Fatal(err)
	}
	assertCached("rename open references", mainSnapshot, mainFile, mainDiagnostics)

	items, _ := instance.importMemberCompletionsInState(mainSnapshot.URI(), mainFile, "Lib", instance.captureWorkspaceNavigationState())
	if len(items) != 1 || items[0].Label != "Value" {
		t.Fatalf("open import completion = %#v", items)
	}
	assertCached("completion open import target", targetSnapshot, targetFile, targetDiagnostics)
	if parsed := instance.parseImportTarget(targetCanonical, targetSource); parsed != targetFile {
		t.Fatalf("open target parser cache = %p, want %p", parsed, targetFile)
	}

	instance.workspaceMu.Lock()
	workspaceSnapshot := instance.workspaceAnalysisSnapshotLocked(mainCanonical, mainFile)
	instance.workspaceMu.Unlock()
	_ = instance.workspaceImportDiagnostics(workspaceSnapshot, mainFile, analysis.Analyze(mainFile))
	if !workspaceSnapshot.ready {
		t.Fatal("workspace import diagnostics were not ready")
	}
	assertCached("import diagnostics open target", targetSnapshot, targetFile, targetDiagnostics)
}

func TestWorkspaceConfigurationRequestOnInitializedAndNullChange(t *testing.T) {
	client := &configurationClient{settings: protocol.LSPAny([]byte(`{"workspace":{"rebuildDebounce":250}}`))}
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
	instance.workspaceMu.Lock()
	delay := instance.workspaceDelay
	instance.workspaceMu.Unlock()
	if client.calls != 1 || delay != 250*time.Millisecond {
		t.Fatalf("initialized configuration calls=%d delay=%s", client.calls, delay)
	}
	client.settings = protocol.LSPAny([]byte(`{"workspace":{"rebuildDebounce":0}}`))
	if err := instance.DidChangeConfiguration(context.Background(), &protocol.DidChangeConfigurationParams{Settings: protocol.LSPAny([]byte("null"))}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceMu.Lock()
	delay = instance.workspaceDelay
	instance.workspaceMu.Unlock()
	if client.calls != 2 || delay != 0 {
		t.Fatalf("changed configuration calls=%d delay=%s", client.calls, delay)
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
	relatedInformation := true
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI: &rootURI, InitializationOptions: protocol.LSPAny([]byte(`{"runtimepath":[]}`)),
		Capabilities: protocol.ClientCapabilities{TextDocument: &protocol.TextDocumentClientCapabilities{
			PublishDiagnostics: &protocol.PublishDiagnosticsClientCapabilities{DiagnosticsCapabilities: protocol.DiagnosticsCapabilities{RelatedInformation: &relatedInformation}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := instance.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	instance.workspaceWG.Wait()
	return instance, published
}

func TestDocumentPullDiagnosticsFullAndUnchanged(t *testing.T) {
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	result, err := instance.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: protocol.ClientCapabilities{
		TextDocument: &protocol.TextDocumentClientCapabilities{Diagnostic: &protocol.DiagnosticClientCapabilities{}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Capabilities.DiagnosticProvider.(*protocol.DiagnosticOptions); !ok {
		t.Fatalf("diagnostic provider = %#v", result.Capabilities.DiagnosticProvider)
	}
	documentURI := uri.URI("file:///pull.vim")
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: "vim9script\nvar value = 1\n"}}); err != nil {
		t.Fatal(err)
	}
	report, err := instance.Diagnostic(context.Background(), &protocol.DocumentDiagnosticParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil {
		t.Fatal(err)
	}
	full, ok := report.(*protocol.RelatedFullDocumentDiagnosticReport)
	if !ok || full.ResultID == nil {
		t.Fatalf("full report = %#v", report)
	}
	report, err = instance.Diagnostic(context.Background(), &protocol.DocumentDiagnosticParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, PreviousResultID: full.ResultID})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := report.(*protocol.RelatedUnchangedDocumentDiagnosticReport); !ok {
		t.Fatalf("unchanged report = %#v", report)
	}
}

func TestDocumentPullDiagnosticsTransportCacheAndConfiguration(t *testing.T) {
	published := make(chan *protocol.PublishDiagnosticsParams, 2)
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	instance.client = &diagnosticClient{published: published}
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: protocol.ClientCapabilities{TextDocument: &protocol.TextDocumentClientCapabilities{Diagnostic: &protocol.DiagnosticClientCapabilities{}}}}); err != nil {
		t.Fatal(err)
	}
	analysisDone := installAnalysisFinishedHook(instance)
	documentURI := uri.URI("file:///pull-cache.vim")
	open := func(version int32) {
		t.Helper()
		if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: version, Text: "vim9script\necho missing\n"}}); err != nil {
			t.Fatal(err)
		}
	}
	pull := func(previous *string) *protocol.RelatedFullDocumentDiagnosticReport {
		t.Helper()
		report, err := instance.Diagnostic(context.Background(), &protocol.DocumentDiagnosticParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, PreviousResultID: previous})
		if err != nil {
			t.Fatal(err)
		}
		full, ok := report.(*protocol.RelatedFullDocumentDiagnosticReport)
		if !ok || full.ResultID == nil {
			t.Fatalf("report = %#v", report)
		}
		return full
	}
	open(1)
	first := pull(nil)
	if len(first.Items) == 0 {
		t.Fatal("expected unresolved-name diagnostic")
	}
	wrong := "not-ours"
	if full := pull(&wrong); full.ResultID == nil || *full.ResultID != *first.ResultID {
		t.Fatalf("wrong-id full = %#v, want cached id %q", full, *first.ResultID)
	}
	select {
	case <-analysisDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for background pull analysis")
	}
	select {
	case params := <-published:
		t.Fatalf("pull client published diagnostics: %#v", params)
	default:
	}
	if err := instance.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}}); err != nil {
		t.Fatal(err)
	}
	open(2)
	second := pull(first.ResultID)
	if *second.ResultID == *first.ResultID {
		t.Fatalf("reopen reused result id %q", *first.ResultID)
	}
	if err := instance.DidChangeConfiguration(context.Background(), &protocol.DidChangeConfigurationParams{Settings: protocol.LSPAny([]byte(`{"diagnostic":{"disabled":["vim/E121"]}}`))}); err != nil {
		t.Fatal(err)
	}
	third := pull(second.ResultID)
	if *third.ResultID == *second.ResultID || len(third.Items) != 0 {
		t.Fatalf("disabled configuration report = %#v", third)
	}
	if !implementedMethod(protocol.MethodTextDocumentDiagnostic) || !implementedMethod(protocol.MethodWorkspaceDiagnostic) {
		t.Fatal("diagnostic dispatch allowlist is incorrect")
	}
}

func TestDocumentPullDiagnosticRefreshCoalesces(t *testing.T) {
	value := true
	t.Run("unsupported", func(t *testing.T) {
		instance := New(nil, nil, io.Discard)
		t.Cleanup(instance.stopAnalysis)
		client := &refreshDiagnosticClient{calls: make(chan struct{}, 1), release: make(chan struct{}, 1)}
		instance.client = client
		if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: protocol.ClientCapabilities{
			TextDocument: &protocol.TextDocumentClientCapabilities{Diagnostic: &protocol.DiagnosticClientCapabilities{}},
		}}); err != nil {
			t.Fatal(err)
		}
		if err := instance.DidChangeConfiguration(context.Background(), &protocol.DidChangeConfigurationParams{Settings: protocol.LSPAny([]byte(`{"diagnostic":{"disabled":["vim/E121"]}}`))}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-client.calls:
			t.Fatal("client without refreshSupport received diagnostic refresh")
		default:
		}
	})

	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := &refreshDiagnosticClient{calls: make(chan struct{}, 2), release: make(chan struct{}, 2)}
	instance.client = client
	if _, err := instance.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: protocol.ClientCapabilities{
		Workspace:    &protocol.WorkspaceClientCapabilities{Diagnostics: &protocol.DiagnosticWorkspaceClientCapabilities{RefreshSupport: &value}},
		TextDocument: &protocol.TextDocumentClientCapabilities{Diagnostic: &protocol.DiagnosticClientCapabilities{}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := instance.DidChangeConfiguration(context.Background(), &protocol.DidChangeConfigurationParams{Settings: protocol.LSPAny([]byte(`{"diagnostic":{"disabled":["vim/E121"]}}`))}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.calls:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for refresh")
	}
	if err := instance.DidChangeConfiguration(context.Background(), &protocol.DidChangeConfigurationParams{Settings: protocol.LSPAny([]byte(`{"diagnostic":{"disabled":["vim/E117"]}}`))}); err != nil {
		t.Fatal(err)
	}
	client.release <- struct{}{}
	select {
	case <-client.calls:
	case <-time.After(10 * time.Second):
		t.Fatal("coalesced refresh was lost")
	}
	client.release <- struct{}{}
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

func openDiagnosticsServer(t *testing.T) (*Server, *diagnosticClient) {
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 10)}
	instance.mu.Lock()
	instance.client = client
	instance.mu.Unlock()
	return instance, client
}

func installAnalysisFinishedHook(instance *Server) <-chan string {
	analysisDone := make(chan string, 10)
	instance.analysisMu.Lock()
	instance.testHooks.afterAnalysisFinished = func(uri string) {
		analysisDone <- uri
	}
	instance.analysisMu.Unlock()
	return analysisDone
}

func TestPushDiagnosticsDeduplicationAndResendOnEdit(t *testing.T) {
	instance, client := openDiagnosticsServer(t)
	documentURI := uri.MustParse("file:///test-dedup.vim")
	source := "vim9script\necho unknownVar\n"

	analysisDone := installAnalysisFinishedHook(instance)

	// 1. Open document at version 1 (has 1 diagnostic)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source},
	}); err != nil {
		t.Fatal(err)
	}

	first := waitForDiagnostics(t, client.published)
	if len(first.Diagnostics) != 1 {
		t.Fatalf("first diagnostics = %#v", first)
	}
	if v, ok := first.Version.Get(); !ok || v != 1 {
		t.Fatalf("first version = %v, want 1", v)
	}
	select {
	case uri := <-analysisDone:
		if uri != documentURI.String() {
			t.Fatalf("step 1: unexpected uri %s", uri)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("step 1: timed out waiting for analysisDone")
	}

	// 2. Pure repeated analysis on unchanged snapshot must NOT publish duplicate
	instance.startAnalysis(documentURI.String())
	select {
	case uri := <-analysisDone:
		if uri != documentURI.String() {
			t.Fatalf("step 2: unexpected uri %s", uri)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for repeated analysis")
	}
	select {
	case duplicate := <-client.published:
		t.Fatalf("unexpected duplicate publication for identical snapshot: %#v", duplicate)
	default:
	}

	// 3. Edit to version 2 (same diagnostic content, but must publish due to new version)
	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{Text: source}},
	}); err != nil {
		t.Fatal(err)
	}

	second := waitForDiagnostics(t, client.published)
	if len(second.Diagnostics) != 1 {
		t.Fatalf("second diagnostics = %#v", second)
	}
	if v, ok := second.Version.Get(); !ok || v != 2 {
		t.Fatalf("second version = %v, want 2", v)
	}
	select {
	case uri := <-analysisDone:
		if uri != documentURI.String() {
			t.Fatalf("step 3: unexpected uri %s", uri)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("step 3: timed out waiting for analysisDone")
	}

	// 4. Edit to version 3 fixing the error (transition from non-empty to empty)
	cleanSource := "vim9script\nvar unknownVar = 42\necho unknownVar\n"
	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 3},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{Text: cleanSource}},
	}); err != nil {
		t.Fatal(err)
	}

	third := waitForDiagnostics(t, client.published)
	if len(third.Diagnostics) != 0 {
		t.Fatalf("third diagnostics should be empty to clear, got %#v", third)
	}
	if v, ok := third.Version.Get(); !ok || v != 3 {
		t.Fatalf("third version = %v, want 3", v)
	}
	select {
	case uri := <-analysisDone:
		if uri != documentURI.String() {
			t.Fatalf("step 4: unexpected uri %s", uri)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("step 4: timed out waiting for analysisDone")
	}

	// 5. Subsequent repeated analysis on clean snapshot must NOT publish
	instance.startAnalysis(documentURI.String())
	select {
	case uri := <-analysisDone:
		if uri != documentURI.String() {
			t.Fatalf("step 5: unexpected uri %s", uri)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for clean repeated analysis")
	}
	select {
	case duplicate := <-client.published:
		t.Fatalf("unexpected duplicate publication for clean snapshot: %#v", duplicate)
	default:
	}
}

func TestPushDiagnosticsHashChangesOnConfiguration(t *testing.T) {
	instance, client := openDiagnosticsServer(t)
	documentURI := uri.MustParse("file:///test-config-hash.vim")
	source := "vim9script\necho unknownVar\n"

	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source},
	}); err != nil {
		t.Fatal(err)
	}

	first := waitForDiagnostics(t, client.published)
	if len(first.Diagnostics) != 1 {
		t.Fatalf("first diagnostics = %#v", first)
	}
	initialSeverity := first.Diagnostics[0].Severity

	// Override severity in configuration
	if err := instance.applyWorkspaceConfiguration(context.Background(), []byte(`{"diagnostic":{"override":{"vim/E121":"information"}}}`)); err != nil {
		t.Fatal(err)
	}

	updated := waitForDiagnostics(t, client.published)
	if len(updated.Diagnostics) != 1 {
		t.Fatalf("updated diagnostics = %#v", updated)
	}
	if updated.Diagnostics[0].Severity == initialSeverity {
		t.Fatalf("expected severity to change, got %v", updated.Diagnostics[0].Severity)
	}
}

func TestPushDiagnosticsRetryOnFailureForNonEmptyDiagnostics(t *testing.T) {
	instance, client := openDiagnosticsServer(t)
	documentURI := uri.MustParse("file:///test-retry-non-empty.vim")
	source := "vim9script\necho unknownVar\n"

	analysisDone := installAnalysisFinishedHook(instance)

	var attempts atomic.Int32
	client.publishHook = func(params *protocol.PublishDiagnosticsParams) error {
		if attempts.Add(1) == 1 {
			return errors.New("temporary network error")
		}
		return nil
	}

	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source},
	}); err != nil {
		t.Fatal(err)
	}

	// Wait for the first attempt to finish and fail.
	select {
	case <-analysisDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first attempt")
	}

	if attempts.Load() != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts.Load())
	}

	// Trigger analysis again for the same snapshot.
	instance.startAnalysis(documentURI.String())

	// Second attempt should succeed and publish the diagnostics.
	params := waitForDiagnostics(t, client.published)
	if len(params.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %#v", params.Diagnostics)
	}
	select {
	case <-analysisDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for second attempt to finish committing")
	}
	if attempts.Load() != 2 {
		t.Fatalf("expected 2 publish attempts, got %d", attempts.Load())
	}
}

func TestPushDiagnosticsRetryOnFailureForClearingDiagnostics(t *testing.T) {
	instance, client := openDiagnosticsServer(t)
	documentURI := uri.MustParse("file:///test-retry-clear.vim")
	source := "vim9script\necho unknownVar\n"

	analysisDone := installAnalysisFinishedHook(instance)

	// 1. Initial publication succeeds.
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source},
	}); err != nil {
		t.Fatal(err)
	}
	first := waitForDiagnostics(t, client.published)
	if len(first.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %#v", first)
	}
	// Drain analysisDone from DidOpen
	<-analysisDone

	// 2. Clear diagnostics fails on first attempt.
	var clearAttempts atomic.Int32
	client.publishHook = func(params *protocol.PublishDiagnosticsParams) error {
		if len(params.Diagnostics) == 0 && clearAttempts.Add(1) == 1 {
			return errors.New("failed to clear diagnostics")
		}
		return nil
	}

	cleanSource := "vim9script\nvar unknownVar = 42\necho unknownVar\n"
	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{Text: cleanSource}},
	}); err != nil {
		t.Fatal(err)
	}

	// Wait for the clear attempt to finish and fail.
	select {
	case <-analysisDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for failed clear attempt")
	}

	if clearAttempts.Load() != 1 {
		t.Fatalf("expected 1 clear attempt, got %d", clearAttempts.Load())
	}

	// 3. Trigger re-analysis of the clean snapshot.
	instance.startAnalysis(documentURI.String())

	// 4. Second attempt to clear succeeds.
	cleared := waitForDiagnostics(t, client.published)
	if len(cleared.Diagnostics) != 0 {
		t.Fatalf("expected 0 diagnostics to clear, got %#v", cleared)
	}
	select {
	case <-analysisDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for second clear attempt to finish committing")
	}
	if clearAttempts.Load() != 2 {
		t.Fatalf("expected 2 clear attempts, got %d", clearAttempts.Load())
	}
}

func TestPushDiagnosticsClientNilDoesNotCommitPublishedState(t *testing.T) {
	instance, client := openDiagnosticsServer(t)
	documentURI := uri.MustParse("file:///test-client-nil.vim")
	source := "vim9script\necho unknownVar\n"

	analysisDone := installAnalysisFinishedHook(instance)

	instance.mu.Lock()
	instance.client = nil
	instance.mu.Unlock()

	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source},
	}); err != nil {
		t.Fatal(err)
	}

	// Wait for first analysis to complete under client == nil
	select {
	case <-analysisDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for analysis with nil client")
	}

	instance.publishMu.Lock()
	st := instance.published[documentURI.String()]
	instance.publishMu.Unlock()
	if st.hasDiagnostics {
		t.Fatal("expected hasDiagnostics to be false when client is nil")
	}

	// Restore client and trigger analysis.
	instance.mu.Lock()
	instance.client = client
	instance.mu.Unlock()

	instance.startAnalysis(documentURI.String())
	select {
	case <-analysisDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for analysis after restoring client")
	}

	params := waitForDiagnostics(t, client.published)
	if len(params.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %#v", params.Diagnostics)
	}
}

func TestPushDiagnosticsStaleSendDoesNotClearNewerPendingState(t *testing.T) {
	instance, client := openDiagnosticsServer(t)
	documentURI := uri.MustParse("file:///test-stale-send.vim")
	source := "vim9script\necho unknownVar\n"

	analysisDone := installAnalysisFinishedHook(instance)

	v1Started := make(chan struct{})
	v1Release := make(chan struct{})

	client.publishHook = func(params *protocol.PublishDiagnosticsParams) error {
		select {
		case <-v1Started:
		default:
			close(v1Started)
			<-v1Release
		}
		return nil
	}

	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source},
	}); err != nil {
		t.Fatal(err)
	}

	// Wait until v1 is in the middle of PublishDiagnostics
	select {
	case <-v1Started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for v1 to start publishing")
	}

	// DidSave arrives on the same snapshot/hash, setting mustPublish and incrementing publishSeq
	if err := instance.DidSave(context.Background(), &protocol.DidSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
	}); err != nil {
		t.Fatal(err)
	}

	// Unblock v1
	close(v1Release)

	// First publication received
	first := waitForDiagnostics(t, client.published)
	if len(first.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic on first publication, got %#v", first.Diagnostics)
	}
	<-analysisDone

	// Trigger re-analysis: because mustPublish was preserved despite identical hash/version,
	// it must publish again
	instance.startAnalysis(documentURI.String())
	select {
	case <-analysisDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for re-analysis")
	}

	second := waitForDiagnostics(t, client.published)
	if len(second.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic on second publication, got %#v", second.Diagnostics)
	}

	// Subsequent analysis on clean snapshot must NOT publish again (deduplication works)
	instance.startAnalysis(documentURI.String())
	select {
	case <-analysisDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for clean repeated analysis")
	}
	select {
	case dup := <-client.published:
		t.Fatalf("unexpected duplicate publication: %#v", dup)
	default:
	}
}

func TestPushDiagnosticsEditDuringSendStillClearsStaleDiagnostics(t *testing.T) {
	instance, client := openDiagnosticsServer(t)
	documentURI := uri.MustParse("file:///test-edit-during-send.vim")
	analysisDone := installAnalysisFinishedHook(instance)
	sendStarted := make(chan struct{})
	releaseSend := make(chan struct{})
	client.publishHook = func(params *protocol.PublishDiagnosticsParams) error {
		if len(params.Diagnostics) > 0 {
			select {
			case <-sendStarted:
			default:
				close(sendStarted)
				<-releaseSend
			}
		}
		return nil
	}

	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI: documentURI, Version: 1,
			Text: "vim9script\nvar value: number = 'error'\n",
		},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sendStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for initial diagnostics send")
	}

	if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{
			Text: "vim9script\nvar value: number = 42\necho value\n",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	close(releaseSend)

	first := waitForDiagnostics(t, client.published)
	if len(first.Diagnostics) == 0 {
		t.Fatal("initial diagnostics unexpectedly empty")
	}
	for step := range 2 {
		select {
		case <-analysisDone:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for analysis %d", step+1)
		}
	}
	select {
	case cleared := <-client.published:
		if version, ok := cleared.Version.Get(); !ok || version != 2 || len(cleared.Diagnostics) != 0 {
			t.Fatalf("cleared diagnostics = %#v", cleared)
		}
	default:
		t.Fatal("current clean snapshot did not clear stale diagnostics")
	}
}

func TestPushDiagnosticsCloseWhileFirstNonEmptyPublishBlocked(t *testing.T) {
	instance, client := openDiagnosticsServer(t)
	documentURI := uri.MustParse("file:///test-close-blocked.vim")
	source := "vim9script\necho unknownVar\n"

	v1Started := make(chan struct{})
	v1Release := make(chan struct{})

	client.publishHook = func(params *protocol.PublishDiagnosticsParams) error {
		if len(params.Diagnostics) > 0 {
			select {
			case <-v1Started:
			default:
				close(v1Started)
				<-v1Release
			}
		}
		return nil
	}

	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source},
	}); err != nil {
		t.Fatal(err)
	}

	// Wait until v1 non-empty publish has started and is blocked in publishHook
	select {
	case <-v1Started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for non-empty publish to start")
	}

	// Close the document while v1 is blocked
	closeDone := make(chan error, 1)
	go func() {
		closeDone <- instance.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		})
	}()

	// Unblock v1 publish
	close(v1Release)

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for DidClose")
	}

	// First publication should be the non-empty one (v1)
	first := waitForDiagnostics(t, client.published)
	if len(first.Diagnostics) == 0 {
		t.Fatalf("expected first publication to have diagnostics, got empty")
	}

	// Second (last) publication MUST be empty diagnostics
	last := waitForDiagnostics(t, client.published)
	if len(last.Diagnostics) != 0 {
		t.Fatalf("expected final publication after close to be empty, got %#v", last.Diagnostics)
	}
}
