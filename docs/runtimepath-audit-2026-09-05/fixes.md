# 53 条误报修复进度

基线：521b0bd17e6869e82fc88becbf904b1cfed39769。执行批准的 18 类修复方案。

边界：XPTemplate 不做语言支持；用户后续批准通过扫描器通用 `-exclude '*.xpt.vim'` 跳过模板文件。不修改插件、官方 Vim 或旧会话；动态正文只检查可证明部分，不执行用户代码，不全局禁用错误码。保留无关删除改动，只做本地提交。

每个独立根因：正反回归 → gofmt / gopls → go test ./... / go vet ./... / make → 本地提交。共享根因可以合并 FP 编号。源码位置、引用和类型检查不得以简单压低总数代替。

| 编号 | 工作项 | 状态 |
|---|---|---|
| FP01 | 初始化作用域 | 待修复 |
| FP02 | 用户命令 args | 待修复 |
| FP03 | 函数与变量命名空间 | 待修复 |
| FP04 | 局部字典方法 | 已修复：正反回归及全量门禁通过 |
| FP05 | CTRL-V 引用 | 待修复 |
| FP06 | 修饰符与地址 | 待修复 |
| FP07 | new +cmd | 待修复 |
| FP08 | 数字 autoload 名称 | 已修复：正反回归及全量门禁通过 |
| FP09 | 逻辑操作数 | 待修复 |
| FP10 | map 容器约束 | 待修复 |
| FP11 | 解构丢弃占位符 | 待修复 |
| FP12 | 循环解构类型 | 待修复 |
| FP13 | 删除后重定义 | 待修复 |
| FP14 | 映射正文诊断 | 待修复 |
| FP15 | map 回调 | 待修复 |
| FP16 | 引用参数占位符 | 待修复 |
| FP17 | 函数型选项 | 待修复 |
| FP18 | 参数尾随逗号 | 待修复 |

## 研究阶段补充

- FP08 直接原因是 Ex 命令名在数字前截断，autoload 检测未使用完整标识符，不是实际识别成修饰符。
- map：固定元素类型变量即使没有显式注解，也可能不能改变元素类型；copy 返回的容器会放宽约束。两种路径必须分别验证。
- 尾随逗号后不只有换行合法，同一行空格后接右括号也合法。

## 验证与提交记录

- FP08：完整标识符后的 # 调用识别支持数字／下划线；检查 AST、字节范围、后续命令及 legacy 不开启隐式调用。TestVim9AutoloadCallWithDigitsAndUnderscores 与全量 go test / go vet / make 通过。模板排除提交为 a767677。

原始 errors.txt、classification.json、source-manifest.json 保留为基线，不覆盖。

- FP04：允许 legacy 有效命名空间字典方法，保留 E884 非法函数名与 E1182 Vim9 字典限制；TestLegacyScopedDictionaryMethodNames、全量 go test / go vet / make 通过。gopls MCP 仅报告已知 benchreport/release 缓存诊断，未报告本次文件错误。

- `a81dde1`：FP04 修复提交；`73b6467`：原始审计证据与修复记录提交。
- 模板排除：新增可重复的 `-exclude` basename glob，仅影响扫描工具；默认不排除。测试覆盖默认行为、子目录模板、多个规则、重叠规则去重、全部排除、无匹配和非法规则不覆盖输出文件。
- 模板排除验证：全量 go test / go vet / make 通过。实际 runtimepath 复扫 2980 文件，排除 1 个 scala.xpt.vim，剩余 76 条错误，恰为原始 86 条减去 FP04 的 5 条及模板的 5 条。原有缺失 vimfiles/after 导致扫描程序退出 2，不影响其他根目录结果。快照：errors-after-template-exclusion.txt。
