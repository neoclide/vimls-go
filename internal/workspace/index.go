package workspace

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
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
	Path                string
	Name                string
	Kind                analysis.SymbolKind
	Range               syntax.Span
	SelectionRange      syntax.Span
	OwnerSelectionRange syntax.Span
	Detail              string
	Signature           string
	Documentation       string
	Dialect             syntax.Dialect
	Deprecated          bool
	Exported            bool
	TopLevel            bool
	Abstract            bool
	Static              bool
}

// SymbolKey is the stable source identity used by relationship indexes.
type SymbolKey struct {
	Path           string
	SelectionRange syntax.Span
	Kind           analysis.SymbolKind
}

func (fact SymbolFact) Key() SymbolKey {
	return SymbolKey{Path: fact.Path, SelectionRange: fact.SelectionRange, Kind: fact.Kind}
}

// SymbolMatch is an immutable workspace symbol match. Source is the complete
// source retained for the indexed file containing Fact.
type SymbolMatch struct {
	Fact   SymbolFact
	Source string
}

// FunctionMatch pairs a callable spelling with its indexed declaration.
// Vim9 autoload exports derive the spelling's prefix from the file path.
type FunctionMatch struct {
	Name       string
	Parameters []string
	Match      SymbolMatch
}

type ExternalReferenceKind uint8

const (
	ExternalReferenceImportMember ExternalReferenceKind = iota + 1
	ExternalReferenceAutoload
	ExternalReferenceGlobalFunction
	ExternalReferenceGlobalVariable
)

// ExternalReferenceFact is a statically provable cross-file reference. Import
// members retain the literal import spelling needed by PathResolver. Autoload
// and ordinary global function names are stored without an optional g: prefix.
type ExternalReferenceFact struct {
	Path           string
	Name           string
	Span           syntax.Span
	Kind           ExternalReferenceKind
	DirectCall     bool
	ImportPath     string
	ImportAutoload bool
}

type ExternalReferenceMatch struct {
	Fact   ExternalReferenceFact
	Source string
}

// TypeRelationFact is a direct nominal aggregate relationship. ParentName is
// only a reverse-index key; callers must resolve ParentSpan before accepting a
// match.
type TypeRelationFact struct {
	Child      SymbolKey
	ParentName string
	ParentSpan syntax.Span
	Kind       analysis.TypeRelationKind
}

type TypeRelationMatch struct {
	Fact   TypeRelationFact
	Source string
}

// TypeAliasFact keeps the target spelling needed to discover aliases during
// reverse hierarchy queries.
type TypeAliasFact struct {
	Alias      SymbolKey
	AliasName  string
	TargetName string
	TargetSpan syntax.Span
}

type TypeAliasMatch struct {
	Fact   TypeAliasFact
	Source string
}

// CallFact is a statically named call owned by a named callable. CalleeName is
// only a reverse-index key; callers must resolve CalleeSpan before accepting a
// match.
type CallFact struct {
	Caller     SymbolKey
	CalleeName string
	CalleeSpan syntax.Span
}

type CallMatch struct {
	Fact   CallFact
	Source string
}

// UserCommandFact is a statically parsed :command definition retained by the
// workspace index. Runtimepath order and script execution are deliberately not
// inferred here; diagnostics use only the complete set of defined names.
type UserCommandFact struct {
	Path        string
	Name        string
	Span        syntax.Span
	BufferLocal bool
}

// AugroupFact is an active statically named :augroup definition retained by
// the workspace index.
type AugroupFact struct {
	Path string
	Name string
	Span syntax.Span
}

type AugroupMatch struct {
	Fact   AugroupFact
	Source string
}

// GlobalNameFact is a statically named global function or variable that is
// still present after replaying direct declarations and deletions in one file.
type GlobalNameFact struct {
	Path string
	Name string
	Span syntax.Span
	Kind analysis.NameDeclarationKind
}

type indexedFile struct {
	bytes              int
	source             string
	facts              []SymbolFact
	functionParameters map[syntax.Span][]string
	references         []ExternalReferenceFact
	commands           []UserCommandFact
	augroups           []AugroupFact
	globals            []GlobalNameFact
	types              []TypeRelationFact
	aliases            []TypeAliasFact
	calls              []CallFact
}

// Index stores symbols from a bounded set of syntax files. All methods are
// safe for concurrent use. A non-positive limit means that limit is disabled.
type Index struct {
	mu                  sync.RWMutex
	maxFiles            int
	maxBytes            int
	maxRelationsPerFile int
	maxRelations        int
	bytes               int
	relations           int
	revision            uint64
	files               map[string]indexedFile
	byName              map[string][]SymbolFact
	byExternalName      map[string][]ExternalReferenceFact
	byUserCommand       map[string][]UserCommandFact
	byAugroup           map[string][]AugroupFact
	byGlobalName        map[string][]GlobalNameFact
	typesByChild        map[SymbolKey][]TypeRelationFact
	typesByParent       map[string][]TypeRelationFact
	aliasesByTarget     map[string][]TypeAliasFact
	callsByCaller       map[SymbolKey][]CallFact
	callsByCallee       map[string][]CallFact
	runtimePaths        []string
	runtimeAfter        []bool
	runtimeFiles        []map[string]string
	runtimeCatalog      map[string]struct{}
	complete            bool
	relationOverflow    map[string]bool
}

// SetRuntimePaths configures the ordered runtime roots used to classify the
// indexed source table. Callers use the resulting catalog instead of walking
// runtimepath during foreground requests.
func (i *Index) SetRuntimePaths(paths []string) {
	normalized := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path, err := normalizeIndexPath(path)
		if err != nil {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		normalized = append(normalized, path)
	}
	i.mu.Lock()
	i.runtimePaths = normalized
	i.runtimeAfter = make([]bool, len(normalized))
	i.runtimeFiles = make([]map[string]string, len(normalized))
	for index := range i.runtimeFiles {
		i.runtimeAfter[index] = filepath.Base(normalized[index]) == "after"
		i.runtimeFiles[index] = make(map[string]string)
	}
	for path := range i.files {
		i.addRuntimeFileLocked(path)
	}
	for path := range i.runtimeCatalog {
		if !i.runtimeColorPathLocked(path) {
			delete(i.runtimeCatalog, path)
			continue
		}
		i.addRuntimeFileLocked(path)
	}
	i.mu.Unlock()
}

// AddRuntimePathFile adds a path-only runtime file to the completion catalog.
// It does not parse, analyze, or count the file as a workspace source.
func (i *Index) AddRuntimePathFile(path string) error {
	normalized, err := normalizeIndexPath(path)
	if err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.runtimeCatalog == nil {
		i.runtimeCatalog = make(map[string]struct{})
	}
	i.runtimeCatalog[normalized] = struct{}{}
	i.addRuntimeFileLocked(normalized)
	return nil
}

// CopyRuntimeCatalogFrom retains path-only entries that belong to this index's
// runtime roots, without rediscovering them on a workspace rebuild.
func (i *Index) CopyRuntimeCatalogFrom(source *Index) {
	source.mu.RLock()
	paths := make([]string, 0, len(source.runtimeCatalog))
	for path := range source.runtimeCatalog {
		paths = append(paths, path)
	}
	source.mu.RUnlock()
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, path := range paths {
		if i.runtimeColorPathLocked(path) && i.addRuntimeFileLocked(path) {
			if i.runtimeCatalog == nil {
				i.runtimeCatalog = make(map[string]struct{})
			}
			i.runtimeCatalog[path] = struct{}{}
		}
	}
}

func (i *Index) runtimeColorPathLocked(path string) bool {
	for _, root := range i.runtimePaths {
		if IsRuntimePathColorPath(root, path) {
			return true
		}
	}
	return false
}

// NewIndex creates a workspace symbol index with file-count and source-byte
// limits. Optional relationship limits are per-file and total counts. The
// source byte count is len(file.Source), and the source is retained once for
// each indexed file.
func NewIndex(maxFiles, maxBytes int, relationLimits ...int) *Index {
	index := &Index{
		maxFiles:         maxFiles,
		maxBytes:         maxBytes,
		files:            make(map[string]indexedFile),
		byName:           make(map[string][]SymbolFact),
		byExternalName:   make(map[string][]ExternalReferenceFact),
		byUserCommand:    make(map[string][]UserCommandFact),
		byAugroup:        make(map[string][]AugroupFact),
		byGlobalName:     make(map[string][]GlobalNameFact),
		typesByChild:     make(map[SymbolKey][]TypeRelationFact),
		typesByParent:    make(map[string][]TypeRelationFact),
		aliasesByTarget:  make(map[string][]TypeAliasFact),
		callsByCaller:    make(map[SymbolKey][]CallFact),
		callsByCallee:    make(map[string][]CallFact),
		relationOverflow: make(map[string]bool),
	}
	if len(relationLimits) > 0 {
		index.maxRelationsPerFile = relationLimits[0]
	}
	if len(relationLimits) > 1 {
		index.maxRelations = relationLimits[1]
	}
	return index
}

// Replace atomically replaces path's symbols. If either configured limit would
// be exceeded, ErrIndexLimit is returned and the previous entry is unchanged.
func (i *Index) Replace(path string, file *syntax.File) error {
	return i.ReplaceWithAnalysis(path, file, nil)
}

// ReplaceWithAnalysis atomically replaces path's symbols. A result belonging
// to file is reused for external-reference and call facts; nil or mismatched
// results fall back to a fresh analysis for independently discovered files.
// If either configured limit would be exceeded, ErrIndexLimit is returned and
// the previous entry is unchanged.
func (i *Index) ReplaceWithAnalysis(path string, file *syntax.File, result *analysis.FileAnalysis) error {
	if file == nil {
		return ErrIndexNilFile
	}
	normalized, err := normalizeIndexPath(path)
	if err != nil {
		return err
	}
	if result == nil || result.File != file {
		result = analysis.Analyze(file)
	}
	functions := functionFacts(file)
	facts := collectFileSymbolFacts(normalized, file, functions)
	functionParameters := make(map[syntax.Span][]string, len(functions))
	for span, function := range functions {
		functionParameters[span] = function.parameters
	}
	sortFacts(facts)
	references := CollectExternalReferencesFromAnalysis(normalized, file, result)
	commands := CollectUserCommandFacts(normalized, file)
	augroups := CollectAugroupFacts(normalized, file)
	globals := CollectGlobalNameFacts(normalized, file)
	types := CollectTypeRelationFacts(normalized, file)
	aliases := CollectTypeAliasFacts(normalized, file)
	calls := CollectCallFactsFromAnalysis(normalized, file, result)
	indexed := indexedFile{bytes: len(file.Source), source: strings.Clone(file.Source), facts: facts, functionParameters: functionParameters, references: references, commands: commands, augroups: augroups, globals: globals, types: types, aliases: aliases, calls: calls}
	return i.replaceIndexedFile(normalized, indexed)
}

// CopyFileFrom reuses immutable analysis facts when rebuilding another part
// of the index. No source is read or parsed. Missing files are ignored.
func (i *Index) CopyFileFrom(source *Index, path string) error {
	source.mu.RLock()
	file, ok := source.files[path]
	source.mu.RUnlock()
	if !ok {
		return nil
	}
	return i.replaceIndexedFile(path, file)
}

func (i *Index) replaceIndexedFile(normalized string, indexed indexedFile) error {
	facts, references, commands := indexed.facts, indexed.references, indexed.commands
	augroups, globals := indexed.augroups, indexed.globals
	types, aliases, calls := indexed.types, indexed.aliases, indexed.calls

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
	relationCount := len(types) + len(aliases) + len(calls)
	relationsWithoutOld := i.relations
	if exists {
		relationsWithoutOld -= len(old.types) + len(old.aliases) + len(old.calls)
	}
	relationOverflow := i.maxRelationsPerFile > 0 && relationCount > i.maxRelationsPerFile || i.maxRelations > 0 && relationsWithoutOld+relationCount > i.maxRelations
	if relationOverflow {
		types = nil
		aliases = nil
		calls = nil
		indexed.types = nil
		indexed.aliases = nil
		indexed.calls = nil
	}
	if exists {
		i.removeFactsLocked(old.facts)
		i.removeExternalReferencesLocked(old.references)
		i.removeUserCommandsLocked(old.commands)
		i.removeAugroupsLocked(old.augroups)
		i.removeGlobalNamesLocked(old.globals)
		i.removeTypeRelationsLocked(old.types)
		i.removeTypeAliasesLocked(old.aliases)
		i.removeCallsLocked(old.calls)
	}
	delete(i.relationOverflow, normalized)
	if relationOverflow {
		i.relationOverflow[normalized] = true
	}
	i.files[normalized] = indexed
	i.addRuntimeFileLocked(normalized)
	i.bytes = indexedBytes
	i.relations = relationsWithoutOld + len(types) + len(aliases) + len(calls)
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
	for _, command := range commands {
		i.byUserCommand[command.Name] = append(i.byUserCommand[command.Name], command)
	}
	for _, fact := range augroups {
		i.byAugroup[fact.Name] = append(i.byAugroup[fact.Name], fact)
	}
	for _, fact := range globals {
		i.byGlobalName[fact.Name] = append(i.byGlobalName[fact.Name], fact)
	}
	for _, fact := range types {
		i.typesByChild[fact.Child] = append(i.typesByChild[fact.Child], fact)
		i.typesByParent[fact.ParentName] = append(i.typesByParent[fact.ParentName], fact)
	}
	for _, fact := range aliases {
		i.aliasesByTarget[fact.TargetName] = append(i.aliasesByTarget[fact.TargetName], fact)
	}
	for _, fact := range calls {
		i.callsByCaller[fact.Caller] = append(i.callsByCaller[fact.Caller], fact)
		i.callsByCallee[fact.CalleeName] = append(i.callsByCallee[fact.CalleeName], fact)
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
	i.removeUserCommandsLocked(old.commands)
	i.removeAugroupsLocked(old.augroups)
	i.removeGlobalNamesLocked(old.globals)
	i.removeTypeRelationsLocked(old.types)
	i.removeTypeAliasesLocked(old.aliases)
	i.removeCallsLocked(old.calls)
	i.removeRuntimeFileLocked(normalized)
	delete(i.relationOverflow, normalized)
	delete(i.files, normalized)
	i.bytes -= old.bytes
	i.relations -= len(old.types) + len(old.aliases) + len(old.calls)
	i.revision++
}

// RuntimeFile returns the first indexed file with relativePath in configured
// runtimepath order.
func (i *Index) RuntimeFile(relativePath string) (string, bool) {
	relativePath, ok := cleanRuntimeRelativePath(relativePath)
	if !ok {
		return "", false
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	for index, files := range i.runtimeFiles {
		if i.runtimeAfter[index] {
			continue
		}
		if path := files[relativePath]; path != "" {
			return path, true
		}
	}
	return "", false
}

// HasAutoloadFunction reports whether the first matching runtimepath file
// declares name as a legacy autoload function or a Vim9 exported function.
func (i *Index) HasAutoloadFunction(name string) bool {
	relativePath, ok := AutoloadPath(name)
	if !ok {
		return false
	}
	name = strings.TrimPrefix(name, "g:")
	baseName := name[strings.LastIndexByte(name, '#')+1:]
	i.mu.RLock()
	defer i.mu.RUnlock()
	for index, files := range i.runtimeFiles {
		if i.runtimeAfter[index] {
			continue
		}
		path := files[relativePath]
		if path == "" {
			continue
		}
		for _, fact := range i.files[path].facts {
			if !fact.TopLevel || fact.Kind != analysis.SymbolKindFunction {
				continue
			}
			if (fact.Dialect == syntax.Legacy && strings.TrimPrefix(fact.Name, "g:") == name) || (fact.Dialect == syntax.Vim9 && fact.Exported && fact.Name == baseName) {
				return true
			}
		}
		return false
	}
	return false
}

// AutoloadDependents returns indexed files with direct autoload calls whose
// target is the runtime-relative file containing path.
func (i *Index) AutoloadDependents(path string) []string {
	normalized, err := normalizeIndexPath(path)
	if err != nil {
		return nil
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	relativePath := ""
	for index, root := range i.runtimePaths {
		if i.runtimeAfter[index] {
			continue
		}
		relative, err := filepath.Rel(root, normalized)
		if err != nil {
			continue
		}
		relative, ok := cleanRuntimeRelativePath(relative)
		if ok && strings.HasPrefix(relative, "autoload/") && strings.HasSuffix(relative, ".vim") {
			relativePath = relative
			break
		}
	}
	if relativePath == "" {
		return nil
	}
	seen := make(map[string]bool)
	for name, references := range i.byExternalName {
		target, ok := AutoloadPath(name)
		if !ok || target != relativePath {
			continue
		}
		for _, reference := range references {
			if reference.Kind == ExternalReferenceAutoload && reference.DirectCall {
				seen[reference.Path] = true
			}
		}
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// GlobalFunctionDependents returns indexed files with direct calls to a
// top-level global function declared by path.
func (i *Index) GlobalFunctionDependents(path string) []string {
	normalized, err := normalizeIndexPath(path)
	if err != nil {
		return nil
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	file, ok := i.files[normalized]
	if !ok {
		return nil
	}
	names := make(map[string]bool)
	for _, fact := range file.facts {
		name := strings.TrimPrefix(fact.Name, "g:")
		if fact.TopLevel && fact.Kind == analysis.SymbolKindFunction && !strings.Contains(name, "#") &&
			(fact.Dialect == syntax.Legacy || strings.HasPrefix(fact.Name, "g:")) {
			names[name] = true
		}
	}
	seen := make(map[string]bool)
	for name := range names {
		for _, reference := range i.byExternalName[name] {
			if reference.Kind == ExternalReferenceGlobalFunction && reference.DirectCall {
				seen[reference.Path] = true
			}
		}
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// RuntimePathCompletions returns direct indexed children below one runtime
// directory. Duplicate displays keep the first runtimepath entry.
func (i *Index) RuntimePathCompletions(directory, prefix string, limit int, acceptPath ...func(string) bool) ([]PathCompletion, bool) {
	return i.runtimePathCompletions(directory, prefix, limit, true, false, false, firstPathPredicate(acceptPath))
}

func firstPathPredicate(predicates []func(string) bool) func(string) bool {
	if len(predicates) == 0 {
		return nil
	}
	return predicates[0]
}

func (i *Index) runtimePathCompletions(directory, prefix string, limit int, includeDirectories, includeAfter, fuzzy bool, acceptPath func(string) bool) ([]PathCompletion, bool) {
	if limit <= 0 || strings.ContainsAny(prefix, "\x00\r\n\\") {
		return nil, false
	}
	prefix = filepath.ToSlash(prefix)
	dirPart, namePrefix := filepath.Split(filepath.FromSlash(prefix))
	dirPart = filepath.ToSlash(dirPart)
	namePrefixFolded := strings.ToLower(namePrefix)
	wantedDirectory, ok := cleanRuntimeRelativePath(filepath.ToSlash(filepath.Join(directory, filepath.FromSlash(dirPart))))
	if !ok {
		return nil, false
	}
	i.mu.RLock()
	seen := make(map[string]PathCompletion)
	for index, files := range i.runtimeFiles {
		if i.runtimeAfter[index] && !includeAfter {
			continue
		}
		for relative, path := range files {
			parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(relative)))
			if parent == wantedDirectory {
				name := filepath.Base(filepath.FromSlash(relative))
				matches := strings.HasPrefix(strings.ToLower(name), namePrefixFolded)
				if fuzzy {
					matches = fuzzyTextMatches(namePrefix, strings.TrimSuffix(name, ".vim"))
				}
				if matches && strings.HasSuffix(name, ".vim") {
					if acceptPath != nil && !acceptPath(path) {
						continue
					}
					display := dirPart + name
					if _, exists := seen[display]; !exists {
						seen[display] = PathCompletion{Display: display, Path: path}
					}
				}
				continue
			}
			if !includeDirectories {
				continue
			}
			prefixDirectory := wantedDirectory + "/"
			if !strings.HasPrefix(parent, prefixDirectory) {
				continue
			}
			child := strings.TrimPrefix(parent, prefixDirectory)
			if slash := strings.IndexByte(child, '/'); slash >= 0 {
				child = child[:slash]
			}
			if child == "" || !strings.HasPrefix(strings.ToLower(child), namePrefixFolded) {
				continue
			}
			display := dirPart + child + "/"
			if _, exists := seen[display]; !exists {
				seen[display] = PathCompletion{Display: display, IsDir: true}
			}
		}
	}
	incomplete := !i.complete || len(seen) > limit
	result := make([]PathCompletion, 0, min(len(seen), limit))
	for _, completion := range seen {
		result = append(result, completion)
	}
	i.mu.RUnlock()
	sort.Slice(result, func(left, right int) bool { return result[left].Display < result[right].Display })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, incomplete
}

// ColorSchemeCompletions returns indexed top-level colors/*.vim files.
func (i *Index) ColorSchemeCompletions(prefix string, limit int, acceptPath ...func(string) bool) ([]PathCompletion, bool) {
	files, incomplete := i.runtimePathCompletions("colors", prefix, limit, false, true, true, firstPathPredicate(acceptPath))
	result := files[:0]
	for _, file := range files {
		if file.IsDir {
			continue
		}
		file.Display = strings.TrimSuffix(file.Display, ".vim")
		result = append(result, file)
	}
	return result, incomplete
}

func fuzzyTextMatches(pattern, text string) bool {
	patternRunes := []rune(strings.ToLower(pattern))
	if len(patternRunes) == 0 {
		return true
	}
	patternIndex := 0
	for _, textRune := range []rune(strings.ToLower(text)) {
		if patternRunes[patternIndex] == textRune {
			patternIndex++
			if patternIndex == len(patternRunes) {
				return true
			}
		}
	}
	return false
}

func (i *Index) addRuntimeFileLocked(path string) bool {
	added := false
	for index, root := range i.runtimePaths {
		if !pathWithinOrEqual(root, path) {
			continue
		}
		relative, err := filepath.Rel(root, path)
		if err == nil && relative != "." {
			i.runtimeFiles[index][filepath.ToSlash(relative)] = path
			added = true
		}
	}
	return added
}

func (i *Index) removeRuntimeFileLocked(path string) {
	for index, root := range i.runtimePaths {
		if !pathWithinOrEqual(root, path) {
			continue
		}
		if relative, err := filepath.Rel(root, path); err == nil {
			delete(i.runtimeFiles[index], filepath.ToSlash(relative))
		}
	}
}

func cleanRuntimeRelativePath(path string) (string, bool) {
	path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if path == "." || path == ".." || strings.HasPrefix(path, "../") || filepath.IsAbs(filepath.FromSlash(path)) {
		return "", false
	}
	return path, true
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

// Revision changes after every observable index state change: a successful
// Replace, an effective Remove, or a complete-state transition.
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

// TypeRelations returns direct parent facts for child in source order.
func (i *Index) TypeRelations(child SymbolKey) []TypeRelationMatch {
	var err error
	child.Path, err = normalizeIndexPath(child.Path)
	if err != nil {
		return nil
	}
	i.mu.RLock()
	facts := i.typesByChild[child]
	result := make([]TypeRelationMatch, 0, len(facts))
	for _, fact := range facts {
		if file, ok := i.files[fact.Child.Path]; ok {
			result = append(result, TypeRelationMatch{Fact: fact, Source: file.source})
		}
	}
	i.mu.RUnlock()
	return result
}

// TypeRelationCandidates returns reverse-index candidates for a parent name.
// The caller must resolve each ParentSpan and compare the resulting SymbolKey.
func (i *Index) TypeRelationCandidates(parentName string) []TypeRelationMatch {
	if parentName == "" {
		return nil
	}
	i.mu.RLock()
	facts := i.typesByParent[parentName]
	result := make([]TypeRelationMatch, 0, len(facts))
	for _, fact := range facts {
		if file, ok := i.files[fact.Child.Path]; ok {
			result = append(result, TypeRelationMatch{Fact: fact, Source: file.source})
		}
	}
	i.mu.RUnlock()
	sort.SliceStable(result, func(left, right int) bool { return relationFactLess(result[left].Fact, result[right].Fact) })
	return result
}

// TypeAliasCandidates returns aliases whose target's final source name matches
// targetName. Callers must resolve TargetSpan before accepting a candidate.
func (i *Index) TypeAliasCandidates(targetName string) []TypeAliasMatch {
	if targetName == "" {
		return nil
	}
	i.mu.RLock()
	facts := i.aliasesByTarget[targetName]
	result := make([]TypeAliasMatch, 0, len(facts))
	for _, fact := range facts {
		if file, ok := i.files[fact.Alias.Path]; ok {
			result = append(result, TypeAliasMatch{Fact: fact, Source: file.source})
		}
	}
	i.mu.RUnlock()
	sort.SliceStable(result, func(left, right int) bool { return aliasFactLess(result[left].Fact, result[right].Fact) })
	return result
}

// Calls returns direct call facts owned by caller in source order.
func (i *Index) Calls(caller SymbolKey) []CallMatch {
	var err error
	caller.Path, err = normalizeIndexPath(caller.Path)
	if err != nil {
		return nil
	}
	i.mu.RLock()
	facts := i.callsByCaller[caller]
	result := make([]CallMatch, 0, len(facts))
	for _, fact := range facts {
		if file, ok := i.files[fact.Caller.Path]; ok {
			result = append(result, CallMatch{Fact: fact, Source: file.source})
		}
	}
	i.mu.RUnlock()
	return result
}

// CallCandidates returns reverse-index candidates for a callee name. The
// caller must resolve each CalleeSpan and compare the resulting SymbolKey.
func (i *Index) CallCandidates(calleeName string) []CallMatch {
	if calleeName == "" {
		return nil
	}
	i.mu.RLock()
	facts := i.callsByCallee[calleeName]
	result := make([]CallMatch, 0, len(facts))
	for _, fact := range facts {
		if file, ok := i.files[fact.Caller.Path]; ok {
			result = append(result, CallMatch{Fact: fact, Source: file.source})
		}
	}
	i.mu.RUnlock()
	sort.SliceStable(result, func(left, right int) bool { return callFactLess(result[left].Fact, result[right].Fact) })
	return result
}

// UserCommandNames returns every distinct parsed user-command definition in
// bytewise name order. The returned slice is independent of the index.
func (i *Index) UserCommandNames() []string {
	i.mu.RLock()
	names := make([]string, 0, len(i.byUserCommand))
	for name := range i.byUserCommand {
		names = append(names, name)
	}
	i.mu.RUnlock()
	sort.Strings(names)
	return names
}

// AugroupNames returns every distinct statically defined autocommand group in
// bytewise name order. The returned slice is independent of the index.
func (i *Index) AugroupNames() []string {
	i.mu.RLock()
	names := make([]string, 0, len(i.byAugroup))
	for name := range i.byAugroup {
		names = append(names, name)
	}
	i.mu.RUnlock()
	sort.Strings(names)
	return names
}

// AugroupDefinitions returns active static definitions with name in path and
// source order. Returned matches do not alias index-owned slices.
func (i *Index) AugroupDefinitions(name string) []AugroupMatch {
	if name == "" {
		return nil
	}
	i.mu.RLock()
	facts := i.byAugroup[name]
	result := make([]AugroupMatch, 0, len(facts))
	for _, fact := range facts {
		if file, ok := i.files[fact.Path]; ok {
			result = append(result, AugroupMatch{Fact: fact, Source: file.source})
		}
	}
	i.mu.RUnlock()
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].Fact.Path != result[right].Fact.Path {
			return result[left].Fact.Path < result[right].Fact.Path
		}
		return result[left].Fact.Span.Start < result[right].Fact.Span.Start
	})
	return result
}

// UserCommandCompletionNames returns user commands from accepted source
// files. The predicate is applied before names are deduplicated.
func (i *Index) UserCommandCompletionNames(acceptPath func(string) bool) []string {
	i.mu.RLock()
	names := make(map[string]struct{})
	for name, facts := range i.byUserCommand {
		for _, fact := range facts {
			if acceptPath == nil || acceptPath(fact.Path) {
				names[name] = struct{}{}
				break
			}
		}
	}
	i.mu.RUnlock()
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// GlobalNameFacts returns the active global declarations with name. Results
// are independent of the index and retain their declaration locations.
func (i *Index) GlobalNameFacts(name string) []GlobalNameFact {
	if name == "" {
		return nil
	}
	i.mu.RLock()
	facts := append([]GlobalNameFact(nil), i.byGlobalName[name]...)
	i.mu.RUnlock()
	sort.SliceStable(facts, func(left, right int) bool {
		if facts[left].Path != facts[right].Path {
			return facts[left].Path < facts[right].Path
		}
		if facts[left].Span.Start != facts[right].Span.Start {
			return facts[left].Span.Start < facts[right].Span.Start
		}
		return facts[left].Kind < facts[right].Kind
	})
	return facts
}

// GlobalVariableCompletions returns active legacy global variables outside
// excludePath that do not conflict with an indexed global function of the
// same name. The current file is analyzed separately for position visibility.
func (i *Index) GlobalVariableCompletions(prefix, excludePath string, limit int, acceptPath ...func(string) bool) ([]GlobalNameFact, bool) {
	prefix = strings.ToLower(prefix)
	return i.GlobalVariableCompletionsMatching(func(name string) bool {
		return strings.HasPrefix(strings.ToLower(name), prefix)
	}, excludePath, limit, acceptPath...)
}

// GlobalVariableCompletionsMatching applies matches before sorting and limiting.
func (i *Index) GlobalVariableCompletionsMatching(matches func(string) bool, excludePath string, limit int, acceptPath ...func(string) bool) ([]GlobalNameFact, bool) {
	excluded, _ := normalizeIndexPath(excludePath)
	i.mu.RLock()
	facts := make([]GlobalNameFact, 0)
	for name, candidates := range i.byGlobalName {
		if matches != nil && !matches(name) {
			continue
		}
		var variable *GlobalNameFact
		conflict := false
		for index := range candidates {
			candidate := &candidates[index]
			if len(acceptPath) > 0 && acceptPath[0] != nil && !acceptPath[0](candidate.Path) {
				continue
			}
			if candidate.Kind == analysis.NameDeclarationFunction {
				conflict = true
				break
			}
			if candidate.Path == excluded {
				continue
			}
			if variable == nil || candidate.Path < variable.Path || candidate.Path == variable.Path && candidate.Span.Start < variable.Span.Start {
				variable = candidate
			}
		}
		if !conflict && variable != nil {
			facts = append(facts, *variable)
		}
	}
	complete := i.complete
	i.mu.RUnlock()
	sort.SliceStable(facts, func(left, right int) bool { return facts[left].Name < facts[right].Name })
	truncated := limit > 0 && len(facts) > limit
	if truncated {
		facts = facts[:limit]
	}
	return facts, truncated || !complete
}

// GlobalNameConflictDiagnostics warns on current-file declarations that have
// an opposite-kind declaration in another indexed file. Same-file conflicts
// are owned by analysis.Analyze and are skipped here.
func (i *Index) GlobalNameConflictDiagnostics(path string, file *syntax.File) []syntax.Diagnostic {
	normalized, err := normalizeIndexPath(path)
	if err != nil || file == nil {
		return nil
	}
	active := make(map[string]map[analysis.NameDeclarationKind]bool)
	diagnostics := make([]syntax.Diagnostic, 0)
	for _, event := range analysis.CollectNameDeclarationEvents(file) {
		if event.Scope != analysis.NameDeclarationGlobal {
			continue
		}
		if active[event.Name] == nil {
			active[event.Name] = make(map[analysis.NameDeclarationKind]bool)
		}
		if event.Delete {
			delete(active[event.Name], event.Kind)
			continue
		}
		opposite := analysis.NameDeclarationVariable
		if event.Kind == analysis.NameDeclarationVariable {
			opposite = analysis.NameDeclarationFunction
		}
		localConflict := active[event.Name][opposite]
		if localConflict {
			continue
		}
		i.mu.RLock()
		conflict := false
		for _, fact := range i.byGlobalName[event.Name] {
			if fact.Path != normalized && fact.Kind == opposite {
				conflict = true
				break
			}
		}
		i.mu.RUnlock()
		if conflict {
			diagnostics = append(diagnostics, globalNameConflictDiagnostic(event))
			continue
		}
		active[event.Name][event.Kind] = true
	}
	return diagnostics
}

func globalNameConflictDiagnostic(event analysis.NameDeclarationEvent) syntax.Diagnostic {
	if event.Kind == analysis.NameDeclarationVariable {
		return syntax.Diagnostic{
			Code: "vim/E705", Message: "Variable " + event.Name + " conflicts with a function declared in the global scope; rename one to avoid runtime conflicts", Span: event.Span,
		}
	}
	return syntax.Diagnostic{
		Code: "vim/E707", Message: "Function " + event.Name + " conflicts with a variable declared in the global scope; rename one to avoid runtime conflicts", Span: event.Span,
	}
}

// SetComplete records whether every discovered source fitted in the index.
// Cross-file diagnostics that require a closed world must remain silent when
// this value is false.
func (i *Index) SetComplete(complete bool) {
	i.mu.Lock()
	if i.complete == complete {
		i.mu.Unlock()
		return
	}
	i.complete = complete
	i.revision++
	i.mu.Unlock()
}

func (i *Index) Complete() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.complete
}

// RelationshipsComplete reports whether every discovered source installed all
// type, alias and call facts. It is intentionally separate from the ordinary
// source index completeness used by completion and diagnostics.
func (i *Index) RelationshipsComplete() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.complete && len(i.relationOverflow) == 0
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

// GlobalFunction returns an unambiguous top-level function declaration for
// name. Script-local functions and autoload short names are not guessed.
func (i *Index) GlobalFunction(name string) (SymbolMatch, bool) {
	return i.globalSymbol(name, analysis.NameDeclarationFunction)
}

// HasGlobalFunction reports whether any indexed top-level global function has
// name. Unlike GlobalFunction, ambiguity does not make an indexed name absent.
func (i *Index) HasGlobalFunction(name string) bool {
	name = strings.TrimPrefix(name, "g:")
	if name == "" {
		return false
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	for _, global := range i.byGlobalName[name] {
		if global.Kind != analysis.NameDeclarationFunction {
			continue
		}
		for _, fact := range i.files[global.Path].facts {
			if fact.SelectionRange == global.Span && fact.TopLevel && fact.Kind == analysis.SymbolKindFunction &&
				(fact.Dialect == syntax.Legacy || strings.HasPrefix(fact.Name, "g:")) {
				return true
			}
		}
	}
	return false
}

// GlobalVariable returns one unambiguous top-level legacy global variable.
func (i *Index) GlobalVariable(name string) (SymbolMatch, bool) {
	return i.globalSymbol(name, analysis.NameDeclarationVariable)
}

func (i *Index) globalSymbol(name string, kind analysis.NameDeclarationKind) (SymbolMatch, bool) {
	name = strings.TrimPrefix(name, "g:")
	if name == "" {
		return SymbolMatch{}, false
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	var match SymbolMatch
	found := false
	for _, global := range i.byGlobalName[name] {
		if global.Kind != kind {
			continue
		}
		file := i.files[global.Path]
		for _, fact := range file.facts {
			if fact.SelectionRange != global.Span || !fact.TopLevel || !(fact.Dialect == syntax.Legacy || strings.HasPrefix(fact.Name, "g:")) {
				continue
			}
			if kind == analysis.NameDeclarationFunction && fact.Kind != analysis.SymbolKindFunction || kind == analysis.NameDeclarationVariable && fact.Kind != analysis.SymbolKindVariable && fact.Kind != analysis.SymbolKindConstant {
				continue
			}
			if found && (match.Fact.Path != fact.Path || match.Fact.SelectionRange != fact.SelectionRange) {
				return SymbolMatch{}, false
			}
			match = SymbolMatch{Fact: fact, Source: file.source}
			found = true
		}
	}
	return match, found
}

// FunctionCompletions returns indexed callable function names. Autoload
// functions are available in both dialects. includeLegacyGlobals additionally
// includes top-level global functions.
func (i *Index) FunctionCompletions(prefix string, includeLegacyGlobals bool, limit int, acceptPath ...func(string) bool) ([]FunctionMatch, bool) {
	prefix = strings.ToLower(prefix)
	return i.FunctionCompletionsMatching(func(name string) bool {
		return strings.HasPrefix(strings.ToLower(name), prefix)
	}, includeLegacyGlobals, limit, acceptPath...)
}

// FunctionCompletionsMatching applies matches before sorting and limiting.
func (i *Index) FunctionCompletionsMatching(matchName func(string) bool, includeLegacyGlobals bool, limit int, acceptPath ...func(string) bool) ([]FunctionMatch, bool) {
	i.mu.RLock()
	byCallableName := make(map[string]FunctionMatch)
	for path, file := range i.files {
		if len(acceptPath) > 0 && acceptPath[0] != nil && !acceptPath[0](path) {
			continue
		}
		for _, fact := range file.facts {
			if !fact.TopLevel || fact.Kind != analysis.SymbolKindFunction {
				continue
			}
			name := strings.TrimPrefix(fact.Name, "g:")
			if strings.Contains(name, "#") {
				// Legacy autoload declarations already contain the full name.
			} else if fact.Exported {
				if derived, ok := i.runtimeAutoloadNameLocked(path, name); ok {
					name = derived
				} else if !includeLegacyGlobals || fact.Dialect != syntax.Legacy {
					continue
				}
			} else if !includeLegacyGlobals || (fact.Dialect != syntax.Legacy && !strings.HasPrefix(fact.Name, "g:")) || strings.HasPrefix(fact.Name, "s:") {
				continue
			}
			if matchName != nil && !matchName(name) {
				continue
			}
			previous, exists := byCallableName[name]
			if !exists || factLess(fact, previous.Match.Fact) {
				parameters := append([]string(nil), file.functionParameters[fact.SelectionRange]...)
				byCallableName[name] = FunctionMatch{Name: name, Parameters: parameters, Match: SymbolMatch{Fact: fact, Source: file.source}}
			}
		}
	}
	complete := i.complete
	i.mu.RUnlock()
	matches := make([]FunctionMatch, 0, len(byCallableName))
	for _, match := range byCallableName {
		matches = append(matches, match)
	}
	sort.SliceStable(matches, func(left, right int) bool { return matches[left].Name < matches[right].Name })
	truncated := limit > 0 && len(matches) > limit
	if truncated {
		matches = matches[:limit]
	}
	return matches, truncated || !complete
}

func (i *Index) runtimeAutoloadNameLocked(path, name string) (string, bool) {
	for rootIndex, root := range i.runtimePaths {
		if i.runtimeAfter[rootIndex] {
			continue
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		relative = filepath.ToSlash(relative)
		if i.runtimeFiles[rootIndex][relative] != path || !strings.HasPrefix(relative, "autoload/") || !strings.HasSuffix(relative, ".vim") {
			continue
		}
		prefix := strings.TrimSuffix(strings.TrimPrefix(relative, "autoload/"), ".vim")
		return strings.ReplaceAll(prefix, "/", "#") + "#" + name, true
	}
	return "", false
}

// Search returns symbols whose names contain query as a case-insensitive
// ordered subsequence. Exact matches rank before prefixes, followed by other
// subsequence matches; ties use the index's stable fact ordering. An empty
// query matches every symbol. A positive limit caps the result count.
func (i *Index) Search(query string, limit int) []SymbolMatch {
	return i.search(query, limit, nil)
}

// SearchInRoots returns symbols whose source files are below one of roots.
// Filtering happens before ranking and limiting so external files cannot hide
// workspace results.
func (i *Index) SearchInRoots(query string, roots []string, limit int) []SymbolMatch {
	return i.search(query, limit, func(path string) bool {
		for _, root := range roots {
			if pathWithinOrEqual(root, path) {
				return true
			}
		}
		return false
	})
}

func (i *Index) search(query string, limit int, acceptPath func(string) bool) []SymbolMatch {
	type rankedMatch struct {
		match SymbolMatch
		rank  int
	}
	queryFolded := strings.ToLower(query)
	i.mu.RLock()
	ranked := make([]rankedMatch, 0)
	for path, file := range i.files {
		if acceptPath != nil && !acceptPath(path) {
			continue
		}
		for _, fact := range file.facts {
			rank, ok := searchRank(query, queryFolded, fact.Name)
			if !ok {
				continue
			}
			ranked = append(ranked, rankedMatch{match: SymbolMatch{Fact: fact, Source: file.source}, rank: rank})
		}
	}
	i.mu.RUnlock()
	sort.SliceStable(ranked, func(left, right int) bool {
		if ranked[left].rank != ranked[right].rank {
			return ranked[left].rank < ranked[right].rank
		}
		return factLess(ranked[left].match.Fact, ranked[right].match.Fact)
	})
	if limit > 0 && len(ranked) > limit {
		ranked = ranked[:limit]
	}
	matches := make([]SymbolMatch, len(ranked))
	for index := range ranked {
		matches[index] = ranked[index].match
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
	for _, nameRune := range foldedName {
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
	return CanonicalPath(filepath.Clean(abs))
}

// CollectUserCommandFacts returns every explicit :command definition in file,
// including definitions in deferred command lists. Dynamic :execute forms are
// intentionally absent because their full command name is not statically
// known.
func CollectUserCommandFacts(path string, file *syntax.File) []UserCommandFact {
	normalized, err := normalizeIndexPath(path)
	if err != nil || file == nil {
		return nil
	}
	facts := make([]UserCommandFact, 0)
	var collect func([]syntax.Command)
	collect = func(commands []syntax.Command) {
		for index := range commands {
			command := &commands[index]
			if name, span, bufferLocal, ok := syntax.DefinedUserCommand(file, command); ok && len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
				facts = append(facts, UserCommandFact{Path: normalized, Name: strings.Clone(name), Span: span, BufferLocal: bufferLocal})
			}
			if command.Embedded != nil {
				collect(command.Embedded.Commands)
			}
		}
	}
	collect(file.Commands)
	return facts
}

// CollectAugroupFacts returns the active statically named :augroup
// definitions in file.
func CollectAugroupFacts(path string, file *syntax.File) []AugroupFact {
	normalized, err := normalizeIndexPath(path)
	if err != nil || file == nil {
		return nil
	}
	definitions := analysis.CollectAugroupDefinitions(file)
	facts := make([]AugroupFact, 0, len(definitions))
	for _, definition := range definitions {
		facts = append(facts, AugroupFact{Path: normalized, Name: definition.Name, Span: definition.Span})
	}
	return facts
}

// CollectGlobalNameFacts replays direct global declarations and deletions in
// one file and returns the declarations still active at the end of that file.
func CollectGlobalNameFacts(path string, file *syntax.File) []GlobalNameFact {
	normalized, err := normalizeIndexPath(path)
	if err != nil || file == nil {
		return nil
	}
	type factKey struct {
		name string
		kind analysis.NameDeclarationKind
	}
	active := make(map[factKey]GlobalNameFact)
	for _, event := range analysis.CollectNameDeclarationEvents(file) {
		if event.Scope != analysis.NameDeclarationGlobal {
			continue
		}
		key := factKey{name: event.Name, kind: event.Kind}
		if event.Delete {
			delete(active, key)
			continue
		}
		opposite := analysis.NameDeclarationVariable
		if event.Kind == analysis.NameDeclarationVariable {
			opposite = analysis.NameDeclarationFunction
		}
		if _, conflict := active[factKey{name: event.Name, kind: opposite}]; conflict {
			continue
		}
		if _, exists := active[key]; !exists {
			active[key] = GlobalNameFact{Path: normalized, Name: strings.Clone(event.Name), Span: event.Span, Kind: event.Kind}
		}
	}
	facts := make([]GlobalNameFact, 0, len(active))
	for _, fact := range active {
		facts = append(facts, fact)
	}
	sort.SliceStable(facts, func(left, right int) bool {
		if facts[left].Name != facts[right].Name {
			return facts[left].Name < facts[right].Name
		}
		if facts[left].Kind != facts[right].Kind {
			return facts[left].Kind < facts[right].Kind
		}
		return facts[left].Span.Start < facts[right].Span.Start
	})
	return facts
}

// CollectSymbolFacts returns immutable symbol facts for file. Exported is set
// only when the declaration-bearing top-level command has an export modifier.
func CollectSymbolFacts(path string, file *syntax.File) []SymbolFact {
	normalized, err := normalizeIndexPath(path)
	if err != nil || file == nil {
		return nil
	}
	functions := functionFacts(file)
	return collectFileSymbolFacts(normalized, file, functions)
}

func collectFileSymbolFacts(normalized string, file *syntax.File, functions map[syntax.Span]indexedFunctionFact) []SymbolFact {
	exported := exportedSymbolSpans(file)
	metadata := symbolMetadata(file)
	facts := make([]SymbolFact, 0)
	collectSymbolFacts(normalized, analysis.CollectSymbols(file), exported, functions, metadata, true, syntax.Span{}, &facts)
	sortFacts(facts)
	return facts
}

type indexedFunctionFact struct {
	signature     string
	documentation string
	dialect       syntax.Dialect
	parameters    []string
}

type indexedSymbolMetadata struct {
	abstract bool
	static   bool
}

func collectSymbolFacts(path string, symbols []*analysis.Symbol, exported map[syntax.Span]bool, functions map[syntax.Span]indexedFunctionFact, metadata map[syntax.Span]indexedSymbolMetadata, topLevel bool, owner syntax.Span, facts *[]SymbolFact) {
	for _, symbol := range symbols {
		if symbol == nil || symbol.Name == "" {
			continue
		}
		function := functions[symbol.SelectionRange]
		meta := metadata[symbol.SelectionRange]
		*facts = append(*facts, SymbolFact{
			Path:                path,
			Name:                strings.Clone(symbol.Name),
			Kind:                symbol.Kind,
			Range:               symbol.Range,
			SelectionRange:      symbol.SelectionRange,
			OwnerSelectionRange: owner,
			Detail:              strings.Clone(symbol.Detail),
			Signature:           strings.Clone(function.signature),
			Documentation:       strings.Clone(function.documentation),
			Dialect:             function.dialect,
			Deprecated:          symbol.Deprecated,
			Exported:            exported[symbol.SelectionRange],
			TopLevel:            topLevel,
			Abstract:            meta.abstract,
			Static:              meta.static,
		})
		collectSymbolFacts(path, symbol.Children, exported, functions, metadata, false, symbol.SelectionRange, facts)
	}
}

func symbolMetadata(file *syntax.File) map[syntax.Span]indexedSymbolMetadata {
	result := make(map[syntax.Span]indexedSymbolMetadata)
	var collect func([]syntax.Command)
	collect = func(commands []syntax.Command) {
		for index := range commands {
			command := &commands[index]
			metadata := indexedSymbolMetadata{abstract: commandHasModifier(command, "abstract"), static: commandHasModifier(command, "static")}
			switch {
			case command.Aggregate != nil:
				result[command.Aggregate.Name] = metadata
				if command.Aggregate.Kind == syntax.BlockInterface {
					for _, memberIndex := range command.Aggregate.Members {
						if memberIndex < 0 || memberIndex >= len(commands) {
							continue
						}
						member := &commands[memberIndex]
						memberMetadata := indexedSymbolMetadata{abstract: true, static: commandHasModifier(member, "static")}
						if member.Function != nil {
							result[member.Function.Name] = memberMetadata
						}
						if member.Declaration != nil {
							for _, binding := range member.Declaration.Bindings {
								result[binding.Name] = memberMetadata
							}
						}
					}
				}
			case command.Function != nil:
				metadata.abstract = metadata.abstract || result[command.Function.Name].abstract
				result[command.Function.Name] = metadata
			case command.Declaration != nil:
				for _, binding := range command.Declaration.Bindings {
					metadata.abstract = metadata.abstract || result[binding.Name].abstract
					result[binding.Name] = metadata
				}
			}
			if command.Embedded != nil {
				collect(command.Embedded.Commands)
			}
		}
	}
	collect(file.Commands)
	return result
}

// CollectTypeRelationFacts returns immutable direct aggregate relationships.
func CollectTypeRelationFacts(path string, file *syntax.File) []TypeRelationFact {
	normalized, err := normalizeIndexPath(path)
	if err != nil || file == nil {
		return nil
	}
	relations := analysis.CollectTypeRelations(file)
	facts := make([]TypeRelationFact, 0, len(relations))
	for _, relation := range relations {
		facts = append(facts, TypeRelationFact{
			Child:      SymbolKey{Path: normalized, SelectionRange: relation.ChildSpan, Kind: relation.ChildKind},
			ParentName: strings.Clone(relation.ParentName), ParentSpan: relation.ParentSpan, Kind: relation.Kind,
		})
	}
	sort.SliceStable(facts, func(left, right int) bool { return relationFactLess(facts[left], facts[right]) })
	return facts
}

// CollectTypeAliasFacts returns aliases of named types. Compound aliases are
// deliberately absent because they cannot lead to aggregate declarations.
func CollectTypeAliasFacts(path string, file *syntax.File) []TypeAliasFact {
	normalized, err := normalizeIndexPath(path)
	if err != nil || file == nil {
		return nil
	}
	facts := make([]TypeAliasFact, 0)
	var collect func([]syntax.Command)
	collect = func(commands []syntax.Command) {
		for index := range commands {
			command := &commands[index]
			alias := command.TypeAlias
			if alias != nil && alias.Type != nil && alias.Type.Kind == syntax.TypeNamed && validIndexSpan(file, alias.Name) && validIndexSpan(file, alias.Type.Span) {
				aliasName := file.Text(alias.Name)
				targetName := finalTypeName(alias.Type.Name)
				if aliasName != "" && targetName != "" {
					facts = append(facts, TypeAliasFact{
						Alias:     SymbolKey{Path: normalized, SelectionRange: alias.Name, Kind: analysis.SymbolKindTypeAlias},
						AliasName: strings.Clone(aliasName), TargetName: strings.Clone(targetName), TargetSpan: alias.Type.Span,
					})
				}
			}
			if command.Embedded != nil {
				collect(command.Embedded.Commands)
			}
		}
	}
	collect(file.Commands)
	sort.SliceStable(facts, func(left, right int) bool { return aliasFactLess(facts[left], facts[right]) })
	return facts
}

func finalTypeName(name string) string {
	if separator := strings.LastIndexByte(name, '.'); separator >= 0 {
		name = name[separator+1:]
	}
	return name
}

// CollectCallFactsFromAnalysis returns immutable named call relationships and
// reuses result's semantic pass.
func CollectCallFactsFromAnalysis(path string, file *syntax.File, result *analysis.FileAnalysis) []CallFact {
	normalized, err := normalizeIndexPath(path)
	if err != nil || file == nil || result == nil || result.File != file {
		return nil
	}
	relations := analysis.CollectCallRelations(result)
	facts := make([]CallFact, 0, len(relations))
	for _, relation := range relations {
		facts = append(facts, CallFact{
			Caller:     SymbolKey{Path: normalized, SelectionRange: relation.CallerSpan, Kind: relation.CallerKind},
			CalleeName: strings.Clone(relation.CalleeName), CalleeSpan: relation.CalleeSpan,
		})
	}
	sort.SliceStable(facts, func(left, right int) bool { return callFactLess(facts[left], facts[right]) })
	return facts
}

func relationFactLess(left, right TypeRelationFact) bool {
	if left.Child.Path != right.Child.Path {
		return left.Child.Path < right.Child.Path
	}
	if left.Child.SelectionRange != right.Child.SelectionRange {
		return left.Child.SelectionRange.Start < right.Child.SelectionRange.Start
	}
	if left.ParentSpan != right.ParentSpan {
		return left.ParentSpan.Start < right.ParentSpan.Start
	}
	return left.Kind < right.Kind
}

func aliasFactLess(left, right TypeAliasFact) bool {
	if left.Alias.Path != right.Alias.Path {
		return left.Alias.Path < right.Alias.Path
	}
	if left.Alias.SelectionRange != right.Alias.SelectionRange {
		return left.Alias.SelectionRange.Start < right.Alias.SelectionRange.Start
	}
	return left.TargetSpan.Start < right.TargetSpan.Start
}

func callFactLess(left, right CallFact) bool {
	if left.Caller.Path != right.Caller.Path {
		return left.Caller.Path < right.Caller.Path
	}
	if left.Caller.SelectionRange != right.Caller.SelectionRange {
		return left.Caller.SelectionRange.Start < right.Caller.SelectionRange.Start
	}
	if left.CalleeSpan != right.CalleeSpan {
		return left.CalleeSpan.Start < right.CalleeSpan.Start
	}
	return left.CalleeName < right.CalleeName
}

func functionFacts(file *syntax.File) map[syntax.Span]indexedFunctionFact {
	result := make(map[syntax.Span]indexedFunctionFact)
	var collect func([]syntax.Command)
	collect = func(commands []syntax.Command) {
		for index := range commands {
			command := &commands[index]
			if command.Function != nil {
				name := file.Text(command.Function.Name)
				parameters := make([]string, 0, len(command.Function.Parameters))
				for _, parameter := range command.Function.Parameters {
					if parameter.Name.Start >= parameter.Name.End {
						continue
					}
					label := file.Text(parameter.Name)
					if parameter.Variadic && !strings.HasPrefix(label, "...") {
						label = "..." + label
					}
					parameters = append(parameters, strings.Clone(label))
				}
				signature, _ := FormatFunctionSignature(file, name, command.Function)
				result[command.Function.Name] = indexedFunctionFact{
					signature:     signature,
					documentation: LeadingFunctionDocumentation(file, command),
					dialect:       command.Dialect,
					parameters:    parameters,
				}
			}
			if command.Embedded != nil {
				collect(command.Embedded.Commands)
			}
		}
	}
	collect(file.Commands)
	return result
}

// FormatFunctionSignature formats a function declaration signature and returns
// its full signature label along with individual parameter labels.
func FormatFunctionSignature(file *syntax.File, name string, function *syntax.Function) (string, []string) {
	if file == nil || function == nil {
		return name + "()", nil
	}
	parts := make([]string, 0, len(function.Parameters))
	for _, parameter := range function.Parameters {
		part := file.Text(parameter.Name)
		if parameter.Type != nil {
			part += ": " + file.Text(parameter.TypeSpan)
		}
		if parameter.Default != nil {
			part += " = " + file.Text(parameter.DefaultSpan)
		}
		if parameter.Variadic && !strings.HasPrefix(part, "...") {
			part = "..." + part
		}
		parts = append(parts, part)
	}
	signature := name + "(" + strings.Join(parts, ", ") + ")"
	if function.ReturnType != nil {
		signature += ": " + file.Text(function.ReturnTypeSpan)
	}
	return signature, parts
}

// LeadingFunctionDocumentation returns the consecutive comment lines directly
// above a function declaration without their comment leaders.
func LeadingFunctionDocumentation(file *syntax.File, command *syntax.Command) string {
	if file == nil || command == nil || command.Span.Start <= 0 {
		return ""
	}
	lineStart := strings.LastIndexByte(file.Source[:command.Span.Start], '\n') + 1
	before := file.Source[lineStart:command.Span.Start]
	for i := 0; i < len(before); i++ {
		if before[i] != ' ' && before[i] != '\t' {
			return ""
		}
	}
	lines := make([]string, 0)
	for lineStart > 0 {
		lineEnd := lineStart - 1
		if lineEnd > 0 && file.Source[lineEnd-1] == '\r' {
			lineEnd--
		}
		previousStart := strings.LastIndexByte(file.Source[:lineEnd], '\n') + 1
		first := previousStart
		for first < lineEnd && (file.Source[first] == ' ' || file.Source[first] == '\t') {
			first++
		}
		if first == lineEnd || !indexedCommentToken(file, first, lineEnd) {
			break
		}
		text := strings.TrimSpace(file.Source[first+1 : lineEnd])
		lines = append(lines, text)
		lineStart = previousStart
	}
	for left, right := 0, len(lines)-1; left < right; left, right = left+1, right-1 {
		lines[left], lines[right] = lines[right], lines[left]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func indexedCommentToken(file *syntax.File, start, end int) bool {
	for _, token := range file.Tokens {
		if token.Kind == syntax.TokenComment && token.Span == (syntax.Span{Start: start, End: end}) {
			return true
		}
	}
	return false
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
// import alias, an unresolved autoload call, or a legacy global function call.
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
	collectImports(file.Commands, file.Blocks, false, imports, &importNodes)
	importsByName := make(map[string][]*syntax.Import)
	for _, importNode := range importNodes {
		name := ImportAlias(file, importNode)
		if name != "" {
			importsByName[name] = append(importsByName[name], importNode)
		}
	}

	facts := make([]ExternalReferenceFact, 0)
	visited := make(map[*syntax.Expression]bool)
	directCalls := make(map[syntax.Span]bool)
	visitIndexCommands(file.Commands, visited, func(expression *syntax.Expression) {
		if expression != nil && expression.Kind == syntax.ExpressionCall && expression.Value == "" && len(expression.Children) > 0 && expression.Children[0] != nil {
			callee := expression.Children[0]
			if callee.Kind == syntax.ExpressionIdentifier {
				directCalls[callee.Span] = true
			} else if callee.Kind == syntax.ExpressionMember && callee.Value != "" && file.Text(callee.Operator) == "->" {
				directCalls[syntax.Span{Start: callee.Span.End - len(callee.Value), End: callee.Span.End}] = true
			}
		}
		if expression == nil || expression.Kind != syntax.ExpressionMember || len(expression.Children) != 1 || expression.Value == "" {
			return
		}
		member := syntax.Span{Start: expression.Operator.End, End: expression.Span.End}
		if file.Text(expression.Operator) == "->" {
			method := syntax.Span{Start: expression.Span.End - len(expression.Value), End: expression.Span.End}
			if _, ok := AutoloadPath(expression.Value); ok && directCalls[method] && validIndexSpan(file, method) && file.Text(method) == expression.Value {
				facts = append(facts, ExternalReferenceFact{
					Path: normalized, Name: strings.Clone(expression.Value), Span: method, Kind: ExternalReferenceAutoload, DirectCall: true,
				})
			}
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
		importNode := importCandidates[0]
		if reference != nil && reference.Declaration != nil {
			if reference.Declaration.Kind != analysis.SymbolKindImport {
				return
			}
			if imports[reference.Declaration.Span] != importNode {
				return
			}
		} else if (!emptyIndexSpan(importNode.Alias) && !strings.HasPrefix(receiver.Value, "s:")) || importNode.PathSpan.End > receiver.Span.Start {
			// An implicit filename alias has no lexical declaration, and a
			// legacy s: spelling can name a script-local import. Other unbound
			// receivers remain unknown instead of binding to a later import.
			return
		}
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
		if len(importNodes) != 1 || importNodes[0].PathSpan.End > typeNode.Span.Start || !validIndexSpan(file, member) || file.Text(member) != typeNode.Name[separator+1:] || !validIndexSpan(file, importNodes[0].PathSpan) {
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
		if !validIndexSpan(file, reference.Span) {
			continue
		}
		kind := ExternalReferenceAutoload
		if separator > 0 && separator < len(name)-1 {
			// Autoload variables and functions both resolve by their full name.
		} else if globalName := strings.TrimPrefix(reference.Name, "g:"); directCalls[reference.Span] &&
			(file.Dialect == syntax.Legacy || strings.HasPrefix(reference.Name, "g:")) && globalName != "" && globalName[0] >= 'A' && globalName[0] <= 'Z' {
			kind = ExternalReferenceGlobalFunction
		} else if file.Dialect == syntax.Legacy && strings.HasPrefix(reference.Name, "g:") {
			kind = ExternalReferenceGlobalVariable
		} else {
			continue
		}
		facts = append(facts, ExternalReferenceFact{
			Path: normalized, Name: strings.Clone(name), Span: reference.Span, Kind: kind, DirectCall: directCalls[reference.Span],
		})
	}
	sortExternalReferences(facts)
	return deduplicateExternalReferences(facts)
}

func emptyIndexSpan(span syntax.Span) bool {
	return span.Start >= span.End
}

func collectImports(commands []syntax.Command, blocks []syntax.Block, deferred bool, imports map[syntax.Span]*syntax.Import, all *[]*syntax.Import) {
	for index := range commands {
		command := &commands[index]
		insideFunction := deferred || syntax.CommandInsideFunction(command, blocks)
		if command.Import != nil && !insideFunction {
			*all = append(*all, command.Import)
			if command.Import.Alias.Start < command.Import.Alias.End {
				imports[command.Import.Alias] = command.Import
			}
		}
		if command.Embedded != nil {
			collectImports(command.Embedded.Commands, command.Embedded.Blocks, insideFunction, imports, all)
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

// ImportAlias returns the explicit or filename-derived name introduced by an
// import. Dynamic paths without an explicit alias have no statically known
// name.
func ImportAlias(file *syntax.File, importNode *syntax.Import) string {
	if file == nil || importNode == nil {
		return ""
	}
	if name := file.Text(importNode.Alias); name != "" {
		return name
	}
	return defaultImportName(file.Text(importNode.PathSpan))
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

func (i *Index) removeTypeRelationsLocked(facts []TypeRelationFact) {
	for _, fact := range facts {
		children := i.typesByChild[fact.Child]
		for index, candidate := range children {
			if candidate == fact {
				i.typesByChild[fact.Child] = append(children[:index], children[index+1:]...)
				if len(i.typesByChild[fact.Child]) == 0 {
					delete(i.typesByChild, fact.Child)
				}
				break
			}
		}
		parents := i.typesByParent[fact.ParentName]
		for index, candidate := range parents {
			if candidate == fact {
				i.typesByParent[fact.ParentName] = append(parents[:index], parents[index+1:]...)
				if len(i.typesByParent[fact.ParentName]) == 0 {
					delete(i.typesByParent, fact.ParentName)
				}
				break
			}
		}
	}
}

func (i *Index) removeTypeAliasesLocked(facts []TypeAliasFact) {
	for _, fact := range facts {
		aliases := i.aliasesByTarget[fact.TargetName]
		for index, candidate := range aliases {
			if candidate == fact {
				i.aliasesByTarget[fact.TargetName] = append(aliases[:index], aliases[index+1:]...)
				if len(i.aliasesByTarget[fact.TargetName]) == 0 {
					delete(i.aliasesByTarget, fact.TargetName)
				}
				break
			}
		}
	}
}

func (i *Index) removeCallsLocked(facts []CallFact) {
	for _, fact := range facts {
		callers := i.callsByCaller[fact.Caller]
		for index, candidate := range callers {
			if candidate == fact {
				i.callsByCaller[fact.Caller] = append(callers[:index], callers[index+1:]...)
				if len(i.callsByCaller[fact.Caller]) == 0 {
					delete(i.callsByCaller, fact.Caller)
				}
				break
			}
		}
		callees := i.callsByCallee[fact.CalleeName]
		for index, candidate := range callees {
			if candidate == fact {
				i.callsByCallee[fact.CalleeName] = append(callees[:index], callees[index+1:]...)
				if len(i.callsByCallee[fact.CalleeName]) == 0 {
					delete(i.callsByCallee, fact.CalleeName)
				}
				break
			}
		}
	}
}

func (i *Index) removeUserCommandsLocked(facts []UserCommandFact) {
	for _, fact := range facts {
		matches := i.byUserCommand[fact.Name]
		for index, candidate := range matches {
			if candidate == fact {
				i.byUserCommand[fact.Name] = append(matches[:index], matches[index+1:]...)
				if len(i.byUserCommand[fact.Name]) == 0 {
					delete(i.byUserCommand, fact.Name)
				}
				break
			}
		}
	}
}

func (i *Index) removeAugroupsLocked(facts []AugroupFact) {
	for _, fact := range facts {
		matches := i.byAugroup[fact.Name]
		for index := range matches {
			if matches[index] == fact {
				i.byAugroup[fact.Name] = append(matches[:index], matches[index+1:]...)
				if len(i.byAugroup[fact.Name]) == 0 {
					delete(i.byAugroup, fact.Name)
				}
				break
			}
		}
	}
}

func (i *Index) removeGlobalNamesLocked(facts []GlobalNameFact) {
	for _, fact := range facts {
		matches := i.byGlobalName[fact.Name]
		for index, candidate := range matches {
			if candidate == fact {
				i.byGlobalName[fact.Name] = append(matches[:index], matches[index+1:]...)
				if len(i.byGlobalName[fact.Name]) == 0 {
					delete(i.byGlobalName, fact.Name)
				}
				break
			}
		}
	}
}
