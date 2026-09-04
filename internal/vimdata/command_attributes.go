package vimdata

import "strings"

type UserCommandAttribute struct {
	Name          string
	Detail        string
	Documentation string
}

var userCommandAttributes = []UserCommandAttribute{
	{
		Name:          "addr=",
		Detail:        "command address type",
		Documentation: "Type of address for range, e.g. lines, arguments, buffers, loaded_buffers, windows, tabs, quickfix, other.",
	},
	{
		Name:          "bang",
		Detail:        "command flag",
		Documentation: "The command can take a ! modifier (like :q or :w). Available in replacement text as <bang>.",
	},
	{
		Name:          "bar",
		Detail:        "command flag",
		Documentation: "The command can be followed by a '|' and another command, or a comment starting with '\"'.",
	},
	{
		Name:          "buffer",
		Detail:        "command scope",
		Documentation: "The command will only be available in the current buffer.",
	},
	{
		Name:          "complete=",
		Detail:        "command completion",
		Documentation: "Enables argument completion (e.g. file, buffer, custom,{func}, customlist,{func}).",
	},
	{
		Name:          "completeopt=",
		Detail:        "command completion option",
		Documentation: "Comma-separated list of completion options for custom/customlist completion (currently 'escape').",
	},
	{
		Name:          "count",
		Detail:        "command count",
		Documentation: "The command takes an arbitrary count value (-count or -count=N). Available in replacement text as <count>.",
	},
	{
		Name:          "keepscript",
		Detail:        "command flag",
		Documentation: "Do not use the definition script location for verbose messages; use the invocation location instead.",
	},
	{
		Name:          "nargs=",
		Detail:        "command argument count",
		Documentation: "Specifies how many arguments are allowed: 0 (default), 1, _, *, ?, +.",
	},
	{
		Name:          "range",
		Detail:        "command range",
		Documentation: "Range allowed: -range (current line), -range=% (whole file), or -range=N (count default N). Available as <line1>, <line2>, <range>.",
	},
	{
		Name:          "register",
		Detail:        "command register",
		Documentation: "The first argument can be an optional register name (like :del, :put, :yank). Available in replacement text as <reg>.",
	},
}

var userCommandNargsValues = []CompletionValue{
	{Name: "0", Documentation: "No arguments are allowed (the default)."},
	{Name: "1", Documentation: "Exactly one argument is required, includes spaces; completion treats white spaces as argument separation."},
	{Name: "_", Documentation: "Exactly one argument is required, includes spaces; completion treats white spaces as part of the argument."},
	{Name: "*", Documentation: "Any number of arguments are allowed (0, 1, or many), separated by white space."},
	{Name: "?", Documentation: "0 or 1 arguments are allowed."},
	{Name: "+", Documentation: "Arguments must be supplied, but any number are allowed."},
}

var userCommandAddrValues = []CompletionValue{
	{Name: "arguments", Documentation: "Range for arguments."},
	{Name: "buffers", Documentation: "Range for buffers (also not loaded buffers)."},
	{Name: "lines", Documentation: "Range of lines (the default for -range)."},
	{Name: "loaded_buffers", Documentation: "Range for loaded buffers."},
	{Name: "other", Documentation: "Other kind of range; can use '.', '$' and '%' as with 'lines' (the default for -count)."},
	{Name: "quickfix", Documentation: "Range for quickfix entries."},
	{Name: "tabs", Documentation: "Range for tab pages."},
	{Name: "windows", Documentation: "Range for windows."},
}

var userCommandCompleteoptValues = []CompletionValue{
	{Name: "escape", Documentation: "Preserve spaces, tabs and backslashes by escaping them when inserting completion matches."},
}

var userCommandCompleteValues = []CompletionValue{
	{Name: "arglist", Documentation: "File names in argument list."},
	{Name: "augroup", Documentation: "Autocmd groups."},
	{Name: "behave", Documentation: ":behave suboptions."},
	{Name: "breakpoint", Documentation: ":breakadd suboptions."},
	{Name: "buffer", Documentation: "Buffer names."},
	{Name: "color", Documentation: "Color schemes."},
	{Name: "command", Documentation: "Ex command (and arguments)."},
	{Name: "compiler", Documentation: "Compilers."},
	{Name: "cscope", Documentation: ":cscope suboptions."},
	{Name: "custom", Documentation: "Custom completion, defined via custom,{func}."},
	{Name: "customlist", Documentation: "Custom completion, defined via customlist,{func}."},
	{Name: "diff_buffer", Documentation: "Diff buffer names."},
	{Name: "dir", Documentation: "Directory names."},
	{Name: "dir_in_path", Documentation: "Directory names in 'cdpath'."},
	{Name: "environment", Documentation: "Environment variable names."},
	{Name: "event", Documentation: "Autocommand events."},
	{Name: "expression", Documentation: "Vim expression."},
	{Name: "file", Documentation: "File and directory names."},
	{Name: "file_in_path", Documentation: "File and directory names in 'path'."},
	{Name: "filetype", Documentation: "Filetype names in 'filetype'."},
	{Name: "filetypecmd", Documentation: "Filetype suboptions for :filetype."},
	{Name: "function", Documentation: "Function names."},
	{Name: "help", Documentation: "Help subjects."},
	{Name: "highlight", Documentation: "Highlight groups."},
	{Name: "history", Documentation: ":history suboptions."},
	{Name: "keymap", Documentation: "Keyboard mappings."},
	{Name: "locale", Documentation: "Locale names (as output of locale -a)."},
	{Name: "mapclear", Documentation: "Buffer argument for :mapclear."},
	{Name: "mapping", Documentation: "Mapping names."},
	{Name: "menu", Documentation: "Menus."},
	{Name: "messages", Documentation: ":messages suboptions."},
	{Name: "option", Documentation: "Options."},
	{Name: "packadd", Documentation: "Optional package names."},
	{Name: "retab", Documentation: ":retab suboptions."},
	{Name: "runtime", Documentation: "File and directory names in 'runtimepath'."},
	{Name: "scriptnames", Documentation: "Sourced script names."},
	{Name: "shellcmd", Documentation: "Shell commands."},
	{Name: "shellcmdline", Documentation: "First is a shell command and subsequent ones are filenames."},
	{Name: "sign", Documentation: ":sign suboptions."},
	{Name: "syntax", Documentation: "Syntax file names."},
	{Name: "syntime", Documentation: ":syntime suboptions."},
	{Name: "tag", Documentation: "Tags."},
	{Name: "tag_listfiles", Documentation: "Tags, file names are shown when CTRL-D is hit."},
	{Name: "user", Documentation: "User names."},
	{Name: "var", Documentation: "User variables."},
}

func UserCommandAttributes() []UserCommandAttribute {
	return append([]UserCommandAttribute(nil), userCommandAttributes...)
}

func normalizeAttributeName(name string) string {
	name = strings.TrimPrefix(name, "-")
	name = strings.TrimSuffix(name, "=")
	return strings.ToLower(name)
}

func LookupUserCommandAttribute(name string) (UserCommandAttribute, bool) {
	target := normalizeAttributeName(name)
	for _, attr := range userCommandAttributes {
		if normalizeAttributeName(attr.Name) == target {
			return attr, true
		}
	}
	return UserCommandAttribute{}, false
}

func UserCommandNargsValues() []CompletionValue {
	return append([]CompletionValue(nil), userCommandNargsValues...)
}

func UserCommandAddrValues() []CompletionValue {
	return append([]CompletionValue(nil), userCommandAddrValues...)
}

func UserCommandCompleteoptValues() []CompletionValue {
	return append([]CompletionValue(nil), userCommandCompleteoptValues...)
}

func UserCommandCompleteValues() []CompletionValue {
	return append([]CompletionValue(nil), userCommandCompleteValues...)
}

func LookupUserCommandAttributeValue(attribute, value string) (CompletionValue, string, bool) {
	attrName := normalizeAttributeName(attribute)
	var values []CompletionValue
	var detail string
	switch attrName {
	case "addr":
		values = userCommandAddrValues
		detail = "command address type"
	case "complete":
		values = userCommandCompleteValues
		detail = "command completion type"
	case "completeopt":
		values = userCommandCompleteoptValues
		detail = "command completion option"
	case "nargs":
		values = userCommandNargsValues
		detail = "command argument count"
	default:
		return CompletionValue{}, "", false
	}
	value = strings.TrimSpace(value)
	for _, item := range values {
		if item.Name == value {
			return item, detail, true
		}
	}
	return CompletionValue{}, "", false
}
