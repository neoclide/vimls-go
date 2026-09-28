# Finding your way around the code

This page is for contributors. For supported editor features and setup, start
with the [README](../README.md).

## Where things live

| Package | Responsibility |
| --- | --- |
| [cmd/vimls](../cmd/vimls) | Starts the language server. |
| [internal/jsonrpc](../internal/jsonrpc) | Reads and writes messages, tracks requests and cancellation. |
| [internal/text](../internal/text) | Stores document snapshots and converts text positions. |
| [internal/syntax](../internal/syntax) | Parses Legacy Vim script and Vim9, including unfinished input. |
| [internal/analysis](../internal/analysis) | Resolves scopes, symbols, references, types and diagnostics. |
| [internal/workspace](../internal/workspace) | Finds files and keeps import and symbol indexes. |
| [internal/server](../internal/server) | Implements editor features using those packages. |
| [internal/vimdata](../internal/vimdata) | Holds command, function, option and variable metadata. |
| [internal/vimhelp](../internal/vimhelp) | Reads Vim help and converts it for hover and completion. |

Dependencies flow from the server into the smaller packages. Parsing and
analysis do not depend on the editor process or transport. Prefer changing
the existing function or type before adding another layer.

The [architecture improvement plan](architecture-improvement-plan.md) records
the reviewed refactoring scope, sequence and validation requirements.

## From an edit to a result

An edit creates a new document snapshot. Parsing and analysis work from that
snapshot, and the server converts their byte ranges into the client's position
encoding before returning results.

Snapshots and installed indexes are immutable. Before accepting background work
or publishing a result, check that the document, configuration and consumed
workspace data are still current. An older analysis must never replace the
result of a newer edit. Unsaved editor text takes precedence over disk files.

Parsing the same content can reuse a cached result. Changed source is parsed
again in full: incremental text synchronization does not mean incremental AST
editing. Keep analysis-owned diagnostics separate from cached parser data.

## Why there are two parsers

Legacy Vim script and Vim9 have different comment, continuation and expression
rules. They use independent root parsers and share neutral syntax structures
and command data.

A command decides where its arguments end. Splitting a line on every bar or
quote would break mappings, patterns, heredocs and embedded commands. Keep
source spans and raw text until the relevant grammar is known.

`vim9script`, `scriptversion`, `def` and `function` affect the language
context. `vim9cmd` and `legacy` apply to the next command only. Mixed forms
must remain recoverable even where their full analysis is not implemented.

Malformed input should report a local problem and make progress to later code.
Unknown commands and embedded languages can be retained without interpreting
their bodies. A parser panic is a bug, not a way to report invalid source.

## Analysis and indexes

Analysis collects declarations, resolves references and derives the types it
can prove. Use `unknown` when runtime behavior could change the answer.
In particular, do not offer rename edits based only on a matching name.

[`analysis/scopes.go`](../internal/analysis/scopes.go) keeps the analysis model,
entry points and ordered phases. Declaration collection and reference binding
live in `scopes_declarations.go` and `scopes_references.go`. The
`scopes_*_diagnostics.go` files group checks by name, aggregate, control flow,
null receiver and type rules. File boundaries do not change phase order:
later checks can suppress or replace earlier diagnostics.

Workspace files and external runtime files are indexed separately. Runtime
updates retain data for unchanged roots. External symbols remain available for
completion and navigation, while workspace-symbol searches show only workspace
files. The scan scope is documented in
[language support](language-support.md#plugin-files-and-help).

The client supplies runtimepath. If none is usable at startup, the server may
query a clean Vim process for default directories. This loads no user scripts.
Help files are read in the background and cached; hover uses the available
cache instead of waiting for disk reads.

Only refresh features whose consumed data changed and whose client supports
refresh. Keep incomplete indexes distinguishable from complete results,
especially for references and reverse hierarchy queries.

Import processing follows these ownership boundaries:

| Owner | Responsibility |
| --- | --- |
| [analysis](../internal/analysis/import_types.go) | Single-file bindings and immutable imported/exported type facts, without workspace or process state. |
| [workspace](../internal/workspace/export_types.go) | Export inputs and derivation from indexed source. |
| [server import cache](../internal/server/import_types.go) | Per-parsed-document dependency identities, transitive derivation and invalidation. |
| [server diagnostics](../internal/server/import_diagnostics.go) | Combines import facts and current workspace data into document diagnostics. |

`snapshotFacts` loads the document's `importTypeCache`, which calls
`workspace.DeriveExportTypes` using analysis types. The cache is owned by the
parsed document, so editing or closing that document releases its source and
dependency tables. Before installing analysis, the server revalidates consumed
import facts and the document snapshot. Completion facts and full diagnostic
analysis have separate cache slots.

## Server state and background work

Within `internal/server`, `workspace.go` owns workspace builds and index/graph
installation. `runtimepath.go` handles runtime directory changes,
`file_watching.go` handles watched-file batches and registration, and
`workspace_progress.go` owns ordered progress notifications. Their methods
still share `Server`: splitting files does not make index, graph, resolver and
readiness state independently publishable.

The lock order in [server.go](../internal/server/server.go) is authoritative.
It is a partial order, not a list of locks that can all be acquired together.

| Lock | Protected work or state |
| --- | --- |
| `mu` | Protocol state, client capabilities, refresh state and diagnostic settings. |
| `publishMu` | Document cache installation and diagnostic/token result state. |
| `workspaceMu` | Workspace/runtime roots, index and graph identity, readiness and runtime help state. |
| `analysisMu` | Pending/running document analysis and worker admission. |
| `configurationMu` | Configuration request generation, cancellation and response application. |
| `watchMu` | Watch registration and watched-file batch admission. |

Workspace and runtimepath installations keep their existing cross-lock commit
boundaries. Runtimepath batches additionally serialize on `runtimepathRunMu`.
Shutdown closes admission and cancels `analysisContext`; `stopAnalysis` waits
for the analysis, workspace, runtimepath, help and watch worker groups, using
the existing admission barriers before waiting.

[`refresh.go`](../internal/server/refresh.go) shares one scheduling loop across
four fixed refresh kinds, each with its own generation and running state.
Client calls run outside server locks. Diagnostic refresh also requires pull
diagnostics, and workspace-triggered Code Lens refresh requires a complete
index. A waiting refresh coalesces later changes without blocking other kinds.

## Requests and shutdown

Keep stdout for protocol messages and logs on stderr. Use the pinned
`go.lsp.dev/protocol` types at the transport boundary.

Initialization and shutdown have ordering requirements even when ordinary
requests run concurrently. Cancellation belongs to a request; a cancelled
waiter must not cancel analysis shared by other callers. Shutdown cancels
background work and waits for it to exit before the process finishes.

The server bounds message sizes, file sizes, queued work and index size.
Most limits are internal constants, not user settings. Only expose a setting
when the server actually reads it. Oversized frames close the connection
because their message boundaries cannot be recovered safely.

## Checking a change

Follow [testing](testing.md) for commands and fixtures. Timing-sensitive tests
should use channels or barriers to force the relevant ordering.

Language rules come from the current pinned Vim source and tests identified in
[language support](language-support.md).
Protocol behavior follows
[LSP 3.18](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/)
and [JSON-RPC 2.0](https://www.jsonrpc.org/specification).
Do not execute user scripts to make analysis more precise.
