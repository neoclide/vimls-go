package vimdata

// Reviewed against https://macvim.org/docs/gui_mac.txt.html#macvim-options
// and options.txt (r183.1, Vim 9.2.699), accessed 2026-09-06.
// toolbariconsize is also a GTK Vim option, so it is not MacVim-only.
// These overlays supply diagnostics and presentation metadata.
var macvimCompatOptions = []Option{
	{Name: "antialias", ShortName: "anti", Type: OptionBool, Documentation: "'antialias' 'anti' boolean\nglobal\nMacVim GUI: Smooths font edges.\n\n[MacVim reference](https://macvim.org/docs/options.txt.html#'antialias')", DocumentationSource: "https://macvim.org/docs/options.txt.html"},
	{Name: "blurradius", ShortName: "blur", Type: OptionNumber, Documentation: "'blurradius' 'blur' number\nglobal\nMacVim GUI: Positive values blur the background when transparency is enabled.\n\n[MacVim reference](https://macvim.org/docs/options.txt.html#'blurradius')", DocumentationSource: "https://macvim.org/docs/options.txt.html"},
	{Name: "fullscreen", ShortName: "fu", Type: OptionBool, Documentation: "'fullscreen' 'fu' boolean\nglobal\nMacVim GUI: Enables fullscreen display. Custom fullscreen behavior uses fuoptions.\n\n[MacVim reference](https://macvim.org/docs/options.txt.html#'fullscreen')", DocumentationSource: "https://macvim.org/docs/options.txt.html"},
	{Name: "fuoptions", ShortName: "fuopt", Type: OptionString, Documentation: "'fuoptions' 'fuopt' string\nglobal\nMacVim GUI: Configures custom fullscreen using maxvert, maxhorz and background:color. Color accepts #rrggbb, #aarrggbb or a highlight group. Native fullscreen ignores this option.\n\n[MacVim reference](https://macvim.org/docs/options.txt.html#'fuoptions')", DocumentationSource: "https://macvim.org/docs/options.txt.html"},
	{Name: "macligatures", Type: OptionBool, Documentation: "'macligatures' boolean\nglobal\nMacVim GUI: Enables font ligatures with a supporting guifont and the Core Text renderer.\n\n[MacVim reference](https://macvim.org/docs/options.txt.html#'macligatures')", DocumentationSource: "https://macvim.org/docs/options.txt.html"},
	{Name: "macmeta", ShortName: "mmta", Type: OptionBool, Scope: OptionBuffer, Documentation: "'macmeta' 'mmta' boolean\nlocal to buffer\nMacVim GUI: Treats Option/Alt as Meta for key mappings instead of text input.\n\n[MacVim reference](https://macvim.org/docs/options.txt.html#'macmeta')", DocumentationSource: "https://macvim.org/docs/options.txt.html"},
	{Name: "macthinstrokes", Type: OptionBool, Documentation: "'macthinstrokes' boolean\nglobal\nMacVim GUI: Uses lighter text strokes with the Core Text renderer.\n\n[MacVim reference](https://macvim.org/docs/options.txt.html#'macthinstrokes')", DocumentationSource: "https://macvim.org/docs/options.txt.html"},
	{Name: "transparency", ShortName: "transp", Type: OptionNumber, Validation: compatNumberRange(0, 100), Documentation: "'transparency' 'transp' number\nglobal\nMacVim GUI: Sets window transparency from 0 (opaque) to 100 (transparent).\n\n[MacVim reference](https://macvim.org/docs/options.txt.html#'transparency')", DocumentationSource: "https://macvim.org/docs/options.txt.html"},
}

// IsMacVimCompatOption also recognizes short names and option selectors.
func IsMacVimCompatOption(name string) bool {
	_, ok := lookupMacVimCompatOption(optionLookupName(name))
	return ok
}

func lookupMacVimCompatOption(name string) (Option, bool) {
	for _, option := range macvimCompatOptions {
		if name == option.Name || option.ShortName != "" && name == option.ShortName {
			return cloneOption(option), true
		}
	}
	return Option{}, false
}

// LookupOptionMetadata returns option metadata for hover and semantic tokens.
// MacVim overlays take precedence over obsolete entries such as antialias.
// Diagnostic lookup and the pinned completion list remain independent.
func LookupOptionMetadata(name string) (Option, bool) {
	if option, ok := lookupMacVimCompatOption(optionLookupName(name)); ok {
		return option, true
	}
	return LookupOption(name)
}
