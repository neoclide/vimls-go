# Client configuration

vimls-go is an stdio language server. Clients start `vimls`, send the normal
LSP `initialize` request, then send `initialized`. The server does not start
Vim, read vimrc files, or query a running editor.

## Initialize

The supported `initializationOptions` object is:

| Option | Type | Default | Behavior |
| --- | --- | --- | --- |
| `runtimepath` | string array | host Vim runtime discovery | Ordered runtime roots. An explicit empty array disables runtime indexing. Invalid, missing, unreadable, and duplicate realpaths are omitted. |

A complete initialization fragment is:

```json
{
  "workspaceFolders": [
    { "uri": "file:///project", "name": "project" }
  ],
  "initializationOptions": {
    "runtimepath": [
      "/usr/local/share/vim/vim92",
      "/project/.vim"
    ]
  },
  "capabilities": {
    "workspace": {
      "configuration": true,
      "didChangeWatchedFiles": {
        "dynamicRegistration": true,
        "relativePatternSupport": true
      }
    }
  }
}
```

Use URI-encoded absolute `file:` URIs for workspace folders and absolute
filesystem paths for runtimepath. `workspaceFolders` takes precedence over the
deprecated `rootUri` and `rootPath` initialize fields.

When file-watch dynamic registration is advertised, the server registers
`**/*.vim` watchers below active workspace roots only. Runtimepath roots are
never watched. The client owns the workspace watchers and sends
`workspace/didChangeWatchedFiles`; vimls-go does not poll or install an
operating-system watcher itself.

## Workspace indexing progress

If the client advertises `window.workDoneProgress`, vimls-go creates a standard
work-done token and sends `$/progress` `begin` and `end` notifications for each
full workspace index rebuild. The progress begins when the debounced scan
actually starts, not while changes are still being coalesced. Incremental
open-document indexing does not create progress. Clients that do not advertise
the capability receive no progress traffic.

## Workspace settings

If the client advertises `workspace.configuration`, vimls-go requests the
`vim` section after `initialized` and after a
`workspace/didChangeConfiguration` notification with null settings. The
section supports `workspaceRebuildDebounce`:

```json
{
  "workspaceRebuildDebounce": 100
}
```

Clients that include settings in the notification can send the namespaced
form directly:

```json
{
  "jsonrpc": "2.0",
  "method": "workspace/didChangeConfiguration",
  "params": {
    "settings": {
      "vim": {
        "workspaceRebuildDebounce": 100
      }
    }
  }
}
```

`workspaceRebuildDebounce` is dynamically configurable. Invalid updates retain
the previous valid value and produce a visible warning.

## Diagnostic policy

Diagnostic categories have stable default severities:

| Category | LSP severity | Configurable or disabled |
| --- | --- | --- |
| Parser and structural errors | error | fixed; cannot be disabled |
| Unresolved function, variable, import alias, or autoload name (`E117`, `E121`, `E1001`, `E1089`) | warning | fixed; cannot be disabled |
| Runtime-dependent or cross-file conflict warnings (`E122`, `E174`, `E464`, `E705`, `E707`) | warning | fixed; cannot be disabled |
| Unused Vim9 variables | hint with the LSP `unnecessary` tag | fixed; cannot be disabled |
| Deprecated Vim9 references | hint with the LSP `deprecated` tag | fixed; cannot be disabled |

Unknown diagnostics default to error.

## Custom request: `vimls/didChangeRuntimepath`

Runtimepath is not a workspace setting. A language client sends
`vimls/didChangeRuntimepath` when the editor's effective runtimepath changes.
This is a server request handled by vimls-go, not an LSP built-in method:

```json
{
  "jsonrpc": "2.0",
  "id": 42,
  "method": "vimls/didChangeRuntimepath",
  "params": {
    "runtimepath": [
      "/usr/local/share/vim/vim92",
      "/project/.vim"
    ]
  }
}
```

The request succeeds with JSON `null`. Notifications with the same method stay
supported for existing clients. The ordered runtime roots are atomically
replaced after canonicalization and de-duplication; invalid, missing,
non-directory, and unreadable roots are silently dropped. A no-op changes
nothing. Reordering only updates runtime lookup precedence and import
resolution. Added roots discover and analyze only previously unindexed files;
removed roots discard only files outside every workspace or retained runtime
root. File watchers cover workspace roots only; runtimepath changes do not
refresh watcher registration or trigger a watcher rebuild. Runtimepath files
change when the client sends this request (or an explicit watched-file event).
Discovery and read failures inside runtime roots are skipped silently as absent.
Send an empty array to disable runtime indexing.

Standard `workspace/didChangeWorkspaceFolders` changes workspace roots.
Standard `workspace/didChangeWatchedFiles` changes indexed `.vim` files after
the client reports created, changed, or deleted paths. Open document snapshots
always override disk content during these rebuilds.
