package analysis

import (
	"regexp"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
)

func collectStyleDiagnostics(result *FileAnalysis) {
	if result == nil || result.File == nil || len(result.File.Diagnostics) != 0 || !onlyUnusedVariableDiagnostics(result.Diagnostics) {
		return
	}
	collectStyleCommandDiagnostics(result, result.File, result.File.Commands, result.File.Blocks, false)
}

func onlyUnusedVariableDiagnostics(diagnostics []syntax.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != "vimls/unused-variable" {
			return false
		}
	}
	return true
}

// autocmdCoverage models the autocommands that earlier unconditional clears of
// one augroup region have statically removed. A definition of (event, pattern)
// is reload-safe only when every event it registers is covered: either the
// whole group was cleared, the event was cleared for every pattern, or the
// exact (event, pattern) pair was cleared or replaced before the definition.
// Pattern comparison is conservative literal equality because autocmd removal
// patterns cannot be proven equivalent statically.
type autocmdCoverage struct {
	all      bool
	events   map[string]bool
	patterns map[string]map[string]bool
}

func (c *autocmdCoverage) addClear(file *syntax.File, events []syntax.Span, pattern syntax.Span) {
	pat := strings.TrimSpace(file.Text(pattern))
	if len(events) == 0 {
		if pat == "" {
			c.all = true
		}
		return
	}
	if c.patterns == nil {
		c.patterns = make(map[string]map[string]bool)
	}
	for _, span := range events {
		name := file.Text(span)
		if name == "*" {
			if pat == "" {
				c.all = true
			} else {
				if c.patterns["*"] == nil {
					c.patterns["*"] = make(map[string]bool)
				}
				c.patterns["*"][pat] = true
			}
			continue
		}
		if pat == "" {
			if c.events == nil {
				c.events = make(map[string]bool)
			}
			c.events[name] = true
			continue
		}
		if c.patterns[name] == nil {
			c.patterns[name] = make(map[string]bool)
		}
		c.patterns[name][pat] = true
	}
}

func (c *autocmdCoverage) covers(file *syntax.File, events []syntax.Span, pattern syntax.Span) bool {
	if c.all {
		return true
	}
	pat := strings.TrimSpace(file.Text(pattern))
	for _, span := range events {
		name := file.Text(span)
		if name == "*" {
			continue
		}
		if c.events[name] || c.events["*"] {
			continue
		}
		if pat != "" && (c.patterns[name][pat] || c.patterns["*"][pat]) {
			continue
		}
		return false
	}
	return len(events) > 0
}

// augroupTracking is the reload-safety state of one open augroup region. In
// default (plugin) mode only the existence of any clear matters; in config
// mode uncovered persistent autocommands are tracked so the report can point
// to the first one as related information.
type augroupTracking struct {
	header    *syntax.Command
	defines   bool
	clears    bool
	dynamic   bool
	coverage  autocmdCoverage
	uncovered *syntax.Command
}

var dynamicAutocmdPattern = regexp.MustCompile(`\bau!?|autocmd|augroup`)

// dynamicAutocmdText reports whether source likely registers or clears
// autocommands through :execute, which the static augroup reload-safety check
// cannot reason about (kept unknown per §4.3).
func dynamicAutocmdText(source string) bool {
	return dynamicAutocmdPattern.MatchString(strings.ToLower(source))
}

func collectStyleCommandDiagnostics(result *FileAnalysis, file *syntax.File, commands []syntax.Command, blocks []syntax.Block, autocmdContext bool) {
	activeAugroup := false
	var tracking *augroupTracking
	finishAugroup := func() {
		if tracking != nil {
			if result.configFile {
				if tracking.defines && tracking.uncovered != nil && !tracking.dynamic {
					result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
						Code: "vimls/autocmd-group-not-cleared", Message: "augroup does not clear existing autocommands", Span: tracking.header.Augroup,
						Related: syntax.RelatedDiagnostic{
							Message: "autocommand is registered again every time this configuration is sourced",
							Span:    tracking.uncovered.Autocmd.Pattern,
						},
					})
				}
			} else if tracking.defines && !tracking.clears {
				appendStyleDiagnostic(result, "vimls/autocmd-group-not-cleared", "augroup does not clear existing autocommands", tracking.header.Augroup)
			}
		}
		activeAugroup = false
		tracking = nil
	}
	for index := range commands {
		command := &commands[index]
		if command.Canonical == "augroup" {
			name := file.Text(command.Augroup)
			finishAugroup()
			if name != "" && !strings.EqualFold(name, "END") && command.Bang.Start == command.Bang.End {
				activeAugroup = true
				tracking = &augroupTracking{header: command}
			}
		}
		if command.Canonical == "normal" && command.Bang.Start == command.Bang.End && command.Argument.Start < command.Argument.End {
			appendStyleDiagnostic(result, "vimls/normal-without-bang", ":normal may invoke user-defined mappings; prefer :normal!", command.Name)
		}
		if command.Canonical == "function" && command.Function != nil && !hasWord(file.Text(command.Argument), "abort") {
			appendStyleDiagnostic(result, "vimls/function-without-abort", "function does not use abort", command.Name)
		}
		if command.Canonical == "catch" && strings.TrimSpace(file.Text(command.Argument)) != "" && !hasVimErrorCode(file.Text(command.Argument)) {
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
		// In user configuration files a top-level :set establishes global
		// defaults and is not flagged; :setlocal is suggested only inside an
		// autocmd body that targets buffers or windows (e.g. FileType).
		if (autocmdContext || !result.configFile) && command.Canonical == "set" && command.Set != nil {
			for _, option := range command.Set.Options {
				if metadata, ok := vimdata.LookupOption(file.Text(option.Name)); ok && metadata.Scope != vimdata.OptionGlobal {
					appendStyleDiagnostic(result, "vimls/set-vs-setlocal", ":set may modify a global option; consider :setlocal", option.Name)
				}
			}
		}
		if result.configFile && activeAugroup && tracking != nil && command.Canonical == "execute" && command.Argument.Start < command.Argument.End && dynamicAutocmdText(file.Text(command.Argument)) {
			tracking.dynamic = true
		}
		if command.Autocmd != nil {
			operation := command.Autocmd.Operation
			// Autocmd commands may name their group explicitly; only the
			// effective group of the current region is tracked here. Group
			// names are case sensitive in Vim.
			inRegionGroup := true
			if activeAugroup && tracking != nil {
				if explicit := strings.TrimSpace(file.Text(command.Autocmd.Group)); explicit != "" && explicit != strings.TrimSpace(file.Text(tracking.header.Augroup)) {
					inRegionGroup = false
				}
			}
			if operation == syntax.AutocmdDefine && command.Autocmd.Pattern.End < command.Argument.End {
				if command.Autocmd.Group.Start == command.Autocmd.Group.End && !activeAugroup {
					appendStyleDiagnostic(result, "vimls/autocmd-outside-augroup", "autocommand is not contained in an augroup", command.Name)
				}
				body := file.Text(syntax.Span{Start: command.Autocmd.Pattern.End, End: command.Argument.End})
				if commandComplexity(body) > 1 {
					appendStyleDiagnostic(result, "vimls/complex-autocmd", "complex autocommand body; consider delegating to a function", command.Autocmd.Pattern)
				}
				if activeAugroup && tracking != nil && inRegionGroup {
					tracking.defines = true
					if result.configFile && tracking.uncovered == nil && !tracking.coverage.covers(file, command.Autocmd.Events, command.Autocmd.Pattern) {
						tracking.uncovered = command
					}
				}
			} else if activeAugroup && tracking != nil && inRegionGroup {
				switch operation {
				case syntax.AutocmdClear:
					if result.configFile {
						if unconditionalAt(commands, blocks, index) {
							tracking.coverage.addClear(file, command.Autocmd.Events, command.Autocmd.Pattern)
						}
					} else {
						tracking.clears = true
					}
				case syntax.AutocmdReplace:
					if result.configFile {
						// :autocmd! {event} {pattern} {cmd} replaces any prior
						// registration of the same (event, pattern) before
						// defining it, so it never accumulates duplicates and
						// also covers later definitions with the same pair.
						tracking.defines = true
						if unconditionalAt(commands, blocks, index) {
							tracking.coverage.addClear(file, command.Autocmd.Events, command.Autocmd.Pattern)
						}
					}
				}
			}
		}
		if command.UserCommand != nil && command.UserCommand.Body.Start < command.UserCommand.Body.End && commandComplexity(file.Text(command.UserCommand.Body)) > 1 {
			appendStyleDiagnostic(result, "vimls/complex-command", "complex user command body; consider delegating to a function", command.UserCommand.Body)
		}
		collectDeclarationStyleDiagnostics(result, file, command, blocks)
		collectExpressionStyleDiagnostics(result, file, command)
		if command.Embedded != nil {
			collectStyleCommandDiagnostics(result, file, command.Embedded.Commands, command.Embedded.Blocks, autocmdContext || command.Autocmd != nil)
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
	// User configuration files own their g: values: an unconditional top-level
	// g: assignment is user configuration, not plugin-internal state, so the
	// plugin-oriented overwrite/state heuristics are disabled in this mode.
	if result.configFile || command.Block >= 0 || !strings.HasPrefix(name, "g:") || command.Declaration.Assignment.Start == command.Declaration.Assignment.End {
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
	// A recursive mapping may be intentional composition of existing user
	// mappings in a configuration file, so its default level is a hint there.
	if mapping.Kind == syntax.MappingDefine && !strings.Contains(strings.ToLower(rhs), "<plug>") && !mapping.Script {
		if result.configFile {
			severity := syntax.DiagnosticHint
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vimls/recursive-map", Message: "recursive mapping may expand user mappings; prefer a noremap command",
				Span: command.Name, Severity: &severity,
			})
		} else {
			appendStyleDiagnostic(result, "vimls/recursive-map", "recursive mapping may expand user mappings; prefer a noremap command", command.Name)
		}
	}
	// Direct <Leader> mappings and <unique> expectations are normal for user
	// configuration files; only plugin-oriented mappings are checked here.
	if !result.configFile {
		if strings.Contains(strings.ToLower(lhs), "<leader>") && !strings.Contains(strings.ToLower(rhs), "<plug>") {
			appendStyleDiagnostic(result, "vimls/direct-user-keymap", "plugin-defined <leader> mapping reduces configurability; consider exposing <Plug>", mapping.LHS)
		}
		if !mapping.Unique {
			appendStyleDiagnostic(result, "vimls/mapping-without-unique", "mapping may overwrite an existing mapping; consider <unique>", mapping.LHS)
		}
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
