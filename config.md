# Vim 用户配置文件场景优化方案

本文描述 vimls-go 在 Vim 的 `vimrc`、`gvimrc`、`exrc`，Neovim 兼容场景下的 Vimscript `init.vim`，以及用户通过 `configFiles` 指定的拆分配置文件中可以提供的专属能力。本文只讨论 Vimscript 语义，不引入 Neovim-only API。

语义上限固定为 **Vim v9.2.1015**。所有判断都必须来自静态语法、工作区索引和固定版本元数据；语言服务器不能启动 Vim，也不能执行用户配置。

## 1. 目标与原则

配置文件和插件脚本的目的不同：

- 配置文件用于表达用户偏好，顶层 `g:` 变量、直接 `<Leader>` 映射和全局 `:set` 通常是合理的。
- 插件脚本需要避免污染全局状态，并应给用户保留覆盖配置和映射的机会。
- 配置文件经常被 `:source`，因此需要关注重复执行后的状态，而不只是首次加载。

配置文件模式应遵循以下原则：

1. **原生 Vim 错误优先**：能与 Vim v9.2.1015 的错误精确对应时使用 `vim/E...`；最佳实践使用 `vimls/...`。
2. **行为问题高于风格问题**：会导致重复回调、错误键位或失效配置的问题用 Warning；偏好类建议最多用 Hint。
3. **不提供可能改变语义的首选修复**：删除 guard、插入裸 `autocmd!`、将 `map` 改成 `noremap` 都不一定安全。
4. **Legacy 与 Vim9 分开处理**：不能把 `:function`、`:def` 或 Vim9 reload 规则混为一谈。

## 2. 已有前置能力：配置文件识别

配置文件识别已经完成，后续规则直接复用 `Server.IsConfigFile`，不再重复实现路径判断或 glob 匹配。

当前判定规则是：

1. `initializationOptions.configFiles` 中任一 pattern 匹配时视为配置文件（保留原始路径进行匹配，同时支持匹配符号链接目标路径）；显式 pattern 可以将 runtime 标准目录中的文件指定为配置文件；
2. 已知文件名 `.vimrc`、`vimrc`、`.gvimrc`、`gvimrc`、`.exrc`、`exrc`、`_vimrc`、`_gvimrc`，以及 Neovim 兼容名 `init.vim`、`ginit.vim`，默认视为配置文件；
3. 位于 `workspaceRoots` 下的文件，若相对于所属 root 位于 `plugin/`、`autoload/`、`ftplugin/`、`syntax/` 等标准 runtime 目录中，默认不是配置文件；其他 workspace 文件默认视为配置文件；
4. 位于 `runtimeRoots`（如 `$VIMRUNTIME`、外部插件目录等）下的文件默认不是配置文件；
5. 不属于任何 root 的独立文件默认视为配置文件（不扫描绝对路径中的非相关目录段）。

`configFiles` 只接受绝对路径或 `~/` 开头的 pattern，支持 `*`、`**` 和 `?`；非绝对路径（如 workspace-relative 或单独 basename 模式）会被直接忽略。仅在 Windows 平台上强制大小写不敏感匹配。

识别结果只表示文件用途，不改变 Legacy/Vim9 根解析器，也不应写入 AST。analysis 和 completion 在调用时接收现有布尔结果即可。本文后续只规划识别完成后的行为差异。

## 3. 先复用现有能力

以下能力已经是通用 Vimscript 支持的一部分，不应为配置文件另造实现：

- `:set`、`:setlocal`、`:setglobal` 的结构化解析；
- 已知选项名检查：表达式使用 E113，`:set` 系列使用 E518；
- 选项、autocmd 事件、mapping 特殊参数与键码、colorscheme 的上下文补全；
- mapping、autocmd、augroup 以及嵌入命令的语法节点；
- 静态函数、全局变量、autoload 与 `:source` 目标的工作区导航基础；
- 现有 `vimls/autocmd-group-not-cleared`、`vimls/recursive-map`、`vimls/set-vs-setlocal` 等风格诊断。

配置文件优化的重点应是**切换策略、降低误报、补充执行顺序信息**，而不是重复解析这些结构。

## 4. 配置模式下的诊断策略

### 4.1 应禁用或收窄的插件式规则

| 现有规则 | 配置文件中的策略 | 原因 |
| --- | --- | --- |
| `vimls/configuration-overwrite` | 禁用 | 配置文件本来就负责设置用户值。 |
| `vimls/global-internal-state` | 禁用 | 用户维护的 `g:` 状态不等于插件内部状态。 |
| `vimls/direct-user-keymap` | 禁用 | 直接定义 `<Leader>` 映射正是用户配置的常见用途。 |
| `vimls/mapping-without-unique` | 禁用 | `<unique>` 会在已有映射时产生 E226/E227，并会破坏重复 source。 |
| `vimls/set-vs-setlocal` | 顶层禁用；在 FileType 等 buffer/window 上下文中保留 | vimrc 顶层 `:set` 用于建立全局默认值；autocmd/ftplugin 中通常才需要 `:setlocal`。 |
| `vimls/recursive-map` | 降为 Hint，并保留 `<Plug>`、`<script>` 等例外 | 递归 mapping 可能是有意组合已有映射，并非错误。 |

以下通用规则仍有价值：

- `vimls/normal-without-bang`；
- `vimls/function-without-abort`（仅 Legacy `:function`）；
- `vimls/mapping-script-local-reference`；
- 隐式大小写和正则 magic；
- 可静态证明的 Vim 原生语法、名称、参数和类型错误。

### 4.2 不应新增“function 必须带 !”规则

配置文件中无 bang 的顶层函数**不应因为可重复 source 而被警告**。

Vim v9.2.1015 明确规定：同一脚本再次被 source 时，该脚本先前定义的 `:function` 会被静默替换；`:command` 也有同样的来源例外。因此下面两段本身都是可重载的：

```vim
function MyHelper() abort
endfunction

command MyCommand echo 'ok'
```

无条件建议改成 `function!` 或 `command!` 反而可能在首次加载时覆盖其他脚本创建的同名全局项。vimls 可以继续报告同一次分析中可静态证明的重复定义，但不能把“没有 `!`”当作配置文件错误。

Vim9 的 `:def` 还受 `vim9-reload` 的清理规则约束，不能套用 Legacy `:function` 的 lint。

### 4.3 augroup 重载安全

这是最高价值的配置专属检查。

当一个命名 augroup 中定义了持久 autocmd，且在这些定义之前没有确定清除同组旧 autocmd 时，报告：

- code：复用 `vimls/autocmd-group-not-cleared`；
- 默认级别：Warning；
- 诊断位置：augroup 名称；
- related information：第一条持久 autocmd。

典型安全形式：

```vim
augroup vimrc_files
  autocmd!
  autocmd BufReadPost * checktime
augroup END
```

检查必须理解以下边界：

- 空组、只查询 autocmd 的组、没有定义的组不报告；
- `autocmd!` 可以带 event 和 pattern，定向清除也应计入，但只能证明其实际覆盖的定义；
- `autocmd! Group Event Pattern` 与 `autocmd Group Event Pattern ...` 这种显式组写法也应识别；
- `++once` 只保证注册项触发一次，并不阻止每次 source 再注册一项，不能自动视为安全；
- 条件分支中的清除只有在静态证明所有到达定义的路径都经过它时才算有效；
- `execute()` 生成的 autocmd 或清除保持 unknown；
- `augroup! name` 是删除组，不等价于组内 `autocmd!`，也可能影响其他脚本。

不应提供“插入裸 `autocmd!`”的自动修复，因为同名组可能由编辑器状态、未索引脚本或其他插件共享；静态索引无法证明删除操作安全。诊断说明中可以展示推荐结构，由用户手动确认。

### 4.4 插件式 loaded guard

对 `IsConfigFile` 判定为配置文件的文件中的以下静态模式给出 Hint：

```vim
if exists('g:loaded_my_vimrc')
  finish
endif
let g:loaded_my_vimrc = 1
```

建议诊断：`vimls/config-loaded-guard`，提示该 guard 会让后续 `:source` 跳过文件剩余内容，编辑后的配置可能无法生效。

只匹配满足以下条件的模式：

- 位于文件顶层并控制一个到达文件末尾前的 `:finish`；
- `exists()` 参数是静态的 `g:loaded_*` 变量；
- 同一文件存在对应标记赋值，或模式足够明确。

不能误报：

- `exists('*Func')`、`exists(':Command')`、`exists('+option')`、`exists('##Event')`；
- `has('feature')` 等能力或平台检测；
- `if !exists(...)` 包裹的默认值初始化；
- 函数内部的 `finish` 或动态表达式；
- runtime/plugin 文件；
- 用户显式选择的 `vim9script noclear` 单次加载设计。

该提示不提供自动删除修复。对于 Vim9 脚本还要注意：默认 reload 会先清理脚本局部函数和变量；若随后被 guard 提前 `finish`，脚本可能只剩半初始化状态。此时应优先提示删除配置文件 guard，而不是盲目添加 `noclear`。

## 5. Mapping 专属检查

### 5.1 确定的重复或覆盖

建议新增 `vimls/duplicate-mapping`，只报告静态确定的后一个定义，并通过 related information 指向前一个定义。

比较键至少包含：

- 有效 mode 集合，而不只是命令文本；例如 `map` 覆盖 Normal/Visual/Select/Operator-pending，`map!` 覆盖 Insert/Command-line；
- 规范化后的字面量 LHS；
- global 与 `<buffer>` scope；
- mapping 与 abbreviation 的类别。

还应处理：

- `unmap`、`mapclear` 会终止相应的前序定义；
- `vmap` 与 `xmap`/`smap` 只有 mode 交集部分冲突；
- `<Leader>`、`<LocalLeader>` 仅在前序值可静态确定时展开，否则只比较相同源码拼写；
- 互斥条件分支不判冲突；不能证明互斥时最多给 Information；
- 不比较没有已知 source 顺序的两个独立文件；
- runtimepath 中可能存在的动态 mapping 不视为确定冲突；
- 相同 LHS 但无 mode 交集或 local/global scope 不同，不报告。

### 5.2 mapleader 定义顺序

`g:mapleader` 和 `g:maplocalleader` 在 mapping **定义时**展开，之后修改不会更新已有 mapping。因此这是比泛化“重复 mapping”更可靠的配置错误。

当同一条可证明的执行路径上，先出现 `<Leader>`/`<LocalLeader>` mapping，后出现对应 leader 的静态赋值时，建议报告 `vimls/config-mapleader-order` Warning，并指向较早的 mapping。

动态赋值、条件互斥或来自未知 source 顺序的赋值保持 unknown。即使在同一顶层直线命令序列中，移动赋值也可能改变用户有意设置的多套 leader，因此默认只给诊断，不提供自动移动。

### 5.3 递归 mapping

`map`、`nmap`、`imap` 并不自动等于 bug。配置模式下只给 Hint，并跳过：

- RHS 包含 `<Plug>`；
- 使用 `<script>`；
- 明显有意复用另一条 mapping；
- 不能静态解析的 mapping。

“改成 noremap”不应作为 preferred quick fix；该修改可能让依赖二次展开的 RHS 失效。

## 6. 选项检查

### 6.1 名称与语法

继续复用现有 Vim 原生诊断：

- `&missing`、`&g:missing`、`&l:missing` 使用 E113；
- `:set missing`、`:setlocal missing`、`:setglobal missing` 使用 E518。

检查前必须正确处理短名称，以及 `no`、`inv`、`+=`、`-=`、`^=`、`?`、`&`、`<` 等形式。未知 `t_` 终端选项依赖终端和构建，保持 unknown。

### 6.2 作用域建议

- vimrc 顶层 `:set` 是设置全局默认值的正常方式，不报告 `set-vs-setlocal`；
- FileType/BufRead 等 buffer 或 window 定向 autocmd 的静态命令体中，对有局部值的选项继续建议 `:setlocal`；
- 纯全局选项不建议 `:setlocal`；
- `:set` 对 global-local 选项会同时影响全局值和当前局部值，hover 应明确展示这一点。

### 6.3 值检查

优先补全，不急于诊断。只有当固定值集合和错误语义在 Vim v9.2.1015 中可精确证明时，才检查枚举值、布尔形式或范围；路径、表达式、逗号列表、平台相关值和 feature 相关值保持 unknown。

不要创建一个笼统的“invalid option value”规则来替代不同的 Vim 原生错误。

## 7. 补全、Hover 与导航

配置模式只调整相关性，不改变语义结果：

### P0：排序优化

在对应语法位置提高以下候选：

- `:set` 的选项名、运算符和固定值；
- autocmd event、group、`++once`、`++nested`；
- mapping 的 `<silent>`、`<expr>`、`<buffer>`、`<Leader>`、`<LocalLeader>`、`<Cmd>`、`<CR>` 等；
- `colorscheme` 名称；
- 配置文件中已出现的 `g:` 插件配置变量。

排序必须服从语法上下文。例如在普通函数表达式中不能因为文件是 vimrc 就压低局部变量；mapping 的普通 RHS 仍保持 opaque，除非是已支持的 `<expr>` 或可安全识别的 `<Cmd>...<CR>` 命令区间。

### P1：模板补全

为 snippet-capable 客户端提供：

- 带 `autocmd!` 和 `augroup END` 的 augroup 模板；
- Legacy `function ... abort` 与 Vim9 `def ... enddef` 模板；
- 常用 `nnoremap <silent> <Leader>...` 骨架。

模板不应默认加入 `function!`、`command!` 或 `<unique>`。

### P1：静态回调导航

在可解析区域支持 definition/references/hover：

- `autocmd ... call Func()` 或 autocmd block 中的函数调用；
- `<expr>` mapping RHS；
- `<Cmd>call Func()<CR>` 等边界明确的 mapping RHS；
- 选项值中已知为回调函数名的选项，如 `completefunc`、`omnifunc`、`operatorfunc`、`tagfunc`。

动态字符串、传统 `:` mapping 中无法可靠确定的按键流和 `execute()` 保持无结果。

### P2：文件与包补全

补充静态 `:source`、`:runtime`、`:packadd` 的路径或包名补全、document link 和 definition。解析应尊重当前文件目录、workspace roots、runtimepath 顺序和字面量转义，不访问未配置的任意目录。

静态 source 图还可用于：

- 检测确定的自 source 或 source cycle；
- 在执行顺序已知时做跨拆分文件的 mapping/leader 检查。

配置文件角色仍由现有 `IsConfigFile` 判定；source 图不再承担文件识别职责。

## 8. Code Action 安全边界

可以自动执行的修复必须是局部且语义唯一的，例如：

- 修正唯一匹配的选项名拼写；
- 补全缺失的结构结束命令。

以下操作默认只给说明，不提供 preferred quick fix：

- 给函数或用户命令添加 `!`；
- 删除 loaded guard；
- 移动 `mapleader`/`maplocalleader` 赋值；
- 插入裸 `autocmd!`；
- 把递归 mapping 改成 noremap；
- 给 mapping 添加 `<unique>`；
- 将顶层 `:set` 改成 `:setlocal`。

## 9. 实施优先级

### P0：先解决误报和确定行为问题

1. 在 analysis/completion 调用边界传入现有 `IsConfigFile` 结果，不修改 AST，也不再实现识别逻辑。
2. 应用第 4.1 节的配置诊断策略矩阵。
3. 完善 `autocmd-group-not-cleared` 的组、定向清除、条件路径和 `++once` 边界。
4. 增加 `mapleader`/`maplocalleader` 顺序诊断。
5. 保持 E113/E518 与选项元数据的 pinned-version 行为。
6. 增加配置模式补全排序测试。

### P1：提高编辑体验

1. 确定的同文件重复 mapping 检查。
2. loaded guard Hint，以及 Vim9 reload 组合检查。
3. augroup、函数和 mapping snippet。
4. autocmd、`<expr>`/`<Cmd>` mapping、回调选项中的静态导航。

### P2：需要完整索引证据后再做

1. 静态 source 图与 source cycle 检查。
2. 有确定 source 顺序时的跨文件 mapping 冲突。
3. `:source`、`:runtime`、`:packadd` 补全和导航扩展。
4. 经官方测试逐项确认的固定选项值诊断。

## 10. 测试要求

每条配置专属规则至少应覆盖：

- Legacy 与 Vim9 的适用或明确排除；
- 标准 vimrc、显式 `configFiles`、runtime/plugin 文件三种角色；
- 首次加载与重复 source 的预期差异；
- 条件块、续行、`|` 命令链和嵌入 autocmd body；
- 动态 `execute()` 的保守行为；
- CRLF、Unicode LHS 和 LSP position encoding；
- diagnostic disabled/override 与 quick-fix stale-version 校验。

建议保留以下官方依据（均以 Vim v9.2.1015 为准）：

- `runtime/doc/userfunc.txt`：`:function`、E122 以及同脚本再次 source 的替换例外；
- `runtime/doc/map.txt`：`:command`/E174、`:map-commands`、`:map-<unique>`、`:map-<buffer>`、`mapleader`；
- `runtime/doc/autocmd.txt`：`:autocmd!`、`:augroup` 和 vimrc 重复 source 示例；
- `runtime/doc/options.txt`：`:set`、`:setlocal`、`:setglobal`、`local-options`、`global-local`；
- `runtime/doc/vim9.txt`：`vim9-reload` 与 `vim9script noclear`；
- `src/testdir/test_options.vim`、`test_let.vim`、`test_usercommands.vim` 及相关 Vim9 tests。

## 11. 非目标

- 不执行配置文件来计算真实 mapping、option 或 autocmd 状态；
- 不把 Neovim Lua API 或 Neovim-only 行为当作 Vimscript 通用规则；
- 不对未确定加载顺序的所有 workspace/runtimepath 文件做全局冲突猜测；
- 不因为配置文件包含跨 dialect 构造就额外报错；
- 不提供通用格式重写或大范围“最佳实践”迁移。
