// Command vimsource prints the current pinned Vim and Neovim source values.
package main

import (
	"fmt"
	"os"

	"github.com/neoclide/vimls-go/internal/vimdata"
)

func main() {
	values, err := sourceValues()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vimsource:", err)
		os.Exit(1)
	}
	for _, value := range values {
		fmt.Println(value)
	}
}

func sourceValues() ([]string, error) {
	version, ok := vimdata.ParseVimVersion(vimdata.VimSourceTag)
	if !ok {
		return nil, fmt.Errorf("invalid Vim source tag %q", vimdata.VimSourceTag)
	}
	next := version
	next.Patch++
	return []string{
		"VIMLS_VIM_TAG=" + vimdata.VimSourceTag,
		"VIMLS_VIM_COMMIT=" + vimdata.VimSourceCommit,
		"VIMLS_NEOVIM_COMMIT=" + vimdata.NeovimSourceCommit,
		"VIMLS_VIM_VERSION=" + version.String(),
		"VIMLS_VIM_NEXT_PATCH=" + next.String(),
		fmt.Sprintf("VIMLS_VIM_VVERSION=%d", version.Major*100+version.Minor),
	}, nil
}
