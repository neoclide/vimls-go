//go:build unix || darwin || linux

package parsecmd

import (
	"os"
	"syscall"
)

func openInputFile(path string) (*os.File, error) {
	// A regular file may have been replaced by a FIFO since the initial Stat.
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
