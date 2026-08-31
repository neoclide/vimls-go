# 增量解析待办

本文记录 [`incremental-parsing-plan.md`](incremental-parsing-plan.md) 尚未完成的工作。
计划文档仍是行为契约和验收标准的来源；本文件只保留当前差距、执行顺序和完成证据，
避免把“安全回退”误记为对应的增量阶段已经实现。

基线：`7aec1c0`。当前已有可用的保守实现：平坦、独立语法单元可以增量重解析；
状态性语法、block、多行 payload 和不可信恢复路径会退化为完整 `Parse`。Server 已使用
纯 parser AST cache，并保留 snapshot/revision/configuration 的 stale barriers。

## 执行规则

- 严格按 I0-I7 的依赖顺序推进；I1-I5 不并发修改 `internal/syntax`。
- `Parse` 永远是独立的 full-source oracle；不能在增量结果生成后用 `Parse` 覆盖错误结果。
- 每个小任务单独提交，只运行相关测试；阶段完成前补齐计划要求的完整 gate。
- 只有验收证据真实存在时才勾选；复杂输入仅 full fallback 不代表 scanner/structure
  convergence 已完成。
- 每次只 stage 当前任务的 Allowed paths；Go 修改后检查 gopls diagnostics。
- 提交前执行 `/ponytail-review` 并解决发现；提交命令使用
  `VIMLS_PONYTAIL_REVIEWED=1`。若命令不可用，先解决工具可用性问题再提交。
- I7 完成后再按已实现事实更新 architecture、testing、roadmap 和语言支持文档。

## I0：差分工具和基线证据

已具备 JSON、alias topology、span、旧树不可变和 downstream analysis 比较器，以及
10/100/1024 KiB 的基础 benchmark 入口。

- [x] 让 AST 反射覆盖检查遍历 map；新增 pointer/slice/map 字段遗漏 clone 时测试必须失败。
- [x] 把计划第 13 节的文本、方言、command boundary、多行 owner、structure 和恢复
  编辑矩阵固化成后续 I3-I7 共用的数据表。
- [x] 登记生命周期/并发场景、已有测试证据和 I6 缺口；I0 不修改被禁止的
  `internal/server/`，也不把登记表冒充成可执行 server 矩阵。
- [x] 在固定 runner 上记录 Legacy/Vim9、10/100/1024 KiB full Parse 的五次原始样本和
  median：`ns/op`、`B/op`、`allocs/op`。
- [x] 记录 Go 版本、OS/CPU、`GOMAXPROCS` 和 corpus 生成方式。

I1 改动后的 full Parse 回归门槛属于 I1b 验收，继续保留在 I1b，不作为 I0 基线任务的
前置条件。

完成证据：I0 测试文件、五次 benchmark 原始输出、基线摘要和下面的生命周期证据映射。

### I0 共享矩阵证据

| 分组 | 共用数据表 | 提交 |
| --- | --- | --- |
| 文本和位置 | `incrementalTextPositionGroups` | `e9a8f37` |
| 方言和 scanner state | `incrementalDialectStateScenarios` | `f21a82c` |
| command boundary 和 lookahead | `incrementalCommandBoundaryScenarios` | `58b4d81` |
| 多行 owner | `incrementalHeredocOwnerScenarios`、`incrementalTextBodyOwnerScenarios`、`incrementalEmbeddedOwnerScenarios` | `f5431f2`、`b95a323`、`8a288ad` |
| structure state | `incrementalStructureStateScenarios` | `4d42bc8` |
| 错误恢复 | `incrementalRecoveryScenarios` | `f3d6c92` |

这些表已经由对应的 `TestIncrementalEditMatrix*`、`TestReparse*Matrix` 和边界/恢复测试
消费。当前保守 `Reparse` 会执行差分检查或安全回退；I0 的完成不表示 I3-I5 的复用与
收敛门槛已经完成。

生命周期行跨 syntax/server package，I0 只登记共用场景和已有证据；缺少的端到端 cache、
revision 和 publish 断言仍由 I6 完成。

| 生命周期场景 | 已有证据 | 后续留项 |
| --- | --- | --- |
| analysis 期间 didChange | `TestServerDocumentHandlersCancelStaleAnalysis` | I6 增加后台 analysis 端到端回归 |
| 多个中间 revision | `TestAnalysisQueueCoalescesRapidDocumentChanges` | I6 明确证明每个旧 revision 不 cache、不 publish |
| didSave whole content | `TestServerDocumentSynchronization` | I6 补 parser cache/revision 回归 |
| close/reopen、重复 didOpen | `TestServerDocumentParserCacheDoesNotCrossCloseReopen`、`TestServerStaleAnalysisCannotRestoreClosedParserCache`、`TestServerRepeatedDocumentOpenReplacesParserCache` | 已有生命周期 cache 隔离证据 |
| target/config/graph revision | `TestTargetVersionCompatibilityDiagnosticsReanalyze`、`TestGraphRevisionRejectsStaleDiagnostics`、`TestWorkspaceImportGraphTracksOpenDocumentChanges` | I6 补齐组合后的 cache/publish barrier 端到端证据 |
| 同一旧 `File` 并发 Reparse | `TestReparseConcurrentReaders` | I5/I7 执行最终 race gate |

### I0 full Parse 基线（`7ac3ac4`）

Runner：Go `go1.26.5 darwin/amd64`；Darwin `25.5.0`；Intel Core i7-9750H
2.60 GHz；`GOMAXPROCS` 未显式设置，运行时使用 12 个逻辑 CPU。benchmark 在计时区外
生成 source：Legacy 重复 `let value = 1`/`echo value`，Vim9 以 `vim9script` 开头并重复
`var value = 1`/`echo value`，最后截断为精确的 10、100 或 1024 KiB。

命令：

```sh
go test -mod=readonly ./internal/syntax -run '^$' \
  -bench '^BenchmarkParseIncrementalCorpus/(legacy|vim9)/(10|100)KiB$' \
  -benchmem -count=5
go test -mod=readonly ./internal/syntax -run '^$' \
  -bench '^BenchmarkParseIncrementalCorpus/(legacy|vim9)/1024KiB$' \
  -benchmem -count=5
```

每个单元格依次记录五次原始样本；括号内是中位数。

| Dialect/size | `ns/op` | `B/op` | `allocs/op` |
| --- | --- | --- | --- |
| Legacy 10 KiB | 5205000, 5173408, 5020498, 5072345, 5238732 (5173408) | 1868575, 1868482, 1868481, 1868483, 1868484 (1868483) | 4976, 4976, 4976, 4976, 4976 (4976) |
| Legacy 100 KiB | 394189204, 386659041, 383397982, 382246240, 383365382 (383397982) | 27107122, 27105386, 27103688, 27103613, 27103576 (27103688) | 49355, 49353, 49352, 49351, 49351 (49352) |
| Legacy 1024 KiB | 84856873856, 85450522463, 84625935263, 107003683545, 113796939971 (85450522463) | 301413208, 301381288, 301381320, 301381352, 301381432 (301381352) | 504471, 504435, 504437, 504439, 504438 (504438) |
| Vim9 10 KiB | 3099157, 3236239, 3134162, 3102359, 3185215 (3134162) | 3209327, 3209324, 3209423, 3209326, 3209324 (3209326) | 14427, 14427, 14427, 14427, 14427 (14427) |
| Vim9 100 KiB | 37961257, 38176395, 38469971, 37723004, 39290586 (38176395) | 41165694, 41165699, 41165704, 41165699, 41165702 (41165699) | 143777, 143777, 143777, 143777, 143777 (143777) |
| Vim9 1024 KiB | 500611111, 484851802, 462817566, 488948098, 509644208 (488948098) | 441399309, 441399304, 441399309, 441399320, 441399376 (441399309) | 1470700, 1470700, 1470700, 1470700, 1470700 (1470700) |

## I1：明确 full parser 的真实阶段

当前 `parseSourceRange` 仍在一个函数内保存 scanner 局部状态，并连续执行 scan、
`buildBlocks`、details 和 finish；尚无计划要求的 concrete `sourceParser`。

### I1a：封装真实 parser 状态

- [ ] 增加 package-private `sourceParser`，只承载当前已有状态，不增加 interface。
- [ ] 把 initial/active dialect、`scriptVersion`、dialect/aggregate stack、continuation、
  collected block、lambda 和恢复状态移入该类型。
- [ ] `Parse` 和 `parseSourceRange` 的导出 AST、token、diagnostic 顺序保持不变。
- [ ] Legacy/Vim9 command consumer 保持独立；不合并成通用方言 parser。
- [ ] `Parse` 不调用 `Reparse`；I1 不复用旧树。

### I1b：划分阶段

- [ ] 将现有流程拆成明确的 scan、structure、details、finish 方法。
- [ ] coalesce/source ownership 在 structure 前完成。
- [ ] full `buildBlocks` 和所有 finish pass 仍对每次完整 `Parse` 执行。
- [ ] 保存 suppression 前的 scanner/detail diagnostics，最终顺序不变。
- [ ] finished commands 不依赖临时 `logical`/`boundaryExpression`。
- [ ] full Parse 相对 I0 基线的 time 回退不超过 15%，allocation 回退不超过 20%。

完成证据：official corpus 与 syntax package 测试通过；I0 比较器证明 public `Parse`
结果不变；full Parse benchmark 未越过回退线。

## I2：完整 unit、checkpoint 和 structure state

当前 metadata 已记录物理 unit、简化 parser state 和 `[]BlockKind` 路径，但缺少完整
structure/recovery 状态、自检和 diagnostics 分层。

- [ ] unit owner 覆盖 heredoc、text body、keymap、Legacy/Vim9 continuation、collected
  command/autocmd/legacy block、直接 `finish` 和 `OpaqueTail`。
- [ ] 同一 logical line 上由 `|` 分隔的 commands 属于同一 unit。
- [ ] active payload、未完成 continuation 和 collected block 状态下不得建立 checkpoint。
- [ ] 未知用户/未来 command 在边界可信时不是天然 fragile。
- [ ] checkpoint state 覆盖所有影响下一 unit 的 scanner 状态，并逐字段比较。
- [ ] 用具体 `structureState` 记录 block path、def/function/class/interface/enum、
  try/catch/finally、invalid-for、method recovery、Vim9 redir 等状态。
- [ ] 分开保存 scanner diagnostics 与 detail/finish diagnostics。
- [ ] 增加 metadata self-check：顺序、完整 coverage、前进、span、owner、state round trip。
- [ ] 测试相同 source 多次 `Parse` 产生相同 unit/state。

完成证据：`Test(ParseUnit|Checkpoint|StructureState|Incremental)` 白盒断言通过，且
public `Parse` 输出不变。

## I3：安全前缀复用

当前已有 byte dirty interval、相同 source 返回旧指针、lookbehind restart 和 flat unit
clone；尚未从 checkpoint state 恢复 scanner，也没有按计划只复用扫描产物再统一完成
structure/details/finish。

- [ ] 从 restart unit 的完整 entry state 恢复 scanner，并扫描到 EOF。
- [ ] restart 命中 dirty-adjacent unit；payload/body/continuation 命中时退到 owner，再额外
  回退一个 lookbehind unit。
- [ ] 编辑首个有效 `vim9script` 时从 byte 0 开始；编辑 `OpaqueTail` 时从直接
  `finish` owner 开始。
- [ ] unit coverage、owner、state 或 metadata self-check 失败时 full `Parse`。
- [ ] 前缀只复用 command headers、tokens 和 scanner diagnostics；不提前复用 details。
- [ ] 对合成扫描结果全量执行 structure、details 和 finish。
- [ ] 白盒计数证明 restart 前 unit 未进入 scanner。
- [ ] I0 共享矩阵中的 head/middle/tail insert/delete/equal-length/length-changing 和连续
  edits 均与 `Parse` 等价；tail 小编辑还必须优于 full Parse。

## I4：scanner guard 和后缀扫描复用

当前 suffix candidate 只比较 raw bytes、物理行边界和 fragile 标志；记录的 entry/exit
state 没有参与收敛判断，guard output 也未比较。

- [ ] candidate entry state 与旧 unit entry state 完全相同。
- [ ] 新 scanner 已越过 `NewEnd`，候选旧 unit 位于 `OldEnd` 后，并且 delta 能唯一映射
  新旧边界。
- [ ] 新旧 candidate 的原始 source bytes 完全相同；hash 只能快速排除，不能替代比较。
- [ ] 完整解析一个 clean、non-fragile guard unit。
- [ ] 比较 guard exit state、command headers、tokens 和 scanner diagnostics。
- [ ] state mismatch、fragile guard、equal/non-equal-length edit 都有白盒测试。
- [ ] 白盒计数证明 guard 后 unit 未进入 scanner。

## I5：structure convergence 和 detail AST 复用

identity-aware clone/rebase 与 alias tests 已存在；structure state 当前没有参与复用，
含 block 的文件通常直接 full fallback。

- [ ] 对合成 scanner 结果全量运行 `buildBlocks`，生成新树自己的 block indexes。
- [ ] 比较新旧 unit 的 structure entry/exit、语义 block path 和 guard 派生字段。
- [ ] structure 不收敛时只重跑受影响 unit details；收敛时 clone/rebase detail AST。
- [ ] 含 recovery 或不完整 detail payload 的 unit 不得复用 details。
- [ ] structure/global finish diagnostics 始终重新计算。
- [ ] `Command.Block`、`Block`、aggregate members、`detailsOpaque`、
  `hasNextStatement` 等派生字段由新 structure/finish 重建。
- [ ] 所有 nested spans、pointer alias、slice/map backing data 和旧树不可变均有覆盖测试。
- [ ] 已解析的 string 语义值直接复制，不得在新树中延迟从旧 source 取值。
- [ ] concurrent `Reparse` 在 race 下通过。

## I6：Server cache 剩余证据

纯 parser cache、配置诊断局部副本、close/reopen、重复 DidOpen、超大文件和主要同步
请求路径已实现。

- [ ] 把 I0 登记的生命周期行落成可执行的 cache/revision/publish 矩阵；已有单项测试
  可以复用，但不能只引用测试名称代替组合状态断言。
- [ ] 增加 didSave whole-content replacement 的 cache/revision 回归。
- [ ] parser 在 server 锁外运行；只在锁下取得旧 cache pointer 和发布结果。
- [ ] canceled、stale、config-old、graph-old 结果可以完成，但不能 cache 或 publish。
- [ ] 所有 open-document cache miss 统一经过 `parseSnapshot`。
- [ ] `workspace.ParseSources` 和无历史 workspace batch 继续 full `Parse`。
- [ ] 增加 analysis 期间 didChange 和多个中间 revision cancellation 的端到端回归。
- [ ] 证明同步请求早于后台 worker 时会从旧 cache 调用 `Reparse`。
- [ ] 证明第二次及以后的小 edit 真正进入增量路径，而不只是结果正确。
- [ ] 补齐 target/config/graph revision 变化时 cache 与 publish barrier 的端到端证据。
- [ ] 运行相关 LSP subprocess integration test。

## I7：最终 fuzz、corpus 和性能闸门

已有 100-step flat Legacy regression、单步 fuzz、基础 official corpus replay 和并发测试；
尚未覆盖计划的完整编辑序列和性能矩阵。

- [ ] fuzz 生成连续编辑序列，每一步与 clean `Parse` 比较；保留所有发现的 corpus。
- [ ] fuzz seeds 覆盖第 13 节的 dialect、owner、structure、recovery、UTF/astral/combining、
  CRLF/BOM 和生命周期高风险状态。
- [ ] official corpus 对 head/middle/tail 执行 insert/delete/equal-length/length-changing
  replace，而不只做三种基础编辑。
- [ ] 增加 fallback 次数/扫描工作量白盒上界，不能用最终 full Parse 掩盖错误增量结果。
- [ ] benchmark 补齐 head/middle/tail、insert、equal-length replace、heredoc marker、
  Vim9 continuation、fragile、identical source 和 forced fallback。
- [ ] 对 Legacy/Vim9 10/100/1024 KiB 各记录五次原始样本和 median/p95；门槛失败时
  提供 profile 和主要 cost center，不能直接放宽门槛。
- [ ] 验证小编辑 time/allocation 低于 full Parse，fallback 回退不超过 15%/20%。
- [ ] 运行并记录：
  - `go test -mod=readonly ./...`
  - `go test -mod=readonly -race ./...`
  - `go vet -mod=readonly ./...`
  - 30 秒 incremental fuzz
  - 完整五次 benchmark

## 最终完成条件

- [ ] I0-I7 的所有待办均有代码或测试证据。
- [ ] scanner state 与 structure state 都真实参与 convergence。
- [ ] prefix reuse、scanner guard、structure convergence、suffix clone/rebase 均有白盒测试。
- [ ] observable JSON、alias topology、analysis、旧树不可变和所有 span 与 `Parse` 等价。
- [ ] Server cache、stale barriers、workspace batch 和 public LSP behavior 无回归。
- [ ] architecture、testing、roadmap 和语言支持文档只陈述已实现事实。
- [ ] `incremental-parsing-plan.md` 第 15、16 节的 checklist 可以逐项勾选。
