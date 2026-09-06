# Editing vimrc and other configuration files

Code that is normal in a vimrc can be a bad default for a shared plugin. For
example, a vimrc is expected to set global options and define the user's own
keys. vimls-go adjusts its suggestions to account for that.

This changes diagnostics and completion templates. It does not change the
script's language or execute the file.

## Which files count as configuration?

The server checks these rules in order:

1. A path matching `configFiles` is a configuration file.
2. Known names such as `.vimrc`, `vimrc`, `_vimrc`, `gvimrc`, `.gvimrc`,
   `_gvimrc`, `exrc`, `.exrc`, `init.vim` and `ginit.vim` count automatically.
3. Inside a workspace, files count unless they are under a standard Vim runtime
   directory such as `plugin`, `autoload`, `ftplugin` or `syntax`.
4. Other files inside runtime roots receive plugin-oriented suggestions.
5. A standalone file outside all those roots counts as configuration.

If your files are classified incorrectly, add their absolute paths or patterns
to `configFiles`. Patterns can use `~/`, `*` and `**`.
See [startup options](configuration.md#runtimepath-and-configuration-files)
for the client setting.

## Suggestions you will see

In configuration files, vimls-go does not warn about overwriting a global
configuration value, using global state, assigning user keys directly, or
omitting `<unique>` from a mapping.

Other suggestions are adjusted:

| Situation | What vimls-go reports |
| --- | --- |
| A recursive mapping | A hint, since reusing other mappings may be intentional. |
| `:set` at the top of a vimrc | No `set-vs-setlocal` warning. That warning is kept for buffer/window autocommands. |
| `function` or `command` without `!` | A reload-safety hint, using `vim/E122` or `vim/E174`. |
| An augroup that may accumulate commands on reload | `vimls/autocmd-group-not-cleared`. |
| A mapping defined before its leader assignment | `vimls/config-mapleader-order`. |
| A clearly repeated mapping in the same file | `vimls/duplicate-mapping`. |
| A plugin-style loaded guard that skips later reloads | `vimls/config-loaded-guard`. |

For example, set the leader before defining keys that use it:

```vim
let g:mapleader = ','
nnoremap <Leader>w :write<CR>
```

A reloadable augroup usually clears its old commands before adding new ones:

```vim
augroup my_vimrc
  autocmd!
  autocmd FileType vim setlocal shiftwidth=2
augroup END
```

These checks use the code in the file. They do not reconstruct the full order
in which your configuration and plugins run. Cross-file mapping conflicts and
dynamic `execute` commands can remain undetected.

All of the suggestions can be hidden or given a different severity through
the normal [diagnostic settings](configuration.md#settings).

## Completion and navigation

Completion includes augroup, function and mapping templates when the client
supports snippets. Function templates do not automatically add `!` in
configuration files. Global variables get a small preference when top-level
completion candidates otherwise tie.

Navigation follows static calls in autocommands, `<expr>` mappings and
`<Cmd>call ...<CR>` bodies. It also recognizes function names assigned to
`completefunc`, `omnifunc`, `operatorfunc` and `tagfunc`:

```vim
set omnifunc=MyComplete
```

You can jump from `MyComplete` to its definition when that function can be
resolved. Dynamically constructed callback names are not followed.
