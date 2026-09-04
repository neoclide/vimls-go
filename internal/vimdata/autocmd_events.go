package vimdata

import "strings"

const (
	AutocmdEventVimTag    = "v9.2.1015"
	AutocmdEventVimCommit = "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae"
)

// AutocmdEvent is one entry from Vim tag v9.2.1015 commit
// 5ab969f719bb09555e90e8dff8c94fc37bcbf2ae src/autocmd.c event_tab[].
// AliasOf identifies aliases represented by the same event key.
type AutocmdEvent struct{ Name, AliasOf string }

var autocmdEvents = []AutocmdEvent{
	{"BufAdd", ""}, {"BufCreate", "BufAdd"}, {"BufDelete", ""}, {"BufEnter", ""}, {"BufFilePost", ""}, {"BufFilePre", ""}, {"BufHidden", ""}, {"BufLeave", ""}, {"BufNew", ""}, {"BufNewFile", ""}, {"BufRead", "BufReadPost"}, {"BufReadCmd", ""}, {"BufReadPost", ""}, {"BufReadPre", ""}, {"BufUnload", ""}, {"BufWinEnter", ""}, {"BufWinLeave", ""}, {"BufWipeout", ""}, {"BufWrite", "BufWritePost"}, {"BufWriteCmd", ""}, {"BufWritePost", ""}, {"BufWritePre", ""},
	{"CmdlineChanged", ""}, {"CmdlineEnter", ""}, {"CmdlineLeave", ""}, {"CmdlineLeavePre", ""}, {"CmdUndefined", ""}, {"CmdwinEnter", ""}, {"CmdwinLeave", ""}, {"ColorScheme", ""}, {"ColorSchemePre", ""}, {"CompleteChanged", ""}, {"CompleteDone", ""}, {"CompleteDonePre", ""}, {"CursorHold", ""}, {"CursorHoldI", ""}, {"CursorMoved", ""}, {"CursorMovedC", ""}, {"CursorMovedI", ""}, {"DiffUpdated", ""}, {"DirChanged", ""}, {"DirChangedPre", ""}, {"EncodingChanged", ""}, {"ExitPre", ""}, {"FileAppendCmd", ""}, {"FileAppendPost", ""}, {"FileAppendPre", ""}, {"FileChangedRO", ""}, {"FileChangedShell", ""}, {"FileChangedShellPost", ""}, {"FileEncoding", "EncodingChanged"}, {"FileReadCmd", ""}, {"FileReadPost", ""}, {"FileReadPre", ""}, {"FileType", ""}, {"FileWriteCmd", ""}, {"FileWritePost", ""}, {"FileWritePre", ""}, {"FilterReadPost", ""}, {"FilterReadPre", ""}, {"FilterWritePost", ""}, {"FilterWritePre", ""}, {"FocusGained", ""}, {"FocusLost", ""}, {"FuncUndefined", ""}, {"GUIEnter", ""}, {"GUIFailed", ""}, {"InsertChange", ""}, {"InsertCharPre", ""}, {"InsertEnter", ""}, {"InsertLeave", ""}, {"InsertLeavePre", ""}, {"KeyInputPre", ""}, {"MenuPopup", ""}, {"ModeChanged", ""}, {"OptionSet", ""}, {"QuickFixCmdPost", ""}, {"QuickFixCmdPre", ""}, {"QuitPre", ""}, {"RemoteReply", ""}, {"SafeState", ""}, {"SafeStateAgain", ""}, {"SessionLoadPost", ""}, {"SessionLoadPre", ""}, {"SessionWritePost", ""}, {"ShellCmdPost", ""}, {"ShellFilterPost", ""}, {"SigUSR1", ""}, {"SourceCmd", ""}, {"SourcePost", ""}, {"SourcePre", ""}, {"SpellFileMissing", ""}, {"StdinReadPost", ""}, {"StdinReadPre", ""}, {"SwapExists", ""}, {"Syntax", ""}, {"TabClosed", ""}, {"TabClosedPre", ""}, {"TabEnter", ""}, {"TabLeave", ""}, {"TabNew", ""}, {"TermChanged", ""}, {"TerminalOpen", ""}, {"TerminalWinOpen", ""}, {"TermResponse", ""}, {"TermResponseAll", ""}, {"TextChanged", ""}, {"TextChangedI", ""}, {"TextChangedP", ""}, {"TextChangedT", ""}, {"TextPutPost", ""}, {"TextPutPre", ""}, {"TextYankPost", ""}, {"User", ""}, {"VimEnter", ""}, {"VimLeave", ""}, {"VimLeavePre", ""}, {"VimResized", ""}, {"VimResume", ""}, {"VimSuspend", ""}, {"WinClosed", ""}, {"WinEnter", ""}, {"WinLeave", ""}, {"WinNew", ""}, {"WinNewPre", ""}, {"WinResized", ""}, {"WinScrolled", ""},
}

// AutocmdEvents returns a caller-owned event_tab-order copy.
func AutocmdEvents() []AutocmdEvent { return append([]AutocmdEvent(nil), autocmdEvents...) }

// LookupAutocmdEvent looks up a Vim autocmd event by case-insensitive name.
func LookupAutocmdEvent(name string) (AutocmdEvent, bool) {
	for _, event := range autocmdEvents {
		if strings.EqualFold(event.Name, name) {
			return event, true
		}
	}
	return AutocmdEvent{}, false
}

// IsAutocmdEvent reports whether name matches a Vim built-in autocmd event.
func IsAutocmdEvent(name string) bool {
	_, ok := LookupAutocmdEvent(name)
	return ok
}

// IsKnownAutocmdEvent reports whether name matches a Vim or Neovim compat autocmd event.
func IsKnownAutocmdEvent(name string) bool {
	return IsAutocmdEvent(name) || IsNeovimCompatEvent(name)
}
