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
	Exported       bool
}

// SymbolMatch is an immutable workspace symbol match. Source is the complete
// source retained for the indexed file containing Fact.
type SymbolMatch struct {
	Fact   SymbolFact
	Source string
}

type ExternalReferenceKind uint8

const (
	ExternalReferenceImportMember ExternalReferenceKind = iota + 1
	ExternalReferenceAutoload
)

// ExternalReferenceFact is a statically provable cross-file reference. Import
// members retain the literal import spelling needed by PathResolver. Autoload
// names are stored without an optional g: prefix.
type ExternalReferenceFact struct {
	Path           string
	Name           string
	Span           syntax.Span
	Kind           ExternalReferenceKind
	ImportPath     string
	ImportAutoload bool
}

type ExternalReferenceMatch struct {
	Fact   ExternalReferenceFact
	Source string
}

type indexedFile struct {
	bytes      int
	source     string
	facts      []SymbolFact
	references []ExternalReferenceFact
}

// Index stores symbols from a bounded set of syntax files. All methods are
// safe for concurrent use. A non-positive limit means that limit is disabled.
type Index struct {
	mu             sync.RWMutex
	maxFiles       int
	maxBytes       int
	bytes          int
	revision       uint64
	files          map[string]indexedFile
	byName         map[string][]SymbolFact
	byExternalName map[string][]ExternalReferenceFact
}

// NewIndex creates a workspace symbol index with file-count and source-byte
// limits. The source byte count is len(file.Source), and the source is retained
// once for each indexed file.
func NewIndex(maxFiles, maxBytes int) *Index {
	return &Index{
		maxFiles:       maxFiles,
		maxBytes:       maxBytes,
		files:          make(map[string]indexedFile),
		byName:         make(map[string][]SymbolFact),
		byExternalName: make(map[string][]ExternalReferenceFact),
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
	facts := CollectSymbolFacts(normalized, file)
	sortFacts(facts)
	references := CollectExternalReferences(normalized, file)
	indexed := indexedFile{bytes: len(file.Source), source: strings.Clone(file.Source), facts: facts, references: references}

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
		i.removeExternalReferencesLocked(old.references)
	}
	i.files[normalized] = indexed
	i.bytes = indexedBytes
	for _, fact := range facts {
		i.byName[fact.Name] = append(i.byName[fact.Name], fact)
	}
	for _, name := range namesIn(facts) {
		sortFacts(i.byName[name])
	}
	for _, reference := range references {
		i.byExternalName[reference.Name] = append(i.byExternalName[reference.Name], reference)
	}
	for _, name := range referenceNamesIn(references) {
		sortExternalReferences(i.byExternalName[name])
	}
	i.revision++
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
	i.removeExternalReferencesLocked(old.references)
	delete(i.files, normalized)
	i.bytes -= old.bytes
	i.revision++
}

// LookupFile returns exact-name symbols from one indexed file. Results retain
// the immutable source needed to convert their byte spans at the LSP boundary.
func (i *Index) LookupFile(path, name string) []SymbolMatch {
	if name == "" {
		return nil
	}
	all := i.FileSymbols(path)
	matches := make([]SymbolMatch, 0)
	for _, match := range all {
		if match.Fact.Name == name {
			matches = append(matches, match)
		}
	}
	return matches
}

// FileSymbols returns every symbol from one indexed file in source order.
func (i *Index) FileSymbols(path string) []SymbolMatch {
	normalized, err := normalizeIndexPath(path)
	if err != nil {
		return nil
	}
	i.mu.RLock()
	file, ok := i.files[normalized]
	if !ok {
		i.mu.RUnlock()
		return nil
	}
	matches := make([]SymbolMatch, 0, len(file.facts))
	for _, fact := range file.facts {
		matches = append(matches, SymbolMatch{Fact: fact, Source: file.source})
	}
	i.mu.RUnlock()
	return matches
}

// Source returns the immutable source retained for an indexed file. The
// returned string is safe to keep after a later Replace or Remove.
func (i *Index) Source(path string) (string, bool) {
	normalized, err := normalizeIndexPath(path)
	if err != nil {
		return "", false
	}
	i.mu.RLock()
	file, ok := i.files[normalized]
	i.mu.RUnlock()
	if !ok {
		return "", false
	}
	return file.source, true
}

// Revision changes after every successful Replace or effective Remove.
func (i *Index) Revision() uint64 {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.revision
}

// ExternalReferences returns immutable candidate references in path/span
// order. The caller must still resolve each import or autoload path and reject
// candidates that do not identify its target declaration.
func (i *Index) ExternalReferences(name string) []ExternalReferenceMatch {
	if name == "" {
		return nil
	}
	i.mu.RLock()
	facts := i.byExternalName[name]
	result := make([]ExternalReferenceMatch, 0, len(facts))
	for _, fact := range facts {
		if file, ok := i.files[fact.Path]; ok {
			result = append(result, ExternalReferenceMatch{Fact: fact, Source: file.source})
		}
	}
	i.mu.RUnlock()
	return result
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

// CollectSymbolFacts returns immutable symbol facts for file. Exported is set
// only when the declaration-bearing top-level command has an export modifier.
func CollectSymbolFacts(path string, file *syntax.File) []SymbolFact {
	normalized, err := normalizeIndexPath(path)
	if err != nil || file == nil {
		return nil
	}
	exported := exportedSymbolSpans(file)
	facts := make([]SymbolFact, 0)
	collectSymbolFacts(normalized, analysis.CollectSymbols(file), exported, &facts)
	sortFacts(facts)
	return facts
}

func collectSymbolFacts(path string, symbols []*analysis.Symbol, exported map[syntax.Span]bool, facts *[]SymbolFact) {
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
			Exported:       exported[symbol.SelectionRange],
		})
		collectSymbolFacts(path, symbol.Children, exported, facts)
	}
}

func exportedSymbolSpans(file *syntax.File) map[syntax.Span]bool {
	result := make(map[syntax.Span]bool)
	if file == nil {
		return result
	}
	for index := range file.Commands {
		command := &file.Commands[index]
		if !commandHasModifier(command, "export") {
			continue
		}
		if command.Function != nil {
			result[command.Function.Name] = true
		}
		if command.Aggregate != nil {
			result[command.Aggregate.Name] = true
		}
		if command.TypeAlias != nil {
			result[command.TypeAlias.Name] = true
		}
		if command.Declaration != nil {
			for _, binding := range command.Declaration.Bindings {
				result[binding.Name] = true
			}
		}
	}
	return result
}

func commandHasModifier(command *syntax.Command, name string) bool {
	if command == nil {
		return false
	}
	for _, modifier := range command.Modifiers {
		if modifier.Name == name {
			return true
		}
	}
	return false
}

// CollectExternalReferences returns only references whose target can be
// decided later without executing Vimscript: a direct member of a proven
// import alias, or an unresolved legacy autoload name containing '#'.
func CollectExternalReferences(path string, file *syntax.File) []ExternalReferenceFact {
	return CollectExternalReferencesFromAnalysis(path, file, analysis.Analyze(file))
}

// CollectExternalReferencesFromAnalysis reuses an analysis result belonging
// to file. Callers that already analyzed an open snapshot avoid repeating the
// semantic pass used to prove import receivers and unresolved autoload names.
func CollectExternalReferencesFromAnalysis(path string, file *syntax.File, result *analysis.FileAnalysis) []ExternalReferenceFact {
	normalized, err := normalizeIndexPath(path)
	if err != nil || file == nil || result == nil || result.File != file {
		return nil
	}
	references := make(map[syntax.Span]*analysis.Reference, len(result.References))
	for _, reference := range result.References {
		if reference != nil {
			references[reference.Span] = reference
		}
	}
	imports := make(map[syntax.Span]*syntax.Import)
	var importNodes []*syntax.Import
	collectImports(file.Commands, imports, &importNodes)
	importsByName := make(map[string][]*syntax.Import)
	for _, importNode := range importNodes {
		name := file.Text(importNode.Alias)
		if name == "" {
			name = defaultImportName(file.Text(importNode.PathSpan))
		}
		if name != "" {
			importsByName[name] = append(importsByName[name], importNode)
		}
	}

	facts := make([]ExternalReferenceFact, 0)
	visited := make(map[*syntax.Expression]bool)
	visitIndexCommands(file.Commands, visited, func(expression *syntax.Expression) {
		if expression == nil || expression.Kind != syntax.ExpressionMember || len(expression.Children) != 1 || expression.Value == "" {
			return
		}
		receiver := expression.Children[0]
		if receiver == nil || receiver.Kind != syntax.ExpressionIdentifier {
			return
		}
		alias := strings.TrimPrefix(receiver.Value, "s:")
		importCandidates := importsByName[alias]
		if len(importCandidates) != 1 {
			return
		}
		reference := references[receiver.Span]
		var importNode *syntax.Import
		if reference != nil && reference.Declaration != nil {
			if reference.Declaration.Kind != analysis.SymbolKindImport {
				return
			}
			importNode = imports[reference.Declaration.Span]
			if importNode != importCandidates[0] {
				return
			}
		} else {
			importNode = importCandidates[0]
		}
		member := syntax.Span{Start: expression.Operator.End, End: expression.Span.End}
		if importNode == nil || !validIndexSpan(file, member) || file.Text(member) != expression.Value || !validIndexSpan(file, importNode.PathSpan) {
			return
		}
		facts = append(facts, ExternalReferenceFact{
			Path:           normalized,
			Name:           strings.Clone(expression.Value),
			Span:           member,
			Kind:           ExternalReferenceImportMember,
			ImportPath:     strings.Clone(file.Text(importNode.PathSpan)),
			ImportAutoload: importNode.Autoload,
		})
	})
	visitedTypes := make(map[*syntax.Type]bool)
	visitIndexCommandTypes(file, file.Commands, visitedTypes, func(typeNode *syntax.Type) {
		if typeNode == nil || typeNode.Name == "" {
			return
		}
		separator := strings.IndexByte(typeNode.Name, '.')
		if separator <= 0 || separator == len(typeNode.Name)-1 {
			return
		}
		alias := typeNode.Name[:separator]
		importNodes := importsByName[alias]
		member := syntax.Span{Start: typeNode.Span.Start + separator + 1, End: typeNode.Span.Start + len(typeNode.Name)}
		if len(importNodes) != 1 || !validIndexSpan(file, member) || file.Text(member) != typeNode.Name[separator+1:] || !validIndexSpan(file, importNodes[0].PathSpan) {
			return
		}
		facts = append(facts, ExternalReferenceFact{
			Path:           normalized,
			Name:           strings.Clone(typeNode.Name[separator+1:]),
			Span:           member,
			Kind:           ExternalReferenceImportMember,
			ImportPath:     strings.Clone(file.Text(importNodes[0].PathSpan)),
			ImportAutoload: importNodes[0].Autoload,
		})
	})
	for _, reference := range result.References {
		if reference == nil || reference.Declaration != nil {
			continue
		}
		name := strings.TrimPrefix(reference.Name, "g:")
		separator := strings.LastIndexByte(name, '#')
		if separator <= 0 || separator == len(name)-1 || !validIndexSpan(file, reference.Span) {
			continue
		}
		facts = append(facts, ExternalReferenceFact{
			Path: normalized, Name: strings.Clone(name), Span: reference.Span, Kind: ExternalReferenceAutoload,
		})
	}
	sortExternalReferences(facts)
	return deduplicateExternalReferences(facts)
}

func collectImports(commands []syntax.Command, imports map[syntax.Span]*syntax.Import, all *[]*syntax.Import) {
	for index := range commands {
		command := &commands[index]
		if command.Import != nil {
			*all = append(*all, command.Import)
			if command.Import.Alias.Start < command.Import.Alias.End {
				imports[command.Import.Alias] = command.Import
			}
		}
		if command.Embedded != nil {
			collectImports(command.Embedded.Commands, imports, all)
		}
	}
}

func defaultImportName(raw string) string {
	spec, ok := decodeStaticPath(raw)
	if !ok {
		return ""
	}
	name := filepath.Base(filepath.FromSlash(spec))
	extension := strings.Index(name, ".vim")
	if extension <= 0 || extension+4 != len(name) {
		return ""
	}
	return name[:extension]
}

func visitIndexCommands(commands []syntax.Command, visited map[*syntax.Expression]bool, visit func(*syntax.Expression)) {
	for index := range commands {
		command := &commands[index]
		for _, expression := range command.Expressions {
			visitIndexExpression(expression, visited, visit)
		}
		for _, expression := range command.Targets {
			visitIndexExpression(expression, visited, visit)
		}
		if command.Mapping != nil {
			visitIndexExpression(command.Mapping.RHSExpression, visited, visit)
		}
		if command.Declaration != nil {
			visitIndexExpression(command.Declaration.Initializer, visited, visit)
		}
		if command.For != nil {
			visitIndexExpression(command.For.Iterable, visited, visit)
		}
		if command.Import != nil {
			visitIndexExpression(command.Import.Path, visited, visit)
		}
		if command.Function != nil {
			for _, parameter := range command.Function.Parameters {
				visitIndexExpression(parameter.Default, visited, visit)
			}
		}
		for _, value := range command.EnumValues {
			visitIndexExpression(value.Initializer, visited, visit)
			for _, argument := range value.Arguments {
				visitIndexExpression(argument, visited, visit)
			}
		}
		if command.Embedded != nil {
			visitIndexCommands(command.Embedded.Commands, visited, visit)
		}
	}
}

func visitIndexExpression(expression *syntax.Expression, visited map[*syntax.Expression]bool, visit func(*syntax.Expression)) {
	if expression == nil || visited[expression] {
		return
	}
	visited[expression] = true
	visit(expression)
	for _, child := range expression.Children {
		visitIndexExpression(child, visited, visit)
	}
	if expression.LambdaBody != nil {
		visitIndexCommands(expression.LambdaBody.Commands, visited, visit)
	}
}

func visitIndexCommandTypes(file *syntax.File, commands []syntax.Command, visited map[*syntax.Type]bool, visit func(*syntax.Type)) {
	visitedExpressions := make(map[*syntax.Expression]bool)
	visitIndexCommands(commands, visitedExpressions, func(expression *syntax.Expression) {
		visitIndexType(expression.CastType, visited, visit)
		for _, typeArgument := range expression.TypeArguments {
			visitIndexType(typeArgument, visited, visit)
		}
		for _, parameter := range expression.Parameters {
			visitIndexType(parameter.Type, visited, visit)
		}
		visitIndexType(expression.ReturnType, visited, visit)
		if expression.LambdaBody != nil {
			visitIndexCommandTypes(file, expression.LambdaBody.Commands, visited, visit)
		}
	})
	for index := range commands {
		command := &commands[index]
		if command.Declaration != nil {
			for _, binding := range command.Declaration.Bindings {
				visitIndexType(binding.ParsedType, visited, visit)
			}
		}
		if command.For != nil {
			for _, binding := range command.For.Bindings {
				visitIndexType(binding.ParsedType, visited, visit)
			}
		}
		if command.Function != nil {
			for _, parameter := range command.Function.Parameters {
				visitIndexType(parameter.Type, visited, visit)
			}
			visitIndexType(command.Function.ReturnType, visited, visit)
		}
		if command.TypeAlias != nil {
			visitIndexType(command.TypeAlias.Type, visited, visit)
		}
		if command.Aggregate != nil {
			for _, span := range append(append([]syntax.Span(nil), command.Aggregate.Extends...), command.Aggregate.Implements...) {
				if !validIndexSpan(file, span) {
					continue
				}
				name := file.Text(span)
				separator := strings.IndexByte(name, '.')
				if separator > 0 && separator < len(name)-1 {
					visit(&syntax.Type{Kind: syntax.TypeNamed, Span: span, Name: name})
				}
			}
		}
		if command.Embedded != nil {
			visitIndexCommandTypes(file, command.Embedded.Commands, visited, visit)
		}
	}
}

func visitIndexType(typeNode *syntax.Type, visited map[*syntax.Type]bool, visit func(*syntax.Type)) {
	if typeNode == nil || visited[typeNode] {
		return
	}
	visited[typeNode] = true
	visit(typeNode)
	for _, argument := range typeNode.Arguments {
		visitIndexType(argument, visited, visit)
	}
	visitIndexType(typeNode.ReturnType, visited, visit)
}

func validIndexSpan(file *syntax.File, span syntax.Span) bool {
	return file != nil && span.Start >= 0 && span.Start < span.End && span.End <= len(file.Source)
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
	if left.Exported != right.Exported {
		return !left.Exported
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

func referenceNamesIn(facts []ExternalReferenceFact) []string {
	seen := make(map[string]struct{}, len(facts))
	result := make([]string, 0, len(facts))
	for _, fact := range facts {
		if _, ok := seen[fact.Name]; ok {
			continue
		}
		seen[fact.Name] = struct{}{}
		result = append(result, fact.Name)
	}
	return result
}

func sortExternalReferences(facts []ExternalReferenceFact) {
	sort.SliceStable(facts, func(left, right int) bool {
		if facts[left].Path != facts[right].Path {
			return facts[left].Path < facts[right].Path
		}
		if facts[left].Span.Start != facts[right].Span.Start {
			return facts[left].Span.Start < facts[right].Span.Start
		}
		if facts[left].Span.End != facts[right].Span.End {
			return facts[left].Span.End < facts[right].Span.End
		}
		return facts[left].Kind < facts[right].Kind
	})
}

func deduplicateExternalReferences(facts []ExternalReferenceFact) []ExternalReferenceFact {
	if len(facts) < 2 {
		return facts
	}
	result := facts[:1]
	for _, fact := range facts[1:] {
		if result[len(result)-1] != fact {
			result = append(result, fact)
		}
	}
	return result
}

func (i *Index) removeFactsLocked(facts []SymbolFact) {
	for _, fact := range facts {
		matches := i.byName[fact.Name]
		for index, candidate := range matches {
			if candidate == fact {
				i.byName[fact.Name] = append(matches[:index], matches[index+1:]...)
				if len(i.byName[fact.Name]) == 0 {
					delete(i.byName, fact.Name)
				}
				break
			}
		}
	}
}

func (i *Index) removeExternalReferencesLocked(facts []ExternalReferenceFact) {
	for _, fact := range facts {
		matches := i.byExternalName[fact.Name]
		for index, candidate := range matches {
			if candidate == fact {
				i.byExternalName[fact.Name] = append(matches[:index], matches[index+1:]...)
				if len(i.byExternalName[fact.Name]) == 0 {
					delete(i.byExternalName, fact.Name)
				}
				break
			}
		}
	}
}
