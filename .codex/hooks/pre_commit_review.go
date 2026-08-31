package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const reviewMarker = "VIMLS_PONYTAIL_REVIEWED=1"

var commitCommand = regexp.MustCompile(`(?:^|[;&|]\s*)(?:[A-Za-z_][A-Za-z0-9_]*=[^\s]+\s+)*(?:[^\s]*/)?git\s+(?:-[^\s]+\s+)*commit(?:\s|$)`)

type input struct {
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

func main() {
	var in input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		return
	}
	if !shouldBlock(in.ToolInput.Command) {
		return
	}

	fmt.Print(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"Run /ponytail-review and resolve its findings before committing. After the review passes, retry with VIMLS_PONYTAIL_REVIEWED=1 git commit ..."}}`)
}

func shouldBlock(command string) bool {
	return commitCommand.MatchString(command) && !strings.Contains(command, reviewMarker)
}
