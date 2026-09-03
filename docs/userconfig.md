# User configuration files

vimls-go treats a small set of files — your `vimrc`, `gvimrc`, `exrc`,
Neovim-compatible `init.vim`/`ginit.vim`, and any file you explicitly list —
as *user configuration files* and applies a tailored analysis on top of the
regular Vimscript support. Configuration files express user preferences:
top-level `g:` assignments, direct `<Leader>` mappings, and global `:set`
calls are normal there, while the same code would be flagged when it appears
inside a plugin.

The role never changes how a file is parsed and never executes it. It only
adjusts which diagnostics are reported and which completion templates are
offered. The behavior is pinned to Vim v9.2.1015.

## Which files get the configuration role

A file is analyzed as a user configuration file when any of these hold:

1. It matches an absolute `configFiles` pattern from
   `initializationOptions.configFiles` (see
   [configuration.md](configuration.md) for the option and the accepted
   `*`, `**`, and `~/` patterns).
2. Its name is a known configuration name: `.vimrc`, `vimrc`, `.gvimrc`,
   `gvimrc`, `.exrc`, `exrc`, `_vimrc`, `_gvimrc`, or the Neovim-compatible
   `init.vim` and `ginit.vim`.
3. It lives below a workspace root but not inside a standard runtime
   directory (`plugin/`, `autoload/`, `ftplugin/`, `syntax/`, …).
4. It belongs to no workspace or runtime root at all (a standalone file you
   opened directly).

Files inside runtime roots — plugin files, `$VIMRUNTIME` scripts, and other
indexed runtime directories — keep the regular plugin-oriented analysis.

## What changes in the configuration role

### Diagnostics that are turned off

The plugin-oriented warnings below are suppressed because they describe code
habits that are intentional in a vimrc:

- `vimls/configuration-overwrite`
- `vimls/global-internal-state`
- `vimls/direct-user-keymap`
- `vimls/mapping-without-unique`

### Diagnostics that are adjusted

- `vimls/recursive-map` is reported at **Hint** level (instead of Warning):
  recursive mappings in a vimrc often intentionally compose existing
  mappings. The `<Plug>` and `<script>` exceptions still apply.
- `vimls/set-vs-setlocal` is only suggested **inside buffer/window-directed
  autocommand bodies** (FileType, BufRead/BufEnter, Win*…). A top-level `:set`
  in a vimrc establishes global defaults and is not flagged.
- `vim/E122` (function) and `vim/E174` (user command) are only reported for
  statically provable duplicates in the same file. A single no-bang
  definition is not an error: Vim silently replaces same-script functions and
  commands when the file is sourced again.

### Diagnostics that are configuration-specific

- `vimls/autocmd-group-not-cleared` (Warning) understands reload safety in
  depth: a bare `autocmd!` inside the group, event/pattern-targeted clears,
  and `autocmd! event pattern cmd` replacements are recognized, while
  `++once`, conditional clears, and `execute`-generated autocommands are not
  treated as proof. The report carries related information pointing at the
  first uncovered persistent autocommand.
- `vimls/config-mapleader-order` (Warning) reports mappings that use
  `<Leader>`/`<LocalLeader>` before the corresponding `g:mapleader` /
  `g:maplocalleader` is assigned on the same straight-line path — such a
  mapping keeps the old leader, because leader keys are expanded when the
  mapping is defined.
- `vimls/duplicate-mapping` (Warning) reports statically confirmed
  same-key redefinitions (matching modes, LHS spelling, global/`<buffer>`
  scope, mapping vs abbreviation). `unmap` and `mapclear` reset the earlier
  definition.
- `vimls/config-loaded-guard` (Hint) flags the plugin-style
  `if exists('g:loaded_*') | finish | endif` guard in a vimrc: once the file
  has been sourced, later `:source` runs skip every edit below the guard. In
  Vim9 scripts the hint additionally explains that reload already cleared
  script-local items; `vim9script noclear` single-load designs are exempt.

All codes stay configurable through the normal `vim.diagnostic.disabled` and
`vim.diagnostic.override` settings, and are listed in
[diagnostics.md](diagnostics.md) with their defaults.

## Completion and navigation

- Snippet-capable clients get the usual augroup template (with `autocmd!` and
  `augroup END`), the Legacy `function … abort` and Vim9 `def … enddef`
  templates, plus a `<Leader>` mapping skeleton at the mapping key position.
  In the configuration role the `:function` block template omits the `!`,
  and no template adds `!`, `command!`, or `<unique>` by default.
- Static navigation works from `autocmd … call Func()`, `<expr>` mapping
  right-hand sides, `<Cmd>call Func()<CR>` payloads, and the callback options
  `completefunc`, `omnifunc`, `operatorfunc`, and `tagfunc` (`:set opt=Func`
  or `:let &opt = 'Func'`) straight to the function definition.

Completion candidate sets and ordering stay identical to the plugin role for
the same text: the configuration role only influences which rules are
relevant, never the semantics of a result.

## Notes

- The role is decided per document from its path at analysis time; nothing is
  written into the AST, and renaming or moving a file through a workspace
  root reclassifies it automatically when the workspace configuration
  changes.
- If a file is meaningful as a plugin *and* as user configuration, you can
  force the role with an explicit `configFiles` pattern.
