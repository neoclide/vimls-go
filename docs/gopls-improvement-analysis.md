# 从 gopls 反观 vimls-go：可实施的改进审计

本文记录 2026-09-03 对本地 `/Users/chemzqm/lib/gopls` 源码快照与
vimls-go 当前实现的对照研究。目标不是把 gopls 的架构移植过来，而是找出
vimls-go 中有当前源码证据、可以独立验证、且符合本项目边界的改进。

## 研究基线和限制

- gopls 目录是 `golang.org/x/tools/gopls` 的源码导出，不是 Git
  worktree；目录和父目录都没有 `.git`，因此无法从当前目录证明精确提交号。
- 快照的 `go.mod` 声明 Go 1.27、`golang.org/x/tools v0.48.0`，并通过
  `replace golang.org/x/tools => ..` 使用同一导出树的上级源码。
- 本文的 gopls 引用均指该本地快照，格式为绝对路径和行号。若以后刷新
  `/Users/chemzqm/lib/gopls`，应重新核对行号和结论，不能把本文当成新版本事实。
- vimls-go 的判断来自当前 `main` 工作树。本文没有把已有的
  `docs/syntax-coverage.md` 修改或未跟踪的 `CHANGELOG.md` 当成本任务输入。
- 本次只研究和写文档，没有运行 gopls 或 vimls-go 的测试。后续实现必须按每项
  建议列出的验收方式重新取证。

## 总结

vimls-go 已经具备 gopls 最值得学习的几个基础思想：不可变文档快照、打开文件
优先于磁盘内容、完整输入上的纯解析、派生状态失效、反向依赖、原子发布以及
陈旧结果拒绝。它并不缺少一个新的 View、Manager、Service 或分析器框架。

当前最值得做的改进是：

1. 修正重复 JSON-RPC request ID 会覆盖旧取消句柄的边界。
2. 让 `shutdown` 响应与后台资源真正停止建立清晰、可测试的关系。
3. 合并同一快照的并发解析，并在测量证明收益后缓存纯文件分析结果。
4. 把 watched-file 事件从“有任何事件就整库重建”改成保守的逐路径更新，任何
   不确定情况仍回退整库重建。
5. 对 push diagnostics 做内容去重，同时保留编辑后的强制重发语义。
6. 先补齐集成测试的超时、收发 transcript 和预构建二进制复用，再考虑更大的
   测试框架。
7. 落实文档已经承诺、但 CI 尚未运行的定时 bounded fuzz 和 benchmark
   regression lane。

其中第 1、2 项是协议和生命周期加固；第 3、4、5 项应先增加可重复测量或回归
用例；其余是测试可诊断性和产品质量改进。不要因为 gopls 规模大就默认引入
多 View、持久化 CAS、`go/types`、SSA、分析器注册表或复杂 promise/LRU 系统。

## gopls 的核心思路及本项目对应关系

| 主题 | gopls 的做法 | vimls-go 当前对应 | 判断 |
| --- | --- | --- | --- |
| 一致性边界 | 一个 Snapshot 提供稳定的文件内容和派生状态；修改产生新 Snapshot | `text.Snapshot` 不可变，文档更新替换指针，并以 snapshot/config revision 判断陈旧 | 已具备，保留轻量模型 |
| 打开文件 | overlay 是磁盘文件之上的第一等内容源 | 打开文档替换 workspace 文件，关闭恢复磁盘时拒绝覆盖已重开的 overlay | 已具备，不需要通用 overlay FS |
| 解析缓存 | 内容身份变化后仍对完整文件调用 `parser.ParseFile`；promise 合并同一计算 | 内容 ID 加完整 source 相等才命中，miss 后完整 `syntax.Parse` | 正确；只缺并发 miss 合并 |
| 语义缓存 | 新建 `types.Info` 并整包 type-check，以 package key/依赖失效限制范围 | `analysis.Analyze` 生成文件局部语义，workspace index 保存不可变跨文件 facts | 边界正确，可测量文件分析复用 |
| 依赖传播 | metadata graph 的反向依赖和 package key 控制重新加载/检查 | ImportGraph 的反向依赖与 workspace identity 控制重分析和发布 | 已具备适合 Vim 的文件级模型 |
| 诊断调度 | modification ID、dirty views、取消旧 pass、内容 hash 控制发布 | 每 URI pending/running、每文档 context cancel、workspace identity 最终栅栏 | 正确性基础已有，可改善 push 去重 |
| 文件变更 | 保留每个 URI/action 的 modification batch，并做有限精确失效 | watched-file 只检查非空，然后安排整库重建 | 有明确改进空间 |
| 测试 | fake editor、可等待事件、帧记录、marker fixture、固定大仓库基准 | 真实 stdio/TCP、Vim oracle、fuzz seeds、固定 workload 已存在 | 先补诊断性，不复制大 runner |

### 已经实现且不应倒退的能力

1. **快照和陈旧结果防线。**
   [`internal/text/snapshot.go`](../internal/text/snapshot.go) 保存不可变文本、版本、
   revision、行索引和内容 ID；
   [`internal/workspace/documents.go`](../internal/workspace/documents.go) 在替换文档时
   取消旧分析，并通过 snapshot 指针和配置 revision 判断结果是否仍然有效。
   gopls 的同类契约见
   `/Users/chemzqm/lib/gopls/internal/cache/snapshot.go:51-66,1076-1107`。

2. **打开内容优先于磁盘。**
   workspace rebuild 安装前验证所有捕获的打开快照仍相同，关闭文档后的磁盘恢复
   也不能覆盖重新打开的内容，见
   [`internal/server/workspace.go`](../internal/server/workspace.go) 的 rebuild 安装与
   restore 检查。gopls 的 overlay 替换模型见
   `/Users/chemzqm/lib/gopls/internal/cache/fs_overlay.go:16-84` 和
   `/Users/chemzqm/lib/gopls/internal/cache/session.go:992-1083`。

3. **解析缓存的正确键。**
   [`parseSnapshot`](../internal/server/server.go#L1158) 同时检查 URI 对应的
   `ContentID` 和完整 source 相等，解析在锁外进行，只有当前 snapshot 才能安装。
   这比只信任 hash 更安全。gopls 同样按 URI、parser mode、body mode 与文件
   identity/hash 判断复用，见
   `/Users/chemzqm/lib/gopls/internal/cache/parse_cache.go:127-245`。

4. **工作区状态是整体一致的。**
   [`internal/workspace/index.go`](../internal/workspace/index.go) 对一个文件的 symbols、
   refs、type/call facts 做原子替换，并分别记录普通索引和 relationship completeness；
   [`internal/workspace/import_graph.go`](../internal/workspace/import_graph.go) 提供不可变
   graph snapshot 和反向依赖；server 捕获 index、graph、resolver 的统一 identity，
   查询结束后重验，失败时只重试一次。不要改回分别读取可变对象的模式。

5. **pull diagnostics 已经比这份 gopls 快照完整。**
   vimls-go 支持 document `previousResultId`、unchanged report、workspace pull、closed
   documents、partial-result batches，并等待完整 workspace index，见
   [`internal/server/diagnostics_pull.go`](../internal/server/diagnostics_pull.go)。本地
   gopls 快照的 document pull 仍在
   `/Users/chemzqm/lib/gopls/internal/server/diagnostics.go:36-76` 留有 result ID、multiple
   views 等 TODO，`workspace/diagnostic` 未实现。因此不能把该部分列成移植目标。

6. **协议表面和资源上限已经成熟。**
   vimls-go 已有 position encoding 协商、request cancellation、生命周期状态机、
   framing 上限、最大 pending requests、分析并发上限、workspace/index/fact 上限和
   capability gating。gopls 研究不支持再加一层通用 dispatcher 或 job framework。

7. **现有测试层次已覆盖关键正确性。**
   [`docs/testing.md`](testing.md) 记录 parser/text fuzz seeds、Vim v9.2.1015 oracle、
   真正的 Vim/vim-lsp smoke、stdio/TCP integration 以及固定 parser、completion、
   runtimepath、反向依赖和 workspace rebuild 基准。这些是后续优化的基线，不应
   用 gopls 的测试数量否定现有体系。

8. **workspace symbol 已有稳定相关性排序。**
   [`Index.SearchInRoots`](../internal/workspace/index.go#L1042) 会先按 root 过滤，再按
   case-insensitive exact、prefix、ordered subsequence 排序，同 rank 使用稳定 fact
   顺序，最后才截断；对应 exact/prefix/subsequence/limit 测试已存在。gopls 更复杂的
   fuzzy/scored workspace symbol 实现不能单独证明本项目还需要改变排序。除非出现
   具体用户案例和可比较的结果集，否则不改现有行为。

## P0：协议和生命周期加固

### 1. 拒绝并发重复的 JSON-RPC request ID [已完成]

- **状态**：已完成
- **实施改动**：在 `internal/server/server.go` 的 `registerCancellation` 中增加重复 ID 校验，重复时返回 `jsonrpc2.InvalidRequest` 拒绝并保留原请求 context cancel 句柄；在 `internal/server/lifecycle_test.go` 中添加 `TestServerRejectsDuplicateRequestID` 回归测试。

**当前证据**

[`registerCancellation`](../internal/server/server.go#L323) 只检查 pending 数量，然后
直接执行 `s.cancellations[id] = cancel`。如果客户端在第一个请求尚未结束时重用相同
ID，第二个请求会覆盖第一个请求的 cancel function；随后 `$/cancelRequest` 只能取消
最后注册的请求，旧请求失去可寻址的取消入口。

**最小改进**

- 在持有现有 mutex 时先检查 ID 是否已存在。
- 若存在，拒绝新请求，不改动旧 map entry。错误码应在实现前按 JSON-RPC/LSP
  约定固定，并在测试中断言；不要为此新增 request registry 类型。
- 保留现有的 128 pending 上限、先注册再异步 dispatch、handler 完成后按相同 ID
  删除和取消的顺序。

**验收证据**

- 一个受控阻塞的原请求与相同 ID 的第二个请求并发到达。
- 第二个请求收到稳定错误；原请求仍能被 `$/cancelRequest` 取消。
- map 最终为空，且不同 ID 的现有并发和取消测试不变。

**建议改动范围**

- `internal/server/server.go`
- `internal/server/lifecycle_test.go` 或 `internal/server/cancellation_matrix_test.go`

### 2. 让 `shutdown` 等待已取消的后台工作退出 [已完成]

- **状态**：已完成
- **实施改动**：在 `Server` 结构中引入 `stopOnce sync.Once` 确保停止逻辑幂等；`Shutdown` 响应前调用 `stopAnalysis()`，等待 analysis、workspace rebuild 和 file watch 后台 goroutine 全部退出后再同步 `publishMu`；在 `internal/server/lifecycle_test.go` 中添加 `TestServerShutdownWaitsForBackgroundWork` 回归测试。

**当前证据**

[`Shutdown`](../internal/server/server.go#L606) 调用 `cancelAnalysis`，只与正在发布的
`publishMu` 同步后立即返回。真正等待 analysis、workspace rebuild 和 watcher
goroutine 的 [`stopAnalysis`](../internal/server/server.go#L1376) 是 `Run` 退出时才
调用。于是客户端收到 shutdown response 时，磁盘读取或 rebuild goroutine 仍可能
处于收尾阶段；虽然陈旧发布已被禁止，资源生命周期仍不够明确。

gopls 在 shutdown 中关闭 file watcher、等待诊断任务、关闭 session 和进度资源，见
`/Users/chemzqm/lib/gopls/internal/server/general.go:782-803`。

**最小改进**

- 抽取或复用一个只执行一次的 stop/join 路径，使 `Shutdown` 在返回前确认 analysis、
  workspace 和 watcher 工作均退出。
- `Run` 的 defer 仍可安全调用同一路径，必须幂等，不能 double close 或在 WaitGroup
  `Add` 竞态中提前 `Wait`。
- 不需要复制 gopls 的 Session、progress tracker 或 file watcher 类型。

**验收证据**

- 用 test hook 阻塞一个 workspace/analysis worker，证明 shutdown response 在 worker
  放行前不会返回。
- 放行后 shutdown 完成；此后没有 diagnostics、refresh 或 registration 发布。
- EOF、context cancel、TCP 和正常 shutdown/exit 的现有退出码不变。

**建议改动范围**

- `internal/server/server.go`
- `internal/server/lifecycle_test.go`
- 必要时 `internal/server/workspace_state_edge_test.go`

## P1：经过测量的缓存、失效和发布改进

### 3. 合并同一快照的并发解析，并复用纯文件分析

**当前证据**

`parseSnapshot` 的安装是安全的，但 miss 路径是“解锁 → `syntax.Parse` → 加锁”。两个
请求同时遇到同一 snapshot 的首次 miss 时都会完整解析，最后只有一个结果被安装。
这不破坏正确性，却浪费 CPU 和 allocation。

此外，当前多个 handler 在拿到同一不可变 `*syntax.File` 后重复调用
`analysis.Analyze`，包括 navigation、completion、rename、semantic actions、inlay
hints、hierarchy、workspace navigation 和 diagnostics。`FileAnalysis` 是源文件局部的
派生结果；workspace/config diagnostics 在 server 侧另行组合，因此具备按 parser
结果复用的天然边界。

gopls 用 `memoize.Promise` 让相同昂贵计算至多执行一次，并允许等待者按自己的
context 放弃等待，见
`/Users/chemzqm/lib/gopls/internal/util/memoize/memoize.go:5-18,134-207`；parse cache
再以有限 CPU 并发等待结果，见
`/Users/chemzqm/lib/gopls/internal/cache/parse_cache.go:196-245,337-359`。

**最小改进顺序**

1. 先增加 concurrent cold-miss benchmark/test，分别计数 parse 和 Analyze 调用。
2. 在现有 server cache 中为 URI + exact content identity 增加一个 in-flight entry，
   同一计算的其他请求等待结果；请求 context 取消只放弃该等待者。
3. 解析完成后一起保存纯 `*syntax.File` 和纯 `*analysis.FileAnalysis`，或增加一个
   小型 `analyzeSnapshot` helper。配置、workspace facts 和最终 diagnostics 不得进入
   这个 key。
4. 内容变化、close 和超限文件继续清理对应 entry；安装仍须验证 snapshot 当前。

不要先复制 gopls 的通用 Promise store、引用计数、LRU、GC 或 FileSet 管理。当前每
URI 一个完成结果加一个 in-flight 结果足够；只有长期 profile 证明多个历史 snapshot
占用才需要 eviction。

**验收证据**

- N 个并发首次请求对一个 snapshot 只触发一次 parse 和一次 Analyze。
- 一个等待者取消不影响仍在等待的请求；所有等待者取消时允许计算完成或安全放弃，
  但不能安装到新 snapshot。
- 相同内容的新版本可以复用纯派生状态，但 diagnostics/result ID 仍绑定新版本与当前
  workspace identity。
- 增加固定 cold/hot/concurrent workload 的 `-benchmem` 数据；只有测量改善后合入。

**建议改动范围**

- `internal/server/server.go`
- 使用语义分析的现有 handler 文件，只做调用点替换
- `internal/server/document_sync_test.go`
- `internal/syntax/benchmark_test.go` 或现有 server benchmark 文件

### 4. watched-file 事件优先做逐路径增量更新

**当前证据**

[`DidChangeWatchedFiles`](../internal/server/workspace.go#L1591) 只要收到一个事件就调用
`scheduleWorkspaceRebuild`，没有读取 URI、Create/Change/Delete 类型或是否属于
`.vim`/workspace root。一个文件保存会重新发现和读取整个 workspace。

但实现已经具备所需的大部分原语：单文件 replace/remove、打开 snapshot 优先、
反向依赖排队、index/graph identity、complete/relationship-complete 降级和全量 rebuild
回退。gopls 的可借鉴点是先把 watched events 保留为 URI/action modification batch，
交给 snapshot/session 判断失效，见
`/Users/chemzqm/lib/gopls/internal/server/text_synchronization.go:172-285` 和
`/Users/chemzqm/lib/gopls/internal/cache/session.go:766-989`。

**最小改进**

- 在现有 `internal/server/workspace.go` 内 canonicalize、去重并过滤事件：仅处理文件
  URI，以及当前 workspace roots 或受支持 runtimepath roots 下的 `.vim` 文件。虽然
  server 只为 workspace roots 注册 watcher，客户端显式发送的 runtimepath 文件事件
  也是当前公开支持的刷新入口，不能静默丢弃。
- Create/Change：若没有打开 overlay，读取受大小上限约束的磁盘文件，解析后原子
  replace；若已经打开，保留 overlay，不让磁盘事件覆盖它。
- Delete：若没有打开 overlay，remove 文件 facts 和 graph edges；若打开，继续保留
  overlay。
- 对 rename 的 create/delete burst 做批次合并。
- 事件缺失、URI 非法、读取失败、超限、根变化、状态不完整或任何不能证明安全的
  情况，统一安排现有全量 rebuild。不能把不完整 index 标为 complete。

不要一次同时做“增量 watched files”和“runtimepath watcher 扩张”；两者风险不同，
应分别测量和验证。

**验收证据**

- 256 文件 workspace 中单文件 Change 只读取/解析该文件及必要 dependents，不重新
  discover 其他文件。
- Create/Delete、atomic save、同批多事件、symlink/realpath、打开 overlay、事件丢失
  回退均有确定性测试。
- 与完整 rebuild 的 Index、ImportGraph、diagnostics、symbols、refs 做差分比较。
- 保持 workspace pull 在 rebuild 中等待新完整 index；绝不能返回旧 index 假装最新。

**建议改动范围**

- `internal/server/workspace.go`
- `internal/server/workspace_test.go`
- `internal/server/workspace_build_edge_test.go`
- `internal/workspace/index_test.go`、`import_graph_test.go` 仅在需要补原语测试时修改

### 5. push diagnostics 按内容去重，同时在编辑后强制重发

**当前证据**

push 模式的 `published map[string]bool` 只记“是否曾发布非空结果”。每次成功后台分析
都会调用 `PublishDiagnostics`，即使排序后的 diagnostics 与上次完全相同。这个 bool
只能避免“从未有诊断时发送空集”，不能减少重复的非空结果。

gopls 对每个 URI 保存诊断 hash 和 `mustPublish` 状态；相同结果通常跳过，但文件
发生修改时标记必须重发，以满足客户端对新版本诊断的需要，见
`/Users/chemzqm/lib/gopls/internal/server/diagnostics.go:79-101,785-905` 和
`/Users/chemzqm/lib/gopls/internal/server/text_synchronization.go:265-274`。

**最小改进**

- 把 bool 扩成 `{hash, mustPublish, lastVersion}`，仍由 `publishMu` 保护。
- 对已经稳定排序的 protocol diagnostics 计算确定性 hash；至少覆盖 range、code、
  source、message、severity、tags 和 related information。
- DidOpen/DidChange/DidSave、配置变化和明确 refresh 时标 `mustPublish`；纯重复后台
  pass 且 hash/版本均未变时跳过。
- 空结果也是发布状态：曾发布非空后必须发布空集清除；close 继续清除。
- pull diagnostics 的 result ID/key 逻辑保持独立，不要为了共用状态改坏现有契约。

**验收证据**

- 同一 snapshot 重复分析只发送一次。
- 新版本即使 diagnostics 内容相同也按约定重发，并携带新 version。
- 非空变空时发送一次空集；后续纯重复不再发送。
- config severity/disable、workspace graph 变化和 related information 变化会改变 hash。

**建议改动范围**

- `internal/server/server.go`
- `internal/server/diagnostics_test.go`
- `internal/server/document_sync_test.go`

## P1：先补测试可诊断性，再扩充测试框架

### 6. 给子进程集成测试增加 per-operation timeout 和失败 transcript

**当前证据**

主 `TestLSPSubprocess` 包含大量内联 JSON-RPC 交互；`readResponse` 和
`readPublishedDiagnostic` 循环依赖整个进程的 30 秒 context，单个等待没有自己的
deadline。读取失败时也没有自动附上最后若干条收发帧和 server stderr，定位 hang
或乱序消息成本高。

gopls runner 会给测试 context 留出 cleanup 时间，失败时打印记录的帧，并可选择打印
goroutine dump，见
`/Users/chemzqm/lib/gopls/internal/test/integration/runner.go:130-183,234-255`。该文件
自己也承认 runner 已经过度复杂，因此只学习诊断能力，不复制框架。

**最小改进**

- 在现有 integration helper 中维护固定容量的最近收发 transcript，失败时加 stderr。
- 每个“等待 response/notification”接收 context 或 deadline，超时错误包含正在等待的
  method/ID 和 transcript。
- 把大场景拆成少量按 lifecycle、diagnostics、navigation/edit 分组的 subtests；仅在
  重复出现时抽一个 framed-RPC helper。
- stdio 主场景复用一次预构建的 `vimls` 二进制，而不是每次 `go run`。这既减少编译
  噪音，也真正验证发布入口。
- 让 stdio 和 TCP 至少共享一个最小 open/change/diagnostic/shutdown 场景；不要创建
  多 transport Runner 类型。

**验收证据**

- 人工让某个 response 不到达，失败信息在该 operation deadline 内出现，并包含方法、
  ID、最近帧和 stderr。
- 所有 subprocess case 使用同一预构建二进制路径。
- stdio/TCP 共享场景结果相同，测试没有 timing sleep。

**建议改动范围**

- `test/integration/lsp_subprocess_test.go`
- 如确有重复，可在 `test/integration` 内增加一个小 helper 文件

### 7. 落实定时 bounded fuzz 和 benchmark regression lane

**当前证据**

[`docs/testing.md`](testing.md) 已明确：PR replay committed seeds，未来 scheduled lane
运行 bounded live fuzz；固定 workload 也已有 15% time、20% allocation 的确认阈值。
当前 [`.github/workflows/ci.yml`](../.github/workflows/ci.yml) 没有 live fuzz 或 benchmark
regression job。这是已经写入项目测试契约但尚未落地的差距。

gopls 的 didChange benchmark 会先等待 server 已开始处理 change，并为每轮生成唯一
内容避免 cache hit，见
`/Users/chemzqm/lib/gopls/internal/test/integration/bench/didchange_test.go:18-21,43-96`。

**最小改进顺序**

1. scheduled workflow 分别运行现有 fuzz target 的短时 `-fuzztime`，每个 target 有
   独立超时和 artifact；不要和 coverage 合并。
2. 在固定 runner 保存五次原始 `benchmem` 输出和环境信息，先观察稳定性。
3. 只有确认 runner variance 可控后，才按现有文档阈值自动 gate；不稳定时报告数据而
   不是制造 flaky failure。
4. benchmark edit 必须产生唯一内容，并等待目标 server 阶段，避免把 cache hit 或
   client enqueue 时间误认为完整工作量。

**验收证据**

- workflow_dispatch 和 schedule 均可运行；失败 artifact 包含 seed/target 或完整
  benchmark samples。
- 发现的 fuzz crash 写入所属 package 的 `testdata/fuzz` 后，普通 `go test` 可重放。
- benchmark 报告固定 Go 版本、OS/arch、GOMAXPROCS、命令、五次 raw samples、median
  和 p95。

**建议改动范围**

- `.github/workflows/ci.yml`，或一个单独且职责单一的 scheduled workflow
- `docs/testing.md` 只在实际命令或证据格式改变时更新
- 现有 benchmark/fuzz test 文件，仅补唯一 edit 和同步点

## P2：有条件的后续改进

### 8. 只在有用户证据时扩张 runtimepath watcher registration

当前动态 watcher 只注册 workspace roots，见
[`refreshFileWatchRegistration`](../internal/server/workspace.go#L1622)；runtimepath roots
被索引并参与 import/navigation/completion，但 server 不主动为它们注册 watcher。
客户端仍可显式发送 runtimepath 路径的 `workspace/didChangeWatchedFiles`；当前实现会
因此安排 rebuild，配置文档也把它列为受支持刷新入口。是否自动收到这类事件取决于
客户端已有的观察范围，不能概括成“runtimepath 内容变化不会触发 watcher”。

当前 architecture 明确写着 runtimepath roots 不被 server 注册 watch，因此这是产品
选择，不应直接当 bug 修复。

后续应二选一并明确记录：

- 保持当前简单模型：runtimepath 内容在初始化、其他原因的 workspace rebuild、显式
  `vimls/didChangeRuntimepath` 或客户端显式 watched-file event 后刷新；或
- 在实际用户案例证明需要后，观察 canonical workspace/runtimepath roots 的有界并集，
  去掉被父 root 覆盖的重复项，并对 watcher 数量设上限。

gopls 会合并 active views 的 watch patterns，但其源码也记录超过 8,000 patterns 和
coc.nvim 大列表的性能问题，见
`/Users/chemzqm/lib/gopls/internal/cache/snapshot.go:812-891`。因此不要照搬“观察所有
known subdirectories”的策略。

### 9. 新增跨文件 fact 前，先建立集中化不变量检查

workspace index 当前能原子替换多个 fact 类别，但 collectors 分别遍历语法/分析结果。
现有 parser span 验证很完整，跨文件 facts 的 source ownership、span bounds、排序和
replace/remove 可逆性更多依赖各自 case tests。

gopls xref 在序列化前只接受语义已解析且 range 有效的引用，并对结果排序，见
`/Users/chemzqm/lib/gopls/internal/cache/xrefs/xrefs.go:26-119,183-203`。

最小做法是在 `internal/workspace` 增加 test-only validator：

- 每个 fact path 属于其 source；span 在 source 内且边界有序。
- facts 排序确定；同一文件 Replace 两次幂等。
- Remove 后所有正向/反向入口消失，再 Replace 恢复相同结果。
- incomplete/relationship overflow 不能被 validator 或 query 隐藏。

只有 workspace rebuild profile 证明多次 traversal 显著，才把多个 collectors 合成一个
`index.go` 内部的 per-file summary builder。不要新增 Fact Registry 或 Analyzer 接口。

### 10. 可选的用户功能：评论 URL links 与参数名 hints

这两项不是 1.0 正确性缺口，但都能复用已有静态信息：

- DocumentLink 当前只处理 import 和静态 `:source`，而 scanner 已保留精确 comment
  token spans。可以只扫描 `TokenComment` 中的 `http`/`https` URL，绝不执行脚本、
  解析动态路径或扫描字符串。对照 gopls link handler：
  `/Users/chemzqm/lib/gopls/internal/server/link.go:33-54`。
- InlayHint 当前只输出推断类型。已有本地、imported 和 builtin signature/parameter
  信息后，可以增加默认关闭的 parameter-name hints；仅对唯一静态 callable 生效，
  参数名与 argument identifier 相同、variadic/ambiguous/dynamic 时跳过。gopls 的分类
  和抑制规则见
  `/Users/chemzqm/lib/gopls/internal/golang/inlay_hint.go:40-140`。

两项应各自成为独立 milestone，不能借机扩展 whole-source formatter、refactor actions、
动态 rename 或 embedded-language analysis。

### 11. 低成本请求耗时日志，而不是生产 telemetry 平台

gopls 用 structured events 包围 handler，并拥有完整 telemetry/debug 系统。vimls-go
当前主要记录 stderr 错误和少量客户端 warning。若实际故障定位需要，可在现有 handler
边界增加可选的 method、duration、result class 日志；不得记录文档内容、环境值或完整
参数。

优先完成第 6 项的测试 transcript。没有真实线上诊断需求前，不引入 telemetry SDK、
debug HTTP server、全局 metrics registry 或外部上传。

## 明确不迁移的 gopls 设计

### 多 View 和 Go package metadata

gopls 的 View/Snapshot/package graph 解决 build tags、GOOS/GOARCH、module/workspace、
test variants 和 `go/packages` load。vimls-go 的 workspace roots、runtimepath 与 Vim
import graph 没有这些构建变体。现有文件级 ImportGraph 是正确粒度。

### `go/types`、SSA、analysis facts 和 analyzer registry

Vim script 的动态名字、命令和运行时状态不能获得 Go object identity。当前
`unknown` policy、静态引用和有界 facts 比复制 bottom-up package facts 更可靠。任何
新诊断仍应从 pinned Vim 行为和可证明语义出发。

### 把 gopls 误认为增量 AST 实现

该 gopls 快照在内容 hash 变化时仍对完整文件执行 `parser.ParseFile`；它的“增量”主要
来自 overlay、snapshot、cache 和 dependency invalidation，不是 AST node splice。
vimls-go 应继续保持 `syntax.Parse` 为 full oracle。若以后实现 `Reparse`，仍须遵守现有
增量解析计划的 byte/unit/state/structure/alias-topology 差分验证和不安全时 full fallback。

### 无测量的持久化缓存、LRU、arena 和对象池

gopls 的文件级 CAS、persistent maps、xref/methodset caches 服务大型 Go monorepo，伴随
schema、版本、磁盘配额、隐私和 GC 成本。vimls-go 已有 256 MiB index 上限和固定
workspace benchmarks。没有真实启动/内存数据前，继续推迟 persistent index、rope、
arena、pool 和 `unsafe`。

### 整套 gopls integration runner

gopls runner 支持 default、forwarded、separate-process、多环境和大型仓库，自身源码也
承认复杂度过高。vimls-go 只需要在现有 subprocess test 中加入 deadline、transcript、
fixture/helper 和共享 transport 场景。

## 建议实施顺序

| 阶段 | 内容 | 前置证据 | 完成证据 |
| --- | --- | --- | --- |
| A | 重复 request ID；shutdown join | 受控并发/阻塞测试先失败 | lifecycle focused tests、`go test ./...`、`go vet ./...` |
| B | integration deadline/transcript/prebuilt binary | 人工失败输出基线 | 故障能定位到 method/ID，stdio/TCP 共享场景通过 |
| C | parse/Analyze 合并 | cold/hot/concurrent benchmark | 一次计算、取消安全、benchmem 有改善 |
| D | watched-file 单路径 delta | 256 文件 rebuild profile | 增量与 full rebuild 差分相等，未知情况可靠回退 |
| E | push diagnostics hash | 重复 publish 计数测试 | 必要重发不丢、无效重复消失 |
| F | scheduled fuzz/benchmark | workflow dry run | artifact、环境、raw samples 和 regression 证据完整 |
| G | runtimepath watcher 或可选功能 | 真实用户案例和 watcher/延迟基线 | 独立 feature tests，不扩大动态语义承诺 |

不要把 C、D、E 合成一次“性能重构”：它们的 key、取消、正确性 oracle 和回退条件
不同。每一阶段都应更新实际 support/architecture/testing 契约，而不是提前写成已实现。

## 实施任务的边界模板

若把上述项目交给 write agent，每个任务至少固定：

- **Goal**：一个可观察结果，例如“相同 ID 的第二个并发请求被拒绝，原请求仍可取消”。
- **Allowed paths**：只列该项上文建议的精确文件。
- **Forbidden paths**：`go.mod`、共享 testdata、generated files、无关 docs 以及其他 agent
  所有的 package。
- **Required behavior**：snapshot、overlay、workspace identity、unknown policy、full
  fallback 和协议错误语义不得变化。
- **Validation**：先写能证明旧边界的 focused test，再运行 gofmt、`go test ./...`、
  `go vet ./...`；不默认运行 race 或 coverage。

共享 `internal/server`、`internal/workspace`、integration fixture 或 workflow 的任务不能
并发写。主 agent 负责跨 package 合同、最终集成和文档同步。

## 主要源码索引

### gopls 本地快照

- Snapshot 生命周期与 clone：
  `/Users/chemzqm/lib/gopls/internal/cache/snapshot.go:51-66,1507-1545,1655-1879`
- overlay 和 session modification：
  `/Users/chemzqm/lib/gopls/internal/cache/fs_overlay.go:16-84`
  `/Users/chemzqm/lib/gopls/internal/cache/session.go:766-1083`
- parse cache 和 promise：
  `/Users/chemzqm/lib/gopls/internal/cache/parse_cache.go:127-245,319-385`
  `/Users/chemzqm/lib/gopls/internal/util/memoize/memoize.go:5-18,134-207`
- package/type-check invalidation：
  `/Users/chemzqm/lib/gopls/internal/cache/check.go:850-890,1196-1375,1547-1676`
- diagnostics scheduling/publication：
  `/Users/chemzqm/lib/gopls/internal/server/diagnostics.go:79-101,201-328,785-905`
  `/Users/chemzqm/lib/gopls/internal/server/text_synchronization.go:215-341`
- workspace symbols/xrefs：
  `/Users/chemzqm/lib/gopls/internal/golang/workspace_symbol.go:170-441`
  `/Users/chemzqm/lib/gopls/internal/cache/xrefs/xrefs.go:26-119,183-203`
- lifecycle and integration diagnostics：
  `/Users/chemzqm/lib/gopls/internal/server/general.go:775-830`
  `/Users/chemzqm/lib/gopls/internal/test/integration/runner.go:130-260`

### vimls-go

- [Architecture](architecture.md)
- [Test and release strategy](testing.md)
- [Server lifecycle and parse cache](../internal/server/server.go)
- [Workspace rebuild and watched files](../internal/server/workspace.go)
- [Pull diagnostics](../internal/server/diagnostics_pull.go)
- [Document snapshots](../internal/workspace/documents.go)
- [Workspace index](../internal/workspace/index.go)
- [Import graph](../internal/workspace/import_graph.go)
- [Subprocess integration](../test/integration/lsp_subprocess_test.go)
