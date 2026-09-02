package analysis

import (
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
)

func collectStyleDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil || len(result.File.Diagnostics) != 0 || !onlyUnusedVariableDiagnostics(result.Diagnostics) {
		return
	}
	collectStyleCommandDiagnostics(result, result.File, result.File.Commands, result.File.Blocks)
}

func onlyUnusedVariableDiagnostics(diagnostics []syntax.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != "vimls/unused-variable" {
			return false
		}
	}
	return true
}

func collectStyleCommandDiagnostics(result *FileAnalysis, file *syntax.File, commands []syntax.Command, blocks []syntax.Block) {
	activeAugroup := false
	var augroupHeader *syntax.Command
	definesAutocmd := false
	clearsAutocmd := false
	finishAugroup := func() {
		if activeAugroup && definesAutocmd && !clearsAutocmd {
			appendStyleDiagnostic(result, "vimls/autocmd-group-not-cleared", "augroup does not clear existing autocommands", augroupHeader.Augroup)
		}
		activeAugroup = false
		definesAutocmd = false
		clearsAutocmd = false
	}
	for index := range commands {
		command := &commands[index]
		if command.Canonical == "augroup" {
			name := file.Text(command.Augroup)
			finishAugroup()
			if name != "" && !strings.EqualFold(name, "END") && command.Bang.Start == command.Bang.End {
				activeAugroup = true
				augroupHeader = command
			}
		}
		if command.Canonical == "normal" && command.Bang.Start == command.Bang.End && command.Argument.Start < command.Argument.End {
			appendStyleDiagnostic(result, "vimls/normal-without-bang", ":normal may invoke user-defined mappings; prefer :normal!", command.Name)
		}
		if command.Canonical == "function" && command.Function != nil && !hasWord(file.Text(command.Argument), "abort") {
			appendStyleDiagnostic(result, "vimls/function-without-abort", "function does not use abort", command.Name)
		}
		if command.Canonical == "catch" && !hasVimErrorCode(file.Text(command.Argument)) {
			appendStyleDiagnostic(result, "vimls/catch-error-message", "catching human-readable error text is fragile; prefer a Vim error code", command.Argument)
		}
		if command.Canonical == "echoerr" {
			appendStyleDiagnostic(result, "vimls/echoerr", "consider throw for programmatic errors or echomsg for user-facing messages", command.Name)
		}
		if strings.HasSuffix(command.Canonical, "match") && command.Canonical != "matchadd" {
			appendStyleDiagnostic(result, "vimls/match-command", ":match uses shared match slots; prefer matchadd()", command.Name)
		}
		if command.Mapping != nil {
			collectMappingStyleDiagnostics(result, file, command)
		}
		if command.Canonical == "set" && command.Set != nil {
			for _, option := range command.Set.Options {
				if metadata, ok := vimdata.LookupOption(file.Text(option.Name)); ok && metadata.Scope != vimdata.OptionGlobal {
					appendStyleDiagnostic(result, "vimls/set-vs-setlocal", ":set may modify a global option; consider :setlocal", option.Name)
				}
			}
		}
		if command.Autocmd != nil && command.Autocmd.Operation == syntax.AutocmdDefine && command.Autocmd.Pattern.End < command.Argument.End {
			if command.Autocmd.Group.Start == command.Autocmd.Group.End && !activeAugroup {
				appendStyleDiagnostic(result, "vimls/autocmd-outside-augroup", "autocommand is not contained in an augroup", command.Name)
			}
			if activeAugroup {
				definesAutocmd = true
			}
			body := file.Text(syntax.Span{Start: command.Autocmd.Pattern.End, End: command.Argument.End})
			if commandComplexity(body) > 1 {
				appendStyleDiagnostic(result, "vimls/complex-autocmd", "complex autocommand body; consider delegating to a function", command.Autocmd.Pattern)
			}
		}
		if command.Autocmd != nil && command.Autocmd.Operation == syntax.AutocmdClear && activeAugroup {
			clearsAutocmd = true
		}
		if command.UserCommand != nil && command.UserCommand.Body.Start < command.UserCommand.Body.End && commandComplexity(file.Text(command.UserCommand.Body)) > 1 {
			appendStyleDiagnostic(result, "vimls/complex-command", "complex user command body; consider delegating to a function", command.UserCommand.Body)
		}
		collectDeclarationStyleDiagnostics(result, file, command, blocks)
		collectExpressionStyleDiagnostics(result, file, command)
		if command.Embedded != nil {
			collectStyleCommandDiagnostics(result, file, command.Embedded.Commands, command.Embedded.Blocks)
		}
	}
	finishAugroup()
}

func commandInsideStyleBlock(command *syntax.Command, blocks []syntax.Block, kind syntax.BlockKind) bool {
	for blockIndex := command.Block; blockIndex >= 0 && blockIndex < len(blocks); blockIndex = blocks[blockIndex].Parent {
		if blocks[blockIndex].Kind == kind {
			return true
		}
	}
	return false
}

func collectDeclarationStyleDiagnostics(result *FileAnalysis, file *syntax.File, command *syntax.Command, blocks []syntax.Block) {
	if command.Declaration == nil || command.Declaration.Name.Start == command.Declaration.Name.End {
		return
	}
	name := file.Text(command.Declaration.Name)
	if command.Dialect == syntax.Legacy && commandInsideStyleBlock(command, blocks, syntax.BlockFunction) && !strings.Contains(name, ":") {
		appendStyleDiagnostic(result, "vimls/explicit-local-scope", "use an explicit local scope for this variable", command.Declaration.Name)
	}
	if command.Block >= 0 || !strings.HasPrefix(name, "g:") || command.Declaration.Assignment.Start == command.Declaration.Assignment.End {
		return
	}
	lower := strings.ToLower(name[2:])
	if strings.Contains(lower, "internal") || strings.Contains(lower, "state") || strings.Contains(lower, "cache") || strings.Contains(lower, "queue") || strings.Contains(lower, "counter") {
		appendStyleDiagnostic(result, "vimls/global-internal-state", "global variable appears to be plugin-internal state; consider script-local state", command.Declaration.Name)
		return
	}
	if strings.Contains(lower, "option") || strings.Contains(lower, "timeout") || strings.Contains(lower, "enable") || strings.Contains(lower, "disable") || strings.Contains(lower, "path") || strings.Contains(lower, "config") {
		appendStyleDiagnostic(result, "vimls/configuration-overwrite", "unconditional configuration assignment may overwrite a user value", command.Declaration.Name)
	}
}

func collectExpressionStyleDiagnostics(result *FileAnalysis, file *syntax.File, command *syntax.Command) {
	if command.Declaration != nil {
		visitStyleExpression(result, file, command.Declaration.Initializer)
	}
	for _, expression := range command.Expressions {
		visitStyleExpression(result, file, expression)
	}
	if command.Mapping != nil {
		visitStyleExpression(result, file, command.Mapping.RHSExpression)
	}
}

func visitStyleExpression(result *FileAnalysis, file *syntax.File, expression *syntax.Expression) {
	if expression == nil {
		return
	}
	if expression.Kind == syntax.ExpressionBinary && len(expression.Children) == 2 {
		operator := file.Text(expression.Operator)
		left, right := expression.Children[0], expression.Children[1]
		if (operator == "==" || operator == "!=" || operator == "is" || operator == "isnot") && (left.Kind == syntax.ExpressionString || right.Kind == syntax.ExpressionString) {
			appendStyleDiagnostic(result, "vimls/implicit-string-case", "string comparison depends on 'ignorecase'; consider an explicit case operator", expression.Operator)
		}
		if (operator == "=~" || operator == "!~") && right.Kind == syntax.ExpressionString {
			appendStyleDiagnostic(result, "vimls/implicit-pattern-case", "pattern match depends on 'ignorecase'; consider an explicit case operator", expression.Operator)
			pattern := strings.Trim(file.Text(right.Span), "'\"")
			if !strings.HasPrefix(pattern, "\\v") && !strings.HasPrefix(pattern, "\\m") && !strings.HasPrefix(pattern, "\\M") && !strings.HasPrefix(pattern, "\\V") && strings.ContainsAny(pattern, ".*+?(){}") {
				appendStyleDiagnostic(result, "vimls/implicit-regex-magic", "pattern relies on Vim's magic setting; consider an explicit magic prefix", right.Span)
			}
		}
	}
	for _, child := range expression.Children {
		visitStyleExpression(result, file, child)
	}
}

func collectMappingStyleDiagnostics(result *FileAnalysis, file *syntax.File, command *syntax.Command) {
	mapping := command.Mapping
	if mapping == nil || mapping.Query || mapping.LHS.Start == mapping.LHS.End || mapping.RHS.Start == mapping.RHS.End {
		return
	}
	lhs := file.Text(mapping.LHS)
	rhs := file.Text(mapping.RHS)
	if mapping.Kind == syntax.MappingDefine && !strings.Contains(strings.ToLower(rhs), "<plug>") && !mapping.Script {
		appendStyleDiagnostic(result, "vimls/recursive-map", "recursive mapping may expand user mappings; prefer a noremap command", command.Name)
	}
	if strings.Contains(strings.ToLower(lhs), "<leader>") && !strings.Contains(strings.ToLower(rhs), "<plug>") {
		appendStyleDiagnostic(result, "vimls/direct-user-keymap", "plugin-defined <leader> mapping reduces configurability; consider exposing <Plug>", mapping.LHS)
	}
	if !mapping.Unique {
		appendStyleDiagnostic(result, "vimls/mapping-without-unique", "mapping may overwrite an existing mapping; consider <unique>", mapping.LHS)
	}
	if strings.Contains(strings.ToLower(rhs), "s:") && !strings.Contains(strings.ToLower(rhs), "<sid>") {
		appendStyleDiagnostic(result, "vimls/mapping-script-local-reference", "mapping references s: directly; use <SID> or an autoload function", mapping.RHS)
	}
}

func appendStyleDiagnostic(result *FileAnalysis, code, message string, span syntax.Span) {
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: code, Message: message, Span: span})
}

func hasWord(source, word string) bool {
	for _, field := range strings.Fields(source) {
		if field == word {
			return true
		}
	}
	return false
}

func hasVimErrorCode(source string) bool {
	for index := 0; index < len(source); index++ {
		if source[index] != 'E' || index+3 >= len(source) || source[index+1] < '0' || source[index+1] > '9' || source[index+2] < '0' || source[index+2] > '9' || source[index+3] < '0' || source[index+3] > '9' {
			continue
		}
		return true
	}
	return false
}

func commandComplexity(body string) int {
	complexity := strings.Count(body, "|")
	for _, keyword := range []string{"if ", "for ", "while ", "let ", "call "} {
		if strings.Contains(body, keyword) {
			complexity++
		}
	}
	return complexity
}
