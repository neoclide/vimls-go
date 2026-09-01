package analysis

import (
	"reflect"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestDiagnosticsForVersionRewritesE1406BeforeVim920507(t *testing.T) {
	file := syntax.Parse("vim9script\nclass C\n  var value = 1\n  var _value = 2\nendclass\n")
	diagnostics := Analyze(file).Diagnostics
	if len(diagnostics) != 1 || diagnostics[0].Code != "vim/E1406" {
		t.Fatalf("analysis diagnostics = %#v", diagnostics)
	}
	input := append([]syntax.Diagnostic(nil), diagnostics...)

	old := DiagnosticsForVersion(file, diagnostics, syntax.Version{Major: 9, Minor: 2, Patch: 506})
	if len(old) != 1 || old[0].Code != "vim/E1369" || old[0].Message != "Duplicate variable: _value" {
		t.Fatalf("9.2.0506 diagnostics = %#v", old)
	}
	if !reflect.DeepEqual(diagnostics, input) {
		t.Fatalf("input diagnostics mutated: got %#v, want %#v", diagnostics, input)
	}

	current := DiagnosticsForVersion(file, diagnostics, syntax.Version{Major: 9, Minor: 2, Patch: 507})
	if len(current) != 1 || current[0].Code != "vim/E1406" {
		t.Fatalf("9.2.0507 diagnostics = %#v", current)
	}
}
