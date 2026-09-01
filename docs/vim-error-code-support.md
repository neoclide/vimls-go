# Vim error code support

This inventory covers the native `vim/E…` diagnostics referenced by production Go source under `internal/` and `cmd/`. It is pinned to the current repository and Vim 9.2.1015 evidence.

## Reading the status

- **Full**: the implemented source-level trigger has direct or representative local coverage, and the audit found no material static-analysis boundary.
- **Partial**: production code emits the diagnostic only for a statically provable subset, as a conservative warning, or without complete direct-test coverage. The exact boundary is recorded in the row.
- **Audit scope**: `Full` describes the implemented document-analysis rule. It does not claim parity with every Vim runtime path or patch level.
- **Unsupported**: the ledger explicitly excludes the diagnostic because its trigger depends on execution context or mutable Vim runtime state that document analysis cannot prove.

Inventory: **460 supported** (250 full and 210 partial) and **7 explicitly unsupported**. All codes are sorted numerically. The 388 entries previously marked `Implemented; audit pending` were reviewed in four numeric ranges against their emitters, local tests, and Vim 9.2.1015 source/help/test evidence. Every audited code has a real production emitter; E10 is the only supported row without a direct local error-code assertion and is therefore partial.

## Supported error codes

### E10: \ should be followed by /, ? or &

- **Completeness**: Partial
- **Overview**: Invalid :substitute previous-pattern form
- **Implementation and tests**: internal/syntax/substitute.go; no direct E10 assertion recorded
- **Static-analysis boundary**: Emitter exists, but audit found no direct diagnostic assertion.

### E15: Invalid expression

- **Completeness**: Partial
- **Overview**: Invalid expressions across parser/signatures/scopes
- **Implementation and tests**: internal/syntax/expression.go; internal/syntax/expression_test.go:637
- **Static-analysis boundary**: Generic code also covers semantic expression failures; not full runtime parity.

### E16: Invalid range

- **Completeness**: Full
- **Overview**: Malformed Ex range
- **Implementation and tests**: internal/syntax/scanner.go; official_compile_cases_e0000_test.go:125
- **Static-analysis boundary**: Direct parser rule and case coverage.

### E46: Cannot change read-only variable

- **Completeness**: Partial
- **Overview**: Assignment to read-only variable
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:7528
- **Static-analysis boundary**: Only statically known read-only bindings are knowable.

### E107: Missing parentheses

- **Completeness**: Full
- **Overview**: Missing call parentheses
- **Implementation and tests**: internal/syntax/expression.go; expression_test.go:1719
- **Static-analysis boundary**: Direct parser rule and case coverage.

### E109: Missing ':' after '?'

- **Completeness**: Full
- **Overview**: Ternary missing colon
- **Implementation and tests**: internal/syntax/expression.go; expression_test.go:293
- **Static-analysis boundary**: Direct parser rule and case coverage.

### E110: Missing ')'

- **Completeness**: Full
- **Overview**: Malformed function type arguments
- **Implementation and tests**: internal/syntax/type.go; expression_test.go:1393
- **Static-analysis boundary**: Direct parser rule and case coverage.

### E111: Missing ']'

- **Completeness**: Full
- **Overview**: Missing list index closing bracket
- **Implementation and tests**: internal/syntax/scanner.go; expression_test.go:1513
- **Static-analysis boundary**: Direct parser rule and case coverage.

### E113: Unknown option

- **Completeness**: Partial
- **Overview**: Unknown option
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:4854
- **Static-analysis boundary**: Runtime/plugin-defined options cannot be fully modeled.

### E114: Missing double quote

- **Completeness**: Full
- **Overview**: Unterminated double-quoted string
- **Implementation and tests**: internal/syntax/expression.go; expression_test.go:223
- **Static-analysis boundary**: Direct lexical rule and assertion.

### E115: Missing single quote

- **Completeness**: Full
- **Overview**: Unterminated single-quoted string
- **Implementation and tests**: internal/syntax/expression.go; expression_test.go:224
- **Static-analysis boundary**: Direct lexical rule and assertion.

### E116: Invalid arguments for function

- **Completeness**: Partial
- **Overview**: Invalid function arguments
- **Implementation and tests**: internal/syntax/scanner.go; expression_test.go:1567
- **Static-analysis boundary**: Static builtin/signature knowledge is incomplete for runtime-defined functions.

### E117: Unknown function

- **Completeness**: Partial
- **Overview**: Unknown function
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:3846
- **Static-analysis boundary**: Global functions can be defined/deleted dynamically and RTP is incomplete.

### E118: Too many arguments for function

- **Completeness**: Partial
- **Overview**: Too many function arguments
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:1837
- **Static-analysis boundary**: Only resolved/static function signatures are checked.

### E119: Not enough arguments for function

- **Completeness**: Partial
- **Overview**: Too few function arguments
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:2264
- **Static-analysis boundary**: Only resolved/static function signatures are checked.

### E121: Undefined variable

- **Completeness**: Partial
- **Overview**: Undefined variable
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:3846
- **Static-analysis boundary**: Dynamic scopes and runtime-created variables require conservative unknown.

### E122: Function already exists, add ! to replace it

- **Completeness**: Partial
- **Overview**: :function 无 ! 重定义
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:10607`; `internal/server/diagnostics_test.go:31`
- **Static-analysis boundary**: 函数是否已经存在取决于运行时函数表，因此不把风险伪装成确定错误。所有未带 `!` 且允许使用 `!` 的 Legacy `:function` 定义均报告 E122 warning，提示脚本再次 source 时可能冲突；查询、已有 `!`、Vim9 `def` 和不能合法添加 `!` 的 Vim9 本地函数不提示。

### E124: Missing '('

- **Completeness**: Full
- **Overview**: function 定义缺 (
- **Implementation and tests**: `internal/syntax/signature_test.go:32`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E125: Illegal argument

- **Completeness**: Full
- **Overview**: 形参名非法
- **Implementation and tests**: `internal/syntax/official_parser_cases_test.go:609`; `internal/syntax/signature_test.go:115`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E126: Missing :endfunction

- **Completeness**: Full
- **Overview**: function 块未闭合
- **Implementation and tests**: `internal/syntax/blocks_test.go:74`; `internal/syntax/official_parser_cases_test.go:603`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E128: Function name must start with a capital or "s:"

- **Completeness**: Full
- **Overview**: legacy 全局函数命名规则
- **Implementation and tests**: `internal/syntax/signature_test.go:368`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E129: Function name required

- **Completeness**: Full
- **Overview**: function 缺函数名
- **Implementation and tests**: `internal/syntax/official_parser_cases_test.go:195`; `internal/syntax/signature_test.go:561`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E133: return not inside a function

- **Completeness**: Full
- **Overview**: return 不在函数内
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:34`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E146: Regular expressions can't be delimited by letters

- **Completeness**: Full
- **Overview**: Alphabetic :substitute delimiter
- **Implementation and tests**: internal/syntax/substitute.go; substitute_test.go:105
- **Static-analysis boundary**: Direct command grammar rule and assertion.

### E170: Missing :endwhile

- **Completeness**: Full
- **Overview**: Missing :endfor
- **Implementation and tests**: internal/syntax/blocks.go; official_compile_cases_e0000_test.go:966
- **Static-analysis boundary**: Direct block-stack rule and case coverage.

### E171: Missing :endif

- **Completeness**: Full
- **Overview**: Missing :endif
- **Implementation and tests**: internal/syntax/blocks.go; official_compile_cases_e0000_test.go:1020
- **Static-analysis boundary**: Direct block-stack rule and case coverage.

### E172: Missing marker

- **Completeness**: Full
- **Overview**: Missing heredoc marker
- **Implementation and tests**: internal/syntax/scanner.go; heredoc_test.go:141
- **Static-analysis boundary**: Direct heredoc grammar rule and assertion.

### E174: Command already exists: add ! to replace it

- **Completeness**: Partial
- **Overview**: :command 重定义
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:10643`; `internal/server/diagnostics_test.go:34`
- **Static-analysis boundary**: 用户命令是否已经存在取决于会话级可变命令表，因此不把风险伪装成确定错误。所有 Legacy 和 Vim9 中未带 `!` 的 `:command` 定义均报告 E174 warning，提示脚本再次 source 时可能冲突；已有 `!` 以及列表、查询形式不提示。

### E176: Invalid number of arguments

- **Completeness**: Partial
- **Overview**: Invalid argument count
- **Implementation and tests**: internal/analysis/scopes.go; command_body_test.go:290
- **Static-analysis boundary**: Generic error spans syntax and semantic command/function forms.

### E179: Argument required for

- **Completeness**: Partial
- **Overview**: 命令缺必需参数
- **Implementation and tests**: `internal/syntax/command_body_test.go:248`; `internal/syntax/official_parser_cases_test.go:535`
- **Static-analysis boundary**: 覆盖 Vim9 空 `:unlet`、`:lockvar`、`:unlockvar`，以及 `:command` 的 `-addr`、`-complete`、`-completeopt` 缺值路径（含合法缩写和空 `-completeopt=`）。

### E182: Invalid command name

- **Completeness**: Full
- **Overview**: Invalid user-command name
- **Implementation and tests**: internal/syntax/scanner.go; official_parser_cases_test.go:1164
- **Static-analysis boundary**: Direct scanner rule and assertion.

### E183: User defined commands must start with an uppercase letter

- **Completeness**: Full
- **Overview**: 用户命令命名规则
- **Implementation and tests**: `internal/syntax/command_body_test.go:156`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E221: Marker cannot start with lower case letter

- **Completeness**: Full
- **Overview**: Lowercase heredoc marker
- **Implementation and tests**: internal/syntax/scanner.go; heredoc_test.go:140
- **Static-analysis boundary**: Direct heredoc grammar rule and assertion.

### E260: Missing name after ->

- **Completeness**: Full
- **Overview**: Missing name after method arrow
- **Implementation and tests**: internal/syntax/expression.go; expression_test.go:2042
- **Static-analysis boundary**: Direct expression grammar rule and case coverage.

### E274: No white space allowed before parenthesis

- **Completeness**: Full
- **Overview**: Whitespace before call parenthesis
- **Implementation and tests**: internal/syntax/expression.go; expression_test.go:1925
- **Static-analysis boundary**: Direct Vim9 grammar rule and assertion.

### E354: Invalid register name

- **Completeness**: Full
- **Overview**: Invalid register name
- **Implementation and tests**: internal/syntax/expression.go; declarations_test.go:99
- **Static-analysis boundary**: Direct lexical/parser rule and cases.

### E390: Illegal argument

- **Completeness**: Full
- **Overview**: Illegal :syntax argument
- **Implementation and tests**: internal/syntax/syntax_command.go; syntax_command_test.go:462
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E393: group[t]here not accepted here

- **Completeness**: Full
- **Overview**: Invalid :syntax group placement
- **Implementation and tests**: internal/syntax/syntax_command.go; syntax_command_test.go:287
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E395: Contains argument not accepted here

- **Completeness**: Full
- **Overview**: Invalid :syntax contains argument
- **Implementation and tests**: internal/syntax/syntax_command.go; syntax_command_test.go:283
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E397: Filename required

- **Completeness**: Full
- **Overview**: Missing :syntax filename
- **Implementation and tests**: internal/syntax/syntax_command.go; syntax_command_test.go:590
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E398: Missing '='

- **Completeness**: Full
- **Overview**: Missing type alias assignment
- **Implementation and tests**: internal/syntax/declarations.go; declarations_test.go:1651
- **Static-analysis boundary**: Direct declaration grammar rule and cases.

### E399: Not enough arguments: syntax region

- **Completeness**: Full
- **Overview**: Incomplete :syntax region
- **Implementation and tests**: internal/syntax/syntax_command.go; syntax_command_test.go:286
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E400: No cluster specified

- **Completeness**: Full
- **Overview**: Missing :syntax cluster
- **Implementation and tests**: internal/syntax/syntax_command.go; syntax_command_test.go:368
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E401: Pattern delimiter not found

- **Completeness**: Full
- **Overview**: Missing :syntax pattern delimiter
- **Implementation and tests**: internal/syntax/syntax_command.go; syntax_command_test.go:170
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E402: Garbage after pattern

- **Completeness**: Full
- **Overview**: Trailing :syntax pattern garbage
- **Implementation and tests**: internal/syntax/syntax_command.go; syntax_command_test.go:126
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E404: Illegal arguments

- **Completeness**: Full
- **Overview**: Illegal :syntax arguments
- **Implementation and tests**: internal/syntax/syntax_command.go; syntax_command_test.go:781
- **Static-analysis boundary**: Direct recognized-command grammar rule and cases.

### E405: Missing equal sign

- **Completeness**: Full
- **Overview**: Missing :syntax equal sign
- **Implementation and tests**: internal/syntax/syntax_command.go; syntax_command_test.go:289
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E406: Empty argument

- **Completeness**: Full
- **Overview**: Empty :syntax argument
- **Implementation and tests**: internal/syntax/syntax_command.go; syntax_command_test.go:288
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E407: not allowed here

- **Completeness**: Full
- **Overview**: Misplaced :syntax keyword
- **Implementation and tests**: internal/syntax/syntax_command.go; syntax_command_test.go:373
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E408: must be first in contains list

- **Completeness**: Full
- **Overview**: Invalid first :syntax contains item
- **Implementation and tests**: internal/syntax/syntax_command.go; syntax_command_test.go:374
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E412: Not enough arguments: ":highlight link "

- **Completeness**: Full
- **Overview**: Too few :highlight link arguments
- **Implementation and tests**: internal/syntax/highlight.go; highlight_test.go:91
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E413: Too many arguments: ":highlight link "

- **Completeness**: Full
- **Overview**: Too many :highlight link arguments
- **Implementation and tests**: internal/syntax/highlight.go; highlight_test.go:92
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E415: Unexpected equal sign

- **Completeness**: Full
- **Overview**: Unexpected :highlight equal sign
- **Implementation and tests**: internal/syntax/highlight.go; highlight_test.go:93
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E416: Missing equal sign

- **Completeness**: Full
- **Overview**: Missing :highlight equal sign
- **Implementation and tests**: internal/syntax/highlight.go; highlight_test.go:94
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E417: Missing argument

- **Completeness**: Full
- **Overview**: Missing :highlight value
- **Implementation and tests**: internal/syntax/highlight.go; highlight_test.go:95
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E461: Illegal variable name

- **Completeness**: Partial
- **Overview**: 给 v: 变量等非法目标赋值
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3152`; `internal/syntax/official_parser_cases_test.go:186`
- **Static-analysis boundary**: 覆盖 Vim9 非法声明名和 `for` 复合绑定、Legacy 作用域字典的静态非法索引、`extend()` 向作用域字典写入的静态非法字面量键，以及 `setbufvar()`、`settabvar()`、`settabwinvar()`、`setwinvar()` 的静态非法字面量名称。动态键、动态名称、普通 Dictionary 和合法选项名保持安静；依赖 `a:`/`v:` 成员是否已存在的运行时路径不猜测具体错误码。

### E464: Ambiguous use of user-defined command

- **Completeness**: Partial
- **Overview**: 用户命令前缀歧义
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:10684`; `internal/server/diagnostics_test.go:37`
- **Static-analysis boundary**: 服务器先解析完整的 workspace/runtimepath Vim 文件快照并建立用户命令全称索引；只有索引未被文件数、字节数或读取失败截断时，才对命令调用做区分大小写的前缀判断。调用名是已知全称的真前缀时报告 E464 warning，提示使用命令全称避免混淆；精确全名、内置命令和无法由显式 `:command` 定义确认的外部命令不提示。

### E474: Invalid argument

- **Completeness**: Partial
- **Overview**: Invalid :set argument
- **Implementation and tests**: internal/syntax/set_command.go; set_command_test.go:199
- **Static-analysis boundary**: Option value semantics depend on Vim/runtime option definitions.

### E475: Invalid argument

- **Completeness**: Partial
- **Overview**: Generic invalid argument
- **Implementation and tests**: internal/syntax/syntax_command.go; official_compile_cases_e0000_test.go:1251
- **Static-analysis boundary**: One code covers many commands; only implemented grammars are checked.

### E476: Invalid command

- **Completeness**: Partial
- **Overview**: Invalid command form
- **Implementation and tests**: internal/syntax/blocks.go; official_compile_cases_e0000_test.go:1269
- **Static-analysis boundary**: Only recognized block/command forms are diagnosed.

### E477: No ! allowed

- **Completeness**: Partial
- **Overview**: 命令不支持 ! 却带了 !
- **Implementation and tests**: `internal/syntax/blocks_test.go:261`; `internal/syntax/official_parser_cases_test.go:696`; `internal/syntax/scanner_test.go:60`
- **Static-analysis boundary**: 已按固定的 Vim 9.2.1015 内置命令表覆盖不接受 bang 的命令，并覆盖 Vim9 本地 `:function!` 特例；未知和用户定义命令不猜测其 `-bang` 属性。

### E481: No range allowed

- **Completeness**: Full
- **Overview**: Range forbidden for command
- **Implementation and tests**: internal/syntax/scanner.go; official_compile_cases_e0000_test.go:1331
- **Static-analysis boundary**: Direct command metadata rule and case coverage.

### E488: Trailing characters

- **Completeness**: Partial
- **Overview**: Trailing characters
- **Implementation and tests**: internal/syntax/scanner.go; official_compile_cases_e0000_test.go:1435
- **Static-analysis boundary**: Generic trailing-input code; opaque/unknown commands intentionally recover.

### E492: Not an editor command

- **Completeness**: Partial
- **Overview**: Not an editor command in recognized Vim9-invalid forms
- **Implementation and tests**: internal/syntax/scanner.go; scanner_test.go:435
- **Static-analysis boundary**: Unknown/user-defined commands remain opaque by invariant; only unambiguous malformed forms are reported.

### E518: Unknown option

- **Completeness**: Partial
- **Overview**: Unknown option
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:4854
- **Static-analysis boundary**: Runtime/plugin-defined options cannot be fully modeled.

### E580: endif without :if

- **Completeness**: Full
- **Overview**: :endif without :if
- **Implementation and tests**: internal/syntax/blocks.go; blocks_test.go:756
- **Static-analysis boundary**: Direct block-stack rule and case coverage.

### E581: else without :if

- **Completeness**: Full
- **Overview**: :else without :if
- **Implementation and tests**: internal/syntax/blocks.go; blocks_test.go:756
- **Static-analysis boundary**: Direct block-stack rule and case coverage.

### E582: elseif without :if

- **Completeness**: Full
- **Overview**: :elseif without :if
- **Implementation and tests**: internal/syntax/blocks.go; blocks_test.go:756
- **Static-analysis boundary**: Direct block-stack rule and case coverage.

### E583: Multiple :else

- **Completeness**: Full
- **Overview**: Duplicate :else
- **Implementation and tests**: internal/syntax/blocks.go; official_compile_cases_e0000_test.go:1622
- **Static-analysis boundary**: Direct block-stack rule and case coverage.

### E584: elseif after :else

- **Completeness**: Full
- **Overview**: :elseif after :else
- **Implementation and tests**: internal/syntax/blocks.go; official_compile_cases_e0000_test.go:1639
- **Static-analysis boundary**: Direct block-stack rule and case coverage.

### E586: continue without :while or :for

- **Completeness**: Full
- **Overview**: :continue outside loop
- **Implementation and tests**: internal/syntax/blocks.go; blocks_test.go:756
- **Static-analysis boundary**: Direct block-stack rule and case coverage.

### E587: break without :while or :for

- **Completeness**: Full
- **Overview**: :break outside loop
- **Implementation and tests**: internal/syntax/blocks.go; blocks_test.go:756
- **Static-analysis boundary**: Direct block-stack rule and case coverage.

### E588: endwhile without :while

- **Completeness**: Full
- **Overview**: :endwhile without :while
- **Implementation and tests**: internal/syntax/blocks.go; blocks_test.go:440
- **Static-analysis boundary**: Direct block-stack rule and case coverage.

### E600: Missing :endtry

- **Completeness**: Full
- **Overview**: Missing :endtry
- **Implementation and tests**: internal/syntax/blocks.go; official_compile_cases_e0000_test.go:1738
- **Static-analysis boundary**: Direct block-stack rule and case coverage.

### E602: endtry without :try

- **Completeness**: Full
- **Overview**: :endtry without :try
- **Implementation and tests**: internal/syntax/blocks.go; blocks_test.go:756
- **Static-analysis boundary**: Direct block-stack rule and case coverage.

### E603: catch without :try

- **Completeness**: Full
- **Overview**: :catch without :try
- **Implementation and tests**: internal/syntax/blocks.go; blocks_test.go:756
- **Static-analysis boundary**: Direct block-stack rule and case coverage.

### E606: finally without :try

- **Completeness**: Full
- **Overview**: :finally without :try
- **Implementation and tests**: internal/syntax/blocks.go; blocks_test.go:756
- **Static-analysis boundary**: Direct block-stack rule and case coverage.

### E607: Multiple :finally

- **Completeness**: Full
- **Overview**: Duplicate :finally
- **Implementation and tests**: internal/syntax/blocks.go; official_compile_cases_e0000_test.go:1951
- **Static-analysis boundary**: Direct block-stack rule and case coverage.

### E611: Using a Special as a Number

- **Completeness**: Partial
- **Overview**: Special value used as Number
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:1189
- **Static-analysis boundary**: Only statically inferred values/types are checked.

### E684: List index out of range

- **Completeness**: Partial
- **Overview**: 常量索引越界时静态可报
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:7493`
- **Static-analysis boundary**: 不能完整静态判定。是否越界取决于求值后的列表长度和索引，而列表可由参数、函数返回值、循环、`add()`/`remove()` 或外部状态产生。只有长度和索引均为已证明常量，或长度区间足以证明必然越界时才能报告。

### E687: Less targets than List items

- **Completeness**: Partial
- **Overview**: 解构目标少（legacy 路径）
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:11173`
- **Static-analysis boundary**: 不能只靠语法分析判定。Legacy 解构会先求出右侧 List，再将实际长度与目标数量比较；右侧长度可能由运行时数据决定。只有右侧是固定长度字面量、Tuple，或长度分析能证明项目必然过多时才能报告。

### E688: More targets than List items

- **Completeness**: Partial
- **Overview**: 解构目标多（legacy 路径）
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:11135`
- **Static-analysis boundary**: 不能只靠语法分析判定。Legacy 解构会先求出右侧 List，再将实际长度与目标数量比较；右侧长度可能由运行时数据决定。只有右侧是固定长度字面量、Tuple，或长度分析能证明项目必然不足时才能报告。

### E689: Index not allowed after a

- **Completeness**: Partial
- **Overview**: String indexed as List
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:8035
- **Static-analysis boundary**: Only statically inferred receiver types are checked.

### E690: Missing "in" after :for

- **Completeness**: Full
- **Overview**: Missing in in :for
- **Implementation and tests**: internal/syntax/scanner.go; blocks_test.go:457
- **Static-analysis boundary**: Direct parser rule and case coverage.

### E695: Cannot index a Funcref

- **Completeness**: Partial
- **Overview**: 对 Funcref 索引
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:7456`
- **Static-analysis boundary**: 不能仅靠全量符号索引完整判定。被索引表达式的实际类型可能来自 Legacy 动态值、Vim9 `any`、容器成员或函数返回值。只有类型传播能证明目标必然为 Funcref 时才能报告；类型为 `any` 或 `unknown` 时必须保持安静。

### E696: Missing comma in List

- **Completeness**: Full
- **Overview**: Missing List comma
- **Implementation and tests**: internal/syntax/expression.go; expression_test.go:1188
- **Static-analysis boundary**: Direct expression grammar rule and case coverage.

### E697: Missing end of List ']'

- **Completeness**: Full
- **Overview**: Missing List closing bracket
- **Implementation and tests**: internal/syntax/scanner.go; official_compile_cases_e0000_test.go:2177
- **Static-analysis boundary**: Direct expression grammar rule and case coverage.

### E701: Invalid type for len()

- **Completeness**: Partial
- **Overview**: Invalid len() argument type
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:4611
- **Static-analysis boundary**: Only statically inferred argument types are checked.

### E703: Using a Funcref as a Number

- **Completeness**: Partial
- **Overview**: Funcref used as Number
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:1253
- **Static-analysis boundary**: Only statically inferred values/types are checked.

### E704: Funcref variable name must start with a capital

- **Completeness**: Partial
- **Overview**: Invalid Funcref variable capitalization
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:4453
- **Static-analysis boundary**: Dynamic funcref construction/resolution remains conservative.

### E705: Variable name conflicts with existing function

- **Completeness**: Partial
- **Overview**: 变量名与函数冲突
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3184`; `internal/server/diagnostics_test.go:40`; `internal/workspace/index_test.go:121`
- **Static-analysis boundary**: 覆盖静态命名的 `s:` 和全局函数/变量声明。单文件按源码顺序维护动态声明表并处理直接 `:delfunction`、`:unlet`；workspace/runtimepath 初始索引记录仍活动的全局声明及文件、类型和字节位置。变量声明遇到同作用域函数时报告 E705 warning，提示重命名以避免运行时冲突。动态名称、`:execute`、延迟命令体以及无法证明的运行时函数表变化保持安静。

### E707: Function name conflicts with variable

- **Completeness**: Partial
- **Overview**: 函数名与变量冲突
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3185`; `internal/server/diagnostics_test.go:40`; `internal/workspace/index_test.go:129`
- **Static-analysis boundary**: 与 E705 共用同一动态声明表和全局索引。函数声明遇到同作用域变量时报告 E707 warning，提示重命名；直接删除会更新单文件活动状态。另支持 Vim9 autoload 脚本中特有的 `var Name` 后接 `export def Name()`：当文件路径确定属于 workspace/runtimepath 的 `autoload/*.vim` 时，将普通脚本中的 E1041 重分类为 E707；非 autoload 文件、反向声明顺序和非导出 `def` 仍保留 E1041。跨文件只使用索引中静态确定且仍活动的全局声明，动态赋值、`FuncUndefined` 副作用、`:execute` 和未知执行顺序不推断为确定错误。

### E710: List value has more items than targets

- **Completeness**: Partial
- **Overview**: 解构赋值目标少于列表项（列表为字面量时静态可报）
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3769`
- **Static-analysis boundary**: 当前仅覆盖没有 rest 目标的 Vim9 `for` 解构直接 List 字面量中，元素也是直接 List 字面量且必然多于目标数的情况；变量、未知长度、带 rest 目标和 Legacy 范围赋值不报告。

### E711: List value does not have enough items

- **Completeness**: Partial
- **Overview**: 解构赋值目标多于列表项
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3736`
- **Static-analysis boundary**: 当前仅覆盖 Vim9 `for` 解构直接 List 字面量中，元素也是直接 List 字面量且必然少于固定目标数的情况；变量、未知长度和 Legacy 范围赋值不报告。

### E716: Key not present in Dictionary

- **Completeness**: Partial
- **Overview**: Missing Dictionary key
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:4533
- **Static-analysis boundary**: Only statically known dictionary keys are checked.

### E719: Cannot slice a Dictionary

- **Completeness**: Partial
- **Overview**: 对 dict 切片
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3703`
- **Static-analysis boundary**: Legacy 当前仅覆盖直接 Dictionary 字面量和内置 `v:` Dictionary；Vim9 覆盖静态类型明确为 Dictionary 的读取切片。赋值目标、未知值和 `def` 中由 E1166 负责的 `:unlet` 切片不报告 E719。

### E720: Missing colon in Dictionary

- **Completeness**: Full
- **Overview**: Dictionary missing colon
- **Implementation and tests**: internal/syntax/expression.go; official_compile_cases_e0000_test.go:2277
- **Static-analysis boundary**: Direct expression grammar rule and case coverage.

### E721: Duplicate key in Dictionary

- **Completeness**: Full
- **Overview**: Duplicate Dictionary key
- **Implementation and tests**: internal/syntax/expression.go; expression_test.go:1256
- **Static-analysis boundary**: Literal-key parser rule; computed/equivalent runtime keys are outside scope.

### E722: Missing comma in Dictionary

- **Completeness**: Full
- **Overview**: Dictionary missing comma
- **Implementation and tests**: internal/syntax/expression.go; official_compile_cases_e0000_test.go:2385
- **Static-analysis boundary**: Direct expression grammar rule and case coverage.

### E723: Missing end of Dictionary '}'

- **Completeness**: Full
- **Overview**: Dictionary missing closing brace
- **Implementation and tests**: internal/syntax/expression.go; expression_test.go:1120
- **Static-analysis boundary**: Direct expression grammar rule and case coverage.

### E728: Using a Dictionary as a Number

- **Completeness**: Partial
- **Overview**: Dictionary used as Number
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:1305
- **Static-analysis boundary**: Only statically inferred values/types are checked.

### E729: Using a Funcref as a String

- **Completeness**: Partial
- **Overview**: Funcref used as String
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:1357
- **Static-analysis boundary**: Only statically inferred values/types are checked.

### E730: Using a List as a String

- **Completeness**: Partial
- **Overview**: List used as String
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:1417
- **Static-analysis boundary**: Only statically inferred values/types are checked.

### E731: Using a Dictionary as a String

- **Completeness**: Partial
- **Overview**: Dictionary used as String
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:1473
- **Static-analysis boundary**: Only statically inferred values/types are checked.

### E734: Wrong variable type for =

- **Completeness**: Partial
- **Overview**: Wrong variable type
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:1533
- **Static-analysis boundary**: Only statically inferred types are checked.

### E740: Too many arguments for function

- **Completeness**: Partial
- **Overview**: 直接命名函数调用包含超过 20 个实参
- **Implementation and tests**: `internal/syntax/expression_test.go:782`
- **Static-analysis boundary**: 当前仅在调用目标是静态可见的标识符且参数列表没有其他语法诊断时报告；动态 Funcref、partial 和方法调用的有效参数上限可能受已绑定参数影响，不在此规则中推断。

### E741: Value is locked

- **Completeness**: Partial
- **Overview**: 对 :lockvar 锁定的变量赋值
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3669`
- **Static-analysis boundary**: 当前仅覆盖 Legacy 中紧邻 `:lockvar` 的同名变量重赋值，以及默认深度锁定后紧邻的一层成员修改；显式深度 0 的内容修改、别名、嵌套成员、条件流和跨命令状态不报告。

### E742: Cannot change value

- **Completeness**: Partial
- **Overview**: 对固定（fixed）变量赋值
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3636`
- **Static-analysis boundary**: 当前仅覆盖 Legacy 直接修改固定 `v:event` 字典成员，以及直接替换固定参数列表 `a:000` 的元素/切片；普通容器、别名、嵌套元素内部修改和运行时回调值不报告。

### E745: Using a List as a Number

- **Completeness**: Partial
- **Overview**: List used as Number
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:1588
- **Static-analysis boundary**: Only statically inferred values/types are checked.

### E789: Missing ']'

- **Completeness**: Full
- **Overview**: Missing :syntax bracket
- **Implementation and tests**: internal/syntax/syntax_command.go; syntax_command_test.go:206
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E791: Empty keymap entry

- **Completeness**: Full
- **Overview**: Empty keymap entry
- **Implementation and tests**: internal/syntax/scanner.go; loadkeymap_test.go:59
- **Static-analysis boundary**: Direct keymap parser rule and assertion.

### E804: Cannot use '%' with Float

- **Completeness**: Partial
- **Overview**: Float modulo
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:3945
- **Static-analysis boundary**: Only statically inferred operand types are checked.

### E805: Using a Float as a Number

- **Completeness**: Partial
- **Overview**: Float used as Number
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:4042
- **Static-analysis boundary**: Only statically inferred operand types are checked.

### E806: Using a Float as a String

- **Completeness**: Partial
- **Overview**: Float used as String
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:4123
- **Static-analysis boundary**: Only statically inferred operand types are checked.

### E853: Duplicate argument name

- **Completeness**: Full
- **Overview**: 形参重名
- **Implementation and tests**: `internal/syntax/expression_test.go:697`; `internal/syntax/signature_test.go:240`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E884: Function name cannot contain a colon

- **Completeness**: Partial
- **Overview**: 函数名含冒号
- **Implementation and tests**: `internal/syntax/official_parser_cases_test.go:648`; `internal/syntax/signature_test.go:409`
- **Static-analysis boundary**: 覆盖 `<SID>:` 后多余冒号，以及去除合法 `s:`/`g:` 前缀后仍含冒号的函数名；Vim9 嵌套命名空间和脚本级 `s:` 保留 E1075/E1268 优先级。

### E890: Trailing char after ']': ]

- **Completeness**: Full
- **Overview**: Trailing :syntax bracket characters
- **Implementation and tests**: internal/syntax/syntax_command.go; syntax_command_test.go:207
- **Static-analysis boundary**: Direct recognized-command grammar rule and assertion.

### E891: Using a Funcref as a Float

- **Completeness**: Partial
- **Overview**: Funcref 被用于需要 Float 的转换或运算
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3602`
- **Static-analysis boundary**: 当前仅覆盖 `sort()` 的直接 List 字面量在字面量 `"f"` 模式下包含明确 Funcref/Partial 的情况；可变列表、模式变量、`any` 和未知返回值不报告。Vim 9.2.1015 的 E891 专指 Funcref，字符串转 Float 使用其他错误定义。

### E896: Argument of must be a List, Dictionary or Blob

- **Completeness**: Partial
- **Overview**: Invalid argument type
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:4183
- **Static-analysis boundary**: Only statically inferred argument types are checked.

### E908: Using an invalid value as a String

- **Completeness**: Partial
- **Overview**: Invalid value used as String
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:4265
- **Static-analysis boundary**: Only statically inferred values/types are checked.

### E909: Cannot index a special variable

- **Completeness**: Partial
- **Overview**: 对特殊变量索引
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3570`
- **Static-analysis boundary**: 当前仅覆盖不可变内置值 `v:true`、`v:false`、`v:null`、`v:none` 的直接索引或切片；普通变量、`any`、参数和未知返回值不报告。

### E932: Closure function should not be at top level

- **Completeness**: Full
- **Overview**: 闭包函数定义在顶层
- **Implementation and tests**: `internal/syntax/signature_test.go:322`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E963: Setting v: to value with wrong type

- **Completeness**: Partial
- **Overview**: 给 v: 变量赋错误类型（变体）
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3534`
- **Static-analysis boundary**: 当前仅覆盖可写容器型 `v:` 变量的直接 `=` 赋值，且右值顶层类型必须明确不兼容；String/Number 转换、只读变量、成员赋值和未知返回值不报告。

### E973: Blob literal should have an even number of hex characters

- **Completeness**: Full
- **Overview**: Odd-length Blob literal
- **Implementation and tests**: internal/syntax/expression.go; official_compile_cases_e0000_test.go:2917
- **Static-analysis boundary**: Direct literal grammar rule and case coverage.

### E974: Using a Blob as a Number

- **Completeness**: Partial
- **Overview**: Blob used as Number
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:4331
- **Static-analysis boundary**: Only statically inferred values/types are checked.

### E976: Using a Blob as a String

- **Completeness**: Partial
- **Overview**: Blob used as String
- **Implementation and tests**: internal/analysis/scopes.go; diagnostics_test.go:4383
- **Static-analysis boundary**: Only statically inferred values/types are checked.

### E983: Duplicate argument

- **Completeness**: Partial
- **Overview**: 形参重名（脚本路径）
- **Implementation and tests**: `internal/syntax/autocmd_embedded_test.go:311`; `internal/syntax/official_parser_cases_test.go:599`; `internal/syntax/scanner_test.go:130`
- **Static-analysis boundary**: 当前覆盖重复 `vim9script noclear` 参数，以及 `:autocmd` 中重复的 `++once`、`++nested` 和 `nested` 修饰符。

### E985: = is not supported with script version >= 2

- **Completeness**: Full
- **Overview**: Vim9 中用 .=，应用 ..=
- **Implementation and tests**: `internal/syntax/scanner_test.go:977`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E989: Non-default argument follows default argument

- **Completeness**: Full
- **Overview**: legacy 变体：必选参数排在默认参数后
- **Implementation and tests**: `internal/syntax/signature_test.go:179`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E990: Missing end marker

- **Completeness**: Full
- **Overview**: Missing heredoc end marker
- **Implementation and tests**: internal/syntax/blocks.go; heredoc_test.go:160
- **Static-analysis boundary**: Direct heredoc/block rule and assertion.

### E995: Cannot modify existing variable

- **Completeness**: Partial
- **Overview**: Legacy `:const` 尝试修改已存在变量
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3500`
- **Static-analysis boundary**: 当前仅覆盖 Legacy `function` 内连续 `let`/`const` 声明序列中已存在的静态名称；任何其他命令、作用域变化或动态 `{name}` 都会丢弃事实，脚本级、条件声明和重复 source 不报告。

### E996: Cannot lock a range

- **Completeness**: Partial
- **Overview**: Locking unsupported target
- **Implementation and tests**: internal/syntax/scanner.go; official_compile_cases_e0000_test.go:3047
- **Static-analysis boundary**: Vim has multiple E996 runtime target forms; only parsed forms covered.

### E1001: Variable not found

- **Completeness**: Partial
- **Overview**: Unknown Vim9 variable/reference
- **Implementation and tests**: internal/analysis/scopes.go:9000; internal/analysis/official_compile_cases_e1000_test.go:20; Vim runtime/doc/vim9.txt:1570
- **Static-analysis boundary**: Only statically unknown bindings; runtime/global/autoload mutations are intentionally not modeled.

### E1002: Syntax error at

- **Completeness**: Full
- **Overview**: Invalid expression token
- **Implementation and tests**: internal/syntax/scanner.go:3631; internal/syntax/expression_test.go:1646; internal/analysis/official_compile_cases_e1000_test.go:142
- **Static-analysis boundary**: No material static limitation found.

### E1003: Missing return value

- **Completeness**: Partial
- **Overview**: Missing value for typed return
- **Implementation and tests**: internal/analysis/scopes.go:3530; internal/analysis/official_compile_cases_e1000_test.go:200; internal/analysis/diagnostics_test.go:11263
- **Static-analysis boundary**: Only parsed def/function control paths and known return annotations.

### E1004: White space required before and after at

- **Completeness**: Full
- **Overview**: Required Vim9 whitespace around assignment/slice
- **Implementation and tests**: internal/syntax/scanner.go:5041; internal/syntax/expression_test.go:318; internal/analysis/official_compile_cases_e1000_test.go:213
- **Static-analysis boundary**: No material static limitation found.

### E1005: Too many argument types

- **Completeness**: Full
- **Overview**: Too many function type arguments
- **Implementation and tests**: internal/syntax/type.go:270; internal/syntax/type_test.go:217; internal/analysis/official_compile_cases_e1000_test.go:321
- **Static-analysis boundary**: No material static limitation found.

### E1006: is used as an argument

- **Completeness**: Partial
- **Overview**: Variable used as a function argument
- **Implementation and tests**: internal/analysis/scopes.go:8111; internal/analysis/official_compile_cases_e1000_test.go:335; internal/analysis/diagnostics_test.go:4910
- **Static-analysis boundary**: Depends on locally resolved declarations; dynamic references are unknown.

### E1007: Mandatory argument after optional argument

- **Completeness**: Full
- **Overview**: Required parameter after optional parameter
- **Implementation and tests**: internal/syntax/type.go:285; internal/syntax/type_test.go:192; internal/analysis/official_compile_cases_e1000_test.go:351
- **Static-analysis boundary**: No material static limitation found.

### E1008: Missing type after

- **Completeness**: Full
- **Overview**: Missing generic type argument
- **Implementation and tests**: internal/syntax/signature.go:503; internal/syntax/signature_test.go:865; internal/analysis/official_compile_cases_e1000_test.go:365
- **Static-analysis boundary**: No material static limitation found.

### E1009: Missing > after type

- **Completeness**: Full
- **Overview**: Unclosed generic type list
- **Implementation and tests**: internal/syntax/type.go:222; internal/syntax/type_test.go:54; internal/analysis/official_compile_cases_e1000_test.go:427
- **Static-analysis boundary**: No material static limitation found.

### E1010: Type not recognized

- **Completeness**: Full
- **Overview**: Unknown type spelling
- **Implementation and tests**: internal/syntax/type.go:79; internal/syntax/type_test.go:38; internal/analysis/official_compile_cases_e1000_test.go:509
- **Static-analysis boundary**: No material static limitation found.

### E1011: Name too long

- **Completeness**: Full
- **Overview**: Overlong Vim9 identifier
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/official_compile_cases_e1000_test.go:615; internal/analysis/diagnostics_test.go:5070
- **Static-analysis boundary**: Direct lexical length check; no known limitation.

### E1012: Type mismatch; expected but got

- **Completeness**: Partial
- **Overview**: Static type mismatch
- **Implementation and tests**: internal/analysis/scopes.go:5493; internal/analysis/official_compile_cases_e1000_test.go:629; Vim runtime/doc/vim9.txt:348
- **Static-analysis boundary**: Limited type lattice and inference; values crossing dynamic calls remain unknown.

### E1013: Argument : type mismatch, expected but got

- **Completeness**: Partial
- **Overview**: Static function argument type mismatch
- **Implementation and tests**: internal/analysis/scopes.go:9861; internal/analysis/official_compile_cases_e1000_test.go:735; Vim runtime/doc/vim9.txt:2433
- **Static-analysis boundary**: Checks known signatures/types only; dynamic functions and incomplete inference are omitted.

### E1015: Name expected

- **Completeness**: Full
- **Overview**: Name missing in expression
- **Implementation and tests**: internal/syntax/scanner.go:3616; internal/syntax/expression_test.go:625; internal/syntax/official_parser_cases_test.go:383
- **Static-analysis boundary**: No material static limitation found.

### E1016: Cannot declare a variable

- **Completeness**: Full
- **Overview**: Illegal environment/scoped declaration
- **Implementation and tests**: internal/syntax/scanner.go:5391; internal/syntax/declarations_test.go:2104; internal/analysis/official_compile_cases_e1000_test.go:844
- **Static-analysis boundary**: No material static limitation found.

### E1017: Variable already declared

- **Completeness**: Partial
- **Overview**: Duplicate variable declaration
- **Implementation and tests**: internal/analysis/scopes.go:4885; internal/analysis/official_compile_cases_e1000_test.go:962; internal/analysis/diagnostics_test.go:7340
- **Static-analysis boundary**: Only declarations represented in the parsed scope graph; sourced/dynamic declarations are unknown.

### E1018: Cannot assign to a constant

- **Completeness**: Partial
- **Overview**: Assignment to const
- **Implementation and tests**: internal/analysis/scopes.go:7356; internal/analysis/official_compile_cases_e1000_test.go:1026; internal/analysis/diagnostics_test.go:7536
- **Static-analysis boundary**: Only resolved local/static bindings; dynamic targets are conservative.

### E1019: Can only concatenate to string

- **Completeness**: Partial
- **Overview**: Non-string concatenation target
- **Implementation and tests**: internal/analysis/scopes.go:6317; internal/analysis/official_compile_cases_e1000_test.go:1056; internal/analysis/diagnostics_test.go:437
- **Static-analysis boundary**: Requires a known target type; any/dynamic values are not diagnosed.

### E1020: Cannot use an operator on a new variable

- **Completeness**: Full
- **Overview**: Compound operator on new variable
- **Implementation and tests**: internal/syntax/scanner.go:3288; internal/syntax/declarations_test.go:2328; internal/analysis/official_compile_cases_e1000_test.go:1083
- **Static-analysis boundary**: No material static limitation found.

### E1021: Const requires a value

- **Completeness**: Full
- **Overview**: const without value
- **Implementation and tests**: internal/syntax/scanner.go:4805; internal/syntax/official_parser_cases_test.go:390; internal/analysis/official_compile_cases_e1000_test.go:1097
- **Static-analysis boundary**: No material static limitation found.

### E1022: Type or initialization required

- **Completeness**: Full
- **Overview**: Declaration missing type and initializer
- **Implementation and tests**: internal/syntax/scanner.go:4819; internal/syntax/official_parser_cases_test.go:398; internal/analysis/official_compile_cases_e1000_test.go:1111
- **Static-analysis boundary**: No material static limitation found.

### E1023: Using a Number as a Bool

- **Completeness**: Partial
- **Overview**: Number used as Bool
- **Implementation and tests**: internal/analysis/scopes.go:5249; internal/analysis/official_compile_cases_e1000_test.go:1138; internal/analysis/diagnostics_test.go:1707
- **Static-analysis boundary**: Only statically known numeric expressions.

### E1024: Using a Number as a String

- **Completeness**: Partial
- **Overview**: Number used as String
- **Implementation and tests**: internal/analysis/scopes.go:9834; internal/analysis/official_compile_cases_e1000_test.go:1196; internal/analysis/diagnostics_test.go:3896
- **Static-analysis boundary**: Only statically known numeric expressions/arguments.

### E1025: Using } outside of a block scope

- **Completeness**: Full
- **Overview**: Closing brace without open block
- **Implementation and tests**: internal/syntax/blocks_test.go:709; internal/syntax/official_parser_cases_test.go:1161; internal/analysis/official_compile_cases_e1000_test.go:1214
- **Static-analysis boundary**: No material static limitation found.

### E1026: Missing }

- **Completeness**: Full
- **Overview**: Missing closing brace
- **Implementation and tests**: internal/syntax/scanner.go:704; internal/syntax/command_body_test.go:411; internal/analysis/official_compile_cases_e1000_test.go:1228
- **Static-analysis boundary**: No material static limitation found.

### E1027: Missing return statement

- **Completeness**: Partial
- **Overview**: Missing return statement
- **Implementation and tests**: internal/analysis/scopes.go:3488; internal/analysis/official_compile_cases_e1000_test.go:1243; Vim runtime/doc/vim9.txt:1402
- **Static-analysis boundary**: Control-flow analysis is syntactic/conservative, not complete for dynamic commands/calls.

### E1029: Expected but got

- **Completeness**: Partial
- **Overview**: 严格模式类型不符（编译/执行均可报）
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3463`
- **Static-analysis boundary**: 当前覆盖 `def` 中连续简单 `g:` 字面量赋值后，`:unlet` 对 List 使用非 Number 索引，以及 Dictionary 使用明确不可作为 key 的 Blob/List/Dict；遇到其他命令即丢弃该运行时类型事实，脚本级、`any` 和未知值不报告。

### E1030: Using a String as a Number

- **Completeness**: Partial
- **Overview**: String used as Number
- **Implementation and tests**: internal/analysis/scopes.go:5185; internal/analysis/official_compile_cases_e1000_test.go:1281; internal/analysis/diagnostics_test.go:678
- **Static-analysis boundary**: Only statically known string expressions.

### E1031: Cannot use void value

- **Completeness**: Partial
- **Overview**: Void value consumed
- **Implementation and tests**: internal/analysis/scopes.go:5996; internal/analysis/official_compile_cases_e1000_test.go:1373; internal/analysis/diagnostics_test.go:474
- **Static-analysis boundary**: Only known void-producing calls/callbacks.

### E1032: Missing :catch or :finally

- **Completeness**: Full
- **Overview**: try without catch/finally
- **Implementation and tests**: internal/syntax/official_parser_cases_test.go:720; internal/analysis/official_compile_cases_e1000_test.go:1412
- **Static-analysis boundary**: No material static limitation found.

### E1033: Catch unreachable after catch-all

- **Completeness**: Full
- **Overview**: Catch after catch-all
- **Implementation and tests**: internal/syntax/blocks_test.go:501; internal/analysis/official_compile_cases_e1000_test.go:1428
- **Static-analysis boundary**: No material static limitation found.

### E1034: Cannot use reserved name

- **Completeness**: Full
- **Overview**: Reserved declaration name
- **Implementation and tests**: internal/syntax/scanner.go:5346; internal/syntax/declarations_test.go:12; internal/analysis/official_compile_cases_e1000_test.go:1445
- **Static-analysis boundary**: No material static limitation found.

### E1035: equires number arguments

- **Completeness**: Partial
- **Overview**: printf percent conversion requires numbers
- **Implementation and tests**: internal/analysis/scopes.go:5578; internal/analysis/official_compile_cases_e1000_test.go:1507; internal/analysis/diagnostics_test.go:587
- **Static-analysis boundary**: Only literal/known format and argument types are checked.

### E1036: requires number or float arguments

- **Completeness**: Partial
- **Overview**: printf %c conversion requires number/float
- **Implementation and tests**: internal/analysis/scopes.go:5581; internal/analysis/official_compile_cases_e1000_test.go:1569; internal/analysis/diagnostics_test.go:591
- **Static-analysis boundary**: Only literal/known format and argument types are checked.

### E1037: Cannot use with

- **Completeness**: Partial
- **Overview**: Invalid operator/type combination
- **Implementation and tests**: internal/analysis/scopes.go:5433; internal/analysis/official_compile_cases_e1000_test.go:1691; internal/analysis/diagnostics_test.go:748
- **Static-analysis boundary**: Only resolved static operand types.

### E1038: "vim9script" can only be used in a script

- **Completeness**: Full
- **Overview**: vim9script outside script
- **Implementation and tests**: internal/syntax/scanner.go:84; internal/syntax/official_parser_cases_test.go:687; internal/analysis/official_compile_cases_e1000_test.go:1711
- **Static-analysis boundary**: No material static limitation found.

### E1039: "vim9script" must be the first command in a script

- **Completeness**: Full
- **Overview**: vim9script not first command
- **Implementation and tests**: internal/syntax/scanner.go:88; internal/syntax/official_syntax_test.go:207; internal/syntax/scanner_test.go:103
- **Static-analysis boundary**: No material static limitation found.

### E1040: Cannot use :scriptversion after :vim9script

- **Completeness**: Full
- **Overview**: scriptversion after vim9script
- **Implementation and tests**: internal/syntax/scanner.go:136; internal/syntax/scanner_test.go:930; internal/syntax/official_parser_cases_test.go:686
- **Static-analysis boundary**: No material static limitation found.

### E1041: Redefining script item

- **Completeness**: Partial
- **Overview**: Duplicate Vim9 script item
- **Implementation and tests**: internal/analysis/scopes.go:4627; internal/analysis/official_compile_cases_e1000_test.go:1725; internal/analysis/diagnostics_test.go:7340
- **Static-analysis boundary**: Same-file parsed items only; reload/runtime mutations are outside snapshot analysis.

### E1042: Export can only be used in vim9script

- **Completeness**: Full
- **Overview**: export outside vim9script
- **Implementation and tests**: internal/syntax/scanner.go:1541; internal/syntax/scanner_test.go:1055
- **Static-analysis boundary**: No material static limitation found.

### E1043: Invalid command after :export

- **Completeness**: Full
- **Overview**: Invalid command after export
- **Implementation and tests**: internal/syntax/scanner.go:1545; internal/syntax/scanner_test.go:1018; internal/syntax/official_parser_cases_test.go:206
- **Static-analysis boundary**: No material static limitation found.

### E1044: Export with invalid argument

- **Completeness**: Full
- **Overview**: Invalid export argument
- **Implementation and tests**: internal/syntax/scanner.go:1550; internal/syntax/scanner_test.go:1019; internal/syntax/official_parser_cases_test.go:207
- **Static-analysis boundary**: No material static limitation found.

### E1047: Syntax error in import

- **Completeness**: Full
- **Overview**: Malformed import syntax
- **Implementation and tests**: internal/syntax/declarations.go:68; internal/syntax/declarations_test.go:1056; internal/syntax/official_parser_cases_test.go:593
- **Static-analysis boundary**: No material static limitation found.

### E1048: Item not found in script

- **Completeness**: Partial
- **Overview**: Named import missing from known target
- **Implementation and tests**: internal/analysis/imports.go:61; internal/analysis/imports_test.go:23; Vim runtime/doc/vim9.txt:3558
- **Static-analysis boundary**: Only when workspace target is known; unresolved/runtime targets intentionally produce no diagnostic.

### E1049: Item not exported in script

- **Completeness**: Partial
- **Overview**: Named import not exported
- **Implementation and tests**: internal/analysis/imports.go:67; internal/analysis/imports_test.go:23; Vim runtime/doc/vim9.txt:3565
- **Static-analysis boundary**: Only when workspace target/export inventory is known.

### E1050: Colon required before a range

- **Completeness**: Full
- **Overview**: Range requires colon
- **Implementation and tests**: internal/syntax/scanner.go:1112; internal/syntax/official_parser_cases_test.go:518; internal/analysis/official_compile_cases_e1000_test.go:1737
- **Static-analysis boundary**: No material static limitation found.

### E1051: Wrong argument type for +

- **Completeness**: Partial
- **Overview**: Invalid plus operand types
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/official_compile_cases_e1000_test.go:1787; internal/analysis/diagnostics_test.go:437
- **Static-analysis boundary**: Only known static operand types.

### E1052: Cannot declare an option

- **Completeness**: Full
- **Overview**: Option declaration forbidden
- **Implementation and tests**: internal/syntax/scanner.go:5356; internal/syntax/declarations_test.go:23; internal/analysis/official_compile_cases_e1000_test.go:1905
- **Static-analysis boundary**: No material static limitation found.

### E1053: Could not import

- **Completeness**: Partial
- **Overview**: Import target cannot be found
- **Implementation and tests**: internal/analysis/imports.go:58; internal/analysis/imports_test.go:23; Vim runtime/doc/vim9.txt:3478
- **Static-analysis boundary**: Only workspace/runtimepath discovery snapshot; runtime file creation/loading is unknown.

### E1054: Variable already declared in the script

- **Completeness**: Partial
- **Overview**: Duplicate script variable
- **Implementation and tests**: internal/analysis/scopes.go:4771; internal/analysis/diagnostics_test.go:10823; Vim src/errors.h:2758
- **Static-analysis boundary**: No dedicated official compile fixture; same parsed script only.

### E1055: Missing name after

- **Completeness**: Full
- **Overview**: Missing name after varargs
- **Implementation and tests**: internal/syntax/signature.go:592; internal/syntax/signature_test.go:1076; internal/syntax/official_parser_cases_test.go:636
- **Static-analysis boundary**: No material static limitation found.

### E1056: Expected a type

- **Completeness**: Full
- **Overview**: Missing/invalid return type
- **Implementation and tests**: internal/syntax/signature.go:342; internal/syntax/signature_test.go:792
- **Static-analysis boundary**: No direct official compile fixture; parser-level direct test is adequate.

### E1057: Missing :enddef

- **Completeness**: Full
- **Overview**: Missing enddef
- **Implementation and tests**: internal/syntax/blocks_test.go:39; internal/syntax/official_parser_cases_test.go:602
- **Static-analysis boundary**: No material static limitation found.

### E1058: Function nesting too deep

- **Completeness**: Full
- **Overview**: Function nesting depth limit
- **Implementation and tests**: internal/syntax/blocks_test.go:601; internal/analysis/official_compile_cases_e1000_test.go:1919
- **Static-analysis boundary**: No material static limitation found.

### E1059: No white space allowed before colon

- **Completeness**: Full
- **Overview**: Whitespace before colon
- **Implementation and tests**: internal/syntax/scanner.go:5223; internal/syntax/official_parser_cases_test.go:220; internal/analysis/official_compile_cases_e1000_test.go:2029
- **Static-analysis boundary**: No material static limitation found.

### E1060: Expected dot after name

- **Completeness**: Partial
- **Overview**: Missing member-access dot
- **Implementation and tests**: internal/analysis/scopes.go:3415; internal/analysis/official_compile_cases_e1000_test.go:2079; internal/analysis/diagnostics_test.go:874
- **Static-analysis boundary**: Only resolved import/script-name/member expressions; dynamic names are not inferred.

### E1061: Cannot find function

- **Completeness**: Partial
- **Overview**: 编译期找不到函数（如 :def 引用）
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3428`
- **Static-analysis boundary**: 当前仅覆盖脚本根作用域 `defcompile Name` 中 `Name` 已解析为普通变量或常量的已知非函数目标；未解析名称、局部变量、autoload、`FuncUndefined`、带模式参数及动态函数定义不报告。

### E1062: Cannot index a Number

- **Completeness**: Partial
- **Overview**: Indexing known Number
- **Implementation and tests**: internal/analysis/scopes.go:5844; internal/analysis/official_compile_cases_e1000_test.go:2104; internal/analysis/diagnostics_test.go:1156
- **Static-analysis boundary**: Only statically numeric receivers.

### E1065: Command cannot be shortened

- **Completeness**: Full
- **Overview**: Abbreviated command in Vim9
- **Implementation and tests**: internal/syntax/scanner.go:1197; internal/syntax/command_boundary_test.go:1044; internal/analysis/official_compile_cases_e1000_test.go:2122
- **Static-analysis boundary**: Known command grammar only; unknown user commands remain opaque by design.

### E1066: Cannot declare a register

- **Completeness**: Full
- **Overview**: Register declaration forbidden
- **Implementation and tests**: internal/syntax/scanner.go:5079; internal/syntax/declarations_test.go:100; internal/analysis/official_compile_cases_e1000_test.go:2144
- **Static-analysis boundary**: No material static limitation found.

### E1067: Separator mismatch

- **Completeness**: Full
- **Overview**: Mismatched separator
- **Implementation and tests**: internal/syntax/official_parser_cases_test.go:722; internal/analysis/official_compile_cases_e1000_test.go:2218
- **Static-analysis boundary**: No material static limitation found.

### E1068: No white space allowed before

- **Completeness**: Full
- **Overview**: Illegal Vim9 whitespace
- **Implementation and tests**: internal/syntax/type.go:135; internal/syntax/expression_test.go:352; internal/analysis/official_compile_cases_e1000_test.go:2234
- **Static-analysis boundary**: No material static limitation found.

### E1069: White space required after

- **Completeness**: Full
- **Overview**: Required Vim9 whitespace
- **Implementation and tests**: internal/syntax/type.go:207; internal/syntax/expression_test.go:353; internal/analysis/official_compile_cases_e1000_test.go:2336
- **Static-analysis boundary**: No material static limitation found.

### E1071: Invalid string for :import

- **Completeness**: Full
- **Overview**: Invalid import string
- **Implementation and tests**: internal/syntax/declarations.go:48; internal/syntax/declarations_test.go:1080; internal/analysis/official_compile_cases_e1000_test.go:2438
- **Static-analysis boundary**: No material static limitation found.

### E1072: Cannot compare with

- **Completeness**: Partial
- **Overview**: Invalid comparison types
- **Implementation and tests**: internal/analysis/scopes.go:5423; internal/analysis/official_compile_cases_e1000_test.go:2456; internal/analysis/diagnostics_test.go:799
- **Static-analysis boundary**: Only resolved static operand types.

### E1073: Name already defined

- **Completeness**: Partial
- **Overview**: Name already defined
- **Implementation and tests**: internal/analysis/scopes.go:4771; internal/analysis/official_compile_cases_e1000_test.go:2562; internal/analysis/diagnostics_test.go:7340
- **Static-analysis boundary**: Same parsed scope only; dynamic/reload definitions are outside analysis.

### E1074: No white space allowed after dot

- **Completeness**: Full
- **Overview**: Whitespace after member dot
- **Implementation and tests**: internal/analysis/scopes.go:3415; internal/analysis/official_compile_cases_e1000_test.go:2597; internal/analysis/diagnostics_test.go:1105
- **Static-analysis boundary**: No material static limitation found.

### E1075: Namespace not supported

- **Completeness**: Full
- **Overview**: Unsupported namespace syntax
- **Implementation and tests**: internal/syntax/signature.go:58; internal/syntax/signature_test.go:490; internal/analysis/official_compile_cases_e1000_test.go:2621
- **Static-analysis boundary**: No material static limitation found.

### E1077: Missing argument type for

- **Completeness**: Full
- **Overview**: Missing typed argument annotation
- **Implementation and tests**: internal/syntax/signature.go:628; internal/syntax/signature_test.go:763; internal/syntax/official_parser_cases_test.go:1178
- **Static-analysis boundary**: No material static limitation found.

### E1078: Invalid command "nested", did you mean "++nested"?

- **Completeness**: Full
- **Overview**: Autocmd nested requires ++nested
- **Implementation and tests**: internal/syntax/scanner.go:2857; internal/syntax/autocmd_embedded_test.go:300
- **Static-analysis boundary**: No direct official compile fixture; direct syntax test covers trigger.

### E1080: Invalid assignment

- **Completeness**: Full
- **Overview**: Invalid declaration assignment
- **Implementation and tests**: internal/syntax/scanner.go:5058; internal/syntax/official_parser_cases_test.go:393; internal/analysis/official_compile_cases_e1000_test.go:2685
- **Static-analysis boundary**: No material static limitation found.

### E1081: Cannot unlet

- **Completeness**: Partial
- **Overview**: Cannot unlet protected/non-variable target
- **Implementation and tests**: internal/syntax/scanner.go:4734; internal/analysis/official_compile_cases_e1000_test.go:2699; internal/analysis/diagnostics_test.go:2592
- **Static-analysis boundary**: Only syntactically/resolved forbidden targets; runtime target existence is not modeled.

### E1082: Command modifier without command

- **Completeness**: Full
- **Overview**: Modifier without command
- **Implementation and tests**: internal/syntax/scanner.go:1269; internal/syntax/modifier_range_test.go:168; internal/analysis/official_compile_cases_e1000_test.go:2720
- **Static-analysis boundary**: No material static limitation found.

### E1083: Missing backtick

- **Completeness**: Full
- **Overview**: Missing backtick in interpolated expression
- **Implementation and tests**: internal/syntax/vim9_command.go:82; internal/syntax/scanner_test.go:675; internal/analysis/official_compile_cases_e1000_test.go:2762
- **Static-analysis boundary**: No material static limitation found.

### E1084: Cannot delete Vim9 script function

- **Completeness**: Partial
- **Overview**: 使用 `:delfunction[!]` 删除已经声明的 Vim9 script-local 函数
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3232`
- **Static-analysis boundary**: 当前覆盖同文件中已在删除位置之前声明的顶层 Vim9 `function Name()` 和 `def Name()`，包括脚本顶层或函数体内执行的 `:delfunction[!] Name`。`g:` 全局函数、autoload 名称、Legacy 文件、删除位置之后才声明的函数、缺失函数以及动态或字典 Funcref 目标不报告 E1084。

### E1085: Not a callable type

- **Completeness**: Partial
- **Overview**: Known non-callable type invoked
- **Implementation and tests**: internal/analysis/scopes.go:10319; internal/analysis/official_compile_cases_e1000_test.go:2770; internal/analysis/diagnostics_test.go:5141
- **Static-analysis boundary**: Only statically non-callable values; dynamic callable resolution is unknown.

### E1087: Cannot use an index when declaring a variable

- **Completeness**: Full
- **Overview**: Object/List invalid declaration assignment
- **Implementation and tests**: internal/syntax/scanner.go:5100; internal/syntax/official_parser_cases_test.go:394; internal/analysis/official_compile_cases_e1000_test.go:2798
- **Static-analysis boundary**: No material static limitation found.

### E1088: Script cannot import itself

- **Completeness**: Partial
- **Overview**: Script imports itself
- **Implementation and tests**: internal/analysis/imports.go:38; internal/analysis/imports_test.go:23; Vim runtime/doc/vim9.txt:3584
- **Static-analysis boundary**: Only statically normalized paths; runtimepath/symlink identity may remain unknown.

### E1089: Unknown variable

- **Completeness**: Partial
- **Overview**: Unknown Vim9 root variable
- **Implementation and tests**: internal/analysis/scopes.go:9000; internal/analysis/official_compile_cases_e1000_test.go:2824; internal/analysis/diagnostics_test.go:3846
- **Static-analysis boundary**: Only statically unknown roots; globals/runtime injections intentionally remain unknown.

### E1090: Cannot assign to argument

- **Completeness**: Partial
- **Overview**: Assignment to function argument
- **Implementation and tests**: internal/analysis/scopes.go:7341; internal/analysis/diagnostics_test.go:3846; Vim src/errors.h:2832
- **Static-analysis boundary**: No dedicated official compile fixture; only resolved parameter bindings.

### E1092: Cannot nest :redir

- **Completeness**: Full
- **Overview**: Nested redir
- **Implementation and tests**: internal/syntax/blocks_test.go:136; internal/analysis/official_compile_cases_e1000_test.go:2874
- **Static-analysis boundary**: No material static limitation found.

### E1093: Expected items but got

- **Completeness**: Partial
- **Overview**: Destructuring item-count mismatch
- **Implementation and tests**: internal/analysis/scopes.go:5075; internal/analysis/official_compile_cases_e1000_test.go:2892; internal/analysis/diagnostics_test.go:2332
- **Static-analysis boundary**: Only literal/known tuple/list arity; dynamic List length is unknown.

### E1094: Import can only be used in a script

- **Completeness**: Full
- **Overview**: Import outside script
- **Implementation and tests**: internal/syntax/declarations.go:73; internal/syntax/declarations_test.go:1031; internal/analysis/official_compile_cases_e1000_test.go:2936
- **Static-analysis boundary**: No material static limitation found.

### E1095: Unreachable code after

- **Completeness**: Partial
- **Overview**: Unreachable code after terminal command
- **Implementation and tests**: internal/analysis/scopes.go:3696; internal/analysis/official_compile_cases_e1000_test.go:2964; Vim runtime/doc/userfunc.txt:206
- **Static-analysis boundary**: Only syntactically recognized terminal commands/control flow.

### E1096: Returning a value in a function without a return type

- **Completeness**: Partial
- **Overview**: Value returned from untyped function
- **Implementation and tests**: internal/analysis/scopes.go:3534; internal/analysis/diagnostics_test.go:7949; Vim runtime/doc/vim9.txt:1408
- **Static-analysis boundary**: No dedicated official compile fixture; applies to parsed known function form.

### E1097: Line incomplete

- **Completeness**: Full
- **Overview**: Incomplete Vim9 line/expression
- **Implementation and tests**: internal/syntax/scanner.go:5268; internal/syntax/expression_test.go:1388; internal/analysis/official_compile_cases_e1000_test.go:3001
- **Static-analysis boundary**: No material static limitation found.

### E1100: Command not supported in Vim9 script (missing :var?)

- **Completeness**: Full
- **Overview**: Unsupported Vim9 command form
- **Implementation and tests**: internal/syntax/scanner.go:1616; internal/syntax/text_body_test.go:153; internal/analysis/official_compile_cases_e1100_test.go:20
- **Static-analysis boundary**: No material static limitation found.

### E1101: Cannot declare a script variable in a function

- **Completeness**: Full
- **Overview**: Script variable inside function
- **Implementation and tests**: internal/syntax/scanner.go:5386; internal/syntax/declarations_test.go:2210; internal/analysis/official_compile_cases_e1100_test.go:122
- **Static-analysis boundary**: No material static limitation found.

### E1104: Missing >

- **Completeness**: Full
- **Overview**: Missing generic type closing >
- **Implementation and tests**: internal/syntax/expression_test.go:1353; internal/syntax/official_parser_cases_test.go:960; internal/analysis/official_compile_cases_e1100_test.go:160
- **Static-analysis boundary**: No material static limitation found.

### E1105: Cannot convert to string

- **Completeness**: Partial
- **Overview**: Invalid static conversion to string
- **Implementation and tests**: internal/analysis/scopes.go:5151; internal/analysis/official_compile_cases_e1100_test.go:182; internal/analysis/diagnostics_test.go:7763
- **Static-analysis boundary**: Only known source types; runtime conversion behavior is not simulated.

### E1106: One argument too many

- **Completeness**: Partial
- **Overview**: Too many callback/function arguments
- **Implementation and tests**: internal/analysis/scopes.go:9774; internal/analysis/official_compile_cases_e1100_test.go:306; internal/analysis/diagnostics_test.go:1839
- **Static-analysis boundary**: Only statically known callable signatures.

### E1107: String, List, Dict or Blob required

- **Completeness**: Partial
- **Overview**: Invalid receiver for method-like call
- **Implementation and tests**: internal/analysis/scopes.go:5834; internal/analysis/official_compile_cases_e1100_test.go:324; internal/analysis/diagnostics_test.go:7412
- **Static-analysis boundary**: Only statically known receiver types.

### E1117: Cannot use ! with nested

- **Completeness**: Full
- **Overview**: ! not allowed with nested command
- **Implementation and tests**: internal/syntax/scanner.go:3029; internal/syntax/blocks_test.go:258; internal/analysis/official_compile_cases_e1100_test.go:362
- **Static-analysis boundary**: No material static limitation found.

### E1118: Cannot change locked list

- **Completeness**: Partial
- **Overview**: 向 const / 锁定列表写入不存在的索引
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3402`
- **Static-analysis boundary**: 当前覆盖 `def` 中 const 列表字面量后立即写静态可证的越界索引；动态索引、脚本级、别名和运行时锁定/解锁路径不推断。

### E1119: Cannot change locked list item

- **Completeness**: Partial
- **Overview**: 修改被锁定 list 的元素
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3374`
- **Static-analysis boundary**: 当前覆盖 `def` 中 `lockvar list[index]` 后立即写同一文本目标，以及 const 列表字面量后立即写静态可证的既有索引；脚本级、动态索引、别名和跨命令状态不推断。

### E1120: Cannot change dict

- **Completeness**: Partial
- **Overview**: 修改被锁定的 dict
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3798`
- **Static-analysis boundary**: 当前覆盖 `def` 中 const 字典字面量后立即写入静态可证的新 key；别名、动态 key、跨命令和运行时锁定/解锁路径不推断。

### E1121: Cannot change dict item

- **Completeness**: Partial
- **Overview**: 修改被锁定 dict 的元素
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3339`
- **Static-analysis boundary**: 当前覆盖同一作用域内 `lockvar dict.item` 后立即写同一文本目标，以及 `def` 中 const 字典字面量后立即写静态可证的既有 key；动态 key、别名、跨命令和条件控制流不推断。

### E1122: Variable is locked

- **Completeness**: Partial
- **Overview**: def 内给 const 赋值的变体消息
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3305`
- **Static-analysis boundary**: 当前覆盖同一作用域内相邻的 `lockvar 0 target` 与整体赋值，以及带作用域前缀的 `final` 声明后立即重赋值；跨分支、跨命令、默认锁深度和动态锁状态不推断。

### E1123: Missing comma before argument

- **Completeness**: Full
- **Overview**: Missing enum argument comma
- **Implementation and tests**: internal/syntax/declarations.go:485; internal/syntax/expression_test.go:1958; internal/analysis/official_compile_cases_e1100_test.go:400
- **Static-analysis boundary**: No material static limitation found.

### E1124: cannot be used in legacy Vim script

- **Completeness**: Full
- **Overview**: Vim9 特性用于 legacy 脚本
- **Implementation and tests**: `internal/syntax/official_parser_cases_test.go:388`; `internal/syntax/scanner_test.go:943`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1125: Final requires a value

- **Completeness**: Full
- **Overview**: final without value
- **Implementation and tests**: internal/syntax/scanner.go:3248; internal/syntax/declarations_test.go:58; internal/analysis/official_compile_cases_e1100_test.go:414
- **Static-analysis boundary**: No material static limitation found.

### E1126: Cannot use :let in Vim9 script

- **Completeness**: Full
- **Overview**: :let in Vim9
- **Implementation and tests**: internal/syntax/scanner.go:3210; internal/syntax/official_parser_cases_test.go:413; internal/analysis/official_compile_cases_e1100_test.go:428
- **Static-analysis boundary**: No material static limitation found.

### E1127: Missing name after dot

- **Completeness**: Full
- **Overview**: Missing member name after dot
- **Implementation and tests**: internal/syntax/scanner.go:3680; internal/syntax/expression_test.go:1489; internal/analysis/official_compile_cases_e1100_test.go:450
- **Static-analysis boundary**: No material static limitation found.

### E1128: } without {

- **Completeness**: Full
- **Overview**: Stray closing brace
- **Implementation and tests**: internal/syntax/scanner.go:2942; internal/syntax/command_body_test.go:432; internal/syntax/official_parser_cases_test.go:1163
- **Static-analysis boundary**: No material static limitation found.

### E1135: Using a String as a Bool

- **Completeness**: Partial
- **Overview**: String used as Bool
- **Implementation and tests**: internal/analysis/scopes.go:9823; internal/analysis/official_compile_cases_e1100_test.go:494; internal/analysis/diagnostics_test.go:1805
- **Static-analysis boundary**: Only statically known string expressions.

### E1138: Using a Bool as a Number

- **Completeness**: Partial
- **Overview**: Bool used as Number
- **Implementation and tests**: internal/analysis/scopes.go:9828; internal/analysis/official_compile_cases_e1100_test.go:609; internal/analysis/diagnostics_test.go:1888
- **Static-analysis boundary**: Only statically known bool expressions.

### E1139: Missing matching bracket after dict key

- **Completeness**: Full
- **Overview**: Dictionary key missing its closing bracket is parsed and diagnosed.
- **Implementation and tests**: internal/syntax/expression.go:2474; internal/syntax/expression_test.go:1151; /Users/chemzqm/lib/vim/runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct local assertion; parser recovery variants are limited.

### E1141: Indexable type required

- **Completeness**: Partial
- **Overview**: Index/slice receiver must be statically indexable.
- **Implementation and tests**: internal/analysis/scopes.go:7466; internal/analysis/diagnostics_test.go:2104; official_compile_cases_e1100_test.go:695; Vim runtime/doc/eval.txt
- **Static-analysis boundary**: Unknown/any and dynamic legacy values are deliberately not rejected.

### E1143: Empty expression

- **Completeness**: Full
- **Overview**: Empty expression after an Ex construct is detected by scanner.
- **Implementation and tests**: internal/syntax/scanner.go:1656; official_compile_cases_e1100_test.go:767; Vim runtime/doc/eval.txt
- **Static-analysis boundary**: Direct test; only parser-recognized command payloads are covered.

### E1144: Command is not followed by white space

- **Completeness**: Full
- **Overview**: Command must be followed by whitespace when Vim requires it.
- **Implementation and tests**: internal/syntax/scanner.go:1446; official_compile_cases_e1100_test.go:798; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct parser test.

### E1145: Missing heredoc end marker

- **Completeness**: Full
- **Overview**: Missing heredoc end marker is diagnosed at EOF/block close.
- **Implementation and tests**: internal/syntax/blocks.go:660; internal/syntax/heredoc_test.go:299; official_compile_cases_e1100_test.go:900
- **Static-analysis boundary**: Direct tests for command and expression heredocs.

### E1148: Cannot index a

- **Completeness**: Partial
- **Overview**: 对 string 等运行时值执行不支持的索引或成员写入 / 删除
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3270`
- **Static-analysis boundary**: 当前覆盖 Vim9 写入和 `:unlet` 中的已知字符串接收者；普通读取、动态 `any`、外部全局状态及其他未知接收者不报告。

### E1151: Mismatched endfunction

- **Completeness**: Full
- **Overview**: Mismatched :endfunction is diagnosed by block matching.
- **Implementation and tests**: internal/syntax/blocks.go:494; internal/syntax/blocks_test.go:161; Vim runtime/doc/userfunc.txt
- **Static-analysis boundary**: Direct parser test.

### E1152: Mismatched enddef

- **Completeness**: Full
- **Overview**: Mismatched :enddef is diagnosed by block matching.
- **Implementation and tests**: internal/syntax/blocks.go:496; internal/syntax/blocks_test.go:166; Vim src/testdir/test_vim9_func.vim
- **Static-analysis boundary**: Direct parser test.

### E1153: Invalid operation for

- **Completeness**: Partial
- **Overview**: Invalid operation on a statically known object is diagnosed.
- **Implementation and tests**: internal/analysis/scopes.go:5394; internal/analysis/diagnostics_test.go:2202; Vim runtime/doc/eval.txt
- **Static-analysis boundary**: Unknown/dynamic receiver types are not diagnosed.

### E1157: Missing return type

- **Completeness**: Full
- **Overview**: Lambda return type omission is parsed and diagnosed.
- **Implementation and tests**: internal/syntax/expression.go:1527; internal/syntax/expression_test.go:818; official_compile_cases_e1100_test.go:916
- **Static-analysis boundary**: Direct parser test.

### E1158: Cannot use flatten() in Vim9 script, use flattennew()

- **Completeness**: Full
- **Overview**: flatten() use in Vim9 is rejected in builtin-call analysis.
- **Implementation and tests**: internal/analysis/scopes.go:10463; internal/analysis/diagnostics_test.go:2261; official_compile_cases_e1100_test.go:938
- **Static-analysis boundary**: Direct builtin/name and Vim9-context tests.

### E1160: Cannot use a default for variable arguments

- **Completeness**: Full
- **Overview**: Varargs parameter cannot have a default value.
- **Implementation and tests**: internal/syntax/signature.go:634; internal/syntax/signature_test.go:444; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct signature test.

### E1163: Variable : type mismatch, expected but got

- **Completeness**: Partial
- **Overview**: Argument type mismatch is reported when expected and actual types are known.
- **Implementation and tests**: internal/analysis/scopes.go:6393; internal/analysis/diagnostics_test.go:2313; official_compile_cases_e1100_test.go:960
- **Static-analysis boundary**: Unknown/any calls and runtime values are conservatively not rejected.

### E1164: vim9cmd must be followed by a command

- **Completeness**: Full
- **Overview**: vim9cmd without following command is diagnosed.
- **Implementation and tests**: internal/syntax/scanner.go:1267; internal/syntax/modifier_range_test.go:24; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct scanner test.

### E1165: Cannot use a range with an assignment

- **Completeness**: Partial
- **Overview**: Slice assignment is rejected for statically invalid targets.
- **Implementation and tests**: internal/analysis/scopes.go:6290; internal/analysis/diagnostics_test.go:2107; official_compile_cases_e1100_test.go:988
- **Static-analysis boundary**: Dynamic target/container type is not proven.

### E1166: Cannot use a range with a dictionary

- **Completeness**: Partial
- **Overview**: Dictionary range removal is rejected for statically known dictionaries.
- **Implementation and tests**: internal/analysis/scopes.go:7043; internal/analysis/diagnostics_test.go:2490; official_compile_cases_e1100_test.go:1003
- **Static-analysis boundary**: Unknown/dynamic targets are not diagnosed.

### E1167: Argument name shadows existing variable

- **Completeness**: Full
- **Overview**: Function argument shadowing a local is diagnosed from lexical scope.
- **Implementation and tests**: internal/analysis/scopes.go:1467; internal/analysis/diagnostics_test.go:2649; official_compile_cases_e1100_test.go:1018
- **Static-analysis boundary**: Direct lexical-scope test.

### E1168: Argument already declared in the script

- **Completeness**: Full
- **Overview**: Duplicate script argument declaration is diagnosed from script scope.
- **Implementation and tests**: internal/analysis/scopes.go:1454; internal/analysis/diagnostics_test.go:2716; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct local test; external reload state is outside file analysis.

### E1170: Cannot use #{ to start a comment

- **Completeness**: Full
- **Overview**: #{ is rejected where it would start an invalid Vim9 comment.
- **Implementation and tests**: internal/syntax/expression.go:190; internal/syntax/official_parser_cases_test.go:172; official_compile_cases_e1100_test.go:1065
- **Static-analysis boundary**: Direct parser assertions across contexts.

### E1171: Missing } after inline function

- **Completeness**: Full
- **Overview**: Inline lambda missing closing brace is diagnosed.
- **Implementation and tests**: internal/syntax/expression.go:1587; internal/syntax/expression_test.go:928; official_compile_cases_e1100_test.go:1169
- **Static-analysis boundary**: Direct parser test.

### E1172: Cannot use default values in a lambda

- **Completeness**: Full
- **Overview**: Lambda default arguments are rejected.
- **Implementation and tests**: internal/syntax/expression.go:1510; internal/syntax/official_parser_cases_test.go:614; official_compile_cases_e1100_test.go:1184
- **Static-analysis boundary**: Direct parser test.

### E1173: Text found after

- **Completeness**: Full
- **Overview**: Trailing text after no-argument command is diagnosed.
- **Implementation and tests**: internal/syntax/scanner.go:1518; internal/syntax/scanner_test.go:1103; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct scanner test.

### E1174: String required for argument

- **Completeness**: Partial
- **Overview**: Builtin argument is checked as String when its static type is known.
- **Implementation and tests**: internal/analysis/scopes.go:10101; internal/analysis/diagnostics_test.go:4597; official_compile_cases_e1100_test.go:1208
- **Static-analysis boundary**: Unknown/any argument values are not rejected.

### E1175: Non-empty string required for argument

- **Completeness**: Partial
- **Overview**: 内置函数参数需要非空 string
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3110`
- **Static-analysis boundary**: 当前覆盖 Vim 9.2.1015 中不依赖构建特性的对应内置函数，并仅对空字符串字面量报告；变量、函数返回值和其他动态字符串不推断，依赖 `+clientserver` 的 `remote_startserver()` 不做无条件诊断。

### E1176: Misplaced command modifier

- **Completeness**: Full
- **Overview**: Command modifier placement is validated by scanner.
- **Implementation and tests**: internal/syntax/scanner.go:2707; internal/syntax/official_parser_cases_test.go:519; official_compile_cases_e1100_test.go:1290
- **Static-analysis boundary**: Direct parser test.

### E1177: For loop on not supported

- **Completeness**: Partial
- **Overview**: For-loop iterable unsupported type is reported when statically known.
- **Implementation and tests**: internal/analysis/scopes.go:6204; internal/analysis/diagnostics_test.go:2859; official_compile_cases_e1100_test.go:1409
- **Static-analysis boundary**: Dynamic/unknown iterable types are intentionally not rejected.

### E1178: Cannot lock or unlock a local variable

- **Completeness**: Full
- **Overview**: Lock/unlock of a local binding is rejected by scope analysis.
- **Implementation and tests**: internal/analysis/scopes.go:7128; internal/analysis/diagnostics_test.go:3007; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct local-binding test.

### E1180: Variable arguments type must be a list

- **Completeness**: Full
- **Overview**: Varargs type must be list is parsed from signature syntax.
- **Implementation and tests**: internal/syntax/type.go:279; internal/syntax/type_test.go:193; official_compile_cases_e1100_test.go:1453
- **Static-analysis boundary**: Direct type parser test.

### E1181: Cannot use an underscore here

- **Completeness**: Full
- **Overview**: Underscore in forbidden expression position is diagnosed.
- **Implementation and tests**: internal/analysis/scopes.go:9066; internal/analysis/diagnostics_test.go:3843; official_compile_cases_e1100_test.go:1468
- **Static-analysis boundary**: Direct local test.

### E1182: Cannot define a dict function in Vim9 script

- **Completeness**: Full
- **Overview**: Dict function definition is rejected in Vim9 syntax.
- **Implementation and tests**: internal/syntax/signature.go:101; internal/syntax/signature_test.go:519; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct signature test.

### E1183: Cannot use a range with an assignment operator

- **Completeness**: Partial
- **Overview**: Range with compound assignment is rejected for recognized assignment targets.
- **Implementation and tests**: internal/analysis/scopes.go:7378; internal/analysis/diagnostics_test.go:2398; Vim runtime/doc/eval.txt
- **Static-analysis boundary**: Complex/dynamic assignment targets retain conservative recovery.

### E1185: Missing :redir END

- **Completeness**: Full
- **Overview**: Unterminated :redir is diagnosed at block completion.
- **Implementation and tests**: internal/syntax/blocks.go:85; internal/syntax/blocks_test.go:138; official_compile_cases_e1100_test.go:1510
- **Static-analysis boundary**: Direct block test.

### E1186: Expression does not result in a value

- **Completeness**: Partial
- **Overview**: Void-valued expression used where a value is required is diagnosed when return type is known.
- **Implementation and tests**: internal/analysis/scopes.go:6026; internal/analysis/diagnostics_test.go:531; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Unknown function returns are not assumed void.

### E1189: Cannot use :legacy with this command

- **Completeness**: Full
- **Overview**: :legacy is rejected with commands that cannot be legacy-prefixed.
- **Implementation and tests**: internal/syntax/blocks.go:64; internal/syntax/blocks_test.go:752; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct command-context tests.

### E1190: One argument too few

- **Completeness**: Partial
- **Overview**: Builtin argument count/shape is checked for recognized builtins.
- **Implementation and tests**: internal/analysis/scopes.go:9763; internal/analysis/diagnostics_test.go:5455; official_compile_cases_e1100_test.go:1525
- **Static-analysis boundary**: Only modeled builtin signatures and statically inspectable arguments.

### E1202: No white space allowed after

- **Completeness**: Full
- **Overview**: Forbidden whitespace in generic calls/types is diagnosed.
- **Implementation and tests**: internal/syntax/expression.go:669; internal/analysis/diagnostics_test.go:1108; official_compile_cases_e1200_test.go:16
- **Static-analysis boundary**: Direct parser and compile-case tests.

### E1203: Dot not allowed after a

- **Completeness**: Full
- **Overview**: 数字后跟 .key
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:5587`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1205: No white space allowed between option and

- **Completeness**: Full
- **Overview**: Whitespace between :set option and operator is diagnosed.
- **Implementation and tests**: internal/syntax/set_command.go:31; internal/syntax/set_command_test.go:182; Vim src/testdir/test_vim9_script.vim
- **Static-analysis boundary**: Direct command parser test.

### E1206: Dictionary required for argument

- **Completeness**: Partial
- **Overview**: Builtin Dictionary argument requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:10103; internal/analysis/diagnostics_test.go:4604; official_compile_cases_e1200_test.go:20
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1207: Expression without an effect

- **Completeness**: Partial
- **Overview**: Effectless expression statement is diagnosed in Vim9 analysis.
- **Implementation and tests**: internal/analysis/scopes.go:1341; internal/analysis/diagnostics_test.go:5520; official_compile_cases_e1200_test.go:126
- **Static-analysis boundary**: Direct tests cover representative expression forms; unknown command execution is opaque.

### E1208: complete used without allowing arguments

- **Completeness**: Full
- **Overview**: :command -complete requires argument allowance.
- **Implementation and tests**: internal/syntax/scanner.go:4085; internal/syntax/command_body_test.go:210; Vim runtime/doc/map.txt
- **Static-analysis boundary**: Direct command parser test.

### E1210: Number required for argument

- **Completeness**: Partial
- **Overview**: Builtin Number requirement is checked for statically known arguments.
- **Implementation and tests**: internal/analysis/scopes.go:10019; internal/analysis/diagnostics_test.go:5626; official_compile_cases_e1200_test.go:240
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1211: List required for argument

- **Completeness**: Partial
- **Overview**: Builtin List requirement is checked for statically known arguments.
- **Implementation and tests**: internal/analysis/scopes.go:10007; internal/analysis/diagnostics_test.go:5679; official_compile_cases_e1200_test.go:322
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1212: Bool required for argument

- **Completeness**: Partial
- **Overview**: Builtin Bool requirement is checked for statically known arguments.
- **Implementation and tests**: internal/analysis/scopes.go:9995; internal/analysis/diagnostics_test.go:5734; official_compile_cases_e1200_test.go:404
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1213: Redefining imported item

- **Completeness**: Full
- **Overview**: Redeclaration of an imported item is diagnosed.
- **Implementation and tests**: internal/analysis/scopes.go:4825; internal/analysis/diagnostics_test.go:915; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct import-scope test.

### E1216: digraph_setlist() argument must be a list of lists with two items

- **Completeness**: Partial
- **Overview**: digraph_setlist() literal structure is checked.
- **Implementation and tests**: internal/analysis/scopes.go:9744; internal/analysis/diagnostics_test.go:6510; official_compile_cases_e1200_test.go:486
- **Static-analysis boundary**: Only statically inspectable list literals/shapes are validated.

### E1217: Channel or Job required for argument

- **Completeness**: Partial
- **Overview**: Builtin Channel-or-Job requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:10023; internal/analysis/diagnostics_test.go:6564; official_compile_cases_e1200_test.go:504
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1218: Job required for argument

- **Completeness**: Partial
- **Overview**: Builtin Job requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:10027; internal/analysis/diagnostics_test.go:6617; official_compile_cases_e1200_test.go:586
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1219: Float or Number required for argument

- **Completeness**: Partial
- **Overview**: Builtin Float-or-Number requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:10031; internal/analysis/diagnostics_test.go:6670; official_compile_cases_e1200_test.go:628
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1220: String or Number required for argument

- **Completeness**: Partial
- **Overview**: Builtin String-or-Number requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:10035; internal/analysis/diagnostics_test.go:6725; official_compile_cases_e1200_test.go:710
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1221: String or Blob required for argument

- **Completeness**: Partial
- **Overview**: Builtin String-or-Blob requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:10039; internal/analysis/diagnostics_test.go:6779; official_compile_cases_e1200_test.go:792
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1222: String or List required for argument

- **Completeness**: Partial
- **Overview**: Builtin String-or-List requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:10043; internal/analysis/diagnostics_test.go:6833; official_compile_cases_e1200_test.go:818
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1223: String or Dictionary required for argument

- **Completeness**: Partial
- **Overview**: Builtin String-or-Dictionary requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:10051; internal/analysis/diagnostics_test.go:6887; official_compile_cases_e1200_test.go:900
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1224: String, Number or List required for argument

- **Completeness**: Partial
- **Overview**: Builtin String-Number-List requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:10055; internal/analysis/diagnostics_test.go:6941; official_compile_cases_e1200_test.go:918
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1225: String, List, Tuple or Dictionary required for argument

- **Completeness**: Partial
- **Overview**: Builtin String-List-Tuple-Dictionary requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:10059; internal/analysis/diagnostics_test.go:6996; official_compile_cases_e1200_test.go:1000
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1226: List or Blob required for argument

- **Completeness**: Partial
- **Overview**: Builtin List-or-Blob requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:10063; internal/analysis/diagnostics_test.go:7050; official_compile_cases_e1200_test.go:1022
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1228: List, Dictionary or Blob required for argument

- **Completeness**: Partial
- **Overview**: Builtin List-Dictionary-Blob requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:10067; internal/analysis/diagnostics_test.go:7095; official_compile_cases_e1200_test.go:1040
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1229: Expected dictionary for using key , but got

- **Completeness**: Partial
- **Overview**: Member access on known non-Dictionary receiver is diagnosed.
- **Implementation and tests**: internal/analysis/scopes.go:6269; internal/analysis/diagnostics_test.go:7167; official_compile_cases_e1200_test.go:1050
- **Static-analysis boundary**: Unknown/dynamic receiver type is not rejected.

### E1231: Cannot use a bar to separate commands here

- **Completeness**: Full
- **Overview**: Bar separator prohibited in command payload is diagnosed.
- **Implementation and tests**: internal/syntax/scanner.go:1644; internal/syntax/autocmd_embedded_test.go:12; Vim runtime/doc/map.txt
- **Static-analysis boundary**: Direct embedded-command tests.

### E1232: Argument of exists_compiled() must be a literal string

- **Completeness**: Full
- **Overview**: exists_compiled() requires literal string argument.
- **Implementation and tests**: internal/analysis/scopes.go:9703; internal/analysis/diagnostics_test.go:7234; official_compile_cases_e1200_test.go:1065
- **Static-analysis boundary**: Direct context and literal tests.

### E1233: exists_compiled() can only be used in a :def function

- **Completeness**: Full
- **Overview**: exists_compiled() outside :def is diagnosed.
- **Implementation and tests**: internal/analysis/scopes.go:9709; internal/analysis/diagnostics_test.go:7282; official_compile_cases_e1200_test.go:1091
- **Static-analysis boundary**: Direct context tests.

### E1234: legacy must be followed by a command

- **Completeness**: Full
- **Overview**: legacy without following command is diagnosed.
- **Implementation and tests**: internal/syntax/scanner.go:1265; internal/syntax/modifier_range_test.go:79; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct scanner test.

### E1235: Bool or Number required for argument

- **Completeness**: Partial
- **Overview**: Builtin Bool-or-Number requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:9999; internal/analysis/diagnostics_test.go:5789; official_compile_cases_e1200_test.go:1109
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1236: Cannot use itself, it is imported

- **Completeness**: Full
- **Overview**: Imported namespace itself cannot be used as a value.
- **Implementation and tests**: internal/analysis/scopes.go:3394; internal/analysis/diagnostics_test.go:912; official_compile_cases_e1200_test.go:1127
- **Static-analysis boundary**: Direct import-reference tests.

### E1238: Blob required for argument

- **Completeness**: Partial
- **Overview**: Builtin Blob requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:10003; internal/analysis/diagnostics_test.go:5842; official_compile_cases_e1200_test.go:1147
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1241: Separator not supported

- **Completeness**: Full
- **Overview**: Unsupported :substitute separator in Vim9 is diagnosed.
- **Implementation and tests**: internal/syntax/substitute.go:45; internal/syntax/substitute_test.go:105; official_compile_cases_e1200_test.go:1165
- **Static-analysis boundary**: Direct command parser tests.

### E1242: No white space allowed before separator

- **Completeness**: Full
- **Overview**: Whitespace before unsupported substitute separator is diagnosed.
- **Implementation and tests**: internal/syntax/substitute.go:38; internal/syntax/substitute_test.go:105; official_compile_cases_e1200_test.go:1207
- **Static-analysis boundary**: Direct command parser tests.

### E1246: Cannot find variable to (un)lock

- **Completeness**: Partial
- **Overview**: lockvar 目标不存在
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:3066`
- **Static-analysis boundary**: 当前仅报告 Vim9 中直接、无作用域前缀且在整个可见作用域链都没有声明的目标；文件含 `execute` 时跳过，带作用域前缀、成员/索引目标及其他动态变量入口不推断。

### E1247: Line number out of range

- **Completeness**: Full
- **Overview**: Out-of-range command line number is diagnosed during scanner range parsing.
- **Implementation and tests**: internal/syntax/scanner.go:1128; internal/syntax/scanner_test.go:272; Vim runtime/doc/cmdline.txt
- **Static-analysis boundary**: Direct overflow test.

### E1251: List, Tuple, Dictionary, Blob or String required for argument

- **Completeness**: Partial
- **Overview**: Builtin aggregate-or-String requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:10071; internal/analysis/diagnostics_test.go:5896; official_compile_cases_e1200_test.go:1289
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1253: String, List, Tuple or Blob required for argument

- **Completeness**: Partial
- **Overview**: Builtin String-List-Tuple-Blob requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:10079; internal/analysis/diagnostics_test.go:5952; official_compile_cases_e1200_test.go:1363
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1254: Cannot use script variable in for loop

- **Completeness**: Full
- **Overview**: Script variable as :for binding is rejected.
- **Implementation and tests**: internal/analysis/scopes.go:6143; internal/analysis/diagnostics_test.go:2944; official_compile_cases_e1200_test.go:1381
- **Static-analysis boundary**: Direct lexical-binding test.

### E1256: String or function required for argument

- **Completeness**: Partial
- **Overview**: Builtin String-or-function requirement is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:9801; internal/analysis/diagnostics_test.go:72; official_compile_cases_e1200_test.go:1396
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1257: Imported script must use "as" or end in .vim

- **Completeness**: Full
- **Overview**: Import path must use as or end in .vim.
- **Implementation and tests**: internal/syntax/declarations.go:63; internal/syntax/declarations_test.go:1084; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct import parser test.

### E1258: No '.' after imported name

- **Completeness**: Full
- **Overview**: Imported name cannot be followed by dot in invalid form.
- **Implementation and tests**: internal/analysis/scopes.go:3388; internal/analysis/diagnostics_test.go:967; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct import-reference test.

### E1259: Missing name after imported name

- **Completeness**: Full
- **Overview**: Missing member after imported namespace is diagnosed.
- **Implementation and tests**: internal/analysis/scopes.go:3357; internal/analysis/diagnostics_test.go:1032; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct import-reference test.

### E1260: Cannot unlet an imported item

- **Completeness**: Full
- **Overview**: :unlet imported item is rejected.
- **Implementation and tests**: internal/analysis/scopes.go:6994; internal/analysis/diagnostics_test.go:2589; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct import-scope test.

### E1261: Cannot import .vim without using "as"

- **Completeness**: Full
- **Overview**: Importing .vim without as is rejected.
- **Implementation and tests**: internal/syntax/declarations.go:61; internal/syntax/declarations_test.go:1083; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct parser test.

### E1262: Cannot import the same script twice

- **Completeness**: Partial
- **Overview**: Duplicate imported script is diagnosed for indexed import paths.
- **Implementation and tests**: internal/analysis/imports.go:44; internal/analysis/imports_test.go:23; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Resolution/index coverage can be incomplete for unavailable/dynamic workspace files.

### E1263: Cannot use name with # in Vim9 script, use export instead

- **Completeness**: Full
- **Overview**: Vim9 function name containing # is rejected.
- **Implementation and tests**: internal/syntax/signature.go:112; internal/syntax/signature_test.go:519; Vim runtime/doc/userfunc.txt
- **Static-analysis boundary**: Direct signature test.

### E1264: Autoload import cannot use absolute or relative path

- **Completeness**: Full
- **Overview**: Autoload import cannot use absolute/relative path.
- **Implementation and tests**: internal/analysis/imports.go:53; internal/analysis/imports_test.go:23; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct import analysis test.

### E1267: Function name must start with a capital

- **Completeness**: Full
- **Overview**: Vim9 function name must start uppercase.
- **Implementation and tests**: internal/syntax/signature.go:141; internal/syntax/signature_test.go:519; Vim runtime/doc/userfunc.txt
- **Static-analysis boundary**: Direct signature test.

### E1268: Cannot use s: in Vim9 script

- **Completeness**: Full
- **Overview**: Script-local prefix is forbidden in Vim9 declarations.
- **Implementation and tests**: internal/syntax/signature.go:84; internal/syntax/signature_test.go:494; Vim runtime/doc/vim9.txt
- **Static-analysis boundary**: Direct scanner/signature tests.

### E1269: Cannot create a Vim9 script variable in a function

- **Completeness**: Full
- **Overview**: 函数内创建 s: 变量
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:2551`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1270: Cannot use :s\/sub/ in Vim9 script

- **Completeness**: Full
- **Overview**: :s\\ shorthand is forbidden in Vim9 script.
- **Implementation and tests**: internal/syntax/substitute.go:110; internal/syntax/substitute_test.go:93; Vim runtime/doc/change.txt
- **Static-analysis boundary**: Direct substitute parser test.

### E1278: Stray '}' without a matching '{'

- **Completeness**: Full
- **Overview**: Stray closing brace is diagnosed by scanner.
- **Implementation and tests**: internal/syntax/scanner.go:2283; internal/syntax/official_parser_cases_test.go:214; official_compile_cases_e1200_test.go:1462
- **Static-analysis boundary**: Direct parser test.

### E1279: Missing '}'

- **Completeness**: Full
- **Overview**: Missing closing brace is diagnosed by scanner.
- **Implementation and tests**: internal/syntax/scanner.go:2300; internal/syntax/official_parser_cases_test.go:208; official_compile_cases_e1200_test.go:1488
- **Static-analysis boundary**: Direct parser test.

### E1282: Bitshift operands must be numbers

- **Completeness**: Partial
- **Overview**: Bitshift operand type is checked when statically known.
- **Implementation and tests**: internal/analysis/scopes.go:5514; internal/analysis/diagnostics_test.go:1957; Vim runtime/doc/eval.txt
- **Static-analysis boundary**: Unknown/any operands are not rejected.

### E1283: Bitshift amount must be a positive number

- **Completeness**: Partial
- **Overview**: Bitshift amount positivity is checked when statically known.
- **Implementation and tests**: internal/analysis/scopes.go:5546; internal/analysis/diagnostics_test.go:2033; Vim runtime/doc/eval.txt
- **Static-analysis boundary**: Unknown/dynamic amount is not rejected.

### E1300: Cannot use a partial with dictionary for :defer

- **Completeness**: Partial
- **Overview**: :defer 传 partial+dict
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:7841`
- **Static-analysis boundary**: 当前仅在局部变量直接由 `function()`/`funcref()` 和字典字面量创建、且在 `:defer` 前未被改写时报告；参数、函数返回值、成员访问及其他动态 Partial 不推断。

### E1301: String, Number, List, Tuple or Blob required for argument

- **Completeness**: Partial
- **Overview**: Builtin repeat() first argument type is checked for known types.
- **Implementation and tests**: internal/analysis/scopes.go:10083; internal/analysis/diagnostics_test.go:6004; official_compile_cases_e1300_test.go:20
- **Static-analysis boundary**: Unknown/any arguments are not rejected.

### E1304: Cannot use type with this variable

- **Completeness**: Full
- **Overview**: 给 g:/b: 等变量加类型声明
- **Implementation and tests**: `internal/syntax/declarations_test.go:2286`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1306: Loop nesting too deep

- **Completeness**: Full
- **Overview**: Excessive lexical loop nesting is counted and diagnosed.
- **Implementation and tests**: internal/analysis/scopes.go:3725; internal/analysis/diagnostics_test.go:6096; official_compile_cases_e1300_test.go:38
- **Static-analysis boundary**: Direct nesting-boundary test.

### E1307: Argument : Trying to modify a const

- **Completeness**: Partial
- **Overview**: Mutation of known const passed to mutating builtin is diagnosed.
- **Implementation and tests**: internal/analysis/scopes.go:9878; internal/analysis/diagnostics_test.go:6158; official_compile_cases_e1300_test.go:73
- **Static-analysis boundary**: Only modeled mutating builtins; unmodeled runtime calls cannot be proven.

### E1314: Class name must start with an uppercase letter

- **Completeness**: Full
- **Overview**: Class name must begin uppercase.
- **Implementation and tests**: internal/syntax/declarations.go:165; internal/syntax/declarations_test.go:1551; Vim runtime/doc/vim9class.txt
- **Static-analysis boundary**: Direct declaration parser test.

### E1315: White space required after name

- **Completeness**: Full
- **Overview**: Whitespace is required after class/object declaration name.
- **Implementation and tests**: internal/syntax/declarations.go:180; internal/syntax/declarations_test.go:472; Vim runtime/doc/vim9class.txt
- **Static-analysis boundary**: Direct declaration parser test.

### E1316: Class can only be defined in Vim9 script

- **Completeness**: Full
- **Overview**: Class declaration outside Vim9 script is rejected.
- **Implementation and tests**: internal/syntax/declarations.go:139; internal/syntax/declarations_test.go:145; Vim runtime/doc/vim9class.txt
- **Static-analysis boundary**: Direct dialect-context test.

### E1317: Invalid object variable declaration

- **Completeness**: Full
- **Overview**: Invalid object-variable declaration is parsed and diagnosed.
- **Implementation and tests**: internal/syntax/scanner.go:4782; internal/syntax/declarations_test.go:1197; Vim runtime/doc/vim9class.txt
- **Static-analysis boundary**: Direct declaration parser test.

### E1318: Not a valid command in a class

- **Completeness**: Full
- **Overview**: Invalid command inside class is rejected.
- **Implementation and tests**: internal/syntax/blocks.go:837; internal/syntax/blocks_test.go:666; Vim runtime/doc/vim9class.txt
- **Static-analysis boundary**: Direct class-block test.

### E1320: Using an Object as a Number

- **Completeness**: Partial
- **Overview**: 对象当数字用
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:7807`
- **Static-analysis boundary**: 当前覆盖 Vim9 脚本级条件判断、一元与二元数值运算、数值复合赋值中的已知对象；`def` 中保留编译期类型错误，来自 `any`、容器成员或未知返回值时不报告。

### E1324: Using an Object as a String

- **Completeness**: Partial
- **Overview**: 对象当字符串用
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:7878`
- **Static-analysis boundary**: 当前覆盖 `.`/`..` 拼接与字符串 `.=`/`..=` 的已知对象操作数；来自 `any`、容器成员或未知返回值时不报告，其他运行时字符串化入口暂不推断。

### E1325: Method not found in class

- **Completeness**: Partial
- **Overview**: Missing method is diagnosed for statically resolved class/object receiver.
- **Implementation and tests**: internal/analysis/scopes.go:3952; internal/analysis/diagnostics_test.go:6251; Vim runtime/doc/vim9class.txt
- **Static-analysis boundary**: Unknown/any receiver/class members are not resolved; no official compile-case fixture.

### E1326: Variable not found in object

- **Completeness**: Partial
- **Overview**: Missing object variable is diagnosed for statically resolved class/object receiver.
- **Implementation and tests**: internal/analysis/scopes.go:3989; internal/analysis/diagnostics_test.go:6307; Vim runtime/doc/vim9class.txt
- **Static-analysis boundary**: Unknown/any receiver/class members are not resolved; no official compile-case fixture.

### E1328: Constructor default value must be v:none

- **Completeness**: Full
- **Overview**: Constructor default must be v:none.
- **Implementation and tests**: internal/analysis/scopes.go:1023; internal/analysis/diagnostics_test.go:6352; Vim runtime/doc/vim9class.txt
- **Static-analysis boundary**: Direct constructor-default test.

### E1329: Invalid class variable declaration

- **Completeness**: Full
- **Overview**: Invalid class-variable declaration is parsed and diagnosed.
- **Implementation and tests**: internal/syntax/scanner.go:4787; internal/syntax/declarations_test.go:1200; Vim runtime/doc/vim9class.txt
- **Static-analysis boundary**: Direct declaration parser test.

### E1330: Invalid type used in variable declaration

- **Completeness**: Full
- **Overview**: void is invalid in value-bearing variable declarations.
- **Implementation and tests**: internal/analysis/scopes.go:1043; internal/analysis/diagnostics_test.go:6407; official_compile_cases_e1300_test.go:205
- **Static-analysis boundary**: Direct local and official compile-case coverage.

### E1331: public must be followed by "var" or "static" or "final" or "const"

- **Completeness**: Full
- **Overview**: class public modifier must precede an allowed member form
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:1240; Vim src/errors.h
- **Static-analysis boundary**: None found; direct parser rule and assertion.

### E1332: public variable name cannot start with underscore

- **Completeness**: Partial
- **Overview**: public class fields beginning underscore are rejected
- **Implementation and tests**: internal/analysis/scopes.go:1821; internal/analysis/diagnostics_test.go:8199; Vim src/errors.h
- **Static-analysis boundary**: Single-document class analysis only; no runtime/autoload cross-script class resolution evidence.

### E1333: Cannot access protected variable in class

- **Completeness**: Partial
- **Overview**: protected-member access is rejected outside its class
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:8278; Vim src/errors.h
- **Static-analysis boundary**: Static local resolver cannot prove dynamic object/class values across scripts.

### E1335: Variable in class is not writable

- **Completeness**: Partial
- **Overview**: assignment to non-writable class member is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:8328; Vim src/errors.h
- **Static-analysis boundary**: Limited to statically resolved members.

### E1337: Class variable not found in class

- **Completeness**: Partial
- **Overview**: class member used without required object access is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:8385; Vim src/errors.h
- **Static-analysis boundary**: Limited to statically resolved class/object expressions.

### E1340: Argument already declared in the class

- **Completeness**: Partial
- **Overview**: duplicate class constructor argument is rejected
- **Implementation and tests**: internal/analysis/scopes.go:1458; internal/analysis/diagnostics_test.go:8448; Vim src/errors.h
- **Static-analysis boundary**: Class member aggregation is document-local.

### E1341: Variable already declared in the class

- **Completeness**: Partial
- **Overview**: duplicate class variable is rejected
- **Implementation and tests**: internal/analysis/scopes.go:1561; internal/analysis/diagnostics_test.go:8511; Vim src/errors.h
- **Static-analysis boundary**: Class member aggregation is document-local.

### E1342: Interface can only be defined in Vim9 script

- **Completeness**: Full
- **Overview**: interface declaration outside Vim9 script is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:150; Vim test_vim9_interface.vim
- **Static-analysis boundary**: Direct dialect parser rule and assertion.

### E1343: Interface name must start with an uppercase letter

- **Completeness**: Full
- **Overview**: lowercase interface name is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:1595; Vim src/errors.h
- **Static-analysis boundary**: Direct declaration parser rule and assertion.

### E1344: Cannot initialize a variable in an interface

- **Completeness**: Full
- **Overview**: initialized interface variable is rejected
- **Implementation and tests**: internal/syntax/scanner.go:4998; internal/syntax/declarations_test.go:1286; Vim test_vim9_interface.vim
- **Static-analysis boundary**: Direct parser rule and assertion.

### E1345: Not a valid command in an interface

- **Completeness**: Full
- **Overview**: invalid command in interface body is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:1334; Vim test_vim9_interface.vim
- **Static-analysis boundary**: Direct interface-body parser rule and assertion.

### E1346: Interface name not found

- **Completeness**: Partial
- **Overview**: unknown interface in implements list is rejected
- **Implementation and tests**: internal/analysis/scopes.go:790; internal/analysis/diagnostics_test.go:8575; Vim src/errors.h
- **Static-analysis boundary**: Only interfaces indexed in the analyzed document are resolved.

### E1347: Not a valid interface

- **Completeness**: Partial
- **Overview**: non-interface name in implements list is rejected
- **Implementation and tests**: internal/analysis/scopes.go:775; internal/analysis/diagnostics_test.go:8629; Vim src/errors.h
- **Static-analysis boundary**: Only declarations indexed in the analyzed document are resolved.

### E1348: Variable of interface is not implemented

- **Completeness**: Full
- **Overview**: Variable "
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:8715`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1349: Method of interface is not implemented

- **Completeness**: Full
- **Overview**: Method "
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:8801`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1350: Duplicate "implements"

- **Completeness**: Full
- **Overview**: Duplicate "implements"
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:8877`; `internal/syntax/declarations_test.go:383`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1351: Duplicate interface after "implements"

- **Completeness**: Full
- **Overview**: Duplicate interface after "implements":
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:8890`; `internal/syntax/declarations_test.go:521`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1352: Duplicate "extends"

- **Completeness**: Full
- **Overview**: Duplicate "extends"
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:8903`; `internal/syntax/declarations_test.go:440`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1353: Class name not found

- **Completeness**: Full
- **Overview**: Class name not found:
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:8908`; `internal/analysis/official_compile_cases_e1300_test.go:308`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1354: Cannot extend

- **Completeness**: Full
- **Overview**: Cannot extend
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:8908`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1355: Duplicate function

- **Completeness**: Full
- **Overview**: Duplicate function:
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:9314`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1356: "super" must be followed by a dot

- **Completeness**: Full
- **Overview**: "super" must be followed by a dot
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:9407`; `internal/syntax/official_parser_cases_test.go:438`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1357: Using "super" not in a class method

- **Completeness**: Full
- **Overview**: Using "super" not in a class method
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:9487`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1358: Using "super" not in a child class

- **Completeness**: Full
- **Overview**: Using "super" not in a child class
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:9571`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1359: Cannot define a "new" method in an abstract class

- **Completeness**: Full
- **Overview**: Cannot define a "new" method in an abstract class
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:9672`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1360: Using a null object

- **Completeness**: Partial
- **Overview**: Using a null object
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:9792`
- **Static-analysis boundary**: 不能仅凭 Object 静态类型判定，因为该类型不能说明实际对象指针是否为 null；值可能来自参数、分支或函数返回值。只有 `null_object` 常量，或所有到达路径都能证明值必然为 null 时才能报告。

### E1363: Incomplete type

- **Completeness**: Full
- **Overview**: Incomplete type
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:9862`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1365: Cannot use a return type with the "new" method

- **Completeness**: Full
- **Overview**: new method with return type is rejected
- **Implementation and tests**: internal/syntax/scanner.go:3038; internal/syntax/declarations_test.go:2043; Vim test_vim9_class.vim
- **Static-analysis boundary**: Direct class-member parser rule and assertion.

### E1366: Cannot access protected method

- **Completeness**: Partial
- **Overview**: protected method access is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:6254; Vim src/errors.h
- **Static-analysis boundary**: Only statically resolved method receivers are checked.

### E1367: Access level of variable of interface is different

- **Completeness**: Partial
- **Overview**: class/interface variable access mismatch is rejected
- **Implementation and tests**: internal/analysis/scopes.go:2826; internal/analysis/diagnostics_test.go:8741; Vim src/errors.h
- **Static-analysis boundary**: Interface implementation analysis is document-local.

### E1368: Static must be followed by "var" or "def" or "final" or "const"

- **Completeness**: Full
- **Overview**: static modifier must be followed by an allowed member form
- **Implementation and tests**: internal/syntax/scanner.go:4852; internal/syntax/declarations_test.go:1249; Vim test_vim9_class.vim
- **Static-analysis boundary**: Direct parser rule and assertion.

### E1369: Duplicate variable

- **Completeness**: Partial
- **Overview**: duplicate class member is rejected
- **Implementation and tests**: internal/analysis/scopes.go:2251; internal/analysis/diagnostics_test.go:9961; internal/server/diagnostics_test.go:686
- **Static-analysis boundary**: Server rewrites some E1406 output to E1369; cross-file classes are not indexed.

### E1370: Cannot define a "new" method as static

- **Completeness**: Full
- **Overview**: static new method is rejected
- **Implementation and tests**: internal/syntax/scanner.go:4968; internal/syntax/declarations_test.go:1998; Vim src/errors.h
- **Static-analysis boundary**: Direct parser rule and assertion.

### E1371: Abstract must be followed by "def"

- **Completeness**: Full
- **Overview**: abstract modifier requires def
- **Implementation and tests**: internal/syntax/scanner.go:4845; internal/syntax/declarations_test.go:1939; Vim test_vim9_class.vim
- **Static-analysis boundary**: Direct parser rule and assertion.

### E1372: Abstract method cannot be defined in a concrete class

- **Completeness**: Full
- **Overview**: abstract method in concrete class is rejected
- **Implementation and tests**: internal/syntax/scanner.go:4894; internal/syntax/declarations_test.go:1936; Vim src/errors.h
- **Static-analysis boundary**: Direct parser rule and assertion.

### E1373: Abstract method is not implemented

- **Completeness**: Partial
- **Overview**: unimplemented abstract inherited method is rejected
- **Implementation and tests**: internal/analysis/scopes.go:2607; internal/analysis/diagnostics_test.go:9369; Vim src/errors.h
- **Static-analysis boundary**: Inheritance/implementation resolution is document-local and static.

### E1374: Class variable accessible only inside class

- **Completeness**: Partial
- **Overview**: class member conflicts with inherited member
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:12323; Vim src/errors.h
- **Static-analysis boundary**: Inheritance resolution is document-local.

### E1375: Class variable accessible only using class

- **Completeness**: Partial
- **Overview**: class member conflicts with inherited member type
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:12287; Vim src/errors.h
- **Static-analysis boundary**: Inheritance resolution is document-local.

### E1376: Object variable accessible only using class object

- **Completeness**: Partial
- **Overview**: object variable accessed as class member is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:8401; Vim src/errors.h
- **Static-analysis boundary**: Only statically resolved expressions are classified.

### E1377: Access level of method is different in class

- **Completeness**: Partial
- **Overview**: inherited method access-level mismatch is rejected
- **Implementation and tests**: internal/analysis/scopes.go:2653; internal/analysis/diagnostics_test.go:9369; Vim src/errors.h
- **Static-analysis boundary**: Inheritance resolution is document-local.

### E1378: Static member not supported in an interface

- **Completeness**: Full
- **Overview**: static member in interface is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:1814; Vim src/errors.h
- **Static-analysis boundary**: Direct interface parser rule and assertion.

### E1379: Protected variable not supported in an interface

- **Completeness**: Full
- **Overview**: protected variable in interface is rejected
- **Implementation and tests**: internal/syntax/scanner.go:4989; internal/syntax/declarations_test.go:1783; Vim src/errors.h
- **Static-analysis boundary**: Direct interface parser rule and assertion.

### E1380: Protected method not supported in an interface

- **Completeness**: Full
- **Overview**: protected method in interface is rejected
- **Implementation and tests**: internal/syntax/scanner.go:3002; internal/syntax/declarations_test.go:1761; Vim src/errors.h
- **Static-analysis boundary**: Direct interface parser rule and assertion.

### E1381: Interface cannot use "implements"

- **Completeness**: Full
- **Overview**: interface implements clause is rejected
- **Implementation and tests**: internal/syntax/declarations_test.go:422; Vim src/errors.h
- **Static-analysis boundary**: Direct parser assertion; implementation is syntax-level.

### E1382: Variable : type mismatch, expected but got

- **Completeness**: Partial
- **Overview**: interface variable type mismatch is rejected
- **Implementation and tests**: internal/analysis/scopes.go:3021; internal/analysis/diagnostics_test.go:8740; Vim src/errors.h
- **Static-analysis boundary**: Only inferred static types and document-local interfaces are checked.

### E1383: Method : type mismatch, expected but got

- **Completeness**: Partial
- **Overview**: interface method signature mismatch is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:8742; Vim src/errors.h
- **Static-analysis boundary**: Only document-local static signatures are compared.

### E1384: Class method accessible only inside class

- **Completeness**: Partial
- **Overview**: class method conflicts with interface method
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:11953; Vim src/errors.h
- **Static-analysis boundary**: Only document-local static method sets are compared.

### E1385: Class method accessible only using class

- **Completeness**: Partial
- **Overview**: class method/access form mismatch is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:12028; Vim src/errors.h
- **Static-analysis boundary**: Only statically resolved class methods are checked.

### E1386: Object method accessible only using class object

- **Completeness**: Partial
- **Overview**: object method used as class method is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:8402; Vim src/errors.h
- **Static-analysis boundary**: Only statically resolved expressions are classified.

### E1387: public variable not supported in an interface

- **Completeness**: Full
- **Overview**: public variable in interface is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:1737; internal/analysis/diagnostics_test.go:8203
- **Static-analysis boundary**: Direct interface parser rule and assertion.

### E1388: public keyword not supported for a method

- **Completeness**: Full
- **Overview**: public modifier on method is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/blocks_test.go:641; Vim src/errors.h
- **Static-analysis boundary**: Direct parser rule and assertion.

### E1389: Missing name after implements

- **Completeness**: Full
- **Overview**: missing interface name after implements is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:365; Vim test_vim9_interface.vim
- **Static-analysis boundary**: Direct parser rule and assertion.

### E1390: Cannot use an object variable "this." except with the "new" method

- **Completeness**: Full
- **Overview**: object variable use outside new method is rejected
- **Implementation and tests**: internal/syntax/scanner.go:3010; internal/syntax/signature_test.go:1170; Vim src/errors.h
- **Static-analysis boundary**: Direct parser rule and assertion.

### E1391: Cannot (un)lock variable in class

- **Completeness**: Full
- **Overview**: 对类成员加锁/解锁
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:12197`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1392: Cannot (un)lock class variable in class

- **Completeness**: Full
- **Overview**: 对类静态变量加锁/解锁
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:12142`
- **Static-analysis boundary**: No material static-analysis boundary recorded.

### E1393: Type can only be defined in Vim9 script

- **Completeness**: Full
- **Overview**: type alias outside Vim9 script is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:892; Vim src/errors.h
- **Static-analysis boundary**: Direct dialect parser rule and assertion.

### E1394: Type name must start with an uppercase letter

- **Completeness**: Full
- **Overview**: lowercase type alias name is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:904; Vim test_vim9_typealias.vim
- **Static-analysis boundary**: Direct parser rule and assertion.

### E1396: Type alias already exists

- **Completeness**: Partial
- **Overview**: duplicate type alias is rejected
- **Implementation and tests**: internal/analysis/scopes.go:1689; internal/analysis/diagnostics_test.go:10234; Vim src/errors.h
- **Static-analysis boundary**: Only type aliases indexed in current document are considered.

### E1397: Missing type alias name

- **Completeness**: Full
- **Overview**: missing type alias name is rejected
- **Implementation and tests**: internal/syntax/scanner.go:2786; internal/syntax/declarations_test.go:924; Vim test_vim9_typealias.vim
- **Static-analysis boundary**: Direct parser recovery rule and assertion.

### E1398: Missing type alias type

- **Completeness**: Full
- **Overview**: missing type alias right-hand type is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:925; Vim test_vim9_typealias.vim
- **Static-analysis boundary**: Direct parser recovery rule and assertion.

### E1399: Type can only be used in a script

- **Completeness**: Partial
- **Overview**: type alias use outside script context is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:10198; Vim src/errors.h
- **Static-analysis boundary**: Depends on analyzer's document context; no multi-script loading model.

### E1403: Type alias cannot be used as a value

- **Completeness**: Partial
- **Overview**: type alias used as value is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:2903; Vim src/errors.h
- **Static-analysis boundary**: Only names resolved by the current document's type environment are checked.

### E1404: Abstract cannot be used in an interface

- **Completeness**: Full
- **Overview**: abstract modifier in interface is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:1903; Vim src/errors.h
- **Static-analysis boundary**: Direct parser rule and assertion.

### E1405: Class cannot be used as a value

- **Completeness**: Partial
- **Overview**: class name used as value is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:2902; Vim src/errors.h
- **Static-analysis boundary**: Only classes resolved in current document are checked.

### E1406: Public and protected member have the same name: and _

- **Completeness**: Partial
- **Overview**: public/protected member names that collide are rejected
- **Implementation and tests**: internal/analysis/scopes.go:2307; internal/analysis/diagnostics_test.go:8175; internal/server/diagnostics_test.go:681
- **Static-analysis boundary**: Server intentionally rewrites some output to E1369; no cross-file aggregation.

### E1407: Cannot use a Typealias as a variable or value

- **Completeness**: Partial
- **Overview**: type alias used as variable/value is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:2904; Vim src/errors.h
- **Static-analysis boundary**: Only current document type resolution is covered.

### E1408: Final variable not supported in an interface

- **Completeness**: Full
- **Overview**: final variable in interface is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:1869; Vim src/errors.h
- **Static-analysis boundary**: Direct interface parser rule and assertion.

### E1409: Cannot change read-only variable in class

- **Completeness**: Partial
- **Overview**: assignment to public final class member is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:8081; Vim src/errors.h
- **Static-analysis boundary**: Only statically resolved members are checked.

### E1410: Const variable not supported in an interface

- **Completeness**: Full
- **Overview**: const variable in interface is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:1840; Vim src/errors.h
- **Static-analysis boundary**: Direct interface parser rule and assertion.

### E1411: Missing dot after object

- **Completeness**: Partial
- **Overview**: missing dot after object use is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:421; Vim src/errors.h
- **Static-analysis boundary**: Dependent on static object-name resolution.

### E1414: Enum can only be defined in Vim9 script

- **Completeness**: Full
- **Overview**: enum declaration outside Vim9 script is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:155; Vim test_vim9_enum.vim
- **Static-analysis boundary**: Direct dialect parser rule and assertion.

### E1415: Enum name must start with an uppercase letter

- **Completeness**: Full
- **Overview**: lowercase enum name is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:1684; Vim test_vim9_enum.vim
- **Static-analysis boundary**: Direct parser rule and assertion.

### E1416: Enum cannot extend a class or enum

- **Completeness**: Full
- **Overview**: enum extends clause is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:484; Vim src/errors.h
- **Static-analysis boundary**: Direct parser rule and assertion.

### E1417: Abstract cannot be used in an Enum

- **Completeness**: Full
- **Overview**: abstract modifier in enum is rejected
- **Implementation and tests**: internal/syntax/scanner.go:5024; internal/syntax/declarations_test.go:1475; Vim src/errors.h
- **Static-analysis boundary**: Direct parser rule and assertion.

### E1418: Invalid enum value declaration

- **Completeness**: Full
- **Overview**: invalid enum value declaration is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:1453; Vim test_vim9_enum.vim
- **Static-analysis boundary**: Direct parser rule and assertion.

### E1419: Not a valid command in an Enum

- **Completeness**: Full
- **Overview**: invalid command in enum body is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:1419; Vim test_vim9_enum.vim
- **Static-analysis boundary**: Direct enum-body parser rule and assertion.

### E1420: Missing :endenum

- **Completeness**: Full
- **Overview**: missing endenum is reported
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:1389; Vim test_vim9_enum.vim
- **Static-analysis boundary**: Direct parser recovery rule and assertion.

### E1421: Enum cannot be used as a value

- **Completeness**: Partial
- **Overview**: enum type used as value is rejected
- **Implementation and tests**: internal/analysis/scopes.go:2490; internal/analysis/diagnostics_test.go:379; Vim src/errors.h
- **Static-analysis boundary**: Only enums resolved in current document are checked.

### E1422: Enum value not found in enum

- **Completeness**: Partial
- **Overview**: unknown enum member is rejected
- **Implementation and tests**: internal/analysis/scopes.go:2477; internal/analysis/diagnostics_test.go:339; Vim src/errors.h
- **Static-analysis boundary**: Only statically resolved enum receivers are checked.

### E1423: Enum value "." cannot be modified

- **Completeness**: Partial
- **Overview**: assignment to enum name/value is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:273; Vim src/errors.h
- **Static-analysis boundary**: Only statically resolved enum member expressions are checked.

### E1426: Enum ordinal value cannot be modified

- **Completeness**: Partial
- **Overview**: enum ordinal assignment is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:231; Vim src/errors.h
- **Static-analysis boundary**: Only statically resolved enum member expressions are checked.

### E1427: Enum name cannot be modified

- **Completeness**: Partial
- **Overview**: enum name assignment is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:182; Vim src/errors.h
- **Static-analysis boundary**: Only statically resolved enum member expressions are checked.

### E1428: Duplicate enum value

- **Completeness**: Partial
- **Overview**: duplicate enum member is rejected
- **Implementation and tests**: internal/analysis/scopes.go:2330; internal/analysis/diagnostics_test.go:134; Vim src/errors.h
- **Static-analysis boundary**: Enum aggregation is document-local.

### E1429: Class can only be used in a script

- **Completeness**: Full
- **Overview**: class use outside script is rejected
- **Implementation and tests**: internal/syntax/scanner.go:2973; internal/syntax/declarations_test.go:290; Vim test_vim9_class.vim
- **Static-analysis boundary**: Direct parser context rule and assertion.

### E1430: Uninitialized object variable referenced

- **Completeness**: Partial
- **Overview**: 引用未初始化的对象变量（保守：仅构造路径明确时提前报）
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:2784`
- **Static-analysis boundary**: 当前只报告对象成员初始化表达式中立即读取 `this.<同名成员>` 的确定情形；lambda 捕获属于延迟执行，不报告。跨成员顺序、方法调用和构造路径仍保持 `unknown`。

### E1431: Abstract method in class cannot be accessed directly

- **Completeness**: Partial
- **Overview**: unimplemented abstract method is rejected
- **Implementation and tests**: internal/analysis/scopes.go; internal/analysis/diagnostics_test.go:12436; Vim src/errors.h
- **Static-analysis boundary**: Inheritance resolution is document-local.

### E1432: Overriding generic method in class with a concrete method

- **Completeness**: Partial
- **Overview**: generic method override with non-generic method is rejected
- **Implementation and tests**: internal/analysis/scopes.go:2531; internal/analysis/diagnostics_test.go:9369; Vim src/errors.h
- **Static-analysis boundary**: Only current-document parent/method resolution is checked.

### E1433: Overriding concrete method in class with a generic method

- **Completeness**: Partial
- **Overview**: concrete method override with generic method is rejected
- **Implementation and tests**: internal/analysis/scopes.go:2537; internal/analysis/diagnostics_test.go:9369; Vim src/errors.h
- **Static-analysis boundary**: Only current-document parent/method resolution is checked.

### E1434: Mismatched number of type variables for generic method in class

- **Completeness**: Partial
- **Overview**: generic override type-variable count mismatch is rejected
- **Implementation and tests**: internal/analysis/scopes.go:2543; internal/analysis/diagnostics_test.go:9369; Vim src/errors.h
- **Static-analysis boundary**: Only current-document parent/method resolution is checked.

### E1435: Enum can only be used in a script

- **Completeness**: Full
- **Overview**: enum use outside script is rejected
- **Implementation and tests**: internal/syntax/scanner.go; internal/syntax/declarations_test.go:262; Vim test_vim9_enum.vim
- **Static-analysis boundary**: Direct parser context rule and assertion.

### E1436: Interface can only be used in a script

- **Completeness**: Full
- **Overview**: interface use outside script is rejected
- **Implementation and tests**: internal/syntax/scanner.go:2979; internal/syntax/declarations_test.go:234; Vim test_vim9_interface.vim
- **Static-analysis boundary**: Direct parser context rule and assertion.

### E1523: String, List, Tuple or Blob required

- **Completeness**: Partial
- **Overview**: Legacy `:for` 的迭代值不是 String、List、Tuple 或 Blob
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:2820`
- **Static-analysis boundary**: 迭代值来自动态变量、容器成员或未知调用时无法可靠判定；只有类型传播能证明其必然不属于全部允许类型时才报告。Vim9 编译语境对同类错误使用 E1177，不映射为 E1523。

### E1526: Missing end of Tuple ')'

- **Completeness**: Full
- **Overview**: missing closing tuple parenthesis is reported
- **Implementation and tests**: internal/syntax/expression.go:1386; internal/syntax/expression_test.go:580; Vim test_tuple.vim
- **Static-analysis boundary**: Direct expression parser recovery and span assertion.

### E1527: Missing comma in Tuple

- **Completeness**: Full
- **Overview**: missing tuple comma is reported
- **Implementation and tests**: internal/syntax/expression.go:1332; internal/syntax/expression_test.go:575; Vim test_tuple.vim
- **Static-analysis boundary**: Direct expression parser recovery and span assertion.

### E1528: List or Tuple or Blob required for argument

- **Completeness**: Partial
- **Overview**: argument requiring List/Tuple/Blob is type-checked
- **Implementation and tests**: internal/analysis; internal/analysis/diagnostics_test.go:4625; official_compile_cases_e1400_test.go:20
- **Static-analysis boundary**: Only static inferred types; dynamic any values remain unknown.

### E1529: List or Tuple required for argument

- **Completeness**: Partial
- **Overview**: argument requiring List/Tuple is type-checked
- **Implementation and tests**: internal/analysis; internal/analysis/diagnostics_test.go:4639; official_compile_cases_e1400_test.go:30
- **Static-analysis boundary**: Only static inferred types; dynamic any values remain unknown.

### E1530: List or Tuple or Dictionary required for argument

- **Completeness**: Partial
- **Overview**: argument requiring List/Tuple/Dictionary is type-checked
- **Implementation and tests**: internal/analysis; internal/analysis/diagnostics_test.go:4653; official_compile_cases_e1400_test.go:40
- **Static-analysis boundary**: Only static inferred types; dynamic any values remain unknown.

### E1531: Argument of must be a List, Tuple, Dictionary or Blob

- **Completeness**: Partial
- **Overview**: get() collection argument type is checked
- **Implementation and tests**: internal/analysis; internal/analysis/diagnostics_test.go:4667; official_compile_cases_e1400_test.go:58
- **Static-analysis boundary**: Only static inferred types; dynamic any values remain unknown.

### E1532: Cannot modify a tuple

- **Completeness**: Partial
- **Overview**: tuple mutation is rejected
- **Implementation and tests**: internal/analysis; internal/analysis/diagnostics_test.go:2143; official_compile_cases_e1400_test.go:68
- **Static-analysis boundary**: Only statically recognized tuple values are covered.

### E1533: Cannot slice a tuple

- **Completeness**: Partial
- **Overview**: tuple slicing is rejected
- **Implementation and tests**: internal/analysis; internal/analysis/diagnostics_test.go:2397; official_compile_cases_e1400_test.go:92
- **Static-analysis boundary**: Only statically recognized tuple values are covered.

### E1535: List or Tuple required

- **Completeness**: Partial
- **Overview**: operation requiring List/Tuple is type-checked
- **Implementation and tests**: internal/analysis; internal/analysis/diagnostics_test.go:2333; official_compile_cases_e1400_test.go:160
- **Static-analysis boundary**: Only static inferred types; dynamic any values remain unknown.

### E1536: Tuple required

- **Completeness**: Partial
- **Overview**: tuple-required assignment/destructuring is checked
- **Implementation and tests**: internal/analysis; internal/analysis/diagnostics_test.go:11505; Vim src/errors.h
- **Static-analysis boundary**: No official compile-case fixture; static inferred types only.

### E1537: Less targets than Tuple items

- **Completeness**: Partial
- **Overview**: too few destructuring targets for tuple is rejected
- **Implementation and tests**: internal/analysis; internal/analysis/diagnostics_test.go:11462; Vim src/errors.h
- **Static-analysis boundary**: No official compile-case fixture; static tuple shape only.

### E1538: More targets than Tuple items

- **Completeness**: Partial
- **Overview**: too many destructuring targets for tuple is rejected
- **Implementation and tests**: internal/analysis; internal/analysis/diagnostics_test.go:11416; Vim src/errors.h
- **Static-analysis boundary**: No official compile-case fixture; static tuple shape only.

### E1539: Variadic tuple must end with a list type

- **Completeness**: Full
- **Overview**: variadic tuple must end in list type
- **Implementation and tests**: internal/syntax/type.go:170; internal/syntax/official_parser_cases_test.go:376; Vim test_tuple.vim
- **Static-analysis boundary**: Direct type parser rule and official-case assertion.

### E1552: Type variable name must start with an uppercase letter

- **Completeness**: Full
- **Overview**: generic type variable must start uppercase
- **Implementation and tests**: internal/syntax/signature.go:512; internal/syntax/signature_test.go:871; Vim test_vim9_generics.vim
- **Static-analysis boundary**: Direct generic parser rule and assertion.

### E1553: Missing comma after type in generic function

- **Completeness**: Full
- **Overview**: missing generic type comma is reported
- **Implementation and tests**: internal/syntax/expression.go:587; internal/syntax/expression_test.go:2312; Vim test_vim9_generics.vim
- **Static-analysis boundary**: Direct generic parser recovery and assertion.

### E1554: Missing '>' in generic function

- **Completeness**: Full
- **Overview**: missing generic closing bracket is reported
- **Implementation and tests**: internal/syntax/expression.go:625; internal/syntax/expression_test.go:2335; Vim test_vim9_generics.vim
- **Static-analysis boundary**: Direct generic parser recovery and assertion.

### E1555: Empty type list specified for generic function

- **Completeness**: Full
- **Overview**: empty generic type list is rejected
- **Implementation and tests**: internal/syntax/expression.go:703; internal/syntax/expression_test.go:2304; Vim test_vim9_generics.vim
- **Static-analysis boundary**: Direct generic parser rule and assertion.

### E1556: Too many types specified for generic function

- **Completeness**: Partial
- **Overview**: too many generic function type arguments is rejected
- **Implementation and tests**: internal/analysis; internal/analysis/diagnostics_test.go:11022; Vim src/errors.h
- **Static-analysis boundary**: Only in-file statically resolved generic functions are counted.

### E1557: Not enough types specified for generic function

- **Completeness**: Partial
- **Overview**: not enough generic function type arguments is rejected
- **Implementation and tests**: internal/analysis; internal/analysis/diagnostics_test.go:10983; Vim src/errors.h
- **Static-analysis boundary**: Only in-file statically resolved generic functions are counted.

### E1558: Unknown generic function

- **Completeness**: Partial
- **Overview**: 使用类型实参调用当前文件中不存在的泛型函数
- **Implementation and tests**: `internal/analysis/diagnostics_test.go:10896`
- **Static-analysis boundary**: 只做当前文件内验证。解析器保留泛型函数声明的类型参数和调用的类型实参；普通未限定函数名在文件内无法解析时报告 E1558，解析到确定的非泛型函数时继续由 E1560 报告。`g:`、autoload 名称、成员调用、builtin、动态字符串和不完整类型实参不报告 E1558，不查询 workspace 或运行时函数表。

### E1559: Type arguments missing for generic function

- **Completeness**: Partial
- **Overview**: missing generic type arguments is rejected
- **Implementation and tests**: internal/analysis; internal/analysis/diagnostics_test.go:10944; Vim src/errors.h
- **Static-analysis boundary**: Only in-file statically resolved generic functions are checked.

### E1560: Not a generic function

- **Completeness**: Partial
- **Overview**: type arguments on non-generic function are rejected
- **Implementation and tests**: internal/analysis; internal/analysis/diagnostics_test.go:10859; Vim help vim9.txt
- **Static-analysis boundary**: Explicitly scoped to file-local generic/regular functions; no dynamic global lookup.

### E1561: Duplicate type variable name

- **Completeness**: Full
- **Overview**: duplicate generic type variable is rejected
- **Implementation and tests**: internal/syntax/signature.go:476; internal/syntax/signature_test.go:875; Vim test_vim9_generics.vim
- **Static-analysis boundary**: Direct generic parser rule and assertion.

### E1579: completeopt=escape cannot be used with -nargs=_

- **Completeness**: Full
- **Overview**: command -completeopt=escape with -nargs=_ is rejected
- **Implementation and tests**: internal/syntax/scanner.go:4062; internal/syntax/command_body_test.go:332; Vim src/errors.h
- **Static-analysis boundary**: Direct command parser rule and assertion.

## Explicitly unsupported error codes

These codes are not emitted as document diagnostics. The trigger describes how Vim reaches the error; the reason explains why source-only LSP analysis cannot prove that runtime state.

### E120: SID outside script context

- **Completeness**: Unsupported
- **Vim trigger**: <SID> 函数在脚本上下文之外使用
- **Why it is unsupported**: 不能仅靠扫描 Vim 内置定义、`runtimepath` 和工作区文件完整判定。`<SID>` 是否有效取决于命令或函数的实际调用上下文；磁盘脚本内的直接引用通常具有 script context，但命令行、动态命令及运行时构造的函数名可能没有。只有在调用入口可确定为非脚本上下文时才能保守报告。
- **Evidence**: 未在 `src/testdir` 中以该错误码形式直接断言（可能通过变量间接触发，或为编译器内部路径）

### E123: Undefined function

- **Completeness**: Unsupported
- **Vim trigger**: :function 路径下函数名解析失败
- **Why it is unsupported**: 不能完整静态判定。`:delfunction` 等路径查询的是执行时函数表，目标函数可能尚未加载、已被删除，或由 `execute()`、autocmd、autoload 等机制动态创建。只有目标和完整执行顺序均可证明，且不存在未知函数表写入时才能报告。
- **Evidence**: `src/testdir/test_vim9_func.vim:213`; `src/testdir/test_vim9_script.vim:4350`; `src/testdir/test_user_func.vim:261`; additional Vim tests listed in the source ledger

### E1063: Type mismatch for v: variable

- **Completeness**: Unsupported
- **Vim trigger**: 给 v: 变量赋错误类型
- **Why it is unsupported**: 不能仅凭 `v:` 变量定义完整判定。触发与否取决于赋入值的实际类型，右侧可能来自 `any`、Legacy 动态值或未知调用。只有值的类型可证明且必然不符合目标 `v:` 变量约束时才能报告。
- **Evidence**: 未在 `src/testdir` 中以该错误码形式直接断言（可能通过变量间接触发，或为编译器内部路径）

### E1091: Function is not compiled

- **Completeness**: Unsupported
- **Vim trigger**: 引用未编译成功的函数
- **Why it is unsupported**: 不适合作为普通源码静态诊断。此错误检查执行引擎中函数是否已经成功编译的实际状态，该状态受先前编译失败、重定义、清理和执行顺序影响；静态分析器应报告造成编译失败的原始错误，而不是推测后续会出现 E1091。
- **Evidence**: `src/testdir/test_vim9_disassemble.vim:40`; `src/testdir/test_vim9_func.vim:2731`; `src/testdir/test_vim9_func.vim:4338`; additional Vim tests listed in the source ledger

### E1102: Lambda function not found

- **Completeness**: Unsupported
- **Vim trigger**: 执行 `NEWFUNC` 时找不到编译期生成的隐藏 `<lambda>N` 函数模板
- **Why it is unsupported**: 不适合作为普通源码静态诊断。Vim 编译函数内的全局函数定义时生成隐藏 `<lambda>N` 模板和 `NEWFUNC` 指令，执行时再从运行时函数表查找并复制该模板；E1102 只在内部模板已经消失时触发，依赖模板创建、释放、脚本重载和函数表状态。删除 Vim9 script-local 函数不会触发 E1102，而是 E1084。
- **Evidence**: 未在 `src/testdir` 中以该错误码形式直接断言（可能通过变量间接触发，或为编译器内部路径）

### E1274: No script file name to substitute for "script"

- **Completeness**: Unsupported
- **Vim trigger**: `expand('<script>')` 在无脚本文件名的上下文中展开。
- **Why it is unsupported**: 不能仅靠文件内容完整判定。`<script>` 替换取决于当前 sourcing/script context；同一段文本通过文件 source、命令行或动态执行进入 Vim 时可能得到不同结果。只有调用入口可明确证明没有脚本文件名时才能报告。
- **Evidence**: `src/testdir/test_expand.vim:153`

### E1327: Object required, found

- **Completeness**: Unsupported
- **Vim trigger**: 需要对象但类型不符
- **Why it is unsupported**: 不能仅靠目标位置要求 Object 来判定，仍需知道传入值的实际类型。若值来自 `any`、Legacy 动态值或未知函数返回值，完整索引也不能证明不匹配；只有实际类型可证明且必然不是 Object 时才能报告。
- **Evidence**: 未在 `src/testdir` 中以该错误码形式直接断言（可能通过变量间接触发，或为编译器内部路径）
