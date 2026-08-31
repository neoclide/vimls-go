package syntax_test

import (
	"reflect"
	"testing"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
)

func checkIncrementalAnalysis(t *testing.T, parse func(*syntax.File, string) *syntax.File, oldSource, newSource string) {
	t.Helper()
	got := analysis.Analyze(parse(syntax.Parse(oldSource), newSource))
	want := analysis.Analyze(syntax.Parse(newSource))
	if gotSnapshot, wantSnapshot := snapshotAnalysis(got), snapshotAnalysis(want); !reflect.DeepEqual(gotSnapshot, wantSnapshot) {
		t.Fatalf("incremental analysis differs from full parse\ngot:  %#v\nwant: %#v", gotSnapshot, wantSnapshot)
	}
}

type analysisSnapshot struct {
	scopes       []scopeSnapshot
	declarations []declarationSnapshot
	references   []referenceSnapshot
	diagnostics  []syntax.Diagnostic
}

type scopeSnapshot struct {
	block, parent int
	kind          syntax.BlockKind
	span          syntax.Span
}

type declarationSnapshot struct {
	name       string
	kind       analysis.SymbolKind
	span       syntax.Span
	mutable    bool
	deprecated bool
	parameter  bool
	typ        analysis.ValueType
	scope      int
}

type referenceSnapshot struct {
	name        string
	span        syntax.Span
	declaration int
}

func snapshotAnalysis(result *analysis.FileAnalysis) analysisSnapshot {
	snapshot := analysisSnapshot{diagnostics: append([]syntax.Diagnostic(nil), result.Diagnostics...)}
	scopeIndexes := make(map[*analysis.Scope]int, len(result.Scopes))
	for index, scope := range result.Scopes {
		scopeIndexes[scope] = index
	}
	declarationIndexes := make(map[*analysis.Declaration]int, len(result.Declarations))
	for index, declaration := range result.Declarations {
		declarationIndexes[declaration] = index
	}
	for _, scope := range result.Scopes {
		parent := -1
		if scope.Parent != nil {
			parent = scopeIndexes[scope.Parent]
		}
		snapshot.scopes = append(snapshot.scopes, scopeSnapshot{block: scope.Block, parent: parent, kind: scope.Kind, span: scope.Span})
	}
	for _, declaration := range result.Declarations {
		snapshot.declarations = append(snapshot.declarations, declarationSnapshot{
			name: declaration.Name, kind: declaration.Kind, span: declaration.Span, mutable: declaration.Mutable,
			deprecated: declaration.Deprecated, parameter: declaration.Parameter, typ: declaration.Type, scope: scopeIndexes[declaration.Scope],
		})
	}
	for _, reference := range result.References {
		declaration := -1
		if reference.Declaration != nil {
			declaration = declarationIndexes[reference.Declaration]
		}
		snapshot.references = append(snapshot.references, referenceSnapshot{name: reference.Name, span: reference.Span, declaration: declaration})
	}
	return snapshot
}

func TestIncrementalAnalysisHelper(t *testing.T) {
	checkIncrementalAnalysis(t, func(_ *syntax.File, source string) *syntax.File { return syntax.Parse(source) },
		"vim9script\nvar value = 1\necho value\n",
		"vim9script\nvar value = 2\necho value\n")
}

func TestReparseAnalysisMatchesFullParse(t *testing.T) {
	checkIncrementalAnalysis(t, syntax.Reparse,
		"vim9script\nvar one = 1\nvar two = 2\necho one + two\n",
		"vim9script\nvar one = 1\nvar two = 20\necho one + two\n")
}
