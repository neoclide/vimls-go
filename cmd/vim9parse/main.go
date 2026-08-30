package main

import (
	"os"

	"github.com/neoclide/vimls-go/internal/parsecmd"
	"github.com/neoclide/vimls-go/internal/syntax"
)

func main() {
	os.Exit(parsecmd.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, syntax.Vim9))
}
