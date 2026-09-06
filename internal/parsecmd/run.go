package parsecmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/neoclide/vimls-go/internal/syntax"
)

const maxInputBytes = 4 << 20

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "expected one Vim script file or - for stdin")
		return 2
	}
	source, err := readSource(args[0], stdin)
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

func readSource(path string, stdin io.Reader) ([]byte, error) {
	input := stdin
	if path != "-" {
		// Check before opening: opening a FIFO can block waiting for a writer.
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if err := checkInputFile(info); err != nil {
			return nil, err
		}
		file, err := openInputFile(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		info, err = file.Stat()
		if err != nil {
			return nil, err
		}
		if err := checkInputFile(info); err != nil {
			return nil, err
		}
		input = file
	}
	if input == nil {
		return nil, fmt.Errorf("stdin is unavailable")
	}
	// Read one extra byte to distinguish an exact-limit input from truncation.
	source, err := io.ReadAll(io.LimitReader(input, maxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(source) > maxInputBytes {
		return nil, fmt.Errorf("input exceeds the 4 MiB limit")
	}
	return source, nil
}

func checkInputFile(info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("expected a regular file; use - to read stdin")
	}
	if info.Size() > maxInputBytes {
		return fmt.Errorf("input exceeds the 4 MiB limit")
	}
	return nil
}
