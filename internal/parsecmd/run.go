package parsecmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "expected one Vim script file")
		return 2
	}
	source, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "read %s: %v\n", args[0], err)
		return 1
	}
	parsed := syntax.Parse(string(source))
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(parsed); err != nil {
		fmt.Fprintf(stderr, "write syntax tree: %v\n", err)
		return 1
	}
	return 0
}
