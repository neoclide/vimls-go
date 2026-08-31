# vimls-go 增量 AST 解析实施方案

> **状态：已废弃，仅保留为历史研究。** 仓库不再实施本文的局部 AST
> `Reparse`、checkpoint、收敛或 clone/rebase 方案。当前实施契约以仓库根目录的
> `incremental-parsing-plan.md` 为准：LSP edit 只增量合成文本，变化内容始终完整解析。

> 本文件保留现有文件名 `tree-sister.md`，正文统一使用正确名称
> Tree-sitter。
>
> **架构决定**：vimls-go 不接入 Tree-sitter，不增加 CGo、grammar、query、
> `TSTree` 或 `TSNode`。本项目只学习 Tree-sitter 的增量计算方法，在现有纯
> Go、Legacy/Vim9 双方言 parser 上实现增量 AST 解析。
>
> **参考快照**：本方案参考只读本地仓库
> `/Users/chemzqm/lib/tree-sitter` 的提交
> `664e9a678667302fee9c705103072119b4fd4845`。参考源码不是构建依赖，也不应
> 把 Tree-sitter 的 JavaScript 风格 grammar 或错误恢复直接移植到本项目。
>
> **当前状态**：这是实施计划，不表示增量 AST 已经实现。当前
> `syntax.Parse` 仍然对完整 source 做全量解析。

## 实施顺序总览

| 任务 | 可观察交付 | 前置任务 | Owner |
| --- | --- | --- | --- |
| I0 | AST differential oracle、编辑矩阵、full-parse 基线 | 无 | `language_worker` |
| I1 | 可恢复的具体 parser state，scan/structure/details/finish 分阶段 | I0 | `language_worker` |
| I2 | 完整语法单元、entry/exit state、fragile 标记 | I1 | `language_worker` |
| I3 | `Reparse` 安全前缀复用，dirty suffix 解析到 EOF | I2 | `language_worker` |
| I4 | guard-unit 状态收敛、后缀复用、nested span 重定位 | I3 | `language_worker` |
| I5 | server 缓存纯 syntax AST，并在发布前保留 stale 屏障 | I4 | `server_worker` |
| I6 | 随机编辑、官方 corpus 回放、race 和最终性能闸门 | I5 | `qa_reviewer` + 对应 owner |

I0-I6 串行执行，不并发修改 `internal/syntax`。详细、可直接分派的任务 brief 在第
10 节。

## 1. 要交付的结果

最终新增一个直接、保守的入口：

```go
// Parse 始终是全量正确性 oracle。
func Parse(source string) *File

// Reparse 尝试复用 previous；无法证明安全时内部退化为 Parse。
func Reparse(previous *File, source string) *File
```

完成后的可观察契约是：

1. 对任意旧文本、任意新文本和任意合法编辑序列，
   `Reparse(Parse(old), new)` 的导出 AST、token、block、diagnostic、
   `OpaqueTail` 和全部 byte span 必须与 `Parse(new)` 相同。
2. `previous` 在 `Reparse` 前后保持不变；旧 AST 可以同时被旧请求读取。
3. `source` 相同可以直接返回 `previous`；source 不同的结果必须拥有新 source
   对应的完整 AST。
4. 增量路径不依赖 LSP position encoding。LSP range 仍由
   `internal/text` 先转换成最终文本；syntax 层只处理字节和完整 source。
5. Legacy、Vim9、混合方言、错误恢复、未知命令和嵌套 Ex payload 的行为不能
   因为增量化而改变。
6. 增量化不新增第三方依赖，不执行用户 Vim 脚本，不改变 4 MiB 文件上限，
   不把 parser 状态放入 server 或 workspace。
7. 高性能必须由固定 corpus、`-benchmem`、五次采样和 profile 证明，不能只以
   “复用了对象”作为结论。

## 2. 当前实现基线

以下事实来自当前代码，实施 agent 必须从这些事实出发，而不是套用一个假想的
Tree-sitter 项目结构。

| 位置 | 已有能力 | 当前缺口 |
| --- | --- | --- |
| `internal/text/snapshot.go` | 不可变 `Snapshot`、顺序应用多个 change、UTF-8/16/32 到 byte offset 转换 | 每次 change 重建完整 string 和 line index；没有提供给 syntax 的增量树 edit |
| `internal/workspace/documents.go` | URI、client version、内部 revision、取消旧分析、配置 revision 和 stale 检查 | document 只保存当前文本，不保存 AST |
| `internal/syntax/syntax.go` | `File.Source`、绝对半开 byte span、Legacy/Vim9 独立入口、丰富的 command/expression/type AST | 只有 `Parse(source)`，没有旧 AST、checkpoint 或复用元数据 |
| `internal/syntax/scanner.go` | 按物理行推进，处理 dialect stack、`scriptversion`、续行、heredoc、文本体、`loadkeymap`、嵌套命令和恢复 | `parseSource` 的状态是局部变量，不能从安全边界恢复；扫描、结构整理、details 和 diagnostics 交织 |
| `internal/syntax/logical_view.go` | 普通行 identity 路径零拷贝；只为续行建立 logical-to-source 映射 | logical view 生命周期未被记录成可复用语法单元 |
| `internal/syntax/blocks.go` | 从 command stream 构建 block，并产生跨命令诊断 | 每次全文件扫描；会写 `Command.Block`，因此旧 `Command` 不能被新树原地复用 |
| `internal/analysis` | 从一个完整 `syntax.File` 收集 scopes、symbols、references 和 types | 本方案第一阶段不做增量语义；它是后续消费者 |
| `internal/workspace/index.go` | 按文件原子替换不可变 symbol facts | 不需要先改；单文件增量 AST 稳定后再考虑事实增量 |
| `internal/server/server.go` | `parsedDocument{revision, file}` 缓存、最多四个分析 worker、发布前 revision/config 检查 | `analyzeDocument` 和 cache miss 的 `DocumentSymbol` 都调用全量 `syntax.Parse`；target-version diagnostics 和截断当前会直接改写被缓存的 `File.Diagnostics` |
| `internal/syntax/*_test.go` | 大量 Legacy/Vim9/官方 corpus、恢复和 span 测试；已有 full parse benchmarks | 没有任意编辑序列下的 incremental-vs-full AST differential test |

当前已经满足、不得重复建设的部分：

- 文本 snapshot 是不可变的。
- 同一 `didChange` 的 changes 已按中间文本顺序应用。
- syntax span 已统一使用原始 source 的 byte offset。
- server 已阻止旧 revision/configuration 的结果覆盖新结果。
- open-document 和 workspace batch 已有限制为 `min(GOMAXPROCS, 4)` 的 worker
  数量。

因此本方案不创建新的 Document Manager、Tree Manager、Parser Service、
Scheduler 或缓存 Registry。

## 3. 从 Tree-sitter 学什么

| Tree-sitter 思想 | vimls-go 中的对应实现 |
| --- | --- |
| 旧 tree 是 cache | 旧的不可变 `*syntax.File` 是 cache |
| edit 先使旧结果失效 | 比较 `previous.Source` 和新 source，得到一个保守 byte dirty interval |
| subtree 带 parse state | 每个语法单元保存完整 entry/exit parser state |
| lexer lookahead 可能越过 token | dirty 单元之前强制多退一个完整语法单元 |
| fragile/error node 少复用 | 含恢复诊断、不完整 payload 或不可信边界的单元不能作为收敛锚点 |
| 当前 parse state 验证复用 | source bytes、entry state、exit state 和单元边界全部相同才复用 |
| edited old tree 与 new tree diff | 增量结果始终与 `Parse(newSource)` 做 differential testing |
| copy-on-write snapshot | 不修改旧 `File`；新 `File` 克隆并重定位被复用的导出节点 |
| 无法证明时拆小或全量解析 | 先扩大 dirty 范围；仍无安全 checkpoint 时调用 `Parse` |

不学习、也不实现：

- Tree-sitter C runtime、生成式 grammar、GLR stack、external scanner API。
- `TSPoint`、`InputEdit`、`changed_ranges` 或 query language。
- `TSNode` 式跨版本 node identity。
- whole-file token stream 和 panic 驱动的错误恢复。
- 因为“以后可能需要”而提前引入 rope、arena、object pool 或通用增量框架。

## 4. 范围和非目标

本计划只交付“一个已打开文档的增量 syntax AST”。包含：

- command、token、expression、type、block 和 syntax diagnostic。
- Legacy、Vim9 和混合方言状态。
- 物理行、logical continuation、heredoc、文本体、`loadkeymap`、嵌套
  command list、collected command/autocmd block 和 `finish` opaque tail。
- 在 server 中复用最近一次已发布的旧 AST。
- 差分测试、fuzz、benchmark 和 full-parse fallback。

这是性能实施线，不替代 `docs/roadmap.md` 的 M3 syntax completeness。新增 grammar
或恢复行为仍先在 full `Parse` 下取得 Vim 证据，再要求 `Reparse` 与它等价；不能
用增量工作宣称某个尚未实现的 Vim form 已受支持。

第一轮明确不做：

- 增量 scope/type/reference 或 workspace dependency graph。
- semantic token delta、diagnostic delta、增量 symbol index。
- rope/piece table 或增量 line index。
- 持久化磁盘 AST cache。
- 对 unopened workspace file 维护历史 AST。
- 对外暴露 stable node ID、复用率或内部 checkpoint。

如果最终 profile 显示文本复制或 line index，而不是 parser，已经成为主要瓶颈，
应另立任务处理 `internal/text`；不要把该问题混进本方案。

## 5. 必须保持的正确性不变量

1. `File.Source` 与该 `File` 的所有 span、token、AST 和 diagnostic 必须属于同一
   完整文本。
2. `Reparse` 不接受或保存 LSP line/character；syntax 层只处理 byte offset。
3. 旧 `File` 只读。不得为了重定位新树而修改旧 command、expression、type、
   nested `File`、`CommandList` 或 slice backing array。
4. Legacy 与 Vim9 command consumer 保持独立；复用的只是中立状态、单元和
   clone/rebase 机械逻辑。
5. `vim9script`、`def`、`function`、`vim9cmd`、`legacy` 和
   `scriptversion` 的上下文规则不能简化成 file-wide Boolean。
6. `vim9cmd` 和 `legacy` 仍只影响紧随其后的一个 command。
7. 不能在 Legacy logical continuation、Vim9 automatic continuation、heredoc、
   文本体、`loadkeymap` body 或 collected block 内建立 restart checkpoint。
8. 编辑到单元边界时必须把前一个单元也视为 dirty；下一行可能改变前一条 Vim9
   command 的 automatic continuation。
9. source bytes 相同不足以复用；entry parser state 必须相同。
10. hash 只能帮助定位候选，不能作为等价证明；最终必须比较原始 bytes。
11. 含恢复、不完整 payload 或未知边界的单元可以保留 AST，但不能作为后缀收敛
    锚点。
12. 未知用户命令和未来命令仍是 opaque syntax，不因无法增量解释而新增语法
    错误。
13. 所有循环必须前进；每个非 EOF 语法单元至少消费一个 source byte。
14. 增量路径失败是普通 fallback，不得吞掉 parser bug 的 panic。
15. full parse 是永久 oracle，不得为了共享增量实现而让 `Parse` 依赖旧 AST。
16. server 发布前仍必须执行现有 snapshot/revision/configuration 检查。
17. server 缓存的 `*syntax.File` 必须是 target-version 无关、未经 diagnostic cap
    改写的纯 parser 结果；compatibility 和 truncation diagnostics 只属于一次发布。

## 6. 目标数据流

```text
old immutable File + new complete source
                  │
                  ├─ source 相同 ───────────────► 返回 old File
                  │
                  ▼
       求最小保守 byte dirty interval
                  │
                  ▼
  在旧语法单元中选择 restart boundary
  - 命中单元前再退一个单元
  - payload 内退到 owner
  - 无安全边界则 full Parse
                  │
                  ▼
       从 checkpoint state 向前解析
                  │
                  ├─ 未收敛 ─► 解析到 EOF
                  │
                  └─ bytes + state + guard unit 收敛
                                  │
                                  ▼
                         克隆/重定位旧后缀单元
                                  │
                                  ▼
                  重建全局 block 与最终 File
                                  │
                                  ▼
                     与当前 snapshot revision 绑定
```

### 6.1 `Reparse` 的最小 API

第一版只增加 `Reparse(previous, source)`，不增加 `Edit`、`ParseOptions`、
`ParseResult` 或 cache manager。

理由：server 可能连续取消多个 revision，最近已发布 AST 不一定是当前 snapshot 的
直接父版本。直接比较旧、新完整 source 可以安全跨越任意数量的中间 revision，
也不需要让 `internal/text`、`workspace` 和 `syntax` 共享一套 edit 类型。

内部按最长公共前缀和最长公共后缀得到：

```go
type sourceChange struct {
    Start  int // old/new 的第一个不同 byte
    OldEnd int // old dirty interval 的结束
    NewEnd int // new dirty interval 的结束
}
```

要求：

- 比较是 byte comparison，不要求 dirty boundary 是 UTF-8 rune boundary。
- 公共后缀不得与公共前缀重叠。
- 旧、新完全相等时不创建 change。
- 这个 O(n) 比较是第一版允许的成本，因为当前 `Snapshot` 本身已经复制完整
  string；只有 profile 证明它显著占比后，才增加显式 edit provenance。

### 6.2 可恢复 parser state

把当前 `parseSource` 的局部状态收拢为一个具体 `sourceParser`。这不是通用 parser
框架，只服务当前一个 scanner。checkpoint 至少要完整保存：

```text
initialDialect
activeDialect
scriptVersion
vim9ProloguePending
dialectStack (逐项复制，不能共享可变 backing array)
open enum state
open Vim9 collected-command block state
```

`targetVersion`、LSP encoding、document revision 和 configuration revision 不属于
parser state：parser 始终构建最新 v9.2.1015 syntax，版本兼容诊断由 server 在纯
syntax AST 之外计算。

以下状态不允许跨 checkpoint 保存；只有它们已经关闭后才产生安全边界：

```text
active heredoc
active append/change/insert text body
active loadkeymap body
unfinished Legacy logical continuation
unfinished Vim9 automatic continuation
正在收集的 autocmd/command/legacy do block
```

如果 parser 后续新增任何会影响下一单元解释的状态，必须同时加入
`parserState.equal`、checkpoint 测试和 differential corpus。遗漏状态等同于错误
复用，不是普通性能回退。

### 6.3 语法单元

`parseUnit` 是最小复用单元，边界服从 Vim 的 source reader，而不是任意 AST node。
一个单元可以包含同一 logical line 上由 `|` 分隔的多个 command。

每个单元记录：

```go
type parseUnit struct {
    Span                   Span
    Entry                  parserState
    Exit                   parserState
    FirstCommand           int
    CommandCount           int
    FirstToken             int
    TokenCount             int
    FirstScannerDiagnostic int
    ScannerDiagnosticCount int
    FirstDetailDiagnostic  int
    DetailDiagnosticCount  int
    Fragile                bool
}
```

字段名称可以在实现中微调，但必须能证明以下不变量：

- 单元按 source 顺序排列、互不重叠，并覆盖 parser 实际处理的全部 source。
- 普通单元从一个物理行开始，消费完整 logical line 和换行。
- heredoc、文本体和 `loadkeymap` 从 owner command 开始，一直消费到完整终止符；
  不完整时消费到现有恢复边界并标记 `Fragile`。
- collected `:command {}`、`:autocmd ... {}` 和 Legacy do block 的 owner 与多行
  body 属于同一单元。
- 直接 `:finish` 与 `OpaqueTail` 形成最后一个不可作为收敛锚点的单元。
- EOF 前的单元必须 `Span.End > Span.Start`。
- `Entry` 是读取 `Span.Start` 前的状态，`Exit` 是读取 `Span.End` 后的状态。

不要按 expression、token 或单个 `|` command 建 checkpoint。它们的边界仍受所属
Ex command consumer 和 logical line 状态影响。

### 6.4 解析阶段必须先解耦

在实现复用前，把当前全量路径整理为仍在同一 package 内的四个明确阶段：

1. `scan`：产生 command header、token、scanner diagnostics 和 unit state。
2. `structure`：执行 collected block 整理、`buildBlocks`、直接 `finish` 截断。
3. `details`：解析 expression、type、mapping、autocmd、substitute、nested command
   list 等 command-specific AST。
4. `finish`：合并 diagnostics、修正 lambda body source、验证/排序 token，生成最终
   `File`。

必须保留当前 diagnostic 顺序：

```text
scanner diagnostics
block/structure diagnostics
command detail diagnostics（command source order）
```

增量缓存需要区分这三类诊断。不得通过最后统一按 span 排序来掩盖阶段归属变化，
因为这会改变现有可观察结果。

`buildBlocks` 第一版仍可对组合后的 command stream 做全量 O(command count) 扫描。
这是简单且安全的起点。只有 profile 证明它成为主要成本后，才实施第 12 节的条件
优化。

### 6.5 restart 规则

对旧 source 的 dirty interval，按以下固定顺序选择 restart：

1. 找到第一个与 dirty interval 相交、包含 `Start`，或紧邻 `Start` 的旧 unit。
2. 若命中 payload/body/continuation/collected block，选择它的 owner unit。
3. 再向前退一个完整 unit，吸收 command boundary 和下一行 leading-continuation
   lookahead。
4. 若编辑可能改变 `startsWithVim9Script` 的结果，从 byte 0 开始。
5. 若编辑在旧 `OpaqueTail` 内，从直接 `finish` 所在 unit 开始。
6. 若 unit 表、entry state 或 source coverage 自检失败，调用 `Parse(source)`。

restart 之前的 unit 只有在以下条件全部成立时才可复用：

- 对应新 source 前缀逐 byte 相同。
- unit 的导出 AST 不含跨过 restart 的 span。
- unit 不是后来被 owner command 吞入的 continuation/payload/body。
- 克隆后不会与旧 `File` 共享任何将被 `buildBlocks` 或 finish 阶段修改的对象。

### 6.6 后缀收敛和复用

先交付“复用前缀、从 restart 解析到 EOF”的正确版本，再增加后缀收敛。不要在
同一个 patch 中同时引入 checkpoint、收敛和 span 重定位。

后缀复用必须满足：

1. 新 parser 已越过 `NewEnd`，候选旧 unit 位于 `OldEnd` 之后。
2. 用 `delta = NewEnd - OldEnd` 映射新旧 unit 起点。
3. 新旧候选 unit 的原始 source bytes 完全相同。
4. 新 candidate 的 `Entry` 等于旧 candidate 的 `Entry`。
5. 解析一个完整、非 `Fragile` 的 guard unit；其 `Exit`、command/token/diagnostic
   导出结果也与旧 guard unit 相同。
6. 从 guard unit 的下一个边界开始复用旧后缀。不能只看到一行文本相同就立即
   停止解析。

可以用标准库 hash 帮助跳过明显不匹配的候选，但 hash 命中后仍要比较 bytes 和
state。当前 edit 已给出唯一的 offset 映射，不需要全局 hash index。

### 6.7 AST 克隆与 span 重定位

当前 AST 使用绝对 span，`Command.Block` 又会被结构阶段重写，所以新 `File` 不能
直接拼接旧 slice 或修改旧节点。

实现一个 package-private、显式的 clone/rebase visitor，覆盖当前所有含 span 的
类型：

- `File`、`CommandList`、`Command`、`Token`、`Block`、`Diagnostic`。
- `Expression`、lambda `File`、`Type`、`Parameter`、`Binding`。
- heredoc、text body、keymap、mapping、highlight、syntax、set、substitute、
  autocmd、function、aggregate、type alias、import、enum 和 modifier payload。

规则：

- 零值可选 span 保持零值；有效 span 的 Start/End 同时加 delta。
- string value 保持原语义值；不得重新从旧 source 延迟取值。
- slice、map 和将被后处理写入的 pointer 必须深拷贝。
- private `logical`、`boundaryExpression` 等只应存在于尚未 finish 的新单元；最终
  `File` 不依赖旧 parser 的临时对象。
- clone visitor 每新增一个 AST span 字段就必须编译失败或测试失败，不能依赖反射
  修改生产对象。

第一版前缀复用的 delta 为 0，仍要 clone 会被后处理修改的 command。后缀复用用
相同 visitor 处理非零 delta，不另写第二套 shift 逻辑。

### 6.8 Fragile 与 full fallback

以下单元默认 `Fragile`，可以输出 best-effort AST，但不能用来宣布后缀收敛：

- 含 parser recovery diagnostic。
- 未终止字符串、列表、字典、lambda、type 或 command payload。
- 不完整 heredoc、文本体、`loadkeymap` 或 collected block。
- 被直接 `finish` 截断的 tail。
- command 消费边界依赖当前无法序列化的隐藏状态。

以下情况直接 full parse：

- `previous == nil`。
- `previous.Source`、unit coverage 或 checkpoint state 自检失败。
- 新旧 file-level dialect 触发条件变化且 byte 0 restart 路径无法复用。
- clone/rebase 发现越界、倒序或跨 unit span。
- 增量解析没有前进，或资源上限检查失败。

不要 `recover` parser panic 后静默 full parse。panic 是 bug，必须让测试或 fuzz
暴露。

## 7. Differential oracle

测试内使用标准库 `encoding/json` 序列化 `*syntax.File`，比较所有导出字段。JSON
会自然忽略 `parseUnit`、checkpoint 和其它 private cache，因此比较的是调用者可见
结果。

每个测试至少比较：

```go
oldFile := Parse(oldSource)
incremental := Reparse(oldFile, newSource)
clean := Parse(newSource)

assertObservableJSONEqual(t, incremental, clean)
assertObservableJSONEqual(t, oldFile, Parse(oldSource)) // old tree 未被修改
assertAllSpansInBounds(t, incremental)
```

JSON 等价是必要条件，不是全部条件。还要单独断言：

- `File.Source == newSource`。
- token、command、block 和 diagnostic 顺序一致。
- 每个 span 对 `File.Text(span)` 取到正确 bytes。
- 旧 AST 在 `Reparse` 后仍与调用前 JSON 相同。
- 同 source 快路径允许 pointer identity 相同。
- 增量路径实际复用了预期 unit；该断言放在 `package syntax` 的白盒测试中，不对外
  暴露统计 API。

## 8. 必测编辑矩阵

每一类都包含 insert、delete、equal-length replace 和跨行 replace；至少一次编辑
长度变化。

### 文本与位置

- 文件头、文件中间、EOF。
- UTF-8 多字节、astral character、combining character、tab。
- LF、CRLF、BOM、无末尾换行。
- 单次大替换和连续 100 次小编辑。

### 方言和 parser state

- 新增/删除/移动首个有效 `vim9script`。
- `def` 中 Legacy file、`function` 中 Vim9 file。
- `vim9cmd`、`legacy` 只影响下一 command。
- `scriptversion` 1 到 4，以及无效版本恢复。
- mismatched `enddef`/`endfunction` 后继续解析。

### command boundary 和 lookahead

- range、modifier、abbreviation、bang、register、count。
- `|` 在普通 argument、regexp、mapping、substitute、string 和 comment 中。
- 编辑下一行使其成为或不再成为 Vim9 leading continuation。
- 编辑上一行添加/删除 Legacy `\` continuation。
- Vim9 automatic continuation 的 operator、括号、ternary 和 lambda。

### 多行 owner

- 完整/不完整 heredoc；编辑 marker、body 和 end marker。
- `append`、`change`、`insert` 的 dot terminator 和恢复边界。
- `loadkeymap` body。
- `command {}`、autocmd block、global/vglobal、list-do 和 Legacy embedded block。
- nested embedded command depth 上限附近。
- 直接 `finish` 前后、conditional `finish` 和 `OpaqueTail`。

### 错误恢复

- 未闭合 string/list/dict/type/signature。
- malformed command 后同一行有 `|`，下一物理行有有效 declaration。
- unknown user command 和未来 command。
- 编辑把 fragile unit 修复为 clean，或把 clean unit 改成 fragile。

## 9. 性能验收

遵守 `docs/testing.md` 的既有规则：先在引入操作的 milestone 固定 baseline；固定
runner 上经确认的 median/p95 时间回退不得超过 15%，allocation 回退不得超过
20%，除非明确记录并批准原因。

增量 benchmark 使用 10 KiB、100 KiB 和 1 MiB 文档，Legacy 与 Vim9 分开，固定
以下场景：

- 文件前 10% 的一字符 insert。
- 文件中间的一字符 insert。
- 文件后 10% 的一字符 insert。
- equal-length replace，用于验证后缀收敛。
- heredoc marker edit 和 Vim9 continuation edit，用于测量保守扩大范围。
- fragile edit，验证 fallback 不出现异常放大。

不要在实现和 runner 数据出现之前猜一个百分比。I0 记录 full parse 基线；I3
首次记录 prefix-only `Reparse` 基线；I4 在同一 runner 上冻结最终相对预算。最终
预算必须同时包含以下硬性结构门槛和测量门槛：

| 场景 | 门槛 |
| --- | --- |
| source 不变 | 返回原 pointer，不重新扫描，不新增 AST allocation |
| 文件尾小编辑 | 白盒测试证明 restart 前 unit 未进入 scanner/details；median time 和 allocation 都低于 full parse |
| 文件中间小编辑 | 白盒测试证明 guard 后 unit 未进入 scanner/details；median time 和 allocation 都低于 full parse |
| 文件头或强制 full fallback | 相对 full parse 的 time 回退不超过 15%，allocation 回退不超过 20% |
| 全部增量场景 | 达到 I4 基线冻结的绝对/p95 预算，且五次样本没有持续性退化 |

这些门槛不能通过减少 AST、token、diagnostic、官方 corpus 或恢复行为达成。I4
冻结的具体数值必须连同 runner 和原始数据写入实施记录；不能在后续 patch 中静默
放宽。若全局 `buildBlocks`、clone/rebase 或 materialization 使中间编辑没有实质
收益，执行第 12 节的条件优化，而不是把“命中过 cache”当作高性能结论。

记录内容：Go 版本、OS/CPU、`GOMAXPROCS`、corpus/tag、source size、edit offset、
五次 `ns/op`、`B/op`、`allocs/op` 中位数，以及 full/incremental profile 的主要
cost center。

## 10. 可直接分派的实施任务

所有任务按 I0 → I6 串行。I1 到 I4 都修改 `internal/syntax`，禁止同时交给两个
write agent。primary agent 负责 API 冻结、跨 package 集成、文档和最终验证。
每个 write agent 都应先检查 `git status`，视自己为共享工作树中的一个操作者：
只改 Allowed paths，不恢复或格式化他人的改动，交付前把实际 changed paths 与
brief 对照；若必须越界，停止写入并把缺失 contract 报告给 primary agent。

### I0：建立 AST differential oracle 和性能基线

**建议 agent**：`language_worker`，只写测试。

**Goal**

建立可复用的导出 AST JSON 比较器、编辑矩阵和 full-parse benchmark；不改生产
parser 行为。

**Allowed paths**

- `internal/syntax/incremental_test.go`（新文件）。
- `internal/syntax/benchmark_test.go`。

**Forbidden paths**

- `internal/syntax/*.go` 中除 `*_test.go` 外的文件。
- `internal/text/`、`internal/workspace/`、`internal/server/`。
- `go.mod`、`go.sum`、`testdata/`、generated files。

**Required behavior**

- 用 `encoding/json` 比较 `File` 的全部导出字段。
- 提供 old/new edit case 表，覆盖第 8 节的最小代表集。
- benchmark 分离 source 构造和 timed parse；调用 `b.ReportAllocs()`、`b.SetBytes()`。
- 记录 full parse 的 10 KiB、100 KiB、1 MiB Legacy/Vim9 五次基线。
- 不引入 golden 自动更新或第三方比较库。

**Validation**

```sh
gofmt -w internal/syntax/incremental_test.go internal/syntax/benchmark_test.go
go test -mod=readonly ./internal/syntax
go test -mod=readonly ./internal/syntax -run '^$' -bench 'BenchmarkParseIncrementalCorpus' -benchmem -count=5
```

交付证据：测试数、benchmark runner、五次原始数据和中位数；此任务不声称已有
增量性能。

### I1：把全量 parser 整理成可恢复状态机和明确阶段

**建议 agent**：`language_worker`。

**Goal**

把 `parseSource` 的局部状态移入一个具体 `sourceParser`，分离 scan、structure、
details、finish 四阶段；`Parse` 的导出结果逐字节不变。

**Allowed paths**

- `internal/syntax/scanner.go`。
- `internal/syntax/blocks.go`。
- `internal/syntax/syntax.go`。
- `internal/syntax/incremental_test.go`。
- `internal/syntax/benchmark_test.go`。

**Forbidden paths**

- `internal/text/`、`internal/analysis/`、`internal/workspace/`、
  `internal/server/`。
- `testdata/`、`go.mod`、`go.sum`、docs、generated files。

**Required behavior**

- `sourceParser` 只封装当前真实状态，不增加 interface/manager/service。
- Legacy/Vim9 consumer 和现有 AST 类型保持不变。
- scanner、structure 和 detail diagnostics 分开保存，再按现有顺序合并。
- details 映射完成后清除 `Command.logical`、`boundaryExpression` 等短生命周期解析
  对象；最终 cache 只保留后续消费者需要的 AST 和 unit metadata。
- `Parse` 不调用 `Reparse`，仍是独立全量 oracle。
- official corpus 的 command/token/block/diagnostic span 和顺序不变。
- 不在本任务实现 cache、checkpoint 或旧树复用。

**Validation**

```sh
gofmt -w internal/syntax/scanner.go internal/syntax/blocks.go internal/syntax/syntax.go internal/syntax/incremental_test.go internal/syntax/benchmark_test.go
go test -mod=readonly ./internal/syntax
go test -mod=readonly ./...
go test -mod=readonly -race ./...
go vet -mod=readonly ./...
```

预期证据：I0 的 observable JSON cases 全部相同；全量 benchmark 不超过既有
15% time/20% allocation 回退线。

### I2：记录安全语法单元和完整 entry/exit state

**建议 agent**：`language_worker`。

**Goal**

让每次 `Parse` 产生 private `parseUnit`/checkpoint 元数据，但尚不复用旧 AST。

**Allowed paths**

- `internal/syntax/incremental.go`（新文件）。
- `internal/syntax/scanner.go`。
- `internal/syntax/syntax.go`。
- `internal/syntax/incremental_test.go`。

**Forbidden paths**

- `internal/text/`、`internal/analysis/`、`internal/workspace/`、
  `internal/server/`。
- `testdata/`、`go.mod`、`go.sum`、docs、generated files。

**Required behavior**

- 每个 unit 保存第 6.2、6.3 节规定的状态和 source ownership。
- payload、continuation、collected block 内没有 checkpoint。
- unit 自检覆盖顺序、范围、前进、state round trip 和 owner 关系。
- fragile classification 至少覆盖第 6.8 节。
- private cache 不出现在 JSON，不改变 public `File` contract。
- 相同 source 的两次 `Parse` 产生确定性的 unit 边界和 state。

**Validation**

```sh
gofmt -w internal/syntax/incremental.go internal/syntax/scanner.go internal/syntax/syntax.go internal/syntax/incremental_test.go
go test -mod=readonly ./internal/syntax -run 'Test(ParseUnit|Checkpoint|Incremental)'
go test -mod=readonly ./...
go test -mod=readonly -race ./...
go vet -mod=readonly ./...
```

预期证据：所有 full parse 输出不变；白盒测试打印或断言代表性 source 的 unit
边界、entry/exit state 和 fragile 标记。

### I3：实现保守 `Reparse`，只复用安全前缀

**建议 agent**：`language_worker`。

**Goal**

新增 `syntax.Reparse(previous, source)`；选择安全 restart，克隆旧前缀，从该点解析
到 EOF。尚不做后缀收敛。

**Allowed paths**

- `internal/syntax/incremental.go`。
- `internal/syntax/scanner.go`。
- `internal/syntax/syntax.go`。
- `internal/syntax/incremental_test.go`。
- `internal/syntax/benchmark_test.go`。

**Forbidden paths**

- `internal/text/`、`internal/analysis/`、`internal/workspace/`、
  `internal/server/`。
- `testdata/`、`go.mod`、`go.sum`、docs、generated files。

**Required behavior**

- API 严格为 `Reparse(previous *File, source string) *File`。
- 内部 byte diff、restart 和 fallback 遵守第 6.1、6.5、6.8 节。
- `previous == nil` 等价于 `Parse(source)`；相同 source 返回 `previous`。
- 旧 `File` 及其 nested AST 不被修改。
- prefix clone 后重新执行全局 structure/finish；无重复 diagnostics。
- 第 8 节每个 case 的 observable JSON 与 clean parse 相同。
- 白盒测试证明尾部编辑没有重新扫描 restart 前的 unit。

**Validation**

```sh
gofmt -w internal/syntax/incremental.go internal/syntax/scanner.go internal/syntax/syntax.go internal/syntax/incremental_test.go internal/syntax/benchmark_test.go
go test -mod=readonly ./internal/syntax -run 'Test(Reparse|Incremental)'
go test -mod=readonly ./...
go test -mod=readonly -race ./...
go vet -mod=readonly ./...
```

预期证据：正确性矩阵全绿；尾部编辑已优于 full parse，但本任务不要求中部编辑
达到最终预算。

### I4：实现 guard-unit 收敛、后缀复用和 span 重定位

**建议 agent**：`language_worker`。

**Goal**

在 byte、entry/exit state 和一个 clean guard unit 全部收敛后，停止向前解析并克隆/
重定位旧后缀 AST。

**Allowed paths**

- `internal/syntax/incremental.go`。
- `internal/syntax/scanner.go`。
- `internal/syntax/syntax.go`。
- `internal/syntax/incremental_test.go`。
- `internal/syntax/benchmark_test.go`。

**Forbidden paths**

- `internal/text/`、`internal/analysis/`、`internal/workspace/`、
  `internal/server/`。
- `testdata/`、`go.mod`、`go.sum`、docs、generated files。

**Required behavior**

- 遵守第 6.6 节全部收敛条件；fragile unit 不能作 guard。
- clone/rebase visitor 覆盖第 6.7 节全部现有 AST 类型。
- equal-length 和 non-equal-length edit 都能复用安全后缀。
- suffix command 的全部 nested span 随 delta 正确移动。
- 新增 span 字段时，集中 visitor 的测试必须暴露遗漏。
- 仍可随时退化为 I3 的 prefix-to-EOF 路径或 full `Parse`。
- 旧 AST 在并发只读时不发生 data race。

**Validation**

```sh
gofmt -w internal/syntax/incremental.go internal/syntax/scanner.go internal/syntax/syntax.go internal/syntax/incremental_test.go internal/syntax/benchmark_test.go
go test -mod=readonly ./internal/syntax -run 'Test(Reparse|Rebase|Convergence|Incremental)'
go test -mod=readonly ./...
go test -mod=readonly -race ./...
go vet -mod=readonly ./...
go test -mod=readonly ./internal/syntax -run '^$' -bench 'Benchmark(Parse|Reparse)IncrementalCorpus' -benchmem -count=5
```

预期证据：中间和尾部编辑达到第 9 节预算；profile 显示未重复执行已收敛后缀的
command/expression parser。

### I5：接入 open-document server cache

**建议 agent**：`server_worker`。必须等 I4 的 syntax API 冻结后开始。

**Goal**

让 open-document 后台分析和 stale `DocumentSymbol` 读取最近一次纯净、不可变的
syntax AST 作为 `Reparse` 输入，同时保留现有 revision/configuration 发布屏障。

**Allowed paths**

- `internal/server/server.go`。
- `internal/server/server_test.go`。
- `test/integration/lsp_subprocess_test.go`。

**Forbidden paths**

- `internal/syntax/`。
- `internal/text/`、`internal/analysis/`、`internal/workspace/`。
- `go.mod`、`go.sum`、docs、testdata、generated files。

**Required behavior**

- 在现有 `publishMu` 保护下取得 `parsedDocument`，释放锁后执行 `Reparse`。
- 旧 `*File` 在解析期间只读；不得持锁运行 parser。
- 最近缓存 revision 可以早于 snapshot 多个 revision；`Reparse` 直接比较完整
  source，不能假定相邻版本。
- 缓存 `File` 前不得向 `File.Diagnostics` 追加 compatibility diagnostic，也不得为
  `maxDiagnosticsPerDocument` 截断它。复制 parser diagnostics，在一次发布使用的
  local slice 上追加 compatibility/truncation diagnostic。
- target version 变化只重新计算发布 diagnostics；不能污染或废弃 source 未变的
  syntax cache。
- `IsCurrent`、close/reopen、configuration revision、diagnostic cap 和发布版本逻辑
  保持不变。
- canceled/stale parse 可以完成，但不能发布。
- `workspace.ParseSources` 保持 full parse；没有旧 AST 的 batch 不创建伪 cache。

**Validation**

```sh
gofmt -w internal/server/server.go internal/server/server_test.go test/integration/lsp_subprocess_test.go
go test -mod=readonly ./internal/server
go test -mod=readonly -run TestLSPSubprocess ./test/integration
go test -mod=readonly ./...
go test -mod=readonly -race ./...
go vet -mod=readonly ./...
```

预期证据：连续 edits、分析取消、close/reopen 和配置变更测试证明旧结果不能发布；
target-version 切换测试证明 cache 中只有 parser diagnostics；server benchmark/trace
证明第二次及以后的小编辑进入 `Reparse`。

### I6：fuzz、官方 corpus 编辑回放和最终性能闸门

**建议 agent**：`qa_reviewer` 先只读审查集成 diff；修复仍由相应 owner 完成。

**Goal**

用随机编辑和官方 Vim corpus 证明长期等价、无 panic/越界/非终止，并形成可重复
性能证据。

**Allowed paths**

若 review 后由 write agent 补测试，仅允许：

- `internal/syntax/incremental_test.go`。
- `internal/syntax/benchmark_test.go`。
- `internal/server/server_test.go`。

**Forbidden paths**

- 生产代码；review 发现问题后退回 I1-I5 对应 owner。
- `testdata/official/`、`go.mod`、`go.sum`、generated files。

**Required behavior**

- `FuzzIncrementalParse` 从当前 source 产生合法 byte edits；每一步比较 clean parse。
- seed 覆盖第 8 节的高风险状态。
- 随机序列至少保留 100-step deterministic regression case。
- 对 embedded official corpus 做 head/middle/tail 的 insert/delete/replace 回放。
- 断言旧 AST 未修改、所有 span 有界、unit 前进、fallback 有界。
- 五次 benchmark 达到第 9 节预算；不达标时提供 profile，不直接批准合并。

**Validation**

```sh
gofmt -w internal/syntax/incremental_test.go internal/syntax/benchmark_test.go internal/server/server_test.go
go test -mod=readonly ./...
go test -mod=readonly -race ./...
go vet -mod=readonly ./...
go test -mod=readonly ./internal/syntax -run '^$' -fuzz '^FuzzIncrementalParse$' -fuzztime=30s
go test -mod=readonly ./internal/syntax -run '^$' -bench 'Benchmark(Parse|Reparse)IncrementalCorpus' -benchmem -count=5
```

最终由 primary agent 更新实际受影响的 `docs/architecture.md`、`docs/testing.md` 和
`docs/roadmap.md`。只有 I0-I6 全部完成后，文档才能把增量 AST 标为已实现。

## 11. 每个 patch 的审查清单

### Correctness

- [ ] `Parse(new)` 与 `Reparse(old, new)` 的导出 JSON 是否相同？
- [ ] 旧 `File` 是否保持不变？
- [ ] 所有 span 是否属于新 `File.Source` 且有界？
- [ ] Legacy/Vim9/mixed dialect state 是否完整进入 checkpoint？
- [ ] payload、continuation 和 collected block 是否只有 owner boundary？
- [ ] fragile path 是否保守扩展或 fallback？

### Reuse

- [ ] restart 是否包含一个完整 lookbehind unit？
- [ ] 收敛是否同时比较 bytes、entry state、exit state 和 guard unit？
- [ ] 是否避免把 hash 或 node pointer 当作正确性证明？
- [ ] suffix delta 是否递归作用到每个 nested span？
- [ ] 是否有真实白盒证据证明未重扫复用单元？

### Lifecycle

- [ ] server 是否在锁外解析？
- [ ] cache tree 是否只读？
- [ ] cache tree 是否排除了 target-version 和 truncation diagnostics？
- [ ] stale/canceled/config-old 结果是否仍不能发布？
- [ ] close/reopen 是否不会复用另一个 document lifetime 的树？

### Performance

- [ ] benchmark 是否把 fixture 构造移出计时区？
- [ ] 是否报告五次 `ns/op`、`B/op`、`allocs/op`？
- [ ] 是否与同一最终文本的 full `Parse` 比较？
- [ ] 是否保留完整 AST、diagnostics 和 recovery？
- [ ] 未达标时是否先 profile，而不是引入猜测性 cache/arena/rope？

## 12. 只在 profile 证明需要时实施的优化

以下都不是 I0-I6 的前置条件。

### 12.1 增量 block 重建

触发条件：`buildBlocks` 在中间编辑 benchmark 中稳定占 parser CPU 的 30% 以上。

实施方向：让 unit checkpoint 同时保存 block stack 摘要；从 restart 重建，state
收敛后复用并重定位后缀 blocks。仍需与 full `buildBlocks` 做 differential test。

### 12.2 减少 clone/materialization

触发条件：clone/rebase 的 allocation 或 CPU 稳定超过增量总成本的 30%。

实施方向：先把 unit 内 AST span 改为 unit-relative，并让一个集中
`materializeUnit(base)` 生成绝对 public AST。只有现有多个消费者都能直接读取分块
AST 时，才考虑改变 `File.Commands` 的公开内部 contract。

### 12.3 显式 edit provenance

触发条件：最长公共前缀/后缀比较稳定占增量总时间的 15% 以上。

实施方向：由 `internal/text` 返回相对旧 snapshot 的保守 byte change summary；
server 只有在 cached AST revision 与 change base revision 一致时使用它，否则仍由
`Reparse` 比较完整 source。不要为了省一次比较而牺牲跨 revision coalescing。

### 12.4 Rope 或 piece table

触发条件：profile 证明 `Snapshot` string 拼接和 `indexLines` 已经主导 1 MiB 小编辑
延迟，并且 parser 增量目标已达成。

该改动属于 `internal/text` 的独立里程碑，不与 syntax cache 同 patch。

### 12.5 Parser cancellation hook

当前文件上限为 4 MiB，旧任务完成后已有 stale publish 屏障。只有 profile/soak
证明取消后的无用 parser CPU 明显影响交互延迟时，才在 unit boundary 增加轻量
cancel check。syntax 仍不得依赖 LSP 或 server 类型。

## 13. 明确拒绝的实现

- 在 `go.mod` 加 Tree-sitter binding，然后把现有 parser 包一层。
- 把每次编辑所在行直接当成完整 parse range。
- 只比较 source substring，不比较 dialect/script/block state。
- 把 `Command`、`Expression` 指针当作跨 snapshot stable ID。
- 为复用 suffix 原地给旧 AST 的 span 加 delta。
- 遇到 heredoc/continuation/恢复状态仍强行寻找最近一行 checkpoint。
- 用最终 full parse 覆盖错误的 incremental 结果，却仍报告增量成功。
- 在 feature handler 各自维护一份 syntax cache。
- 在 correctness differential 完成前做 arena、pool、unsafe packing。
- 为了 benchmark 删除 token、trivia、diagnostic、nested command 或 official corpus。

## 14. Definition of Done

- [ ] `syntax.Parse` 保持独立全量 oracle。
- [ ] `syntax.Reparse` 已实现且无新依赖。
- [ ] parser state 覆盖所有会影响下一单元解释的当前状态。
- [ ] safe unit 不切开 continuation、payload 或 collected block。
- [ ] prefix reuse、guard convergence、suffix clone/rebase 均有白盒测试。
- [ ] 所有第 8 节 case 的增量导出 JSON 等于 clean parse。
- [ ] 30 秒 fuzz、100-step deterministic edits、官方 corpus 回放通过。
- [ ] `go test ./...`、`go test -race ./...`、`go vet ./...` 通过。
- [ ] 100 KiB/1 MiB benchmark 达到第 9 节预算。
- [ ] server 使用旧不可变 AST，但 stale revision/config 仍不能发布。
- [ ] workspace batch、public LSP behavior 和 language-support matrix 无虚假变化。
- [ ] 架构、测试和 roadmap 只按已经实现的事实更新。

只满足“结果正确但每次都 full parse”不算完成；只满足“benchmark 变快但与 clean
parse 不等价”也不算完成。

## 15. Tree-sitter 参考阅读顺序

只为理解增量计算不变量阅读以下 runtime 文件：

1. `lib/src/tree.c`：edit 与旧 tree 同步。
2. `lib/src/subtree.h`：子树保存的状态和依赖元数据。
3. `lib/src/subtree.c`：edit 传播、lookahead/column invalidation、copy-on-write。
4. `lib/src/reusable_node.h`：按顺序寻找复用候选。
5. `lib/src/parser.c`：当前 parser state 如何验证旧子树。
6. `lib/src/get_changed_ranges.c`：结构变化如何向下游传播。

阅读后的落点必须是本文件第 6 节的数据结构和不变量，而不是引入 Tree-sitter
API。对 vimls-go 最重要的一句话是：

> 不要根据“用户改了哪一行”猜重算范围；要保存旧 AST 所依赖的 parser state，
> 只在新 source、新 state 和完整语法边界都能证明等价时复用。
