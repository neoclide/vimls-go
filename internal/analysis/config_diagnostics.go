package analysis

import (
	"regexp"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
)

// collectConfigLeaderOrderDiagnostics reports vimls/config-mapleader-order for
// user configuration files (§5.2). g:mapleader and g:maplocalleader are
// expanded when a mapping is *defined*, so assigning the leader after a
// mapping that already used <Leader>/<LocalLeader> has no effect on it. Only
// a provable straight-line order (unconditional top-level commands in source
// order) and a statically literal later assignment are reported; dynamic
// assignments, conditional blocks, and function bodies stay unknown.
func collectConfigLeaderOrderDiagnostics(result *FileAnalysis) {
	file := result.File
	if file == nil || len(file.Diagnostics) != 0 {
		return
	}
	leaderState := map[string]*leaderOrderState{
		"mapleader":      {},
		"maplocalleader": {},
	}
	for index := range file.Commands {
		if !unconditionalAt(file.Commands, file.Blocks, index) {
			continue
		}
		command := &file.Commands[index]
		if command.Mapping != nil && command.Mapping.RHS.Start > command.Mapping.LHS.Start {
			text := strings.ToLower(file.Text(syntax.Span{Start: command.Mapping.LHS.Start, End: command.Mapping.RHS.End}))
			if strings.Contains(text, "<leader>") {
				leaderState["mapleader"].noteMapping(command)
			}
			if strings.Contains(text, "<localleader>") {
				leaderState["maplocalleader"].noteMapping(command)
			}
		}
		if command.Declaration != nil && command.Declaration.Name.Start < command.Declaration.Name.End {
			rawName := file.Text(command.Declaration.Name)
			name := strings.TrimPrefix(strings.ToLower(rawName), "g:")
			if state, ok := leaderState[name]; ok {
				state.noteAssignment(result, rawName, command.Declaration.Name, command.Declaration.Initializer)
			}
		} else if len(command.Expressions) == 1 && command.Expressions[0].Kind == syntax.ExpressionAssignment && len(command.Expressions[0].Children) == 2 && file.Text(command.Expressions[0].Operator) == "=" {
			assignment := command.Expressions[0]
			target := assignment.Children[0]
			if target != nil && target.Kind == syntax.ExpressionIdentifier {
				rawName := file.Text(target.Span)
				name := strings.TrimPrefix(strings.ToLower(rawName), "g:")
				if state, ok := leaderState[name]; ok {
					state.noteAssignment(result, rawName, target.Span, assignment.Children[1])
				}
			}
		}
	}
}

// leaderOrderState tracks the straight-line history of one leader variable.
type leaderOrderState struct {
	assigned bool
	mappings []*syntax.Command
	reported map[*syntax.Command]bool
}

func (state *leaderOrderState) noteMapping(command *syntax.Command) {
	if state.assigned {
		return
	}
	state.mappings = append(state.mappings, command)
}

func (state *leaderOrderState) noteAssignment(result *FileAnalysis, targetName string, targetSpan syntax.Span, initializer *syntax.Expression) {
	if state.assigned {
		return
	}
	// A statically literal assignment makes the ordering problem visible: the
	// mapping that ran earlier expanded the old (or default) leader. A dynamic
	// assignment is kept unknown (§5.2) and resets the pending mappings.
	static := initializer != nil && initializer.Kind == syntax.ExpressionString
	if static {
		for _, mapping := range state.mappings {
			if state.reported == nil {
				state.reported = make(map[*syntax.Command]bool)
			}
			if state.reported[mapping] {
				continue
			}
			state.reported[mapping] = true
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
}

// configMappingRecord tracks one mapping key that is statically active while a
// configuration file is sourced (§5.1 duplicate-mapping).
type configMappingRecord struct {
	scope        string
	abbreviation bool
	modes        syntax.MappingMode
	definitions  map[syntax.MappingMode]*syntax.Command
}

func dynamicMappingMutationText(source string) bool {
	for word := range strings.FieldsSeq(source) {
		word = strings.Trim(strings.ToLower(word), "'\"|;")
		command, ok := vimdata.Lookup(word)
		if !ok {
			continue
		}
		if strings.HasSuffix(command.Name, "unmap") || strings.HasSuffix(command.Name, "unabbrev") || command.Name == "unabbreviate" || strings.HasSuffix(command.Name, "mapclear") || strings.HasSuffix(command.Name, "abclear") {
			return true
		}
	}
	return false
}

func configMappingScope(mapping *syntax.Mapping) string {
	if mapping.Buffer {
		return "buffer"
	}
	return "global"
}

// configMappingRecordKey identifies a mapping by its category (mapping vs
// abbreviation), its local/global scope, and its literal LHS spelling.
func configMappingRecordKey(file *syntax.File, mapping *syntax.Mapping) string {
	category := "map"
	if mapping.Abbreviation {
		category = "abbreviation"
	}
	return category + "\x00" + configMappingScope(mapping) + "\x00" + file.Text(mapping.LHS)
}

func (record *configMappingRecord) clearModes(modes syntax.MappingMode) {
	record.modes &^= modes
	for mode := syntax.MappingModeNormal; mode <= syntax.MappingModeLangmap; mode <<= 1 {
		if modes&mode != 0 {
			delete(record.definitions, mode)
		}
	}
}

func (record *configMappingRecord) noteDefinition(command *syntax.Command, modes syntax.MappingMode) {
	if record.definitions == nil {
		record.definitions = make(map[syntax.MappingMode]*syntax.Command)
	}
	for mode := syntax.MappingModeNormal; mode <= syntax.MappingModeLangmap; mode <<= 1 {
		if modes&mode != 0 {
			record.definitions[mode] = command
		}
	}
	record.modes |= modes
}

func (record *configMappingRecord) overlappingDefinition(modes syntax.MappingMode) *syntax.Command {
	var latest *syntax.Command
	for mode := syntax.MappingModeNormal; mode <= syntax.MappingModeLangmap; mode <<= 1 {
		definition := record.definitions[mode]
		if modes&mode != 0 && definition != nil && (latest == nil || definition.Span.Start > latest.Span.Start) {
			latest = definition
		}
	}
	return latest
}

// collectConfigDuplicateMappingDiagnostics reports vimls/duplicate-mapping for
// configuration files (§5.1): when a later mapping definition overwrites an
// earlier definition of the same key (same LHS spelling, same local/global
// scope, same category) in overlapping modes on the same provable execution
// path. :unmap and :mapclear terminate earlier definitions. Mutually exclusive
// or dynamic contexts are not tracked, so no conflict is invented.
func collectConfigDuplicateMappingDiagnostics(result *FileAnalysis) {
	file := result.File
	if file == nil || len(file.Diagnostics) != 0 {
		return
	}
	records := make(map[string]*configMappingRecord)
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Canonical == "execute" && dynamicMappingMutationText(file.Text(command.Argument)) {
			// The command text is evaluated at runtime, so a mapping mutation in
			// it may have changed any tracked mapping. Forget the state rather
			// than reporting a later overwrite as certain.
			clear(records)
			continue
		}
		mapping := command.Mapping
		if mapping == nil {
			continue
		}
		if mapping.Kind == syntax.MappingClear {
			// A conditional or dynamic clear makes prior state unknown too: do
			// not retain it and later claim an overwrite is certain. :mapclear
			// and :abclear have distinct categories.
			scope := configMappingScope(mapping)
			for existingKey, existing := range records {
				if existing.scope == scope && existing.abbreviation == mapping.Abbreviation && existing.modes&mapping.Mode != 0 {
					existing.clearModes(mapping.Mode)
					if existing.modes == 0 {
						delete(records, existingKey)
					}
				}
			}
			continue
		}
		if mapping.Query || mapping.LHS.Start == mapping.LHS.End {
			continue
		}
		key := configMappingRecordKey(file, mapping)
		if mapping.Kind == syntax.MappingUnmap {
			// As above, a mutation that is not statically guaranteed to run
			// invalidates certainty rather than preserving a possible stale map.
			if record := records[key]; record != nil {
				record.clearModes(mapping.Mode)
				if record.modes == 0 {
					delete(records, key)
				}
			}
			continue
		}
		if !unconditionalAt(file.Commands, file.Blocks, index) {
			continue
		}
		switch mapping.Kind {
		case syntax.MappingDefine, syntax.MappingNoremap:
			record := records[key]
			if record != nil && record.modes&mapping.Mode != 0 {
				earlier := record.overlappingDefinition(record.modes & mapping.Mode)
				if earlier == nil {
					continue
				}
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code:    "vimls/duplicate-mapping",
					Message: "mapping for " + file.Text(mapping.LHS) + " is defined again; the later definition overwrites the earlier one",
					Span:    mapping.LHS,
					Related: syntax.RelatedDiagnostic{
						Message: "earlier definition of " + file.Text(mapping.LHS),
						Span:    earlier.Mapping.LHS,
					},
				})
			}
			if record == nil {
				record = &configMappingRecord{scope: configMappingScope(mapping), abbreviation: mapping.Abbreviation}
				records[key] = record
			}
			record.noteDefinition(command, mapping.Mode)
		}
	}
}

// loadedGuardPattern matches an if condition that is exactly one exists()
// call testing a g:loaded_* variable, e.g. exists('g:loaded_my_vimrc').
var loadedGuardPattern = regexp.MustCompile(`(?is)^\s*exists\s*\(\s*['"]g:loaded_([a-z0-9_]+)['"]\s*\)\s*$`)

// collectConfigLoadedGuardDiagnostics reports vimls/config-loaded-guard (§4.4)
// for configuration files whose top-level plugin-style loaded guard prevents a
// later :source from reaching the remainder of the file.
func collectConfigLoadedGuardDiagnostics(result *FileAnalysis) {
	file := result.File
	if file == nil || len(file.Diagnostics) != 0 {
		return
	}
	// vim9script noclear is an explicit single-load design and is exempt.
	vim9 := file.Dialect == syntax.Vim9
	if vim9 && hasVim9NoClear(file) {
		return
	}
	var markers map[string]bool
	if !vim9 {
		markers = configLoadedMarkers(result)
	}
	for index := range file.Commands {
		if !rootScopedCommand(file.Commands, file.Blocks, index) {
			continue
		}
		command := &file.Commands[index]
		if command.Canonical != "if" {
			continue
		}
		guardName, ok := loadedGuardVariable(file.Text(command.Argument))
		if !ok {
			continue
		}
		if markers != nil {
			if _, marked := markers[guardName]; !marked {
				continue
			}
		}
		if !guardFinishesBlock(command, file.Commands, file.Blocks, index) {
			continue
		}
		message := "a loaded guard for " + guardName + " skips the rest of the file on a later :source; edits below may not take effect"
		if vim9 {
			message = "a loaded guard for " + guardName + " skips the rest of the file; Vim9 reload already cleared script-local items, so the file may stay half-initialized"
		}
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
			Code: "vimls/config-loaded-guard", Message: message,
			Span: syntax.Span{Start: command.Argument.Start, End: command.Argument.End},
		})
	}
}

// hasVim9NoClear reports whether a Vim9 root file starts with
// "vim9script noclear", the explicit single-load design.
func hasVim9NoClear(file *syntax.File) bool {
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Canonical != "vim9script" {
			continue
		}
		text := strings.ToLower(file.Text(command.Argument))
		return strings.Contains(text, "noclear")
	}
	return false
}

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

// guardFinishesBlock reports whether the then-part of the if at index directly
// contains a :finish before any elseif/else at the same level.
func guardFinishesBlock(guard *syntax.Command, commands []syntax.Command, blocks []syntax.Block, index int) bool {
	blockIndex := guard.Block
	if blockIndex < 0 || blockIndex >= len(blocks) {
		return false
	}
	for next := index + 1; next < len(commands); next++ {
		if commands[next].Block != blockIndex {
			continue
		}
		switch commands[next].Canonical {
		case "finish":
			return true
		case "elseif", "else", "endif":
			return false
		}
	}
	return false
}

// configLoadedMarkers collects the g:loaded_* variables assigned at the root
// level of a configuration file.
func configLoadedMarkers(result *FileAnalysis) map[string]bool {
	file := result.File
	markers := make(map[string]bool)
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Declaration == nil || !rootScopedCommand(file.Commands, file.Blocks, index) || command.Declaration.Assignment.Start == command.Declaration.Assignment.End || command.Declaration.Initializer == nil || command.Declaration.Initializer.Kind == syntax.ExpressionMissing {
			continue
		}
		name := file.Text(command.Declaration.Name)
		if strings.HasPrefix(name, "g:loaded_") {
			markers[name] = true
		}
	}
	return markers
}
