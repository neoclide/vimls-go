package main

import (
	"os"

	"github.com/chemzqm/vimls-go/internal/parsecmd"
	"github.com/chemzqm/vimls-go/internal/syntax"
)

func main() {
	os.Exit(parsecmd.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, syntax.Legacy))
}
