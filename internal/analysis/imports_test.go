package analysis

import (
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestAnalyzeImportsReportsStaticLoadAndMemberErrors(t *testing.T) {
	diagnostics := AnalyzeImports(
		[]ImportLoad{
			{Span: syntax.Span{Start: 1, End: 16}, Path: "missing.vim", Missing: true},
			{Span: syntax.Span{Start: 20, End: 35}, Path: "autoload.vim", Missing: true, Autoload: true, Runtime: true},
		},
		[]ImportMember{
			{Span: syntax.Span{Start: 40, End: 47}, Name: "Missing", TargetKnown: true},
			{Span: syntax.Span{Start: 50, End: 57}, Name: "Private", TargetKnown: true, Exists: true},
		},
	)
	wantCodes := []string{"vim/E1053", "vim/E1053", "vim/E1048", "vim/E1049"}
	wantMessages := []string{
		`Could not import "missing.vim"`, `Could not import "autoload.vim"`,
		"Item not found in script: Missing", "Item not exported in script: Private",
	}
	if len(diagnostics) != len(wantCodes) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for index := range wantCodes {
		if diagnostics[index].Code != wantCodes[index] || diagnostics[index].Message != wantMessages[index] {
			t.Fatalf("diagnostic[%d] = %#v, want %s %q", index, diagnostics[index], wantCodes[index], wantMessages[index])
		}
	}
}

func TestAnalyzeImportsKeepsUnknownAndValidImportsQuiet(t *testing.T) {
	diagnostics := AnalyzeImports(
		[]ImportLoad{
			{Path: "dynamic.vim"},
			{Path: "relative.vim", Missing: true, Autoload: true},
			{Missing: true},
		},
		[]ImportMember{
			{Name: "Unknown"},
			{Name: "Exported", TargetKnown: true, Exists: true, Exported: true},
		},
	)
	if len(diagnostics) != 0 {
		t.Fatalf("conservative import diagnostics = %#v", diagnostics)
	}
}
