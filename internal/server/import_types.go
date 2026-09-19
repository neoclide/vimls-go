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
			// Autoload compilation does not establish that the target is loaded.
			// Keep its diagnostic inference unknown in this first increment.
			if command.Import != nil && !command.Import.Autoload {
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

// load shares immutable indexed tables and never analyzes or reads a target's
// source. Unrelated index changes revalidate bindings, retaining the same input
// identity and semantic result. The steady-state hit allocates nothing.
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
		s.workspaceMu.Lock()
		if identity != s.workspaceIdentityLocked() || resolver != s.workspaceResolver {
			s.workspaceMu.Unlock()
			continue
		}
		if valid && s.workspaceIndex != nil {
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
		s.workspaceMu.Lock()
		if identity != s.workspaceIdentityLocked() || resolver != s.workspaceResolver {
			s.workspaceMu.Unlock()
			continue
		}
		bindings := make([]analysis.ImportTypeBinding, 0, len(imports))
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
				binding.Exports = s.workspaceIndex.ExportTypes(fact.Target)
			}
			bindings = append(bindings, binding)
		}
		s.workspaceMu.Unlock()
		slices.SortFunc(bindings, func(a, b analysis.ImportTypeBinding) int {
			return cmp.Compare(a.Declaration.Start, b.Declaration.Start)
		})
		if cache.inputs == (analysis.ImportTypes{}) || !slices.Equal(bindings, cache.bindings) {
			cache.inputs = analysis.NewImportTypes(bindings)
			cache.bindings = bindings
		}
		cache.identity, cache.resolver = identity, resolver
		return cache.inputs, nil
	}
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
