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
