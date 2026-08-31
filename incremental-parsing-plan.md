# 基于 gopls 模型的增量编辑实施计划

> 状态：待实施。
>
> 本计划以仓库根目录的 `gopls-incremental-parsing.md` 为设计依据。
> 2026-08-31 已通过提交 `038c4a0` 撤销旧的局部 AST `Reparse` 方案。

## 1. 结论

vimls-go 的“增量编辑”只发生在文本、快照、缓存和依赖失效层，不增量修改 AST。

每次接受 `textDocument/didChange` 后：

1. 按 LSP 顺序把局部 changes 应用到上一份不可变文本快照；
2. 得到一份包含完整新文本的新快照；
3. 内容身份相同才可复用不可变 parser 结果；
4. 内容身份变化时，必须对完整新文本调用 `syntax.Parse`；
5. 需要语义结果时，对变化文件完整执行 `analysis.Analyze`；
6. 只复用未变化文件的纯 parser tree；analysis/diagnostics 只有在拥有独立且完整的
   identity 时才可复用，并用 revision/identity 阻止陈旧结果发布。

不再实现或恢复以下机制：

- `syntax.Reparse`；
- dirty interval、restart unit、checkpoint 或 convergence guard；
- AST subtree splice、clone/rebase 或跨版本 node identity；
- 局部维护 scope、reference、type、diagnostic 或 symbol 表；
- Tree-sitter、CGo、rope、piece table、arena、pool 或持久磁盘 cache。

核心原则是：

> 完整重建最小可信结果，用不可变快照、内容身份和保守依赖失效限制重建范围。

## 2. gopls 概念在 vimls-go 中的对应关系

| gopls | vimls-go | 处理方式 |
| --- | --- | --- |
| overlay | `text.Snapshot` | 保存 URI、version、revision、完整文本、行索引和内容身份 |
| incremental sync | `text.ApplyChanges` | 只把 LSP edits 顺序合成为完整新文本 |
| parse key | URI + 内容身份 | 当前只有一种 full parser mode，不提前增加 mode 抽象 |
| full file parse | `syntax.Parse(snapshot.Text())` | cache miss 时唯一合法的变化文本解析路径 |
| immutable AST | `*syntax.File` | parser 完成后只读；分析诊断不得写回 cache tree |
| package type-check | `analysis.Analyze(file)` | 当前最小一致语义单元是单个完整 Vim 文件 |
| metadata/import graph | `workspace.Index` + `ImportGraphSnapshot` | 原子替换文件 facts，按 revision 阻止混用 |
| unchanged-file AST reuse | open-document parser cache；workspace cache 后置 | 只复用内容完全相同的完整树 |
| conservative invalidation | reverse dependents | 第一版无法证明不受影响时全部重分析 |
| precise pruning | 后续 surface key | 只有真实 consumer 读集和 profile 都证明需要时才实现 |

不照搬 Go 专属层：

- Vim 文件没有 Go package、`go/types.Info`、export data 或 `go list` metadata。
- 目前没有真实的 Header/Full 双解析需求，因此不增加 parser mode。
- Vim parser 已有不完整输入恢复；不增加 gopls 的 `fixAST`/`fixSrc` 对应层。
- workspace 精确裁剪必须由 vimls-go 实际跨文件消费者决定，不能复制 typerefs。
- 不实现 `packageHandle` 状态机或跨文件 semantic cache。

## 3. 回退后的实现基线

当前已经具备且必须保留：

- `text.Snapshot` 是不可变完整文本；
- 同一 `didChange` 的 changes 会依次作用于中间文本；
- UTF-8、UTF-16、UTF-32 position 在 text/server 边界转换为 byte offset；
- `workspace.Documents` 维护 URI、LSP version、内部 revision、取消和 config revision；
- `Documents.IsCurrent` 阻止旧 document/config analysis 提交；
- `ImportGraphSnapshot` 是不可变、带 revision 的图视图；
- workspace rebuild 在发布前检查 workspace generation 和 open snapshot 指针；
- parser、analysis 和 workspace 批量任务已有有界 worker；
- `syntax.Parse` 是完整 source 的唯一 parser oracle。

当前需要修正：

- parser cache 仍按 URI/revision 隐式关联，没有明确的内容身份；
- 后台分析会把 compatibility/semantic diagnostics 混入随后缓存的 `File` 副本；
- 同内容的新 revision 不能明确复用 parser tree；
- `didSave` 无文本变化时仍会重新失效 workspace facts 和排队分析；
- repeated `didOpen` 的旧 cache 生命周期边界不够明确；
- workspace rebuild 会重新解析全部文件，不能复用内容未变化文件的完整 AST；
- publish 只检查 document/config 和 graph revision，尚未统一检查 index/graph 身份。

## 4. 正确性和一致性不变量

以下契约在实现前冻结，后续优化不得放宽。

### 4.1 文本与快照

1. 每个已接受的 `didChange` 都产生新的 immutable `Snapshot` 和内部 revision。
2. 多个 changes 按收到顺序应用；第 N 个 range 基于第 N-1 个 change 的结果。
3. LSP version 仅用于顺序和发布；parser cache 身份由完整文本决定。
4. 内容身份只由完整 source bytes 决定，不包含 URI、version、revision 或配置。
5. 非法 range、非法 encoding、旧/重复 version 不得改变当前 snapshot。
6. LF、CRLF、BOM、无末尾换行、无效 UTF-8、combining 和 astral 内容均按原始 bytes 保留。

### 4.2 Parser cache

1. 内容变化时必须调用 `syntax.Parse` 解析完整新 source。
2. cache hit 必须同时满足 URI、内容身份和 `file.Source == snapshot.Text()`。
3. hash 只作内容身份和快速排除；完整 source equality 是碰撞安全闸门。
4. 内容相同可返回同一个 immutable `*syntax.File`；内容不同必须得到全新树。
5. parser cache 只包含 parser AST、tokens、blocks 和 parser diagnostics。
6. compatibility、semantic、import、workspace、truncation 和 publish diagnostics 不得写入 cache tree。
7. parser 在 server/document/workspace 锁外运行。
8. stale parse 可以完成并返回给持有该 snapshot 的旧请求，但不能覆盖当前 URI cache。
9. 超过 `maxFileBytes` 的文档不调用 parser，也不进入 parser cache。
10. 不以 panic/recover 实现 fallback；parser panic 始终是 bug。

### 4.3 Analysis 与 workspace

1. 变化文件完整执行 `analysis.Analyze`，不修改旧 analysis maps。
2. 每次分析绑定一个 document snapshot、config revision 和 workspace identity。
3. workspace identity 至少包含单调 workspace generation、index 实例、index revision
   与 graph revision；新建 Index 后数值相同的 revision 不能被误认为同一状态。
4. 分析使用的跨文件 source/facts 必须在同一次 workspace lock 内复制出来。
5. parser cache 安装只检查 open-document lifetime/snapshot/source；config/index/graph
   变化不使纯 parser tree 失效。
6. 文件 facts 与 import edges 的提交必须再次确认 document snapshot 为 current。
7. diagnostics 发布前再次确认 document、config 和完整 workspace identity 均未变化。
8. 前台请求不能把可变 `*workspace.Index` 带出锁后无校验地读取：必须捕获 index
   实例/revision，并在返回前复核；或在一个 workspace 临界区复制本请求需要的数据。
9. cache 安装与 diagnostics 发布是独立动作；缓存命中不能绕过 stale publish barrier。
10. 无法证明依赖未受影响时，允许多重分析，不能复用可能陈旧的语义结果。

## 5. 目标数据流

```text
LSP contentChanges
       │
       ▼
按顺序应用到旧 immutable Snapshot
       │
       ▼
新 Snapshot(uri, version, revision, full text, content ID)
       │
       ├─ URI + content ID + exact source 命中
       │       └──────────────► 复用 immutable syntax.File
       │
       └─ miss
               └──────────────► syntax.Parse(完整新文本)
                                      │
                                      ▼
                         条件安装纯 parser cache
                                      │
                                      ▼
                      完整 analysis.Analyze(本次 File view)
                                      │
                                      ▼
               条件原子替换 workspace facts/import edges
                                      │
                                      ▼
                         捕获 post-install workspace identity
                                      │
                                      ▼
                计算跨文件 diagnostics / workspace-dependent 结果
                                      │
                                      ▼
          document/config/workspace identity 全部仍 current
                                      │
                                      ▼
                              发布 diagnostics
```

这里没有从 edit range 到 parser 的边；parser 只接收完整 source。

## 6. 最小数据模型

字段名可在实现时小幅调整，但不得引入额外 Manager/Service/Provider 层。

```go
// ContentID 是完整文本的稳定 SHA-256 身份。
type ContentID [32]byte

func ContentIDOf(source string) ContentID
func (s *Snapshot) ContentID() ContentID

// URI 是 parsed map 的 key；当前只有一种 full parse mode。
type parsedDocument struct {
    contentID text.ContentID
    file      *syntax.File
}

type workspaceIdentity struct {
    generation    uint64
    index         *workspace.Index
    indexRevision uint64
    graphRevision uint64
}
```

约束：

- `Snapshot` 构造时只计算一次 content ID。
- disk workspace source 使用同一个 `text.ContentIDOf`，不重复定义 hash 规则。
- `parsedDocument` 不保存 analysis/config/target/graph 状态。
- `workspaceIdentity.index` 只作实例身份；请求不得依靠该指针绕过 revision 复核。
- 不建立通用 cache interface；open documents 和 workspace rebuild 只使用各自已有 map。
- 第一版不实现 in-flight promise/singleflight。同一 snapshot 的并发 miss 允许重复完整解析，
  最终只能安装等价结果；只有 profile 证明重复解析显著时才增加去重。

## 7. 生命周期状态转换

| 事件 | Snapshot | Parser cache | Analysis/workspace |
| --- | --- | --- | --- |
| `didOpen` | 总是新 lifetime、新 revision | 先清 URI 旧条目 | 旧 analysis 取消；建立 overlay 并排队分析 |
| content-changing `didChange` | 新 version/revision/content ID | 旧条目不命中；完整 Parse | 取消旧 analysis；失效该文件 facts；分析新快照 |
| same-content `didChange` | 新 version/revision，相同 content ID | 复用纯 syntax tree | 不移除相同 workspace facts；为新 version 重新发布需要的结果 |
| `didSave` 无 text/同 text | snapshot 不变 | 保留 | 不取消正确分析，不重复失效 workspace |
| `didSave` 不同 text | 新 revision，保留已有 LSP version | miss，完整 Parse | 取消旧 analysis；替换 overlay facts |
| config/target change | snapshot 不变 | 复用 | 取消旧 analysis，完整重算配置相关 diagnostics |
| graph/index change | snapshot 不变 | 复用 | 只重跑需要跨文件状态的 analysis；旧 identity 不得发布 |
| `didClose` | 删除 overlay | 删除 URI cache | 取消 analysis；恢复磁盘文件 facts；清 diagnostics |
| repeated `didOpen` | 新 lifetime | 删除旧 URI cache，即使文本相同 | 旧任务不能恢复新 lifetime 的 cache/diagnostics |
| oversized document | 正常保存完整 snapshot | 不缓存、不 Parse | 只产生本次 oversized diagnostic view |

额外边界：

- parser cache 的安装只绑定当前 open snapshot 指针和 source。仅 config/index/graph
  变旧的任务仍可保留与当前 snapshot 完全相同的纯 parser tree，但不得提交 facts 或发布。
- repeated `didOpen` 即使 source 相同也形成新 lifetime；旧任务不得向新 snapshot 安装结果。
- close 恢复磁盘文件时，锁内只删除 overlay/cache 并捕获关闭状态；磁盘读取和完整
  `syntax.Parse` 必须在锁外执行；重新加锁后，仅在该 URI 尚未 reopen 且 workspace
  generation 未变化时安装磁盘 facts。

## 8. 串行实施任务

各任务单独提交。I0-I4 完成全部 production code 和正确性；I5 才运行属性、race 和
性能测试。写代理统一使用 Terra high，且同一 package
不得并发修改。

### I0：内容身份

**Goal**

让所有 immutable text snapshots 和 disk source 使用同一个稳定内容身份。

**Allowed paths**

- `internal/text/snapshot.go`
- `internal/text/snapshot_test.go`

**Forbidden paths**

- `internal/syntax/`
- `internal/analysis/`
- `internal/workspace/`
- `internal/server/`
- docs、generated files、`go.mod`、`go.sum`

**Required behavior**

- 使用标准库 SHA-256，不新增依赖。
- 同 bytes 在不同 URI/version/revision 下得到相同 ID。
- 任一 byte 变化都由测试证明 ID 变化。
- `ApplyChanges` 返回的 ID 等于最终完整文本直接计算的 ID。
- 旧 Snapshot 的 text、line index 和 ID 保持不变。

**Validation**

```sh
gofmt -w internal/text/snapshot.go internal/text/snapshot_test.go
go test -mod=readonly ./internal/text \
  -run 'Test(SnapshotContentID|ApplyChanges|IncrementalChanges)'
```

### I1：明确 document transition 并接入同步 handler

**Goal**

让 Open/Change/Save 明确报告内容是否变化，并让直接调用它们的 server handler 消费
该结果；提交结束时所有调用方必须可编译。

**Allowed paths**

- `internal/workspace/documents.go`
- `internal/workspace/documents_test.go`
- `internal/server/server.go`
- `internal/server/document_sync_test.go`
- `internal/server/structure_test.go`
- `internal/server/navigation_test.go`

**Forbidden paths**

- `internal/syntax/`
- `internal/analysis/`
- 其它 workspace production 文件
- 其它 server production/test 文件
- docs、generated files、`go.mod`、`go.sum`

**Required behavior**

- 使用直接返回值 `(*text.Snapshot, bool, error)`；`bool` 表示最终完整内容是否变化，
  不增加 transition manager/type hierarchy。
- `Change` 仍拒绝非递增 version，并为每个接受的通知创建新 snapshot/revision。
- `Change` 返回最终内容相对旧 snapshot 是否改变。
- same-content `Change` 取消旧 analysis，因为请求/diagnostics version 已变化。
- `Save(nil)` 和 `Save(same text)` 返回 unchanged，不创建 snapshot、不取消 analysis。
- `Save(different text)` 创建新 revision，保留已有 LSP version 并取消旧 analysis。
- error path 不修改当前 document。
- handler 对 same-content change 不调用 `removeWorkspaceURI`；是否跳过随后相同 facts
  的 replacement 在 I4 完成。
- handler 对 unchanged save 不移除 workspace facts、不重复排队分析。

**Validation**

```sh
gofmt -w internal/workspace/documents.go internal/workspace/documents_test.go \
  internal/server/server.go internal/server/document_sync_test.go \
  internal/server/structure_test.go internal/server/navigation_test.go
go test -mod=readonly ./internal/workspace \
  -run 'TestDocuments(OpenChangeSaveCloseAndReopen|RejectStaleInvalidAndMissingChanges|CancelAndRejectStaleAnalysis)'
go test -mod=readonly ./internal/server \
  -run 'TestServerDocumentSynchronization'
```

### I2：完整解析的纯 parser cache

**Goal**

把所有 open-document parser 调用汇入一个按内容身份命中的完整解析入口；变化内容永远
调用 `syntax.Parse`，cache tree 永远只读且纯净。

**Allowed paths**

- `internal/server/server.go`
- `internal/server/structure.go`
- `internal/server/navigation.go`
- `internal/server/workspace_navigation.go`
- `internal/server/rename.go`
- `internal/server/language_features.go`
- `internal/server/import_diagnostics.go`
- `internal/server/document_sync_test.go`
- `internal/server/navigation_test.go`
- `internal/server/diagnostics_test.go`

**Forbidden paths**

- `internal/syntax/`
- `internal/text/`
- `internal/analysis/`
- `internal/workspace/`
- integration fixtures、docs、generated files、`go.mod`、`go.sum`

**Required behavior**

- package-private `parseSnapshot` 只做 exact cache hit 或 `syntax.Parse(full source)`。
- hit 比较 URI map key、content ID 和 exact source。
- Parse 在 `publishMu` 外运行；安装时再验证当前 open snapshot 指针。
- changed source 返回新 pointer；same source/new revision 可返回旧 pointer。
- foreground request 和 background analysis 使用同一入口。
- parser result 在安装前不附加 compatibility/semantic/workspace diagnostics。
- 分析使用独立 File header 和独立 Diagnostics slice；若 audit 发现其它消费者写 AST，
  必须先修复消费者或复制对应字段，不能污染 cache。
- repeated open/close 清 cache；旧 document snapshot 的 work 不能恢复 cache。
- cache 安装不依赖 target/config/index/graph revision；这些状态不属于 parser key。
- oversized file 不进入 cache。
- 不增加 promise、LRU、cache interface 或 parser mode。

**Validation**

```sh
gofmt -w <changed-go-files>
go test -mod=readonly ./internal/server \
  -run 'Test(ServerDocumentParserCache|ServerRepeatedDocumentOpen|ServerStaleAnalysis|NavigationReusesCurrentParsedDocument|TargetVersionCompatibilityDiagnosticsReanalyze|ServerSkipsAnalysisForOversizedDocument)'
```

### I3：LSP 生命周期和 stale barrier

**Goal**

让 didOpen/change/save/close、配置变化和并发请求始终观察单一 document snapshot，
并正确分离纯 parser cache 与 document/config diagnostics 的 stale 条件。workspace
index/graph identity 在 I4 验收。

**Allowed paths**

- `internal/server/server.go`
- `internal/server/workspace.go`
- `internal/server/document_sync_test.go`
- `internal/server/diagnostics_test.go`
- `internal/server/navigation_test.go`
- `internal/server/workspace_test.go`
- `internal/workspace/documents.go`
- `internal/workspace/documents_test.go`

**Forbidden paths**

- `internal/syntax/`
- `internal/analysis/`
- `internal/workspace/index.go`、`internal/workspace/import_graph.go`
- integration fixtures、docs、generated files、`go.mod`、`go.sum`

**Required behavior**

- server 使用 I1 的 content-changed 结果决定是否移除 workspace facts。
- same-content didChange 复用 AST，但 diagnostics 的 LSP version 属于新 snapshot。
- didSave unchanged 不失效 cache/index，不取消或重复排队。
- close/reopen、repeated open、change during parse、foreground/background race 均受 pointer
  identity 与 `IsCurrent` 保护。
- close 的磁盘读取与 `syntax.Parse` 在锁外执行；恢复前再次确认 URI 未 reopen 且
  workspace generation 未变化。
- 若 URI 仍关闭但 generation 已变化，放弃本次结果并触发一次 workspace rebuild，
  不能让该磁盘文件永久缺席；若 URI 已 reopen，则由新 overlay analysis 收敛。
- config-only change 复用 syntax tree，但重新计算配置相关 diagnostics。
- oversized document 不安装 parser cache。
- document lifetime/snapshot/source 不匹配时不能安装 parser cache；仅 config/index/graph
  stale 时，当前 source 的纯 parser tree 仍可安装，但 facts 和 diagnostics 不得提交。
- shutdown 后旧 worker 不能恢复状态。

**Validation**

```sh
gofmt -w <changed-go-files>
go test -mod=readonly ./internal/workspace \
  -run 'TestDocuments(OpenChangeSaveCloseAndReopen|CancelAndRejectStaleAnalysis)'
go test -mod=readonly ./internal/server \
  -run 'Test(ServerDocumentSynchronization|ServerDocumentHandlersCancelStaleAnalysis|AnalysisQueueCoalescesRapidDocumentChanges|ServerDocumentParserCache|ServerRepeatedDocumentOpen|ServerStaleAnalysis|TargetVersionCompatibilityDiagnosticsReanalyze|OpenDocumentsOverrideDisk|CloseRestore)'
```

### I4：workspace identity 一致性

**Goal**

使一次分析使用的 index facts 与 import graph 来自同一已发布 workspace 状态，并在提交
和发布时验证同一 identity。此阶段仍保守重跑全部反向依赖。

**Allowed paths**

- `internal/workspace/index.go`
- `internal/workspace/index_test.go`
- `internal/workspace/import_graph.go`
- `internal/workspace/import_graph_test.go`
- `internal/server/workspace.go`
- `internal/server/server.go`
- `internal/server/import_diagnostics.go`
- `internal/server/workspace_navigation.go`
- `internal/server/navigation.go`
- `internal/server/language_features.go`
- `internal/server/rename.go`
- `internal/server/diagnostics_test.go`
- `internal/server/workspace_test.go`
- `internal/server/navigation_test.go`
- `internal/server/rename_test.go`

**Forbidden paths**

- `internal/syntax/`
- `internal/text/`
- `internal/analysis/`
- unrelated server handlers/tests
- integration fixtures、docs、generated files、`go.mod`、`go.sum`

**Required behavior**

- identity 包含 workspace generation、index pointer/revision 和 graph revision。
- index 与 graph replacement 在 `workspaceMu` 下形成一组 post-install identity。
- 本地 analysis 完成后先条件安装当前文件 facts/edges，再捕获 post-install identity，
  然后计算跨文件 diagnostics，最后按同一 identity 发布。
- 所有跨文件分析在同一临界区复制 graph、target source 和 symbol facts；前台请求若
  分多次查询 mutable Index，必须在返回前验证捕获的 index 实例/revision 和 generation。
- 前台 request identity 变化时只允许从新 identity 重试一次；第二次仍变化则丢弃全部
  部分结果并返回 `protocol.ErrContentModified`。rename 等编辑请求不能返回半份 edits。
- background analysis identity 变化时丢弃结果并重新排队，不向客户端返回错误。
- publish 同时检查 workspace generation、index instance/revision 和 graph revision。
- same-content edit 只有在该 path 的 facts 已安装、没有 pending replacement 且 index
  source 与当前 source 完全相同时，才跳过 `replaceWorkspaceFile`、`Index.Replace`、
  `ImportGraph.Replace` 和 reverse-dependent 调度；若旧 analysis 尚未安装 facts，
  新 revision 仍必须正常安装。
- changed content 完整解析/分析后原子替换该文件 facts 和 import edges。
- 无法确认依赖不受影响时继续重跑全部 reverse dependents。
- 新 Index 实例发布后，持有旧 pointer 的请求可安全读完；同一可变 Index 发生 revision
  变化时，旧请求必须丢弃/重试，不能返回混合结果。

**Validation**

```sh
gofmt -w <changed-go-files>
go test -mod=readonly ./internal/workspace \
  -run 'Test(IndexRevision|ImportGraphReplace|ImportGraphRevision)'
go test -mod=readonly ./internal/server \
  -run 'Test(WorkspaceImportGraph|ImportTargetChangeReanalyzesReverseDependent|GraphRevisionRejectsStaleDiagnostics|WorkspaceIdentity|WorkspaceIdentityRetry|OpenDocumentsOverrideDisk|CrossFile)'
```

### I5：属性测试、最终审查和性能证据

**Goal**

代码全部稳定后证明长期编辑一致性、并发安全和实际 cache 收益。性能测试只能在本阶段
运行。

**Allowed paths**

- `internal/text/snapshot_test.go`
- `internal/workspace/documents_test.go`
- `internal/workspace/index_test.go`
- `internal/workspace/import_graph_test.go`
- `internal/workspace/parse_test.go`
- `internal/server/document_sync_test.go`
- `internal/server/diagnostics_test.go`
- `internal/server/workspace_test.go`
- `internal/server/navigation_test.go`
- `internal/server/rename_test.go`
- `incremental-parsing-plan.md`
- 已实现事实对应的 `docs/architecture.md`、`docs/testing.md`、`docs/roadmap.md`

**Forbidden paths**

- 未经对应阶段重新打开的 production code
- `internal/syntax` AST 增量实现
- dependencies、generated files、`go.mod`、`go.sum`

**Required behavior**

- 新增 text edit fuzz/property：每步增量 edits 的最终全文与直接 replacement 相同。
- deterministic 100-step edit sequence 覆盖多编码、CRLF、BOM、combining 和 astral。
- race test 覆盖同 snapshot 并发读、change during parse、close/reopen 和 workspace rebuild。
- readonly QA 审查 parser tree immutability、lock order、stale barriers 和 cache/source 对应。
- 性能只比较：content-ID 构造、same-content open-document cache hit、
  changed-file full Parse，并记录一次 workspace full rebuild/`ParseSources` 基线，
  作为第 10.1 节是否立项的证据。
- 小编辑后的 full parse 不要求比 `syntax.Parse` 更快；它本来就是同一完整解析路径。
- benchmark 构造必须在计时区外，记录五次 `ns/op`、`B/op`、`allocs/op`。

**Validation**

```sh
gofmt -w <changed-go-files>
go test -mod=readonly ./internal/text ./internal/workspace ./internal/server
go test -mod=readonly -race ./internal/text ./internal/workspace ./internal/server
go test -mod=readonly ./internal/text -run '^$' \
  -fuzz '^FuzzApplyChanges$' -fuzztime=30s
go test -mod=readonly ./internal/text -run '^$' \
  -bench 'BenchmarkContentID' -benchmem -count=5
go test -mod=readonly ./internal/server -run '^$' \
  -bench 'Benchmark(ParseCache|WorkspaceRebuild)' -benchmem -count=5
```

## 9. 必测矩阵

### 文本合成

- insert、delete、equal-length replace、长度变化 replace；
- 文件头、中间、EOF、whole-document replacement；
- 同一通知多个 changes，验证每个 range 使用中间文本；
- UTF-8/16/32、tab、BOM、CRLF、combining、astral、无效 UTF-8；
- invalid range、split surrogate/rune、stale version 不改变当前 snapshot；
- 最终内容未变化但 version 前进。

### Cache identity

- 同文本、不同 revision/version 命中同一 parser tree；
- 不同文本即使长度相同也 miss；
- 人工构造相同 hash 身份但 source 不同的测试替身必须 miss；
- changed source 的结果与直接 `syntax.Parse(newSource)` 完全一致；
- cache tree 在 analysis、navigation、rename、completion 和 diagnostics 后不变；
- foreground 与 background 同时 miss 只能安装当前 snapshot 的等价结果。

### 生命周期

- didChange during analysis；
- rapid revision 1..N，中间结果全部不能发布；
- didSave nil/same/different；
- close during parse、close/reopen、repeated didOpen；
- close 后立即 reopen，旧 close 的磁盘恢复不得覆盖新 overlay；
- close 恢复期间 workspace generation 变化会触发 rebuild，文件不会永久缺席；
- configuration/target change；
- graph/index revision change；
- oversized document；
- cancellation 和 shutdown。

### Workspace

- open overlay 覆盖 disk，close 后恢复 disk；
- workspace/runtime roots 变化使旧 rebuild 无法发布；
- watcher rebuild 只允许当前 generation 发布；第一版允许完整重解析其文件集；
- added/changed/removed/unreadable/oversized 文件；
- static、dynamic、missing、cyclic imports；
- reverse dependent 保守重分析；
- same-content edit 在 facts 已安装/尚未安装两种状态下都正确；
- index facts 与 graph edges 同代发布。

## 10. 后续 workspace 优化

### 10.1 未变化 workspace 文件的 parser cache

open-document 增量编辑正确性不依赖 closed-file/workspace rebuild 的 AST cache。只有 I0-I5
完成后的 profile 证明 workspace 全量重解析是主要成本，才增加独立任务：

- build 开始时捕获上一代 `path -> {content ID, source, *syntax.File}` 只读 map；
- path、content ID 和 exact source 都相同才复用；其它文件完整 `syntax.Parse`；
- open overlay 优先于 disk，同 path 的来源状态不能混用；
- 新 parser map 与 index/graph 在同一个 generation 检查后发布；
- removed、unreadable、oversized、canceled 或 superseded 条目不得发布；
- 不增加持久 cache、TTL、LRU、ref-count 或通用 cache package。

该任务需要明确允许 `internal/server/server.go` 和 `internal/server/workspace.go`，但不属于
本轮 Definition of Done。

### 10.2 精确依赖裁剪

gopls 使用 package key 和 typerefs 阻止无关反向依赖继续失效。vimls-go 第一版不猜测
等价的 key：I0-I5 先保留安全的 reverse-dependent 全量重分析。

只有最终 profile 证明 dependent reanalysis 是主要成本，才建立后续任务。届时 key 只能
包含当前跨文件消费者真实读取的数据，例如：

- parser 是否可被静态信任、root dialect；
- resolved static import edges；
- 顶层 symbol 的 name、kind、exported、deprecated 和相关静态语义；
- user command/global/autoload facts。

必须先为每个 consumer 列出读集，再定义 key。key 未覆盖的动态或恢复状态一律保守失效。
这项优化不属于增量编辑正确性的 Definition of Done。

## 11. Commit 和验证纪律

- `038c4a0` 是旧 AST 增量方案的 revert 基线。
- I0-I5 每个任务至少一个 milestone-scoped local commit；不授权 push。
- 每个小任务只跑其 Validation 中的相关 package/测试。
- 修改 Go 文件后先请求 gopls diagnostics，再跑 focused tests。
- 每次提交前使用 Terra high 做只读 `/ponytail-review`，解决 findings 后以
  `VIMLS_PONYTAIL_REVIEWED=1` 提交。
- 只 stage 当前任务 Allowed paths；保留用户的未跟踪参考文档和其它无关改动。
- 所有代码和 correctness tests 完成前不运行 benchmark。
- 只有 I5 完成后才按实际支持更新 architecture/testing/roadmap。

## 12. Definition of Done

- [ ] LSP edits 只在 text 层合成完整新文本。
- [ ] `syntax.Parse` 是变化内容唯一 parser 入口；仓库没有 `Reparse` 或 AST splice。
- [ ] Snapshot 内容身份稳定、与 version/revision 解耦。
- [ ] parser cache 只有精确 source identity 命中，hash collision 不会误复用。
- [ ] changed content 产生完整新 AST；same content 可复用 immutable AST。
- [ ] parser cache 不含 compatibility/semantic/workspace/publish diagnostics。
- [ ] analysis 对变化文件完整重建，旧 analysis 和旧 AST 保持不变。
- [ ] didOpen/change/save/close/config 的状态转换与第 7 节一致。
- [ ] stale document lifetime/snapshot/source 结果不能覆盖当前 URI parser cache。
- [ ] stale document/config/workspace 结果不能替换 facts 或发布 diagnostics；纯 parser
      cache 不因 config/index/graph 变化而失效。
- [ ] text、workspace 和 server 的 focused correctness/race/fuzz gates 通过。
- [ ] 全部代码完成后才记录 content identity、cache hit 和 full parse 性能。
- [ ] 文档只描述已经实现的事实。
