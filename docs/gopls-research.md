# gopls 对 vimls-go 的借鉴价值研究

> 研究快照：
> - gopls：`/Users/chemzqm/lib/gopls`，目录形式源码快照，无 `.git` 元数据，无法按 tag/commit 精确溯源。`go.mod` 声明 `go 1.27.0`、`golang.org/x/tools v0.48.0` 且 `replace golang.org/x/tools => ..`；`internal/protocol/tsclient.go` 标注协议生成自 LSP meta-model `3.18.2`；`doc/release/` 最新草案为 `v0.24.0`；部分文件版权到 2026。本次把该快照当作“当前 gopls 工程状态”的样本，不把尚未发布的接口当作稳定依赖。
> - vimls-go：`/Users/chemzqm/vim-dev/vimls-go`，`main` @ `feb9636dace4a8460ce42a8ac4c31bacf0a6277e`（2026-08-31）。
> - 本报告只做阅读、比较和规划，不修改任何生产行为；所有行号均来自上述快照。

## 0. 结论摘要

gopls 对 vimls-go 最有价值的不是“Go 语言分析怎么写”，而是它用十年迭代换来的五类语言服务器工程能力：

1. **诊断的最终一致性与可治理性**：修改计数、取消上一轮、快照版本/哈希去重发布、快速语法路径 + 延迟全量路径、按 view 合并去重、诊断携带 source/codeDescription/tags/data。vimls-go 已有很好的 stale 防护，但缺少“去抖、去重、延迟全量”三层，正在做 M4 语义诊断时最值得先补。
2. **文件身份与缓存失效**：`file.Handle{URI, Hash, SameContentsOnDisk, Version}` 统一表达“编辑器 overlay”和“磁盘文件”；磁盘读取按 inode/mtime 缓存；解析结果按内容哈希缓存。vimls-go 的 watch 事件目前任何一次都全量重读、重解析 workspace，这是 M5/M7 性能风险最大的点。
3. **配置、协议、文档的单一事实源**：Options 结构体既是运行时配置，也是 `doc/settings.md` 和 `api-json` 的生成来源；LSP 类型从 pinned meta-model 生成；`workspace/executeCommand` 命令由接口 + 生成器分发。vimls-go 目前配置解析散在 `internal/server/config.go` 的裸 JSON 分支里。
4. **可观察性**：progress tracker 的 WorkDone/ShowMessage 双轨、`-debug` HTTP 服务、RPC 直方图、延迟桶、client 计数。vimls-go 现在只有 stderr 日志和 warning，长 workspace 构建对用户不可见。
5. **测试工程**：txtar + marker 注释驱动端到端 LSP 断言、fake editor + Awaiter 等待异步通知、in-process/forwarded/separate-process 多模式。vimls-go 目前只有一个 466 行的手写 subprocess 测试，行为增长后维护成本会快速上升。

不建议照搬的东西同样明确：Session/View/Snapshot 三层、go/packages 加载与 type-checking package handle、go/analysis driver、100MB heap ballast、完整 daemon/MCP/web 交互面、约 520KB 的大型生成协议 codec。vimls-go 的 `AGENTS.md` 原则——“最小直接实现、客户端拥有文件监听、不执行用户脚本、先验证 Vim/LSP 实际行为”——仍然优先于 gopls 的任何大项目机制。

下面的优先级沿用本仓库已有研究文档的 P0/P1/P2/P3：

- **P0**：M4/M5 就应排入实现切片，当前代码有明确缺口，改动小、收益直接。
- **P1**：在 M5/M6 规划，需要先冻结设计再动。
- **P2**：有真实场景或 benchmark 证据后再做。
- **P3**：仅做技术储备，不在 1.0 前引入。

---

## 1. 研究范围与方法

### 1.1 仓库规模

gopls（只统计本目录，不含被 `replace` 指向的父目录）：

| 指标 | gopls 快照 | vimls-go @ feb9636 |
| --- | ---: | ---: |
| Go 文件 | 646 | 143 |
| 其中 `*_test.go` | 214 | 84 |
| Go 总行数 | 约 165,123 | 约 87,597 |
| 顶层内部包 | 29（`internal/` 直接子包） | 8 |
| 协议类型来源 | `internal/protocol` 从 LSP 3.18.2 meta-model 生成 | `go.lsp.dev/protocol` |
| 依赖规模 | go.mod 直接依赖 17 项，含 gofumpt、staticcheck、vuln、MCP SDK | 直接依赖 2 项 |
| 测试资产 | `internal/test/marker/testdata` 395 个 txtar 场景文件 + fake editor/Awaiter | committed Vim corpus、生成 cases、1 个 subprocess 测试 |

关键包规模（生产代码行数，不含测试）：

| gopls 包 | 行数 | 角色 |
| --- | ---: | --- |
| `internal/golang` | 34,397 | Go 语言特性实现 |
| `internal/cache` | 19,485 | Session/View/Snapshot、解析/类型检查/分析缓存 |
| `internal/protocol` | 17,478 | 生成 LSP wire 类型、Mapper、URI |
| `internal/server` | 8,062 | 每个 LSP 方法一个 handler |
| `internal/test` | 6,870 | 集成测试 runner、fake editor、marker 框架 |
| `internal/settings` | 2,790 | 配置树、默认值、枚举、文档源 |
| `internal/cmd` | 5,081 | CLI 子命令和调试入口 |

vimls-go 对应规模（生产代码行数）：

| vimls-go 包 | 行数 | 角色 |
| --- | ---: | --- |
| `internal/syntax` | 17,438 | 双根 parser、恢复、语法诊断 |
| `internal/analysis` | 10,949 | scopes、符号、类型、诊断 |
| `internal/server` | 4,549 | LSP handler、调度、发布 |
| `internal/workspace` | 2,240 | 文档、索引、import graph、文件发现 |
| `internal/vimdata` | 2,189 | 生成命令/函数/选项/变量元数据 |
| `internal/text` | 231 | 快照和位置编码 |
| `internal/jsonrpc` | 257 | bounded framing |

### 1.2 可比性

两个项目高度同构，比较点如下：

| vimls-go | gopls | 同构点 / 差异 |
| --- | --- | --- |
| `cmd/vimls` + `--listen` | `main.go` + `internal/cmd/serve.go` | 单可执行文件；gopls 还提供 daemon/forwarder |
| `internal/jsonrpc` 自实现 bounded framing + `go.lsp.dev/jsonrpc2` | `x/tools/internal/jsonrpc2` + `internal/lsprpc` | vimls framing 上限更严格；gopls 会话/监听分层更完整 |
| `go.lsp.dev/protocol` | `internal/protocol`（meta-model 3.18.2 生成） | 都坚持内部 byte offset、边界转换；gopls 类型覆盖更完整 |
| `internal/text.Snapshot` | `internal/file.Handle` + `protocol.Mapper` | 都是不可变文档视图 + 版本 |
| `internal/syntax` 双根 parser | `internal/cache/parsego` + `golang` | vimls 恢复模型更显式；gopls 针对 go/parser 做树修复 |
| `internal/analysis` 保守分析 | `internal/analysis` + `go/analysis` driver | vimls 固定序列；gopls 模块化 analyzer DAG |
| `internal/workspace` index/graph | `internal/cache` Session/View/Snapshot + metadata.Graph + filecache | vimls 单 workspace 更简单；gopls 为多 build 视图复杂化 |
| `internal/vimdata` 生成表 | `internal/settings` 生成 settings/analyzers 文档 | 两者都从单一来源生成 |
| `testdata/official` + committed artifacts | marker testdata + fake editor + integration modes | vimls 离线官方 corpus 更强；gopls LSP 端到端测试更强 |
| `Makefile` gate | doc/generate + 多套测试 harness | vimls 更轻、完全离线 |

### 1.3 方法

逐层阅读 `doc/design/implementation.md`、`doc/features/*.md`、`doc/settings.md`、`doc/workspace.md`、`doc/daemon.md`，再读 `internal/server`、`internal/cache`、`internal/settings`、`internal/file`、`internal/filecache`、`internal/filewatcher`、`internal/progress`、`internal/test` 的关键控制流；与 vimls-go 的 `AGENTS.md`、`docs/architecture.md`、`docs/roadmap.md`、`docs/testing.md` 和实际代码逐项对照。未运行 gopls 的大规模测试；本报告不改变任何行为。

---

## 2. gopls 的核心架构

### 2.1 进程模型：sidecar、daemon、forwarder、MCP 同进程

`main.go` 只做三件事：允许 linker 覆盖版本、启动 telemetry、调用 `cmd.Main()`。真正的入口在 `internal/cmd/serve.go`：

- 默认 stdio：用 `fakenet.NewConn("stdio", os.Stdin, os.Stdout)` 包成双向流，`jsonrpc2.NewHeaderStream` + `ss.ServeStream`。
- `-listen` 时变成 daemon：`jsonrpc2.ListenAndServe` 接受多个连接，每个连接由 `internal/lsprpc.StreamServer.ServeStream` 创建一个独立 `cache.Session` + `server`，但共享 `cache.Cache`。
- `-remote` 时进程退化为 forwarder：`lsprpc.NewForwarder` 把本进程的 stdio LSP 流转发给共享 daemon，自己只负责 trace/日志/telemetry。
- `-mcp.listen` 可在同一进程同时服务 MCP，共享 LSP session 内存（attached mode）。

对 vimls-go 的意义：gopls 明确区分 **进程**、**会话（LSP connection）**、**视图（workspace folder/build）**、**快照（某一时刻状态）**。vimls-go 现在是单进程单会话、单 workspace 视图，暂不需要前两层，但 TCP `--listen` 现在是单连接；如果未来 Vim/Neovim 多实例共享索引，gopls 的“daemon 共享 Cache、每连接独立 Session”是最接近的成熟参考。

关键证据：`internal/lsprpc/lsprpc.go:5` 包注释；`internal/lsprpc/lsprpc.go` 的 `ServeStream` 在每连接 `cache.NewSession(ctx, s.cache)` 后 `server.New(session, client, options)`；`internal/cmd/serve.go:29` 的 `serve` flags。

### 2.2 协议层：生成类型 + Mapper + URI 规范化

gopls 的 `internal/protocol` 是“字面转录 + 只做必要 Go 化”的生成代码：

- `tsclient.go:9-11` 标明生成自 `release/protocol/3.18.2` 的 metaModel.json，LSP metadata version 3.18.0。
- 服务端 handler 是一个大接口，`internal/server/unimplemented.go` 为未实现方法统一返回 `jsonrpc2.ErrMethodNotFound`，协议升级时编译器会强制补 stub（`server.New` 里有显式说明）。
- `protocol.Mapper`（`internal/protocol/mapper.go:90`）是 gopls 的位置转换中枢：内部用 byte offset，LSP 边界用 UTF-16；line index 惰性计算；明确处理 `\r\n`、EOF、越界。
- `protocol.ParseDocumentURI`（`internal/protocol/uri.go:170`）集中处理 VS Code 变体：`file://` 两斜杠补三斜杠、过度转义、Windows 盘符大小写、仅接受 file scheme。

vimls-go 已有 `internal/text` 的 UTF-8/UTF-16/UTF-32 转换和规范化路径；`legacyInitializeFields` 通过重新 marshal/unmarshal `InitializeParams` 来读 `rootPath/rootUri`，说明当前协议依赖的 wire 类型覆盖存在摩擦。完整生成 codec 是 P3，见 4.13。

### 2.3 server 层：每个方法一个 handler，按文件类型分发

`internal/server/server.go:88` 的 `server` 持有 `client`、`session`、`changedFiles`、`diagnostics`、`progress`、`options` 和“修改计数 + 待诊断 view”状态。每个 LSP 方法都先：

1. `s.session.FileOf(ctx, uri)` 取得 `file.Handle + Snapshot + release`；
2. `snapshot.FileKind(fh)` 判断 `Go/Mod/Sum/Work/Tmpl`；
3. 分发到 `golang`、`mod`、`work`、`template` 包；
4. 结束时 `release()` 释放 snapshot 引用。

这是 vimls-go 未来加入更多“文件方言”时可参考的分发方式；当前 legacy/Vim9 已经由 `syntax.Parse` 内部按 `vim9script` 首有效命令分发，不需要额外层。

另一个重要设计：`server` 的 `diagnosticsSema` 大小被硬编码为 1（`internal/server/server.go` 的 `New`）。gopls 顶层昂贵诊断 pass 全局串行，包内 type-check/analysis 再并行。这与 vimls-go 现在“每个 URI 最多并行 4 个 analysis worker”不同，但当 M4 引入跨文件语义分析后，值得重新测“顶层串行 + 内部并行”和“顶层并行”哪个对总延迟/内存更优。

### 2.4 settings：单一 Options 结构体

`internal/settings/settings.go:54` 的 `Options` 内嵌 `ClientOptions / ServerOptions / UserOptions / InternalOptions`：

- `Set(value any)`（:1031）只接受 JSON null/object；支持 VS Code 的 dotted alias（取最后一段）；每个字段有独立 `setOne` 分支，返回 `(applied CounterPath, err)`。
- 错误分 `SoftError`（弃用/可忽略，发送 warning）和硬错误（发送 error，但 gopls 仍继续用可解析部分）；`handleOptionResult`（`internal/server/general.go:735`）统一排序、去重并延迟到 initialized 后展示。
- `ForClientCapabilities`（:1074）把 client capabilities 折叠进 options：snippet、configuration、dynamic registration、relative patterns、hierarchical symbols、semantic tokens、code action resolve、experimental interactive 等。
- `DefaultOptions`（`internal/settings/default.go:26`）是默认值唯一来源，并被 `doc/generate` 反射生成 `doc/settings.md`、`doc/codelenses.md`、`doc/analyzers.md`、`internal/doc/api.json`。
- 每个字段 doc comment 用 Markdown 书写，生成器 `internal/doc/generate/generate.go:5` 通过 `go/packages` + go/ast + 反射生成文档。

vimls-go 现在的 `internal/server/config.go` 以裸 `map[string]any` 分支解析 `targetVersion / unresolvedSeverity / runtimepath`，每个函数重复做类型分支；文档则手写在 `docs/language-support.md`。这正是 gopls settings 模式能直接改进的部分，见 4.4。

### 2.5 cache：Session/View/Snapshot 与文件身份

这是 gopls 最核心也最复杂的层。动态视角是：LSP didOpen/didChange/didSave/didClose 和 didChangeWatchedFiles 都转成统一的 `file.Modification`，一次性提交给 `Session.DidModifyFiles`，由它决定哪些 View 需要更新，再生成新 Snapshot。

- `file.Modification`（`internal/file/modification.go:10`）显式记录 `Action{Open,Change,Close,Save,Create,Delete}`、`OnDisk`、`Version`、`Text`、`LanguageID`。
- `file.Handle`（`internal/file/file.go:38`）统一 overlay 和磁盘文件：`Identity{URI, SHA-256 Hash}`、`SameContentsOnDisk()`、`Version()`、`Content()`。
- `overlayFS`（`internal/cache/fs_overlay.go`）保存所有未保存编辑；`memoizedFS`（`internal/cache/fs_memoized.go`）按 inode+mtime 缓存磁盘读取，并故意不缓存“mtime 小于 2 秒”的文件，避免低精度文件系统漏改。
- `View`（`internal/cache/view.go:97`）代表一个逻辑 build；`viewDefinition`（:170）是决定 View 重建的不可变等价键。
- `Snapshot`（`internal/cache/snapshot.go:62`）是某次编辑后的文件世界：sequenceID、文件 map、package handle map、memoize promises、分析 keys；`Acquire/release` 引用计数让旧请求安全读完旧世界。
- `Session.DidModifyFiles`（`internal/cache/session.go:775`）先锁 viewMu 更新 overlays，再判断是否要重算 views（打开/关闭文件、go.mod/go.work 变化、build comment 变化），然后对每个 View `invalidateViewLocked`，返回“哪些 View 需要诊断”。
- `Snapshot.clone`（`internal/cache/snapshot.go:1507`）用 persistent treap 克隆文件/包 map，按修改类型精确决定 metadata 失效、依赖包失效、workspace 重载；`fileMap.clone`（`internal/cache/filemap.go:35`）两阶段处理删除和新增。

对 vimls-go 的映射：`workspace.Documents` 已经是简版 Session；`text.Snapshot` 是简版文档 Snapshot；`workspace.Index + ImportGraphSnapshot` 相当于 workspace 快照。缺的是“磁盘文件 handle + 内容哈希缓存”和“watch 事件增量失效”。

### 2.6 诊断管线：修改计数、两阶段、哈希去重

gopls 诊断管线是本次研究重点，完整链条如下：

1. **统一入口**：`didModifyFiles`（`internal/server/text_synchronization.go:215`）处理所有变更来源，用 `wg` 保证测试能等到“状态更新 + 诊断 goroutine”都完成；调 `session.DidModifyFiles` 后对每个变更 URI `mustPublishDiagnostics`。
2. **取消上一轮**：`needsDiagnosis`（:325）在 modificationMu 下取消 `cancelPrevDiagnostics`，生成无父取消的 `modCtx`，递增 `lastModificationID`，并记录每个 View 的最新待诊断号。
3. **后台诊断**：`diagnoseChangedViews`（`internal/server/diagnostics.go:122`）取所有待诊断 View，每个 View 拿当前 Snapshot 并发跑 `diagnoseSnapshot`；只有 `ctx.Err()==nil` 且 `viewsToDiagnose[v] <= modID` 才删除待办，避免“C1 与 C2 快速连续到达时 S2 完成而 S3 被漏诊”的竞态（`internal/server/server.go:154` 有完整推导）。
4. **两阶段**：`diagnoseSnapshot`（`internal/server/diagnostics.go:201`）先立即诊断 `changedURIs`（仅 parse/type-check 直接受影响的包），发布“非 final”结果；`time.After(diagnosticsDelay)`（默认 1s）后做全 workspace 的 parse/type-check/analysis，发布 final 结果。
5. **结果合并**：`diagnose` 按“go.work > go.mod > mod upgrade > vuln > template > package > analysis”递增精度分层，每层错误独立 event 记录；`golang.CombineDiagnostics`（`internal/golang/diagnostics.go:83`）把同 range+message 的分析诊断合并进 type error，只保留 suggested fixes/tags。
6. **版本/哈希去重发布**：`updateDiagnostics`（`internal/server/diagnostics.go:633`）只接受“更旧或同号 final”的 snapshot；`publishFileDiagnosticsLocked`（:788）计算所有诊断哈希，与 `publishedHash` 比较，未变化不重发；每个 `fileDiagnostics` 保留 `version`，不同文档版本的诊断 payload 不会互相覆盖。
7. **诊断数据结构**：`cache.Diagnostic`（`internal/cache/diagnostics.go`）是协议无关内部诊断，带 `URI/Range/Severity/Code/CodeHref/Source/Message/Tags/Related/SuggestedFixes/BundledFixes`；`Hash()` 对除 URI 外的全部字段做 SHA-256，`BundledFixes` 可以 JSON 编进 LSP `Diagnostic.data`，code action 阶段无需重新分析。

vimls-go 目前：didChange 立即 `startAnalysis`；`analyzeDocument` 一次做完 syntax + analysis + import diagnostics；`publishSyntax` 只在“从未发布过且 0 诊断”时跳过，其他每次变更都重发；watch 事件一次触发全量 rebuild。4.1/4.2 给出直接落地方案。

### 2.7 workspace 配置与文件监听

gopls 的 workspace 模型在 v0.15 后变成“zero config”：`doc/workspace.md` 描述按打开文件、go.mod/go.work/GOPATH、GOOS/GOARCH build tag 自动推导 View；`directoryFilters` 使用 `+/-` 和 `**` 规则；`workspaceFiles` 允许自定义 build 系统声明逻辑工作区文件。

文件监听是双层：

- **客户端优先**：`updateWatchedDirectories`（`internal/server/general.go:482`）根据 `DynamicWatchedFilesSupported / RelativePatternsSupported` 决定注册普通 glob 还是 relative pattern；注册新 watchers 后才取消旧 watchers，避免“无监听窗口”。
- **服务端 fallback**：`updateServerSideWatcher`（:528）根据 `fileWatcher` 设置（`off|fsnotify|poll`）创建 `internal/filewatcher`。fsnotify 实现用 500ms debounce 批量 flush（`fsnotify_watcher.go:48`），处理目录新增的递归 create 合成、目录删除、已知目录映射；poll 实现用自适应回退（被 `Poke` 后回到快速扫描，空闲逐步放慢），并把目录状态持久化到 filecache 以跨会话 baseline（`poll_watcher.go:31`）。
- **批量操作特判**：`handleModuleChanges`（`text_synchronization.go:294`）若 watched-file 批次混有非 module 文件，则按 bulk operation（如 git branch switch）跳过逐文件依赖检查；注释明确这是为了兼容 Vim/Neovim 的 atomic save（Delete + Create 成对出现）。

vimls-go 已有客户端动态注册和注册后“再扫一次”关闭窗口，但 `DidChangeWatchedFiles` 对任意事件都立即 `scheduleWorkspaceRebuild()`，没有过滤、合并、阈值和 bulk 特判。4.2 是 P0。

### 2.8 分析框架、内存缓存与持久索引

- `Snapshot.Analyze`（`internal/cache/analysis.go:113`）把启用的 analyzer 组成 DAG：根包跑完整集合，依赖包只跑产生 facts 的子集；并行后序遍历；每个 analysis node 有加密 recipe key，先查 machine-global filecache，命中直接加载序列化 diagnostics/facts，跨进程复用。
- `Snapshot.Symbols`（`internal/cache/symbols.go:31`）以“文件 identities + package path”为 key 从 filecache 读 symbol 索引，miss 才 parse/symbolize；写入是异步的。
- `filecache`（`internal/filecache/filecache.go:7`）是机器级持久化 blob cache：`(kind, SHA-256 recipe key) -> []byte`，CAS 文件 + index 文件，带 100MB 内存 LRU 前置、I/O 并发限流、空间预算（`SetBudget` :338）、损坏内容哈希校验和 ErrNoCache 明确失败语义。
- `parseCache`（`internal/cache/parse_cache.go:97`）是 1 分钟 TTL + 最少 100 文件的 LRU；用文件内容哈希作 key，并为 `token.Pos` 空间预分配、重复解析安全兜底。
- `typerefs` 等包把 type-check 结果序列化成可压缩索引，支撑大 workspace 低内存查询。

vimls-go 不需要 go/types 这一层，但“**发现/读取/解析/索引**”的缓存分层完全适用：先做磁盘文件内容哈希缓存和解析缓存（4.3），持久化 filecache 留到 1.0 后（4.13）。

### 2.9 progress、debug、telemetry

- `progress.Tracker`（`internal/progress/progress.go:34`）根据 client 是否支持 WorkDoneProgress 自动选择 `window/workDoneProgress/create + $/progress` 或降级 `window/showMessage`；`WorkDone.Cancel` 支持用户取消长任务；所有 progress 消息都使用 `context.WithoutCancel`，避免用户取消请求导致 UI 卡在 begin/end 不配对。
- `internal/debug` 提供 HTTP debug 页面：内存/包/文件统计、RPC inbound/outbound 方法级直方图（`debug/rpc.go`）、trace 和 pprof。
- `internal/telemetry/latency.go:84` 用 10ms/50ms/100ms/200ms/500ms/1s/5s 桶记录每个 LSP 操作的延迟，取消样本不计数；`server/counters.go` 记录 completion 使用率、rename/refactor 尝试等产品行为。
- `settings` 的 `memoryLimit` 直接调 `runtime/debug.SetMemoryLimit`。

vimls-go 当前 stderr 日志已经满足“stdout 纯净”，但没有长任务可见性，也没有结构化指标。4.6/4.11 建议分两步补。

### 2.10 测试基础设施

gopls 有三层测试：

1. **marker 测试**：`internal/test/marker/doc.go` 定义 txtar 场景，`//@` 注释里的 Go-like 调用在测试中执行真实 LSP 操作。支持 `@loc/@diag/@def/@refs/@rename/@hover/@symbol/@token/@complete/...`，支持 `flags`、`settings.json`、`capabilities.json`、`env`、`@golden` 文件。marker 不是字符串匹配，而是由位置/正则转换器生成强类型参数。
2. **integration 框架**：`internal/test/integration/doc.go` 提供 `Run(t, files, func(t, env))` 脚本模型；`fake.Editor` 模拟真实编辑器（open/edit/save/close、LSP client hooks），`Awaiter` 在收到 publishDiagnostics/showMessage/registerCapability 等通知时原子检查 expectation。`runner.go` 的 `Mode` 把同一测试在 `default / forwarded / separate_process` 三种执行模式下各跑一遍。
3. **subprocess + daemon 测试**：验证真实 stdio/转发/daemon 生命周期。

vimls-go 已有强 parser 测试和官方 corpus，但 LSP 行为测试只靠手写 JSON 顺序读消息；未来 code action/completion/rename 的异步交互会非常难表达。4.5 建议做一个“缩小版 marker + fake client”，而不是照搬 gopls 整个 fake editor。

### 2.11 解析修复、模糊匹配、补全预算

- `parsego.Parse`（`internal/cache/parsego/parse.go:50`）在 go/parser 出错后做两类修复：AST 修复（BadStmt/BadExpr/phantom selector/空 switch）和源文本修复（补 `{}`、插入 `_`），最多 10 轮，并记录 `fixedSrc/fixedAST`。这不是替代错误恢复，而是为了在用户正在输入时仍能给 completion/type-check 一个可用的近似 AST。
- `fuzzy`（`internal/fuzzy/matcher.go:74`）实现带角色分类和 DP 的模糊评分，限制输入 127 字节、pattern 63 字节，返回 `Score` 和 `MatchedRanges`；`symbolMatcher` 有 fuzzy/fastFuzzy/case-insensitive/case-sensitive 多档。
- completion 有 100ms `completionBudget`（`settings` 默认），先用近似结果填满预算，再 deep search（`internal/golang/completion/completion.go:657-710`）。

vimls-go 的 parser 恢复已经很显式且更强；gopls 的“修复 AST + 标记 synthetic position”思想只在未来语义分析因缺 block 结束符而不可用时再引入。模糊匹配和预算可在 M6 直接借用（4.8）。

### 2.12 LSP 之外的新表面

本快照还包含三类“未来”表面，说明 gopls 在标准 LSP 之外如何复用同一 session：

- **MCP**：`doc/features/mcp.md`、`internal/mcp`；attached mode 与 LSP 共享内存，detached mode 跑 headless LSP。
- **interactive refactoring**：`doc/design/integrating-interactive-refactoring.md`；`workspace/executeCommand` 支持 `command/resolve` 表单问答。
- **web server**：server 内按需启动本地 HTTP，提供包文档页面（`internal/server/server.go` web 部分）。

对 vimls-go 来说这些都是 P3，1.0 前只做趋势观察。

---

## 3. vimls-go 现状与差距矩阵

“已具备”表示不需要从 gopls 复制，只做小范围补强。

| 能力 | vimls-go 现状 | gopls 做法 | 差距 / 建议 |
| --- | --- | --- | --- |
| framing | 8KiB header / 16MiB body，帧大小可测 | header stream + jsonrpc2 | 已更强；保留 |
| 生命周期 | state 机、cancellation map、128 并发上限 | serverState + lsprpc | 已足够；M7 可加 panic/bug 上报 |
| 文档快照 | `text.Snapshot` + `Documents.IsCurrent` | file.Handle + Snapshot | 已正确；可加内容哈希 |
| 位置转换 | UTF-8/16/32，lazy? 每快照全量 indexLines | Mapper lazy line index | 保留；需要 benchmark 再决定 lazy |
| 后台分析 | pending/running map + 4 workers | 修改计数 + 取消 + 两阶段 | 缺去抖、全量延迟、诊断去重 |
| 诊断发布 | 每次重发，仅首次空结果跳过 | version + publishedHash | P0：哈希去重 + version 记录 |
| 诊断模型 | `syntax.Diagnostic{Code,Message,Span}` + 31 个 vimls 定义 | 富 Diagnostic + generated analyzer catalog | P1：补 Source/CodeDescription/Tags/Data，生成 E 码索引 |
| workspace rebuild | 任何 watch 事件全量发现/读/解析/重建 index+graph | 事件过滤/合并/阈值 + memoizedFS + parseCache + 增量失效 | P0/P1：先事件治理，再文件/解析缓存 |
| 磁盘读取 | 每次 rebuild 重新 `os.ReadFile` | inode+mtime memoizedFS | P1：文件身份缓存 |
| 索引一致性 | 可变 `Index` + 不可变 `ImportGraphSnapshot` | Snapshot + persistent maps | P2：`IndexSnapshot` 对齐 |
| 配置 | 裸 JSON 分支解析 | typed Options + Set + 生成文档 | P1 |
| 长任务可见性 | warning only | WorkDone/ShowMessage + critical status | P1 |
| workspace 符号排序 | 子序列 rank | fuzzy 评分 + style/scope | P2 |
| completion | 表驱动 + 2000 cap | budget + fuzzy + deep search | P2 |
| code action | 仅 missing-end quickfix | kind 层级 + diagnostic.data lazy fixes + executeCommand | P2 |
| server watch | 客户端 dynamic registration | 客户端优先 + fsnotify/poll fallback | P2 |
| LSP 端到端测试 | 1 个手写 subprocess 测试 | marker/txtar + fake editor + 3 modes | P1 |
| 调试/指标 | stderr | debug HTTP、RPC 直方图、telemetry | P2 |
| 协议类型 | `go.lsp.dev/protocol` | 生成 3.18.2 codec | P3，出现第二处 wire 摩擦再评估 |
| 持久缓存/daemon | 无 | filecache、daemon、MCP | P3 |

---

## 4. 可借鉴项与落地方案

每条按以下格式展开：

- **参考**：gopls 的对应实现和证据。
- **现状**：vimls-go 对应代码。
- **建议**：裁剪后的设计。
- **成本/风险**。
- **阶段与验收**。

### 4.1 P0：诊断调度改为“立即语法路径 + 延迟全量路径 + 哈希去重发布”

**参考**

`internal/server/text_synchronization.go` 的 `didModifyFiles -> needsDiagnosis -> diagnoseChangedViews`；`internal/server/diagnostics.go` 的 `diagnoseSnapshot` 两阶段（:201）和 `publishFileDiagnosticsLocked` 的 `publishedHash`（:788）；`internal/server/server.go:154` 对快速连续变更竞态的完整推导。

**现状**

`internal/server/server.go`：

- `DidChange`（:481）更新快照后立即 `startAnalysis(documentURI)`。
- `startAnalysis`（:747）只有 pending/running map，没有时间维度合并。
- `analyzeDocument`（:838）一次完成 syntax、same-file analysis、import diagnostics 和发布。
- `publishSyntax`（:922）除“首次空诊断不发布”外，每次分析完成都重发相同诊断。
- 已有时序保护：`BeginAnalysis/IsCurrent`、`workspaceGraphView.Revision()` 检查。

**建议**

不要动 `Documents` 的立即文本更新，只改“何时在后台跑分析和何时发布”：

1. 把 `analysisPending map[string]struct{}` 升级为 `map[string]*scheduledAnalysis`，每个 URI 记录 `generation uint64`、`timer *time.Timer`。`didOpen` 排 0ms；`didChange/didSave` 排 `diagnosticsDelay`（默认先取 150ms，benchmark 后写进 setting，不要一开始照抄 gopls 的 1s）；`didChangeConfiguration` 和 graph 变化排 0ms。
2. `scheduleAnalysis` 每次递增 generation 并 `Stop/Reset` timer；timer 到点后只有 generation 最新才把 URI 放回 pending map。worker 结构不变。
3. 若 M4 跨文件语义分析变贵，再把 `analyzeDocument` 拆成：
   - fast：`syntax.Parse` + `analysis.Analyze`（same-file）立即发布；
   - slow：`workspaceImportDiagnostics` 及未来跨文件类型/依赖诊断，在 `diagnosticsDelay` 后再跑并发布 final。
   - 学习 gopls：fast 结果只在“没有更新 snapshot 结果”时写入；final 结果可以覆盖同 snapshot 的 fast 结果；取消的 pass 不发布，由下一次 pass 保证最终一致。
4. 发布侧增加 per-URI `publishedDiagnostics`：`{version int32, hash file.Hash}`。哈希在 LSP 边界算，覆盖 `range/severity/code/source/message/tags/related/data`，不覆盖 version/URI。`publishSyntax` 只在 version 或 hash 变化时 `PublishDiagnostics`；关闭文档仍发一次空数组并删除记录。保留“首次空结果不发布”语义。
5. 继续保留 `IsCurrent` 和 graph revision 检查；去抖只能减少计算，不能替代 stale 防护。

**成本/风险**

改动集中在 `internal/server`，约 150-250 行加测试；风险是 timer/generation 竞态。必须用 `-race` 测试“0ms、重复 didChange、关闭后 timer 触发、configuration change、graph revision 变化”等交错。

**阶段与验收**

- M4 切片内完成；`go test -race ./...`。
- 测试断言：连续 20 次 didChange 只启动不超过 N 次后台分析；相同结果不重复 `PublishDiagnostics`；最后一次结果永远来自最新 snapshot；旧 generation 永不发布。

### 4.2 P0：watched-file 事件过滤、合并、阈值与 atomic-save 识别

**参考**

`internal/server/text_synchronization.go:185` 的 `DidChangeWatchedFiles -> file.Modification{OnDisk:true}`；:294 的 bulk operation 特判；`internal/filewatcher/fsnotify_watcher.go` 的 500ms debounce 和目录 create 合成；`internal/server/general.go:596` 的 watcher 注册顺序。

**现状**

`internal/server/workspace.go:738` 的 `DidChangeWatchedFiles` 只有 `if len(params.Changes) > 0 { scheduleWorkspaceRebuild() }`。客户端已经按注册的 `**/*.vim` 监听，但：
- 没有防御性 URI/扩展名校验；
- 没有合并窗口，编辑器/文件系统经常连续给 Create+Change，甚至 Delete+Create；
- 没有阈值，git branch switch 产生的成百上千事件会排队触发同等数量的全量 rebuild（现有 revision 机制只保证最后结果正确，不减少重复工作）。

**建议**

在 `internal/server/workspace.go` 增加 `workspaceEventBatcher`：

1. `DidChangeWatchedFiles` 先把 events 转成 canonical path 集合：
   - 只接受 `workspaceURIPath` 成功、位于 workspace/runtime roots 内、`isVimFile` 规则命中的事件；
   - 不匹配的 events 丢弃（watcher 配置变化时重新扫描仍能兜底）。
2. 同一路径在一批内按 `Create+Change -> Change`、`Delete+Create -> Change/Save` 合并；这是 Vim/Neovim atomic save 的常见形态。
3. 事件进入 300ms 窗口（可配置，但默认固定即可，避免过早暴露 setting）。窗口关闭时：
   - 变化路径数 <= 阈值（建议 256）走增量 rebuild（4.3 落地后）；
   - 超过阈值直接标 full rebuild，并按 bulk 处理，跳过逐路径依赖检查；
   - 任何一批都只有一个 background rebuild goroutine。
4. 保留 `workspaceRevision++` 作为 generation；timer 触发后读取最新 roots/revision，构建完成后用现有 `workspaceSnapshotsCurrent` 和 revision 检查决定是否提交。
5. 保留“注册变更后扫一次”的现有逻辑。

**成本/风险**

100-200 行。合并 Delete+Create 可能隐藏真实“删除后立即新建同名文件”的语义差异；对索引/图来说两者最终状态一致，风险可控。需要测试 burst、atomic save、窗口内配置变化。

**阶段与验收**

- M5 切片内完成。
- 测试：1000 个无关事件不触发 rebuild；burst 500 个 Vim 文件只重建一次；Delete+Create 只改变最终索引一次；旧 revision 结果被丢弃。

### 4.3 P1：磁盘文件身份缓存 + 解析缓存 + 增量 reindex

**参考**

`internal/file/file.go` 的 Handle/Identity；`internal/cache/fs_memoized.go` 的 inode+mtime 缓存和 2 秒 mtime 保守策略；`internal/cache/parse_cache.go` 的 hash key + TTL/LRU；`internal/cache/filemap.go` 的两阶段增删；`internal/cache/session.go` 的 `DidModifyFiles` 只让受影响 View 诊断；`internal/cache/symbols.go` 用 file identities 作 cache key。

**现状**

`internal/server/workspace.go:252` 的 `buildWorkspaceIndex` 每次：
1. 重新 walk 所有 roots；
2. 对每个候选重新 `os.ReadFile`；
3. 重新 `syntax.Parse`；
4. 重建 `workspace.Index` 和 `workspace.ImportGraph`。

优点是简单、绝对一致；缺点是 watch 一个文件也全量重算。`workspace.Index.Replace/Remove` 和 `ImportGraph.Replace/Remove` 已经支持 per-file 原子更新，具备增量基础。

**建议分两步**

第一步（P1a，M5）：**只优化计算，不改变重建语义**。

- 在 `internal/workspace` 增加 `FileCache`：
  - key：canonical path；
  - entry：`size`、`modTime`、内容 SHA-256、`source string`、`*syntax.File`（约定为只读，诊断 slice 不共享）；
  - `Get(path) (entry, unchanged bool)`：先 `os.Stat`，size+mtime 没变且文件 mtime 距现在 > 2s 才认为不变；否则读内容，hash 变化才替换；
  - 容量用现有 `maxWorkspaceFiles` 约束，淘汰未使用路径。
- `buildWorkspaceIndex` 中：只有新文件/变化文件调用 `os.ReadFile` 和 `syntax.Parse`；未变化文件直接复用 source、`CollectSymbolFacts` 和 import facts。Index/Graph 仍重建，但 CPU 从“全量 parse”降为“变化文件 parse + 全量 facts 重放”。
- `FileCache` 绝不缓存错误读取；权限错误每次重试或按现有发现逻辑跳过。

第二步（P1b，M6，有 benchmark 证据后再做）：**真正增量提交**。

- 比较上一轮 path 集合，得到 added/changed/removed；
- 对 added/changed：解析并 `Index.Replace`、`Graph.Replace`；
- 对 removed：`Index.Remove`、`Graph.Remove`；
- 新增文件可能让此前 missing 的 import 变得可解析，删除文件会让 inbound edge 变 missing；所以提交后必须对受影响 importers 和 reverse dependents 重新跑 `collectWorkspaceImportFacts`，并复用 `startWorkspaceDependents` 重析开放 importer。
- 这个重解析集合要计算到稳定（最多一轮候选补解析），或按 gopls 的做法只让“metadata 被失效的包 + reverse dependents”进入下一轮；vimls 的 import graph 已有 `ReverseDependents`，天然适合。
- 当 changed 集合过大（4.2 阈值触发 bulk）时仍回退全量重建，保证简单路径覆盖坏情况。

**成本/风险**

P1a 低风险，约 200-300 行 + 并发测试；P1b 是 M5/M6 最需要小心的改动。风险：`syntax.File` 目前不是显式 immutable；任何分析器写共享 AST 会污染缓存。必须加约定测试：两个分析任务拿到同一 cached File 后各自追加 Diagnostics 不互相影响；现有 `prepareSyntax` 已经浅拷贝并复制 diagnostics slice，应继续保持并补测试。

**阶段与验收**

- P1a：`make check` 全绿；benchmark 证明未变化文件的 rebuild 不再调用 `syntax.Parse`（可用计数器 hook）；race 下 rebuild 与查询并发安全。
- P1b：增量 rebuild 结果与全量 rebuild 完全一致（同一 fixture 双路径对比）；新增/删除/修改/atomic save 四种变化都覆盖；graph 的 missing/dynamic 语义不回归。

### 4.4 P1：把配置改造成 typed Options + 单一默认值 + 生成 settings 文档

**参考**

`internal/settings/settings.go` 的 `Options`、`Set`、`ForClientCapabilities`、`SoftError`；`internal/settings/default.go`；`internal/doc/generate/generate.go` 从 struct doc comment 生成 Markdown。

**现状**

`internal/server/config.go` 的三个解析函数重复处理 `map[string]any` / `[]byte` 类型分支；`DefaultTargetVersion` 等常量散落；`Initialize` 手工把三个 warning 拼成 `pendingWarning`；`applyWorkspaceConfiguration` 再用 `targetVersionFromSettings` 和 `unresolvedSeverityFromSettings`。没有 `docs/settings.md`。

**建议**

保持“先改现有包，不新建空包”的 AGENTS 原则：

1. 在 `internal/server/config.go` 旁新增 `options.go`（同包）：
   ```go
   type Options struct {
       TargetVersion       TargetVersion
       TargetVersionSet    bool // initializationOptions 显式覆盖，禁止 workspace config 再覆盖
       UnresolvedSeverity  syntax.DiagnosticSeverity
       RuntimePaths        []string
       RuntimePathSet      bool
       DiagnosticsDelay    time.Duration
       DiagnosticsTrigger  DiagnosticsTrigger // edit|save
       FileWatcher         FileWatcherMode     // off|poll，fsnotify 视 4.9
       MaxWorkspaceFiles   int
       MaxIndexBytes       int
   }
   func DefaultOptions() Options
   func (o *Options) Set(raw any) (warnings []string, errors []string)
   func (o *Options) ForClientCapabilities(caps *protocol.ClientCapabilities)
   ```
2. `Set` 一次遍历 JSON object，复用 gopls 的体验：
   - dotted key 取最后一段（兼容 VS Code 式层级）；
   - 每个字段独立校验，失败时保留旧值并累积 warning/error；
   - 弃用别名返回 `SoftError` 语义；
   - 不认识的字段 warning（当前静默忽略），便于用户发现拼写错误。
3. `Initialize` 和 `DidChangeConfiguration` 只调用 `options.Set`；`ForClientCapabilities` 记录 `workspace/configuration`、watch dynamic/relative、rename prepare 等能力，把 `Initialize` 里一串布尔变量收敛进 options。
4. 文档生成：加 `tools/gensettings`（纯 Go，模式同 `tools/gencommands`），用 `go/ast` 读取 `Options` 字段 doc 注释 + 反射 `DefaultOptions()`，生成 `docs/settings.md` 的“User settings”区块；`docs/language-support.md` 保留行为契约，链接到 settings 文档。`make check` 中校验生成文件无漂移（gopls 的 `doc/generate` 同样做 write/diff 两态）。

**成本/风险**

主要是重构，行为必须逐项对照现有测试。风险是把 valid/invalid 语义改丢；因此先迁移 `targetVersion/unresolvedSeverity/runtimepath` 三个现有字段并跑现有 config tests，再添加新字段。

**阶段与验收**

- M5/M6 间完成。
- 现有 `config_test.go` 全绿，并新增：dotted key、未知 key warning、部分合法/部分非法同对象、deprecated alias、workspace configuration 优先级、生成文档无漂移。

### 4.5 P1：最小 marker/txtar LSP 测试框架 + fake client 等待器

**参考**

`internal/test/marker/doc.go` 的 marker 类型和 txtar 约定；`internal/test/integration/doc.go` 的 `Run/Env/Awaiter`；`internal/test/integration/fake` 的 ClientHooks；`runner.go` 的三种执行模式。

**现状**

`test/integration/lsp_subprocess_test.go`（466 行）手写 JSON 帧、按固定顺序读响应，只能断言字符串包含关系。server 包内已有不错的单元测试，但跨通知/请求的异步行为没有通用 harness。

**建议**

不要移植 gopls 的 fake editor 全部能力，做一个 vimls 专用缩小版：

1. 新建 `internal/test/lspmarker`（或 `test/lspmarker`，仅测试代码）：
   - 用 `net.Pipe()` 或 `io.Pipe()` 加现有 `internal/jsonrpc` 的 Reader/Writer 跑 in-process `server.New`；
   - fake client 实现 `go.lsp.dev/protocol.Client`，把 diagnostics、logMessage、registerCapability、progress 写进带锁状态；
   - `Awaiter` 只保留 `AfterChange` 和“条件在任一通知后原子检查”两个原语，配合超时。
2. marker 数据用 txtar，放在 `testdata/markers/`：
   - `-- settings.json --`、`-- capabilities.json --` 可选；
   - Vim 文件里的标记用独立注释行，避免影响语法：`" vimls@loc(name, "pattern")`；位置参数用带捕获组的正则（gopls 也是 location 可用正则），动作 marker 放文件底部。
   - 第一批 marker：`loc`、`diag`、`def`、`decl`、`refs`、`hover`、`symbol(golden)`、`rename(golden)`、`token(golden)`、`workspaceSymbol(golden)`。
3. 保留现有 subprocess 测试作为真实 stdio/二进制 smoke；marker harness 只用于可维护的功能矩阵。M7 再考虑是否把 runner 扩展成 in-process/subprocess 双模式。

**成本/风险**

约 400-600 行测试基础设施。风险是 marker 解析器本身出错；必须从 parser/analysis 无关的最小语法开始，golden 失败时必须输出可读 diff。

**阶段与验收**

- M5/M6 开始，先把现有 466 行测试中的 document symbol/definition/rename 场景迁移为 marker，保证行为等价。
- 验收：一个 marker 文件可表达多请求+异步诊断；失败输出直接显示期望/实际；`go test -race ./...` 通过。

### 4.6 P1：长任务 progress 与关键错误状态

**参考**

`internal/progress/progress.go` 的 Tracker/WorkDone 双轨；`internal/server/general.go` 的 `updateCriticalErrorStatus` 用 progress 报告 workspace load 失败；`server/diagnostics.go` 的分析进度 `AnalysisProgressTitle`。

**现状**

vimls-go 只有 `sendWarning`（`window/logMessage`），workspace 构建、runtimepath 变更、大量文件索引对用户完全不可见；`workspace` warning 在构建完成后才一次发出。

**建议**

1. 在 server 增加极简 `progress` 字段（可放 `internal/server` 或小包，取决于是否被 cmd 复用；先放 server）：
   - `Initialize` 从 `capabilities.Window.WorkDoneProgress` 设置 `supportsWorkDoneProgress`；
   - `Start(title, message, cancel)` 在支持时 `workDoneProgress/create` + `begin/report/end`，不支持时只发一次 `showMessage` 或完全静默；
   - 取消处理注册 `workDoneProgress/cancel`，转发到任务 context。
2. workspace worker 在发现/读取/解析阶段报 `vimls: indexing workspace`；分析全量阶段报 `vimls: analyzing`；消息不要包含环境变量/绝对隐私路径。
3. 初始化失败/索引失败用 progress error 状态（gopls 的 criticalErrorStatus 模式），并保留现有 `sendWarning` 降级。
4. 所有 progress 消息用 `context.WithoutCancel`，保证请求取消不会让 begin/end 失配。

**成本/风险**

需要确认 `go.lsp.dev/protocol.Client` 对 workDone 方法的表达能力；若没有，先只做 showMessage 降级并在 4.13 协议生成评估里记录。风险是给老客户端发不支持的通知；必须按 capabilities gate。

**阶段与验收**

- M5/M6。测试 fake client 断言：支持 workDone 的客户端收到 begin/end 配对；不支持时收到合理降级或零通知；workspace 大索引期间进度单调；取消后 worker 停止。

### 4.7 P1：集中式诊断目录与生成 Vim E 码索引

**参考**

gopls 诊断的 `Source/CodeDescription/Tags/Data` 富模型（`internal/cache/diagnostics.go`）；`settings.AllAnalyzers` 生成 `doc/analyzers.md`；`internal/golang/diagnostics.go` 的 `CombineDiagnostics`。

**现状**

vimls 已有 `internal/syntax/vimls_diagnostics.go` 的 31 个 vimls 定义；`vim/E...` 代码直接由 parser/analyzer 发出，severity 在 `internal/server/server.go` 的 `protocolDiagnosticSeverity` 中硬编码 4 个 E 码；官方 compile cases 按 E 码分文件提供证据，但“哪些 E 码已支持、默认消息、severity、建议修复”没有运行时目录。

**建议**

1. `tools/generrors` 从 pinned Vim `v9.2.1015` 的 `src/errors.h` 生成 `internal/vimdata/errors_generated.go`：
   - 字段 `Code, Name, DefaultMessage, Supported bool, Kind, DefaultSeverity`；
   - `Supported` 只对已迁移官方 compile case / `docs/diagnostics.md` 证据的 E 码为 true；
   - 生成器记录 tag/commit，逻辑与 `tools/gencommands` 一致。
2. `syntax.Diagnostic` 只增加协议无关字段（span 仍是 byte）：`Tags`、`Related`、`Data`；`DiagnosticDefinition` 增加 `Source/Help`。`LookupDiagnostic(code)` 同时查 vimls 定义和生成 E 码表。
3. 删除 `protocolDiagnosticSeverity` 中的硬编码 E 码，改为查表；未知 E 码在测试中失败（panic on bug 原则），生产 fallback Error。
4. 后续 code action 可以给诊断绑定 `Data`，避免 code action 阶段重新扫描整个文件；quickfix 用 `diagnostic.Code` 精确匹配，保留现有 `clientHasDiagnostic` 的 range 检查。

**成本/风险**

生成器约 150-250 行；需要验证 errors.h 的解析不会把同码多条消息合并错。风险很低，且让 M4 的“支持哪些诊断”从测试资产变为运行时可查目录。

**阶段与验收**

- M4 内完成。
- `go generate ./...` 可重现；生成文件只读；每个 Supported E 码能指到至少一个 official case 或 doc evidence；未知 E 码测试失败；`docs/diagnostics.md` 增加“生成索引”说明。

### 4.8 P2：workspace symbol 模糊评分与 completion 预算

**参考**

`internal/fuzzy/matcher.go` 和 `symbol_matcher`；`settings` 的 `symbolMatcher/symbolStyle/symbolScope`；`internal/golang/workspace_symbol.go` 的评分裁剪；`completion.go` 的 100ms budget。

**现状**

`workspace.Index.Search`（`internal/workspace/index.go:263`）目前是全量 facts 的子序列 rank；completion 直接遍历可见声明/命令表并去重截断，没有评分和预算。20,000 文件上限下 workspace symbol 的朴素扫描可能成为 P95 延迟问题。

**建议**

1. 在 `internal/workspace` 或新 `internal/fuzzy`（等真实使用出现）实现一个约 150 行的评分器：大小写、segment 边界、连续匹配加分、pattern/candidate 长度归一化、返回匹配区间。不要复制 gopls 源文件；如果要移植，先从上游 x/tools 取完整 LICENSE 并保留版权声明。
2. `Index.Search` 改为 score + 稳定 tie-breaker；空查询保留现有语义；`limit` 仍截断。可先用 benchmark 对比现有 rank。
3. completion 加 `completionBudget`（默认 100ms），在候选收集循环里周期性检查 deadline；当前表驱动基本不会超时，但跨 workspace import-member 场景可能。
4. 设置只加一个 `symbolMatcher`（后续再考虑 style/scope），默认 fuzzy。

**成本/风险**

中。评分算法容易引入浮点/边界 bug；必须有 property test（score=1 iff exact，score=0 iff no match，排序稳定）。依赖无新增。

**阶段与验收**

- M6，先 benchmark 后落地。验收：workspace symbol 在 20k 文件 fixture 的 P95 不超过设定预算（记录具体机器）；精确匹配仍排第一；补全超预算返回已收集的确定性子集且不 panic。

### 4.9 P2：服务端 file watcher fallback（先 poll，后 fsnotify）

**参考**

`internal/filewatcher/filewatcher.go` 的 `Watcher` 接口；`fsnotify_watcher.go` 的 500ms 批量、目录合成、重试；`poll_watcher.go` 的自适应回退和跨会话状态持久化；`settings` 的 `fileWatcher` 默认 `off`。

**现状**

vimls-go 遵循 AGENTS“客户端拥有文件监听”，在客户端支持 dynamic registration 时注册 watcher；不支持时只靠 `DidChangeWatchedFiles` 永远不重建。对某些 Vim LSP 客户端或 CLI 使用场景，这会导致磁盘文件永不刷新。

**建议**

保持默认 `off`，只做显式 opt-in：

1. `vimls.fileWatcher = "poll"` 时启动服务端 poll watcher：
   - 复用 `workspace.DiscoverFiles` 的过滤规则（VCS/node_modules/symlink 策略），扫描 roots；
   - 比较 `path -> {size, mtime}`（必要时内容 hash），只在有变化时把 `file.Modification` 转成现有 `DidChangeWatchedFiles` 路径；
   - 自适应间隔：活动后 1s，空闲增长到 30s，用户 didChange/didSave 时 `Poke` 立即扫；
   - 不持久化跨会话状态（1.0 前不做 filecache），启动第一轮只建立 baseline，不产生事件。
2. `"fsnotify"` 仅作为后续选项；若引入 `github.com/fsnotify/fsnotify`，必须按 AGENTS 的依赖门槛单独论证。优先 poll，零依赖。
3. 客户端 watcher 注册成功时优先使用客户端；poll 只处理“客户端不可用”的缺口。

**成本/风险**

poll 是 CPU/磁盘换正确性，必须限速且默认关闭。风险是扫描巨大 runtimepath 拖慢机器；用 4.2 的阈值和退避控制。

**阶段与验收**

- M7 前按真实客户端缺口决定；不进入 1.0 默认配置。
- 验收：关闭客户端 watcher capability 后，外部修改 `.vim` 文件在可配置窗口内触发索引更新；空闲 CPU 有上限；连续 didChange 会重置为快速轮询。

### 4.10 P2：code action kind 层级与诊断内嵌修复

**参考**

`internal/server/code_action.go:35` 的 kind 层级匹配（`refactor.rewrite.foo -> refactor.rewrite -> refactor -> ""`）；`internal/cache/diagnostics.go` 的 `SuggestedFix`、`BundledFixes`、`BundledLazyFixes`；`internal/protocol/command/interface.go` 的生成命令接口。

**现状**

`internal/server/semantic_actions.go` 只有一个 missing-end quickfix；`allowsQuickFix` 手工比较 quickfix 前缀；诊断没有 fixes；无 executeCommand。

**建议**

M6 内只做两点，不建 command registry：

1. 把 `allowsQuickFix` 替换为 gopls 式层级匹配函数，参数为 `Only []CodeActionKind` 和 `requested Kind`，为 future source/refactor 预留；自动触发 `Only=[]` 仍按 LSP 1.0 语义只回 quickfix。
2. `syntax.Diagnostic` 增加 `Data`（4.7），把“缺失 end”这类能从诊断确定计算的修复绑定到诊断；`CodeAction` 优先从 `params.Context.Diagnostics` 的 `Data` 生成 action，miss 时才回退当前扫描。不要引入 lazy command/executeCommand 直到出现第二种复杂修复。

**成本/风险**

低。注意 `go.lsp.dev/protocol.Diagnostic.Data` 的表达能力；若字段不支持则延后到 4.13。

**阶段与验收**

- M6。测试：层级 kind 请求返回/不返回；重复 code action 请求不再重复扫描（可用计数器）；未知诊断数据被安全忽略。

### 4.11 P2：debug/指标最小闭环（不引入 telemetry 上传）

**参考**

`internal/debug/serve.go`、`debug/metrics.go` 的 RPC 直方图；`telemetry/latency.go` 的延迟桶；`server/general.go` 的 `memoryLimit`；`doc/troubleshooting.md` 的日志/pprof 工作流。

**现状**

vimls-go 只有 `--listen`、`--version` 和 stderr 日志。没有 profile、RPC 统计、内存统计。

**建议**

1. `cmd/vimls` 增加 opt-in flags：`-debug=127.0.0.1:0`、`-logfile`、`-rpc.trace`。实现从 gopls 裁剪：`net/http/pprof` + 一个只读 JSON/HTML 页面，暴露 `documents.Len()`、workspace file count/bytes、pending analysis、published diagnostics、goroutine/pprof。
2. 不引入 `golang.org/x/telemetry`；用进程内 `expvar` 或普通 atomic map 记录每方法调用数、错误数、延迟桶（10/50/100/200/500ms）。这些只用于开发者和 issue 报告，不自动上传。
3. `memoryLimit` 设置可做可不做；若做，直接 `runtime/debug.SetMemoryLimit`，并在 settings 文档标注 experimental。
4. `rpc.trace` 写 stderr/logfile，stdout 保持 LSP 纯净。

**成本/风险**

中。debug HTTP 是攻击面，必须只绑 loopback 且默认关闭；M7 前不能默认开启。日志不得包含环境变量/用户路径。

**阶段与验收**

- M7。`-debug` 启动后页面可访问且 stdout 仍只含 LSP；有文档说明如何抓 profile；无任何自动上传代码。

### 4.12 P2：`IndexSnapshot` 与 clone-on-write 索引视图

**参考**

`metadata.Graph`（`internal/cache/metadata/graph.go`）的不可变图 + `Update` 返回新图；`Snapshot.clone` 的 persistent maps；`internal/cache/filemap.go` 的两阶段变更；gopls `updateDiagnostics` 对多 view 的合并逻辑。

**现状**

`workspace.ImportGraphSnapshot` 已经是不可变 revision 视图；`workspace.Index` 仍是“锁内可变单对象”，查询取 facts 后释放锁，`SymbolMatch.Source` 是 immutable string，单次查询安全，但一次 workspace/symbol 或 rename 涉及多文件时，无法保证所有文件来自同一 revision。

**建议**

在 M6 rename/references 跨文件逻辑变复杂前：
1. 把 `workspace.Index` 拆成 build state + `IndexSnapshot`（revision + files/byName/byExternalName），查询走 snapshot；
2. build 完成后发布 `IndexSnapshot` 和 `ImportGraphSnapshot` 为同一个 `WorkspaceView{IndexRevision, GraphRevision}`，替换现有分别读锁；
3. 已索引文件的 `indexedFile` 按值共享，Source 保持 immutable string；新 revision 只复制变化路径，未变化路径引用同一数据。若实现复杂，先只做 snapshot 语义，不做 COW 优化。

**成本/风险**

中。当前 index 查询已经安全，这不是正确性修复，而是为未来一致性和并行查询做的结构性准备；必须 benchmark 证明无回退。

**阶段与验收**

- M6 切片可选。验收：同一 revision 内多文件 rename 输入一致；rebuild 后旧请求持有的 snapshot 仍可完成；race 测试通过。

### 4.13 P3：协议生成、持久索引、daemon、MCP、pull diagnostics

以下只在真实需要出现时再评估，1.0 前不排入主路径：

1. **裁剪版协议生成**：当前 `legacyInitializeFields` 是第一处 wire 摩擦；若 pull diagnostics、更多 custom 方法、严格 required/null 校验成为需求，可像 gopls 一样从 pinned meta-model 3.18.x 生成 vimls 实现的约 25 个方法。先做“生成 codec 与 `go.lsp.dev/protocol` 对同一输入解码一致的只读对照测试”，再谈替换。
2. **持久化 filecache**：gopls 的 filecache 是为了跨进程和重启复用 Go type-check/analysis 结果；vimls-go 的 `vimdata` 和 parser 结果如果出现“冷启动重扫 runtimepath 超过可接受值”，再引入 machine-global CAS cache。先量启动时间，不要预建。
3. **daemon/forwarder**：当实测多个 Vim 实例同时跑 vimls 造成重复索引时，参考 `lsprpc` 的“共享 Cache + 每连接 Session”；现有 TCP 单连接不支持多客户端，不改变。
4. **MCP/attached tools**：gopls 已把 MCP 作为 LSP 同进程复用面；vimls-go 若做 AI 工具，应复用 `internal/server` 已解析状态，而不是独立进程重解析。
5. **pull diagnostics**：gopls 的 `textDocument/diagnostic` 仍是 opt-in；LSP 3.18 客户端可能只拉不推。实现前先写客户端矩阵和 push/pull 互斥/共存策略，只做 FullDocumentDiagnosticReport。

---

## 5. 不建议现在迁移的东西

| gopls 能力 | 不迁移原因 | 替代/触发条件 |
| --- | --- | --- |
| Session/View/Snapshot 三层 | vimls 单 workspace、单语言；AGENTS 禁止无真实边界先加层 | 多 workspace folder per-folder 配置或 daemon 时再拆 |
| go/packages + package metadata | Go 专用；vimls import graph 已足够 | 语义分析需要类似包依赖缓存时再抽象 |
| go/analysis driver | vimls 诊断是固定序列 + 官方 compile cases，不是插件 analyzer | 出现多种可选分析器且互相依赖时 |
| 100MB heap ballast | 是针对 gopls 小稳态堆/大瞬态分配的 hack | vimls 先跑 M7 benchmark，需要时用 GOMEMLIMIT 和实测，不照抄 |
| filecache 持久索引 | 当前 workspace 上限 20k 文件/256MB，冷启动成本未证明 | 启动扫描 benchmark 超过预算再引入 |
| 完整生成协议 codec | 约 520KB 生成面和 codec 维护成本 | 第二处 wire 摩擦或 pull diagnostics |
| fake editor 完整实现 | vimls 只需要 fake client + marker，不需要完整编辑操作层 | M6 测试矩阵膨胀时再扩展 |
| daemon/MCP/web | 1.0 范围外；安全和生命周期成本高 | 真实用户/客户端证据 |
| zero-config 多 View | Vim 文件没有 GOOS/build tag 对应的多 build 概念 | 若未来 Vim runtime 变体按平台/版本分叉时 |
| interactive refactoring | 1.0 无交互重构需求 | 复杂 code action 需要用户输入时 |

---

## 6. 分期采纳路线图

| 阶段 | 采纳项 | 对应 gopls 证据 |
| --- | --- | --- |
| M4（语义诊断） | 4.1 诊断去抖/去重；4.7 E 码目录 | `server/diagnostics.go`、`cache/diagnostics.go`、`settings/analysis.go` |
| M5（workspace index） | 4.2 事件批处理/阈值；4.3 P1a 文件/解析缓存；4.6 progress | `text_synchronization.go`、`filewatcher`、`fs_memoized.go`、`progress.go` |
| M6（completion/编辑） | 4.4 typed settings + 生成文档；4.5 marker/fake client；4.8 fuzzy/budget；4.10 code action 层级 | `settings`、`marker`、`fuzzy`、`code_action.go` |
| M7（性能/发布） | 4.3 P1b 增量提交；4.9 poll watcher；4.11 debug/metrics；按 benchmark 决定 4.12/4.13 | `filecache`、`poll_watcher`、`debug`、`lsprpc` |
| post-1.0 | daemon、持久索引、协议生成、MCP | `lsprpc`、`filecache`、`protocol/generate`、`mcp` |

每个切片仍遵守 AGENTS 的任务 brief、owned paths 和验证命令；本表只是规划输入，不是已批准的实现合同。

---

## 7. 验收标准

### 7.1 P0 项

**诊断调度**

- 快速连续 didChange 不会排队 N 次等价分析；最终诊断来自最新 URI/revision/config/graph。
- 相同诊断内容不重复 `textDocument/publishDiagnostics`；版本变化或内容变化必发。
- 关闭文档清空诊断；旧分析不能在 close 后发布。
- `go test -race ./...`、`make check` 全绿。

**watch 事件**

- 无关 URI/扩展名、目录事件不触发 rebuild。
- burst 只触发一次；Delete+Create 合并后结果与全量 rebuild 相同。
- 超过阈值走全量路径并发送一次 warning（如适用）。

### 7.2 P1 项

**文件/解析缓存**

- 未变化文件在 rebuild 中不重新 `os.ReadFile`/`syntax.Parse`（测试 hook 计数）。
- 缓存复用的 AST 不被任何诊断追加污染；`-race` 下并发查询安全。
- 增量结果与全量重建逐项一致（paths、facts、graph edges、revision 语义除外）。

**typed settings**

- 现有配置测试不回归；新测试覆盖未知 key、弃用 alias、部分失败、workspace configuration 优先级。
- `tools/gensettings` 生成 `docs/settings.md`，`make check` 检测漂移。

**progress**

- workDone begin/report/end 配对；不支持客户端降级不崩溃；取消能停任务。
- 不向 stdout 写日志。

**marker/fake client**

- 第一批 marker 覆盖 document symbol/definition/rename/diagnostics。
- 失败输出可读 diff；subprocess smoke 保留。

**E 码目录**

- 每个 Supported E 码有 official/evidence 链接；未知码测试失败；生成可重现。

### 7.3 P2 项

- fuzzy benchmark 与现有 rank 对比后决定合并；completion 预算不 panic。
- 服务端 watcher 默认关闭；启用后外部变更最终一致且空闲开销有上限。
- debug 服务默认关闭、仅 loopback；stdout 纯净；无自动上传。
- `IndexSnapshot` 在 benchmark 无回退的前提下合并。

---

## 8. 结论

gopls 的快照印证了 vimls-go 已有架构选择：**不可变文档快照、内部 byte span、边界位置转换、版本化发布、固定包边界、bounded worker、官方 corpus 测试**都是正确的，不需要推翻。gopls 额外提供的是这层架构在长期运行中的“运维性补全”：诊断去抖与去重、文件身份缓存、watch 事件治理、配置/文档单一来源、progress 和测试 harness。

建议的第一批行动（按性价比排序）：

1. 落地 4.1 的 generation debounce + `publishedHash`，这是 M4 语义诊断的发布质量基础。
2. 落地 4.2 的 watched-file 批处理/阈值，直接消除当前最大的重复重建浪费。
3. M5 前做 4.3 P1a 的文件内容哈希 + 解析缓存，再根据 benchmark 决定 4.3 P1b。
4. 用 4.4 收敛配置解析，并生成 `docs/settings.md`。
5. 用 4.5 的 marker/fake client 替换手写 LSP 测试的增长部分。

同时保留 vimls-go 已有的三个优势：**bounded framing、纯 Go/Makefile 离线 gate、committed official artifacts**。不要用 gopls 的 Session/View 全家桶、持久 filecache、daemon 和完整生成协议替换它们。

---

## 附录 A：关键文件索引

### gopls

| 关注点 | 路径 |
| --- | --- |
| 入口/CLI/daemon | `main.go`、`internal/cmd/serve.go`、`internal/cmd/cmd.go`、`internal/lsprpc/lsprpc.go` |
| 协议生成与位置 | `internal/protocol/doc.go`、`tsclient.go`、`protocol.go`、`mapper.go`、`uri.go`、`edits.go` |
| 配置 | `internal/settings/settings.go`、`default.go`、`analysis.go`；`doc/settings.md` |
| 文件模型 | `internal/file/file.go`、`hash.go`、`kind.go`、`modification.go` |
| cache 核心 | `internal/cache/session.go`、`view.go`、`snapshot.go`、`filemap.go`、`fs_overlay.go`、`fs_memoized.go` |
| 解析缓存/分析 | `internal/cache/parse_cache.go`、`parse.go`、`analysis.go`、`symbols.go`、`metadata/graph.go`、`typerefs/refs.go` |
| 诊断管线 | `internal/server/server.go`、`text_synchronization.go`、`diagnostics.go`；`internal/golang/diagnostics.go`；`internal/cache/diagnostics.go` |
| workspace/watch | `internal/server/workspace.go`、`general.go`；`internal/filewatcher/*`；`doc/workspace.md` |
| progress/debug/telemetry | `internal/progress/progress.go`、`internal/debug/*`、`internal/telemetry/*`、`internal/server/counters.go` |
| 测试 | `internal/test/marker/doc.go`、`internal/test/integration/doc.go`、`runner.go`、`env.go`、`expectation.go`、`fake/*` |
| 命令生成 | `internal/protocol/command/interface.go`、`gen/*` |
| 文档生成 | `internal/doc/generate/generate.go`、`internal/doc/api.go` |
| 解析修复 | `internal/cache/parsego/parse.go` |
| 模糊/补全 | `internal/fuzzy/matcher.go`、`symbol.go`；`internal/golang/completion/completion.go` |
| 新表面 | `internal/mcp/mcp.go`、`doc/features/mcp.md`、`doc/daemon.md`、`doc/design/integrating-interactive-refactoring.md` |

### vimls-go

| 关注点 | 路径 |
| --- | --- |
| server 生命周期/调度/发布 | `internal/server/server.go`（`Server`、`Run`、`startAnalysis`、`analyzeDocument`、`publishSyntax`） |
| 配置解析 | `internal/server/config.go` |
| workspace rebuild/watch | `internal/server/workspace.go`（`scheduleWorkspaceRebuild`、`buildWorkspaceIndex`、`DidChangeWatchedFiles`、`refreshFileWatchRegistration`） |
| 文档快照 | `internal/workspace/documents.go` |
| 文本/位置 | `internal/text/snapshot.go` |
| 解析 worker | `internal/workspace/parse.go` |
| 索引/图 | `internal/workspace/index.go`、`import_graph.go`、`files.go` |
| 诊断定义 | `internal/syntax/vimls_diagnostics.go` |
| 语义分析 | `internal/analysis/scopes.go`、`types.go`、`diagnostics_test.go` |
| 生成器 | `tools/gencommands`、`tools/genbuiltins`、`tools/genoptions`、`tools/genvariables` |
| 测试策略/架构/路线 | `docs/testing.md`、`docs/architecture.md`、`docs/roadmap.md`、`docs/diagnostics.md` |
| subprocess 测试 | `test/integration/lsp_subprocess_test.go` |
| 本地 gate | `Makefile` |

## 附录 B：规模与依赖对比

| 指标 | vimls-go | gopls 快照 |
| --- | ---: | ---: |
| Go 文件 | 143 | 646 |
| `*_test.go` | 84 | 214 |
| Go 总行数 | 约 87,597 | 约 165,123 |
| 内部包数（直接） | 8 | 27 |
| 协议来源 | `go.lsp.dev/protocol` | LSP 3.18.2 meta-model 生成 |
| 直接依赖 | 2 | 17 |
| framing 上限 | header 8KiB / body 16MiB | 使用上游 jsonrpc2；gopls 自身未显式收紧 |
| 文件监听 | 客户端 LSP watcher | 客户端优先，`fsnotify`/`poll` opt-in fallback |
| 测试模式 | 单 subprocess + 包测试 | marker、fake editor、in-process/forwarded/separate-process |
| 官方资产 | Vim 9.2.1015 corpus，committed artifacts | Go 工具链与 LSP 场景，txtar marker |
