# vimls-go 深度代码审计（2026-09-05）

> 本报告保留审计基线的原始发现与证据。后续修复状态、验证和提交见 [修复记录](code-audit-2026-09-05-fixes.md)，不以本文历史状态判断当前代码。

## 1. 审计快照与结论

审计基线：`c6f4e28db8dead49d8c420a314973d0e486e8028`（`main`，`fix(analysis): fix block scope`）。开始时工作区干净。本次只审计与编写材料，不修复生产代码，不提交、不推送。

结论：发现 16 项可定位的问题，分为 7 项高优先级、9 项中优先级。前 14 项涉及运行时、语义或协议正确性；后 2 项涉及工程验证与发布承诺。高优先级不等同于已确认的远程安全漏洞，表示存在请求阻塞、状态倒退、错误编辑或核心功能失效的具体路径。

最应先处理的是：请求生命周期旁路、非普通文件读取、索引安装时序、重命名安全性及文档同步。无需引入新的 Manager/Coordinator 等架构层；已有取消包装、快照、索引身份、受限文件读取及多根路径解析 API 可以直接复用。

### 发现总表

| 编号 | 优先级 | 问题 | 已取得的证据 |
| --- | --- | --- | --- |
| F01 | 高 | runtimepath 自定义请求绕过公共请求生命周期，并持有发布锁进行扫描 | 真实 LSP 帧进入 `Server.Run`；发现阶段取消注册数为 0、发布锁被持有 |
| F02 | 高 | 部分磁盘读取没有普通文件/读取上限保护，关闭文档可被 FIFO 阻塞 | 取消后 `didClose` 仍阻塞，直至 FIFO 写端提供内容并关闭 |
| F03 | 高 | Rename 接受与已有声明冲突的新名字 | 返回编辑后产生两个同名 Vim9 变量和 E1041 |
| F04 | 高 | Rename 为已改变的关闭文件返回旧位置且没有版本保护 | 磁盘插入一行后，仍返回索引旧 range、`version=null` |
| F05 | 高 | 较旧 watched-file 计算可覆盖较新的完整重建结果 | 受控时序使索引从 `Latest()` 倒退为 `OldEvent()` |
| F06 | 高 | 多根工作区在非空 runtimepath 下只信任首个根 | 第二根的相对 import 失败；已有多根 API 同输入成功 |
| F07 | 中 | 列表切片被推断为元素类型 | `list<number>` 切片得到 `number`；固定版本 Vim 确认结果仍为列表 |
| F08 | 中 | 函数返回推断跨越嵌套函数边界，并混淆 legacy 隐式返回 | 外层推断为内层的 `string`；legacy 无 return 推断为 `void`；Vim 分别返回 42、0 |
| F09 | 中 | 延迟的旧配置拉取响应覆盖较新的配置 | 新配置 99 被旧响应 5 覆盖 |
| F10 | 中 | 发现阶段接受的无 `.vim` 后缀文件无法通过监听增量更新 | `.vimrc` change 被接受但索引仍保存旧文本；注册 glob 仅 `**/*.vim` |
| F11 | 中 | 工作区增量安装后没有通知客户端刷新相关拉取结果 | 关闭文件更新后四类 refresh generation 均为 0 |
| F12 | 中 | workspace diagnostics 对已删除 URI 不返回清空报告 | 携带旧 resultId 再拉取只得到 `items=[]`，缺少该 URI 的空 full report |
| F13 | 高 | LSP 行尾以外的 character 被视为错误，整批变更丢失 | `didChange` end.character=1000 后服务器仍停在 version 1 |
| F14 | 中 | WorkspaceEdit / DocumentSymbol 缺少客户端能力降级 | 空 capabilities 客户端仍收到 DocumentChanges 和层级 DocumentSymbol |
| F15 | 中 | 性能任务只生成统计摘要，没有执行文档承诺的回归门禁 | 空输入 benchreport 退出 0；没有基线或阈值比较 |
| F16 | 中 | 发布工作流与受测试的 Go 发布工具分叉 | 实际 workflow 自行打包，目标/文件名/时间戳策略与 Go 工具不同 |

### 已完成验证

- 初始 `go test ./...` 使用测试缓存通过；收尾又执行 `go test -count=1 ./...`，全部包无缓存通过。需要额外环境变量启用的 oracle 场景不因该命令而自动执行，固定 Vim 的本次交叉验证见下文。
- `go vet ./...`、`make` 通过；未执行 race、coverage 或长时间 fuzz。
- 16 个独立 `TestAudit*` 探针以 `-count=1` 运行通过，其中 15 个验证当前缺陷表现，1 个验证固定版本 Vim 的实际行为。探针的 PASS 表示复现成功，**不是缺陷已修复**。
- Vim oracle：`/Users/chemzqm/lib/vim/src/vim`，Vim 9.2，patches 1–1015；校验 `has('patch-9.2.1015')=1`、`has('patch-9.2.1016')=0`，退出 0，`v:errors=[]`、`:messages` 为空。
- 官方 Vim tag：`v9.2.1015` → `bef0a4f69e54a8ec0a4a9dcc3c9e3e938b403d31`。已检查官方 checkout 状态，原有修改与未跟踪文件均未改动；只在临时目录执行本次编写的无外部副作用 oracle 输入。
- 本机工具链为 `go1.27.0 darwin/amd64`；项目声明 Go 1.26.0、CI 使用 1.26.x。本次不把本机通过等同于完成全部 CI 平台验证。

## 2. 方法与覆盖边界

使用 `open-code-review-delegate` 工作流：OCR 仅负责确定文件集合和规则，语义审计、调用链追踪及复现由主审计代理完成；没有调用 OCR 的外部模型审查模式。

OCR 修复后版本为 `1.11.5 (2190f11fcd)`。基线没有 Git diff，因此 delegate workspace preview 的 `total_files=0`、`reviewed_files=0`、`skipped_files=0`，`coverage_rate=N/A`。创建临时测试后再次 preview 得到 total=1、reviewable=0、excluded=1，唯一排除项是测试路径 `internal/server/audit_probe_test.go`；该文件是本次复现材料，并已由人工检查。不能把“空 diff 已覆盖”说成“全项目 100% 审计”。

另外，对全部已跟踪的非测试 Go 文件和 workflow 执行 delegate rule：89 个 Go 文件、3 个 workflow，共 92 个文件匹配到规则。这是**规则分配覆盖**，不是逐行人工覆盖率。使用 gopls 的符号、引用和诊断辅助定位，再阅读实现、调用方和相关测试。

审查重点：

| 范围 | 本次工作深度 |
| --- | --- |
| JSON-RPC framing、普通请求生命周期、text snapshot | 阅读主要实现，检查取消、错误处理及协议边界 |
| server/workspace、rename、pull diagnostics、refresh、配置 | 重点追踪读→计算→安装→发布的完整路径，编写受控时序和输入复现 |
| workspace 文件发现、受限读取、路径解析、缓存/索引接口 | 对比完整构建、增量更新和查询侧的契约一致性 |
| analysis 类型、作用域、函数返回；syntax 表达式与块结构 | 定向检查边界并用固定 Vim oracle 交叉验证；不是全部 Vim 语法逐形式穷举 |
| 其余语言功能、生成元数据、工具与测试 | 接口/调用方/代表性测试抽查，重点检查是否消费上述不一致状态 |
| CI、发布、性能与项目文档 | 对照实际入口和对外承诺，运行安全的本地检查 |

未实施：生产修复、真实编辑器全功能会话、完整官方 Vim oracle 套件、Windows/Linux 全平台矩阵、发布、线上行为验证、漏洞数据库扫描、长时间 fuzz、RSS/延迟基准。未发现与尚未检查不能等同；本报告不是“没有其他问题”的证明。

## 3. 详细发现

### F01 — 高：runtimepath 自定义请求绕过公共生命周期，扫描期间持有发布锁

源码：[server.go:307](../internal/server/server.go#L307)、[server.go:331](../internal/server/server.go#L331)、[server.go:444](../internal/server/server.go#L444)、[workspace.go:1348](../internal/server/workspace.go#L1348)、[workspace.go:1414](../internal/server/workspace.go#L1414)。

`Run` 的包装顺序是 lifecycle → cancellation → protocol handler，但 lifecycle 自己直接处理 `MethodDidChangeRuntimepath`，没有进入 cancellation handler。由此跳过普通请求的取消 ID 注册、重复 ID/并发数量保护，以及释放连接读循环的 `jsonrpc2.Async`。同时，`DidChangeRuntimepath` 在调用增量扫描前取得 `publishMu`；下层虽暂时释放 `workspaceMu`，却没有释放 `publishMu`。

复现通过真实 LSP 帧发送 initialize、id=42 的 runtimepath 请求、shutdown、exit，在发现文件的 hook 中得到：

```text
runtimepath request id=42: registered=0 publishMuHeldDuringDiscovery=true
```

影响：大型或缓慢 runtimepath 扫描期间，后续输入处理受阻，`$/cancelRequest` 既不能通过正常路径及时处理，也找不到该请求的取消条目；需要 `publishMu` 的文档安装/发布同样等待。普通请求已有的防护不能覆盖这个自定义入口。此项确认了调用链与锁状态，没有把它夸大为已完成真实编辑器延迟测量。

最小修复：把自定义方法解析分发放到公共请求生命周期之内；只在短锁区捕获配置与索引身份，锁外扫描和分析，再验证身份/更新代次后安装。保留 initialize/shutdown 的顺序边界；对于通知，也必须明确更新顺序，不能简单把所有通知异步化。

验收：真实传输层发送“慢 runtimepath 请求 → cancel → didChange/shutdown”，确认取消生效、后续输入可处理、发布锁不包围扫描；同时覆盖通知形式、重复 ID 和旧 runtimepath 结果不得覆盖新配置。

### F02 — 高：受限文件读取契约没有覆盖全部入口

源码：[workspace.go:1181](../internal/server/workspace.go#L1181)、[workspace.go:1504](../internal/server/workspace.go#L1504)、[diagnostics_pull.go:197](../internal/server/diagnostics_pull.go#L197)、[workspace.go:142](../internal/server/workspace.go#L142)。已有安全读取函数：[workspace.go:2010](../internal/server/workspace.go#L2010)。

完整构建和 watched-file 更新已经使用 `readRegularWorkspaceFile`：非阻塞打开、基于文件句柄确认普通文件、大小检查、`LimitReader(max+1)`。但是以下入口没有复用该契约：

- 关闭文档后恢复磁盘内容：`os.ReadFile` 完成之后才检查 `maxFileBytes`。
- runtimepath 增量读入：先完整 `os.ReadFile`，再检查内容大小和剩余预算。
- workspace diagnostic 拉取：直接读取关闭文件，然后才进入后续分析/大小判断。
- runtimepath 根过滤：先 `os.Open` 再 `ReadDir`，打开前没有避免 FIFO 等特殊文件阻塞的保障。

已实测的路径是第一项：将已索引、随后打开的文件替换为 FIFO，再发起 `DidClose`。调用 `cancelAnalysis` 后，它仍等待在读取阶段；只有另行打开 FIFO 写端、写入并关闭后才结束。全部文件变化仅发生在探针的 `t.TempDir()` 中。

```text
didClose stayed blocked on FIFO after lifecycle cancellation;
completed only after a writer supplied data and closed
```

影响：本地工作区文件的类型变化可阻塞同步通知处理；大文件在“检查限制前已经全部读入”，因此声明的 4 MiB 单文件限制不是这些入口的实际读取上界。内存耗尽是静态推导的风险，本次没有构造巨型文件或执行 OOM 压测。context 取消本身不会中断此类阻塞的 `os.ReadFile`。

最小修复：所有磁盘源码读取复用现有受限普通文件读取函数；目录验证另用明确的目录/句柄检查，避免“先阻塞打开再判断”。读失败沿已有保守降级路径处理，不执行脚本、不无限重试。锁外读取仍必须保留安装前的快照有效性检查。

验收：普通文件、超限文件、读取过程中增长的文件、删除/替换为 FIFO、符号链接目标变化，以及取消/关闭全过程均有有界退出测试。不要仅增加一次 `Stat` 就认为消除了检查与打开之间的替换窗口。

### F03 — 高：Rename 只验证名字形式，不验证绑定冲突

源码：[rename.go:137](../internal/server/rename.go#L137)、[rename.go:289](../internal/server/rename.go#L289)。

`validRenameReplacement` 主要检查标识符形式和命名空间兼容性，后续编辑收集没有证明新名字在定义与各引用位置仍绑定到原符号。合法标识符不等于合法重命名。

输入：

```vim
vim9script
var first = 1
var other = 2
echo first + other
```

把 `first` 重命名为 `other`，服务器返回正常 WorkspaceEdit。应用后出现两个 `var other`，引用成为 `echo other + other`，当前分析器给出 `vim/E1041`。Vim9 的声明/遮蔽规则也见固定版本 [vim9.txt](https://github.com/vim/vim/blob/v9.2.1015/runtime/doc/vim9.txt#L529)。本项已经由代码复现确认，不依赖对所有动态 legacy 绑定作推断。

影响：一次重构把原本合法的文件改为无效文件；其他作用域中的名字捕获也缺乏保护，但本次没有把未逐项复现的捕获形式列为独立缺陷。

最小修复：使用现有 scope/declaration/reference 信息检查定义与每个受影响引用的目标绑定；发现明确冲突或无法保证安全的动态情形就拒绝。按声明种类保留各自命名约束，不要只扩大正则。无须增加通用重构框架。

验收：同作用域冲突、内外层捕获、参数/导入别名冲突，以及无冲突成功案例；失败时不得返回部分编辑。

### F04 — 高：关闭文件重命名没有确认磁盘内容仍匹配索引

源码：[rename.go:336](../internal/server/rename.go#L336)。

`renameEdits` 对关闭文件使用捕获的 `workspaceState.index.Source(path)` 构造文本快照，并在这份旧文本里校验待替换字符串。它没有读取磁盘确认旧索引仍对应即将被客户端编辑的文件；返回的关闭文件 TextDocumentEdit 没有版本号。

复现：先索引含 `export def Run()` 的 `lib.vim`，打开导入它的 `main.vim`；在磁盘 `lib.vim` 顶部插入注释，但不先发送文件监听事件；从 `main.vim` 发起 Run → Execute。返回结果仍是旧声明位置：

```text
range={Start:{Line:1 Character:11} End:{Line:1 Character:14}}
version=<nil>
currentOffsetErrors=(invalid text position,invalid text position)
```

行列均为 LSP 的零基坐标。影响取决于编辑器如何处理过时范围：可能拒绝整个 WorkspaceEdit，也可能编辑错误区域。不能把“对旧索引文本校验通过”当作对当前磁盘的安全校验。

最小修复：生成关闭文件编辑前进行受限读取，与计算引用时真正消费的源文本做精确比较；不一致时返回 ContentModified/拒绝本次重构并触发必要的重新索引。打开文件继续依赖精确快照和版本。服务器无法彻底消除响应送出后的外部磁盘变化，这个残余窗口应与当前“响应前已经过时”区别处理。

验收：关闭文件在收集前、收集中、结果组装前改变/删除/替换的场景，以及内容未改变的正常跨文件重命名；绝不返回混合新旧快照的部分结果。

### F05 — 高：watched-file 安装没有重新验证工作区代次

源码：[workspace.go:1685](../internal/server/workspace.go#L1685)、[workspace.go:1832](../internal/server/workspace.go#L1832)、[workspace.go:1843](../internal/server/workspace.go#L1843)。

入口检查工作区“已完成且没有重建”，然后锁外读文件、解析、分析。重新进入安装锁时，只再次检查文件是否已打开，没有证明 `workspaceIndex`/revision 仍是开始计算时的工作区，也没有证明该路径未被更晚的构建更新。最开始的 `rebuilding` 检查不能保证整个计算期间都没有重建。

探针用现有 `beforeWatchedFileInstall` hook 控制时序，不使用 race 模式：

```text
旧文件事件读入 OldEvent()，安装前暂停
→ 磁盘写入 Latest()
→ 完整重建完成，索引含 Latest()
→ 恢复旧文件事件安装
→ 当前索引倒退为 OldEvent()
```

这是“旧结果确实覆盖新结果”的确定性复现，不只是潜在数据竞争猜测。受影响的索引消费方包括工作区导航、引用/重命名及跨文件分析。

最小修复：读前捕获工作区和路径状态身份，安装时在现有锁内验证；重建/根变化/该路径更新后拒绝旧结果，沿已有 full rebuild 或重新调度路径恢复。删除和超限文件的增量分支也需要相同保证，不能只修正常文件分支。

验收：旧更新与重建交错、旧删除与重建交错、根目录移除、文件打开/关闭交错；确保最终状态代表最新被接受的来源，且没有丢失刷新通知。

### F06 — 高：多根路径解析器只包含首个工作区根

源码：[workspace.go:913](../internal/server/workspace.go#L913)、已有正确接口 [resolver.go:158](../internal/workspace/resolver.go#L158)。

`workspacePathResolver` 在循环中首次构造成功就返回 `NewPathResolver(root, searchPaths)`。当 runtimepath 非空时，`searchPaths` 不再包含全部 workspace roots，后续工作区根没有成为解析器的合法根。runtimepath 为空时把 workspace roots 当作 search paths 的行为恰好掩盖了问题。

复现：两个独立工作区根，一个独立且非空的 runtimepath；第二根的 `main.vim` 相对导入同目录 `./lib.vim`。服务器创建的 resolver 返回 `path="" candidates=0`；直接调用仓库已经存在的 `NewPathResolverForRoots`，同样的输入成功解析。

影响：第二及后续根内原本有效的 import 无法进入正确依赖关系，相关跳转和补全随之受影响。不是“尚未实现多根 API”，而是已有多根 API 没有接入 server。

最小修复：使用现有 `NewPathResolverForRoots` 传入完整根集合，同时明确无根、空 runtimepath、首根工作目录以及 runtimepath 搜索优先级，保留当前安全根边界。

验收：两个根 + 非空 runtimepath 的相对/绝对 import、路径补全及实际 server 导航测试；覆盖首根不可用、根移除与禁止越出可信根的用例。

### F07 — 中：列表切片复用了单元素索引的类型规则

源码：[types.go:521](../internal/analysis/types.go#L521)。

`ExpressionIndex` 与 `ExpressionSlice` 共用同一分支，均调用 `indexedType(base)`。对列表，前者取元素，后者仍返回列表；这不是相同的类型运算。

```vim
vim9script
var values = [1, 2, 3]
var firstTwo = values[0 : 1]
echo len(firstTwo)
```

当前分析结果：`firstTwo.Type.Name == "number"`。固定 v9.2.1015 oracle 确认值是 `[1, 2]`、类型为 `v:t_list`。本例当前诊断列表为空，因此本报告只确认错误类型及其下游传播风险，**不声称该示例已经产生错误诊断**。

影响：悬浮信息、推断声明类型以及后续依赖该类型的分析可能得到不正确结果。源码中明确区分的 AST 节点，在分析阶段被过度合并。

最小修复：分离 slice 与 index 的推断；列表切片保留列表及元素类型，其他可切片对象按固定 Vim 版本的真实规则处理，未知容器保持 unknown。

验收：显式/省略上下界、负索引、空切片、索引与切片对照；补充 list/string/blob 各自支持的形式，不从列表规则推导全部容器。

### F08 — 中：函数返回推断不尊重函数所有权及 legacy 默认返回

源码：[types.go:325](../internal/analysis/types.go#L325)。

`inferFunctionReturnsCommands` 从函数声明之后线性扫描命令，遇到第一个 `endfunction` 或 `enddef` 即结束；它会收集嵌套函数的 `return`，并在内层函数结束时提前结束外层扫描。另一个独立表现是没有值返回时统一设置为 `void`，未区分 legacy 与 Vim9。

复现一：

```vim
function! Outer()
  function! Inner()
    return 'text'
  endfunction
  return 42
endfunction
let value = Outer()
```

分析器把 `Outer()` 返回类型推断为 `string`；固定 Vim oracle 返回 42。该示例完全属于 legacy-root，不是项目明确延后的混合方言穷尽支持。

复现二：legacy `function! NoReturn()` 空函数被推断为 `void`，但固定 Vim 返回数值 0。此行为也由固定版本 [userfunc.txt:192](https://github.com/vim/vim/blob/v9.2.1015/runtime/doc/userfunc.txt#L192) 明确规定。

影响：错误类型传播到调用结果、悬浮和后续检查。根因是用“平铺命令中的第一个结束标记”代替已有词法作用域/块所有权信息，并在共享分析器中遗漏方言差异。

最小修复：按已有 block/scope 身份只收集属于该函数的返回；缺省返回按定义的适用方言处理。对分支、混合返回和无法静态证明的 legacy 行为保守合并为 unknown，不新增不可靠诊断。

验收：嵌套函数内外返回不同类型、内层结束后外层返回、legacy 无返回、Vim9 无值返回，以及原有嵌套/不完整恢复用例。修复后同时校验返回类型和调用表达式类型。

### F09 — 中：旧的 workspace/configuration 响应可覆盖更新后的配置

源码：[server.go:829](../internal/server/server.go#L829)、[server.go:851](../internal/server/server.go#L851)。

配置拉取正确使用 `Async` 释放读循环，但响应回来后直接 `applyWorkspaceConfiguration`，没有请求序号或配置版本检查。释放读循环意味着等待期间新通知/新拉取可以进入；响应完成次序不等于配置意图的新旧次序。

复现：旧配置请求开始但暂不返回；应用较新的 `diagnostic.maxNumber=99`；随后允许旧请求返回 `maxNumber=5`。最终配置为 5。

影响：初始化的延迟响应或多次更新的乱序响应可能撤销用户刚修改的设置。当前探针直接控制服务端配置调用交错，没有声称已用真实编辑器重现其 UI 表现。

最小修复：为配置更新保留单调递增的代次，拉取开始记录代次，应用响应前确认仍是当前更新；推送设置和拉取响应必须进入同一新旧判断。解析/应用可以继续复用现有函数，避免构建新的配置管理层。

验收：旧 pull → 新 push → 旧响应、两个 pull 反序返回、失败响应及 shutdown 中返回；只有最新有效更新影响配置、分析修订与 refresh。

### F10 — 中：文件发现和监听更新采用了不同的文件集合

源码：[files.go:283](../internal/workspace/files.go#L283)、[workspace.go:1714](../internal/server/workspace.go#L1714)、[workspace.go:1934](../internal/server/workspace.go#L1934)。

文件发现认识 `.vimrc`、`_vimrc` 等配置文件及特定 runtime 目录中的无扩展名文件；主动注册的 watcher 却只有 `**/*.vim`。更重要的是，即使客户端显式发送 `.vimrc` Changed/Create 事件，增量代码也因后缀不为 `.vim` 跳过，然后返回成功，阻止上层退回完整重建。

复现：初始 `.vimrc` 定义 `OldName()`，磁盘改为 `NewName()`，显式传入 Changed 事件。处理返回 true，索引仍是 `OldName()`。删除事件的分支不同，会触发重建回退，不能据此认为修改事件也得到支持。

影响：这些已承诺支持、已经进入索引的文件在外部修改后长期陈旧，直到打开文件或其他原因触发重建。

最小修复：让发现、事件资格判断及 watcher 模式共享同一文件选择契约。对已知配置名和允许的无扩展名 runtime 位置定向监听；不要粗暴扩展为整个根的所有文件都解析。

验收：`.vimrc`、`_vimrc`、普通 `.vim`、合法无扩展名文件的 create/change/delete，以及应忽略文件；同时验证主动 watcher 与显式事件两条路径。

### F11 — 中：工作区增量安装后缺少结果刷新通知

源码：[workspace.go:1348](../internal/server/workspace.go#L1348)、[workspace.go:1857](../internal/server/workspace.go#L1857)、[server.go:1438](../internal/server/server.go#L1438)、现有刷新实现 [refresh.go](../internal/server/refresh.go)。

完整重建与部分配置路径有刷新安排，但 watched-file 成功增量安装只调用 `startWorkspaceDependents`；runtimepath 增量只重新调度打开文档的分析。这不保证已显示的 pull diagnostics、Code Lens 等结果被客户端重新请求。已有文档再次分析成功，也不能依靠“第一次分析完成”的刷新分支补上这个事件。

探针启用客户端对 diagnostics、semantic tokens、inlay hint、Code Lens refresh 的支持，更新一个关闭文件并通过 `DidChangeWatchedFiles` 安装。四类 refresh generation 均保持 `[0 0 0 0]`。

影响：依赖工作区状态的已有结果可能继续停留在客户端缓存，直到客户端主动轮询、用户操作或另一个事件触发刷新。例如，关闭文件诊断发生变化，或关闭文件增删引用影响打开文件的 Code Lens。此项没有假定所有客户端都不轮询，也没有断言每次文件更新都会影响全部四类结果。

最小修复：在成功安装实际被消费的数据之后，锁外调度需要的 capability-gated、可合并 refresh。诊断/Code Lens 与工作区变化的依赖需明确；semantic tokens、inlay hint 仅在实际消费的状态变化时刷新，不能仅依据“排入了依赖队列”盲目刷新所有功能。失败、过时或无内容变化的结果不应触发无意义刷新。

验收：已有缓存结果的跨文件变化、没有打开依赖文件的关闭文件诊断变化、runtimepath 增删、无变化事件、过时计算、客户端未声明 refresh 支持，以及刷新合并次数。

### F12 — 中：已删除文件的 workspace diagnostics 没有清空报告

源码：[diagnostics_pull.go:150](../internal/server/diagnostics_pull.go#L150)、[diagnostics_pull.go:219](../internal/server/diagnostics_pull.go#L219)、[diagnostics_pull.go:233](../internal/server/diagnostics_pull.go#L233)。

服务器把 `previousResultIds` 用于当前仍枚举到的文档的缓存命中；枚举不到的关闭文件会从服务器缓存删除，却没有在输出中生成该 URI 的空 full report。

复现：第一次 workspace pull 为 `bad.vim` 返回 1 条诊断和 resultId；删除文件并处理 Deleted 事件；下一次携带旧 `previousResultIds` 拉取，响应顶层 `items=[]`，而非包含 `bad.vim` 且该文档诊断 `items=[]` 的报告。

两种空数组含义不同：前者“本次没有任何 URI 更新”，后者“这个 URI 的诊断已清空”。LSP workspace pull 允许按 URI 分批、增量传递，不能当作每次响应完整替换整个集合。[LSP 3.18 pull diagnostics 定义](https://github.com/microsoft/language-server-protocol/blob/gh-pages/_specifications/lsp/3.18/language/pullDiagnostics.md) 描述了其报告模型；[官方客户端实现](https://github.com/microsoft/vscode-languageserver-node/blob/main/client/src/common/diagnostic.ts#L475) 的 workspace 消费逻辑只遍历返回的 URI，对 full report 调用 `diagnostics.set(uri, items)`，不因某个 URI 缺席自动清空它。

影响判断：服务器“没有删除清空报告”的行为已由探针确认；对于按上述模式维护诊断集合的客户端，旧诊断可能残留，这是结合客户端代码作出的互操作性推断，**本次没有启动真实客户端验证残留 UI**。官方客户端链接为审计时的 main，可能随上游变化；不将这里描述为规范中一条未经找到的专门 deleted-file MUST 条款。

最小修复：对本服务先前报告过、此次确定已删除/退出结果集的 URI，返回空 full report 和相应 resultId；不要清除不属于本服务或不能确认已删除的文档。处理 partial results、根移除与当前打开 overlay 时保持同一规则。

验收：客户端保留旧诊断集合的集成夹具，验证删除后确实收到针对该 URI 的清空动作；另测权限暂时失败、文件重新出现、根移除、仍打开但磁盘删除的文件。

### F13 — 高：合法的行尾越界 Position 导致文档变更整体丢失

源码：[snapshot.go:87](../internal/text/snapshot.go#L87)、[snapshot.go:175](../internal/text/snapshot.go#L175)、[server.go:725](../internal/server/server.go#L725)。

`encodedOffset` 对 character 超过当前行长度返回 `ErrInvalidPosition`；`DidChange` 因此拒绝整批变更并只记日志。LSP 的 Position 明确允许 character 大于行长度，并要求将其折回行尾。[LSP 3.18 Position 定义](https://github.com/microsoft/language-server-protocol/blob/gh-pages/_specifications/lsp/3.18/types/position.md)

复现：打开 version 1 的 `var value = 1`，发送 version 2 的行替换，range 从该行 character=0 到 1000，新文本为 `var value = 2`。`DidChange` 返回 nil，但服务器仍是 version 1 和旧文本。

影响：客户端已经接受新版本，而服务器以旧版本继续分析；后续增量编辑的位置基于客户端新内容，可能进一步放大偏差。合法协议输入不应该进入“忽略错误通知”的降级分支。

最小修复：在文档/协议边界按协商编码把超出行尾的 character 限制到实际行尾。只修这一条规范要求；不要顺带接受无效行号、负位置或 UTF-16 代理对中间的非法边界。项目内部需要严格位置检查的调用应保留清晰语义。

验收：UTF-8/16/32 的恰好行尾与超出行尾、CRLF、空行、末尾换行、astral 字符、多项按序变更；确认版本、文本、位置索引与诊断全部推进到新快照。

### F14 — 中：未按客户端能力选择 WorkspaceEdit 与 DocumentSymbol 结果

源码：[rename.go:336](../internal/server/rename.go#L336)、[semantic_actions.go:638](../internal/server/semantic_actions.go#L638)、[server.go:925](../internal/server/server.go#L925)。

Initialize 没有保存/使用 `workspace.workspaceEdit.documentChanges` 与 `textDocument.documentSymbol.hierarchicalDocumentSymbolSupport` 来选择结果形式。Rename 固定返回 `DocumentChanges`，CodeAction 也构造这种编辑；DocumentSymbol 固定返回层级 `DocumentSymbolSlice`。

复现使用空 capabilities 初始化，再调用 Rename 和 DocumentSymbol：前者只有 `DocumentChanges`、没有 `Changes`，后者为 `DocumentSymbolSlice`。这不是服务器未实现功能，而是功能结果不适用于未声明这些可选能力的客户端。

[LSP WorkspaceEdit 定义](https://github.com/microsoft/language-server-protocol/blob/gh-pages/_specifications/lsp/3.18/types/workspaceEdit.md) 规定基于客户端能力使用 documentChanges，并为无该能力的客户端提供 changes 形式；[DocumentSymbol 定义](https://github.com/microsoft/language-server-protocol/blob/gh-pages/_specifications/lsp/3.18/language/documentSymbol.md) 区分层级 DocumentSymbol 与扁平 SymbolInformation 结果。

最小修复：保存两个必要的 capability 布尔值，在既有转换出口选择形式；支持时保留带版本的编辑和层级符号，不支持时提供 URI→TextEdit 的 changes 及 SymbolInformation。该降级不能替代 F04 的磁盘一致性验证。

验收：capability absent/false/true 三种初始化，以及 Rename、含 WorkspaceEdit 的 CodeAction 和有嵌套函数的 DocumentSymbol。检查实际 JSON 输出，而不仅是 Go 类型可编译。

### F15 — 中：所谓 benchmark regression lane 只生成摘要

源码：[scheduled.yml:60](../.github/workflows/scheduled.yml#L60)、[benchreport/main.go:25](../tools/benchreport/main.go#L25)、[benchreport/main.go:86](../tools/benchreport/main.go#L86)。承诺：[testing.md:309](testing.md#L309)、[testing.md:330](testing.md#L330)、[testing.md:376](testing.md#L376)。

文档规定 completion 100 ms 预算、时间回归 15% 和分配回归 20% 的限制，并称 scheduled lane 做回归检查。实际 workflow 运行五次 benchmark，再调用 benchreport 打印 median/P95；工具只有输入参数，没有基线、阈值或超限失败路径。

还实测了失去全部样本的行为：

```sh
go run ./tools/benchreport < /dev/null
```

输出 `No benchmark samples found.`，退出码 0。也就是说，即使 benchmark 正则因重命名不再选中任何目标，摘要阶段也无法揭示验证已失效。

影响：目前绿色任务只能说明命令完成，不能证明没有性能回归。这里没有声称已经测得性能退化，也没有执行耗时 benchmark。另需区分“几次 benchmark 聚合样本的 P95”和“用户请求延迟分布的 P95”，两者不是同一个指标。

最小修复：保持 Go 工具入口，定义基线提交/样本、最低有效样本集合、稳定运行环境及比较策略；缺样本或确认超限必须非零退出。对嘈杂共享 runner 的失败需要复测/批准策略，但不能只靠任务名称承担门禁职责。若暂不实现，应将文档改为明确的“收集报告，人工对比”。

验收：空输入、缺少指定 workload、格式错误、稳定样本、恰好阈值、明确超限，以及经批准的基线更新；至少一个测试证明“发生回归时 CI 真的失败”。

### F16 — 中：真实发布流程没有使用文档所述的受测试 Go 打包路径

源码：[release.yml:53](../.github/workflows/release.yml#L53)、[release/main.go:25](../tools/release/main.go#L25)、[release/main.go:74](../tools/release/main.go#L74)。承诺：[testing.md:381](testing.md#L381)。

文档说推送 `v*` tag 会调用 `tools/release`，并使用提交时间固定 tar/zip 条目。实际 workflow 自行在 shell 中编译、复制文件，再执行系统 `tar`/`zip`，没有调用该 Go 工具，也没有将提交时间统一应用到归档条目。

| 项目 | `tools/release` | 实际 release workflow |
| --- | --- | --- |
| 目标 | Linux/macOS/Windows × amd64/arm64，6 个 | 另含 Linux armv7、FreeBSD amd64，8 个 |
| 文件名 | `vimls-go_<version>_<os>_<arch>` | `vimls-<tag>-<os>-<arch>` |
| 打包内容布局 | 版本目录下的二进制 | 根目录二进制、README、可用 LICENSES 等 |
| 时间戳 | 显式使用 `-epoch` | 复制/构建产生的文件时间，没有同等归一化 |
| 验证路径 | Go 归档函数及其测试 | 另一份 shell 实现，不能由前者测试担保 |

影响：按文档运行本地工具不能得到同一发布资产契约；归档稳定性相关测试通过也不能证明实际发布归档稳定。这里只确认实现分叉，没有实际推送 tag、生成发布或比较远程二进制哈希。

最小修复：明确唯一生产发布入口，让 workflow 调用受测试的 Go 实现；整合时必须保留当前已经发布的 8 个目标、下载文件名及必要附件契约，不能直接删掉 shell 就悄悄丢失两种平台。更新文档并通过实际入口验证可重复打包。

验收：同提交、同工具链、同 epoch 在两个临时目录构建，归档哈希一致；目标、归档名称、目录布局、版本和 checksum 的测试覆盖实际工作流输入。是否发布仍须单独授权。

## 4. 复现材料与验收记录

探针源文件：[code-audit-2026-09-05-probes_test.go.txt](code-audit-2026-09-05-probes_test.go.txt)。它保留为 `.txt`，不进入正常 `go test ./...`，避免把“断言旧缺陷”混入永久回归测试。

从仓库根目录通过 [Go overlay 配置](code-audit-2026-09-05-overlay.json) 加载：

```sh
VIM_EXECUTABLE=/Users/chemzqm/lib/vim/src/vim go test \
  -overlay=docs/code-audit-2026-09-05-overlay.json \
  ./internal/server -run '^TestAudit' -count=1 -v -timeout 60s
```

注意：FIFO 探针针对本次 macOS/Unix 环境，不能直接当作 Windows 测试；Vim 路径可替换，但 oracle 严格要求 v9.2.1015。未设置 `VIM_EXECUTABLE` 时只有 oracle 会跳过。后续修复相应问题后，原探针预期会失败，应将所需场景改为“断言正确行为”的正式测试，不要机械保持其通过。

| 探针 | 关联发现 |
| --- | --- |
| `TestAuditRuntimepathDispatch` | F01 |
| `TestAuditCloseFIFO` | F02 |
| `TestAuditRenameConflict` | F03 |
| `TestAuditRenameClosedDiskStale` | F04 |
| `TestAuditWatchedUpdateAfterRebuild` | F05 |
| `TestAuditMultiRootResolver` | F06 |
| `TestAuditListSliceType` | F07 |
| `TestAuditNestedFunctionReturn`、`TestAuditLegacyImplicitReturn` | F08 |
| `TestAuditConfigurationOutOfOrder` | F09 |
| `TestAuditWatchedExtensionlessFile` | F10 |
| `TestAuditWatchedUpdateNoRefresh` | F11 |
| `TestAuditWorkspaceDiagnosticDeletedFile` | F12 |
| `TestAuditPositionPastLineEnd` | F13 |
| `TestAuditClientCapabilityFallback` | F14 |
| `TestAuditPinnedVimOracle` | F07/F08 的固定版本交叉验证 |

## 5. 修复顺序与未决风险

### 5.1 建议的最小修复顺序

| 顺序 | 范围 | 验收重点 |
| --- | --- | --- |
| 1 | F01/F02：入口生命周期、锁外计算与受限读取 | 所有相关入口均可有界退出，不让特殊文件或目录扫描阻塞连接/发布 |
| 2 | F05/F09/F11/F12：状态安装、新旧判断与客户端可见性 | 受控交错下不倒退；新结果成功安装后刷新，消失的诊断被明确清空 |
| 3 | F03/F04：重命名安全性 | 明确冲突与过时磁盘均拒绝，不返回部分或混合快照编辑 |
| 4 | F13/F14：协议边界与结果能力协商 | 用真实 JSON 帧/结果断言验证编码、版本与降级形式 |
| 5 | F06/F10：根与文件集合的一致性 | 多根 import 有效；所有被支持发现的文件有相应更新路径 |
| 6 | F07/F08：类型推断所有权 | 固定版本 oracle + AST/analysis 类型断言同时通过 |
| 7 | F15/F16：工程门禁与实际发布入口 | 门禁能失败；被测试的发布实现就是生产运行的实现 |

这些顺序是风险排序，不是扩大范围授权。修复时按上述相邻责任组织小变更；既有 `AGENTS.md` 要求的源语言/协议边界仍保持不变。原有缺陷探针应转换为正确行为测试后进入所属包，不能把审计文本附件直接加入正常套件。

### 5.2 需要继续测量的设计风险（不计入 16 项已定位问题）

**R1：局部限流不等于进程总资源上界。** [server.go:1314](../internal/server/server.go#L1314) 的前台 cache miss 直接解析/分析；后台分析 worker 的上限 6 不覆盖所有这样的前台工作。普通请求数量限制也不是按文件大小加权的内存预算。[workspace.go:745](../internal/server/workspace.go#L745) 先汇集源码再批量生成 AST/analysis，256 MiB 指的是索引源码预算，而不是解析堆或进程 RSS 上界。建议先测多文件冷缓存并发、解析堆与峰值 RSS，再决定是否在现有 parse 入口增加共享的有界并发；不能只靠把每个局部数字调小。

**R2：全量重建的安全重试可能牺牲持续输入时的活性。** [workspace.go:286](../internal/server/workspace.go#L286) 在所有打开快照与构建开始时不一致时丢弃结果并再次去抖重建。这保证不会安装旧 overlay，是正确的安全边界；但慢构建与持续编辑叠加时，工作区 readiness 可能很久不能完成。需要固定慢构建 + 持续编辑场景，测量等待工作区请求的完成时间。优化不能移除旧结果保护；优先考虑复用未变文件结果、只重算真正变化输入，而不是引入新的协调层。

**R3：取消等待不等于中断正在执行的 CPU 工作。** 同快照 in-flight 合并已存在，等待者取消不会破坏共享计算；但 `syntax.Parse`/`analysis.Analyze` 的调用本身没有 context 参数，多个路径只在调用前后观察取消。[server.go:1407](../internal/server/server.go#L1407)、[parse.go:39](../internal/workspace/parse.go#L39)。应测合法但高度嵌套/关系密集输入的实际耗时，再决定是否需要更细粒度的有界工作检查。未执行相关长耗时/内存压力测试，不把此风险报告为已复现的 hang。

以上三项均有静态依据但缺少本次负载测量，因此不与确定性缺陷混为一谈。

### 5.3 现有正确设计，应保留

- immutable document snapshot、版本/配置修订以及多处安装/发布前的有效性检查已经存在；F05 是某条路径漏用契约，不是全项目没有快照模型。
- 相同内容的 parse/analysis 复用与 in-flight 合并已经存在；缓存不仅检查 content ID，还校验实际源文本与配置角色。
- JSON-RPC framing 有 8 KiB header、16 MiB message 上限；普通请求有取消、重复 ID 和数量保护。F01 是旁路问题，不应重写整个传输层。
- 普通文件受限读取、多根路径解析及可合并 refresh 都已有可复用实现。
- 对未知 Ex 命令和动态 legacy 语义保持保守，正常语言服务器分析不执行用户脚本；本次 oracle 只执行审计者编写的临时输入。

共同的设计改进方向是把现有契约贯彻到所有入口：相同数据必须具有相同读取边界；所有耗时结果必须凭原始身份安装；重构必须比只读查询更严格地确认目标；客户端缓存必须有明确更新或清空事件。无需为这些问题引入新的层级或替换解析技术。

## 6. 续审与交付状态

本文件、探针文本和 overlay 是持久记录，下一轮可直接按 F 编号复查。源码链接与行号均对应开头的审计提交，后续改动后需重新定位。

- 生产代码修复：未实施，全部 F 项仍待修复。
- 审计探针：已从正常测试目录移出；从仓库根目录使用所附 overlay 重放，16/16 通过，包耗时 1.208 s。该数字是本次探针执行时间，不是产品性能基准。
- 探针格式化与诊断：已执行 gofmt；gopls MCP 对新增临时测试文件未提供 package metadata，已改用本机 `gopls check internal/server/audit_probe_test.go` 检查，通过且无输出；overlay 重新编译运行亦通过。
- F15 空样本检测：已复跑，确认退出 0。
- F16 发布一致性：源码对照确认，没有执行发布或远程写操作。
- 最终 `go test -count=1 ./...`：全部包通过；其中 integration 包 13.948 s、server 包 7.791 s。这是测试命令报告的包耗时，不是总墙钟时间或产品延迟。
- 收尾 `go vet ./...`、`make` 再次通过；探针 `gofmt -l` 无输出。三个新增文件的 whitespace 检查无告警，文档本地链接目标均存在，16 个详细 F 条目和 16 个 TestAudit 函数数量已核对。
- 最终 HEAD 仍为审计基线；Git 状态只新增本报告、探针文本和 overlay 三个未跟踪文档文件，没有生产代码差异。`make` 仅更新项目既有忽略规则下的本地构建产物。

OCR 规则取用命令（审计期间已成功执行）：

```sh
git ls-files '*.go' '.github/workflows/*.yml' \
  | rg -v '_test\.go$' \
  | xargs /Users/chemzqm/lib/npm/bin/ocr delegate rule --format json
```

收尾再次查看该命令帮助时，指定 OCR 路径又暂时不存在；此前成功取得的 v1.11.5 规则结果及本报告复现不受影响。未擅自安装或修改 OCR。这是审计工具可用性记录，不计作 vimls-go 的缺陷。
