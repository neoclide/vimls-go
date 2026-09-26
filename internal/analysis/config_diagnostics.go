package analysis

import (
	"regexp"
	"slices"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
)

// collectConfigFileDiagnostics applies the four configuration-only rules in
// one source-order walk. Later rules buffer their output to retain the previous
// rule order for diagnostics at identical spans after the final stable sort.
func collectConfigFileDiagnostics(result *FileAnalysis) {
	file := result.File
	if file == nil || len(file.Diagnostics) != 0 {
		return
	}
	var mapLeader, mapLocalLeader leaderOrderState
	records := make(map[configMappingKey]*configMappingRecord)
	markers := make(map[string]bool)
	type guardCandidate struct {
		name     string
		span     syntax.Span
		block    int
		finished bool
	}
	var guards []guardCandidate
	activeGuard := -1
	vim9 := file.Dialect == syntax.Vim9
	seenVim9script, vim9NoClear := false, false
	seenScriptencoding := false
	var duplicateDiagnostics, guardDiagnostics []syntax.Diagnostic
	var encodingSpans []syntax.Span

	for index := range file.Commands {
		if !result.analysisStep() {
			return
		}
		command := &file.Commands[index]
		unconditional := unconditionalAt(file.Commands, file.Blocks, index)

		if unconditional {
			if command.Mapping != nil && command.Mapping.RHS.Start > command.Mapping.LHS.Start {
				text := strings.ToLower(file.Text(syntax.Span{Start: command.Mapping.LHS.Start, End: command.Mapping.RHS.End}))
				if strings.Contains(text, "<leader>") {
					mapLeader.noteMapping(command)
				}
				if strings.Contains(text, "<localleader>") {
					mapLocalLeader.noteMapping(command)
				}
			}
			var rawName string
			var targetSpan syntax.Span
			var initializer *syntax.Expression
			if command.Declaration != nil && command.Declaration.Name.Start < command.Declaration.Name.End {
				rawName = file.Text(command.Declaration.Name)
				targetSpan = command.Declaration.Name
				initializer = command.Declaration.Initializer
			} else if len(command.Expressions) == 1 && command.Expressions[0].Kind == syntax.ExpressionAssignment && len(command.Expressions[0].Children) == 2 && file.Text(command.Expressions[0].Operator) == "=" {
				assignment := command.Expressions[0]
				target := assignment.Children[0]
				if target != nil && target.Kind == syntax.ExpressionIdentifier {
					rawName = file.Text(target.Span)
					targetSpan = target.Span
					initializer = assignment.Children[1]
				}
			}
			switch strings.TrimPrefix(strings.ToLower(rawName), "g:") {
			case "mapleader":
				if !mapLeader.noteAssignment(result, rawName, targetSpan, initializer) {
					return
				}
			case "maplocalleader":
				if !mapLocalLeader.noteAssignment(result, rawName, targetSpan, initializer) {
					return
				}
			}
		}

		// Even conditional and dynamic removals invalidate a previously certain
		// definition; only new definitions require an unconditional command.
		if command.Canonical == "execute" && dynamicMappingMutationText(file.Text(command.Argument)) {
			clear(records)
		} else if mapping := command.Mapping; mapping != nil {
			if mapping.Kind == syntax.MappingClear {
				for key, existing := range records {
					if !result.analysisStep() {
						return
					}
					if key.buffer == mapping.Buffer && key.abbreviation == mapping.Abbreviation && existing.modes&mapping.Mode != 0 {
						existing.clearModes(mapping.Mode)
						if existing.modes == 0 {
							delete(records, key)
						}
					}
				}
			} else if !mapping.Query && mapping.LHS.Start != mapping.LHS.End {
				key := configMappingKey{lhs: file.Text(mapping.LHS), buffer: mapping.Buffer, abbreviation: mapping.Abbreviation}
				if mapping.Kind == syntax.MappingUnmap {
					if record := records[key]; record != nil {
						record.clearModes(mapping.Mode)
						if record.modes == 0 {
							delete(records, key)
						}
					}
				} else if unconditional {
					switch mapping.Kind {
					case syntax.MappingDefine, syntax.MappingNoremap:
						record := records[key]
						if record != nil && record.modes&mapping.Mode != 0 {
							earlier := record.overlappingDefinition(record.modes & mapping.Mode)
							if earlier == nil {
								break
							}
							duplicateDiagnostics = append(duplicateDiagnostics, syntax.Diagnostic{
								Code:    "vimls/duplicate-mapping",
								Message: "mapping for " + key.lhs + " is defined again; the later definition overwrites the earlier one",
								Span:    mapping.LHS,
								Related: syntax.RelatedDiagnostic{Message: "earlier definition of " + key.lhs, Span: earlier.Mapping.LHS},
							})
						}
						if record == nil {
							record = &configMappingRecord{}
							records[key] = record
						}
						record.noteDefinition(command, mapping.Mode)
					}
				}
			}
		}

		if activeGuard >= 0 && command.Block == guards[activeGuard].block {
			switch command.Canonical {
			case "finish":
				guards[activeGuard].finished = true
				activeGuard = -1
			case "elseif", "else", "endif":
				activeGuard = -1
			}
		}
		if command.Canonical == "vim9script" && !seenVim9script {
			seenVim9script = true
			vim9NoClear = strings.Contains(strings.ToLower(file.Text(command.Argument)), "noclear")
		}
		if !vim9 && command.Declaration != nil && rootScopedCommand(file.Commands, file.Blocks, index) && command.Declaration.Assignment.Start != command.Declaration.Assignment.End && command.Declaration.Initializer != nil && command.Declaration.Initializer.Kind != syntax.ExpressionMissing {
			name := file.Text(command.Declaration.Name)
			if strings.HasPrefix(name, "g:loaded_") {
				markers[name] = true
			}
		}
		if command.Canonical == "if" && rootScopedCommand(file.Commands, file.Blocks, index) && command.Block >= 0 && command.Block < len(file.Blocks) {
			if name, ok := loadedGuardVariable(file.Text(command.Argument)); ok {
				guards = append(guards, guardCandidate{name: name, span: command.Argument, block: command.Block})
				activeGuard = len(guards) - 1
			}
		}

		if !isTopLevelConfigCommand(command, file.Blocks) {
			continue
		}
		if command.Canonical == "scriptencoding" {
			seenScriptencoding = true
			continue
		}
		if !seenScriptencoding {
			continue
		}
		if command.Set != nil {
			for _, option := range command.Set.Options {
				if !result.analysisStep() {
					return
				}
				if isEncodingOption(file.Text(option.Name)) {
					isAssignment := (option.Operator.Start < option.Operator.End && file.Text(option.Operator) != "?") || option.Prefix.Start < option.Prefix.End
					if isAssignment {
						span := option.Name
						if span.Start == span.End {
							span = option.Span
						}
						encodingSpans = append(encodingSpans, span)
					}
				}
			}
			continue
		}
		if command.Declaration != nil && command.Declaration.Name.Start < command.Declaration.Name.End {
			name := file.Text(command.Declaration.Name)
			if isEncodingOption(name) && command.Declaration.Assignment.Start < command.Declaration.Assignment.End {
				encodingSpans = append(encodingSpans, command.Declaration.Name)
				continue
			}
		}
		if len(command.Expressions) > 0 && command.Expressions[0].Kind == syntax.ExpressionAssignment && len(command.Expressions[0].Children) == 2 {
			target := command.Expressions[0].Children[0]
			if target != nil && target.Kind == syntax.ExpressionIdentifier && isEncodingOption(target.Value) {
				encodingSpans = append(encodingSpans, target.Span)
			}
		}
	}

	if !vim9 || !vim9NoClear {
		for _, guard := range guards {
			if !result.analysisStep() {
				return
			}
			if !guard.finished || (!vim9 && !markers[guard.name]) {
				continue
			}
			message := "a loaded guard for " + guard.name + " skips the rest of the file on a later :source; edits below may not take effect"
			if vim9 {
				message = "a loaded guard for " + guard.name + " skips the rest of the file; Vim9 reload already cleared script-local items, so the file may stay half-initialized"
			}
			guardDiagnostics = append(guardDiagnostics, syntax.Diagnostic{Code: "vimls/config-loaded-guard", Message: message, Span: guard.span})
		}
	}
	result.Diagnostics = slices.Grow(result.Diagnostics, len(duplicateDiagnostics)+len(guardDiagnostics)+len(encodingSpans))
	result.Diagnostics = append(result.Diagnostics, duplicateDiagnostics...)
	result.Diagnostics = append(result.Diagnostics, guardDiagnostics...)
	for _, span := range encodingSpans {
		if !result.analysisStep() {
			return
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code:    "vimls/encoding-after-scriptencoding",
			Message: "set 'encoding' before ':scriptencoding'; setting 'encoding' after ':scriptencoding' may corrupt character conversion",
			Span:    span,
		})
	}
}

// leaderOrderState tracks the straight-line history of one leader variable.
type leaderOrderState struct {
	assigned bool
	mappings []*syntax.Command
}

func (state *leaderOrderState) noteMapping(command *syntax.Command) {
	if state.assigned {
		return
	}
	state.mappings = append(state.mappings, command)
}

func (state *leaderOrderState) noteAssignment(result *FileAnalysis, targetName string, targetSpan syntax.Span, initializer *syntax.Expression) bool {
	if state.assigned {
		return true
	}
	// A statically literal assignment makes the ordering problem visible: the
	// mapping that ran earlier expanded the old (or default) leader. A dynamic
	// assignment is kept unknown (§5.2) and resets the pending mappings.
	static := initializer != nil && initializer.Kind == syntax.ExpressionString
	if static {
		for _, mapping := range state.mappings {
			if !result.analysisStep() {
				return false
			}
			leader := strings.TrimSpace(targetName)
			if !strings.HasPrefix(leader, "g:") {
				leader = "g:" + leader
			}
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code:    "vimls/config-mapleader-order",
				Message: "mapping uses " + leader + " before it is assigned; the leader key is expanded when the mapping is defined",
				Span:    mapping.Mapping.LHS,
				Related: syntax.RelatedDiagnostic{
					Message: leader + " is assigned here",
					Span:    targetSpan,
				},
			})
		}
	}
	state.mappings = nil
	state.assigned = true
	return true
}

// configMappingRecord tracks one mapping key that is statically active while a
// configuration file is sourced (§5.1 duplicate-mapping). The array positions
// correspond to the current eight syntax.MappingMode bits, Normal to Langmap.
type configMappingRecord struct {
	modes       syntax.MappingMode
	definitions [8]*syntax.Command
}

type configMappingKey struct {
	lhs          string
	buffer       bool
	abbreviation bool
}

func dynamicMappingMutationText(source string) bool {
	for word := range strings.FieldsSeq(source) {
		word = strings.Trim(strings.ToLower(word), "'\"|;")
		command, ok := vimdata.Lookup(":" + strings.TrimPrefix(word, ":"))
		if !ok {
			continue
		}
		if strings.HasSuffix(command.Name, "unmap") || strings.HasSuffix(command.Name, "unabbrev") || command.Name == "unabbreviate" || strings.HasSuffix(command.Name, "mapclear") || strings.HasSuffix(command.Name, "abclear") {
			return true
		}
	}
	return false
}

func (record *configMappingRecord) clearModes(modes syntax.MappingMode) {
	record.modes &^= modes
	for index := range record.definitions {
		mode := syntax.MappingModeNormal << index
		if modes&mode != 0 {
			record.definitions[index] = nil
		}
	}
}

func (record *configMappingRecord) noteDefinition(command *syntax.Command, modes syntax.MappingMode) {
	for index := range record.definitions {
		mode := syntax.MappingModeNormal << index
		if modes&mode != 0 {
			record.definitions[index] = command
		}
	}
	record.modes |= modes
}

func (record *configMappingRecord) overlappingDefinition(modes syntax.MappingMode) *syntax.Command {
	var latest *syntax.Command
	for index, definition := range record.definitions {
		mode := syntax.MappingModeNormal << index
		if modes&mode != 0 && definition != nil && (latest == nil || definition.Span.Start > latest.Span.Start) {
			latest = definition
		}
	}
	return latest
}

// loadedGuardPattern matches an if condition that is exactly one exists()
// call testing a g:loaded_* variable, e.g. exists('g:loaded_my_vimrc').
var loadedGuardPattern = regexp.MustCompile(`(?is)^\s*exists\s*\(\s*['"]g:loaded_([a-z0-9_]+)['"]\s*\)\s*$`)

// loadedGuardVariable extracts the g:loaded_* variable tested by a candidate
// guard condition. Reject exists() arguments that address functions ('*...'),
// commands (':...'), options ('+...'), autocommands ('##...'), or anything
// that is not a plain global variable name.
func loadedGuardVariable(argument string) (string, bool) {
	match := loadedGuardPattern.FindStringSubmatch(argument)
	if match == nil {
		return "", false
	}
	return "g:loaded_" + match[1], true
}

func isTopLevelConfigCommand(command *syntax.Command, blocks []syntax.Block) bool {
	for blockIndex := command.Block; blockIndex >= 0 && blockIndex < len(blocks); blockIndex = blocks[blockIndex].Parent {
		switch blocks[blockIndex].Kind {
		case syntax.BlockFunction, syntax.BlockDef, syntax.BlockClass, syntax.BlockInterface, syntax.BlockEnum:
			return false
		}
	}
	return true
}

func isEncodingOption(name string) bool {
	if opt, ok := vimdata.LookupOption(name); ok && opt.Name == "encoding" {
		return true
	}
	clean := strings.TrimPrefix(name, "&")
	if strings.HasPrefix(clean, "l:") || strings.HasPrefix(clean, "g:") {
		clean = clean[2:]
	}
	return clean == "encoding" || clean == "enc"
}
