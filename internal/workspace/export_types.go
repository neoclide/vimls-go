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

// ExportTypeInput is the immutable, file-local input needed to derive export
// types after dependency exports have been resolved. It retains the existing
// parsed tree so foreground dependency resolution neither reads nor reparses.
type ExportTypeInput struct {
	path    string
	file    *syntax.File
	exports map[syntax.Span]string
	named   map[syntax.Span]bool
	imports map[syntax.Span]syntax.Span
}

func (i *Index) ExportTypeInput(path string) *ExportTypeInput {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.files[path].exportTypeInput
}

func newExportTypeInput(path string, file *syntax.File, facts []SymbolFact) *ExportTypeInput {
	if file == nil || file.Dialect != syntax.Vim9 || len(file.Diagnostics) != 0 {
		return nil
	}
	exports := make(map[syntax.Span]string)
	named := make(map[syntax.Span]bool)
	names := make(map[string]int)
	for _, fact := range facts {
		if fact.TopLevel {
			names[fact.Name]++
		}
	}
	hasExportedNamed := false
	for _, fact := range facts {
		if !fact.TopLevel || names[fact.Name] != 1 {
			continue
		}
		if fact.Exported && (fact.Kind == analysis.SymbolKindVariable || fact.Kind == analysis.SymbolKindConstant || fact.Kind == analysis.SymbolKindFunction) {
			exports[fact.SelectionRange] = fact.Name
		}
		if fact.Kind == analysis.SymbolKindClass || fact.Kind == analysis.SymbolKindInterface || fact.Kind == analysis.SymbolKindEnum || fact.Kind == analysis.SymbolKindTypeAlias {
			named[fact.SelectionRange] = fact.Exported
			hasExportedNamed = hasExportedNamed || fact.Exported
		}
	}
	if len(exports) == 0 && !hasExportedNamed {
		return nil
	}
	imports := make(map[syntax.Span]syntax.Span)
	var collect func([]syntax.Command, []syntax.Block)
	collect = func(commands []syntax.Command, blocks []syntax.Block) {
		for index := range commands {
			command := &commands[index]
			if syntax.CommandInsideFunction(command, blocks) {
				continue
			}
			if command.Import != nil {
				imports[command.Import.PathSpan] = analysis.ImportDeclarationSpan(file, command.Import)
			}
			if command.Embedded != nil {
				collect(command.Embedded.Commands, command.Embedded.Blocks)
			}
		}
	}
	collect(file.Commands, file.Blocks)
	return &ExportTypeInput{path: path, file: file, exports: exports, named: named, imports: imports}
}

func (input *ExportTypeInput) ImportDeclaration(pathSpan syntax.Span) (syntax.Span, bool) {
	if input == nil {
		return syntax.Span{}, false
	}
	span, ok := input.imports[pathSpan]
	return span, ok
}

// DeriveExportTypes applies resolved dependency inputs to one indexed file.
// It uses completion's request-owned inference rather than publishing inferred
// import results back into the local source index.
func DeriveExportTypes(input *ExportTypeInput, imports analysis.ImportTypes) *analysis.ExportTypes {
	if input == nil {
		return nil
	}
	facts := analysis.CollectCompletionFactsWithOptions(input.file, analysis.Options{Imports: imports, SourceIdentity: input.path})
	query := analysis.NewCompletionTypes(facts)
	members := make(map[string]analysis.StaticType, len(input.exports))
	named := make(map[string]analysis.NamedType, len(input.named))
	for _, declaration := range facts.Declarations {
		name, isExport := input.exports[declaration.Span]
		exported, isNamed := input.named[declaration.Span]
		if !isExport && !isNamed {
			continue
		}
		typ := query.DeclarationType(declaration)
		if isExport {
			members[name] = analysis.FreezeDerivedType(typ)
		}
		if isNamed {
			named[declaration.Name] = analysis.NamedType{Type: analysis.FreezeDerivedType(typ), Exported: exported}
		}
	}
	return analysis.NewExportTypes(members, named)
}

func collectExportTypes(input *ExportTypeInput, facts []SymbolFact) *analysis.ExportTypes {
	if input == nil {
		return nil
	}
	// Resolve local identities from canonical source without import inputs.
	// This keeps cold and enriched indexes independent of dependency state.
	completion := analysis.CollectCompletionFactsWithOptions(input.file, analysis.Options{SourceIdentity: input.path})
	query := analysis.NewCompletionTypes(completion)
	members := make(map[string]analysis.StaticType, len(input.exports))
	named := make(map[string]analysis.NamedType, len(input.named))
	for _, declaration := range completion.Declarations {
		name, isExport := input.exports[declaration.Span]
		exported, isNamed := input.named[declaration.Span]
		if !isExport && !isNamed {
			continue
		}
		typ := analysis.FreezeType(query.DeclarationType(declaration))
		if isExport {
			members[name] = typ
		}
		if isNamed {
			named[declaration.Name] = analysis.NamedType{Type: typ, Exported: exported}
		}
	}
	for index := range facts {
		if name, ok := input.exports[facts[index].SelectionRange]; ok {
			facts[index].StaticType = members[name]
		}
	}
	return analysis.NewExportTypes(members, named)
}
