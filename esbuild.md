# 从 esbuild 提炼的 vimls-go 解析器优化路线

本文不是 esbuild 架构的泛化摘要，而是 vimls-go 的解析性能设计记录。结论来自
本地源码 `/Users/chemzqm/lib/esbuild`，固定在提交
`f6058f8364fe7ab91ca57a83e02577ed74c9cae4`。所有源码行号均相对于这个提交。
下文的 `esbuild/` 前缀表示这个 checkout 根目录，不是 vimls-go 中的同名路径。

研究目标只有三个：先保证 legacy Vim script 和 Vim9 script 的语法与 loose
recovery 正确，再降低单文件解析的分配和重复工作，最后用有界并行和可复现缓存
扩展到工作区。不能为了模仿 esbuild 而改变 Vim 的语言行为。

## 结论

vimls-go 当前最值得做的不是自定义分配器或压缩全部 AST，而是依次完成：

1. 保持 legacy 与 Vim9 两套独立的命令语法和恢复规则。
2. 已让命令边界解析得到的表达式 AST 被 details 阶段直接复用，消除了目标命令中
   同一表达式的第二次 lex/parse。
3. 已将单条逻辑表达式的 lexer 从“先生成完整 token slice”改为“parser 按需推进一个
   token”，只在真实歧义点复制很小的 cursor 状态。
4. 正在逐批让 dialect-specific command consumer 一次完成 argument boundary、AST 和
   diagnostics；expression-list、声明、循环、Vim9 command-start、`put =expr` 以及
   `global`/`vglobal`、`folddoopen`/`folddoclosed` 的嵌套 Ex payload 已迁移到准确的
   command-owned boundary/details 路径。`filter[!]` modifier 也已按 Vim 的 regexp
   boundary 保留细节，并在坏 pattern 后按行恢复。
5. 继续使用至多 `min(GOMAXPROCS, 4)` 个稳定后台 worker；文件内无共享可变状态，
   文件间结果写入预分配槽位，最后按输入顺序合并。
6. 只有新的 profile 证明它们仍是主要成本时，才改 logical span 映射、AST 布局或
   后续语义 pass。

逐行恢复是不可让步的约束：合法 continuation 先组成逻辑语句；一旦当前语句确认
有语法错误，当前 command 消费剩余范围，不再把其余 `|` 猜成新命令；下一条独立
物理行重新正常解析。剩余文本可能仍属于原 argument span，并不要求所有错误路径都
生成 `TokenOpaque`。esbuild 的整文件 hard-error 中止不适用于编辑器。

VimL 通常不会像 JavaScript 一样被压缩成一条超长物理行，因此“适合 esbuild”本身
不是优化依据。vimls-go 的同步和性能单位仍是物理行，只有 Vim 认可的 continuation
才组成一条逻辑语句。cursor 只存在于这个短生命周期范围内，用来避免为每个短表达式
分配完整 token slice；不建设全文件 token stream、复杂 lexer mode 或 JS 式广泛回溯。

## esbuild 实际采用的设计

### 1. 独立、按需推进的 lexer

esbuild 的 lexer 与 parser 是两个独立类型，并不是把 lexer 代码内联进 parser。
关键在于 parser 调用 `Lexer.Next()` 按需取得当前 token，而不是先把整个文件转换成
`[]Token`：

- `esbuild/internal/js_lexer/js_lexer.go:3-14` 明确说明 lexer 不会先运行到文件末尾。
- `esbuild/internal/js_lexer/js_lexer.go:241-293` 的 `Lexer` 只保存当前 token、源码和游标等
  状态。
- `esbuild/internal/js_lexer/js_lexer.go:999-1075` 展示 `Next()` 直接从源码推进当前 token。
- `esbuild/internal/js_lexer/js_lexer.go:374-392` 直接切原始源码，并延迟字符串 escape 解码。

这不等于“没有 token”。esbuild 有明确的 token enum 和当前 token，只是没有为普通
解析建立全文件 token 数组。对 vimls-go 的直接启示是消除
`lexExpression(...) []expressionToken`，而不是把命令 scanner 和表达式 parser 混成
一个巨大函数。

### 2. 源码切片、紧凑引用和按需值转换

`MaybeSubstring` 在标识符未转义的常见路径上保存源码子串的位置和长度；只有转义
标识符等少见情况才保存额外分配的字符串：

- `esbuild/internal/js_lexer/js_lexer.go:214-239`
- `esbuild/internal/js_parser/js_parser.go:1896-1935`

esbuild 不是普遍使用 `[]byte` 零拷贝。源码主体是 Go `string`，标识符常见路径引用
它，字符串字面量则按语义需要延迟解码为 UTF-16。vimls-go 应照搬的是“原始 byte
span 是事实来源、语义值按需解码”，不是某一种容器类型。

### 3. 少量必要 pass，而不是追求一个 pass

esbuild 的 JavaScript parser 有两个主要 pass：

1. 按需 lex、parse、建立 scope 并声明 symbol。
2. 绑定 symbol，同时做常量折叠、lowering 和部分 mangling。

源码证据见 `esbuild/internal/js_parser/js_parser.go:125-153`、`17815-17855`、
`18023-18129`。`scopesInOrder` 让两个 pass 复用 scope 顺序，而不用把 scope 数据挂到
每个 AST 节点上。

因此“pass 越少越好”不是规则。正确规则是：如果语义依赖尚未满足，就保留独立
pass；如果一次遍历已经拥有某项结果，就不要丢弃后再重新扫描源码或 AST。

### 4. AST 并不简单等于“扁平结构体”

esbuild 的表达式和语句仍使用 Go interface variant 加具体节点指针：
`esbuild/internal/js_ast/js_ast.go:446-494`、`935-976`。它的内存收益来自多项具体选择：

- 空值、missing、empty statement 等无状态节点共享 singleton，见
  `esbuild/internal/js_ast/js_ast.go:569-579`。
- identifier 使用 `sourceIndex + innerIndex` 的 `ast.Ref`，各文件可并行产生互不冲突
  的引用，见 `esbuild/internal/ast/ast.go:372-388`。
- symbol 放在文件级连续数组中，跨文件合并为二维数组；profile 证明它比
  `map[Ref]Symbol` 更快，见 `esbuild/internal/ast/ast.go:647-663`。
- import records 等需要集中处理的数据放在 AST side table，避免为一次后处理遍历
  整棵树，见 `esbuild/internal/js_ast/js_ast.go:1553-1600`。
- 对解析时无法决定的 import 表示保留专用 `EImportIdentifier` 节点，推迟到 linker/
  printer 决定，避免额外全树改写，见
  `esbuild/internal/js_ast/js_ast.go:738-765`。

vimls-go 只有在 heap profile 显示 AST 常驻内存已成为主要成本后，才应考虑缩小
`Span`、拆分 `Expression` 的冷字段、共享无状态 missing 节点或建立文件级 side
table。不能仅凭“esbuild AST 紧凑”就提前重写所有节点。

### 5. 歧义只做局部回滚

esbuild 大多数情况下只看一个 token。部分 TypeScript/JSX speculative parse 路径会
复制 lexer 状态、临时关闭 diagnostics，失败时恢复；普通 lookahead 也会保存和恢复
lexer，但不一定关闭 diagnostics：

- `esbuild/internal/js_parser/ts_parser.go:843-892`
- `esbuild/internal/js_parser/ts_parser.go:1052-1072`
- `esbuild/internal/js_parser/ts_parser.go:1236-1246`

可迁移原则是只保存和恢复歧义所需的 scanner/lexer 状态，而不是复制完整 parser 或
建立两份 token stream。esbuild 自身复制的 `Lexer` struct 并不小，不能把这种实现
形式当成目标。legacy/Vim9 的 command-name 与 expression-name 歧义也必须先由 Vim
源码证明，不能把 speculative parse 当作普通控制流。

### 6. 文件私有并行、单 owner 汇总

esbuild 的 scan phase 会为发现的文件启动 goroutine，但共享的 `scanner.results`、
`visited` 和剩余计数只由一个协调线程修改：

- `esbuild/internal/bundler/bundler.go:1324-1343`
- `esbuild/internal/bundler/bundler.go:1540-1654`
- `esbuild/internal/bundler/bundler.go:2176-2221`
- `esbuild/internal/bundler/bundler.go:2812-2827`

每个 parser 用唯一 source index 和本地 inner index 产生 symbol。并行结果按 source
index 放入槽位；多 entry point 的结果也先写各自槽位，再按 entry point 顺序连接，
见 `esbuild/internal/bundler/bundler.go:3089-3123`。

这不是“全程零锁”。source index cache、AST cache 和 logger 都有 mutex：
`esbuild/internal/cache/cache.go:63-114`、
`esbuild/internal/cache/cache_ast.go:139-189`、
`esbuild/internal/logger/logger.go:799-811`。真正有价值的是锁的职责很窄，昂贵
parse 在锁外完成，最终输出有确定顺序。

esbuild 的 goroutine-per-file 适合其短生命周期构建器，不适合后台常驻 LSP 直接
照搬。vimls-go 必须限制 worker 数、队列、文件数、AST 总内存和取消路径。

### 7. 缓存完整 parse result 和 diagnostics

`JSCache.Parse` 的 key 实际由路径槽位、完整 `Source` 值和 parser options 共同决定。
命中时复用 AST 并重放 diagnostics；未命中时只在查表和写回时持锁，parse 在锁外：
`esbuild/internal/cache/cache_ast.go:13-24`、`139-189`。

vimls-go 可以借鉴完整结果缓存，但不能只以 URI 为 key，也不能缓存随后会被语义
阶段原地修改的 AST。进程内 key 至少需要 snapshot revision/content identity、
dialect trigger 和所有影响 parse 的选项；当前二进制隐式固定 parser code。只有做
持久 cache 时才显式加入 grammar/build identity。用户配置的 target version 只影响
兼容性 diagnostics，不应让纯语法 AST cache miss。

### 8. profile 指导预分配

esbuild 在 lexer 中近似统计 newline，只为 source-map line table 预分配容量。注释
明确说明这是 heap profile 的第一大分配，误差只影响容量不影响正确性：
`esbuild/internal/js_lexer/js_lexer.go:2447-2468`。source index cache 也用历史长度加少量余量
预分配 result array，见 `esbuild/internal/cache/cache.go:82-89` 和
`esbuild/internal/bundler/bundler.go:1411-1421`。

可迁移原则是“先看 profile，再给已知大小的 slice 容量”，而不是到处猜容量。容量
估计永远不能改变语法行为。

## vimls-go 当前基线

### 当前解析流水线

```text
physical lines
  -> legacy/Vim9 logical view
  -> scanCommands: command header + argument boundary
  -> coalesce embedded legacy blocks
  -> buildBlocks
  -> parseLogicalCommandDetails: expression/type/detail AST
  -> normalize lambda source spans
```

关键现状：

- `LegacyParser` 和 `Vim9Parser` 是独立公开入口；command argument consumer 已分为
  `legacy_command.go` 与 `vim9_command.go`。
- 两种语言目前仍共享带 `dialect` 字段的 Pratt expression parser。后续优化不得把
  两套 command grammar 重新合并；共享表达式机制只限于源码证明相同的部分。
- 普通 legacy 物理行直接读原始 source；只有删除 continuation prefix 或插入字符时
  才建立压缩 `logicalSegment` 映射。
- `scanLegacyExpression`/`scanVim9Expression` 为寻找 `|` 调用
  `parseExpressionPrefix`；普通单表达式命令现在把该 AST 和 diagnostics 暂存在
  `Command`，details 阶段直接消费，不再二次 parse。
- production expression parser 已改成 command-argument 范围内的 cursor lexer；
  `lexExpression` 只保留为测试 token collector，不再为每次 parse 建立完整 token slice。
- continuation 路径会建立临时 `File` 并在 details 后递归映射 AST span；旧 profile
  显示这里主要是累计到子解析的成本，mapper 自身不是首要 flat hotspot。
- 后台 analysis 已使用最多 4 个 worker，并以 document revision 拒绝 stale result；
  当前没有跨文件 workspace merge barrier。

### 2026-08-29 本地历史观测

输入是当时用户 runtimepath 中 2911 个 Vim 文件，共 17,971,810 bytes，每个文件只调用
一次 `syntax.Parse`。这些是一次性本地 helper 的 wall-clock/heap 结果，不是生产
server benchmark。helper、输入 manifest/hash、机器信息和 profile 文件均未保留，
因此这些数字只能解释当时的优化方向，不能作为当前可复现基准或未来验收依据。

| 状态 | 单 worker 时间 | TotalAlloc | mallocs | GC |
| --- | ---: | ---: | ---: | ---: |
| logical segment 优化后、autocmd 修复前 | 2.61 s | 1.305 GB | 5.267 M | 402 |
| legacy autocmd 只复制 body 后 | 2.39 s | 1.073 GB | 5.266 M | 392 |

autocmd 修复把 `parseLegacyAutocmdCommandList` 的约 230 MB 整源 flat allocation
移除，TotalAlloc 降约 17.8%，单次 wall time 降约 8%。它仍会为需要归一化的 autocmd
body 建立可变 `[]byte` 并转换回 `string`，是否继续优化必须看新的 profile。

修复前同一输入的 worker 扩展结果为：

| worker | wall time | max RSS |
| ---: | ---: | ---: |
| 1 | 2.607 s | 148.5 MB |
| 2 | 1.482 s | 155.7 MB |
| 4 | 0.951 s | 190.2 MB |

4 workers 相对 1 worker 约 2.74x，RSS 增约 28%。这支持当前默认上限 4，但不能用来
证明任意机器都应固定为 4，也不能把文件发现/I/O 的时间混进纯 parser 结论。

修复前 CPU/heap profile 的主要分配归属：

| 函数 | flat/cumulative | 含义 |
| --- | ---: | --- |
| `scanCommands` | 281.9 MB flat / 500.5 MB cum | command/token slice 与扫描结果 |
| `parseLegacyAutocmdCommandList` | 230.1 MB flat | 已移除的整源复制 |
| `lexExpression` | 218.2 MB flat | 完整 expression token slice |
| `parseExpressionPrefix` | 117 MB flat | parser/token/AST 与重复 prefix parse |
| `parseCommandDetailsDepth` | 443 MB cum | details 下游表达式和嵌套命令成本 |
| `parseLogicalCommandDetails` | 7.5 MB flat / 461.6 MB cum | flat 很小，不应先重写 mapper |

新的优化必须重新采 profile；不能继续按已删除 hotspot 的旧排序工作。

### P2 可复现微基准

`internal/syntax/benchmark_test.go` 现在提供
`BenchmarkParseSingleExpressionCommands`。每个子用例包含 64 组非平凡 `if` 表达式和
`endif`，分别走 legacy 和 Vim9 文件入口。它是专门测 P2 的 parser microbenchmark，
不是 RTP 或 workspace 吞吐基准。

环境：Go 1.26.5、macOS 26.5.2、darwin/amd64、Intel Core i7-9750H，
`GOMAXPROCS=1`。每个状态运行 5 组，每组 `benchtime=30x`，时间取中位数；分配数字在
5 组中完全一致：

```sh
GOMAXPROCS=1 GOPROXY=off GOSUMDB=off \
  go test -mod=readonly ./internal/syntax -run '^$' \
  -bench '^BenchmarkParseSingleExpressionCommands$' \
  -benchmem -benchtime=30x -count=5
```

| dialect | 状态 | ns/op 中位数 | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| legacy | P2 前 | 1,465,703 | 838,024 | 3,878 |
| legacy | P2 后 | 1,052,117 | 523,192 | 2,215 |
| legacy | 变化 | -28.2% | -37.6% | -42.9% |
| legacy | P3 后 | 909,307 | 343,672 | 1,819 |
| legacy | P2 后到 P3 后 | -13.6% | -34.3% | -17.9% |
| Vim9 | P2 前 | 2,122,958 | 1,038,950 | 9,786 |
| Vim9 | P2 后 | 1,755,718 | 724,068 | 8,122 |
| Vim9 | 变化 | -17.3% | -30.3% | -17.0% |
| Vim9 | P3 后 | 1,561,660 | 549,987 | 7,738 |
| Vim9 | P2 后到 P3 后 | -11.1% | -24.0% | -4.7% |

P2 后同一 microbenchmark 的 `alloc_space` profile 中，`lexExpression` 是第一 flat
allocation：36.03 MB，占 26.52%。P3 后该完整 token-slice allocation 已消失；新的
第一项是 `scanCommands`（20.47 MB，26.32%），随后是 AST 构造所在的
`expressionParser.parsePrefix`（15.50 MB，19.94%）和 Vim9 临时 command 扫描
`scanLogicalCommandRange`（11.20 MB，14.40%）。`newExpressionBoundary` 降到 0.50 MB。
因此下一步回到 P4 的逐行 dialect command consumer，而不是继续扩建 lexer 或提前改
AST layout。profile 混合了两个 dialect 且输入是定向样例，只决定相邻阶段顺序，不代表
真实 RTP 的最终占比。

`BenchmarkParseExpressionLineShapes` 另测 VimL 自己的输入形态：普通短行、合法 legacy/
Vim9 continuation、错误行，以及防止 pathological 回退的嵌套 curly name。64 到 256
条 continuation 的输入约放大 4 倍，时间和 AST 分配也约按 4 倍增长；这只是复杂度
guard，不把人工超长表达式当成主要优化目标。嵌套 leading-curly 的既有歧义判断仍会
重复扫描局部 token/source，合成的 256 层单行约 22 ms；鉴于正常 VimL 以短行为主，
当前不为这个极端样例引入全行 token cache，后续只有真实 profile 命中才处理。

### go-vimlparser legacy 外部基线

为了回答“新 parser 相对已有 Go 实现是否有实际价值”，主外部基线改为本地
`/Users/chemzqm/lib/go-vimlparser` 的
`52036d11227cf7f1ce5bfcbaf00866c91702ac19`。它只作为 legacy parser 对照；Vim9 不参与
排名。可复现 harness 位于 `tools/benchlegacy` 的独立 Go module，通过临时 `go.work`
同时引用两个本地 checkout，不把参考 parser 加进 production module graph，也不需要
网络或 vendor 目录。

公平边界：

- 同一进程内预读、排序和去重文件，文件 I/O、line split、dialect 分类和成功集合筛选
  均在 `b.ResetTimer` 前；
- go-vimlparser 获得预先拆好的行，计时仍包含每次必需的 `NewStringReader` 和 typed AST
  导出；vimls-go 直接接收同一 source 的原始 string；
- 主集合只包含双方都完整成功的 legacy 文件，避免 go-vimlparser fail-fast 在错误文件
  上少做工作而虚假变快；
- 两边都保留一整个 corpus 的 AST 到该轮结束，使用默认 GC、`GOMAXPROCS=1`，各自独立
  进程 warmup 2 轮后采 20 轮，每轮恰好解析一次 corpus；
- 这是默认 GC 下的端到端 parser/result-construction 比较，不是 lexer-only 或纯 mutator
  CPU 比较。两套 AST schema 不同，“无诊断/无 panic”也不等于 AST 语义逐节点等价。

环境：Go 1.26.5、macOS 26.5.2、darwin/amd64、Intel Core i7-9750H，`GOGC` 未设置
（runtime 默认值）。vimls-go 尚无 Git HEAD；本轮 `internal/syntax/*.go` 的排序内容 hash
为 `6f64863545dac2ede760b7e0ae657e480091f9e636282f4ed04f325253335741`。

输入仍是用户 RTP 的 2911 个 `.vim` 文件、17,971,810 bytes，corpus hash 为
`241dbbcc5756aa9f678059bb2b1bd0f990493a275f7c436097f01974f80c08f8`。其中 58 个文件由
`vim9script` 触发而排除；2853 个 legacy 文件中，go-vimlparser 对 126 个 fail-fast，
vimls-go 对其中 6 个产生共 11 条 recovery diagnostics。共同成功集合为 2727 个文件、
14,386,817 bytes，占 legacy 文件数 95.6%、bytes 83.4%。被排除的 16.6% bytes 不在速度
结论内。

| parser | median | p95 | median ns/byte | median B/op | median allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: |
| vimls-go legacy | 1.914 s | 2.020 s | 133.0 | 588,627,736 | 2,223,274 |
| go-vimlparser | 10.427 s | 10.631 s | 724.7 | 7,530,619,964 | 49,991,846.5 |

在这个固定共同集合上，vimls-go median 快 5.45x，p95 快 5.26x；时间少 81.6%，
分配 bytes 少 92.2%（12.79x），allocation 次数少 95.6%（22.49x）。这个优势包含真实
allocator/GC 成本，不能表述为“纯 parser CPU 快 5.45x”。

为排除“vimls-go 少构建结果”的解释，harness 还统计共同集合的产出：

| 产出 | vimls-go | go-vimlparser |
| --- | ---: | ---: |
| 实际 command | 214,495 | 212,197 |
| expression node | 505,158 | 497,758 |
| comment | 53,258 token | 53,081 statement |
| 额外结构 | 1,183,379 tokens；23,722 blocks | 765,763 public AST nodes total |

vimls-go 的 command/expression 数没有更少，并额外保留 token/trivia、完整 byte span、block
和 loose diagnostics。go-vimlparser 的 212,197 个 command 中，109,372 个是专用 typed
statement，102,825 个仍是 generic `Excmd`；vimls-go 的“structured command”启发式计数
为 130,525。但节点计数只能排除明显少做工作，不能证明语义等价。当前仍 opaque 的
`substitute` 细节、`syntax` 规则、普通文件/窗口/buffer/option 参数等必须按 Vim 9.2.1015
继续实现，不能用性能领先替代语法完成度。

### 2026-08-29 P4 前全 RTP 有界并发基线

在 text body、heredoc、`loadkeymap` 和 Vim9 `:command {}` 恢复规则完成后，使用最终代码
重新测量全部 2911 个文件、17,971,810 bytes。corpus hash 仍为
`241dbbcc5756aa9f678059bb2b1bd0f990493a275f7c436097f01974f80c08f8`。输入预先读取、排序
并去重，发现、I/O、dialect 分类和 go-vimlparser 成功集合计算均不计时；每轮保留全部
AST，使用默认 GC、固定 `GOMAXPROCS=4`、2 次 warmup 和 20 个单次 corpus sample。

| worker | median | p95 | median throughput | 相对 1 worker |
| ---: | ---: | ---: | ---: | ---: |
| 1 | 1.608 s | 1.661 s | 11.17 MB/s | 1.00x |
| 2 | 0.840 s | 0.872 s | 21.40 MB/s | 1.92x |
| 4 | 0.534 s | 0.564 s | 33.66 MB/s | 3.01x |

4-worker 有一个 0.852 s 的调度/GC 长尾样本；表中的 p95 按 20 个样本的 nearest-rank
第 19 项计算，因此没有隐藏这个最大值。各 lane 每轮约分配 776.6 MB、4.169 M 次，说明
worker 并行提高 wall-clock throughput，但不降低总分配；下一轮性能工作应先 profile
这些分配，不继续盲目增加 worker。

当前语料分类为 2853 个 legacy、58 个 Vim9。vimls-go 在 5 个文件中保留 10 条 loose
diagnostics；逐项核对后，它们来自 vim-matchup 的故意非法 fixture、vim-scala 的 XPT
模板 DSL，以及系统 runtime `indent/dylan.vim` 中 Vim 和两个参考 parser 都拒绝的
`return = match(...)`。因此不能为了制造“零诊断”而放宽 legacy grammar。共同成功集合
仍为 2727 个 legacy 文件、14,386,817 bytes；当前产出为 214,488 commands、130,518
structured commands、505,158 expression nodes、1,219,161 tokens、23,722 blocks 和
53,258 comment tokens。相对上一个同 corpus、同 worker 协议的记录，median 分别降低
23.7%、24.5% 和 15.6%；这是整组改动后的观测差异，不归因于某一个 patch。

P4 的 expression-list 子阶段完成后，使用相同 corpus、`GOMAXPROCS=4`、2 次 warmup 和
20 个 sample 重新确认。初次把三个 lane 放在同一进程中顺序测量时出现了明显调度竞争，
因此最终表让每个 lane 在独立进程中运行；仍保留每个 lane 的最大值，不把它混入 p95：

| worker | median | p95 | max | median throughput | 相对 1 worker | median 变化 | p95 变化 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 1.575 s | 1.605 s | 1.936 s | 11.41 MB/s | 1.00x | -2.0% | -3.4% |
| 2 | 0.812 s | 0.839 s | 1.149 s | 22.13 MB/s | 1.94x | -3.3% | -3.9% |
| 4 | 0.496 s | 0.506 s | 0.520 s | 36.23 MB/s | 3.18x | -7.1% | -10.1% |

同一次完整 `benchmem` 系列的 1-worker 中位数从 776,546,840 B/op、4,168,856.5
allocs/op 降到 772,488,456 B/op、4,135,126 allocs/op，分别减少 0.52% 和 0.81%。
这个全语料变化比定向 benchmark 小，因为 expression-list command 只占全部 command 的
一部分。2911 个文件的 dialect 分类、5 个 diagnostic 文件、10 条 diagnostic，以及所有
command、structured command、expression、token、block、comment 和 text-body 计数保持
完全一致。

为避免顶层 `-cpuprofile` 把发现、分类和 go-vimlparser 参考解析混入结果，
`tools/benchlegacy` 新增了显式启用的 `TestProfileVimlsBatch`。它先预读并 hash source，CPU
profile 只包围 `workspace.ParseSources`，allocation/heap 使用 before/after profile。
P4 后三轮 1-worker 的 sampled `alloc_space` 从 2,274.62 MB 降到 2,222.08 MB；sampled
profile 只能用于归因，精确总量仍以上面的 `benchmem` 为准。当前 `scanCommands` 仍是第一
flat allocation，其中 command slice 扩容所在的 append 行占主要部分；下一步先验证容量
估算是否能在真实 RTP 降低分配且不放大小文件常驻内存，再决定是否保留。

容量统计把热点进一步拆开：全部文件的顶层 command slice 为 269,381 个 live slot、
367,413 个 capacity slot，浪费 98,032 个 352-byte slot；嵌套 command list 只有 2,500/
2,714，浪费 214 个 slot。因此实验只在顶层 `parseSource` 使用
`min(len(source)/128, 64)`，不碰 logical/embedded parser。相邻的 5-sample 1-worker A/B
中，median 从 1.441 s 降到 1.431 s（-0.7%），B/op 从 723,742,744 降到
699,991,816（-3.3%），allocs/op 从 3,763,009 降到 3,753,325（-0.3%）；但最终顶层
capacity 反而增加 1,591 个 slot，长 comment/heredoc 单 command 微基准分别多分配约
11.9/10.5 KiB，Vim9 128-line 样例也因 growth 序列改变多分配约 61 KiB。这个启发式
没有达到时间门槛且放大常驻内存，生产改动已撤回；统计工具和 shape benchmark 保留，
后续不再按 source byte 数猜 command 数量。

## 优化路线

### P0：固定 correctness 和 recovery contract

状态：恢复 contract 和聚焦测试已建立；完整语法覆盖仍持续扩充。

- legacy 与 Vim9 文件触发规则、command name、comments、continuation、separator、
  heredoc 和 block 状态独立测试。
- 表达式或 command consumer 确认错误后，同一逻辑语句的剩余 `|` 不再产生 command；
  下一独立物理行仍可产生完整 command、block、declaration 和 diagnostics。
- 无论输入多坏，cursor 必须前进；源码错误不能以 panic 作为普通恢复路径。
- incomplete expression、未闭合 string/list/dict/call、缺少 block end、嵌套 embedded
  command 都要有固定 regression fixture。

任何性能改动先证明 AST、token span、diagnostic code/span 和 recovery 后续命令不变。

### P1：删除与语法范围无关的源码复制

状态：第一项已完成。

`parseLegacyAutocmdCommandList` 过去把整个 `File.Source` 转为 `[]byte`，即使只解析末尾
很小的 autocmd body。现在只复制 `span.Start:span.End` 并在完成后统一 rebase；大前缀
绝对 span 测试和官方/embedded corpus 已覆盖。

下一步只做 profile 驱动的小改动：

- 检查 autocmd body 的 `[]byte -> string` 第二次复制是否仍显著。
- 对已知结果数量的 command/token/diagnostic slice 做有证据的容量预估。
- 不引入 arena、`unsafe`、全局对象池或自定义 allocator。

验收：绝对 span 与 nested lambda span 完全一致；目标 flat allocation 消失；若总指标
改善不足 5%，必须有清晰的单一 hotspot 消除证据才保留改动。

### P2：复用 boundary expression result

状态：已完成。相对后续 lexer/consumer 重写风险较低，但 continuation、diagnostic 和
logical span 等价测试仍是必要条件。

先只覆盖一条普通表达式的命令：

```text
if elseif while return throw call eval defer
caddexpr cexpr cgetexpr laddexpr lexpr lgetexpr
```

legacy/Vim9 argument consumer 调用 `parseExpressionPrefix` 时，除了 consumed boundary
之外，保留 AST 和 diagnostics 作为 command 私有的暂存结果。details 阶段直接消费
它，不再次调用 `parseExpression`。实现只增加 `Command` 的私有 boundary 指针和小型
结果结构，没有新增 Manager、Service 或 parser interface。

必须保持：

- trailing-expression diagnostics 与当前 `parseExpression` 等价。
- logical view 中缓存的是 logical 坐标，最终只映射一次。
- Vim9 continuation 扩展后重新计算缓存；command kind/argument 被 promote 时清空旧缓存。
- block 构建、`ScriptVersion` 更新、heredoc 和 embedded command 的时序不变。
- 第一批不处理 expression list、declaration RHS、assignment、`put =` 或 `for`。

验收结果：聚焦 benchmark 证明重复 parse 已消除，并获得上表的时间和分配下降；
legacy/Vim9 bar、comment、malformed line、空 `return`、反斜杠 continuation 和 Vim9
automatic continuation 覆盖 AST、diagnostic 数量及绝对 span；official corpus 和完整
syntax tests 通过。重新 profile 后 `lexExpression` 是第一 flat allocation，因此 P3
先于继续扩大 P4。

### P3：表达式 lexer 改为 cursor-driven

状态：已完成，但作用域严格限制为单个 command argument / logical expression。

`expressionParser.tokens []expressionToken + index` 已改为保存 `source`、`base`、
`offset` 和 `current token` 的 lexer。parser 构造时扫描第一个 token，`advance()` 才扫描
下一个。token text 继续引用原始 source；字符串的语义解码只在 consumer 需要时发生。
file scanner 仍负责物理行、continuation 和逐行 recovery，expression lexer 不跨逻辑
语句运行。

只为已证明的歧义复制小型 lexer 值，例如 Vim9 generic-call 与 `<` 比较、legacy curly
name 和 dictionary key。失败的 speculative path 不发布 diagnostics，也不移动主 cursor。
普通 command boundary 不再复制完整 token slice；`:execute` 静态 heredoc 检测也只保留
最后一个 token。

验收结果：token golden 覆盖两个 dialect、非零 base、comment/string、blob/number、
longest operator 和 EOF；parser fixtures 覆盖 curly name、generic/comparison、插值、
incomplete delimiter、comment continuation 及绝对 span。官方 syntax corpus、完整 tests
和 race tests 通过。上表显示短行时间和分配继续下降，`lexExpression` 的完整 token
slice flat allocation 已从新 profile 消失。

### P4：让 dialect command consumer 一次产出 boundary 和 details

状态：进行中；普通单表达式、expression-list、declaration/for、Vim9 command-start
call/assignment RHS、typed-declaration continuation probe、legacy/Vim9 `put =expr`、
`global`/`vglobal` 和 `folddoopen`/`folddoclosed` 已完成。实现继续以物理行/逻辑语句为
边界，不追求 JavaScript 风格的全文件 one-pass。

最终 API 不应是“generic scanner 先切 argument，再由 details parser 重读”。legacy
consumer 和 Vim9 consumer 应直接填充现有 `Command`，只额外返回 argument end、
separator/comment 和 recovery 状态。generic `scanCommands` 只负责 range、modifier、
命令查表、结果追加与行间调度；不为这个迁移新增通用 result/interface 层。

分阶段迁移，不能一次重写所有 command：

1. 普通单表达式 command 已通过 P2 的 boundary result 完成一次 parse。
2. expression list 已完成；成功时直接复用 scanner 产生的完整 AST，任一表达式失败时不
   发布部分 cache，details 回退原路径以保持 loose recovery 和 diagnostics。
3. declaration RHS 和 `for` iterable 已完成；两者只缓存 RHS，declaration target、for
   binding/type annotation 仍走各自的专用 grammar。malformed 或 span mismatch 不发布
   cache，`=<<` heredoc 仍走独立路径。
4. Vim9 word-start call 已复用完整 expression；assignment 只复用已验证的 RHS，LHS、
   operator 和最终 assignment AST 仍由专用 details grammar 构造。punctuation-start、
   destructuring 和 option/environment/register target 保留原 fallback。
5. typed declaration 的 continuation probe 已改为非 destructive 地检查同一 RHS
   boundary；只有 expression、diagnostics 和 logical span 全部有效才跳过旧 parse，
   malformed、空 RHS、span mismatch 和 heredoc 仍走原 fallback。
6. `put`/`iput` 已分别按两种方言迁移。legacy 先执行 Vim 的 Ex delimiter 规则，只把
   `\|` 和 `\"` 收缩到临时表达式视图，并用局部精确 contraction segment 恢复原始 span；
   空 RHS 保留 expression register reuse。Vim9 直接使用表达式边界，字符串内 bar、
   `#` comment、自动 continuation 和空 RHS 错误均保持 Vim9 规则。
7. `global`/`vglobal` 已改为先拥有完整外层 argument，再按 Vim `skip_regexp_ex()` 的
   delimiter、转义、collection、`\v`/`\V` 和 previous-pattern 规则提取 body；只把
   closing delimiter 后的内容交给嵌套 command-list parser。regexp 和 nested payload
   内的 bar、quote、Vim9 `#` 不再被外层提前切开。
8. `folddoopen`/`folddoclosed` 的完整 argument 已作为嵌套 Ex command list。它们和
   `global`、`windo` 一样通过递归 `do_cmdline()` 执行，因此外层 `legacy`/`vim9cmd`
   override 不泄漏到 body；body 从所在脚本或函数的 enclosing dialect 开始。
9. substitute、mapping、syntax、heredoc 等专用 consumer 保持独立。

legacy 与 Vim9 不是用一个 boolean 到处切换的同一语言。公共代码只保留 byte cursor、
Span、Diagnostic、已验证相同的 operator table 等机械部分；comments、command
abbreviation、whitespace、continuation 和 recovery 决策留在各自 parser。

验收：profile 中普通 command 不再出现 boundary/details 双重 parse；每次迁移都有
before/after AST golden 和 Vim 官方 fixture；不得改变 embedded scope ownership。

expression-list 的定向 benchmark 每行包含三个非平凡表达式，legacy/Vim9 各重复 64
次；`GOMAXPROCS=1`、`benchtime=30x`、10 个 sample：

| dialect | 状态 | ns/op 中位数 | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| legacy | P4 前 | 888,898 | 450,888 | 3,455 |
| legacy | expression-list 后 | 566,993 | 283,072 | 1,922 |
| legacy | 变化 | -36.2% | -37.2% | -44.4% |
| Vim9 | P4 前 | 1,332,873 | 619,731 | 10,802 |
| Vim9 | expression-list 后 | 1,007,060 | 453,971 | 9,266 |
| Vim9 | 变化 | -24.4% | -26.7% | -14.2% |

Vim 9.1.0000 与 9.2.1015 的 `ex_echo()`、`ex_execute()` 和 `compile_mult_expr()` 共同
确认：这些命令按 dialect 连续解析完整表达式，顶层 `|`/换行才结束；legacy 的 `"` 不能
先当普通 trailing comment，Vim9 `#` 需要合法前置空白且不能误吞 `#{`。回归测试覆盖
logical continuation 的绝对 span、第二个表达式损坏后下一物理行恢复，以及一条命令级
`legacy`/`vim9cmd` override。

declaration initializer 和 `for` iterable 继续使用同一协议做独立定向 benchmark：

| consumer | dialect | 状态 | ns/op 中位数 | B/op | allocs/op |
| --- | --- | --- | ---: | ---: | ---: |
| declaration RHS | legacy | 前 | 862,490 | 437,264 | 3,325 |
| declaration RHS | legacy | 后 | 596,468 | 292,368 | 1,981 |
| declaration RHS | legacy | 变化 | -30.8% | -33.1% | -40.4% |
| declaration RHS | Vim9 | 前 | 1,696,072 | 742,096 | 10,866 |
| declaration RHS | Vim9 | 后 | 1,435,050 | 597,200 | 9,522 |
| declaration RHS | Vim9 | 变化 | -15.4% | -19.5% | -12.4% |
| `for` iterable | legacy | 前 | 772,683 | 397,720 | 2,367 |
| `for` iterable | legacy | 后 | 640,433 | 300,440 | 1,471 |
| `for` iterable | legacy | 变化 | -17.1% | -24.5% | -37.9% |
| `for` iterable | Vim9 | 前 | 1,196,510 | 585,027 | 7,610 |
| `for` iterable | Vim9 | 后 | 1,055,268 | 487,747 | 6,714 |
| `for` iterable | Vim9 | 变化 | -11.8% | -16.6% | -11.8% |

declaration tests 固定 LHS/destructuring、bar/comment、legacy/Vim9 continuation、
malformed fallback、下一物理行恢复和 heredoc 排除。`for` tests 固定专用 binding/type
grammar、`in` 前后换行、nested iterable continuation 的绝对 span、bar/comment 与
missing/malformed iterable recovery。完成两项后，全 RTP 的 corpus hash、dialect、AST、
token、block、comment、text-body 和 diagnostic 计数仍逐项不变。

Vim9 command-start call 和 assignment 使用相同的 64 行、`GOMAXPROCS=1`、
`benchtime=30x`、10 sample 协议。assignment 仍单独解析 LHS，只复用 RHS：

| consumer | 状态 | ns/op 中位数 | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| command-start call | 前 | 827,196 | 368,306 | 5,743 |
| command-start call | 后 | 669,836 | 284,338 | 4,975 |
| command-start call | 变化 | -19.0% | -22.8% | -13.4% |
| command-start assignment | 前 | 1,446,972 | 662,308 | 10,230 |
| command-start assignment | 后 | 1,157,748 | 517,411 | 8,886 |
| command-start assignment | 变化 | -20.0% | -21.9% | -13.1% |

随后发现 `completeVim9TypedDeclaration` 为判断是否需要 automatic continuation，会在
scanner 已得到合法 RHS 后再次调用 `parseExpression`。它现在只读检查原 boundary，且
不提前消费 details 所需结果。相邻 declaration benchmark 中，Vim9 中位从
1,486,425 ns/op 降到 1,175,716 ns/op（-20.9%），B/op 从 597,200 降到 452,304
（-24.3%），allocs/op 从 9,522 降到 8,178（-14.1%）。测试固定单行 typed
declaration、list/method continuation、malformed 下一行恢复和 heredoc 排除。

`put =expr` 的定向 benchmark 同样使用 64 行、`GOMAXPROCS=1`、`benchtime=30x`、10
sample。legacy 常见无转义路径不会构造 normalized view；成功时表达式直接保存在
`Command`，只有 malformed recovery 才分配 boundary 对象。最初“每条命令都构造映射”
的实现中位约 1.037 ms、514,930 B/op、3,551 allocs/op，因约 45% 时间回退被拒绝。

| dialect | 状态 | ns/op 中位数 | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| legacy | 前 | 715,248 | 339,288 | 2,092 |
| legacy | 最终 | 691,133 | 342,312 | 2,122 |
| legacy | 变化 | -3.4% | +0.9% | +1.4% |
| Vim9 | 前 | 1,221,900 | 546,595 | 9,207 |
| Vim9 | boundary consumer 后 | 1,243,917 | 549,667 | 9,271 |
| Vim9 | 变化 | +1.8% | +0.6% | +0.7% |

这里保留 Vim9 的小幅成本是为了获得正确的字符串/bar/comment/continuation boundary；
三项均低于 3%。legacy 的转换只在真实 `\|`/`\"` 输入上发生，普通 RTP `put =expr`
不承担映射分配。

完成上述 P4 子阶段后，再用相同全部 2911 文件、17,971,810 bytes、2 次 warmup、20
sample、各 lane 独立进程复测。corpus hash、2853/58 dialect 分类、5 个 diagnostic
文件/10 条 diagnostic，以及 command、structured command、expression、token、block、
comment 和 text-body 计数均与 expression-list 后完全一致：

| worker | median | p95 | max | median throughput | 相对 1 worker | median 变化 | p95 变化 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 1.390 s | 1.422 s | 1.436 s | 12.93 MB/s | 1.00x | -11.8% | -11.4% |
| 2 | 0.728 s | 0.742 s | 0.763 s | 24.68 MB/s | 1.91x | -10.3% | -11.5% |
| 4 | 0.431 s | 0.444 s | 0.447 s | 41.74 MB/s | 3.23x | -13.2% | -12.3% |

表中变化相对 expression-list 子阶段后的 1/2/4-worker 数字。1-worker median B/op 从
772,488,456 降到 720,764,656（-6.7%），allocs/op 从 4,135,126 降到
3,736,407.5（-9.6%）。这轮没有保留 command-slice byte-count capacity heuristic；它
只带来约 0.7% 时间差且增加 retained capacity，不能用分配数字掩盖常驻内存回退。

完成 `put` 后的最终 RTP smoke 使用相同 hash 和结构计数，4 worker 为 434.9 ms、
720,659,632 B/op、3,736,465 allocs/op，耗时仍在上表既有 p95 内，bytes/op 略低于该
基线。曾尝试把 exact boundary slice 放进所有 `logicalView`，单轮会额外增加约 6 MB；
该设计已删除，收缩映射只为真正出现 `\|`/`\"` 的 legacy `put` 建立局部 segment。

`global`/`vglobal` 完成后增加了永久定向 benchmark：64 组 `g/^foo$/echo foo` 与
`v/^bar$/echo bar`，共 128 个顶层和 128 个嵌套 command。`GOMAXPROCS=1`、
`benchtime=30x`、10 sample 的中位数为 612,550 ns/op，259,144 B/op、1,196 allocs/op；
10 轮分配完全一致。这个绝对基准只防止后续实现退化，不能与旧 parser 的 opaque
argument 路径作等价 before/after 比较，因为新路径有意构造了更多 AST。

最终代码对相同 2911 文件的 corpus hash、2853/58 dialect 分类、5 个 diagnostic 文件和
10 条 diagnostic 保持不变。共同成功集合的输出变为 214,498 commands、130,522
structured commands、505,193 expressions、1,219,160 tokens、23,722 blocks、53,257
comments。全量 RTP 的 nested live-command 数增加 14；其中共同成功集合增加 10 个
command，均来自 12 条真实顶层 `global` 的 payload。token/comment 各少 1 是因为
regexp/payload 不再被外层错误分类。RTP 中没有真实执行的 `folddo*`，因此该项不改变语料
输出。

4-worker 独立跑了两组各 20 sample，median 分别为 441.2 ms 和 441.4 ms（约
40.7 MB/s），相对 `put` 后 430.6 ms 基线约 +2.5%；p95 分别为 460.7 ms 和
477.5 ms，受少量 0.47--0.49 s 调度/GC 长尾影响。单次相邻 smoke 的分配从
720,659,632 B/op、3,736,465 allocs/op 变为 720,796,032 B/op、3,736,629 allocs/op，
只增加 0.019% bytes 和 0.004% allocation 次数；`Command` 仍为 352 bytes。三轮隔离
profile 中新增功能没有进入 CPU 或 allocation top，第一 flat allocation 仍是
`scanCommands`。因此记录这次小幅 wall-clock 差异但不据此重写 parser；后续性能判断继续
看可复现 profile 和相邻 A/B，而不是追逐并行长尾。

同一 profile 还显示，`scanCommands` 中每条命令调用的 `vimdata.Lookup` 累计约占 CPU
6%。旧实现为了保留 Vim command table 的缩写优先级，每次都从头线性扫描约 600 条命令；
这个顺序约束并不要求扫描首字节不可能匹配的项。当前实现初始化 256 个首字节的有序
区间，查询只扫描对应区间，仍然返回原 table 中第一个 prefix match。永久测试把每个内建
命令的每个非空 prefix 与旧的全表有序 lookup 比较，避免优化改变歧义缩写语义。

`BenchmarkLookupCommands` 每次迭代查询 16 个真实 exact、abbreviation、特殊命令和 miss
输入；`GOMAXPROCS=1`、10 sample 的结果如下，均为 0 B/op、0 allocs/op：

| 状态 | batch ns/op 中位数 | 变化 | 倍速 |
| --- | ---: | ---: | ---: |
| 全表线性 lookup | 24,470 | - | 1.00x |
| 首字节有序区间 | 821 | -96.6% | 29.8x |

随后用完全相同的 2911 文件、17,971,810 bytes、2 次 warmup 和 20 sample 复测 4 worker。
corpus hash、dialect 分类、diagnostic 和上面的全部输出计数均未变化；median 为 326.0 ms，
p95 为 336.4 ms，max 为 338.8 ms，median throughput 为 55.1 MB/s。相对紧邻的两组
441.2/441.4 ms 中位数，wall-clock 降约 26.1%；B/op 与 allocs/op 只剩 benchmark/GC
噪声级变化。这说明先按 profile 消除高频确定性工作，比猜测 AST slice 容量更有效。

随后补齐的 `filter[!]` modifier 先消费自身 bang、delimiter、pattern 与 `g`/`j`/`f`
flags，再把剩余范围交回普通 command scanner。它修复的是 prefix grammar 和坏行恢复，
不是 RTP 热点；当前语料中没有可靠的实际 filter modifier。为了不让冷门 regexp 字段
放大每个普通 modifier，只有通用 bang 直接保留，filter regexp 细节放在按需分配的
`FilterModifier`。相同 RTP 的 5-sample 4-worker smoke 中位为 319.9 ms、
720,737,968 B/op、3,736,617 allocs/op；corpus hash、diagnostic 与全部结构计数不变。
这组短样本只确认没有可见回退，不替代上面的 20-sample lookup 基线。

`substitute`/`smagic`/`snomagic`/`~` 随后迁移到同一个 command-owned one-walk
consumer。pattern 扫描复用 `skip_regexp_ex()` 对应的 magic/collection 边界，replacement
按 Vim 的 escaped-byte 规则扫描；只有完成 replacement 后的 flags/count tail 才能暴露
`|`。legacy 的 `:s` 字母紧邻形式严格复刻 `one_letter_cmd()` 的 `c/g/i/I/r` 歧义规则，
避免把 `setbufvar(...)` 一类未知命令误拆成 `:s`；Vim9 则先识别 `s:Func()`、索引、成员和
method-call expression，再考虑 substitute separator。`\=expr` 在 boundary 阶段只解析一次，
同一指针同时挂到 `Substitute` 与 `Command.Expressions`。E1241/E1242/E1270/E10/E146、
trailing E488、缺失 delimiter 和坏表达式都遵守“当前行剩余归属错误命令、下一物理行恢复”。

官方 corpus 首轮暴露了两个有价值的歧义回归：过宽的 legacy `s*` fallback 会把
`setbufvar(...)` 当作 `:s`，而 Vim9 `s:XxdFind()` 应先成为 expression。修复后，相同 RTP
仍是 hash `241dbbcc5756aa9f678059bb2b1bd0f990493a275f7c436097f01974f80c08f8`、
2911 files / 17,971,810 bytes、2853 legacy / 58 Vim9、5 个文件 / 10 条 diagnostic。
共同成功集合变为 214,498 commands、130,524 structured commands、505,221
expressions，其余 token/block/comment/text-body 计数不变。其中有 14 个 substitute AST、
2 个 `\=expr` replacement；expression 总数的其余变化来自此前被 `:s` 歧义吞掉的 Vim9
expression 现在按源码顺序正确分类。

4 worker、2 次 warmup、20 sample 的 median 为 330.2 ms，p95 354.5 ms，max 357.2 ms，
约 54.4 MB/s；median B/op 为 725,031,104，allocs/op 为 3,736,723。相对 lookup 后的
326.0 ms median，时间约 +1.3%；`Command` 因冷 `*Substitute` 从 352 增至 360 bytes，
全 RTP median B/op 相对 filter smoke 增约 4.29 MB（0.6%）。这是完整 AST 的已测成本，
尚不足以支持一次高风险 command layout 重构；后续新增高频 payload 前继续用 profile 和
全 RTP retained bytes 决定是否进入 P8 compaction。

`highlight` 随后按相同 ownership 原则结构化 list、query、clear、link、default 与
attribute。这里不能复用 generic opaque scanner：Vim 的 Ex 层先识别未转义 `|`、legacy
`"` 和 Vim9 `#`，单引号只在之后的 highlight consumer 中用于包含空格的 value，因此
单引号里的 bar/comment 仍会先结束外层命令。E412/E413/E415/E416/E417/E475 保留已扫描
字段，但按照编辑态 recovery 规则吞掉同一物理行尾；下一物理行继续正常解析。动态 group
是否存在、颜色/attribute value 的运行时有效性以及未知未来 key 不产生静态语法误报。
`ctermfont` 始于 Vim 9.1.0030，latest AST 始终保留它，target-version pass 单独诊断旧目标。

本轮可复现 RTP manifest 已变为 2905 files / 17,791,591 bytes，hash
`9fd0f23dea8354ac62802a99ec76f7039343c4451331c356ab46fc53af77b687`；显式 RTP 中的
`/Users/chemzqm/vim-dev/coc-clangd` 当前不存在。它比上一轮少 6 个文件，因此下面结果不
作为严格同语料 A/B。diagnostic 仍为 5 files / 10 diagnostics；共同成功集合为 214,006
commands、145,984 structured commands、505,113 expressions、1,203,008 tokens、23,709
blocks、53,084 comments，其中有 18,174 个 highlight AST、6,435 个 attribute、14 个
substitute AST 和 2 个 replacement expression。

相同机器、4 worker、2 次 warmup、20 sample 的 median 为 337.9 ms，p95 354.9 ms，
max 365.1 ms，约 52.6 MB/s；median B/op 为 735,028,248，allocs/op 为 3,794,233。
上一轮 2911-file substitute median 为 330.2 ms、725,031,104 B/op、3,736,723 allocs/op；
由于 corpus 已漂移，只能说明完整高频 highlight AST 的总成本仍在低个位数百分比时间和
约 10 MB allocation 量级，不能声称精确回退比例，也不以此为由立即重构 Command layout。

紧接着的 post-highlight profile 确认 `vim9ContinuationScan.appendTail` 通过
`tail += string(character)` 制造大量短命字符串；它占 allocation objects 的显著比例，
但只为 `is`/`isnot` 和符号 operator 保留至多几个尾字节。实现改为内嵌 `[7]byte` ring：
7 bytes 同时保留最长 `isnot#` 和它前面的 word-boundary byte，既消除分配，也修复旧 6-byte
截断可能把 `valueisnot#` 误判为 continuation 的边界缺失。固定 micro benchmark 从约
67.7 us、22,496 B/op、2,910 allocs/op 降至约 11.2 us、0 B/op、0 allocs/op。

在完全相同的 2905-file manifest 上，优化前后的 20-sample 4-worker A/B 为：median
337.9 -> 332.7 ms（-1.5%），median B/op 735,028,248 -> 725,635,280（-9.39 MB，-1.3%），
allocs/op 3,794,233 -> 2,537,191（-1,257,042，-33.1%）；优化后 p95 341.7 ms、max
356.0 ms、约 53.5 MB/s。虽然 wall-clock 改善低于常规 5% 门槛，但它删除了 profile
确认的 top object hotspot、显著降低 GC 压力并同时修正 word-boundary correctness，因此
保留。corpus hash、5 files / 10 diagnostics 和全部 AST/token 计数不变。

随后结构化高频 `:syntax keyword`、`:syntax match` 和 `:syntax region`。这里同样不能先由
generic Ex scanner 切 argument：keyword token 内的 `|`、legacy `"` 和 Vim9 `#` 是 payload，
match/region 的任意 regexp delimiter、collection、escape 和 offset 也必须先由 syntax
consumer 拥有。实现依据本地 Vim v9.1.0000 与 v9.2.1015 的 `src/syntax.c`、`src/ex_docmd.c`
以及官方 syntax 测试；两版这部分 grammar 没有差异。未知或未来 subcommand 保持 opaque，
而确定的 E393/E395/E398/E399/E401/E402/E405/E406/E475/E789/E890 保留 partial AST、吞掉
同一行剩余内容，并从下一物理行恢复。

同一 2905-file manifest 的 diagnostic 仍为 5 files / 10 diagnostics。共同成功集合仍为
214,006 commands、505,113 expressions、23,709 blocks；structured commands 增至 193,580，
并新增 53,802 个 syntax AST、130,080 keywords、32,571 options、29,921 patterns。token 和
comment 各增加 141，因为 syntax consumer 现在能暴露此前 opaque payload 内的 comment，
其余既有结构计数不变。

初版 20-sample 4-worker median 为 339.3 ms、p95 354.6 ms、769,996,984 B/op、
2,935,503 allocs/op，相对 post-ring 同 corpus 基线的 wall time 约 +2%，但 allocation
增加 44.4 MB 和 398,312 次。隔离 profile 证明 `syntaxParseResult`、`SyntaxOption` 和
`SyntaxPattern` 的临时指针返回是新增 object hotspot；改为 value return 后，公开 AST、
诊断和 recovery 不变，median B/op 降至 757,347,464（-12.65 MB），allocs/op 降至
2,793,174（-142,329）。20-sample wall-time median 为 337.1 ms；该批次有 450.6 ms 的系统
长尾，因此不声称 latency 提升。新 profile 中 option/pattern 临时对象的 flat allocation
归零，`parseSyntaxCommand` 只保留最终 `*SyntaxCommand`，所以该低风险简化予以保留。一个
试图消除 `parseGroupListValue` 局部地址逃逸的后续改写在真实语料上没有减少分配，已撤销。

下一片按固定 RTP 中剩余 opaque `:syntax` 频率选择：cluster 1,175、case 585、sync 443、
include 134，其余更少。先结构化 grammar 较小且能直接提供 group membership 的 cluster，
保留重复 `contains`/`add`/`remove`、`@cluster`、完整 list span 和各 item span，并补齐纯语法
可证明的 E400/E405/E406/E407/E408；依赖当前 Vim highlight table 的 E409 不做静态猜测。
RTP 首次回归暴露 `/usr/local/share/vim/vim91/syntax/elmfilt.vim` 中合法的连续逗号：Vim
接受 leading/interior empty item，而且该 empty item 会影响 `ALL` 等特殊值是否位于首项。
parser 因此保留 zero-width item span；只有 `=` 后整体缺值才是 E406。修正后重新回到同一
5 files / 10 diagnostics。

共同成功集合新增 920 个 cluster AST，structured commands 从 193,580 增至 194,262，
syntax options 从 32,571 增至 33,559；其余既有计数不变。20-sample 4-worker median 为
330.1 ms、p95 341.6 ms、max 344.5 ms、约 53.9 MB/s，median B/op 757,833,320、
allocs/op 约 2,798,717。相对 cluster 前的 value-return 基线只增加约 486 KB 和 5,543 次
分配；wall time 样本反而低约 2%，只据此判定无可测回退，不宣称新增功能带来加速。

随后结构化 585 个 RTP `:syntax case`，共同成功集合中实际新增 508 个 AST。Vim 的
`syn_cmd_case()` 只校验第一个 whitespace-delimited token 是否为大小写不敏感的
`match`/`ignore`，同时 `find_nextcmd()` 独立寻找第一个 `|`；两者之间的任意 bytes 会被
忽略，legacy `"` 和 Vim9 `#` 在这里也不是 consumer comment。无参数是 query，非法首项
为 E390 并按 loose recovery 吞掉同一行尾。为 case 单独给 `SyntaxCommand` 增加 `Span`
曾使全部 5.5 万个 syntax node 跨 Go size class，单轮多约 1 MB，故撤回；mode 作为现有
keyword span 保留，既完整支持 semantic token，又不扩大高频 node。

同一 20-sample 4-worker 协议得到 median 338.9 ms、p95 361.9 ms、max 366.4 ms、约
52.5 MB/s，median B/op 757,944,752、allocs/op 2,799,895。相对前一批异常偏快的 cluster
样本 median +2.65%，但仍接近 cluster 前 337.1 ms 基线；确定的新增成本只有约 111 KB 和
1,179 次分配，因此记录为功能成本且不声称 latency 改善或回退。

`conceal on/off` 和 `spell toplevel/notoplevel/default` 与 case 使用同一个 Vim consumer
shape，因此复用同一小函数并保留 query/mode keyword span；固定共同成功集合新增 21 个
AST，仍为 5 files / 10 diagnostics。没有新增公开字段或 package，单次 RTP smoke 的
bytes/alloc 波动处于 benchmark 噪声内。

随后结构化 `:syntax include [@cluster] filename`。Vim v9.1.0000 与 v9.2.1015 的
`syn_cmd_include()` 都会在 consumer 内动态加入 `EX_XFILE | EX_NOSPC`，因此 filename
不是普通 token：空白、legacy `"`、Vim9 `#`、反斜杠或 CTRL-V 保护的 bar 以及
`` `=expr` `` 都先属于 filename，只有 consumer 确认的未保护 bar 才能开始下一条 Ex
command。parser 保留 cluster 和未展开的完整 filename byte span，不执行表达式、不展开
路径，也不 source 用户脚本；无法可靠闭合的 backtick payload 吞掉本行剩余内容并从下一
物理行恢复。`EX_NOTRLCOM` 导致的 filename 尾随空白也原样保留。

同一固定 RTP 共同成功集合新增 109 个 include AST，`syntax_commands` 从 55,251 增至
55,360，`syntax_keywords` 从 130,609 增至 130,718；完整语料仍为 5 files / 10
diagnostics，既有 expression、block、highlight、substitute 等计数不变。4 worker、2 次
warmup、20 sample 的 median 为 332.4 ms、p95 352.4 ms、max 357.8 ms、约 53.5 MB/s，
median B/op 为 757,965,488、allocs/op 约 2,800,220。该结果仍落在 include 前已记录的
330--339 ms 样本带内，不声称提速，也没有达到性能回退门槛。

本轮还重新运行同一 2,721-file / 14,206,598-byte common-success legacy corpus 的单
worker 对照。vimls-go 20-sample median 为 974.6 ms、581,417,544 B/op、2,088,022
allocs/op；go-vimlparser 为 7.483 s、6,136,732,232 B/op、49,575,189 allocs/op，即该
harness 下 vimls-go 约快 7.68 倍。两者 AST shape 不同，因此这个数字只比较固定共同
成功输入上的完整 parse 成本；结构计数仍单独记录，不能把速度差当作语义等价证明。

`syntax clear`、显式 `syntax list` 以及空/非 ASCII 字母开头 subcommand 所选择的隐式
list 随后复用同一 group-token consumer。它保留普通 group 与 `@cluster` span，但不把
依赖当前 highlight/cluster table 的 E28/E391/E392 猜成静态诊断。共同成功集合新增 69
个 syntax AST 和 41 个 group operand span，仍为 5 files / 10 diagnostics；额外暴露的
一个 comment/token 来自此前整行 opaque 的查询边界。相同 20-sample 4-worker 结果为
median 336.1 ms、p95 350.1 ms、max 378.8 ms、约 52.9 MB/s，median B/op
757,920,624、allocs/op 2,800,341。相对 include 轮的 median 约 +1.1%，p95 反而更低，
新增约 122 次 allocation 与新增 node/slice 数一致，因此没有可测性能回退。

随后结构化 `syntax sync`。Vim v9.1.0000 与 v9.2.1015 的 `syn_cmd_sync()` 在该
grammar 上一致：direct setting 的外层 key 和 `ccomment` 可选 group 都使用
`skiptowhite()`，因此只有 token 起点的 `|` 才是 Ex 边界，已经进入 token 的 bar 必须保留；
`linecont` 则直接使用 `skip_regexp(..., TRUE)`，支持 character class 和 magic 切换，且不
消费 syntax offsets。`sync match` 复用 match AST 并保留 `grouphere/groupthere` target，
`sync region` 复用 region AST 且仍拒绝这两个 match-only option。E394 依赖已经定义的
region；E403 还依赖 buffer 中已有的已编译 line-continuation regexp、条件执行和 regexp
编译成功，因此 stateless syntax parser 都不猜测，而是保留可证明的完整 AST。

固定 common-success corpus 的 `syntax_commands` 从 55,429 增至 55,783，新增 354 个
sync AST；`syntax_options` 从 33,559 增至 33,929，`syntax_patterns` 从 29,921 增至
30,056。完整 RTP 仍为相同 hash、2905 files / 17,791,591 bytes、5 files / 10
diagnostics。20-sample 4-worker median 为 331.6 ms、p95 355.0 ms、max 369.2 ms、约
53.65 MB/s，median B/op 758,029,968、allocs/op 2,801,434。相对 clear/list 轮新增约
109 KB 和 1,093 次 allocation，与新增 AST/slice 数一致；wall time 仍处在既有样本带内，
因此判定没有可测性能回退，不声称提速。

下一小片完成 `syntax iskeyword`、`syntax foldlevel` 以及
`on/enable/manual/off/reset`。`iskeyword` 不调用 `find_nextcmd()`，所以从第一个非空白
byte 到 logical line 真正结尾都是一个 chartab payload，包含 bar、legacy quote、Vim9
hash 和尾随空白；parser 只保留 span，不复刻 `buf_init_chartab()`。`foldlevel` 仅接受
大小写不敏感的完整 `start`/`minimum` 首 token，任何其它 trailing byte（包括 separator
或 comment）都是 E390，并按 loose recovery 从下一物理行继续。

五个 runtime mode 都只调用 `set_nextcmd()` 后 source 对应 runtime 文件；静态 parser 不
执行这些脚本。它们只在剩余 argument 跳过空白后立即遇到 bar 或 Vim9 `#` 时分隔，已有
普通 trailing payload 会吞掉其后的 bar/comment。与此同时，`:syntax` dispatch 改为精确
复现 `ex_syntax()` 的连续 ASCII-alpha subcommand：已知 subcommand 后的第一个非字母 byte
直接属于参数，不再为个别 consumer 增加边界特例。

固定 common-success corpus 的 `syntax_commands` 从 55,783 增至 55,895，
`syntax_keywords` 从 130,759 增至 130,796；完整 RTP hash 和 5 files / 10 diagnostics
保持不变。20-sample 4-worker median 为 331.2 ms、p95 358.3 ms、max 362.7 ms、约
53.73 MB/s，median B/op 758,045,008、allocs/op 2,801,601。相对 sync 轮 median latency
基本不变，新增约 15 KB 和 168 次 allocation，与 112 个新增 syntax AST 和 37 个 payload
span slice 一致，没有可测性能回退。

完成全部 `:syntax` subcommand 后重新隔离 profile，`scanCommands` 仍是首要 flat
allocation：3 次单 worker 全语料 parse 中约 995.8 MB，占 sampled allocation space
46.0%；CPU cumulative 约 45.7%。最终保留的顶层 command slice 有 268,893 个 live slot、
361,824 个 capacity slot，浪费 92,931 个 slot；嵌套 command list 只有 2,510/2,718。
这再次说明若要处理 command 常驻内存，只应针对顶层 slice 或 `Command` layout，不应重写
logical/embedded parser。

为核对 esbuild 的 newline 近似预分配思路，profile harness 还统计了物理行数 hint，但
没有把它接入 production。`min(physicalLines, 64)` 会预留 125,338 个 slot，只覆盖
93,262 个真实 command slot，却在 2,032 个文件中多留 32,076 个；cap 128 会预留
190,484 个、只覆盖 136,066 个，并在 2,383 个文件中多留 54,418 个。VimL 的 comment、
heredoc、text body 和 continuation 使“物理行数约等于 command 数”不成立，因此该方案与
此前 source-byte hint 一样明确否决，不再重试。

`Command.ScriptVersion` 的真实值域只有 1--4，把它从 `int` 收窄为 `uint8` 后
`unsafe.Sizeof(Command{})` 从 376 降为 368 bytes。相同 profile 的顶层 live/capacity
内存从 101,103,768/136,045,824 降至 98,952,624/133,468,816 bytes；即使 slice growth
噪声多出 863 个 capacity slot，仍少保留约 2.58 MB。相同 4-worker 20-sample 的两组
median 为 342.8/338.3 ms，较 331.2 ms 功能基线慢约 2--3.5%，但低于既定回退门槛且
受 GC/调度噪声影响；median B/op 从 758,045,008 降到约 751.5 MB，allocation 次数基本
不变。因此只主张确定的 layout/retained allocation 收益，不声称 latency 提升。收窄后的
空洞也允许下一项冷 command pointer 在不超过原 376-byte layout 的前提下加入。

### P5：logical view 只按 profile 再优化

状态：延后。

identity legacy 路径已直接扫描原 source，变换路径已使用压缩 segments。当前
`parseLogicalCommandDetails` flat allocation 仅约 7.5 MB，巨大 cumulative 值主要来自
它调用的 expression/details parse。

先完成 P2/P3 并重新 profile。只有 mapper 成为新的 flat hotspot 时，才考虑：

- details 构造时通过现有 `logicalView.mapSpan` 直接产生原始 span；或
- 让 command 暂存结果保持 logical 坐标并执行一次无 map-set 的专用递归映射。

不为了“零临时 File”引入全局 span abstraction、通用 SourceMap interface 或每节点
额外指针。

### P6：有界文件并行和确定性 workspace merge

状态：open-document worker 和有界 `workspace.ParseSources` batch 已实现；确定性的跨文件
semantic index merge 尚未实现。

当前批量 parser 已采用固定 worker pool，而不是 goroutine-per-file；后续 semantic merge
沿用同一所有权模型：

1. 文件发现阶段规范化并排序 URI，分配稳定 file index。
2. 至多 `min(GOMAXPROCS, 4)` 个 worker 从有界队列读取 immutable snapshot。
3. 每个 worker 独占 parser、AST、diagnostics 和文件局部 symbol slice。
4. 结果只写 `results[fileIndex]`，不直接修改共享 workspace index。
5. coordinator 按 file index 合并 symbols/references；某文件失败只产生该槽位结果。
6. 每个任务检查 context 和 snapshot revision；shutdown 等待 worker 退出。

当 semantic index 需要跨文件引用时，可采用 `fileIndex + localIndex`，与 esbuild 的
`sourceIndex + innerIndex` 同理。先使用普通 slice，不提前实现 union-find 或二维
SymbolMap。

验收：同一输入在 `GOMAXPROCS=1/2/4` 下 diagnostics 与 index 序列化结果 byte-for-byte
一致；race test 通过；取消/关闭无 goroutine 泄漏；4 worker 加速和 RSS 都在固定
预算内。

### P7：immutable snapshot parse cache

状态：workspace/index 出现重复解析后实施。

缓存单位是完整纯语法结果，不是零散 token。key 至少包含 content identity/revision、
dialect trigger 和所有语法选项。进程内 cache 的 parser code identity 由当前二进制
隐式保证；只有将来做持久 cache 时才显式加入 grammar/build identity。target-version
compatibility diagnostics 与 syntax diagnostics 分离，使配置变化可复用 syntax AST。

缓存查找/写入用短临界区，parse 在锁外。同一 key 的并发 miss 可以先容忍重复工作；
只有 profile 证明值得时才加 singleflight。缓存值发布后不可被 semantic pass 原地
修改，逐出受文件数和总估算 bytes 限制。

验收：配置-only reanalysis 不重新 parse；命中重放相同 syntax diagnostics；修改任一
key 因素必定 miss；内存上限和关闭路径有测试。

### P8：AST 紧凑化和 pass 合并

状态：最后考虑。

可调查项：

- 4 MiB 文件上限下，`Span` 是否可安全改为两个 `uint32`，并在协议边界转 `int`。
- `Expression` 的 lambda/type/parameter 等冷字段是否应只存在于对应 typed payload。
- `ExpressionMissing` 等无状态 payload 是否可共享。
- declarations、references、imports 是否适合作为文件级 side table，在已有 syntax walk
  中顺便收集。
- semantic binding/type checking 哪些必须分 pass，哪些可以在同一 walk 收集数据。

每项都必须先用 `unsafe.Sizeof` 报告、heap profile 和真实 AST 数量估算收益。Go
interface variant、更多小指针和 side table 也可能增加分配，因此不能假定“拆类型”
一定更紧凑。

## 明确不照搬的内容

- 不使用 `LexerPanic` 在第一个语法错误后放弃整文件；编辑器必须 loose recovery。
- 不为每个 workspace 文件无界启动 goroutine。
- 不宣称 parser 全程无锁；共享 cache、logger 和最终 index 需要窄而明确的同步。
- 不把 esbuild 的 JavaScript/TypeScript 合并方式套到 legacy/Vim9。Vim9 触发、空白、
  comment、continuation 和 Ex command 规则决定它们是两套语言工具。
- 不复制 JS 的 regex/JSX/ASI lexer mode 或 TypeScript 任意回溯；只实现 Vim 源码要求
  的歧义。
- 不把“只有三个 full-AST passes”当目标数字；pass 数由 Vim 语义依赖决定。
- 不在 profile 前引入 arena、对象池、`unsafe` 字符串、packed union 或自定义 allocator。
- 不为了避免一个临时对象引入 Manager、Factory、Strategy、Adapter 或通用 parser
  framework。

## 固定验证协议

### Correctness gate

依赖已由本地构建预下载并进入 Go module cache 后，每个优化至少运行：

```sh
GOPROXY=off GOSUMDB=off go test -mod=readonly ./internal/syntax
GOPROXY=off GOSUMDB=off go test -mod=readonly ./...
```

并检查：

- 固定 v9.2.1015 official corpus；
- legacy 与 Vim9 focused syntax/recovery matrix；
- heredoc、embedded Ex、autocmd、mapping、substitute、continuation；
- malformed source 不 panic、不无限循环，后续独立行仍解析；
- 本地 RTP audit 只作为临时外部验证，不提交绝对路径测试。

### Parser benchmark

当前有两层永久 benchmark：

- `internal/syntax/benchmark_test.go`：固定小输入的 parser/expression regression；
- `tools/benchlegacy`：独立 module 的本地 RTP legacy 对照，通过临时 `go.work` 引入
  go-vimlparser，不污染 production dependencies。

RTP harness 预先读取并排序输入，使计时区间只包含 parser 必需工作。worker 数是
harness 参数，不是单文件 `syntax.Parse` 参数。每次固定记录 manifest/content hash、
文件数、总 bytes、Go 版本、OS/CPU、两个 checkout identity 和 parser options。每组先
warm up 2 次，再独立运行至少 20 次，报告 median/p95：

```text
workers:       1, 2, 4
metrics:       wall time, ns/byte, bytes/op, allocs/op, GC count, max RSS
profiles:      CPU, alloc_space, inuse_space
```

单 worker 分配用 Go benchmark/runtime metrics；多进程 max RSS 在 macOS 用
`/usr/bin/time -l`，Linux 用 `/usr/bin/time -v`。每轮明确记录是 cold filesystem cache
还是 warm filesystem cache，并在测量前固定 `GOMAXPROCS` 和 GC 设置。

完整 workspace scan 另测 discovery/I/O/parse/merge 四段，不能再用“2911 文件总耗时”
推断 parser 本身的瓶颈。

外部基线必须同时记录成功/失败和产出量。go-vimlparser 只支持 legacy 且遇错即停，
所以全 RTP 耗时不能与 loose parser 直接排名；主排名只用预先固定的共同成功 legacy
集合，完整 legacy 集合另报 vimls-go recovery 吞吐。不同 AST 的 node count 不是语义
等价证明，但应至少确认 command、expression、comment 和保留 span/trivia 的工作量没有
数量级缺失。

### 保留性能改动的门槛

- correctness 和 deterministic output 必须零回归。
- 常规优化应使主目标在重复测量中改善至少 5%，且其他核心指标无超过 3% 的稳定
  回退。
- 小于 5% 的改动只有在删除明确的 top flat allocation、简化代码或解除后续结构
  阻塞时保留，并在变更记录中说明。
- 并行优化同时看吞吐、p95 latency、RSS、取消延迟和后台稳定性，不能只看最快一次。

## 实施顺序

```text
P0 recovery/correctness gate
  -> P1 scoped-copy cleanup
  -> P2 reuse boundary AST
  -> re-profile
  -> P3 cursor expression lexer and P4 one-walk consumers in measured order
  -> re-profile
  -> P5 logical mapping only if hot
  -> P6 bounded workspace batches
  -> P7 immutable parse cache
  -> re-profile
  -> P8 AST/pass compaction only if hot
```

每完成一阶段都更新本文的当前基线和 profile，不跨阶段预建抽象。P2 已消除普通
单表达式命令中“先切 argument、后解析 details”的重复，P3 已删除逻辑表达式的完整
token slice。P4 已迁移 expression list、声明 RHS、`for` iterable、Vim9 command-start
call/assignment RHS、typed-declaration continuation probe、两种方言的 `put =expr`、
`global`/`vglobal` 和 `folddoopen`/`folddoclosed` embedded Ex payload。新 profile 的第一
flat allocation 仍是 `scanCommands`；其中 command metadata lookup 的 CPU 热点已通过
保持 table 顺序的首字节区间消除，而按 source bytes 猜 command capacity 的旧实验已证明
不值得保留。`filter[!]` 的 prefix regexp 与 substitute command payload 已结构化且保持
冷字段按需分配。下一步按真实 RTP 的不透明 command 分布优先处理高价值 command-specific
缺口；只有新的 profile 证明 logical mapping 成为热点时才进入 P5。
