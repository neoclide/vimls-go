package analysis

import "github.com/neoclide/vimls-go/internal/syntax"

// AutoloadExportedDefDiagnostics adjusts duplicate script-item diagnostics for
// exported Vim9 definitions in autoload scripts. It does not modify diagnostics.
func AutoloadExportedDefDiagnostics(file *syntax.File, result *FileAnalysis, autoload bool, diagnostics []syntax.Diagnostic) []syntax.Diagnostic {
	if !autoload || file == nil || result == nil || result.Root == nil {
		return diagnostics
	}
	adjusted := diagnostics
	copied := false
	for index := range diagnostics {
		diagnostic := diagnostics[index]
		if diagnostic.Code != "vim/E1041" || !autoloadExportedDefVariableConflict(file, result, diagnostic.Span) {
			continue
		}
		if !copied {
			adjusted = append([]syntax.Diagnostic(nil), diagnostics...)
			copied = true
		}
		name := file.Text(diagnostic.Span)
		adjusted[index].Code = "vim/E707"
		adjusted[index].Message = "Function name conflicts with variable: " + name
	}
	return adjusted
}

func autoloadExportedDefVariableConflict(file *syntax.File, result *FileAnalysis, span syntax.Span) bool {
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Canonical != "def" || command.Function == nil || command.Function.Name != span || !commandHasModifier(command, "export") {
			continue
		}
		name := file.Text(span)
		for _, declaration := range result.Root.Declarations {
			if declaration != nil && declaration.Span.Start < span.Start && declaration.Name == name &&
				(declaration.Kind == SymbolKindVariable || declaration.Kind == SymbolKindConstant) {
				return true
			}
		}
	}
	return false
}
