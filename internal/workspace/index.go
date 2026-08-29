package workspace

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/chemzqm/vimls-go/internal/analysis"
	"github.com/chemzqm/vimls-go/internal/syntax"
)

var (
	ErrIndexLimit       = errors.New("workspace symbol index limit exceeded")
	ErrIndexInvalidPath = errors.New("workspace symbol index path is empty")
	ErrIndexNilFile     = errors.New("workspace symbol index file is nil")
)

// SymbolFact is the immutable, protocol-independent part of a document
// symbol needed by workspace lookup. It contains no pointer into a syntax
// tree and no source text.
type SymbolFact struct {
	Path           string
	Name           string
	Kind           analysis.SymbolKind
	Range          syntax.Span
	SelectionRange syntax.Span
	Detail         string
}

// SymbolMatch is an immutable workspace symbol match. Source is the complete
// source retained for the indexed file containing Fact.
type SymbolMatch struct {
	Fact   SymbolFact
	Source string
}

type indexedFile struct {
	bytes  int
	source string
	facts  []SymbolFact
}

// Index stores symbols from a bounded set of syntax files. All methods are
// safe for concurrent use. A non-positive limit means that limit is disabled.
type Index struct {
	mu       sync.RWMutex
	maxFiles int
	maxBytes int
	bytes    int
	files    map[string]indexedFile
	byName   map[string][]SymbolFact
}

// NewIndex creates a workspace symbol index with file-count and source-byte
// limits. The source byte count is len(file.Source), and the source is retained
// once for each indexed file.
func NewIndex(maxFiles, maxBytes int) *Index {
	return &Index{
		maxFiles: maxFiles,
		maxBytes: maxBytes,
		files:    make(map[string]indexedFile),
		byName:   make(map[string][]SymbolFact),
	}
}

// Replace atomically replaces path's symbols. If either configured limit would
// be exceeded, ErrIndexLimit is returned and the previous entry is unchanged.
func (i *Index) Replace(path string, file *syntax.File) error {
	if file == nil {
		return ErrIndexNilFile
	}
	normalized, err := normalizeIndexPath(path)
	if err != nil {
		return err
	}
	facts := make([]SymbolFact, 0)
	collectSymbolFacts(normalized, analysis.CollectSymbols(file), &facts)
	sortFacts(facts)
	indexed := indexedFile{bytes: len(file.Source), source: strings.Clone(file.Source), facts: facts}

	i.mu.Lock()
	defer i.mu.Unlock()
	old, exists := i.files[normalized]
	fileCount := len(i.files)
	if !exists {
		fileCount++
	}
	indexedBytes := i.bytes
	if exists {
		indexedBytes -= old.bytes
	}
	indexedBytes += indexed.bytes
	if (i.maxFiles > 0 && fileCount > i.maxFiles) || (i.maxBytes > 0 && indexedBytes > i.maxBytes) {
		return ErrIndexLimit
	}
	if exists {
		i.removeFactsLocked(old.facts)
	}
	i.files[normalized] = indexed
	i.bytes = indexedBytes
	for _, fact := range facts {
		i.byName[fact.Name] = append(i.byName[fact.Name], fact)
	}
	for _, name := range namesIn(facts) {
		sortFacts(i.byName[name])
	}
	return nil
}

// Remove deletes path from the index. Invalid or absent paths are ignored.
func (i *Index) Remove(path string) {
	normalized, err := normalizeIndexPath(path)
	if err != nil {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	old, ok := i.files[normalized]
	if !ok {
		return
	}
	i.removeFactsLocked(old.facts)
	delete(i.files, normalized)
	i.bytes -= old.bytes
}

// Lookup returns exact-name matches sorted by path and source span. The
// returned slice is independent of the index and can be freely modified.
func (i *Index) Lookup(name string) []SymbolFact {
	if name == "" {
		return nil
	}
	i.mu.RLock()
	facts := append([]SymbolFact(nil), i.byName[name]...)
	i.mu.RUnlock()
	return facts
}

// Search returns symbols whose names contain query as a case-insensitive
// ordered subsequence. Exact matches rank before prefixes, followed by other
// subsequence matches; ties use the index's stable fact ordering. An empty
// query matches every symbol. A positive limit caps the result count.
func (i *Index) Search(query string, limit int) []SymbolMatch {
	queryFolded := strings.ToLower(query)
	i.mu.RLock()
	matches := make([]SymbolMatch, 0)
	for _, file := range i.files {
		for _, fact := range file.facts {
			_, ok := searchRank(query, queryFolded, fact.Name)
			if !ok {
				continue
			}
			matches = append(matches, SymbolMatch{Fact: fact, Source: file.source})
		}
	}
	i.mu.RUnlock()
	sort.SliceStable(matches, func(left, right int) bool {
		leftRank, _ := searchRank(query, queryFolded, matches[left].Fact.Name)
		rightRank, _ := searchRank(query, queryFolded, matches[right].Fact.Name)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return factLess(matches[left].Fact, matches[right].Fact)
	})
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

func searchRank(query, foldedQuery, name string) (int, bool) {
	if query == "" {
		return 0, true
	}
	foldedName := strings.ToLower(name)
	if foldedName == foldedQuery {
		return 0, true
	}
	if strings.HasPrefix(foldedName, foldedQuery) {
		return 1, true
	}
	queryRunes := []rune(foldedQuery)
	if len(queryRunes) == 0 {
		return 0, true
	}
	queryIndex := 0
	for _, nameRune := range []rune(foldedName) {
		if nameRune == queryRunes[queryIndex] {
			queryIndex++
			if queryIndex == len(queryRunes) {
				return 2, true
			}
		}
	}
	return 0, false
}

// FileCount reports the number of indexed paths.
func (i *Index) FileCount() int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.files)
}

// IndexedBytes reports the source bytes charged against the index limits.
func (i *Index) IndexedBytes() int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.bytes
}

func normalizeIndexPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", ErrIndexInvalidPath
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func collectSymbolFacts(path string, symbols []*analysis.Symbol, facts *[]SymbolFact) {
	for _, symbol := range symbols {
		if symbol == nil || symbol.Name == "" {
			continue
		}
		*facts = append(*facts, SymbolFact{
			Path:           path,
			Name:           strings.Clone(symbol.Name),
			Kind:           symbol.Kind,
			Range:          symbol.Range,
			SelectionRange: symbol.SelectionRange,
			Detail:         strings.Clone(symbol.Detail),
		})
		collectSymbolFacts(path, symbol.Children, facts)
	}
}

func sortFacts(facts []SymbolFact) {
	sort.SliceStable(facts, func(left, right int) bool {
		return factLess(facts[left], facts[right])
	})
}

func factLess(left, right SymbolFact) bool {
	if left.Path != right.Path {
		return left.Path < right.Path
	}
	if left.SelectionRange.Start != right.SelectionRange.Start {
		return left.SelectionRange.Start < right.SelectionRange.Start
	}
	if left.SelectionRange.End != right.SelectionRange.End {
		return left.SelectionRange.End < right.SelectionRange.End
	}
	if left.Range.Start != right.Range.Start {
		return left.Range.Start < right.Range.Start
	}
	if left.Range.End != right.Range.End {
		return left.Range.End < right.Range.End
	}
	if left.Name != right.Name {
		return left.Name < right.Name
	}
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	return left.Detail < right.Detail
}

func namesIn(facts []SymbolFact) []string {
	seen := make(map[string]struct{}, len(facts))
	names := make([]string, 0, len(facts))
	for _, fact := range facts {
		if _, ok := seen[fact.Name]; ok {
			continue
		}
		seen[fact.Name] = struct{}{}
		names = append(names, fact.Name)
	}
	return names
}

func (i *Index) removeFactsLocked(facts []SymbolFact) {
	for _, fact := range facts {
		matches := i.byName[fact.Name]
		for index, candidate := range matches {
			if candidate.Path == fact.Path && candidate.Name == fact.Name && candidate.Kind == fact.Kind && candidate.Range == fact.Range && candidate.SelectionRange == fact.SelectionRange && candidate.Detail == fact.Detail {
				i.byName[fact.Name] = append(matches[:index], matches[index+1:]...)
				if len(i.byName[fact.Name]) == 0 {
					delete(i.byName, fact.Name)
				}
				break
			}
		}
	}
}
