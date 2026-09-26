package analysis

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

const nullGuardClass = "class C\n  def Foo()\n  enddef\nendclass\n"

func nullReceiverCount(source string) int {
	count := 0
	for _, diagnostic := range Analyze(syntax.Parse(source)).Diagnostics {
		if diagnostic.Code == "vim/E1360" {
			count++
		}
	}
	return count
}

func nullReceiverOffsets(source string) []int {
	file := syntax.Parse(source)
	var offsets []int
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vim/E1360" {
			offsets = append(offsets, diagnostic.Span.Start)
		}
	}
	return offsets
}

func occurrence(source, text string, index int) int {
	start := -1
	for range index + 1 {
		start = strings.Index(source[start+1:], text) + start + 1
	}
	return start
}

func TestNullReceiverGuardsSuppressKnownNonNullBranch(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         int
	}{
		{
			name:   "not null object true branch",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif value != null_object\n  value.Foo()\nendif\nvalue.Foo()\n",
			want:   1,
		},
		{
			name:   "swapped parenthesized negation",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif !(null_object == (value))\n  value.Foo()\nendif\n",
			want:   0,
		},
		{
			name:   "isnot null object",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif value isnot null_object\n  value.Foo()\nendif\n",
			want:   0,
		},
		{
			name:   "generic null identity is not a proof",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif value isnot null\n  value.Foo()\nendif\n",
			want:   1,
		},
		{
			name:   "all visible writes exclude candidate",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif value != null_object\n  value.Foo()\nendif\nvalue = C.new()\n",
			want:   0,
		},
		{
			name:   "multiple guarded calls remain protected",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif value != null_object\n  value.Foo()\n  value.Foo()\nendif\n",
			want:   0,
		},
		{
			name:   "compound predicate is excluded",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif value != null_object && true\n  value.Foo()\nendif\n",
			want:   1,
		},
		{
			name:   "incomplete if is excluded",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif value != null_object\n  value.Foo()\n",
			want:   1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := nullReceiverCount(test.source); got != test.want {
				t.Fatalf("E1360 count = %d, want %d\n%s", got, test.want, test.source)
			}
		})
	}
}

func TestNullReceiverInstanceofGuardsAreNarrow(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         int
	}{
		{
			name:   "direct ordinary class",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif instanceof(value, C)\n  value.Foo()\nendif\n",
			want:   0,
		},
		{
			name:   "extra class argument",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif instanceof(value, C, C)\n  value.Foo()\nendif\n",
			want:   1,
		},
		{
			name:   "class alias",
			source: "vim9script\n" + nullGuardClass + "type Alias = C\nvar value: C\nif instanceof(value, Alias)\n  value.Foo()\nendif\n",
			want:   1,
		},
		{
			name:   "interface",
			source: "vim9script\ninterface I\n  def Foo()\nendinterface\nvar value: I\nif instanceof(value, I)\n  value.Foo()\nendif\n",
			want:   1,
		},
		{
			name:   "null class",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif instanceof(value, null_class)\n  value.Foo()\nendif\n",
			want:   1,
		},
		{
			name:   "shadowed builtin name",
			source: "vim9script\n" + nullGuardClass + "def instanceof(value: C, class: C): bool\n  return true\nenddef\nvar value: C\nif instanceof(value, C)\n  value.Foo()\nendif\n",
			want:   1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := nullReceiverCount(test.source); got != test.want {
				t.Fatalf("E1360 count = %d, want %d\n%s", got, test.want, test.source)
			}
		})
	}
}

func TestNullReceiverGuardBranchLocationsAndWhitelist(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         []int
	}{
		{
			name:   "equal null object protects only else",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif value == null_object\n  value.Foo()\nelse\n  value.Foo()\nendif\n",
			want:   []int{0},
		},
		{
			name:   "elseif does not protect else",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif value == null_object\n  value.Foo()\nelseif value != null_object\n  value.Foo()\nelse\n  value.Foo()\nendif\n",
			want:   []int{0, 2},
		},
		{
			name:   "nested guard ends with its own branch",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif value != null_object\n  if value != null_object\n    value.Foo()\n  endif\n  value.Foo()\nendif\nvalue.Foo()\n",
			want:   []int{2},
		},
		{
			name:   "shadowed declaration is not protected",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif value != null_object\n  value.Foo()\n  var value: C\n  value.Foo()\nendif\n",
			want:   []int{1},
		},
		{
			name:   "negated instanceof protects else only",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif !instanceof(value, C)\n  value.Foo()\nelse\n  value.Foo()\nendif\n",
			want:   []int{0},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := nullReceiverOffsets(test.source)
			want := make([]int, len(test.want))
			for index, occurrenceIndex := range test.want {
				want[index] = occurrence(test.source, "value.Foo()", occurrenceIndex)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("E1360 offsets = %v, want %v\n%s", got, want, test.source)
			}
		})
	}

	for _, test := range []struct {
		name, condition, body string
	}{
		{"not null object", "value != null_object", "  value.Foo()\n"},
		{"swapped not null object", "null_object != value", "  value.Foo()\n"},
		{"isnot null object", "value isnot null_object", "  value.Foo()\n"},
		{"not generic null", "value != null", "  value.Foo()\n"},
		{"equals null object else", "value == null_object", "else\n  value.Foo()\n"},
		{"is null object else", "value is null_object", "else\n  value.Foo()\n"},
		{"equals generic null else", "value == null", "else\n  value.Foo()\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "vim9script\n" + nullGuardClass + "var value: C\nif " + test.condition + "\n" + test.body + "endif\n"
			if got := nullReceiverCount(source); got != 0 {
				t.Fatalf("whitelisted guard reported %d E1360 diagnostics\n%s", got, source)
			}
		})
	}
}

func TestNullReceiverGuardsRequireCompleteHeaders(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         int
	}{
		{
			name:   "if trailing expression",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif value != null_object junk\n  value.Foo()\nendif\n",
			want:   1,
		},
		{
			name:   "if bang prefix",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif! value != null_object\n  value.Foo()\nendif\n",
			want:   1,
		},
		{
			name:   "elseif trailing expression",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif false\nelseif value != null_object junk\n  value.Foo()\nendif\n",
			want:   1,
		},
		{
			name:   "comment trivia remains valid",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif value != null_object # safe comment\n  value.Foo()\nendif\n",
			want:   0,
		},
		{
			name:   "else tail invalidates false proof",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif value == null_object\n  value.Foo()\nelse junk\n  value.Foo()\nendif\n",
			want:   2,
		},
		{
			name:   "endif tail invalidates proof",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif value != null_object\n  value.Foo()\nendif junk\n",
			want:   1,
		},
		{
			name:   "incomplete callable starts empty",
			source: "vim9script\n" + nullGuardClass + "var value: C\nif value != null_object\n  def MissingEnd()\n    value.Foo()\n",
			want:   1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := nullReceiverCount(test.source); got != test.want {
				t.Fatalf("E1360 count = %d, want %d\n%s", got, test.want, test.source)
			}
		})
	}
}

func TestNullReceiverDeferredBodiesCanEstablishOwnGuards(t *testing.T) {
	source := "vim9script\n" + nullGuardClass + `var value: C
if value != null_object
  def FromDef()
    if value != null_object
      value.Foo()
    endif
  enddef
  var FromLambda = () => {
    if value != null_object
      value.Foo()
    endif
  }
  command FromCommand {
    if value != null_object
      value.Foo()
    endif
  }
  autocmd BufEnter * {
    if value != null_object
      value.Foo()
    endif
  }
endif
`
	if got := nullReceiverCount(source); got != 0 {
		t.Fatalf("E1360 count = %d, want 0\n%s", got, source)
	}
}

func TestNullReceiverGuardTraversalHonorsCancellation(t *testing.T) {
	source := "vim9script\n" + nullGuardClass
	for index := 0; index < 96; index++ {
		source += "var value" + strconv.Itoa(index) + ": C\nvalue" + strconv.Itoa(index) + ".Foo()\n"
	}
	file := syntax.Parse(source)
	result := Analyze(file)
	result.Diagnostics = nil
	calls := 0
	result.progress = &analysisProgress{yield: func() error {
		calls++
		return context.Canceled
	}}
	collectNullReceiverDiagnostics(result)
	if calls != 1 || !errors.Is(result.progress.err, context.Canceled) {
		t.Fatalf("cancellation calls=%d err=%v", calls, result.progress.err)
	}
}

func TestNullReceiverInstanceofDoesNotChangeStaticTypes(t *testing.T) {
	source := `vim9script
class Base
endclass
class Derived extends Base
  def DerivedOnly()
  enddef
endclass
def NeedsDerived(value: Derived)
enddef
def Check()
  var value: Base
  if instanceof(value, Derived)
    var copied = value
    value.DerivedOnly()
    NeedsDerived(value)
  endif
enddef
`
	file := syntax.Parse(source)
	result := Analyze(file)
	var got []string
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1325" || diagnostic.Code == "vim/E1013" {
			got = append(got, diagnostic.Code)
		}
		if diagnostic.Code == "vim/E1360" {
			t.Fatalf("instanceof guard retained E1360: %#v", result.Diagnostics)
		}
	}
	if !slices.Equal(got, []string{"vim/E1325", "vim/E1013"}) {
		t.Fatalf("static diagnostics = %#v; all = %#v", got, result.Diagnostics)
	}
	declarations := declarationsByName(result)
	value, copied := declarations["value"], declarations["copied"]
	if value == nil || value.Type.Name != "Base" || copied == nil || copied.Type.Name != "Base" {
		t.Fatalf("declarations: value=%#v copied=%#v", value, copied)
	}
	var initializer *syntax.Expression
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Declaration != nil && len(command.Declaration.Bindings) == 1 && file.Text(command.Declaration.Bindings[0].Name) == "copied" {
			initializer = command.Declaration.Initializer
			break
		}
	}
	if initializer == nil || result.TypeOf(initializer).Name != "Base" {
		t.Fatalf("initializer type = %#v", result.TypeOf(initializer))
	}
	facts := CollectCompletionFacts(file)
	completion := NewCompletionTypes(facts)
	completionDeclarations := declarationsByName(facts)
	completionValue, completionCopied := completionDeclarations["value"], completionDeclarations["copied"]
	if completionValue == nil || completionCopied == nil || completion.DeclarationType(completionValue).Name != "Base" || completion.DeclarationType(completionCopied).Name != "Base" || completion.TypeOf(initializer, initializer.Span.Start).Name != "Derived" {
		t.Fatalf("completion types = value %#v copied %#v initializer %#v", completion.DeclarationType(completionValue), completion.DeclarationType(completionCopied), completion.TypeOf(initializer, initializer.Span.Start))
	}
}

func TestNullReceiverGuardsDoNotCrossDeferredBodies(t *testing.T) {
	source := "vim9script\n" + nullGuardClass + `var value: C
if value != null_object
  def FromDef()
    value.Foo()
  enddef
  var FromLambda = () => {
    value.Foo()
  }
  var ShortLambda = () => value.Foo()
  def WithDefault(argument: any = value.Foo())
  enddef
  command FromCommand {
    value.Foo()
  }
  autocmd BufEnter * {
    value.Foo()
  }
  nnoremap <expr> x value.Foo()
endif
`
	file := syntax.Parse(source)
	result := Analyze(file)
	var got []string
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1360" {
			got = append(got, file.Text(diagnostic.Span))
		}
	}
	if len(got) != 7 {
		t.Fatalf("deferred executable bodies E1360 = %#v, want seven\nparse=%#v\n%s", got, file.Diagnostics, source)
	}
}
