package analysis

import (
	"strings"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
)

func TestVersionDiagnosticsCommand(t *testing.T) {
	file := syntax.Parse("defer Close()\n")
	analysisResult, _ := AnalyzeWithYield(file, false, nil)

	// defer was added in 9.0.0370
	oldVersion, _ := vimdata.ParseVimVersion("9.0.0300")
	diags := VersionDiagnostics(file, analysisResult, oldVersion)
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %#v", len(diags), diags)
	}
	if diags[0].Code != "vim/E492" || diags[0].Message != "Not an editor command: defer (added in Vim 9.0.0370)" {
		t.Fatalf("unexpected diagnostic: %#v", diags[0])
	}

	newVersion, _ := vimdata.ParseVimVersion("9.0.0500")
	diags = VersionDiagnostics(file, analysisResult, newVersion)
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d: %#v", len(diags), diags)
	}
}

func TestVersionDiagnosticsOption(t *testing.T) {
	// smoothscroll was added in 9.0.0640
	source := "set smoothscroll\nset sms\necho &smoothscroll\n"
	file := syntax.Parse(source)
	analysisResult, _ := AnalyzeWithYield(file, false, nil)

	targetVersion, _ := vimdata.ParseVimVersion("9.0.0500")
	diags := VersionDiagnostics(file, analysisResult, targetVersion)
	if len(diags) != 3 {
		t.Fatalf("expected 3 diagnostics, got %d: %#v", len(diags), diags)
	}
	if diags[0].Code != "vim/E518" || diags[0].Message != "Unknown option: smoothscroll (added in Vim 9.0.0640)" {
		t.Errorf("diag 0: %#v", diags[0])
	}
	if diags[1].Code != "vim/E518" || diags[1].Message != "Unknown option: sms (added in Vim 9.0.0640)" {
		t.Errorf("diag 1: %#v", diags[1])
	}
	if diags[2].Code != "vim/E113" || diags[2].Message != "Unknown option: smoothscroll (added in Vim 9.0.0640)" {
		t.Errorf("diag 2: %#v", diags[2])
	}

	okVersion, _ := vimdata.ParseVimVersion("9.1.0000")
	diags = VersionDiagnostics(file, analysisResult, okVersion)
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d: %#v", len(diags), diags)
	}
}

func TestVersionDiagnosticsFunction(t *testing.T) {
	// indexof was added in 9.0.0196
	source := "vim9script\necho indexof([1, 2], 'v:val == 2')\necho [1, 2]->indexof('v:val == 2')\n"
	file := syntax.Parse(source)
	analysisResult, _ := AnalyzeWithYield(file, false, nil)

	targetVersion, _ := vimdata.ParseVimVersion("9.0.0000")
	diags := VersionDiagnostics(file, analysisResult, targetVersion)
	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d: %#v", len(diags), diags)
	}
	if diags[0].Code != "vim/E117" || diags[0].Message != "Unknown function: indexof (added in Vim 9.0.0196)" {
		t.Errorf("diag 0: %#v", diags[0])
	}
	if diags[1].Code != "vim/E117" || diags[1].Message != "Unknown function: indexof (added in Vim 9.0.0196)" {
		t.Errorf("diag 1: %#v", diags[1])
	}

	okVersion, _ := vimdata.ParseVimVersion("9.0.0200")
	diags = VersionDiagnostics(file, analysisResult, okVersion)
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d: %#v", len(diags), diags)
	}
}

func TestVersionDiagnosticsAutocmdEvent(t *testing.T) {
	// WinResized was added in 9.0.0917
	source := "autocmd WinResized * echo 1\n"
	file := syntax.Parse(source)
	analysisResult, _ := AnalyzeWithYield(file, false, nil)

	targetVersion, _ := vimdata.ParseVimVersion("9.0.0500")
	diags := VersionDiagnostics(file, analysisResult, targetVersion)
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %#v", len(diags), diags)
	}
	if diags[0].Code != "vim/E216" || diags[0].Message != "No such group or event: WinResized (added in Vim 9.0.0917)" {
		t.Errorf("unexpected diagnostic: %#v", diags[0])
	}

	okVersion, _ := vimdata.ParseVimVersion("9.1.0000")
	diags = VersionDiagnostics(file, analysisResult, okVersion)
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d: %#v", len(diags), diags)
	}
}

func TestVersionDiagnosticsUserDeclaredVariableNotConfusedWithFunction(t *testing.T) {
	source := "let indexof = 1\necho indexof\n"
	file := syntax.Parse(source)
	analysisResult, _ := AnalyzeWithYield(file, false, nil)

	targetVersion, _ := vimdata.ParseVimVersion("9.0.0000")
	diags := VersionDiagnostics(file, analysisResult, targetVersion)
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics for user variable, got: %#v", diags)
	}
}

func TestVersionDiagnosticsExternalVariableNotReportedAsFunction(t *testing.T) {
	source := "echo g:indexof\n"
	file := syntax.Parse(source)
	analysisResult, _ := AnalyzeWithYield(file, false, nil)

	targetVersion, _ := vimdata.ParseVimVersion("9.0.0000")
	diags := VersionDiagnostics(file, analysisResult, targetVersion)
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics for external variable g:indexof, got: %#v", diags)
	}
}

func TestVersionDiagnosticsLegacyOptionReadAfterAssignment(t *testing.T) {
	source := "let &smoothscroll = 1\necho &smoothscroll\n"
	file := syntax.Parse(source)
	analysisResult, _ := AnalyzeWithYield(file, false, nil)

	targetVersion, _ := vimdata.ParseVimVersion("9.0.0000")
	diags := VersionDiagnostics(file, analysisResult, targetVersion)
	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics for &smoothscroll (assignment and read), got %d: %#v", len(diags), diags)
	}
	if diags[0].Code != "vim/E113" || diags[0].Message != "Unknown option: smoothscroll (added in Vim 9.0.0640)" {
		t.Errorf("diag 0: %#v", diags[0])
	}
	if diags[1].Code != "vim/E113" || diags[1].Message != "Unknown option: smoothscroll (added in Vim 9.0.0640)" {
		t.Errorf("diag 1: %#v", diags[1])
	}
}

func TestVersionDiagnosticsLambdaBodyCommands(t *testing.T) {
	source := "vim9script\nvar F = () => {\n  set smoothscroll\n}\n"
	file := syntax.Parse(source)
	analysisResult, _ := AnalyzeWithYield(file, false, nil)

	targetVersion, _ := vimdata.ParseVimVersion("9.0.0000")
	diags := VersionDiagnostics(file, analysisResult, targetVersion)
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic in lambda body, got %d: %#v", len(diags), diags)
	}
	if diags[0].Code != "vim/E518" || diags[0].Message != "Unknown option: smoothscroll (added in Vim 9.0.0640)" {
		t.Errorf("unexpected diagnostic: %#v", diags[0])
	}
}

func TestVersionDiagnosticsCommandModifier(t *testing.T) {
	source := "horizontal wincmd =\n"
	file := syntax.Parse(source)
	analysisResult, _ := AnalyzeWithYield(file, false, nil)

	targetVersion, _ := vimdata.ParseVimVersion("9.0.0000")
	diags := VersionDiagnostics(file, analysisResult, targetVersion)
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic for horizontal modifier, got %d: %#v", len(diags), diags)
	}
	if diags[0].Code != "vim/E492" || diags[0].Message != "Not an editor command: horizontal (added in Vim 9.0.0342)" {
		t.Errorf("unexpected diagnostic: %#v", diags[0])
	}
}

func TestVersionDiagnosticsDeeplyNestedLambdas(t *testing.T) {
	// Each declaration exposes the same initializer through both Expressions
	// and Declaration.Initializer. Nest declarations to reproduce the former
	// exponential traversal, rather than nesting only lambda expressions.
	const depth = 24
	source := "vim9script\n" + strings.Repeat("var F = () => {\n", depth) +
		"set smoothscroll\n" + strings.Repeat("}\n", depth)

	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("unexpected parse diagnostics: %#v", file.Diagnostics)
	}

	targetVersion, _ := vimdata.ParseVimVersion("9.0.0000")
	// Command checks need only syntax; isolate the traversal regression from
	// the cost of full semantic analysis of deeply nested functions.
	start := time.Now()
	diags := VersionDiagnostics(file, nil, targetVersion)
	elapsed := time.Since(start)

	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %#v", len(diags), diags)
	}
	if diags[0].Code != "vim/E518" {
		t.Fatalf("unexpected diagnostic: %#v", diags[0])
	}
	startOffset := strings.Index(source, "smoothscroll")
	wantSpan := (syntax.Span{Start: startOffset, End: startOffset + len("smoothscroll")})
	if diags[0].Span != wantSpan {
		t.Fatalf("diagnostic span = %#v, want %#v", diags[0].Span, wantSpan)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("version diagnostics took too long (%v) on 24-level nested lambdas", elapsed)
	}
}
