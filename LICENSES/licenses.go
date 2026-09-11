// Package licenses embeds distribution notices for standalone executables.
package licenses

import (
	"embed"
	"fmt"
	"io"
)

// Texts is the same set of license and attribution files shipped in archives.
//
//go:embed *.txt
var Texts embed.FS

// Write prints every embedded notice in filename order.
func Write(w io.Writer) error {
	entries, err := Texts.ReadDir(".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		data, err := Texts.ReadFile(entry.Name())
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "=== %s ===\n%s\n", entry.Name(), data); err != nil {
			return err
		}
	}
	return nil
}
