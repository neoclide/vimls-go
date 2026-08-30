package workspace

import (
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
)

var ErrImportGraphPath = errors.New("workspace import graph path is empty")

// ImportFact is the immutable metadata retained for one Vim9 :import.
// Target is empty for a dynamic or unresolved import; such imports are kept
// for diagnostics but do not create graph edges.
type ImportFact struct {
	Importer   string
	Target     string
	ImportPath string
	PathSpan   syntax.Span
	Alias      string
	AliasSpan  syntax.Span
	Autoload   bool
	Dynamic    bool
	Missing    bool
}

// ImportGraph is mutable build state for Vim9 import dependencies. Callers
// publish Snapshot values and never expose the graph itself to analysis.
type ImportGraph struct {
	revision uint64
	ready    bool
	files    map[string]struct{}
	imports  map[string][]ImportFact
	incoming map[string][]ImportFact
}

// ImportGraphSnapshot is an immutable view of one graph revision. Its maps and
// slices are private, and query methods return independent result slices.
type ImportGraphSnapshot struct {
	revision uint64
	ready    bool
	files    map[string]struct{}
	imports  map[string][]ImportFact
	incoming map[string][]ImportFact
}

func NewImportGraph() *ImportGraph {
	return &ImportGraph{
		files:    make(map[string]struct{}),
		imports:  make(map[string][]ImportFact),
		incoming: make(map[string][]ImportFact),
	}
}

// Replace atomically replaces importer's outgoing imports. Calling Replace
// represents a new file state and therefore advances the graph revision even
// when the retained imports are unchanged.
func (g *ImportGraph) Replace(importer string, imports []ImportFact) error {
	if g == nil {
		return ErrImportGraphPath
	}
	canonicalImporter, err := normalizeImportGraphPath(importer)
	if err != nil {
		return err
	}
	normalized := make([]ImportFact, 0, len(imports))
	for _, fact := range imports {
		fact.Importer = canonicalImporter
		fact.ImportPath = strings.Clone(fact.ImportPath)
		fact.Alias = strings.Clone(fact.Alias)
		if fact.Dynamic {
			fact.Target = ""
			fact.Missing = false
		} else if fact.Target != "" {
			fact.Target, err = normalizeImportGraphPath(fact.Target)
			if err != nil {
				return err
			}
			fact.Missing = false
		}
		normalized = append(normalized, fact)
	}
	sortImportFacts(normalized)

	for _, old := range g.imports[canonicalImporter] {
		if old.Target != "" {
			g.removeIncoming(canonicalImporter, old.Target)
		}
	}
	g.files[canonicalImporter] = struct{}{}
	g.imports[canonicalImporter] = normalized
	for _, fact := range normalized {
		if fact.Target == "" {
			continue
		}
		g.files[fact.Target] = struct{}{}
		g.incoming[fact.Target] = append(g.incoming[fact.Target], fact)
		sortImportFacts(g.incoming[fact.Target])
	}
	g.advance()
	return nil
}

// Remove deletes a file and every incident edge. Imports from other files that
// targeted the removed file are retained as unresolved static imports.
func (g *ImportGraph) Remove(path string) {
	if g == nil {
		return
	}
	canonical, err := normalizeImportGraphPath(path)
	if err != nil {
		return
	}
	if _, exists := g.files[canonical]; !exists {
		return
	}
	inbound := append([]ImportFact(nil), g.incoming[canonical]...)
	for _, fact := range g.imports[canonical] {
		if fact.Target != "" {
			g.removeIncoming(canonical, fact.Target)
		}
	}
	delete(g.imports, canonical)
	delete(g.incoming, canonical)
	delete(g.files, canonical)
	for _, edge := range inbound {
		if edge.Importer == canonical {
			continue
		}
		facts := g.imports[edge.Importer]
		for index := range facts {
			if facts[index].Target == canonical {
				facts[index].Target = ""
				facts[index].Missing = true
			}
		}
		g.imports[edge.Importer] = facts
	}
	g.advance()
}

// SetReady changes whether this graph represents a completed workspace pass.
// Dynamic and unresolved imports do not prevent a completed graph from being
// ready because they are deliberately retained as unknown.
func (g *ImportGraph) SetReady(ready bool) {
	if g == nil || g.ready == ready {
		return
	}
	g.ready = ready
	g.advance()
}

func (g *ImportGraph) Revision() uint64 {
	if g == nil {
		return 0
	}
	return g.revision
}

func (g *ImportGraph) Ready() bool {
	return g != nil && g.ready
}

// AdvanceRevision makes the next published state newer than revision. It is
// used when an off-thread graph build is committed after another graph state.
func (g *ImportGraph) AdvanceRevision(revision uint64) {
	if g == nil || g.revision > revision {
		return
	}
	if revision == math.MaxUint64 {
		g.revision = math.MaxUint64
	} else {
		g.revision = revision + 1
	}
}

// Snapshot returns an immutable copy of the current graph state.
func (g *ImportGraph) Snapshot() ImportGraphSnapshot {
	if g == nil {
		return ImportGraphSnapshot{}
	}
	return ImportGraphSnapshot{
		revision: g.revision,
		ready:    g.ready,
		files:    cloneImportFiles(g.files),
		imports:  cloneImportFactMap(g.imports),
		incoming: cloneImportFactMap(g.incoming),
	}
}

func (s ImportGraphSnapshot) Revision() uint64 { return s.revision }

func (s ImportGraphSnapshot) Ready() bool { return s.ready }

func (s ImportGraphSnapshot) Has(path string) bool {
	canonical, err := normalizeImportGraphPath(path)
	if err != nil {
		return false
	}
	_, ok := s.files[canonical]
	return ok
}

// Imports returns all retained imports for path, including dynamic and
// unresolved imports that do not appear in Outgoing.
func (s ImportGraphSnapshot) Imports(path string) []ImportFact {
	canonical, err := normalizeImportGraphPath(path)
	if err != nil {
		return nil
	}
	return cloneImportFacts(s.imports[canonical])
}

func (s ImportGraphSnapshot) Outgoing(path string) []ImportFact {
	imports := s.Imports(path)
	result := imports[:0]
	for _, fact := range imports {
		if fact.Target != "" {
			result = append(result, fact)
		}
	}
	return result
}

func (s ImportGraphSnapshot) Incoming(path string) []ImportFact {
	canonical, err := normalizeImportGraphPath(path)
	if err != nil {
		return nil
	}
	return cloneImportFacts(s.incoming[canonical])
}

// ReverseDependents returns the unique canonical importers of path.
func (s ImportGraphSnapshot) ReverseDependents(path string) []string {
	incoming := s.Incoming(path)
	result := make([]string, 0, len(incoming))
	previous := ""
	for _, fact := range incoming {
		if fact.Importer == previous {
			continue
		}
		previous = fact.Importer
		result = append(result, fact.Importer)
	}
	return result
}

func (g *ImportGraph) removeIncoming(importer, target string) {
	facts := g.incoming[target]
	kept := facts[:0]
	for _, fact := range facts {
		if fact.Importer != importer {
			kept = append(kept, fact)
		}
	}
	if len(kept) == 0 {
		delete(g.incoming, target)
		return
	}
	g.incoming[target] = kept
}

func (g *ImportGraph) advance() {
	if g.revision < math.MaxUint64 {
		g.revision++
	}
}

func normalizeImportGraphPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", ErrImportGraphPath
	}
	return CanonicalPath(path)
}

func sortImportFacts(facts []ImportFact) {
	sort.SliceStable(facts, func(left, right int) bool {
		if facts[left].Importer != facts[right].Importer {
			return facts[left].Importer < facts[right].Importer
		}
		if facts[left].PathSpan != facts[right].PathSpan {
			if facts[left].PathSpan.Start != facts[right].PathSpan.Start {
				return facts[left].PathSpan.Start < facts[right].PathSpan.Start
			}
			return facts[left].PathSpan.End < facts[right].PathSpan.End
		}
		if facts[left].Target != facts[right].Target {
			return facts[left].Target < facts[right].Target
		}
		return facts[left].Alias < facts[right].Alias
	})
}

func cloneImportFiles(files map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{}, len(files))
	for path := range files {
		result[path] = struct{}{}
	}
	return result
}

func cloneImportFactMap(values map[string][]ImportFact) map[string][]ImportFact {
	result := make(map[string][]ImportFact, len(values))
	for path, facts := range values {
		result[path] = cloneImportFacts(facts)
	}
	return result
}

func cloneImportFacts(facts []ImportFact) []ImportFact {
	if len(facts) == 0 {
		return nil
	}
	result := make([]ImportFact, len(facts))
	copy(result, facts)
	return result
}
