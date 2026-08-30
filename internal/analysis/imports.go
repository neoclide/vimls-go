package analysis

import (
	"sort"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// ImportLoad describes one statically decoded import. Runtime is true for an
// autoload lookup through 'runtimepath'; relative and absolute autoload
// failures use Vim's file-open diagnostics instead of E1053.
type ImportLoad struct {
	Span      syntax.Span
	Path      string
	Self      bool
	Duplicate bool
	Missing   bool
	Autoload  bool
	Runtime   bool
}

// ImportMember describes one member use whose import target and exported
// symbol inventory were captured from the same workspace graph revision.
type ImportMember struct {
	Span        syntax.Span
	Name        string
	TargetKnown bool
	Exists      bool
	Exported    bool
}

// AnalyzeImports returns conservative cross-file Vim9 import diagnostics.
// Unknown targets never produce a missing or visibility error.
func AnalyzeImports(loads []ImportLoad, members []ImportMember) []syntax.Diagnostic {
	diagnostics := make([]syntax.Diagnostic, 0, len(loads)+len(members))
	for _, load := range loads {
		if load.Self {
			diagnostics = append(diagnostics, syntax.Diagnostic{
				Code: "vim/E1088", Message: "Script cannot import itself", Span: load.Span,
			})
			continue
		}
		if load.Duplicate && load.Path != "" {
			diagnostics = append(diagnostics, syntax.Diagnostic{
				Code: "vim/E1262", Message: "Cannot import the same script twice: " + load.Path, Span: load.Span,
			})
			continue
		}
		if !load.Missing || load.Path == "" || load.Autoload && !load.Runtime {
			continue
		}
		diagnostics = append(diagnostics, syntax.Diagnostic{
			Code: "vim/E1053", Message: `Could not import "` + load.Path + `"`, Span: load.Span,
		})
	}
	for _, member := range members {
		if !member.TargetKnown || member.Name == "" || member.Exported {
			continue
		}
		if member.Exists {
			diagnostics = append(diagnostics, syntax.Diagnostic{
				Code: "vim/E1049", Message: "Item not exported in script: " + member.Name, Span: member.Span,
			})
		} else {
			diagnostics = append(diagnostics, syntax.Diagnostic{
				Code: "vim/E1048", Message: "Item not found in script: " + member.Name, Span: member.Span,
			})
		}
	}
	sort.SliceStable(diagnostics, func(left, right int) bool {
		if diagnostics[left].Span.Start != diagnostics[right].Span.Start {
			return diagnostics[left].Span.Start < diagnostics[right].Span.Start
		}
		return diagnostics[left].Code < diagnostics[right].Code
	})
	return diagnostics
}
