# 2026-09-05 审计修复记录

基线：`c6f4e28`。依据 [完整审计](code-audit-2026-09-05.md) 逐项修复；每项通过定向测试、`go test ./...`、`go vet ./...`、`make` 后单独本地提交，不推送。不运行 race/coverage。

原审计与探针保留为基线证据；探针断言的是旧缺陷，修复后不应要求原探针继续通过。正确行为测试放入相应包。

| 项目 | 状态 | 验证与提交 |
| --- | --- | --- |
| F01 | 已修复 | `fix(server): make runtimepath updates cancellable and snapshot-safe`；见下方验证 |
| F02 | 待修复 | |
| F03 | 待修复 | |
| F04 | 待修复 | |
| F05 | 待修复 | |
| F06 | 待修复 | |
| F07 | 待修复 | |
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
