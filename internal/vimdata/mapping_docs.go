package vimdata

// Command help is adapted from Vim v9.2.1015 runtime/doc/map.txt, revision
// 5ab969f719bb09555e90e8dff8c94fc37bcbf2ae. The installed
// /usr/local/share/vim/vim92/doc/map.txt was byte-identical when reviewed.
// Modified Vim manual excerpts (OPL-1.0+); see LICENSES/VIM-DOC.txt for
// attribution and modification details.
// These excerpts are maintained manually, without a documentation generator.

var mappingCommandDocumentation = buildMappingCommandDocumentation()

func buildMappingCommandDocumentation() map[string]string {
	docs := make(map[string]string)
	// Prefixes select the same modes for all four mapping command families.
	for _, mode := range []struct{ prefix, name string }{
		{"", "Normal, Visual, Select and Operator-pending"},
		{"n", "Normal"}, {"v", "Visual and Select"}, {"x", "Visual"},
		{"s", "Select"}, {"o", "Operator-pending"}, {"i", "Insert"},
		{"l", "Insert, Command-line and Lang-Arg"}, {"c", "Command-line"},
		{"t", "Terminal-Job"},
	} {
		for _, family := range []string{"map", "noremap", "unmap", "mapclear"} {
			name := mode.prefix + family
			docs[name] = mappingFamilyDocumentation(name, family, mode.name)
		}
	}
	for _, family := range []string{"map", "noremap", "unmap", "mapclear"} {
		docs[family+"!"] = mappingFamilyDocumentation(family+"!", family, "Insert and Command-line")
	}
	for _, mode := range []struct{ define, nonrecursive, remove, clear, name string }{
		{"abbreviate", "noreabbrev", "unabbreviate", "abclear", "Insert and Command-line"},
		{"iabbrev", "inoreabbrev", "iunabbrev", "iabclear", "Insert"},
		{"cabbrev", "cnoreabbrev", "cunabbrev", "cabclear", "Command-line"},
	} {
		docs[mode.define] = abbreviationDefinitionDocumentation(mode.define, mode.name, false)
		docs[mode.nonrecursive] = abbreviationDefinitionDocumentation(mode.nonrecursive, mode.name, true)
		docs[mode.remove] = mapFileCommandDoc(mode.remove, ":"+mode.remove+" [<buffer>] {lhs}",
			"Remove the abbreviation for `{lhs}` in **"+mode.name+"** mode. If none is found, remove abbreviations whose `{rhs}` matches `{lhs}`. This permits removal after expansion. To avoid expansion while typing the command, insert a CTRL-V (type it twice).\n\n"+
				"Use `<buffer>` to remove a buffer-local abbreviation.")
		docs[mode.clear] = mapFileCommandDoc(mode.clear, ":"+mode.clear+" [<buffer>]",
			"Remove all abbreviations for **"+mode.name+"** mode. Use `<buffer>` to remove abbreviations local to the current buffer.")
	}
	const commandBody = "With no arguments, list all user-defined commands. With only `{cmd}`, list commands whose names start with `{cmd}`. The listing marks `!` for `-bang`, `\"` for `-register`, `|` for `-bar`, and `b` for a buffer-local command. `:filter` can filter the names; a nonzero 'verbose' also shows where a command was defined and its completion argument.\n\n" +
		"With `{repl}`, define a user command named `{cmd}` with replacement text `{repl}`. An existing command causes an error unless `!` is specified to redefine it. There is one exception: when sourcing a script again, a command previously defined in that script is silently replaced even without `!`.\n\n" +
		"Attributes control arguments (`-nargs`), completion (`-complete`), ranges and counts (`-range`, `-count`, `-addr`), and special behavior such as `-bang`, `-bar`, `-register` and `-buffer`. Without `-nargs`, no arguments are allowed. `-buffer` makes the command local to the current buffer. See `command-attributes`.\n\n" +
		"Replacement text runs in the context of the defining script and can refer to script-local functions and mappings. See `command-repl` for replacement placeholders and command blocks."
	docs["command"] = mapFileCommandDoc("command", ":command\n:command {cmd}\n:command[!] [{attr}...] {cmd} {repl}", commandBody)
	docs["command!"] = mapFileCommandDoc("command!", ":command! [{attr}...] {cmd} {repl}", commandBody)
	docs["delcommand"] = mapFileCommandDoc("delcommand", ":delcommand {cmd}\n:delcommand -buffer {cmd}",
		"Delete the user-defined command `{cmd}`. With `-buffer`, delete the command defined for the current buffer. Deleting a command is not allowed while commands are being listed, for example from a timer.")
	docs["comclear"] = mapFileCommandDoc("comclear", ":comclear", "Delete all user-defined commands.")
	return docs
}

func mappingFamilyDocumentation(name, family, modes string) string {
	usage := ":" + name
	body := "Applies in **" + modes + "** mode.\n\n"
	switch family {
	case "map", "noremap":
		usage += " {lhs} {rhs}\n:" + name + " {lhs}\n:" + name
		if family == "map" {
			body += "Map the key sequence `{lhs}` to `{rhs}` for these modes. The result, including `{rhs}`, is then further scanned for mappings. This allows for nested and recursive use of mappings.\n\n"
		} else {
			body += "Map the key sequence `{lhs}` to `{rhs}` for these modes. Disallow mapping of `{rhs}`, to avoid nested and recursive mappings. This is often used to redefine a command.\n\n" +
				"Keys in `{rhs}` also do not trigger abbreviations, except `i_CTRL-]` and `c_CTRL-]`. When `<Plug>` appears in `{rhs}`, that part is always applied even when remapping is disallowed.\n\n"
		}
		body += "With no arguments, list all mappings for these modes. With only `{lhs}`, list mappings whose key sequence starts with `{lhs}`.\n\n" +
			"Trailing spaces are included in `{rhs}`, because space is a valid Normal mode command. See `map-trailing-white`. Use `<buffer>` before `{lhs}` to define or list buffer-local mappings; see `:map-arguments` for other special arguments."
	case "unmap":
		usage += " [<buffer>] {lhs}"
		body += "Remove the mapping of `{lhs}` for these modes. The mapping may remain defined for other modes where it applies.\n\n" +
			"It also works when `{lhs}` matches the `{rhs}` of a mapping, for when an abbreviation applied. Trailing spaces are included in `{lhs}`; see `map-trailing-white`.\n\n" +
			"Use `<buffer>` to remove a mapping local to the current buffer."
	case "mapclear":
		usage += " [<buffer>]"
		body += "Remove **ALL** mappings for these modes. Use `<buffer>` to remove buffer-local mappings.\n\n" +
			"Warning: this also removes the Mac and DOS standard mappings (`mac-standard-mappings` and `dos-standard-mappings`)."
	}
	return mapFileCommandDoc(name, usage, body)
}

func abbreviationDefinitionDocumentation(name, modes string, nonrecursive bool) string {
	usage := ":" + name + " [<expr>] [<buffer>] {lhs} {rhs}\n:" + name + " [<buffer>] {lhs}\n:" + name + " [<buffer>]"
	body := "Define an abbreviation from `{lhs}` to `{rhs}` for **" + modes + "** mode. If `{lhs}` already exists, replace it with the new `{rhs}`. The replacement may contain spaces.\n\n"
	if nonrecursive {
		body += "Do not remap this `{rhs}`.\n\n"
	}
	body += "With no arguments, list all abbreviations for these modes. With only `{lhs}`, list abbreviations that start with `{lhs}`. When typing `{lhs}` on the command line, a CTRL-V (typed twice) can prevent abbreviation expansion.\n\n" +
		"The first listing column indicates `i` for Insert mode, `c` for Command-line mode, or `!` for both. With a nonzero 'verbose', listing also shows where the abbreviation was last defined.\n\n" +
		"Use `<expr>` to evaluate the right-hand side as an expression, and `<buffer>` for a buffer-local abbreviation. See `:map-<expr>` and `:map-<buffer>`."
	return mapFileCommandDoc(name, usage, body)
}

func mapFileCommandDoc(name, usage, body string) string {
	return "### :" + name + "\n\n```vim\n" + usage + "\n```\n\n" + body
}

// LookupMappingCommandDocumentation returns manually maintained map.txt help
// for a canonical Ex command name. Matching is case-sensitive. Bang selects
// the documented map!, noremap!, unmap!, mapclear! or command! variant.
func LookupMappingCommandDocumentation(name string, bang bool) (string, bool) {
	if bang {
		name += "!"
	}
	doc, ok := mappingCommandDocumentation[name]
	return doc, ok
}
