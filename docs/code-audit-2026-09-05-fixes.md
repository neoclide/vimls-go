# 2026-09-05 审计修复记录

基线：`c6f4e28`。依据 [完整审计](code-audit-2026-09-05.md) 逐项修复；每项通过定向测试、`go test ./...`、`go vet ./...`、`make` 后单独本地提交，不推送。不运行 race/coverage。

原审计与探针保留为基线证据；探针断言的是旧缺陷，修复后不应要求原探针继续通过。正确行为测试放入相应包。

| 项目 | 状态 | 验证与提交 |
| --- | --- | --- |
| F01 | 已提交 | `82cba9b`；见下方验证 |
| F02 | 已提交 | `2f368b7`；见下方验证 |
| F03 | 已提交 | `d98cef0`；见下方验证 |
| F04 | 已提交 | `49639ca`；见下方验证 |
| F05 | 已提交 | `008adf7` |
| F06 | 已提交 | `2cabe15` |
| F07 | 已修复 | `fix(analysis): preserve container types for slices` |
| F08 | 待修复 | |
| F09 | 待修复 | |
| F10 | 待修复 | |
| F11 | 待修复 | |
| F12 | 待修复 | |
| F13 | 待修复 | |
| F14 | 待修复 | |
| F15 | 待修复 | |
| F16 | 待修复 | |

## F01

自定义方法进入公共取消/请求数量检查；runtimepath 更新在释放输入循环前保留单调序号，后到更新取消并淘汰旧计算。扫描、解析、import 解析移出发布锁，安装前验证索引身份、更新序号及打开快照；两次受干扰后沿既有工作区重建路径恢复。shutdown 等待 runtimepath 工作退出。

正式回归测试在 `internal/server/runtimepath_lifecycle_test.go`：真实帧取消/重复 ID/didOpen/shutdown，较新无操作更新淘汰旧计算，打开 overlay 变化后重试。旧通知测试改为等待实际完成，不再假定异步通知必须赶在紧随的 shutdown 前安装。

验证：`go test ./internal/server -run TestRuntimepath -count=10 -timeout 60s`、`go test ./...`、`go vet ./...`、`make` 均通过；gofmt、gopls 检查通过（新测试文件的 MCP metadata 暂缺，使用本机 gopls check 补查）。原审计及探针随本项保存，不修改其旧缺陷断言。

## F02

关闭文档恢复、runtimepath 新源码和 workspace diagnostics 使用现有非阻塞/普通文件/LimitReader 上限读取；runtimepath 根也以非阻塞打开后的句柄检查目录类型。无法完整读入 runtimepath 源码时标记索引不完整，避免把遗漏当成完整结果。

验证：四个读取入口 FIFO 复现改为有界成功返回，另覆盖已有文件被替换、精确读取上界及关闭/runtimepath 超限文件；`go test ./internal/server -run 'Test(WorkspaceReadEntrypoints|ReadRegularWorkspaceFile|Runtimepath|DidChangeWatchedFiles.*FIFO)' -count=1 -timeout 60s`、`go test ./...`、`go vet ./...`、`make`、gofmt/gopls 均通过。FIFO 测试保留 Unix 构建约束，未声称中断任意网络文件系统的内核 I/O。

## F03

生成编辑前对拟修改文本重新解析/分析，验证声明身份、所有本地引用的目标绑定、同作用域新冲突和新增 Vim 编译错误；另检查未参与编辑的文件中的已索引全局名字冲突。编辑后源码大小在构造文本之前受限，位置映射使用有序编辑的前缀偏移与二分查找，避免按引用数乘编辑数扫描。

新增同名声明、嵌套捕获、参数、保留字、函数大小写、未解析引用捕获、legacy 脚本函数覆盖及跨文件全局冲突测试；保留独立函数作用域允许同名的正例。原有接口/类成员、跨文件 import、`s:`/`<SID>` 与编码测试通过。

验证：`go test ./internal/server -run 'Test.*Rename' -count=1 -timeout 60s`、`go test ./...`、`go vet ./...`、`make`、gofmt/gopls、diff check 均通过。动态绑定仍沿既有保守拒绝路径处理，不声称能证明任意 Vim 动态重构安全。

## F04

关闭文件在生成 range 前、编辑验证后均通过受限读取与计算所用的索引文本精确比较；不一致返回 ContentModified，不返回部分编辑。文件已删除且无法解析目标时沿原有安全拒绝路径返回错误。打开文件继续使用已捕获的 overlay/version，不以磁盘变更覆盖编辑器内容。

验证：插入行、删除、文件超限、磁盘已改变但打开 overlay 有效，以及原有跨文件/编码 Rename 测试；`go test ./internal/server -run 'Test.*Rename' -count=1 -timeout 60s`、`go test ./...`、`go vet ./...`、`make`、gofmt/gopls、diff check 均通过。最终检查之后的外部磁盘写入仍不是服务器能原子锁住的客户端编辑事务，未作此保证。

## F05

修改、删除与超限清除三个分支安装前统一验证工作区身份、取消状态及重建状态；过期结果返回既有重建回退路径。每次成功增量安装推进修订，并更新本批次预期身份，避免同时启动的重建反向覆盖该增量。测试受控暂停三种旧事件、安装新完整重建再恢复旧事件，确认拒绝旧结果且保留 Latest 文本。

验证：`go test ./internal/server -run 'Test.*(Watched|Workspace|Runtimepath)' -count=1 -timeout 60s`、`go test ./...`、`go vet ./...`、`make`、gofmt/gopls、diff check 均通过。

## F06

server 直接使用已有 `NewPathResolverForRoots`，保留全部工作区根、安全边界及 runtimepath 顺序。回归覆盖第二根的实际 Definition、相对/绝对 import 和根外拒绝。`go test ./internal/server ./internal/workspace -run 'Test.*(MultipleRoots|Resolver|Runtimepath)' -count=1 -timeout 60s`、全量 test、vet、make、gofmt/gopls 均通过。

## F07

索引与切片采用不同类型规则；list/string/blob 切片保留容器类型，tuple 的动态切片保留容器但不假定原来的元素位置/长度。覆盖完整、负数、省略界限、空切片及单元素索引对照。固定 v9.2.1015 的审计 oracle 再次通过（退出 0、v:errors/messages 为空）；定向类型测试、全量 test、vet、make、gofmt/gopls 均通过。
