package vimdata

import "strings"

// neovimCompatFunctions are Neovim API and channel/rpc functions that
// vim-language-server accepted from its builtin documentation set. They are
// intentionally kept out of BuiltinFunctions() and LookupFunction so this
// server does not add Neovim completion, hover, signature help, or type
// checking yet. The IsNeovimCompat* predicates are used only to suppress
// unknown-name diagnostics.
var neovimCompatFunctions = []BuiltinFunction{
	{Name: "api_info"},
	{Name: "chanclose"},
	{Name: "chansend"},
	{Name: "ctxget"},
	{Name: "ctxpop"},
	{Name: "ctxpush"},
	{Name: "ctxset"},
	{Name: "ctxsize"},
	{Name: "dictwatcheradd"},
	{Name: "dictwatcherdel"},
	{Name: "jobpid"},
	{Name: "jobresize"},
	{Name: "jobstart"},
	{Name: "jobstop"},
	{Name: "jobwait"},
	{Name: "msgpackdump"},
	{Name: "msgpackparse"},
	{Name: "nvim__buf_redraw_range"},
	{Name: "nvim__buf_set_luahl"},
	{Name: "nvim__buf_stats"},
	{Name: "nvim__id"},
	{Name: "nvim__id_array"},
	{Name: "nvim__id_dictionary"},
	{Name: "nvim__id_float"},
	{Name: "nvim__inspect_cell"},
	{Name: "nvim__put_attr"},
	{Name: "nvim__stats"},
	{Name: "nvim_buf_add_highlight"},
	{Name: "nvim_buf_attach"},
	{Name: "nvim_buf_clear_namespace"},
	{Name: "nvim_buf_del_extmark"},
	{Name: "nvim_buf_del_keymap"},
	{Name: "nvim_buf_del_var"},
	{Name: "nvim_buf_detach"},
	{Name: "nvim_buf_get_changedtick"},
	{Name: "nvim_buf_get_commands"},
	{Name: "nvim_buf_get_extmark_by_id"},
	{Name: "nvim_buf_get_extmarks"},
	{Name: "nvim_buf_get_keymap"},
	{Name: "nvim_buf_get_lines"},
	{Name: "nvim_buf_get_mark"},
	{Name: "nvim_buf_get_name"},
	{Name: "nvim_buf_get_offset"},
	{Name: "nvim_buf_get_option"},
	{Name: "nvim_buf_get_var"},
	{Name: "nvim_buf_get_virtual_text"},
	{Name: "nvim_buf_is_loaded"},
	{Name: "nvim_buf_is_valid"},
	{Name: "nvim_buf_line_count"},
	{Name: "nvim_buf_set_extmark"},
	{Name: "nvim_buf_set_keymap"},
	{Name: "nvim_buf_set_lines"},
	{Name: "nvim_buf_set_name"},
	{Name: "nvim_buf_set_option"},
	{Name: "nvim_buf_set_var"},
	{Name: "nvim_buf_set_virtual_text"},
	{Name: "nvim_call_atomic"},
	{Name: "nvim_call_dict_function"},
	{Name: "nvim_call_function"},
	{Name: "nvim_command"},
	{Name: "nvim_create_buf"},
	{Name: "nvim_create_namespace"},
	{Name: "nvim_del_current_line"},
	{Name: "nvim_del_keymap"},
	{Name: "nvim_del_var"},
	{Name: "nvim_err_write"},
	{Name: "nvim_err_writeln"},
	{Name: "nvim_eval"},
	{Name: "nvim_exec"},
	{Name: "nvim_exec_lua"},
	{Name: "nvim_feedkeys"},
	{Name: "nvim_get_api_info"},
	{Name: "nvim_get_chan_info"},
	{Name: "nvim_get_color_by_name"},
	{Name: "nvim_get_color_map"},
	{Name: "nvim_get_commands"},
	{Name: "nvim_get_context"},
	{Name: "nvim_get_current_buf"},
	{Name: "nvim_get_current_line"},
	{Name: "nvim_get_current_tabpage"},
	{Name: "nvim_get_current_win"},
	{Name: "nvim_get_hl_by_id"},
	{Name: "nvim_get_hl_by_name"},
	{Name: "nvim_get_hl_id_by_name"},
	{Name: "nvim_get_keymap"},
	{Name: "nvim_get_mode"},
	{Name: "nvim_get_namespaces"},
	{Name: "nvim_get_option"},
	{Name: "nvim_get_proc"},
	{Name: "nvim_get_proc_children"},
	{Name: "nvim_get_var"},
	{Name: "nvim_get_vvar"},
	{Name: "nvim_input"},
	{Name: "nvim_input_mouse"},
	{Name: "nvim_list_bufs"},
	{Name: "nvim_list_chans"},
	{Name: "nvim_list_runtime_paths"},
	{Name: "nvim_list_tabpages"},
	{Name: "nvim_list_uis"},
	{Name: "nvim_list_wins"},
	{Name: "nvim_load_context"},
	{Name: "nvim_open_win"},
	{Name: "nvim_out_write"},
	{Name: "nvim_parse_expression"},
	{Name: "nvim_paste"},
	{Name: "nvim_put"},
	{Name: "nvim_replace_termcodes"},
	{Name: "nvim_select_popupmenu_item"},
	{Name: "nvim_set_client_info"},
	{Name: "nvim_set_current_buf"},
	{Name: "nvim_set_current_dir"},
	{Name: "nvim_set_current_line"},
	{Name: "nvim_set_current_tabpage"},
	{Name: "nvim_set_current_win"},
	{Name: "nvim_set_keymap"},
	{Name: "nvim_set_option"},
	{Name: "nvim_set_var"},
	{Name: "nvim_set_vvar"},
	{Name: "nvim_strwidth"},
	{Name: "nvim_subscribe"},
	{Name: "nvim_tabpage_del_var"},
	{Name: "nvim_tabpage_get_number"},
	{Name: "nvim_tabpage_get_var"},
	{Name: "nvim_tabpage_get_win"},
	{Name: "nvim_tabpage_is_valid"},
	{Name: "nvim_tabpage_list_wins"},
	{Name: "nvim_tabpage_set_var"},
	{Name: "nvim_ui_attach"},
	{Name: "nvim_ui_detach"},
	{Name: "nvim_ui_pum_set_height"},
	{Name: "nvim_ui_set_option"},
	{Name: "nvim_ui_try_resize"},
	{Name: "nvim_ui_try_resize_grid"},
	{Name: "nvim_unsubscribe"},
	{Name: "nvim_win_close"},
	{Name: "nvim_win_del_var"},
	{Name: "nvim_win_get_buf"},
	{Name: "nvim_win_get_config"},
	{Name: "nvim_win_get_cursor"},
	{Name: "nvim_win_get_height"},
	{Name: "nvim_win_get_number"},
	{Name: "nvim_win_get_option"},
	{Name: "nvim_win_get_position"},
	{Name: "nvim_win_get_tabpage"},
	{Name: "nvim_win_get_var"},
	{Name: "nvim_win_get_width"},
	{Name: "nvim_win_is_valid"},
	{Name: "nvim_win_set_buf"},
	{Name: "nvim_win_set_config"},
	{Name: "nvim_win_set_cursor"},
	{Name: "nvim_win_set_height"},
	{Name: "nvim_win_set_option"},
	{Name: "nvim_win_set_var"},
	{Name: "nvim_win_set_width"},
	{Name: "prompt_addtext"},
	{Name: "rpcnotify"},
	{Name: "rpcrequest"},
	{Name: "sockconnect"},
	{Name: "stdioopen"},
	{Name: "stdpath"},
	{Name: "wait"},
}

// neovimCompatOptions are Neovim-only options present in vim-language-server's
// builtin docs. They are recognized for diagnostics without enabling Neovim
// option completion.
var neovimCompatOptions = []Option{
	{Name: "channel"},
	{Name: "inccommand"},
	{Name: "pumblend"},
	{Name: "redrawdebug"},
	{Name: "scrollback"},
	{Name: "shada"},
	{Name: "shadafile"},
	{Name: "winblend"},
}

// neovimCompatVariables are Neovim-only v: variables accepted by
// vim-language-server's docs.
var neovimCompatVariables = []Variable{
	{Name: "v:lua", Type: "any"},
	{Name: "v:msgpack_types", Type: "dict<any>"},
	{Name: "v:stderr", Type: "channel"},
}

// neovimCompatCommands are Neovim-only Ex commands accepted by
// vim-language-server's docs.
var neovimCompatCommands = []Command{
	{Name: "rshada", Flags: AllowBang | AllowBar | FileArgument},
	{Name: "wshada", Flags: AllowBang | AllowBar | FileArgument},
}

func IsNeovimCompatFunction(name string) bool {
	_, ok := lookupNeovimCompatFunction(name)
	return ok
}

func IsNeovimCompatOption(name string) bool {
	name = strings.TrimPrefix(name, "&")
	if strings.HasPrefix(name, "g:") || strings.HasPrefix(name, "l:") {
		name = name[2:]
	}
	_, ok := lookupNeovimCompatOption(name)
	return ok
}

func IsNeovimCompatVariable(name string) bool {
	_, ok := lookupNeovimCompatVariable(name)
	return ok
}

func IsNeovimCompatCommand(name string) bool {
	_, ok := lookupNeovimCompatCommand(name)
	return ok
}

func lookupNeovimCompatFunction(name string) (BuiltinFunction, bool) {
	for _, function := range neovimCompatFunctions {
		if function.Name == name {
			return function, true
		}
	}
	return BuiltinFunction{}, false
}

func lookupNeovimCompatOption(name string) (Option, bool) {
	for _, option := range neovimCompatOptions {
		if option.Name == name {
			return option, true
		}
	}
	return Option{}, false
}

func lookupNeovimCompatVariable(name string) (Variable, bool) {
	for _, variable := range neovimCompatVariables {
		if variable.Name == name {
			return variable, true
		}
	}
	return Variable{}, false
}

func lookupNeovimCompatCommand(name string) (Command, bool) {
	for _, command := range neovimCompatCommands {
		if len(name) >= 3 && strings.HasPrefix(command.Name, name) {
			return command, true
		}
	}
	return Command{}, false
}
