package vimdata

import "sort"

// OptionType is the Vim value category accepted by an option.
type OptionType uint8

const (
	OptionBool OptionType = iota
	OptionNumber
	OptionString
)

// OptionScope describes where Vim stores an option value.
type OptionScope uint8

const (
	OptionGlobal OptionScope = iota
	OptionWindow
	OptionBuffer
	OptionGlobalLocal
)

// Option describes one option from Vim's pinned options[] table. Terminal
// t_XX options are included because Vim accepts them through &t_XX syntax.
type Option struct {
	Name                string
	ShortName           string
	Type                OptionType
	Scope               OptionScope
	Documentation       string
	DocumentationSource string
}

// LookupOption resolves an exact canonical name or Vim's exact documented
// abbreviation. Vim does not accept arbitrary unique prefixes. The & sigil
// and an optional g: or l: selector are accepted for expression analysis.
func LookupOption(name string) (Option, bool) {
	name = optionLookupName(name)
	if name == "" {
		return Option{}, false
	}
	for _, option := range builtinOptions {
		if option.Name == name {
			return option, true
		}
	}
	for _, option := range builtinOptions {
		if option.ShortName == name {
			return option, true
		}
	}
	return Option{}, false
}

// IsTerminalOptionName reports the t_xx option namespace accepted by Vim.
// Terminal option names are runtime/build dependent, so an entry missing from
// the pinned table is still a valid terminal option rather than an unknown
// ordinary option.
func IsTerminalOptionName(name string) bool {
	name = optionLookupName(name)
	return len(name) >= 2 && name[:2] == "t_"
}

func optionLookupName(name string) string {
	if len(name) > 0 && name[0] == '&' {
		name = name[1:]
		if len(name) > 2 && (name[:2] == "g:" || name[:2] == "l:") {
			name = name[2:]
		}
	}
	if len(name) >= 4 && name[0] == '<' && name[len(name)-1] == '>' && name[1] == 't' && name[2] == '_' {
		name = name[1 : len(name)-1]
	}
	return name
}

// BuiltinOptionCount reports the number of normal and terminal options in the
// pinned Vim options[] table.
func BuiltinOptionCount() int { return len(builtinOptions) }

// Options returns the pinned options[] table by canonical name.  Callers own
// the returned slice.
func Options() []Option {
	result := append([]Option(nil), builtinOptions[:]...)
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
