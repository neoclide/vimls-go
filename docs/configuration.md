# Client configuration

vimls-go is an stdio language server. Clients start `vimls`, send the normal
LSP `initialize` request, then send `initialized`. The server does not start
Vim, read vimrc files, or query a running editor.

## Initialize

The supported `initializationOptions` object is:

| Option | Type | Default | Behavior |
| --- | --- | --- | --- |
| `targetVersion` | string | `9.2.1015` | Accepts `major.minor`, `major.minor.patch`, or `latest`. The minimum is 9.1 and the maximum is 9.2.1015. An explicit value overrides later workspace target settings for the session. |
| `runtimepath` | string array | host Vim runtime discovery | Ordered runtime roots. An explicit empty array disables runtime indexing. Invalid, missing, unreadable, and duplicate realpaths are omitted. |
| `unresolvedSeverity` | string | `warning` | `error`, `warning`, `information`, or `hint` for unresolved-symbol diagnostics. |
| `workspaceRebuildDebounce` | number | `100` | Non-negative integer milliseconds to wait after the latest workspace rebuild trigger. `0` rebuilds immediately. |

A complete initialization fragment is:

```json
{
  "workspaceFolders": [
    { "uri": "file:///project", "name": "project" }
  ],
  "initializationOptions": {
    "targetVersion": "9.2.1015",
    "runtimepath": [
      "/usr/local/share/vim/vim92",
      "/project/.vim"
    ],
    "unresolvedSeverity": "warning",
    "workspaceRebuildDebounce": 100
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
`**/*.vim` watchers below the active workspace and runtime roots. The client
owns those watchers and sends `workspace/didChangeWatchedFiles`; vimls-go does
not poll or install an operating-system watcher itself.

## Workspace settings

If the client advertises `workspace.configuration`, vimls-go requests the
`vimls` section after `initialized` and after a
`workspace/didChangeConfiguration` notification with null settings. The
section supports `targetVersion`, `unresolvedSeverity`, and `workspaceRebuildDebounce`:

```json
{
  "targetVersion": "9.2.1015",
  "unresolvedSeverity": "hint",
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
      "vimls": {
        "targetVersion": "9.2.1015",
        "unresolvedSeverity": "warning",
        "workspaceRebuildDebounce": 100
      }
    }
  }
}
```

An initialization `targetVersion` is a deliberate session override, so later
workspace settings do not replace it. `unresolvedSeverity` and
`workspaceRebuildDebounce` remain dynamically configurable. Invalid updates
retain the previous valid value and produce a visible warning.

## Diagnostic policy

Diagnostic categories have stable default severities:

| Category | LSP severity | Configurable or disabled |
| --- | --- | --- |
| Parser and structural errors | error | fixed; cannot be disabled |
| Target-version compatibility | error | fixed; selecting a compatible `targetVersion` removes the mismatch |
| Unresolved function, variable, import alias, or autoload name (`E117`, `E121`, `E1001`, `E1089`) | warning | severity may be `error`, `warning`, `information`, or `hint`; cannot be disabled |
| Runtime-dependent or cross-file conflict warnings (`E122`, `E174`, `E464`, `E705`, `E707`) | warning | fixed; cannot be disabled |
| Unused Vim9 variables | hint with the LSP `unnecessary` tag | fixed; cannot be disabled |
| Deprecated Vim9 references | hint with the LSP `deprecated` tag | fixed; cannot be disabled |

Unknown diagnostics default to error. The value `off` is deliberately invalid
for `unresolvedSeverity`; an invalid initialization value falls back to warning,
and an invalid workspace update retains the previous valid severity.

## Runtimepath changes

Runtimepath is not a workspace setting. Send the custom notification below
when the editor's effective runtimepath changes:

```json
{
  "jsonrpc": "2.0",
  "method": "vimls/didChangeRuntimepath",
  "params": {
    "runtimepath": [
      "/usr/local/share/vim/vim92",
      "/project/.vim"
    ]
  }
}
```

The message must be a notification, not a request. It atomically replaces the
ordered runtime roots, rebuilds the bounded Vim source index, and refreshes the
client-owned watcher registration when dynamic registration is active. Send an
empty array to disable runtime indexing.

Standard `workspace/didChangeWorkspaceFolders` changes workspace roots.
Standard `workspace/didChangeWatchedFiles` changes indexed `.vim` files after
the client reports created, changed, or deleted paths. Open document snapshots
always override disk content during these rebuilds.
