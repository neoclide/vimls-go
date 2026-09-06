# issues.md 复核与修复计划

复核日期：2026-09-07。代码基线：`dea659bea38f4b484cd5a227e0bfe569f4240adf`。
输入是未跟踪的 [`issues.md`](../issues.md)，原始复核保留原文，只做分析与计划。
后续用户选择已确认：实施 M-8 的基础输入防护及 stdin 支持，不新增 L-8 的冲突方言修饰符提示。
M-8 的实现属于上述基线后的工作区变更，其余修复仍是计划。
语言证据读取 `/Users/chemzqm/lib/vim` 的 **v9.2.1015** 对象；该 checkout 的未跟踪文件保持原状。
本次没有运行 Vim 脚本，语言判断来自固定版本的源码和帮助。

## 结论与优先顺序

原报告的“7 项高危”不能直接作为修复清单。部分问题成立，但严重度、触发条件或修复方案有误；有些建议会改变现有正确行为。

建议优先处理 H-5 和 L-6：两处都存在可用很小输入触发的重复扫描，其中 L-6 并非原报告所称的最多两遍。
随后修复 M-4、M-1、H-2 和 L-1。H-7 按防御性路径校验处理，不能据此宣称已存在任意文件读取漏洞。
H-3/H-4、M-5 属于有明确优化空间的工作，应保留现有接口约定、按可比较工作负载验收。

这里的 P1 表示优先修复的可复现正确性或资源问题；P2 表示随后处理的优化、加固或清理。
本次没有证据支持把任何一项称为已发生的 P0 数据丢失或远程安全事故。

| 原编号 | 复核结论 | 建议 |
| --- | --- | --- |
| H-1 | CRLF 中间偏移确实拒绝转换，但符合现有严格坐标约定；未复现诊断全部丢失 | 不按原方案修改；随 M-4 加强边界验收 |
| H-2 | 嵌套切片共享确实违反副本所有权；未发现生产调用者写入导致的现行竞态 | 已修复，枚举和查询均深拷贝 |
| H-3 | 线性查询成立，“每个标识符数百次比较”过度概括 | P2，与 H-4 合并优化 |
| H-4 | 不必要拷贝成立；固定 6 次分配与表大小数据不准确 | P2，保留安全查询 API，增加轻量查询 |
| H-5 | 自动续行有多处累计文本重扫、重拷贝 | P1，优先修复 |
| H-6 | 忽略了服务层安装身份检查；worker pool 并不直接乱序写入在线索引 | 驳回所述缺陷，不添加重复版本体系 |
| H-7 | 路径映射接受不安全字符串；实际利用路径未证明 | P2，收紧静态 autoload 路径边界 |
| M-1 | 未响应配置请求可在活跃连接中长期积压，通知不受请求数量上限保护 | P1，超时、取代取消、生命周期绑定 |
| M-2 | 独立 Stream.Read 无法取消阻塞读取；生产 Run 已有取消与关闭路径 | 不整体重写；补充传输所有权与取消验证 |
| M-3 | 固定版本 Vim 允许循环导入，不应按环路统一报错 | 驳回通用循环诊断 |
| M-4 | 独立 CR 不被 text 和 parser 正确切行，违反 LSP 文本约定 | P1，跨层修复 |
| M-5 | 中间快照重复哈希成立；后续编辑仍需当前行索引 | P2，分离编辑中间态与最终快照 |
| M-6 | 子串持有源字符串成立，但当前仅用于生成器，不是服务器长期泄漏 | 暂缓，先有 retained heap 收益证据 |
| M-7 | 空文档导致整体失败成立，但这是生成器的严格完整性校验 | 保留，不静默跳过或向前误合并 |
| M-8 | 输入无界与特殊文件是加固点；用户选择基础防护和 stdin | 工作区已实现，见下文 |
| M-9 | 三种坐标各有职责；未证明支持平台/输入上的溢出 | 保留分层，转换 helper 仅随相关工作收敛 |
| L-1 | 先取 200 个目录项会漏掉后面的匹配项 | P1，修复相对/绝对路径补全 |
| L-2 | nil 明确生成无版本快照；生产编辑始终传非 nil 版本 | 不改变语义，可补充 API 注释 |
| L-3 | 已有单请求在途与 generation 合并；未证明刷新风暴 | 暂缓，不插入固定 Sleep |
| L-4 | 只有一个会话和一个帧读取者，报告的并发慢客户端模型不成立 | 驳回，不添加全局池/信号量 |
| L-5 | 指针键的性能影响无测量证据；向 AST 嵌入 Scope 会破坏分层 | 暂缓结构改造 |
| L-6 | 多个未闭合 heredoc 反复扫描后缀，存在平方增长 | 升为 P1，与 H-5 同批设计、分开修复 |
| L-7 | 固定子节点已使用切片字面量；可变子节点数量通常未知 | 暂缓，避免额外计数扫描 |
| L-8 | 覆盖修饰符符合固定版本 Vim 的实现，提示属于新风格功能 | 用户确认不新增提示 |
| L-9 | Go 1.26 已发布，直接依赖也要求 Go 1.26 | 驳回，不降级 |
| L-10 | tidy 删除失效校验和，并修正 uri 的直接依赖声明 | 已完成，无版本升级 |

## 逐项依据与实施方案

### H-1：CRLF 坐标不是“Windows 诊断全部丢失”

[`Snapshot.Position`](../internal/text/snapshot.go) 在 `foo\r\n` 的偏移 3 返回第一行末尾，在偏移 5 返回下一行开头，只有 4 被拒绝。
4 表示 CR 与 LF 之间；[LSP 3.18 文本约定](https://github.com/microsoft/language-server-protocol/blob/gh-pages/_specifications/lsp/3.18/types/textDocuments.md) 明确这种位置无法表达。
这不强制内部 API 必须报错，但足以说明“拒绝该位置本身就是协议错误”不成立。
现有 `TestSnapshotRejectsInvalidPositions` 明确把 CRLF 内部位置列入非法偏移。

`protocolDiagnostics` 确实跳过转换失败的诊断，但必须先证明生产者会生成这种 Span。
本次六个完整/不完整 CRLF 样本共有 8 条 parser/analysis 诊断，范围全部可转换；服务器现有 CRLF 诊断测试也通过。
这只能反驳报告的全量丢失结论，不能证明所有语法路径都正确。

计划：保持严格转换，在 M-4 的端到端验收中覆盖换行附近的诊断、符号和编辑范围。
如果发现某个真实 Span 落在 CRLF 内部，优先修复该 Span 的生产者；不要用统一 clamp 掩盖范围错误。
全局规范化属于另一个内部 API 设计选择，目前没有必要。

### H-2：修复所有权缺口，避免引入另一处共享可变数据

[`functions.go`](../internal/vimdata/functions.go) 的 `BuiltinFunctions` 只复制结构体数组，`LookupFunction` 返回的结构体也包含共享 `ArgumentChecks`。
本次修改枚举副本中 `abs.ArgumentChecks[0]` 后，再查询能读到修改值，证实别名共享。
当前生产调用者主要读取这些字段，未发现“已经触发并发崩溃”的证据；原报告混淆了潜在竞态与已发生竞态。

已实施：枚举和按名查询的返回值都拥有自己的嵌套切片，保留 nil 的语义。
测试遍历整个函数表，覆盖元素写入、重新切片/append 后的隔离及第二次查询不受影响。
`go test -count=1 ./internal/vimdata` 与 gopls diagnostics 通过。
Go 的 `*BuiltinFunction` 并非只读指针，不能拿它替代所有权保证。
为避免布尔查询也产生新分配，结合 H-3/H-4 提供实际需要的存在性查询；不一次性创造完整的新元数据框架。

### H-3/H-4：按使用目的优化查询，保留值拷贝 API

相关实现：[`functions.go`](../internal/vimdata/functions.go)、[`options.go`](../internal/vimdata/options.go)、
[`variables.go`](../internal/vimdata/variables.go)、[`autocmd_events.go`](../internal/vimdata/autocmd_events.go)。
本次枚举得到 591 个函数、562 个选项、118 个变量和 127 个事件，与报告数字不同。
[`scopes.go`](../internal/analysis/scopes.go) 的检查有 `&`、`v:`、调用表达式等上下文条件，不是所有标识符都会遍历全部表。

本机 `testing.AllocsPerRun` 测得 `LookupOption(number/aleph/ambiwidth/ballooneval)` 分别为 3/3/5/4 次，未命中为 0；
`Options()` 为 1675 次。空切片不会必然分配，因此不能把 6 个 clone 字段直接当作 6 次堆分配。
`TestOptionMetadataIsDeepCopied` 又明确要求查询结果可独立修改，删除 clone 会破坏现有约定。

计划：

1. 在包内部按源表建立不可变名字索引，canonical 名优先于短名；排除空短名。
2. 保留选项 `&`、`&g:`、`&l:`、终端选项拼写及精确短名语义；事件保持大小写不敏感，其他名字不随意折叠。
3. 增加 `IsOption` 等当前确有调用需求的轻量查询，迁移仅检查存在性的调用者。
4. 保留 `LookupOption` 的深拷贝接口。需要类型等少量标量的热路径，先测量再考虑窄查询，避免暴露可写全局指针。
5. 排序顺序可预计算一次，枚举仍返回调用方自己的副本。`OptionValues` 避免先拷贝整个 Option 再拷贝一次 values。
6. 用命中表头/中间/尾部、未命中、短名、实际 option-heavy 脚本比较 ns/op、B/op、allocs/op；同时记录索引初始化成本。

### H-5：自动续行需要整体消除累计工作

[`scanner.go`](../internal/syntax/scanner.go) 自动续行分支每次调用 `scanVim9CommandArgument` 重新扫描完整逻辑参数，
之后又用空状态执行 `scanVim9Continuation`。
[`logical_view.go`](../internal/syntax/logical_view.go) 的 `extendVim9LogicalCommand` 每行执行 `finishText`，
下一行 `makeMapped` 又复制已经累计的文本。
因此仅把一个状态变量传回扫描函数不能完成修复。

复现输入是 `vim9script\nvar xs = [\n` + N 次 `  1,\n` + `]\n`，没有语法诊断。
5,144 字节的 1024 行样本本次单次 Parse 累计分配约 110.5 MB；4 MiB 文件限制不能防住这种放大。

计划：

1. 为当前逻辑命令保留追加式文本/源片段映射和边界扫描状态，提交命令时才固化完整文本。
2. 新物理行只增量更新引号、转义、注释、括号、三元表达式和 lambda 上下文；必要的跨片段 lookbehind/lookahead 要明确定义。
3. 把“寻找命令边界”与“构建完整参数表达式”拆开，避免每行重新解析完整 AST；遇到真正的 separator 时结束旧命令、初始化新状态。
4. 处理显式反斜杠续行、前导 `|`、前导运算符、注释行、函数签名、嵌套 block lambda 与 heredoc 转换，不能只修复列表样例。
5. 保持原始字节 Span、trivia、诊断和恢复行为；使用固定的现有 parser 结果作为重构差分基线，并对接受语法使用固定版本 Vim 证据。

验收：代表性命令的完整 AST/Span/诊断等价；N 倍增时的扫描字节数与累计分配不再近似四倍增长。
记录普通单行、小文件和已有真实脚本的性能，防止为大续行牺牲常见路径。
不以任意的最大续行数代替算法修复。

### H-6：版本保护在服务层，不能只检查底层容器

已追踪 `Index.ReplaceWithAnalysis` 的生产调用者：

- [`prepareSyntax`](../internal/server/server.go) 持有 `publishMu`，先检查 `documents.IsCurrent(work)` 才安装文档分析。
- [`workspace.go`](../internal/server/workspace.go) 的全量构建先在新 Index 中依次安装 worker 的结果；发布前验证 open snapshots 与 workspace revision。
- runtimepath 增量安装验证完整 workspace identity 与 open snapshots。
- 文件监听安装再次检查文件是否已经打开以及 captured identity；关闭后的磁盘恢复检查 revision 和重新打开的 URI/别名 overlay。

相关 stale/reopen/rebuild 测试本次通过。报告所描述的“worker pool 直接乱序覆盖同一个在线索引”不符合当前调用流程。
底层 Index 提供并发安全的原子替换，不自行解释客户端版本，这符合包职责。

计划：不添加 `version <= oldVersion` 规则。LSP 文档版本、磁盘事实和关闭后重开的新文档生命周期不能用一个数字简单比较。
若未来出现新写入路径，应在服务层补充捕获与安装身份检查，并用 barrier 复现；本结论不等于已证明所有未来时序都安全。

### H-7：作为静态路径加固，先保留 Vim 名字语义

[`AutoloadPath`](../internal/workspace/resolver.go) 确实接受 `g:../etc/passwd#func` 并返回 `autoload/../etc/passwd.vim`。
固定版本 Vim 的 `src/scriptfile.c:autoload_name` 本身也只是替换 `#`，合法函数名不能仅由这一个函数推导。
当前导航主要查询 runtime index；`cleanRuntimeRelativePath` 阻止逃出总根，但会清理掉 `autoload/..`。
直接调用 resolver 仍受配置根与符号链接边界限制。未证明恶意字符串能作为正常 identifier 走通实际读取链路。

计划：在 autoload 专用入口拒绝路径分隔符、NUL、行分隔符和能造成目录跳转的成分；保证结果仍在 `autoload/` 子目录。
将不可信/动态名字视为无法静态解析，不额外制造 Vim 错误诊断。
实现前用 pinned 名字扫描规则确认 Unicode、`g:`、多级 `#`、空段等形式，避免直接套 ASCII 白名单或猜测全部合法名字。
测试普通/多级 autoload、目录穿越、Windows 分隔符、符号链接边界，并验证实际导航不返回其他 runtime 子目录的事实。

### M-1：约束配置请求生命周期与数量

[`refreshWorkspaceConfiguration`](../internal/server/server.go) 有 generation 检查，但没有请求 deadline，也不会取消已被取代的请求。
`jsonrpc2.Async` 释放读循环后，下一条 null 配置通知可以再发一次请求；这类通知绕过 `maxPendingRequests`。
底层 jsonrpc2 在连接终止时会清退等待者，因此准确影响是活跃连接内长期积压，不能笼统说进程退出后仍永久泄漏。

计划：为配置拉取保存当前取消函数，更新 generation 时取消已被取代的拉取；请求绑定服务生命周期并设置内部超时（建议 10 秒）。
带完整 settings 的通知也应使旧拉取失效并取消，旧取消函数不得影响更新的请求。
失败或超时保留最后成功配置，不重置成默认值；成功返回仍必须通过 generation 检查。
保留读循环提前释放、初始化/关闭顺序和现有配置安装锁约定。

验证：客户端已读请求但不回包、连续 null 通知、乱序响应、完整 settings 覆盖拉取、超时后恢复、shutdown/exit；
用 barrier 和可控短 deadline 验证 waiter/handler 结束，不使用真实 10 秒 Sleep。
这里的 timeout 解决等待响应；它本身不能使任意不响应取消的 io.Writer 可中断。

### M-2：区分 Stream 契约与会话关闭

[`stream.go`](../internal/jsonrpc/stream.go) 只在阻塞 I/O 前检查 context，单独调用确实不能取消已进入底层 Reader 的读取。
本次在可控阻塞 Reader 中确认取消后读取仍等待，释放 Reader 后正常退出。
但 [`Server.Run`](../internal/server/server.go) 自己 select context/exit 并关闭 stream，已有 `io.Pipe` 会话取消测试通过。
[`cmd/vimls/main.go`](../cmd/vimls/main.go) 的 TCP 路径也使用可关闭连接。

计划：当前不引入第二层永久读取 goroutine。任意 io.Reader 无可关闭能力时，把读取移到另一个 goroutine 只能转移阻塞，不能消除泄漏。
先明确 Stream 的 context 检查与 transport Close 所有权；补充“已经进入阻塞读”的 barrier 测试、半帧取消、TCP/stdio 子进程退出验证。
如果这些受支持场景确实不能关闭，再针对传输层选择 deadline/close 方案；不根据现有报告做全局重写。

### M-3：循环导入不等于错误

[Vim v9.2.1015 的 `:import-cycle`](https://github.com/vim/vim/blob/v9.2.1015/runtime/doc/vim9.txt#L3706) 说明，
再次导入正在加载的脚本会被跳过，环本身不直接构成错误；后续声明尚未执行才可能造成成员不可用。
autoload 导入还具有不同的加载时机。

因此 `A → B → C → A` 的统一 DFS 错误诊断会误报合法脚本。
保留直接 self-import 的 E1088 与图遍历防环逻辑。
若以后分析加载顺序，应只对能证明的“使用早于定义”给出诊断，动态控制流和懒加载保持保守；它不是本轮必须修复项。

### M-4：独立 CR 要贯通文本、语法及编辑功能

[LSP 3.18](https://github.com/microsoft/language-server-protocol/blob/gh-pages/_specifications/lsp/3.18/types/textDocuments.md) 指定 LF、CRLF、CR 三类换行。
`indexLines` 和 [`physicalLineEnd`](../internal/syntax/logical_view.go) 当前都只查 LF；`foo\r\nbar\rnext` 得到 2 行而非 3 行。
仅修改 Snapshot 会产生“坐标分行了、parser 仍是一条命令”的新不一致。

计划：text 的行索引与 syntax 的物理行边界使用一致的三种终止符规则，CRLF 只算一个换行。
审查 formatting、semantic token 切行、行注释/字符串/续行与嵌入 payload 中的 LF 专用扫描，区分语言载荷里的 CR 与物理行终止符。
保留原始 source、ContentID 和字节位置，不把整个文件预先转换为 LF。
服务端的转换继续留在 server/text，不引入 syntax 对 protocol 的依赖。

验证矩阵包括 LF/CRLF/CR/混合换行、末尾换行、空行、BOM、无效 UTF-8、组合字符和 astral 字符，覆盖 UTF-8/16/32、顺序增量编辑与全量结果一致。
端到端验证诊断、符号、rename/edit、格式化不改变原文件换行拼写。

### M-5：先移除重复哈希，不能移除中间坐标状态

[`ApplyChanges`](../internal/text/snapshot.go) 对每个 change 建立完整 Snapshot，重复 SHA-256 与全量行扫描，成立。
但这些 changes 来自按顺序解释的文本同步；不能把下一步的 Range 放到原文坐标中，也不能只维护字符串而不提供新的行坐标解析。
它也不是服务器处理所有格式化请求的通用路径。

计划：引入 text 包内部的短生命周期编辑状态，保留当前文本和需要的行索引，所有变化成功后才计算最终 ContentID 并产生不可变 Snapshot。
零变化复用不可变内容/索引并更新所需身份字段；全量替换正确重建后续编辑所需的坐标状态。
第一步可以只消除中间哈希和多余快照；若仍每次复制字符串/重建行索引，复杂度仍可能 O(KN)，不得宣称已完全消除。
局部行索引维护另行按测量收益推进，不为这项优化立刻引入 rope/piece table。
验证编辑顺序、前一步增删行、全量与增量混合、中途非法 Range 的原子失败、Unicode 和换行边界；测量多次小编辑的累计成本。

### M-6/M-7：生成器的内存与严格性

[`ParseTags`](../internal/vimhelp/help.go) 子串确实持有完整输入字符串，但所有 tag 都被保留，且当前调用者在 `tools/genmetadata`。
[`runtime_help.go`](../internal/server/runtime_help.go) 使用 `ExtractSymbols`，没有调用这里的 ParseTags/Extract。
因此不是“用户启动 LSP 后 tags 文件永远无法 GC”的已证实泄漏。
Clone 每个名称和文件名会增加许多小对象；是否降低 retained heap，需要比较生成器实际生命周期和数据密度。
本轮暂缓；若后续确有收益，可按 filename 去重并拷贝需要长期保留的键值，保持重复/空 tags 的错误检查。

`Extract` 的空末尾 boundary 会令提取失败，本次用 `*good*\nUseful documentation.\n*empty*\n` 复现。
但该 API 要求所有目标存在、唯一且有文档；当前生成器需要这种完整性门禁。
向前合并可能把另一条目的文档错误归给空标签，跳过则会让生成结果悄悄不完整。
保留严格报错。未来若为第三方帮助专门需要容错提取，应有独立且显式的调用契约，本次不改变。

### M-8：基础输入防护与 stdin（用户已确认）

复核基线的 [`parsecmd.Run`](../internal/parsecmd/run.go) 直接 ReadFile，未设置输入上限、未区分普通文件/设备/FIFO。
有限长度读取可以防止无界分配，但仅添加 LimitReader 不能解决 FIFO 打开或空闲读取阻塞。
`recover` 不能兜住 OOM，也不能替代修复 parser panic。

用户确认后，工作区实现为：路径参数只接受普通文件，`-` 从可注入 stdin 读取，二者使用相同的 4 MiB 上限。
读取最多上限加一个字节，用于区分恰好到上限与超限；超限或读失败不输出部分 JSON，stderr 报告原因，退出码为 1。
文件打开前后检查类型和大小；Unix 打开使用 O_NONBLOCK，避免普通文件在检查后被替换成 FIFO 时阻塞。
stdin 保留正常的流行为，等待数据/EOF，不新增读取超时或复杂配置。
保留原 JSON 格式、参数错误退出码 2 和 parser panic，不增加 recover 包装。
定向测试覆盖文件/stdin 输出一致、空输入、恰好上限、超限读取边界、目录、设备、FIFO 和读写失败。

### M-9：坐标分层合理，转换可局部集中

`syntax.Span` 保留原始字节，`text.Position` 负责编码坐标，`protocol.Position` 是线类型，这是既有架构边界。
server 中集中转换有维护价值，但不能把三种表示当作架构缺陷，尤其不能让 analysis/syntax 依赖协议。
Go `int` 在当前支持的 64 位构建上不是 `int32`；分析文件限制也使通常的输出远小于溢出边界。

不安排全局类型合并。M-4 涉及的转换可收敛到 server helper，维持已有严格校验。
如增加合法 wire 范围检查，依据 LSP 的 uinteger 上限 `2^31-1`，不要误用 `MaxUint32`。
只有实际需要支持的新架构或超大输入路径才单独扩展测试，不为假设规模改造全仓库。

### L-1：过滤先于候选预算

[`ImportPathCompletions`](../internal/workspace/resolver.go) 已通过 `os.ReadDir` 读完并排序目录项，然后截前 200 项。
本次目录包含 `a000.vim` 至 `a200.vim` 和 `zz_target.vim`，输入 `./import/zz_` 得到 0 项且 incomplete=true。
前缀更精确也找不到该文件，因此不是正常的结果上限行为。
[`completion.go`](../internal/server/completion.go) 的 runtime 前缀走 index，实际 LSP 中这项主要影响相对/绝对 import 路径。

计划：先进行廉价的名字前缀与扩展名过滤，再对候选执行 regular-file、canonical、安全边界和 acceptPath 检查，最后累计结果。
目录扫描预算、昂贵候选检查预算与可见结果上限要区分；在预算已尽时准确标记 incomplete，避免精确前缀被无关目录项消耗。
保留排序、去重、`./` 拼写、目录尾部 `/`、自身文件排除、符号链接限制和 runtimepath 顺序。
增加大目录后部匹配、非 Vim 文件/被过滤项占前列、恰好达到上限/额外一项、逃逸路径等测试。

### L-2/L-3/L-4：不按报告改动现有生命周期

L-2：`ApplyChanges(..., nil, ...)` 的确生成无版本快照；本次复现版本变成 `(0,false)`。
但 [`Documents.Change`](../internal/workspace/documents.go) 是唯一生产调用者，始终传入 `&version`，并拒绝不递增版本。
不存在报告暗示的正常编辑丢版本链路。保留 nil 的显式无版本含义，按需在 API 注释中写清楚。

L-3：[`diagnostics_pull.go`](../internal/server/diagnostics_pull.go) 已有 running 标志、generation 合并，客户端请求在锁外发送，相关合并测试通过。
新变化产生后续 refresh 是预期行为。没有测量实际频率前，不增加每次 100 ms 的延迟。
若真实客户端证实需要限速，应使用可取消定时器合并、保证最后一次状态刷新，不能 Sleep 堵住关闭或让持续变化无限推迟末次请求。

L-4：jsonrpc2 v1.0.1 的 `readIncoming` 同时只有一个 reader，Async 只是移交读取职责。
TCP 命令只 Accept 一个连接后关闭 listener；报告的“多个慢速客户端并发分配帧”不适用。
合法多请求仍有其他内存成本，但不等于多帧同时在读。`sync.Pool` 也不是内存上限，可能保留大 buffer；不实施该建议。

### L-5/L-7：没有证据支持结构性优化

L-5 的指针键 map 建立的是某个不可变 AST 内的分析关联。换整型 ID 需要分配与维护稳定身份，不能只看 key 类型。
把 `analysis.Scope` 放入 syntax AST 还会引入反向依赖或可变分析状态，违背包职责。
没有真实 workload 的 GC 证据时不改造；若出现明确瓶颈，先定位占用再比较 side table 或 ID 的收益。

L-7 中很多固定子节点已经用 `[]*Expression{left, right}` 等字面量创建。
Tuple/lambda/list 解析时通常尚不知道最终节点数；预先数逗号必须理解嵌套、字符串和不完整输入，会引入重复扫描。
只有已经拥有可靠数量信息的局部才考虑预分配，不能全局机械替换 append。本轮暂缓。

### L-6：多个未闭合 heredoc 的重复后缀扫描

[`scanner.go`](../internal/syntax/scanner.go) 在没有找到结束标记时，记住首个空行/函数结束行，但继续查到 EOF，随后回退恢复。
恢复后遇到下一条未闭合 heredoc，会再次扫描剩余后缀。输入 N 次 `let x =<< END\n\n` 可触发 N 个逐渐缩短的全量扫描。
这是实质性的平方增长问题，需要提升优先级。

计划：使结束标记查找可复用同一源文件的物理行信息，避免每次从当前位置线性查到 EOF。
可在 syntax 扫描上下文中建立精确行/标记候选位置索引，并用位置查找得到下一个合法结束标记；trim 模式需要遵循命令缩进，而非无条件 TrimSpace。
实现必须避免“每个不同 marker 再扫一遍全文”，否则换不同 marker 仍可重现平方增长。
保留完整 heredoc 中的空行、`endfunction`、`}` 等合法载荷；复用现有恢复点规则，只改变找标记的成本。
对 `:command {}`、嵌入语言、eval heredoc 的不同边界分别验证，不能一概套用普通 heredoc 终止规则。

验收包括重复/不同 marker 的缺失序列、长但完整的 heredoc、trim 缩进、恢复后续命令、函数和 command block；
以扫描量或比较工作负载验证扩展性，而非依赖脆弱的毫秒断言。
不采用“1000 行后停止恢复”的任意语义截断。

### L-8/L-9/L-10：风格决策与工具链事实

L-8：固定版本 `src/ex_docmd.c` 在读到 `legacy` 时清除 `CMOD_VIM9CMD`，读到 `vim9cmd` 时清除 `CMOD_LEGACY`，顺序覆盖是 Vim 自身的行为。
用户已确认保持现状，不新增冲突方言修饰符提示。

L-9：[Go 官方 1.26 发布说明](https://go.dev/doc/go1.26) 记录其在 2026 年 2 月发布。
本地固定依赖 jsonrpc2、protocol、uri 及 go-json-experiment/json 均声明 Go 1.26；CI 也用 1.26.x。
不降低 go.mod；改变最低版本意味着另一个依赖兼容性项目，不属于修复未发布版本。

L-10：已运行 `go mod tidy`，将 go.sum 从 231 行降至 10 行，
并把实际直接导入的 `go.lsp.dev/uri` 移到直接依赖块，依赖版本不变。
大量条目与 protocol 模块中的 tool 依赖对应，不能仅因没在根 go.mod 中看到名字就手工删除校验和。
再次 `go mod tidy -diff` 无差异；`go build -mod=readonly ./...`（包括仓库 Go 工具）通过。
全量测试随最终集成 gate 执行，不升级依赖、不改变工具链版本。

## 执行批次与验收

| 批次 | 范围 | 交付与验证 |
| --- | --- | --- |
| A | H-5、L-6 | 先固化两类最小复现和差分基线，再分别修复逻辑续行与 heredoc 扫描；比较工作量、内存与常见脚本性能 |
| B | M-4，并核对 H-1 | 跨 text/syntax/server 一致换行，端到端诊断与编辑无坐标偏差，保留所有原始字节 |
| C | M-1、H-2、L-1 | 配置等待有界、元数据副本隔离、精确路径前缀不遗漏；各自独立回归测试 |
| D | H-7、H-3/H-4、M-5 | 先确定静态名字契约；性能优化保持接口行为，并给出前后测量。M-8 已单独实施 |
| E | L-10 | 单独依赖清理，无升级；其他驳回/暂缓项只修正问题记录 |

批次是依赖与优先级安排，不要求每个简单修复等待整个 parser 重构完成。
M-8 已按用户选择单独实施基础输入防护及 stdin，L-8 保持现状；这不表示其他计划中的修复已经实施。
生产代码修复完成后，按 AGENTS.md 对相关 Go 文件 gofmt，请求 gopls diagnostics，并在整合后运行一次：

```sh
go test -count=1 ./...
go vet ./...
make
```

parser 语义与换行变化还要运行相关 oracle/真实客户端验证，记录固定 Vim 版本、v:errors、messages 和退出码。
不默认运行 race、coverage 或广泛 fuzz。影响支持行为的实施批次需同步受影响的公共文档与 roadmap；除单独实施的 M-8 外，其余功能仍处于计划阶段。

## 原始复核验证证据（基线 dea659b）

Go 工具链：`go1.27.0 darwin/amd64`。以下为相同进程环境、每组 3 次 Parse、每次前调用 runtime.GC 的小规模基线；
时间取中位数，分配量取 TotalAlloc 差值的均值。它们是复现证据，不是修复后性能承诺，也未执行大输入压力活动。

| 自动续行项数 | 输入字节 | Parse 中位时间 | 平均累计分配字节 |
| --- | --- | --- | --- |
| 128 | 664 | 2.059 ms | 1,913,762 |
| 256 | 1,304 | 9.225 ms | 7,228,525 |
| 512 | 2,584 | 32.369 ms | 28,163,373 |
| 1024 | 5,144 | 177.425 ms | 110,547,680 |

| 未闭合 heredoc 数 | 输入字节 | Parse 中位时间 | 命令/诊断数 |
| --- | --- | --- | --- |
| 256 | 3,840 | 2.443 ms | 256 / 256 |
| 512 | 7,680 | 8.398 ms | 512 / 512 |
| 1024 | 15,360 | 32.508 ms | 1024 / 1024 |
| 2048 | 30,720 | 129.531 ms | 2048 / 2048 |

临时 Go 探针还验证了 H-1/H-2/H-4/H-7、M-2/M-4/M-7、L-1/L-2，输入与结论记录在相应条目。
探针只在当前进程中临时修改元数据副本来检验共享，随后恢复；没有更改生产文件或执行用户 Vim 脚本。

本次通过的现有 focused checks：

```sh
go test -count=1 ./internal/text ./internal/vimdata \
  -run 'Test(Snapshot|ApplyChanges|OptionMetadataIsDeepCopied|BuiltinFunctions)'

go test -count=1 ./internal/vimhelp ./internal/server \
  -run 'Test(ParseTags|ConfigDiagnosticsPreserveUnicodeCRLFPositions|WorkspaceRestoreRejectsReopenedURIAndAliasOverlay|WorkspaceRestoreGenerationStaleSchedulesMergedRebuild|ServerCloseReopenRejectsPausedRestore|ServerRebuildCancellationDoesNotPublishIndex|ServerDocumentHandlersCancelStaleAnalysis|ServerCancellationAndEOFExitCleanly|WorkspaceConfigurationRejectsOldResponses|DocumentPullDiagnosticRefreshCoalesces)$'
```

第二条实际调用还包含 text 包与未匹配的 Extract/Snapshot/ApplyChanges 名称；该轮 text 报 no tests to run，随后已用第一条有效前缀选择运行。
这里没有把未匹配的选择计作已通过测试。原始复核未运行全量 gate、原生 Windows、race、oracle 或修复后性能测试。

## M-8 实施验证（基线后的工作区变更）

该次实施范围为 `internal/parsecmd`、`cmd/vimparse/main.go` 及对应文档，提交为 `c8681eb`。后续修复状态记录在各编号条目。
定向测试、`go test -count=1 ./...`、`go vet ./...` 和 `make` 均通过。
gopls MCP 尚未识别新文件的 package metadata，改用本地 `gopls check` 检查当前平台的改动 Go 文件，无诊断。
生成的 `bin/vimparse` 已验证文件与 stdin 的 JSON 等价、超限 stdin 返回 1 且无 JSON、参数错误返回 2。
Windows amd64 的 CLI 交叉编译通过，未运行原生 Windows 测试；未额外运行 race、coverage 或 Vim oracle 活动。
