package analysis

import "github.com/neoclide/vimls-go/internal/syntax"

// DiagnosticsForVersion adapts semantic diagnostics to the target Vim version.
// It does not modify diagnostics.
func DiagnosticsForVersion(file *syntax.File, diagnostics []syntax.Diagnostic, target syntax.Version) []syntax.Diagnostic {
	if target.Major > 9 || target.Major == 9 && (target.Minor > 2 || target.Minor == 2 && target.Patch >= 507) {
		return diagnostics
	}
	versioned := append([]syntax.Diagnostic(nil), diagnostics...)
	for index := range versioned {
		if versioned[index].Code == "vim/E1406" {
			versioned[index].Code = "vim/E1369"
			versioned[index].Message = "Duplicate variable: " + file.Text(versioned[index].Span)
		}
	}
	return versioned
}
