# Client configuration

vimls-go is an stdio language server. Clients start `vimls`, send the normal
LSP `initialize` request, then send `initialized`. The server does not start
Vim, read vimrc files, or query a running editor.

## Initialize

The supported `initializationOptions` object is:

| Option | Type | Default | Behavior |
| --- | --- | --- | --- |
| `runtimepath` | string array | Vim runtimepath discovery | Ordered runtime roots. If no usable roots remain at initialization, query Vim for its default runtimepath. Invalid, missing, unreadable, and duplicate realpaths are omitted. |
| `configFiles` | string array | `[]` | Absolute glob patterns or paths for files treated as user configuration files (e.g. `["~/.vimrc", "~/.config/nvim/**/*.vim"]`). Supports `*`, `**`, and `~/`. Non-absolute patterns are ignored. Files outside runtime directories (`plugin/`, `autoload/`, etc.) or named `vimrc`/`init.vim` are treated as config files by default. |

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
    ],
    "configFiles": [
      "~/.vimrc",
      "~/.config/nvim/**/*.vim"
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

When initialization supplies no usable runtimepath (including an omitted value
or an empty array), the server looks up `vim` on `PATH` and runs:

```sh
vim -u NORC --noplugin -i NONE -es -V1 \
  --cmd "echo json_encode(globpath(&runtimepath, '', 0, 1))|q"
```

`globpath()` returns an array with wildcard expansion, so paths containing
commas or spaces are preserved through JSON. The server captures both output
streams privately; with these flags Vim writes the JSON to stderr.
`-u NORC --noplugin` avoids loading vimrc files and plugins, and `-i NONE`
disables viminfo reads/writes. Discovery has a two-second timeout and a 64 KiB
output limit. Missing Vim, command failure, timeout, or invalid output silently
leaves runtimepath empty. Usable explicit client roots take precedence. This
replaces guessing runtime directories from conventional installation paths.

`configFiles` specifies absolute patterns for files treated as user configuration
files rather than plugin scripts. Patterns must be absolute (or start with `~/`);
non-absolute patterns are ignored. Patterns support `*`, `**`, and `~/`. By
default, files not under a standard runtime directory (like `plugin/` or
`autoload/`) are automatically treated as configuration files.

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
the capability receive no progress traffic. Reports name workspace folders only;
progress ends after their index is installed, before external runtimepath work.
Runtimepath roots outside workspace folders are scanned with at most four
goroutines. Each completed directory scan sends `window/logMessage` with its
path, file counts and elapsed scan time. Contained runtime roots are handled
by workspace discovery only.

After each workspace or runtimepath index installation, the server requests
`workspace/diagnostic/refresh` (pull-diagnostic clients only),
`workspace/semanticTokens/refresh`, `workspace/inlayHint/refresh`, and, when the
index is complete, `workspace/codeLens/refresh`. Each request requires the
corresponding client `refreshSupport` capability. Push-diagnostic clients get
updated diagnostics through reanalysis. Hover, completion and navigation use
the new index on their next request.

## Workspace settings

If the client advertises `workspace.configuration`, vimls-go requests the
`vim` section after `initialized` and after a
`workspace/didChangeConfiguration` notification with null settings. The
section's value is an object carrying the optional settings
`workspace.rebuildDebounce`, `diagnostic.disabled`, `diagnostic.override`,
`diagnostic.maxNumber`, and `suggest.excludeRuntimePath`:

```json
{
  "workspace": {
    "rebuildDebounce": 100
  },
  "diagnostic": {
    "disabled": ["vim/E117", "vimls/deprecated"],
    "override": {
      "vim/E121": "warning",
      "vimls/deprecated": "information"
    },
    "maxNumber": 1000
  },
  "suggest": {
    "excludeRuntimePath": true
  }
}
```

Every setting is optional. A missing, empty, or `null` setting is never an
error: it falls back to the documented default or keeps the previous value,
and produces no warning.

Clients that include settings in the notification can send the namespaced
form directly:

```json
{
  "jsonrpc": "2.0",
  "method": "workspace/didChangeConfiguration",
  "params": {
    "settings": {
      "vim": {
        "workspace": {
          "rebuildDebounce": 100
        }
      }
    }
  }
}
```

`workspace.rebuildDebounce` is dynamically configurable. When omitted, null,
or empty the previous value is kept; invalid updates retain the previous valid
value and produce a visible warning.

`suggest.excludeRuntimePath` is a boolean workspace setting, defaulting to
`false`. When enabled, completion omits candidates sourced from runtimepath
files outside the current workspace roots; files under a workspace root remain
eligible. Empty, missing, or `null` settings mean `false`. This setting does
not affect diagnostics or navigation. `workspace/symbol` always omits symbols
from files outside the current workspace roots, regardless of this setting.

Diagnostic visibility and protocol severity are configured in the same `vim`
section:

```json
{
  "vim": {
    "diagnostic": {
      "disabled": ["vim/E117", "vimls/deprecated"],
      "override": {
        "vim/E121": "information",
        "vimls/unused-variable": "warning"
      },
      "maxNumber": 1000
    }
  }
}
```

`diagnostic.disabled` contains exact, non-empty diagnostic-code strings. Codes
may be native `vim/E...`, `vimls/...`, or a future code; disabling takes
precedence over an entry in `diagnostic.override`. Override values are exactly
`error`, `warning`, `information`, or `hint` (lowercase); `off` is not a
severity. Overrides affect only published LSP diagnostics and do not change
the syntax or analysis result. Disabled diagnostics are removed before the
per-document diagnostic limit is applied.

`diagnostic.maxNumber` is a positive integer and defaults to `1000`. It includes
the truncation marker. When a document exceeds the limit, diagnostics are
retained in Error, Warning, Information, then Hint order.

All diagnostic settings are dynamic. A changed valid value reanalyzes open
documents and republishes their diagnostics. An unchanged value does not
invalidate diagnostics. A missing field in a complete configuration snapshot
resets `disabled` and `override` to empty and `maxNumber` to its default. Invalid
field values retain that field's previous value and produce a visible warning,
while the other fields can still update.
The settings are read from workspace configuration, not `initializationOptions`.

## Diagnostic policy

Diagnostic categories have stable default severities:

| Category | LSP severity | Configurable or disabled |
| --- | --- | --- |
| Parser and structural errors | error | configurable by exact code |
| Unresolved function, variable, import alias, or autoload name (`E117`, `E121`, `E1001`, `E1089`) | warning | configurable by exact code |
| Runtime-dependent or cross-file conflict warnings (`E122`, `E174`, `E464`, `E705`, `E707`) | warning | configurable by exact code |
| Unused Vim9 variables | hint with the LSP `unnecessary` tag | configurable by exact code |
| Deprecated Vim9 references | hint with the LSP `deprecated` tag | configurable by exact code |

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
supported for existing clients. Updates use a 100ms debounce; only the latest
pending list is processed. A newer update does not cancel an active runtimepath
batch: that batch finishes before the next debounced update starts. Explicit
request cancellation and shutdown still cancel their work. Workspace rebuilds
reuse retained external runtime analysis facts and colorscheme paths.
The ordered runtime roots are atomically
replaced after canonicalization and de-duplication; invalid, missing,
non-directory, and unreadable roots are silently dropped. A no-op changes
nothing. Reordering only updates runtime lookup precedence and import
resolution. Added roots discover and analyze only previously unindexed files;
removed roots discard only files outside every workspace or retained runtime
root. File watchers cover workspace roots only; runtimepath changes do not
refresh watcher registration or trigger a watcher rebuild. Runtimepath files
change when the client sends this request (or an explicit watched-file event).
Discovery and read failures inside runtime roots are skipped as absent;
directory discovery errors are included in the scan log.
After initialization, send an empty array to disable runtime indexing and clear
runtime help. This explicit update does not rerun default Vim discovery.

Global variable/function, autoload and complete `<Plug>(name)` mapping help from
each root's `doc/*.txt` is loaded into memory by one background goroutine. Runtimepath changes reuse
completed and in-flight work for retained roots, read only added roots, discard
removed roots, and apply the new ordering to duplicate help names. The first
nonempty entry in runtimepath/file/line order wins. No-op updates perform no
help I/O. Retained help files are not polled or watched; remove/re-add the root
or restart to reload them.

Hover, built-in completion resolve and built-in signature help only look up this
cache and never read or parse help files. Hover appends runtime help after the
existing signature/comment documentation as a separate Markdown item; plaintext
clients receive a separated plaintext section. While loading, requests return
whatever documentation is already available. New roots become available as
their scans finish. Removal takes effect immediately.

Unreadable, non-UTF-8, oversized or failed help files are skipped and logged;
an unexpected parser panic is isolated to that file. The scan continues and
shutdown cancels and joins the worker. Help collection accepts regular files up
to 16 MiB each, at most 20,000 cached files, and conservatively accounts for at
most 256 MiB of cached entry text and overhead. It executes no Vim scripts and
does not depend on generated `tags` files.

Standard `workspace/didChangeWorkspaceFolders` changes workspace roots.
Standard `workspace/didChangeWatchedFiles` changes indexed `.vim` files after
the client reports created, changed, or deleted paths. Open document snapshots
always override disk content during these rebuilds.
