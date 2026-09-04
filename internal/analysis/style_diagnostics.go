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
		if diagnostic.Severity != nil && *diagnostic.Severity == syntax.DiagnosticHint {
			continue
		}
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

func autocmdEventNames(file *syntax.File, events []syntax.Span) []string {
	names := make([]string, 0, len(events))
	known := vimdata.AutocmdEvents()
	for _, event := range events {
		name := strings.TrimSpace(file.Text(event))
		if name != "" {
			canonical := name
			for _, candidate := range known {
				if !strings.EqualFold(name, candidate.Name) {
					continue
				}
				if candidate.AliasOf != "" {
					canonical = candidate.AliasOf
				} else {
					canonical = candidate.Name
				}
				break
			}
			names = append(names, strings.ToLower(canonical))
		}
	}
	return names
}

func autocmdPatterns(file *syntax.File, pattern syntax.Span) []string {
	text := strings.TrimSpace(file.Text(pattern))
	if text == "" {
		return nil
	}
	patterns := make([]string, 0, strings.Count(text, ",")+1)
	start, braces := 0, 0
	for index := range len(text) {
		switch text[index] {
		case '{':
			braces++
		case '}':
			braces--
		case ',':
			// Match Vim's v9.2.1015 autocmd.c separator rule: a comma
			// belongs to the pattern when it is inside braces or its
			// immediately preceding byte is a backslash.
			if braces == 0 && (index == 0 || text[index-1] != '\\') {
				if item := strings.TrimSpace(text[start:index]); item != "" {
					patterns = append(patterns, item)
				}
				start = index + 1
			}
		}
	}
	if item := strings.TrimSpace(text[start:]); item != "" {
		patterns = append(patterns, item)
	}
	return patterns
}

func (c *autocmdCoverage) addClear(file *syntax.File, events []syntax.Span, pattern syntax.Span) {
	patterns := autocmdPatterns(file, pattern)
	if len(events) == 0 {
		if len(patterns) == 0 {
			c.all = true
		}
		return
	}
	if c.patterns == nil {
		c.patterns = make(map[string]map[string]bool)
	}
	for _, name := range autocmdEventNames(file, events) {
		if name == "*" {
			if len(patterns) == 0 {
				c.all = true
			} else {
				if c.patterns["*"] == nil {
					c.patterns["*"] = make(map[string]bool)
				}
				for _, pattern := range patterns {
					c.patterns["*"][pattern] = true
				}
			}
			continue
		}
		if len(patterns) == 0 {
			if c.events == nil {
				c.events = make(map[string]bool)
			}
			c.events[name] = true
			continue
		}
		if c.patterns[name] == nil {
			c.patterns[name] = make(map[string]bool)
		}
		for _, pattern := range patterns {
			c.patterns[name][pattern] = true
		}
	}
}

func (c *autocmdCoverage) covers(event, pattern string) bool {
	if c.all {
		return true
	}
	if event == "*" {
		return pattern != "" && c.patterns["*"][pattern]
	}
	if c.events[event] || c.events["*"] {
		return true
	}
	return pattern != "" && (c.patterns[event][pattern] || c.patterns["*"][pattern])
}

// augroupTracking is the reload-safety state of one effective augroup. In
// default (plugin) mode only the existence of any clear matters; in config
// mode uncovered persistent autocommands are tracked so the report can point
// to the first one as related information. Explicit group operands outside an
// :augroup region contribute to the same state.
type augroupTracking struct {
	span      syntax.Span
	defines   bool
	clears    bool
	coverage  autocmdCoverage
	uncovered []uncoveredAutocmd
}

type uncoveredAutocmd struct {
	command *syntax.Command
	event   string
	pattern string
}

var dynamicAutocmdPattern = regexp.MustCompile(`(?i)(^|[^[:alnum:]_])(?:au|aut(?:o(?:c(?:m(?:d)?)?)?)?|aug(?:r(?:o(?:u(?:p)?)?)?)?)!?([^[:alnum:]_]|$)`)

// dynamicAutocmdText reports whether source likely registers or clears
// autocommands through :execute, which the static augroup reload-safety check
// cannot reason about (kept unknown per §4.3).
func dynamicAutocmdText(source string) bool {
	return dynamicAutocmdPattern.MatchString(source)
}

func autocmdTargetsLocal(file *syntax.File, events []syntax.Span) bool {
	names := autocmdEventNames(file, events)
	if len(names) == 0 {
		return false
	}
	for _, event := range names {
		switch event {
		case "bufadd", "bufcreate", "bufdelete", "bufenter", "buffilepost", "buffilepre", "bufhidden", "bufleave", "bufnew", "bufnewfile", "bufread", "bufreadcmd", "bufreadpost", "bufreadpre", "bufunload", "bufwinenter", "bufwinleave", "bufwipeout", "bufwrite", "bufwritecmd", "bufwritepost", "bufwritepre", "encodingchanged", "fileappendcmd", "fileappendpost", "fileappendpre", "filechangedro", "filechangedshell", "filechangedshellpost", "fileencoding", "filereadcmd", "filereadpost", "filereadpre", "filetype", "filewritecmd", "filewritepost", "filewritepre", "filterreadpost", "filterreadpre", "filterwritepost", "filterwritepre", "syntax", "swapexists", "winclosed", "winenter", "winleave", "winnew", "winnewpre", "winresized", "winscrolled":
			continue
		}
		return false
	}
	return true
}

func collectStyleCommandDiagnostics(result *FileAnalysis, file *syntax.File, commands []syntax.Command, blocks []syntax.Block, autocmdContext bool) {
	groups := make(map[string]*augroupTracking)
	activeGroup := ""
	dynamicAutocmd := false
	trackingFor := func(name string, span syntax.Span) *augroupTracking {
		tracking := groups[name]
		if tracking == nil {
			tracking = &augroupTracking{span: span}
			groups[name] = tracking
		} else if tracking.span.Start == tracking.span.End && span.Start < span.End {
			tracking.span = span
		}
		return tracking
	}
	retireDefinitions := func(tracking *augroupTracking, events []syntax.Span, pattern syntax.Span) {
		clear := autocmdCoverage{}
		clear.addClear(file, events, pattern)
		remaining := tracking.uncovered[:0]
		for _, definition := range tracking.uncovered {
			if !clear.covers(definition.event, definition.pattern) {
				remaining = append(remaining, definition)
			}
		}
		tracking.uncovered = remaining
	}
	noteDefinition := func(tracking *augroupTracking, command *syntax.Command) {
		tracking.defines = true
		for _, event := range autocmdEventNames(file, command.Autocmd.Events) {
			for _, pattern := range autocmdPatterns(file, command.Autocmd.Pattern) {
				if !tracking.coverage.covers(event, pattern) {
					tracking.uncovered = append(tracking.uncovered, uncoveredAutocmd{command: command, event: event, pattern: pattern})
				}
			}
		}
	}
	for index := range commands {
		command := &commands[index]
		if command.Canonical == "augroup" {
			name := file.Text(command.Augroup)
			if name != "" && !strings.EqualFold(name, "END") && command.Bang.Start == command.Bang.End {
				activeGroup = name
				trackingFor(name, command.Augroup)
			} else {
				activeGroup = ""
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
		if result.configFile && command.Canonical == "execute" && command.Argument.Start < command.Argument.End && dynamicAutocmdText(file.Text(command.Argument)) {
			// Dynamic command text may target any existing group, including one
			// named explicitly outside the current :augroup region.
			dynamicAutocmd = true
		}
		if command.Autocmd != nil {
			operation := command.Autocmd.Operation
			// Groups are case sensitive in Vim. An explicit group names that
			// group even while a different :augroup is active.
			group := strings.TrimSpace(file.Text(command.Autocmd.Group))
			if group == "" {
				group = activeGroup
			}
			if operation == syntax.AutocmdDefine && command.Autocmd.Pattern.End < command.Argument.End {
				if group == "" {
					appendStyleDiagnostic(result, "vimls/autocmd-outside-augroup", "autocommand is not contained in an augroup", command.Name)
				}
				body := file.Text(syntax.Span{Start: command.Autocmd.Pattern.End, End: command.Argument.End})
				if commandComplexity(body) > 1 {
					appendStyleDiagnostic(result, "vimls/complex-autocmd", "complex autocommand body; consider delegating to a function", command.Autocmd.Pattern)
				}
				if group != "" {
					tracking := trackingFor(group, command.Autocmd.Group)
					if result.configFile {
						noteDefinition(tracking, command)
					} else {
						tracking.defines = true
					}
				}
			} else if group != "" {
				tracking := trackingFor(group, command.Autocmd.Group)
				switch operation {
				case syntax.AutocmdClear:
					if result.configFile {
						if unconditionalAt(commands, blocks, index) {
							tracking.coverage.addClear(file, command.Autocmd.Events, command.Autocmd.Pattern)
							retireDefinitions(tracking, command.Autocmd.Events, command.Autocmd.Pattern)
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
							retireDefinitions(tracking, command.Autocmd.Events, command.Autocmd.Pattern)
						}
					} else {
						tracking.defines = true
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
			embeddedAutocmdContext := autocmdContext
			if command.Autocmd != nil {
				embeddedAutocmdContext = autocmdTargetsLocal(file, command.Autocmd.Events)
			}
			collectStyleCommandDiagnostics(result, file, command.Embedded.Commands, command.Embedded.Blocks, embeddedAutocmdContext)
		}
	}
	for _, tracking := range groups {
		if result.configFile {
			if tracking.defines && len(tracking.uncovered) != 0 && !dynamicAutocmd {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code: "vimls/autocmd-group-not-cleared", Message: "augroup does not clear existing autocommands", Span: tracking.span,
					Related: syntax.RelatedDiagnostic{
						Message: "autocommand is registered again every time this configuration is sourced",
						Span:    tracking.uncovered[0].command.Autocmd.Pattern,
					},
				})
			}
		} else if tracking.defines && !tracking.clears {
			appendStyleDiagnostic(result, "vimls/autocmd-group-not-cleared", "augroup does not clear existing autocommands", tracking.span)
		}
	}
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
	// User configuration files own their g: values, so plugin-oriented
	// overwrite/state heuristics are disabled in this mode.
	target := command.Declaration.Target
	if result.configFile || target == nil || target.Kind != syntax.ExpressionIdentifier ||
		!strings.HasPrefix(name, "g:") || command.Declaration.Assignment.Start == command.Declaration.Assignment.End {
		return
	}
	initializer := command.Declaration.Initializer
	if file.Text(command.Declaration.Assignment) == "=" &&
		initializer != nil && initializer.Kind == syntax.ExpressionCall && (len(initializer.Children) == 3 || len(initializer.Children) == 4) &&
		initializer.Children[0].Kind == syntax.ExpressionIdentifier && initializer.Children[0].Value == "get" &&
		initializer.Children[1].Kind == syntax.ExpressionIdentifier && initializer.Children[1].Value == "g:" &&
		initializer.Children[2].Kind == syntax.ExpressionString && simpleVimStringLiteral(initializer.Children[2].Value) == name[2:] {
		return
	}
	lower := strings.ToLower(name[2:])
	if command.Block < 0 && (strings.Contains(lower, "internal") || strings.Contains(lower, "state") || strings.Contains(lower, "cache") || strings.Contains(lower, "queue") || strings.Contains(lower, "counter")) {
		appendStyleDiagnostic(result, "vimls/global-internal-state", "global variable appears to be plugin-internal state; consider script-local state", command.Declaration.Name)
		return
	}
	if command.Block < 0 && (strings.Contains(lower, "option") || strings.Contains(lower, "timeout") || strings.Contains(lower, "enable") || strings.Contains(lower, "disable") || strings.Contains(lower, "path") || strings.Contains(lower, "config")) {
		appendStyleDiagnostic(result, "vimls/configuration-overwrite", "unconditional configuration assignment may overwrite a user value", command.Declaration.Name)
		return
	}
	if strings.HasPrefix(lower, "loaded_") {
		return
	}
	appendStyleDiagnostic(result, "vimls/global-internal-state", "global assignment may be leftover debugging; prefer script-local or local state", command.Declaration.Name)
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
	if mapping.Kind == syntax.MappingDefine && (!result.configFile || !mapping.Abbreviation) && !strings.Contains(strings.ToLower(rhs), "<plug>") && !mapping.Script {
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
	for field := range strings.FieldsSeq(source) {
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
