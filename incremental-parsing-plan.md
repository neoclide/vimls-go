# 文件变化后的增量 AST 解析实施规划

## 1. 结论

vimls-go 不接入 Tree-sitter，不增加 CGo、grammar、query、`TSTree` 或
`TSNode`。本项目只学习 Tree-sitter 的增量计算原则，在现有纯 Go、Legacy/Vim9
双根 parser 上实现增量 AST。

最终保留两个直接入口：

```go
// Parse 始终从完整 source 建立新树，是永久的正确性 oracle。
func Parse(source string) *File

// Reparse 尝试复用 previous；无法证明安全时退化为 Parse。
func Reparse(previous *File, source string) *File
```

实现必须满足：

1. 对任意旧文本、新文本和合法编辑序列，`Reparse(Parse(old), new)` 的导出
   AST、token、block、diagnostic、`OpaqueTail`、byte span 和节点共享关系必须与
   `Parse(new)` 等价。
2. `previous` 永远只读；增量解析期间旧请求可以继续并发读取它。
3. source 相同直接返回 `previous`；source 不同时结果必须完整属于新 source。
4. syntax 层只接收完整 source 和 byte offset，不接收 LSP position、document
   revision 或 configuration revision。
5. Legacy、Vim9、混合方言、未知命令、恢复节点和嵌套 Ex payload 的行为不变。
6. full fallback 是正常路径；parser panic 仍是 bug，不能 recover 后静默 fallback。
7. “命中 cache”不等于完成。必须同时证明等价性和实际的 time/allocation 收益。

## 2. 研究结论如何落地

本规划综合以下三份研究：

- [`docs/tree-sister-research.md`](docs/tree-sister-research.md)：提供 byte dirty
  interval、parser state、语法单元、guard、clone/rebase 和 full fallback 主线。
- [`docs/gopls-research.md`](docs/gopls-research.md)：采用不可变旧结果、内容身份、
  旧请求安全读完、纯 parser cache 和 stale publish barrier；不迁移完整
  Session/View/Snapshot 或持久 filecache。
- [`docs/typescript-go-research.md`](docs/typescript-go-research.md)：采用 generation、
  COW、内容缓存和 baseline/fuzz 纪律；parser pool、arena、ref-count 等优化必须等
  profile 证明需要。

Tree-sitter 思想在 vimls-go 中的对应关系：

| Tree-sitter 思想 | vimls-go 实现 |
| --- | --- |
| 旧 tree 是 cache | 旧的不可变 `*syntax.File` |
| edit 使旧结果局部失效 | 比较新旧完整 source，求保守 byte dirty interval |
| subtree 带 parse state | unit 保存完整 scanner entry/exit state |
| lexer 有 lookahead | restart 前强制多退一个完整 unit |
| error node 少复用 | fragile unit 不得作为收敛 guard |
| 当前状态验证旧 subtree | bytes、scanner state、structure state 和 guard 全部匹配 |
| copy-on-write | clone/rebase 被复用节点，不修改旧树 |
| 无法证明时扩大范围 | 解析到 EOF 或 full `Parse` |

不实现：

- Tree-sitter runtime、生成式 grammar、GLR、external scanner 和 query language。
- 跨版本 stable node ID、`changed_ranges` 或公开复用统计。
- rope、piece table、arena、pool、`unsafe` packing 和通用增量框架。
- 第一阶段的增量 scope、type、reference、diagnostic delta 或 symbol index。

## 3. 当前实现基线

必须从当前代码事实出发：

| 位置 | 已有能力 | 增量实现缺口 |
| --- | --- | --- |
| `internal/text/snapshot.go` | 不可变 snapshot、顺序 change、UTF-8/16/32 转 byte offset | 每次 change 重建完整 string 和 line index，但这不是 parser 增量化的前置问题 |
| `internal/workspace/documents.go` | URI/version/revision、分析取消、configuration revision、`IsCurrent` | document 不保存 parser checkpoint |
| `internal/syntax/syntax.go` | 完整 `File`、byte span、独立 Legacy/Vim9 入口 | 只有全量 `Parse` |
| `internal/syntax/scanner.go` | dialect stack、`scriptversion`、continuation、heredoc、text body、keymap、嵌套命令和恢复 | parser 状态是局部变量，scan/structure/details/finish 交织 |
| `internal/syntax/blocks.go` | 全文件 block 构建和跨命令诊断 | 会改写 `Command.Block`、blocks 和部分恢复状态 |
| `internal/analysis` | 从完整 syntax tree 收集 scope、symbol、reference、type 和 diagnostics | 第一阶段仍全量运行，但必须只读 syntax cache |
| `internal/server/server.go` | `parsedDocument{revision,file}`、bounded workers、stale publish barrier | cache 当前包含 target/semantic diagnostics，cache miss 仍全量解析 |
| `internal/workspace/parse.go` | bounded full parsing | 无历史树时继续 full parse |

当前已经正确、不得重复建设：

- 文本 snapshot 已不可变。
- 同一 `didChange` 内的 changes 已按中间文本顺序应用。
- syntax span 已统一使用原始 source 的半开 byte offset。
- stale revision、configuration 和 workspace graph 结果已经不能覆盖新结果。
- parser worker 数已经限制为 `min(GOMAXPROCS, 4)`。

## 4. 原方案必须补齐的两个正确性边界

### 4.1 Scanner state 相同不等于完整 AST 可复用

当前 parser 的真实阶段是：

1. 扫描 source，产生 command headers、tokens 和 scanner diagnostics。
2. coalesce collected blocks，执行 `buildBlocks`，处理直接 `finish`。
3. 解析 expression、type、mapping、autocmd、substitute、nested command 等 details。
4. 执行 aggregate member 构建、diagnostic suppression、lambda source normalization
   和 token ordering。

`buildBlocks` 和 finish 会改写 `Command.Block`、`Block`、aggregate members、恢复标志和
诊断。因此后缀 source bytes 和 scanner entry/exit state 相同，只能证明扫描产物可
复用；完整 detail AST 还必须证明结构上下文相同。

实现必须分两道闸门：

- scanner convergence：允许复用 command header、token 和 scanner diagnostics。
- structure convergence：允许复用 expression/type/payload 等 detail AST。

### 4.2 JSON 相同不等于 AST 图等价

当前 AST 可能让 `Command.Expressions`、`Command.Targets` 等字段共享相同的
`*Expression`。analysis 使用节点指针作为 map key。普通 JSON 会把共享节点展开两次，
无法发现 clone 把一个共享节点错误复制成两个节点。

因此 differential oracle 除 JSON 外还必须比较：

- `*Expression`、`*Type`、lambda `*File`、`*CommandList` 的别名拓扑；
- `analysis.Analyze` 后的 declarations、references、types 和 diagnostics；
- 旧树在 `Reparse` 前后的可观察结果和别名拓扑。

## 5. 目标数据流

```text
LSP contentChanges
       │
       ▼
immutable text.Snapshot
       │ 完整新 source
       ▼
Reparse(old immutable File, new source)
       │
       ├─ source 相同 ───────────────────────► 返回 old pointer
       │
       ├─ metadata/self-check 不可信 ────────► Parse(new source)
       │
       ▼
最长公共 byte prefix/suffix -> dirty interval
       │
       ▼
选择 restart unit
  - 命中 owner unit
  - 再向前退一个完整 unit
  - 必要时从 byte 0 开始
       │
       ▼
从 checkpoint state 继续扫描
       │
       ├─ 不收敛 ───────────────────────────► 扫描到 EOF
       │
       └─ clean guard + scanner state 收敛
                         │
                         ▼
                  复用后缀扫描产物
                         │
                         ▼
                 全量重建 structure
                         │
       ┌─────────────────┴─────────────────┐
       │ structure state 相同              │ 不同
       ▼                                   ▼
clone/rebase 旧 detail AST             重跑 unit details
       └─────────────────┬─────────────────┘
                         ▼
                  全量执行 finish
                         │
                         ▼
                  pure syntax.File
                         │
                         ▼
analysis/compat/workspace diagnostics 使用局部副本
                         │
                         ▼
revision/config/graph 检查后发布
```

## 6. 最小内部数据模型

具体字段名可以在实现中微调，但下列信息不得缺失。

```go
type parserState struct {
    initialDialect Dialect
    activeDialect  Dialect
    scriptVersion  uint8
    vim9Prologue   bool
    lambdaBody     bool

    // equality 必须逐项比较；checkpoint 不得共享可变 backing array。
    dialectStack   []Dialect
    aggregateStack []BlockKind
}

type parseUnit struct {
    span  Span
    entry parserState
    exit  parserState

    firstCommand int
    commandCount int
    firstToken   int
    tokenCount   int

    scannerDiagnostics []Diagnostic
    detailDiagnostics  []Diagnostic

    structureEntry structureState
    structureExit  structureState
    fragile       bool
}
```

`structureState` 不保存易变的 block 数字索引，而保存会影响当前或后续 details 的
语义状态：

- 当前 block kind 路径；
- def/function/class/interface/enum 上下文；
- try/catch/finally、invalid-for 和 recovery 状态；
- interface/class method recovery；
- Vim9 `redir` 等会改变后续诊断的状态。

第一版仍全量执行 `buildBlocks`，只在其遍历过程中记录 unit 边界的 structure state。
不要在同一里程碑实现增量 block builder。

## 7. 安全语法单元

`parseUnit` 服从 Vim source reader 的所有权，不服从用户编辑行或任意 AST node：

- 普通 unit 从物理行开头开始，包含完整 logical line 和换行。
- 同一 logical line 上由 `|` 分隔的多个 command 属于同一 unit。
- heredoc owner、body 和 end marker 属于同一 unit。
- `append`、`change`、`insert` 的 owner、body 和 dot terminator 属于同一 unit。
- `loadkeymap` owner 和 body 属于同一 unit。
- Legacy/Vim9 continuation 不得被 checkpoint 切开。
- collected `command {}`、autocmd block 和 Legacy do block 与 owner 属于同一 unit。
- 直接 `finish` 与 `OpaqueTail` 组成最后一个 fragile unit。
- EOF 前每个 unit 至少消费一个 source byte。

以下状态未关闭时不能建立 checkpoint：

- active heredoc、text body 或 keymap body；
- unfinished Legacy logical continuation；
- unfinished Vim9 automatic/lambda continuation；
- 正在收集的 command/autocmd/legacy embedded block。

以下 unit 默认 fragile，可以产生 best-effort AST，但不能作为 suffix guard：

- 包含 recovery diagnostic；
- 未终止 string/list/dict/lambda/type/signature；
- 不完整 heredoc、text body、keymap 或 collected block；
- `finish` opaque tail；
- command boundary 依赖尚未序列化的隐藏状态。

未知用户命令或未来命令本身不是 fragile；只要其边界和 state 可证明，它仍是合法的
opaque unit。

## 8. Dirty interval、restart 和 convergence

### 8.1 Dirty interval

内部按最长公共 byte 前缀和最长公共 byte 后缀计算：

```go
type sourceChange struct {
    Start  int
    OldEnd int
    NewEnd int
}
```

规则：

- 比较 byte，不要求边界落在 UTF-8 rune boundary。
- 公共前缀和后缀不得重叠。
- 完全相同不创建 change，直接返回旧指针。
- 第一版允许 O(n) source comparison；只有 profile 证明它占增量总时间 15% 以上时，
  才考虑从 `internal/text` 传递 edit provenance。

### 8.2 Restart

固定顺序：

1. 找到与 dirty interval 相交、包含 `Start` 或紧邻 `Start` 的旧 unit。
2. 若命中 payload/body/continuation/collected block，退到 owner unit。
3. 再向前退一个完整 unit，吸收 command boundary 和 leading continuation lookahead。
4. 编辑可能改变首个有效 `vim9script` 时，从 byte 0 开始。
5. 编辑旧 `OpaqueTail` 时，从直接 `finish` unit 开始。
6. unit coverage、state 或 owner self-check 失败时 full `Parse`。

### 8.3 Scanner convergence

后缀扫描复用必须同时满足：

1. 新 parser 已越过 `NewEnd`，候选旧 unit 位于 `OldEnd` 之后。
2. `delta = NewEnd - OldEnd` 能唯一映射候选边界。
3. 新旧 unit 原始 source bytes 完全相同。
4. 新 candidate entry state 等于旧 candidate entry state。
5. 完整解析一个非 fragile guard unit。
6. guard 的 exit state、command headers、tokens 和 scanner diagnostics 相同。

hash 只能快速排除候选；命中后仍必须比较原始 bytes 和 state。

### 8.4 Structure convergence

复用 detail AST 还必须满足：

- 新旧 unit 的 `structureEntry` 和 `structureExit` 等价；
- 新 `buildBlocks` 得到的语义 block path 相同；
- guard command 的结构派生字段等价；
- unit 不含 recovery 或不完整 detail payload。

若 scanner 已收敛但 structure 未收敛，只重跑该 unit details，不必 full parse。

## 9. Clone/rebase 规则

新 File 不能直接拼接旧 slice，也不能原地修改旧 span。实现一个 package-private、
显式、identity-aware 的 clone/rebase 路径：

- 用 memo map 保留 `*Expression`、`*Type`、lambda `*File` 和 `*CommandList` 的共享
  关系。
- 深拷贝所有 slice、map 和可能被后处理写入的 pointer。
- 有效 span 的 Start/End 同时加 delta；零值 optional span 保持零值。
- string value 保持解析后的语义值，不能延迟从旧 source 取值。
- `Command.Block`、`Block`、aggregate `Members`、`detailsOpaque`、
  `hasNextStatement` 等派生字段由新 structure/finish 阶段重新建立。
- finished File 不依赖旧 `logical` 或 `boundaryExpression` 临时对象。

当前 `expression.go` 已有 identity-aware lambda rebase traversal。增量实现出现后，
这是第二个真实调用者，可以把它收敛成共享的具体 rebase traversal；不要引入通用
visitor interface。

测试可以使用反射枚举 AST 中的 `Span`、pointer、slice 和 map 字段，防止新增字段漏掉
clone coverage；生产 clone/rebase 不使用反射。

## 10. 串行实施任务

I0-I7 必须按顺序执行。I1-I5 都修改 `internal/syntax`，不得并发交给两个 write
agent。每个任务开始前检查 `git status`，只修改 Allowed paths；需要越界时停止并向
primary agent 报告缺失 contract。

### I0：Differential helpers 和 full-parse 基线

**Owner**

`language_worker`，只写测试。

**Goal**

建立后续 `Reparse` 共用的 observable、alias、analysis 比较器和 full-parse 性能基线，
不修改生产 parser。

**Allowed paths**

- `internal/syntax/incremental_test.go`（新文件）
- `internal/syntax/incremental_external_test.go`（新文件）
- `internal/syntax/benchmark_test.go`

**Forbidden paths**

- `internal/syntax` 下所有非测试文件
- `internal/text/`
- `internal/analysis/`
- `internal/workspace/`
- `internal/server/`
- `testdata/official/`
- `go.mod`、`go.sum`

**Required behavior**

- 比较 helper 接收一个 `func(*File, string) *File` 参数；I0 不能引用尚不存在的
  `Reparse`，也不能为了让测试编译而增加生产 stub。
- 使用 `encoding/json` 比较所有导出 syntax 字段。
- 增加 alias topology、span bounds、`File.Text(span)` 和 old-tree immutability helper。
- external test 使用 `analysis.Analyze` 比较 downstream 行为。
- 建立第 13 节编辑矩阵的数据表，但在 I3 前不调用 `Reparse`。
- benchmark 构造 source 和 edit 必须在计时区外。
- 记录 10 KiB、100 KiB、1 MiB Legacy/Vim9 五次 full-parse 基线。

**Validation**

```sh
gofmt -w internal/syntax/incremental_test.go \
  internal/syntax/incremental_external_test.go \
  internal/syntax/benchmark_test.go
go test -mod=readonly ./internal/syntax
go test -mod=readonly ./internal/syntax \
  -run '^$' -bench 'BenchmarkParseIncrementalCorpus' -benchmem -count=5
```

**Acceptance evidence**

- 测试/benchmark 数量；
- runner、Go、CPU、`GOMAXPROCS`；
- 五次原始 `ns/op`、`B/op`、`allocs/op` 和中位数。

### I1：把 full parser 拆成明确阶段和具体状态机

**Owner**

`language_worker`。

**Goal**

把 `parseSource` 局部状态移入具体 `sourceParser`，明确 scan、structure、details、
finish 阶段；`Parse` 的导出结果不变。

**Allowed paths**

- `internal/syntax/scanner.go`
- `internal/syntax/blocks.go`
- `internal/syntax/logical_map.go`
- `internal/syntax/expression.go`
- `internal/syntax/syntax.go`
- I0 测试文件

**Forbidden paths**

- 其它 production package
- `testdata/`
- docs
- generated files
- `go.mod`、`go.sum`

**Required behavior**

- `sourceParser` 只封装当前真实状态，不增加 interface、manager 或 service。
- Legacy/Vim9 command consumer 保持独立。
- coalescing 和 source ownership 在 structure 前完成。
- 保存 suppression 前的 scanner/detail diagnostics，最终顺序不变。
- full `buildBlocks` 和所有 finish pass 仍执行。
- details 映射完成后，finished commands 不再保存临时 logical objects。
- `Parse` 不调用 `Reparse`。
- 不实现旧树复用。

**Validation**

```sh
gofmt -w <changed-go-files>
go test -mod=readonly ./internal/syntax
go test -mod=readonly ./...
go test -mod=readonly -race ./...
go vet -mod=readonly ./...
```

**Acceptance evidence**

- official corpus、focused syntax tests 和 I0 helpers 全绿；
- full-parse benchmark 未越过 15% time/20% allocation 回退线。

### I2：记录 unit、checkpoint、fragile 和 structure state

**Owner**

`language_worker`。

**Goal**

让每次 full `Parse` 生成确定性的 private incremental metadata，尚不复用旧树。

**Allowed paths**

- `internal/syntax/incremental.go`（新文件）
- `internal/syntax/scanner.go`
- `internal/syntax/blocks.go`
- `internal/syntax/syntax.go`
- I0 测试文件

**Forbidden paths**

- 其它 production package
- `testdata/`
- docs
- generated files
- `go.mod`、`go.sum`

**Required behavior**

- unit 满足第 7 节所有 source ownership 规则。
- state 包含 `aggregateStack`，不能用单一 open-enum Boolean 代替。
- checkpoint 不跨 continuation/payload/collected block。
- `buildBlocks` 全量运行并记录 unit structure state。
- metadata 不出现在 JSON，不改变 public File contract。
- self-check 覆盖顺序、coverage、前进、state round trip、owner 和 span bounds。
- 相同 source 的多次 `Parse` 产生相同 unit/state。

**Validation**

```sh
gofmt -w <changed-go-files>
go test -mod=readonly ./internal/syntax \
  -run 'Test(ParseUnit|Checkpoint|StructureState|Incremental)'
go test -mod=readonly ./...
go test -mod=readonly -race ./...
go vet -mod=readonly ./...
```

**Acceptance evidence**

- 代表性 Legacy/Vim9/payload source 的 unit 边界、entry/exit 和 fragile 白盒断言；
- public `Parse` 输出仍不变。

### I3：实现只复用安全前缀的 `Reparse`

**Owner**

`language_worker`。

**Goal**

新增 `Reparse`，复用 restart 前的扫描产物，从 restart 扫描到 EOF；structure、
details 和 finish 仍全量执行。

**Allowed paths**

- `internal/syntax/incremental.go`
- `internal/syntax/scanner.go`
- `internal/syntax/syntax.go`
- I0 测试文件

**Forbidden paths**

- 其它 production package
- `testdata/`
- docs
- generated files
- `go.mod`、`go.sum`

**Required behavior**

- API 严格为 `Reparse(previous *File, source string) *File`。
- nil previous、相同 source、dirty interval、restart 和 fallback 遵守第 8 节。
- 克隆前缀扫描产物，不共享可写 backing arrays。
- 不复用 detail AST，不实现 suffix convergence。
- 旧树不可变。
- 每个编辑 case 与 clean `Parse` 等价。
- 白盒计数证明尾部编辑未扫描 restart 前 units。

**Validation**

```sh
gofmt -w <changed-go-files>
go test -mod=readonly ./internal/syntax -run 'Test(Reparse|Incremental)'
go test -mod=readonly ./...
go test -mod=readonly -race ./...
go vet -mod=readonly ./...
go test -mod=readonly ./internal/syntax \
  -run '^$' -bench 'Benchmark(Parse|Reparse)IncrementalCorpus' -benchmem -count=5
```

**Acceptance evidence**

- 全编辑矩阵等价；
- 尾部小编辑 time 和 allocation 均低于 full parse。

### I4：实现 scanner guard 和后缀扫描复用

**Owner**

`language_worker`。

**Goal**

在 bytes、entry/exit state 和一个 clean guard unit 全部收敛后，复用旧后缀的扫描
产物；details 仍全量执行。

**Allowed paths**

- `internal/syntax/incremental.go`
- `internal/syntax/scanner.go`
- I0 测试文件

**Forbidden paths**

- `internal/syntax/blocks.go` 的增量重构
- 其它 production package
- `testdata/`
- docs
- generated files
- `go.mod`、`go.sum`

**Required behavior**

- 遵守第 8.3 节全部 convergence 条件。
- hash 不能替代 raw bytes/state 比较。
- fragile unit 不能作为 guard。
- equal-length/non-equal-length edit 都可重定位后缀扫描产物。
- 仍可退化为 I3 的 prefix-to-EOF 或 full `Parse`。
- 白盒计数证明 guard 后 units 未进入 scanner。

**Validation**

```sh
gofmt -w <changed-go-files>
go test -mod=readonly ./internal/syntax \
  -run 'Test(Reparse|Convergence|Incremental)'
go test -mod=readonly ./...
go test -mod=readonly -race ./...
go vet -mod=readonly ./...
```

**Acceptance evidence**

- 中部编辑扫描 work 与 dirty units 相关；
- clean guard、fragile guard 和 state mismatch 均有白盒证据。

### I5：实现 structure convergence 和完整 AST clone/rebase

**Owner**

`language_worker`。

**Goal**

在 scanner 与 structure context 都收敛时复用 detail AST；否则只重跑受影响 unit
details。

**Allowed paths**

- `internal/syntax/incremental.go`
- `internal/syntax/scanner.go`
- `internal/syntax/blocks.go`
- `internal/syntax/expression.go`
- `internal/syntax/syntax.go`
- I0 测试文件

**Forbidden paths**

- 其它 production package
- `testdata/`
- docs
- generated files
- `go.mod`、`go.sum`

**Required behavior**

- full `buildBlocks` 仍运行，并产生新树自己的 block indexes。
- detail reuse 遵守第 8.4 节。
- clone/rebase 遵守第 9 节并保留 alias topology。
- suffix 所有 nested spans 应用 delta。
- structure/global finish diagnostics 始终重新计算。
- 新增 AST span/pointer/slice 字段时 coverage test 必须失败。
- 旧树可被多个 concurrent `Reparse` 安全读取。

**Validation**

```sh
gofmt -w <changed-go-files>
go test -mod=readonly ./internal/syntax \
  -run 'Test(Reparse|Rebase|Alias|StructureConvergence|Incremental)'
go test -mod=readonly ./...
go test -mod=readonly -race ./...
go vet -mod=readonly ./...
go test -mod=readonly ./internal/syntax \
  -run '^$' -bench 'Benchmark(Parse|Reparse)IncrementalCorpus' -benchmem -count=5
```

**Acceptance evidence**

- observable JSON、alias graph 和 `analysis.Analyze` 结果全部等价；
- profile 证明 guard 后 command details 未重新执行；
- 中部和尾部编辑达到第 14 节性能门槛。

### I6：接入 open-document server cache

**Owner**

`server_worker`，必须等 I5 syntax API 冻结。

**Goal**

让后台分析和 open-document 同步请求使用最近一次纯净 syntax AST 作为 `Reparse`
输入，同时保持现有 stale barriers。

**Allowed paths**

- `internal/server/server.go`
- `internal/server/structure.go`
- `internal/server/navigation.go`
- `internal/server/workspace_navigation.go`
- `internal/server/rename.go`
- `internal/server/language_features.go`
- `internal/server/server_test.go`
- `internal/server/navigation_test.go`
- `internal/server/document_sync_test.go`
- `test/integration/lsp_subprocess_test.go`

**Forbidden paths**

- `internal/syntax/`
- `internal/text/`
- `internal/analysis/`
- `internal/workspace/`
- docs
- testdata
- generated files
- `go.mod`、`go.sum`

**Required behavior**

- 在 `publishMu` 下取得旧 cache pointer，释放锁后调用 `Reparse`。
- `parsedDocument.file` 只保存 `Parse/Reparse` 的纯 parser 结果。
- compatibility、semantic、workspace、truncation diagnostics 只写本次分析的局部 File
  或局部 slice。
- 若 analysis 必须看 compatibility diagnostics，浅拷贝 File 并复制 Diagnostics；
  AST 仍共享且只读。
- target version 改变但 source 不变时返回同一 syntax pointer，只重算诊断。
- publish cache 前检查 snapshot/revision/configuration；发布诊断前再检查 graph
  revision。
- canceled/stale parse 可以完成，但不能缓存或发布。
- close 删除 cache；重复 didOpen 不跨 document lifetime 复用。
- 超过 4 MiB 时不解析，也不把 `vimls/file-too-large` 写入 parser cache。
- 所有 open-document cache-miss Parse 调用汇入一个 package-private
  `parseSnapshot` 函数；不增加 manager/service。
- `workspace.ParseSources` 和无历史的 workspace batch 保持 full parse。

**Validation**

```sh
gofmt -w <changed-go-files>
go test -mod=readonly ./internal/server
go test -mod=readonly -run TestLSPSubprocess ./test/integration
go test -mod=readonly ./...
go test -mod=readonly -race ./...
go vet -mod=readonly ./...
```

**Acceptance evidence**

- 连续 edits、分析取消、close/reopen、save、configuration change 和 graph change
  都不能发布旧结果；
- cache 中只有 parser diagnostics；
- 同步请求早于后台 worker 时也使用 `Reparse`；
- 第二次及以后的小编辑进入增量路径。

### I7：Fuzz、official corpus 回放和最终性能闸门

**Owner**

`qa_reviewer` 先做只读审查；发现问题退回 I1-I6 对应 owner。需要补测试时由明确的
write agent 完成。

**Goal**

证明任意编辑序列下的长期等价、无 panic/越界/非终止、无 data race，并冻结可重复
性能证据。

**Allowed paths**

- `internal/syntax/incremental_test.go`
- `internal/syntax/incremental_external_test.go`
- `internal/syntax/benchmark_test.go`
- `internal/server/server_test.go`

**Forbidden paths**

- production code；发现问题必须退回 owning phase
- `testdata/official/`
- generated files
- `go.mod`、`go.sum`

**Required behavior**

- `FuzzIncrementalParse` 生成合法 byte edits，每一步和 clean Parse 比较。
- seed 覆盖第 13 节高风险状态。
- 保留至少一个 committed 100-step deterministic regression sequence。
- 对 embedded official corpus 回放 head/middle/tail insert/delete/replace。
- 多 goroutine 从同一旧 File 执行不同 Reparse，race 下安全。
- fallback 次数有界；不能用最终 full Parse 覆盖错误的 incremental 输出。
- 五次 benchmark 达到第 14 节预算；失败时提供 profile，不直接放宽门槛。

**Validation**

```sh
gofmt -w <changed-go-files>
go test -mod=readonly ./...
go test -mod=readonly -race ./...
go vet -mod=readonly ./...
go test -mod=readonly ./internal/syntax \
  -run '^$' -fuzz '^FuzzIncrementalParse$' -fuzztime=30s
go test -mod=readonly ./internal/syntax \
  -run '^$' -bench 'Benchmark(Parse|Reparse)IncrementalCorpus' -benchmem -count=5
```

**Acceptance evidence**

- full gate、race、fuzz、official replay 输出；
- 五次原始 benchmark 和 profile；
- Definition of Done 逐项签核。

## 11. Server cache 纯度设计

当前 `analyzeDocument` 会把 compatibility 和 semantic diagnostics 追加到同一个 File，
然后交给 `prepareSyntax` 缓存。增量接入前必须拆开：

```go
syntaxFile := syntax.Reparse(previousSyntaxFile, snapshot.Text())

// analysisFile 是本次分析视图；syntaxFile 保持纯净和只读。
analysisFile := *syntaxFile
analysisFile.Diagnostics = append(
    append([]syntax.Diagnostic(nil), syntaxFile.Diagnostics...),
    syntax.CompatibilityDiagnostics(syntaxFile, target)...,
)

fileAnalysis := analysis.Analyze(&analysisFile)
diagnostics := analysis.CombinedDiagnostics(&analysisFile, versionedAnalysis)
diagnostics = append(diagnostics, importDiagnostics...)
diagnostics = append(diagnostics, workspaceDiagnostics...)
sortDiagnostics(diagnostics)
diagnostics = capDiagnostics(diagnostics)
```

缓存和发布是两件事：

- cache：`syntaxFile`，只含 parser AST 和 parser diagnostics；
- publish：本次 snapshot/target/config/graph 对应的局部 diagnostics。

syntaxFile 构建完成前不持 server 锁；发布前继续执行现有 stale 检查。

## 12. 磁盘 workspace 文件变化

I0-I7 只交付 open-document incremental AST。`workspace/didChangeWatchedFiles` 还涉及
文件发现、Index 和 ImportGraph 失效，不能与 parser 增量化混在同一个 patch。

完成 I7 后，若需要让关闭文件也增量重建，按以下独立顺序实施：

### W0：事件治理

- 只接受 workspace/runtime roots 内相关 Vim 文件。
- 300-500ms generation debounce，同一路径事件合并。
- 过量事件放弃逐文件增量，只触发一次全量 rebuild。
- 保留 watcher 注册后的再扫描，避免无监听窗口。

### W1：内存文件身份和 parser cache

- 保存 `path -> source identity + immutable syntax.File + facts`。
- added 使用 `Parse`；changed 使用 `Reparse(oldFile, newSource)`；removed 删除。
- 只用标准库内容哈希或直接 source equality，不新增 hash 依赖。
- cache 条目只读，不保存发布 diagnostics。
- 不做持久磁盘 cache、TTL/LRU framework 或 ref-count。

### W2：原子增量 reindex

- changed/added 使用 `Index.Replace`、`ImportGraph.Replace`。
- removed 使用 `Index.Remove`、`ImportGraph.Remove`。
- 重新分析受影响 importer 和 reverse dependents。
- Index、Graph 和 resolver 以同一 revision 发布。
- 每个 fixture 同时走增量和全量 rebuild，逐项比较 paths、facts、edges 和 missing
  import 语义。

W0-W2 不属于 syntax `Reparse` 的 Definition of Done；它们只有在 open-document
增量路径稳定并有 workspace benchmark 证据后开始。

## 13. 必测编辑矩阵

每类至少覆盖 insert、delete、equal-length replace、长度变化 replace 和连续编辑。

### 文本和位置

- 文件头、中间、EOF；
- UTF-8 多字节、astral、combining character、tab；
- LF、CRLF、BOM、无末尾换行；
- whole-document replacement；
- 连续 100 次小编辑。

### 方言和 scanner state

- 新增、删除、移动首个有效 `vim9script`；
- legacy root 中的 `def`，Vim9 root 中的 `function`；
- `vim9cmd`、`legacy` 只影响下一 command；
- `scriptversion` 1-4 和无效版本恢复；
- mismatched `enddef`/`endfunction` 后继续解析。

### Command boundary 和 lookahead

- range、modifier、abbreviation、bang、register、count；
- `|` 位于普通 argument、regexp、mapping、substitute、string 和 comment；
- 下一行变成或不再是 Vim9 leading continuation；
- Legacy `\` continuation；
- Vim9 operator、括号、ternary、lambda continuation。

### 多行 owner

- 完整/不完整 heredoc，编辑 marker/body/end marker；
- append/change/insert body 和 dot terminator；
- `loadkeymap` body；
- command/autocmd block、global/vglobal、list-do、Legacy embedded block；
- nested embedded command 深度上限附近；
- 直接/conditional `finish` 和 `OpaqueTail`。

### Structure state

- 在 `if/try/for/while` 前后插入/删除 opener/closer；
- edit 改变 catch-all、multiple finally 或 invalid-for recovery；
- class/interface method recovery；
- Vim9 `redir` open/close；
- block 数字 index 改变但语义 block path 不变；
- scanner state 收敛但 structure state 不收敛。

### 错误恢复

- 未闭合 string/list/dict/type/signature/lambda；
- malformed command 后同一行 `|` 和下一行有效 declaration；
- unknown user/future command；
- fragile -> clean 和 clean -> fragile。

### 生命周期和并发

- didChange during analysis；
- 多个中间 revision 被取消；
- didSave whole content；
- close/reopen 和重复 didOpen；
- target/config/graph revision 变化；
- 同一旧 File 被多个 goroutine 并发 Reparse。

## 14. 性能验收

遵守 `docs/testing.md` 的既有预算：在固定 runner 上，经确认的 median/p95 时间回退
不得超过 15%，allocation 回退不得超过 20%，除非单独记录并批准原因。

固定 benchmark：

- 10 KiB、100 KiB、1 MiB；
- Legacy 和 Vim9 分开；
- 文件前 10%、中间、后 10% 的一字符 insert；
- equal-length replace；
- heredoc marker、Vim9 continuation 和 fragile edit；
- source 不变和强制 full fallback。

硬门槛：

| 场景 | 门槛 |
| --- | --- |
| source 不变 | 返回旧 pointer，不扫描，不新增 AST allocation |
| 尾部小编辑 | restart 前 unit 不进入 scanner/details；time、allocation 低于 full parse |
| 中部小编辑 | guard 后 unit 不进入 scanner/details；time、allocation 低于 full parse |
| 强制 fallback | 相对 full parse 的 time 回退 <= 15%，allocation 回退 <= 20% |
| 全部场景 | 达到 I5 冻结的绝对 median/p95 预算，五次样本无持续退化 |

记录：Go 版本、OS/CPU、`GOMAXPROCS`、corpus/tag、source size、edit offset、五次
`ns/op`、`B/op`、`allocs/op`、中位数和主要 profile cost center。

只有 profile 命中以下阈值才建立后续任务：

- `buildBlocks` >= 增量 CPU 30%：评估增量 block state 重建。
- clone/rebase >= 增量 CPU 或 allocation 30%：评估 unit-relative span/materialization。
- source prefix/suffix comparison >= 增量时间 15%：评估 edit provenance。
- snapshot string/indexLines 主导 1 MiB 编辑：单独评估 rope/piece table。
- canceled parser CPU 明显影响交互：只在 unit boundary 增加 cancellation hook。

## 15. 每个 patch 的审查清单

### Correctness

- [ ] `Parse(new)` 与 `Reparse(old,new)` 的导出结果相同。
- [ ] alias topology 和 downstream analysis 结果相同。
- [ ] 旧 File 在调用前后不变。
- [ ] 所有 span 属于新 source 且有界。
- [ ] scanner state 和 structure state 都进入收敛判断。
- [ ] payload/continuation/collected block 只有 owner boundary。
- [ ] fragile path 保守扩大或 fallback。

### Reuse

- [ ] restart 包含一个完整 lookbehind unit。
- [ ] guard 比较 bytes、entry/exit、scanner output。
- [ ] detail reuse 额外比较 structure state。
- [ ] clone 保留共享节点关系。
- [ ] suffix delta 作用到所有 nested spans。
- [ ] 白盒证据证明复用 unit 未重新执行相应阶段。

### Lifecycle

- [ ] parser 在 server 锁外执行。
- [ ] cache tree 只读且只有 parser diagnostics。
- [ ] target/config change 不污染 syntax cache。
- [ ] stale/canceled/config-old/graph-old 结果不能缓存或发布。
- [ ] close/reopen 不跨 document lifetime 复用。
- [ ] workspace batch 无历史时仍 full parse。

### Performance

- [ ] fixture 构造在 benchmark 计时区外。
- [ ] 报告五次 time/bytes/allocs。
- [ ] 与同一最终文本的 full Parse 比较。
- [ ] 不通过删除 AST、token、diagnostic 或 corpus 提速。
- [ ] 未达标先 profile，不先加 pool/arena/rope。

## 16. Commit 和验证纪律

- I0-I7 每个任务一个 milestone-scoped local commit；不授权 push。
- 每个 Go patch 修改后先请求 gopls diagnostics。
- 每次提交前运行 `/ponytail-review` 并解决 findings。
- review 通过后用 `VIMLS_PONYTAIL_REVIEWED=1 git commit ...`。
- 只 stage 当前任务 Allowed paths，保留其它 tracked/untracked 工作。
- 每阶段至少运行 narrow tests；提交前运行相关 full test/race/vet gate。
- I7 完成后才更新 `docs/architecture.md`、`docs/testing.md`、
  `docs/roadmap.md` 和实际 language-support 状态。

## 17. Definition of Done

- [ ] `syntax.Parse` 保持独立 full-source oracle。
- [ ] `syntax.Reparse` 已实现且无新依赖。
- [ ] parser state 覆盖所有影响下一 unit 的当前状态。
- [ ] structure state 覆盖所有影响 details 的当前 block/recovery 状态。
- [ ] unit 不切开 continuation、payload 或 collected block。
- [ ] prefix reuse、scanner guard、structure convergence、suffix clone/rebase 均有白盒测试。
- [ ] observable JSON、alias topology 和 analysis 输出都与 clean Parse 等价。
- [ ] 旧树不可变并通过 concurrent race 测试。
- [ ] 30 秒 fuzz、100-step edits 和 official corpus 回放通过。
- [ ] server 缓存纯 syntax AST，stale barriers 不回归。
- [ ] `go test ./...`、`go test -race ./...`、`go vet ./...` 通过。
- [ ] 100 KiB/1 MiB benchmark 达到第 14 节预算。
- [ ] workspace batch 和 public LSP behavior 无虚假变化。
- [ ] 文档只按已实现事实更新。

只满足“结果正确但每次都 full parse”不算完成；只满足“benchmark 变快但 JSON、alias
或 analysis 不等价”也不算完成。
