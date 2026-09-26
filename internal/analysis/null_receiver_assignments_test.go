package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestNullReceiverLaterAssignmentRetainsEarlierDiagnostic(t *testing.T) {
	source := "vim9script\n" + nullGuardClass + "var value: C\nvalue.Foo()\nvalue = C.new()\n"
	file := syntax.Parse(source)
	var got []syntax.Span
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vim/E1360" {
			got = append(got, diagnostic.Span)
		}
	}
	start := strings.Index(source, "value.Foo()")
	want := syntax.Span{Start: start, End: start + len("value")}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("E1360 spans = %#v, want %#v", got, want)
	}
}

func TestNullReceiverAssignmentOrderBoundaries(t *testing.T) {
	for _, test := range []struct {
		name         string
		source       string
		receivers    []string
		embeddedBody bool
	}{
		{
			name: "deferred bodies establish their own facts",
			source: "vim9script\n" + nullGuardClass + `def Check()
  var value: C
  value.Foo()
  value = C.new()
enddef
var Callback = () => {
  var local: C
  local.Foo()
  local = C.new()
}
`,
			receivers: []string{"value.Foo()", "local.Foo()"},
		},
		{
			name: "uninvoked writers preserve the outer fact",
			source: "vim9script\n" + nullGuardClass + `var value: C
def Write()
  value = C.new()
enddef
var Callback = () => {
  value = C.new()
}
command WriteCommand {
  value = C.new()
}
autocmd BufEnter * {
  value = C.new()
}
value.Foo()
`,
			receivers: []string{"value.Foo()"},
		},
		{
			name: "called writer clears the fact",
			source: "vim9script\n" + nullGuardClass + `var value: C
def Write()
  value = C.new()
enddef
Write()
value.Foo()
`,
		},
		{
			name: "called user command clears the fact",
			source: "vim9script\n" + nullGuardClass + `var value: C
command WriteCommand {
  value = C.new()
}
WriteCommand
value.Foo()
`,
		},
		{
			name: "deferred capture does not use outer source order",
			source: "vim9script\n" + nullGuardClass + `var value: C
def Read()
  value.Foo()
enddef
value = C.new()
`,
		},
		{
			name: "loop invalidates the fact across iterations",
			source: "vim9script\n" + nullGuardClass + `var value: C
for index in [0, 1]
  if index == 1
    value.Foo()
  endif
  value = C.new()
endfor
`,
		},
		{
			name: "assignment reads its right hand side before invalidation",
			source: `vim9script
class C
  def Copy(): C
    return C.new()
  enddef
endclass
var value: C
value = value.Copy()
`,
			receivers: []string{"value.Copy()"},
		},
		{
			name: "unknown ex command clears the fact",
			source: "vim9script\n" + nullGuardClass + `var value: C
execute 'echo "side effect"'
value.Foo()
value = C.new()
`,
		},
		{
			name:         "unknown ex command clears before its embedded body",
			embeddedBody: true,
			source: "vim9script\n" + nullGuardClass + `var value: C
global /x/ echo value.Foo()
value = C.new()
`,
		},
		{
			name: "shadowed declarations retain independent facts",
			source: "vim9script\n" + nullGuardClass + `var value: C
value.Foo()
def Check()
  var value: C
  value.Foo()
  value = C.new()
enddef
value = C.new()
`,
			receivers: []string{"value.Foo()", "value.Foo()"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			if len(file.Diagnostics) != 0 {
				t.Fatalf("parse diagnostics = %#v", file.Diagnostics)
			}
			if test.embeddedBody {
				found := false
				for _, command := range file.Commands {
					if command.Canonical == "global" && command.Embedded != nil {
						found = true
						break
					}
				}
				if !found {
					t.Fatal("global command has no embedded body")
				}
			}
			var got []syntax.Span
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E1360" {
					got = append(got, diagnostic.Span)
				}
			}
			var want []syntax.Span
			from := 0
			for _, receiver := range test.receivers {
				start := strings.Index(test.source[from:], receiver)
				if start < 0 {
					t.Fatalf("receiver %q not found", receiver)
				}
				start += from
				name := receiver[:strings.Index(receiver, ".")]
				want = append(want, syntax.Span{Start: start, End: start + len(name)})
				from = start + len(receiver)
			}
			if len(got) != len(want) {
				t.Fatalf("E1360 receiver spans = %#v, want %#v", got, want)
			}
			for index := range want {
				if got[index] != want[index] {
					t.Fatalf("E1360 receiver spans = %#v, want %#v", got, want)
				}
			}
		})
	}
}
