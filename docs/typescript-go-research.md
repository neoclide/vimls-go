# typescript-go 对 vimls-go 的借鉴价值研究

> 研究快照：
> - typescript-go：`/Users/chemzqm/lib/typescript-go`，`main` @ `89d5d5b2849a0db0957065889ca58536fa6d2e4a`（2026-08-20，repo 关闭公告提交）。
> - vimls-go：`/Users/chemzqm/vim-dev/vimls-go`，`main` @ `f5d4ef2b4aba795df5c1a6ce7bbd88b2a5e8c373`（2026-08-31）。
> - 两个仓库均要求 Go 1.26。
> - typescript-go 是 TypeScript 7 原生移植的 staging repo，README 已声明“closed”，后续开发合并回 `microsoft/TypeScript`。本报告把 2026-08 快照当作工程范本，不把其发布状态当作依赖。

## 0. 结论摘要

typescript-go 对 vimls-go 最大的借鉴价值不在“TypeScript 解析器怎么写”，而在以下四类工程能力：

1. **LSP 会话与状态管理**：`readLoop / dispatchLoop / writeLoop` 三循环、按请求上下文取消、同步/异步 handler 分离、不可变 `Session-Snapshot`、按需 flush、generation token 去抖。
2. **解析与索引的性能边界**：内容哈希 `ParseCache`、ref-count 快照、dirty-map 克隆、过量文件事件失效、parser pool、lazy line-map。
3. **诊断的可治理性**：从上游源生成集中式诊断目录、语法/语义/建议分层、pull+push 双轨、诊断稳定性检测。
4. **测试与发布证据**：fourslash 内联 LSP 测试、baseline local/reference/accept 工作流、官方上游测试迁移与 accepted/triaged 治理、CI 矩阵变体。

不应直接照搬的东西同样明确：typescript-go 的完整 snapshot/project 体系（1.15M LOC 规模）、content-mapper 外部进程、ATA 自动类型获取、完整自研 fswatch 后端、872KB 的完整 LSP 生成类型，以及它没有 header/message 大小上限的 framing。vimls-go 的 `AGENTS.md` 第一原则（最小直接实现、不执行用户脚本、客户端拥有文件监听）仍然优先于任何“大项目做法”。

下面每节都给出：typescript-go 怎么做、vimls-go 现状、可借鉴点、成本/风险、建议阶段。

---

## 1. 研究范围与方法

### 1.1 仓库概况

typescript-go：

- 定位：TypeScript 7 编译器 + CLI + LSP 的 Go 原生移植。README 标注 Program/parse/type check/emit/watch/build 已完成，Language service（LSP）in progress。
- 规模（只统计 `internal/` + `cmd/` 的 Go 文件）：
  - Go 文件 5,075 个，其中 `*_test.go` 4,537 个。
  - 总行数约 1,152,085 行。
  - `internal/fourslash/tests/` 下有 4,359 个内联 LSP 场景测试文件，约 786k 行。
- 关键依赖很少：`go-json-experiment/json`、`golang.org/x/sync`、`golang.org/x/sys`、`zeebo/xxh3`、`google/go-cmp` 等。
- 关键包规模（LOC）：

| 包 | 行数 | 说明 |
| --- | ---: | --- |
| `internal/checker` | 60,479 | 完整类型检查器 |
| `internal/ls` | 31,166 | 语言服务特性 |
| `internal/ast` | 22,139 | 生成的 AST/visitor/position map |
| `internal/project` | 20,311 | Session/Snapshot/项目集合 |
| `internal/fswatch` | 10,063 | 多平台文件监听 |
| `internal/diagnostics` | 9,776 | 诊断目录与本地化 |
| `internal/parser` | 9,432 | 递归下降 parser |
| `internal/lsp` | 5,429 | LSP server |
| `internal/scanner` | 4,425 | 扫描器 |
| `internal/binder` | 3,590 | 符号绑定 |
| `internal/core` | 2,943 | 基础数据结构和运行时选项 |

vimls-go：

- 定位：Legacy Vim script + Vim9 script 的 Go 语言服务器，语法源钉在 Vim `v9.2.1015`，兼容地板 Vim 9.1。
- 规模：Go 文件 141 个，其中 `*_test.go` 83 个，总行数约 86,944 行。
- 包边界已经清晰：`jsonrpc / text / syntax / analysis / workspace / server / vimdata`，依赖方向从 server 指向小包。
- 测试资产：Vim `src/testdir` 全部 362 个 `.vim` 文件（8,558,061 字节）、3,267 个官方抽取脚本、5,733 个 `Check*` 候选的 inventory、5,261 个 parser 变体，全部以 committed artifacts 离线运行。

### 1.2 可比性

两个项目在工程结构上高度同构：

| vimls-go | typescript-go | 同构点 / 差异 |
| --- | --- | --- |
| `cmd/vimls` | `cmd/tsgo --lsp` | 单可执行文件，stdio 优先 |
| `internal/jsonrpc`（bounded framing）+ `go.lsp.dev/jsonrpc2` | `internal/jsonrpc` + `internal/lsp/lsproto`（自生成协议） | vimls framing 更安全；tsgo 协议类型更严 |
| `internal/text` | `internal/core.TextChange`、`internal/ast.PositionMap`、`internal/ls/lsconv` | 两者都坚持内部 byte offset、边界转换 |
| `internal/syntax` | `internal/scanner` + `internal/parser` + `internal/ast` | 都是不执行源码的静态解析器 |
| `internal/analysis` | `internal/binder` + `internal/checker` | vimls 保守推断；tsgo 完整求解 |
| `internal/workspace` | `internal/project` + `internal/ls` | 索引/图 vs snapshot/项目集合 |
| `internal/vimdata` 生成表 | `internal/bundled` + `internal/diagnostics` | 都是从上游元数据生成的版本化数据 |
| `testdata/official` + generated cases | `_submodules/TypeScript` + fourslash + baseline | vimls committed artifacts 离线；tsgo 运行时读 submodule |
| `Makefile` gate | `Herebyfile.mjs` tasks | vimls 更轻；tsgo 发布矩阵更完整 |

研究方法：逐包读代码、抽取关键控制流和测试工作流，再与 vimls-go 现有 `AGENTS.md`、`docs/architecture.md`、`docs/testing.md`、`docs/roadmap.md` 对照，最后按“价值/成本/与现有架构的冲突程度”排序。未运行大规模测试；本报告不改变任何行为。

---

## 2. typescript-go 的核心架构

### 2.1 进程与消息循环

`internal/lsp/server.go` 的 `Run` 把工作拆成三层：

- `readLoop`：阻塞读 stdin，不放入 errgroup；处理 initialize 前置状态、response 回填、cancel、其余入 `requestQueue`。
- `dispatchLoop`：从 `requestQueue` 取消息；给每个请求建立带 request ID 的 context，登记 `pendingClientRequests`；handler 返回“同步部分 + 可选异步函数”，异步部分在 goroutine 中执行并负责回包。
- `writeLoop`：单一消费者序列化所有 `outgoingQueue` 写操作，统一处理 marshal error。

`internal/lsp/dynamic_queue.go` 是一个 context-aware 的可取消队列状态机。它解决的是“读线程阻塞不可取消、写必须串行、调度要可取消”三者之间的同步问题。

vimls-go 现状：

- `internal/server/server.go` 使用 `go.lsp.dev/jsonrpc2.NewConn` + `conn.Go`，外层有 `lifecycleHandler` 和 `cancellationHandler` 中间件。
- 已有 pending request 上限 128、`$/cancelRequest`、analysis 取消和 stale publish 防护。
- 没有显式 sync/async handler 分离；handler 在库的 goroutine 模型里执行。

可借鉴点（低优先级，按需）：

- 如果将来要主动向客户端发请求（例如 `workspace/configuration` 之外的 pull 诊断注册、动态刷新），可以引入 tsgo 的 `pendingServerRequests` + `sendClientRequest` 泛型 helper，而不是继续手写 ID 匹配。
- 如果未来 handler 中要区分“必须串行的状态更新”和“可并行的查询”，采用 `handler() (func() error, error)` 的 sync/async 契约比新开 goroutine 更可测试。
- 不必替换 `go.lsp.dev/jsonrpc2`；目前库已满足生命周期、取消和错误码需求。tsgo 自建循环的收益只有在需要精确控制 write error 和 marshaling 时才值回复杂度。

### 2.2 协议类型：从 LSP meta-model 生成严格 codec

typescript-go 的 `internal/lsp/lsproto/lsp_generated.go`（约 872KB）由 `_generate/generate.mts` 从 `vscode-languageclient` 锁定版本的 meta-model 生成：

- 每个方法有 `RequestInfo[Params, Resp]` / `NotificationInfo[Params]`，handler 注册是类型安全的。
- `Message.UnmarshalJSON` 把 `params` 保留为 `json.Value`，到真正 dispatch 时才 `UnmarshalParams[T]`，未实现的方法不会强制解码。
- `structcodec.go` 用反射缓存 struct spec：未知字段跳过（前向兼容），required 字段缺失报错，非 nullable 字段收到 `null` 报错。
- 自定义结构、VS 扩展方法直接补进生成器，而不是手改生成文件。

vimls-go 现状：

- `go.lsp.dev/protocol` 提供 wire 类型；`internal/server/server.go` 为了读 `rootPath/rootUri` 不得不把 `InitializeParams` 重新 `protocol.Marshal` 再 `encoding/json.Unmarshal` 到私有结构（`legacyInitializeFields`）。这已经暴露出“依赖类型没有覆盖所需 wire 字段”的摩擦。
- `go.lsp.dev/protocol` 的 union/optional 表达和 null 严格性对 vimls 已经够用；当前没有批处理需求，也没有 VS 扩展方法。

可借鉴点（P2，1.0 后再评估）：

- 不要把 872KB 的完整 codec 搬进来。可以做一个裁剪版：只从 pinned meta-model 生成 vimls 实现的约 25 个方法和自定义 `vimls/didChangeRuntimepath`，未知字段跳过、required/null 严格性照抄。
- 这能消除 `legacyInitializeFields` 这类 round-trip 代码，也能让 `implementedMethod` 白名单由生成常量替代。
- 触发条件：开始实现 pull diagnostics、file operations、workspace symbol resolve、更多自定义方法，或 `go.lsp.dev/protocol` 无法表达某个合法 wire 形态时。
- 风险：自生成 codec 是新维护面；在 M7 之前，先在 research branch 生成“只读对照测试”，验证它和 `go.lsp.dev/protocol` 对同一输入的解码一致，再决定切换。

### 2.3 Session-Snapshot：不可变快照与 ref-count

typescript-go 的 `internal/project/session.go` + `snapshot.go`：

- `Session` 持有当前 `*Snapshot`；文档 open/change 只写 `pendingFileChanges`。
- `getSnapshot` 在请求到来时才 flush pending changes；`DidOpenFile` 立即更新，`DidCloseFile` 走 500ms debounce。
- `updateSnapshot` 在 `snapshotMu` 下用 `old.Clone(ctx, change, overlays, session)` 生成新 snapshot，替换后旧 snapshot `Deref`。
- `Snapshot` 带 `refCount`、`parentId`、`SnapshotFS`、`ProjectCollection`；clone 通过 `dirty.Map / dirty.SyncMap` 只复制变化的文件/目录缓存。
- 每个请求拿到的 snapshot 有 caller ref，旧请求结束时释放；dispose 时通过 `programCounter` 释放 parse cache 引用和 checker pool。

vimls-go 现状：

- `internal/workspace/documents.go` 已经是每文档不可变 `text.Snapshot`，`IsCurrent` 检查 URI/revision/config revision，做得正确。
- `workspace.Index` 是线程安全但可变的单对象；`workspace.ImportGraph` 已经采用“可变 build + 不可变 Snapshot + revision”的模式。
- 跨文件的 workspace 重建目前持锁替换指针，读请求只在锁内取指针，然后无锁搜索 index；这在单 index 下是安全的。

可借鉴点（P1，M5 阶段）：

- 把 `workspace.Index` 也改成“可变 build + 不可变 `IndexSnapshot` + revision”，与 `ImportGraphSnapshot` 对齐。workspace symbol、cross-file reference、rename 都从一次 revision 的一致性视图读，避免未来跨文件语义合并时“文件 A 用新 index、文件 B 用旧 index”。
- 引入 snapshot ref-count 不需要一开始就做；先给 `IndexSnapshot` 加 revision 和只读查询，引用计数只在并发长请求真正持有 index 时再加。
- 借鉴 `dirty.Map` 的 clone-on-write：新 index 构建时共享未变化文件的 `indexedFile`（`SymbolFact` 已是值类型、`Source` 是 immutable string），只有变化路径才重建 facts/references。这比每次 watch event 全量 `buildWorkspaceIndex` 的收益更大。
- 不要照搬 `SnapshotFS / ProjectCollection / programCounter` 整层；vimls 没有 project/config/program 概念，照搬违反 AGENTS“没有真实多个实现就不要加层”。

### 2.4 按需 flush、去抖与 generation token

typescript-go 的去抖不是简单 `time.AfterFunc`：

- `scheduleDiagnosticsRefresh(delay)` 每次取消旧 context，`generation++`，在后台队列里等待 `delay`；执行前检查 generation 是否仍然最新。
- `ScheduleSnapshotUpdate(reason)` 同样处理 snapshot 更新。
- `DidChangeFile` 对普通源码取消 diagnostics refresh（pull diagnostics 客户端会自行按 keystroke 拉取），对 content-mapped 文件 `scheduleDiagnosticsRefresh(0)`。
- `DidChangeWatchedFiles` 只对相关扩展/目录安排 refresh；config 变化另走 snapshot update。

vimls-go 现状：

- `didChange` 立即更新 `text.Snapshot` 并调用 `startAnalysis`，没有分析级去抖。`analysisPending/analysisRunning` 保证同一 URI 不会同时跑两份，但快速连续输入会触发“完成一次再跑一次”。
- `DidChangeWatchedFiles` 只要 `len(changes)>0` 就 `scheduleWorkspaceRebuild()`，没有事件相关性过滤，也没有去抖。
- `scheduleWorkspaceRebuild` 用 `workspaceRevision++` 防止旧结果覆盖新结果，方向正确，但缺时间维度的合并。

可借鉴点（P0，M4/M5 就要做）：

- 文档文本状态必须继续立即应用（协议语义），但**后台分析发布**可以按 URI 去抖。建议 `startAnalysis` 改为：每个 URI 维护 `analysisTimer` + generation；`didChange/didSave` 取消旧 timer 并排 100–250ms 的 debounce；`didOpen` 立即分析；同步请求路径仍用 `structureDocument` 实时 parse，保证 completion/hover 延迟不变。
- `DidChangeWatchedFiles` 先按扩展名和目录过滤：只接受 `*.vim`、无扩展 runtime 目录、目录事件；连续事件在 300–500ms 窗口合并为一次 rebuild；超过阈值（建议沿用 1000）直接放弃增量、安排一次全量 rebuild。
- 使用 generation token，而不是比较时间戳，避免“旧 timer 取消失败”之类竞态。
- 必须保留现有 `IsCurrent`/graph revision 双重检查；去抖只能减少计算，不能成为 stale publish 的新入口。

### 2.5 ParseCache：按内容哈希共享解析结果

typescript-go `internal/project/parsecache.go`：

- key = `SourceFileParseOptions + ScriptKind + xxh3.Hash128(content)`。
- `ParseCache` 是 ref-count cache；snapshot clone 时共享未变化文件的 AST，dispose 时按引用计数释放。
- `overlayfs.go` 用 `xxh3.HashString128` 比较 overlay 与磁盘内容，磁盘文件和 overlay 都保存 hash 与懒加载 line-map。

vimls-go 现状：

- `workspace/buildWorkspaceIndex` 每次 rebuild 都 `os.ReadFile` 全部文件并 `workspace.ParseSources`；watch 一个文件变化也全量重读重解析。
- 文档请求路径会在 `parsed` 缓存失效时 `syntax.Parse`；`prepareSyntax` 把 `*file` 浅拷贝后保存，避免诊断 slice 被后续共享修改。
- `internal/text` 每份 snapshot 重新 `indexLines`，没有跨相同文本共享。

可借鉴点（P1，M5）：

- 为 workspace 文件建立 `path -> {content hash, size, syntax.File}` 的内存缓存。重建时：
  1. 发现文件，读内容前先 `os.Stat` 拿 size/mtime 作为低成本候选；
  2. 对候选变化文件读内容并 `xxh3.HashString128`，未变则复用上一轮 AST/facts/source；
  3. 只对变化文件和新文件调用 `syntax.Parse` 和 `CollectSymbolFacts`。
- 哈希库选择：与 tsgo 一致的 `github.com/zeebo/xxh3` 是合理依赖；若不想加依赖，进程内 `hash/maphash` 也行，但不能跨进程复用。
- 风险：`syntax.File` 不是显式 immutable 类型。缓存条目必须视为只读，消费方沿用 `prepareSyntax` 的浅拷贝策略，每个分析任务拥有自己的 `Diagnostics` slice；如果未来 analysis 要写 AST 字段，先给 AST 加 clone/freeze 约定。
- 文本行索引也可以懒化：`text.Snapshot` 的 `indexLines` 改成 `sync.Once`，但对单文档小文本收益有限，先 benchmark 再动。

### 2.6 诊断目录：生成、分层、本地化

typescript-go `internal/diagnostics/`：

- `diagnostics_generated.go` 由 `generate.go` 从 TypeScript submodule 的 `diagnosticMessages.json` 生成，每个诊断是带 `code/category/key/text/reportsUnnecessary/reportsDeprecated` 的 `*Message`。
- `Localize` 按 locale 查表，支持 `{0}` 占位符，locale 数据以 gzip JSON 嵌入。
- parser/checker 等各层只产生 `ast.Diagnostic`（file + byte range + message + args + related），最后统一转 LSP。

vimls-go 现状：

- `internal/syntax/vimls_diagnostics.go` 只有 31 个 vimls-owned 定义，线性查找；`vim/E...` 代码没有集中目录。
- `internal/server/server.go` 的 `protocolDiagnosticSeverity` 硬编码 `vim/E117/E121/E1001/E1089` 为 unresolved 等级。
- 官方 compile cases 按 E 码分文件，provenance 好，但“支持哪些码、默认消息、severity、category”分散在测试和 `docs/diagnostics.md` 里。

可借鉴点（P0，M4）：

- 新建 `internal/vimdata/errors_generated.go`，生成器从 pinned Vim tag 的 `src/errors.h` 抽取完整索引：编号、宏名、默认消息（`errors.h` 的 `INIT(= N_("E...: ..."))` 就是稳定默认文本），但只把“已经有 official compile case 或 `docs/diagnostics.md` 证据支持”的 E 码标记为 `Supported`。未迁移的 E 码只进索引、不产生诊断消息，避免冒充支持。
- 表结构建议：`Code string, Name string, Message string, Supported bool, Severity vimdata.DiagnosticSeverity, Kind enum(parser/semantic/compat), Source string(evidence location)`。severity 用 `internal/vimdata` 自己的 enum 或 `uint8`，避免 `vimdata -> syntax` 反向依赖；`internal/server` 负责转成 LSP severity。
- `protocolDiagnosticSeverity` 改为查表，硬编码只作为 fallback。
- vimls-owned 定义也迁到同一个表（或 JSON manifest + generator），保留 `vimls/*` 前缀；生成器必须像 `tools/gencommands` 一样校验 pinned tag/commit。
- 本地化现在不做；Vim 本身的错误文本在不同 locale 下也不稳定，1.0 保持英文。

### 2.7 Pull + push 诊断双轨

typescript-go `handleInitialize` 广告 `DiagnosticProvider`，`handleDocumentDiagnostic` 走 `LanguageService.ProvideDiagnostics`；push diagnostics 由 `disablePushDiagnostics` 控制，并做 snapshot diff 后发布 project 诊断。

它还有一个“flake logging”机制：`trackFlakyDiagnostics` 选项下，先提供一次诊断，再触发 emit，二次提供诊断，diff 前后集合，记录或 panic。这用来抓检查器状态污染和非确定性缓存。

vimls-go 现状：

- 只 push `textDocument/publishDiagnostics`，没有 `textDocument/diagnostic`。
- 分析本身是确定性的固定遍历，但 map 迭代路径必须显式排序；目前诊断已 `sort.SliceStable`。

可借鉴点（P1，M5 评估，M7 前决定）：

- 调研 LSP 3.18 pull diagnostics 与 push 混用的规范：哪些客户端会只拉不推、`documentDiagnostic` 返回后是否还发 push、version/relatedDocuments 语义。
- 若实现，初期只支持 `FullDocumentDiagnosticReport`，返回当前 revision 的已排序诊断；不实现 unchanged 优化。
- 把 tsgo 的 flake 检测思想改成测试工具：`vimls --flake` 或 test hook 下对同一文档连续分析两次，diff 诊断结果，抓 map 顺序、共享 AST 污染、workspace 状态泄漏。这比上线时埋点更符合 vimls“panic 是 bug”的原则。

### 2.8 Checker pool：诊断专用 + 查询亲和

typescript-go `internal/project/checkerpool.go`：

- 每个 program 的 pool 有 1 个 diagnostics checker（保证诊断遍历顺序一致）+ N-1 个 query checker（默认 N=4）+ 1 个 API persistent checker。
- query checker 有 request affinity 和 file affinity；请求 context 结束后自动清 request 关联；被 cancel 的 checker 立即丢弃，避免复用已取消状态。
- idle 30s 清理 query/diagnostics checker；pool 被替换后 `Discard` 停止 timer 但不销毁，等待旧快照使用者结束。

vimls-go 现状：

- 分析是全局最多 `min(GOMAXPROCS,4)` 个 worker，每个文档分析自包含；没有跨文件共享的检查器缓存。
- M4 计划中的跨文件类型信息尚未开始。

可借鉴点（P2，仅在 M4 出现真实共享 solver 后）：

- 未来如果出现“每个 workspace graph revision 一个语义分析器/缓存”，照抄三池模型：诊断固定单池保证顺序；请求/文件亲和减少乒乓；cancel 后丢弃而不是 reset；idle 回收。
- 当前不要建空 `checkerpool` 包；AGENTS 明确禁止提前建包。

### 2.9 文件事件过滤、合并与过量失效

typescript-go `internal/project/session.go` 的 `DidChangeWatchedFiles`：

1. 把 create/change/delete 映射成内部 `FileChangeKind`；
2. 判断 config 文件是否受影响；
3. 判断扩展名或目录是否 relevant；
4. 只有 relevant 才安排 diagnostics refresh，只有 config 变化才安排 snapshot update；
5. `filechange.go` 定义 `excessiveChangeThreshold = 1000`，超过后走 `InvalidateAll` 或按 node_modules 局部失效，而不是逐文件 rebuild。

vimls-go 现状：

- `DidChangeWatchedFiles` 对任意事件全量重建。
- `buildWorkspaceIndex` 每次重新 walk、read、parse、index。

可借鉴点（P0，M5）：

- 第一步不引入复杂失效策略，只做：
  - 事件相关性过滤（`*.vim`、runtime 目录、workspace 目录）；
  - 500ms 去抖合并；
  - 单批超过 1000 个事件：放弃增量、清 workspaceFiles 并全量重建一次，发一条 warning；
  - 注册 watch 后保留现有的“再扫一次”逻辑，防止注册窗口漏事件。
- 第二步结合 2.5 的哈希缓存做增量 reindex。

### 2.10 内置 fswatch 回退（有条件借鉴）

typescript-go 的 `internal/fswatch` 有 FSEvents/inotify/fanotify/kqueue/Windows 后端，选择策略是：

- 客户端支持动态 watch registration → 用 LSP watcher；
- 否则，只有 Windows（ReadDirectoryChangesW）或 macOS FSEvents 这种内核级递归后端才启用内置 watcher；
- Linux inotify 递归需要大量 fd 和目录 walk，因此不作为静默 fallback。

vimls-go 现状：

- `internal/server/workspace.go` 明确“server 不创建 filesystem watcher，不轮询 roots”，只动态注册 `**/*.vim`；客户端不支持动态注册时没有 fallback。

建议：**1.0 前不照搬。** 理由：

- vimls-go 的架构文档和 AGENTS 把“客户端拥有文件监听”写死为边界，违背它需要单独的 planning change。
- tsgo 的 fswatch 是 10k LOC 的平台代码，包含 cgo-free FSEvents 汇编路径；vimls 的必需面没有这么大。
- 若未来 Neovim/Vim 客户端出现“无动态注册但需要可靠刷新”的真实案例，优先考虑用维护良好的 `fsnotify`（仅递归目录订阅）做显式 fallback，或移植 tsgo 的“fast-recursive 检测”开关，而不是移植全部后端。

### 2.11 Parser pool、mark/rewind 与错误去重

typescript-go parser 的三个工程细节：

- `internal/parser/parser.go` 用 `sync.Pool` 复用 parser 及其 scanner（`getParser/putParser`），`putParser` 只保留 scanner 和闭包，重置其余字段。
- `mark()/rewind()` 把 scanner state、诊断长度、context flags 打包成 `ParserState`，lookahead 可任意试探后回滚。
- `parseErrorAtRange` 在相同 start position 不重复报错，避免恢复路径产生诊断风暴。

vimls-go 现状：

- `internal/syntax` 没有 parser pool；`Parse` 每次新建 parser。
- 已有独立的 expression parser 和 type parser，lookahead 实现因 parser 而异。
- 诊断排序在 server 层统一稳定。

可借鉴点（P1，需 benchmark）：

- 在 `tools/benchlegacy` 中加一个“1 KiB/10 KiB/100 KiB 文档、连续 Parse、GC off”的 alloc 对比；如果 parser 初始化和中间 slice 分配显著，再给顶层 `parseSource` 加 `sync.Pool`。**先测后改**，否则违反 `docs/architecture.md`“不猜测 slice 容量、不提前引入池/arena”的纪律。
- `mark/rewind` 模式对嵌套 payload、`:filter` 后的试探、Vim9 表达式边界判定有价值；但 vimls parser 是 Ex-command 驱动，按行同步点恢复，不能直接套用 tsgo 的 token-level lookahead。
- “同位置去重”值得作为 parser 层的最后一道防线，与现有 server 层 cap 配合。

### 2.12 请求级 panic 恢复与隐私化堆栈

typescript-go `internal/lsp/server.go`：

- 每个语言服务请求的异步函数里 `defer s.recover(req)`。
- panic 被记录完整 stack 到 logger，向客户端返回 `-32603 InternalError`；telemetry 开启时发送 sanitized stack（`stack_sanitizer.go` 会把非 `typescript-go/internal` 帧替换成 `(REDACTED FRAME)`，并避免 VS Code telemetry 的 secret 正则误伤）。
- 测试层有 `testutil.RecoverAndFail` 反例，确保测试中 panic 仍可见。

vimls-go 现状：

- `internal/syntax` 规定 panic 是 parser bug，必须让测试/fuzz 失败；这是正确的语言侧策略。
- server handler 层没有显式 recover 中间件。

可借鉴点（P0，M4）：

- 在 `cancellationHandler/lifecycleHandler` 之外加一个 `recoverHandler`，只包住业务 handler：
  - 记录完整 stack 到 stderr；
  - 对 request 返回 `-32603`，对 notification 只记录；
  - 不吞 context.Canceled、不吞 JSON-RPC 协议错误。
- 测试策略分两层：parser/analysis 单元测试继续让 panic 直接失败；server 集成测试新增“handler panic 后连接仍可处理下一请求”的用例。
- 不实现 telemetry/stack sanitizer，1.0 没有遥测通道；完整 stack 只写 stderr，不写 diagnostics、不写 logMessage。
- 这与“panic 是 bug”不冲突：恢复是为了不让一个 bug 杀死整个会话，但 bug 必须留下可定位证据。

### 2.13 日志分级与开发调试命令

typescript-go：

- `internal/lsp/logger.go` 在 initialize 前写 stderr，initialize 后走 `window/logMessage`，受 `custom/setLogVerbosity` 控制；`$/setTrace` 显式 no-op。
- 提供 `custom/runGC`、`custom/saveHeapProfile`、`custom/saveAllocProfile`、`custom/startCPUProfile`、`custom/stopCPUProfile`、`custom/projectInfo` 等调试方法。
- `cmd/tsgo` 支持 `TS_GO_DEBUG_STACK_LIMIT` 调大 Go 栈限制，避免深度递归测试挂死。

vimls-go 现状：

- 所有日志写 stderr，stdout 纯净；只有配置 warning 主动发 `window/logMessage`。
- 没有 profile/GC 调试通道，profile 靠外部 `go test -bench` 和 pprof 文件。

可借鉴点（P2，M7）：

- 保留 stderr 为默认路径；增加 `vimls/setLogVerbosity` notification 作为 opt-in 客户端日志开关，默认 `off` 或 `warning`，避免向所有客户端刷 `window/logMessage`。
- `vimls/runGC` 适合长期会话和大 workspace 排障；heap/cpu profile 命令必须显式限制输出路径（拒绝覆盖任意文件），且只在 initializationOptions 显式打开时注册。
- `TS_GO_DEBUG_STACK_LIMIT` 或等价环境变量对 parser 深递归测试有价值，可以跟随 parser 深度上限一起设计。

### 2.14 Baseline 测试工作流

typescript-go 的 baseline 机制（`internal/testutil/baseline/baseline.go` + `Herebyfile.mjs`）：

- 测试把实际输出写到 `testdata/baselines/local/`，并与 `reference/` 比较；不同就写 local 文件并 fail。
- `hereby baseline-accept` 把 local 复制到 reference，`.delete` 文件删除 reference。
- 测试运行期间通过 `TSGO_BASELINE_TRACKING_DIR` 记录每个用到的 baseline；测试结束后扫 reference 中未使用的 baseline，强制清理。
- CI 失败时自动跑 `baseline-accept`，`git diff --staged --exit-code` 后打印 reference 需要的变更，并上传 local artifact。
- 对 TypeScript submodule 的基线差异，分为 `submodule/`、`submoduleAccepted/`、`submoduleTriaged/` 三类，用 diff 文件治理“已知不一致”。

vimls-go 现状：

- parser golden 断言多在 Go 测试代码内联；official corpus 以 committed compressed artifacts 形式存在。
- CI 不自动接受 baseline，也不上传 diff artifact；测试失败需要本地复现。

可借鉴点（P1，M5）：

- 为“AST 序列化、官方 parser 失败矩阵、诊断 range 快照”引入轻量 baseline 目录和 `make baseline-accept`/`make baseline-diff`。
- CI 保持“永不自动改写 reference”，但像 tsgo 一样在失败 job 中生成 diff 并上传 artifact。
- 未使用 baseline 扫描对 vimls 同样重要：防止删除测试后 baseline 腐烂。
- 不建议把官方 Vim checkout 变成运行时依赖；vimls 已 committed artifacts，继续离线。tsgo 的 `accepted/triaged` 治理可以映射到 vimls 的 `parser-files.json` + helper inventory，不需要 submodule。

### 2.15 Fourslash 内联 LSP 场景测试

typescript-go 的 `internal/fourslash`：

- 单个 Go 测试文件用 `////` 标记描述多文件内容、光标、range、symlink、全局选项和期望 LSP 结果。
- `newFourslash` 启动内存 vfs + 真实 `lsp.Server` + `lsptestutil.LSPClient`，走完整协议。
- 4,359 个场景文件证明这套 harness 能支撑大规模特性矩阵，同时保持每个用例自包含。

vimls-go 现状：

- 有 `test/integration/lsp_subprocess_test.go` 走真实 stdio 子进程，覆盖面是里程碑主链路。
- 包内 server 测试直接调 handler，快但不覆盖 wire 层和 lifecycle。

可借鉴点（P1，M6）：

- 做一个小型 `internal/server/lsp_test_harness`（或 `test/lsptest`），用内存 pipe 启动 server，支持 “send request/notification、等 response、收集 publishDiagnostics” 三种原语即可。
- 不必移植 fourslash 的标记语言；vimls 测试风格偏显式 Go table，先提供显式 client API，标记语法等用例规模上来再说。
- 目标是让 M6 的 completion/rename/semantic tokens 和 M7 的 editor smoke 前面多一层快速回归，而不是取代 subprocess smoke。

### 2.16 CI 矩阵与发布证据

typescript-go `.github/workflows/ci.yml` 的测试矩阵：

- ubuntu coverage main、windows、macOS（PR runner 不足时仅 main）、no submodules、race（XL runner）、noembed、concurrent test programs。
- 每个 job 还跑 benchmarks、tools tests、API tests，最后 `git diff --staged --exit-code`。
- 另有 CodeQL、build/release、extension 等独立 job。

vimls-go `.github/workflows/ci.yml` 当前：

- Linux/macOS × Go 1.26 的 `make format-check test race vet build`；coverage 单独 Linux job。

可借鉴点（P1/P2，M7）：

- 增加 Windows build + subprocess test（roadmap 已要求）。
- 增加“完全离线 gate”：`GOPROXY=off GOSUMDB=off make check`，证明 committed artifacts + module cache 足够。
- 增加 benchmark 编译/运行 lane（`-run=- -bench=. -benchtime=1x`），防止 benchmark 代码腐烂。
- 增加 CodeQL 和 `govulncheck` 计划 lane。
- 失败时上传 parser triage 或 baseline diff artifact；不用 codecov 也没关系。

### 2.17 性能遥测与容量信号

typescript-go `internal/project/session.go` 的 `StartPerformanceTelemetry`：

- 仅当客户端显式 `enableTelemetry` 才启动；
- 周期性读取 Go `runtime/metrics`（heap live、GC、goroutine 等）+ `go-osstat` 系统内存，并统计 open files/project count/cache count；
- 通过 `telemetry/event` 发给客户端，失败只记日志；
- project info telemetry 用 `seenProjects` 去重。

vimls-go 现状：

- 有 resource limits，但运行时只输出 log，没有统计通道。

可借鉴点（P2，M7+）：

- 不引入外部遥测服务；利用 LSP `telemetry/event`，显式 opt-in。
- 指标选最少的：open documents、workspace files、index bytes、parse cache entries、分析队列深度、goroutine 数、heap live、GC CPU。与资源 limit 对照，提前发现“快到 20,000 files / 256 MiB / 200 diagnostics cap”的现场。
- 如果 M7 时间不够，可以先做 `vimls/status` 调试请求写 stderr，遥测后置。

### 2.18 依赖约束与 lint

typescript-go `.golangci.yml` + 自定义 plugin：

- `depguard` 禁止生产包直接 import `encoding/json`（必须走内部 json wrapper）；
- `forbidigo` 禁止核心包使用 `os`、`path/filepath`、`runtime.GOOS`，强制通过 `osutil/nativepath/tspath` 等 host 抽象；
- 启用 30 个左右小 linter（errorlint、modernize、usestdlibvars、bodyclose 等）；
- 格式化用 dprint + gofumpt 插件，而非裸 gofmt。

vimls-go 现状：

- `gofmt` + `go vet`；依赖只有 `go.lsp.dev/*`。
- `internal/workspace` 直接使用 `os`/`filepath`，这是其职责所在；`internal/syntax/analysis` 基本无 I/O。

可借鉴点（P2，可选）：

- 不要上自定义 golangci plugin；如果 M7 引入 golangci-lint，只启用默认可用、零自定义的 linter 子集：`errorlint`、`usestdlibvars`、`depguard`（只禁生产包 `encoding/json` 的裸用）、`misspell`、`nolintlint`。
- `forbidigo` 对 vimls 可以只限制 `internal/syntax`、`internal/analysis`、`internal/text` 不得用 `os`/`filepath`，这能机械保证“语法/分析不碰进程和文件系统”的架构边界。
- 是否从 gofmt 迁到 dprint/gofumpt 属于风格决策，不带来功能价值，1.0 前不必改。

### 2.19 上游迁移治理与“有意差异”台账

typescript-go：

- `CHANGES.md` 逐组件记录 Go 版与 TS 旧版的有意差异，每条给出旧行为、新行为和理由。
- baseline 的 `submoduleAccepted/submoduleTriaged` 文件把“与上游测试不一致但已裁决”的状态版本化，避免每次回归都重新讨论。
- `README` 用 done/in progress/prototype/not ready 明确特性状态，防止把“能解析”当成“完成”。

vimls-go 现状：

- `docs/diagnostics.md` 已按 E 码记录 Vim 证据和保守边界；`language-support.md` 和 roadmap 的状态更新纪律严格。
- `testdata/official/parser-files.json` 是 44-file allowlist + 24 显式排除 + 294 默认排除的迁移边界，治理思想接近 tsgo 的 triaged。

可借鉴点（P0，低成本）：

- 新建 `docs/vim-compat-changes.md`，记录 vimls 与真实 Vim 行为的每一处有意差异，例如：混合方言宽松恢复、某些 runtime 状态判为 unknown、E1406→E1369 的版本化重写。每个条目必须有 Vim 版本、证据位置、差异原因、何时可移除。
- 版本升级时，用同一文件做 diff 清单，而不是散落在 PR 描述里。
- 继续坚持 roadmap 状态定义；tsgo 的 “done / in progress / prototype” 可以直接借用来审计 M4/M5 的剩余项。

---

## 3. 不建议现在迁移的东西

| 项 | tsgo 做法 | 不建议理由 |
| --- | --- | --- |
| 完整 Session/Snapshot/Project 体系 | `SnapshotFS`、`ProjectCollection`、`programCounter`、`ConfigFileRegistry` 一体 | vimls 没有 tsconfig/program 概念；照搬会产生大量空转层，违反 AGENTS“不要 Manager/Factory/Registry” |
| content-mapper 外部进程 | 扩展语言映射器可 spawn 进程 | vimls 安全红线是“绝不执行用户脚本/外部 payload”；即使未来做 embedded-language delegation，也必须先有静态边界 |
| ATA / npm install | 自动获取 @types | 无对应物 |
| 完整自研 fswatch | FSEvents/inotify/fanotify/kqueue 10k LOC | 客户端监听已是 vimls 架构边界；真实需要时优先 fsnotify 或仅移植 fast-recursive 开关 |
| 872KB 完整 LSP codec | 从 meta-model 全量生成 | 维护面大；当前 go.lsp.dev/protocol 够用，只有明确摩擦累积后再做裁剪版 |
| parser arena / node factory / 生成 AST | `core.Arena`、`ast_generated.go` | vimls AST 是手写命令节点，收益不同；架构文档明确“没有更强测量证据不引入 arena/pool” |
| 本地化诊断 | TS 的 12 个 locale gzip | vimls 1.0 稳定英文；Vim 错误文本本地化证据不完整 |
| 无界 framing | tsgo `baseproto` 只查 Content-Length，无 header/body 上限 | 这是 tsgo 的弱点，不是优点；vimls 必须保留 8KiB header / 16MiB body 限制 |
| 运行时读 submodule | 测试依赖 `_submodules/TypeScript` | vimls committed artifacts + `GOPROXY=off make check` 更可复现，不要倒退 |
| 外部格式化/Node 构建链 | Hereby/dprint/npm 工作区 | vimls 纯 Go + Makefile 已足够；引入 Node 工具链只会在 M7 增加供应链成本 |

---

## 4. 分期采纳路线图

优先级定义：

- **P0**：不依赖新架构、直接改善 M4/M5 质量与性能，建议最近两个 milestone 做。
- **P1**：有明确里程碑触发点（M5/M6），需要先写小实验或 benchmark。
- **P2**：1.0 后或出现真实痛点时再做。

| # | 借鉴项 | 落地位置 | 价值 | 成本 | 风险 | 阶段 |
| ---: | --- | --- | --- | --- | --- | --- |
| 1 | 集中式 `vim/E` + `vimls/` 诊断目录 | `internal/vimdata/errors_generated.go` + `tools/genvimerrors` | 高 | 中 | 低 | P0 / M4 |
| 2 | 分析去抖（文档 URI 级） | `internal/server/server.go` 分析调度 | 高 | 中 | 中：需保证 stale 防护不回归 | P0 / M4 |
| 3 | 请求级 panic 恢复 | `internal/server` 中间件 | 高 | 低 | 低 | P0 / M4 |
| 4 | watched-file 事件过滤 + 去抖 + 过量阈值 | `internal/server/workspace.go` | 高 | 中 | 中 | P0 / M5 |
| 5 | “有意差异”台账 | `docs/vim-compat-changes.md` | 中 | 低 | 低 | P0 / 随时 |
| 6 | parser pool / mark-rewind benchmark | `tools/benchlegacy` + `internal/syntax` | 中 | 低 | 低：只测不改 | P1 / M4 末 |
| 7 | workspace parse cache + 增量 reindex | `internal/workspace` | 高 | 高 | 高：AST 共享和索引一致性 | P1 / M5 |
| 8 | `IndexSnapshot` + revision 一致性读 | `internal/workspace/index.go` | 中 | 中 | 中 | P1 / M5 |
| 9 | baseline local/reference/accept 工作流 | `testdata/baselines` + Makefile + CI | 高 | 中 | 低 | P1 / M5 |
| 10 | pull diagnostics 评估与实现 | `internal/server` | 中 | 中 | 中：push/pull 混用语义 | P1 / M5–M6 |
| 11 | 小型 in-process LSP harness | `test/` 或 `internal/server` test 工具 | 高 | 中 | 低 | P1 / M6 |
| 12 | 日志分级 + `vimls/setLogVerbosity` | `internal/server` | 中 | 低 | 低 | P2 / M7 |
| 13 | `vimls/runGC` 与 profile 调试请求 | `internal/server` | 低 | 低 | 中：路径安全 | P2 / M7 |
| 14 | 性能遥测 opt-in | `internal/server` | 中 | 中 | 低 | P2 / M7+ |
| 15 | CI 矩阵扩展（Windows/offline/benchmark/CodeQL） | `.github/workflows/ci.yml` | 高 | 低 | 低 | P1 / M7 |
| 16 | golangci-lint 子集 | `.golangci.yml` | 中 | 中 | 低 | P2 / M7 |
| 17 | 父进程 watchdog | `cmd/vimls` | 中 | 低 | 平台差异 | P2 / M7 |
| 18 | 裁剪版 LSP codec 评估 | research branch 对照测试 | 中 | 高 | 高 | P2 / 1.0 后 |
| 19 | 内置 watcher fallback 评估 | 独立 spike | 低 | 高 | 高 | P2 / 1.0 后 |

---

## 5. 每项建议的验收标准

### 5.1 P0 项

**诊断目录**

- `tools/genvimerrors` 能校验 `VIM_SOURCE` 精确解析到 `v9.2.1015` / commit `5ab969f719bb09555e90e8dff8c94fc37bcbf2ae`。
- 已支持的所有 `vim/E...` 都能从 `internal/vimdata/errors_generated.go` 查到 code/severity/message；`protocolDiagnosticSeverity` 不再硬编码 E117/E121/E1001/E1089。
- 未迁移的 E 码查表返回“unsupported”，不伪造消息。
- `make check` 和 `GOPROXY=off GOSUMDB=off make check` 均通过。

**分析去抖**

- `didChange` 仍立即产生正确的 `text.Snapshot`（现有 document sync 测试全部通过）。
- 同一 URI 在 debounce 窗口内的多次 change 只安排一次后台分析；`didOpen` 不等待窗口。
- 同步 `Hover/Completion/DocumentSymbol` 等请求仍走 `structureDocument` 实时 parse，不感知去抖。
- 快速 change 后 close：不得发布旧诊断；`go test -race ./internal/server ./test/integration` 通过。

**panic 恢复**

- handler panic 返回 JSON-RPC `-32603`，连接不断，下一请求正常。
- stderr 含完整 stack；notification panic 不回包、只记录。
- parser/analysis 单测中的 panic 仍直接失败（恢复只装在 server 层）。

**文件事件过滤/去抖**

- `.txt` 或无关目录事件不触发 rebuild；`*.vim` 事件在窗口内合并成一次。
- 超过 1000 个相关事件时只安排一次全量 rebuild，并发送一条 warning。
- 现有 watcher 注册窗口“再扫一次”测试不回归。

**有意差异台账**

- 每条差异有 Vim 版本、`src/testdir` 或 runtime doc 证据、vimls 行为、原因。
- M7 release report 能直接引用该文件作为 known differences。

### 5.2 P1 项

**parse cache + 增量 reindex**

- 未变化文件在 rebuild 时不重新 `os.ReadFile`/`syntax.Parse`（benchmark 有计数证据）。
- 单文件 watch change 的 rebuild 时间与变化文件数相关，与 workspace 总文件数无关。
- 缓存条目不共享 `Diagnostics` slice；两次分析同一 AST 不互相污染。
- `-race` 下并发查询和 rebuild 通过。

**IndexSnapshot**

- workspace symbols 和 cross-file navigation 从同一个 revision 读取。
- 旧请求持有的 snapshot 在 rebuild 后仍可安全完成，不被新 index 内容破坏。

**baseline**

- `make baseline-accept` 只移动 local → reference；`make baseline-diff` 输出 readable diff。
- CI 失败 job 上传 local artifact；repo 中不存在未使用 baseline。
- 生成器/测试失败不会自动改写 reference。

**pull diagnostics**

- 实现前先落一篇短 doc：哪些客户端/场景必须 pull、与 push 如何互斥或共存、version 如何处理。
- subprocess 测试覆盖“支持 pull 的客户端”与“仅 push 的旧客户端”。

**in-process LSP harness**

- 能完成 initialize → didOpen → 请求 → publishDiagnostics → shutdown 全链路。
- 保持现有 subprocess smoke 不变；harness 只增不减。

### 5.3 P2 项

- 日志/遥测/profile 均显式 opt-in；stdout 纯净。
- Windows CI 至少跑 build + subprocess test；offline gate 成为 release 证据之一。
- 任何 codec/watcher spike 先出 benchmark 和对照测试，再进入主分支规划。

---

## 6. 结论

typescript-go 证明了一件事：一个从旧实现移植来的、规模很大的 Go 语言服务器，可以靠**不可变快照、按需 flush、generation 去抖、内容哈希缓存、分层诊断、基线化上游测试**保持可维护性和性能纪律。vimls-go 不需要它的 1.15M 行规模，也不需要 project/checker/content-mapper 全家桶；它需要的是把这些已经验证的“工程机制”按 vimls 自己的里程碑裁剪进去。

建议的第一批行动（按性价比排序）：

1. 建立 `docs/vim-compat-changes.md` 和集中式诊断目录（M4，低成本高治理收益）。
2. 给后台分析和 watched-file rebuild 加去抖/过滤/阈值（M4/M5，直接改善 CPU 与响应）。
3. 给 server 加 panic 恢复中间件（M4，低风险）。
4. 在 `tools/benchlegacy` 先测 parser pool，再决定是否落地（M4/M5）。
5. 为 workspace reindex 引入哈希缓存和 `IndexSnapshot`（M5，收益最大但需要最小心）。
6. 在 M5/M6 建立 baseline accept/diff 工作流和小型 LSP harness，M7 扩展 CI 矩阵与发布证据。

同时保留 vimls-go 已有的三个优势：bounded framing、纯 Go/Makefile 工具链、committed official artifacts 离线测试。不要用 typescript-go 的 Node/Hereby、submodule 运行时依赖和无界 framing 替换它们。

---

## 附录 A：关键文件索引

### typescript-go

| 关注点 | 路径 |
| --- | --- |
| CLI 入口 | `cmd/tsgo/main.go`、`cmd/tsgo/lsp.go` |
| LSP 三循环、handler 注册、recover | `internal/lsp/server.go`（`Run` 约 :855，`readLoop` :879，`dispatchLoop` :964，`writeLoop` :1025，`handleRequestOrNotification` :1140，`recover` :1475） |
| context-aware 队列 | `internal/lsp/dynamic_queue.go` |
| 生成协议与严格解码 | `internal/lsp/lsproto/lsp.go`、`structcodec.go`、`jsonrpc.go`、`_generate/generate.mts`、`_generate/fetchModel.mts` |
| 日志/堆栈清洗 | `internal/lsp/logger.go`、`internal/lsp/stack_sanitizer.go` |
| Session/Snapshot | `internal/project/session.go`（`DidChangeWatchedFiles` :483，`scheduleDiagnosticsRefresh` :577，`ScheduleSnapshotUpdate` :640，`getSnapshot` :1091，`updateSnapshot` :1437）、`internal/project/snapshot.go`（`Clone` :265，`ref` :572，`Deref` :595） |
| 文件变化模型与阈值 | `internal/project/filechange.go`（`excessiveChangeThreshold` :8） |
| parse cache / ref-count | `internal/project/parsecache.go`、`internal/project/refcountcache.go` |
| dirty/COW | `internal/project/dirty/map.go`、`syncmap.go`、`box.go` |
| overlay/snapshot FS | `internal/project/overlayfs.go`、`internal/project/snapshotfs.go` |
| checker pool | `internal/project/checkerpool.go`（`GetChecker` :123，`getQueryChecker` :252，`cleanupIdleCheckers` :428） |
| watch registry | `internal/project/watch.go` |
| fswatch | `internal/fswatch/watcher.go`、`fsevents_darwin.go`、`inotify_linux.go`、`kqueue.go` |
| parser pool/lookahead | `internal/parser/parser.go`（`parserPool` :120，`ParseSourceFile` :135，`initializeState` :291，`parseErrorAtRange` :330，`mark` :352，`rewind` :365） |
| 诊断目录 | `internal/diagnostics/diagnostics.go`、`diagnostics_generated.go`、`generate.go`、`loc/` |
| 语言服务分层 | `internal/ls/languageservice.go`、`host.go`、`diagnostics.go` |
| fourslash harness | `internal/fourslash/fourslash.go`（`newFourslash` :177）、`test_parser.go`、`tests/` |
| baseline | `internal/testutil/baseline/baseline.go`（`Run` :30）、`internal/testutil/fsbaselineutil/differ.go` |
| 构建/测试/发布 | `Herebyfile.mjs`（`runTests`、`baselineAcceptTask`、`allChecks`）、`.github/workflows/ci.yml` |
| 差异台账 | `CHANGES.md` |
| lint/格式 | `.golangci.yml`、`.custom-gcl.yml`、`.dprint.jsonc` |

### vimls-go

| 关注点 | 路径 |
| --- | --- |
| server 主循环/lifecycle/分析调度 | `internal/server/server.go`（`Run`、`cancellationHandler`、`lifecycleHandler`、`startAnalysis`、`analysisWorker`、`analyzeDocument`、`publishSyntax`） |
| workspace rebuild/watch | `internal/server/workspace.go`（`scheduleWorkspaceRebuild`、`workspaceIndexWorker`、`buildWorkspaceIndex`、`DidChangeWatchedFiles`、`vimFileWatchers`） |
| 文档快照 | `internal/workspace/documents.go` |
| 文本/位置 | `internal/text/snapshot.go` |
| 索引/图 | `internal/workspace/index.go`、`import_graph.go` |
| 诊断定义 | `internal/syntax/vimls_diagnostics.go` |
| 诊断严重性映射 | `internal/server/server.go` 的 `protocolDiagnosticSeverity` |
| 生成器 | `tools/gencommands/main.go`、`tools/genbuiltins/main.go`、`tools/genoptions/main.go`、`tools/genvariables/main.go` |
| 架构/测试/路线 | `docs/architecture.md`、`docs/testing.md`、`docs/roadmap.md`、`docs/diagnostics.md` |
| CI | `.github/workflows/ci.yml` |
| 本地 gate | `Makefile` |

## 附录 B：规模对比

| 指标 | vimls-go | typescript-go |
| --- | ---: | ---: |
| Go 文件（internal+cmd） | 141 | 5,075 |
| `*_test.go` | 83 | 4,537 |
| Go 总行数 | ~86,944 | ~1,152,085 |
| 顶层内部包数 | 8 | 60+ |
| 官方测试资产 | 362 `.vim` 文件 + 3,267 抽取脚本 + 5,733 helper inventory，committed artifacts | TypeScript submodule + 4,359 fourslash 文件 + baseline reference |
| 默认测试入口 | `make check`（纯 Go） | `npx hereby test`（Node + Go） |
| 协议类型来源 | `go.lsp.dev/protocol` | 自生成 meta-model codec |
| framing 上限 | header 8KiB / body 16MiB | 无显式上限 |
| 文件监听 | 客户端 LSP watcher | 客户端优先，Windows/FSEvents 内置 fallback |

