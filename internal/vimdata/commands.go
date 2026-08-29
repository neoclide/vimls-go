package vimdata

import "strings"

// CommandFlags are the Ex command properties needed at parse time.
type CommandFlags uint8

const (
	AllowBang CommandFlags = 1 << iota
	AllowBar
	NoTrailingComment
	ExpressionArgument
	NeedArgument
	ExactInVim9
	FileArgument
	Exportable
)

// Command describes one built-in Ex command from the pinned Vim baseline.
type Command struct {
	Name  string
	Flags CommandFlags
}

var (
	commandLookupStart [256]int
	commandLookupEnd   [256]int
)

func init() {
	for index := range commands {
		first := commands[index].Name[0]
		if commandLookupEnd[first] == 0 {
			commandLookupStart[first] = index
		}
		commandLookupEnd[first] = index + 1
	}
}

// Lookup resolves a built-in command or its abbreviation using Vim's command
// table order. That order is significant when abbreviations are ambiguous.
func Lookup(name string) (Command, bool) {
	if name == "" {
		return Command{}, false
	}
	start := commandLookupStart[name[0]]
	end := commandLookupEnd[name[0]]
	for _, command := range commands[start:end] {
		if strings.HasPrefix(command.Name, name) {
			return command, true
		}
	}
	return Command{}, false
}
