//go:build !unix && !darwin && !linux

package server

import "os"

func openNonBlockingFile(path string) (*os.File, error) {
	return os.Open(path)
}
