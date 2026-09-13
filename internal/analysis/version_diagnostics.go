package analysis

import (
	"fmt"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
)

// VersionDiagnostics checks the analyzed file against a target Vim version,
// reporting errors (vim/E492, vim/E518, vim/E113, vim/E117, vim/E216) for
// language features that were introduced in later Vim versions.
// It leverages precomputed semantic references from fileAnalysis to avoid
// redundant AST expression traversals.
func VersionDiagnostics(file *syntax.File, fileAnalysis *FileAnalysis, targetVersion vimdata.VimVersion) []syntax.Diagnostic {
	if file == nil || !targetVersion.Valid() {
		return nil
	}
	var diags []syntax.Diagnostic
	seen := make(map[syntax.Span]bool)
	visitedCmd := make(map[*syntax.Command]bool)
	visitedExpr := make(map[*syntax.Expression]bool)

	// 1. Check built-in Ex commands, modifiers, :set options, and autocommand events.
	var checkCommand func(*syntax.Command)
	var walkExprLambdas func(*syntax.Expression)

	walkExprLambdas = func(expression *syntax.Expression) {
		if expression == nil || visitedExpr[expression] {
			return
		}
		visitedExpr[expression] = true
		for _, child := range expression.Children {
			walkExprLambdas(child)
		}
		if expression.LambdaBody != nil {
			for i := range expression.LambdaBody.Commands {
				checkCommand(&expression.LambdaBody.Commands[i])
			}
		}
	}

	checkCommand = func(cmd *syntax.Command) {
		if cmd == nil || visitedCmd[cmd] {
			return
		}
		visitedCmd[cmd] = true

		// Check command modifiers (e.g. `horizontal` introduced in 9.0.0342)
		for _, mod := range cmd.Modifiers {
			if seen[mod.Span] {
				continue
			}
			if history, ok := vimdata.LookupCommandHistory(mod.Name); ok {
				if history.VimVersion().Compare(targetVersion) > 0 {
					name := file.Text(mod.Span)
					if name == "" {
						name = mod.Name
					}
					diags = append(diags, syntax.Diagnostic{
						Code:    "vim/E492",
						Message: fmt.Sprintf("Not an editor command: %s (added in Vim %s)", name, history.Version),
						Span:    mod.Span,
					})
					seen[mod.Span] = true
				}
			}
		}

		// Built-in Ex command check
		if cmd.Kind == syntax.CommandBuiltin && !seen[cmd.Name] {
			if history, ok := vimdata.LookupCommandHistory(cmd.Canonical); ok {
				if history.VimVersion().Compare(targetVersion) > 0 {
					name := file.Text(cmd.Name)
					diags = append(diags, syntax.Diagnostic{
						Code:    "vim/E492",
						Message: fmt.Sprintf("Not an editor command: %s (added in Vim %s)", name, history.Version),
						Span:    cmd.Name,
					})
					seen[cmd.Name] = true
				}
			}
		}

		// :set options check
		if cmd.Set != nil {
			for _, option := range cmd.Set.Options {
				if seen[option.Name] {
					continue
				}
				optName := file.Text(option.Name)
				if history, ok := vimdata.LookupOptionHistory(optName); ok {
					if history.VimVersion().Compare(targetVersion) > 0 {
						diags = append(diags, syntax.Diagnostic{
							Code:    "vim/E518",
							Message: fmt.Sprintf("Unknown option: %s (added in Vim %s)", optName, history.Version),
							Span:    option.Name,
						})
						seen[option.Name] = true
					}
				}
			}
		}

		// Autocmd events check
		if cmd.Autocmd != nil {
			for _, eventSpan := range cmd.Autocmd.Events {
				if seen[eventSpan] {
					continue
				}
				eventName := strings.TrimSpace(file.Text(eventSpan))
				if eventName == "" || eventName == "*" {
					continue
				}
				if history, ok := vimdata.LookupAutocmdEventHistory(eventName); ok {
					if history.VimVersion().Compare(targetVersion) > 0 {
						diags = append(diags, syntax.Diagnostic{
							Code:    "vim/E216",
							Message: fmt.Sprintf("No such group or event: %s (added in Vim %s)", eventName, history.Version),
							Span:    eventSpan,
						})
						seen[eventSpan] = true
					}
				}
			}
		}

		// Embedded command list check (e.g. function/class/augroup/heredoc/etc.)
		if cmd.Embedded != nil {
			for i := range cmd.Embedded.Commands {
				checkCommand(&cmd.Embedded.Commands[i])
			}
		}

		// Traverse expressions within the command for lambda bodies containing commands
		for _, expr := range cmd.Expressions {
			walkExprLambdas(expr)
		}
		for _, expr := range cmd.Targets {
			walkExprLambdas(expr)
		}
		if cmd.Declaration != nil {
			walkExprLambdas(cmd.Declaration.Initializer)
		}
		if cmd.For != nil {
			walkExprLambdas(cmd.For.Iterable)
		}
		if cmd.Mapping != nil {
			walkExprLambdas(cmd.Mapping.RHSExpression)
		}
		for _, val := range cmd.EnumValues {
			walkExprLambdas(val.Initializer)
			for _, arg := range val.Arguments {
				walkExprLambdas(arg)
			}
		}
		if cmd.Function != nil {
			for _, param := range cmd.Function.Parameters {
				walkExprLambdas(param.Default)
			}
		}
		if cmd.Import != nil {
			walkExprLambdas(cmd.Import.Path)
		}
	}

	for i := range file.Commands {
		checkCommand(&file.Commands[i])
	}

	// Also check any lambda bodies indexed in fileAnalysis if not yet visited
	if fileAnalysis != nil && fileAnalysis.lambdaScopes != nil {
		for expr := range fileAnalysis.lambdaScopes {
			if expr != nil && expr.LambdaBody != nil {
				for i := range expr.LambdaBody.Commands {
					checkCommand(&expr.LambdaBody.Commands[i])
				}
			}
		}
	}

	// 2. Check function references and option expressions from precomputed references.
	if fileAnalysis != nil {
		for _, ref := range fileAnalysis.References {
			if ref == nil || seen[ref.Span] {
				continue
			}
			if strings.HasPrefix(ref.Name, "&") {
				optName := strings.TrimPrefix(ref.Name, "&")
				if strings.HasPrefix(optName, "l:") || strings.HasPrefix(optName, "g:") {
					optName = optName[2:]
				}
				if history, ok := vimdata.LookupOptionHistory(optName); ok {
					if history.VimVersion().Compare(targetVersion) > 0 {
						diags = append(diags, syntax.Diagnostic{
							Code:    "vim/E113",
							Message: fmt.Sprintf("Unknown option: %s (added in Vim %s)", optName, history.Version),
							Span:    ref.Span,
						})
						seen[ref.Span] = true
					}
				}
				continue
			}

			// Only report unknown function diagnostics for actual function calls
			if !ref.functionCallee || ref.Declaration != nil {
				continue
			}

			fnName := strings.TrimPrefix(ref.Name, "g:")
			if history, ok := vimdata.LookupFunctionHistory(fnName); ok {
				if history.VimVersion().Compare(targetVersion) > 0 {
					diags = append(diags, syntax.Diagnostic{
						Code:    "vim/E117",
						Message: fmt.Sprintf("Unknown function: %s (added in Vim %s)", ref.Name, history.Version),
						Span:    ref.Span,
					})
					seen[ref.Span] = true
				}
			}
		}
	}

	return diags
}
