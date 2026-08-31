package main

import "testing"

func TestCommitCommand(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{"git commit -m test", true},
		{"git diff --check && git commit -m test", true},
		{"FOO=bar /usr/bin/git commit --amend", true},
		{"VIMLS_PONYTAIL_REVIEWED=1 git commit -m test", false},
		{"git status", false},
		{`echo "git commit"`, false},
	}
	for _, test := range tests {
		if got := shouldBlock(test.command); got != test.want {
			t.Errorf("shouldBlock(%q) = %t, want %t", test.command, got, test.want)
		}
	}
}
