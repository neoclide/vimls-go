# Architecture plan

## Constraints

- One portable Go executable using stdio by default, with a single-session TCP
  listener for debugging.
- LSP 3.18 and JSON-RPC 2.0 at the wire boundary.
- Legacy Vim script and Vim9 script, including local dialect switches.
- Minimum Vim 9.1 with explicit gates for later syntax.
- Safe static analysis: the server never sources workspace scripts.
- Fast feedback for incomplete files without a framework-sized foundation.

## Data flow

An input frame is decoded into an LSP request. Document notifications create an
immutable text snapshot. The snapshot is parsed into a recovering syntax tree,
analyzed into symbols/types/diagnostics, and then atomically published only if
the document version is still current. Feature handlers query that same snapshot
and analysis result.

No syntax or analysis package knows about JSON-RPC, goroutines, stdout, or an
editor process.

## Initial package map

Create a package only when its milestone starts:

```text
cmd/vimls/          process entry point
internal/jsonrpc/   bounded Content-Length stream adapter
internal/text/      immutable snapshots, changes, line and UTF indexes
internal/syntax/    dialect context, lexer, AST, parser, recovery
internal/analysis/  scopes, symbols, references, types, diagnostics
internal/workspace/ open documents, disk files, imports, incremental index
internal/server/    lifecycle and feature handlers
internal/vimdata/   versioned commands, builtins, options and help metadata
```

Use the vendored v1 releases of `go.lsp.dev/jsonrpc2` for the connection state
machine and `go.lsp.dev/protocol` for generated LSP 3.18 types and dispatch. The
repository retains only a thin stream adapter because the application requires
an 8 KiB header and 16 MiB message bound, stricter than the library default.
The parser and analyzer remain repository-owned Go code.

## Text and positions

A `Snapshot` contains URI, an internal monotonically increasing source revision,
an optional LSP client version for open documents, raw text, and line bounds.
Syntax spans use half-open byte offsets. At the LSP
boundary, positions are converted using the encoding negotiated during
`initialize`; UTF-16 is the compatibility fallback.

Changes are applied in notification order to the previous open-document
snapshot; client-version ordering is enforced only for open LSP documents.
Analysis captures a snapshot value and runs without holding the document-store
lock. A result is installed only if URI and internal revision still match. Start
with complete string snapshots; introduce a rope or piece table only after a
benchmark proves the copy cost matters.

## Parser shape

Vim script is an Ex-command language with context-sensitive command payloads.
Use two cooperating parsers:

1. A line/command parser recognizes ranges, modifiers, command names and
   abbreviations, bang/count/register arguments, separators, comments,
   continuation, heredocs, and dialect-changing commands.
2. A Pratt expression parser handles precedence and dialect-specific literals,
   operators, calls, indexing, slicing, lambdas, types, and method chains.

Known structural commands create typed AST nodes. Unknown user commands and
payloads that cannot be safely parsed create `UnknownCommand` nodes containing
their exact spans and tokens. Recovery synchronizes at a valid command boundary,
block terminator, or newline that is not continued.

The production grammar is derived from the pinned Vim `v9.2.1015` source and
tests. Target-version compatibility is a later diagnostic pass over that latest
syntax tree; it does not make the parser repeatedly rediscover command grammar
from older releases.

### Esbuild study and parser refactoring target

The parser design was checked against the local esbuild source at
`/Users/chemzqm/lib/esbuild`, commit
`f6058f8364fe7ab91ca57a83e02577ed74c9cae4`. The transferable part is its
cursor-driven parsing and ownership model, not its JavaScript grammar or error
policy. The source evidence, measured baseline, prioritized implementation
phases, and rejection list are maintained in the canonical
[esbuild optimization study](../esbuild.md).

vimls-go already has separate legacy and Vim9 command-argument consumers,
immutable input strings, byte spans, and compressed mappings for transformed
logical views. A transformed view uses compressed segments for continuations
and a local one-logical-byte-to-multiple-source-byte segment when Ex
preprocessing contracts source bytes. Ordinary single-expression commands and
expression-list commands
reuse the AST produced while finding their boundary. Declaration initializer
RHS and `for` iterables do the same while their targets/bindings retain dedicated
grammars. Vim9 word-start calls reuse the complete expression, while word-start
assignments reuse only the RHS so the dedicated LHS and operator grammar remains
authoritative. Typed-declaration continuation probing reads that same boundary
without consuming it. Legacy `put`/`iput` applies Vim's pre-expression Ex
delimiter pass: unescaped `|` and `"` end the command, while `\|` and `\"` lose
one backslash in a temporary view whose AST and diagnostic spans map exactly to
the original bytes. Its empty RHS remains a valid expression-register reuse.
Vim9 `put`/`iput` instead uses Vim9 expression, comment, separator, and automatic
continuation rules; an empty RHS is malformed. Logical projections use a
temporary `File`, and embedded Ex payloads intentionally parse their own nested
command list. `global`/`vglobal` first own their complete outer argument, then
scan the regexp delimiter with Vim's `skip_regexp_ex()` boundary rules and parse
only the trailing command payload as a nested list. `folddoopen` and
`folddoclosed` use the same nested-list mechanism, but their complete argument
is the payload and no regexp boundary is needed. Vim's recursive `do_cmdline()`
clears an outer one-command `legacy`/`vim9cmd` override, so these nested payloads
start in the enclosing script or function dialect; modifiers written inside the
payload still affect their own following command. Bars, quotes, and Vim9 `#`
bytes inside an outer regexp or payload are never exposed to the outer command
splitter. Previous-pattern forms (`\/`, `\?`, and `\&`), a missing closing
delimiter, and an empty default-print body remain structurally recoverable
without inventing a `global` embedded command. The `filter[!]` command modifier
similarly owns its regexp before normal command parsing resumes: it preserves
the optional force bang, delimiter, pattern, and `g`/`j`/`f` flags, including
Vim9 `#pattern#` where `#` is not a comment. An unfinished delimited pattern
owns the rest of that logical line and recovery restarts on the next physical
line. Filter-only regexp spans live behind a cold optional node so ordinary
modifiers do not pay for them. `substitute`, `smagic`, `snomagic`, and `~`
likewise use a command-owned scanner: regexp collections and escaped
delimiters cannot expose an outer bar, replacement escapes are consumed once,
and the optional flags/count tail is the only place where a following bar can
become an Ex separator. Legacy one-byte `:s` recognition mirrors Vim's narrow
`one_letter_cmd()` exceptions before ordinary abbreviation lookup, while Vim9
first recognizes complete `s:Func()`, `s[index]`, `s.member`, and `s->Method()`
expression shapes. A `\=expr` replacement is parsed once and shared with the
command expression list. Invalid separators, backslash forms, replacement
expressions, and trailing bytes retain the parsed prefix, own the rest of the
line, and recover on the next physical line. Substitute-only fields live behind
one cold optional command node. `highlight` likewise owns its boundary before
building typed list/query/clear/link/definition nodes. Vim's Ex splitter sees
an unescaped bar or legacy double quote even inside a highlight single-quoted
value, so the generic quote-aware opaque scanner is not used. Proven E412/E413/E415/
E416/E417/E475 structure errors retain the partial attributes, suppress a
same-line tail, and recover at the next physical line. Runtime group existence,
attribute-value validity, and future keys remain conservative; `ctermfont` is
preserved by the latest grammar and version-gated separately at Vim 9.1.0030.
Highlight-only fields live behind one cold optional command node. The following
seven `:syntax` item forms also own their boundary before the
generic Ex splitter: `keyword` treats bars and dialect comment bytes inside a
keyword token as payload, while `match` and `region` scan arbitrary regexp
delimiters, collections, escapes, offsets, and surrounding syntax options.
`cluster` preserves repeated `contains`/`add`/`remove` group lists, including
empty comma items that Vim accepts and counts for special-item ordering.
`case`, `conceal`, and `spell` preserve their optional case-insensitive mode
keyword. Vim's consumers validate only that first whitespace-delimited token
while `find_nextcmd()` independently selects the first bar, so intervening
bytes are ignored payload rather than a legacy/Vim9 comment.
`include` preserves its optional `@cluster` and full raw filename payload.
Because Vim adds `EX_XFILE | EX_NOSPC` inside that consumer, whitespace,
legacy `"`, Vim9 `#`, escaped bars, CTRL-V-protected characters, and
`` `=expression` `` payloads must be scanned before an Ex separator is chosen.
The syntax layer never expands the filename, evaluates the expression, or
sources the referenced script.
`clear` and explicit or implicit `list` preserve each group or `@cluster`
operand. Group existence is mutable Vim runtime state, so E28/E391/E392 are
not parser diagnostics; the later analysis layer may resolve them against a
versioned workspace snapshot.
`sync` preserves direct settings, line-continuation regexp delimiters, clear
operands, and the complete match/region structure. Its outer setting tokens
and `ccomment` group use Vim's whitespace-only boundary, so a bar already
inside one of those tokens is payload. `linecont` uses the shared Vim regexp
boundary scanner but does not consume syntax offsets. E403 and E394 depend on
mutable buffer state or regexp compilation and therefore are not emitted by
the stateless syntax parser.
`iskeyword` owns its complete remaining logical-line payload, including bars,
comment bytes, and trailing spaces; chartab validation remains runtime work.
`foldlevel` accepts only its case-insensitive `start`/`minimum` token and treats
all other trailing bytes as E390 recovery payload. The `on`, `enable`, `manual`,
`off`, and `reset` consumers preserve Vim's `set_nextcmd()` boundary: only an
immediately following bar or Vim9 `#` comment ends the command, while arbitrary
trailing bytes are ignored. Parsing these modes never sources Vim runtime
scripts or changes syntax state.
`set`, `setlocal`, and `setglobal` also own a tentative escaped Ex boundary
before constructing option details. Each item retains its complete span plus
prefix, name, operator, and value spans. Legacy permits whitespace before an
operator; Vim9 reports proven E1205 forms and an unterminated angle option as
E474. A proven structural error owns the rest of its physical line and resumes
at the next line. Unknown option names and value validity remain runtime or
versioned-analysis concerns, so the syntax parser does not invent diagnostics
for them.
Malformed items retain the group and every completed option/pattern, suppress
the remaining same-line tail, and resume on the next physical line. Unknown or
future `:syntax` subcommands remain opaque instead of producing a static error.
Syntax-only fields live behind one cold optional command node. The following
items are refactoring targets, not claims about the current implementation:

- A legacy or Vim9 command consumer owns both its payload boundary and AST
  construction. A generic pass must not first split an argument and then ask a
  second parser to rediscover command-specific separators.
- Legacy Vim and Vim9 expose independent parsers and command consumers. Neutral
  byte cursor, span, token, AST, and Pratt machinery may be shared, but comment,
  continuation, expression, and recovery decisions are explicitly selected by
  dialect. Expression-name scanning and Ex command-name scanning remain
  distinct: Vim9 recognizes a full variable or call before ASCII Ex lookup.
- Syntax values retain source spans and decode semantic values only on demand.
  The ordinary legacy identity path already reads the source directly, and
  transformed logical views use compressed segments. The legacy autocmd view
  now copies only its normalized body instead of the whole file. Any remaining
  projection is removed only with benchmark and span-equivalence evidence.
- Construct each ordinary command and expression tree once, then reuse it in
  syntax normalization and semantic passes. Nested embedded command lists are a
  deliberate separate parse scope. Any other repeated parse must correspond to
  a demonstrated ambiguity and remain visible in allocation profiles.
- Speculative parsing is local and explicit. Copy and restore only the required
  scanner state; suppress diagnostics only for a speculative path that may
  fail. Do not clone a whole parser for ordinary command disambiguation.

### Loose recovery contract

esbuild deliberately aborts parsing one file after a hard lexer/parser error by
raising and catching `LexerPanic`. That policy is unsuitable for an editor.
vimls-go treats malformed source as data and must still reach later declarations:

- A failed command retains its parsed prefix and original argument span, reports
  a local diagnostic, and always advances the cursor.
- A command-owned unescaped `|` is a separator only while the command prefix is
  syntactically valid. After a confirmed source error, the remainder of that
  physical line is not interpreted as another command; parsing restarts at the
  next physical line.
- Explicit legacy `\` continuation and valid Vim9 automatic continuation are
  assembled before deciding that the logical statement is malformed. This
  preserves valid multiline expressions while still making the next physical
  statement the primary recovery boundary.
- Heredoc terminators, embedded Ex body ranges, braces, and dialect-specific
  block terminators are trusted structural boundaries. Embedded command lists
  own their block state and cannot leak it into the containing list.
- Vim9 `:command {}` collection owns its physical closing boundary before any
  stored payload is executed. The first line whose first non-white byte is `}`
  closes the definition and remains part of Vim's replacement text; a deferred
  heredoc, text body, or `loadkeymap` payload must not consume later source past
  that same structural byte.
- Source diagnostics never use panic for control flow. Panics represent parser
  defects and must fail tests.

### Parallel parsing

The current server parses immutable open-document snapshots with at most
`min(GOMAXPROCS, 4)` analysis workers and rejects stale results by document
revision. Workers currently publish each document independently; there is no
cross-file merge barrier. A future workspace batch parser should keep the same
bound, have each worker own its AST and diagnostics, write into a pre-indexed
result slot, and merge workspace symbols in stable input order. Parser workers
must not mutate the shared workspace index directly.

Command and builtin metadata is versioned. Start with a small reviewed table.
If a Go generator becomes justified, its manifest must pin the upstream Vim tag
and commit, `go generate ./internal/vimdata` must reproduce committed output,
and CI must fail on a generated diff. The server never requires Vim at runtime.

## Analysis passes

Run a small fixed sequence rather than a plugin pipeline:

1. Collect declarations and create lexical/script/class scopes.
2. Resolve imports and references against the file and workspace index.
3. Infer/check Vim9 types and conservatively classify legacy values.
4. Produce diagnostics with stable codes and syntax-backed ranges.

An `unknown` result suppresses claims that require more information. Dynamic
commands may contribute heuristic completion candidates, but never authoritative
rename edits or undefined-symbol errors.

The workspace index replaces one file's declarations atomically. It initially
indexes open documents, workspace Vim files, and explicitly configured
runtimepath roots; it does not scan the entire machine by default.

## Protocol lifecycle

- Before `initialize`, accept only the requests permitted by LSP.
- Negotiate position encoding and workspace folders, and advertise only
  implemented capabilities.
- Apply `didOpen`, ordered `didChange`, `didSave`, and `didClose` to snapshots.
- Resolve `vimls.targetVersion` using the language-support precedence rules;
  configuration revisions participate in stale-result checks.
- Associate cancellation with request context. Cancellation is not a protocol
  error and cannot publish partial or stale results.
- After `shutdown`, reject ordinary requests and wait for `exit`.
- Write only framed protocol messages to stdout; diagnostics and logs use
  stderr.
- Default to one stdio session. `--listen <host:port>` accepts one TCP session
  for debugging, reports an ephemeral port on stderr, and shares the same
  protocol stack, lifecycle, limits, and shutdown path.

Use direct handler methods on one concrete `Server`. Do not add a dispatcher,
service container, provider registry, or background-job framework until real
code demonstrates the need.

## Concurrency and bounds

- Serialize document mutations per URI; parse immutable versions concurrently.
- Cancel superseded analysis and check version again before publishing.
- Enforce the resource defaults below before allocating or scheduling work.
- Avoid a goroutine per token, symbol, or completion item. Every background
  goroutine has an owning context and shutdown path.

| Setting | Default | Exceeded behavior |
| --- | ---: | --- |
| `limits.maxHeaderBytes` | 8 KiB | Log a framing error and close the stream |
| `limits.maxMessageBytes` | 16 MiB | Log a framing error and close the stream because resynchronization is ambiguous |
| `limits.maxFileBytes` | 4 MiB | Keep document sync, skip analysis, and publish one `vimls/file-too-large` diagnostic |
| `limits.maxPendingRequests` | 128 | Reject another request with JSON-RPC server error `-32000` |
| `limits.maxParallelAnalysis` | min(`GOMAXPROCS`, 4), at least 1 | Queue work up to the pending-request limit |
| `limits.maxDiagnosticsPerDocument` | 200 | Truncate deterministically and append `vimls/diagnostics-truncated` within the cap |
| `limits.maxCompletionItems` | 200 | Return the first deterministic page with `isIncomplete = true` |
| `limits.maxWorkspaceFiles` | 20,000 | Stop deterministic discovery and send one workspace warning |
| `limits.maxIndexBytes` | 256 MiB | Stop adding new files and send one workspace warning |

Negative, non-decimal, malformed, or oversized `Content-Length` is a framing
error and closes the stdio session. JSON-RPC batch arrays are rejected as an
invalid request; vimls-go does not advertise a batch extension.

## Security

- Never source, compile, or invoke workspace Vim scripts in the server process.
- Normalize and validate file URIs before disk access; do not follow workspace
  imports outside configured roots without an explicit setting.
- Treat text, JSON, command names, modelines, help tags, and runtimepath entries
  as untrusted input.
- Do not expose environment values in hover text, logs, or diagnostics.

## Decisions deferred until evidence exists

- Persistent on-disk index.
- Rope/piece-table storage.
- Embedded-language delegation.
- Plugin-specific client extensions.

## Primary references

- [Language Server Protocol 3.18](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/)
- [JSON-RPC 2.0](https://www.jsonrpc.org/specification)
- [Vim 9.1 release](https://www.vim.org/vim-9.1-released.php)
- [Official Vim9 reference](https://github.com/vim/vim/blob/v9.1.0000/runtime/doc/vim9.txt)
