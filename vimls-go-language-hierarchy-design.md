# vimls-go：Language Hierarchy 与 Implementation 设计

> 设计基线：`main` 为 `47f1871`
> 审核日期：2026-09-01
> 协议目标：Language Server Protocol 3.18
> Vim 语义上限：Vim v9.2.1015
> 实现状态：7 个标准方法、关系索引和验证路径已完成

本文保留实现契约和取舍依据。当前 `docs/language-support.md` 已同步列出 Call
Hierarchy、Type Hierarchy 和 Implementation lookup 的可用范围。

本次审核直接使用当前 Go 模块、`go.lsp.dev/protocol v1.0.1` 的生成代码、
本地 Vim v9.2.1015 源码与项目测试，不再沿用旧稿中“zip 快照、无 Go
toolchain”的前提。

## 1. 结论

三项能力按以下顺序实现：

1. **Type Hierarchy**：增加直接 `extends` / `implements` 事实和反向索引。
2. **Implementation**：复用类型关系，补充成员 provider 的精确验证。
3. **Call Hierarchy**：最后增加 caller 归属、静态调用事实和反向调用索引。

当前项目已经具备大部分基础：

- AST、token、block 和 expression 都保留原始 byte span；
- `analysis.FileAnalysis` 已有作用域、声明、引用和保守类型推断；
- `workspace.Index` 已保留源码、符号事实、外部引用、revision 和完整性状态；
- `workspace_navigation.go` 已能解析 import、autoload、legacy global 和 imported
  member，并处理 open-buffer overlay；
- foreground workspace 请求已经采用“捕获 identity、锁外计算、检查陈旧、最多
  重试一次”的一致性模型；
- pinned `go.lsp.dev/protocol v1.0.1` 已生成全部 7 个请求方法和 capability 类型。

实施前真正缺少的是两类可索引事实：

- aggregate 之间的直接类型关系；
- 具名 callable 之间可静态证明的调用关系。

不要为此增加新的顶层 package、Manager、Service、Registry 或通用图框架。
第一版只需要扩展现有 analysis/workspace 数据，并在 server 中复用现有导航路径。

## 2. 实施前代码事实

| 现状 | 影响 |
| --- | --- |
| `CollectSymbolFacts()` 会递归扁平化 `analysis.Symbol.Children` | member fact 丢失 owner，无法仅凭 fact 判断它属于哪个 class/interface/function。 |
| `SymbolFact` 没有 `abstract`、`static` 元数据 | Implementation 无法仅从索引粗筛有效 provider。 |
| `Index` 有 `byName`、`byExternalName` 等反向表，但没有类型边和调用边 | subtype、implementation、incoming call 不能做有界候选查询。 |
| `Index.Replace()` 当前通过 external-reference 收集间接执行一次 `analysis.Analyze(file)` | 新关系收集必须显式复用同一份 analysis，不能再重复分析。 |
| `Reference.functionCallee` 只区分 identifier 是否位于直接调用槽位 | 它没有 caller owner，也不能完整表达 member、constructor、lambda/deferred 边界。 |
| `memberNavigationSymbols()` 和 workspace member navigation 已有保守的本地/跨文件目标解析 | 应复用这些现有路径，不应先搬迁或重写整个成员诊断系统。 |
| `workspaceNavigationSnapshot` 已捕获 resolver、index、roots 和 workspace identity | hierarchy 查询应沿用它；只有出现真实重复后再泛化名称。 |

路线图和用户文档现已按实际 capability 同步；每项 capability 都在 handler、索引、
陈旧检查和 stdio round-trip 完成后广告。

## 3. LSP 3.18 契约

### 3.1 七个请求

| 功能 | LSP 方法 | 当前依赖中的 Go 签名 |
| --- | --- | --- |
| Implementation | `textDocument/implementation` | `Implementation(context.Context, *protocol.ImplementationParams) (protocol.DefinitionResult, error)` |
| Call prepare | `textDocument/prepareCallHierarchy` | `PrepareCallHierarchy(context.Context, *protocol.CallHierarchyPrepareParams) ([]protocol.CallHierarchyItem, error)` |
| Incoming calls | `callHierarchy/incomingCalls` | `IncomingCalls(context.Context, *protocol.CallHierarchyIncomingCallsParams) ([]protocol.CallHierarchyIncomingCall, error)` |
| Outgoing calls | `callHierarchy/outgoingCalls` | `OutgoingCalls(context.Context, *protocol.CallHierarchyOutgoingCallsParams) ([]protocol.CallHierarchyOutgoingCall, error)` |
| Type prepare | `textDocument/prepareTypeHierarchy` | `PrepareTypeHierarchy(context.Context, *protocol.TypeHierarchyPrepareParams) ([]protocol.TypeHierarchyItem, error)` |
| Supertypes | `typeHierarchy/supertypes` | `Supertypes(context.Context, *protocol.TypeHierarchySupertypesParams) ([]protocol.TypeHierarchyItem, error)` |
| Subtypes | `typeHierarchy/subtypes` | `Subtypes(context.Context, *protocol.TypeHierarchySubtypesParams) ([]protocol.TypeHierarchyItem, error)` |

Implementation 从 LSP 3.6 开始提供，Call Hierarchy 从 3.16 开始提供，Type
Hierarchy 从 3.17 开始提供。Call/Type Hierarchy 都是惰性的一层查询：prepare
返回 item，客户端随后请求该 item 的直接父/子或 incoming/outgoing 关系。

Implementation 第一版返回 `protocol.LocationSlice` 即可。它是
`protocol.DefinitionResult` 的合法分支，并与当前 Definition/Declaration 的返回形状
一致；暂不需要处理可选的 `linkSupport`。

### 3.2 Capability 和 allowlist

每项功能都在对应 handler、wire test 和陈旧检查完成后广告：

```go
ImplementationProvider: protocol.Boolean(true),
CallHierarchyProvider:   protocol.Boolean(true),
TypeHierarchyProvider:   protocol.Boolean(true),
```

`implementedMethod()` 同步加入：

```go
protocol.MethodTextDocumentImplementation,
protocol.MethodTextDocumentPrepareCallHierarchy,
protocol.MethodCallHierarchyIncomingCalls,
protocol.MethodCallHierarchyOutgoingCalls,
protocol.MethodTextDocumentPrepareTypeHierarchy,
protocol.MethodTypeHierarchySupertypes,
protocol.MethodTypeHierarchySubtypes,
```

不要一次性广告三项能力后再逐步补 handler。`protocol.UnimplementedServer` 仍会对
未实现请求返回错误，产生“capability 声称支持但实际不可用”的协议不一致。

### 3.3 空结果和错误

| 情况 | 契约 |
| --- | --- |
| 光标下没有适用符号 | 返回非 nil 空 slice。 |
| 目标动态、歧义或无法静态证明 | 返回空 slice，不猜测。 |
| hierarchy item 的 `data` 缺失、格式错误或与 item 字段冲突 | 返回空 slice，不使用客户端提供的 URI/span 读取文件。 |
| `data` 有效，但对应 source content 已改变 | 返回 `protocol.ErrContentModified`，要求客户端重新 prepare。 |
| context 已取消 | 返回 `protocol.ErrRequestCancelled`。 |
| 查询期间 workspace identity 改变 | 重试一次；第二次仍变化则返回 `ErrContentModified`。 |
| subtype、incoming 或 implementation 所需的关系索引不完整 | 返回 code 为 `LSPErrorCodesRequestFailed` 的 JSON-RPC error，不能伪装成完整空结果。 |
| 结果超过硬上限 | 返回 `RequestFailed`，不能静默截断。 |

Supertypes 和 outgoing calls 是从一个已知节点读取正向事实；当目标来自当前 open
snapshot 时，可以直接从该 snapshot 计算。Subtypes、incoming calls 和
implementation 是闭世界反向查询，必须先确认关系索引完整。

## 4. Vim 语义契约

LSP 只定义 wire shape，不定义 Vim 中什么算 implementation 或静态调用。以下规则
必须作为产品语义固定，并用 Vim v9.2.1015 证据和项目测试锁定。

### 4.1 Type Hierarchy

只有 class、interface 和 enum 是 hierarchy item。type alias 不是独立节点；当现有
分析能唯一证明它最终指向 aggregate 时，可透明解析到该 aggregate。class
typealias、imported typealias 以及它们能否出现在具体 `extends`/`implements` 位置，
必须分别用 v9.2.1015 官方测试或 clean-Vim oracle 验证，不能从普通类型注解规则外推。

直接父类型：

| 当前类型 | `supertypes` |
| --- | --- |
| class | 直接 `extends` class，以及直接 `implements` interface。 |
| interface | 直接 `extends` interface。 |
| enum | 直接 `implements` interface。 |

直接子类型：

| 当前类型 | `subtypes` |
| --- | --- |
| class | 直接 `extends` 它的 class。 |
| interface | 直接 `extends` 它的 interface，以及直接 `implements` 它的 class/enum。 |
| enum | 空。 |

prepare 支持：

- aggregate 声明名；
- 静态解析到 aggregate 的引用；
- `extends` / `implements` 中的类型名；
- 唯一解析的 import-qualified aggregate；
- 经 v9.2.1015 验证且能唯一解开的 aggregate typealias。

每次 supertypes/subtypes 只返回一层，不在 server 内展开传递闭包。动态 import、alias
环、同名歧义、`unknown` 或受未恢复诊断影响的名称返回空结果。

本地 clean Vim v9.2.1015（patch 1-1015）验证了 class alias 可用于 `extends`，
interface alias 可用于 `implements`；官方 `test_vim9_typealias.vim` 同时覆盖 exported
class 的 imported alias。实现因此只透明展开唯一解析的 named aggregate alias。

### 4.2 Implementation

第一版采用单向、保守语义：

| 光标目标 | 返回结果 |
| --- | --- |
| interface | 声明为其 subinterface 或 implementor 的 aggregate；沿有效关系传递查找。 |
| abstract class | 非 abstract descendant class。 |
| concrete class / enum | 空；父子关系由 Type Hierarchy 提供。 |
| interface method/variable | 每个相关类型中能够证明兼容的 effective provider。 |
| abstract method | descendant 中的 concrete provider。 |
| concrete、可覆盖的方法 | descendant 中显式声明的 override。 |
| constructor、普通函数、常量、enum value | 空。 |

类型级结果以源码中的名义 `extends` / `implements` 关系为准。即使编辑中的 class
尚有“未完整实现接口”的诊断，也可以返回该类型声明，帮助用户导航；成员级结果则
必须实际证明 provider 有效。

成员 provider 至少验证：

1. member 名称和类别一致，method 不能由 variable 代替；
2. object/static 属性一致；
3. access 规则符合 Vim v9.2.1015；
4. 参数、可选参数、variadic、泛型和返回类型兼容；
5. inherited concrete member 可以满足接口，结果指向真正提供该 member 的声明；
6. 多个 descendant 共用同一 inherited provider 时按符号身份去重；
7. abstract provider 不是最终结果，但不阻止继续搜索 descendant。

第一版不在 concrete class 上反向返回它实现的接口，也不在 concrete member 上返回
上层声明；Type Hierarchy 和 Declaration 已分别覆盖这些方向。

### 4.3 Call Hierarchy

第一版 callable：

- named Legacy/Vim9 function；
- class/interface method；
- 有真实声明 span 的显式 constructor。

隐式默认 constructor 没有独立源码声明，不在第一版制造 synthetic item。lambda、
top-level script、autocmd/mapping/user-command deferred body 也暂不作为 synthetic
caller；其中的调用不能错误归属到外层 named function。

收录的静态调用：

- 同文件唯一解析的直接函数调用；
- 唯一解析的 imported exported function；
- 唯一解析的 global/autoload function；
- 静态 receiver 类型可证明的 method/constructor call；
- generic direct call；
- `defer Foo()` 中仍由正常 call AST 表达、且 caller 归属明确的调用。

排除：

- `execute()`、`eval()` 和字符串拼接形成的名称；
- 无法证明未被重新赋值的 Funcref/partial 调用；
- 动态 dictionary member call；
- builtin function，因为它没有工作区源码 URI；
- 无稳定 caller item 的 lambda、script 和 deferred command body。

interface-typed receiver 的 method call 指向 interface method declaration，不扇出到所有
concrete implementation。Call Hierarchy 表达静态调用图；可能的运行时实现由
Implementation 单独回答。

prepare 规则：

1. callable 声明名或静态 callable 引用命中时，返回该 callable；
2. 静态 call target 命中时，返回被调用方；
3. 否则返回最内层 named enclosing callable；
4. 光标位于没有独立 item 的 lambda/deferred body 时返回空，不回退到外层函数。

Incoming/Outgoing 都按对端 SymbolKey 分组。`FromRanges` 始终位于 caller 文件，保留
全部调用点并稳定排序、去重。

## 5. 最小数据模型

不需要旧稿中的通用 `TargetFact` tagged union。索引已经保留完整 source，server 也已
有本地和跨文件导航解析。关系事实只保存候选键和原始 span；请求阶段再用当前导航逻辑
精确确认目标，避免复制 import/autoload/member 解析规则。

### 5.1 SymbolKey

```go
type SymbolKey struct {
    Path           string
    SelectionRange syntax.Span
    Kind           analysis.SymbolKind
}
```

`Path` 必须是 index 使用的 canonical path。名称不能单独作为身份；同名符号可存在于
不同文件和作用域。LSP position 也不能作为身份，因为客户端协商的编码可能不同。

给现有 `SymbolFact` 增加当前查询真正需要的元数据：

```go
OwnerSelectionRange syntax.Span
Abstract            bool
Static              bool
```

top-level owner 使用零 span。访问级别和完整签名兼容性不复制进 fact，查询时从当前
source 精确验证。

### 5.2 TypeRelationFact

```go
type TypeRelationKind uint8

const (
    TypeRelationExtends TypeRelationKind = iota + 1
    TypeRelationImplements
)

type TypeRelationFact struct {
    Child      SymbolKey
    ParentName string
    ParentSpan syntax.Span
    Kind       TypeRelationKind
}
```

`ParentName` 只用于反向候选缩小。`ParentSpan` 指向原始父类型名称；最终匹配必须从该
span 重新走本地/import/typealias 导航并与目标 SymbolKey 比较，不能只比较名称。

索引增加：

```go
typeRelationsByChild      map[SymbolKey][]TypeRelationFact
typeRelationsByParentName map[string][]TypeRelationFact
```

### 5.3 TypeAliasFact

反向 subtype 查询还需要从 aggregate 找到指向它的 alias。别名事实同样只保存候选名
和可重新解析的目标 span：

```go
type TypeAliasFact struct {
    Alias      SymbolKey
    AliasName  string
    TargetName string
    TargetSpan syntax.Span
}
```

`aliasesByTarget` 只索引 named type 的最终名称。查询从目标 aggregate 开始，逐层验证
alias 的 `TargetSpan` 最终解析到同一 SymbolKey，再将 alias 名加入父类型候选；cycle 用
Alias SymbolKey 截断。list、dict、func 等 compound alias 不进入该索引。

### 5.4 CallFact

```go
type CallFact struct {
    Caller     SymbolKey
    CalleeName string
    CalleeSpan syntax.Span
}
```

`CalleeSpan` 是可导航的函数/member 名称 span，不是整个 call expression。最终目标同样
在请求阶段精确解析。

索引增加：

```go
callsByCaller     map[SymbolKey][]CallFact
callsByCalleeName map[string][]CallFact
```

反向表只生成候选，不是权威关系。任何 incoming/implementation/subtype 结果都必须做
精确目标确认。

## 6. Analysis 与事实收集

关系收集应保持协议无关，并只提供当前功能需要的数据：

- aggregate header 生成直接 `TypeRelationFact`；
- named typealias 生成可验证的 `TypeAliasFact`；
- call expression 生成 callee 名称/span；
- 作用域/block 信息确定最内层 named callable；
- lambda/deferred 状态显式阻止错误 caller 归属；
- 本地可解析目标可用于测试，但 workspace fact 不持有 AST、analysis pointer。

不先设计通用 graph API，也不要求一次性把 `memberNavigationSymbols()` 或全部诊断规则
迁入 analysis。Implementation 出现真实重复时，只提取被导航、诊断和 provider 验证
共同使用的纯 predicate，例如 category/static/signature compatibility；不要借此重写
整个 `scopes.go`。

`Index.Replace()` 调整为：

1. 在锁外对 file 执行一次 `analysis.Analyze(file)`；
2. 复用该结果调用现有 `CollectExternalReferencesFromAnalysis()` 和新关系 collector；
3. 完成 symbol、external reference、type relation、call fact 等全部收集和排序；
4. 检查文件/index/关系预算；
5. 加锁后一次性移除旧文件的全部正反向事实，再安装新事实；
6. 成功后只增加一次 revision。

不能让 parser worker 直接写共享关系表。索引仍由稳定顺序的整文件结果更新。

## 7. Workspace 查询与一致性

### 7.1 两阶段反向查询

所有反向查询都采用：

1. 按 parent/callee 最终名称从反向表取得候选；
2. 获取候选的 index source 或 open snapshot；
3. 在事实记录的 span 上复用当前 navigation/import/autoload/member/typealias 解析；
4. 将解析结果转换为 SymbolKey，与请求目标精确比较；
5. 只保留精确匹配。

这样无需每次扫描全部 20,000 个文件，也不会把不同文件的同名符号、import alias 或
runtimepath 中不同来源混在一起。

### 7.2 Open buffer wins

查询必须遵守：

- 当前 open snapshot 覆盖 index 中的 closed source；
- 查询扫描过的所有 open snapshot 都参与最终 current check；
- workspace generation、index pointer/revision 和 import-graph revision 必须仍匹配；
- 第一次状态变化时重试，第二次仍变化返回 `ErrContentModified`；
- 不在持有 `workspaceMu`/`publishMu` 时 parse、遍历关系或转换大量 range。

直接复用 `workspaceNavigationSnapshot` 和
`navigationDocument.workspaceNavigationCurrent()`。只有实现后出现两套真实重复调用路径，
才把它们改成更一般的名称；设计阶段不先添加第二套 coordinator。

### 7.3 关系完整性和预算

现有 `maxIndexBytes` 只按 source byte 计费，新增关系事实不能无限增长。实现前用实际
workspace corpus 和构造型大文件 benchmark 确定：

- 每文件关系事实上限；
- 全 index 关系事实上限；
- 单次 hierarchy 结果上限。

构造型 1,000-call benchmark 在本地 darwin/amd64 上约为 3.85 ms/op、274 KB/op。基于
该结果，生产上限设为每文件 32,768 个关系事实、全索引 262,144 个关系事实、单次
hierarchy 结果 1,000 个。关系上限和结果上限都可在测试中注入小值；type relation、
typealias 和 call fact 共同计入关系预算。

关系事实未能完整安装时，index 需要独立的 relationship completeness 状态；不要让
Call/Type/Implementation 把旧事实或部分事实当成完整答案。反向闭世界查询返回
`RequestFailed`。所有候选循环按现有 workspace navigation 风格定期检查 `ctx.Err()`。

## 8. Handler 算法

### 8.1 Type prepare

```text
document position
-> current snapshot / byte offset
-> existing navigation + aggregate/typealias resolution
-> unique aggregate SymbolKey
-> TypeHierarchyItem + validated data
```

歧义时返回空；不要因为协议返回数组就返回多个猜测。

### 8.2 Supertypes

1. 验证 item/data 和对应 source content。
2. 对 open source 可直接收集当前 aggregate 的父边；否则读取
   `typeRelationsByChild`。
3. 精确解析每个 parent span。
4. 验证 class/interface/enum 的合法关系组合。
5. 转成 item，排序、去重并做 current check。

### 8.3 Subtypes

1. 验证 item/data，并要求 relationship index complete。
2. 按目标名称取得 `typeRelationsByParentName` 候选。
3. 精确解析每个候选 parent，与目标 SymbolKey 比较。
4. 返回匹配边的 child item。
5. 只返回直接 child，不做递归。

### 8.4 Implementation

类型级查询在 direct subtype 图上做 cycle-safe BFS：

- interface 沿 subinterface/implementor 关系向下；
- abstract class 沿 class descendants 向下；
- 使用 SymbolKey visited；
- 结果按 canonical URI 和 selection range 排序。

成员级查询先得到相关 descendant，再在每个当前 source 中按“本类优先、随后 base
chain”查找 effective provider，执行第 4.2 节的兼容性验证，并按 provider SymbolKey
去重。

### 8.5 Call prepare

按第 4.3 节的优先级选择唯一 callable。item：

- `name` 使用源码函数/member/constructor 名；
- `kind` 使用 Function、Method 或 Constructor；
- `detail` 使用已有 signature，method 可附 owner；
- `range` 是完整 callable block；
- `selectionRange` 是名称 span；
- `data` 保存内部身份和 content ID。

### 8.6 Outgoing calls

1. 验证 caller item/data。
2. open caller 从当前 analysis 取事实；closed caller 从 `callsByCaller` 取事实。
3. 精确解析每个 callee span。
4. 按 callee SymbolKey 分组。
5. 将调用点转换为 caller 文件的 LSP range，排序、去重。

### 8.7 Incoming calls

1. 验证目标 item/data，并要求 relationship index complete。
2. 按目标名称读取 `callsByCalleeName` 候选。
3. 精确解析候选 callee，与目标 SymbolKey 比较。
4. 按 Caller SymbolKey 分组。
5. 每个 caller 返回全部 caller 文件内的 `FromRanges`。

递归调用 A→A 是正常边；互递归 A→B→A 也保留。单次请求不递归展开。

## 9. hierarchy item data

CallHierarchyItem 和 TypeHierarchyItem 的 `data` 是客户端原样带回的未受信任 JSON。
它只能作为身份和陈旧检查凭据，不能直接作为文件读取指令。

```go
type hierarchyItemData struct {
    Version   uint8               `json:"v"`
    URI       string              `json:"u"`
    Kind      analysis.SymbolKind `json:"k"`
    Start     int                 `json:"s"`
    End       int                 `json:"e"`
    ContentID string              `json:"c"` // text.ContentID 的小写 hex
}
```

使用 `protocol.Marshal()` 得到 JSON bytes，再转换为 `protocol.LSPAny`。验证顺序：

1. raw data 不超过固定小上限；
2. JSON 可解码，version 已知；
3. URI 是 canonical file URI，并与 item.URI 一致；
4. start/end 非负且位于当前 source；
5. content ID hex 可解码且等于当前 `text.ContentIDOf(source)`；
6. 当前 source 在该 span 找到同 kind 的唯一 symbol；
7. 重建的 name/range/selectionRange 与客户端 item 一致。

格式错误或字段冲突返回空结果；格式有效但 content ID 已变化返回
`ErrContentModified`。不要把 Index pointer、revision 或绝对磁盘内部对象编码进 JSON。

## 10. 最小改动范围

先修改现有文件，只有单个文件职责明显过重时再拆分：

| 位置 | 必要改动 |
| --- | --- |
| `internal/analysis` | 增加 caller ownership 和单文件直接关系收集；若 `scopes.go` 继续膨胀，可创建一个 `relations.go`。 |
| `internal/workspace/index.go` | 扩展 SymbolFact/indexedFile、正反向关系表、Replace/Remove、完整性和可注入预算。不要先创建独立 registry。 |
| `internal/server` | 先用一个 `hierarchy.go` 实现共享 item/data 和 7 个 handler；只有代码形成真实独立区域后再按功能拆文件。 |
| `internal/server/server.go` | 在各功能完成时逐项增加 method allowlist 和 capability。 |
| package tests | analysis 语义、index 原子性、server wire/stale/open-overlay/encoding。 |
| `test/integration/lsp_subprocess_test.go` | 真实 framing/dispatch 和 item data round-trip。 |
| 用户文档 | capability 真正可用后再更新 language-support 和 roadmap。 |

不要为了这个里程碑先改写所有成员诊断、重命名现有 workspace snapshot 类型或创建空
package/file。

## 11. 测试要求

### 11.1 Analysis 与 Vim 语义

- class extends class、interface extends interface、class/enum implements interface；
- class typealias、alias chain/cycle 和 imported typealias，行为有 v9.2.1015 证据；
- direct/generic/method/constructor/defer calls；
- nested named function 归属最内层 callable；
- lambda、autocmd、mapping、user-command body 不误归属；
- direct function、reassigned Funcref、dictionary member 的保守边界；
- inherited/abstract/static/object member provider 和签名兼容。

### 11.2 Workspace index

- Replace/Remove 后旧正向和反向事实完全消失；
- 同名不同 scope/file 不串线；
- canonical/symlink path 行为与现有 index 一致；
- 超限不会暴露部分关系，relationship completeness 正确变化；
- revision 只在可观察状态变化时增加；
- 排序、去重和并发查询确定。

### 11.3 Type Hierarchy

- prepare 覆盖声明、引用、extends/implements、import 和已验证 alias；
- supertypes/subtypes 只返回直接一层；
- open buffer relation 覆盖 closed index；
- UTF-8/16/32、astral、combining 和 CRLF range；
- malformed/tampered/stale data、取消、workspace 两次变化；
- incomplete reverse index 返回 RequestFailed。

### 11.4 Implementation

- interface、subinterface、abstract/concrete class、enum；
- direct/inherited/overridden member provider；
- 多个 descendant 共用 provider 时去重；
- abstract/static/category/signature 不兼容项排除；
- imported aggregate 和 open imported provider；
- cycle、结果上限和 incomplete index。

### 11.5 Call Hierarchy

- local/import/global/autoload function；
- local/imported method 和显式 constructor；
- 多个 call site 的 FromRanges 分组；
- self recursion、mutual recursion；
- 同名不同 scope/file 不串线；
- interface receiver 不扇出实现；
- builtin/dynamic/Funcref/lambda/deferred exclusion；
- exact occurrence、enclosing fallback、stale item、open overlay、取消和结果上限。

### 11.6 Wire 与真实客户端

`TestLSPSubprocess` 必须验证：

1. capability 只在对应功能实现后出现；
2. 7 个请求均经过真实 JSON-RPC framing/dispatch；
3. Call/Type item data 经 JSON round-trip 后能继续展开；
4. Unicode range、FromRanges 分组和空结果正确；可注入的小上限由 handler 测试验证
   JSON-RPC `RequestFailed` error code；
5. shutdown/exit 生命周期不回归。

当前 pinned vim-lsp smoke 未证明提供 hierarchy UI。不要依赖私有命令伪造真实客户端
覆盖；先完成标准 subprocess 测试，再为明确支持标准 hierarchy 请求的客户端增加可选
smoke。

## 12. 实施切片

### 切片 1：Type Hierarchy

- 冻结 v9.2.1015 aggregate/typealias 语义；
- 增加 SymbolKey、owner 元数据、TypeRelationFact 和正反向索引；
- 完成 prepare/supertypes/subtypes、data 验证和 subprocess；
- 只在该切片全部通过后广告 TypeHierarchyProvider。

### 切片 2：Implementation

- 在类型图上实现 cycle-safe descendant 查询；
- 复用现有 member navigation，提取最小兼容性 predicate；
- 完成 imported/open provider、去重、上限和 incomplete-index 错误；
- 全部通过后广告 ImplementationProvider。

### 切片 3：Call Hierarchy

- 增加 caller ownership、CallFact 和反向索引；
- 完成 prepare/incoming/outgoing 与静态调用边界；
- 完成 data、stale、open overlay、subprocess 和真实可用客户端验证；
- 全部通过后广告 CallHierarchyProvider。

每个切片使用里程碑范围的本地提交，不把“创建 PR”写成实现前提。文档和 roadmap 只
描述已经验证的实际行为。

## 13. 最终门禁

三项能力完成时必须满足：

- 7 个标准方法通过真实 stdio subprocess；
- capability 与 `implementedMethod()` 一致；
- 同文件、import、autoload/global 和 open overlay 的承诺范围均有测试；
- Type Hierarchy 只返回直接边，Implementation 才做明确的传递查询；
- Call Hierarchy 不把动态调用或 deferred/lambda body 错归属给 named caller；
- 同名跨文件/作用域没有 false positive；
- item data 篡改、陈旧 source、workspace revision 变化和取消有确定结果；
- Replace/Remove 原子更新所有关系事实；
- 关系预算和结果上限经过 benchmark，并且不会产生貌似完整的部分结果；
- `gofmt`、`go test ./...`、`go vet ./...` 通过；
- 全部实现完成后再运行一次 `go test -race ./...`，不在每个未集成切片重复运行；
- `docs/language-support.md` 和 `docs/roadmap.md` 与实际 capability 同步。

实现验证（2026-09-01）：`go test ./...`、`go vet ./...` 和集成完成后的单次
`go test -race ./...` 均通过；真实 stdio subprocess 覆盖 7 个方法、item data
round-trip、Unicode `FromRanges`、空结果和篡改数据。关系预算 benchmark 与用户文档、
roadmap、architecture 也已同步。

## 14. 参考资料

项目内：

- `AGENTS.md`
- `docs/architecture.md`
- `docs/language-support.md`
- `docs/roadmap.md`
- `docs/syntax-coverage.md`
- `docs/testing.md`
- `internal/syntax/expression.go`
- `internal/syntax/syntax.go`
- `internal/analysis/scopes.go`
- `internal/analysis/symbols.go`
- `internal/workspace/index.go`
- `internal/server/server.go`
- `internal/server/navigation.go`
- `internal/server/workspace_navigation.go`

协议与 Vim：

- [LSP 3.18 Call Hierarchy](https://github.com/microsoft/language-server-protocol/blob/gh-pages/_specifications/lsp/3.18/language/callHierarchy.md)
- [LSP 3.18 Type Hierarchy](https://github.com/microsoft/language-server-protocol/blob/gh-pages/_specifications/lsp/3.18/language/typeHierarchy.md)
- [LSP 3.18 Implementation](https://github.com/microsoft/language-server-protocol/blob/gh-pages/_specifications/lsp/3.18/language/implementation.md)
- `go.lsp.dev/protocol v1.0.1` 本地生成代码
- Vim v9.2.1015 `runtime/doc/vim9class.txt`
- Vim v9.2.1015 `src/testdir/test_vim9_class.vim`
- Vim v9.2.1015 `src/testdir/test_vim9_interface.vim`
- Vim v9.2.1015 `src/testdir/test_vim9_typealias.vim`
