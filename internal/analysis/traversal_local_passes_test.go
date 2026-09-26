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
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vim/E1360" {
			t.Fatalf("assignment left a null receiver candidate: %#v", diagnostic)
		}
	}
}

func TestOverwriteRiskDiagnosticsRetainPhaseOrderAndConfigSeverity(t *testing.T) {
	file := syntax.Parse("command Outer command Nested echo 'value'\nfunction Legacy()\nendfunction\nfunction Other()\nendfunction\n")
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
	want := []string{"vim/E122", "vim/E122", "vim/E174", "vim/E174"}
	if !reflect.DeepEqual(codes, want) {
		t.Fatalf("diagnostic phase order = %#v, want %#v", codes, want)
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
