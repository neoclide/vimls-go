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
    "unresolvedSeverity": "warning"
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
section supports `targetVersion` and `unresolvedSeverity`:

```json
{
  "targetVersion": "9.2.1015",
  "unresolvedSeverity": "hint"
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
        "unresolvedSeverity": "warning"
      }
    }
  }
}
```

An initialization `targetVersion` is a deliberate session override, so later
workspace settings do not replace it. `unresolvedSeverity` remains dynamically
configurable. Invalid updates retain the previous valid value and produce a
visible warning.

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
