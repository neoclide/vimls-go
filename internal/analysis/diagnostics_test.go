package analysis

import (
	"strings"
	"testing"

	"github.com/chemzqm/vimls-go/internal/syntax"
)

func TestAnalyzeBuiltinArgumentTypeDiagnostics(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   int
	}{
		{"number", "vim9script\ndef F()\n  abs('x')\nenddef\n", 1},
		{"string", "vim9script\ndef F()\n  split(1)\nenddef\n", 1},
		{"buffer union mismatch", "vim9script\ndef F()\n  bufname({})\nenddef\n", 1},
		{"buffer union matches", "vim9script\ndef F()\n  bufname(1)\n  bufname('x')\nenddef\n", 0},
		{"len union", "vim9script\ndef F()\n  len(1)\n  len({})\n  len(1.0)\nenddef\n", 1},
		{"method argument", "vim9script\ndef F()\n  ['x']->map(3)\nenddef\n", 1},
		{"null argument", "vim9script\ndef F()\n  assert_match('a', 'b', null)\nenddef\n", 1},
		{"builtin function result", "vim9script\ndef F()\n  foldclosed(function('min'))\nenddef\n", 1},
		{"builtin channel result", "vim9script\ndef F()\n  map(test_null_channel(), '1')\nenddef\n", 1},
		{"builtin void result", "vim9script\ndef F()\n  test_feedinput(test_void())\nenddef\n", 1},
		{"filter callback parameter", "vim9script\ndef F()\n  var values = [1, 2]\n  filter(values, (i: string, v: number) => true)\nenddef\n", 1},
		{"map callback return", "vim9script\ndef F()\n  var values: list<number> = [1, 2]\n  map(values, (_, v) => [])\nenddef\n", 1},
		{"sort callback return", "vim9script\ndef F()\n  sort([1, 2], (a: number, b: number) => true)\nenddef\n", 1},
		{"callback void return", "vim9script\ndef F()\n  def TestIdx(k: number, v: dict<any>)\n  enddef\n  indexof([{color: 'red'}], TestIdx)\nenddef\n", 1},
		{"unknown argument", "vim9script\ndef F(value: any)\n  abs(value)\nenddef\n", 0},
		{"incomplete call", "vim9script\ndef F()\n  len(\nenddef\n", 0},
		{"legacy", "echo abs('x')\n", 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(syntax.Parse(test.source))
			var got int
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1013" {
					got++
					if diagnostic.Span.Start >= diagnostic.Span.End || diagnostic.Message == "" {
						t.Fatalf("invalid E1013 = %#v", diagnostic)
					}
				}
			}
			if got != test.want {
				t.Fatalf("E1013 diagnostics = %d, want %d; all diagnostics = %#v", got, test.want, result.Diagnostics)
			}
		})
	}
}

func TestAnalyzeFunctionArgumentTypeDiagnostics(t *testing.T) {
	source := "vim9script\ndef F()\n  var Ref = (x: number, y: number) => x + y\n  Ref(1, 'x')\n  Ref(1, 2)\nenddef\n"
	var diagnostics []syntax.Diagnostic
	for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
		if diagnostic.Code == "vim/E1013" {
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	if len(diagnostics) != 1 || source[diagnostics[0].Span.Start:diagnostics[0].Span.End] != "'x'" {
		t.Fatalf("E1013 diagnostics = %#v", diagnostics)
	}
}

func TestAnalyzeBuiltinArityDiagnostics(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []syntax.Diagnostic
	}{
		{
			name:   "legacy direct builtins",
			source: "echo len()\necho abs(1, 2)\necho range(1, 2, 3, 4)\n",
			want: []syntax.Diagnostic{
				{Code: "vim/E119", Message: "Not enough arguments for function: len"},
				{Code: "vim/E118", Message: "Too many arguments for function: abs"},
				{Code: "vim/E118", Message: "Too many arguments for function: range"},
			},
		},
		{
			name:   "vim9 direct builtins",
			source: "vim9script\nvar first = len()\nvar second = abs(1, 2)\n",
			want: []syntax.Diagnostic{
				{Code: "vim/E119", Message: "Not enough arguments for function: len"},
				{Code: "vim/E118", Message: "Too many arguments for function: abs"},
			},
		},
		{
			name:   "optional variadic and conservative targets",
			source: "vim9script\nvar optional = range(1)\nvar variadic = instanceof(null_object, 2, 3, 4)\nvar dynamic = call('len', [])\nMyFunction()\ns:len()\nitems->len()\n",
		},
		{
			name:   "incomplete calls do not cascade",
			source: "vim9script\nvar missing = len(\nvar missingComma = len(1 2)\nvar missingArgument = abs(1,)\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			got := Analyze(file).Diagnostics
			if len(got) != len(test.want) {
				t.Fatalf("diagnostics = %#v, want %#v (syntax diagnostics = %#v)", got, test.want, file.Diagnostics)
			}
			last := -1
			for index, diagnostic := range got {
				want := test.want[index]
				if diagnostic.Code != want.Code || diagnostic.Message != want.Message {
					t.Fatalf("diagnostic[%d] = %#v, want %#v", index, diagnostic, want)
				}
				name := diagnostic.Message[strings.LastIndex(diagnostic.Message, ": ")+2:]
				start := strings.Index(test.source[last+1:], name) + last + 1
				wantSpan := syntax.Span{Start: start, End: start + len(name)}
				if start <= last || diagnostic.Span != wantSpan || file.Text(diagnostic.Span) != name {
					t.Fatalf("diagnostic[%d] span = %#v (%q), want %#v (%q)", index, diagnostic.Span, file.Text(diagnostic.Span), wantSpan, name)
				}
				last = start
			}
		})
	}
}

func TestAnalyzeImmutableAssignmentDiagnostics(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []syntax.Diagnostic
	}{
		{
			name:   "script const and final",
			source: "vim9script\nconst first = 1\nfinal second = 2\nfirst = 3\nsecond = 4\n",
			want: []syntax.Diagnostic{
				{Code: "vim/E46", Message: `Cannot change read-only variable "first"`},
				{Code: "vim/E46", Message: `Cannot change read-only variable "second"`},
			},
		},
		{
			name:   "def const and final",
			source: "vim9script\ndef Test()\n  const first = 1\n  final second = 2\n  first = 3\n  second = 4\nenddef\n",
			want: []syntax.Diagnostic{
				{Code: "vim/E1018", Message: "Cannot assign to a constant: first"},
				{Code: "vim/E1018", Message: "Cannot assign to a constant: second"},
			},
		},
		{
			name:   "conservative exclusions",
			source: "vim9script\nconst fixed = [1]\nvar mutable = 1\nif true\n  var fixed = 2\n  fixed = 3\nendif\nmutable = 2\nlegacy fixed = 4\ns:fixed = 5\nfixed.member = 6\nfixed[0] = 6\n[fixed] = [7]\nfixed += 8\nfixed++\nfixed--\nmissing = 9\nfixed =\n",
		},
		{
			name:   "embedded command",
			source: "vim9script\nconst fixed = 1\nglobal /x/ fixed = 2\n",
			want:   []syntax.Diagnostic{{Code: "vim/E46", Message: `Cannot change read-only variable "fixed"`}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			got := Analyze(file).Diagnostics
			if len(got) != len(test.want) {
				t.Fatalf("diagnostics = %#v, want %#v", got, test.want)
			}
			last := -1
			for index := range got {
				if got[index].Code != test.want[index].Code || got[index].Message != test.want[index].Message {
					t.Fatalf("diagnostic[%d] = %#v, want %#v", index, got[index], test.want[index])
				}
				name := immutableDiagnosticName(got[index].Message)
				start := strings.LastIndex(test.source, name+" =")
				wantSpan := syntax.Span{Start: start, End: start + len(name)}
				if got[index].Span.Start <= last || got[index].Span != wantSpan || file.Text(got[index].Span) != name {
					t.Fatalf("diagnostic[%d] span = %#v (%q), want %#v (%q)", index, got[index].Span, file.Text(got[index].Span), wantSpan, name)
				}
				last = got[index].Span.Start
			}
		})
	}
}

func immutableDiagnosticName(message string) string {
	if index := strings.LastIndex(message, ": "); index >= 0 {
		return message[index+2:]
	}
	return strings.Trim(message[len(`Cannot change read-only variable `):], `"`)
}

func TestAnalyzeUndefinedVim9DefIdentifiers(t *testing.T) {
	source := `vim9script
var scriptValue = 1
def Check(value: number)
  echo missing
  echo missing + scriptValue
  if value > 0
    echo blockMissing
  endif
  echo MissingCall()
  echo MissingGeneric<number>(1)
  echo s:scoped
  echo this
  echo super
  var lambda = (item: number) => item + lambdaMissing
  legacy echo legacyMissing
enddef
echo outsideMissing
`
	file := syntax.Parse(source)
	result := Analyze(file)
	want := []string{"missing", "missing", "blockMissing", "lambdaMissing"}
	var got []syntax.Diagnostic
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1001" {
			got = append(got, diagnostic)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("E1001 diagnostics = %#v, want %v (all diagnostics = %#v, syntax diagnostics = %#v)", got, want, result.Diagnostics, file.Diagnostics)
	}
	for index, name := range want {
		diagnostic := got[index]
		if diagnostic.Message != "Variable not found: "+name || file.Text(diagnostic.Span) != name {
			t.Fatalf("diagnostic[%d] = %#v (%q), want %q", index, diagnostic, file.Text(diagnostic.Span), name)
		}
	}
}
