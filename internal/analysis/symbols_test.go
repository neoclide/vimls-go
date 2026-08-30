package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestCollectSymbolsBuildsDeclarationHierarchy(t *testing.T) {
	source := `vim9script
import './mod.vim'
import autoload './auto.vim' as auto
class Widget
  const ID: number = 1
  def new(value: number)
    if value > 0
      var local = value
    endif
    return this
  enddef
  def run()
  enddef
endclass
interface Runnable
  def execute()
endinterface
enum Color
  Red,
  Green
endenum
type Pair = tuple<number, string>
var [left, right] = values
final name = 'x'
`
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
	}
	symbols := CollectSymbols(file)
	if got, want := names(symbols), []string{"'./mod.vim'", "auto", "Widget", "Runnable", "Color", "Pair", "left", "right", "name"}; !equalStrings(got, want) {
		t.Fatalf("root symbols = %#v, want %#v", got, want)
	}

	mod, auto := symbols[0], symbols[1]
	if mod.Kind != SymbolKindImport || mod.SelectionRange != file.Commands[1].Import.PathSpan || mod.Name != "'./mod.vim'" {
		t.Fatalf("plain import = %#v", mod)
	}
	if auto.Kind != SymbolKindImport || auto.SelectionRange != file.Commands[2].Import.Alias || auto.Name != "auto" || auto.Detail != "autoload import" {
		t.Fatalf("autoload import = %#v", auto)
	}

	widget := symbols[2]
	if widget.Kind != SymbolKindClass || widget.SelectionRange != file.Commands[3].Aggregate.Name || file.Text(widget.Range) != "class Widget\n  const ID: number = 1\n  def new(value: number)\n    if value > 0\n      var local = value\n    endif\n    return this\n  enddef\n  def run()\n  enddef\nendclass" {
		t.Fatalf("class symbol = %#v, range text = %q", widget, file.Text(widget.Range))
	}
	if got, want := names(widget.Children), []string{"ID", "new", "run"}; !equalStrings(got, want) {
		t.Fatalf("class children = %#v, want %#v", got, want)
	}
	if widget.Children[0].Kind != SymbolKindConstant || widget.Children[0].SelectionRange != file.Commands[4].Declaration.Bindings[0].Name {
		t.Fatalf("class constant = %#v", widget.Children[0])
	}
	constructor := widget.Children[1]
	if constructor.Kind != SymbolKindConstructor || constructor.SelectionRange != file.Commands[5].Function.Name || !strings.HasPrefix(file.Text(constructor.Range), "def new(") {
		t.Fatalf("constructor = %#v, range text = %q", constructor, file.Text(constructor.Range))
	}
	if got, want := names(constructor.Children), []string{"local"}; !equalStrings(got, want) {
		t.Fatalf("constructor children = %#v, want %#v", got, want)
	}
	if constructor.Children[0].Kind != SymbolKindVariable || constructor.Children[0].SelectionRange != file.Commands[7].Declaration.Bindings[0].Name {
		t.Fatalf("nested variable = %#v", constructor.Children[0])
	}
	if widget.Children[2].Kind != SymbolKindMethod || widget.Children[2].Name != "run" {
		t.Fatalf("regular method = %#v", widget.Children[2])
	}

	runnable := symbols[3]
	if runnable.Kind != SymbolKindInterface || len(runnable.Children) != 1 || runnable.Children[0].Kind != SymbolKindMethod || runnable.Children[0].Name != "execute" {
		t.Fatalf("interface hierarchy = %#v", runnable)
	}
	if file.Text(runnable.Range) != "interface Runnable\n  def execute()\nendinterface" {
		t.Fatalf("interface range = %q", file.Text(runnable.Range))
	}

	color := symbols[4]
	if color.Kind != SymbolKindEnum || len(color.Children) != 2 || names(color.Children)[0] != "Red" || names(color.Children)[1] != "Green" {
		t.Fatalf("enum hierarchy = %#v", color)
	}
	for _, child := range color.Children {
		if child.Kind != SymbolKindEnumMember || child.Range != child.SelectionRange || file.Text(child.Range) == "" {
			t.Fatalf("enum member = %#v", child)
		}
	}

	if symbols[5].Kind != SymbolKindTypeAlias || symbols[5].Name != "Pair" {
		t.Fatalf("type alias = %#v", symbols[5])
	}
	if symbols[6].Kind != SymbolKindVariable || symbols[7].Kind != SymbolKindVariable || symbols[8].Kind != SymbolKindConstant {
		t.Fatalf("declaration kinds = %#v", symbols[5:])
	}
	if symbols[6].Name != "left" || symbols[7].Name != "right" || symbols[8].Name != "name" {
		t.Fatalf("declaration names = %#v", symbols[5:])
	}
}

func TestCollectSymbolsSkipsControlBlocksAndKeepsSourceOrder(t *testing.T) {
	source := `vim9script
class C
  def run()
    if true
      var inside = 1
    endif
  enddef
endclass
var outside = 2
`
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
	}
	symbols := CollectSymbols(file)
	if len(symbols) != 2 || symbols[0].Name != "C" || symbols[1].Name != "outside" {
		t.Fatalf("symbols = %#v", symbols)
	}
	if len(symbols[0].Children) != 1 || symbols[0].Children[0].Name != "run" {
		t.Fatalf("control block child = %#v", symbols[0].Children)
	}
	if len(symbols[0].Children[0].Children) != 1 || symbols[0].Children[0].Children[0].Name != "inside" {
		t.Fatalf("nested declaration = %#v", symbols[0].Children[0].Children)
	}
}

func TestCollectSymbolsUsesPathForImportWithoutAlias(t *testing.T) {
	file := syntax.Parse("vim9script\nimport './plain.vim'\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
	}
	symbols := CollectSymbols(file)
	if len(symbols) != 1 || symbols[0].Name != "'./plain.vim'" || symbols[0].Name == "" || symbols[0].SelectionRange != file.Commands[1].Import.PathSpan {
		t.Fatalf("symbols = %#v", symbols)
	}
}

func TestCollectSymbolsWalksEmbeddedListsAndPreservesContainers(t *testing.T) {
	source := `vim9script
class Holder
  def run()
    windo if outer | var nested = outer | endif
  enddef
endclass
autocmd BufEnter * windo if condition | var body = condition | endif
`
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
	}
	symbols := CollectSymbols(file)
	if got, want := names(symbols), []string{"Holder", "body"}; !equalStrings(got, want) {
		t.Fatalf("root symbols = %#v, want %#v", got, want)
	}
	holder := symbols[0]
	if got, want := names(holder.Children), []string{"run"}; !equalStrings(got, want) {
		t.Fatalf("class children = %#v, want %#v", got, want)
	}
	run := holder.Children[0]
	if got, want := names(run.Children), []string{"nested"}; !equalStrings(got, want) {
		t.Fatalf("function embedded children = %#v, want %#v", got, want)
	}
	if symbols[1].Name != "body" || symbols[1].Kind != SymbolKindVariable {
		t.Fatalf("autocmd symbol = %#v", symbols[1])
	}
	for _, symbol := range []*Symbol{holder, run, run.Children[0], symbols[1]} {
		if symbol.SelectionRange.Start < 0 || symbol.SelectionRange.End > len(file.Source) || file.Text(symbol.SelectionRange) == "" {
			t.Fatalf("invalid embedded symbol span = %#v", symbol)
		}
	}
}

func TestCollectSymbolsWalksMultiLevelEmbeddedDeclarations(t *testing.T) {
	source := `vim9script
windo if condition | var nested = condition | endif
`
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
	}
	symbols := CollectSymbols(file)
	if len(symbols) != 1 || symbols[0].Name != "nested" || symbols[0].Kind != SymbolKindVariable {
		t.Fatalf("embedded symbols = %#v", symbols)
	}
	if file.Text(symbols[0].SelectionRange) != "nested" {
		t.Fatalf("embedded selections = %#v", symbols)
	}
}

func TestCollectSymbolsHandlesMalformedEmbeddedSyntax(t *testing.T) {
	embedded := &syntax.CommandList{
		Span: syntax.Span{Start: 6, End: 7},
		Commands: []syntax.Command{{Block: 99, Span: syntax.Span{Start: -1, End: 100}, Declaration: &syntax.Declaration{
			Bindings: []syntax.Binding{{Name: syntax.Span{Start: -1, End: 100}}},
		}}},
		Blocks: []syntax.Block{{Parent: 99}},
	}
	file := &syntax.File{
		Source:   "windo x",
		Commands: []syntax.Command{{Embedded: embedded}},
	}
	if symbols := CollectSymbols(file); len(symbols) != 0 {
		t.Fatalf("malformed embedded symbols = %#v", symbols)
	}
}

func names(symbols []*Symbol) []string {
	result := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		result = append(result, symbol.Name)
	}
	return result
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
