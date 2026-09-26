package server

import (
	"cmp"
	"context"
	"slices"
	"sync"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/uri"
)

// This cache belongs to one parsed document, so edits/close release its source
// and dependency tables with the existing parser cache. No workspace-wide cache
// of closed or obsolete documents is introduced.
type importTypeCache struct {
	mu       sync.Mutex
	file     *syntax.File
	nodes    map[syntax.Span]*syntax.Import
	identity workspaceIdentity
	resolver *workspace.PathResolver
	inputs   analysis.ImportTypes
	bindings []analysis.ImportTypeBinding
	derived  map[string]derivedExportTypes
}

type derivedExportTypes struct {
	input    *workspace.ExportTypeInput
	bindings []analysis.ImportTypeBinding
	exports  *analysis.ExportTypes
}

func newImportTypeCache(file *syntax.File) *importTypeCache {
	var cache *importTypeCache
	var collect func([]syntax.Command, []syntax.Block)
	collect = func(commands []syntax.Command, blocks []syntax.Block) {
		for i := range commands {
			command := &commands[i]
			if syntax.CommandInsideFunction(command, blocks) {
				continue
			}
			if command.Import != nil {
				if cache == nil {
					cache = &importTypeCache{file: file, nodes: make(map[syntax.Span]*syntax.Import)}
				}
				cache.nodes[command.Import.PathSpan] = command.Import
			}
			if command.Embedded != nil {
				collect(command.Embedded.Commands, command.Embedded.Blocks)
			}
		}
	}
	collect(file.Commands, file.Blocks)
	return cache
}

// load derives immutable export tables from indexed syntax without reading
// files or running diagnostic analysis. Unrelated index changes revalidate
// bindings, retaining the same input identity and semantic result.
func (cache *importTypeCache) load(ctx context.Context, s *Server, documentURI string) (analysis.ImportTypes, error) {
	if cache == nil {
		return analysis.ImportTypes{}, nil
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return analysis.ImportTypes{}, err
		}
		s.workspaceMu.Lock()
		identity, resolver := s.workspaceIdentityLocked(), s.workspaceResolver
		if cache.inputs != (analysis.ImportTypes{}) && cache.identity == identity && cache.resolver == resolver {
			s.workspaceMu.Unlock()
			return cache.inputs, nil
		}
		s.workspaceMu.Unlock()
		path, valid := workspaceURIPath(uri.URI(documentURI))
		var imports []workspace.ImportFact
		var graph workspace.ImportGraphSnapshot
		var index *workspace.Index
		s.workspaceMu.Lock()
		if identity != s.workspaceIdentityLocked() || resolver != s.workspaceResolver {
			s.workspaceMu.Unlock()
			continue
		}
		if valid && s.workspaceIndex != nil {
			graph, index = s.workspaceGraphView, s.workspaceIndex
			if s.workspaceIndex.MatchesSource(path, cache.file.Source) {
				imports, _ = s.workspaceGraphView.ImportsAtCanonicalPath(path)
			}
		}
		s.workspaceMu.Unlock()
		if imports == nil && valid && resolver != nil {
			openByPath := make(map[string]*text.Snapshot)
			for _, snapshot := range s.documents.Snapshots() {
				if openPath, ok := workspaceURIPath(uri.URI(snapshot.URI())); ok && snapshot.ByteLen() <= maxFileBytes {
					openByPath[openPath] = snapshot
				}
			}
			imports = collectWorkspaceImportFacts(path, cache.file, resolver, openByPath)
		}
		starts := make([]string, 0, len(imports))
		for _, fact := range imports {
			if fact.Target != "" && !fact.Dynamic {
				starts = append(starts, fact.Target)
			}
		}
		cycleEdges, err := importCycleEdges(ctx, graph, starts)
		if err != nil {
			return analysis.ImportTypes{}, err
		}
		bindings := make([]analysis.ImportTypeBinding, 0, len(imports))
		nextDerived := make(map[string]derivedExportTypes)
		aliases := make(map[string]int, len(imports))
		for _, fact := range imports {
			aliases[fact.Alias]++
		}
		for _, fact := range imports {
			node := cache.nodes[fact.PathSpan]
			if node == nil {
				continue
			}
			binding := analysis.ImportTypeBinding{Declaration: analysis.ImportDeclarationSpan(cache.file, node)}
			if fact.Target != "" && fact.Target != path && !fact.Dynamic && aliases[fact.Alias] == 1 {
				binding.Exports = cache.deriveExportTypes(ctx, index, graph, fact.Target, nextDerived, cycleEdges)
			}
			bindings = append(bindings, binding)
		}
		slices.SortFunc(bindings, func(a, b analysis.ImportTypeBinding) int {
			return cmp.Compare(a.Declaration.Start, b.Declaration.Start)
		})
		if err := ctx.Err(); err != nil {
			return analysis.ImportTypes{}, err
		}
		s.workspaceMu.Lock()
		if identity != s.workspaceIdentityLocked() || resolver != s.workspaceResolver {
			s.workspaceMu.Unlock()
			continue
		}
		s.workspaceMu.Unlock()
		if cache.inputs == (analysis.ImportTypes{}) || !slices.Equal(bindings, cache.bindings) {
			cache.inputs = analysis.NewImportTypes(bindings)
			cache.bindings = bindings
		}
		cache.derived = nextDerived
		cache.identity, cache.resolver = identity, resolver
		return cache.inputs, nil
	}
}

// deriveExportTypes evaluates an indexed import closure from its leaves. The
// cache keeps only immutable summaries belonging to this parsed importer; a
// binding key prevents unrelated graph revisions from changing ImportTypes.
func (cache *importTypeCache) deriveExportTypes(ctx context.Context, index *workspace.Index, graph workspace.ImportGraphSnapshot, path string, next map[string]derivedExportTypes, cycleEdges map[importTypeEdge]bool) *analysis.ExportTypes {
	if index == nil || ctx.Err() != nil {
		return nil
	}
	if derived, ok := next[path]; ok {
		return derived.exports
	}
	input := index.ExportTypeInput(path)
	if input == nil {
		next[path] = derivedExportTypes{}
		return nil
	}
	imports, _ := graph.ImportsAtCanonicalPath(path)
	if len(imports) == 0 {
		exports := index.ExportTypes(path)
		next[path] = derivedExportTypes{input: input, exports: exports}
		return exports
	}
	aliases := make(map[string]int, len(imports))
	for _, fact := range imports {
		aliases[fact.Alias]++
	}
	bindings := make([]analysis.ImportTypeBinding, 0, len(imports))
	for _, fact := range imports {
		if ctx.Err() != nil {
			return nil
		}
		declaration, ok := input.ImportDeclaration(fact.PathSpan)
		if !ok {
			continue
		}
		binding := analysis.ImportTypeBinding{Declaration: declaration}
		if fact.Target != "" && !fact.Dynamic && aliases[fact.Alias] == 1 {
			if cycleEdges[importTypeEdge{path, fact.Target}] {
				binding.Exports = index.ExportTypes(fact.Target)
			} else {
				binding.Exports = cache.deriveExportTypes(ctx, index, graph, fact.Target, next, cycleEdges)
			}
		}
		bindings = append(bindings, binding)
	}
	slices.SortFunc(bindings, func(a, b analysis.ImportTypeBinding) int {
		return cmp.Compare(a.Declaration.Start, b.Declaration.Start)
	})
	if previous, ok := cache.derived[path]; ok && previous.input == input && slices.Equal(previous.bindings, bindings) {
		next[path] = previous
		return previous.exports
	}
	importsInput := analysis.NewImportTypes(bindings)
	exports := workspace.DeriveExportTypes(input, importsInput)
	next[path] = derivedExportTypes{input: input, bindings: bindings, exports: exports}
	return exports
}

type importTypeEdge struct{ from, to string }

// importCycleEdges captures the reachable closure once and labels SCC edges
// with two iterative graph walks. This avoids order-dependent cycle facts and
// repeated path searches for every import edge.
func importCycleEdges(ctx context.Context, graph workspace.ImportGraphSnapshot, starts []string) (map[importTypeEdge]bool, error) {
	adjacent := make(map[string][]string)
	reverse := make(map[string][]string)
	seen := make(map[string]bool)
	queue := append([]string(nil), starts...)
	for len(queue) != 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := queue[0]
		queue = queue[1:]
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		imports, _ := graph.ImportsAtCanonicalPath(path)
		for _, fact := range imports {
			if fact.Target == "" {
				continue
			}
			adjacent[path] = append(adjacent[path], fact.Target)
			reverse[fact.Target] = append(reverse[fact.Target], path)
			queue = append(queue, fact.Target)
		}
	}
	order := make([]string, 0, len(seen))
	visited := make(map[string]bool, len(seen))
	for start := range seen {
		if visited[start] {
			continue
		}
		stack := []struct {
			path string
			next int
		}{{path: start}}
		visited[start] = true
		for len(stack) != 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			top := &stack[len(stack)-1]
			if top.next < len(adjacent[top.path]) {
				next := adjacent[top.path][top.next]
				top.next++
				if !visited[next] {
					visited[next] = true
					stack = append(stack, struct {
						path string
						next int
					}{path: next})
				}
				continue
			}
			order = append(order, top.path)
			stack = stack[:len(stack)-1]
		}
	}
	component := make(map[string]int, len(seen))
	sizes := []int{0}
	for i := len(order) - 1; i >= 0; i-- {
		start := order[i]
		if component[start] != 0 {
			continue
		}
		id := len(sizes)
		component[start] = id
		work := []string{start}
		size := 0
		for len(work) != 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			path := work[0]
			work = work[1:]
			size++
			for _, next := range reverse[path] {
				if component[next] == 0 {
					component[next] = id
					work = append(work, next)
				}
			}
		}
		sizes = append(sizes, size)
	}
	cycles := make(map[importTypeEdge]bool)
	for from, targets := range adjacent {
		for _, to := range targets {
			if component[from] == component[to] && (sizes[component[from]] > 1 || from == to) {
				cycles[importTypeEdge{from, to}] = true
			}
		}
	}
	return cycles, nil
}

// The caller holds publishMu. No loader acquires publishMu while holding the
// cache lock, preserving the existing publication -> workspace lock order.
func (cache *importTypeCache) current(s *Server, inputs analysis.ImportTypes) bool {
	if cache == nil {
		return inputs == (analysis.ImportTypes{})
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	s.workspaceMu.Lock()
	defer s.workspaceMu.Unlock()
	return cache.inputs == inputs && cache.identity == s.workspaceIdentityLocked() && cache.resolver == s.workspaceResolver
}

func (cache *importTypeCache) consumedIdentity(inputs analysis.ImportTypes) (workspaceIdentity, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.identity, cache.inputs == inputs
}
