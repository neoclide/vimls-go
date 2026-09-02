# Neovim 独有且自带的函数、选项、变量和命令

> 对比对象：
> - Neovim：`/Users/chemzqm/lib/neovim` (v0.12.4)
> - Vim：`/Users/chemzqm/lib/vim` (v9.2.1015)
>
> 说明：下面列出的是 Neovim 有而 Vim 没有的、Neovim 官方源码/运行时中自带（不需要第三方插件）的条目。`help` 标签可用于在 Nvim 中查看详细官方文档。

## 一、Vimscript 内置函数（常规函数）

这些函数出现在 Neovim 的 `:h vimfn.txt` / `vimscript-functions` 中，Vim 没有同名内置函数。

| 函数 | 说明 |
|---|---|
| `api_info()` | 返回 Neovim API 元数据字典。 |
| `chanclose({id} [, {stream}])` | 关闭 channel；可指定 `"stdin"`, `"stdout"`, `"stderr"`, `"rpc"` 等流。 |
| `chansend({id}, {data})` | 向 channel 写入数据；对 job 写 stdin，对 stdio channel 写 stdout。 |
| `ctxget([{index}])` | 取得上下文栈中指定层级的 Neovim `context` 字典。 |
| `ctxpop()` | 弹出并恢复上下文栈顶的 `context`。 |
| `ctxpush([{types}])` | 把当前编辑器状态压入上下文栈。 |
| `ctxset({context} [, {index}])` | 设置上下文栈中某一层级的上下文。 |
| `ctxsize()` | 返回上下文栈大小。 |
| `dictwatcheradd({dict}, {pattern}, {callback})` | 为字典增加 watcher，修改时调用回调。 |
| `dictwatcherdel({dict}, {pattern}, {callback})` | 删除字典 watcher。 |
| `jobpid({job})` | 返回 job 的 PID。 |
| `jobresize({job}, {width}, {height})` | 调整带 pty 的 job 终端尺寸。 |
| `jobstart({cmd} [, {opts}])` | 启动异步 job；`rpc/pty/term` 等选项控制通信方式。 |
| `jobstop({id})` | 停止 job。 |
| `jobwait({jobs} [, {timeout}])` | 等待 job 及其 `on_exit` 完成。 |
| `menu_get({path} [, {modes}])` | 返回菜单字典列表。 |
| `msgpackdump({list} [, {type}])` | 把 Vimscript 对象序列化为 MessagePack。 |
| `msgpackparse({data})` | 把 MessagePack 数据解析为 Vimscript 对象。 |
| `prompt_appendbuf({buf}, {text})` | 在 prompt buffer 当前 prompt 前追加文本。 |
| `prompt_getinput({buf})` | 获取 prompt buffer 中用户当前输入。 |
| `reg_recorded()` | 返回最近录制的寄存器名。 |
| `rpcnotify({channel}, {event} [, {args}...])` | 向 channel 异步发送 RPC 通知。 |
| `rpcrequest({channel}, {method} [, {args}...])` | 向 channel 同步发送 RPC 请求并等待回复。 |
| `serverstart([{address}])` | 在指定地址开启 RPC server/socket。 |
| `serverstop({address})` | 关闭 RPC server/socket。 |
| `sockconnect({mode}, {address} [, {opts}])` | 连接本地 socket/named pipe 或 TCP 地址。 |
| `stdioopen({opts})` | 在 `--headless` 下把 stdin/stdout 打开为 channel。 |
| `stdpath({what})` | 返回 Neovim 标准路径（config/data/state/cache 等）。 |
| `wait({timeout}, {condition} [, {interval}])` | 轮询等待条件表达式/函数为真。 |

已弃用但仍为 Neovim 自带、Vim 没有的内置函数：

| 函数 | 说明 |
|---|---|
| `jobclose()` | `chanclose()` 的旧名称。 |
| `jobsend()` | `chansend()` 的旧名称。 |
| `rpcstart()` | 旧 RPC 启动方式；改用 `jobstart(..., {'rpc': v:true})`。 |
| `rpcstop()` | 旧 RPC 停止方式；改用 `jobstop()`/`chanclose()`。 |
| `termopen()` | 旧终端 job 启动方式；改用 `jobstart(..., {term: v:true})`。 |

> Neovim 源码中还有仅供测试/内部使用的 `test_write_list_log()`，没有公开帮助文档，一般不作为用户 API 使用。

## 二、Nvim API 函数（`nvim_*`，也可通过 RPC/Lua/部分 Vimscript 调用）

Vim 没有 `nvim_*` API 函数层。官方完整解释位于 `:h api.txt`。以下是 Neovim v0.12.4 自带、Vim 中没有的 API 函数完整名单。

### Buffer API

```text
nvim_buf_attach                    nvim_buf_call
nvim_buf_clear_highlight           nvim_buf_clear_namespace
nvim_buf_create_user_command       nvim_buf_del_extmark
nvim_buf_del_keymap                nvim_buf_del_mark
nvim_buf_del_user_command          nvim_buf_del_var
nvim_buf_delete                    nvim_buf_detach
nvim_buf_get_changedtick           nvim_buf_get_commands
nvim_buf_get_extmark_by_id         nvim_buf_get_extmarks
nvim_buf_get_keymap                nvim_buf_get_lines
nvim_buf_get_mark                  nvim_buf_get_name
nvim_buf_get_offset                nvim_buf_get_option
nvim_buf_get_text                  nvim_buf_get_var
nvim_buf_is_loaded                 nvim_buf_is_valid
nvim_buf_line_count                nvim_buf_set_extmark
nvim_buf_set_keymap                nvim_buf_set_lines
nvim_buf_set_mark                  nvim_buf_set_name
nvim_buf_set_option                nvim_buf_set_text
nvim_buf_set_var                   nvim_buf_set_virtual_text
```

### 通用/编辑器核心 API

```text
nvim_call_atomic                   nvim_call_dict_function
nvim_call_function                 nvim_chan_send
nvim_clear_autocmds                nvim_cmd
nvim_command                       nvim_command_output
nvim_create_augroup                nvim_create_autocmd
nvim_create_buf                    nvim_create_namespace
nvim_create_user_command           nvim_del_augroup_by_id
nvim_del_augroup_by_name           nvim_del_autocmd
nvim_del_current_line              nvim_del_keymap
nvim_del_mark                      nvim_del_user_command
nvim_del_var                       nvim_echo
nvim_eval                          nvim_eval_statusline
nvim_exec                          nvim_exec2
nvim_exec_autocmds                 nvim_exec_lua
nvim_execute_lua（deprecated）     nvim_feedkeys
nvim_get_all_options_info          nvim_get_api_info
nvim_get_autocmds                  nvim_get_chan_info
nvim_get_color_by_name             nvim_get_color_map
nvim_get_commands                  nvim_get_context
nvim_get_current_buf               nvim_get_current_line
nvim_get_current_tabpage           nvim_get_current_win
nvim_get_hl                        nvim_get_hl_by_id
nvim_get_hl_by_name                nvim_get_hl_id_by_name
nvim_get_hl_ns                     nvim_get_keymap
nvim_get_mark                      nvim_get_mode
nvim_get_namespaces                nvim_get_option
nvim_get_option_info               nvim_get_option_info2
nvim_get_option_value              nvim_get_proc
nvim_get_proc_children             nvim_get_runtime_file
nvim_get_var                       nvim_get_vvar
nvim_input                         nvim_input_mouse
nvim_list_bufs                     nvim_list_chans
nvim_list_runtime_paths            nvim_list_tabpages
nvim_list_uis                      nvim_list_wins
nvim_load_context                  nvim_open_tabpage
nvim_open_term                     nvim_open_win
nvim_parse_cmd                     nvim_parse_expression
nvim_paste                         nvim_put
nvim_replace_termcodes             nvim_select_popupmenu_item
nvim_set_client_info               nvim_set_current_buf
nvim_set_current_dir               nvim_set_current_line
nvim_set_current_tabpage           nvim_set_current_win
nvim_set_decoration_provider       nvim_set_hl
nvim_set_hl_ns                     nvim_set_hl_ns_fast
nvim_set_keymap                    nvim_set_option
nvim_set_option_value              nvim_set_var
nvim_set_vvar                      nvim_strwidth
```

### Tabpage API

```text
nvim_tabpage_del_var               nvim_tabpage_get_number
nvim_tabpage_get_var               nvim_tabpage_get_win
nvim_tabpage_is_valid              nvim_tabpage_list_wins
nvim_tabpage_set_var               nvim_tabpage_set_win
```

### UI API

```text
nvim_ui_attach                     nvim_ui_detach
nvim_ui_pum_set_bounds             nvim_ui_pum_set_height
nvim_ui_send                       nvim_ui_set_focus
nvim_ui_set_option                 nvim_ui_try_resize
nvim_ui_try_resize_grid
```

### Window API

```text
nvim_win_call                      nvim_win_close
nvim_win_del_var                   nvim_win_get_buf
nvim_win_get_config                nvim_win_get_cursor
nvim_win_get_height                nvim_win_get_number
nvim_win_get_option                nvim_win_get_position
nvim_win_get_tabpage               nvim_win_get_var
nvim_win_get_width                 nvim_win_hide
nvim_win_is_valid                  nvim_win_set_buf
nvim_win_set_config                nvim_win_set_cursor
nvim_win_set_height                nvim_win_set_hl_ns
nvim_win_set_option                nvim_win_set_var
nvim_win_set_width                 nvim_win_text_height
```

### 内部/私有 API（一般不要调用，但是 Neovim 自带）

```text
nvim__chan_set_detach              nvim__complete_set
nvim__exec_lua_fast                nvim__get_runtime
nvim__id                           nvim__id_array
nvim__id_dict                      nvim__id_float
nvim__inspect_cell                 nvim__invalidate_glyph_cache
nvim__ns_get                       nvim__ns_set
nvim__redraw                       nvim__stats
```

## 三、Neovim 独有选项

| 选项 | 类型/作用域 | 说明 |
|---|---|---|
| `'busy'` | number, buffer-local | 标记 buffer 忙碌状态；语义由拥有该 buffer 的插件定义。 |
| `'channel'` | number, buffer-local | 与 buffer 相连的 channel id；只读。 |
| `'inccommand'` | string, global | 在执行 `:substitute` 等命令时实时预览。 |
| `'mousescroll'` | string, global | 鼠标滚轮垂直/水平滚动行数与列数。默认 `"ver:3,hor:6"`。 |
| `'pumblend'` | number, global | 弹出菜单伪透明度，0 不透明、0-100。 |
| `'redrawdebug'` | string, global | 红描调试标志（如 `compositor`, `line`, `flush` 等）。 |
| `'scrollback'` | number, buffer-local | 终端 buffer 的最大回滚行数，默认 10000。 |
| `'shada'` | string, global | 替代 Vim 的 `'viminfo'`，控制 ShaDa 文件保存内容。 |
| `'shadafile'` | string, global | ShaDa 文件名；兼容 `'viminfofile'`。 |
| `'statuscolumn'` | string, window-local | 自定义折叠/符号/行号区域的内容，格式类似 `'statusline'`。 |
| `'termpastefilter'` | string, global | 粘贴到终端窗口时过滤的控制字符。默认 `"BS,HT,ESC,DEL"`。 |
| `'winbar'` | string, global/window-local | 窗口顶部栏的自定义格式。 |
| `'winblend'` | number, window-local | 浮窗伪透明度，0 不透明，0-100。 |
| `'winborder'` | string, global | 浮窗默认边框样式：`"single"`, `"double"`, `"rounded"`, `"shadow"`, `"solid"` 或自定义 8 字符。 |

## 四、Neovim 独有预定义变量

| 变量 | 说明 |
|---|---|
| `v:argf` | 启动时传入的文件参数（绝对路径，不含 `-u`、`--cmd`、`+cmd` 等选项）。 |
| `v:exitreason` | 当前退出原因：空、`"quit"` 或 `"restart"`。 |
| `v:lua` | Vimscript 表达式中调用 Lua 函数的前缀。 |
| `v:msgpack_types` | `msgpackparse()`/`msgpackdump()` 使用的 MessagePack 特殊类型字典。 |
| `v:relnum` | `'statuscolumn'` 表达式中当前绘制行的相对行号。 |
| `v:starttime` | Nvim 进程启动时间戳（纳秒）。 |
| `v:stderr` | 标准错误对应的 channel id（固定为 2）。 |
| `v:termrequest` | 内嵌终端中进程发来的最近 OSC/DCS/APC 控制序列，供 `TermRequest` 使用。 |
| `v:virtnum` | `'statuscolumn'` 表达式中当前绘制行的虚拟行号：实际行 0、包裹行正数、虚拟行负数。 |

## 五、Neovim 独有 Ex 命令

### 核心 Ex 命令

| 命令 | 说明 |
|---|---|
| `:checkhealth [{plugins}]` | 运行 Nvim/插件的健康检查。 |
| `:connect {address}` / `:connect! {address}` | 把当前 UI 改连到另一个 Nvim server。 |
| `:detach` | 分离当前 UI；Nvim server 继续在后台运行。 |
| `:fclose[!]` / `:fc[lose][!]` | 关闭 z-index 最高的浮窗；带 `!` 关闭全部浮窗。 |
| `:lsp enable\|disable\|restart\|stop ...` | 管理内置 LSP client。 |
| `:perlfile {file}` | 通过 provider 执行 Perl 脚本文件。 |
| `:restart [+cmd] [command]` | 重启 Nvim 并重新连接 UI。 |
| `:rsh[ada][!] [file]` | 从 ShaDa 文件读取信息。 |
| `:trust [++deny] [++remove] [file]` | 管理受信任文件，控制 `'exrc'`/`vim.secure.read()` 是否执行文件。 |
| `:wsh[ada][!] [file]` | 把信息写入 ShaDa 文件。 |

### Neovim 运行时自带的默认用户命令

| 命令 | 说明 |
|---|---|
| `:Inspect[!]` | 检查光标处的 extmark/syntax/treesitter 高亮信息；带 `!` 打印结构化结果。 |
| `:InspectTree` | 以树状方式查看当前 buffer 的 treesitter 语法树。 |
| `:EditQuery` | 打开 treesitter query 编辑器。 |
| `:UpdateRemotePlugins` | 刷新远程插件 manifest。 |
| `:Undotree` | 打开 Nvim 随附的 `nvim.undotree` 可选包后提供的撤销树。 |
| `:DiffTool {left} {right}` | 使用 `nvim.difftool` 可选包提供的差异工具。 |

> `:lua`、`:luado`、`:luafile` 在本次对比的 Vim 9.2.1015 源码中已经作为可选 `+lua` 接口存在，因此未列入“Vim 没有的命令”；但当前机器上安装的 Vim 未编译 `+lua`，实际不可用。Nvim 的 Lua 是默认且完整内置的。
