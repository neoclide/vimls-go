package server

import (
	"context"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
)

type indexingClient struct {
	protocol.UnimplementedClient
	mu       sync.Mutex
	progress []string
	logs     []string
	refresh  chan string
}

func (c *indexingClient) WorkDoneProgressCreate(context.Context, *protocol.WorkDoneProgressCreateParams) error {
	return nil
}
func (c *indexingClient) Progress(_ context.Context, p *protocol.ProgressParams) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.progress = append(c.progress, string(p.Value))
	return nil
}
func (c *indexingClient) LogMessage(_ context.Context, p *protocol.LogMessageParams) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs = append(c.logs, p.Message)
	return nil
}
func (c *indexingClient) DiagnosticRefresh(context.Context) error {
	c.refresh <- "diagnostic"
	return nil
}
func (c *indexingClient) SemanticTokensRefresh(context.Context) error {
	c.refresh <- "semantic"
	return nil
}
func (c *indexingClient) InlayHintRefresh(context.Context) error { c.refresh <- "inlay"; return nil }
func (c *indexingClient) CodeLensRefresh(context.Context) error  { c.refresh <- "lens"; return nil }

func TestWorkspaceAndRuntimepathHaveSeparateProgressAndRefresh(t *testing.T) {
	root, external := t.TempDir(), t.TempDir()
	workspacePath := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "workspace.vim", "let g:Workspace = 1\n"))
	runtimePath := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, external, "plugin/runtime.vim", "command! RuntimeCommand echo 1\n"))
	s := initializeWorkspaceServer(t, root)
	client := &indexingClient{refresh: make(chan string, 16)}
	s.client = client
	s.workspaceProgress = true
	s.diagnosticRefreshSupport = true
	s.pullDiagnostics = true
	s.semanticTokensRefreshSupport = true
	s.inlayHintRefreshSupport = true
	s.codeLensRefreshSupport = true
	s.setRuntimePaths([]string{external})
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	s.testHooks.discoverWorkspaceFiles = func(ctx context.Context, path string, limit int) ([]string, bool, error) {
		if path == mustWorkspaceCanonicalPath(t, external) {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return []string{runtimePath}, false, ctx.Err()
		}
		return workspace.DiscoverFilesContext(ctx, path, limit)
	}
	s.scheduleWorkspaceRebuild()
	waitForServerRace(t, started, "runtime phase after workspace")
	if _, ok := s.workspaceIndex.Source(workspacePath); !ok {
		t.Fatal("workspace not installed before runtime scan")
	}
	client.mu.Lock()
	progress := strings.Join(client.progress, "\n")
	client.mu.Unlock()
	if !strings.Contains(progress, `"kind":"end"`) || strings.Contains(progress, external) {
		t.Fatalf("workspace progress = %s", progress)
	}
	waitRefresh := func() {
		t.Helper()
		got := make([]string, 0, 4)
		for range 4 {
			select {
			case name := <-client.refresh:
				got = append(got, name)
			case <-time.After(5 * time.Second):
				t.Fatalf("refreshes = %v", got)
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, []string{"diagnostic", "inlay", "lens", "semantic"}) {
			t.Fatalf("refreshes = %v", got)
		}
	}
	waitRefresh()
	once.Do(func() { close(release) })
	s.workspaceWG.Wait()
	waitRefresh()
	if _, ok := s.workspaceIndex.Source(runtimePath); !ok {
		t.Fatal("runtime source missing")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	runtimeLogs := 0
	for _, message := range client.logs {
		if strings.Contains(message, "scanned runtimepath; total elapsed ") {
			runtimeLogs++
		}
	}
	if runtimeLogs != 1 {
		t.Fatalf("runtime logs = %v", client.logs)
	}
}

func TestRuntimepathScansRootsConcurrentlyAndRetainsFactsOnWorkspaceRebuild(t *testing.T) {
	s := initializeWorkspaceServer(t, t.TempDir())
	roots := []string{t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()}
	paths := make(map[string]string)
	for _, root := range roots {
		paths[mustWorkspaceCanonicalPath(t, root)] = mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, root, "plugin/test.vim", "command! Original echo 1\n"))
	}
	color := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, roots[0], "colors/retained.vim", ""))
	started, release := make(chan string, 4), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	s.testHooks.discoverWorkspaceFiles = func(ctx context.Context, root string, limit int) ([]string, bool, error) {
		if path, ok := paths[root]; ok {
			started <- root
			select {
			case <-release:
			case <-ctx.Done():
			}
			files := []string{path}
			if root == mustWorkspaceCanonicalPath(t, roots[0]) {
				files = append(files, color)
			}
			return files, false, ctx.Err()
		}
		return workspace.DiscoverFilesContext(ctx, root, limit)
	}
	done := make(chan error, 1)
	go func() {
		done <- s.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: roots})
	}()
	for range roots {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("runtime roots not scanned concurrently")
		}
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, root := range roots {
		writeWorkspaceFile(t, root, "plugin/test.vim", "command! Changed echo 2\n")
	}
	s.scheduleWorkspaceRebuild()
	s.workspaceWG.Wait()
	select {
	case root := <-started:
		t.Fatalf("retained root rescanned: %s", root)
	default:
	}
	for _, path := range paths {
		if source, _ := s.workspaceIndex.Source(path); source != "command! Original echo 1\n" {
			t.Fatalf("retained source reparsed: %q", source)
		}
	}
	if !slices.Contains(s.workspaceIndex.UserCommandNames(), "Original") {
		t.Fatal("retained command facts lost")
	}
	if path, ok := s.workspaceIndex.RuntimeFile("colors/retained.vim"); !ok || path != color {
		t.Fatal("retained colorscheme catalog lost")
	}
}

func TestRuntimepathDebouncesBurst(t *testing.T) {
	// Filesystem setup must not consume the debounce window. Fake time makes
	// request spacing independent of filesystem speed and CI scheduling.
	roots := make([]string, 5)
	for index := range roots {
		roots[index] = mustWorkspaceCanonicalPath(t, t.TempDir())
	}
	synctest.Test(t, func(t *testing.T) {
		s := New(nil, nil, io.Discard)
		t.Cleanup(s.stopAnalysis)
		scanned := make(chan string, len(roots))
		s.testHooks.discoverWorkspaceFiles = func(_ context.Context, root string, _ int) ([]string, bool, error) {
			scanned <- root
			return nil, false, nil
		}
		var wg sync.WaitGroup
		for index, root := range roots {
			if index > 0 {
				time.Sleep(defaultRuntimepathDebounce / 4)
			}
			wg.Go(func() {
				if err := s.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{root}}); err != nil {
					t.Errorf("runtimepath update: %v", err)
				}
			})
			// Ensure input is accepted and its timer is waiting before advancing.
			synctest.Wait()
		}
		time.Sleep(defaultRuntimepathDebounce - time.Nanosecond)
		synctest.Wait()
		if len(scanned) != 0 {
			t.Fatal("runtimepath scanned before the latest debounce expired")
		}
		time.Sleep(time.Nanosecond)
		wg.Wait()
		last := roots[len(roots)-1]
		if !slices.Equal(s.runtimePaths, []string{last}) {
			t.Fatalf("latest runtimepath = %v", s.runtimePaths)
		}
		if len(scanned) != 1 || <-scanned != last {
			t.Fatal("burst scanned superseded roots")
		}
	})
}

func TestRuntimepathDirectoryMovedOutOfWorkspaceIsScanned(t *testing.T) {
	root := t.TempDir()
	runtime := filepath.Join(root, "runtime")
	path := writeWorkspaceFile(t, runtime, "plugin/test.vim", "let g:Before = 1\n")
	s := initializeWorkspaceServer(t, root)
	if err := s.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{runtime}}); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceFile(t, runtime, "plugin/new.vim", "let g:Added = 1\n")
	s.setWorkspaceRoots(nil)
	s.scheduleWorkspaceRebuild()
	s.workspaceWG.Wait()
	if _, ok := s.workspaceIndex.Source(mustWorkspaceCanonicalPath(t, path)); !ok {
		t.Fatal("retained file lost")
	}
	if _, ok := s.workspaceIndex.Source(mustWorkspaceCanonicalPath(t, filepath.Join(runtime, "plugin/new.vim"))); !ok {
		t.Fatal("newly external root not scanned")
	}
}

func TestRuntimepathRemovingNestedRootDiscardsItsFacts(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "pack", "plugin")
	path := writeWorkspaceFile(t, nested, "plugin/command.vim", "command! RemovedCommand echo 1\n")
	writeWorkspaceFile(t, nested, "colors/removed.vim", "")
	s := initializeWorkspaceServer(t, t.TempDir())
	for _, paths := range [][]string{{root, nested}, {root}} {
		if err := s.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: paths}); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := s.workspaceIndex.Source(mustWorkspaceCanonicalPath(t, path)); ok {
		t.Fatal("deleted nested root still owns source through parent runtime root")
	}
	if slices.Contains(s.workspaceIndex.UserCommandNames(), "RemovedCommand") {
		t.Fatal("removed user command facts retained")
	}
	if _, ok := s.workspaceIndex.RuntimeFile("pack/plugin/colors/removed.vim"); ok {
		t.Fatal("removed colorscheme catalog retained through parent root")
	}
}

func TestRuntimepathQueuedUpdateStartsAfterActiveBatchInstalled(t *testing.T) {
	s := initializeWorkspaceServer(t, t.TempDir())
	first, second := t.TempDir(), t.TempDir()
	firstPath := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, first, "plugin/first.vim", "command! First echo 1\n"))
	secondPath := mustWorkspaceCanonicalPath(t, writeWorkspaceFile(t, second, "plugin/second.vim", "command! Second echo 2\n"))
	started, release := make(chan struct{}), make(chan struct{})
	observed := make(chan bool, 1)
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	s.testHooks.discoverWorkspaceFiles = func(ctx context.Context, root string, _ int) ([]string, bool, error) {
		if root == mustWorkspaceCanonicalPath(t, first) {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return []string{firstPath}, false, ctx.Err()
		}
		s.workspaceMu.Lock()
		_, installed := s.workspaceIndex.Source(firstPath)
		s.workspaceMu.Unlock()
		observed <- installed
		return []string{secondPath}, false, nil
	}
	done := make(chan error, 2)
	go func() {
		done <- s.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{first}})
	}()
	waitForServerRace(t, started, "first batch discovery")
	go func() {
		done <- s.DidChangeRuntimepath(context.Background(), &DidChangeRuntimepathParams{Runtimepath: []string{second}})
	}()
	select {
	case <-observed:
		t.Fatal("queued batch overlapped active batch")
	case <-time.After(150 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if !<-observed {
		t.Fatal("active batch discarded instead of installed")
	}
	if _, ok := s.workspaceIndex.Source(firstPath); ok {
		t.Fatal("old batch source was not removed")
	}
	if _, ok := s.workspaceIndex.Source(secondPath); !ok {
		t.Fatal("queued batch source missing")
	}
}
