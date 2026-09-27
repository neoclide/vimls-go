package vimdata

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	vimSourceTagPattern    = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	vimSourceCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

func TestVimSourceIsCanonical(t *testing.T) {
	if !vimSourceTagPattern.MatchString(VimSourceTag) || !vimSourceCommitPattern.MatchString(VimSourceCommit) || !vimSourceCommitPattern.MatchString(NeovimSourceCommit) {
		t.Fatalf("invalid source pins: Vim=%q/%q Neovim=%q", VimSourceTag, VimSourceCommit, NeovimSourceCommit)
	}
}

func TestManualVimSourceMatchesReviewedActiveSource(t *testing.T) {
	if ManualVimTag != VimSourceTag || ManualVimCommit != VimSourceCommit {
		t.Fatalf("manual Vim source = %s/%s, active source = %s/%s; review manual metadata before updating the active pin", ManualVimTag, ManualVimCommit, VimSourceTag, VimSourceCommit)
	}
}

func TestCurrentVimSourceIsDocumented(t *testing.T) {
	for _, path := range []string{
		filepath.Join("..", "..", "docs", "language-support.md"),
		filepath.Join("..", "..", "LICENSES", "VIM-DOC.txt"),
	} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(source), VimSourceTag) || !strings.Contains(string(source), VimSourceCommit) {
			t.Fatalf("%s does not record the current Vim source %s/%s", path, VimSourceTag, VimSourceCommit)
		}
	}
}
