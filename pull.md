# Document Pull Diagnostics Plan

## Goal

Add LSP 3.18 document pull diagnostics without changing the parser or analysis
contracts. Clients that advertise `textDocument.diagnostic` use
`textDocument/diagnostic`; older clients keep the existing
`textDocument/publishDiagnostics` behavior.

The first milestone deliberately excludes `workspace/diagnostic`.

## Protocol contract

The implementation follows the LSP 3.18 pull diagnostics specification:

- <https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_diagnostic>
- <https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#workspace_diagnostic>
- <https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#diagnostic_refresh>

When the client provides `textDocument.diagnostic`, advertise:

```json
{
  "diagnosticProvider": {
    "interFileDependencies": true,
    "workspaceDiagnostics": false
  }
}
```

Use static `DiagnosticOptions`; do not dynamically register the capability.
Do not advertise work-done progress and do not emit partial results. A request
containing progress tokens still receives one final response.

The document response is one of:

```json
{
  "kind": "full",
  "resultId": "vimls-diagnostic-1",
  "items": []
}
```

```json
{
  "kind": "unchanged",
  "resultId": "vimls-diagnostic-1"
}
```

Return `unchanged` only when the request's `previousResultId` matches a result
issued by this server for the current immutable document, configuration, and
workspace state. Otherwise return `full`. Full reports, including empty ones,
always contain a result ID.

`relatedDocuments` is omitted in this milestone. Existing
`Diagnostic.relatedInformation` remains supported and is negotiated from the
pull diagnostic client capability in pull mode.

## Supported modes

Choose one diagnostic transport for each client connection:

| Client capabilities | Advertised capability | Server behavior |
| --- | --- | --- |
| `textDocument.diagnostic` present | `diagnosticProvider` | Pull only; do not send `publishDiagnostics` |
| `textDocument.diagnostic` absent | none | Preserve the existing push behavior |

Background analysis still runs for pull clients because open-document overlays,
the workspace index, the import graph, and dependent reanalysis require it.
Only the push notification is suppressed.

Do not add `workspace/diagnostic` to the implemented-method allowlist. An
unexpected workspace pull therefore continues to return MethodNotFound.

## Package boundaries

The implementation belongs in `internal/server`:

- `internal/server/server.go`: capability negotiation, method allowlist,
  shared analysis pipeline, push/pull transport selection, and server state.
- `internal/server/config.go`: select pull or push diagnostic client
  capabilities for related information.
- `internal/server/diagnostics_pull.go`: the document diagnostic handler,
  result cache, full/unchanged construction, and refresh coalescing.
- `internal/server/workspace.go`: request a diagnostic refresh only at existing
  workspace rebuild completion boundaries.

Keep these packages unchanged:

- `internal/syntax`
- `internal/analysis`
- `internal/text`
- `internal/workspace`
- `internal/jsonrpc`

The pinned `go.lsp.dev/protocol v1.0.1` already contains
`DocumentDiagnosticParams`, `DocumentDiagnosticReport`, `DiagnosticOptions`,
`DiagnosticRefresh`, and the generated dispatch methods. No dependency change
is needed.

## Shared diagnostic pipeline

Refactor the body of the existing asynchronous `analyzeDocument` so both the
background worker and a pull request use one computation path. The shared path
must retain the current order:

1. Capture a `workspace.Analysis` containing the immutable text snapshot and
   configuration revision.
2. Reuse the parser-only snapshot cache.
3. Run `analysis.Analyze` outside server locks.
4. Install the open-document syntax and semantic state in the workspace index.
5. Add autoload, import/member, user-command, and global diagnostics.
6. Filter `disabledDiagnostics`.
7. Sort deterministically by byte span.
8. Apply `maxDiagnosticsPerDocument` after filtering.
9. Convert byte spans to negotiated protocol positions.
10. Apply protocol-only severity overrides, tags, and related information.

The pure conversion from `[]syntax.Diagnostic` to `[]protocol.Diagnostic` must
be shared by push and pull. Do not move protocol types into syntax or analysis.

For pull clients, background analysis should populate the current pull cache
but must not call `PublishDiagnostics`. This lets a pull arriving after normal
background analysis return immediately.

## Cache identity

Use one result per URI, protected by the existing `publishMu`:

```go
type pullDiagnosticKey struct {
    snapshot       *text.Snapshot
    configRevision uint64
    workspace      workspaceIdentity
}

type pullDiagnosticResult struct {
    key      pullDiagnosticKey
    resultID string
    items    []protocol.Diagnostic
}
```

The snapshot pointer identifies the exact immutable open-document state.
`configRevision` prevents a result produced under previous diagnostic settings
from becoming current. `workspaceIdentity` covers workspace generation, index
instance and revision, and import-graph revision.

Use a server-local monotonic result ID such as `vimls-diagnostic-<n>`. Do not
encode paths, source, diagnostic JSON, or internal pointers in the ID.

Maps and diagnostic slices placed in the cache are immutable after
publication. Replace complete entries rather than mutating them.

Closing a document removes its cache entry. Reopening the URI must produce a
new full report even when the text is identical.

## Request flow

`Server.Diagnostic` performs the following steps:

```text
validate URI
  -> waitForWorkspaceIndex(ctx)
  -> begin analysis for the current open snapshot
  -> capture the current workspace identity
  -> current cache key + matching previousResultId: unchanged
  -> current cache key + different previousResultId: full cached result
  -> run the shared diagnostic computation
  -> convert to protocol diagnostics
  -> verify document, configuration, and workspace identity again
  -> install cache entry
  -> return full
```

If the URI is not open, return an empty full report without reading or
executing a file from disk. Closed-file diagnostics belong to the later
workspace-pull milestone.

Never hold a server lock during parsing, semantic analysis, filesystem I/O, or
a client request. Preserve the documented lock order:

```text
publishMu -> mu
publishMu -> workspaceMu -> analysisMu
publishMu -> analysisMu
watchMu -> mu
watchMu -> workspaceMu
```

If the document, configuration, or workspace identity becomes stale during a
pull, retry the complete operation once. A second stale result returns
`protocol.ErrContentModified`.

Error behavior remains consistent with existing accurate requests:

- client cancellation: `protocol.ErrRequestCancelled`
- stale document/workspace after one retry: `protocol.ErrContentModified`
- workspace index wait exceeding one second: RequestFailed with
  `workspace index did not become ready within 1s`
- server shutdown/cancellation: return a response; never leave the request
  hanging and never install stale cache data

## Diagnostic refresh

Read `workspace.diagnostics.refreshSupport`. When unsupported, never send
`workspace/diagnostic/refresh`.

Send one coalesced refresh after a server-side event can change diagnostics
without a corresponding current-document pull trigger:

- diagnostic settings actually change
- a complete workspace rebuild installs a new index/graph
- workspace folders, runtimepath, or external watched files cause such a
  rebuild to complete

Do not send a global refresh for `didOpen`, ordinary `didChange`, `didClose`, or
a debounce-only configuration change. `interFileDependencies: true` tells the
client to repull visible dependent documents after edits.

Use a generation counter and one in-flight flag. State changes increment the
generation. The refresh request runs outside locks. When it finishes, send at
most one further request if the generation advanced while it was in flight.
Refresh failures are logged and do not fail document pulls.

## Implementation phases

### Phase 1: capability and dispatch

- Record pull and diagnostic-refresh client capabilities during initialize.
- Conditionally advertise `DiagnosticOptions`.
- Add only `textDocument/diagnostic` to `implementedMethod`.
- Keep legacy push behavior unchanged.

### Phase 2: shared computation and conversion

- Extract the current diagnostic computation from `analyzeDocument`.
- Extract protocol conversion from `publishSyntax`.
- Prove push output remains byte-for-byte equivalent in existing tests.

### Phase 3: cache and pull handler

- Add the per-URI immutable cache and result counter.
- Implement full, cached full, and unchanged reports.
- Add exact stale checks and one retry.
- Remove cache state on close.

### Phase 4: refresh

- Add refresh capability negotiation.
- Add coalesced asynchronous refresh.
- Trigger it at configuration and completed-rebuild boundaries only.

### Phase 5: documentation and validation

- Document pull/push selection and the deferred workspace pull scope.
- Update architecture, diagnostics, roadmap, language support, and README only
  after the behavior is implemented.

## Test matrix

### Capabilities and routing

- Pull capability present: advertise `diagnosticProvider` with
  `interFileDependencies=true` and `workspaceDiagnostics=false`.
- Pull capability absent: omit it and preserve push.
- Pull client receives no `publishDiagnostics` after open/change/close.
- `textDocument/diagnostic` dispatches to the handler.
- `workspace/diagnostic` remains MethodNotFound.
- Invalid or missing document URI returns InvalidParams.

### Reports and cache

- First pull returns full with a non-empty result ID.
- Matching previous result ID returns unchanged.
- Unknown or stale previous result ID returns full.
- A current cached result can be returned without recomputation.
- Empty diagnostics can return full and then unchanged.
- didChange, diagnostic configuration change, workspace index replacement, or
  graph revision change produces a new full result and result ID.
- close/reopen cannot reuse the old result ID.

### Correctness and stale state

- A stale analysis cannot update the cache or return as current.
- First stale attempt can retry successfully.
- Two stale attempts return ContentModified.
- Cancellation returns RequestCancelled without a cache update.
- Pull waits for an active workspace rebuild.
- The existing one-second timeout and exact RequestFailed message are retained.

### Diagnostic contents

- Syntax, semantic, import, user-command, and global diagnostics match push.
- Disabled diagnostics are removed before truncation.
- Severity overrides affect only protocol output.
- Disabled settings take precedence over overrides.
- Related information follows pull client capability.
- Tags, file-too-large, and the deterministic truncation sentinel are retained.
- UTF-8, UTF-16, UTF-32, CRLF, BOM, combining-character, and astral-character
  positions remain correct.
- `relatedDocuments` is absent.

### Refresh and integration

- Diagnostic setting changes produce one refresh.
- Completed workspace rebuilds produce one refresh.
- Concurrent refresh triggers are coalesced without losing a later generation.
- Unsupported refresh clients receive no refresh request.
- Refresh failure only logs.
- A subprocess test covers initialize, no push, full, unchanged, didChange to
  full, configuration invalidation, refresh response, shutdown, and exit.
- The existing subprocess push scenario remains unchanged for legacy clients.

## Validation

Run:

```sh
gofmt -w <changed-go-files>
go test -mod=readonly ./...
go vet ./...
git diff --check
```

Request gopls diagnostics for every modified Go file. Do not run race tests or
collect coverage unless separately requested.

## Deferred workspace pull milestone

`workspace/diagnostic` requires a separate design for closed-file diagnostic
storage, `version: null`, workspace URI enumeration, `previousResultIds`,
partial streaming, cancellation, and document-pull precedence. Do not advertise
`workspaceDiagnostics: true` until those behaviors and integration tests exist.
