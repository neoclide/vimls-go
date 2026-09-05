# runtimepath 非误报记录（2026-09-05）

仅保留最终扫描中的 28 条非误报及相关证据。结果对应代码提交 `3c6ed06`，Vim 基线为 v9.2.1015。

| 分类 | 条数 | 含义 |
|---|---:|---|
| 条件性真实代码错误 | 10 | 执行到对应调用或恢复分支时会报错，不表示当前启动已触发 |
| 旧 session/view 兼容错误 | 5 | 当前 Vim 拒绝相关选项或值 |
| 非法或不完整测试夹具 | 13 | 输入确实非法，但不是正常插件入口 |

## 保留材料

- [错误输出](errors.txt)：28 条诊断，保留最终扫描顺序和原始消息。
- [逐项说明](findings.md)：位置、原因、触发边界和验证案例。
- [机器可读分类](classification.json)：仅含这 28 条，ID 沿用原审计编号。
- [Vim 验证结果](oracle/results.json)：对应 10 个安全精简案例，均产生预期错误。
- [源码校验清单](source-manifest.json)：仅保留这些诊断涉及的 14 个文件。
- [扫描根目录](roots.txt)及 [Vim 构建信息](vim-version.txt)。

真实代码错误涉及 coc.nvim health 的 5 处缺少参数调用、sml.vim 的 1 处缺少 call、editorconfig 的 2 处缺少 call，以及 netrw 的 2 处修改只读参数。兼容错误涉及 4 份 session 的 fillchars 和 1 份 view 的 macmeta；另有 13 条 vim-matchup 测试夹具错误。

## 扫描范围与复现

最终扫描 53 个去重根目录中的 2980 个文件，得到 28 条诊断。\
`/usr/local/share/vim/vimfiles/after` 原本不存在，扫描程序因此退出 2，其余根目录扫描完成。

从仓库根执行，使用新输出路径避免覆盖证据：

```sh
go run ./tools/diagnosticscan \\
  -runtimepath "$(paste -sd, docs/runtimepath-audit-2026-09-05/roots.txt)" \\
  -output /tmp/vimls-runtimepath-errors.txt
```

扫描器递归发现文件，包括 bundle、测试、session 和 view；扫描集合不等于 Vim 实际启动执行集合。工具报告单文件 error 级诊断，不是完整跨文件 LSP 会话或运行时错误穷举。

验证使用干净的 Vim 9.2 patches 1–1015，只执行人工裁剪的安全案例，没有整体 source 插件、session 或 runtimepath。结果保留退出码、stdout/stderr、v:errors、messages、异常和补丁探测。同类位置共享最小案例，不代表完整执行每个原始文件。已有结果中的绝对路径及 throwpoint 是当时证据；重跑前应先核对源码 SHA256 和 Vim 版本。
