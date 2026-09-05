# 已确认的 53 条误报

这些结论针对扫描器标出的具体语法／语义，不承诺整个插件文件在任意环境都不会发生其他错误。验证版本为本机 Vim 9.2.1015，与项目 pin 一致。

完整原始诊断及非误报部分见 [86 条逐项分类](findings.md)。最小案例、退出码、异常、v:errors 与消息见 [oracle/results.json](oracle/results.json)。

## FP01：初始化表达式中的 lambda 参数同名（3 条）

声明目标 cmd／循环变量 n 在初始化表达式求值时尚未成为冲突的外层变量；Vim 允许回调参数使用这个名字。这不意味着允许遮蔽任意已存在变量。

- #1 [$VIMHOME/autoload/coc/t.vim:86:36](</Users/chemzqm/.vim/autoload/coc/t.vim:86>) — vim/E1167
- #2 [$VIMHOME/autoload/x.vim:59:36](</Users/chemzqm/.vim/autoload/x.vim:59>) — vim/E1167
- #69 [$VIMRUNTIME/indent/hare.vim:331:45](</usr/local/share/vim/vim92/indent/hare.vim:331>) — vim/E1167

[验证案例与证据](findings.md#fp01)。

## FP02：用户命令 &lt;args&gt; 被当成选项（9 条）

CompilerSet 的替换文本在命令调用时展开 &lt;args&gt;；它不是 setlocal 的字面选项。最小案例实际调用 CompilerSet 并断言 shiftwidth。

- #3 [$VIMHOME/bundle/splitjoin.vim/spec/support/rust.vim/compiler/cargo.vim:19:43](</Users/chemzqm/.vim/bundle/splitjoin.vim/spec/support/rust.vim/compiler/cargo.vim:19>) — vim/E518
- #4 [$VIMHOME/bundle/splitjoin.vim/spec/support/rust.vim/compiler/rustc.vim:18:43](</Users/chemzqm/.vim/bundle/splitjoin.vim/spec/support/rust.vim/compiler/rustc.vim:18>) — vim/E518
- #5 [$VIMHOME/bundle/splitjoin.vim/spec/support/vim-elixir/compiler/credo.vim:7:43](</Users/chemzqm/.vim/bundle/splitjoin.vim/spec/support/vim-elixir/compiler/credo.vim:7>) — vim/E518
- #6 [$VIMHOME/bundle/splitjoin.vim/spec/support/vim-elixir/compiler/exunit.vim:7:41](</Users/chemzqm/.vim/bundle/splitjoin.vim/spec/support/vim-elixir/compiler/exunit.vim:7>) — vim/E518
- #7 [$VIMHOME/bundle/splitjoin.vim/spec/support/vim-elixir/compiler/mix.vim:7:43](</Users/chemzqm/.vim/bundle/splitjoin.vim/spec/support/vim-elixir/compiler/mix.vim:7>) — vim/E518
- #8 [$VIMHOME/bundle/splitjoin.vim/spec/support/vim-javascript/compiler/eslint.vim:12:42](</Users/chemzqm/.vim/bundle/splitjoin.vim/spec/support/vim-javascript/compiler/eslint.vim:12>) — vim/E518
- #9 [$VIMHOME/bundle/swift.vim/compiler/swift.vim:17:43](</Users/chemzqm/.vim/bundle/swift.vim/compiler/swift.vim:17>) — vim/E518
- #10 [$VIMHOME/bundle/typescript-vim/compiler/typescript.vim:15:42](</Users/chemzqm/.vim/bundle/typescript-vim/compiler/typescript.vim:15>) — vim/E518
- #28 [$VIMHOME/bundle/vim-scala/compiler/sbt.vim:14:41](</Users/chemzqm/.vim/bundle/vim-scala/compiler/sbt.vim:14>) — vim/E518

[验证案例与证据](findings.md#fp02)。

## FP03：函数与变量命名空间混淆（4 条）

fugitive 同时定义 s:fnameescape() 函数和 s:fnameescape 字符串变量合法；字符串拼接读取变量，而不是把 Funcref 当字符串。

- #11 [$VIMHOME/bundle/vim-fugitive/autoload/fugitive.vim:484:62](</Users/chemzqm/.vim/bundle/vim-fugitive/autoload/fugitive.vim:484>) — vim/E729
- #12 [$VIMHOME/bundle/vim-fugitive/autoload/fugitive.vim:2078:21](</Users/chemzqm/.vim/bundle/vim-fugitive/autoload/fugitive.vim:2078>) — vim/E729
- #13 [$VIMHOME/bundle/vim-fugitive/autoload/fugitive.vim:2084:21](</Users/chemzqm/.vim/bundle/vim-fugitive/autoload/fugitive.vim:2084>) — vim/E729
- #14 [$VIMHOME/bundle/vim-fugitive/autoload/fugitive.vim:2105:60](</Users/chemzqm/.vim/bundle/vim-fugitive/autoload/fugitive.vim:2105>) — vim/E729

[验证案例与证据](findings.md#fp03)。

## FP04：局部字典方法被当作非法函数名（5 条）

l:matcher.get_entry()、l:popup.close() 等是合法字典方法，l: 修饰字典变量。Vim 验证了方法定义和调用；不表示整个 Neovim 专用模块能在 Vim 执行。

- #34 [$VIMHOME/bundle/vimtex/autoload/vimtex/parser/toc.vim:176:17](</Users/chemzqm/.vim/bundle/vimtex/autoload/vimtex/parser/toc.vim:176>) — vim/E884
- #35 [$VIMHOME/bundle/vimtex/autoload/vimtex/ui/nvim.vim:14:12](</Users/chemzqm/.vim/bundle/vimtex/autoload/vimtex/ui/nvim.vim:14>) — vim/E884
- #36 [$VIMHOME/bundle/vimtex/autoload/vimtex/ui/nvim.vim:50:12](</Users/chemzqm/.vim/bundle/vimtex/autoload/vimtex/ui/nvim.vim:50>) — vim/E884
- #37 [$VIMHOME/bundle/vimtex/autoload/vimtex/ui/nvim.vim:114:12](</Users/chemzqm/.vim/bundle/vimtex/autoload/vimtex/ui/nvim.vim:114>) — vim/E884
- #38 [$VIMHOME/bundle/vimtex/autoload/vimtex/ui/nvim.vim:230:12](</Users/chemzqm/.vim/bundle/vimtex/autoload/vimtex/ui/nvim.vim:230>) — vim/E884

[验证案例与证据](findings.md#fp04)。

## FP05：session 的 CTRL-V 转义被误切分（4 条）

生成的 listchars 命令使用字面 CTRL-V 引用字节及转义空格。保留原始字节逐条执行均成功；不能根据终端显示的替换字符认定源命令损坏。

- #40 [$VIMHOME/sessions/coc-list.vim:96:32](</Users/chemzqm/.vim/sessions/coc-list.vim:96>) — vim/E518
- #43 [$VIMHOME/sessions/mini.vim:95:32](</Users/chemzqm/.vim/sessions/mini.vim:95>) — vim/E518
- #45 [$VIMHOME/sessions/python.vim:91:32](</Users/chemzqm/.vim/sessions/python.vim:91>) — vim/E518
- #47 [$VIMHOME/sessions/someroject-rust.vim:103:32](</Users/chemzqm/.vim/sessions/someroject-rust.vim:103>) — vim/E518

[验证案例与证据](findings.md#fp05)。

## FP06：修饰符后的纯地址命令（1 条）

keepjumps :3 是合法跳转；最小案例准备三行文本并断言到达第三行，不缺少命令。

- #41 [$VIMHOME/sessions/default.vim:304:3](</Users/chemzqm/.vim/sessions/default.vim:304>) — vim/E1082

[验证案例与证据](findings.md#fp06)。

## FP07：new +cmd 的嵌入命令被错误拆分（4 条）

new +setlocal\ previewwindow|… 的 +cmd 转义、命令分隔与文件名有各自上下文；后续选项和 [Document] 不应作为错误的 setlocal 选项。Vim 实际执行并验证了相关窗口／缓冲选项。

- #49 [$DEV/coc.nvim/autoload/coc/ui.vim:175:48](</Users/chemzqm/vim-dev/coc.nvim/autoload/coc/ui.vim:175>) — vim/E518
- #50 [$DEV/coc.nvim/autoload/coc/ui.vim:175:73](</Users/chemzqm/vim-dev/coc.nvim/autoload/coc/ui.vim:175>) — vim/E518
- #51 [$DEV/coc.nvim/autoload/coc/ui.vim:175:94](</Users/chemzqm/vim-dev/coc.nvim/autoload/coc/ui.vim:175>) — vim/E518
- #52 [$DEV/coc.nvim/autoload/coc/ui.vim:175:101](</Users/chemzqm/vim-dev/coc.nvim/autoload/coc/ui.vim:175>) — vim/E518

[验证案例与证据](findings.md#fp07)。

## FP08：Vim9 autoload 调用被当成非法命令（2 条）

modula2#SetDialect(...) 是合法的 Vim9 隐式函数调用，不是命令修饰符。使用隔离 runtime 中的无副作用同名 autoload 函数验证两种调用形态。

- #58 [$VIMRUNTIME/autoload/dist/ft.vim:901:3](</usr/local/share/vim/vim92/autoload/dist/ft.vim:901>) — vim/E476
- #84 [$VIMRUNTIME/synmenu.vim:19:5](</usr/local/share/vim/vim92/synmenu.vim:19>) — vim/E476

[验证案例与证据](findings.md#fp08)。

## FP09：逻辑表达式类型推导错误（3 条）

这些 &&／|| 表达式及 0、1 布尔上下文在 Vim 中合法，表达式结果可用于 bool 返回值／条件；不可扩大为任意 number 都能当 bool。

- #59 [$VIMRUNTIME/autoload/dist/vim9.vim:13:10](</usr/local/share/vim/vim92/autoload/dist/vim9.vim:13>) — vim/E1012
- #70 [$VIMRUNTIME/indent/mp.vim:100:10](</usr/local/share/vim/vim92/indent/mp.vim:100>) — vim/E1012
- #86 [$VIMRUNTIME/syntax/2html.vim:1592:34](</usr/local/share/vim/vim92/syntax/2html.vim:1592>) — vim/E1012

[验证案例与证据](findings.md#fp09)。

## FP10：map 的容器元素类型转换被拒绝（2 条）

官方 vimindent 中 map 把字典的 list&lt;string&gt; 值或复制列表的 list&lt;string&gt; 元素变成 string，实际 Vim 允许这些具体操作；分析器错误要求输出仍匹配输入元素类型。

- #60 [$VIMRUNTIME/autoload/dist/vimindent.vim:180:7](</usr/local/share/vim/vim92/autoload/dist/vimindent.vim:180>) — vim/E1012
- #61 [$VIMRUNTIME/autoload/dist/vimindent.vim:262:11](</usr/local/share/vim/vim92/autoload/dist/vimindent.vim:262>) — vim/E1012

[验证案例与证据](findings.md#fp10)。

## FP11：解构中的重复丢弃占位符（2 条）

const [_, matched_line, matched_col, _, _] 中的 _ 是丢弃占位符，不产生多个同名变量。

- #62 [$VIMRUNTIME/autoload/racket.vim:108:42](</usr/local/share/vim/vim92/autoload/racket.vim:108>) — vim/E1017
- #63 [$VIMRUNTIME/autoload/racket.vim:108:45](</usr/local/share/vim/vim92/autoload/racket.vim:108>) — vim/E1017

[验证案例与证据](findings.md#fp11)。

## FP12：带类型的 for 解构错误比较整行类型（2 条）

for [paths: list&lt;string&gt;, Action: func: any] 等应逐元素解构 list&lt;any&gt; 行；不能把整行 list&lt;any&gt; 与某一个绑定的 func／string 类型比较。

- #64 [$VIMRUNTIME/ftplugin/java.vim:404:50](</usr/local/share/vim/vim92/ftplugin/java.vim:404>) — vim/E1012
- #78 [$VIMRUNTIME/pack/dist/opt/helptoc/autoload/helptoc.vim:579:51](</usr/local/share/vim/vim92/pack/dist/opt/helptoc/autoload/helptoc.vim:579>) — vim/E1012

[验证案例与证据](findings.md#fp12)。

## FP13：未考虑 delfunction 后重新定义（1 条）

lua.vim 是 legacy 根文件：先定义传统 s:LuaFold 函数，再删除，再以 def 重建，Vim 接受。另存反例证明删除 Vim9 根脚本 def 会真正触发 E1084，不能无条件放宽重定义。

- #65 [$VIMRUNTIME/ftplugin/lua.vim:149:5](</usr/local/share/vim/vim92/ftplugin/lua.vim:149>) — vim/E1073

[验证案例与证据](findings.md#fp13)。

## FP14：映射 RHS 的 &lt;Bar&gt; 和续行被当成选项（2 条）

man.vim 的 &lt;Bar&gt; 属于映射右侧，续行后仍是映射内容，不是 setlocal 的 Bar 选项。案例检查 maparg() 的实际结果。

- #66 [$VIMRUNTIME/ftplugin/man.vim:50:53](</usr/local/share/vim/vim92/ftplugin/man.vim:50>) — vim/E518
- #67 [$VIMRUNTIME/ftplugin/man.vim:51:4](</usr/local/share/vim/vim92/ftplugin/man.vim:51>) — vim/E518

[验证案例与证据](findings.md#fp14)。

## FP15：map 回调函数类型被错误拒绝（5 条）

官方脚本的 number→string、dict→string、number→dict、string→number 回调及 str2nr 回调均可在 Vim9 编译函数内执行；分析器对 map 第二参数的 Funcref 类型判断过严。

- #68 [$VIMRUNTIME/import/dist/vimhighlight.vim:118:35](</usr/local/share/vim/vim92/import/dist/vimhighlight.vim:118>) — vim/E1013
- #79 [$VIMRUNTIME/pack/dist/opt/helptoc/autoload/helptoc.vim:857:19](</usr/local/share/vim/vim92/pack/dist/opt/helptoc/autoload/helptoc.vim:857>) — vim/E1013
- #80 [$VIMRUNTIME/pack/dist/opt/helptoc/autoload/helptoc.vim:1127:44](</usr/local/share/vim/vim92/pack/dist/opt/helptoc/autoload/helptoc.vim:1127>) — vim/E1013
- #81 [$VIMRUNTIME/pack/dist/opt/helptoc/autoload/helptoc.vim:1247:19](</usr/local/share/vim/vim92/pack/dist/opt/helptoc/autoload/helptoc.vim:1247>) — vim/E1013
- #85 [$VIMRUNTIME/syntax/2html.vim:1114:78](</usr/local/share/vim/vim92/syntax/2html.vim:1114>) — vim/E1013

[验证案例与证据](findings.md#fp15)。

## FP16：用户命令的引用参数占位符未展开（2 条）

Cfilter／Lfilter 调用 Qf_filter(true/false, &lt;q-args&gt;, &lt;q-bang&gt;) 时会展开并引用参数；不是实参不足。案例定义并实际调用带 bang 的两个命令。

- #72 [$VIMRUNTIME/pack/dist/opt/cfilter/plugin/cfilter.vim:69:33](</usr/local/share/vim/vim92/pack/dist/opt/cfilter/plugin/cfilter.vim:69>) — vim/E119
- #73 [$VIMRUNTIME/pack/dist/opt/cfilter/plugin/cfilter.vim:70:33](</usr/local/share/vim/vim92/pack/dist/opt/cfilter/plugin/cfilter.vim:70>) — vim/E119

[验证案例与证据](findings.md#fp16)。

## FP17：函数型选项被当作普通 string 赋值（1 条）

operatorfunc 虽以 string 形式存储，却允许 Funcref／lambda，Vim 会转换为函数名；不能直接以 string 与 func 不一致报错。

- #74 [$VIMRUNTIME/pack/dist/opt/comment/autoload/comment.vim:15:15](</usr/local/share/vim/vim92/pack/dist/opt/comment/autoload/comment.vim:15>) — vim/E1012

[验证案例与证据](findings.md#fp17)。

## FP18：多行参数末尾逗号误报空白（1 条）

函数实参最后的逗号后跟换行，再接右括号合法；换行不能被当作缺失空白。

- #77 [$VIMRUNTIME/pack/dist/opt/helptoc/autoload/helptoc.vim:878:21](</usr/local/share/vim/vim92/pack/dist/opt/helptoc/autoload/helptoc.vim:878>) — vim/E1069

[验证案例与证据](findings.md#fp18)。
