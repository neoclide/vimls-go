package workspace

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestIndexGlobalFactsAndParseSourceWorkerBoundaries(t *testing.T) {
	index := NewIndex(10, 10_000)
	root := t.TempDir()
	for _, test := range []struct{ path, source string }{
		{filepath.Join(root, "later.vim"), "let g:shared = 1\nfunction! Shared()\nendfunction\n"},
		{filepath.Join(root, "early.vim"), "let g:shared = 2\nlet g:other = 3\n"},
	} {
		if err := index.Replace(test.path, syntax.Parse(test.source)); err != nil {
			t.Fatal(err)
		}
	}
	facts := index.GlobalNameFacts("shared")
	if len(facts) != 2 || facts[0].Path >= facts[1].Path {
		t.Fatalf("ordered global facts = %#v", facts)
	}
	items, incomplete := index.GlobalVariableCompletions("", "", 10)
	if !incomplete || len(items) != 2 || items[0].Name != "other" || items[1].Name != "shared" {
		t.Fatalf("global completions = %#v, incomplete=%t", items, incomplete)
	}
	if got, incomplete := index.GlobalVariableCompletions("", "", 1); !incomplete || len(got) != 1 {
		t.Fatalf("limited global completions = %#v, incomplete=%t", got, incomplete)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if results := ParseSources(cancelled, []string{"vim9script\nvar skipped = 1\n"}, 1); len(results) != 1 || results[0] != nil {
		t.Fatalf("cancelled parses = %#v", results)
	}
	results := ParseSources(context.Background(), []string{"vim9script\nvar first = 1\n", "let second = 2\n"}, 99)
	if len(results) != 2 || results[0] == nil || results[1] == nil || results[0].Dialect != syntax.Vim9 || results[1].Dialect != syntax.Legacy {
		t.Fatalf("worker parses = %#v", results)
	}
}

func TestCollectGlobalNameFactsReplaysConflictDeletionAndOrdering(t *testing.T) {
	source := "g:beta g:alpha g:alpha g:beta"
	file := &syntax.File{Source: source, Commands: []syntax.Command{
		{Dialect: syntax.Vim9, Declaration: &syntax.Declaration{Bindings: []syntax.Binding{{Name: syntax.Span{Start: 0, End: 6}}}}},
		{Dialect: syntax.Vim9, Function: &syntax.Function{Name: syntax.Span{Start: 7, End: 14}}},
		{Canonical: "delfunction", Dialect: syntax.Vim9, Targets: []*syntax.Expression{{Kind: syntax.ExpressionIdentifier, Span: syntax.Span{Start: 15, End: 22}}}},
		{Canonical: "unlet", Dialect: syntax.Vim9, Targets: []*syntax.Expression{{Kind: syntax.ExpressionIdentifier, Span: syntax.Span{Start: 23, End: 29}}}},
	}}
	facts := CollectGlobalNameFacts(filepath.Join(t.TempDir(), "globals.vim"), file)
	if len(facts) != 0 {
		t.Fatalf("deleted globals = %#v", facts)
	}
	file.Commands = file.Commands[:2]
	facts = CollectGlobalNameFacts(filepath.Join(t.TempDir(), "globals.vim"), file)
	if len(facts) != 2 || facts[0].Name != "alpha" || facts[0].Kind != analysis.NameDeclarationFunction || facts[1].Name != "beta" || facts[1].Kind != analysis.NameDeclarationVariable {
		t.Fatalf("replayed globals = %#v", facts)
	}
	if CollectGlobalNameFacts("\x00invalid", file) != nil || CollectGlobalNameFacts("valid.vim", nil) != nil {
		t.Fatal("invalid global-fact input produced results")
	}
}

func TestPathIdentityAndDiscoveryHelperBoundaries(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child", "item.vim")
	if !pathWithin(root, child) || !pathWithinOrEqual(root, root) || !pathWithinOrEqual(root, child) {
		t.Fatal("contained paths were rejected")
	}
	for _, outside := range []string{filepath.Dir(root), root + "-other"} {
		if pathWithin(root, outside) || pathWithinOrEqual(root, outside) {
			t.Errorf("outside path accepted: %q", outside)
		}
	}
	for _, test := range []struct {
		path string
		want bool
	}{{"/absolute/path", true}, {"C:/windows/path", true}, {"D:\\windows\\path", true}, {"relative/path", false}, {"C:relative", false}} {
		if got := isAbsolutePath(test.path); got != test.want {
			t.Errorf("isAbsolutePath(%q) = %t", test.path, got)
		}
	}
	if got, err := CanonicalPath(""); err == nil || got != "" {
		t.Fatalf("empty canonical path = %q, %v", got, err)
	}
	missing := filepath.Join(root, "missing", "child.vim")
	canonicalRoot, err := CanonicalPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := CanonicalPath(missing); err != nil || got != filepath.Join(canonicalRoot, "missing", "child.vim") {
		t.Fatalf("missing canonical path = %q, %v", got, err)
	}
	for _, test := range []struct {
		relative, name string
		runtime        bool
		want           bool
	}{{"plugin/file", "file", true, true}, {"plugin/file.txt", "file.txt", true, false}, {"plain.vim", "plain.vim", false, true}, {"vimrc", "vimrc", false, true}, {"plain.txt", "plain.txt", false, false}} {
		if got := isVimFile(test.relative, test.name, test.runtime); got != test.want {
			t.Errorf("isVimFile(%q, %q, %t) = %t", test.relative, test.name, test.runtime, got)
		}
	}
}

func TestImportGraphSnapshotReverseDependenciesAndFactOrdering(t *testing.T) {
	root := t.TempDir()
	paths := []string{filepath.Join(root, "a.vim"), filepath.Join(root, "b.vim"), filepath.Join(root, "c.vim")}
	for index := range paths {
		canonical, err := CanonicalPath(paths[index])
		if err != nil {
			t.Fatal(err)
		}
		paths[index] = canonical
	}
	snapshot := ImportGraphSnapshot{incoming: map[string][]ImportFact{
		paths[2]: {{Importer: paths[1], Target: paths[2]}},
		paths[1]: {{Importer: paths[0], Target: paths[1]}, {Importer: paths[2], Target: paths[1]}},
	}}
	dependents := snapshot.ReverseDependents(paths[2])
	if len(dependents) != 2 || dependents[0] != paths[0] || dependents[1] != paths[1] {
		t.Fatalf("reverse dependents = %#v", dependents)
	}
	if snapshot.ReverseDependents("") != nil {
		t.Fatal("empty reverse-dependency path accepted")
	}
	facts := []ImportFact{
		{Importer: "b", PathSpan: syntax.Span{Start: 2, End: 4}, Target: "b", Alias: "b"},
		{Importer: "a", PathSpan: syntax.Span{Start: 2, End: 4}, Target: "b", Alias: "b"},
		{Importer: "a", PathSpan: syntax.Span{Start: 1, End: 4}, Target: "b", Alias: "b"},
		{Importer: "a", PathSpan: syntax.Span{Start: 1, End: 3}, Target: "b", Alias: "b"},
		{Importer: "a", PathSpan: syntax.Span{Start: 1, End: 3}, Target: "a", Alias: "b"},
		{Importer: "a", PathSpan: syntax.Span{Start: 1, End: 3}, Target: "a", Alias: "a"},
	}
	sortImportFacts(facts)
	if facts[0].Alias != "a" || facts[len(facts)-1].Importer != "b" {
		t.Fatalf("sorted import facts = %#v", facts)
	}
}

func TestPathResolverCanonicalBoundaryAndNilImportResolution(t *testing.T) {
	root := t.TempDir()
	resolver, err := NewPathResolver(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "missing.vim")
	if got, ok := resolver.Canonical(inside); !ok || !pathWithinOrEqual(mustResolverCanonical(t, root), got) || !resolver.Allows(inside) {
		t.Fatalf("inside canonical = %q, %t", got, ok)
	}
	if got, ok := resolver.Canonical(""); ok || got != "" || resolver.Allows(filepath.Dir(root)) {
		t.Fatalf("boundary rejection = %q, %t", got, ok)
	}
	if result := (*PathResolver)(nil).ResolveImportPath("", "'x.vim'", false); result.Dynamic || result.Path != "" {
		t.Fatalf("nil resolver import = %#v", result)
	}
	if result := resolver.ResolveImport("", nil, nil); result.Path != "" || result.Dynamic {
		t.Fatalf("nil import syntax = %#v", result)
	}
}

func TestIndexRuntimeCatalogReconfigurationRejectsUnsafeQueries(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	firstFile := filepath.Join(first, "import", "pkg", "one.vim")
	secondFile := filepath.Join(second, "import", "pkg", "two.vim")
	index := NewIndex(10, 10_000)
	for _, path := range []string{firstFile, secondFile} {
		if err := index.Replace(path, syntax.Parse("vim9script\nexport def Shared()\nenddef\n")); err != nil {
			t.Fatal(err)
		}
	}

	// Reconfiguring runtime roots must rebuild the catalog from already indexed
	// files and skip duplicate/invalid roots.
	index.SetRuntimePaths([]string{"", first, first, filepath.Join(first, "missing"), second})
	if got, ok := index.RuntimeFile("import/pkg/one.vim"); !ok || got != mustResolverCanonical(t, firstFile) {
		t.Fatalf("first runtime file = %q, %t", got, ok)
	}
	if got, ok := index.RuntimeFile("import/pkg/two.vim"); !ok || got != mustResolverCanonical(t, secondFile) {
		t.Fatalf("second runtime file = %q, %t", got, ok)
	}
	for _, unsafe := range []string{"", ".", "..", "../pkg/one.vim", "/etc/passwd"} {
		if got, ok := index.RuntimeFile(unsafe); ok || got != "" {
			t.Errorf("unsafe runtime lookup %q = %q, %t", unsafe, got, ok)
		}
	}
	for _, prefix := range []string{"../", "bad\\path", "bad\npath"} {
		items, incomplete := index.RuntimePathCompletions("import", prefix, 10)
		if len(items) != 0 || incomplete {
			t.Errorf("invalid runtime completion %q = %#v, incomplete=%t", prefix, items, incomplete)
		}
	}

	index.SetRuntimePaths([]string{second})
	if _, ok := index.RuntimeFile("import/pkg/one.vim"); ok {
		t.Fatal("stale runtime root retained first file")
	}
	if got, ok := index.RuntimeFile("import/pkg/two.vim"); !ok || got != mustResolverCanonical(t, secondFile) {
		t.Fatalf("reconfigured runtime file = %q, %t", got, ok)
	}
}

func TestIndexFileAndRelationshipQueriesRejectEmptyOrUnknownKeys(t *testing.T) {
	index := NewIndex(10, 10_000)
	path := filepath.Join(t.TempDir(), "facts.vim")
	source := "vim9script\nclass Child extends Parent\nendclass\ntype Alias = Parent\ndef Target()\nenddef\ndef Caller()\n  Target()\nenddef\n"
	if err := index.Replace(path, syntax.Parse(source)); err != nil {
		t.Fatal(err)
	}
	if got := index.LookupFile(path, ""); got != nil {
		t.Fatalf("empty file lookup = %#v", got)
	}
	if got := index.FileSymbols(filepath.Join(t.TempDir(), "missing.vim")); got != nil {
		t.Fatalf("missing file symbols = %#v", got)
	}
	if got, ok := index.Source(filepath.Join(t.TempDir(), "missing.vim")); ok || got != "" {
		t.Fatalf("missing source = %q, %t", got, ok)
	}
	if index.TypeRelationCandidates("") != nil || index.TypeAliasCandidates("") != nil || index.CallCandidates("") != nil || index.ExternalReferences("") != nil || index.GlobalNameFacts("") != nil {
		t.Fatal("empty reverse-index query returned results")
	}
	if got := index.TypeRelations(SymbolKey{Path: filepath.Join(t.TempDir(), "missing.vim")}); len(got) != 0 {
		t.Fatalf("missing child relationships = %#v", got)
	}
	if got := index.Calls(SymbolKey{Path: filepath.Join(t.TempDir(), "missing.vim")}); len(got) != 0 {
		t.Fatalf("missing caller relationships = %#v", got)
	}
}

func TestPathResolverDynamicAndMissingCandidateContracts(t *testing.T) {
	root := t.TempDir()
	runtimePath := filepath.Join(root, "runtime")
	resolver, err := NewPathResolver(root, []string{runtimePath})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"name", "'unterminated", "$'interpolated'"} {
		result := resolver.ResolveImportPath(filepath.Join(root, "from.vim"), raw, false)
		if !result.Dynamic || result.Path != "" || result.Candidates != nil {
			t.Errorf("dynamic import %q = %#v", raw, result)
		}
	}
	missing := resolver.ResolveImportPath(filepath.Join(root, "from.vim"), "'not-found.vim'", false)
	if missing.Dynamic || missing.Path != "" || len(missing.Candidates) != 1 || missing.Candidates[0] != mustResolverCanonical(t, filepath.Join(runtimePath, "import", "not-found.vim")) {
		t.Fatalf("missing runtime candidate = %#v", missing)
	}
	if result := resolver.ResolveSource("", "%"); !result.Dynamic || result.Path != "" {
		t.Fatalf("dynamic source = %#v", result)
	}
	for _, prefix := range []string{"bad\\path", "bad\npath", "bad\x00path"} {
		items, truncated := resolver.ImportPathCompletions("", prefix, false, 10)
		if items != nil || truncated {
			t.Errorf("unsafe import completion %q = %#v, %t", prefix, items, truncated)
		}
	}
}
