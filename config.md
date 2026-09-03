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

检查前必须正确处理短名称，以及 `no`、`inv`、`+=`、`-=`、`^=`、`?`、`&`、`<` 等形式。未知 `t_` 终端选项依赖终端和构建，不做名称或值诊断。

### 6.2 作用域建议

- vimrc 顶层 `:set` 是设置全局默认值的正常方式，不报告 `set-vs-setlocal`；
- FileType/BufRead 等 buffer 或 window 定向 autocmd 的静态命令体中，对有局部值的选项继续建议 `:setlocal`；
- 纯全局选项不建议 `:setlocal`；
- `:set` 对 global-local 选项会同时影响全局值和当前局部值，hover 应明确展示这一点。

### 6.3 值检查

选项值诊断必须在**完整迁移 Vim v9.2.1015 的每个选项定义之后**启用，不能继续扩充
手写的 `fixedOptionValues` 并把补全候选误当成合法值全集。迁移与检查分为以下阶段。

#### 6.3.1 完整定义的来源与内容

现有生成器已经从 `src/optiondefs.h` 迁移 469 个普通选项与 93 个 `t_` 终端选项，
但每项仅保留名称、短名、粗粒度类型、作用域和帮助文本。下一步仍在
`tools/genmetadata` 与 `internal/vimdata` 内扩展现有实现，不新建独立包；生成器继续通过
`git show v9.2.1015:<path>` 读取固定提交，而不依赖 Vim checkout 的当前 HEAD。

每个普通选项的生成定义必须保留：

- `fullname`、`shortname`，以及 `P_BOOL`/`P_NUM`/`P_STRING`；
- `option.h` 中所有声明性 flags，而不只保留当前用到的 `P_COMMA`、`P_FLAGLIST` 等；
- `var`、`indir` 及其条件编译分支，用于区分 global、window、buffer、global-local，并推导该
  option 可用所需的完整编译条件；
- `opt_did_set_cb` 与 `opt_expand_cb` 的原始函数符号；`NULL` 也必须显式保留；
- Vi/Vim 两套默认值的原始常量或字面量，以及影响它们的平台/feature 条件；不能把条件默认值
  折叠成生成机器上的一个值；
- 帮助文档来源，以及定义和校验回调所在的 Vim 源文件与符号。

`t_` 终端选项也保留在完整清单中，但其存在和值由终端、构建和运行环境决定，不启用值校验。
语言服务器不能因为固定 tag 的 `p_term()` 清单中没有某个 `t_XX` 就报告错误。

迁移时的权威文件分工固定为：`optiondefs.h` 提供每项定义，`option.h` 提供 flags 语义，
`optionstr.c` 提供字符串 callback 与补全数组，`option.c` 提供数值 callback 和通用 bounds，
`errors.h` 提供错误 code，`src/testdir/test_options.vim` 及各 option 所属的专项测试提供行为证据。
帮助文本只能补充解释，不能覆盖这些源码与测试结论。

生成器不能继续用只截取前三列的正则表达式承担完整迁移。应使用一个有界的 C 初始化器扫描器：
识别 `options[]` 的条目边界、字符串/注释/括号，并记录 `#if`/`#else`/`#endif` 条件栈；不需要实现
通用 C 解析器。任何无法解析的条目、flag、callback、default 或条件分支都使生成失败，不能静默
降级成空字段。

#### 6.3.2 补全数据与校验规则分离

`opt_expand_cb` 给出的只是命令行补全候选，不一定是合法值全集；`opt_did_set_cb` 还可能只执行
副作用而不拒绝值。因此生成数据必须把以下内容分开：

1. `CompletionValues`：从 `expand_set_*` 及其固定数组迁移，用于补全；替代当前手写
   `fixedOptionValues`，并保持已有候选顺序。
2. `Validation`：仅在能静态证明错误时描述一个不执行 Vim C 代码的纯校验规则；至少区分
   exact enum、逗号列表、单字符 flag list、数值范围和结构化语法。不能静态校验的项不生成规则。
3. `MigrationStatus`：这是生成器的迁移完整性分类，不是值校验结果。每个选项必须明确标记
   `validator`、`skip-dynamic`、`skip-side-effect`、`skip-runtime`、`skip-build` 或迁移期间的
   `not-yet-ported`，并带 Vim callback 来源。

“完整”表示 562 个选项都有来源完整的定义和明确分类，不表示强行给 562 个选项制造错误。
生成器应拒绝未分类的新 option 或 callback，这样 Vim tag 升级时不会静默漏掉新语义。迁移阶段
可以使用 `not-yet-ported` 暴露进度，但开始启用值诊断前必须清零；最终没有 validator 的项必须由
脚本根据源代码明确归类为 skip-dynamic、skip-runtime、skip-build 或 skip-side-effect，而不是人工漏项。

建议直接扩展现有 `vimdata.Option`，而不是再加一层 service/registry。字段关系应能表达下面的
等价信息（名称可在实现时按现有风格调整）：

```go
type Option struct {
    Name, ShortName string
    Type             OptionType
    Scope            OptionScope
    Flags            []string
    Variants         []OptionVariant
    CompletionValues []string
    AvailableWhen    string
    RequiredFeatures []string
    DefinitionSource string
    DefinitionLine   int
    // Added when a pure validator is implemented:
    Validation       OptionValidation
    Documentation, DocumentationSource string
}

type OptionVariant struct {
    Condition                  string
    Variable, Indirect         string
    DidSetCallback, ExpandCallback string
    ViDefault, VimDefault      string
}
```

同一 option 在不同 `#if` 分支中的 storage、callback 或 default 不得相互覆盖；无条件且完全相同的
分支才可在生成时合并。由于结构中包含 slice，现有 `Options()` 的“调用者拥有返回值”合同也必须
升级为深拷贝测试，不能把 generated backing arrays 暴露给调用方。

校验规则至少需要表达：允许值或 flag、是否允许空值、列表分隔方式、是否允许连续逗号和重复项、
静态上下界、对应 Vim 错误 code，以及规则成立所需的平台/feature 条件。复杂 callback 不应被
翻译成一个万能正则；它应保留 callback 身份，并在专属纯函数完成前保持 `not-yet-ported`。

迁移脚本负责把所有 option 与 callback 映射为有限的规则类型：直接读取固定 value arrays、flag
常量、通用 `did_set_opt_strings()`/`did_set_option_listflag()` 调用、静态数值比较和返回的 Vim
错误符号；同一种结构只生成一个规则实现和不同参数。无法化约为这些共享形态的 callback 映射到
按语法类别实现的 custom rule；custom rule 的数量由不同语法决定，不按 option 数量复制。
脚本必须对 callback body 的未消费判断、返回分支或新调用报错，避免 Vim 升级后沿用不完整规则。

#### 6.3.3 只报告可证明错误的边界

值校验入口只返回“一个确定的 Vim diagnostic”或“不返回 diagnostic”，不引入 valid/invalid/unknown
状态。只有以下条件同时成立才返回 diagnostic：

- 选项、运算符和值都被结构化解析，且值是无需执行代码即可确定的字面量；
- 对该赋值形式存在已迁移的纯校验规则；
- 相同错误在目标 tag 的所有相关平台/feature 分支上成立；
- 能使用 Vim 实际返回的原生错误 code 和对应值 span。

首批接入 `:set`、`:setlocal`、`:setglobal` 的完整替换运算符 `=`/`:`，以及
`:let &option = literal` 和 Vim9 的 `&option = literal`。字符串表达式必须先按各自语法解码；
`:set` 的反斜杠转义与 Vim 表达式字符串不是同一种规则。

`+=`、`^=`、`-=` 的最终结果依赖旧值。只有新增片段自身无论旧值为何都非法时才能报告；重复项、
最终长度、最终范围等依赖状态的情况不运行对应校验。变量、函数调用、字符串拼接、插值、
`execute()`、环境展开和无法静态求值的表达式直接跳过，不产生值诊断。

以下选项默认不启用值校验：

- 路径、shell 命令、编码、locale、runtime 名称或外部程序相关值；
- 校验依赖当前 buffer/window、终端能力、屏幕尺寸或其他 option 当前值；
- 某些构建中 option 不可用，或不同平台/feature 分支的接受集合不同；
- callback 会触发 autocommand、加载 runtime 文件或包含无法隔离的副作用，而错误条件尚未被
  纯函数完整迁移。

语言服务器只移植**声明性定义与纯校验语义**，不移植或模拟 callback 的副作用，也绝不执行
用户 Vimscript。

#### 6.3.4 错误码与启用顺序

不能新增笼统的 `vimls/invalid-option-value`。每条规则使用固定 Vim 实际产生的错误，例如
`vim/E474`（Invalid argument）或 `vim/E487`（Argument must be positive）；若同一 callback 在
不同失败分支返回不同 code，规则也必须保留该区别。无法确定具体分支时不报告 Error。

实现顺序固定为：

1. 先完成全部 option 定义、callback/expander inventory、默认值与条件分支迁移，此阶段不新增诊断；
2. 用生成的 completion 数据替换手写表，并证明候选无回退；
3. 接入无状态的 exact enum 与单值规则；
4. 接入逗号列表、flag list 及其 `P_ONECOMMA`/`P_NODUP`/`P_COLON` 结构规则；
5. 接入不依赖运行状态的数值集合与上下界；
6. 最后逐个迁移 `did_set_*` 中的结构化纯校验；动态或副作用 callback 不生成 validator。

每一批只启用已经由迁移脚本从 pinned Vim 源码完整提取、并由该规则类型的代表性正反例验证过的
规则。不得因为 callback 名称、帮助文本措辞或 completion 数组看起来像枚举就推断错误语义。

#### 6.3.5 测试与验收

完整定义迁移的生成器门槛：

- 562 个 option 全部可追溯，名称/短名唯一，type/scope/flags/callback/default/条件分支均不丢失；
- optiondefs 中出现的每个 `did_set_*`、`expand_set_*` 和 `NULL` 都被 inventory 覆盖；
- 每个 option 都有明确 `MigrationStatus`，tag 升级新增未分类项时 `metadata-check` 失败；
- 两次生成 byte-for-byte 一致，且不受 checkout 当前分支、平台或本机 feature 集影响；
- 生成的 completion 数据覆盖 66 个具有静态数组或 flag 字符串的 option，原有 30 项候选及顺序
  保持不变；动态 callback 不生成静态候选。

测试不按 562 个 option 展开，也不按错误 code 复制官方测试。测试单位是**不同的迁移和校验情况**：

- C initializer 的普通项、条件分支、不可用分支、两套 default 与 `p_term()`；
- 无 callback、纯副作用 callback、固定 enum、逗号列表、单字符 flags、静态 number bounds、
  custom structured grammar，以及 dynamic/runtime/build-dependent 跳过分类；
- 空值、重复项、连续分隔符、非法 flag、上下界内外和同一规则的不同 Vim 错误分支；
- `:set` 与 option expression、短名、Legacy/Vim9、转义、incomplete 输入，以及动态值不产生诊断。

每种情况选择一个最小代表 option 做端到端断言；共享规则再用 table test 覆盖参数差异，不为每个
option 重复相同测试。对全部 option 的保证来自生成器 invariant：每项字段完整、每个 callback body
已消费、每项引用的 rule kind 和参数有效、没有 `not-yet-ported`，而不是 562 组手工行为测试。

迁移脚本完成后必须额外生成一份可读 Vim 脚本，按迁移结果为清单中的每个 option 写出对应的
`:set` 命令，并用干净的 Vim v9.2.1015 进程完整执行一次。脚本不能在首个错误处退出；每条命令
都要保留 option 名称与源码 provenance，并收集 `v:errors`、`:messages`、异常、退出状态、Vim
版本和 patch level，使失败能直接定位到迁移错误。受当前构建影响而不可用的 option
也必须出现在脚本中，其预期结果由迁移出的条件分支决定，不能从生成脚本中静默省略。

这次全量 Vim 执行用于确认每个 option 的迁移结果确实能被目标 Vim 接受，不需要再为每个 option
复制一份 Go 行为测试，也不保存按 option 或错误码批量展开的诊断 artifact。规则级 Go 测试仍只
覆盖上一段列出的不同情况。正常语言服务器分析不启动 Vim；feature/platform 分支无法由静态迁移
证明一致时仍不生成值校验器。

每批默认验证：

    gofmt -w <changed-go-files>
    VIM_SOURCE=/Users/chemzqm/lib/vim make metadata-check
    go test ./internal/vimdata ./tools/genmetadata ./internal/syntax ./internal/analysis ./internal/server
    go test ./...
    go vet ./...

不运行 race 或 coverage，除非任务另有要求。

## 7. 补全、Hover 与导航

配置模式只调整相关性，不改变语义结果：

option hover 对 `AvailableWhen != "1"` 的条目显示由 pinned `optiondefs.h` 推导出的 Vim 编译条件；
简单的单个 `FEAT_*` 条件同时显示对应的 `+feature` 名称，复杂的平台/feature 表达式保持原样以免
错误简化。当前只展示要求，不探测 client Vim 的实际 feature，也不新增“不支持选项”诊断。

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
4. 完整选项定义迁移，以及经官方测试按规则情况确认的值、列表、范围和结构诊断。

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

## 12. 实施进度

> 每完成一个小任务在此标记 `[x]` 并注明日期与行为依据。判断依据来自代码、
> 测试与 Vim v9.2.1015 二进制复现，不属于「意图」。

### P0（§9 P0 实施优先级）

- [x] **P0-1：把 `IsConfigFile` 结果传入 analysis 调用边界。**
  新增 `analysis.AnalyzeConfigFile`（角色字段不进入 AST，语义结构与 `Analyze` 一致）；
  服务器端在开放文档解析缓存（`analyzeSnapshotContext`）与封闭文件工作区诊断
  （`computeClosedWorkspaceDiagnostics`）中按路径判定角色后选择对应入口，缓存身份
  同时包含角色，配置变化后可正确重析；completion 复用这份角色分析，不再另行执行
  无角色的 analysis。`vimls/recursive-map` 在配置模式降为 Hint
  通过逐条诊断级别实现（`syntax.Diagnostic.Severity`），不改变稳定 code。
- [x] **P0-2：应用第 4.1 节配置诊断策略矩阵。**
  配置模式下禁用 `vimls/configuration-overwrite`、`vimls/global-internal-state`、
  `vimls/direct-user-keymap`、`vimls/mapping-without-unique`；`vimls/set-vs-setlocal`
  仅保留在 autocmd 体（FileType/BufRead/Win* 等 buffer/window 定向）中；递归 mapping
  保留 code 但默认级别 Hint。附带 §4.2：`vim/E122`/`vim/E174` 在配置模式下只报告
  同一次分析内可静态证明（同一无条件上下文、排除条件互斥分支）的重复定义——
  依据 v9.2.1015 实测：同脚本再次 source 的函数与 `:command` 会静默替换。
- [x] P0-3：完善 `autocmd-group-not-cleared` 的组、定向清除、条件路径和 `++once` 边界。
  - 配置模式改进已实现（分析层 + related information 转换测试）：按有效组归属统计持久 autocmd，
    包括区域外的显式组写法；
    `autocmd!` 裸清除覆盖全部；`autocmd! Event` 按事件覆盖；`autocmd! Event Pattern` 按
    字面 pattern 覆盖；`autocmd! Event Pattern cmd` 替换形式自身不累积且覆盖同 (event, pattern)
    后续定义；`++once` 不视为安全；条件/循环内的清除不做证明；`execute` 动态内容保持 unknown
    不报告；显式组写法按组名（大小写敏感）归属；空组/查询/无定义组不报告。
- [x] P0-4：`mapleader`/`maplocalleader` 定义顺序诊断。
  - 新增 `vimls/config-mapleader-order`（Warning，指向较早 mapping，related 指向后续赋值）：
    仅配置模式、同一无条件顶层直线序列内，<Leader>/<LocalLeader> mapping 先于对应 leader
    的静态字面量赋值时报告；动态赋值、条件块、函数体内 mapping 均保持 unknown 不报告；
    赋值在 mapping 前或赋值间隔后的 mapping 不报告；`mapleader` 与 `g:mapleader` 等价。
- [x] P0-5：保持 E113/E518 与选项元数据的 pinned-version 行为。
  - 新增配置模式回归测试（`config_option_test.go`）：短/长名称、`no`/`inv`、`+=`/`-=`/`^=`/`?`/`&`、
    `:setlocal`/`:setglobal`、`&g:`/`&l:` 与未知 `t_` 终端选项的 E113/E518 行为与插件模式一致，
    选项元数据（vimdata，pinned v9.2.1015）在两种角色下等价，无任何配置专属偏差。
- [x] P0-6：配置模式补全排序测试。
  - 新增服务器端测试（`completion_config_test.go`）：在配置角色下验证 `:set` 选项名、
    mapping 修饰参数、autocmd 事件、文件中已出现 `g:` 配置变量的补全候选与顺序均服从语法
    上下文且确定；两种角色的候选集合一致（补全语义结果不受角色影响），但配置文件内已声明
    的 `g:` 配置变量在顶层无显式作用域的表达式位置优先于同分的未限定声明，plugin 角色保持原排序；
    completion 同时复用路径角色分析并验证缓存角色；其余候选仍按「分数 → 字面量」排序。

### P1（§9 P1 提高编辑体验）

- [x] 确定的同文件重复 mapping 检查（`vimls/duplicate-mapping`）。
  - 配置模式（同一无条件直线序列）下报告静态确定的后一个定义并 related 指向前一个定义；
    比较键含 effective mode 集合（`map`=nvso、`map!`=ic、`vmap` 与 `xmap`/`smap` 按 mode 交集），
    规范化字面量 LHS（`<Leader>` 同拼写比较）、global/`<buffer>` scope、mapping 与 abbreviation
    类别；`unmap`/`mapclear` 会终止相应前序定义，条件或动态清除会将相关状态降为 unknown；
    条件分支内定义不做证明（不误报）。
- [x] loaded guard Hint（`vimls/config-loaded-guard`）与 Vim9 reload 组合检查。
  - 配置模式：顶层 `if exists('g:loaded_*')` → 直接 `:finish` → `endif`，且文件存在对应
    `let g:loaded_*` 标记赋值（Legacy）时给 Hint；Vim9 无需标记并带 reload 半初始化提示，
    `vim9script noclear` 显式单次加载设计豁免。不误报 `exists('*Func')`/`:Cmd`/`+opt`/`##Event`、
    `has()`、`!exists` 初始化、函数内 `finish`、缺标记或 else 分支里的 `finish`。
- [x] augroup、函数和 mapping snippet。
  - 复用现有 augroup（含 `autocmd!` 与 `augroup END`）、Legacy `function ... abort`/Vim9 `def` 模板；
    配置模式下 `:function` 块不再默认带 `!`（与 §4.2 的重新 source 语义一致，plugin 角色保持 `!`）；
    配置模式新增 `<Leader>` mapping 骨架 snippet（含 `<Cmd>call ...` 占位、无 `<unique>`），
    仅在 LHS 空位与 snippet-capable 客户端提供。
- [x] autocmd、`<expr>`/`<Cmd>` mapping、回调选项中的静态导航。
  - autocmd body 与 `<expr>` mapping RHS 的引用导航沿用既有表达式管道（有测试固定）；
    新增 `<Cmd>...<CR>` RHS 的静态 `call Func(...)` 载荷解析与回调选项（`completefunc`/
    `omnifunc`/`operatorfunc`/`tagfunc` 的 `:set opt=Func` 与 `:let &opt = 'Func'`）
    的函数名引用与同名解析，definition/references/hover 均可达定义；动态载荷与拼接值保持
    opaque。

### P2（§9 P2 需要完整索引证据）

> 评估结论（2026）：四项均需要「跨文件顺序/边索引」或「逐选项官方证据」，
> 本里程碑已完成 P0/P1 全部配置专属增量；P2 记录为明确决定并给出触发条件，不静默实现。

- [ ]（评估：推迟）静态 source 图与 source cycle 检查。
  - 现状证据：`workspace.PathResolver.ResolveSource` 只解析单条 `:source`
    （相对 cwd/root），workspace 图仅对 Vim9 `import` 建边；没有 `:source` 边索引。
  - 决定：需先为跨文件 `:source` 边建模与循环检测提供索引与顺序证据，超出本增量范围；单文件
    self-source（`:source` 自身路径）为低价值个案，一并推迟。
- [ ]（评估：推迟）有确定 source 顺序时的跨文件 mapping 冲突。
  - 现状证据：配置文件之间加载顺序由用户启动顺序决定，静态上大多不确定；§5.1 因此限定同文件
    检查。跨文件需「顺序 + 映射状态传播 + leader 前值」三层证据，实现前必须先建立跨文件映射序。
- [ ]（评估：推迟）`:source`、`:runtime`、`:packadd` 补全与导航扩展。
  - 现状证据：`:source` document link（DocumentLink → `ResolveSource`）与 import/colorscheme
    路径补全已存在并在配置测试中保留；`:runtime`/`:packadd` 尚无 runtimepath/package 顺序候选
    与参数语法位识别。
  - 决定：`runtime`/`packadd` 的路径解析与补全列为后续工作项，需先补齐 runtimepath 顺序与
    `pack/*/start|opt/*` 路径索引及 `<sfile>`/转义的 v9.2.1015 语义确认。
- [ ]（基础值诊断已实现，结构化 callback 待实现）完整选项定义迁移与可证明的值诊断。
  - `Option` 已迁移 562 项的完整 flags、条件 storage/default、`did_set`/`expand` callback 和源码行号；
    `metadata-check` 会拒绝字段或生成结果遗漏，生成的 Vim 脚本也已在 v9.2.1015 中逐项执行 `:set`。
  - `expand_set_*` 的固定数组与 flag 字符串已由脚本迁移为 66 项 `CompletionValues`，原有
    `fixedOptionValues` 已删除。生成器还会从 pinned tag 自动定位全部 callback 实现；334 个带
    callback 的 option 均保留所有实现分支的源码位置，其中 48 项已经从共享 helper、静态数值
    比较或结构化 callback 迁移为 `OptionValidation`（exact enum 10、逗号列表 15、flag list 5、
    number range 14、`listchars`/`fillchars`、`statuslineopt`、`winhighlight` 各 1）。
    其余复杂、动态或只有副作用的 callback 使用 `ValidationNone`，即不做值检查；这不是校验结果的
    第三态。上述四类规则中 36 项能证明 option 在所有构建中存在，已接入
    `:set`/`:setlocal`/`:setglobal` 的 `=`/`:` 完整赋值，以及 Legacy 和 Vim9 的 option 字面量赋值；
    分别产生 `vim/E474`、`vim/E487`、`vim/E539`。其余 8 项条件编译规则只保留元数据，在尚未协商
    client feature 前不诊断；值包含尚未精确解码的 `:set` 转义或表达式为动态值时也不诊断。
    第一批结构化 callback 已从 pinned `screen.c` 与 callback 调用关系迁移：`listchars`/
    `fillchars` 检查字段名、字段字符数和 `leadtab` 依赖；`statuslineopt` 检查
    `fixedheight` 与正整数 `maxheight:`；`winhighlight` 检查非空 `from:to` 项。转义、字符
    cell 宽度、运行时 highlight group 是否存在和窗口高度等仍不诊断。`statuslineopt` 仍受
    `+statusline` 编译条件保护，在 client 尚未协商 feature 前只保留规则元数据。其余结构化
    callback 留待后续分批实现；测试按不同规则情况选择代表 option，不为每个 option 或每个错误
    code 重复生成测试。
  - runtime/build/state-dependent 项不生成 validator、不产生值诊断；不引入笼统的
    “invalid option value” 规则，不执行 Vim callback 或用户配置。

## 13. 非目标

- 不执行配置文件来计算真实 mapping、option 或 autocmd 状态；
- 不把 Neovim Lua API 或 Neovim-only 行为当作 Vimscript 通用规则；
- 不对未确定加载顺序的所有 workspace/runtimepath 文件做全局冲突猜测；
- 不因为配置文件包含跨 dialect 构造就额外报错；
- 不提供通用格式重写或大范围“最佳实践”迁移。
