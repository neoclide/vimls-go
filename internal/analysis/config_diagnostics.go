package analysis

import (
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
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
		"mapleader":      &leaderOrderState{},
		"maplocalleader": &leaderOrderState{},
	}
	for index := range file.Commands {
		if !unconditionalAt(file.Commands, file.Blocks, index) {
			continue
		}
		command := &file.Commands[index]
		if command.Mapping != nil && command.Mapping.RHS.Start > command.Mapping.LHS.Start {
			text := strings.ToLower(file.Text(syntax.Span{Start: command.Mapping.LHS.Start, End: command.Mapping.RHS.End}))
			if strings.Contains(text, "<leader>") {
				leaderState["mapleader"].noteMapping(result, command)
			}
			if strings.Contains(text, "<localleader>") {
				leaderState["maplocalleader"].noteMapping(result, command)
			}
		}
		if command.Declaration != nil && command.Declaration.Name.Start < command.Declaration.Name.End {
			name := strings.TrimPrefix(strings.ToLower(file.Text(command.Declaration.Name)), "g:")
			if state, ok := leaderState[name]; ok {
				state.noteAssignment(result, file, command)
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

func (state *leaderOrderState) noteMapping(result *FileAnalysis, command *syntax.Command) {
	if state.assigned {
		return
	}
	state.mappings = append(state.mappings, command)
}

func (state *leaderOrderState) noteAssignment(result *FileAnalysis, file *syntax.File, command *syntax.Command) {
	if state.assigned {
		return
	}
	// A statically literal assignment makes the ordering problem visible: the
	// mapping that ran earlier expanded the old (or default) leader. A dynamic
	// assignment is kept unknown (§5.2) and resets the pending mappings.
	static := command.Declaration.Initializer != nil && command.Declaration.Initializer.Kind == syntax.ExpressionString
	if static {
		for _, mapping := range state.mappings {
			if state.reported == nil {
				state.reported = make(map[*syntax.Command]bool)
			}
			if state.reported[mapping] {
				continue
			}
			state.reported[mapping] = true
			leader := strings.TrimSpace(file.Text(command.Declaration.Name))
			if !strings.HasPrefix(leader, "g:") {
				leader = "g:" + leader
			}
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code:    "vimls/config-mapleader-order",
				Message: "mapping uses " + leader + " before it is assigned; the leader key is expanded when the mapping is defined",
				Span:    mapping.Mapping.LHS,
				Related: syntax.RelatedDiagnostic{
					Message: leader + " is assigned here",
					Span:    command.Declaration.Name,
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
	scope  string
	modes  syntax.MappingMode
	latest *syntax.Command
}

func configMappingScope(file *syntax.File, mapping *syntax.Mapping) string {
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
	return category + "\x00" + configMappingScope(file, mapping) + "\x00" + file.Text(mapping.LHS)
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
		if !unconditionalAt(file.Commands, file.Blocks, index) {
			continue
		}
		command := &file.Commands[index]
		mapping := command.Mapping
		if mapping == nil {
			continue
		}
		if mapping.Kind == syntax.MappingClear {
			// :mapclear removes every mapping of its modes for its scope.
			scope := configMappingScope(file, mapping)
			for existingKey, existing := range records {
				if existing.scope == scope && existing.modes&mapping.Mode != 0 {
					existing.modes &^= mapping.Mode
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
		switch mapping.Kind {
		case syntax.MappingDefine, syntax.MappingNoremap:
			record := records[key]
			if record != nil && record.modes&mapping.Mode != 0 {
				result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
					Code:    "vimls/duplicate-mapping",
					Message: "mapping for " + file.Text(mapping.LHS) + " is defined again; the later definition overwrites the earlier one",
					Span:    mapping.LHS,
					Related: syntax.RelatedDiagnostic{
						Message: "earlier definition of " + file.Text(mapping.LHS),
						Span:    record.latest.Mapping.LHS,
					},
				})
			}
			if record == nil {
				record = &configMappingRecord{}
				records[key] = record
			}
			record.modes |= mapping.Mode
			record.scope = configMappingScope(file, mapping)
			record.latest = command
		case syntax.MappingUnmap:
			if record := records[key]; record != nil {
				record.modes &^= mapping.Mode
				if record.modes == 0 {
					delete(records, key)
				}
			}
		}
	}
}
