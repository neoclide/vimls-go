package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestIndexReplaceWithAnalysisReusesSuppliedResultAndNilFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.vim")
	file := syntax.Parse("vim9script\nimport './library.vim' as library\necho library.Public()\n")

	provided := analysis.Analyze(file)
	provided.References = nil
	withProvided := NewIndex(10, 10000)
	if err := withProvided.ReplaceWithAnalysis(path, file, provided); err != nil {
		t.Fatal(err)
	}
	if references := withProvided.ExternalReferences("Public"); len(references) != 0 {
		t.Fatalf("supplied analysis was not reused: external references = %#v", references)
	}

	withFallback := NewIndex(10, 10000)
	if err := withFallback.ReplaceWithAnalysis(path, file, nil); err != nil {
		t.Fatal(err)
	}
	references := withFallback.ExternalReferences("Public")
	if len(references) != 1 || references[0].Fact.Kind != ExternalReferenceImportMember {
		t.Fatalf("nil analysis did not fall back to analysis: external references = %#v", references)
	}
}

func TestIndexLookupIncludesNestedSymbols(t *testing.T) {
	index := NewIndex(10, 10000)
	path := filepath.Join(t.TempDir(), "nested.vim")
	file := syntax.Parse("vim9script\nclass Widget\n  def run()\n    var inside = 1\n  enddef\nendclass\n")
	if err := index.Replace(path, file); err != nil {
		t.Fatal(err)
	}
	class := index.Lookup("Widget")
	if len(class) != 1 || class[0].Path != mustResolverCanonical(t, path) || class[0].Kind != analysis.SymbolKindClass {
		t.Fatalf("class lookup = %#v", class)
	}
	method := index.Lookup("run")
	if len(method) != 1 || method[0].Kind != analysis.SymbolKindMethod {
		t.Fatalf("method lookup = %#v", method)
	}
	variable := index.Lookup("inside")
	if len(variable) != 1 || variable[0].Kind != analysis.SymbolKindVariable {
		t.Fatalf("nested lookup = %#v", variable)
	}
}

func TestIndexRelationshipsTrackReplaceAndRemove(t *testing.T) {
	index := NewIndex(10, 10000)
	path := filepath.Join(t.TempDir(), "relations.vim")
	source := "vim9script\nabstract class Base\nendclass\ntype Alias = Base\nclass Child extends Base\n  static def Build()\n  enddef\nendclass\ndef Target()\nenddef\ndef Caller()\n  Target()\n  Child.Build()\nenddef\n"
	if err := index.Replace(path, syntax.Parse(source)); err != nil {
		t.Fatal(err)
	}
	base := index.Lookup("Base")
	child := index.Lookup("Child")
	build := index.Lookup("Build")
	caller := index.Lookup("Caller")
	if len(base) != 1 || !base[0].Abstract || len(child) != 1 || len(build) != 1 || !build[0].Static || build[0].OwnerSelectionRange != child[0].SelectionRange || len(caller) != 1 {
		t.Fatalf("symbol metadata: base=%#v child=%#v build=%#v caller=%#v", base, child, build, caller)
	}
	types := index.TypeRelations(child[0].Key())
	if len(types) != 1 || types[0].Fact.ParentName != "Base" || types[0].Fact.Kind != analysis.TypeRelationExtends {
		t.Fatalf("child relations = %#v", types)
	}
	if candidates := index.TypeRelationCandidates("Base"); len(candidates) != 1 || candidates[0].Fact != types[0].Fact {
		t.Fatalf("type candidates = %#v", candidates)
	}
	aliases := index.TypeAliasCandidates("Base")
	if len(aliases) != 1 || aliases[0].Fact.AliasName != "Alias" || aliases[0].Fact.TargetName != "Base" {
		t.Fatalf("type alias candidates = %#v", aliases)
	}
	calls := index.Calls(caller[0].Key())
	if len(calls) != 2 || calls[0].Fact.CalleeName != "Target" || calls[1].Fact.CalleeName != "Build" {
		t.Fatalf("caller relations = %#v", calls)
	}
	if candidates := index.CallCandidates("Target"); len(candidates) != 1 || candidates[0].Fact.Caller != caller[0].Key() {
		t.Fatalf("call candidates = %#v", candidates)
	}

	if err := index.Replace(path, syntax.Parse("vim9script\nclass Replacement\nendclass\n")); err != nil {
		t.Fatal(err)
	}
	if len(index.TypeRelationCandidates("Base")) != 0 || len(index.TypeAliasCandidates("Base")) != 0 || len(index.CallCandidates("Target")) != 0 {
		t.Fatalf("replace retained relationships: types=%#v aliases=%#v calls=%#v", index.TypeRelationCandidates("Base"), index.TypeAliasCandidates("Base"), index.CallCandidates("Target"))
	}
	if err := index.Replace(path, syntax.Parse(source)); err != nil {
		t.Fatal(err)
	}
	index.Remove(path)
	if len(index.TypeRelationCandidates("Base")) != 0 || len(index.TypeAliasCandidates("Base")) != 0 || len(index.CallCandidates("Target")) != 0 {
		t.Fatalf("remove retained relationships: types=%#v aliases=%#v calls=%#v", index.TypeRelationCandidates("Base"), index.TypeAliasCandidates("Base"), index.CallCandidates("Target"))
	}
}

func TestIndexRelationshipLimitsKeepOrdinaryFactsAndCompletenessSeparate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "limited.vim")
	index := NewIndex(10, 10000, 1, 10)
	overflow := syntax.Parse("vim9script\ndef Target()\nenddef\ndef Caller()\n  Target()\n  Target()\nenddef\n")
	if err := index.Replace(path, overflow); err != nil {
		t.Fatal(err)
	}
	index.SetComplete(true)
	if !index.Complete() || index.RelationshipsComplete() || len(index.Lookup("Caller")) != 1 || len(index.CallCandidates("Target")) != 0 {
		t.Fatalf("overflow state: complete=%t relationships=%t caller=%#v calls=%#v", index.Complete(), index.RelationshipsComplete(), index.Lookup("Caller"), index.CallCandidates("Target"))
	}
	withinLimit := syntax.Parse("vim9script\ndef Target()\nenddef\ndef Caller()\n  Target()\nenddef\n")
	if err := index.Replace(path, withinLimit); err != nil {
		t.Fatal(err)
	}
	if !index.RelationshipsComplete() || len(index.CallCandidates("Target")) != 1 {
		t.Fatalf("recovered relationship state: complete=%t calls=%#v", index.RelationshipsComplete(), index.CallCandidates("Target"))
	}

	other := filepath.Join(root, "other.vim")
	totalLimited := NewIndex(10, 10000, 10, 1)
	if err := totalLimited.Replace(path, withinLimit); err != nil {
		t.Fatal(err)
	}
	if err := totalLimited.Replace(other, withinLimit); err != nil {
		t.Fatal(err)
	}
	totalLimited.SetComplete(true)
	if totalLimited.RelationshipsComplete() {
		t.Fatal("total relationship overflow reported complete")
	}
	totalLimited.Remove(other)
	if !totalLimited.RelationshipsComplete() {
		t.Fatal("removing overflow file did not restore completeness")
	}
}

func BenchmarkIndexRelationshipFacts(b *testing.B) {
	var source strings.Builder
	source.WriteString("vim9script\ndef Target()\nenddef\ndef Caller()\n")
	for range 1000 {
		source.WriteString("  Target()\n")
	}
	source.WriteString("enddef\n")
	file := syntax.Parse(source.String())
	result := analysis.Analyze(file)
	facts := CollectCallFactsFromAnalysis(filepath.Join(b.TempDir(), "calls.vim"), file, result)
	if len(facts) != 1000 {
		b.Fatalf("call facts = %d", len(facts))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		CollectCallFactsFromAnalysis(filepath.Join("bench", "calls.vim"), file, result)
	}
	b.ReportMetric(float64(unsafe.Sizeof(CallFact{})), "call-fact-B")
	b.ReportMetric(float64(unsafe.Sizeof(TypeRelationFact{})), "type-fact-B")
	b.ReportMetric(float64(unsafe.Sizeof(TypeAliasFact{})), "alias-fact-B")
}

func TestCollectSymbolFactsKeepsDeprecatedVim9Declarations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deprecated.vim")
	facts := CollectSymbolFacts(path, syntax.Parse("vim9script\n# deprecated\nexport var OldValue = 1\n# @deprecated\nexport def OldFunc()\nenddef\n"))
	deprecated := map[string]bool{}
	for _, fact := range facts {
		if fact.Name == "OldValue" || fact.Name == "OldFunc" {
			deprecated[fact.Name] = fact.Deprecated && fact.Exported
		}
	}
	if !deprecated["OldValue"] || !deprecated["OldFunc"] {
		t.Fatalf("deprecated facts = %#v; all = %#v", deprecated, facts)
	}
}

func TestIndexLookupOrdersSameNamesAndReplaceRemovesOldSymbols(t *testing.T) {
	index := NewIndex(10, 10000)
	root := t.TempDir()
	first := filepath.Join(root, "z.vim")
	second := filepath.Join(root, "a.vim")
	if err := index.Replace(first, syntax.Parse("var same = 1\n")); err != nil {
		t.Fatal(err)
	}
	if err := index.Replace(second, syntax.Parse("var same = 2\nvar same = 3\n")); err != nil {
		t.Fatal(err)
	}
	got := index.Lookup("same")
	if len(got) != 3 || got[0].Path != mustResolverCanonical(t, second) || got[1].Path != mustResolverCanonical(t, second) || got[2].Path != mustResolverCanonical(t, first) {
		t.Fatalf("same-name order = %#v", got)
	}
	if got[0].SelectionRange.Start >= got[1].SelectionRange.Start {
		t.Fatalf("same-file spans are not ordered: %#v", got)
	}
	if err := index.Replace(first, syntax.Parse("var replacement = 1\n")); err != nil {
		t.Fatal(err)
	}
	if len(index.Lookup("same")) != 2 || len(index.Lookup("replacement")) != 1 {
		t.Fatalf("replace did not remove old facts: same=%#v replacement=%#v", index.Lookup("same"), index.Lookup("replacement"))
	}
}

func TestIndexUserCommandNamesTrackReplaceAndRemove(t *testing.T) {
	index := NewIndex(10, 10000)
	root := t.TempDir()
	first := filepath.Join(root, "first.vim")
	second := filepath.Join(root, "second.vim")
	if err := index.Replace(first, syntax.Parse("command! BuildProject echo 'value'\ncommand Build\n")); err != nil {
		t.Fatal(err)
	}
	if err := index.Replace(second, syntax.Parse("command -buffer LocalBuild echo 'value'\n")); err != nil {
		t.Fatal(err)
	}
	if got := index.UserCommandNames(); len(got) != 2 || got[0] != "BuildProject" || got[1] != "LocalBuild" {
		t.Fatalf("user command names = %#v", got)
	}
	if err := index.Replace(first, syntax.Parse("command! TestProject echo 'value'\n")); err != nil {
		t.Fatal(err)
	}
	if got := index.UserCommandNames(); len(got) != 2 || got[0] != "LocalBuild" || got[1] != "TestProject" {
		t.Fatalf("replaced user command names = %#v", got)
	}
	index.Remove(second)
	if got := index.UserCommandNames(); len(got) != 1 || got[0] != "TestProject" {
		t.Fatalf("removed user command names = %#v", got)
	}
	index.SetComplete(true)
	if !index.Complete() {
		t.Fatal("complete index was not recorded")
	}
}

func TestIndexGlobalNameFactsTrackLocationsAndDeletes(t *testing.T) {
	index := NewIndex(10, 10000)
	root := t.TempDir()
	path := filepath.Join(root, "globals.vim")
	source := "function Shared()\nendfunction\nfunction Gone()\nendfunction\ndelfunction Gone\nlet g:Value = 1\nlet g:Removed = 1\nunlet g:Removed\nlet s:ScriptOnly = 1\n"
	if err := index.Replace(path, syntax.Parse(source)); err != nil {
		t.Fatal(err)
	}
	functions := index.GlobalNameFacts("Shared")
	variables := index.GlobalNameFacts("Value")
	if len(functions) != 1 || functions[0].Kind != analysis.NameDeclarationFunction || functions[0].Span.Start != 9 {
		t.Fatalf("global function facts = %#v", functions)
	}
	if len(variables) != 1 || variables[0].Kind != analysis.NameDeclarationVariable || variables[0].Span.Start <= functions[0].Span.Start {
		t.Fatalf("global variable facts = %#v", variables)
	}
	if match, ok := index.GlobalVariable("g:Value"); !ok || match.Fact.Name != "g:Value" || match.Fact.SelectionRange != variables[0].Span {
		t.Fatalf("global variable match = %#v, %t", match, ok)
	}
	if len(index.GlobalNameFacts("Gone")) != 0 || len(index.GlobalNameFacts("Removed")) != 0 || len(index.GlobalNameFacts("ScriptOnly")) != 0 {
		t.Fatalf("deleted or script-local facts leaked: gone=%#v removed=%#v script=%#v", index.GlobalNameFacts("Gone"), index.GlobalNameFacts("Removed"), index.GlobalNameFacts("ScriptOnly"))
	}
	if match, ok := index.GlobalFunction("Gone"); ok {
		t.Fatalf("deleted global function resolved to %#v", match)
	}
	if match, ok := index.GlobalVariable("Removed"); ok {
		t.Fatalf("deleted global variable resolved to %#v", match)
	}
	otherPath := filepath.Join(root, "other.vim")
	if err := index.Replace(otherPath, syntax.Parse("let g:Another = 1\nlet g:Shared = 1\n")); err != nil {
		t.Fatal(err)
	}
	index.SetComplete(true)
	completions, incomplete := index.GlobalVariableCompletions("", "", 10)
	if incomplete || len(completions) != 2 || completions[0].Name != "Another" || completions[1].Name != "Value" {
		t.Fatalf("global variable completions = %#v, incomplete=%t", completions, incomplete)
	}
	if limited, incomplete := index.GlobalVariableCompletions("", "", 1); !incomplete || len(limited) != 1 || limited[0].Name != "Another" {
		t.Fatalf("limited global variable completions = %#v, incomplete=%t", limited, incomplete)
	}
	index.SetComplete(false)
	if matches, incomplete := index.GlobalVariableCompletions("Val", "", 10); !incomplete || len(matches) != 1 || matches[0].Name != "Value" {
		t.Fatalf("incomplete global variable completions = %#v, incomplete=%t", matches, incomplete)
	}
	if matches, _ := index.GlobalVariableCompletions("Val", path, 10); len(matches) != 0 {
		t.Fatalf("excluded current-file variables = %#v", matches)
	}
}

func TestIndexGlobalNameConflictDiagnostics(t *testing.T) {
	index := NewIndex(10, 10000)
	root := t.TempDir()
	functionPath := filepath.Join(root, "function.vim")
	variablePath := filepath.Join(root, "variable.vim")
	if err := index.Replace(functionPath, syntax.Parse("function Shared()\nendfunction\n")); err != nil {
		t.Fatal(err)
	}
	variableFile := syntax.Parse("let g:Shared = 1\n")
	if got := index.GlobalNameConflictDiagnostics(variablePath, variableFile); len(got) != 1 || got[0].Code != "vim/E705" || variableFile.Text(got[0].Span) != "g:Shared" {
		t.Fatalf("E705 diagnostics = %#v", got)
	}
	if err := index.Replace(variablePath, variableFile); err != nil {
		t.Fatal(err)
	}
	otherFunctionPath := filepath.Join(root, "other-function.vim")
	otherFunction := syntax.Parse("function g:Shared()\nendfunction\n")
	if got := index.GlobalNameConflictDiagnostics(otherFunctionPath, otherFunction); len(got) != 1 || got[0].Code != "vim/E707" || otherFunction.Text(got[0].Span) != "g:Shared" {
		t.Fatalf("E707 diagnostics = %#v", got)
	}

	localConflict := syntax.Parse("function LocalOnly()\nendfunction\nlet g:LocalOnly = 1\n")
	localPath := filepath.Join(root, "local.vim")
	if err := index.Replace(localPath, localConflict); err != nil {
		t.Fatal(err)
	}
	if got := index.GlobalNameConflictDiagnostics(localPath, localConflict); len(got) != 0 {
		t.Fatalf("same-file conflict duplicated by index: %#v", got)
	}

	deletedPath := filepath.Join(root, "deleted.vim")
	if err := index.Replace(deletedPath, syntax.Parse("function Deleted()\nendfunction\ndelfunction Deleted\n")); err != nil {
		t.Fatal(err)
	}
	deletedVariable := syntax.Parse("let g:Deleted = 1\n")
	if got := index.GlobalNameConflictDiagnostics(filepath.Join(root, "deleted-variable.vim"), deletedVariable); len(got) != 0 {
		t.Fatalf("deleted indexed function produced warning: %#v", got)
	}
}

func TestIndexSearchRanksSubsequencesAndLimitsResults(t *testing.T) {
	index := NewIndex(10, 10000)
	root := t.TempDir()
	path := filepath.Join(root, "symbols.vim")
	source := "var Exact = 1\nvar exactly = 2\nvar e_x_a_c_t = 3\nvar unrelated = 4\n"
	if err := index.Replace(path, syntax.Parse(source)); err != nil {
		t.Fatal(err)
	}
	results := index.Search("EXACT", 0)
	if len(results) != 3 {
		t.Fatalf("search result count = %d, want 3: %#v", len(results), results)
	}
	if got := []string{results[0].Fact.Name, results[1].Fact.Name, results[2].Fact.Name}; got[0] != "Exact" || got[1] != "exactly" || got[2] != "e_x_a_c_t" {
		t.Fatalf("search ranking = %#v", got)
	}
	if results[0].Source != source || results[1].Source != source || results[2].Source != source {
		t.Fatalf("search source = %#v", results)
	}
	if got := index.Search("x_a_c", 1); len(got) != 1 || got[0].Fact.Name != "e_x_a_c_t" {
		t.Fatalf("subsequence or limit result = %#v", got)
	}
	if got := index.Search("", -1); len(got) != 4 {
		t.Fatalf("empty query result count = %d", len(got))
	}
}

func TestIndexSearchReturnsIndependentFactsAndRetainsCurrentSource(t *testing.T) {
	index := NewIndex(10, 10000)
	root := t.TempDir()
	path := filepath.Join(root, "source.vim")
	oldSource := "var old = 1\n"
	oldFile := syntax.Parse(oldSource)
	if err := index.Replace(path, oldFile); err != nil {
		t.Fatal(err)
	}
	if retained, ok := index.Source(path); !ok || retained != oldSource {
		t.Fatalf("Source() = %q, %v", retained, ok)
	}
	oldFile.Source = "var mutated = 2\n"
	results := index.Search("old", 0)
	if len(results) != 1 || results[0].Source != oldSource {
		t.Fatalf("retained source = %#v", results)
	}
	results[0].Fact.Name = "changed"
	results[0].Fact.Range.Start = 999
	results[0].Source = "changed"
	results = index.Search("old", 0)
	if len(results) != 1 || results[0].Fact.Name != "old" || results[0].Fact.Range.Start == 999 || results[0].Source != oldSource {
		t.Fatalf("search leaked mutable result: %#v", results)
	}

	newSource := "var replacement = 123\n"
	if err := index.Replace(path, syntax.Parse(newSource)); err != nil {
		t.Fatal(err)
	}
	if index.IndexedBytes() != len(newSource) || len(index.Search("old", 0)) != 0 {
		t.Fatalf("replacement state: bytes=%d old=%#v", index.IndexedBytes(), index.Search("old", 0))
	}
	if retained, ok := index.Source(path); !ok || retained != newSource {
		t.Fatalf("replacement Source() = %q, %v", retained, ok)
	}
	results = index.Search("replacement", 0)
	if len(results) != 1 || results[0].Source != newSource {
		t.Fatalf("replacement source = %#v", results)
	}
	index.Remove(path)
	if retained, ok := index.Source(path); ok || retained != "" {
		t.Fatalf("removed Source() = %q, %v", retained, ok)
	}
	if index.IndexedBytes() != 0 || len(index.Search("replacement", 0)) != 0 {
		t.Fatalf("removal state: bytes=%d results=%#v", index.IndexedBytes(), index.Search("replacement", 0))
	}
}

func TestIndexCollectsExportedSymbolsAndStaticExternalReferences(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.vim")
	source := `vim9script
import './lib.vim' as lib
var first = lib.Run()
echo lib.Value
var typed: lib.Public<number>
echo unknown.Run()
export def Public()
enddef
def Private()
enddef
`
	file := syntax.Parse(source)
	facts := CollectSymbolFacts(path, file)
	exported := make(map[string]bool)
	for _, fact := range facts {
		exported[fact.Name] = fact.Exported
	}
	if !exported["Public"] || exported["Private"] || exported["first"] {
		t.Fatalf("exported facts = %#v", facts)
	}
	references := CollectExternalReferences(path, file)
	if len(references) != 3 || references[0].Name != "Run" || references[1].Name != "Value" || references[2].Name != "Public" {
		t.Fatalf("import references = %#v", references)
	}
	for _, reference := range references {
		if reference.Kind != ExternalReferenceImportMember || reference.ImportPath != "'./lib.vim'" || file.Text(reference.Span) != reference.Name {
			t.Fatalf("import reference = %#v", reference)
		}
	}
	defaultImport := syntax.Parse("vim9script\nimport autoload 'for/search.vim'\necho search.Run()\n")
	defaultReferences := CollectExternalReferences(filepath.Join(root, "default.vim"), defaultImport)
	if len(defaultReferences) != 1 || defaultReferences[0].Name != "Run" || defaultReferences[0].ImportPath != "'for/search.vim'" || !defaultReferences[0].ImportAutoload {
		t.Fatalf("default import reference = %#v", defaultReferences)
	}
	ambiguousImport := syntax.Parse("vim9script\nimport './one.vim' as duplicate\nimport './two.vim' as duplicate\necho duplicate.Run()\n")
	if ambiguousReferences := CollectExternalReferences(filepath.Join(root, "ambiguous.vim"), ambiguousImport); len(ambiguousReferences) != 0 {
		t.Fatalf("ambiguous import references = %#v", ambiguousReferences)
	}
	forwardImport := syntax.Parse("vim9script\necho lib.Run()\nvar typed: lib.Public<number>\nimport './lib.vim' as lib\n")
	if forwardReferences := CollectExternalReferences(filepath.Join(root, "forward.vim"), forwardImport); len(forwardReferences) != 0 {
		t.Fatalf("forward import references = %#v", forwardReferences)
	}
	functionImport := syntax.Parse("vim9script\ndef Deferred()\n  import './lib.vim' as lib\n  echo lib.Run()\nenddef\n")
	if functionReferences := CollectExternalReferences(filepath.Join(root, "function.vim"), functionImport); len(functionReferences) != 0 {
		t.Fatalf("function-local import references = %#v", functionReferences)
	}

	legacyPath := filepath.Join(root, "legacy.vim")
	legacySource := "call foo#bar#Run()\nlet value = g:foo#bar#Value\necho g:WorkspaceValue\n"
	legacy := syntax.Parse(legacySource)
	legacyReferences := CollectExternalReferences(legacyPath, legacy)
	if len(legacyReferences) != 3 || legacyReferences[0].Name != "foo#bar#Run" || legacyReferences[1].Name != "foo#bar#Value" || legacyReferences[2].Name != "WorkspaceValue" || legacyReferences[2].Kind != ExternalReferenceGlobalVariable {
		t.Fatalf("autoload references = %#v", legacyReferences)
	}
	for _, reference := range legacyReferences[:2] {
		if reference.Kind != ExternalReferenceAutoload {
			t.Fatalf("autoload reference = %#v", reference)
		}
	}
	if !legacyReferences[0].DirectCall || legacyReferences[1].DirectCall {
		t.Fatalf("autoload call classification = %#v", legacyReferences[:2])
	}
	method := syntax.Parse("vim9script\n[1]->  foo#bar#Transform()\n")
	methodReferences := CollectExternalReferences(filepath.Join(root, "method.vim"), method)
	if len(methodReferences) != 1 || methodReferences[0].Name != "foo#bar#Transform" || !methodReferences[0].DirectCall {
		t.Fatalf("autoload method references = %#v; syntax diagnostics = %#v", methodReferences, method.Diagnostics)
	}
	vim9Global := syntax.Parse("vim9script\ng:MissingGlobal()\n")
	vim9GlobalReferences := CollectExternalReferences(filepath.Join(root, "vim9-global.vim"), vim9Global)
	if len(vim9GlobalReferences) != 1 || vim9GlobalReferences[0].Name != "MissingGlobal" || vim9GlobalReferences[0].Kind != ExternalReferenceGlobalFunction || !vim9GlobalReferences[0].DirectCall {
		t.Fatalf("Vim9 global function references = %#v; syntax diagnostics = %#v", vim9GlobalReferences, vim9Global.Diagnostics)
	}

	index := NewIndex(10, 10000)
	if err := index.Replace(path, file); err != nil {
		t.Fatal(err)
	}
	if err := index.Replace(legacyPath, legacy); err != nil {
		t.Fatal(err)
	}
	if got := index.ExternalReferences("Run"); len(got) != 1 || got[0].Fact.Path != mustResolverCanonical(t, path) || got[0].Source != source {
		t.Fatalf("indexed import references = %#v", got)
	}
	if got := index.ExternalReferences("foo#bar#Run"); len(got) != 1 || got[0].Fact.Path != mustResolverCanonical(t, legacyPath) || got[0].Source != legacySource {
		t.Fatalf("indexed autoload references = %#v", got)
	}
	if got := index.LookupFile(path, "Public"); len(got) != 1 || !got[0].Fact.Exported || got[0].Source != source {
		t.Fatalf("file lookup = %#v", got)
	}
	if err := index.Replace(path, syntax.Parse("vim9script\n")); err != nil {
		t.Fatal(err)
	}
	if got := index.ExternalReferences("Run"); len(got) != 0 {
		t.Fatalf("replaced references = %#v", got)
	}
}

func TestIndexUsesCanonicalPathIdentity(t *testing.T) {
	realRoot := t.TempDir()
	aliasRoot := filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	realPath := filepath.Join(realRoot, "main.vim")
	aliasPath := filepath.Join(aliasRoot, "main.vim")
	index := NewIndex(1, 1000)
	if err := index.Replace(aliasPath, syntax.Parse("var old = 1\n")); err != nil {
		t.Fatal(err)
	}
	if _, ok := index.Source(realPath); !ok {
		t.Fatal("canonical path did not find symlink-indexed source")
	}
	if err := index.Replace(realPath, syntax.Parse("var current = 1\n")); err != nil {
		t.Fatal(err)
	}
	if index.FileCount() != 1 || len(index.Lookup("old")) != 0 || len(index.Lookup("current")) != 1 {
		t.Fatalf("canonical replacement state: files=%d old=%#v current=%#v", index.FileCount(), index.Lookup("old"), index.Lookup("current"))
	}
	index.Remove(aliasPath)
	if index.FileCount() != 0 {
		t.Fatalf("canonical removal left %d files", index.FileCount())
	}
}

func TestIndexRuntimeFileCatalogUsesPrecedenceAndUpdates(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	after := filepath.Join(first, "after")
	index := NewIndex(20, 10000)
	index.SetRuntimePaths([]string{first, after, second})
	paths := []string{
		filepath.Join(first, "colors", "shared.vim"),
		filepath.Join(first, "colors", "my-dark.vim"),
		filepath.Join(after, "colors", "after-dark.vim"),
		filepath.Join(after, "import", "after-only.vim"),
		filepath.Join(second, "colors", "shared.vim"),
		filepath.Join(first, "import", "pkg", "api.vim"),
		filepath.Join(first, "autoload", "pkg", "nested", "api.vim"),
	}
	for _, path := range paths {
		if err := index.Replace(path, syntax.Parse("vim9script\n")); err != nil {
			t.Fatal(err)
		}
	}
	index.SetComplete(true)
	colors, incomplete := index.ColorSchemeCompletions("", 10)
	if incomplete || len(colors) != 3 || colors[0].Display != "after-dark" || colors[1].Display != "my-dark" || colors[2].Display != "shared" || colors[2].Path != mustResolverCanonical(t, paths[0]) {
		t.Fatalf("colorscheme catalog = %#v, incomplete=%v", colors, incomplete)
	}
	colors, incomplete = index.ColorSchemeCompletions("my-", 1)
	if incomplete || len(colors) != 1 || colors[0].Display != "my-dark" {
		t.Fatalf("prefixed colorscheme catalog = %#v, incomplete=%v", colors, incomplete)
	}
	imports, incomplete := index.RuntimePathCompletions("import", "pkg/", 10)
	if incomplete || len(imports) != 1 || imports[0].Display != "pkg/api.vim" || imports[0].IsDir {
		t.Fatalf("import catalog = %#v, incomplete=%v", imports, incomplete)
	}
	autoloads, incomplete := index.RuntimePathCompletions("autoload", "pkg/", 10)
	if incomplete || len(autoloads) != 1 || autoloads[0].Display != "pkg/nested/" || !autoloads[0].IsDir {
		t.Fatalf("autoload catalog = %#v, incomplete=%v", autoloads, incomplete)
	}
	if path, ok := index.RuntimeFile("import/pkg/api.vim"); !ok || path != mustResolverCanonical(t, paths[5]) {
		t.Fatalf("runtime file = %q, %v", path, ok)
	}
	if path, ok := index.RuntimeFile("import/after-only.vim"); ok || path != "" {
		t.Fatalf("after import leaked into runtime lookup = %q, %v", path, ok)
	}
	colors, incomplete = index.ColorSchemeCompletions("", 1)
	if !incomplete || len(colors) != 1 || colors[0].Display != "after-dark" {
		t.Fatalf("limited colorscheme catalog = %#v, incomplete=%v", colors, incomplete)
	}
	index.Remove(paths[5])
	if path, ok := index.RuntimeFile("import/pkg/api.vim"); ok || path != "" {
		t.Fatalf("removed runtime file = %q, %v", path, ok)
	}
	index.SetComplete(false)
	if _, incomplete := index.ColorSchemeCompletions("", 10); !incomplete {
		t.Fatal("incomplete source table did not mark completion incomplete")
	}
}

func TestIndexFunctionCompletionsRecordSignaturesCommentsAndVim9AutoloadNames(t *testing.T) {
	root := t.TempDir()
	index := NewIndex(10, 10000)
	index.SetRuntimePaths([]string{root})
	vim9Path := filepath.Join(root, "autoload", "for", "search.vim")
	vim9 := syntax.Parse("vim9script\n# Search for matching items.\n# Uses the runtime cache.\nexport def Stuff(arg: string, count = 1): bool\n  return true\nenddef\n")
	if err := index.Replace(vim9Path, vim9); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(root, "plugin", "legacy.vim")
	if err := index.Replace(legacyPath, syntax.Parse("\" Run a legacy task.\nfunction GlobalRun(arg, ...)\nendfunction\n")); err != nil {
		t.Fatal(err)
	}
	if err := index.Replace(filepath.Join(root, "plugin", "vim9.vim"), syntax.Parse("vim9script\ndef ScriptLocal()\nenddef\n")); err != nil {
		t.Fatal(err)
	}
	index.SetComplete(true)

	vim9Matches, incomplete := index.FunctionCompletions("for#sea", false, 10)
	if incomplete || len(vim9Matches) != 1 || vim9Matches[0].Name != "for#search#Stuff" || !reflect.DeepEqual(vim9Matches[0].Parameters, []string{"arg", "count"}) {
		t.Fatalf("Vim9 autoload completions = %#v, incomplete=%t", vim9Matches, incomplete)
	}
	fact := vim9Matches[0].Match.Fact
	if fact.Name != "Stuff" || fact.Signature != "Stuff(arg: string, count = 1): bool" || fact.Documentation != "Search for matching items.\nUses the runtime cache." || !fact.Exported || fact.Dialect != syntax.Vim9 {
		t.Fatalf("Vim9 autoload fact = %#v", fact)
	}
	legacyMatches, incomplete := index.FunctionCompletions("Global", true, 10)
	if incomplete || len(legacyMatches) != 1 || legacyMatches[0].Name != "GlobalRun" || !reflect.DeepEqual(legacyMatches[0].Parameters, []string{"arg"}) || legacyMatches[0].Match.Fact.Signature != "GlobalRun(arg, ...)" || legacyMatches[0].Match.Fact.Documentation != "Run a legacy task." {
		t.Fatalf("legacy function completions = %#v, incomplete=%t", legacyMatches, incomplete)
	}
	if matches, _ := index.FunctionCompletions("Global", false, 10); len(matches) != 0 {
		t.Fatalf("legacy globals leaked into Vim9 completion = %#v", matches)
	}
	match, ok := index.GlobalFunction("GlobalRun")
	if !ok || match.Fact.Path != mustResolverCanonical(t, legacyPath) {
		t.Fatalf("global function = %#v, %t", match, ok)
	}
	if match, ok := index.GlobalFunction("ScriptLocal"); ok {
		t.Fatalf("Vim9 script-local function treated as global: %#v", match)
	}
}

func TestIndexAutoloadFunctionLookupAndDependents(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	index := NewIndex(20, 10000)
	index.SetRuntimePaths([]string{first, second})
	legacyPath := filepath.Join(first, "autoload", "foo", "bar.vim")
	vim9Path := filepath.Join(first, "autoload", "vim9", "api.vim")
	shadowedPath := filepath.Join(second, "autoload", "foo", "bar.vim")
	globalPath := filepath.Join(first, "plugin", "globals.vim")
	callerPath := filepath.Join(first, "plugin", "caller.vim")
	for path, source := range map[string]string{
		legacyPath:   "function foo#bar#Known() abort\nendfunction\n",
		vim9Path:     "vim9script\nexport def Run()\nenddef\ndef Private()\nenddef\n",
		shadowedPath: "function foo#bar#ShadowOnly() abort\nendfunction\n",
		globalPath:   "function GlobalRun() abort\nendfunction\n",
		callerPath:   "call foo#bar#Known()\ncall foo#bar#Missing()\ncall vim9#api#Run()\ncall GlobalRun()\nlet value = g:foo#bar#Value\n",
	} {
		if err := index.Replace(path, syntax.Parse(source)); err != nil {
			t.Fatal(err)
		}
	}
	if !index.HasAutoloadFunction("foo#bar#Known") || !index.HasAutoloadFunction("vim9#api#Run") {
		t.Fatal("known autoload functions were not found")
	}
	if index.HasAutoloadFunction("foo#bar#Missing") || index.HasAutoloadFunction("foo#bar#ShadowOnly") || index.HasAutoloadFunction("vim9#api#Private") {
		t.Fatal("missing, shadowed, or private autoload function was found")
	}
	wantCaller := mustResolverCanonical(t, callerPath)
	for _, target := range []string{legacyPath, vim9Path} {
		if dependents := index.AutoloadDependents(target); !reflect.DeepEqual(dependents, []string{wantCaller}) {
			t.Fatalf("autoload dependents for %s = %#v, want %#v", target, dependents, []string{wantCaller})
		}
	}
	if dependents := index.GlobalFunctionDependents(globalPath); !reflect.DeepEqual(dependents, []string{wantCaller}) {
		t.Fatalf("global function dependents = %#v, want %#v", dependents, []string{wantCaller})
	}
}

func TestIndexFunctionCompletionsTruncateDeterministically(t *testing.T) {
	index := NewIndex(10, 10000)
	path := filepath.Join(t.TempDir(), "functions.vim")
	file := syntax.Parse("function TruncateCharlie()\nendfunction\nfunction TruncateAlpha()\nendfunction\nfunction TruncateBravo()\nendfunction\n")
	if err := index.Replace(path, file); err != nil {
		t.Fatal(err)
	}
	index.SetComplete(true)

	for range 2 {
		matches, incomplete := index.FunctionCompletions("Truncate", true, 2)
		if !incomplete || len(matches) != 2 || matches[0].Name != "TruncateAlpha" || matches[1].Name != "TruncateBravo" {
			t.Fatalf("limited function completions = %#v, incomplete=%t", matches, incomplete)
		}
	}
	matches, incomplete := index.FunctionCompletionsMatching(func(name string) bool { return name == "TruncateCharlie" }, true, 1)
	if incomplete || len(matches) != 1 || matches[0].Name != "TruncateCharlie" {
		t.Fatalf("filtered completion before limit = %#v, incomplete=%t", matches, incomplete)
	}
}

func TestIndexRemoveFreesCapacity(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.vim")
	second := filepath.Join(root, "second.vim")
	file := syntax.Parse("var value = 1\n")
	index := NewIndex(1, len(file.Source))
	if err := index.Replace(first, file); err != nil {
		t.Fatal(err)
	}
	if index.FileCount() != 1 || index.IndexedBytes() != len(file.Source) {
		t.Fatalf("initial stats = %d files, %d bytes", index.FileCount(), index.IndexedBytes())
	}
	if err := index.Replace(second, file); !errors.Is(err, ErrIndexLimit) {
		t.Fatalf("second replace error = %v, want ErrIndexLimit", err)
	}
	index.Remove(first)
	if index.FileCount() != 0 || index.IndexedBytes() != 0 || len(index.Lookup("value")) != 0 {
		t.Fatalf("remove stats = %d files, %d bytes, lookup=%#v", index.FileCount(), index.IndexedBytes(), index.Lookup("value"))
	}
	if err := index.Replace(second, file); err != nil {
		t.Fatalf("replace after remove: %v", err)
	}
}

func TestIndexRevisionTracksSuccessfulChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "entry.vim")
	file := syntax.Parse("var value = 1\n")
	index := NewIndex(1, len(file.Source))
	if got := index.Revision(); got != 0 {
		t.Fatalf("initial revision = %d", got)
	}
	index.SetComplete(false)
	if got := index.Revision(); got != 0 {
		t.Fatalf("unchanged incomplete revision = %d", got)
	}
	index.SetComplete(true)
	if got := index.Revision(); got != 1 {
		t.Fatalf("complete revision = %d", got)
	}
	index.SetComplete(true)
	if got := index.Revision(); got != 1 {
		t.Fatalf("unchanged complete revision = %d", got)
	}
	index.Remove(path)
	if got := index.Revision(); got != 1 {
		t.Fatalf("no-op remove revision = %d", got)
	}
	if err := index.Replace(path, file); err != nil {
		t.Fatal(err)
	}
	if got := index.Revision(); got != 2 {
		t.Fatalf("replace revision = %d", got)
	}
	if err := index.Replace(filepath.Join(root, "other.vim"), file); !errors.Is(err, ErrIndexLimit) {
		t.Fatalf("rejected replace error = %v", err)
	}
	if got := index.Revision(); got != 2 {
		t.Fatalf("rejected replace revision = %d", got)
	}
	index.Remove(path)
	if got := index.Revision(); got != 3 {
		t.Fatalf("remove revision = %d", got)
	}
	index.SetComplete(false)
	if got := index.Revision(); got != 4 {
		t.Fatalf("incomplete revision = %d", got)
	}
	index.SetComplete(false)
	if got := index.Revision(); got != 4 {
		t.Fatalf("unchanged incomplete revision = %d", got)
	}
}

func TestIndexLimitsRejectReplaceAndKeepOldEntry(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "entry.vim")
	old := syntax.Parse("var old = 1\n")
	newFile := syntax.Parse("var replacement = 123456\n")
	index := NewIndex(1, len(old.Source))
	if err := index.Replace(path, old); err != nil {
		t.Fatal(err)
	}
	if err := index.Replace(path, newFile); !errors.Is(err, ErrIndexLimit) {
		t.Fatalf("oversized replacement error = %v, want ErrIndexLimit", err)
	}
	if len(index.Lookup("old")) != 1 || len(index.Lookup("replacement")) != 0 || index.IndexedBytes() != len(old.Source) {
		t.Fatalf("rejected replacement changed state: old=%#v replacement=%#v bytes=%d", index.Lookup("old"), index.Lookup("replacement"), index.IndexedBytes())
	}
	other := filepath.Join(root, "other.vim")
	if err := index.Replace(other, old); !errors.Is(err, ErrIndexLimit) {
		t.Fatalf("file-limit error = %v, want ErrIndexLimit", err)
	}
	if index.FileCount() != 1 || len(index.Lookup("old")) != 1 {
		t.Fatalf("rejected file changed state: files=%d lookup=%#v", index.FileCount(), index.Lookup("old"))
	}

	exact := NewIndex(2, len(old.Source))
	if err := exact.Replace(path, old); err != nil {
		t.Fatalf("exact byte limit rejected: %v", err)
	}
	if exact.IndexedBytes() != len(old.Source) {
		t.Fatalf("exact byte stats = %d", exact.IndexedBytes())
	}
}

func TestIndexRejectsInvalidInputAndIsolatedResults(t *testing.T) {
	index := NewIndex(10, 1000)
	file := syntax.Parse("var value = 1\n")
	if err := index.Replace("", file); !errors.Is(err, ErrIndexInvalidPath) {
		t.Fatalf("empty path error = %v", err)
	}
	if err := index.Replace("  \t", file); !errors.Is(err, ErrIndexInvalidPath) {
		t.Fatalf("blank path error = %v", err)
	}
	if err := index.Replace("file.vim", nil); !errors.Is(err, ErrIndexNilFile) {
		t.Fatalf("nil file error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "isolated.vim")
	if err := index.Replace(path, file); err != nil {
		t.Fatal(err)
	}
	result := index.Lookup("value")
	if len(result) != 1 {
		t.Fatalf("lookup = %#v", result)
	}
	result[0].Path = "mutated"
	result[0].Range.Start = 999
	file.Source = "var changed = 2\n"
	file.Commands = nil
	again := index.Lookup("value")
	if len(again) != 1 || again[0].Path == "mutated" || again[0].Range.Start == 999 || len(index.Lookup("caller-only")) != 0 {
		t.Fatalf("index leaked mutable state: %#v", again)
	}
	index.Remove("")
}

func TestIndexConcurrentOperations(t *testing.T) {
	index := NewIndex(20, 100000)
	root := t.TempDir()
	var group sync.WaitGroup
	for worker := range 8 {
		group.Go(func() {
			for iteration := range 100 {
				path := filepath.Join(root, "file", string(rune('a'+worker)), "doc.vim")
				file := syntax.Parse("var shared = 1\n")
				_ = index.Replace(path, file)
				_ = index.Lookup("shared")
				_ = index.Search("sha", 0)
				if iteration%3 == 0 {
					index.Remove(path)
				}
			}
		})
	}
	group.Wait()
	if index.FileCount() < 0 || index.IndexedBytes() < 0 {
		t.Fatalf("invalid final stats: files=%d bytes=%d", index.FileCount(), index.IndexedBytes())
	}
}
