package analysis

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestAssignmentExecuteFactReachesNestedCommands(t *testing.T) {
	for _, test := range []struct {
		name, body string
		embedded   bool
	}{
		{"embedded", "autocmd User LockIt lockvar nameX\n", true},
		{"lambda body", "var Callback = () => {\n  lockvar nameX\n}\n", false},
	} {
		for _, execute := range []bool{false, true} {
			name := test.name + "/without execute"
			source := "vim9script\n" + test.body
			if execute {
				name = test.name + "/with execute"
				source += "execute 'var nameX = []'\n"
			}
			t.Run(name, func(t *testing.T) {
				file := syntax.Parse(source)
				if len(file.Diagnostics) != 0 || len(file.Commands) < 2 {
					t.Fatalf("invalid fixture: commands=%#v syntax=%#v", file.Commands, file.Diagnostics)
				}
				if test.embedded {
					body := file.Commands[1].Embedded
					if body == nil || len(body.Commands) != 1 || body.Commands[0].Canonical != "lockvar" {
						t.Fatalf("expected embedded lockvar, got %#v", body)
					}
				} else {
					declaration := file.Commands[1].Declaration
					if declaration == nil || declaration.Initializer == nil || declaration.Initializer.LambdaBody == nil ||
						len(declaration.Initializer.LambdaBody.Commands) != 1 || declaration.Initializer.LambdaBody.Commands[0].Canonical != "lockvar" {
						t.Fatalf("expected lambda body lockvar, got %#v", declaration)
					}
				}
				count := 0
				for _, diagnostic := range Analyze(file).Diagnostics {
					if diagnostic.Code == "vim/E1246" {
						count++
						if file.Text(diagnostic.Span) != "nameX" {
							t.Fatalf("E1246 span = %q", file.Text(diagnostic.Span))
						}
					}
				}
				want := 1
				if execute {
					want = 0
				}
				if count != want {
					t.Fatalf("E1246 count = %d, want %d", count, want)
				}
			})
		}
	}
}

func TestOperatorCompletenessAndLambdaBody(t *testing.T) {
	for _, test := range []struct {
		name, source string
		wantE684     int
		incomplete   bool
	}{
		{"complete expression", "echo [1][2]\n", 1, false},
		{"incomplete parent", "echo [1][2] +\n", 1, true},
		{"incomplete index", "echo [1][\n", 0, true},
		{"lambda body", "vim9script\nvar Callback = () => {\n  echo [1][2]\n}\n", 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			if (len(file.Diagnostics) > 0) != test.incomplete {
				t.Fatalf("fixture recovery: syntax=%#v", file.Diagnostics)
			}
			command := &file.Commands[0]
			if test.name == "lambda body" {
				command = &file.Commands[1]
			}
			if test.incomplete && (len(command.Expressions) != 1 || !expressionContainsMissing(command.Expressions[0])) {
				t.Fatalf("incomplete fixture lacks recovering expression: %#v", command.Expressions)
			}
			count := 0
			for _, diagnostic := range Analyze(file).Diagnostics {
				if diagnostic.Code == "vim/E684" {
					count++
				}
			}
			if count != test.wantE684 {
				t.Fatalf("E684 count = %d, want %d; syntax = %#v", count, test.wantE684, file.Diagnostics)
			}
		})
	}
}

func TestOperatorCompleteOuterKeepsLambdaBodyIncomplete(t *testing.T) {
	file := syntax.Parse("vim9script\necho len([() => {\n  echo 'bad' ? 1 :\n}])\n")
	if len(file.Diagnostics) == 0 || len(file.Commands) != 2 || len(file.Commands[1].Expressions) != 1 {
		t.Fatalf("expected one recovering lambda body: commands=%#v syntax=%#v", file.Commands, file.Diagnostics)
	}
	outer := file.Commands[1].Expressions[0]
	if outer.Kind != syntax.ExpressionCall || expressionContainsMissing(outer) || len(outer.Children) != 2 {
		t.Fatalf("outer call must be complete by Children: %#v", outer)
	}
	list := outer.Children[1]
	if list == nil || list.Kind != syntax.ExpressionList || len(list.Children) != 1 {
		t.Fatalf("outer list = %#v", list)
	}
	lambda := list.Children[0]
	if lambda == nil || lambda.Kind != syntax.ExpressionLambda || lambda.LambdaBody == nil || len(lambda.LambdaBody.Commands) != 1 ||
		len(lambda.LambdaBody.Commands[0].Expressions) != 1 {
		t.Fatalf("lambda body = %#v", lambda)
	}
	bodyExpression := lambda.LambdaBody.Commands[0].Expressions[0]
	if bodyExpression.Kind != syntax.ExpressionTernary || len(bodyExpression.Children) != 3 || !expressionContainsMissing(bodyExpression) {
		t.Fatalf("lambda body must contain a missing ternary branch: %#v", bodyExpression)
	}
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vim/E1135" && diagnostic.Span.Start >= bodyExpression.Span.Start && diagnostic.Span.End <= bodyExpression.Span.End {
			t.Fatalf("incomplete lambda body received string-to-Bool error: %#v", diagnostic)
		}
	}
}

func TestSemanticCollectorsCancelWithinOneExpression(t *testing.T) {
	file := syntax.Parse("echo [" + strings.Repeat("1,", 512) + "1]\n")
	for _, test := range []struct {
		name string
		run  func(*FileAnalysis)
	}{
		{"operator", func(result *FileAnalysis) { collectOperatorDiagnostics(result, file.Commands, result.Root) }},
		{"assignment", func(result *FileAnalysis) {
			collectAssignmentCommandDiagnostics(result, file.Commands, result.Root, false)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze(file)
			calls := 0
			result.progress = &analysisProgress{yield: func() error {
				calls++
				return context.Canceled
			}}
			test.run(result)
			if calls != 1 || !errors.Is(result.progress.err, context.Canceled) || result.progress.steps != 64 {
				t.Fatalf("cancellation: calls=%d err=%v steps=%d", calls, result.progress.err, result.progress.steps)
			}
		})
	}
}
