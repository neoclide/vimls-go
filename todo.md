# 审计待办（除 internal/syntax parser 模块之外）

审计范围：`internal/analysis`、`internal/jsonrpc`、`internal/parsecmd`、`internal/server`、
`internal/text`、`internal/vimdata`、`internal/workspace`、`cmd/`、`test/`、`tools/`、
`docs/`、`Makefile`、`.github/`。基线：`go test ./...` 中非 parser 包全部通过，
`gofmt -l` 与 `go vet ./...` 均干净。

优先级说明：P0 = 正确性/健壮性缺陷，P1 = 性能/资源，P2 = 文档与实际不符、
测试与工具链缺口、代码卫生。

---

## P0 — 正确性 / 健壮性

### 1. `analysis.Analyze` 结果无缓存，每次导航/补全请求都重做全文件分析
- 位置：`internal/server/navigation.go`（`navigationAt`）、`internal/server/language_features.go`
  （`Completion`、`SignatureHelp`）、`internal/server/semantic_actions.go`
  （`SemanticTokensFull`、`InlayHint`）、`internal/server/rename.go`。
- 现状：`parsed` 只缓存了 `*syntax.File`，每次 hover/definition/references/completion
  等请求都对同一 snapshot 重新调用 `analysis.Analyze(file)`（全量 scope + reference + 类型推断）。
  大文件下这是每次按键触发的 O(file) 重复计算。
- 建议：在 `Server` 按 `(uri, revision)` 缓存 `*analysis.FileAnalysis`，随
  `parsedDocument` 一起失效；`DidChange`/`DidClose`/配置变更时清除。

### 2. `workspacePathResolver` 每次请求重建（重复 stat/EvalSymlinks）
- 位置：`internal/server/workspace.go:315` `workspacePathResolver`，被
  `workspaceNavigationState()`（`workspace_navigation.go:54`）在 definition/references/
  completion/documentLink 热路径反复调用。
- 现状：每次调用都 `NewPathResolver`，对 roots × runtimePaths 做 `os.Stat` +
  `filepath.EvalSymlinks` + `canonicalPathAllowMissing`。文件系统 I/O 重复发生在每个请求上。
- 建议：把 resolver 作为 workspace 状态的一部分缓存，仅在 `workspaceIndexWorker`
  重建（或 runtimepath/roots 变更）时更新。

### 3. `workspaceIndexWorker` 在持 `workspaceMu` 期间解析所有 open 文档
- 位置：`internal/server/workspace.go:126-147`。
- 现状：`s.workspaceMu.Lock()` 之后调用 `s.documents.Snapshots()` 并对每个 open
  snapshot 执行 `syntax.Parse` + `index.Replace`。`workspaceMu` 是普通 `sync.Mutex`，
  在持锁期间所有依赖它的请求（workspace/symbol、rename、completion、definition、
  documentLink 等）都会被阻塞，大 workspace（上限 20,000 文件 / 256 MiB）重建时阻塞时间明显。
- 建议：先在锁内拷贝 open snapshot 列表并释放锁，锁外解析，再短暂加锁原子交换
  `s.workspaceIndex` / `s.workspaceFiles`。

### 4. `walkCommand` 中函数参数默认值作用域分支是无效代码
- 位置：`internal/analysis/scopes.go:447-451`。
- 现状：
  ```go
  functionScope := scope
  if functionScope.Block >= 0 && functionScope.Kind != syntax.BlockFunction && functionScope.Kind != syntax.BlockDef {
      functionScope = scope
  }
  ```
  if 分支给 `functionScope` 赋了它已经等于的值，等价于无操作。意图不明，且与
  `collectCommandDeclarations`（`scopes.go:326` 把参数声明在函数块 scope）的作用域
  选择逻辑未对齐。要么删掉死代码，要么补上真实的“定位函数块 scope”逻辑并加测试。
- 建议：删除或修正，并为“参数默认值引用前一个参数”补一条作用域解析测试。

### 5. 用户函数 arity（参数个数）检查缺失，`vimdata` 返回类型元数据未被消费
- 位置：`internal/analysis/*`（不产生任何语义诊断）、`internal/vimdata/functions.go`
  （`BuiltinFunction.ReturnType` 字段）、`internal/server/language_features.go`
  （`builtinFunctionDetail` 只用 `MinArgs`/`MaxArgs` 拼文案）。
- 现状：
  - roadmap M4 声称交付 “mutability, arity”，但 `analysis` 包从不产生 arity 或
    undefined-name 诊断（`scopes.go:63` 明确注释“不报告 undefined names”），只靠
    `syntax.CompatibilityDiagnostics` 提供版本兼容诊断。
  - `genbuiltins` 解析的 `ret_*` 返回类型映射到 `FunctionReturnType` 后，没有任何
    server/analysis 代码读取，属于“生成但未使用”的元数据（builtin hover/completion
    无法展示返回类型）。
- 建议：明确登记为未完成项（而非宣称已交付），至少让 builtin 返回类型在 hover/
  completion resolve 中可用；arity 检查要么实现、要么在 roadmap 标注为未交付。

---

## P1 — 性能 / 资源

### 6. `Index.Search` 在排序比较器里重复计算 `searchRank`，且先全量再截断
- 位置：`internal/workspace/index.go:262-288`。
- 现状：`sort.SliceStable` 的 less 每次比较都调用 `searchRank` 两次（O(n log n) 次
  rune 级 subsequence 扫描），且在排序前已遍历全部 facts 构建完整 `matches`，最后才
  `matches[:limit]`。`workspace/symbol` 在 20,000 文件索引上会明显变慢。
- 建议：预计算每个候选项的 rank，做 top-k（小顶堆）而非全量排序；rank 用一次计算缓存。

### 7. `Stream.Read` / `ReadFrame` 每个消息解码两次
- 位置：`internal/jsonrpc/stream.go:29-59`。
- 现状：`ReadFrame` 先 `jsonrpc2.DecodeMessage` 校验合法性，`Stream.Read` 拿到 body
  后又 `jsonrpc2.DecodeMessage` 一次。每个入站消息被解析两遍。
- 建议：让 `ReadFrame` 返回已解码的 message（或让 `Read` 直接复用校验结果），避免双重解码。

### 8. 结果转换时为每个位置新建 `text.Snapshot`
- 位置：`internal/server/workspace.go:539-540`（`Symbols`）、
  `internal/server/workspace_navigation.go:126-128`、`163`、`195-196` 等。
- 现状：为把字节 span 转成 LSP 位置，对每个 match/reference 都 `text.NewSnapshot(...)`
  重新 `indexLines`。同一次请求内重复建行索引。
- 建议：复用同一份已构建的行索引（或提供 `text.Snapshot.Position` 的轻量缓存）。

### 9. `logf` 无并发保护
- 位置：`internal/server/server.go:924-928`。
- 现状：多个 goroutine（分析 worker、workspace worker、watch goroutine、请求 handler）
  并发写 `s.log`，stderr 输出可能交错。
- 建议：加互斥或单写者队列。

---

## P2 — 文档与实际不符、测试与工具链缺口、代码卫生

### 10. AGENTS.md 声称存在 `internal/lsp` 包，实际不存在
- 位置：`AGENTS.md` 的 “Architecture boundaries” 列出 `internal/lsp: LSP wire types and
  encoding conversion only`；实际 `internal/` 下没有该包，LSP 类型全部来自
  `go.lsp.dev/protocol`（`docs/architecture.md` 自身是自洽的）。
- 建议：更新 AGENTS.md 的架构边界描述，与 architecture.md 对齐。

### 11. `docs/testing.md` 声称的测试目录未建立
- 位置：`docs/testing.md` “Test layout”。
- 现状：声称的 `testdata/mixed/`、`testdata/incomplete/`、`testdata/unicode/`、
  `testdata/workspace/`、`testdata/fuzz/`、`test/vim/`（Vim oracle）均不存在；
  实际 `testdata/` 只有 `legacy`、`official`、`vim9`，`test/` 只有 `integration`。
- 建议：要么建立并填充这些目录，要么把文档中的布局改写成“当前实际布局 + 计划”。

### 12. 文档声称的 fuzz 目标与 benchmark 大部分缺失
- 位置：`docs/testing.md` “Fuzz and properties” 与 `docs/architecture.md`
  “Performance discipline”。
- 现状：fuzz 仅有 framing、position round-trip、parser（3 个），缺少声称的 lexer fuzz
  和 incremental-edit application fuzz；benchmark 除 `tools/benchlegacy` 与
  `internal/vimdata/commands_test.go` 外，缺少声称的文档/编辑/诊断/索引/补全/引用的
  多尺寸 benchmark。
- 建议：补充目标，或在文档中如实降级这些声明。

### 13. CI 与本地 `make check` 不一致
- 位置：`.github/workflows/ci.yml` 与 `Makefile`。
- 现状：本地 `check` = format-check + test + race + vet + coverage + build；
  CI 的 `test` job 只跑 `go test -race` + `go vet` + `go build ./cmd/vimls`（无 gofmt 检查），
  `coverage` job（仅 ubuntu）只跑 `make format-check coverage`。`make build` 构建三个
  binary，CI 只 build `cmd/vimls`。
- 建议：让 CI 完整复现 `make check`（至少在 ubuntu 上），并统一构建目标。

### 14. `cmd/` 三个二进制零测试，`tools/gencommands` 零测试
- 位置：`cmd/vimls/main.go`（含 `runTCP`、`--listen`、信号路径）、
  `cmd/vimparse`、`cmd/vim9parse`、`tools/gencommands/main.go`。
- 现状：`cmd/*` 无任何 `_test.go`；`tools/gencommands` 无测试（`tools/genbuiltins`、
  `tools/genofficial` 都有 `main_test.go`）。
- 建议：为 `runTCP` 的错误路径、flag 解析、gencommands 的 flag 解析/数量断言补测试。

### 15. `vimdata.Lookup` 依赖命令表按首字母连续排列的隐含假设
- 位置：`internal/vimdata/commands.go:36-59`。
- 现状：`init()` 用“首个/末个该首字母命令的下标”划区间，隐式假设生成的
  `commands` 数组同首字母条目连续（依赖 Vim `ex_cmds.h` 的字母序）。若上游重排，
  会静默返回错误缩写结果，无任何保护。
- 建议：在 `init()` 增加区间连续性断言，或改为显式按首字母分组索引。

### 16. `isVimFile` 在 Vim runtime 目录下把任意文件当作 Vim 脚本
- 位置：`internal/workspace/files.go:168-178`。
- 现状：`underRuntimeDirectory` 为真时直接返回 `true`，导致 `plugin/`、`syntax/` 等
  目录下的 `.md`/`.txt`/`.json` 也会被读入并 parse，产生无意义符号/诊断。
  文档 `language-support.md` 声称“文件扩展名不足以识别 Vim 脚本”，但反方向
  （runtime 目录下完全忽略扩展名）可能过度收录。
- 建议：在 runtime 目录内对非脚本扩展名做白名单过滤，并补充边界测试。

### 17. 工作区卫生：`reasonix.toml` 未提交也未忽略
- 位置：仓库根目录 `reasonix.toml`（`git status` 显示 untracked）。
- 现状：该文件是本地 agent 权限配置（含硬编码的 `git status` 命令），既未被
  `.gitignore` 忽略，也未纳入版本管理。
- 建议：决定是纳入 `.gitignore` 还是提交（去掉本机特定内容）并说明用途。

---

## 备注

- `internal/syntax`（parser）的若干测试当前失败（`TestVim9TypedDeclarationBoundaryProbeMapsMalformedContinuation`、
  `TestVim9CommandDiagnosticMakesSameLineTailOpaque`、`TestGeneratedOfficialVimEmbeddedCorpus`、
  `TestOfficialVimParserCases`、`TestVim9IncompleteExpressionRecoversAtFollowingStatements`、
  `TestGeneratedOfficialVimTestFiles`），按本次审计范围（排除 parser）不展开，但会导致
  `go test ./...` 与 CI 红，需在 parser 侧单独跟进。
