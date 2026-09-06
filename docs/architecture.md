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

Language rules come from the pinned
[Vim v9.2.1015 source and tests](https://github.com/vim/vim/tree/v9.2.1015).
Protocol behavior follows
[LSP 3.18](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/)
and [JSON-RPC 2.0](https://www.jsonrpc.org/specification).
Do not execute user scripts to make analysis more precise.
