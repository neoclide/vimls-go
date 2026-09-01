package analysis

import (
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// TestDiagnosticCoverageMatrix keeps independent, compact reproductions for
// semantic paths whose recovery must not prevent later analysis. The expected
// diagnostics are established by the adjacent diagnostic tests and the pinned
// Vim compile cases.
func TestDiagnosticCoverageMatrix(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		code   string
		span   string
	}{
		{
			name:   "typed return needs a value",
			source: "vim9script\ndef Number(): number\n  return\nenddef\n",
			code:   "vim/E1003",
			span:   "return",
		},
		{
			name:   "incompatible declaration initializer",
			source: "vim9script\ndef Assign()\n  var value: number = 'text'\nenddef\n",
			code:   "vim/E1012",
			span:   "'text'",
		},
		{
			name:   "invalid numeric operator",
			source: "vim9script\ndef Operator()\n  var value = 'one' % 'two'\nenddef\n",
			code:   "vim/E1035",
			span:   "%",
		},
		{
			name:   "tuple item is immutable",
			source: "vim9script\ndef Index()\n  var values = (1, 2)\n  values[0] = 3\nenddef\n",
			code:   "vim/E1532",
			span:   "values[0]",
		},
		{
			name:   "builtin callback return type",
			source: "vim9script\ndef Callback()\n  var values: list<number> = [1, 2]\n  map(values, (_, value) => [])\nenddef\n",
			code:   "vim/E1013",
			span:   "(_, value) => []",
		},
		{
			name:   "class cannot extend an interface",
			source: "vim9script\ninterface Parent\nendinterface\nclass Child extends Parent\nendclass\n",
			code:   "vim/E1354",
			span:   "endclass",
		},
		{
			name:   "super requires child class",
			source: "vim9script\nclass Parent\n  def Run()\n  enddef\nendclass\nclass Child\n  def Run()\n    super.Run()\n  enddef\nendclass\n",
			code:   "vim/E1358",
			span:   "super",
		},
		{
			name:   "legacy function may overwrite",
			source: "function! Existing()\nendfunction\nfunction Existing()\nendfunction\n",
			code:   "vim/E122",
			span:   "Existing",
		},
		{
			name:   "user command may overwrite",
			source: "command Existing echo 'one'\n",
			code:   "vim/E174",
			span:   "Existing",
		},
		{
			name:   "return outside function",
			source: "vim9script\nreturn 1\n",
			code:   "vim/E133",
			span:   "return",
		},
		{
			name:   "class extends unknown name",
			source: "vim9script\nclass Child extends Missing\nendclass\n",
			code:   "vim/E1353",
			span:   "endclass",
		},
		{
			name:   "class implements unknown interface",
			source: "vim9script\nclass Child implements Missing\nendclass\n",
			code:   "vim/E1346",
			span:   "endclass",
		},
		{
			name:   "script function cannot be deleted",
			source: "vim9script\ndef Existing()\nenddef\ndelfunction Existing\n",
			code:   "vim/E1084",
			span:   "Existing",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			result := Analyze(file)
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == test.code && file.Text(diagnostic.Span) == test.span {
					return
				}
			}
			t.Fatalf("diagnostics = %#v, want %s on %q", result.Diagnostics, test.code, test.span)
		})
	}
}

func TestDiagnosticRecoveryRetainsFollowingDeclaration(t *testing.T) {
	source := "vim9script\ndef Broken(): number\n  return\nenddef\ndef Later(value: string): string\n  return value\nenddef\n"
	file := syntax.Parse(source)
	result := Analyze(file)
	foundDiagnostic := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1003" && file.Text(diagnostic.Span) == "return" {
			foundDiagnostic = true
		}
	}
	if !foundDiagnostic {
		t.Fatalf("diagnostics = %#v, want E1003", result.Diagnostics)
	}
	for _, symbol := range CollectSymbols(file) {
		if symbol.Name == "Later" && symbol.Kind == SymbolKindFunction && file.Text(symbol.SelectionRange) == "Later" {
			return
		}
	}
	t.Fatalf("symbols = %#v, later declaration was not retained", CollectSymbols(file))
}
