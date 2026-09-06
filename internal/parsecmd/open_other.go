//go:build !unix && !darwin && !linux

package parsecmd

import "os"

func openInputFile(path string) (*os.File, error) {
	return os.Open(path)
}
