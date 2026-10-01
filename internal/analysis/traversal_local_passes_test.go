package analysis

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestBuiltinCompletenessTraversalPreservesChildAndLambdaBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, source string
	}{
		{
			name:   "complete child of incomplete parent",
			source: "echo gettext('') +\n",
		},
		{
			name:   "complete outer call with incomplete lambda body",
			source: "vim9script\necho bindtextdomain('', () => {\n  echo 'bad' ? 1 :\n})\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			if len(file.Diagnostics) == 0 {
				t.Fatal("expected a recovering expression")
			}
			// Isolate the missing-node proof from parser diagnostic overlap.
			file.Diagnostics = nil
			result := Analyze(file)
			found := false
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "vim/E1175" && file.Text(diagnostic.Span) == "''" {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing complete builtin argument diagnostic: syntax=%#v diagnostics=%#v", file.Diagnostics, result.Diagnostics)
			}
		})
	}
}

func TestBuiltinCompleteOuterDoesNotCompleteLambdaBody(t *testing.T) {
	file := syntax.Parse("vim9script\necho len([() => {\n  echo bindtextdomain('', 'text' ..)\n}])\n")
	if len(file.Diagnostics) == 0 || len(file.Commands) != 2 || len(file.Commands[1].Expressions) != 1 {
		t.Fatalf("expected a recovering lambda body: %#v", file.Diagnostics)
	}
	outer := file.Commands[1].Expressions[0]
	if outer.Kind != syntax.ExpressionCall || expressionContainsMissing(outer) {
		t.Fatalf("outer call must be complete through Children: %#v", outer)
	}
	lambda := outer.Children[1].Children[0]
	if lambda.LambdaBody == nil || len(lambda.LambdaBody.Commands) != 1 || len(lambda.LambdaBody.Commands[0].Expressions) != 1 {
		t.Fatalf("expected one lambda body expression: %#v", lambda)
	}
	body := lambda.LambdaBody.Commands[0].Expressions[0]
	if body.Kind != syntax.ExpressionCall || !expressionContainsMissing(body) {
		t.Fatalf("lambda call must contain a missing child: %#v", body)
	}
	// Exercise missing-node suppression independently of the overlapping parser
	// diagnostic, which would otherwise mask an incorrectly inherited proof.
	file.Diagnostics = nil
	result := Analyze(file)
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "vim/E1175" {
			t.Fatalf("incomplete lambda body received an argument type error: %#v", diagnostic)
		}
	}
}

func TestBuiltinCompletenessTraversalChecksSingleExpressionCancellation(t *testing.T) {
	file := syntax.Parse("vim9script\necho " + strings.Repeat("abs(", 128) + "1" + strings.Repeat(")", 128) + "\n")
	result := newFileAnalysis(file, false)
	calls := 0
	result.progress = &analysisProgress{yield: func() error {
		calls++
		return context.Canceled
	}}
	collectBuiltinArgumentTypeDiagnostics(result, file.Commands, result.Root)
	if calls != 1 || !errors.Is(result.progress.err, context.Canceled) {
		t.Fatalf("cancellation = calls:%d err:%v", calls, result.progress.err)
	}
}

func TestBuiltinArgumentTraversalCoversFunctionDefaultsAndEnumArguments(t *testing.T) {
	for _, test := range []struct {
		name, source, code, span string
	}{
		{
			name:   "bad function default",
			source: "vim9script\ndef Default(value: number = abs('bad'))\nenddef\n",
			code:   "vim/E1013",
			span:   "'bad'",
		},
		{
			name:   "good function default",
			source: "vim9script\ndef Default(value: number = abs(-1))\nenddef\nDefault()\n",
		},
		{
			name:   "bad enum argument",
			source: "vim9script\nenum Sample\n  Bad(abs('bad'))\n  var value: number\n  def new(value: number)\n    this.value = value\n  enddef\nendenum\n",
			code:   "vim/E1219",
			span:   "'bad'",
		},
		{
			name:   "good enum argument",
			source: "vim9script\nenum Sample\n  Good(abs(-1))\n  var value: number\n  def new(value: number)\n    this.value = value\n  enddef\nendenum\n",
		},
		{
			name:   "function default arity once",
			source: "vim9script\ndef Default(value: number = abs(1, 2))\nenddef\n",
			code:   "vim/E118",
			span:   "abs",
		},
		{
			name:   "enum argument arity once",
			source: "vim9script\nenum Sample\n  Bad(abs(1, 2))\n  var value: number\n  def new(value: number)\n    this.value = value\n  enddef\nendenum\n",
			code:   "vim/E118",
			span:   "abs",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			if len(file.Diagnostics) != 0 {
				t.Fatalf("syntax diagnostics = %#v", file.Diagnostics)
			}
			diagnostics := Analyze(file).Diagnostics
			if test.code == "" {
				if len(diagnostics) != 0 {
					t.Fatalf("diagnostics = %#v", diagnostics)
				}
				return
			}
			if len(diagnostics) != 1 || diagnostics[0].Code != test.code || file.Text(diagnostics[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want one %s on %q", diagnostics, test.code, test.span)
			}
		})
	}
}

func TestNullReceiverAssignmentsUseLambdaAndEmbeddedScopes(t *testing.T) {
	source := "vim9script\nclass C\n  def Foo()\n  enddef\nendclass\nvar top: C\nvar Callback = () => {\n  var local: C\n  local.Foo()\n  local = C.new()\n}\ncommand Fix {\n  top = C.new()\n}\ntop.Foo()\necho Callback\n"
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("parse diagnostics = %#v", file.Diagnostics)
	}
	var lambda *syntax.Expression
	for index := range file.Commands {
		declaration := file.Commands[index].Declaration
		if declaration != nil && declaration.Initializer != nil && declaration.Initializer.Kind == syntax.ExpressionLambda {
			lambda = declaration.Initializer
			break
		}
	}
	if lambda == nil || lambda.Kind != syntax.ExpressionLambda {
		t.Fatalf("lambda = %#v", lambda)
	}
	file.Commands[len(file.Commands)-1].Expressions = append(file.Commands[len(file.Commands)-1].Expressions, lambda)
	var got []syntax.Span
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vim/E1360" {
			got = append(got, diagnostic.Span)
		}
	}
	want := []syntax.Span{
		{Start: strings.Index(source, "local.Foo()"), End: strings.Index(source, "local.Foo()") + len("local")},
		{Start: strings.Index(source, "top.Foo()"), End: strings.Index(source, "top.Foo()") + len("top")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("E1360 receiver spans = %#v, want %#v", got, want)
	}
}

func TestOverwriteRiskDiagnosticsRetainPhaseOrderAndConfigSeverity(t *testing.T) {
	file := syntax.Parse("command Outer echo 'one'\ncommand Outer echo 'two'\nfunction Legacy()\nendfunction\nfunction Other()\nendfunction\n")
	if len(file.Diagnostics) != 0 {
		t.Fatalf("parse diagnostics = %#v", file.Diagnostics)
	}
	result := newFileAnalysis(file, true)
	collectOverwriteRiskDiagnostics(result, file.Commands)
	var codes []string
	for _, diagnostic := range result.Diagnostics {
		codes = append(codes, diagnostic.Code)
		if diagnostic.Severity == nil || *diagnostic.Severity != syntax.DiagnosticHint {
			t.Fatalf("diagnostic severity = %#v", diagnostic)
		}
	}
	want := []string{"vim/E122", "vim/E122", "vim/E174"}
	if !reflect.DeepEqual(codes, want) {
		t.Fatalf("diagnostic phase order = %#v, want %#v", codes, want)
	}
}

func TestOverwriteRiskDiagnosticsStopAtProgressCancellation(t *testing.T) {
	file := syntax.Parse(strings.Repeat("function F()\nendfunction\n", 128))
	if len(file.Diagnostics) != 0 {
		t.Fatalf("parse diagnostics = %#v", file.Diagnostics)
	}
	definitions := make([]syntax.Command, 0, 128)
	for _, command := range file.Commands {
		if command.Canonical == "function" {
			definitions = append(definitions, command)
		}
	}
	if len(definitions) != 128 {
		t.Fatalf("function definitions = %d", len(definitions))
	}
	deep := definitions
	for range 64 {
		deep = []syntax.Command{{Embedded: &syntax.CommandList{Commands: deep}}}
	}
	wide := make([]syntax.Command, 4)
	for index := range wide {
		wide[index].Embedded = &syntax.CommandList{Commands: definitions[index*32 : (index+1)*32]}
	}
	for _, test := range []struct {
		name     string
		commands []syntax.Command
	}{
		{name: "top level", commands: definitions},
		{name: "deep embedded", commands: deep},
		{name: "wide embedded", commands: wide},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := newFileAnalysis(file, false)
			calls := 0
			result.progress = &analysisProgress{yield: func() error {
				calls++
				return context.Canceled
			}}
			collectOverwriteRiskDiagnostics(result, test.commands)
			if calls != 1 || !errors.Is(result.progress.err, context.Canceled) || len(result.Diagnostics) >= len(definitions) {
				t.Fatalf("cancellation: calls=%d err=%v diagnostics=%d", calls, result.progress.err, len(result.Diagnostics))
			}
		})
	}
}

func TestNullReceiverSharedLambdaKeepsParameterScope(t *testing.T) {
	source := "vim9script\nclass C\n  def Foo()\n  enddef\nendclass\nvar item: C\nvar Callback = (item: C) => {\n  item = C.new()\n}\nitem.Foo()\necho Callback\n"
	file := syntax.Parse(source)
	if len(file.Diagnostics) != 0 {
		t.Fatalf("parse diagnostics = %#v", file.Diagnostics)
	}
	var lambda *syntax.Expression
	for index := range file.Commands {
		if declaration := file.Commands[index].Declaration; declaration != nil && declaration.Initializer != nil && declaration.Initializer.Kind == syntax.ExpressionLambda {
			lambda = declaration.Initializer
			break
		}
	}
	if lambda == nil || lambda.LambdaBody == nil || len(lambda.LambdaBody.Commands) != 1 || len(lambda.LambdaBody.Commands[0].Expressions) != 1 {
		t.Fatalf("expected a lambda containing one assignment: %#v", lambda)
	}
	assignment := lambda.LambdaBody.Commands[0].Expressions[0]
	if assignment.Kind != syntax.ExpressionAssignment {
		t.Fatalf("lambda command expression = %#v", assignment)
	}
	// Share the assignment through Children and the lambda through two roots.
	// Every visit must resolve its target to the parameter, never the outer item.
	lambda.Children = append(lambda.Children, assignment)
	file.Commands[len(file.Commands)-1].Expressions = append(file.Commands[len(file.Commands)-1].Expressions, lambda)
	var got []syntax.Span
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vim/E1360" {
			got = append(got, diagnostic.Span)
		}
	}
	start := strings.Index(source, "item.Foo()")
	want := []syntax.Span{{Start: start, End: start + len("item")}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outer null receiver diagnostics = %#v, want %#v", got, want)
	}
}

func TestAugroupEventFactsKeepFinalStateAndWorkspaceNames(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         []string
	}{
		{
			name:   "reference before definition and recreate",
			source: "autocmd group_a\naugroup group_a\naugroup END\nautocmd group_a\naugroup! group_a\nautocmd group_a\naugroup group_a\naugroup END\n",
		},
		{
			name:   "lambda group does not affect root",
			source: "vim9script\nvar Callback = () => {\n  augroup hidden\n  augroup END\n}\nautocmd hidden\n",
			want:   []string{"hidden"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			if len(file.Diagnostics) != 0 {
				t.Fatalf("parse diagnostics = %#v", file.Diagnostics)
			}
			var got []string
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vimls/unknown-autocmd-event" {
					got = append(got, file.Text(diagnostic.Span))
				}
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("unknown autocmd events = %#v, want %#v", got, test.want)
			}
		})
	}

	file := syntax.Parse("augroup! remote_group\nautocmd remote_group\n")
	_, span, ok := AutocmdAugroupReference(file, &file.Commands[1])
	if !ok {
		t.Fatal("missing remote_group reference")
	}
	diagnostics := []syntax.Diagnostic{{Code: "vimls/unknown-autocmd-event", Span: span}}
	if got := SuppressKnownAugroupEventDiagnostics(file, append([]syntax.Diagnostic(nil), diagnostics...), []string{"remote_group"}); len(got) != 0 {
		t.Fatalf("workspace augroup did not suppress diagnostic: %#v", got)
	}
	if got := SuppressKnownAugroupEventDiagnostics(file, append([]syntax.Diagnostic(nil), diagnostics...), []string{""}); !reflect.DeepEqual(got, diagnostics) {
		t.Fatalf("empty workspace name suppressed diagnostic: %#v", got)
	}
}
