# 个人 runtimepath 诊断审计（2026-09-05）

扫描与逐项分类已完成：2981 个文件中，46 个文件产生 86 条 error 级诊断。其中 53 条是已确认的分析器误报，归为 18 类。这里的“误报”只针对标出的构造，不保证整个插件没有其他错误。

## 结论与清单

| 分类 | 条数 | 判断 |
|---|---:|---|
| 已确认误报 | 53 | 对应合法构造在 Vim 9.2.1015 中验证成功 |
| XPTemplate 模板上下文排除 | 5 | 模板正文不是普通 Vim 命令，加载行为取决于 XPTemplate 环境 |
| 条件性真实代码错误 | 10 | 执行到相关调用／分支时确实报错，不代表当前启动已触发 |
| 旧 session／view 兼容错误 | 5 | 当前 Vim 实际拒绝相关选项或值 |
| 非法／不完整测试夹具 | 13 | 文本确实不合法，但不是正常插件执行入口 |
| 合计 | 86 | 每条恰好分类一次 |

- [53 条误报列表](false-positives.md)：按原因分组，逐条列出位置和错误码。
- [86 条完整逐项分类](findings.md)：原始诊断、分类理由、验证案例及边界条件。
- [原始错误输出](errors.txt)：无删减、不重新排序；其行号就是审计编号。
- [机器可读分类](classification.json)：用于后续逐项修复与对照。
- [Vim 验证结果](oracle/results.json)：33/33 个案例符合预期。
- [源码 SHA-256 清单](source-manifest.json)：46 个产生诊断的文件；18 个官方 runtime 文件逐一与 v9.2.1015 内容匹配。
- [Vim 完整构建信息](vim-version.txt)。

误报涉及：初始化作用域、用户命令占位符、函数与变量命名空间、局部字典方法、CTRL-V 引用、修饰符与地址、new +cmd、autoload 调用、逻辑表达式、map 类型与回调、解构、删除后重定义、映射 RHS、函数型选项、多行逗号。详见误报列表。

10 条条件性真实代码错误分别是：

- coc.nvim health：五处 s:report_error 调用缺少第二个参数，E119。
- 官方 indent/sml.vim：一处传统函数调用缺少 call，E492。
- 官方 editorconfig：两处传统函数调用缺少 call，E492；包含 Windows 专属分支。
- 官方 netrw：两处失败恢复分支修改只读 a:newdir，E46。

官方来源不自动证明诊断是误报；测试夹具中的真实语法错误也不意味着插件正常使用有故障。

## 扫描范围

源码基线：521b0bd17e6869e82fc88becbf904b1cfed39769。

输入见 [roots.txt](roots.txt)：已拼回聊天显示中被拆开的路径，保留顺序和重复项。54 项去重后为 53 个根目录，52 个存在；唯一缺失的是 /usr/local/share/vim/vimfiles/after。扫描器记录该错误后继续处理其余根目录，并写出全部 86 条结果。

原扫描摘要：

```text
diagnosticscan: stat workspace root "/usr/local/share/vim/vimfiles/after": lstat /usr/local/share/vim/vimfiles/after: no such file or directory
diagnosticscan: scanned 53 roots, 2981 files, found 86 errors
exit status 2
```

退出码 2 来自缺失目录，不表示没有得到结果。.vim 根会递归纳入 bundle、测试夹具、session、view，包括未单独列在活动 runtimepath 中的目录。因此扫描集合不是实际 Vim 启动执行集合。

工具只输出其支持的 error 级 syntax／analysis.CombinedDiagnostics，不是完整跨文件 LSP 会话或运行时错误穷举；普通无扩展名脚本不在其后缀扫描范围。文件发现还会跳过 VCS、node_modules、符号链接目录及不允许的路径等。“全部”指本次工具范围内的完整输出，不表示扫描了磁盘上的所有内容。

## 验证证据与限制

使用 /usr/local/bin/vim：Vim 9.2 patches 1–1015，与项目 pin v9.2.1015 相同。官方证据从只读 checkout /Users/chemzqm/lib/vim 的该标签查询，不假设 HEAD 与 pin 一致；已有改动保留。

先检查原文件上下文，再以 [oracle_runner.go](oracle_runner.go) 运行人工裁剪的安全复现。没有整体 source 用户插件、session 或整个 runtimepath。四条 listchars 复现保留原始行字节；其他案例保留关键语言形态，不是整文件功能测试。

每例启动干净 Vim，参数为 -Nu NONE -U NONE -n -es -X -i NONE -S；runtimepath 仅包含证据目录内的安全 autoload stub，每例超时 5 秒。记录退出码、stdout／stderr、v:errors、:messages、异常、抛出位置和补丁探测。

- 22 个合法案例：退出 0，无异常，v:errors 为空。
- 11 个预期失败案例：产生预期错误码；包含限定 lua 重定义结论的 Vim9 删除函数反例。
- 共 33/33 符合预期。81 条非模板诊断各有一个案例归属，同类不同位置可共享案例。
- 同组精简案例经过当前扫描器产生 49 条诊断，见 [oracle/scanner-errors.txt](oracle/scanner-errors.txt)。精简案例和原文件不同，不能逐行一对一比较。
- 5 条 Scala 模板正文按上游加载语义单独分类，不计入本机执行验证。

XPTemplate 在 XPT 命令处收集模板并结束当前 source，再由解析器读取正文，因此正文不应逐行当作 Vim 命令。本次未确认本地 XPTemplate 提供者存在，不将这 5 条混入已验证的 53 条。依据：[上游命令定义](https://raw.githubusercontent.com/drmingdrmer/xptemplate/master/plugin/xptemplate.parser.vim)、[模板解析器](https://raw.githubusercontent.com/drmingdrmer/xptemplate/master/autoload/xpt/parser.vim)。

版本依据还包括 v9.2.1015 的 runtime/doc/vim9.txt（解构丢弃、续行与调用）、map.txt（用户命令替换与映射分隔）、options.txt（option-value-function）。

## 重跑

从仓库根执行；命令会覆盖对应证据文件，需要保留快照时应改用新输出路径。

后续修复执行中，经用户确认，扫描器增加了可选的模板文件排除方式：

```sh
go run ./tools/diagnosticscan \
  -runtimepath "$(paste -sd, docs/runtimepath-audit-2026-09-05/roots.txt)" \
  -exclude '*.xpt.vim' \
  -output docs/runtimepath-audit-2026-09-05/errors-after-template-exclusion.txt
```

`-exclude` 使用 Go `filepath.Match` 的文件名 glob，匹配每个文件的 basename，在所有子目录生效；可以重复传入多个规则。它不是路径 glob，不提供递归 `**` 语义。请给通配符加引号，避免 shell 提前展开。空规则和非法 glob 返回退出码 2，且不会打开输出文件。未指定参数时默认行为不变。

排除文件不参与解析或诊断，摘要单独报告去重后的排除文件数；语言服务器不受此参数影响。排除意味着该文件中的其他真实错误也不会扫描，不等于已经支持其模板语言。

下面是不加排除规则的原始基线重跑方式：

```sh
go run ./tools/diagnosticscan \
  -runtimepath "$(paste -sd, docs/runtimepath-audit-2026-09-05/roots.txt)" \
  -output docs/runtimepath-audit-2026-09-05/errors.txt

go run ./docs/runtimepath-audit-2026-09-05/oracle_runner.go

go run ./tools/diagnosticscan \
  -runtimepath docs/runtimepath-audit-2026-09-05/oracle/cases \
  -output docs/runtimepath-audit-2026-09-05/oracle/scanner-errors.txt
```

oracle_runner 保留本次个人 session 的绝对路径和行号，原文件变动后须先核对 source-manifest.json 与行号。案例已保存在 oracle/cases/。重新扫描不会自动更新 classification.json；诊断数量或顺序改变时，旧编号不能直接解释为新结果。

## 项目检查与变更范围

已通过 gofmt、本地 gopls check oracle_runner.go、go test -count=1 ./...、go vet ./...、make。分类检查确认 86 条无遗漏／重复，53+5+10+5+13=86，33 个 Vim 案例全部 verified=true。

gopls MCP 未报告本次 Go 文件错误，但仍报告 benchreport／release 测试与当前源码不符的缓存诊断；新启动的本地 gopls 检查和全量测试／vet 均通过，没有为此改动无关文件。

本轮只新增审计目录和本地构建产物，未修改分析器、插件或官方 Vim 源码，未提交或推送。工作区另外出现的四份旧审计文件删除改动未处理。后续若修复，建议以 FP 编号建立独立回归，保留真错误反例，不批量禁用错误码。
