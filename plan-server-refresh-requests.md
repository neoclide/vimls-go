# Plan: LSP Server-to-Client Refresh Requests

## 背景

LSP 规范定义了若干由 Server 主动发给 Client 的 `workspace/*/refresh` 请求，用于通知客户端
重新拉取某类数据。现有代码已实现 `workspace/diagnostic/refresh`（见
`internal/server/diagnostics_pull.go`）。本计划完成其余与当前功能集直接相关的两个
refresh 请求：

- `workspace/semanticTokens/refresh`
- `workspace/inlayHint/refresh`

其他 refresh（`codeLens`、`foldingRange`、`inlineValue`、`textDocumentContent`）与当前
已实现能力无关，暂不实现。

---

## 范围

### 在当前功能集内、应实现的 Refresh

| Method | Client 能力字段 | 触发场景 |
|--------|----------------|---------|
| `workspace/semanticTokens/refresh` | `workspace.semanticTokens.refreshSupport` | 文档首次打开；工作区索引构建完成 |
| `workspace/inlayHint/refresh` | `workspace.inlayHint.refreshSupport` | 文档首次打开；工作区索引构建完成 |

### 不实现（原因）

| Method | 原因 |
|--------|------|
| `workspace/codeLens/refresh` | `textDocument/codeLens` 未实现 |
| `workspace/foldingRange/refresh` | 折叠区间纯粹基于单文件 AST，不依赖跨文件分析 |
| `workspace/inlineValue/refresh` | 调试 inline value 未实现 |
| `workspace/textDocumentContent/refresh` | 无自定义虚拟文档 scheme |

---

## 参考实现：`workspace/diagnostic/refresh`

`diagnostics_pull.go` 中的模式是本计划其余实现的模板：

1. **Capability 读取**（`Initialize`）：从 `params.Capabilities.Workspace.Diagnostics.RefreshSupport` 读取布尔值，存入 `s.diagnosticRefreshSupport`。
2. **防抖调度**（`scheduleDiagnosticRefresh`）：用 `mu` 保护的 `generation uint64` + `running bool` 对进行合并——每次触发只递增 generation；若已有 goroutine 在跑则直接返回。
3. **后台发送**（`runDiagnosticRefresh`）：goroutine 内循环，持有 generation 快照调用 `client.DiagnosticRefresh`，完成后若 generation 未再递增则退出，否则继续执行。
4. **触发点**：文档 `DidOpen` 完成后，以及 `workspace.go` 的 `workspaceIndexWorker` 安装完成的新索引后触发。

---

## 实现步骤

### Step 1：扩展 `Server` 结构体字段

**文件**：`internal/server/server.go`

在 `s.diagnosticRefreshRunning` 附近，在 `Server` struct 内新增：

```go
semanticTokensRefreshSupport    bool
semanticTokensRefreshGeneration uint64
semanticTokensRefreshRunning    bool

inlayHintRefreshSupport    bool
inlayHintRefreshGeneration uint64
inlayHintRefreshRunning    bool
```

所有字段均受 `s.mu` 保护，与现有 `diagnosticRefresh*` 字段保持一致。

---

### Step 2：在 `Initialize` 中读取 Client Capability

**文件**：`internal/server/server.go`，`Initialize` 方法

紧随 `diagnosticRefreshSupport := ...` 之后新增：

```go
semanticTokensRefreshSupport := params.Capabilities.Workspace != nil &&
    params.Capabilities.Workspace.SemanticTokens != nil &&
    params.Capabilities.Workspace.SemanticTokens.RefreshSupport != nil &&
    *params.Capabilities.Workspace.SemanticTokens.RefreshSupport

inlayHintRefreshSupport := params.Capabilities.Workspace != nil &&
    params.Capabilities.Workspace.InlayHint != nil &&
    params.Capabilities.Workspace.InlayHint.RefreshSupport != nil &&
    *params.Capabilities.Workspace.InlayHint.RefreshSupport
```

在随后的 `s.mu.Lock()` 块内赋值：

```go
s.semanticTokensRefreshSupport = semanticTokensRefreshSupport
s.inlayHintRefreshSupport      = inlayHintRefreshSupport
```

---

### Step 3：实现调度与发送函数

**文件**：新建 `internal/server/refresh.go`

按照 `diagnostics_pull.go` 的防抖模式实现两对函数：

```go
package server

// ---- Semantic Tokens Refresh ------------------------------------------------

func (s *Server) scheduleSemanticTokensRefresh() {
    s.mu.Lock()
    if !s.semanticTokensRefreshSupport || s.state == stateShutdown || s.client == nil {
        s.mu.Unlock()
        return
    }
    s.semanticTokensRefreshGeneration++
    if s.semanticTokensRefreshRunning {
        s.mu.Unlock()
        return
    }
    s.semanticTokensRefreshRunning = true
    s.mu.Unlock()
    go s.runSemanticTokensRefresh()
}

func (s *Server) runSemanticTokensRefresh() {
    for {
        s.mu.Lock()
        if s.state == stateShutdown || !s.semanticTokensRefreshSupport || s.client == nil {
            s.semanticTokensRefreshRunning = false
            s.mu.Unlock()
            return
        }
        client, generation := s.client, s.semanticTokensRefreshGeneration
        s.mu.Unlock()
        if err := client.SemanticTokensRefresh(s.analysisContext); err != nil && s.analysisContext.Err() == nil {
            s.logf("vimls: refresh semantic tokens: %v", err)
        }
        s.mu.Lock()
        if s.semanticTokensRefreshGeneration == generation {
            s.semanticTokensRefreshRunning = false
            s.mu.Unlock()
            return
        }
        s.mu.Unlock()
    }
}

// ---- Inlay Hint Refresh -----------------------------------------------------

func (s *Server) scheduleInlayHintRefresh() {
    s.mu.Lock()
    if !s.inlayHintRefreshSupport || s.state == stateShutdown || s.client == nil {
        s.mu.Unlock()
        return
    }
    s.inlayHintRefreshGeneration++
    if s.inlayHintRefreshRunning {
        s.mu.Unlock()
        return
    }
    s.inlayHintRefreshRunning = true
    s.mu.Unlock()
    go s.runInlayHintRefresh()
}

func (s *Server) runInlayHintRefresh() {
    for {
        s.mu.Lock()
        if s.state == stateShutdown || !s.inlayHintRefreshSupport || s.client == nil {
            s.inlayHintRefreshRunning = false
            s.mu.Unlock()
            return
        }
        client, generation := s.client, s.inlayHintRefreshGeneration
        s.mu.Unlock()
        if err := client.InlayHintRefresh(s.analysisContext); err != nil && s.analysisContext.Err() == nil {
            s.logf("vimls: refresh inlay hints: %v", err)
        }
        s.mu.Lock()
        if s.inlayHintRefreshGeneration == generation {
            s.inlayHintRefreshRunning = false
            s.mu.Unlock()
            return
        }
        s.mu.Unlock()
    }
}
```

> `client.SemanticTokensRefresh` / `client.InlayHintRefresh` 均是
> `protocol.Client` 接口上已有的方法（`go.lsp.dev/protocol@v1.0.1`），无需额外 wire 代码。

---

### Step 4：挂接触发点

触发点有两处：

1. `internal/server/server.go` 的 `DidOpen` 保存文档快照后。此时 semantic tokens 和
   inlay hints 可以按需解析当前快照，无需等待后台诊断分析。
2. `internal/server/workspace.go` 的 `workspaceIndexWorker`，紧接索引完成后的
   `scheduleDiagnosticRefresh()` 调用。调用在 `workspaceMu` 解锁后进行。

执行前先确认精确位置：

```sh
grep -n "scheduleDiagnosticRefresh" internal/server/workspace.go
```

`DidOpen` 完成后追加：

```go
s.scheduleSemanticTokensRefresh()
s.scheduleInlayHintRefresh()
```

索引完成后追加：

```go
// 现有
s.scheduleDiagnosticRefresh()
// 新增
s.scheduleSemanticTokensRefresh()
s.scheduleInlayHintRefresh()
```

当前 semantic tokens 和 inlay hints 都只分析当前文档，不消费依赖文件的分析结果，
因此依赖文件重分析不触发全局 refresh。未来加入跨文件结果后，应在新结果发布完成处
增加触发，而不是在依赖分析刚排队时触发。

如果 client 在工作区索引完成前请求 semantic tokens 或 inlay hints，server 按 LSP
允许的结果类型返回 `null`，不返回 `ContentModified`。索引安装完成后的 refresh 会让
client 重新请求已显示编辑器的数据。

---

### Step 5：Shutdown 安全

`runSemanticTokensRefresh` / `runInlayHintRefresh` 循环开头均检查
`s.state == stateShutdown` 并退出，与 `runDiagnosticRefresh` 一致。

`stopAnalysis()` 取消 `s.analysisContext`，使 client 调用返回，goroutine 自然终止。
无需额外处理。

---

### Step 6：测试

新建 `internal/server/refresh_test.go`，覆盖以下场景：

#### 6a. Capability guard
- client 未声明 `refreshSupport`：`schedule*` 调用后不发送任何请求（mock client 断言 0 次调用）。
- client 声明 `refreshSupport: true`：发送一次请求。

#### 6b. 防抖合并
- 在第一次 client 调用返回前连续调用 `schedule*` 多次：client 实际收到的请求数 ≤ 2（当前代 + 最多一次合并代）。

#### 6c. Shutdown 安全
- `Shutdown()` 后调用 `schedule*`：不产生新 goroutine，不发送请求。

#### 6d. 集成路径
- 使用 mock client 运行实际工作区索引 worker，断言 client 收到
  `workspace/semanticTokens/refresh` 和 `workspace/inlayHint/refresh`。
- 文档首次打开后，断言 client 收到两个 initial refresh。

#### 6e. 单文件边界
- 普通单文件分析和依赖文件分析排队都不发送全局 refresh。
- 索引尚未准备好时，semantic tokens 和 inlay hints 请求返回 `null` 且不报错。

如果现有 `mockClient` 尚未实现 `SemanticTokensRefresh` / `InlayHintRefresh` 方法，
在测试文件中补充即可。

---

## 文件变更汇总

| 文件 | 操作 | 变更内容 |
|------|------|---------|
| `internal/server/server.go` | 修改 | 新增 refresh 状态与 capability 读取；文档打开后触发 initial refresh |
| `internal/server/refresh.go` | 新建 | `scheduleSemanticTokensRefresh`、`runSemanticTokensRefresh`、`scheduleInlayHintRefresh`、`runInlayHintRefresh` |
| `internal/server/workspace.go` | 修改 | 工作区索引完成后追加两个 refresh 调用 |
| `internal/server/refresh_test.go` | 新建 | 覆盖 capability、合并、shutdown、触发点和单文件边界 |

---

## 约束与不变量

- 所有 `schedule*` 函数只在 `s.mu` 保护下检查字段，持锁时间极短。
- `run*` goroutine 不持有任何 Server 锁期间调用 client（与现有 `runDiagnosticRefresh` 一致）。
- `s.analysisContext` 是发送 refresh 请求时的 context，生命周期与 Server 一致。
- 不修改 `InitializeResult` 中的 `ServerCapabilities`——refresh 是 Server 主动推送给 client 的请求，无需在 server capabilities 里声明任何新条目。

---

## 验证命令

```sh
grep -n "scheduleDiagnosticRefresh" internal/server/workspace.go  # 确认触发点位置
gofmt -w internal/server/refresh.go internal/server/refresh_test.go internal/server/semantic_actions.go internal/server/semantic_actions_test.go internal/server/server.go internal/server/workspace.go
go vet ./internal/server/...
go test ./internal/server/...
```
