package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func runtimeHelpHover(t *testing.T, s *Server, documentURI uri.URI, needle string) *protocol.Hover {
	t.Helper()
	snapshot, _ := s.documents.Snapshot(documentURI.String())
	offset := strings.LastIndex(snapshot.Text(), needle)
	if offset < 0 {
		t.Fatalf("missing hover needle %q", needle)
	}
	position, err := snapshot.Position(offset, s.encoding)
	if err != nil {
		t.Fatal(err)
	}
	hover, err := s.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: uint32(position.Line), Character: uint32(position.Character)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return hover
}

func TestRuntimeHelpEmptyRuntimepathDoesNotStartWorker(t *testing.T) {
	s := New(nil, nil, io.Discard)
	t.Cleanup(s.stopAnalysis)
	read := make(chan struct{}, 1)
	s.testHooks.beforeRuntimeHelpRead = func(context.Context, string) {
		read <- struct{}{}
	}
	s.setRuntimePaths(nil)
	s.runtimeHelpWG.Wait()
	s.workspaceMu.Lock()
	running := s.runtimeHelpRunning
	s.workspaceMu.Unlock()
	if running {
		t.Fatal("empty runtimepath started runtime help worker")
	}
	select {
	case <-read:
		t.Fatal("empty runtimepath read a help file")
	default:
	}
}

func TestRuntimeHelpHoverAppendsSeparateDocument(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "doc/plugin.txt", "*PluginRun()*\nRuntime function help.\ng:enabled *g:enabled*\nRuntime variable help.\n*plugin#run*\nRuntime autoload help.\nlen({expr}) *len()*\nRuntime built-in help.\n*<Plug>(coc-diagnostic-prev)*\nJump to the previous diagnostic.\n")
	for _, tc := range []struct {
		name, source, needle, want string
	}{
		{"function", "\" Existing function comment.\nfunction! PluginRun()\nendfunction\ncall PluginRun()\n", "PluginRun", "Runtime function help."},
		{"explicit function", "function! g:PluginRun()\nendfunction\ncall g:PluginRun()\n", "g:PluginRun", "Runtime function help."},
		{"variable", "let g:enabled = 1\necho '😀' g:enabled\n", "g:enabled", "Runtime variable help."},
		{"implicit global", "let enabled = 1\necho enabled\n", "enabled", "Runtime variable help."},
		{"help-only variable", "echo g:enabled\n", "g:enabled", "Runtime variable help."},
		{"vim9 help-only variable", "vim9script\necho g:enabled\n", "g:enabled", "Runtime variable help."},
		{"help-only autoload", "call plugin#run()\n", "plugin#run", "Runtime autoload help."},
		{"builtin", "echo len([])\n", "len", "Runtime built-in help."},
		{"builtin arrow", "echo []->len()\n", "len", "Runtime built-in help."},
		{"plug mapping", "nmap <silent> [c <Plug>(coc-diagnostic-prev)\n", "coc-diagnostic-prev", "Jump to the previous diagnostic."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, documentURI := openNavigationDocument(t, text.UTF16, tc.source)
			t.Cleanup(s.stopAnalysis)
			before := runtimeHelpHover(t, s, documentURI, tc.needle)
			s.setRuntimePaths([]string{root})
			s.runtimeHelpWG.Wait()
			after := runtimeHelpHover(t, s, documentURI, tc.needle)
			if after == nil {
				t.Fatal("missing help hover")
			}
			sections, ok := after.Contents.(protocol.MarkedStringSlice)
			if !ok || len(sections) == 0 || !strings.Contains(fmt.Sprint(sections[len(sections)-1]), tc.want) {
				t.Fatalf("hover contents = %#v", after.Contents)
			}
			if tc.name == "variable" {
				path := mustWorkspaceCanonicalPath(t, filepath.Join(root, "doc/plugin.txt"))
				want := protocol.String("Runtime variable help.\n\n`" + path + ":3`")
				if sections[len(sections)-1] != want {
					t.Fatalf("help body/path/tag order = %#v, want %s", sections[len(sections)-1], want)
				}
			}
			if tc.name == "plug mapping" {
				if *after.Range != navigationRange(0, 17, 44) {
					t.Fatalf("plug mapping range = %#v", *after.Range)
				}
				path := mustWorkspaceCanonicalPath(t, filepath.Join(root, "doc/plugin.txt"))
				want := protocol.String("Jump to the previous diagnostic.\n\n`" + path + ":9`")
				if sections[len(sections)-1] != want {
					t.Fatalf("plug mapping help = %#v, want %s", sections[len(sections)-1], want)
				}
			}
			if tc.name == "builtin" {
				wantPath := mustWorkspaceCanonicalPath(t, filepath.Join(root, "doc/plugin.txt"))
				want := protocol.String("Runtime built-in help.\n\n`" + wantPath + ":7`")
				if sections[len(sections)-1] != want {
					t.Fatalf("builtin hover repeated signature or prose = %#v, want %s", sections[len(sections)-1], want)
				}
				resolved, err := s.CompletionResolve(context.Background(), &protocol.CompletionItem{Label: "len", Data: completionResolveTargetData(completionResolveBuiltinFunction, "len")})
				if err != nil {
					t.Fatal(err)
				}
				doc, ok := resolved.Documentation.(*protocol.MarkupContent)
				if !ok || !strings.Contains(doc.Value, "Runtime built-in help.") {
					t.Fatalf("builtin completion documentation = %#v", resolved.Documentation)
				}
			}
			if before != nil {
				if *before.Range != *after.Range {
					t.Fatal("appending help changed the hover range")
				}
				switch old := before.Contents.(type) {
				case protocol.MarkedStringSlice:
					if len(sections) != len(old)+1 {
						t.Fatalf("sections = %#v, before = %#v", sections, old)
					}
					for i, section := range old {
						got, _ := protocol.Marshal(sections[i])
						want, _ := protocol.Marshal(section)
						if string(got) != string(want) {
							t.Fatalf("existing section changed: %s != %s", got, want)
						}
					}
				case *protocol.MarkupContent:
					if sections[0] != protocol.String(old.Value) {
						t.Fatalf("existing description changed: %#v", sections)
					}
				}
			}
		})
	}
}

func TestRuntimeHelpHoverPlaintextAndLocalShadowing(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "doc/plugin.txt", "*PluginRun()*\nGlobal function help.\n*g:enabled*\nGlobal variable help.\n")
	for _, tc := range []struct{ source, needle string }{
		{"vim9script\ndef PluginRun()\nenddef\nPluginRun()\n", "PluginRun"},
		{"function! s:PluginRun()\nendfunction\ncall s:PluginRun()\n", "s:PluginRun"},
		{"let s:enabled = 1\necho s:enabled\n", "s:enabled"},
		{"vim9script\nvar enabled = 1\necho enabled\n", "enabled"},
		{"function! Test(enabled)\necho a:enabled\nendfunction\n", "a:enabled"},
		{"function! Test()\nlet enabled = 1\necho enabled\nendfunction\n", "enabled"},
	} {
		s, documentURI := openNavigationDocument(t, text.UTF16, tc.source)
		t.Cleanup(s.stopAnalysis)
		before := runtimeHelpHover(t, s, documentURI, tc.needle)
		s.setRuntimePaths([]string{root})
		s.runtimeHelpWG.Wait()
		after := runtimeHelpHover(t, s, documentURI, tc.needle)
		old, _ := protocol.Marshal(before)
		got, _ := protocol.Marshal(after)
		if string(old) != string(got) {
			t.Fatalf("local symbol inherited global help: %s", got)
		}
	}
	s, documentURI := openNavigationDocument(t, text.UTF16, "let g:enabled = 1\necho g:enabled\n")
	t.Cleanup(s.stopAnalysis)
	s.languageFeatures.hoverMarkup = protocol.MarkupKindPlainText
	before := runtimeHelpHover(t, s, documentURI, "g:enabled").Contents.(*protocol.MarkupContent).Value
	s.setRuntimePaths([]string{root})
	s.runtimeHelpWG.Wait()
	content := runtimeHelpHover(t, s, documentURI, "g:enabled").Contents.(*protocol.MarkupContent)
	if content.Kind != protocol.MarkupKindPlainText || !strings.HasPrefix(content.Value, before+"\n\n---\n\n") || !strings.Contains(content.Value, "Global variable help.") {
		t.Fatalf("plaintext hover = %#v", content)
	}
}

func TestRuntimeHelpRuntimepathUpdatesAreIncremental(t *testing.T) {
	s := initializeWorkspaceServer(t, t.TempDir())
	a, b := t.TempDir(), t.TempDir()
	aPath := writeWorkspaceFile(t, a, "doc/a.txt", "*g:shared*\nFirst root.\n*g:only_a*\nOnly A.\n")
	bPath := writeWorkspaceFile(t, b, "doc/b.txt", "*g:shared*\nSecond root.\n")
	var mu sync.Mutex
	reads := make(map[string]int)
	s.testHooks.beforeRuntimeHelpRead = func(_ context.Context, path string) {
		mu.Lock()
		reads[filepath.Base(path)]++
		mu.Unlock()
	}
	update := func(roots ...string) {
		t.Helper()
		if err := s.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: roots}); err != nil {
			t.Fatal(err)
		}
		s.runtimeHelpWG.Wait()
	}
	update(a)
	// Retained files must not be read even when their on-disk contents change.
	if err := os.WriteFile(aPath, []byte("*g:shared*\nChanged on disk.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	update(a, b)
	if s.runtimeHelp["g:shared"].Markdown != "First root." {
		t.Fatalf("retained root was reparsed: %#v", s.runtimeHelp)
	}
	update(b, a)
	update(b, a) // no-op
	if s.runtimeHelp["g:shared"].Markdown != "Second root." {
		t.Fatal("reorder did not change duplicate precedence")
	}
	update(b)
	if _, ok := s.runtimeHelp["g:only_a"]; ok || len(s.runtimeHelpFiles) != 1 {
		t.Fatal("removed root retained documentation")
	}
	mu.Lock()
	if reads[filepath.Base(aPath)] != 1 || reads[filepath.Base(bPath)] != 1 {
		t.Fatalf("non-incremental reads = %v", reads)
	}
	mu.Unlock()
	update(b, a)
	if docs := s.runtimeHelpFiles[mustWorkspaceCanonicalPath(t, aPath)]; len(docs) != 1 || docs[0].Markdown != "Changed on disk." {
		t.Fatalf("re-added root did not reload: %#v", docs)
	}
	update()
	if len(s.runtimeHelp) != 0 || len(s.runtimeHelpRoots) != 0 || len(s.runtimeHelpFiles) != 0 {
		t.Fatal("empty runtimepath retained help")
	}
}

func TestRuntimeHelpLoadingDoesNotBlockHoverOrPublishObsoleteDocs(t *testing.T) {
	s, documentURI := openNavigationDocument(t, text.UTF16, "let g:enabled = 1\necho g:enabled\n")
	t.Cleanup(s.stopAnalysis)
	a, b := t.TempDir(), t.TempDir()
	writeWorkspaceFile(t, a, "doc/a.txt", "*g:enabled*\nObsolete help.\n")
	writeWorkspaceFile(t, b, "doc/b.txt", "*g:enabled*\nCurrent help.\n")
	started, stopped := make(chan struct{}), make(chan struct{})
	s.testHooks.beforeRuntimeHelpRead = func(ctx context.Context, path string) {
		if filepath.Base(path) == "a.txt" {
			close(started)
			<-ctx.Done()
			close(stopped)
		}
	}
	s.setRuntimePaths([]string{a})
	waitForServerRace(t, started, "background help read")
	if hover := runtimeHelpHover(t, s, documentURI, "g:enabled"); hover == nil {
		t.Fatal("loading suppressed existing hover")
	}
	s.setRuntimePaths([]string{b})
	s.runtimeHelpWG.Wait()
	waitForServerRace(t, stopped, "obsolete help cancellation")
	sections := runtimeHelpHover(t, s, documentURI, "g:enabled").Contents.(protocol.MarkedStringSlice)
	if !strings.Contains(fmt.Sprint(sections[len(sections)-1]), "Current help.") || len(s.runtimeHelpFiles) != 1 {
		t.Fatalf("obsolete worker installed results: %#v", sections)
	}
}

func TestRuntimeHelpInitializeAndShutdownLifecycle(t *testing.T) {
	s := New(nil, nil, io.Discard)
	t.Cleanup(s.stopAnalysis)
	root := t.TempDir()
	writeWorkspaceFile(t, root, "doc/plugin.txt", "*g:enabled*\nHelp.\n")
	started, finished := make(chan struct{}), make(chan struct{})
	s.testHooks.beforeRuntimeHelpRead = func(ctx context.Context, _ string) {
		close(started)
		<-ctx.Done()
		close(finished)
	}
	options := protocol.LSPAny(fmt.Sprintf(`{"runtimepath":[%q]}`, root))
	if _, err := s.Initialize(context.Background(), &protocol.InitializeParams{InitializationOptions: options}); err != nil {
		t.Fatal(err)
	}
	waitForServerRace(t, started, "initial help discovery")
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("shutdown returned before help worker exited")
	}
	if len(s.runtimeHelp) != 0 {
		t.Fatal("canceled worker published help")
	}
}

func TestRuntimeHelpParserFailuresStayWithinOneFile(t *testing.T) {
	var logs bytes.Buffer
	s := New(nil, nil, &logs)
	t.Cleanup(s.stopAnalysis)
	root := t.TempDir()
	writeWorkspaceFile(t, root, "doc/a-panic.txt", "*g:bad*\nBad help.\n")
	writeWorkspaceFile(t, root, "doc/b-encoding.txt", "*g:bad*\n\xff\n")
	writeWorkspaceFile(t, root, "doc/c-read.txt", "*g:bad*\nRemoved before read.\n")
	writeWorkspaceFile(t, root, "doc/d-good.txt", "*g:good*\nGood help.\n*Incomplete()*\nExample: >\n  echo 1\n")
	s.testHooks.beforeRuntimeHelpParse = func(path string) {
		if filepath.Base(path) == "a-panic.txt" {
			panic("simulated parser failure")
		}
	}
	s.testHooks.beforeRuntimeHelpRead = func(_ context.Context, path string) {
		if filepath.Base(path) == "c-read.txt" {
			if err := os.Remove(path); err != nil {
				t.Error(err)
			}
		}
	}
	s.setRuntimePaths([]string{root})
	s.runtimeHelpWG.Wait()
	if len(s.runtimeHelp) != 2 || s.runtimeHelp["g:good"].Markdown != "Good help." || !strings.HasSuffix(s.runtimeHelp["Incomplete"].Markdown, "```") {
		t.Fatalf("valid/incomplete documents lost after parser error: %#v", s.runtimeHelp)
	}
	for _, want := range []string{"simulated parser failure", "not valid UTF-8", "c-read.txt"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("missing error %q in %s", want, &logs)
		}
	}
	// The same server still handles requests after the background failures.
	documentURI := uri.File(filepath.Join(root, "consumer.vim"))
	s.documents.Open(documentURI.String(), 1, "echo g:good\n")
	s.languageFeatures.hoverMarkup = protocol.MarkupKindMarkdown
	if hover := runtimeHelpHover(t, s, documentURI, "g:good"); hover == nil {
		t.Fatal("server stopped responding after parser panic")
	}
}

func TestRuntimeHelpCrossFileHoverRetainsSourceComments(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "plugin/demo.vim", "\" Source function comment.\nfunction! DemoRun()\nendfunction\n")
	writeWorkspaceFile(t, root, "autoload/demo.vim", "function! demo#Run()\nendfunction\n")
	writeWorkspaceFile(t, root, "doc/demo.txt", "*DemoRun()*\nHelp-file function description.\n*demo#Run()*\nAutoload function-value help.\n")
	s := initializeWorkspaceServer(t, root)
	s.languageFeatures.hoverMarkup = protocol.MarkupKindMarkdown
	if err := s.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{root}}); err != nil {
		t.Fatal(err)
	}
	s.runtimeHelpWG.Wait()
	documentURI := uri.File(filepath.Join(root, "consumer.vim"))
	s.documents.Open(documentURI.String(), 1, "call DemoRun()\n")
	hover := runtimeHelpHover(t, s, documentURI, "DemoRun")
	if hover == nil {
		t.Fatal("missing external function hover")
	}
	sections, ok := hover.Contents.(protocol.MarkedStringSlice)
	if !ok || len(sections) != 3 || sections[1] != protocol.String("Source function comment.") || !strings.Contains(fmt.Sprint(sections[2]), "Help-file function description.") {
		t.Fatalf("cross-file hover = %#v", hover.Contents)
	}
	s.documents.Open(documentURI.String(), 2, "vim9script\nvar Ref = demo#Run\n")
	hover = runtimeHelpHover(t, s, documentURI, "demo#Run")
	if hover == nil {
		t.Fatal("missing autoload function-value hover")
	}
	sections, ok = hover.Contents.(protocol.MarkedStringSlice)
	if !ok || len(sections) != 2 || !strings.Contains(fmt.Sprint(sections[1]), "Autoload function-value help.") {
		t.Fatalf("autoload function value confused with variable: %#v", hover.Contents)
	}
}

func TestRuntimeHelpReadRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = file.Truncate(maxRuntimeHelpFileBytes + 1)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readRuntimeHelpFile(path); err == nil {
		t.Fatal("oversized input was not rejected")
	}
}

func TestRuntimeHelpRetainsInFlightParseWhenAddingOrReorderingRoots(t *testing.T) {
	s := New(nil, nil, io.Discard)
	t.Cleanup(s.stopAnalysis)
	a, b := t.TempDir(), t.TempDir()
	writeWorkspaceFile(t, a, "doc/a.txt", "*g:shared*\nA help.\n")
	writeWorkspaceFile(t, b, "doc/b.txt", "*g:shared*\nB help.\n")
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	reads := make(chan string, 4)
	s.testHooks.beforeRuntimeHelpRead = func(ctx context.Context, path string) {
		reads <- filepath.Base(path)
		if filepath.Base(path) == "a.txt" {
			close(started)
			<-release
			if ctx.Err() != nil {
				t.Error("retained in-flight root was canceled")
			}
		}
	}
	s.setRuntimePaths([]string{a})
	waitForServerRace(t, started, "retained root read")
	s.setRuntimePaths([]string{a, b})
	s.setRuntimePaths([]string{b, a})
	once.Do(func() { close(release) })
	s.runtimeHelpWG.Wait()
	if len(reads) != 2 || <-reads != "a.txt" || <-reads != "b.txt" || s.runtimeHelp["g:shared"].Markdown != "B help." {
		t.Fatal("in-flight root was reparsed or reordered precedence was lost")
	}
}
