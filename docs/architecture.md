# Architecture

## Constraints

- Ship one portable Go executable. Stdio is the default transport; a
  single-session TCP listener exists only for debugging.
- Keep stdout reserved for LSP frames and write logs to stderr.
- Implement LSP 3.18 and JSON-RPC 2.0 at the wire boundary.
- Parse legacy Vim script and Vim9 script through independent root parsers.
  Retain cross-dialect constructs with loose recovery without requiring an
  exhaustive matrix of mixed combinations.
- Use Vim `v9.2.1015` as the production grammar source and Vim 9.1 as the
  compatibility floor.
- Analyze untrusted source without executing or sourcing it.
- Recover through incomplete editor input without unbounded work.

## Package boundaries

```text
cmd/vimls/          process entry point
internal/jsonrpc/   bounded Content-Length framing and request lifecycle
internal/text/      immutable snapshots, edits, lines, and position indexes
internal/syntax/    dialect context, tokens, AST, parser, and recovery
internal/analysis/  scopes, symbols, references, types, and diagnostics
internal/workspace/ documents, discovery, imports, batches, and indexes
internal/server/    LSP lifecycle and capability handlers
internal/vimdata/   versioned commands, builtins, options, and help metadata
```

Dependencies point from the server toward smaller packages. Syntax and analysis
do not depend on transport, goroutines, editor processes, or mutable workspace
state. Create a package or interface only when a current implementation needs
the boundary.

`go.lsp.dev/jsonrpc2` supplies connection state and `go.lsp.dev/protocol`
supplies generated LSP types. Their versions are pinned in `go.mod` and loaded
from Go's module cache; the repository has no `vendor/` tree. The project owns a
small stream adapter because its framing limits are stricter than the library
defaults.

## Text and positions

A `Snapshot` contains a URI, an internal monotonically increasing revision, an
optional client version, immutable text, line indexes, and a SHA-256 content ID
of the complete source bytes. The content ID excludes URI, version, revision,
and configuration. Changes apply in notification order to the previous open
snapshot and produce another complete immutable string. Analysis captures a
snapshot without holding the document-store lock and may publish only while
that snapshot and its configuration revision remain current.

Syntax spans are half-open byte offsets into the original source. Tokens retain
trivia and physical newlines. Only the LSP boundary converts positions to the
negotiated encoding; UTF-16 is the fallback. Keep complete string snapshots
until a measured edit benchmark justifies a rope or piece table.

## Parser design

Vim script is an Ex-command language whose payload grammar depends on the
resolved command and dialect. Legacy and Vim9 therefore have independent
command consumers. They may share byte cursors, spans, neutral AST types, and
Pratt mechanics, but comment, continuation, expression, and recovery decisions
remain dialect-specific.

The parser follows these rules:

- Derive production grammar from the pinned Vim `v9.2.1015` source, tests, and
  runtime help. Target-version checks run later over the latest syntax tree.
- Let each known command own both its argument boundary and detailed AST.
  A generic pass must not split a payload before regexp, mapping, substitute,
  `:syntax`, `:set`, heredoc, or embedded-command rules are known.
- Parse ordinary expressions with a cursor-driven Pratt parser. Reuse the AST
  produced while finding a boundary; parse a second time only for a deliberate
  nested command scope or a proven ambiguity.
- Preserve source spans and raw values. Decode semantic values only when an
  analysis consumer needs them.
- Read ordinary physical lines directly from the source. Continuations use a
  compact logical view whose segments map every result back to original bytes.
- Keep unknown user or future commands as opaque syntax. Unknown runtime option,
  group, file, or command state is not a parser error.

The parser never expands paths, evaluates backtick expressions, compiles regexps,
or sources runtime files. Those operations depend on mutable editor state or can
execute user code.

## Incremental editing and parser cache

Incremental editing means composing LSP text changes and invalidating derived
state; it does not splice or rebase AST nodes. A cache miss always passes the
complete new source to `syntax.Parse`, including for a one-character edit.

The open-document parser cache is keyed by URI and accepts a hit only when both
the snapshot content ID and `syntax.File.Source` exactly match. This permits a
same-content, newer revision to reuse one immutable parser tree while making a
hash collision harmless. Parsing runs outside server locks, and a miss is
installed only if the same snapshot is still current. Configuration and
workspace revisions are deliberately absent from this pure parser key.

Cached files contain only parser-owned syntax and parser diagnostics. Analysis
copies the `File` header and diagnostics slice before adding compatibility,
semantic, import, workspace, truncation, or publication diagnostics; those
results never mutate the cached tree.

## Recovery

- A malformed command retains its parsed prefix and original spans, reports a
  local diagnostic, and always advances.
- After a proven syntax error, the remainder of that physical line is not
  reinterpreted as another command. Parsing resumes on the next physical line.
- Valid legacy `\` continuation and Vim9 automatic continuation are assembled
  before the statement is judged malformed.
- Heredoc terminators, text-body boundaries, embedded Ex ranges, braces, and
  dialect-specific block terminators remain trusted synchronization points.
- Nested command lists own their own block state and cannot leak it outward.
- Source diagnostics never use panic for control flow. A panic is a parser bug
  and must fail a test or fuzz target.

## Analysis and workspace

Analysis uses a fixed sequence rather than a plugin pipeline:

1. Collect declarations and lexical, script, and aggregate scopes.
2. Resolve imports and references against the immutable file/workspace view.
3. Infer Vim9 types and conservatively classify legacy values.
4. Produce diagnostics with stable codes and syntax-backed ranges.

`unknown` suppresses claims that require more information. Dynamic commands may
offer heuristic completion candidates, but never authoritative rename edits or
undefined-symbol errors.

Open-document analysis and `workspace.ParseSources` use a bounded worker pool of
at most `min(GOMAXPROCS, 4)`. Each worker owns its AST and diagnostics and writes
one pre-indexed result slot, so one file failure does not corrupt another result.
Future cross-file semantic merging must consume those slots in stable input order
instead of letting parser workers mutate the shared index.

The workspace indexes open documents, workspace Vim files, and configured or
locally discovered Vim runtime roots. When initialization does not provide a
runtimepath, it checks only the conventional installation directories for the
host operating system and uses the newest runtime in the first match. It starts
no Vim process and uses no environment fallback; an explicit empty runtimepath
also disables this lookup. Canonical realpath keys deduplicate aliased roots and
files without changing runtime lookup precedence. The server never scans the
entire machine.
The language client owns filesystem watching: after initialization the server
dynamically registers `**/*.vim` watchers when supported, and consumes the
resulting `workspace/didChangeWatchedFiles` notifications. Runtimepath is
initialized through the option or local discovery and replaced by the custom
`vimls/didChangeRuntimepath` notification.
Generated command and builtin metadata pins its upstream Vim tag and must be
reproducible without requiring Vim at server runtime.

Every published workspace state has an identity consisting of its generation,
index instance and revision, and import-graph revision. Cross-file source,
symbol facts, and graph edges used by one analysis are copied under the same
workspace lock. Background work drops and requeues a stale result; foreground
workspace requests retry once and return `protocol.ErrContentModified` if the
replacement state also changes. Rebuilds additionally validate the complete set of open
snapshot pointers before publication, and a delayed close restore cannot
replace a reopened overlay.

## Protocol lifecycle

- Before `initialize`, accept only methods allowed by LSP.
- Negotiate position encoding and advertise only implemented capabilities.
- Apply `didOpen`, ordered `didChange`, `didSave`, and `didClose` to snapshots.
- Include target-version configuration revisions in stale-result checks.
- Associate cancellation with request context; canceled work cannot publish.
- After `shutdown`, reject ordinary requests and wait for `exit`.
- Stdio and TCP share framing, lifecycle, bounds, and shutdown behavior.

Use direct methods on one concrete `Server`. Do not add a dispatcher, service
container, provider registry, or background-job framework without current code
that needs it.

## Resource bounds

| Setting | Default | Exceeded behavior |
| --- | ---: | --- |
| `limits.maxHeaderBytes` | 8 KiB | Log a framing error and close the stream |
| `limits.maxMessageBytes` | 16 MiB | Log a framing error and close the stream |
| `limits.maxFileBytes` | 4 MiB | Keep sync, skip analysis, publish one file-too-large diagnostic |
| `limits.maxPendingRequests` | 128 | Reject another request with JSON-RPC server error `-32000` |
| `limits.maxParallelAnalysis` | min(`GOMAXPROCS`, 4), at least 1 | Queue bounded work |
| `limits.maxDiagnosticsPerDocument` | 200 | Truncate deterministically within the cap |
| `limits.maxCompletionItems` | 2,000 | Return a deterministic bounded result |
| `limits.maxWorkspaceFiles` | 20,000 | Stop discovery and send one warning |
| `limits.maxIndexBytes` | 256 MiB | Stop adding files and send one warning |

Malformed or oversized `Content-Length` closes the stream because byte framing
cannot be safely resynchronized. JSON-RPC batch arrays are rejected; vimls-go
does not advertise a batch extension.

## Performance discipline

- Use only the pinned official Vim `v9.2.1015` runtime for routine parser A/B.
- For the incremental-edit baseline, run each fixed fixture for exactly 100
  operations with `-benchmem -benchtime=100x -count=1`; report time, bytes/op,
  and allocs/op. Allocation metrics describe allocation pressure, not peak live
  heap or process RSS.
- Broader comparative studies may use five identical samples and report the
  median, with roots, ordering, warmups, `GOMAXPROCS`, and worker count fixed.
- Profile only parser work before selecting an optimization. Preserve complete
  AST, spans, trivia, diagnostics, loose recovery, and deterministic ordering.
- Do not guess slice capacity from source bytes or physical line count. Do not
  introduce arenas, object pools, `unsafe` storage, packed unions, or generic
  parser abstractions without stronger measured evidence than their complexity.
- Personal runtimepaths are occasional correctness smoke inputs, not performance
  gates.

## Security

- Never source, compile, or invoke workspace Vim scripts in the server process.
- Normalize and validate file URIs before disk access; do not follow imports
  outside configured roots without an explicit setting.
- Treat source text, JSON, command names, modelines, help tags, and runtimepath
  entries as untrusted.
- Do not expose environment values in hover text, logs, or diagnostics.

## Deferred decisions

- Persistent on-disk index.
- Rope or piece-table text storage.
- Embedded-language delegation.
- Plugin-specific client extensions.

## Primary references

- [Vim v9.2.1015 source and tests](https://github.com/vim/vim/tree/v9.2.1015)
- [Vim v9.2.1015 Vim9 reference](https://github.com/vim/vim/blob/v9.2.1015/runtime/doc/vim9.txt)
- [Vim v9.1.0000 compatibility baseline](https://github.com/vim/vim/tree/v9.1.0000)
- [Language Server Protocol 3.18](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/)
- [JSON-RPC 2.0](https://www.jsonrpc.org/specification)
