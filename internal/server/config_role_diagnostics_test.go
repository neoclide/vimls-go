package server

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// configRoleTestSource is deliberately identical for the config-file and the
// plugin file so the two tests differ only in the file role.
const configRoleTestSource = `let g:plug_config = 1
let g:state = {}
map <leader>x :echo 'x'<CR>
set tabstop=2
autocmd FileType python set sw=4
`

func publishedCodes(params *protocol.PublishDiagnosticsParams) map[string]int {
	codes := make(map[string]int)
	for _, diagnostic := range params.Diagnostics {
		if code, ok := diagnostic.Code.(protocol.String); ok {
			codes[string(code)]++
		}
	}
	return codes
}

// TestConfigFileRolePublishesConfigDiagnosticPolicy opens identical content
// once as a user configuration file (.vimrc) and once as a plugin file and
// verifies the §4.1 policy matrix end to end through the published LSP
// diagnostics.
func newRootedServer(t *testing.T, root string) *Server {
	t.Helper()
	instance := New(nil, nil, io.Discard)
	t.Cleanup(instance.stopAnalysis)
	canonical := mustWorkspaceCanonicalPath(t, root)
	index, graph, files, warnings := instance.buildWorkspaceIndex(context.Background(), []string{canonical}, []string{canonical}, workspacePathResolver(nil, []string{canonical}), nil)
	if len(warnings) != 0 {
		t.Fatalf("workspace index warnings = %#v", warnings)
	}
	instance.workspaceMu.Lock()
	instance.workspaceIndex = index
	instance.workspaceGraph = graph
	instance.workspaceGraphView = graph.Snapshot()
	instance.workspaceFiles = files
	instance.workspaceBuilt = true
	instance.workspaceRoots = []string{canonical}
	instance.workspaceMu.Unlock()
	return instance
}

func TestConfigFileRolePublishesConfigDiagnosticPolicy(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pluginPath := filepath.Join(root, "plugin", "demo.vim")
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginPath, []byte(configRoleTestSource), 0o644); err != nil {
		t.Fatal(err)
	}
	vimrcPath := filepath.Join(root, ".vimrc")
	if err := os.WriteFile(vimrcPath, []byte(configRoleTestSource), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name     string
		document string
		absent   []string
		present  []string
	}{
		{
			name:     "config file",
			document: uri.File(vimrcPath).String(),
			absent: []string{
				"vimls/configuration-overwrite",
				"vimls/global-internal-state",
				"vimls/direct-user-keymap",
				"vimls/mapping-without-unique",
			},
			present: []string{"vimls/recursive-map", "vimls/set-vs-setlocal", "vimls/autocmd-outside-augroup"},
		},
		{
			name:     "plugin file",
			document: uri.File(pluginPath).String(),
			absent:   nil,
			present: []string{
				"vimls/configuration-overwrite",
				"vimls/global-internal-state",
				"vimls/direct-user-keymap",
				"vimls/mapping-without-unique",
				"vimls/recursive-map",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance := newRootedServer(t, root)
			client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
			instance.client = client
			documentURI := uri.MustParse(test.document)
			if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
				URI: documentURI, Version: 1, Text: configRoleTestSource,
			}}); err != nil {
				t.Fatal(err)
			}
			params := waitForDiagnostics(t, client.published)
			codes := publishedCodes(params)
			for _, code := range test.absent {
				if codes[code] != 0 {
					t.Fatalf("unexpected diagnostic %s in %s: %#v", code, test.name, codes)
				}
			}
			for _, code := range test.present {
				if codes[code] == 0 {
					t.Fatalf("missing diagnostic %s in %s: %#v", code, test.name, codes)
				}
			}
		})
	}
}

// TestConfigFileRoleSetVsSetlocalOnlyInAutocmd verifies that a top-level :set
// is not reported for configuration files while the FileType-autocmd :set is,
// and that the plugin file reports both.
func TestConfigFileRoleSetVsSetlocalOnlyInAutocmd(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := "set tabstop=2\nautocmd FileType python set sw=4\n"
	pluginPath := filepath.Join(root, "plugin", "demo.vim")
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	vimrcPath := filepath.Join(root, ".vimrc")
	if err := os.WriteFile(vimrcPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	openAndWait := func(t *testing.T, path string) map[string]int {
		t.Helper()
		instance := newRootedServer(t, root)
		client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
		instance.client = client
		documentURI := uri.MustParse(path)
		if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
			URI: documentURI, Version: 1, Text: source,
		}}); err != nil {
			t.Fatal(err)
		}
		return publishedCodes(waitForDiagnostics(t, client.published))
	}

	pluginCodes := openAndWait(t, uri.File(pluginPath).String())
	if pluginCodes["vimls/set-vs-setlocal"] != 2 {
		t.Fatalf("plugin set-vs-setlocal count = %d, want 2: %#v", pluginCodes["vimls/set-vs-setlocal"], pluginCodes)
	}
	configCodes := openAndWait(t, uri.File(vimrcPath).String())
	if configCodes["vimls/set-vs-setlocal"] != 1 {
		t.Fatalf("config set-vs-setlocal count = %d, want 1 (autocmd body only): %#v", configCodes["vimls/set-vs-setlocal"], configCodes)
	}
}

// TestConfigFileRoleRecursiveMapSeverityHint verifies the severity of
// vimls/recursive-map is hint for a configuration file and warning for a
// plugin file through the protocol conversion.
func TestConfigFileRoleRecursiveMapSeverityHint(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := "map <leader>x :echo 'x'<CR>\n"
	pluginPath := filepath.Join(root, "plugin", "demo.vim")
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	vimrcPath := filepath.Join(root, ".vimrc")
	if err := os.WriteFile(vimrcPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	severityFor := func(t *testing.T, path string) protocol.DiagnosticSeverity {
		t.Helper()
		instance := newRootedServer(t, root)
		client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
		instance.client = client
		documentURI := uri.MustParse(path)
		if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
			URI: documentURI, Version: 1, Text: source,
		}}); err != nil {
			t.Fatal(err)
		}
		params := waitForDiagnostics(t, client.published)
		for _, diagnostic := range params.Diagnostics {
			if code, ok := diagnostic.Code.(protocol.String); ok && string(code) == "vimls/recursive-map" {
				return diagnostic.Severity
			}
		}
		t.Fatalf("no recursive-map diagnostic published: %#v", publishedCodes(params))
		return 0
	}

	if got := severityFor(t, uri.File(pluginPath).String()); got != protocol.DiagnosticSeverityWarning {
		t.Fatalf("plugin recursive-map severity = %v, want warning", got)
	}
	if got := severityFor(t, uri.File(vimrcPath).String()); got != protocol.DiagnosticSeverityHint {
		t.Fatalf("config recursive-map severity = %v, want hint", got)
	}
}

// TestConfigFileRoleMapleaderOrderPublishesOnlyForConfigFiles verifies §5.2
// end to end: vimls/config-mapleader-order is published for a .vimrc whose
// mapping precedes the leader assignment, and never for the plugin role.
func TestConfigFileRoleMapleaderOrderPublishesOnlyForConfigFiles(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := "map <Leader>a :echo 1<CR>\nlet g:mapleader = ','\n"
	pluginPath := filepath.Join(root, "plugin", "demo.vim")
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	vimrcPath := filepath.Join(root, ".vimrc")
	if err := os.WriteFile(vimrcPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	openAndWait := func(t *testing.T, path string) map[string]int {
		t.Helper()
		instance := newRootedServer(t, root)
		client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
		instance.client = client
		documentURI := uri.MustParse(path)
		if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
			URI: documentURI, Version: 1, Text: source,
		}}); err != nil {
			t.Fatal(err)
		}
		return publishedCodes(waitForDiagnostics(t, client.published))
	}

	code := "vimls/config-mapleader-order"
	if codes := openAndWait(t, uri.File(pluginPath).String()); codes[code] != 0 {
		t.Fatalf("plugin file reported %s: %#v", code, codes)
	}
	if codes := openAndWait(t, uri.File(vimrcPath).String()); codes[code] != 1 {
		t.Fatalf("config file reported %s %d times, want 1: %#v", code, codes[code], codes)
	}
}

// TestConfigFileRolesUseServerPathClassification exercises the three §10 file
// roles through DidOpen.  In particular, an explicit configFiles entry must be
// an absolute path and takes precedence over the plugin/runtime defaults.
func TestConfigFileRolesUseServerPathClassification(t *testing.T) {
	root := mustWorkspaceCanonicalPath(t, t.TempDir())
	runtimeRoot := mustWorkspaceCanonicalPath(t, t.TempDir())
	source := "map <Leader>a :echo 1<CR>\nlet g:mapleader = ','\n"
	explicitPath := filepath.Join(root, "plugin", "configured.vim")
	paths := []string{
		filepath.Join(root, ".vimrc"),
		explicitPath,
		filepath.Join(runtimeRoot, "plugin", "runtime.vim"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, test := range []struct {
		name, path string
		wantConfig bool
	}{
		{name: "standard vimrc", path: paths[0], wantConfig: true},
		{name: "explicit absolute configFiles plugin", path: explicitPath, wantConfig: true},
		{name: "runtime plugin", path: paths[2], wantConfig: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance := newRootedServer(t, root)
			instance.mu.Lock()
			instance.configFiles = []string{explicitPath}
			instance.mu.Unlock()
			instance.workspaceMu.Lock()
			instance.runtimePaths = []string{runtimeRoot}
			instance.workspaceMu.Unlock()
			if got := instance.IsConfigFile(test.path); got != test.wantConfig {
				t.Fatalf("IsConfigFile(%q) = %v, want %v", test.path, got, test.wantConfig)
			}
			client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
			instance.client = client
			documentURI := uri.File(test.path)
			if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source}}); err != nil {
				t.Fatal(err)
			}
			got := publishedCodes(waitForDiagnostics(t, client.published))["vimls/config-mapleader-order"]
			if test.wantConfig && got != 1 || !test.wantConfig && got != 0 {
				t.Fatalf("mapleader diagnostics = %d, want config role %v", got, test.wantConfig)
			}
		})
	}
}

// TestConfigDiagnosticsPreserveUnicodeCRLFPositions verifies that the
// configuration-only duplicate-mapping diagnostic is converted at the LSP
// boundary, not by byte offset.  The second LHS follows a CRLF and contains a
// non-ASCII rune, so UTF-8 and UTF-16 necessarily have distinct end columns.
func TestConfigDiagnosticsPreserveUnicodeCRLFPositions(t *testing.T) {
	source := "nnoremap 你 :echo 1<CR>\r\nnnoremap 你 :echo 2<CR>\r\n"
	for _, test := range []struct {
		name     string
		encoding text.Encoding
		want     protocol.Range
	}{
		{name: "utf8", encoding: text.UTF8, want: navigationRange(1, 9, 12)},
		{name: "utf16", encoding: text.UTF16, want: navigationRange(1, 9, 10)},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := mustWorkspaceCanonicalPath(t, t.TempDir())
			path := filepath.Join(root, ".vimrc")
			if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}
			instance := newRootedServer(t, root)
			instance.encoding = test.encoding
			client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 2)}
			instance.client = client
			documentURI := uri.File(path)
			if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source}}); err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range waitForDiagnostics(t, client.published).Diagnostics {
				if diagnostic.Code == protocol.String("vimls/duplicate-mapping") {
					if diagnostic.Range != test.want {
						t.Fatalf("duplicate mapping range = %#v, want %#v", diagnostic.Range, test.want)
					}
					return
				}
			}
			t.Fatal("missing duplicate-mapping diagnostic")
		})
	}
}

// TestConfigDiagnosticSettingsApplyToConfigOnlyRules verifies that the normal
// disabled/override settings pipeline also controls diagnostics introduced for
// configuration files, rather than treating them as an unconfigurable side
// channel.
func TestConfigDiagnosticSettingsApplyToConfigOnlyRules(t *testing.T) {
	root := mustWorkspaceCanonicalPath(t, t.TempDir())
	path := filepath.Join(root, ".vimrc")
	source := "map <Leader>a :echo 1<CR>\nlet g:mapleader = ','\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	instance := newRootedServer(t, root)
	client := &diagnosticClient{published: make(chan *protocol.PublishDiagnosticsParams, 4)}
	instance.client = client
	documentURI := uri.File(path)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: documentURI, Version: 1, Text: source}}); err != nil {
		t.Fatal(err)
	}
	code := protocol.String("vimls/config-mapleader-order")
	find := func(params *protocol.PublishDiagnosticsParams) *protocol.Diagnostic {
		for index := range params.Diagnostics {
			if params.Diagnostics[index].Code == code {
				return &params.Diagnostics[index]
			}
		}
		return nil
	}
	if diagnostic := find(waitForDiagnostics(t, client.published)); diagnostic == nil || diagnostic.Severity != protocol.DiagnosticSeverityWarning {
		t.Fatalf("initial config diagnostic = %#v", diagnostic)
	}
	if err := instance.applyWorkspaceConfiguration(context.Background(), []byte(`{"diagnostic":{"disabled":["vimls/config-mapleader-order"]}}`)); err != nil {
		t.Fatal(err)
	}
	if diagnostic := find(waitForDiagnostics(t, client.published)); diagnostic != nil {
		t.Fatalf("disabled config diagnostic remained published: %#v", diagnostic)
	}
	if err := instance.applyWorkspaceConfiguration(context.Background(), []byte(`{"diagnostic":{"disabled":[],"override":{"vimls/config-mapleader-order":"information"}}}`)); err != nil {
		t.Fatal(err)
	}
	if diagnostic := find(waitForDiagnostics(t, client.published)); diagnostic == nil || diagnostic.Severity != protocol.DiagnosticSeverityInformation {
		t.Fatalf("overridden config diagnostic = %#v", diagnostic)
	}
}

// TestProtocolDiagnosticsSameFileRelatedInformation verifies that a related
// diagnostic without an explicit URI is published against the current document
// (used by the config-mode augroup report).
func TestProtocolDiagnosticsSameFileRelatedInformation(t *testing.T) {
	documentURI := "file:///rel.vim"
	source := "augroup g\n  autocmd BufRead * echomsg 'x'\naugroup END\n"
	snapshot := text.NewSnapshot(documentURI, 1, nil, source)
	file := &syntax.File{Source: source, Diagnostics: []syntax.Diagnostic{{
		Code:    "vimls/autocmd-group-not-cleared",
		Message: "augroup does not clear existing autocommands",
		Span:    syntax.Span{Start: 8, End: 9},
		Related: syntax.RelatedDiagnostic{Message: "autocommand is registered again every time this configuration is sourced", Span: syntax.Span{Start: 23, End: 34}},
	}}}
	items := protocolDiagnostics(snapshot, file, text.UTF8, true, nil)
	if len(items) != 1 || len(items[0].RelatedInformation) != 1 {
		t.Fatalf("related diagnostics = %#v", items)
	}
	related := items[0].RelatedInformation[0]
	if related.Location.URI != uri.URI(documentURI) {
		t.Fatalf("related URI = %v, want %v", related.Location.URI, documentURI)
	}
	if related.Location.Range.Start.Line != 1 || related.Message == "" {
		t.Fatalf("related location = %#v message = %q", related.Location, related.Message)
	}
}
