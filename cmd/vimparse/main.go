package main

import (
	"os"

	"github.com/neoclide/vimls-go/internal/parsecmd"
)

func main() {
	os.Exit(parsecmd.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
