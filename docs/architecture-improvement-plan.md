# 架构改进计划

状态：A—E 已实施，主代理审核与最终集成验证通过（2026-09-28）。改动保留在工作树，未提交。

核对日期：2026-09-28。代码基线：`5eefd763d29bae3af1c30553c589fcb6663c61d7`，核对前工作树干净。
审核来源：`~/.gemini/antigravity-cli/brain/c9310ee8-f564-4504-85cc-a8807099c710/architecture-review.md`。
以下取舍来自该基线的源码、gopls 引用查询、包依赖和已有测试静态核对；规划时未确认新的并发故障或性能退化。实施状态与验证记录见文末。

## 目标与边界

消除已经存在的刷新重复，按职责整理大文件，让缓存安装和后台任务的生命周期更容易核对。
保持现有语言、LSP、配置、取消和诊断行为。当前语言基线为
[Vim v9.2.1132](language-support.md)，实施时仍以该文档记录为准。

优先使用现有包、类型和锁。文件拆分只移动已有声明；不同时调整算法、诊断阶段或测试预期。
新增辅助函数必须封装完整操作或消除重复，不能只是转发参数。
不以字段数、方法数、文件行数或锁数量作为验收指标。

## 对审核建议的取舍

| 报告项 | 基线核对结果 | 处理决定 |
| --- | --- | --- |
| 1. `Server` 职责过多 | 编排职责确实集中；已有明确的锁偏序，workspace、runtimepath、help 和诊断之间存在共同安装边界。 | 分批处理刷新状态、workspace 文件职责和锁归属说明；暂不拆成一组独立 manager。 |
| 2. 四类刷新重复 | `refresh.go` 三组循环与 `diagnostics_pull.go` 的诊断刷新重复；诊断另需 pull 模式，Code Lens 另受索引完整性约束。 | 第一批合并共同调度，保留各自能力和触发条件。 |
| 3. `scopes.go` 过大 | 当前为 8,563 行，声明收集、引用解析及多组诊断混在一起。 | 按现有职责机械拆文件，保留统一的分析入口和阶段顺序。 |
| 4. 分析入口过多 | `AnalyzeWithYield` 仍被 server、workspace 生产代码调用；`AnalyzeConfigFile` 还被 `tools/diagnosticscan` 调用，并非都只是测试便利函数。 | 低优先级清理纯转发链；保留有实际调用用途的普通文件和配置文件便利入口。 |
| 5. `parsecmd` 包不必要 | 普通文件检查、4 MiB 上限、打开后复检及 Unix 非阻塞打开，落实了已有 CLI 契约，防止 FIFO 等输入阻塞。 | 本轮保留包和检查。仅减少一个包的收益不足以支持迁移，不能据文件规模删除输入边界。 |
| 6. server 跨层访问 | [AGENTS.md](../AGENTS.md) 与 [架构说明](architecture.md) 规定 server 组合小包，没有要求依赖严格经过 workspace。 | 不增加 AST/analysis 转发层。现有直接依赖符合约定。 |
| 7. workspace 与 analysis 双向依赖 | 实际为 `workspace → analysis → syntax/vimdata`，不存在反向依赖；`ReplaceWithAnalysis` 复用与 AST 匹配的分析结果来收集引用及调用事实。 | 保留具体类型，避免为单一实现增加接口或另一套 facts 传输类型。 |
| 8. import 逻辑散布 | 分别处理单文件语义、跨文件导出事实，以及与服务器快照绑定的缓存和诊断。 | 在架构文档补一条调用链和所有权说明，保留当前包边界。 |
| 9. vimdata 查询分散 | 调用者消费不同的元数据；报告也未指出重复实现。 | 不改。没有具体 API 变更需求时，不建立统一查询包装层。 |
| 10. `snapshotFacts` 难读 | 同时处理导入事实、缓存共享、独立等待取消、计算和安装；身份复检有明确作用。 | 在现有回归测试约束下整理流程，集中结果安装，不新增缓存层或任务框架。 |

规模和重复说明维护成本，不直接证明并发错误。因此不将整体 `Server` 拆分列为必须立即完成的修复。

## 执行顺序

建议按 A → B → C → D → E 分批交付。A、B 是第一批；完成后即可获得明确收益，后续步骤各自可独立验收。
这些步骤没有必须一次完成的功能依赖；排序用于隔离状态机改动、机械移动和调用点迁移。

| 步骤 | 优先级 | 交付内容 | 主要风险 |
| --- | --- | --- | --- |
| A | 高 | 四类刷新共用调度实现 | 合并期间丢刷新、能力判断混用 |
| B | 高 | 按职责拆分 `scopes.go` | 移动时遗漏声明或改变诊断阶段 |
| C | 中 | 理顺 `snapshotFacts` 的安装边界 | 旧结果覆盖新结果、取消共享工作 |
| D | 中 | 拆分 workspace 文件并说明状态归属 | 不慎改变跨锁安装事务 |
| E | 低 | 删除冗余分析转发入口 | 丢失配置角色或 Yield 行为 |

### A. 合并四类刷新调度

范围：[refresh.go](../internal/server/refresh.go)、
[diagnostics_pull.go](../internal/server/diagnostics_pull.go)、
[server.go](../internal/server/server.go) 的刷新字段及初始化，以及对应调用点和测试。
调用点只作必要迁移，不调整其触发时机。

1. 在 `server` 包内定义固定四类的 `refreshKind` 和一个 `refreshState`，后者只保存现有的 support、generation、running。
   `Server` 持有固定四项状态，由现有 `s.mu` 保护。
2. 用一组 `scheduleRefresh` / `runRefresh` 完成共同流程；用明确的分支选择现有客户端方法。
   替换旧 `schedule*` / `run*`，不保留只转发到新函数的四组包装。
3. 保留 `scheduleWorkspaceRefresh(indexComplete)` 的编排作用。
   不采用报告中保存 `*sync.Mutex`、`*bool` 和长期回调的 runner；这里不需要注册表、接口、新锁或新后台队列。

必须保持：

- 四类各自合并 generation，彼此可以独立发送；执行中的多次变更仍能触发后续刷新。
- 能力不支持或客户端不可用时不发送；诊断刷新还必须启用 pull diagnostics。
- workspace 索引未完整时不触发 Code Lens 刷新；其他既有调用条件保持不变。
- 客户端 RPC 在服务器锁外执行，继续使用 `analysisContext`。
- shutdown 后的调度不发起请求；在途请求响应生命周期取消。
- 客户端报错沿用现有日志和退出条件，不增加无变更重试。

验证：复用 [refresh_test.go](../internal/server/refresh_test.go) 的能力、合并和关闭测试。
现有这组三类刷新测试需纳入诊断刷新，明确覆盖“支持刷新但未启用 pull”的情况。
用 channel/barrier 覆盖请求阻塞期间再次调度、不同刷新互不阻塞及客户端错误后的再次调度；只补已有测试未覆盖的行为。
保留 workspace 安装、初始文档刷新及普通单文档分析不刷新等现有测试。

验收：四类使用同一份调度循环，各自触发条件和可观察请求行为不变。

### B. 按职责机械拆分 `scopes.go`

范围：[scopes.go](../internal/analysis/scopes.go) 和同包新增文件。
已有 `scopes_assignment.go`、`scopes_operator.go`、`scopes_builtin_args.go` 保持当前职责。

目标归属如下；实施前以声明清单确认每个函数及其直接辅助函数的去向，避免按行号截取代码。

| 文件 | 归属 |
| --- | --- |
| `scopes.go` | `FileAnalysis`、`Scope`、`Declaration`、`Reference` 等模型，分析入口、初始化、进度检查和最终诊断组合。 |
| `scopes_declarations.go` | `collectCommandScopes`、嵌入命令/lambda/enum 声明收集、声明添加和排序。 |
| `scopes_references.go` | `walkCommand`、`walkExpression`、`walkAssignmentTarget`、`resolveValue`、`resolve` 及名称绑定辅助函数。 |
| `scopes_name_diagnostics.go` | 名称声明事件、覆盖/重声明、参数冲突及名称相关诊断。 |
| `scopes_aggregate_diagnostics.go` | class/interface/enum 定义、继承、实现关系和成员访问诊断。 |
| `scopes_flow_diagnostics.go` | return、defer、不可达代码、循环嵌套和 `functionFlow` 辅助逻辑。 |
| `scopes_null_diagnostics.go` | `collectNullReceiverDiagnostics` 的完整实现。 |
| `scopes_type_diagnostics.go` | 其余类型有效性、类型不匹配、解构和 void 值检查。 |

先移动诊断族，再移动声明与引用收集。每批只移动完整声明及其说明，调整 import；共享辅助函数放在最直接的职责文件中，不复制、不建立 `utils` 文件。
最终入口保留可顺序阅读的阶段列表；不引入 analyzer registry，也不合并 walker。

必须保持签名、函数体、调用顺序、稳定排序、诊断抑制关系、源位置和合作式取消检查。
尤其不能因为把 null、成员和赋值诊断分到不同文件，就改变其相对执行顺序。
配置诊断遍历与现有性能优化也不在本步骤调整。

验证：对比移动前后的包级声明集合和函数体，运行 `go test -count=1 ./internal/analysis`。
使用已有官方诊断、配置诊断和 [progress_test.go](../internal/analysis/progress_test.go)；机械移动不需要另写镜像测试。

验收：每个原有声明只保留一份，阶段列表和行为不变，可以直接从职责定位实现。

### C. 整理快照分析与结果安装

范围：[snapshot_analysis.go](../internal/server/snapshot_analysis.go) 及对应服务器测试。
保留 `parsedDocument`、in-flight key、`inFlightAnalysis` 和 `importTypeCache` 等现有类型。

1. 将 `snapshotFacts` 按现有执行顺序标明五段：取得导入事实、复用缓存或运行中任务、登记计算、执行计算、验证并完成任务。
2. 将尾部“按任务身份移除 in-flight、校验快照、更新 full/completion 缓存、关闭 done”提取为一个有完整意义的完成操作。
   调用者继续持有 `publishMu`，以 `Locked` 命名和注释说明；导入事实的最终检查紧邻安装，保持在原有锁边界内。
3. 导入事实加载和实际分析仍在锁外。缓存查找及等待逻辑优先保留在主流程，不为缩短函数而再包装几行代码。
   不增加结果状态枚举、通用 cache manager 或额外重试。

必须保持：

- 身份包含 URI、content ID/源文本、配置角色，以及实际消费的 import facts。
- completion 可以复用当前 full 结果；冷 completion 不等待或启动 full analysis，completion facts 不装入诊断分析槽位。
- 一个等待者取消不会取消其他调用者共享的计算。
- 编辑、关闭、依赖变化和 shutdown 仍能取消过期语义工作；A/B/A 不重新加入已取消任务。
- 等待完成后及安装前保留依赖复检；无关索引变动仍可复用未变化的导入事实。
- 旧任务不能删除替代任务；结果只能安装到仍匹配的文档和解析树；`done` 只关闭一次。

验证重点是已有
[analysis_stale_test.go](../internal/server/analysis_stale_test.go)、
[completion_scheduling_test.go](../internal/server/completion_scheduling_test.go)、
[import_types_test.go](../internal/server/import_types_test.go)。
使用其中的屏障确定时序；若配置角色变化或关闭后完成等路径缺少覆盖，只补对应缺口。

验收：复用、计算、安装的锁边界可直接核对，取消和过期结果回归测试通过；缓存键及生命周期不变。

### D. 整理 `Server` 的 workspace 职责

范围：[workspace.go](../internal/server/workspace.go)、`server.go` 的相关说明，以及 [architecture.md](architecture.md)。

先在同一个 `server` 包内移动完整实现：

| 文件 | 内容 |
| --- | --- |
| `workspace.go` | workspace 构建、索引/图安装、打开文档覆盖、恢复和依赖调度。 |
| `runtimepath.go` | runtimepath 通知入口、变更处理、delta 安装及其专用辅助函数。 |
| `file_watching.go` | watched-file 通知、事件批次应用、安装身份检查和 watcher 注册。 |
| `workspace_progress.go` | 已有 `workspaceProgressSession` 及进度发送/结束逻辑。 |

保留 `Server` receiver 和当前字段存储，不同时把锁搬进子对象。
索引、导入图、resolver、revision/readiness 的联合安装继续使用原边界；文件拆开不代表这些状态可以独立发布。

结合实际读写点补充字段组的锁归属和 worker 的启动、取消、等待责任。
保留 `server.go` 当前的锁偏序，不能整理成一个不符合实现的总顺序；尤其不能新增同时持有 `publishMu/watchMu` 或 `mu/workspaceMu` 的路径。
不另设一套生命周期状态。

在架构文档说明 import 调用链：
`snapshotFacts → server.importTypeCache → workspace.DeriveExportTypes → analysis`，
并说明单文件语义、跨文件事实和快照缓存各由哪一包拥有。

验证：声明/函数体对比，以及 workspace、runtimepath、watched-file、进度、runtime help 和 shutdown 现有测试。
保留未变化 runtime 根、打开文本优先、索引与图共同发布、旧批次不得覆盖新状态等行为。

验收：可按职责找到实现并识别状态保护边界。只有以后出现能够独立拥有完整状态和生命周期的真实需求，才另行设计组件；本计划不预建组件接口。

### E. 缩短分析入口转发链

范围：`internal/analysis` 的入口及调用测试、
[analysis_priority.go](../internal/server/analysis_priority.go)、
[workspace/parse.go](../internal/workspace/parse.go)。

删除 `AnalyzeWithYield`，将其真实调用点改为 `AnalyzeWithOptions`，显式保留原有 `ConfigFile` 和 `Yield`。
删除私有的 `analyzeWithRole`，让已有 `Analyze` / `AnalyzeConfigFile` 直接调用 `AnalyzeWithOptions`。
保留这两个便利入口：前者用于普通分析，后者仍被配置诊断测试和 `tools/diagnosticscan` 使用；不为了限定公开函数数量而迁移全部调用者。
不新增兼容转发函数，也不修改现有取消错误处理契约。

验证：gopls 引用查询确认旧入口没有遗漏；运行 analysis、workspace、server 与 diagnosticscan 的相关测试。
验收重点是配置诊断角色与 Yield 暂停/取消行为，不是减少到两个入口。

## 验证与交付约定

实施每一步之前重新核对工作树和调用点；以上代码位置与事实仅对应记录的基线。
每批先做相关包或现有用例的定向检查。机械拆分与状态机改动分开评审，不夹带语言规则、性能优化或公共行为变更。

Go 改动后请求 gopls diagnostics；如果是新文件元数据或 overlay 陈旧，使用 `gopls check <files>` 和编译结果确认。
完成所选批次的集成后，执行一次仓库规定的最终门禁：

```sh
gofmt -w <changed-go-files>
go test -count=1 ./...
go vet ./...
make
git diff --check
```

不因单纯文件移动增加 benchmark、race、coverage 或广泛 fuzz 任务。
本计划不作性能提升承诺；以后若改变调度或遍历算法，应另外记录可比负载和前后测量。
若确实改变支持行为，单列行为改动，并按 [testing.md](testing.md) 补相应 oracle/真实客户端验证及公共契约、roadmap 更新。
纯结构改动只更新受影响的贡献者说明，不将待实施事项写成已经支持的功能。

交付前逐项核对新增概念：A 的 kind/state 对应四份现有重复；C 的辅助函数对应完整任务完成操作；B、D 只调整文件归属；E 删除转发。
凡不能说明当前用途的新增层、状态或防御分支均删除。

- [x] A：刷新调度合并并验证四类行为。
- [x] B：诊断族、声明和引用机械拆分。
- [x] C：快照分析结果安装整理。
- [x] D：workspace 文件职责与锁归属说明。
- [x] E：分析入口转发链清理。

实施使用 Terra 子代理，主代理负责声明归属、刷新状态机及缓存安装设计，并独立审核。
机械拆分按顶层声明清单执行，使用一次性 Go 工具核对声明、函数体和注释；校验工具保留在仓库外。

已验证：A 的刷新/能力/关闭定向测试、B 的 analysis 包测试、C 的过期结果/补全/导入/配置角色定向测试均通过。
B 拆分前后 570 个包级声明的 token 无差异，原文件 156 条注释全部保留。
D 的 workspace/runtimepath/watcher/help/shutdown 定向测试通过；86 个声明的 token 无差异，75 条注释全部保留。主代理补充了现有锁保护的状态和 worker 生命周期说明。
E 的 analysis/workspace/diagnosticscan 测试及 server 调度/配置/导入定向测试通过，Go 源码中两个旧入口无残余引用。

主代理最终审核记录：

- 刷新调度保持每类独立 generation/running、锁外 RPC、原生命周期上下文及触发点；补强了实际 Initialize 的诊断能力矩阵，用 `synctest.Wait` 确定禁止刷新场景的异步状态。
- `finishSnapshotFactsLocked` 仅提取原完成操作，导入事实的最终校验与安装仍处于同一 `publishMu` 临界区；保留任务身份检查、快照检查、结果槽位区分及关闭 `done` 的顺序。
- B、D 的声明和注释由主代理独立复核。E 完成后再次比较整个 analysis 包：仅 `Analyze` / `AnalyzeConfigFile` 的直接调用改写及 `analyzeWithRole` / `AnalyzeWithYield` 的删除，共四项预期差异；分析阶段实现未变。
- 新增生产概念仅为四类刷新对应的 kind/state 和完整任务完成函数；没有新增锁、队列、重试、包接口或依赖。全部 28 个改动路径符合分工范围。

最终验证对应上述基线加当前工作树改动：

| 检查 | 结果 |
| --- | --- |
| 所有改动 Go 文件的 `gofmt` | 通过。 |
| `go test -mod=readonly -count=1 ./...` | 全部通过，包括服务器进程集成测试。 |
| `go vet -mod=readonly ./...` | 通过。 |
| `make` | 通过，构建 `bin/vimls` 和 `bin/vimparse`。 |
| 实际改动文件的 `gopls check` | 无编译诊断；仅两处已有的 `reflect.DeepEqual` 提示。MCP 的旧 overlay 报错已用磁盘文件检查和编译排除。 |
| 文档本地链接与 `git diff --check` | 通过。 |
