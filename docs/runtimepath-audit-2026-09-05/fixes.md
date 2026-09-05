# 53 条误报修复进度

基线：521b0bd17e6869e82fc88becbf904b1cfed39769。执行批准的 18 类修复方案。

边界：XPTemplate 不做语言支持；按用户最新要求，扫描器直接忽略 `.xpt.vim` 文件，移除先前的 `-exclude` 参数。不修改插件、官方 Vim 或旧会话；动态正文只检查可证明部分，不执行用户代码，不全局禁用错误码。保留无关删除改动，只做本地提交。

每个独立根因：正反回归 → gofmt / gopls → go test ./... / go vet ./... / make → 本地提交。共享根因可以合并 FP 编号。源码位置、引用和类型检查不得以简单压低总数代替。

| 编号 | 工作项 | 状态 |
|---|---|---|
| FP01 | 初始化作用域 | 已修复：作用域、引用及全量门禁通过 |
| FP02 | 用户命令 args | 待修复 |
| FP03 | 函数与变量命名空间 | 已修复：名称绑定、类型及全量门禁通过 |
| FP04 | 局部字典方法 | 已修复：正反回归及全量门禁通过 |
| FP05 | CTRL-V 引用 | 已修复：字节夹具及全量门禁通过 |
| FP06 | 修饰符与地址 | 已修复：正反回归及全量门禁通过 |
| FP07 | new +cmd | 待修复 |
| FP08 | 数字 autoload 名称 | 已修复：正反回归及全量门禁通过 |
| FP09 | 逻辑操作数 | 已修复：正反回归及全量门禁通过 |
| FP10 | map 容器约束 | 待修复 |
| FP11 | 解构丢弃占位符 | 已修复：声明、符号及全量门禁通过 |
| FP12 | 循环解构类型 | 已修复：正反回归及全量门禁通过 |
| FP13 | 删除后重定义 | 已修复：正反回归及全量门禁通过 |
| FP14 | 映射正文诊断 | 待修复 |
| FP15 | map 回调 | 待修复 |
| FP16 | 引用参数占位符 | 待修复 |
| FP17 | 函数型选项 | 已修复：正反回归及全量门禁通过 |
| FP18 | 参数尾随逗号 | 已修复：正反回归及全量门禁通过 |

## 研究阶段补充

- FP08 直接原因是 Ex 命令名在数字前截断，autoload 检测未使用完整标识符，不是实际识别成修饰符。
- map：固定元素类型变量即使没有显式注解，也可能不能改变元素类型；copy 返回的容器会放宽约束。两种路径必须分别验证。
- 尾随逗号后不只有换行合法，同一行空格后接右括号也合法。

## 验证与提交记录

- FP03：legacy 普通值/赋值目标解析排除同名函数，继续查找可见变量；无法证明的条件变量保持 unknown，类型推断不再把已解析为未知的引用回退绑定到函数。调用名称和 Vim9 函数值不受影响。TestLegacyFunctionAndValueNamespaces 检查前置变量、条件变量、函数本身不是变量及 Vim9 Funcref 的引用绑定；全量 go test / go vet / make、gofmt、diff 检查通过。原 oracle 保留独立命名空间的 Vim 9.2.1015 成功证据。FP13 提交 394f0c2。

- FP13：先前的 legacy 函数在后续 def 前被脚本级 delfunction 删除，不再误报 E1073；条件删除按不可证明冲突处理，未调用函数体内的删除不算已执行。覆盖 SID 别名、不同函数、重新创建旧函数、未删除反例，并保留 Vim9 的 E1084。TestLegacyFunctionDeletionBeforeDef、全量 go test / go vet / make、gofmt、diff 检查通过；原 oracle 保留删除后重定义的 Vim 9.2.1015 成功证据。FP01 提交 2a58913。

- FP01：初始化表达式及 for 迭代表达式中尚不可见的声明目标，不再导致 lambda 参数 E1167/E1168；已有外层变量、后续初始化表达式仍保持遮蔽检查。测试同时检查 lambda 引用绑定参数；本地 Vim 9.2.1015 验证脚本/局部自身初始化合法及已有局部变量 E1167。TestInitializerLambdaParametersDoNotShadowTargets、原引用回归、全量 go test / go vet / make、gofmt、diff 检查通过。FP12 提交 c593cd6。

- FP12：循环解构按成员位置检查，不再把整行 list 类型与单个绑定比较；字面量逐行检查，保留后续行真实不匹配及 E710/E711，动态 any 保持保守。TestTypedForDestructuringDiagnostics 覆盖 typed pairs、rest、discard、错误成员/后续行/已知类型容器及普通循环。Vim 9.2.1015 正反探针验证合法 typed pairs/rest 和错误成员 E1163；全量 go test / go vet / make、gofmt、diff 检查通过。FP17 提交 9430267。

- FP17：按 Vim v9.2.1015 options.txt 的 option-value-function，仅允许 9 个函数型选项接受 Funcref/lambda，读取仍为 string；覆盖别名、局部/全局前缀、命名函数、普通字符串选项反例及 lambda 正文类型检查。TestFunctionValuedOptionAssignments、gofmt、全量 go test / go vet / make、diff 检查通过；gopls 仅有已记录的无关缓存错误。真实 Vim 9.2.1015 验证 opfunc lambda 成功、filetype lambda 报 E1012。另发现数字 opfunc 在 Vim 报 E921（当前分析器仍报 E1012），属于错误码精度后续项，不计入本次已确认误报修复。

- 默认模板跳过：移除 `-exclude` 及通用 glob 逻辑，在扫描文件筛选中直接忽略 `.xpt.vim` 后缀（大小写不敏感）；仅影响扫描器。回归覆盖默认跳过嵌套模板、保留普通文件及同名目录下的 Vim 文件、拒绝旧参数且不覆盖输出。gofmt、扫描器测试、全量 go test ./...、go vet ./...、make、git diff --check 通过；gopls 未报告本次文件错误，仅保留已知 benchreport/release 缓存错误。
- 最新复扫快照：`errors-after-default-template-skip.txt`。53 个去重根目录中扫描 2980 个文件，63 条诊断；原有 `/usr/local/share/vim/vimfiles/after` 不存在，扫描程序退出 2，其他根目录扫描完成。按完整路径、行列、错误码及消息逐条比对原始 classification.json，无新增诊断；已消除 18 条误报（7 类）及跳过 5 条模板诊断。35 条误报仍未修复，不能宣称全部完成。28 条非误报全部保留（13 条测试夹具非法片段、5 条旧会话兼容错误、10 条条件性真实错误）。

| 剩余误报 | 数量 |
|---|---:|
| FP01 初始化作用域 | 3 |
| FP02 用户命令 args | 9 |
| FP03 函数与变量命名空间 | 4 |
| FP07 new +cmd | 4 |
| FP10 map 容器约束 | 2 |
| FP12 循环解构类型 | 2 |
| FP13 删除后重定义 | 1 |
| FP14 映射正文诊断 | 2 |
| FP15 map 回调 | 5 |
| FP16 引用参数占位符 | 2 |
| FP17 函数型选项 | 1 |

- FP11：Vim9 解构中的 _ 不再加入声明表或文档符号；保留普通非法 _ 使用及 legacy 的合法 _ 变量。正反回归、全量 go test / go vet / make 通过。FP09 提交 bd2f29d。

- FP09：number 变量／调用保留运行时布尔转换，已知非 0/1 字面量保留 E1012，静态短路右侧不产生类型错误。新增最小 Vim 验证及 TestCompiledLogicalNumberConversions；全量 go test / go vet / make 通过。FP05 提交 17b04bd。

- FP05：独立 CTRL-V 引用的 UTF-8 字节不再误吞后续反斜杠；保留原始 Value span、普通空格选项分隔及后续命令。TestSetSessionQuotedUTF8Bytes、原有 set 测试、全量 go test / go vet / make 通过。FP18 提交 0cf3959。

- FP18：尾随逗号后的空格、TAB、LF、CRLF 不产生虚假实参；保留无空白、连续逗号及空白前置错误。修正旧测试把合法尾随逗号当缺失实参的假设，21 实参仍报 E740。全量 go test / go vet / make 通过。FP06 提交 a9f455c。

- FP06：有合法地址的修饰符不再触发缺失命令，裸修饰符仍触发 E1082；含尾注释及后续命令的回归和全量 go test / go vet / make 通过。FP08 提交 5e07b0b。

- FP08：完整标识符后的 # 调用识别支持数字／下划线；检查 AST、字节范围、后续命令及 legacy 不开启隐式调用。TestVim9AutoloadCallWithDigitsAndUnderscores 与全量 go test / go vet / make 通过。模板排除提交为 a767677。

原始 errors.txt、classification.json、source-manifest.json 保留为基线，不覆盖。

- FP04：允许 legacy 有效命名空间字典方法，保留 E884 非法函数名与 E1182 Vim9 字典限制；TestLegacyScopedDictionaryMethodNames、全量 go test / go vet / make 通过。gopls MCP 仅报告已知 benchreport/release 缓存诊断，未报告本次文件错误。

- `a81dde1`：FP04 修复提交；`73b6467`：原始审计证据与修复记录提交。
- 模板排除：新增可重复的 `-exclude` basename glob，仅影响扫描工具；默认不排除。测试覆盖默认行为、子目录模板、多个规则、重叠规则去重、全部排除、无匹配和非法规则不覆盖输出文件。
- 模板排除验证：全量 go test / go vet / make 通过。实际 runtimepath 复扫 2980 文件，排除 1 个 scala.xpt.vim，剩余 76 条错误，恰为原始 86 条减去 FP04 的 5 条及模板的 5 条。原有缺失 vimfiles/after 导致扫描程序退出 2，不影响其他根目录结果。快照：errors-after-template-exclusion.txt。
