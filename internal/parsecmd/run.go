package parsecmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/chemzqm/vimls-go/internal/syntax"
)

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, dialect syntax.Dialect) int {
	if len(args) > 1 {
		fmt.Fprintln(stderr, "expected at most one Vim script file")
		return 2
	}
	reader := stdin
	var file *os.File
	if len(args) == 1 && args[0] != "-" {
		opened, err := os.Open(args[0])
		if err != nil {
			fmt.Fprintf(stderr, "open %s: %v\n", args[0], err)
			return 1
		}
		file = opened
		defer file.Close()
		reader = file
	}
	source, err := io.ReadAll(reader)
	if err != nil {
		fmt.Fprintf(stderr, "read Vim script: %v\n", err)
		return 1
	}
	var parsed *syntax.File
	if dialect == syntax.Vim9 {
		parsed = (syntax.Vim9Parser{}).Parse(string(source))
	} else {
		parsed = (syntax.LegacyParser{}).Parse(string(source))
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(parsed); err != nil {
		fmt.Fprintf(stderr, "write syntax tree: %v\n", err)
		return 1
	}
	return 0
}
