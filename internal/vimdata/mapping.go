package vimdata

import "strings"

// MappingItem describes a mapping modifier or special key form in Vim.
type MappingItem struct {
	Name          string
	Detail        string
	Documentation string
}

var mappingItems = []MappingItem{
	{
		Name:   "<buffer>",
		Detail: "A mapping argument.",
		Documentation: `If the first argument to one of these commands is "<buffer>" the mapping will
be effective in the current buffer only.  Example:
` + "```vim\n:map <buffer>  ,w  /[.,;]<CR>\n```" + `
Then you can map ",w" to something else in another buffer:
` + "```vim\n:map <buffer>  ,w  /[#&!]<CR>\n```" + `
The local buffer mappings are used before the global ones.  See ` + "`<nowait>`" + ` below
to make a short local mapping take effect when a longer global one exists.
The "<buffer>" argument can also be used to clear mappings:
` + "```vim\n:unmap <buffer> ,w\n:mapclear <buffer>\n```" + `
Local mappings are also cleared when a buffer is deleted, but not when it is
unloaded.  Just like local option values.
Also see ` + "`map-precedence`" + `.`,
	},
	{
		Name:   "<nowait>",
		Detail: "A mapping argument.",
		Documentation: `When defining a buffer-local mapping for "," there may be a global mapping
that starts with ",".  Then you need to type another character for Vim to know
whether to use the "," mapping or the longer one.  To avoid this add the
` + "`<nowait>`" + ` argument.  Then the mapping will be used when it matches, Vim does
not wait for more characters to be typed.  However, if the characters were
already typed they are used.
Note that this works when the ` + "`<nowait>`" + ` mapping fully matches and is found
before any partial matches.  This works when:
- There is only one matching buffer-local mapping, since these are always
  found before global mappings.
- There is another buffer-local mapping that partly matches, but it is
  defined earlier (last defined mapping is found first).`,
	},
	{
		Name:   "<silent>",
		Detail: "A mapping argument.",
		Documentation: `To define a mapping which will not be echoed on the command line, add
"<silent>" as the first argument.  Example:
` + "```vim\n:map <silent> ,h /Header<CR>\n```" + `
The search string will not be echoed when using this mapping.  Messages from
the executed command are still given though.  To shut them up too, add a
":silent" in the executed command:
` + "```vim\n:map <silent> ,h :exe \":silent normal /Header\\r\"<CR>\n```" + `
Note that the effect of a command might also be silenced, e.g., when the
mapping selects another entry for command line completion it won't be
displayed.
Prompts will still be given, e.g., for ` + "`inputdialog()`" + `.
Using "<silent>" for an abbreviation is possible, but will cause redrawing of
the command line to fail.`,
	},
	{
		Name:   "<special>",
		Detail: "A mapping argument.",
		Documentation: `Define a mapping with <> notation for special keys, even though the "<" flag
may appear in 'cpoptions'.  This is useful if the side effect of setting
'cpoptions' is not desired.  Example:
` + "```vim\n:map <special> <F12> /Header<CR>\n```",
	},
	{
		Name:   "<script>",
		Detail: "A mapping argument.",
		Documentation: `If the first argument to one of these commands is "<script>" and it is used to
define a new mapping or abbreviation, the mapping will only remap characters
in the {rhs} using mappings that were defined local to a script, starting with
"<SID>".  This can be used to avoid that mappings from outside a script
interfere (e.g., when CTRL-V is remapped in mswin.vim), but do use other
mappings defined in the script.
Note: ":map <script>" and ":noremap <script>" do the same thing.  The
"<script>" overrules the command name.  Using ":noremap <script>" is
preferred, because it's clearer that remapping is (mostly) disabled.`,
	},
	{
		Name:   "<unique>",
		Detail: "A mapping argument.",
		Documentation: `If the first argument to one of these commands is "<unique>" and it is used to
define a new mapping or abbreviation, the command will fail if the mapping or
abbreviation already exists.  Example:
` + "```vim\n:map <unique> ,w  /[#&!]<CR>\n```" + `
When defining a local mapping, there will also be a check if a global map
already exists which is equal.
Example of what will fail:
` + "```vim\n:map ,w  /[#&!]<CR>\n:map <buffer> <unique> ,w  /[.,;]<CR>\n```" + `
If you want to map a key and then have it do what it was originally mapped to,
have a look at ` + "`maparg()`" + `.`,
	},
	{
		Name:   "<expr>",
		Detail: "A mapping argument.",
		Documentation: `If the first argument to one of these commands is "<expr>" and it is used to
define a new mapping or abbreviation, the argument is an expression.  The
expression is evaluated to obtain the {rhs} that is used.  Example:
` + "```vim\n:inoremap <expr> . <SID>InsertDot()\n```" + `
The result of the s:InsertDot() function will be inserted.  It could check the
text before the cursor and start omni completion when some condition is met.
Using a script-local function is preferred, to avoid polluting the global
namespace.  Use <SID> in the RHS so that the script that the mapping was
defined in can be found.

For abbreviations ` + "`v:char`" + ` is set to the character that was typed to trigger
the abbreviation.  You can use this to decide how to expand the {lhs}.  You
should not either insert or change the ` + "`v:char`" + `.`,
	},
	{
		Name:   "<Plug>",
		Detail: "A special key name for internal mappings.",
		Documentation: `The special key name "<Plug>" can be used for an internal mapping, which is
not to be matched with any key sequence.  This is useful in plugins
` + "`using-<Plug>`" + `.`,
	},
	{
		Name:   "<SID>",
		Detail: "A special key name for script-local mappings.",
		Documentation: `In a script the special key name "<SID>" can be used to define a mapping
that's local to the script.  See ` + "`<SID>`" + ` for details.`,
	},
	{
		Name:   "<ScriptCmd>",
		Detail: "A script command mapping.",
		Documentation: `The special text <ScriptCmd> begins a "script command mapping", it executes
the command directly without changing modes, using the script context where
the mapping was defined.`,
	},
	{
		Name:   "<Cmd>",
		Detail: "A command mapping.",
		Documentation: `The special text <Cmd> begins a "command mapping", it executes the command
directly without changing modes.  Where you might use ":...<CR>" in the
{rhs} of a mapping, you can instead use "<Cmd>...<CR>".
Example:
` + "```vim\nnoremap x <Cmd>echo mode(1)<CR>\n```",
	},
	{
		Name:   "<Leader>",
		Detail: "A map leader prefix.",
		Documentation: `To define a mapping which uses the "g:mapleader" variable, the special string
"<Leader>" can be used.  It is replaced with the string value of
"g:mapleader".  If "g:mapleader" is not set or empty, a backslash is used
instead.  Example:
` + "```vim\nmap <Leader>A  oanother line<Esc>\n```",
	},
	{
		Name:   "<LocalLeader>",
		Detail: "A local map leader prefix.",
		Documentation: `<LocalLeader> is just like <Leader>, except that it uses "maplocalleader"
instead of "mapleader".  <LocalLeader> is to be used for mappings which are
local to a buffer.  Example:
` + "```vim\n:map <buffer> <LocalLeader>A  oanother line<Esc>\n```",
	},
}

func normalizeMappingName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "<")
	name = strings.TrimSuffix(name, ">")
	return strings.ToLower(name)
}

// LookupMappingItem returns mapping modifier or special key documentation for names
// like "<silent>", "silent", "<Plug>", "Plug", etc.
func LookupMappingItem(name string) (MappingItem, bool) {
	norm := normalizeMappingName(name)
	for _, item := range mappingItems {
		if normalizeMappingName(item.Name) == norm {
			return item, true
		}
	}
	return MappingItem{}, false
}
