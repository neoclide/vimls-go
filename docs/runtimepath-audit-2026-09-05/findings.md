# 86 条诊断逐项分类

编号是 [errors.txt](errors.txt) 的原始行号；列号为从 1 开始的 UTF-8 字节列。诊断排序保留扫描器输出，未按源码行号重新排列。

路径缩写：`$VIMHOME=/Users/chemzqm/.vim`，`$DEV=/Users/chemzqm/vim-dev`，`$VIMRUNTIME=/usr/local/share/vim/vim92`。点击位置可打开原始文件；源码以后变化时请对照本次证据。

每条编号恰好归入一类。除 5 条模板语言问题外，81 条都有对应的最小 Vim 验证案例；同类诊断可共享一个案例，不代表完整执行了每个原始文件。

| # | 原始位置 | 诊断 | 分类／原因编号 |
|---|---|---|---|
| 1 | [$VIMHOME/autoload/coc/t.vim:86:36](</Users/chemzqm/.vim/autoload/coc/t.vim:86>) | vim/E1167 Argument name shadows existing variable: cmd | 误报 [FP01](#fp01) |
| 2 | [$VIMHOME/autoload/x.vim:59:36](</Users/chemzqm/.vim/autoload/x.vim:59>) | vim/E1167 Argument name shadows existing variable: cmd | 误报 [FP01](#fp01) |
| 3 | [$VIMHOME/bundle/splitjoin.vim/spec/support/rust.vim/compiler/cargo.vim:19:43](</Users/chemzqm/.vim/bundle/splitjoin.vim/spec/support/rust.vim/compiler/cargo.vim:19>) | vim/E518 Unknown option: &lt;args&gt; | 误报 [FP02](#fp02) |
| 4 | [$VIMHOME/bundle/splitjoin.vim/spec/support/rust.vim/compiler/rustc.vim:18:43](</Users/chemzqm/.vim/bundle/splitjoin.vim/spec/support/rust.vim/compiler/rustc.vim:18>) | vim/E518 Unknown option: &lt;args&gt; | 误报 [FP02](#fp02) |
| 5 | [$VIMHOME/bundle/splitjoin.vim/spec/support/vim-elixir/compiler/credo.vim:7:43](</Users/chemzqm/.vim/bundle/splitjoin.vim/spec/support/vim-elixir/compiler/credo.vim:7>) | vim/E518 Unknown option: &lt;args&gt; | 误报 [FP02](#fp02) |
| 6 | [$VIMHOME/bundle/splitjoin.vim/spec/support/vim-elixir/compiler/exunit.vim:7:41](</Users/chemzqm/.vim/bundle/splitjoin.vim/spec/support/vim-elixir/compiler/exunit.vim:7>) | vim/E518 Unknown option: &lt;args&gt; | 误报 [FP02](#fp02) |
| 7 | [$VIMHOME/bundle/splitjoin.vim/spec/support/vim-elixir/compiler/mix.vim:7:43](</Users/chemzqm/.vim/bundle/splitjoin.vim/spec/support/vim-elixir/compiler/mix.vim:7>) | vim/E518 Unknown option: &lt;args&gt; | 误报 [FP02](#fp02) |
| 8 | [$VIMHOME/bundle/splitjoin.vim/spec/support/vim-javascript/compiler/eslint.vim:12:42](</Users/chemzqm/.vim/bundle/splitjoin.vim/spec/support/vim-javascript/compiler/eslint.vim:12>) | vim/E518 Unknown option: &lt;args&gt; | 误报 [FP02](#fp02) |
| 9 | [$VIMHOME/bundle/swift.vim/compiler/swift.vim:17:43](</Users/chemzqm/.vim/bundle/swift.vim/compiler/swift.vim:17>) | vim/E518 Unknown option: &lt;args&gt; | 误报 [FP02](#fp02) |
| 10 | [$VIMHOME/bundle/typescript-vim/compiler/typescript.vim:15:42](</Users/chemzqm/.vim/bundle/typescript-vim/compiler/typescript.vim:15>) | vim/E518 Unknown option: &lt;args&gt; | 误报 [FP02](#fp02) |
| 11 | [$VIMHOME/bundle/vim-fugitive/autoload/fugitive.vim:484:62](</Users/chemzqm/.vim/bundle/vim-fugitive/autoload/fugitive.vim:484>) | vim/E729 Using a Funcref as a String | 误报 [FP03](#fp03) |
| 12 | [$VIMHOME/bundle/vim-fugitive/autoload/fugitive.vim:2078:21](</Users/chemzqm/.vim/bundle/vim-fugitive/autoload/fugitive.vim:2078>) | vim/E729 Using a Funcref as a String | 误报 [FP03](#fp03) |
| 13 | [$VIMHOME/bundle/vim-fugitive/autoload/fugitive.vim:2084:21](</Users/chemzqm/.vim/bundle/vim-fugitive/autoload/fugitive.vim:2084>) | vim/E729 Using a Funcref as a String | 误报 [FP03](#fp03) |
| 14 | [$VIMHOME/bundle/vim-fugitive/autoload/fugitive.vim:2105:60](</Users/chemzqm/.vim/bundle/vim-fugitive/autoload/fugitive.vim:2105>) | vim/E729 Using a Funcref as a String | 误报 [FP03](#fp03) |
| 15 | [$VIMHOME/bundle/vim-matchup/test/issues/10/legacy.vim:24:3](</Users/chemzqm/.vim/bundle/vim-matchup/test/issues/10/legacy.vim:24>) | vim/E492 Not an editor command: account.  Thus in | 测试夹具非法片段 [FIXTURE01](#fixture01) |
| 16 | [$VIMHOME/bundle/vim-matchup/test/issues/10/legacy.vim:25:3](</Users/chemzqm/.vim/bundle/vim-matchup/test/issues/10/legacy.vim:25>) | vim/E492 Not an editor command: parenthesis match.  When included | 测试夹具非法片段 [FIXTURE01](#fixture01) |
| 17 | [$VIMHOME/bundle/vim-matchup/test/issues/10/legacy.vim:26:3](</Users/chemzqm/.vim/bundle/vim-matchup/test/issues/10/legacy.vim:26>) | vim/E492 Not an editor command: backslashes, which is Vi compatible. | 测试夹具非法片段 [FIXTURE01](#fixture01) |
| 18 | [$VIMHOME/bundle/vim-matchup/test/issues/16/any.vim:5:26](</Users/chemzqm/.vim/bundle/vim-matchup/test/issues/16/any.vim:5>) | vim/E114 Missing double quote | 测试夹具非法片段 [FIXTURE02](#fixture02) |
| 19 | [$VIMHOME/bundle/vim-matchup/test/legacy/forwhile.vim:16:15](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/forwhile.vim:16>) | vim/E133 :return not inside a function | 测试夹具非法片段 [FIXTURE03](#fixture03) |
| 20 | [$VIMHOME/bundle/vim-matchup/test/legacy/forwhile.vim:21:11](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/forwhile.vim:21>) | vim/E133 :return not inside a function | 测试夹具非法片段 [FIXTURE03](#fixture03) |
| 21 | [$VIMHOME/bundle/vim-matchup/test/legacy/forwhile.vim:25:5](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/forwhile.vim:25>) | vim/E133 :return not inside a function | 测试夹具非法片段 [FIXTURE03](#fixture03) |
| 22 | [$VIMHOME/bundle/vim-matchup/test/legacy/forwhile.vim:27:33](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/forwhile.vim:27>) | vim/E133 :return not inside a function | 测试夹具非法片段 [FIXTURE03](#fixture03) |
| 23 | [$VIMHOME/bundle/vim-matchup/test/legacy/forwhile.vim:29:5](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/forwhile.vim:29>) | vim/E133 :return not inside a function | 测试夹具非法片段 [FIXTURE03](#fixture03) |
| 24 | [$VIMHOME/bundle/vim-matchup/test/legacy/parts.vim:7:1](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/parts.vim:7>) | vim/E588 :endwhile without :while | 测试夹具非法片段 [FIXTURE04](#fixture04) |
| 25 | [$VIMHOME/bundle/vim-matchup/test/legacy/parts.vim:4:3](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/parts.vim:4>) | vim/E133 :return not inside a function | 测试夹具非法片段 [FIXTURE03](#fixture03) |
| 26 | [$VIMHOME/bundle/vim-matchup/test/legacy/tabs.vim:9:5](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/tabs.vim:9>) | vim/E133 :return not inside a function | 测试夹具非法片段 [FIXTURE03](#fixture03) |
| 27 | [$VIMHOME/bundle/vim-matchup/test/legacy/tabs.vim:14:3](</Users/chemzqm/.vim/bundle/vim-matchup/test/legacy/tabs.vim:14>) | vim/E133 :return not inside a function | 测试夹具非法片段 [FIXTURE03](#fixture03) |
| 28 | [$VIMHOME/bundle/vim-scala/compiler/sbt.vim:14:41](</Users/chemzqm/.vim/bundle/vim-scala/compiler/sbt.vim:14>) | vim/E518 Unknown option: &lt;args&gt; | 误报 [FP02](#fp02) |
| 29 | [$VIMHOME/bundle/vim-scala/ftplugin/scala.xpt.vim:16:1](</Users/chemzqm/.vim/bundle/vim-scala/ftplugin/scala.xpt.vim:16>) | vim/E492 Not an editor command: trait `trait^Component { | 模板上下文排除 [DSL01](#dsl01) |
| 30 | [$VIMHOME/bundle/vim-scala/ftplugin/scala.xpt.vim:17:2](</Users/chemzqm/.vim/bundle/vim-scala/ftplugin/scala.xpt.vim:17>) | vim/E492 Not an editor command: trait `trait^ { | 模板上下文排除 [DSL01](#dsl01) |
| 31 | [$VIMHOME/bundle/vim-scala/ftplugin/scala.xpt.vim:21:2](</Users/chemzqm/.vim/bundle/vim-scala/ftplugin/scala.xpt.vim:21>) | vim/E492 Not an editor command: val `trait^SV('(.)', '\l\1', '')^^: `trait^ | 模板上下文排除 [DSL01](#dsl01) |
| 32 | [$VIMHOME/bundle/vim-scala/ftplugin/scala.xpt.vim:24:1](</Users/chemzqm/.vim/bundle/vim-scala/ftplugin/scala.xpt.vim:24>) | vim/E492 Not an editor command: trait `derived^`trait^Component extends `trait^Component { | 模板上下文排除 [DSL01](#dsl01) |
| 33 | [$VIMHOME/bundle/vim-scala/ftplugin/scala.xpt.vim:26:2](</Users/chemzqm/.vim/bundle/vim-scala/ftplugin/scala.xpt.vim:26>) | vim/E492 Not an editor command: override lazy val `trait^SV('(.)', '\l\1', '')^^ = new `trait^ { | 模板上下文排除 [DSL01](#dsl01) |
| 34 | [$VIMHOME/bundle/vimtex/autoload/vimtex/parser/toc.vim:176:17](</Users/chemzqm/.vim/bundle/vimtex/autoload/vimtex/parser/toc.vim:176>) | vim/E884 Function name cannot contain a colon: l:matcher.get_entry | 误报 [FP04](#fp04) |
| 35 | [$VIMHOME/bundle/vimtex/autoload/vimtex/ui/nvim.vim:14:12](</Users/chemzqm/.vim/bundle/vimtex/autoload/vimtex/ui/nvim.vim:14>) | vim/E884 Function name cannot contain a colon: l:popup_cfg.highlight | 误报 [FP04](#fp04) |
| 36 | [$VIMHOME/bundle/vimtex/autoload/vimtex/ui/nvim.vim:50:12](</Users/chemzqm/.vim/bundle/vimtex/autoload/vimtex/ui/nvim.vim:50>) | vim/E884 Function name cannot contain a colon: l:popup_cfg.highlight | 误报 [FP04](#fp04) |
| 37 | [$VIMHOME/bundle/vimtex/autoload/vimtex/ui/nvim.vim:114:12](</Users/chemzqm/.vim/bundle/vimtex/autoload/vimtex/ui/nvim.vim:114>) | vim/E884 Function name cannot contain a colon: l:popup_cfg.highlight | 误报 [FP04](#fp04) |
| 38 | [$VIMHOME/bundle/vimtex/autoload/vimtex/ui/nvim.vim:230:12](</Users/chemzqm/.vim/bundle/vimtex/autoload/vimtex/ui/nvim.vim:230>) | vim/E884 Function name cannot contain a colon: l:popup.close | 误报 [FP04](#fp04) |
| 39 | [$VIMHOME/sessions/coc-list.vim:66:20](</Users/chemzqm/.vim/sessions/coc-list.vim:66>) | vim/E474 Invalid argument | 旧会话兼容错误 [ENV01](#env01) |
| 40 | [$VIMHOME/sessions/coc-list.vim:96:32](</Users/chemzqm/.vim/sessions/coc-list.vim:96>) | vim/E518 Unknown option: ,trail:\x16�\x16�\x16�,extends:#,nbsp:. | 误报 [FP05](#fp05) |
| 41 | [$VIMHOME/sessions/default.vim:304:3](</Users/chemzqm/.vim/sessions/default.vim:304>) | vim/E1082 command modifier without command | 误报 [FP06](#fp06) |
| 42 | [$VIMHOME/sessions/mini.vim:65:20](</Users/chemzqm/.vim/sessions/mini.vim:65>) | vim/E474 Invalid argument | 旧会话兼容错误 [ENV01](#env01) |
| 43 | [$VIMHOME/sessions/mini.vim:95:32](</Users/chemzqm/.vim/sessions/mini.vim:95>) | vim/E518 Unknown option: ,trail:\x16�\x16�\x16�,extends:#,nbsp:. | 误报 [FP05](#fp05) |
| 44 | [$VIMHOME/sessions/python.vim:61:20](</Users/chemzqm/.vim/sessions/python.vim:61>) | vim/E474 Invalid argument | 旧会话兼容错误 [ENV01](#env01) |
| 45 | [$VIMHOME/sessions/python.vim:91:32](</Users/chemzqm/.vim/sessions/python.vim:91>) | vim/E518 Unknown option: ,trail:\x16�\x16�\x16�,extends:#,nbsp:. | 误报 [FP05](#fp05) |
| 46 | [$VIMHOME/sessions/someroject-rust.vim:73:20](</Users/chemzqm/.vim/sessions/someroject-rust.vim:73>) | vim/E474 Invalid argument | 旧会话兼容错误 [ENV01](#env01) |
| 47 | [$VIMHOME/sessions/someroject-rust.vim:103:32](</Users/chemzqm/.vim/sessions/someroject-rust.vim:103>) | vim/E518 Unknown option: ,trail:\x16�\x16�\x16�,extends:#,nbsp:. | 误报 [FP05](#fp05) |
| 48 | [$VIMHOME/view/~=+.vim=+vimrc=+unite.vim=1.vim:79:10](</Users/chemzqm/.vim/view/~=+.vim=+vimrc=+unite.vim=1.vim:79>) | vim/E518 Unknown option: macmeta | 旧会话兼容错误 [ENV02](#env02) |
| 49 | [$DEV/coc.nvim/autoload/coc/ui.vim:175:48](</Users/chemzqm/vim-dev/coc.nvim/autoload/coc/ui.vim:175>) | vim/E518 Unknown option: \ buftype=nofile | 误报 [FP07](#fp07) |
| 50 | [$DEV/coc.nvim/autoload/coc/ui.vim:175:73](</Users/chemzqm/vim-dev/coc.nvim/autoload/coc/ui.vim:175>) | vim/E518 Unknown option: \ noswapfile | 误报 [FP07](#fp07) |
| 51 | [$DEV/coc.nvim/autoload/coc/ui.vim:175:94](</Users/chemzqm/vim-dev/coc.nvim/autoload/coc/ui.vim:175>) | vim/E518 Unknown option: \ wrap | 误报 [FP07](#fp07) |
| 52 | [$DEV/coc.nvim/autoload/coc/ui.vim:175:101](</Users/chemzqm/vim-dev/coc.nvim/autoload/coc/ui.vim:175>) | vim/E518 Unknown option: [Document] | 误报 [FP07](#fp07) |
| 53 | [$DEV/coc.nvim/autoload/health/coc.vim:31:12](</Users/chemzqm/vim-dev/coc.nvim/autoload/health/coc.vim:31>) | vim/E119 Not enough arguments for function: s:report_error | 条件性真实错误 [REAL01](#real01) |
| 54 | [$DEV/coc.nvim/autoload/health/coc.vim:48:10](</Users/chemzqm/vim-dev/coc.nvim/autoload/health/coc.vim:48>) | vim/E119 Not enough arguments for function: s:report_error | 条件性真实错误 [REAL01](#real01) |
| 55 | [$DEV/coc.nvim/autoload/health/coc.vim:53:10](</Users/chemzqm/vim-dev/coc.nvim/autoload/health/coc.vim:53>) | vim/E119 Not enough arguments for function: s:report_error | 条件性真实错误 [REAL01](#real01) |
| 56 | [$DEV/coc.nvim/autoload/health/coc.vim:58:10](</Users/chemzqm/vim-dev/coc.nvim/autoload/health/coc.vim:58>) | vim/E119 Not enough arguments for function: s:report_error | 条件性真实错误 [REAL01](#real01) |
| 57 | [$DEV/coc.nvim/autoload/health/coc.vim:84:10](</Users/chemzqm/vim-dev/coc.nvim/autoload/health/coc.vim:84>) | vim/E119 Not enough arguments for function: s:report_error | 条件性真实错误 [REAL01](#real01) |
| 58 | [$VIMRUNTIME/autoload/dist/ft.vim:901:3](</usr/local/share/vim/vim92/autoload/dist/ft.vim:901>) | vim/E476 Invalid command: modula2#SetDialect(dialect, extension) | 误报 [FP08](#fp08) |
| 59 | [$VIMRUNTIME/autoload/dist/vim9.vim:13:10](</usr/local/share/vim/vim92/autoload/dist/vim9.vim:13>) | vim/E1012 Type mismatch; expected bool but got number | 误报 [FP09](#fp09) |
| 60 | [$VIMRUNTIME/autoload/dist/vimindent.vim:180:7](</usr/local/share/vim/vim92/autoload/dist/vimindent.vim:180>) | vim/E1012 Type mismatch; expected list&lt;string&gt; but got string | 误报 [FP10](#fp10) |
| 61 | [$VIMRUNTIME/autoload/dist/vimindent.vim:262:11](</usr/local/share/vim/vim92/autoload/dist/vimindent.vim:262>) | vim/E1012 Type mismatch; expected list&lt;string&gt; but got string | 误报 [FP10](#fp10) |
| 62 | [$VIMRUNTIME/autoload/racket.vim:108:42](</usr/local/share/vim/vim92/autoload/racket.vim:108>) | vim/E1017 Variable already declared: _ | 误报 [FP11](#fp11) |
| 63 | [$VIMRUNTIME/autoload/racket.vim:108:45](</usr/local/share/vim/vim92/autoload/racket.vim:108>) | vim/E1017 Variable already declared: _ | 误报 [FP11](#fp11) |
| 64 | [$VIMRUNTIME/ftplugin/java.vim:404:50](</usr/local/share/vim/vim92/ftplugin/java.vim:404>) | vim/E1012 Type mismatch; expected func but got list&lt;any&gt; | 误报 [FP12](#fp12) |
| 65 | [$VIMRUNTIME/ftplugin/lua.vim:149:5](</usr/local/share/vim/vim92/ftplugin/lua.vim:149>) | vim/E1073 Name already defined: s:LuaFold | 误报 [FP13](#fp13) |
| 66 | [$VIMRUNTIME/ftplugin/man.vim:50:53](</usr/local/share/vim/vim92/ftplugin/man.vim:50>) | vim/E518 Unknown option: Bar | 误报 [FP14](#fp14) |
| 67 | [$VIMRUNTIME/ftplugin/man.vim:51:4](</usr/local/share/vim/vim92/ftplugin/man.vim:51>) | vim/E518 Unknown option: &lt;Bar&gt; | 误报 [FP14](#fp14) |
| 68 | [$VIMRUNTIME/import/dist/vimhighlight.vim:118:35](</usr/local/share/vim/vim92/import/dist/vimhighlight.vim:118>) | vim/E1013 Argument 2: type mismatch, expected string or function but got func&lt;any, number&gt; | 误报 [FP15](#fp15) |
| 69 | [$VIMRUNTIME/indent/hare.vim:331:45](</usr/local/share/vim/vim92/indent/hare.vim:331>) | vim/E1167 Argument name shadows existing variable: n | 误报 [FP01](#fp01) |
| 70 | [$VIMRUNTIME/indent/mp.vim:100:10](</usr/local/share/vim/vim92/indent/mp.vim:100>) | vim/E1012 Type mismatch; expected bool but got number | 误报 [FP09](#fp09) |
| 71 | [$VIMRUNTIME/indent/sml.vim:153:3](</usr/local/share/vim/vim92/indent/sml.vim:153>) | vim/E492 Not an editor command: cursor(lnum,1) | 条件性真实错误 [REAL02](#real02) |
| 72 | [$VIMRUNTIME/pack/dist/opt/cfilter/plugin/cfilter.vim:69:33](</usr/local/share/vim/vim92/pack/dist/opt/cfilter/plugin/cfilter.vim:69>) | vim/E119 Not enough arguments for function: Qf_filter | 误报 [FP16](#fp16) |
| 73 | [$VIMRUNTIME/pack/dist/opt/cfilter/plugin/cfilter.vim:70:33](</usr/local/share/vim/vim92/pack/dist/opt/cfilter/plugin/cfilter.vim:70>) | vim/E119 Not enough arguments for function: Qf_filter | 误报 [FP16](#fp16) |
| 74 | [$VIMRUNTIME/pack/dist/opt/comment/autoload/comment.vim:15:15](</usr/local/share/vim/vim92/pack/dist/opt/comment/autoload/comment.vim:15>) | vim/E1012 Type mismatch; expected string but got func&lt;any&gt; | 误报 [FP17](#fp17) |
| 75 | [$VIMRUNTIME/pack/dist/opt/editorconfig/plugin/editorconfig.vim:104:9](</usr/local/share/vim/vim92/pack/dist/opt/editorconfig/plugin/editorconfig.vim:104>) | vim/E492 Not an editor command: setbufvar(a:bufnr, '&shellslash', 0) | 条件性真实错误 [REAL03](#real03) |
| 76 | [$VIMRUNTIME/pack/dist/opt/editorconfig/plugin/editorconfig.vim:111:9](</usr/local/share/vim/vim92/pack/dist/opt/editorconfig/plugin/editorconfig.vim:111>) | vim/E492 Not an editor command: setbufvar(a:bufnr, '&shellslash', s:old_shellslash) | 条件性真实错误 [REAL03](#real03) |
| 77 | [$VIMRUNTIME/pack/dist/opt/helptoc/autoload/helptoc.vim:878:21](</usr/local/share/vim/vim92/pack/dist/opt/helptoc/autoload/helptoc.vim:878>) | vim/E1069 white space required after ',' | 误报 [FP18](#fp18) |
| 78 | [$VIMRUNTIME/pack/dist/opt/helptoc/autoload/helptoc.vim:579:51](</usr/local/share/vim/vim92/pack/dist/opt/helptoc/autoload/helptoc.vim:579>) | vim/E1012 Type mismatch; expected string but got list&lt;any&gt; | 误报 [FP12](#fp12) |
| 79 | [$VIMRUNTIME/pack/dist/opt/helptoc/autoload/helptoc.vim:857:19](</usr/local/share/vim/vim92/pack/dist/opt/helptoc/autoload/helptoc.vim:857>) | vim/E1013 Argument 2: type mismatch, expected string or function but got func&lt;any, dict&lt;any&gt;&gt; | 误报 [FP15](#fp15) |
| 80 | [$VIMRUNTIME/pack/dist/opt/helptoc/autoload/helptoc.vim:1127:44](</usr/local/share/vim/vim92/pack/dist/opt/helptoc/autoload/helptoc.vim:1127>) | vim/E1013 Argument 2: type mismatch, expected string or function but got func&lt;any, number&gt; | 误报 [FP15](#fp15) |
| 81 | [$VIMRUNTIME/pack/dist/opt/helptoc/autoload/helptoc.vim:1247:19](</usr/local/share/vim/vim92/pack/dist/opt/helptoc/autoload/helptoc.vim:1247>) | vim/E1013 Argument 2: type mismatch, expected string or function but got func&lt;any, string&gt; | 误报 [FP15](#fp15) |
| 82 | [$VIMRUNTIME/pack/dist/opt/netrw/autoload/netrw.vim:9304:17](</usr/local/share/vim/vim92/pack/dist/opt/netrw/autoload/netrw.vim:9304>) | vim/E46 Cannot change read-only variable "a:newdir" | 条件性真实错误 [REAL04](#real04) |
| 83 | [$VIMRUNTIME/pack/dist/opt/netrw/autoload/netrw.vim:9308:17](</usr/local/share/vim/vim92/pack/dist/opt/netrw/autoload/netrw.vim:9308>) | vim/E46 Cannot change read-only variable "a:newdir" | 条件性真实错误 [REAL04](#real04) |
| 84 | [$VIMRUNTIME/synmenu.vim:19:5](</usr/local/share/vim/vim92/synmenu.vim:19>) | vim/E476 Invalid command: modula2#SetDialect(dialect) | 误报 [FP08](#fp08) |
| 85 | [$VIMRUNTIME/syntax/2html.vim:1114:78](</usr/local/share/vim/vim92/syntax/2html.vim:1114>) | vim/E1013 Argument 2: type mismatch, expected string or function but got func&lt;any, any&gt; | 误报 [FP15](#fp15) |
| 86 | [$VIMRUNTIME/syntax/2html.vim:1592:34](</usr/local/share/vim/vim92/syntax/2html.vim:1592>) | vim/E1012 Type mismatch; expected bool but got number | 误报 [FP09](#fp09) |

## 分类依据与验证案例

<a id="fp01"></a>

### FP01：初始化表达式中的 lambda 参数同名（3 条）

编号：1、2、69。分类：误报。

声明目标 cmd／循环变量 n 在初始化表达式求值时尚未成为冲突的外层变量；Vim 允许回调参数使用这个名字。这不意味着允许遮蔽任意已存在变量。

验证：[initializer-lambda-shadow.vim](oracle/cases/initializer-lambda-shadow.vim)（[结果](oracle/initializer-lambda-shadow.json)）；[loop-initializer-lambda-shadow.vim](oracle/cases/loop-initializer-lambda-shadow.vim)（[结果](oracle/loop-initializer-lambda-shadow.json)）。

<a id="fp02"></a>

### FP02：用户命令 &lt;args&gt; 被当成选项（9 条）

编号：3、4、5、6、7、8、9、10、28。分类：误报。

CompilerSet 的替换文本在命令调用时展开 &lt;args&gt;；它不是 setlocal 的字面选项。最小案例实际调用 CompilerSet 并断言 shiftwidth。

验证：[command-args-expansion.vim](oracle/cases/command-args-expansion.vim)（[结果](oracle/command-args-expansion.json)）。

<a id="fp03"></a>

### FP03：函数与变量命名空间混淆（4 条）

编号：11、12、13、14。分类：误报。

fugitive 同时定义 s:fnameescape() 函数和 s:fnameescape 字符串变量合法；字符串拼接读取变量，而不是把 Funcref 当字符串。

验证：[function-variable-separate-namespaces.vim](oracle/cases/function-variable-separate-namespaces.vim)（[结果](oracle/function-variable-separate-namespaces.json)）。

<a id="fp04"></a>

### FP04：局部字典方法被当作非法函数名（5 条）

编号：34、35、36、37、38。分类：误报。

l:matcher.get_entry()、l:popup.close() 等是合法字典方法，l: 修饰字典变量。Vim 验证了方法定义和调用；不表示整个 Neovim 专用模块能在 Vim 执行。

验证：[local-dictionary-method.vim](oracle/cases/local-dictionary-method.vim)（[结果](oracle/local-dictionary-method.json)）。

<a id="fp05"></a>

### FP05：session 的 CTRL-V 转义被误切分（4 条）

编号：40、43、45、47。分类：误报。

生成的 listchars 命令使用字面 CTRL-V 引用字节及转义空格。保留原始字节逐条执行均成功；不能根据终端显示的替换字符认定源命令损坏。

验证：[session-listchars-40.vim](oracle/cases/session-listchars-40.vim)（[结果](oracle/session-listchars-40.json)）；[session-listchars-43.vim](oracle/cases/session-listchars-43.vim)（[结果](oracle/session-listchars-43.json)）；[session-listchars-45.vim](oracle/cases/session-listchars-45.vim)（[结果](oracle/session-listchars-45.json)）；[session-listchars-47.vim](oracle/cases/session-listchars-47.vim)（[结果](oracle/session-listchars-47.json)）。

<a id="fp06"></a>

### FP06：修饰符后的纯地址命令（1 条）

编号：41。分类：误报。

keepjumps :3 是合法跳转；最小案例准备三行文本并断言到达第三行，不缺少命令。

验证：[session-modifier-address.vim](oracle/cases/session-modifier-address.vim)（[结果](oracle/session-modifier-address.json)）。

<a id="fp07"></a>

### FP07：new +cmd 的嵌入命令被错误拆分（4 条）

编号：49、50、51、52。分类：误报。

new +setlocal\ previewwindow|… 的 +cmd 转义、命令分隔与文件名有各自上下文；后续选项和 [Document] 不应作为错误的 setlocal 选项。Vim 实际执行并验证了相关窗口／缓冲选项。

验证：[new-plus-command-payload.vim](oracle/cases/new-plus-command-payload.vim)（[结果](oracle/new-plus-command-payload.json)）。

<a id="fp08"></a>

### FP08：Vim9 autoload 调用被当成非法命令（2 条）

编号：58、84。分类：误报。

modula2#SetDialect(...) 是合法的 Vim9 隐式函数调用，不是命令修饰符。使用隔离 runtime 中的无副作用同名 autoload 函数验证两种调用形态。

验证：[vim9-autoload-call.vim](oracle/cases/vim9-autoload-call.vim)（[结果](oracle/vim9-autoload-call.json)）。

<a id="fp09"></a>

### FP09：逻辑表达式类型推导错误（3 条）

编号：59、70、86。分类：误报。

这些 &&／|| 表达式及 0、1 布尔上下文在 Vim 中合法，表达式结果可用于 bool 返回值／条件；不可扩大为任意 number 都能当 bool。

验证：[logical-expression-bool.vim](oracle/cases/logical-expression-bool.vim)（[结果](oracle/logical-expression-bool.json)）。

<a id="fp10"></a>

### FP10：map 的容器元素类型转换被拒绝（2 条）

编号：60、61。分类：误报。

官方 vimindent 中 map 把字典的 list&lt;string&gt; 值或复制列表的 list&lt;string&gt; 元素变成 string，实际 Vim 允许这些具体操作；分析器错误要求输出仍匹配输入元素类型。

验证：[map-changing-container-value-type.vim](oracle/cases/map-changing-container-value-type.vim)（[结果](oracle/map-changing-container-value-type.json)）。

<a id="fp11"></a>

### FP11：解构中的重复丢弃占位符（2 条）

编号：62、63。分类：误报。

const [_, matched_line, matched_col, _, _] 中的 _ 是丢弃占位符，不产生多个同名变量。

验证：[repeated-destructuring-ignore.vim](oracle/cases/repeated-destructuring-ignore.vim)（[结果](oracle/repeated-destructuring-ignore.json)）。

<a id="fp12"></a>

### FP12：带类型的 for 解构错误比较整行类型（2 条）

编号：64、78。分类：误报。

for [paths: list&lt;string&gt;, Action: func: any] 等应逐元素解构 list&lt;any&gt; 行；不能把整行 list&lt;any&gt; 与某一个绑定的 func／string 类型比较。

验证：[typed-loop-destructuring.vim](oracle/cases/typed-loop-destructuring.vim)（[结果](oracle/typed-loop-destructuring.json)）。

<a id="fp13"></a>

### FP13：未考虑 delfunction 后重新定义（1 条）

编号：65。分类：误报。

lua.vim 是 legacy 根文件：先定义传统 s:LuaFold 函数，再删除，再以 def 重建，Vim 接受。另存反例证明删除 Vim9 根脚本 def 会真正触发 E1084，不能无条件放宽重定义。

验证：[delete-then-redefine-function.vim](oracle/cases/delete-then-redefine-function.vim)（[结果](oracle/delete-then-redefine-function.json)）。

边界反例：[counterexample-vim9-delete.vim](oracle/cases/counterexample-vim9-delete.vim)（[预期 E1084](oracle/counterexample-vim9-delete.json)）。

<a id="fp14"></a>

### FP14：映射 RHS 的 &lt;Bar&gt; 和续行被当成选项（2 条）

编号：66、67。分类：误报。

man.vim 的 &lt;Bar&gt; 属于映射右侧，续行后仍是映射内容，不是 setlocal 的 Bar 选项。案例检查 maparg() 的实际结果。

验证：[mapping-bar-payload.vim](oracle/cases/mapping-bar-payload.vim)（[结果](oracle/mapping-bar-payload.json)）。

<a id="fp15"></a>

### FP15：map 回调函数类型被错误拒绝（5 条）

编号：68、79、80、81、85。分类：误报。

官方脚本的 number→string、dict→string、number→dict、string→number 回调及 str2nr 回调均可在 Vim9 编译函数内执行；分析器对 map 第二参数的 Funcref 类型判断过严。

验证：[map-funcref-arguments.vim](oracle/cases/map-funcref-arguments.vim)（[结果](oracle/map-funcref-arguments.json)）。

<a id="fp16"></a>

### FP16：用户命令的引用参数占位符未展开（2 条）

编号：72、73。分类：误报。

Cfilter／Lfilter 调用 Qf_filter(true/false, &lt;q-args&gt;, &lt;q-bang&gt;) 时会展开并引用参数；不是实参不足。案例定义并实际调用带 bang 的两个命令。

验证：[vim9-command-quoted-args.vim](oracle/cases/vim9-command-quoted-args.vim)（[结果](oracle/vim9-command-quoted-args.json)）。

<a id="fp17"></a>

### FP17：函数型选项被当作普通 string 赋值（1 条）

编号：74。分类：误报。

operatorfunc 虽以 string 形式存储，却允许 Funcref／lambda，Vim 会转换为函数名；不能直接以 string 与 func 不一致报错。

验证：[operatorfunc-lambda.vim](oracle/cases/operatorfunc-lambda.vim)（[结果](oracle/operatorfunc-lambda.json)）。

<a id="fp18"></a>

### FP18：多行参数末尾逗号误报空白（1 条）

编号：77。分类：误报。

函数实参最后的逗号后跟换行，再接右括号合法；换行不能被当作缺失空白。

验证：[vim9-trailing-comma.vim](oracle/cases/vim9-trailing-comma.vim)（[结果](oracle/vim9-trailing-comma.json)）。

<a id="dsl01"></a>

### DSL01：XPTemplate 的 Scala 模板正文（5 条）

编号：29、30、31、32、33。分类：模板上下文排除。

scala.xpt.vim 的 trait／val／override 是模板文本。正确 XPTemplate 加载器在 XPT 命令处收集模板并 finish，不把正文作为 Vim 命令执行。本次未找到本地加载器，因此不宣称直接 source 该文件一定成功；缺少加载器会有另一类未定义命令问题。

上游依据：[XPTemplate 命令定义](https://raw.githubusercontent.com/drmingdrmer/xptemplate/master/plugin/xptemplate.parser.vim)；[模板解析器](https://raw.githubusercontent.com/drmingdrmer/xptemplate/master/autoload/xpt/parser.vim)。本项依赖加载语义，不在本机 33 个 Vim 执行案例内。

<a id="real01"></a>

### REAL01：health 错误报告函数少传参数（5 条）

编号：53、54、55、56、57。分类：条件性真实错误。

s:report_error(report, advises) 需要两个参数，五处错误分支只传一个；实际进入调用时产生 E119。未调用完整 health 检查，不代表当前启动会触发。

验证：[required-health-argument.vim](oracle/cases/required-health-argument.vim)（[结果](oracle/required-health-argument.json)）。

<a id="real02"></a>

### REAL02：legacy cursor 调用缺少 call（1 条）

编号：71。分类：条件性真实错误。

indent/sml.vim 的传统函数内直接写 cursor(lnum,1)，调用到这一行时 Vim 报 E492。

验证：[legacy-call-without-call.vim](oracle/cases/legacy-call-without-call.vim)（[结果](oracle/legacy-call-without-call.json)）。

<a id="real03"></a>

### REAL03：legacy setbufvar 调用缺少 call（2 条）

编号：75、76。分类：条件性真实错误。

editorconfig 的传统函数内直接写 setbufvar(...)，相同形态实际执行会报 E492。第一处受 Windows／shell 分支限制，恢复分支又依赖此前状态，不能当成当前 macOS 启动故障。

验证：[legacy-setbufvar-without-call.vim](oracle/cases/legacy-setbufvar-without-call.vim)（[结果](oracle/legacy-setbufvar-without-call.json)）。

<a id="real04"></a>

### REAL04：修改只读函数参数（2 条）

编号：82、83。分类：条件性真实错误。

netrw 的目录切换失败恢复路径中 let a:newdir = ... 会触发 E46；正常切换不代表会进入这些分支。

验证：[legacy-readonly-argument.vim](oracle/cases/legacy-readonly-argument.vim)（[结果](oracle/legacy-readonly-argument.json)）。

<a id="env01"></a>

### ENV01：当前 Vim 不接受 fillchars 的 msgsep 项（4 条）

编号：39、42、44、46。分类：旧会话兼容错误。

四份 session 的 setlocal fillchars=msgsep:- 在当前 Vim 9.2.1015 实际产生 E474，不是误报；可能只在加载这些旧 session 时出现。

验证：[session-fillchars-msgsep.vim](oracle/cases/session-fillchars-msgsep.vim)（[结果](oracle/session-fillchars-msgsep.json)）。

<a id="env02"></a>

### ENV02：当前 Vim 不存在 macmeta 选项（1 条）

编号：48。分类：旧会话兼容错误。

旧 view 中 setlocal macmeta 在本机当前 Vim 实际产生 E518；不能仅凭 macOS 平台推断选项存在。

验证：[view-macmeta.vim](oracle/cases/view-macmeta.vim)（[结果](oracle/view-macmeta.json)）。

<a id="fixture01"></a>

### FIXTURE01：未注释的帮助文字（3 条）

编号：15、16、17。分类：测试夹具非法片段。

vim-matchup 的匹配测试输入包含 account.／parenthesis／backslashes 等帮助正文；作为 Vim 命令执行确实是 E492，但不属于正常插件入口。

验证：[fixture-help-text.vim](oracle/cases/fixture-help-text.vim)（[结果](oracle/fixture-help-text.json)）。

<a id="fixture02"></a>

### FIXTURE02：echom 后未闭合的字符串（1 条）

编号：18。分类：测试夹具非法片段。

echom "hello andy" " CURSOR 中第二个双引号不是这里的普通尾注释，Vim 实际产生 E114。

验证：[fixture-unclosed-echo-string.vim](oracle/cases/fixture-unclosed-echo-string.vim)（[结果](oracle/fixture-unclosed-echo-string.json)）。

<a id="fixture03"></a>

### FIXTURE03：函数外 return（8 条）

编号：19、20、21、22、23、25、26、27。分类：测试夹具非法片段。

forwhile／parts／tabs 是不完整匹配测试输入，没有包住这些 return 的有效函数声明；Vim 的函数外 return 确实触发 E133。

验证：[fixture-top-level-return.vim](oracle/cases/fixture-top-level-return.vim)（[结果](oracle/fixture-top-level-return.json)）。

<a id="fixture04"></a>

### FIXTURE04：无匹配 while 的 endwhile（1 条）

编号：24。分类：测试夹具非法片段。

parts.vim 的片段含无匹配循环开始的 endwhile，Vim 确实报 E588。

验证：[fixture-unmatched-endwhile.vim](oracle/cases/fixture-unmatched-endwhile.vim)（[结果](oracle/fixture-unmatched-endwhile.json)）。
