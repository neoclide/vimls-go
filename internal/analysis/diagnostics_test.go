package analysis

import (
	"strings"
	"testing"

	"github.com/chemzqm/vimls-go/internal/syntax"
)

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
