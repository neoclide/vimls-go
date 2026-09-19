package server

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/uri"
)

func importedDeclarationType(t *testing.T, result *analysis.FileAnalysis, name string) string {
	t.Helper()
	if result == nil {
		t.Fatal("analysis unexpectedly abandoned")
	}
	for _, declaration := range result.Declarations {
		if declaration.Name == name {
			if declaration.Type.Name == "" {
				return "unknown"
			}
			return formatValueType(declaration.Type)
		}
	}
	t.Fatalf("missing declaration %s", name)
	return ""
}

func installImportTypeTarget(t *testing.T, s *Server, path, source string) {
	t.Helper()
	file := syntax.Parse(source)
	s.publishMu.Lock()
	s.replaceWorkspaceFileWithAnalysisSnapshot(uri.File(path).String(), file, analysis.Analyze(file))
	s.publishMu.Unlock()
}

func TestImportTypesRefreshWithoutImporterEdits(t *testing.T) {
	root := t.TempDir()
	target := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport var Value = 1\n")
	source := "vim9script\nimport './lib.vim'\nvar value = lib.Value\n"
	main := writeWorkspaceFile(t, root, "main.vim", source)
	other := writeWorkspaceFile(t, root, "other.vim", "vim9script\nvar x = 1\n")
	s := initializeWorkspaceServer(t, root)
	snapshot := s.documents.Open(uri.File(main).String(), 1, source)
	file, before := s.analyzeSnapshot(snapshot)
	if got := importedDeclarationType(t, before, "value"); got != "number" {
		t.Fatalf("initial type = %s", got)
	}
	_, hot := s.analyzeSnapshot(snapshot)
	if hot != before {
		t.Fatal("hot analysis was not shared")
	}
	installImportTypeTarget(t, s, other, "vim9script\nvar x = 'changed'\n")
	_, unrelated := s.analyzeSnapshot(snapshot)
	if unrelated != before {
		t.Fatal("unrelated edit invalidated analysis")
	}
	s.publishMu.Lock()
	s.documents.Open(uri.File(target).String(), 2, "vim9script\nexport var Value = 'new'\n")
	s.removeWorkspaceURI(uri.File(target).String())
	s.publishMu.Unlock()
	_, pending := s.analyzeSnapshot(snapshot)
	if got := importedDeclarationType(t, pending, "value"); got != "unknown" && got != "" {
		t.Fatalf("pending target supplied old type: %s", got)
	}
	installImportTypeTarget(t, s, target, "vim9script\nexport var Value = 'new'\n")
	afterFile, after := s.analyzeSnapshot(snapshot)
	if got := importedDeclarationType(t, after, "value"); got != "string" {
		t.Fatalf("updated type = %s", got)
	}
	if afterFile != file || after == before {
		t.Fatal("syntax was discarded or stale analysis reused")
	}
	// A/B/A must use a new dependency identity even when the source returns.
	installImportTypeTarget(t, s, target, "vim9script\nexport var Value = 1\n")
	_, restored := s.analyzeSnapshot(snapshot)
	if restored == before || importedDeclarationType(t, restored, "value") != "number" {
		t.Fatal("old dependency computation resurrected")
	}
}

func TestImportTypesPathFormsAndConservativeTargets(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "import/lib.vim", "vim9script\nexport const Value = 1\nvar Private = 'x'\n")
	target := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport var Value = 1\n")
	writeWorkspaceFile(t, root, "legacy.vim", "let Value = 1\n")
	writeWorkspaceFile(t, root, "duplicate.vim", "vim9script\nexport var Value = 1\nvar Value = 'x'\n")
	s := initializeWorkspaceServer(t, root)
	for i, test := range []struct{ statement, member, want string }{
		{"import './lib.vim' as lib", "lib.Value", "number"},
		{"import 'lib.vim'", "lib.Value", "number"},
		{fmt.Sprintf("import '%s' as lib", filepath.ToSlash(target)), "lib.Value", "number"},
		{"import autoload './lib.vim' as lib", "lib.Value", "unknown"},
		{"import './missing.vim' as lib", "lib.Value", "unknown"},
		{"import 'lib.vim' as lib", "lib.Private", "unknown"},
		{"import './legacy.vim' as lib", "lib.Value", "unknown"},
		{"import './duplicate.vim' as lib", "lib.Value", "unknown"},
		{"import './lib.vim' as lib\nimport 'lib.vim' as lib", "lib.Value", "unknown"},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			source := "vim9script\n" + test.statement + "\nvar value = " + test.member + "\n"
			snapshot := s.documents.Open(uri.File(filepath.Join(root, fmt.Sprintf("main%d.vim", i))).String(), 1, source)
			_, result := s.analyzeSnapshot(snapshot)
			if got := importedDeclarationType(t, result, "value"); got != test.want {
				t.Fatalf("%s: type %q, want %q", test.statement, got, test.want)
			}
		})
	}
}

func TestImportTypesCancelStaleDependencyAnalysis(t *testing.T) {
	root := t.TempDir()
	target := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport var Value = 1\n")
	s := initializeWorkspaceServer(t, root)
	source := "vim9script\nimport './lib.vim'\nvar value = lib.Value\n"
	snapshot := s.documents.Open(uri.File(filepath.Join(root, "main.vim")).String(), 1, source)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s.testHooks.beforeAnalyze = func(file *syntax.File) {
		if file.Source == source {
			once.Do(func() { close(entered); <-release })
		}
	}
	done := make(chan *analysis.FileAnalysis, 1)
	go func() { _, result := s.analyzeSnapshot(snapshot); done <- result }()
	<-entered
	installImportTypeTarget(t, s, target, "vim9script\nexport var Value = 'changed'\n")
	close(release)
	if result := <-done; result != nil {
		t.Fatal("stale dependency result was returned")
	}
	_, result := s.analyzeSnapshot(snapshot)
	if got := importedDeclarationType(t, result, "value"); got != "string" {
		t.Fatalf("fresh type = %s", got)
	}
}

func TestImportTypesClosedDiagnosticsAndCompletion(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport var Value = 1\n")
	source := "vim9script\nimport './lib.vim'\nvar value = lib.Value\ndef Check()\n  var wrong: string = lib.Value\nenddef\n"
	main := writeWorkspaceFile(t, root, "main.vim", source)
	s := initializeWorkspaceServer(t, root)
	closed := text.NewSnapshot(uri.File(main).String(), 1, nil, source)
	file, _, ok := s.computeClosedWorkspaceDiagnostics(context.Background(), closed)
	if !ok {
		t.Fatal("closed diagnostics abandoned")
	}
	found := false
	for _, d := range file.Diagnostics {
		found = found || d.Code == "vim/E1012"
	}
	if !found {
		t.Fatalf("missing imported type diagnostic: %#v", file.Diagnostics)
	}
	snapshot := s.documents.Open(closed.URI(), 1, source)
	parsed := s.parseSnapshot(snapshot)
	s.testHooks.beforeAnalyze = func(*syntax.File) { t.Error("completion started full analysis") }
	_, facts := s.completionSnapshotFacts(context.Background(), snapshot, parsed)
	query := analysis.NewCompletionTypes(facts)
	for _, declaration := range facts.Declarations {
		if declaration.Name == "value" && query.DeclarationType(declaration).Name != "number" {
			t.Fatal("completion type missing")
		}
	}
}

func TestImportTypesResolverChangesAndTargetRecreation(t *testing.T) {
	root := t.TempDir()
	first := writeWorkspaceFile(t, root, "first/import/lib.vim", "vim9script\nexport var Value = 1\n")
	second := writeWorkspaceFile(t, root, "second/import/lib.vim", "vim9script\nexport var Value = 's'\n")
	s := initializeWorkspaceServer(t, root)
	s.setRuntimePaths([]string{filepath.Dir(filepath.Dir(first)), filepath.Dir(filepath.Dir(second))})
	s.refreshWorkspaceResolver()
	source := "vim9script\nimport 'lib.vim'\nvar value = lib.Value\n"
	snapshot := s.documents.Open(uri.File(filepath.Join(root, "main.vim")).String(), 1, source)
	_, before := s.analyzeSnapshot(snapshot)
	if importedDeclarationType(t, before, "value") != "number" {
		t.Fatal("first runtime target missing")
	}
	s.setRuntimePaths([]string{filepath.Dir(filepath.Dir(second)), filepath.Dir(filepath.Dir(first))})
	s.refreshWorkspaceResolver()
	_, after := s.analyzeSnapshot(snapshot)
	if importedDeclarationType(t, after, "value") != "string" {
		t.Fatal("runtime order did not change type")
	}
	s.publishMu.Lock()
	s.replaceWorkspaceFileWithAnalysisSnapshot(uri.File(second).String(), nil, nil)
	s.publishMu.Unlock()
	_, removed := s.analyzeSnapshot(snapshot)
	if importedDeclarationType(t, removed, "value") != "unknown" {
		t.Fatal("removed source retained its type")
	}
	installImportTypeTarget(t, s, second, "vim9script\nexport var Value = true\n")
	_, recreated := s.analyzeSnapshot(snapshot)
	if importedDeclarationType(t, recreated, "value") != "bool" {
		t.Fatal("recreated target did not refresh")
	}
}

func TestImportTypesRejectStalePreparationAndKeepCompletionCache(t *testing.T) {
	root := t.TempDir()
	target := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport var Value = 1\n")
	source := "vim9script\nimport './lib.vim'\nvar value = lib.Value\n"
	main := writeWorkspaceFile(t, root, "main.vim", source)
	s := initializeWorkspaceServer(t, root)
	snapshot := s.documents.Open(uri.File(main).String(), 1, source)
	work, ok := s.documents.BeginAnalysis(context.Background(), snapshot.URI())
	if !ok {
		t.Fatal("work not created")
	}
	file, old := s.analyzeSnapshot(snapshot)
	installImportTypeTarget(t, s, target, "vim9script\nexport var Value = 's'\n")
	if _, ok := s.prepareSyntax(work, file, old); ok {
		t.Fatal("stale imports were installed with a fresh workspace identity")
	}
	_, first := s.completionSnapshotFacts(context.Background(), snapshot, file)
	_, second := s.completionSnapshotFacts(context.Background(), snapshot, file)
	if first == nil || first != second || first == old {
		t.Fatal("stale full facts prevented lexical cache reuse")
	}
}

func TestImportTypesDoNotPublishInferredReexports(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport const Value = 1\n")
	source := "vim9script\nimport './lib.vim'\nexport var Value = lib.Value\nexport var Annotated: number = lib.Value\n"
	bridge := writeWorkspaceFile(t, root, "bridge.vim", source)
	s := initializeWorkspaceServer(t, root)
	snapshot := s.documents.Open(uri.File(bridge).String(), 1, source)
	file, result := s.analyzeSnapshot(snapshot)
	if importedDeclarationType(t, result, "Value") != "number" {
		t.Fatal("local importer should infer the value")
	}
	s.publishMu.Lock()
	s.replaceWorkspaceFileWithAnalysisSnapshot(snapshot.URI(), file, result)
	s.publishMu.Unlock()
	for _, match := range s.workspaceIndex.FileSymbols(bridge) {
		got := match.Fact.StaticType.ValueType().Name
		if match.Fact.Name == "Value" && got != "" {
			t.Fatal("warm index leaked a transitive type")
		}
		if match.Fact.Name == "Annotated" && got != "number" {
			t.Fatal("explicit export annotation lost")
		}
	}
}

func TestImportTypesBackgroundRetryFinishesPendingIndex(t *testing.T) {
	root := t.TempDir()
	target := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport var Value = 1\n")
	source := "vim9script\nimport './lib.vim'\nvar value = lib.Value\n"
	main := writeWorkspaceFile(t, root, "main.vim", source)
	s := initializeWorkspaceServer(t, root)
	snapshot := s.documents.Open(uri.File(main).String(), 1, source)
	entered, release := make(chan struct{}), make(chan struct{})
	finished := make(chan struct{}, 4)
	var once sync.Once
	s.testHooks.beforeAnalyze = func(file *syntax.File) {
		if file.Source == source {
			once.Do(func() { close(entered); <-release })
		}
	}
	s.testHooks.afterAnalysisFinished = func(documentURI string) {
		if documentURI == snapshot.URI() {
			finished <- struct{}{}
		}
	}
	s.publishMu.Lock()
	s.removeWorkspaceURI(snapshot.URI())
	s.publishMu.Unlock()
	s.startAnalysis(snapshot.URI())
	<-entered
	installImportTypeTarget(t, s, target, "vim9script\nexport var Value = 's'\n")
	close(release)
	for {
		select {
		case <-finished:
			s.publishMu.Lock()
			result := s.parsed[snapshot.URI()].analysis
			s.publishMu.Unlock()
			if result == nil {
				continue
			}
			if importedDeclarationType(t, result, "value") != "string" {
				t.Fatal("retry retained old type")
			}
			s.workspaceMu.Lock()
			pending := len(s.workspacePending)
			s.workspaceMu.Unlock()
			if pending != 0 {
				t.Fatalf("retry stranded %d pending files", pending)
			}
			return
		case <-time.After(5 * time.Second):
			t.Fatal("dependency retry did not finish")
		}
	}
}

func TestImportTypesOwnerCancellationPreservesSharedAnalysis(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport var Value = 1\n")
	s := initializeWorkspaceServer(t, root)
	source := "vim9script\nimport './lib.vim'\nvar value = lib.Value\n"
	snapshot := s.documents.Open(uri.File(filepath.Join(root, "main.vim")).String(), 1, source)
	entered, release, waiting := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s.testHooks.beforeAnalyze = func(*syntax.File) { close(entered); <-release }
	s.testHooks.beforeAnalysisInFlightWait = func(*text.Snapshot) { close(waiting) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ownerDone := make(chan struct{})
	go func() { s.analyzeSnapshotContext(ctx, snapshot); close(ownerDone) }()
	<-entered
	waiterDone := make(chan *analysis.FileAnalysis, 1)
	go func() { _, result := s.analyzeSnapshotContext(context.Background(), snapshot); waiterDone <- result }()
	<-waiting
	cancel()
	close(release)
	<-ownerDone
	result := <-waiterDone
	if importedDeclarationType(t, result, "value") != "number" {
		t.Fatal("owner canceled useful shared analysis")
	}
	_, cached := s.analyzeSnapshot(snapshot)
	if cached != result {
		t.Fatal("shared result was not cached")
	}
}
