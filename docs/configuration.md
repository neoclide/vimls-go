# Editor setup and settings

The server executable is named `vimls`. It uses standard input and output,
so the usual launch command is just `vimls`, with no arguments.

## coc.nvim

Install the [coc-vimls extension](https://github.com/neoclide/coc-vimls):

```vim
:CocInstall coc-vimls
```

It downloads the server and passes the editor's runtimepath automatically.
Remove an old manual Vim language-server entry or competing extension so that
two servers do not report diagnostics for the same file.

Use `:CocCommand vimls.update` to update the downloaded server and
`:CocCommand vimls.openOutput` to view its logs. A custom local build can be
selected in `:CocConfig`:

```json
{
  "vimls.command": "/path/to/vimls-go/bin/vimls"
}
```

Restart the service after changing the executable path.

## vim-lsp

Install `vimls` on your `PATH`, then add this to your vimrc:

```vim
if executable('vimls')
  augroup vimls_lsp
    autocmd!
    autocmd User lsp_setup call lsp#register_server({
          \ 'name': 'vimls',
          \ 'cmd': {server_info -> ['vimls']},
          \ 'allowlist': ['vim'],
          \ 'initialization_options': {
          \   'runtimepath': globpath(&runtimepath, '', 0, 1),
          \ },
          \ })
  augroup END
endif
```

For Neovim's built-in client, use the [Neovim setup](neovim.md).

## Settings

All settings below are optional. In coc.nvim, use these full names in
`:CocConfig`. Other clients send the same values inside their `vim` settings
section.

| Setting | Default | What it changes |
| --- | --- | --- |
| `vim.diagnostic.disabled` | `[]` | Diagnostic codes to hide. Use the full code, such as `vim/E117`. |
| `vim.diagnostic.override` | `{}` | Change a code's severity to `error`, `warning`, `information` or `hint`. |
| `vim.diagnostic.maxNumber` | `1000` | Maximum diagnostics per file. Must be a positive integer. |
| `vim.suggest.excludeRuntimePath` | `false` | Hide completion items from runtime files outside the workspace. Navigation and diagnostics still use those files. |
| `vim.workspace.rebuildDebounce` | `100` | Milliseconds to wait after workspace changes before rebuilding the index. Use a non-negative integer. |

For example, in `:CocConfig`:

```json
{
  "vim.diagnostic.disabled": ["vimls/explicit-local-scope"],
  "vim.diagnostic.override": {
    "vimls/unused-variable": "information"
  }
}
```

Disabling a code takes precedence over changing its severity. The diagnostic
limit includes the notice that more results were omitted; errors are kept
before warnings and hints. See [diagnostics](diagnostics.md) for help choosing
which suggestions to keep.

These settings update while the server is running. Invalid values leave the
previous value in place and produce a warning.

## Runtimepath and configuration files

Clients can pass two startup options:

| Startup option | Value |
| --- | --- |
| `runtimepath` | An ordered list of absolute runtime directory paths. |
| `configFiles` | Paths or absolute glob patterns for files that should be treated as user configuration. Supports `~/`, `*` and `**`. |

For a client with an `initializationOptions` field:

```json
{
  "runtimepath": ["/usr/local/share/vim/vim92", "/home/me/.vim"],
  "configFiles": ["~/.vimrc", "~/.config/vim/**/*.vim"]
}
```

Use directories that exist on your machine. The client should pass its actual
runtimepath when possible, since this includes installed plugins. Missing,
unreadable and duplicate directories are ignored.

When no usable runtimepath is supplied at startup, including an empty list,
vimls-go tries `vim` on `PATH` to discover default runtime directories. This
clean process loads no user configuration or plugins. If it cannot run, the
server continues without runtime indexing.

Most vimrc-style files are recognized automatically. `configFiles` is useful
when your configuration lives in other files; it does not add search roots.
coc-vimls exposes it as `vim.configFiles` and requires a server restart after
changes. See [editing configuration files](userconfig.md) for how this affects
suggestions.

## Troubleshooting

**The server does not start.** Check `vimls --version` from the environment
used by the editor. A GUI editor may have a different `PATH` from your shell.
Use an absolute executable path if needed.

**A plugin function has no completion or help.** Check that the client sends
the plugin's runtime directory. The external scan covers `plugin`, `autoload`,
`import` and `doc`, with color-scheme names from `colors`. It does not scan
every directory in a plugin. The full scope is in
[language support](language-support.md#plugin-files-and-help).

**Help is missing just after opening a file.** Source indexing and help loading
run in the background. Try the hover again after loading finishes.

**Edited plugin help still shows old text.** Help files outside the workspace
are cached and not watched. Restart the server, or remove and then re-add the
runtime root. Resending an unchanged runtimepath does not reload retained files.

**A warning looks wrong.** Include a small reproducing script and the full
diagnostic code in a bug report. You can hide that code in the meantime.

## For client authors

Use file URIs for workspace folders and ordinary absolute paths for runtime
directories. The server registers Vim-file watchers below workspace folders
when the client supports them; it does not register watchers for external
runtimepath roots.

Send the `vim` section through `workspace/configuration` or a
`workspace/didChangeConfiguration` notification. For example, the section
value can be:

```json
{
  "diagnostic": {
    "disabled": ["vimls/explicit-local-scope"]
  }
}
```

Diagnostic settings and `suggest.excludeRuntimePath` reset to defaults when
omitted from a complete settings snapshot. An omitted `workspace.rebuildDebounce`
keeps its previous value. These are workspace settings, not startup options.

When the editor's runtimepath changes, send the custom
`vimls/didChangeRuntimepath` request:

```json
{
  "jsonrpc": "2.0",
  "id": 42,
  "method": "vimls/didChangeRuntimepath",
  "params": {
    "runtimepath": ["/usr/local/share/vim/vim92", "/home/me/.vim"]
  }
}
```

The result is `null`; notifications with the same method are also accepted.
The list replaces the previous one. An empty list sent **after startup** clears
runtime indexing and help without rerunning discovery.

The server keeps data for retained roots and coalesces rapid updates. Clients
can receive workspace progress, runtime scan logs, and refresh requests for
supported diagnostic, highlighting, inlay-hint and Code Lens features.
Document and workspace pull diagnostics are supported; older clients receive
push diagnostics.
