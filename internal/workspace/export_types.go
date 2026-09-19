package workspace

import (
	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
)

// MatchesSource compares a source already identified by a canonical index path.
// Import-input capture has canonicalized the URI and must not repeat filesystem
// canonicalization for each query into the same workspace snapshot.
func (i *Index) MatchesSource(canonicalPath, source string) bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	file, ok := i.files[canonicalPath]
	return ok && file.source == source
}

// ExportTypes returns immutable file-local facts for a canonical index path. It does not parse source or
// copy member tables; the identity survives CopyFileFrom during index rebuilds.
func (i *Index) ExportTypes(path string) *analysis.ExportTypes {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.files[path].exportTypes
}

func collectExportTypes(file *syntax.File, result *analysis.FileAnalysis, facts []SymbolFact) *analysis.ExportTypes {
	if file.Dialect != syntax.Vim9 || len(file.Diagnostics) != 0 {
		return nil
	}
	var eligible map[syntax.Span]*SymbolFact
	for index := range facts {
		fact := &facts[index]
		if !fact.TopLevel || !fact.Exported || fact.Kind != analysis.SymbolKindVariable && fact.Kind != analysis.SymbolKindConstant {
			continue
		}
		if eligible == nil {
			eligible = make(map[syntax.Span]*SymbolFact)
		}
		eligible[fact.SelectionRange] = fact
	}
	if len(eligible) == 0 {
		return nil
	}
	members := make(map[string]analysis.StaticType, len(eligible))
	for _, declaration := range result.Declarations {
		if fact := eligible[declaration.Span]; fact != nil {
			fact.StaticType = analysis.FreezeType(declaration.Type)
			members[fact.Name] = fact.StaticType
		}
	}
	// Count only eligible names; storage is bounded by exports, not locals.
	seen := make(map[string]bool, len(members))
	for _, fact := range facts {
		if _, eligible := members[fact.Name]; fact.TopLevel && eligible {
			if seen[fact.Name] {
				delete(members, fact.Name)
			}
			seen[fact.Name] = true
		}
	}
	return analysis.NewExportTypes(members)
}
