# 28 条非误报逐项记录

编号沿用原审计 ID，不等于 [errors.txt](errors.txt) 的当前行号。行、列均从 1 开始；列为 UTF-8 字节列。原文件变化时请核对 source-manifest.json。

## FIXTURE01：测试夹具非法片段（3 条）

vim-matchup 的匹配测试输入包含 account.／parenthesis／backslashes 等帮助正文；作为 Vim 命令执行确实是 E492，但不属于正常插件入口。

| 原 ID | 文件位置 | 诊断 |
|---|---|---|
| 15 | [/Users/chemzqm/.vim/bundle/vim-matchup/test/issues/10/legacy.vim:24:3](</Users/chemzqm/.vim/bundle/vim-matchup/test/issues/10/legacy.vim:24>) | vim/E492 Not an editor command: account.  Thus in |
| 16 | [/Users/chemzqm/.vim/bundle/vim-matchup/test/issues/10/legacy.vim:25:3](</Users/chemzqm/.vim/bundle/vim-matchup/test/issues/10/legacy.vim:25>) | vim/E492 Not an editor command: parenthesis match.  When included |
| 17 | [/Users/chemzqm/.vim/bundle/vim-matchup/test/issues/10/legacy.vim:26:3](</Users/chemzqm/.vim/bundle/vim-matchup/test/issues/10/legacy.vim:26>) | vim/E492 Not an editor command: backslashes, which is Vi compatible. |

验证案例：[fixture-help-text](oracle/cases/fixture-help-text.vim)（[原始结果](oracle/fixture-help-text.json)）。

## FIXTURE02：测试夹具非法片段（1 条）

echom "hello andy" " CURSOR 中第二个双引号不是这里的普通尾注释，Vim 实际产生 E114。

| 原 ID | 文件位置 | 诊断 |
|---|---|---|
| 18 | [/Users/chemzqm/.vim/bundle/vim-matchup/test/issues/16/any.vim:5:26](</Users/chemzqm/.vim/bundle/vim-matchup/test/issues/16/any.vim:5>) | vim/E114 Missing double quote |

验证案例：[fixture-unclosed-echo-string](oracle/cases/fixture-unclosed-echo-string.vim)（[原始结果](oracle/fixture-unclosed-echo-string.json)）。

## FIXTURE03：测试夹具非法片段（8 条）

forwhile／parts／tabs 是不完整匹配测试输入，没有包住这些 return 的有效函数声明；Vim 的函数外 return 确实触发 E133。

| 原 ID | 文件位置 | 诊断 |
|---|---|---|
| 19 | [/Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/forwhile.vim:16:15](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/forwhile.vim:16>) | vim/E133 :return not inside a function |
| 20 | [/Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/forwhile.vim:21:11](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/forwhile.vim:21>) | vim/E133 :return not inside a function |
| 21 | [/Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/forwhile.vim:25:5](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/forwhile.vim:25>) | vim/E133 :return not inside a function |
| 22 | [/Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/forwhile.vim:27:33](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/forwhile.vim:27>) | vim/E133 :return not inside a function |
| 23 | [/Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/forwhile.vim:29:5](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/forwhile.vim:29>) | vim/E133 :return not inside a function |
| 25 | [/Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/parts.vim:4:3](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/parts.vim:4>) | vim/E133 :return not inside a function |
| 26 | [/Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/tabs.vim:9:5](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/tabs.vim:9>) | vim/E133 :return not inside a function |
| 27 | [/Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/tabs.vim:14:3](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/tabs.vim:14>) | vim/E133 :return not inside a function |

验证案例：[fixture-top-level-return](oracle/cases/fixture-top-level-return.vim)（[原始结果](oracle/fixture-top-level-return.json)）。

## FIXTURE04：测试夹具非法片段（1 条）

parts.vim 的片段含无匹配循环开始的 endwhile，Vim 确实报 E588。

| 原 ID | 文件位置 | 诊断 |
|---|---|---|
| 24 | [/Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/parts.vim:7:1](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/parts.vim:7>) | vim/E588 :endwhile without :while |

验证案例：[fixture-unmatched-endwhile](oracle/cases/fixture-unmatched-endwhile.vim)（[原始结果](oracle/fixture-unmatched-endwhile.json)）。

## ENV01：旧会话兼容错误（4 条）

四份 session 的 setlocal fillchars=msgsep:- 在当前 Vim 9.2.1015 实际产生 E474，不是误报；可能只在加载这些旧 session 时出现。

| 原 ID | 文件位置 | 诊断 |
|---|---|---|
| 39 | [/Users/chemzqm/.vim/sessions/coc-list.vim:66:20](</Users/chemzqm/.vim/sessions/coc-list.vim:66>) | vim/E474 Invalid argument |
| 42 | [/Users/chemzqm/.vim/sessions/mini.vim:65:20](</Users/chemzqm/.vim/sessions/mini.vim:65>) | vim/E474 Invalid argument |
| 44 | [/Users/chemzqm/.vim/sessions/python.vim:61:20](</Users/chemzqm/.vim/sessions/python.vim:61>) | vim/E474 Invalid argument |
| 46 | [/Users/chemzqm/.vim/sessions/someroject-rust.vim:73:20](</Users/chemzqm/.vim/sessions/someroject-rust.vim:73>) | vim/E474 Invalid argument |

验证案例：[session-fillchars-msgsep](oracle/cases/session-fillchars-msgsep.vim)（[原始结果](oracle/session-fillchars-msgsep.json)）。

## ENV02：旧会话兼容错误（1 条）

旧 view 中 setlocal macmeta 在本机当前 Vim 实际产生 E518；不能仅凭 macOS 平台推断选项存在。

| 原 ID | 文件位置 | 诊断 |
|---|---|---|
| 48 | [/Users/chemzqm/.vim/view/~=+.vim=+vimrc=+unite.vim=1.vim:79:10](</Users/chemzqm/.vim/view/~=+.vim=+vimrc=+unite.vim=1.vim:79>) | vim/E518 Unknown option: macmeta |

验证案例：[view-macmeta](oracle/cases/view-macmeta.vim)（[原始结果](oracle/view-macmeta.json)）。

## REAL01：条件性真实错误（5 条）

s:report_error(report, advises) 需要两个参数，五处错误分支只传一个；实际进入调用时产生 E119。未调用完整 health 检查，不代表当前启动会触发。

| 原 ID | 文件位置 | 诊断 |
|---|---|---|
| 53 | [/Users/chemzqm/vim-dev/coc.nvim/autoload/health/coc.vim:31:12](</Users/chemzqm/vim-dev/coc.nvim/autoload/health/coc.vim:31>) | vim/E119 Not enough arguments for function: s:report_error |
| 54 | [/Users/chemzqm/vim-dev/coc.nvim/autoload/health/coc.vim:48:10](</Users/chemzqm/vim-dev/coc.nvim/autoload/health/coc.vim:48>) | vim/E119 Not enough arguments for function: s:report_error |
| 55 | [/Users/chemzqm/vim-dev/coc.nvim/autoload/health/coc.vim:53:10](</Users/chemzqm/vim-dev/coc.nvim/autoload/health/coc.vim:53>) | vim/E119 Not enough arguments for function: s:report_error |
| 56 | [/Users/chemzqm/vim-dev/coc.nvim/autoload/health/coc.vim:58:10](</Users/chemzqm/vim-dev/coc.nvim/autoload/health/coc.vim:58>) | vim/E119 Not enough arguments for function: s:report_error |
| 57 | [/Users/chemzqm/vim-dev/coc.nvim/autoload/health/coc.vim:84:10](</Users/chemzqm/vim-dev/coc.nvim/autoload/health/coc.vim:84>) | vim/E119 Not enough arguments for function: s:report_error |

验证案例：[required-health-argument](oracle/cases/required-health-argument.vim)（[原始结果](oracle/required-health-argument.json)）。

## REAL02：条件性真实错误（1 条）

indent/sml.vim 的传统函数内直接写 cursor(lnum,1)，调用到这一行时 Vim 报 E492。

| 原 ID | 文件位置 | 诊断 |
|---|---|---|
| 71 | [/usr/local/share/vim/vim92/indent/sml.vim:153:3](</usr/local/share/vim/vim92/indent/sml.vim:153>) | vim/E492 Not an editor command: cursor(lnum,1) |

验证案例：[legacy-call-without-call](oracle/cases/legacy-call-without-call.vim)（[原始结果](oracle/legacy-call-without-call.json)）。

## REAL03：条件性真实错误（2 条）

editorconfig 的传统函数内直接写 setbufvar(...)，相同形态实际执行会报 E492。第一处受 Windows／shell 分支限制，恢复分支又依赖此前状态，不能当成当前 macOS 启动故障。

| 原 ID | 文件位置 | 诊断 |
|---|---|---|
| 75 | [/usr/local/share/vim/vim92/pack/dist/opt/editorconfig/plugin/editorconfig.vim:104:9](</usr/local/share/vim/vim92/pack/dist/opt/editorconfig/plugin/editorconfig.vim:104>) | vim/E492 Not an editor command: setbufvar(a:bufnr, '&shellslash', 0) |
| 76 | [/usr/local/share/vim/vim92/pack/dist/opt/editorconfig/plugin/editorconfig.vim:111:9](</usr/local/share/vim/vim92/pack/dist/opt/editorconfig/plugin/editorconfig.vim:111>) | vim/E492 Not an editor command: setbufvar(a:bufnr, '&shellslash', s:old_shellslash) |

验证案例：[legacy-setbufvar-without-call](oracle/cases/legacy-setbufvar-without-call.vim)（[原始结果](oracle/legacy-setbufvar-without-call.json)）。

## REAL04：条件性真实错误（2 条）

netrw 的目录切换失败恢复路径中 let a:newdir = ... 会触发 E46；正常切换不代表会进入这些分支。

| 原 ID | 文件位置 | 诊断 |
|---|---|---|
| 82 | [/usr/local/share/vim/vim92/pack/dist/opt/netrw/autoload/netrw.vim:9304:17](</usr/local/share/vim/vim92/pack/dist/opt/netrw/autoload/netrw.vim:9304>) | vim/E46 Cannot change read-only variable "a:newdir" |
| 83 | [/usr/local/share/vim/vim92/pack/dist/opt/netrw/autoload/netrw.vim:9308:17](</usr/local/share/vim/vim92/pack/dist/opt/netrw/autoload/netrw.vim:9308>) | vim/E46 Cannot change read-only variable "a:newdir" |

验证案例：[legacy-readonly-argument](oracle/cases/legacy-readonly-argument.vim)（[原始结果](oracle/legacy-readonly-argument.json)）。
