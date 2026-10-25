package workspace

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// AutoloadNameDiagnostics checks literal, unconditional function definitions
// against their source filename. Conditional definitions and redefinitions
// remain conservative because Vim checks E746 only when creating a function.
func AutoloadNameDiagnostics(path string, file *syntax.File) []syntax.Diagnostic {
	if path == "" || file == nil {
		return nil
	}
	path = filepath.ToSlash(filepath.Clean(path))
	var diagnostics []syntax.Diagnostic
	seen := make(map[string]bool)
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Function == nil || command.Dialect != syntax.Legacy || command.Block < 0 || command.Block >= len(file.Blocks) {
			continue
		}
		block := file.Blocks[command.Block]
		if block.Parent >= 0 || block.Header != index || block.End < 0 {
			continue
		}
		name := file.Text(command.Function.Name)
		if strings.ContainsAny(strings.TrimPrefix(name, "g:"), "{}.:<>") {
			continue
		}
		relative, ok := AutoloadPath(name)
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		// Vim compares the suffix after "autoload", allowing plugin/foo.vim
		// as well as autoload/foo.vim (userfunc.c, Vim v9.2.1132).
		suffix := strings.TrimPrefix(relative, "autoload")
		matches := strings.HasSuffix(path, suffix)
		if runtime.GOOS == "windows" {
			matches = strings.HasSuffix(strings.ToLower(path), strings.ToLower(suffix))
		}
		if matches {
			continue
		}
		diagnostics = append(diagnostics, syntax.Diagnostic{Code: "vim/E746", Message: "Function name does not match script file name: " + name, Span: command.Function.Name})
	}
	return diagnostics
}
