package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestAnalyzeBindsTypeReferences(t *testing.T) {
	source := "vim9script\n" +
		"import './types.vim' as types\n" +
		"class Widget\nendclass\n" +
		"interface Face\nendinterface\n" +
		"class Child extends Widget implements Face\nendclass\n" +
		"type Wrapped = list<Widget>\n" +
		"var value: list<Widget>\n" +
		"var optional: tuple<?Widget, ...list<Widget>>\n" +
		"def Use(item: tuple<Widget, ?Face>): func(Widget): Widget\nenddef\n" +
		"var callback = (item: Widget): Face => item\n" +
		"var casted = <Widget>value\n" +
		"def Identity<T>(item: T): T\n  return item\nenddef\n" +
		"var result = Identity<Widget>(value)\n" +
		"var imported: list<types.Widget>\n" +
		"var number = 1\nvar hidden = 1\nvar primitive: number\nvar unknown: Missing\nvar hiddenType: hidden\n"
	file := syntax.Parse(source)
	result := Analyze(file)

	var got []string
	for _, reference := range result.References {
		if reference.Declaration == nil || reference.Declaration.Kind != SymbolKindClass && reference.Declaration.Kind != SymbolKindInterface && reference.Declaration.Kind != SymbolKindEnum && reference.Declaration.Kind != SymbolKindTypeAlias && reference.Declaration.Kind != SymbolKindImport {
			continue
		}
		if text := file.Text(reference.Span); text != reference.Name {
			t.Fatalf("reference span %v gives %q, want %q", reference.Span, text, reference.Name)
		}
		got = append(got, reference.Name)
	}
	want := []string{"Widget", "Face", "Widget", "Widget", "Widget", "Widget", "Widget", "Face", "Widget", "Widget", "Widget", "Face", "Widget", "Widget", "types"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("type references = %v, want %v", got, want)
	}

	for _, reference := range result.References {
		if reference.Name == "?" || reference.Name == "..." || reference.Name == "T" {
			t.Fatalf("non-name type syntax produced a reference: %#v", reference)
		}
		if file.Text(reference.Span) == "T" || file.Text(reference.Span) == "number" || file.Text(reference.Span) == "Missing" || file.Text(reference.Span) == "hidden" {
			if reference.Declaration != nil && (reference.Declaration.Kind == SymbolKindVariable || reference.Declaration.Kind == SymbolKindFunction) {
				t.Fatalf("non-type name bound through an annotation: %#v", reference)
			}
		}
	}
}
