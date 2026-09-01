package analysis

import (
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestAnalyzeImportsReportsStaticLoadAndMemberErrors(t *testing.T) {
	privateRelated := syntax.RelatedDiagnostic{URI: "file:///lib.vim", Source: "var Private\n", Message: "Private is declared here", Span: syntax.Span{Start: 4, End: 11}}
	oldRelated := syntax.RelatedDiagnostic{URI: "file:///lib.vim", Source: "var Old\n", Message: "Old is declared here", Span: syntax.Span{Start: 4, End: 7}}
	diagnostics := AnalyzeImports(
		[]ImportLoad{
			{Span: syntax.Span{Start: 1, End: 16}, Path: "missing.vim", Missing: true},
			{Span: syntax.Span{Start: 20, End: 35}, Path: "autoload.vim", Missing: true, Autoload: true, Runtime: true},
			{Span: syntax.Span{Start: 36, End: 39}, Path: "./relative.vim", Missing: true, Autoload: true},
			{Span: syntax.Span{Start: 60, End: 72}, Path: "self.vim", Self: true, Duplicate: true},
			{Span: syntax.Span{Start: 80, End: 91}, Path: "same.vim", Duplicate: true},
		},
		[]ImportMember{
			{Span: syntax.Span{Start: 40, End: 47}, Name: "Missing", TargetKnown: true},
			{Span: syntax.Span{Start: 50, End: 57}, Name: "Private", TargetKnown: true, Exists: true, Related: privateRelated},
			{Span: syntax.Span{Start: 58, End: 61}, Name: "Old", TargetKnown: true, Exists: true, Exported: true, Deprecated: true, Related: oldRelated},
		},
	)
	wantCodes := []string{"vim/E1053", "vim/E1053", "vim/E1264", "vim/E1048", "vim/E1049", "vimls/deprecated", "vim/E1088", "vim/E1262"}
	wantMessages := []string{
		`Could not import "missing.vim"`, `Could not import "autoload.vim"`,
		"Autoload import cannot use absolute or relative path: ./relative.vim",
		"Item not found in script: Missing", "Item not exported in script: Private",
		"Old is deprecated",
		"Script cannot import itself", "Cannot import the same script twice: same.vim",
	}
	if len(diagnostics) != len(wantCodes) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for index := range wantCodes {
		if diagnostics[index].Code != wantCodes[index] || diagnostics[index].Message != wantMessages[index] {
			t.Fatalf("diagnostic[%d] = %#v, want %s %q", index, diagnostics[index], wantCodes[index], wantMessages[index])
		}
	}
	if diagnostics[4].Related != privateRelated || diagnostics[5].Related != oldRelated {
		t.Fatalf("related diagnostics = %#v, %#v", diagnostics[4].Related, diagnostics[5].Related)
	}
}

func TestAnalyzeImportsKeepsUnknownAndValidImportsQuiet(t *testing.T) {
	diagnostics := AnalyzeImports(
		[]ImportLoad{
			{Path: "dynamic.vim"},
			{Missing: true},
			{Duplicate: true},
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
