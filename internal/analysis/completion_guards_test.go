package analysis

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestCompletionTypesGuardedIdentifierQueries(t *testing.T) {
	for _, test := range []struct {
		name, source, want string
	}{
		{
			name:   "positive parenthesized type guard",
			source: "vim9script\ndef F(value: any)\n  if (type((value)) == v:t_string)\n    return value\n  endif\nenddef\n",
			want:   "string",
		},
		{
			name:   "reversed elseif guard",
			source: "vim9script\ndef F(value: any)\n  if type(value) == v:t_number\n  elseif v:t_string == type(value)\n    return value\n  endif\nenddef\n",
			want:   "string",
		},
		{
			name:   "negative guard else",
			source: "vim9script\ndef F(value: any)\n  if type(value) != v:t_string\n  else\n    return value\n  endif\nenddef\n",
			want:   "string",
		},
		{
			name:   "negated guard else",
			source: "vim9script\ndef F(value: any)\n  if !(type(value) != v:t_string)\n    return value\n  endif\nenddef\n",
			want:   "string",
		},
		{
			name:   "preserves list element type",
			source: "vim9script\ndef F(value: list<string>)\n  if type(value) == v:t_list\n    return value\n  endif\nenddef\n",
			want:   "list<string>",
		},
		{
			name:   "incomplete branch still refines",
			source: "vim9script\ndef F(value: any)\n  if type(value) == v:t_string\n    return value\n",
			want:   "string",
		},
		{
			name:   "malformed guard does not refine",
			source: "vim9script\ndef F(value: any)\n  if type(value) == v:t_string trailing\n    return value\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "missing closing parenthesis rejects guard",
			source: "vim9script\ndef F(value: any): any\n  if (type(value) == v:t_string\n    return value\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "compound guard stays conservative",
			source: "vim9script\ndef F(value: any): any\n  if type(value) == v:t_string && true\n    return value\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "negative branch does not refine",
			source: "vim9script\ndef F(value: any): any\n  if type(value) != v:t_string\n    return value\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "guard header is not the guarded body",
			source: "vim9script\ndef F(value: any)\n  if type(value) == v:t_string\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "malformed else does not refine",
			source: "vim9script\ndef F(value: any): any\n  if type(value) != v:t_string\n  else trailing\n    return value\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "outside branch does not refine",
			source: "vim9script\ndef F(value: any): any\n  if type(value) == v:t_string\n  endif\n  return value\nenddef\n",
			want:   "any",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := completionGuardType(t, test.source); got != test.want {
				t.Fatalf("guarded type = %q, want %q\n%s", got, test.want, test.source)
			}
		})
	}
}

func TestCompletionTypesGuardedInstanceofAndEffects(t *testing.T) {
	const classes = "class Base\n  def BaseMember()\n  enddef\nendclass\nclass Derived extends Base\n  def DerivedMember()\n  enddef\nendclass\n"
	for _, test := range []struct {
		name, source, want string
	}{
		{
			name:   "local class",
			source: "vim9script\n" + classes + "def F(value: Base)\n  if instanceof(value, Derived)\n    return value\n  endif\nenddef\n",
			want:   "Derived",
		},
		{
			name:   "unclosed instanceof call rejects guard",
			source: "vim9script\n" + classes + "def F(value: Base): Base\n  if instanceof(value, Derived\n    return value\n  endif\nenddef\n",
			want:   "Base",
		},
		{
			name:   "negated instanceof else",
			source: "vim9script\n" + classes + "def F(value: Base): Base\n  if !instanceof(value, Derived)\n  else\n    return value\n  endif\nenddef\n",
			want:   "Derived",
		},
		{
			name:   "prior assignment invalidates",
			source: "vim9script\ndef F(value: any)\n  if type(value) == v:t_string\n    value = 1\n    return value\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "nested write invalidates",
			source: "vim9script\ndef F(value: any, flag: bool)\n  if type(value) == v:t_string\n    if flag\n      value = 1\n    endif\n    return value\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "prior call invalidates",
			source: "vim9script\ndef Change()\nenddef\ndef F(value: any)\n  if type(value) == v:t_string\n    Change()\n    return value\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "script call cannot revive interpreted guard",
			source: "vim9script\nvar value: any = ''\nif type(value) == v:t_string\n  Change()\n  echo value\nendif\n",
			want:   "any",
		},
		{
			name:   "earlier call argument invalidates",
			source: "vim9script\ndef F(value: any): any\n  if type(value) == v:t_string\n    return Use(Change(), value)\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "declaration initializer call invalidates",
			source: "vim9script\ndef F(value: any): any\n  if type(value) == v:t_string\n    var other = Change()\n    return value\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "compound assignment invalidates",
			source: "vim9script\ndef F(value: any): any\n  if type(value) == v:t_string\n    value ..= 'x'\n    return value\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "destructuring assignment invalidates",
			source: "vim9script\ndef F(value: any): any\n  if type(value) == v:t_string\n    [value] = [1]\n    return value\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "loop invalidates",
			source: "vim9script\ndef F(value: any)\n  if type(value) == v:t_string\n    while false\n    endwhile\n    return value\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "try invalidates",
			source: "vim9script\ndef F(value: any)\n  if type(value) == v:t_string\n    try\n    finally\n    endtry\n    return value\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "current return reads before command",
			source: "vim9script\ndef F(value: any)\n  if type(value) == v:t_string\n    return value\n  endif\nenddef\n",
			want:   "string",
		},
		{
			name:   "current call argument reads before call",
			source: "vim9script\ndef Use(value: any)\nenddef\ndef F(value: any)\n  if type(value) == v:t_string\n    call Use(value)\n  endif\nenddef\n",
			want:   "string",
		},
		{
			name:   "deferred function body does not invalidate",
			source: "vim9script\ndef F(value: any)\n  if type(value) == v:t_string\n    def Change()\n      value = 1\n    enddef\n    return value\n  endif\nenddef\n",
			want:   "string",
		},
		{
			name:   "defining lambda does not execute its call",
			source: "vim9script\ndef F(value: any): any\n  if type(value) == v:t_string\n    var Callback = () => Change()\n    return value\n  endif\nenddef\n",
			want:   "string",
		},
		{
			name:   "outer guard does not enter command body",
			source: "vim9script\ndef F(value: any)\n  if type(value) == v:t_string\n    command Test {\n      echo value\n    }\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "outer guard does not enter lambda body",
			source: "vim9script\ndef F(value: any)\n  if type(value) == v:t_string\n    var Callback = () => {\n      return value\n    }\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "outer guard does not enter autocmd body",
			source: "vim9script\nvar value: any = ''\nif type(value) == v:t_string\n  autocmd User Test echo value\nendif\n",
			want:   "any",
		},
		{
			name:   "outer guard does not enter expression mapping",
			source: "vim9script\nvar value: any = ''\nif type(value) == v:t_string\n  nnoremap <expr> x value\nendif\n",
			want:   "any",
		},
		{
			name:   "shadowed instanceof rejects guard",
			source: "vim9script\nclass Base\nendclass\nclass Derived extends Base\nendclass\ndef instanceof(value: Base, class: Base): bool\n  return true\nenddef\ndef F(value: Base)\n  if instanceof(value, Derived)\n    return value\n  endif\nenddef\n",
			want:   "Base",
		},
		{
			name:   "class alias rejects guard",
			source: "vim9script\nclass Base\nendclass\nclass Derived extends Base\nendclass\ntype Alias = Derived\ndef F(value: Base)\n  if instanceof(value, Alias)\n    return value\n  endif\nenddef\n",
			want:   "Base",
		},
		{
			name:   "lambda owns its guard",
			source: "vim9script\ndef F(value: any)\n  var Callback = () => {\n    if type(value) == v:t_string\n      return value\n    endif\n  }\nenddef\n",
			want:   "string",
		},
		{
			name:   "side effect in static sample rejects guard",
			source: "vim9script\ndef Change(): string\n  return ''\nenddef\ndef F(value: any)\n  if type(value) == type([Change()])\n    return value\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "shadowed type rejects guard",
			source: "vim9script\ndef type(value: any): number\n  return 1\nenddef\ndef F(value: any)\n  if type(value) == v:t_string\n    return value\n  endif\nenddef\n",
			want:   "any",
		},
		{
			name:   "legacy keeps existing inference",
			source: "function F(value)\n  if type(a:value) == v:t_string\n    return a:value\n  endif\nendfunction\n",
			want:   "string",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := completionGuardType(t, test.source); got != test.want {
				t.Fatalf("guarded type = %q, want %q\n%s", got, test.want, test.source)
			}
		})
	}
}

func TestCompletionGuardPreservesFunctionSignature(t *testing.T) {
	file := syntax.Parse("vim9script\ndef F(value: func(number): string): any\n  if type(value) == v:t_func\n    return value\n  endif\nenddef\n")
	expression := completionGuardIdentifier(file.Commands, strings.LastIndex(file.Source, "value"))
	if expression == nil {
		t.Fatal("missing receiver")
	}
	typ := NewCompletionTypes(CollectCompletionFacts(file)).TypeOf(expression, expression.Span.Start)
	if typ.Name != "func" || len(typ.Arguments) != 1 || typ.Arguments[0].Name != "number" || typ.Return == nil || typ.Return.Name != "string" || !typ.ArgumentCountKnown || typ.RequiredArguments != 1 {
		t.Fatalf("guard lost declared function signature: %#v", typ)
	}
}

func completionGuardType(t *testing.T, source string) string {
	t.Helper()
	file := syntax.Parse(source)
	offset := strings.LastIndex(source, "value")
	if offset < 0 {
		t.Fatal("missing receiver")
	}
	expression := completionGuardIdentifier(file.Commands, offset)
	if expression == nil {
		t.Fatalf("missing identifier at %d\n%s", offset, source)
	}
	return valueTypeDisplay(NewCompletionTypes(CollectCompletionFacts(file)).TypeOf(expression, expression.Span.Start))
}

func completionGuardIdentifier(commands []syntax.Command, offset int) *syntax.Expression {
	var found *syntax.Expression
	var walk func(*syntax.Expression)
	walk = func(expression *syntax.Expression) {
		if expression == nil || found != nil {
			return
		}
		if expression.Kind == syntax.ExpressionIdentifier && (expression.Span.Start == offset || expression.Span.End == offset+len("value")) {
			found = expression
			return
		}
		for _, child := range expression.Children {
			walk(child)
		}
		if expression.LambdaBody != nil {
			found = completionGuardIdentifier(expression.LambdaBody.Commands, offset)
		}
	}
	for index := range commands {
		command := &commands[index]
		for _, expression := range command.Expressions {
			walk(expression)
		}
		for _, expression := range command.Targets {
			walk(expression)
		}
		if command.Declaration != nil {
			walk(command.Declaration.Initializer)
		}
		if command.Mapping != nil {
			walk(command.Mapping.RHSExpression)
		}
		if command.Embedded != nil {
			found = completionGuardIdentifier(command.Embedded.Commands, offset)
		}
		if found != nil {
			return found
		}
	}
	return found
}
